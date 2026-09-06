//go:build linux || darwin

package validator

// Compact measurements have an explicit hotkey envelope version and signature
// domain. Every seal and trusted verification performs the complete v2 replay;
// a decoded envelope proves only its signature, never the referenced evidence.

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urfoundation/sn/crv4"
)

const (
	ReleaseMeasurementEnvelopeSchemaV2        = "urnetwork-validator-release-measurement-envelope-v2"
	ReleaseMeasurementEnvelopeSigningDomainV2 = "urnetwork/validator/release-measurement-envelope/v2"
)

// These are format widths, not capacities or invented signature values. The
// unsigned envelope already includes the empty JSON string delimiters.
const releaseMeasurementEnvelopeV2SigningFieldBytes = uint64(len("sha256:") + 64 + len("0x") + 128)

var (
	errReleaseMeasurementEnvelopeV2ControlBound = errors.New("compact measurement envelope exceeds its control bound")
	errReleaseMeasurementEnvelopeV2WireBound    = errors.New("compact measurement envelope exceeds its wire bound")
)

// Count encoding/json's exact flat-envelope bytes without allocating an
// escape-amplified string. Unknown field kinds or tag behavior fail closed;
// changing this wire shape requires updating its explicit admission too.
func releaseMeasurementEnvelopeV2WireSize(ctx context.Context, envelope *ReleaseMeasurementEnvelope, limit, reserved uint64) (uint64, error) {
	if ctx == nil || envelope == nil {
		return 0, errors.New("compact measurement envelope wire context is nil")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	used := uint64(0)
	charge := func(count uint64) error {
		if count > limit-used {
			return errReleaseMeasurementEnvelopeV2WireBound
		}
		used += count
		return nil
	}
	stringBytes := func(value string) error {
		if err := charge(2); err != nil {
			return err
		}
		for index := 0; index < len(value); {
			if err := ctx.Err(); err != nil {
				return err
			}
			char := value[index]
			width, encodedWidth := 1, uint64(1)
			switch {
			case char == '"' || char == '\\' || char == '\b' || char == '\f' || char == '\n' || char == '\r' || char == '\t':
				encodedWidth = 2
			case char < 0x20 || char == '<' || char == '>' || char == '&':
				encodedWidth = 6
			case char >= utf8.RuneSelf:
				var runeValue rune
				runeValue, width = utf8.DecodeRuneInString(value[index:])
				encodedWidth = uint64(width)
				if runeValue == utf8.RuneError && width == 1 || runeValue == '\u2028' || runeValue == '\u2029' {
					encodedWidth = 6
				}
			}
			if err := charge(encodedWidth); err != nil {
				return err
			}
			index += width
		}
		return nil
	}
	// Braces and the canonical trailing newline precede the fixed, bounded
	// future signing fields; no placeholder hash/signature is constructed.
	if err := charge(3); err != nil {
		return 0, err
	}
	if err := charge(reserved); err != nil {
		return 0, err
	}
	value := reflect.ValueOf(envelope).Elem()
	for index := 0; index < value.NumField(); index++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		field := value.Type().Field(index)
		name := field.Tag.Get("json")
		if field.PkgPath != "" || name == "" || name == "-" || strings.Contains(name, ",") {
			return 0, errors.New("compact measurement envelope has unsupported JSON field behavior")
		}
		if index != 0 {
			if err := charge(1); err != nil {
				return 0, err
			}
		}
		if err := stringBytes(name); err != nil {
			return 0, err
		}
		if err := charge(1); err != nil {
			return 0, err
		}
		switch fieldValue := value.Field(index); fieldValue.Kind() {
		case reflect.String:
			if err := stringBytes(fieldValue.String()); err != nil {
				return 0, err
			}
		case reflect.Uint16, reflect.Uint64:
			if err := charge(uint64(len(strconv.FormatUint(fieldValue.Uint(), 10)))); err != nil {
				return 0, err
			}
		default:
			return 0, errors.New("compact measurement envelope has an unsupported wire field")
		}
	}
	return used, ctx.Err()
}

// Both owned control storage (including the root struct) and complete
// canonical wire must fit before normalization, signing or artifact readers.
// Unsigned admission reserves the two fixed ASCII signing fields exactly.
func admitReleaseMeasurementEnvelopeV2Storage(ctx context.Context, envelope *ReleaseMeasurementEnvelope, maxControlBytes uint64, unsigned bool) error {
	if ctx == nil || envelope == nil || maxControlBytes == 0 || maxControlBytes > releaseMeasurementEnvelopeMaxArtifactSize {
		return errors.New("compact measurement envelope context or bound is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if envelope.Schema != ReleaseMeasurementEnvelopeSchemaV2 || envelope.MeasurementSchema != ReleaseMeasurementSchemaV2 {
		return errors.New("compact measurement envelope schema is invalid")
	}
	reserved := uint64(0)
	if unsigned {
		if envelope.SigningHash != "" || envelope.Signature != "" {
			return errors.New("compact measurement unsigned envelope contains signing fields")
		}
		reserved = releaseMeasurementEnvelopeV2SigningFieldBytes
	}
	remaining := maxControlBytes
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(envelope), &remaining); err != nil {
		return errors.Join(errReleaseMeasurementEnvelopeV2ControlBound, err)
	}
	if reserved > remaining {
		return errReleaseMeasurementEnvelopeV2ControlBound
	}
	if _, err := releaseMeasurementEnvelopeV2WireSize(ctx, envelope, maxControlBytes, reserved); err != nil {
		return err
	}
	return ctx.Err()
}

// Caller-owned control bounds precede marshaling and signature normalization.
// Domain selection is fixed by this entrypoint, not by a candidate schema.
func validateReleaseMeasurementEnvelopeV2(ctx context.Context, envelope *ReleaseMeasurementEnvelope, maxControlBytes uint64) error {
	if err := admitReleaseMeasurementEnvelopeV2Storage(ctx, envelope, maxControlBytes, false); err != nil {
		return err
	}
	if err := validateReleaseMeasurementEnvelopeFieldsWithHexWork(envelope, canonicalHexWork{}, ReleaseMeasurementEnvelopeSigningDomainV2); err != nil {
		return err
	}
	return ctx.Err()
}

// Capture the private signing implementation before callbacks can replace the
// caller's keypair. Only public-key acquisition is guarded: an uninitialized
// private key fails closed, while signing and callback panics remain untouched.
func ownReleaseMeasurementEnvelopeV2Hotkey(hotkey *crv4.Keypair) (owned *crv4.Keypair, publicKey [32]byte, resultErr error) {
	if hotkey == nil {
		return nil, publicKey, errors.New("compact measurement envelope signer is missing")
	}
	ownedHotkey := *hotkey
	owned = &ownedHotkey
	publicKey, resultErr = func() (key [32]byte, err error) {
		defer func() {
			if recover() != nil {
				key = [32]byte{}
				err = errors.New("compact measurement envelope signer public key is unavailable")
			}
		}()
		return owned.PublicKey(), nil
	}()
	if resultErr != nil {
		return nil, [32]byte{}, resultErr
	}
	if publicKey == ([32]byte{}) {
		return nil, publicKey, errors.New("compact measurement envelope signer public key is zero")
	}
	return owned, publicKey, nil
}

// Every required operator's independently authenticated activation belongs to
// the same expected native signer. This is consent binding, not chain history.
func validateReleaseMeasurementEnvelopeV2Authority(options ReleaseMeasurementV2Options, hotkey [32]byte, uid uint16) error {
	if hotkey == ([32]byte{}) || options.Expected.SelfUID != uid || options.MaxOperators == 0 || len(options.Operators) == 0 || uint64(len(options.Operators)) > options.MaxOperators {
		return errors.New("compact measurement envelope signer authority is incomplete")
	}
	for _, operator := range options.Operators {
		if operator.Expected.Activation.Hotkey != hotkey {
			return errors.New("compact measurement envelope signer differs from operator activation")
		}
	}
	if options.Settlement != nil {
		if len(options.Settlement.Operators) != len(options.Operators) {
			return errors.New("compact measurement envelope terminal signer census differs")
		}
		for _, operator := range options.Settlement.Operators {
			if operator.Expected.Activation.Hotkey != hotkey {
				return errors.New("compact measurement envelope signer differs from terminal activation")
			}
		}
	}
	return nil
}

// A candidate envelope's mirrored coordinates must equal the caller's pinned
// decision before any record/proof reader is allowed to run.
func releaseMeasurementEnvelopeV2Decision(envelope *ReleaseMeasurementEnvelope) ReleaseMeasurementV2Decision {
	return ReleaseMeasurementV2Decision{
		DeploymentID: envelope.DeploymentID, ChainID: envelope.ChainID, GenesisHash: envelope.GenesisHash,
		Coordinator: envelope.Coordinator, SettlementVault: envelope.SettlementVault,
		ValidatorID: envelope.ValidatorID, Netuid: envelope.Netuid, SubnetEpoch: envelope.SubnetEpoch,
		NativeSnapshotBlock: envelope.NativeSnapshotBlock, NativeSnapshotHash: envelope.NativeSnapshotHash,
		EVMSnapshotBlock: envelope.EVMSnapshotBlock, EVMSnapshotHash: envelope.EVMSnapshotHash,
		SettlementEpoch: envelope.SettlementEpoch, PolicyHash: envelope.PolicyHash,
		PreviousArtifactHash: envelope.PreviousArtifactHash, SelfUID: envelope.ValidatorUID,
	}
}

// Signs only after canonical decoding and full terminal/current record replay.
// Inputs are owned before callbacks; late errors or cancellation clear results.
// Options still require independently authenticated history and activation.
func SealReleaseMeasurementEnvelopeV2(ctx context.Context, measurement []byte, validatorUID uint16, hotkey *crv4.Keypair, preparedExtrinsicHash string, signedAt time.Time, options ReleaseMeasurementV2Options) (encoded []byte, contentHash string, envelope *ReleaseMeasurementEnvelope, resultErr error) {
	if ctx == nil {
		return nil, "", nil, errors.New("compact measurement envelope context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			encoded, contentHash, envelope = nil, "", nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, "", nil, err
	}
	if hotkey == nil || signedAt.IsZero() {
		return nil, "", nil, errors.New("compact measurement envelope signer or time is missing")
	}
	if options.MaxArtifactBytes == 0 || options.MaxArtifactBytes > releaseMeasurementEnvelopeMaxArtifactSize || len(measurement) == 0 || uint64(len(measurement)) > options.MaxArtifactBytes || options.MaxControlBytes == 0 || options.MaxControlBytes > releaseMeasurementEnvelopeMaxArtifactSize {
		return nil, "", nil, errors.New("compact measurement envelope artifact or control bound is invalid")
	}
	preparedHash, err := parseReleaseHex32("prepared extrinsic hash", preparedExtrinsicHash, false)
	if err != nil {
		return nil, "", nil, err
	}
	hotkey, publicKey, err := ownReleaseMeasurementEnvelopeV2Hotkey(hotkey)
	if err != nil {
		return nil, "", nil, err
	}
	if err := validateReleaseMeasurementEnvelopeV2Authority(options, publicKey, validatorUID); err != nil {
		return nil, "", nil, err
	}
	measurement = bytes.Clone(measurement)
	// Split the existing decode/replay chain only to insert complete envelope
	// admission before any terminal/current callback. Structural decode alone
	// is never acceptance and does not select or sign a trusted decision.
	artifact, err := decodeReleaseMeasurementV2Bytes(ctx, measurement, options.MaxArtifactBytes, options.MaxOperators)
	if err != nil {
		return nil, "", nil, fmt.Errorf("compact measurement envelope artifact: %w", err)
	}
	envelope = newReleaseMeasurementEnvelope(artifact, measurement, publicKey, validatorUID, preparedHash, signedAt, ReleaseMeasurementEnvelopeSchemaV2)
	if err := admitReleaseMeasurementEnvelopeV2Storage(ctx, envelope, options.MaxControlBytes, true); err != nil {
		return nil, "", nil, err
	}
	if _, err := VerifyReleaseMeasurementArtifactV2(ctx, artifact, options); err != nil {
		return nil, "", nil, fmt.Errorf("compact measurement envelope artifact: %w", err)
	}
	digest, err := releaseMeasurementEnvelopeSigningDigestWithDomain(envelope, ReleaseMeasurementEnvelopeSigningDomainV2)
	if err != nil {
		return nil, "", nil, err
	}
	envelope.SigningHash = "sha256:" + hex.EncodeToString(digest[:])
	signature, err := hotkey.Sign(digest[:])
	if err != nil {
		return nil, "", nil, err
	}
	envelope.Signature = "0x" + hex.EncodeToString(signature)
	if err := validateReleaseMeasurementEnvelopeV2(ctx, envelope, options.MaxControlBytes); err != nil {
		return nil, "", nil, err
	}
	encoded, err = canonicalReleaseMeasurementEnvelopeBytes(envelope)
	if err != nil || uint64(len(encoded)) > options.MaxControlBytes {
		return nil, "", nil, errors.Join(errors.New("compact measurement envelope exceeds its wire bound"), err)
	}
	return encoded, ReleaseMeasurementEnvelopeContentHash(encoded), envelope, nil
}

// Bounded strict decoding verifies the explicit v2 signature only. The trusted
// verifier below binds the expected signer and fully replays referenced bytes;
// historical eligibility remains an independently authenticated caller input.
func DecodeReleaseMeasurementEnvelopeV2(ctx context.Context, encoded []byte, maxControlBytes uint64) (*ReleaseMeasurementEnvelope, error) {
	if ctx == nil || maxControlBytes == 0 || maxControlBytes > releaseMeasurementEnvelopeMaxArtifactSize || len(encoded) == 0 || uint64(len(encoded)) > maxControlBytes {
		return nil, errors.New("compact measurement envelope exceeds its wire bound")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	envelope, err := decodeReleaseMeasurementEnvelopeFields(encoded)
	if err != nil {
		return nil, err
	}
	if err := validateReleaseMeasurementEnvelopeV2(ctx, envelope, maxControlBytes); err != nil {
		return nil, err
	}
	return envelope, nil
}

// Authenticates the expected signer, complete decision and immutable bytes,
// then fully replays both terminal and ordinary streams. No header-only or
// legacy fallback exists; results remain private until every reader closes.
func VerifyReleaseMeasurementEnvelopeV2(ctx context.Context, envelope *ReleaseMeasurementEnvelope, measurement []byte, expectedHotkey [32]byte, expectedUID uint16, expectedPreparedExtrinsicHash string, options ReleaseMeasurementV2Options) (artifact *ReleaseMeasurementArtifact, verified VerifiedReleaseMeasurementV2, resultErr error) {
	if ctx == nil {
		return nil, verified, errors.New("compact measurement envelope context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			artifact, verified = nil, VerifiedReleaseMeasurementV2{}
		}
	}()
	if err := validateReleaseMeasurementEnvelopeV2(ctx, envelope, options.MaxControlBytes); err != nil {
		return nil, verified, err
	}
	ownedEnvelope := *envelope
	envelope = &ownedEnvelope
	if err := validateReleaseMeasurementEnvelopeV2Authority(options, expectedHotkey, expectedUID); err != nil {
		return nil, verified, err
	}
	if envelope.ValidatorHotkey != releaseHex32(expectedHotkey) || releaseMeasurementEnvelopeV2Decision(envelope) != options.Expected || envelope.ValidatorUID != expectedUID {
		return nil, verified, errors.New("compact measurement envelope differs from expected signer or decision")
	}
	if envelope.PreparedExtrinsicHash != normalizeReleasePreparedHex32(expectedPreparedExtrinsicHash, canonicalHexWork{}) {
		return nil, verified, errors.New("compact measurement envelope prepared extrinsic hash differs")
	}
	if options.MaxArtifactBytes == 0 || options.MaxArtifactBytes > releaseMeasurementEnvelopeMaxArtifactSize || len(measurement) == 0 || uint64(len(measurement)) > options.MaxArtifactBytes || uint64(len(measurement)) != envelope.MeasurementArtifactSize {
		return nil, verified, errors.New("compact measurement envelope artifact size differs")
	}
	measurement = bytes.Clone(measurement)
	if envelope.MeasurementArtifactHash != ReleaseMeasurementContentHash(measurement) {
		return nil, verified, errors.New("compact measurement envelope artifact hash differs")
	}
	artifact, verified, err := DecodeReleaseMeasurementArtifactV2(ctx, measurement, options)
	if err != nil {
		return nil, VerifiedReleaseMeasurementV2{}, fmt.Errorf("compact measurement envelope artifact: %w", err)
	}
	if !releaseMeasurementEnvelopeMatchesArtifact(envelope, artifact, expectedUID) {
		return nil, VerifiedReleaseMeasurementV2{}, errors.New("compact measurement envelope fields differ from artifact")
	}
	return artifact, verified, nil
}

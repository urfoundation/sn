package validator

// release_measurement_envelope.go defines the validator-hotkey signature that
// authenticates one immutable release measurement artifact.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vedhavyas/go-subkey/v2/sr25519"

	"github.com/urfoundation/sn/v2026/crv4"
)

// ReleaseMeasurementEnvelopeSchema is the immutable signed-envelope format.
const ReleaseMeasurementEnvelopeSchema = "urnetwork-validator-release-measurement-envelope-v1"

// ReleaseMeasurementEnvelopeSigningDomain separates these signatures from
// every other sr25519 protocol message signed by the validator hotkey.
const ReleaseMeasurementEnvelopeSigningDomain = "urnetwork/validator/release-measurement-envelope/v1"

const releaseMeasurementEnvelopeSignatureScheme = "sr25519"

const releaseMeasurementEnvelopeMaxArtifactSize = 64 * 1024 * 1024

// ReleaseMeasurementEnvelope authenticates the identity, pinned snapshots and
// exact bytes of one independently verifiable release measurement artifact.
type ReleaseMeasurementEnvelope struct {
	Schema                  string `json:"schema"`
	MeasurementSchema       string `json:"measurement_schema"`
	DeploymentID            string `json:"deployment_id"`
	ChainID                 uint64 `json:"chain_id"`
	GenesisHash             string `json:"genesis_hash"`
	Coordinator             string `json:"coordinator"`
	SettlementVault         string `json:"settlement_vault"`
	ValidatorID             uint64 `json:"validator_id"`
	ValidatorHotkey         string `json:"validator_hotkey"`
	ValidatorUID            uint16 `json:"validator_uid"`
	Netuid                  uint16 `json:"netuid"`
	SubnetEpoch             uint64 `json:"subnet_epoch"`
	SettlementEpoch         uint64 `json:"settlement_epoch"`
	PolicyHash              string `json:"policy_hash"`
	PreviousArtifactHash    string `json:"previous_artifact_hash"`
	NativeSnapshotBlock     uint64 `json:"native_snapshot_block"`
	NativeSnapshotHash      string `json:"native_snapshot_hash"`
	EVMSnapshotBlock        uint64 `json:"evm_snapshot_block"`
	EVMSnapshotHash         string `json:"evm_snapshot_hash"`
	MeasurementArtifactHash string `json:"measurement_artifact_hash"`
	MeasurementArtifactSize uint64 `json:"measurement_artifact_size"`
	PreparedExtrinsicHash   string `json:"prepared_extrinsic_hash"`
	SignedAt                string `json:"signed_at"`
	SignatureScheme         string `json:"signature_scheme"`
	SigningHash             string `json:"signing_hash"`
	Signature               string `json:"signature"`
}

// canonicalReleaseMeasurementEnvelopeBytes renders the unique stored form.
func canonicalReleaseMeasurementEnvelopeBytes(envelope *ReleaseMeasurementEnvelope) ([]byte, error) {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// releaseMeasurementEnvelopeSigningDigest hashes the versioned domain and
// canonical unsigned envelope into the fixed-size sr25519 signing message.
func releaseMeasurementEnvelopeSigningDigest(envelope *ReleaseMeasurementEnvelope) ([32]byte, error) {
	if envelope == nil {
		return [32]byte{}, errors.New("release measurement envelope is nil")
	}
	unsigned := *envelope
	unsigned.SigningHash = ""
	unsigned.Signature = ""
	encoded, err := json.Marshal(&unsigned)
	if err != nil {
		return [32]byte{}, err
	}
	digestInput := make([]byte, 0, len(ReleaseMeasurementEnvelopeSigningDomain)+1+len(encoded))
	digestInput = append(digestInput, ReleaseMeasurementEnvelopeSigningDomain...)
	digestInput = append(digestInput, 0)
	digestInput = append(digestInput, encoded...)
	return sha256.Sum256(digestInput), nil
}

// parseReleaseMeasurementEnvelopeSignature requires canonical 64-byte hex.
func parseReleaseMeasurementEnvelopeSignature(encoded string) ([]byte, error) {
	if encoded != strings.ToLower(encoded) || len(encoded) != 130 || !strings.HasPrefix(encoded, "0x") {
		return nil, errors.New("release measurement envelope signature is not canonical 64-byte hex")
	}
	signature, err := hex.DecodeString(encoded[2:])
	if err != nil || len(signature) != 64 {
		return nil, errors.New("release measurement envelope signature is invalid")
	}
	return signature, nil
}

// validateReleaseMeasurementEnvelope checks canonical field representations
// and verifies the embedded hotkey signature without assigning signer trust.
func validateReleaseMeasurementEnvelope(envelope *ReleaseMeasurementEnvelope) error {
	if envelope == nil {
		return errors.New("release measurement envelope is nil")
	}
	if envelope.Schema != ReleaseMeasurementEnvelopeSchema || envelope.MeasurementSchema != ReleaseMeasurementSchema {
		return errors.New("release measurement envelope schema is invalid")
	}
	if envelope.DeploymentID == "" || envelope.DeploymentID != strings.TrimSpace(envelope.DeploymentID) || envelope.ChainID == 0 || envelope.ValidatorID == 0 || envelope.Netuid == 0 {
		return errors.New("release measurement envelope identity is incomplete")
	}
	if envelope.Coordinator != strings.ToLower(envelope.Coordinator) || envelope.SettlementVault != strings.ToLower(envelope.SettlementVault) || !common.IsHexAddress(envelope.Coordinator) || common.HexToAddress(envelope.Coordinator) == (common.Address{}) || !common.IsHexAddress(envelope.SettlementVault) || common.HexToAddress(envelope.SettlementVault) == (common.Address{}) {
		return errors.New("release measurement envelope contract identity is invalid")
	}
	if _, err := parseReleaseHex32("genesis hash", envelope.GenesisHash, false); err != nil {
		return err
	}
	hotkey, err := parseReleaseHex32("validator hotkey", envelope.ValidatorHotkey, false)
	if err != nil {
		return err
	}
	if _, err := parseReleaseHex32("policy hash", envelope.PolicyHash, false); err != nil {
		return err
	}
	if envelope.PreviousArtifactHash != "" {
		if _, err := parseReleaseContentHash(envelope.PreviousArtifactHash); err != nil {
			return fmt.Errorf("previous artifact hash: %w", err)
		}
	}
	if envelope.NativeSnapshotBlock == 0 || envelope.EVMSnapshotBlock == 0 {
		return errors.New("release measurement envelope snapshot block is zero")
	}
	if _, err := parseReleaseHex32("native snapshot hash", envelope.NativeSnapshotHash, false); err != nil {
		return err
	}
	if _, err := parseReleaseHex32("EVM snapshot hash", envelope.EVMSnapshotHash, false); err != nil {
		return err
	}
	if _, err := parseReleaseContentHash(envelope.MeasurementArtifactHash); err != nil {
		return fmt.Errorf("measurement artifact hash: %w", err)
	}
	if envelope.MeasurementArtifactSize == 0 || envelope.MeasurementArtifactSize > releaseMeasurementEnvelopeMaxArtifactSize {
		return errors.New("release measurement envelope artifact size is invalid")
	}
	if _, err := parseReleaseHex32("prepared extrinsic hash", envelope.PreparedExtrinsicHash, false); err != nil {
		return err
	}
	signedAt, err := time.Parse(time.RFC3339Nano, envelope.SignedAt)
	if err != nil || envelope.SignedAt != signedAt.UTC().Format(time.RFC3339Nano) {
		return errors.New("release measurement envelope signing time is not canonical UTC")
	}
	if envelope.SignatureScheme != releaseMeasurementEnvelopeSignatureScheme {
		return errors.New("release measurement envelope signature scheme is invalid")
	}
	signingHash, err := parseReleaseContentHash(envelope.SigningHash)
	if err != nil {
		return fmt.Errorf("release measurement envelope signing hash: %w", err)
	}
	digest, err := releaseMeasurementEnvelopeSigningDigest(envelope)
	if err != nil {
		return err
	}
	if signingHash != digest {
		return errors.New("release measurement envelope signing hash differs")
	}
	signature, err := parseReleaseMeasurementEnvelopeSignature(envelope.Signature)
	if err != nil {
		return err
	}
	publicKey, err := (sr25519.Scheme{}).FromPublicKey(hotkey[:])
	if err != nil || !publicKey.Verify(digest[:], signature) {
		return errors.New("release measurement envelope signature is invalid")
	}
	return nil
}

// ReleaseMeasurementEnvelopeContentHash returns the SHA-256 content address of
// canonical envelope bytes.
func ReleaseMeasurementEnvelopeContentHash(encoded []byte) string {
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// persistReleaseMeasurementEnvelope stores immutable validator-signed bytes
// beside their measurement and returns the intent-relative locator.
func persistReleaseMeasurementEnvelope(stateDir string, encoded []byte, contentHash string) (string, uint64, error) {
	if _, err := parseReleaseContentHash(contentHash); err != nil {
		return "", 0, err
	}
	relativePath := filepath.ToSlash(filepath.Join("measurements", "envelopes", strings.TrimPrefix(contentHash, "sha256:")+".json"))
	absolutePath := filepath.Join(stateDir, filepath.FromSlash(relativePath))
	if existing, err := os.ReadFile(absolutePath); err == nil {
		info, statErr := os.Lstat(absolutePath)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !bytes.Equal(existing, encoded) {
			return "", 0, errors.New("existing measurement envelope content address is not the expected private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", 0, err
	} else if err := atomicStateWrite(absolutePath, encoded, 0o600); err != nil {
		return "", 0, err
	}
	return relativePath, uint64(len(encoded)), nil
}

// SealReleaseMeasurementEnvelope validates the canonical measurement, binds
// its self UID, and signs its identity and content address with the hotkey.
func SealReleaseMeasurementEnvelope(measurement []byte, validatorUID uint16, hotkey *crv4.Keypair, preparedExtrinsicHash string, signedAt time.Time) ([]byte, string, *ReleaseMeasurementEnvelope, error) {
	if hotkey == nil {
		return nil, "", nil, errors.New("release measurement envelope hotkey is nil")
	}
	if signedAt.IsZero() {
		return nil, "", nil, errors.New("release measurement envelope signing time is zero")
	}
	if len(measurement) == 0 || len(measurement) > releaseMeasurementEnvelopeMaxArtifactSize {
		return nil, "", nil, errors.New("release measurement envelope artifact size is invalid")
	}
	preparedHash, err := parseReleaseHex32("prepared extrinsic hash", strings.ToLower(preparedExtrinsicHash), false)
	if err != nil || preparedExtrinsicHash != strings.ToLower(preparedExtrinsicHash) {
		return nil, "", nil, errors.New("release measurement envelope prepared extrinsic hash is not canonical")
	}
	artifact, _, err := DecodeReleaseMeasurementArtifact(measurement)
	if err != nil {
		return nil, "", nil, fmt.Errorf("release measurement envelope artifact: %w", err)
	}
	if artifact.SelfUID != validatorUID {
		return nil, "", nil, errors.New("release measurement envelope validator UID differs from artifact")
	}
	publicKey := hotkey.PublicKey()
	envelope := &ReleaseMeasurementEnvelope{
		Schema:                  ReleaseMeasurementEnvelopeSchema,
		MeasurementSchema:       artifact.Schema,
		DeploymentID:            artifact.DeploymentID,
		ChainID:                 artifact.ChainID,
		GenesisHash:             artifact.GenesisHash,
		Coordinator:             artifact.Coordinator,
		SettlementVault:         artifact.SettlementVault,
		ValidatorID:             artifact.ValidatorID,
		ValidatorHotkey:         releaseHex32(publicKey),
		ValidatorUID:            validatorUID,
		Netuid:                  artifact.Netuid,
		SubnetEpoch:             artifact.SubnetEpoch,
		SettlementEpoch:         artifact.SettlementEpoch,
		PolicyHash:              artifact.PolicyHash,
		PreviousArtifactHash:    artifact.PreviousArtifactHash,
		NativeSnapshotBlock:     artifact.NativeSnapshotBlock,
		NativeSnapshotHash:      artifact.NativeSnapshotHash,
		EVMSnapshotBlock:        artifact.EVMSnapshotBlock,
		EVMSnapshotHash:         artifact.EVMSnapshotHash,
		MeasurementArtifactHash: ReleaseMeasurementContentHash(measurement),
		MeasurementArtifactSize: uint64(len(measurement)),
		PreparedExtrinsicHash:   releaseHex32(preparedHash),
		SignedAt:                signedAt.UTC().Format(time.RFC3339Nano),
		SignatureScheme:         releaseMeasurementEnvelopeSignatureScheme,
	}
	digest, err := releaseMeasurementEnvelopeSigningDigest(envelope)
	if err != nil {
		return nil, "", nil, err
	}
	envelope.SigningHash = "sha256:" + hex.EncodeToString(digest[:])
	signature, err := hotkey.Sign(digest[:])
	if err != nil {
		return nil, "", nil, err
	}
	envelope.Signature = "0x" + hex.EncodeToString(signature)
	if err := validateReleaseMeasurementEnvelope(envelope); err != nil {
		return nil, "", nil, err
	}
	encoded, err := canonicalReleaseMeasurementEnvelopeBytes(envelope)
	if err != nil {
		return nil, "", nil, err
	}
	return encoded, ReleaseMeasurementEnvelopeContentHash(encoded), envelope, nil
}

// DecodeReleaseMeasurementEnvelope accepts only the exact canonical encoding
// and verifies its self-declared sr25519 signature.
func DecodeReleaseMeasurementEnvelope(encoded []byte) (*ReleaseMeasurementEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var envelope ReleaseMeasurementEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("release measurement envelope contains trailing JSON")
		}
		return nil, err
	}
	canonical, err := canonicalReleaseMeasurementEnvelopeBytes(&envelope)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(encoded, canonical) {
		return nil, errors.New("release measurement envelope bytes are not canonical")
	}
	if err := validateReleaseMeasurementEnvelope(&envelope); err != nil {
		return nil, err
	}
	return &envelope, nil
}

// VerifyReleaseMeasurementEnvelope pins the trusted hotkey and UID, verifies
// the exact artifact bytes, and cross-checks every mirrored measurement field.
func VerifyReleaseMeasurementEnvelope(envelope *ReleaseMeasurementEnvelope, measurement []byte, expectedHotkey [32]byte, expectedUID uint16, expectedPreparedExtrinsicHash string) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, error) {
	if err := validateReleaseMeasurementEnvelope(envelope); err != nil {
		return nil, nil, err
	}
	if expectedHotkey == ([32]byte{}) {
		return nil, nil, errors.New("expected validator hotkey is zero")
	}
	encodedExpectedHotkey := releaseHex32(expectedHotkey)
	if envelope.ValidatorHotkey != encodedExpectedHotkey {
		return nil, nil, errors.New("release measurement envelope signer is not the expected validator hotkey")
	}
	if envelope.ValidatorUID != expectedUID {
		return nil, nil, errors.New("release measurement envelope UID is not the expected pinned UID")
	}
	if envelope.PreparedExtrinsicHash != strings.ToLower(expectedPreparedExtrinsicHash) {
		return nil, nil, errors.New("release measurement envelope prepared extrinsic hash differs")
	}
	if len(measurement) == 0 || len(measurement) > releaseMeasurementEnvelopeMaxArtifactSize || envelope.MeasurementArtifactSize != uint64(len(measurement)) {
		return nil, nil, errors.New("release measurement envelope artifact size differs")
	}
	if envelope.MeasurementArtifactHash != ReleaseMeasurementContentHash(measurement) {
		return nil, nil, errors.New("release measurement envelope artifact hash differs")
	}
	artifact, verified, err := DecodeReleaseMeasurementArtifact(measurement)
	if err != nil {
		return nil, nil, fmt.Errorf("release measurement envelope artifact: %w", err)
	}
	if artifact.Schema != envelope.MeasurementSchema || artifact.DeploymentID != envelope.DeploymentID || artifact.ChainID != envelope.ChainID || artifact.GenesisHash != envelope.GenesisHash || artifact.Coordinator != envelope.Coordinator || artifact.SettlementVault != envelope.SettlementVault || artifact.ValidatorID != envelope.ValidatorID || artifact.SelfUID != envelope.ValidatorUID || artifact.SelfUID != expectedUID || artifact.Netuid != envelope.Netuid || artifact.SubnetEpoch != envelope.SubnetEpoch || artifact.SettlementEpoch != envelope.SettlementEpoch || artifact.PolicyHash != envelope.PolicyHash || artifact.PreviousArtifactHash != envelope.PreviousArtifactHash || artifact.NativeSnapshotBlock != envelope.NativeSnapshotBlock || artifact.NativeSnapshotHash != envelope.NativeSnapshotHash || artifact.EVMSnapshotBlock != envelope.EVMSnapshotBlock || artifact.EVMSnapshotHash != envelope.EVMSnapshotHash {
		return nil, nil, errors.New("release measurement envelope fields differ from artifact")
	}
	digest, err := releaseMeasurementEnvelopeSigningDigest(envelope)
	if err != nil {
		return nil, nil, err
	}
	signature, err := parseReleaseMeasurementEnvelopeSignature(envelope.Signature)
	if err != nil {
		return nil, nil, err
	}
	publicKey, err := (sr25519.Scheme{}).FromPublicKey(expectedHotkey[:])
	if err != nil || !publicKey.Verify(digest[:], signature) {
		return nil, nil, errors.New("release measurement envelope signature does not verify under expected hotkey")
	}
	return artifact, verified, nil
}

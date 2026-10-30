// Package headsummary implements docs/spec/head-summary-v1.md and
// docs/spec/head-summary-v2.md: the signed per-tempo envelope in which a
// validator publishes its head result to an operator, its canonical
// encoding, its sr25519 signature, and the strict decoder an operator uses to
// ingest it. The scores, order and selection are protocol/headscore's.
//
// Version 1 carries only the result: fleet identities, exact scores, ranks and
// selection. Version 2 adds exact cut references and the recipient operator's
// eligible client census. Neither carries client keys or measurement artifacts.
package headsummary

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/vedhavyas/go-subkey/v2/sr25519"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/protocol/headscore"
)

const (
	// Schema names version 1 of the envelope.
	Schema = "urnetwork-validator-head-summary-v1"
	// SchemaV2 authenticates the exact published cuts and their eligible bindings.
	SchemaV2 = "urnetwork-validator-head-summary-v2"
	// SigningDomain separates these signatures from every other message the
	// validator hotkey signs.
	SigningDomain   = "urnetwork/validator/head-summary/v1"
	SigningDomainV2 = "urnetwork/validator/head-summary/v2"
	// SignatureScheme is the hotkey's signature scheme.
	SignatureScheme = "sr25519"
	// Path is the operator route that receives a summary.
	Path = "/sn/head-summary"
	// MaxBytes bounds an encoded summary; the operator answers 413 above it.
	MaxBytes = 16 * 1024 * 1024
	// MaxEntries bounds the eligible head UIDs of one summary.
	MaxEntries = 4096
	// These are v2 wire limits, shared by its publisher and strict decoder.
	MaxEvidenceCuts     = 4096
	MaxEvidenceBindings = 65536
)

// Summary is the envelope. Its fields are in canonical order; see the spec
// for each one.
type Summary struct {
	Schema                          string           `json:"schema"`
	Netuid                          uint16           `json:"netuid"`
	NativeEpoch                     uint64           `json:"native_epoch"`
	NativeSnapshotBlock             uint64           `json:"native_snapshot_block"`
	NativeSnapshotHash              string           `json:"native_snapshot_hash"`
	EVMSnapshotBlock                uint64           `json:"evm_snapshot_block"`
	EVMSnapshotHash                 string           `json:"evm_snapshot_hash"`
	SettlementEpoch                 uint64           `json:"settlement_epoch"`
	PolicyID                        uint64           `json:"policy_id"`
	PolicyHash                      string           `json:"policy_hash"`
	MeasurementArtifactHash         string           `json:"measurement_artifact_hash"`
	PreviousMeasurementArtifactHash string           `json:"previous_measurement_artifact_hash,omitempty"`
	EvidenceEmptyPrior              bool             `json:"evidence_empty_prior,omitempty"`
	EvidenceRecipient               uint64           `json:"evidence_recipient,omitempty"`
	EvidenceOperators               []uint64         `json:"evidence_operators,omitempty"`
	EvidenceCuts                    []EvidenceCut    `json:"evidence_cuts,omitempty"`
	EvidencePolicy                  *protocol.Policy `json:"evidence_policy,omitempty"`
	ValidatorHotkey                 string           `json:"validator_hotkey"`
	ValidatorUID                    uint16           `json:"validator_uid"`
	MaximumHeadFleets               uint16           `json:"maximum_head_fleets"`
	Entries                         []Entry          `json:"entries"`
	SelectedUIDs                    []uint16         `json:"selected_uids"`
	RejectedUIDs                    []uint16         `json:"rejected_uids"`
	SignedAt                        string           `json:"signed_at"`
	SignatureScheme                 string           `json:"signature_scheme"`
	SigningHash                     string           `json:"signing_hash"`
	Signature                       string           `json:"signature"`
}

// Entry is one eligible head UID.
type Entry struct {
	// Rank is the 1-based position in headscore's rank order.
	Rank   uint32             `json:"rank"`
	UID    uint16             `json:"uid"`
	Hotkey string             `json:"hotkey"`
	Score  headscore.Rational `json:"score"`
	// Selected is true exactly when Rank ≤ MaximumHeadFleets.
	Selected bool `json:"selected"`
	// Fleets are the live identities whose EMA values sum to Score.
	Fleets []Fleet `json:"fleets"`
}

// Fleet is one live fleet identity of an entry's UID and hotkey.
type Fleet struct {
	FleetID    string `json:"fleet_id"`
	Generation uint64 `json:"generation"`
}

// EvidenceCut identifies an already published canonical attempt-cut-v2 header.
// Its signed content hash authenticates the header's complete context and both
// stream roots. Bindings are the eligible census at this measurement's pinned
// native and coordinator view, including live identities with no completed hop.
// They contain no client keys, key captures, latencies or settlement inputs.
type EvidenceCut struct {
	NoId            uint64            `json:"no_id"`
	NativeBlock     uint64            `json:"native_block"`
	NativeBlockHash string            `json:"native_block_hash"`
	ContentHash     string            `json:"content_hash"`
	Bindings        []EvidenceBinding `json:"bindings"`
}

// One eligible historical identity, scoped to its owning operator's envelope.
type EvidenceBinding struct {
	ClientId   string `json:"client_id"`
	FleetId    string `json:"fleet_id"`
	Hotkey     string `json:"hotkey"`
	Generation uint64 `json:"generation"`
	Uid        uint16 `json:"uid"`
}

// Hex32 renders a 32-byte value as the envelope's lowercase 0x hex.
func Hex32(value [32]byte) string { return "0x" + hex.EncodeToString(value[:]) }

// ParseHex32 accepts only the envelope's lowercase 0x hex of 32 bytes.
func ParseHex32(value string) ([32]byte, error) {
	var out [32]byte
	if len(value) != 66 || !strings.HasPrefix(value, "0x") || strings.ToLower(value) != value {
		return out, errors.New("value is not lowercase 0x hex of 32 bytes")
	}
	if _, err := hex.Decode(out[:], []byte(value[2:])); err != nil {
		return out, errors.New("value is not lowercase 0x hex of 32 bytes")
	}
	return out, nil
}

func parseContentHash(value string) error {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") || strings.ToLower(value) != value {
		return errors.New("value is not a lowercase sha256 content hash")
	}
	if _, err := hex.DecodeString(value[7:]); err != nil {
		return errors.New("value is not a lowercase sha256 content hash")
	}
	return nil
}

func parseSignature(value string) ([]byte, error) {
	if len(value) != 130 || !strings.HasPrefix(value, "0x") || strings.ToLower(value) != value {
		return nil, errors.New("signature is not lowercase 0x hex of 64 bytes")
	}
	signature, err := hex.DecodeString(value[2:])
	if err != nil {
		return nil, errors.New("signature is not lowercase 0x hex of 64 bytes")
	}
	return signature, nil
}

func nonzeroHex32(name, value string) ([32]byte, error) {
	parsed, err := ParseHex32(value)
	if err != nil {
		return parsed, fmt.Errorf("%s: %w", name, err)
	}
	if parsed == ([32]byte{}) {
		return parsed, fmt.Errorf("%s is zero", name)
	}
	return parsed, nil
}

// validateContent checks every field but the signature: formats, the rank
// order, the selection boundary and the selected and rejected lists. The
// selection is reconstructed from the exact scores by headscore.
func (self *Summary) validateContent() error {
	if self == nil {
		return errors.New("head summary is nil")
	}
	if self.Schema != Schema && self.Schema != SchemaV2 {
		return fmt.Errorf("head summary schema %q is unknown", self.Schema)
	}
	if self.Netuid == 0 || self.NativeSnapshotBlock == 0 || self.EVMSnapshotBlock == 0 || self.PolicyID == 0 || self.MaximumHeadFleets == 0 {
		return errors.New("head summary identity is incomplete")
	}
	for _, field := range [][2]string{{"native snapshot hash", self.NativeSnapshotHash}, {"EVM snapshot hash", self.EVMSnapshotHash}, {"policy hash", self.PolicyHash}} {
		if _, err := ParseHex32(field[1]); err != nil {
			return fmt.Errorf("head summary %s: %w", field[0], err)
		}
	}
	if err := parseContentHash(self.MeasurementArtifactHash); err != nil {
		return fmt.Errorf("head summary measurement artifact hash: %w", err)
	}
	if err := self.validateEvidence(); err != nil {
		return err
	}
	if _, err := nonzeroHex32("head summary validator hotkey", self.ValidatorHotkey); err != nil {
		return err
	}
	if self.Entries == nil || self.SelectedUIDs == nil || self.RejectedUIDs == nil {
		return errors.New("head summary lists must be present, even when empty")
	}
	if len(self.Entries) > MaxEntries {
		return errors.New("head summary has too many entries")
	}
	uids := make([]uint16, len(self.Entries))
	scores := make([]headscore.Rational, len(self.Entries))
	for index, entry := range self.Entries {
		if entry.Rank != uint32(index)+1 {
			return fmt.Errorf("head summary entry %d has rank %d", index, entry.Rank)
		}
		if entry.Selected != (entry.Rank <= uint32(self.MaximumHeadFleets)) {
			return fmt.Errorf("head summary entry %d selection differs from its rank", index)
		}
		if _, err := nonzeroHex32(fmt.Sprintf("head summary entry %d hotkey", index), entry.Hotkey); err != nil {
			return err
		}
		if len(entry.Fleets) == 0 {
			return fmt.Errorf("head summary entry %d has no fleet", index)
		}
		previous := ""
		for position, fleet := range entry.Fleets {
			if _, err := nonzeroHex32(fmt.Sprintf("head summary entry %d fleet %d id", index, position), fleet.FleetID); err != nil {
				return err
			}
			if fleet.Generation == 0 {
				return fmt.Errorf("head summary entry %d fleet %d generation is zero", index, position)
			}
			// Hex of equal length orders as the bytes; the generation is
			// fixed-width here so it orders numerically.
			order := fmt.Sprintf("%s:%020d", fleet.FleetID, fleet.Generation)
			if position > 0 && order <= previous {
				return fmt.Errorf("head summary entry %d fleets are not strictly ordered", index)
			}
			previous = order
		}
		uids[index], scores[index] = entry.UID, entry.Score
	}
	// Positive canonical scores, unique UIDs, rank order, and the selected and
	// rejected lists at the boundary, all reconstructed from the scores.
	if err := headscore.ValidateSelection(uids, scores, self.SelectedUIDs, self.RejectedUIDs, self.MaximumHeadFleets); err != nil {
		return fmt.Errorf("head summary selection: %w", err)
	}
	signedAt, err := time.Parse(time.RFC3339Nano, self.SignedAt)
	if err != nil || self.SignedAt != signedAt.UTC().Format(time.RFC3339Nano) {
		return errors.New("head summary signing time is not canonical UTC")
	}
	if self.SignatureScheme != SignatureScheme {
		return errors.New("head summary signature scheme is not sr25519")
	}
	return nil
}

// canonical renders the exact bytes of the envelope as it stands.
func (self *Summary) canonical() ([]byte, error) {
	return json.Marshal(self)
}

// SigningDigest is SHA-256 of the signing domain, a zero byte, and the
// canonical encoding with signing_hash and signature set to "".
func SigningDigest(summary *Summary) ([32]byte, error) {
	if summary == nil {
		return [32]byte{}, errors.New("head summary is nil")
	}
	unsigned := *summary
	unsigned.SigningHash, unsigned.Signature = "", ""
	encoded, err := unsigned.canonical()
	if err != nil {
		return [32]byte{}, err
	}
	domain := SigningDomain
	if summary.Schema == SchemaV2 {
		domain = SigningDomainV2
	}
	input := make([]byte, 0, len(domain)+1+len(encoded))
	input = append(input, domain...)
	input = append(input, 0)
	input = append(input, encoded...)
	return sha256.Sum256(input), nil
}

// Sign completes an unsigned summary in place with the signing time, scheme,
// digest and the hotkey's sr25519 signature, and returns its canonical bytes.
// sign is the hotkey's signer over a message (such as crv4.Keypair.Sign);
// hotkey must be its public key and the summary's validator_hotkey.
func Sign(summary *Summary, hotkey [32]byte, signedAt time.Time, sign func(message []byte) ([]byte, error)) ([]byte, error) {
	if summary == nil || sign == nil || signedAt.IsZero() {
		return nil, errors.New("head summary signing is incomplete")
	}
	if summary.ValidatorHotkey != Hex32(hotkey) {
		return nil, errors.New("head summary validator hotkey is not the signer")
	}
	summary.SignedAt = signedAt.UTC().Format(time.RFC3339Nano)
	summary.SignatureScheme = SignatureScheme
	summary.SigningHash, summary.Signature = "", ""
	if err := summary.validateContent(); err != nil {
		return nil, err
	}
	digest, err := SigningDigest(summary)
	if err != nil {
		return nil, err
	}
	signature, err := sign(digest[:])
	if err != nil {
		return nil, err
	}
	summary.SigningHash = "sha256:" + hex.EncodeToString(digest[:])
	summary.Signature = "0x" + hex.EncodeToString(signature)
	if err := verify(summary); err != nil {
		return nil, err
	}
	encoded, err := summary.canonical()
	if err != nil {
		return nil, err
	}
	if len(encoded) > MaxBytes {
		return nil, errors.New("head summary exceeds its byte bound")
	}
	return encoded, nil
}

// verify checks the signing hash and the signature under validator_hotkey.
func verify(summary *Summary) error {
	digest, err := SigningDigest(summary)
	if err != nil {
		return err
	}
	if summary.SigningHash != "sha256:"+hex.EncodeToString(digest[:]) {
		return errors.New("head summary signing hash differs from its content")
	}
	signature, err := parseSignature(summary.Signature)
	if err != nil {
		return err
	}
	hotkey, err := ParseHex32(summary.ValidatorHotkey)
	if err != nil {
		return err
	}
	public, err := (sr25519.Scheme{}).FromPublicKey(hotkey[:])
	if err != nil || !public.Verify(digest[:], signature) {
		return errors.New("head summary signature is invalid")
	}
	return nil
}

// Decode accepts exactly one canonical, consistent, correctly signed summary
// of at most MaxBytes: no unknown or duplicate members, whitespace or
// trailing data, and a valid sr25519 signature by validator_hotkey. It does
// not decide whether that hotkey is a validator the operator trusts.
func Decode(body []byte) (*Summary, error) {
	if len(body) > MaxBytes {
		return nil, errors.New("head summary exceeds its byte bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var summary Summary
	if err := decoder.Decode(&summary); err != nil {
		return nil, fmt.Errorf("head summary is not one JSON envelope: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("head summary has trailing data")
	}
	if err := summary.validateContent(); err != nil {
		return nil, err
	}
	canonical, err := summary.canonical()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, body) {
		return nil, errors.New("head summary is not canonically encoded")
	}
	if err := verify(&summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

// SelectedEntries lists the selected entries, in rank order.
func (self *Summary) SelectedEntries() []Entry {
	return slices.DeleteFunc(slices.Clone(self.Entries), func(entry Entry) bool { return !entry.Selected })
}

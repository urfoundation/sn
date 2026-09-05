package validator

// Compact cuts authenticate bounded stream references instead of copying
// complete record histories into every measurement. Header verification is
// not stream replay: callers must still authenticate every referenced byte,
// record, server signature, lifecycle, count and terminal proof projection.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/urfoundation/sn/protocol"
)

const (
	AttemptCutV2Schema     = "urnetwork-validator-attempt-cut-v2"
	attemptCutV2SignDomain = "urnetwork-validator-attempt-cut-signature-v2\x00"
)

// The earlier activation record binds the migration prefix and policy. Its
// anchor must not hash this cut or the terminal census that contains this cut.
// Native hotkey is independent of its mutable UID and operator-scoped VPK.
type AttemptCutV2Activation struct {
	Domain        protocol.ValidatorEvidenceDomain `json:"domain"`
	Hotkey        [32]byte                         `json:"hotkey"`
	FirstSequence uint64                           `json:"first_sequence"`
	PriorRoot     string                           `json:"prior_root"`
}

// Caller-supplied, authenticated context pins an ordinary cut independently
// of its candidate header. EgressGeneration distinguishes successive native
// measurement cuts even when they share the same finalized EVM boundary.
type AttemptCutV2Context struct {
	Identity            AttemptLedgerIdentity  `json:"identity"`
	Activation          AttemptCutV2Activation `json:"activation"`
	Boundary            AttemptBoundary        `json:"boundary"`
	FirstSequence       uint64                 `json:"first_sequence"`
	EgressFirstSequence uint64                 `json:"egress_first_sequence"`
	EgressGeneration    uint64                 `json:"egress_generation"`
	PriorRoot           string                 `json:"prior_root"`
}

// All bounds are explicit caller policy. These types install no production
// defaults, approve no global cap increase and activate no v2 producer.
type AttemptCutV2Bounds struct {
	MaxHeaderBytes uint64
	Records        AttemptStreamV2Bounds
	Proofs         AttemptStreamV2Bounds
}

// The record root/range commits the existing ordered v1 record hash chain.
// Streams commit canonical JSONL bytes and their complete paged descriptor
// census. Completed and failed counts count terminal records, not repeated
// pending checkpoints. A fully replayed cut must contain no pending trail.
type AttemptCutV2 struct {
	Schema        string                   `json:"schema"`
	Context       AttemptCutV2Context      `json:"context"`
	LastSequence  uint64                   `json:"last_sequence"`
	RecordCount   uint64                   `json:"record_count"`
	CompleteCount uint64                   `json:"complete_count"`
	FailedCount   uint64                   `json:"failed_count"`
	Root          string                   `json:"root"`
	Records       AttemptStreamV2Reference `json:"records"`
	Proofs        AttemptStreamV2Reference `json:"proofs"`
	Signature     []byte                   `json:"signature,omitempty"`
}

// Rejects inconsistent seed/public halves before signing or persisting state.
func attemptCutV2PrivateKey(privateKey ed25519.PrivateKey, vpk ed25519.PublicKey) error {
	if len(privateKey) != ed25519.PrivateKeySize || len(vpk) != ed25519.PublicKeySize {
		return errors.New("compact attempt cut private key is invalid")
	}
	derived := ed25519.NewKeyFromSeed(privateKey[:ed25519.SeedSize])
	if subtle.ConstantTimeCompare(derived, privateKey) != 1 || subtle.ConstantTimeCompare(derived[ed25519.SeedSize:], vpk) != 1 {
		return errors.New("compact attempt cut private key does not match its validator")
	}
	return nil
}

// Binds duplicated ledger and chain context explicitly; matching a signature
// under a candidate-selected key is not evidence of the expected namespace.
func (self AttemptCutV2Context) Validate() error {
	vpk, err := canonicalAttemptHex32("compact attempt validator vpk", self.Identity.ValidatorVPK, false)
	if err != nil {
		return err
	}
	if err := validateAttemptLedgerIdentity(self.Identity, vpk[:]); err != nil {
		return err
	}
	if err := self.Activation.Domain.Validate(); err != nil {
		return err
	}
	genesis, err := canonicalAttemptHex32("compact attempt genesis", self.Identity.GenesisHash, false)
	if err != nil {
		return err
	}
	domain := self.Activation.Domain
	if domain.ChainID != self.Identity.ChainID || domain.Netuid != self.Identity.Netuid || domain.GenesisHash != genesis || domain.DeploymentIDHash != sha256.Sum256([]byte(self.Identity.DeploymentID)) || self.Activation.Hotkey == ([32]byte{}) {
		return errors.New("compact attempt activation differs from the ledger namespace")
	}
	if err := validateAttemptBoundary(self.Boundary); err != nil {
		return err
	}
	if self.Activation.FirstSequence == 0 || self.Activation.FirstSequence == ^uint64(0) || self.FirstSequence < self.Activation.FirstSequence || self.EgressFirstSequence < self.FirstSequence || self.Boundary.SettlementEpoch < domain.ActivationEpoch {
		return errors.New("compact attempt cut precedes its activation or range")
	}
	activationRoot, err := canonicalAttemptHex32("compact attempt activation root", self.Activation.PriorRoot, true)
	if err != nil {
		return err
	}
	priorRoot, err := canonicalAttemptHex32("compact attempt prior root", self.PriorRoot, true)
	if err != nil {
		return err
	}
	if (self.Activation.FirstSequence == 1) != (activationRoot == ([32]byte{})) || (self.FirstSequence == 1) != (priorRoot == ([32]byte{})) || self.FirstSequence == self.Activation.FirstSequence && self.PriorRoot != self.Activation.PriorRoot {
		return errors.New("compact attempt cut differs from its migration prefix")
	}
	return nil
}

// Checks bounded structure only. It does not trust the declared stream totals
// as observations; replay must recompute them from all canonical signed rows.
func (self AttemptCutV2) Validate(bounds AttemptCutV2Bounds) error {
	if bounds.MaxHeaderBytes == 0 {
		return errors.New("compact attempt header byte bound is missing")
	}
	if err := bounds.Records.Validate(); err != nil {
		return err
	}
	if err := bounds.Proofs.Validate(); err != nil {
		return err
	}
	if self.Schema != AttemptCutV2Schema {
		return errors.New("compact attempt cut schema is unsupported")
	}
	if uint64(len(self.Context.Identity.DeploymentID)) > bounds.MaxHeaderBytes {
		return errors.New("compact attempt deployment exceeds its header bound")
	}
	if err := self.Context.Validate(); err != nil {
		return err
	}
	if self.LastSequence == ^uint64(0) || self.Context.FirstSequence > self.LastSequence+1 || self.Context.EgressFirstSequence > self.LastSequence+1 || self.RecordCount != self.LastSequence+1-self.Context.FirstSequence {
		return errors.New("compact attempt cut range is inconsistent or overflows")
	}
	if self.CompleteCount > self.RecordCount || self.FailedCount > self.RecordCount-self.CompleteCount || self.Records.ItemCount != self.RecordCount || self.Proofs.ItemCount != self.CompleteCount {
		return errors.New("compact attempt cut terminal or stream counts differ")
	}
	root, err := canonicalAttemptHex32("compact attempt root", self.Root, true)
	if err != nil {
		return err
	}
	if self.RecordCount == 0 {
		if self.Root != self.Context.PriorRoot || self.Context.EgressFirstSequence != self.Context.FirstSequence {
			return errors.New("empty compact attempt cut root or egress cursor differs")
		}
	} else if root == ([32]byte{}) || self.CompleteCount == 0 && self.FailedCount == 0 {
		return errors.New("nonempty compact attempt cut has no terminal outcome or root")
	}
	if err := self.Records.Validate(bounds.Records); err != nil {
		return fmt.Errorf("compact attempt records: %w", err)
	}
	if err := self.Proofs.Validate(bounds.Proofs); err != nil {
		return fmt.Errorf("compact attempt proofs: %w", err)
	}
	if len(self.Signature) != 0 && len(self.Signature) != ed25519.SignatureSize {
		return errors.New("compact attempt signature length is invalid")
	}
	encoded, err := json.Marshal(self)
	if err != nil || uint64(len(encoded))+1 > bounds.MaxHeaderBytes {
		return errors.Join(errors.New("compact attempt cut exceeds its header byte bound"), err)
	}
	return nil
}

// The signature uses a distinct v2 ordinary-cut domain and excludes only its
// own signature field. Terminal publication uses ValidatorEvidenceHeader.
func (self AttemptCutV2) SigningDigest(bounds AttemptCutV2Bounds) ([32]byte, error) {
	if err := self.Validate(bounds); err != nil {
		return [32]byte{}, err
	}
	self.Signature = nil
	encoded, err := json.Marshal(self)
	if err != nil {
		return [32]byte{}, err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(attemptCutV2SignDomain))
	_, _ = digest.Write(encoded)
	var result [32]byte
	copy(result[:], digest.Sum(nil))
	return result, nil
}

// Returns a signature without changing the header or any caller-owned bytes.
func (self AttemptCutV2) Sign(privateKey ed25519.PrivateKey, bounds AttemptCutV2Bounds) ([]byte, error) {
	vpk, err := canonicalAttemptHex32("compact attempt validator vpk", self.Context.Identity.ValidatorVPK, false)
	if err != nil {
		return nil, err
	}
	if err := attemptCutV2PrivateKey(privateKey, vpk[:]); err != nil {
		return nil, err
	}
	digest, err := self.SigningDigest(bounds)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(privateKey, digest[:])
	self.Signature = signature
	if err := self.Validate(bounds); err != nil {
		return nil, err
	}
	return signature, nil
}

// Authenticates the exact caller-pinned context before checking the VPK
// signature. This deliberately makes no claim that the streams are complete.
func (self AttemptCutV2) VerifyHeader(expected AttemptCutV2Context, bounds AttemptCutV2Bounds) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	if self.Context != expected {
		return errors.New("compact attempt cut differs from expected context")
	}
	digest, err := self.SigningDigest(bounds)
	if err != nil {
		return err
	}
	vpk, err := canonicalAttemptHex32("compact attempt validator vpk", expected.Identity.ValidatorVPK, false)
	if err != nil {
		return err
	}
	if len(self.Signature) != ed25519.SignatureSize || !ed25519.Verify(vpk[:], digest[:], self.Signature) {
		return errors.New("compact attempt cut validator signature is invalid")
	}
	return nil
}

// Encodes the one supported public byte representation including its newline.
func (self AttemptCutV2) CanonicalJSON(bounds AttemptCutV2Bounds) ([]byte, error) {
	if err := self.Validate(bounds); err != nil {
		return nil, err
	}
	if len(self.Signature) != ed25519.SignatureSize {
		return nil, errors.New("compact attempt cut is unsigned")
	}
	encoded, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// Raw length is checked before decoding. Unknown, duplicate, reordered,
// noncanonical, unsigned and trailing data cannot become a compact header.
func DecodeAttemptCutV2(data []byte, expected AttemptCutV2Context, bounds AttemptCutV2Bounds) (*AttemptCutV2, error) {
	if bounds.MaxHeaderBytes == 0 || uint64(len(data)) > bounds.MaxHeaderBytes {
		return nil, errors.New("compact attempt cut exceeds its header byte bound")
	}
	var cut AttemptCutV2
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cut); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("compact attempt cut contains trailing JSON")
	}
	canonical, err := cut.CanonicalJSON(bounds)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, canonical) {
		return nil, errors.New("compact attempt cut is not canonical")
	}
	if err := cut.VerifyHeader(expected, bounds); err != nil {
		return nil, err
	}
	return &cut, nil
}

package protocol

// Fixed-width evidence authentication is independent of its storage transport.
// It proves hotkey/VPK consent, not historical validator eligibility, complete
// campaign coverage, or the truth/availability of the committed proof bytes.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"

	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

const (
	ValidatorEvidenceHeaderDomain             = "urnetwork/validator-evidence-header/v1"
	ValidatorEvidenceSlotDomain               = "urnetwork/validator-evidence-slot/v1"
	ValidatorEvidenceAuditSubjectDomain       = "urnetwork/validator-evidence-audit-subject/v1"
	ValidatorEvidenceClosedCensus       uint8 = 1
	ValidatorEvidenceDepositAudit       uint8 = 2
	ValidatorEvidenceDomainPayloadSize        = 8 + 32 + 2 + 20 + 20 + 32 + 32 + 8 + 32
	ValidatorEvidenceHeaderPayloadSize        = len(ValidatorEvidenceHeaderDomain) + 1 + ValidatorEvidenceDomainPayloadSize + 32 + 8 + 8 + 1 + 32 + 32 + 8 + 32 + 32 + 32 + 8
)

// ActivationHash names an earlier immutable activation record; neither it nor
// CensusHash may include this header or signatures that already name them.
type ValidatorEvidenceDomain struct {
	ChainID          uint64   `json:"chain_id"`
	GenesisHash      [32]byte `json:"genesis_hash"`
	Netuid           uint16   `json:"netuid"`
	Coordinator      [20]byte `json:"coordinator"`
	SettlementVault  [20]byte `json:"settlement_vault"`
	DeploymentIDHash [32]byte `json:"deployment_id_hash"`
	PolicyHash       [32]byte `json:"policy_hash"`
	ActivationEpoch  uint64   `json:"activation_epoch"`
	ActivationHash   [32]byte `json:"activation_hash"`
}

// A terminal census uses zero coordinates. Later audits identify the actual
// observation settlement epoch and native scheduler epoch, not an intent hash,
// verdict, payload, receipt or relayer. Native epoch zero is a valid coordinate.
type ValidatorEvidenceSubject struct {
	ObservationEpoch uint64 `json:"observation_epoch"`
	NativeEpoch      uint64 `json:"native_epoch"`
}

// One operator header always costs one hotkey and one VPK verification.
// CensusHash commits the shared canonical unsigned member payload hashes and
// counts; public replay still checks exact all-operator membership/completeness.
// JSON tags name transport fields only; signatures use the fixed binary payload.
type ValidatorEvidenceHeader struct {
	Domain        ValidatorEvidenceDomain  `json:"domain"`
	Hotkey        [32]byte                 `json:"hotkey"`
	NoID          uint64                   `json:"no_id"`
	Epoch         uint64                   `json:"epoch"`
	Kind          uint8                    `json:"kind"`
	Subject       ValidatorEvidenceSubject `json:"subject"`
	VPK           [32]byte                 `json:"vpk"`
	BoundaryBlock uint64                   `json:"boundary_block"`
	BoundaryHash  [32]byte                 `json:"boundary_hash"`
	CensusHash    [32]byte                 `json:"census_hash"`
	PayloadHash   [32]byte                 `json:"payload_hash"`
	PayloadBytes  uint64                   `json:"payload_bytes"`
}

// These expected values must come from authenticated policy/history, never
// from the candidate header. EndBlock is exclusive, matching epochEndBlock.
type ValidatorEvidenceWindow struct {
	Epoch          uint64                   `json:"epoch"`
	StartBlock     uint64                   `json:"start_block"`
	EndBlock       uint64                   `json:"end_block"`
	FinalizedBlock uint64                   `json:"finalized_block"`
	Subject        ValidatorEvidenceSubject `json:"subject"`
}

// Epoch zero is allowed for a fresh deployment; a nonzero activation anchor
// disambiguates it from absent activation state.
func (self ValidatorEvidenceDomain) Validate() error {
	if self.ChainID == 0 || self.Netuid == 0 || self.GenesisHash == ([32]byte{}) || self.Coordinator == ([20]byte{}) || self.SettlementVault == ([20]byte{}) || self.Coordinator == self.SettlementVault || self.DeploymentIDHash == ([32]byte{}) || self.PolicyHash == ([32]byte{}) || self.ActivationHash == ([32]byte{}) {
		return errors.New("validator evidence domain is incomplete")
	}
	return nil
}

// Produces Solidity's exact abi.encodePacked widths without integer casts.
func (self ValidatorEvidenceDomain) payload() []byte {
	data := make([]byte, 0, ValidatorEvidenceDomainPayloadSize)
	data = binary.BigEndian.AppendUint64(data, self.ChainID)
	data = append(data, self.GenesisHash[:]...)
	data = binary.BigEndian.AppendUint16(data, self.Netuid)
	data = append(data, self.Coordinator[:]...)
	data = append(data, self.SettlementVault[:]...)
	data = append(data, self.DeploymentIDHash[:]...)
	data = append(data, self.PolicyHash[:]...)
	data = binary.BigEndian.AppendUint64(data, self.ActivationEpoch)
	return append(data, self.ActivationHash[:]...)
}

// Rejects unsupported kinds and arbitrary terminal/audit subjects before signing.
func (self ValidatorEvidenceHeader) Validate() error {
	if err := self.Domain.Validate(); err != nil {
		return err
	}
	if self.Hotkey == ([32]byte{}) || self.VPK == ([32]byte{}) || self.NoID == 0 || self.Epoch < self.Domain.ActivationEpoch || self.BoundaryBlock == 0 || self.BoundaryHash == ([32]byte{}) || self.CensusHash == ([32]byte{}) || self.PayloadHash == ([32]byte{}) || self.PayloadBytes == 0 {
		return errors.New("validator evidence header is incomplete or precedes activation")
	}
	switch self.Kind {
	case ValidatorEvidenceClosedCensus:
		if self.Subject != (ValidatorEvidenceSubject{}) {
			return errors.New("closed validator evidence has a nonzero subject")
		}
	case ValidatorEvidenceDepositAudit:
		if self.Subject.ObservationEpoch <= self.Epoch {
			return errors.New("validator evidence audit is not a later observation")
		}
	default:
		return errors.New("validator evidence kind is unsupported")
	}
	return nil
}

// Checks the complete trusted domain and closed-window geometry without
// guessing historical identity, native finality, policy or batch membership.
func (self ValidatorEvidenceHeader) ValidateAt(expected ValidatorEvidenceDomain, window ValidatorEvidenceWindow) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	if err := self.Validate(); err != nil {
		return err
	}
	if self.Domain != expected {
		return errors.New("validator evidence differs from expected domain")
	}
	if self.Epoch != window.Epoch || self.Subject != window.Subject || window.StartBlock == 0 || window.EndBlock <= window.StartBlock || window.FinalizedBlock < window.EndBlock || self.BoundaryBlock > window.FinalizedBlock {
		return errors.New("validator evidence differs from the expected closed window")
	}
	if self.Kind == ValidatorEvidenceClosedCensus && self.BoundaryBlock != window.EndBlock-1 {
		return errors.New("validator evidence boundary is not the terminal block")
	}
	if self.Kind == ValidatorEvidenceDepositAudit && self.BoundaryBlock < window.EndBlock {
		return errors.New("validator evidence audit boundary precedes closure")
	}
	return nil
}

// A changed terminal boundary cannot create another slot. Audit coordinates
// are fixed-width and exclude observation outcomes and signed content.
func (self ValidatorEvidenceHeader) SubjectHash() ([32]byte, error) {
	if err := self.Validate(); err != nil {
		return [32]byte{}, err
	}
	if self.Kind == ValidatorEvidenceClosedCensus {
		return [32]byte{}, nil
	}
	data := append([]byte(ValidatorEvidenceAuditSubjectDomain), 0)
	data = binary.BigEndian.AppendUint64(data, self.Subject.ObservationEpoch)
	data = binary.BigEndian.AppendUint64(data, self.Subject.NativeEpoch)
	return sha256.Sum256(data), nil
}

// Owns its encoded bytes; signatures cover this tagged fixed-width message's
// SHA-256 digest, never an existing raw FINAL or native intent signature.
func (self ValidatorEvidenceHeader) Payload() ([]byte, error) {
	subjectHash, err := self.SubjectHash()
	if err != nil {
		return nil, err
	}
	data := make([]byte, 0, ValidatorEvidenceHeaderPayloadSize)
	data = append(data, ValidatorEvidenceHeaderDomain...)
	data = append(data, 0)
	data = append(data, self.Domain.payload()...)
	data = append(data, self.Hotkey[:]...)
	data = binary.BigEndian.AppendUint64(data, self.NoID)
	data = binary.BigEndian.AppendUint64(data, self.Epoch)
	data = append(data, self.Kind)
	data = append(data, subjectHash[:]...)
	data = append(data, self.VPK[:]...)
	data = binary.BigEndian.AppendUint64(data, self.BoundaryBlock)
	data = append(data, self.BoundaryHash[:]...)
	data = append(data, self.CensusHash[:]...)
	data = append(data, self.PayloadHash[:]...)
	return binary.BigEndian.AppendUint64(data, self.PayloadBytes), nil
}

// Returns the fixed 32-byte signing message supported by both precompiles.
func (self ValidatorEvidenceHeader) Digest() ([32]byte, error) {
	data, err := self.Payload()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

// Ownership excludes VPK, policy/activation revisions, content, boundaries,
// numeric validator UID and relayer. Trusted domain validation remains mandatory.
func (self ValidatorEvidenceHeader) SlotKey() ([32]byte, error) {
	subjectHash, err := self.SubjectHash()
	if err != nil {
		return [32]byte{}, err
	}
	data := append([]byte(ValidatorEvidenceSlotDomain), 0)
	data = binary.BigEndian.AppendUint64(data, self.Domain.ChainID)
	data = append(data, self.Domain.GenesisHash[:]...)
	data = binary.BigEndian.AppendUint16(data, self.Domain.Netuid)
	data = append(data, self.Domain.Coordinator[:]...)
	data = append(data, self.Domain.SettlementVault[:]...)
	data = append(data, self.Domain.DeploymentIDHash[:]...)
	data = append(data, self.Hotkey[:]...)
	data = binary.BigEndian.AppendUint64(data, self.NoID)
	data = binary.BigEndian.AppendUint64(data, self.Epoch)
	data = append(data, self.Kind)
	data = append(data, subjectHash[:]...)
	return sha256.Sum256(data), nil
}

// Derives and validates the full private key before signing. A caller-supplied
// public half alone cannot authenticate an inconsistent seed/public-key pair.
func (self ValidatorEvidenceHeader) SignVPK(privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("validator evidence private key does not match VPK")
	}
	derivedKey := ed25519.NewKeyFromSeed(privateKey[:ed25519.SeedSize])
	if subtle.ConstantTimeCompare(derivedKey, privateKey) != 1 || subtle.ConstantTimeCompare(derivedKey[ed25519.SeedSize:], self.VPK[:]) != 1 {
		return nil, errors.New("validator evidence private key does not match VPK")
	}
	digest, err := self.Digest()
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(derivedKey, digest[:]), nil
}

// Verifies possession only; callers accepting evidence use Verify with context.
func (self ValidatorEvidenceHeader) VerifyVPK(signature []byte) bool {
	digest, err := self.Digest()
	return err == nil && ed25519.Verify(ed25519.PublicKey(self.VPK[:]), digest[:], signature)
}

// Verifies possession only, without consulting current UID/permit state.
func (self ValidatorEvidenceHeader) VerifyHotkey(signature []byte) bool {
	digest, err := self.Digest()
	if err != nil || len(signature) != 64 {
		return false
	}
	key, err := (sr25519.Scheme{}).FromPublicKey(self.Hotkey[:])
	return err == nil && key.Verify(digest[:], signature)
}

// Checks trusted context before either signature; it never authenticates a
// caller-selected domain merely because that caller can sign its own key.
func (self ValidatorEvidenceHeader) Verify(expected ValidatorEvidenceDomain, window ValidatorEvidenceWindow, vpkSignature, hotkeySignature []byte) error {
	if err := self.ValidateAt(expected, window); err != nil {
		return err
	}
	if !self.VerifyVPK(vpkSignature) || !self.VerifyHotkey(hotkeySignature) {
		return errors.New("validator evidence signature is invalid")
	}
	return nil
}

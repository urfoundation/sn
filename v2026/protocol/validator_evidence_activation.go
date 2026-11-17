package protocol

// Earlier activation records bind a validator/operator's migration prefix and
// independently observed chain identities. Their unsigned digest is the later
// evidence domain's activation hash: no cut, census or later signature enters
// that digest. Consent does not establish historical eligibility or inclusion.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"

	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

const (
	ValidatorEvidenceActivationSignDomain  = "urnetwork/validator-evidence-activation/v1"
	ValidatorEvidenceActivationPayloadSize = len(ValidatorEvidenceActivationSignDomain) + 1 + 8 + 32 + 2 + 20 + 20 + 32 + 32 + 8 + 32 + 8 + 32 + 8 + 32 + 8 + 32 + 8 + 32
)

// Omits the activation hash deliberately: that hash names this earlier record
// and therefore cannot be an input to its own construction. Epoch zero is valid.
type ValidatorEvidenceActivationDomain struct {
	ChainID          uint64   `json:"chain_id"`
	GenesisHash      [32]byte `json:"genesis_hash"`
	Netuid           uint16   `json:"netuid"`
	Coordinator      [20]byte `json:"coordinator"`
	SettlementVault  [20]byte `json:"settlement_vault"`
	DeploymentIDHash [32]byte `json:"deployment_id_hash"`
	PolicyHash       [32]byte `json:"policy_hash"`
	Epoch            uint64   `json:"epoch"`
}

// Both snapshots precede publication and are verified against captured public
// history by the caller. FirstSequence/PriorRoot are the complete authenticated
// migration prefix, not a substitute for prior EMA or terminal-ID history.
// Numeric native UID and relayer are not durable identity; hotkey and the
// operator-scoped VPK are. Signature bytes are separate from this fixed record.
type ValidatorEvidenceActivation struct {
	Domain        ValidatorEvidenceActivationDomain `json:"domain"`
	Hotkey        [32]byte                          `json:"hotkey"`
	NoID          uint64                            `json:"no_id"`
	VPK           [32]byte                          `json:"vpk"`
	FirstSequence uint64                            `json:"first_sequence"`
	PriorRoot     [32]byte                          `json:"prior_root"`
	NativeBlock   uint64                            `json:"native_block"`
	NativeHash    [32]byte                          `json:"native_hash"`
	EVMBlock      uint64                            `json:"evm_block"`
	EVMHash       [32]byte                          `json:"evm_hash"`
}

// Checks complete fixed-width identity without inventing a placeholder anchor.
func (self ValidatorEvidenceActivationDomain) Validate() error {
	if self.ChainID == 0 || self.Netuid == 0 || self.GenesisHash == ([32]byte{}) ||
		self.Coordinator == ([20]byte{}) || self.SettlementVault == ([20]byte{}) ||
		self.Coordinator == self.SettlementVault || self.DeploymentIDHash == ([32]byte{}) || self.PolicyHash == ([32]byte{}) {
		return errors.New("validator evidence activation domain is incomplete")
	}
	return nil
}

// Requires real snapshot coordinates and a canonical empty/nonempty prefix.
// Clock ordering across Substrate and EVM is deliberately not inferred from
// their numeric heights; public history must authenticate that relationship.
func (self ValidatorEvidenceActivation) Validate() error {
	if err := self.Domain.Validate(); err != nil {
		return err
	}
	if self.Hotkey == ([32]byte{}) || self.VPK == ([32]byte{}) || self.NoID == 0 ||
		self.NativeBlock == 0 || self.NativeHash == ([32]byte{}) || self.EVMBlock == 0 || self.EVMHash == ([32]byte{}) {
		return errors.New("validator evidence activation identity or snapshots are incomplete")
	}
	if self.FirstSequence == 0 || self.FirstSequence == ^uint64(0) || (self.FirstSequence == 1) != (self.PriorRoot == ([32]byte{})) {
		return errors.New("validator evidence activation migration prefix is invalid")
	}
	return nil
}

// Encodes exact Solidity packed widths; returned bytes have no shared owner.
func (self ValidatorEvidenceActivation) Payload() ([]byte, error) {
	if err := self.Validate(); err != nil {
		return nil, err
	}
	data := make([]byte, 0, ValidatorEvidenceActivationPayloadSize)
	data = append(data, ValidatorEvidenceActivationSignDomain...)
	data = append(data, 0)
	data = binary.BigEndian.AppendUint64(data, self.Domain.ChainID)
	data = append(data, self.Domain.GenesisHash[:]...)
	data = binary.BigEndian.AppendUint16(data, self.Domain.Netuid)
	data = append(data, self.Domain.Coordinator[:]...)
	data = append(data, self.Domain.SettlementVault[:]...)
	data = append(data, self.Domain.DeploymentIDHash[:]...)
	data = append(data, self.Domain.PolicyHash[:]...)
	data = binary.BigEndian.AppendUint64(data, self.Domain.Epoch)
	data = append(data, self.Hotkey[:]...)
	data = binary.BigEndian.AppendUint64(data, self.NoID)
	data = append(data, self.VPK[:]...)
	data = binary.BigEndian.AppendUint64(data, self.FirstSequence)
	data = append(data, self.PriorRoot[:]...)
	data = binary.BigEndian.AppendUint64(data, self.NativeBlock)
	data = append(data, self.NativeHash[:]...)
	data = binary.BigEndian.AppendUint64(data, self.EVMBlock)
	return append(data, self.EVMHash[:]...), nil
}

// Admits one exact packed record, with no trailing bytes or variable-width
// representation. The returned fixed arrays never alias the input; decoding
// is not consent, historical authority, migration replay or inclusion proof.
func DecodeValidatorEvidenceActivationPayload(data []byte) (ValidatorEvidenceActivation, error) {
	if len(data) != ValidatorEvidenceActivationPayloadSize ||
		string(data[:len(ValidatorEvidenceActivationSignDomain)]) != ValidatorEvidenceActivationSignDomain ||
		data[len(ValidatorEvidenceActivationSignDomain)] != 0 {
		return ValidatorEvidenceActivation{}, errors.New("validator evidence activation payload framing is invalid")
	}
	offset := len(ValidatorEvidenceActivationSignDomain) + 1
	read := func(width int) []byte {
		field := data[offset : offset+width]
		offset += width
		return field
	}
	var activation ValidatorEvidenceActivation
	activation.Domain.ChainID = binary.BigEndian.Uint64(read(8))
	copy(activation.Domain.GenesisHash[:], read(32))
	activation.Domain.Netuid = binary.BigEndian.Uint16(read(2))
	copy(activation.Domain.Coordinator[:], read(20))
	copy(activation.Domain.SettlementVault[:], read(20))
	copy(activation.Domain.DeploymentIDHash[:], read(32))
	copy(activation.Domain.PolicyHash[:], read(32))
	activation.Domain.Epoch = binary.BigEndian.Uint64(read(8))
	copy(activation.Hotkey[:], read(32))
	activation.NoID = binary.BigEndian.Uint64(read(8))
	copy(activation.VPK[:], read(32))
	activation.FirstSequence = binary.BigEndian.Uint64(read(8))
	copy(activation.PriorRoot[:], read(32))
	activation.NativeBlock = binary.BigEndian.Uint64(read(8))
	copy(activation.NativeHash[:], read(32))
	activation.EVMBlock = binary.BigEndian.Uint64(read(8))
	copy(activation.EVMHash[:], read(32))
	if err := activation.Validate(); err != nil {
		return ValidatorEvidenceActivation{}, err
	}
	return activation, nil
}

// The same tagged unsigned digest is signed by both keys and referenced by
// later cuts. Randomized sr25519 signatures cannot change the activation hash.
func (self ValidatorEvidenceActivation) Digest() ([32]byte, error) {
	data, err := self.Payload()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

// Builds a later evidence domain from exact earlier bytes, not from a header
// that already references it. This conversion is not a chain inclusion proof.
func (self ValidatorEvidenceActivation) EvidenceDomain() (ValidatorEvidenceDomain, error) {
	hash, err := self.Digest()
	if err != nil {
		return ValidatorEvidenceDomain{}, err
	}
	return ValidatorEvidenceDomain{
		ChainID: self.Domain.ChainID, GenesisHash: self.Domain.GenesisHash,
		Netuid: self.Domain.Netuid, Coordinator: self.Domain.Coordinator,
		SettlementVault: self.Domain.SettlementVault, DeploymentIDHash: self.Domain.DeploymentIDHash,
		PolicyHash: self.Domain.PolicyHash, ActivationEpoch: self.Domain.Epoch, ActivationHash: hash,
	}, nil
}

// Requires a consistent seed/public pair belonging to this operator-scoped VPK.
func (self ValidatorEvidenceActivation) SignVPK(privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("validator evidence activation private key differs from VPK")
	}
	derived := ed25519.NewKeyFromSeed(privateKey[:ed25519.SeedSize])
	if subtle.ConstantTimeCompare(derived, privateKey) != 1 || subtle.ConstantTimeCompare(derived[ed25519.SeedSize:], self.VPK[:]) != 1 {
		return nil, errors.New("validator evidence activation private key differs from VPK")
	}
	digest, err := self.Digest()
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(derived, digest[:]), nil
}

// Possession alone does not authenticate a candidate-selected chain or prefix.
func (self ValidatorEvidenceActivation) VerifyVPK(signature []byte) bool {
	digest, err := self.Digest()
	return err == nil && ed25519.Verify(ed25519.PublicKey(self.VPK[:]), digest[:], signature)
}

// Uses the same real native signature scheme as later evidence headers.
func (self ValidatorEvidenceActivation) VerifyHotkey(signature []byte) bool {
	digest, err := self.Digest()
	if err != nil || len(signature) != 64 {
		return false
	}
	key, err := (sr25519.Scheme{}).FromPublicKey(self.Hotkey[:])
	return err == nil && key.Verify(digest[:], signature)
}

// Expected fields come from independently authenticated deployment, historical
// identity/snapshots and migration state, never by copying the candidate here.
// This checks consent and those exact inputs; inclusion/finality and complete
// migration replay remain required before a runtime may activate v2.
func (self ValidatorEvidenceActivation) Verify(expected ValidatorEvidenceActivation, vpkSignature, hotkeySignature []byte) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	if err := self.Validate(); err != nil {
		return err
	}
	if self != expected {
		return errors.New("validator evidence activation differs from expected authority")
	}
	if !self.VerifyVPK(vpkSignature) || !self.VerifyHotkey(hotkeySignature) {
		return errors.New("validator evidence activation signature is invalid")
	}
	return nil
}

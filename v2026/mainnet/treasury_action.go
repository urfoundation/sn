// Each treasury action authorizes one signatory's exact native extrinsic.
// Multisig operation identity, mortality and budgets never renew on recovery.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/v2026/validator"
	"golang.org/x/crypto/blake2b"
)

const treasuryActionSchema = "urnetwork-native-treasury-action-v1"
const treasuryConfigSchema = "urnetwork-native-treasury-execution-v1"
const treasuryStateFile = "native-treasury-action.json"

// Source and Wasm pins are external reviewed inputs, never RPC-selected trust.
type treasuryChainPolicy struct {
	NativeChain string `json:"native_chain"`
	GenesisHash string `json:"genesis_hash"`
	EvmChainId  uint64 `json:"evm_chain_id"`
	rootReceiptProfile
}

// The original approval's inclusion position selects one pending operation.
type treasuryTimepoint struct {
	Height uint32 `json:"height"`
	Index  uint32 `json:"index"`
}

// Only bounded registration, exact stake liquidation and keep-alive transfers
// are supported. No opaque utility batch or arbitrary native call is admitted.
type treasuryInnerCall struct {
	Kind        string `json:"kind"`
	Hotkey      string `json:"hotkey,omitempty"`
	Destination string `json:"destination,omitempty"`
	Amount      uint64 `json:"amount,omitempty"`
	LimitPrice  uint64 `json:"limit_price,omitempty"`
}

// Local references remain in the offline review only. The public economic
// TreasuryPolicy is produced from independently checked registered generations.
type treasuryAction struct {
	Schema                  string                    `json:"schema"`
	Policy                  treasuryChainPolicy       `json:"policy"`
	Descriptor              treasuryDescriptor        `json:"custody"`
	ObservationHash         string                    `json:"birth_observation_hash"`
	ReviewHash              string                    `json:"independent_review_hash"`
	TreasuryPolicyHash      string                    `json:"treasury_policy_hash,omitempty"`
	Treasury                *validator.TreasuryPolicy `json:"treasury_policy,omitempty"`
	StatePath               string                    `json:"state_path"`
	CustodyId               string                    `json:"custody_id"`
	Owner                   string                    `json:"signatory_account_id"`
	SubnetRegistrationBlock uint64                    `json:"subnet_registration_block"`
	SubnetGeneration        uint64                    `json:"subnet_generation"`
	Nonce                   uint32                    `json:"nonce"`
	BirthBlock              uint64                    `json:"birth_block"`
	BirthHash               string                    `json:"birth_hash"`
	Period                  uint64                    `json:"mortal_period"`
	FeeReserveRao           uint64                    `json:"fee_reserve_rao"`
	DepositLimitRao         uint64                    `json:"deposit_limit_rao"`
	Operation               string                    `json:"operation"`
	Timepoint               *treasuryTimepoint        `json:"timepoint"`
	Inner                   treasuryInnerCall         `json:"inner"`
	MaxRefTime              uint64                    `json:"max_ref_time"`
	MaxProofSize            uint64                    `json:"max_proof_size"`
	SignatureScheme         string                    `json:"signature_scheme"`
	MetadataDigest          string                    `json:"check_metadata_hash"`
	DerivationPath          string                    `json:"signer_derivation_path"`
	LedgerMetadataHash      string                    `json:"ledger_metadata_blake2b_256"`
	Call                    string                    `json:"call_scale"`
	Payload                 string                    `json:"payload_scale"`
	RequestHash             string                    `json:"request_hash"`
}

// One independent signature includes the exact route, action and cumulative
// transmission allowance. Hardware confirmation remains separate consent.
type treasuryConfig struct {
	Schema        string               `json:"schema"`
	Action        treasuryAction       `json:"action"`
	Route         ownedSubmissionRoute `json:"owned_route"`
	MaximumPosts  uint8                `json:"maximum_posts"`
	AuthorityHash string               `json:"independent_production_authority_hash"`
	Signature     string               `json:"approval_signature_ed25519"`
}

// Approval bytes use a new domain; no old owner/recycle signature is reusable.
func (self treasuryConfig) signingBytes() []byte {
	self.Signature = ""
	raw, _ := json.Marshal(self)
	return append([]byte(treasuryConfigSchema+"\x00"), raw...)
}

// A valid descriptor alone grants no executable native authority.
func (self treasuryConfig) validate(key string) error {
	if err := self.Action.validate(); err != nil {
		return err
	}
	if self.Schema != treasuryConfigSchema || !planSha256(self.AuthorityHash) || !rootCanonicalHash(key) || self.MaximumPosts == 0 || self.MaximumPosts > 8 || self.Route.ReadRetrySeconds < 60 || self.Route.ReadRetrySeconds > 900 || self.Route.SendTimeoutSeconds == 0 || self.Route.SendTimeoutSeconds > 60 {
		return errors.New("treasury execution lacks finite independently approved transport")
	}
	if err := self.Route.validate(); err != nil {
		return err
	}
	public, _ := hex.DecodeString(key[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil || !ed25519.Verify(public, self.signingBytes(), signature) {
		return errors.New("treasury execution approval is invalid")
	}
	return nil
}

// Independent chain pins select an exact artifact with reviewed owner semantics.
func (self treasuryChainPolicy) validate() error {
	if strings.TrimSpace(self.NativeChain) == "" || self.EvmChainId != mainnetEvmChainId || !nativeOwnerRuntimeProfile(self.rootReceiptProfile) {
		return errors.New("treasury requires reviewed native owner capabilities and exact mainnet runtime identity")
	}
	for _, hash := range []string{self.GenesisHash, self.RuntimeCodeHash, self.RuntimeMetadataHash} {
		if !rootCanonicalHash(hash) {
			return errors.New("treasury requires independent canonical genesis, code and metadata pins")
		}
	}
	return nil
}

// SCALE bytes are fixed by the source and rechecked against pinned metadata.
func (self treasuryAction) innerCall() ([]byte, error) {
	c := self.Inner
	hotkey := false
	for _, h := range self.Descriptor.RecipientHotkeys {
		if h.AccountId == c.Hotkey {
			hotkey = true
		}
	}
	if c.Kind == "remove_stake_limit" && self.Treasury != nil && self.Treasury.AutoStakeDestination != nil && c.Hotkey == "0x"+hex.EncodeToString(self.Treasury.AutoStakeDestination[:]) {
		hotkey = true
	}
	switch c.Kind {
	case "register_limit":
		if !hotkey || c.Amount != 0 || c.Destination != "" || c.LimitPrice == 0 {
			return nil, errors.New("treasury registration requires a declared recipient and explicit burn ceiling")
		}
		raw := []byte{7, 134, 25, 0}
		account, _ := hex.DecodeString(c.Hotkey[2:])
		raw = append(raw, account...)
		return binary.LittleEndian.AppendUint64(raw, c.LimitPrice), nil
	case "remove_stake_limit":
		if !hotkey || c.Amount == 0 || c.LimitPrice == 0 || c.Destination != "" || !rootCanonicalHash(self.TreasuryPolicyHash) {
			return nil, errors.New("treasury liquidation requires a declared recipient, exact amount, price floor and admitted policy hash")
		}
		raw := []byte{7, 89}
		account, _ := hex.DecodeString(c.Hotkey[2:])
		raw = append(raw, account...)
		raw = binary.LittleEndian.AppendUint16(raw, 25)
		raw = binary.LittleEndian.AppendUint64(raw, c.Amount)
		raw = binary.LittleEndian.AppendUint64(raw, c.LimitPrice)
		return append(raw, 0), nil // No partial liquidation is authorized.
	case "transfer_keep_alive":
		if !rootCanonicalHash(c.Destination) || c.Destination == self.Descriptor.Multisig.AccountId || c.Hotkey != "" || c.LimitPrice != 0 || c.Amount == 0 || !rootCanonicalHash(self.TreasuryPolicyHash) {
			return nil, errors.New("treasury transfer requires exact destination, amount and admitted policy hash")
		}
		raw := []byte{5, 3, 0}
		destination, _ := hex.DecodeString(c.Destination[2:])
		raw = append(raw, destination...)
		return append(raw, rootCompact(c.Amount)...), nil
	}
	return nil, errors.New("unsupported treasury inner call")
}

// The original signatory is excluded exactly once from the ordered full set.
func (self treasuryAction) encoding() ([]byte, []byte, error) {
	if err := errors.Join(self.Policy.validate(), self.Descriptor.validate()); err != nil {
		return nil, nil, err
	}
	if err := self.validateTreasuryPolicy(); err != nil {
		return nil, nil, err
	}
	if self.Schema != treasuryActionSchema || self.Descriptor.GenesisHash != self.Policy.GenesisHash || !planSha256(self.ObservationHash) || !planSha256(self.ReviewHash) || !planLabel(self.CustodyId) || !bootstrapRootAbsolutePath(self.StatePath) || filepath.Base(self.StatePath) != treasuryStateFile || !rootCanonicalHash(self.Owner) || !rootCanonicalHash(self.BirthHash) || self.BirthBlock < self.SubnetRegistrationBlock || self.BirthBlock > math.MaxUint32 || self.Nonce == math.MaxUint32 || self.FeeReserveRao == 0 || self.SignatureScheme != "ed25519" || !rootCanonicalHash(self.MetadataDigest) || !rootCanonicalHash(self.LedgerMetadataHash) {
		return nil, nil, errors.New("treasury action has invalid scope, custody, nonce, budget or Ledger profile")
	}
	if _, err := ownerLedgerDerivationPath(self.DerivationPath); err != nil {
		return nil, nil, err
	}
	inner, err := self.innerCall()
	if err != nil {
		return nil, nil, err
	}
	index := byte(0)
	switch self.Operation {
	case "as_multi":
		index = 1
	case "approve_as_multi":
		index = 2
	case "cancel_as_multi":
		index = 3
	default:
		return nil, nil, errors.New("treasury multisig operation is unsupported")
	}
	if self.Operation == "cancel_as_multi" && self.Timepoint == nil || self.Operation != "cancel_as_multi" && (self.MaxRefTime == 0 || self.MaxProofSize == 0) || self.Timepoint == nil && self.DepositLimitRao == 0 {
		return nil, nil, errors.New("treasury operation requires original timepoint or bounded initial deposit and dispatch weight")
	}
	if self.Timepoint != nil && (self.Timepoint.Height == 0 || uint64(self.Timepoint.Height) > self.BirthBlock) {
		return nil, nil, errors.New("treasury original timepoint is invalid")
	}
	others := make([]byte, 0, 32*(len(self.Descriptor.Multisig.Signatories)-1))
	found := false
	for _, signer := range self.Descriptor.Multisig.Signatories {
		if signer.AccountId == self.Owner {
			found = true
			continue
		}
		raw, _ := hex.DecodeString(signer.AccountId[2:])
		others = append(others, raw...)
	}
	if !found {
		return nil, nil, errors.New("treasury outer signer is not an original multisig signatory")
	}
	call := binary.LittleEndian.AppendUint16([]byte{13, index}, self.Descriptor.Multisig.Threshold)
	call = append(call, rootCompact(uint64(len(others)/32))...)
	call = append(call, others...)
	if self.Operation != "cancel_as_multi" {
		if self.Timepoint == nil {
			call = append(call, 0)
		} else {
			call = append(call, 1)
		}
	}
	if self.Timepoint != nil {
		call = binary.LittleEndian.AppendUint32(call, self.Timepoint.Height)
		call = binary.LittleEndian.AppendUint32(call, self.Timepoint.Index)
	}
	if self.Operation == "as_multi" {
		call = append(call, inner...)
	} else {
		hash := blake2b.Sum256(inner)
		call = append(call, hash[:]...)
	}
	if self.Operation != "cancel_as_multi" {
		call = append(call, rootCompact(self.MaxRefTime)...)
		call = append(call, rootCompact(self.MaxProofSize)...)
	}
	era, err := rootMortalEra(self.BirthBlock, self.Period)
	if err != nil {
		return nil, nil, err
	}
	payload := append(append([]byte(nil), call...), era...)
	payload = append(payload, rootCompact(uint64(self.Nonce))...)
	payload = append(payload, 0, 1)
	payload = binary.LittleEndian.AppendUint32(payload, self.Policy.RuntimeVersion.SpecVersion)
	payload = binary.LittleEndian.AppendUint32(payload, self.Policy.RuntimeVersion.TransactionVersion)
	for _, hash := range []string{self.Policy.GenesisHash, self.BirthHash} {
		raw, _ := hex.DecodeString(hash[2:])
		payload = append(payload, raw...)
	}
	digest, _ := hex.DecodeString(self.MetadataDigest[2:])
	payload = append(payload, 1)
	payload = append(payload, digest...)
	return call, payload, nil
}

// The checksum binds all public planning choices; approval authenticates it.
func (self treasuryAction) validate() error {
	call, payload, err := self.encoding()
	if err != nil {
		return err
	}
	claimed := self.RequestHash
	self.RequestHash = ""
	if self.Call != "0x"+hex.EncodeToString(call) || self.Payload != "0x"+hex.EncodeToString(payload) || claimed != rootObjectHash(self) {
		return errors.New("treasury action bytes or content hash changed")
	}
	return nil
}

// Substrate signs Blake2-256 only when the complete payload exceeds 256 bytes.
func (self treasuryAction) signingBytes() []byte {
	raw, _ := hex.DecodeString(self.Payload[2:])
	if len(raw) > 256 {
		hash := blake2b.Sum256(raw)
		return hash[:]
	}
	return raw
}

// Signature validation authenticates the complete immutable extrinsic.
func (self treasuryAction) signed(signature []byte) ([]byte, error) {
	if err := self.validate(); err != nil {
		return nil, err
	}
	account, _ := hex.DecodeString(self.Owner[2:])
	if len(signature) != 64 || !ed25519.Verify(account, self.signingBytes(), signature) {
		return nil, errors.New("treasury signature does not match the exact Ledger signatory and payload")
	}
	call, payload, _ := self.encoding()
	extraLength := 2 + len(rootCompact(uint64(self.Nonce))) + 2
	body := append([]byte{0x84, 0}, account...)
	body = append(body, 0)
	body = append(body, signature...)
	body = append(body, payload[len(call):len(call)+extraLength]...)
	body = append(body, call...)
	return append(rootCompact(uint64(len(body))), body...), nil
}

// No re-encoded substitute or signature replacement is accepted on recovery.
func treasurySignedAction(action treasuryAction, raw []byte) error {
	reader := rootScaleReader{data: raw}
	n, err := reader.compact()
	if err != nil || n != uint64(len(raw)-reader.offset) {
		return errors.New("treasury extrinsic length changed")
	}
	body := raw[reader.offset:]
	if len(body) < 99 || body[0] != 0x84 || body[1] != 0 || body[34] != 0 {
		return errors.New("treasury extrinsic envelope changed")
	}
	expected, err := action.signed(body[35:99])
	if err != nil || !bytes.Equal(expected, raw) {
		return errors.Join(errors.New("treasury signed bytes differ from original action"), err)
	}
	return nil
}

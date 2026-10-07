// Root registration owns one distinct operator nonce and mortal native call.
// Its explicit exposure approval cannot inherit reserve, subnet-owner or old root authority.
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
	"slices"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"

	"github.com/vedhavyas/go-subkey/v2/sr25519"
	"golang.org/x/crypto/blake2b"
)

const rootRegisterActionSchema = "urnetwork-mainnet-root-register-action-v1"
const rootRegisterConfigSchema = "urnetwork-mainnet-root-register-execution-v1"
const rootRegisterStateFile = "root-register-action.json"

const rootRegisterPolicySchema = "urnetwork-mainnet-root-register-policy-v1"
const rootRegisterBalanceExposure = "whole-reducible-operator-balance-at-inclusion"
const rootRegisterPortableHardware = "portable-root-hardware-v1"
const rootRegisterLedgerHardware = "ledger-rfc78-v1"

// Approved role separation and complete runtime identity precede any RPC read.
// Artifact hashes are expectations, never source-to-Wasm or signing authority.
type rootRegisterPolicy struct {
	Schema              string                      `json:"schema"`
	NativeChain         string                      `json:"native_chain"`
	GenesisHash         string                      `json:"genesis_hash"`
	EvmChainId          uint64                      `json:"evm_chain_id"`
	RuntimeSourceCommit string                      `json:"runtime_source_commit"`
	RuntimeVersion      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash     string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash string                      `json:"runtime_metadata_hash"`
	Hotkey              string                      `json:"root_hotkey_account_id"`
	Operator            string                      `json:"operator_coldkey_account_id"`
	Reserve             string                      `json:"receive_only_reserve_account_id"`
	SubnetOwner         string                      `json:"subnet_owner_account_id"`
}

// The reviewed473 implementation is the only registration source admitted here.
func (self rootRegisterPolicy) validate() error {
	if self.Schema != rootRegisterPolicySchema || strings.TrimSpace(self.NativeChain) == "" || self.EvmChainId != mainnetEvmChainId || self.RuntimeSourceCommit != crv4.NativeOwnerSource473 || self.RuntimeVersion.SpecName != "node-subtensor" || self.RuntimeVersion.SpecVersion != 473 || self.RuntimeVersion.TransactionVersion != 1 || self.RuntimeVersion.StateVersion != 1 {
		return errors.New("root registration requires the independently reviewed473 source and complete mainnet runtime tuple")
	}
	for _, value := range []string{self.GenesisHash, self.RuntimeCodeHash, self.RuntimeMetadataHash, self.Hotkey, self.Operator, self.Reserve, self.SubnetOwner} {
		if !rootCanonicalHash(value) {
			return errors.New("root registration requires canonical nonzero runtime and account pins")
		}
	}
	if self.Operator == self.Reserve || self.Operator == self.SubnetOwner || self.Hotkey == self.Operator || self.Hotkey == self.Reserve || self.Hotkey == self.SubnetOwner {
		return errors.New("root operator and hotkey must remain distinct from the receive-only reserve and subnet owner")
	}
	return nil
}

// Only explicit expected identity is provided to the owned read transport.
func (self rootRegisterPolicy) identity() identityExpectation {
	return identityExpectation{NativeChain: self.NativeChain, GenesisHash: self.GenesisHash, EvmChainId: self.EvmChainId}
}

// Exact artifact pins are retained in both preparation and original replay.
func (self rootRegisterPolicy) runtime() rootReceiptProfile {
	return rootReceiptProfile{RuntimeSourceCommit: self.RuntimeSourceCommit, RuntimeVersion: self.RuntimeVersion, RuntimeCodeHash: self.RuntimeCodeHash, RuntimeMetadataHash: self.RuntimeMetadataHash}
}

// These statements are signed separately from the native payload. They do not
// purport to add a cap or predicate that root_register does not implement.
func rootRegisterExposureAcknowledgements() []string {
	return []string{
		"I authorize only this distinct operator coldkey; the receive-only reserve and subnet-owner accounts are excluded from signing and spending.",
		"Native root_register has no maximum-burn argument: its current burn may consume the operator's whole reducible balance at inclusion, including funds received after this preview. The quoted burn threshold is preflight only.",
		"The fee reserve is preflight only, not an on-chain maximum; successful or failed dispatch can charge a fee above it.",
		"Admission, immunity, competing stake, registration limits, burn and runtime state can change before inclusion. Failed dispatch may still create account associations and charge fees; no automatic retry with another nonce is authorized.",
		"Success may evict the lowest eligible nonimmune root seat, set the default delegate take to11796, and automatically assign subnet-owner child keys unless opted out. I accept those exact source-defined side effects.",
		"The original operator signature and nonce require exclusive external custody. Local files and RPC observations alone do not prove global custody, finality or production source-to-Wasm approval.",
		"A root registration receipt does not activate a passive root service, UR validators, or any contract action; those retain their separate approvals.",
	}
}

// One root_register call has no numeric burn cap. The operator separately
// approves whole reducible-balance exposure at inclusion, including new funds.
// Quote and fee thresholds are preflight observations only.
type rootRegisterAction struct {
	Schema                   string             `json:"schema"`
	Policy                   rootRegisterPolicy `json:"policy"`
	ObservationHash          string             `json:"birth_observation_hash"`
	ReviewHash               string             `json:"independent_review_hash"`
	CustodyId                string             `json:"custody_id"`
	StatePath                string             `json:"state_path"`
	Nonce                    uint32             `json:"nonce"`
	BirthBlock               uint64             `json:"birth_block"`
	BirthHash                string             `json:"birth_hash"`
	Period                   uint64             `json:"mortal_period"`
	FeeReserveRao            uint64             `json:"fee_reserve_rao"`
	QuotedBurnLimitRao       uint64             `json:"quoted_burn_preflight_limit_rao"`
	QuotedBurnRao            uint64             `json:"observed_burn_rao"`
	ObservedFreeRao          uint64             `json:"observed_free_balance_rao"`
	ObservedReducibleRao     uint64             `json:"observed_conservative_reducible_balance_rao"`
	Exposure                 string             `json:"operator_balance_exposure"`
	StrictBurnCapRao         *uint64            `json:"strict_burn_cap_rao,omitempty"`
	ExposureAcknowledgements []string           `json:"explicit_operator_exposure_acknowledgements"`
	SigningProfile           string             `json:"operator_signing_profile"`
	HardwareCustodyHash      string             `json:"independent_operator_hardware_custody_hash"`
	SignatureScheme          string             `json:"signature_scheme"`
	MetadataDigest           string             `json:"check_metadata_hash,omitempty"`
	DerivationPath           string             `json:"signer_derivation_path,omitempty"`
	LedgerMetadataHash       string             `json:"ledger_metadata_blake2b_256,omitempty"`
	CallIndex                [2]byte            `json:"call_index"`
	Call                     string             `json:"call_scale"`
	Payload                  string             `json:"payload_scale"`
	RequestHash              string             `json:"request_hash"`
}

// The independently supplied approval key authorizes exactly one action/route.
// No signed document supplies live custody or source-to-Wasm enforcement.
type rootRegisterConfig struct {
	Schema    string               `json:"schema"`
	Action    rootRegisterAction   `json:"action"`
	Route     ownedSubmissionRoute `json:"owned_route"`
	Signature string               `json:"approval_signature_ed25519"`
}

// Separate domain bytes prevent reuse of any preexisting owner approval.
func (self rootRegisterConfig) signingBytes() []byte {
	self.Signature = ""
	raw, _ := json.Marshal(self)
	return append([]byte(self.Schema+"\x00"), raw...)
}

// Every reopen authenticates the independently pinned approval key.
func (self rootRegisterConfig) validate(key string) error {
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > 128*1024 {
		return errors.New("root registration execution config exceeds its physical custody bound")
	}
	if err := self.Action.validate(); err != nil {
		return err
	}
	if self.Schema != rootRegisterConfigSchema || !rootCanonicalHash(key) || self.Route.ReadRetrySeconds < 60 || self.Route.ReadRetrySeconds > 900 || self.Route.SendTimeoutSeconds == 0 || self.Route.SendTimeoutSeconds > 60 {
		return errors.New("root registration execution requires independent approval and bounded owned transport")
	}
	if err := self.Route.validate(); err != nil {
		return err
	}
	public, _ := hex.DecodeString(key[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil || !ed25519.Verify(public, self.signingBytes(), signature) {
		return errors.New("root registration action/route approval is invalid")
	}
	return nil
}

// Exact source and artifact pins travel with the original owner/admin action.
func (self rootRegisterAction) runtime() rootReceiptProfile {
	p := self.Policy
	return rootReceiptProfile{RuntimeSourceCommit: p.RuntimeSourceCommit, RuntimeVersion: p.RuntimeVersion, RuntimeCodeHash: p.RuntimeCodeHash, RuntimeMetadataHash: p.RuntimeMetadataHash}
}

// Metadata authenticates the entire native envelope and exact enum argument.
func prepareRootRegisterAction(action rootRegisterAction, metadataHex string) (rootRegisterAction, error) {
	if len(metadataHex) > 2+2*maxMetadataRpcReplyBytes {
		return rootRegisterAction{}, errors.New("root registration metadata exceeds bound")
	}
	metadata, digest, err := nativePinnedMetadata(metadataHex, action.Policy.RuntimeMetadataHash)
	if err != nil || digest != action.Policy.RuntimeMetadataHash {
		return rootRegisterAction{}, errors.Join(errors.New("root registration metadata differs from independent pin"), err)
	}
	variant, index := "Sr25519", uint8(1)
	if action.SignatureScheme == "ed25519" {
		variant, index = "Ed25519", 0
	}
	if err := nativeSigningProfileForSignature(metadata, variant, index); err != nil {
		return rootRegisterAction{}, err
	}
	call, err := rootRegisterCall(metadata)
	if err != nil {
		return rootRegisterAction{}, err
	}
	action.CallIndex = call
	callRaw, payload, err := action.encoding()
	if err != nil {
		return rootRegisterAction{}, err
	}
	action.Call, action.Payload = "0x"+hex.EncodeToString(callRaw), "0x"+hex.EncodeToString(payload)
	action.RequestHash = ""
	action.RequestHash = rootObjectHash(action)
	return action, nil
}

// Reconstruct exactly root_register(hotkey), zero tip and the approved era.
// Neither the 34-byte call nor its extensions encode a maximum burn or fee.
func (self rootRegisterAction) encoding() ([]byte, []byte, error) {
	if err := self.Policy.validate(); err != nil {
		return nil, nil, err
	}
	if self.Schema != rootRegisterActionSchema || !nativeOwnerRuntimeProfile(self.runtime()) ||
		!planSha256(self.ObservationHash) || !planSha256(self.ReviewHash) || !planLabel(self.CustodyId) || !bootstrapRootAbsolutePath(self.StatePath) || filepath.Base(self.StatePath) != rootRegisterStateFile ||
		!rootCanonicalHash(self.Policy.Operator) || !rootCanonicalHash(self.BirthHash) || self.BirthBlock == 0 || self.Nonce == math.MaxUint32 || self.FeeReserveRao == 0 || self.CallIndex != [2]byte{7, 62} || self.Exposure != rootRegisterBalanceExposure || self.StrictBurnCapRao != nil || !planSha256(self.HardwareCustodyHash) || !slices.Equal(self.ExposureAcknowledgements, rootRegisterExposureAcknowledgements()) {
		return nil, nil, errors.New("root registration action has invalid reviewed owner scope, custody, era or allowance")
	}
	if self.ObservedReducibleRao > self.ObservedFreeRao || self.QuotedBurnRao > self.QuotedBurnLimitRao || self.QuotedBurnRao > self.ObservedReducibleRao || self.FeeReserveRao > self.ObservedReducibleRao-self.QuotedBurnRao {
		return nil, nil, errors.New("root registration signed birth quote exceeds its preflight allowance")
	}
	for _, hash := range []string{self.Policy.GenesisHash, self.Policy.RuntimeCodeHash, self.Policy.RuntimeMetadataHash} {
		if !rootCanonicalHash(hash) {
			return nil, nil, errors.New("root registration policy hashes must be canonical")
		}
	}
	mode := byte(0)
	switch self.SigningProfile {
	case rootRegisterPortableHardware:
		if self.SignatureScheme != "sr25519" && self.SignatureScheme != "ed25519" {
			return nil, nil, errors.New("root registration portable hardware requires an explicit native signature scheme")
		}
		if self.MetadataDigest != "" || self.LedgerMetadataHash != "" || self.DerivationPath != "" {
			return nil, nil, errors.New("root registration portable hardware cannot inherit Ledger metadata or derivation")
		}
	case rootRegisterLedgerHardware:
		if self.SignatureScheme != "ed25519" || !rootCanonicalHash(self.MetadataDigest) || !rootCanonicalHash(self.LedgerMetadataHash) {
			return nil, nil, errors.New("root registration Ledger profile requires independently approved RFC78 digest and metadata15 hash")
		}
		if _, err := ownerLedgerDerivationPath(self.DerivationPath); err != nil {
			return nil, nil, err
		}
		mode = 1
	default:
		return nil, nil, errors.New("root registration requires an independently selected operator hardware signing profile")
	}
	era, err := rootMortalEra(self.BirthBlock, self.Period)
	if err != nil {
		return nil, nil, err
	}
	hotkey, _ := hex.DecodeString(self.Policy.Hotkey[2:])
	call := append(append([]byte(nil), self.CallIndex[:]...), hotkey...)
	payload := append(append([]byte(nil), call...), era...)
	payload = append(payload, rootCompact(uint64(self.Nonce))...)
	payload = append(payload, 0, mode)
	payload = binary.LittleEndian.AppendUint32(payload, self.Policy.RuntimeVersion.SpecVersion)
	payload = binary.LittleEndian.AppendUint32(payload, self.Policy.RuntimeVersion.TransactionVersion)
	for _, hash := range []string{self.Policy.GenesisHash, self.BirthHash} {
		data, _ := hex.DecodeString(hash[2:])
		payload = append(payload, data...)
	}
	payload = append(payload, mode)
	if mode == 1 {
		data, _ := hex.DecodeString(self.MetadataDigest[2:])
		payload = append(payload, data...)
	}
	return call, payload, nil
}

// Historical request bytes cannot be rebuilt with a new nonce, era or runtime.
func (self rootRegisterAction) validate() error {
	call, payload, err := self.encoding()
	if err != nil {
		return err
	}
	claimed := self.RequestHash
	self.RequestHash = ""
	if self.Call != "0x"+hex.EncodeToString(call) || self.Payload != "0x"+hex.EncodeToString(payload) || claimed != rootObjectHash(self) {
		return errors.New("root registration action encoding or seal changed")
	}
	return nil
}

// Verify the exact owner's public signature before constructing retained bytes.
func (self rootRegisterAction) signed(signature []byte) ([]byte, error) {
	if err := self.validate(); err != nil {
		return nil, err
	}
	account, _ := hex.DecodeString(self.Policy.Operator[2:])
	payload, _ := hex.DecodeString(self.Payload[2:])
	if len(payload) > 256 {
		digest := blake2b.Sum256(payload)
		payload = digest[:]
	}
	variant, mode, valid := byte(1), byte(0), false
	if self.SignatureScheme == "ed25519" {
		variant = 0
		valid = len(signature) == 64 && ed25519.Verify(account, payload, signature)
	} else {
		public, err := (sr25519.Scheme{}).FromPublicKey(account)
		if err != nil {
			return nil, err
		}
		valid = len(signature) == 64 && public.Verify(payload, signature)
	}
	if !valid {
		return nil, errors.New("root registration signature belongs to another owner or action")
	}
	if self.SigningProfile == rootRegisterLedgerHardware {
		mode = 1
	}
	body := append([]byte{0x84, 0}, account...)
	body = append(body, variant)
	body = append(body, signature...)
	era, _ := rootMortalEra(self.BirthBlock, self.Period)
	body = append(body, era...)
	body = append(body, rootCompact(uint64(self.Nonce))...)
	body = append(body, 0, mode)
	call, _ := hex.DecodeString(self.Call[2:])
	body = append(body, call...)
	return append(rootCompact(uint64(len(body))), body...), nil
}

// Compare the complete extrinsic, including signature variant and signed extras.
func rootRegisterSignedAction(action rootRegisterAction, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	reader := rootScaleReader{data: raw}
	length, err := reader.compact()
	if err != nil || length != uint64(len(raw)-reader.offset) {
		return errors.New("root registration extrinsic length changed")
	}
	body := raw[reader.offset:]
	variant := byte(1)
	if action.SignatureScheme == "ed25519" {
		variant = 0
	}
	if len(body) < 99 || body[0] != 0x84 || body[1] != 0 || body[34] != variant {
		return errors.New("root registration extrinsic envelope changed")
	}
	expected, err := action.signed(body[35:99])
	if err != nil || !bytes.Equal(expected, raw) {
		return errors.Join(errors.New("root registration extrinsic differs from original action"), err)
	}
	return nil
}

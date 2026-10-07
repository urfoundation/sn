// Owner trim is a separate, explicitly approved native action. Original v3
// preparation approvals are retained and cannot be promoted into trim authority.
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

	"github.com/vedhavyas/go-subkey/v2/sr25519"
	"golang.org/x/crypto/blake2b"
)

const ownerTrimActionSchema = "urnetwork-mainnet-owner-trim-action-v1"
const ownerTrimExecutionSchema = "urnetwork-mainnet-owner-trim-execution-v1"
const ownerTrimLedgerActionSchema = "urnetwork-mainnet-owner-trim-action-v2"
const ownerTrimLedgerExecutionSchema = "urnetwork-mainnet-owner-trim-execution-v2"
const ownerTrimBestEffortActionSchema = "urnetwork-mainnet-owner-trim-best-effort-action-v1"
const ownerTrimBestEffortExecutionSchema = "urnetwork-mainnet-owner-trim-best-effort-execution-v1"
const ownerTrimBestEffortSelection = "reviewed-current-selection-with-explicit-inclusion-risks"
const ownerTrimStateFile = "owner-trim-action.json"

// One action owns one coldkey nonce, era and exact call for its whole lifetime.
// The local fee reserve is not a native maximum-fee field.
type ownerTrimAction struct {
	Schema                  string             `json:"schema"`
	PreparationHash         string             `json:"preparation_hash"`
	PreparationStateHash    string             `json:"preparation_state_hash"`
	PolicyHash              string             `json:"policy_file_sha256"`
	ReviewHash              string             `json:"retained_trim_review_hash"`
	Network                 planNetwork        `json:"network"`
	Runtime                 rootReceiptProfile `json:"runtime"`
	CustodyId               string             `json:"custody_id"`
	StatePath               string             `json:"state_path"`
	Coldkey                 string             `json:"coldkey_account_id"`
	Netuid                  uint16             `json:"netuid"`
	SubnetRegistrationBlock uint64             `json:"subnet_registration_block"`
	SubnetGeneration        uint64             `json:"subnet_generation"`
	MaximumUids             uint16             `json:"maximum_uids"`
	SelectionRule           string             `json:"selection_rule"`
	Nonce                   uint32             `json:"nonce"`
	BirthBlock              uint64             `json:"birth_block"`
	BirthHash               string             `json:"birth_hash"`
	Period                  uint64             `json:"mortal_period"`
	FeeReserveRao           uint64             `json:"fee_reserve_rao"`
	MaxBroadcasts           uint8              `json:"max_broadcasts"`
	CallIndex               [2]byte            `json:"call_index"`
	Call                    string             `json:"call_scale"`
	Payload                 string             `json:"payload_scale"`
	RequestHash             string             `json:"request_hash"`
	SignatureScheme         string             `json:"signature_scheme,omitempty"`
	MetadataDigest          string             `json:"check_metadata_hash,omitempty"`
	DerivationPath          string             `json:"signer_derivation_path,omitempty"`
	LedgerMetadataHash      string             `json:"ledger_metadata_blake2b_256,omitempty"`
}

// A fresh domain-specific approval covers the exact native action and owned
// route. The verifier receives its public key independently of this document.
type ownerTrimExecutionConfig struct {
	Schema    string               `json:"schema"`
	Action    ownerTrimAction      `json:"action"`
	Route     ownedSubmissionRoute `json:"owned_route"`
	Signature string               `json:"approval_signature_ed25519"`
}

// Signing this domain does not establish current window enforcement or custody.
func (self ownerTrimExecutionConfig) signingBytes() []byte {
	self.Signature = ""
	raw, _ := json.Marshal(self)
	return append([]byte(self.Schema+"\x00"), raw...)
}

// V1 keeps its original implicit sr25519/disabled-metadata wire contract.
// V2 is a fresh approval domain for an Ed25519 account and RFC78 digest.
func (self ownerTrimExecutionConfig) validSchemas() bool {
	return self.Schema == ownerTrimExecutionSchema && self.Action.Schema == ownerTrimActionSchema ||
		self.Schema == ownerTrimLedgerExecutionSchema && self.Action.Schema == ownerTrimLedgerActionSchema ||
		self.Schema == ownerTrimBestEffortExecutionSchema && self.Action.Schema == ownerTrimBestEffortActionSchema
}

// A fresh best-effort approval retains the Ledger envelope without inheriting
// the strict v2 execution guarantee or adopting a previously claimed action.
func (self ownerTrimAction) ledgerSigning() bool {
	return self.Schema == ownerTrimLedgerActionSchema || self.Schema == ownerTrimBestEffortActionSchema
}

// Every reopen verifies the independent approval instead of trusting a journal
// checksum or treating the earlier conditional qualifier as authorization.
func (self ownerTrimExecutionConfig) validate(approvalKey string) error {
	if err := self.Action.validate(); err != nil {
		return err
	}
	if !self.validSchemas() || !rootCanonicalHash(approvalKey) ||
		self.Route.ReadRetrySeconds < 60 || self.Route.ReadRetrySeconds > 900 || self.Route.SendTimeoutSeconds == 0 || self.Route.SendTimeoutSeconds > 60 {
		return errors.New("owner trim requires independent action approval and bounded transport")
	}
	if err := self.Route.validate(); err != nil {
		return err
	}
	key, _ := hex.DecodeString(approvalKey[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil || !ed25519.Verify(key, self.signingBytes(), signature) {
		return errors.New("owner trim independent action/route approval is invalid")
	}
	return nil
}

// Build only from exact pinned metadata. No private key or current-ready flag
// is accepted. Native signing still requires the execution owner's authority.
func prepareOwnerTrimAction(action ownerTrimAction, metadataHex string) (ownerTrimAction, error) {
	if len(metadataHex) > 2+2*maxMetadataRpcReplyBytes {
		return ownerTrimAction{}, errors.New("owner trim metadata exceeds bound")
	}
	metadata, digest, err := nativePinnedMetadata(metadataHex, action.Runtime.RuntimeMetadataHash)
	if err != nil || digest != action.Runtime.RuntimeMetadataHash {
		return ownerTrimAction{}, errors.Join(errors.New("owner trim metadata differs from approved artifact"), err)
	}
	variant, index := "Sr25519", uint8(1)
	if action.ledgerSigning() {
		variant, index = "Ed25519", 0
	}
	if err := nativeSigningProfileForSignature(metadata, variant, index); err != nil {
		return ownerTrimAction{}, err
	}
	call, err := subnetOwnerTrimCall(metadata)
	if err != nil {
		return ownerTrimAction{}, err
	}
	action.CallIndex = [2]byte{call.PalletIndex, call.CallIndex}
	encoded, payload, err := action.encoding()
	if err != nil {
		return ownerTrimAction{}, err
	}
	action.Call, action.Payload = "0x"+hex.EncodeToString(encoded), "0x"+hex.EncodeToString(payload)
	action.RequestHash = ""
	action.RequestHash = rootObjectHash(action)
	return action, nil
}

// Reconstruct the six-byte direct owner call and reviewed mortal envelope.
func (self ownerTrimAction) encoding() ([]byte, []byte, error) {
	if self.Schema == ownerTrimActionSchema {
		if self.SignatureScheme != "" || self.MetadataDigest != "" || self.DerivationPath != "" || self.LedgerMetadataHash != "" {
			return nil, nil, errors.New("owner trim v1 signature and metadata mode cannot change")
		}
	} else if !self.ledgerSigning() || self.SignatureScheme != "ed25519" || !rootCanonicalHash(self.MetadataDigest) || !rootCanonicalHash(self.LedgerMetadataHash) {
		return nil, nil, errors.New("owner trim v2 requires explicit Ed25519 and an approved RFC78 metadata digest")
	}
	if self.ledgerSigning() {
		if _, err := ownerLedgerDerivationPath(self.DerivationPath); err != nil {
			return nil, nil, err
		}
	}
	selection := ownerTrimSubsetRule
	if self.Schema == ownerTrimBestEffortActionSchema {
		selection = ownerTrimBestEffortSelection
	}
	if self.Netuid != 25 || self.Network.NativeChain == "" || self.Network.EvmChainId != mainnetEvmChainId ||
		!mainnetRuntimeCodecSource(self.Runtime.RuntimeSourceCommit) || self.Runtime.RuntimeVersion.SpecName == "" || self.Runtime.RuntimeVersion.SpecVersion == 0 ||
		self.Runtime.RuntimeVersion.TransactionVersion == 0 || self.Runtime.RuntimeVersion.StateVersion != 1 ||
		self.SelectionRule != selection || !planLabel(self.CustodyId) || self.MaximumUids == 0 ||
		self.Nonce == math.MaxUint32 || self.BirthBlock < self.SubnetRegistrationBlock || self.FeeReserveRao == 0 || self.MaxBroadcasts == 0 || self.MaxBroadcasts > 8 ||
		!bootstrapRootAbsolutePath(self.StatePath) || filepath.Base(self.StatePath) != ownerTrimStateFile {
		return nil, nil, errors.New("owner trim action scope, call, custody or allowance is invalid")
	}
	for _, hash := range []string{self.PreparationHash, self.PreparationStateHash, self.PolicyHash, self.ReviewHash} {
		if !planSha256(hash) {
			return nil, nil, errors.New("owner trim action lacks original preparation and review seals")
		}
	}
	for _, hash := range []string{self.Network.GenesisHash, self.Runtime.RuntimeCodeHash, self.Runtime.RuntimeMetadataHash, self.Coldkey, self.BirthHash} {
		if !rootCanonicalHash(hash) {
			return nil, nil, errors.New("owner trim action has an invalid domain, owner or anchor")
		}
	}
	era, err := rootMortalEra(self.BirthBlock, self.Period)
	if err != nil {
		return nil, nil, err
	}
	call := binary.LittleEndian.AppendUint16(append([]byte(nil), self.CallIndex[:]...), self.Netuid)
	call = binary.LittleEndian.AppendUint16(call, self.MaximumUids)
	payload := append(append([]byte(nil), call...), era...)
	payload = append(payload, rootCompact(uint64(self.Nonce))...)
	mode := byte(0)
	if self.ledgerSigning() {
		mode = 1
	}
	payload = append(payload, 0, mode)
	payload = binary.LittleEndian.AppendUint32(payload, self.Runtime.RuntimeVersion.SpecVersion)
	payload = binary.LittleEndian.AppendUint32(payload, self.Runtime.RuntimeVersion.TransactionVersion)
	genesis, _ := hex.DecodeString(self.Network.GenesisHash[2:])
	birth, _ := hex.DecodeString(self.BirthHash[2:])
	payload = append(payload, genesis...)
	payload = append(payload, birth...)
	payload = append(payload, mode)
	if mode == 1 {
		digest, _ := hex.DecodeString(self.MetadataDigest[2:])
		payload = append(payload, digest...)
	}
	return call, payload, nil
}

// Historical action bytes remain fixed even after a runtime upgrade or expiry.
func (self ownerTrimAction) validate() error {
	call, payload, err := self.encoding()
	if err != nil {
		return err
	}
	claimed := self.RequestHash
	self.RequestHash = ""
	if self.Call != "0x"+hex.EncodeToString(call) || self.Payload != "0x"+hex.EncodeToString(payload) || claimed != rootObjectHash(self) {
		return errors.New("owner trim retained request differs from its exact encoding")
	}
	return nil
}

// Retain only the coldkey's signature over the approved native payload.
func (self ownerTrimAction) signed(signature []byte) ([]byte, error) {
	if err := self.validate(); err != nil {
		return nil, err
	}
	account, _ := hex.DecodeString(self.Coldkey[2:])
	payload, _ := hex.DecodeString(self.Payload[2:])
	if len(payload) > 256 {
		digest := blake2b.Sum256(payload)
		payload = digest[:]
	}
	valid := false
	variant, mode := byte(1), byte(0)
	if self.ledgerSigning() {
		variant, mode = 0, 1
		valid = ed25519.Verify(account, payload, signature)
	} else {
		public, err := (sr25519.Scheme{}).FromPublicKey(account)
		if err != nil {
			return nil, err
		}
		valid = len(signature) == 64 && public.Verify(payload, signature)
	}
	if !valid {
		return nil, errors.New("owner trim signature belongs to another coldkey or action")
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

// A receipt/import rechecks the entire original public extrinsic, not its label.
func ownerTrimSignedAction(action ownerTrimAction, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	reader := rootScaleReader{data: raw}
	length, err := reader.compact()
	if err != nil || length != uint64(len(raw)-reader.offset) {
		return errors.New("owner trim extrinsic length differs")
	}
	body := raw[reader.offset:]
	variant := byte(1)
	if action.ledgerSigning() {
		variant = 0
	}
	if len(body) < 99 || body[0] != 0x84 || body[1] != 0 || body[34] != variant {
		return errors.New("owner trim extrinsic envelope differs")
	}
	expected, err := action.signed(body[35:99])
	if err != nil || !bytes.Equal(expected, raw) {
		return errors.Join(errors.New("owner trim extrinsic differs from original action"), err)
	}
	return nil
}

// A recycle transition owns one native owner nonce and immutable mortal call.
// Its approval domain cannot inherit trim, bootstrap or economic-read authority.
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

const ownerRecycleActionSchema = "urnetwork-mainnet-owner-recycle-action-v1"
const ownerRecycleConfigSchema = "urnetwork-mainnet-owner-recycle-execution-v1"
const ownerRecycleStateFile = "owner-recycle-action.json"

// Only Burn-to-Recycle on netuid25 is supported. The birth observation and
// independent review are bound alongside the explicit network/runtime policy.
// Fee reserve is an admission limit, not a maximum encoded native fee.
type ownerRecycleAction struct {
	Schema                  string        `json:"schema"`
	Policy                  recyclePolicy `json:"policy"`
	ObservationHash         string        `json:"birth_observation_hash"`
	ReviewHash              string        `json:"independent_review_hash"`
	CustodyId               string        `json:"custody_id"`
	StatePath               string        `json:"state_path"`
	Owner                   string        `json:"owner_account_id"`
	SubnetRegistrationBlock uint64        `json:"subnet_registration_block"`
	Nonce                   uint32        `json:"nonce"`
	BirthBlock              uint64        `json:"birth_block"`
	BirthHash               string        `json:"birth_hash"`
	Period                  uint64        `json:"mortal_period"`
	FeeReserveRao           uint64        `json:"fee_reserve_rao"`
	SignatureScheme         string        `json:"signature_scheme"`
	MetadataDigest          string        `json:"check_metadata_hash,omitempty"`
	DerivationPath          string        `json:"signer_derivation_path,omitempty"`
	LedgerMetadataHash      string        `json:"ledger_metadata_blake2b_256,omitempty"`
	CallIndex               [2]byte       `json:"call_index"`
	Call                    string        `json:"call_scale"`
	Payload                 string        `json:"payload_scale"`
	RequestHash             string        `json:"request_hash"`
}

// The independently supplied approval key authorizes exactly one action/route.
// No signed document supplies live custody or source-to-Wasm enforcement.
type ownerRecycleConfig struct {
	Schema    string               `json:"schema"`
	Action    ownerRecycleAction   `json:"action"`
	Route     ownedSubmissionRoute `json:"owned_route"`
	Signature string               `json:"approval_signature_ed25519"`
}

// Separate domain bytes prevent reuse of any preexisting owner approval.
func (self ownerRecycleConfig) signingBytes() []byte {
	self.Signature = ""
	raw, _ := json.Marshal(self)
	return append([]byte(self.Schema+"\x00"), raw...)
}

// Every reopen authenticates the independently pinned approval key.
func (self ownerRecycleConfig) validate(key string) error {
	if err := self.Action.validate(); err != nil {
		return err
	}
	if self.Schema != ownerRecycleConfigSchema || !rootCanonicalHash(key) || self.Route.ReadRetrySeconds < 60 || self.Route.ReadRetrySeconds > 900 || self.Route.SendTimeoutSeconds == 0 || self.Route.SendTimeoutSeconds > 60 {
		return errors.New("recycle execution requires independent approval and bounded owned transport")
	}
	if err := self.Route.validate(); err != nil {
		return err
	}
	public, _ := hex.DecodeString(key[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil || !ed25519.Verify(public, self.signingBytes(), signature) {
		return errors.New("recycle action/route approval is invalid")
	}
	return nil
}

// Exact source and artifact pins travel with the original owner/admin action.
func (self ownerRecycleAction) runtime() rootReceiptProfile {
	p := self.Policy
	return rootReceiptProfile{RuntimeSourceCommit: p.RuntimeSourceCommit, RuntimeVersion: p.RuntimeVersion, RuntimeCodeHash: p.RuntimeCodeHash, RuntimeMetadataHash: p.RuntimeMetadataHash}
}

// Metadata authenticates the entire native envelope and exact enum argument.
func prepareOwnerRecycleAction(action ownerRecycleAction, metadataHex string) (ownerRecycleAction, error) {
	if len(metadataHex) > 2+2*maxMetadataRpcReplyBytes {
		return ownerRecycleAction{}, errors.New("recycle metadata exceeds bound")
	}
	metadata, digest, err := nativePinnedMetadata(metadataHex, action.Policy.RuntimeMetadataHash)
	if err != nil || digest != action.Policy.RuntimeMetadataHash {
		return ownerRecycleAction{}, errors.Join(errors.New("recycle metadata differs from independent pin"), err)
	}
	variant, index := "Sr25519", uint8(1)
	if action.SignatureScheme == "ed25519" {
		variant, index = "Ed25519", 0
	}
	if err := nativeSigningProfileForSignature(metadata, variant, index); err != nil {
		return ownerRecycleAction{}, err
	}
	call, err := ownerRecycleCall(metadata)
	if err != nil {
		return ownerRecycleAction{}, err
	}
	action.CallIndex = call
	callRaw, payload, err := action.encoding()
	if err != nil {
		return ownerRecycleAction{}, err
	}
	action.Call, action.Payload = "0x"+hex.EncodeToString(callRaw), "0x"+hex.EncodeToString(payload)
	action.RequestHash = ""
	action.RequestHash = rootObjectHash(action)
	return action, nil
}

// Reconstruct the five-byte direct call, zero tip and fixed mortal envelope.
func (self ownerRecycleAction) encoding() ([]byte, []byte, error) {
	if err := self.Policy.validate(); err != nil {
		return nil, nil, err
	}
	if self.Schema != ownerRecycleActionSchema || !nativeOwnerRuntimeProfile(self.runtime()) ||
		!planSha256(self.ObservationHash) || !planSha256(self.ReviewHash) || !planLabel(self.CustodyId) || !bootstrapRootAbsolutePath(self.StatePath) || filepath.Base(self.StatePath) != ownerRecycleStateFile ||
		!rootCanonicalHash(self.Owner) || !rootCanonicalHash(self.BirthHash) || self.BirthBlock < self.SubnetRegistrationBlock || self.Nonce == math.MaxUint32 || self.FeeReserveRao == 0 {
		return nil, nil, errors.New("recycle action has invalid reviewed owner scope, custody, era or allowance")
	}
	for _, hash := range []string{self.Policy.GenesisHash, self.Policy.RuntimeCodeHash, self.Policy.RuntimeMetadataHash} {
		if !rootCanonicalHash(hash) {
			return nil, nil, errors.New("recycle policy hashes must be canonical")
		}
	}
	mode := byte(0)
	switch self.SignatureScheme {
	case "sr25519":
		if self.MetadataDigest != "" || self.LedgerMetadataHash != "" || self.DerivationPath != "" {
			return nil, nil, errors.New("recycle sr25519 profile has no Ledger metadata mode")
		}
	case "ed25519":
		if !rootCanonicalHash(self.MetadataDigest) || !rootCanonicalHash(self.LedgerMetadataHash) {
			return nil, nil, errors.New("recycle Ledger profile requires independently approved RFC78 digest and metadata15 hash")
		}
		if _, err := ownerLedgerDerivationPath(self.DerivationPath); err != nil {
			return nil, nil, err
		}
		mode = 1
	default:
		return nil, nil, errors.New("unsupported recycle native signature scheme")
	}
	era, err := rootMortalEra(self.BirthBlock, self.Period)
	if err != nil {
		return nil, nil, err
	}
	call := binary.LittleEndian.AppendUint16(append([]byte(nil), self.CallIndex[:]...), self.Policy.Netuid)
	call = append(call, 1) // Recycle, never Burn or a caller-selected enum.
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
func (self ownerRecycleAction) validate() error {
	call, payload, err := self.encoding()
	if err != nil {
		return err
	}
	claimed := self.RequestHash
	self.RequestHash = ""
	if self.Call != "0x"+hex.EncodeToString(call) || self.Payload != "0x"+hex.EncodeToString(payload) || claimed != rootObjectHash(self) {
		return errors.New("recycle action encoding or seal changed")
	}
	return nil
}

// Verify the exact owner's public signature before constructing retained bytes.
func (self ownerRecycleAction) signed(signature []byte) ([]byte, error) {
	if err := self.validate(); err != nil {
		return nil, err
	}
	account, _ := hex.DecodeString(self.Owner[2:])
	payload, _ := hex.DecodeString(self.Payload[2:])
	if len(payload) > 256 {
		digest := blake2b.Sum256(payload)
		payload = digest[:]
	}
	variant, mode, valid := byte(1), byte(0), false
	if self.SignatureScheme == "ed25519" {
		variant, mode = 0, 1
		valid = len(signature) == 64 && ed25519.Verify(account, payload, signature)
	} else {
		public, err := (sr25519.Scheme{}).FromPublicKey(account)
		if err != nil {
			return nil, err
		}
		valid = len(signature) == 64 && public.Verify(payload, signature)
	}
	if !valid {
		return nil, errors.New("recycle signature belongs to another owner or action")
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
func ownerRecycleSignedAction(action ownerRecycleAction, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	reader := rootScaleReader{data: raw}
	length, err := reader.compact()
	if err != nil || length != uint64(len(raw)-reader.offset) {
		return errors.New("recycle extrinsic length changed")
	}
	body := raw[reader.offset:]
	variant := byte(1)
	if action.SignatureScheme == "ed25519" {
		variant = 0
	}
	if len(body) < 99 || body[0] != 0x84 || body[1] != 0 || body[34] != variant {
		return errors.New("recycle extrinsic envelope changed")
	}
	expected, err := action.signed(body[35:99])
	if err != nil || !bytes.Equal(expected, raw) {
		return errors.Join(errors.New("recycle extrinsic differs from original action"), err)
	}
	return nil
}

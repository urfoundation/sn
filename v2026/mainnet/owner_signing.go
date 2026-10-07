// Portable owner requests carry approved native bytes and public metadata.
// Owner-side verification never opens paths or routes embedded in the request.
package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"golang.org/x/crypto/blake2b"
)

const ownerSigningRequestSchema = "urnetwork-mainnet-owner-signing-request-v1"
const ownerSigningReplySchema = "urnetwork-mainnet-owner-signing-reply-v1"
const ownerSigningRequestLimit = 4*maxMetadataRpcReplyBytes + 128*1024
const ownerSigningReplyLimit = 16 * 1024

// These pins come from the owner's independent review, never from request data.
type ownerSigningTrust struct {
	RequestHash string
	ApprovalKey string
	Owner       string
	Genesis     string
}

// Config includes the original exact call, nonce, era, custody and approval.
// Raw metadata authenticates encoding; it is not the RFC78 Merkle digest/proof.
type ownerSigningRequest struct {
	Schema          string                   `json:"schema"`
	Config          ownerTrimExecutionConfig `json:"approved_config"`
	MetadataHex     string                   `json:"runtime_metadata_scale"`
	LedgerMetadata  string                   `json:"ledger_metadata_scale,omitempty"`
	SignatureScheme string                   `json:"signature_scheme"`
	SigningBytes    string                   `json:"signing_bytes"`
	ContentHash     string                   `json:"content_hash"`
}

// Substrate signs the exact payload or its Blake2b-256 digest above 256 bytes.
func ownerSigningBytes(action ownerTrimAction) []byte {
	payload, _ := hex.DecodeString(strings.TrimPrefix(action.Payload, "0x"))
	if len(payload) > 256 {
		digest := blake2b.Sum256(payload)
		return digest[:]
	}
	return payload
}

// Export retains the old scheme for v1; it never upgrades an approved action.
func newOwnerSigningRequest(config ownerTrimExecutionConfig, key, metadataHex string, ledgerMetadata ...string) (ownerSigningRequest, error) {
	if err := config.validate(key); err != nil {
		return ownerSigningRequest{}, err
	}
	if len(metadataHex) > 2+2*maxMetadataRpcReplyBytes {
		return ownerSigningRequest{}, errors.New("owner signing metadata exceeds bound")
	}
	request := ownerSigningRequest{Schema: ownerSigningRequestSchema, Config: config, MetadataHex: metadataHex,
		SignatureScheme: "sr25519", SigningBytes: "0x" + hex.EncodeToString(ownerSigningBytes(config.Action))}
	if len(ledgerMetadata) > 1 || len(ledgerMetadata) == 1 && len(ledgerMetadata[0]) > 2+2*maxMetadataRpcReplyBytes {
		return ownerSigningRequest{}, errors.New("owner signing requires one bounded Ledger metadata artifact")
	}
	if len(ledgerMetadata) == 1 {
		request.LedgerMetadata = ledgerMetadata[0]
	}
	if config.Action.ledgerSigning() {
		request.SignatureScheme = "ed25519"
	}
	request.ContentHash = rootObjectHash(request)
	trust := ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: key, Owner: config.Action.Coldkey, Genesis: config.Action.Network.GenesisHash}
	return request, request.validate(trust)
}

// Rebuild the native call from pinned metadata and verify the separate approval.
// Embedded custody paths are inert labels on the owner's independent computer.
func (self ownerSigningRequest) validate(trust ownerSigningTrust) error {
	if !planSha256(trust.RequestHash) || !rootCanonicalHash(trust.Owner) || !rootCanonicalHash(trust.Genesis) ||
		self.Schema != ownerSigningRequestSchema || self.Config.Action.Coldkey != trust.Owner || self.Config.Action.Network.GenesisHash != trust.Genesis {
		return errors.New("owner signing request differs from independent request, account or chain pins")
	}
	if err := self.Config.validate(trust.ApprovalKey); err != nil {
		return err
	}
	if _, err := rootReceiptHex(self.MetadataHex, maxMetadataRpcReplyBytes); err != nil {
		return errors.New("owner signing metadata requires bounded canonical hex")
	}
	if self.Config.Action.ledgerSigning() {
		hash, err := ownerLedgerMetadataHash(self.LedgerMetadata)
		if err != nil || hash != self.Config.Action.LedgerMetadataHash {
			return errors.Join(errors.New("owner signing metadata15 differs from its independent approval"), err)
		}
	} else if self.LedgerMetadata != "" {
		return errors.New("owner v1 cannot acquire a different metadata/signing profile")
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if claimed != trust.RequestHash || rootObjectHash(self) != claimed {
		return errors.New("owner signing request content seal differs")
	}
	action, err := prepareOwnerTrimAction(self.Config.Action, self.MetadataHex)
	if err != nil || rootObjectHash(action) != rootObjectHash(self.Config.Action) {
		return errors.Join(errors.New("owner signing action differs from pinned metadata or exact native encoding"), err)
	}
	scheme := "sr25519"
	if action.ledgerSigning() {
		scheme = "ed25519"
	}
	if self.SignatureScheme != scheme || self.SigningBytes != "0x"+hex.EncodeToString(ownerSigningBytes(action)) {
		return errors.New("owner signing scheme or exact signing bytes differ")
	}
	return nil
}

// RFC78's pinned implementation requires metadata15. This raw artifact hash
// is independently approved; the SDK validates its structure and digest before
// opening a device. It is distinct from metadata14 and the RFC78 digest itself.
func ownerLedgerMetadataHash(encoded string) (string, error) {
	raw, err := rootReceiptHex(encoded, maxMetadataRpcReplyBytes)
	if err != nil || len(raw) < 5 || !bytes.Equal(raw[:5], []byte{'m', 'e', 't', 'a', 15}) {
		return "", errors.New("owner Ledger metadata requires unwrapped canonical metadata15 bytes")
	}
	return rootExtrinsicHash(raw), nil
}

// A reply binds the original portable request and the complete verified native
// envelope. It makes no claim of current eligibility or physical device origin.
type ownerSigningReply struct {
	Schema          string `json:"schema"`
	RequestHash     string `json:"request_hash"`
	ActionHash      string `json:"action_request_hash"`
	SignatureScheme string `json:"signature_scheme"`
	Owner           string `json:"owner_account_id"`
	Signature       string `json:"signature"`
	RawExtrinsic    string `json:"signed_extrinsic"`
	ExtrinsicHash   string `json:"extrinsic_hash"`
}

// Only an independently obtained public signature enters this constructor.
func newOwnerSigningReply(request ownerSigningRequest, signature []byte) (ownerSigningReply, error) {
	action := request.Config.Action
	raw, err := action.signed(signature)
	if err != nil {
		return ownerSigningReply{}, err
	}
	return ownerSigningReply{Schema: ownerSigningReplySchema, RequestHash: request.ContentHash, ActionHash: action.RequestHash,
		SignatureScheme: request.SignatureScheme, Owner: action.Coldkey, Signature: hex.EncodeToString(signature),
		RawExtrinsic: "0x" + hex.EncodeToString(raw), ExtrinsicHash: rootExtrinsicHash(raw)}, nil
}

// Verification includes native signature, account, payload and exact envelope;
// a rehashed reply or another valid signature cannot change the request domain.
func (self ownerSigningReply) validate(request ownerSigningRequest) ([]byte, error) {
	if self.Schema != ownerSigningReplySchema || self.RequestHash != request.ContentHash || self.ActionHash != request.Config.Action.RequestHash ||
		self.SignatureScheme != request.SignatureScheme || self.Owner != request.Config.Action.Coldkey {
		return nil, errors.New("owner signing reply belongs to another request, account or algorithm")
	}
	signature, err := rootOfflineSignatureBytes(self.Signature)
	if err != nil {
		return nil, err
	}
	expected, err := newOwnerSigningReply(request, signature)
	if err != nil || expected != self {
		return nil, errors.Join(errors.New("owner signing reply differs from its exact native signature and extrinsic"), err)
	}
	return signature, nil
}

// Ledger returns a Substrate MultiSignature: variant zero and 64 Ed25519 bytes.
// Status words and raw 64-byte signatures are not silently stripped or guessed.
func ownerLedgerResponse(request ownerSigningRequest, response []byte) ([]byte, error) {
	if !request.Config.Action.ledgerSigning() || request.SignatureScheme != "ed25519" || len(response) != 65 || response[0] != 0 {
		return nil, errors.New("Ledger owner response requires exactly MultiSignature::Ed25519 (00 plus 64 bytes)")
	}
	signature := bytes.Clone(response[1:])
	if _, err := request.Config.Action.signed(signature); err != nil {
		return nil, err
	}
	return signature, nil
}

// The generic app fixes the first two path components. Remaining components
// are explicit, canonical u31 indices; no account, hardening or path is guessed.
func ownerLedgerDerivationPath(path string) ([5]uint32, error) {
	var result [5]uint32
	parts := strings.Split(path, "/")
	if len(parts) != 6 || parts[0] != "m" || parts[1] != "44'" || parts[2] != "354'" {
		return result, errors.New("owner Ledger path requires explicit m/44'/354'/account/change/index")
	}
	for index, part := range parts[1:] {
		hardened := strings.HasSuffix(part, "'")
		number := strings.TrimSuffix(part, "'")
		value, err := strconv.ParseUint(number, 10, 31)
		if err != nil || strconv.FormatUint(value, 10) != number {
			return result, errors.New("owner Ledger path has a noncanonical or oversized component")
		}
		result[index] = uint32(value)
		if hardened {
			result[index] |= 0x80000000
		}
	}
	return result, nil
}

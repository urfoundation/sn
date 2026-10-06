// The Ledger transcript describes the pinned generic-app protocol offline.
// It neither connects to a device nor qualifies its metadata proof or firmware.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
)

const ownerLedgerTranscriptSchema = "urnetwork-mainnet-owner-ledger-transcript-v1"
const ownerLedgerAppSource = "644ec63851072f6eac9c3911ea23a811f3a1b15c"
const ownerLedgerPayloadLimit = 16 * 1024

// Address confirmation precedes signing on a qualified transport. Its returned
// 32-byte key must equal ExpectedPublicKey; an SS58 display string is not enough.
type ownerLedgerTranscript struct {
	Schema                string   `json:"schema"`
	RequestHash           string   `json:"request_hash"`
	AppSource             string   `json:"app_source_commit"`
	DerivationPath        string   `json:"derivation_path"`
	ExpectedPublicKey     string   `json:"expected_public_key"`
	MetadataDigest        string   `json:"check_metadata_hash"`
	MetadataProofSha256   string   `json:"metadata_proof_sha256"`
	AddressApdu           string   `json:"confirm_address_apdu"`
	SigningApdus          []string `json:"signing_apdus"`
	SignatureResponse     string   `json:"signature_response_encoding"`
	DeviceQualified       bool     `json:"device_qualified"`
	MetadataProofVerified bool     `json:"metadata_proof_verified"`
	Signing               bool     `json:"signing"`
	NetworkEffects        bool     `json:"network_effects"`
}

// Match Zondax's five little-endian path words, u16 payload length and 250-byte
// chunks. The device verifies shortened RFC78 metadata appended after payload.
// A generated transcript alone grants no signing or custody authority.
func newOwnerLedgerTranscript(request ownerSigningRequest, metadataProof []byte) (ownerLedgerTranscript, error) {
	action := request.Config.Action
	if err := action.validate(); err != nil {
		return ownerLedgerTranscript{}, err
	}
	if !action.ledgerSigning() || request.SignatureScheme != "ed25519" {
		return ownerLedgerTranscript{}, errors.New("Ledger generic app cannot sign the retained sr25519/disabled-metadata owner v1 action")
	}
	payload, _ := hex.DecodeString(action.Payload[2:])
	return ownerLedgerTranscriptForPayload(request.ContentHash, action.Coldkey, action.DerivationPath, action.MetadataDigest, payload, metadataProof)
}

// Shared wire construction carries no call authority. Each caller validates its
// own approved action and metadata before creating an inert device transcript.
func ownerLedgerTranscriptForPayload(requestHash, owner, derivationPath, metadataDigest string, payload, metadataProof []byte) (ownerLedgerTranscript, error) {
	if len(metadataProof) == 0 || len(payload)+len(metadataProof) > ownerLedgerPayloadLimit {
		return ownerLedgerTranscript{}, errors.New("Ledger payload and shortened metadata require a nonempty proof and at most 16 KiB combined")
	}
	path, err := ownerLedgerDerivationPath(derivationPath)
	if err != nil {
		return ownerLedgerTranscript{}, err
	}
	var encodedPath []byte
	for _, index := range path {
		encodedPath = binary.LittleEndian.AppendUint32(encodedPath, index)
	}
	proofHash := sha256.Sum256(metadataProof)
	result := ownerLedgerTranscript{Schema: ownerLedgerTranscriptSchema, RequestHash: requestHash, AppSource: ownerLedgerAppSource,
		DerivationPath: derivationPath, ExpectedPublicKey: owner, MetadataDigest: metadataDigest,
		MetadataProofSha256: "sha256:" + hex.EncodeToString(proofHash[:]), SignatureResponse: "00 || ed25519_signature_64_bytes; APDU status 9000 excluded"}
	// SS58 prefix 42 is the inspected Subtensor display prefix; identity remains
	// the independently pinned raw AccountId32, never a derived replacement.
	address := binary.LittleEndian.AppendUint16(append([]byte{0xf9, 0x01, 0x01, 0x00, 22}, encodedPath...), 42)
	result.AddressApdu = "0x" + hex.EncodeToString(address)
	initial := binary.LittleEndian.AppendUint16(append([]byte{0xf9, 0x02, 0x00, 0x00, 22}, encodedPath...), uint16(len(payload)))
	result.SigningApdus = append(result.SigningApdus, "0x"+hex.EncodeToString(initial))
	data := append(append([]byte(nil), payload...), metadataProof...)
	for offset := 0; offset < len(data); offset += 250 {
		end, kind := min(offset+250, len(data)), byte(1)
		if end == len(data) {
			kind = 2
		}
		apdu := append([]byte{0xf9, 0x02, kind, 0x00, byte(end - offset)}, data[offset:end]...)
		result.SigningApdus = append(result.SigningApdus, "0x"+hex.EncodeToString(apdu))
	}
	return result, nil
}

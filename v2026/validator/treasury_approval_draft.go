// Unsigned treasury approvals are bound to the exact normalized config that the
// production loader hashes. Drafting and assembly never load or create a key.
package validator

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
)

// The signed message is SHA-256 of this line followed by the approval JSON.
const TreasuryApprovalSignatureDomain = "urnetwork-native-treasury-approval-signature-v1\n"

// Bounds both the approval file and the envelope, as the loader bounds envelopes.
const MaximumTreasuryApprovalBytes = maximumOwnerRecycleApprovalBytes

// One unsigned approval and the complete 32-byte message an external Ed25519
// signer approves. Encoded is the approval file: Go's canonical JSON encoding
// of Approval followed by exactly one newline. Digest covers that JSON without
// the newline. A draft is neither a signature nor an approval.
type TreasuryApprovalDraft struct {
	Approval TreasuryApproval
	Encoded  []byte
	Digest   [32]byte
}

// Decodes and normalizes the borrowed config bytes exactly as the schema-3
// production loader does for path and derives ConfigHash with the loader's
// own hash; any supplied ConfigHash is replaced. A hash that would change if
// the file moved, because a path is relative, is refused. The config must pin
// signer, and the body must pass every admission check that precedes the
// signature. The envelope reference may still be incomplete: the hash
// excludes it, and the loader checks it with the signature, the runtime and
// authority history and the full config.
func DraftTreasuryApproval(path string, raw []byte, signer [32]byte, approval TreasuryApproval) (*TreasuryApprovalDraft, error) {
	if err := ValidateReleaseEvidenceV2Path(path); err != nil {
		return nil, err
	}
	// The producer loader lowercases both addresses before it hashes schema 3.
	decode := func(at string) (*ReleaseConfig, error) {
		cfg, err := decodeReleaseConfigDocument(at, raw)
		if err != nil {
			return nil, err
		}
		cfg.Coordinator, cfg.SettlementVault = strings.ToLower(cfg.Coordinator), strings.ToLower(cfg.SettlementVault)
		return cfg, nil
	}
	cfg, err := decode(path)
	if err != nil {
		return nil, err
	}
	if cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion || cfg.TreasuryApproval == nil || cfg.OwnerRecycleApproval != nil {
		return nil, errors.New("treasury approval requires a schema 3 config with only its treasury_approval selector")
	}
	if err := validateOwnerRecycleApprovalScope(cfg); err != nil {
		return nil, err
	}
	if cfg.TreasuryApproval.Signer != attemptHex32(signer) {
		return nil, errors.New("treasury approval config pins another signer")
	}
	approval.ConfigHash, err = OwnerRecycleConfigHash(cfg)
	if err != nil {
		return nil, err
	}
	moved, err := decode(filepath.Join(filepath.Dir(path), "relocated", filepath.Base(path)))
	if err != nil {
		return nil, err
	}
	movedHash, err := OwnerRecycleConfigHash(moved)
	if err != nil || movedHash != approval.ConfigHash {
		return nil, errors.Join(errors.New("treasury approval config hash depends on where the config file sits; give every path absolutely"), err)
	}
	message, err := admitOwnerRecycleApproval(cfg, &approval)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(approval)
	if err != nil {
		return nil, err
	}
	// The returned draft owns a decoded copy and must survive its own reader.
	draft, err := DecodeTreasuryApprovalDraft(append(encoded, '\n'))
	if err != nil || !bytes.Equal(draft.Digest[:], message) {
		return nil, errors.Join(errors.New("treasury approval draft does not survive its canonical file round trip"), err)
	}
	return draft, nil
}

// Admits only a canonical treasury approval file: unique keys, known fields,
// Go's exact JSON encoding and one final newline. The digest is recomputed
// from those bytes and is not bound to any config by this decoder.
func DecodeTreasuryApprovalDraft(encoded []byte) (*TreasuryApprovalDraft, error) {
	if len(encoded) < 2 || len(encoded) > MaximumTreasuryApprovalBytes || encoded[len(encoded)-1] != '\n' {
		return nil, errors.New("treasury approval file is empty, oversized or lacks its final newline")
	}
	body := encoded[:len(encoded)-1]
	if err := protocol.ValidateUniqueJsonKeys(body); err != nil {
		return nil, err
	}
	var approval TreasuryApproval
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&approval); err != nil {
		return nil, fmt.Errorf("treasury approval: %w", err)
	}
	canonical, err := json.Marshal(approval)
	if err != nil || !bytes.Equal(body, canonical) {
		return nil, errors.Join(errors.New("treasury approval file must hold the canonical approval JSON"), err)
	}
	if approval.Schema != TreasuryApprovalSchema {
		return nil, errors.New("treasury approval file has another approval schema")
	}
	message, err := approval.SigningMessage()
	if err != nil {
		return nil, err
	}
	return &TreasuryApprovalDraft{Approval: approval, Encoded: bytes.Clone(encoded), Digest: [32]byte(message)}, nil
}

// Verifies an external signature over the digest recomputed from the approval
// file and returns the exact envelope file the production loader reads. The
// signer is the config's independently pinned treasury_approval.signer.
func AssembleTreasuryApprovalEnvelope(encoded []byte, signer [32]byte, signature string) ([]byte, *TreasuryApprovalDraft, error) {
	draft, err := DecodeTreasuryApprovalDraft(encoded)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(TreasuryApprovalEnvelope{Approval: draft.Approval, Signature: signature})
	if err != nil {
		return nil, nil, err
	}
	raw = append(raw, '\n')
	// The accounting verifier repeats canonical decoding, the treasury schemas
	// and the lowercase Ed25519 check over the same recomputed digest.
	if _, err := VerifyTreasuryApproval(raw, signer, attemptHex32(sha256.Sum256(raw))); err != nil {
		return nil, nil, err
	}
	return raw, draft, nil
}

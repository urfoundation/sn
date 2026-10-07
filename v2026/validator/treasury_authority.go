// Treasury approval exports admit exact signed public policy without ever
// loading a treasury signer or treating an accounting projection as a producer.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Full production configuration admission retains all existing source and
// runtime requirements and grants no private prepared-transaction owner.
func DecodeTreasuryApproval(cfg *ReleaseConfig, encoded []byte) (*TreasuryApprovalEnvelope, error) {
	if cfg == nil || cfg.TreasuryApproval == nil || cfg.OwnerRecycleApproval != nil {
		return nil, errors.New("treasury approval requires its explicit configuration selector")
	}
	return decodeOwnerRecycleApproval(cfg, encoded)
}

// Accounting callers independently pin signer and content hash inside their
// own signed authority. This verifies only that signed policy projection; it
// cannot approve a validator config, observed census or native transaction.
func VerifyTreasuryApprovalPolicy(encoded []byte, signer [32]byte, approvalHash string) (*TreasuryPolicy, error) {
	envelope, err := VerifyTreasuryApproval(encoded, signer, approvalHash)
	if err != nil {
		return nil, err
	}
	return envelope.Approval.Proposal.Treasury, nil
}

// Exposes the original signed scope so execution observers must additionally
// match its genesis, runtime artifact and finite native activation interval.
func VerifyTreasuryApproval(encoded []byte, signer [32]byte, approvalHash string) (*TreasuryApprovalEnvelope, error) {
	if signer == ([32]byte{}) || len(encoded) == 0 || len(encoded) > maximumOwnerRecycleApprovalBytes ||
		approvalHash != attemptHex32(sha256.Sum256(encoded)) {
		return nil, errors.New("treasury approval policy projection lacks exact bounded bytes and signer")
	}
	if err := protocol.ValidateUniqueJsonKeys(encoded); err != nil {
		return nil, err
	}
	var envelope TreasuryApprovalEnvelope
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(envelope)
	approval := &envelope.Approval
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) || approval.Schema != TreasuryApprovalSchema ||
		approval.Proposal.Schema != TreasuryProposalSchema || approval.Proposal.Treasury == nil || approval.Production == nil ||
		approval.Production.Schema != TreasuryProductionScope {
		return nil, errors.New("treasury approval policy projection has another schema or noncanonical bytes")
	}
	message, err := approval.SigningMessage()
	if err != nil {
		return nil, err
	}
	signature, err := hex.DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || hex.EncodeToString(signature) != envelope.Signature ||
		!ed25519.Verify(signer[:], message, signature) {
		return nil, errors.New("treasury approval policy signature differs from its independently pinned signer")
	}
	return &envelope, nil
}

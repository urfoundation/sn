// Prospective mapping consent carries the operator's original issuance
// boundary inside the coldkey-signed message. Historical v1 bytes are unchanged.
package protocol

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const WalletMappingProspectiveSchema = "urnetwork-provider-wallet-mapping-consent-v2"

// This is a signed issuance statement, not independent consensus finality.
// The consumer independently selects the operator signer and earning boundary.
type WalletMappingProspectiveApproval struct {
	Boundary  ClientKeyEffectiveBoundary `json:"boundary"`
	Signer    common.Address             `json:"signer"`
	Signature [65]byte                   `json:"signature"`
}

// Every displayed identity, nonce, predecessor and interval is covered by the
// operator signature before the actual wallet signs those exact same bytes.
func (self WalletMappingStatement) prospectiveDigest() ([32]byte, error) {
	if self.Schema != WalletMappingProspectiveSchema || self.Prospective.Signer == (common.Address{}) || self.FromEpoch <= self.Prospective.Boundary.Epoch {
		return [32]byte{}, ErrWalletMappingIntegrity
	}
	if err := errors.Join(self.Domain.Validate(), self.Prospective.Boundary.Validate()); err != nil {
		return [32]byte{}, errors.Join(ErrWalletMappingIntegrity, err)
	}
	self.Prospective.Signature = [65]byte{}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > MaxWalletMappingMessageBytes {
		return [32]byte{}, errors.Join(ErrWalletMappingIntegrity, err)
	}
	return sha256.Sum256(raw), nil
}

// The actual operator owner supplies a freshly authenticated finalized
// boundary and its private key; no public request can supply this key.
func SignProspectiveWalletMapping(statement *WalletMappingStatement, boundary ClientKeyEffectiveBoundary, key *ecdsa.PrivateKey) error {
	if statement == nil || !validClientKeyStatementSigner(key) {
		return ErrWalletMappingUnavailable
	}
	owned := *statement
	owned.Schema = WalletMappingProspectiveSchema
	owned.Prospective = WalletMappingProspectiveApproval{Boundary: boundary, Signer: crypto.PubkeyToAddress(key.PublicKey)}
	digest, err := owned.prospectiveDigest()
	if err != nil {
		return err
	}
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		return err
	}
	copy(owned.Prospective.Signature[:], signature)
	if _, err := owned.Message(); err != nil {
		return err
	}
	*statement = owned
	return nil
}

// Signature verification does not select a trusted signer. That separate
// authority is mandatory for the prospective earning-window admission below.
func (self WalletMappingStatement) VerifyProspectiveSignature() error {
	digest, err := self.prospectiveDigest()
	if err != nil {
		return err
	}
	if err := verifyClientKeyStatementSignature(digest, self.Prospective.Signature, self.Prospective.Signer); err != nil {
		return errors.Join(ErrWalletMappingIntegrity, err)
	}
	return nil
}

// The independently authenticated start clock prevents a newly collected
// consent from retroactively changing an already-earned window. Old v1
// consent proves key possession only and remains unavailable for this gate.
func VerifyProspectiveWalletMapping(ctx context.Context, mapping *VerifiedWalletMapping, signer common.Address, startBlock uint64, startUnix int64) error {
	if ctx == nil || mapping == nil || signer == (common.Address{}) || startBlock == 0 || startUnix <= 0 {
		return ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	statement := mapping.Statement
	if statement.Schema != WalletMappingProspectiveSchema {
		return ErrWalletMappingUnavailable
	}
	if err := statement.VerifyProspectiveSignature(); err != nil {
		return err
	}
	if statement.Prospective.Signer != signer || statement.Prospective.Boundary.Block >= startBlock || statement.FromEpoch <= statement.Prospective.Boundary.Epoch {
		return ErrWalletMappingIntegrity
	}
	// Expiry bounds possible acceptance; it is not an original acceptance time.
	// A legitimately accepted near-boundary consent can therefore be unknown
	// without contradicting its exact approved identity or earning interval.
	if statement.ExpiresAt > startUnix {
		return ErrWalletMappingUnavailable
	}
	return ctx.Err()
}

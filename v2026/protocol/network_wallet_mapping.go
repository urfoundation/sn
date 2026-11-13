// Network wallet mapping consent: the coldkey owner signs once for a network,
// and the consent covers every provider client of that network, current and
// future. It is a separate chain per (domain, network) beside the unchanged
// per-provider chains. The first line, the schema and the explicit scope keep
// the two kinds apart: each decoder requires its own prefix and refuses the
// other's fields, so a signature over one kind never verifies as the other.
// Network consents are prospective from their first schema: the operator signs
// the issuance boundary before the wallet signs the same displayed bytes.
package protocol

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const NetworkWalletMappingConsentSchema = "urnetwork-network-wallet-mapping-consent-v1"
const NetworkWalletMappingConsentPrefix = "Approve URnetwork network wallet mapping\n"

// the only scope a network statement carries
const NetworkWalletMappingScope = "network"

// The displayed network statement. It names the authenticated network owner,
// the network and the coldkey, and no client: it applies to every provider
// client of the network.
type NetworkWalletMappingStatement struct {
	Schema       string                           `json:"schema"`
	Scope        string                           `json:"scope"`
	Domain       ClientKeyHistoryDomain           `json:"domain"`
	UserId       [16]byte                         `json:"user_id"`
	NetworkId    [16]byte                         `json:"network_id"`
	Coldkey      [32]byte                         `json:"coldkey"`
	Generation   uint64                           `json:"generation"`
	PreviousHash [32]byte                         `json:"previous_hash"`
	Nonce        [32]byte                         `json:"nonce"`
	IssuedAt     int64                            `json:"issued_at"`
	ExpiresAt    int64                            `json:"expires_at"`
	FromEpoch    uint64                           `json:"from_epoch"`
	ThroughEpoch uint64                           `json:"through_epoch"`
	Prospective  WalletMappingProspectiveApproval `json:"prospective"`
}

// The identity, lineage, interval and acceptance window rules shared by every
// encoding and signing path.
func (self NetworkWalletMappingStatement) validate() error {
	if err := self.Domain.Validate(); err != nil {
		return errors.Join(ErrWalletMappingIntegrity, err)
	}
	if self.Schema != NetworkWalletMappingConsentSchema || self.Scope != NetworkWalletMappingScope || self.UserId == ([16]byte{}) || self.NetworkId == ([16]byte{}) || self.Coldkey == ([32]byte{}) || self.Nonce == ([32]byte{}) || self.Generation == 0 || self.Generation > MaxWalletMappingHistory || (self.Generation == 1) != (self.PreviousHash == ([32]byte{})) || self.IssuedAt <= 0 || self.ExpiresAt <= self.IssuedAt || self.ExpiresAt-self.IssuedAt > 300 || self.ThroughEpoch < self.FromEpoch || self.ThroughEpoch-self.FromEpoch > 65535 {
		return ErrWalletMappingIntegrity
	}
	return nil
}

// Fixed canonical bytes make the displayed approval and the retained signature
// the same object. The operator's prospective signature must already verify.
func (self NetworkWalletMappingStatement) Message() (string, error) {
	if err := self.validate(); err != nil {
		return "", err
	}
	if err := self.VerifyProspectiveSignature(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw)+len(NetworkWalletMappingConsentPrefix) > MaxWalletMappingMessageBytes {
		return "", errors.Join(ErrWalletMappingIntegrity, err)
	}
	return NetworkWalletMappingConsentPrefix + string(raw), nil
}

// Every displayed field is covered by the operator signature before the
// wallet signs those exact bytes.
func (self NetworkWalletMappingStatement) prospectiveDigest() ([32]byte, error) {
	if err := self.validate(); err != nil {
		return [32]byte{}, err
	}
	if self.Prospective.Signer == (common.Address{}) || self.FromEpoch <= self.Prospective.Boundary.Epoch {
		return [32]byte{}, ErrWalletMappingIntegrity
	}
	if err := self.Prospective.Boundary.Validate(); err != nil {
		return [32]byte{}, errors.Join(ErrWalletMappingIntegrity, err)
	}
	self.Prospective.Signature = [65]byte{}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > MaxWalletMappingMessageBytes {
		return [32]byte{}, errors.Join(ErrWalletMappingIntegrity, err)
	}
	return sha256.Sum256(raw), nil
}

// The operator owner supplies a freshly authenticated finalized boundary and
// its private key; no public request can supply this key.
func SignProspectiveNetworkWalletMapping(statement *NetworkWalletMappingStatement, boundary ClientKeyEffectiveBoundary, key *ecdsa.PrivateKey) error {
	if statement == nil || !validClientKeyStatementSigner(key) {
		return ErrWalletMappingUnavailable
	}
	owned := *statement
	owned.Schema, owned.Scope = NetworkWalletMappingConsentSchema, NetworkWalletMappingScope
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

// Signature verification does not select a trusted signer; the earning gate
// below requires the independently selected operator signer.
func (self NetworkWalletMappingStatement) VerifyProspectiveSignature() error {
	digest, err := self.prospectiveDigest()
	if err != nil {
		return err
	}
	if err := verifyClientKeyStatementSignature(digest, self.Prospective.Signature, self.Prospective.Signer); err != nil {
		return errors.Join(ErrWalletMappingIntegrity, err)
	}
	return nil
}

// Decoding does not normalize the signed message into another spelling, and
// a provider statement never decodes here.
func DecodeNetworkWalletMappingStatement(message string) (*NetworkWalletMappingStatement, error) {
	if len(message) > MaxWalletMappingMessageBytes || !bytes.HasPrefix([]byte(message), []byte(NetworkWalletMappingConsentPrefix)) {
		return nil, ErrWalletMappingIntegrity
	}
	raw := []byte(message[len(NetworkWalletMappingConsentPrefix):])
	if err := ValidateUniqueJsonKeys(raw); err != nil {
		return nil, errors.Join(ErrWalletMappingIntegrity, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result NetworkWalletMappingStatement
	if err := decoder.Decode(&result); err != nil {
		return nil, errors.Join(ErrWalletMappingIntegrity, err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrWalletMappingIntegrity
	}
	exact, err := result.Message()
	if err != nil || exact != message {
		return nil, errors.Join(ErrWalletMappingIntegrity, err)
	}
	return &result, nil
}

// The retained original and its content hash, after the coldkey's signature
// over the exact message verifies.
func VerifyNetworkWalletMappingConsent(ctx context.Context, original WalletMappingConsent) (*NetworkWalletMappingStatement, [32]byte, error) {
	if ctx == nil {
		return nil, [32]byte{}, ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, [32]byte{}, err
	}
	statement, err := DecodeNetworkWalletMappingStatement(original.Message)
	if err != nil {
		return nil, [32]byte{}, err
	}
	if err := verifyWalletMappingColdkeySignature(statement.Coldkey, original); err != nil {
		return nil, [32]byte{}, err
	}
	raw, err := json.Marshal(original)
	if err != nil || len(raw) > MaxWalletMappingConsentBytes {
		return nil, [32]byte{}, errors.Join(ErrWalletMappingIntegrity, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, [32]byte{}, err
	}
	return statement, sha256.Sum256(raw), nil
}

// The independently approved network head and the earning epoch it is read at.
type NetworkWalletMappingHistoryExpectation struct {
	Domain     ClientKeyHistoryDomain
	NetworkId  [16]byte
	HeadHash   [32]byte
	Generation uint64
	Epoch      uint64
}

// The network consent effective at the expected epoch, with the pinned head.
type VerifiedNetworkWalletMapping struct {
	Statement    NetworkWalletMappingStatement
	OriginalHash [32]byte
	HeadHash     [32]byte
	Generation   uint64
}

// Reconstruct the complete network lineage through the pinned head, then
// select the last consent effective at the epoch. A verified chain with no
// effective consent returns ErrWalletMappingNotEffective joined with
// ErrWalletMappingUnavailable; missing originals return only the latter.
func VerifyNetworkWalletMappingHistory(ctx context.Context, originals []WalletMappingConsent, expected NetworkWalletMappingHistoryExpectation) (*VerifiedNetworkWalletMapping, error) {
	if ctx == nil || expected.HeadHash == ([32]byte{}) || expected.Generation == 0 || len(originals) == 0 {
		return nil, ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(originals) > MaxWalletMappingHistory || expected.Generation > MaxWalletMappingHistory {
		return nil, ErrWalletMappingCapacity
	}
	if err := expected.Domain.Validate(); err != nil || expected.NetworkId == ([16]byte{}) || uint64(len(originals)) > expected.Generation {
		return nil, errors.Join(ErrWalletMappingIntegrity, err)
	}
	if uint64(len(originals)) < expected.Generation {
		return nil, ErrWalletMappingUnavailable
	}
	var previous [32]byte
	var previousEpoch uint64
	var selected *VerifiedNetworkWalletMapping
	for index, original := range originals {
		statement, hash, err := VerifyNetworkWalletMappingConsent(ctx, original)
		if err != nil {
			return nil, err
		}
		if statement.Domain != expected.Domain || statement.NetworkId != expected.NetworkId || statement.Generation != uint64(index)+1 || statement.PreviousHash != previous || index > 0 && statement.FromEpoch <= previousEpoch {
			return nil, ErrWalletMappingIntegrity
		}
		previous, previousEpoch = hash, statement.FromEpoch
		if statement.FromEpoch <= expected.Epoch {
			selected = &VerifiedNetworkWalletMapping{Statement: *statement, OriginalHash: hash, HeadHash: expected.HeadHash, Generation: expected.Generation}
		}
	}
	if previous != expected.HeadHash {
		return nil, ErrWalletMappingIntegrity
	}
	if selected == nil || selected.Statement.ThroughEpoch < expected.Epoch {
		return nil, errors.Join(ErrWalletMappingUnavailable, ErrWalletMappingNotEffective)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return selected, nil
}

// The same earning gate as a provider consent: the exact operator signer, an
// issuance boundary before the window and an acceptance that expired before
// the window started. A consent that may have been accepted inside the window
// is unknown, not effective.
func VerifyProspectiveNetworkWalletMapping(ctx context.Context, mapping *VerifiedNetworkWalletMapping, signer common.Address, startBlock uint64, startUnix int64) error {
	if ctx == nil || mapping == nil || signer == (common.Address{}) || startBlock == 0 || startUnix <= 0 {
		return ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	statement := mapping.Statement
	if err := statement.VerifyProspectiveSignature(); err != nil {
		return err
	}
	if statement.Prospective.Signer != signer || statement.Prospective.Boundary.Block >= startBlock || statement.FromEpoch <= statement.Prospective.Boundary.Epoch {
		return ErrWalletMappingIntegrity
	}
	if statement.ExpiresAt > startUnix {
		return ErrWalletMappingUnavailable
	}
	return ctx.Err()
}

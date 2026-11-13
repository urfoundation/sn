// Hotkey network delegation: the network consent with the hotkey in place of
// the coldkey. The hotkey signs once per (operator domain, network) that the
// network earns to the global hotkey consent head the statement pins, and the
// operator co-signs the issuance boundary before the hotkey signs, exactly as
// for a network consent. It is a separate chain per (domain, network). The
// hotkey may change between generations: the network owner may move the
// network to another hotkey, and the new hotkey signs. The first line, the
// schema and the scope keep it apart from the provider, network and global
// hotkey kinds.
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

const HotkeyNetworkDelegationSchema = "urnetwork-hotkey-network-delegation-v1"
const HotkeyNetworkDelegationPrefix = "Delegate URnetwork network wallet to hotkey\n"

// the only scope a delegation statement carries
const HotkeyNetworkDelegationScope = "hotkey-network"

// The displayed delegation. It names the authenticated network owner, the
// network, the hotkey and the global consent head, and no coldkey or client.
type HotkeyNetworkDelegationStatement struct {
	Schema            string                           `json:"schema"`
	Scope             string                           `json:"scope"`
	Domain            ClientKeyHistoryDomain           `json:"domain"`
	UserId            [16]byte                         `json:"user_id"`
	NetworkId         [16]byte                         `json:"network_id"`
	Hotkey            [32]byte                         `json:"hotkey"`
	ConsentHeadHash   [32]byte                         `json:"consent_head_hash"`
	ConsentGeneration uint64                           `json:"consent_generation"`
	Generation        uint64                           `json:"generation"`
	PreviousHash      [32]byte                         `json:"previous_hash"`
	Nonce             [32]byte                         `json:"nonce"`
	IssuedAt          int64                            `json:"issued_at"`
	ExpiresAt         int64                            `json:"expires_at"`
	FromEpoch         uint64                           `json:"from_epoch"`
	ThroughEpoch      uint64                           `json:"through_epoch"`
	Prospective       WalletMappingProspectiveApproval `json:"prospective"`
}

// The identity, lineage, interval and acceptance window rules shared by every
// encoding and signing path. A pinned consent generation past the history
// bound names a chain that cannot exist.
func (self HotkeyNetworkDelegationStatement) validate() error {
	if err := self.Domain.Validate(); err != nil {
		return errors.Join(ErrWalletMappingIntegrity, err)
	}
	if self.Schema != HotkeyNetworkDelegationSchema || self.Scope != HotkeyNetworkDelegationScope || self.UserId == ([16]byte{}) || self.NetworkId == ([16]byte{}) || self.Hotkey == ([32]byte{}) || self.ConsentHeadHash == ([32]byte{}) || self.ConsentGeneration == 0 || self.ConsentGeneration > MaxWalletMappingHistory || self.Nonce == ([32]byte{}) || self.Generation == 0 || self.Generation > MaxWalletMappingHistory || (self.Generation == 1) != (self.PreviousHash == ([32]byte{})) || self.IssuedAt <= 0 || self.ExpiresAt <= self.IssuedAt || self.ExpiresAt-self.IssuedAt > 300 || self.ThroughEpoch < self.FromEpoch || self.ThroughEpoch-self.FromEpoch > 65535 {
		return ErrWalletMappingIntegrity
	}
	return nil
}

// Fixed canonical bytes make the displayed delegation and the retained
// signature the same object. The operator's prospective signature must
// already verify.
func (self HotkeyNetworkDelegationStatement) Message() (string, error) {
	if err := self.validate(); err != nil {
		return "", err
	}
	if err := self.VerifyProspectiveSignature(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw)+len(HotkeyNetworkDelegationPrefix) > MaxWalletMappingMessageBytes {
		return "", errors.Join(ErrWalletMappingIntegrity, err)
	}
	return HotkeyNetworkDelegationPrefix + string(raw), nil
}

// Every displayed field is covered by the operator signature before the
// hotkey signs those exact bytes. The schema inside the digest keeps it apart
// from the other kinds' operator digests.
func (self HotkeyNetworkDelegationStatement) prospectiveDigest() ([32]byte, error) {
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
// its private key; no public request can supply this key. Failure leaves the
// statement unchanged.
func SignProspectiveHotkeyNetworkDelegation(statement *HotkeyNetworkDelegationStatement, boundary ClientKeyEffectiveBoundary, key *ecdsa.PrivateKey) error {
	if statement == nil || !validClientKeyStatementSigner(key) {
		return ErrWalletMappingUnavailable
	}
	owned := *statement
	owned.Schema, owned.Scope = HotkeyNetworkDelegationSchema, HotkeyNetworkDelegationScope
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
func (self HotkeyNetworkDelegationStatement) VerifyProspectiveSignature() error {
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
// no other kind's statement decodes here.
func DecodeHotkeyNetworkDelegationStatement(message string) (*HotkeyNetworkDelegationStatement, error) {
	if len(message) > MaxWalletMappingMessageBytes || !bytes.HasPrefix([]byte(message), []byte(HotkeyNetworkDelegationPrefix)) {
		return nil, ErrWalletMappingIntegrity
	}
	raw := []byte(message[len(HotkeyNetworkDelegationPrefix):])
	if err := ValidateUniqueJsonKeys(raw); err != nil {
		return nil, errors.Join(ErrWalletMappingIntegrity, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result HotkeyNetworkDelegationStatement
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

// The original is a WalletMappingConsent whose Signature is the statement
// hotkey's, over the raw or <Bytes>-wrapped message. Returns the statement and
// the original's content hash.
func VerifyHotkeyNetworkDelegation(ctx context.Context, original WalletMappingConsent) (*HotkeyNetworkDelegationStatement, [32]byte, error) {
	if ctx == nil {
		return nil, [32]byte{}, ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, [32]byte{}, err
	}
	statement, err := DecodeHotkeyNetworkDelegationStatement(original.Message)
	if err != nil {
		return nil, [32]byte{}, err
	}
	// the coldkey verifier checks any sr25519 key's wallet transcripts
	if err := verifyWalletMappingColdkeySignature(statement.Hotkey, original); err != nil {
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

// The independently approved delegation head and the earning epoch it is read
// at.
type HotkeyNetworkDelegationHistoryExpectation struct {
	Domain     ClientKeyHistoryDomain
	NetworkId  [16]byte
	HeadHash   [32]byte
	Generation uint64
	Epoch      uint64
}

// The delegation effective at the expected epoch, with the pinned head.
type VerifiedHotkeyNetworkDelegation struct {
	Statement    HotkeyNetworkDelegationStatement
	OriginalHash [32]byte
	HeadHash     [32]byte
	Generation   uint64
}

// Reconstruct the complete delegation lineage through the pinned head, then
// select the last delegation effective at the epoch. A verified chain with no
// effective delegation returns ErrWalletMappingNotEffective joined with
// ErrWalletMappingUnavailable; missing originals return only the latter.
func VerifyHotkeyNetworkDelegationHistory(ctx context.Context, originals []WalletMappingConsent, expected HotkeyNetworkDelegationHistoryExpectation) (*VerifiedHotkeyNetworkDelegation, error) {
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
	var selected *VerifiedHotkeyNetworkDelegation
	for index, original := range originals {
		statement, hash, err := VerifyHotkeyNetworkDelegation(ctx, original)
		if err != nil {
			return nil, err
		}
		if statement.Domain != expected.Domain || statement.NetworkId != expected.NetworkId || statement.Generation != uint64(index)+1 || statement.PreviousHash != previous || index > 0 && statement.FromEpoch <= previousEpoch {
			return nil, ErrWalletMappingIntegrity
		}
		previous, previousEpoch = hash, statement.FromEpoch
		if statement.FromEpoch <= expected.Epoch {
			selected = &VerifiedHotkeyNetworkDelegation{Statement: *statement, OriginalHash: hash, HeadHash: expected.HeadHash, Generation: expected.Generation}
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

// The same earning gate as a network consent: the exact operator signer, an
// issuance boundary before the window and an acceptance that expired before
// the window started. A delegation that may have been accepted inside the
// window is unknown, not effective.
func VerifyProspectiveHotkeyNetworkDelegation(ctx context.Context, delegation *VerifiedHotkeyNetworkDelegation, signer common.Address, startBlock uint64, startUnix int64) error {
	if ctx == nil || delegation == nil || signer == (common.Address{}) || startBlock == 0 || startUnix <= 0 {
		return ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	statement := delegation.Statement
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

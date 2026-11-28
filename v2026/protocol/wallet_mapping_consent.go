// Wallet mapping consent signs the exact provider association. Login challenges
// remain a different domain and cannot establish this prospective mapping.
package protocol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
)

const WalletMappingConsentSchema = "urnetwork-provider-wallet-mapping-consent-v1"
const WalletMappingConsentPrefix = "Approve URnetwork provider wallet mapping\n"
const MaxWalletMappingMessageBytes = 8 * 1024
const MaxWalletMappingConsentBytes = 12 * 1024
const MaxWalletMappingHistory = 4096

var ErrWalletMappingUnavailable = errors.New("original wallet mapping authority is unavailable")
var ErrWalletMappingIntegrity = errors.New("original wallet mapping contradicts its approved identity")
var ErrWalletMappingCapacity = errors.New("original wallet mapping exceeds its finite history profile")

// Joined with ErrWalletMappingUnavailable: the roster pins no chain for this
// owner. Earning-wallet selection may fall back from an absent provider chain.
var ErrWalletMappingAbsent = errors.New("no wallet mapping chain is pinned for this owner")

// Joined with ErrWalletMappingUnavailable: the complete pinned chain verifies
// and no consent in it is effective at the epoch. This is the only verified
// outcome, besides absence, from which earning-wallet selection falls back.
var ErrWalletMappingNotEffective = errors.New("no wallet mapping consent is effective at the epoch")

// The caller signs both the new association and its exact retained predecessor.
// Acceptance expiry is distinct from the inclusive approved earning epochs.
type WalletMappingStatement struct {
	Schema       string                           `json:"schema"`
	Domain       ClientKeyHistoryDomain           `json:"domain"`
	UserId       [16]byte                         `json:"user_id"`
	ClientId     [16]byte                         `json:"client_id"`
	NetworkId    [16]byte                         `json:"network_id"`
	Coldkey      [32]byte                         `json:"coldkey"`
	Generation   uint64                           `json:"generation"`
	PreviousHash [32]byte                         `json:"previous_hash"`
	Nonce        [32]byte                         `json:"nonce"`
	IssuedAt     int64                            `json:"issued_at"`
	ExpiresAt    int64                            `json:"expires_at"`
	FromEpoch    uint64                           `json:"from_epoch"`
	ThroughEpoch uint64                           `json:"through_epoch"`
	Prospective  WalletMappingProspectiveApproval `json:"prospective,omitzero"`
}

// Fixed canonical bytes make the displayed approval and retained signature the
// same object; aliases, reordered JSON and generic login text are refused.
func (self WalletMappingStatement) Message() (string, error) {
	if err := self.Domain.Validate(); err != nil {
		return "", errors.Join(ErrWalletMappingIntegrity, err)
	}
	if self.Schema != WalletMappingConsentSchema && self.Schema != WalletMappingProspectiveSchema || self.UserId == ([16]byte{}) || self.ClientId == ([16]byte{}) || self.NetworkId == ([16]byte{}) || self.Coldkey == ([32]byte{}) || self.Nonce == ([32]byte{}) || self.Generation == 0 || (self.Generation == 1) != (self.PreviousHash == ([32]byte{})) || self.IssuedAt <= 0 || self.ExpiresAt <= self.IssuedAt || self.ExpiresAt-self.IssuedAt > 300 || self.ThroughEpoch < self.FromEpoch || self.ThroughEpoch-self.FromEpoch > 65535 {
		return "", ErrWalletMappingIntegrity
	}
	if self.Schema == WalletMappingConsentSchema {
		if self.Prospective != (WalletMappingProspectiveApproval{}) {
			return "", ErrWalletMappingIntegrity
		}
	} else if err := self.VerifyProspectiveSignature(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw)+len(WalletMappingConsentPrefix) > MaxWalletMappingMessageBytes {
		return "", errors.Join(ErrWalletMappingIntegrity, err)
	}
	return WalletMappingConsentPrefix + string(raw), nil
}

// Decoding does not normalize the signed message into another spelling.
func DecodeWalletMappingStatement(message string) (*WalletMappingStatement, error) {
	if len(message) > MaxWalletMappingMessageBytes || !bytes.HasPrefix([]byte(message), []byte(WalletMappingConsentPrefix)) {
		return nil, ErrWalletMappingIntegrity
	}
	raw := []byte(message[len(WalletMappingConsentPrefix):])
	if err := ValidateUniqueJsonKeys(raw); err != nil {
		return nil, errors.Join(ErrWalletMappingIntegrity, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result WalletMappingStatement
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

// Original signature bytes are never regenerated by an operator or validator.
type WalletMappingConsent struct {
	Message   string   `json:"message"`
	Signature [64]byte `json:"signature"`
}

// SignRaw wallets may use their standard Bytes wrapper; both transcripts bind
// this exact domain-specific message, never an unsigned association alongside it.
func VerifyWalletMappingConsent(ctx context.Context, original WalletMappingConsent) (*WalletMappingStatement, [32]byte, error) {
	if ctx == nil {
		return nil, [32]byte{}, ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, [32]byte{}, err
	}
	statement, err := DecodeWalletMappingStatement(original.Message)
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

// The coldkey's sr25519 signature in the substrate context over the exact
// message, raw or in the Bytes wrapper that SignRaw wallets apply.
func verifyWalletMappingColdkeySignature(coldkey [32]byte, original WalletMappingConsent) error {
	public := &schnorrkel.PublicKey{}
	if err := public.Decode(coldkey); err != nil {
		return errors.Join(ErrWalletMappingIntegrity, err)
	}
	signature := &schnorrkel.Signature{}
	if err := signature.Decode(original.Signature); err != nil {
		return errors.Join(ErrWalletMappingIntegrity, err)
	}
	for _, message := range []string{original.Message, "<Bytes>" + original.Message + "</Bytes>"} {
		ok, err := public.Verify(signature, schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
		if err == nil && ok {
			return nil
		}
	}
	return ErrWalletMappingIntegrity
}

// The selected head is independent approved window authority. Neither a SQL
// latest row nor an artifact-selected maximum generation can supply it.
type WalletMappingHistoryExpectation struct {
	Domain     ClientKeyHistoryDomain
	ClientId   [16]byte
	HeadHash   [32]byte
	Generation uint64
	Epoch      uint64
}

// The final original statement is retained even when a previous generation is
// effective at the selected epoch. Missing, expired and future mappings differ.
type VerifiedWalletMapping struct {
	Statement    WalletMappingStatement
	OriginalHash [32]byte
	HeadHash     [32]byte
	Generation   uint64
}

// Reconstruct the complete original lineage through the independently pinned
// head, then select the last consent effective at the approved earning epoch.
func VerifyWalletMappingHistory(ctx context.Context, originals []WalletMappingConsent, expected WalletMappingHistoryExpectation) (*VerifiedWalletMapping, error) {
	if ctx == nil || expected.HeadHash == ([32]byte{}) || expected.Generation == 0 || len(originals) == 0 {
		return nil, ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(originals) > MaxWalletMappingHistory || expected.Generation > MaxWalletMappingHistory {
		return nil, ErrWalletMappingCapacity
	}
	if err := expected.Domain.Validate(); err != nil || expected.ClientId == ([16]byte{}) || uint64(len(originals)) > expected.Generation {
		return nil, errors.Join(ErrWalletMappingIntegrity, err)
	}
	if uint64(len(originals)) < expected.Generation {
		return nil, ErrWalletMappingUnavailable
	}
	var previous [32]byte
	var previousEpoch uint64
	var selected *VerifiedWalletMapping
	for index, original := range originals {
		statement, hash, err := VerifyWalletMappingConsent(ctx, original)
		if err != nil {
			return nil, err
		}
		if statement.Domain != expected.Domain || statement.ClientId != expected.ClientId || statement.Generation != uint64(index)+1 || statement.PreviousHash != previous || index > 0 && statement.FromEpoch <= previousEpoch {
			return nil, ErrWalletMappingIntegrity
		}
		previous, previousEpoch = hash, statement.FromEpoch
		if statement.FromEpoch <= expected.Epoch {
			selected = &VerifiedWalletMapping{Statement: *statement, OriginalHash: hash, HeadHash: expected.HeadHash, Generation: expected.Generation}
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

// Global hotkey wallet mapping consent: the coldkey and the hotkey each sign
// the same message once, and it names the coldkey that earns for the hotkey's
// networks on every operator of one subnet. It is a chain per (subnet, hotkey)
// that no operator signs and that carries no acceptance expiry. It is not
// prospective by itself: an operator's network earns to it only through a
// hotkey network delegation, which pins one head of this chain and carries the
// operator's prospective approval. The first line, the schema and the scope
// keep it apart from the provider, network and delegation kinds.
package protocol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
)

const HotkeyWalletMappingConsentSchema = "urnetwork-hotkey-wallet-mapping-consent-v1"
const HotkeyWalletMappingConsentPrefix = "Approve URnetwork hotkey wallet mapping\n"

// the only scope a global hotkey statement carries
const HotkeyWalletMappingScope = "hotkey"

// The subnet the consent applies to, on every operator of it.
type HotkeyWalletMappingSubnet struct {
	ChainID     uint64   `json:"chain_id"`
	GenesisHash [32]byte `json:"genesis_hash"`
	Netuid      uint16   `json:"netuid"`
}

// Every field is nonzero.
func (self HotkeyWalletMappingSubnet) Validate() error {
	if self.ChainID == 0 || self.GenesisHash == ([32]byte{}) || self.Netuid == 0 {
		return errors.New("hotkey wallet mapping subnet is incomplete")
	}
	return nil
}

// The part of an operator domain that every operator of the subnet shares.
func (self ClientKeyHistoryDomain) HotkeySubnet() HotkeyWalletMappingSubnet {
	return HotkeyWalletMappingSubnet{ChainID: self.ChainID, GenesisHash: self.GenesisHash, Netuid: self.Netuid}
}

// The displayed global statement. It names no operator, user, network or
// client.
type HotkeyWalletMappingStatement struct {
	Schema       string                    `json:"schema"`
	Scope        string                    `json:"scope"`
	Subnet       HotkeyWalletMappingSubnet `json:"subnet"`
	Hotkey       [32]byte                  `json:"hotkey"`
	Coldkey      [32]byte                  `json:"coldkey"`
	Generation   uint64                    `json:"generation"`
	PreviousHash [32]byte                  `json:"previous_hash"`
	Nonce        [32]byte                  `json:"nonce"`
	IssuedAt     int64                     `json:"issued_at"`
	FromEpoch    uint64                    `json:"from_epoch"`
	ThroughEpoch uint64                    `json:"through_epoch"`
}

// The identity, lineage and interval rules shared by every encoding path.
func (self HotkeyWalletMappingStatement) validate() error {
	if err := self.Subnet.Validate(); err != nil {
		return errors.Join(ErrWalletMappingIntegrity, err)
	}
	if self.Schema != HotkeyWalletMappingConsentSchema || self.Scope != HotkeyWalletMappingScope || self.Hotkey == ([32]byte{}) || self.Coldkey == ([32]byte{}) || self.Nonce == ([32]byte{}) || self.Generation == 0 || self.Generation > MaxWalletMappingHistory || (self.Generation == 1) != (self.PreviousHash == ([32]byte{})) || self.IssuedAt <= 0 || self.ThroughEpoch < self.FromEpoch || self.ThroughEpoch-self.FromEpoch > 65535 {
		return ErrWalletMappingIntegrity
	}
	return nil
}

// Fixed canonical bytes make the displayed approval and both retained
// signatures the same object.
func (self HotkeyWalletMappingStatement) Message() (string, error) {
	if err := self.validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw)+len(HotkeyWalletMappingConsentPrefix) > MaxWalletMappingMessageBytes {
		return "", errors.Join(ErrWalletMappingIntegrity, err)
	}
	return HotkeyWalletMappingConsentPrefix + string(raw), nil
}

// Decoding does not normalize the signed message into another spelling, and
// no other kind's statement decodes here.
func DecodeHotkeyWalletMappingStatement(message string) (*HotkeyWalletMappingStatement, error) {
	if len(message) > MaxWalletMappingMessageBytes || !bytes.HasPrefix([]byte(message), []byte(HotkeyWalletMappingConsentPrefix)) {
		return nil, ErrWalletMappingIntegrity
	}
	raw := []byte(message[len(HotkeyWalletMappingConsentPrefix):])
	if err := ValidateUniqueJsonKeys(raw); err != nil {
		return nil, errors.Join(ErrWalletMappingIntegrity, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result HotkeyWalletMappingStatement
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

// Both sr25519 signatures in the substrate context over the exact message, raw
// or <Bytes>-wrapped, each made by the statement's own key.
type HotkeyWalletMappingConsent struct {
	Message          string   `json:"message"`
	ColdkeySignature [64]byte `json:"coldkey_signature"`
	HotkeySignature  [64]byte `json:"hotkey_signature"`
}

// The retained original and its content hash, after the coldkey's and the
// hotkey's signatures over the exact message both verify.
func VerifyHotkeyWalletMappingConsent(ctx context.Context, original HotkeyWalletMappingConsent) (*HotkeyWalletMappingStatement, [32]byte, error) {
	if ctx == nil {
		return nil, [32]byte{}, ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, [32]byte{}, err
	}
	statement, err := DecodeHotkeyWalletMappingStatement(original.Message)
	if err != nil {
		return nil, [32]byte{}, err
	}
	// the coldkey verifier checks any sr25519 key's wallet transcripts
	if err := verifyWalletMappingColdkeySignature(statement.Coldkey, WalletMappingConsent{Message: original.Message, Signature: original.ColdkeySignature}); err != nil {
		return nil, [32]byte{}, err
	}
	if err := verifyWalletMappingColdkeySignature(statement.Hotkey, WalletMappingConsent{Message: original.Message, Signature: original.HotkeySignature}); err != nil {
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

// Walks the complete lineage from generation 1: every original verifies, each
// links the hash of the one before it with a strictly later FromEpoch, and the
// subnet and hotkey never change. The coldkey may change, since replacement is
// how a coldkey changes. visit, when set, sees each statement and its original
// hash in order. Returns the head statement and its hash.
func verifyHotkeyWalletMappingChain(ctx context.Context, originals []HotkeyWalletMappingConsent, visit func(*HotkeyWalletMappingStatement, [32]byte)) (*HotkeyWalletMappingStatement, [32]byte, error) {
	var head *HotkeyWalletMappingStatement
	var previous [32]byte
	for index, original := range originals {
		statement, hash, err := VerifyHotkeyWalletMappingConsent(ctx, original)
		if err != nil {
			return nil, [32]byte{}, err
		}
		if statement.Generation != uint64(index)+1 || statement.PreviousHash != previous || head != nil && (statement.Subnet != head.Subnet || statement.Hotkey != head.Hotkey || statement.FromEpoch <= head.FromEpoch) {
			return nil, [32]byte{}, ErrWalletMappingIntegrity
		}
		if visit != nil {
			visit(statement, hash)
		}
		head, previous = statement, hash
	}
	return head, previous, nil
}

// Complete lineage from generation 1; returns the head statement and hash.
// Used when accepting and storing a chain, where no head is pinned yet.
func VerifyHotkeyWalletMappingLineage(ctx context.Context, originals []HotkeyWalletMappingConsent) (*HotkeyWalletMappingStatement, [32]byte, error) {
	if ctx == nil || len(originals) == 0 {
		return nil, [32]byte{}, ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, [32]byte{}, err
	}
	if len(originals) > MaxWalletMappingHistory {
		return nil, [32]byte{}, ErrWalletMappingCapacity
	}
	head, headHash, err := verifyHotkeyWalletMappingChain(ctx, originals, nil)
	if err != nil {
		return nil, [32]byte{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, [32]byte{}, err
	}
	return head, headHash, nil
}

// The independently pinned head of one hotkey's chain and the earning epoch it
// is read at. A delegation supplies the head, never a SQL latest row.
type HotkeyWalletMappingHistoryExpectation struct {
	Subnet     HotkeyWalletMappingSubnet
	Hotkey     [32]byte
	HeadHash   [32]byte
	Generation uint64
	Epoch      uint64
}

// The global consent effective at the expected epoch, with the pinned head.
type VerifiedHotkeyWalletMapping struct {
	Statement    HotkeyWalletMappingStatement
	OriginalHash [32]byte
	HeadHash     [32]byte
	Generation   uint64
}

// Reconstruct the complete lineage through the pinned head, then select the
// last consent effective at the epoch. A verified chain with no effective
// consent returns ErrWalletMappingNotEffective joined with
// ErrWalletMappingUnavailable; missing originals return only the latter.
func VerifyHotkeyWalletMappingHistory(ctx context.Context, originals []HotkeyWalletMappingConsent, expected HotkeyWalletMappingHistoryExpectation) (*VerifiedHotkeyWalletMapping, error) {
	if ctx == nil || expected.HeadHash == ([32]byte{}) || expected.Generation == 0 || len(originals) == 0 {
		return nil, ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(originals) > MaxWalletMappingHistory || expected.Generation > MaxWalletMappingHistory {
		return nil, ErrWalletMappingCapacity
	}
	if err := expected.Subnet.Validate(); err != nil || expected.Hotkey == ([32]byte{}) || uint64(len(originals)) > expected.Generation {
		return nil, errors.Join(ErrWalletMappingIntegrity, err)
	}
	if uint64(len(originals)) < expected.Generation {
		return nil, ErrWalletMappingUnavailable
	}
	var selected *VerifiedHotkeyWalletMapping
	head, headHash, err := verifyHotkeyWalletMappingChain(ctx, originals, func(statement *HotkeyWalletMappingStatement, hash [32]byte) {
		if statement.FromEpoch <= expected.Epoch {
			selected = &VerifiedHotkeyWalletMapping{Statement: *statement, OriginalHash: hash, HeadHash: expected.HeadHash, Generation: expected.Generation}
		}
	})
	if err != nil {
		return nil, err
	}
	// every generation shares the head's subnet and hotkey
	if head.Subnet != expected.Subnet || head.Hotkey != expected.Hotkey || headHash != expected.HeadHash {
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

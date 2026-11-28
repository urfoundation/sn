// Actual coldkey signatures exercise exact mapping domains and complete
// independently pinned histories, including absence and rotation boundaries.
package protocol

import (
	"context"
	"errors"
	"strings"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
)

// Synthetic wallet keys sign exactly the same substrate transcripts as wallets.
func walletMappingTestOriginal(t testing.TB, statement *WalletMappingStatement, seed byte, wrapped bool) (WalletMappingConsent, [32]byte) {
	t.Helper()
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	statement.Coldkey = mini.Public().Encode()
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	payload := message
	if wrapped {
		payload = "<Bytes>" + message + "</Bytes>"
	}
	signature, err := mini.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(payload)))
	if err != nil {
		t.Fatal(err)
	}
	original := WalletMappingConsent{Message: message, Signature: signature.Encode()}
	_, hash, err := VerifyWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hash
}

// Every field is visibly synthetic; epoch zero is an ordinary approved epoch.
func walletMappingTestStatement() WalletMappingStatement {
	return WalletMappingStatement{Schema: WalletMappingConsentSchema, Domain: ClientKeyHistoryDomain{ChainID: 964, GenesisHash: [32]byte{1}, Netuid: 25, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6}, UserId: [16]byte{7}, ClientId: [16]byte{8}, NetworkId: [16]byte{9}, Nonce: [32]byte{10}, Generation: 1, IssuedAt: 1000, ExpiresAt: 1300, FromEpoch: 0, ThroughEpoch: 100}
}

// Acceptance expiry does not erase an original wallet approval during replay.
func TestWalletMappingOriginalRawAndWrappedSignatures(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		statement := walletMappingTestStatement()
		original, hash := walletMappingTestOriginal(t, &statement, 11, wrapped)
		known, err := VerifyWalletMappingHistory(t.Context(), []WalletMappingConsent{original}, WalletMappingHistoryExpectation{Domain: statement.Domain, ClientId: statement.ClientId, HeadHash: hash, Generation: 1, Epoch: 0})
		if err != nil || known == nil || known.Statement != statement || known.OriginalHash != hash {
			t.Fatal("original mapping was not reconstructed", wrapped, known, err)
		}
	}
}

// Key possession does not authorize a different client/network/deployment or
// a changed interval, predecessor, authenticated user or nonce.
func TestWalletMappingSignatureBindsEveryAssociationField(t *testing.T) {
	statement := walletMappingTestStatement()
	original, _ := walletMappingTestOriginal(t, &statement, 11, false)
	for index, mutate := range []func(*WalletMappingStatement){
		func(s *WalletMappingStatement) { s.Domain.PolicyHash[0]++ },
		func(s *WalletMappingStatement) { s.Domain.NoID++ },
		func(s *WalletMappingStatement) { s.ClientId[0]++ },
		func(s *WalletMappingStatement) { s.NetworkId[0]++ },
		func(s *WalletMappingStatement) { s.UserId[0]++ },
		func(s *WalletMappingStatement) { s.Nonce[0]++ },
		func(s *WalletMappingStatement) { s.ThroughEpoch++ },
		func(s *WalletMappingStatement) { s.ExpiresAt-- },
	} {
		changed := statement
		mutate(&changed)
		message, err := changed.Message()
		if err != nil {
			t.Fatal(index, err)
		}
		value := original
		value.Message = message
		if result, _, err := VerifyWalletMappingConsent(t.Context(), value); result != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("changed association borrowed the original signature", index, err)
		}
	}
}

// The complete independently pinned head selects an epoch; a supplied older
// head cannot hide rotation, and a later effective consent cannot erase history.
func TestWalletMappingPinnedHistoryRetainsOriginalEpoch(t *testing.T) {
	first := walletMappingTestStatement()
	old, oldHash := walletMappingTestOriginal(t, &first, 11, false)
	second := first
	second.Generation, second.PreviousHash, second.FromEpoch = 2, oldHash, 10
	second.Nonce[0]++
	newer, newHash := walletMappingTestOriginal(t, &second, 12, true)
	expected := WalletMappingHistoryExpectation{Domain: first.Domain, ClientId: first.ClientId, HeadHash: newHash, Generation: 2, Epoch: 9}
	history := []WalletMappingConsent{old, newer}
	value, err := VerifyWalletMappingHistory(t.Context(), history, expected)
	if err != nil || value == nil || value.Statement.Coldkey != first.Coldkey || value.OriginalHash != oldHash {
		t.Fatal("later mapping rewrote original epoch", value, err)
	}
	expected.Epoch = 10
	value, err = VerifyWalletMappingHistory(t.Context(), history, expected)
	if err != nil || value == nil || value.Statement.Coldkey != second.Coldkey {
		t.Fatal("approved successor did not become effective", value, err)
	}
	for _, partial := range [][]WalletMappingConsent{{old}, {newer}, {newer, old}} {
		if value, err := VerifyWalletMappingHistory(t.Context(), partial, expected); value != nil || err == nil {
			t.Fatal("partial or reordered mapping lineage was admitted", err)
		}
	}
	expected.HeadHash = oldHash
	if value, err := VerifyWalletMappingHistory(t.Context(), history, expected); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("witness selected its own latest mapping", err)
	}
}

// Malformed aliases and generic login text cannot gain the mapping domain.
func TestWalletMappingMissingAndCanonicalRefusalsStayDistinct(t *testing.T) {
	statement := walletMappingTestStatement()
	original, hash := walletMappingTestOriginal(t, &statement, 11, false)
	for _, message := range []string{"Sign in to URnetwork\nChallenge: synthetic\nTimestamp: 1000", original.Message + " ", strings.Replace(original.Message, `"schema":`, `"extra":0,"schema":`, 1)} {
		if value, err := DecodeWalletMappingStatement(message); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("noncanonical mapping was normalized", err)
		}
	}
	expected := WalletMappingHistoryExpectation{Domain: statement.Domain, ClientId: statement.ClientId, Generation: 1, Epoch: 0}
	if value, err := VerifyWalletMappingHistory(t.Context(), []WalletMappingConsent{original}, expected); value != nil || !errors.Is(err, ErrWalletMappingUnavailable) {
		t.Fatal("missing independent head became authority", err)
	}
	expected.HeadHash, expected.Epoch = hash, statement.ThroughEpoch+1
	if value, err := VerifyWalletMappingHistory(t.Context(), []WalletMappingConsent{original}, expected); value != nil || !errors.Is(err, ErrWalletMappingUnavailable) {
		t.Fatal("expired epoch gained a mapping", err)
	}
}

// Cancellation erases a candidate instead of publishing a stale successful map.
func TestWalletMappingCanceledOwnerPublishesNoOriginal(t *testing.T) {
	statement := walletMappingTestStatement()
	original, hash := walletMappingTestOriginal(t, &statement, 11, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if value, err := VerifyWalletMappingHistory(ctx, []WalletMappingConsent{original}, WalletMappingHistoryExpectation{Domain: statement.Domain, ClientId: statement.ClientId, Generation: 1, HeadHash: hash}); value != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled mapping published authority", err)
	}
}

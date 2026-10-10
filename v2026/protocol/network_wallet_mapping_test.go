// Network consent signs one coldkey for every provider client of a network.
// These tests use synthetic operator and coldkey keys that sign the same
// substrate transcripts as wallets, and visibly synthetic identities.
package protocol

import (
	"crypto/ecdsa"
	"errors"
	"strings"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// The operator boundary every synthetic network consent is issued at.
var networkWalletTestBoundary = ClientKeyEffectiveBoundary{Epoch: 50, Block: 500, Hash: [32]byte{12}}

// The synthetic operator key and its address.
func networkWalletTestOperator(t testing.TB) (*ecdsa.PrivateKey, common.Address) {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	return key, crypto.PubkeyToAddress(key.PublicKey)
}

// Every field is visibly synthetic; the interval starts after the boundary.
func networkWalletTestStatement() NetworkWalletMappingStatement {
	return NetworkWalletMappingStatement{Domain: walletMappingTestStatement().Domain, UserId: [16]byte{7}, NetworkId: [16]byte{9}, Nonce: [32]byte{10}, Generation: 1, IssuedAt: 1000, ExpiresAt: 1300, FromEpoch: 51, ThroughEpoch: 151}
}

// Signs the statement as the operator and then as the coldkey with the given
// seed, raw or in the Bytes wrapper, and returns the original and its hash.
func networkWalletTestOriginal(t testing.TB, statement *NetworkWalletMappingStatement, seed byte, wrapped bool) (WalletMappingConsent, [32]byte) {
	t.Helper()
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	statement.Coldkey = mini.Public().Encode()
	operator, _ := networkWalletTestOperator(t)
	if err := SignProspectiveNetworkWalletMapping(statement, networkWalletTestBoundary, operator); err != nil {
		t.Fatal(err)
	}
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
	_, hash, err := VerifyNetworkWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hash
}

func TestNetworkWalletMappingRawAndWrappedSignatures(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		statement := networkWalletTestStatement()
		original, hash := networkWalletTestOriginal(t, &statement, 11, wrapped)
		if !strings.HasPrefix(original.Message, "Approve URnetwork network wallet mapping\n{") || !strings.Contains(original.Message, `"scope":"network"`) || strings.Contains(original.Message, "client_id") {
			t.Fatalf("network message does not state its scope: %q", original.Message)
		}
		mapping, err := VerifyNetworkWalletMappingHistory(t.Context(), []WalletMappingConsent{original}, NetworkWalletMappingHistoryExpectation{Domain: statement.Domain, NetworkId: statement.NetworkId, HeadHash: hash, Generation: 1, Epoch: 51})
		if err != nil || mapping == nil || mapping.Statement != statement || mapping.OriginalHash != hash {
			t.Fatal("network consent was not reconstructed", wrapped, mapping, err)
		}
	}
}

// A provider consent never verifies as a network consent, and the reverse.
func TestNetworkWalletMappingScopeCannotCrossProviderScope(t *testing.T) {
	providerOriginal, _, _ := walletProspectiveFixture(t)
	if value, err := DecodeNetworkWalletMappingStatement(providerOriginal.Message); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("provider statement decoded as network scope", err)
	}
	if value, _, err := VerifyNetworkWalletMappingConsent(t.Context(), providerOriginal); value != nil || err == nil {
		t.Fatal("provider original verified as network scope", err)
	}

	statement := networkWalletTestStatement()
	networkOriginal, _ := networkWalletTestOriginal(t, &statement, 11, false)
	if value, err := DecodeWalletMappingStatement(networkOriginal.Message); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("network statement decoded as provider scope", err)
	}
	if value, _, err := VerifyWalletMappingConsent(t.Context(), networkOriginal); value != nil || err == nil {
		t.Fatal("network original verified as provider scope", err)
	}

	// neither the first line nor the scope field can be exchanged
	body := strings.TrimPrefix(networkOriginal.Message, NetworkWalletMappingConsentPrefix)
	for _, message := range []string{
		WalletMappingConsentPrefix + body,
		NetworkWalletMappingConsentPrefix + strings.Replace(body, `"scope":"network"`, `"scope":"provider"`, 1),
		NetworkWalletMappingConsentPrefix + strings.Replace(body, `"scope":"network",`, ``, 1),
		NetworkWalletMappingConsentPrefix + strings.Replace(body, `"user_id":`, `"client_id":[8,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"user_id":`, 1),
		networkOriginal.Message + " ",
	} {
		if value, err := DecodeNetworkWalletMappingStatement(message); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatalf("altered network statement decoded: %q %v", message[:60], err)
		}
		if value, err := DecodeWalletMappingStatement(message); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatalf("altered network statement decoded as provider scope: %q %v", message[:60], err)
		}
	}
}

// The coldkey's signature covers the network, the user, the coldkey, the nonce,
// the interval and the acceptance window.
func TestNetworkWalletMappingSignatureBindsEveryField(t *testing.T) {
	statement := networkWalletTestStatement()
	original, _ := networkWalletTestOriginal(t, &statement, 11, false)
	operator, _ := networkWalletTestOperator(t)
	for index, mutate := range []func(*NetworkWalletMappingStatement){
		func(s *NetworkWalletMappingStatement) { s.NetworkId[0]++ },
		func(s *NetworkWalletMappingStatement) { s.UserId[0]++ },
		func(s *NetworkWalletMappingStatement) { s.Domain.NoID++ },
		func(s *NetworkWalletMappingStatement) { s.Nonce[0]++ },
		func(s *NetworkWalletMappingStatement) { s.ThroughEpoch++ },
		func(s *NetworkWalletMappingStatement) { s.ExpiresAt-- },
		func(s *NetworkWalletMappingStatement) { s.FromEpoch++ },
	} {
		changed := statement
		mutate(&changed)
		if err := SignProspectiveNetworkWalletMapping(&changed, networkWalletTestBoundary, operator); err != nil {
			t.Fatal(index, err)
		}
		message, err := changed.Message()
		if err != nil {
			t.Fatal(index, err)
		}
		value := original
		value.Message = message
		if result, _, err := VerifyNetworkWalletMappingConsent(t.Context(), value); result != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("changed network association borrowed the original signature", index, err)
		}
	}
	// the operator signature covers the displayed bytes too
	message := strings.Replace(original.Message, `"through_epoch":151`, `"through_epoch":152`, 1)
	if value, err := DecodeNetworkWalletMappingStatement(message); value != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("operator signature did not cover the interval", err)
	}
}

// Generations select by epoch through the pinned head; a verified chain with
// no effective consent is distinct from missing originals.
func TestNetworkWalletMappingHistorySelectsEffectiveConsent(t *testing.T) {
	first := networkWalletTestStatement()
	old, oldHash := networkWalletTestOriginal(t, &first, 11, false)
	second := networkWalletTestStatement()
	second.Generation, second.PreviousHash, second.FromEpoch, second.ThroughEpoch = 2, oldHash, 60, 70
	second.Nonce[0]++
	newer, newHash := networkWalletTestOriginal(t, &second, 12, true)
	history := []WalletMappingConsent{old, newer}
	expected := NetworkWalletMappingHistoryExpectation{Domain: first.Domain, NetworkId: first.NetworkId, HeadHash: newHash, Generation: 2}
	for _, c := range []struct {
		epoch   uint64
		coldkey [32]byte
	}{
		{epoch: 51, coldkey: first.Coldkey},
		{epoch: 59, coldkey: first.Coldkey},
		{epoch: 60, coldkey: second.Coldkey},
		{epoch: 70, coldkey: second.Coldkey},
	} {
		expected.Epoch = c.epoch
		mapping, err := VerifyNetworkWalletMappingHistory(t.Context(), history, expected)
		if err != nil || mapping == nil || mapping.Statement.Coldkey != c.coldkey || mapping.HeadHash != newHash || mapping.Generation != 2 {
			t.Fatal("network consent selection differs", c.epoch, mapping, err)
		}
	}
	// before the first consent and after the newest one ends: not effective
	for _, epoch := range []uint64{50, 71} {
		expected.Epoch = epoch
		if mapping, err := VerifyNetworkWalletMappingHistory(t.Context(), history, expected); mapping != nil || !errors.Is(err, ErrWalletMappingNotEffective) || !errors.Is(err, ErrWalletMappingUnavailable) {
			t.Fatal("ineffective network consent was selected", epoch, err)
		}
	}
	// missing originals are unavailable, never not effective
	expected.Epoch = 60
	if mapping, err := VerifyNetworkWalletMappingHistory(t.Context(), []WalletMappingConsent{old}, expected); mapping != nil || !errors.Is(err, ErrWalletMappingUnavailable) || errors.Is(err, ErrWalletMappingNotEffective) {
		t.Fatal("partial network lineage read as not effective", err)
	}
	for _, partial := range [][]WalletMappingConsent{{newer}, {newer, old}} {
		if mapping, err := VerifyNetworkWalletMappingHistory(t.Context(), partial, expected); mapping != nil || err == nil || errors.Is(err, ErrWalletMappingNotEffective) {
			t.Fatal("broken network lineage was admitted", err)
		}
	}
	expected.HeadHash = oldHash
	if mapping, err := VerifyNetworkWalletMappingHistory(t.Context(), history, expected); mapping != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a foreign head selected the network lineage", err)
	}
	expected.HeadHash, expected.NetworkId = newHash, [16]byte{10}
	if mapping, err := VerifyNetworkWalletMappingHistory(t.Context(), history, expected); mapping != nil || !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("another network's lineage was admitted", err)
	}
}

// The earning gate requires the operator signer and a boundary and acceptance
// before the window.
func TestNetworkWalletMappingProspectiveGate(t *testing.T) {
	statement := networkWalletTestStatement()
	original, hash := networkWalletTestOriginal(t, &statement, 11, false)
	mapping, err := VerifyNetworkWalletMappingHistory(t.Context(), []WalletMappingConsent{original}, NetworkWalletMappingHistoryExpectation{Domain: statement.Domain, NetworkId: statement.NetworkId, HeadHash: hash, Generation: 1, Epoch: 51})
	if err != nil {
		t.Fatal(err)
	}
	_, signer := networkWalletTestOperator(t)
	if err := VerifyProspectiveNetworkWalletMapping(t.Context(), mapping, signer, 510, 1400); err != nil {
		t.Fatal("approved network consent was refused", err)
	}
	if err := VerifyProspectiveNetworkWalletMapping(t.Context(), mapping, common.Address{99}, 510, 1400); !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a foreign operator signer was accepted", err)
	}
	if err := VerifyProspectiveNetworkWalletMapping(t.Context(), mapping, signer, 500, 1400); !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("a boundary inside the window was accepted", err)
	}
	if err := VerifyProspectiveNetworkWalletMapping(t.Context(), mapping, signer, 510, 1299); !errors.Is(err, ErrWalletMappingUnavailable) || errors.Is(err, ErrWalletMappingNotEffective) {
		t.Fatal("an acceptance that may fall inside the window became known", err)
	}
}

// A provider chain with no consent at the epoch reports not effective;
// missing provider originals stay plain unavailable.
func TestWalletMappingHistoryReportsNotEffective(t *testing.T) {
	statement := walletMappingTestStatement()
	statement.FromEpoch = 10
	original, hash := walletMappingTestOriginal(t, &statement, 11, false)
	expected := WalletMappingHistoryExpectation{Domain: statement.Domain, ClientId: statement.ClientId, HeadHash: hash, Generation: 1}
	for _, epoch := range []uint64{9, statement.ThroughEpoch + 1} {
		expected.Epoch = epoch
		if mapping, err := VerifyWalletMappingHistory(t.Context(), []WalletMappingConsent{original}, expected); mapping != nil || !errors.Is(err, ErrWalletMappingNotEffective) || !errors.Is(err, ErrWalletMappingUnavailable) {
			t.Fatal("ineffective provider consent was not reported", epoch, err)
		}
	}
	expected.Epoch, expected.Generation = 10, 2
	if mapping, err := VerifyWalletMappingHistory(t.Context(), []WalletMappingConsent{original}, expected); mapping != nil || !errors.Is(err, ErrWalletMappingUnavailable) || errors.Is(err, ErrWalletMappingNotEffective) {
		t.Fatal("missing provider originals read as not effective", err)
	}
}

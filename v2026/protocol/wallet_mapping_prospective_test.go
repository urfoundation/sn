// Original issuance authority cannot be invented by a wallet or backdated
// across its actual finalized epoch. Legacy signatures retain their own scope.
package protocol

import (
	"errors"
	"strings"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Both independent signatures cover the same exact displayed message.
func walletProspectiveFixture(t testing.TB) (WalletMappingConsent, *VerifiedWalletMapping, common.Address) {
	t.Helper()
	statement := walletMappingTestStatement()
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{11})
	if err != nil {
		t.Fatal(err)
	}
	statement.Coldkey, statement.FromEpoch = mini.Public().Encode(), 51
	key, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := SignProspectiveWalletMapping(&statement, ClientKeyEffectiveBoundary{Epoch: 50, Block: 500, Hash: [32]byte{12}}, key); err != nil {
		t.Fatal(err)
	}
	original, hash := walletMappingTestOriginal(t, &statement, 11, false)
	value, err := VerifyWalletMappingHistory(t.Context(), []WalletMappingConsent{original}, WalletMappingHistoryExpectation{Domain: statement.Domain, ClientId: statement.ClientId, HeadHash: hash, Generation: 1, Epoch: 51})
	if err != nil {
		t.Fatal(err)
	}
	return original, value, crypto.PubkeyToAddress(key.PublicKey)
}

// The exact independent signer and original earning boundary are required;
// possession of the wallet key alone is insufficient to claim past earnings.
func TestWalletProspectiveOriginalRequiresPriorApprovedBoundary(t *testing.T) {
	_, value, signer := walletProspectiveFixture(t)
	if err := VerifyProspectiveWalletMapping(t.Context(), value, signer, 510, 1400); err != nil {
		t.Fatal("actual prospective original was refused", err)
	}
	for _, bad := range []struct {
		signer common.Address
		block  uint64
		clock  int64
	}{{signer: common.Address{99}, block: 510, clock: 1400}, {signer: signer, block: 500, clock: 1400}} {
		if err := VerifyProspectiveWalletMapping(t.Context(), value, bad.signer, bad.block, bad.clock); !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("foreign or late original became historical consent", bad, err)
		}
	}
}

// A new original generation at epoch50 cannot introduce a mapping at epoch1,
// even when the caller supplies an increasing predecessor generation.
func TestWalletProspectiveSignerRefusesBackdatedGeneration(t *testing.T) {
	_, value, _ := walletProspectiveFixture(t)
	statement := value.Statement
	statement.Generation, statement.PreviousHash, statement.FromEpoch = 2, value.OriginalHash, 1
	statement.Nonce[0]++
	before := statement
	key, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := SignProspectiveWalletMapping(&statement, ClientKeyEffectiveBoundary{Epoch: 50, Block: 501, Hash: [32]byte{13}}, key); !errors.Is(err, ErrWalletMappingIntegrity) || statement != before {
		t.Fatal("backdated generation acquired operator approval or partial mutation", err)
	}
}

// A wallet may sign a changed string, but cannot recreate the independent
// operator's original nonce/interval/boundary approval inside that string.
func TestWalletProspectiveFieldsCannotBorrowOperatorSignature(t *testing.T) {
	_, value, _ := walletProspectiveFixture(t)
	for index, mutate := range []func(*WalletMappingStatement){
		func(s *WalletMappingStatement) { s.FromEpoch++ },
		func(s *WalletMappingStatement) { s.Nonce[0]++ },
		func(s *WalletMappingStatement) { s.Prospective.Boundary.Hash[0]++ },
		func(s *WalletMappingStatement) { s.NetworkId[0]++ },
		func(s *WalletMappingStatement) { s.IssuedAt-- },
	} {
		statement := value.Statement
		mutate(&statement)
		if _, err := statement.Message(); !errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("modified original borrowed operator signature", index, err)
		}
	}
}

// The v1 signature domain stays usable for its historical key-possession
// meaning, but cannot silently enter the new prospective measurement gate.
func TestWalletProspectiveGatePreservesLegacyUnknown(t *testing.T) {
	statement := walletMappingTestStatement()
	original, hash := walletMappingTestOriginal(t, &statement, 11, false)
	value, err := VerifyWalletMappingHistory(t.Context(), []WalletMappingConsent{original}, WalletMappingHistoryExpectation{Domain: statement.Domain, ClientId: statement.ClientId, HeadHash: hash, Generation: 1, Epoch: 0})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(original.Message, "prospective") {
		t.Fatal("legacy signed bytes acquired a new field")
	}
	if err := VerifyProspectiveWalletMapping(t.Context(), value, common.Address{1}, 510, 1400); !errors.Is(err, ErrWalletMappingUnavailable) {
		t.Fatal("legacy wallet possession became historical measurement authority", err)
	}
}

// An original challenge may be accepted immediately before the next earning
// boundary while its remaining expiry crosses that boundary. The challenge
// alone cannot prove the acceptance time, so that valid case remains unknown.
func TestWalletProspectiveCrossingExpiryLeavesAcceptanceUnknown(t *testing.T) {
	_, value, signer := walletProspectiveFixture(t)
	for _, start := range []int64{1002, 1299} {
		if err := VerifyProspectiveWalletMapping(t.Context(), value, signer, 510, start); !errors.Is(err, ErrWalletMappingUnavailable) || errors.Is(err, ErrWalletMappingIntegrity) {
			t.Fatal("possible original early acceptance became corruption or authority", start, err)
		}
	}
	if err := VerifyProspectiveWalletMapping(t.Context(), value, signer, 510, 1300); err != nil {
		t.Fatal("complete pre-window acceptance interval was refused", err)
	}
	if err := VerifyProspectiveWalletMapping(t.Context(), value, common.Address{99}, 510, 1002); !errors.Is(err, ErrWalletMappingIntegrity) {
		t.Fatal("unknown time hid foreign original authority", err)
	}
}

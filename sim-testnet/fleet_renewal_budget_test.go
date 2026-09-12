package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestFleetRenewalBudgetAccountsAllSignedAttemptsAndNonceGaps(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.Repeat("7", 64))
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey)
	chain := big.NewInt(945)
	roles := &RoleSecrets{EVM: map[string]EVMRoleSecret{"keeper": {Address: address.Hex()}}}
	base := &SetupPlan{ChainID: 945, PlanHash: common.Hash{0x41}.Hex(), Actions: []Action{{ID: "source", Kind: "evm-transaction", IntentHash: "intent", AcceptedPriorIntentHashes: []string{"old-intent"}}}}
	stateDir := filepath.Join(t.TempDir(), "state")
	sign := func(nonce, gas, fee, value uint64, chainID *big.Int) (*ethTypes.Transaction, string) {
		t.Helper()
		tx, err := ethTypes.SignTx(ethTypes.NewTx(&ethTypes.DynamicFeeTx{ChainID: chainID, Nonce: nonce, GasTipCap: new(big.Int), GasFeeCap: new(big.Int).SetUint64(fee), Gas: gas, To: &address, Value: new(big.Int).SetUint64(value)}), ethTypes.LatestSignerForChainID(chainID), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(stateDir, "transactions", stringsTrim0x(tx.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return tx, "0x" + hex.EncodeToString(raw)
	}
	first, firstRaw := sign(0, 21000, 10, 0, chain)
	second, secondRaw := sign(1, 22000, 12, 0, chain)
	_, claimRaw := sign(2, 23000, 13, 7, chain)
	_, futureRaw := sign(4, 24000, 14, 0, chain)
	entries := []JournalEntry{{PlanHash: base.PlanHash, ActionID: "source", IntentHash: "old-intent", Signer: address.Hex(), Nonce: "0", TransactionHash: first.Hash().Hex()}, {PlanHash: base.PlanHash, ActionID: "source", IntentHash: "intent", Signer: address.Hex(), Nonce: "1", TransactionHash: second.Hash().Hex()}}
	inputs, err := canonicalFleetRenewalTransactions([]string{firstRaw, secondRaw, claimRaw, claimRaw, futureRaw})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 4 {
		t.Fatal("exact duplicate signed transactions were not deduplicated")
	}
	exposure, err := fleetRenewalCampaignExposure(stateDir, base, entries, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if exposure.Liability != "899007" {
		t.Fatalf("signed liabilities=%s, want source replacement + claim + future signed attempt", exposure.Liability)
	}
	points := []FleetRenewalNonce{{Role: "keeper", Address: address, Finalized: 3, Latest: 3, Pending: 3}}
	if err := validateFleetRenewalNonceCoverage(roles, exposure, points); err != nil {
		t.Fatal(err)
	}
	points[0].Pending = 5
	if err := validateFleetRenewalNonceCoverage(roles, exposure, points); err == nil || !strings.Contains(err.Error(), "role keeper nonce 3") {
		t.Fatalf("missing owned nonce was not diagnosed exactly: %v", err)
	}
	points[0].Pending = 3
	points[0].Address = common.Address{1}
	if err := validateFleetRenewalNonceCoverage(roles, exposure, points); err == nil {
		t.Fatal("changed role custody admitted")
	}
	_, wrongChain := sign(3, 21000, 10, 0, big.NewInt(1))
	if _, err := fleetRenewalCampaignExposure(stateDir, base, entries, []string{wrongChain}); err == nil {
		t.Fatal("foreign chain liability admitted")
	}
	entries[0].Nonce = "99"
	if _, err := fleetRenewalCampaignExposure(stateDir, base, entries, inputs); err == nil {
		t.Fatal("journal nonce did not authenticate signed bytes")
	}
}

func TestFleetRenewalBudgetDoesNotChargeRetiredGasTwice(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	source := fixture.base
	action, err := exactPlanActionByID(source, "evm.reserve-sink")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(fixture.stateDir, "plans", stringsTrim0x(source.PlanHash)+".json")
	if err := atomicWrite(archive, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	base := &SetupPlan{ChainID: source.ChainID, PlanHash: common.Hash{0x76}.Hex(), PriorPlanHashes: []string{source.PlanHash}, SupersededSpend: Spend{EVMGasWei: "500000"}}
	key, err := crypto.HexToECDSA(strings.Repeat("9", 64))
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey)
	chain := new(big.Int).SetUint64(source.ChainID)
	var entries []JournalEntry
	for nonce := uint64(0); nonce < 3; nonce++ {
		tx, err := ethTypes.SignTx(ethTypes.NewTx(&ethTypes.DynamicFeeTx{ChainID: chain, Nonce: nonce, GasTipCap: new(big.Int), GasFeeCap: big.NewInt(10), Gas: 21000, To: &address, Value: new(big.Int)}), ethTypes.LatestSignerForChainID(chain), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(fixture.stateDir, "transactions", stringsTrim0x(tx.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		entry := JournalEntry{PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Signer: address.Hex(), Nonce: fmt.Sprint(nonce), TransactionHash: tx.Hash().Hex(), Stage: StageBroadcast}
		if nonce == 2 {
			entry.ActionID = "repair.outside-source-plan"
		}
		entries = append(entries, entry)
		if nonce != 1 {
			entry.Stage = StageFinalized
			entries = append(entries, entry)
			entry.Stage = StageVerified
			entries = append(entries, entry)
		}
	}
	exposure, err := fleetRenewalCampaignExposure(fixture.stateDir, base, entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if exposure.SupersededCredit != "210000" || exposure.Liability != "420000" {
		t.Fatalf("retired gas credit=%s campaign=%s, pending/recovery must remain charged", exposure.SupersededCredit, exposure.Liability)
	}
	base.SupersededSpend.EVMGasWei = "100000"
	exposure, err = fleetRenewalCampaignExposure(fixture.stateDir, base, entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if exposure.SupersededCredit != "100000" || exposure.Liability != "530000" {
		t.Fatal("retired credit exceeded its already reserved allowance")
	}
	if err := os.WriteFile(archive, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fleetRenewalCampaignExposure(fixture.stateDir, base, entries, nil); err == nil {
		t.Fatal("unauthenticated ancestor received a gas credit")
	}
}

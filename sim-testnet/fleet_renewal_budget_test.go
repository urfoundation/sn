package main

import (
	"encoding/hex"
	"math/big"
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

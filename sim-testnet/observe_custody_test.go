package main

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/ss58"
)

func contractCustodyBatchFixture(t *testing.T, cfg *ResolvedConfig, deployment *ContractDeployment) map[string][]any {
	t.Helper()
	coordinatorSelf := ss58.EvmMirrorPubkey(deployment.CoordinatorProxy)
	vaultSelf := ss58.EvmMirrorPubkey(deployment.SettlementVault)
	reserveSelf := ss58.EvmMirrorPubkey(deployment.ReserveSink)
	escrowHotkey := [32]byte{1, 2, 3}
	reserveHotkey := [32]byte{4, 5, 6}
	minimumTTL, ok := checkedMul(cfg.Policy.Settlement.EpochBlocks, cfg.Policy.Settlement.ClaimTTLEpochs)
	if !ok {
		t.Fatal("fixture minimum TTL overflow")
	}
	return map[string][]any{
		"coordinator_netuid":                   {cfg.Netuid},
		"coordinator_self_coldkey":             {coordinatorSelf},
		"coordinator_guardian":                 {common.HexToAddress("0x1000000000000000000000000000000000000001")},
		"coordinator_active_guardian":          {common.HexToAddress("0x1000000000000000000000000000000000000003")},
		"coordinator_paused":                   {false},
		"coordinator_commitment_oracle":        {common.HexToAddress("0x1000000000000000000000000000000000000002")},
		"coordinator_active_commitment_oracle": {common.HexToAddress("0x1000000000000000000000000000000000000004")},
		"coordinator_vault":                    {deployment.SettlementVault},
		"coordinator_reserve":                  {deployment.ReserveSink},
		"vault_coordinator":                    {deployment.CoordinatorProxy},
		"vault_netuid":                         {cfg.Netuid},
		"vault_self_coldkey":                   {vaultSelf},
		"vault_escrow_hotkey":                  {escrowHotkey},
		"vault_escrow_registered":              {true},
		"vault_minimum_claim_ttl":              {minimumTTL},
		"vault_minimum_transfer":               {uint64(1)},
		"reserve_recorder":                     {deployment.CoordinatorProxy},
		"reserve_netuid":                       {cfg.Netuid},
		"reserve_self_coldkey":                 {reserveSelf},
		"reserve_hotkey":                       {reserveHotkey},
	}
}

func TestDecodeContractCustodyViewBindsImmutableLiveWiring(t *testing.T) {
	cfg := testResolvedConfig(t)
	deployment := &ContractDeployment{
		CoordinatorProxy: common.HexToAddress("0x2000000000000000000000000000000000000001"),
		SettlementVault:  common.HexToAddress("0x2000000000000000000000000000000000000002"),
		ReserveSink:      common.HexToAddress("0x2000000000000000000000000000000000000003"),
	}
	results := contractCustodyBatchFixture(t, cfg, deployment)
	view, err := decodeContractCustodyView(results, deployment, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if view.CoordinatorNetuid != cfg.Netuid || view.VaultNetuid != cfg.Netuid || view.ReserveNetuid != cfg.Netuid ||
		!strings.EqualFold(view.CoordinatorVault, deployment.SettlementVault.Hex()) || !strings.EqualFold(view.CoordinatorReserve, deployment.ReserveSink.Hex()) ||
		!strings.EqualFold(view.VaultCoordinator, deployment.CoordinatorProxy.Hex()) || !strings.EqualFold(view.ReserveRecorder, deployment.CoordinatorProxy.Hex()) ||
		!view.VaultEscrowRegistered || view.VaultMinimumTransferRao != 1 || view.CoordinatorPaused || view.CoordinatorActiveGuardian == view.CoordinatorGuardian || view.CoordinatorActiveCommitmentOracle == view.CoordinatorCommitmentOracle {
		t.Fatalf("decoded custody identity is incomplete: %+v", view)
	}

	// Paused is mutable during the mandatory governance drill and therefore is
	// captured, not rejected by the observer. The terminal semantic gate is
	// responsible for requiring restoration to false.
	results["coordinator_paused"] = []any{true}
	view, err = decodeContractCustodyView(results, deployment, cfg)
	if err != nil || !view.CoordinatorPaused {
		t.Fatalf("transient paused state was not captured: view=%+v err=%v", view, err)
	}
}

func TestDecodeContractCustodyViewRejectsAdjacentIdentitySubstitutions(t *testing.T) {
	cfg := testResolvedConfig(t)
	deployment := &ContractDeployment{
		CoordinatorProxy: common.HexToAddress("0x3000000000000000000000000000000000000001"),
		SettlementVault:  common.HexToAddress("0x3000000000000000000000000000000000000002"),
		ReserveSink:      common.HexToAddress("0x3000000000000000000000000000000000000003"),
	}
	wrongAddress := common.HexToAddress("0x4000000000000000000000000000000000000001")
	wrongKey := [32]byte{9}
	cases := []struct {
		name string
		edit func(map[string][]any)
	}{
		{name: "coordinator netuid", edit: func(v map[string][]any) { v["coordinator_netuid"] = []any{cfg.Netuid + 1} }},
		{name: "vault netuid", edit: func(v map[string][]any) { v["vault_netuid"] = []any{cfg.Netuid + 1} }},
		{name: "reserve netuid", edit: func(v map[string][]any) { v["reserve_netuid"] = []any{cfg.Netuid + 1} }},
		{name: "coordinator self coldkey", edit: func(v map[string][]any) { v["coordinator_self_coldkey"] = []any{wrongKey} }},
		{name: "vault self coldkey", edit: func(v map[string][]any) { v["vault_self_coldkey"] = []any{wrongKey} }},
		{name: "reserve self coldkey", edit: func(v map[string][]any) { v["reserve_self_coldkey"] = []any{wrongKey} }},
		{name: "coordinator vault", edit: func(v map[string][]any) { v["coordinator_vault"] = []any{wrongAddress} }},
		{name: "coordinator reserve", edit: func(v map[string][]any) { v["coordinator_reserve"] = []any{wrongAddress} }},
		{name: "vault coordinator", edit: func(v map[string][]any) { v["vault_coordinator"] = []any{wrongAddress} }},
		{name: "reserve recorder", edit: func(v map[string][]any) { v["reserve_recorder"] = []any{wrongAddress} }},
		{name: "escrow unregistered", edit: func(v map[string][]any) { v["vault_escrow_registered"] = []any{false} }},
		{name: "minimum TTL", edit: func(v map[string][]any) { v["vault_minimum_claim_ttl"] = []any{uint64(1)} }},
		{name: "zero escrow hotkey", edit: func(v map[string][]any) { v["vault_escrow_hotkey"] = []any{[32]byte{}} }},
		{name: "zero reserve hotkey", edit: func(v map[string][]any) { v["reserve_hotkey"] = []any{[32]byte{}} }},
		{name: "missing guardian", edit: func(v map[string][]any) { delete(v, "coordinator_guardian") }},
		{name: "missing active guardian", edit: func(v map[string][]any) { delete(v, "coordinator_active_guardian") }},
		{name: "missing base oracle", edit: func(v map[string][]any) { delete(v, "coordinator_commitment_oracle") }},
		{name: "missing active oracle", edit: func(v map[string][]any) { delete(v, "coordinator_active_commitment_oracle") }},
		{name: "zero minimum transfer", edit: func(v map[string][]any) { v["vault_minimum_transfer"] = []any{uint64(0)} }},
	}
	for _, test := range cases {
		results := contractCustodyBatchFixture(t, cfg, deployment)
		test.edit(results)
		if _, err := decodeContractCustodyView(results, deployment, cfg); err == nil {
			t.Fatalf("%s substituted or incomplete custody identity was accepted", test.name)
		}
	}
}

package main

import (
	"os"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// This opt-in probe performs no mutation. It recomputes runtime-452's next
// victim from one finalized public testnet state root and binds it to the exact
// deterministic lifecycle roles before any approved plan can launch.
func TestLiveFleetLifecyclePruneSnapshot(t *testing.T) {
	if os.Getenv("SIM_TESTNET_LIVE_LIFECYCLE") != "1" {
		t.Skip("set SIM_TESTNET_LIVE_LIFECYCLE=1 for the public testnet read-only probe")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := crv4.DialChain(cfg.OperationalSubstrate)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	manager := &SubstrateManager{cfg: cfg, chain: chain}
	snapshot, err := manager.FleetLifecyclePruneSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFleetLifecycleLaunchSnapshot(snapshot, roles); err != nil {
		t.Fatalf("public testnet lifecycle launch snapshot: %v; snapshot=%+v", err, snapshot)
	}
	uidOne := snapshot.Inputs[1]
	t.Logf("finalized native head=%d/%s runtime_prune_uid=%d uid1_immortal=%t uid1_immune=%t uid1_emission_rao=%d uid1_registration_block=%d nonimmune=%d minimum_nonimmune=%d", snapshot.Head.Number, snapshot.Head.Hash, snapshot.RuntimePruneUID, uidOne.Immortal, uidOne.Immune, uidOne.EmissionRao, uidOne.RegistrationBlock, snapshot.NonImmuneUIDs, snapshot.MinimumNonImmuneUIDs)
}

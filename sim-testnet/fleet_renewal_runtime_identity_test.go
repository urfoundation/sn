package main

// fleet_renewal_runtime_identity_test.go covers local action ownership and
// already-approved resume compatibility without chain reads or live state.

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// TestFleetRenewalRuntimeIdentityOwnsChangedLocalIntents isolates the producer
// boundary with three actions; no 202-fleet census is needed for copy ownership.
func TestFleetRenewalRuntimeIdentityOwnsChangedLocalIntents(t *testing.T) {
	oldConfig, policy, oldResolved := finalFleetGenerationTestHash(7401), finalFleetGenerationTestHash(7402), finalFleetGenerationTestHash(7403)
	plan := &SetupPlan{ConfigHash: oldConfig, PolicyHash: policy, ResolvedInputsHash: oldResolved, Actions: []Action{
		{ID: "config.render", Kind: "local", Parameters: map[string]string{"config_hash": oldConfig, "policy_hash": policy, "resolved_inputs_hash": oldResolved, "native_runtime_hash": finalFleetGenerationTestHash(7404)}, AcceptedPriorIntentHashes: []string{finalFleetGenerationTestHash(7405)}, DependsOn: []string{"retained-prerequisite"}},
		{ID: "topology.launch", Kind: "local", Parameters: map[string]string{"config_hash": oldConfig, "policy_hash": policy}},
		{ID: "fleet.renew.1.1.bind.1", Kind: "evm-transaction", Parameters: map[string]string{"signed_bytes": "retained"}, Spend: Spend{EVMGasWei: "123"}},
	}}
	for index := range plan.Actions {
		var err error
		plan.Actions[index].IntentHash, err = actionIntentHash(plan.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	before, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ResolvedConfig{ConfigHash: finalFleetGenerationTestHash(7406), PolicyHash: policy, ObjectStoreHost: "renewal.objects.example"}
	rebound, err := bindFleetRenewalRuntimeIdentity(cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 2 {
		action := rebound.Actions[index]
		if action.Parameters["config_hash"] != rebound.ConfigHash || action.Parameters["policy_hash"] != policy || action.IntentHash == plan.Actions[index].IntentHash || len(action.AcceptedPriorIntentHashes) != 0 {
			t.Fatalf("local action %s retained the old runtime approval", action.ID)
		}
	}
	if rebound.Actions[0].Parameters["resolved_inputs_hash"] != rebound.ResolvedInputsHash || rebound.Actions[0].Parameters["native_runtime_hash"] != plan.Actions[0].Parameters["native_runtime_hash"] || !reflect.DeepEqual(rebound.Actions[0].DependsOn, plan.Actions[0].DependsOn) || !reflect.DeepEqual(rebound.Actions[2], plan.Actions[2]) {
		t.Fatal("runtime rebinding changed immutable dependencies, native pins, or transaction authority")
	}
	if after, err := json.Marshal(plan); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("runtime rebinding mutated its source: %v", err)
	}
	again, err := bindFleetRenewalRuntimeIdentity(cfg, rebound)
	if err != nil || again.PlanHash != rebound.PlanHash {
		t.Fatalf("runtime rebinding is not idempotent: %v", err)
	}
	for _, changed := range []ResolvedConfig{
		{ConfigHash: cfg.ConfigHash, PolicyHash: finalFleetGenerationTestHash(7407)},
		{ConfigHash: cfg.ConfigHash, PolicyHash: policy, ownedRPCAuthority: "unapproved.rpc.example"},
	} {
		if _, err := bindFleetRenewalRuntimeIdentity(&changed, plan); err == nil {
			t.Fatal("runtime rebinding changed immutable policy or owned Rpc route")
		}
	}
}

// TestFleetRenewalRuntimeIdentityPreservesLegacyApproval reproduces the old
// valid wire shape: resolved inputs changed while render kept its earlier
// stamp. Exact imported consent and signed recovery must survive the hotfix.
func TestFleetRenewalRuntimeIdentityPreservesLegacyApproval(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	expected, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	fixture.cfg.ObjectStoreHost = "legacy-renewal.objects.example"
	legacy := *expected
	legacy.ResolvedInputsHash, err = resolvedInputsHash(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	legacy.PlanHash, err = legacy.hash()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(&legacy)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := decodePersistedPlanBytes(raw)
	if err != nil {
		t.Fatalf("legacy approval fixture is not a valid import: %v", err)
	}
	entries := []JournalEntry{{EntryHash: fixture.renewal.JournalHash}}
	if err := validateFleetRenewalSource(fixture.cfg, fixture.base, approved, entries); err != nil {
		t.Fatalf("hotfix invalidated exact legacy recovery: %v", err)
	}
	if matches, err := finalFleetRenewalApprovalMatches(expected, approved); err != nil || !matches {
		t.Fatalf("archive lost exact legacy approval: %v", err)
	}
	current, err := bindFleetRenewalRuntimeIdentity(fixture.cfg, expected)
	if err != nil || current.PlanHash == approved.PlanHash {
		t.Fatalf("fresh planning retained the legacy unstamped local intent: %v", err)
	}
	if err := validateFleetRenewalSource(fixture.cfg, fixture.base, current, entries); err != nil {
		t.Fatalf("fresh source reconstruction differs from its producer: %v", err)
	}
}

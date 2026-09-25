// Synthetic queue fixtures distinguish historical repair from complete,
// current-window payout coverage without changing the immutable queue.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Two candidate miners and two tail miners cover both operators.
func scenarioClaimWindowTestEvaluation() *scenarioEvaluation {
	cfg := &ResolvedConfig{Config: &HarnessConfig{Topology: TopologyConfig{Operators: 2, Miners: 4, HeadFleets: 2, ClientsPerHeadFleet: 1}}}
	e := &scenarioEvaluation{Cfg: cfg, Window: &ScenarioAcceptanceWindow{FirstEpoch: 100, EpochCount: 2}, Current: &ScenarioObservation{}}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		status := "finalized"
		if miner <= cfg.Config.Topology.fleetCandidateMiners() {
			status = "no-claim"
		}
		e.Current.Claims = append(e.Current.Claims, ClaimObservation{
			MinerID: miner, NoID: operatorForMiner(cfg, miner), LastDiscovered: 101,
			Discovered: 100, Finalized: 3, NoClaim: 4, Uncertain: 2, Failed: 1,
			EpochOutcomes: []ClaimEpochObservation{{Epoch: 99, Status: "uncertain"}, {Epoch: 100, Status: status}, {Epoch: 101, Status: "no-claim"}},
		})
	}
	return e
}

// Runs the production release checks rather than duplicating their predicates.
func scenarioClaimWindowTestCheck(t *testing.T, e *scenarioEvaluation, id string) (bool, string) {
	t.Helper()
	for _, check := range releaseScenarioChecks() {
		if check.ID == id {
			return check.Check(e)
		}
	}
	t.Fatalf("missing production claim check %s", id)
	return false, ""
}

// Old unresolved transactions remain visible, but complete current outcomes
// satisfy the signed window without pretending to repair historical receipts.
func TestScenarioClaimWindowKeepsHistoricalFindingsOutsideCurrentCoverage(t *testing.T) {
	e := scenarioClaimWindowTestEvaluation()
	before, err := json.Marshal(e.Current.Claims)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"claims_finalized_per_no", "tier_exclusive_claim_outcomes"} {
		if ok, reason := scenarioClaimWindowTestCheck(t, e, id); !ok {
			t.Fatalf("%s rejected exact current coverage: %s", id, reason)
		}
	}
	after, err := json.Marshal(e.Current.Claims)
	if err != nil || string(before) != string(after) {
		t.Fatalf("window evaluation changed historical evidence: %v", err)
	}
	e.Window = nil
	if ok, _ := scenarioClaimWindowTestCheck(t, e, "claims_finalized_per_no"); ok {
		t.Fatal("non-window evaluation hid lifetime uncertainty")
	}
}

// Historical success cannot substitute for discovery, submission or payout
// in the current signed window; unresolved current sends remain strict.
func TestScenarioClaimWindowRejectsAbsentAndUnresolvedCurrentClaims(t *testing.T) {
	tests := []struct {
		name string
		edit func(*scenarioEvaluation)
	}{
		{name: "no epoch observation", edit: func(e *scenarioEvaluation) { e.Current.Claims[2].EpochOutcomes = nil }},
		{name: "undiscovered current epoch", edit: func(e *scenarioEvaluation) { e.Current.Claims[2].LastDiscovered = 99 }},
		{name: "missing first epoch", edit: func(e *scenarioEvaluation) { e.Current.Claims[2].EpochOutcomes[1].Epoch = 98 }},
		{name: "missing queue entry", edit: func(e *scenarioEvaluation) { e.Current.Claims[2].EpochOutcomes[1].Status = "undiscovered" }},
		{name: "current uncertain signed transaction", edit: func(e *scenarioEvaluation) { e.Current.Claims[2].EpochOutcomes[1].Status = "uncertain" }},
		{name: "current submitting", edit: func(e *scenarioEvaluation) { e.Current.Claims[2].EpochOutcomes[1].Status = "submitting" }},
		{name: "current failed", edit: func(e *scenarioEvaluation) { e.Current.Claims[2].EpochOutcomes[1].Status = "failed" }},
		{name: "only historical finalized", edit: func(e *scenarioEvaluation) { e.Current.Claims[2].EpochOutcomes[1].Status = "pending" }},
		{name: "unknown status", edit: func(e *scenarioEvaluation) { e.Current.Claims[2].EpochOutcomes[1].Status = "invented-success" }},
	}
	for _, test := range tests {
		e := scenarioClaimWindowTestEvaluation()
		// Remove historical failures, so old cumulative checks incorrectly pass.
		for i := range e.Current.Claims {
			e.Current.Claims[i].Uncertain, e.Current.Claims[i].Failed = 0, 0
		}
		test.edit(e)
		for _, id := range []string{"claims_finalized_per_no", "tier_exclusive_claim_outcomes"} {
			if ok, reason := scenarioClaimWindowTestCheck(t, e, id); ok {
				t.Fatalf("%s: %s accepted missing or unresolved current payout: %s", test.name, id, reason)
			}
		}
	}
}

// Exact miner/operator and epoch identities are required even when every
// supplied status string claims success.
func TestScenarioClaimWindowRejectsAmbiguousCoverageIdentity(t *testing.T) {
	tests := []struct {
		name string
		edit func(*scenarioEvaluation)
	}{
		{name: "duplicate miner", edit: func(e *scenarioEvaluation) { e.Current.Claims[1] = e.Current.Claims[0] }},
		{name: "missing miner", edit: func(e *scenarioEvaluation) { e.Current.Claims = e.Current.Claims[:3] }},
		{name: "wrong operator", edit: func(e *scenarioEvaluation) { e.Current.Claims[0].NoID = 2 }},
		{name: "duplicate epoch", edit: func(e *scenarioEvaluation) {
			e.Current.Claims[0].EpochOutcomes[0] = e.Current.Claims[0].EpochOutcomes[1]
		}},
		{name: "source read error", edit: func(e *scenarioEvaluation) { e.Current.Claims[0].Error = "synthetic read failure" }},
		{name: "overflow", edit: func(e *scenarioEvaluation) { e.Window.FirstEpoch = ^uint64(0) }},
		{name: "empty window", edit: func(e *scenarioEvaluation) { e.Window.EpochCount = 0 }},
	}
	for _, test := range tests {
		e := scenarioClaimWindowTestEvaluation()
		test.edit(e)
		if _, err := scenarioClaimsForAcceptance(e); err == nil {
			t.Fatalf("%s coverage identity accepted", test.name)
		}
	}
}

// Reads only the requested epoch census into the observation, preserves
// lifetime counters and deterministically selects the latest transaction.
func TestScenarioClaimWindowQueueProjectionPreservesExactSource(t *testing.T) {
	e := scenarioClaimWindowTestEvaluation()
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "runtime", "miner-3", "claims", "claim-queue.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema":"urnetwork-provider-claim-queue-v1","last_discovered":101,"entries":{"99":{"epoch":99,"status":"uncertain","tx_hash":"synthetic-old"},"100":{"epoch":100,"status":"finalized","tx_hash":"synthetic-new"},"101":{"epoch":101,"status":"no-claim"}}}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	want := []ClaimEpochObservation{{Epoch: 100, Status: "finalized"}, {Epoch: 101, Status: "no-claim"}, {Epoch: 102, Status: "undiscovered"}}
	for range 20 {
		claim := inspectClaimQueue(e.Cfg, stateDir, 3, 100, 101, 102)
		if claim.Error != "" || claim.Uncertain != 1 || claim.Finalized != 1 || claim.NoClaim != 1 || claim.Discovered != 3 || claim.LastDiscovered != 101 || claim.LastTxHash != "synthetic-new" || !slices.Equal(claim.EpochOutcomes, want) {
			t.Fatalf("claim queue projection changed identity/scope: %+v", claim)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(raw) {
		t.Fatalf("observation mutated claim queue: %v", err)
	}
}

// Canonical decimal keys and their body epoch are one identity; aliases must
// not create duplicate-effective epochs or forge a covered row.
func TestScenarioClaimWindowQueueRejectsConflictingEpochKeys(t *testing.T) {
	e := scenarioClaimWindowTestEvaluation()
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, "runtime", "miner-3", "claims", "claim-queue.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{
		`"0100":{"epoch":100,"status":"finalized"}`,
		`"100":{"epoch":99,"status":"finalized"}`,
		`"-1":{"epoch":-1,"status":"finalized"}`,
		`"unknown":{"epoch":100,"status":"finalized"}`,
	} {
		raw := fmt.Sprintf(`{"schema":"urnetwork-provider-claim-queue-v1","last_discovered":101,"entries":{%s}}`, entry)
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		claim := inspectClaimQueue(e.Cfg, stateDir, 3, 100, 101)
		if !strings.Contains(claim.Error, "epoch identity") {
			t.Fatalf("conflicting queue identity accepted: %s, %+v", entry, claim)
		}
	}
}

package main

// final_semantic_fleet_generation_source_test.go covers the closed archive
// namespace consumed while reconstructing ordinary fleet generations.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Rejects every unexpected file below plan-history before it can be silently
// excluded from the lineage artifact. The one canonical predecessor file is
// accepted first, proving that the test is about namespace closure rather
// than a missing-plan error.
func TestFinalFleetGenerationSourceRejectsUnapprovedPlanHistoryEntries(t *testing.T) {
	archive, evidence, chain, events, priorHash, canonicalPath := finalFleetGenerationSourceNamespaceFixture(t)
	baseline, err := newFinalFleetGenerationSource(archive, evidence, chain, events)
	if err != nil {
		t.Fatalf("accept exact predecessor namespace: %v", err)
	}
	if baseline.planPaths[priorHash] != canonicalPath {
		t.Fatalf("predecessor path=%q, want %q", baseline.planPaths[priorHash], canonicalPath)
	}
	for _, test := range []struct {
		name string
		path string
	}{
		{name: "binary sibling", path: "plan-history/extra.bin"},
		{name: "nested JSON", path: "plan-history/nested/" + stringsTrim0x(priorHash) + ".json"},
		{name: "foreign canonical-looking JSON", path: "plan-history/" + strings.Repeat("f", 64) + ".json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive, evidence, chain, events, _, _ := finalFleetGenerationSourceNamespaceFixture(t)
			archive.files[test.path] = []byte("unapproved plan-history input")
			if _, err := newFinalFleetGenerationSource(archive, evidence, chain, events); err == nil || !strings.Contains(err.Error(), "not an approved canonical predecessor path") {
				t.Fatalf("unapproved plan-history entry error=%v", err)
			}
		})
	}
}

// Builds two independently persisted, budget-valid plans so this test can
// exercise the source's exact predecessor-path join without a campaign-sized
// fixture. The archive has one valid journal row because source construction
// must authenticate that closed input before reading plan history.
func finalFleetGenerationSourceNamespaceFixture(t *testing.T) (*finalSemanticArchive, *FinalSemanticEvidence, *FinalCollectedChainSnapshot, *finalSemanticEventIndex, string, string) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	prior := *base
	prior.PriorPlanHashes = nil
	prior.PlanHash = ""
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	priorBytes, err := json.Marshal(&prior)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, decodeErr := decodePersistedPlanBytes(priorBytes); decodeErr != nil || decoded.PlanHash != prior.PlanHash {
		t.Fatalf("persist predecessor plan: %v", decodeErr)
	}
	current := prior
	current.PriorPlanHashes = []string{prior.PlanHash}
	current.PlanHash = ""
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	currentBytes, err := json.Marshal(&current)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, decodeErr := decodePersistedPlanBytes(currentBytes); decodeErr != nil || decoded.PlanHash != current.PlanHash {
		t.Fatalf("persist current plan: %v", decodeErr)
	}
	if len(current.Actions) == 0 {
		t.Fatal("fixture plan has no action for its journal")
	}
	journal := JournalEntry{
		Schema: "urnetwork-sim-journal-v1", Sequence: 1, Time: time.Unix(1_700_000_001, 0).UTC().Format(time.RFC3339Nano),
		DeploymentID: current.DeploymentID, PlanHash: current.PlanHash, ActionID: current.Actions[0].ID,
		IntentHash: current.Actions[0].IntentHash, Stage: StageIntent,
	}
	journal.EntryHash, err = canonicalHashHex(journal)
	if err != nil {
		t.Fatal(err)
	}
	journalBytes, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	journalBytes = append(journalBytes, '\n')
	batcher, _, err := finalPlanFleetBatcher(&current)
	if err != nil {
		t.Fatal(err)
	}
	canonicalPath := filepath.ToSlash(filepath.Join("plan-history", stringsTrim0x(prior.PlanHash)+".json"))
	archive := &finalSemanticArchive{files: map[string][]byte{
		"launch-foundation/plan.json":     currentBytes,
		"launch-foundation/journal.jsonl": journalBytes,
		canonicalPath:                     priorBytes,
	}}
	evidence := &FinalSemanticEvidence{DeploymentID: current.DeploymentID, PlanHash: current.PlanHash, ChainID: current.ChainID, Netuid: current.Netuid}
	chain := &FinalCollectedChainSnapshot{FleetBatcher: strings.ToLower(batcher.Hex())}
	return archive, evidence, chain, &finalSemanticEventIndex{}, prior.PlanHash, canonicalPath
}

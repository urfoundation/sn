// Generated approvals must remain readable across import, archive and runtime
// reload. Larger plan capacity cannot relax proof limits or filesystem custody.
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Use the real renewal generator and compact command serializer. Synthetic
// approved text crosses the former proof-file cap without production data.
func TestSetupPlanLargeGeneratedRenewalRoundTrip(t *testing.T) {
	fixture, _, _, _ := provisionalFleetRenewalFixture(t)
	action := &fixture.base.Actions[0]
	action.Description = strings.Repeat("synthetic approved history; ", maximumCampaignEvidenceRawFileBytes/27+1)
	var err error
	action.IntentHash, err = actionIntentHash(*action)
	if err != nil {
		t.Fatal(err)
	}
	fixture.base.PlanHash, err = fixture.base.hash()
	if err != nil {
		t.Fatal(err)
	}
	fixture.renewal.SourcePlanHash = fixture.base.PlanHash
	approved, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := writeJSONResult(&output, approved); err != nil {
		t.Fatal(err)
	}
	raw := output.Bytes()
	if len(raw) <= maximumCampaignEvidenceRawFileBytes || len(raw) > maximumSetupPlanFileBytes {
		t.Fatalf("generated approval did not exercise the distinct plan capacity: %d", len(raw))
	}
	path := filepath.Join(fixture.stateDir, "renewal.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	imported, err := readFleetRenewalPlan(path)
	if err != nil || imported.PlanHash != approved.PlanHash || len(imported.FleetRenewals) != 1 {
		t.Fatalf("generated renewal cannot be imported with its exact approval: %v", err)
	}
	archived, err := archiveReviewedSetupPlanBytes(fixture.stateDir, approved.PlanHash, raw)
	if err != nil || !bytes.Equal(archived, raw) {
		t.Fatalf("generated renewal lost immutable archive bytes: %v", err)
	}
	historical, err := readValidatorEvidenceHistoricalPlan(fixture.stateDir, approved.PlanHash)
	if err != nil || historical.PlanHash != approved.PlanHash || !historical.validatorEvidenceHistorical {
		t.Fatalf("generated renewal cannot be authenticated from its archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture.stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	strict := *fixture.cfg
	strict.provisionalResume = nil
	runtimePlan, err := loadRuntimePersistedPlan(&strict, fixture.stateDir)
	if err != nil || runtimePlan.PlanHash != approved.PlanHash {
		t.Fatalf("generated renewal cannot survive a strict runtime reload: %v", err)
	}
	if _, err := readValidatorEvidenceHistoricalFile(fixture.stateDir, "renewal.json", maximumCampaignEvidenceRawFileBytes); err == nil {
		t.Fatal("plan capacity leaked into ordinary proof admission")
	}
	changed := bytes.Clone(raw)
	offset := bytes.Index(changed, []byte("synthetic approved history"))
	if offset < 0 {
		t.Fatal("synthetic authenticated payload is absent")
	}
	changed[offset] = 'X'
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readFleetRenewalPlan(path); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("large plan changed without renewed approval: %v", err)
	}
}

// Sparse oversized files are rejected by metadata admission before allocation.
// Exact producer bounds include the newline and never silently truncate output.
func TestSetupPlanFileCapacityRejectsOversizedAndEmpty(t *testing.T) {
	for _, size := range []int64{0, maximumSetupPlanFileBytes + 1} {
		path := filepath.Join(t.TempDir(), "approval.json")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(file.Truncate(size), file.Close()); err != nil {
			t.Fatal(err)
		}
		if raw, err := readSetupPlanFileBytes(path); err == nil || raw != nil {
			t.Fatalf("size %d admitted plan bytes: %v", size, err)
		}
		if err := validateSetupPlanWireSize(int(size)); err == nil {
			t.Fatalf("size %d accepted by producer", size)
		}
	}
	if err := validateSetupPlanWireSize(maximumSetupPlanFileBytes); err != nil {
		t.Fatal("exact plan bound refused", err)
	}
	if _, err := readValidatorEvidenceHistoricalFile(t.TempDir(), "approval.json", maximumCampaignEvidenceRawFileBytes+1); err == nil || !strings.Contains(err.Error(), "bound is invalid") {
		t.Fatal("ordinary reader acquired the plan-only capacity", err)
	}
}

// Every external import component has the same no-follow requirement as the
// stored approval. A missing file, directory or FIFO grants no partial plan.
func TestSetupPlanImportRejectsAliasesAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "owner")
	if err := os.Mkdir(owner, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(owner, "approval.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, err := readSetupPlanFileBytes(path); err != nil || string(raw) != "{}" {
		t.Fatal("regular plan bytes unavailable", err)
	}
	leaf := filepath.Join(owner, "alias.json")
	parent := filepath.Join(root, "alias")
	if err := errors.Join(os.Symlink(path, leaf), os.Symlink(owner, parent)); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(owner, "fifo.json")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{leaf, filepath.Join(parent, "approval.json"), owner, fifo, filepath.Join(owner, "absent.json")} {
		if raw, err := readSetupPlanFileBytes(invalid); err == nil || raw != nil {
			t.Fatalf("external import accepted %s: %v", invalid, err)
		}
	}
}

// A larger approval need not fit the optional decoded-plan cache. The exact
// fresh decoder still runs and the cache keeps its original memory ceiling.
func TestSetupPlanOwnerCacheDoesNotEvictEmptyForLargeApproval(t *testing.T) {
	cache := &evidenceRelayOwnerPlanCache{}
	key := evidenceRelayOwnerPlanKey{planHash: "0x" + strings.Repeat("ad", 32)}
	raw := make([]byte, evidenceRelayOwnerPlanCacheBytes+1)
	calls := 0
	decode := func([]byte) (*SetupPlan, error) {
		calls++
		return &SetupPlan{PlanHash: key.planHash, validatorEvidenceHistorical: true}, nil
	}
	for i := 0; i < 2; i++ {
		plan, err := cache.decode(t.Context(), key, raw, decode)
		if err != nil || plan.PlanHash != key.planHash || cache.rawBytes != 0 || len(cache.entryKVs) != 0 || len(cache.order) != 0 {
			t.Fatal("large valid approval exceeded the optional cache owner", err)
		}
	}
	if calls != 2 {
		t.Fatalf("uncached approval skipped fresh authentication: %d", calls)
	}
}

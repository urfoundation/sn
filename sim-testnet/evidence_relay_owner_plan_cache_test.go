//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestEvidenceRelayOwnerPlanCacheReusesExactArchivedBytes(t *testing.T) {
	t.Parallel()
	_, stateDir, original := fleetCensusPlanTestFixture(t)
	raw, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	cache := &evidenceRelayOwnerPlanCache{}
	key := evidenceRelayOwnerPlanKey{contextHash: bytesSHA256([]byte("synthetic approved context")), stateDir: stateDir, planHash: original.PlanHash}
	calls := 0
	decode := func(raw []byte) (*SetupPlan, error) {
		calls++
		return decodePersistedPlanBytesForHistory(raw, true)
	}
	for index := 0; index < 3; index++ {
		plan, err := cache.decode(t.Context(), key, raw, decode)
		if err != nil || plan.PlanHash != original.PlanHash || calls != 1 {
			t.Fatalf("identical archived approval decoded again: calls=%d error=%v", calls, err)
		}
	}
	// Same authority encoded with different exact bytes must be authenticated
	// again; neither a hash-looking pathname nor its old success is sufficient.
	raw = append(raw, '\n')
	if _, err := cache.decode(t.Context(), key, raw, decode); err != nil || calls != 2 {
		t.Fatalf("changed wire inherited validation: calls=%d error=%v", calls, err)
	}
	key.contextHash = bytesSHA256([]byte("another approved source context"))
	if _, err := cache.decode(t.Context(), key, raw, decode); err != nil || calls != 3 {
		t.Fatalf("changed context inherited validation: calls=%d error=%v", calls, err)
	}
	key.stateDir = filepath.Join(stateDir, "another-owner")
	if _, err := cache.decode(t.Context(), key, raw, decode); err != nil || calls != 4 {
		t.Fatalf("changed state owner inherited validation: calls=%d error=%v", calls, err)
	}
	original.DeploymentID += "-substituted"
	changed, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.decode(t.Context(), key, changed, decode); err == nil || calls != 5 || len(cache.entryKVs) != 4 {
		t.Fatal("substituted plan became a successful owner", calls, err)
	}
}

func TestEvidenceRelayOwnerPlanCacheRejectsMissingEmptyPartialAndCanceledReads(t *testing.T) {
	t.Parallel()
	_, stateDir, original := fleetCensusPlanTestFixture(t)
	raw, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "plans", stringsTrim0x(original.PlanHash)+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := &evidenceRelayOwnerPlanCache{}
	contextHash, err := canonicalHashHex("synthetic context")
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"missing", "empty", "partial", "symlink", "canceled-read"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		var opened func(*os.File) error
		switch fault {
		case "empty":
			err = os.WriteFile(path, nil, 0o600)
		case "partial":
			err = os.WriteFile(path, raw[:len(raw)/2], 0o600)
		case "symlink":
			err = os.Symlink(filepath.Join(stateDir, "plan.json"), path)
		case "canceled-read":
			err = os.WriteFile(path, raw, 0o600)
			opened = func(*os.File) error { cancel(); return nil }
		}
		if err != nil {
			t.Fatal(err)
		}
		plan, readErr := cache.read(ctx, stateDir, contextHash, original.PlanHash, opened)
		cancel()
		if readErr == nil || plan != nil || len(cache.entryKVs) != 0 {
			t.Fatalf("%s produced cached authority: %v", fault, readErr)
		}
		if fault == "canceled-read" && !errors.Is(readErr, context.Canceled) {
			t.Fatal("read cancellation lost its cause", readErr)
		}
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := cache.read(t.Context(), stateDir, contextHash, original.PlanHash, nil)
	if err != nil || len(cache.entryKVs) != 1 {
		t.Fatal("repaired original did not authenticate", err)
	}
	// A warm entry still requires the real file on every use.
	if err := os.WriteFile(path, raw[:len(raw)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if plan, err := cache.read(t.Context(), stateDir, contextHash, original.PlanHash, nil); err == nil || plan != nil {
		t.Fatal("warm owner concealed truncation", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if again, err := cache.read(t.Context(), stateDir, contextHash, original.PlanHash, nil); err != nil || again != first {
		t.Fatal("restored exact bytes did not reuse the valid private snapshot", err)
	}
}

func TestEvidenceRelayOwnerPlanCacheDoesNotPublishFailedOrCanceledValidation(t *testing.T) {
	t.Parallel()
	_, stateDir, original := fleetCensusPlanTestFixture(t)
	raw, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	key := evidenceRelayOwnerPlanKey{contextHash: "synthetic", stateDir: stateDir, planHash: original.PlanHash}
	cache := &evidenceRelayOwnerPlanCache{}
	failure := errors.New("synthetic incomplete archived read")
	for index := 0; index < 2; index++ {
		if _, err := cache.decode(t.Context(), key, raw, func([]byte) (*SetupPlan, error) { return nil, failure }); !errors.Is(err, failure) || len(cache.entryKVs) != 0 {
			t.Fatal("failed verification became reusable", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	if _, err := cache.decode(ctx, key, raw, func(raw []byte) (*SetupPlan, error) {
		plan, err := decodePersistedPlanBytesForHistory(raw, true)
		cancel()
		return plan, err
	}); !errors.Is(err, context.Canceled) || len(cache.entryKVs) != 0 {
		t.Fatal("canceled completed decode became authority", err)
	}
	if _, err := cache.decode(t.Context(), key, raw, func(raw []byte) (*SetupPlan, error) { return decodePersistedPlanBytesForHistory(raw, true) }); err != nil || len(cache.entryKVs) != 1 {
		t.Fatal("fresh retry did not recover validation", err)
	}
}

func TestEvidenceRelayOwnerPlanCacheBoundsEntriesAndRetainedBytes(t *testing.T) {
	t.Parallel()
	_, stateDir, original := fleetCensusPlanTestFixture(t)
	raw, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	cache := &evidenceRelayOwnerPlanCache{}
	calls := 0
	decode := func(raw []byte) (*SetupPlan, error) {
		calls++
		return decodePersistedPlanBytesForHistory(raw, true)
	}
	key := evidenceRelayOwnerPlanKey{stateDir: stateDir, planHash: original.PlanHash}
	for index := 0; index <= evidenceRelayOwnerPlanCacheEntries; index++ {
		key.contextHash = fmt.Sprint(index)
		if _, err := cache.decode(t.Context(), key, raw, decode); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entryKVs) != evidenceRelayOwnerPlanCacheEntries || cache.rawBytes != evidenceRelayOwnerPlanCacheEntries*len(raw) {
		t.Fatal("owner cache lost its finite entry/byte ownership")
	}
	key.contextHash = "0"
	if _, err := cache.decode(t.Context(), key, raw, decode); err != nil || calls != evidenceRelayOwnerPlanCacheEntries+2 {
		t.Fatal("evicted owner bypassed reauthentication", calls, err)
	}
	// Two maximum-sized authenticated plans fit exactly; another owner must
	// evict one even when the independent entry-count allowance has room.
	cache = &evidenceRelayOwnerPlanCache{}
	padded := append(append([]byte(nil), raw...), bytes.Repeat([]byte{' '}, maximumCampaignEvidenceRawFileBytes-len(raw))...)
	for index := 0; index < 2; index++ {
		key.contextHash = fmt.Sprint(index)
		if _, err := cache.decode(t.Context(), key, padded, decode); err != nil {
			t.Fatal(err)
		}
	}
	if cache.rawBytes != evidenceRelayOwnerPlanCacheBytes || len(cache.entryKVs) != 2 {
		t.Fatal("exact owner byte boundary was not retained")
	}
	key.contextHash = "third"
	if _, err := cache.decode(t.Context(), key, raw, decode); err != nil || cache.rawBytes != maximumCampaignEvidenceRawFileBytes+len(raw) || len(cache.entryKVs) != 2 {
		t.Fatal("owner byte overflow did not evict its oldest approval", err)
	}
}

func TestEvidenceRelayOwnerPlanCacheKeepsRequestsAndJournalFresh(t *testing.T) {
	t.Parallel()
	fixture, executor, continuation := evidenceRelayContinuationTest(t)
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, continuation)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, plan, fixture.roles); err != nil {
		t.Fatal(err)
	}
	executor.plan = plan
	executor.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: plan.PlanHash, ConfigHash: plan.ConfigHash, Provisional: true}}
	actionId := continuation.Debits[0].ActionID
	entries := executor.journal.Entries()
	_, first, raw, err := executor.readRetainedEvidenceRelayRequest(t.Context(), entries, actionId)
	if err != nil || len(executor.evidenceRelayOwnerPlans.entryKVs) != 1 {
		t.Fatal("original request did not authenticate its owner", err)
	}
	before := len(executor.journal.Entries())
	first.Action.Parameters["synthetic-consumer-mutation"] = "must not change cached plan"
	for index := 0; index < 2; index++ {
		_, record, _, err := executor.readRetainedEvidenceRelayRequest(t.Context(), entries, actionId)
		if err != nil || len(executor.evidenceRelayOwnerPlans.entryKVs) != 1 || record.Action.Parameters["synthetic-consumer-mutation"] != "" {
			t.Fatal("repeated request lost ownership or borrowed mutable action state", err)
		}
		if _, _, err := executor.admitOwnedEvidenceRelayAction(t.Context(), record.Evidence); err != nil {
			t.Fatal("actual retained admission failed", err)
		}
	}
	path := filepath.Join(fixture.stateDir, "evidence-relay", stringsTrimRelayPrefix(actionId)+".json")
	for _, changed := range [][]byte{nil, raw[:len(raw)/2], bytes.Replace(raw, []byte(`"plan_hash":"`), []byte(`"plan_hash":"changed-`), 1)} {
		if err := os.WriteFile(path, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := executor.readRetainedEvidenceRelayRequest(t.Context(), entries, actionId); err == nil {
			t.Fatal("owner cache concealed changed request bytes")
		}
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := executor.readRetainedEvidenceRelayRequest(t.Context(), nil, actionId); err == nil {
		t.Fatal("owner cache concealed a missing original debit")
	}
	competing := append(append([]JournalEntry(nil), entries...), JournalEntry{ActionID: actionId, PlanHash: plan.PlanHash, DeploymentID: plan.DeploymentID})
	if _, _, _, err := executor.readRetainedEvidenceRelayRequest(t.Context(), competing, actionId); err == nil {
		t.Fatal("owner cache concealed competing journal ownership")
	}
	if len(executor.journal.Entries()) != before {
		t.Fatal("retained authentication appended or retried a transaction")
	}
	oldConfigHash := executor.cfg.ConfigHash
	executor.cfg.ConfigHash, err = canonicalHashHex("synthetic changed configuration")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := executor.readRetainedEvidenceRelayRequest(t.Context(), entries, actionId); err != nil || len(executor.evidenceRelayOwnerPlans.entryKVs) != 2 {
		t.Fatal("changed configuration inherited prior owner validation", err)
	}
	executor.cfg.ConfigHash = oldConfigHash
	oldPreparedHash := plan.EvidenceRelayContinuation.PreparedSHA256
	plan.EvidenceRelayContinuation.PreparedSHA256 = bytesSHA256([]byte("synthetic different retained source"))
	changedContext, err := executor.evidenceRelayOwnerContextHash()
	if err != nil {
		t.Fatal(err)
	}
	plan.EvidenceRelayContinuation.PreparedSHA256 = oldPreparedHash
	originalContext, err := executor.evidenceRelayOwnerContextHash()
	if err != nil || changedContext == originalContext {
		t.Fatal("cache context omitted retained source commitments", err)
	}
	// Strict readers never consult or create the provisional optimization.
	executor.cfg.provisionalResume.Record.FinalAcceptance = true
	executor.evidenceRelayOwnerPlans = nil
	if _, _, _, err := executor.readRetainedEvidenceRelayRequest(t.Context(), entries, actionId); err != nil || executor.evidenceRelayOwnerPlans != nil {
		t.Fatal("final-acceptance provenance used a provisional cache", err)
	}
	executor.cfg.provisionalResume = nil
	executor.evidenceRelayOwnerPlans = nil
	if _, _, _, err := executor.readRetainedEvidenceRelayRequest(t.Context(), entries, actionId); err != nil || executor.evidenceRelayOwnerPlans != nil {
		t.Fatal("strict request authentication used a provisional cache", err)
	}
}

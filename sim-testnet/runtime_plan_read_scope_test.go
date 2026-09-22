// Count real admission calls rather than imposing timing thresholds. Exact
// source/authority invalidation and owned return values are deterministic.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Instrument the full production validator without changing its result or
// sharing a global test hook with concurrent rendering and verification.
func countedRuntimePlanReadScopeTest(cfg *ResolvedConfig, count *atomic.Int64) *ResolvedConfig {
	reader := runtimePlanReadScopeConfig(cfg)
	reader.runtimePlanReads.authenticate = func(cfg *ResolvedConfig, raw []byte, retainRelease bool) (*SetupPlan, error) {
		count.Add(1)
		return loadPlanIdentityBytes(cfg, raw, retainRelease)
	}
	return reader
}

// Even semantically identical new bytes require full authentication. Failed,
// missing or symlinked sources cannot use the last successful proof.
func TestRuntimePlanReadScopeRequiresIdenticalSource(t *testing.T) {
	cfg, plan, stateDir, options, before := provisionalRuntimePlanFixture(t)
	if err := prepareProvisionalResume(t.Context(), cfg, stateDir, "scenario", options, plan); err != nil {
		t.Fatal(err)
	}
	var count atomic.Int64
	reader := countedRuntimePlanReadScopeTest(cfg, &count)
	first, err := loadRuntimePersistedPlan(reader, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	first.Actions[0].Parameters = map[string]string{"synthetic-mutation": "caller-owned"}
	second, err := loadRuntimePersistedPlan(reader, stateDir)
	if err != nil || second.Actions[0].Parameters["synthetic-mutation"] != "" || count.Load() != 1 {
		t.Fatalf("warm proof lost ownership or repeated admission: count=%d err=%v", count.Load(), err)
	}
	path := filepath.Join(stateDir, "plan.json")
	changed := append(append([]byte{}, before...), '\n')
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRuntimePersistedPlan(reader, stateDir); err != nil || count.Load() != 2 {
		t.Fatalf("new source bytes did not reauthenticate: count=%d err=%v", count.Load(), err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := loadRuntimePersistedPlan(reader, stateDir); err == nil {
			t.Fatal("corrupted source borrowed a successful proof")
		}
	}
	if count.Load() != 4 {
		t.Fatal("failed authentication was retained as reusable authority")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRuntimePersistedPlan(reader, stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing source borrowed a successful proof", err)
	}
	other := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(other, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRuntimePersistedPlan(reader, stateDir); err == nil {
		t.Fatal("symlinked source borrowed an authenticated plan")
	}
}

// Private assurance and route fields are part of the cache authority even
// though they are intentionally absent from persisted configuration hashes.
func TestRuntimePlanReadScopeRequiresExactAuthorityAndOwner(t *testing.T) {
	cfg, plan, stateDir, options, before := provisionalRuntimePlanFixture(t)
	if err := prepareProvisionalResume(t.Context(), cfg, stateDir, "scenario", options, plan); err != nil {
		t.Fatal(err)
	}
	var count atomic.Int64
	reader := countedRuntimePlanReadScopeTest(cfg, &count)
	if _, err := loadRuntimePersistedPlan(reader, stateDir); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"config", "budget", "owned route", "public route", "strict", "approval"} {
		changed := *reader
		switch fault {
		case "config":
			changed.ConfigHash += "changed"
		case "budget":
			changed.MaximumTAORao++
		case "owned route":
			changed.ownedRPCAuthority = "unapproved.rpc.example:29944"
		case "public route":
			changed.OperationalEVM += "/changed"
		case "strict":
			changed.provisionalResume = nil
		case "approval":
			provenance, record := *changed.provisionalResume, *changed.provisionalResume.Record
			record.PlanHash = "0x" + strings.Repeat("db", 32)
			provenance.Record, changed.provisionalResume = &record, &provenance
		}
		prior := count.Load()
		if _, err := loadRuntimePersistedPlan(&changed, stateDir); err == nil || count.Load() != prior+1 {
			t.Errorf("%s borrowed prior authority: calls=%d err=%v", fault, count.Load()-prior, err)
		}
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "plan.json"), before, 0o600); err != nil {
		t.Fatal(err)
	}
	prior := count.Load()
	if _, err := loadRuntimePersistedPlan(reader, other); err != nil || count.Load() != prior+1 {
		t.Fatalf("new source owner borrowed a prior proof: count=%d err=%v", count.Load()-prior, err)
	}
	if cfg.runtimePlanReads != nil {
		t.Fatal("scoped reader modified its original caller")
	}
}

// A warm parallel reader returns independent maps/slices, and no external
// authentication runs while its cache lock is held. Tiny synthetic bytes keep
// this ownership proof independent of the fleet-size performance fixture.
func TestRuntimePlanReadScopeConcurrentResultsAreOwned(t *testing.T) {
	cfg := &ResolvedConfig{Config: &HarnessConfig{}}
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("af", 32), Actions: []Action{{ID: "synthetic", Parameters: map[string]string{"owner": "original"}}}}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	scope := &runtimePlanReadScope{}
	scope.authenticate = func(_ *ResolvedConfig, wire []byte, _ bool) (*SetupPlan, error) {
		calls.Add(1)
		// The callback must be able to acquire the state lock itself.
		scope.stateLock.Lock()
		scope.stateLock.Unlock()
		var result SetupPlan
		err := json.Unmarshal(wire, &result)
		return &result, err
	}
	root := t.TempDir()
	if _, err := scope.load(cfg, root, raw, false); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := scope.load(cfg, root, raw, false)
			if err != nil {
				t.Error(err)
				return
			}
			result.Actions[0].Parameters["owner"] = "private-copy"
		}()
	}
	workers.Wait()
	result, err := scope.load(cfg, root, raw, false)
	if err != nil || calls.Load() != 1 || result.Actions[0].Parameters["owner"] != "original" {
		t.Fatalf("warm readers shared mutable proof data: calls=%d err=%v", calls.Load(), err)
	}
}

// A successful proof retains its original decode shape. In particular,
// omitempty fields must not turn an explicit empty map into nil on a warm read.
func TestRuntimePlanReadScopeRetainsDecodedWireShape(t *testing.T) {
	cfg := &ResolvedConfig{Config: &HarnessConfig{}}
	raw := []byte(`{"actions":[{"id":"synthetic","parameters":{}}]}`)
	scope := &runtimePlanReadScope{authenticate: func(_ *ResolvedConfig, wire []byte, _ bool) (*SetupPlan, error) {
		var plan SetupPlan
		err := json.Unmarshal(wire, &plan)
		return &plan, err
	}}
	root := t.TempDir()
	for range 2 {
		plan, err := scope.load(cfg, root, raw, false)
		if err != nil || len(plan.Actions) != 1 || plan.Actions[0].Parameters == nil {
			t.Fatal("warm proof changed the original decoded wire shape", err)
		}
	}
}

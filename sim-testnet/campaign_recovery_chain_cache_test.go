// Signed recovery fixtures prove invocation reuse retains all source checks
// and never shares mutable attempts or authorizes strict final acceptance.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A cold fixture has two authenticated generations and an inherited failed
// pre-acceptance run, so a hit would otherwise reread plans and the journal.
func campaignRecoveryChainCacheFixture(t *testing.T) (*campaignSuccessionFixture, *scenarioCampaignAttempt, string, []scenarioCampaignRecoveryFile) {
	t.Helper()
	fixture, _, current, runDir := newCampaignRecoveryCacheFixture(t)
	fixture.cfg.provisionalResume.recoveryChain = nil
	files, err := scenarioCampaignRecoveryFiles(fixture.stateDir)
	if err != nil || len(files) != 2 {
		t.Fatalf("recovery source namespace: files=%d error=%v", len(files), err)
	}
	return fixture, current, runDir, files
}

// Build one new signed link without replacing the caller's retained memo, as
// happens when recovery creation and the immediate run reread share history.
func extendCampaignRecoveryChainCacheFixture(t *testing.T, fixture *campaignSuccessionFixture, current *scenarioCampaignAttempt) (*scenarioCampaignAttempt, []scenarioCampaignRecoveryFile) {
	t.Helper()
	retained := fixture.cfg.provisionalResume.recoveryChain
	fixture.cfg.provisionalResume.recoveryChain = nil
	bindPreAcceptanceFailedRecoveryGeneration(t, fixture, current)
	third, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(3*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	fixture.cfg.provisionalResume.recoveryChain = retained
	files, err := scenarioCampaignRecoveryFiles(fixture.stateDir)
	if err != nil || len(files) != 3 {
		t.Fatalf("extended namespace: files=%d error=%v", len(files), err)
	}
	return third, files
}

// A second reader projects independently owned values without traversing any
// source validator. Writes in the current run remain outside this old proof.
func TestScenarioCampaignRecoveryChainCacheReusesIndependentProjection(t *testing.T) {
	t.Parallel()
	fixture, current, _, files := campaignRecoveryChainCacheFixture(t)
	calls := 0
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls++
		return readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
	}
	first, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read)
	if err != nil || calls != 1 || len(first) != 2 {
		t.Fatalf("cold chain: calls=%d records=%d error=%v", calls, len(first), err)
	}
	wantRaw := bytes.Clone(first[0].raw)
	wantPriorRun := first[0].attempt.payload.Recovery.PriorRunID
	first[0].raw[0] ^= 1
	first[0].attempt.payload.Recovery.PriorRunID = "mutated-return-value"
	currentRun := filepath.Join(fixture.stateDir, "runs", current.payload.RunID)
	if err := os.MkdirAll(currentRun, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(currentRun, "observations.jsonl"), []byte("current run progress\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		again, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read)
		if err != nil || calls != 1 || !bytes.Equal(again[0].raw, wantRaw) || again[0].attempt.payload.Recovery.PriorRunID != wantPriorRun {
			t.Fatalf("projected chain changed or replayed: calls=%d error=%v", calls, err)
		}
		again[0].raw[0] ^= 1
		again[0].attempt.payload.Recovery.PriorRunID = "mutated-cache-hit"
	}
}

// A new generation validates its own predecessor and sources once, retaining
// the unchanged authenticated prefix instead of redecoding all old history.
func TestScenarioCampaignRecoveryChainCacheAppendedGeneration(t *testing.T) {
	t.Parallel()
	fixture, current, _, files := campaignRecoveryChainCacheFixture(t)
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err != nil {
		t.Fatal(err)
	}
	third, files := extendCampaignRecoveryChainCacheFixture(t, fixture, current)
	calls, retainedCount := 0, 0
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls++
		retainedCount = len(prefix)
		return readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
	}
	for index := 0; index < 2; index++ {
		records, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read)
		if err != nil || calls != 1 || retainedCount != 2 || len(records) != 3 || records[2].attempt.payload.RunID != third.payload.RunID {
			t.Fatalf("appended chain: calls=%d retained=%d records=%d error=%v", calls, retainedCount, len(records), err)
		}
	}
}

// Same-size source replacement must miss even if the writer restores mtime.
// Rejected reads never hide the error behind a cached partial success.
func TestScenarioCampaignRecoveryChainCacheSourceMutationAndRetry(t *testing.T) {
	t.Parallel()
	fixture, _, runDir, files := campaignRecoveryChainCacheFixture(t)
	calls := 0
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls++
		return readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runDir, "result.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(raw, []byte("deferred startup"), []byte("tampered startup"), 1)
	if bytes.Equal(changed, raw) || len(changed) != len(raw) {
		t.Fatal("source mutation must preserve length")
	}
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil {
			t.Fatal("changed signed source was accepted")
		}
	}
	if calls != 3 {
		t.Fatalf("failed read was cached: calls=%d", calls)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 {
		t.Fatalf("successful repair did not restore reusable proof: calls=%d", calls)
	}
}

// An absent or empty historical source and a canceled reader cannot populate
// a reusable proof; repaired evidence is validated before the next hit.
func TestScenarioCampaignRecoveryChainCacheMissingEmptyAndCanceledReads(t *testing.T) {
	t.Parallel()
	fixture, _, runDir, files := campaignRecoveryChainCacheFixture(t)
	path := filepath.Join(runDir, "observations.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err == nil {
		t.Fatal("missing authenticated source was accepted")
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err == nil {
		t.Fatal("empty authenticated source was accepted")
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls++
		records, err := readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
		if err == nil && calls == 1 {
			return records[:1], context.Canceled
		}
		return records, err
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled partial chain lost its error: %v", err)
	}
	for index := 0; index < 2; index++ {
		if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("partial failure was reused or successful retry replayed: calls=%d", calls)
	}
}

// Valid journal suffixes preserve historical authority. Invalid suffixes and
// newly written production evidence still reject reuse of old successes.
func TestScenarioCampaignRecoveryChainCacheNewJournalAndTerminalMarkers(t *testing.T) {
	t.Parallel()
	fixture, _, _, files := campaignRecoveryChainCacheFixture(t)
	calls := 0
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls++
		return readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.Append(JournalEntry{DeploymentID: fixture.current.DeploymentID, PlanHash: fixture.current.PlanHash, ActionID: "chain-cache-progress", IntentHash: "0x" + strings.Repeat("a8", 32), Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil || calls != 1 {
		t.Fatalf("valid journal append replayed historical sources: calls=%d error=%v", calls, err)
	}
	path := scenarioCampaignAttemptPath(fixture.stateDir, "production-soak")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil || calls != 2 || !strings.Contains(err.Error(), "production descendant") {
		t.Fatalf("new production descendant reused old proof: calls=%d error=%v", calls, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(fixture.stateDir, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("{}\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil || calls != 3 || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("invalid journal suffix reused old proof: calls=%d error=%v", calls, err)
	}
}

// Concurrent consumers share one validation, while custody or invocation-only
// admission changes invalidate the proof without relying on a persisted hash.
func TestScenarioCampaignRecoveryChainCacheConcurrentContextAndInvocation(t *testing.T) {
	t.Parallel()
	fixture, _, _, files := campaignRecoveryChainCacheFixture(t)
	var calls atomic.Int64
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls.Add(1)
		return readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
	}
	start := make(chan struct{})
	errorsCh := make(chan error, 8)
	var workers sync.WaitGroup
	for index := 0; index < cap(errorsCh); index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			records, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read)
			if err == nil && len(records) != 2 {
				err = errors.New("concurrent chain lost a generation")
			}
			errorsCh <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent readers repeated validation: %d", calls.Load())
	}
	oldInvocation := fixture.cfg.provisionalResume
	invocation := *oldInvocation
	invocation.RecordPath = "provisional-resumes/new-invocation.evidence.json"
	// Even an explicitly carried pointer cannot reuse a differently bound
	// invocation. The normal constructor starts with no pointer at all.
	fixture.cfg.provisionalResume = &invocation
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil || calls.Load() != 2 {
		t.Fatalf("new invocation reused old proof: calls=%d error=%v", calls.Load(), err)
	}
	invocation.recoveryChain = nil
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil || calls.Load() != 3 {
		t.Fatalf("fresh invocation skipped validation: calls=%d error=%v", calls.Load(), err)
	}
	fixture.cfg.readOnlyAudit = true
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil || calls.Load() != 4 {
		t.Fatalf("changed invocation mode reused old proof: calls=%d error=%v", calls.Load(), err)
	}
	owner := fixture.roles.EVM["testnet-owner"]
	owner.Address = "0x" + strings.Repeat("a8", 20)
	fixture.roles.EVM["testnet-owner"] = owner
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil || calls.Load() != 5 {
		t.Fatalf("changed custody reused old proof: calls=%d error=%v", calls.Load(), err)
	}
}

// Strict readers always execute their ordinary validator, even if the same
// configuration previously accumulated a successful provisional projection.
func TestScenarioCampaignRecoveryChainCacheNeverAuthorizesFinalAcceptance(t *testing.T) {
	t.Parallel()
	fixture, _, _, files := campaignRecoveryChainCacheFixture(t)
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err != nil {
		t.Fatal(err)
	}
	calls := 0
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls++
		if len(prefix) != 0 {
			t.Fatal("strict read received an authenticated provisional prefix")
		}
		return readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
	}
	fixture.cfg.provisionalResume.Record.FinalAcceptance = true
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil || calls != 0 {
		t.Fatalf("final acceptance consumed provisional memo: calls=%d error=%v", calls, err)
	}
	fixture.cfg.provisionalResume = nil
	for index := 0; index < 2; index++ {
		_, actualErr := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read)
		_, wantErr := readScenarioCampaignRecoveryChainFrom(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, nil)
		if (actualErr == nil) != (wantErr == nil) || actualErr != nil && actualErr.Error() != wantErr.Error() {
			t.Fatalf("strict verdict changed: actual=%v want=%v", actualErr, wantErr)
		}
	}
	if calls != 2 {
		t.Fatalf("strict validation was cached: calls=%d", calls)
	}
}

// A source changed after validation cannot become the cache's new witness for
// an older success. The following read must authenticate it and reject it.
func TestScenarioCampaignRecoveryChainCacheMutationDuringReadIsNotStored(t *testing.T) {
	t.Parallel()
	fixture, _, runDir, files := campaignRecoveryChainCacheFixture(t)
	calls := 0
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls++
		records, err := readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
		if err == nil && calls == 1 {
			if err := os.WriteFile(filepath.Join(runDir, "result.json"), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return records, err
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil {
		t.Fatal(err)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil || calls != 2 {
		t.Fatalf("source mutation was bound to an older success: calls=%d error=%v", calls, err)
	}
}

// A deterministic mutation barrier during suffix validation must reject the
// combined result instead of attributing changed history to an older proof.
func TestScenarioCampaignRecoveryChainCacheMutationDuringPrefixReuse(t *testing.T) {
	t.Parallel()
	fixture, current, runDir, files := campaignRecoveryChainCacheFixture(t)
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err != nil {
		t.Fatal(err)
	}
	_, files = extendCampaignRecoveryChainCacheFixture(t, fixture, current)
	calls := 0
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls++
		if calls == 1 && len(prefix) != 2 || calls == 2 && len(prefix) != 0 {
			t.Fatalf("incorrect retained prefix after mutation: calls=%d retained=%d", calls, len(prefix))
		}
		records, err := readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
		if err == nil && calls == 1 {
			if err := os.WriteFile(filepath.Join(runDir, "result.json"), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return records, err
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil || !strings.Contains(err.Error(), "changed during authenticated prefix reuse") {
		t.Fatalf("source changed across reused proof: %v", err)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil || calls != 2 {
		t.Fatalf("mutated prefix was cached for retry: calls=%d error=%v", calls, err)
	}
}

// An altered current signed attempt changes the selected chain even without
// adding a generation. A reordered selector cannot reuse the old projection.
func TestScenarioCampaignRecoveryChainCacheCurrentAttemptAndSelection(t *testing.T) {
	t.Parallel()
	fixture, _, _, files := campaignRecoveryChainCacheFixture(t)
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err != nil {
		t.Fatal(err)
	}
	selected := append([]scenarioCampaignRecoveryFile(nil), files...)
	selected[0], selected[1] = selected[1], selected[0]
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, selected); err == nil {
		t.Fatal("reordered chain selector reused old proof")
	}
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[1].path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err == nil {
		t.Fatal("changed current attempt reused old proof")
	}
}

// Mutable unexported route approvals are included despite their omission from
// persisted configuration JSON and the existing approval's ConfigHash.
func TestScenarioCampaignRecoveryChainCacheContextBindsRoutesAndApprovals(t *testing.T) {
	t.Parallel()
	fixture, _, _, _ := campaignRecoveryChainCacheFixture(t)
	want, err := scenarioCampaignRecoveryChainContext(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ResolvedConfig){
		func(cfg *ResolvedConfig) { cfg.ownedRPCAuthority = "192.0.2.30:9944" },
		func(cfg *ResolvedConfig) { cfg.provisionalRPCAuthority = "192.0.2.31:9944" },
		func(cfg *ResolvedConfig) { cfg.relayCapturePlanHash = "changed-capture-approval" },
		func(cfg *ResolvedConfig) { cfg.provisionalResume.AcceptedPlanHashes = []string{"changed-ancestor"} },
		func(cfg *ResolvedConfig) { cfg.provisionalResume.RecordHash = "changed-invocation" },
		func(cfg *ResolvedConfig) { cfg.provisionalResume.Driver.ExecutablePath = "changed-driver" },
	} {
		copied := *fixture.cfg
		invocation := *copied.provisionalResume
		copied.provisionalResume = &invocation
		change(&copied)
		actual, err := scenarioCampaignRecoveryChainContext(&copied, fixture.stateDir, fixture.roles, fixture.current.PlanHash)
		if err != nil || actual == want {
			t.Fatalf("changed context did not invalidate: actual=%s error=%v", actual, err)
		}
	}
	if got, err := scenarioCampaignRecoveryChainContext(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("context mutation escaped its copy: %s %v", got, err)
	}
}

// Cache bounds and filesystem safety remain explicit even for empty regular
// files, absent paths, symlinks and incomplete decoder output.
func TestScenarioCampaignRecoveryChainCacheBoundsAndUnsafeWitnesses(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if witness, ok := scenarioCampaignRecoverySourceWitness(stateDir, "source.json", false); !ok || witness.Mode != 0 {
		t.Fatalf("missing source must have an explicit negative witness: %+v %v", witness, ok)
	}
	if _, ok := scenarioCampaignRecoverySourceWitness(stateDir, "source.json", true); ok {
		t.Fatal("missing directory received a safe source witness")
	}
	path := filepath.Join(stateDir, "source.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if witness, ok := scenarioCampaignRecoverySourceWitness(stateDir, "source.json", false); !ok || witness.Mode == 0 || witness.Size != 0 {
		t.Fatalf("empty file must differ from a missing file: %+v %v", witness, ok)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, ok := scenarioCampaignRecoverySourceWitness(stateDir, "source.json", false); ok {
		t.Fatal("writable-by-others source received a safe witness")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target.json", path); err != nil {
		t.Fatal(err)
	}
	if _, ok := scenarioCampaignRecoverySourceWitness(stateDir, "source.json", false); ok {
		t.Fatal("source symlink received a safe witness")
	}
	if _, err := scenarioCampaignRecoveryProject([]scenarioCampaignRecoveryRecord{{}}); err == nil {
		t.Fatal("incomplete reader output produced a successful projection")
	}
	record := scenarioCampaignRecoveryRecord{attempt: &scenarioCampaignAttempt{cfg: newCampaignSuccessionFixture(t).cfg}, raw: make([]byte, scenarioCampaignRecoveryChainCacheBytes)}
	if _, err := scenarioCampaignRecoveryProject([]scenarioCampaignRecoveryRecord{record}); err == nil || !strings.Contains(err.Error(), "cache byte bound") {
		t.Fatal("payload plus envelope exceeded the cache bound without rejection")
	}
}

// A partial decoder result cannot panic the cache or retain an attempt that
// lacks the governed policy needed to expand a historical generation.
func TestScenarioCampaignRecoveryChainCacheRejectsIncompletePolicy(t *testing.T) {
	t.Parallel()
	for _, cfg := range []*ResolvedConfig{nil, {}} {
		record := scenarioCampaignRecoveryRecord{attempt: &scenarioCampaignAttempt{cfg: cfg}, raw: []byte("{}")}
		if _, err := scenarioCampaignRecoveryProject([]scenarioCampaignRecoveryRecord{record}); err == nil || !strings.Contains(err.Error(), "authenticated policy") {
			t.Fatalf("incomplete policy became a reusable projection: %v", err)
		}
	}
}

// Real owner-signed recovery sources exercise reuse without allowing cached
// verdicts to replace changed evidence or newly appended journal validation.
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The second generation inherits a failed pre-acceptance run and consequently
// requires both immutable ancestor evidence and the still-growing journal.
func newCampaignRecoveryCacheFixture(t *testing.T) (*campaignSuccessionFixture, *scenarioCampaignAttempt, *scenarioCampaignAttempt, string) {
	t.Helper()
	fixture := newCampaignSuccessionFixture(t)
	root, runDir := bindCampaignRecoveryFixture(t, fixture)
	first, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	bindPreAcceptanceFailedRecoveryGeneration(t, fixture, first)
	second, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(2*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, root, second, runDir
}

// Repeated lifecycle consumers reuse one proof, while writes in the current
// run and unrelated live state do not replay the historical observation log.
func TestScenarioCampaignRecoveryCacheHitAndCurrentRunProgress(t *testing.T) {
	t.Parallel()
	fixture, root, attempt, _ := newCampaignRecoveryCacheFixture(t)
	calls := 0
	validate := func(candidate *scenarioCampaignAttempt) error {
		calls++
		return validateScenarioCampaignRecovery(candidate)
	}
	ancestors, err := scenarioCampaignRecoveryAncestors(attempt, validate)
	if err != nil || !ancestors[root.payload.RunID] || !ancestors[attempt.payload.Recovery.PriorRunID] {
		t.Fatalf("first authenticated proof: ancestors=%v error=%v", ancestors, err)
	}
	delete(ancestors, root.payload.RunID)
	currentRun := filepath.Join(fixture.stateDir, "runs", attempt.payload.RunID)
	if err := os.MkdirAll(currentRun, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(currentRun, "observations.jsonl"), []byte("active run is still collecting\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.stateDir, "unrelated-live.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ancestors, err = scenarioCampaignRecoveryAncestors(attempt, validate)
	if err != nil || calls != 1 || !ancestors[root.payload.RunID] {
		t.Fatalf("unchanged history was not reused: calls=%d ancestors=%v error=%v", calls, ancestors, err)
	}
	if err := validateScenarioCampaignRecoveryAncestor(attempt, root.payload.RunID); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignRecoveryAncestor(attempt, "unrelated-run"); err == nil {
		t.Fatal("cached proof accepted a run outside its authenticated ancestry")
	}
}

// An in-place same-size rewrite with restored mtime must still miss by ctime,
// then fail the original signed-source validator instead of trusting the cache.
func TestScenarioCampaignRecoveryCacheWitnessMutationRevalidates(t *testing.T) {
	t.Parallel()
	fixture, root, attempt, runDir := newCampaignRecoveryCacheFixture(t)
	calls := 0
	validate := func(candidate *scenarioCampaignAttempt) error {
		calls++
		return validateScenarioCampaignRecovery(candidate)
	}
	if _, err := scenarioCampaignRecoveryAncestors(attempt, validate); err != nil {
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
	if bytes.Equal(raw, changed) || len(raw) != len(changed) {
		t.Fatal("fixture mutation must change source bytes without changing file size")
	}
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := scenarioCampaignRecoveryAncestors(attempt, validate); err == nil {
			t.Fatal("mutated signed predecessor was accepted")
		}
	}
	if calls != 3 {
		t.Fatalf("source mutation or failed verdict was cached: calls=%d", calls)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scenarioCampaignRecoveryAncestors(attempt, validate); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scenarioCampaignAttemptPath(fixture.stateDir, "production-soak"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateScenarioCampaignRecoveryAncestor(attempt, root.payload.RunID); err == nil || !strings.Contains(err.Error(), "production descendant") {
		t.Fatalf("new production descendant did not close cached ancestry: %v", err)
	}
}

// Valid appends keep immutable work reusable. A subsequent invalid hash-chain
// record is observed immediately, even though all signed old prefixes match.
func TestScenarioCampaignRecoveryCacheJournalAppendFreshValidation(t *testing.T) {
	t.Parallel()
	fixture, _, attempt, _ := newCampaignRecoveryCacheFixture(t)
	calls := 0
	validate := func(candidate *scenarioCampaignAttempt) error {
		calls++
		return validateScenarioCampaignRecovery(candidate)
	}
	if _, err := scenarioCampaignRecoveryAncestors(attempt, validate); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.Append(JournalEntry{DeploymentID: fixture.current.DeploymentID, PlanHash: fixture.current.PlanHash, ActionID: "cache-test-progress", IntentHash: "0x" + strings.Repeat("b7", 32), Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	if _, err := scenarioCampaignRecoveryAncestors(attempt, validate); err != nil || calls != 1 {
		t.Fatalf("valid journal append replayed immutable verification: calls=%d error=%v", calls, err)
	}
	file, err := os.OpenFile(filepath.Join(fixture.stateDir, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("{}\n")
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := scenarioCampaignRecoveryAncestors(attempt, validate); err == nil || !strings.Contains(err.Error(), "hash mismatch") || calls != 2 {
		t.Fatalf("cache hid an invalid newly appended journal record: calls=%d error=%v", calls, err)
	}
}

// Independent lifecycle readers share exactly one full validation and receive
// independent ancestor maps. A copied attempt still invalidates changed context.
func TestScenarioCampaignRecoveryCacheConcurrentReadersAndContext(t *testing.T) {
	t.Parallel()
	fixture, root, attempt, _ := newCampaignRecoveryCacheFixture(t)
	var calls atomic.Int64
	validate := func(candidate *scenarioCampaignAttempt) error {
		calls.Add(1)
		return validateScenarioCampaignRecovery(candidate)
	}
	var workers sync.WaitGroup
	errorsCh := make(chan error, 8)
	start := make(chan struct{})
	for index := 0; index < cap(errorsCh); index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			ancestors, err := scenarioCampaignRecoveryAncestors(attempt, validate)
			if err == nil && !ancestors[root.payload.RunID] {
				err = errors.New("concurrent proof lost its root")
			}
			delete(ancestors, root.payload.RunID)
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
		t.Fatalf("concurrent readers replayed validation %d times", calls.Load())
	}
	copied := *attempt
	config := *fixture.cfg
	config.ConfigHash = "0x" + strings.Repeat("ca", 32)
	copied.cfg = &config
	if _, err := scenarioCampaignRecoveryAncestors(&copied, validate); err == nil || calls.Load() != 2 {
		t.Fatalf("copied attempt reused a different configuration: calls=%d error=%v", calls.Load(), err)
	}
}

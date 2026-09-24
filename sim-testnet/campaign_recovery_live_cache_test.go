// Live campaign progress changes only the current signed generation and the
// journal suffix; deterministic barriers prove old history stays reusable.
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A normal observation checkpoint reauthenticates the tail, without replaying
// immutable generations or trusting the old current-attempt payload.
func TestScenarioCampaignRecoveryChainCacheCurrentProgressReusesPrefix(t *testing.T) {
	t.Parallel()
	fixture, current, _, files := campaignRecoveryChainCacheFixture(t)
	var retainedCounts []int
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		retainedCounts = append(retainedCounts, len(prefix))
		return readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil {
		t.Fatal(err)
	}
	if err := current.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.Append(JournalEntry{DeploymentID: fixture.current.DeploymentID, PlanHash: fixture.current.PlanHash, ActionID: "active-campaign-progress", IntentHash: "0x" + strings.Repeat("a9", 32), Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		records, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read)
		if err != nil || len(records) != 2 || !records[1].attempt.payload.PreparationComplete || !reflect.DeepEqual(retainedCounts, []int{0, 1}) {
			t.Fatalf("live checkpoint replayed history or retained stale tail: prefixes=%v records=%d error=%v", retainedCounts, len(records), err)
		}
	}
}

// The active writer may checkpoint while a cold history audit is running.
// That changes no old proof; retain it, then reauthenticate the changed tail.
func TestScenarioCampaignRecoveryChainCacheConcurrentProgressRetainsPrefix(t *testing.T) {
	t.Parallel()
	fixture, current, _, files := campaignRecoveryChainCacheFixture(t)
	var retainedCounts []int
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		retainedCounts = append(retainedCounts, len(prefix))
		records, err := readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
		if err == nil && len(retainedCounts) == 1 {
			// Model an independently authenticated writer. Calling the public
			// updater here would recursively acquire this test's cache lock.
			current.payload.PreparationComplete = true
			if err := writeScenarioCampaignAttempt(current); err != nil {
				t.Fatal(err)
			}
			if err := fixture.journal.Append(JournalEntry{DeploymentID: fixture.current.DeploymentID, PlanHash: fixture.current.PlanHash, ActionID: "concurrent-campaign-progress", IntentHash: "0x" + strings.Repeat("aa", 32), Stage: StageIntent}); err != nil {
				t.Fatal(err)
			}
		}
		return records, err
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err != nil {
		t.Fatal(err)
	}
	records, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read)
	if err != nil || len(records) != 2 || !records[1].attempt.payload.PreparationComplete || !reflect.DeepEqual(retainedCounts, []int{0, 1}) {
		t.Fatalf("concurrent checkpoint discarded immutable proof: prefixes=%v records=%d error=%v", retainedCounts, len(records), err)
	}
}

// Current progress cannot excuse an altered historical envelope or source.
// Such a mutation must restart authentication and preserve the real rejection.
func TestScenarioCampaignRecoveryChainCacheCurrentProgressRejectsChangedHistory(t *testing.T) {
	t.Parallel()
	fixture, current, runDir, files := campaignRecoveryChainCacheFixture(t)
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err != nil {
		t.Fatal(err)
	}
	if err := current.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "result.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		calls++
		if len(prefix) != 0 {
			t.Fatal("changed historical source retained a cached prefix")
		}
		return readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil || calls != 1 {
		t.Fatalf("changed history escaped authentication: calls=%d error=%v", calls, err)
	}
}

// A malformed or interrupted current checkpoint is never accepted, but its
// repair should not discard the independently authenticated old generations.
func TestScenarioCampaignRecoveryChainCacheFailedTailRetainsPrefix(t *testing.T) {
	t.Parallel()
	fixture, current, _, files := campaignRecoveryChainCacheFixture(t)
	if _, err := readScenarioCampaignRecoveryChain(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[len(files)-1].path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var retainedCounts []int
	read := func(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash string, selected []scenarioCampaignRecoveryFile, prefix []scenarioCampaignRecoveryRecord) ([]scenarioCampaignRecoveryRecord, error) {
		retainedCounts = append(retainedCounts, len(prefix))
		return readScenarioCampaignRecoveryChainFrom(cfg, stateDir, roles, planHash, selected, prefix)
	}
	if _, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read); err == nil {
		t.Fatal("malformed current envelope became a successful chain")
	}
	if err := writeScenarioCampaignAttempt(current); err != nil {
		t.Fatal(err)
	}
	records, err := readScenarioCampaignRecoveryChainMemo(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, files, read)
	if err != nil || len(records) != 2 || !reflect.DeepEqual(retainedCounts, []int{1, 1}) {
		t.Fatalf("failed tail discarded authenticated history: prefixes=%v records=%d error=%v", retainedCounts, len(records), err)
	}
}

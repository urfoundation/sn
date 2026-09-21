package main

// Count production reconciliation reads over synthetic approvals. No timing,
// external node, executable cache entry or transaction sender proves progress.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type carriedPreparationTest struct {
	executor *Executor
	source   *SetupPlan
	entries  []JournalEntry
	records  []*ActionPostcondition
}

func TestCarriedActionVerificationWorkersBoundOwnedLANArchiveReads(t *testing.T) {
	t.Parallel()
	if got := carriedActionVerificationWorkersFor(&ResolvedConfig{OperationalRPCMode: rpcModeOwnedNode}); got != carriedActionOwnedVerificationWorkers {
		t.Fatalf("owned LAN verification workers=%d want=%d", got, carriedActionOwnedVerificationWorkers)
	}
	if got := carriedActionVerificationWorkersFor(&ResolvedConfig{OperationalRPCMode: rpcModePublicOverride}); got != carriedActionVerificationWorkers {
		t.Fatalf("public verification workers=%d want=%d", got, carriedActionVerificationWorkers)
	}
	if got := carriedActionVerificationWorkersFor(nil); got != carriedActionVerificationWorkers {
		t.Fatalf("default verification workers=%d want=%d", got, carriedActionVerificationWorkers)
	}
}

func TestPlanActionIndexUsesApprovedFirstAction(t *testing.T) {
	first := Action{ID: "fleet.renew.1.1.commitment", IntentHash: "first"}
	second := Action{ID: first.ID, IntentHash: "second"}
	index := planActionIndex(&SetupPlan{Actions: []Action{first, second}})
	if got, ok := index[first.ID]; !ok || got.ID != first.ID || got.IntentHash != first.IntentHash {
		t.Fatalf("indexed action=%+v found=%t, want first approved action", got, ok)
	}
	if got := planActionIndex(nil); got != nil {
		t.Fatalf("nil plan index=%v, want nil", got)
	}
}

func TestVerifyCarriedActionWithTimeoutBoundsCachedFleetReads(t *testing.T) {
	start := time.Now()
	err := verifyCarriedActionWithTimeout(t.Context(), func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("cached fleet verification has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining > carriedActionVerificationTimeout || remaining < carriedActionVerificationTimeout-time.Second {
			return fmt.Errorf("cached fleet verification deadline remaining=%s, want about %s", remaining, carriedActionVerificationTimeout)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("bounded cached fleet verification returned too slowly: %s", elapsed)
	}
}

// The archived v1 approval exercises the real historical decoder and owned
// receipt migration without unrelated deployment or network prerequisites.
func newCarriedPreparationTest(t *testing.T, count int) carriedPreparationTest {
	t.Helper()
	sourceConfig := ownedRPCSourceConfigTest(t)
	sourceConfig.Authority = "synthetic-authority.example:12345"
	sourceConfig.ObjectStoreHost = "synthetic-object-store.example"
	inputsHash, err := resolvedInputsHash(sourceConfig)
	if err != nil {
		t.Fatal(err)
	}
	source := &SetupPlan{
		Schema: "urnetwork-sim-plan-v1", Release: "1.0", DeploymentID: sourceConfig.Config.Deployment.DeploymentID,
		ChainID: sourceConfig.ChainID, GenesisHash: testnetGenesis, Netuid: sourceConfig.Netuid, Owner: sourceConfig.WalletPublic,
		ConfigHash: sourceConfig.ConfigHash, PolicyHash: sourceConfig.PolicyHash, ResolvedInputsHash: inputsHash,
		Limits: configuredPlanLimits(sourceConfig),
	}
	for index := 0; index < count; index++ {
		action := Action{ID: fmt.Sprintf("synthetic.reserve.%03d", index), Kind: "budget-reserve", Target: "synthetic-reserve", Spend: Spend{TAORao: 1}}
		action.IntentHash, err = actionIntentHash(action)
		if err != nil {
			t.Fatal(err)
		}
		source.Actions = append(source.Actions, action)
	}
	source.MaximumSpend, err = maximumActionSpend(source.Actions)
	if err != nil {
		t.Fatal(err)
	}
	source.PlanHash, err = source.hash()
	if err != nil {
		t.Fatal(err)
	}
	// Admission requires a private IPv4 address; derive a test-only value and
	// never dial it. No deployment address is copied into this fixture.
	address := sha256.Sum256([]byte("synthetic carried preparation transport"))
	authority := fmt.Sprintf("10.%d.%d.%d:12345", address[0], address[1], address[2])
	config, err := prepareOwnedRPCConfiguration(sourceConfig, authority)
	if err != nil {
		t.Fatal(err)
	}
	current := *source
	current.PriorPlanHashes = []string{source.PlanHash}
	current.OwnedRPCAuthority = authority
	current.ResolvedInputsHash, err = resolvedInputsHash(config)
	if err != nil {
		t.Fatal(err)
	}
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plans", stringsTrim0x(source.PlanHash)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readValidatorEvidenceHistoricalPlan(stateDir, source.PlanHash); err != nil {
		t.Fatalf("synthetic source must pass the full historical decoder: %v", err)
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture := carriedPreparationTest{executor: &Executor{cfg: config, plan: &current, stateDir: stateDir, journal: journal}, source: source}
	head := testEVMHead(20, 0x35)
	for _, action := range source.Actions {
		record := &ActionPostcondition{
			Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: source.DeploymentID,
			PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
			OperationalRPCMode: rpcModePublicOverride, IndependentRPC: false,
			SubstrateFinalized: head, EVMFinalized: head, EVMHashDomain: "evm-rpc", Observed: map[string]any{"synthetic": true},
			IndependentSubstrateFinalized: head, IndependentEVMFinalized: head, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: map[string]any{"synthetic": true},
		}
		path, hash, err := fixture.executor.persistActionPostcondition(record)
		if err != nil {
			t.Fatal(err)
		}
		entry := JournalEntry{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
			Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}
		if err := journal.Append(entry); err != nil {
			t.Fatal(err)
		}
		fixture.records = append(fixture.records, record)
	}
	fixture.entries = journal.Entries()
	return fixture
}

// The real collector must take two linear snapshots, independent of the
// number of actions: one to index and one to fence successful reconciliation.
func TestCarriedPreparationJournalReadWorkIsBounded(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 64)
	executor := fixture.executor
	reads, copied := 0, 0
	readEntries := func() []JournalEntry {
		entries := executor.journal.Entries()
		reads++
		copied += len(entries)
		return entries
	}
	if err := executor.collectCarriedActionHistoryWithReaders(t.Context(), readEntries, readValidatorEvidenceHistoricalPlan); err != nil {
		t.Errorf("actual carried reconciliation: %v", err)
	}
	if reads != 2 || copied != 2*len(fixture.entries) {
		t.Errorf("journal work grew per action: reads=%d copied=%d entries=%d", reads, copied, len(fixture.entries))
	}
	if len(executor.carriedVerificationKeys) != len(fixture.entries) {
		t.Errorf("authenticated keys=%d want=%d", len(executor.carriedVerificationKeys), len(fixture.entries))
	}
	if !reflect.DeepEqual(fixture.entries, executor.journal.Entries()) {
		t.Error("reconciliation changed original journal custody")
	}
}

// Every receipt still goes through its complete hash and RPC identity checks;
// only the same immutable source plan's full decode is shared.
func TestCarriedPreparationSourceReadWorkIsBounded(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 64)
	executor := fixture.executor
	reads := 0
	readSource := func(stateDir, hash string) (*SetupPlan, error) {
		reads++
		return readValidatorEvidenceHistoricalPlan(stateDir, hash)
	}
	if err := executor.collectCarriedActionHistoryWithReaders(t.Context(), executor.journal.Entries, readSource); err != nil {
		t.Errorf("actual owned-history reconciliation: %v", err)
	}
	if reads != 1 {
		t.Errorf("one immutable source decoded %d times for %d receipts", reads, len(fixture.entries))
	}
	for _, entry := range fixture.entries {
		if !executor.carriedVerificationKeys[carriedVerificationKey(entry)] {
			t.Errorf("missing exact verified key for %s", entry.ActionID)
		}
	}
}

// Distinct admitted ancestors never share a decoded source just because the
// receipt belongs to the same action or has the same original RPC label.
func TestCarriedPreparationAuthenticatesEachSourcePlan(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 4)
	executor := fixture.executor
	second := *fixture.source
	second.Limits.TAORao--
	config := *executor.cfg
	config.ownedRPCAuthority = ""
	var err error
	config.OperationalSubstrate, config.OperationalEVM, config.OperationalRPCMode, err = resolveOperationalRPCs(config.Authority, config.Config.LaunchInputs.PublicSubstrateRPCOverride, config.Config.LaunchInputs.PublicEVMRPCOverride)
	if err != nil {
		t.Fatal(err)
	}
	config.MaximumTAORao = second.Limits.TAORao
	second.ResolvedInputsHash, err = resolvedInputsHash(&config)
	if err != nil {
		t.Fatal(err)
	}
	second.PlanHash, err = second.hash()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(executor.stateDir, "plans", stringsTrim0x(second.PlanHash)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	executor.plan.PriorPlanHashes = append(executor.plan.PriorPlanHashes, second.PlanHash)
	executor.plan.PlanHash, err = executor.plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	var replacement JournalEntry
	for index := 0; index < 2; index++ {
		record := *fixture.records[index]
		record.PlanHash = second.PlanHash
		path, hash, err := executor.persistActionPostcondition(&record)
		if err != nil {
			t.Fatal(err)
		}
		replacement = JournalEntry{DeploymentID: record.DeploymentID, PlanHash: record.PlanHash, ActionID: record.ActionID, IntentHash: record.IntentHash,
			Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}
		if err := executor.journal.Append(replacement); err != nil {
			t.Fatal(err)
		}
	}
	reads := map[string]int{}
	readSource := func(stateDir, hash string) (*SetupPlan, error) {
		reads[hash]++
		return readValidatorEvidenceHistoricalPlan(stateDir, hash)
	}
	if err := executor.collectCarriedActionHistoryWithReaders(t.Context(), executor.journal.Entries, readSource); err != nil {
		t.Errorf("two-source reconciliation: %v", err)
	}
	if len(reads) != 2 || reads[fixture.source.PlanHash] == 0 || reads[second.PlanHash] == 0 {
		t.Errorf("original source authority was collapsed: %v", reads)
	}
	if len(executor.carriedVerificationKeys) != 4 || !executor.carriedVerificationKeys[carriedVerificationKey(replacement)] {
		t.Error("complete admitted source lineage lost its exact latest receipts")
	}
	if executor.carriedVerificationKeys[carriedVerificationKey(fixture.entries[0])] || executor.carriedVerificationKeys[carriedVerificationKey(fixture.entries[1])] {
		t.Error("superseded source receipts won over later authenticated entries")
	}
}

// Compare the index to the unchanged execution reader across all lineage and
// intent combinations, including topology's stricter current-plan rule.
func TestCarriedPreparationIndexPreservesJournalSelection(t *testing.T) {
	plan := &SetupPlan{PlanHash: "current", PriorPlanHashes: []string{"older", "recent"}}
	var entries []JournalEntry
	for index := 0; index < 24; index++ {
		plan.Actions = append(plan.Actions, Action{ID: fmt.Sprintf("synthetic.%d", index), IntentHash: "current-intent", AcceptedPriorIntentHashes: []string{"older-intent", "recent-intent"}})
	}
	plan.Actions = append(plan.Actions, Action{ID: "topology.launch", IntentHash: "current-intent", AcceptedPriorIntentHashes: []string{"older-intent", "recent-intent"}})
	for _, hash := range []string{"older", "current", "recent", "unapproved"} {
		for _, action := range plan.Actions {
			for _, intent := range []string{"current-intent", "older-intent", "recent-intent", "unapproved-intent"} {
				for _, stage := range []JournalStage{StageVerified, StageFailed, StageFinalized} {
					entries = append(entries, JournalEntry{PlanHash: hash, ActionID: action.ID, IntentHash: intent, Stage: stage, Sequence: uint64(10000 - len(entries))})
				}
			}
		}
	}
	journal := &Journal{entries: entries}
	executor := &Executor{plan: plan, journal: journal}
	index := newCarriedPreparationIndex(plan, journal.Entries())
	for _, action := range append(plan.Actions, Action{ID: "absent", IntentHash: "current-intent"}) {
		for _, includeTopology := range []bool{false, true} {
			want, wantFound := executor.verifiedActionEntryForScope(action, includeTopology)
			got, found := index.find(action, includeTopology)
			if found != wantFound || got != want {
				t.Errorf("%s topology-history=%t selection differs: got=%+v/%t want=%+v/%t", action.ID, includeTopology, got, found, want, wantFound)
			}
		}
	}
}

// Success from an earlier invocation cannot hide changed on-disk authority.
// Independent intact receipts still receive keys after one receipt fails.
func TestCarriedPreparationRetryReauthenticatesSourceAndReceipts(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 4)
	executor := fixture.executor
	if err := executor.collectCarriedActionHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(executor.stateDir, "plans", stringsTrim0x(fixture.source.PlanHash)+".json")
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	changed := *fixture.source
	changed.Owner = "synthetic-changed-owner"
	raw, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(sourcePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.collectCarriedActionHistory(t.Context()); err == nil || !strings.Contains(err.Error(), "plan hash mismatch") {
		t.Errorf("changed source bypassed full historical authentication: %v", err)
	}
	if len(executor.carriedVerificationKeys) != 0 {
		t.Error("corrupt source inherited prior reconciliation keys")
	}
	if err := atomicWrite(sourcePath, sourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(executor.stateDir, fixture.entries[1].PostconditionPath)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	record := *fixture.records[1]
	record.Observed, record.IndependentObserved = map[string]any{"changed": true}, map[string]any{"changed": true}
	raw, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.collectCarriedActionHistory(t.Context()); err == nil || !strings.Contains(err.Error(), "persisted postcondition hash") {
		t.Errorf("changed receipt borrowed cached source authority: %v", err)
	}
	for index, entry := range fixture.entries {
		if executor.carriedVerificationKeys[carriedVerificationKey(entry)] != (index != 1) {
			t.Errorf("independent receipt %d has the wrong verification result", index)
		}
	}
	if err := atomicWrite(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.collectCarriedActionHistory(t.Context()); err != nil || len(executor.carriedVerificationKeys) != len(fixture.entries) {
		t.Errorf("restored authority did not receive a fresh complete audit: %v", err)
	}
}

// A new reconciliation owns its current route and ancestry even when called
// on the same executor object after a previous successful invocation.
func TestCarriedPreparationRetryRechecksScopeAndAuthority(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 3)
	executor := fixture.executor
	for _, fault := range []string{"resolved-input", "route", "deployment", "ancestry", "current-plan"} {
		if err := executor.collectCarriedActionHistory(t.Context()); err != nil {
			t.Fatal(err)
		}
		config, plan := *executor.cfg, *executor.plan
		savedConfig, savedPlan := executor.cfg, executor.plan
		executor.cfg, executor.plan = &config, &plan
		switch fault {
		case "resolved-input":
			config.ObjectStoreHost = "different-synthetic-store.example"
		case "route":
			config.OperationalEVM = "http://192.0.2.45:12345"
		case "deployment":
			plan.DeploymentID = "another-synthetic-deployment"
		case "ancestry":
			plan.PriorPlanHashes = nil
		case "current-plan":
			plan.PlanHash = fixture.source.PlanHash
		}
		err := executor.collectCarriedActionHistory(t.Context())
		if fault != "ancestry" && fault != "current-plan" && err == nil {
			t.Errorf("%s retained authority from another scope", fault)
		}
		if len(executor.carriedVerificationKeys) != 0 {
			t.Errorf("%s inherited old reconciliation keys", fault)
		}
		executor.cfg, executor.plan = savedConfig, savedPlan
	}
}

// Explicitly append while the source reader is active. The incomplete scope
// publishes no keys, and retry selects the new current-plan receipt freshly.
func TestCarriedPreparationJournalAppendInvalidatesScope(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 3)
	executor := fixture.executor
	appended := false
	var current JournalEntry
	readSource := func(stateDir, hash string) (*SetupPlan, error) {
		source, err := readValidatorEvidenceHistoricalPlan(stateDir, hash)
		if err != nil || appended {
			return source, err
		}
		record := *fixture.records[0]
		record.PlanHash, record.OperationalRPCMode = executor.plan.PlanHash, executor.cfg.OperationalRPCMode
		path, hash, err := executor.persistActionPostcondition(&record)
		if err != nil {
			return nil, err
		}
		current = JournalEntry{DeploymentID: record.DeploymentID, PlanHash: record.PlanHash, ActionID: record.ActionID, IntentHash: record.IntentHash,
			Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}
		if err := executor.journal.Append(current); err != nil {
			return nil, err
		}
		appended = true
		return source, nil
	}
	if err := executor.collectCarriedActionHistoryWithReaders(t.Context(), executor.journal.Entries, readSource); err == nil || !strings.Contains(err.Error(), "journal changed during reconciliation") {
		t.Errorf("changed snapshot published a complete reconciliation: %v", err)
	}
	if !appended || len(executor.carriedVerificationKeys) != 0 {
		t.Errorf("append boundary was not exercised or retained old keys: appended=%t keys=%d", appended, len(executor.carriedVerificationKeys))
	}
	if err := executor.collectCarriedActionHistory(t.Context()); err != nil {
		t.Errorf("fresh journal retry: %v", err)
	}
	if len(executor.carriedVerificationKeys) != len(fixture.entries)-1 || executor.carriedVerificationKeys[carriedVerificationKey(fixture.entries[0])] {
		t.Error("retry failed to replace the superseded ancestor receipt")
	}
	if err := atomicWrite(filepath.Join(executor.stateDir, current.PostconditionPath), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(t.Context(), executor.plan.Actions[0]); err == nil {
		t.Error("current-plan receipt escaped execution revalidation")
	}
}

// Cancellation after the actual source read must discard its authority and
// all keys. A new call must perform that source read again before succeeding.
func TestCarriedPreparationCanceledSourceReadRequiresFreshRetry(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 3)
	executor := fixture.executor
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reads := 0
	readSource := func(stateDir, hash string) (*SetupPlan, error) {
		reads++
		source, err := readValidatorEvidenceHistoricalPlan(stateDir, hash)
		cancel()
		return source, err
	}
	if err := executor.collectCarriedActionHistoryWithReaders(ctx, executor.journal.Entries, readSource); !errors.Is(err, context.Canceled) {
		t.Errorf("source-read cancellation lost: %v", err)
	}
	if reads != 1 || len(executor.carriedVerificationKeys) != 0 {
		t.Errorf("canceled scope retained authority: reads=%d keys=%d", reads, len(executor.carriedVerificationKeys))
	}
	reads = 0
	readSource = func(stateDir, hash string) (*SetupPlan, error) {
		reads++
		return readValidatorEvidenceHistoricalPlan(stateDir, hash)
	}
	if err := executor.collectCarriedActionHistoryWithReaders(t.Context(), executor.journal.Entries, readSource); err != nil {
		t.Errorf("fresh source retry: %v", err)
	}
	if reads == 0 || len(executor.carriedVerificationKeys) != len(fixture.entries) {
		t.Errorf("retry did not rebuild complete authority: reads=%d keys=%d", reads, len(executor.carriedVerificationKeys))
	}
	if !reflect.DeepEqual(fixture.entries, executor.journal.Entries()) {
		t.Error("canceled read or retry changed the retained journal")
	}
}

func TestVerifyCarriedActionWithTimeoutExtendsOwnedLANHistoricalReads(t *testing.T) {
	err := verifyCarriedActionWithTimeoutFor(t.Context(), &ResolvedConfig{OperationalRPCMode: rpcModeOwnedNode}, func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("owned historical verification has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining > carriedActionOwnedVerificationTimeout || remaining < carriedActionOwnedVerificationTimeout-time.Second {
			return fmt.Errorf("owned deadline remaining=%s, want about %s", remaining, carriedActionOwnedVerificationTimeout)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCarriedActionWithTimeoutReturnsOwnedLANDeadlineWithoutRestarting(t *testing.T) {
	calls := 0
	err := verifyCarriedActionWithTimeoutFor(t.Context(), &ResolvedConfig{OperationalRPCMode: rpcModeOwnedNode}, func(context.Context) error {
		calls++
		if calls == 1 {
			return context.DeadlineExceeded
		}
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("owned deadline repeated its action: error=%v calls=%d", err, calls)
	}
}

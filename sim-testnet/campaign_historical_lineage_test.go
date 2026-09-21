// Synthetic signed chains prove that changing an approved driver/configuration
// appends a fresh recovery without rewriting or accepting historical evidence.
package main

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A real scenario-length change changes configuration and executable
// definition while preserving governed policy, installed contracts and custody.
func advanceCampaignLineageFixture(t *testing.T, fixture *campaignSuccessionFixture) {
	t.Helper()
	cfg := *fixture.cfg
	config := *cfg.Config
	config.Analysis.WriteHTML = !config.Analysis.WriteHTML
	config.Scenarios.ShortEpochs++
	cfg.Config = &config
	var err error
	cfg.ConfigHash, err = releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	plan := *fixture.current
	plan.PriorPlanHashes = append([]string{plan.PlanHash}, plan.PriorPlanHashes...)
	plan.ConfigHash = cfg.ConfigHash
	plan.Actions = append([]Action(nil), plan.Actions...)
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if action.ID == "config.render" || action.ID == "topology.launch" {
			action.Parameters = maps.Clone(action.Parameters)
			action.Parameters["config_hash"] = cfg.ConfigHash
			action.Parameters["policy_hash"] = cfg.PolicyHash
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	plan.ResolvedInputsHash, err = resolvedInputsHash(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture.cfg, fixture.current = &cfg, &plan
	fixture.writeCurrent(t)
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: plan.PlanHash, Provisional: true}}
}

// Preserve complete signed attempts, original run files and approvals rather
// than comparing only selected payload fields after recovery publication.
func retainedCampaignLineageBytes(t *testing.T, stateDir string) map[string][]byte {
	t.Helper()
	retainedKVs := map[string][]byte{}
	for _, directory := range []string{"campaign-attempts", "runs", "plans"} {
		if err := filepath.WalkDir(filepath.Join(stateDir, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				retainedKVs[path] = raw
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return retainedKVs
}

// The real entry point crosses an approved config change after multiple signed
// recoveries, binds a new run, and reopens it without a second publication.
func TestScenarioCampaignHistoricalLineageExtendsAndReopensExactChain(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	first, second, _ := createSecondCampaignRecovery(t, fixture)
	bindFailedRecoveryGeneration(t, fixture, second, 10)
	oldPlanHash, oldConfigHash := fixture.current.PlanHash, fixture.cfg.ConfigHash
	advanceCampaignLineageFixture(t, fixture)
	if oldConfigHash == fixture.cfg.ConfigHash || oldPlanHash == fixture.current.PlanHash {
		t.Fatal("fixture did not change approved configuration and plan")
	}
	retainedKVs := retainedCampaignLineageBytes(t, fixture.stateDir)
	journalBefore := readCampaignSuccessionFixtureBytes(t, filepath.Join(fixture.stateDir, "journal.jsonl"))
	planBefore := readCampaignSuccessionFixtureBytes(t, filepath.Join(fixture.stateDir, "plan.json"))
	if _, err := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0"); !errors.Is(err, errScenarioCampaignHistoricalLineage) {
		t.Fatalf("historical chain was admitted as a current run: %v", err)
	}
	attempt, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(3*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := scenarioCampaignRecoveryGeneration(attempt.payload.Recovery)
	if err != nil || generation != 3 || attempt.payload.ConfigHash != fixture.cfg.ConfigHash || attempt.payload.PolicyHash != fixture.cfg.PolicyHash || attempt.payload.PlanHash != fixture.current.PlanHash || attempt.payload.RunID == second.payload.RunID || attempt.cfg != fixture.cfg || attempt.cfg.readOnlyAudit {
		t.Fatalf("recovery did not retain current admission: generation=%d attempt=%+v err=%v", generation, attempt.payload, err)
	}
	if attempt.payload.Recovery.PriorRunID != second.payload.RunID || attempt.payload.Recovery.PriorAttemptSha256 != bytesSHA256(retainedKVs[second.path()]) || attempt.payload.Recovery.ApprovedPlanSha256 != bytesSHA256(planBefore) || attempt.payload.PreparationComplete || attempt.payload.AcceptanceBoundary != nil || attempt.payload.Recovery.InheritedPreparationSha256 != "" {
		t.Fatalf("recovery mixed old acceptance/preparation with current approval: %+v", attempt.payload)
	}
	for _, ancestor := range []string{first.payload.RunID, second.payload.RunID} {
		for range 2 {
			if err := validateScenarioCampaignRecoveryAncestor(attempt, ancestor); err != nil {
				t.Fatalf("cold/warm ancestor proof did not retain historical context: %v", err)
			}
		}
	}
	admission := *fixture.cfg.provisionalResume.Record
	for _, mutation := range []func(*provisionalResumeRecord){
		func(record *provisionalResumeRecord) { record.PlanHash = oldPlanHash },
		func(record *provisionalResumeRecord) { record.Provisional = false },
		func(record *provisionalResumeRecord) { record.FinalAcceptance = true },
	} {
		changed := admission
		mutation(&changed)
		fixture.cfg.provisionalResume.Record = &changed
		if err := validateScenarioCampaignRecoveryAncestor(attempt, second.payload.RunID); err == nil || !strings.Contains(err.Error(), "exact provisional non-accepting approval") {
			t.Fatalf("warm ancestor cache ignored changed admission %+v: %v", changed, err)
		}
	}
	fixture.cfg.provisionalResume.Record = &admission
	if err := validateScenarioCampaignAcceptanceBoundary(fixture.cfg, "release-1.0", second.payload.AcceptanceBoundary); err == nil || !strings.Contains(err.Error(), "acceptance geometry is not exact") {
		t.Fatalf("current acceptance accepted the old scenario definition: %v", err)
	}
	reopened, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(4*time.Hour), fixture.journal)
	if err != nil || !reflect.DeepEqual(reopened.payload, attempt.payload) || reopened.cfg != fixture.cfg {
		t.Fatalf("reopen created or substituted an attempt: %+v %v", reopened, err)
	}
	files, err := scenarioCampaignRecoveryFiles(fixture.stateDir)
	if err != nil || len(files) != 3 {
		t.Fatalf("recovery duplicated a generation: %v %v", files, err)
	}
	for path, before := range retainedKVs {
		if after := readCampaignSuccessionFixtureBytes(t, path); !bytes.Equal(before, after) {
			t.Fatalf("recovery rewrote historical source %s", path)
		}
	}
	if !bytes.Equal(journalBefore, readCampaignSuccessionFixtureBytes(t, filepath.Join(fixture.stateDir, "journal.jsonl"))) || !bytes.Equal(planBefore, readCampaignSuccessionFixtureBytes(t, filepath.Join(fixture.stateDir, "plan.json"))) {
		t.Fatal("campaign recovery changed the approved plan or transaction journal")
	}
	strict := *fixture.cfg
	strict.provisionalResume = nil
	if _, err := readScenarioCampaignAttempt(&strict, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0"); err == nil {
		t.Fatal("historical compatibility implicitly enabled strict acceptance")
	}
}

// The same transition works before a first recovery exists; no source namespace
// may be overwritten to make the old successor match the current approval.
func TestScenarioCampaignHistoricalLineageExtendsFailedSuccessor(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, _ := bindCampaignRecoveryFixture(t, fixture)
	advanceCampaignLineageFixture(t, fixture)
	before := readCampaignSuccessionFixtureBytes(t, prior.path())
	attempt, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil || attempt.payload.Recovery == nil || attempt.payload.Recovery.PriorAttemptSha256 != bytesSHA256(before) || attempt.payload.PlanHash != fixture.current.PlanHash {
		t.Fatalf("failed successor did not append a current recovery: %+v %v", attempt, err)
	}
	if !bytes.Equal(before, readCampaignSuccessionFixtureBytes(t, prior.path())) {
		t.Fatal("historical successor was rewritten")
	}
}

// Only named ancestors with identical governed and custody dimensions may
// cross an approval; common membership never makes sibling plans successive.
func TestScenarioCampaignHistoricalLineageRejectsSemanticAndMixedAncestors(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior := fixture.prior
	current := fixture.current
	if !scenarioCampaignLineagePlansMatch(current, prior) {
		t.Fatal("fixture has no compatible ancestor")
	}
	mutations := []struct {
		name string
		edit func(*SetupPlan)
	}{
		{"unknown", func(plan *SetupPlan) { plan.PriorPlanHashes = nil }},
		{"deployment", func(plan *SetupPlan) { plan.DeploymentID += "-other" }},
		{"chain", func(plan *SetupPlan) { plan.ChainID++ }},
		{"netuid", func(plan *SetupPlan) { plan.Netuid++ }},
		{"genesis", func(plan *SetupPlan) { plan.GenesisHash = "0x" + strings.Repeat("ab", 32) }},
		{"owner", func(plan *SetupPlan) { plan.Owner += "other" }},
		{"policy", func(plan *SetupPlan) { plan.PolicyHash = "0x" + strings.Repeat("ab", 32) }},
		{"custody", func(plan *SetupPlan) { plan.Roles.Owner += "other" }},
		{"contract", func(plan *SetupPlan) { plan.Deployment.SettlementVault[0] ^= 1 }},
	}
	for _, mutation := range mutations {
		copyPlan := *current
		mutation.edit(&copyPlan)
		if scenarioCampaignLineagePlansMatch(&copyPlan, prior) {
			t.Fatalf("accepted changed %s", mutation.name)
		}
	}
	if scenarioCampaignLineagePlansMatch(prior, current) {
		t.Fatal("accepted a future approval as an ancestor")
	}
	sibling := *current
	sibling.MaximumSpend.TAORao++
	var err error
	sibling.PlanHash, err = sibling.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(fixture.stateDir, "plans", stringsTrim0x(sibling.PlanHash)+".json"), &sibling); err != nil {
		t.Fatal(err)
	}
	currentAttempt := &scenarioCampaignAttempt{cfg: fixture.cfg, stateDir: fixture.stateDir, payload: scenarioCampaignAttemptPayload{PlanHash: sibling.PlanHash}}
	priorAttempt := &scenarioCampaignAttempt{payload: scenarioCampaignAttemptPayload{PlanHash: current.PlanHash, ConfigHash: fixture.cfg.ConfigHash, PolicyHash: fixture.cfg.PolicyHash}}
	if err := validateScenarioCampaignLineageEdge(currentAttempt, priorAttempt); err == nil {
		t.Fatal("accepted sibling approvals as a forward recovery edge")
	}
}

// Completed preparation can survive a same-approval restart, but a new
// configuration must prepare a fresh interval and cannot sign a carry claim.
func TestScenarioCampaignHistoricalLineageDoesNotCarryPriorPreparation(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	prior, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := prior.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	bindPreAcceptanceFailedRecoveryGeneration(t, fixture, prior)
	priorRaw := readCampaignSuccessionFixtureBytes(t, prior.path())
	advanceCampaignLineageFixture(t, fixture)
	attempt, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(2*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.payload.PreparationComplete || attempt.payload.Recovery.InheritedPreparationSha256 != "" || attempt.payload.AcceptanceBoundary != nil {
		t.Fatalf("new approval inherited historical preparation: %+v", attempt.payload)
	}
	copyAttempt := *attempt
	copyRecovery := *attempt.payload.Recovery
	copyAttempt.payload.Recovery = &copyRecovery
	copyAttempt.payload.PreparationComplete = true
	copyRecovery.InheritedPreparationSha256 = bytesSHA256(priorRaw)
	if err := validateScenarioCampaignRecoveryFromPrior(&copyAttempt, prior, scenarioCampaignRecoveryRelativePath(1), priorRaw); err == nil || !strings.Contains(err.Error(), "cannot inherit preparation across an approval change") {
		t.Fatalf("signed cross-approval preparation carry admitted: %v", err)
	}
}

// An approved descendant does not authorize abandoning an active attempt or
// inventing its terminal result. Only an actual retained failure may advance.
func TestScenarioCampaignHistoricalLineageRejectsActivePredecessor(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	prior, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	before := readCampaignSuccessionFixtureBytes(t, prior.path())
	advanceCampaignLineageFixture(t, fixture)
	if _, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(2*time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "no terminal failed result") {
		t.Fatalf("active historical predecessor was replaced: %v", err)
	}
	files, err := scenarioCampaignRecoveryFiles(fixture.stateDir)
	if err != nil || len(files) != 1 || !bytes.Equal(before, readCampaignSuccessionFixtureBytes(t, prior.path())) {
		t.Fatalf("rejected active predecessor was changed: %v %v", files, err)
	}
}

// Historical hash selection never supplies current authority, and a production
// descendant prevents any new provisional recovery even with valid ancestry.
func TestScenarioCampaignHistoricalLineageRejectsUnapprovedAndProduction(t *testing.T) {
	t.Parallel()
	fixture := newCampaignSuccessionFixture(t)
	prior, _ := bindCampaignRecoveryFixture(t, fixture)
	advanceCampaignLineageFixture(t, fixture)
	reader, err := newScenarioCampaignLineageReader(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	original := readCampaignSuccessionFixtureBytes(t, prior.path())
	copyAttempt := *prior
	copyAttempt.payload.ConfigHash = fixture.cfg.ConfigHash
	if err := writeScenarioCampaignAttempt(&copyAttempt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reader.read(prior.path()); err == nil || !strings.Contains(err.Error(), "approved configuration") {
		t.Fatalf("current hash substituted for historical signed config: %v", err)
	}
	copyAttempt.payload = prior.payload
	copyAttempt.payload.PlanHash = "0x" + strings.Repeat("cd", 32)
	if err := writeScenarioCampaignAttempt(&copyAttempt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reader.read(prior.path()); err == nil || !strings.Contains(err.Error(), "not an approved ancestor") {
		t.Fatalf("unknown signed plan admitted: %v", err)
	}
	if err := atomicWrite(prior.path(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*scenarioCampaignAttemptPayload){
		func(payload *scenarioCampaignAttemptPayload) {
			payload.AcceptanceBoundary.AcceptanceWindow.TerminalBlock++
		},
		func(payload *scenarioCampaignAttemptPayload) { payload.AcceptanceBoundary.Faults[0].AppliedBlock = 1 },
		func(payload *scenarioCampaignAttemptPayload) {
			payload.AcceptanceInvalidation = ""
			payload.AcceptanceInvalidatedAt = ""
		},
	} {
		copyAttempt := *prior
		boundary := *prior.payload.AcceptanceBoundary
		boundary.Faults = cloneScenarioFaultRecords(boundary.Faults)
		copyAttempt.payload.AcceptanceBoundary = &boundary
		mutation(&copyAttempt.payload)
		if err := writeScenarioCampaignAttempt(&copyAttempt); err != nil {
			t.Fatal(err)
		}
		if _, _, err := reader.read(prior.path()); err == nil {
			t.Fatal("historical reader accepted malformed or active source")
		}
	}
	if err := atomicWrite(prior.path(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(scenarioCampaignAttemptPath(fixture.stateDir, "production-soak"), []byte("retained descendant\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal); err == nil || !strings.Contains(err.Error(), "production descendant") {
		t.Fatalf("production descendant admitted a new recovery: %v", err)
	}
	if files, err := scenarioCampaignRecoveryFiles(fixture.stateDir); err != nil || len(files) != 0 {
		t.Fatalf("rejected transition published a recovery: %v %v", files, err)
	}
}

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

type campaignSuccessionFixture struct {
	cfg            *ResolvedConfig
	stateDir       string
	roles          *RoleSecrets
	prior, current *SetupPlan
	attempt        *scenarioCampaignAttempt
	result         *ScenarioResult
	journal        *Journal
	now            time.Time
}

// Owns complete provider custody and independently persisted signed history;
// the plan retains only the dependency closure needed for succession admission.
func newCampaignSuccessionFixture(t *testing.T) *campaignSuccessionFixture {
	t.Helper()
	f := &campaignSuccessionFixture{cfg: testResolvedConfig(t), stateDir: filepath.Join(t.TempDir(), "state"), now: time.Date(2026, 9, 13, 6, 0, 0, 0, time.UTC)}
	if err := ensurePrivateDir(f.stateDir); err != nil {
		t.Fatal(err)
	}
	public, err := derivePublicRoles(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.prior, err = buildPlan(f.cfg, testSetupFacts(), public, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	// Succession authenticates retained plans, not the fleet setup scheduler.
	// Keep the policy, evidence and alpha barriers with their original dependency closure.
	renderDependencyKVs := map[string]bool{
		"policy.await-bootstrap":            true,
		validatorEvidenceAnchorActionID:     true,
		"alpha.transfer.operator-deposit.1": true,
		"alpha.transfer.operator-deposit.2": true,
		"validator.reserve-majority":        true,
	}
	requiredActionKVs := map[string]bool{"config.render": true}
	var retainedActions []Action
	for index := len(f.prior.Actions) - 1; index >= 0; index-- {
		action := f.prior.Actions[index]
		if !requiredActionKVs[action.ID] {
			continue
		}
		delete(requiredActionKVs, action.ID)
		if action.ID == "config.render" {
			var dependencies []string
			for _, dependency := range action.DependsOn {
				if renderDependencyKVs[dependency] {
					dependencies = append(dependencies, dependency)
				}
			}
			action.DependsOn = dependencies
			action.IntentHash, err = actionIntentHash(action)
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, dependency := range action.DependsOn {
			requiredActionKVs[dependency] = true
		}
		retainedActions = append(retainedActions, action)
	}
	if len(requiredActionKVs) != 0 {
		t.Fatalf("succession fixture lost action dependencies: %v", requiredActionKVs)
	}
	slices.Reverse(retainedActions)
	f.prior.Actions = retainedActions
	f.prior.MaximumSpend, err = maximumActionSpend(f.prior.Actions)
	if err != nil {
		t.Fatal(err)
	}
	f.prior.PlanHash, err = f.prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(f.prior); err != nil {
		t.Fatal(err)
	}
	f.roles, err = BuildRoleSecrets(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for label, role := range f.roles.Clients {
		id := sha256.Sum256([]byte(label))
		role.ClientIDHex = hex.EncodeToString(id[:16])
		f.roles.Clients[label] = role
	}
	if err := saveRoleSecrets(filepath.Join(f.stateDir, "secrets", "roles.json"), f.roles); err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(f.cfg, f.stateDir, f.prior, f.roles); err != nil {
		t.Fatal(err)
	}
	started := f.now.Add(-48 * time.Hour)
	f.attempt, err = loadOrCreateScenarioCampaignAttempt(f.cfg, f.stateDir, f.roles, f.prior.PlanHash, "release-1.0", nil, started)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.attempt.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	finalAcceptance := false
	f.result = &ScenarioResult{Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", DeploymentID: f.cfg.Config.Deployment.DeploymentID,
		RunID: f.attempt.payload.RunID, Name: "release-1.0", StartedAt: started.Format(time.RFC3339Nano), CompletedAt: started.Add(time.Hour).Format(time.RFC3339Nano),
		ConfigHash: f.cfg.ConfigHash, PolicyHash: f.cfg.PolicyHash, ChainID: f.cfg.ChainID, GenesisHash: f.cfg.Public.Chain.GenesisHash, Netuid: f.cfg.Netuid,
		Provisional: true, FinalAcceptance: &finalAcceptance, Result: "fail", AssertionCount: 1, FailedAssertionCount: 1,
		Assertions: []AssertionRecord{{ID: "native_application", Passed: false, Message: "retained pre-acceptance failure"}},
	}
	f.writeResult(t)
	copyPlan := *f.prior
	f.current = &copyPlan
	f.current.PriorPlanHashes = append([]string{f.prior.PlanHash}, f.prior.PriorPlanHashes...)
	f.writeCurrent(t)
	f.journal = openCampaignTestJournal(t, f.stateDir)
	action := actionByID(t, f.prior, "config.render")
	head := ChainHead{Number: 10, Hash: "0x" + strings.Repeat("ab", 32)}
	record := &ActionPostcondition{Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: f.prior.DeploymentID, PlanHash: f.prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		OperationalRPCMode: f.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(f.cfg), SubstrateFinalized: head, EVMFinalized: head, EVMHashDomain: "evm-rpc", Observed: map[string]any{"verified": true},
		IndependentSubstrateFinalized: head, IndependentEVMFinalized: head, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: map[string]any{"verified": true}}
	executor := &Executor{cfg: f.cfg, stateDir: f.stateDir, plan: f.prior, journal: f.journal}
	postconditionPath, postconditionHash, err := executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.journal.Append(JournalEntry{DeploymentID: f.prior.DeploymentID, PlanHash: f.prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: postconditionPath, PostconditionHash: postconditionHash}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *campaignSuccessionFixture) writeResult(t *testing.T) {
	t.Helper()
	var err error
	f.result.EvidenceHash, err = canonicalScenarioResultHash(f.result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(f.stateDir, "runs", f.attempt.payload.RunID, "result.json"), f.result); err != nil {
		t.Fatal(err)
	}
}

func (f *campaignSuccessionFixture) writeCurrent(t *testing.T) {
	t.Helper()
	var err error
	f.current.PlanHash, err = f.current.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(f.cfg, f.stateDir, f.current, f.roles); err != nil {
		t.Fatal(err)
	}
}

func (f *campaignSuccessionFixture) open() (*scenarioCampaignAttempt, error) {
	return loadOrCreateScenarioCampaignAttempt(f.cfg, f.stateDir, f.roles, f.current.PlanHash, "release-1.0", nil, f.now, f.journal)
}

func readCampaignSuccessionFixtureBytes(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Bounds repeated history decoding without reducing the real custody census or
// bypassing either active or historical plan authentication.
func TestScenarioCampaignAttemptSuccessionFixtureBoundsPlanDecoding(t *testing.T) {
	t.Parallel()
	f := newCampaignSuccessionFixture(t)
	for _, plan := range []*SetupPlan{f.prior, f.current} {
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > 256*1024 || len(plan.Actions) > 192 {
			t.Fatalf("succession fixture exceeds its plan decoding work bound: bytes=%d actions=%d", len(raw), len(plan.Actions))
		}
		if _, err := decodePersistedPlanBytes(raw); err != nil {
			t.Fatal(err)
		}
		if _, err := readValidatorEvidenceHistoricalPlan(f.stateDir, plan.PlanHash); err != nil {
			t.Fatal(err)
		}
		t.Logf("authenticated succession fixture: bytes=%d actions=%d", len(raw), len(plan.Actions))
	}
	if f.cfg.Config.Topology.Miners != 1000 {
		t.Fatalf("succession fixture reduced the provider census to %d", f.cfg.Config.Topology.Miners)
	}
	providerIdKVs := map[string]bool{}
	for index := 1; index <= f.cfg.Config.Topology.Miners; index++ {
		role, exists := f.roles.Clients["miner-"+strconv.Itoa(index)]
		id, err := hex.DecodeString(role.ClientIDHex)
		if !exists || err != nil || len(id) != 16 || providerIdKVs[role.ClientIDHex] {
			t.Fatalf("succession fixture lost unique assigned provider %d", index)
		}
		providerIdKVs[role.ClientIDHex] = true
	}
	if _, err := loadPersistedPlan(f.cfg, f.stateDir); err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(); err != nil {
		t.Fatal(err)
	}
}

// Rehashed fixture mutations must still reach the production budget and intent
// checks; the smaller history is never a relaxed plan decoder.
func TestScenarioCampaignAttemptSuccessionFixtureRejectsRehashedBudgetAndIntent(t *testing.T) {
	t.Parallel()
	f := newCampaignSuccessionFixture(t)
	raw, err := json.Marshal(f.current)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"budget", "intent"} {
		var plan SetupPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			t.Fatal(err)
		}
		want := "action spend total"
		if mutation == "budget" {
			plan.MaximumSpend.TAORao++
		} else {
			plan.Actions[0].Spend.TAORao++
			want = "intent hash does not bind its executable fields"
		}
		plan.PlanHash, err = plan.hash()
		if err != nil {
			t.Fatal(err)
		}
		changed, err := json.Marshal(&plan)
		if err != nil {
			t.Fatal(err)
		}
		for _, historical := range []bool{false, true} {
			if _, err := decodePersistedPlanBytesForHistory(changed, historical); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s mutation bypassed plan authentication (historical=%v): %v", mutation, historical, err)
			}
		}
	}
}

func TestScenarioCampaignAttemptSuccessionPreservesFailureAndRestartsPreparation(t *testing.T) {
	t.Parallel()
	f := newCampaignSuccessionFixture(t)
	legacy := scenarioCampaignAttemptPath(f.stateDir, "release-1.0")
	resultPath := filepath.Join(f.stateDir, "runs", f.attempt.payload.RunID, "result.json")
	originalAttempt := readCampaignSuccessionFixtureBytes(t, legacy)
	originalResult := readCampaignSuccessionFixtureBytes(t, resultPath)
	originalJournal := readCampaignSuccessionFixtureBytes(t, filepath.Join(f.stateDir, "journal.jsonl"))
	// The exact old source mismatch remains a read refusal. Only an owned
	// approved-plan transition can create the separate full successor.
	if _, err := readScenarioCampaignAttempt(f.cfg, f.stateDir, f.roles, f.current.PlanHash, "release-1.0"); err == nil || !strings.Contains(err.Error(), "differs from the approved phase") {
		t.Fatalf("old-source causal control: %v", err)
	}
	if _, err := loadOrCreateScenarioCampaignAttempt(f.cfg, f.stateDir, f.roles, f.current.PlanHash, "release-1.0", nil, f.now); err == nil {
		t.Fatal("unowned old-source retry replaced the failure")
	}
	next, err := f.open()
	if err != nil {
		t.Fatal(err)
	}
	if next.payload.Succession == nil || next.payload.RunID == f.attempt.payload.RunID || next.payload.PreparationComplete || next.payload.HandoffAuthenticated || next.payload.AcceptanceBoundary != nil {
		t.Fatal("successor inherited predecessor progress")
	}
	if next.payload.Succession.PriorAttemptSHA256 != bytesSHA256(originalAttempt) || next.payload.Succession.PriorResultSHA256 != bytesSHA256(originalResult) {
		t.Fatal("successor did not pin exact original bytes")
	}
	prepared := 0
	if err := beginScenarioCampaignPreparation(t.Context(), "release-1.0", next.payload.RunID, scenarioRunOptions{Attempt: next, Prepare: func(context.Context) error { prepared++; return nil }}); err != nil {
		t.Fatal(err)
	}
	again, err := f.open()
	if err != nil || again.payload.RunID != next.payload.RunID || !again.payload.PreparationComplete || prepared != 1 {
		t.Fatalf("exact successor reopen: %v", err)
	}
	if !bytes.Equal(originalAttempt, readCampaignSuccessionFixtureBytes(t, legacy)) || !bytes.Equal(originalResult, readCampaignSuccessionFixtureBytes(t, resultPath)) || !bytes.Equal(originalJournal, readCampaignSuccessionFixtureBytes(t, filepath.Join(f.stateDir, "journal.jsonl"))) {
		t.Fatal("succession rewrote original failure or journal")
	}
}

func TestScenarioCampaignAttemptSuccessionRejectsUnknownLineageAndCustody(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"unknown-plan", "same-plan", "missing-plan", "wrong-deployment", "wrong-owner", "forged-attempt", "missing-roles", "missing-provider", "unassigned-provider", "duplicate-provider"} {
		t.Run(name, func(t *testing.T) {
			f := newCampaignSuccessionFixture(t)
			switch name {
			case "unknown-plan":
				f.current.PriorPlanHashes = []string{"0x" + strings.Repeat("98", 32)}
				f.writeCurrent(t)
			case "same-plan":
				f.current = f.prior
				f.writeCurrent(t)
			case "missing-plan":
				if err := os.Remove(filepath.Join(f.stateDir, "plans", stringsTrim0x(f.prior.PlanHash)+".json")); err != nil {
					t.Fatal(err)
				}
			case "wrong-deployment":
				f.current.DeploymentID = "another-deployment"
				f.writeCurrent(t)
			case "wrong-owner":
				role := f.roles.EVM["keeper"]
				f.roles.EVM["testnet-owner"] = role
			case "forged-attempt":
				path := scenarioCampaignAttemptPath(f.stateDir, "release-1.0")
				var envelope ReleaseEvidenceEnvelope
				if err := json.Unmarshal(readCampaignSuccessionFixtureBytes(t, path), &envelope); err != nil {
					t.Fatal(err)
				}
				envelope.Signature = "0x" + strings.Repeat("00", 65)
				if err := writePublicJSON(path, envelope); err != nil {
					t.Fatal(err)
				}
			case "missing-roles":
				if err := os.Remove(filepath.Join(f.stateDir, "secrets", "roles.json")); err != nil {
					t.Fatal(err)
				}
			case "missing-provider":
				delete(f.roles.Clients, "miner-1000")
				if err := saveRoleSecrets(filepath.Join(f.stateDir, "secrets", "roles.json"), f.roles); err != nil {
					t.Fatal(err)
				}
			case "unassigned-provider":
				role := f.roles.Clients["miner-1000"]
				role.ClientIDHex = ""
				f.roles.Clients["miner-1000"] = role
				if err := saveRoleSecrets(filepath.Join(f.stateDir, "secrets", "roles.json"), f.roles); err != nil {
					t.Fatal(err)
				}
			case "duplicate-provider":
				role := f.roles.Clients["miner-1000"]
				role.ClientIDHex = f.roles.Clients["miner-1"].ClientIDHex
				f.roles.Clients["miner-1000"] = role
				if err := saveRoleSecrets(filepath.Join(f.stateDir, "secrets", "roles.json"), f.roles); err != nil {
					t.Fatal(err)
				}
			}
			_, err := createScenarioCampaignSuccessor(f.cfg, f.stateDir, f.roles, f.current.PlanHash, f.now, f.journal)
			if err == nil {
				t.Fatal("invalid source admitted a successor")
			}
			if _, err := os.Lstat(scenarioCampaignSuccessorPath(f.stateDir)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refused succession published: %v", err)
			}
		})
	}
}

func TestScenarioCampaignAttemptSuccessionRejectsActiveAcceptedAndIncompletePredecessors(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"active", "accepted", "accepted-file", "complete-file", "production-attempt", "started-lifecycle", "passing-result", "final-acceptance", "changed-result", "future-completion"} {
		t.Run(name, func(t *testing.T) {
			f := newCampaignSuccessionFixture(t)
			run := filepath.Join(f.stateDir, "runs", f.attempt.payload.RunID)
			switch name {
			case "active":
				if err := os.Remove(filepath.Join(run, "result.json")); err != nil {
					t.Fatal(err)
				}
			case "accepted":
				accepted, _, _, _ := bindCampaignAttemptBoundaryFixture(t, f.cfg, t.TempDir())
				f.attempt.payload.AcceptanceBoundary = accepted.payload.AcceptanceBoundary
				if err := validateScenarioCampaignAttemptPayload(f.cfg, f.prior.PlanHash, "release-1.0", &f.attempt.payload); err != nil {
					t.Fatal(err)
				}
				if err := writeScenarioCampaignAttempt(f.attempt); err != nil {
					t.Fatal(err)
				}
			case "accepted-file":
				if err := atomicWrite(filepath.Join(run, scenarioCampaignStartFilename), []byte("retained acceptance"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "complete-file":
				if err := atomicWrite(filepath.Join(run, "complete.json"), []byte("retained completion"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "production-attempt":
				if err := atomicWrite(scenarioCampaignAttemptPath(f.stateDir, "production-soak"), []byte("retained production"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "started-lifecycle":
				if err := atomicWrite(filepath.Join(f.stateDir, "public", "fleet-lifecycle.json"), []byte("retained lifecycle"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "passing-result":
				f.result.Result = "pass"
				f.writeResult(t)
			case "final-acceptance":
				accepted := true
				f.result.FinalAcceptance = &accepted
				f.writeResult(t)
			case "changed-result":
				f.result.Assertions[0].Message = "changed after hashing"
				if err := writePublicJSON(filepath.Join(run, "result.json"), f.result); err != nil {
					t.Fatal(err)
				}
			case "future-completion":
				f.result.CompletedAt = f.now.Add(time.Hour).Format(time.RFC3339Nano)
				f.writeResult(t)
			}
			if _, err := f.open(); err == nil {
				t.Fatal("active, accepted or incomplete predecessor admitted")
			}
			if _, err := os.Lstat(scenarioCampaignSuccessorPath(f.stateDir)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refused succession published: %v", err)
			}
		})
	}
}

func TestScenarioCampaignAttemptSuccessionReopenRejectsChangedAndMissingSources(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"attempt", "result", "prior-plan", "current-plan", "missing-prior-plan", "missing-result", "another-plan"} {
		t.Run(name, func(t *testing.T) {
			f := newCampaignSuccessionFixture(t)
			if _, err := f.open(); err != nil {
				t.Fatal(err)
			}
			before := readCampaignSuccessionFixtureBytes(t, scenarioCampaignSuccessorPath(f.stateDir))
			paths := map[string]string{"attempt": scenarioCampaignAttemptPath(f.stateDir, "release-1.0"), "result": filepath.Join(f.stateDir, "runs", f.attempt.payload.RunID, "result.json"), "prior-plan": filepath.Join(f.stateDir, "plans", stringsTrim0x(f.prior.PlanHash)+".json"), "current-plan": filepath.Join(f.stateDir, "plans", stringsTrim0x(f.current.PlanHash)+".json")}
			if strings.HasPrefix(name, "missing-") {
				if err := os.Remove(paths[strings.TrimPrefix(name, "missing-")]); err != nil {
					t.Fatal(err)
				}
			} else if name == "another-plan" {
				f.current.PriorPlanHashes = append(f.current.PriorPlanHashes, f.current.PlanHash)
				f.writeCurrent(t)
			} else {
				path := paths[name]
				if err := atomicWrite(path, append(readCampaignSuccessionFixtureBytes(t, path), ' '), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.open(); err == nil {
				t.Fatal("reopen silently replaced the approved successor or its source")
			}
			if !bytes.Equal(before, readCampaignSuccessionFixtureBytes(t, scenarioCampaignSuccessorPath(f.stateDir))) {
				t.Fatal("failed reopen rewrote its signed successor")
			}
		})
	}
}

func TestScenarioCampaignAttemptSuccessionRequiresExclusiveDeploymentOwner(t *testing.T) {
	t.Parallel()
	f := newCampaignSuccessionFixture(t)
	// This is the actual admission lock acquired by runMutation before both
	// CLI paths. An active scenario owner prevents another invocation entering.
	if other, err := OpenJournal(f.stateDir); err == nil {
		other.Close()
		t.Fatal("active deployment owner was replaced")
	}
	if err := f.journal.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(); err == nil {
		t.Fatal("closed deployment owner signed a succession")
	}
	if _, err := os.Lstat(scenarioCampaignSuccessorPath(f.stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unowned succession published")
	}
}

func installCampaignSuccessionLogFixture(t *testing.T, f *campaignSuccessionFixture) {
	t.Helper()
	stdout, stderr := filepath.Join(f.stateDir, "processes", "owner.stdout.log"), filepath.Join(f.stateDir, "processes", "owner.stderr.log")
	for _, path := range []string{stdout, stderr} {
		if err := atomicWrite(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: f.cfg.Config.Deployment.DeploymentID, BinaryHash: "binary-hash", Specs: []ProcessSpec{{ID: "miner-1", Role: "miner", Identity: "miner-1", StdoutPath: stdout, StderrPath: stderr}}}
	hash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: currentProcessStartTimeTicks(t), ManifestHash: hash, Processes: []ProcessState{{ID: "miner-1", Role: "miner", Identity: "miner-1", PID: os.Getpid(), Healthy: true}}}
	gate, err := initializeProcessLogGate(f.stateDir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Bind(state); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(f.stateDir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(f.stateDir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
}

func TestScenarioCampaignAttemptSuccessionBothRuntimeEntryPoints(t *testing.T) {
	t.Parallel()
	t.Run("release-candidate", func(t *testing.T) {
		f := newCampaignSuccessionFixture(t)
		stop := errors.New("full successor preparation reached")
		called := false
		err := runReleaseCandidateCampaignWithAnalyzer(t.Context(), f.cfg, f.stateDir, f.journal, &Executor{plan: f.current}, f.roles,
			func(ctx context.Context, _ *ResolvedConfig, _, phase string, _ *Journal, _ *Executor, attempt *scenarioCampaignAttempt) error {
				called = true
				if phase != "release-1.0" || attempt.payload.Succession == nil || attempt.payload.PreparationComplete {
					t.Fatal("composite did not begin a fresh full release")
				}
				return beginScenarioCampaignPreparation(ctx, phase, attempt.payload.RunID, scenarioRunOptions{Attempt: attempt, Prepare: func(context.Context) error { return stop }})
			}, noOpCampaignPreflight, func(context.Context, *ResolvedConfig, string, string, *RoleSecrets, *ScenarioResult) error {
				return nil
			})
		if !called || !errors.Is(err, stop) {
			t.Fatalf("composite succession did not reach preparation: %v", err)
		}
	})
	t.Run("release-1.0", func(t *testing.T) {
		f := newCampaignSuccessionFixture(t)
		installCampaignSuccessionLogFixture(t, f)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		// The real entry point creates its owned successor before opening the
		// shared execution transport. The fixture deliberately supplies no RPC
		// endpoint, so it must stop there without inheriting preparation.
		err := runScenarioCampaignAttemptWithTimeout(ctx, f.cfg, f.stateDir, "release-1.0", f.journal, &Executor{plan: f.current}, nil, 0)
		next, readErr := readScenarioCampaignAttempt(f.cfg, f.stateDir, f.roles, f.current.PlanHash, "release-1.0")
		if err == nil || !strings.Contains(err.Error(), "open campaign executor through shared EVM egress: execution RPC configuration: invalid RPC endpoint") || readErr != nil || next.payload.Succession == nil || next.payload.PreparationComplete {
			t.Fatalf("single-phase succession did not reach the real execution-owner boundary: error=%v read=%v", err, readErr)
		}
	})
}

func TestScenarioCampaignAttemptSuccessionAcceptanceUsesSuccessorBytes(t *testing.T) {
	t.Parallel()
	f := newCampaignSuccessionFixture(t)
	next, err := f.open()
	if err != nil {
		t.Fatal(err)
	}
	legacy := readCampaignSuccessionFixtureBytes(t, scenarioCampaignAttemptPath(f.stateDir, "release-1.0"))
	if err := next.updateProgress(false, true); err != nil {
		t.Fatal(err)
	}
	started, _ := time.Parse(time.RFC3339Nano, next.payload.StartedAt)
	campaignStart, baseline := testScenarioObservation(f.cfg, 6), testScenarioObservation(f.cfg, 7)
	campaignStart.ObservedAt, baseline.ObservedAt = started.Format(time.RFC3339Nano), started.Add(time.Minute).Format(time.RFC3339Nano)
	campaignStart.ObservationHash, _ = canonicalHashHex(campaignStart)
	baseline.ObservationHash, _ = canonicalHashHex(baseline)
	run := filepath.Join(f.stateDir, "runs", next.payload.RunID)
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	observationLogPrefix, err := captureScenarioObservationLogPrefix(filepath.Join(run, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, observation := range []*ScenarioObservation{campaignStart, baseline} {
		if err := appendObservation(filepath.Join(run, "observations.jsonl"), observation); err != nil {
			t.Fatal(err)
		}
	}
	definition, err := scenarioDefinitionFor(f.cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		t.Fatal(err)
	}
	window, err := buildScenarioAcceptanceWindow(f.cfg, definition, baseline)
	if err != nil {
		t.Fatal(err)
	}
	faults, err := initializeFaultRecords(window.StartBlock, definition.Faults)
	if err != nil {
		t.Fatal(err)
	}
	for i := range faults {
		if faults[i].PreAcceptance {
			faults[i].ArmedBlock = baseline.Status.Contracts.FinalizedHead.Number
			faults[i].ArmedBlockHash = baseline.Status.Contracts.FinalizedHead.Hash
		}
	}
	adversary := &AdversaryCampaignEvidence{Schema: "urnetwork-adversary-campaign-v1", Release: "1.0", MatrixHash: definition.AdversarialMatrixHash, StartedAt: started.Add(-time.Minute).Format(time.RFC3339Nano), HappyPathStartedAt: started.Format(time.RFC3339Nano), Status: "running"}
	if err := next.bindAcceptanceBoundary(run, "0x"+strings.Repeat("77", 32), definitionHash, adversary, started.Add(2*time.Minute), campaignStart, baseline, window, faults, observationLogPrefix, "0x"+strings.Repeat("88", 32)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readCampaignSuccessionFixtureBytes(t, next.path()), readCampaignSuccessionFixtureBytes(t, filepath.Join(run, scenarioCampaignStartFilename))) || !bytes.Equal(legacy, readCampaignSuccessionFixtureBytes(t, scenarioCampaignAttemptPath(f.stateDir, "release-1.0"))) {
		t.Fatal("acceptance copied legacy bytes or overwrote the failed attempt")
	}
	if window.EpochCount != 5 || window.EpochBlocks != 300 || window.FinalizeOffsetBlocks != 150 {
		t.Fatalf("succession reduced the full release window: %+v", window)
	}
}

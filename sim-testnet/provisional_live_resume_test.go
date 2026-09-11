package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProvisionalLiveAdoptionPreservesHistoryAndRejectsNewErrors(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	appendProcessLog(t, fixture.stderrPath, "panic: recovered before provisional adoption\n")
	if err := fixture.gate.RequireClean(false); err == nil {
		t.Fatal("strict historical failure absent")
	}
	originalGate, err := os.ReadFile(fixture.gate.path)
	if err != nil {
		t.Fatal(err)
	}
	originalLog, err := os.ReadFile(fixture.stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(fixture.dir, "provisional-resumes", "invocation-test")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	adoption := &provisionalLiveTopology{
		Schema: "urnetwork-sim-provisional-live-topology-v1", Provisional: true, PlanHash: "approved-plan",
		CompletedAt: time.Now().UTC().Format(time.RFC3339Nano), ManifestHash: fixture.supervisor.ManifestHash,
		SupervisorPID: fixture.supervisor.SupervisorPID, SupervisorStartTimeTicks: fixture.supervisor.SupervisorStartTimeTicks,
		ProcessLogGatePath: filepath.Join(dir, "process-log-gate.json"), manifest: fixture.manifest,
	}
	gate, err := newProvisionalProcessLogGate(fixture.dir, adoption)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.RequireClean(false); err != nil {
		t.Fatalf("recovered historical error blocked provisional gate: %v", err)
	}
	afterGate, _ := os.ReadFile(fixture.gate.path)
	afterLog, _ := os.ReadFile(fixture.stderrPath)
	if !bytes.Equal(originalGate, afterGate) || !bytes.Equal(originalLog, afterLog) {
		t.Fatal("adoption changed prior gate or raw log")
	}
	for path, value := range map[string]any{
		"supervisor.json":                        fixture.manifest,
		"supervisor.state.json":                  fixture.supervisor,
		"provisional-resumes/live-topology.json": adoption,
	} {
		if err := writePublicJSON(filepath.Join(fixture.dir, path), value); err != nil {
			t.Fatal(err)
		}
	}
	cfg := testResolvedConfig(t)
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: adoption.PlanHash}}
	reloaded, err := loadProvisionalOrStrictProcessLogGate(cfg, fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.path != gate.path {
		t.Fatal("successor campaign did not retain adoption boundary")
	}
	appendProcessLog(t, fixture.stderrPath, "panic: new failure after provisional adoption\n")
	if err := reloaded.RequireClean(false); err == nil {
		t.Fatal("new runtime failure was waived")
	}
	cfg.provisionalResume = nil
	strict, err := loadProvisionalOrStrictProcessLogGate(cfg, fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	if strict.path != fixture.gate.path || strict.RequireClean(false) == nil {
		t.Fatal("strict loader adopted provisional history waiver")
	}
}

func TestProvisionalLiveAdoptionKeepsGenerationAndEveryProofDomain(t *testing.T) {
	fixture := newProcessLogGateFixture(t, "", "")
	adoption := &provisionalLiveTopology{ManifestHash: fixture.supervisor.ManifestHash, SupervisorPID: fixture.supervisor.SupervisorPID, SupervisorStartTimeTicks: fixture.supervisor.SupervisorStartTimeTicks}
	fixture.supervisor.Processes[0].Restarts = 2
	if err := provisionalAdoptionGeneration(adoption, fixture.supervisor); err != nil {
		t.Fatal(err)
	}
	if !supervisorStateReady(fixture.supervisor, adoption.ManifestHash, fixture.manifest.Specs) {
		t.Fatal("recovered healthy process was rejected")
	}
	for _, mutate := range []func(*SupervisorState){
		func(state *SupervisorState) { state.SupervisorPID++ },
		func(state *SupervisorState) { state.SupervisorStartTimeTicks++ },
		func(state *SupervisorState) { state.ManifestHash = "different" },
	} {
		changed := fixture.supervisor
		mutate(&changed)
		if provisionalAdoptionGeneration(adoption, changed) == nil {
			t.Fatal("different supervisor generation accepted")
		}
	}
	baseline := map[string]int{"validator-1/no-1": 1, "validator-1/no-2": 2, "validator-2/no-1": 3, "validator-2/no-2": 4}
	current := map[string]int{"validator-1/no-1": 2, "validator-1/no-2": 3, "validator-2/no-1": 4, "validator-2/no-2": 5}
	if !provisionalProofsAdvanced(baseline, current) {
		t.Fatal("complete fresh proof coverage rejected")
	}
	current["validator-2/no-2"] = 4
	if provisionalProofsAdvanced(baseline, current) {
		t.Fatal("stale proof domain accepted")
	}
	delete(current, "validator-2/no-2")
	if provisionalProofsAdvanced(baseline, current) {
		t.Fatal("missing proof domain accepted")
	}
}

func TestProvisionalLiveAdoptionDoctorGuardPreservesFutureBudgetsAndPendingWrites(t *testing.T) {
	makeExecutor := func() *Executor {
		plan := &SetupPlan{PlanHash: "active", MaximumSpend: Spend{TAORao: 1000}, Actions: []Action{
			{ID: "wallet.native-fee-reserve", Kind: "budget-reserve", IntentHash: "reserve", Spend: Spend{TAORao: 100}},
			{ID: "topology.launch", Kind: "local", IntentHash: "topology"},
			{ID: "fleet.register.201", Kind: "substrate-extrinsic", IntentHash: "register", Spend: Spend{Registrations: 1}},
			{ID: "fleet.commitment.201", Kind: "substrate-extrinsic", IntentHash: "commitment"},
			{ID: "churn.tournament-complete", Kind: "local", IntentHash: "tournament"},
			{ID: "future-campaign-write", Kind: "evm-transaction", IntentHash: "future", Spend: Spend{TAORao: 10}},
		}}
		entries := []JournalEntry{}
		for _, index := range []int{0, 2, 3} {
			action := plan.Actions[index]
			entries = append(entries, JournalEntry{PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified})
		}
		cfg := &ResolvedConfig{provisionalResume: &provisionalResumeState{Record: &provisionalResumeRecord{PlanHash: plan.PlanHash}}}
		return &Executor{cfg: cfg, plan: plan, journal: &Journal{entries: entries}}
	}
	executor := makeExecutor()
	// The reserve and a future campaign write remain approved; neither is a
	// transaction performed by this adoption after all setup writes verify.
	before := executor.plan.MaximumSpend
	if need, err := provisionalLiveResumeNeedsDoctor(executor); err != nil || need {
		t.Fatalf("completed zero-spend adoption needs doctor: %t %v", need, err)
	}
	if executor.plan.MaximumSpend != before || !spendIsZero(executor.plan.Actions[3].Spend) {
		t.Fatal("doctor guard changed approved budget inputs")
	}
	for name, change := range map[string]func(*Executor){
		"strict":                        func(e *Executor) { e.cfg.provisionalResume = nil },
		"pending-registration":          func(e *Executor) { e.journal.entries[1].Stage = StageFinalized },
		"pending-zero-spend-commitment": func(e *Executor) { e.journal.entries[2].Stage = StageFinalized },
		"wrong-intent":                  func(e *Executor) { e.journal.entries[2].IntentHash = "different" },
		"unapproved-plan":               func(e *Executor) { e.journal.entries[2].PlanHash = "unapproved" },
		"missing-setup":                 func(e *Executor) { e.journal.entries[0].Stage = StageFailed },
		"local-nonzero-spend":           func(e *Executor) { e.plan.Actions[1].Spend.AlphaRao = 1 },
		"local-actually-transaction":    func(e *Executor) { e.plan.Actions[1].Kind = "evm-transaction" },
	} {
		t.Run(name, func(t *testing.T) {
			executor := makeExecutor()
			change(executor)
			if need, err := provisionalLiveResumeNeedsDoctor(executor); err == nil && !need {
				t.Fatal("pending work bypassed doctor")
			}
		})
	}
}

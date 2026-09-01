package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/protocol"
)

func previousAcceleratedPolicy(t *testing.T, cfg *ResolvedConfig) (*protocol.Policy, string) {
	t.Helper()
	previous := *cfg.Policy
	previous.Deposit = cfg.Policy.Deposit
	previous.ProductionCadence = protocol.CadenceSnapshot{
		AfterAcceleratedEpochs: 20,
		EpochBlocks:            2_400, RootCommitWindowBlocks: 200,
		FinalizeOffsetBlocks: 1_200, CloseGraceBlocks: 20,
	}
	previous.Deposit.TotalTestCampaignCapRao = 496_000_000_000
	hash, err := historicalPolicyHash(&previous)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "0x2cbb4cdd991d9463f321f0de7f7bd77d028da611c2b5eb656626eac31ab5a356" {
		t.Fatalf("historical policy hash=%s", hash)
	}
	return &previous, hash
}

func writeRenderedPolicy(t *testing.T, stateDir string, validator int, policy *protocol.Policy, hash string) {
	t.Helper()
	wire, err := yaml.Marshal(struct {
		Policy     *protocol.Policy `yaml:"policy"`
		PolicyHash string           `yaml:"policy_hash"`
	}{Policy: policy, PolicyHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "runtime", "validator-"+strconv.Itoa(validator), "validator.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
}

func testFuturePolicyRevision(t *testing.T) (*ResolvedConfig, string, *SetupPlan, []JournalEntry) {
	t.Helper()
	cfg := testResolvedConfig(t)
	previous, previousHash := previousAcceleratedPolicy(t, cfg)
	stateDir := t.TempDir()
	writeRenderedPolicy(t, stateDir, 1, previous, previousHash)
	writeRenderedPolicy(t, stateDir, 2, previous, previousHash)
	prior := &SetupPlan{PolicyHash: previousHash, PlanHash: "0x" + strings.Repeat("11", 32)}
	entries := []JournalEntry{{PlanHash: prior.PlanHash, ActionID: voluntaryConvictionActionID, Stage: StageVerified}}
	return cfg, stateDir, prior, entries
}

func TestPolicyRevisionAuthenticatesNarrowFutureAcceleration(t *testing.T) {
	cfg, stateDir, prior, entries := testFuturePolicyRevision(t)
	decision, err := classifyPolicyRevision(cfg, stateDir, prior, entries)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Class != policyRevisionFutureAcceleration || decision.PreviousPolicy == nil {
		t.Fatalf("revision decision=%+v", decision)
	}

	mutations := []func(*protocol.Policy){
		func(policy *protocol.Policy) { policy.Settlement.CloseGraceBlocks++ },
		func(policy *protocol.Policy) { policy.Steering.MaximumHeadFleets-- },
		func(policy *protocol.Policy) { policy.Deposit.EpochCapRaoPerOperator++ },
		func(policy *protocol.Policy) { policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB++ },
		func(policy *protocol.Policy) { policy.Verify.TrailDepth++ },
		func(policy *protocol.Policy) { policy.Binding.MaximumValidityEpochs++ },
		func(policy *protocol.Policy) { policy.Safety.MaximumFinalizedHeadLagBlocks++ },
	}
	for index, mutate := range mutations {
		candidate := *cfg.Policy
		candidate.Deposit = cfg.Policy.Deposit
		candidate.Deposit.Tiers = append([]protocol.DepositTier(nil), cfg.Policy.Deposit.Tiers...)
		mutate(&candidate)
		if err := validateFuturePolicyAcceleration(decision.PreviousPolicy, &candidate); err == nil {
			t.Errorf("active-policy mutation %d was accepted", index)
		}
	}

	longer := *cfg.Policy
	longer.Deposit = cfg.Policy.Deposit
	longer.ProductionCadence = decision.PreviousPolicy.ProductionCadence
	longer.ProductionCadence.AfterAcceleratedEpochs++
	if err := validateFuturePolicyAcceleration(decision.PreviousPolicy, &longer); err == nil {
		t.Fatal("a longer future campaign was accepted")
	}
	largerCap := *cfg.Policy
	largerCap.Deposit = cfg.Policy.Deposit
	largerCap.Deposit.TotalTestCampaignCapRao = decision.PreviousPolicy.Deposit.TotalTestCampaignCapRao + 1
	if err := validateFuturePolicyAcceleration(decision.PreviousPolicy, &largerCap); err == nil {
		t.Fatal("a larger future campaign cap was accepted")
	}
}

func TestPolicyRevisionFailsClosedOnHistoryOrLiveWorkloadMismatch(t *testing.T) {
	cfg, stateDir, prior, entries := testFuturePolicyRevision(t)
	previous, previousHash := previousAcceleratedPolicy(t, cfg)
	previous.Steering.MaximumHeadFleets--
	writeRenderedPolicy(t, stateDir, 2, previous, previousHash)
	if _, err := classifyPolicyRevision(cfg, stateDir, prior, entries); err == nil || !strings.Contains(err.Error(), "does not authenticate") {
		t.Fatalf("tampered rendered policy was accepted: %v", err)
	}

	_, stateDir, prior, entries = testFuturePolicyRevision(t)
	entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: "topology.launch", Stage: StageBroadcast})
	if _, err := classifyPolicyRevision(cfg, stateDir, prior, entries); err == nil || !strings.Contains(err.Error(), "policy revision is forbidden") {
		t.Fatalf("post-launch policy revision was accepted: %v", err)
	}
}

func TestPolicyRevisionRequiresExactStoppedTopologyBoundary(t *testing.T) {
	cfg, stateDir, prior, entries := testFuturePolicyRevision(t)
	expectedProcesses := 2 + 3*cfg.Config.Topology.Operators + cfg.Config.Topology.MinerSwarmProcesses + cfg.Config.Topology.Operators + cfg.Config.Topology.Validators
	manifest := SupervisorFile{
		Schema: "urnetwork-sim-supervisor-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		BinaryHash: "sha256:" + strings.Repeat("11", 32),
	}
	state := SupervisorState{
		Schema: "urnetwork-sim-supervisor-state-v1", UpdatedAt: time.Unix(10, 0).UTC().Format(time.RFC3339Nano),
		SupervisorPID: 99_999_999, SupervisorStartTimeTicks: 1,
	}
	for index := 0; index < expectedProcesses; index++ {
		spec := ProcessSpec{ID: "process-" + strconv.Itoa(index), Role: "test-role", Identity: "identity-" + strconv.Itoa(index)}
		manifest.Specs = append(manifest.Specs, spec)
		state.Processes = append(state.Processes, ProcessState{ID: spec.ID, Role: spec.Role, Identity: spec.Identity, ExitError: "supervisor stopped"})
	}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state.ManifestHash = manifestHash
	manifestWire, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "supervisor.json"), manifestWire, 0o600); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "supervisor.state.json"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	prior.Actions = []Action{
		{ID: "topology.launch", IntentHash: "topology-intent"},
		{ID: "churn.tournament-complete", IntentHash: "churn-intent"},
	}
	entries = append(entries,
		JournalEntry{Sequence: 1, PlanHash: prior.PlanHash, ActionID: "topology.launch", IntentHash: "topology-intent", Stage: StageVerified},
		JournalEntry{Sequence: 2, PlanHash: prior.PlanHash, ActionID: "churn.tournament-complete", IntentHash: "churn-intent", Stage: StageVerified},
	)
	decision, err := classifyPolicyRevision(cfg, stateDir, prior, entries)
	if err != nil || decision.Class != policyRevisionFutureAcceleration || !decision.RestartRequired {
		t.Fatalf("stopped topology decision=%+v error=%v", decision, err)
	}
	state.SupervisorStartTimeTicks = 0
	wire, _ = json.Marshal(state)
	if err := os.WriteFile(filepath.Join(stateDir, "supervisor.state.json"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := classifyPolicyRevision(cfg, stateDir, prior, entries); err != nil {
		t.Fatalf("stopped legacy supervisor without a start tick was rejected: %v", err)
	}

	state.Processes[0].PID = os.Getpid()
	wire, _ = json.Marshal(state)
	if err := os.WriteFile(filepath.Join(stateDir, "supervisor.state.json"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := classifyPolicyRevision(cfg, stateDir, prior, entries); err == nil || !strings.Contains(err.Error(), "exact stopped state") {
		t.Fatalf("live topology child was accepted: %v", err)
	}
	state.Processes[0].PID = 0
	wire, _ = json.Marshal(state)
	if err := os.WriteFile(filepath.Join(stateDir, "supervisor.state.json"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	advanced := append(append([]JournalEntry(nil), entries...), JournalEntry{Sequence: 3, PlanHash: prior.PlanHash, ActionID: "precompile.commitment-write", Stage: StageIntent})
	if _, err := classifyPolicyRevision(cfg, stateDir, prior, advanced); err == nil || !strings.Contains(err.Error(), "advanced after M0A") {
		t.Fatalf("post-M0A scenario journal was accepted: %v", err)
	}
}

func TestPolicyRevisionReserveAccountingIsBoundToConvictionEvidence(t *testing.T) {
	cfg, stateDir, prior, _, entries, recovery := testVoluntaryConvictionDuplicateRecovery(t)
	accounting, err := authenticatedPolicyRevisionReserveAccounting(cfg, stateDir, prior, entries, planRevisionRecoveries{VoluntaryConvictions: []voluntaryConvictionDuplicateRecovery{recovery}})
	if err != nil {
		t.Fatal(err)
	}
	if accounting.CampaignReservedRao != recovery.CumulativeAfterRao || accounting.NextDepositNonces[1] != 3 || accounting.NextDepositNonces[2] != 0 {
		t.Fatalf("duplicate conviction accounting=%+v", accounting)
	}

	if err := os.MkdirAll(filepath.Join(stateDir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	evidenceWire, err := json.Marshal(recovery.OriginalEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "public", "voluntary-conviction.json"), evidenceWire, 0o600); err != nil {
		t.Fatal(err)
	}
	accounting, err = authenticatedPolicyRevisionReserveAccounting(cfg, stateDir, prior, entries, planRevisionRecoveries{})
	if err != nil {
		t.Fatal(err)
	}
	if accounting.CampaignReservedRao != cfg.Config.Scenarios.VoluntaryConvictionRao || accounting.NextDepositNonces[1] != 2 {
		t.Fatalf("single conviction accounting=%+v", accounting)
	}

	tampered := recovery
	tampered.CumulativeAfterRao++
	if _, err := authenticatedPolicyRevisionReserveAccounting(cfg, stateDir, prior, entries, planRevisionRecoveries{VoluntaryConvictions: []voluntaryConvictionDuplicateRecovery{tampered}}); err == nil {
		t.Fatal("tampered conviction reserve accounting was accepted")
	}
}

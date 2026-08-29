package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type staticScenarioProbe struct {
	observations []*ScenarioObservation
	err          error
	calls        int
}

type transientErrorScenarioProbe struct {
	start, recovered *ScenarioObservation
	calls            int
}

func (p *transientErrorScenarioProbe) Snapshot(context.Context) (*ScenarioObservation, error) {
	p.calls++
	switch p.calls {
	case 1:
		copy := *p.start
		copy.ObservationHash, _ = canonicalHashHex(copy)
		return &copy, nil
	case 2:
		return nil, errors.New("temporary finalized-head RPC failure")
	default:
		copy := *p.recovered
		copy.ObservationHash, _ = canonicalHashHex(copy)
		return &copy, nil
	}
}

func (p *staticScenarioProbe) Snapshot(context.Context) (*ScenarioObservation, error) {
	if p.err != nil {
		return nil, p.err
	}
	if len(p.observations) == 0 {
		return nil, errors.New("no observation")
	}
	index := p.calls
	if index >= len(p.observations) {
		index = len(p.observations) - 1
	}
	p.calls++
	copy := *p.observations[index]
	copy.ObservationHash, _ = canonicalHashHex(copy)
	return &copy, nil
}

func testScenarioObservation(cfg *ResolvedConfig, epoch uint64) *ScenarioObservation {
	contracts := &ContractView{
		Deployment:    &ContractDeployment{Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, CoordinatorProxy: common.HexToAddress("0x0000000000000000000000000000000000000011"), SettlementVault: common.HexToAddress("0x0000000000000000000000000000000000000022")},
		FinalizedHead: ChainHead{Number: 100 + epoch, Hash: "0x" + strings.Repeat("ab", 32)}, CurrentEpoch: epoch,
		OperatorCount: 2, PolicyHash: cfg.PolicyHash, RuntimeCodeMatches: true, ConservationHolds: true,
		TotalCaptured: "10", TotalPaid: "3", Outstanding: "7", ReservePrincipal: "2", ReserveLiveStake: "3",
	}
	return &ScenarioObservation{Schema: "urnetwork-sim-scenario-observation-v1", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: &DeploymentStatus{Schema: "urnetwork-sim-status-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, Contracts: contracts, Healthy: true}}
}

func TestScenarioRunnerWritesCompleteEvidenceOnlyOnPass(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	observation := testScenarioObservation(cfg, 1)
	definition := scenarioDefinition{Name: "unit-pass", Checks: []scenarioCheck{{ID: "conservation", Check: func(e *scenarioEvaluation) (bool, string) {
		return e.Current.Status.Contracts.ConservationHolds, "exact"
	}}}}
	fixed := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, &staticScenarioProbe{observations: []*ScenarioObservation{observation}}, scenarioRunOptions{Now: func() time.Time { return fixed }, Publish: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "pass" || result.FailedAssertionCount != 0 || result.EvidenceHash == "" {
		t.Fatalf("result = %+v", result)
	}
	runDir := filepath.Join(dir, "runs", result.RunID)
	for _, name := range []string{"observations.jsonl", "assertions.json", "anomalies.json", "analysis.json", "analysis.html", "junit.xml", "result.json", "complete.json"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var complete map[string]any
	b, _ := os.ReadFile(filepath.Join(runDir, "complete.json"))
	if json.Unmarshal(b, &complete) != nil || complete["schema"] != "urnetwork-sim-complete-v1" {
		t.Fatalf("invalid complete evidence: %s", b)
	}
	var anomalies ScenarioAnomalyLedger
	b, _ = os.ReadFile(filepath.Join(runDir, "anomalies.json"))
	if json.Unmarshal(b, &anomalies) != nil || anomalies.Status != "clean" || len(anomalies.Entries) != 0 {
		t.Fatalf("passing run anomaly ledger = %+v (%s)", anomalies, b)
	}
}

func TestScenarioRunnerFailureHasNoCompleteMarker(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	definition := scenarioDefinition{Name: "unit-fail", Checks: []scenarioCheck{{ID: "never", Check: func(*scenarioEvaluation) (bool, string) { return false, "not yet" }}}}
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, &staticScenarioProbe{observations: []*ScenarioObservation{testScenarioObservation(cfg, 0)}}, scenarioRunOptions{PollInterval: time.Millisecond, Timeout: 3 * time.Millisecond})
	if err == nil || result == nil || result.Result != "fail" || result.FailedAssertionCount != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	runDir := filepath.Join(dir, "runs", result.RunID)
	if _, err := os.Stat(filepath.Join(runDir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed scenario wrote complete marker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "result.json")); err != nil {
		t.Fatal(err)
	}
	var anomalies ScenarioAnomalyLedger
	b, _ := os.ReadFile(filepath.Join(runDir, "anomalies.json"))
	if json.Unmarshal(b, &anomalies) != nil || anomalies.Status != "open" || len(anomalies.Entries) == 0 {
		t.Fatalf("failed run anomaly ledger = %+v (%s)", anomalies, b)
	}
}

func TestScenarioRunnerRetainsTransientSnapshotFailureAfterRecovery(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	start := testScenarioObservation(cfg, 0)
	recovered := testScenarioObservation(cfg, 1)
	definition := scenarioDefinition{Name: "unit-transient-rpc", Checks: []scenarioCheck{{ID: "epoch-advanced", Check: func(e *scenarioEvaluation) (bool, string) {
		return e.Current.Status.Contracts.CurrentEpoch >= 1, "wait for recovered epoch"
	}}}}
	probe := &transientErrorScenarioProbe{start: start, recovered: recovered}
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Second})
	if err == nil || result == nil || result.Result != "fail" || probe.calls < 3 {
		t.Fatalf("result=%+v error=%v calls=%d", result, err, probe.calls)
	}
	found := false
	for _, assertion := range result.Assertions {
		if strings.HasPrefix(assertion.ID, "scenario_snapshot_") && !assertion.Passed && strings.Contains(assertion.Message, "temporary finalized-head RPC failure") {
			found = true
		}
	}
	if !found || result.Anomalies == nil || result.Anomalies.Status != "open" {
		t.Fatalf("transient snapshot failure was lost: assertions=%+v anomalies=%+v", result.Assertions, result.Anomalies)
	}
}

func TestScenarioRunnerPersistsInitialObservationFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	definition := scenarioDefinition{Name: "unit-initial-failure", Checks: []scenarioCheck{{ID: "unused", Check: func(*scenarioEvaluation) (bool, string) { return true, "" }}}}
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, &staticScenarioProbe{err: errors.New("rpc unavailable")}, scenarioRunOptions{})
	if err == nil || result == nil || result.Result != "fail" || result.FailedAssertionCount != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	runDir := filepath.Join(dir, "runs", result.RunID)
	for _, name := range []string{"result.json", "assertions.json", "anomalies.json", "analysis.json", "junit.xml"} {
		if _, statErr := os.Stat(filepath.Join(runDir, name)); statErr != nil {
			t.Fatalf("missing %s: %v", name, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(runDir, "complete.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("initial failure wrote complete marker: %v", statErr)
	}
}

func TestScenarioPreparationRunsUnderAdversariesAndPersistsFailure(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	campaign := &scenarioAdversaryStub{evidence: healthyAdversaryEvidence()}
	definition := scenarioDefinition{
		Name: "unit-preparation-failure", AdversarialMatrixHash: campaign.evidence.MatrixHash,
		Checks: []scenarioCheck{{ID: "unused", Check: func(*scenarioEvaluation) (bool, string) { return true, "" }}},
	}
	probe := &staticScenarioProbe{err: errors.New("probe must not run after preparation failure")}
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, probe, scenarioRunOptions{
		Adversaries: campaign,
		Prepare: func(context.Context) error {
			if !campaign.started || campaign.happyStarted.IsZero() {
				t.Fatal("scenario preparation ran before the adversarial campaign")
			}
			return errors.New("governance preparation failed")
		},
	})
	if err == nil || result == nil || result.Result != "fail" || campaign.startCalls != 1 || campaign.stopCalls != 1 || probe.calls != 0 {
		t.Fatalf("result=%+v err=%v campaign=%+v probe_calls=%d", result, err, campaign, probe.calls)
	}
	runDir := filepath.Join(dir, "runs", result.RunID)
	for _, name := range []string{"result.json", "assertions.json", "anomalies.json", "adversaries.json"} {
		if _, statErr := os.Stat(filepath.Join(runDir, name)); statErr != nil {
			t.Fatalf("missing %s: %v", name, statErr)
		}
	}
	if result.Anomalies == nil || result.Anomalies.Status != "open" || len(result.Anomalies.Entries) == 0 {
		t.Fatalf("preparation failure anomaly ledger=%+v", result.Anomalies)
	}
}

func TestScenarioRunnerExecutesAndRecordsFinalizedBlockFault(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	observations := []*ScenarioObservation{testScenarioObservation(cfg, 1), testScenarioObservation(cfg, 1), testScenarioObservation(cfg, 1)}
	observations[0].Status.Contracts.FinalizedHead.Number = 100
	observations[1].Status.Contracts.FinalizedHead.Number = 101
	observations[2].Status.Contracts.FinalizedHead.Number = 102
	definition := scenarioDefinition{
		Name:   "unit-fault",
		Checks: []scenarioCheck{{ID: "always", Check: func(*scenarioEvaluation) (bool, string) { return true, "ready" }}},
		Faults: []scenarioFaultSpec{{ID: "pause", Kind: "process-pause", Targets: []string{"miner-8"}, TriggerOffsetBlocks: 1, DurationBlocks: 1}},
	}
	driver := &fakeFaultDriver{}
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, &staticScenarioProbe{observations: observations}, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Second, FaultDriver: driver})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "pass" || len(result.Faults) != 1 || result.Faults[0].Status != "restored" || len(driver.applied) != 1 || len(driver.restored) != 1 || driver.recovered != 1 {
		t.Fatalf("result=%+v driver=%+v", result, driver)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs", result.RunID, "faults.json")); err != nil {
		t.Fatal(err)
	}
}

func TestScenarioDefinitionsAreStrict(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Scenarios.ShortEpochs = 20
	for _, name := range []string{"precompile-conformance", "smoke", "epoch", "release-1.0", "production-soak", "fault-miner-offline", "fault-operator-offline", "fault-validator-offline", "fault-claim-relayer-offline", "fault-taskworker-offline"} {
		definition, err := scenarioDefinitionFor(cfg, name)
		if err != nil || definition.Name != name || len(definition.Checks) == 0 {
			t.Fatalf("definition %s = %+v, %v", name, definition, err)
		}
	}
	if _, err := scenarioDefinitionFor(cfg, "typo"); err == nil {
		t.Fatal("unknown scenario accepted")
	}
	release, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]bool{}
	for _, check := range release.Checks {
		checks[check.ID] = true
	}
	for _, required := range []string{"native_head_weight_observed", "validator_self_uids_masked", "reserve_yield_auto_compounds", "validator_pool_scores_are_non_global", "claims_finalized_per_no", "signed_weight_cap_enforced"} {
		if !checks[required] {
			t.Fatalf("release scenario is missing %s", required)
		}
	}
}

func TestReleaseChecksExerciseAffiliatedAndIndependentValidators(t *testing.T) {
	cfg := testResolvedConfig(t)
	observation := &ScenarioObservation{
		FleetBindingsValid: true,
		HeadFleetUIDs:      []uint16{20, 21},
		Status: &DeploymentStatus{Contracts: &ContractView{Operators: []OperatorView{
			{NoID: 1, PoolUID: 10, PoolLive: true},
			{NoID: 2, PoolUID: 11, PoolLive: true},
		}}},
		Validators: []ValidatorObservation{
			{ValidatorID: 1, SelfUID: 5, MaskedUIDs: []uint16{5, 10, 20}, AppliedWeights: []IntentWeightObservation{
				{UID: 11, Numerator: "1", Denominator: "1", Value: 32768},
				{UID: 21, Numerator: "1", Denominator: "2", Value: 32767},
			}},
			{ValidatorID: 2, SelfUID: 6, MaskedUIDs: []uint16{6}, AppliedWeights: []IntentWeightObservation{
				{UID: 10, Numerator: "2", Denominator: "1", Value: 32767},
				{UID: 11, Numerator: "1", Denominator: "1", Value: 16383},
				{UID: 20, Numerator: "1", Denominator: "2", Value: 8192},
				{UID: 21, Numerator: "1", Denominator: "2", Value: 8192},
			}},
		},
	}
	evaluation := &scenarioEvaluation{Cfg: cfg, Current: observation}
	checks := map[string]scenarioCheck{}
	for _, check := range releaseScenarioChecks() {
		checks[check.ID] = check
	}
	for _, id := range []string{"native_head_weight_observed", "two_fleet_shared_prefix_split", "validator_self_uids_masked", "validator_pool_scores_are_non_global", "signed_weight_cap_enforced"} {
		passed, message := checks[id].Check(evaluation)
		if !passed {
			t.Errorf("%s: %s", id, message)
		}
	}
	observation.Validators[0].MaskedUIDs = []uint16{5}
	if passed, _ := checks["validator_self_uids_masked"].Check(evaluation); passed {
		t.Fatal("affiliated validator passed without masking its controlled pool/fleets")
	}
}

func TestWeightValuesRespectSignedCapExactly(t *testing.T) {
	within := []IntentWeightObservation{{Value: 32768}, {Value: 32767}}
	if ok, maximum, sum := weightValuesRespectCap(within, 32768); !ok || maximum != 32768 || sum != 65535 {
		t.Fatalf("feasible exact cap result = %t max=%d sum=%d", ok, maximum, sum)
	}
	over := []IntentWeightObservation{{Value: 32769}, {Value: 32766}}
	if ok, _, _ := weightValuesRespectCap(over, 32768); ok {
		t.Fatal("over-cap vector was accepted")
	}
	if ok, _, _ := weightValuesRespectCap(nil, 32768); ok {
		t.Fatal("empty vector was accepted")
	}
}

func TestProductionSoakDefinitionAndChecks(t *testing.T) {
	cfg := testResolvedConfig(t)
	definition, err := scenarioDefinitionFor(cfg, "production-soak")
	if err != nil {
		t.Fatal(err)
	}
	wantFaults := (2*cfg.Config.Topology.Operators + 1) + (2 + 3*cfg.Config.Topology.Operators + 2*cfg.Config.Topology.Miners + cfg.Config.Topology.Validators)
	if definition.GoalEpochs != 3 || len(definition.Faults) != wantFaults {
		t.Fatalf("production definition = %+v", definition)
	}
	start := testScenarioObservation(cfg, 20)
	current := testScenarioObservation(cfg, 23)
	current.Status.Contracts.Policy = PolicyView{
		EffectiveEpoch: 21, EpochBlocks: 50_400, RootCommitWindowBlocks: 1_200,
		FinalizeOffsetBlocks: 14_400, CloseGraceBlocks: 120,
	}
	assertions := evaluateScenario(cfg, definition, start, current, time.Now())
	byID := map[string]bool{}
	for _, assertion := range assertions {
		byID[assertion.ID] = assertion.Passed
	}
	if !byID["production_cadence_active"] || !byID["complete_production_epochs"] || !byID["required_finalized_epochs"] {
		t.Fatalf("production assertions = %+v", assertions)
	}
	if _, ok := byID["verify_key_rotation_preserves_history"]; !ok {
		t.Fatal("production soak omitted verify-key rotation evidence")
	}
	minimum := time.Duration((300+2*50_400+14_400)*12+120) * time.Second
	if got := scenarioTimeout(cfg, definition); got < minimum {
		t.Fatalf("production timeout %s is below %s", got, minimum)
	}
}

func TestArtifactHistoryKeysAcceptsBlobObjectShape(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	b := []byte(`{"schema":"urnetwork-payout-artifact-history-v1","objects":[{"Key":"prefix/` + hash + `.json","Size":10}]}`)
	keys := artifactHistoryKeys(b)
	if len(keys) != 1 || !strings.Contains(keys[0], hash) {
		t.Fatalf("keys = %v", keys)
	}
}

func TestFetchVerifyPublicKeysRejectsDuplicates(t *testing.T) {
	keys := []map[string]any{{"server_key_id": 0, "public_key": []byte(strings.Repeat("a", 32))}, {"server_key_id": 1, "public_key": []byte(strings.Repeat("b", 32))}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	defer server.Close()
	got, err := fetchVerifyPublicKeys(context.Background(), server.URL)
	if err != nil || got[0] == "" || got[1] == "" {
		t.Fatalf("keys = %v, %v", got, err)
	}
	keys[1]["server_key_id"] = 0
	if _, err := fetchVerifyPublicKeys(context.Background(), server.URL); err == nil {
		t.Fatal("duplicate verify key id was accepted")
	}
}

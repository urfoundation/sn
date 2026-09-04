package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gsrpcTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/payoutartifact"
	validatorpkg "github.com/urfoundation/sn/validator"
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

// descendingHeadScores builds canonical positive score evidence in the exact
// deterministic ranking order expected by the independent scenario verifier.
func descendingHeadScores(count int, multiplier int64) []validatorpkg.RationalJSON {
	scores := make([]validatorpkg.RationalJSON, count)
	for index := range scores {
		scores[index] = validatorpkg.RationalJSON{Numerator: fmt.Sprintf("%d", int64(count-index)*multiplier), Denominator: "1"}
	}
	return scores
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
	epochStart := uint64(1_000) + epoch*cfg.Policy.Settlement.EpochBlocks
	contracts := &ContractView{
		Deployment:    &ContractDeployment{Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, CoordinatorProxy: common.HexToAddress("0x0000000000000000000000000000000000000011"), SettlementVault: common.HexToAddress("0x0000000000000000000000000000000000000022")},
		FinalizedHead: ChainHead{Number: epochStart + 100, Hash: "0x" + strings.Repeat("ab", 32)}, CurrentEpoch: epoch, CurrentEpochStart: epochStart, CurrentEpochEnd: epochStart + cfg.Policy.Settlement.EpochBlocks,
		OperatorCount: 2, PolicyHash: cfg.PolicyHash, RuntimeCodeMatches: true, ConservationHolds: true,
		Policy:             PolicyView{EffectiveEpoch: 1, EffectiveBlock: 1_000, EpochBlocks: cfg.Policy.Settlement.EpochBlocks, RootCommitWindowBlocks: cfg.Policy.Settlement.RootCommitWindowBlocks, FinalizeOffsetBlocks: cfg.Policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: cfg.Policy.Settlement.CloseGraceBlocks},
		MinimumTransferRao: 100_000, TotalCaptured: "10", TotalPaid: "3", EscrowAccounted: "7", PendingFunding: "2", Outstanding: "5", LiveEscrowStake: "7", ReservePrincipal: "2", ReserveLiveStake: "3",
	}
	return &ScenarioObservation{Schema: "urnetwork-sim-scenario-observation-v1", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: &DeploymentStatus{Schema: "urnetwork-sim-status-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, Contracts: contracts, Healthy: true}}
}

// setProductionObservationGeometry moves a unit observation onto the exact
// release-locked production policy without relying on accelerated coordinates.
func setProductionObservationGeometry(cfg *ResolvedConfig, observation *ScenarioObservation, epoch, offset uint64) {
	start := uint64(20_000) + epoch*cfg.Policy.ProductionCadence.EpochBlocks
	contracts := observation.Status.Contracts
	contracts.CurrentEpoch = epoch
	contracts.CurrentEpochStart = start
	contracts.CurrentEpochEnd = start + cfg.Policy.ProductionCadence.EpochBlocks
	contracts.FinalizedHead = ChainHead{Number: start + offset, Hash: "0x" + strings.Repeat("bc", 32)}
	contracts.Policy = PolicyView{
		EffectiveEpoch: epoch - 1, EffectiveBlock: start - cfg.Policy.ProductionCadence.EpochBlocks,
		EpochBlocks: cfg.Policy.ProductionCadence.EpochBlocks, RootCommitWindowBlocks: cfg.Policy.ProductionCadence.RootCommitWindowBlocks,
		FinalizeOffsetBlocks: cfg.Policy.ProductionCadence.FinalizeOffsetBlocks, CloseGraceBlocks: cfg.Policy.ProductionCadence.CloseGraceBlocks,
	}
	observation.ObservationHash, _ = canonicalHashHex(observation)
}

// appendTerminalEpochFixtures materializes every accepted operator position in
// terminal status for pure acceptance-window tests.
func appendTerminalEpochFixtures(contracts *ContractView, window *ScenarioAcceptanceWindow, operators int) {
	for offset := uint64(0); offset < window.EpochCount; offset++ {
		epoch := EpochView{Epoch: window.FirstEpoch + offset}
		for noID := 1; noID <= operators; noID++ {
			epoch.Operators = append(epoch.Operators, EpochOperatorView{NoID: uint64(noID), Status: 2})
		}
		contracts.Epochs = append(contracts.Epochs, epoch)
	}
}

// Release acceptance begins at the next boundary and includes all five full
// epochs plus the terminal finalization offset.
func TestReleaseAcceptanceWindowDiscardsPartialBaseline(t *testing.T) {
	cfg := testResolvedConfig(t)
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	baseline := testScenarioObservation(cfg, 7)
	baseline.ObservationHash, _ = canonicalHashHex(baseline)
	window, err := buildScenarioAcceptanceWindow(cfg, definition, baseline)
	if err != nil {
		t.Fatal(err)
	}
	wantStart := baseline.Status.Contracts.CurrentEpochEnd
	wantEnd := wantStart + 5*cfg.Policy.Settlement.EpochBlocks
	if window.BaselineEpoch != 7 || window.FirstEpoch != 8 || window.EpochCount != 5 || window.StartBlock != wantStart || window.EndBlock != wantEnd || window.TerminalBlock != wantEnd+cfg.Policy.Settlement.FinalizeOffsetBlocks {
		t.Fatalf("release acceptance window=%+v", window)
	}
}

// Production acceptance similarly discards the dishonest-deposit preparation
// epoch and binds exactly three subsequent complete 360-block epochs.
func TestProductionAcceptanceWindowUsesThreeCompleteEpochs(t *testing.T) {
	cfg := testResolvedConfig(t)
	definition, err := scenarioDefinitionFor(cfg, "production-soak")
	if err != nil {
		t.Fatal(err)
	}
	baseline := testScenarioObservation(cfg, 20)
	setProductionObservationGeometry(cfg, baseline, 20, 100)
	window, err := buildScenarioAcceptanceWindow(cfg, definition, baseline)
	if err != nil {
		t.Fatal(err)
	}
	wantEnd := baseline.Status.Contracts.CurrentEpochEnd + 3*cfg.Policy.ProductionCadence.EpochBlocks
	if window.FirstEpoch != 21 || window.EpochCount != 3 || window.EpochBlocks != 360 || window.EndBlock != wantEnd || window.TerminalBlock != wantEnd+180 {
		t.Fatalf("production acceptance window=%+v", window)
	}
}

// A boundary transition alone is insufficient: the final accepted epoch must
// reach its exact finalization offset and every operator must be terminal.
func TestAcceptanceChecksRejectPrematureAndNonterminalEpochs(t *testing.T) {
	cfg := testResolvedConfig(t)
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	baseline := testScenarioObservation(cfg, 7)
	baseline.ObservationHash, _ = canonicalHashHex(baseline)
	window, err := buildScenarioAcceptanceWindow(cfg, definition, baseline)
	if err != nil {
		t.Fatal(err)
	}
	current := testScenarioObservation(cfg, window.FirstEpoch+window.EpochCount)
	current.Status.Contracts.FinalizedHead.Number = window.TerminalBlock - 1
	appendTerminalEpochFixtures(current.Status.Contracts, window, cfg.Config.Topology.Operators)
	evaluation := &scenarioEvaluation{Cfg: cfg, Start: baseline, Current: current, GoalEpoch: window.FirstEpoch + window.EpochCount, Window: window, Definition: definition}
	checks := acceptanceScenarioChecks()
	if passed, _ := checks[0].Check(evaluation); passed {
		t.Fatal("accepted window passed one block before terminal finalization")
	}
	current.Status.Contracts.FinalizedHead.Number = window.TerminalBlock
	if passed, detail := checks[0].Check(evaluation); !passed {
		t.Fatalf("terminal window rejected: %s", detail)
	}
	current.Status.Contracts.Epochs[len(current.Status.Contracts.Epochs)-1].Operators[1].Status = 1
	if passed, _ := checks[1].Check(evaluation); passed {
		t.Fatal("nonterminal operator epoch was accepted")
	}
	current.Status.Contracts.Epochs[len(current.Status.Contracts.Epochs)-1].Operators[1].Status = 2
	current.Status.Contracts.Epochs = append(current.Status.Contracts.Epochs, current.Status.Contracts.Epochs[0])
	if passed, _ := checks[1].Check(evaluation); passed {
		t.Fatal("duplicated accepted epoch was accepted")
	}
}

// Fault evidence must remain fully inside accepted epochs, including actual
// application and restoration rather than only its planned offsets.
func TestAcceptanceFaultAssertionRejectsWindowSpill(t *testing.T) {
	window := &ScenarioAcceptanceWindow{StartBlock: 1_000, EndBlock: 2_000}
	record := ScenarioFaultRecord{ID: "restart", TriggerBlock: 1_100, RestoreBlock: 1_120, AppliedBlock: 1_100, RestoredBlock: 1_120, Status: "restored"}
	observation := &ScenarioObservation{ObservationHash: "0x" + strings.Repeat("ab", 32)}
	assertions := appendAcceptanceFaultAssertion(nil, []ScenarioFaultRecord{record}, window, time.Now(), observation)
	if len(assertions) != 1 || !assertions[0].Passed {
		t.Fatalf("contained fault rejected: %+v", assertions)
	}
	record.RestoredBlock = window.EndBlock + 1
	assertions = appendAcceptanceFaultAssertion(nil, []ScenarioFaultRecord{record}, window, time.Now(), observation)
	if len(assertions) != 1 || assertions[0].Passed {
		t.Fatalf("fault outside window accepted: %+v", assertions)
	}
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

func TestPublishedScenarioCandidateKeepsFrozenHashWhenClockAdvances(t *testing.T) {
	times := []time.Time{
		time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 2, 12, 0, 17, 0, time.UTC),
	}
	clockIndex := 0
	now := func() time.Time {
		value := times[clockIndex]
		if clockIndex < len(times)-1 {
			clockIndex++
		}
		return value
	}
	completed := now()
	result := &ScenarioResult{
		Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: "advancing-clock",
		StartedAt: completed.Add(-time.Minute).Format(time.RFC3339Nano), CompletedAt: completed.Format(time.RFC3339Nano), Result: "pass",
	}
	attachScenarioAnomalyGate(result, completed, nil, nil)
	expectedHash, err := canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	result.EvidenceHash = expectedHash
	if later := now(); !later.After(completed) {
		t.Fatal("test clock did not advance")
	}
	if err := refreshPublishedScenarioCandidate(result, completed, nil, nil, expectedHash); err != nil {
		t.Fatal(err)
	}
	if result.EvidenceHash != expectedHash || result.Anomalies.GeneratedAt != completed.Format(time.RFC3339Nano) {
		t.Fatalf("published candidate changed after clock advance: hash=%s anomalies_at=%s", result.EvidenceHash, result.Anomalies.GeneratedAt)
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
	probe := &staticScenarioProbe{observations: []*ScenarioObservation{testScenarioObservation(cfg, 7)}}
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, probe, scenarioRunOptions{
		Adversaries: campaign,
		Prepare: func(context.Context) error {
			if !campaign.started || campaign.happyStarted.IsZero() {
				t.Fatal("scenario preparation ran before the adversarial campaign")
			}
			if probe.calls != 1 {
				t.Fatalf("campaign boundary observations=%d, want one before preparation", probe.calls)
			}
			return errors.New("governance preparation failed")
		},
	})
	if err == nil || result == nil || result.Result != "fail" || campaign.startCalls != 1 || campaign.stopCalls != 1 || probe.calls != 1 {
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

func TestReleaseScenarioCountsFiveEpochsFromDelayedLiveTopology(t *testing.T) {
	cfg := testResolvedConfig(t)
	definition := scenarioDefinition{
		Name: "release-1.0", GoalEpochs: 5,
		Checks: []scenarioCheck{{ID: "live-campaign-boundary", Check: func(e *scenarioEvaluation) (bool, string) {
			return e.Current.Status.Contracts.CurrentEpoch >= e.GoalEpoch, "wait for five live epochs"
		}}},
	}
	start := testScenarioObservation(cfg, 26)
	before := testScenarioObservation(cfg, 30)
	atBoundary := testScenarioObservation(cfg, 31)
	started := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	assertions := evaluateScenario(cfg, definition, start, before, nil, started)
	if len(assertions) != 1 || assertions[0].Passed {
		t.Fatalf("epoch 30 passed delayed campaign boundary: %+v", assertions)
	}
	assertions = evaluateScenario(cfg, definition, start, atBoundary, nil, started)
	if len(assertions) != 1 || !assertions[0].Passed {
		t.Fatalf("epoch 31 did not complete delayed campaign boundary: %+v", assertions)
	}
}

func TestScenarioPreparationIsInsideMeasuredCampaignBoundary(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	start := testScenarioObservation(cfg, 26)
	prepared := testScenarioObservation(cfg, 27)
	complete := testScenarioObservation(cfg, 46)
	probe := &staticScenarioProbe{observations: []*ScenarioObservation{start, prepared, complete}}
	definition := scenarioDefinition{
		Name: "unit-preparation", GoalEpochs: 20,
		Checks: []scenarioCheck{{ID: "campaign-complete", Check: func(e *scenarioEvaluation) (bool, string) {
			return e.Current.Status.Contracts.CurrentEpoch >= e.GoalEpoch, "wait for campaign boundary"
		}}},
	}
	preparedCalls := 0
	result, err := runScenarioWithProbe(context.Background(), cfg, dir, definition, probe, scenarioRunOptions{
		PollInterval: time.Microsecond,
		Timeout:      time.Second,
		Prepare: func(context.Context) error {
			preparedCalls++
			if probe.calls != 1 {
				t.Fatalf("preparation began after %d observations, want exactly the live boundary", probe.calls)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if preparedCalls != 1 || result.StartEpoch != 26 || result.EndEpoch != 46 || probe.calls != 3 {
		t.Fatalf("result=%+v prepare_calls=%d probe_calls=%d", result, preparedCalls, probe.calls)
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
	cfg.Config.Scenarios.ShortEpochs = 5
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
	for _, required := range []string{"native_head_weight_observed", "head_slot_boundary_enforced", "head_fault_uid_tiebreak_safe", "head_selected_paid_rejected_zero_weight", "head_decision_history_valid", "validator_local_top200_disagreement", "head_promotion_demotion_transition", "native_head_pool_and_validator_rewards", "payout_artifacts_enforce_one_tier", "tier_exclusive_claim_outcomes", "validator_self_uids_masked", "reserve_yield_auto_compounds", "validator_pool_scores_are_non_global", "claims_finalized_per_no", "signed_weight_cap_enforced"} {
		if !checks[required] {
			t.Fatalf("release scenario is missing %s", required)
		}
	}
}

func TestProductionTransitionIncludesEveryHyperparameterAndNothingElse(t *testing.T) {
	tests := []struct {
		action Action
		want   bool
	}{
		{action: Action{ID: "production.schedule-policy"}, want: true},
		{action: Action{ID: "production.hyperparameter.immunity_period"}, want: true},
		{action: Action{ID: "production.hyperparameter.burn_half_life"}, want: true},
		{action: Action{ID: "production.hyperparameter."}, want: true},
		{action: Action{ID: "subnet.hyperparameter.burn_half_life"}, want: false},
		{action: Action{ID: "campaign.dishonest-deposit.2"}, want: false},
	}
	for _, test := range tests {
		if got := isProductionTransitionAction(test.action); got != test.want {
			t.Errorf("isProductionTransitionAction(%q)=%t, want %t", test.action.ID, got, test.want)
		}
	}
}

func TestReleaseChecksExerciseAffiliatedAndIndependentValidators(t *testing.T) {
	cfg := testResolvedConfig(t)
	headUIDs := make([]uint16, cfg.Config.Topology.fleetCandidates())
	eligible := make([]uint16, cfg.Config.Topology.fleetCandidates())
	for index := range eligible {
		eligible[index] = uint16(20 + index)
		if index < len(headUIDs) {
			headUIDs[index] = eligible[index]
		}
	}
	selected := append([]uint16(nil), eligible[:cfg.Config.Topology.HeadSlots]...)
	rejected := append([]uint16(nil), eligible[cfg.Config.Topology.HeadSlots:]...)
	affiliated := ValidatorObservation{
		ValidatorID: 1, SelfUID: 5, MaskedUIDs: []uint16{5, 10},
		EligibleHeadUIDs: eligible, EligibleHeadScores: descendingHeadScores(len(eligible), 1), SelectedHeadUIDs: selected, RejectedHeadUIDs: rejected,
		AppliedWeights: []IntentWeightObservation{{UID: 11, Numerator: "1", Denominator: "1", Value: 100}},
	}
	independent := ValidatorObservation{
		ValidatorID: 2, SelfUID: 6, MaskedUIDs: []uint16{6},
		EligibleHeadUIDs: eligible, EligibleHeadScores: descendingHeadScores(len(eligible), 2), SelectedHeadUIDs: selected, RejectedHeadUIDs: rejected,
		AppliedWeights: []IntentWeightObservation{
			{UID: 10, Numerator: "2", Denominator: "1", Value: 100},
			{UID: 11, Numerator: "1", Denominator: "1", Value: 50},
		},
	}
	for index, uid := range eligible {
		denominator := "1"
		if index < 2 {
			denominator = "2"
		}
		if index%cfg.Config.Topology.Operators == 0 {
			affiliated.MaskedUIDs = append(affiliated.MaskedUIDs, uid)
		} else if index < len(selected) {
			affiliated.AppliedWeights = append(affiliated.AppliedWeights, IntentWeightObservation{UID: uid, Numerator: "1", Denominator: denominator, Value: 300})
		}
		if index < len(selected) {
			independent.AppliedWeights = append(independent.AppliedWeights, IntentWeightObservation{UID: uid, Numerator: "1", Denominator: denominator, Value: 300})
		}
	}
	if !slices.Contains(affiliated.MaskedUIDs, rejected[0]) {
		t.Fatalf("affiliated rejected challenger UID %d was not masked", rejected[0])
	}
	observation := &ScenarioObservation{
		FleetBindingsValid: true,
		CandidateFleetUIDs: headUIDs,
		Status: &DeploymentStatus{Contracts: &ContractView{Operators: []OperatorView{
			{NoID: 1, PoolUID: 10, PoolLive: true},
			{NoID: 2, PoolUID: 11, PoolLive: true},
		}}},
		Validators: []ValidatorObservation{affiliated, independent},
	}
	evaluation := &scenarioEvaluation{Cfg: cfg, Current: observation}
	checks := map[string]scenarioCheck{}
	for _, check := range releaseScenarioChecks() {
		checks[check.ID] = check
	}
	for _, id := range []string{"native_head_weight_observed", "head_slot_boundary_enforced", "head_selected_paid_rejected_zero_weight", "two_fleet_shared_prefix_split", "validator_self_uids_masked", "validator_pool_scores_are_non_global", "signed_weight_cap_enforced"} {
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

func TestHeadSlotBoundaryFailsClosedOnMalformedClassification(t *testing.T) {
	base := ValidatorObservation{
		EligibleHeadUIDs:   []uint16{10, 11, 12},
		EligibleHeadScores: descendingHeadScores(3, 1),
		SelectedHeadUIDs:   []uint16{10, 11},
		RejectedHeadUIDs:   []uint16{12},
	}
	if ok, detail := validateHeadSlotBoundary(base, 2, 3); !ok {
		t.Fatalf("valid boundary rejected: %s", detail)
	}
	cases := []struct {
		name   string
		mutate func(*ValidatorObservation)
	}{
		{name: "duplicate eligible", mutate: func(value *ValidatorObservation) { value.EligibleHeadUIDs[2] = 11 }},
		{name: "selected rejected overlap", mutate: func(value *ValidatorObservation) { value.RejectedHeadUIDs[0] = 11 }},
		{name: "unclassified eligible", mutate: func(value *ValidatorObservation) { value.SelectedHeadUIDs = value.SelectedHeadUIDs[:1] }},
		{name: "stale binding", mutate: func(value *ValidatorObservation) { value.StaleHeadBindings = 1 }},
	}
	for _, testCase := range cases {
		value := base
		value.EligibleHeadUIDs = append([]uint16(nil), base.EligibleHeadUIDs...)
		value.EligibleHeadScores = append([]validatorpkg.RationalJSON(nil), base.EligibleHeadScores...)
		value.SelectedHeadUIDs = append([]uint16(nil), base.SelectedHeadUIDs...)
		value.RejectedHeadUIDs = append([]uint16(nil), base.RejectedHeadUIDs...)
		testCase.mutate(&value)
		if ok, _ := validateHeadSlotBoundary(value, 2, 3); ok {
			t.Fatalf("%s classification was accepted: %+v", testCase.name, value)
		}
	}
}

func TestHeadSelectionHistoryRecordsPromotionAndDemotion(t *testing.T) {
	intent := func(epoch uint64, selected, rejected []uint16) validatorpkg.SteeringIntent {
		return validatorpkg.SteeringIntent{Status: "applied", SubnetEpoch: epoch, EligibleHeadUIDs: []uint16{10, 11, 12}, SelectedHeadUIDs: selected, RejectedHeadUIDs: rejected}
	}
	history := summarizeHeadSelectionHistory([]validatorpkg.SteeringIntent{
		intent(1, []uint16{10, 11}, []uint16{12}),
		intent(2, []uint16{11, 12}, []uint16{10}),
		intent(3, []uint16{10, 11}, []uint16{12}),
		{Status: "pending", EligibleHeadUIDs: []uint16{10, 11, 12}, SelectedHeadUIDs: []uint16{10, 12}, RejectedHeadUIDs: []uint16{11}},
	}, 2, 3)
	if history.DecisionEpochs != 3 || history.Transitions != 2 || !slices.Equal(history.Promoted, []uint16{10, 12}) || !slices.Equal(history.Demoted, []uint16{10, 12}) {
		t.Fatalf("head selection history=%+v", history)
	}
	static := summarizeHeadSelectionHistory([]validatorpkg.SteeringIntent{
		intent(1, []uint16{10, 11}, []uint16{12}),
		intent(2, []uint16{11, 10}, []uint16{12}),
	}, 2, 3)
	if static.Transitions != 0 || len(static.Promoted) != 0 || len(static.Demoted) != 0 {
		t.Fatalf("ordering-only change counted as transition: %+v", static)
	}
}

// Historical transitions from a prior attempt cannot satisfy the current
// campaign: every validator must record the outage swap and restoration swap
// after the post-preparation baseline.
func TestHeadPromotionTransitionMustAdvanceInsideCurrentCampaign(t *testing.T) {
	cfg := testResolvedConfig(t)
	start := &ScenarioObservation{Validators: []ValidatorObservation{{ValidatorID: 1, HeadDecisionEpochs: 8, HeadTransitions: 3}, {ValidatorID: 2, HeadDecisionEpochs: 9, HeadTransitions: 4}}}
	current := &ScenarioObservation{Validators: []ValidatorObservation{
		{ValidatorID: 1, HeadDecisionEpochs: 10, HeadTransitions: 5, PromotedHeadUIDs: []uint16{220}, DemotedHeadUIDs: []uint16{22}},
		{ValidatorID: 2, HeadDecisionEpochs: 11, HeadTransitions: 6, PromotedHeadUIDs: []uint16{220}, DemotedHeadUIDs: []uint16{22}},
	}}
	var transition scenarioCheck
	for _, check := range releaseScenarioChecks() {
		if check.ID == "head_promotion_demotion_transition" {
			transition = check
		}
	}
	if transition.ID == "" {
		t.Fatal("head transition release check is absent")
	}
	evaluation := &scenarioEvaluation{Cfg: cfg, Start: start, Current: current}
	if passed, detail := transition.Check(evaluation); !passed {
		t.Fatalf("fresh two-transition evidence: %s", detail)
	}
	current.Validators[1].HeadTransitions = start.Validators[1].HeadTransitions
	if passed, _ := transition.Check(evaluation); passed {
		t.Fatal("stale pre-campaign transition evidence was accepted")
	}
}

func TestPayoutTierMembershipExcludesEveryLiveCandidate(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.Miners = 10
	cfg.Config.Topology.HeadFleets = 2
	cfg.Config.Topology.ChallengerFleets = 1
	cfg.Config.Topology.ClientsPerHeadFleet = 2
	clients := map[[16]byte]int{}
	artifact := &payoutArtifact{NoID: 1}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		var clientID [16]byte
		clientID[0], clientID[1] = byte(miner), byte(miner>>8)
		clients[clientID] = miner
		if operatorForMiner(cfg, miner) != 1 {
			continue
		}
		provider := payoutartifact.ProviderInput{ClientID: clientID}
		if miner <= cfg.Config.Topology.fleetCandidateMiners() {
			provider.HeadExcluded = true
			provider.ExclusionReason = "head_fleet_active"
		} else {
			provider.Eligible = true
			artifact.Leaves = append(artifact.Leaves, payoutartifact.Leaf{ClientID: clientID})
		}
		artifact.Providers = append(artifact.Providers, provider)
	}
	summary, err := summarizePayoutTierMembership(cfg, 1, artifact, clients)
	if err != nil || summary.CandidateProviders != 4 || summary.CandidateHeadExcluded != 4 || summary.CandidateLeaves != 0 || summary.PoolTailProviders != 2 || summary.PoolTailLeaves != 2 {
		t.Fatalf("canonical membership=%+v error=%v", summary, err)
	}
	mutated := *artifact
	mutated.Providers = append([]payoutartifact.ProviderInput(nil), artifact.Providers...)
	mutated.Providers[0].HeadExcluded = false
	if _, err := summarizePayoutTierMembership(cfg, 1, &mutated, clients); err == nil {
		t.Fatal("candidate returned to its pool while its fleet UID remained live")
	}
	mutated = *artifact
	mutated.Leaves = append(append([]payoutartifact.Leaf(nil), artifact.Leaves...), payoutartifact.Leaf{ClientID: artifact.Providers[0].ClientID})
	if _, err := summarizePayoutTierMembership(cfg, 1, &mutated, clients); err == nil {
		t.Fatal("candidate received a pool leaf while head-excluded")
	}
	mutated = *artifact
	mutated.Providers = append([]payoutartifact.ProviderInput(nil), artifact.Providers[:len(artifact.Providers)-1]...)
	if _, err := summarizePayoutTierMembership(cfg, 1, &mutated, clients); err == nil {
		t.Fatal("incomplete operator provider population was accepted")
	}
}

func TestNativeRewardChecksBindTopBoundaryPoolsAndValidatorDividends(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.Miners = 10
	cfg.Config.Topology.HeadSlots = 2
	cfg.Config.Topology.HeadFleets = 2
	cfg.Config.Topology.ChallengerFleets = 2
	cfg.Config.Topology.ClientsPerHeadFleet = 2
	rewards := &NativeRewardObservation{
		FinalizedHead: ChainHead{Number: 100, Hash: "0xhead"},
		EmissionRao:   make([]string, 9), Incentive: make([]uint16, 9), Dividends: make([]uint16, 9),
	}
	for index := range rewards.EmissionRao {
		rewards.EmissionRao[index] = "0"
	}
	for _, uid := range []uint16{1, 2, 5, 6, 7} {
		rewards.EmissionRao[uid], rewards.Incentive[uid] = "10", 1
	}
	for _, uid := range []uint16{3, 4} {
		rewards.EmissionRao[uid], rewards.Dividends[uid] = "5", 1
	}
	validators := []ValidatorObservation{
		{ValidatorID: 1, SelfUID: 3, EligibleHeadUIDs: []uint16{5, 6, 7, 8}, EligibleHeadScores: descendingHeadScores(4, 1), SelectedHeadUIDs: []uint16{5, 6}, RejectedHeadUIDs: []uint16{7, 8}, AppliedWeights: []IntentWeightObservation{{UID: 5, Value: 10}, {UID: 6, Value: 10}, {UID: 7}, {UID: 8}}},
		{ValidatorID: 2, SelfUID: 4, EligibleHeadUIDs: []uint16{5, 7, 6, 8}, EligibleHeadScores: descendingHeadScores(4, 2), SelectedHeadUIDs: []uint16{5, 7}, RejectedHeadUIDs: []uint16{6, 8}, AppliedWeights: []IntentWeightObservation{{UID: 5, Value: 10}, {UID: 6}, {UID: 7, Value: 10}, {UID: 8}}},
	}
	observation := &ScenarioObservation{
		CandidateFleetUIDs: []uint16{5, 6, 7, 8}, NativeRewards: rewards, Validators: validators,
		Status: &DeploymentStatus{Contracts: &ContractView{Operators: []OperatorView{{NoID: 1, PoolUID: 1, PoolLive: true}, {NoID: 2, PoolUID: 2, PoolLive: true}}}},
	}
	if ok, detail := validateHeadWeightDecision(cfg, observation); !ok {
		t.Fatalf("head decision: %s", detail)
	}
	if ok, detail := validateNativeRewardChannels(cfg, observation); !ok {
		t.Fatalf("native channels: %s", detail)
	}
	observation.Validators[1].RejectedHeadUIDs[0] = 7
	if ok, _ := validateHeadWeightDecision(cfg, observation); ok {
		t.Fatal("validator with an overlapping selected/rejected boundary was accepted")
	}
	observation.Validators[1].RejectedHeadUIDs[0] = 6
	rewards.EmissionRao[8], rewards.Incentive[8] = "1", 1
	if ok, _ := validateNativeRewardChannels(cfg, observation); ok {
		t.Fatal("head rejected by every validator received a native payout")
	}
}

func TestNativeRewardStakeRequiresExactNonnegativeUIDCensus(t *testing.T) {
	rewards := &NativeRewardObservation{
		EmissionRao:         []string{"0", "7"},
		Incentive:           []uint16{0, 1},
		Dividends:           []uint16{0, 0},
		TotalHotkeyAlphaRao: []string{"0", "123456789"},
	}
	stake, ok := nativeRewardStakeAt(rewards, 1)
	if !ok || stake.String() != "123456789" {
		t.Fatalf("exact native stake decoded as %v/%t", stake, ok)
	}
	for _, malformed := range []string{"", "-1", "1.5", "+1", " 1", "01"} {
		rewards.TotalHotkeyAlphaRao[1] = malformed
		if stake, ok := nativeRewardStakeAt(rewards, 1); ok || stake != nil {
			t.Errorf("malformed native stake %q decoded as %v/%t", malformed, stake, ok)
		}
	}
	rewards.TotalHotkeyAlphaRao = rewards.TotalHotkeyAlphaRao[:1]
	if stake, ok := nativeRewardStakeAt(rewards, 1); ok || stake != nil {
		t.Fatalf("missing UID stake decoded as %v/%t", stake, ok)
	}
}

func TestNativeRewardObservationBindsStakeToExactUIDOrder(t *testing.T) {
	head := ChainHead{Number: 77, Hash: "0xhead"}
	reward, err := nativeRewardObservationFromFinalizedState(
		head,
		[]gsrpcTypes.U64{3, 5},
		[]gsrpcTypes.U16{7, 11},
		[]gsrpcTypes.U16{13, 17},
		[]ExistingUIDFact{{UID: 0, TotalHotkeyAlphaRao: 19}, {UID: 1, TotalHotkeyAlphaRao: 23}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if reward.FinalizedHead != head || !slices.Equal(reward.EmissionRao, []string{"3", "5"}) || !slices.Equal(reward.Incentive, []uint16{7, 11}) || !slices.Equal(reward.Dividends, []uint16{13, 17}) || !slices.Equal(reward.TotalHotkeyAlphaRao, []string{"19", "23"}) {
		t.Fatalf("native reward snapshot is not exact: %+v", reward)
	}
}

func TestNativeRewardObservationRejectsEveryAdjacentShapeMismatch(t *testing.T) {
	head := ChainHead{Number: 77, Hash: "0xhead"}
	emission := []gsrpcTypes.U64{3, 5}
	incentive := []gsrpcTypes.U16{7, 11}
	dividends := []gsrpcTypes.U16{13, 17}
	facts := []ExistingUIDFact{{UID: 0, TotalHotkeyAlphaRao: 19}, {UID: 1, TotalHotkeyAlphaRao: 23}}
	tests := []struct {
		name      string
		head      ChainHead
		emission  []gsrpcTypes.U64
		incentive []gsrpcTypes.U16
		dividends []gsrpcTypes.U16
		facts     []ExistingUIDFact
	}{
		{name: "zero head", head: ChainHead{}, emission: emission, incentive: incentive, dividends: dividends, facts: facts},
		{name: "empty emission", head: head, emission: nil, incentive: nil, dividends: nil, facts: nil},
		{name: "short incentive", head: head, emission: emission, incentive: incentive[:1], dividends: dividends, facts: facts},
		{name: "short dividends", head: head, emission: emission, incentive: incentive, dividends: dividends[:1], facts: facts},
		{name: "short stake", head: head, emission: emission, incentive: incentive, dividends: dividends, facts: facts[:1]},
		{name: "reordered UID", head: head, emission: emission, incentive: incentive, dividends: dividends, facts: []ExistingUIDFact{{UID: 1}, {UID: 0}}},
	}
	for _, test := range tests {
		if value, err := nativeRewardObservationFromFinalizedState(test.head, test.emission, test.incentive, test.dividends, test.facts); err == nil || value != nil {
			t.Errorf("%s mismatch returned value=%+v error=%v", test.name, value, err)
		}
	}
}

// The release-sized fixture proves that each validator's own 202-score record
// reconstructs its own top 200 even when the boundary differs, that only its
// selected claimants receive positive submitted weights, and that an unrelated
// live UID cannot smuggle a positive head weight into either vector.
func TestReleaseHeadEvidenceReconstructsTwoIndependentTopTwoHundredDecisions(t *testing.T) {
	cfg := testResolvedConfig(t)
	eligible := make([]uint16, cfg.Config.Topology.fleetCandidates())
	for index := range eligible {
		eligible[index] = uint16(20 + index)
	}
	rankings := [][]uint16{append([]uint16(nil), eligible...), append([]uint16(nil), eligible...)}
	rankings[1][199], rankings[1][200] = rankings[1][200], rankings[1][199]
	validators := make([]ValidatorObservation, cfg.Config.Topology.Validators)
	for index, ranked := range rankings {
		selected := append([]uint16(nil), ranked[:cfg.Config.Topology.HeadSlots]...)
		rejected := append([]uint16(nil), ranked[cfg.Config.Topology.HeadSlots:]...)
		weights := []IntentWeightObservation{{UID: 1, Value: 1}, {UID: 2, Value: 1}}
		for _, uid := range selected {
			weights = append(weights, IntentWeightObservation{UID: uid, Value: 1})
		}
		for _, uid := range rejected {
			weights = append(weights, IntentWeightObservation{UID: uid})
		}
		validators[index] = ValidatorObservation{
			ValidatorID: index + 1, SelfUID: uint16(3 + index),
			EligibleHeadUIDs: append([]uint16(nil), ranked...), EligibleHeadScores: descendingHeadScores(len(ranked), int64(index+1)),
			SelectedHeadUIDs: append([]uint16(nil), selected...), RejectedHeadUIDs: append([]uint16(nil), rejected...), AppliedWeights: weights,
		}
	}
	if slices.Equal(sortedUIDs(validators[0].SelectedHeadUIDs), sortedUIDs(validators[1].SelectedHeadUIDs)) {
		t.Fatal("independent validator fixture did not exercise a divergent top-200 boundary")
	}
	rewards := &NativeRewardObservation{FinalizedHead: ChainHead{Number: 100, Hash: "0xhead"}, EmissionRao: make([]string, 256), Incentive: make([]uint16, 256), Dividends: make([]uint16, 256)}
	for index := range rewards.EmissionRao {
		rewards.EmissionRao[index] = "0"
	}
	selectedByEither := map[uint16]bool{}
	for _, validator := range validators {
		for _, uid := range validator.SelectedHeadUIDs {
			selectedByEither[uid] = true
		}
	}
	for _, uid := range []uint16{1, 2} {
		rewards.EmissionRao[uid], rewards.Incentive[uid] = "1", 1
	}
	for uid := range selectedByEither {
		rewards.EmissionRao[uid], rewards.Incentive[uid] = "1", 1
	}
	for _, uid := range []uint16{3, 4} {
		rewards.EmissionRao[uid], rewards.Dividends[uid] = "1", 1
	}
	observation := &ScenarioObservation{
		CandidateFleetUIDs: eligible, NativeRewards: rewards, Validators: validators,
		Status: &DeploymentStatus{Contracts: &ContractView{Operators: []OperatorView{{NoID: 1, PoolUID: 1, PoolLive: true}, {NoID: 2, PoolUID: 2, PoolLive: true}}}},
	}
	if ok, detail := validateHeadWeightDecision(cfg, observation); !ok {
		t.Fatalf("release-sized head decision: %s", detail)
	}
	if ok, detail := validateNativeRewardChannels(cfg, observation); !ok {
		t.Fatalf("release-sized reward channels: %s", detail)
	}
	duplicateValidator := *observation
	duplicateValidator.Validators = append([]ValidatorObservation(nil), observation.Validators...)
	duplicateValidator.Validators[1].ValidatorID = duplicateValidator.Validators[0].ValidatorID
	if ok, _ := validateHeadWeightDecision(cfg, &duplicateValidator); ok {
		t.Fatal("one validator's decision was counted twice as independent evidence")
	}
	observation.Validators[1].SelectedHeadUIDs[199], observation.Validators[1].RejectedHeadUIDs[0] = observation.Validators[1].RejectedHeadUIDs[0], observation.Validators[1].SelectedHeadUIDs[199]
	if ok, _ := validateHeadWeightDecision(cfg, observation); ok {
		t.Fatal("validator-selected claimant substitution passed unchanged score evidence")
	}
	observation.Validators[1].SelectedHeadUIDs[199], observation.Validators[1].RejectedHeadUIDs[0] = observation.Validators[1].RejectedHeadUIDs[0], observation.Validators[1].SelectedHeadUIDs[199]
	for index := range observation.Validators[1].AppliedWeights {
		if observation.Validators[1].AppliedWeights[index].UID == observation.Validators[1].SelectedHeadUIDs[0] {
			observation.Validators[1].AppliedWeights[index].Value = 0
		}
	}
	if ok, _ := validateHeadWeightDecision(cfg, observation); ok {
		t.Fatal("validator-selected claimant received zero submitted weight")
	}
	for index := range observation.Validators[1].AppliedWeights {
		if observation.Validators[1].AppliedWeights[index].UID == observation.Validators[1].SelectedHeadUIDs[0] {
			observation.Validators[1].AppliedWeights[index].Value = 1
		}
	}
	observation.Validators[0].AppliedWeights = append(observation.Validators[0].AppliedWeights, IntentWeightObservation{UID: 250, Value: 1})
	if ok, _ := validateHeadWeightDecision(cfg, observation); ok {
		t.Fatal("unapproved claimed UID received a positive validator weight")
	}
}

// Builds two independent 2/4 boundaries with one decision predating the
// acceptance baseline and one freshly applied decision per validator.
func headDecisionHistoryFixture(t *testing.T) (*ResolvedConfig, *ScenarioObservation, *ScenarioObservation) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.HeadSlots = 2
	cfg.Config.Topology.HeadFleets = 2
	cfg.Config.Topology.ChallengerFleets = 2
	decision := func(hash string, epoch, block uint64, eligible, selected, rejected []uint16) HeadDecisionObservation {
		weights := []IntentWeightObservation{{UID: 1, Value: 1}, {UID: 2, Value: 1}}
		for _, uid := range selected {
			weights = append(weights, IntentWeightObservation{UID: uid, Value: 1})
		}
		return HeadDecisionObservation{
			VectorHash: hash, SubnetEpoch: epoch, ApplicationBlock: block, ApplicationBlockHash: fmt.Sprintf("0xblock-%d", block),
			EligibleHeadUIDs: append([]uint16(nil), eligible...), EligibleHeadScores: descendingHeadScores(len(eligible), int64(epoch)),
			SelectedHeadUIDs: append([]uint16(nil), selected...), RejectedHeadUIDs: append([]uint16(nil), rejected...), AppliedWeights: weights,
		}
	}
	rankingOne := []uint16{5, 6, 7, 8}
	rankingTwo := []uint16{5, 7, 6, 8}
	baselineOne := decision("0xbaseline-1", 10, 100, rankingOne, rankingOne[:2], rankingOne[2:])
	baselineTwo := decision("0xbaseline-2", 10, 100, rankingTwo, rankingTwo[:2], rankingTwo[2:])
	freshOne := decision("0xfresh-1", 11, 110, rankingOne, rankingOne[:2], rankingOne[2:])
	freshTwo := decision("0xfresh-2", 11, 110, rankingTwo, rankingTwo[:2], rankingTwo[2:])
	start := &ScenarioObservation{Validators: []ValidatorObservation{
		{ValidatorID: 1, HeadDecisions: []HeadDecisionObservation{baselineOne}},
		{ValidatorID: 2, HeadDecisions: []HeadDecisionObservation{baselineTwo}},
	}}
	current := &ScenarioObservation{
		CandidateFleetUIDs: []uint16{5, 6, 7, 8},
		Status: &DeploymentStatus{Contracts: &ContractView{Operators: []OperatorView{
			{NoID: 1, PoolUID: 1, PoolLive: true},
			{NoID: 2, PoolUID: 2, PoolLive: true},
		}}},
		Validators: []ValidatorObservation{
			{ValidatorID: 1, HeadDecisions: []HeadDecisionObservation{baselineOne, freshOne}},
			{ValidatorID: 2, HeadDecisions: []HeadDecisionObservation{baselineTwo, freshTwo}},
		},
	}
	return cfg, start, current
}

func TestHeadDecisionHistoryValidatesEveryFreshIndependentDecision(t *testing.T) {
	cfg, start, current := headDecisionHistoryFixture(t)
	if ok, detail := validateHeadDecisionHistory(cfg, start, current); !ok {
		t.Fatalf("fresh independent decisions: %s", detail)
	}
}

// Reproduces the live evidence gap where a valid terminal decision used to
// hide an earlier applied vector which paid a rejected slot claimant.
func TestHeadDecisionHistoryRejectsSupersededInvalidDecision(t *testing.T) {
	cfg, start, current := headDecisionHistoryFixture(t)
	bad := current.Validators[0].HeadDecisions[1]
	bad.VectorHash = "0xbad-intermediate"
	bad.SubnetEpoch = 11
	bad.ApplicationBlock = 109
	bad.ApplicationBlockHash = "0xblock-109"
	bad.AppliedWeights = append(append([]IntentWeightObservation(nil), bad.AppliedWeights...), IntentWeightObservation{UID: bad.RejectedHeadUIDs[0], Value: 1})
	current.Validators[0].HeadDecisions = []HeadDecisionObservation{current.Validators[0].HeadDecisions[0], bad, current.Validators[0].HeadDecisions[1]}
	if ok, _ := validateHeadDecisionHistory(cfg, start, current); ok {
		t.Fatal("a superseded applied decision paid a rejected claimant without failing the acceptance history")
	}
}

func TestHeadDecisionHistoryRequiresFreshDecisionFromEveryValidator(t *testing.T) {
	cfg, start, current := headDecisionHistoryFixture(t)
	secondFresh := current.Validators[0].HeadDecisions[1]
	secondFresh.VectorHash = "0xfresh-1-again"
	secondFresh.SubnetEpoch++
	secondFresh.ApplicationBlock++
	secondFresh.ApplicationBlockHash = "0xblock-111"
	current.Validators[0].HeadDecisions = append(current.Validators[0].HeadDecisions, secondFresh)
	current.Validators[1].HeadDecisions = current.Validators[1].HeadDecisions[:1]
	if ok, _ := validateHeadDecisionHistory(cfg, start, current); ok {
		t.Fatal("two decisions from one validator substituted for a missing independent validator decision")
	}
}

func validatorLocalBoundaryFixture(t *testing.T) (*ResolvedConfig, *ScenarioObservation, *ScenarioObservation, uint16, uint16) {
	t.Helper()
	cfg := testResolvedConfig(t)
	eligible := make([]uint16, cfg.Config.Topology.fleetCandidates())
	for index := range eligible {
		eligible[index] = uint16(20 + index)
	}
	targetUID := eligible[validatorLocalHeadBoundaryFleet-1]
	filteredRanking := make([]uint16, 0, len(eligible))
	for _, uid := range eligible {
		if uid != targetUID {
			filteredRanking = append(filteredRanking, uid)
		}
	}
	filteredRanking = append(filteredRanking, targetUID)
	replacementUID := filteredRanking[cfg.Config.Topology.HeadSlots-1]
	decision := func(hash string, epoch uint64, ranking []uint16) HeadDecisionObservation {
		selected := append([]uint16(nil), ranking[:cfg.Config.Topology.HeadSlots]...)
		rejected := append([]uint16(nil), ranking[cfg.Config.Topology.HeadSlots:]...)
		weights := make([]IntentWeightObservation, 0, len(ranking))
		for _, uid := range selected {
			weights = append(weights, IntentWeightObservation{UID: uid, Numerator: "1", Denominator: "1", Value: 1})
		}
		for _, uid := range rejected {
			weights = append(weights, IntentWeightObservation{UID: uid, Numerator: "0", Denominator: "1"})
		}
		return HeadDecisionObservation{VectorHash: hash, SubnetEpoch: epoch, SelectedHeadUIDs: selected, RejectedHeadUIDs: rejected, AppliedWeights: weights}
	}
	start := &ScenarioObservation{Validators: []ValidatorObservation{{ValidatorID: 1}, {ValidatorID: 2}}}
	current := &ScenarioObservation{CandidateFleetUIDs: eligible, Validators: []ValidatorObservation{
		{ValidatorID: 1, HeadDecisions: []HeadDecisionObservation{decision("0xfiltered-divergence", 10, filteredRanking), decision("0xfiltered-restored", 11, eligible)}},
		{ValidatorID: 2, HeadDecisions: []HeadDecisionObservation{decision("0xindependent-divergence", 10, eligible), decision("0xindependent-restored", 11, eligible)}},
	}}
	return cfg, start, current, targetUID, replacementUID
}

func TestValidatorLocalTopTwoHundredBoundaryProvesOpposingWeightsAndRestoration(t *testing.T) {
	cfg, start, current, targetUID, replacementUID := validatorLocalBoundaryFixture(t)
	if ok, detail := validateValidatorLocalHeadBoundary(cfg, start, current); !ok {
		t.Fatalf("validator-local boundary: %s", detail)
	}
	if targetUID == replacementUID {
		t.Fatal("fixture did not cross the top-200 boundary")
	}
}

func TestHeadBoundaryFaultPreflightRequiresLowerUIDChallengers(t *testing.T) {
	cfg := testResolvedConfig(t)
	candidates := make([]uint16, 0, cfg.Config.Topology.fleetCandidates())
	for uid := uint16(54); len(candidates) < cfg.Config.Topology.HeadSlots; uid++ {
		candidates = append(candidates, uid)
	}
	candidates = append(candidates, 5, 6)
	observation := &ScenarioObservation{CandidateFleetUIDs: candidates}
	if ok, detail := headBoundaryUIDTieGeometry(cfg, observation); !ok {
		t.Fatalf("lower-UID challenger geometry: %s", detail)
	}
	observation.CandidateFleetUIDs[len(observation.CandidateFleetUIDs)-1] = 254
	if ok, _ := headBoundaryUIDTieGeometry(cfg, observation); ok {
		t.Fatal("a challenger that wins neither fault-target tie passed preflight")
	}
}

func TestHeadBoundaryFaultRestoreConditionsUseAppliedOpposingVectors(t *testing.T) {
	cfg := testResolvedConfig(t)
	candidates := make([]uint16, 0, cfg.Config.Topology.fleetCandidates())
	for uid := uint16(54); len(candidates) < cfg.Config.Topology.HeadSlots; uid++ {
		candidates = append(candidates, uid)
	}
	candidates = append(candidates, 5, 6)
	targetGlobal, targetLocal := candidates[2], candidates[validatorLocalHeadBoundaryFleet-1]
	rankingWithout := func(excluded ...uint16) []uint16 {
		exclude := uint16Set(excluded)
		ranking := make([]uint16, 0, len(candidates))
		for _, uid := range candidates {
			if !exclude[uid] {
				ranking = append(ranking, uid)
			}
		}
		return append(ranking, excluded...)
	}
	decision := func(hash string, ranking []uint16) HeadDecisionObservation {
		selected := append([]uint16(nil), ranking[:cfg.Config.Topology.HeadSlots]...)
		rejected := append([]uint16(nil), ranking[cfg.Config.Topology.HeadSlots:]...)
		weights := make([]IntentWeightObservation, 0, len(ranking))
		for _, uid := range selected {
			weights = append(weights, IntentWeightObservation{UID: uid, Value: 1})
		}
		for _, uid := range rejected {
			weights = append(weights, IntentWeightObservation{UID: uid})
		}
		return HeadDecisionObservation{VectorHash: hash, SubnetEpoch: 10, SelectedHeadUIDs: selected, RejectedHeadUIDs: rejected, AppliedWeights: weights}
	}
	start := &ScenarioObservation{Validators: []ValidatorObservation{{ValidatorID: 1}, {ValidatorID: 2}}}
	current := &ScenarioObservation{CandidateFleetUIDs: candidates, Validators: []ValidatorObservation{
		{ValidatorID: 1, HeadDecisions: []HeadDecisionObservation{decision("0xvalidator-1", rankingWithout(targetGlobal, targetLocal))}},
		{ValidatorID: 2, HeadDecisions: []HeadDecisionObservation{decision("0xvalidator-2", rankingWithout(targetGlobal))}},
	}}
	for _, condition := range []string{"global-head-boundary-diverged", "validator-local-head-boundary-diverged"} {
		met, err := faultRestoreConditionMet(cfg, start, current, scenarioFaultSpec{RestoreCondition: condition})
		if err != nil || !met {
			t.Fatalf("condition %s=(%t,%v), want true", condition, met, err)
		}
	}
	for index := range current.Validators[0].HeadDecisions[0].AppliedWeights {
		if current.Validators[0].HeadDecisions[0].AppliedWeights[index].UID == targetLocal {
			current.Validators[0].HeadDecisions[0].AppliedWeights[index].Value = 1
		}
	}
	if met, err := faultRestoreConditionMet(cfg, start, current, scenarioFaultSpec{RestoreCondition: "validator-local-head-boundary-diverged"}); err != nil || met {
		t.Fatalf("invalid rejected weight condition=(%t,%v), want false", met, err)
	}
}

func TestValidatorLocalTopTwoHundredBoundaryRejectsMissingWeightAndRestorationEvidence(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*ScenarioObservation, uint16, uint16)
	}{
		{name: "rejected claimant weighted", mutate: func(current *ScenarioObservation, target, _ uint16) {
			for index := range current.Validators[0].HeadDecisions[0].AppliedWeights {
				if current.Validators[0].HeadDecisions[0].AppliedWeights[index].UID == target {
					current.Validators[0].HeadDecisions[0].AppliedWeights[index].Value = 1
				}
			}
		}},
		{name: "selected claimant zero", mutate: func(current *ScenarioObservation, target, _ uint16) {
			for index := range current.Validators[1].HeadDecisions[0].AppliedWeights {
				if current.Validators[1].HeadDecisions[0].AppliedWeights[index].UID == target {
					current.Validators[1].HeadDecisions[0].AppliedWeights[index].Value = 0
				}
			}
		}},
		{name: "no later restoration", mutate: func(current *ScenarioObservation, _, _ uint16) {
			current.Validators[0].HeadDecisions = current.Validators[0].HeadDecisions[:1]
			current.Validators[1].HeadDecisions = current.Validators[1].HeadDecisions[:1]
		}},
		{name: "no common divergence epoch", mutate: func(current *ScenarioObservation, _, _ uint16) {
			current.Validators[1].HeadDecisions[0].SubnetEpoch = 9
		}},
		{name: "replacement never zeroed", mutate: func(current *ScenarioObservation, _, replacement uint16) {
			for index := range current.Validators[0].HeadDecisions[1].AppliedWeights {
				if current.Validators[0].HeadDecisions[1].AppliedWeights[index].UID == replacement {
					current.Validators[0].HeadDecisions[1].AppliedWeights[index].Value = 1
				}
			}
		}},
	}
	for _, mutation := range mutations {
		cfg, start, current, targetUID, replacementUID := validatorLocalBoundaryFixture(t)
		mutation.mutate(current, targetUID, replacementUID)
		if ok, _ := validateValidatorLocalHeadBoundary(cfg, start, current); ok {
			t.Fatalf("%s validator-local evidence was accepted", mutation.name)
		}
	}
}

func TestInspectValidatorIntentRejectsUnauthenticatedCurrentHeadEvidence(t *testing.T) {
	stateDir := t.TempDir()
	intentDir := filepath.Join(stateDir, "runtime", "validator-1", "state")
	if err := os.MkdirAll(intentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	intent := validatorpkg.SteeringIntent{
		Schema: validatorpkg.SteeringIntentSchema, SubnetEpoch: 9, SelfUID: 4, MaskedUIDs: []uint16{4},
		EligibleHeadUIDs: []uint16{10, 11, 12}, EligibleHeadScores: descendingHeadScores(3, 1), SelectedHeadUIDs: []uint16{10, 11}, RejectedHeadUIDs: []uint16{12},
		StaleHeadBindings: []validatorpkg.StaleHeadBinding{}, Status: "applied",
		DepositAudits: []validatorpkg.DepositAudit{{NoID: 1, Epoch: 4, SourceEpoch: 3, Status: validatorpkg.DepositAuditCompliant, Compliant: true, Disposition: "pool_weight_eligible", RequiredDepositRao: "1", ObservedDepositRao: "1", ConvictionBeforeRao: "0", ArtifactHash: "sha256:test"}},
		UIDs:          []uint16{10, 11}, Scores: []validatorpkg.RationalJSON{{Numerator: "2", Denominator: "1"}, {Numerator: "1", Denominator: "1"}}, Values: []uint16{2, 1},
	}
	hash, err := intent.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	intent.VectorHash = hash
	store := map[string]any{"schema": validatorpkg.SteeringIntentSchema, "current": intent, "history": []any{}}
	if err := writePublicJSON(filepath.Join(intentDir, "steering-intents.json"), store); err != nil {
		t.Fatal(err)
	}
	observed := inspectValidatorIntent(stateDir, 1, 2, 3)
	if observed.Error == "" || observed.AppliedIntents != 0 || len(observed.HeadDecisions) != 0 {
		t.Fatalf("unauthenticated intent lifecycle reached scenario evidence: %+v", observed)
	}
}

func TestInspectValidatorPathProofsRequiresEveryOperatorDomain(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	serverSeed := []byte(strings.Repeat("s", ed25519.SeedSize))
	serverKey := ed25519.NewKeyFromSeed(serverSeed)
	serverPublic := serverKey.Public().(ed25519.PublicKey)
	operators := make([]OperatorObservation, 0, cfg.Config.Topology.Operators)
	for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
		root := filepath.Join(stateDir, "runtime", "validator-1", "state", "operators", fmt.Sprintf("no-%d", noID))
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		seed := []byte(strings.Repeat(string(rune('a'+noID)), ed25519.SeedSize))
		if err := os.WriteFile(filepath.Join(root, "client.key"), seed, 0o600); err != nil {
			t.Fatal(err)
		}
		validatorKey := ed25519.NewKeyFromSeed(seed)
		vpk := validatorKey.Public().(ed25519.PublicKey)
		trailID := connect.Id{byte(noID)}
		nonce := []byte(strings.Repeat(string(rune('n'+noID)), connect.VerifyNonceSize))
		hops := make([]connect.VerifyProofHop, cfg.Policy.Verify.TrailDepth)
		trail := make([]connect.Id, len(hops))
		for index := range hops {
			trail[index][0], hops[index].ClientId[0] = byte(index+1), byte(index+1)
			hops[index].TimeMs = uint64(100 + index)
		}
		finalMessage, err := connect.BuildVerifyFinalMessage(1, trailID, nonce, vpk, byte(len(hops)), hops)
		if err != nil {
			t.Fatal(err)
		}
		extendMessage, err := connect.BuildVerifyExtendMessage(trailID, nonce, vpk, byte(len(hops)), trail)
		if err != nil {
			t.Fatal(err)
		}
		digest := connect.VerifyFinalDigest(finalMessage)
		pathID := validatorpkg.TrailPathId(trailID, vpk, 1)
		record := validatorpkg.ProofRecord{
			Version: 1, Epoch: 4, TrailId: trailID, ServerNonce: nonce, Vpk: vpk, M: len(hops), Hops: hops,
			ServerKeyId: 1, FinalSig: ed25519.Sign(serverKey, finalMessage), VerifierSig: ed25519.Sign(validatorKey, extendMessage),
			FinalDigest: digest[:], VpkSig: ed25519.Sign(validatorKey, finalMessage), Coverage: uint64(len(hops) - 1), PathId: pathID[:], CompleteTimeMs: hops[len(hops)-1].TimeMs,
		}
		line, err := json.Marshal(&record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "proofs.jsonl"), append(line, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		operators = append(operators, OperatorObservation{NoID: noID, VerifyKeys: []VerifyKeyObservation{{ServerKeyID: 1, PublicKey: serverPublic}}})
	}
	counts, err := inspectValidatorPathProofs(cfg, stateDir, 1, operators)
	if err != nil || counts[1] != 1 || counts[2] != 1 || len(counts) != 2 {
		t.Fatalf("validator path counts=%v error=%v", counts, err)
	}
	path := filepath.Join(stateDir, "runtime", "validator-1", "state", "operators", "no-2", "proofs.jsonl")
	line, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tampered validatorpkg.ProofRecord
	if err := json.Unmarshal(line, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.FinalSig[0]++
	line, err = json.Marshal(&tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectValidatorPathProofs(cfg, stateDir, 1, operators); err == nil || !strings.Contains(err.Error(), "server FINAL signature") {
		t.Fatalf("tampered operator path signature error=%v", err)
	}
	if err := os.WriteFile(path, []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectValidatorPathProofs(cfg, stateDir, 1, operators); err == nil || !strings.Contains(err.Error(), "validator 1 operator 2 path proofs") {
		t.Fatalf("durable corrupt operator domain error=%v", err)
	}
}

func TestEpochScenarioRequiresFreshProofForEveryValidatorOperatorPair(t *testing.T) {
	cfg := testResolvedConfig(t)
	start := testScenarioObservation(cfg, 5)
	current := testScenarioObservation(cfg, 6)
	start.Validators = []ValidatorObservation{
		{ValidatorID: 1, PathProofCounts: map[int]int{1: 3, 2: 4}},
		{ValidatorID: 2, PathProofCounts: map[int]int{1: 5, 2: 6}},
	}
	current.Validators = []ValidatorObservation{
		{ValidatorID: 1, PathProofCounts: map[int]int{1: 8, 2: 9}},
		{ValidatorID: 2, PathProofCounts: map[int]int{1: 10, 2: 11}},
	}
	var pathCheck scenarioCheck
	for _, check := range epochScenarioChecks() {
		if check.ID == "validator_path_proofs_advance" {
			pathCheck = check
			break
		}
	}
	if pathCheck.Check == nil {
		t.Fatal("validator path-proof check is missing")
	}
	evaluation := &scenarioEvaluation{Cfg: cfg, Start: start, Current: current, Definition: scenarioDefinition{GoalEpochs: 5}}
	if ok, detail := pathCheck.Check(evaluation); !ok {
		t.Fatalf("fresh path proofs rejected: %s", detail)
	}
	current.Validators[1].PathProofCounts[2] = start.Validators[1].PathProofCounts[2] + 4
	if ok, detail := pathCheck.Check(evaluation); ok || !strings.Contains(detail, "validator 2 operator 2") {
		t.Fatalf("stale path proof accepted: ok=%t detail=%s", ok, detail)
	}
	delete(current.Validators[1].PathProofCounts, 2)
	if ok, detail := pathCheck.Check(evaluation); ok || !strings.Contains(detail, "path domains") {
		t.Fatalf("missing path domain accepted: ok=%t detail=%s", ok, detail)
	}
	current.Validators[1] = current.Validators[0]
	if ok, detail := pathCheck.Check(evaluation); ok || !strings.Contains(detail, "duplicated") {
		t.Fatalf("duplicate validator path evidence accepted: ok=%t detail=%s", ok, detail)
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
	wantFaults := (2*cfg.Config.Topology.Operators + 1) + (2 + 4*cfg.Config.Topology.Operators + cfg.Config.Topology.MinerSwarmProcesses + cfg.Config.Topology.Validators)
	if definition.GoalEpochs != 3 || len(definition.Faults) != wantFaults {
		t.Fatalf("production definition = %+v", definition)
	}
	start := testScenarioObservation(cfg, 5)
	current := testScenarioObservation(cfg, 9)
	productionPolicy := PolicyView{
		EffectiveEpoch: 6, EpochBlocks: 360, RootCommitWindowBlocks: 60,
		FinalizeOffsetBlocks: 180, CloseGraceBlocks: 6,
	}
	start.Status.Contracts.Policy = productionPolicy
	current.Status.Contracts.Policy = productionPolicy
	assertions := evaluateScenario(cfg, definition, start, current, nil, time.Now())
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
	minimum := time.Duration((4*360+180)*12+120) * time.Second
	if got := scenarioTimeout(cfg, definition); got < minimum {
		t.Fatalf("production timeout %s is below %s", got, minimum)
	}
}

// Both release campaigns discard the post-preparation partial epoch. Their
// watchdogs must therefore cover the longest possible wait to the next
// boundary, not merely the accepted epochs and terminal finalization.
func TestReleaseScenarioTimeoutCoversCompleteBoundaryWait(t *testing.T) {
	cfg := testResolvedConfig(t)
	tests := []struct {
		name           string
		epochBlocks    uint64
		acceptedEpochs uint64
		finalizeBlocks uint64
	}{
		{
			name: "release-1.0", epochBlocks: cfg.Policy.Settlement.EpochBlocks,
			acceptedEpochs: uint64(cfg.Config.Scenarios.ShortEpochs), finalizeBlocks: cfg.Policy.Settlement.FinalizeOffsetBlocks,
		},
		{
			name: "production-soak", epochBlocks: cfg.Policy.ProductionCadence.EpochBlocks,
			acceptedEpochs: uint64(cfg.Config.Scenarios.ProductionEpochs), finalizeBlocks: cfg.Policy.ProductionCadence.FinalizeOffsetBlocks,
		},
	}
	for _, testCase := range tests {
		definition, err := scenarioDefinitionFor(cfg, testCase.name)
		if err != nil {
			t.Fatal(err)
		}
		timeout := scenarioTimeout(cfg, definition)
		for _, baselineOffset := range []uint64{0, testCase.epochBlocks / 2, testCase.epochBlocks - 1} {
			boundaryWait := testCase.epochBlocks - baselineOffset
			requiredBlocks := boundaryWait + testCase.acceptedEpochs*testCase.epochBlocks + testCase.finalizeBlocks
			required := time.Duration(requiredBlocks*cfg.Public.Chain.ExpectedBlockSeconds) * time.Second
			if timeout < required {
				t.Fatalf("%s timeout %s does not cover offset %d requirement %s", testCase.name, timeout, baselineOffset, required)
			}
		}
	}
}

func TestFetchPayoutArtifactHistoryTraversesBoundedCanonicalPages(t *testing.T) {
	cfg := testResolvedConfig(t)
	objects := make([]payoutArtifactHistoryObject, payoutArtifactHistoryPageObjects+1)
	for index := range objects {
		hash := fmt.Sprintf("%064x", index+1)
		objects[index] = payoutArtifactHistoryObject{
			Key:  fmt.Sprintf("blob/operator-1/st/v1/history/%s/%d/10/1/%s.json", cfg.Config.Deployment.DeploymentID, cfg.Netuid, hash),
			Size: 10, ContentHash: "sha256:" + hash,
		}
	}
	requests := 0
	getter := func(_ context.Context, endpoint string, limit int64) ([]byte, int, error) {
		requests++
		parsed, err := url.Parse(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Query().Get("deployment_id") != cfg.Config.Deployment.DeploymentID || parsed.Query().Get("netuid") != fmt.Sprint(cfg.Netuid) || parsed.Query().Get("limit") != fmt.Sprint(payoutArtifactHistoryPageObjects) || limit != maximumPayoutArtifactHistoryPageBytes {
			t.Fatalf("history request %d is not exactly scoped: %s limit=%d", requests, endpoint, limit)
		}
		page := payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1"}
		if requests == 1 {
			if parsed.Query().Get("after") != "" {
				t.Fatalf("first history request has cursor %q", parsed.Query().Get("after"))
			}
			page.Objects = objects[:payoutArtifactHistoryPageObjects]
			page.More = true
			page.NextAfter = page.Objects[len(page.Objects)-1].Key
		} else {
			if parsed.Query().Get("after") != objects[payoutArtifactHistoryPageObjects-1].Key {
				t.Fatalf("second history cursor = %q", parsed.Query().Get("after"))
			}
			page.Objects = objects[payoutArtifactHistoryPageObjects:]
		}
		encoded, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		return encoded, http.StatusOK, nil
	}
	keys, gotRequests, err := fetchPayoutArtifactHistory(context.Background(), "https://operator.example", cfg.Config.Deployment.DeploymentID, cfg.Netuid, getter)
	if err != nil || gotRequests != 2 || requests != 2 || len(keys) != len(objects) || keys[0] != objects[0].Key || keys[len(keys)-1] != objects[len(objects)-1].Key {
		t.Fatalf("bounded artifact history keys=%d requests=%d/%d error=%v", len(keys), gotRequests, requests, err)
	}
}

func TestFetchPayoutArtifactHistoryRejectsTruncationCursorAndScope(t *testing.T) {
	cfg := testResolvedConfig(t)
	hash := strings.Repeat("ab", 32)
	valid := payoutArtifactHistoryObject{
		Key:  fmt.Sprintf("blob/operator-1/st/v1/history/%s/%d/10/1/%s.json", cfg.Config.Deployment.DeploymentID, cfg.Netuid, hash),
		Size: 10, ContentHash: "sha256:" + hash,
	}
	tests := []struct {
		name string
		page payoutArtifactHistoryPage
	}{
		{name: "truncated", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{valid}, More: true}},
		{name: "short-continuation", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{valid}, More: true, NextAfter: valid.Key}},
		{name: "terminal-cursor", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{valid}, NextAfter: valid.Key}},
		{name: "duplicate", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{valid, valid}}},
		{name: "unsafe-separator", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{{Key: strings.Replace(valid.Key, "/10/1/", "\\10/1/", 1), Size: valid.Size, ContentHash: valid.ContentHash}}}},
		{name: "foreign-deployment", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{{Key: strings.Replace(valid.Key, cfg.Config.Deployment.DeploymentID, "foreign", 1), Size: valid.Size, ContentHash: valid.ContentHash}}}},
		{name: "foreign-netuid", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{{Key: strings.Replace(valid.Key, fmt.Sprintf("/%d/10/1/", cfg.Netuid), fmt.Sprintf("/%d/10/1/", cfg.Netuid+1), 1), Size: valid.Size, ContentHash: valid.ContentHash}}}},
		{name: "extra-scope-segment", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{{Key: strings.Replace(valid.Key, "/10/1/", "/extra/10/1/", 1), Size: valid.Size, ContentHash: valid.ContentHash}}}},
		{name: "noncanonical-epoch", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{{Key: strings.Replace(valid.Key, "/10/1/", "/010/1/", 1), Size: valid.Size, ContentHash: valid.ContentHash}}}},
		{name: "zero-operator", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{{Key: strings.Replace(valid.Key, "/10/1/", "/10/0/", 1), Size: valid.Size, ContentHash: valid.ContentHash}}}},
		{name: "content-mismatch", page: payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", Objects: []payoutArtifactHistoryObject{{Key: valid.Key, Size: valid.Size, ContentHash: "sha256:" + strings.Repeat("cd", 32)}}}},
	}
	for _, testCase := range tests {
		getter := func(context.Context, string, int64) ([]byte, int, error) {
			encoded, err := json.Marshal(testCase.page)
			return encoded, http.StatusOK, err
		}
		if _, requests, err := fetchPayoutArtifactHistory(context.Background(), "https://operator.example", cfg.Config.Deployment.DeploymentID, cfg.Netuid, getter); err == nil || requests != 1 {
			t.Fatalf("%s invalid artifact history page accepted requests=%d error=%v", testCase.name, requests, err)
		}
	}
}

func TestFetchPayoutArtifactHistoryRejectsContinuationBeyondGlobalCap(t *testing.T) {
	cfg := testResolvedConfig(t)
	pageNo := 0
	getter := func(_ context.Context, endpoint string, _ int64) ([]byte, int, error) {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		page := payoutArtifactHistoryPage{Schema: "urnetwork-payout-artifact-history-v1", More: true}
		for index := 0; index < payoutArtifactHistoryPageObjects; index++ {
			ordinal := pageNo*payoutArtifactHistoryPageObjects + index + 1
			hash := fmt.Sprintf("%064x", ordinal)
			page.Objects = append(page.Objects, payoutArtifactHistoryObject{
				Key:  fmt.Sprintf("blob/operator-1/st/v1/history/%s/%d/10/1/%s.json", cfg.Config.Deployment.DeploymentID, cfg.Netuid, hash),
				Size: 10, ContentHash: "sha256:" + hash,
			})
		}
		if pageNo == 0 {
			if parsed.Query().Get("after") != "" {
				t.Fatalf("first page cursor = %q", parsed.Query().Get("after"))
			}
		} else if parsed.Query().Get("after") == "" {
			t.Fatal("continued page omitted its cursor")
		}
		page.NextAfter = page.Objects[len(page.Objects)-1].Key
		pageNo++
		encoded, err := json.Marshal(page)
		return encoded, http.StatusOK, err
	}
	if _, requests, err := fetchPayoutArtifactHistory(context.Background(), "https://operator.example", cfg.Config.Deployment.DeploymentID, cfg.Netuid, getter); err == nil || !strings.Contains(err.Error(), "over limit") || requests != uint64(maximumPayoutArtifactHistoryKeys/payoutArtifactHistoryPageObjects) {
		t.Fatalf("history beyond global cap requests=%d error=%v", requests, err)
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

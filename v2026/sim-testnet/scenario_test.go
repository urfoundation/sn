package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
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
		MinimumTransferRao: 100_000, TotalCaptured: "10", TotalPaid: "3", EscrowAccounted: "7", PendingFunding: "2", Outstanding: "5", LiveEscrowStake: "7", ReservePrincipal: "2", ReserveLiveStake: "3",
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

func TestReleaseScenarioCountsTwentyEpochsFromDelayedLiveTopology(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Config.Scenarios.ShortEpochs = 20
	definition := scenarioDefinition{
		Name: "release-1.0", GoalEpochs: 20,
		Checks: []scenarioCheck{{ID: "live-campaign-boundary", Check: func(e *scenarioEvaluation) (bool, string) {
			return e.Current.Status.Contracts.CurrentEpoch >= e.GoalEpoch, "wait for twenty live epochs"
		}}},
	}
	start := testScenarioObservation(cfg, 26)
	before := testScenarioObservation(cfg, 45)
	atBoundary := testScenarioObservation(cfg, 46)
	started := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	assertions := evaluateScenario(cfg, definition, start, before, started)
	if len(assertions) != 1 || assertions[0].Passed {
		t.Fatalf("epoch 45 passed delayed campaign boundary: %+v", assertions)
	}
	assertions = evaluateScenario(cfg, definition, start, atBoundary, started)
	if len(assertions) != 1 || !assertions[0].Passed {
		t.Fatalf("epoch 46 did not complete delayed campaign boundary: %+v", assertions)
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
		Name: "release-1.0", GoalEpochs: 20,
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
	for _, required := range []string{"native_head_weight_observed", "head_slot_boundary_enforced", "head_selected_paid_rejected_zero_weight", "head_promotion_demotion_transition", "native_head_pool_and_validator_rewards", "payout_artifacts_enforce_one_tier", "tier_exclusive_claim_outcomes", "validator_self_uids_masked", "reserve_yield_auto_compounds", "validator_pool_scores_are_non_global", "claims_finalized_per_no", "signed_weight_cap_enforced"} {
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
		EligibleHeadUIDs: eligible, SelectedHeadUIDs: selected, RejectedHeadUIDs: rejected,
		AppliedWeights: []IntentWeightObservation{{UID: 11, Numerator: "1", Denominator: "1", Value: 100}},
	}
	independent := ValidatorObservation{
		ValidatorID: 2, SelfUID: 6, MaskedUIDs: []uint16{6},
		EligibleHeadUIDs: eligible, SelectedHeadUIDs: selected, RejectedHeadUIDs: rejected,
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
	for _, id := range []string{"native_head_weight_observed", "head_slot_boundary_enforced", "two_fleet_shared_prefix_split", "validator_self_uids_masked", "validator_pool_scores_are_non_global", "signed_weight_cap_enforced"} {
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
		EligibleHeadUIDs: []uint16{10, 11, 12},
		SelectedHeadUIDs: []uint16{10, 11},
		RejectedHeadUIDs: []uint16{12},
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
	cfg.Config.Topology.ChallengerFleets = 1
	cfg.Config.Topology.ClientsPerHeadFleet = 2
	rewards := &NativeRewardObservation{
		FinalizedHead: ChainHead{Number: 100, Hash: "0xhead"},
		EmissionRao:   make([]string, 8), Incentive: make([]uint16, 8), Dividends: make([]uint16, 8),
	}
	for index := range rewards.EmissionRao {
		rewards.EmissionRao[index] = "0"
	}
	for _, uid := range []uint16{1, 2, 5, 6} {
		rewards.EmissionRao[uid], rewards.Incentive[uid] = "10", 1
	}
	for _, uid := range []uint16{3, 4} {
		rewards.EmissionRao[uid], rewards.Dividends[uid] = "5", 1
	}
	validators := []ValidatorObservation{
		{ValidatorID: 1, SelfUID: 3, EligibleHeadUIDs: []uint16{5, 6, 7}, SelectedHeadUIDs: []uint16{5, 6}, RejectedHeadUIDs: []uint16{7}, AppliedWeights: []IntentWeightObservation{{UID: 5, Value: 10}, {UID: 6, Value: 10}, {UID: 7}}},
		{ValidatorID: 2, SelfUID: 4, EligibleHeadUIDs: []uint16{5, 6, 7}, SelectedHeadUIDs: []uint16{6, 5}, RejectedHeadUIDs: []uint16{7}, AppliedWeights: []IntentWeightObservation{{UID: 5, Value: 10}, {UID: 6, Value: 10}, {UID: 7}}},
	}
	observation := &ScenarioObservation{
		CandidateFleetUIDs: []uint16{5, 6, 7}, NativeRewards: rewards, Validators: validators,
		Status: &DeploymentStatus{Contracts: &ContractView{Operators: []OperatorView{{NoID: 1, PoolUID: 1, PoolLive: true}, {NoID: 2, PoolUID: 2, PoolLive: true}}}},
	}
	if ok, detail := validateHeadWeightDecision(cfg, observation); !ok {
		t.Fatalf("head decision: %s", detail)
	}
	if ok, detail := validateNativeRewardChannels(cfg, observation); !ok {
		t.Fatalf("native channels: %s", detail)
	}
	observation.Validators[1].RejectedHeadUIDs[0] = 6
	if ok, _ := validateHeadWeightDecision(cfg, observation); ok {
		t.Fatal("validators with divergent/overlapping top boundaries were accepted")
	}
	observation.Validators[1].RejectedHeadUIDs[0] = 7
	rewards.EmissionRao[7], rewards.Incentive[7] = "1", 1
	if ok, _ := validateNativeRewardChannels(cfg, observation); ok {
		t.Fatal("rejected head with native payout was accepted")
	}
}

func TestInspectValidatorIntentReadsVersionFourHeadAndDepositEvidence(t *testing.T) {
	stateDir := t.TempDir()
	intentDir := filepath.Join(stateDir, "runtime", "validator-1", "state")
	if err := os.MkdirAll(intentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	intent := validatorpkg.SteeringIntent{
		Schema: "urnetwork-validator-steering-intent-v4", SubnetEpoch: 9, SelfUID: 4, MaskedUIDs: []uint16{4},
		EligibleHeadUIDs: []uint16{10, 11, 12}, SelectedHeadUIDs: []uint16{10, 11}, RejectedHeadUIDs: []uint16{12},
		StaleHeadBindings: []validatorpkg.StaleHeadBinding{}, Status: "applied",
		DepositAudits: []validatorpkg.DepositAudit{{NoID: 1, Epoch: 4, SourceEpoch: 3, Status: validatorpkg.DepositAuditCompliant, Compliant: true, Disposition: "pool_weight_eligible", RequiredDepositRao: "1", ObservedDepositRao: "1", ConvictionBeforeRao: "0", ArtifactHash: "sha256:test"}},
		UIDs:          []uint16{10, 11}, Scores: []validatorpkg.RationalJSON{{Numerator: "2", Denominator: "1"}, {Numerator: "1", Denominator: "1"}}, Values: []uint16{2, 1},
	}
	hash, err := intent.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	intent.VectorHash = hash
	store := map[string]any{"schema": "urnetwork-validator-steering-intent-v4", "current": intent, "history": []any{}}
	if err := writePublicJSON(filepath.Join(intentDir, "steering-intents.json"), store); err != nil {
		t.Fatal(err)
	}
	observed := inspectValidatorIntent(stateDir, 1, 2, 3)
	if observed.Error != "" || observed.CurrentEpoch != 9 || len(observed.EligibleHeadUIDs) != 3 || len(observed.SelectedHeadUIDs) != 2 || len(observed.RejectedHeadUIDs) != 1 || observed.StaleHeadBindings != 0 || len(observed.DepositAudits) != 1 || !observed.DepositAudits[0].Compliant {
		t.Fatalf("version four intent evidence was not preserved: %+v", observed)
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
	if definition.GoalEpochs != 4 || len(definition.Faults) != wantFaults {
		t.Fatalf("production definition = %+v", definition)
	}
	start := testScenarioObservation(cfg, 20)
	current := testScenarioObservation(cfg, 24)
	productionPolicy := PolicyView{
		EffectiveEpoch: 21, EpochBlocks: 2_400, RootCommitWindowBlocks: 200,
		FinalizeOffsetBlocks: 1_200, CloseGraceBlocks: 20,
	}
	start.Status.Contracts.Policy = productionPolicy
	current.Status.Contracts.Policy = productionPolicy
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
	minimum := time.Duration((300+3*2_400+1_200)*12+120) * time.Second
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

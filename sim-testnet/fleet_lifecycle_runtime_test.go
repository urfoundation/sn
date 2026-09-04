package main

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

func fleetLifecycleDecisionFixture(epoch uint64) *ScenarioObservation {
	settlementEpoch := epoch + 100
	candidateUIDs := make([]uint16, 202)
	candidateHotkeys := make([]string, 202)
	selected := make([]uint16, 0, 200)
	rejected := []uint16{fleetLifecycleTargetExpectedUID, fleetLifecycleCompanionExpectedUID}
	weights := make([]IntentWeightObservation, 0, 202)
	for index := range candidateUIDs {
		uid := uint16(index + 1)
		candidateUIDs[index] = uid
		candidateHotkeys[index] = "0x" + strings.Repeat("0", 60) + strings.ToLower(hexUint16(uid))
		value := uint16(1)
		if uid == rejected[0] || uid == rejected[1] {
			value = 0
		} else {
			selected = append(selected, uid)
		}
		weights = append(weights, IntentWeightObservation{UID: uid, Numerator: "1", Denominator: "1", Value: value})
	}
	validators := make([]ValidatorObservation, 0, 2)
	for validatorID := 1; validatorID <= 2; validatorID++ {
		nativeSnapshotHash := "0x" + strings.Repeat([]string{"9a", "9b"}[validatorID-1], 32)
		evmSnapshotHash := "0x" + strings.Repeat([]string{"aa", "ab"}[validatorID-1], 32)
		decision := HeadDecisionObservation{
			VectorHash: "0x" + strings.Repeat("10", 32), ExtrinsicHash: "0x" + strings.Repeat(string(rune('3'+validatorID)), 64),
			MeasurementArtifactHash: "sha256:" + strings.Repeat([]string{"21", "22"}[validatorID-1], 32),
			CandidateFleetUIDs:      append([]uint16(nil), candidateUIDs...), CandidateFleetHotkeys: append([]string(nil), candidateHotkeys...),
			SettlementEpoch: settlementEpoch,
			NativeSnapshot:  ChainHead{Number: uint64(70 + validatorID), Hash: nativeSnapshotHash},
			EVMSnapshot:     ChainHead{Number: uint64(60 + validatorID), Hash: evmSnapshotHash},
			FinalizedBlock:  uint64(80 + validatorID), FinalizedBlockHash: "0x" + strings.Repeat(string(rune('5'+validatorID)), 64), RevealBlock: uint64(85 + validatorID), RevealBlockHash: "0x" + strings.Repeat(string(rune('7'+validatorID)), 64), SubnetEpoch: epoch,
			ApplicationBlock: uint64(90 + validatorID), ApplicationBlockHash: "0x" + strings.Repeat(string(rune('1'+validatorID)), 64),
			EligibleHeadUIDs: append([]uint16(nil), candidateUIDs...), SelectedHeadUIDs: append([]uint16(nil), selected...), RejectedHeadUIDs: append([]uint16(nil), rejected...), AppliedWeights: append([]IntentWeightObservation(nil), weights...),
		}
		validators = append(validators, ValidatorObservation{ValidatorID: validatorID, HeadDecisions: []HeadDecisionObservation{decision}})
	}
	return &ScenarioObservation{
		ObservationHash:    "0x" + strings.Repeat("aa", 32),
		Status:             &DeploymentStatus{Contracts: &ContractView{CurrentEpoch: settlementEpoch, FinalizedHead: ChainHead{Number: 80, Hash: "0x" + strings.Repeat("bb", 32)}}},
		NativeRewards:      &NativeRewardObservation{FinalizedHead: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("cc", 32)}},
		CandidateFleetUIDs: candidateUIDs, CandidateFleetHotkeys: candidateHotkeys, Validators: validators,
	}
}

func hexUint16(value uint16) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[(value>>12)&15], digits[(value>>8)&15], digits[(value>>4)&15], digits[value&15]})
}

func TestFleetLifecycleDecisionCensusRequiresExact202By200By2Partitions(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("exact lifecycle census ready=%t error=%v", ready, err)
	}
	if len(census.CandidateUIDs) != 202 || len(census.Validators) != 2 || !fleetLifecycleCensusRejects(census, fleetLifecycleTargetExpectedUID, fleetLifecycleCompanionExpectedUID) || census.NativeObservedHead != observation.NativeRewards.FinalizedHead {
		t.Fatalf("exact lifecycle census=%+v", census)
	}
}

func TestFleetLifecycleDecisionCensusRejectsDuplicateValidatorIdentity(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	observation.Validators[1].ValidatorID = observation.Validators[0].ValidatorID
	if _, ready, err := fleetLifecycleDecisionCensus(observation); err == nil || !ready {
		t.Fatalf("duplicate validator identity ready=%t error=%v", ready, err)
	}
}

func TestFleetLifecycleDecisionCensusMaterializesMissingRejectedWeightAsZero(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	weights := observation.Validators[0].HeadDecisions[0].AppliedWeights
	for index := range weights {
		if weights[index].UID == fleetLifecycleTargetExpectedUID {
			observation.Validators[0].HeadDecisions[0].AppliedWeights = append(weights[:index], weights[index+1:]...)
			break
		}
	}
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("implicit rejected zero ready=%t error=%v", ready, err)
	}
	found := false
	for _, weight := range census.Validators[0].AppliedWeights {
		if weight.UID == fleetLifecycleTargetExpectedUID {
			found = weight.Value == 0
		}
	}
	if !found {
		t.Fatal("implicit rejected weight was not materialized as an exact zero")
	}
}

func TestFleetLifecycleDecisionCensusRejectsMissingSelectedWeight(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	selected := observation.Validators[0].HeadDecisions[0].SelectedHeadUIDs[0]
	weights := observation.Validators[0].HeadDecisions[0].AppliedWeights
	for index := range weights {
		if weights[index].UID == selected {
			observation.Validators[0].HeadDecisions[0].AppliedWeights = append(weights[:index], weights[index+1:]...)
			break
		}
	}
	if _, ready, err := fleetLifecycleDecisionCensus(observation); err == nil || !ready {
		t.Fatalf("missing selected weight ready=%t error=%v", ready, err)
	}
}

func TestFleetLifecycleDecisionCensusRejectsOverlappingSelectionPartition(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	decision := &observation.Validators[0].HeadDecisions[0]
	decision.SelectedHeadUIDs[0] = fleetLifecycleTargetExpectedUID
	if _, ready, err := fleetLifecycleDecisionCensus(observation); err == nil || !ready {
		t.Fatalf("overlapping decision partition ready=%t error=%v", ready, err)
	}
}

func TestFleetLifecycleDecisionCensusRejectsNoncanonicalApplicationHash(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	observation.Validators[0].HeadDecisions[0].ApplicationBlockHash = "native-head"
	if _, ready, err := fleetLifecycleDecisionCensus(observation); err == nil || !ready {
		t.Fatalf("noncanonical application hash ready=%t error=%v", ready, err)
	}
}

func TestFleetLifecycleDecisionCensusRejectsMissingCommitReceipt(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	observation.Validators[0].HeadDecisions[0].ExtrinsicHash = ""
	if _, ready, err := fleetLifecycleDecisionCensus(observation); err == nil || !ready {
		t.Fatalf("missing commit receipt ready=%t error=%v", ready, err)
	}
}

func TestFleetLifecycleDecisionCensusRejectsApplicationBeforeReveal(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	observation.Validators[0].HeadDecisions[0].ApplicationBlock = observation.Validators[0].HeadDecisions[0].RevealBlock - 1
	if _, ready, err := fleetLifecycleDecisionCensus(observation); err == nil || !ready {
		t.Fatalf("application before reveal ready=%t error=%v", ready, err)
	}
}

func TestFleetLifecycleCensusReceiptsRemainInsideTheirExactAcceptedEpoch(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("fixture census ready=%t error=%v", ready, err)
	}
	evidence := &FleetLifecycleEvidence{
		FirstAcceptedEpoch:              110,
		AcceptanceStartBlock:            1,
		AcceptanceEndBlock:              1_501,
		AcceptanceTerminalBlock:         1_651,
		ReleaseEVMEvidenceDeadlineBlock: 1_651,
		ReleaseHandoffSchedule:          &FleetLifecycleNativeSchedule{ApplicationDeadlineBlock: 1_651},
	}
	if err := validateFleetLifecycleCensusBlockRange(evidence, census); err != nil {
		t.Fatal(err)
	}
}

func TestFleetLifecycleCensusRejectsReceiptOrObservationOutsideItsAcceptedEpoch(t *testing.T) {
	evidence := &FleetLifecycleEvidence{
		FirstAcceptedEpoch:              110,
		AcceptanceStartBlock:            1,
		AcceptanceEndBlock:              1_501,
		AcceptanceTerminalBlock:         1_651,
		ReleaseEVMEvidenceDeadlineBlock: 1_651,
		ReleaseHandoffSchedule:          &FleetLifecycleNativeSchedule{ApplicationDeadlineBlock: 1_651},
	}
	for _, mutation := range []struct {
		name   string
		mutate func(*FleetLifecycleCandidateCensus)
	}{
		{name: "validator EVM snapshot", mutate: func(census *FleetLifecycleCandidateCensus) { census.Validators[0].EVMSnapshot.Number = 301 }},
		{name: "native observation before application", mutate: func(census *FleetLifecycleCandidateCensus) { census.NativeObservedHead.Number = 90 }},
		{name: "commit", mutate: func(census *FleetLifecycleCandidateCensus) { census.Validators[0].Commit.Number = 301 }},
		{name: "reveal", mutate: func(census *FleetLifecycleCandidateCensus) { census.Validators[0].RevealBlock = 301 }},
		{name: "application", mutate: func(census *FleetLifecycleCandidateCensus) { census.Validators[0].Application.Number = 301 }},
	} {
		observation := fleetLifecycleDecisionFixture(10)
		census, ready, err := fleetLifecycleDecisionCensus(observation)
		if err != nil || !ready {
			t.Fatalf("%s fixture census ready=%t error=%v", mutation.name, ready, err)
		}
		mutation.mutate(&census)
		if err := validateFleetLifecycleCensusBlockRange(evidence, census); err == nil {
			t.Fatalf("%s outside the exact epoch was accepted", mutation.name)
		}
	}
}

func TestFleetLifecycleCensusAllowsDelayedCaptureAcrossSettlementBoundary(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("fixture census ready=%t error=%v", ready, err)
	}
	evidence := &FleetLifecycleEvidence{FirstAcceptedEpoch: 110, AcceptanceStartBlock: 1, AcceptanceEndBlock: 1_501, AcceptanceTerminalBlock: 1_651, ReleaseEVMEvidenceDeadlineBlock: 1_651, ReleaseHandoffSchedule: &FleetLifecycleNativeSchedule{ApplicationDeadlineBlock: 1_651}}
	census.ObservedHead = ChainHead{Number: 2_000, Hash: "0x" + strings.Repeat("dd", 32)}
	if err := validateFleetLifecycleCensusBlockRange(evidence, census); err != nil {
		t.Fatalf("delayed capture rejected even though validator snapshots remain in their exact settlement epoch: %v", err)
	}
	if census.PostAcceptance {
		t.Fatal("within-acceptance validator snapshots were mislabeled as post-acceptance because capture was delayed")
	}
}

func TestFleetLifecycleCensusRejectsEVMSnapshotBeyondSeparateTailBound(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("fixture census ready=%t error=%v", ready, err)
	}
	evidence := &FleetLifecycleEvidence{FirstAcceptedEpoch: 110, AcceptanceStartBlock: 1, AcceptanceEndBlock: 1_501, AcceptanceTerminalBlock: 1_651, ReleaseEVMEvidenceDeadlineBlock: 1_700, ReleaseHandoffSchedule: &FleetLifecycleNativeSchedule{ApplicationDeadlineBlock: 1_900}}
	census.ObservedHead = ChainHead{Number: 2_000, Hash: "0x" + strings.Repeat("de", 32)}
	census.Validators[0].EVMSnapshot.Number = 1_701
	if err := validateFleetLifecycleCensusBlockRange(evidence, census); err == nil {
		t.Fatal("validator EVM snapshot beyond the distinct signed EVM evidence bound was accepted")
	}
}

func TestFleetLifecycleCensusRejectsNativeApplicationBeyondNativeDeadline(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("fixture census ready=%t error=%v", ready, err)
	}
	evidence := &FleetLifecycleEvidence{FirstAcceptedEpoch: 110, AcceptanceStartBlock: 1, AcceptanceEndBlock: 1_501, AcceptanceTerminalBlock: 1_651, ReleaseEVMEvidenceDeadlineBlock: 2_000, ReleaseHandoffSchedule: &FleetLifecycleNativeSchedule{ApplicationDeadlineBlock: 150}}
	census.NativeObservedHead = ChainHead{Number: 201, Hash: "0x" + strings.Repeat("df", 32)}
	census.Validators[0].Application.Number = 151
	if err := validateFleetLifecycleCensusBlockRange(evidence, census); err == nil {
		t.Fatal("validator native application beyond its distinct native deadline was accepted")
	}
}

func TestFleetLifecycleRevealHeadsResolveFromCanonicalFinalizedBlocks(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("fixture census ready=%t error=%v", ready, err)
	}
	for index := range census.Validators {
		census.Validators[index].RevealBlockHash = ""
	}
	reads := 0
	if err := resolveFleetLifecycleRevealHeads(&census, func(block uint64) (string, error) {
		reads++
		return "0x" + strings.Repeat(hexUint16(uint16(block)), 16), nil
	}); err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatalf("canonical reveal reads=%d, want one per distinct block", reads)
	}
	for _, validator := range census.Validators {
		want := "0x" + strings.Repeat(hexUint16(uint16(validator.RevealBlock)), 16)
		if validator.RevealBlockHash != want {
			t.Fatalf("validator %d reveal head=%d/%s, want %s", validator.ValidatorID, validator.RevealBlock, validator.RevealBlockHash, want)
		}
	}
}

func TestFleetLifecycleRevealHeadsRejectCapturedCanonicalDrift(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("fixture census ready=%t error=%v", ready, err)
	}
	if err := resolveFleetLifecycleRevealHeads(&census, func(uint64) (string, error) {
		return "0x" + strings.Repeat("ee", 32), nil
	}); err == nil {
		t.Fatal("captured reveal hash differing from the canonical block was accepted")
	}
}

func TestFleetLifecycleTerminalCensusWaitsForBothRestoredProvidersToReceivePositiveWeight(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	stale, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("stale terminal census ready=%t error=%v", ready, err)
	}
	if fleetLifecycleCensusSelects(stale, fleetLifecycleCompanionExpectedUID, fleetLifecycleTerminalVictimUID) {
		t.Fatal("pre-filter-removal census was accepted as positive terminal recovery")
	}
	for validatorIndex := range observation.Validators {
		decision := &observation.Validators[validatorIndex].HeadDecisions[0]
		selected := decision.SelectedHeadUIDs[:0]
		for _, uid := range decision.SelectedHeadUIDs {
			if uid != 201 {
				selected = append(selected, uid)
			}
		}
		decision.SelectedHeadUIDs = append(selected, fleetLifecycleCompanionExpectedUID)
		sort.Slice(decision.SelectedHeadUIDs, func(i, j int) bool { return decision.SelectedHeadUIDs[i] < decision.SelectedHeadUIDs[j] })
		decision.RejectedHeadUIDs = []uint16{fleetLifecycleTargetExpectedUID, 201}
		for index := range decision.AppliedWeights {
			switch decision.AppliedWeights[index].UID {
			case fleetLifecycleCompanionExpectedUID:
				decision.AppliedWeights[index].Value = 1
			case 201:
				decision.AppliedWeights[index].Value = 0
			}
		}
	}
	restored, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("restored terminal census ready=%t error=%v", ready, err)
	}
	if !fleetLifecycleCensusSelects(restored, fleetLifecycleCompanionExpectedUID, fleetLifecycleTerminalVictimUID) {
		t.Fatal("both validators' positive terminal provider weights were not recognized")
	}
}

func TestFleetLifecycleAcceptanceWindowBindsExactFiveEpochGeometry(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	lifecycle := &liveFleetLifecycle{
		cfg: cfg, stateDir: stateDir,
		evidence: &FleetLifecycleEvidence{Schema: fleetLifecycleEvidenceSchema, DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: "plan", RunID: "run", Stage: fleetLifecycleStageAwaitingDemotion, TakeoverEffectiveEpoch: 10},
	}
	window := &ScenarioAcceptanceWindow{EpochCount: 5, EpochBlocks: 300, FinalizeOffsetBlocks: 150, FirstEpoch: 10, StartBlock: 1_000, EndBlock: 2_500, TerminalBlock: 2_650}
	if err := lifecycle.BindAcceptanceWindow(window); err != nil {
		t.Fatal(err)
	}
	if lifecycle.evidence.FirstAcceptedEpoch != 10 || lifecycle.evidence.AcceptanceStartBlock != 1_000 || lifecycle.evidence.AcceptanceEndBlock != 2_500 || lifecycle.evidence.AcceptanceTerminalBlock != 2_650 {
		t.Fatalf("persisted acceptance geometry=%+v", lifecycle.evidence)
	}
}

func TestFleetLifecycleAcceptanceWindowRejectsCompactOrShiftedGeometry(t *testing.T) {
	cfg := testResolvedConfig(t)
	for _, window := range []*ScenarioAcceptanceWindow{
		{EpochCount: 4, EpochBlocks: 300, FinalizeOffsetBlocks: 150, FirstEpoch: 10, StartBlock: 1_000, EndBlock: 2_200, TerminalBlock: 2_350},
		{EpochCount: 5, EpochBlocks: 299, FinalizeOffsetBlocks: 150, FirstEpoch: 10, StartBlock: 1_000, EndBlock: 2_495, TerminalBlock: 2_645},
		{EpochCount: 5, EpochBlocks: 300, FinalizeOffsetBlocks: 149, FirstEpoch: 10, StartBlock: 1_000, EndBlock: 2_500, TerminalBlock: 2_649},
		{EpochCount: 5, EpochBlocks: 300, FinalizeOffsetBlocks: 150, FirstEpoch: 11, StartBlock: 1_000, EndBlock: 2_500, TerminalBlock: 2_650},
	} {
		lifecycle := &liveFleetLifecycle{
			cfg: cfg, stateDir: t.TempDir(),
			evidence: &FleetLifecycleEvidence{Schema: fleetLifecycleEvidenceSchema, DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: "plan", RunID: "run", Stage: fleetLifecycleStageAwaitingDemotion, TakeoverEffectiveEpoch: 10},
		}
		if err := lifecycle.BindAcceptanceWindow(window); err == nil {
			t.Fatalf("invalid lifecycle acceptance window was accepted: %+v", window)
		}
	}
}

func fleetLifecycleNativeScheduleFixture(t *testing.T, phase string, start, terminal uint64) *FleetLifecycleNativeSchedule {
	t.Helper()
	state := crv4.EpochScheduleState{LastEpochBlock: start - 100, SubnetEpochIndex: 40, Tempo: 360, BlocksSinceLastStep: 100, CurrentBlock: start}
	reveal, err := crv4.PredictFirstRevealBlock(&state, 1)
	if err != nil {
		t.Fatal(err)
	}
	milestones, mutations, safety := uint64(1), uint64(0), uint64(fleetLifecycleMutationSafetyBlocks)
	if phase == "release-1.0" {
		milestones, mutations, safety = 3, 3, 0
	}
	remaining := (milestones - 1) * 2 * uint64(state.Tempo)
	deadline := reveal + remaining + mutations*fleetLifecycleMutationSafetyBlocks + safety
	return &FleetLifecycleNativeSchedule{
		Phase: phase, ObservedHead: ChainHead{Number: start, Hash: finalTestHex(byte(start))},
		LastEpochBlock: state.LastEpochBlock, SubnetEpoch: state.SubnetEpochIndex, Tempo: state.Tempo, BlocksSinceLastStep: state.BlocksSinceLastStep,
		RevealPeriodEpochs: 1, RequiredMilestones: milestones, RequiredMutations: mutations, ApplicationSafetyBlocks: safety,
		FirstQualifyingRevealBlock: reveal, ApplicationDeadlineBlock: deadline,
	}
}

func TestFleetLifecycleReleaseHandoffTailHasExactConservativeBound(t *testing.T) {
	required, err := fleetLifecycleReleaseScheduleRequired(360, 1)
	if err != nil || required != 2_460 {
		t.Fatalf("release lifecycle bound=%d error=%v, want 2460", required, err)
	}
	acceptanceSpan := uint64(5*300 + 150)
	if required-acceptanceSpan != 810 {
		t.Fatalf("release lifecycle tail=%d, want 810", required-acceptanceSpan)
	}
	schedule := fleetLifecycleNativeScheduleFixture(t, "release-1.0", 1_000, 2_650)
	if err := validateFleetLifecycleNativeSchedule(schedule, "release-1.0", 1_000, 2_650); err != nil {
		t.Fatal(err)
	}
}

func TestFleetLifecycleReleaseHandoffTailRejectsOneBlockShortOrOverflow(t *testing.T) {
	schedule := fleetLifecycleNativeScheduleFixture(t, "release-1.0", 1_000, 2_650)
	schedule.ApplicationDeadlineBlock--
	if err := validateFleetLifecycleNativeSchedule(schedule, "release-1.0", 1_000, 2_650); err == nil {
		t.Fatal("one-block-short release handoff deadline was accepted")
	}
	if _, err := fleetLifecycleReleaseScheduleRequired(math.MaxUint64, 1); err == nil {
		t.Fatal("overflowing release handoff geometry was accepted")
	}
}

func TestFleetLifecycleProductionScheduleRejectsStalePreApplicationDeadline(t *testing.T) {
	schedule := fleetLifecycleNativeScheduleFixture(t, "production-soak", 5_000, 6_260)
	if err := validateFleetLifecycleNativeSchedule(schedule, "production-soak", 5_000, 6_260); err != nil {
		t.Fatal(err)
	}
	schedule.ApplicationDeadlineBlock = schedule.FirstQualifyingRevealBlock + fleetLifecycleMutationSafetyBlocks - 1
	if err := validateFleetLifecycleNativeSchedule(schedule, "production-soak", 5_000, 6_260); err == nil {
		t.Fatal("production schedule ending before its exact application-finality reserve was accepted")
	}
}

func TestFleetLifecycleEVMEvidenceDeadlineUsesLaterIndependentBound(t *testing.T) {
	releaseTerminal := uint64(2_650)
	releaseSchedule := fleetLifecycleNativeScheduleFixture(t, "release-1.0", 1_000, releaseTerminal)
	if releaseSchedule.ApplicationDeadlineBlock != 3_000 {
		t.Fatalf("release fixture native deadline=%d, want 3000 after terminal %d", releaseSchedule.ApplicationDeadlineBlock, releaseTerminal)
	}
	releaseDeadline, err := fleetLifecycleExpectedEVMEvidenceDeadline(releaseTerminal, releaseSchedule)
	if err != nil || releaseDeadline != releaseSchedule.ApplicationDeadlineBlock {
		t.Fatalf("release EVM deadline=%d error=%v, want native deadline %d", releaseDeadline, err, releaseSchedule.ApplicationDeadlineBlock)
	}

	productionTerminal := uint64(6_260)
	productionSchedule := fleetLifecycleNativeScheduleFixture(t, "production-soak", 5_000, productionTerminal)
	if productionSchedule.FirstQualifyingRevealBlock != 5_260 || productionSchedule.ApplicationDeadlineBlock != 5_360 {
		t.Fatalf("production fixture reveal/native deadlines=%d/%d, want 5260/5360 before terminal %d", productionSchedule.FirstQualifyingRevealBlock, productionSchedule.ApplicationDeadlineBlock, productionTerminal)
	}
	productionDeadline, err := fleetLifecycleExpectedEVMEvidenceDeadline(productionTerminal, productionSchedule)
	if err != nil || productionDeadline != productionTerminal {
		t.Fatalf("production EVM deadline=%d error=%v, want acceptance terminal %d", productionDeadline, err, productionTerminal)
	}

	overflow := *productionSchedule
	overflow.ApplicationDeadlineBlock = math.MaxUint64
	if _, err := fleetLifecycleExpectedEVMEvidenceDeadline(productionTerminal, &overflow); err == nil {
		t.Fatal("overflowing inclusive EVM evidence deadline was accepted")
	}
}

func TestFleetLifecycleProductionCensusUsesIndependentEVMAndNativeDeadlines(t *testing.T) {
	schedule := fleetLifecycleNativeScheduleFixture(t, "production-soak", 5_000, 6_260)
	evidence := &FleetLifecycleEvidence{
		ProductionFirstSettlementEpoch:     200,
		ProductionAcceptanceStartBlock:     5_000,
		ProductionAcceptanceTerminalBlock:  6_260,
		ProductionNativeSchedule:           schedule,
		ProductionEVMEvidenceDeadlineBlock: 6_260,
	}
	census := FleetLifecycleCandidateCensus{
		Phase: "production-soak", ObservedHead: ChainHead{Number: 6_260}, NativeObservedHead: ChainHead{Number: 5_360},
		Validators: []FleetLifecycleValidatorCensus{
			{SettlementEpoch: 203, NativeSnapshot: ChainHead{Number: 5_200}, EVMSnapshot: ChainHead{Number: 6_260}, Commit: ChainHead{Number: 5_260}, RevealBlock: 5_300, Application: ChainHead{Number: 5_359}},
			{SettlementEpoch: 203, NativeSnapshot: ChainHead{Number: 5_200}, EVMSnapshot: ChainHead{Number: 6_260}, Commit: ChainHead{Number: 5_260}, RevealBlock: 5_300, Application: ChainHead{Number: 5_360}},
		},
	}
	if err := validateFleetLifecycleCensusBlockRange(evidence, census); err != nil {
		t.Fatalf("exact EVM terminal/native application boundaries were rejected: %v", err)
	}
	census.Validators[0].EVMSnapshot.Number = 6_261
	if err := validateFleetLifecycleCensusBlockRange(evidence, census); err == nil {
		t.Fatal("EVM snapshot one block after its independent deadline was accepted")
	}
	census.Validators[0].EVMSnapshot.Number = 6_260
	census.Validators[0].Application.Number = 5_361
	census.NativeObservedHead.Number = 5_361
	if err := validateFleetLifecycleCensusBlockRange(evidence, census); err == nil {
		t.Fatal("native application one block after its independent deadline was accepted")
	}
}

func TestFleetLifecycleProductionScheduleStartsAfterTerminalMutation(t *testing.T) {
	evidence := &FleetLifecycleEvidence{
		TerminalRegistration:     &FleetLifecycleRegistrationEvidence{BlockNumber: 200},
		ProductionNativeSchedule: &FleetLifecycleNativeSchedule{ObservedHead: ChainHead{Number: 202}},
		CandidateCensuses: []FleetLifecycleCandidateCensus{{
			Milestone:  fleetLifecycleMilestoneProviderActive,
			Validators: []FleetLifecycleValidatorCensus{{Application: ChainHead{Number: 200}}, {Application: ChainHead{Number: 201}}},
		}},
	}
	if err := validateFleetLifecycleProductionSchedulePredecessor(evidence); err != nil {
		t.Fatal(err)
	}
}

func TestFleetLifecycleProductionScheduleRejectsPreTerminalHead(t *testing.T) {
	evidence := &FleetLifecycleEvidence{
		TerminalRegistration:     &FleetLifecycleRegistrationEvidence{BlockNumber: 200},
		ProductionNativeSchedule: &FleetLifecycleNativeSchedule{ObservedHead: ChainHead{Number: 200}},
		CandidateCensuses: []FleetLifecycleCandidateCensus{{
			Milestone:  fleetLifecycleMilestoneProviderActive,
			Validators: []FleetLifecycleValidatorCensus{{Application: ChainHead{Number: 190}}, {Application: ChainHead{Number: 191}}},
		}},
	}
	if err := validateFleetLifecycleProductionSchedulePredecessor(evidence); err == nil {
		t.Fatal("production schedule accepted a head at the terminal registration block")
	}
}

func TestFleetLifecycleProductionScheduleRejectsPreProviderApplicationHead(t *testing.T) {
	evidence := &FleetLifecycleEvidence{
		TerminalRegistration:     &FleetLifecycleRegistrationEvidence{BlockNumber: 190},
		ProductionNativeSchedule: &FleetLifecycleNativeSchedule{ObservedHead: ChainHead{Number: 200}},
		CandidateCensuses: []FleetLifecycleCandidateCensus{{
			Milestone:  fleetLifecycleMilestoneProviderActive,
			Validators: []FleetLifecycleValidatorCensus{{Application: ChainHead{Number: 199}}, {Application: ChainHead{Number: 201}}},
		}},
	}
	if err := validateFleetLifecycleProductionSchedulePredecessor(evidence); err == nil {
		t.Fatal("production schedule accepted a head before both provider applications")
	}
}

func TestFleetLifecycleReleaseMutationFitsExactDeadlineAndRejectsOneBlockShort(t *testing.T) {
	schedule := &FleetLifecycleNativeSchedule{Tempo: 360, RevealPeriodEpochs: 1, ApplicationDeadlineBlock: 3_000}
	census := FleetLifecycleCandidateCensus{Validators: []FleetLifecycleValidatorCensus{{Application: ChainHead{Number: 1_260}}, {Application: ChainHead{Number: 1_260}}}}
	lifecycle := &liveFleetLifecycle{evidence: &FleetLifecycleEvidence{ReleaseHandoffSchedule: schedule}}
	if err := lifecycle.releaseMutationScheduleFits(&census, 2, 3); err != nil {
		t.Fatal(err)
	}
	lifecycle.evidence.ReleaseHandoffSchedule.ApplicationDeadlineBlock--
	if err := lifecycle.releaseMutationScheduleFits(&census, 2, 3); err == nil {
		t.Fatal("release mutation geometry one block short was accepted")
	}
}

func TestFleetLifecycleMutationRangesStayInsideExactAcceptedEpochs(t *testing.T) {
	evidence := &FleetLifecycleEvidence{AcceptanceStartBlock: 1_000, AcceptanceEndBlock: 2_500, AcceptanceTerminalBlock: 2_650}
	want := []struct {
		offset uint64
		start  uint64
		end    uint64
	}{{0, 1_000, 1_300}, {2, 1_600, 1_900}, {4, 2_200, 2_500}, {5, 2_500, 2_651}}
	for _, expected := range want {
		start, end, err := fleetLifecycleBlockRange(evidence, expected.offset)
		if err != nil || start != expected.start || end != expected.end {
			t.Fatalf("epoch offset %d range=[%d,%d) error=%v, want [%d,%d)", expected.offset, start, end, err, expected.start, expected.end)
		}
		if !fleetLifecycleBlockInRange(start, start, end) || !fleetLifecycleBlockInRange(end-1, start, end) || fleetLifecycleBlockInRange(end, start, end) {
			t.Fatalf("epoch offset %d does not enforce its exclusive mutation boundary", expected.offset)
		}
	}
}

func TestFleetLifecycleFaultGeometrySpansExactAcceptedBoundaries(t *testing.T) {
	cfg := testResolvedConfig(t)
	faults, err := releaseFleetLifecycleFaults(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(faults) != 2 {
		t.Fatalf("lifecycle fault census=%d, want 2", len(faults))
	}
	wantBound, err := fleetLifecycleReleaseScheduleRequired(
		hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"]),
		hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["commit_reveal_period"]),
	)
	if err != nil {
		t.Fatal(err)
	}
	wantDuration := wantBound - 1
	wantAcceptanceDuration := uint64(cfg.Config.Scenarios.ShortEpochs)*cfg.Policy.Settlement.EpochBlocks - 1
	wantTargetMinimum := 4*cfg.Policy.Settlement.EpochBlocks - 1
	if !faults[0].PreAcceptance || !faults[1].PreAcceptance || !faults[0].PostAcceptanceEvidenceTail || !faults[1].PostAcceptanceEvidenceTail || faults[0].TriggerOffsetBlocks != 1 || faults[1].TriggerOffsetBlocks != 1 || faults[0].DurationBlocks != wantDuration || faults[1].DurationBlocks != wantDuration || faults[0].MinimumDurationBlocks != wantTargetMinimum || faults[1].MinimumDurationBlocks != wantAcceptanceDuration || faults[0].RestoreCondition != "fleet-lifecycle-provider-paid" || faults[1].RestoreCondition != "fleet-lifecycle-terminal-effective" {
		t.Fatalf("lifecycle fault geometry=%+v", faults)
	}
	records, err := initializeFaultRecords(1_000, faults)
	if err != nil {
		t.Fatal(err)
	}
	if records[0].TriggerBlock != 1_001 || records[0].RestoreBlock != 3_460 || records[1].RestoreBlock != 3_460 {
		t.Fatalf("lifecycle fault boundaries=%+v", records)
	}
}

func TestFleetLifecycleActionWavesAreCompleteAndOrdered(t *testing.T) {
	members := 4
	waves := []struct {
		name      string
		wantCount int
		first     string
		last      string
	}{
		{name: "prepare", wantCount: 16, first: "lifecycle.prepare.target.fund-hotkey", last: "lifecycle.prepare.companion.installed"},
		{name: "fallback", wantCount: 10, first: "lifecycle.fallback.fund", last: "lifecycle.fallback.installed"},
		{name: "provider", wantCount: 14, first: "lifecycle.provider.cleanup.1", last: "lifecycle.provider.installed"},
		{name: "terminal", wantCount: 18, first: "lifecycle.terminal.cleanup-companion.1", last: "lifecycle.terminal.installed"},
	}
	for _, wave := range waves {
		ids := fleetLifecycleActionIDs(wave.name, members)
		if len(ids) != wave.wantCount || ids[0] != wave.first || ids[len(ids)-1] != wave.last {
			t.Fatalf("%s lifecycle wave=%v", wave.name, ids)
		}
	}
}

func TestFleetLifecyclePayoutResumeRejectsSameDispositionWithDifferentRoot(t *testing.T) {
	lifecycle := &liveFleetLifecycle{evidence: &FleetLifecycleEvidence{}}
	payout := FleetLifecyclePayoutEvidence{Epoch: 10, NoID: 1, ContentHash: "sha256:first", PayoutRoot: "0x" + strings.Repeat("11", 32), Disposition: "pool"}
	if err := lifecycle.appendPayout(payout); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.appendPayout(payout); err != nil || len(lifecycle.evidence.Payouts) != 1 {
		t.Fatalf("idempotent payout append error=%v evidence=%+v", err, lifecycle.evidence.Payouts)
	}
	payout.PayoutRoot = "0x" + strings.Repeat("22", 32)
	if err := lifecycle.appendPayout(payout); err == nil {
		t.Fatal("same payout disposition with a different root was accepted")
	}
}

func TestFleetLifecyclePayoutSelectsExactHistoricalEpochAfterSkippedPoll(t *testing.T) {
	lifecycle, _ := fleetLifecyclePersistedStateFixture(t)
	miners := fleetLifecycleMembers(lifecycle.cfg, fleetLifecycleTargetFleet)
	for _, miner := range miners {
		label := fmt.Sprintf("miner-%d", miner)
		client := lifecycle.executor.roles.Clients[label]
		client.ClientIDHex = strings.Repeat("0", 28) + hexUint16(uint16(miner))
		lifecycle.executor.roles.Clients[label] = client
	}
	clientIDs, err := fleetLifecycleClientIDs(lifecycle.executor.roles, miners)
	if err != nil {
		t.Fatal(err)
	}
	noID := operatorForMiner(lifecycle.cfg, miners[0])
	clients := make([]OperatorPayoutClientTierObservation, 0, len(clientIDs))
	for _, clientID := range clientIDs {
		clients = append(clients, OperatorPayoutClientTierObservation{ClientID: clientID, Leaf: true})
	}
	observation := &ScenarioObservation{Operators: []OperatorObservation{{
		NoID: noID, LatestArtifactEpoch: 14,
		LifecyclePayoutArtifacts: []OperatorLifecyclePayoutArtifactObservation{{
			Epoch: 12, NoID: uint64(noID), ContentHash: "sha256:" + strings.Repeat("22", 32), PayoutRoot: "0x" + strings.Repeat("33", 32), Clients: clients,
		}},
	}}}
	payout, found, err := lifecycle.payoutEvidence(observation, 12, miners, false, "historical-pool")
	if err != nil || !found || payout.Epoch != 12 || payout.ContentHash != "sha256:"+strings.Repeat("22", 32) {
		t.Fatalf("historical payout=%+v found=%t error=%v", payout, found, err)
	}
}

func TestFleetLifecyclePayoutRejectsOppositeOrAmbiguousTier(t *testing.T) {
	lifecycle, _ := fleetLifecyclePersistedStateFixture(t)
	miners := fleetLifecycleMembers(lifecycle.cfg, fleetLifecycleTargetFleet)
	for _, miner := range miners {
		label := fmt.Sprintf("miner-%d", miner)
		client := lifecycle.executor.roles.Clients[label]
		client.ClientIDHex = strings.Repeat("0", 28) + hexUint16(uint16(miner))
		lifecycle.executor.roles.Clients[label] = client
	}
	clientIDs, err := fleetLifecycleClientIDs(lifecycle.executor.roles, miners)
	if err != nil {
		t.Fatal(err)
	}
	noID := operatorForMiner(lifecycle.cfg, miners[0])
	clients := make([]OperatorPayoutClientTierObservation, 0, len(clientIDs))
	for _, clientID := range clientIDs {
		clients = append(clients, OperatorPayoutClientTierObservation{ClientID: clientID, HeadExcluded: true})
	}
	observation := &ScenarioObservation{Operators: []OperatorObservation{{NoID: noID, LifecyclePayoutArtifacts: []OperatorLifecyclePayoutArtifactObservation{{Epoch: 12, NoID: uint64(noID), ContentHash: "sha256:" + strings.Repeat("22", 32), PayoutRoot: "0x" + strings.Repeat("33", 32), Clients: clients}}}}}
	if _, _, err := lifecycle.payoutEvidence(observation, 12, miners, false, "wrong-tier"); err == nil {
		t.Fatal("head-excluded clients were accepted as leaf payouts")
	}
	observation.Operators[0].LifecyclePayoutArtifacts[0].Clients[0].Leaf = true
	if _, _, err := lifecycle.payoutEvidence(observation, 12, miners, true, "ambiguous-tier"); err == nil {
		t.Fatal("client duplicated across payout tiers was accepted")
	}
}

func TestFleetLifecyclePayoutRejectsMissingRequestedEpochAfterNewerArtifact(t *testing.T) {
	lifecycle, _ := fleetLifecyclePersistedStateFixture(t)
	miners := fleetLifecycleMembers(lifecycle.cfg, fleetLifecycleTargetFleet)
	noID := operatorForMiner(lifecycle.cfg, miners[0])
	observation := &ScenarioObservation{Operators: []OperatorObservation{{NoID: noID, LatestArtifactEpoch: 13}}}
	if _, _, err := lifecycle.payoutEvidence(observation, 12, miners, false, "missing-history"); err == nil {
		t.Fatal("newer payout artifact concealed a missing requested historical epoch")
	}
}

func TestFleetLifecycleCensusResumeRejectsSameApplicationsWithDifferentDecision(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("fixture census ready=%t error=%v", ready, err)
	}
	lifecycle := &liveFleetLifecycle{evidence: &FleetLifecycleEvidence{}}
	if err := lifecycle.appendCensus(census); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.appendCensus(census); err != nil || len(lifecycle.evidence.CandidateCensuses) != 1 {
		t.Fatalf("idempotent census append error=%v evidence=%+v", err, lifecycle.evidence.CandidateCensuses)
	}
	census.Validators[0].RejectedUIDs[0]++
	if err := lifecycle.appendCensus(census); err == nil {
		t.Fatal("same application receipts with a different decision were accepted")
	}
}

func TestFleetLifecycleHistoricalCandidateIdentityComesFromEachMeasurement(t *testing.T) {
	oldArtifact := &validatorpkg.ReleaseMeasurementArtifact{Bindings: []validatorpkg.ReleaseBindingMeasurement{{Active: true, LiveUIDFound: true, LiveUID: 7, RecordUID: 7, Hotkey: "0x" + strings.Repeat("11", 32)}}}
	newArtifact := &validatorpkg.ReleaseMeasurementArtifact{Bindings: []validatorpkg.ReleaseBindingMeasurement{{Active: true, LiveUIDFound: true, LiveUID: 7, RecordUID: 7, Hotkey: "0x" + strings.Repeat("22", 32)}}}
	oldUIDs, oldHotkeys, err := headDecisionCandidateIdentities(oldArtifact, []uint16{7})
	if err != nil {
		t.Fatal(err)
	}
	newUIDs, newHotkeys, err := headDecisionCandidateIdentities(newArtifact, []uint16{7})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(oldUIDs, newUIDs) || slices.Equal(oldHotkeys, newHotkeys) || oldHotkeys[0] != "0x"+strings.Repeat("11", 32) || newHotkeys[0] != "0x"+strings.Repeat("22", 32) {
		t.Fatalf("historical identities old=%v/%v new=%v/%v", oldUIDs, oldHotkeys, newUIDs, newHotkeys)
	}
}

func TestFleetLifecycleDelayedPollRetainsPreAndPostUIDReuseIdentities(t *testing.T) {
	oldObservation := fleetLifecycleDecisionFixture(10)
	newObservation := fleetLifecycleDecisionFixture(11)
	oldHotkey := "0x" + strings.Repeat("31", 32)
	newHotkey := "0x" + strings.Repeat("32", 32)
	for validatorIndex := range oldObservation.Validators {
		oldDecision := &oldObservation.Validators[validatorIndex].HeadDecisions[0]
		newDecision := newObservation.Validators[validatorIndex].HeadDecisions[0]
		oldDecision.CandidateFleetHotkeys[6] = oldHotkey
		newDecision.CandidateFleetHotkeys[6] = newHotkey
		newDecision.NativeSnapshot.Number += 100
		newDecision.NativeSnapshot.Hash = "0x" + strings.Repeat([]string{"41", "42"}[validatorIndex], 32)
		newDecision.FinalizedBlock += 100
		newDecision.FinalizedBlockHash = "0x" + strings.Repeat([]string{"43", "44"}[validatorIndex], 32)
		newDecision.RevealBlock += 100
		newDecision.RevealBlockHash = "0x" + strings.Repeat([]string{"45", "46"}[validatorIndex], 32)
		newDecision.ApplicationBlock += 100
		newDecision.ApplicationBlockHash = "0x" + strings.Repeat([]string{"47", "48"}[validatorIndex], 32)
		newDecision.EVMSnapshot.Number += 300
		newDecision.EVMSnapshot.Hash = "0x" + strings.Repeat([]string{"49", "4a"}[validatorIndex], 32)
		oldObservation.Validators[validatorIndex].HeadDecisions = append(oldObservation.Validators[validatorIndex].HeadDecisions, newDecision)
	}
	oldObservation.Status.Contracts.CurrentEpoch = 111
	oldObservation.Status.Contracts.FinalizedHead = ChainHead{Number: 400, Hash: "0x" + strings.Repeat("4b", 32)}
	oldObservation.NativeRewards.FinalizedHead = ChainHead{Number: 300, Hash: "0x" + strings.Repeat("4c", 32)}
	censuses, ready, err := fleetLifecycleDecisionCensuses(oldObservation, "release-1.0", 1, 500)
	if err != nil || !ready || len(censuses) != 2 {
		t.Fatalf("delayed pre/post reuse census count=%d ready=%t error=%v", len(censuses), ready, err)
	}
	if censuses[0].CandidateHotkeys[6] != oldHotkey || censuses[1].CandidateHotkeys[6] != newHotkey {
		t.Fatalf("delayed poll collapsed historical UID identity to one terminal map: old=%s new=%s", censuses[0].CandidateHotkeys[6], censuses[1].CandidateHotkeys[6])
	}
}

func TestFleetLifecycleDecisionCollectionWaitsForBothValidatorSuccessors(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	successor := fleetLifecycleDecisionFixture(11).Validators[0].HeadDecisions[0]
	successor.EVMSnapshot.Number = 70
	observation.Validators[0].HeadDecisions = append(observation.Validators[0].HeadDecisions, successor)
	if censuses, ready, err := fleetLifecycleDecisionCensuses(observation, "release-1.0", 1, 500); err != nil || ready || len(censuses) != 0 {
		t.Fatalf("one-sided validator successor permitted an older mutation basis: censuses=%d ready=%t error=%v", len(censuses), ready, err)
	}
}

func TestFleetLifecycleCensusResumeRejectsTerminalIdentitySubstitution(t *testing.T) {
	observation := fleetLifecycleDecisionFixture(10)
	census, ready, err := fleetLifecycleDecisionCensus(observation)
	if err != nil || !ready {
		t.Fatalf("fixture census ready=%t error=%v", ready, err)
	}
	lifecycle := &liveFleetLifecycle{evidence: &FleetLifecycleEvidence{}}
	if err := lifecycle.appendCensus(census); err != nil {
		t.Fatal(err)
	}
	census.CandidateHotkeys[6] = "0x" + strings.Repeat("ef", 32)
	if err := lifecycle.appendCensus(census); err == nil {
		t.Fatal("terminal UID owner substituted into an earlier application receipt was accepted")
	}
}

//go:build linux || darwin

package main

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// The native reader already authenticated source and receipt coordinates.
// This fixture isolates an applied pool vector with no eligible head fleet.
func scenarioPoolOnlyObservationTestSource() *validatorpkg.ReleaseNativeSourceObservationV2 {
	source := scenarioNativeSourceV2TestFixture()
	source.References = source.References[:1]
	intent := &source.References[0].Intent
	intent.EligibleHeadUIDs, intent.EligibleHeadScores = nil, nil
	intent.SelectedHeadUIDs, intent.RejectedHeadUIDs = nil, nil
	intent.Values = []uint16{32767, 32768}
	intent.Scores = []validatorpkg.RationalJSON{{Numerator: "1", Denominator: "2"}, {Numerator: "1", Denominator: "2"}}
	artifact := source.References[0].Artifact
	artifact.Bindings = nil
	artifact.Pools = []validatorpkg.ReleasePoolMeasurement{
		{NoID: 1, UID: intent.UIDs[0], PoolHotkey: common.Hash{21}.Hex()},
		{NoID: 2, UID: intent.UIDs[1], PoolHotkey: common.Hash{22}.Hex()},
	}
	return source
}

func TestScenarioPoolOnlyObservationPreservesNativeProgressWithoutHeadCoverage(t *testing.T) {
	source := scenarioPoolOnlyObservationTestSource()
	got, err := projectScenarioNativeSourcesV2(source, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Error != "" || got.NativeSourceScopeV2 != scenarioNativeSourceScopeV2 || got.FinalizedIntents != 1 || got.AppliedIntents != 1 || len(got.NativeCommitsV2) != 1 || len(got.AppliedWeights) != 2 || len(got.HeadDecisions) != 1 {
		t.Fatalf("pool-only native source lost authenticated progress: %+v", got)
	}
	decision := got.HeadDecisions[0]
	if len(decision.CandidateFleetUIDs) != 0 || len(decision.CandidateFleetHotkeys) != 0 || len(got.EligibleHeadUIDs) != 0 || len(got.SelectedHeadUIDs) != 0 || got.HeadDecisionEpochs != 0 {
		t.Fatalf("pool-only vector invented head coverage: %+v", got)
	}
	if ok, detail := validateHeadSlotBoundary(got, 1, 2); ok || !strings.Contains(detail, "eligible/selected/rejected=0/0/0") {
		t.Fatalf("pool-only vector passed the required head gate: %t %s", ok, detail)
	}
	if decision.ApplicationBlock != source.References[0].Intent.ApplicationBlock || decision.ApplicationNativeEpoch != source.References[0].Lifecycle.ApplicationNativeEpoch {
		t.Fatal("pool-only observation lost canonical application coordinates")
	}
	got.AppliedWeights[0].Value = 1
	if source.References[0].Intent.Values[0] != 32767 {
		t.Fatal("pool-only projection borrowed its source vector")
	}
}

func TestScenarioPoolOnlyObservationPreservesLocalScopeAndSignedStore(t *testing.T) {
	cfg, observed, path, intent, _ := provisionalIntentProjectionSourceTest(t, scenarioPoolOnlyObservationTestSource())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	got := inspectProvisionalValidatorIntentObserved(context.Background(), cfg, 1, func() (ValidatorObservation, string) {
		calls++
		return observed, "synthetic-pool-only-generation"
	})
	if calls != 2 || got.Error != "" || got.LocalRuntimeIntents == nil || got.LocalRuntimeIntents.State != "observed" || got.VectorHash != intent.VectorHash || len(got.AppliedWeights) != 2 || len(got.HeadDecisions) != 1 || got.HeadDecisions[0].Error != "" {
		t.Fatalf("signed pool-only local observation failed: calls=%d %+v", calls, got)
	}
	if got.FinalizedIntents != 0 || got.AppliedIntents != 0 || got.NativeSourceScopeV2 != "" || len(got.NativeCommitsV2) != 0 || got.LocalRuntimeIntents.FinalAcceptance || got.HeadDecisionEpochs != 0 {
		t.Fatal("local pool-only observation promoted claims or fabricated head coverage")
	}
	if got.LocalRuntimeIntents.RecordedAppliedIntents == nil || *got.LocalRuntimeIntents.RecordedAppliedIntents != 1 {
		t.Fatal("local applied receipt claim was discarded")
	}
	if ok, _ := validateHeadSlotBoundary(got, 1, 2); ok {
		t.Fatal("local pool-only source passed the final head coverage gate")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("pool-only observation mutated original signed state", err)
	}
}

func TestScenarioPoolOnlyObservationRejectsEmptyAndInvalidAppliedVectors(t *testing.T) {
	for _, fault := range []string{"empty", "length", "duplicate", "zero-values", "zero-score", "negative-score", "zero-denominator", "noncanonical-score", "missing-source"} {
		source := scenarioPoolOnlyObservationTestSource()
		intent := &source.References[0].Intent
		switch fault {
		case "empty":
			intent.UIDs, intent.Values, intent.Scores = nil, nil, nil
		case "length":
			intent.Values = intent.Values[:1]
		case "duplicate":
			intent.UIDs[1] = intent.UIDs[0]
		case "zero-values":
			intent.Values = []uint16{0, 0}
		case "zero-score":
			intent.Scores[0].Numerator = "0"
		case "negative-score":
			intent.Scores[0].Numerator = "-1"
		case "zero-denominator":
			intent.Scores[0].Denominator = "0"
		case "noncanonical-score":
			intent.Scores[0].Numerator = "01"
		case "missing-source":
			source.References[0].Artifact = nil
		}
		got, err := projectScenarioNativeSourcesV2(source, 1, 1, 2)
		if err == nil || !reflect.DeepEqual(got, ValidatorObservation{}) {
			t.Fatalf("%s: invalid native source retained partial progress: %+v %v", fault, got, err)
		}
		local := projectValidatorIntent([]validatorpkg.SteeringIntent{*intent}, 1, 1, 2, func(*validatorpkg.SteeringIntent) (*validatorpkg.ReleaseMeasurementArtifact, error) {
			return source.References[0].Artifact, nil
		})
		if len(local.HeadDecisions) != 1 || local.HeadDecisions[0].Error == "" {
			t.Fatalf("%s: invalid local source was silently projected: %+v", fault, local)
		}
	}
}

func TestScenarioPoolOnlyObservationKeepsExactNonemptyHeadIdentities(t *testing.T) {
	for _, fault := range []string{"missing", "duplicate", "inactive", "cleaned", "stale-uid", "invalid-hotkey", "conflict"} {
		source := scenarioNativeSourceV2TestFixture()
		artifact, eligible := source.References[0].Artifact, source.References[0].Intent.EligibleHeadUIDs
		switch fault {
		case "missing":
			artifact = nil
		case "duplicate":
			eligible = []uint16{11, 11}
		case "inactive":
			artifact.Bindings[0].Active = false
		case "cleaned":
			artifact.Bindings[0].Cleaned = true
		case "stale-uid":
			artifact.Bindings[0].LiveUID++
		case "invalid-hotkey":
			artifact.Bindings[0].Hotkey = "not-a-hotkey"
		case "conflict":
			conflict := artifact.Bindings[0]
			conflict.Hotkey = common.Hash{33}.Hex()
			artifact.Bindings = append(artifact.Bindings, conflict)
		}
		if _, _, err := headDecisionCandidateIdentities(artifact, eligible); err == nil {
			t.Fatalf("%s: nonempty head identity checks were weakened", fault)
		}
	}
}

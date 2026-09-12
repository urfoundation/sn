//go:build linux || darwin

package main

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// Projection has no replay authority. This fixture isolates how already
// observed source/receipt coordinates become operational progress, including
// a newer finalized commit while the preceding vector remains applied.
func scenarioNativeSourceV2TestFixture() *validatorpkg.ReleaseNativeSourceObservationV2 {
	measurement := bytesSHA256([]byte("original signed measurement"))
	intent := validatorpkg.SteeringIntent{ValidatorID: 1, Status: "applied", VectorHash: common.Hash{1}.Hex(), SubnetEpoch: 1405, SettlementEpoch: 309, SelfUID: 3, MeasurementArtifactHash: measurement,
		NativeSnapshotBlock: 100, NativeSnapshotHash: common.Hash{2}.Hex(), EVMSnapshotBlock: 95, EVMSnapshotHash: common.Hash{3}.Hex(), FinalizedBlock: 101, FinalizedBlockHash: common.Hash{4}.Hex(), RevealBlock: 461, ApplicationBlock: 463, ApplicationBlockHash: common.Hash{5}.Hex(),
		UIDs: []uint16{11, 12}, Values: []uint16{65535, 0}, Scores: []validatorpkg.RationalJSON{{Numerator: "1", Denominator: "1"}, {Numerator: "0", Denominator: "1"}}, EligibleHeadUIDs: []uint16{11, 12}, EligibleHeadScores: []validatorpkg.RationalJSON{{Numerator: "1", Denominator: "1"}, {Numerator: "0", Denominator: "1"}}, SelectedHeadUIDs: []uint16{11}, RejectedHeadUIDs: []uint16{12}}
	artifact := &validatorpkg.ReleaseMeasurementArtifact{Bindings: []validatorpkg.ReleaseBindingMeasurement{
		{Active: true, LiveUIDFound: true, LiveUID: 11, RecordUID: 11, Hotkey: common.Hash{11}.Hex()},
		{Active: true, LiveUIDFound: true, LiveUID: 12, RecordUID: 12, Hotkey: common.Hash{12}.Hex()},
	}}
	pending := intent
	pending.Status, pending.VectorHash, pending.SubnetEpoch = "finalized", common.Hash{6}.Hex(), 1406
	pending.MeasurementArtifactHash = bytesSHA256([]byte("next signed measurement"))
	pending.FinalizedBlock, pending.FinalizedBlockHash, pending.RevealBlock = 470, common.Hash{7}.Hex(), 821
	pending.ApplicationBlock, pending.ApplicationBlockHash = 0, ""
	return &validatorpkg.ReleaseNativeSourceObservationV2{StoreSHA256: bytesSHA256([]byte("original canonical store")), References: []validatorpkg.ReleaseNativeSourceReferenceV2{
		{Intent: intent, Artifact: artifact, Lifecycle: validatorpkg.ReleaseEvidenceV2DecisionObservation{MeasurementHash: measurement, CommitNativeEpoch: 1405, RevealNativeEpoch: 1406, ApplicationNativeEpoch: 1406}},
		{Intent: pending, Artifact: artifact, Lifecycle: validatorpkg.ReleaseEvidenceV2DecisionObservation{MeasurementHash: pending.MeasurementArtifactHash, CommitNativeEpoch: 1406}},
	}}
}

func TestScenarioNativeObservationV2PreservesAppliedRowAndLaterCommit(t *testing.T) {
	source := scenarioNativeSourceV2TestFixture()
	got, err := projectScenarioNativeSourcesV2(source, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.NativeSourceScopeV2 != scenarioNativeSourceScopeV2 || got.NativeSourceStoreSHA256 != source.StoreSHA256 || got.LocalRuntimeIntents != nil || got.CurrentStatus != "finalized" || got.FinalizedIntents != 2 || got.AppliedIntents != 1 || len(got.HeadDecisions) != 1 || len(got.NativeCommitsV2) != 2 {
		t.Fatalf("strict native observation lost its source/receipt scope: %+v", got)
	}
	decision := got.HeadDecisions[0]
	if decision.NativeSnapshot.Number != 100 || decision.EVMSnapshot.Number != 95 || decision.SettlementEpoch != 309 || decision.CommitNativeEpoch != 1405 || decision.RevealNativeEpoch != 1406 || decision.ApplicationNativeEpoch != 1406 || got.NativeCommitsV2[1].Block.Number != 470 || got.NativeCommitsV2[1].NativeEpoch != 1406 || got.AppliedWeights[0].Value != 65535 {
		t.Fatal("native/settlement clocks, applied vector or newer commit were conflated")
	}
	got.HeadDecisions[0].AppliedWeights[0].Value = 1
	got.SelectedHeadUIDs[0] = 99
	if source.References[0].Intent.Values[0] != 65535 || source.References[0].Intent.SelectedHeadUIDs[0] != 11 {
		t.Fatal("projected result borrowed mutable original source slices")
	}
	// A canonical commit remains relevant to LastUpdate even if later work
	// failed. Its failure is retained, not dropped from the commit census.
	source.References[1].Intent.Status, source.References[1].Intent.Error = "failed", "original failure"
	got, err = projectScenarioNativeSourcesV2(source, 1, 1, 2)
	if err != nil || len(got.NativeCommitsV2) != 2 || got.AppliedIntents != 1 {
		t.Fatal("failed later lifecycle hid a finalized native commit", err)
	}
}

func TestScenarioNativeObservationV2RejectsIncompleteSourceWithoutPartialProgress(t *testing.T) {
	for _, problem := range []string{"source", "validator", "commit", "reveal", "application", "binding", "vector"} {
		t.Run(problem, func(t *testing.T) {
			source := scenarioNativeSourceV2TestFixture()
			switch problem {
			case "source":
				source.References[1].Lifecycle.MeasurementHash = bytesSHA256([]byte("other"))
			case "validator":
				source.References[1].Intent.ValidatorID = 2
			case "commit":
				source.References[1].Lifecycle.CommitNativeEpoch = 0
			case "reveal":
				source.References[0].Lifecycle.RevealNativeEpoch = 0
			case "application":
				source.References[0].Lifecycle.ApplicationNativeEpoch = 1404
			case "binding":
				source.References[0].Artifact.Bindings[0].LiveUIDFound = false
			case "vector":
				source.References[0].Intent.Values = nil
			}
			got, err := projectScenarioNativeSourcesV2(source, 1, 1, 2)
			if err == nil || !reflect.DeepEqual(got, ValidatorObservation{}) {
				t.Fatal("incomplete source retained partial progress", err)
			}
		})
	}
}

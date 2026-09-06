package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"testing"
)

// Cross-window artifacts retain independent terminal admission, full replay,
// exact successor folds and record-observed lineage at their public boundary.
// A signed batch alone cannot authorize prior quality or a rewritten prefix.
func TestProducerGateStateSelectionCompactMeasurementRetainsSettlementLineage(t *testing.T) {
	for _, edge := range []struct{ path, caller, callee string }{
		{path: "../validator/release_measurement_v2.go", caller: "ownReleaseMeasurementV2", callee: "admitAttemptSettlementV2"},
		{path: "../validator/release_measurement_v2.go", caller: "ownReleaseMeasurementV2", callee: "verifyAttemptSettlementV2Successor"},
		{path: "../validator/release_measurement_v2.go", caller: "ownReleaseMeasurementV2", callee: "Dir"},
		{path: "../validator/attempt_transition_v2_verify.go", caller: "ownAttemptSettlementV2Options", callee: "Dir"},
		{path: "../validator/release_measurement_v2.go", caller: "verifyOwnedReleaseMeasurementV2", callee: "replay"},
		{path: "../validator/release_measurement_v2.go", caller: "verifyOwnedReleaseMeasurementV2", callee: "visit"},
		{path: "../validator/release_measurement_v2.go", caller: "decodeReleaseMeasurementV2Bytes", callee: "decodeAttemptSettlementClosureV2Bytes"},
		{path: "../validator/release_measurement_v2.go", caller: "VerifyReleaseMeasurementLineageV2", callee: "releasePriorQualityState"},
		{path: "../validator/attempt_closure_v2.go", caller: "DecodeAttemptSettlementClosureV2", callee: "decodeAttemptSettlementClosureV2Bytes"},
		{path: "../validator/attempt_closure_v2.go", caller: "DecodeAttemptSettlementClosureV2", callee: "admitAttemptSettlementV2"},
		{path: "../validator/attempt_closure_v2.go", caller: "DecodeAttemptSettlementClosureV2", callee: "replay"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.caller)[edge.callee] {
			t.Errorf("%s lost required complete settlement lineage edge %s", edge.caller, edge.callee)
		}
	}
}

// Canonical wire, ordinary reconstruction and lineage must reach the same
// real policy replay and exact existing scoring math, never an unused helper.
func TestProducerGateStateSelectionCompactMeasurementRetainsFullReplayAndMath(t *testing.T) {
	for _, edge := range []struct{ path, caller, callee string }{
		{path: "../validator/release_measurement_v2.go", caller: "DecodeReleaseMeasurementArtifactV2", callee: "VerifyReleaseMeasurementArtifactV2"},
		{path: "../validator/release_measurement_v2.go", caller: "VerifyReleaseMeasurementArtifactV2", callee: "ownReleaseMeasurementV2"},
		{path: "../validator/release_measurement_v2.go", caller: "VerifyReleaseMeasurementArtifactV2", callee: "verifyOwnedReleaseMeasurementV2"},
		{path: "../validator/release_measurement_v2.go", caller: "ownReleaseMeasurementV2", callee: "equalAttemptCutV2CommonDomain"},
		{path: "../validator/release_measurement_v2.go", caller: "VerifyReleaseMeasurementIntentV2", callee: "releaseMeasurementV2ControlStorage"},
		{path: "../validator/release_measurement_v2.go", caller: "VerifyReleaseMeasurementIntentV2", callee: "DecodeReleaseMeasurementArtifactV2"},
		{path: "../validator/release_measurement_v2.go", caller: "VerifyReleaseMeasurementIntentV2", callee: "VerifyReleaseMeasurementIntent"},
		{path: "../validator/release_measurement_v2.go", caller: "SealReleaseMeasurementArtifactV2", callee: "verifyOwnedReleaseMeasurementV2"},
		{path: "../validator/release_measurement_v2.go", caller: "VerifyReleaseMeasurementLineageV2", callee: "verifyOwnedReleaseMeasurementV2"},
		{path: "../validator/release_measurement_v2.go", caller: "VerifyReleaseMeasurementLineageV2", callee: "verifyReleaseMeasurementHeadLineage"},
		{path: "../validator/release_measurement_v2.go", caller: "verifyOwnedReleaseMeasurementV2", callee: "releaseMeasurementBindingObservations"},
		{path: "../validator/release_measurement_v2.go", caller: "verifyOwnedReleaseMeasurementV2", callee: "verifyReleaseStatsAndHeadWithAttemptCutV2"},
		{path: "../validator/release_measurement_v2.go", caller: "verifyOwnedReleaseMeasurementV2", callee: "assembleReleaseMeasurement"},
		{path: "../validator/attempt_cut_v2_measurement.go", caller: "VerifyReleaseStatsAndHeadWithAttemptCutV2", callee: "verifyReleaseStatsAndHeadWithAttemptCutV2"},
		{path: "../validator/attempt_cut_v2_measurement.go", caller: "verifyReleaseStatsAndHeadWithAttemptCutV2", callee: "ReplayAttemptCutV2WithPolicy"},
		{path: "../validator/attempt_cut_v2_measurement.go", caller: "verifyReleaseStatsAndHeadWithAttemptCutV2", callee: "newAttemptCutV2StatsProjection"},
		{path: "../validator/attempt_cut_v2_measurement.go", caller: "verifyReleaseStatsAndHeadWithAttemptCutV2", callee: "newAttemptCutV2HeadProjection"},
		{path: "../validator/release_measurement.go", caller: "releaseMeasurementBindings", callee: "releaseMeasurementBindingObservations"},
		{path: "../validator/release_measurement.go", caller: "VerifyReleaseMeasurementArtifact", callee: "assembleReleaseMeasurement"},
		{path: "../validator/release_measurement.go", caller: "assembleReleaseMeasurement", callee: "releaseMeasurementHead"},
		{path: "../validator/release_measurement.go", caller: "assembleReleaseMeasurement", callee: "releaseMeasurementPools"},
		{path: "../validator/release_measurement.go", caller: "assembleReleaseMeasurement", callee: "BuildWeightVectorExact"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.caller)[edge.callee] {
			t.Errorf("%s lost required complete compact measurement edge %s", edge.caller, edge.callee)
		}
	}
	for _, function := range []string{"verifyOwnedReleaseMeasurementV2", "VerifyReleaseMeasurementArtifactV2", "SealReleaseMeasurementArtifactV2"} {
		calls := releaseClosureFunctionCalls(t, "../validator/release_measurement_v2.go", function)
		if calls["VerifyReleaseMeasurementArtifact"] || calls["ReplayAttemptCutV2"] || calls["AttemptCutEgressClaims"] {
			t.Errorf("%s bypasses explicit compact authority with a legacy/stateless route", function)
		}
	}
	// The snapshot owns every field read by the actual common intent join,
	// plus the two explicit content-address pins checked by the v2 wrapper.
	function := func(path, name string) *ast.FuncDecl {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			if declaration, ok := declaration.(*ast.FuncDecl); ok && declaration.Name.Name == name {
				return declaration
			}
		}
		t.Fatalf("missing actual measurement function %s", name)
		return nil
	}
	wanted := map[string]bool{"MeasurementArtifactHash": true, "MeasurementArtifactSize": true}
	ast.Inspect(function("../validator/release_measurement.go", "VerifyReleaseMeasurementIntent").Body, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "intent" {
				wanted[selector.Sel.Name] = true
			}
		}
		return true
	})
	owned := map[string]bool{}
	ast.Inspect(function("../validator/release_measurement_v2.go", "VerifyReleaseMeasurementIntentV2").Body, func(node ast.Node) bool {
		if literal, ok := node.(*ast.CompositeLit); ok {
			if name, ok := literal.Type.(*ast.Ident); ok && name.Name == "SteeringIntent" {
				for _, element := range literal.Elts {
					entry, ok := element.(*ast.KeyValueExpr)
					if !ok {
						t.Fatal("intent snapshot lost explicit field ownership")
					}
					key, ok := entry.Key.(*ast.Ident)
					if !ok {
						t.Fatal("intent snapshot field is not named")
					}
					owned[key.Name] = true
				}
			}
		}
		return true
	})
	if !maps.Equal(wanted, owned) {
		t.Fatalf("compact intent snapshot differs from actual compared fields: wanted=%v owned=%v", wanted, owned)
	}
}

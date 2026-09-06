package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"testing"
)

// Canonical wire, ordinary reconstruction and lineage must reach the same
// real policy replay and exact existing scoring math, never an unused helper.
func TestProducerGateStateSelectionCompactMeasurementRetainsFullReplayAndMath(t *testing.T) {
	for _, edge := range []struct{ path, caller, callee string }{
		{"../validator/release_measurement_v2.go", "DecodeReleaseMeasurementArtifactV2", "VerifyReleaseMeasurementArtifactV2"},
		{"../validator/release_measurement_v2.go", "VerifyReleaseMeasurementArtifactV2", "ownReleaseMeasurementV2"},
		{"../validator/release_measurement_v2.go", "VerifyReleaseMeasurementArtifactV2", "verifyOwnedReleaseMeasurementV2"},
		{"../validator/release_measurement_v2.go", "ownReleaseMeasurementV2", "equalAttemptCutV2CommonDomain"},
		{"../validator/release_measurement_v2.go", "VerifyReleaseMeasurementIntentV2", "ownReleaseMeasurementV2Intent"},
		{"../validator/release_measurement_v2.go", "VerifyReleaseMeasurementIntentV2", "DecodeReleaseMeasurementArtifactV2"},
		{"../validator/release_measurement_v2.go", "VerifyReleaseMeasurementIntentV2", "VerifyReleaseMeasurementIntent"},
		{"../validator/release_measurement_v2.go", "SealReleaseMeasurementArtifactV2", "verifyOwnedReleaseMeasurementV2"},
		{"../validator/release_measurement_v2.go", "VerifyReleaseMeasurementLineageV2", "verifyOwnedReleaseMeasurementV2"},
		{"../validator/release_measurement_v2.go", "VerifyReleaseMeasurementLineageV2", "verifyReleaseMeasurementHeadLineage"},
		{"../validator/release_measurement_v2.go", "verifyOwnedReleaseMeasurementV2", "releaseMeasurementBindingObservations"},
		{"../validator/release_measurement_v2.go", "verifyOwnedReleaseMeasurementV2", "verifyReleaseStatsAndHeadWithAttemptCutV2"},
		{"../validator/release_measurement_v2.go", "verifyOwnedReleaseMeasurementV2", "assembleReleaseMeasurement"},
		{"../validator/attempt_cut_v2_measurement.go", "VerifyReleaseStatsAndHeadWithAttemptCutV2", "verifyReleaseStatsAndHeadWithAttemptCutV2"},
		{"../validator/attempt_cut_v2_measurement.go", "verifyReleaseStatsAndHeadWithAttemptCutV2", "ReplayAttemptCutV2WithPolicy"},
		{"../validator/attempt_cut_v2_measurement.go", "verifyReleaseStatsAndHeadWithAttemptCutV2", "newAttemptCutV2StatsProjection"},
		{"../validator/attempt_cut_v2_measurement.go", "verifyReleaseStatsAndHeadWithAttemptCutV2", "newAttemptCutV2HeadProjection"},
		{"../validator/release_measurement.go", "releaseMeasurementBindings", "releaseMeasurementBindingObservations"},
		{"../validator/release_measurement.go", "VerifyReleaseMeasurementArtifact", "assembleReleaseMeasurement"},
		{"../validator/release_measurement.go", "assembleReleaseMeasurement", "releaseMeasurementHead"},
		{"../validator/release_measurement.go", "assembleReleaseMeasurement", "releaseMeasurementPools"},
		{"../validator/release_measurement.go", "assembleReleaseMeasurement", "BuildWeightVectorExact"},
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
		parsed,err:=parser.ParseFile(token.NewFileSet(),path,nil,0)
		if err!=nil{t.Fatal(err)}
		for _,declaration:=range parsed.Decls{
			if declaration,ok:=declaration.(*ast.FuncDecl);ok&&declaration.Name.Name==name{return declaration}
		}
		t.Fatalf("missing actual measurement function %s",name)
		return nil
	}
	wanted:=map[string]bool{"MeasurementArtifactHash":true,"MeasurementArtifactSize":true}
	ast.Inspect(function("../validator/release_measurement.go","VerifyReleaseMeasurementIntent").Body,func(node ast.Node)bool{
		if selector,ok:=node.(*ast.SelectorExpr);ok{
			if receiver,ok:=selector.X.(*ast.Ident);ok&&receiver.Name=="intent"{wanted[selector.Sel.Name]=true}
		}
		return true
	})
	owned:=map[string]bool{}
	ast.Inspect(function("../validator/release_measurement_v2.go","ownReleaseMeasurementV2Intent").Body,func(node ast.Node)bool{
		if literal,ok:=node.(*ast.CompositeLit);ok{
			if name,ok:=literal.Type.(*ast.Ident);ok&&name.Name=="SteeringIntent"{
				for _,element:=range literal.Elts{
					entry,ok:=element.(*ast.KeyValueExpr);if !ok{t.Fatal("intent snapshot lost explicit field ownership")}
					key,ok:=entry.Key.(*ast.Ident);if !ok{t.Fatal("intent snapshot field is not named")}
					owned[key.Name]=true
				}
			}
		}
		return true
	})
	if !maps.Equal(wanted,owned){t.Fatalf("compact intent snapshot differs from actual compared fields: wanted=%v owned=%v",wanted,owned)}
}

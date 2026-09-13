package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Both public v2 signing boundaries must reach full artifact replay and the
// real sr25519 verifier, while the legacy decoder remains explicitly legacy.
// These source edges supplement, never replace, the real M8 behavioral tests.
func TestProducerGateStateSelectionCompactEnvelopeRetainsReplayAndDomains(t *testing.T) {
	// The public signer cannot acquire the internal invocation replay cache.
	// Its one forwarding return must preserve every input and supply nil.
	file, err := parser.ParseFile(token.NewFileSet(), "../validator/release_measurement_envelope_v2.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var seal *ast.FuncDecl
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "SealReleaseMeasurementEnvelopeV2" {
			seal = function
		}
	}
	if seal == nil || seal.Body == nil || len(seal.Body.List) != 1 {
		t.Fatal("public compact signer is not one direct forwarding return")
	}
	statement, ok := seal.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(statement.Results) != 1 {
		t.Fatal("public compact signer does not return its full replay result")
	}
	call, ok := statement.Results[0].(*ast.CallExpr)
	arguments := []string{"ctx", "measurement", "validatorUID", "hotkey", "preparedExtrinsicHash", "signedAt", "options", "nil"}
	if !ok || len(call.Args) != len(arguments) {
		t.Fatal("public compact signer changed its replay call")
	}
	callee, ok := call.Fun.(*ast.Ident)
	if !ok || callee.Name != "sealReleaseMeasurementEnvelopeV2" {
		t.Fatal("public compact signer bypasses the owned replay boundary")
	}
	for index, expected := range arguments {
		argument, ok := call.Args[index].(*ast.Ident)
		if !ok || argument.Name != expected {
			t.Fatalf("public compact signer changed argument %d from %s", index, expected)
		}
	}
	for _, edge := range []struct{ path, caller, callee string }{
		{path: "../validator/release_measurement_envelope_v2.go", caller: "SealReleaseMeasurementEnvelopeV2", callee: "sealReleaseMeasurementEnvelopeV2"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "sealReleaseMeasurementEnvelopeV2", callee: "ownReleaseMeasurementEnvelopeV2Hotkey"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "sealReleaseMeasurementEnvelopeV2", callee: "validateReleaseMeasurementEnvelopeV2Authority"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "sealReleaseMeasurementEnvelopeV2", callee: "decodeReleaseMeasurementV2Bytes"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "sealReleaseMeasurementEnvelopeV2", callee: "admitReleaseMeasurementEnvelopeV2Storage"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "sealReleaseMeasurementEnvelopeV2", callee: "VerifyReleaseMeasurementArtifactV2"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "sealReleaseMeasurementEnvelopeV2", callee: "releaseMeasurementEnvelopeSigningDigestWithDomain"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "sealReleaseMeasurementEnvelopeV2", callee: "Sign"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "sealReleaseMeasurementEnvelopeV2", callee: "Clone"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "validateReleaseMeasurementEnvelopeV2Authority"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "releaseMeasurementEnvelopeV2Decision"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "DecodeReleaseMeasurementArtifactV2"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "releaseMeasurementEnvelopeMatchesArtifact"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "VerifyReleaseMeasurementEnvelopeV2", callee: "Clone"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "DecodeReleaseMeasurementEnvelopeV2", callee: "validateReleaseMeasurementEnvelopeV2"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "validateReleaseMeasurementEnvelopeV2", callee: "admitReleaseMeasurementEnvelopeV2Storage"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "admitReleaseMeasurementEnvelopeV2Storage", callee: "releaseMeasurementV2ControlStorage"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "admitReleaseMeasurementEnvelopeV2Storage", callee: "releaseMeasurementEnvelopeV2WireSize"},
		{path: "../validator/release_measurement_envelope_v2.go", caller: "validateReleaseMeasurementEnvelopeV2", callee: "validateReleaseMeasurementEnvelopeFieldsWithHexWork"},
		{path: "../validator/release_measurement_envelope.go", caller: "validateReleaseMeasurementEnvelopeFieldsWithHexWork", callee: "Verify"},
		{path: "../validator/release_measurement_envelope.go", caller: "DecodeReleaseMeasurementEnvelope", callee: "validateReleaseMeasurementEnvelope"},
		{path: "../validator/release_measurement_envelope.go", caller: "validateReleaseMeasurementEnvelope", callee: "validateReleaseMeasurementEnvelopeWithHexWork"},
		{path: "../validator/release_measurement_envelope.go", caller: "validateReleaseMeasurementEnvelopeWithHexWork", callee: "validateReleaseMeasurementEnvelopeFieldsWithHexWork"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.caller)[edge.callee] {
			t.Errorf("%s lost required envelope edge %s", edge.caller, edge.callee)
		}
	}
	for _, caller := range []string{"SealReleaseMeasurementEnvelopeV2", "sealReleaseMeasurementEnvelopeV2", "VerifyReleaseMeasurementEnvelopeV2", "DecodeReleaseMeasurementEnvelopeV2"} {
		calls := releaseClosureFunctionCalls(t, "../validator/release_measurement_envelope_v2.go", caller)
		for _, legacy := range []string{"DecodeReleaseMeasurementArtifact", "VerifyReleaseMeasurementArtifact", "DecodeReleaseMeasurementEnvelope", "VerifyReleaseMeasurementEnvelope", "SealReleaseMeasurementEnvelope"} {
			if calls[legacy] {
				t.Errorf("compact boundary %s acquired legacy fallback %s", caller, legacy)
			}
		}
	}
}

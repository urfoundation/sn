package main

// Pins producer, terminal capture and public replay reachability independently
// of the selected census, so regenerating a weakened list is not sufficient.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Requires one named call inside the actual production function body, not a
// comment or unrelated helper that merely mentions the same verifier.
func releaseClosureFunctionCalls(t *testing.T, path, name string) map[string]bool {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != name || function.Body == nil {
			continue
		}
		calls := map[string]bool{}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				switch target := call.Fun.(type) {
				case *ast.Ident:
					calls[target.Name] = true
				case *ast.SelectorExpr:
					calls[target.Sel.Name] = true
				}
			}
			return true
		})
		return calls
	}
	t.Fatalf("production closure function %s is absent from %s", name, path)
	return nil
}

// Both gate paths retain the production closure and all its adjacent tests.
func TestReleaseSemanticCensusPinsSettlementClosureRegressions(t *testing.T) {
	producerBytes, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	producer := string(producerBytes)
	selector, err := releaseConnectPolicySelectorAssignment(producer, "producer_tests")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../validator/attempt_closure_test.go", "../validator/release_settlement_test.go", "../validator/attempt_settlement_admission_test.go", "../validator/release_steer_cut_wait_test.go", "../validator/attempt_verification_test.go", "../validator/attempt_verification_cache_test.go", "../validator/attempt_cut_boundary_test.go"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyReleaseSourceTestCoverage(selector, "^Test", []string{string(source)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"TestFinalCollectorIncludesCompletedSettlementTail", "TestFinalSettlementClosureLastWindowNeedsNoNextIntent", "TestFinalSettlementClosureRejectsRehashedProofOmission", "TestFinalSettlementClosureRejectsDomainCensusAndBoundaryChanges", "TestFinalSettlementClosureProofProjectionUsesExactEpochBounds", "TestFinalSettlementClosureWaitHonorsPublicationAndCancellation", "TestFinalSettlementClosureWaitAuthenticatesLastWindow", "TestFinalSettlementClosureCollectedGraphRejectsAttemptOmission", "TestFinalSettlementClosureRejectsConflictingSuccessorTransition", "TestFinalSemanticFixtureTerminalTransitionMatchesSuccessorMeasurement"} {
		if selected, err := releaseSelectedTestDeclarations(releaseSemanticIntegritySelector, []string{"func " + name + "(t *testing.T) {}\n"}); err != nil || len(selected) != 1 {
			t.Fatalf("semantic selector omits %s: %v", name, err)
		}
	}
	aggregateBytes, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"settlement_closure_tests='^Test(Attempt(Settlement|Cut|Assignment)|ReleaseSettlementRefresh|ReleaseSteeringLoop)'", "go test ./validator -run \"$settlement_closure_tests\" -count=1", "go test -race ./validator -run \"$settlement_closure_tests\" -count=1"} {
		if !strings.Contains(string(aggregateBytes), command) {
			t.Fatalf("aggregate gate omits %s", command)
		}
	}
	for _, check := range []struct{ path, function, callee string }{
		{path: "../validator/attempt_settlement.go", function: "advanceAttemptSettlementEpochWithIOMode", callee: "publishAttemptSettlementClosure"},
		{path: "../validator/attempt_settlement.go", function: "recoverAttemptSettlementEpochWithRemove", callee: "publishAttemptSettlementClosure"},
		{path: "../validator/release_run.go", function: "RunRelease", callee: "runReleaseSettlementRefresh"},
		{path: "../validator/release_run.go", function: "RunRelease", callee: "Wait"},
		{path: "../validator/trail.go", function: "RunTrail", callee: "beginAttempt"},
		{path: "../validator/trail.go", function: "RunTrail", callee: "captureAttemptAssignment"},
		{path: "scenario.go", function: "runScenarioWithProbe", callee: "waitClosures"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputs", callee: "ReadAttemptSettlementClosure"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputs", callee: "collectFinalSettlementClosure"},
		{path: "final_semantic_settlement_closure.go", function: "collectFinalSettlementClosure", callee: "DecodeAttemptSettlementClosureWithServerKeys"},
		{path: "../validator/attempt_closure.go", function: "DecodeAttemptSettlementClosureWithServerKeys", callee: "decodeAttemptSettlementClosureWithServerKeysAndVerifier"},
		{path: "../validator/attempt_closure.go", function: "decodeAttemptSettlementClosureWithServerKeysAndVerifier", callee: "decodeAttemptSettlementClosureWithCutVerifier"},
		{path: "../validator/attempt_ledger.go", function: "verifyAttemptLedgerCutWithAssignVerifier", callee: "verifyAttemptRecordWithAssignVerifier"},
		{path: "../validator/attempt_ledger.go", function: "BuildCut", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/attempt_ledger.go", function: "verifyAttemptLedgerCutWithAssignVerifier", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/release_measurement.go", function: "releaseMeasurementStats", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/release_measurement.go", function: "releaseAttemptCutExtends", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/release_measurement.go", function: "loadOrDetachReleaseMeasurementInput", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/release_steer.go", function: "takeHeadEvidence", callee: "releaseBlockAtOrBefore"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputs", callee: "verifyFinalMeasurementSettlementClosures"},
		{path: "final_semantic_collect.go", function: "verifyFinalCollectedClosedGraph", callee: "verifyFinalCollectedSettlementAuthority"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalSettlementClosureArtifacts", callee: "verifyFinalMeasurementSettlementClosures"},
		{path: "final_semantic_evidence.go", function: "VerifyFinalSemanticArtifacts", callee: "verifyFinalSettlementClosureArtifacts"},
	} {
		if !releaseClosureFunctionCalls(t, check.path, check.function)[check.callee] {
			t.Errorf("%s omits actual call %s", check.function, check.callee)
		}
	}
	scenarioBytes, err := os.ReadFile("scenario.go")
	if err != nil {
		t.Fatal(err)
	}
	scenario := string(scenarioBytes)
	wait := strings.Index(scenario, "waitClosures(ctx, cfg, stateDir, current, window, deadline, options.PollInterval)")
	collect := strings.Index(scenario, "collect(ctx, cfg, stateDir, runDir, result, current, observationHistory)")
	if wait < 0 || collect <= wait || !strings.Contains(scenario, "waitClosures = waitFinalValidatorSettlementClosures") {
		t.Fatal("live capture lost its required existing-deadline terminal closure handoff")
	}
}

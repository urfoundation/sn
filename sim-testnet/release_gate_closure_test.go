package main

// Pins producer, terminal capture and public replay reachability independently
// of the selected census, so regenerating a weakened list is not sufficient.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
	if err := verifyReleaseGateSettlementClosureOwners(string(aggregateBytes)); err != nil {
		t.Fatalf("aggregate settlement closure ownership: %v", err)
	}
	for _, check := range []struct{ path, function, callee string }{
		{path: "../validator/attempt_settlement.go", function: "advanceAttemptSettlementEpochWithIOMode", callee: "advanceAttemptSettlementEpochWithIOModeContext"},
		{path: "../validator/attempt_settlement.go", function: "advanceAttemptSettlementEpochWithIOModeContext", callee: "advanceAttemptSettlementCandidatesOwned"},
		{path: "../validator/attempt_settlement.go", function: "advanceAttemptSettlementCandidatesOwned", callee: "publishAttemptSettlementClosure"},
		{path: "../validator/attempt_settlement.go", function: "RecoverAttemptSettlementEpoch", callee: "recoverAttemptSettlementEpochOwned"},
		{path: "../validator/attempt_settlement.go", function: "recoverAttemptSettlementEpochWithRemove", callee: "recoverAttemptSettlementEpochOwned"},
		{path: "../validator/attempt_settlement_legacy_recovery_owned.go", function: "recoverAttemptSettlementEpochOwned", callee: "attemptSettlementClosureFromTransaction"},
		{path: "../validator/attempt_settlement_legacy_recovery_owned.go", function: "recoverAttemptSettlementEpochOwned", callee: "publishAttemptSettlementOwnedClosure"},
		{path: "../validator/release_run.go", function: "RunRelease", callee: "runReleaseWithActivationSetup"},
		{path: "../validator/release_run.go", function: "runReleaseWithActivationSetup", callee: "runReleaseWithStartupV2"},
		{path: "../validator/release_run.go", function: "runReleaseWithStartupV2", callee: "newReleaseRuntimeV2"},
		{path: "../validator/release_runtime_v2.go", function: "newReleaseRuntimeV2", callee: "newReleaseRuntimeV2WithRuntime"},
		{path: "../validator/release_runtime_v2.go", function: "newReleaseRuntimeV2WithRuntime", callee: "startReleaseEvidenceV2DiskStateOwned"},
		{path: "../validator/release_startup_v2.go", function: "startReleaseEvidenceV2DiskStateOwned", callee: "recoverAttemptSettlementEpochV2"},
		{path: "../validator/release_startup_v2.go", function: "startReleaseEvidenceV2DiskStateOwned", callee: "initializeAttemptSettlementEpochV2"},
		{path: "../validator/release_run.go", function: "runReleaseWithStartupV2", callee: "runReleaseSettlementRefresh"},
		{path: "../validator/release_run.go", function: "runReleaseWithStartupV2", callee: "runReleaseOperatorWorkers"},
		{path: "../validator/release_shutdown.go", function: "runReleaseOperatorWorkers", callee: "Wait"},
		{path: "../validator/trail.go", function: "RunTrail", callee: "beginAttempt"},
		{path: "../validator/trail.go", function: "RunTrail", callee: "captureAttemptAssignment"},
		{path: "scenario.go", function: "runScenarioWithProbe", callee: "waitClosures"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputs", callee: "collectFinalValidatorInputsWithSeedObserver"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputsWithSeedObserver", callee: "collectFinalValidatorInputsWithPathAuthority"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputsWithPathAuthority", callee: "ReadAttemptSettlementClosure"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputsWithPathAuthority", callee: "collectFinalSettlementClosure"},
		{path: "final_semantic_settlement_closure.go", function: "collectFinalSettlementClosure", callee: "DecodeAttemptSettlementClosureWithServerKeys"},
		{path: "../validator/attempt_closure.go", function: "DecodeAttemptSettlementClosureWithServerKeys", callee: "decodeAttemptSettlementClosureWithServerKeysAndVerifier"},
		{path: "../validator/attempt_closure.go", function: "decodeAttemptSettlementClosureWithServerKeysAndVerifier", callee: "decodeAttemptSettlementClosureWithCutVerifier"},
		{path: "../validator/attempt_ledger.go", function: "verifyAttemptLedgerCutWithAssignVerifier", callee: "verifyAttemptLedgerCutWithComparison"},
		{path: "../validator/attempt_ledger.go", function: "verifyAttemptLedgerCutWithComparison", callee: "verifyAttemptRecordWithAssignVerifier"},
		{path: "../validator/attempt_ledger.go", function: "BuildCut", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/attempt_ledger.go", function: "verifyAttemptLedgerCutWithComparison", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/release_measurement.go", function: "releaseMeasurementStats", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/release_measurement.go", function: "releaseAttemptCutExtends", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/release_measurement.go", function: "loadOrDetachReleaseMeasurementInput", callee: "releaseBlockAtOrBefore"},
		{path: "../validator/release_steer.go", function: "takeHeadEvidence", callee: "releaseBlockAtOrBefore"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputsWithPathAuthority", callee: "verifyFinalMeasurementSettlementClosures"},
		{path: "final_semantic_collect.go", function: "verifyFinalCollectedClosedGraph", callee: "verifyFinalCollectedSettlementAuthorityWithReader"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalSettlementClosureArtifacts", callee: "verifyFinalSettlementClosureArtifactsWithLineage"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalSettlementClosureArtifactsWithLineage", callee: "verifyFinalSettlementClosureArtifactsWithAuthority"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalSettlementClosureArtifactsWithAuthority", callee: "verifyFinalMeasurementSettlementClosures"},
		{path: "final_semantic_evidence.go", function: "VerifyFinalSemanticArtifacts", callee: "verifyFinalSettlementClosureArtifactsWithAuthority"},
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

// The original focused family is still part of both complete package owners;
// its own duplicate implicit-deadline pair is not a separate certificate.
const releaseSettlementClosureOwnerSelector = "^Test(Attempt(Settlement|Cut|Assignment)|ReleaseSettlementRefresh|ReleaseSteeringLoop)"

// Require complete uncached normal/race owners before delegating any focused
// root, and reject reinstating the redundant shorter aggregate pair.
func verifyReleaseGateSettlementClosureOwners(script string) error {
	if err := verifyReleaseGateFullValidatorRace(script); err != nil {
		return err
	}
	if strings.Contains(script, "settlement_closure_tests=") || strings.Contains(script, "$settlement_closure_tests") {
		return fmt.Errorf("settlement closure duplicated its complete normal/race owners under a focused package deadline")
	}
	return nil
}

// Independently derive the original family from every current validator test
// source, then prove the producer and complete aggregate owners retain it.
func TestReleaseSemanticCensusRequiresCompleteSettlementClosureOwners(t *testing.T) {
	t.Parallel()
	aggregateRaw, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseGateSettlementClosureOwners(string(aggregateRaw)); err != nil {
		t.Fatal(err)
	}
	producerRaw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	producerSelector, err := releaseConnectPolicySelectorAssignment(string(producerRaw), "producer_tests")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob("../validator/*_test.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("validator source census: %v", err)
	}
	var sources []string
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, string(raw))
	}
	for _, owner := range []string{"^Test", producerSelector} {
		if err := verifyReleaseSourceTestCoverage(owner, releaseSettlementClosureOwnerSelector, sources); err != nil {
			t.Fatalf("settlement closure lost complete source coverage: %v", err)
		}
	}
}

// A full command must really execute in each mode; an old focused duplicate,
// hidden registry, omitted body, cache hit or selected subset cannot replace it.
func TestReleaseSemanticCensusRejectsIncompleteSettlementClosureOwners(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if err := verifyReleaseGateSettlementClosureOwners(script); err != nil {
		t.Fatal(err)
	}
	const oldPair = "settlement_closure_tests='" + releaseSettlementClosureOwnerSelector + "'\ngo test ./validator -run \"$settlement_closure_tests\" -count=1\ngo test -race ./validator -run \"$settlement_closure_tests\" -count=1\n"
	if err := verifyReleaseGateSettlementClosureOwners(script + "\n" + oldPair); err == nil {
		t.Fatal("aggregate accepted the original redundant implicit-deadline closure pair")
	}
	for _, command := range []string{
		"go test -parallel=4 -timeout 90m ./... -count=1",
		"go test -race -parallel=4 -timeout 90m ./validator -count=1",
	} {
		if strings.Count(script, command) != 1 {
			t.Fatal("full closure owner is not unique", command)
		}
		for _, replacement := range []string{
			"# " + command,
			command + " -run '^TestAttemptSettlement$'",
			command + " -skip '^TestAttempt'",
			command + " -run '^$'",
			strings.Replace(command, " -count=1", "", 1),
			"if false; then\n" + command + "\nfi",
		} {
			if err := verifyReleaseGateSettlementClosureOwners(strings.Replace(script, command, replacement, 1)); err == nil {
				t.Fatal("aggregate accepted incomplete closure owner", replacement)
			}
		}
	}
	const start = "release_gate_start sn-validator-race release_phase_sn_validator_race"
	for _, replacement := range []string{
		"# " + start,
		"if false; then\n" + start + "\nfi",
		"release_unused_owner() {\n" + start + "\n}",
	} {
		if err := verifyReleaseGateSettlementClosureOwners(strings.Replace(script, start, replacement, 1)); err == nil {
			t.Fatal("aggregate accepted disconnected closure owner", replacement)
		}
	}
}

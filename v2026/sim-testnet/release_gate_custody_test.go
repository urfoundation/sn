package main

// The pre-launch gate must exercise every production seed-loading boundary,
// its shared descriptor-backed implementation, and these selection checks.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"testing"
)

// One source census serves producer and aggregate selection checks.
func releaseCustodyRegressionPaths() []string {
	return []string{
		"../crv4/keys_test.go",
		"../crv4/keys_custody_test.go",
		"../crv4/seed_file_test.go",
		"../crv4/seed_file_read_test.go",
		"../validator/identity_test.go",
		"../validator/identity_custody_test.go",
		"../validator/identity_custody_adjacent_test.go",
		"../validator/release_client_seed_custody_test.go",
		"../validator/release_client_seed_continuity_test.go",
		"../validator/release_client_seed_identity_test.go",
		"../validator/release_client_seed_adjacent_test.go",
	}
}

// Both gates retain the real provisioner-key regressions and every new public
// authority/atomic-publication root, not merely the older raw-read boundaries.
func TestProducerGateCustodySelectionCoversOperatorPathIdentity(t *testing.T) {
	var sources []string
	for _, path := range []string{"simulator_operator_path_identity_test.go", "simulator_operator_path_authority_test.go"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, string(raw))
	}
	for _, path := range []string{"../scripts/test-release-1.0-producer-gate.sh", "../scripts/test-release-1.0-local.sh"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		selector, err := releaseConnectPolicySelectorAssignment(string(raw), "simulator_seed_custody_tests")
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyReleaseSourceTestCoverage(selector, "^TestSimulatorOperatorPath", sources); err != nil {
			t.Fatalf("%s operator path census: %v", path, err)
		}
	}
}

// All real producers and replay routes must reach independent expected-role
// admission, per-operator crypto and a complete staged publication boundary.
func TestProducerGateCustodySelectionCoversOperatorPathCallEdges(t *testing.T) {
	for _, check := range []struct{ path, function, callee string }{
		{path: "final_semantic_path_identity.go", function: "loadFinalOperatorPathAuthority", callee: "finalCollectedFileEntry"},
		{path: "final_semantic_path_identity.go", function: "loadFinalOperatorPathAuthority", callee: "decodeFinalOperatorPathAuthority"},
		{path: "final_semantic_path_identity.go", function: "loadFinalOperatorPathAuthority", callee: "LoadRawSeedFile"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputsWithPathAuthority", callee: "collectFinalAttemptCuts"},
		{path: "final_semantic_collect.go", function: "collectFinalAttemptCuts", callee: "VerifyAttemptLedgerCut"},
		{path: "final_semantic_collect.go", function: "collectFinalAttemptCuts", callee: "mergeFinalAttemptCutsAtomically"},
		{path: "final_semantic_collect.go", function: "verifyFinalCollectedClosedGraph", callee: "validateFinalArtifactLocatorReuse"},
		{path: "final_semantic_settlement_closure.go", function: "collectFinalSettlementClosure", callee: "mergeFinalAttemptCutsAtomically"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalCollectedSettlementAuthority", callee: "finalCollectedPublicIdentityBytes"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalCollectedSettlementAuthority", callee: "decodeFinalOperatorPathAuthority"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalCollectedSettlementAuthority", callee: "verify"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalSettlementClosureArtifacts", callee: "decodeFinalFleetLifecycleLineageFiles"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalSettlementClosureArtifactsWithLineage", callee: "decodeFinalOperatorPathAuthority"},
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalSettlementClosureArtifactsWithAuthority", callee: "verify"},
		{path: "final_semantic_evidence.go", function: "VerifyFinalSemanticArtifacts", callee: "decodeFinalFleetLifecycleLineageFiles"},
		{path: "final_semantic_evidence.go", function: "VerifyFinalSemanticArtifacts", callee: "decodeFinalOperatorPathAuthority"},
		{path: "final_semantic_evidence.go", function: "VerifyFinalSemanticArtifacts", callee: "verifyFinalFleetLifecycleArtifactsWithIdentities"},
		{path: "final_semantic_evidence.go", function: "verifyFinalPathProofArtifactBound", callee: "finalOperatorPathKeys"},
		{path: "final_semantic_evidence.go", function: "verifyFinalPathProofArtifactBound", callee: "VerifyProofRecord"},
		{path: "final_semantic_source.go", function: "buildValidators", callee: "decodeFinalOperatorPathAuthority"},
		{path: "final_semantic_source.go", function: "buildValidators", callee: "verify"},
		{path: "final_semantic_lifecycle.go", function: "verifyFinalFleetLifecycleArtifacts", callee: "decodeFinalFleetLifecycleLineageFiles"},
		{path: "final_semantic_lifecycle.go", function: "verifyFinalFleetLifecycleArtifactsWithFiles", callee: "decodeStrictJSONBytes"},
	} {
		if !releaseClosureFunctionCalls(t, check.path, check.function)[check.callee] {
			t.Errorf("%s omits operator path authority edge %s", check.function, check.callee)
		}
	}
	// The full entry point decodes one checked lineage/public document; its
	// owned-authority consumers may not route back through standalone decoders.
	parsed, err := parser.ParseFile(token.NewFileSet(), "final_semantic_evidence.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{"decodeFinalFleetLifecycleLineageFiles": 0, "decodeFinalOperatorPathAuthority": 0}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "VerifyFinalSemanticArtifacts" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name, ok := call.Fun.(*ast.Ident); ok {
				if _, tracked := counts[name.Name]; tracked {
					counts[name.Name]++
				}
			}
			return true
		})
	}
	for name, count := range counts {
		if count != 1 {
			t.Errorf("full replay calls %s %d times, want one owned decode", name, count)
		}
	}
	for _, owner := range []struct{ path, function string }{
		{path: "final_semantic_settlement_closure.go", function: "verifyFinalSettlementClosureArtifactsWithAuthority"},
		{path: "final_semantic_lifecycle.go", function: "verifyFinalFleetLifecycleArtifactsWithIdentities"},
	} {
		calls := releaseClosureFunctionCalls(t, owner.path, owner.function)
		for _, repeated := range []string{"decodeFinalFleetLifecycleLineageFiles", "decodeFinalOperatorPathAuthority", "verifyFinalFleetLifecycleArtifacts", "verifyFinalSettlementClosureArtifacts"} {
			if calls[repeated] {
				t.Errorf("owned consumer %s redundantly routes through %s", owner.function, repeated)
			}
		}
	}
}

// Both gates select every raw-reader regression and retain the three original
// operator-domain, publication/cancellation and independent-clock controls.
func TestProducerGateCustodySelectionCoversSimulatorReaders(t *testing.T) {
	raw, err := os.ReadFile("simulator_client_seed_custody_test.go")
	if err != nil {
		t.Fatal(err)
	}
	requiredRoots, err := releaseSelectedTestDeclarations("^Test", []string{string(raw)})
	if err != nil || len(requiredRoots) == 0 {
		t.Fatalf("raw custody source census: %v", err)
	}
	for _, control := range []struct{ path, name string }{
		{path: "scenario_test.go", name: "TestInspectValidatorPathProofsRequiresEveryOperatorDomain"},
		{path: "final_semantic_settlement_closure_test.go", name: "TestFinalSettlementClosureWaitHonorsPublicationAndCancellation"},
		{path: "final_semantic_collect_test.go", name: "TestFinalLifecycleIntentRequirementsKeepSettlementAndNativeClocksDistinct"},
	} {
		source, err := os.ReadFile(control.path)
		if err != nil {
			t.Fatal(err)
		}
		declared, err := releaseSelectedTestDeclarations("^"+regexp.QuoteMeta(control.name)+"$", []string{string(source)})
		if err != nil || len(declared) != 1 {
			t.Fatalf("original raw custody control %s is absent: %v", control.name, err)
		}
		requiredRoots = append(requiredRoots, control.name)
	}
	for _, path := range []string{"../scripts/test-release-1.0-producer-gate.sh", "../scripts/test-release-1.0-local.sh"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		script := string(raw)
		selector, err := releaseConnectPolicySelectorAssignment(script, "simulator_seed_custody_tests")
		if err != nil {
			t.Fatal(err)
		}
		selected, err := regexp.Compile(selector)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range requiredRoots {
			if !selected.MatchString(name) {
				t.Errorf("%s omits raw custody source regression %s", path, name)
			}
		}
		for _, prefix := range []string{"go test ", "go test -race "} {
			command := prefix + "./sim-testnet -run \"$simulator_seed_custody_tests\" -count=1"
			if !regexp.MustCompile("(?m)^[\\t ]*" + regexp.QuoteMeta(command) + "(?:[\\t ]+[^\\n]*)?$").MatchString(script) {
				t.Errorf("%s omits actual raw custody invocation %s", path, command)
			}
		}
	}
}

// Require real readers and default live dispatch through the shared raw-only
// loader, preserving signature/closure verification after key admission.
func TestProducerGateCustodySelectionCoversSimulatorCallEdges(t *testing.T) {
	for _, check := range []struct{ path, function, callee string }{
		{path: "scenario.go", function: "RunScenario", callee: "runScenarioCampaignAttempt"},
		{path: "scenario.go", function: "runScenarioCampaignAttempt", callee: "runScenarioWithProbe"},
		{path: "scenario.go", function: "Snapshot", callee: "inspectValidatorPathProofs"},
		{path: "scenario.go", function: "inspectValidatorPathProofs", callee: "loadFinalOperatorPathAuthority"},
		{path: "scenario.go", function: "inspectValidatorPathProofs", callee: "VerifyProofRecord"},
		{path: "scenario.go", function: "runScenarioWithProbe", callee: "waitClosures"},
		{path: "scenario.go", function: "runScenarioWithProbe", callee: "collect"},
		{path: "final_semantic_collect.go", function: "CollectFinalSemanticInputs", callee: "collectFinalValidatorInputsWithPathAuthority"},
		{path: "final_semantic_collect.go", function: "CollectFinalSemanticInputs", callee: "loadFinalOperatorPathAuthority"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputs", callee: "collectFinalValidatorInputsWithSeedObserver"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputsWithSeedObserver", callee: "loadFinalOperatorPathAuthority"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputsWithSeedObserver", callee: "collectFinalValidatorInputsWithPathAuthority"},
		{path: "final_semantic_collect.go", function: "collectFinalValidatorInputsWithPathAuthority", callee: "collectFinalSettlementClosure"},
		{path: "final_semantic_settlement_closure.go", function: "waitFinalValidatorSettlementClosures", callee: "waitFinalValidatorSettlementClosuresWithWait"},
		{path: "final_semantic_settlement_closure.go", function: "waitFinalValidatorSettlementClosuresWithWait", callee: "loadFinalOperatorPathAuthority"},
		{path: "final_semantic_settlement_closure.go", function: "waitFinalValidatorSettlementClosuresWithWait", callee: "collectFinalSettlementClosure"},
		{path: "final_semantic_path_identity.go", function: "loadFinalOperatorPathAuthority", callee: "LoadRawSeedFile"},
		{path: "../crv4/seed_file_read.go", function: "LoadRawSeedFile", callee: "loadSeedFileParsed"},
	} {
		if !releaseClosureFunctionCalls(t, check.path, check.function)[check.callee] {
			t.Errorf("%s omits actual simulator custody call %s", check.function, check.callee)
		}
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "scenario.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	defaults := map[string]string{
		"waitClosures": "waitFinalValidatorSettlementClosures",
		"collect":      "CollectFinalSemanticInputs",
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "runScenarioWithProbe" || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			branch, ok := node.(*ast.IfStmt)
			if !ok {
				return true
			}
			condition, ok := branch.Cond.(*ast.BinaryExpr)
			if !ok || condition.Op != token.EQL {
				return true
			}
			target, targetOK := condition.X.(*ast.Ident)
			absent, absentOK := condition.Y.(*ast.Ident)
			if !targetOK || !absentOK || absent.Name != "nil" {
				return true
			}
			want, required := defaults[target.Name]
			if !required {
				return true
			}
			for _, statement := range branch.Body.List {
				assignment, ok := statement.(*ast.AssignStmt)
				if !ok || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
					continue
				}
				left, leftOK := assignment.Lhs[0].(*ast.Ident)
				right, rightOK := assignment.Rhs[0].(*ast.Ident)
				if leftOK && rightOK && left.Name == target.Name && right.Name == want {
					delete(defaults, target.Name)
				}
			}
			return true
		})
	}
	if len(defaults) != 0 {
		t.Fatalf("live simulator omits actual nil-default custody dispatch: %v", defaults)
	}
}

// Select the complete reviewed source groups, including preserved format,
// key-vector, identity reload and mirror-address controls.
func TestProducerGateCustodySelectionCoversBothIdentityEntryPoints(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "seed_custody_tests", "./crv4 ./validator", "seed-and-validator-custody", releaseCustodyRegressionPaths())
}

// A guard omitted from its own ordinary/race gate cannot protect deployment.
func TestProducerGateCustodySelectionCoversItsOwnChecks(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "custody-selection", []string{
		"release_gate_custody_test.go",
	})
}

// The aggregate gate must explicitly retain every custody root in both modes,
// independently of its later full-package suites and the producer selection.
func TestProducerGateCustodySelectionCoversAggregate(t *testing.T) {
	raw, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	selector, err := releaseConnectPolicySelectorAssignment(script, "seed_custody_tests")
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, path := range releaseCustodyRegressionPaths() {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, string(raw))
	}
	if err := verifyReleaseSourceTestCoverage(selector, "^Test", sources); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"go test ", "go test -race "} {
		command := prefix + "./crv4 ./validator -run \"$seed_custody_tests\" -count=1"
		if !regexp.MustCompile("(?m)^[\\t ]*" + regexp.QuoteMeta(command) + "(?:[\\t ]+[^\\n]*)?$").MatchString(script) {
			t.Fatalf("aggregate gate omits actual custody invocation %s", command)
		}
	}
}

// Pin actual release call edges through the same tested reader and admission,
// not merely names in comments or a helper unused by RunRelease.
func TestProducerGateCustodySelectionCoversReleaseStartup(t *testing.T) {
	for _, check := range []struct{ path, function, callee string }{
		{path: "../validator/release_run.go", function: "RunRelease", callee: "loadReleaseAttemptState"},
		{path: "../validator/release_run.go", function: "RunRelease", callee: "startReleaseOperator"},
		{path: "../validator/release_run.go", function: "loadReleaseAttemptState", callee: "loadReleaseAttemptStateWithObserver"},
		{path: "../validator/release_run.go", function: "loadReleaseAttemptStateWithObserver", callee: "loadClientSeed"},
		{path: "../validator/release_run.go", function: "loadReleaseAttemptStateWithObserver", callee: "NewAttemptLedger"},
		{path: "../validator/release_run.go", function: "loadReleaseAttemptStateWithObserver", callee: "AttachAttemptLedger"},
		{path: "../validator/release_run.go", function: "loadClientSeed", callee: "LoadRawOrBareHexSeedFile"},
		{path: "../validator/release_run.go", function: "startReleaseOperator", callee: "startReleaseOperatorWithAdmission"},
		{path: "../validator/release_run.go", function: "startReleaseOperatorWithAdmission", callee: "loadClientSeed"},
		{path: "../validator/release_run.go", function: "startReleaseOperatorWithAdmission", callee: "validateAttemptLedgerIdentity"},
		{path: "../validator/release_run.go", function: "startReleaseOperatorWithAdmission", callee: "NewTrailEngine"},
		{path: "../crv4/seed_file_read.go", function: "LoadRawOrBareHexSeedFile", callee: "loadSeedFileParsed"},
		{path: "../crv4/seed_file_read.go", function: "LoadRawSeedFile", callee: "loadSeedFileParsed"},
		{path: "../crv4/seed_file_read.go", function: "loadSeedFileParsed", callee: "openSeedFileDirectory"},
		{path: "../crv4/seed_file_read.go", function: "loadSeedFileParsed", callee: "readWithParser"},
	} {
		if !releaseClosureFunctionCalls(t, check.path, check.function)[check.callee] {
			t.Errorf("%s omits actual custody call %s", check.function, check.callee)
		}
	}
}

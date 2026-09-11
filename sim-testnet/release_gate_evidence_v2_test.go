package main

// Evidence launch qualification includes the real bootstrap, current-history,
// contract wire and installer boundaries. A source file or an offline aggregate
// pass cannot substitute for both executable modes in an admitted gate phase.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Independent reviewed alternatives retain every preexisting producer family.
// New adjacent roots are selected by prefix, not by a frozen hand-picked list.
const releaseEvidenceV2OldProducerGroups = "Attempt|DiskAttempt|HTTPAttemptStreamV2|SealAttemptCutV2|TrailPolicyDepth|StatsWrite|StatsSnapshotWrite|StatsMultiBatch|StatsSettlement|Deposited|ReleaseMeasurement|ReleaseStatsV2Runtime|ReleaseHeadV2|IntentStore|SteeringIntent|MeasurementStats|ExactPoolQuality|HeadEMA|ReleaseSteeringLoop|ReleaseSettlementRefresh"
const releaseEvidenceV2NewProducerGroups = "ReleaseEvidenceV2|ReleaseActivationV2|ReleaseBootstrapV2|ReleaseActivationHistoryV2|ReleaseInitialBoundaryV2|ReleaseStartupV2|ValidatorEvidenceCensusV2|ChainEvidence|RunRelease|ValidatorEvidence|ValidatorUpload|ReleaseClientKeyHistory|ReleaseClientKeyAuthority|ReleaseRuntimeV2|ArtifactHttpObservation|NativeSourceHash|IntentV2|ReleaseNativeCaptureV2|ReleaseCaptureV2"

type releaseEvidenceV2GateGroup struct {
	phase            string
	job              string
	variable         string
	alternatives     string
	requiredSelector string
	packages         []string
	commands         []string
	sources          map[string][]string
}

// The group is fixed before inspecting candidate shell text. Source census is
// package-qualified, so two packages may legitimately declare the same root.
func verifyReleaseEvidenceV2GateGroup(script string, group releaseEvidenceV2GateGroup) error {
	function := "release_phase_" + group.phase
	pattern := regexp.MustCompile("(?ms)^[\\t ]*" + regexp.QuoteMeta(function) + "\\(\\) \\{\\n(.*?)^[\\t ]*\\}[\\t ]*$")
	functions := pattern.FindAllStringSubmatch(script, -1)
	if len(functions) != 1 {
		return fmt.Errorf("evidence gate has %d %s phase definitions", len(functions), function)
	}
	job := group.job
	if job == "" {
		job = group.phase
	}
	start := "release_gate_start " + job + " " + function
	invocations := regexp.MustCompile("(?m)^[\\t ]*" + regexp.QuoteMeta(start) + "[\\t ]*$")
	if len(invocations.FindAllString(script, -1)) != 1 {
		return fmt.Errorf("evidence gate does not admit exactly one %s phase", group.phase)
	}
	body := functions[0][1]
	selector, err := releaseConnectPolicySelectorAssignment(body, group.variable)
	if err != nil {
		return err
	}
	global, err := releaseConnectPolicySelectorAssignment(script, group.variable)
	if err != nil {
		return err
	}
	if global != selector {
		return fmt.Errorf("evidence gate selector ownership differs")
	}
	if group.alternatives != "" {
		if !strings.HasPrefix(selector, "^Test(") || !strings.HasSuffix(selector, ")") {
			return fmt.Errorf("evidence gate %s narrowed its prefix boundary", group.variable)
		}
		counts := map[string]int{}
		for _, alternative := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(selector, "^Test("), ")"), "|") {
			counts[alternative]++
			if alternative == "" || counts[alternative] != 1 {
				return fmt.Errorf("evidence gate repeats or empties an alternative")
			}
		}
		for _, required := range strings.Split(group.alternatives, "|") {
			if counts[required] != 1 {
				return fmt.Errorf("evidence gate %s omits required family %s", group.variable, required)
			}
		}
	}
	if len(group.sources) != len(group.packages) || len(group.packages) == 0 {
		return fmt.Errorf("evidence gate source package census differs")
	}
	requiredSelector := group.requiredSelector
	if requiredSelector == "" {
		requiredSelector = "^Test"
	}
	for _, packagePath := range group.packages {
		if err := verifyReleaseSourceTestCoverage(selector, requiredSelector, group.sources[packagePath]); err != nil {
			return fmt.Errorf("evidence gate %s source census: %w", packagePath, err)
		}
	}
	for _, command := range group.commands {
		invocation := regexp.MustCompile("(?m)^[\\t ]*" + regexp.QuoteMeta(command) + "[\\t ]*$")
		if len(invocation.FindAllString(body, -1)) != 1 {
			return fmt.Errorf("evidence gate phase %s does not execute exactly one %s", group.phase, command)
		}
	}
	return nil
}

// Reviewed filename groups also protect a renamed root that escapes its old
// family prefix. Every matched file is read; no candidate selector filters the
// required source census. Fixture-only files do not invent test declarations.
func releaseEvidenceV2GateSources(t *testing.T, patterns []string) []string {
	t.Helper()
	seen := map[string]bool{}
	var sources []string
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil || len(paths) == 0 {
			t.Fatalf("evidence source group %s is absent: %v", pattern, err)
		}
		for _, path := range paths {
			if seen[path] {
				continue
			}
			seen[path] = true
			encoded, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			sources = append(sources, string(encoded))
		}
	}
	return sources
}

// This reads actual repository source only. It never launches a gate, service,
// test subprocess, Solidity compiler, network client or deployment fixture.
func releaseEvidenceV2GateFixture(t *testing.T) (string, []releaseEvidenceV2GateGroup) {
	t.Helper()
	encoded, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	validator := releaseEvidenceV2GateSources(t, []string{
		"../validator/config_evidence_v2*_test.go", "../validator/chain_evidence*_test.go",
		"../validator/release_activation_v2*_test.go", "../validator/release_activation_history_v2*_test.go",
		"../validator/release_bootstrap_v2*_test.go", "../validator/release_initial_boundary_v2*_test.go",
		"../validator/release_state_v2*_test.go", "../validator/release_startup*_test.go",
		"../validator/release_proof_state_v2*_test.go", "../validator/release_evidence*_test.go",
		"../validator/release_client_key_history*_test.go", "../validator/validator_upload*_test.go",
		"../validator/release_runtime_v2*_test.go", "../validator/release_artifact_observation_v2*_test.go", "../validator/intent_v2*_test.go",
		"../validator/release_capture_native_v2_test.go", "../validator/release_capture_v2_test.go",
		"../validator/release_capture_budget_v2_test.go",
	})
	groups := []releaseEvidenceV2GateGroup{
		{phase: "settlement", variable: "producer_tests", alternatives: releaseEvidenceV2OldProducerGroups + "|" + releaseEvidenceV2NewProducerGroups, packages: []string{"./validator"}, sources: map[string][]string{"./validator": validator}, commands: []string{
			`go test ./validator -run "$producer_tests" -count=1 -parallel=4 -timeout 90m`,
			`go test -race ./validator -run "$producer_tests" -count=1 -parallel=4 -timeout 90m`,
		}},
		{phase: "solidity", variable: "validator_evidence_tests", alternatives: "ValidatorEvidence|RuntimeEvidenceV2|RuntimeEvidence|EvidenceRelay|EvmTxManager|ClientKeyHistory", packages: []string{"./protocol", "./stabi", "./sim-testnet/gencontracts"}, sources: map[string][]string{
			"./protocol":                 releaseEvidenceV2GateSources(t, []string{"../protocol/validator_evidence*_test.go", "../protocol/client_key_history*_test.go"}),
			"./stabi":                    releaseEvidenceV2GateSources(t, []string{"../stabi/validator_evidence*_test.go"}),
			"./sim-testnet/gencontracts": releaseEvidenceV2GateSources(t, []string{"gencontracts/evidence*_test.go"}),
		}, commands: []string{
			`go test ./protocol ./stabi ./sim-testnet/gencontracts -run "$validator_evidence_tests" -count=1`,
			`go test -race ./protocol ./stabi ./sim-testnet/gencontracts -run "$validator_evidence_tests" -count=1`,
		}},
		{phase: "capture", variable: "capture_tests", packages: []string{"./sim-testnet"}, sources: map[string][]string{"./sim-testnet": releaseEvidenceV2GateSources(t, []string{"release_gate_evidence_v2_test.go", "release_gate_simulator_evidence_test.go", "release_gate_capture_metadata_test.go", "release_gate_history_population_test.go", "release_gate_launch_source_test.go", "final_semantic_capture_v2_test.go", "final_semantic_pending_prior_v2_test.go", "evidence_streaming_test.go", "final_semantic_capture_capacity_test.go", "final_semantic_capture_streaming_v2_test.go", "evidence_limits_v2_test.go", "evidence_readback_v2_test.go", "evidence_public_file_v2_test.go", "evidence_population_v2_test.go", "evidence_metadata_row_size_v2_test.go", "evidence_metadata_census_v2_test.go", "evidence_metadata_v2_test.go", "evidence_publication_batch_test.go", "final_semantic_prior_carrier_v2_test.go", "final_semantic_prior_carrier_decode_v2_test.go", "final_semantic_prior_carrier_canonical_v2_test.go"})}, commands: []string{
			`go test ./sim-testnet -run "$capture_tests" -count=1` + releaseGateCaptureOwnerSkip + ` -timeout 5m`,
			`go test -race ./sim-testnet -run "$capture_tests" -count=1` + releaseGateCaptureOwnerSkip + ` -timeout 10m`,
		}},
		{phase: "evidence_simulator", job: "evidence-simulator", variable: "simulator_evidence_tests", alternatives: "ValidatorEvidence|RuntimeEvidenceV2|RuntimeEvidence|EvidenceRelay|EvmTxManager|ClientKeyHistory", packages: []string{"./sim-testnet"}, sources: map[string][]string{
			"./sim-testnet": releaseEvidenceV2GateSources(t, []string{"evidence_deployment*_test.go", "evidence_carry*_test.go", "runtime_evidence*_test.go", "evidence_relay*_test.go", "evidence_relay_launch_budget_test.go", "evidence_relay_launch_runtime_test.go", "evm_nonce_turn_test.go", "adversary_client_key_batch_test.go"}),
		}, commands: []string{
			`go test ./sim-testnet -run "$simulator_evidence_tests" -count=1` + releaseGateSimulatorEvidenceOwnerSkip + ` -timeout 10m`,
			`go test -race ./sim-testnet -run "$simulator_evidence_tests" -count=1` + releaseGateSimulatorEvidenceOwnerSkip + ` -timeout 10m`,
		}},
	}
	return string(encoded), groups
}

// The original script omits these actual launch-critical source declarations.
func TestProducerGateStateSelectionCoversEvidenceV2ValidatorSources(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	if err := verifyReleaseEvidenceV2GateGroup(script, groups[0]); err != nil {
		t.Fatalf("producer evidence source gate is incomplete: %v", err)
	}
}

// Forge behavior and generated freshness are not Go signature/installer tests.
func TestProducerGateStateSelectionCoversEvidenceV2ContractAndInstallerSources(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	for _, index := range []int{1, 3} {
		if err := verifyReleaseEvidenceV2GateGroup(script, groups[index]); err != nil {
			t.Fatalf("companion Go source gate is incomplete: %v", err)
		}
	}
}

// A guard excluded from the gate cannot certify the gate that excluded it.
func TestProducerGateStateSelectionCoversEvidenceV2OwnGuardSources(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	if err := verifyReleaseEvidenceV2GateGroup(script, groups[2]); err != nil {
		t.Fatal(err)
	}
}

// Old alternatives survive independently, including adjacent StatsSnapshot,
// compact Stats runtime and head families absent from the older coarse guard.
func TestProducerGateStateSelectionEvidenceV2PreservesEveryOldFamily(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	group := groups[0]
	selector, err := releaseConnectPolicySelectorAssignment(script, group.variable)
	if err != nil {
		t.Fatal(err)
	}
	for _, omitted := range strings.Split(releaseEvidenceV2OldProducerGroups, "|") {
		alternatives := strings.Split(strings.TrimSuffix(strings.TrimPrefix(selector, "^Test("), ")"), "|")
		var retained []string
		for _, alternative := range alternatives {
			if alternative != omitted {
				retained = append(retained, alternative)
			}
		}
		changed := strings.Replace(script, group.variable+"='"+selector+"'", group.variable+"='^Test("+strings.Join(retained, "|")+")'", 1)
		if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
			t.Fatalf("evidence guard lost old family %s", omitted)
		}
	}
}

// Each new validator and companion family is necessary on its own, rather
// than only in one aggregate negative selector that might mask an omission.
func TestProducerGateStateSelectionEvidenceV2RejectsEveryNewFamilyOmission(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	for _, index := range []int{0, 1, 3} {
		group := groups[index]
		families := "ValidatorEvidence|RuntimeEvidenceV2|RuntimeEvidence|EvidenceRelay|EvmTxManager|ClientKeyHistory"
		if index == 0 {
			families = releaseEvidenceV2NewProducerGroups
		}
		selector, err := releaseConnectPolicySelectorAssignment(script, group.variable)
		if err != nil {
			t.Fatal(err)
		}
		for _, omitted := range strings.Split(families, "|") {
			alternatives := strings.Split(strings.TrimSuffix(strings.TrimPrefix(selector, "^Test("), ")"), "|")
			var retained []string
			for _, alternative := range alternatives {
				if alternative != omitted {
					retained = append(retained, alternative)
				}
			}
			changed := strings.Replace(script, group.variable+"='"+selector+"'", group.variable+"='^Test("+strings.Join(retained, "|")+")'", 1)
			if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
				t.Fatalf("evidence guard lost new family %s", omitted)
			}
		}
	}
}

// Renaming a reviewed source root cannot escape coverage by leaving the old
// prefix. Reordering alternatives and adding genuine adjacent roots is valid.
func TestProducerGateStateSelectionEvidenceV2KeepsAdjacentSourceCoverage(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	for index, group := range groups {
		for _, packagePath := range group.packages {
			original := group.sources[packagePath]
			group.sources[packagePath] = append(append([]string{}, original...), "func TestUnselectedEvidenceBoundary(t *testing.T) {}\n")
			if err := verifyReleaseEvidenceV2GateGroup(script, group); err == nil {
				t.Fatalf("renamed %s source escaped its real selector", packagePath)
			}
			prefix := []string{"ReleaseEvidenceV2", "ValidatorEvidence", "ProducerGateStateSelection", "ValidatorEvidence"}[index]
			group.sources[packagePath] = append(append([]string{}, original...), "func Test"+prefix+"FutureAdjacentBoundary(t *testing.T) {}\n")
			if err := verifyReleaseEvidenceV2GateGroup(script, group); err != nil {
				t.Fatalf("adjacent %s source was narrowed: %v", packagePath, err)
			}
			group.sources[packagePath] = original
		}
		if group.alternatives == "" {
			continue
		}
		selector, err := releaseConnectPolicySelectorAssignment(script, group.variable)
		if err != nil {
			t.Fatal(err)
		}
		alternatives := strings.Split(strings.TrimSuffix(strings.TrimPrefix(selector, "^Test("), ")"), "|")
		for left, right := 0, len(alternatives)-1; left < right; left, right = left+1, right-1 {
			alternatives[left], alternatives[right] = alternatives[right], alternatives[left]
		}
		changed := strings.Replace(script, group.variable+"='"+selector+"'", group.variable+"='^Test("+strings.Join(alternatives, "|")+"|FutureEvidenceBoundary)'", 1)
		if err := verifyReleaseEvidenceV2GateGroup(changed, group); err != nil {
			t.Fatalf("independent alternative order was refused: %v", err)
		}
	}
}

// Comments, a duplicate command or an ignored failure cannot stand in for one
// real normal/race process in the phase that owns the actual selector.
func TestProducerGateStateSelectionEvidenceV2RejectsMissingExecutableModes(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	for _, group := range groups {
		for _, command := range group.commands {
			for _, replacement := range []string{":", "# " + command, command + "\n  " + command, command + " || true"} {
				changed := strings.Replace(script, command, replacement, 1)
				if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
					t.Fatalf("non-executable or ambiguous %s mode was accepted", group.variable)
				}
			}
		}
	}
}

// The checked code has to be the unique function admitted by the real bounded
// phase runner. An uncalled lookalike body does not provide gate execution.
func TestProducerGateStateSelectionEvidenceV2RejectsDisconnectedPhases(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	for _, group := range groups {
		job := group.job
		if job == "" {
			job = group.phase
		}
		start := "release_gate_start " + job + " release_phase_" + group.phase
		for _, replacement := range []string{":", "# " + start, start + "\n" + start} {
			changed := strings.Replace(script, start, replacement, 1)
			if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
				t.Fatalf("disconnected or duplicate %s phase was accepted", group.phase)
			}
		}
		changed := script + "\nrelease_phase_" + group.phase + "() {\n}\n"
		if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
			t.Fatalf("duplicate %s function owner was accepted", group.phase)
		}
	}
}

// A narrowed end anchor, repeated assignment or assignment outside its phase
// cannot keep the original complete source coverage through a stale variable.
func TestProducerGateStateSelectionEvidenceV2RejectsSelectorOwnershipDrift(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	for _, group := range groups {
		selector, err := releaseConnectPolicySelectorAssignment(script, group.variable)
		if err != nil {
			t.Fatal(err)
		}
		assignment := group.variable + "='" + selector + "'"
		for _, changed := range []string{
			strings.Replace(script, assignment, "# "+assignment, 1),
			script + "\n" + assignment + "\n",
			strings.Replace(script, assignment, ":", 1) + "\n" + assignment + "\n",
			strings.Replace(script, assignment, group.variable+"='"+selector+"$'", 1),
		} {
			if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
				t.Fatalf("selector ownership drift escaped %s", group.variable)
			}
		}
	}
}

// Empty, missing and duplicate source roots are not a successful package
// census; equal root names in distinct packages still have independent owners.
func TestProducerGateStateSelectionEvidenceV2RequiresCompleteSourceCensus(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	for _, group := range groups {
		for _, packagePath := range group.packages {
			original := group.sources[packagePath]
			group.sources[packagePath] = nil
			if err := verifyReleaseEvidenceV2GateGroup(script, group); err == nil {
				t.Fatalf("empty %s source census was accepted", packagePath)
			}
			delete(group.sources, packagePath)
			if err := verifyReleaseEvidenceV2GateGroup(script, group); err == nil {
				t.Fatalf("missing %s source census was accepted", packagePath)
			}
			group.sources[packagePath] = append(append([]string{}, original...), original...)
			if err := verifyReleaseEvidenceV2GateGroup(script, group); err == nil {
				t.Fatalf("duplicate %s source census was accepted", packagePath)
			}
			group.sources[packagePath] = original
		}
	}
	contract := groups[1]
	for _, packagePath := range contract.packages {
		contract.sources[packagePath] = append(contract.sources[packagePath], "func TestValidatorEvidenceSharedPackageRoot(t *testing.T) {}\n")
	}
	if err := verifyReleaseEvidenceV2GateGroup(script, contract); err != nil {
		t.Fatalf("independent package identities were conflated: %v", err)
	}
}

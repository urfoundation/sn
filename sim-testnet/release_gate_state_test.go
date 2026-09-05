package main

// New safety regressions must be selected by the actual launch-critical gate,
// not merely exist in source or happen to run in the later aggregate suite.

import (
	"os"
	"regexp"
	"testing"
)

// Check every declaration in each reviewed source group and both executable
// normal/race command lines. Comments cannot stand in for an invocation.
func assertProducerStateRegressionCoverage(t *testing.T, variable, packagePath, boundary string, paths []string) {
	t.Helper()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	selector, err := releaseConnectPolicySelectorAssignment(script, variable)
	if err != nil {
		t.Fatal(err)
	}
	sources := make([]string, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, string(raw))
	}
	if err := verifyReleaseSourceTestCoverage(selector, "^Test", sources); err != nil {
		t.Fatalf("producer selector omits %s regression: %v", boundary, err)
	}
	for _, prefix := range []string{"go test ", "go test -race "} {
		command := prefix + packagePath + " -run \"$" + variable + "\" -count=1"
		invocation := regexp.MustCompile("(?m)^[\\t ]*" + regexp.QuoteMeta(command) + "(?:[\\t ]+[^\\n]*)?$")
		if !invocation.MatchString(script) {
			t.Fatalf("producer gate does not execute %s regressions with %s", boundary, command)
		}
	}
}

// Disk-prefixed tests include import, namespace, private-directory, replay,
// ownership, proof-projection and durable-failure behavior beyond Attempt*.
func TestProducerGateStateSelectionCoversDiskLedger(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "disk-ledger", []string{
		"../validator/attempt_ledger_disk_test.go",
		"../validator/attempt_proof_stream_test.go",
	})
}

// The depth family must include every original substitution and adjacent
// clamp/encoding/authentication regression, not only the generic trail tests.
func TestProducerGateStateSelectionCoversPolicyDepth(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "policy-depth", []string{
		"../validator/trail_policy_depth_test.go",
		"../validator/trail_policy_depth_admission_test.go",
	})
}

// State protection is required at inner classification and the actual render,
// launch-before-migration and render-before-payload-recovery entry points.
func TestProducerGateStateSelectionCoversAllNamespaceEntryPoints(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "state-namespace", []string{
		"validator_state_namespace_test.go",
		"validator_state_namespace_disk_test.go",
		"validator_state_namespace_render_test.go",
		"validator_state_namespace_outer_test.go",
	})
}

// The launcher and these source-selection assertions must themselves run in
// the launch-critical gate; a self-omitted guard cannot protect that gate.
func TestProducerGateStateSelectionCoversLauncherAndItsOwnChecks(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "qualification-launcher", []string{
		"qualification_launcher_test.go",
		"release_gate_state_test.go",
	})
}

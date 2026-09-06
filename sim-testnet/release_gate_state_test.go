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

// Transport framing and complete public sealing must both qualify before a
// launch can publish signed cuts; the generic Attempt prefix selects neither.
func TestProducerGateStateSelectionCoversAttemptStreamPublication(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "attempt-stream-publication", []string{
		"../validator/attempt_stream_v2_http_test.go",
		"../validator/attempt_stream_v2_http_replay_test.go",
		"../validator/attempt_cut_v2_seal_test.go",
		"../validator/attempt_cut_v2_seal_integrity_test.go",
		"../validator/attempt_cut_v2_seal_policy_test.go",
		"../validator/attempt_cut_v2_seal_lifecycle_test.go",
		"../validator/attempt_cut_v2_seal_scratch_test.go",
		"../validator/attempt_cut_v2_replica_test.go",
		"../validator/attempt_cut_v2_replica_goexit_test.go",
		"../validator/attempt_cut_v2_replica_lifecycle_test.go",
		"../validator/attempt_cut_v2_replica_publication_test.go",
	})
}

// Compact score reconstruction must retain complete signed replay and all of
// its negative census, cursor, publication and independent-bound controls.
func TestProducerGateStateSelectionCoversAttemptStreamStats(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "attempt-stream-statistics", []string{
		"../validator/attempt_cut_v2_stats_test.go",
		"../validator/attempt_cut_v2_stats_quality_test.go",
		"../validator/attempt_cut_v2_stats_admission_test.go",
	})
}

// Compact head scores must retain exact signed binding provenance and refuse
// stale-owner attribution without discarding any part of the stream replay.
func TestProducerGateStateSelectionCoversAttemptStreamHead(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "attempt-stream-head", []string{
		"../validator/attempt_cut_v2_head_test.go",
	})
}

// Both statistics and head attribution share the actual complete replay;
// integration cannot omit either projection's atomic-publication controls.
func TestProducerGateStateSelectionCoversAttemptStreamMeasurement(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "attempt-stream-measurement", []string{
		"../validator/attempt_cut_v2_measurement_test.go",
	})
}

// The public compact artifact path must retain real-stream wire, ownership,
// complete-census and lineage controls in both actual producer gate modes.
func TestProducerGateStateSelectionCoversReleaseMeasurementV2(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "compact-release-measurement", []string{
		"../validator/release_measurement_v2_test.go",
		"../validator/release_measurement_v2_settlement_test.go",
		"../validator/release_measurement_envelope_v2_test.go",
	})
}

// Terminal replay, complete participant consent, restart and exact successor
// fold/cursor continuity must qualify before a compact runtime can close.
func TestProducerGateStateSelectionCoversCompactSettlement(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "compact-settlement", []string{
		"../validator/attempt_transition_v2_test.go",
		"../validator/attempt_closure_v2_test.go",
	})
}

// Simulator storage bindings must qualify before public replication can use
// their per-operator namespaces, independent typed limits and origin profile.
func TestProducerGateStateSelectionCoversAttemptReplicaStorage(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "attempt-replica-storage", []string{
		"attempt_replicas_test.go",
	})
}

// Write ownership spans startup, every event writer, persistence, multi-engine
// ordering and runtime routing; a broad Attempt prefix omits the Stats roots.
func TestProducerGateStateSelectionCoversStatsWriteOwnership(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "statistics-write-ownership", []string{
		"../validator/attempt_stats_callbacks_test.go",
		"../validator/attempt_stats_replay_test.go",
		"../validator/stats_write_test.go",
		"../validator/stats_multibatch_order_test.go",
		"../validator/stats_multibatch_adjacent_test.go",
		"../validator/stats_settlement_routing_test.go",
		"../validator/stats_settlement_routing_adjacent_test.go",
		"../validator/stats_settlement_publication_test.go",
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
		"release_gate_measurement_v2_test.go",
		"release_gate_terminal_v2_test.go",
		"release_gate_canonical_hex_test.go",
		"release_gate_envelope_v2_test.go",
	})
}

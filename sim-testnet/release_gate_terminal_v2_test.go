package main

// Runtime wiring is a separate requirement. These guards pin the actual
// terminal APIs to complete shared replay, not a header-only helper silo.

import "testing"

// Every public terminal path reaches admission and one joint policy traversal.
// This deliberately does not claim the legacy runtime already calls v2.
func TestProducerGateStateSelectionPinsCompactSettlementReplayEdges(t *testing.T) {
	for _, check := range []struct{ path, function, callee string }{
		{path: "../validator/attempt_transition_v2_verify.go", function: "SealAttemptSettlementBatchV2", callee: "admitAttemptSettlementV2"},
		{path: "../validator/attempt_transition_v2_verify.go", function: "SealAttemptSettlementBatchV2", callee: "replay"},
		{path: "../validator/attempt_transition_v2_verify.go", function: "SealAttemptSettlementBatchV2", callee: "attemptSettlementV2Members"},
		{path: "../validator/attempt_transition_v2_verify.go", function: "VerifyAttemptSettlementClosureV2", callee: "verifyAttemptSettlementClosureV2"},
		{path: "../validator/attempt_transition_v2_verify.go", function: "verifyAttemptSettlementClosureV2", callee: "admitAttemptSettlementV2"},
		{path: "../validator/attempt_transition_v2_verify.go", function: "verifyAttemptSettlementClosureV2", callee: "replay"},
		{path: "../validator/attempt_transition_v2_verify.go", function: "replay", callee: "verifyReleaseStatsAndHeadWithAttemptCutV2"},
		{path: "../validator/attempt_cut_v2_measurement.go", function: "verifyReleaseStatsAndHeadWithAttemptCutV2", callee: "ReplayAttemptCutV2WithPolicy"},
		{path: "../validator/attempt_closure_v2.go", function: "DecodeAttemptSettlementClosureV2", callee: "admitAttemptSettlementV2"},
		{path: "../validator/attempt_closure_v2.go", function: "DecodeAttemptSettlementClosureV2", callee: "replay"},
		{path: "../validator/attempt_closure_v2.go", function: "VerifyAttemptSettlementClosureV2Lineage", callee: "verifyAttemptSettlementV2Successor"},
		{path: "../validator/attempt_closure_v2.go", function: "VerifyAttemptSettlementClosureV2Lineage", callee: "replay"},
	} {
		if !releaseClosureFunctionCalls(t, check.path, check.function)[check.callee] {
			t.Errorf("%s omits actual complete terminal edge %s", check.function, check.callee)
		}
	}
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "compact-settlement-replay-edges", []string{"release_gate_terminal_v2_test.go"})
}

package main

// Snapshot custody remains in the actual producer selection and call graph;
// a standalone helper/test cannot silently replace the runtime write path.

import "testing"

// Preserve every new direct, compatibility and genuine signed-replay control.
func TestProducerGateStateSelectionCoversStatsSnapshotCustody(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "stats-snapshot-custody", []string{
		"../validator/stats_snapshot_write_test.go",
		"../validator/stats_snapshot_write_adjacent_test.go",
		"../validator/stats_snapshot_write_runtime_test.go",
	})
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "stats-snapshot-custody-retention", []string{"release_gate_stats_snapshot_test.go"})
}

// Each edge is inside the real method. Publication/cleanup stay native and
// callbacks cannot bypass either the early owner or the post-close witness.
func TestProducerGateStateSelectionStatsSnapshotRetainsNativeCustody(t *testing.T) {
	for _, edge := range []struct { path, caller, callee string }{
		{path: "../validator/stats_write.go", caller: "persistChecked", callee: "acquireStatsSnapshotDirectory"},
		{path: "../validator/stats_write.go", caller: "persistChecked", callee: "admit"},
		{path: "../validator/stats_write.go", caller: "persistChecked", callee: "writeStatsSnapshotOwned"},
		{path: "../validator/stats_write.go", caller: "persistChecked", callee: "finishSnapshot"},
		{path: "../validator/stats_write.go", caller: "saveOwned", callee: "encodeStatsSnapshot"},
		{path: "../validator/attempt_ledger.go", caller: "AttachAttemptLedgerContext", callee: "prepareSnapshot"},
		{path: "../validator/attempt_ledger.go", caller: "AttachAttemptLedgerContext", callee: "finishSnapshot"},
		{path: "../validator/measurement_stats.go", caller: "detachReleaseStatsMeasurement", callee: "prepareSnapshot"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2WithJournalGuard", callee: "prepareSnapshot"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2WithJournalGuard", callee: "prepareSnapshot"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2OwnedWithJournalGuard", callee: "checkSnapshot"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2OwnedWithJournalGuard", callee: "checkSnapshot"},
		{path: "../validator/attempt_settlement.go", caller: "AdvanceAttemptSettlementEpoch", callee: "writeEncodedStatsSnapshot"},
		{path: "../validator/release_settlement.go", caller: "advanceReleaseSettlementSnapshotWithMode", callee: "writeEncodedStatsSnapshot"},
		{path: "../validator/stats_snapshot_write_native.go", caller: "writeStatsSnapshotOwned", callee: "openFile"},
		{path: "../validator/stats_snapshot_write_native.go", caller: "writeStatsSnapshotOwned", callee: "Renameat"},
		{path: "../validator/stats_snapshot_write_native.go", caller: "writeStatsSnapshotOwned", callee: "statAttemptPrivateFile"},
		{path: "../validator/stats_snapshot_write_native.go", caller: "removeTemporary", callee: "Unlinkat"},
		{path: "../validator/stats_snapshot_write_native.go", caller: "makeReady", callee: "Mkdirat"},
		{path: "../validator/stats_snapshot_write_native.go", caller: "finish", callee: "checkFinal"},
		{path: "../validator/stats_snapshot_write_native.go", caller: "checkFinal", callee: "openAttemptPrivateDirectory"},
		{path: "../validator/stats_snapshot_write_native.go", caller: "checkFinal", callee: "checkLeaf"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.caller)[edge.callee] { t.Errorf("%s lost Stats snapshot custody edge %s", edge.caller, edge.callee) }
	}
	for _, name := range []string{"writeStatsSnapshotOwned", "removeTemporary"} {
		calls := releaseClosureFunctionCalls(t, "../validator/stats_snapshot_write_native.go", name)
		if calls["atomicStateWrite"] || calls["CreateTemp"] || calls["Rename"] || calls["Remove"] { t.Errorf("native Stats snapshot %s acquired a pathname write bypass", name) }
	}
}

package main

import "testing"

// Every ordinary state/journal control participates in both producer modes;
// this guard's own file must also remain in the actual simulator selection.
func TestProducerGateStateSelectionCoversOrdinaryRuntimeV2(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "compact-ordinary-runtime", []string{
		"../validator/measurement_stats_v2_test.go",
		"../validator/release_measurement_input_v2_test.go",
		"../validator/release_measurement_input_v2_read_test.go",
		"../validator/release_measurement_input_v2_custody_commit_test.go",
	})
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "compact-ordinary-runtime-retention", []string{
		"release_gate_ordinary_v2_test.go",
	})
}

// Private runtime entrypoints retain real ledger ownership, complete replay,
// immutable journal persistence and actual successor snapshot publication.
// Startup activation and outer live routing remain separate integration gates.
func TestProducerGateStateSelectionOrdinaryRuntimeV2RetainsOwnerAndReplay(t *testing.T) {
	for _, edge := range []struct { path, caller, callee string }{
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2", callee: "detachReleaseStatsMeasurementV2WithJournalGuard"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2WithJournalGuard", callee: "acquireStatsWrite"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2WithJournalGuard", callee: "releaseStatsV2Measurement"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2WithJournalGuard", callee: "detachReleaseStatsMeasurementV2OwnedWithJournalGuard"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2Owned", callee: "detachReleaseStatsMeasurementV2OwnedWithJournalGuard"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2OwnedWithJournalGuard", callee: "SealAttemptCutV2"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2OwnedWithJournalGuard", callee: "VerifyReleaseStatsMeasurementWithAttemptCutV2"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2OwnedWithJournalGuard", callee: "releaseStatsV2OwnedHead"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2OwnedWithJournalGuard", callee: "saveOwned"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2OwnedWithJournalGuard", callee: "guard"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2", callee: "reconcileReleaseStatsCutV2WithJournalGuard"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2WithJournalGuard", callee: "acquireStatsWrite"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2WithJournalGuard", callee: "reconcileReleaseStatsCutV2OwnedWithJournalGuard"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2Owned", callee: "reconcileReleaseStatsCutV2OwnedWithJournalGuard"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2OwnedWithJournalGuard", callee: "releaseStatsV2PrefixRoot"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2OwnedWithJournalGuard", callee: "VerifyHeader"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2OwnedWithJournalGuard", callee: "VerifyReleaseStatsMeasurementWithAttemptCutV2"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2OwnedWithJournalGuard", callee: "releaseStatsV2SuffixEgress"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2OwnedWithJournalGuard", callee: "saveOwned"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2OwnedWithJournalGuard", callee: "guard"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2OwnedWithJournalGuard", callee: "releaseStatsV2JournalPersist"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2OwnedWithJournalGuard", callee: "releaseStatsV2JournalPersist"},
		{path: "../validator/measurement_stats_v2.go", caller: "releaseStatsV2JournalPersist", callee: "persistChecked"},
		{path: "../validator/measurement_stats_v2.go", caller: "releaseStatsV2JournalPersist", callee: "guard"},
		{path: "../validator/measurement_stats_v2.go", caller: "releaseStatsV2SuffixEgress", callee: "VerifyAttemptRecord"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2", callee: "loadOrDetachReleaseMeasurementInputV2WithReadHooks"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2WithReadHooks", callee: "validateReleaseMeasurementInputV2Context"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2WithReadHooks", callee: "detachReleaseStatsMeasurementV2WithJournalGuard"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2WithReadHooks", callee: "reconcileReleaseStatsCutV2WithJournalGuard"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2WithReadHooks", callee: "acquireReleaseMeasurementInputV2Owner"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2WithReadHooks", callee: "read"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2WithReadHooks", callee: "write"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2WithReadHooks", callee: "sync"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2WithReadHooks", callee: "finish"},
		{path: "../validator/release_measurement_input_v2_read.go", caller: "finish", callee: "openAttemptPrivateDirectory"},
		{path: "../validator/release_measurement_input_v2_read.go", caller: "finish", callee: "checkLeaf"},
		{path: "../validator/release_measurement_input_v2_read.go", caller: "read", callee: "openFile"},
		{path: "../validator/release_measurement_input_v2_read.go", caller: "read", callee: "close"},
		{path: "../validator/release_measurement_input_v2_read.go", caller: "read", callee: "check"},
		{path: "../validator/release_measurement_input_v2_write.go", caller: "write", callee: "Linkat"},
		{path: "../validator/release_measurement_input_v2_write.go", caller: "write", callee: "removeTemporary"},
		{path: "../validator/release_measurement_input_v2_write.go", caller: "removeTemporary", callee: "Unlinkat"},
		{path: "../validator/release_measurement_input_v2_write.go", caller: "sync", callee: "Sync"},
		{path: "../validator/release_measurement_input_v2_write.go", caller: "sync", callee: "check"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.caller)[edge.callee] { t.Errorf("%s lost ordinary runtime edge %s", edge.caller, edge.callee) }
	}
	for _, caller := range []string{"detachReleaseStatsMeasurementV2Owned", "reconcileReleaseStatsCutV2Owned", "detachReleaseStatsMeasurementV2OwnedWithJournalGuard", "reconcileReleaseStatsCutV2OwnedWithJournalGuard"} {
		calls := releaseClosureFunctionCalls(t, "../validator/measurement_stats_v2.go", caller)
		if calls["BuildCut"] || calls["VerifyReleaseStatsMeasurement"] { t.Errorf("compact runtime %s acquired a legacy replay bypass", caller) }
	}
}

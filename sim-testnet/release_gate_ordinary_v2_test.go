package main

import "testing"

// Every ordinary state/journal control participates in both producer modes;
// this guard's own file must also remain in the actual simulator selection.
func TestProducerGateStateSelectionCoversOrdinaryRuntimeV2(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "compact-ordinary-runtime", []string{
		"../validator/measurement_stats_v2_test.go",
		"../validator/release_measurement_input_v2_test.go",
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
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2", callee: "acquireStatsWrite"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2", callee: "releaseStatsV2Measurement"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2", callee: "detachReleaseStatsMeasurementV2Owned"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2Owned", callee: "SealAttemptCutV2"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2Owned", callee: "VerifyReleaseStatsMeasurementWithAttemptCutV2"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2Owned", callee: "releaseStatsV2OwnedHead"},
		{path: "../validator/measurement_stats_v2.go", caller: "detachReleaseStatsMeasurementV2Owned", callee: "saveOwned"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2", callee: "acquireStatsWrite"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2Owned", callee: "releaseStatsV2PrefixRoot"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2Owned", callee: "VerifyHeader"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2Owned", callee: "VerifyReleaseStatsMeasurementWithAttemptCutV2"},
		{path: "../validator/measurement_stats_v2.go", caller: "reconcileReleaseStatsCutV2Owned", callee: "releaseStatsV2SuffixEgress"},
		{path: "../validator/measurement_stats_v2.go", caller: "releaseStatsV2SuffixEgress", callee: "VerifyAttemptRecord"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2", callee: "validateReleaseMeasurementInputV2Context"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2", callee: "detachReleaseStatsMeasurementV2"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2", callee: "reconcileReleaseStatsCutV2"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2", callee: "writeReleaseMeasurementInputV2"},
		{path: "../validator/release_measurement_input_v2.go", caller: "loadOrDetachReleaseMeasurementInputV2", callee: "syncReleaseMeasurementInputV2Directory"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.caller)[edge.callee] { t.Errorf("%s lost ordinary runtime edge %s", edge.caller, edge.callee) }
	}
	for _, caller := range []string{"detachReleaseStatsMeasurementV2Owned", "reconcileReleaseStatsCutV2Owned"} {
		calls := releaseClosureFunctionCalls(t, "../validator/measurement_stats_v2.go", caller)
		if calls["BuildCut"] || calls["VerifyReleaseStatsMeasurement"] { t.Errorf("compact runtime %s acquired a legacy replay bypass", caller) }
	}
}

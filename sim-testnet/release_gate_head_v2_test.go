package main

import "testing"

// Live compact head behavior and this guard must be selected in both actual
// producer modes, not only happen to execute in the wider aggregate suite.
func TestProducerGateStateSelectionCoversLiveHeadV2(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "compact-live-head", []string{"../validator/release_head_v2_test.go"})
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "compact-live-head-retention", []string{"release_gate_head_v2_test.go"})
}

// These source edges supplement real HTTP/M8 behavior; they do not claim the
// new head method is wired into startup/SubmitOnce or authenticate chain history.
func TestProducerGateStateSelectionLiveHeadV2RetainsReplayAndMath(t *testing.T) {
	for _, edge := range []struct { path, caller, callee string }{
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "ownReleaseMeasurementV2"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "admitReleaseHeadV2Controls"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "ownReleaseMeasurementEnvelopeV2Hotkey"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "VerifyHeader"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "ReleaseBindingsAtHashContext"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "releaseMeasurementBindingObservations"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "VerifyReleaseStatsAndHeadWithAttemptCutV2"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "previewForEpochV2"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "assembleReleaseHead"},
		{path: "../validator/release_head_v2.go", caller: "previewForEpochV2", callee: "releaseMeasurementV2ControlStorage"},
		{path: "../validator/release_head_v2.go", caller: "previewForEpochV2", callee: "previewForEpochWithLock"},
		{path: "../validator/head_ema.go", caller: "PreviewForEpoch", callee: "previewForEpochWithLock"},
		{path: "../validator/release_steer.go", caller: "gatherHead", callee: "finishReleaseHead"},
		{path: "../validator/release_head.go", caller: "finishReleaseHead", callee: "releaseRawHeadScores"},
		{path: "../validator/release_head.go", caller: "finishReleaseHead", callee: "PreviewForEpoch"},
		{path: "../validator/release_head.go", caller: "finishReleaseHead", callee: "assembleReleaseHead"},
		{path: "../validator/release_head.go", caller: "assembleReleaseHead", callee: "selectHeadFleets"},
		{path: "../validator/release_head.go", caller: "assembleReleaseHead", callee: "excludeLiveHeadMembers"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.caller)[edge.callee] { t.Errorf("%s lost live head edge %s", edge.caller, edge.callee) }
	}
	calls := releaseClosureFunctionCalls(t, "../validator/release_head_v2.go", "gatherHeadV2")
	if calls["BuildCut"] || calls["AttemptCutEgressClaims"] || calls["VerifyReleaseStatsMeasurement"] || calls["FindUidByHotkey"] || calls["CommitForEpoch"] { t.Fatal("compact live head acquired a legacy/current-height or early-publication bypass") }
}

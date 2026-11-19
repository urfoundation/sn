package main

import "testing"

// Live compact head behavior and this guard must be selected in both actual
// producer modes, not only happen to execute in the wider aggregate suite.
func TestProducerGateStateSelectionCoversLiveHeadV2(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "compact-live-head", []string{"../validator/release_head_v2_test.go", "../validator/release_head_v2_admission_test.go", "../validator/release_head_v2_integration_test.go"})
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "compact-live-head-retention", []string{"release_gate_head_v2_test.go"})
}

// These source edges supplement real HTTP/M8 behavior; they do not claim the
// new head method is wired into startup/SubmitOnce or authenticate chain history.
func TestProducerGateStateSelectionLiveHeadV2RetainsReplayAndMath(t *testing.T) {
	for _, edge := range []struct{ path, caller, callee string }{
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "ownReleaseMeasurementV2"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "admitReleaseHeadV2Controls"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "reserveReleaseHeadV2OperatorControls"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "ownReleaseMeasurementEnvelopeV2Hotkey"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "VerifyHeader"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "readReleaseProviderBindingsV2Context"},
		{path: "../validator/release_decision_chain_v2.go", caller: "readReleaseProviderBindingsV2Context", callee: "batchCallsAtHashContext"},
		{path: "../validator/release_decision_chain_v2.go", caller: "readReleaseProviderBindingsV2Context", callee: "canonicalReleaseDecisionV2View"},
		{path: "../validator/release_decision_chain_v2.go", caller: "readReleaseProviderBindingsV2Context", callee: "UnpackBindingAt"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "releaseMeasurementBindingObservations"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "verifyReleaseStatsAndHeadOnce"},
		{path: "../validator/release_cut_replay_memo.go", caller: "verifyReleaseStatsAndHeadOnce", callee: "verifyReleaseStatsAndHeadWithAttemptCutV2"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "admitReleaseHeadV2Known"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "admitReleaseHeadV2Current"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "ownReleaseHeadEMAV2"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "witness"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "finish"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "previewFleets"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "checkReleaseHeadV2Result"},
		{path: "../validator/release_head_v2.go", caller: "gatherHeadV2", callee: "assembleReleaseHead"},
		{path: "../validator/release_head_v2.go", caller: "admitReleaseHeadV2Controls", callee: "reserveReleaseHeadV2CollectionDerived"},
		{path: "../validator/release_head_v2.go", caller: "previewForEpochV2", callee: "previewForEpochV2WithBudget"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewForEpochV2WithBudget", callee: "planReleaseHeadV2PreviewWithLock"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewForEpochV2WithBudget", callee: "previewForEpochWithLock"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewForEpochV2WithBudget", callee: "checkReleaseHeadV2Output"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewReleaseHeadV2Fleets", callee: "ownReleaseHeadEMAV2"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewReleaseHeadV2Fleets", callee: "previewFleets"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewFleets", callee: "previewReleaseHeadV2FleetsWithLock"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewReleaseHeadV2FleetsWithLock", callee: "planReleaseHeadV2Raw"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewReleaseHeadV2FleetsWithLock", callee: "planReleaseHeadV2PreviewWithLock"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewReleaseHeadV2FleetsWithLock", callee: "buildReleaseHeadV2Raw"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewReleaseHeadV2FleetsWithLock", callee: "previewForEpochWithLock"},
		{path: "../validator/release_head_v2_admission.go", caller: "previewReleaseHeadV2FleetsWithLock", callee: "checkReleaseHeadV2Output"},
		{path: "../validator/head_ema.go", caller: "PreviewForEpoch", callee: "previewForEpochWithLock"},
		{path: "../validator/release_steer.go", caller: "gatherHead", callee: "finishReleaseHead"},
		{path: "../validator/release_head.go", caller: "finishReleaseHead", callee: "releaseRawHeadScores"},
		{path: "../validator/release_head.go", caller: "finishReleaseHead", callee: "PreviewForEpoch"},
		{path: "../validator/release_head.go", caller: "finishReleaseHead", callee: "assembleReleaseHead"},
		{path: "../validator/release_head.go", caller: "assembleReleaseHead", callee: "selectHeadFleets"},
		{path: "../validator/release_head.go", caller: "assembleReleaseHead", callee: "excludeLiveHeadMembers"},
	} {
		if !releaseClosureFunctionCalls(t, edge.path, edge.caller)[edge.callee] {
			t.Errorf("%s lost live head edge %s", edge.caller, edge.callee)
		}
	}
	calls := releaseClosureFunctionCalls(t, "../validator/release_head_v2.go", "gatherHeadV2")
	if calls["BuildCut"] || calls["AttemptCutEgressClaims"] || calls["VerifyReleaseStatsMeasurement"] || calls["FindUidByHotkey"] || calls["CommitForEpoch"] || calls["releaseRawHeadScores"] {
		t.Fatal("compact live head acquired a legacy/current-height, unplanned rational or early-publication bypass")
	}
}

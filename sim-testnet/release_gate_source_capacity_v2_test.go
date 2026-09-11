package main

import "testing"

// The real private capture/intent bounds and canonical page geometry run in
// both validator modes, not only in a configuration-only admission test.
func TestProducerGateStateSelectionCoversActualSourceCapacityV2(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "source-capture-capacity", []string{
		"../validator/release_capture_capacity_v2_test.go",
	})
}

// Full campaign configuration and its genuine pre-Prepare refusal join the
// same actual simulator phase as the funded runtime they depend on.
func TestProducerGateStateSelectionCoversCampaignSourceCapacityV2(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "simulator_evidence_tests", "./sim-testnet", "campaign-source-capacity", []string{
		"runtime_evidence_source_capacity_test.go",
	})
}

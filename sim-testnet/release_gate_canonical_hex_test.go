package main

// Both producer modes retain the shared real parser work and compatibility
// regressions after ordinary and compact identity admission are composed.

import "testing"

func TestProducerGateStateSelectionCoversCanonicalHexAdmission(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "producer_tests", "./validator", "canonical-hex-admission", []string{
		"../validator/canonical_hex_test.go",
	})
}

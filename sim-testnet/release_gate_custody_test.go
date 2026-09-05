package main

// The pre-launch gate must exercise both public seed-loading entry points,
// their shared descriptor-backed implementation, and these selection checks.

import "testing"

// Select the complete reviewed source groups, including preserved format,
// key-vector, identity reload and mirror-address controls.
func TestProducerGateCustodySelectionCoversBothIdentityEntryPoints(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "seed_custody_tests", "./crv4 ./validator", "seed-and-validator-custody", []string{
		"../crv4/keys_test.go",
		"../crv4/keys_custody_test.go",
		"../crv4/seed_file_test.go",
		"../validator/identity_test.go",
		"../validator/identity_custody_test.go",
		"../validator/identity_custody_adjacent_test.go",
	})
}

// A guard omitted from its own ordinary/race gate cannot protect deployment.
func TestProducerGateCustodySelectionCoversItsOwnChecks(t *testing.T) {
	assertProducerStateRegressionCoverage(t, "capture_tests", "./sim-testnet", "custody-selection", []string{
		"release_gate_custody_test.go",
	})
}

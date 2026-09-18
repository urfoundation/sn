// Runtime467 source and artifact provenance are independently pinned before it
// may become the current execution authority.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestRuntime467SourceAttestationRetainsConsumedScopeAndFeeChanges(t *testing.T) {
	raw, err := os.ReadFile("../docs/spec/runtime-v467-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "5e0ca8e26792f73b2fd618cb827e4e2adfa75ca7b43d5dbd3e11bd459850ca96" {
		t.Fatal("runtime467 exact source scope changed")
	}
	paths := map[string]bool{}
	for _, row := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(row)
		if len(fields) != 2 || paths[fields[1]] {
			t.Fatal("runtime467 source rows are malformed or duplicated")
		}
		paths[fields[1]] = true
	}
	if len(paths) != 120 {
		t.Fatalf("runtime467 source scope has %d paths, want 120", len(paths))
	}
	prior, err := os.ReadFile("../docs/spec/runtime-v461-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range strings.Split(strings.TrimSpace(string(prior)), "\n") {
		if !paths[strings.Fields(row)[1]] {
			t.Fatal("runtime467 omitted previously reviewed consumed source")
		}
	}
	for _, path := range []string{
		"runtime/src/lib.rs", "runtime/src/staking_fee.rs", "runtime/src/transaction_payment_wrapper.rs",
		"pallets/transaction-fee/src/lib.rs", "pallets/subtensor/src/weights.rs",
		"pallets/subtensor/src/subnets/weights.rs", "pallets/subtensor/src/staking/claim_root.rs",
		"pallets/commitments/src/lib.rs", "precompiles/src/neuron.rs",
	} {
		if !paths[path] {
			t.Fatalf("runtime467 omitted changed consumed dependency %s", path)
		}
	}
	checker, err := os.ReadFile("../scripts/check-runtime-v454-source.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"for current_spec in 455 458 459 460 461 467", "runtime-v467-source.sha256", "current_commit=\"c6bcb4a7400764c94c1d1b1938514c6c2dd3d33b\"", "expected_current_files=120", "SUBTENSOR_RUNTIME467_SOURCE", "expected_metadata_files=30"} {
		if !strings.Contains(string(checker), required) {
			t.Fatalf("runtime467 source checker omits %s", required)
		}
	}
}

// Runtime460 preserves the complete459 source corpus and changes only its
// reviewed share-pool implementation, regressions and embedded spec version.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// Exact source hashes retain unaffected review and constrain this admission to
// the five changed native paths from the authoritative459-to460 source delta.
func TestRuntime460SourceAttestationPreservesReviewed459Scope(t *testing.T) {
	raw, err := os.ReadFile("../docs/spec/runtime-v460-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "98c2aac04434d2ec67931479cf646918571970718078c50f711486c8db889644" {
		t.Fatal("runtime460 exact source scope changed")
	}
	prior, err := os.ReadFile("../docs/spec/runtime-v459-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	priorRows := strings.Split(strings.TrimSpace(string(prior)), "\n")
	if len(rows) != 71 || len(rows) != len(priorRows) {
		t.Fatal("runtime460 lost retained source scope")
	}
	changed := map[string]bool{
		"pallets/subtensor/src/staking/stake_utils.rs":       false,
		"pallets/subtensor/src/tests/destroy_alpha_tests.rs": false,
		"pallets/subtensor/src/tests/staking.rs":             false,
		"primitives/share-pool/src/lib.rs":                   false,
		"runtime/src/lib.rs":                                 false,
	}
	for index, row := range rows {
		fields, previous := strings.Fields(row), strings.Fields(priorRows[index])
		if len(fields) != 2 || len(previous) != 2 || fields[1] != previous[1] {
			t.Fatal("runtime460 changed the retained source corpus")
		}
		if fields[0] != previous[0] {
			if _, ok := changed[fields[1]]; !ok {
				t.Fatalf("unreviewed source delta: %s", fields[1])
			}
			changed[fields[1]] = true
		}
	}
	for path, found := range changed {
		if !found {
			t.Fatalf("runtime460 omitted source delta %s", path)
		}
	}
	checker, err := os.ReadFile("../scripts/check-runtime-v454-source.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"for current_spec in 455 458 459 460", "runtime-v460-source.sha256", "current_commit=\"8d5f20ec1a5e5d90295d43046dacdefc54aaed06\"", "SUBTENSOR_RUNTIME460_SOURCE", "expected_metadata_files=30"} {
		if !strings.Contains(string(checker), required) {
			t.Fatalf("runtime460 source checker omits %s", required)
		}
	}
}

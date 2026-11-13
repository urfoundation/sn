// Mainnet may begin at steady cadence without a synthetic accelerated era.
// Existing policy bytes, testnet transitions and historical mainnet profiles
// retain their original interpretation.
package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// Select all four windows explicitly before hashing; no runtime default or
// inferred chain observation contributes to this synthetic policy.
func mainnetSteadyCadenceTestPolicy(t *testing.T) Policy {
	t.Helper()
	policy, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	policy.NetworkProfile, policy.EffectiveEpoch = "mainnet", 0
	policy.ProductionCadence = CadenceSnapshot{AfterAcceleratedEpochs: 0, EpochBlocks: 50_400, RootCommitWindowBlocks: 8_400, FinalizeOffsetBlocks: 25_200, CloseGraceBlocks: 840}
	policy.Settlement.EpochBlocks, policy.Settlement.RootCommitWindowBlocks = 50_400, 8_400
	policy.Settlement.FinalizeOffsetBlocks, policy.Settlement.CloseGraceBlocks = 25_200, 840
	return *policy
}

// The ordinary parser/hash is used by validators, servers and install plans.
func TestPolicyMainnetSteadyCadenceBeginsAtEpochZero(t *testing.T) {
	policy := mainnetSteadyCadenceTestPolicy(t)
	canonical, err := policy.CanonicalBytes()
	if err != nil {
		t.Fatalf("steady mainnet cadence cannot be committed from epoch zero: %v", err)
	}
	wire, err := yaml.Marshal(struct {
		Policy Policy `yaml:"policy"`
	}{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParsePolicy(wire)
	if err != nil {
		t.Fatalf("public policy parser refused explicit steady mainnet: %v", err)
	}
	hash, err := parsed.Hash()
	if err != nil || hash != sha256.Sum256(canonical) || parsed.EffectiveEpoch != 0 || parsed.ProductionCadence.AfterAcceleratedEpochs != 0 {
		t.Fatalf("steady mainnet round-trip changed its committed identity: %v", err)
	}
	parsed.EffectiveEpoch = 7
	if err := parsed.Validate(); err != nil {
		t.Fatalf("later independently approved steady policy required invented acceleration: %v", err)
	}
}

// Zero is a declared mainnet mode, never permission to skip a mismatched or
// incomplete transition. All comparisons precede hashes and caller authority.
func TestPolicyMainnetSteadyCadenceRejectsMixedWindows(t *testing.T) {
	original := mainnetSteadyCadenceTestPolicy(t)
	for _, testCase := range []struct {
		name   string
		change func(*Policy)
	}{
		{name: "testnet-zero", change: func(policy *Policy) {
			policy.NetworkProfile = "testnet"
			policy.Settlement.EpochBlocks, policy.ProductionCadence.EpochBlocks = 360, 360
			policy.Settlement.FinalizeOffsetBlocks, policy.ProductionCadence.FinalizeOffsetBlocks = 180, 180
			policy.Settlement.RootCommitWindowBlocks, policy.ProductionCadence.RootCommitWindowBlocks = 60, 60
			policy.Settlement.CloseGraceBlocks, policy.ProductionCadence.CloseGraceBlocks = 6, 6
		}},
		{name: "initial-epoch", change: func(policy *Policy) { policy.Settlement.EpochBlocks-- }},
		{name: "initial-root", change: func(policy *Policy) { policy.Settlement.RootCommitWindowBlocks-- }},
		{name: "initial-finalize", change: func(policy *Policy) { policy.Settlement.FinalizeOffsetBlocks-- }},
		{name: "initial-close", change: func(policy *Policy) { policy.Settlement.CloseGraceBlocks-- }},
		{name: "false-transition", change: func(policy *Policy) { policy.ProductionCadence.AfterAcceleratedEpochs = 5 }},
		{name: "wrong-mainnet-period", change: func(policy *Policy) { policy.Settlement.EpochBlocks++; policy.ProductionCadence.EpochBlocks++ }},
		{name: "zero-root", change: func(policy *Policy) {
			policy.Settlement.RootCommitWindowBlocks, policy.ProductionCadence.RootCommitWindowBlocks = 0, 0
		}},
		{name: "finalize-at-end", change: func(policy *Policy) {
			policy.Settlement.FinalizeOffsetBlocks, policy.ProductionCadence.FinalizeOffsetBlocks = 50_400, 50_400
		}},
		{name: "close-after-root", change: func(policy *Policy) {
			policy.Settlement.CloseGraceBlocks, policy.ProductionCadence.CloseGraceBlocks = 8_401, 8_401
		}},
	} {
		policy := original
		testCase.change(&policy)
		if _, err := policy.CanonicalBytes(); err == nil {
			t.Fatalf("%s acquired a steady mainnet policy hash", testCase.name)
		}
	}
}

// Already committed accelerated mainnet policies remain replayable. The new
// steady representation never mutates or normalizes their serialized fields.
func TestPolicyMainnetSteadyCadencePreservesAcceleratedHistory(t *testing.T) {
	policy, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	policy.NetworkProfile = "mainnet"
	policy.ProductionCadence.EpochBlocks = 50_400
	expected, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := policy.CanonicalBytes()
	if err != nil || !bytes.Equal(actual, expected) || policy.ProductionCadence.AfterAcceleratedEpochs != 5 || policy.Settlement.EpochBlocks != 300 {
		t.Fatalf("new steady mode reinterpreted accelerated mainnet history: %v", err)
	}
}

// JSON tooling must be able to express the same zero-count mode; cross-field
// window equality remains the shared Go validator's semantic responsibility.
func TestPolicyMainnetSteadyCadenceSchemaScopesZeroToMainnet(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "spec", "policy-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	definitions := document["$defs"].(map[string]any)
	cadence := definitions["productionCadence"].(map[string]any)["properties"].(map[string]any)
	if cadence["after_accelerated_epochs"].(map[string]any)["$ref"] != "#/$defs/u64" {
		t.Fatal("public JSON schema still forbids an explicit zero transition")
	}
	policy := document["properties"].(map[string]any)["policy"].(map[string]any)
	condition := policy["allOf"].([]any)[0].(map[string]any)
	zero := condition["if"].(map[string]any)["properties"].(map[string]any)["production_cadence"].(map[string]any)["properties"].(map[string]any)["after_accelerated_epochs"].(map[string]any)["const"]
	result := condition["then"].(map[string]any)["properties"].(map[string]any)
	if zero != float64(0) || result["network_profile"].(map[string]any)["const"] != "mainnet" {
		t.Fatal("zero transition JSON schema lost its mainnet-only scope")
	}
	for _, field := range []string{"settlement", "production_cadence"} {
		if result[field].(map[string]any)["properties"].(map[string]any)["epoch_blocks"].(map[string]any)["const"] != float64(50_400) {
			t.Fatalf("%s JSON schema lost its steady mainnet epoch bound", field)
		}
	}
}

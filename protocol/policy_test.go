package protocol

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

func testPolicyPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "deploy", "testnet", "policy-v1.yml")
}

func TestRequiredDepositRaoUsesExactTierFloorAndCap(t *testing.T) {
	policy, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	amount, tier, err := RequiredDepositRao(2*(1<<30)+512, big.NewInt(0), policy.Deposit)
	if err != nil {
		t.Fatal(err)
	}
	if amount.Uint64() != 2_000_000 || tier.RateNumeratorRaoPerGiB != 1_000_000 {
		t.Fatalf("baseline deposit/tier = %s/%+v", amount, tier)
	}
	conviction := big.NewInt(1_000_000_000)
	tier, err = DepositTierAt(policy.Deposit, conviction)
	if err != nil {
		t.Fatal(err)
	}
	// Derive an input whose uncapped result is strictly above the locked cap.
	// A fixed GiB fixture silently stopped testing the cap when the runtime-valid
	// testnet deposit cap increased.
	capPlusOne := new(big.Int).Add(new(big.Int).SetUint64(policy.Deposit.EpochCapRaoPerOperator), big.NewInt(1))
	usageNumerator := new(big.Int).Mul(capPlusOne, new(big.Int).SetUint64(1<<30))
	usageNumerator.Mul(usageNumerator, new(big.Int).SetUint64(tier.RateDenominator))
	usageBytes := new(big.Int).Quo(usageNumerator, new(big.Int).SetUint64(tier.RateNumeratorRaoPerGiB))
	usageBytes.Add(usageBytes, big.NewInt(1))
	if !usageBytes.IsUint64() {
		t.Fatalf("cap-crossing usage does not fit uint64: %s", usageBytes)
	}
	amount, tier, err = RequiredDepositRao(usageBytes.Uint64(), conviction, policy.Deposit)
	if err != nil {
		t.Fatal(err)
	}
	if amount.Uint64() != policy.Deposit.EpochCapRaoPerOperator || tier.RateNumeratorRaoPerGiB != 800_000 {
		t.Fatalf("capped conviction-tier deposit = %s/%+v", amount, tier)
	}
	if _, _, err := RequiredDepositRao(1, big.NewInt(-1), policy.Deposit); err == nil {
		t.Fatal("negative conviction was accepted")
	}
}

func TestLoadPolicyCanonicalHash(t *testing.T) {
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	h1, err := p.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || h1 == "" {
		t.Fatal("empty canonical policy/hash")
	}
	p2, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := p2.HashHex()
	if h1 != h2 {
		t.Fatalf("non-deterministic policy hash: %s != %s", h1, h2)
	}
}

func TestPolicyStrictAndFailClosed(t *testing.T) {
	b, err := os.ReadFile(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	bad := append(append([]byte(nil), b...), []byte("\n  unknown_field: true\n")...)
	if _, err := ParsePolicy(bad); err == nil {
		t.Fatal("unknown policy field accepted")
	}
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Deposit.Tiers[0].RateDenominator = 0
	if err := p.Validate(); err == nil {
		t.Fatal("zero rate accepted")
	}
	p, err = LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Steering.MaxWeightLimitU16 = 0
	if err := p.Validate(); err == nil {
		t.Fatal("zero release weight cap accepted")
	}
}

func TestPolicyCadenceWindowsFailClosed(t *testing.T) {
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if p.NetworkProfile != "testnet" || p.Settlement.CloseGraceBlocks != 5 || p.ProductionCadence.EpochBlocks != 360 || p.ProductionCadence.AfterAcceleratedEpochs != 5 {
		t.Fatalf("unexpected release cadence: settlement=%+v production=%+v", p.Settlement, p.ProductionCadence)
	}
	tests := []func(*Policy){
		func(v *Policy) { v.Settlement.CloseGraceBlocks = 0 },
		func(v *Policy) { v.Settlement.CloseGraceBlocks = v.Settlement.RootCommitWindowBlocks + 1 },
		func(v *Policy) { v.ProductionCadence.AfterAcceleratedEpochs = 0 },
		func(v *Policy) { v.ProductionCadence.EpochBlocks = v.Settlement.EpochBlocks },
		func(v *Policy) {
			v.ProductionCadence.RootCommitWindowBlocks = v.ProductionCadence.FinalizeOffsetBlocks + 1
		},
	}
	for index, mutate := range tests {
		copy := *p
		mutate(&copy)
		if err := copy.Validate(); err == nil {
			t.Fatalf("invalid cadence mutation %d accepted", index)
		}
	}
}

func TestPolicyRejectsInfeasibleMinimumBreadthWeightCap(t *testing.T) {
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Steering.MaxWeightLimitU16 = 32767
	p.Safety.MinimumHealthyNOCount = 2
	if err := p.Validate(); err == nil {
		t.Fatal("two-recipient policy accepted a cap whose total capacity is below one u16 vector")
	}
	p.Steering.MaxWeightLimitU16 = 32768
	if err := p.Validate(); err != nil {
		t.Fatalf("smallest feasible two-recipient cap was rejected: %v", err)
	}
}

func TestPolicyAllowsExactlyFeasibleMinimumBreadthWeightCap(t *testing.T) {
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Steering.MaxWeightLimitU16 = 21845
	p.Safety.MinimumHealthyNOCount = 3
	if err := p.Validate(); err != nil {
		t.Fatalf("exactly feasible three-recipient cap was rejected: %v", err)
	}
}

func TestPolicyRequiresPositiveFinalizedHeadLagBound(t *testing.T) {
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Safety.MaximumFinalizedHeadLagBlocks = 0
	if err := p.Validate(); err == nil {
		t.Fatal("unbounded finalized-head lag was accepted")
	}
}

func TestPolicyRequiresPositiveVerifyHardLimits(t *testing.T) {
	mutations := []func(*Policy){
		func(policy *Policy) { policy.Verify.HardSeedPerMinutePerSource = 0 },
		func(policy *Policy) { policy.Verify.HardExtendPerMinutePerSource = 0 },
		func(policy *Policy) { policy.Verify.HardActiveTrailsPerSource = 0 },
	}
	for index, mutate := range mutations {
		policy, err := LoadPolicy(testPolicyPath(t))
		if err != nil {
			t.Fatal(err)
		}
		mutate(policy)
		if err := policy.Validate(); err == nil {
			t.Errorf("zero verify hard-limit mutation %d was accepted", index)
		}
	}
}

func TestPolicyJSONSchemaRequiresWeightCapAndPositiveHeadLag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "docs", "spec", "policy-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Definitions map[string]struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	steering := schema.Definitions["steering"]
	required := false
	for _, name := range steering.Required {
		if name == "max_weight_limit_u16" {
			required = true
		}
	}
	if !required || len(steering.Properties["max_weight_limit_u16"]) == 0 {
		t.Fatal("policy schema does not require and define max_weight_limit_u16")
	}
	var lag struct {
		Reference string `json:"$ref"`
	}
	if err := json.Unmarshal(schema.Definitions["safety"].Properties["maximum_finalized_head_lag_blocks"], &lag); err != nil {
		t.Fatal(err)
	}
	if lag.Reference != "#/$defs/positive" {
		t.Fatalf("maximum_finalized_head_lag_blocks schema reference = %q, want positive", lag.Reference)
	}
}

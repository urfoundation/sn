package protocol

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
	amount, tier, err := RequiredDepositRao(2*(1<<30)+512, 0, big.NewInt(0), policy.Deposit)
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
	amount, tier, err = RequiredDepositRao(usageBytes.Uint64(), 0, conviction, policy.Deposit)
	if err != nil {
		t.Fatal(err)
	}
	if amount.Uint64() != policy.Deposit.EpochCapRaoPerOperator || tier.RateNumeratorRaoPerGiB != 800_000 {
		t.Fatalf("capped conviction-tier deposit = %s/%+v", amount, tier)
	}
	if _, _, err := RequiredDepositRao(1, 0, big.NewInt(-1), policy.Deposit); err == nil {
		t.Fatal("negative conviction was accepted")
	}
}

// The canonical hashes of the checked-in testnet policies are pinned so that
// the optional per-user rate and zero_rate_action fields (both omitted when
// zero/absent) never change an existing policy's commitment.
func TestCheckedInTestnetPoliciesKeepCanonicalHashes(t *testing.T) {
	for _, testCase := range []struct{ name, hash string }{
		{"policy-v1.yml", "0x1526b242cf4908cc31f7e58006664bce6064003c69fd8452eab2d49122fef277"},
		{"policy-v2.yml", "0x41f0c7efe7e1b23b2fd22dac9352ca18be48d2e4d1fb5b41ce89660bc899b0dd"},
	} {
		policy, err := LoadPolicy(filepath.Join("..", "deploy", "testnet", testCase.name))
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		hash, err := policy.HashHex()
		if err != nil || hash != testCase.hash {
			t.Fatalf("%s: canonical hash %s (%v), want %s", testCase.name, hash, err, testCase.hash)
		}
		if policy.IsZeroPrice() || policy.Deposit.ZeroRateAction != "" || policy.Deposit.Unit != DepositUnitRaoPerGiB {
			t.Fatalf("%s: priced testnet policy misread as zero price: %+v", testCase.name, policy.Deposit)
		}
		canonical, err := policy.CanonicalBytes()
		if err != nil || strings.Contains(string(canonical), "rate_numerator_rao_per_user") || strings.Contains(string(canonical), "zero_rate_action") {
			t.Fatalf("%s: optional fields leaked into canonical bytes: %v", testCase.name, err)
		}
	}
}

func zeroPriceTiers() []DepositTier {
	return []DepositTier{
		{MinConvictionRao: 0, RateDenominator: 1},
		{MinConvictionRao: 1_000_000_000, RateDenominator: 1},
	}
}

func TestPolicyZeroPriceIsExplicit(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		unit      string
		action    string
		tiers     []DepositTier
		wantError string
		zeroPrice bool
	}{
		{name: "all-zero with equal_demand", unit: DepositUnitRaoPerGiB, action: DepositZeroRateEqualDemand, tiers: zeroPriceTiers(), zeroPrice: true},
		{name: "all-zero with both units", unit: DepositUnitRaoPerGiBAndUser, action: DepositZeroRateEqualDemand, tiers: zeroPriceTiers(), zeroPrice: true},
		{name: "all-zero without action", unit: DepositUnitRaoPerGiB, tiers: zeroPriceTiers(), wantError: "zero_rate_action must be equal_demand"},
		{name: "all-zero with explicit halt", unit: DepositUnitRaoPerGiB, action: DepositZeroRateHalt, tiers: zeroPriceTiers(), wantError: "zero_rate_action must be equal_demand"},
		{name: "unknown action", unit: DepositUnitRaoPerGiB, action: "ignore", tiers: zeroPriceTiers(), wantError: "unsupported deposit zero_rate_action"},
		{name: "mixed zero tier", unit: DepositUnitRaoPerGiB, action: DepositZeroRateEqualDemand, tiers: []DepositTier{{RateNumeratorRaoPerGiB: 10, RateDenominator: 1}, {MinConvictionRao: 1, RateDenominator: 1}}, wantError: "per-GiB rate is zero in some tiers only"},
		{name: "mixed zero baseline", unit: DepositUnitRaoPerGiB, action: DepositZeroRateEqualDemand, tiers: []DepositTier{{RateDenominator: 1}, {MinConvictionRao: 1, RateNumeratorRaoPerGiB: 10, RateDenominator: 1}}, wantError: "per-GiB rate is zero in some tiers only"},
		{name: "zero denominator", unit: DepositUnitRaoPerGiB, action: DepositZeroRateEqualDemand, tiers: []DepositTier{{RateDenominator: 0}}, wantError: "zero rate denominator"},
		{name: "equal_demand is latent on a priced schedule", unit: DepositUnitRaoPerGiB, action: DepositZeroRateEqualDemand, tiers: []DepositTier{{RateNumeratorRaoPerGiB: 10, RateDenominator: 1}}},
		{name: "users priced under rao_per_gib", unit: DepositUnitRaoPerGiB, tiers: []DepositTier{{RateNumeratorRaoPerGiB: 10, RateNumeratorRaoPerUser: 1, RateDenominator: 1}}, wantError: "prices users but unit is rao_per_gib"},
		{name: "users priced in every tier", unit: DepositUnitRaoPerGiBAndUser, tiers: []DepositTier{{RateNumeratorRaoPerGiB: 10, RateNumeratorRaoPerUser: 2, RateDenominator: 1}, {MinConvictionRao: 1, RateNumeratorRaoPerGiB: 8, RateNumeratorRaoPerUser: 1, RateDenominator: 1}}},
		{name: "users priced in some tiers only", unit: DepositUnitRaoPerGiBAndUser, tiers: []DepositTier{{RateNumeratorRaoPerGiB: 10, RateNumeratorRaoPerUser: 2, RateDenominator: 1}, {MinConvictionRao: 1, RateNumeratorRaoPerGiB: 8, RateDenominator: 1}}, wantError: "per-user rate is zero in some tiers only"},
		{name: "users only priced", unit: DepositUnitRaoPerGiBAndUser, tiers: []DepositTier{{RateNumeratorRaoPerUser: 2, RateDenominator: 1}}},
		{name: "per-user rate increases with conviction", unit: DepositUnitRaoPerGiBAndUser, tiers: []DepositTier{{RateNumeratorRaoPerGiB: 10, RateNumeratorRaoPerUser: 1, RateDenominator: 1}, {MinConvictionRao: 1, RateNumeratorRaoPerGiB: 8, RateNumeratorRaoPerUser: 2, RateDenominator: 1}}, wantError: "per-user rate increases with conviction"},
		{name: "per-GiB rate increases with conviction", unit: DepositUnitRaoPerGiB, tiers: []DepositTier{{RateNumeratorRaoPerGiB: 10, RateDenominator: 1}, {MinConvictionRao: 1, RateNumeratorRaoPerGiB: 11, RateDenominator: 1}}, wantError: "deposit rate increases with conviction"},
		{name: "tiers out of order", unit: DepositUnitRaoPerGiB, tiers: []DepositTier{{RateNumeratorRaoPerGiB: 10, RateDenominator: 1}, {MinConvictionRao: 5, RateNumeratorRaoPerGiB: 8, RateDenominator: 1}, {MinConvictionRao: 4, RateNumeratorRaoPerGiB: 6, RateDenominator: 1}}, wantError: "not strictly increasing"},
	} {
		policy, err := LoadPolicy(testPolicyPath(t))
		if err != nil {
			t.Fatal(err)
		}
		policy.Deposit.Unit = testCase.unit
		policy.Deposit.ZeroRateAction = testCase.action
		policy.Deposit.Tiers = testCase.tiers
		err = policy.Validate()
		if testCase.wantError != "" {
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Errorf("%s: error %v, want %q", testCase.name, err, testCase.wantError)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: valid policy rejected: %v", testCase.name, err)
			continue
		}
		if policy.IsZeroPrice() != testCase.zeroPrice {
			t.Errorf("%s: IsZeroPrice = %t, want %t", testCase.name, policy.IsZeroPrice(), testCase.zeroPrice)
		}
		if _, err := ParsePolicy(mustYAML(t, policy)); err != nil {
			t.Errorf("%s: round trip through YAML failed: %v", testCase.name, err)
		}
	}
}

func mustYAML(t *testing.T, policy *Policy) []byte {
	t.Helper()
	encoded, err := yaml.Marshal(struct {
		Policy Policy `yaml:"policy"`
	}{Policy: *policy})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestDepositTierAtAcceptsZeroPriceScheduleOnly(t *testing.T) {
	policy := DepositPolicy{EpochCapRaoPerOperator: 1, Tiers: zeroPriceTiers()}
	tier, err := DepositTierAt(policy, big.NewInt(2_000_000_000))
	if err != nil || tier.MinConvictionRao != 1_000_000_000 || !tier.IsZeroRate() {
		t.Fatalf("zero-price tier = %+v, %v", tier, err)
	}
	policy.Tiers[1].RateNumeratorRaoPerGiB = 5
	if _, err := DepositTierAt(policy, big.NewInt(0)); err == nil {
		t.Fatal("schedule with a zero rate in some tiers only was accepted")
	}
	if _, err := DepositTierAt(DepositPolicy{}, big.NewInt(0)); err == nil {
		t.Fatal("empty schedule was accepted")
	}
}

func TestRequiredDepositRaoPricesBytesAndUsers(t *testing.T) {
	const gib = uint64(1) << 30
	priced := DepositPolicy{EpochCapRaoPerOperator: 1_000_000, Tiers: []DepositTier{
		{MinConvictionRao: 0, RateNumeratorRaoPerGiB: 1000, RateNumeratorRaoPerUser: 7, RateDenominator: 1},
		{MinConvictionRao: 100, RateNumeratorRaoPerGiB: 500, RateNumeratorRaoPerUser: 3, RateDenominator: 2},
	}}
	zero := DepositPolicy{EpochCapRaoPerOperator: 1_000_000, Tiers: zeroPriceTiers()}
	for _, testCase := range []struct {
		name       string
		policy     DepositPolicy
		bytes      uint64
		users      uint64
		conviction int64
		want       uint64
	}{
		{name: "nothing", policy: priced, want: 0},
		{name: "bytes only", policy: priced, bytes: 3 * gib / 2, want: 1500},
		{name: "bytes floor", policy: priced, bytes: gib/1000 + 1, want: 1},
		{name: "users only", policy: priced, users: 3, want: 21},
		{name: "both", policy: priced, bytes: 2 * gib, users: 10, want: 2070},
		{name: "one floor over the exact sum", policy: priced, bytes: gib / 2, users: 1, conviction: 100, want: 126},
		{name: "discounted tier", policy: priced, bytes: 2 * gib, users: 10, conviction: 150, want: 515},
		{name: "cap", policy: priced, bytes: 5000 * gib, users: 1, want: 1_000_000},
		{name: "users cap", policy: priced, users: 1_000_000, want: 1_000_000},
		{name: "zero price bytes", policy: zero, bytes: 5000 * gib, want: 0},
		{name: "zero price users", policy: zero, users: 1_000_000, want: 0},
		{name: "zero price both discounted tier", policy: zero, bytes: gib, users: 5, conviction: 1_000_000_000, want: 0},
	} {
		amount, _, err := RequiredDepositRao(testCase.bytes, testCase.users, big.NewInt(testCase.conviction), testCase.policy)
		if err != nil || amount.Uint64() != testCase.want {
			t.Errorf("%s: required = %v (%v), want %d", testCase.name, amount, err, testCase.want)
		}
	}
	// The exact uncapped demand carries the fractional part the floor drops.
	demand := DepositDemandRao(gib/2, 1, priced.Tiers[1])
	if demand == nil || demand.Cmp(big.NewRat(253, 2)) != 0 {
		t.Fatalf("exact tier demand = %v, want 253/2", demand)
	}
	if DepositDemandRao(1, 1, DepositTier{}) != nil {
		t.Fatal("zero denominator produced a demand")
	}
}

func TestPolicyJSONSchemaDeclaresPerUserRateAndZeroRateAction(t *testing.T) {
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
	deposit := schema.Definitions["deposit"]
	for _, name := range deposit.Required {
		if name == "zero_rate_action" {
			t.Fatal("zero_rate_action must stay optional so existing policies validate unchanged")
		}
	}
	var action struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(deposit.Properties["zero_rate_action"], &action); err != nil || len(action.Enum) != 2 || action.Enum[0] != DepositZeroRateHalt || action.Enum[1] != DepositZeroRateEqualDemand {
		t.Fatalf("zero_rate_action schema = %s (%v)", deposit.Properties["zero_rate_action"], err)
	}
	var tiers struct {
		Items struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"items"`
	}
	if err := json.Unmarshal(deposit.Properties["tiers"], &tiers); err != nil || len(tiers.Items.Properties["rate_numerator_rao_per_user"]) == 0 {
		t.Fatalf("tier schema lacks rate_numerator_rao_per_user: %v", err)
	}
	for _, name := range tiers.Items.Required {
		if name == "rate_numerator_rao_per_user" {
			t.Fatal("rate_numerator_rao_per_user must stay optional (absent reads as zero)")
		}
	}
	var unit struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(deposit.Properties["unit"], &unit); err != nil || len(unit.Enum) != 2 || unit.Enum[0] != DepositUnitRaoPerGiB || unit.Enum[1] != DepositUnitRaoPerGiBAndUser {
		t.Fatalf("unit schema = %s (%v)", deposit.Properties["unit"], err)
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

// A complete first document does not make a malformed second one EOF.
func TestPolicyRejectsMalformedTrailingYAML(t *testing.T) {
	fixture, err := os.ReadFile(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := ParsePolicy(fixture)
	if err != nil {
		t.Fatal(err)
	}
	wantHash, err := policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name, suffix, wantError string
	}{
		{name: "valid"},
		{name: "comment", suffix: "\n# permitted trailing comment\n"},
		{name: "explicit document end", suffix: "\n...\n"},
		{name: "second document", suffix: "\n---\n{}\n", wantError: "multiple YAML documents"},
		{name: "incomplete mapping", suffix: "\n---\n{\n", wantError: "trailing YAML"},
		{name: "incomplete sequence", suffix: "\n---\n[unterminated\n", wantError: "trailing YAML"},
		{name: "invalid escape", suffix: "\n---\n\"\\q\"\n", wantError: "unknown escape"},
	} {
		wire := append(append([]byte(nil), fixture...), testCase.suffix...)
		got, err := ParsePolicy(wire)
		if testCase.wantError != "" {
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) || got != nil {
				t.Errorf("%s: policy present=%t error=%v, want %q", testCase.name, got != nil, err, testCase.wantError)
			}
			continue
		}
		if err != nil || got == nil {
			t.Fatalf("%s: valid policy rejected: %v", testCase.name, err)
		}
		gotHash, err := got.HashHex()
		if err != nil || gotHash != wantHash {
			t.Errorf("%s: policy changed: hash=%s error=%v", testCase.name, gotHash, err)
		}
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
		func(v *Policy) { v.Settlement.FinalizeOffsetBlocks = v.Settlement.EpochBlocks },
		func(v *Policy) { v.ProductionCadence.AfterAcceleratedEpochs = 0 },
		func(v *Policy) { v.ProductionCadence.EpochBlocks = v.Settlement.EpochBlocks },
		func(v *Policy) { v.ProductionCadence.FinalizeOffsetBlocks = v.ProductionCadence.EpochBlocks },
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

func TestPolicyRejectsOverflowingClaimRetentionEpochCount(t *testing.T) {
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Settlement.ClaimTTLEpochs = ^uint64(0)
	p.Settlement.ClaimGraceEpochs = 1
	if err := p.Validate(); err == nil {
		t.Fatal("overflowing claim-retention epoch count was accepted")
	}
}

func TestPolicyRejectsOverflowingProductionClaimBlockHorizon(t *testing.T) {
	p, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	p.Settlement.ClaimTTLEpochs = ^uint64(0)/p.ProductionCadence.EpochBlocks + 1
	p.Settlement.ClaimGraceEpochs = 0
	if p.Settlement.EpochBlocks > ^uint64(0)/p.Settlement.ClaimTTLEpochs {
		t.Fatal("fixture also overflows the accelerated horizon")
	}
	if err := p.Validate(); err == nil {
		t.Fatal("overflowing production claim block horizon was accepted")
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

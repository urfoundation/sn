// The governed rate correction preserves exact arithmetic and rejects every
// adjacent policy change, including a changed spending ceiling.
package protocol

import (
	"encoding/json"
	"math/big"
	"testing"
)

// Clone through the canonical document so table mutations cannot alias tiers.
func testRateAmendmentPolicies(t *testing.T) (*Policy, *Policy) {
	t.Helper()
	previous, err := LoadPolicy(testPolicyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	var next Policy
	if err := json.Unmarshal(wire, &next); err != nil {
		t.Fatal(err)
	}
	next.PolicyID++
	for index := range next.Deposit.Tiers {
		next.Deposit.Tiers[index].RateNumeratorRaoPerGiB *= 40_000
	}
	return previous, &next
}

// Exact rates remain uncapped at a small completed epoch, with at least two
// native movement floors in every tier at a synthetic conservative price.
func TestTestnetRateAmendmentExactTierEconomics(t *testing.T) {
	previous, next := testRateAmendmentPolicies(t)
	if err := ValidateTestnetRateAmendment(previous, next); err != nil {
		t.Fatal(err)
	}
	before, _ := previous.HashHex()
	after, _ := next.HashHex()
	if before == after {
		t.Fatal("amendment did not change policy commitment")
	}
	for _, tier := range next.Deposit.Tiers {
		amount, selected, err := RequiredDepositRao(24*1024*1024, new(big.Int).SetUint64(tier.MinConvictionRao), next.Deposit)
		if err != nil || selected != tier {
			t.Fatalf("tier changed: %+v %v", selected, err)
		}
		tao := new(big.Int).Mul(new(big.Int).Add(amount, big.NewInt(2)), big.NewInt(500_000_000_000_000))
		tao.Quo(tao, big.NewInt(1_000_000_000_000_000_000))
		if tao.Cmp(big.NewInt(2*100_000)) < 0 {
			t.Fatalf("tier lacks native minimum headroom: %s", tao)
		}
	}
	if next.Deposit.EpochCapRaoPerOperator != previous.Deposit.EpochCapRaoPerOperator || next.Deposit.TotalTestCampaignCapRao != previous.Deposit.TotalTestCampaignCapRao {
		t.Fatal("amendment increased custody bounds")
	}
}

// None of the rate correction's authority extends to other policy dimensions.
func TestTestnetRateAmendmentRejectsAdjacentPolicyChanges(t *testing.T) {
	for _, mutate := range []func(*Policy){
		func(p *Policy) { p.PolicyID++ },
		func(p *Policy) { p.EffectiveEpoch++ },
		func(p *Policy) { p.Deposit.EpochCapRaoPerOperator++ },
		func(p *Policy) { p.Deposit.TotalTestCampaignCapRao++ },
		func(p *Policy) { p.Deposit.Tiers[1].MinConvictionRao++ },
		func(p *Policy) { p.Deposit.Tiers[1].RateDenominator++ },
		func(p *Policy) { p.Deposit.Tiers[1].RateNumeratorRaoPerGiB++ },
		func(p *Policy) { p.Deposit.UsageLagEpochs++ },
		func(p *Policy) { p.Verify.ReliabilityAMin++ },
		func(p *Policy) { p.Settlement.RootCommitWindowBlocks++ },
		func(p *Policy) { p.Steering.Theta.Numerator++ },
		func(p *Policy) { p.NetworkProfile = "mainnet" },
	} {
		previous, next := testRateAmendmentPolicies(t)
		mutate(next)
		if err := ValidateTestnetRateAmendment(previous, next); err == nil {
			t.Fatal("foreign policy change gained rate amendment authority")
		}
	}
	previous, next := testRateAmendmentPolicies(t)
	if ValidateTestnetRateAmendment(nil, next) == nil || ValidateTestnetRateAmendment(previous, nil) == nil || ValidateTestnetRateAmendment(next, previous) == nil {
		t.Fatal("missing or reversed transition accepted")
	}
}

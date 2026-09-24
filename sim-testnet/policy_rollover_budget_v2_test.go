package main

import (
	"reflect"
	"testing"
)

func TestPolicyRolloverLifetimeBudgetPreservesEveryExistingAllocation(t *testing.T) {
	base := &SetupPlan{
		MaximumSpend:    Spend{TAORao: 367125440000, EVMGasWei: "334230700000000000000", AlphaRao: 46498306443274, Registrations: 259},
		SupersededSpend: Spend{EVMGasWei: "13100500000000000000", AlphaRao: 501693556726, Registrations: 3},
		Limits:          Spend{TAORao: 512000000000, EVMGasWei: "512000000000000000000", AlphaRao: 47000000000000, Registrations: 262},
		Actions:         []Action{{ID: "campaign.evm-gas-reserve", Kind: "budget-reserve", Spend: Spend{EVMGasWei: "10536750000000000000"}}},
	}
	p := &policyRolloverPlanV2{MaximumGasWei: "400000000000000000"}
	liability := DecimalUint("23166872726276440891")
	before := *base
	if err := validatePolicyRolloverLifetimeBudgetV2(base, p, liability); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, *base) {
		t.Fatal("rollover rewrote existing budget allocations")
	}
	for _, dimension := range []string{"evm", "tao", "alpha", "registration"} {
		t.Run(dimension, func(t *testing.T) {
			changed := *base
			switch dimension {
			case "evm":
				changed.Limits.EVMGasWei = "370898072726276440890"
			case "tao":
				changed.Limits.TAORao = 390692312726
			case "alpha":
				changed.Limits.AlphaRao--
			case "registration":
				changed.Limits.Registrations--
			}
			if err := validatePolicyRolloverLifetimeBudgetV2(&changed, p, liability); err == nil {
				t.Fatal("rollover exceeded a retained lifetime cap")
			}
		})
	}
}

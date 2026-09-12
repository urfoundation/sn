package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFleetRenewalRevisionPreservesApprovedRoundsAndChargesOnce(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	prior, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(prior)
	original := prior
	roles, err := derivePublicRoles(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 2; iteration++ {
		revised, err := buildPlan(fixture.cfg, testSetupFacts(), roles, time.Unix(int64(iteration+2), 0))
		if err != nil {
			t.Fatal(err)
		}
		revised.PriorPlanHashes = append(append([]string(nil), prior.PriorPlanHashes...), prior.PlanHash)
		if err := carryFleetRenewalRevision(revised, prior); err != nil {
			t.Fatalf("retain renewal round on revision%d: %v", iteration, err)
		}
		if !finalJSONEqual(revised.FleetRenewals, prior.FleetRenewals) || !finalJSONEqual(revised.FleetLifecycleRenewal, prior.FleetLifecycleRenewal) {
			t.Fatal("revision changed exact approved round or successor generation")
		}
		for _, action := range prior.Actions {
			if isFleetRenewalAction(action) || fleetLifecycleRenewalFutureAction(action.ID) {
				if !finalJSONEqual(action, actionByID(t, revised, action.ID)) {
					t.Fatalf("revision rewrote approved action %s", action.ID)
				}
			}
		}
		if revised.MaximumSpend != prior.MaximumSpend || actionByID(t, revised, "campaign.evm-gas-reserve").Spend != actionByID(t, prior, "campaign.evm-gas-reserve").Spend {
			t.Fatal("revision dropped or double-charged a renewal liability")
		}
		revised.PlanHash, err = revised.hash()
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(revised)
		if _, err := decodePersistedPlanBytes(raw); err != nil {
			t.Fatalf("persist renewed revision: %v", err)
		}
		prior = revised
	}
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("revision mutated its original approved plan")
	}
	// The full revision entry point must use the same carry, not only its helper.
	current := testSetupFacts()
	revised, err := buildPlanRevisionFromFacts(fixture.cfg, fixture.stateDir, prior, current, nil, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("full renewed revision: %v", err)
	}
	if len(revised.FleetRenewals) != 1 || !finalJSONEqual(revised.FleetLifecycleRenewal, prior.FleetLifecycleRenewal) || !finalJSONEqual(actionByID(t, revised, "fleet.renew.1.5.bind.1"), actionByID(t, prior, "fleet.renew.1.5.bind.1")) {
		t.Fatal("full revision omitted the approved renewal carry")
	}
}

func TestFleetRenewalRevisionRefusesCustodyFeeOrLiabilityChanges(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	prior, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := derivePublicRoles(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*SetupPlan)
	}{
		{"native fee", func(p *SetupPlan) { p.NativeTransactionFeeLimitRao++ }},
		{"custody", func(p *SetupPlan) { p.Deployment.CoordinatorProxy[0] ^= 1 }},
		{"gas capacity", func(p *SetupPlan) {
			for i := range p.Actions {
				if p.Actions[i].ID == "campaign.evm-gas-reserve" {
					p.Actions[i].Spend.EVMGasWei = "1"
				}
			}
		}},
		{"missing successor", func(p *SetupPlan) {
			for i, a := range p.Actions {
				if a.ID == "lifecycle.provider.commitment" {
					p.Actions = append(p.Actions[:i], p.Actions[i+1:]...)
					return
				}
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			revised, err := buildPlan(fixture.cfg, testSetupFacts(), roles, time.Unix(2, 0))
			if err != nil {
				t.Fatal(err)
			}
			revised.PriorPlanHashes = append(append([]string(nil), prior.PriorPlanHashes...), prior.PlanHash)
			test.change(revised)
			if err := carryFleetRenewalRevision(revised, prior); err == nil {
				t.Fatal("accepted a changed renewal custody, envelope or liability")
			}
		})
	}
}

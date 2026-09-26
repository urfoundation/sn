// The actor and honest validators share validator identities but must avoid
// intentionally filtered fleet members when testing valid signed walks.
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// Reproduce both operator-local lifecycle exclusions using the production
// membership builder. Neighbor fleets remain eligible and received 503s remain
// blocking even while a view fault is active.
func TestVerifyProviderFaultSelectionScopesLifecycleViews(t *testing.T) {
	cfg := testResolvedConfig(t)
	for _, operator := range []int{1, 2} {
		fleet := fleetLifecycleTargetFleet
		if operator == 2 {
			fleet = fleetLifecycleCompanionFleet
		}
		actor := &verifyAdversary{cfg: cfg, faults: newAdversaryFaultWindow(time.Second), seedProviders: map[int][]connect.Id{}, providerSources: map[int]map[connect.Id]string{operator: {}}, providerTargetKVs: map[connect.Id][]string{}}
		for _, candidateFleet := range []int{fleet, fleet + cfg.Config.Topology.Operators} {
			for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
				miner := fleetMemberMinerIndex(cfg, candidateFleet, member)
				provider := connect.Id{byte(miner)}
				targets, err := adversaryVerifyProviderFaultTargets(cfg, miner)
				if err != nil {
					t.Fatal(err)
				}
				if operatorForMiner(cfg, miner) != operator {
					t.Fatal("synthetic provider belongs to the wrong operator")
				}
				actor.seedProviders[operator] = append(actor.seedProviders[operator], provider)
				actor.providerSources[operator][provider] = minerTestEgressSourceIP(miner)
				actor.providerTargetKVs[provider] = targets
			}
		}
		actor.faults.Update([]string{lifecycleValidatorViewFaultTarget(operator)})
		selected, err := actor.unaffectedSeedProvider(operator, 0)
		want := connect.Id{byte(fleetMemberMinerIndex(cfg, fleet+cfg.Config.Topology.Operators, 1))}
		if err != nil || selected != want {
			t.Fatalf("operator=%d selected excluded fleet member: got=%s want=%s err=%v", operator, selected, want, err)
		}
		if actor.sampleError(operator, adversaryVerifyHttpFailure("valid verify SEED", http.StatusServiceUnavailable, nil), 2, 1).Outcome != adversaryOutcomeError {
			t.Fatal("view fault waived an observed HTTP503")
		}
	}
}

// A validator-local view is scoped to its VPK. This actor uses validator-2, so
// the existing validator-1 boundary fault must not remove an unaffected route.
func TestVerifyProviderFaultSelectionRetainsOtherValidatorView(t *testing.T) {
	cfg := testResolvedConfig(t)
	miner := fleetMemberMinerIndex(cfg, validatorLocalHeadBoundaryFleet, 1)
	provider := connect.Id{byte(miner)}
	operator := operatorForMiner(cfg, miner)
	targets, err := adversaryVerifyProviderFaultTargets(cfg, miner)
	if err != nil {
		t.Fatal(err)
	}
	actor := &verifyAdversary{faults: newAdversaryFaultWindow(time.Second), seedProviders: map[int][]connect.Id{operator: {provider}}, providerSources: map[int]map[connect.Id]string{operator: {provider: minerTestEgressSourceIP(miner)}}, providerTargetKVs: map[connect.Id][]string{provider: targets}}
	actor.faults.Update([]string{validatorViewFaultTarget(operator, 1), lifecycleValidatorViewFaultTarget(operator)})
	if got, err := actor.unaffectedSeedProvider(operator, 0); err != nil || got != provider {
		t.Fatalf("unrelated validator/fleet view changed route: %s %v", got, err)
	}
}

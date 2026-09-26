// Signed valid walks use provisioned providers outside the exact fault targets.
// Selection skips never turn a received semantic failure into accepted evidence.
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/urnetwork/connect"
)

// Derive membership from the same topology and fleet constants used by the
// fault driver. Fleet lifecycle views exclude only their four declared members;
// the validator-1-only boundary view does not affect this validator-2 actor.
func adversaryVerifyProviderFaultTargets(cfg *ResolvedConfig, miner int) ([]string, error) {
	swarm, err := minerSwarmFor(cfg, miner)
	if err != nil {
		return nil, err
	}
	operator := operatorForMiner(cfg, miner)
	targets := []string{fmt.Sprintf("miner-%d", miner), fmt.Sprintf("miner-swarm-%d", swarm), fmt.Sprintf("operator-%d-connect", operator)}
	if cfg.Config.Topology.ClientsPerHeadFleet <= 0 {
		return nil, errors.New("verify provider fault topology has no fleet members")
	}
	if miner <= cfg.Config.Topology.fleetCandidateMiners() {
		fleet := 1 + (miner-1)/cfg.Config.Topology.ClientsPerHeadFleet
		if fleet == fleetLifecycleTargetFleet || fleet == fleetLifecycleCompanionFleet {
			targets = append(targets, lifecycleValidatorViewFaultTarget(operator))
		}
		if fleet == validatorLocalHeadBoundaryFleet && validatorLocalHeadBoundaryValidator == 2 {
			targets = append(targets, validatorViewFaultTarget(operator, 2))
		}
	}
	return targets, nil
}

// Acquires an admission lease without holding the fault-state lock across I/O.
// The sole verify actor holds this only inside its bounded signed walk. Update
// drains the lease before publishing targets, which precedes actual fault apply.
func (self *adversaryFaultWindow) reserveWalk(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if self == nil {
		return func() {}, nil
	}
	select {
	case <-self.walkAdmission:
	default:
		if self.beforeWalkWaitForTest != nil {
			self.beforeWalkWaitForTest()
		}
		select {
		case <-self.walkAdmission:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var once sync.Once
	release := func() { once.Do(func() { self.walkAdmission <- struct{}{} }) }
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// Exists only before a request is issued. It cannot wrap response errors, so
// signatures, source mismatches, statuses, and joined failures stay blocking.
type adversaryVerifyRouteUnavailable struct {
	operator int
	provider connect.Id
	target   string
	stage    string
}

// The route and reason remain visible without claiming a completed sample.
func (self *adversaryVerifyRouteUnavailable) Error() string {
	return fmt.Sprintf("verify route skipped before request: operator=%d provider=%s target=%s stage=%s", self.operator, self.provider, self.target, self.stage)
}

// Scope comes from provisioned roles and process ownership, never from a
// failed response or broad health signal. Restoration has only existing grace.
func (self *verifyAdversary) providerFaultTarget(provider connect.Id) string {
	for _, target := range self.providerTargetKVs[provider] {
		if self.faults.Expected(target) {
			return target
		}
	}
	return ""
}

// Keep the deterministic rotation while moving past unavailable members.
// Exhaustion is an explicit skipped sample and contributes no success metric.
func (self *verifyAdversary) unaffectedSeedProvider(operator int, sequence uint64) (connect.Id, error) {
	providers := self.seedProviders[operator]
	if len(providers) == 0 {
		return connect.Id{}, errors.New("operator has no adversarial seed provider")
	}
	start := int(sequence % uint64(len(providers)))
	for offset := range providers {
		provider := providers[(start+offset)%len(providers)]
		if self.providerSources[operator][provider] == "" {
			return connect.Id{}, fmt.Errorf("provisioned verify seed provider %s has no source", provider)
		}
		if self.providerFaultTarget(provider) == "" {
			return provider, nil
		}
	}
	return connect.Id{}, &adversaryVerifyRouteUnavailable{operator: operator, stage: "SEED", target: "all provisioned providers affected"}
}

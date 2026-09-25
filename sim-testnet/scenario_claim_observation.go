// Claim progress retains lifetime diagnostics while acceptance reads only the
// exact settlement epochs in its signed window. Missing coverage stays a failure.
package main

import (
	"errors"
	"fmt"
)

// Only epochs present in the contract snapshot are projected. An absent queue
// entry is explicit; collecting an observation never invents a claim outcome.
type ClaimEpochObservation struct {
	Epoch  uint64 `json:"epoch"`
	Status string `json:"status"`
}

// Recomputes counters from exact observed epoch rows, never from lifetime
// successes or failures. Legacy scenarios without a window keep their old scope.
func scenarioClaimsForAcceptance(e *scenarioEvaluation) ([]ClaimObservation, error) {
	if e == nil || e.Current == nil {
		return nil, errors.New("claim acceptance observation is unavailable")
	}
	if e.Window == nil {
		if e.Definition.Name == "release-1.0" || e.Definition.Name == "production-soak" {
			return nil, errors.New("release claim acceptance requires its signed epoch window")
		}
		return e.Current.Claims, nil
	}
	if e.Cfg == nil || e.Cfg.Config == nil || e.Window.EpochCount == 0 {
		return nil, errors.New("claim acceptance window or configuration is unavailable")
	}
	end, ok := checkedAdd(e.Window.FirstEpoch, e.Window.EpochCount)
	if !ok {
		return nil, errors.New("claim acceptance epoch window overflows")
	}
	if len(e.Current.Claims) != e.Cfg.Config.Topology.Miners {
		return nil, fmt.Errorf("claim acceptance miner census=%d want=%d", len(e.Current.Claims), e.Cfg.Config.Topology.Miners)
	}
	seenMiners := map[int]bool{}
	claims := make([]ClaimObservation, 0, len(e.Current.Claims))
	for _, claim := range e.Current.Claims {
		if claim.MinerID < 1 || claim.MinerID > e.Cfg.Config.Topology.Miners || seenMiners[claim.MinerID] || claim.NoID != operatorForMiner(e.Cfg, claim.MinerID) {
			return nil, fmt.Errorf("claim acceptance miner %d has a duplicate or wrong operator identity", claim.MinerID)
		}
		seenMiners[claim.MinerID] = true
		if claim.Error != "" {
			return nil, fmt.Errorf("claim acceptance miner %d: %s", claim.MinerID, claim.Error)
		}
		if len(claim.EpochOutcomes) == 0 {
			return nil, fmt.Errorf("claim acceptance miner %d scoped epoch evidence is unavailable in the retained observation; lifetime counters do not prove discovery or outcomes in [%d,%d)", claim.MinerID, e.Window.FirstEpoch, end)
		}
		if claim.LastDiscovered < 0 || uint64(claim.LastDiscovered) < end-1 || uint64(len(claim.EpochOutcomes)) < e.Window.EpochCount {
			return nil, fmt.Errorf("claim acceptance miner %d has not discovered complete window [%d,%d): last=%d observed=%d", claim.MinerID, e.Window.FirstEpoch, end, claim.LastDiscovered, len(claim.EpochOutcomes))
		}
		outcomes := map[uint64]string{}
		for _, outcome := range claim.EpochOutcomes {
			if _, duplicate := outcomes[outcome.Epoch]; duplicate {
				return nil, fmt.Errorf("claim acceptance miner %d has duplicate epoch %d", claim.MinerID, outcome.Epoch)
			}
			outcomes[outcome.Epoch] = outcome.Status
		}
		scoped := ClaimObservation{MinerID: claim.MinerID, NoID: claim.NoID, LastDiscovered: claim.LastDiscovered}
		for epoch := e.Window.FirstEpoch; epoch < end; epoch++ {
			status, exists := outcomes[epoch]
			if !exists || status == "undiscovered" {
				return nil, fmt.Errorf("claim acceptance miner %d has no observed queue entry for epoch %d", claim.MinerID, epoch)
			}
			scoped.Discovered++
			switch status {
			case "finalized":
				scoped.Finalized++
			case "no-claim":
				scoped.NoClaim++
			case "pending", "retry":
				scoped.Pending++
			case "submitting", "uncertain":
				scoped.Uncertain++
			case "failed":
				scoped.Failed++
			default:
				return nil, fmt.Errorf("claim acceptance miner %d epoch %d has unknown status %q", claim.MinerID, epoch, status)
			}
		}
		claims = append(claims, scoped)
	}
	return claims, nil
}

// Historical claim repair remains in immutable observations. The acceptance
// anomaly verdict describes the exact signed settlement window only.
package main

import "fmt"

// Pending and submitting are normal intermediate states, not failed outcomes.
// Terminal coverage checks still reject unfinished submissions. An uncertain or
// failed current claim stays open even if a later queue status claims success.
func (self *anomalyCollector) addClaimWindowIncidents(claim ClaimObservation, window *ScenarioAcceptanceWindow, observation *ScenarioObservation) {
	source := fmt.Sprintf("claim:min%d:no%d", claim.MinerID, claim.NoID)
	if window == nil {
		if claim.Uncertain != 0 || claim.Failed != 0 {
			self.add("claim-terminal-state", "critical", source, fmt.Sprintf("uncertain=%d failed=%d", claim.Uncertain, claim.Failed), observation.ObservedAt)
		}
		return
	}
	end, ok := checkedAdd(window.FirstEpoch, window.EpochCount)
	if !ok || window.EpochCount == 0 {
		self.add("claim-observation-gap", "critical", source, "invalid claim anomaly epoch window", observation.ObservedAt)
		return
	}
	if len(claim.EpochOutcomes) == 0 && observation.Status != nil && observation.Status.Contracts != nil && observation.Status.Contracts.CurrentEpoch > window.FirstEpoch {
		self.add("claim-observation-gap", "critical", source, "current-window claim epoch observations are unavailable", observation.ObservedAt)
	}
	seenEpochs := map[uint64]bool{}
	for _, outcome := range claim.EpochOutcomes {
		if seenEpochs[outcome.Epoch] {
			self.add("claim-observation-gap", "critical", source, fmt.Sprintf("duplicate claim observation epoch %d", outcome.Epoch), observation.ObservedAt)
			continue
		}
		seenEpochs[outcome.Epoch] = true
		if outcome.Epoch < window.FirstEpoch || outcome.Epoch >= end {
			continue
		}
		switch outcome.Status {
		case "uncertain", "failed":
			self.add("claim-terminal-state", "critical", source, fmt.Sprintf("epoch=%d status=%s", outcome.Epoch, outcome.Status), observation.ObservedAt)
		case "pending", "retry", "submitting", "finalized", "no-claim", "undiscovered":
		default:
			self.add("claim-observation-gap", "critical", source, fmt.Sprintf("unknown claim status %q at epoch %d", outcome.Status, outcome.Epoch), observation.ObservedAt)
		}
	}
}

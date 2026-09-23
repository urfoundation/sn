// Complete-epoch scenarios collect progress until their exact terminal block.
// Interrupted runs retain their real cause without evaluating future gates.
package main

import (
	"fmt"
	"time"
)

// Epoch advancement alone is insufficient: the final settlement offset must
// also be finalized. Non-window scenarios keep their existing check loop.
func scenarioAcceptanceIntervalObserved(window *ScenarioAcceptanceWindow, observation *ScenarioObservation) bool {
	if window == nil {
		return true
	}
	if observation == nil || observation.Status == nil || observation.Status.Contracts == nil || window.EpochCount == 0 || window.TerminalBlock == 0 {
		return false
	}
	endEpoch, ok := checkedAdd(window.FirstEpoch, window.EpochCount)
	contracts := observation.Status.Contracts
	return ok && contracts.CurrentEpoch >= endEpoch && contracts.FinalizedHead.Number >= window.TerminalBlock
}

// Pending epochs and faults are progress, not failed terminal assertions.
func evaluateScenarioInterval(cfg *ResolvedConfig, definition scenarioDefinition, start, current *ScenarioObservation, window *ScenarioAcceptanceWindow, faults []ScenarioFaultRecord, started time.Time) []AssertionRecord {
	if !scenarioAcceptanceIntervalObserved(window, current) {
		return nil
	}
	assertions := appendFaultAssertions(evaluateScenario(cfg, definition, start, current, window, started), faults, started, current)
	return appendAcceptanceFaultAssertion(assertions, faults, window, started, current)
}

// A durable interruption remains a failed recovery source, while its pending
// acceptance gates are represented by one explicit incomplete-interval record.
func scenarioInterruptedIntervalAssertion(window *ScenarioAcceptanceWindow, current *ScenarioObservation, started, completed time.Time) AssertionRecord {
	head, epoch, observationHash := uint64(0), uint64(0), ""
	if current != nil {
		observationHash = current.ObservationHash
		if current.Status != nil && current.Status.Contracts != nil {
			head, epoch = current.Status.Contracts.FinalizedHead.Number, current.Status.Contracts.CurrentEpoch
		}
	}
	return AssertionRecord{ID: "acceptance_interval_observed", Passed: false,
		Message:   fmt.Sprintf("acceptance observation interrupted before terminal evaluation: first_epoch=%d epoch_count=%d terminal_block=%d observed_epoch=%d observed_block=%d; pending scenario, fault and adversary gates were not evaluated", window.FirstEpoch, window.EpochCount, window.TerminalBlock, epoch, head),
		StartedAt: started.UTC().Format(time.RFC3339Nano), CompletedAt: completed.UTC().Format(time.RFC3339Nano), DurationSeconds: completed.Sub(started).Seconds(), ObservationHash: observationHash}
}

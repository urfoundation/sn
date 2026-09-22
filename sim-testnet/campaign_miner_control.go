// Signed runtime checkpoints retain incomplete control rounds without treating
// their process census as a completed fault transition.
package main

import "fmt"

// Only miner-control retries may carry partial census/error evidence. The
// schedule, applied/restored boundaries and final fault verdict remain exact.
func validateScenarioCampaignMinerControl(record ScenarioFaultRecord) error {
	started := record.ControlStartedBlock != 0
	if record.Kind != "miner-control" {
		if started || record.ControlStartedBlockHash != "" || record.ControlPendingRounds != 0 {
			return fmt.Errorf("scenario campaign fault %q has foreign miner control progress", record.ID)
		}
		return nil
	}
	if started {
		if record.ControlStartedBlock < record.TriggerBlock || !validCanonicalHashHex(record.ControlStartedBlockHash) || record.ControlPendingRounds == 0 || record.AppliedBlock != 0 && record.ControlStartedBlock > record.AppliedBlock {
			return fmt.Errorf("scenario campaign miner control %q has an invalid pending boundary", record.ID)
		}
	} else if record.ControlStartedBlockHash != "" {
		return fmt.Errorf("scenario campaign miner control %q has a hash without a pending boundary", record.ID)
	}
	if record.Status != "pending" {
		return nil
	}
	if !started {
		if record.ControlPendingRounds != 0 {
			return fmt.Errorf("scenario campaign miner control %q has pending rounds without a boundary", record.ID)
		}
		return nil
	}
	if record.Error == "" || len(record.Processes) != len(record.Targets) {
		return fmt.Errorf("scenario campaign pending miner control %q lacks retry evidence", record.ID)
	}
	targetKVs := make(map[string]bool, len(record.Targets))
	for _, target := range record.Targets {
		targetKVs[target] = true
	}
	for _, process := range record.Processes {
		if !targetKVs[process.ID] || process.Role != "miner" || process.Identity == "" || process.PID <= 1 {
			return fmt.Errorf("scenario campaign pending miner control %q has invalid process evidence", record.ID)
		}
		delete(targetKVs, process.ID)
	}
	return nil
}

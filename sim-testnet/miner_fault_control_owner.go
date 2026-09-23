// Swarm restart readiness is separate from control intent and ownership.
// Wait only for a known owner; no mutation or completed fault is inferred.
package main

import (
	"context"
	"fmt"
	"os"
)

// Only a complete checksum-bound census can produce this readiness result.
// A missing row, malformed state or changed identity never acquires it.
type minerControlOwnerUnavailableError struct{ targets []string }

// Identify the deferred control members without exposing configuration data.
func (self *minerControlOwnerUnavailableError) Error() string {
	return fmt.Sprintf("miner control owning swarm is restarting or unhealthy for %v", self.targets)
}

// A finite admission wait precedes durable first dispatch. Later rounds retain
// their existing exact progress; both paths re-read ownership before control.
func (self *liveScenarioFaultDriver) waitMinerControlProcesses(ctx context.Context, spec scenarioFaultSpec, enable bool) ([]FaultProcessEvidence, error) {
	waitCtx, cancel := context.WithTimeout(ctx, minerControlTargetTimeout)
	defer cancel()
	wait := self.minerControlWait
	if wait == nil {
		wait = waitSupervisorRestart
	}
	var owners []FaultProcessEvidence
	for {
		if err := waitCtx.Err(); err != nil {
			return nil, err
		}
		processes, err := self.minerControlProcesses(spec, enable)
		_, unavailable := err.(*minerControlOwnerUnavailableError)
		if err != nil && !unavailable {
			return nil, err
		}
		if owners != nil {
			if len(processes) != len(owners) {
				return nil, fmt.Errorf("miner control owner census changed while waiting for %s", spec.ID)
			}
			for index, process := range processes {
				owner := owners[index]
				if process.ID != owner.ID || process.Role != owner.Role || process.Identity != owner.Identity {
					return nil, fmt.Errorf("miner control owner identity changed while waiting for %s", process.ID)
				}
			}
		}
		if err == nil {
			return processes, nil
		}
		if owners == nil {
			owners = processes
			fmt.Fprintf(os.Stderr, "sim-testnet: %v; awaiting the same owner within %s before control\n", err, minerControlTargetTimeout)
		}
		if err := wait(waitCtx, minerControlRetryDelay); err != nil {
			return nil, err
		}
	}
}

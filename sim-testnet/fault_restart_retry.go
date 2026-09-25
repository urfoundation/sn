// A recorded restart remains an unfinished transition while its exact child
// exits and its replacement becomes healthy. Readiness is not an integrity error.
package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"syscall"
)

// Only an independently read, checksum-bound supervisor snapshot creates this
// marker. It cannot grant readiness or erase the durable original generation.
type processRestartPendingError struct {
	faultId string
	targets []string
}

// Keep the pending target census visible without reporting a completed restart.
func (self *processRestartPendingError) Error() string {
	return fmt.Sprintf("process restart %s awaiting replacement health: %v", self.faultId, self.targets)
}

// Exact fault ownership and a direct type prevent mixed failures, diagnostic
// text, or another transition from inheriting readiness retry permission.
func processRestartPending(spec scenarioFaultSpec, err error) bool {
	pending, ok := err.(*processRestartPendingError)
	return ok && pending != nil && spec.Kind == "process-restart" && pending.faultId == spec.ID && len(pending.targets) != 0 && slices.Equal(pending.targets, spec.Targets)
}

// One synchronous observation keeps the heartbeat responsive. Repeated calls
// never resend termination; the supervisor owns replacement. All original role
// and identity checks precede interpreting missing health as pending work.
func (self *liveScenarioFaultDriver) observeRestartTargets(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	active, err := readActiveFaultFile(self.activePath())
	if err != nil {
		return nil, err
	}
	if spec.Kind != "process-restart" {
		return nil, errors.New("restart readiness requires an exact process-restart fault")
	}
	if _, err := activeFaultIndex(active, spec); err != nil {
		return nil, err
	}
	prior := make(map[string]FaultProcessEvidence, len(active.Processes))
	for _, process := range active.Processes {
		prior[process.ID] = process
	}
	states, specs, err := self.processSnapshot()
	if err != nil {
		return nil, err
	}
	ready := make([]FaultProcessEvidence, 0, len(spec.Targets))
	for _, id := range spec.Targets {
		state, stateOk := states[id]
		processSpec, specOk := specs[id]
		original, priorOk := prior[id]
		if !stateOk || !specOk || !priorOk || processSpec.Role == "" || processSpec.Identity == "" || state.Role != processSpec.Role || state.Identity != processSpec.Identity || original.Role != processSpec.Role || original.Identity != processSpec.Identity || state.PID < 0 || state.PID == 1 {
			return nil, fmt.Errorf("restart target %s identity differs from its retained owner", id)
		}
		if state.PID == 0 || state.PID == original.PID || !state.Healthy {
			continue
		}
		if err := syscall.Kill(state.PID, syscall.Signal(0)); errors.Is(err, syscall.ESRCH) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("observe restart target %s process: %w", id, err)
		}
		ready = append(ready, FaultProcessEvidence{ID: id, Role: processSpec.Role, Identity: processSpec.Identity, PID: state.PID})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ready) != len(spec.Targets) {
		return nil, &processRestartPendingError{faultId: spec.ID, targets: slices.Clone(spec.Targets)}
	}
	return ready, nil
}

// Cleanup owns its existing finite context; the live controller owns block
// heartbeats. This wait retries only exact pending restoration, checking owner
// cancellation before each read. The wait callback is the deterministic seam.
func waitScenarioFaultRestore(ctx context.Context, spec scenarioFaultSpec, restore func() ([]FaultProcessEvidence, error), wait func() error) ([]FaultProcessEvidence, error) {
	if ctx == nil || restore == nil || wait == nil {
		return nil, errors.New("fault restore retry owner is incomplete")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		processes, err := restore()
		if !(spec.Kind == "miner-control" && minerControlPending(err) || processRestartPending(spec, err) || faultCompletionPending(spec, "enable", err)) {
			return processes, err
		}
		if err := wait(); err != nil {
			return processes, err
		}
	}
}

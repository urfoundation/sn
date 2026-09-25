// A restart is an exact durable request before it becomes a process signal.
// The existing supervisor owns unfinished termination and replacement health.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Capture the complete cohort before the first mutation. The retained start
// ticks authorize only these originals; full in-memory kernel/image/argv
// identity guards each immediate signal against changes after persistence.
func (self *liveScenarioFaultDriver) requestProcessRestart(ctx context.Context, active activeFaultFile, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if spec.Kind != "process-restart" || spec.ID == "" || len(spec.Targets) == 0 {
		return nil, errors.New("process restart intent is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	commands, processes, err := self.captureFaultProcessCommands(ctx, spec)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	persist := self.restartPersist
	if persist == nil {
		persist = appendActiveFault
	}
	// This is also the crash boundary. An exact subsequent Apply adopts the
	// existing intent without repeating any signal. If dispatch is interrupted,
	// the supervisor can finish only the recorded originals after its grace.
	if err := persist(self.activePath(), active, spec, processes); err != nil {
		return nil, err
	}
	signal := self.restartSignal
	if signal == nil {
		signal = func(command supervisedCommand, value syscall.Signal) bool {
			return signalSupervisedCommandWithObserver(command, value, observeSupervisedProcessIdentity, syscall.Kill)
		}
	}
	for _, command := range commands {
		if err := ctx.Err(); err != nil {
			return processes, err
		}
		if !signal(command, syscall.SIGTERM) {
			// Refusal or an already-exited original cannot make the request
			// disappear or authorize a second signal at a replacement. Readiness
			// remains pending until ordinary Restore sees the actual successor.
			fmt.Fprintf(os.Stderr, "sim-testnet: retained process restart %s original %s pid=%d start=%d did not confirm graceful signal; supervisor completion and replacement readiness remain pending\n", spec.ID, command.spec.ID, command.identity.PID, command.identity.StartTimeTicks)
		}
	}
	return processes, nil
}

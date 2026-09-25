// A supervisor's original kernel generation must precede a new signal intent.
// Current numeric PID observations cannot manufacture that original authority.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
)

// Capture all manifest roles and original start ticks before any signal or
// durable restart write. Legacy state remains readable but cannot signal.
func (self *liveScenarioFaultDriver) captureFaultProcessCommands(ctx context.Context, spec scenarioFaultSpec) ([]supervisedCommand, []FaultProcessEvidence, error) {
	if ctx == nil || (spec.Kind != "process-restart" && spec.Kind != "process-pause") || len(spec.Targets) == 0 {
		return nil, nil, errors.New("process fault capture is incomplete")
	}
	states, specs, err := self.processSnapshot()
	if err != nil {
		return nil, nil, err
	}
	targets := slices.Clone(spec.Targets)
	slices.Sort(targets)
	commands := make([]supervisedCommand, 0, len(targets))
	processes := make([]FaultProcessEvidence, 0, len(targets))
	for index, id := range targets {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if index != 0 && targets[index-1] == id {
			return nil, nil, errors.New("process fault repeats a target")
		}
		state, stateOk := states[id]
		processSpec, specOk := specs[id]
		if !stateOk || !specOk || state.PID <= 1 || processSpec.Role == "" || processSpec.Identity == "" || state.Role != processSpec.Role || state.Identity != processSpec.Identity || spec.Kind == "process-restart" && processSpec.RestartLimit < 1 {
			return nil, nil, fmt.Errorf("fault target %s is not an exact manifest process", id)
		}
		if state.StartTimeTicks == 0 {
			return nil, nil, fmt.Errorf("fault target %s has no supervisor-recorded kernel generation; signaling is unavailable", id)
		}
		identity, err := observeSupervisedProcessIdentity(state.PID)
		if err != nil || identity.PID != state.PID || identity.ProcessGroupID != state.PID || identity.StartTimeTicks != state.StartTimeTicks || identity.ExecutableFile.Inode == 0 || identity.CommandLineHash == "" {
			return nil, nil, stateMismatchError(err, "fault target %s kernel generation differs from supervisor state", id)
		}
		commands = append(commands, supervisedCommand{spec: processSpec, cmd: &exec.Cmd{Process: &os.Process{Pid: state.PID}}, identity: identity})
		processes = append(processes, FaultProcessEvidence{ID: id, Role: processSpec.Role, Identity: processSpec.Identity, PID: state.PID, StartTimeTicks: state.StartTimeTicks})
	}
	return commands, processes, nil
}

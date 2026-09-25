// Container restoration reconciles one retained generation in bounded rounds.
// A slow readiness probe keeps the fault active; it never grants completed health.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

const containerRestartRoundTimeout = 5 * time.Second

// Only the exact managed runtime creates this after ownership authentication.
// It describes an incomplete start/readiness round, not an arbitrary failure.
type containerStartPendingError struct {
	name        string
	containerId string
	specHash    string
	cause       error
}

// Retain the actual readiness or lost-reply diagnosis in the signed fault row.
func (self *containerStartPendingError) Error() string {
	return fmt.Sprintf("container %s awaiting readiness: %v", self.name, self.cause)
}

// The same exact-spec marker governs rollback and ordinary restoration.
func containerStartPending(spec managedContainerSpec, err error) bool {
	pending, ok := err.(*containerStartPendingError)
	hash, hashErr := managedContainerSpecHash(spec)
	return ok && pending != nil && pending.name == spec.Name && validSupervisorDependencyHex(pending.containerId) && hashErr == nil && pending.specHash == hash
}

// The scheduler marker binds one exact fault and its complete target census.
type containerRestartPendingError struct {
	faultId string
	targets []string
	detail  string
}

// No restored process evidence accompanies this still-active transition.
func (self *containerRestartPendingError) Error() string {
	return fmt.Sprintf("container restart %s awaiting replacement health: %v: %s", self.faultId, self.targets, self.detail)
}

// Direct typing refuses wrapped, joined, forged-text, and foreign-fault errors.
func containerRestartPending(spec scenarioFaultSpec, err error) bool {
	pending, ok := err.(*containerRestartPendingError)
	return ok && pending != nil && spec.Kind == "container-restart" && pending.faultId == spec.ID && len(pending.targets) != 0 && slices.Equal(pending.targets, spec.Targets)
}

// The original supervisor manifest owns container ids across driver restarts.
// Its checksum-bound state prevents a new same-name container becoming authority.
func (self *liveScenarioFaultDriver) containerDependencies() (map[string]supervisorDependency, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(self.stateDir, "supervisor.json"))
	if err != nil {
		return nil, err
	}
	stateBytes, err := os.ReadFile(filepath.Join(self.stateDir, "supervisor.state.json"))
	if err != nil {
		return nil, err
	}
	var manifest SupervisorFile
	var state SupervisorState
	if decodeStrictJSONBytes(manifestBytes, &manifest) != nil || manifest.Schema != "urnetwork-sim-supervisor-v1" || decodeStrictJSONBytes(stateBytes, &state) != nil || state.Schema != "urnetwork-sim-supervisor-state-v1" {
		return nil, errors.New("container restoration has invalid supervisor ownership")
	}
	hash, err := canonicalHashHex(manifest)
	if err != nil || state.ManifestHash != hash || len(manifest.Dependencies) == 0 {
		return nil, errors.New("container restoration lacks checksum-bound dependency generations")
	}
	if err := validateSupervisorDependencies(manifest.Dependencies); err != nil {
		return nil, err
	}
	dependencyKVs := make(map[string]supervisorDependency, len(manifest.Dependencies))
	for _, dependency := range manifest.Dependencies {
		dependencyKVs[dependency.Name] = dependency
	}
	return dependencyKVs, nil
}

// Config identity may only select the already captured generation of that spec.
func (self *dockerScenarioContainerRuntime) dependency(spec managedContainerSpec) (supervisorDependency, error) {
	dependency, ok := self.dependencyKVs[spec.Name]
	hash, err := managedContainerSpecHash(spec)
	if err != nil || !ok || dependency.Name != spec.Name || dependency.Image != spec.Image || dependency.SpecHash != hash || !validSupervisorDependencyHex(dependency.ContainerId) {
		return supervisorDependency{}, errors.Join(errors.New("container fault differs from its captured generation or creation spec"), err)
	}
	return dependency, nil
}

// Tests inject commands at the same boundary used by the real docker process.
func (self *dockerScenarioContainerRuntime) run(ctx context.Context, args ...string) ([]byte, error) {
	if self.command != nil {
		return self.command(ctx, args...)
	}
	return self.docker.commandContext(ctx, args...).CombinedOutput()
}

// Reinspect before mutation, address every command by captured id, and skip
// start if a prior reply was lost after the exact container became running.
func (self *dockerScenarioContainerRuntime) Start(ctx context.Context, spec managedContainerSpec) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	dependency, err := self.dependency(spec)
	if err != nil {
		return 0, err
	}
	round, cancel := context.WithTimeout(ctx, containerRestartRoundTimeout)
	defer cancel()
	state, err := self.inspect(round, spec)
	if err != nil {
		return 0, err
	}
	pending := func(cause error) (int, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 0, &containerStartPendingError{name: spec.Name, containerId: dependency.ContainerId, specHash: dependency.SpecHash, cause: cause}
	}
	if !state.Running {
		output, err := self.run(round, "start", dependency.ContainerId)
		if err != nil {
			cause := fmt.Errorf("start simulator dependency %s: %w: %s", spec.Name, err, strings.TrimSpace(string(output)))
			if errors.Is(round.Err(), context.DeadlineExceeded) {
				return pending(cause)
			}
			return 0, cause
		}
	}
	if err := waitContainerReadyWithProbe(round, spec, func(ctx context.Context) ([]byte, error) {
		args := append([]string{"exec", dependency.ContainerId}, spec.ReadyProbe...)
		return self.run(ctx, args...)
	}); err != nil {
		return pending(err)
	}
	state, err = self.inspect(round, spec)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && errors.Is(round.Err(), context.DeadlineExceeded) {
			return pending(err)
		}
		return 0, err
	}
	if !state.Running {
		return pending(fmt.Errorf("simulator dependency %s stopped before readiness readback", spec.Name))
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return state.PID, nil
}

// Authenticate every retained target before starting any member. Complete
// members are rechecked on retry; partial progress never clears the fault ledger.
func (self *liveScenarioFaultDriver) restoreContainerFault(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if self.cfg == nil || spec.Kind != "container-restart" || len(spec.Targets) == 0 {
		return nil, fmt.Errorf("unsupported container fault %q", spec.Kind)
	}
	targetKVs, err := dependencyFaultTargets(self.cfg)
	if err != nil {
		return nil, err
	}
	active, err := readActiveFaultFile(self.activePath())
	if err != nil {
		return nil, err
	}
	if _, err := activeFaultIndex(active, spec); err != nil {
		return nil, err
	}
	priorProcessKVs := make(map[string]FaultProcessEvidence, len(active.Processes))
	for _, process := range active.Processes {
		priorProcessKVs[process.ID] = process
	}
	ids := slices.Clone(spec.Targets)
	sort.Strings(ids)
	for _, id := range ids {
		target, ok := targetKVs[id]
		original, retained := priorProcessKVs[id]
		if !ok || !retained || original.Role != target.role || original.Identity != target.spec.Name || original.PID <= 1 {
			return nil, fmt.Errorf("container fault target %q differs from its retained owner", id)
		}
	}
	runtime, err := self.containerRuntime(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]FaultProcessEvidence, 0, len(ids))
	var pending []string
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		target := targetKVs[id]
		pid, startErr := runtime.Start(ctx, target.spec)
		if startErr != nil {
			if !containerStartPending(target.spec, startErr) {
				return nil, startErr
			}
			pending = append(pending, startErr.Error())
			continue
		}
		if pid <= 1 || pid == priorProcessKVs[id].PID {
			return nil, fmt.Errorf("simulator dependency %s restarted without replacing pid %d", target.spec.Name, priorProcessKVs[id].PID)
		}
		result = append(result, FaultProcessEvidence{ID: id, Role: target.role, Identity: target.spec.Name, PID: pid})
	}
	if len(pending) != 0 {
		return nil, &containerRestartPendingError{faultId: spec.ID, targets: slices.Clone(spec.Targets), detail: strings.Join(pending, "; ")}
	}
	return result, nil
}

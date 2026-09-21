package main

// The running supervisor repairs Docker-daemon outages without recreating data
// or enabling restart-after-reboot. Fault injection shares its mutation lock.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Public ownership metadata contains no container environment or credentials.
// ContainerId is captured once at launch and never rebound during recovery.
type supervisorDependency struct {
	TargetId    string `json:"target_id"`
	ContainerId string `json:"container_id"`
	Name        string `json:"name"`
	Image       string `json:"image"`
	SpecHash    string `json:"spec_hash"`
}

// Limits inspection output to the fields needed to bind one existing container.
type supervisorDependencyObservation struct {
	containerId string
	name        string
	image       string
	specHash    string
	running     bool
	paused      bool
	restarting  bool
}

// Injecting this boundary lets tests force daemon failures and replacements.
type supervisorDependencyRuntime interface {
	inspect(context.Context, string) (supervisorDependencyObservation, error)
	start(context.Context, string) error
}

// Uses only exact container IDs for mutations, even if a name is replaced.
type dockerSupervisorDependencyRuntime struct{ docker dockerCLI }

// Never dump a whole Docker inspect document: it contains private credentials.
func (self *dockerSupervisorDependencyRuntime) inspect(ctx context.Context, identity string) (supervisorDependencyObservation, error) {
	format := "{{.Id}}|{{.Name}}|{{.Config.Image}}|{{index .Config.Labels \"" + managedContainerSpecHashLabel + "\"}}|{{.HostConfig.RestartPolicy.Name}}|{{.State.Running}}|{{.State.Paused}}|{{.State.Restarting}}"
	output, err := self.docker.commandContext(ctx, "container", "inspect", "--format", format, identity).CombinedOutput()
	if err != nil {
		return supervisorDependencyObservation{}, fmt.Errorf("inspect dependency %s: %w: %s", identity, err, strings.TrimSpace(string(output)))
	}
	fields := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(fields) != 8 || fields[4] != "no" {
		return supervisorDependencyObservation{}, errors.New("dependency inspection has invalid fields or reboot restart policy")
	}
	for _, value := range fields[5:] {
		if value != "true" && value != "false" {
			return supervisorDependencyObservation{}, errors.New("dependency inspection has invalid lifecycle flags")
		}
	}
	return supervisorDependencyObservation{
		containerId: fields[0], name: strings.TrimPrefix(fields[1], "/"), image: fields[2], specHash: fields[3],
		running: fields[5] == "true", paused: fields[6] == "true", restarting: fields[7] == "true",
	}, nil
}

// Restarting an existing ID cannot initialize or switch to another volume.
func (self *dockerSupervisorDependencyRuntime) start(ctx context.Context, containerId string) error {
	output, err := self.docker.commandContext(ctx, "start", containerId).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start dependency %s: %w: %s", containerId, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// The approved launch already started and authenticated these exact specs.
// Capture IDs before the supervisor manifest becomes immutable.
func captureSupervisorDependencies(ctx context.Context, cfg *ResolvedConfig) ([]supervisorDependency, error) {
	targets, err := dependencyFaultTargets(cfg)
	if err != nil {
		return nil, err
	}
	docker, err := resolveDockerCLI(ctx)
	if err != nil {
		return nil, err
	}
	runtime := &dockerSupervisorDependencyRuntime{docker: docker}
	dependencies := make([]supervisorDependency, 0, len(targets))
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		for _, role := range []string{"postgres", "redis"} {
			targetId := fmt.Sprintf("operator-%d-%s", operator, role)
			spec := targets[targetId].spec
			specHash, err := managedContainerSpecHash(spec)
			if err != nil {
				return nil, err
			}
			observation, err := runtime.inspect(ctx, spec.Name)
			if err != nil {
				return nil, err
			}
			dependency := supervisorDependency{TargetId: targetId, ContainerId: observation.containerId, Name: spec.Name, Image: spec.Image, SpecHash: specHash}
			if err := validateSupervisorDependencyObservation(dependency, observation); err != nil {
				return nil, err
			}
			dependencies = append(dependencies, dependency)
		}
	}
	return dependencies, validateSupervisorDependencies(dependencies)
}

// Docker container IDs and creation-spec labels use canonical SHA-256 hex.
func validSupervisorDependencyHex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, digit := range value {
		if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f') {
			return false
		}
	}
	return true
}

// A malformed or duplicate ownership entry must never become restart authority.
func validateSupervisorDependencies(dependencies []supervisorDependency) error {
	ids, names, targets := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, dependency := range dependencies {
		if !validSupervisorDependencyHex(dependency.ContainerId) || !validSupervisorDependencyHex(dependency.SpecHash) || dependency.TargetId == "" || dependency.Name == "" || dependency.Image == "" || ids[dependency.ContainerId] || names[dependency.Name] || targets[dependency.TargetId] {
			return errors.New("supervisor dependency identity is incomplete or duplicated")
		}
		ids[dependency.ContainerId], names[dependency.Name], targets[dependency.TargetId] = true, true, true
	}
	return nil
}

// The exact generation, release image and creation-spec label must still match.
func validateSupervisorDependencyObservation(dependency supervisorDependency, observation supervisorDependencyObservation) error {
	if observation.containerId != dependency.ContainerId || observation.name != dependency.Name || observation.image != dependency.Image || observation.specHash != dependency.SpecHash {
		return fmt.Errorf("dependency %s differs from its captured container identity", dependency.TargetId)
	}
	return nil
}

// A busy fault mutation is a normal deferral. Lock ownership spans both the
// container mutation and its durable fault ledger update.
func trySupervisorDependencyLock(stateDir string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(stateDir, "dependency-recovery.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, nil
		}
		return nil, err
	}
	return func() { _ = file.Close() }, nil
}

// Fault commands wait with cancellation; the background repair merely skips a
// busy cycle so it never contends with deliberately injected outages.
func lockSupervisorDependencyFault(ctx context.Context, stateDir string) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		unlock, err := trySupervisorDependencyLock(stateDir)
		if err != nil || unlock != nil {
			return unlock, err
		}
		if err := waitSupervisorRestart(ctx, 100*time.Millisecond); err != nil {
			return nil, err
		}
	}
}

// Each cycle attempts all independent dependencies, retaining failures for the
// next bounded cycle instead of abandoning the healthy processes. It never
// creates, removes, unpauses, or replaces a container.
func recoverSupervisorDependencies(ctx context.Context, stateDir string, dependencies []supervisorDependency, runtime supervisorDependencyRuntime) ([]string, error) {
	if err := validateSupervisorDependencies(dependencies); err != nil {
		return nil, err
	}
	if runtime == nil {
		return nil, errors.New("supervisor dependency runtime is missing")
	}
	unlock, err := trySupervisorDependencyLock(stateDir)
	if err != nil || unlock == nil {
		return nil, err
	}
	defer unlock()
	active, err := readActiveFaultFile(filepath.Join(stateDir, "active-faults.json"))
	if err != nil {
		return nil, err
	}
	excludedTargetIds := map[string]bool{}
	for _, fault := range active.Faults {
		for _, targetId := range fault.Targets {
			excludedTargetIds[targetId] = true
		}
	}
	var repairedTargetIds []string
	var failures []error
	for _, dependency := range dependencies {
		if err := ctx.Err(); err != nil {
			return repairedTargetIds, errors.Join(append(failures, err)...)
		}
		if excludedTargetIds[dependency.TargetId] {
			continue
		}
		observation, err := runtime.inspect(ctx, dependency.ContainerId)
		if err == nil {
			err = validateSupervisorDependencyObservation(dependency, observation)
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if observation.running || observation.paused || observation.restarting {
			continue
		}
		if err := ctx.Err(); err != nil {
			return repairedTargetIds, errors.Join(append(failures, err)...)
		}
		if err := runtime.start(ctx, dependency.ContainerId); err != nil {
			failures = append(failures, err)
			continue
		}
		repairedTargetIds = append(repairedTargetIds, dependency.TargetId)
	}
	return repairedTargetIds, errors.Join(failures...)
}

// Background recovery is owned by the live supervisor, so host reboot remains
// an explicit resume boundary. Cancellation joins the worker before return.
func startSupervisorDependencyRecovery(ctx context.Context, stateDir string, dependencies []supervisorDependency, output io.Writer) func() {
	if len(dependencies) == 0 {
		return func() {}
	}
	recoveryCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		lastFinding := ""
		for {
			cycleCtx, cancelCycle := context.WithTimeout(recoveryCtx, 15*time.Second)
			docker, err := resolveDockerCLI(cycleCtx)
			var repaired []string
			if err == nil {
				repaired, err = recoverSupervisorDependencies(cycleCtx, stateDir, dependencies, &dockerSupervisorDependencyRuntime{docker: docker})
			}
			cancelCycle()
			if recoveryCtx.Err() != nil {
				return
			}
			if len(repaired) != 0 {
				fmt.Fprintf(output, "sim-testnet: recovered existing supervised dependencies: %s\n", strings.Join(repaired, ","))
			}
			finding := ""
			if err != nil {
				finding = err.Error()
			}
			if finding != "" && finding != lastFinding {
				fmt.Fprintf(output, "sim-testnet: dependency recovery deferred; retrying: %s\n", finding)
			}
			lastFinding = finding
			select {
			case <-recoveryCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}

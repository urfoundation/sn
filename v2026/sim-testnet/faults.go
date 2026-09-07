package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type scenarioFaultSpec struct {
	ID                         string   `json:"id"`
	Kind                       string   `json:"kind"`
	Targets                    []string `json:"targets"`
	Impacts                    []string `json:"impacts,omitempty"`
	ValidatorID                int      `json:"validator_id,omitempty"`
	FleetIndex                 int      `json:"fleet_index,omitempty"`
	FleetIndices               []int    `json:"fleet_indices,omitempty"`
	PreAcceptance              bool     `json:"pre_acceptance,omitempty"`
	PostAcceptanceEvidenceTail bool     `json:"post_acceptance_evidence_tail,omitempty"`
	ActivationCondition        string   `json:"activation_condition,omitempty"`
	RestoreCondition           string   `json:"restore_condition,omitempty"`
	MinimumDurationBlocks      uint64   `json:"minimum_duration_blocks,omitempty"`
	TriggerOffsetBlocks        uint64   `json:"trigger_offset_blocks"`
	DurationBlocks             uint64   `json:"duration_blocks"`
}

type FaultProcessEvidence struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Identity string `json:"identity"`
	PID      int    `json:"pid"`
}

type ScenarioFaultRecord struct {
	ID                         string                 `json:"id"`
	Kind                       string                 `json:"kind"`
	Targets                    []string               `json:"targets"`
	Impacts                    []string               `json:"impacts,omitempty"`
	ValidatorID                int                    `json:"validator_id,omitempty"`
	FleetIndex                 int                    `json:"fleet_index,omitempty"`
	FleetIndices               []int                  `json:"fleet_indices,omitempty"`
	PreAcceptance              bool                   `json:"pre_acceptance,omitempty"`
	PostAcceptanceEvidenceTail bool                   `json:"post_acceptance_evidence_tail,omitempty"`
	ArmedBlock                 uint64                 `json:"armed_block,omitempty"`
	ArmedBlockHash             string                 `json:"armed_block_hash,omitempty"`
	ActivationCondition        string                 `json:"activation_condition,omitempty"`
	RestoreCondition           string                 `json:"restore_condition,omitempty"`
	MinimumDurationBlocks      uint64                 `json:"minimum_duration_blocks,omitempty"`
	TriggerBlock               uint64                 `json:"trigger_block"`
	RestoreBlock               uint64                 `json:"restore_block"`
	AppliedBlock               uint64                 `json:"applied_block,omitempty"`
	AppliedBlockHash           string                 `json:"applied_block_hash,omitempty"`
	RestoredBlock              uint64                 `json:"restored_block,omitempty"`
	RestoredBlockHash          string                 `json:"restored_block_hash,omitempty"`
	RestoreConditionMet        bool                   `json:"restore_condition_met,omitempty"`
	RestoreConditionBlock      uint64                 `json:"restore_condition_block,omitempty"`
	ActivationConditionMet     bool                   `json:"activation_condition_met,omitempty"`
	ActivationConditionBlock   uint64                 `json:"activation_condition_block,omitempty"`
	Processes                  []FaultProcessEvidence `json:"processes,omitempty"`
	RestoredProcesses          []FaultProcessEvidence `json:"restored_processes,omitempty"`
	Status                     string                 `json:"status"`
	Error                      string                 `json:"error,omitempty"`
}

type scenarioFaultDriver interface {
	Apply(context.Context, scenarioFaultSpec) ([]FaultProcessEvidence, error)
	Restore(context.Context, scenarioFaultSpec) ([]FaultProcessEvidence, error)
	Recover(context.Context) error
}

type scenarioContainerRuntime interface {
	Stop(context.Context, managedContainerSpec) (int, error)
	Start(context.Context, managedContainerSpec) (int, error)
}

type liveScenarioFaultDriver struct {
	stateDir        string
	cfg             *ResolvedConfig
	planHash        string
	coordinator     string
	containers      scenarioContainerRuntime
	minerControlURL func(swarm int, target, action string) string
}

type dockerScenarioContainerRuntime struct{ docker dockerCLI }

type scenarioContainerState struct {
	Running bool
	PID     int
}

func (runtime *dockerScenarioContainerRuntime) inspect(ctx context.Context, spec managedContainerSpec) (scenarioContainerState, error) {
	specHash, err := managedContainerSpecHash(spec)
	if err != nil {
		return scenarioContainerState{}, err
	}
	format := "{{.State.Running}}|{{.State.Pid}}|{{.Config.Image}}|{{index .Config.Labels \"" + managedContainerSpecHashLabel + "\"}}"
	output, err := runtime.docker.commandContext(ctx, "container", "inspect", "--format", format, spec.Name).CombinedOutput()
	if err != nil {
		return scenarioContainerState{}, fmt.Errorf("inspect simulator dependency %s: %w: %s", spec.Name, err, strings.TrimSpace(string(output)))
	}
	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) != 4 || parts[2] != spec.Image || parts[3] != specHash {
		return scenarioContainerState{}, fmt.Errorf("simulator dependency %s no longer matches its release-locked container spec", spec.Name)
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil {
		return scenarioContainerState{}, fmt.Errorf("simulator dependency %s has invalid PID %q", spec.Name, parts[1])
	}
	return scenarioContainerState{Running: parts[0] == "true", PID: pid}, nil
}

func (runtime *dockerScenarioContainerRuntime) Stop(ctx context.Context, spec managedContainerSpec) (int, error) {
	before, err := runtime.inspect(ctx, spec)
	if err != nil {
		return 0, err
	}
	if !before.Running || before.PID <= 1 {
		return 0, fmt.Errorf("simulator dependency %s is not running", spec.Name)
	}
	output, err := runtime.docker.commandContext(ctx, "stop", "--time", "5", spec.Name).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("stop simulator dependency %s: %w: %s", spec.Name, err, strings.TrimSpace(string(output)))
	}
	after, err := runtime.inspect(ctx, spec)
	if err != nil {
		return 0, err
	}
	if after.Running || after.PID != 0 {
		return 0, fmt.Errorf("simulator dependency %s remained running after stop", spec.Name)
	}
	return before.PID, nil
}

func (runtime *dockerScenarioContainerRuntime) Start(ctx context.Context, spec managedContainerSpec) (int, error) {
	state, err := runtime.inspect(ctx, spec)
	if err != nil {
		return 0, err
	}
	if !state.Running {
		output, startErr := runtime.docker.commandContext(ctx, "start", spec.Name).CombinedOutput()
		if startErr != nil {
			return 0, fmt.Errorf("start simulator dependency %s: %w: %s", spec.Name, startErr, strings.TrimSpace(string(output)))
		}
	}
	if err := waitContainerReady(ctx, runtime.docker, spec); err != nil {
		return 0, err
	}
	state, err = runtime.inspect(ctx, spec)
	if err != nil {
		return 0, err
	}
	if !state.Running || state.PID <= 1 {
		return 0, fmt.Errorf("simulator dependency %s did not return with a live PID", spec.Name)
	}
	return state.PID, nil
}

type dependencyFaultTarget struct {
	spec managedContainerSpec
	role string
}

func dependencyFaultTargets(cfg *ResolvedConfig) (map[string]dependencyFaultTarget, error) {
	specs, err := dependencyContainerSpecs(cfg)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]dependencyFaultTarget, len(specs))
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		base := (operator - 1) * 2
		targets[fmt.Sprintf("operator-%d-postgres", operator)] = dependencyFaultTarget{spec: specs[base], role: "postgresql"}
		targets[fmt.Sprintf("operator-%d-redis", operator)] = dependencyFaultTarget{spec: specs[base+1], role: "redis"}
	}
	return targets, nil
}

func (d *liveScenarioFaultDriver) containerRuntime(ctx context.Context) (scenarioContainerRuntime, error) {
	if d.containers != nil {
		return d.containers, nil
	}
	docker, err := resolveDockerCLI(ctx)
	if err != nil {
		return nil, err
	}
	d.containers = &dockerScenarioContainerRuntime{docker: docker}
	return d.containers, nil
}

type activeFaultFile struct {
	Schema    string                 `json:"schema"`
	Faults    []scenarioFaultSpec    `json:"faults"`
	Processes []FaultProcessEvidence `json:"processes,omitempty"`
}

func (d *liveScenarioFaultDriver) activePath() string {
	return filepath.Join(d.stateDir, "active-faults.json")
}

// readActiveFaultFile authenticates the crash-recovery ledger while allowing
// the no-fault state to be represented by an absent file.
func readActiveFaultFile(path string) (activeFaultFile, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return activeFaultFile{Schema: "urnetwork-sim-active-faults-v1"}, nil
	}
	if err != nil {
		return activeFaultFile{}, err
	}
	var active activeFaultFile
	if json.Unmarshal(b, &active) != nil || active.Schema != "urnetwork-sim-active-faults-v1" || len(active.Faults) == 0 {
		return activeFaultFile{}, errors.New("invalid active fault recovery file; refusing ambiguous process state")
	}
	ids := map[string]bool{}
	targets := map[string]bool{}
	for _, fault := range active.Faults {
		if fault.ID == "" || ids[fault.ID] || len(fault.Targets) == 0 {
			return activeFaultFile{}, errors.New("active fault recovery identities are invalid")
		}
		ids[fault.ID] = true
		for _, target := range fault.Targets {
			if target == "" || targets[target] {
				return activeFaultFile{}, errors.New("active fault recovery targets overlap")
			}
			targets[target] = true
		}
	}
	processes := map[string]bool{}
	for _, process := range active.Processes {
		if !targets[process.ID] || processes[process.ID] || process.PID <= 1 {
			return activeFaultFile{}, errors.New("active fault process evidence is invalid")
		}
		processes[process.ID] = true
	}
	if len(processes) != len(targets) {
		return activeFaultFile{}, errors.New("active fault process evidence is incomplete")
	}
	return active, nil
}

// activeFaultIndex finds an exact immutable specification in the recovery
// ledger. Matching only an ID would let a corrupted duration or target restore
// the wrong process set.
func activeFaultIndex(active activeFaultFile, spec scenarioFaultSpec) (int, error) {
	want, err := canonicalHashHex(spec)
	if err != nil {
		return -1, err
	}
	for index, candidate := range active.Faults {
		got, hashErr := canonicalHashHex(candidate)
		if hashErr != nil {
			return -1, hashErr
		}
		if got == want {
			return index, nil
		}
	}
	return -1, fmt.Errorf("active fault %s is not recorded exactly", spec.ID)
}

// validateFaultActivation rejects duplicate IDs or process targets before any
// signal, logical-miner control, or container mutation occurs.
func validateFaultActivation(active activeFaultFile, spec scenarioFaultSpec) error {
	if spec.ID == "" || len(spec.Targets) == 0 {
		return errors.New("active fault identity is incomplete")
	}
	usedTargets := map[string]bool{}
	for _, fault := range active.Faults {
		if fault.ID == spec.ID {
			return fmt.Errorf("fault %s is already active", spec.ID)
		}
		for _, target := range fault.Targets {
			usedTargets[target] = true
		}
	}
	for _, target := range spec.Targets {
		if target == "" || usedTargets[target] {
			return fmt.Errorf("fault %s target %q is already active", spec.ID, target)
		}
		usedTargets[target] = true
	}
	return nil
}

// appendActiveFault durably adds one disjoint fault after its mutation has
// succeeded. The caller remains responsible for rolling the mutation back if
// the atomic write fails.
func appendActiveFault(path string, active activeFaultFile, spec scenarioFaultSpec, processes []FaultProcessEvidence) error {
	if len(processes) != len(spec.Targets) {
		return fmt.Errorf("fault %s process evidence count %d does not match %d targets", spec.ID, len(processes), len(spec.Targets))
	}
	targets := map[string]bool{}
	for _, target := range spec.Targets {
		targets[target] = true
	}
	seen := map[string]bool{}
	for _, process := range processes {
		if !targets[process.ID] || seen[process.ID] || process.PID <= 1 {
			return fmt.Errorf("fault %s returned invalid process evidence for %q", spec.ID, process.ID)
		}
		seen[process.ID] = true
	}
	active.Faults = append(active.Faults, spec)
	active.Processes = append(active.Processes, processes...)
	b, err := json.MarshalIndent(active, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'), 0o600)
}

// removeActiveFault removes exactly one restored fault while preserving every
// concurrent independent fault for crash recovery.
func removeActiveFault(path string, active activeFaultFile, index int) error {
	if index < 0 || index >= len(active.Faults) {
		return errors.New("active fault removal index is out of bounds")
	}
	targets := map[string]bool{}
	for _, target := range active.Faults[index].Targets {
		targets[target] = true
	}
	active.Faults = append(active.Faults[:index], active.Faults[index+1:]...)
	remaining := active.Processes[:0]
	removed := 0
	for _, process := range active.Processes {
		if targets[process.ID] {
			removed++
			continue
		}
		remaining = append(remaining, process)
	}
	active.Processes = remaining
	if removed != len(targets) {
		return errors.New("restored fault process evidence is incomplete")
	}
	if len(active.Faults) == 0 {
		if len(active.Processes) != 0 {
			return errors.New("orphan active fault process evidence remains")
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := json.MarshalIndent(active, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'), 0o600)
}

func (d *liveScenarioFaultDriver) processSnapshot() (map[string]ProcessState, map[string]ProcessSpec, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(d.stateDir, "supervisor.json"))
	if err != nil {
		return nil, nil, err
	}
	stateBytes, err := os.ReadFile(filepath.Join(d.stateDir, "supervisor.state.json"))
	if err != nil {
		return nil, nil, err
	}
	var manifest SupervisorFile
	var state SupervisorState
	if json.Unmarshal(manifestBytes, &manifest) != nil || manifest.Schema != "urnetwork-sim-supervisor-v1" {
		return nil, nil, errors.New("invalid supervisor manifest")
	}
	if json.Unmarshal(stateBytes, &state) != nil || state.Schema != "urnetwork-sim-supervisor-state-v1" {
		return nil, nil, errors.New("invalid supervisor state")
	}
	wantHash, err := canonicalHashHex(manifest)
	if err != nil || state.ManifestHash != wantHash {
		return nil, nil, errors.New("supervisor state does not match the checksum-locked manifest")
	}
	states := map[string]ProcessState{}
	specs := map[string]ProcessSpec{}
	for _, process := range state.Processes {
		states[process.ID] = process
	}
	for _, spec := range manifest.Specs {
		specs[spec.ID] = spec
	}
	return states, specs, nil
}

func (d *liveScenarioFaultDriver) signal(spec scenarioFaultSpec, signal syscall.Signal) ([]FaultProcessEvidence, error) {
	if (spec.Kind != "process-pause" && spec.Kind != "process-restart") || len(spec.Targets) == 0 {
		return nil, fmt.Errorf("unsupported fault kind %q", spec.Kind)
	}
	states, specs, err := d.processSnapshot()
	if err != nil {
		return nil, err
	}
	targets := append([]string(nil), spec.Targets...)
	sort.Strings(targets)
	result := make([]FaultProcessEvidence, 0, len(targets))
	for _, id := range targets {
		state, stateOK := states[id]
		processSpec, specOK := specs[id]
		if !stateOK || !specOK || state.PID <= 1 {
			return nil, fmt.Errorf("fault target %q is not a live manifest process", id)
		}
		group, groupErr := syscall.Getpgid(state.PID)
		if groupErr != nil || group != state.PID {
			return nil, fmt.Errorf("fault target %q pid %d is not its expected process-group leader", id, state.PID)
		}
		if err := syscall.Kill(-state.PID, signal); err != nil {
			if signal == syscall.SIGSTOP {
				for _, prior := range result {
					_ = syscall.Kill(-prior.PID, syscall.SIGCONT)
				}
			}
			return nil, fmt.Errorf("signal fault target %q: %w", id, err)
		}
		result = append(result, FaultProcessEvidence{ID: id, Role: processSpec.Role, Identity: processSpec.Identity, PID: state.PID})
	}
	return result, nil
}

func minerSwarmFor(cfg *ResolvedConfig, miner int) (int, error) {
	if cfg == nil || cfg.Config == nil || cfg.Config.Topology.MinerSwarmProcesses < 1 || cfg.Config.Topology.Miners < 1 || cfg.Config.Topology.Miners%cfg.Config.Topology.MinerSwarmProcesses != 0 || miner < 1 || miner > cfg.Config.Topology.Miners {
		return 0, fmt.Errorf("miner %d cannot be mapped to a configured swarm", miner)
	}
	return 1 + (miner-1)/(cfg.Config.Topology.Miners/cfg.Config.Topology.MinerSwarmProcesses), nil
}

func (d *liveScenarioFaultDriver) controlURL(swarm int, target, action string) string {
	if d.minerControlURL != nil {
		return d.minerControlURL(swarm, target, action)
	}
	return fmt.Sprintf("http://127.0.0.1:%d/control/%s/%s", 21080+swarm, target, action)
}

func (d *liveScenarioFaultDriver) controlMiners(ctx context.Context, spec scenarioFaultSpec, enable bool) ([]FaultProcessEvidence, error) {
	if d.cfg == nil || spec.Kind != "miner-control" || len(spec.Targets) == 0 {
		return nil, fmt.Errorf("unsupported miner control fault %q", spec.Kind)
	}
	states, processSpecs, err := d.processSnapshot()
	if err != nil {
		return nil, err
	}
	targets := append([]string(nil), spec.Targets...)
	sort.Strings(targets)
	action := "disable"
	rollbackAction := "enable"
	if enable {
		action, rollbackAction = "enable", "disable"
	}
	completed := make([]struct {
		miner int
		swarm int
	}, 0, len(targets))
	rollback := func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer rollbackCancel()
		for index := len(completed) - 1; index >= 0; index-- {
			item := completed[index]
			request, _ := http.NewRequestWithContext(rollbackCtx, http.MethodPost, d.controlURL(item.swarm, fmt.Sprintf("miner-%d", item.miner), rollbackAction), nil)
			response, requestErr := http.DefaultClient.Do(request)
			if requestErr == nil {
				response.Body.Close()
			}
		}
	}
	result := make([]FaultProcessEvidence, 0, len(targets))
	for _, target := range targets {
		var miner int
		if _, scanErr := fmt.Sscanf(target, "miner-%d", &miner); scanErr != nil || target != fmt.Sprintf("miner-%d", miner) {
			rollback()
			return nil, fmt.Errorf("invalid miner control target %q", target)
		}
		swarm, mapErr := minerSwarmFor(d.cfg, miner)
		if mapErr != nil {
			rollback()
			return nil, mapErr
		}
		processID := fmt.Sprintf("miner-swarm-%d", swarm)
		state, stateOK := states[processID]
		processSpec, specOK := processSpecs[processID]
		if !stateOK || !specOK || state.PID <= 1 || (!enable && !state.Healthy) {
			rollback()
			return nil, fmt.Errorf("miner control target %s has no healthy owning swarm", target)
		}
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, d.controlURL(swarm, target, action), nil)
		if requestErr != nil {
			rollback()
			return nil, requestErr
		}
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			rollback()
			return nil, fmt.Errorf("%s %s: %w", action, target, requestErr)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil || response.StatusCode/100 != 2 {
			rollback()
			return nil, fmt.Errorf("%s %s: HTTP %d: %s: %v", action, target, response.StatusCode, strings.TrimSpace(string(body)), readErr)
		}
		completed = append(completed, struct {
			miner int
			swarm int
		}{miner: miner, swarm: swarm})
		result = append(result, FaultProcessEvidence{ID: target, Role: "miner", Identity: processSpec.Identity, PID: state.PID})
	}
	return result, nil
}

func (d *liveScenarioFaultDriver) applyContainerFault(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if d.cfg == nil || spec.Kind != "container-restart" || len(spec.Targets) == 0 {
		return nil, fmt.Errorf("unsupported container fault %q", spec.Kind)
	}
	targets, err := dependencyFaultTargets(d.cfg)
	if err != nil {
		return nil, err
	}
	runtime, err := d.containerRuntime(ctx)
	if err != nil {
		return nil, err
	}
	ids := append([]string(nil), spec.Targets...)
	sort.Strings(ids)
	result := make([]FaultProcessEvidence, 0, len(ids))
	stopped := make([]dependencyFaultTarget, 0, len(ids))
	rollback := func() {
		for index := len(stopped) - 1; index >= 0; index-- {
			_, _ = runtime.Start(context.Background(), stopped[index].spec)
		}
	}
	for _, id := range ids {
		target, ok := targets[id]
		if !ok {
			rollback()
			return nil, fmt.Errorf("container fault target %q is not a simulator-owned PostgreSQL/Redis dependency", id)
		}
		pid, stopErr := runtime.Stop(ctx, target.spec)
		if stopErr != nil {
			rollback()
			return nil, stopErr
		}
		stopped = append(stopped, target)
		result = append(result, FaultProcessEvidence{ID: id, Role: target.role, Identity: target.spec.Name, PID: pid})
	}
	return result, nil
}

func (d *liveScenarioFaultDriver) restoreContainerFault(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	if d.cfg == nil || spec.Kind != "container-restart" || len(spec.Targets) == 0 {
		return nil, fmt.Errorf("unsupported container fault %q", spec.Kind)
	}
	targets, err := dependencyFaultTargets(d.cfg)
	if err != nil {
		return nil, err
	}
	runtime, err := d.containerRuntime(ctx)
	if err != nil {
		return nil, err
	}
	prior := map[string]int{}
	if activeBytes, readErr := os.ReadFile(d.activePath()); readErr == nil {
		var active activeFaultFile
		if json.Unmarshal(activeBytes, &active) != nil || active.Schema != "urnetwork-sim-active-faults-v1" {
			return nil, errors.New("invalid active container fault evidence")
		}
		for _, process := range active.Processes {
			prior[process.ID] = process.PID
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	ids := append([]string(nil), spec.Targets...)
	sort.Strings(ids)
	result := make([]FaultProcessEvidence, 0, len(ids))
	for _, id := range ids {
		target, ok := targets[id]
		if !ok {
			return nil, fmt.Errorf("container fault target %q is not a simulator-owned PostgreSQL/Redis dependency", id)
		}
		pid, startErr := runtime.Start(ctx, target.spec)
		if startErr != nil {
			return nil, startErr
		}
		if prior[id] > 1 && pid == prior[id] {
			return nil, fmt.Errorf("simulator dependency %s restarted without replacing PID %d", target.spec.Name, pid)
		}
		result = append(result, FaultProcessEvidence{ID: id, Role: target.role, Identity: target.spec.Name, PID: pid})
	}
	return result, nil
}

func (d *liveScenarioFaultDriver) Apply(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	active, err := readActiveFaultFile(d.activePath())
	if err != nil {
		return nil, err
	}
	// Pre-acceptance faults are armed before the accepted boundary and then
	// adopted by the ordinary in-window scheduler. Exact adoption is
	// idempotent; a merely matching ID or overlapping target still fails below.
	if index, exactErr := activeFaultIndex(active, spec); exactErr == nil {
		targets := make(map[string]bool, len(active.Faults[index].Targets))
		for _, target := range active.Faults[index].Targets {
			targets[target] = true
		}
		processes := make([]FaultProcessEvidence, 0, len(targets))
		for _, process := range active.Processes {
			if targets[process.ID] {
				processes = append(processes, process)
			}
		}
		if len(processes) != len(targets) {
			return nil, fmt.Errorf("fault %s has incomplete adopted process evidence", spec.ID)
		}
		sort.Slice(processes, func(i, j int) bool { return processes[i].ID < processes[j].ID })
		return processes, nil
	}
	if err := validateFaultActivation(active, spec); err != nil {
		return nil, err
	}
	if spec.Kind == "container-restart" || spec.Kind == "miner-control" || spec.Kind == "validator-view-filter" {
		var processes []FaultProcessEvidence
		var mutationErr error
		if spec.Kind == "container-restart" {
			processes, mutationErr = d.applyContainerFault(ctx, spec)
		} else if spec.Kind == "miner-control" {
			processes, mutationErr = d.controlMiners(ctx, spec, false)
		} else {
			processes, mutationErr = d.applyValidatorViewFilter(ctx, spec)
		}
		if mutationErr != nil {
			return nil, mutationErr
		}
		if err := appendActiveFault(d.activePath(), active, spec, processes); err != nil {
			if spec.Kind == "container-restart" {
				_, _ = d.restoreContainerFault(context.Background(), spec)
			} else if spec.Kind == "miner-control" {
				_, _ = d.controlMiners(context.Background(), spec, true)
			} else {
				_, _ = d.restoreValidatorViewFilter(context.Background(), spec)
			}
			return nil, err
		}
		return processes, nil
	}
	signal := syscall.SIGSTOP
	if spec.Kind == "process-restart" {
		signal = syscall.SIGTERM
	}
	processes, err := d.signal(spec, signal)
	if err != nil {
		return nil, err
	}
	if err := appendActiveFault(d.activePath(), active, spec, processes); err != nil {
		if spec.Kind == "process-pause" {
			_, _ = d.signal(spec, syscall.SIGCONT)
		}
		return nil, err
	}
	return processes, nil
}

func (d *liveScenarioFaultDriver) Restore(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	active, err := readActiveFaultFile(d.activePath())
	if err != nil {
		return nil, err
	}
	index, err := activeFaultIndex(active, spec)
	if err != nil {
		return nil, err
	}
	var processes []FaultProcessEvidence
	var restoreErr error
	if spec.Kind == "container-restart" {
		processes, restoreErr = d.restoreContainerFault(ctx, spec)
	} else if spec.Kind == "miner-control" {
		processes, restoreErr = d.controlMiners(ctx, spec, true)
	} else if spec.Kind == "validator-view-filter" {
		processes, restoreErr = d.restoreValidatorViewFilter(ctx, spec)
	} else if spec.Kind == "process-restart" {
		processes, restoreErr = d.waitTargetsHealthy(ctx, spec, 2*time.Minute)
	} else {
		processes, restoreErr = d.signal(spec, syscall.SIGCONT)
	}
	if restoreErr != nil {
		return nil, restoreErr
	}
	if err := removeActiveFault(d.activePath(), active, index); err != nil {
		return nil, err
	}
	return processes, nil
}

func (d *liveScenarioFaultDriver) waitTargetsHealthy(ctx context.Context, spec scenarioFaultSpec, timeout time.Duration) ([]FaultProcessEvidence, error) {
	activeBytes, err := os.ReadFile(d.activePath())
	if err != nil {
		return nil, err
	}
	var active activeFaultFile
	if json.Unmarshal(activeBytes, &active) != nil || active.Schema != "urnetwork-sim-active-faults-v1" {
		return nil, errors.New("invalid active restart evidence")
	}
	prior := map[string]int{}
	for _, process := range active.Processes {
		prior[process.ID] = process.PID
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		states, specs, err := d.processSnapshot()
		if err == nil {
			ready := make([]FaultProcessEvidence, 0, len(spec.Targets))
			for _, id := range spec.Targets {
				state, stateOK := states[id]
				processSpec, specOK := specs[id]
				if !stateOK || !specOK || state.PID <= 1 || state.PID == prior[id] || !state.Healthy || syscall.Kill(state.PID, syscall.Signal(0)) != nil {
					ready = nil
					break
				}
				ready = append(ready, FaultProcessEvidence{ID: id, Role: processSpec.Role, Identity: processSpec.Identity, PID: state.PID})
			}
			if len(ready) == len(spec.Targets) {
				return ready, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("restart targets did not become healthy: %v", spec.Targets)
}

func (d *liveScenarioFaultDriver) Recover(ctx context.Context) error {
	active, err := readActiveFaultFile(d.activePath())
	if err != nil {
		return err
	}
	if len(active.Faults) == 0 {
		return d.removeOrphanValidatorViewFilters()
	}
	for _, fault := range active.Faults {
		if _, err := d.Restore(ctx, fault); err != nil {
			return fmt.Errorf("recover active fault %s: %w", fault.ID, err)
		}
	}
	return d.removeOrphanValidatorViewFilters()
}

func releaseQualityFault(cfg *ResolvedConfig) (scenarioFaultSpec, error) {
	var targets []string
	for miner := cfg.Config.Topology.fleetCandidateMiners() + 1; miner <= cfg.Config.Topology.Miners; miner++ {
		if operatorForMiner(cfg, miner) == cfg.Config.Scenarios.QualityFaultOperator {
			targets = append(targets, fmt.Sprintf("miner-%d", miner))
		}
	}
	if len(targets) == 0 {
		return scenarioFaultSpec{}, errors.New("quality fault has no non-head miners in the selected operator")
	}
	return scenarioFaultSpec{ID: "quality-cohort", Kind: "miner-control", Targets: targets, TriggerOffsetBlocks: cfg.Config.Scenarios.QualityFaultStartBlocks, DurationBlocks: cfg.Config.Scenarios.QualityFaultDurationBlocks}, nil
}

// testFleetPrefixScores reconstructs the split-adjusted head scores generated
// by the release topology. Deriving the fault duration from this geometry
// prevents a harmless fixture change from silently extending the live run.
func testFleetPrefixScores(cfg *ResolvedConfig) (map[int]*big.Rat, error) {
	if cfg == nil || cfg.Config == nil || cfg.Policy == nil || cfg.Policy.Verify.EgressIPv4Prefix < 1 || cfg.Policy.Verify.EgressIPv4Prefix > 32 {
		return nil, errors.New("test fleet prefix geometry is unavailable")
	}
	prefixes := make(map[int]map[netip.Prefix]bool, cfg.Config.Topology.fleetCandidates())
	claims := map[netip.Prefix]uint64{}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		prefixes[fleet] = map[netip.Prefix]bool{}
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			miner := fleetMemberMinerIndex(cfg, fleet, member)
			address, err := netip.ParseAddr(minerTestEgressSourceIP(miner))
			if err != nil {
				return nil, fmt.Errorf("fleet %d member %d source identity is invalid: %w", fleet, member, err)
			}
			if !address.Is4() {
				return nil, fmt.Errorf("fleet %d member %d source identity %s is not IPv4", fleet, member, address)
			}
			prefixes[fleet][netip.PrefixFrom(address, cfg.Policy.Verify.EgressIPv4Prefix).Masked()] = true
		}
		for prefix := range prefixes[fleet] {
			claims[prefix]++
		}
	}
	scores := make(map[int]*big.Rat, len(prefixes))
	for fleet, fleetPrefixes := range prefixes {
		score := new(big.Rat)
		for prefix := range fleetPrefixes {
			if claims[prefix] == 0 {
				return nil, fmt.Errorf("fleet %d prefix %s has no claimant", fleet, prefix)
			}
			score.Add(score, new(big.Rat).SetFrac64(1, int64(claims[prefix])))
		}
		scores[fleet] = score
	}
	return scores, nil
}

// headBoundaryDecayTempos returns the exact number of missing-observation EMA
// folds required to move the selected score to or below the challenger. The
// live topology preflights that both low-UID challengers win an equal-score tie
// against the two faulted production fleets.
func headBoundaryDecayTempos(numerator, denominator uint64, selected, challenger *big.Rat) (uint64, error) {
	if denominator == 0 || numerator == 0 || numerator > denominator || selected == nil || challenger == nil || selected.Sign() <= 0 || challenger.Sign() <= 0 || selected.Cmp(challenger) <= 0 {
		return 0, errors.New("head-boundary fault requires an EMA strictly above zero and at most one")
	}
	retained := new(big.Rat).SetFrac(
		new(big.Int).SetUint64(denominator-numerator),
		new(big.Int).SetUint64(denominator),
	)
	score := new(big.Rat).Set(selected)
	for folds := uint64(1); folds <= 256; folds++ {
		score.Mul(score, retained)
		if score.Cmp(challenger) <= 0 {
			return folds, nil
		}
	}
	return 0, errors.New("head-boundary EMA cannot cross the challenger within 256 tempos")
}

// headBoundaryRecoveryTempos starts at the exact post-decay score and returns
// the folds required for fresh observations to put the original fleet strictly
// above the challenger again. Strict comparison avoids relying on UID tie-break
// ordering for restoration evidence.
func headBoundaryRecoveryTempos(numerator, denominator uint64, selected, challenger *big.Rat, decayTempos uint64) (uint64, error) {
	if denominator == 0 || numerator == 0 || numerator > denominator || selected == nil || challenger == nil || selected.Sign() <= 0 || challenger.Sign() <= 0 || selected.Cmp(challenger) <= 0 || decayTempos == 0 {
		return 0, errors.New("head-boundary recovery requires valid positive scores, EMA, and decay")
	}
	alpha := new(big.Rat).SetFrac(new(big.Int).SetUint64(numerator), new(big.Int).SetUint64(denominator))
	retained := new(big.Rat).Sub(big.NewRat(1, 1), alpha)
	score := new(big.Rat).Set(selected)
	for fold := uint64(0); fold < decayTempos; fold++ {
		score.Mul(score, retained)
	}
	if score.Cmp(challenger) > 0 {
		return 0, errors.New("head-boundary decay does not cross the challenger")
	}
	fresh := new(big.Rat).Mul(alpha, selected)
	for folds := uint64(1); folds <= 256; folds++ {
		score.Mul(score, retained)
		score.Add(score, fresh)
		if score.Cmp(challenger) > 0 {
			return folds, nil
		}
	}
	return 0, errors.New("head-boundary EMA cannot recover above the challenger within 256 tempos")
}

func secondsToBlocksCeil(seconds int, blockSeconds uint64) (uint64, error) {
	if seconds < 0 || blockSeconds == 0 {
		return 0, errors.New("invalid seconds-to-blocks geometry")
	}
	value := uint64(seconds)
	if value == 0 {
		return 0, nil
	}
	if value > ^uint64(0)-(blockSeconds-1) {
		return 0, errors.New("seconds-to-blocks geometry overflows")
	}
	return (value + blockSeconds - 1) / blockSeconds, nil
}

// headBoundaryRecoveryGraceBlocks budgets a complete fresh trail after the
// fault is restored. The ordinary quality-fault duration is retained as
// block-level scheduler/polling margin.
func headBoundaryRecoveryGraceBlocks(cfg *ResolvedConfig) (uint64, error) {
	if cfg == nil || cfg.Config == nil || cfg.Policy == nil || cfg.Public == nil {
		return 0, errors.New("head-boundary evidence geometry is unavailable")
	}
	blockSeconds := cfg.Public.Chain.ExpectedBlockSeconds
	recoverySeconds := cfg.Policy.Verify.EgressRefreshSeconds + cfg.Policy.Verify.TrailTTLGraceSeconds + cfg.Policy.Verify.TrailDepth*(cfg.Policy.Verify.StepTimeoutSeconds+cfg.Policy.Verify.StepTimeoutGraceSeconds)
	recovery, err := secondsToBlocksCeil(recoverySeconds, blockSeconds)
	if err != nil {
		return 0, err
	}
	margin := cfg.Config.Scenarios.QualityFaultDurationBlocks
	if recovery > ^uint64(0)-margin {
		return 0, errors.New("head-boundary evidence grace overflows")
	}
	return recovery + margin, nil
}

func releaseHeadBoundaryFault(cfg *ResolvedConfig, firstOffset uint64) (scenarioFaultSpec, error) {
	if cfg == nil || cfg.Config == nil || cfg.Config.Topology.HeadFleets < 3 {
		return scenarioFaultSpec{}, errors.New("head-boundary fault requires at least three initial head fleets")
	}
	tempo := hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"])
	scores, err := testFleetPrefixScores(cfg)
	if err != nil {
		return scenarioFaultSpec{}, err
	}
	selected := scores[3]
	challenger := scores[cfg.Config.Topology.HeadFleets+1]
	decayTempos, err := headBoundaryDecayTempos(
		cfg.Policy.Steering.HeadScoreEMA.Numerator,
		cfg.Policy.Steering.HeadScoreEMA.Denominator,
		selected,
		challenger,
	)
	if err != nil {
		return scenarioFaultSpec{}, err
	}
	recoveryTempos, err := headBoundaryRecoveryTempos(
		cfg.Policy.Steering.HeadScoreEMA.Numerator,
		cfg.Policy.Steering.HeadScoreEMA.Denominator,
		selected,
		challenger,
		decayTempos,
	)
	if err != nil {
		return scenarioFaultSpec{}, err
	}
	recoveryGrace, err := headBoundaryRecoveryGraceBlocks(cfg)
	if err != nil {
		return scenarioFaultSpec{}, err
	}
	if tempo == 0 || decayTempos == ^uint64(0) || decayTempos+1 > (^uint64(0)-cfg.Config.Scenarios.QualityFaultDurationBlocks)/tempo {
		return scenarioFaultSpec{}, errors.New("head-boundary fault has an invalid approved tempo")
	}
	targets := make([]string, 0, cfg.Config.Topology.ClientsPerHeadFleet)
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		targets = append(targets, fmt.Sprintf("miner-%d", fleetMemberMinerIndex(cfg, 3, member)))
	}
	// The first native decision atomically closes the pre-fault head window. The
	// following exact decay folds see only filtered evidence. An evidence-driven
	// restore happens as soon as both validators' applied decisions prove the
	// transition; DurationBlocks remains the hard deadline.
	duration := (decayTempos+1)*tempo + cfg.Config.Scenarios.QualityFaultDurationBlocks
	campaignBlocks, ok := checkedMul(uint64(cfg.Config.Scenarios.ShortEpochs), cfg.Policy.Settlement.EpochBlocks)
	if !ok || firstOffset > campaignBlocks || duration > campaignBlocks-firstOffset {
		return scenarioFaultSpec{}, errors.New("head-boundary fault does not restore inside the accelerated acceptance window")
	}
	terminalBlocks, ok := checkedAdd(campaignBlocks, cfg.Policy.Settlement.FinalizeOffsetBlocks)
	if !ok || recoveryTempos > (^uint64(0)-recoveryGrace)/tempo {
		return scenarioFaultSpec{}, errors.New("head-boundary recovery geometry overflows")
	}
	recoveryBlocks := recoveryGrace + recoveryTempos*tempo
	faultEnd := firstOffset + duration
	if faultEnd > terminalBlocks || recoveryBlocks > terminalBlocks-faultEnd {
		return scenarioFaultSpec{}, fmt.Errorf("head-boundary recovery needs %d blocks after fault end %d, terminal offset is %d", recoveryBlocks, faultEnd, terminalBlocks)
	}
	return scenarioFaultSpec{
		ID: "head-boundary", Kind: "miner-control", Targets: targets,
		RestoreCondition: "global-head-boundary-diverged", MinimumDurationBlocks: cfg.Config.Scenarios.QualityFaultDurationBlocks,
		TriggerOffsetBlocks: firstOffset, DurationBlocks: duration,
	}, nil
}

func releaseFleetLifecycleFaults(cfg *ResolvedConfig, _ uint64) ([]scenarioFaultSpec, error) {
	if err := validateFleetLifecycleTopology(cfg.Config.Topology); err != nil {
		return nil, err
	}
	campaignBlocks, ok := checkedMul(uint64(cfg.Config.Scenarios.ShortEpochs), cfg.Policy.Settlement.EpochBlocks)
	tempo := hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"])
	revealPeriods := hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["commit_reveal_period"])
	required, scheduleErr := fleetLifecycleReleaseScheduleRequired(tempo, revealPeriods)
	if !ok || scheduleErr != nil || campaignBlocks < 2 || required <= 1 {
		return nil, errors.New("fleet lifecycle faults do not fit the accelerated acceptance window")
	}
	// These rules are armed before the accepted boundary so no validator can
	// publish an accepted decision that pays either soon-to-be-pruned UID. The
	// scheduler adopts them at offset one and removes them after the exact
	// evidence-driven restoration. Their hard bound is the separately labeled
	// release-handoff tail; no tail block is counted as an accepted epoch.
	duration := required - 1
	build := func(id string, fleet int, restoreCondition string, minimum uint64) (scenarioFaultSpec, error) {
		operator := operatorForMiner(cfg, fleetMemberMinerIndex(cfg, fleet, 1))
		if operator < 1 {
			return scenarioFaultSpec{}, errors.New("fleet lifecycle fault has no operator")
		}
		for member := 2; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			if operatorForMiner(cfg, fleetMemberMinerIndex(cfg, fleet, member)) != operator {
				return scenarioFaultSpec{}, errors.New("fleet lifecycle fault crosses operators")
			}
		}
		return scenarioFaultSpec{
			ID: id, Kind: validatorLocalHeadBoundaryFaultKind,
			Targets: []string{lifecycleValidatorViewFaultTarget(operator)}, Impacts: []string{"validator-1", "validator-2"},
			FleetIndices: []int{fleet}, PreAcceptance: true, PostAcceptanceEvidenceTail: true,
			RestoreCondition: restoreCondition, MinimumDurationBlocks: minimum,
			TriggerOffsetBlocks: 1, DurationBlocks: duration,
		}, nil
	}
	targetMinimum, ok := checkedMul(4, cfg.Policy.Settlement.EpochBlocks)
	if !ok || targetMinimum == 0 {
		return nil, errors.New("fleet lifecycle target fault geometry overflows")
	}
	targetMinimum--
	target, err := build("fleet-lifecycle-target-prune", fleetLifecycleTargetFleet, "fleet-lifecycle-provider-paid", targetMinimum)
	if err != nil {
		return nil, err
	}
	companion, err := build("fleet-lifecycle-companion-prune", fleetLifecycleCompanionFleet, "fleet-lifecycle-terminal-effective", campaignBlocks-1)
	if err != nil {
		return nil, err
	}
	return []scenarioFaultSpec{target, companion}, nil
}

func namedProcessFault(cfg *ResolvedConfig, name string) (scenarioFaultSpec, bool) {
	start := cfg.Config.Scenarios.QualityFaultStartBlocks
	duration := cfg.Config.Scenarios.QualityFaultDurationBlocks
	spec := scenarioFaultSpec{ID: name, Kind: "process-pause", TriggerOffsetBlocks: start, DurationBlocks: duration}
	switch name {
	case "fault-miner-offline":
		spec.Kind = "miner-control"
		spec.Targets = []string{fmt.Sprintf("miner-%d", cfg.Config.Topology.Miners)}
	case "fault-operator-offline":
		for _, role := range []string{"api", "connect", "taskworker"} {
			spec.Targets = append(spec.Targets, fmt.Sprintf("operator-%d-%s", cfg.Config.Scenarios.QualityFaultOperator, role))
		}
	case "fault-validator-offline":
		spec.Targets = []string{fmt.Sprintf("validator-%d", cfg.Config.Topology.Validators)}
	case "fault-claim-relayer-offline":
		spec.Targets = []string{fmt.Sprintf("claim-relayer-%d", operatorForMiner(cfg, cfg.Config.Topology.Miners))}
	case "fault-taskworker-offline":
		spec.Targets = []string{fmt.Sprintf("operator-%d-taskworker", cfg.Config.Scenarios.QualityFaultOperator)}
	default:
		return scenarioFaultSpec{}, false
	}
	return spec, true
}

func operatorDependencyImpacts(cfg *ResolvedConfig, operator int) []string {
	impacts := []string{
		fmt.Sprintf("operator-%d-api", operator),
		fmt.Sprintf("operator-%d-connect", operator),
		fmt.Sprintf("operator-%d-taskworker", operator),
		fmt.Sprintf("claim-relayer-%d", operator),
	}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		if operatorForMiner(cfg, miner) == operator {
			impacts = append(impacts, fmt.Sprintf("miner-%d", miner))
		}
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		impacts = append(impacts, fmt.Sprintf("validator-%d", validator))
	}
	sort.Strings(impacts)
	return impacts
}

func rpcProxyImpacts(cfg *ResolvedConfig) []string {
	impacts := []string{workloadRPCProxyProcessID, workloadSubstrateProcessID}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		impacts = append(impacts,
			fmt.Sprintf("operator-%d-api", operator),
			fmt.Sprintf("operator-%d-connect", operator),
			fmt.Sprintf("operator-%d-taskworker", operator),
			fmt.Sprintf("claim-relayer-%d", operator),
		)
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		impacts = append(impacts, fmt.Sprintf("validator-%d", validator))
	}
	sort.Strings(impacts)
	return impacts
}

// dependencyOutageFaults stops only simulator-owned PostgreSQL/Redis
// containers and the simulator-owned loopback RPC proxy. It never stops,
// signals, rate-limits, or firewall-blocks the shared Subtensor or MinIO
// services.
func dependencyOutageFaults(cfg *ResolvedConfig, prefix string, firstOffset uint64) []scenarioFaultSpec {
	duration := max64(5, cfg.Config.Scenarios.QualityFaultDurationBlocks)
	spacing := duration + max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks)
	var faults []scenarioFaultSpec
	index := uint64(0)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		for _, dependency := range []string{"postgres", "redis"} {
			faults = append(faults, scenarioFaultSpec{
				ID: fmt.Sprintf("%s-%s-%d", prefix, dependency, operator), Kind: "container-restart",
				Targets: []string{fmt.Sprintf("operator-%d-%s", operator, dependency)}, Impacts: operatorDependencyImpacts(cfg, operator),
				TriggerOffsetBlocks: firstOffset + spacing*index, DurationBlocks: duration,
			})
			index++
		}
	}
	faults = append(faults, scenarioFaultSpec{
		ID: prefix + "-rpc-path", Kind: "process-pause", Targets: []string{workloadRPCProxyProcessID, workloadSubstrateProcessID}, Impacts: rpcProxyImpacts(cfg),
		TriggerOffsetBlocks: firstOffset + spacing*index, DurationBlocks: duration,
	})
	return faults
}

// productionRollingFaults exercises bounded failover of every persistent
// operator, miner/claim-relayer, and validator process without overlapping
// faults. Each target must recover before the next target is paused.
func rollingProcessFaults(cfg *ResolvedConfig, prefix string, firstOffset uint64) []scenarioFaultSpec {
	return rollingProcessFaultsForTargets(cfg, prefix, firstOffset, rollingProcessTargets(cfg))
}

// rollingProcessTargets returns every persistent release workload in stable
// supervisor order so a caller may safely partition independent fault lanes.
func rollingProcessTargets(cfg *ResolvedConfig) []string {
	targets := []string{workloadRPCProxyProcessID, workloadSubstrateProcessID}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		for _, role := range []string{"api", "connect", "taskworker"} {
			targets = append(targets, fmt.Sprintf("operator-%d-%s", operator, role))
		}
	}
	for swarm := 1; swarm <= cfg.Config.Topology.MinerSwarmProcesses; swarm++ {
		targets = append(targets, fmt.Sprintf("miner-swarm-%d", swarm))
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		targets = append(targets, fmt.Sprintf("claim-relayer-%d", operator))
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		targets = append(targets, fmt.Sprintf("validator-%d", validator))
	}
	return targets
}

// rollingProcessFaultsForTargets schedules non-overlapping restart evidence
// for an explicit stable target lane.
func rollingProcessFaultsForTargets(cfg *ResolvedConfig, prefix string, firstOffset uint64, targets []string) []scenarioFaultSpec {
	duration := max64(5, cfg.Config.Scenarios.QualityFaultDurationBlocks)
	spacing := duration + max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks)
	faults := make([]scenarioFaultSpec, 0, len(targets))
	for index, target := range targets {
		faults = append(faults, scenarioFaultSpec{
			ID: fmt.Sprintf("%s-rolling-%02d", prefix, index+1), Kind: "process-restart", Targets: []string{target},
			TriggerOffsetBlocks: firstOffset + spacing*uint64(index), DurationBlocks: duration,
		})
	}
	return faults
}

func productionRollingFaults(cfg *ResolvedConfig) []scenarioFaultSpec {
	duration := max64(5, cfg.Config.Scenarios.QualityFaultDurationBlocks)
	first := duration + max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks)
	faults := dependencyOutageFaults(cfg, "production-dependency", first)
	last := faults[len(faults)-1]
	rollingFirst := last.TriggerOffsetBlocks + last.DurationBlocks + max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks)
	return append(faults, rollingProcessFaults(cfg, "production", rollingFirst)...)
}

func releaseCampaignFaults(cfg *ResolvedConfig) ([]scenarioFaultSpec, error) {
	quality, err := releaseQualityFault(cfg)
	if err != nil {
		return nil, err
	}
	spacing := max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks)
	head, err := releaseHeadBoundaryFault(cfg, quality.TriggerOffsetBlocks+quality.DurationBlocks+spacing)
	if err != nil {
		return nil, err
	}
	validatorView, err := validatorLocalHeadBoundaryFault(cfg, head)
	if err != nil {
		return nil, err
	}
	lifecycle, err := releaseFleetLifecycleFaults(cfg, head.TriggerOffsetBlocks)
	if err != nil {
		return nil, err
	}
	// The long logical-miner decay and the restart lane are independent. Run
	// them concurrently, while keeping all dependency/process restarts within
	// that second lane strictly sequential for deterministic attribution.
	firstRestart := head.TriggerOffsetBlocks + spacing
	faults := []scenarioFaultSpec{quality, validatorView, head}
	faults = append(faults, lifecycle...)
	dependencies := dependencyOutageFaults(cfg, "release-dependency", firstRestart)
	faults = append(faults, dependencies...)
	last := dependencies[len(dependencies)-1]
	rollingFirst := last.TriggerOffsetBlocks + last.DurationBlocks + max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks)
	headSwarm, err := minerSwarmFor(cfg, fleetMemberMinerIndex(cfg, 3, 1))
	if err != nil {
		return nil, err
	}
	headSwarmID := fmt.Sprintf("miner-swarm-%d", headSwarm)
	backgroundTargets := make([]string, 0, len(rollingProcessTargets(cfg))-1)
	for _, target := range rollingProcessTargets(cfg) {
		if target != headSwarmID {
			backgroundTargets = append(backgroundTargets, target)
		}
	}
	if len(backgroundTargets)+1 != len(rollingProcessTargets(cfg)) {
		return nil, fmt.Errorf("head-boundary owner %s is not exactly one persistent target", headSwarmID)
	}
	faults = append(faults, rollingProcessFaultsForTargets(cfg, "release", rollingFirst, backgroundTargets)...)
	backgroundEnd := rollingFirst
	if len(backgroundTargets) > 0 {
		backgroundEnd = faults[len(faults)-1].TriggerOffsetBlocks + faults[len(faults)-1].DurationBlocks
	}
	headEnd := head.TriggerOffsetBlocks + head.DurationBlocks
	deferredOffset := max64(headEnd, backgroundEnd) + spacing
	faults = append(faults, rollingProcessFaultsForTargets(cfg, "release-head-owner", deferredOffset, []string{headSwarmID})...)
	return faults, nil
}

// armPreAcceptanceFaults installs only explicitly marked rules before the
// post-preparation snapshot. That exact snapshot proves the filters were live
// before the next complete accepted epoch began. The ordinary scheduler later
// adopts the same authenticated faults at their in-window trigger.
func armPreAcceptanceFaults(ctx context.Context, specs []scenarioFaultSpec, driver scenarioFaultDriver) (map[string][]FaultProcessEvidence, error) {
	armed := make(map[string][]FaultProcessEvidence)
	if driver == nil {
		for _, spec := range specs {
			if spec.PreAcceptance {
				return nil, errors.New("pre-acceptance fault requires a live fault driver")
			}
		}
		return armed, nil
	}
	var applied []scenarioFaultSpec
	for _, spec := range specs {
		if !spec.PreAcceptance {
			continue
		}
		processes, err := driver.Apply(ctx, spec)
		if err != nil {
			for index := len(applied) - 1; index >= 0; index-- {
				_, _ = driver.Restore(context.Background(), applied[index])
			}
			return nil, fmt.Errorf("arm pre-acceptance fault %s: %w", spec.ID, err)
		}
		armed[spec.ID] = append([]FaultProcessEvidence(nil), processes...)
		applied = append(applied, spec)
	}
	return armed, nil
}

func initializeFaultRecords(start uint64, specs []scenarioFaultSpec) ([]ScenarioFaultRecord, error) {
	records := make([]ScenarioFaultRecord, len(specs))
	for i, spec := range specs {
		if spec.ID == "" || spec.Kind == "" || len(spec.Targets) == 0 || spec.TriggerOffsetBlocks == 0 || spec.DurationBlocks == 0 || start > ^uint64(0)-spec.TriggerOffsetBlocks || start+spec.TriggerOffsetBlocks > ^uint64(0)-spec.DurationBlocks {
			return nil, fmt.Errorf("invalid fault schedule at index %d", i)
		}
		if (spec.RestoreCondition == "" && spec.MinimumDurationBlocks != 0) || (spec.RestoreCondition != "" && spec.MinimumDurationBlocks == 0) || spec.MinimumDurationBlocks > spec.DurationBlocks {
			return nil, fmt.Errorf("invalid conditional fault schedule at index %d", i)
		}
		records[i] = ScenarioFaultRecord{
			ID: spec.ID, Kind: spec.Kind, Targets: append([]string(nil), spec.Targets...), Impacts: append([]string(nil), spec.Impacts...),
			ValidatorID: spec.ValidatorID, FleetIndex: spec.FleetIndex, FleetIndices: append([]int(nil), spec.FleetIndices...), PreAcceptance: spec.PreAcceptance, PostAcceptanceEvidenceTail: spec.PostAcceptanceEvidenceTail, ActivationCondition: spec.ActivationCondition,
			RestoreCondition: spec.RestoreCondition, MinimumDurationBlocks: spec.MinimumDurationBlocks,
			TriggerBlock: start + spec.TriggerOffsetBlocks, RestoreBlock: start + spec.TriggerOffsetBlocks + spec.DurationBlocks, Status: "pending",
		}
	}
	return records, nil
}

func advanceFaults(ctx context.Context, head ChainHead, specs []scenarioFaultSpec, records []ScenarioFaultRecord, driver scenarioFaultDriver) error {
	return advanceFaultsWhen(ctx, head, specs, records, driver, nil)
}

// advanceFaultsWhen permits evidence-triggered early restoration while keeping
// DurationBlocks as a fail-safe deadline. Only explicitly conditional faults
// consult ready, and they must remain active for their minimum duration first.
func advanceFaultsWhen(ctx context.Context, head ChainHead, specs []scenarioFaultSpec, records []ScenarioFaultRecord, driver scenarioFaultDriver, ready func(scenarioFaultSpec) (bool, error)) error {
	return advanceFaultsWithConditions(ctx, head, specs, records, driver, nil, ready)
}

// advanceFaultsWithConditions gates a pending fault on immutable scenario
// evidence before applying it. This is used when the same logical miners must
// be restored for a payout epoch and disabled again only after their new UID
// exists; it prevents overlapping control mutations on the same target.
func advanceFaultsWithConditions(ctx context.Context, head ChainHead, specs []scenarioFaultSpec, records []ScenarioFaultRecord, driver scenarioFaultDriver, activateReady, restoreReady func(scenarioFaultSpec) (bool, error)) error {
	for i := range records {
		record := &records[i]
		switch record.Status {
		case "pending":
			if head.Number < record.TriggerBlock {
				continue
			}
			if specs[i].ActivationCondition != "" {
				if activateReady == nil {
					continue
				}
				conditionMet, err := activateReady(specs[i])
				if err != nil {
					record.Status, record.Error = "failed", err.Error()
					return err
				}
				if !conditionMet {
					continue
				}
				record.ActivationConditionMet, record.ActivationConditionBlock = true, head.Number
			}
			processes, err := driver.Apply(ctx, specs[i])
			if err != nil {
				record.Status, record.Error = "failed", err.Error()
				return err
			}
			record.Status, record.AppliedBlock, record.AppliedBlockHash, record.Processes = "active", head.Number, head.Hash, processes
		case "active":
			minimumRestore, ok := checkedAdd(record.AppliedBlock, specs[i].MinimumDurationBlocks)
			if !ok {
				record.Status, record.Error = "failed", "fault minimum restoration block overflows"
				return errors.New(record.Error)
			}
			shouldRestore := head.Number >= record.RestoreBlock && (specs[i].RestoreCondition == "" || head.Number >= minimumRestore)
			if !shouldRestore && specs[i].RestoreCondition != "" && head.Number >= minimumRestore && restoreReady != nil {
				conditionMet, err := restoreReady(specs[i])
				if err != nil {
					record.Status, record.Error = "failed", err.Error()
					return err
				}
				if conditionMet {
					record.RestoreConditionMet, record.RestoreConditionBlock = true, head.Number
					shouldRestore = true
				}
			}
			if !shouldRestore {
				continue
			}
			processes, err := driver.Restore(ctx, specs[i])
			if err != nil {
				record.Status, record.Error = "failed", err.Error()
				return err
			}
			record.Status, record.RestoredBlock, record.RestoredBlockHash, record.RestoredProcesses = "restored", head.Number, head.Hash, processes
		}
	}
	return nil
}

func faultsComplete(records []ScenarioFaultRecord) bool {
	for _, record := range records {
		if record.Status != "restored" {
			return false
		}
	}
	return true
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type scenarioFaultSpec struct {
	ID                  string   `json:"id"`
	Kind                string   `json:"kind"`
	Targets             []string `json:"targets"`
	Impacts             []string `json:"impacts,omitempty"`
	TriggerOffsetBlocks uint64   `json:"trigger_offset_blocks"`
	DurationBlocks      uint64   `json:"duration_blocks"`
}

type FaultProcessEvidence struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Identity string `json:"identity"`
	PID      int    `json:"pid"`
}

type ScenarioFaultRecord struct {
	ID                string                 `json:"id"`
	Kind              string                 `json:"kind"`
	Targets           []string               `json:"targets"`
	Impacts           []string               `json:"impacts,omitempty"`
	TriggerBlock      uint64                 `json:"trigger_block"`
	RestoreBlock      uint64                 `json:"restore_block"`
	AppliedBlock      uint64                 `json:"applied_block,omitempty"`
	AppliedBlockHash  string                 `json:"applied_block_hash,omitempty"`
	RestoredBlock     uint64                 `json:"restored_block,omitempty"`
	RestoredBlockHash string                 `json:"restored_block_hash,omitempty"`
	Processes         []FaultProcessEvidence `json:"processes,omitempty"`
	RestoredProcesses []FaultProcessEvidence `json:"restored_processes,omitempty"`
	Status            string                 `json:"status"`
	Error             string                 `json:"error,omitempty"`
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
	stateDir   string
	cfg        *ResolvedConfig
	containers scenarioContainerRuntime
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
	if spec.Kind == "container-restart" {
		processes, err := d.applyContainerFault(ctx, spec)
		if err != nil {
			return nil, err
		}
		active := activeFaultFile{Schema: "urnetwork-sim-active-faults-v1", Faults: []scenarioFaultSpec{spec}, Processes: processes}
		b, _ := json.MarshalIndent(active, "", "  ")
		if err := atomicWrite(d.activePath(), append(b, '\n'), 0o600); err != nil {
			_, _ = d.restoreContainerFault(context.Background(), spec)
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
	active := activeFaultFile{Schema: "urnetwork-sim-active-faults-v1", Faults: []scenarioFaultSpec{spec}, Processes: processes}
	b, _ := json.MarshalIndent(active, "", "  ")
	if err := atomicWrite(d.activePath(), append(b, '\n'), 0o600); err != nil {
		if spec.Kind == "process-pause" {
			_, _ = d.signal(spec, syscall.SIGCONT)
		}
		return nil, err
	}
	return processes, nil
}

func (d *liveScenarioFaultDriver) Restore(ctx context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
	var processes []FaultProcessEvidence
	var err error
	if spec.Kind == "container-restart" {
		processes, err = d.restoreContainerFault(ctx, spec)
	} else if spec.Kind == "process-restart" {
		processes, err = d.waitTargetsHealthy(ctx, spec, 2*time.Minute)
	} else {
		processes, err = d.signal(spec, syscall.SIGCONT)
	}
	if err != nil {
		return nil, err
	}
	if err := os.Remove(d.activePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
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
	b, err := os.ReadFile(d.activePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var active activeFaultFile
	if json.Unmarshal(b, &active) != nil || active.Schema != "urnetwork-sim-active-faults-v1" || len(active.Faults) == 0 {
		return errors.New("invalid active fault recovery file; refusing to leave process state ambiguous")
	}
	for _, fault := range active.Faults {
		if _, err := d.Restore(ctx, fault); err != nil {
			return fmt.Errorf("recover active fault %s: %w", fault.ID, err)
		}
	}
	return nil
}

func releaseQualityFault(cfg *ResolvedConfig) (scenarioFaultSpec, error) {
	var targets []string
	for miner := cfg.Config.Topology.HeadFleets*cfg.Config.Topology.ClientsPerHeadFleet + 1; miner <= cfg.Config.Topology.Miners; miner++ {
		if operatorForMiner(cfg, miner) == cfg.Config.Scenarios.QualityFaultOperator {
			targets = append(targets, fmt.Sprintf("miner-%d", miner))
		}
	}
	if len(targets) == 0 {
		return scenarioFaultSpec{}, errors.New("quality fault has no non-head miners in the selected operator")
	}
	return scenarioFaultSpec{ID: "quality-cohort", Kind: "process-pause", Targets: targets, TriggerOffsetBlocks: cfg.Config.Scenarios.QualityFaultStartBlocks, DurationBlocks: cfg.Config.Scenarios.QualityFaultDurationBlocks}, nil
}

func namedProcessFault(cfg *ResolvedConfig, name string) (scenarioFaultSpec, bool) {
	start := cfg.Config.Scenarios.QualityFaultStartBlocks
	duration := cfg.Config.Scenarios.QualityFaultDurationBlocks
	spec := scenarioFaultSpec{ID: name, Kind: "process-pause", TriggerOffsetBlocks: start, DurationBlocks: duration}
	switch name {
	case "fault-miner-offline":
		spec.Targets = []string{fmt.Sprintf("miner-%d", cfg.Config.Topology.Miners)}
	case "fault-operator-offline":
		for _, role := range []string{"api", "connect", "taskworker"} {
			spec.Targets = append(spec.Targets, fmt.Sprintf("operator-%d-%s", cfg.Config.Scenarios.QualityFaultOperator, role))
		}
	case "fault-validator-offline":
		spec.Targets = []string{fmt.Sprintf("validator-%d", cfg.Config.Topology.Validators)}
	case "fault-claim-relayer-offline":
		spec.Targets = []string{fmt.Sprintf("miner-%d-claims", cfg.Config.Topology.Miners)}
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
	}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		if operatorForMiner(cfg, miner) == operator {
			impacts = append(impacts, fmt.Sprintf("miner-%d", miner), fmt.Sprintf("miner-%d-claims", miner))
		}
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		impacts = append(impacts, fmt.Sprintf("validator-%d", validator))
	}
	sort.Strings(impacts)
	return impacts
}

func rpcProxyImpacts(cfg *ResolvedConfig) []string {
	impacts := []string{workloadRPCProxyProcessID}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		impacts = append(impacts,
			fmt.Sprintf("operator-%d-api", operator),
			fmt.Sprintf("operator-%d-connect", operator),
			fmt.Sprintf("operator-%d-taskworker", operator),
		)
	}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		impacts = append(impacts, fmt.Sprintf("miner-%d-claims", miner))
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
		ID: prefix + "-rpc-path", Kind: "process-pause", Targets: []string{workloadRPCProxyProcessID}, Impacts: rpcProxyImpacts(cfg),
		TriggerOffsetBlocks: firstOffset + spacing*index, DurationBlocks: duration,
	})
	return faults
}

// productionRollingFaults exercises bounded failover of every persistent
// operator, miner/claim-relayer, and validator process without overlapping
// faults. Each target must recover before the next target is paused.
func rollingProcessFaults(cfg *ResolvedConfig, prefix string, firstOffset uint64) []scenarioFaultSpec {
	targets := []string{workloadRPCProxyProcessID}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		for _, role := range []string{"api", "connect", "taskworker"} {
			targets = append(targets, fmt.Sprintf("operator-%d-%s", operator, role))
		}
	}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		targets = append(targets, fmt.Sprintf("miner-%d", miner), fmt.Sprintf("miner-%d-claims", miner))
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		targets = append(targets, fmt.Sprintf("validator-%d", validator))
	}
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
	firstRestart := quality.TriggerOffsetBlocks + quality.DurationBlocks + max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks)
	faults := []scenarioFaultSpec{quality}
	dependencies := dependencyOutageFaults(cfg, "release-dependency", firstRestart)
	faults = append(faults, dependencies...)
	last := dependencies[len(dependencies)-1]
	rollingFirst := last.TriggerOffsetBlocks + last.DurationBlocks + max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks)
	faults = append(faults, rollingProcessFaults(cfg, "release", rollingFirst)...)
	return faults, nil
}

func initializeFaultRecords(start uint64, specs []scenarioFaultSpec) ([]ScenarioFaultRecord, error) {
	records := make([]ScenarioFaultRecord, len(specs))
	for i, spec := range specs {
		if spec.ID == "" || spec.Kind == "" || len(spec.Targets) == 0 || spec.TriggerOffsetBlocks == 0 || spec.DurationBlocks == 0 || start > ^uint64(0)-spec.TriggerOffsetBlocks || start+spec.TriggerOffsetBlocks > ^uint64(0)-spec.DurationBlocks {
			return nil, fmt.Errorf("invalid fault schedule at index %d", i)
		}
		records[i] = ScenarioFaultRecord{ID: spec.ID, Kind: spec.Kind, Targets: append([]string(nil), spec.Targets...), Impacts: append([]string(nil), spec.Impacts...), TriggerBlock: start + spec.TriggerOffsetBlocks, RestoreBlock: start + spec.TriggerOffsetBlocks + spec.DurationBlocks, Status: "pending"}
	}
	return records, nil
}

func advanceFaults(ctx context.Context, head ChainHead, specs []scenarioFaultSpec, records []ScenarioFaultRecord, driver scenarioFaultDriver) error {
	for i := range records {
		record := &records[i]
		switch record.Status {
		case "pending":
			if head.Number < record.TriggerBlock {
				continue
			}
			processes, err := driver.Apply(ctx, specs[i])
			if err != nil {
				record.Status, record.Error = "failed", err.Error()
				return err
			}
			record.Status, record.AppliedBlock, record.AppliedBlockHash, record.Processes = "active", head.Number, head.Hash, processes
		case "active":
			if head.Number < record.RestoreBlock {
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

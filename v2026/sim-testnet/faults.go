package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

type scenarioFaultSpec struct {
	ID                  string   `json:"id"`
	Kind                string   `json:"kind"`
	Targets             []string `json:"targets"`
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

type liveScenarioFaultDriver struct{ stateDir string }

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

func (d *liveScenarioFaultDriver) Apply(_ context.Context, spec scenarioFaultSpec) ([]FaultProcessEvidence, error) {
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
	if spec.Kind == "process-restart" {
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
		if 1+(miner-1)%cfg.Config.Topology.Operators == cfg.Config.Scenarios.QualityFaultOperator {
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

// productionRollingFaults exercises bounded failover of every persistent
// operator, miner/claim-relayer, and validator process without overlapping
// faults. Each target must recover before the next target is paused.
func rollingProcessFaults(cfg *ResolvedConfig, prefix string, firstOffset uint64) []scenarioFaultSpec {
	var targets []string
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
	return rollingProcessFaults(cfg, "production", duration+max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks))
}

func releaseCampaignFaults(cfg *ResolvedConfig) ([]scenarioFaultSpec, error) {
	quality, err := releaseQualityFault(cfg)
	if err != nil {
		return nil, err
	}
	firstRestart := quality.TriggerOffsetBlocks + quality.DurationBlocks + max64(5, cfg.Config.Scenarios.QualityFaultStartBlocks)
	faults := []scenarioFaultSpec{quality}
	faults = append(faults, rollingProcessFaults(cfg, "release", firstRestart)...)
	return faults, nil
}

func initializeFaultRecords(start uint64, specs []scenarioFaultSpec) ([]ScenarioFaultRecord, error) {
	records := make([]ScenarioFaultRecord, len(specs))
	for i, spec := range specs {
		if spec.ID == "" || spec.Kind == "" || len(spec.Targets) == 0 || spec.TriggerOffsetBlocks == 0 || spec.DurationBlocks == 0 || start > ^uint64(0)-spec.TriggerOffsetBlocks || start+spec.TriggerOffsetBlocks > ^uint64(0)-spec.DurationBlocks {
			return nil, fmt.Errorf("invalid fault schedule at index %d", i)
		}
		records[i] = ScenarioFaultRecord{ID: spec.ID, Kind: spec.Kind, Targets: append([]string(nil), spec.Targets...), TriggerBlock: start + spec.TriggerOffsetBlocks, RestoreBlock: start + spec.TriggerOffsetBlocks + spec.DurationBlocks, Status: "pending"}
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

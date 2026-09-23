// Removing the active ledger is a separate durability boundary. Retain its exact
// completed restoration intent first so a failed unlink/rename sync can resume.
package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
)

// The complete immutable specification selects the retained restoration; an
// altered schedule or target list cannot discover another fault's checkpoint.
func (self *liveScenarioFaultDriver) minerControlRemovalPath(spec scenarioFaultSpec) (string, error) {
	hash, err := canonicalHashHex(spec)
	if err != nil {
		return "", err
	}
	return filepath.Join(self.stateDir, "miner-control-restorations", stringsTrim0x(hash)+".json"), nil
}

// A checkpoint certifies only a prior live observation. Resumption restores
// recovery intent and reconciles every member again before declaring success.
func validateMinerControlRemoval(active activeFaultFile, spec scenarioFaultSpec) error {
	if spec.Kind != "miner-control" || len(active.Faults) != 1 || len(active.MinerControls) != 1 {
		return errors.New("miner restoration checkpoint has an ambiguous fault census")
	}
	if _, err := activeFaultIndex(active, spec); err != nil {
		return err
	}
	if err := validateMinerFaultControlProgress(active); err != nil {
		return err
	}
	progress := active.MinerControls[0]
	if progress.Phase != "restoring" || progress.CompletedCount != progress.Total || progress.Pending != "" || len(progress.PendingTargets) != 0 || progress.LastError != "" || len(active.Processes) != progress.Total {
		return errors.New("miner restoration checkpoint is not a complete restored census")
	}
	for _, process := range active.Processes {
		if !slices.Contains(progress.Completed, process) {
			return errors.New("miner restoration checkpoint changed its completed process identity")
		}
	}
	return nil
}

// A single immutable checkpoint covers only this restored fault. Concurrent
// faults stay solely in the ordinary active ledger and are never copied back.
func (self *liveScenarioFaultDriver) checkpointMinerControlRemoval(active activeFaultFile, spec scenarioFaultSpec, processes []FaultProcessEvidence) error {
	checkpoint := activeFaultFile{Schema: minerControlProgressSchema, Faults: []scenarioFaultSpec{spec}, Processes: slices.Clone(processes)}
	for _, progress := range active.MinerControls {
		if progress.FaultId == spec.ID {
			checkpoint.MinerControls = append(checkpoint.MinerControls, progress)
		}
	}
	if err := validateMinerControlRemoval(checkpoint, spec); err != nil {
		return err
	}
	path, err := self.minerControlRemovalPath(spec)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(raw, '\n'), 0o600)
}

// A missing or malformed completion stays hard. An exact retained completion
// may recreate only its own recovery intent, never a successful scenario result.
func (self *liveScenarioFaultDriver) resumeMinerControlRemoval(active activeFaultFile, spec scenarioFaultSpec) (activeFaultFile, error) {
	if err := validateFaultActivation(active, spec); err != nil {
		return active, err
	}
	path, err := self.minerControlRemovalPath(spec)
	if err != nil {
		return active, err
	}
	checkpoint, err := readActiveFaultFile(path)
	if err != nil {
		return active, err
	}
	if err := validateMinerControlRemoval(checkpoint, spec); err != nil {
		return active, err
	}
	upgradeMinerControlProgress(&active)
	active.Faults = append(active.Faults, spec)
	active.Processes = append(active.Processes, checkpoint.Processes...)
	active.MinerControls = append(active.MinerControls, checkpoint.MinerControls...)
	if err := validateMinerFaultControlProgress(active); err != nil {
		return active, err
	}
	raw, err := json.MarshalIndent(active, "", "  ")
	if err != nil {
		return active, err
	}
	if err := atomicWrite(self.activePath(), append(raw, '\n'), 0o600); err != nil {
		return active, err
	}
	return active, nil
}

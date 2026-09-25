// A non-mutating lifecycle exception may release its local filters after the
// signed interval. Cleanup remains distinct from the unproved lifecycle event.
package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"
)

const scenarioLifecycleCleanupSchema = "urnetwork-sim-lifecycle-cleanup-v1"

// The owner signs the request before removing a filter, retains the exact
// removal census, and completes it only from a subsequent full observation.
type ScenarioLifecycleCleanup struct {
	Schema                   string                 `json:"schema"`
	LifecycleHandoffHash     string                 `json:"lifecycle_handoff_hash"`
	RequestedHead            ChainHead              `json:"requested_head"`
	RequestedObservationHash string                 `json:"requested_observation_hash"`
	RequestedAt              string                 `json:"requested_at"`
	RemovedProcesses         []FaultProcessEvidence `json:"removed_processes,omitempty"`
	CompletedObservationHash string                 `json:"completed_observation_hash,omitempty"`
}

// The active lifecycle owner grants the exact terminal cleanup scope.
type scenarioLifecycleCleanupAuthority interface {
	provisionalTerminalCleanup(*scenarioCampaignAttempt, *ScenarioAcceptanceWindow, *ScenarioObservation) (*ScenarioLifecycleHandoff, error)
}

// Only an authenticated live lifecycle owner can supply this authority. A
// public observation's bypass flag alone never grants mutation permission.
func (self *liveFleetLifecycle) provisionalTerminalCleanup(attempt *scenarioCampaignAttempt, window *ScenarioAcceptanceWindow, observation *ScenarioObservation) (*ScenarioLifecycleHandoff, error) {
	if self == nil || self.evidence == nil || self.evidence.ProvisionalBypass == nil || !provisionalResumeEnabled(self.cfg) {
		return nil, nil
	}
	if self.phase != "release-1.0" || !self.Complete() || attempt == nil || self.attempt != attempt || attempt.payload.Phase != self.phase || attempt.payload.AcceptanceBoundary == nil || attempt.payload.AcceptanceInvalidation != "" || !scenarioAcceptanceWindowsEqual(window, &attempt.payload.AcceptanceBoundary.AcceptanceWindow) || !scenarioAcceptanceIntervalObserved(window, observation) {
		return nil, errors.New("provisional lifecycle cleanup requires its live signed terminal interval")
	}
	if !fleetLifecycleCanonicalEqual(observation.FleetLifecycle, self.evidence) {
		return nil, errors.New("provisional lifecycle cleanup observation differs from authenticated history")
	}
	if err := self.validateProvisionalBypassState(self.phase, self.evidence.RunID, self.evidence); err != nil {
		return nil, fmt.Errorf("provisional lifecycle cleanup authority: %w", err)
	}
	runDir := filepath.Join(self.stateDir, "runs", attempt.payload.RunID)
	binding, err := captureScenarioLifecycleHandoff(self.cfg, self.stateDir, runDir, attempt.payload.RunID, attempt)
	if err != nil {
		return nil, err
	}
	canonical, err := fleetLifecycleCanonicalBytes(self.evidence)
	if err != nil || binding.ContentHash != bytesSHA256(canonical) {
		return nil, stateMismatchError(err, "provisional lifecycle cleanup source changed during capture")
	}
	return binding, nil
}

// Recognize only scheduled lifecycle filters; this shape alone is not authority.
func lifecycleCleanupFault(record ScenarioFaultRecord) bool {
	condition := ""
	switch record.ID {
	case "fleet-lifecycle-target-prune":
		condition = "fleet-lifecycle-provider-paid"
	case "fleet-lifecycle-companion-prune":
		condition = "fleet-lifecycle-terminal-effective"
	default:
		return false
	}
	return record.Kind == validatorLocalHeadBoundaryFaultKind && record.PreAcceptance && record.PostAcceptanceEvidenceTail && record.RestoreCondition == condition
}

// Validate the retained request, exact owner census and later observation shape.
func validateScenarioLifecycleCleanup(window *ScenarioAcceptanceWindow, record ScenarioFaultRecord) error {
	proof := record.LifecycleCleanup
	if proof == nil {
		return nil
	}
	minimum, ok := checkedAdd(record.AppliedBlock, record.MinimumDurationBlocks)
	_, timeErr := time.Parse(time.RFC3339Nano, proof.RequestedAt)
	if window == nil || !lifecycleCleanupFault(record) || record.RestoreConditionMet || record.RestoreConditionBlock != 0 || proof.Schema != scenarioLifecycleCleanupSchema || !validSHA256ContentHash(proof.LifecycleHandoffHash) || !validCanonicalHashHex(proof.RequestedObservationHash) || !validCanonicalHashHex(proof.RequestedHead.Hash) || timeErr != nil || !ok || proof.RequestedHead.Number < minimum || proof.RequestedHead.Number < window.TerminalBlock || (record.Status != "active" && record.Status != "restored") {
		return errors.New("provisional lifecycle cleanup has foreign authority, timing or condition evidence")
	}
	if len(proof.RemovedProcesses) != 0 {
		if len(proof.RemovedProcesses) != len(record.Targets) || len(record.Processes) != len(record.Targets) {
			return errors.New("provisional lifecycle cleanup removal census is incomplete")
		}
		for index, process := range proof.RemovedProcesses {
			original := record.Processes[index]
			if process.ID != record.Targets[index] || original.ID != process.ID || process.PID <= 1 || process.Role == "" || process.Identity == "" || process.Role != original.Role || process.Identity != original.Identity {
				return errors.New("provisional lifecycle cleanup removal census changed its target")
			}
		}
	}
	if record.Status == "restored" {
		if len(proof.RemovedProcesses) == 0 || !slices.Equal(record.RestoredProcesses, proof.RemovedProcesses) || record.RestoredBlock < proof.RequestedHead.Number || !validCanonicalHashHex(proof.CompletedObservationHash) || proof.CompletedObservationHash == proof.RequestedObservationHash {
			return errors.New("provisional lifecycle cleanup lacks a later complete restoration observation")
		}
	} else if proof.CompletedObservationHash != "" {
		return errors.New("active provisional lifecycle cleanup claims completion")
	}
	return nil
}

// Previously signed cleanup fields are immutable as each later stage is added.
func validateScenarioLifecycleCleanupProgress(before, after *ScenarioLifecycleCleanup) error {
	if before == nil {
		return nil
	}
	if after == nil || before.Schema != after.Schema || before.LifecycleHandoffHash != after.LifecycleHandoffHash || before.RequestedHead != after.RequestedHead || before.RequestedObservationHash != after.RequestedObservationHash || before.RequestedAt != after.RequestedAt || len(before.RemovedProcesses) != 0 && !slices.Equal(before.RemovedProcesses, after.RemovedProcesses) || before.CompletedObservationHash != "" && before.CompletedObservationHash != after.CompletedObservationHash {
		return errors.New("provisional lifecycle cleanup evidence moved backward or changed identity")
	}
	return nil
}

// Only the exact two scheduled local filters can enter this path. All other
// faults continue under the ordinary scheduler, including unknown kinds.
func advanceScenarioLifecycleCleanup(ctx context.Context, cfg *ResolvedConfig, window *ScenarioAcceptanceWindow, current *ScenarioObservation, binding *ScenarioLifecycleHandoff, records []ScenarioFaultRecord, driver scenarioFaultDriver, checkpoint func() error) error {
	if window == nil || !provisionalResumeEnabled(cfg) || binding == nil || !scenarioAcceptanceIntervalObserved(window, current) {
		return errors.New("provisional lifecycle cleanup has no terminal authority")
	}
	if driver == nil || checkpoint == nil || !validSHA256ContentHash(binding.ContentHash) || !validCanonicalHashHex(current.ObservationHash) {
		return errors.New("provisional lifecycle cleanup dependencies are incomplete")
	}
	specs, err := releaseFleetLifecycleFaults(cfg, window.EpochBlocks)
	if err != nil {
		return err
	}
	expected, err := initializeFaultRecords(window.StartBlock, specs)
	if err != nil {
		return err
	}
	indices := make([]int, len(specs))
	for index, want := range expected {
		indices[index] = -1
		for candidate, record := range records {
			if record.ID != want.ID {
				continue
			}
			if indices[index] >= 0 || !scenarioFaultRecordMatchesSchedule(record, want) || record.Status != "active" && record.Status != "restored" {
				return errors.New("provisional lifecycle cleanup differs from the exact scheduled filters")
			}
			indices[index] = candidate
		}
		if indices[index] < 0 {
			return errors.New("provisional lifecycle cleanup is missing an exact scheduled filter")
		}
	}
	for index, at := range indices {
		record := &records[at]
		if record.Status == "restored" {
			continue
		}
		minimum, ok := checkedAdd(record.AppliedBlock, record.MinimumDurationBlocks)
		if !ok || current.Status.Contracts.FinalizedHead.Number < minimum || record.RestoreConditionMet {
			continue
		}
		if record.LifecycleCleanup == nil {
			record.LifecycleCleanup = &ScenarioLifecycleCleanup{Schema: scenarioLifecycleCleanupSchema, LifecycleHandoffHash: binding.ContentHash, RequestedHead: current.Status.Contracts.FinalizedHead, RequestedObservationHash: current.ObservationHash, RequestedAt: current.ObservedAt}
			if err := validateScenarioLifecycleCleanup(window, *record); err != nil {
				return err
			}
			if err := checkpoint(); err != nil {
				return err
			}
		}
		proof := record.LifecycleCleanup
		if proof.LifecycleHandoffHash != binding.ContentHash {
			return errors.New("provisional lifecycle cleanup changed its retained handoff")
		}
		if len(proof.RemovedProcesses) == 0 {
			restore := driver.Restore
			if reconciler, ok := driver.(scenarioLifecycleCleanupRestorer); ok {
				restore = reconciler.restoreLifecycleCleanup
			}
			processes, err := restore(ctx, specs[index])
			if err != nil {
				return err
			}
			proof.RemovedProcesses = append([]FaultProcessEvidence(nil), processes...)
			if len(processes) == 0 {
				return errors.New("provisional lifecycle cleanup produced no removal census")
			}
			if err := validateScenarioLifecycleCleanup(window, *record); err != nil {
				return err
			}
			if err := checkpoint(); err != nil {
				return err
			}
			continue
		}
		requested, _ := time.Parse(time.RFC3339Nano, proof.RequestedAt)
		observed, err := time.Parse(time.RFC3339Nano, current.ObservedAt)
		if err != nil || !observed.After(requested) || current.ObservationHash == proof.RequestedObservationHash || current.Status.Contracts.FinalizedHead.Number < proof.RequestedHead.Number {
			continue
		}
		record.Status, record.RestoredBlock, record.RestoredBlockHash = "restored", current.Status.Contracts.FinalizedHead.Number, current.Status.Contracts.FinalizedHead.Hash
		record.RestoredProcesses = append([]FaultProcessEvidence(nil), proof.RemovedProcesses...)
		record.Error = ""
		proof.CompletedObservationHash = current.ObservationHash
		if err := validateScenarioLifecycleCleanup(window, *record); err != nil {
			return err
		}
		if err := checkpoint(); err != nil {
			return err
		}
	}
	return nil
}

// Completing diagnostic work does not turn any failed assertion into a pass.
// The existing provisional production integrity and restored-fault gates still
// decide whether a later, non-accepting production workload may begin.
func scenarioLifecycleOperationalCompletion(cfg *ResolvedConfig, definition scenarioDefinition, window *ScenarioAcceptanceWindow, current *ScenarioObservation, binding *ScenarioLifecycleHandoff, records []ScenarioFaultRecord, assertions []AssertionRecord) bool {
	if window == nil || !provisionalResumeEnabled(cfg) || definition.Name != "release-1.0" || binding == nil || !scenarioAcceptanceIntervalObserved(window, current) || !faultsComplete(records) {
		return false
	}
	for _, id := range provisionalProductionIntegrityAssertionIDs() {
		found := false
		for _, assertion := range assertions {
			if assertion.ID == id {
				found = assertion.Passed
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// A later provisional production handoff authenticates both signed observation
// cuts and the exact bypass bytes; a cleanup annotation cannot invent either.
func validateProvisionalLifecycleCleanupHistory(cfg *ResolvedConfig, result *ScenarioResult, history []*ScenarioObservation) error {
	byHash := make(map[string]*ScenarioObservation, len(history))
	for _, observation := range history {
		if observation != nil {
			byHash[observation.ObservationHash] = observation
		}
	}
	for _, record := range result.Faults {
		proof := record.LifecycleCleanup
		if proof == nil {
			continue
		}
		if err := validateScenarioLifecycleCleanup(result.AcceptanceWindow, record); err != nil {
			return err
		}
		if result.LifecycleHandoff == nil || proof.LifecycleHandoffHash != result.LifecycleHandoff.ContentHash {
			return errors.New("provisional lifecycle cleanup names a foreign handoff")
		}
		requested, complete := byHash[proof.RequestedObservationHash], byHash[proof.CompletedObservationHash]
		if requested == nil || complete == nil || !scenarioAcceptanceIntervalObserved(result.AcceptanceWindow, requested) || !scenarioAcceptanceIntervalObserved(result.AcceptanceWindow, complete) || requested.ObservedAt != proof.RequestedAt || requested.Status.Contracts.FinalizedHead != proof.RequestedHead || complete.Status.Contracts.FinalizedHead != (ChainHead{Number: record.RestoredBlock, Hash: record.RestoredBlockHash}) {
			return errors.New("provisional lifecycle cleanup is absent from signed terminal observations")
		}
		requestedAt, err := time.Parse(time.RFC3339Nano, requested.ObservedAt)
		completeAt, completeErr := time.Parse(time.RFC3339Nano, complete.ObservedAt)
		if err != nil || completeErr != nil || !completeAt.After(requestedAt) {
			return errors.New("provisional lifecycle cleanup completion does not follow its request")
		}
		for _, observation := range []*ScenarioObservation{requested, complete} {
			evidence := observation.FleetLifecycle
			if evidence == nil || evidence.ProvisionalBypass == nil || !evidence.ProvisionalBypass.Provisional || evidence.ProvisionalBypass.FinalAcceptance || evidence.TerminalEffectiveEpoch != 0 || evidence.Stage != fleetLifecycleStageReleaseHandoff {
				return errors.New("provisional lifecycle cleanup observation lacks its non-mutating bypass")
			}
			raw, err := fleetLifecycleCanonicalBytes(evidence)
			if err != nil || validateScenarioLifecycleHandoffBinding(cfg, *result.LifecycleHandoff, raw) != nil {
				return stateMismatchError(err, "provisional lifecycle cleanup observation differs from retained handoff bytes")
			}
		}
	}
	return nil
}

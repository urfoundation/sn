package main

// This file authenticates the only fleet state transition that may consume a
// generation-1 setup postcondition: an exact generation-1 install/convergence
// batch followed by the exact generation-2 refresh for the same fleet range.

import (
	"errors"
	"fmt"
	"strings"
)

// Identifies one supported source within its immutable ten-fleet batch.
type fleetGenerationOneActionCoordinates struct {
	Batch      int
	FirstFleet int
	LastFleet  int
	Fleet      int
	Member     int
	Install    bool
	Alias      bool
}

// Accept only a canonical install, legacy per-fleet write, or atomic-install
// read proof. Challenger fleets and future action shapes remain live-checked.
func fleetGenerationOneCoordinates(cfg *ResolvedConfig, action Action) (fleetGenerationOneActionCoordinates, bool, error) {
	coordinates := fleetGenerationOneActionCoordinates{}
	if cfg == nil || cfg.Config == nil || cfg.Config.Topology.HeadFleets < 1 || cfg.Config.Topology.ClientsPerHeadFleet < 1 {
		return coordinates, false, errors.New("fleet generation-1 topology is unavailable")
	}
	switch {
	case strings.HasPrefix(action.ID, "fleet.install.batch."):
		batch := suffixInt(action.ID)
		if batch < 1 || action.ID != fmt.Sprintf("fleet.install.batch.%d", batch) {
			return coordinates, true, fmt.Errorf("fleet install action %q is not canonical", action.ID)
		}
		firstFleet, lastFleet, err := fleetInstallActionRange(cfg, action, batch)
		if err != nil {
			return coordinates, true, err
		}
		if _, err := fleetBatcherAddressForAction(action); err != nil {
			return coordinates, true, err
		}
		return fleetGenerationOneActionCoordinates{Batch: batch, FirstFleet: firstFleet, LastFleet: lastFleet, Install: true}, true, nil
	case strings.HasPrefix(action.ID, "fleet.mirror."), strings.HasPrefix(action.ID, "fleet.bind."):
	default:
		return coordinates, false, nil
	}

	if action.Parameters["batch_installed"] == "true" {
		batch, fleet, member, err := fleetInstallAliasIndices(cfg, action)
		if err != nil {
			return coordinates, true, err
		}
		if !spendIsZero(action.Spend) {
			return coordinates, true, fmt.Errorf("fleet install alias %s has nonzero spend", action.ID)
		}
		if member == 0 && action.Parameters[fleetCommitmentStorageParameter] != fleetCommitmentStorageV2 {
			return coordinates, true, errors.New("fleet mirror alias has an invalid commitment storage schema")
		}
		firstFleet := (batch-1)*fleetRefreshBatchSize + 1
		lastFleet := min(firstFleet+fleetRefreshBatchSize-1, cfg.Config.Topology.HeadFleets)
		return fleetGenerationOneActionCoordinates{Batch: batch, FirstFleet: firstFleet, LastFleet: lastFleet, Fleet: fleet, Member: member, Alias: true}, true, nil
	}
	if _, present := action.Parameters["batch_installed"]; present {
		return coordinates, true, fmt.Errorf("fleet generation-1 action %s has an invalid batch_installed marker", action.ID)
	}
	if action.Kind != "evm-transaction" || action.Spend.TAORao != 0 || action.Spend.AlphaRao != 0 || action.Spend.Registrations != 0 || action.Spend.SubnetCreations != 0 {
		return coordinates, true, fmt.Errorf("legacy fleet generation-1 action %s has an invalid transaction shape", action.ID)
	}
	if _, _, err := evmActionFeeEnvelope(action); err != nil {
		return coordinates, true, err
	}

	fleet, member := 0, 0
	if strings.HasPrefix(action.ID, "fleet.mirror.") {
		var err error
		fleet, err = fleetMirrorRecoveryIndex(action.ID, cfg.Config.Topology.fleetCandidates())
		if err != nil {
			return coordinates, true, err
		}
		if fleet > cfg.Config.Topology.HeadFleets {
			return coordinates, false, nil
		}
		if action.Target != fmt.Sprintf("head-fleet:%d", fleet) || action.Parameters[fleetCommitmentStorageParameter] != fleetCommitmentStorageV2 {
			return coordinates, true, errors.New("legacy fleet mirror has an invalid target or commitment storage schema")
		}
	} else {
		var err error
		fleet, member, err = fleetBindingActionIndices(action.ID)
		if err != nil || action.ID != fmt.Sprintf("fleet.bind.%d.%d", fleet, member) {
			return coordinates, true, stateMismatchError(err, "legacy fleet binding %s is not canonical", action.ID)
		}
		if fleet > cfg.Config.Topology.HeadFleets {
			return coordinates, false, nil
		}
		if member > cfg.Config.Topology.ClientsPerHeadFleet || action.Target != fmt.Sprintf("miner:%d", fleetMemberMinerIndex(cfg, fleet, member)) {
			return coordinates, true, errors.New("legacy fleet binding is outside the head-fleet topology")
		}
	}
	batch := 1 + (fleet-1)/fleetRefreshBatchSize
	firstFleet := (batch-1)*fleetRefreshBatchSize + 1
	lastFleet := min(firstFleet+fleetRefreshBatchSize-1, cfg.Config.Topology.HeadFleets)
	return fleetGenerationOneActionCoordinates{Batch: batch, FirstFleet: firstFleet, LastFleet: lastFleet, Fleet: fleet, Member: member}, true, nil
}

// Check the exact hash-bound receipt identity and dual-observer equality used
// by a successor chain before interpreting any compact observation fields.
func validateFleetSupersessionPostcondition(cfg *ResolvedConfig, action Action, entry JournalEntry, record *ActionPostcondition) error {
	if cfg == nil || cfg.Config == nil || entry.Sequence == 0 || entry.Stage != StageVerified || entry.ActionID != action.ID || !actionAcceptsIntent(action, entry.IntentHash) {
		return fmt.Errorf("fleet action %s has no exact verified journal identity", action.ID)
	}
	if record == nil || (record.Schema != "urnetwork-sim-action-postcondition-v3" && record.Schema != "urnetwork-sim-action-postcondition-v4") ||
		record.DeploymentID != cfg.Config.Deployment.DeploymentID || record.PlanHash != entry.PlanHash || record.ActionID != entry.ActionID || record.IntentHash != entry.IntentHash {
		return fmt.Errorf("fleet action %s has no exact replayable postcondition identity", action.ID)
	}
	if record.EVMFinalized.Number == 0 || record.EVMFinalized.Hash == "" || record.IndependentEVMFinalized.Number == 0 || record.IndependentEVMFinalized.Hash == "" {
		return fmt.Errorf("fleet action %s has incomplete EVM checkpoints", action.ID)
	}
	if err := observedPostconditionMatches(record.Observed, record.IndependentObserved); err != nil {
		return fmt.Errorf("fleet action %s observer evidence: %w", action.ID, err)
	}
	return nil
}

// Compare a JSON-preserved batch counter without lossy float conversion.
func fleetObservedUint64(observed map[string]any, key string, want uint64) error {
	value, err := fleetInstallObservedUint64(observed[key])
	if err != nil || value != want {
		return stateMismatchError(err, "fleet successor %s=%d want %d", key, value, want)
	}
	return nil
}

// Compare an approval-bound string in one compact batch observation.
func fleetObservedString(observed map[string]any, key, want string) error {
	value, ok := observed[key].(string)
	if !ok || value != want {
		return fmt.Errorf("fleet successor %s=%q want %q", key, value, want)
	}
	return nil
}

// Authenticate the compact batch observations before they are allowed to
// authorize historical replay of hundreds of per-fleet actions. Each batch's
// full on-chain state is independently replayed once by its own carried audit.
func validateFleetSupersessionBatchObservation(action Action, record *ActionPostcondition, batch, firstFleet, lastFleet int, generation uint64, membersPerFleet int) error {
	if record == nil || batch < 1 || firstFleet < 1 || lastFleet < firstFleet || membersPerFleet < 1 {
		return errors.New("fleet successor batch observation context is incomplete")
	}
	for _, observed := range []map[string]any{record.Observed, record.IndependentObserved} {
		for key, want := range map[string]uint64{
			"batch": uint64(batch), "first_fleet": uint64(firstFleet), "last_fleet": uint64(lastFleet), "generation": generation,
		} {
			if err := fleetObservedUint64(observed, key, want); err != nil {
				return err
			}
		}
		if err := fleetObservedString(observed, "kind", action.Kind); err != nil {
			return err
		}
		if err := fleetObservedString(observed, "target", action.Target); err != nil {
			return err
		}
		fleetCount := uint64(lastFleet - firstFleet + 1)
		if err := fleetObservedUint64(observed, "members", fleetCount*uint64(membersPerFleet)); err != nil {
			return err
		}
		if generation == 1 {
			installed, err := fleetInstallObservedUint64(observed["installed_fleets"])
			if err != nil {
				return err
			}
			carried, err := fleetInstallObservedUint64(observed["carried_fleets"])
			if err != nil || installed > fleetCount || carried != fleetCount-installed {
				return stateMismatchError(err, "fleet install partition %d+%d want %d", installed, carried, fleetCount)
			}
			transactionHash, ok := observed["transaction_hash"].(string)
			if !ok || (installed > 0 && transactionHash == "") {
				return errors.New("fleet install transaction observation is inconsistent with its installed partition")
			}
			if transactionHash != "" {
				if _, err := decodeHex32("fleet install transaction", transactionHash); err != nil {
					return err
				}
			}
			continue
		}
		if err := fleetObservedUint64(observed, "fleets", fleetCount); err != nil {
			return err
		}
		for _, key := range []string{"transaction_hash", "calldata_hash"} {
			value, ok := observed[key].(string)
			if !ok {
				return fmt.Errorf("fleet refresh %s is unavailable", key)
			}
			if _, err := decodeHex32("fleet refresh "+key, value); err != nil {
				return err
			}
		}
		effective, err := fleetInstallObservedUint64(observed["effective_epoch"])
		if err != nil || effective == 0 {
			return stateMismatchError(err, "fleet refresh effective epoch is invalid")
		}
		validTo, err := fleetInstallObservedUint64(observed["valid_to_epoch"])
		if err != nil || validTo < effective {
			return stateMismatchError(err, "fleet refresh validity ends before it begins")
		}
	}
	return nil
}

// Require both operational and comparison observers to advance monotonically.
func fleetCheckpointNotBefore(later, earlier *ActionPostcondition) bool {
	return later != nil && earlier != nil && later.EVMFinalized.Number >= earlier.EVMFinalized.Number && later.IndependentEVMFinalized.Number >= earlier.IndependentEVMFinalized.Number
}

// Require exact journal/checkpoint order and an identical batch/deployment
// envelope. A verified adjacent batch, partial refresh, or earlier observation
// cannot convert a stale live mismatch into historical success.
func validateFleetGenerationOneSupersession(
	cfg *ResolvedConfig,
	coordinates fleetGenerationOneActionCoordinates,
	sourceAction Action,
	sourceEntry JournalEntry,
	sourceRecord *ActionPostcondition,
	installAction Action,
	installEntry JournalEntry,
	installRecord *ActionPostcondition,
	refreshAction Action,
	refreshEntry JournalEntry,
	refreshRecord *ActionPostcondition,
) error {
	for _, evidence := range []struct {
		action Action
		entry  JournalEntry
		record *ActionPostcondition
	}{
		{action: sourceAction, entry: sourceEntry, record: sourceRecord},
		{action: installAction, entry: installEntry, record: installRecord},
		{action: refreshAction, entry: refreshEntry, record: refreshRecord},
	} {
		if err := validateFleetSupersessionPostcondition(cfg, evidence.action, evidence.entry, evidence.record); err != nil {
			return err
		}
	}
	installFirst, installLast, err := fleetInstallActionRange(cfg, installAction, coordinates.Batch)
	if err != nil {
		return err
	}
	refreshFirst, refreshLast, err := fleetRefreshActionRange(cfg, refreshAction, coordinates.Batch)
	if err != nil {
		return err
	}
	installTarget, err := fleetBatcherAddressForAction(installAction)
	if err != nil {
		return err
	}
	refreshTarget, err := fleetBatcherAddressForAction(refreshAction)
	if err != nil {
		return err
	}
	if installFirst != coordinates.FirstFleet || installLast != coordinates.LastFleet || refreshFirst != coordinates.FirstFleet || refreshLast != coordinates.LastFleet || installTarget != refreshTarget {
		return errors.New("fleet generation-1 successor range or batcher differs from its source")
	}
	manifestHash := installAction.Parameters[deploymentManifestHashParameter]
	if manifestHash == "" || refreshAction.Parameters[deploymentManifestHashParameter] != manifestHash || sourceAction.Parameters[deploymentManifestHashParameter] != manifestHash {
		return errors.New("fleet generation-1 successor deployment manifest differs from its source")
	}
	if err := validateFleetSupersessionBatchObservation(installAction, installRecord, coordinates.Batch, coordinates.FirstFleet, coordinates.LastFleet, 1, cfg.Config.Topology.ClientsPerHeadFleet); err != nil {
		return fmt.Errorf("fleet install successor observation: %w", err)
	}
	if err := validateFleetSupersessionBatchObservation(refreshAction, refreshRecord, coordinates.Batch, coordinates.FirstFleet, coordinates.LastFleet, 2, cfg.Config.Topology.ClientsPerHeadFleet); err != nil {
		return fmt.Errorf("fleet refresh successor observation: %w", err)
	}
	if refreshEntry.Sequence <= installEntry.Sequence || refreshEntry.Sequence <= sourceEntry.Sequence || !fleetCheckpointNotBefore(refreshRecord, installRecord) || !fleetCheckpointNotBefore(refreshRecord, sourceRecord) {
		return errors.New("fleet generation-2 refresh does not follow the generation-1 evidence")
	}
	if coordinates.Install {
		if sourceEntry.Sequence != installEntry.Sequence || sourceEntry.PlanHash != installEntry.PlanHash || sourceEntry.IntentHash != installEntry.IntentHash {
			return errors.New("fleet install source differs from its successor-chain install evidence")
		}
	} else if coordinates.Alias {
		if sourceEntry.Sequence <= installEntry.Sequence || sourceEntry.Sequence >= refreshEntry.Sequence ||
			sourceRecord.EVMFinalized != installRecord.EVMFinalized || sourceRecord.IndependentEVMFinalized != installRecord.IndependentEVMFinalized {
			return errors.New("fleet install alias does not follow its exact batch checkpoint")
		}
		for _, observed := range []map[string]any{sourceRecord.Observed, sourceRecord.IndependentObserved} {
			if err := fleetObservedString(observed, "source_action", installAction.ID); err != nil {
				return err
			}
			if err := fleetObservedString(observed, "source_postcondition_hash", installEntry.PostconditionHash); err != nil {
				return err
			}
			if err := fleetObservedUint64(observed, "batch", uint64(coordinates.Batch)); err != nil {
				return err
			}
		}
	} else if installEntry.Sequence <= sourceEntry.Sequence || !fleetCheckpointNotBefore(installRecord, sourceRecord) {
		return errors.New("fleet generation-1 convergence batch does not follow the legacy write")
	}
	return nil
}

// Resolve and authenticate the exact install/refresh chain for one carried
// generation-1 action. Absence of a refresh retains ordinary live checking;
// any partial or malformed successor fails closed.
func (self *Executor) fleetGenerationOneActionSuperseded(action Action, verified JournalEntry, record *ActionPostcondition) (bool, error) {
	if self == nil {
		return false, errors.New("fleet generation-1 successor context is unavailable")
	}
	coordinates, applicable, err := fleetGenerationOneCoordinates(self.cfg, action)
	if err != nil || !applicable {
		return false, err
	}
	if self.plan == nil || self.journal == nil {
		return false, errors.New("fleet generation-1 successor context is unavailable")
	}
	installAction := action
	if !coordinates.Install {
		installAction, err = self.planAction(fmt.Sprintf("fleet.install.batch.%d", coordinates.Batch))
		if err != nil {
			return false, err
		}
	}
	refreshVerified, err := self.fleetInstallBatchSuperseded(installAction)
	if err != nil || !refreshVerified {
		return false, err
	}
	refreshAction, err := self.planAction(fmt.Sprintf("fleet.refresh.batch.%d", coordinates.Batch))
	if err != nil {
		return false, err
	}
	installEntry, installVerified := self.verifiedActionEntry(installAction)
	if coordinates.Install {
		installEntry, installVerified = verified, true
	}
	refreshEntry, refreshFound := self.verifiedActionEntry(refreshAction)
	if !installVerified || !refreshFound {
		return false, errors.New("fleet generation-1 successor chain is only partially verified")
	}
	installRecord := record
	if !coordinates.Install {
		installRecord, err = self.readPersistedPostcondition(installEntry)
		if err != nil {
			return false, fmt.Errorf("fleet generation-1 install postcondition: %w", err)
		}
	}
	refreshRecord, err := self.readPersistedPostcondition(refreshEntry)
	if err != nil {
		return false, fmt.Errorf("fleet generation-2 refresh postcondition: %w", err)
	}
	if err := validateFleetGenerationOneSupersession(
		self.cfg, coordinates, action, verified, record,
		installAction, installEntry, installRecord,
		refreshAction, refreshEntry, refreshRecord,
	); err != nil {
		return false, err
	}
	return true, nil
}

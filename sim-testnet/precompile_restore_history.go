// Completed fleet renewal supersedes the precompile drill's restored native
// value. Replay retains the exact restore checkpoint and phase receipt.
package main

import (
	"context"
	"errors"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Separates the pinned restore proof from the ordinary current-state check.
type precompileCommitmentReader interface {
	fleetCommitmentAt([32]byte, types.Hash) (*crv4.FinalizedCommitment, error)
	fleetCommitmentFinalized([32]byte) (*crv4.FinalizedCommitment, error)
}

// Historical callers must first authenticate the restore transaction and its
// completed renewal. Ordinary callers continue requiring the current value.
func verifyPrecompileRestoreState(reader precompileCommitmentReader, hotkey [32]byte, commitment PrecompileCommitmentEvidence, historical bool) error {
	canonical, err := decodeHex32("precompile canonical commitment", commitment.CanonicalHash)
	if err != nil || commitment.CanonicalGeneration != precompileCanonicalFleetGeneration {
		return errors.New("precompile restore has no canonical generation-2 commitment")
	}
	if reader == nil {
		return errors.New("precompile restore native reader is unavailable")
	}
	if !historical {
		current, err := reader.fleetCommitmentFinalized(hotkey)
		if err != nil || current == nil || current.Hash != canonical || current.CommitmentBlock != commitment.RestoreCommitmentBlock {
			return conformanceMismatch("restored generation-2 commitment is not current finalized state", err)
		}
		return nil
	}
	blockHash, err := types.NewHashFromHexString(commitment.RestoreFinalizedHead.Hash)
	if err != nil || blockHash == (types.Hash{}) || commitment.RestoreFinalizedHead.Number == 0 || commitment.RestoreCommitmentBlock != commitment.RestoreFinalizedHead.Number {
		return errors.New("precompile restore checkpoint differs from its exact native write")
	}
	observed, err := reader.fleetCommitmentAt(hotkey, blockHash)
	if err != nil {
		return err
	}
	if err := crv4.ValidateFleetCommitmentWrite(canonical, commitment.RestoreFinalizedHead.Number, observed); err != nil {
		return err
	}
	if observed.FinalizedHash != blockHash {
		return errors.New("precompile restore native proof changed its recorded block hash")
	}
	return nil
}

// Later phases append to one evidence file. Recover only the known restore
// snapshot when the current file is newer; the complete recorded map must match.
func precompileRestoreHistoricalObservation(action Action, evidence *PrecompileConformanceEvidence, record *ActionPostcondition) (map[string]any, error) {
	if evidence == nil || record == nil {
		return nil, errors.New("precompile restore historical observation is unavailable")
	}
	snapshot := *evidence
	if record.Observed["evidence_hash"] != evidence.EvidenceHash {
		snapshot.Battery = PrecompileBatteryEvidence{}
		snapshot.Seed = PrecompileValueStep{}
		snapshot.Forward, snapshot.Back = PrecompileMoveStep{}, PrecompileMoveStep{}
		snapshot.Snapshot = PrecompileSnapshotStep{}
		snapshot.Dividend = PrecompileDividendStep{}
		snapshot.Transfer = PrecompileTransferStep{}
		snapshot.Complete = false
		snapshot.EvidenceHash = ""
		var err error
		snapshot.EvidenceHash, err = canonicalHashHex(&snapshot)
		if err != nil {
			return nil, err
		}
	}
	state := map[string]any{
		"kind": action.Kind, "target": action.Target, "probe": snapshot.ProbeAddress,
		"evidence_hash": snapshot.EvidenceHash, "complete": snapshot.Complete, "canonical_chain_evidence": true,
	}
	if err := observedPostconditionMatches(record.Observed, state); err != nil {
		return nil, err
	}
	return state, nil
}

// Uses the original native restore anchor only after the normal renewal
// constructor authenticated every successor and the exact source receipt.
func (self *Executor) verifyHistoricalPrecompileRestorePostState(ctx context.Context, action Action, record *ActionPostcondition) (map[string]any, error) {
	if self == nil || self.plan == nil || self.fleetCommitmentHistory == nil || self.fleetCommitmentHistory.plan == nil || !slices.Contains(self.fleetCommitmentHistory.fleets, 1) || !self.fleetCommitmentHistory.plan.allowedPlanHashes()[self.plan.PlanHash] || self.fleetCommitmentHistory.plan.DeploymentID != self.plan.DeploymentID {
		return nil, errors.New("precompile restore lacks approved completed-renewal history scope")
	}
	if action.ID != "precompile.commitment-restore" || record == nil || record.PlanHash != self.plan.PlanHash || record.ActionID != action.ID || record.IntentHash != action.IntentHash {
		return nil, errors.New("precompile restore historical source identity differs")
	}
	if _, err := fleetRenewalHistoricalActionFleets(self.cfg, action); err != nil {
		return nil, err
	}
	if self.payloads == nil {
		return nil, errors.New("precompile deployment payloads are unavailable")
	}
	evidence, err := self.historicalPrecompileEvidence()
	if err != nil {
		return nil, err
	}
	if err := validatePrecompileEvidenceIdentity(self.cfg, self.payloads.PrecompileProbeAddress, evidence); err != nil {
		return nil, err
	}
	if !evidence.Commitment.Restored || evidence.Commitment.RestoreCommitmentBlock <= evidence.Commitment.WriteCommitmentBlock {
		return nil, errors.New("precompile restore historical evidence is incomplete")
	}
	state, err := precompileRestoreHistoricalObservation(action, evidence, record)
	if err != nil {
		return nil, err
	}
	if err := self.verifySubstrateTransactionEvidence(ctx, evidence.Commitment.RestoreFinalizedHead, evidence.Commitment.RestoreTransactionHash); err != nil {
		return nil, err
	}
	hotkey, err := roleBytes32(self.roles, fleetHotkeyLabel(1))
	if err != nil {
		return nil, err
	}
	if err := verifyPrecompileRestoreState(self.substrate, hotkey, evidence.Commitment, true); err != nil {
		return nil, err
	}
	return state, nil
}

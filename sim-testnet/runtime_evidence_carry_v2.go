//go:build linux || darwin

// Completed activation setup belongs to its original approval. A later source
// revision can read that owner only through its exact signed and verified history.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/stabi"
)

// Resolve a completed ancestor without changing its signatures, first epoch,
// quotas or runtime domain. Same-plan setup keeps its existing fresh admission.
func runtimeEvidenceSetupSourcePlanV2(cfg *ResolvedConfig, plan *SetupPlan, stateDir string, roles *RoleSecrets, prepared *runtimeEvidenceActivationPreparedV2, encoded []byte, completed *runtimeEvidenceActivationCompletedV2, entries []JournalEntry) (*SetupPlan, error) {
	if cfg == nil || cfg.Config == nil || plan == nil || roles == nil || prepared == nil {
		return nil, errors.New("activation setup carry owners are incomplete")
	}
	if prepared.PlanHash == plan.PlanHash {
		return plan, nil
	}
	if !plan.allowedPlanHashes()[prepared.PlanHash] {
		return nil, errors.New("activation setup source is outside approved lineage")
	}
	source, err := readValidatorEvidenceHistoricalPlan(stateDir, prepared.PlanHash)
	if err != nil {
		return nil, err
	}
	if err := validatorEvidenceSourcePlanMatches(plan, source); err != nil {
		return nil, err
	}
	if source.ConfigHash != plan.ConfigHash || source.ConfigHash != cfg.ConfigHash || source.PolicyHash != plan.PolicyHash || source.PolicyHash != cfg.PolicyHash {
		return nil, errors.New("activation setup carry changed its approved configuration or policy")
	}
	if err := validateRuntimeEvidencePreparedInputsV2(cfg, source, roles, prepared); err != nil {
		return nil, err
	}
	preparedHash := fmt.Sprintf("0x%x", sha256.Sum256(encoded))
	if completed == nil || completed.Schema != "urnetwork-sim-evidence-activation-completed-v2" || completed.PlanHash != source.PlanHash || completed.PreparedHash != preparedHash || completed.Boundary.Number <= prepared.Evm.Number {
		return nil, errors.New("activation setup carry has no exact original completion")
	}
	if err := verifyFinalHead("activation setup original boundary", completed.Boundary); err != nil {
		return nil, err
	}
	actions := make([]Action, 0, len(prepared.Members)+1)
	for _, member := range prepared.Members {
		action, err := exactPlanActionByID(source, runtimeEvidenceActivationActionId(int(member.ValidatorId), int(member.NoId)))
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	boundaryAction, err := exactPlanActionByID(source, runtimeEvidenceActivationBoundaryActionId)
	if err != nil {
		return nil, err
	}
	actions = append(actions, boundaryAction)
	allowed := plan.allowedPlanHashes()
	for _, action := range actions {
		current, err := exactPlanActionByID(plan, action.ID)
		if err != nil {
			return nil, err
		}
		// Recompute every bound field. DecimalUint's in-memory zero and its
		// decoded canonical zero have one intent but different Go values.
		originalIntent, originalErr := actionIntentHash(action)
		currentIntent, currentErr := actionIntentHash(current)
		if originalErr != nil || currentErr != nil || originalIntent != action.IntentHash || currentIntent != current.IntentHash || originalIntent != currentIntent {
			return nil, errors.Join(fmt.Errorf("activation setup carry changed original approved action %s: source=%s current=%s", action.ID, action.IntentHash, current.IntentHash), originalErr, currentErr)
		}
		for _, entry := range entries {
			if allowed[entry.PlanHash] && entry.ActionID == action.ID && (entry.PlanHash != source.PlanHash || entry.DeploymentID != source.DeploymentID || entry.IntentHash != action.IntentHash) {
				return nil, errors.New("activation setup carry contains competing progress")
			}
		}
	}
	var lastActivationSequence uint64
	for index, member := range prepared.Members {
		action := actions[index]
		reference, broadcast, verified, err := validatorEvidenceHistoryReceipt(plan, entries, action)
		if err != nil || reference.PlanHash != source.PlanHash || reference.BlockNumber <= prepared.Evm.Number || reference.BlockNumber > completed.Boundary.Number {
			return nil, errors.Join(errors.New("activation setup carry lacks original finalized source history"), err)
		}
		transaction, err := readValidatorEvidenceSourceTransaction(stateDir, source, action, reference, broadcast)
		if err != nil {
			return nil, err
		}
		calldata, err := stabi.PackValidatorEvidenceActivation(member.Activation, member.Activation, member.VpkSignature, member.HotkeySignature)
		if err != nil || transaction.To() == nil || *transaction.To() != source.ValidatorEvidence.Address || transaction.Value().Sign() != 0 || !bytes.Equal(transaction.Data(), calldata) || !common.IsHexAddress(roles.EVM["keeper"].Address) || broadcast.Signer != roles.EVM["keeper"].Address {
			return nil, errors.Join(errors.New("activation setup carry signed transaction differs from original consent or keeper"), err)
		}
		record, err := readValidatorEvidenceSourcePostcondition(stateDir, cfg, source, verified)
		if err != nil {
			return nil, err
		}
		digest, err := member.Activation.Digest()
		if err != nil {
			return nil, err
		}
		expected := map[string]any{"kind": action.Kind, "target": action.Target, "prepared_hash": preparedHash, "activation_hash": fmt.Sprintf("0x%x", digest), "published_block": reference.BlockNumber}
		if record.SubstrateFinalized.Number < prepared.Native.Number || record.IndependentSubstrateFinalized.Number < prepared.Native.Number || record.EVMFinalized.Number < reference.BlockNumber || record.IndependentEVMFinalized.Number < reference.BlockNumber || !finalJSONEqual(record.Observed, expected) || !finalJSONEqual(record.IndependentObserved, expected) {
			return nil, errors.New("activation setup receipt differs from original prepared bytes or publication")
		}
		lastActivationSequence = max(lastActivationSequence, verified.Sequence)
	}
	var boundaryVerified *JournalEntry
	var boundaryIntentSequence uint64
	for _, entry := range entries {
		if entry.PlanHash != source.PlanHash || entry.ActionID != boundaryAction.ID {
			continue
		}
		switch entry.Stage {
		case StageIntent:
			boundaryIntentSequence = entry.Sequence
		case StageVerified:
			if boundaryVerified != nil {
				return nil, errors.New("activation setup completion verification is ambiguous")
			}
			copy := entry
			boundaryVerified = &copy
		case StageFailed:
		default:
			return nil, errors.New("activation setup completion has unexpected transaction progress")
		}
	}
	if boundaryVerified == nil || boundaryIntentSequence <= lastActivationSequence || boundaryVerified.Sequence <= boundaryIntentSequence {
		return nil, errors.New("activation setup completion is not verified after every original activation")
	}
	record, err := readValidatorEvidenceSourcePostcondition(stateDir, cfg, source, *boundaryVerified)
	if err != nil {
		return nil, err
	}
	expected := map[string]any{"kind": boundaryAction.Kind, "target": boundaryAction.Target, "prepared_hash": preparedHash,
		"boundary": map[string]any{"number": completed.Boundary.Number, "hash": completed.Boundary.Hash}, "pair_count": len(prepared.Members)}
	if record.SubstrateFinalized.Number < prepared.Native.Number || record.IndependentSubstrateFinalized.Number < prepared.Native.Number || record.EVMFinalized.Number < completed.Boundary.Number || record.IndependentEVMFinalized.Number < completed.Boundary.Number || !finalJSONEqual(record.Observed, expected) || !finalJSONEqual(record.IndependentObserved, expected) {
		return nil, errors.New("activation setup completion receipt differs from original prepared bytes or boundary")
	}
	return source, nil
}

// Refuse a revision of partial/foreign setup before producing a new approval.
// A never-prepared deployment still follows the ordinary fresh setup path.
func validateRuntimeEvidenceSetupRevisionV2(cfg *ResolvedConfig, stateDir string, plan *SetupPlan, roles *RoleSecrets, entries []JournalEntry) error {
	if cfg == nil || cfg.Config == nil || !cfg.Config.ProvisionValidatorEvidenceV2 {
		return nil
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return err
	}
	var prepared runtimeEvidenceActivationPreparedV2
	encoded, err := readRuntimeEvidenceSetupV2(context.Background(), filepath.Join(stateDir, "evidence-v2-setup", "prepared.json"), limit, &prepared)
	if errors.Is(err, os.ErrNotExist) {
		for _, entry := range entries {
			if plan.allowedPlanHashes()[entry.PlanHash] && (strings.HasPrefix(entry.ActionID, "evidence.activate.") || entry.ActionID == runtimeEvidenceActivationBoundaryActionId) {
				return errors.New("activation setup revision has progress without its original preparation")
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	var completed runtimeEvidenceActivationCompletedV2
	if _, err := readRuntimeEvidenceSetupV2(context.Background(), filepath.Join(stateDir, "evidence-v2-setup", "completed.json"), limit, &completed); err != nil {
		return err
	}
	_, err = runtimeEvidenceSetupSourcePlanV2(cfg, plan, stateDir, roles, &prepared, encoded, &completed, entries)
	return err
}

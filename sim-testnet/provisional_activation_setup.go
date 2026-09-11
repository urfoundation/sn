//go:build linux || darwin

package main

// One explicit simulator invocation may hand its authenticated original setup
// to restarted validator children. The handoff never changes on-chain approval.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

func attachProvisionalActivationSetup(cfg *ResolvedConfig, stateDir string, plan *SetupPlan, roles *RoleSecrets, specs []ProcessSpec) error {
	if !provisionalResumeEnabled(cfg) {
		return nil
	}
	if plan == nil || plan.PlanHash != cfg.provisionalResume.Record.PlanHash || !cfg.Config.ProvisionValidatorEvidenceV2 {
		return errors.New("provisional activation handoff requires its retained approved setup")
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return err
	}
	var prepared runtimeEvidenceActivationPreparedV2
	preparedBytes, err := readRuntimeEvidenceSetupV2(context.Background(), filepath.Join(stateDir, "evidence-v2-setup", "prepared.json"), limit, &prepared)
	if err != nil {
		return err
	}
	var completed runtimeEvidenceActivationCompletedV2
	completedBytes, err := readRuntimeEvidenceSetupV2(context.Background(), filepath.Join(stateDir, "evidence-v2-setup", "completed.json"), limit, &completed)
	if err != nil {
		return err
	}
	if prepared.PlanHash == plan.PlanHash {
		return errors.New("provisional activation reuse requires a completed original ancestor")
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		return err
	}
	source, err := runtimeEvidenceSetupSourcePlanV2(cfg, plan, stateDir, roles, &prepared, preparedBytes, &completed, entries)
	if err != nil {
		return err
	}
	values, inputs, err := runtimeEvidenceFixedInputsV2(cfg, source, stateDir, roles, &prepared, &completed)
	if err != nil {
		return err
	}
	for _, configured := range values {
		for _, operator := range configured.Evidence.Operators {
			for index, reference := range operator.Files() {
				actual, err := validatorcomponent.ReadReleaseEvidenceV2File(context.Background(), reference, runtimeEvidenceV2ReferenceLimit(configured.Evidence.Bounds, index))
				if err != nil || !bytes.Equal(actual, inputs[reference.Path]) {
					return errors.Join(errors.New("provisional activation handoff input differs from original setup"), err)
				}
			}
		}
	}
	actions := make([]Action, 0, len(prepared.Members)+1)
	for _, member := range prepared.Members {
		action, err := exactPlanActionByID(source, runtimeEvidenceActivationActionId(int(member.ValidatorId), int(member.NoId)))
		if err != nil {
			return err
		}
		actions = append(actions, action)
	}
	boundary, err := exactPlanActionByID(source, runtimeEvidenceActivationBoundaryActionId)
	if err != nil {
		return err
	}
	actions = append(actions, boundary)
	view, err := runtimeEvidenceSetupCarryHistoryV2(stateDir, plan, source, actions, entries)
	if err != nil {
		return err
	}
	publications := make(map[string]uint64, len(prepared.Members))
	receipts := make([]validatorcomponent.ProvisionalActivationSetupV2Receipt, 0, len(actions))
	for _, action := range actions {
		if action.ID != runtimeEvidenceActivationBoundaryActionId {
			reference, _, _, err := validatorEvidenceHistoryReceipt(plan, view, action)
			if err != nil {
				return err
			}
			publications[action.ID] = reference.BlockNumber
		}
		for _, entry := range view {
			if entry.PlanHash != source.PlanHash || entry.ActionID != action.ID || entry.Stage != StageVerified {
				continue
			}
			if _, err := readValidatorEvidenceSourcePostcondition(stateDir, cfg, source, entry); err != nil {
				return err
			}
			raw, err := readValidatorEvidenceHistoricalFile(stateDir, entry.PostconditionPath, maximumCampaignEvidenceRawFileBytes)
			if err != nil {
				return err
			}
			receipts = append(receipts, validatorcomponent.ProvisionalActivationSetupV2Receipt{ActionID: action.ID, PostconditionHash: entry.PostconditionHash, SHA256: bytesSHA256(raw)})
		}
	}
	if len(receipts) != len(actions) {
		return errors.New("provisional activation handoff receipt census differs")
	}
	for validatorID, configured := range values {
		id := fmt.Sprintf("validator-%d", validatorID+1)
		configPath := filepath.Join(stateDir, "runtime", id, "validator.yml")
		configHash, err := fileSHA256(configPath)
		if err != nil {
			return err
		}
		handoff := validatorcomponent.ProvisionalActivationSetupV2{
			Schema: validatorcomponent.ProvisionalActivationSetupV2Schema, Provisional: true,
			DeploymentID: plan.DeploymentID, ValidatorID: uint64(validatorID + 1), ApprovedPlanHash: plan.PlanHash, SourcePlanHash: source.PlanHash,
			PreparedSHA256: bytesSHA256(preparedBytes), CompletedSHA256: bytesSHA256(completedBytes), ConfigSHA256: configHash, Receipts: receipts,
		}
		coordinatorState := filepath.Join(stateDir, "runtime", id, "coordinator-state-v2")
		if exists, err := regularDirectoryExists(coordinatorState); err != nil {
			return err
		} else if exists {
			handoff.CoordinatorStateDir = coordinatorState
		}
		for _, operator := range configured.Evidence.Operators {
			actionID := runtimeEvidenceActivationActionId(validatorID+1, int(operator.NoID))
			handoff.Members = append(handoff.Members, validatorcomponent.ProvisionalActivationSetupV2Member{NoID: operator.NoID, ContextSHA256: operator.Context.SHA256, PublishedBlock: publications[actionID]})
		}
		raw, err := json.MarshalIndent(handoff, "", "  ")
		if err != nil {
			return err
		}
		raw = append(raw, '\n')
		if len(raw) > validatorcomponent.ProvisionalActivationSetupV2MaximumBytes {
			return errors.New("provisional activation handoff exceeds its byte bound")
		}
		path := filepath.Join(filepath.Dir(cfg.provisionalResume.RecordPath), id+"-activation-setup.json")
		if err := atomicWrite(path, raw, 0o600); err != nil {
			return err
		}
		found := false
		for index := range specs {
			if specs[index].ID == id && specs[index].Role == "validator" {
				specs[index].Args = append(specs[index].Args, "--provisional-activation-setup="+path, "--provisional-activation-setup-sha256="+bytesSHA256(raw))
				found = true
			}
		}
		if !found {
			return errors.New("provisional activation handoff has no validator process owner")
		}
	}
	return nil
}

func readProvisionalActivationSetup(configPath, handoffPath string) ([]byte, error) {
	if !filepath.IsAbs(configPath) || filepath.Clean(configPath) != configPath || !filepath.IsAbs(handoffPath) || filepath.Clean(handoffPath) != handoffPath {
		return nil, errors.New("provisional activation handoff paths must be clean and absolute")
	}
	stateDir := filepath.Dir(filepath.Dir(filepath.Dir(configPath)))
	relative, err := filepath.Rel(stateDir, handoffPath)
	if err != nil || !strings.HasPrefix(filepath.ToSlash(relative), "provisional-resumes/") {
		return nil, errors.New("provisional activation handoff is outside simulator provenance")
	}
	return readValidatorEvidenceHistoricalFile(stateDir, filepath.ToSlash(relative), validatorcomponent.ProvisionalActivationSetupV2MaximumBytes)
}

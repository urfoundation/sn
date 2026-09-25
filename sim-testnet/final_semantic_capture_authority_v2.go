//go:build linux || darwin

// External terminal diagnostics may inspect retained approvals without granting
// strict acceptance. Every original transaction keeps its own archived authority.
package main

import (
	"errors"
	"fmt"
)

// Only the already admitted external diagnostic invocation gets retained reads.
// A provisional scenario or a read-only flag alone cannot select this path.
func finalDiagnosticCaptureV2(cfg *ResolvedConfig) bool {
	if cfg == nil || cfg.Config == nil || !cfg.readOnlyAudit || !provisionalResumeEnabled(cfg) {
		return false
	}
	record := cfg.provisionalResume.Record
	return record.Schema == "urnetwork-sim-provisional-resume-v1" && record.Command == "terminal-diagnostics" && record.ReadOnly && record.Provisional && !record.FinalAcceptance && record.ConfigHash == cfg.ConfigHash && record.DeploymentID == cfg.Config.Deployment.DeploymentID
}

// Revalidate the exact active plan and operational inputs on reload. Ordinary
// collection continues to require strict reconciliation of provisional sources.
func loadFinalCapturePlanV2(cfg *ResolvedConfig, stateDir string) (*SetupPlan, error) {
	if finalDiagnosticCaptureV2(cfg) {
		plan, err := loadRuntimePersistedPlan(cfg, stateDir)
		if err != nil {
			return nil, err
		}
		if cfg.provisionalResume.Record.ReleaseLockHash != plan.ReleaseLockHash {
			return nil, errors.New("compact diagnostic capture changed its retained release authority")
		}
		return plan, nil
	}
	return loadPersistedPlan(cfg, stateDir)
}

// Select immutable finalized rows before any network read. Diagnostics also
// authenticate their original plan, signed transaction and hashed verification;
// source-only revisions never relabel them under the current approval.
func finalCompanionActionJournalV2(cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, names []string) (map[string]JournalEntry, error) {
	if cfg == nil || plan == nil {
		return nil, errors.New("compact companion fixed action authority is absent")
	}
	diagnostic := finalDiagnosticCaptureV2(cfg)
	if diagnostic && cfg.provisionalResume.Record.PlanHash != plan.PlanHash {
		return nil, errors.New("compact companion diagnostic differs from its admitted plan")
	}
	allowed := map[string]bool{plan.PlanHash: true}
	if diagnostic {
		allowed = plan.allowedPlanHashes()
	}
	owners := map[string]*SetupPlan{plan.PlanHash: plan}
	selected := make(map[string]JournalEntry, len(names))
	for _, name := range names {
		action, err := exactPlanActionByID(plan, name)
		if err != nil {
			return nil, err
		}
		var finalized *JournalEntry
		for index := range entries {
			entry := &entries[index]
			if !allowed[entry.PlanHash] || entry.ActionID != name || entry.Stage != StageFinalized {
				continue
			}
			if !actionAcceptsIntent(action, entry.IntentHash) || entry.BlockNumber == 0 || finalized != nil && (diagnostic || finalized.TransactionHash != entry.TransactionHash || finalized.BlockHash != entry.BlockHash) {
				return nil, errors.New("compact companion fixed action has conflicting finality")
			}
			finalized = entry
		}
		if finalized == nil {
			return nil, fmt.Errorf("compact companion action %s lacks actual finalized journal evidence", name)
		}
		if diagnostic {
			source := owners[finalized.PlanHash]
			if source == nil {
				source, err = readValidatorEvidenceHistoricalPlan(stateDir, finalized.PlanHash)
				if err != nil {
					return nil, err
				}
				if err := validatorEvidenceSourcePlanMatches(plan, source); err != nil {
					return nil, err
				}
				owners[source.PlanHash] = source
			}
			original, err := exactPlanActionByID(source, name)
			if err != nil {
				return nil, err
			}
			intent, err := actionIntentHash(original)
			if err != nil || intent != original.IntentHash || intent != action.IntentHash || finalized.DeploymentID != plan.DeploymentID {
				return nil, errors.Join(errors.New("compact companion source action differs from retained approval"), err)
			}
			view := entries
			if name != validatorEvidenceDeployActionID && name != validatorEvidenceAnchorActionID {
				// Failed attempts before the successful activation retain their
				// original approvals and may contain no transaction metadata.
				view, err = runtimeEvidenceSetupCarryHistoryV2(stateDir, plan, source, []Action{original}, entries)
				if err != nil {
					return nil, err
				}
			}
			reference, broadcast, verified, err := validatorEvidenceHistoryReceipt(plan, view, original)
			if err != nil || reference.PlanHash != finalized.PlanHash || reference.TransactionHash != finalized.TransactionHash || reference.BlockNumber != finalized.BlockNumber || reference.BlockHash != finalized.BlockHash {
				return nil, errors.Join(errors.New("compact companion receipt lacks its exact original history"), err)
			}
			if _, err := readValidatorEvidenceSourceTransaction(stateDir, source, original, reference, broadcast); err != nil {
				return nil, err
			}
			if _, err := readValidatorEvidenceSourcePostcondition(stateDir, cfg, plan, verified); err != nil {
				return nil, err
			}
		}
		selected[name] = *finalized
	}
	return selected, nil
}

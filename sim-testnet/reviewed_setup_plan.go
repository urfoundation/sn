package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// A read-only plan command publishes its complete review before printing it.
// The first archived bytes for a hash win, including the observation fields
// intentionally excluded from the economic approval hash. Never rewrite an
// archived review or the active plan while a user reviews an approval.
func archiveReviewedSetupPlan(stateDir string, plan *SetupPlan) (*SetupPlan, error) {
	if plan == nil || !validCanonicalHashHex(plan.PlanHash) {
		return nil, errors.New("reviewed setup plan identity is unavailable")
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	if _, err := decodePersistedPlanBytes(raw); err != nil {
		return nil, err
	}
	raw, err = archiveReviewedSetupPlanBytes(stateDir, plan.PlanHash, append(raw, '\n'))
	if err != nil {
		return nil, err
	}
	return decodePersistedPlanBytes(raw)
}

// Linking a synced private temporary file creates the immutable archive name
// atomically without replacing a concurrent review of the same economic hash.
func archiveReviewedSetupPlanBytes(stateDir, hash string, raw []byte) ([]byte, error) {
	if !validCanonicalHashHex(hash) {
		return nil, errors.New("reviewed setup archive hash is invalid")
	}
	plan, err := decodePersistedPlanWire(raw)
	if err != nil || plan.PlanHash != hash {
		return nil, stateMismatchError(err, "reviewed setup archive bytes differ from their hash")
	}
	relative := filepath.Join("plans", stringsTrim0x(hash)+".json")
	readExisting := func() ([]byte, error) {
		existing, err := readValidatorEvidenceHistoricalFile(stateDir, relative, maximumCampaignEvidenceRawFileBytes)
		if err != nil {
			return nil, err
		}
		archived, err := decodePersistedPlanWire(existing)
		if err != nil || archived.PlanHash != hash {
			return nil, stateMismatchError(err, "existing reviewed setup archive identity differs")
		}
		return existing, nil
	}
	if existing, err := readExisting(); err == nil || !errors.Is(err, os.ErrNotExist) {
		return existing, err
	}
	dir := filepath.Join(stateDir, "plans")
	if err := ensurePrivateDir(dir); err != nil {
		return nil, err
	}
	if err := rejectFinalArtifactSymlinkComponents(stateDir, dir); err != nil {
		return nil, err
	}
	temporary, err := os.CreateTemp(dir, ".reviewed-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temporary.Name())
	_, writeErr := temporary.Write(raw)
	syncErr, closeErr := temporary.Sync(), temporary.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return nil, err
	}
	if err := os.Link(temporary.Name(), filepath.Join(stateDir, relative)); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("publish exact reviewed setup plan: %w", err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	syncErr, closeErr = directory.Sync(), directory.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return nil, err
	}
	return readExisting()
}

// A review cannot retire an action which acquired signing or transaction
// evidence later. Retained actions use their original exactly-once recovery
// keys; removed reserve repairs must still prove the original pre-sign state.
func validateReviewedSetupRepairRetirements(source, reviewed *SetupPlan, entries []JournalEntry) error {
	if source == nil || reviewed == nil {
		return errors.New("reviewed repair predecessor is unavailable")
	}
	for _, prior := range source.Actions {
		kind, index, err := alphaTransferTargetFromActionID(prior.ID)
		if err != nil || kind != "validator" || index != 1 || prior.Parameters[alphaRepairReserveShareParameter] != "true" {
			continue
		}
		retained := false
		for _, current := range reviewed.Actions {
			retained = retained || current.ID == prior.ID && actionAcceptsIntent(current, prior.IntentHash)
		}
		if retained {
			continue
		}
		failure, err := reserveRepairPreSignFailure(source, prior, entries)
		if err != nil {
			return fmt.Errorf("reviewed reserve repair %s acquired conflicting execution evidence: %w", prior.ID, err)
		}
		if failure != nil {
			matched := false
			for _, current := range reviewed.Actions {
				matched = matched || current.Parameters["retired_pre_sign_action_id"] == prior.ID &&
					current.Parameters["retired_pre_sign_intent_hash"] == prior.IntentHash &&
					current.Parameters["retired_pre_sign_plan_hash"] == source.PlanHash &&
					current.Parameters["retired_pre_sign_failure_hash"] == failure.EntryHash
			}
			if !matched {
				return errors.New("reviewed repair lacks its exact pre-sign retirement provenance")
			}
		}
	}
	return nil
}

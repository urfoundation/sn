// Reviewed local approvals and already-applied fleet successors authenticate
// retained startup without completing or dispatching pending setup actions.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
)

// Only shared deterministic allowance/relay transforms qualify. Ordinary
// setup repairs retain their prefix/doctor path; repeat adoption authenticates
// the original source without calling any retained pending action.
func (self *Executor) authenticateProvisionalPlanOnlyAdoption(ctx context.Context, activeBytes []byte) (bool, error) {
	return self.authenticateProvisionalPlanAdoption(ctx, activeBytes, false)
}

// Restart may retain an already-approved fleet transaction plan. It does not
// approve a new action, dispatch setup, or relax the fleet apply preflight.
func (self *Executor) authenticateProvisionalRetainedPlan(ctx context.Context, activeBytes []byte) (bool, error) {
	if self == nil || !provisionalResumeEnabled(self.cfg) || !provisionalRetainedStartupAllowed(self.cfg.provisionalResume.Record) || self.cfg.strictHistoryAdoption != nil {
		return false, errors.New("retained plan requires explicit non-accepting process startup")
	}
	return self.authenticateProvisionalPlanAdoption(ctx, activeBytes, true)
}

// Local setup transforms and retained startup share exact provenance checks.
// Only retained startup additionally admits a reconstructed fleet successor.
func (self *Executor) authenticateProvisionalPlanAdoption(ctx context.Context, activeBytes []byte, retainedStartup bool) (bool, error) {
	if ctx == nil || self == nil || self.cfg == nil || self.cfg.Config == nil || self.plan == nil || self.journal == nil {
		return false, errors.New("plan-only adoption context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if self.cfg.Config.Deployment.Network != "bittensor-testnet" || self.cfg.Config.Deployment.Subnet != "existing" || self.cfg.ChainID != testnetChainID {
		return false, errors.New("plan-only adoption is restricted to retained testnet custody")
	}
	active, err := decodePersistedPlanWire(activeBytes)
	if err != nil {
		return false, err
	}
	if retainedStartup {
		if active.PlanHash != self.plan.PlanHash || !evidenceRelayContinuationSameJSON(active, self.plan) {
			return false, errors.New("retained startup differs from the active approved plan")
		}
		reviewedBytes, err := readSetupPlanBytes(self.stateDir, "plans/"+stringsTrim0x(self.plan.PlanHash)+".json")
		if err != nil {
			return false, err
		}
		reviewed, err := decodePersistedPlanWire(reviewedBytes)
		if err != nil || !evidenceRelayContinuationSameJSON(reviewed, self.plan) {
			return false, errors.Join(errors.New("retained startup archived approval changed"), err)
		}
	}
	source, sourceBytes := active, activeBytes
	if source.PlanHash == self.plan.PlanHash {
		if len(source.PriorPlanHashes) == 0 {
			return false, nil
		}
		hash := source.PriorPlanHashes[len(source.PriorPlanHashes)-1]
		sourceBytes, err = readSetupPlanBytes(self.stateDir, "plans/"+stringsTrim0x(hash)+".json")
		if err != nil {
			return false, err
		}
		source, err = decodePersistedPlanWire(sourceBytes)
		if err != nil || source.PlanHash != hash {
			return false, errors.Join(errors.New("plan-only adoption original source changed"), err)
		}
	}
	matched, err := self.provisionalPlanOnlyTransform(source, sourceBytes)
	if err == nil && !matched && retainedStartup && self.plan.CampaignConfigMigrationHash != "" && source.ConfigHash != self.plan.ConfigHash {
		_, _, receipt, migrationErr := authenticatedCampaignConfigMigrationSource(ctx, self.cfg, self.stateDir, self.plan, source.PlanHash)
		if migrationErr != nil {
			return false, migrationErr
		}
		matched = receipt.PlanHash == self.plan.PlanHash
	}
	if err == nil && !matched && retainedStartup && self.plan.PolicyRateAmendment != nil {
		matched, err = self.authenticateRetainedPolicyRateAmendment(ctx, source)
	}
	if err == nil && !matched && retainedStartup && len(self.plan.FleetRenewals) != 0 {
		renewal := self.plan.FleetRenewals[len(self.plan.FleetRenewals)-1]
		if renewal.SourcePlanHash == source.PlanHash {
			entries := self.journal.Entries()
			checkpoint := -1
			for index, entry := range entries {
				if entry.EntryHash == renewal.JournalHash {
					checkpoint = index
					break
				}
			}
			if checkpoint < 0 {
				return false, errors.New("retained fleet renewal source journal checkpoint is absent")
			}
			// Apply's trailing-journal exclusion protects an unsubmitted plan.
			// Restart validates the same exact append at its original checkpoint;
			// authenticated later preparation belongs to the continuing journal.
			if err := validateFleetRenewalSource(self.cfg, source, self.plan, entries[:checkpoint+1]); err != nil {
				return false, err
			}
			if err := self.verifyProvisionalActionHistory(ctx); err != nil {
				return false, err
			}
			matched = true
		}
	}
	if err != nil || !matched {
		return false, err
	}
	if !provisionalResumeEnabled(self.cfg) || self.cfg.readOnlyAudit {
		return false, errors.New("plan-only adoption requires explicit provisional execution")
	}
	invocation := self.cfg.provisionalResume
	record := invocation.Record
	retainedStartup = provisionalRetainedStartupAllowed(record)
	if (record.Command != "setup" && !retainedStartup) || (retainedStartup && active.PlanHash != self.plan.PlanHash) || !record.Provisional || record.FinalAcceptance || record.PlanHash != self.plan.PlanHash ||
		record.ConfigHash != self.cfg.ConfigHash || record.DeploymentID != self.plan.DeploymentID || record.ReleaseLockHash != self.plan.ReleaseLockHash ||
		record.Driver != invocation.Driver || !filepath.IsAbs(invocation.RecordPath) {
		return false, errors.New("plan-only adoption lost its exact retained release and non-accepting provenance")
	}
	relative, err := filepath.Rel(self.stateDir, invocation.RecordPath)
	if err != nil {
		return false, err
	}
	raw, err := readValidatorEvidenceHistoricalFile(self.stateDir, filepath.ToSlash(relative), maximumCampaignEvidenceRawFileBytes)
	var persisted provisionalResumeRecord
	if err != nil || bytesSHA256(raw) != invocation.RecordHash {
		return false, errors.Join(errors.New("plan-only adoption provenance bytes changed"), err)
	}
	if err := json.Unmarshal(raw, &persisted); err != nil || !reflect.DeepEqual(&persisted, record) {
		return false, errors.Join(errors.New("plan-only adoption provenance identity changed"), err)
	}
	latest, err := readSetupPlanBytes(self.stateDir, "plan.json")
	if err != nil || !bytes.Equal(latest, activeBytes) {
		return false, errors.Join(errors.New("plan-only adoption active source changed"), err)
	}
	if _, err := self.authenticateProvisionalTopology(ctx, self.plan, self.journal.Entries, readValidatorEvidenceHistoricalPlan); err != nil {
		return false, err
	}
	return true, ctx.Err()
}

// Reconstruct the complete successor, including every allowed reserve change.
// A transaction action delta can never gain this local-only admission.
func (self *Executor) provisionalPlanOnlyTransform(source *SetupPlan, sourceBytes []byte) (bool, error) {
	normalized := *self.plan
	normalized.Limits = source.Limits
	normalized.EVMFundingAllocationWei = source.EVMFundingAllocationWei
	normalized.ResolvedInputsHash = source.ResolvedInputsHash
	normalized.PriorPlanHashes = source.PriorPlanHashes
	normalized.PlanHash = source.PlanHash
	if evidenceRelayContinuationSameJSON(&normalized, source) {
		expected, err := buildAllowanceOnlyPlanFromSource(self.cfg, sourceBytes, source.PlanHash)
		if err != nil || expected.PlanHash != self.plan.PlanHash || !evidenceRelayContinuationSameJSON(expected, self.plan) {
			return false, errors.Join(errors.New("plan-only adoption differs from its exact reviewed allowance transform"), err)
		}
		return true, nil
	}
	continuation := self.plan.EvidenceRelayContinuation
	if continuation == nil || continuation.SourcePlanHash != source.PlanHash {
		return false, nil
	}
	if _, err := loadPlanIdentityBytes(self.cfg, sourceBytes, true); err != nil {
		return false, err
	}
	expected, err := appendEvidenceRelayContinuationPlan(source, *continuation)
	if err != nil || expected.PlanHash != self.plan.PlanHash || !evidenceRelayContinuationSameJSON(expected, self.plan) {
		return false, errors.Join(errors.New("plan-only adoption differs from its exact reviewed relay transform"), err)
	}
	if err := validateEvidenceRelayContinuationSource(self.stateDir, self.plan, self.journal.Entries()); err != nil {
		return false, err
	}
	return true, nil
}

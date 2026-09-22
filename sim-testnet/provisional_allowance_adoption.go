// Reviewed testnet allowance and relay-reserve changes are local approvals.
// They neither complete pending setup actions nor dispatch them.
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
	source, sourceBytes := active, activeBytes
	if source.PlanHash == self.plan.PlanHash {
		if len(source.PriorPlanHashes) == 0 {
			return false, nil
		}
		hash := source.PriorPlanHashes[len(source.PriorPlanHashes)-1]
		sourceBytes, err = readValidatorEvidenceHistoricalFile(self.stateDir, "plans/"+stringsTrim0x(hash)+".json", maximumCampaignEvidenceRawFileBytes)
		if err != nil {
			return false, err
		}
		source, err = decodePersistedPlanWire(sourceBytes)
		if err != nil || source.PlanHash != hash {
			return false, errors.Join(errors.New("plan-only adoption original source changed"), err)
		}
	}
	matched, err := self.provisionalPlanOnlyTransform(source, sourceBytes)
	if err != nil || !matched {
		return false, err
	}
	if !provisionalResumeEnabled(self.cfg) || self.cfg.readOnlyAudit {
		return false, errors.New("plan-only adoption requires explicit provisional execution")
	}
	invocation := self.cfg.provisionalResume
	record := invocation.Record
	retainedStartup := provisionalRetainedStartupAllowed(record)
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
	latest, err := readValidatorEvidenceHistoricalFile(self.stateDir, "plan.json", maximumCampaignEvidenceRawFileBytes)
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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// A provisional repair approval is independent of historical chain replay.
// Its old approval and receipts stay immutable; only the approved setup repair
// prefix is reconciled. The current runtime configuration is never rendered.
func (self *Executor) activateProvisionalSetupRevision(ctx context.Context, sourceBytes []byte, execute func(context.Context, Action) error) error {
	if ctx == nil || self == nil || self.journal == nil || self.plan == nil || execute == nil ||
		!provisionalResumeEnabled(self.cfg) || self.cfg.readOnlyAudit {
		return errors.New("provisional setup activation context is unavailable")
	}
	provenance := self.cfg.provisionalResume.Record
	if provenance.Command != "setup" || !provenance.Provisional || provenance.FinalAcceptance ||
		provenance.PlanHash != self.plan.PlanHash || !filepath.IsAbs(self.cfg.provisionalResume.RecordPath) {
		return errors.New("provisional setup activation lacks exact non-acceptance provenance")
	}
	source, err := decodePersistedPlanWire(sourceBytes)
	if err != nil {
		return err
	}
	hash, err := self.plan.hash()
	if err != nil || hash != self.plan.PlanHash {
		return stateMismatchError(err, "provisional setup approval hash differs")
	}
	if source.DeploymentID != self.plan.DeploymentID || source.ChainID != self.plan.ChainID ||
		source.GenesisHash != self.plan.GenesisHash || source.Netuid != self.plan.Netuid || source.PolicyHash != self.plan.PolicyHash ||
		!self.plan.allowedPlanHashes()[source.PlanHash] || self.plan.Limits != configuredPlanLimits(self.cfg) {
		return errors.New("provisional setup revision differs from its retained deployment, policy, lineage or allowance")
	}
	if err := validatePlanBudget(self.plan); err != nil {
		return err
	}
	entries := self.journal.Entries()
	if err := validateReviewedSetupRepairRetirements(source, self.plan, entries); err != nil {
		return err
	}
	reviewedBytes, err := readValidatorEvidenceHistoricalFile(self.stateDir, filepath.Join("plans", stringsTrim0x(self.plan.PlanHash)+".json"), maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	reviewed, err := decodePersistedPlanWire(reviewedBytes)
	if err != nil {
		return err
	}
	wantSnapshot, err := canonicalHashHex(self.plan)
	if err != nil {
		return err
	}
	gotSnapshot, err := canonicalHashHex(reviewed)
	if err != nil || gotSnapshot != wantSnapshot {
		return stateMismatchError(err, "reviewed setup snapshot changed after approval loading")
	}
	// This is the authenticated local receipt cache, never an archive RPC
	// replay. A cache miss authenticates every retained receipt and source.
	if err := self.verifyProvisionalActionHistory(ctx); err != nil {
		return err
	}
	planOnly, err := self.authenticateProvisionalPlanOnlyAdoption(ctx, sourceBytes)
	if err != nil {
		return err
	}
	var pending, deferred []Action
	if planOnly {
		verified := newCarriedPreparationIndex(self.plan, entries)
		for _, action := range self.plan.Actions {
			if action.ID == "topology.launch" {
				break
			}
			if _, ok := verified.find(action, false); !ok {
				deferred = append(deferred, action)
			}
		}
	} else {
		_, pending, err = self.provisionalSetupPrefix(ctx, self.plan, self.journal.Entries, readValidatorEvidenceHistoricalPlan, true)
		if err != nil {
			return err
		}
	}
	current, err := readValidatorEvidenceHistoricalFile(self.stateDir, "plan.json", maximumCampaignEvidenceRawFileBytes)
	if err != nil || !bytes.Equal(current, sourceBytes) || !slices.Equal(entries, self.journal.Entries()) {
		return stateMismatchError(err, "provisional setup source plan or journal changed before activation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var boundary JournalEntry
	if len(entries) > 0 {
		boundary = entries[len(entries)-1]
	}
	record := struct {
		Schema                  string       `json:"schema"`
		Provisional             bool         `json:"provisional"`
		FinalAcceptance         bool         `json:"final_acceptance"`
		HistoricalAuditDeferred bool         `json:"historical_audit_deferred"`
		SourcePlanHash          string       `json:"source_plan_hash"`
		SourcePlanBytesSHA256   string       `json:"source_plan_bytes_sha256"`
		PlanHash                string       `json:"plan_hash"`
		ReviewedPlanBytesSHA256 string       `json:"reviewed_plan_bytes_sha256"`
		JournalBoundary         JournalEntry `json:"authenticated_journal_boundary"`
		PendingActions          []Action     `json:"pending_setup_actions"`
		PlanOnly                bool         `json:"plan_only,omitempty"`
		DeferredSetupActions    []Action     `json:"deferred_setup_actions,omitempty"`
	}{Schema: "urnetwork-sim-provisional-setup-activation-v1", Provisional: true, HistoricalAuditDeferred: true,
		SourcePlanHash: source.PlanHash, SourcePlanBytesSHA256: bytesSHA256(sourceBytes), PlanHash: self.plan.PlanHash,
		ReviewedPlanBytesSHA256: bytesSHA256(reviewedBytes),
		JournalBoundary:         boundary, PendingActions: pending, PlanOnly: planOnly, DeferredSetupActions: deferred}
	wire, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "setup-activation.json"), append(wire, '\n'), 0o600); err != nil {
		return err
	}
	// Archive exact predecessor bytes before switching the active pointer. A
	// crash after activation can resume the same plan and transaction intents.
	if source.PlanHash != self.plan.PlanHash {
		if err := atomicWrite(filepath.Join(self.stateDir, "plans", stringsTrim0x(source.PlanHash)+".json"), sourceBytes, 0o600); err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(self.stateDir, "plan.json"), reviewedBytes, 0o600); err != nil {
			return err
		}
	}
	fmt.Fprintln(os.Stderr, "sim-testnet: provisional setup approval activated; historical_audit_deferred=true; runtime files retained; final_acceptance=false")
	if planOnly {
		fmt.Fprintln(os.Stderr, "sim-testnet: plan-only adoption dispatched no actions; pending setup remains deferred; final_acceptance=false")
		return ctx.Err()
	}
	_, err = self.reconcileProvisionalSetupPrefix(ctx, self.plan, execute)
	return err
}

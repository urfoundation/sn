// Provisional startup records deferred history without issuing archive reads.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type provisionalHistoricalActionDeferral struct {
	ActionId      string       `json:"action_id"`
	ActionHash    string       `json:"action_hash"`
	SourceReceipt JournalEntry `json:"authenticated_source_receipt"`
	Detail        string       `json:"detail"`
}

type provisionalHistoricalAuditDeferral struct {
	Schema          string                                `json:"schema"`
	Provisional     bool                                  `json:"provisional"`
	FinalAcceptance bool                                  `json:"final_acceptance"`
	PlanHash        string                                `json:"plan_hash"`
	ProvenanceHash  string                                `json:"provenance_hash"`
	JournalRoot     string                                `json:"journal_root"`
	OperationalEvm  string                                `json:"operational_evm"`
	IndependentEvm  string                                `json:"independent_evm"`
	Actions         []provisionalHistoricalActionDeferral `json:"deferred_actions"`
}

// Publish only after every local receipt authenticated. The owning command has
// immutable invocation provenance; library-only receipt checks have no output
// path. These references bind every action/receipt separately and never cache
// a failed RPC response or confer historical/final acceptance authority.
func (self *Executor) recordProvisionalHistoryDeferral(ctx context.Context, entries []JournalEntry) error {
	if ctx == nil || self == nil || self.plan == nil || !provisionalResumeEnabled(self.cfg) {
		return errors.New("provisional historical deferral context is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	invocation := self.cfg.provisionalResume
	if invocation.RecordPath == "" {
		return nil
	}
	if invocation.Record.PlanHash != self.plan.PlanHash || !invocation.Record.Provisional || invocation.Record.FinalAcceptance {
		return errors.New("provisional historical deferral has no exact non-acceptance invocation")
	}
	if err := rejectFinalArtifactSymlinkComponents(self.stateDir, invocation.RecordPath); err != nil {
		return err
	}
	provenance, err := os.ReadFile(invocation.RecordPath)
	if err != nil || bytesSHA256(provenance) != invocation.RecordHash {
		return stateMismatchError(err, "provisional historical deferral provenance changed")
	}
	record := provisionalHistoricalAuditDeferral{
		Schema: "urnetwork-sim-provisional-historical-deferral-v1", Provisional: true,
		PlanHash: self.plan.PlanHash, ProvenanceHash: invocation.RecordHash,
		OperationalEvm: self.cfg.OperationalEVM, IndependentEvm: verificationEVMEndpoint(self.cfg),
	}
	if len(entries) > 0 {
		record.JournalRoot = entries[len(entries)-1].EntryHash
	}
	verified := newCarriedPreparationIndex(self.plan, entries)
	for _, action := range self.plan.Actions {
		entry, ok := verified.find(action, true)
		if !ok {
			continue
		}
		hash, err := canonicalHashHex(action)
		if err != nil {
			return err
		}
		record.Actions = append(record.Actions, provisionalHistoricalActionDeferral{
			ActionId: action.ID, ActionHash: hash, SourceReceipt: entry,
			Detail: "local receipt authenticated; historical chain replay deferred to independent audit; final_acceptance=false",
		})
	}
	wire, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	wire = append(wire, '\n')
	path := filepath.Join(filepath.Dir(invocation.RecordPath), "historical-audit-deferred.json")
	if err := rejectFinalArtifactSymlinkComponents(self.stateDir, filepath.Dir(path)); err != nil {
		return err
	}
	// Atomic first publication has no leaf yet. An existing destination must
	// remain a private ordinary file, never a link, directory, or pipe.
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errors.New("provisional historical deferral destination is not a private regular file")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if previous, err := os.ReadFile(path); err == nil && bytes.Equal(previous, wire) {
		return ctx.Err()
	}
	if err := atomicWrite(path, wire, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "sim-testnet: retained %d deferred historical action references in %s; final_acceptance=false\n", len(record.Actions), path)
	return ctx.Err()
}

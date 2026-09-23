// A second dual-signed request can replace the first unsigned recovery intent
// after an observed gas-ceiling refusal. Original approvals and failures remain.
package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

const precompileRecoveryGasRevisionFilename = "public/precompile-recovery-authorization-v2.json"
const precompileRecoveryRevisedGasUnits uint64 = 6_000_000

// The old snapshot includes its original dual signatures and exact failed
// quote. Only one v1-to-v2 revision is supported; nested revisions are refused.
type PrecompileRecoveryGasRevision struct {
	Evidence        PrecompileConformanceEvidence `json:"evidence"`
	JournalSequence uint64                        `json:"journal_sequence"`
	JournalHash     string                        `json:"journal_hash"`
	PaddedGasUnits  uint64                        `json:"padded_gas_units"`
}

// Legacy authority keeps its exact 500k bound. The revision retains every other
// custody field and all original evidence while separately approving its cost.
func validatePrecompileRecoveryGasRevision(evidence *PrecompileConformanceEvidence, authorization *PrecompileRecoveryAuthorization) error {
	r := authorization.Request
	if r.Schema == "urnetwork-precompile-recovery-v1" {
		if r.GasRevision != nil || r.MaximumGasUnits != precompileRecoveryGasUnits {
			return errors.New("legacy probe recovery gas authority changed")
		}
		return nil
	}
	if r.Schema != "urnetwork-precompile-recovery-v2" || r.MaximumGasUnits != precompileRecoveryRevisedGasUnits || r.GasRevision == nil {
		return errors.New("probe recovery gas revision has unsupported bounds")
	}
	revision := r.GasRevision
	prior := &revision.Evidence
	if prior.Recovery == nil || prior.Recovery.Authorization.Request.Schema != "urnetwork-precompile-recovery-v1" || prior.Recovery.Authorization.Request.GasRevision != nil || prior.Complete || prior.Transfer != (PrecompileTransferStep{}) || len(prior.Recovery.Steps) != 1 || prior.Recovery.Steps[0].Operation != "top-up" {
		return errors.New("probe recovery gas revision is not the first unsigned top-up")
	}
	copy := *prior
	copy.EvidenceHash = ""
	hash, err := canonicalHashHex(copy)
	if err != nil || hash != prior.EvidenceHash || !validCanonicalHashHex(prior.EvidenceHash) || revision.JournalSequence == 0 || !validCanonicalHashHex(revision.JournalHash) || revision.PaddedGasUnits <= precompileRecoveryGasUnits || revision.PaddedGasUnits > precompileRecoveryRevisedGasUnits/2 {
		return errors.New("probe recovery gas revision lost its exact refusal evidence")
	}
	if _, _, settled, err := precompileRecoveryPositions(prior); err != nil || settled {
		return errors.Join(errors.New("probe recovery gas revision changes an executed or invalid sequence"), err)
	}
	minimal := PrecompileRecoveryEvidence{Authorization: prior.Recovery.Authorization, Steps: prior.Recovery.Steps}
	if !reflect.DeepEqual(minimal, *prior.Recovery) {
		return errors.New("probe recovery gas revision contains unrelated transfer progress")
	}
	want := prior.Recovery.Authorization.Request
	want.Schema, want.MaximumGasUnits = r.Schema, r.MaximumGasUnits
	want.Budget, want.BudgetHash, want.GasRevision = r.Budget, r.BudgetHash, r.GasRevision
	if !reflect.DeepEqual(want, r) {
		return errors.New("probe recovery gas revision changed custody or its original evidence identity")
	}
	return nil
}

// A refusal has one exact retained action and no transaction metadata. Later
// v2 receipts remain subject to the ordinary contiguous-nonce and call replay.
func validatePrecompileRecoveryGasRevisionJournal(plan *SetupPlan, evidence *PrecompileConformanceEvidence, entries []JournalEntry) error {
	if evidence == nil || evidence.Recovery == nil || evidence.Recovery.Authorization.Request.GasRevision == nil {
		return nil
	}
	revision := evidence.Recovery.Authorization.Request.GasRevision
	if err := validatePrecompileRecoveryPlan(plan, evidence, &evidence.Recovery.Authorization); err != nil {
		return err
	}
	prior := revision.Evidence.Recovery.Steps[0].Action
	wantFailure := fmt.Sprintf("%s padded gas %d exceeds approved gas-unit ceiling %d", prior.ID, revision.PaddedGasUnits, precompileRecoveryGasUnits)
	anchor, intent, originalBudget, revisedBudget := false, false, false, false
	for _, entry := range entries {
		if entry.PlanHash == plan.PlanHash && entry.DeploymentID == plan.DeploymentID {
			originalBudget = originalBudget || (entry.EntryHash == revision.Evidence.Recovery.Authorization.Request.Budget.JournalHash && entry.Sequence < revision.JournalSequence)
			revisedBudget = revisedBudget || (entry.EntryHash == evidence.Recovery.Authorization.Request.Budget.JournalHash && entry.Sequence >= revision.JournalSequence)
		}
		if entry.Sequence == revision.JournalSequence {
			anchor = entry.EntryHash == revision.JournalHash && entry.ActionID == prior.ID && entry.Stage == StageFailed && entry.Error == wantFailure
		}
		if strings.HasPrefix(entry.ActionID, precompileRecoveryActionPrefix+"v2.") {
			matched := false
			for i, step := range evidence.Recovery.Steps {
				if step.Action.ID != entry.ActionID {
					continue
				}
				action, _, err := precompileRecoveryAction(&evidence.Recovery.Authorization, i, step)
				matched = err == nil && precompileRecoveryActionEqual(step.Action, action) && entry.IntentHash == action.IntentHash && entry.PlanHash == plan.PlanHash && entry.DeploymentID == plan.DeploymentID
			}
			if !matched {
				return errors.New("probe gas revision has unapproved revised recovery history")
			}
			continue
		}
		if !strings.HasPrefix(entry.ActionID, precompileRecoveryActionPrefix) {
			continue
		}
		if entry.PlanHash != plan.PlanHash || entry.DeploymentID != plan.DeploymentID || entry.ActionID != prior.ID || entry.IntentHash != prior.IntentHash || (entry.Stage != StageIntent && entry.Stage != StageFailed) || entry.TransactionHash != "" || entry.Signer != "" || entry.Nonce != "" || entry.BlockNumber != 0 || entry.BlockHash != "" || entry.PostconditionHash != "" || entry.PostconditionPath != "" || entry.RecoveryBlock != 0 || entry.RecoveryBlockHash != "" {
			return errors.New("probe gas revision cannot replace a signed, completed or unrelated recovery")
		}
		if entry.Stage == StageIntent && entry.Sequence < revision.JournalSequence {
			intent = true
		}
	}
	if !anchor || !intent || !originalBudget || !revisedBudget {
		return errors.New("probe gas revision lost its authenticated intent and gas refusal")
	}
	return nil
}

// Captures a stable source snapshot and exact refusal before either owner
// signs. Transaction bytes are checked separately under exclusive adoption.
func newPrecompileRecoveryGasRevisionRequest(plan *SetupPlan, evidence *PrecompileConformanceEvidence, budget PrecompileRecoveryBudget, entries []JournalEntry) (PrecompileRecoveryRequest, error) {
	if evidence == nil || evidence.Recovery == nil || evidence.Recovery.Authorization.Request.GasRevision != nil || len(evidence.Recovery.Steps) != 1 {
		return PrecompileRecoveryRequest{}, errors.New("probe gas revision requires the first retained unsigned step")
	}
	if err := validatePrecompileRecoveryPlan(plan, evidence, &evidence.Recovery.Authorization); err != nil {
		return PrecompileRecoveryRequest{}, err
	}
	prior := evidence.Recovery.Steps[0].Action
	var refusal *JournalEntry
	var padded uint64
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.ActionID != prior.ID {
			continue
		}
		pattern := prior.ID + " padded gas %d exceeds approved gas-unit ceiling 500000"
		if entry.Stage == StageFailed {
			if _, err := fmt.Sscanf(entry.Error, pattern, &padded); err == nil && entry.Error == fmt.Sprintf("%s padded gas %d exceeds approved gas-unit ceiling %d", prior.ID, padded, precompileRecoveryGasUnits) {
				refusal = &entry
			}
		}
		break
	}
	if refusal == nil {
		return PrecompileRecoveryRequest{}, errors.New("probe gas revision has no retained gas-ceiling refusal")
	}
	r := evidence.Recovery.Authorization.Request
	r.Schema, r.MaximumGasUnits, r.Budget = "urnetwork-precompile-recovery-v2", precompileRecoveryRevisedGasUnits, budget
	var err error
	r.BudgetHash, err = canonicalHashHex(budget)
	if err != nil {
		return PrecompileRecoveryRequest{}, err
	}
	r.GasRevision = &PrecompileRecoveryGasRevision{Evidence: *evidence, JournalSequence: refusal.Sequence, JournalHash: refusal.EntryHash, PaddedGasUnits: padded}
	if err := validatePrecompileRecoveryGasRevision(evidence, &PrecompileRecoveryAuthorization{Request: r}); err != nil {
		return PrecompileRecoveryRequest{}, err
	}
	return r, nil
}

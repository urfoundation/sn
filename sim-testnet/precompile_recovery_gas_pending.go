// A provisional release may observe an explicitly accounted probe liability
// while its unsigned repair waits for new gas authority. No signing limit or
// final conformance requirement is relaxed by this continuation state.
package main

import (
	"context"
	"errors"
	"fmt"
)

// This refusal is created only before signing by the exact envelope check.
// It remains a hard error everywhere except authenticated release continuation.
type evmGasUnitCeilingError struct {
	actionId   string
	intentHash string
	paddedGas  uint64
	maximumGas uint64
}

func (self *evmGasUnitCeilingError) Error() string {
	return fmt.Sprintf("%s padded gas %d exceeds approved gas-unit ceiling %d", self.actionId, self.paddedGas, self.maximumGas)
}

// The current release writer retains one refusal until its exact action,
// authority or action journal changes. Unrelated observations never re-journal
// the same known insufficient approval.
type precompileRecoveryGasPending struct {
	failure           *evmGasUnitCeilingError
	planHash          string
	authorizationHash string
	journalHash       string
}

// Borrows authenticated evidence and the current journal. Only a lone typed
// pre-signing refusal can become pending; composite failures remain hard.
func (self *Executor) deferPrecompileRecoveryGas(ctx context.Context, evidence *PrecompileConformanceEvidence, failure error) error {
	singlePoll, _ := ctx.Value(precompileDividendSinglePollKey{}).(bool)
	if self == nil || self.precompileRecoveryOnly || !singlePoll || !provisionalResumeEnabled(self.cfg) || self.plan == nil || self.journal == nil || self.cfg.provisionalResume.Record == nil {
		return failure
	}
	record := self.cfg.provisionalResume.Record
	if record.Command != "scenario" || record.Scenario != "release-1.0" || record.PlanHash != self.plan.PlanHash || !record.Provisional || record.FinalAcceptance {
		return failure
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(failure, err)
	}
	leaf := failure
	for {
		wrapped, ok := leaf.(interface{ Unwrap() error })
		if !ok || wrapped.Unwrap() == nil {
			break
		}
		leaf = wrapped.Unwrap()
	}
	refusal, ok := leaf.(*evmGasUnitCeilingError)
	if !ok || evidence == nil || evidence.Recovery == nil || len(evidence.Recovery.Steps) == 0 {
		return failure
	}
	authorization := &evidence.Recovery.Authorization
	if err := validatePrecompileRecoveryPlan(self.plan, evidence, authorization); err != nil {
		return errors.Join(failure, err)
	}
	_, move, settled, err := precompileRecoveryPositions(evidence)
	if err != nil {
		return errors.Join(failure, err)
	}
	step := evidence.Recovery.Steps[len(evidence.Recovery.Steps)-1]
	transactionHash, block, blockHash := precompileRecoveryReceipt(step)
	if settled || transactionHash != "" || block != 0 || blockHash != "" || refusal.actionId != step.Action.ID || refusal.intentHash != step.Action.IntentHash || refusal.maximumGas != authorization.Request.MaximumGasUnits || refusal.paddedGas <= refusal.maximumGas {
		return failure
	}
	if _, err := self.precompileRecoveryStepForAction(step.Action, evidence); err != nil {
		return errors.Join(failure, err)
	}
	anchorFound, actionFound := false, false
	journalHash := ""
	var latest JournalEntry
	for _, entry := range self.journal.Entries() {
		if entry.EntryHash == authorization.Request.Budget.JournalHash && entry.PlanHash == self.plan.PlanHash {
			anchorFound = true
		}
		if entry.ActionID != step.Action.ID {
			continue
		}
		if entry.PlanHash != self.plan.PlanHash || entry.DeploymentID != self.plan.DeploymentID || entry.IntentHash != step.Action.IntentHash || !validCanonicalHashHex(entry.EntryHash) {
			return errors.Join(failure, errors.New("pending probe repair journal changed its exact authority"))
		}
		if entry.Stage != StageIntent && entry.Stage != StageFailed || entry.Signer != "" || entry.Nonce != "" || entry.TransactionHash != "" || entry.BlockNumber != 0 || entry.BlockHash != "" || entry.RecoveryBlock != 0 || entry.RecoveryBlockHash != "" || entry.PostconditionHash != "" || entry.PostconditionPath != "" {
			return failure
		}
		actionFound, journalHash, latest = true, entry.EntryHash, entry
	}
	if !anchorFound || !actionFound || latest.Stage != StageFailed || latest.Error != refusal.Error() {
		return errors.Join(failure, errors.New("pending probe repair lacks its authenticated budget or unsigned journal frontier"))
	}
	pending := self.precompileRecoveryGasPending
	if pending == nil || pending.failure != refusal || pending.planHash != self.plan.PlanHash || pending.authorizationHash != authorization.Hash || pending.journalHash != journalHash {
		// The exclusive journal writer owns signing throughout this cache's
		// lifetime. Re-authenticate the saved-signature census when its exact
		// action frontier or authority changes, not on every observation.
		if err := requireUnsignedPrecompileRecovery(self.stateDir, evidence, self.journal.Entries()); err != nil {
			return errors.Join(failure, err)
		}
	}
	self.precompileRecoveryGasPending = &precompileRecoveryGasPending{failure: refusal, planHash: self.plan.PlanHash, authorizationHash: authorization.Hash, journalHash: journalHash}
	return fmt.Errorf("%w: unsigned action=%s authorization=%s padded_gas=%d approved_gas=%d outstanding_move_alpha_rao=%d; explicit authority amendment required", errPrecompileRecoveryPending, step.Action.ID, authorization.Hash, refusal.paddedGas, refusal.maximumGas, move)
}

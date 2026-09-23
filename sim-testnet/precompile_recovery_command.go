// Post-interval cleanup uses the existing exclusive journal and transaction
// sender. It starts no topology, scenario, acceptance window or historical audit.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// Refuses a stale reserve allocation before signing. Already signed repair
// transactions are charged by the ordinary exposure census; only their unsigned
// remainder is added here, avoiding duplicate charging after a crash.
func (self *Executor) validatePrecompileRecoveryBudget(evidence *PrecompileConformanceEvidence) error {
	if evidence == nil || evidence.Recovery == nil {
		return errors.New("probe recovery budget has no authority")
	}
	authorization := evidence.Recovery.Authorization
	entries := self.journal.Entries()
	found := false
	for _, entry := range entries {
		if entry.EntryHash == authorization.Request.Budget.JournalHash && entry.PlanHash == self.plan.PlanHash {
			found = true
			break
		}
	}
	if !found {
		return errors.New("probe recovery budget has no retained journal anchor")
	}
	external, err := readFleetRenewalQueueTransactions(self.cfg, self.stateDir)
	if err != nil {
		return err
	}
	exposure, err := fleetRenewalCampaignExposure(self.stateDir, self.plan, entries, external)
	if err != nil {
		return err
	}
	liability, err := exposure.Liability.Big()
	if err != nil {
		return err
	}
	remaining, err := authorization.Request.Budget.MaximumRecoveryWei.Big()
	if err != nil {
		return err
	}
	for _, step := range evidence.Recovery.Steps {
		if _, signed := self.journal.LatestTransaction(self.plan.PlanHash, step.Action.ID, step.Action.IntentHash); !signed {
			continue
		}
		gas, err := step.Action.Spend.EVMGasWei.Big()
		if err != nil {
			return err
		}
		remaining.Sub(remaining, gas)
		value := new(big.Int).Mul(new(big.Int).SetUint64(step.Action.Spend.TAORao), big.NewInt(1_000_000_000))
		remaining.Sub(remaining, value)
	}
	reserved, err := authorization.Request.Budget.CampaignReserveWei.Big()
	if err != nil {
		return err
	}
	if remaining.Sign() < 0 || new(big.Int).Add(liability, remaining).Cmp(reserved) > 0 {
		return errors.New("probe recovery and existing signed liabilities exceed retained campaign reserve")
	}
	return nil
}

func runPrecompileRecovery(ctx context.Context, cfg *ResolvedConfig, stateDir string, options cliOptions) error {
	if err := validatePrecompileRecoveryOptions("probe-recovery", options); err != nil {
		return err
	}
	if !options.ProbeRecoveryExecute {
		return errors.New("probe recovery execution was not selected")
	}
	plan, err := loadInvocationPlan(cfg, stateDir, "probe-recovery", options)
	if err != nil {
		return err
	}
	// Acquire before provenance or evidence mutation: the interval's existing
	// writer remains authoritative until it has fully exited.
	journal, err := OpenJournal(stateDir)
	if err != nil {
		return err
	}
	defer journal.Close()
	if err := prepareProvisionalResume(ctx, cfg, stateDir, "probe-recovery", options, plan); err != nil {
		return err
	}
	if err := prepareProvisionalRPCOverride(cfg, stateDir, options.ProvisionalRPCAuthority); err != nil {
		return err
	}
	evidence, err := loadPrecompileEvidence(stateDir)
	if err != nil {
		return err
	}
	var roles RoleSecrets
	if err := readJSONFile(filepath.Join(stateDir, "secrets", "roles.json"), &roles); err != nil {
		return err
	}
	if err := validateCoordinatorRepairSigningRoles(plan, &roles); err != nil {
		return err
	}
	owner := &Executor{cfg: cfg, plan: plan, stateDir: stateDir, journal: journal, roles: &roles}
	if err := owner.validatePrecompileEvidence(approvedPrecompileProbe(plan), evidence); err != nil {
		return err
	}
	if err := owner.loadPrecompileRecoveryAuthorization(evidence); err != nil {
		return err
	}
	if !precompileEvidenceComplete(evidence) {
		if err := owner.validatePrecompileRecoveryBudget(evidence); err != nil {
			return err
		}
	}
	active, err := supervisedCampaignEgressActive(ctx, stateDir)
	if err != nil {
		return err
	}
	runtimeCfg, err := selectReadOnlyRPCConfig(cfg, active)
	if err != nil {
		return err
	}
	executor, err := newPrecompileRecoveryExecutor(ctx, cfg, runtimeCfg, stateDir, plan, journal, &roles, evidence)
	if err != nil {
		return err
	}
	defer executor.Close()
	// Exact saved completion is immutable and idempotent, even when the chain's
	// current head has advanced since the first successful strict replay.
	var completion PrecompileRecoveryCompletion
	if err := readJSONFile(filepath.Join(stateDir, precompileRecoveryCompletionFilename), &completion); err == nil {
		if err := verifyPrecompileRecoveryCompletion(plan, evidence, &completion); err != nil {
			return err
		}
		if err := executor.verifyPrecompileRecoveryFinalEvidence(ctx, completion.Record.FinalizedHead, evidence); err != nil {
			return err
		}
		return printResult(options.Format, &completion, nil)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	for turn := 0; turn < 64; turn++ {
		evidence, err = loadPrecompileEvidence(stateDir)
		if err != nil {
			return err
		}
		err = precompileRecoveryTurn(runCtx, plan, evidence, executor.Execute, executor.executePrecompileContinuationAction)
		if err == nil {
			current, readErr := loadPrecompileEvidence(stateDir)
			if readErr != nil {
				return readErr
			}
			if !precompileEvidenceComplete(current) {
				continue
			}
			completion, completeErr := executor.completePrecompileRecovery(runCtx)
			err = completeErr
			if err == nil {
				return printResult(options.Format, completion, nil)
			}
			if !historicalPreparationReadIsTransient(err) {
				return err
			}
		} else if !errors.Is(err, errPrecompileRecoveryPending) && !historicalPreparationReadIsTransient(err) {
			return err
		}
		current, readErr := loadPrecompileEvidence(stateDir)
		if readErr != nil {
			return readErr
		}
		if errors.Is(err, errPrecompileRecoveryPending) && current.EvidenceHash == evidence.EvidenceHash {
			return fmt.Errorf("probe recovery retained progress and needs a new observation or authority: %w", err)
		}
		if err := executor.validatePrecompileRecoveryBudget(current); err != nil {
			return err
		}
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("%w: bounded recovery turns exhausted; retained exact steps can resume", errPrecompileRecoveryPending)
}

// The returned execution error belongs to the turn owner, including the first
// missing dividend phase; a nested declaration cannot hide it from retry logic.
func precompileRecoveryTurn(ctx context.Context, plan *SetupPlan, evidence *PrecompileConformanceEvidence, dividend, transfer func(context.Context, Action) error) error {
	if evidence == nil || dividend == nil || transfer == nil {
		return errors.New("probe recovery turn is incomplete")
	}
	id, execute := "precompile.transfer-out", transfer
	if evidence.Dividend.DeltaRao == 0 {
		id, execute = "precompile.dividend", dividend
	}
	action, err := exactPlanActionByID(plan, id)
	if err != nil {
		return err
	}
	return execute(ctx, action)
}

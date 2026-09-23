// A refused unsigned repair retains its liability during provisional observation;
// it never expands signing authority or substitutes for final conformance.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"
)

func newPrecompileGasPendingFixture(t *testing.T) (*precompileRecoveryTestFixture, *Executor, error) {
	t.Helper()
	f, journal, _ := newPrecompileRecoveryExecutorFixture(t, false)
	f.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Command: "scenario", Scenario: "release-1.0", PlanHash: f.plan.PlanHash, Provisional: true}}
	action := f.evidence.Recovery.Steps[0].Action
	_, _, failure := validateEVMTransactionEnvelope(action, 600000, big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil), new(big.Int))
	var refusal *evmGasUnitCeilingError
	if !errors.As(failure, &refusal) {
		t.Fatalf("fixture did not reach the real pre-signing ceiling: %v", failure)
	}
	if err := journal.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFailed, Error: failure.Error()}); err != nil {
		t.Fatal(err)
	}
	return f, &Executor{cfg: f.cfg, plan: f.plan, journal: journal, stateDir: f.stateDir, payloads: f.base.payloads, roles: f.roles}, fmt.Errorf("action %s: %w", action.ID, failure)
}

// The live failure shape has one quoted unsigned top-up after a conserved
// residual. Two subsequent snapshots must keep observing without another send.
func TestPrecompileGasPendingKeepsUnsignedRepairAndObserver(t *testing.T) {
	f, owner, failure := newPrecompileGasPendingFixture(t)
	ctx := context.WithValue(t.Context(), precompileDividendSinglePollKey{}, true)
	if err := owner.deferPrecompileRecoveryGas(ctx, f.evidence, failure); !errors.Is(err, errPrecompileRecoveryPending) || !precompileContinuationMayWait(err) {
		t.Fatalf("accounted unsigned refusal remained terminal: %v", err)
	}
	before := len(owner.journal.Entries())
	action := actionByID(t, f.plan, "precompile.transfer-out")
	want := &ScenarioObservation{PrecompileConformance: f.evidence, PrecompileConformanceValid: true}
	observations := 0
	for range 2 {
		got, err := precompileContinuationSnapshot(ctx, func(ctx context.Context) error {
			return owner.executePrecompileContinuationAction(ctx, action)
		}, func(context.Context) (*ScenarioObservation, error) { observations++; return want, nil })
		if err != nil || got != want {
			t.Fatalf("pending repair stopped ordinary observation: %v", err)
		}
	}
	if observations != 2 || len(owner.journal.Entries()) != before || owner.deployer != nil {
		t.Fatal("unchanged refusal repeated intent, acquired a signer, or lost observation")
	}
	retained, err := loadPrecompileEvidence(f.stateDir)
	if err != nil || retained.EvidenceHash != f.evidence.EvidenceHash || precompileEvidenceComplete(retained) {
		t.Fatalf("pending refusal changed evidence or completed the repair: %v", err)
	}
	if passed, _ := precompileContinuationCompleteCheck().Check(&scenarioEvaluation{Current: want}); passed {
		t.Fatal("pending liability passed strict final acceptance")
	}
}

// Generic strings, joined integrity failures, strict commands, and absent durable
// failure records must never borrow this release-only continuation permission.
func TestPrecompileGasPendingRejectsWrongScopeAndCompositeFailure(t *testing.T) {
	f, owner, failure := newPrecompileGasPendingFixture(t)
	ctx := context.WithValue(t.Context(), precompileDividendSinglePollKey{}, true)
	for _, hard := range []error{errors.New(failure.Error()), errors.Join(failure, errors.New("changed receipt")), errors.Join(failure, context.Canceled)} {
		if err := owner.deferPrecompileRecoveryGas(ctx, f.evidence, hard); errors.Is(err, errPrecompileRecoveryPending) || precompileContinuationMayWait(err) {
			t.Fatalf("hard error acquired pending authority: %v", err)
		}
	}
	if err := owner.deferPrecompileRecoveryGas(t.Context(), f.evidence, failure); errors.Is(err, errPrecompileRecoveryPending) {
		t.Fatal("standalone repair ignored its signed ceiling")
	}
	owner.precompileRecoveryOnly = true
	if err := owner.deferPrecompileRecoveryGas(ctx, f.evidence, failure); errors.Is(err, errPrecompileRecoveryPending) {
		t.Fatal("scoped standalone repair borrowed release permission")
	}
	owner.precompileRecoveryOnly = false
	action := f.evidence.Recovery.Steps[0].Action
	if err := owner.journal.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	if err := owner.deferPrecompileRecoveryGas(ctx, f.evidence, failure); errors.Is(err, errPrecompileRecoveryPending) {
		t.Fatal("missing durable failure frontier acquired pending permission")
	}
}

// A crash can save signed bytes before its broadcast row. The pending classifier
// must inspect custody and reject those bytes even when the journal looks unsigned.
func TestPrecompileGasPendingRejectsOrphanSignature(t *testing.T) {
	f, owner, failure := newPrecompileGasPendingFixture(t)
	for _, transaction := range f.reader.transactions {
		if transaction.To() == nil || *transaction.To() != approvedPrecompileProbe(f.plan) {
			continue
		}
		raw, err := transaction.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(f.stateDir, "transactions", stringsTrim0x(transaction.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		ctx := context.WithValue(t.Context(), precompileDividendSinglePollKey{}, true)
		if err := owner.deferPrecompileRecoveryGas(ctx, f.evidence, failure); errors.Is(err, errPrecompileRecoveryPending) || err == nil {
			t.Fatal("unjournaled signed probe call was treated as unsigned")
		}
		return
	}
	t.Fatal("fixture had no signed probe transaction")
}

// Explicit durable frontiers and chain heads prove that dividend waiting cannot
// starve the release observer or replace complete custody evidence with readiness.
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

// Supplies only the action identities used by the transition owner; full plan,
// receipt, nonce and budget authentication remains in the existing executor.
func precompileContinuationTestPlan() *SetupPlan {
	plan := &SetupPlan{}
	for _, id := range []string{"precompile.probe-deploy", "precompile.commitment-write", "precompile.commitment-restore", "precompile.read-battery", "precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.dividend", "precompile.transfer-out"} {
		plan.Actions = append(plan.Actions, Action{ID: id})
	}
	return plan
}

// Unexpected, missing, duplicated or reordered actions cannot disappear inside
// a prefix-based selector, and the approved setup plan is never modified.
func TestPrecompileContinuationRequiresExactActionOrder(t *testing.T) {
	plan := precompileContinuationTestPlan()
	preparation, continuation, err := splitPrecompileActions(plan)
	if err != nil || len(preparation) != 8 || len(continuation) != 2 || preparation[7].ID != "precompile.snapshot" || continuation[0].ID != "precompile.dividend" {
		t.Fatalf("invalid split: preparation=%v continuation=%v err=%v", preparation, continuation, err)
	}
	for _, mutation := range []func(*SetupPlan){
		func(plan *SetupPlan) { plan.Actions = plan.Actions[:9] },
		func(plan *SetupPlan) { plan.Actions = append(plan.Actions, Action{ID: "precompile.unknown"}) },
		func(plan *SetupPlan) { plan.Actions[2] = plan.Actions[1] },
		func(plan *SetupPlan) { plan.Actions[7], plan.Actions[8] = plan.Actions[8], plan.Actions[7] },
	} {
		candidate := precompileContinuationTestPlan()
		mutation(candidate)
		if _, _, err := splitPrecompileActions(candidate); err == nil {
			t.Fatalf("altered action set admitted: %v", candidate.Actions)
		}
	}
}

// A partial preparation invokes the same durable executor on resume, preserving
// completed bodies and never invoking the two observation-dependent actions.
func TestPrecompileContinuationPreparationResumesOnlyUnfinishedBodies(t *testing.T) {
	plan := precompileContinuationTestPlan()
	verified := map[string]bool{}
	var dispatched []string
	interrupt := true
	execute := func(ctx context.Context, action Action) error {
		if verified[action.ID] {
			return nil
		}
		if action.ID == "precompile.move-forward" && interrupt {
			interrupt = false
			return context.DeadlineExceeded
		}
		dispatched = append(dispatched, action.ID)
		verified[action.ID] = true
		return ctx.Err()
	}
	if err := executePrecompilePreparation(context.Background(), plan, execute); !errors.Is(err, context.DeadlineExceeded) || len(dispatched) != 5 {
		t.Fatalf("interrupted preparation: dispatched=%v err=%v", dispatched, err)
	}
	if err := executePrecompilePreparation(context.Background(), plan, execute); err != nil || len(dispatched) != 8 || verified["precompile.dividend"] || verified["precompile.transfer-out"] {
		t.Fatalf("resumed preparation: dispatched=%v err=%v", dispatched, err)
	}
}

// Multiple pending observation turns still reach the scenario snapshot each time
// without a wait, journal action, transfer, or synthetic conformance success.
func TestPrecompileContinuationPendingKeepsReleaseObserverRunning(t *testing.T) {
	plan := precompileContinuationTestPlan()
	reads, snapshots, actions := 0, 0, 0
	advance := func(ctx context.Context) error {
		return continuePrecompileActions(ctx, plan,
			func(action Action) (bool, error) {
				return action.ID != "precompile.dividend" && action.ID != "precompile.transfer-out", nil
			},
			func(context.Context) (bool, error) { reads++; return false, nil },
			func(context.Context, Action) error { actions++; return errors.New("pending proof must not dispatch") })
	}
	observe := func(context.Context) (*ScenarioObservation, error) {
		snapshots++
		return &ScenarioObservation{PrecompileConformanceValid: false}, nil
	}
	for index := 0; index < 4; index++ {
		observed, err := precompileContinuationSnapshot(context.Background(), advance, observe)
		if err != nil || observed == nil || observed.PrecompileConformanceValid {
			t.Fatalf("pending proof suppressed observation: observation=%v err=%v", observed, err)
		}
	}
	if reads != 4 || snapshots != 4 || actions != 0 {
		t.Fatalf("pending lifecycle: reads=%d snapshots=%d actions=%d", reads, snapshots, actions)
	}
}

// Transfer interruption does not repeat the verified dividend action. The next
// turn is restricted to the same outstanding transfer and single-poll capability.
func TestPrecompileContinuationRetainsDividendAcrossTransferInterruption(t *testing.T) {
	plan := precompileContinuationTestPlan()
	verified := map[string]bool{}
	for _, action := range plan.Actions[:8] {
		verified[action.ID] = true
	}
	reads := 0
	var executions []string
	interrupt := true
	execute := func(ctx context.Context, action Action) error {
		if once, _ := ctx.Value(precompileDividendSinglePollKey{}).(bool); !once {
			t.Fatal("continuation lost its single-poll scope")
		}
		executions = append(executions, action.ID)
		if action.ID == "precompile.transfer-out" && interrupt {
			interrupt = false
			return context.DeadlineExceeded
		}
		verified[action.ID] = true
		return nil
	}
	advance := func() error {
		return continuePrecompileActions(context.Background(), plan,
			func(action Action) (bool, error) { return verified[action.ID], nil },
			func(context.Context) (bool, error) { reads++; return true, nil }, execute)
	}
	if err := advance(); !errors.Is(err, context.DeadlineExceeded) || !verified["precompile.dividend"] || verified["precompile.transfer-out"] {
		t.Fatalf("interruption changed frontier: verified=%v err=%v", verified, err)
	}
	if err := advance(); err != nil || !verified["precompile.transfer-out"] || reads != 1 || !reflect.DeepEqual(executions, []string{"precompile.dividend", "precompile.transfer-out", "precompile.transfer-out"}) {
		t.Fatalf("resume repeated dividend: executions=%v reads=%d err=%v", executions, reads, err)
	}
}

// Missing preparation stays pending, whereas a substituted authenticated receipt
// remains a hard failure before any observation or remaining action is attempted.
func TestPrecompileContinuationRequiresAuthenticatedPreparationFrontier(t *testing.T) {
	plan := precompileContinuationTestPlan()
	for _, changed := range []bool{false, true} {
		calls := 0
		err := continuePrecompileActions(context.Background(), plan,
			func(action Action) (bool, error) {
				if action.ID == "precompile.snapshot" {
					if changed {
						return false, errors.New("snapshot receipt checksum mismatch")
					}
					return false, nil
				}
				return true, nil
			},
			func(context.Context) (bool, error) { calls++; return true, nil },
			func(context.Context, Action) error { calls++; return nil })
		if (err != nil) != changed || calls != 0 {
			t.Fatalf("changed=%t calls=%d err=%v", changed, calls, err)
		}
	}
}

// Only typed transient failures defer the continuation; an integrity error,
// even when joined with a timeout, must not be hidden by a healthy snapshot.
func TestPrecompileContinuationTransientReadsPreserveHardFailures(t *testing.T) {
	for _, failure := range []struct {
		err     error
		pending bool
	}{
		{err: errPrecompileDividendPending, pending: true},
		{err: fmt.Errorf("chain read: %w", context.DeadlineExceeded), pending: true},
		{err: errors.New("probe identity mismatch")},
		{err: errors.Join(context.DeadlineExceeded, errors.New("receipt identity mismatch"))},
		{err: context.Canceled},
	} {
		observed := false
		_, err := precompileContinuationSnapshot(context.Background(), func(context.Context) error { return failure.err }, func(context.Context) (*ScenarioObservation, error) {
			observed = true
			return &ScenarioObservation{}, nil
		})
		if observed != failure.pending || (err == nil) != failure.pending {
			t.Fatalf("failure=%v pending=%t observed=%t err=%v", failure.err, failure.pending, observed, err)
		}
	}
}

// Exact block and value transitions, including uint64 edges, cannot mint a
// positive proof early or turn a changed snapshot into a retryable delay.
func TestPrecompileContinuationDividendRequiresFullNativeWindow(t *testing.T) {
	evidence := &PrecompileConformanceEvidence{Snapshot: PrecompileSnapshotStep{BaselineRao: 100, SinceBlock: 500}}
	head := ChainHead{Number: 859, Hash: "0x" + strings.Repeat("12", 32)}
	if _, ready, err := evaluatePrecompileDividend(evidence, 360, head, 100, 101, 500); err != nil || ready {
		t.Fatalf("short window accepted: ready=%t err=%v", ready, err)
	}
	head.Number = 860
	if _, ready, err := evaluatePrecompileDividend(evidence, 360, head, 100, 100, 500); err != nil || ready {
		t.Fatalf("zero dividend accepted: ready=%t err=%v", ready, err)
	}
	step, ready, err := evaluatePrecompileDividend(evidence, 360, head, 100, 101, 500)
	if err != nil || !ready || step.DeltaRao != 1 || step.FinalizedHead != head {
		t.Fatalf("exact dividend rejected: step=%+v ready=%t err=%v", step, ready, err)
	}
	for _, values := range [][3]uint64{{101, 102, 500}, {100, 101, 501}, {100, 99, 500}} {
		if _, _, err := evaluatePrecompileDividend(evidence, 360, head, values[0], values[1], values[2]); err == nil {
			t.Fatalf("changed custody accepted: %v", values)
		}
	}
	evidence.Snapshot.SinceBlock = math.MaxUint64 - 10
	head.Number = math.MaxUint64
	if _, ready, err := evaluatePrecompileDividend(evidence, 360, head, 100, 101, evidence.Snapshot.SinceBlock); err != nil || ready {
		t.Fatalf("overflow forged maturity: ready=%t err=%v", ready, err)
	}
}

// A live deployment and even a forged ready flag cannot satisfy final acceptance
// without positive dividend evidence and exact complete custody recovery.
func TestPrecompileContinuationFinalGateRequiresCompleteEvidence(t *testing.T) {
	check := precompileContinuationCompleteCheck()
	evidence := completePrecompileEvidence()
	current := &ScenarioObservation{PrecompileConformance: evidence, PrecompileConformanceValid: true}
	evaluation := &scenarioEvaluation{Current: current}
	if passed, detail := check.Check(evaluation); !passed {
		t.Fatalf("complete evidence rejected: %s", detail)
	}
	evidence.Dividend = PrecompileDividendStep{}
	if passed, _ := check.Check(evaluation); passed {
		t.Fatal("prepared-only evidence passed final conformance")
	}
	current.PrecompileConformance = completePrecompileEvidence()
	current.PrecompileConformance.Transfer.ProbeAfterRao = 1
	if passed, _ := check.Check(evaluation); passed {
		t.Fatal("unrecovered probe stake passed final conformance")
	}
}

// The split is explicitly provisional and retains the exact mandatory plan hash.
func TestPrecompileContinuationPreparationCliRequiresExactProvisionalApproval(t *testing.T) {
	hash := "0x" + strings.Repeat("12", 32)
	approved := []string{"scenario", "--name", precompilePreparationScenario, "--provisional-resume", "--apply", "--plan-hash", hash}
	if _, _, err := parseCLI(approved); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"scenario", "--name", precompilePreparationScenario},
		{"scenario", "--name", precompilePreparationScenario, "--provisional-resume", "--apply"},
		append(append([]string(nil), approved...), "--detach"),
	} {
		if _, _, err := parseCLI(args); err == nil {
			t.Fatalf("ambiguous preparation admitted: %v", args)
		}
	}
}

// The consumer uses actual signed receipt/native proofs through the admitted
// fee producer. Its synthetic Go replay peer tests the ownership/protocol join;
// unchanged real-Wasm fee tests remain a separate qualification scope.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// This component fixture selects the Server export's synthetic network. It
// does not relax the public mainnet policy's separate fixed network admission.
func economicConservationFeeTestPolicy(request economicNativeFeeRequest) economicConservationPolicy {
	return economicConservationPolicy{Schema: economicConservationPolicySchema, MaximumFacts: 256, FeeAuthority: &request.Policy, Native: monitorEconomicNativePolicy{Observation: economicEmissionPolicy{Network: planNetwork{NativeChain: "synthetic-fee-proof", GenesisHash: request.Policy.Genesis, EvmChainId: request.Policy.EvmChainId}}}}
}

func economicConservationFeeTestAdmit(t *testing.T, mode string) (economicConservationPolicy, economicNativeFeeRequest, *economicConservationState) {
	t.Helper()
	request, _, _ := economicNativeFeeTestRequest(t, "success", mode)
	policy := economicConservationFeeTestPolicy(request)
	prior := newEconomicConservationState(policy)
	next, err := admitEconomicConservationNativeFees(t.Context(), policy, prior, request, time.Minute, historicalReplayHooks{})
	if err != nil || next == nil || len(prior.NativeFees) != 0 || len(next.NativeFees) != 1 {
		t.Fatal("completed original fee verifier did not reach detached conservation state", err)
	}
	return policy, request, next
}

func TestEconomicConservationConsumesOriginalNativeFeeProofAndKeepsPartialCensusUnknown(t *testing.T) {
	policy, request, state := economicConservationFeeTestAdmit(t, "pair")
	summary, err := state.feeSummary(policy)
	if err != nil || summary == nil || summary.OriginalRequests != 1 || summary.AuthenticatedFees != 1 || summary.MissingFees == 0 || summary.SelectedCensusComplete || summary.WholeProviderCensus || summary.WithdrawalRao != nil || summary.RefundRao != nil || summary.DebitRao != nil {
		t.Fatal("partial original receipt census became complete provider fees", summary, err)
	}
	retained := state.NativeFees[0]
	if retained.Request.Context != request.Context || retained.Evidence.Context.ContextProof == nil || retained.Evidence.Context.Replay == nil || retained.Evidence.RequestHash != rootObjectHash(request) {
		t.Fatal("conservation lost original native fee proof inputs")
	}
	for _, transaction := range retained.Evidence.Context.Transactions {
		if transaction.FeeAuthenticated && (*transaction.ActualWithdrawalRao != "1000" || *transaction.ActualRefundRao != "250" || *transaction.ActualDebitRao != "750" || transaction.NativeBlock == nil || transaction.Receipt == nil) {
			t.Fatal("conservation substituted gas cost or dropped original fee context")
		}
	}
}

func TestEconomicConservationNativeFeeAbsenceAndObservedZeroStayDistinct(t *testing.T) {
	for _, mode := range []string{"missing", "zero"} {
		policy, _, state := economicConservationFeeTestAdmit(t, mode)
		summary, err := state.feeSummary(policy)
		if err != nil || summary == nil || summary.SelectedCensusComplete {
			t.Fatal("partial fixture unexpectedly became all selected fees", mode, err)
		}
		if mode == "missing" && summary.AuthenticatedFees != 0 || mode == "zero" && summary.AuthenticatedFees != 1 {
			t.Fatal("missing fee and actual zero fee were conflated", mode, summary)
		}
		for _, transaction := range state.NativeFees[0].Evidence.Context.Transactions {
			if mode == "missing" && transaction.ActualRefundRao != nil || mode == "zero" && transaction.FeeAuthenticated && (transaction.ActualRefundRao == nil || *transaction.ActualRefundRao != "0") {
				t.Fatal("conservation changed original refund knowledge", mode, transaction)
			}
		}
	}
}

func TestEconomicConservationNativeFeeRetryDoesNotRerunOrRecount(t *testing.T) {
	policy, request, state := economicConservationFeeTestAdmit(t, "pair")
	before := state.hash()
	if err := os.Remove(request.Context.Job.Path); err != nil {
		t.Fatal(err)
	}
	starts := 0
	next, err := admitEconomicConservationNativeFees(t.Context(), policy, state, request, time.Minute, historicalReplayHooks{beforeStart: func(context.Context, *os.File) { starts++ }})
	if err != nil || next == nil || next.hash() != before || starts != 0 || len(next.NativeFees) != 1 {
		t.Fatal("retained original fee was rerun or counted twice after acknowledgment loss", err, starts)
	}
}

func TestEconomicConservationNativeFeeOverlappingArchivesCountOriginalOnce(t *testing.T) {
	policy, _, state := economicConservationFeeTestAdmit(t, "pair")
	request, _, _ := economicNativeFeeTestRequest(t, "success", "pair")
	next, err := admitEconomicConservationNativeFees(t.Context(), policy, state, request, time.Minute, historicalReplayHooks{})
	if err != nil || next == nil || len(next.NativeFees) != 2 {
		t.Fatal("second original archive request failed", err)
	}
	priorSummary, err := state.feeSummary(policy)
	if err != nil {
		t.Fatal(err)
	}
	nextSummary, err := next.feeSummary(policy)
	if err != nil || nextSummary.OriginalRequests != 2 || nextSummary.SelectedTransactions != priorSummary.SelectedTransactions || nextSummary.AuthenticatedFees != priorSummary.AuthenticatedFees || state.NativeFees[0].Evidence.ContentHash != next.NativeFees[0].Evidence.ContentHash {
		t.Fatal("overlapping archive repeated or replaced an original fee", err, priorSummary, nextSummary)
	}
}

func TestEconomicConservationNativeFeeConflictingOriginalRefundCannotReplaceKnownEvidence(t *testing.T) {
	policy, _, state := economicConservationFeeTestAdmit(t, "pair")
	before := state.hash()
	request, _, _ := economicNativeFeeTestRequest(t, "success", "zero")
	next, err := admitEconomicConservationNativeFees(t.Context(), policy, state, request, time.Minute, historicalReplayHooks{})
	if err == nil || next != nil || !strings.Contains(err.Error(), "contradicts an original retained transaction") || state.hash() != before {
		t.Fatal("new fee request rewrote an original known refund", err)
	}
}

func TestEconomicConservationNativeFeeArchivedOriginalCannotDisappearFromRestoredHead(t *testing.T) {
	policy, _, state := economicConservationFeeTestAdmit(t, "pair")
	request, _, _ := economicNativeFeeTestRequest(t, "success", "pair")
	var err error
	state, err = admitEconomicConservationNativeFees(t.Context(), policy, state, request, time.Minute, historicalReplayHooks{})
	if err != nil {
		t.Fatal(err)
	}
	view := newEconomicConservationArchiveView(policy.initialResources())
	if err := view.retainFeeEvidence(state); err != nil {
		t.Fatal(err)
	}
	state.archiveView = view
	if _, err := state.feeSummary(policy); err != nil {
		t.Fatal("unchanged archived fee evidence refused", err)
	}
	before := view.entries
	if err := view.retainFeeEvidence(state); err != nil || view.entries != before {
		t.Fatal("same original fee index was charged twice", err)
	}
	for _, retained := range [][]economicConservationFeeEvidence{nil, state.NativeFees[:1]} {
		changed := *state
		changed.NativeFees = retained
		changed.ContentHash = changed.hash()
		if _, err := changed.feeSummary(policy); err == nil {
			t.Fatal("restored conservation head dropped archived native fee evidence")
		}
	}
}

func TestEconomicConservationNativeFeeCapacityRefusalPreservesCompletePriorProofs(t *testing.T) {
	request, _, _ := economicNativeFeeTestRequest(t, "success", "pair")
	policy := economicConservationFeeTestPolicy(request)
	policy.MaximumFacts = 16
	state := newEconomicConservationState(policy)
	for index := 0; index < 3; index++ {
		if index != 0 {
			request, _, _ = economicNativeFeeTestRequest(t, "success", "pair")
		}
		before := state.hash()
		next, err := admitEconomicConservationNativeFees(t.Context(), policy, state, request, time.Minute, historicalReplayHooks{})
		if index == 2 {
			if !errors.Is(err, errMonitorEconomicCapacity) || next != nil || state.hash() != before || len(state.NativeFees) != 2 {
				t.Fatal("fee capacity refusal dropped or admitted partial original proofs", err)
			}
		} else {
			if err != nil || next == nil {
				t.Fatal("finite fee fixture did not reach capacity boundary", err)
			}
			state = next
		}
	}
}

func TestEconomicConservationNativeFeeCancellationPreservesCauseEvenForRetainedInput(t *testing.T) {
	policy, request, state := economicConservationFeeTestAdmit(t, "pair")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	next, err := admitEconomicConservationNativeFees(ctx, policy, state, request, time.Minute, historicalReplayHooks{})
	if next != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("retained fee input bypassed canceled owner", err)
	}
}

func TestEconomicConservationPublicFeeInputCannotEnrollItsOwnAuthority(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	request, _, _ := economicNativeFeeTestRequest(t, "success", "pair")
	args := append(f.args(t), "--native-fee-request", request.Approval.Path, "--native-fee-request-sha256", request.Approval.Sha256)
	var output, diagnostic bytes.Buffer
	code := runMonitorStorageTestWithHooks(t, t.Context(), args, &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{})
	if code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "original policy authority") {
		t.Fatal("optional input enrolled new economic fee authority", code, diagnostic.String())
	}
}

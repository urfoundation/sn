// These arithmetic roots start with internal already-derived facts. They do
// not authenticate a capture; separate public roots run actual native replay,
// receipt/artifact admission, retirement and reopen with the same constraints.
package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func economicFundingFlowFixture(t *testing.T) *economicConservationState {
	t.Helper()
	total := "100"
	return &economicConservationState{
		Captures:     []economicConservationCapture{{Id: "synthetic-capture", Event: monitorEconomicEvmEvent{ReceiptHash: "synthetic-receipt", Values: map[string]string{"epoch": "1", "noId": "1", "amount": "100"}}, PrincipalEffects: &economicConservationCaptureEffects{Schema: economicCaptureEffectsSchema, CaptureId: "synthetic-capture", ReceiptHash: "synthetic-receipt", OpeningStock: "50", Deposits: "0", Withdrawals: "0", Refunds: "0", LiquidEarnings: "50", Captured: "100", After: "0"}}},
		Entitlements: []economicConservationEntitlement{{Id: "1/1", Epoch: "1", PoolId: "1", Funded: "100", Total: &total, Claimed: "0", Status: "finalized", Sources: []economicConservationBacking{{Kind: "capture", Id: "synthetic-capture", Amount: "100"}}}},
	}
}

func economicFundingFlowClaim(id, entitlement, amount string) economicConservationClaim {
	return economicConservationClaim{Id: id, Entitlement: entitlement, Status: "original-entitlement-observed", Event: monitorEconomicEvmEvent{Values: map[string]string{"amount": amount}}}
}

func economicFundingFlowPayment(id, amount string, claims ...string) economicConservationPayment {
	return economicConservationPayment{Id: id, Status: "aggregate-credit-observed", Event: monitorEconomicEvmEvent{Values: map[string]string{"amount": amount}}, Credit: economicConservationCredit{Opening: "0", Claims: claims}}
}

func economicFundingFlowSummary(t *testing.T, state *economicConservationState) *economicConservationFundingSummary {
	t.Helper()
	result, err := state.fundingSummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Either first leaf can be entirely income. The disjoint pair cannot both
// consume the same fifty units, including when paid in separate transfers.
func TestEconomicFundingJointPartialClaimsExhaustOriginalObligation(t *testing.T) {
	state := economicFundingFlowFixture(t)
	state.Claims = []economicConservationClaim{economicFundingFlowClaim("first", "1/1", "50")}
	state.Payments = []economicConservationPayment{economicFundingFlowPayment("first-payment", "50", "first")}
	first := economicFundingFlowSummary(t, state)
	economicFundingTestRange(t, first.Accepted, "50", "0", "50", "0", "50", true)
	economicFundingTestRange(t, first.Paid, "50", "0", "50", "0", "50", true)
	if first.NoNonIncomeProviderCredit != nil {
		t.Fatal("a partial original leaf acquired an invented source color", first)
	}
	state.Claims = append(state.Claims, economicFundingFlowClaim("second", "1/1", "50"))
	state.Payments = append(state.Payments, economicFundingFlowPayment("second-payment", "50", "second"))
	result := economicFundingFlowSummary(t, state)
	economicFundingTestRange(t, result.Accepted, "100", "50", "50", "50", "50", true)
	economicFundingTestRange(t, result.Paid, "100", "50", "50", "50", "50", true)
	if result.NoNonIncomeProviderCredit == nil || *result.NoNonIncomeProviderCredit || result.OriginalClaims != 2 || result.OriginalPayments != 2 {
		t.Fatal("exhausted original obligation hid definite capital consumption", result)
	}
}

// Paid is the exact selected credit set, not all accepted leaves. Three
// quarters select between twenty-five and fifty income units; adding the last
// unpaid quarter to the payment would falsely narrow that interval to fifty.
func TestEconomicFundingJointPaymentUsesOnlyItsOriginalClaimSubset(t *testing.T) {
	state := economicFundingFlowFixture(t)
	state.Claims = []economicConservationClaim{economicFundingFlowClaim("a", "1/1", "25"), economicFundingFlowClaim("b", "1/1", "50"), economicFundingFlowClaim("unpaid", "1/1", "25")}
	state.Payments = []economicConservationPayment{economicFundingFlowPayment("selected", "75", "a", "b")}
	result := economicFundingFlowSummary(t, state)
	economicFundingTestRange(t, result.Accepted, "100", "50", "50", "50", "50", true)
	economicFundingTestRange(t, result.Paid, "75", "25", "50", "25", "50", true)
	resolver, err := newEconomicFundingResolver(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	value, err := resolver.payment(state.Payments[0])
	if err != nil || value != result.Paid {
		t.Fatal("single payment and whole selected transfer set disagree", value, err)
	}
}

func economicFundingFlowCarryFixture(t *testing.T) *economicConservationState {
	t.Helper()
	state := economicFundingFlowFixture(t)
	total := "50"
	state.Entitlements[0].Status, state.Entitlements[0].Claimed = "carried", "50"
	state.Entitlements = append(state.Entitlements, economicConservationEntitlement{Id: "2/1", Epoch: "2", PoolId: "1", Funded: "0", Total: &total, Claimed: "50", Status: "finalized", Sources: []economicConservationBacking{{Kind: "expired-entitlement", Id: "1/1", Amount: "50"}}})
	state.Claims = []economicConservationClaim{economicFundingFlowClaim("old", "1/1", "50"), economicFundingFlowClaim("new", "2/1", "50")}
	return state
}

// Paying the successor first does not identify its color. When the older
// credit is paid later, its flow must update the already selected successor.
func TestEconomicFundingJointExpiredCarryAndLatePaymentShareOriginalSource(t *testing.T) {
	state := economicFundingFlowCarryFixture(t)
	state.Payments = []economicConservationPayment{economicFundingFlowPayment("new-payment", "50", "new")}
	first := economicFundingFlowSummary(t, state)
	economicFundingTestRange(t, first.Accepted, "100", "50", "50", "50", "50", true)
	economicFundingTestRange(t, first.Paid, "50", "0", "50", "0", "50", true)
	state.Payments = append(state.Payments, economicFundingFlowPayment("late-old-payment", "50", "old"))
	result := economicFundingFlowSummary(t, state)
	economicFundingTestRange(t, result.Paid, "100", "50", "50", "50", "50", true)
	state.Claims[0], state.Claims[1] = state.Claims[1], state.Claims[0]
	state.Payments[0], state.Payments[1] = state.Payments[1], state.Payments[0]
	reordered := economicFundingFlowSummary(t, state)
	if !reflect.DeepEqual(result, reordered) {
		t.Fatal("selection iteration order invented a source coloring", result, reordered)
	}
}

// Future income cannot flow backwards into a previously accepted capital
// claim. A global pool-only cap would lose this original carry constraint.
func TestEconomicFundingJointCarryKeepsFutureIncomeOutOfEarlierClaims(t *testing.T) {
	state := economicFundingFlowCarryFixture(t)
	state.Captures[0].PrincipalEffects.OpeningStock, state.Captures[0].PrincipalEffects.LiquidEarnings = "100", "0"
	second := state.Captures[0]
	second.Id, second.Event = "future-income", monitorEconomicEvmEvent{ReceiptHash: "future-receipt", Values: map[string]string{"epoch": "2", "noId": "1", "amount": "100"}}
	second.PrincipalEffects = &economicConservationCaptureEffects{Schema: economicCaptureEffectsSchema, CaptureId: second.Id, ReceiptHash: second.Event.ReceiptHash, OpeningStock: "0", Deposits: "0", Withdrawals: "0", Refunds: "0", LiquidEarnings: "100", Captured: "100", After: "0"}
	state.Captures = append(state.Captures, second)
	total := "150"
	state.Entitlements[1].Funded, state.Entitlements[1].Total = "100", &total
	state.Entitlements[1].Sources = append(state.Entitlements[1].Sources, economicConservationBacking{Kind: "capture", Id: second.Id, Amount: "100"})
	state.Payments = []economicConservationPayment{economicFundingFlowPayment("old-payment", "50", "old"), economicFundingFlowPayment("new-payment", "50", "new")}
	result := economicFundingFlowSummary(t, state)
	economicFundingTestRange(t, result.Accepted, "100", "0", "50", "50", "100", true)
	economicFundingTestRange(t, result.Paid, "100", "0", "50", "50", "100", true)
}

// The seventy-five already accepted income units remain unpaid. They cannot
// finance the successor, whose receipt carries only the other twenty-five.
func TestEconomicFundingJointPaidSubsetRespectsOriginalCarryCapacity(t *testing.T) {
	state := economicFundingFlowCarryFixture(t)
	state.Captures[0].PrincipalEffects.OpeningStock, state.Captures[0].PrincipalEffects.LiquidEarnings = "0", "100"
	state.Entitlements[0].Claimed = "75"
	state.Claims[0].Event.Values["amount"] = "75"
	second := state.Captures[0]
	second.Id, second.Event = "future-capital", monitorEconomicEvmEvent{ReceiptHash: "future-capital-receipt", Values: map[string]string{"epoch": "2", "noId": "1", "amount": "75"}}
	second.PrincipalEffects = &economicConservationCaptureEffects{Schema: economicCaptureEffectsSchema, CaptureId: second.Id, ReceiptHash: second.Event.ReceiptHash, OpeningStock: "75", Deposits: "0", Withdrawals: "0", Refunds: "0", LiquidEarnings: "0", Captured: "75", After: "0"}
	state.Captures = append(state.Captures, second)
	total := "100"
	state.Entitlements[1].Funded, state.Entitlements[1].Total, state.Entitlements[1].Claimed = "75", &total, "100"
	state.Entitlements[1].Sources = []economicConservationBacking{{Kind: "expired-entitlement", Id: "1/1", Amount: "25"}, {Kind: "capture", Id: second.Id, Amount: "75"}}
	state.Claims[1].Event.Values["amount"] = "100"
	state.Payments = []economicConservationPayment{economicFundingFlowPayment("new-payment", "100", "new")}
	result := economicFundingFlowSummary(t, state)
	economicFundingTestRange(t, result.Accepted, "175", "100", "100", "75", "75", true)
	economicFundingTestRange(t, result.Paid, "100", "25", "25", "75", "75", true)
}

// This internal helper models the funding part of admitted retirement. The
// separate public test authenticates the source checkpoint and its receipts.
func economicFundingFlowRetire(t *testing.T, view *economicConservationArchiveView, original, compacted *economicConservationState) {
	t.Helper()
	for _, claim := range original.Claims {
		view.claims[claim.Id] = claim
	}
	for _, entitlement := range original.Entitlements {
		view.entitlements[entitlement.Id] = entitlement
	}
	if err := view.retainFundingComposition(t.Context(), original, compacted); err != nil {
		t.Fatal(err)
	}
	compacted.archiveView = view
}

func economicFundingFlowView() *economicConservationArchiveView {
	return newEconomicConservationArchiveView(economicConservationResources{IndexEntries: 4096, IndexBytes: 4 * 1024 * 1024})
}

// First retirement contains only half the obligation. The other half joins
// through a private overlay, and replaying both original retirement segments
// reconstructs the same bound without counting any claim or payment twice.
func TestEconomicFundingJointColdAndActiveExhaustionReplaysOriginalSegments(t *testing.T) {
	first := economicFundingFlowFixture(t)
	first.Claims = []economicConservationClaim{economicFundingFlowClaim("first", "1/1", "50")}
	first.Payments = []economicConservationPayment{economicFundingFlowPayment("first-payment", "50", "first")}
	second := &economicConservationState{Entitlements: first.Entitlements}
	view := economicFundingFlowView()
	economicFundingFlowRetire(t, view, first, second)
	before, entries, bytes := view.funding.accepted, view.entries, view.bytes
	second.Claims = []economicConservationClaim{economicFundingFlowClaim("second", "1/1", "50")}
	second.Payments = []economicConservationPayment{economicFundingFlowPayment("second-payment", "50", "second")}
	result := economicFundingFlowSummary(t, second)
	economicFundingTestRange(t, result.Accepted, "100", "50", "50", "50", "50", true)
	economicFundingTestRange(t, result.Paid, "100", "50", "50", "50", "50", true)
	if view.funding.accepted != before || view.entries != entries || view.bytes != bytes || len(view.funding.acceptedSelection.claims) != 1 {
		t.Fatal("live funding projection mutated admitted cold originals")
	}
	third := &economicConservationState{Entitlements: first.Entitlements}
	economicFundingFlowRetire(t, view, second, third)
	cold := economicFundingFlowSummary(t, third)
	if !reflect.DeepEqual(result, cold) {
		t.Fatal("second retirement lost the shared original constraint", result, cold)
	}
	reopened := economicFundingFlowView()
	firstCompact := &economicConservationState{Entitlements: first.Entitlements}
	economicFundingFlowRetire(t, reopened, first, firstCompact)
	secondOriginal := *second
	secondOriginal.archiveView = reopened
	last := &economicConservationState{Entitlements: first.Entitlements}
	economicFundingFlowRetire(t, reopened, &secondOriginal, last)
	if again := economicFundingFlowSummary(t, last); !reflect.DeepEqual(cold, again) {
		t.Fatal("original segment replay changed correlated funding", cold, again)
	}
}

// An older accepted credit can remain hot while its already paid successor is
// cold. Late payment must still revise the retained successor's source choice.
func TestEconomicFundingJointColdSuccessorKeepsLatePredecessorPayment(t *testing.T) {
	first := economicFundingFlowCarryFixture(t)
	first.Payments = []economicConservationPayment{economicFundingFlowPayment("new-payment", "50", "new")}
	next := &economicConservationState{Entitlements: first.Entitlements, Claims: first.Claims[:1]}
	view := economicFundingFlowView()
	economicFundingFlowRetire(t, view, first, next)
	next.Payments = []economicConservationPayment{economicFundingFlowPayment("old-payment", "50", "old")}
	result := economicFundingFlowSummary(t, next)
	economicFundingTestRange(t, result.Accepted, "100", "50", "50", "50", "50", true)
	economicFundingTestRange(t, result.Paid, "100", "50", "50", "50", "50", true)
	if view.funding.paid.Amount != "50" || view.funding.paid.MinimumIncome != "0" || len(view.funding.paidSelection.claims) != 1 {
		t.Fatal("late payment projection rewrote original cold selection")
	}
}

func TestEconomicFundingJointUnknownSourcesStayUnknownAtExhaustion(t *testing.T) {
	state := economicFundingFlowFixture(t)
	state.Captures[0].PrincipalEffects = nil
	state.Claims = []economicConservationClaim{economicFundingFlowClaim("a", "1/1", "50"), economicFundingFlowClaim("b", "1/1", "50")}
	state.Payments = []economicConservationPayment{economicFundingFlowPayment("payment", "100", "a", "b")}
	result := economicFundingFlowSummary(t, state)
	economicFundingTestRange(t, result.Accepted, "100", "0", "100", "0", "100", false)
	economicFundingTestRange(t, result.Paid, "100", "0", "100", "0", "100", false)
	if result.NoNonIncomeProviderCredit != nil || result.CapitalSubsidyAuthorized {
		t.Fatal("exhaustion authenticated unknown original sources", result)
	}
}

func TestEconomicFundingJointUnknownOpeningCreditDoesNotBorrowClaimIncome(t *testing.T) {
	state := economicFundingFlowFixture(t)
	state.Captures[0].PrincipalEffects.OpeningStock, state.Captures[0].PrincipalEffects.LiquidEarnings = "0", "100"
	state.Claims = []economicConservationClaim{economicFundingFlowClaim("income", "1/1", "100")}
	state.Payments = []economicConservationPayment{economicFundingFlowPayment("payment", "105", "income")}
	state.Payments[0].Credit.Opening = "5"
	result := economicFundingFlowSummary(t, state)
	economicFundingTestRange(t, result.Accepted, "100", "100", "100", "0", "0", true)
	economicFundingTestRange(t, result.Paid, "105", "100", "105", "0", "5", false)
	if result.NoNonIncomeProviderCredit != nil {
		t.Fatal("known claim income authenticated unknown opening credit", result)
	}
}

// Two captured/entitlement facts plus two independent selection-flow nodes
// require four entries. The admitted half-budget here permits only three.
func TestEconomicFundingJointColdSourceNodesConsumeReviewedIndexCapacity(t *testing.T) {
	state := economicFundingFlowFixture(t)
	state.Entitlements[0].Status, state.Entitlements[0].Total = "root-missed", nil
	view := newEconomicConservationArchiveView(economicConservationResources{IndexEntries: 6, IndexBytes: 4 * 1024 * 1024})
	if err := view.retainFundingComposition(t.Context(), state, &economicConservationState{}); !errors.Is(err, errMonitorEconomicCapacity) {
		t.Fatal("new cold source-flow facts bypassed reviewed index capacity", err)
	}
}

func TestEconomicFundingJointSelectionRejectsRepeatedClaimAcrossPayments(t *testing.T) {
	state := economicFundingFlowFixture(t)
	state.Claims = []economicConservationClaim{economicFundingFlowClaim("only", "1/1", "50")}
	state.Payments = []economicConservationPayment{economicFundingFlowPayment("a", "50", "only"), economicFundingFlowPayment("b", "50", "only")}
	if result, err := state.fundingSummary(t.Context()); result != nil || err == nil || !strings.Contains(err.Error(), "repeats an original accepted claim") {
		t.Fatal("repeated original payment leaf manufactured exhaustion", result, err)
	}
}

func TestEconomicFundingJointSelectionRejectsCarryOverlapAndChangedBinding(t *testing.T) {
	state := economicFundingFlowCarryFixture(t)
	state.Claims[0].Event.Values["amount"] = "51"
	if result, err := state.fundingSummary(t.Context()); result != nil || err == nil || !strings.Contains(err.Error(), "carry") {
		t.Fatal("claim and carry consumed the same original unit", result, err)
	}
	state = economicFundingFlowFixture(t)
	state.Claims = []economicConservationClaim{economicFundingFlowClaim("a", "1/1", "50")}
	next := &economicConservationState{Captures: state.Captures, Entitlements: append([]economicConservationEntitlement(nil), state.Entitlements...)}
	view := economicFundingFlowView()
	economicFundingFlowRetire(t, view, state, next)
	next.Entitlements[0].Sources = []economicConservationBacking{{Kind: "opening-funding-unattributed", Id: "different-original", Amount: "100"}}
	next.Claims = []economicConservationClaim{economicFundingFlowClaim("b", "1/1", "50")}
	if result, err := next.fundingSummary(t.Context()); result != nil || err == nil || !strings.Contains(err.Error(), "original source binding") {
		t.Fatal("equal total replaced the cold original source graph", result, err)
	}
}

func TestEconomicFundingJointCanceledProjectionKeepsColdIndex(t *testing.T) {
	state := economicFundingFlowFixture(t)
	state.Claims = []economicConservationClaim{economicFundingFlowClaim("a", "1/1", "50")}
	next := &economicConservationState{Entitlements: state.Entitlements}
	view := economicFundingFlowView()
	economicFundingFlowRetire(t, view, state, next)
	before, entries, bytes := view.funding.accepted, view.entries, view.bytes
	ctx, cancel := context.WithCancel(t.Context())
	resolver, err := newEconomicFundingResolver(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	selected := newEconomicFundingSelection(view.funding.acceptedSelection)
	cancel()
	if err := selected.claim(resolver, economicFundingFlowClaim("b", "1/1", "50")); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled selection accepted another original claim", err)
	}
	if view.funding.accepted != before || view.entries != entries || view.bytes != bytes || len(view.funding.acceptedSelection.claims) != 1 {
		t.Fatal("canceled overlay changed admitted original funding")
	}
}

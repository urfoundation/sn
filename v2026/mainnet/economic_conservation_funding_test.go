// Source composition is conservative when fungible stake is withdrawn or a
// partial leaf is claimed. These ordinary roots exercise exact integer bounds;
// separate public roots obtain the sources from actual execution and receipts.
package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Independent expectations specify both complementary source components.
func economicFundingTestRange(t *testing.T, actual economicFundingRange, amount, minimum, maximum, capitalMinimum, capitalMaximum string, complete bool) {
	t.Helper()
	expected := economicFundingRange{Amount: amount, MinimumIncome: minimum, MaximumIncome: maximum, MinimumNonIncome: capitalMinimum, MaximumNonIncome: capitalMaximum, Complete: complete}
	if actual != expected {
		t.Fatal("original funding bounds differ", actual, expected)
	}
}

// Removing three units cannot tell whether those units were earnings or stock.
func TestEconomicFundingWithdrawalAndPartialCreditDoNotInventColoring(t *testing.T) {
	original, err := newEconomicFundingRange("27", "6", "6", true)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := original.subset("24")
	if err != nil {
		t.Fatal(err)
	}
	economicFundingTestRange(t, captured, "24", "3", "6", "18", "21", true)
	accepted, err := captured.subset("23")
	if err != nil {
		t.Fatal(err)
	}
	economicFundingTestRange(t, accepted, "23", "2", "6", "17", "21", true)
	if _, err := captured.subset("25"); err == nil {
		t.Fatal("funding subset exceeded its original amount")
	}
	for _, bounds := range [][3]string{{"1", "2", "2"}, {"2", "2", "1"}, {"01", "0", "1"}, {"-1", "0", "0"}} {
		if _, err := newEconomicFundingRange(bounds[0], bounds[1], bounds[2], true); err == nil {
			t.Fatal("invalid original funding bounds acquired authority", bounds)
		}
	}
}

// An absent opening source is not zero. A zero transfer is explicit evidence
// only about its own zero amount and cannot prove complete provider income.
func TestEconomicFundingMissingOpeningCreditRemainsUnknown(t *testing.T) {
	state := &economicConservationState{}
	resolver, err := newEconomicFundingResolver(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	payment := economicConservationPayment{Status: "opening-credit-unavailable", Event: monitorEconomicEvmEvent{Values: map[string]string{"amount": "7"}}}
	value, err := resolver.payment(payment)
	if err != nil {
		t.Fatal(err)
	}
	economicFundingTestRange(t, value, "7", "0", "7", "0", "7", false)
	payment.Event.Values["amount"] = "0"
	value, err = resolver.payment(payment)
	if err != nil {
		t.Fatal(err)
	}
	economicFundingTestRange(t, value, "0", "0", "0", "0", "0", true)
	state.Payments = []economicConservationPayment{payment}
	summary, err := state.fundingSummary(t.Context())
	if err != nil || summary.NoNonIncomeProviderCredit != nil || summary.CapitalSubsidyAuthorized {
		t.Fatal("zero payment fabricated complete provider funding", summary, err)
	}
}

// Repeated identities and canceled owners cannot select a new original source.
func TestEconomicFundingCanceledAndDuplicateCensusNeverBuildsAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	state := &economicConservationState{}
	if resolver, err := newEconomicFundingResolver(ctx, state); resolver != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled owner built original funding index", resolver, err)
	}
	for _, source := range []*economicConservationState{
		{Captures: []economicConservationCapture{{Id: "capture"}, {Id: "capture"}}},
		{Entitlements: []economicConservationEntitlement{{Id: "2/1"}, {Id: "2/1"}}},
		{Claims: []economicConservationClaim{{Id: "claim"}, {Id: "claim"}}},
	} {
		if resolver, err := newEconomicFundingResolver(t.Context(), source); resolver != nil || err == nil {
			t.Fatal("duplicate funding original selected a replacement", resolver, err)
		}
	}
	ctx, cancel = context.WithCancel(t.Context())
	resolver, err := newEconomicFundingResolver(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := resolver.payment(economicConservationPayment{}); !errors.Is(err, context.Canceled) {
		t.Fatal("retained funding resolver ignored original cancellation", err)
	}
}

// A foreign pool or cyclic carry cannot be explained by an equal amount.
func TestEconomicFundingCarryRetainsOriginalEpochAndPool(t *testing.T) {
	total := "20"
	state := &economicConservationState{Captures: []economicConservationCapture{{Id: "capture", Event: monitorEconomicEvmEvent{Values: map[string]string{"noId": "1", "epoch": "2", "amount": "20"}}}}, Entitlements: []economicConservationEntitlement{
		{Id: "2/1", PoolId: "1", Epoch: "2", Funded: "20", Sources: []economicConservationBacking{{Kind: "capture", Id: "capture", Amount: "20"}}},
		{Id: "3/1", PoolId: "1", Epoch: "3", Funded: "0", Total: &total, Sources: []economicConservationBacking{{Kind: "root-missed", Id: "2/1", Amount: "20"}}},
	}}
	resolver, err := newEconomicFundingResolver(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	value, err := resolver.entitlement("3/1")
	if err != nil || value.Epoch != "3" || value.Pool != "1" || value.Funding.Complete || value.Funding.Amount != "20" {
		t.Fatal("valid unknown carry lost its original obligation", value, err)
	}
	original := append([]economicConservationEntitlement(nil), state.Entitlements...)
	state.Entitlements[0].PoolId = "2"
	resolver, err = newEconomicFundingResolver(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.entitlement("3/1"); err == nil {
		t.Fatal("foreign original pool financed later entitlement")
	}
	state.Entitlements = original
	state.Entitlements[0].Sources = []economicConservationBacking{{Kind: "root-missed", Id: "3/1", Amount: "20"}}
	resolver, err = newEconomicFundingResolver(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.entitlement("3/1"); err == nil || !strings.Contains(err.Error(), "repeats an original obligation") {
		t.Fatal("cyclic original funding was not refused", err)
	}
}

// Scaled comparisons distinguish one unit outside the actual derived bound;
// neither decimal rounding nor an arbitrary relative tolerance is accepted.
func TestEconomicFundingNativeSplitUsesCompleteDenominatorAndExactTolerance(t *testing.T) {
	within, provider, owner, tolerance, err := economicNativeSplit("98", "9", "89", "2")
	if err != nil || !within || provider != "-8" || owner != "8" || tolerance != "2" {
		t.Fatal("complete native denominator lost exact ten/ninety comparison", within, provider, owner, tolerance, err)
	}
	within, provider, owner, tolerance, err = economicNativeSplit("100", "13", "87", "2")
	if err != nil || within || provider != "30" || owner != "-30" || tolerance != "2" {
		t.Fatal("out-of-bound original allocation acquired conformance", within, provider, owner, tolerance, err)
	}
	within, _, _, _, err = economicNativeSplit("100", "12", "88", "2")
	if err != nil || !within {
		t.Fatal("exact derived quantization boundary was changed", within, err)
	}
}

// A known violation remains false even while unrelated authority is unavailable.
func TestEconomicFundingDefiniteCapitalConsumptionIsNotUnknownConformance(t *testing.T) {
	value := false
	summary := economicConservationSummary{Funding: &economicConservationFundingSummary{Captured: zeroEconomicFunding(), Accepted: zeroEconomicFunding(), Paid: zeroEconomicFunding(), NoNonIncomeProviderCredit: &value}}
	if err := summary.assessConformance(); err != nil {
		t.Fatal(err)
	}
	if summary.TargetMet == nil || *summary.TargetMet || summary.Conformance.CompleteEvidence || len(summary.Conformance.Contradictions) != 1 || len(summary.MissingEvidence) == 0 || summary.ActivationReady {
		t.Fatal("observed unapproved capital consumption was hidden by missing authority", summary)
	}
	if !reflect.DeepEqual(summary.Conformance.Missing, summary.MissingEvidence) {
		t.Fatal("public missing source census changed during classification")
	}
}

// These are internal summary inputs, not public admission evidence. Actual
// producers have separate public tests and cannot accept these booleans from
// a request. This root verifies refresh of already-derived projections.
func TestEconomicFundingConformanceRefreshDropsStaleAcceptanceAndFeeProjection(t *testing.T) {
	zero, denominator, tolerance, stock := "0", "100", "0", "0"
	accepted := true
	income, err := newEconomicFundingRange("10", "10", "10", true)
	if err != nil {
		t.Fatal(err)
	}
	boundary := economicEmissionBoundary{Number: 101, Hash: "0x" + strings.Repeat("1", 64)}
	summary := economicConservationSummary{
		NativeCurrent: true, VaultCurrent: true, NativeCursor: boundary, ClaimStatuses: []string{"ok"},
		Execution:         &nativeExecutionWindow{Through: boundary, ProviderEntitlement: "10", OwnerRecycled: "90", ResidualEntitlement: "0", FixedPointTolerance: "0"},
		Yuma:              &economicConservationYumaSummary{Through: boundary, Current: true, MinerDenominator: &denominator, FullQuantizationTolerance: &tolerance},
		Funding:           &economicConservationFundingSummary{Captured: income, Accepted: income, Paid: income, OriginalClaims: 1, NoNonIncomeProviderCredit: &accepted},
		OpeningPrincipals: &economicConservationPrincipalSummary{}, OpeningPrincipalAlpha: &stock,
		PrincipalEffects:     &economicConservationPrincipalEffectSummary{Current: true},
		OriginalEntitlements: &economicConservationEntitlementSummary{CompleteObservedCensus: true, ProviderMeasurementsAuthenticated: true, IndependentFinalityAuthenticated: true},
		OriginalFees:         &economicWholeFeeSummary{Head: economicWholeFeeHead{Through: boundary}, Complete: true, WithdrawalRao: &zero, RefundRao: &zero},
	}
	if err := summary.assessConformance(); err != nil || summary.TargetMet == nil || !*summary.TargetMet || summary.NativeFeeWithdrawalRao == nil || summary.NativeFeeRefundRao == nil || summary.OriginalEntitlements.NativeIncomeFundingAlpha == nil || *summary.OriginalEntitlements.NativeIncomeFundingAlpha != "10" || summary.ActivationReady {
		t.Fatal("complete internal projection baseline failed", summary, err)
	}
	unknown, err := newEconomicFundingRange("10", "0", "10", false)
	if err != nil {
		t.Fatal(err)
	}
	summary.Funding.Captured, summary.Funding.Accepted, summary.Funding.Paid = unknown, unknown, unknown
	summary.Funding.NoNonIncomeProviderCredit = nil
	summary.OriginalEntitlements.ProviderMeasurementsAuthenticated = false
	summary.OriginalFees.Complete = false
	if err := summary.assessConformance(); err != nil || summary.TargetMet != nil || summary.NativeFeeWithdrawalRao != nil || summary.NativeFeeRefundRao != nil || summary.OriginalEntitlements.NativeIncomeFundingAlpha != nil || summary.OriginalEntitlements.CapitalFundingAlpha != nil || summary.Conformance.CompleteEvidence {
		t.Fatal("true-to-unknown retained stale acceptance, fee or income projection", summary, err)
	}
	contradiction := false
	summary.Funding.NoNonIncomeProviderCredit = &contradiction
	if err := summary.assessConformance(); err != nil || summary.TargetMet == nil || *summary.TargetMet {
		t.Fatal("observed contradiction refresh was lost", summary, err)
	}
	summary.Funding.NoNonIncomeProviderCredit = nil
	if err := summary.assessConformance(); err != nil || summary.TargetMet != nil || len(summary.Conformance.Contradictions) != 0 {
		t.Fatal("false-to-unknown retained an earlier contradiction", summary, err)
	}
}

// Unselected recipient identity is missing evidence. If the known original
// owner is within its exact bound, partial provider classification cannot fail.
func TestEconomicFundingUnclassifiedRecipientWithValidOwnerRemainsUnknown(t *testing.T) {
	denominator, tolerance := "100", "0"
	summary := economicConservationSummary{
		Execution: &nativeExecutionWindow{ProviderEntitlement: "5", OwnerRecycled: "90", ResidualEntitlement: "5"},
		Yuma:      &economicConservationYumaSummary{Current: true, MinerDenominator: &denominator, FullQuantizationTolerance: &tolerance},
		Funding:   &economicConservationFundingSummary{Captured: zeroEconomicFunding(), Accepted: zeroEconomicFunding(), Paid: zeroEconomicFunding()},
	}
	if err := summary.assessConformance(); err != nil {
		t.Fatal(err)
	}
	if summary.TargetMet != nil || summary.Conformance.NativeSplitWithinTolerance != nil || summary.Conformance.OwnerRecycleWithinTolerance == nil || !*summary.Conformance.OwnerRecycleWithinTolerance || len(summary.Conformance.Contradictions) != 0 || !strings.Contains(strings.Join(summary.MissingEvidence, ","), "complete-original-provider-recipient-membership") {
		t.Fatal("missing original recipient membership became a false policy contradiction", summary)
	}
}

// Full Q=2 already includes the final-stage3 diagnostic. Adding that stage
// again would hide this observed3-unit deviation on both sides of the split.
func TestEconomicFundingFullToleranceDoesNotDoubleCountFinalStage(t *testing.T) {
	denominator, tolerance := "100", "2"
	summary := economicConservationSummary{
		Execution: &nativeExecutionWindow{ProviderEntitlement: "13", OwnerRecycled: "87", ResidualEntitlement: "0", FixedPointTolerance: "3"},
		Yuma:      &economicConservationYumaSummary{Current: true, MinerDenominator: &denominator, FullQuantizationTolerance: &tolerance},
		Funding:   &economicConservationFundingSummary{Captured: zeroEconomicFunding(), Accepted: zeroEconomicFunding(), Paid: zeroEconomicFunding()},
	}
	if err := summary.assessConformance(); err != nil {
		t.Fatal(err)
	}
	if summary.TargetMet == nil || *summary.TargetMet || summary.Conformance.NativeSplitWithinTolerance == nil || *summary.Conformance.NativeSplitWithinTolerance || *summary.Conformance.NativeSplitTolerance != "2" || *summary.Conformance.ProviderDeviationNumerator != "30" || *summary.Conformance.OwnerDeviationNumerator != "-30" {
		t.Fatal("duplicated final quantization tolerance admitted original out-of-bound split", summary)
	}
}

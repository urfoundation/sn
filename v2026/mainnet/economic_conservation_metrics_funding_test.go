// These controls exercise the actual public metrics publisher and the existing
// exact conformance arithmetic. Synthetic internal summaries never authorize
// provider payments or supply missing producer evidence.
package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real completed public read has funding and an assessment even while its
// provider credit and full native denominator remain unresolved.
func TestEconomicMetricsPublicFundingPresenceDoesNotInventIncomeOnlyCredit(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	var output, diagnostic bytes.Buffer
	code := runMonitorStorageTestWithHooks(t, t.Context(), append(f.args(t), "--metrics-file", path), &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{})
	if code != 0 {
		t.Fatal("public economic read did not complete", code, diagnostic.String())
	}
	var summary economicConservationSummary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Funding == nil || summary.Conformance == nil || summary.Funding.NoNonIncomeProviderCredit != nil || summary.Conformance.NativeSplitWithinTolerance != nil || len(summary.Conformance.Missing) == 0 {
		t.Fatal("public fixture no longer exercises present but unresolved evidence", summary)
	}
	values := economicMetricsTestFile(t, path)
	if values["sample_timestamp_seconds"] != uint64(summary.SampleAt.Unix()) || values["funding_present"] != 1 || values["conformance_present"] != 1 || values["provider_credit_known"] != 0 || values["provider_credit_excludes_non_income"] != 0 || values["native_split_known"] != 0 || values["native_split_within_tolerance"] != 0 || values["conformance_missing_evidence"] == 0 || values["conformance_contradictions"] != 0 || values["target_known"] != 0 {
		t.Fatal("durable public metrics conflated missing evidence with a funding result", values)
	}
}

// Complete source accounting may retain fungible ambiguity. Each refresh must
// distinguish that unknown result from both observed acceptance and refusal.
func TestEconomicMetricsFundingRefreshPreservesUnknownAndDefiniteRefusal(t *testing.T) {
	funding, err := newEconomicFundingRange("10", "0", "10", true)
	if err != nil {
		t.Fatal(err)
	}
	summary := economicMetricsFundingTestSummary()
	summary.Funding = &economicConservationFundingSummary{Captured: funding, Accepted: funding, Paid: funding}
	for _, state := range []string{"ambiguous", "income-only", "non-income", "unknown-again", "absent"} {
		summary.Funding.NoNonIncomeProviderCredit = nil
		if state == "income-only" || state == "non-income" {
			result := state == "income-only"
			summary.Funding.NoNonIncomeProviderCredit = &result
		}
		if err := summary.assessConformance(); err != nil {
			t.Fatal(err)
		}
		if state == "absent" {
			summary.Funding, summary.Conformance, summary.TargetMet = nil, nil, nil
		}
		raw, err := renderEconomicConservationMetrics(&summary)
		if err != nil {
			t.Fatal(err)
		}
		values := readEconomicMetricsTest(t, raw)
		present, known, accepted, contradictions := uint64(1), uint64(0), uint64(0), uint64(0)
		if state == "income-only" || state == "non-income" {
			known = 1
		}
		if state == "income-only" {
			accepted = 1
		}
		if state == "non-income" {
			contradictions = 1
		}
		if state == "absent" {
			present = 0
		}
		if values["funding_present"] != present || values["funding_captured_complete"] != present || values["funding_accepted_complete"] != present || values["funding_paid_complete"] != present || values["provider_credit_known"] != known || values["provider_credit_excludes_non_income"] != accepted || values["conformance_present"] != present || values["conformance_contradictions"] != contradictions || values["target_met"] != 0 || values["target_known"] != contradictions {
			t.Fatal("funding refresh invented or retained a definite result", state, values)
		}
	}
}

// The native and owner conclusions can differ: an unclassified provider
// recipient keeps the full split unknown while the original owner is known.
func TestEconomicMetricsNativeAllocationKeepsIndependentKnownResults(t *testing.T) {
	for _, state := range []struct {
		name, provider, owner, residual string
		denominator                     bool
		nativeKnown, nativeMet          uint64
		ownerKnown, ownerMet            uint64
		contradictions                  uint64
	}{
		{name: "within", provider: "10", owner: "90", residual: "0", denominator: true, nativeKnown: 1, nativeMet: 1, ownerKnown: 1, ownerMet: 1},
		{name: "outside", provider: "13", owner: "87", residual: "0", denominator: true, nativeKnown: 1, ownerKnown: 1, contradictions: 1},
		{name: "unclassified", provider: "5", owner: "90", residual: "5", denominator: true, ownerKnown: 1, ownerMet: 1},
		{name: "missing-denominator", provider: "10", owner: "90", residual: "0"},
	} {
		summary := economicMetricsFundingTestSummary()
		denominator, tolerance := "100", "0"
		summary.Execution = &nativeExecutionWindow{ProviderEntitlement: state.provider, OwnerRecycled: state.owner, ResidualEntitlement: state.residual}
		if state.denominator {
			summary.Yuma = &economicConservationYumaSummary{Current: true, MinerDenominator: &denominator, FullQuantizationTolerance: &tolerance}
		}
		summary.Funding = &economicConservationFundingSummary{Captured: zeroEconomicFunding(), Accepted: zeroEconomicFunding(), Paid: zeroEconomicFunding()}
		if err := summary.assessConformance(); err != nil {
			t.Fatal(err)
		}
		raw, err := renderEconomicConservationMetrics(&summary)
		if err != nil {
			t.Fatal(err)
		}
		values := readEconomicMetricsTest(t, raw)
		if values["native_split_known"] != state.nativeKnown || values["native_split_within_tolerance"] != state.nativeMet || values["owner_recycle_known"] != state.ownerKnown || values["owner_recycle_within_tolerance"] != state.ownerMet || values["conformance_contradictions"] != state.contradictions || values["conformance_missing_evidence"] == 0 || values["complete_evidence"] != 0 || values["target_met"] != 0 {
			t.Fatal("original split metric lost its separate measured result", state.name, values)
		}
	}
}

// The startup marker exposes the full finite contract while asserting no
// observations. Consumers must use both presence and the retained timestamp.
func TestEconomicMetricsStartupFundingAndConformanceRemainUnknown(t *testing.T) {
	raw, err := renderEconomicConservationMetrics(nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range readEconomicMetricsTest(t, raw) {
		if value != 0 {
			t.Fatal("startup marker invented an economic observation", name, value)
		}
	}
}

// This shape is an internal projection fixture, not a public policy or witness.
func economicMetricsFundingTestSummary() economicConservationSummary {
	return economicConservationSummary{Schema: "urnetwork-economic-conservation-sample-v1", PolicyHash: "sha256:" + strings.Repeat("1", 64), CheckpointHash: "sha256:" + strings.Repeat("2", 64), SampleAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

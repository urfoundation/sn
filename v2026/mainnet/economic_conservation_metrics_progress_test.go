// Scalar controls distinguish admitted partial evidence, absent authority and
// a value that is exact in JSON but inexact in a Prometheus double. They exercise
// the production renderer, not an alternate source or economic verifier.
package main

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Independent known bits must not hide partial coverage behind full acceptance.
func TestEconomicMetricsProviderPartialCoverageRemainsVisible(t *testing.T) {
	summary := economicMetricsFundingTestSummary()
	summary.OriginalEntitlements = &economicConservationEntitlementSummary{
		FinalizedRoots: 3, CompleteRoots: 2, PendingRoots: 1, ProviderMeasurementRoots: 1, ProviderMeasurementProviders: 2,
		ProviderMeasurementBytes: "123", ProviderMeasurementAssignments: "7", ProviderMeasurementConfirmations: "0",
		ClosedWorkRoots: 2, ClosedWorkWindows: 1, ClockMatchedWindows: 1, CompleteReportInventories: 1,
		InventoryReports: 9, SignedCloseReports: 8, RegisteredCloseReports: 7, CloseAmountJoins: 6,
	}
	raw, err := renderEconomicConservationMetrics(&summary)
	if err != nil {
		t.Fatal(err)
	}
	values := readEconomicMetricsTest(t, raw)
	for name, want := range map[string]uint64{
		"entitlement_census_present": 1, "provider_coverage_known": 1, "entitlement_finalized_roots": 3,
		"entitlement_complete_roots": 2, "entitlement_pending_roots": 1, "provider_measurement_roots": 1,
		"provider_measurement_providers": 2, "provider_measurements_authenticated": 0,
		"provider_bytes_known": 1, "provider_bytes": 123, "provider_assignments_known": 1, "provider_assignments": 7,
		"provider_confirmations_known": 1, "provider_confirmations": 0, "closed_work_roots": 2,
		"closed_work_windows": 1, "clock_matched_windows": 1, "complete_report_inventories": 1,
		"inventory_reports": 9, "signed_close_reports": 8, "registered_close_reports": 7, "close_amount_joins": 6,
	} {
		if value, present := values[name]; !present || value != want {
			t.Fatal("partial original coverage became absent or fully authenticated", name, value, want)
		}
	}
	if summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.TargetMet != nil || values["target_known"] != 0 {
		t.Fatal("coverage projection created economic acceptance")
	}
}

// Empty census, missing measurement and authenticated known zero differ.
func TestEconomicMetricsProviderAbsenceAndKnownZeroStayDistinct(t *testing.T) {
	for _, row := range []struct {
		name    string
		census  *economicConservationEntitlementSummary
		present uint64
		known   uint64
	}{
		{name: "absent"},
		{name: "empty-census", census: &economicConservationEntitlementSummary{}, present: 1},
		{name: "missing-measurement", census: &economicConservationEntitlementSummary{FinalizedRoots: 1, PendingRoots: 1}, present: 1},
		{name: "admitted-zero", census: &economicConservationEntitlementSummary{FinalizedRoots: 1, CompleteRoots: 1, ProviderMeasurementRoots: 1, ProviderMeasurementBytes: "0", ProviderMeasurementAssignments: "0", ProviderMeasurementConfirmations: "0", CompleteObservedCensus: true, ProviderMeasurementsAuthenticated: true}, present: 1, known: 1},
	} {
		summary := economicMetricsFundingTestSummary()
		summary.OriginalEntitlements = row.census
		raw, err := renderEconomicConservationMetrics(&summary)
		if err != nil {
			t.Fatal(err)
		}
		values := readEconomicMetricsTest(t, raw)
		if values["entitlement_census_present"] != row.present || values["provider_coverage_known"] != row.present {
			t.Fatal("missing census acquired known coverage", row.name, values)
		}
		for _, name := range []string{"provider_bytes", "provider_assignments", "provider_confirmations"} {
			if values[name+"_known"] != row.known || values[name] != 0 {
				t.Fatal("absent original measurement became known zero", row.name, name, values)
			}
		}
	}
}

// Large counts stay byte-for-byte exact in the source summary. Each optional
// scalar withholds its own inexact projection without stopping other domains.
func TestEconomicMetricsProviderExactIntegerProjectionIsIndependent(t *testing.T) {
	for _, row := range []struct {
		raw   string
		known uint64
		want  uint64
	}{
		{raw: "0", known: 1},
		{raw: "123", known: 1, want: 123},
		{raw: "9007199254740991", known: 1, want: maximumEconomicMetricInteger - 1},
		{raw: "9007199254740992", known: 1, want: maximumEconomicMetricInteger},
		{raw: "9007199254740993"}, {raw: "18446744073709551615"}, {raw: "123456789012345678901234567890"},
		{raw: ""}, {raw: "01"}, {raw: "+1"}, {raw: "-1"}, {raw: " 1"}, {raw: "1.0"}, {raw: "1e2"},
	} {
		summary := economicMetricsFundingTestSummary()
		summary.OriginalEntitlements = &economicConservationEntitlementSummary{ProviderMeasurementRoots: 1, ProviderMeasurementBytes: row.raw, ProviderMeasurementAssignments: "9", ProviderMeasurementConfirmations: "8"}
		before, err := json.Marshal(summary)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := renderEconomicConservationMetrics(&summary)
		if err != nil {
			t.Fatal("optional scalar refused an original financial summary", row.raw, err)
		}
		after, err := json.Marshal(summary)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("metric projection changed exact retained input", row.raw, err)
		}
		values := readEconomicMetricsTest(t, raw)
		if values["provider_bytes_known"] != row.known || values["provider_bytes"] != row.want || values["provider_assignments_known"] != 1 || values["provider_assignments"] != 9 || values["provider_confirmations_known"] != 1 || values["provider_confirmations"] != 8 {
			t.Fatal("inexact value rounded or erased independent known counts", row.raw, values)
		}
	}
}

// Coverage integers also need exact scalar representation; the original
// summary remains present while that fixed group becomes explicitly unknown.
func TestEconomicMetricsCoverageOverflowDoesNotEraseOtherEvidence(t *testing.T) {
	summary := economicMetricsFundingTestSummary()
	summary.OriginalEntitlements = &economicConservationEntitlementSummary{FinalizedRoots: maximumEconomicMetricInteger + 1, ProviderMeasurementRoots: 1, ProviderMeasurementBytes: "5", ProviderMeasurementAssignments: "0", ProviderMeasurementConfirmations: "0"}
	raw, err := renderEconomicConservationMetrics(&summary)
	if err != nil {
		t.Fatal(err)
	}
	values := readEconomicMetricsTest(t, raw)
	if values["entitlement_census_present"] != 1 || values["provider_coverage_known"] != 0 || values["entitlement_finalized_roots"] != 0 || values["provider_measurement_roots"] != 0 || values["provider_bytes_known"] != 1 || values["provider_bytes"] != 5 || summary.OriginalEntitlements.FinalizedRoots != maximumEconomicMetricInteger+1 {
		t.Fatal("coverage overflow invented exactness or refused independent evidence", values)
	}
}

// The scalar contract exposes elapsed preparation separately from original
// observation delivery cadence, with explicit ETA and backlog presence.
func TestEconomicMetricsProgressKeepsIndependentRatesAndUnknownDomains(t *testing.T) {
	tracker, now := economicProgressTestTracker()
	target := economicProgressTestBoundary(200)
	tracker.observe(economicProgressTestBoundary(100), &target, now, now, true, false, false, "", true)
	target = economicProgressTestBoundary(210)
	progress := tracker.observe(economicProgressTestBoundary(120), &target, now.Add(20*time.Second), now.Add(30*time.Second), true, false, false, "", true)
	summary := economicMetricsFundingTestSummary()
	summary.Progress = &economicConservationProgressSummary{Native: progress}
	raw, err := renderEconomicConservationMetrics(&summary)
	if err != nil {
		t.Fatal(err)
	}
	values := readEconomicMetricsTest(t, raw)
	for name, want := range map[string]uint64{
		"native_backlog_known": 1, "native_backlog_blocks": 90, "native_catchup_eta_known": 1,
		"native_catchup_eta_seconds": 135, "native_rate_blocks": 20, "native_rate_window_seconds": 30,
		"native_cadence_known": 1, "native_cadence_milliseconds": 2000,
		"vault_backlog_known": 0, "vault_catchup_eta_known": 0, "vault_rate_blocks": 0, "vault_rate_window_seconds": 0, "vault_cadence_known": 0,
	} {
		if value, present := values[name]; !present || value != want {
			t.Fatal("independent measurement projection changed its basis", name, value, want)
		}
	}
}

// An exact JSON estimate can exceed the scalar domain without corrupting the
// command. Its independent backlog or cadence remains separately observable.
func TestEconomicMetricsProgressOverflowWithdrawsOnlyItsProjection(t *testing.T) {
	backlog, eta := maximumEconomicMetricInteger, maximumEconomicMetricInteger+1
	summary := economicMetricsFundingTestSummary()
	summary.Progress = &economicConservationProgressSummary{Native: economicConservationComponentProgress{BacklogBlocks: &backlog, CatchupEtaSeconds: &eta, Preparation: &economicConservationRate{Blocks: maximumEconomicMetricInteger + 1, ElapsedNanoseconds: int64(time.Second)}, FinalizedCadence: &economicConservationRate{Blocks: 1, ElapsedNanoseconds: int64(time.Second)}}}
	raw, err := renderEconomicConservationMetrics(&summary)
	if err != nil {
		t.Fatal(err)
	}
	values := readEconomicMetricsTest(t, raw)
	if values["native_backlog_known"] != 1 || values["native_backlog_blocks"] != backlog || values["native_catchup_eta_known"] != 0 || values["native_catchup_eta_seconds"] != 0 || values["native_rate_blocks"] != 0 || values["native_rate_window_seconds"] != 0 || values["native_cadence_known"] != 1 || values["native_cadence_milliseconds"] != 1000 || *summary.Progress.Native.CatchupEtaSeconds != eta {
		t.Fatal("estimate overflow rounded, erased evidence or changed original JSON", values)
	}
}

// Even every scalar's maximum printed width fits the existing durable profile.
func TestEconomicMetricsExtendedContractFitsOriginalOwnerBound(t *testing.T) {
	raw, err := renderEconomicConservationMetrics(nil)
	if err != nil {
		t.Fatal(err)
	}
	values := readEconomicMetricsTest(t, raw)
	for name, value := range values {
		if value != 0 {
			t.Fatal("startup extension invented an observation", name, value)
		}
	}
	var largest strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if !strings.HasPrefix(line, "#") {
			fields := strings.Fields(line)
			line = fields[0] + " " + strconv.FormatUint(math.MaxUint64, 10)
		}
		largest.WriteString(line + "\n")
	}
	if largest.Len() > 16*1024 || len(values) != 72 {
		t.Fatal("fixed progress contract requires new unapproved metrics custody", largest.Len(), len(values))
	}
}

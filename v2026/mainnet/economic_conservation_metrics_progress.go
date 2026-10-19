// New diagnostic gauges project only admitted summary facts. The fixed scalar
// surface has no identities or labels; exact JSON remains authoritative when a
// count cannot be represented by a Prometheus double without rounding.
package main

import (
	"strconv"
	"time"
)

const maximumEconomicMetricInteger uint64 = 9007199254740992

// Finite unlabelled gauges are emitted with TYPE declarations. Their source,
// presence and estimation semantics are documented with the command contract.
type economicConservationProgressMetric struct {
	name  string
	value uint64
}

// Exact canonical decimal admission is separate from signed source admission.
// A missing, oversized or inexact optional projection never stops its owner.
func economicConservationMetricInteger(raw string) (uint64, bool) {
	if raw == "" || len(raw) > 16 || len(raw) > 1 && raw[0] == '0' {
		return 0, false
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value > maximumEconomicMetricInteger || strconv.FormatUint(value, 10) != raw {
		return 0, false
	}
	return value, true
}

// Coverage describes retained admitted roots and inventories, including partial
// coverage. Complete provider authentication is the existing independent bit;
// sums of assignments and confirmations are not per-provider reliability scores.
func economicConservationProgressMetrics(summary economicConservationSummary) []economicConservationProgressMetric {
	bit := func(value bool) uint64 {
		if value {
			return 1
		}
		return 0
	}
	value := economicConservationEntitlementSummary{}
	if summary.OriginalEntitlements != nil {
		value = *summary.OriginalEntitlements
	}
	coverage := []economicConservationProgressMetric{
		{name: "entitlement_finalized_roots", value: value.FinalizedRoots},
		{name: "entitlement_complete_roots", value: value.CompleteRoots},
		{name: "provider_measurement_roots", value: value.ProviderMeasurementRoots},
		{name: "provider_measurement_providers", value: value.ProviderMeasurementProviders},
		{name: "closed_work_roots", value: value.ClosedWorkRoots},
		{name: "closed_work_windows", value: value.ClosedWorkWindows},
		{name: "clock_matched_windows", value: value.ClockMatchedWindows},
		{name: "complete_report_inventories", value: value.CompleteReportInventories},
		{name: "inventory_reports", value: value.InventoryReports},
		{name: "signed_close_reports", value: value.SignedCloseReports},
		{name: "registered_close_reports", value: value.RegisteredCloseReports},
		{name: "close_amount_joins", value: value.CloseAmountJoins},
	}
	coverageKnown := summary.OriginalEntitlements != nil
	for _, metric := range coverage {
		coverageKnown = coverageKnown && metric.value <= maximumEconomicMetricInteger
	}
	if !coverageKnown {
		for index := range coverage {
			coverage[index].value = 0
		}
	}
	metrics := []economicConservationProgressMetric{
		{name: "entitlement_census_present", value: bit(summary.OriginalEntitlements != nil)},
		{name: "provider_coverage_known", value: bit(coverageKnown)},
	}
	metrics = append(metrics, coverage...)
	for _, item := range []struct{ name, raw string }{
		{name: "provider_bytes", raw: value.ProviderMeasurementBytes},
		{name: "provider_assignments", raw: value.ProviderMeasurementAssignments},
		{name: "provider_confirmations", raw: value.ProviderMeasurementConfirmations},
	} {
		count, known := economicConservationMetricInteger(item.raw)
		known = known && value.ProviderMeasurementRoots > 0
		if !known {
			count = 0
		}
		metrics = append(metrics, economicConservationProgressMetric{name: item.name + "_known", value: bit(known)}, economicConservationProgressMetric{name: item.name, value: count})
	}
	progress := economicConservationProgressSummary{}
	if summary.Progress != nil {
		progress = *summary.Progress
	}
	for _, component := range []struct {
		name  string
		value economicConservationComponentProgress
	}{
		{name: "native", value: progress.Native},
		{name: "vault", value: progress.Vault},
	} {
		value := component.value
		var backlog, eta, rateBlocks, rateSeconds, cadenceMilliseconds uint64
		backlogKnown := value.BacklogBlocks != nil && *value.BacklogBlocks <= maximumEconomicMetricInteger
		if backlogKnown {
			backlog = *value.BacklogBlocks
		}
		etaKnown := value.CatchupEtaSeconds != nil && *value.CatchupEtaSeconds <= maximumEconomicMetricInteger
		if etaKnown {
			eta = *value.CatchupEtaSeconds
		}
		if value.Preparation != nil && value.Preparation.Blocks > 0 && value.Preparation.Blocks <= maximumEconomicMetricInteger && value.Preparation.ElapsedNanoseconds > 0 {
			rateBlocks = value.Preparation.Blocks
			rateSeconds = uint64((value.Preparation.ElapsedNanoseconds-1)/int64(time.Second)) + 1
		}
		cadenceKnown := false
		if value.FinalizedCadence != nil {
			period := economicConservationRoundedDuration(1, value.FinalizedCadence.ElapsedNanoseconds, value.FinalizedCadence.Blocks, int64(time.Millisecond))
			if period != nil && *period <= maximumEconomicMetricInteger {
				cadenceMilliseconds, cadenceKnown = *period, true
			}
		}
		for _, metric := range []economicConservationProgressMetric{
			{name: "backlog_known", value: bit(backlogKnown)},
			{name: "backlog_blocks", value: backlog},
			{name: "catchup_eta_known", value: bit(etaKnown)},
			{name: "catchup_eta_seconds", value: eta},
			{name: "rate_blocks", value: rateBlocks},
			{name: "rate_window_seconds", value: rateSeconds},
			{name: "cadence_known", value: bit(cadenceKnown)},
			{name: "cadence_milliseconds", value: cadenceMilliseconds},
		} {
			metric.name = component.name + "_" + metric.name
			metrics = append(metrics, metric)
		}
	}
	return metrics
}

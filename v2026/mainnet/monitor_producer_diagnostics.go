// Producer diagnostic delivery is a separate operational domain. A missing
// optional extension stays unknown; retained counts never become current just
// because the monitor successfully re-read an old heartbeat.
package main

import (
	"fmt"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Only these five wire fields can select series; no producer-supplied strings
// become labels. Each expected validator role keeps its independent namespace.
func appendMonitorProducerDiagnostics(raw []byte, role string, state *monitorValidatorState) []byte {
	var output strings.Builder
	output.Write(raw)
	var states [5]protocol.ValidatorDiagnosticState
	known, current := 0, 0
	if state.Record != nil && state.Record.Diagnostics != nil {
		known = 1
		states = state.Record.Diagnostics.States()
		if state.sourceCurrent(state.SampleAt) && monitorServiceFresh(state.SampleAt, monitorProgressTime(state.Record.Diagnostics.ObservedAt)) {
			current = 1
		}
	}
	for index, domain := range []string{"startup", "steering", "progress", "operator", "runtime"} {
		value := states[index]
		status := map[string]int{"starting": 0, "delivered": 1, "retrying": 2, "unavailable": 3}[value.Outcome]
		var lastSuccess int64
		if value.LastSuccessAt != "" {
			lastSuccess = monitorProgressTime(value.LastSuccessAt).Unix()
		}
		for _, metric := range []struct {
			name  string
			value any
		}{
			{name: "known", value: known}, {name: "current", value: current}, {name: "status", value: status}, {name: "last_success_timestamp_seconds", value: lastSuccess},
			{name: "delivered_total", value: value.Delivered}, {name: "dropped_total", value: value.Dropped}, {name: "dropped_bytes_total", value: value.DroppedBytes}, {name: "unavailable_total", value: value.Unavailable},
		} {
			name := "sn_mainnet_validator_producer_diagnostic_" + metric.name
			kind := "gauge"
			if strings.HasSuffix(metric.name, "_total") {
				kind = "counter"
			}
			if index == 0 {
				fmt.Fprintf(&output, "# TYPE %s %s\n", name, kind)
			}
			fmt.Fprintf(&output, "%s{role=%q,domain=%q} %v\n", name, role, domain, metric.value)
		}
	}
	return []byte(output.String())
}

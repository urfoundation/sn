// Long-lived monitor output is independently bounded. Its delivery counters
// describe diagnostic records only and never refresh chain or service progress.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/diagnostics"
)

// The output owner closes after every sampling worker, including early errors.
// Aliased stdout/stderr share one exporter so partial records cannot interleave.
type monitorOutput struct {
	events *diagnostics.Exporter
	errors *diagnostics.Exporter
}

// The source census is finite and supplied independently of observed records.
func newMonitorOutput(ctx context.Context, stdout, stderr io.Writer, roles int, now func() time.Time) (*monitorOutput, error) {
	names := []string{"chain", "diagnostic"}
	for index := 0; index < roles; index++ {
		names = append(names, fmt.Sprintf("validator%d", index))
	}
	output, err := diagnostics.NewWithClock(ctx, stdout, names, now)
	if err != nil {
		return nil, err
	}
	self := &monitorOutput{events: output}
	if sameMonitorSink(stdout, stderr) {
		self.errors = output
		return self, nil
	}
	self.errors, err = diagnostics.NewWithClock(ctx, stderr, []string{"diagnostic"}, now)
	if err != nil {
		return nil, errors.Join(err, output.Close())
	}
	return self, nil
}

// This is alias inspection only; no unbounded Writer callback is invoked.
func sameMonitorSink(first, second io.Writer) bool {
	if reflect.TypeOf(first) == reflect.TypeOf(second) && reflect.TypeOf(first) != nil && reflect.TypeOf(first).Comparable() && first == second {
		return true
	}
	one, oneOk := first.(*os.File)
	two, twoOk := second.(*os.File)
	if !oneOk || !twoOk || one == nil || two == nil {
		return false
	}
	oneInfo, oneErr := one.Stat()
	twoInfo, twoErr := two.Stat()
	return oneErr == nil && twoErr == nil && os.SameFile(oneInfo, twoInfo)
}

// Descriptor cleanup errors survive the parent result; no writer is abandoned.
func (self *monitorOutput) close() error {
	if self == nil {
		return nil
	}
	err := self.events.Close()
	if self.errors != self.events {
		err = errors.Join(err, self.errors.Close())
	}
	return err
}

// Values are copied before publication. Unknown legacy test/direct callers
// remain unknown rather than being described as delivered output.
type monitorDiagnosticObservation struct {
	Events *diagnostics.Snapshot `json:"events,omitempty"`
	Errors *diagnostics.Snapshot `json:"errors,omitempty"`
}

// Snapshot is an immutable scalar copy of the selected domain's real exporter.
func monitorDiagnosticSnapshot(events, diagnostic io.Writer) *monitorDiagnosticObservation {
	value := &monitorDiagnosticObservation{}
	if source, ok := events.(interface{ DiagnosticSnapshot() diagnostics.Snapshot }); ok {
		state := source.DiagnosticSnapshot()
		value.Events = &state
	}
	if source, ok := diagnostic.(interface{ DiagnosticSnapshot() diagnostics.Snapshot }); ok {
		state := source.DiagnosticSnapshot()
		value.Errors = &state
	}
	return value
}

// Fixed stream labels supplement the existing independent role label. Counts
// reset on process restart and saturate; no input path or error becomes a label.
func appendMonitorOutputMetrics(raw []byte, prefix, role string, value *monitorDiagnosticObservation) []byte {
	var output strings.Builder
	output.Write(raw)
	for _, stream := range []string{"events", "diagnostics"} {
		var state *diagnostics.Snapshot
		if value != nil {
			if stream == "events" {
				state = value.Events
			} else {
				state = value.Errors
			}
		}
		known, status := 0, 0
		var delivered, dropped, droppedBytes, unavailable uint64
		var lastSuccess int64
		if state != nil {
			known = 1
			status = map[string]int{"starting": 0, "delivered": 1, "retrying": 2, "unavailable": 3}[state.Outcome]
			delivered, dropped, droppedBytes, unavailable = state.Delivered, state.Dropped, state.DroppedBytes, state.Unavailable
			if parsed, err := time.Parse(time.RFC3339Nano, state.LastSuccessAt); err == nil {
				lastSuccess = parsed.Unix()
			}
		}
		label := fmt.Sprintf("stream=%q", stream)
		if role != "" {
			label = fmt.Sprintf("role=%q,", role) + label
		}
		for _, metric := range []struct {
			name  string
			value any
		}{
			{name: "known", value: known}, {name: "status", value: status}, {name: "last_success_timestamp_seconds", value: lastSuccess},
			{name: "delivered_total", value: delivered}, {name: "dropped_total", value: dropped}, {name: "dropped_bytes_total", value: droppedBytes}, {name: "unavailable_total", value: unavailable},
		} {
			kind := "gauge"
			if strings.HasSuffix(metric.name, "_total") {
				kind = "counter"
			}
			name := prefix + "_output_" + metric.name
			// Family metadata occurs once, followed by its two stream samples.
			if stream == "events" {
				fmt.Fprintf(&output, "# TYPE %s %s\n", name, kind)
			}
			fmt.Fprintf(&output, "%s{%s} %v\n", name, label, metric.value)
		}
	}
	return []byte(output.String())
}

// Standalone and composed commands use the same strict long-lived admission.
// Finite inspect/plan/bootstrap stdout and stderr are deliberately unchanged.
func runMonitorOnly(ctx context.Context, client *rpcClient, expected identityExpectation, checkpointPath, metricsPath string, interval, stallAfter time.Duration, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) (result int) {
	output, err := newMonitorOutput(ctx, stdout, stderr, 0, now)
	if err != nil {
		return 3
	}
	defer func() {
		if output.close() != nil {
			result = 3
		}
	}()
	return runChainMonitor(ctx, client, expected, checkpointPath, metricsPath, interval, stallAfter, output.events.Writer("chain"), output.errors.Writer("diagnostic"), now, hooks)
}

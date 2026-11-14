// Read continuity uses only the existing role label; incident identities and
// canonical record digests remain in checkpoints and bounded structured events.
package main

import (
	"fmt"
	"strings"
)

// Persistent counts are gauges because restoring a checkpoint may move them
// backward. Known and prior-history flags prevent zero from inventing coverage.
func appendMonitorReadIncidentMetrics(raw []byte, role string, history *monitorReadIncidentHistory) []byte {
	var known, priorUnknown, open, latestStatus int
	var incidents, failedReads, latestFailedReads uint64
	var firstAt, lastAt, recoveredAt int64
	if history != nil {
		known = 1
		if history.PriorHistoryUnknown {
			priorUnknown = 1
		}
		incidents, failedReads = history.Incidents, history.FailedReads
		if first := history.FirstIncident; first != nil {
			firstAt = first.FirstFailure.ObservedAt.Unix()
		}
		if last := history.LastIncident; last != nil {
			lastAt, latestFailedReads = last.LastFailure.ObservedAt.Unix(), last.FailedReads
			latestStatus = monitorServiceStatusCodes[last.LastFailure.Status]
			if last.Recovery == nil {
				open = 1
			}
		}
		if history.LastRecovery != nil {
			recoveredAt = history.LastRecovery.ObservedAt.Unix()
		}
	}
	var output strings.Builder
	output.Write(raw)
	for _, metric := range []struct {
		name  string
		value any
	}{
		{name: "history_known", value: known}, {name: "prior_history_unknown", value: priorUnknown},
		{name: "count", value: incidents}, {name: "failed_reads", value: failedReads},
		{name: "open", value: open}, {name: "first_failure_timestamp_seconds", value: firstAt},
		{name: "last_failure_timestamp_seconds", value: lastAt}, {name: "last_recovery_timestamp_seconds", value: recoveredAt},
		{name: "latest_failed_reads", value: latestFailedReads}, {name: "latest_failure_status", value: latestStatus},
	} {
		name := "sn_mainnet_validator_read_incident_" + metric.name
		fmt.Fprintf(&output, "# TYPE %s gauge\n%s{role=%q} %v\n", name, name, role, metric.value)
	}
	return []byte(output.String())
}

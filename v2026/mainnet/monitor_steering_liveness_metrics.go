// Bounded liveness gauges retain their incident independently of source reads.
// The existing expected-host roster detects a stopped monitor or missing host.
package main

import (
	"fmt"
	"strings"
	"time"
)

// Fixed names and the independently configured role are the complete census.
func appendMonitorSteeringLivenessMetrics(raw []byte, policy monitorValidatorPolicy, state *monitorValidatorState) []byte {
	stamp := func(value time.Time) int64 {
		if value.IsZero() {
			return 0
		}
		return value.Unix()
	}
	flag := func(value bool) int {
		if value {
			return 1
		}
		return 0
	}
	observation := state.steeringLiveness(policy)
	var warning, critical uint32
	if policy.SteeringLiveness != nil {
		warning, critical = policy.SteeringLiveness.WarningAfterSeconds, policy.SteeringLiveness.CriticalAfterSeconds
	}
	var incidents uint64
	var baseline, first, last, recovery int64
	if history := state.SteeringLiveness; history != nil {
		incidents, first = history.Incidents, stamp(history.FirstDetectedAt)
		if history.Baseline != nil {
			baseline = stamp(history.Baseline.ObservedAt)
		}
		if history.LastIncident != nil {
			last = stamp(history.LastIncident.Failure.ObservedAt)
			if history.LastIncident.Recovery != nil {
				recovery = stamp(history.LastIncident.Recovery.ObservedAt)
			}
		}
	}
	var output strings.Builder
	for _, metric := range []struct {
		name  string
		value any
	}{
		{name: "enabled", value: flag(policy.SteeringLiveness != nil)},
		{name: "current", value: flag(observation.Current)},
		{name: "status", value: monitorSteeringLivenessCodes[observation.Status]},
		{name: "outcome_timestamp_seconds", value: stamp(observation.OutcomeAt)},
		{name: "warning_after_seconds", value: warning},
		{name: "critical_after_seconds", value: critical},
		{name: "baseline_timestamp_seconds", value: baseline},
		{name: "unresolved", value: flag(state.SteeringLiveness.unresolved())},
		{name: "incidents", value: incidents},
		{name: "first_detected_timestamp_seconds", value: first},
		{name: "last_detected_timestamp_seconds", value: last},
		{name: "recovery_timestamp_seconds", value: recovery},
	} {
		fmt.Fprintf(&output, "# TYPE sn_mainnet_validator_steering_liveness_%s gauge\nsn_mainnet_validator_steering_liveness_%s{role=%q} %v\n", metric.name, metric.name, policy.Role, metric.value)
	}
	return append(raw, output.String()...)
}

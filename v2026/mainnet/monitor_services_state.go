// Consumer continuity preserves successful reads and original protocol ages.
// These observations never attest to on-chain acceptance or authorize repair.
package main

import (
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Initial thresholds are provisional operational choices, separate from any
// native epoch/reveal deadline. Rule examples require deployment qualification.
const monitorServiceWarningAfter = 90 * time.Second
const monitorServiceCriticalAfter = 120 * time.Second
const monitorServiceOutageCriticalAfter = 5 * time.Minute
const monitorServiceClockAllowance = 30 * time.Second

// A restart never restores readCurrent. The last accepted record remains
// usable retained evidence while missing, invalid or future input is refused.
type monitorValidatorState struct {
	SampleAt                 time.Time                     `json:"sample_at"`
	HighWaterAt              time.Time                     `json:"high_water_at"`
	LastReadSuccessAt        time.Time                     `json:"last_read_success_at"`
	OutageSince              time.Time                     `json:"outage_since"`
	ClockFaultAt             time.Time                     `json:"clock_fault_at"`
	CandidateHeartbeatAt     time.Time                     `json:"candidate_heartbeat_at"`
	ReadStatus               string                        `json:"read_status"`
	Record                   *protocol.ValidatorProgress   `json:"record,omitempty"`
	PublicationLastSuccessAt time.Time                     `json:"publication_last_success_at"`
	NativeDeadline           *monitorNativeDeadlineHistory `json:"native_deadline,omitempty"`
	ReadIncidents            *monitorReadIncidentHistory   `json:"read_incidents,omitempty"`
	readCurrent              bool
}

// Only fixed classifications can become metrics or retained checkpoint state.
var monitorServiceStatusCodes = map[string]int{
	"starting": 0, "observed": 1, "missing": 2, "unavailable": 3,
	"invalid": 4, "identity": 5, "clock": 6, "stale": 7,
	"publisher": 8, "unknown": 9, "intent-failed": 10, "changed": 11,
	"native-deadline": 12, "native-window-missed": 13,
}

// Times are parsed only after the strict producer decoder validated the wire.
func monitorProgressTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

// Every producer clock participates, including retained success and intent age.
// A fresh heartbeat cannot hide a future timestamp in another domain.
func monitorProgressMaximumTime(value *protocol.ValidatorProgress) time.Time {
	times := []string{value.StartedAt, value.HeartbeatAt, value.Publisher.LastSuccessAt}
	if value.Intent != nil {
		times = append(times, value.Intent.ObservedAt, value.Intent.LastSuccessAt)
		if value.Intent.Value != nil {
			times = append(times, value.Intent.Value.CreatedAt, value.Intent.Value.ProgressAt)
		}
	}
	if value.Native != nil {
		times = append(times, value.Native.ObservedAt, value.Native.LastSuccessAt)
	}
	if value.Settlement != nil {
		times = append(times, value.Settlement.ObservedAt, value.Settlement.LastSuccessAt, value.Settlement.ProgressAt)
	}
	if value.Steering != nil {
		times = append(times, value.Steering.ObservedAt, value.Steering.LastSuccessAt)
	}
	if value.Diagnostics != nil {
		times = append(times, value.Diagnostics.ObservedAt)
		for _, state := range value.Diagnostics.States() {
			times = append(times, state.LastSuccessAt)
		}
	}
	var maximum time.Time
	for _, value := range times {
		if parsed := monitorProgressTime(value); parsed.After(maximum) {
			maximum = parsed
		}
	}
	return maximum
}

// Consumer time is monotonic evidence across restart. A same-vector source
// cannot reset pending age or silently reinterpret the original config.
func (self *monitorValidatorState) observe(startedAt, now time.Time, value *protocol.ValidatorProgress, code string) {
	self.SampleAt, self.readCurrent = now, false
	if self.HighWaterAt.After(now.Add(monitorServiceClockAllowance)) {
		code, self.ClockFaultAt = "clock", self.HighWaterAt
	}
	if now.After(self.HighWaterAt) {
		self.HighWaterAt = now
	}
	if value != nil {
		self.CandidateHeartbeatAt = monitorProgressTime(value.HeartbeatAt)
		if maximum := monitorProgressMaximumTime(value); maximum.After(now.Add(monitorServiceClockAllowance)) {
			code, self.ClockFaultAt = "clock", maximum
		}
		if prior := self.Record; prior != nil {
			if prior.InstanceId == value.InstanceId && prior.Diagnostics != nil && value.Diagnostics != nil {
				if before := monitorProgressTime(prior.Diagnostics.ObservedAt); before.After(monitorProgressTime(value.Diagnostics.ObservedAt).Add(monitorServiceClockAllowance)) {
					code, self.ClockFaultAt = "clock", before
				}
				previous, next := prior.Diagnostics.States(), value.Diagnostics.States()
				for index, old := range previous {
					current := next[index]
					if current.Delivered < old.Delivered || current.Dropped < old.Dropped || current.DroppedBytes < old.DroppedBytes || current.Unavailable < old.Unavailable {
						code = "invalid"
					}
					if before := monitorProgressTime(old.LastSuccessAt); before.After(monitorProgressTime(current.LastSuccessAt).Add(monitorServiceClockAllowance)) {
						code, self.ClockFaultAt = "clock", before
					}
				}
			}
			if prior.InstanceId == value.InstanceId && monitorProgressTime(prior.HeartbeatAt).After(self.CandidateHeartbeatAt.Add(monitorServiceClockAllowance)) {
				code, self.ClockFaultAt = "clock", monitorProgressTime(prior.HeartbeatAt)
			}
			if prior.Intent != nil && prior.Intent.Value != nil && value.Intent != nil && value.Intent.Value != nil {
				old, next := prior.Intent.Value, value.Intent.Value
				if old.VectorHash == next.VectorHash && (old.CreatedAt != next.CreatedAt || old.ConfigHash != next.ConfigHash) {
					code = "invalid"
				}
				if old.VectorHash == next.VectorHash && old.Status == next.Status && old.FinalizedBlock == next.FinalizedBlock && old.ApplicationBlock == next.ApplicationBlock && old.ProgressAt != next.ProgressAt {
					code = "invalid"
				}
			}
		}
	}
	if code == "ok" && value != nil {
		self.Record, self.LastReadSuccessAt, self.OutageSince, self.readCurrent = value, now, time.Time{}, true
		self.ClockFaultAt = time.Time{}
	} else if self.OutageSince.IsZero() {
		// Startup bounds an outage only when no success has ever been seen.
		// A new failure after a healthy period starts at this observation;
		// retained outages keep their original boundary across restart.
		self.OutageSince = now
		if self.LastReadSuccessAt.IsZero() {
			self.OutageSince = startedAt
		}
	}
	self.ReadStatus = code
}

// Zero or future times are unavailable, never age-zero healthy observations.
func monitorServiceFresh(now, value time.Time) bool {
	return !value.IsZero() && !value.After(now.Add(monitorServiceClockAllowance)) && now.Sub(value) < monitorServiceWarningAfter
}

// Current fields require a fresh successful read and a fresh process record.
// An available retained record alone cannot refresh any protocol observation.
func (self *monitorValidatorState) sourceCurrent(now time.Time) bool {
	return self.readCurrent && self.ReadStatus == "ok" && self.Record != nil && monitorServiceFresh(now, monitorProgressTime(self.Record.HeartbeatAt))
}

// A complete current observation is distinct from protocol progress. Native
// schedule absence stays unknown; no elapsed wall time invents a chain deadline.
func (self *monitorValidatorState) condition(now time.Time, policy monitorValidatorPolicy) (string, string) {
	status, severity := self.observationCondition(now)
	if severity == "critical" {
		return status, severity
	}
	if self.NativeDeadline != nil {
		return "native-window-missed", "critical"
	}
	deadline := self.nativeDeadline(policy, now)
	if deadline.Status == "critical" || deadline.Status == "missed-window" {
		return "native-deadline", "critical"
	}
	if deadline.Status == "warning" {
		return "native-deadline", "warning"
	}
	return status, severity
}

// Read availability and producer freshness remain separate from retained
// deadline incidents. An otherwise healthy sample cannot clear an old miss.
func (self *monitorValidatorState) observationCondition(now time.Time) (string, string) {
	if self.SampleAt.IsZero() {
		return "starting", ""
	}
	if !self.ClockFaultAt.IsZero() {
		return "clock", "critical"
	}
	switch self.ReadStatus {
	case "clock", "identity", "invalid":
		return self.ReadStatus, "critical"
	case "missing", "unavailable", "changed":
		severity := ""
		if now.Before(self.OutageSince) || now.Sub(self.OutageSince) >= monitorServiceOutageCriticalAfter {
			severity = "critical"
		} else if now.Sub(self.OutageSince) >= monitorServiceCriticalAfter {
			severity = "warning"
		}
		return self.ReadStatus, severity
	}
	if self.Record == nil || !self.readCurrent {
		return "unknown", ""
	}
	value := self.Record
	heartbeat := monitorProgressTime(value.HeartbeatAt)
	if !monitorServiceFresh(now, heartbeat) {
		if now.Sub(heartbeat) >= monitorServiceCriticalAfter {
			return "stale", "critical"
		}
		return "stale", "warning"
	}
	published := monitorProgressTime(value.Publisher.LastSuccessAt)
	if value.Publisher.Outcome != "published" || !monitorServiceFresh(now, published) {
		if published.IsZero() {
			published = monitorProgressTime(value.StartedAt)
		}
		if now.Sub(published) >= monitorServiceOutageCriticalAfter {
			return "publisher", "critical"
		}
		if value.Publisher.Outcome == "retrying" || now.Sub(published) >= monitorServiceCriticalAfter {
			return "publisher", "warning"
		}
		return "publisher", ""
	}
	intentCurrent := value.Intent != nil && value.Intent.Current && monitorServiceFresh(now, monitorProgressTime(value.Intent.ObservedAt)) && monitorServiceFresh(now, monitorProgressTime(value.Intent.LastSuccessAt))
	if intentCurrent && value.Intent.Value != nil && value.Intent.Value.Status == "failed" {
		return "intent-failed", "critical"
	}
	if !intentCurrent || value.Settlement == nil || !value.Settlement.Current || !value.Settlement.CursorKnown ||
		!monitorServiceFresh(now, monitorProgressTime(value.Settlement.ObservedAt)) || !monitorServiceFresh(now, monitorProgressTime(value.Settlement.LastSuccessAt)) ||
		value.Native == nil || !value.Native.Current || !monitorServiceFresh(now, monitorProgressTime(value.Native.ObservedAt)) || !monitorServiceFresh(now, monitorProgressTime(value.Native.LastSuccessAt)) {
		return "unknown", ""
	}
	return "observed", ""
}

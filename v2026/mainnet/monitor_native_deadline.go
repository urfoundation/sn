// Native submission forecasts and missed-window reports consume only the
// existing producer wire. They neither expire signatures nor attest to success.
package main

import (
	"errors"
	"math"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Operators supply a measured completion/finality margin in native blocks.
// No default margin or process heartbeat silently enables deadline inference.
type monitorNativeDeadlinePolicy struct {
	CompletionMarginBlocks uint64 `json:"completion_margin_blocks"`
	EpochScheduleProfile   string `json:"epoch_schedule_profile,omitempty"`
}

// Known describes a submission forecast or a reported epoch crossing, never
// chain acceptance. A projected block can move when the native schedule changes.
type monitorNativeDeadlineObservation struct {
	Status                 string `json:"status"`
	Known                  bool   `json:"known"`
	Current                bool   `json:"current"`
	IntentEpoch            uint64 `json:"intent_epoch"`
	ObservedEpoch          uint64 `json:"observed_epoch"`
	ObservedBlock          uint64 `json:"observed_block"`
	ProjectedBoundaryBlock uint64 `json:"projected_boundary_block"`
	BlocksRemaining        uint64 `json:"blocks_remaining"`
	WarningBlocks          uint64 `json:"warning_blocks"`
	CriticalBlocks         uint64 `json:"critical_blocks"`
}

// A missed submission window remains an unresolved operational incident.
// The complete fixed-size source evidence is retained for independent review;
// even a later unsigned applied report cannot resolve this historical finding.
type monitorNativeDeadlineIncident struct {
	DetectedAt time.Time                             `json:"detected_at"`
	Intent     protocol.ValidatorIntentObservation   `json:"intent"`
	Native     protocol.ValidatorNativeObservation   `json:"native"`
	Steering   protocol.ValidatorSteeringObservation `json:"steering"`
}

// Fixed first/last records and a saturating count bound storage independently
// of runtime length. Middle occurrences remain in the ordinary event stream.
type monitorNativeDeadlineHistory struct {
	MissedWindows uint64                        `json:"missed_windows"`
	FirstMiss     monitorNativeDeadlineIncident `json:"first_miss"`
	LastMiss      monitorNativeDeadlineIncident `json:"last_miss"`
}

// A closed vocabulary keeps unavailable reads separate from observed misses.
var monitorNativeDeadlineStatusCodes = map[string]int{
	"disabled": 0, "unknown": 1, "unavailable": 2, "incoherent": 3,
	"not-pending": 4, "pending": 5, "warning": 6, "critical": 7, "missed-window": 8,
}

// Bounds also make doubling the completion margin safe on every platform.
func (self *monitorNativeDeadlinePolicy) validate() error {
	if self != nil {
		if err := crv4.ValidateEpochScheduleProfile(self.EpochScheduleProfile); err != nil {
			return err
		}
	}
	if self != nil && (self.CompletionMarginBlocks == 0 || self.CompletionMarginBlocks > crv4.MaxTempo) {
		return errors.New("native deadline completion margin must be between one and the maximum native tempo")
	}
	return nil
}

// Existing callers retain the old drand-v2 forecast. New production monitoring
// explicitly selects the same tempo-drift profile as its approved producer.
func monitorNativeEpochBoundary(native protocol.ValidatorNativeObservation) (uint64, bool) {
	return monitorNativeEpochBoundaryWithProfile(native, "")
}

// Monitor policy selects the profile alongside its exact expected producer and
// config. Empty retains old evidence; a forecast cannot declare a missed epoch.
func monitorNativeEpochBoundaryWithProfile(native protocol.ValidatorNativeObservation, profile string) (uint64, bool) {
	if crv4.ValidateEpochScheduleProfile(profile) != nil {
		return 0, false
	}
	if native.Block == 0 || native.Block >= math.MaxUint32 || native.Tempo == 0 || profile == "" && uint64(native.Tempo) > crv4.MaxTempo ||
		native.LastEpochBlock > native.Block || native.BlocksSinceLastStep > native.Block || native.PendingEpochAt > math.MaxUint32 {
		return 0, false
	}
	next := native.Block + 1
	boundary := max(next, native.LastEpochBlock+uint64(native.Tempo))
	if native.PendingEpochAt != 0 {
		boundary = min(boundary, max(next, native.PendingEpochAt))
	}
	limit := crv4.MaxTempo
	if profile == crv4.TempoDriftEpochScheduleProfile {
		limit = uint64(native.Tempo)
	}
	safetyBlocks := uint64(1)
	if native.BlocksSinceLastStep < limit {
		safetyBlocks = limit - native.BlocksSinceLastStep + 1
	}
	boundary = min(boundary, native.Block+safetyBlocks)
	return boundary, boundary <= math.MaxUint32
}

// Separate callbacks can coexist in one publication. A completed receipt wait
// must follow both its intent read and its schedule read, with an exact epoch
// match. Equal timestamps are ambiguous and conservatively stay unknown.
func monitorNativeDeadlineOrdered(intent protocol.ValidatorIntentObservation, native protocol.ValidatorNativeObservation, steering protocol.ValidatorSteeringObservation) bool {
	return intent.Value != nil && intent.Value.Status == "pending" && intent.Value.PreparedAtBlock != 0 &&
		intent.Value.PreparedAtBlock <= native.Block && intent.Value.FinalizedBlock == 0 && intent.Value.ApplicationBlock == 0 &&
		intent.Current && native.Current && steering.Current && steering.EpochKnown && steering.Outcome == "receipt_pending" &&
		steering.NativeEpoch == native.Epoch && intent.ObservedAt == intent.LastSuccessAt && native.ObservedAt == native.LastSuccessAt && steering.ObservedAt == steering.LastSuccessAt &&
		monitorProgressTime(steering.ObservedAt).After(monitorProgressTime(intent.ObservedAt)) &&
		monitorProgressTime(steering.ObservedAt).After(monitorProgressTime(native.ObservedAt))
}

// A source match permits only interpretation of local producer facts. Missing
// observations, failed receipt searches and stale clocks cannot create a miss.
func (self *monitorValidatorState) nativeDeadline(policy monitorValidatorPolicy, now time.Time) monitorNativeDeadlineObservation {
	result := monitorNativeDeadlineObservation{Status: "disabled"}
	if policy.NativeDeadline == nil {
		return result
	}
	result.Status, result.CriticalBlocks = "unavailable", policy.NativeDeadline.CompletionMarginBlocks
	if !self.sourceCurrent(now) || self.Record.Source != policy.ExpectedSource {
		return result
	}
	value := self.Record
	if value.Intent == nil || value.Native == nil || value.Steering == nil {
		result.Status = "unknown"
		return result
	}
	fresh := func(observed, success string, current bool) bool {
		return current && monitorServiceFresh(now, monitorProgressTime(observed)) && monitorServiceFresh(now, monitorProgressTime(success))
	}
	if !fresh(value.Intent.ObservedAt, value.Intent.LastSuccessAt, value.Intent.Current) ||
		!fresh(value.Native.ObservedAt, value.Native.LastSuccessAt, value.Native.Current) ||
		!fresh(value.Steering.ObservedAt, value.Steering.LastSuccessAt, value.Steering.Current) {
		return result
	}
	result.Current = true
	intent, native := value.Intent.Value, value.Native
	if intent == nil || intent.Status != "pending" {
		result.Status = "not-pending"
		return result
	}
	result.IntentEpoch, result.ObservedEpoch, result.ObservedBlock = intent.NativeEpoch, native.Epoch, native.Block
	boundary, valid := monitorNativeEpochBoundaryWithProfile(*native, policy.NativeDeadline.EpochScheduleProfile)
	if !valid || !monitorNativeDeadlineOrdered(*value.Intent, *native, *value.Steering) || intent.NativeEpoch > native.Epoch {
		result.Status = "incoherent"
		return result
	}
	result.Known = true
	if intent.NativeEpoch < native.Epoch {
		result.Status = "missed-window"
		return result
	}
	result.ProjectedBoundaryBlock, result.BlocksRemaining = boundary, boundary-native.Block
	result.WarningBlocks = max((uint64(native.Tempo)+4)/5, 2*result.CriticalBlocks)
	result.Status = "pending"
	if result.BlocksRemaining <= result.CriticalBlocks {
		result.Status = "critical"
	} else if result.BlocksRemaining <= result.WarningBlocks {
		result.Status = "warning"
	}
	return result
}

// The first finding and its original evidence cannot be replaced by a restart,
// configuration renewal, unavailable read, or a later finalized/applied report.
func (self *monitorValidatorState) retainNativeDeadline(policy monitorValidatorPolicy, now time.Time) {
	if self.nativeDeadline(policy, now).Status != "missed-window" {
		return
	}
	value := self.Record
	if self.NativeDeadline != nil && value.Intent.Value.NativeEpoch <= self.NativeDeadline.LastMiss.Intent.Value.NativeEpoch {
		return
	}
	intent := *value.Intent
	copy := *intent.Value
	intent.Value = &copy
	incident := monitorNativeDeadlineIncident{DetectedAt: now, Intent: intent, Native: *value.Native, Steering: *value.Steering}
	if self.NativeDeadline == nil {
		self.NativeDeadline = &monitorNativeDeadlineHistory{FirstMiss: incident}
	}
	self.NativeDeadline.LastMiss = incident
	if self.NativeDeadline.MissedWindows != math.MaxUint64 {
		self.NativeDeadline.MissedWindows++
	}
}

// Checkpoint admission verifies the same causal predicates used to retain a
// miss. The checksum binds it to the role; it is not a chain signature or ack.
func (self *monitorNativeDeadlineHistory) validate(source protocol.ValidatorProgressSource) error {
	if self == nil {
		return nil
	}
	if self.MissedWindows == 0 {
		return errors.New("native deadline incident has no missed window")
	}
	for _, incident := range []monitorNativeDeadlineIncident{self.FirstMiss, self.LastMiss} {
		stamp := incident.DetectedAt.Format(time.RFC3339Nano)
		probe := protocol.ValidatorProgress{Schema: protocol.ValidatorProgressSchema, Source: source,
			InstanceId: "11111111111111111111111111111111", StartedAt: stamp, HeartbeatAt: stamp,
			Publisher: protocol.ValidatorPublicationObservation{Outcome: "starting"}, Intent: &incident.Intent, Native: &incident.Native, Steering: &incident.Steering}
		if err := probe.Validate(); err != nil {
			return err
		}
		// Historical incidents contain an observed crossing, not a predicted
		// boundary. Validate root-set u16 tempos without changing that evidence.
		if _, valid := monitorNativeEpochBoundaryWithProfile(incident.Native, crv4.TempoDriftEpochScheduleProfile); !valid || incident.DetectedAt.IsZero() ||
			!monitorNativeDeadlineOrdered(incident.Intent, incident.Native, incident.Steering) || incident.Intent.Value.NativeEpoch >= incident.Native.Epoch {
			return errors.New("native deadline incident lacks its observed epoch crossing")
		}
		for _, observed := range []string{incident.Intent.ObservedAt, incident.Native.ObservedAt, incident.Steering.ObservedAt} {
			if !monitorServiceFresh(incident.DetectedAt, monitorProgressTime(observed)) {
				return errors.New("native deadline incident used unavailable evidence")
			}
		}
	}
	first, last := self.FirstMiss, self.LastMiss
	firstIntent, lastIntent := first.Intent, last.Intent
	firstIntent.Value, lastIntent.Value = nil, nil
	same := first.DetectedAt.Equal(last.DetectedAt) && first.Native == last.Native && first.Steering == last.Steering &&
		firstIntent == lastIntent && *first.Intent.Value == *last.Intent.Value
	if self.MissedWindows == 1 && !same || self.MissedWindows > 1 &&
		(first.Intent.Value.NativeEpoch >= last.Intent.Value.NativeEpoch || first.DetectedAt.After(last.DetectedAt.Add(monitorServiceClockAllowance))) {
		return errors.New("native deadline incident history is inconsistent")
	}
	return nil
}

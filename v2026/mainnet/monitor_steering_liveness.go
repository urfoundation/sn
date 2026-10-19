// Loop responsiveness is independent of publisher heartbeats and economic
// progress. Fixed local evidence diagnoses a stall but grants no repair action.
package main

import (
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Operators explicitly budget the longest admitted steering call plus its poll
// and observation intervals. These seconds are not native protocol deadlines.
type monitorSteeringLivenessPolicy struct {
	WarningAfterSeconds  uint32 `json:"warning_after_seconds"`
	CriticalAfterSeconds uint32 `json:"critical_after_seconds"`
}

// A returned retry/read-wait outcome demonstrates responsiveness even when its
// Current flag is false. It does not demonstrate success or economic progress.
type monitorSteeringSample struct {
	ObservedAt  time.Time                              `json:"observed_at"`
	ConfigHash  string                                 `json:"config_hash"`
	InstanceId  string                                 `json:"instance_id"`
	StartedAt   string                                 `json:"started_at"`
	HeartbeatAt string                                 `json:"heartbeat_at"`
	PublishedAt string                                 `json:"published_at"`
	Steering    *protocol.ValidatorSteeringObservation `json:"steering,omitempty"`
}

// One episode retains the responsive baseline and the exact stale/missing
// outcome under its original budget. Recovery is a later real loop outcome.
type monitorSteeringIncident struct {
	Id       string                        `json:"id"`
	Sequence uint64                        `json:"sequence"`
	Policy   monitorSteeringLivenessPolicy `json:"policy"`
	Baseline monitorSteeringSample         `json:"baseline"`
	Failure  monitorSteeringSample         `json:"failure"`
	Recovery *monitorSteeringSample        `json:"recovery,omitempty"`
}

// Only one baseline and the latest episode are retained. Earlier complete
// episodes belong in the event sink; the first detection and count stay bounded.
type monitorSteeringHistory struct {
	Baseline        *monitorSteeringSample   `json:"baseline,omitempty"`
	Incidents       uint64                   `json:"incidents"`
	FirstDetectedAt time.Time                `json:"first_detected_at"`
	LastIncident    *monitorSteeringIncident `json:"last_incident,omitempty"`
}

// Current means a fresh exact-source publication, not protocol success. Unknown
// includes first startup or a replacement instance without a responsive baseline.
type monitorSteeringLivenessObservation struct {
	Status    string    `json:"status"`
	Current   bool      `json:"current"`
	OutcomeAt time.Time `json:"outcome_at"`
}

var monitorSteeringLivenessCodes = map[string]int{
	"disabled": 0, "unavailable": 1, "unknown": 2, "responsive": 3,
	"warning": 4, "stalled": 5, "incoherent": 6,
}

// Finite explicit margins cannot override the independent source-freshness cut.
func (self *monitorSteeringLivenessPolicy) validate() error {
	if self != nil && (self.WarningAfterSeconds < 90 || self.CriticalAfterSeconds <= self.WarningAfterSeconds || self.CriticalAfterSeconds > 24*60*60) {
		return errors.New("steering liveness requires 90 <= warning < critical <= 86400 seconds")
	}
	return nil
}

// Copy the fixed producer evidence so subsequent sampling cannot rewrite a cut.
func (self *monitorValidatorState) steeringSample() monitorSteeringSample {
	value := self.Record
	sample := monitorSteeringSample{ObservedAt: self.SampleAt, ConfigHash: value.Source.ConfigHash, InstanceId: value.InstanceId,
		StartedAt: value.StartedAt, HeartbeatAt: value.HeartbeatAt, PublishedAt: value.Publisher.LastSuccessAt}
	if value.Steering != nil {
		steering := *value.Steering
		sample.Steering = &steering
	}
	return sample
}

// The source and publisher must both be fresh. Future clocks, including those
// within the general monitor skew allowance, cannot arm or recover this signal.
func (self monitorSteeringSample) validate(source protocol.ValidatorProgressSource) error {
	source.ConfigHash = self.ConfigHash
	value := protocol.ValidatorProgress{Schema: protocol.ValidatorProgressSchema, Source: source,
		InstanceId: self.InstanceId, StartedAt: self.StartedAt, HeartbeatAt: self.HeartbeatAt,
		Publisher: protocol.ValidatorPublicationObservation{Outcome: "published", LastSuccessAt: self.PublishedAt}, Steering: self.Steering}
	if err := value.Validate(); err != nil || self.ObservedAt.IsZero() ||
		monitorProgressMaximumTime(&value).After(self.ObservedAt) ||
		!monitorServiceFresh(self.ObservedAt, monitorProgressTime(self.HeartbeatAt)) ||
		!monitorServiceFresh(self.ObservedAt, monitorProgressTime(self.PublishedAt)) ||
		monitorProgressTime(self.StartedAt).After(monitorProgressTime(self.HeartbeatAt)) {
		return errors.New("steering sample lacks fresh coherent producer publication")
	}
	if self.Steering != nil && (monitorProgressTime(self.Steering.ObservedAt).After(monitorProgressTime(self.HeartbeatAt)) ||
		monitorProgressTime(self.Steering.LastSuccessAt).After(monitorProgressTime(self.Steering.ObservedAt))) {
		return errors.New("steering outcome exceeds its containing observation")
	}
	return nil
}

// A startup/restored record is not a loop completion by this process instance.
// An unchanged unsuccessful outcome is responsive only at its original age.
func (self monitorSteeringSample) responsive() bool {
	return self.Steering != nil && self.Steering.Outcome != "starting" &&
		!monitorProgressTime(self.Steering.ObservedAt).Before(monitorProgressTime(self.StartedAt)) &&
		monitorServiceFresh(self.ObservedAt, monitorProgressTime(self.Steering.ObservedAt))
}

// Missing steering preserves the independently retained last responsive cut.
// A different instance must first establish its own actual loop outcome.
func (self monitorSteeringSample) outcomeAfter(baseline *monitorSteeringSample) (time.Time, bool) {
	if baseline == nil || self.InstanceId != baseline.InstanceId || self.StartedAt != baseline.StartedAt || self.ObservedAt.Before(baseline.ObservedAt) {
		return time.Time{}, false
	}
	stamp := monitorProgressTime(baseline.Steering.ObservedAt)
	if self.Steering != nil && self.Steering.Outcome != "starting" {
		current := monitorProgressTime(self.Steering.ObservedAt)
		if current.Before(stamp) {
			return time.Time{}, false
		}
		stamp = current
	}
	return stamp, true
}

// Re-reading fresh publisher bytes cannot move the loop's last outcome time.
func (self *monitorValidatorState) steeringLiveness(policy monitorValidatorPolicy) monitorSteeringLivenessObservation {
	result := monitorSteeringLivenessObservation{Status: "disabled"}
	if policy.SteeringLiveness == nil {
		return result
	}
	result.Status = "unavailable"
	if !self.ClockFaultAt.IsZero() || !self.sourceCurrent(self.SampleAt) || self.Record.Source != policy.ExpectedSource || self.Record.Publisher.Outcome != "published" {
		return result
	}
	sample := self.steeringSample()
	if sample.validate(policy.ExpectedSource) != nil {
		result.Status = "incoherent"
		return result
	}
	result.Current, result.Status = true, "unknown"
	if sample.responsive() {
		result.Status, result.OutcomeAt = "responsive", monitorProgressTime(sample.Steering.ObservedAt)
		return result
	}
	if self.SteeringLiveness == nil {
		return result
	}
	stamp, known := sample.outcomeAfter(self.SteeringLiveness.Baseline)
	if !known {
		return result
	}
	result.OutcomeAt = stamp
	age := sample.ObservedAt.Sub(stamp)
	if age >= time.Duration(policy.SteeringLiveness.CriticalAfterSeconds)*time.Second {
		result.Status = "stalled"
	} else if age >= time.Duration(policy.SteeringLiveness.WarningAfterSeconds)*time.Second {
		result.Status = "warning"
	} else if sample.Steering != nil && sample.Steering.Outcome != "starting" {
		result.Status = "responsive"
	}
	return result
}

// A digest binds each episode to its role, original budget and causal evidence.
func (self monitorSteeringIncident) identity(policy monitorValidatorPolicy) (string, error) {
	self.Id, self.Recovery = "", nil
	source := policy.ExpectedSource
	source.ConfigHash = ""
	raw, err := json.Marshal(struct {
		Kind     string                           `json:"kind"`
		Role     string                           `json:"role"`
		Source   protocol.ValidatorProgressSource `json:"source"`
		Incident monitorSteeringIncident          `json:"incident"`
	}{Kind: "urnetwork-mainnet-steering-incident-v1", Role: policy.Role, Source: source, Incident: self})
	if err != nil {
		return "", err
	}
	return monitorReadDigest(raw), nil
}

// Fresh reads, policy edits and monitor restart never resolve an open stall.
// A later returned outcome may resolve responsiveness while protocol faults stay.
func (self *monitorValidatorState) retainSteeringLiveness(policy monitorValidatorPolicy) error {
	observation := self.steeringLiveness(policy)
	if !observation.Current {
		return nil
	}
	sample := self.steeringSample()
	if sample.responsive() {
		if self.SteeringLiveness == nil {
			self.SteeringLiveness = &monitorSteeringHistory{}
		}
		history := self.SteeringLiveness
		if incident := history.LastIncident; incident != nil && incident.Recovery == nil &&
			monitorProgressTime(sample.Steering.ObservedAt).After(incident.Failure.ObservedAt) {
			incident.Recovery = &sample
		}
		history.Baseline = &sample
		return nil
	}
	if observation.Status != "stalled" {
		return nil
	}
	history := self.SteeringLiveness
	if history.LastIncident != nil && history.LastIncident.Recovery == nil {
		return nil
	}
	if history.Incidents == math.MaxUint64 {
		return errors.New("steering incident sequence is exhausted")
	}
	incident := &monitorSteeringIncident{Sequence: history.Incidents + 1, Policy: *policy.SteeringLiveness,
		Baseline: *history.Baseline, Failure: sample}
	var err error
	incident.Id, err = incident.identity(policy)
	if err != nil {
		return err
	}
	history.Incidents, history.LastIncident = incident.Sequence, incident
	if history.FirstDetectedAt.IsZero() {
		history.FirstDetectedAt = sample.ObservedAt
	}
	return nil
}

// A retained open incident remains unresolved when its policy is removed or its
// input is unavailable. Only the sampling owner can attach recovery evidence.
func (self *monitorSteeringHistory) unresolved() bool {
	return self != nil && self.LastIncident != nil && self.LastIncident.Recovery == nil
}

// Recheck causal cuts as well as hashes. Local checksums are not hostile-host
// attestation, independent process identity, or permission to stop a service.
func (self *monitorSteeringHistory) validate(policy monitorValidatorPolicy, highWater time.Time) error {
	if self == nil {
		return nil
	}
	checkSample := func(sample *monitorSteeringSample, responsive bool) error {
		if sample == nil || sample.validate(policy.ExpectedSource) != nil || sample.ObservedAt.After(highWater) || responsive && !sample.responsive() {
			return errors.New("steering history has an invalid sample")
		}
		return nil
	}
	if err := checkSample(self.Baseline, true); err != nil {
		return err
	}
	if self.Incidents == 0 {
		if self.LastIncident != nil || !self.FirstDetectedAt.IsZero() {
			return errors.New("empty steering history contains an incident")
		}
		return nil
	}
	incident := self.LastIncident
	if incident == nil || incident.Sequence != self.Incidents || incident.Policy.validate() != nil ||
		checkSample(&incident.Baseline, true) != nil || checkSample(&incident.Failure, false) != nil ||
		self.FirstDetectedAt.IsZero() || self.FirstDetectedAt.After(incident.Failure.ObservedAt) ||
		self.Incidents == 1 && !self.FirstDetectedAt.Equal(incident.Failure.ObservedAt) {
		return errors.New("steering incident lost its original evidence")
	}
	stamp, known := incident.Failure.outcomeAfter(&incident.Baseline)
	id, err := incident.identity(policy)
	if !known || incident.Failure.ObservedAt.Sub(stamp) < time.Duration(incident.Policy.CriticalAfterSeconds)*time.Second || err != nil || id != incident.Id {
		return errors.New("steering incident is not an attributable expired loop budget")
	}
	if recovery := incident.Recovery; recovery != nil {
		if checkSample(recovery, true) != nil || !monitorProgressTime(recovery.Steering.ObservedAt).After(incident.Failure.ObservedAt) ||
			self.Baseline.ObservedAt.Before(recovery.ObservedAt) {
			return errors.New("steering recovery lacks a later real loop outcome")
		}
	}
	return nil
}

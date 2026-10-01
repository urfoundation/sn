// Read incidents retain bounded local continuity across recovery and restart.
// They describe completed source reads, never service health or repair authority.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// A closed classification is retained without raw errors or candidate labels.
type monitorReadFailure struct {
	ObservedAt time.Time `json:"observed_at"`
	Status     string    `json:"status"`
}

// The digest names the canonical decoded record, including its original ages.
// A successful read can recover while that record still reports stale or failed
// application work. This is not an independent receipt or incident resolution.
type monitorReadRecovery struct {
	IncidentId  string    `json:"incident_id"`
	Sequence    uint64    `json:"sequence"`
	ObservedAt  time.Time `json:"observed_at"`
	ConfigHash  string    `json:"config_hash"`
	InstanceId  string    `json:"instance_id"`
	HeartbeatAt string    `json:"heartbeat_at"`
	RecordHash  string    `json:"record_hash"`
}

// One contiguous run of rejected reads owns one stable identity. The producer
// domain lives once in history; each episode preserves its original config.
type monitorReadIncident struct {
	Id                 string               `json:"id"`
	Sequence           uint64               `json:"sequence"`
	ExpectedConfigHash string               `json:"expected_config_hash"`
	OutageSince        time.Time            `json:"outage_since"`
	FirstFailure       monitorReadFailure   `json:"first_failure"`
	LastFailure        monitorReadFailure   `json:"last_failure"`
	FailedReads        uint64               `json:"failed_reads"`
	Recovery           *monitorReadRecovery `json:"recovery,omitempty"`
}

// Fixed first/latest summaries and the latest recovery bound storage. Middle
// episodes require external event retention; totals are local lifetime lower
// bounds after counter saturation or migration from a checkpoint without history.
type monitorReadIncidentHistory struct {
	Producer            protocol.ValidatorProgressSource `json:"producer"`
	StartedAt           time.Time                        `json:"started_at"`
	PriorHistoryUnknown bool                             `json:"prior_history_unknown"`
	Incidents           uint64                           `json:"incidents"`
	FailedReads         uint64                           `json:"failed_reads"`
	FirstIncident       *monitorReadIncident             `json:"first_incident,omitempty"`
	LastIncident        *monitorReadIncident             `json:"last_incident,omitempty"`
	LastRecovery        *monitorReadRecovery             `json:"last_recovery,omitempty"`
}

// Only completed read failures open incidents. Staleness and protocol failures
// inside an accepted record keep their existing independent status semantics.
func monitorReadFailed(status string) bool {
	switch status {
	case "missing", "unavailable", "changed", "invalid", "identity", "clock":
		return true
	default:
		return false
	}
}

// A domain-separated digest makes identity independent of retries or exports.
func monitorReadIncidentId(policy monitorValidatorPolicy, incident monitorReadIncident) (string, error) {
	source := policy.ExpectedSource
	source.ConfigHash = incident.ExpectedConfigHash
	raw, err := json.Marshal(struct {
		Kind           string                           `json:"kind"`
		Role           string                           `json:"role"`
		Sequence       uint64                           `json:"sequence"`
		ExpectedSource protocol.ValidatorProgressSource `json:"expected_source"`
		OutageSince    time.Time                        `json:"outage_since"`
		FirstFailure   monitorReadFailure               `json:"first_failure"`
	}{Kind: "urnetwork-mainnet-validator-read-incident-v1", Role: policy.Role,
		Sequence: incident.Sequence, ExpectedSource: source,
		OutageSince: incident.OutageSince, FirstFailure: incident.FirstFailure})
	if err != nil {
		return "", err
	}
	return monitorReadDigest(raw), nil
}

// Canonical local hashes detect substitution, not hostile-host forgery.
func monitorReadDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Check the canonical spelling without accepting alternate hash encodings.
func validMonitorReadDigest(value string) bool {
	if len(value) != 71 || value[:7] != "sha256:" {
		return false
	}
	raw, err := hex.DecodeString(value[7:])
	var nonzero byte
	for _, value := range raw {
		nonzero |= value
	}
	return err == nil && nonzero != 0 && hex.EncodeToString(raw) == value[7:]
}

// Only the sampling owner mutates history, once per completed observation.
// Publishing or replaying an event never reopens an incident or repeats recovery.
func (self *monitorValidatorState) retainReadIncident(policy monitorValidatorPolicy) error {
	if self.SampleAt.IsZero() || self.ReadStatus != "ok" && !monitorReadFailed(self.ReadStatus) {
		return errors.New("read incident requires a completed classified observation")
	}
	if self.ReadIncidents == nil {
		self.ReadIncidents = &monitorReadIncidentHistory{Producer: policy.ExpectedSource, StartedAt: self.SampleAt}
	}
	history := self.ReadIncidents
	if self.ReadStatus == "ok" {
		if history.LastIncident == nil || history.LastIncident.Recovery != nil {
			return nil
		}
		if !self.readCurrent || self.Record == nil || self.Record.Source != policy.ExpectedSource {
			return errors.New("read incident recovery lacks a current exact-source read")
		}
		raw, err := self.Record.Encode()
		if err != nil {
			return err
		}
		incident := *history.LastIncident
		incident.Recovery = &monitorReadRecovery{IncidentId: incident.Id, Sequence: incident.Sequence,
			ObservedAt: self.SampleAt, ConfigHash: self.Record.Source.ConfigHash, InstanceId: self.Record.InstanceId,
			HeartbeatAt: self.Record.HeartbeatAt, RecordHash: monitorReadDigest(raw)}
		history.LastIncident, history.LastRecovery = &incident, incident.Recovery
	} else {
		failure := monitorReadFailure{ObservedAt: self.SampleAt, Status: self.ReadStatus}
		if history.LastIncident == nil || history.LastIncident.Recovery != nil {
			if history.Incidents == math.MaxUint64 {
				return errors.New("read incident sequence is exhausted")
			}
			incident := monitorReadIncident{Sequence: history.Incidents + 1, ExpectedConfigHash: policy.ExpectedSource.ConfigHash,
				OutageSince: self.OutageSince, FirstFailure: failure}
			var err error
			incident.Id, err = monitorReadIncidentId(policy, incident)
			if err != nil {
				return err
			}
			history.Incidents, history.LastIncident = incident.Sequence, &incident
		}
		incident := *history.LastIncident
		incident.LastFailure = failure
		if incident.FailedReads != math.MaxUint64 {
			incident.FailedReads++
		}
		if history.FailedReads != math.MaxUint64 {
			history.FailedReads++
		}
		history.LastIncident = &incident
	}
	if history.Incidents == 1 {
		history.FirstIncident = history.LastIncident
	}
	return nil
}

// The original retained outage can be carried forward, but an old snapshot
// supplies only its last failed read. Earlier failures and recoveries stay unknown.
func (self *monitorValidatorState) migrateReadIncidents(policy monitorValidatorPolicy) error {
	if err := self.retainReadIncident(policy); err != nil {
		return err
	}
	self.ReadIncidents.PriorHistoryUnknown = true
	return nil
}

// Recovery identity uses the existing strict source and instance contract.
// Clock ordering is deliberately not guessed after a clock-related incident.
func (self *monitorReadRecovery) validate(policy monitorValidatorPolicy) error {
	if self == nil {
		return nil
	}
	source := policy.ExpectedSource
	source.ConfigHash = self.ConfigHash
	probe := protocol.ValidatorProgress{Schema: protocol.ValidatorProgressSchema, Source: source,
		InstanceId: self.InstanceId, StartedAt: self.HeartbeatAt, HeartbeatAt: self.HeartbeatAt,
		Publisher: protocol.ValidatorPublicationObservation{Outcome: "starting"}}
	if err := probe.Validate(); err != nil {
		return err
	}
	if self.ObservedAt.IsZero() || self.Sequence == 0 || !validMonitorReadDigest(self.RecordHash) ||
		!validMonitorReadDigest(self.IncidentId) {
		return errors.New("read recovery has incomplete or foreign evidence")
	}
	if monitorProgressTime(self.HeartbeatAt).After(self.ObservedAt.Add(monitorServiceClockAllowance)) {
		return errors.New("read recovery has an unaccepted future heartbeat")
	}
	return nil
}

// All immutable identity fields are rehashed on checkpoint admission. First
// and latest read times may go backward; the clock failure itself must survive.
func (self *monitorReadIncident) validate(policy monitorValidatorPolicy) error {
	if self == nil || self.Sequence == 0 || self.FailedReads == 0 || self.OutageSince.IsZero() ||
		self.FirstFailure.ObservedAt.IsZero() || self.LastFailure.ObservedAt.IsZero() ||
		!monitorReadFailed(self.FirstFailure.Status) || !monitorReadFailed(self.LastFailure.Status) ||
		!validMonitorReadDigest(self.ExpectedConfigHash) {
		return errors.New("read incident has incomplete or foreign evidence")
	}
	id, err := monitorReadIncidentId(policy, *self)
	if err != nil || self.Id != id || self.FailedReads == 1 && self.FirstFailure != self.LastFailure {
		return errors.New("read incident identity or failure count differs")
	}
	if err := self.Recovery.validate(policy); err != nil {
		return err
	}
	if self.Recovery != nil && (self.Recovery.IncidentId != self.Id || self.Recovery.Sequence != self.Sequence) {
		return errors.New("read recovery names another incident")
	}
	return nil
}

// Pointer identity does not survive serialization; compare the complete value.
func sameMonitorReadIncident(first, last *monitorReadIncident) bool {
	a, b := *first, *last
	a.Recovery, b.Recovery = nil, nil
	return a == b && (first.Recovery == nil && last.Recovery == nil ||
		first.Recovery != nil && last.Recovery != nil && *first.Recovery == *last.Recovery)
}

// Check both summaries and their causal relationship to the actual saved read.
// A checksum alone cannot turn an open incident into successful recovery.
func (self *monitorReadIncidentHistory) validate(policy monitorValidatorPolicy, state *monitorValidatorState) error {
	if self == nil || self.StartedAt.IsZero() {
		return errors.New("read incident history is absent")
	}
	if self.StartedAt.After(state.HighWaterAt) {
		return errors.New("read incident baseline exceeds its observed clock high-water")
	}
	probe := protocol.ValidatorProgress{Schema: protocol.ValidatorProgressSchema, Source: self.Producer,
		InstanceId: "11111111111111111111111111111111", StartedAt: "2000-01-01T00:00:00Z", HeartbeatAt: "2000-01-01T00:00:00Z",
		Publisher: protocol.ValidatorPublicationObservation{Outcome: "starting"}}
	if err := probe.Validate(); err != nil || !monitorSameProducerRole(self.Producer, policy.ExpectedSource) {
		return errors.New("read incident history has an invalid or foreign producer")
	}
	if self.Incidents == 0 {
		if self.FailedReads != 0 || self.FirstIncident != nil || self.LastIncident != nil || self.LastRecovery != nil || state.ReadStatus != "ok" {
			return errors.New("empty read history hides a failed observation")
		}
		return nil
	}
	for _, incident := range []*monitorReadIncident{self.FirstIncident, self.LastIncident} {
		if err := incident.validate(policy); err != nil {
			return err
		}
		if incident.FirstFailure.ObservedAt.After(state.HighWaterAt) || incident.LastFailure.ObservedAt.After(state.HighWaterAt) ||
			incident.Recovery != nil && incident.Recovery.ObservedAt.After(state.HighWaterAt) {
			return errors.New("read incident evidence exceeds its observed clock high-water")
		}
	}
	first, last := self.FirstIncident, self.LastIncident
	if first.Sequence != 1 || last.Sequence != self.Incidents || self.FailedReads < self.Incidents ||
		self.FailedReads < first.FailedReads || self.FailedReads < last.FailedReads {
		return errors.New("read incident counts are inconsistent")
	}
	if self.Incidents == 1 {
		if !sameMonitorReadIncident(first, last) || self.FailedReads != first.FailedReads {
			return errors.New("single read incident summaries differ")
		}
	} else if first.Id == last.Id || first.Recovery == nil || self.LastRecovery == nil ||
		self.FailedReads != math.MaxUint64 && self.FailedReads-first.FailedReads < last.FailedReads {
		return errors.New("recurring read incident lost prior recovery")
	}
	if err := self.LastRecovery.validate(policy); err != nil {
		return err
	}
	if self.LastRecovery != nil && self.LastRecovery.ObservedAt.After(state.HighWaterAt) {
		return errors.New("read recovery exceeds its observed clock high-water")
	}
	if last.Recovery == nil {
		if state.ReadStatus == "ok" || state.OutageSince != last.OutageSince ||
			last.LastFailure != (monitorReadFailure{ObservedAt: state.SampleAt, Status: state.ReadStatus}) ||
			self.Incidents == 1 && self.LastRecovery != nil || self.Incidents > 1 && self.LastRecovery.Sequence != self.Incidents-1 {
			return errors.New("open read incident differs from its failed observation")
		}
	} else if state.ReadStatus != "ok" || self.LastRecovery == nil || *self.LastRecovery != *last.Recovery {
		return errors.New("recovered read incident differs from its successful observation")
	}
	if self.LastRecovery != nil && self.LastRecovery.Sequence == first.Sequence &&
		(first.Recovery == nil || *self.LastRecovery != *first.Recovery) {
		return errors.New("first read recovery evidence differs")
	}
	return nil
}

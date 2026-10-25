// Active repair is a separate independent signature over one diagnosed process
// generation. A stopped-repair approval never grants a stop permission.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

const repairActiveValidatorSchema = "urnetwork-mainnet-active-validator-repair-v1"
const repairActiveValidatorApprovalDomain = "urnetwork-mainnet-active-validator-repair-approval-v1"

// The process profile preserves its entire original v4 incident. The exact
// services file pins enabled liveness policy independently of producer bytes.
type repairActiveValidatorPlan struct {
	Process           repairValidatorPlan `json:"process"`
	MonitorServices   planFileReference   `json:"monitor_services"`
	MaximumStops      uint8               `json:"maximum_stops"`
	JoinWindowSeconds uint32              `json:"join_window_seconds"`
}

// No approver private key or approval issuer exists in this controller.
type repairActiveValidatorApproval struct {
	Schema    string                    `json:"schema"`
	Plan      repairActiveValidatorPlan `json:"plan"`
	Signature string                    `json:"signature_ed25519"`
}

// Canonical signatures cover every original checkpoint field and finite limit.
func (self repairActiveValidatorApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(repairActiveValidatorApprovalDomain+"\x00"), raw...), err
}

// The expected key is an independent command input, never selected by the file.
func (self repairActiveValidatorApproval) validate(publicKey string) error {
	if self.Schema != repairActiveValidatorSchema || !rootCanonicalHash(publicKey) {
		return errors.New("active repair schema or independent key differs")
	}
	if err := self.Plan.validate(); err != nil {
		return err
	}
	key, _ := hex.DecodeString(publicKey[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	message, encodeErr := self.signingBytes()
	if err != nil || encodeErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("active repair independent signature differs")
	}
	return nil
}

// The signed original must itself be a current, unrecovered diagnosed stall,
// with a recorded outcome. Missing/starting-only telemetry does not admit stops.
func (self repairActiveValidatorPlan) validate() error {
	p := self.Process
	if err := p.validateProfile(); err != nil {
		return err
	}
	if self.MaximumStops != 1 || self.JoinWindowSeconds < 60 || self.JoinWindowSeconds > 600 || p.CommandTimeoutSeconds < 35 ||
		!repairValidatorPath(self.MonitorServices.Path) || !validMonitorReadDigest(self.MonitorServices.Sha256) {
		return errors.New("active repair policy pin or stop/join limits differ")
	}
	for _, path := range []string{p.StatePath, p.StatePath + ".lock", p.Unit.File.Path, p.Unit.Binary.Path, p.Unit.Config.Path, p.Unit.ProgressFile, p.Unit.StateDirectory, p.Unit.File.Path + ".sn-control.lock", p.Systemctl.Path, p.MonitorCheckpoint} {
		if self.MonitorServices.Path == path {
			return errors.New("active repair policy path overlaps an owned input")
		}
	}
	if err := self.incident(p.Original); err != nil {
		return err
	}
	state := p.Original.State
	state.readCurrent = true
	if state.steeringLiveness(self.policy()).Status != "stalled" || monitorProgressMaximumTime(state.Record).After(state.SampleAt) {
		return errors.New("active repair original was not a current steering stall")
	}
	return nil
}

// The original episode supplies its immutable, independently signed margins.
func (self repairActiveValidatorPlan) policy() monitorValidatorPolicy {
	p := self.Process
	policy := monitorValidatorPolicy{Role: p.Role, ProgressFile: p.Unit.ProgressFile, ExpectedSource: p.Source}
	if history := p.Original.State.SteeringLiveness; history != nil && history.LastIncident != nil {
		margins := history.LastIncident.Policy
		policy.SteeringLiveness = &margins
	}
	return policy
}

// Restart cannot substitute a later episode, recovered baseline, changed source
// or changed margins. Post-stop availability loss is interpreted only by phase.
func (self repairActiveValidatorPlan) incident(record monitorServiceCheckpointRecord) error {
	p := self.Process
	policy := self.policy()
	hash, err := hashMonitorServiceCheckpoint(record)
	if err != nil || hash != record.ContentHash || record.Schema != monitorServiceCheckpointSchema || record.Role != p.Role || record.Expected != p.Source ||
		validateMonitorValidatorState(record.State) != nil || record.State.ReadIncidents.validate(policy, &record.State) != nil || record.State.SteeringLiveness.validate(policy, record.State.HighWaterAt) != nil {
		return errors.New("active repair incident checkpoint differs")
	}
	history, original := record.State.SteeringLiveness, p.Original.State.SteeringLiveness
	if history == nil || original == nil || history.LastIncident == nil || original.LastIncident == nil || !history.unresolved() ||
		history.LastIncident.Id != p.IncidentId || rootObjectHash(history) != rootObjectHash(original) || !record.State.ClockFaultAt.IsZero() ||
		record.State.ReadStatus != "ok" && record.State.ReadStatus != "missing" && record.State.ReadStatus != "unavailable" {
		return errors.New("active repair requires its exact original unresolved steering episode")
	}
	incident := history.LastIncident
	value := record.State.Record
	if value == nil || value.Source != p.Source || incident.Failure.ConfigHash != p.Source.ConfigHash || incident.Baseline.ConfigHash != p.Source.ConfigHash ||
		value.InstanceId != incident.Failure.InstanceId || value.StartedAt != incident.Failure.StartedAt || incident.Failure.Steering == nil ||
		incident.Failure.Steering.Outcome == "starting" || value.Steering == nil || *value.Steering != *incident.Failure.Steering {
		return errors.New("active repair producer changed or made steering progress")
	}
	return nil
}

// A direct producer read must retain the exact diagnosed outcome. This catches
// recovery before the monitor's next checkpoint and does not trust file age alone.
func (self repairActiveValidatorPlan) stalledProgress(value *protocol.ValidatorProgress, now time.Time, fresh bool) error {
	p := self.Process
	original := p.Original.State.Record
	if value == nil || value.Source != p.Source || value.InstanceId != original.InstanceId || value.StartedAt != original.StartedAt ||
		value.Steering == nil || *value.Steering != *original.Steering || monitorProgressMaximumTime(value).After(now) {
		return errors.New("active repair direct producer changed or made steering progress")
	}
	if fresh && (value.Publisher.Outcome != "published" || !monitorServiceFresh(now, monitorProgressTime(value.HeartbeatAt)) ||
		!monitorServiceFresh(now, monitorProgressTime(value.Publisher.LastSuccessAt))) {
		return errors.New("active repair producer liveness is unknown")
	}
	return nil
}

// A fresh stop must leave the full join budget plus both command durations
// inside the original signature window. This is headroom, not recovery grace.
func (self repairActiveValidatorPlan) stopWindow(now time.Time) error {
	budget := time.Duration(self.JoinWindowSeconds+2*self.Process.CommandTimeoutSeconds) * time.Second
	if now.Add(budget).After(self.Process.ExpiresAt) {
		return errors.New("active repair stop lacks signed join/start headroom")
	}
	return nil
}

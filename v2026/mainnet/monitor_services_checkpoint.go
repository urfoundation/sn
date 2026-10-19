// Per-role checkpoints use the existing process lock and atomic publication.
// Checksums preserve bounded local evidence, not hostile-host attestation.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/urfoundation/sn/v2026/protocol"
)

const monitorServiceCheckpointSchema = "urnetwork-mainnet-validator-checkpoint-v4"
const maxMonitorServiceCheckpointBytes = 16 * 1024

// The role and independent source are checksummed alongside retained evidence.
type monitorServiceCheckpointRecord struct {
	Schema      string                           `json:"schema"`
	Role        string                           `json:"role"`
	Expected    protocol.ValidatorProgressSource `json:"expected_source"`
	State       monitorValidatorState            `json:"state"`
	ContentHash string                           `json:"content_hash"`
}

// An existing common checkpoint lock protects one service-role record.
type monitorServiceCheckpoint struct {
	owner         *monitorCheckpointStore
	policy        monitorValidatorPolicy
	directoryInfo os.FileInfo
}

// Reuse the existing owner acquisition; source decoding belongs to this schema.
func openMonitorServiceCheckpoint(path string, expected identityExpectation, policy monitorValidatorPolicy, contexts ...context.Context) (*monitorServiceCheckpoint, error) {
	owner, err := openMonitorCheckpoint(path, expected, contexts...)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(filepath.Dir(owner.path))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return nil, monitorAdmissionFailure(errors.Join(errors.New("service checkpoint requires a protected directory"), err), owner.close())
	}
	return &monitorServiceCheckpoint{owner: owner, policy: policy, directoryInfo: info}, nil
}

// Configuration renewal can retain old operational evidence for the same role.
// It cannot substitute deployment, validator, network or genesis identity.
func monitorSameProducerRole(a, b protocol.ValidatorProgressSource) bool {
	a.ConfigHash, b.ConfigHash = "", ""
	return a == b
}

// Hash every field except the checksum itself using its stable wire encoding.
func hashMonitorServiceCheckpoint(record monitorServiceCheckpointRecord) (string, error) {
	record.ContentHash = ""
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// No missing/failed read or restart creates current evidence or an empty intent.
func (self *monitorServiceCheckpoint) load(ctx context.Context) (*monitorValidatorState, error) {
	if err := self.validateOwner(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := self.owner.directory.read(filepath.Base(self.owner.path), maxMonitorServiceCheckpointBytes, true)
	if err != nil {
		if monitorCheckpointAbsent(err) {
			return &monitorValidatorState{ReadStatus: "starting"}, nil
		}
		return nil, err
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record monitorServiceCheckpointRecord
	if err := decoder.Decode(&record); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("service checkpoint contains trailing JSON")
	}
	actual, err := hashMonitorServiceCheckpoint(record)
	legacy := (record.Schema == "urnetwork-mainnet-validator-checkpoint-v1" && record.State.NativeDeadline == nil ||
		record.Schema == "urnetwork-mainnet-validator-checkpoint-v2") && record.State.ReadIncidents == nil
	previous := record.Schema == "urnetwork-mainnet-validator-checkpoint-v3" && record.State.SteeringLiveness == nil
	if err != nil || actual != record.ContentHash || record.Schema != monitorServiceCheckpointSchema && !previous && !legacy || legacy && record.State.SteeringLiveness != nil || record.Role != self.policy.Role || !monitorSameProducerRole(record.Expected, self.policy.ExpectedSource) {
		return nil, errors.New("service checkpoint checksum or expected producer differs")
	}
	if err := validateMonitorValidatorState(record.State); err != nil {
		return nil, err
	}
	if record.State.Record != nil && !monitorSameProducerRole(record.State.Record.Source, record.Expected) {
		return nil, errors.New("retained service record differs from its role")
	}
	if legacy {
		originalPolicy := self.policy
		originalPolicy.ExpectedSource = record.Expected
		if err := record.State.migrateReadIncidents(originalPolicy); err != nil {
			return nil, err
		}
	}
	if err := record.State.ReadIncidents.validate(self.policy, &record.State); err != nil {
		return nil, err
	}
	if err := record.State.SteeringLiveness.validate(self.policy, record.State.HighWaterAt); err != nil {
		return nil, err
	}
	return &record.State, nil
}

// Retained numbers require their original read and source evidence. Future
// timestamps remain intact so restart cannot reset a clock incident.
func validateMonitorValidatorState(state monitorValidatorState) error {
	if state.SampleAt.IsZero() || state.HighWaterAt.IsZero() {
		return errors.New("service checkpoint has no completed observation")
	}
	switch state.ReadStatus {
	case "ok", "missing", "unavailable", "invalid", "identity", "clock", "changed":
	default:
		return errors.New("service checkpoint read status is unknown")
	}
	if (state.Record == nil) != state.LastReadSuccessAt.IsZero() || state.ReadStatus == "ok" && (state.Record == nil || !state.OutageSince.IsZero()) || state.ReadStatus != "ok" && state.OutageSince.IsZero() {
		return errors.New("service checkpoint has incomplete read evidence")
	}
	if state.Record != nil {
		if err := state.Record.Validate(); err != nil {
			return err
		}
		return state.NativeDeadline.validate(state.Record.Source)
	}
	if state.NativeDeadline != nil {
		return errors.New("service checkpoint deadline incident lost its producer identity")
	}
	return nil
}

// Check the same physical parent and lock before every read or write.
func (self *monitorServiceCheckpoint) validateOwner() error {
	if self == nil || self.owner == nil {
		return errors.New("service checkpoint owner is absent")
	}
	if err := self.owner.requireOwner(); err != nil {
		return err
	}
	if self.owner.directory.head != nil {
		return nil
	}
	if self == nil || self.owner == nil || self.owner.lock == nil {
		return &monitorOutputOwnershipError{reason: "service checkpoint owner is closed"}
	}
	info, err := os.Lstat(filepath.Dir(self.owner.path))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 || !os.SameFile(info, self.directoryInfo) {
		return &monitorOutputOwnershipError{reason: "service checkpoint directory changed"}
	}
	owned, ownedErr := self.owner.lock.Stat()
	named, nameErr := self.owner.directory.stat(filepath.Base(self.owner.path) + ".lock")
	if err := errors.Join(ownedErr, nameErr); err != nil {
		return monitorNamedObservation(err)
	}
	if !named.Mode().IsRegular() || named.Mode().Perm()&0077 != 0 || !os.SameFile(owned, named) {
		return &monitorOutputOwnershipError{reason: "service checkpoint lock changed"}
	}
	info, err = self.owner.directory.stat(filepath.Base(self.owner.path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return &monitorOutputOwnershipError{reason: "service checkpoint destination changed"}
	}
	return nil
}

// Complete atomic replacement uses the shared primitive and its ambiguity
// contract. Retrying republishes the complete in-memory role state.
func (self *monitorServiceCheckpoint) save(state *monitorValidatorState) error {
	if err := self.validateOwner(); err != nil {
		return err
	}
	if err := validateMonitorValidatorState(*state); err != nil {
		return err
	}
	// Direct callers may supply one legacy-shaped observation. The worker
	// already retained its completed read before attempting any publication.
	if state.ReadIncidents == nil {
		if err := state.retainReadIncident(self.policy); err != nil {
			return err
		}
	}
	if err := state.ReadIncidents.validate(self.policy, state); err != nil {
		return err
	}
	if err := state.SteeringLiveness.validate(self.policy, state.HighWaterAt); err != nil {
		return err
	}
	record := monitorServiceCheckpointRecord{Schema: monitorServiceCheckpointSchema, Role: self.policy.Role, Expected: self.policy.ExpectedSource, State: *state}
	var err error
	record.ContentHash, err = hashMonitorServiceCheckpoint(record)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw)+1 > maxMonitorServiceCheckpointBytes {
		return errors.New("service checkpoint exceeds its bound")
	}
	return errors.Join(self.owner.directory.publish(filepath.Base(self.owner.path), append(raw, '\n'), 0600, self.owner.syncDirectory), self.validateOwner())
}

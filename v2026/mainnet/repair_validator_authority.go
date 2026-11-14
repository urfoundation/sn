// One independently approved stopped validator may consume one service start.
// This authority neither creates a validator nor changes its signing policy.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

const repairValidatorSchema = "urnetwork-mainnet-validator-repair-v1"
const repairValidatorApprovalDomain = "urnetwork-mainnet-validator-repair-approval-v1"

var repairValidatorPathPattern = regexp.MustCompile(`^/[a-zA-Z0-9_./-]+$`)
var repairValidatorMountPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+\.mount$`)

// Every launch input and operational source is selected outside the controller.
// Config is the existing production config, not a generated replacement.
type repairValidatorUnit struct {
	DurableVolumes *durablevolume.Reference `json:"durable_volumes,omitempty"`
	Name           string                   `json:"name"`
	File           planFileReference        `json:"file"`
	Binary         planFileReference        `json:"binary"`
	Config         planFileReference        `json:"config"`
	StateDirectory string                   `json:"state_directory"`
	ProgressFile   string                   `json:"progress_file"`
	Uid            uint32                   `json:"uid"`
	Gid            uint32                   `json:"gid"`
}

// PID alone is insufficient: boot and monotonic start identify its lifetime.
type repairValidatorGeneration struct {
	InvocationId string `json:"invocation_id"`
	Pid          uint32 `json:"pid"`
	StartedUsec  uint64 `json:"started_monotonic_usec"`
}

// Original contains the actual monitor incident and preceding producer record.
// It is retained even if subsequent monitoring records recover or disappear.
type repairValidatorPlan struct {
	Role                    string                           `json:"role"`
	Source                  protocol.ValidatorProgressSource `json:"source"`
	MachineId               string                           `json:"machine_id"`
	BootId                  string                           `json:"boot_id"`
	Unit                    repairValidatorUnit              `json:"unit"`
	Systemctl               planFileReference                `json:"systemctl"`
	RequiredMounts          []string                         `json:"required_mounts"`
	Previous                repairValidatorGeneration        `json:"previous"`
	Original                monitorServiceCheckpointRecord   `json:"original"`
	IncidentId              string                           `json:"incident_id"`
	MonitorCheckpoint       string                           `json:"monitor_checkpoint"`
	MonitorUid              uint32                           `json:"monitor_uid"`
	StatePath               string                           `json:"state_path"`
	ValidFrom               time.Time                        `json:"valid_from"`
	ExpiresAt               time.Time                        `json:"expires_at"`
	MaximumStarts           uint8                            `json:"maximum_starts"`
	MaximumObservations     uint32                           `json:"maximum_observations"`
	CommandTimeoutSeconds   uint32                           `json:"command_timeout_seconds"`
	MaximumSampleAgeSeconds uint32                           `json:"maximum_sample_age_seconds"`
}

// The verifier key is independently supplied; this document cannot select it.
type repairValidatorApproval struct {
	Schema    string              `json:"schema"`
	Plan      repairValidatorPlan `json:"plan"`
	Signature string              `json:"signature_ed25519"`
}

// No implicit environment expansion, unit specifier, quoting or shell syntax.
func repairValidatorPath(path string) bool {
	return repairValidatorPathPattern.MatchString(path) && len(path) <= 2048 && filepath.Clean(path) == path && path != "/"
}

// Fixed-size lowercase IDs preserve one spelling in signatures and journals.
func repairValidatorHex(value string, size int) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == size && hex.EncodeToString(raw) == value && value != strings.Repeat("0", 2*size)
}

// This dedicated installed profile cannot poll latest images or run stop hooks.
// Initial installation/activation is a separately approved operations task.
func (self repairValidatorUnit) render() []byte {
	return []byte(fmt.Sprintf("[Unit]\nDescription=Approved standard validator %s\nDefaultDependencies=no\nAfter=network-online.target\n\n[Service]\nType=exec\nUser=%d\nGroup=%d\nWorkingDirectory=%s\nExecStart=%s %s\nRestart=no\nKillMode=control-group\nSendSIGKILL=yes\nTimeoutStartSec=30\nTimeoutStopSec=30\nUMask=0077\nNoNewPrivileges=yes\nDelegate=no\n\n[Install]\nWantedBy=multi-user.target\n", self.Name, self.Uid, self.Gid, self.StateDirectory, self.Binary.Path, self.arguments()))
}

// The sole admitted incident is missing/unavailable operational output from a
// previously observed exact producer. Integrity faults never authorize restart.
func (self repairValidatorPlan) incident(record monitorServiceCheckpointRecord) error {
	policy := monitorValidatorPolicy{Role: self.Role, ProgressFile: self.Unit.ProgressFile, ExpectedSource: self.Source}
	hash, err := hashMonitorServiceCheckpoint(record)
	previousSchema := record.Schema == "urnetwork-mainnet-validator-checkpoint-v3" && record.State.SteeringLiveness == nil
	if err != nil || record.Schema != monitorServiceCheckpointSchema && !previousSchema || record.Role != self.Role || record.Expected != self.Source || hash != record.ContentHash || validateMonitorValidatorState(record.State) != nil || record.State.SteeringLiveness.validate(policy, record.State.HighWaterAt) != nil {
		return errors.New("validator repair incident checkpoint differs")
	}
	history, previous := record.State.ReadIncidents, record.State.Record
	if history == nil || history.validate(policy, &record.State) != nil || history.LastIncident == nil || history.LastIncident.Id != self.IncidentId || history.LastIncident.Recovery != nil || previous == nil || previous.Source != self.Source || record.State.ReadStatus != "missing" && record.State.ReadStatus != "unavailable" {
		return errors.New("validator repair requires its original open availability incident")
	}
	return nil
}

// Every limit is finite, signed and specific to this one stopped generation.
func (self repairValidatorPlan) validate() error {
	if err := validateUnitDurableReference(self.Unit.DurableVolumes); err != nil {
		return err
	}
	if err := self.validateProfile(); err != nil {
		return err
	}
	return self.incident(self.Original)
}

// Shared fixed-unit admission does not select an incident or grant an action.
func (self repairValidatorPlan) validateProfile() error {
	u := self.Unit
	if !monitorRolePattern.MatchString(self.Role) || u.Name != "sn-mainnet-validator-"+self.Role+".service" || filepath.Base(u.File.Path) != u.Name || filepath.Dir(u.ProgressFile) != u.StateDirectory || u.Uid == 0 || u.Gid == 0 || self.Source.ChainId != mainnetEvmChainId || self.Source.Netuid == 0 || !repairValidatorHex(self.MachineId, 16) || len(self.BootId) != 36 || self.BootId[8] != '-' || self.BootId[13] != '-' || self.BootId[18] != '-' || self.BootId[23] != '-' || !repairValidatorHex(strings.ReplaceAll(self.BootId, "-", ""), 16) || !repairValidatorHex(self.Previous.InvocationId, 16) || self.Previous.Pid <= 1 || self.Previous.StartedUsec == 0 || !validMonitorReadDigest(self.IncidentId) {
		return errors.New("validator repair host, role or prior generation is incomplete")
	}
	paths := map[string]bool{}
	for _, path := range []string{u.File.Path, u.Binary.Path, u.Config.Path, u.StateDirectory, u.ProgressFile, self.Systemctl.Path, self.MonitorCheckpoint, self.StatePath, self.StatePath + ".lock"} {
		if !repairValidatorPath(path) || paths[path] {
			return errors.New("validator repair paths must be explicit and separate")
		}
		paths[path] = true
	}
	for _, file := range []planFileReference{u.File, u.Binary, u.Config, self.Systemctl} {
		if !validMonitorReadDigest(file.Sha256) {
			return errors.New("validator repair release pin is incomplete")
		}
	}
	if monitorReadDigest(u.render()) != u.File.Sha256 || self.MaximumStarts != 1 || self.MaximumObservations == 0 || self.MaximumObservations > 1024 || self.CommandTimeoutSeconds == 0 || self.CommandTimeoutSeconds > 60 || self.MaximumSampleAgeSeconds == 0 || self.MaximumSampleAgeSeconds > 600 || self.ValidFrom.IsZero() || !self.ExpiresAt.After(self.ValidFrom) || self.ExpiresAt.Sub(self.ValidFrom) > 24*time.Hour {
		return errors.New("validator repair unit profile, window or limits differ")
	}
	if len(self.RequiredMounts) > 8 {
		return errors.New("validator repair mount census exceeds its bound")
	}
	previous := ""
	for _, mount := range self.RequiredMounts {
		if len(mount) > 128 || !repairValidatorMountPattern.MatchString(mount) || mount <= previous {
			return errors.New("validator repair mount census is not canonical")
		}
		previous = mount
	}
	return nil
}

// Domain separation prevents another signed release/config from granting starts.
func (self repairValidatorApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(repairValidatorApprovalDomain+"\x00"), raw...), err
}

// Expiry gates fresh effects; retained receipts remain verifiable afterwards.
func (self repairValidatorApproval) validate(publicKey string) error {
	if self.Schema != repairValidatorSchema || !rootCanonicalHash(publicKey) {
		return errors.New("validator repair approval schema or independent key differs")
	}
	if err := self.Plan.validate(); err != nil {
		return err
	}
	key, _ := hex.DecodeString(publicKey[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	message, encodeErr := self.signingBytes()
	if err != nil || encodeErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("validator repair independent signature differs")
	}
	return nil
}

// The original command text is unchanged when the signed field is absent.
func (self repairValidatorUnit) arguments() string {
	return "run --config=" + self.Config.Path + " --progress-file=" + self.ProgressFile + unitDurableArguments(self.DurableVolumes)
}

// Passive root hosting has separate signed process authority. It neither
// borrows the two UR starts nor introduces a native signing capability.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const rootPassiveHostSchema = "urnetwork-mainnet-root-passive-host-v1"
const rootPassiveHostApprovalDomain = "urnetwork-mainnet-root-passive-host-approval-v1"
const rootPassiveHostUnitName = "sn-mainnet-root-passive.service"

type rootPassiveHostPlan struct {
	Preparation              planFileReference `json:"bootstrap_config"`
	PlanHash                 string            `json:"bootstrap_plan_hash"`
	Runtime                  planFileReference `json:"runtime_config"`
	RootPlanHash             string            `json:"root_plan_hash"`
	Unit                     planFileReference `json:"unit_file"`
	Binary                   planFileReference `json:"mainnet_binary"`
	Systemctl                planFileReference `json:"systemctl"`
	MachineId                string            `json:"machine_id"`
	BootId                   string            `json:"boot_id"`
	CheckpointDirectory      string            `json:"checkpoint_directory"`
	RequiredMounts           []string          `json:"required_mounts"`
	StatePath                string            `json:"state_path"`
	ValidFrom                time.Time         `json:"valid_from"`
	ExpiresAt                time.Time         `json:"expires_at"`
	MaximumOperations        uint32            `json:"maximum_operations"`
	CommandTimeoutSeconds    uint32            `json:"command_timeout_seconds"`
	MaximumSampleAgeSeconds  uint32            `json:"maximum_sample_age_seconds"`
	InstallStaticUnit        bool              `json:"install_static_unit"`
	AuthorizeOnePassiveStart bool              `json:"authorize_one_passive_start"`
}

type rootPassiveHostApproval struct {
	Schema    string              `json:"schema"`
	Plan      rootPassiveHostPlan `json:"plan"`
	Signature string              `json:"signature_ed25519"`
}

func (self rootPassiveHostApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(rootPassiveHostApprovalDomain+"\x00"), raw...), err
}

func (self rootPassiveHostApproval) validate(key string) error {
	p := self.Plan
	if self.Schema != rootPassiveHostSchema || !rootCanonicalHash(key) || !p.InstallStaticUnit || !p.AuthorizeOnePassiveStart ||
		!planSha256(p.PlanHash) || !planSha256(p.RootPlanHash) || !repairValidatorHex(p.MachineId, 16) ||
		len(p.BootId) != 36 || p.BootId[8] != '-' || p.BootId[13] != '-' || p.BootId[18] != '-' || p.BootId[23] != '-' || !repairValidatorHex(strings.ReplaceAll(p.BootId, "-", ""), 16) ||
		p.ValidFrom.IsZero() || !p.ExpiresAt.After(p.ValidFrom) || p.ExpiresAt.Sub(p.ValidFrom) > 24*time.Hour || p.MaximumOperations == 0 || p.MaximumOperations > 128 ||
		p.CommandTimeoutSeconds == 0 || p.CommandTimeoutSeconds > 60 || p.MaximumSampleAgeSeconds == 0 || p.MaximumSampleAgeSeconds > 600 || filepath.Base(p.Unit.Path) != rootPassiveHostUnitName {
		return errors.New("passive host requires independent bounded initial-start authority and exact host identity")
	}
	paths := map[string]bool{}
	for _, path := range []string{p.Preparation.Path, p.Runtime.Path, p.Unit.Path, p.Unit.Path + ".sn-control.lock", p.Binary.Path, p.Systemctl.Path, p.CheckpointDirectory, p.StatePath, p.StatePath + ".lock"} {
		if !repairValidatorPath(path) || paths[path] {
			return errors.New("passive host paths overlap or are not canonical")
		}
		paths[path] = true
	}
	for _, ref := range []planFileReference{p.Preparation, p.Runtime, p.Unit, p.Binary, p.Systemctl} {
		if !planSha256(ref.Sha256) {
			return errors.New("passive host requires every exact source hash")
		}
	}
	if p.Unit.Sha256 != monitorReadDigest(p.render()) {
		return errors.New("passive host unit differs from the fixed sandboxed profile")
	}
	previous := ""
	if len(p.RequiredMounts) > 8 {
		return errors.New("passive host mount census exceeds its bound")
	}
	for _, mount := range p.RequiredMounts {
		if len(mount) > 128 || !repairValidatorMountPattern.MatchString(mount) || mount <= previous {
			return errors.New("passive host mount census is not canonical")
		}
		previous = mount
	}
	raw, err := self.signingBytes()
	signature, signatureErr := hex.DecodeString(self.Signature)
	public, _ := hex.DecodeString(key[2:])
	if err != nil || signatureErr != nil || hex.EncodeToString(signature) != self.Signature || len(signature) != ed25519.SignatureSize || !ed25519.Verify(public, raw, signature) {
		return errors.New("passive host independent signature differs")
	}
	return nil
}

func (self rootPassiveHostPlan) arguments() string {
	return "root-passive-service run --config=" + self.Runtime.Path + " --accept-runtime-sha256=" + self.Runtime.Sha256
}

// Root can read the original private approvals. The observer receives no
// capabilities, write access to authority, restart policy or native action flags.
func (self rootPassiveHostPlan) render() []byte {
	return []byte(fmt.Sprintf("[Unit]\nDescription=Approved passive netuid-0 observer\nDefaultDependencies=no\nAfter=network-online.target\n\n[Service]\nType=exec\nUser=0\nGroup=0\nWorkingDirectory=%s\nExecStart=%s %s\nRestart=no\nKillMode=control-group\nSendSIGKILL=yes\nTimeoutStartSec=30\nTimeoutStopSec=30\nUMask=0077\nNoNewPrivileges=yes\nDelegate=no\nCapabilityBoundingSet=\nProtectSystem=strict\nReadWritePaths=%s\nPrivateDevices=yes\nProtectKernelTunables=yes\nProtectKernelModules=yes\nProtectControlGroups=yes\nRestrictSUIDSGID=yes\nLockPersonality=yes\n", self.CheckpointDirectory, self.Binary.Path, self.arguments(), self.CheckpointDirectory))
}

func (self rootPassiveHostPlan) sandbox() map[string]string {
	return map[string]string{"NoNewPrivileges": "yes", "UMask": "0077", "CapabilityBoundingSet": "", "ProtectSystem": "strict", "ReadWritePaths": self.CheckpointDirectory, "PrivateDevices": "yes", "ProtectKernelTunables": "yes", "ProtectKernelModules": "yes", "ProtectControlGroups": "yes", "RestrictSUIDSGID": "yes", "LockPersonality": "yes"}
}

// Reopen the original v4 composition, both UR inspections and exact passive
// role signature. No runtime copy or current observation can replace these.
func loadRootPassiveHostPreparation(ctx context.Context, approval rootPassiveHostApproval) (bootstrapChainPreparation, error) {
	p := approval.Plan
	preparation, err := loadBootstrapChainPreparation(ctx, p.Preparation.Path)
	if err != nil {
		return preparation, err
	}
	if preparation.Plan.Config.Schema != bootstrapChainConfigSchemaV4 || preparation.Plan.ConfigSha256 != p.Preparation.Sha256 || preparation.Plan.ContentHash != p.PlanHash || preparation.Root.ContentHash != p.RootPlanHash || preparation.Root.PassiveService == nil {
		return preparation, errors.New("passive host original v4 preparation differs")
	}
	root, digest, err := loadRootPassiveRuntime(ctx, p.Runtime.Path)
	if err != nil || digest != p.Runtime.Sha256 || root.ContentHash != preparation.Root.ContentHash {
		return preparation, errors.Join(errors.New("passive host exact runtime differs from preparation"), err)
	}
	raw, _, err := readBootstrapRootFile(ctx, p.Runtime.Path, rootServiceStoreLimit)
	var runtime rootPassiveRuntimeConfig
	if err != nil || decodePlanJson(raw, &runtime) != nil || rootObjectHash(runtime.Role) != rootObjectHash(*preparation.Plan.Config.RootValidator) || runtime.Root != preparation.Plan.Config.Root {
		return preparation, errors.New("passive host runtime role authority differs")
	}
	if filepath.Dir(root.PassiveService.CheckpointPath) != p.CheckpointDirectory {
		return preparation, errors.New("passive host checkpoint directory differs")
	}
	protected := []string{p.Preparation.Path, p.Runtime.Path, p.Binary.Path, p.Systemctl.Path, root.ConfigPath, root.ServiceInput.Path, runtime.Role.Approval.Path}
	// Include every original input and journal, not only the passive child's.
	protected = append(protected, preparation.childPaths()...)
	c := preparation.Plan.Config
	protected = append(protected, c.OwnerTrimPolicy.Path, c.OwnerTrimPlan.Path, c.Contracts.Path, preparation.Contracts.Config.Plan.Artifacts.Path)
	for _, role := range c.Validators {
		protected = append(protected, role.Config.Path)
	}
	for _, inspection := range preparation.Plan.ValidatorInspections {
		protected = append(protected, inspection.DeclaredPaths...)
		protected = append(protected, inspection.ApprovalReference.Path)
	}
	for _, path := range protected {
		for _, effect := range []string{p.Unit.Path, p.Unit.Path + ".sn-control.lock", p.StatePath, p.StatePath + ".lock", p.CheckpointDirectory} {
			if rootPassiveHostPathContains(effect, path) || rootPassiveHostPathContains(path, effect) || effect == path+".lock" {
				return preparation, errors.New("passive host effect path overlaps authority or preparation")
			}
		}
	}
	for _, path := range []string{p.Unit.Path, p.Unit.Path + ".sn-control.lock", p.StatePath, p.StatePath + ".lock"} {
		if rootPassiveHostPathContains(p.CheckpointDirectory, path) {
			return preparation, errors.New("passive host writable checkpoint directory contains host custody")
		}
	}
	return preparation, nil
}

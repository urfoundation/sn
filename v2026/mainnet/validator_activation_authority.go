// An independently signed bootstrap activation grants two initial process
// starts. It cannot replace either producer's signed protocol authority.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

const validatorActivationSchema = "urnetwork-mainnet-validator-activation-v1"
const validatorActivationApprovalDomain = "urnetwork-mainnet-validator-activation-approval-v1"

// Source is checked against the existing independently approved producer hash,
// not the file hash or an identity self-reported by a running process.
type validatorActivationUnit struct {
	Role   string                           `json:"role"`
	Unit   repairValidatorUnit              `json:"unit"`
	Source protocol.ValidatorProgressSource `json:"source"`
}

// The first-start scope is explicit. Each unit has one lifetime allowance;
// neither a new boot nor missing state renews it. Reboots require disposition.
type validatorActivationPlan struct {
	Preparation                  planFileReference         `json:"bootstrap_config"`
	PlanHash                     string                    `json:"bootstrap_plan_hash"`
	MachineId                    string                    `json:"machine_id"`
	BootId                       string                    `json:"boot_id"`
	Units                        []validatorActivationUnit `json:"units"`
	Systemctl                    planFileReference         `json:"systemctl"`
	RequiredMounts               []string                  `json:"required_mounts"`
	Route                        ownedSubmissionRoute      `json:"read_only_owned_route"`
	StatePath                    string                    `json:"state_path"`
	ValidFrom                    time.Time                 `json:"valid_from"`
	ExpiresAt                    time.Time                 `json:"expires_at"`
	MaximumOperations            uint32                    `json:"maximum_operations"`
	CommandTimeoutSeconds        uint32                    `json:"command_timeout_seconds"`
	MaximumSampleAgeSeconds      uint32                    `json:"maximum_sample_age_seconds"`
	InstallStaticUnits           bool                      `json:"install_static_units"`
	AuthorizeProductionValidator bool                      `json:"authorize_production_validator_run"`
}

// The approver is supplied independently to every invocation, never selected
// by the envelope or its retained journal. This is not a native signing key.
type validatorActivationApproval struct {
	Schema    string                  `json:"schema"`
	Plan      validatorActivationPlan `json:"plan"`
	Signature string                  `json:"signature_ed25519"`
}

func (self validatorActivationApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(validatorActivationApprovalDomain+"\x00"), raw...), err
}

// Unit grammar is exactly the previously qualified stopped-validator profile.
// A different unit name, command, shell, daemon or implicit restart is refused.
func (self validatorActivationApproval) validate(publicKey string) error {
	p := self.Plan
	if self.Schema != validatorActivationSchema || !rootCanonicalHash(publicKey) || !p.InstallStaticUnits || !p.AuthorizeProductionValidator ||
		len(p.Units) != 2 || !planSha256(p.PlanHash) || !repairValidatorHex(p.MachineId, 16) ||
		len(p.BootId) != 36 || p.BootId[8] != '-' || p.BootId[13] != '-' || p.BootId[18] != '-' || p.BootId[23] != '-' || !repairValidatorHex(strings.ReplaceAll(p.BootId, "-", ""), 16) ||
		p.ValidFrom.IsZero() || !p.ExpiresAt.After(p.ValidFrom) || p.ExpiresAt.Sub(p.ValidFrom) > 24*time.Hour ||
		p.MaximumOperations == 0 || p.MaximumOperations > 128 || p.CommandTimeoutSeconds == 0 || p.CommandTimeoutSeconds > 60 || p.MaximumSampleAgeSeconds == 0 || p.MaximumSampleAgeSeconds > 600 ||
		p.Route.ReadRetrySeconds < 60 || p.Route.ReadRetrySeconds > 900 || p.Route.SendTimeoutSeconds != 0 {
		return errors.New("validator activation requires explicit initial two-unit authority, host, window and finite limits")
	}
	if err := p.Route.validate(); err != nil {
		return err
	}
	if len(p.RequiredMounts) > 8 {
		return errors.New("validator activation mount census exceeds its bound")
	}
	previous := ""
	for _, mount := range p.RequiredMounts {
		if len(mount) > 128 || !repairValidatorMountPattern.MatchString(mount) || mount <= previous {
			return errors.New("validator activation mounts are not canonical")
		}
		previous = mount
	}
	paths := map[string]bool{}
	for _, path := range []string{p.Preparation.Path, p.Systemctl.Path, p.StatePath, p.StatePath + ".lock"} {
		if !repairValidatorPath(path) || paths[path] {
			return errors.New("validator activation custody paths overlap or are invalid")
		}
		paths[path] = true
	}
	for _, file := range []planFileReference{p.Preparation, p.Systemctl} {
		if !planSha256(file.Sha256) {
			return errors.New("validator activation source pin is absent")
		}
	}
	for i, item := range p.Units {
		u := item.Unit
		if err := validateUnitDurableReference(u.DurableVolumes); err != nil {
			return err
		}
		if item.Role != []string{"majority", "secondary"}[i] || u.Name != "sn-mainnet-validator-"+item.Role+".service" || filepath.Base(u.File.Path) != u.Name ||
			u.Uid == 0 || u.Gid == 0 || filepath.Dir(u.ProgressFile) != u.StateDirectory || !strings.HasSuffix(u.ProgressFile, ".json") || monitorReadDigest(u.render()) != u.File.Sha256 ||
			item.Source.ChainId != mainnetEvmChainId || item.Source.Netuid != 25 || item.Source.ValidatorId == 0 || item.Source.DeploymentId == "" || !rootCanonicalHash(item.Source.GenesisHash) || !planSha256(item.Source.ConfigHash) {
			return errors.New("validator activation unit or independent source differs")
		}
		for _, path := range []string{u.File.Path, u.Config.Path, u.StateDirectory, u.ProgressFile} {
			if !repairValidatorPath(path) || paths[path] {
				return errors.New("validator activation unit paths overlap or are invalid")
			}
			paths[path] = true
		}
		for _, ref := range []planFileReference{u.Binary, u.Config} {
			if !repairValidatorPath(ref.Path) || !planSha256(ref.Sha256) {
				return errors.New("validator activation binary/config pin is absent")
			}
		}
	}
	if p.Units[0].Unit.Binary != p.Units[1].Unit.Binary || paths[p.Units[0].Unit.Binary.Path] {
		return errors.New("validator activation requires one separate exact standard validator release")
	}
	key, _ := hex.DecodeString(publicKey[2:])
	signature, err := rootOfflineSignatureBytes(self.Signature)
	message, encodeErr := self.signingBytes()
	if err != nil || encodeErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("validator activation independent signature differs")
	}
	return nil
}

// Reload all original preparation inputs and producer approvals. No retained
// envelope can substitute for a changed or missing independent source.
func loadValidatorActivationPreparation(ctx context.Context, approval validatorActivationApproval) (bootstrapChainPreparation, error) {
	p := approval.Plan
	preparation, err := loadBootstrapChainPreparation(ctx, p.Preparation.Path)
	if err != nil {
		return preparation, err
	}
	c := preparation.Plan.Config
	if !bootstrapChainHasRootRole(c.Schema) || c.Netuid != 25 || preparation.Plan.ContentHash != p.PlanHash || preparation.Plan.ConfigSha256 != p.Preparation.Sha256 {
		return preparation, errors.New("validator activation differs from original signed v3 bootstrap admission")
	}
	protected := preparation.protectedPaths()
	for _, inspection := range preparation.Plan.ValidatorInspections {
		protected = append(protected, inspection.DeclaredPaths...)
		protected = append(protected, inspection.ApprovalReference.Path)
	}
	for _, role := range c.Validators {
		protected = append(protected, role.Config.Path)
	}
	for i, item := range p.Units {
		role, inspection := c.Validators[i], preparation.Plan.ValidatorInspections[i]
		expected := protocol.ValidatorProgressSource{ConfigHash: "sha256:" + hex.EncodeToString(inspection.Approval.ConfigHash[:]), DeploymentId: c.DeploymentId,
			ValidatorId: role.ValidatorId, ChainId: c.Network.EvmChainId, GenesisHash: c.Network.GenesisHash, Netuid: c.Netuid}
		if item.Role != role.Role || item.Unit.Config.Path == role.Config.Path || item.Unit.Config.Sha256 != role.Config.Sha256 || item.Source != expected || len(inspection.DeclaredPaths) == 0 {
			return preparation, errors.New("validator activation unit differs from its signed producer config, role, generation or state path")
		}
		// Identical bytes at a different path must retain identical production
		// interpretation. The real parser normalizes every declared path and
		// checks the signed semantic config hash at the runtime copy location.
		raw, err := readBootstrapChainInput(ctx, role.Config, 2*1024*1024)
		if err != nil {
			return preparation, err
		}
		copied, err := validator.InspectProductionBootstrapConfig(ctx, item.Unit.Config.Path, raw)
		if err != nil || copied == nil || rootObjectHash(copied) != rootObjectHash(inspection) {
			return preparation, errors.Join(errors.New("validator activation runtime copy changes production path semantics"), err)
		}
		for _, path := range protected {
			for _, reserved := range []string{p.StatePath, p.StatePath + ".lock", item.Unit.File.Path, item.Unit.Binary.Path, item.Unit.Config.Path, p.Systemctl.Path, item.Unit.StateDirectory, item.Unit.ProgressFile} {
				// The actual producer refuses progress inside protocol state.
				// Working/output directories must be separate operational custody.
				if reserved == path || strings.HasPrefix(reserved, path+"/") || strings.HasPrefix(path, reserved+"/") {
					return preparation, errors.New("validator activation effect path overlaps original custody")
				}
			}
		}
	}
	return preparation, ctx.Err()
}

// The host reuses only the qualified unit, cgroup, process and progress checks;
// a zero prior generation admits initial start, never repair of a used unit.
func (self validatorActivationPlan) hostPlan(index int) repairValidatorPlan {
	item := self.Units[index]
	return repairValidatorPlan{Role: item.Role, Unit: item.Unit, Source: item.Source, MachineId: self.MachineId, BootId: self.BootId,
		Systemctl: self.Systemctl, RequiredMounts: self.RequiredMounts, CommandTimeoutSeconds: self.CommandTimeoutSeconds, MaximumSampleAgeSeconds: self.MaximumSampleAgeSeconds,
		Original: monitorServiceCheckpointRecord{State: monitorValidatorState{Record: &protocol.ValidatorProgress{}}}}
}

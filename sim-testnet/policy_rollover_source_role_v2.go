//go:build linux || darwin

package main

// The activation receipt and original generation configs remain immutable. A
// separately approved owner-signed overlay adds only the retained native
// source's custody proof. It imports no intents, EMA, policy, or ledger state.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
	"gopkg.in/yaml.v3"
)

const policyRolloverSourceRoleSchemaV2 = "urnetwork-sim-policy-rollover-source-role-v2"
const policyRolloverSourceRoleKindV2 = "policy-rollover-source-role"

type policyRolloverSourceRoleValidatorV2 struct {
	ValidatorID      uint64                                    `json:"validator_id"`
	PreviousStateDir string                                    `json:"previous_state_dir"`
	OriginalConfig   validatorcomponent.ReleaseEvidenceV2File  `json:"original_config"`
	Config           validatorcomponent.ReleaseEvidenceV2File  `json:"config"`
	SourceIntents    *validatorcomponent.ReleaseEvidenceV2File `json:"source_intents,omitempty"`
	Predecessor      *validatorcomponent.ReleaseEvidenceV2File `json:"source_role_predecessor_v2,omitempty"`
}

type policyRolloverSourceRolePlanV2 struct {
	Schema                  string                                `json:"schema"`
	PlanHash                string                                `json:"plan_hash"`
	SourcePlanHash          string                                `json:"source_plan_hash"`
	RolloverPlanHash        string                                `json:"rollover_plan_hash"`
	OriginalHandoffSHA256   string                                `json:"original_handoff_sha256"`
	DeploymentID            string                                `json:"deployment_id"`
	StateDir                string                                `json:"state_dir"`
	ConfigHash              string                                `json:"config_hash"`
	PolicyHash              string                                `json:"policy_hash"`
	Generation              uint64                                `json:"generation"`
	RoleOnly                bool                                  `json:"role_only"`
	Provisional             bool                                  `json:"provisional"`
	FinalAcceptance         bool                                  `json:"final_acceptance"`
	LedgerContinuityClaimed bool                                  `json:"ledger_continuity_claimed"`
	StateImported           bool                                  `json:"state_imported"`
	Validators              []policyRolloverSourceRoleValidatorV2 `json:"validators"`
}

type policyRolloverSourceRoleIOV2 struct {
	prepare func(context.Context, *validatorcomponent.ReleaseConfig, string) ([]byte, error)
	decode  func(context.Context, *validatorcomponent.ReleaseConfig, []byte) (*validatorcomponent.ReleaseSourceRolePredecessorV2, error)
}

func policyRolloverSourceRoleIO() policyRolloverSourceRoleIOV2 {
	return policyRolloverSourceRoleIOV2{validatorcomponent.PrepareReleaseSourceRolePredecessorV2, validatorcomponent.DecodeReleaseSourceRolePredecessorV2}
}

func policyRolloverSourceRoleRootV2(stateDir string, generation uint64) string {
	return filepath.Join(policyRolloverRoot(stateDir), "source-role", fmt.Sprintf("generation-%020d", generation))
}

func (p *policyRolloverSourceRolePlanV2) hash() (string, error) {
	owned := *p
	owned.PlanHash = ""
	return canonicalHashHex(owned)
}

func policyRolloverSourceRoleConfigV2(ctx context.Context, reference validatorcomponent.ReleaseEvidenceV2File, limit uint64) (*validatorcomponent.ReleaseConfig, error) {
	raw, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, reference, limit)
	if err != nil {
		return nil, err
	}
	var config validatorcomponent.ReleaseConfig
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.Join(errors.New("source role config has trailing YAML"), err)
	}
	if err := validatorcomponent.ValidateReleaseEvidenceV2ConfigYAML(raw); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, ctx.Err()
}

// Preparation owns only new immutable files. In particular it cannot publish
// the selecting receipt or take the deployment writer while a campaign runs.
func preparePolicyRolloverSourceRoleV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, h *policyRolloverHandoffV2, io policyRolloverSourceRoleIOV2) (*policyRolloverSourceRolePlanV2, error) {
	if h == nil || base == nil || len(h.Validators) != 2 || io.prepare == nil || io.decode == nil {
		return nil, errors.New("source role planning requires an authenticated active generation")
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	root := policyRolloverSourceRoleRootV2(stateDir, h.Generation)
	var retained policyRolloverSourceRolePlanV2
	if _, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(root, "plan.json"), limit, &retained); err == nil {
		return &retained, validatePolicyRolloverSourceRoleV2(ctx, cfg, stateDir, base, h, &retained, io)
	} else if !validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return nil, err
	}
	p := &policyRolloverSourceRolePlanV2{Schema: policyRolloverSourceRoleSchemaV2, SourcePlanHash: base.PlanHash, RolloverPlanHash: h.PlanHash, OriginalHandoffSHA256: h.sourceSHA256, DeploymentID: base.DeploymentID,
		StateDir: stateDir, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Generation: h.Generation, RoleOnly: true, Provisional: true}
	for _, owner := range h.Validators {
		config, err := policyRolloverSourceRoleConfigV2(ctx, owner.Config, limit)
		if err != nil {
			return nil, err
		}
		member := policyRolloverSourceRoleValidatorV2{ValidatorID: owner.ValidatorID, PreviousStateDir: owner.PreviousStateDir, OriginalConfig: owner.Config, Config: owner.Config}
		intentPath := filepath.Join(owner.PreviousStateDir, "steering-intents.json")
		before, beforeErr := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, intentPath, config.EvidenceV2.Bounds.IntentFileLimit())
		if beforeErr != nil && !validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(beforeErr) {
			return nil, beforeErr
		}
		descriptor, err := io.prepare(ctx, config, owner.PreviousStateDir)
		if err != nil {
			return nil, err
		}
		if len(descriptor) != 0 {
			if beforeErr != nil {
				return nil, errors.New("source role predecessor appeared during capture")
			}
			source := policyRolloverFile(intentPath, before)
			if _, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, source, config.EvidenceV2.Bounds.IntentFileLimit()); err != nil {
				return nil, err
			}
			reference := policyRolloverFile(filepath.Join(root, fmt.Sprintf("validator-%d", owner.ValidatorID), "source-role-predecessor.json"), descriptor)
			if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, reference.Path, descriptor, validatorcomponent.ReleaseSourceRolePredecessorV2MaximumBytes); err != nil {
				return nil, err
			}
			config.SourceRolePredecessorV2 = &reference
			if err := config.Validate(); err != nil {
				return nil, err
			}
			raw, err := yaml.Marshal(config)
			if err != nil {
				return nil, err
			}
			member.Config = policyRolloverFile(filepath.Join(root, fmt.Sprintf("validator-%d", owner.ValidatorID), "validator.yml"), raw)
			member.SourceIntents, member.Predecessor = &source, &reference
			if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, member.Config.Path, raw, limit); err != nil {
				return nil, err
			}
		}
		p.Validators = append(p.Validators, member)
	}
	p.PlanHash, err = p.hash()
	if err != nil {
		return nil, err
	}
	if err := validatePolicyRolloverSourceRoleV2(ctx, cfg, stateDir, base, h, p, io); err != nil {
		return nil, err
	}
	if _, err := writeRuntimeEvidenceSetupV2(ctx, filepath.Join(root, "plan.json"), p, limit); err != nil {
		return nil, err
	}
	return p, ctx.Err()
}

// Approval binds both full configs, and the only permitted semantic difference
// is the explicit proof reference. Fresh ledger, policy, keys, bounds, replay
// history, and operator namespaces must remain byte-for-byte equivalent values.
func validatePolicyRolloverSourceRoleV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, h *policyRolloverHandoffV2, p *policyRolloverSourceRolePlanV2, io policyRolloverSourceRoleIOV2) error {
	if cfg == nil || base == nil || h == nil || p == nil || io.decode == nil {
		return errors.New("source role approval owner is absent")
	}
	hash, err := p.hash()
	if err != nil || hash != p.PlanHash || p.Schema != policyRolloverSourceRoleSchemaV2 || p.SourcePlanHash != base.PlanHash || p.RolloverPlanHash != h.PlanHash || p.OriginalHandoffSHA256 != h.sourceSHA256 || !validSHA256ContentHash(p.OriginalHandoffSHA256) || p.DeploymentID != base.DeploymentID || p.StateDir != stateDir || p.ConfigHash != cfg.ConfigHash || p.PolicyHash != cfg.PolicyHash || p.Generation != h.Generation || !p.RoleOnly || !p.Provisional || p.FinalAcceptance || p.LedgerContinuityClaimed || p.StateImported || len(p.Validators) != len(h.Validators) || len(p.Validators) != 2 {
		return errors.Join(errors.New("source role plan differs from its exact generation and deployment approval"), err)
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return err
	}
	count := 0
	for index, member := range p.Validators {
		owner := h.Validators[index]
		if member.ValidatorID != owner.ValidatorID || member.PreviousStateDir != owner.PreviousStateDir || member.OriginalConfig != owner.Config {
			return errors.New("source role plan changed the predecessor or original generation owner")
		}
		original, err := policyRolloverSourceRoleConfigV2(ctx, owner.Config, limit)
		if err != nil {
			return err
		}
		if original.SourceRolePredecessorV2 != nil {
			return errors.New("source role original config already contains another predecessor")
		}
		if member.Predecessor == nil {
			if member.SourceIntents != nil || member.Config != owner.Config {
				return errors.New("source role empty predecessor changed its config")
			}
			continue
		}
		count++
		root := filepath.Join(policyRolloverSourceRoleRootV2(stateDir, h.Generation), fmt.Sprintf("validator-%d", owner.ValidatorID))
		if member.Config.Path != filepath.Join(root, "validator.yml") || member.Predecessor.Path != filepath.Join(root, "source-role-predecessor.json") || member.SourceIntents == nil || member.SourceIntents.Path != filepath.Join(owner.PreviousStateDir, "steering-intents.json") {
			return errors.New("source role config or predecessor reference escaped its approved namespace")
		}
		if _, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, *member.SourceIntents, original.EvidenceV2.Bounds.IntentFileLimit()); err != nil {
			return err
		}
		selected, err := policyRolloverSourceRoleConfigV2(ctx, member.Config, limit)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(selected.SourceRolePredecessorV2, member.Predecessor) {
			return errors.New("source role selected config differs from its exact proof reference")
		}
		selected.SourceRolePredecessorV2 = nil
		if !reflect.DeepEqual(selected, original) {
			return errors.New("source role overlay changed fields outside the predecessor reference")
		}
		raw, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, *member.Predecessor, validatorcomponent.ReleaseSourceRolePredecessorV2MaximumBytes)
		if err != nil {
			return err
		}
		proof, err := io.decode(ctx, original, raw)
		if err != nil {
			return err
		}
		if proof == nil {
			return errors.New("source role descriptor decoded without a predecessor")
		}
		measurement := filepath.Join("measurements", strings.TrimPrefix(proof.Intent.MeasurementArtifactHash, "sha256:")+".json")
		envelope := filepath.Join("measurements", "envelopes", strings.TrimPrefix(proof.Intent.MeasurementEnvelopeHash, "sha256:")+".json")
		if !validSHA256ContentHash(proof.Intent.MeasurementArtifactHash) || !validSHA256ContentHash(proof.Intent.MeasurementEnvelopeHash) || proof.Intent.MeasurementArtifactPath != filepath.ToSlash(measurement) || proof.Intent.MeasurementEnvelopePath != filepath.ToSlash(envelope) || proof.Measurement.Path != filepath.Join(owner.PreviousStateDir, measurement) || proof.Envelope.Path != filepath.Join(owner.PreviousStateDir, envelope) {
			return errors.New("source role proof selects files outside its immutable predecessor")
		}
	}
	if count == 0 {
		return errors.New("source role plan has no retained predecessor to authenticate")
	}
	return ctx.Err()
}

func readPolicyRolloverSourceRoleOverlayV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, h *policyRolloverHandoffV2) (*policyRolloverHandoffV2, error) {
	return readPolicyRolloverSourceRoleOverlayWithV2(ctx, cfg, stateDir, base, h, policyRolloverSourceRoleIO())
}

func readPolicyRolloverSourceRoleOverlayWithV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, h *policyRolloverHandoffV2, io policyRolloverSourceRoleIOV2) (*policyRolloverHandoffV2, error) {
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	root := policyRolloverSourceRoleRootV2(stateDir, h.Generation)
	path := filepath.Join(root, "handoff.evidence.json")
	var signed ReleaseEvidenceEnvelope
	raw, err := readRuntimeEvidenceSetupV2(ctx, path, limit, &signed)
	if validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	if err := verifyEvidence(&signed, nil); err != nil {
		return nil, err
	}
	var p policyRolloverSourceRolePlanV2
	if err := decodeStrictJSONBytes(signed.Payload, &p); err != nil {
		return nil, err
	}
	if signed.Kind != policyRolloverSourceRoleKindV2 || signed.RunID != p.PlanHash || signed.Signer != common.HexToAddress(base.Roles.Owner) || signed.DeploymentID != base.DeploymentID || signed.ChainID != cfg.ChainID || signed.Netuid != cfg.Netuid || !strings.EqualFold(signed.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return nil, errors.New("source role handoff lacks the exact deployment owner's signed approval")
	}
	var reviewed policyRolloverSourceRolePlanV2
	if _, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(root, "plan.json"), limit, &reviewed); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(reviewed, p) {
		return nil, errors.New("source role signed handoff differs from the reviewed immutable plan")
	}
	source, err := retainedPolicyRolloverSourceV2(ctx, cfg, stateDir, base, p.SourcePlanHash)
	if err != nil {
		return nil, err
	}
	if err := validatePolicyRolloverSourceRoleV2(ctx, cfg, stateDir, source, h, &p, io); err != nil {
		return nil, err
	}
	selected := *h
	selected.Validators = append([]policyRolloverValidatorHandoffV2(nil), h.Validators...)
	for index, member := range p.Validators {
		selected.Validators[index].Config = member.Config
		selected.Validators[index].SourceRolePredecessorV2 = member.Predecessor
	}
	reference := policyRolloverFile(path, raw)
	selected.SourceRoleOverlay = &reference
	return &selected, ctx.Err()
}

// The caller owns the deployment's ordinary journal lock. This path creates
// no transaction manager and no source of native signing authority.
func runPolicyRolloverSourceRoleV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, roles *RoleSecrets, o cliOptions) error {
	h, err := readBasePolicyRolloverHandoffV2(ctx, cfg, stateDir, base)
	if err != nil {
		return err
	}
	if h == nil {
		return errors.New("source role continuation requires an active policy generation")
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return err
	}
	io := policyRolloverSourceRoleIO()
	var plan *policyRolloverSourceRolePlanV2
	if o.RolloverPlan == "" {
		plan, err = preparePolicyRolloverSourceRoleV2(ctx, cfg, stateDir, base, h, io)
	} else {
		plan = &policyRolloverSourceRolePlanV2{}
		_, err = readRuntimeEvidenceSetupV2(ctx, o.RolloverPlan, limit, plan)
		if err == nil {
			err = validatePolicyRolloverSourceRoleV2(ctx, cfg, stateDir, base, h, plan, io)
		}
	}
	if err != nil {
		return err
	}
	if !o.Apply {
		return printResult(o.Format, plan, nil)
	}
	if err := requireApproved(true, o.RolloverPlanHash, plan.PlanHash); err != nil {
		return err
	}
	operation, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var native *crv4.Chain
	defer func() {
		if native != nil {
			native.API.Client.Close()
		}
	}()
	selected, err := activatePolicyRolloverSourceRoleV2(operation, cfg, stateDir, base, roles, h, plan, io, func(ctx context.Context, config *validatorcomponent.ReleaseConfig) error {
		if native == nil {
			runtime, err := campaignRPCConfig(cfg)
			if err != nil {
				return err
			}
			native, _, err = dialReleaseSubstrateChainContext(ctx, runtime, runtime.OperationalSubstrate)
			if err != nil {
				return err
			}
		}
		hotkey, err := crv4.KeypairFromSeedHex(roles.Substrate[validatorHotkeyLabel(int(config.ValidatorID))].SeedHex)
		if err != nil {
			return err
		}
		return validatorcomponent.VerifyReleaseSourceRolePredecessorV2(ctx, config, native, hotkey.PublicKey())
	})
	if err != nil {
		return err
	}
	return printResult(o.Format, selected, nil)
}

// Only an independently verified current native slot can precede publication.
// The callback is a read-only chain boundary, never a signing or submit owner.
func activatePolicyRolloverSourceRoleV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, roles *RoleSecrets, h *policyRolloverHandoffV2, plan *policyRolloverSourceRolePlanV2, io policyRolloverSourceRoleIOV2, verify func(context.Context, *validatorcomponent.ReleaseConfig) error) (*policyRolloverHandoffV2, error) {
	if verify == nil || roles == nil {
		return nil, errors.New("source role activation requires explicit native verification and owner")
	}
	if err := validatePolicyRolloverSourceRoleV2(ctx, cfg, stateDir, base, h, plan, io); err != nil {
		return nil, err
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return nil, err
	}
	for _, owner := range h.Validators {
		if err := requireValidatorStateStopped(stateDir, int(owner.ValidatorID)); err != nil {
			return nil, err
		}
	}
	root := policyRolloverSourceRoleRootV2(stateDir, h.Generation)
	if _, err := writeRuntimeEvidenceSetupV2(ctx, filepath.Join(root, "plan.json"), plan, limit); err != nil {
		return nil, err
	}
	// An exact selected retry remains valid after normal subsequent writes;
	// the validator independently authenticates the retained historical proof.
	selected, err := readPolicyRolloverSourceRoleOverlayWithV2(ctx, cfg, stateDir, base, h, io)
	if err != nil {
		return nil, err
	}
	if selected.SourceRoleOverlay != nil {
		return selected, nil
	}
	for _, member := range plan.Validators {
		config, err := policyRolloverSourceRoleConfigV2(ctx, member.Config, limit)
		if err != nil {
			return nil, err
		}
		if err := verify(ctx, config); err != nil {
			return nil, fmt.Errorf("validator %d current native source role: %w", member.ValidatorID, err)
		}
	}
	if err := validatePolicyRolloverSourceRoleV2(ctx, cfg, stateDir, base, h, plan, io); err != nil {
		return nil, err
	}
	retained, err := readBasePolicyRolloverHandoffV2(ctx, cfg, stateDir, base)
	if err != nil || !reflect.DeepEqual(retained, h) {
		return nil, errors.Join(errors.New("source role original generation changed during native verification"), err)
	}
	signed, err := signEvidence(cfg, policyRolloverSourceRoleKindV2, plan.PlanHash, plan, roles.EVM["testnet-owner"])
	if err != nil {
		return nil, err
	}
	if signed.Signer != common.HexToAddress(base.Roles.Owner) {
		return nil, errors.New("source role signing key differs from the deployment owner")
	}
	if _, err := writeRuntimeEvidenceSetupV2(ctx, filepath.Join(root, "handoff.evidence.json"), signed, limit); err != nil {
		return nil, err
	}
	return readPolicyRolloverSourceRoleOverlayWithV2(ctx, cfg, stateDir, base, h, io)
}

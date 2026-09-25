//go:build linux || darwin

// A separately activated source adds a proof branch; it never replaces the
// original activation or borrows that source's namespace, policy or receipts.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	validatorpkg "github.com/urfoundation/sn/validator"

	"gopkg.in/yaml.v3"
)

// Only public, bounded immutable inputs live here. Large archived plans and
// the complete journal retain their independently bounded artifact owners.
type finalValidatorGenerationAuthorityV2 struct {
	FrozenConfigs   [][]byte `json:"frozen_configs"`
	FrozenInputs    [][]byte `json:"frozen_inputs"`
	Plan            []byte   `json:"plan_bytes"`
	Handoff         []byte   `json:"handoff_bytes"`
	Identities      []byte   `json:"identities_bytes"`
	Configs         [][]byte `json:"generation_configs"`
	SourceRole      []byte   `json:"source_role_bytes,omitempty"`
	SelectedConfigs [][]byte `json:"selected_configs"`
	Publications    [][]byte `json:"publication_receipts"`
}

// The manifest selects these independently from the submitted runtime YAML.
// A missing active source plan or journal is fatal even with an approved appendix.
type finalValidatorGenerationReplayV2 struct {
	source  *SetupPlan
	entries []JournalEntry
	read    func(string, uint64) ([]byte, error)
}

// Capture reuses the live read-only handoff authenticator before retaining its
// exact source bytes; the detached verifier repeats that authentication below.
func captureFinalValidatorGenerationAuthorityV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, current *SetupPlan) (*finalValidatorGenerationAuthorityV2, error) {
	if current.EvidenceRelayContinuation == nil || current.EvidenceRelayContinuation.ActiveGeneration == nil {
		return nil, nil
	}
	h, err := readPolicyRolloverSourceHandoffV2(ctx, cfg, stateRoot, current)
	if err != nil {
		return nil, err
	}
	if err := current.EvidenceRelayContinuation.ActiveGeneration.matchesHandoff(h); err != nil {
		return nil, err
	}
	return captureFinalValidatorGenerationInputsV2(ctx, cfg, stateRoot, h)
}

// Retain only public bytes after the handoff reader has selected its owner.
func captureFinalValidatorGenerationInputsV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, h *policyRolloverHandoffV2) (*finalValidatorGenerationAuthorityV2, error) {
	if h == nil || len(h.Validators) != 2 {
		return nil, errors.New("final generation source census differs")
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return nil, err
	}
	out := &finalValidatorGenerationAuthorityV2{}
	for id := 1; id <= 2; id++ {
		raw, err := validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(stateRoot, "runtime", fmt.Sprintf("validator-%d", id), "validator.yml"), limit)
		if err != nil {
			return nil, err
		}
		out.FrozenConfigs = append(out.FrozenConfigs, raw)
		config, err := finalGenerationConfigV2(raw)
		if err != nil {
			return nil, err
		}
		for _, operator := range config.EvidenceV2.Operators {
			for index, ref := range operator.Files() {
				raw, err := validatorpkg.ReadReleaseEvidenceV2File(ctx, ref, runtimeEvidenceV2ReferenceLimit(config.EvidenceV2.Bounds, index))
				if err != nil {
					return nil, err
				}
				out.FrozenInputs = append(out.FrozenInputs, raw)
			}
		}
	}
	out.Plan, err = validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, policyRolloverPlanPathV2(stateRoot, h.Generation, h.CutoffEpoch), limit)
	if err != nil {
		return nil, err
	}
	out.Handoff, err = validatorpkg.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(policyRolloverRoot(stateRoot), "handoff.json"), limit)
	if err != nil {
		return nil, err
	}
	var base policyRolloverHandoffV2
	if err := decodeStrictJSONBytes(out.Handoff, &base); err != nil {
		return nil, err
	}
	if len(base.Validators) != 2 || bytesSHA256(out.Handoff) != h.sourceSHA256 {
		return nil, errors.New("final generation handoff changed after authenticated selection")
	}
	out.Identities, err = validatorpkg.ReadReleaseEvidenceV2File(ctx, h.Identities, limit)
	if err != nil {
		return nil, err
	}
	for index, owner := range h.Validators {
		raw, err := validatorpkg.ReadReleaseEvidenceV2File(ctx, base.Validators[index].Config, limit)
		if err != nil {
			return nil, err
		}
		out.Configs = append(out.Configs, raw)
		raw, err = validatorpkg.ReadReleaseEvidenceV2File(ctx, owner.Config, limit)
		if err != nil {
			return nil, err
		}
		out.SelectedConfigs = append(out.SelectedConfigs, raw)
	}
	if h.SourceRoleOverlay != nil {
		out.SourceRole, err = validatorpkg.ReadReleaseEvidenceV2File(ctx, *h.SourceRoleOverlay, limit)
		if err != nil {
			return nil, err
		}
	}
	var plan policyRolloverPlanV2
	if err := decodeStrictJSONBytes(out.Plan, &plan); err != nil {
		return nil, err
	}
	entries, err := readJournalEntries(stateRoot)
	if err != nil {
		return nil, err
	}
	for _, action := range plan.Actions {
		_, verified := policyRolloverPriorV2(&plan, action, entries)
		if verified == nil {
			return nil, errors.New("final active generation lost its publication receipt")
		}
		raw, err := readValidatorEvidenceHistoricalFile(stateRoot, verified.PostconditionPath, int64(min(limit, maximumCampaignEvidenceRawFileBytes)))
		if err != nil {
			return nil, err
		}
		out.Publications = append(out.Publications, raw)
	}
	return out, ctx.Err()
}

// Predecessor descriptors and their signed payloads are separate archive
// sources, so a control envelope cannot smuggle an unbounded intent history.
func captureFinalActiveGenerationSourcesV2(ctx context.Context, cfg *ResolvedConfig, stateRoot string, validatorId uint64, retain func(context.Context, validatorpkg.ReleaseEvidenceV2CaptureSource, []byte) error) error {
	current, err := loadFinalCapturePlanV2(cfg, stateRoot)
	if err != nil {
		return err
	}
	if current.EvidenceRelayContinuation == nil || current.EvidenceRelayContinuation.ActiveGeneration == nil {
		return nil
	}
	h, err := readPolicyRolloverSourceHandoffV2(ctx, cfg, stateRoot, current)
	if err != nil {
		return err
	}
	if h == nil || validatorId == 0 || validatorId > uint64(len(h.Validators)) {
		return errors.New("final active generation source is absent")
	}
	if h.SourceRoleOverlay == nil {
		return nil
	}
	raw, err := validatorpkg.ReadReleaseEvidenceV2File(ctx, *h.SourceRoleOverlay, maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return err
	}
	var signed ReleaseEvidenceEnvelope
	var plan policyRolloverSourceRolePlanV2
	if err := decodeStrictJSONBytes(raw, &signed); err != nil {
		return err
	}
	if err := decodeStrictJSONBytes(signed.Payload, &plan); err != nil {
		return err
	}
	owner := plan.Validators[validatorId-1]
	if owner.Predecessor == nil {
		return nil
	}
	emit := func(name string, ref validatorpkg.ReleaseEvidenceV2File, maximum uint64) ([]byte, error) {
		raw, err := validatorpkg.ReadReleaseEvidenceV2File(ctx, ref, min(maximum, maximumCampaignEvidenceRawFileBytes))
		if err != nil {
			return nil, err
		}
		err = retain(ctx, validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "active-generation/" + name}, raw)
		return raw, err
	}
	if owner.SourceIntents == nil {
		return errors.New("final active source role lost its original intents")
	}
	if _, err := emit("source-intents", *owner.SourceIntents, h.Validators[validatorId-1].Evidence.Bounds.IntentFileLimit()); err != nil {
		return err
	}
	descriptor, err := emit("source-role", *owner.Predecessor, validatorpkg.ReleaseSourceRolePredecessorV2MaximumBytes)
	if err != nil {
		return err
	}
	var proof validatorpkg.ReleaseSourceRolePredecessorV2
	if err := decodeStrictJSONBytes(descriptor, &proof); err != nil {
		return err
	}
	if _, err := emit("source-measurement", proof.Measurement, h.Validators[validatorId-1].Evidence.Bounds.MaxArtifactBytes); err != nil {
		return err
	}
	_, err = emit("source-envelope", proof.Envelope, h.Validators[validatorId-1].Evidence.Bounds.MaxControlBytes)
	return err
}

// References never grant filesystem access. The caller supplies already
// captured bytes, and even jointly edited payloads must match the approved pin.
func finalGenerationPinnedBytesV2(ref validatorpkg.ReleaseEvidenceV2File, raw []byte, limit uint64) error {
	if !filepath.IsAbs(ref.Path) || filepath.Clean(ref.Path) != ref.Path || ref.Bytes == 0 || ref.Bytes > limit || ref.Bytes != uint64(len(raw)) || bytesSHA256(raw) != "sha256:"+strings.TrimPrefix(ref.SHA256, "0x") {
		return errors.New("final active generation bytes differ from their original file pin")
	}
	return nil
}

// Historical YAML is decoded without opening any of its private references.
func finalGenerationConfigV2(raw []byte) (*validatorpkg.ReleaseConfig, error) {
	if len(raw) == 0 || len(raw) > maximumCampaignEvidenceRawFileBytes {
		return nil, errors.New("final generation config exceeds its bound")
	}
	var config validatorpkg.ReleaseConfig
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("final generation config has trailing YAML")
	}
	if err := validatorpkg.ValidateReleaseEvidenceV2ConfigYAML(raw); err != nil {
		return nil, err
	}
	if err := config.ValidateHistorical(); err != nil {
		return nil, err
	}
	return &config, nil
}

// The active generation has its own dual consents and original journal owner.
// The approved appendix pins those bytes but is never a substitute for them.
func verifyFinalValidatorGenerationV2(ctx context.Context, cfg *ResolvedConfig, current, original *SetupPlan, authority *finalValidatorAuthorityV2, prepared *runtimeEvidenceActivationPreparedV2, runtimeBytes []byte, validatorId uint64, replay finalValidatorGenerationReplayV2) (*validatorpkg.ReleaseConfig, error) {
	if current.EvidenceRelayContinuation == nil || current.EvidenceRelayContinuation.ActiveGeneration == nil || authority.ActiveGeneration == nil || replay.source == nil || len(replay.entries) == 0 {
		return nil, errors.New("final active generation proof branch is incomplete")
	}
	generation, input := current.EvidenceRelayContinuation.ActiveGeneration, authority.ActiveGeneration
	source := replay.source
	if source.PlanHash != generation.SourcePlanHash || source.ConfigHash != current.ConfigHash || source.PolicyHash != current.PolicyHash || source.OwnedRPCAuthority != current.OwnedRPCAuthority {
		return nil, errors.New("final active generation source approval differs")
	}
	if err := validatorEvidenceSourcePlanMatches(current, source); err != nil {
		return nil, err
	}
	var plan policyRolloverPlanV2
	if err := decodeStrictJSONBytes(input.Plan, &plan); err != nil {
		return nil, err
	}
	if err := validatePolicyRolloverPlanV2(cfg, source, authority.StateRoot, &plan); err != nil {
		return nil, err
	}
	var h policyRolloverHandoffV2
	if err := decodeStrictJSONBytes(input.Handoff, &h); err != nil {
		return nil, err
	}
	if h.Schema != policyRolloverHandoffV2Schema || !h.Activated || h.LedgerContinuityClaimed || h.SourceRoleOverlay != nil || h.PlanHash != plan.PlanHash || h.SourcePlanHash != plan.SourcePlanHash || h.DeploymentID != plan.DeploymentID || h.Generation != plan.Generation || h.CutoffEpoch != plan.Epoch || h.FirstFullEpoch != plan.Epoch+1 || h.Native != plan.Native || h.EVM != plan.EVM || h.Boundary.Number <= plan.EVM.Number || !validCanonicalHashHex(h.Boundary.Hash) || !reflect.DeepEqual(h.Members, plan.Members) || len(h.Validators) != 2 || len(input.Configs) != 2 || len(input.SelectedConfigs) != 2 || len(input.Publications) != 4 {
		return nil, errors.New("final activated handoff differs from its signed generation")
	}
	h.sourceSHA256 = bytesSHA256(input.Handoff)
	checkpoint := false
	for _, entry := range replay.entries {
		checkpoint = checkpoint || entry.EntryHash == plan.SourceJournalHash
	}
	action, err := policyRolloverHandoffActionV2(&plan)
	if err != nil {
		return nil, err
	}
	_, verified := policyRolloverPriorV2(&plan, action, replay.entries)
	if !checkpoint || verified == nil || verified.PostconditionHash != "0x"+strings.TrimPrefix(h.sourceSHA256, "sha256:") {
		return nil, errors.New("final generation has no exact original handoff journal receipt")
	}
	for index, member := range h.Members {
		if len(prepared.Members) != 4 || plan.Validators[index/2].PreviousActivations[index%2] != prepared.Members[index].Activation {
			return nil, errors.New("final generation replaced its original activation lineage")
		}
		_, receipt := policyRolloverPriorV2(&plan, plan.Actions[index], replay.entries)
		var publication policyRolloverPublicationV2
		if err := decodeStrictJSONBytes(input.Publications[index], &publication); err != nil {
			return nil, err
		}
		digest, err := member.Activation.Digest()
		if err != nil {
			return nil, err
		}
		if receipt == nil || receipt.PostconditionHash != "0x"+strings.TrimPrefix(bytesSHA256(input.Publications[index]), "sha256:") || publication.Schema != "urnetwork-sim-policy-rollover-publication-v2" || publication.PlanHash != plan.PlanHash || publication.ValidatorID != member.ValidatorId || publication.NoID != member.NoId || publication.ActivationHash != fmt.Sprintf("0x%x", digest) || publication.PublishedBlock <= plan.EVM.Number || publication.PublishedBlock > h.Boundary.Number {
			return nil, errors.New("final generation publication differs from its exact dual consent")
		}
	}
	identitiesPath := filepath.Join(policyRolloverGenerationCommonRootV2(authority.StateRoot, h.Generation), "public", "identities.json")
	if h.Identities.Path != identitiesPath {
		return nil, errors.New("final generation identity namespace differs")
	}
	if err := finalGenerationPinnedBytesV2(h.Identities, input.Identities, maximumCampaignEvidenceRawFileBytes); err != nil {
		return nil, err
	}
	identities, err := decodeFinalOperatorPathAuthority(input.Identities, current.DeploymentID, 2, 2)
	if err != nil {
		return nil, err
	}
	keys := func(id, noId uint64) ([32]byte, [32]byte, error) {
		hotkey, err := decodeHex32("final active hotkey", identities.identities.Substrate[validatorHotkeyLabel(int(id))].PublicKey)
		key := identities.keysByValidator[id][noId]
		if err != nil || len(key) != 32 {
			return [32]byte{}, [32]byte{}, errors.New("final active public custody is incomplete")
		}
		return hotkey, [32]byte(key), nil
	}
	activePrepared := runtimeEvidenceActivationPreparedV2{Schema: "urnetwork-sim-evidence-activation-prepared-v2", PlanHash: source.PlanHash, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Epoch: plan.Epoch, Native: plan.Native, Evm: plan.EVM, Members: plan.Members}
	activeCompleted := runtimeEvidenceActivationCompletedV2{Schema: "urnetwork-sim-evidence-activation-completed-v2", PlanHash: source.PlanHash, Boundary: h.Boundary}
	values, fixed, err := runtimeEvidenceFixedPublicInputsV2(cfg, source, authority.StateRoot, &activePrepared, &activeCompleted, keys)
	if err != nil {
		return nil, err
	}
	for index := range values {
		owner := &h.Validators[index]
		id := uint64(index + 1)
		root := policyRolloverGenerationRootV2(authority.StateRoot, id, h.Generation)
		expected := &values[index].Evidence
		for opIndex := range expected.Operators {
			op := &expected.Operators[opIndex]
			refs := op.Files()
			for n, ref := range refs {
				refs[n] = policyRolloverFile(filepath.Join(root, "evidence-v2", fmt.Sprintf("no-%d", op.NoID), filepath.Base(ref.Path)), fixed[ref.Path])
			}
			op.Activation, op.VPKSignature, op.HotkeySignature, op.Context, op.History = refs[0], refs[1], refs[2], refs[3], refs[4]
			op.ReplayScratchRoot = filepath.Join(root, "scratch-v2", fmt.Sprintf("no-%d", op.NoID), "replay")
			op.SealScratchRoot = filepath.Join(root, "scratch-v2", fmt.Sprintf("no-%d", op.NoID), "seal")
		}
		if owner.ValidatorID != id || owner.PreviousStateDir != plan.Validators[index].StateDir || owner.StateDir != filepath.Join(root, "coordinator-state-v2") || owner.ClientStateDir != filepath.Join(root, "state") || owner.Config.Path != filepath.Join(root, "validator.yml") || owner.Identities != h.Identities || owner.SourceRolePredecessorV2 != nil || !reflect.DeepEqual(owner.Evidence, *expected) {
			return nil, errors.New("final generation changed its namespace or deterministic fixed inputs")
		}
		if err := finalGenerationPinnedBytesV2(owner.Config, input.Configs[index], maximumCampaignEvidenceRawFileBytes); err != nil {
			return nil, err
		}
		config, err := finalGenerationConfigV2(input.Configs[index])
		if err != nil {
			return nil, err
		}
		if config.ValidatorID != id || config.DeploymentID != current.DeploymentID || config.ChainID != cfg.ChainID || config.Netuid != cfg.Netuid || !strings.EqualFold(config.GenesisHash, cfg.Public.Chain.GenesisHash) || config.StateDir != owner.StateDir || config.PolicyHash != cfg.PolicyHash || !reflect.DeepEqual(config.Policy, *cfg.Policy) || config.PreviousPolicy != nil || config.SourceRolePredecessorV2 != nil || !reflect.DeepEqual(config.EvidenceV2, owner.Evidence) || len(config.Operators) != 2 || common.HexToAddress(config.Coordinator) != current.ValidatorEvidence.Coordinator || common.HexToAddress(config.SettlementVault) != current.ValidatorEvidence.SettlementVault {
			return nil, errors.New("final generation config changes approved custody or policy")
		}
		for j, op := range config.Operators {
			if op.NoID != uint64(j+1) || op.APIURL != cfg.OperatorAPIOrigins[j] || op.StateDir != filepath.Join(owner.ClientStateDir, "operators", fmt.Sprintf("no-%d", j+1)) {
				return nil, errors.New("final generation operator authority differs")
			}
		}
	}
	if err := verifyFinalGenerationSourceRoleV2(ctx, cfg, current, authority.StateRoot, &h, input, validatorId, replay.read); err != nil {
		return nil, err
	}
	if err := generation.matchesHandoff(&h); err != nil {
		return nil, err
	}
	if validatorId == 0 || validatorId > uint64(len(h.Validators)) {
		return nil, errors.New("final active validator is absent")
	}
	overlay := generation.Runtime[validatorId-1]
	selected, err := finalGenerationConfigV2(input.SelectedConfigs[validatorId-1])
	if err != nil {
		return nil, err
	}
	selected.RuntimeSpec, selected.TransactionVersion, selected.StateVersion = cfg.Release.Runtime.SpecVersion, cfg.Release.Runtime.TransactionVersion, cfg.Release.Runtime.StateVersion
	selected.RuntimeCodeHash, selected.RuntimeMetadataHash = cfg.Release.Runtime.CodeHash, cfg.Release.Runtime.MetadataHash
	selected.ProvisionalRuntimeCompatibility, selected.ProvisionalDeferClosedNativeInput = "", false
	expected, err := yaml.Marshal(selected)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(h.Validators[validatorId-1].StateDir), "validator-strict-"+stringsTrim0x(current.ReleaseLockHash)+".yml")
	if overlay.ValidatorId != validatorId || overlay.Original != h.Validators[validatorId-1].Config || overlay.Config != policyRolloverFile(path, expected) || overlay.Content != string(expected) || !bytes.Equal(runtimeBytes, expected) {
		return nil, errors.New("final strict generation runtime differs from its exact authenticated projection")
	}
	selected.Coordinator, selected.SettlementVault = strings.ToLower(selected.Coordinator), strings.ToLower(selected.SettlementVault)
	if err := selected.ValidateHistorical(); err != nil {
		return nil, err
	}
	return selected, ctx.Err()
}

// Owner signatures authorize one proof-reference addition, never an edited
// policy/config. The predecessor's signed payloads remain independently pinned.
func verifyFinalGenerationSourceRoleV2(ctx context.Context, cfg *ResolvedConfig, current *SetupPlan, stateRoot string, h *policyRolloverHandoffV2, input *finalValidatorGenerationAuthorityV2, validatorId uint64, read func(string, uint64) ([]byte, error)) error {
	if len(input.SourceRole) == 0 {
		for index := range h.Validators {
			if !bytes.Equal(input.Configs[index], input.SelectedConfigs[index]) {
				return errors.New("final generation changed config without a source-role approval")
			}
		}
		return nil
	}
	var signed ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(input.SourceRole, &signed); err != nil {
		return err
	}
	if err := verifyEvidence(&signed, nil); err != nil {
		return err
	}
	var plan policyRolloverSourceRolePlanV2
	if err := decodeStrictJSONBytes(signed.Payload, &plan); err != nil {
		return err
	}
	hash, err := plan.hash()
	if err != nil {
		return err
	}
	if signed.Kind != policyRolloverSourceRoleKindV2 || signed.RunID != plan.PlanHash || signed.Signer != common.HexToAddress(current.Roles.Owner) || signed.DeploymentID != current.DeploymentID || signed.ChainID != cfg.ChainID || signed.Netuid != cfg.Netuid || !strings.EqualFold(signed.GenesisHash, cfg.Public.Chain.GenesisHash) || hash != plan.PlanHash || plan.Schema != policyRolloverSourceRoleSchemaV2 || plan.SourcePlanHash != h.SourcePlanHash || !current.allowedPlanHashes()[plan.SourcePlanHash] || plan.RolloverPlanHash != h.PlanHash || plan.OriginalHandoffSHA256 != h.sourceSHA256 || plan.DeploymentID != current.DeploymentID || plan.StateDir != stateRoot || plan.ConfigHash != cfg.ConfigHash || plan.PolicyHash != cfg.PolicyHash || plan.Generation != h.Generation || !plan.RoleOnly || !plan.Provisional || plan.FinalAcceptance || plan.LedgerContinuityClaimed || plan.StateImported || len(plan.Validators) != 2 {
		return errors.New("final source-role overlay lacks its exact owner-signed generation approval")
	}
	count := 0
	for index, member := range plan.Validators {
		owner := &h.Validators[index]
		if member.ValidatorID != owner.ValidatorID || member.PreviousStateDir != owner.PreviousStateDir || member.OriginalConfig != owner.Config {
			return errors.New("final source-role overlay changed its original generation")
		}
		if member.Predecessor == nil {
			if member.SourceIntents != nil || member.Config != owner.Config || !bytes.Equal(input.Configs[index], input.SelectedConfigs[index]) {
				return errors.New("final empty source-role overlay changed authority")
			}
			continue
		}
		count++
		root := filepath.Join(policyRolloverSourceRoleRootV2(plan.StateDir, h.Generation), fmt.Sprintf("validator-%d", owner.ValidatorID))
		if member.Config.Path != filepath.Join(root, "validator.yml") || member.Predecessor.Path != filepath.Join(root, "source-role-predecessor.json") || member.SourceIntents == nil || member.SourceIntents.Path != filepath.Join(owner.PreviousStateDir, "steering-intents.json") {
			return errors.New("final source-role overlay escaped its original namespace")
		}
		if err := finalGenerationPinnedBytesV2(member.Config, input.SelectedConfigs[index], maximumCampaignEvidenceRawFileBytes); err != nil {
			return err
		}
		selected, err := finalGenerationConfigV2(input.SelectedConfigs[index])
		if err != nil {
			return err
		}
		original, err := finalGenerationConfigV2(input.Configs[index])
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(selected.SourceRolePredecessorV2, member.Predecessor) {
			return errors.New("final source-role config replaced its exact predecessor")
		}
		selected.SourceRolePredecessorV2 = nil
		if !reflect.DeepEqual(selected, original) {
			return errors.New("final source-role overlay changed fields outside its single proof reference")
		}
		if owner.ValidatorID == validatorId {
			if read == nil {
				return errors.New("final source-role archive inputs are absent")
			}
			load := func(name string, ref validatorpkg.ReleaseEvidenceV2File, limit uint64) ([]byte, error) {
				raw, err := read("active-generation/"+name, min(limit, maximumCampaignEvidenceRawFileBytes))
				if err != nil {
					return nil, err
				}
				if err := finalGenerationPinnedBytesV2(ref, raw, limit); err != nil {
					return nil, err
				}
				return raw, ctx.Err()
			}
			intents, err := load("source-intents", *member.SourceIntents, original.EvidenceV2.Bounds.IntentFileLimit())
			if err != nil {
				return err
			}
			raw, err := load("source-role", *member.Predecessor, validatorpkg.ReleaseSourceRolePredecessorV2MaximumBytes)
			if err != nil {
				return err
			}
			var descriptor validatorpkg.ReleaseSourceRolePredecessorV2
			if err := decodeStrictJSONBytes(raw, &descriptor); err != nil {
				return err
			}
			measurement := filepath.Join("measurements", strings.TrimPrefix(descriptor.Intent.MeasurementArtifactHash, "sha256:")+".json")
			envelope := filepath.Join("measurements", "envelopes", strings.TrimPrefix(descriptor.Intent.MeasurementEnvelopeHash, "sha256:")+".json")
			if !validSHA256ContentHash(descriptor.Intent.MeasurementArtifactHash) || !validSHA256ContentHash(descriptor.Intent.MeasurementEnvelopeHash) || descriptor.Intent.MeasurementArtifactPath != filepath.ToSlash(measurement) || descriptor.Intent.MeasurementEnvelopePath != filepath.ToSlash(envelope) || descriptor.Measurement.Path != filepath.Join(owner.PreviousStateDir, measurement) || descriptor.Envelope.Path != filepath.Join(owner.PreviousStateDir, envelope) {
				return errors.New("final source-role payloads escaped their predecessor")
			}
			proof, err := validatorpkg.DecodeReleaseSourceRolePredecessorArchiveV2(ctx, original, raw, func(ctx context.Context, ref validatorpkg.ReleaseEvidenceV2File, maximum uint64) ([]byte, error) {
				switch ref {
				case descriptor.Measurement:
					return load("source-measurement", ref, maximum)
				case descriptor.Envelope:
					return load("source-envelope", ref, maximum)
				default:
					return nil, errors.New("final source role requested another source")
				}
			})
			if err != nil {
				return err
			}
			if proof.Intent.Prepared == nil || proof.Intent.Prepared.HotkeyHex != fmt.Sprintf("0x%x", h.Members[index*2].Activation.Hotkey) {
				return errors.New("final source-role predecessor changes original hotkey custody")
			}
			prior, err := decodeValidatorIntentBytes(intents, int(validatorId))
			if err != nil {
				return err
			}
			matches := 0
			for _, intent := range prior {
				if reflect.DeepEqual(intent, proof.Intent) {
					matches++
				}
			}
			if matches != 1 {
				return errors.New("final source-role proof has no exact original intent")
			}
		}
		owner.Config, owner.SourceRolePredecessorV2 = member.Config, member.Predecessor
	}
	if count == 0 {
		return errors.New("final source-role approval contains no predecessor")
	}
	ref := policyRolloverFile(filepath.Join(policyRolloverSourceRoleRootV2(plan.StateDir, h.Generation), "handoff.evidence.json"), input.SourceRole)
	h.SourceRoleOverlay = &ref
	return ctx.Err()
}

// The retained inventory still binds the original activated config files.
// New strict projections are independently pinned by the approved appendix.
func verifyFinalGenerationInventoryV2(cfg *ResolvedConfig, authority *finalValidatorAuthorityV2, raw []byte) error {
	var inventory RuntimeConfigManifest
	if err := decodeStrictJSONBytes(raw, &inventory); err != nil {
		return err
	}
	hash, err := runtimeConfigManifestHash(inventory)
	if err != nil || inventory.Schema != runtimeConfigManifestSchema || inventory.ManifestHash != hash || inventory.DeploymentID != cfg.Config.Deployment.DeploymentID || inventory.ConfigHash != cfg.ConfigHash || inventory.PolicyHash != cfg.PolicyHash {
		return errors.Join(errors.New("final active generation retained inventory differs"), err)
	}
	var h policyRolloverHandoffV2
	if err := decodeStrictJSONBytes(authority.ActiveGeneration.Handoff, &h); err != nil {
		return err
	}
	paths := map[string][]byte{}
	for index, owner := range h.Validators {
		paths[owner.Config.Path] = authority.ActiveGeneration.Configs[index]
		paths[filepath.Join(authority.StateRoot, "runtime", fmt.Sprintf("validator-%d", index+1), "validator.yml")] = authority.ActiveGeneration.FrozenConfigs[index]
	}
	for path, raw := range paths {
		relative, err := filepath.Rel(authority.StateRoot, path)
		if err != nil {
			return err
		}
		count := 0
		for _, file := range inventory.Files {
			if file.Path != filepath.ToSlash(relative) {
				continue
			}
			if file.Mode != "0600" || file.SHA256 != bytesSHA256(raw) {
				return errors.New("final active generation changed its inventoried original config")
			}
			count++
		}
		if count != 1 {
			return errors.New("final active generation inventory config is missing or duplicated")
		}
	}
	return nil
}

// A new activation never discards the exact original fixed files. Template
// capacities are current approved limits; payloads derive only from the old
// signed prepared/completed owner, its policy and its original public keys.
func verifyFinalFrozenGenerationV2(cfg *ResolvedConfig, source *SetupPlan, authority *finalValidatorAuthorityV2, values []validatorpkg.ReleaseValidatorEvidenceV2Config, inputs map[string][]byte) error {
	input := authority.ActiveGeneration
	if len(values) != 2 || len(input.FrozenConfigs) != 2 || len(input.FrozenInputs) != 20 {
		return errors.New("final frozen generation lost its exact source census")
	}
	index := 0
	for validatorIndex, value := range values {
		config, err := finalGenerationConfigV2(input.FrozenConfigs[validatorIndex])
		if err != nil {
			return err
		}
		expected := value.Evidence
		expected.Bounds = config.EvidenceV2.Bounds
		boundsAllowed := reflect.DeepEqual(config.EvidenceV2.Bounds, value.Evidence.Bounds)
		for _, bounds := range cfg.Config.ValidatorEvidenceV2 {
			if bounds.ValidatorID == uint64(validatorIndex+1) {
				boundsAllowed = boundsAllowed || reflect.DeepEqual(config.EvidenceV2.Bounds, bounds.Evidence.Bounds)
			}
		}
		policyHash, err := config.Policy.HashHex()
		if err != nil || config.PolicyHash != policyHash || (config.PolicyHash != cfg.PolicyHash && config.PolicyHash != source.PolicyHash) || config.ValidatorID != uint64(validatorIndex+1) || config.DeploymentID != source.DeploymentID || config.ChainID != source.ChainID || config.Netuid != source.Netuid || !strings.EqualFold(config.GenesisHash, cfg.Public.Chain.GenesisHash) || !boundsAllowed || !reflect.DeepEqual(config.EvidenceV2, expected) || common.HexToAddress(config.Coordinator) != source.ValidatorEvidence.Coordinator || common.HexToAddress(config.SettlementVault) != source.ValidatorEvidence.SettlementVault {
			return errors.Join(errors.New("final frozen config differs from original activation authority"), err)
		}
		for _, operator := range value.Evidence.Operators {
			for _, ref := range operator.Files() {
				if !bytes.Equal(input.FrozenInputs[index], inputs[ref.Path]) || finalGenerationPinnedBytesV2(ref, input.FrozenInputs[index], uint64(len(inputs[ref.Path]))) != nil {
					return errors.New("final frozen activation bytes differ from original signed authority")
				}
				index++
			}
		}
	}
	return nil
}

// Path routing keeps the archived original identities intact. Capture readback
// verifies signatures here but remains pending semantic verification; the final
// builder calls this only after the complete generation/journal replay above.
func finalGenerationPathAuthorityV2(original *finalOperatorPathAuthority, authority *finalValidatorAuthorityV2) (*finalOperatorPathAuthority, error) {
	if original == nil || authority == nil {
		return nil, errors.New("final generation path owner is absent")
	}
	input := authority.ActiveGeneration
	if input == nil {
		return original, nil
	}
	var h policyRolloverHandoffV2
	if err := decodeStrictJSONBytes(input.Handoff, &h); err != nil {
		return nil, err
	}
	if len(h.Members) != 4 || !h.Activated || h.DeploymentID != original.identities.DeploymentID {
		return nil, errors.New("final generation path census differs")
	}
	if err := finalGenerationPinnedBytesV2(h.Identities, input.Identities, maximumCampaignEvidenceRawFileBytes); err != nil {
		return nil, err
	}
	active, err := decodeFinalOperatorPathAuthority(input.Identities, h.DeploymentID, 2, 2)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(active.identities.EVM, original.identities.EVM) || !reflect.DeepEqual(active.identities.Substrate, original.identities.Substrate) || len(active.identities.Clients) != len(original.identities.Clients) {
		return nil, errors.New("final generation changed unrelated public custody")
	}
	for label, client := range original.identities.Clients {
		if !strings.HasPrefix(label, "validator-") && !reflect.DeepEqual(active.identities.Clients[label], client) {
			return nil, errors.New("final generation changed another client")
		}
	}
	for index, member := range h.Members {
		id, noId := uint64(index/2+1), uint64(index%2+1)
		hotkey, err := decodeHex32("original generation hotkey", original.identities.Substrate[validatorHotkeyLabel(int(id))].PublicKey)
		if err != nil || member.ValidatorId != id || member.NoId != noId || member.Activation.Hotkey != hotkey || !bytes.Equal(member.Activation.VPK[:], active.keysByValidator[id][noId]) {
			return nil, errors.Join(errors.New("final generation path differs from its signed source"), err)
		}
		if err := member.Activation.Verify(member.Activation, member.VpkSignature, member.HotkeySignature); err != nil {
			return nil, err
		}
	}
	return active, nil
}

// The raw capture's source census supplies routing bytes only. It cannot grant
// a semantic pass; openFinalValidatorReplayV2 independently checks the complete
// archived approval, original inputs, activated receipts and strict adoption.
func finalCapturedGenerationPathAuthorityV2(ctx context.Context, collected FinalCollectedValidatorInputs, original *finalOperatorPathAuthority, read FinalArtifactLoader) (*finalOperatorPathAuthority, error) {
	if collected.EvidenceV2 == nil {
		return original, nil
	}
	for _, source := range collected.EvidenceV2.Sources {
		if source.Source != (validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "simulator-authority"}) {
			continue
		}
		raw, err := loadFinalV2Source(ctx, read, source.Artifact, maximumCampaignEvidenceRawFileBytes)
		if err != nil {
			return nil, err
		}
		var authority finalValidatorAuthorityV2
		if err := decodeStrictJSONBytes(raw, &authority); err != nil {
			return nil, err
		}
		return finalGenerationPathAuthorityV2(original, &authority)
	}
	return original, nil
}

//go:build linux || darwin

package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

func validatePolicyRolloverOptionsV2(command string, o cliOptions) error {
	used := o.RolloverPlan != "" || o.RolloverPlanHash != "" || o.RolloverEpoch != 0 || o.RolloverGeneration != 0
	if command != "policy-rollover" {
		if used {
			return errors.New("rollover options require policy-rollover")
		}
		return nil
	}
	if o.Detach || o.Name != "" || o.PrepareOnly || o.ThenReleaseCandidate || o.Manifest != "" || o.StrictHistoryAdoption != "" || !validCanonicalHashHex(o.PlanHash) {
		return errors.New("policy-rollover requires the exact existing --plan-hash and cannot launch a topology")
	}
	if o.Apply {
		if !validCanonicalHashHex(o.RolloverPlanHash) || o.RolloverPlan == "" {
			return errors.New("policy-rollover apply requires --rollover-plan and its exact --rollover-plan-hash")
		}
	} else if o.RolloverPlanHash != "" {
		return errors.New("--rollover-plan-hash requires --apply")
	}
	if o.RolloverPlan != "" {
		if !filepath.IsAbs(o.RolloverPlan) || filepath.Clean(o.RolloverPlan) != o.RolloverPlan || o.RolloverEpoch != 0 || o.RolloverGeneration != 0 {
			return errors.New("rollover import requires one absolute plan path and no replacement epoch or generation")
		}
	} else if o.RolloverEpoch == 0 || o.RolloverEpoch == ^uint64(0) || o.RolloverGeneration == 0 {
		return errors.New("policy-rollover planning requires explicit --rollover-epoch and --rollover-generation")
	}
	return nil
}

func (e *Executor) authenticatePolicyRolloverSnapshotsV2(ctx context.Context, chain *validatorcomponent.ChainClient, p *policyRolloverPlanV2, roles *RoleSecrets) error {
	if err := validatePolicyRolloverPlanV2(e.cfg, e.plan, e.stateDir, p); err != nil {
		return err
	}
	if p.Keeper != common.HexToAddress(e.roles.EVM["keeper"].Address) {
		return errors.New("rollover keeper differs from existing role")
	}
	block, _, err := chain.FinalizedBlockContext(ctx)
	if err != nil || block < p.EVM.Number {
		return errors.Join(errors.New("rollover EVM snapshot is not finalized"), err)
	}
	hash, err := chain.BlockHashContext(ctx, p.EVM.Number)
	if err != nil || common.Hash(hash).Hex() != p.EVM.Hash {
		return errors.Join(errors.New("rollover EVM snapshot changed"), err)
	}
	coordinator := stabi.NewSTCoordinator()
	current, err := rawCoordinatorCallAt(ctx, e.keeper, common.Address(p.Members[0].Activation.Domain.Coordinator), coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch, p.EVM.Number)
	if err != nil || current == nil || !current.IsUint64() || current.Uint64() >= p.Epoch {
		return errors.Join(errors.New("rollover snapshot does not precede the explicitly selected future epoch"), err)
	}
	policy, err := rawCoordinatorCallAt(ctx, e.keeper, e.plan.ValidatorEvidence.Coordinator, coordinator.PackPolicyAt(new(big.Int).SetUint64(p.Epoch)), coordinator.UnpackPolicyAt, p.EVM.Number)
	if err != nil || policy.PolicyHash != p.Members[0].Activation.Domain.PolicyHash || policy.EffectiveEpoch > p.Epoch {
		return errors.Join(errors.New("rollover future policy differs at immutable snapshot"), err)
	}
	for _, member := range p.Members {
		hotkey, key, err := runtimeEvidenceActivationKeysV2(roles, member.ValidatorId, member.NoId)
		if err != nil {
			return err
		}
		if member.Activation.Hotkey != hotkey.PublicKey() || member.Activation.VPK != [32]byte(key[ed25519.SeedSize:]) {
			return errors.New("rollover consent differs from deterministic generation roles")
		}
		observation, err := crv4.ReadValidatorScheduleAtContext(ctx, e.substrate.chain, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(member.Activation.Domain.GenesisHash), BlockHash: types.Hash(member.Activation.NativeHash), BlockNumber: p.Native.Number, Netuid: e.cfg.Netuid, Hotkey: hotkey.PublicKey(), MaximumSubnetUIDs: uint32(hyperparameterUint64(e.cfg.Hyperparameters.OwnerControlled["max_allowed_uids"]))}, e.runtimeEvidenceNativeIdentityV2())
		if err != nil || !observation.Stake.MeetsNonSelfStakeAndPermit() || observation.Stake.Identity.UID != member.ValidatorUid {
			return errors.Join(errors.New("rollover historical native identity, stake or permit differs"), err)
		}
		operator, err := rawCoordinatorCallAt(ctx, e.keeper, e.plan.ValidatorEvidence.Coordinator, coordinator.PackOperatorAt(new(big.Int).SetUint64(member.NoId), new(big.Int).SetUint64(p.Epoch)), coordinator.UnpackOperatorAt, p.EVM.Number)
		if err != nil || !operator.Active || operator.EffectiveEpoch > p.Epoch {
			return errors.Join(errors.New("rollover operator is inactive at the approved epoch"), err)
		}
	}
	canonical, err := chain.BlockHashContext(ctx, p.EVM.Number)
	if err != nil || canonical != hash {
		return errors.Join(errors.New("rollover immutable snapshot changed during authority reads"), err)
	}
	return ctx.Err()
}

func capturePolicyRolloverPlanV2(ctx context.Context, e *Executor, chain *validatorcomponent.ChainClient, epoch, generation uint64, limit uint64) (*policyRolloverPlanV2, error) {
	path := policyRolloverPlanPathV2(e.stateDir, generation, epoch)
	retained, err := readPolicyRolloverPlanV2(ctx, e.cfg, e.plan, e.stateDir, path)
	if err == nil {
		if retained.Epoch != epoch || retained.Generation != generation {
			return nil, errors.New("immutable rollover plan already owns a different epoch or generation")
		}
		return retained, nil
	}
	if !validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return nil, err
	}
	if generation == 0 || epoch == 0 || epoch == ^uint64(0) {
		return nil, errors.New("rollover requires explicit generation and bounded future epoch")
	}
	planned, scanErr := os.ReadDir(filepath.Join(policyRolloverRoot(e.stateDir), "plans"))
	if scanErr != nil && !errors.Is(scanErr, os.ErrNotExist) {
		return nil, scanErr
	}
	for _, entry := range planned {
		if strings.HasPrefix(entry.Name(), fmt.Sprintf("generation-%020d-epoch-", generation)) && entry.Name() != filepath.Base(policyRolloverPlanRootV2(e.stateDir, generation, epoch)) {
			return nil, errors.New("generation already has an immutable epoch plan; use a new explicit generation to preserve prior consent history")
		}
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(e.cfg, e.stateDir)
	if err != nil {
		return nil, err
	}
	if resolved.Config.Topology.Validators != 2 || resolved.Config.Topology.Operators != 2 || e.plan.ValidatorEvidence == nil {
		return nil, errors.New("rollover requires the existing exact four-member deployment")
	}
	roles, err := policyRolloverGenerationRolesV2(e.cfg, e.roles, generation)
	if err != nil {
		return nil, err
	}
	block, hash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	nativeHash, err := crv4.FinalizedHeadContext(ctx, e.substrate.chain)
	if err != nil {
		return nil, err
	}
	nativeHeader, err := e.substrate.chain.HeaderAtContext(ctx, nativeHash)
	if err != nil {
		return nil, err
	}
	entries := e.journal.Entries()
	if len(entries) == 0 {
		return nil, errors.New("rollover requires an existing durable deployment checkpoint")
	}
	p := &policyRolloverPlanV2{Schema: policyRolloverPlanV2Schema, SourcePlanHash: e.plan.PlanHash, SourceJournalHash: entries[len(entries)-1].EntryHash,
		DeploymentID: e.plan.DeploymentID, StateDir: e.stateDir, ConfigHash: e.cfg.ConfigHash, PolicyHash: e.cfg.PolicyHash, Epoch: epoch, Generation: generation,
		Native: ChainHead{Number: uint64(nativeHeader.Number), Hash: nativeHash.Hex()}, EVM: ChainHead{Number: block, Hash: common.Hash(hash).Hex()},
		Journal: e.plan.ValidatorEvidence.Address, JournalRuntimeHash: e.plan.ValidatorEvidence.RuntimeCodeHash, Keeper: common.HexToAddress(e.roles.EVM["keeper"].Address),
		MaximumGasUnits: e.cfg.Config.ValidatorEvidenceActivationGasUnits, MaximumFeePerGasWei: e.plan.MaximumEVMFeePerGasWei, MaximumAttempts: 3, AttemptTimeoutSeconds: 90}
	policyHash, err := decodeHex32("rollover policy", e.cfg.PolicyHash)
	if err != nil {
		return nil, err
	}
	for index, configured := range resolved.Config.ValidatorEvidenceV2 {
		id := uint64(index + 1)
		originalPath := filepath.Join(e.stateDir, "runtime", fmt.Sprintf("validator-%d", id), "validator.yml")
		configBytes, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, originalPath, limit)
		if err != nil {
			return nil, err
		}
		savedPath := filepath.Join(policyRolloverPlanRootV2(e.stateDir, generation, epoch), "source", fmt.Sprintf("validator-%d.yml", id))
		if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, savedPath, configBytes, limit); err != nil {
			return nil, err
		}
		checkpoint := policyRolloverValidatorCheckpointV2{ValidatorID: id, Config: policyRolloverFile(savedPath, configBytes), StateDir: filepath.Join(e.stateDir, "runtime", fmt.Sprintf("validator-%d", id), "state")}
		if e.plan.EvidenceRelayContinuation != nil {
			checkpoint.StateDir = filepath.Join(e.stateDir, "runtime", fmt.Sprintf("validator-%d", id), "coordinator-state-v2")
		}
		for _, operator := range configured.Evidence.Operators {
			raw, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, operator.Activation, uint64(protocol.ValidatorEvidenceActivationPayloadSize))
			if err != nil {
				return nil, err
			}
			prior, err := protocol.DecodeValidatorEvidenceActivationPayload(raw)
			if err != nil {
				return nil, err
			}
			checkpoint.PreviousActivations = append(checkpoint.PreviousActivations, prior)
			hotkey, key, err := runtimeEvidenceActivationKeysV2(roles, id, operator.NoID)
			if err != nil {
				return nil, err
			}
			observation, err := crv4.ReadValidatorScheduleAtContext(ctx, e.substrate.chain, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(e.plan.ValidatorEvidence.GenesisHash), BlockHash: nativeHash, BlockNumber: p.Native.Number, Netuid: e.cfg.Netuid, Hotkey: hotkey.PublicKey(), MaximumSubnetUIDs: uint32(hyperparameterUint64(e.cfg.Hyperparameters.OwnerControlled["max_allowed_uids"]))}, e.runtimeEvidenceNativeIdentityV2())
			if err != nil || !observation.Stake.MeetsNonSelfStakeAndPermit() {
				return nil, errors.Join(errors.New("rollover validator lacks native eligibility"), err)
			}
			activation := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: testnetChainID, GenesisHash: [32]byte(e.plan.ValidatorEvidence.GenesisHash), Netuid: e.cfg.Netuid, Coordinator: [20]byte(e.plan.ValidatorEvidence.Coordinator), SettlementVault: [20]byte(e.plan.ValidatorEvidence.SettlementVault), DeploymentIDHash: [32]byte(e.plan.ValidatorEvidence.DeploymentIDHash), PolicyHash: policyHash, Epoch: epoch},
				Hotkey: hotkey.PublicKey(), VPK: [32]byte(key[ed25519.SeedSize:]), NoID: operator.NoID, FirstSequence: 1, NativeBlock: p.Native.Number, NativeHash: [32]byte(nativeHash), EVMBlock: block, EVMHash: hash}
			vpkSignature, err := activation.SignVPK(key)
			if err != nil {
				return nil, err
			}
			digest, err := activation.Digest()
			if err != nil {
				return nil, err
			}
			hotkeySignature, err := hotkey.Sign(digest[:])
			if err != nil {
				return nil, err
			}
			p.Members = append(p.Members, runtimeEvidenceActivationMemberV2{ValidatorId: id, NoId: operator.NoID, ValidatorUid: observation.Stake.Identity.UID, Activation: activation, VpkSignature: vpkSignature, HotkeySignature: hotkeySignature})
		}
		p.Validators = append(p.Validators, checkpoint)
	}
	p.Actions, err = policyRolloverActionsV2(p)
	if err != nil {
		return nil, err
	}
	maximum := new(big.Int).Mul(new(big.Int).SetUint64(p.MaximumGasUnits), new(big.Int).SetUint64(p.MaximumFeePerGasWei))
	p.MaximumGasWei = DecimalUint(maximum.Mul(maximum, big.NewInt(4)).String())
	p.PlanHash, err = p.hash()
	if err != nil {
		return nil, err
	}
	if err := validatePolicyRolloverBudgetV2(e.cfg, e.stateDir, e.plan, p, entries); err != nil {
		return nil, err
	}
	if err := e.authenticatePolicyRolloverSnapshotsV2(ctx, chain, p, roles); err != nil {
		return nil, err
	}
	if _, err := writeRuntimeEvidenceSetupV2(ctx, path, p, limit); err != nil {
		return nil, err
	}
	return p, ctx.Err()
}

func (e *Executor) policyRolloverBoundaryV2(ctx context.Context, chain *validatorcomponent.ChainClient, p *policyRolloverPlanV2, publications []policyRolloverPublicationV2) (ChainHead, error) {
	block, hash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return ChainHead{}, err
	}
	start, err := chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, new(big.Int).SetUint64(p.Epoch))
	if err != nil {
		return ChainHead{}, err
	}
	end, err := chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, new(big.Int).SetUint64(p.Epoch+1))
	if err != nil {
		return ChainHead{}, err
	}
	boundary := start
	for _, publication := range publications {
		boundary = max(boundary, publication.PublishedBlock)
	}
	if start == 0 || end <= start || boundary >= end || boundary <= p.EVM.Number {
		return ChainHead{}, errors.New("rollover publications do not fit the approved activation epoch")
	}
	if boundary > block {
		return ChainHead{}, fmt.Errorf("rollover publications are complete; resume after activation boundary block %d is finalized", boundary)
	}
	boundaryHash, err := chain.BlockHashContext(ctx, boundary)
	if err != nil {
		return ChainHead{}, err
	}
	for _, member := range p.Members {
		authority := validatorcomponent.ReleaseActivationV2Authority{Expected: member.Activation, Journal: p.Journal, RuntimeHash: [32]byte(p.JournalRuntimeHash), ValidatorUID: member.ValidatorUid, NativeRuntime: e.runtimeEvidenceNativeIdentityV2()}
		if _, err := chain.AuthenticateReleaseActivationV2Context(ctx, e.substrate.chain, authority, member.Activation, member.VpkSignature, member.HotkeySignature, boundary, boundaryHash); err != nil {
			return ChainHead{}, err
		}
	}
	return ChainHead{Number: boundary, Hash: common.Hash(boundaryHash).Hex()}, ctx.Err()
}

func runPolicyRolloverV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, o cliOptions) error {
	base, err := loadInvocationPlan(cfg, stateDir, "policy-rollover", o)
	if err != nil {
		return err
	}
	if err := requireApproved(true, o.PlanHash, base.PlanHash); err != nil {
		return err
	}
	if err := prepareProvisionalResume(ctx, cfg, stateDir, "policy-rollover", o, base); err != nil {
		return err
	}
	roles, err := loadExistingProvisionalRoles(cfg, stateDir)
	if err != nil {
		return err
	}
	limit, err := runtimeEvidenceProvisionLimit(cfg)
	if err != nil {
		return err
	}
	var journal *Journal
	if o.Apply {
		journal, err = OpenJournal(stateDir)
	} else {
		journal, err = OpenJournalSnapshot(stateDir)
	}
	if err != nil {
		return err
	}
	defer journal.Close()
	executorCfg := *cfg
	executorCfg.readOnlyAudit = !o.Apply
	e, err := NewExecutor(ctx, &executorCfg, stateDir, base, journal, roles)
	if err != nil {
		return err
	}
	defer e.Close()
	chain, err := e.runtimeEvidenceActivationChainV2(ctx)
	if err != nil {
		return err
	}
	defer chain.Close()
	var plan *policyRolloverPlanV2
	if o.RolloverPlan == "" {
		plan, err = capturePolicyRolloverPlanV2(ctx, e, chain, o.RolloverEpoch, o.RolloverGeneration, limit)
	} else {
		plan, err = readPolicyRolloverPlanV2(ctx, cfg, base, stateDir, o.RolloverPlan)
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
	if err := validatePolicyRolloverJournalV2(plan, journal.Entries()); err != nil {
		return err
	}
	if _, err := writeRuntimeEvidenceSetupV2(ctx, policyRolloverPlanPathV2(stateDir, plan.Generation, plan.Epoch), plan, limit); err != nil {
		return err
	}
	generationRoles, err := policyRolloverGenerationRolesV2(cfg, roles, plan.Generation)
	if err != nil {
		return err
	}
	if err := e.authenticatePolicyRolloverSnapshotsV2(ctx, chain, plan, generationRoles); err != nil {
		return err
	}
	if err := validatePolicyRolloverBudgetV2(cfg, stateDir, base, plan, journal.Entries()); err != nil {
		return err
	}
	publications, err := publishPolicyRolloverV2(ctx, plan, journal, limit, policyRolloverPublicationIOV2{
		Observe: func(ctx context.Context, member runtimeEvidenceActivationMemberV2) (uint64, error) {
			block, hash, err := chain.FinalizedBlockContext(ctx)
			if err != nil {
				return 0, err
			}
			observed, err := chain.ValidatorEvidenceActivationAtHashContext(ctx, plan.Journal, [32]byte(plan.JournalRuntimeHash), member.Activation, block, hash)
			return observed.PublishedBlock, err
		},
		Send: func(ctx context.Context, action Action, member runtimeEvidenceActivationMemberV2) error {
			data, err := stabi.PackValidatorEvidenceActivation(member.Activation, member.Activation, member.VpkSignature, member.HotkeySignature)
			if err != nil {
				return err
			}
			_, err = e.keeper.Send(ctx, plan.PlanHash, action, &plan.Journal, new(big.Int), data)
			return err
		},
	})
	if err != nil {
		return err
	}
	boundary, err := e.policyRolloverBoundaryV2(ctx, chain, plan, publications)
	if err != nil {
		return err
	}
	if err := provisionPolicyRolloverGenerationClientsV2(ctx, cfg, stateDir, plan.Generation, generationRoles); err != nil {
		return err
	}
	validators, err := stagePolicyRolloverGenerationV2(ctx, cfg, base, stateDir, plan.Generation, generationRoles, plan.Members, boundary)
	if err != nil {
		return err
	}
	for index := range validators {
		validators[index].PreviousStateDir = plan.Validators[index].StateDir
	}
	handoff := &policyRolloverHandoffV2{Schema: policyRolloverHandoffV2Schema, PlanHash: plan.PlanHash, SourcePlanHash: plan.SourcePlanHash, DeploymentID: plan.DeploymentID, Generation: plan.Generation, CutoffEpoch: plan.Epoch, FirstFullEpoch: plan.Epoch + 1, Native: plan.Native, EVM: plan.EVM, Boundary: boundary, Members: plan.Members, Validators: validators}
	if len(validators) != 2 {
		return errors.New("rollover staging omitted a validator")
	}
	handoff.Identities = validators[0].Identities
	if _, err := writeRuntimeEvidenceSetupV2(ctx, filepath.Join(policyRolloverPlanRootV2(stateDir, plan.Generation, plan.Epoch), "staged.json"), handoff, limit); err != nil {
		return err
	}
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return printResult(o.Format, map[string]any{"status": "staged-awaiting-stopped-topology", "rollover_plan_hash": plan.PlanHash, "staged": filepath.Join(policyRolloverPlanRootV2(stateDir, plan.Generation, plan.Epoch), "staged.json")}, nil)
	}
	for id := 1; id <= 2; id++ {
		if err := requireValidatorStateStopped(stateDir, id); err != nil {
			return err
		}
	}
	handoff.Activated = true
	action, err := policyRolloverHandoffActionV2(plan)
	if err != nil {
		return err
	}
	_, prior := policyRolloverPriorV2(plan, action, journal.Entries())
	if prior == nil {
		if err := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
			return err
		}
	}
	// All referenced files and the verified activation journal precede the
	// pointer. A crash before this immutable final write resumes exactly.
	if err := persistPolicyRolloverPostconditionV2(ctx, plan, journal, action, handoff, limit); err != nil {
		return err
	}
	if _, err := writeRuntimeEvidenceSetupV2(ctx, filepath.Join(policyRolloverRoot(stateDir), "handoff.json"), handoff, limit); err != nil {
		return err
	}
	return printResult(o.Format, handoff, nil)
}

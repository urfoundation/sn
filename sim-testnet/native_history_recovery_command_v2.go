//go:build linux || darwin

// Recovery planning authenticates stopped sources and current native finality.
// Publication writes only an exact owner-signed local handoff under exclusion.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// lockNativeHistoryRecoveryV2 takes the existing deployment writer lock using
// a read-only descriptor. Capture cannot create or append a deployment file.
func lockNativeHistoryRecoveryV2(stateDir string) (func() error, error) {
	path := filepath.Join(stateDir, "deployment.lock")
	if err := rejectFinalArtifactSymlinkComponents(stateDir, path); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0o600 {
		return nil, errors.Join(errors.New("native recovery deployment lock is not an existing private regular file"), err, file.Close())
	}
	owner, ok := stat.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, errors.Join(errors.New("native recovery deployment lock has another owner"), file.Close())
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return func() error {
		current, err := os.Lstat(path)
		if err == nil && !os.SameFile(stat, current) {
			err = errors.New("native recovery deployment lock was replaced")
		}
		return errors.Join(err, syscall.Flock(fd, syscall.LOCK_UN), file.Close())
	}, nil
}

// requireNativeHistoryRecoveryStoppedV2 excludes active processes and any
// unresolved release perturbation before both capture and publication.
func requireNativeHistoryRecoveryStoppedV2(stateDir string) error {
	if err := strictHistorySupervisorStopped(stateDir); err != nil {
		return err
	}
	for _, id := range []int{1, 2} {
		if err := requireValidatorStateStopped(stateDir, id); err != nil {
			return err
		}
	}
	faults, err := readActiveFaultFile(filepath.Join(stateDir, "active-faults.json"))
	if err != nil || len(faults.Faults) != 0 {
		return errors.Join(errors.New("native recovery requires restored release perturbations"), err)
	}
	return nil
}

// nativeRecoveryScheduleV2 authenticates actual runtime, registration, stake,
// permit, schedule and canonical hash at the same independently pinned block.
func nativeRecoveryScheduleV2(ctx context.Context, native *crv4.Chain, config *validatorcomponent.ReleaseConfig, block ChainHead) (crv4.ValidatorScheduleObservation, error) {
	seed, err := crv4.LoadSeedFile(config.HotkeySeedFile)
	if err != nil {
		return crv4.ValidatorScheduleObservation{}, err
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		return crv4.ValidatorScheduleObservation{}, err
	}
	hash, err := types.NewHashFromHexString(block.Hash)
	if err != nil {
		return crv4.ValidatorScheduleObservation{}, err
	}
	anchor := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: config.RuntimeSpec, TransactionVersion: config.TransactionVersion, StateVersion: config.StateVersion}, CodeHash: config.RuntimeCodeHash, MetadataHash: config.RuntimeMetadataHash}
	return crv4.ReadValidatorScheduleAtContext(ctx, native, crv4.ValidatorScheduleQuery{GenesisHash: native.GenesisHash, BlockHash: hash, BlockNumber: block.Number, Netuid: config.Netuid, Hotkey: hotkey.PublicKey(), MaximumSubnetUIDs: 65535}, validatorcomponent.HistoricalReleaseRuntimeArtifacts(anchor)...)
}

// nativeRecoveryHeadV2 resolves only finalized native blocks, never an EVM
// epoch number or a wall-clock estimate of the native schedule.
func nativeRecoveryHeadV2(ctx context.Context, native *crv4.Chain) (ChainHead, error) {
	hash, err := crv4.FinalizedHeadContext(ctx, native)
	if err != nil {
		return ChainHead{}, err
	}
	header, err := native.HeaderAtContext(ctx, hash)
	if err != nil {
		return ChainHead{}, err
	}
	return ChainHead{Number: uint64(header.Number), Hash: hash.Hex()}, nil
}

// verifyNativeHistoryRecoverySourcesV2 authenticates original signed sources
// and applied native rows. The current schedule must still precede the one
// approved first epoch; a missed epoch cannot be repaired by advancing it here.
func verifyNativeHistoryRecoverySourcesV2(ctx context.Context, native *crv4.Chain, p *nativeHistoryRecoveryPlanV2) error {
	current, err := nativeRecoveryHeadV2(ctx, native)
	if err != nil {
		return err
	}
	for _, request := range p.Validators {
		config, err := validatorcomponent.LoadReleaseConfig(request.ConfigPath)
		if err != nil {
			return err
		}
		pinned, err := nativeRecoveryScheduleV2(ctx, native, config, p.Native)
		if err != nil || pinned.SubnetEpochIndex != p.NativeEpoch || pinned.Stake.Identity.Runtime != p.Runtime || !pinned.Stake.MeetsNonSelfStakeAndPermit() {
			return errors.Join(errors.New("native recovery pinned schedule, runtime or validator eligibility changed"), err)
		}
		fresh, err := nativeRecoveryScheduleV2(ctx, native, config, current)
		if err != nil || fresh.SubnetEpochIndex >= p.FirstNativeEpoch || fresh.Stake.Identity.Runtime != p.Runtime || !fresh.Stake.MeetsNonSelfStakeAndPermit() || fresh.Stake.Identity.Hotkey != pinned.Stake.Identity.Hotkey || fresh.Stake.Identity.UID != pinned.Stake.Identity.UID {
			return errors.Join(errors.New("native recovery first epoch expired or current native identity changed"), err)
		}
		observed, err := validatorcomponent.ObserveReleaseNativeSourcesV2(ctx, config, native, pinned.Stake.Identity.Hotkey, &request.Adoption)
		if err != nil {
			return err
		}
		if observed.StoreSHA256 != request.Adoption.IntentPrefixSHA256 || uint64(len(observed.References)) != request.Adoption.IntentPrefixCount {
			return errors.New("native recovery authenticated source differs from its stopped reviewed prefix")
		}
		last := observed.References[len(observed.References)-1].Intent
		if last.SubnetEpoch != request.Adoption.LastNativeEpoch || last.Status != "applied" || last.Prepared == nil || last.Prepared.SourceCommitment == nil || last.Prepared.SourceCommitment.RuntimeSpec != p.Runtime.Version.SpecVersion || last.Prepared.SourceCommitment.TransactionVersion != p.Runtime.Version.TransactionVersion || last.Prepared.SourceCommitment.CompatibilityProfile != request.CompatibilityProfile {
			return errors.New("native recovery terminal applied source has another runtime domain")
		}
	}
	return ctx.Err()
}

// captureNativeHistoryRecoveryPlanV2 preserves the selected generation and
// overlay. It neither renders configs nor writes a recovery request to disk.
func captureNativeHistoryRecoveryPlanV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, h *policyRolloverHandoffV2, native *crv4.Chain, source string, first uint64) (*nativeHistoryRecoveryPlanV2, error) {
	if h == nil || h.Generation != 2 || h.SourceRoleOverlay == nil || len(h.Validators) != 2 {
		return nil, errors.New("native recovery needs generation 2 and its authenticated source-role overlay")
	}
	terminal, result, runId, err := authenticateNativeRecoveryTerminalV2(ctx, cfg, stateDir, base, source)
	if err != nil {
		return nil, err
	}
	head, err := nativeRecoveryHeadV2(ctx, native)
	if err != nil {
		return nil, err
	}
	config, err := policyRolloverSourceRoleConfigV2(ctx, h.Validators[0].Config, validatorcomponent.ReleaseNativeHistoryRecoveryV2MaximumBytes)
	if err != nil {
		return nil, err
	}
	schedule, err := nativeRecoveryScheduleV2(ctx, native, config, head)
	if err != nil {
		return nil, err
	}
	p := &nativeHistoryRecoveryPlanV2{Schema: nativeHistoryRecoverySchemaV2, BasePlanHash: base.PlanHash, DeploymentId: base.DeploymentID, StateDir: stateDir, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Generation: h.Generation, RolloverPlanHash: h.PlanHash, RolloverHandoffSha256: h.sourceSHA256, SourceRole: *h.SourceRoleOverlay, Driver: cfg.provisionalResume.Driver, Terminal: terminal, Result: result, RunId: runId, Native: head, NativeEpoch: schedule.SubnetEpochIndex, FirstNativeEpoch: first, Runtime: schedule.Stake.Identity.Runtime, Provisional: true}
	_, _, p.ConfigMigration, err = nativeRecoveryPredecessorScopeV2(ctx, cfg, stateDir, base)
	if err != nil {
		return nil, err
	}
	for _, selected := range h.Validators {
		configRaw, err := validatorcomponent.ReadReleaseEvidenceV2File(ctx, selected.Config, validatorcomponent.ReleaseNativeHistoryRecoveryV2MaximumBytes)
		if err != nil {
			return nil, err
		}
		raw, err := validatorcomponent.CaptureReleaseNativeHistoryRecoveryV2(ctx, stateDir, selected.Config.Path, configRaw, base.PlanHash, h.PlanHash, first, p.Runtime)
		if err != nil {
			return nil, err
		}
		request, err := validatorcomponent.DecodeReleaseNativeHistoryRecoveryV2(raw, bytesSHA256(raw))
		if err != nil {
			return nil, err
		}
		p.Validators = append(p.Validators, *request)
	}
	p.PlanHash, err = p.hash()
	if err != nil {
		return nil, err
	}
	if err := validateNativeHistoryRecoveryPlanV2(ctx, cfg, stateDir, base, h, p, true); err != nil {
		return nil, err
	}
	if err := verifyNativeHistoryRecoverySourcesV2(ctx, native, p); err != nil {
		return nil, err
	}
	return p, nil
}

// publishNativeHistoryRecoveryV2 creates immutable local files after native
// verification and an exact stopped-source recheck. Only the owner signs.
func publishNativeHistoryRecoveryV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, roles *RoleSecrets, h *policyRolloverHandoffV2, p *nativeHistoryRecoveryPlanV2, verify func(context.Context, *nativeHistoryRecoveryPlanV2) error) (validatorcomponent.ReleaseEvidenceV2File, error) {
	if verify == nil || roles == nil {
		return validatorcomponent.ReleaseEvidenceV2File{}, errors.New("native recovery publication lacks its verification or owner")
	}
	if err := requireNativeHistoryRecoveryStoppedV2(stateDir); err != nil {
		return validatorcomponent.ReleaseEvidenceV2File{}, err
	}
	if err := validateNativeHistoryRecoveryPlanV2(ctx, cfg, stateDir, base, h, p, true); err != nil {
		return validatorcomponent.ReleaseEvidenceV2File{}, err
	}
	approvedHash := p.PlanHash
	if err := verify(ctx, p); err != nil {
		return validatorcomponent.ReleaseEvidenceV2File{}, err
	}
	if p.PlanHash != approvedHash {
		return validatorcomponent.ReleaseEvidenceV2File{}, errors.New("native recovery verification changed the approved plan hash")
	}
	if err := validateNativeHistoryRecoveryPlanV2(ctx, cfg, stateDir, base, h, p, true); err != nil {
		return validatorcomponent.ReleaseEvidenceV2File{}, err
	}
	if err := requireNativeHistoryRecoveryStoppedV2(stateDir); err != nil {
		return validatorcomponent.ReleaseEvidenceV2File{}, err
	}
	root := nativeHistoryRecoveryRootV2(stateDir, p.PlanHash)
	path := filepath.Join(root, "handoff.evidence.json")
	if retained, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, path, nativeHistoryRecoveryMaximumBytesV2); err == nil {
		if _, err := readNativeHistoryRecoveryHandoffV2(ctx, cfg, stateDir, base, h, path, policyRolloverFile(path, retained).SHA256); err != nil {
			return validatorcomponent.ReleaseEvidenceV2File{}, err
		}
		return policyRolloverFile(path, retained), nil
	} else if !validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return validatorcomponent.ReleaseEvidenceV2File{}, err
	}
	if _, err := writeRuntimeEvidenceSetupV2(ctx, filepath.Join(root, "plan.json"), p, nativeHistoryRecoveryMaximumBytesV2); err != nil {
		return validatorcomponent.ReleaseEvidenceV2File{}, err
	}
	for _, request := range p.Validators {
		raw, err := nativeHistoryRecoveryRequestBytesV2(request)
		if err != nil {
			return validatorcomponent.ReleaseEvidenceV2File{}, err
		}
		if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, filepath.Join(root, fmt.Sprintf("validator-%d.json", request.Adoption.ValidatorID)), raw, validatorcomponent.ReleaseNativeHistoryRecoveryV2MaximumBytes); err != nil {
			return validatorcomponent.ReleaseEvidenceV2File{}, err
		}
	}
	signed, err := signEvidence(cfg, nativeHistoryRecoveryKindV2, p.PlanHash, p, roles.EVM["testnet-owner"])
	if err != nil {
		return validatorcomponent.ReleaseEvidenceV2File{}, err
	}
	if signed.Signer != common.HexToAddress(base.Roles.Owner) {
		return validatorcomponent.ReleaseEvidenceV2File{}, errors.New("native recovery signer differs from the deployment owner")
	}
	raw, err := writeRuntimeEvidenceSetupV2(ctx, path, signed, nativeHistoryRecoveryMaximumBytesV2)
	if err != nil {
		return validatorcomponent.ReleaseEvidenceV2File{}, err
	}
	return policyRolloverFile(path, raw), nil
}

// runNativeHistoryRecoveryV2 has no executor or native signer. Dry-run prints
// an exact reviewable plan; apply publishes only its separately approved receipt.
func runNativeHistoryRecoveryV2(ctx context.Context, cfg *ResolvedConfig, stateDir string, o cliOptions) (resultErr error) {
	base, err := loadInvocationPlan(cfg, stateDir, "native-history-recovery", o)
	if err != nil {
		return err
	}
	if err := prepareProvisionalResume(ctx, cfg, stateDir, "native-history-recovery", o, base); err != nil {
		return err
	}
	unlock, err := lockNativeHistoryRecoveryV2(stateDir)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, unlock()) }()
	if err := requireNativeHistoryRecoveryStoppedV2(stateDir); err != nil {
		return err
	}
	h, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, base)
	if err != nil {
		return err
	}
	operation, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	native, err := crv4.DialChainContext(operation, cfg.OperationalSubstrate)
	if err != nil {
		return err
	}
	defer native.API.Client.Close()
	genesis, err := types.NewHashFromHexString(cfg.Public.Chain.GenesisHash)
	if err != nil {
		return err
	}
	// The authenticated artifact is returned inside the reviewable plan. This
	// read-only connection must never persist runtime observations to live state.
	if err := native.EnableProvisionalRuntimeCompatibility(genesis, func(crv4.AuthenticatedRuntimeArtifact) error { return nil }); err != nil {
		return err
	}
	if !o.Apply {
		p, err := captureNativeHistoryRecoveryPlanV2(operation, cfg, stateDir, base, h, native, o.NativeRecoverySource, o.FirstNativeEpoch)
		if err != nil {
			return err
		}
		if err := requireNativeHistoryRecoveryStoppedV2(stateDir); err != nil {
			return err
		}
		return printResult(o.Format, p, nil)
	}
	var p nativeHistoryRecoveryPlanV2
	if _, err := readRuntimeEvidenceSetupV2(ctx, o.NativeRecoveryPlan, nativeHistoryRecoveryMaximumBytesV2, &p); err != nil {
		return err
	}
	if err := requireApproved(true, o.NativeRecoveryPlanHash, p.PlanHash); err != nil {
		return err
	}
	roles, err := loadExistingProvisionalRoles(cfg, stateDir)
	if err != nil {
		return err
	}
	receipt, err := publishNativeHistoryRecoveryV2(operation, cfg, stateDir, base, roles, h, &p, func(ctx context.Context, p *nativeHistoryRecoveryPlanV2) error {
		current, err := readPolicyRolloverHandoffV2(ctx, cfg, stateDir, base)
		if err != nil || !reflect.DeepEqual(current, h) {
			return errors.Join(errors.New("native recovery generation changed during approval"), err)
		}
		return verifyNativeHistoryRecoverySourcesV2(ctx, native, p)
	})
	return printResult(o.Format, receipt, err)
}

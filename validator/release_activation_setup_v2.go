//go:build linux || darwin

// `validator activate` is the independent validator's own activation flow.
// It mirrors the harness's fixed setup actions: prepare and sign one
// activation per configured operator from finalized chain snapshots, publish
// each through the evidence journal with any relayer key, wait for the
// activation epoch to begin, and only then render the five runtime inputs at
// the common finalized initial boundary and pin them in the configuration.
// Every chain read is authenticated exactly as RunRelease authenticates it.
package validator

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/miner/onchain"
	"github.com/urfoundation/sn/stabi"
)

const (
	ReleaseActivationSetupPreparedSchemaV2  = "urnetwork-validator-activation-prepared-v2"
	ReleaseActivationSetupCompletedSchemaV2 = "urnetwork-validator-activation-completed-v2"
	releaseActivationSetupDirectory         = "evidence-v2-setup"
	releaseActivationSetupPreparedFile      = "prepared.json"
	releaseActivationSetupCompletedFile     = "completed.json"
)

// ReleaseActivationSetupHeadV2 locates one finalized block of either chain.
type ReleaseActivationSetupHeadV2 struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}

// ReleaseActivationSetupPreparedV2 retains the original randomized consent
// bytes across restarts. Chain locations are discovery, re-authenticated
// through actual historical reads on every later operation.
type ReleaseActivationSetupPreparedV2 struct {
	Schema       string                       `json:"schema"`
	DeploymentID string                       `json:"deployment_id"`
	ValidatorID  uint64                       `json:"validator_id"`
	Netuid       uint16                       `json:"netuid"`
	PolicyHash   string                       `json:"policy_hash"`
	Epoch        uint64                       `json:"epoch"`
	Journal      string                       `json:"journal"`
	RuntimeHash  string                       `json:"runtime_hash"`
	Native       ReleaseActivationSetupHeadV2 `json:"native"`
	EVM          ReleaseActivationSetupHeadV2 `json:"evm"`
	Members      []ReleaseActivationMemberV2  `json:"members"`
}

// ReleaseActivationSetupCompletedV2 pins the context inputs written after
// every activation was publicly finalized at this same boundary.
type ReleaseActivationSetupCompletedV2 struct {
	Schema       string                       `json:"schema"`
	PreparedHash string                       `json:"prepared_hash"`
	Boundary     ReleaseActivationSetupHeadV2 `json:"boundary"`
}

// ReleaseActivationSetupPaths returns the setup journal files under the
// validator's own state directory.
func ReleaseActivationSetupPaths(cfg *ReleaseConfig) (prepared, completed string) {
	root := filepath.Join(cfg.StateDir, releaseActivationSetupDirectory)
	return filepath.Join(root, releaseActivationSetupPreparedFile), filepath.Join(root, releaseActivationSetupCompletedFile)
}

// Exact canonical setup bytes use the existing private no-overwrite publication.
func writeReleaseActivationSetupV2(ctx context.Context, path string, value any, limit uint64) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if uint64(len(encoded)) > limit {
		return nil, errors.New("validator activation setup exceeds its explicit control bound")
	}
	if _, err := WriteReleaseEvidenceV2File(ctx, path, encoded, limit); err != nil {
		return nil, err
	}
	return encoded, nil
}

// Required receipts must contain canonical data; callers separately admit the
// descriptor reader's verified initial absence.
func readReleaseActivationSetupV2(ctx context.Context, path string, limit uint64, value any) ([]byte, error) {
	encoded, err := ReadReleaseEvidenceV2SetupFile(ctx, path, limit)
	if err != nil {
		return nil, fmt.Errorf("validator activation setup %s: %w", filepath.Base(path), err)
	}
	if len(encoded) == 0 {
		return nil, fmt.Errorf("validator activation setup %s is empty", filepath.Base(path))
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return nil, fmt.Errorf("validator activation setup %s: %w", filepath.Base(path), err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("validator activation setup %s has trailing JSON", filepath.Base(path))
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return nil, errors.Join(errors.New("validator activation setup bytes are noncanonical"), err)
	}
	return encoded, ctx.Err()
}

// releaseActivationSetup owns one activate/complete invocation.
type releaseActivationSetup struct {
	cfg        *ReleaseConfig
	configPath string
	output     io.Writer
	hotkey     *crv4.Keypair
	clientKeys map[uint64]ed25519.PrivateKey
	chain      *ChainClient
	native     *crv4.Chain
	runtime    crv4.RuntimeArtifactIdentity
	deployment ReleaseActivationDeploymentV2
	limit      uint64
}

func (self *releaseActivationSetup) close() {
	if self.chain != nil {
		self.chain.Close()
	}
	if self.native != nil && self.native.API != nil && self.native.API.Client != nil {
		self.native.API.Client.Close()
	}
}

// Loads the pre-activation configuration and both signing key families, then
// dials and authenticates both chains exactly as RunRelease does.
func openReleaseActivationSetup(ctx context.Context, configPath string, output io.Writer) (*releaseActivationSetup, error) {
	if ctx == nil || output == nil {
		return nil, errors.New("activation setup context or output is nil")
	}
	cfg, err := LoadReleaseConfigPreActivation(configPath)
	if err != nil {
		return nil, err
	}
	hotkey, err := loadReleaseHotkey(cfg)
	if err != nil {
		return nil, err
	}
	setup := &releaseActivationSetup{cfg: cfg, configPath: configPath, output: output, hotkey: hotkey, clientKeys: map[uint64]ed25519.PrivateKey{}, limit: cfg.EvidenceV2.Bounds.MaxControlBytes}
	for _, operator := range cfg.Operators {
		seed, err := loadClientSeed(operator.ClientKeySeedFile)
		if err != nil {
			return nil, fmt.Errorf("no_id %d client key: %w (run `validator init --config=%s` to create it)", operator.NoID, err, configPath)
		}
		setup.clientKeys[operator.NoID] = ed25519.NewKeyFromSeed(seed)
	}
	genesis, err := parseHash32("genesis_hash", cfg.GenesisHash)
	if err != nil {
		return nil, err
	}
	policyHash, err := parseHash32("policy_hash", cfg.PolicyHash)
	if err != nil {
		return nil, err
	}
	setup.deployment = ReleaseActivationDeploymentV2{
		DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: genesis, Netuid: cfg.Netuid,
		Coordinator: [20]byte(common.HexToAddress(cfg.Coordinator)), SettlementVault: [20]byte(common.HexToAddress(cfg.SettlementVault)), PolicyHash: policyHash,
	}
	setup.runtime = releaseNativeRuntimeIdentity(cfg)
	setup.chain, err = DialReleaseChainContext(ctx, cfg.RPC, common.HexToAddress(cfg.Coordinator))
	if err != nil {
		return nil, err
	}
	setup.native, err = dialPinnedNative(ctx, cfg)
	if err != nil {
		setup.close()
		return nil, err
	}
	fmt.Fprintf(output, "activation: validator=%d netuid=%d deployment=%s operators=%d\n", cfg.ValidatorID, cfg.Netuid, cfg.DeploymentID, len(cfg.Operators))
	return setup, nil
}

func (self *releaseActivationSetup) coordinatorEpochAt(ctx context.Context, block uint64, hash [32]byte) (uint64, error) {
	current, err := chainViewAtHashContext(ctx, self.chain, block, hash, self.chain.coordinator.PackCurrentEpoch(), self.chain.coordinator.UnpackCurrentEpoch)
	if err != nil {
		return 0, fmt.Errorf("currentEpoch at finalized block %d: %w", block, err)
	}
	if current == nil || !current.IsUint64() || current.Uint64() == ^uint64(0) {
		return 0, errors.New("activation current epoch is invalid")
	}
	return current.Uint64(), nil
}

// Authenticates the policy, journal anchor and every configured operator at
// the activation epoch, all read at one canonical EVM block.
func (self *releaseActivationSetup) authenticateEpochAt(ctx context.Context, block uint64, hash [32]byte, epoch uint64) (common.Address, [32]byte, error) {
	policy, err := chainViewAtHashContext(ctx, self.chain, block, hash, self.chain.coordinator.PackPolicyAt(new(big.Int).SetUint64(epoch)), self.chain.coordinator.UnpackPolicyAt)
	if err != nil {
		return common.Address{}, [32]byte{}, err
	}
	if policy.PolicyHash != self.deployment.PolicyHash || policy.EffectiveEpoch > epoch {
		return common.Address{}, [32]byte{}, fmt.Errorf("coordinator policy at epoch %d is 0x%x (effective epoch %d); configuration pins 0x%x", epoch, policy.PolicyHash, policy.EffectiveEpoch, self.deployment.PolicyHash)
	}
	journal, err := chainViewAtHashContext(ctx, self.chain, block, hash, self.chain.coordinator.PackValidatorEvidence(), self.chain.coordinator.UnpackValidatorEvidence)
	if err != nil {
		return common.Address{}, [32]byte{}, err
	}
	if journal == (common.Address{}) || journal == common.Address(self.deployment.Coordinator) || journal == common.Address(self.deployment.SettlementVault) {
		return common.Address{}, [32]byte{}, errors.New("coordinator names no validator evidence journal")
	}
	runtimeHash, err := self.chain.ValidatorEvidenceRuntimeHashAtHashContext(ctx, journal, block, hash)
	if err != nil {
		return common.Address{}, [32]byte{}, err
	}
	for _, operator := range self.cfg.EvidenceV2.Operators {
		record, err := chainViewAtHashContext(ctx, self.chain, block, hash, self.chain.coordinator.PackOperatorAt(new(big.Int).SetUint64(operator.NoID), new(big.Int).SetUint64(epoch)), self.chain.coordinator.UnpackOperatorAt)
		if err != nil {
			return common.Address{}, [32]byte{}, err
		}
		if !record.Active || record.EffectiveEpoch > epoch {
			return common.Address{}, [32]byte{}, fmt.Errorf("operator no_id %d is not active at epoch %d", operator.NoID, epoch)
		}
	}
	return journal, runtimeHash, nil
}

// Reads the hotkey's native registration, stake and permit at one native
// block through the real CRv4 reader, resolving the UID by hotkey.
func (self *releaseActivationSetup) nativeObservationAt(ctx context.Context, number uint64, hash types.Hash) (crv4.ValidatorScheduleObservation, error) {
	observation, err := crv4.ReadValidatorScheduleAtContext(ctx, self.native, crv4.ValidatorScheduleQuery{
		GenesisHash: types.Hash(self.deployment.GenesisHash), BlockHash: hash, BlockNumber: number, Netuid: self.cfg.Netuid,
		Hotkey: self.hotkey.PublicKey(), MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs,
	}, HistoricalReleaseRuntimeArtifacts(self.runtime)...)
	if err != nil {
		return observation, fmt.Errorf("native validator observation at block %d: %w", number, err)
	}
	if !observation.Stake.MeetsNonSelfStakeAndPermit() {
		return observation, fmt.Errorf("hotkey %s lacks native stake/permit authority at block %d: uid=%d weighted_stake_rao=%d threshold_rao=%d permit=%t (register and stake first)", self.hotkey.Address(), number, observation.Stake.Identity.UID, observation.Stake.TotalStakeRao, observation.Stake.StakeThresholdRao, observation.Stake.Identity.ValidatorPermit)
	}
	return observation, nil
}

// Fresh activation is refused while any operator state survives on disk; a
// retained history needs an authenticated migration prefix, never a fresh one.
func requireFreshReleaseOperatorState(cfg *ReleaseConfig) error {
	for _, operator := range cfg.Operators {
		entries, err := os.ReadDir(operator.StateDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == filepath.Base(operator.ClientKeySeedFile) || name == filepath.Base(operator.NetworkJWTFile) || name == filepath.Base(operator.ClientJWTFile) {
				continue
			}
			return fmt.Errorf("operator no_id %d state directory %s already holds %s; a fresh activation cannot replace retained validator history", operator.NoID, operator.StateDir, name)
		}
	}
	return nil
}

// prepare builds (but does not sign or persist) one activation per operator
// from the current finalized snapshots of both chains.
func (self *releaseActivationSetup) prepare(ctx context.Context) (*ReleaseActivationSetupPreparedV2, error) {
	block, hash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	current, err := self.coordinatorEpochAt(ctx, block, hash)
	if err != nil {
		return nil, err
	}
	epoch := current + 1
	journal, runtimeHash, err := self.authenticateEpochAt(ctx, block, hash, epoch)
	if err != nil {
		return nil, err
	}
	nativeHash, err := crv4.FinalizedHeadContext(ctx, self.native)
	if err != nil {
		return nil, err
	}
	nativeHeader, err := self.native.HeaderAtContext(ctx, nativeHash)
	if err != nil {
		return nil, err
	}
	prepared := &ReleaseActivationSetupPreparedV2{
		Schema: ReleaseActivationSetupPreparedSchemaV2, DeploymentID: self.cfg.DeploymentID, ValidatorID: self.cfg.ValidatorID, Netuid: self.cfg.Netuid,
		PolicyHash: attemptHex32(self.deployment.PolicyHash), Epoch: epoch, Journal: journal.Hex(), RuntimeHash: attemptHex32(runtimeHash),
		Native: ReleaseActivationSetupHeadV2{Number: uint64(nativeHeader.Number), Hash: nativeHash.Hex()},
		EVM:    ReleaseActivationSetupHeadV2{Number: block, Hash: common.Hash(hash).Hex()},
	}
	observation, err := self.nativeObservationAt(ctx, prepared.Native.Number, nativeHash)
	if err != nil {
		return nil, err
	}
	snapshot := ReleaseActivationSnapshotV2{Epoch: epoch, NativeBlock: prepared.Native.Number, NativeHash: [32]byte(nativeHash), EVMBlock: block, EVMHash: hash}
	for _, operator := range self.cfg.EvidenceV2.Operators {
		key := self.clientKeys[operator.NoID]
		activation, err := BuildFreshReleaseActivationV2(self.deployment, snapshot, self.hotkey.PublicKey(), operator.NoID, [32]byte(key[ed25519.SeedSize:]))
		if err != nil {
			return nil, err
		}
		prepared.Members = append(prepared.Members, ReleaseActivationMemberV2{NoID: operator.NoID, ValidatorUID: observation.Stake.Identity.UID, Activation: activation})
	}
	canonical, err := self.chain.BlockHashContext(ctx, block)
	if err != nil || canonical != hash {
		return nil, errors.Join(errors.New("activation preparation EVM snapshot changed during reads"), err)
	}
	return prepared, nil
}

func (self *releaseActivationSetup) sign(prepared *ReleaseActivationSetupPreparedV2) error {
	for index := range prepared.Members {
		member := &prepared.Members[index]
		vpkSignature, hotkeySignature, err := SignReleaseActivationV2(member.Activation, self.hotkey, self.clientKeys[member.NoID])
		if err != nil {
			return err
		}
		member.VPKSignature, member.HotkeySignature = vpkSignature, hotkeySignature
	}
	return nil
}

// authenticatePrepared re-derives every field of a retained preparation from
// the configuration, the keys and actual finalized history, so a retained
// file cannot select a policy, prefix, uid, epoch or snapshot.
func (self *releaseActivationSetup) authenticatePrepared(ctx context.Context, prepared *ReleaseActivationSetupPreparedV2) error {
	if prepared == nil || prepared.Schema != ReleaseActivationSetupPreparedSchemaV2 || prepared.DeploymentID != self.cfg.DeploymentID || prepared.ValidatorID != self.cfg.ValidatorID || prepared.Netuid != self.cfg.Netuid ||
		prepared.PolicyHash != attemptHex32(self.deployment.PolicyHash) || prepared.Epoch == 0 || prepared.EVM.Number == 0 || prepared.Native.Number == 0 || len(prepared.Members) != len(self.cfg.EvidenceV2.Operators) {
		return errors.New("retained activation preparation differs from the configuration census or policy")
	}
	evmHash, err := parseHash32("prepared EVM snapshot", prepared.EVM.Hash)
	if err != nil {
		return err
	}
	nativeHash, err := parseHash32("prepared native snapshot", prepared.Native.Hash)
	if err != nil {
		return err
	}
	finalized, _, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil || finalized < prepared.EVM.Number {
		return errors.Join(errors.New("activation preparation EVM snapshot is not finalized"), err)
	}
	canonical, err := self.chain.BlockHashContext(ctx, prepared.EVM.Number)
	if err != nil || canonical != evmHash {
		return errors.Join(errors.New("activation preparation EVM snapshot is not canonical"), err)
	}
	current, err := self.coordinatorEpochAt(ctx, prepared.EVM.Number, evmHash)
	if err != nil {
		return err
	}
	if prepared.Epoch != current+1 {
		return errors.New("activation preparation epoch differs from the independently observed next epoch")
	}
	journal, runtimeHash, err := self.authenticateEpochAt(ctx, prepared.EVM.Number, evmHash, prepared.Epoch)
	if err != nil {
		return err
	}
	if !strings.EqualFold(prepared.Journal, journal.Hex()) || prepared.RuntimeHash != attemptHex32(runtimeHash) {
		return errors.New("activation preparation journal or runtime differs from the coordinator's anchor")
	}
	observation, err := self.nativeObservationAt(ctx, prepared.Native.Number, types.Hash(nativeHash))
	if err != nil {
		return err
	}
	snapshot := ReleaseActivationSnapshotV2{Epoch: prepared.Epoch, NativeBlock: prepared.Native.Number, NativeHash: nativeHash, EVMBlock: prepared.EVM.Number, EVMHash: evmHash}
	for index, operator := range self.cfg.EvidenceV2.Operators {
		member := prepared.Members[index]
		key := self.clientKeys[operator.NoID]
		expected, err := BuildFreshReleaseActivationV2(self.deployment, snapshot, self.hotkey.PublicKey(), operator.NoID, [32]byte(key[ed25519.SeedSize:]))
		if err != nil {
			return err
		}
		if member.NoID != operator.NoID || member.ValidatorUID != observation.Stake.Identity.UID {
			return errors.New("activation preparation omitted, reordered or re-identified a configured operator")
		}
		if err := member.Activation.Verify(expected, member.VPKSignature, member.HotkeySignature); err != nil {
			return fmt.Errorf("no_id %d retained activation: %w", member.NoID, err)
		}
	}
	return ctx.Err()
}

// readPrepared returns the retained preparation, or missing=true when the
// setup was never prepared.
func (self *releaseActivationSetup) readPrepared(ctx context.Context) (*ReleaseActivationSetupPreparedV2, []byte, bool, error) {
	path, _ := ReleaseActivationSetupPaths(self.cfg)
	var prepared ReleaseActivationSetupPreparedV2
	encoded, err := readReleaseActivationSetupV2(ctx, path, self.limit, &prepared)
	if ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return nil, nil, true, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	if err := self.authenticatePrepared(ctx, &prepared); err != nil {
		return nil, nil, false, err
	}
	return &prepared, encoded, false, nil
}

func (self *releaseActivationSetup) printPrepared(prepared *ReleaseActivationSetupPreparedV2) {
	fmt.Fprintf(self.output, "activation epoch: %d (EVM snapshot %d %s, native snapshot %d %s)\n", prepared.Epoch, prepared.EVM.Number, prepared.EVM.Hash, prepared.Native.Number, prepared.Native.Hash)
	fmt.Fprintf(self.output, "evidence journal: %s (runtime %s)\n", prepared.Journal, prepared.RuntimeHash)
	for _, member := range prepared.Members {
		digest, _ := member.Activation.Digest()
		fmt.Fprintf(self.output, "  no_id %d: validator uid %d, vpk %s, activation %s, signed=%t\n", member.NoID, member.ValidatorUID, attemptHex32(member.Activation.VPK), attemptHex32(digest), len(member.VPKSignature) == 64 && len(member.HotkeySignature) == 64)
	}
}

// loadOrPrepare returns the retained preparation, or prepares, signs and
// journals a new one. Without apply it only prints what would be prepared.
func (self *releaseActivationSetup) loadOrPrepare(ctx context.Context, apply bool) (*ReleaseActivationSetupPreparedV2, []byte, error) {
	prepared, encoded, missing, err := self.readPrepared(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !missing {
		fmt.Fprintf(self.output, "prepared: retained preparation authenticated\n")
		self.printPrepared(prepared)
		return prepared, encoded, nil
	}
	_, completedPath := ReleaseActivationSetupPaths(self.cfg)
	if _, err := os.Lstat(completedPath); err == nil {
		return nil, nil, errors.New("activation setup completion exists without its original preparation")
	}
	if err := requireFreshReleaseOperatorState(self.cfg); err != nil {
		return nil, nil, err
	}
	prepared, err = self.prepare(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !apply {
		fmt.Fprintf(self.output, "prepared (dry run): nothing written; re-run with --apply to sign and journal this preparation\n")
		self.printPrepared(prepared)
		return nil, nil, nil
	}
	if err := self.sign(prepared); err != nil {
		return nil, nil, err
	}
	path, _ := ReleaseActivationSetupPaths(self.cfg)
	encoded, err = writeReleaseActivationSetupV2(ctx, path, prepared, self.limit)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(self.output, "prepared: signed activations journaled at %s\n", path)
	self.printPrepared(prepared)
	return prepared, encoded, nil
}

func (self *releaseActivationSetup) journal(prepared *ReleaseActivationSetupPreparedV2) (common.Address, [32]byte, error) {
	runtimeHash, err := parseHash32("prepared runtime hash", prepared.RuntimeHash)
	if err != nil {
		return common.Address{}, [32]byte{}, err
	}
	return common.HexToAddress(prepared.Journal), runtimeHash, nil
}

// publicationState reports, at the current finalized EVM block, which members
// are published and at which height.
func (self *releaseActivationSetup) publicationState(ctx context.Context, prepared *ReleaseActivationSetupPreparedV2) ([]uint64, uint64, [32]byte, error) {
	journal, runtimeHash, err := self.journal(prepared)
	if err != nil {
		return nil, 0, [32]byte{}, err
	}
	block, hash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, 0, [32]byte{}, err
	}
	published := make([]uint64, len(prepared.Members))
	for index, member := range prepared.Members {
		publication, err := self.chain.ValidatorEvidenceActivationAtHashContext(ctx, journal, runtimeHash, member.Activation, block, hash)
		if errors.Is(err, ErrValidatorEvidenceAbsent) {
			fmt.Fprintf(self.output, "  no_id %d: not published at finalized block %d\n", member.NoID, block)
			continue
		}
		if err != nil {
			return nil, 0, [32]byte{}, err
		}
		published[index] = publication.PublishedBlock
		fmt.Fprintf(self.output, "  no_id %d: published at block %d\n", member.NoID, publication.PublishedBlock)
	}
	return published, block, hash, nil
}

// publish sends each unpublished member's consent through the journal with
// the relayer key. The relayer receives no authority; both signatures are the
// validator's. Without apply only the eth_call preflight runs.
func (self *releaseActivationSetup) publish(ctx context.Context, prepared *ReleaseActivationSetupPreparedV2, published []uint64, relayer *ecdsa.PrivateKey, apply bool) error {
	journal, runtimeHash, err := self.journal(prepared)
	if err != nil {
		return err
	}
	for index, member := range prepared.Members {
		if published[index] != 0 {
			continue
		}
		calldata, err := stabi.PackValidatorEvidenceActivation(member.Activation, member.Activation, member.VPKSignature, member.HotkeySignature)
		if err != nil {
			return err
		}
		fmt.Fprintf(self.output, "publishing no_id %d activation through %s\n", member.NoID, journal.Hex())
		receipt, err := onchain.Submit(ctx, onchain.SubmitParams{Contract: journal, Rpcs: self.cfg.RPC, Key: relayer, Calldata: calldata, ChainID: new(big.Int).SetUint64(self.cfg.ChainID), DryRun: !apply})
		if err != nil {
			return fmt.Errorf("publish no_id %d activation: %w", member.NoID, err)
		}
		if receipt == nil {
			continue
		}
		block, hash, err := self.chain.FinalizedBlockContext(ctx)
		if err != nil {
			return err
		}
		publication, err := self.chain.ValidatorEvidenceActivationAtHashContext(ctx, journal, runtimeHash, member.Activation, block, hash)
		if err != nil {
			return fmt.Errorf("no_id %d activation after publication: %w", member.NoID, err)
		}
		published[index] = publication.PublishedBlock
		fmt.Fprintf(self.output, "  no_id %d: published at block %d (tx %s)\n", member.NoID, publication.PublishedBlock, receipt.TxHash.Hex())
	}
	return nil
}

// boundary resolves the common initial boundary once the activation epoch has
// begun, authenticating every member there exactly as startup will.
func (self *releaseActivationSetup) boundary(ctx context.Context, prepared *ReleaseActivationSetupPreparedV2, encoded []byte) (*ReleaseActivationSetupCompletedV2, bool, error) {
	journal, runtimeHash, err := self.journal(prepared)
	if err != nil {
		return nil, false, err
	}
	block, hash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, false, err
	}
	current, err := self.coordinatorEpochAt(ctx, block, hash)
	if err != nil {
		return nil, false, err
	}
	if current < prepared.Epoch {
		fmt.Fprintf(self.output, "waiting: activation epoch %d has not begun (current epoch %d at finalized block %d); re-run `validator activate` or start `validator run` after it begins\n", prepared.Epoch, current, block)
		return nil, false, nil
	}
	start, err := self.chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, new(big.Int).SetUint64(prepared.Epoch))
	if err != nil {
		return nil, false, err
	}
	end, err := self.chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, new(big.Int).SetUint64(prepared.Epoch+1))
	if err != nil {
		return nil, false, err
	}
	published := make([]uint64, len(prepared.Members))
	for index, member := range prepared.Members {
		publication, err := self.chain.ValidatorEvidenceActivationAtHashContext(ctx, journal, runtimeHash, member.Activation, block, hash)
		if err != nil {
			return nil, false, fmt.Errorf("no_id %d activation publication: %w", member.NoID, err)
		}
		published[index] = publication.PublishedBlock
	}
	boundaryBlock, err := ReleaseActivationBoundaryBlockV2(start, end, prepared.EVM.Number, block, published)
	if err != nil {
		return nil, false, err
	}
	boundaryHash, err := self.chain.BlockHashContext(ctx, boundaryBlock)
	if err != nil {
		return nil, false, err
	}
	for _, member := range prepared.Members {
		authority := ReleaseActivationV2Authority{Expected: member.Activation, Journal: journal, RuntimeHash: runtimeHash, ValidatorUID: member.ValidatorUID, NativeRuntime: self.runtime}
		if _, err := self.chain.AuthenticateReleaseActivationV2Context(ctx, self.native, authority, member.Activation, member.VPKSignature, member.HotkeySignature, boundaryBlock, boundaryHash); err != nil {
			return nil, false, fmt.Errorf("no_id %d activation at boundary %d: %w", member.NoID, boundaryBlock, err)
		}
	}
	fmt.Fprintf(self.output, "boundary: block %d (%s); epoch %d spans blocks [%d, %d)\n", boundaryBlock, common.Hash(boundaryHash).Hex(), prepared.Epoch, start, end)
	return &ReleaseActivationSetupCompletedV2{Schema: ReleaseActivationSetupCompletedSchemaV2, PreparedHash: attemptHex32(sha256.Sum256(encoded)), Boundary: ReleaseActivationSetupHeadV2{Number: boundaryBlock, Hash: common.Hash(boundaryHash).Hex()}}, true, nil
}

// operatorPaths keeps paths an operator pre-declared and fills the rest with
// the default layout beside the validator's state directory.
func (self *releaseActivationSetup) operatorPaths(operator ReleaseEvidenceV2OperatorConfig) ReleaseEvidenceV2OperatorPaths {
	paths := DefaultReleaseEvidenceV2OperatorPaths(filepath.Dir(self.cfg.StateDir), operator.NoID)
	for index, file := range operator.Files() {
		if file.Path != "" {
			paths.Files[index] = file.Path
		}
	}
	if operator.ReplayScratchRoot != "" {
		paths.ReplayScratchRoot = operator.ReplayScratchRoot
	}
	if operator.SealScratchRoot != "" {
		paths.SealScratchRoot = operator.SealScratchRoot
	}
	return paths
}

// complete renders and writes the five inputs per operator at the boundary,
// journals the completion, pins the references in the configuration file and
// proves the strict loader and the production activation reader accept them.
func (self *releaseActivationSetup) complete(ctx context.Context, prepared *ReleaseActivationSetupPreparedV2, completed *ReleaseActivationSetupCompletedV2) error {
	journal, runtimeHash, err := self.journal(prepared)
	if err != nil {
		return err
	}
	boundaryHash, err := parseHash32("completed boundary", completed.Boundary.Hash)
	if err != nil {
		return err
	}
	boundary := ReleaseActivationBoundaryV2{Block: completed.Boundary.Number, Hash: boundaryHash}
	ledger := ReleaseActivationLedgerV2{DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID, GenesisHash: strings.ToLower(self.cfg.GenesisHash), Netuid: self.cfg.Netuid, ValidatorID: self.cfg.ValidatorID}
	rendered := make([]ReleaseEvidenceV2OperatorConfig, 0, len(prepared.Members))
	for index, operator := range self.cfg.EvidenceV2.Operators {
		member := prepared.Members[index]
		if member.NoID != operator.NoID {
			return errors.New("activation members differ from the configured census")
		}
		entry, inputs, err := RenderReleaseActivationInputsV2(ledger, member, [20]byte(journal), runtimeHash, boundary, self.cfg.EvidenceV2.Bounds, self.operatorPaths(operator))
		if err != nil {
			return err
		}
		for fileIndex, reference := range entry.Files() {
			if _, err := WriteReleaseEvidenceV2File(ctx, reference.Path, inputs[reference.Path], ReleaseEvidenceV2ReferenceLimit(self.cfg.EvidenceV2.Bounds, fileIndex)); err != nil {
				return fmt.Errorf("no_id %d %s: %w", operator.NoID, filepath.Base(reference.Path), err)
			}
		}
		fmt.Fprintf(self.output, "rendered: no_id %d inputs under %s\n", operator.NoID, filepath.Dir(entry.Activation.Path))
		rendered = append(rendered, entry)
	}
	_, completedPath := ReleaseActivationSetupPaths(self.cfg)
	var retained ReleaseActivationSetupCompletedV2
	if _, err := readReleaseActivationSetupV2(ctx, completedPath, self.limit, &retained); err == nil {
		if retained != *completed {
			return errors.New("retained activation completion differs from the resolved boundary")
		}
	} else if !ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		return err
	} else if _, err := writeReleaseActivationSetupV2(ctx, completedPath, completed, self.limit); err != nil {
		return err
	}
	if err := RewriteReleaseConfigEvidenceV2Operators(self.configPath, rendered); err != nil {
		return err
	}
	fmt.Fprintf(self.output, "configuration: evidence_v2.operators pinned in %s\n", self.configPath)
	cfg, err := LoadReleaseConfig(self.configPath)
	if err != nil {
		return fmt.Errorf("rendered configuration does not load strictly: %w", err)
	}
	if _, err := loadReleaseEvidenceV2ActivationInputs(ctx, cfg, self.chain, self.native, self.hotkey.PublicKey()); err != nil {
		return fmt.Errorf("rendered activation inputs do not load: %w", err)
	}
	fmt.Fprintf(self.output, "activation complete: the production loader accepts every rendered input; start `validator run --config=%s`\n", self.configPath)
	return nil
}

// ReleaseActivationOptions controls one `validator activate` invocation.
type ReleaseActivationOptions struct {
	ConfigPath     string
	RelayerKeyFile string
	Apply          bool
	Output         io.Writer
}

// RunReleaseActivation drives the whole flow as far as the release lifecycle
// allows right now: prepare, publish, then complete once the activation
// epoch has begun. Without Apply nothing is written, signed or sent.
func RunReleaseActivation(ctx context.Context, options ReleaseActivationOptions) error {
	setup, err := openReleaseActivationSetup(ctx, options.ConfigPath, options.Output)
	if err != nil {
		return err
	}
	defer setup.close()
	prepared, encoded, err := setup.loadOrPrepare(ctx, options.Apply)
	if err != nil || prepared == nil {
		return err
	}
	fmt.Fprintf(setup.output, "publication at the finalized EVM head:\n")
	published, _, _, err := setup.publicationState(ctx, prepared)
	if err != nil {
		return err
	}
	unpublished := 0
	for _, block := range published {
		if block == 0 {
			unpublished++
		}
	}
	if unpublished > 0 {
		if options.RelayerKeyFile == "" {
			fmt.Fprintf(setup.output, "%d activation(s) are not published; re-run with --relayer_key_file=<evm key funded for gas> to publish them (the relayer receives no authority)\n", unpublished)
			return nil
		}
		relayer, err := onchain.LoadKeyFile(options.RelayerKeyFile)
		if err != nil {
			return err
		}
		if err := setup.publish(ctx, prepared, published, relayer, options.Apply); err != nil {
			return err
		}
		if !options.Apply {
			fmt.Fprintf(setup.output, "dry run: publication preflight only; re-run with --apply to send\n")
			return nil
		}
	}
	completed, started, err := setup.boundary(ctx, prepared, encoded)
	if err != nil || !started {
		return err
	}
	if !options.Apply {
		fmt.Fprintf(setup.output, "dry run: the activation epoch has begun and every activation is finalized; re-run with --apply to render the inputs and pin them in the configuration\n")
		return nil
	}
	return setup.complete(ctx, prepared, completed)
}

// CompletePendingReleaseActivation finishes an already prepared and published
// activation without any key beyond the validator's own: it renders the
// inputs once the activation epoch has begun. RunRelease calls it when the
// configuration's evidence_v2 inputs are still unrendered.
func CompletePendingReleaseActivation(ctx context.Context, configPath string, output io.Writer) error {
	setup, err := openReleaseActivationSetup(ctx, configPath, output)
	if err != nil {
		return err
	}
	defer setup.close()
	prepared, encoded, missing, err := setup.readPrepared(ctx)
	if err != nil {
		return err
	}
	if missing {
		return fmt.Errorf("no activation has been prepared for %s; run `validator activate --config=%s --apply`", configPath, configPath)
	}
	setup.printPrepared(prepared)
	published, _, _, err := setup.publicationState(ctx, prepared)
	if err != nil {
		return err
	}
	for index, block := range published {
		if block == 0 {
			return fmt.Errorf("no_id %d activation is not published; run `validator activate --config=%s --relayer_key_file=<path> --apply`", prepared.Members[index].NoID, configPath)
		}
	}
	completed, started, err := setup.boundary(ctx, prepared, encoded)
	if err != nil {
		return err
	}
	if !started {
		return fmt.Errorf("activation epoch %d has not begun; the release lifecycle cannot render the inputs yet", prepared.Epoch)
	}
	return setup.complete(ctx, prepared, completed)
}

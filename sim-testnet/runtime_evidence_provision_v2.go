//go:build linux || darwin

// Fixed setup actions prepare all original identities once, publish their
// actual consents through the existing keeper, then retain a common finalized
// initial boundary and exact runtime input files before config rendering.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const runtimeEvidenceActivationBoundaryActionId = "evidence.activation-boundary"

// A prepared member retains the original randomized consent bytes across
// process restarts. Native uid is only a historical observation at Native.
type runtimeEvidenceActivationMemberV2 struct {
	ValidatorId     uint64                               `json:"validator_id"`
	NoId            uint64                               `json:"no_id"`
	ValidatorUid    uint16                               `json:"validator_uid"`
	Activation      protocol.ValidatorEvidenceActivation `json:"activation"`
	VpkSignature    []byte                               `json:"vpk_signature"`
	HotkeySignature []byte                               `json:"hotkey_signature"`
}

// Chain locations are discovery, reauthenticated through actual historical
// reads on every setup/recovery operation. The plan fixes role and quota owners.
type runtimeEvidenceActivationPreparedV2 struct {
	Schema     string                              `json:"schema"`
	PlanHash   string                              `json:"plan_hash"`
	ConfigHash string                              `json:"config_hash"`
	PolicyHash string                              `json:"policy_hash"`
	Epoch      uint64                              `json:"epoch"`
	Native     ChainHead                           `json:"native"`
	Evm        ChainHead                           `json:"evm"`
	Members    []runtimeEvidenceActivationMemberV2 `json:"members"`
}

// The immutable completed output pins only the context inputs actually
// written after every activation was publicly finalized at this same boundary.
type runtimeEvidenceActivationCompletedV2 struct {
	Schema       string    `json:"schema"`
	PlanHash     string    `json:"plan_hash"`
	PreparedHash string    `json:"prepared_hash"`
	Boundary     ChainHead `json:"boundary"`
}

func runtimeEvidenceActivationActionId(validatorId, noId int) string {
	return fmt.Sprintf("evidence.activate.%d.%d", validatorId, noId)
}

// The entire setup control file uses the smallest explicitly approved
// validator control allowance; topology never creates a new byte budget.
func runtimeEvidenceProvisionLimit(cfg *ResolvedConfig) (uint64, error) {
	if cfg == nil || cfg.Config == nil || !cfg.Config.ProvisionValidatorEvidenceV2 {
		return 0, errors.New("validator evidence provisioning was not explicitly configured")
	}
	if err := validateSimulatorEvidenceV2Census(cfg.Config); err != nil {
		return 0, err
	}
	limit := ^uint64(0)
	for _, configured := range cfg.Config.ValidatorEvidenceV2 {
		limit = min(limit, configured.Evidence.Bounds.MaxControlBytes)
	}
	if limit == 0 || limit >= uint64(^uint(0)>>1) {
		return 0, errors.New("validator evidence setup control bound is invalid")
	}
	return limit, nil
}

// Exact canonical setup bytes use existing private no-overwrite publication.
func writeRuntimeEvidenceSetupV2(ctx context.Context, path string, value any, limit uint64) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if uint64(len(encoded)) > limit {
		return nil, errors.New("validator evidence setup exceeds its explicit control bound")
	}
	if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, path, encoded, limit); err != nil {
		return nil, err
	}
	return encoded, nil
}

func readRuntimeEvidenceSetupV2(ctx context.Context, path string, limit uint64, value any) ([]byte, error) {
	encoded, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, path, limit)
	if err != nil {
		return nil, err
	}
	if err := decodeStrictJSONBytes(encoded, value); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return nil, errors.Join(errors.New("validator evidence setup bytes are noncanonical"), err)
	}
	return encoded, ctx.Err()
}

// Public identities and both seeds come from the existing admitted role owner.
// This validates the seed/public halves before any signature or chain write.
func runtimeEvidenceActivationKeysV2(roles *RoleSecrets, validatorId, noId uint64) (*crv4.Keypair, ed25519.PrivateKey, error) {
	if roles == nil {
		return nil, nil, errors.New("validator evidence role owner is absent")
	}
	hotkeyRole := roles.Substrate[validatorHotkeyLabel(int(validatorId))]
	hotkeySeed, err := hex.DecodeString(hotkeyRole.SeedHex)
	if err != nil || len(hotkeySeed) != 32 {
		return nil, nil, errors.New("validator evidence hotkey seed is invalid")
	}
	hotkey, err := crv4.KeypairFromSeed([32]byte(hotkeySeed))
	if err != nil {
		return nil, nil, err
	}
	publicKey := hotkey.PublicKey()
	if hex.EncodeToString(publicKey[:]) != hotkeyRole.PublicKeyHex {
		return nil, nil, errors.New("validator evidence hotkey role differs from its seed")
	}
	clientRole := roles.Clients[fmt.Sprintf("validator-%d-no-%d", validatorId, noId)]
	seed, err := hex.DecodeString(clientRole.SeedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, nil, errors.New("validator evidence original client seed is invalid")
	}
	key := ed25519.NewKeyFromSeed(seed)
	if hex.EncodeToString(key[ed25519.SeedSize:]) != clientRole.PublicKeyHex {
		return nil, nil, errors.New("validator evidence client role differs from its seed")
	}
	return hotkey, key, nil
}

// Historical calldata always uses a canonical EVM hash. Raw tuple encoding
// and explicit canonical rechecks remain owned by the existing chain client.
func (self *Executor) runtimeEvidenceActivationChainV2(ctx context.Context) (*validatorcomponent.ChainClient, error) {
	if self == nil || self.cfg == nil || self.plan == nil || self.plan.ValidatorEvidence == nil || self.substrate == nil || self.substrate.chain == nil || self.keeper == nil {
		return nil, errors.New("validator evidence activation runtime owners are incomplete")
	}
	endpoint := self.cfg.OperationalEVM
	if self.independentEVM != nil && self.keeper.client == self.independentEVM {
		endpoint = self.cfg.Public.Chain.EVMPublicReadEndpoint
	}
	return validatorcomponent.DialReleaseChainContext(ctx, []string{endpoint}, self.plan.ValidatorEvidence.Coordinator)
}

func (self *Executor) runtimeEvidenceNativeIdentityV2() crv4.RuntimeArtifactIdentity {
	return crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: self.cfg.Public.Chain.ExpectedRuntimeSpec,
		TransactionVersion: self.cfg.Public.Chain.ExpectedTransactionVersion, StateVersion: self.cfg.Public.Chain.ExpectedStateVersion},
		CodeHash: self.cfg.Release.Runtime.CodeHash, MetadataHash: self.cfg.Release.Runtime.MetadataHash}
}

// Fresh activation is refused if any mutable statistics or ledger survives.
// Existing namespace policy owns explicit migrations; this path never asserts
// that a nonempty historical ledger has an empty prefix.
func requireRuntimeEvidenceFreshStateV2(cfg *ResolvedConfig, stateDir string) error {
	for validatorId := 1; validatorId <= cfg.Config.Topology.Validators; validatorId++ {
		if err := requireValidatorStateStopped(stateDir, validatorId); err != nil {
			return err
		}
		state := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorId), "state")
		legacy, signed, err := classifyValidatorAttemptState(state, cfg.Config.Topology.Operators)
		if err != nil || legacy || signed {
			return errors.Join(errors.New("fresh evidence activation cannot replace retained validator history"), err)
		}
	}
	return nil
}

// Prepared snapshot locators cannot choose policy, roles, prefix, uid or
// epoch. All are reconstructed from immutable setup pins and actual history.
func (self *Executor) authenticateRuntimeEvidencePreparedV2(ctx context.Context, chain *validatorcomponent.ChainClient, prepared *runtimeEvidenceActivationPreparedV2) error {
	if prepared == nil || prepared.Schema != "urnetwork-sim-evidence-activation-prepared-v2" || prepared.PlanHash != self.plan.PlanHash || prepared.ConfigHash != self.cfg.ConfigHash || prepared.PolicyHash != self.cfg.PolicyHash ||
		len(prepared.Members) != self.cfg.Config.Topology.Validators*self.cfg.Config.Topology.Operators || prepared.Epoch == 0 || prepared.Evm.Number == 0 || prepared.Native.Number == 0 {
		return errors.New("validator evidence prepared census or plan differs")
	}
	policyHash, err := decodeHex32("activation policy", self.cfg.PolicyHash)
	if err != nil {
		return err
	}
	evmHash, err := decodeHex32("prepared activation EVM hash", prepared.Evm.Hash)
	if err != nil {
		return err
	}
	nativeHash, err := decodeHex32("prepared activation native hash", prepared.Native.Hash)
	if err != nil {
		return err
	}
	finalized, _, err := chain.FinalizedBlockContext(ctx)
	if err != nil || finalized < prepared.Evm.Number {
		return errors.Join(errors.New("activation preparation EVM snapshot is not finalized"), err)
	}
	canonical, err := chain.BlockHashContext(ctx, prepared.Evm.Number)
	if err != nil || canonical != evmHash {
		return errors.Join(errors.New("activation preparation EVM snapshot is not canonical"), err)
	}
	coordinator := stabi.NewSTCoordinator()
	current, err := rawCoordinatorCallAt(ctx, self.keeper, self.plan.ValidatorEvidence.Coordinator, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch, prepared.Evm.Number)
	if err != nil || current == nil || !current.IsUint64() || current.Uint64() == ^uint64(0) || prepared.Epoch != current.Uint64()+1 {
		return errors.Join(errors.New("activation preparation epoch differs from independently observed next epoch"), err)
	}
	policy, err := rawCoordinatorCallAt(ctx, self.keeper, self.plan.ValidatorEvidence.Coordinator, coordinator.PackPolicyAt(new(big.Int).SetUint64(prepared.Epoch)), coordinator.UnpackPolicyAt, prepared.Evm.Number)
	if err != nil || policy.PolicyHash != policyHash || policy.EffectiveEpoch > prepared.Epoch {
		return errors.Join(errors.New("activation preparation policy differs"), err)
	}
	for index, member := range prepared.Members {
		validatorId, noId := uint64(index/self.cfg.Config.Topology.Operators+1), uint64(index%self.cfg.Config.Topology.Operators+1)
		if member.ValidatorId != validatorId || member.NoId != noId {
			return errors.New("activation preparation omitted or reordered a configured pair")
		}
		hotkey, key, err := runtimeEvidenceActivationKeysV2(self.roles, validatorId, noId)
		if err != nil {
			return err
		}
		observation, err := crv4.ReadValidatorScheduleAtContext(ctx, self.substrate.chain, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(self.plan.ValidatorEvidence.GenesisHash), BlockHash: types.Hash(nativeHash), BlockNumber: prepared.Native.Number, Netuid: self.cfg.Netuid, Hotkey: hotkey.PublicKey(), MaximumSubnetUIDs: uint32(hyperparameterUint64(self.cfg.Hyperparameters.OwnerControlled["max_allowed_uids"]))}, self.runtimeEvidenceNativeIdentityV2())
		if err != nil || !observation.Stake.MeetsNonSelfStakeAndPermit() || observation.Stake.Identity.UID != member.ValidatorUid {
			return errors.Join(errors.New("activation preparation native eligibility or historical uid differs"), err)
		}
		operator, err := rawCoordinatorCallAt(ctx, self.keeper, self.plan.ValidatorEvidence.Coordinator, coordinator.PackOperatorAt(new(big.Int).SetUint64(noId), new(big.Int).SetUint64(prepared.Epoch)), coordinator.UnpackOperatorAt, prepared.Evm.Number)
		if err != nil || !operator.Active || operator.EffectiveEpoch > prepared.Epoch {
			return errors.Join(errors.New("activation preparation operator is not active"), err)
		}
		expected := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: testnetChainID, GenesisHash: [32]byte(self.plan.ValidatorEvidence.GenesisHash), Netuid: self.cfg.Netuid,
			Coordinator: [20]byte(self.plan.ValidatorEvidence.Coordinator), SettlementVault: [20]byte(self.plan.ValidatorEvidence.SettlementVault), DeploymentIDHash: [32]byte(self.plan.ValidatorEvidence.DeploymentIDHash), PolicyHash: policyHash, Epoch: prepared.Epoch},
			Hotkey: hotkey.PublicKey(), NoID: noId, VPK: [32]byte(key[ed25519.SeedSize:]), FirstSequence: 1, NativeBlock: prepared.Native.Number, NativeHash: nativeHash, EVMBlock: prepared.Evm.Number, EVMHash: evmHash}
		if err := member.Activation.Verify(expected, member.VpkSignature, member.HotkeySignature); err != nil {
			return err
		}
	}
	canonical, err = chain.BlockHashContext(ctx, prepared.Evm.Number)
	if err != nil || canonical != evmHash {
		return errors.Join(errors.New("activation preparation EVM snapshot changed after history reads"), err)
	}
	return ctx.Err()
}

// Every randomized signature is durable before the first transaction. A
// restart reuses those exact bytes and authenticates the historical inputs.
func (self *Executor) prepareRuntimeEvidenceActivationsV2(ctx context.Context, chain *validatorcomponent.ChainClient) (*runtimeEvidenceActivationPreparedV2, []byte, error) {
	limit, err := runtimeEvidenceProvisionLimit(self.cfg)
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(self.stateDir, "evidence-v2-setup", "prepared.json")
	var prepared runtimeEvidenceActivationPreparedV2
	encoded, err := readRuntimeEvidenceSetupV2(ctx, path, limit, &prepared)
	if err == nil {
		if err := self.authenticateRuntimeEvidencePreparedV2(ctx, chain, &prepared); err != nil {
			return nil, nil, err
		}
		return &prepared, encoded, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if err := requireRuntimeEvidenceFreshStateV2(self.cfg, self.stateDir); err != nil {
		return nil, nil, err
	}
	policyHash, err := decodeHex32("activation policy", self.cfg.PolicyHash)
	if err != nil {
		return nil, nil, err
	}
	block, hash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	nativeHash, err := crv4.FinalizedHeadContext(ctx, self.substrate.chain)
	if err != nil {
		return nil, nil, err
	}
	nativeHeader, err := self.substrate.chain.HeaderAtContext(ctx, nativeHash)
	if err != nil {
		return nil, nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	current, err := rawCoordinatorCallAt(ctx, self.keeper, self.plan.ValidatorEvidence.Coordinator, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch, block)
	if err != nil || current == nil || !current.IsUint64() || current.Uint64() == ^uint64(0) {
		return nil, nil, errors.Join(errors.New("activation preparation current epoch is invalid"), err)
	}
	prepared = runtimeEvidenceActivationPreparedV2{Schema: "urnetwork-sim-evidence-activation-prepared-v2", PlanHash: self.plan.PlanHash, ConfigHash: self.cfg.ConfigHash, PolicyHash: self.cfg.PolicyHash, Epoch: current.Uint64() + 1,
		Native: ChainHead{Number: uint64(nativeHeader.Number), Hash: nativeHash.Hex()}, Evm: ChainHead{Number: block, Hash: common.Hash(hash).Hex()}}
	for validatorId := 1; validatorId <= self.cfg.Config.Topology.Validators; validatorId++ {
		for noId := 1; noId <= self.cfg.Config.Topology.Operators; noId++ {
			hotkey, key, err := runtimeEvidenceActivationKeysV2(self.roles, uint64(validatorId), uint64(noId))
			if err != nil {
				return nil, nil, err
			}
			observation, err := crv4.ReadValidatorScheduleAtContext(ctx, self.substrate.chain, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(self.plan.ValidatorEvidence.GenesisHash), BlockHash: nativeHash, BlockNumber: prepared.Native.Number, Netuid: self.cfg.Netuid, Hotkey: hotkey.PublicKey(), MaximumSubnetUIDs: uint32(hyperparameterUint64(self.cfg.Hyperparameters.OwnerControlled["max_allowed_uids"]))}, self.runtimeEvidenceNativeIdentityV2())
			if err != nil || !observation.Stake.MeetsNonSelfStakeAndPermit() {
				return nil, nil, errors.Join(errors.New("activation preparation lacks native stake or permit"), err)
			}
			activation := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: testnetChainID, GenesisHash: [32]byte(self.plan.ValidatorEvidence.GenesisHash), Netuid: self.cfg.Netuid,
				Coordinator: [20]byte(self.plan.ValidatorEvidence.Coordinator), SettlementVault: [20]byte(self.plan.ValidatorEvidence.SettlementVault), DeploymentIDHash: [32]byte(self.plan.ValidatorEvidence.DeploymentIDHash), PolicyHash: policyHash, Epoch: prepared.Epoch},
				Hotkey: hotkey.PublicKey(), NoID: uint64(noId), VPK: [32]byte(key[ed25519.SeedSize:]), FirstSequence: 1, NativeBlock: prepared.Native.Number, NativeHash: [32]byte(nativeHash), EVMBlock: block, EVMHash: hash}
			vpkSignature, err := activation.SignVPK(key)
			if err != nil {
				return nil, nil, err
			}
			digest, err := activation.Digest()
			if err != nil {
				return nil, nil, err
			}
			hotkeySignature, err := hotkey.Sign(digest[:])
			if err != nil {
				return nil, nil, err
			}
			prepared.Members = append(prepared.Members, runtimeEvidenceActivationMemberV2{ValidatorId: uint64(validatorId), NoId: uint64(noId), ValidatorUid: observation.Stake.Identity.UID, Activation: activation, VpkSignature: vpkSignature, HotkeySignature: hotkeySignature})
		}
	}
	if err := self.authenticateRuntimeEvidencePreparedV2(ctx, chain, &prepared); err != nil {
		return nil, nil, err
	}
	encoded, err = writeRuntimeEvidenceSetupV2(ctx, path, &prepared, limit)
	if err != nil {
		return nil, nil, err
	}
	return &prepared, encoded, nil
}

// Fixed action routing is checked against the complete approved plan before
// the retained activation is selected. Other slots cannot consume this budget.
func (self *Executor) runtimeEvidenceActivationMemberV2(action Action, prepared *runtimeEvidenceActivationPreparedV2) (*runtimeEvidenceActivationMemberV2, error) {
	validatorId, firstErr := strconv.Atoi(action.Parameters["validator_id"])
	noId, secondErr := strconv.Atoi(action.Parameters["no_id"])
	if firstErr != nil || secondErr != nil || validatorId < 1 || validatorId > self.cfg.Config.Topology.Validators || noId < 1 || noId > self.cfg.Config.Topology.Operators ||
		action.ID != runtimeEvidenceActivationActionId(validatorId, noId) || action.Kind != "evm-transaction" || action.Target != self.plan.ValidatorEvidence.Address.Hex() || action.Parameters["mode"] != "fresh-v2" || action.Parameters["policy_hash"] != self.cfg.PolicyHash {
		return nil, errors.New("activation action differs from the approved fixed source and quota owner")
	}
	actualHash, err := actionIntentHash(action)
	if err != nil || actualHash != action.IntentHash {
		return nil, errors.Join(errors.New("activation action content is not its approved intent"), err)
	}
	for _, expected := range self.plan.Actions {
		if expected.ID == action.ID && expected.IntentHash == action.IntentHash {
			return &prepared.Members[(validatorId-1)*self.cfg.Config.Topology.Operators+noId-1], nil
		}
	}
	return nil, errors.New("activation action is absent from the approved plan")
}

// The genuine manager retains nonce and signed bytes before broadcasting.
// An exact third-party publication is read first and consumes no fresh nonce.
func (self *Executor) publishRuntimeEvidenceActivationV2(ctx context.Context, action Action) error {
	chain, err := self.runtimeEvidenceActivationChainV2(ctx)
	if err != nil {
		return err
	}
	defer chain.Close()
	prepared, _, err := self.prepareRuntimeEvidenceActivationsV2(ctx, chain)
	if err != nil {
		return err
	}
	member, err := self.runtimeEvidenceActivationMemberV2(action, prepared)
	if err != nil {
		return err
	}
	block, hash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	_, err = chain.ValidatorEvidenceActivationAtHashContext(ctx, self.plan.ValidatorEvidence.Address, [32]byte(self.plan.ValidatorEvidence.RuntimeCodeHash), member.Activation, block, hash)
	if err == nil {
		return nil
	}
	if !errors.Is(err, validatorcomponent.ErrValidatorEvidenceAbsent) {
		return err
	}
	if err := requireRuntimeEvidenceFreshStateV2(self.cfg, self.stateDir); err != nil {
		return err
	}
	calldata, err := stabi.PackValidatorEvidenceActivation(member.Activation, member.Activation, member.VpkSignature, member.HotkeySignature)
	if err != nil {
		return err
	}
	target := self.plan.ValidatorEvidence.Address
	_, sendErr := self.keeper.Send(ctx, self.plan.PlanHash, action, &target, new(big.Int), calldata)
	block, hash, err = chain.FinalizedBlockContext(ctx)
	if err != nil {
		return errors.Join(sendErr, err)
	}
	_, err = chain.ValidatorEvidenceActivationAtHashContext(ctx, target, [32]byte(self.plan.ValidatorEvidence.RuntimeCodeHash), member.Activation, block, hash)
	if err != nil {
		return errors.Join(sendErr, err)
	}
	return errors.Join(sendErr, ctx.Err())
}

// Finalized activation publication must precede the first cut. The boundary
// is deterministic from public history, so a crash before any file publication
// cannot select different context bytes or silently discard an elapsed epoch.
func (self *Executor) completeRuntimeEvidenceActivationV2(ctx context.Context) error {
	chain, err := self.runtimeEvidenceActivationChainV2(ctx)
	if err != nil {
		return err
	}
	defer chain.Close()
	prepared, encoded, err := self.prepareRuntimeEvidenceActivationsV2(ctx, chain)
	if err != nil {
		return err
	}
	completed, err := self.runtimeEvidenceActivationBoundaryV2(ctx, chain, prepared, encoded)
	if err != nil {
		return err
	}
	return self.retainRuntimeEvidenceInputsV2(ctx, prepared, encoded, completed)
}

// Completion is published last. Every prior fixed input is immutable, so an
// interrupted last-member write resumes without replacing earlier signatures.
func (self *Executor) retainRuntimeEvidenceInputsV2(ctx context.Context, prepared *runtimeEvidenceActivationPreparedV2, encoded []byte, completed *runtimeEvidenceActivationCompletedV2) error {
	limit, err := runtimeEvidenceProvisionLimit(self.cfg)
	if err != nil {
		return err
	}
	if completed == nil || completed.PreparedHash != fmt.Sprintf("0x%x", sha256.Sum256(encoded)) {
		return errors.New("activation completion refers to different prepared bytes")
	}
	configured, inputs, err := runtimeEvidenceFixedInputsV2(self.cfg, self.plan, self.stateDir, self.roles, prepared, completed)
	if err != nil {
		return err
	}
	for _, value := range configured {
		for _, operator := range value.Evidence.Operators {
			for index, reference := range operator.Files() {
				if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, reference.Path, inputs[reference.Path], runtimeEvidenceV2ReferenceLimit(value.Evidence.Bounds, index)); err != nil {
					return err
				}
			}
		}
	}
	_, err = writeRuntimeEvidenceSetupV2(ctx, filepath.Join(self.stateDir, "evidence-v2-setup", "completed.json"), completed, limit)
	return err
}

// Epoch geometry and publication heights, not a mutable clock or a private
// locator, choose the common initial boundary. A late restart can replay it.
func (self *Executor) runtimeEvidenceActivationBoundaryV2(ctx context.Context, chain *validatorcomponent.ChainClient, prepared *runtimeEvidenceActivationPreparedV2, encoded []byte) (*runtimeEvidenceActivationCompletedV2, error) {
	var block uint64
	var hash [32]byte
	coordinator := stabi.NewSTCoordinator()
	for {
		var err error
		block, hash, err = chain.FinalizedBlockContext(ctx)
		if err != nil {
			return nil, err
		}
		current, err := rawCoordinatorCallAt(ctx, self.keeper, self.plan.ValidatorEvidence.Coordinator, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch, block)
		if err != nil || current == nil || !current.IsUint64() {
			return nil, errors.Join(errors.New("activation initial epoch is unavailable"), err)
		}
		canonical, err := chain.BlockHashContext(ctx, block)
		if err != nil || canonical != hash {
			return nil, errors.Join(errors.New("activation epoch checkpoint changed during read"), err)
		}
		if current.Uint64() >= prepared.Epoch {
			break
		}
		delay := time.Duration(self.cfg.Public.Chain.ExpectedBlockSeconds) * time.Second
		if delay <= 0 {
			return nil, errors.New("activation boundary polling has no positive configured interval")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	start, err := chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, new(big.Int).SetUint64(prepared.Epoch))
	if err != nil {
		return nil, err
	}
	if prepared.Epoch == ^uint64(0) {
		return nil, errors.New("activation epoch has no bounded successor")
	}
	end, err := chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, new(big.Int).SetUint64(prepared.Epoch+1))
	if err != nil || start == 0 || end <= start {
		return nil, errors.Join(errors.New("activation epoch geometry is invalid"), err)
	}
	boundary := start
	for _, member := range prepared.Members {
		observed, err := chain.ValidatorEvidenceActivationAtHashContext(ctx, self.plan.ValidatorEvidence.Address, [32]byte(self.plan.ValidatorEvidence.RuntimeCodeHash), member.Activation, block, hash)
		if err != nil {
			return nil, err
		}
		boundary = max(boundary, observed.PublishedBlock)
	}
	if boundary >= end || boundary > block || boundary <= prepared.Evm.Number {
		return nil, errors.New("all activations must be finalized before their common initial epoch boundary")
	}
	boundaryHash, err := chain.BlockHashContext(ctx, boundary)
	if err != nil {
		return nil, err
	}
	for _, member := range prepared.Members {
		authority := validatorcomponent.ReleaseActivationV2Authority{Expected: member.Activation, Journal: self.plan.ValidatorEvidence.Address, RuntimeHash: [32]byte(self.plan.ValidatorEvidence.RuntimeCodeHash), ValidatorUID: member.ValidatorUid, NativeRuntime: self.runtimeEvidenceNativeIdentityV2()}
		if _, err := chain.AuthenticateReleaseActivationV2Context(ctx, self.substrate.chain, authority, member.Activation, member.VpkSignature, member.HotkeySignature, boundary, boundaryHash); err != nil {
			return nil, err
		}
	}
	return &runtimeEvidenceActivationCompletedV2{Schema: "urnetwork-sim-evidence-activation-completed-v2", PlanHash: self.plan.PlanHash, PreparedHash: fmt.Sprintf("0x%x", sha256.Sum256(encoded)), Boundary: ChainHead{Number: boundary, Hash: common.Hash(boundaryHash).Hex()}}, ctx.Err()
}

// Actual postconditions reauthenticate the retained preparation and the named
// public slot at the caller's finalized checkpoint. They never create files.
func (self *Executor) runtimeEvidenceActivationPostStateV2(ctx context.Context, action Action, head ChainHead, state map[string]any) (map[string]any, error) {
	limit, err := runtimeEvidenceProvisionLimit(self.cfg)
	if err != nil {
		return nil, err
	}
	var prepared runtimeEvidenceActivationPreparedV2
	encoded, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(self.stateDir, "evidence-v2-setup", "prepared.json"), limit, &prepared)
	if err != nil {
		return nil, err
	}
	if prepared.PlanHash != self.plan.PlanHash {
		var completed runtimeEvidenceActivationCompletedV2
		if _, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(self.stateDir, "evidence-v2-setup", "completed.json"), limit, &completed); err != nil {
			return nil, err
		}
		if self.journal == nil {
			return nil, errors.New("activation setup carry journal is absent")
		}
		source, err := runtimeEvidenceSetupSourcePlanV2(self.cfg, self.plan, self.stateDir, self.roles, &prepared, encoded, &completed, self.journal.Entries())
		if err != nil {
			return nil, err
		}
		// Only this read-only verifier sees the original owner. Mutation paths
		// retain exact current-plan admission and cannot republish an ancestor.
		verifier := *self
		verifier.plan = source
		self = &verifier
	}
	chain, err := self.runtimeEvidenceActivationChainV2(ctx)
	if err != nil {
		return nil, err
	}
	defer chain.Close()
	if err := self.authenticateRuntimeEvidencePreparedV2(ctx, chain, &prepared); err != nil {
		return nil, err
	}
	hash, err := decodeHex32("activation postcondition Evm hash", head.Hash)
	if err != nil || head.Number == 0 {
		return nil, errors.Join(errors.New("activation postcondition has no finalized checkpoint"), err)
	}
	state["prepared_hash"] = fmt.Sprintf("0x%x", sha256.Sum256(encoded))
	if action.ID == runtimeEvidenceActivationBoundaryActionId {
		var retained runtimeEvidenceActivationCompletedV2
		if _, err := readRuntimeEvidenceSetupV2(ctx, filepath.Join(self.stateDir, "evidence-v2-setup", "completed.json"), limit, &retained); err != nil {
			return nil, err
		}
		if retained.Boundary.Number > head.Number {
			return nil, errors.New("activation boundary follows the supplied postcondition checkpoint")
		}
		expected, err := self.runtimeEvidenceActivationBoundaryV2(ctx, chain, &prepared, encoded)
		if err != nil {
			return nil, err
		}
		if retained != *expected {
			return nil, errors.New("retained activation boundary differs from actual public history")
		}
		if _, err := runtimeEvidenceV2ResolvedConfig(self.cfg, self.stateDir); err != nil {
			return nil, err
		}
		state["boundary"], state["pair_count"] = retained.Boundary, len(prepared.Members)
	} else {
		member, err := self.runtimeEvidenceActivationMemberV2(action, &prepared)
		if err != nil {
			return nil, err
		}
		publication, err := chain.ValidatorEvidenceActivationAtHashContext(ctx, self.plan.ValidatorEvidence.Address, [32]byte(self.plan.ValidatorEvidence.RuntimeCodeHash), member.Activation, head.Number, hash)
		if err != nil {
			return nil, err
		}
		digest, err := member.Activation.Digest()
		if err != nil {
			return nil, err
		}
		state["activation_hash"], state["published_block"] = fmt.Sprintf("0x%x", digest), publication.PublishedBlock
	}
	canonical, err := chain.BlockHashContext(ctx, head.Number)
	if err != nil || canonical != hash {
		return nil, errors.Join(errors.New("activation postcondition checkpoint changed"), err)
	}
	return state, ctx.Err()
}

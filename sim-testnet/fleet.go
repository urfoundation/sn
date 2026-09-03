package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

type FleetCommitmentEvidence struct {
	Schema             string `json:"schema"`
	DeploymentID       string `json:"deployment_id,omitempty"`
	PlanHash           string `json:"plan_hash,omitempty"`
	ActionID           string `json:"action_id,omitempty"`
	IntentHash         string `json:"intent_hash,omitempty"`
	ManifestURI        string `json:"manifest_uri"`
	CommitmentHash     string `json:"commitment_hash"`
	Hotkey             string `json:"hotkey"`
	ExtrinsicHash      string `json:"extrinsic_hash"`
	CommitmentBlock    uint64 `json:"commitment_block"`
	FinalizedBlock     uint64 `json:"finalized_block"`
	FinalizedBlockHash string `json:"finalized_block_hash"`
}

type FleetBindingEvidence struct {
	Schema          string `json:"schema"`
	DeploymentID    string `json:"deployment_id,omitempty"`
	PlanHash        string `json:"plan_hash,omitempty"`
	ActionID        string `json:"action_id,omitempty"`
	IntentHash      string `json:"intent_hash,omitempty"`
	ClientID        string `json:"client_id"`
	ClientKey       string `json:"client_key"`
	FleetID         string `json:"fleet_id"`
	Hotkey          string `json:"hotkey"`
	Generation      uint64 `json:"generation"`
	ValidFromEpoch  uint64 `json:"valid_from_epoch"`
	ValidToEpoch    uint64 `json:"valid_to_epoch"`
	CommitmentHash  string `json:"commitment_hash"`
	BindingDigest   string `json:"binding_digest"`
	ClientSignature string `json:"client_signature"`
	HotkeySignature string `json:"hotkey_signature"`
	TransactionHash string `json:"transaction_hash"`
	BlockNumber     uint64 `json:"block_number"`
	BlockHash       string `json:"block_hash"`
	UID             uint16 `json:"uid"`
}

const fleetCommitmentEvidenceSchemaV2 = "urnetwork-fleet-commitment-evidence-v2"

const futureEpochInclusionSafetyBlocks uint64 = 16

type futureEpochTransactionWindow struct {
	HeadBlock       uint64
	CurrentEpoch    uint64
	EpochStartBlock uint64
	EpochEndBlock   uint64
	EffectiveEpoch  uint64
}

// Refuse to sign a next-epoch transaction close enough to the boundary that
// estimation or inclusion could observe a different current epoch.
func selectFutureEpochTransactionWindow(headBlock, currentEpoch, epochStartBlock, epochEndBlock uint64) (futureEpochTransactionWindow, bool, error) {
	window := futureEpochTransactionWindow{
		HeadBlock:       headBlock,
		CurrentEpoch:    currentEpoch,
		EpochStartBlock: epochStartBlock,
		EpochEndBlock:   epochEndBlock,
	}
	if epochStartBlock > headBlock || headBlock >= epochEndBlock || epochEndBlock-epochStartBlock <= futureEpochInclusionSafetyBlocks {
		return window, false, fmt.Errorf("inconsistent future-epoch window: head=%d epoch=%d start=%d end=%d", headBlock, currentEpoch, epochStartBlock, epochEndBlock)
	}
	if currentEpoch == ^uint64(0) {
		return window, false, errors.New("future-effective epoch overflows uint64")
	}
	window.EffectiveEpoch = currentEpoch + 1
	remaining := epochEndBlock - headBlock
	return window, remaining > futureEpochInclusionSafetyBlocks, nil
}

// Read all epoch coordinates at one latest block so finalized-state lag cannot
// produce an already-active transaction payload.
func readFutureEpochTransactionWindow(ctx context.Context, manager *EVMTxManager, addr common.Address, coordinator *stabi.STCoordinator) (futureEpochTransactionWindow, bool, error) {
	if manager == nil || manager.client == nil || coordinator == nil {
		return futureEpochTransactionWindow{}, false, errors.New("future-epoch transaction reader is unavailable")
	}
	headBlock, err := manager.client.BlockNumber(ctx)
	if err != nil {
		return futureEpochTransactionWindow{}, false, err
	}
	epoch, err := rawCoordinatorCallAt(ctx, manager, addr, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch, headBlock)
	if err != nil || !epoch.IsUint64() {
		return futureEpochTransactionWindow{}, false, stateMismatchError(err, "latest coordinator epoch is not uint64")
	}
	start, err := rawCoordinatorCallAt(ctx, manager, addr, coordinator.PackEpochStartBlock(epoch), coordinator.UnpackEpochStartBlock, headBlock)
	if err != nil || !start.IsUint64() {
		return futureEpochTransactionWindow{}, false, stateMismatchError(err, "latest epoch start is not uint64")
	}
	end, err := rawCoordinatorCallAt(ctx, manager, addr, coordinator.PackEpochEndBlock(epoch), coordinator.UnpackEpochEndBlock, headBlock)
	if err != nil || !end.IsUint64() {
		return futureEpochTransactionWindow{}, false, stateMismatchError(err, "latest epoch end is not uint64")
	}
	return selectFutureEpochTransactionWindow(headBlock, epoch.Uint64(), start.Uint64(), end.Uint64())
}

// Wait only across the unsafe tail of an epoch; cancellation remains owned by
// the enclosing simulator action.
func waitFutureEpochTransactionWindow(ctx context.Context, manager *EVMTxManager, addr common.Address, coordinator *stabi.STCoordinator) (futureEpochTransactionWindow, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		window, ready, err := readFutureEpochTransactionWindow(ctx, manager, addr, coordinator)
		if err != nil {
			return futureEpochTransactionWindow{}, err
		}
		if ready {
			return window, nil
		}
		select {
		case <-ctx.Done():
			return futureEpochTransactionWindow{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Preserve the exact policy validity length while rejecting both zero-length
// policy input and uint64 wraparound.
func fleetBindingValidityEnd(validFrom, maximumValidityEpochs uint64) (uint64, error) {
	if maximumValidityEpochs == 0 {
		return 0, errors.New("fleet binding maximum validity is zero")
	}
	validTo, ok := checkedAdd(validFrom, maximumValidityEpochs-1)
	if !ok {
		return 0, errors.New("fleet binding validity overflows uint64")
	}
	return validTo, nil
}

// Retry only an unbroadcast attempt invalidated by an epoch transition. Any
// persisted transaction or same-epoch revert must remain a hard failure.
func retryFleetBindingAfterEpochTransition(currentEpoch, validFrom uint64, transactionPersisted bool) bool {
	return !transactionPersisted && currentEpoch >= validFrom
}

// Builds one immutable manifest generation while preserving the stable fleet,
// hotkey and member identities across renewals.
func fleetManifestForGeneration(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, fleetIndex int, generation uint64) (protocol.FleetManifest, []byte, [32]byte, error) {
	if fleetIndex < 1 || fleetIndex > cfg.Config.Topology.fleetCandidates() {
		return protocol.FleetManifest{}, nil, [32]byte{}, fmt.Errorf("fleet index %d out of range", fleetIndex)
	}
	miners := make([]int, 0, cfg.Config.Topology.ClientsPerHeadFleet)
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miners = append(miners, fleetMemberMinerIndex(cfg, fleetIndex, member))
	}
	return fleetManifestForMembers(cfg, stateDir, roles, derive32(cfg, fmt.Sprintf("fleet-id/%d", fleetIndex)), fleetHotkeyLabel(fleetIndex), generation, miners)
}

func fleetManifestForMembers(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, fleetID [32]byte, hotkeyLabel string, generation uint64, miners []int) (protocol.FleetManifest, []byte, [32]byte, error) {
	if cfg == nil || cfg.Config == nil || roles == nil || fleetID == ([32]byte{}) || hotkeyLabel == "" || len(miners) != cfg.Config.Topology.ClientsPerHeadFleet {
		return protocol.FleetManifest{}, nil, [32]byte{}, errors.New("fleet manifest member inputs are incomplete")
	}
	if generation == 0 {
		return protocol.FleetManifest{}, nil, [32]byte{}, errors.New("fleet manifest generation is zero")
	}
	deployment, err := loadContractDeployment(stateDir)
	if err != nil {
		return protocol.FleetManifest{}, nil, [32]byte{}, err
	}
	hotkey, err := roleBytes32(roles, hotkeyLabel)
	if err != nil {
		return protocol.FleetManifest{}, nil, [32]byte{}, err
	}
	var coordinator [20]byte
	copy(coordinator[:], deployment.CoordinatorProxy[:])
	m := protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: testnetChainID, Netuid: cfg.Netuid, Coordinator: coordinator, FleetID: fleetID, Hotkey: hotkey, Generation: generation}
	for _, minerIndex := range miners {
		role, ok := roles.Clients[fmt.Sprintf("miner-%d", minerIndex)]
		if !ok || role.ClientIDHex == "" {
			return protocol.FleetManifest{}, nil, [32]byte{}, fmt.Errorf("miner-%d has no provisioned client_id", minerIndex)
		}
		idRaw, err := hex.DecodeString(role.ClientIDHex)
		if err != nil || len(idRaw) != 16 {
			return protocol.FleetManifest{}, nil, [32]byte{}, fmt.Errorf("miner-%d client_id is invalid", minerIndex)
		}
		keyRaw, err := hex.DecodeString(role.PublicKeyHex)
		if err != nil || len(keyRaw) != 32 {
			return protocol.FleetManifest{}, nil, [32]byte{}, fmt.Errorf("miner-%d client key is invalid", minerIndex)
		}
		var member protocol.FleetMember
		copy(member.ClientID[:], idRaw)
		copy(member.ClientKey[:], keyRaw)
		m.Members = append(m.Members, member)
	}
	canonical, err := m.Canonical()
	if err != nil {
		return protocol.FleetManifest{}, nil, [32]byte{}, err
	}
	hash, err := m.CommitmentHash()
	return m, canonical, hash, err
}

// Preserves the generation-1 public API used by initial and challenger fleets.
func fleetManifest(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, fleetIndex int) (protocol.FleetManifest, []byte, [32]byte, error) {
	return fleetManifestForGeneration(cfg, stateDir, roles, fleetIndex, 1)
}

func writePublicJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'), 0o644)
}

// Distinguishes a safe deterministic pre-transaction manifest left by a crash
// from complete exact evidence and from corruption. Complete evidence may be
// carried across a formally revised plan without submitting the same native
// commitment again.
func (self *Executor) fleetCommitmentGenerationAlreadyPublished(action Action, fleetIndex int, generation uint64, canonical []byte, manifestName, evidenceName string) (bool, error) {
	manifestPath := filepath.Join(self.stateDir, "public", manifestName)
	evidencePath := filepath.Join(self.stateDir, "public", evidenceName)
	manifestBytes, manifestErr := os.ReadFile(manifestPath)
	_, evidenceErr := os.Stat(evidencePath)
	manifestExists := manifestErr == nil
	evidenceExists := evidenceErr == nil
	if manifestErr != nil && !errors.Is(manifestErr, os.ErrNotExist) {
		return false, manifestErr
	}
	if evidenceErr != nil && !errors.Is(evidenceErr, os.ErrNotExist) {
		return false, evidenceErr
	}
	if !manifestExists && !evidenceExists {
		return false, nil
	}
	if !manifestExists && evidenceExists {
		return false, errors.New("fleet commitment evidence exists without its manifest")
	}
	if !bytes.Equal(manifestBytes, append(append([]byte(nil), canonical...), '\n')) {
		return false, errors.New("existing fleet manifest differs from its canonical generation")
	}
	if !evidenceExists {
		return false, nil
	}
	_, _, evidence, _, err := self.validatedFleetCommitmentGeneration(fleetIndex, generation)
	if err != nil {
		return false, fmt.Errorf("existing fleet commitment evidence is not exact current state: %w", err)
	}
	if err := validateFleetCommitmentRecoveryEvidence(action, evidence); err != nil {
		// The old evidence is still exact state, but this approved recovery must
		// replace it with a strictly later finalized commitment.
		if _, _, _, recovery, envelopeErr := fleetCommitmentRecoveryEnvelope(action); envelopeErr == nil && recovery {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Publishes one exact manifest generation and preserves its canonical finalized
// Subtensor evidence under generation-specific paths.
func (self *Executor) publishFleetCommitmentGeneration(ctx context.Context, action Action, fleetIndex int, generation uint64, manifestName, evidenceName string) error {
	_, canonical, hash, err := fleetManifestForGeneration(self.cfg, self.stateDir, self.roles, fleetIndex, generation)
	if err != nil {
		return err
	}
	published, err := self.fleetCommitmentGenerationAlreadyPublished(action, fleetIndex, generation, canonical, manifestName, evidenceName)
	if err != nil || published {
		return err
	}
	manifestPath := filepath.Join(self.stateDir, "public", manifestName)
	if err := atomicWrite(manifestPath, append(canonical, '\n'), 0o644); err != nil {
		return err
	}
	hotkey, err := roleBytes32(self.roles, fleetHotkeyLabel(fleetIndex))
	if err != nil {
		return err
	}
	call, err := self.substrate.chain.NewSetFleetCommitmentCall(self.cfg.Netuid, hash)
	if err != nil {
		return err
	}
	signer, err := self.substrate.RoleSigner(self.roles, fleetHotkeyLabel(fleetIndex))
	if err != nil {
		return err
	}
	txHash, transactionBlock, err := self.substrate.SendAs(ctx, self.plan.PlanHash, action, call, signer)
	if err != nil {
		return err
	}
	transactionBlockHash, err := self.substrate.chain.API.RPC.Chain.GetBlockHash(transactionBlock)
	if err != nil {
		return err
	}
	observed, err := self.substrate.fleetCommitmentAt(hotkey, transactionBlockHash)
	if err != nil {
		return err
	}
	if err := crv4.ValidateFleetCommitmentWrite(hash, transactionBlock, observed); err != nil {
		return err
	}
	evidence := FleetCommitmentEvidence{Schema: fleetCommitmentEvidenceSchemaV2, ManifestURI: manifestName, CommitmentHash: "0x" + hex.EncodeToString(hash[:]), Hotkey: "0x" + hex.EncodeToString(hotkey[:]), ExtrinsicHash: txHash.Hex(), CommitmentBlock: observed.CommitmentBlock, FinalizedBlock: transactionBlock, FinalizedBlockHash: transactionBlockHash.Hex()}
	return writePublicJSON(filepath.Join(self.stateDir, "public", evidenceName), evidence)
}

// Publishes the initial immutable fleet generation.
func (self *Executor) publishFleetCommitment(ctx context.Context, action Action, fleetIndex int) error {
	return self.publishFleetCommitmentGeneration(ctx, action, fleetIndex, 1, fmt.Sprintf("fleet-%d.json", fleetIndex), fmt.Sprintf("fleet-%d.commitment.json", fleetIndex))
}

// Publishes generation 2 without overwriting the generation-1 audit trail.
func (self *Executor) publishFleetRefreshCommitment(ctx context.Context, action Action, fleetIndex int) error {
	return self.publishFleetCommitmentGeneration(ctx, action, fleetIndex, 2, fmt.Sprintf("fleet-%d.refresh.json", fleetIndex), fmt.Sprintf("fleet-%d.refresh.commitment.json", fleetIndex))
}

func loadFleetCommitmentEvidence(stateDir string, fleetIndex int) (*FleetCommitmentEvidence, error) {
	return loadFleetCommitmentEvidenceGeneration(stateDir, fleetIndex, 1)
}

// Loads only the path/schema pair assigned to the requested generation.
func loadFleetCommitmentEvidenceGeneration(stateDir string, fleetIndex int, generation uint64) (*FleetCommitmentEvidence, error) {
	manifestName := fmt.Sprintf("fleet-%d.json", fleetIndex)
	evidenceName := fmt.Sprintf("fleet-%d.commitment.json", fleetIndex)
	if generation == 2 {
		manifestName = fmt.Sprintf("fleet-%d.refresh.json", fleetIndex)
		evidenceName = fmt.Sprintf("fleet-%d.refresh.commitment.json", fleetIndex)
	} else if generation != 1 {
		return nil, fmt.Errorf("unsupported fleet commitment generation %d", generation)
	}
	b, err := os.ReadFile(filepath.Join(stateDir, "public", evidenceName))
	if err != nil {
		return nil, err
	}
	var evidence FleetCommitmentEvidence
	if err := json.Unmarshal(b, &evidence); err != nil {
		return nil, err
	}
	if evidence.Schema != fleetCommitmentEvidenceSchemaV2 || evidence.ManifestURI != manifestName || evidence.CommitmentBlock == 0 || evidence.FinalizedBlock != evidence.CommitmentBlock {
		return nil, fmt.Errorf("invalid fleet commitment evidence schema")
	}
	for label, value := range map[string]string{
		"fleet commitment":                 evidence.CommitmentHash,
		"fleet commitment hotkey":          evidence.Hotkey,
		"fleet commitment extrinsic":       evidence.ExtrinsicHash,
		"fleet commitment finalized block": evidence.FinalizedBlockHash,
	} {
		if _, err := decodeHex32(label, value); err != nil {
			return nil, err
		}
	}
	return &evidence, nil
}

func decodeHex32(label, value string) ([32]byte, error) {
	var out [32]byte
	b, err := hex.DecodeString(stringsTrim0x(value))
	if err != nil || len(b) != 32 {
		return out, fmt.Errorf("%s is not 32-byte hex", label)
	}
	copy(out[:], b)
	return out, nil
}

// Compare every field the oracle attests. Matching only the commitment and
// block hash can preserve a type-confused or stale block height indefinitely.
func fleetMirrorMatches(current stabi.MirroredCommitmentsOutput, commitmentHash [32]byte, commitmentBlock uint64, finalizedBlockHash [32]byte) bool {
	return current.CommitmentHash == commitmentHash &&
		current.FinalizedBlock == commitmentBlock &&
		current.FinalizedBlockHash == finalizedBlockHash
}

func (e *Executor) mirrorFleetCommitment(ctx context.Context, a Action, fleetIndex int) error {
	manifest, _, hash, err := fleetManifest(e.cfg, e.stateDir, e.roles, fleetIndex)
	if err != nil {
		return err
	}
	evidence, err := loadFleetCommitmentEvidence(e.stateDir, fleetIndex)
	if err != nil {
		return err
	}
	finalizedHash, err := types.NewHashFromHexString(evidence.FinalizedBlockHash)
	if err != nil {
		return err
	}
	observed, err := e.substrate.fleetCommitmentAt(manifest.Hotkey, finalizedHash)
	if err != nil {
		return err
	}
	if err := crv4.ValidateFleetCommitmentWrite(hash, evidence.FinalizedBlock, observed); err != nil {
		return fmt.Errorf("native commitment evidence no longer verifies: %w", err)
	}
	canonicalHash, err := e.substrate.chain.API.RPC.Chain.GetBlockHash(evidence.FinalizedBlock)
	if err != nil || canonicalHash != finalizedHash {
		return stateMismatchError(err, "native commitment block %d is not canonical", evidence.FinalizedBlock)
	}
	currentNative, err := e.substrate.fleetCommitmentFinalized(manifest.Hotkey)
	if err != nil || currentNative.Hash != hash || currentNative.CommitmentBlock != evidence.CommitmentBlock {
		return fmt.Errorf("native commitment evidence no longer verifies")
	}
	coordinator := stabi.NewSTCoordinator()
	if current, readErr := rawCoordinatorCall(ctx, e.oracle, common.BytesToAddress(manifest.Coordinator[:]), coordinator.PackMirroredCommitments(manifest.Hotkey), coordinator.UnpackMirroredCommitments); readErr == nil && fleetMirrorMatches(current, hash, evidence.FinalizedBlock, [32]byte(finalizedHash)) {
		return nil
	}
	if a.Parameters["batch_installed"] == "true" {
		return fmt.Errorf("fleet %d atomic installer did not establish its exact commitment mirror", fleetIndex)
	}
	commitmentAction, err := e.planAction(fmt.Sprintf("fleet.commitment.%d", fleetIndex))
	if err != nil {
		return err
	}
	headBlock, err := e.oracle.client.BlockNumber(ctx)
	if err != nil {
		return err
	}
	if err := validateFleetCommitmentInclusionLifetime(e.cfg, commitmentAction, evidence, headBlock); err != nil {
		return err
	}
	data, err := coordinator.TryPackMirrorCommitment(manifest.Hotkey, hash, evidence.FinalizedBlock, [32]byte(finalizedHash))
	if err != nil {
		return err
	}
	addr := common.BytesToAddress(manifest.Coordinator[:])
	if _, err := e.oracle.Send(ctx, e.plan.PlanHash, a, &addr, big.NewInt(0), data); err != nil {
		return err
	}
	current, err := rawCoordinatorCall(ctx, e.oracle, addr, coordinator.PackMirroredCommitments(manifest.Hotkey), coordinator.UnpackMirroredCommitments)
	if err != nil {
		return err
	}
	if !fleetMirrorMatches(current, hash, evidence.FinalizedBlock, [32]byte(finalizedHash)) {
		return fmt.Errorf("coordinator commitment mirror postcondition mismatch")
	}
	return nil
}

func rawCoordinatorCall[T any](ctx context.Context, manager *EVMTxManager, addr common.Address, data []byte, unpack func([]byte) (T, error)) (T, error) {
	var zero T
	head, err := finalizedEVMHead(ctx, manager.client)
	if err != nil {
		return zero, err
	}
	out, err := manager.client.CallContract(ctx, ethereum.CallMsg{To: &addr, Data: data}, new(big.Int).SetUint64(head.Number))
	if err != nil {
		return zero, err
	}
	return unpack(out)
}

func rawCoordinatorCallAt[T any](ctx context.Context, manager *EVMTxManager, addr common.Address, data []byte, unpack func([]byte) (T, error), block uint64) (T, error) {
	var zero T
	out, err := manager.client.CallContract(ctx, ethereum.CallMsg{To: &addr, Data: data}, new(big.Int).SetUint64(block))
	if err != nil {
		return zero, err
	}
	return unpack(out)
}

const maximumEVMRPCBatchCalls = 50

type coordinatorCallAt struct {
	Address common.Address
	Data    []byte
	Block   uint64
}

// The public testnet endpoint accepts at most fifty JSON-RPC batch elements.
// Selectors may differ so historical proofs from many finalized blocks still
// share bounded HTTP requests without losing their point-in-time identity.
func rawCoordinatorBatchCallsAt(ctx context.Context, client *ethclient.Client, calls []coordinatorCallAt) ([][]byte, error) {
	if client == nil || len(calls) == 0 {
		return nil, errors.New("coordinator batch call is unavailable")
	}
	outputs := make([][]byte, len(calls))
	for start := 0; start < len(calls); start += maximumEVMRPCBatchCalls {
		end := min(start+maximumEVMRPCBatchCalls, len(calls))
		results := make([]hexutil.Bytes, end-start)
		batch := make([]rpc.BatchElem, end-start)
		for index := start; index < end; index++ {
			call := calls[index]
			if call.Block == 0 || call.Address == (common.Address{}) || len(call.Data) == 0 {
				return nil, fmt.Errorf("coordinator batch call %d has an incomplete target, calldata, or block", index)
			}
			batch[index-start] = rpc.BatchElem{
				Method: "eth_call",
				Args: []any{
					map[string]any{"to": call.Address, "data": hexutil.Bytes(call.Data)},
					hexutil.EncodeUint64(call.Block),
				},
				Result: &results[index-start],
			}
		}
		if err := client.Client().BatchCallContext(ctx, batch); err != nil {
			return nil, err
		}
		for index := range batch {
			if batch[index].Error != nil {
				return nil, fmt.Errorf("coordinator batch call %d: %w", start+index, batch[index].Error)
			}
			outputs[start+index] = append([]byte(nil), results[index]...)
		}
	}
	return outputs, nil
}

// Preserve the common single-snapshot call shape used by install and refresh
// verification while sharing the historical multi-snapshot transport.
func rawCoordinatorBatchCallAt(ctx context.Context, manager *EVMTxManager, addr common.Address, calls [][]byte, block uint64) ([][]byte, error) {
	if manager == nil || manager.client == nil || len(calls) == 0 || block == 0 {
		return nil, errors.New("coordinator batch call is unavailable")
	}
	requests := make([]coordinatorCallAt, len(calls))
	for index, data := range calls {
		requests[index] = coordinatorCallAt{Address: addr, Data: data, Block: block}
	}
	return rawCoordinatorBatchCallsAt(ctx, manager.client, requests)
}

func (e *Executor) bindFleetMember(ctx context.Context, a Action, fleetIndex, memberIndex int) error {
	manifest, _, _, err := fleetManifest(e.cfg, e.stateDir, e.roles, fleetIndex)
	if err != nil {
		return err
	}
	if memberIndex < 1 || memberIndex > len(manifest.Members) {
		return fmt.Errorf("fleet member index %d out of range", memberIndex)
	}
	if a.Parameters["batch_installed"] == "true" {
		return e.verifyBatchInstalledFleetMember(ctx, manifest, fleetIndex, memberIndex)
	}
	member := manifest.Members[memberIndex-1]
	coordinator := stabi.NewSTCoordinator()
	addr := common.BytesToAddress(manifest.Coordinator[:])
	minerIndex := fleetMemberMinerIndex(e.cfg, fleetIndex, memberIndex)
	clientRole := e.roles.Clients[fmt.Sprintf("miner-%d", minerIndex)]
	seed, err := hex.DecodeString(clientRole.SeedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("fleet member %d client seed invalid", memberIndex)
	}
	hotRole := e.roles.Substrate[fleetHotkeyLabel(fleetIndex)]
	hotkey, err := crv4.KeypairFromSeedHex(hotRole.SeedHex)
	if err != nil {
		return err
	}
	clientPrivateKey := ed25519.NewKeyFromSeed(seed)
	for {
		window, err := waitFutureEpochTransactionWindow(ctx, e.keeper, addr, coordinator)
		if err != nil {
			return err
		}
		validFrom := window.EffectiveEpoch
		validTo, err := fleetBindingValidityEnd(validFrom, e.cfg.Policy.Binding.MaximumValidityEpochs)
		if err != nil {
			return err
		}
		binding, err := manifest.Binding(member, validFrom, validTo)
		if err != nil {
			return err
		}
		if prior, readErr := rawCoordinatorCall(ctx, e.keeper, addr, coordinator.PackBindingAt(member.ClientID, new(big.Int).SetUint64(validFrom)), coordinator.UnpackBindingAt); readErr == nil && prior.Active && prior.Record.Generation == binding.Generation && prior.Record.CommitmentHash == binding.CommitmentHash {
			return nil
		}
		clientSignature, err := binding.SignClient(clientPrivateKey)
		if err != nil {
			return err
		}
		digest, err := binding.Digest()
		if err != nil {
			return err
		}
		hotkeySignature, err := hotkey.Sign(digest[:])
		if err != nil {
			return err
		}
		if !binding.VerifyClient(clientSignature) || !binding.VerifyHotkey(hotkeySignature) {
			return errors.New("locally generated fleet signatures did not verify")
		}
		contractBinding := stabi.STCoordinatorFleetBinding{ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: addr, FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash}
		data, err := coordinator.TryPackBindFleetMember(contractBinding, clientSignature, hotkeySignature)
		if err != nil {
			return err
		}
		receipt, sendErr := e.keeper.Send(ctx, e.plan.PlanHash, a, &addr, big.NewInt(0), data)
		if sendErr != nil {
			_, transactionPersisted := e.keeper.journal.LatestTransaction(e.plan.PlanHash, a.ID, a.IntentHash)
			latest, _, readErr := readFutureEpochTransactionWindow(ctx, e.keeper, addr, coordinator)
			if readErr == nil && retryFleetBindingAfterEpochTransition(latest.CurrentEpoch, validFrom, transactionPersisted) {
				continue
			}
			return sendErr
		}
		post, err := rawCoordinatorCall(ctx, e.keeper, addr, coordinator.PackBindingAt(member.ClientID, new(big.Int).SetUint64(validFrom)), coordinator.UnpackBindingAt)
		if err != nil {
			return err
		}
		if !post.Active || post.Record.FleetId != binding.FleetID || post.Record.Hotkey != binding.Hotkey || post.Record.ClientKey != binding.ClientKey || post.Record.CommitmentHash != binding.CommitmentHash || post.Record.Generation != binding.Generation || post.Record.ValidFromEpoch != validFrom || post.Record.ValidToEpoch != validTo {
			return fmt.Errorf("fleet binding postcondition mismatch for member %d", memberIndex)
		}
		evidence := FleetBindingEvidence{Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: "0x" + hex.EncodeToString(member.ClientID[:]), ClientKey: "0x" + hex.EncodeToString(member.ClientKey[:]), FleetID: "0x" + hex.EncodeToString(binding.FleetID[:]), Hotkey: "0x" + hex.EncodeToString(binding.Hotkey[:]), Generation: binding.Generation, ValidFromEpoch: validFrom, ValidToEpoch: validTo, CommitmentHash: "0x" + hex.EncodeToString(binding.CommitmentHash[:]), BindingDigest: "0x" + hex.EncodeToString(digest[:]), ClientSignature: "0x" + hex.EncodeToString(clientSignature), HotkeySignature: "0x" + hex.EncodeToString(hotkeySignature), TransactionHash: receipt.TxHash.Hex(), BlockNumber: receipt.BlockNumber.Uint64(), BlockHash: receipt.BlockHash.Hex(), UID: post.Record.Uid}
		return writePublicJSON(filepath.Join(e.stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", fleetIndex, memberIndex)), evidence)
	}
}

// Requires the install batch to have produced the standard generation-1
// evidence and every exact coordinator record field. Verification actions can
// never fall back to an unauthorized per-member write while the helper is the
// active oracle.
func (self *Executor) verifyBatchInstalledFleetMember(ctx context.Context, manifest protocol.FleetManifest, fleetIndex, memberIndex int) error {
	evidence, binding, err := loadVerifiedPriorFleetBinding(self.stateDir, manifest, fleetIndex, memberIndex)
	if err != nil {
		return err
	}
	coordinator := stabi.NewSTCoordinator()
	coordinatorAddress := common.BytesToAddress(manifest.Coordinator[:])
	head, err := finalizedEVMHead(ctx, self.keeper.client)
	if err != nil {
		return err
	}
	count, record, err := readFleetBindingVersionAt(ctx, self.keeper, coordinatorAddress, coordinator, binding.ClientID, 0, head.Number)
	if err != nil || !count.IsUint64() || count.Uint64() != 1 || !fleetBindingRecordMatches(record, binding, binding.ValidToEpoch, evidence.UID) {
		return stateMismatchError(err, "fleet %d member %d atomic install record mismatch", fleetIndex, memberIndex)
	}
	return nil
}

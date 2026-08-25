package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

type FleetCommitmentEvidence struct {
	Schema             string `json:"schema"`
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

func fleetManifest(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, fleetIndex int) (protocol.FleetManifest, []byte, [32]byte, error) {
	if fleetIndex < 1 || fleetIndex > cfg.Config.Topology.HeadFleets {
		return protocol.FleetManifest{}, nil, [32]byte{}, fmt.Errorf("fleet index %d out of range", fleetIndex)
	}
	deployment, err := loadContractDeployment(stateDir)
	if err != nil {
		return protocol.FleetManifest{}, nil, [32]byte{}, err
	}
	hotkey, err := roleBytes32(roles, fleetHotkeyLabel(fleetIndex))
	if err != nil {
		return protocol.FleetManifest{}, nil, [32]byte{}, err
	}
	fleetID := derive32(cfg, fmt.Sprintf("fleet-id/%d", fleetIndex))
	var coordinator [20]byte
	copy(coordinator[:], deployment.CoordinatorProxy[:])
	m := protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: testnetChainID, Netuid: cfg.Netuid, Coordinator: coordinator, FleetID: fleetID, Hotkey: hotkey, Generation: 1}
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		minerIndex := fleetMemberMinerIndex(cfg, fleetIndex, member)
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

func writePublicJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'), 0o644)
}

func (e *Executor) publishFleetCommitment(ctx context.Context, a Action, fleetIndex int) error {
	_, canonical, hash, err := fleetManifest(e.cfg, e.stateDir, e.roles, fleetIndex)
	if err != nil {
		return err
	}
	manifestName := fmt.Sprintf("fleet-%d.json", fleetIndex)
	manifestPath := filepath.Join(e.stateDir, "public", manifestName)
	if err := atomicWrite(manifestPath, append(canonical, '\n'), 0o644); err != nil {
		return err
	}
	hotkey, err := roleBytes32(e.roles, fleetHotkeyLabel(fleetIndex))
	if err != nil {
		return err
	}
	if existing, readErr := e.substrate.chain.FleetCommitmentFinalized(e.cfg.Netuid, hotkey); readErr == nil && existing.Hash == hash {
		return writePublicJSON(filepath.Join(e.stateDir, "public", fmt.Sprintf("fleet-%d.commitment.json", fleetIndex)), FleetCommitmentEvidence{Schema: "urnetwork-fleet-commitment-evidence-v1", ManifestURI: manifestName, CommitmentHash: "0x" + hex.EncodeToString(hash[:]), Hotkey: "0x" + hex.EncodeToString(hotkey[:]), CommitmentBlock: existing.CommitmentBlock, FinalizedBlock: existing.FinalizedAt, FinalizedBlockHash: existing.FinalizedHash.Hex()})
	}
	call, err := e.substrate.chain.NewSetFleetCommitmentCall(e.cfg.Netuid, hash)
	if err != nil {
		return err
	}
	signer, err := e.substrate.RoleSigner(e.roles, fleetHotkeyLabel(fleetIndex))
	if err != nil {
		return err
	}
	txHash, _, err := e.substrate.SendAs(ctx, e.plan.PlanHash, a, call, signer)
	if err != nil {
		return err
	}
	observed, err := e.substrate.chain.FleetCommitmentFinalized(e.cfg.Netuid, hotkey)
	if err != nil {
		return err
	}
	if observed.Hash != hash {
		return fmt.Errorf("finalized fleet commitment mismatch: got 0x%x want 0x%x", observed.Hash, hash)
	}
	evidence := FleetCommitmentEvidence{Schema: "urnetwork-fleet-commitment-evidence-v1", ManifestURI: manifestName, CommitmentHash: "0x" + hex.EncodeToString(hash[:]), Hotkey: "0x" + hex.EncodeToString(hotkey[:]), ExtrinsicHash: txHash.Hex(), CommitmentBlock: observed.CommitmentBlock, FinalizedBlock: observed.FinalizedAt, FinalizedBlockHash: observed.FinalizedHash.Hex()}
	return writePublicJSON(filepath.Join(e.stateDir, "public", fmt.Sprintf("fleet-%d.commitment.json", fleetIndex)), evidence)
}

func loadFleetCommitmentEvidence(stateDir string, fleetIndex int) (*FleetCommitmentEvidence, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d.commitment.json", fleetIndex)))
	if err != nil {
		return nil, err
	}
	var evidence FleetCommitmentEvidence
	if err := json.Unmarshal(b, &evidence); err != nil {
		return nil, err
	}
	if evidence.Schema != "urnetwork-fleet-commitment-evidence-v1" {
		return nil, fmt.Errorf("invalid fleet commitment evidence schema")
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
	observed, err := e.substrate.chain.FleetCommitmentAt(e.cfg.Netuid, manifest.Hotkey, finalizedHash)
	if err != nil {
		return err
	}
	if observed.Hash != hash || observed.CommitmentBlock != evidence.CommitmentBlock {
		return fmt.Errorf("native commitment evidence no longer verifies")
	}
	coordinator := stabi.NewSTCoordinator()
	if current, readErr := rawCoordinatorCall(ctx, e.oracle, common.BytesToAddress(manifest.Coordinator[:]), coordinator.PackMirroredCommitments(manifest.Hotkey), coordinator.UnpackMirroredCommitments); readErr == nil && current.CommitmentHash == hash && current.FinalizedBlockHash == [32]byte(finalizedHash) {
		return nil
	}
	data, err := coordinator.TryPackMirrorCommitment(manifest.Hotkey, hash, observed.CommitmentBlock, [32]byte(finalizedHash))
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
	if current.CommitmentHash != hash || current.FinalizedBlockHash != [32]byte(finalizedHash) || current.FinalizedBlock != observed.CommitmentBlock {
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

func (e *Executor) bindFleetMember(ctx context.Context, a Action, fleetIndex, memberIndex int) error {
	manifest, _, _, err := fleetManifest(e.cfg, e.stateDir, e.roles, fleetIndex)
	if err != nil {
		return err
	}
	if memberIndex < 1 || memberIndex > len(manifest.Members) {
		return fmt.Errorf("fleet member index %d out of range", memberIndex)
	}
	member := manifest.Members[memberIndex-1]
	coordinator := stabi.NewSTCoordinator()
	addr := common.BytesToAddress(manifest.Coordinator[:])
	epoch, err := rawCoordinatorCall(ctx, e.keeper, addr, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch)
	if err != nil {
		return err
	}
	if !epoch.IsUint64() {
		return fmt.Errorf("coordinator epoch exceeds uint64")
	}
	validFrom := epoch.Uint64() + 1
	validTo := validFrom + e.cfg.Policy.Binding.MaximumValidityEpochs - 1
	binding, err := manifest.Binding(member, validFrom, validTo)
	if err != nil {
		return err
	}
	if prior, readErr := rawCoordinatorCall(ctx, e.keeper, addr, coordinator.PackBindingAt(member.ClientID, new(big.Int).SetUint64(validFrom)), coordinator.UnpackBindingAt); readErr == nil && prior.Active && prior.Record.Generation == binding.Generation && prior.Record.CommitmentHash == binding.CommitmentHash {
		return nil
	}

	minerIndex := fleetMemberMinerIndex(e.cfg, fleetIndex, memberIndex)
	clientRole := e.roles.Clients[fmt.Sprintf("miner-%d", minerIndex)]
	seed, err := hex.DecodeString(clientRole.SeedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("fleet member %d client seed invalid", memberIndex)
	}
	clientSignature, err := binding.SignClient(ed25519.NewKeyFromSeed(seed))
	if err != nil {
		return err
	}
	hotRole := e.roles.Substrate[fleetHotkeyLabel(fleetIndex)]
	hotkey, err := crv4.KeypairFromSeedHex(hotRole.SeedHex)
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
		return fmt.Errorf("locally generated fleet signatures did not verify")
	}
	contractBinding := stabi.STCoordinatorFleetBinding{ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: addr, FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash}
	data, err := coordinator.TryPackBindFleetMember(contractBinding, clientSignature, hotkeySignature)
	if err != nil {
		return err
	}
	receipt, err := e.keeper.Send(ctx, e.plan.PlanHash, a, &addr, big.NewInt(0), data)
	if err != nil {
		return err
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

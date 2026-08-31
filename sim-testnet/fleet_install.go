package main

// This file accelerates initial testnet fleet setup. It installs only wholly
// absent fleets, carries only wholly exact fleets, and rejects partial state so
// the helper's transaction remains atomic and auditable across plan revisions.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

const fleetInstallPreparedSchema = "urnetwork-fleet-install-prepared-v1"
const fleetInstallBatchEvidenceSchema = "urnetwork-fleet-install-batch-evidence-v1"

// Retains the exact finalized native attestation and signatures for one newly
// installed fleet.
type fleetInstallPreparedFleet struct {
	Fleet                 int                    `json:"fleet"`
	ManifestURI           string                 `json:"manifest_uri"`
	CommitmentEvidenceURI string                 `json:"commitment_evidence_uri"`
	Hotkey                string                 `json:"hotkey"`
	CommitmentHash        string                 `json:"commitment_hash"`
	FinalizedBlock        uint64                 `json:"finalized_block"`
	FinalizedBlockHash    string                 `json:"finalized_block_hash"`
	Members               []FleetBindingEvidence `json:"members"`
}

// Is written with private permissions before transaction persistence.
type fleetInstallPreparedEvidence struct {
	Schema         string                      `json:"schema"`
	DeploymentID   string                      `json:"deployment_id"`
	PlanHash       string                      `json:"plan_hash"`
	ActionID       string                      `json:"action_id"`
	IntentHash     string                      `json:"intent_hash"`
	Batch          int                         `json:"batch"`
	FirstFleet     int                         `json:"first_fleet"`
	LastFleet      int                         `json:"last_fleet"`
	Generation     uint64                      `json:"generation"`
	EffectiveEpoch uint64                      `json:"effective_epoch"`
	ValidToEpoch   uint64                      `json:"valid_to_epoch"`
	CarriedFleets  []int                       `json:"carried_fleets"`
	Calldata       string                      `json:"calldata"`
	CalldataHash   string                      `json:"calldata_hash"`
	Fleets         []fleetInstallPreparedFleet `json:"fleets"`
}

// Is the public group proof; an all-carried batch intentionally has no EVM
// transaction fields and names the exact carried partition.
type FleetInstallBatchEvidence struct {
	Schema          string   `json:"schema"`
	Batch           int      `json:"batch"`
	FirstFleet      int      `json:"first_fleet"`
	LastFleet       int      `json:"last_fleet"`
	Generation      uint64   `json:"generation"`
	EffectiveEpoch  uint64   `json:"effective_epoch"`
	ValidToEpoch    uint64   `json:"valid_to_epoch"`
	InstalledFleets []int    `json:"installed_fleets"`
	CarriedFleets   []int    `json:"carried_fleets"`
	MemberEvidence  []string `json:"member_evidence"`
	CalldataHash    string   `json:"calldata_hash,omitempty"`
	TransactionHash string   `json:"transaction_hash,omitempty"`
	BlockNumber     uint64   `json:"block_number,omitempty"`
	BlockHash       string   `json:"block_hash,omitempty"`
}

// Describes the only two valid pre-states for a group.
type fleetInstallClassification struct {
	MissingFleets []int
	CarriedFleets []int
}

// Validates the immutable ten-fleet partition and generation-1 marker.
func fleetInstallActionRange(cfg *ResolvedConfig, action Action, batch int) (int, int, error) {
	if cfg == nil || batch < 1 {
		return 0, 0, errors.New("fleet install action range is unavailable")
	}
	firstFleet, err := strconv.Atoi(action.Parameters["first_fleet"])
	if err != nil {
		return 0, 0, fmt.Errorf("fleet install first_fleet: %w", err)
	}
	lastFleet, err := strconv.Atoi(action.Parameters["last_fleet"])
	if err != nil {
		return 0, 0, fmt.Errorf("fleet install last_fleet: %w", err)
	}
	if action.Parameters["generation"] != "1" {
		return 0, 0, errors.New("fleet install generation is not 1")
	}
	wantFirst := (batch-1)*fleetRefreshBatchSize + 1
	wantLast := wantFirst + fleetRefreshBatchSize - 1
	if wantLast > cfg.Config.Topology.HeadFleets {
		wantLast = cfg.Config.Topology.HeadFleets
	}
	if firstFleet != wantFirst || lastFleet != wantLast || firstFleet < 1 || lastFleet < firstFleet || lastFleet > cfg.Config.Topology.HeadFleets || lastFleet-firstFleet+1 > fleetRefreshBatchSize {
		return 0, 0, fmt.Errorf("fleet install batch %d range %d..%d, want %d..%d", batch, firstFleet, lastFleet, wantFirst, wantLast)
	}
	return firstFleet, lastFleet, nil
}

// Uses the same private transaction-recovery boundary as refresh batches.
func fleetInstallPreparedPath(stateDir string, batch int) string {
	return filepath.Join(stateDir, "secrets", fmt.Sprintf("fleet-install-batch-%d.prepared.json", batch))
}

// Requires a fleet to be wholly absent or wholly exact. A partial member set
// cannot be made equivalent to one atomic install and therefore fails closed.
func (self *Executor) classifyFleetInstallRange(ctx context.Context, firstFleet, lastFleet int, block uint64) (fleetInstallClassification, error) {
	classification := fleetInstallClassification{}
	coordinator := stabi.NewSTCoordinator()
	coordinatorAddress := self.payloads.Manifest.CoordinatorProxy
	type candidate struct {
		fleetIndex         int
		manifest           protocol.FleetManifest
		commitmentHash     [32]byte
		commitmentEvidence *FleetCommitmentEvidence
		finalizedBlockHash [32]byte
	}
	candidates := make([]candidate, 0, lastFleet-firstFleet+1)
	clientIDs := make([][16]byte, 0, (lastFleet-firstFleet+1)*self.cfg.Config.Topology.ClientsPerHeadFleet)
	mirrorCalls := make([][]byte, 0, lastFleet-firstFleet+1)
	for fleetIndex := firstFleet; fleetIndex <= lastFleet; fleetIndex++ {
		manifest, commitmentHash, commitmentEvidence, finalizedBlockHash, err := self.validatedFleetCommitmentGeneration(fleetIndex, 1)
		if err != nil {
			return classification, err
		}
		candidates = append(candidates, candidate{
			fleetIndex: fleetIndex, manifest: manifest, commitmentHash: commitmentHash,
			commitmentEvidence: commitmentEvidence, finalizedBlockHash: finalizedBlockHash,
		})
		for _, member := range manifest.Members {
			clientIDs = append(clientIDs, member.ClientID)
		}
		mirrorCalls = append(mirrorCalls, coordinator.PackMirroredCommitments(manifest.Hotkey))
	}
	memberReads, err := readFleetBindingVersionsAt(ctx, self.oracle, coordinatorAddress, coordinator, clientIDs, 0, block)
	if err != nil {
		return classification, err
	}
	mirrorOutputs, err := rawCoordinatorBatchCallAt(ctx, self.oracle, coordinatorAddress, mirrorCalls, block)
	if err != nil {
		return classification, err
	}
	memberCursor := 0
	for candidateIndex, current := range candidates {
		missingMembers, exactMembers := 0, 0
		for memberIndex := range current.manifest.Members {
			read := memberReads[memberCursor]
			memberCursor++
			if read.Count == nil || !read.Count.IsUint64() {
				return classification, fmt.Errorf("fleet %d member %d binding count is invalid", current.fleetIndex, memberIndex+1)
			}
			switch read.Count.Uint64() {
			case 0:
				if _, statErr := os.Stat(filepath.Join(self.stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", current.fleetIndex, memberIndex+1))); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
					return classification, stateMismatchError(statErr, "fleet %d member %d has evidence without an on-chain binding", current.fleetIndex, memberIndex+1)
				}
				missingMembers++
			case 1:
				evidence, binding, verifyErr := loadVerifiedPriorFleetBinding(self.stateDir, current.manifest, current.fleetIndex, memberIndex+1)
				if verifyErr != nil || !fleetBindingRecordMatches(read.Record, binding, binding.ValidToEpoch, evidence.UID) {
					return classification, stateMismatchError(verifyErr, "fleet %d member %d carried binding is not exact", current.fleetIndex, memberIndex+1)
				}
				exactMembers++
			default:
				return classification, fmt.Errorf("fleet %d member %d already has %d binding versions", current.fleetIndex, memberIndex+1, read.Count.Uint64())
			}
		}
		if missingMembers == len(current.manifest.Members) {
			classification.MissingFleets = append(classification.MissingFleets, current.fleetIndex)
			continue
		}
		if exactMembers != len(current.manifest.Members) {
			return classification, fmt.Errorf("fleet %d has partial generation-1 state (%d missing, %d exact)", current.fleetIndex, missingMembers, exactMembers)
		}
		mirror, err := coordinator.UnpackMirroredCommitments(mirrorOutputs[candidateIndex])
		if err != nil || !fleetMirrorMatches(mirror, current.commitmentHash, current.commitmentEvidence.FinalizedBlock, current.finalizedBlockHash) {
			return classification, stateMismatchError(err, "fleet %d carried commitment mirror is not exact", current.fleetIndex)
		}
		classification.CarriedFleets = append(classification.CarriedFleets, current.fleetIndex)
	}
	return classification, nil
}

// Builds generation-1 signatures for only the wholly absent fleet partition.
func (self *Executor) prepareFleetInstallBatch(ctx context.Context, action Action, batch, firstFleet, lastFleet int, classification fleetInstallClassification, window futureEpochTransactionWindow) (*fleetInstallPreparedEvidence, error) {
	if len(classification.MissingFleets) == 0 {
		return nil, errors.New("fleet install preparation has no missing fleets")
	}
	validToEpoch, err := fleetBindingValidityEnd(window.EffectiveEpoch, self.cfg.Policy.Binding.MaximumValidityEpochs)
	if err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	oracleState, err := readFleetRefreshOracleStateAt(ctx, self.oracle, self.payloads.Manifest.CoordinatorProxy, coordinator, window.HeadBlock)
	if err != nil {
		return nil, err
	}
	if oracleState.Immutable != self.payloads.CommitmentOracle || oracleState.Active != self.payloads.FleetBatcherAddress {
		return nil, errors.New("fleet installer is not the active commitment oracle")
	}
	parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
	if err != nil {
		return nil, err
	}
	prepared := &fleetInstallPreparedEvidence{
		Schema: fleetInstallPreparedSchema, DeploymentID: self.cfg.Config.Deployment.DeploymentID,
		PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		Batch: batch, FirstFleet: firstFleet, LastFleet: lastFleet, Generation: 1,
		EffectiveEpoch: window.EffectiveEpoch, ValidToEpoch: validToEpoch,
		CarriedFleets: append([]int(nil), classification.CarriedFleets...),
	}
	contractFleets := make([]fleetBatcherFleetRefresh, 0, len(classification.MissingFleets))
	for _, fleetIndex := range classification.MissingFleets {
		manifest, commitmentHash, commitmentEvidence, finalizedBlockHash, err := self.validatedFleetCommitmentGeneration(fleetIndex, 1)
		if err != nil {
			return nil, err
		}
		hotkeyRole := self.roles.Substrate[fleetHotkeyLabel(fleetIndex)]
		hotkey, err := crv4.KeypairFromSeedHex(hotkeyRole.SeedHex)
		if err != nil {
			return nil, err
		}
		contractFleet := fleetBatcherFleetRefresh{
			Hotkey: manifest.Hotkey, CommitmentHash: commitmentHash,
			FinalizedBlock: commitmentEvidence.FinalizedBlock, FinalizedBlockHash: finalizedBlockHash,
		}
		preparedFleet := fleetInstallPreparedFleet{
			Fleet: fleetIndex, ManifestURI: commitmentEvidence.ManifestURI,
			CommitmentEvidenceURI: fmt.Sprintf("fleet-%d.commitment.json", fleetIndex),
			Hotkey:                "0x" + hex.EncodeToString(manifest.Hotkey[:]), CommitmentHash: "0x" + hex.EncodeToString(commitmentHash[:]),
			FinalizedBlock: commitmentEvidence.FinalizedBlock, FinalizedBlockHash: commitmentEvidence.FinalizedBlockHash,
		}
		for memberIndex, member := range manifest.Members {
			minerIndex := fleetMemberMinerIndex(self.cfg, fleetIndex, memberIndex+1)
			clientRole, ok := self.roles.Clients[fmt.Sprintf("miner-%d", minerIndex)]
			if !ok {
				return nil, fmt.Errorf("miner-%d client role is absent", minerIndex)
			}
			seed, err := hex.DecodeString(clientRole.SeedHex)
			if err != nil || len(seed) != ed25519.SeedSize {
				return nil, fmt.Errorf("miner-%d client seed is invalid", minerIndex)
			}
			clientPrivateKey := ed25519.NewKeyFromSeed(seed)
			binding, err := manifest.Binding(member, window.EffectiveEpoch, validToEpoch)
			if err != nil {
				return nil, err
			}
			clientSignature, err := binding.SignClient(clientPrivateKey)
			if err != nil {
				return nil, err
			}
			digest, err := binding.Digest()
			if err != nil {
				return nil, err
			}
			hotkeySignature, err := hotkey.Sign(digest[:])
			if err != nil || !binding.VerifyClient(clientSignature) || !binding.VerifyHotkey(hotkeySignature) {
				return nil, stateMismatchError(err, "fleet %d member %d install signatures did not verify", fleetIndex, memberIndex+1)
			}
			contractBinding := stabi.STCoordinatorFleetBinding{
				ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: self.payloads.Manifest.CoordinatorProxy,
				FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey,
				Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch,
				CommitmentHash: binding.CommitmentHash,
			}
			contractFleet.Members = append(contractFleet.Members, fleetBatcherMemberRefresh{
				PriorGeneration: 0, Binding: contractBinding, RevokeSignature: []byte{},
				ClientSignature: clientSignature, HotkeySignature: hotkeySignature,
			})
			preparedFleet.Members = append(preparedFleet.Members, FleetBindingEvidence{
				Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: "0x" + hex.EncodeToString(binding.ClientID[:]),
				ClientKey: "0x" + hex.EncodeToString(binding.ClientKey[:]), FleetID: "0x" + hex.EncodeToString(binding.FleetID[:]),
				Hotkey: "0x" + hex.EncodeToString(binding.Hotkey[:]), Generation: binding.Generation,
				ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch,
				CommitmentHash: "0x" + hex.EncodeToString(binding.CommitmentHash[:]), BindingDigest: "0x" + hex.EncodeToString(digest[:]),
				ClientSignature: "0x" + hex.EncodeToString(clientSignature), HotkeySignature: "0x" + hex.EncodeToString(hotkeySignature),
			})
		}
		contractFleets = append(contractFleets, contractFleet)
		prepared.Fleets = append(prepared.Fleets, preparedFleet)
	}
	data, err := parsed.Pack("install", contractFleets)
	if err != nil {
		return nil, fmt.Errorf("pack fleet install batch: %w", err)
	}
	prepared.Calldata = "0x" + hex.EncodeToString(data)
	prepared.CalldataHash = crypto.Keccak256Hash(data).Hex()
	return prepared, nil
}

// Validates recovered preparation before it can be paired with a persisted
// transaction.
func validateFleetInstallPrepared(prepared *fleetInstallPreparedEvidence, cfg *ResolvedConfig, plan *SetupPlan, action Action, batch, firstFleet, lastFleet int) ([]byte, error) {
	if prepared == nil || cfg == nil || plan == nil {
		return nil, errors.New("fleet install prepared evidence is unavailable")
	}
	if prepared.Schema != fleetInstallPreparedSchema || prepared.DeploymentID != cfg.Config.Deployment.DeploymentID ||
		prepared.PlanHash != plan.PlanHash || prepared.ActionID != action.ID || prepared.IntentHash != action.IntentHash ||
		prepared.Batch != batch || prepared.FirstFleet != firstFleet || prepared.LastFleet != lastFleet || prepared.Generation != 1 ||
		prepared.EffectiveEpoch == 0 || prepared.ValidToEpoch < prepared.EffectiveEpoch || len(prepared.Fleets) == 0 || len(prepared.Fleets) > lastFleet-firstFleet+1 {
		return nil, errors.New("fleet install prepared evidence identity mismatch")
	}
	data, err := decodeEvidenceHex(prepared.Calldata)
	if err != nil || len(data) < 4 || !strings.EqualFold(prepared.CalldataHash, crypto.Keccak256Hash(data).Hex()) {
		return nil, errors.New("fleet install prepared calldata is invalid")
	}
	return data, nil
}

// Recovers exact persisted calldata or prepares the current missing partition.
func (self *Executor) fleetInstallCalldata(ctx context.Context, action Action, batch, firstFleet, lastFleet int) (*fleetInstallPreparedEvidence, []byte, *FleetInstallBatchEvidence, error) {
	path := fleetInstallPreparedPath(self.stateDir, batch)
	if transaction, persisted := self.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash); persisted {
		var prepared fleetInstallPreparedEvidence
		if err := readJSONFile(path, &prepared); err != nil {
			return nil, nil, nil, fmt.Errorf("recover persisted fleet install preparation: %w", err)
		}
		data, err := validateFleetInstallPrepared(&prepared, self.cfg, self.plan, action, batch, firstFleet, lastFleet)
		if err != nil {
			return nil, nil, nil, err
		}
		raw, err := os.ReadFile(filepath.Join(self.stateDir, "transactions", stringsTrim0x(transaction.TransactionHash)+".rlp"))
		if err != nil {
			return nil, nil, nil, err
		}
		var exact ethTypes.Transaction
		if err := exact.UnmarshalBinary(raw); err != nil {
			return nil, nil, nil, err
		}
		if exact.Hash().Hex() != transaction.TransactionHash || exact.To() == nil || *exact.To() != self.payloads.FleetBatcherAddress || string(exact.Data()) != string(data) {
			return nil, nil, nil, errors.New("persisted fleet install transaction differs from prepared calldata")
		}
		return &prepared, data, nil, nil
	}
	headBlock, err := self.oracle.client.BlockNumber(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	classification, err := self.classifyFleetInstallRange(ctx, firstFleet, lastFleet, headBlock)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(classification.MissingFleets) == 0 {
		evidence := &FleetInstallBatchEvidence{
			Schema: fleetInstallBatchEvidenceSchema, Batch: batch, FirstFleet: firstFleet, LastFleet: lastFleet,
			Generation: 1, CarriedFleets: classification.CarriedFleets,
		}
		for fleetIndex := firstFleet; fleetIndex <= lastFleet; fleetIndex++ {
			for memberIndex := 1; memberIndex <= self.cfg.Config.Topology.ClientsPerHeadFleet; memberIndex++ {
				evidence.MemberEvidence = append(evidence.MemberEvidence, fmt.Sprintf("fleet-%d-member-%d.binding.json", fleetIndex, memberIndex))
			}
		}
		return nil, nil, evidence, nil
	}
	window, err := waitFutureEpochTransactionWindow(ctx, self.oracle, self.payloads.Manifest.CoordinatorProxy, stabi.NewSTCoordinator())
	if err != nil {
		return nil, nil, nil, err
	}
	classification, err = self.classifyFleetInstallRange(ctx, firstFleet, lastFleet, window.HeadBlock)
	if err != nil {
		return nil, nil, nil, err
	}
	prepared, err := self.prepareFleetInstallBatch(ctx, action, batch, firstFleet, lastFleet, classification, window)
	if err != nil {
		return nil, nil, nil, err
	}
	data, err := validateFleetInstallPrepared(prepared, self.cfg, self.plan, action, batch, firstFleet, lastFleet)
	if err != nil {
		return nil, nil, nil, err
	}
	encoded, err := json.MarshalIndent(prepared, "", "  ")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := atomicWrite(path, append(encoded, '\n'), 0o600); err != nil {
		return nil, nil, nil, err
	}
	return prepared, data, nil, nil
}

// Requires one exact FleetInstalled event per newly installed fleet.
func verifyFleetInstallEvents(receipt *ethTypes.Receipt, parsed abi.ABI, batcherAddress common.Address, prepared *fleetInstallPreparedEvidence) error {
	if receipt == nil || prepared == nil {
		return errors.New("fleet install receipt or preparation is unavailable")
	}
	event, ok := parsed.Events["FleetInstalled"]
	if !ok {
		return errors.New("fleet install ABI has no FleetInstalled event")
	}
	matched := 0
	for _, log := range receipt.Logs {
		if log.Address != batcherAddress || len(log.Topics) != 3 || log.Topics[0] != event.ID {
			continue
		}
		if matched >= len(prepared.Fleets) {
			return errors.New("fleet install receipt has excess events")
		}
		fleet := prepared.Fleets[matched]
		hotkey, err := decodeHex32("fleet install event hotkey", fleet.Hotkey)
		if err != nil {
			return err
		}
		commitmentHash, err := decodeHex32("fleet install event commitment", fleet.CommitmentHash)
		if err != nil {
			return err
		}
		values, err := event.Inputs.NonIndexed().Unpack(log.Data)
		if err != nil || len(values) != 1 {
			return stateMismatchError(err, "fleet install event data is invalid")
		}
		memberCount, ok := values[0].(*big.Int)
		if !ok || !memberCount.IsUint64() || memberCount.Uint64() != uint64(len(fleet.Members)) || log.Topics[1] != common.BytesToHash(hotkey[:]) || log.Topics[2] != common.BytesToHash(commitmentHash[:]) {
			return errors.New("fleet install event differs from prepared batch")
		}
		matched++
	}
	if matched != len(prepared.Fleets) {
		return fmt.Errorf("fleet install receipt has %d events, want %d", matched, len(prepared.Fleets))
	}
	return nil
}

// Verifies newly installed records at one exact block and returns evidence with
// the runtime uid and canonical receipt filled in.
func (self *Executor) verifyAndPublishFleetInstall(ctx context.Context, prepared *fleetInstallPreparedEvidence, receipt *ethTypes.Receipt) (*FleetInstallBatchEvidence, error) {
	coordinator := stabi.NewSTCoordinator()
	coordinatorAddress := self.payloads.Manifest.CoordinatorProxy
	type installedSnapshot struct {
		manifest           protocol.FleetManifest
		commitmentHash     [32]byte
		commitmentEvidence *FleetCommitmentEvidence
		finalizedBlockHash [32]byte
	}
	snapshots := make([]installedSnapshot, len(prepared.Fleets))
	calls := make([][]byte, 0, len(prepared.Fleets)*(2+2*self.cfg.Config.Topology.ClientsPerHeadFleet))
	for fleetOffset, fleet := range prepared.Fleets {
		manifest, commitmentHash, commitmentEvidence, finalizedBlockHash, err := self.validatedFleetCommitmentGeneration(fleet.Fleet, 1)
		if err != nil {
			return nil, err
		}
		snapshots[fleetOffset] = installedSnapshot{
			manifest: manifest, commitmentHash: commitmentHash,
			commitmentEvidence: commitmentEvidence, finalizedBlockHash: finalizedBlockHash,
		}
		calls = append(calls, coordinator.PackMirroredCommitments(manifest.Hotkey))
		for _, member := range manifest.Members {
			calls = append(calls,
				coordinator.PackBindingVersionCount(member.ClientID),
				coordinator.PackBindingVersionAt(member.ClientID, new(big.Int)),
			)
		}
		calls = append(calls, coordinator.PackFleetMemberCount(manifest.FleetID))
	}
	outputs, err := rawCoordinatorBatchCallAt(ctx, self.oracle, coordinatorAddress, calls, receipt.BlockNumber.Uint64())
	if err != nil {
		return nil, err
	}
	installedFleets := make([]int, 0, len(prepared.Fleets))
	outputIndex := 0
	for fleetOffset := range prepared.Fleets {
		fleet := &prepared.Fleets[fleetOffset]
		snapshot := snapshots[fleetOffset]
		mirror, err := coordinator.UnpackMirroredCommitments(outputs[outputIndex])
		outputIndex++
		if err != nil || !fleetMirrorMatches(mirror, snapshot.commitmentHash, snapshot.commitmentEvidence.FinalizedBlock, snapshot.finalizedBlockHash) {
			return nil, stateMismatchError(err, "fleet %d installed mirror mismatch", fleet.Fleet)
		}
		for memberOffset := range fleet.Members {
			evidence := &fleet.Members[memberOffset]
			binding, err := snapshot.manifest.Binding(snapshot.manifest.Members[memberOffset], evidence.ValidFromEpoch, evidence.ValidToEpoch)
			if err != nil {
				return nil, err
			}
			count, err := coordinator.UnpackBindingVersionCount(outputs[outputIndex])
			outputIndex++
			if err != nil {
				return nil, err
			}
			record, err := coordinator.UnpackBindingVersionAt(outputs[outputIndex])
			outputIndex++
			if err != nil || !count.IsUint64() || count.Uint64() != 1 || record.Generation != 1 || record.Cleaned {
				return nil, stateMismatchError(err, "fleet %d member %d install record is absent", fleet.Fleet, memberOffset+1)
			}
			evidence.UID = record.Uid
			if !fleetBindingRecordMatches(record, binding, binding.ValidToEpoch, evidence.UID) {
				return nil, fmt.Errorf("fleet %d member %d install record differs from its signature", fleet.Fleet, memberOffset+1)
			}
			evidence.TransactionHash = receipt.TxHash.Hex()
			evidence.BlockNumber = receipt.BlockNumber.Uint64()
			evidence.BlockHash = receipt.BlockHash.Hex()
			if err := writePublicJSON(filepath.Join(self.stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet.Fleet, memberOffset+1)), evidence); err != nil {
				return nil, err
			}
		}
		memberCount, err := coordinator.UnpackFleetMemberCount(outputs[outputIndex])
		outputIndex++
		if err != nil || !memberCount.IsUint64() || memberCount.Uint64() != uint64(len(fleet.Members)) {
			return nil, stateMismatchError(err, "fleet %d installed member count=%v", fleet.Fleet, memberCount)
		}
		installedFleets = append(installedFleets, fleet.Fleet)
	}
	evidence := &FleetInstallBatchEvidence{
		Schema: fleetInstallBatchEvidenceSchema, Batch: prepared.Batch,
		FirstFleet: prepared.FirstFleet, LastFleet: prepared.LastFleet, Generation: 1,
		EffectiveEpoch: prepared.EffectiveEpoch, ValidToEpoch: prepared.ValidToEpoch,
		InstalledFleets: installedFleets, CarriedFleets: prepared.CarriedFleets,
		CalldataHash: prepared.CalldataHash, TransactionHash: receipt.TxHash.Hex(),
		BlockNumber: receipt.BlockNumber.Uint64(), BlockHash: receipt.BlockHash.Hex(),
	}
	for fleetIndex := prepared.FirstFleet; fleetIndex <= prepared.LastFleet; fleetIndex++ {
		for memberIndex := 1; memberIndex <= self.cfg.Config.Topology.ClientsPerHeadFleet; memberIndex++ {
			evidence.MemberEvidence = append(evidence.MemberEvidence, fmt.Sprintf("fleet-%d-member-%d.binding.json", fleetIndex, memberIndex))
		}
	}
	return evidence, nil
}

// Executes one bounded install batch or records an exact all-carried proof.
func (self *Executor) installFleetBatch(ctx context.Context, action Action, batch int) error {
	if err := self.ensurePayloads(ctx); err != nil {
		return err
	}
	firstFleet, lastFleet, err := fleetInstallActionRange(self.cfg, action, batch)
	if err != nil {
		return err
	}
	if !common.IsHexAddress(action.Target) || common.HexToAddress(action.Target) != self.payloads.FleetBatcherAddress {
		return errors.New("fleet install action target differs from the deployed batcher")
	}
	prepared, data, carried, err := self.fleetInstallCalldata(ctx, action, batch, firstFleet, lastFleet)
	if err != nil {
		return err
	}
	if carried != nil {
		return writePublicJSON(filepath.Join(self.stateDir, "public", fmt.Sprintf("fleet-install-batch-%d.json", batch)), carried)
	}
	batcherAddress := self.payloads.FleetBatcherAddress
	receipt, sendErr := self.oracle.Send(ctx, self.plan.PlanHash, action, &batcherAddress, big.NewInt(0), data)
	if sendErr != nil {
		_, persisted := self.oracle.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
		latest, _, readErr := readFutureEpochTransactionWindow(ctx, self.oracle, self.payloads.Manifest.CoordinatorProxy, stabi.NewSTCoordinator())
		if readErr == nil && retryFleetBindingAfterEpochTransition(latest.CurrentEpoch, prepared.EffectiveEpoch, persisted) {
			return self.installFleetBatch(ctx, action, batch)
		}
		return sendErr
	}
	parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
	if err != nil {
		return err
	}
	if err := verifyFleetInstallEvents(receipt, parsed, batcherAddress, prepared); err != nil {
		return err
	}
	evidence, err := self.verifyAndPublishFleetInstall(ctx, prepared, receipt)
	if err != nil {
		return err
	}
	return writePublicJSON(filepath.Join(self.stateDir, "public", fmt.Sprintf("fleet-install-batch-%d.json", batch)), evidence)
}

// Validates that installed and carried partitions cover the exact range once.
func validateFleetInstallPartitions(evidence FleetInstallBatchEvidence) error {
	seen := map[int]bool{}
	for _, fleetIndex := range append(append([]int(nil), evidence.InstalledFleets...), evidence.CarriedFleets...) {
		if fleetIndex < evidence.FirstFleet || fleetIndex > evidence.LastFleet || seen[fleetIndex] {
			return errors.New("fleet install evidence partition is out of range or duplicated")
		}
		seen[fleetIndex] = true
	}
	for fleetIndex := evidence.FirstFleet; fleetIndex <= evidence.LastFleet; fleetIndex++ {
		if !seen[fleetIndex] {
			return errors.New("fleet install evidence partition is incomplete")
		}
	}
	return nil
}

func fleetInstallAliasIndices(cfg *ResolvedConfig, action Action) (int, int, int, error) {
	if cfg == nil || cfg.Config == nil || action.Kind != "evm-read" || action.Parameters["batch_installed"] != "true" {
		return 0, 0, 0, errors.New("fleet install alias is not an atomic-install read proof")
	}
	fleet, member := 0, 0
	switch {
	case strings.HasPrefix(action.ID, "fleet.mirror."):
		fleet = suffixInt(action.ID)
	case strings.HasPrefix(action.ID, "fleet.bind."):
		var err error
		fleet, member, err = fleetBindingActionIndices(action.ID)
		if err != nil {
			return 0, 0, 0, err
		}
	default:
		return 0, 0, 0, fmt.Errorf("action %s is not a fleet install alias", action.ID)
	}
	if fleet < 1 || fleet > cfg.Config.Topology.HeadFleets || member < 0 || member > cfg.Config.Topology.ClientsPerHeadFleet {
		return 0, 0, 0, fmt.Errorf("fleet install alias %s is out of range", action.ID)
	}
	wantTarget := fmt.Sprintf("head-fleet:%d", fleet)
	if member > 0 {
		wantTarget = fmt.Sprintf("miner:%d", fleetMemberMinerIndex(cfg, fleet, member))
	}
	if action.Target != wantTarget {
		return 0, 0, 0, fmt.Errorf("fleet install alias %s target=%q want %q", action.ID, action.Target, wantTarget)
	}
	batch := 1 + (fleet-1)/fleetRefreshBatchSize
	return batch, fleet, member, nil
}

func fleetInstallObservedUint64(value any) (uint64, error) {
	switch typed := value.(type) {
	case json.Number:
		return strconv.ParseUint(string(typed), 10, 64)
	case uint64:
		return typed, nil
	case int:
		if typed >= 0 {
			return uint64(typed), nil
		}
	}
	return 0, fmt.Errorf("fleet install observation has type %T", value)
}

// Derives an individual mirror/member receipt from the already-authenticated
// batch receipt and its dual-signed local artifact. It performs no RPC call;
// the source batch remains the one live-chain assertion revalidated on resume.
func (self *Executor) verifyFleetInstallAliasState(action Action, state map[string]any) (JournalEntry, *ActionPostcondition, map[string]any, error) {
	batch, fleet, member, err := fleetInstallAliasIndices(self.cfg, action)
	if err != nil {
		return JournalEntry{}, nil, nil, err
	}
	sourceAction, err := self.planAction(fmt.Sprintf("fleet.install.batch.%d", batch))
	if err != nil {
		return JournalEntry{}, nil, nil, err
	}
	firstFleet, lastFleet, err := fleetInstallActionRange(self.cfg, sourceAction, batch)
	if err != nil || fleet < firstFleet || fleet > lastFleet {
		return JournalEntry{}, nil, nil, stateMismatchError(err, "fleet install alias is outside source batch %d", batch)
	}
	sourceEntry, ok := self.verifiedActionEntry(sourceAction)
	if !ok {
		return JournalEntry{}, nil, nil, fmt.Errorf("fleet install source batch %d is not verified", batch)
	}
	sourceRecord, err := self.readPersistedPostcondition(sourceEntry)
	if err != nil {
		return JournalEntry{}, nil, nil, err
	}
	if err := observedPostconditionMatches(sourceRecord.Observed, sourceRecord.IndependentObserved); err != nil {
		return JournalEntry{}, nil, nil, fmt.Errorf("fleet install source observations differ: %w", err)
	}
	wantMembers := uint64((lastFleet - firstFleet + 1) * self.cfg.Config.Topology.ClientsPerHeadFleet)
	for key, want := range map[string]uint64{
		"batch": uint64(batch), "first_fleet": uint64(firstFleet), "last_fleet": uint64(lastFleet), "generation": 1, "members": wantMembers,
	} {
		observed, readErr := fleetInstallObservedUint64(sourceRecord.Observed[key])
		if readErr != nil || observed != want {
			return JournalEntry{}, nil, nil, stateMismatchError(readErr, "fleet install source %s=%d want %d", key, observed, want)
		}
	}
	var batchEvidence FleetInstallBatchEvidence
	if err := readJSONFile(filepath.Join(self.stateDir, "public", fmt.Sprintf("fleet-install-batch-%d.json", batch)), &batchEvidence); err != nil {
		return JournalEntry{}, nil, nil, err
	}
	if batchEvidence.Schema != fleetInstallBatchEvidenceSchema || batchEvidence.Batch != batch || batchEvidence.FirstFleet != firstFleet || batchEvidence.LastFleet != lastFleet || batchEvidence.Generation != 1 {
		return JournalEntry{}, nil, nil, errors.New("fleet install alias source evidence identity mismatch")
	}
	if err := validateFleetInstallPartitions(batchEvidence); err != nil {
		return JournalEntry{}, nil, nil, err
	}
	if observed, _ := sourceRecord.Observed["transaction_hash"].(string); observed != batchEvidence.TransactionHash {
		return JournalEntry{}, nil, nil, errors.New("fleet install alias transaction differs from its source receipt")
	}
	installed, err := fleetInstallObservedUint64(sourceRecord.Observed["installed_fleets"])
	if err != nil || installed != uint64(len(batchEvidence.InstalledFleets)) {
		return JournalEntry{}, nil, nil, stateMismatchError(err, "fleet install source installed partition mismatch")
	}
	carried, err := fleetInstallObservedUint64(sourceRecord.Observed["carried_fleets"])
	if err != nil || carried != uint64(len(batchEvidence.CarriedFleets)) {
		return JournalEntry{}, nil, nil, stateMismatchError(err, "fleet install source carried partition mismatch")
	}
	wantEvidenceCount := int(wantMembers)
	if len(batchEvidence.MemberEvidence) != wantEvidenceCount {
		return JournalEntry{}, nil, nil, errors.New("fleet install alias source member evidence count mismatch")
	}
	evidenceIndex := 0
	for fleetIndex := firstFleet; fleetIndex <= lastFleet; fleetIndex++ {
		for memberIndex := 1; memberIndex <= self.cfg.Config.Topology.ClientsPerHeadFleet; memberIndex++ {
			want := fmt.Sprintf("fleet-%d-member-%d.binding.json", fleetIndex, memberIndex)
			if batchEvidence.MemberEvidence[evidenceIndex] != want {
				return JournalEntry{}, nil, nil, errors.New("fleet install alias source member evidence ordering mismatch")
			}
			evidenceIndex++
		}
	}
	state["kind"], state["target"] = action.Kind, action.Target
	state["source_action"], state["source_postcondition_hash"] = sourceAction.ID, sourceEntry.PostconditionHash
	state["batch"], state["fleet"] = batch, fleet
	manifest, _, commitmentHash, err := fleetManifest(self.cfg, self.stateDir, self.roles, fleet)
	if err != nil {
		return JournalEntry{}, nil, nil, err
	}
	if member == 0 {
		commitmentEvidence, err := loadFleetCommitmentEvidence(self.stateDir, fleet)
		if err != nil || !strings.EqualFold(commitmentEvidence.CommitmentHash, "0x"+hex.EncodeToString(commitmentHash[:])) || !strings.EqualFold(commitmentEvidence.Hotkey, "0x"+hex.EncodeToString(manifest.Hotkey[:])) {
			return JournalEntry{}, nil, nil, stateMismatchError(err, "fleet %d commitment artifact differs from source batch", fleet)
		}
		state["commitment_hash"], state["finalized_block"] = commitmentEvidence.CommitmentHash, commitmentEvidence.FinalizedBlock
		return sourceEntry, sourceRecord, state, nil
	}
	bindingEvidence, binding, err := loadVerifiedPriorFleetBinding(self.stateDir, manifest, fleet, member)
	if err != nil || binding.CommitmentHash != commitmentHash {
		return JournalEntry{}, nil, nil, stateMismatchError(err, "fleet %d member %d artifact differs from source batch", fleet, member)
	}
	state["member"], state["client_id"], state["uid"] = member, bindingEvidence.ClientID, bindingEvidence.UID
	return sourceEntry, sourceRecord, state, nil
}

func (self *Executor) verifyFleetInstallAliasPostcondition(action Action) (*ActionPostcondition, error) {
	_, sourceRecord, observed, err := self.verifyFleetInstallAliasState(action, map[string]any{})
	if err != nil {
		return nil, err
	}
	independentObserved, err := cloneObservedPostState(observed)
	if err != nil {
		return nil, err
	}
	return &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: self.cfg.Config.Deployment.DeploymentID,
		PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		OperationalRPCMode: sourceRecord.OperationalRPCMode, IndependentRPC: sourceRecord.IndependentRPC,
		SubstrateFinalized: sourceRecord.SubstrateFinalized, EVMFinalized: sourceRecord.EVMFinalized, EVMHashDomain: sourceRecord.EVMHashDomain,
		Observed: observed, IndependentSubstrateFinalized: sourceRecord.IndependentSubstrateFinalized,
		IndependentEVMFinalized: sourceRecord.IndependentEVMFinalized, IndependentEVMHashDomain: sourceRecord.IndependentEVMHashDomain,
		IndependentObserved: independentObserved,
	}, nil
}

// Rechecks every standard member artifact, native commitment, EVM mirror and
// binding record at the postcondition's canonical finalized head.
func (self *Executor) verifyFleetInstallBatchPostState(ctx context.Context, action Action, evmHead ChainHead, state map[string]any) (map[string]any, error) {
	if err := self.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	batch := suffixInt(action.ID)
	firstFleet, lastFleet, err := fleetInstallActionRange(self.cfg, action, batch)
	if err != nil {
		return nil, err
	}
	var evidence FleetInstallBatchEvidence
	if err := readJSONFile(filepath.Join(self.stateDir, "public", fmt.Sprintf("fleet-install-batch-%d.json", batch)), &evidence); err != nil {
		return nil, err
	}
	if evidence.Schema != fleetInstallBatchEvidenceSchema || evidence.Batch != batch || evidence.FirstFleet != firstFleet || evidence.LastFleet != lastFleet || evidence.Generation != 1 {
		return nil, errors.New("fleet install batch evidence identity mismatch")
	}
	if err := validateFleetInstallPartitions(evidence); err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	type postconditionSnapshot struct {
		fleetIndex         int
		manifest           protocol.FleetManifest
		commitmentHash     [32]byte
		commitmentEvidence *FleetCommitmentEvidence
		finalizedBlockHash [32]byte
	}
	snapshots := make([]postconditionSnapshot, 0, lastFleet-firstFleet+1)
	calls := make([][]byte, 0, (lastFleet-firstFleet+1)*(1+2*self.cfg.Config.Topology.ClientsPerHeadFleet))
	for fleetIndex := firstFleet; fleetIndex <= lastFleet; fleetIndex++ {
		manifest, commitmentHash, commitmentEvidence, finalizedBlockHash, err := self.validatedFleetCommitmentGeneration(fleetIndex, 1)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, postconditionSnapshot{
			fleetIndex: fleetIndex, manifest: manifest, commitmentHash: commitmentHash,
			commitmentEvidence: commitmentEvidence, finalizedBlockHash: finalizedBlockHash,
		})
		calls = append(calls, coordinator.PackMirroredCommitments(manifest.Hotkey))
		for _, member := range manifest.Members {
			calls = append(calls,
				coordinator.PackBindingVersionCount(member.ClientID),
				coordinator.PackBindingVersionAt(member.ClientID, new(big.Int)),
			)
		}
	}
	outputs, err := rawCoordinatorBatchCallAt(ctx, self.oracle, self.payloads.Manifest.CoordinatorProxy, calls, evmHead.Number)
	if err != nil {
		return nil, err
	}
	outputIndex := 0
	for _, snapshot := range snapshots {
		mirror, err := coordinator.UnpackMirroredCommitments(outputs[outputIndex])
		outputIndex++
		if err != nil || !fleetMirrorMatches(mirror, snapshot.commitmentHash, snapshot.commitmentEvidence.FinalizedBlock, snapshot.finalizedBlockHash) {
			return nil, stateMismatchError(err, "fleet %d install postcondition mirror mismatch", snapshot.fleetIndex)
		}
		for memberIndex := 1; memberIndex <= len(snapshot.manifest.Members); memberIndex++ {
			memberEvidence, binding, err := loadVerifiedPriorFleetBinding(self.stateDir, snapshot.manifest, snapshot.fleetIndex, memberIndex)
			if err != nil {
				return nil, err
			}
			count, err := coordinator.UnpackBindingVersionCount(outputs[outputIndex])
			outputIndex++
			if err != nil {
				return nil, err
			}
			record, err := coordinator.UnpackBindingVersionAt(outputs[outputIndex])
			outputIndex++
			if err != nil || !count.IsUint64() || count.Uint64() != 1 || !fleetBindingRecordMatches(record, binding, binding.ValidToEpoch, memberEvidence.UID) {
				return nil, stateMismatchError(err, "fleet %d member %d install postcondition mismatch", snapshot.fleetIndex, memberIndex)
			}
		}
	}
	wantMemberEvidence := (lastFleet - firstFleet + 1) * self.cfg.Config.Topology.ClientsPerHeadFleet
	if len(evidence.MemberEvidence) != wantMemberEvidence {
		return nil, errors.New("fleet install batch member evidence count mismatch")
	}
	evidenceIndex := 0
	for fleetIndex := firstFleet; fleetIndex <= lastFleet; fleetIndex++ {
		for memberIndex := 1; memberIndex <= self.cfg.Config.Topology.ClientsPerHeadFleet; memberIndex++ {
			want := fmt.Sprintf("fleet-%d-member-%d.binding.json", fleetIndex, memberIndex)
			if evidence.MemberEvidence[evidenceIndex] != want {
				return nil, errors.New("fleet install batch member evidence ordering mismatch")
			}
			evidenceIndex++
		}
	}
	if len(evidence.InstalledFleets) == 0 {
		if evidence.TransactionHash != "" || evidence.BlockNumber != 0 || evidence.BlockHash != "" || evidence.CalldataHash != "" {
			return nil, errors.New("all-carried fleet install unexpectedly names a transaction")
		}
		wantCarried := make([]int, 0, lastFleet-firstFleet+1)
		for fleetIndex := firstFleet; fleetIndex <= lastFleet; fleetIndex++ {
			wantCarried = append(wantCarried, fleetIndex)
		}
		if !slices.Equal(evidence.CarriedFleets, wantCarried) {
			return nil, errors.New("all-carried fleet install partition is not canonical")
		}
	} else {
		if evidence.EffectiveEpoch == 0 || evidence.ValidToEpoch < evidence.EffectiveEpoch {
			return nil, errors.New("fleet install transaction evidence has invalid epochs")
		}
		if _, err := decodeHex32("fleet install transaction hash", evidence.TransactionHash); err != nil {
			return nil, err
		}
		if _, err := decodeHex32("fleet install block hash", evidence.BlockHash); err != nil {
			return nil, err
		}
		receipt, err := self.oracle.client.TransactionReceipt(ctx, common.HexToHash(evidence.TransactionHash))
		if err != nil || receipt.Status != ethTypes.ReceiptStatusSuccessful || receipt.BlockNumber == nil || receipt.BlockNumber.Uint64() != evidence.BlockNumber || receipt.BlockHash.Hex() != evidence.BlockHash || evidence.BlockNumber > evmHead.Number {
			return nil, stateMismatchError(err, "fleet install receipt mismatch")
		}
		canonicalHash, err := canonicalEVMBlockHash(ctx, ethEVMBlockReader{client: self.oracle.client}, evidence.BlockNumber)
		if err != nil || !strings.EqualFold(canonicalHash, evidence.BlockHash) {
			return nil, stateMismatchError(err, "fleet install receipt is not canonical")
		}
		var prepared fleetInstallPreparedEvidence
		if err := readJSONFile(fleetInstallPreparedPath(self.stateDir, batch), &prepared); err != nil {
			return nil, err
		}
		data, err := validateFleetInstallPrepared(&prepared, self.cfg, self.plan, action, batch, firstFleet, lastFleet)
		if err != nil || !strings.EqualFold(crypto.Keccak256Hash(data).Hex(), evidence.CalldataHash) {
			return nil, stateMismatchError(err, "fleet install calldata evidence mismatch")
		}
		parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
		if err != nil {
			return nil, err
		}
		if err := verifyFleetInstallEvents(receipt, parsed, self.payloads.FleetBatcherAddress, &prepared); err != nil {
			return nil, err
		}
		installedFleets := make([]int, 0, len(prepared.Fleets))
		for _, fleet := range prepared.Fleets {
			installedFleets = append(installedFleets, fleet.Fleet)
		}
		if !slices.Equal(evidence.InstalledFleets, installedFleets) || !slices.Equal(evidence.CarriedFleets, prepared.CarriedFleets) {
			return nil, errors.New("fleet install public partition differs from prepared transaction")
		}
	}
	state["batch"] = batch
	state["first_fleet"] = firstFleet
	state["last_fleet"] = lastFleet
	state["generation"] = 1
	state["installed_fleets"] = len(evidence.InstalledFleets)
	state["carried_fleets"] = len(evidence.CarriedFleets)
	state["members"] = wantMemberEvidence
	state["transaction_hash"] = evidence.TransactionHash
	return state, nil
}

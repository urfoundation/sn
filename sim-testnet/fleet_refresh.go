package main

// This file owns the testnet-only, generation-2 fleet renewal. Each batch is
// prepared durably before broadcast, then atomically mirrors native evidence,
// client-revokes generation 1 and installs dual-signed generation 2 bindings.

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
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

const fleetRefreshPreparedSchema = "urnetwork-fleet-refresh-prepared-v1"
const fleetRefreshBindingEvidenceSchema = "urnetwork-fleet-refresh-binding-evidence-v1"
const fleetRefreshBatchEvidenceSchema = "urnetwork-fleet-refresh-batch-evidence-v1"

// Mirrors the nested Solidity ABI tuple. Exported fields are required by the
// ABI reflection layer; the type remains private to the simulator.
type fleetBatcherMemberRefresh struct {
	PriorGeneration uint64
	Binding         stabi.STCoordinatorFleetBinding
	RevokeSignature []byte
	ClientSignature []byte
	HotkeySignature []byte
}

// Mirrors one fleet and all of its bounded member replacements.
type fleetBatcherFleetRefresh struct {
	Hotkey             [32]byte
	CommitmentHash     [32]byte
	FinalizedBlock     uint64
	FinalizedBlockHash [32]byte
	Members            []fleetBatcherMemberRefresh
}

// Preserves every signature and prior/successor coordinate needed to recover
// public evidence after an exact persisted transaction is resumed.
type FleetRefreshBindingEvidence struct {
	Schema                    string `json:"schema"`
	Fleet                     int    `json:"fleet"`
	Member                    int    `json:"member"`
	ClientID                  string `json:"client_id"`
	ClientKey                 string `json:"client_key"`
	FleetID                   string `json:"fleet_id"`
	Hotkey                    string `json:"hotkey"`
	PriorGeneration           uint64 `json:"prior_generation"`
	PriorValidFromEpoch       uint64 `json:"prior_valid_from_epoch"`
	PriorOriginalValidToEpoch uint64 `json:"prior_original_valid_to_epoch"`
	PriorCommitmentHash       string `json:"prior_commitment_hash"`
	ReplacementGeneration     uint64 `json:"replacement_generation"`
	ValidFromEpoch            uint64 `json:"valid_from_epoch"`
	ValidToEpoch              uint64 `json:"valid_to_epoch"`
	CommitmentHash            string `json:"commitment_hash"`
	RevokeDigest              string `json:"revoke_digest"`
	RevokeSignature           string `json:"revoke_signature"`
	BindingDigest             string `json:"binding_digest"`
	ClientSignature           string `json:"client_signature"`
	HotkeySignature           string `json:"hotkey_signature"`
	UID                       uint16 `json:"uid"`
	TransactionHash           string `json:"transaction_hash,omitempty"`
	BlockNumber               uint64 `json:"block_number,omitempty"`
	BlockHash                 string `json:"block_hash,omitempty"`
}

// Couples one native commitment attestation to its exact replacement members.
type fleetRefreshPreparedFleet struct {
	Fleet                 int                           `json:"fleet"`
	ManifestURI           string                        `json:"manifest_uri"`
	CommitmentEvidenceURI string                        `json:"commitment_evidence_uri"`
	Hotkey                string                        `json:"hotkey"`
	CommitmentHash        string                        `json:"commitment_hash"`
	FinalizedBlock        uint64                        `json:"finalized_block"`
	FinalizedBlockHash    string                        `json:"finalized_block_hash"`
	Members               []FleetRefreshBindingEvidence `json:"members"`
}

// Is written before transaction persistence so a crash cannot separate an
// exact signed transaction from the epoch-specific evidence that created it.
type fleetRefreshPreparedEvidence struct {
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
	Calldata       string                      `json:"calldata"`
	CalldataHash   string                      `json:"calldata_hash"`
	Fleets         []fleetRefreshPreparedFleet `json:"fleets"`
}

// Is the public transaction-level proof written after every member post-state
// has been checked at the canonical finalized inclusion block.
type FleetRefreshBatchEvidence struct {
	Schema          string   `json:"schema"`
	Batch           int      `json:"batch"`
	FirstFleet      int      `json:"first_fleet"`
	LastFleet       int      `json:"last_fleet"`
	Generation      uint64   `json:"generation"`
	EffectiveEpoch  uint64   `json:"effective_epoch"`
	ValidToEpoch    uint64   `json:"valid_to_epoch"`
	CalldataHash    string   `json:"calldata_hash"`
	FleetCount      int      `json:"fleet_count"`
	MemberCount     int      `json:"member_count"`
	MemberEvidence  []string `json:"member_evidence"`
	TransactionHash string   `json:"transaction_hash"`
	BlockNumber     uint64   `json:"block_number"`
	BlockHash       string   `json:"block_hash"`
}

// Captures all oracle routing values at one EVM block.
type fleetRefreshOracleState struct {
	CurrentEpoch uint64
	Immutable    common.Address
	Active       common.Address
	Pending      common.Address
	PendingEpoch uint64
}

// Validates the immutable action partition used to bound each transaction.
func fleetRefreshActionRange(cfg *ResolvedConfig, action Action, batch int) (int, int, error) {
	if cfg == nil || batch < 1 {
		return 0, 0, errors.New("fleet refresh action range is unavailable")
	}
	first, err := strconv.Atoi(action.Parameters["first_fleet"])
	if err != nil {
		return 0, 0, fmt.Errorf("fleet refresh first_fleet: %w", err)
	}
	last, err := strconv.Atoi(action.Parameters["last_fleet"])
	if err != nil {
		return 0, 0, fmt.Errorf("fleet refresh last_fleet: %w", err)
	}
	if action.Parameters["generation"] != "2" {
		return 0, 0, errors.New("fleet refresh generation is not 2")
	}
	wantFirst := (batch-1)*fleetRefreshBatchSize + 1
	wantLast := wantFirst + fleetRefreshBatchSize - 1
	if wantLast > cfg.Config.Topology.HeadFleets {
		wantLast = cfg.Config.Topology.HeadFleets
	}
	if first != wantFirst || last != wantLast || first < 1 || last < first || last > cfg.Config.Topology.HeadFleets || last-first+1 > fleetRefreshBatchSize {
		return 0, 0, fmt.Errorf("fleet refresh batch %d range %d..%d, want %d..%d", batch, first, last, wantFirst, wantLast)
	}
	return first, last, nil
}

// Reads all values at one block so an epoch transition cannot mix active and
// pending oracle generations in a single decision.
func readFleetRefreshOracleStateAt(ctx context.Context, manager *EVMTxManager, coordinatorAddress common.Address, coordinator *stabi.STCoordinator, block uint64) (fleetRefreshOracleState, error) {
	var state fleetRefreshOracleState
	if coordinator == nil {
		return state, errors.New("fleet refresh oracle reader is unavailable")
	}
	outputs, err := rawCoordinatorBatchCallAt(ctx, manager, coordinatorAddress, [][]byte{
		coordinator.PackCurrentEpoch(),
		coordinator.PackCommitmentOracle(),
		coordinator.PackActiveCommitmentOracle(),
		coordinator.PackPendingCommitmentOracle(),
		coordinator.PackPendingCommitmentOracleEpoch(),
	}, block)
	if err != nil {
		return state, fmt.Errorf("fleet refresh oracle state batch: %w", err)
	}
	epoch, err := coordinator.UnpackCurrentEpoch(outputs[0])
	if err != nil || !epoch.IsUint64() {
		return state, stateMismatchError(err, "fleet refresh current epoch is not uint64")
	}
	state.CurrentEpoch = epoch.Uint64()
	state.Immutable, err = coordinator.UnpackCommitmentOracle(outputs[1])
	if err != nil {
		return state, err
	}
	state.Active, err = coordinator.UnpackActiveCommitmentOracle(outputs[2])
	if err != nil {
		return state, err
	}
	state.Pending, err = coordinator.UnpackPendingCommitmentOracle(outputs[3])
	if err != nil {
		return state, err
	}
	state.PendingEpoch, err = coordinator.UnpackPendingCommitmentOracleEpoch(outputs[4])
	if err != nil {
		return state, err
	}
	return state, nil
}

// Derives the only oracle address allowed by each immutable action id.
func fleetRefreshOracleTarget(action Action, payloads *DeploymentPayloads) (common.Address, error) {
	if payloads == nil {
		return common.Address{}, errors.New("fleet refresh deployment payloads are unavailable")
	}
	var target common.Address
	switch action.ID {
	case "fleet.refresh.oracle-activate", "fleet.refresh.oracle-await-active":
		target = payloads.FleetBatcherAddress
	case "fleet.refresh.oracle-restore", "fleet.refresh.oracle-await-restored":
		target = payloads.CommitmentOracle
	default:
		return common.Address{}, fmt.Errorf("action %s is not a fleet refresh oracle action", action.ID)
	}
	if action.ID == "fleet.refresh.oracle-restore" || action.ID == "fleet.refresh.oracle-await-restored" {
		generation := action.Parameters[fleetRefreshBatcherParameter]
		presentInvalid := generation != "" && generation != payloads.FleetBatcherAddress.Hex()
		requiredMissing := payloads.PrecompileProbeAddress != payloads.Manifest.PrecompileProbe && generation == ""
		if presentInvalid || requiredMissing {
			return common.Address{}, fmt.Errorf("action %s does not bind the activated fleet batcher generation", action.ID)
		}
	}
	if strings.Contains(action.ID, "await") {
		if !common.IsHexAddress(action.Target) || common.HexToAddress(action.Target) != target {
			return common.Address{}, fmt.Errorf("action %s target differs from its approved oracle", action.ID)
		}
	} else if !common.IsHexAddress(action.Parameters["oracle"]) || common.HexToAddress(action.Parameters["oracle"]) != target {
		return common.Address{}, fmt.Errorf("action %s parameter differs from its approved oracle", action.ID)
	}
	return target, nil
}

// Resolve the hash-bound oracle target directly from a planned handoff action.
// This deliberately does not infer an address from current chain state: it is
// used to prove that an old activation and its later restore are opposite ends
// of the same approved transition.
func plannedFleetRefreshOracleTarget(action Action) (common.Address, error) {
	var raw string
	switch action.ID {
	case "fleet.refresh.oracle-activate", "fleet.refresh.oracle-restore":
		if action.Kind != "evm-transaction" || !common.IsHexAddress(action.Target) || common.HexToAddress(action.Target) == (common.Address{}) {
			return common.Address{}, fmt.Errorf("action %s has no canonical coordinator transaction", action.ID)
		}
		raw = action.Parameters["oracle"]
	case "fleet.refresh.oracle-await-active", "fleet.refresh.oracle-await-restored":
		if action.Kind != "evm-read" {
			return common.Address{}, fmt.Errorf("action %s is not a canonical oracle read", action.ID)
		}
		raw = action.Target
	default:
		return common.Address{}, fmt.Errorf("action %s is not a fleet refresh oracle action", action.ID)
	}
	if !common.IsHexAddress(raw) || common.HexToAddress(raw) == (common.Address{}) {
		return common.Address{}, fmt.Errorf("action %s has no nonzero oracle target", action.ID)
	}
	return common.HexToAddress(raw), nil
}

func oraclePostconditionAddress(record *ActionPostcondition, field string, want common.Address, requireActive bool) error {
	if record == nil || want == (common.Address{}) {
		return errors.New("fleet refresh oracle postcondition context is incomplete")
	}
	for _, observation := range []struct {
		name     string
		observed map[string]any
	}{
		{"operational", record.Observed},
		{"comparison", record.IndependentObserved},
	} {
		target, targetOK := observation.observed[field].(string)
		if !targetOK || !strings.EqualFold(target, want.Hex()) {
			return fmt.Errorf("%s fleet refresh oracle %s differs from %s", observation.name, field, want)
		}
		if requireActive {
			active, activeOK := observation.observed["active_oracle"].(string)
			if !activeOK || !strings.EqualFold(active, want.Hex()) {
				return fmt.Errorf("%s fleet refresh active oracle differs from restored %s", observation.name, want)
			}
		}
	}
	return nil
}

func validateFleetRefreshOraclePostconditionIdentity(action Action, entry JournalEntry, record *ActionPostcondition) error {
	if entry.Sequence == 0 || entry.Stage != StageVerified || entry.ActionID != action.ID || !actionAcceptsIntent(action, entry.IntentHash) {
		return fmt.Errorf("fleet refresh oracle action %s has no exact verified journal identity", action.ID)
	}
	if record == nil || record.PlanHash != entry.PlanHash || record.ActionID != entry.ActionID || record.IntentHash != entry.IntentHash {
		return fmt.Errorf("fleet refresh oracle action %s has no exact postcondition identity", action.ID)
	}
	return nil
}

// Prove that an activation checkpoint was intentionally consumed by the exact
// later restore and await-restored actions. Journal order and both observer
// checkpoints must advance monotonically; an earlier same-state observation or
// an adjacent oracle action cannot authorize historical-only replay.
func validateFleetRefreshOracleSupersession(
	activeAction Action,
	activeEntry JournalEntry,
	activeRecord *ActionPostcondition,
	restoreAction Action,
	restoreEntry JournalEntry,
	restoreRecord *ActionPostcondition,
	awaitAction Action,
	awaitEntry JournalEntry,
	awaitRecord *ActionPostcondition,
) error {
	if activeAction.ID != "fleet.refresh.oracle-activate" && activeAction.ID != "fleet.refresh.oracle-await-active" {
		return fmt.Errorf("action %s is not a supersedable fleet refresh activation", activeAction.ID)
	}
	if restoreAction.ID != "fleet.refresh.oracle-restore" || awaitAction.ID != "fleet.refresh.oracle-await-restored" {
		return errors.New("fleet refresh oracle supersession does not use the exact restore pair")
	}
	activeTarget, err := plannedFleetRefreshOracleTarget(activeAction)
	if err != nil {
		return err
	}
	restoreTarget, err := plannedFleetRefreshOracleTarget(restoreAction)
	if err != nil {
		return err
	}
	awaitTarget, err := plannedFleetRefreshOracleTarget(awaitAction)
	if err != nil {
		return err
	}
	if activeTarget == restoreTarget || restoreTarget != awaitTarget || (activeAction.ID == "fleet.refresh.oracle-activate" && !strings.EqualFold(restoreAction.Target, activeAction.Target)) {
		return errors.New("fleet refresh oracle restore does not reverse the exact activation")
	}
	if len(awaitAction.DependsOn) != 1 || awaitAction.DependsOn[0] != restoreAction.ID {
		return errors.New("fleet refresh await-restored action does not depend exactly on restore")
	}
	if activeAction.ID == "fleet.refresh.oracle-await-active" && (len(activeAction.DependsOn) != 1 || activeAction.DependsOn[0] != "fleet.refresh.oracle-activate") {
		return errors.New("fleet refresh await-active action does not depend exactly on activation")
	}
	for _, evidence := range []struct {
		action Action
		entry  JournalEntry
		record *ActionPostcondition
	}{
		{activeAction, activeEntry, activeRecord},
		{restoreAction, restoreEntry, restoreRecord},
		{awaitAction, awaitEntry, awaitRecord},
	} {
		if err := validateFleetRefreshOraclePostconditionIdentity(evidence.action, evidence.entry, evidence.record); err != nil {
			return err
		}
	}
	if restoreEntry.Sequence <= activeEntry.Sequence || awaitEntry.Sequence <= restoreEntry.Sequence {
		return errors.New("fleet refresh oracle restore evidence is not later in journal order")
	}
	if activeRecord.EVMFinalized.Number == 0 || activeRecord.IndependentEVMFinalized.Number == 0 ||
		restoreRecord.EVMFinalized.Number < activeRecord.EVMFinalized.Number || restoreRecord.IndependentEVMFinalized.Number < activeRecord.IndependentEVMFinalized.Number ||
		awaitRecord.EVMFinalized.Number < restoreRecord.EVMFinalized.Number || awaitRecord.IndependentEVMFinalized.Number < restoreRecord.IndependentEVMFinalized.Number {
		return errors.New("fleet refresh oracle restore checkpoints do not follow activation")
	}
	if err := oraclePostconditionAddress(activeRecord, "target_oracle", activeTarget, activeAction.ID == "fleet.refresh.oracle-await-active"); err != nil {
		return err
	}
	if err := oraclePostconditionAddress(restoreRecord, "target_oracle", restoreTarget, false); err != nil {
		return err
	}
	return oraclePostconditionAddress(awaitRecord, "target_oracle", awaitTarget, true)
}

func (self *Executor) fleetRefreshOracleActivationSuperseded(action Action, verified JournalEntry, record *ActionPostcondition) (bool, error) {
	if action.ID != "fleet.refresh.oracle-activate" && action.ID != "fleet.refresh.oracle-await-active" {
		return false, nil
	}
	if self == nil || self.plan == nil || self.journal == nil {
		return false, errors.New("fleet refresh oracle supersession context is unavailable")
	}
	restore, err := self.planAction("fleet.refresh.oracle-restore")
	if err != nil {
		return false, err
	}
	awaitRestored, err := self.planAction("fleet.refresh.oracle-await-restored")
	if err != nil {
		return false, err
	}
	restoreEntry, restoreVerified := self.verifiedActionEntry(restore)
	awaitEntry, awaitVerified := self.verifiedActionEntry(awaitRestored)
	if !restoreVerified && !awaitVerified {
		return false, nil
	}
	if !restoreVerified || !awaitVerified {
		if awaitVerified {
			return false, errors.New("fleet refresh await-restored is verified without its restore")
		}
		return false, nil
	}
	restoreRecord, err := self.readPersistedPostcondition(restoreEntry)
	if err != nil {
		return false, fmt.Errorf("fleet refresh restore postcondition: %w", err)
	}
	awaitRecord, err := self.readPersistedPostcondition(awaitEntry)
	if err != nil {
		return false, fmt.Errorf("fleet refresh await-restored postcondition: %w", err)
	}
	if err := validateFleetRefreshOracleSupersession(action, verified, record, restore, restoreEntry, restoreRecord, awaitRestored, awaitEntry, awaitRecord); err != nil {
		return false, err
	}
	return true, nil
}

// Schedules one future-effective handoff and retries only if estimation crossed
// the epoch boundary before any exact transaction was persisted.
func (self *Executor) scheduleFleetRefreshOracle(ctx context.Context, action Action) error {
	if err := self.ensurePayloads(ctx); err != nil {
		return err
	}
	target, err := fleetRefreshOracleTarget(action, self.payloads)
	if err != nil {
		return err
	}
	coordinator := stabi.NewSTCoordinator()
	coordinatorAddress := self.payloads.Manifest.CoordinatorProxy
	for {
		window, err := waitFutureEpochTransactionWindow(ctx, self.owner, coordinatorAddress, coordinator)
		if err != nil {
			return err
		}
		state, err := readFleetRefreshOracleStateAt(ctx, self.owner, coordinatorAddress, coordinator, window.HeadBlock)
		if err != nil {
			return err
		}
		if state.Immutable != self.payloads.CommitmentOracle {
			return fmt.Errorf("coordinator immutable commitment oracle %s differs from deployment %s", state.Immutable, self.payloads.CommitmentOracle)
		}
		if state.Active == target || (state.Pending == target && state.PendingEpoch > state.CurrentEpoch) {
			return nil
		}
		data, err := coordinator.TryPackScheduleCommitmentOracle(target, window.EffectiveEpoch)
		if err != nil {
			return err
		}
		receipt, sendErr := self.owner.Send(ctx, self.plan.PlanHash, action, &coordinatorAddress, big.NewInt(0), data)
		if sendErr != nil {
			_, persisted := self.owner.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
			latest, _, readErr := readFutureEpochTransactionWindow(ctx, self.owner, coordinatorAddress, coordinator)
			if readErr == nil && retryFleetBindingAfterEpochTransition(latest.CurrentEpoch, window.EffectiveEpoch, persisted) {
				continue
			}
			return sendErr
		}
		post, err := readFleetRefreshOracleStateAt(ctx, self.owner, coordinatorAddress, coordinator, receipt.BlockNumber.Uint64())
		if err != nil {
			return err
		}
		if post.Active != target && (post.Pending != target || post.PendingEpoch != window.EffectiveEpoch || post.PendingEpoch <= post.CurrentEpoch) {
			return errors.New("fleet refresh oracle schedule postcondition mismatch")
		}
		return nil
	}
}

// Waits at finalized heads until the scheduled address is actually active.
func (self *Executor) awaitFleetRefreshOracle(ctx context.Context, action Action) error {
	if err := self.ensurePayloads(ctx); err != nil {
		return err
	}
	target, err := fleetRefreshOracleTarget(action, self.payloads)
	if err != nil {
		return err
	}
	coordinator := stabi.NewSTCoordinator()
	coordinatorAddress := self.payloads.Manifest.CoordinatorProxy
	for {
		head, err := finalizedEVMHead(ctx, self.owner.client)
		if err != nil {
			return err
		}
		state, err := readFleetRefreshOracleStateAt(ctx, self.owner, coordinatorAddress, coordinator, head.Number)
		if err != nil {
			return err
		}
		if state.Immutable != self.payloads.CommitmentOracle {
			return errors.New("immutable commitment oracle changed during fleet refresh")
		}
		if state.Active == target {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// Verifies a generation-specific native commitment at its exact canonical
// finalized block. Until its exact EVM consumer verifies, the commitment must
// also be the current finalized pallet value. A consumed generation remains
// replayable after a successor or the bounded precompile drill replaces it.
func (self *Executor) validatedFleetCommitmentGeneration(fleetIndex int, generation uint64) (protocol.FleetManifest, [32]byte, *FleetCommitmentEvidence, [32]byte, error) {
	manifest, _, commitmentHash, err := fleetManifestForGeneration(self.cfg, self.stateDir, self.roles, fleetIndex, generation)
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, [32]byte{}, err
	}
	evidence, err := loadFleetCommitmentEvidenceGeneration(self.stateDir, fleetIndex, generation)
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, [32]byte{}, err
	}
	evidenceHash, err := decodeHex32("fleet refresh commitment", evidence.CommitmentHash)
	if err != nil || evidenceHash != commitmentHash {
		return protocol.FleetManifest{}, [32]byte{}, nil, [32]byte{}, stateMismatchError(err, "fleet %d generation %d commitment hash mismatch", fleetIndex, generation)
	}
	finalizedHash, err := types.NewHashFromHexString(evidence.FinalizedBlockHash)
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, [32]byte{}, err
	}
	canonicalHash, err := self.substrate.chain.API.RPC.Chain.GetBlockHash(evidence.FinalizedBlock)
	if err != nil || canonicalHash != finalizedHash {
		return protocol.FleetManifest{}, [32]byte{}, nil, [32]byte{}, stateMismatchError(err, "fleet %d generation %d commitment block is not canonical", fleetIndex, generation)
	}
	historical, err := self.substrate.fleetCommitmentAt(manifest.Hotkey, finalizedHash)
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, [32]byte{}, err
	}
	if err := crv4.ValidateFleetCommitmentWrite(commitmentHash, evidence.FinalizedBlock, historical); err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, [32]byte{}, err
	}
	_, consumed, err := self.consumedFleetCommitmentGeneration(fleetIndex, generation)
	if err != nil {
		return protocol.FleetManifest{}, [32]byte{}, nil, [32]byte{}, err
	}
	if consumed {
		return manifest, commitmentHash, evidence, [32]byte(finalizedHash), nil
	}
	current, err := self.substrate.fleetCommitmentFinalized(manifest.Hotkey)
	if err != nil || current.Hash != commitmentHash || current.CommitmentBlock != evidence.CommitmentBlock {
		return protocol.FleetManifest{}, [32]byte{}, nil, [32]byte{}, stateMismatchError(err, "fleet %d generation %d is not the exact current finalized commitment", fleetIndex, generation)
	}
	return manifest, commitmentHash, evidence, [32]byte(finalizedHash), nil
}

// Requires every generation-1 evidence field and signature to agree with the
// deterministic manifest before it can authorize a successor.
func loadVerifiedPriorFleetBinding(stateDir string, manifest protocol.FleetManifest, fleetIndex, memberIndex int) (FleetBindingEvidence, protocol.FleetBinding, error) {
	var evidence FleetBindingEvidence
	path := filepath.Join(stateDir, "public", fmt.Sprintf("fleet-%d-member-%d.binding.json", fleetIndex, memberIndex))
	if err := readJSONFile(path, &evidence); err != nil {
		return evidence, protocol.FleetBinding{}, err
	}
	if evidence.Schema != "urnetwork-fleet-binding-evidence-v1" || evidence.Generation != 1 || memberIndex < 1 || memberIndex > len(manifest.Members) {
		return evidence, protocol.FleetBinding{}, errors.New("prior fleet binding evidence has invalid identity or generation")
	}
	if evidence.BlockNumber == 0 {
		return evidence, protocol.FleetBinding{}, errors.New("prior fleet binding evidence has no inclusion block")
	}
	if _, err := decodeHex32("prior fleet binding transaction", evidence.TransactionHash); err != nil {
		return evidence, protocol.FleetBinding{}, err
	}
	if _, err := decodeHex32("prior fleet binding block", evidence.BlockHash); err != nil {
		return evidence, protocol.FleetBinding{}, err
	}
	binding, err := manifest.Binding(manifest.Members[memberIndex-1], evidence.ValidFromEpoch, evidence.ValidToEpoch)
	if err != nil {
		return evidence, protocol.FleetBinding{}, err
	}
	exactHex := []struct {
		encoded string
		want    []byte
	}{
		{encoded: evidence.ClientID, want: binding.ClientID[:]},
		{encoded: evidence.ClientKey, want: binding.ClientKey[:]},
		{encoded: evidence.FleetID, want: binding.FleetID[:]},
		{encoded: evidence.Hotkey, want: binding.Hotkey[:]},
		{encoded: evidence.CommitmentHash, want: binding.CommitmentHash[:]},
	}
	for _, field := range exactHex {
		decoded, ok := evidenceFixedHex(field.encoded, len(field.want))
		if !ok || string(decoded) != string(field.want) {
			return evidence, protocol.FleetBinding{}, errors.New("prior fleet binding evidence differs from its manifest")
		}
	}
	digest, err := binding.Digest()
	if err != nil || !strings.EqualFold(evidence.BindingDigest, "0x"+hex.EncodeToString(digest[:])) {
		return evidence, protocol.FleetBinding{}, stateMismatchError(err, "prior fleet binding digest mismatch")
	}
	clientSignature, ok := evidenceFixedHex(evidence.ClientSignature, ed25519.SignatureSize)
	if !ok || !binding.VerifyClient(clientSignature) {
		return evidence, protocol.FleetBinding{}, errors.New("prior fleet client signature is invalid")
	}
	hotkeySignature, ok := evidenceFixedHex(evidence.HotkeySignature, 64)
	if !ok || !binding.VerifyHotkey(hotkeySignature) {
		return evidence, protocol.FleetBinding{}, errors.New("prior fleet hotkey signature is invalid")
	}
	return evidence, binding, nil
}

// Compares every coordinator record field represented by a signed binding.
func fleetBindingRecordMatches(record stabi.STCoordinatorBindingRecord, binding protocol.FleetBinding, validTo uint64, uid uint16) bool {
	return record.FleetId == binding.FleetID && record.Hotkey == binding.Hotkey && record.ClientKey == binding.ClientKey &&
		record.CommitmentHash == binding.CommitmentHash && record.Generation == binding.Generation &&
		record.ValidFromEpoch == binding.ValidFromEpoch && record.ValidToEpoch == validTo && record.Uid == uid && !record.Cleaned && record.CleanedAtEpoch == 0
}

type fleetBindingVersionRead struct {
	Count  *big.Int
	Record stabi.STCoordinatorBindingRecord
}

// Binds one signed replacement to the exact predecessor record which must be
// truncated at the atomic refresh epoch.
type fleetRefreshVerificationMember struct {
	Evidence     FleetRefreshBindingEvidence
	Binding      protocol.FleetBinding
	PriorBinding protocol.FleetBinding
}

// Carries locally authenticated native and member expectations into one
// block-pinned coordinator batch. No RPC result can change these expectations.
type fleetRefreshVerificationFleet struct {
	Fleet              int
	Hotkey             [32]byte
	CommitmentHash     [32]byte
	FinalizedBlock     uint64
	FinalizedBlockHash [32]byte
	FleetID            [32]byte
	Members            []fleetRefreshVerificationMember
}

// Loads version counts and records from one pinned snapshot in bounded
// JSON-RPC batches instead of two HTTP requests per member.
func readFleetBindingVersionsAt(ctx context.Context, manager *EVMTxManager, coordinatorAddress common.Address, coordinator *stabi.STCoordinator, clientIDs [][16]byte, index uint64, block uint64) ([]fleetBindingVersionRead, error) {
	if coordinator == nil || len(clientIDs) == 0 {
		return nil, errors.New("fleet binding version batch is unavailable")
	}
	calls := make([][]byte, 0, 2*len(clientIDs))
	for _, clientID := range clientIDs {
		calls = append(calls,
			coordinator.PackBindingVersionCount(clientID),
			coordinator.PackBindingVersionAt(clientID, new(big.Int).SetUint64(index)),
		)
	}
	outputs, err := rawCoordinatorBatchCallAt(ctx, manager, coordinatorAddress, calls, block)
	if err != nil {
		return nil, err
	}
	reads := make([]fleetBindingVersionRead, len(clientIDs))
	for clientIndex := range clientIDs {
		count, err := coordinator.UnpackBindingVersionCount(outputs[2*clientIndex])
		if err != nil {
			return nil, fmt.Errorf("decode fleet binding %d version count: %w", clientIndex, err)
		}
		record, err := coordinator.UnpackBindingVersionAt(outputs[2*clientIndex+1])
		if err != nil {
			return nil, fmt.Errorf("decode fleet binding %d version: %w", clientIndex, err)
		}
		reads[clientIndex] = fleetBindingVersionRead{Count: count, Record: record}
	}
	return reads, nil
}

// Preserves one bounded request for the targeted single-member verifier while
// bulk preparation passes every member to the slice API above.
func readFleetBindingVersionAt(ctx context.Context, manager *EVMTxManager, coordinatorAddress common.Address, coordinator *stabi.STCoordinator, clientID [16]byte, index uint64, block uint64) (*big.Int, stabi.STCoordinatorBindingRecord, error) {
	reads, err := readFleetBindingVersionsAt(ctx, manager, coordinatorAddress, coordinator, [][16]byte{clientID}, index, block)
	if err != nil {
		return nil, stabi.STCoordinatorBindingRecord{}, err
	}
	return reads[0].Count, reads[0].Record, nil
}

// Reads every mirror, predecessor, successor and fleet cardinality through
// bounded batches. One production refresh is 140 calls and three HTTP requests
// instead of one hundred independently rate-gated requests.
func verifyFleetRefreshStateAt(ctx context.Context, manager *EVMTxManager, coordinatorAddress common.Address, coordinator *stabi.STCoordinator, fleets []fleetRefreshVerificationFleet, block uint64) (int, error) {
	if coordinator == nil || len(fleets) == 0 || block == 0 {
		return 0, errors.New("fleet refresh state batch is unavailable")
	}
	calls := make([][]byte, 0)
	for _, fleet := range fleets {
		if fleet.Fleet < 1 || len(fleet.Members) == 0 {
			return 0, errors.New("fleet refresh state batch has an incomplete fleet")
		}
		calls = append(calls, coordinator.PackMirroredCommitments(fleet.Hotkey))
		for _, member := range fleet.Members {
			if member.Binding.ValidFromEpoch == 0 || member.Binding.FleetID != fleet.FleetID || member.PriorBinding.FleetID != fleet.FleetID || member.Binding.ClientID != member.PriorBinding.ClientID {
				return 0, fmt.Errorf("fleet %d member %d verification identity is incomplete", fleet.Fleet, member.Evidence.Member)
			}
			calls = append(calls,
				coordinator.PackBindingVersionCount(member.Binding.ClientID),
				coordinator.PackBindingVersionAt(member.Binding.ClientID, new(big.Int)),
				coordinator.PackBindingVersionAt(member.Binding.ClientID, big.NewInt(1)),
			)
		}
		calls = append(calls, coordinator.PackFleetMemberCount(fleet.FleetID))
	}
	outputs, err := rawCoordinatorBatchCallAt(ctx, manager, coordinatorAddress, calls, block)
	if err != nil {
		return 0, fmt.Errorf("fleet refresh state batch: %w", err)
	}
	outputIndex := 0
	members := 0
	for _, fleet := range fleets {
		mirror, err := coordinator.UnpackMirroredCommitments(outputs[outputIndex])
		outputIndex++
		if err != nil || !fleetMirrorMatches(mirror, fleet.CommitmentHash, fleet.FinalizedBlock, fleet.FinalizedBlockHash) {
			return 0, stateMismatchError(err, "fleet %d refreshed mirror mismatch", fleet.Fleet)
		}
		for _, member := range fleet.Members {
			count, err := coordinator.UnpackBindingVersionCount(outputs[outputIndex])
			outputIndex++
			if err != nil || count == nil || !count.IsUint64() || count.Uint64() != 2 {
				return 0, stateMismatchError(err, "fleet %d member %d binding version count=%v, want 2", fleet.Fleet, member.Evidence.Member, count)
			}
			prior, err := coordinator.UnpackBindingVersionAt(outputs[outputIndex])
			outputIndex++
			if err != nil || !fleetBindingRecordMatches(prior, member.PriorBinding, member.Binding.ValidFromEpoch-1, member.Evidence.UID) {
				return 0, stateMismatchError(err, "fleet %d member %d revoked generation mismatch", fleet.Fleet, member.Evidence.Member)
			}
			successor, err := coordinator.UnpackBindingVersionAt(outputs[outputIndex])
			outputIndex++
			if err != nil || !fleetBindingRecordMatches(successor, member.Binding, member.Binding.ValidToEpoch, member.Evidence.UID) {
				return 0, stateMismatchError(err, "fleet %d member %d replacement generation mismatch", fleet.Fleet, member.Evidence.Member)
			}
			members++
		}
		memberCount, err := coordinator.UnpackFleetMemberCount(outputs[outputIndex])
		outputIndex++
		if err != nil || memberCount == nil || !memberCount.IsUint64() || memberCount.Uint64() != uint64(len(fleet.Members)) {
			return 0, stateMismatchError(err, "fleet %d member count=%v, want %d", fleet.Fleet, memberCount, len(fleet.Members))
		}
	}
	if outputIndex != len(outputs) {
		return 0, errors.New("fleet refresh state batch output partition is inconsistent")
	}
	return members, nil
}

// Rejects expired, cleaned or overlapping generation-1 state before any new
// signatures are persisted.
func validateFleetRefreshPriorState(currentEpoch, effectiveEpoch uint64, priorEvidence FleetBindingEvidence, record stabi.STCoordinatorBindingRecord, binding protocol.FleetBinding) error {
	if effectiveEpoch <= currentEpoch {
		return errors.New("fleet refresh effective epoch is not future")
	}
	if effectiveEpoch > priorEvidence.ValidToEpoch {
		return fmt.Errorf("fleet generation 1 expires at epoch %d before replacement epoch %d", priorEvidence.ValidToEpoch, effectiveEpoch)
	}
	if !fleetBindingRecordMatches(record, binding, priorEvidence.ValidToEpoch, priorEvidence.UID) {
		return errors.New("fleet generation-1 on-chain record differs from signed evidence")
	}
	return nil
}

// Builds and signs one batch from exact native and EVM state at the selected
// safe latest-head epoch window.
func (self *Executor) prepareFleetRefreshBatch(ctx context.Context, action Action, batch, firstFleet, lastFleet int, window futureEpochTransactionWindow) (*fleetRefreshPreparedEvidence, error) {
	validToEpoch, err := fleetBindingValidityEnd(window.EffectiveEpoch, self.cfg.Policy.Binding.MaximumValidityEpochs)
	if err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	coordinatorAddress := self.payloads.Manifest.CoordinatorProxy
	oracleState, err := readFleetRefreshOracleStateAt(ctx, self.oracle, coordinatorAddress, coordinator, window.HeadBlock)
	if err != nil {
		return nil, err
	}
	if oracleState.Immutable != self.payloads.CommitmentOracle || oracleState.Active != self.payloads.FleetBatcherAddress {
		return nil, errors.New("fleet refresh batcher is not the active oracle")
	}
	parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
	if err != nil {
		return nil, err
	}
	prepared := &fleetRefreshPreparedEvidence{
		Schema: fleetRefreshPreparedSchema, DeploymentID: self.cfg.Config.Deployment.DeploymentID,
		PlanHash: self.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		Batch: batch, FirstFleet: firstFleet, LastFleet: lastFleet, Generation: 2,
		EffectiveEpoch: window.EffectiveEpoch, ValidToEpoch: validToEpoch,
	}
	// Keep authenticated local evidence alongside its exact batched read so a
	// second file pass cannot race the state used to create signatures.
	type preparationMember struct {
		MemberIndex   int
		PriorEvidence FleetBindingEvidence
		PriorBinding  protocol.FleetBinding
	}
	type preparationFleet struct {
		FleetIndex         int
		Manifest           protocol.FleetManifest
		CommitmentHash     [32]byte
		CommitmentEvidence *FleetCommitmentEvidence
		FinalizedBlockHash [32]byte
		Members            []preparationMember
	}
	preparationFleets := make([]preparationFleet, 0, lastFleet-firstFleet+1)
	priorClientIDs := make([][16]byte, 0, (lastFleet-firstFleet+1)*self.cfg.Config.Topology.ClientsPerHeadFleet)
	seenPriorClientIDs := map[[16]byte]bool{}
	for fleetIndex := firstFleet; fleetIndex <= lastFleet; fleetIndex++ {
		priorManifest, _, _, err := fleetManifestForGeneration(self.cfg, self.stateDir, self.roles, fleetIndex, 1)
		if err != nil {
			return nil, err
		}
		manifest, commitmentHash, commitmentEvidence, finalizedBlockHash, err := self.validatedFleetCommitmentGeneration(fleetIndex, 2)
		if err != nil {
			return nil, err
		}
		commitmentAction, err := self.planAction(fmt.Sprintf("fleet.refresh.commitment.%d", fleetIndex))
		if err != nil {
			return nil, err
		}
		if err := validateFleetCommitmentInclusionLifetime(self.cfg, commitmentAction, commitmentEvidence, window.HeadBlock); err != nil {
			return nil, err
		}
		preparation := preparationFleet{
			FleetIndex: fleetIndex, Manifest: manifest, CommitmentHash: commitmentHash,
			CommitmentEvidence: commitmentEvidence, FinalizedBlockHash: finalizedBlockHash,
			Members: make([]preparationMember, 0, len(manifest.Members)),
		}
		for memberIndex := 1; memberIndex <= len(manifest.Members); memberIndex++ {
			priorEvidence, priorBinding, err := loadVerifiedPriorFleetBinding(self.stateDir, priorManifest, fleetIndex, memberIndex)
			if err != nil {
				return nil, fmt.Errorf("fleet %d member %d prior evidence: %w", fleetIndex, memberIndex, err)
			}
			if seenPriorClientIDs[priorBinding.ClientID] {
				return nil, fmt.Errorf("fleet %d member %d duplicates a refresh client id", fleetIndex, memberIndex)
			}
			seenPriorClientIDs[priorBinding.ClientID] = true
			preparation.Members = append(preparation.Members, preparationMember{
				MemberIndex: memberIndex, PriorEvidence: priorEvidence, PriorBinding: priorBinding,
			})
			priorClientIDs = append(priorClientIDs, priorBinding.ClientID)
		}
		preparationFleets = append(preparationFleets, preparation)
	}
	priorReads, err := readFleetBindingVersionsAt(ctx, self.oracle, coordinatorAddress, coordinator, priorClientIDs, 0, window.HeadBlock)
	if err != nil {
		return nil, fmt.Errorf("fleet refresh prior-state batch: %w", err)
	}
	if len(priorReads) != len(priorClientIDs) {
		return nil, errors.New("fleet refresh prior-state batch result count mismatch")
	}
	contractFleets := make([]fleetBatcherFleetRefresh, 0, len(preparationFleets))
	priorReadIndex := 0
	for _, preparation := range preparationFleets {
		fleetIndex := preparation.FleetIndex
		manifest := preparation.Manifest
		commitmentHash := preparation.CommitmentHash
		commitmentEvidence := preparation.CommitmentEvidence
		finalizedBlockHash := preparation.FinalizedBlockHash
		hotkeyRole := self.roles.Substrate[fleetHotkeyLabel(fleetIndex)]
		hotkey, err := crv4.KeypairFromSeedHex(hotkeyRole.SeedHex)
		if err != nil {
			return nil, err
		}
		contractFleet := fleetBatcherFleetRefresh{
			Hotkey: manifest.Hotkey, CommitmentHash: commitmentHash,
			FinalizedBlock: commitmentEvidence.FinalizedBlock, FinalizedBlockHash: finalizedBlockHash,
		}
		preparedFleet := fleetRefreshPreparedFleet{
			Fleet: fleetIndex, ManifestURI: commitmentEvidence.ManifestURI,
			CommitmentEvidenceURI: fmt.Sprintf("fleet-%d.refresh.commitment.json", fleetIndex),
			Hotkey:                "0x" + hex.EncodeToString(manifest.Hotkey[:]), CommitmentHash: "0x" + hex.EncodeToString(commitmentHash[:]),
			FinalizedBlock: commitmentEvidence.FinalizedBlock, FinalizedBlockHash: commitmentEvidence.FinalizedBlockHash,
		}
		for _, preparationMember := range preparation.Members {
			memberIndex := preparationMember.MemberIndex
			priorEvidence := preparationMember.PriorEvidence
			priorBinding := preparationMember.PriorBinding
			priorRead := priorReads[priorReadIndex]
			priorReadIndex++
			if priorRead.Count == nil || !priorRead.Count.IsUint64() || priorRead.Count.Uint64() != 1 {
				return nil, fmt.Errorf("fleet %d member %d has %v binding versions before atomic refresh", fleetIndex, memberIndex, priorRead.Count)
			}
			if err := validateFleetRefreshPriorState(window.CurrentEpoch, window.EffectiveEpoch, priorEvidence, priorRead.Record, priorBinding); err != nil {
				return nil, fmt.Errorf("fleet %d member %d: %w", fleetIndex, memberIndex, err)
			}
			minerIndex := fleetMemberMinerIndex(self.cfg, fleetIndex, memberIndex)
			clientRole, ok := self.roles.Clients[fmt.Sprintf("miner-%d", minerIndex)]
			if !ok {
				return nil, fmt.Errorf("miner-%d client role is absent", minerIndex)
			}
			seed, err := hex.DecodeString(clientRole.SeedHex)
			if err != nil || len(seed) != ed25519.SeedSize {
				return nil, fmt.Errorf("miner-%d client seed is invalid", minerIndex)
			}
			clientPrivateKey := ed25519.NewKeyFromSeed(seed)
			revoke := protocol.FleetRevoke{
				ChainID: manifest.ChainID, Netuid: manifest.Netuid, Coordinator: manifest.Coordinator,
				ClientID: manifest.Members[memberIndex-1].ClientID, Generation: priorBinding.Generation, EffectiveEpoch: window.EffectiveEpoch,
			}
			revokeDigest, err := revoke.Digest()
			if err != nil {
				return nil, err
			}
			revokeSignature, err := revoke.SignClient(clientPrivateKey)
			if err != nil || !revoke.VerifyClient(ed25519.PublicKey(priorBinding.ClientKey[:]), revokeSignature) {
				return nil, stateMismatchError(err, "fleet %d member %d revoke signature did not verify", fleetIndex, memberIndex)
			}
			replacement, err := manifest.Binding(manifest.Members[memberIndex-1], window.EffectiveEpoch, validToEpoch)
			if err != nil {
				return nil, err
			}
			clientSignature, err := replacement.SignClient(clientPrivateKey)
			if err != nil {
				return nil, err
			}
			bindingDigest, err := replacement.Digest()
			if err != nil {
				return nil, err
			}
			hotkeySignature, err := hotkey.Sign(bindingDigest[:])
			if err != nil || !replacement.VerifyClient(clientSignature) || !replacement.VerifyHotkey(hotkeySignature) {
				return nil, stateMismatchError(err, "fleet %d member %d replacement signatures did not verify", fleetIndex, memberIndex)
			}
			contractBinding := stabi.STCoordinatorFleetBinding{
				ChainId: replacement.ChainID, Netuid: replacement.Netuid, Coordinator: coordinatorAddress,
				FleetId: replacement.FleetID, Hotkey: replacement.Hotkey, ClientId: replacement.ClientID,
				ClientKey: replacement.ClientKey, Generation: replacement.Generation,
				ValidFromEpoch: replacement.ValidFromEpoch, ValidToEpoch: replacement.ValidToEpoch,
				CommitmentHash: replacement.CommitmentHash,
			}
			contractFleet.Members = append(contractFleet.Members, fleetBatcherMemberRefresh{
				PriorGeneration: priorBinding.Generation, Binding: contractBinding,
				RevokeSignature: revokeSignature, ClientSignature: clientSignature, HotkeySignature: hotkeySignature,
			})
			memberEvidence := FleetRefreshBindingEvidence{
				Schema: fleetRefreshBindingEvidenceSchema, Fleet: fleetIndex, Member: memberIndex,
				ClientID: "0x" + hex.EncodeToString(replacement.ClientID[:]), ClientKey: "0x" + hex.EncodeToString(replacement.ClientKey[:]),
				FleetID: "0x" + hex.EncodeToString(replacement.FleetID[:]), Hotkey: "0x" + hex.EncodeToString(replacement.Hotkey[:]),
				PriorGeneration: priorBinding.Generation, PriorValidFromEpoch: priorBinding.ValidFromEpoch,
				PriorOriginalValidToEpoch: priorBinding.ValidToEpoch, PriorCommitmentHash: "0x" + hex.EncodeToString(priorBinding.CommitmentHash[:]),
				ReplacementGeneration: replacement.Generation, ValidFromEpoch: replacement.ValidFromEpoch, ValidToEpoch: replacement.ValidToEpoch,
				CommitmentHash: "0x" + hex.EncodeToString(replacement.CommitmentHash[:]), RevokeDigest: "0x" + hex.EncodeToString(revokeDigest[:]),
				RevokeSignature: "0x" + hex.EncodeToString(revokeSignature), BindingDigest: "0x" + hex.EncodeToString(bindingDigest[:]),
				ClientSignature: "0x" + hex.EncodeToString(clientSignature), HotkeySignature: "0x" + hex.EncodeToString(hotkeySignature), UID: priorEvidence.UID,
			}
			preparedFleet.Members = append(preparedFleet.Members, memberEvidence)
		}
		contractFleets = append(contractFleets, contractFleet)
		prepared.Fleets = append(prepared.Fleets, preparedFleet)
	}
	if priorReadIndex != len(priorReads) {
		return nil, errors.New("fleet refresh prior-state batch partition mismatch")
	}
	data, err := parsed.Pack("refresh", contractFleets)
	if err != nil {
		return nil, fmt.Errorf("pack fleet refresh batch: %w", err)
	}
	prepared.Calldata = "0x" + hex.EncodeToString(data)
	prepared.CalldataHash = crypto.Keccak256Hash(data).Hex()
	return prepared, nil
}

// Uses a private durable path because prepared data is transaction recovery
// state, not public proof until its transaction finalizes successfully.
func fleetRefreshPreparedPath(stateDir string, batch int) string {
	return filepath.Join(stateDir, "secrets", fmt.Sprintf("fleet-refresh-batch-%d.prepared.json", batch))
}

// Validates the immutable identity and calldata hash of recovered preparation.
func validateFleetRefreshPrepared(prepared *fleetRefreshPreparedEvidence, cfg *ResolvedConfig, plan *SetupPlan, action Action, batch, firstFleet, lastFleet int) ([]byte, error) {
	if prepared == nil || cfg == nil || plan == nil {
		return nil, errors.New("fleet refresh prepared evidence is unavailable")
	}
	if prepared.Schema != fleetRefreshPreparedSchema || prepared.DeploymentID != cfg.Config.Deployment.DeploymentID ||
		prepared.PlanHash != plan.PlanHash || prepared.ActionID != action.ID || prepared.IntentHash != action.IntentHash ||
		prepared.Batch != batch || prepared.FirstFleet != firstFleet || prepared.LastFleet != lastFleet || prepared.Generation != 2 ||
		prepared.EffectiveEpoch == 0 || prepared.ValidToEpoch < prepared.EffectiveEpoch || len(prepared.Fleets) != lastFleet-firstFleet+1 {
		return nil, errors.New("fleet refresh prepared evidence identity mismatch")
	}
	data, err := decodeEvidenceHex(prepared.Calldata)
	if err != nil || len(data) < 4 || !strings.EqualFold(prepared.CalldataHash, crypto.Keccak256Hash(data).Hex()) {
		return nil, errors.New("fleet refresh prepared calldata is invalid")
	}
	return data, nil
}

// Reads a variable-length canonical 0x byte string.
func decodeEvidenceHex(value string) ([]byte, error) {
	if !strings.HasPrefix(value, "0x") || value != "0x"+strings.ToLower(strings.TrimPrefix(value, "0x")) {
		return nil, errors.New("evidence hex is not canonical lowercase 0x")
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

// Recovers the exact epoch-specific calldata if a transaction was persisted;
// otherwise creates and durably records a fresh safe-window preparation.
func (self *Executor) fleetRefreshCalldata(ctx context.Context, action Action, batch, firstFleet, lastFleet int) (*fleetRefreshPreparedEvidence, []byte, error) {
	path := fleetRefreshPreparedPath(self.stateDir, batch)
	if transaction, persisted := self.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash); persisted {
		var prepared fleetRefreshPreparedEvidence
		if err := readJSONFile(path, &prepared); err != nil {
			return nil, nil, fmt.Errorf("recover persisted fleet refresh preparation: %w", err)
		}
		data, err := validateFleetRefreshPrepared(&prepared, self.cfg, self.plan, action, batch, firstFleet, lastFleet)
		if err != nil {
			return nil, nil, err
		}
		raw, err := os.ReadFile(filepath.Join(self.stateDir, "transactions", stringsTrim0x(transaction.TransactionHash)+".rlp"))
		if err != nil {
			return nil, nil, err
		}
		var exact ethTypes.Transaction
		if err := exact.UnmarshalBinary(raw); err != nil {
			return nil, nil, err
		}
		if exact.Hash().Hex() != transaction.TransactionHash || exact.To() == nil || *exact.To() != self.payloads.FleetBatcherAddress || string(exact.Data()) != string(data) {
			return nil, nil, errors.New("persisted fleet refresh transaction differs from prepared calldata")
		}
		return &prepared, data, nil
	}
	coordinator := stabi.NewSTCoordinator()
	window, err := waitFutureEpochTransactionWindow(ctx, self.oracle, self.payloads.Manifest.CoordinatorProxy, coordinator)
	if err != nil {
		return nil, nil, err
	}
	prepared, err := self.prepareFleetRefreshBatch(ctx, action, batch, firstFleet, lastFleet, window)
	if err != nil {
		return nil, nil, err
	}
	data, err := validateFleetRefreshPrepared(prepared, self.cfg, self.plan, action, batch, firstFleet, lastFleet)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.MarshalIndent(prepared, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	if err := atomicWrite(path, append(encoded, '\n'), 0o600); err != nil {
		return nil, nil, err
	}
	return prepared, data, nil
}

// Reconstructs a signed binding and revoke from public/prepared evidence.
func fleetRefreshEvidenceBindings(evidence FleetRefreshBindingEvidence, coordinatorAddress common.Address, chainID uint64, netuid uint16) (protocol.FleetBinding, protocol.FleetRevoke, error) {
	if evidence.Schema != fleetRefreshBindingEvidenceSchema || evidence.PriorGeneration == 0 || evidence.ReplacementGeneration != evidence.PriorGeneration+1 {
		return protocol.FleetBinding{}, protocol.FleetRevoke{}, errors.New("fleet refresh member evidence generation is invalid")
	}
	read := func(label, value string, size int) ([]byte, error) {
		decoded, ok := evidenceFixedHex(value, size)
		if !ok {
			return nil, fmt.Errorf("%s is invalid", label)
		}
		return decoded, nil
	}
	clientIDBytes, err := read("client id", evidence.ClientID, 16)
	if err != nil {
		return protocol.FleetBinding{}, protocol.FleetRevoke{}, err
	}
	clientKeyBytes, err := read("client key", evidence.ClientKey, 32)
	if err != nil {
		return protocol.FleetBinding{}, protocol.FleetRevoke{}, err
	}
	fleetIDBytes, err := read("fleet id", evidence.FleetID, 32)
	if err != nil {
		return protocol.FleetBinding{}, protocol.FleetRevoke{}, err
	}
	hotkeyBytes, err := read("hotkey", evidence.Hotkey, 32)
	if err != nil {
		return protocol.FleetBinding{}, protocol.FleetRevoke{}, err
	}
	commitmentBytes, err := read("commitment", evidence.CommitmentHash, 32)
	if err != nil {
		return protocol.FleetBinding{}, protocol.FleetRevoke{}, err
	}
	var coordinator [20]byte
	copy(coordinator[:], coordinatorAddress[:])
	binding := protocol.FleetBinding{
		ChainID: chainID, Netuid: netuid, Coordinator: coordinator,
		Generation: evidence.ReplacementGeneration, ValidFromEpoch: evidence.ValidFromEpoch, ValidToEpoch: evidence.ValidToEpoch,
	}
	copy(binding.ClientID[:], clientIDBytes)
	copy(binding.ClientKey[:], clientKeyBytes)
	copy(binding.FleetID[:], fleetIDBytes)
	copy(binding.Hotkey[:], hotkeyBytes)
	copy(binding.CommitmentHash[:], commitmentBytes)
	revoke := protocol.FleetRevoke{
		ChainID: chainID, Netuid: netuid, Coordinator: coordinator, ClientID: binding.ClientID,
		Generation: evidence.PriorGeneration, EffectiveEpoch: evidence.ValidFromEpoch,
	}
	return binding, revoke, nil
}

// Requires prepared replacement evidence to name the exact deterministic
// manifest member and generation, not merely a separately valid signature.
func fleetRefreshReplacementMatchesManifest(binding protocol.FleetBinding, manifest protocol.FleetManifest, memberIndex int, commitmentHash [32]byte, effectiveEpoch, validToEpoch uint64) bool {
	if memberIndex < 1 || memberIndex > len(manifest.Members) || manifest.Generation != 2 {
		return false
	}
	member := manifest.Members[memberIndex-1]
	return binding.ChainID == manifest.ChainID && binding.Netuid == manifest.Netuid && binding.Coordinator == manifest.Coordinator &&
		binding.FleetID == manifest.FleetID && binding.Hotkey == manifest.Hotkey && binding.ClientID == member.ClientID && binding.ClientKey == member.ClientKey &&
		binding.Generation == manifest.Generation && binding.ValidFromEpoch == effectiveEpoch && binding.ValidToEpoch == validToEpoch && binding.CommitmentHash == commitmentHash
}

// Verifies the transaction's exact mirror and both binding generations at one
// canonical EVM block. It is shared by execution recovery and postconditions.
func (self *Executor) verifyFleetRefreshPreparedAt(ctx context.Context, prepared *fleetRefreshPreparedEvidence, block uint64) (int, error) {
	if self == nil || self.cfg == nil || self.payloads == nil || prepared == nil || prepared.EffectiveEpoch == 0 || block == 0 {
		return 0, errors.New("fleet refresh verification context is unavailable")
	}
	coordinator := stabi.NewSTCoordinator()
	coordinatorAddress := self.payloads.Manifest.CoordinatorProxy
	verificationFleets := make([]fleetRefreshVerificationFleet, 0, len(prepared.Fleets))
	for fleetOffset, fleet := range prepared.Fleets {
		wantFleet := prepared.FirstFleet + fleetOffset
		if fleet.Fleet != wantFleet || len(fleet.Members) != self.cfg.Config.Topology.ClientsPerHeadFleet {
			return 0, errors.New("fleet refresh prepared fleet/member ordering mismatch")
		}
		manifest, nativeCommitmentHash, nativeEvidence, nativeFinalizedBlockHash, err := self.validatedFleetCommitmentGeneration(fleet.Fleet, 2)
		if err != nil {
			return 0, err
		}
		hotkey, err := decodeHex32("fleet refresh hotkey", fleet.Hotkey)
		if err != nil {
			return 0, err
		}
		commitmentHash, err := decodeHex32("fleet refresh commitment", fleet.CommitmentHash)
		if err != nil {
			return 0, err
		}
		finalizedBlockHash, err := decodeHex32("fleet refresh finalized block", fleet.FinalizedBlockHash)
		if err != nil {
			return 0, err
		}
		if hotkey != manifest.Hotkey || commitmentHash != nativeCommitmentHash || fleet.FinalizedBlock != nativeEvidence.FinalizedBlock || finalizedBlockHash != nativeFinalizedBlockHash || fleet.ManifestURI != nativeEvidence.ManifestURI || fleet.CommitmentEvidenceURI != fmt.Sprintf("fleet-%d.refresh.commitment.json", fleet.Fleet) {
			return 0, fmt.Errorf("fleet %d prepared native evidence mismatch", fleet.Fleet)
		}
		verificationFleet := fleetRefreshVerificationFleet{
			Fleet: fleet.Fleet, Hotkey: hotkey, CommitmentHash: commitmentHash,
			FinalizedBlock: fleet.FinalizedBlock, FinalizedBlockHash: finalizedBlockHash,
			Members: make([]fleetRefreshVerificationMember, 0, len(fleet.Members)),
		}
		var fleetID [32]byte
		for memberOffset, evidence := range fleet.Members {
			if evidence.Fleet != fleet.Fleet || evidence.Member != memberOffset+1 || evidence.ValidFromEpoch != prepared.EffectiveEpoch || evidence.ValidToEpoch != prepared.ValidToEpoch {
				return 0, errors.New("fleet refresh member evidence ordering/epoch mismatch")
			}
			binding, revoke, err := fleetRefreshEvidenceBindings(evidence, coordinatorAddress, testnetChainID, self.cfg.Netuid)
			if err != nil {
				return 0, err
			}
			if !fleetRefreshReplacementMatchesManifest(binding, manifest, evidence.Member, nativeCommitmentHash, prepared.EffectiveEpoch, prepared.ValidToEpoch) {
				return 0, fmt.Errorf("fleet %d member %d replacement differs from its manifest", fleet.Fleet, evidence.Member)
			}
			if memberOffset == 0 {
				fleetID = binding.FleetID
			} else if binding.FleetID != fleetID {
				return 0, errors.New("fleet refresh members cross fleet ids")
			}
			bindingDigest, err := binding.Digest()
			if err != nil || !strings.EqualFold(evidence.BindingDigest, "0x"+hex.EncodeToString(bindingDigest[:])) {
				return 0, stateMismatchError(err, "fleet %d member %d binding digest mismatch", fleet.Fleet, evidence.Member)
			}
			revokeDigest, err := revoke.Digest()
			if err != nil || !strings.EqualFold(evidence.RevokeDigest, "0x"+hex.EncodeToString(revokeDigest[:])) {
				return 0, stateMismatchError(err, "fleet %d member %d revoke digest mismatch", fleet.Fleet, evidence.Member)
			}
			clientSignature, err := decodeEvidenceHex(evidence.ClientSignature)
			if err != nil || !binding.VerifyClient(clientSignature) {
				return 0, stateMismatchError(err, "fleet %d member %d client signature mismatch", fleet.Fleet, evidence.Member)
			}
			hotkeySignature, err := decodeEvidenceHex(evidence.HotkeySignature)
			if err != nil || !binding.VerifyHotkey(hotkeySignature) {
				return 0, stateMismatchError(err, "fleet %d member %d hotkey signature mismatch", fleet.Fleet, evidence.Member)
			}
			revokeSignature, err := decodeEvidenceHex(evidence.RevokeSignature)
			if err != nil || !revoke.VerifyClient(ed25519.PublicKey(binding.ClientKey[:]), revokeSignature) {
				return 0, stateMismatchError(err, "fleet %d member %d revoke signature mismatch", fleet.Fleet, evidence.Member)
			}
			priorBinding := protocol.FleetBinding{
				ChainID: testnetChainID, Netuid: self.cfg.Netuid, Coordinator: binding.Coordinator,
				FleetID: binding.FleetID, Hotkey: binding.Hotkey, ClientID: binding.ClientID, ClientKey: binding.ClientKey,
				Generation: evidence.PriorGeneration, ValidFromEpoch: evidence.PriorValidFromEpoch,
				ValidToEpoch: evidence.PriorOriginalValidToEpoch,
			}
			priorCommitment, err := decodeHex32("prior commitment", evidence.PriorCommitmentHash)
			if err != nil {
				return 0, err
			}
			priorBinding.CommitmentHash = priorCommitment
			verificationFleet.Members = append(verificationFleet.Members, fleetRefreshVerificationMember{
				Evidence: evidence, Binding: binding, PriorBinding: priorBinding,
			})
		}
		verificationFleet.FleetID = fleetID
		verificationFleets = append(verificationFleets, verificationFleet)
	}
	return verifyFleetRefreshStateAt(ctx, self.oracle, coordinatorAddress, coordinator, verificationFleets, block)
}

// Requires one exact FleetRefreshed event per prepared fleet.
func verifyFleetRefreshEvents(receipt *ethTypes.Receipt, parsed abi.ABI, batcherAddress common.Address, prepared *fleetRefreshPreparedEvidence) error {
	if receipt == nil {
		return errors.New("fleet refresh receipt is unavailable")
	}
	event, ok := parsed.Events["FleetRefreshed"]
	if !ok {
		return errors.New("fleet refresh ABI has no FleetRefreshed event")
	}
	matched := 0
	for _, log := range receipt.Logs {
		if log.Address != batcherAddress || len(log.Topics) != 3 || log.Topics[0] != event.ID {
			continue
		}
		if matched >= len(prepared.Fleets) {
			return errors.New("fleet refresh receipt has excess events")
		}
		fleet := prepared.Fleets[matched]
		hotkey, err := decodeHex32("fleet refresh event hotkey", fleet.Hotkey)
		if err != nil {
			return err
		}
		commitmentHash, err := decodeHex32("fleet refresh event commitment", fleet.CommitmentHash)
		if err != nil {
			return err
		}
		values, err := event.Inputs.NonIndexed().Unpack(log.Data)
		if err != nil || len(values) != 1 {
			return stateMismatchError(err, "fleet refresh event data is invalid")
		}
		memberCount, ok := values[0].(*big.Int)
		if !ok || !memberCount.IsUint64() || memberCount.Uint64() != uint64(len(fleet.Members)) || log.Topics[1] != common.BytesToHash(hotkey[:]) || log.Topics[2] != common.BytesToHash(commitmentHash[:]) {
			return errors.New("fleet refresh event differs from prepared batch")
		}
		matched++
	}
	if matched != len(prepared.Fleets) {
		return fmt.Errorf("fleet refresh receipt has %d events, want %d", matched, len(prepared.Fleets))
	}
	return nil
}

// Writes member evidence first and the batch evidence last, making the latter
// the durable completeness marker after a crash.
func (self *Executor) publishFleetRefreshEvidence(prepared *fleetRefreshPreparedEvidence, receipt *ethTypes.Receipt) error {
	memberPaths := []string{}
	memberCount := 0
	for fleetIndex := range prepared.Fleets {
		for memberIndex := range prepared.Fleets[fleetIndex].Members {
			evidence := prepared.Fleets[fleetIndex].Members[memberIndex]
			evidence.TransactionHash = receipt.TxHash.Hex()
			evidence.BlockNumber = receipt.BlockNumber.Uint64()
			evidence.BlockHash = receipt.BlockHash.Hex()
			name := fmt.Sprintf("fleet-%d-member-%d.refresh.binding.json", evidence.Fleet, evidence.Member)
			if err := writePublicJSON(filepath.Join(self.stateDir, "public", name), evidence); err != nil {
				return err
			}
			memberPaths = append(memberPaths, name)
			memberCount++
		}
	}
	evidence := FleetRefreshBatchEvidence{
		Schema: fleetRefreshBatchEvidenceSchema, Batch: prepared.Batch,
		FirstFleet: prepared.FirstFleet, LastFleet: prepared.LastFleet, Generation: prepared.Generation,
		EffectiveEpoch: prepared.EffectiveEpoch, ValidToEpoch: prepared.ValidToEpoch,
		CalldataHash: prepared.CalldataHash, FleetCount: len(prepared.Fleets), MemberCount: memberCount,
		MemberEvidence: memberPaths, TransactionHash: receipt.TxHash.Hex(), BlockNumber: receipt.BlockNumber.Uint64(), BlockHash: receipt.BlockHash.Hex(),
	}
	return writePublicJSON(filepath.Join(self.stateDir, "public", fmt.Sprintf("fleet-refresh-batch-%d.json", prepared.Batch)), evidence)
}

// Executes one atomic batch or resumes its exact persisted transaction.
func (self *Executor) refreshFleetBatch(ctx context.Context, action Action, batch int) error {
	if err := self.ensurePayloads(ctx); err != nil {
		return err
	}
	firstFleet, lastFleet, err := fleetRefreshActionRange(self.cfg, action, batch)
	if err != nil {
		return err
	}
	if !common.IsHexAddress(action.Target) || common.HexToAddress(action.Target) != self.payloads.FleetBatcherAddress {
		return errors.New("fleet refresh action target differs from the deployed batcher")
	}
	prepared, data, err := self.fleetRefreshCalldata(ctx, action, batch, firstFleet, lastFleet)
	if err != nil {
		return err
	}
	batcherAddress := self.payloads.FleetBatcherAddress
	receipt, sendErr := self.oracle.Send(ctx, self.plan.PlanHash, action, &batcherAddress, big.NewInt(0), data)
	if sendErr != nil {
		_, persisted := self.oracle.journal.LatestTransaction(self.plan.PlanHash, action.ID, action.IntentHash)
		latest, _, readErr := readFutureEpochTransactionWindow(ctx, self.oracle, self.payloads.Manifest.CoordinatorProxy, stabi.NewSTCoordinator())
		if readErr == nil && retryFleetBindingAfterEpochTransition(latest.CurrentEpoch, prepared.EffectiveEpoch, persisted) {
			return self.refreshFleetBatch(ctx, action, batch)
		}
		return sendErr
	}
	parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
	if err != nil {
		return err
	}
	if err := verifyFleetRefreshEvents(receipt, parsed, batcherAddress, prepared); err != nil {
		return err
	}
	if _, err := self.verifyFleetRefreshPreparedAt(ctx, prepared, receipt.BlockNumber.Uint64()); err != nil {
		return err
	}
	return self.publishFleetRefreshEvidence(prepared, receipt)
}

// Proves both a pending schedule and an activated handoff at the exact
// postcondition checkpoint supplied by the action verifier.
func (self *Executor) verifyFleetRefreshOraclePostState(ctx context.Context, action Action, evmHead ChainHead, state map[string]any) (map[string]any, error) {
	if err := self.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	target, err := fleetRefreshOracleTarget(action, self.payloads)
	if err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	observed, err := readFleetRefreshOracleStateAt(ctx, self.owner, self.payloads.Manifest.CoordinatorProxy, coordinator, evmHead.Number)
	if err != nil {
		return nil, err
	}
	if observed.Immutable != self.payloads.CommitmentOracle {
		return nil, errors.New("fleet refresh immutable oracle postcondition mismatch")
	}
	await := strings.Contains(action.ID, "await")
	if await && observed.Active != target {
		return nil, errors.New("fleet refresh awaited oracle is not active")
	}
	if !await && observed.Active != target && (observed.Pending != target || observed.PendingEpoch <= observed.CurrentEpoch) {
		return nil, errors.New("fleet refresh oracle is neither active nor safely pending")
	}
	state["current_epoch"] = observed.CurrentEpoch
	state["immutable_oracle"] = observed.Immutable.Hex()
	state["active_oracle"] = observed.Active.Hex()
	state["pending_oracle"] = observed.Pending.Hex()
	state["pending_epoch"] = observed.PendingEpoch
	state["target_oracle"] = target.Hex()
	return state, nil
}

// Proves the generation-2 manifest and exact canonical finalized native write.
func (self *Executor) verifyFleetRefreshCommitmentPostState(action Action, fleetIndex int, state map[string]any) (map[string]any, error) {
	manifest, commitmentHash, evidence, _, err := self.validatedFleetCommitmentGeneration(fleetIndex, 2)
	if err != nil {
		return nil, err
	}
	if err := validateFleetCommitmentRecoveryEvidence(action, evidence); err != nil {
		return nil, err
	}
	if manifest.Generation != 2 {
		return nil, errors.New("fleet refresh manifest generation mismatch")
	}
	state["fleet"] = fleetIndex
	state["generation"] = manifest.Generation
	state["commitment_hash"] = "0x" + hex.EncodeToString(commitmentHash[:])
	state["commitment_block"] = evidence.CommitmentBlock
	state["manifest_uri"] = evidence.ManifestURI
	return state, nil
}

// Reloads only complete public evidence and rechecks every signed/on-chain
// member field at the postcondition's canonical finalized head.
func (self *Executor) verifyFleetRefreshBatchPostState(ctx context.Context, action Action, evmHead ChainHead, state map[string]any) (map[string]any, error) {
	if err := self.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	batch := suffixInt(action.ID)
	firstFleet, lastFleet, err := fleetRefreshActionRange(self.cfg, action, batch)
	if err != nil {
		return nil, err
	}
	batcherAddress, err := fleetBatcherAddressForAction(action)
	if err != nil {
		return nil, err
	}
	var public FleetRefreshBatchEvidence
	if err := readJSONFile(filepath.Join(self.stateDir, "public", fmt.Sprintf("fleet-refresh-batch-%d.json", batch)), &public); err != nil {
		return nil, err
	}
	var prepared fleetRefreshPreparedEvidence
	if err := readJSONFile(fleetRefreshPreparedPath(self.stateDir, batch), &prepared); err != nil {
		return nil, err
	}
	data, err := validateFleetRefreshPrepared(&prepared, self.cfg, self.plan, action, batch, firstFleet, lastFleet)
	if err != nil {
		return nil, err
	}
	if public.Schema != fleetRefreshBatchEvidenceSchema || public.Batch != batch || public.FirstFleet != firstFleet || public.LastFleet != lastFleet ||
		public.Generation != 2 || public.EffectiveEpoch != prepared.EffectiveEpoch || public.ValidToEpoch != prepared.ValidToEpoch ||
		public.CalldataHash != prepared.CalldataHash || public.FleetCount != len(prepared.Fleets) || public.BlockNumber == 0 || public.BlockNumber > evmHead.Number ||
		public.TransactionHash == "" || public.BlockHash == "" {
		return nil, errors.New("fleet refresh public batch evidence identity mismatch")
	}
	if _, err := decodeHex32("fleet refresh transaction hash", public.TransactionHash); err != nil {
		return nil, err
	}
	if _, err := decodeHex32("fleet refresh block hash", public.BlockHash); err != nil {
		return nil, err
	}
	receipt, err := self.oracle.client.TransactionReceipt(ctx, common.HexToHash(public.TransactionHash))
	if err != nil || receipt.Status != ethTypes.ReceiptStatusSuccessful || receipt.BlockNumber == nil || receipt.BlockNumber.Uint64() != public.BlockNumber || !strings.EqualFold(receipt.BlockHash.Hex(), public.BlockHash) {
		return nil, stateMismatchError(err, "fleet refresh public receipt mismatch")
	}
	canonicalHash, err := canonicalEVMBlockHash(ctx, ethEVMBlockReader{client: self.oracle.client}, public.BlockNumber)
	if err != nil || !strings.EqualFold(canonicalHash, public.BlockHash) {
		return nil, stateMismatchError(err, "fleet refresh public receipt is not canonical")
	}
	parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
	if err != nil {
		return nil, err
	}
	if err := verifyFleetRefreshEvents(receipt, parsed, batcherAddress, &prepared); err != nil {
		return nil, err
	}
	members, err := self.verifyFleetRefreshPreparedAt(ctx, &prepared, evmHead.Number)
	if err != nil {
		return nil, err
	}
	if members != public.MemberCount || len(public.MemberEvidence) != members {
		return nil, errors.New("fleet refresh public member count mismatch")
	}
	evidenceIndex := 0
	for _, relativePath := range public.MemberEvidence {
		if filepath.Base(relativePath) != relativePath || strings.ContainsAny(relativePath, `/\\`) {
			return nil, errors.New("fleet refresh public member path is unsafe")
		}
		var evidence FleetRefreshBindingEvidence
		if err := readJSONFile(filepath.Join(self.stateDir, "public", relativePath), &evidence); err != nil {
			return nil, err
		}
		if evidence.TransactionHash != public.TransactionHash || evidence.BlockNumber != public.BlockNumber || !strings.EqualFold(evidence.BlockHash, public.BlockHash) {
			return nil, errors.New("fleet refresh member receipt differs from its batch")
		}
		fleetOffset := evidenceIndex / self.cfg.Config.Topology.ClientsPerHeadFleet
		memberOffset := evidenceIndex % self.cfg.Config.Topology.ClientsPerHeadFleet
		want := prepared.Fleets[fleetOffset].Members[memberOffset]
		want.TransactionHash = public.TransactionHash
		want.BlockNumber = public.BlockNumber
		want.BlockHash = public.BlockHash
		if !reflect.DeepEqual(evidence, want) {
			return nil, errors.New("fleet refresh public member evidence differs from prepared transaction")
		}
		evidenceIndex++
	}
	state["batch"] = batch
	state["first_fleet"] = firstFleet
	state["last_fleet"] = lastFleet
	state["generation"] = 2
	state["effective_epoch"] = prepared.EffectiveEpoch
	state["valid_to_epoch"] = prepared.ValidToEpoch
	state["fleets"] = len(prepared.Fleets)
	state["members"] = members
	state["transaction_hash"] = public.TransactionHash
	state["calldata_hash"] = crypto.Keccak256Hash(data).Hex()
	return state, nil
}

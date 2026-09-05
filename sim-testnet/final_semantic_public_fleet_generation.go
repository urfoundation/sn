package main

// final_semantic_public_fleet_generation.go replays the complete ordinary
// setup lineage at the exact historical heads retained by FINAL evidence.
// Terminal fleet state is insufficient: this unit proves every carried
// generation-one write and every generation-two atomic replacement payload.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Combines the two narrow public surfaces needed for historical ordinary-fleet
// replay. An implementation cannot accidentally satisfy only the generic
// receipt API while omitting native commitments or exact transaction inputs.
type finalFleetGenerationChainReader interface {
	FinalSemanticFleetGenerationChainReader
	NativeFleetCommitment(context.Context, uint16, string, ChainHead) (FinalNativeFleetCommitmentState, []FinalRPCExchange, error)
}

var finalFleetGenerationEventABIs = struct {
	once        sync.Once
	coordinator abi.ABI
	batcher     abi.ABI
	err         error
}{}

// Runs a bounded, fixed-order historical replay after the shared coordinator
// runtime census has authenticated every referenced EVM head. The source
// evidence remains the canonical topology; RPC calls only prove that each
// retained native commitment and EVM write occurred at its sealed head.
func verifyFinalSemanticFleetGenerationOnChain(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if evidence == nil || reader == nil || appendExchanges == nil {
		return errors.New("public ordinary fleet generation replay is unavailable")
	}
	if evidence.FleetGeneration == nil {
		return nil
	}
	if err := verifyFinalFleetGenerationLineage(evidence, evidence.FleetGeneration); err != nil {
		return err
	}
	fleetReader, ok := reader.(finalFleetGenerationChainReader)
	if !ok {
		return errors.New("public semantic reader does not expose fleet generation history")
	}
	runtimeReader, ok := reader.(finalFleetGenerationRuntimeReader)
	if !ok {
		return errors.New("public semantic reader does not expose fleet generation runtimes")
	}
	for _, fleet := range evidence.FleetGeneration.SetupFleets {
		for _, version := range []FinalFleetGenerationVersionEvidence{fleet.Initial, fleet.Refresh} {
			if err := verifyFinalFleetGenerationNativeCommitment(ctx, evidence, fleetReader, version, appendExchanges); err != nil {
				return fmt.Errorf("setup fleet %d generation %d: %w", fleet.FleetID, version.Generation, err)
			}
		}
	}
	for _, challenger := range evidence.FleetGeneration.ChallengerFleets {
		if err := verifyFinalFleetGenerationNativeCommitment(ctx, evidence, fleetReader, challenger.Initial, appendExchanges); err != nil {
			return fmt.Errorf("challenger fleet %d generation one: %w", challenger.FleetID, err)
		}
	}
	for _, batch := range evidence.FleetGeneration.Batches {
		for _, write := range batch.CarriedHistory {
			if err := verifyFinalFleetGenerationEVMWrite(ctx, evidence, fleetReader, write, appendExchanges); err != nil {
				return fmt.Errorf("carried batch %d action %s: %w", batch.Batch, write.Action.ActionID, err)
			}
			if err := verifyFinalFleetGenerationRuntime(ctx, evidence, runtimeReader, write, appendExchanges); err != nil {
				return fmt.Errorf("carried batch %d action %s: %w", batch.Batch, write.Action.ActionID, err)
			}
		}
		if batch.BatchWrite == nil {
			if batch.Generation == 1 {
				continue
			}
			return fmt.Errorf("refresh batch %d has no write", batch.Batch)
		}
		if err := verifyFinalFleetGenerationEVMWrite(ctx, evidence, fleetReader, *batch.BatchWrite, appendExchanges); err != nil {
			return fmt.Errorf("fleet generation batch %d/%d: %w", batch.Generation, batch.Batch, err)
		}
		if err := verifyFinalFleetGenerationRuntime(ctx, evidence, runtimeReader, *batch.BatchWrite, appendExchanges); err != nil {
			return fmt.Errorf("fleet generation batch %d/%d: %w", batch.Generation, batch.Batch, err)
		}
	}
	if err := verifyFinalFleetGenerationEventTopology(evidence, evidence.FleetGeneration); err != nil {
		return fmt.Errorf("ordinary fleet generation event topology: %w", err)
	}
	return nil
}

// Confirms the native commitment storage record at its exact finalized head.
// The retained postcondition separately binds the approved action, intent,
// and extrinsic; this query proves the signed commitment is not a detached
// artifact substituted after that mutation.
func verifyFinalFleetGenerationNativeCommitment(ctx context.Context, evidence *FinalSemanticEvidence, reader finalFleetGenerationChainReader, version FinalFleetGenerationVersionEvidence, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	state, exchanges, err := reader.NativeFleetCommitment(ctx, evidence.Netuid, version.Hotkey, version.NativeHead)
	if err != nil {
		return err
	}
	if err := appendExchanges("substrate", version.NativeHead, exchanges); err != nil {
		return err
	}
	if !strings.EqualFold(state.Hotkey, version.Hotkey) || !strings.EqualFold(state.CommitmentHash, version.CommitmentHash) || state.CommitmentBlock != version.NativeHead.Number || state.Block != version.NativeHead {
		return errors.New("native commitment state differs from sealed generation")
	}
	return nil
}

// Replays one exact historical EVM write, including its input bytes and every
// release-owned receipt log. It rejects partial log matching: a relevant
// additional event is a semantic mutation, not ignorable receipt noise.
func verifyFinalFleetGenerationEVMWrite(ctx context.Context, evidence *FinalSemanticEvidence, reader finalFleetGenerationChainReader, write FinalFleetGenerationWriteEvidence, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	state, exchanges, err := reader.FleetGenerationEVMWrite(ctx, write)
	if err != nil {
		return err
	}
	if err := appendExchanges("evm", write.Receipt.Block, exchanges); err != nil {
		return err
	}
	target, err := finalFleetGenerationWriteTarget(evidence, write)
	if err != nil {
		return err
	}
	if !strings.EqualFold(state.TransactionHash, write.Receipt.TransactionHash) || state.Block != write.Receipt.Block || state.Status != write.Receipt.Status || !strings.EqualFold(state.To, target) || state.Calldata != write.Calldata || len(state.Logs) != len(write.Events) {
		return errors.New("historical fleet write receipt or calldata differs from sealed evidence")
	}
	for index, event := range write.Events {
		if !finalSemanticCanonicalLogEqual(state.Logs[index], event.Log) {
			return fmt.Errorf("historical fleet write log %d differs from sealed evidence", index)
		}
		if err := verifyFinalFleetGenerationWriteEvent(evidence, write, event); err != nil {
			return fmt.Errorf("historical fleet write log %d: %w", index, err)
		}
	}
	return nil
}

// Narrows a receipt replay to the exact contracts that could have emitted the
// sealed write. This intentionally does not reuse the terminal release census:
// generation writes can predate a batcher replacement, and accepting every
// historical or terminal contract would make the receipt filter broader than
// the approved write-time mutation graph.
func finalFleetGenerationWriteReleaseContractAddresses(write FinalFleetGenerationWriteEvidence) (map[common.Address]bool, error) {
	if !common.IsHexAddress(write.CoordinatorProxy) || common.HexToAddress(write.CoordinatorProxy) == (common.Address{}) {
		return nil, errors.New("ordinary fleet generation write has an invalid coordinator emitter")
	}
	allowed := map[common.Address]bool{common.HexToAddress(write.CoordinatorProxy): true}
	switch {
	case strings.HasPrefix(write.Action.ActionID, "fleet.mirror."), strings.HasPrefix(write.Action.ActionID, "fleet.bind."):
		if write.BatcherAddress != "" || write.BatcherRuntimeHash != "" {
			return nil, errors.New("ordinary fleet generation carried write names a batcher emitter")
		}
	case strings.HasPrefix(write.Action.ActionID, "fleet.install.batch."), strings.HasPrefix(write.Action.ActionID, "fleet.refresh.batch."):
		if !common.IsHexAddress(write.BatcherAddress) || common.HexToAddress(write.BatcherAddress) == (common.Address{}) || write.BatcherRuntimeHash == "" {
			return nil, errors.New("ordinary fleet generation batch write has an invalid batcher emitter")
		}
		allowed[common.HexToAddress(write.BatcherAddress)] = true
	default:
		return nil, fmt.Errorf("ordinary fleet generation action %q has no approved emitter graph", write.Action.ActionID)
	}
	return allowed, nil
}

// Resolves the only contract target allowed for each retained write class.
// A batcher target comes from its sealed write-time plan/runtime projection,
// not a mutable observer field or a later terminal release helper.
func finalFleetGenerationWriteTarget(evidence *FinalSemanticEvidence, write FinalFleetGenerationWriteEvidence) (string, error) {
	if evidence == nil {
		return "", errors.New("ordinary fleet generation target is unavailable")
	}
	switch {
	case strings.HasPrefix(write.Action.ActionID, "fleet.mirror."), strings.HasPrefix(write.Action.ActionID, "fleet.bind."):
		if !common.IsHexAddress(write.CoordinatorProxy) || common.HexToAddress(write.CoordinatorProxy) == (common.Address{}) {
			return "", errors.New("ordinary fleet generation historical coordinator target is unavailable")
		}
		return strings.ToLower(common.HexToAddress(write.CoordinatorProxy).Hex()), nil
	case strings.HasPrefix(write.Action.ActionID, "fleet.install.batch."), strings.HasPrefix(write.Action.ActionID, "fleet.refresh.batch."):
		if !common.IsHexAddress(write.BatcherAddress) || common.HexToAddress(write.BatcherAddress) == (common.Address{}) {
			return "", errors.New("ordinary fleet generation batcher target is unavailable")
		}
		return strings.ToLower(common.HexToAddress(write.BatcherAddress).Hex()), nil
	default:
		return "", fmt.Errorf("ordinary fleet generation action %q has no EVM target", write.Action.ActionID)
	}
}

// Parses the two immutable contract ABIs once. Event decoding is intentionally
// separate from generic economic receipt decoding because FleetBound and
// FleetRefreshed are lineage facts rather than operator-pool settlements.
func finalFleetGenerationABIs() (abi.ABI, abi.ABI, error) {
	finalFleetGenerationEventABIs.once.Do(func() {
		finalFleetGenerationEventABIs.coordinator, finalFleetGenerationEventABIs.err = abi.JSON(strings.NewReader(CoordinatorABI))
		if finalFleetGenerationEventABIs.err != nil {
			return
		}
		finalFleetGenerationEventABIs.batcher, finalFleetGenerationEventABIs.err = abi.JSON(strings.NewReader(FleetBatcherABI))
	})
	return finalFleetGenerationEventABIs.coordinator, finalFleetGenerationEventABIs.batcher, finalFleetGenerationEventABIs.err
}

// Requires the stored semantic projection to be byte-for-byte and
// field-for-field identical to a fresh ABI decode of the retained log. This
// catches a topic or argument substitution even when a receipt hash remains
// self-consistent with maliciously rewritten evidence.
func verifyFinalFleetGenerationWriteEvent(evidence *FinalSemanticEvidence, write FinalFleetGenerationWriteEvidence, value FinalFleetGenerationEventEvidence) error {
	decoded, err := finalFleetGenerationDecodeEventForBatcher(evidence, write.Action.ActionID, write.BatcherAddress, value.Log)
	if err != nil {
		return err
	}
	if !finalJSONEqual(decoded.Evidence, value) {
		return errors.New("ordinary fleet generation event fields differ from the retained ABI log")
	}
	return nil
}

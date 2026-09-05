package main

// final_semantic_fleet_generation_runtime.go authenticates the proxy dispatch
// identity at every ordinary-fleet write head. It deliberately does not use
// the terminal release-root census for carried writes, because predecessor
// transactions predate the current batcher and coordinator implementation.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Adds the exact head-local runtime read needed beside transaction and log
// replay. Keeping it narrow prevents older readers from silently treating a
// generic receipt lookup as a proof of proxy dispatch.
type finalFleetGenerationRuntimeReader interface {
	FleetGenerationRuntime(context.Context, FinalFleetGenerationWriteEvidence) (FinalFleetGenerationRuntimeState, []FinalRPCExchange, error)
}

// Reads and compares the proxy slot plus executable code hashes at the write's
// sealed EVM head. Current refreshes require the terminal release roots;
// historical install writes instead bind the exact batcher address and code
// from their archived approving plan. Carried writes have no batcher fields.
func verifyFinalFleetGenerationRuntime(ctx context.Context, evidence *FinalSemanticEvidence, reader finalFleetGenerationRuntimeReader, write FinalFleetGenerationWriteEvidence, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if evidence == nil || reader == nil || appendExchanges == nil {
		return errors.New("ordinary fleet generation runtime reader is unavailable")
	}
	state, exchanges, err := reader.FleetGenerationRuntime(ctx, write)
	if err != nil {
		return err
	}
	if err := appendExchanges("evm", write.EVMHead, exchanges); err != nil {
		return err
	}
	if state.Block != write.EVMHead || !strings.EqualFold(state.CoordinatorProxy, write.CoordinatorProxy) || !strings.EqualFold(state.CoordinatorImplementation, write.CoordinatorImplementation) || !strings.EqualFold(state.CoordinatorImplementationSlot, write.CoordinatorImplementationSlot) || !strings.EqualFold(state.CoordinatorProxyRuntimeHash, write.CoordinatorProxyRuntimeHash) || !strings.EqualFold(state.CoordinatorRuntimeHash, write.CoordinatorRuntimeHash) {
		return errors.New("ordinary fleet generation proxy runtime differs at the sealed write head")
	}
	if write.BatcherRuntimeHash == "" {
		if state.BatcherAddress != "" || state.BatcherRuntimeHash != "" {
			return errors.New("ordinary fleet generation carried write unexpectedly has a batcher runtime")
		}
		return nil
	}
	if !common.IsHexAddress(write.BatcherAddress) || common.HexToAddress(write.BatcherAddress) == (common.Address{}) || !strings.EqualFold(state.BatcherAddress, write.BatcherAddress) || !strings.EqualFold(state.BatcherRuntimeHash, write.BatcherRuntimeHash) {
		return errors.New("ordinary fleet generation batcher runtime differs at the sealed write head")
	}
	if !strings.EqualFold(write.Action.PlanHash, evidence.PlanHash) {
		return nil
	}
	batcher, err := finalReleaseRuntimeRootByName(evidence, "fleet_batcher")
	if err != nil {
		return err
	}
	proxy, proxyErr := finalReleaseRuntimeRootByName(evidence, "coordinator_proxy")
	implementation, implementationErr := finalReleaseRuntimeRootByName(evidence, "coordinator_upgrade_implementation")
	if proxyErr != nil || implementationErr != nil {
		return stateMismatchError(errors.Join(proxyErr, implementationErr), "ordinary fleet generation refresh runtime roots are unavailable")
	}
	if !strings.EqualFold(write.CoordinatorProxy, proxy.Address) || !strings.EqualFold(write.CoordinatorImplementation, implementation.Address) || !strings.EqualFold(write.CoordinatorProxyRuntimeHash, proxy.RuntimeCodeHash) || !strings.EqualFold(write.CoordinatorRuntimeHash, implementation.RuntimeCodeHash) || !strings.EqualFold(write.BatcherAddress, batcher.Address) || !strings.EqualFold(write.BatcherRuntimeHash, batcher.RuntimeCodeHash) {
		return errors.New("ordinary fleet generation current batch write does not bind release roots")
	}
	return nil
}

// Validates the exact historical identity fields before a public reader sends
// RPC. This catches incomplete source evidence before an endpoint can be
// asked to fill a missing predecessor-plan value with current chain state.
func verifyFinalFleetGenerationRuntimeRequest(write FinalFleetGenerationWriteEvidence) error {
	if !common.IsHexAddress(write.CoordinatorProxy) || common.HexToAddress(write.CoordinatorProxy) == (common.Address{}) || !common.IsHexAddress(write.CoordinatorImplementation) || common.HexToAddress(write.CoordinatorImplementation) == (common.Address{}) {
		return errors.New("ordinary fleet generation runtime request has an invalid coordinator address")
	}
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "ordinary fleet generation implementation slot", value: write.CoordinatorImplementationSlot},
		{label: "ordinary fleet generation proxy code hash", value: write.CoordinatorProxyRuntimeHash},
		{label: "ordinary fleet generation implementation code hash", value: write.CoordinatorRuntimeHash},
	} {
		if err := requireFinalHex32(field.label, field.value); err != nil {
			return err
		}
	}
	if write.BatcherRuntimeHash != "" {
		if !common.IsHexAddress(write.BatcherAddress) || common.HexToAddress(write.BatcherAddress) == (common.Address{}) {
			return errors.New("ordinary fleet generation runtime batcher address is invalid")
		}
		if err := requireFinalHex32("ordinary fleet generation batcher code hash", write.BatcherRuntimeHash); err != nil {
			return err
		}
	} else if write.BatcherAddress != "" {
		return errors.New("ordinary fleet generation runtime carried write names a batcher")
	}
	if err := verifyFinalHead("ordinary fleet generation runtime head", write.EVMHead); err != nil {
		return err
	}
	return nil
}

// Formats an identity mismatch with the write action so large historical
// replays identify the one plan mutation needing investigation.
func finalFleetGenerationRuntimeError(write FinalFleetGenerationWriteEvidence, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("ordinary fleet generation runtime %s: %w", write.Action.ActionID, err)
}

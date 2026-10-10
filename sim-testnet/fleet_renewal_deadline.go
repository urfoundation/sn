// Renewal admission budgets the remaining joined pipeline, and every fresh
// write checks the latest chain boundary. Recovery keeps its exact signed bytes.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Inclusion, finality and postcondition reads need more than one block per
// joined wave. This admission floor is a forecast, not a promise about Rpc I/O.
const fleetRenewalMinimumWaveBlocks uint64 = 4

// Keep admission and execution on the same operation and in-flight boundaries.
func fleetRenewalWaveEnd(actions []Action, offset int) int {
	end := offset
	for end < len(actions) && end-offset < int(fleetRenewalMaximumInFlight) && actions[end].Parameters["operation"] == actions[offset].Parameters["operation"] {
		end++
	}
	return end
}

// Completed waves cost no new inclusion time. A wholly signed recovery must
// remain possible after activation; only new unsigned work needs admission.
func fleetRenewalRemainingWindow(planHash string, actions []Action, entries []JournalEntry) (uint64, uint64, error) {
	type progress struct{ signed, finalized bool }
	progressKVs := map[string]progress{}
	intentKVs := map[string]string{}
	for _, action := range actions {
		intentKVs[action.ID] = action.IntentHash
	}
	for _, entry := range entries {
		if entry.PlanHash != planHash || intentKVs[entry.ActionID] != entry.IntentHash {
			continue
		}
		state := progressKVs[entry.ActionID]
		state.signed = state.signed || entry.TransactionHash != ""
		state.finalized = state.finalized || entry.Stage == StageFinalized || entry.Stage == StageVerified
		progressKVs[entry.ActionID] = state
	}
	var unsigned, waves uint64
	for offset := 0; offset < len(actions); {
		end := fleetRenewalWaveEnd(actions, offset)
		pending := false
		for _, action := range actions[offset:end] {
			state := progressKVs[action.ID]
			if !state.signed && !state.finalized {
				unsigned++
			}
			pending = pending || !state.finalized
		}
		if pending {
			waves++
		}
		offset = end
	}
	if unsigned == 0 {
		return 0, 0, nil
	}
	blocks, ok := checkedMul(waves, fleetRenewalMinimumWaveBlocks)
	if !ok {
		return 0, 0, errors.New("renewal remaining wave horizon overflows")
	}
	blocks, ok = checkedAdd(blocks, futureEpochInclusionSafetyBlocks)
	if !ok {
		return 0, 0, errors.New("renewal remaining inclusion margin overflows")
	}
	return waves, blocks, nil
}

// Strictly future at finalized height is insufficient when latest has already
// crossed the boundary. Read the actual scheduled activation at latest height.
func readFleetRenewalDeadline(ctx context.Context, manager *EvmTxManager, address common.Address, validFrom uint64) (uint64, uint64, error) {
	if manager == nil || manager.client == nil || validFrom == 0 {
		return 0, 0, errors.New("renewal deadline reader is unavailable")
	}
	head, err := manager.client.BlockNumber(ctx)
	if err != nil {
		return 0, 0, err
	}
	coordinator := stabi.NewSTCoordinator()
	start, err := rawCoordinatorCallAt(ctx, manager, address, coordinator.PackEpochStartBlock(new(big.Int).SetUint64(validFrom)), coordinator.UnpackEpochStartBlock, head)
	if err != nil || start == nil || !start.IsUint64() {
		return 0, 0, stateMismatchError(err, "renewal activation block is not uint64")
	}
	return head, start.Uint64(), nil
}

// The exact approval remains unchanged when its time budget has expired.
func validateFleetRenewalDeadline(validFrom, head, activation, waves, required uint64) error {
	if activation <= head || activation-head <= required {
		return fmt.Errorf("renewal activation epoch %d starts at block %d; latest block %d leaves insufficient time for %d remaining waves (%d blocks including safety margin); choose a later --renewal-valid-from-epoch for a successor and retain exact signed transaction recovery", validFrom, activation, head, waves, required)
	}
	return nil
}

// Apply and planning both use fresh chain time after their expensive audits.
// Replay-only resumes do not inherit a deadline intended for new signatures.
func verifyFleetRenewalDeadline(ctx context.Context, manager *EvmTxManager, address common.Address, renewal FleetRenewal, planHash string, actions []Action, entries []JournalEntry) error {
	waves, required, err := fleetRenewalRemainingWindow(planHash, actions, entries)
	if err != nil || required == 0 {
		return err
	}
	head, activation, err := readFleetRenewalDeadline(ctx, manager, address, renewal.ValidFromEpoch)
	if err != nil {
		return err
	}
	return validateFleetRenewalDeadline(renewal.ValidFromEpoch, head, activation, waves, required)
}

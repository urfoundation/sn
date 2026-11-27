package miner

// What `provider fleet bind` and `provider fleet status` tell a miner about the
// epoch 8 late-binding window (protocol/fleet_late_binding_epoch8.go): the
// settlement epoch a binding counts from, from the block it landed in.

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// The FleetBound log reads of `fleet status` span at most this many blocks.
const fleetLateBindingLogChunkBlocks = uint64(5_000)

// fleetBindingCountsFrom says which settlement epoch a binding signed for
// [validFromEpoch, validToEpoch] that landed in landedBlock counts from.
func fleetBindingCountsFrom(manifest *protocol.FleetManifest, validFromEpoch uint64, validToEpoch uint64, landedBlock uint64) string {
	first := protocol.FleetBindingFirstEpoch(manifest.ChainID, manifest.Coordinator, validFromEpoch, landedBlock)
	if first != validFromEpoch {
		return fmt.Sprintf("fleet binding counts from epoch %d: it landed in block %d, inside the epoch %d late-binding window (blocks %d to %d), so it counts for epoch %d from that block and then for its signed epochs [%d,%d]", first, landedBlock, protocol.Epoch8LateBindingEpoch, protocol.Epoch8LateBindingFirstBlock, protocol.Epoch8LateBindingEndBlock-1, first, validFromEpoch, validToEpoch)
	}
	return fmt.Sprintf("fleet binding counts from epoch %d, its signed first epoch (it landed in block %d)", first, landedBlock)
}

// fleetStatusCountsFrom is `fleet status`'s counts_from_epoch line. Only a
// binding signed from epoch 9 on the mainnet coordinator can be in the window;
// for one, the line reads the binding's FleetBound event over the window.
func fleetStatusCountsFrom(ctx context.Context, authority *fleetMainnetRuntimeAuthority, manifest *protocol.FleetManifest, endpoints []string, clientID [16]byte, record stabi.STCoordinatorBindingRecord) string {
	if record.Generation == 0 {
		return "counts_from_epoch: none, the client has no binding"
	}
	if !protocol.Epoch8LateBindingDeployment(manifest.ChainID, manifest.Coordinator) || record.ValidFromEpoch != protocol.Epoch8LateBindingEpoch+1 {
		return fmt.Sprintf("counts_from_epoch: %d", record.ValidFromEpoch)
	}
	landed, err := fleetBoundBlock(ctx, authority, endpoints, clientID, record.Generation)
	if err != nil {
		return fmt.Sprintf("counts_from_epoch: %d, or %d if it landed in blocks %d to %d (the epoch %d late-binding window); its FleetBound event was not read: %v", record.ValidFromEpoch, protocol.Epoch8LateBindingEpoch, protocol.Epoch8LateBindingFirstBlock, protocol.Epoch8LateBindingEndBlock-1, protocol.Epoch8LateBindingEpoch, err)
	}
	if landed == 0 {
		return fmt.Sprintf("counts_from_epoch: %d, it did not land in the epoch %d late-binding window (blocks %d to %d)", record.ValidFromEpoch, protocol.Epoch8LateBindingEpoch, protocol.Epoch8LateBindingFirstBlock, protocol.Epoch8LateBindingEndBlock-1)
	}
	first := protocol.FleetBindingFirstEpoch(manifest.ChainID, manifest.Coordinator, record.ValidFromEpoch, landed)
	return fmt.Sprintf("counts_from_epoch: %d (it landed in block %d, inside the epoch %d late-binding window, blocks %d to %d)", first, landed, protocol.Epoch8LateBindingEpoch, protocol.Epoch8LateBindingFirstBlock, protocol.Epoch8LateBindingEndBlock-1)
}

// fleetBoundBlock returns the block of the client's FleetBound event for
// generation inside the window, or 0 when it is not there.
func fleetBoundBlock(ctx context.Context, authority *fleetMainnetRuntimeAuthority, endpoints []string, clientID [16]byte, generation uint64) (uint64, error) {
	if authority == nil {
		return 0, errors.New("no mainnet runtime authority")
	}
	client, _, err := authority.dialEvm(ctx, endpoints)
	if err != nil {
		return 0, err
	}
	defer client.Close()
	finalized, err := client.HeaderByNumber(ctx, big.NewInt(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return 0, err
	}
	last := min(protocol.Epoch8LateBindingEndBlock-1, finalized.Number.Uint64())
	contractABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return 0, err
	}
	var topic common.Hash
	copy(topic[:], clientID[:])
	query := ethereum.FilterQuery{Addresses: []common.Address{protocol.Epoch8LateBindingCoordinator}, Topics: [][]common.Hash{{contractABI.Events["FleetBound"].ID}, {topic}}}
	for start := protocol.Epoch8LateBindingFirstBlock; start <= last; start += fleetLateBindingLogChunkBlocks {
		query.FromBlock, query.ToBlock = new(big.Int).SetUint64(start), new(big.Int).SetUint64(min(start+fleetLateBindingLogChunkBlocks-1, last))
		logs, err := client.FilterLogs(ctx, query)
		if err != nil {
			return 0, err
		}
		for index := range logs {
			event, err := stCoordinator.UnpackFleetBoundEvent(&logs[index])
			if err != nil || logs[index].Removed {
				continue
			}
			if event.ClientId == clientID && event.Generation == generation {
				return logs[index].BlockNumber, nil
			}
		}
	}
	return 0, nil
}

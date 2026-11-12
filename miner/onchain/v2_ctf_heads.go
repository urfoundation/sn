package onchain

// v2_ctf_heads.go — the head bindings of a capture the flag audit
// (CAPTURETHEFLAG.md §4.4, §8 check 3). Before each draw, every census
// candidate with an active fleet binding at either boundary of the epoch is
// left out, so a census that lists a head-bound client, which P5 keeps out of
// every census, still draws no bound winner. The validator reads those
// bindings from the chain (validator/settlement_v2_audit_chain.go), and the
// check reads the same public record, so that both reach the same draw:
//
//   - the v1 coordinator, STSettlement2's v1Coordinator();
//   - its FleetBound events, from the start of its epoch 0, the block that
//     initialized it, before which it emits none, through the finalized
//     block, in spans the public endpoint answers;
//   - for each drawn epoch k, every client with a FleetBound event whose
//     validity covers k, and bindingAt(client, k) at k's start and end
//     blocks. A client active at either is bound.
//
// bindingAt reads the bound hotkey's UID at those blocks, and old blocks'
// events come from their state, so the reads need an archive RPC. A pruned
// node answers a log scan of old blocks with fewer events, not an error, so
// the check first reads the coordinator's code at its first block: on a
// pruned node that read fails, and the audit with it, instead of drawing
// without bindings it could not see.

import (
	"context"
	"fmt"
	"maps"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/ctf"
	"github.com/urfoundation/sn/stabi"
)

// v2Settlement is STSettlement2, whose v1Coordinator() names the v1
// coordinator.
var v2Settlement = stabi.NewSTSettlement2()

// v2FleetBoundID is the topic of the v1 coordinator's FleetBound event.
var v2FleetBoundID = func() common.Hash {
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		panic(err)
	}
	return parsed.Events[stabi.STCoordinatorFleetBoundEventName].ID
}()

// v2FleetBinding is one FleetBound event: a client and the epochs its
// binding is valid for.
type v2FleetBinding struct {
	client             [16]byte
	validFrom, validTo uint64
}

// v2CtfHeads holds the head bindings of the drawn epochs: for each epoch
// with a committed census, the clients whose FleetBound validity covers it,
// mapped to whether bindingAt was active at either of its boundaries. It is
// the audit's ledger.HeadOracle.
type v2CtfHeads struct {
	coordinator common.Address
	// from is the coordinator's first block, where the scan began, and events
	// the FleetBound events it read through the finalized block.
	from   uint64
	events int
	bound  map[uint64]map[[16]byte]bool
}

func (h *v2CtfHeads) HeadBindings(ctx context.Context, epoch uint64) (map[[16]byte]bool, error) {
	bound, ok := h.bound[epoch]
	if !ok {
		return nil, fmt.Errorf("no head bindings were read for epoch %d", epoch)
	}
	return maps.Clone(bound), nil
}

// readV2CtfHeads reads, at the reader's block, the head bindings of each
// covered epoch whose census was committed, from the v1 coordinator of
// STSettlement2 at settlement. It reads nothing when no census was
// committed: there is no draw.
func readV2CtfHeads(ctx context.Context, r *v2Reader, settlement common.Address, epochs map[uint64]ctf.EpochFacts) (*v2CtfHeads, error) {
	var drawn []uint64
	for k, facts := range epochs {
		if facts.Census != nil {
			drawn = append(drawn, k)
		}
	}
	if len(drawn) == 0 {
		return nil, nil
	}
	slices.Sort(drawn)
	head := r.block.Uint64()
	ret, err := ethCallAt(ctx, r.client, settlement, v2Settlement.PackV1Coordinator(), r.block)
	if err != nil {
		return nil, fmt.Errorf("coordinator %s v1Coordinator(): %w", settlement.Hex(), err)
	}
	coordinator, err := v2Settlement.UnpackV1Coordinator(ret)
	if err != nil {
		return nil, fmt.Errorf("coordinator %s v1Coordinator(): %w", settlement.Hex(), err)
	}
	h := &v2CtfHeads{coordinator: coordinator, bound: map[uint64]map[[16]byte]bool{}}
	boundary := func(name string, pack func(*big.Int) []byte, unpack func([]byte) (*big.Int, error), epoch uint64) (uint64, error) {
		ret, err := ethCallAt(ctx, r.client, coordinator, pack(new(big.Int).SetUint64(epoch)), r.block)
		var block *big.Int
		if err == nil {
			block, err = unpack(ret)
		}
		if err == nil && !block.IsUint64() {
			err = fmt.Errorf("block %s is not a 64-bit integer", block)
		}
		if err != nil {
			return 0, fmt.Errorf("v1 coordinator %s %s(%d): %w", coordinator.Hex(), name, epoch, err)
		}
		return block.Uint64(), nil
	}
	start := func(epoch uint64) (uint64, error) {
		return boundary("epochStartBlock", stCoordinator.PackEpochStartBlock, stCoordinator.UnpackEpochStartBlock, epoch)
	}
	end := func(epoch uint64) (uint64, error) {
		return boundary("epochEndBlock", stCoordinator.PackEpochEndBlock, stCoordinator.UnpackEpochEndBlock, epoch)
	}
	if h.from, err = start(0); err != nil {
		return nil, err
	}
	if h.from > head {
		return nil, fmt.Errorf("v1 coordinator %s starts at block %d, after the finalized block %d", coordinator.Hex(), h.from, head)
	}
	from := new(big.Int).SetUint64(h.from)
	code, err := retryOnchainRead(ctx, func(ctx context.Context) ([]byte, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return r.client.CodeAt(cctx, coordinator, from)
	})
	if err == nil && len(code) == 0 {
		err = fmt.Errorf("the RPC answered no code")
	}
	if err != nil {
		return nil, fmt.Errorf("the head bindings need an archive RPC: v1 coordinator %s's code at its first block %d: %w", coordinator.Hex(), h.from, err)
	}
	bindings, err := readV2FleetBindings(ctx, r, coordinator, h.from, head)
	if err != nil {
		return nil, err
	}
	h.events = len(bindings)
	for _, k := range drawn {
		var candidates [][16]byte
		for _, binding := range bindings {
			if binding.validFrom <= k && k <= binding.validTo && !slices.Contains(candidates, binding.client) {
				candidates = append(candidates, binding.client)
			}
		}
		bound := make(map[[16]byte]bool, len(candidates))
		h.bound[k] = bound
		if len(candidates) == 0 {
			continue
		}
		first, err := start(k)
		if err != nil {
			return nil, err
		}
		last, err := end(k)
		if err != nil {
			return nil, err
		}
		if first >= last || last > head {
			return nil, fmt.Errorf("epoch %d boundaries [%d,%d] are not closed at the finalized block %d", k, first, last, head)
		}
		epoch := new(big.Int).SetUint64(k)
		for _, block := range []uint64{first, last} {
			at := new(big.Int).SetUint64(block)
			for _, client := range candidates {
				ret, err := ethCallAt(ctx, r.client, coordinator, stCoordinator.PackBindingAt(client, epoch), at)
				var binding stabi.BindingAtOutput
				if err == nil {
					binding, err = stCoordinator.UnpackBindingAt(ret)
				}
				if err != nil {
					return nil, fmt.Errorf("v1 coordinator %s bindingAt(0x%x,%d) at block %d: %w", coordinator.Hex(), client, k, block, err)
				}
				bound[client] = bound[client] || binding.Active
			}
		}
	}
	return h, nil
}

// readV2FleetBindings reads the coordinator's FleetBound events over
// [from, to], in spans the public endpoint answers.
func readV2FleetBindings(ctx context.Context, r *v2Reader, coordinator common.Address, from, to uint64) ([]v2FleetBinding, error) {
	query := ethereum.FilterQuery{Addresses: []common.Address{coordinator}, Topics: [][]common.Hash{{v2FleetBoundID}}}
	var bindings []v2FleetBinding
	for start := from; start <= to; start += v2LogChunkBlocks {
		end := min(start+v2LogChunkBlocks-1, to)
		query.FromBlock, query.ToBlock = new(big.Int).SetUint64(start), new(big.Int).SetUint64(end)
		logs, err := retryOnchainRead(ctx, func(ctx context.Context) ([]types.Log, error) {
			cctx, cancel := context.WithTimeout(ctx, callTimeout)
			defer cancel()
			return r.client.FilterLogs(cctx, query)
		})
		if err != nil {
			return nil, fmt.Errorf("v1 coordinator %s FleetBound events: eth_getLogs [%d,%d]: %w", coordinator.Hex(), start, end, err)
		}
		for i := range logs {
			log := &logs[i]
			if log.Removed || log.Address != coordinator || log.BlockNumber < start || log.BlockNumber > end {
				return nil, fmt.Errorf("v1 coordinator %s FleetBound events: eth_getLogs [%d,%d] returned log %d outside the query", coordinator.Hex(), start, end, i)
			}
			event, err := stCoordinator.UnpackFleetBoundEvent(log)
			if err != nil {
				return nil, fmt.Errorf("v1 coordinator %s FleetBound log %d of block %d: %w", coordinator.Hex(), log.Index, log.BlockNumber, err)
			}
			bindings = append(bindings, v2FleetBinding{client: event.ClientId, validFrom: event.ValidFromEpoch, validTo: event.ValidToEpoch})
		}
		if end == to {
			break
		}
	}
	return bindings, nil
}

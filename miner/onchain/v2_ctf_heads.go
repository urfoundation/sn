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
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

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

// v2CtfHeads is the audit's ledger.HeadOracle, over the v1 coordinator of
// STSettlement2 at settlement, read at the reader's block. ctf.Verify asks it
// for each epoch it draws, as it asks the validator's: the first ask reads
// the coordinator, checks that the RPC serves its first block's state and
// scans its FleetBound events; each epoch's bindings are then read once. An
// audit that draws nothing reads nothing.
type v2CtfHeads struct {
	r          *v2Reader
	settlement common.Address
	// scanned is set once the scan read coordinator, its first block from,
	// where the scan began, and the FleetBound events through the finalized
	// block.
	scanned     bool
	coordinator common.Address
	from        uint64
	bindings    []v2FleetBinding
	// bound holds each drawn epoch's clients whose FleetBound validity covers
	// it, mapped to whether bindingAt was active at either of its boundaries.
	bound map[uint64]map[[16]byte]bool
}

func newV2CtfHeads(r *v2Reader, settlement common.Address) *v2CtfHeads {
	return &v2CtfHeads{r: r, settlement: settlement, bound: map[uint64]map[[16]byte]bool{}}
}

func (h *v2CtfHeads) HeadBindings(ctx context.Context, epoch uint64) (map[[16]byte]bool, error) {
	if !h.scanned {
		if err := h.scan(ctx); err != nil {
			return nil, err
		}
	}
	bound, ok := h.bound[epoch]
	if !ok {
		var err error
		if bound, err = h.read(ctx, epoch); err != nil {
			return nil, err
		}
		h.bound[epoch] = bound
	}
	return maps.Clone(bound), nil
}

// scan reads the v1 coordinator, checks that the RPC serves the state of its
// first block, and reads its FleetBound events from there through the
// reader's block.
func (h *v2CtfHeads) scan(ctx context.Context) error {
	r := h.r
	ret, err := ethCallAt(ctx, r.client, h.settlement, v2Settlement.PackV1Coordinator(), r.block)
	var coordinator common.Address
	if err == nil {
		coordinator, err = v2Settlement.UnpackV1Coordinator(ret)
	}
	if err != nil {
		return fmt.Errorf("coordinator %s v1Coordinator(): %w", h.settlement.Hex(), err)
	}
	from, err := h.boundary(ctx, coordinator, "epochStartBlock", stCoordinator.PackEpochStartBlock, stCoordinator.UnpackEpochStartBlock, 0)
	if err != nil {
		return err
	}
	if head := r.block.Uint64(); from > head {
		return fmt.Errorf("v1 coordinator %s starts at block %d, after the finalized block %d", coordinator.Hex(), from, head)
	}
	code, err := retryOnchainRead(ctx, func(ctx context.Context) ([]byte, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return r.client.CodeAt(cctx, coordinator, new(big.Int).SetUint64(from))
	})
	if err == nil && len(code) == 0 {
		err = errors.New("the RPC answered no code")
	}
	if err != nil {
		return fmt.Errorf("the head bindings need an archive RPC: v1 coordinator %s's code at its first block %d: %w", coordinator.Hex(), from, err)
	}
	bindings, err := readV2FleetBindings(ctx, r, coordinator, from, r.block.Uint64())
	if err != nil {
		return err
	}
	h.scanned, h.coordinator, h.from, h.bindings = true, coordinator, from, bindings
	return nil
}

// read reads epoch's head bindings: for each client with a FleetBound event
// whose validity covers it, in first-bound order, bindingAt(client, epoch) at
// the epoch's start and end blocks.
func (h *v2CtfHeads) read(ctx context.Context, epoch uint64) (map[[16]byte]bool, error) {
	var candidates [][16]byte
	for _, binding := range h.bindings {
		if binding.validFrom <= epoch && epoch <= binding.validTo && !slices.Contains(candidates, binding.client) {
			candidates = append(candidates, binding.client)
		}
	}
	bound := make(map[[16]byte]bool, len(candidates))
	if len(candidates) == 0 {
		return bound, nil
	}
	first, err := h.boundary(ctx, h.coordinator, "epochStartBlock", stCoordinator.PackEpochStartBlock, stCoordinator.UnpackEpochStartBlock, epoch)
	if err != nil {
		return nil, err
	}
	last, err := h.boundary(ctx, h.coordinator, "epochEndBlock", stCoordinator.PackEpochEndBlock, stCoordinator.UnpackEpochEndBlock, epoch)
	if err != nil {
		return nil, err
	}
	if head := h.r.block.Uint64(); first >= last || last > head {
		return nil, fmt.Errorf("epoch %d boundaries [%d,%d] are not closed at the finalized block %d", epoch, first, last, head)
	}
	k := new(big.Int).SetUint64(epoch)
	for _, block := range []uint64{first, last} {
		at := new(big.Int).SetUint64(block)
		for _, client := range candidates {
			ret, err := ethCallAt(ctx, h.r.client, h.coordinator, stCoordinator.PackBindingAt(client, k), at)
			var binding stabi.BindingAtOutput
			if err == nil {
				binding, err = stCoordinator.UnpackBindingAt(ret)
			}
			if err != nil {
				return nil, fmt.Errorf("v1 coordinator %s bindingAt(0x%x,%d) at block %d: %w", h.coordinator.Hex(), client, epoch, block, err)
			}
			bound[client] = bound[client] || binding.Active
		}
	}
	return bound, nil
}

// boundary reads one of coordinator's epoch boundaries at the reader's block.
func (h *v2CtfHeads) boundary(ctx context.Context, coordinator common.Address, name string, pack func(*big.Int) []byte, unpack func([]byte) (*big.Int, error), epoch uint64) (uint64, error) {
	ret, err := ethCallAt(ctx, h.r.client, coordinator, pack(new(big.Int).SetUint64(epoch)), h.r.block)
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

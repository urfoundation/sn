package main

// Each retained fleet proof owns its checkpoint. A bad block or unavailable
// observer cannot hide independent proofs whose own checkpoints authenticate.

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

func collectHistoricalFleetCheckpoints(ctx context.Context, client *ethclient.Client, calls []historicalFleetGenerationOneCall, independent bool) []error {
	failures := make([]error, len(calls))
	if len(calls) == 0 { return failures }
	var finalized ChainHead
	var headErr error
	if client == nil { headErr = errors.New("historical fleet EVM reader is unavailable") } else { finalized, headErr = finalizedEVMHead(ctx, client) }
	if headErr != nil {
		for index := range calls { failures[index] = fmt.Errorf("checkpoint blocked by finalized EVM head: %w", headErr) }
		return failures
	}
	checkpoints := make([]ChainHead, len(calls))
	numbers := make([]uint64, 0, len(calls))
	positions := map[uint64]int{}
	for index, call := range calls {
		if call.record == nil || call.observe == nil || (len(call.data) != 0 && call.address == (common.Address{})) { failures[index] = errors.New("historical fleet call evidence is incomplete"); continue }
		checkpoint := call.record.EVMFinalized
		if independent { checkpoint = call.record.IndependentEVMFinalized }
		if checkpoint.Number == 0 || checkpoint.Hash == "" { failures[index] = errors.New("historical EVM checkpoint is incomplete"); continue }
		checkpoints[index] = checkpoint
		if _, exists := positions[checkpoint.Number]; !exists { positions[checkpoint.Number] = len(numbers); numbers = append(numbers, checkpoint.Number) }
	}
	heads := make([]ChainHead, len(numbers))
	headerErrors := make([]error, len(numbers))
	for first := 0; first < len(numbers); first += maximumEVMRPCBatchCalls {
		last := min(first+maximumEVMRPCBatchCalls, len(numbers))
		blocks := make([]*evmRPCBlock, last-first)
		batch := make([]rpc.BatchElem, last-first)
		for index := first; index < last; index++ { batch[index-first] = rpc.BatchElem{Method: "eth_getBlockByNumber", Args: []any{hexutil.EncodeUint64(numbers[index]), false}, Result: &blocks[index-first]} }
		var batchErr error
		if batchErr = ctx.Err(); batchErr == nil { batchErr = client.Client().BatchCallContext(ctx, batch) }
		for index := range batch {
			position := first+index
			if batchErr != nil { headerErrors[position] = batchErr; continue }
			if batch[index].Error != nil { headerErrors[position] = batch[index].Error; continue }
			heads[position], headerErrors[position] = decodeEVMRPCBlock(blocks[index], new(big.Int).SetUint64(numbers[position]))
		}
	}
	for index, checkpoint := range checkpoints {
		if failures[index] != nil { continue }
		position := positions[checkpoint.Number]
		if headerErrors[position] != nil { failures[index] = fmt.Errorf("historical EVM block %d: %w", checkpoint.Number, headerErrors[position]); continue }
		ready, err := checkpointVisibility(checkpoint, finalized, heads[position].Hash)
		if err != nil { failures[index] = fmt.Errorf("historical EVM block %d: %w", checkpoint.Number, err) } else if !ready { failures[index] = fmt.Errorf("historical EVM block %d is not finalized (head %d)", checkpoint.Number, finalized.Number) }
	}
	return failures
}

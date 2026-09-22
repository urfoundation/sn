// Receipt publication can lag finalized account state. Reconcile the exact
// signed hash before treating an advanced nonce as loss of an approved action.
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

// Repeated uncertain submission responses cannot create an unlimited write
// retry loop; every retry reuses the previously persisted signed transaction.
const maximumExactEvmBroadcastFailures = 3

// The retry owner cannot sign or submit through this read-only capability.
type evmExactReceiptReader interface {
	TransactionReceipt(context.Context, common.Hash) (*ethTypes.Receipt, error)
}

// Missing receipt after nonce progress is incomplete observation, not evidence
// that another transaction won. The ordinary bounded RPC owner may retry it.
type evmReceiptPropagationError struct{ hash common.Hash }

// Diagnostics retain the original transaction identity for a later resume.
func (self *evmReceiptPropagationError) Error() string {
	return fmt.Sprintf("exact transaction %s receipt is pending after finalized nonce progress; retained signed bytes remain authoritative", self.hash)
}

// A propagation gap is temporary without claiming a transport timeout.
func (self *evmReceiptPropagationError) Temporary() bool { return true }

// Keep net.Error classification distinct from elapsed request time.
func (self *evmReceiptPropagationError) Timeout() bool { return false }

// The initial lookup may legitimately miss an unmined transaction. Only a
// consumed nonce grants bounded missing-receipt retries, with no rebroadcast.
func readExactEvmReceipt(ctx context.Context, reader evmExactReceiptReader, hash common.Hash, consumed bool, policy finalSemanticRPCRetryPolicy) (*ethTypes.Receipt, error) {
	if reader == nil || hash == (common.Hash{}) {
		return nil, errors.New("exact receipt recovery authority is incomplete")
	}
	var receipt *ethTypes.Receipt
	err := retryEvmReadRpcCall(ctx, "exact transaction receipt "+hash.Hex(), policy, func(attemptCtx context.Context) error {
		current, err := reader.TransactionReceipt(attemptCtx, hash)
		if errors.Is(err, ethereum.NotFound) && consumed {
			return &evmReceiptPropagationError{hash: hash}
		}
		if err != nil {
			return err
		}
		if err := validateEVMReceiptIdentity(current, hash); err != nil {
			return err
		}
		receipt = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

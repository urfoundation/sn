// Pinned EVM reads recover within one deadline. Batch subdivision shares that
// deadline and each element's attempt ceiling; writes never enter this owner.
package validator

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	chainReadMaximumAttempts = 4
	chainReadRetryDelay      = 250 * time.Millisecond
)

// These instance-owned hooks exercise cancellation and budgets without clock
// races. Production leaves them zero, and callers freeze them before reads.
type chainReadRetryHooks struct {
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait        func(context.Context, time.Duration) error
}

// The private marker allows nested header, code and view readers to borrow
// their operation's deadline instead of granting each split a fresh budget.
type chainReadOperationKey struct{}

// Establishes one finite owner even for command callers with no deadline.
func (self *ChainClient) chainReadOperationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if owned, _ := ctx.Value(chainReadOperationKey{}).(bool); owned {
		return ctx, func() {}
	}
	withTimeout := self.readRetryHooks.withTimeout
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	ctx, cancel := withTimeout(ctx, chainReadMaximumAttempts*chainReadCallTimeout(ctx))
	return context.WithValue(ctx, chainReadOperationKey{}, true), cancel
}

// A completed batch can omit an expected id. This transport failure is distinct
// from an empty or malformed result, which never receives this marker.
type chainRpcMissingResponseError struct {
	cause error
}

// Keeps the failing transport's diagnostic and error tree intact.
func (self *chainRpcMissingResponseError) Error() string { return self.cause.Error() }

// Exposes cancellation and structured status to the common classifier.
func (self *chainRpcMissingResponseError) Unwrap() error { return self.cause }

// Backoff applies after a failure only; healthy owned-node reads have no quota
// delay. Jitter prevents independent validators from retrying in lockstep.
func (self *ChainClient) waitChainReadRetry(ctx context.Context, attempt int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delay := chainReadRetryDelay << min(attempt-1, chainReadMaximumAttempts-1)
	delay += time.Duration(rand.Int64N(int64(delay / 2)))
	wait := self.readRetryHooks.wait
	if wait == nil {
		wait = waitReleaseSnapshotRetry
	}
	return wait(ctx, delay)
}

// A successful retry discards only its recovered transport failure. Caller
// cancellation and any mixed semantic failure remain visible immediately.
func (self *ChainClient) retryChainRead(ctx context.Context, read func(context.Context) error) error {
	ctx, cancel := self.chainReadOperationContext(ctx)
	defer cancel()
	var lastErr error
	for attempt := 1; attempt <= chainReadMaximumAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(lastErr, err)
		}
		callCtx, callCancel := context.WithTimeout(ctx, chainReadCallTimeout(ctx))
		err := read(callCtx)
		err = errors.Join(err, callCtx.Err())
		callCancel()
		if err == nil {
			return ctx.Err()
		}
		lastErr = err
		if !RetryableEvidenceTransportError(lastErr) || attempt == chainReadMaximumAttempts {
			return lastErr
		}
		if err := self.waitChainReadRetry(ctx, attempt); err != nil {
			return errors.Join(lastErr, err)
		}
	}
	return lastErr
}

// Every element is inspected before admitting a retry so a missing response
// cannot hide a sibling revert, malformed result or changed canonical hash.
// Successful members stay in outputs and never consume another request.
func (self *ChainClient) readChainBatch(ctx context.Context, selector rpc.BlockNumberOrHash, calls []chainBatchCall, indices []int, outputs [][]byte, attempt int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw := make([]hexutil.Bytes, len(indices))
	batch := make([]rpc.BatchElem, len(indices))
	for index, absolute := range indices {
		call := calls[absolute]
		batch[index] = rpc.BatchElem{
			Method: "eth_call",
			Args: []any{
				map[string]any{"to": call.address, "input": hexutil.Bytes(call.calldata)},
				selector,
			},
			Result: &raw[index],
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, chainReadCallTimeout(ctx))
	err := self.client.Client().BatchCallContext(callCtx, batch)
	err = errors.Join(err, callCtx.Err())
	cancel()
	pending := indices
	if err == nil {
		pending = make([]int, 0, len(indices))
		var failures []error
		for index, element := range batch {
			absolute := indices[index]
			switch {
			case element.Error != nil:
				cause := element.Error
				if cause == rpc.ErrMissingBatchResponse {
					cause = &chainRpcMissingResponseError{cause: cause}
				}
				failures = append(failures, fmt.Errorf("exact-block EVM batch element %d: %w", absolute, cause))
				pending = append(pending, absolute)
			case len(raw[index]) == 0:
				failures = append(failures, fmt.Errorf("exact-block EVM batch element %d is empty", absolute))
			default:
				outputs[absolute] = append([]byte(nil), raw[index]...)
			}
		}
		err = errors.Join(failures...)
	}
	if err == nil {
		return ctx.Err()
	}
	if !RetryableEvidenceTransportError(err) || attempt == chainReadMaximumAttempts {
		return err
	}
	if waitErr := self.waitChainReadRetry(ctx, attempt); waitErr != nil {
		return errors.Join(err, waitErr)
	}
	if ownerErr := ctx.Err(); ownerErr != nil {
		return errors.Join(err, ownerErr)
	}
	// A refused large request can succeed at smaller sizes. Each child inherits
	// the failed attempt, so splitting cannot multiply any element's allowance.
	groups := [][]int{pending}
	if len(pending) > 1 {
		middle := len(pending) / 2
		groups = [][]int{pending[:middle], pending[middle:]}
	}
	for _, group := range groups {
		if childErr := self.readChainBatch(ctx, selector, calls, group, outputs, attempt+1); childErr != nil {
			return errors.Join(err, childErr)
		}
	}
	return ctx.Err()
}

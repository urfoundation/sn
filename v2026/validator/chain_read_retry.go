// Pinned EVM reads recover within one deadline. Batch subdivision shares that
// deadline and capped retry pacing; writes never enter this owner.
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
	chainReadOperationTimeout = 300 * time.Second
	chainReadRetryDelay       = 250 * time.Millisecond
	chainReadRetryMaximumStep = 3
)

// These instance-owned hooks exercise cancellation and budgets without clock
// races. Production leaves them zero, and callers freeze them before reads.
type chainReadRetryHooks struct {
	withTimeout        func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	withAttemptTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait               func(context.Context, time.Duration) error
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
	ctx, cancel := withTimeout(ctx, chainReadOperationTimeout)
	return context.WithValue(ctx, chainReadOperationKey{}, true), cancel
}

// Each request has a fresh attempt bound, clamped by the admitted operation
// and caller. A separate seam keeps operation-budget tests instance-owned.
func (self *ChainClient) chainReadAttemptContext(ctx context.Context) (context.Context, context.CancelFunc) {
	withTimeout := self.readRetryHooks.withAttemptTimeout
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	return withTimeout(ctx, chainCallTimeout)
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
	if err := evidenceReadContextError(ctx); err != nil {
		return err
	}
	delay := chainReadRetryDelay << min(attempt-1, chainReadRetryMaximumStep)
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
	if ctx == nil || self == nil || read == nil {
		return errors.New("EVM read retry owner is unavailable")
	}
	ctx, cancel := self.chainReadOperationContext(ctx)
	defer cancel()
	var lastErr error
	for attempt := 1; ; attempt++ {
		if err := evidenceReadContextError(ctx); err != nil {
			return errors.Join(lastErr, err)
		}
		callCtx, callCancel := self.chainReadAttemptContext(ctx)
		err := evidenceReadContextError(callCtx)
		if err == nil {
			err = read(callCtx)
		}
		err = errors.Join(err, evidenceReadContextError(callCtx))
		callCancel()
		ownerErr := evidenceReadContextError(ctx)
		if err == nil {
			return ownerErr
		}
		lastErr = errors.Join(err, ownerErr)
		if ownerErr != nil || !RetryableEvidenceTransportError(lastErr) {
			return lastErr
		}
		if err := self.waitChainReadRetry(ctx, attempt); err != nil {
			return errors.Join(lastErr, err, evidenceReadContextError(ctx))
		}
	}
}

// Every element is inspected before admitting a retry so a missing response
// cannot hide a sibling revert, malformed result or changed canonical hash.
// Successful members stay in outputs and never consume another request.
func (self *ChainClient) readChainBatch(ctx context.Context, selector rpc.BlockNumberOrHash, calls []chainBatchCall, indices []int, outputs [][]byte, attempt int) error {
	var lastErr error
	for {
		if err := evidenceReadContextError(ctx); err != nil {
			return errors.Join(lastErr, err)
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
		callCtx, cancel := self.chainReadAttemptContext(ctx)
		callErr := evidenceReadContextError(callCtx)
		if callErr == nil {
			callErr = self.client.Client().BatchCallContext(callCtx, batch)
		}
		attemptErr := evidenceReadContextError(callCtx)
		cancel()
		pending, err := collectChainBatchReadResults(batch, raw, indices, outputs, callErr, attemptErr)
		ownerErr := evidenceReadContextError(ctx)
		err = errors.Join(err, ownerErr)
		if err == nil {
			return nil
		}
		if ownerErr != nil || !RetryableEvidenceTransportError(err) {
			return err
		}
		lastErr = err
		if waitErr := self.waitChainReadRetry(ctx, attempt); waitErr != nil {
			return errors.Join(err, waitErr, evidenceReadContextError(ctx))
		}
		if ownerErr := evidenceReadContextError(ctx); ownerErr != nil {
			return errors.Join(err, ownerErr)
		}
		// A refused large request can succeed at smaller sizes. A singleton
		// retries in place, bounding stack depth by the original batch size.
		if len(pending) == 1 {
			indices = pending
			attempt++
			continue
		}
		middle := len(pending) / 2
		for _, group := range [][]int{pending[:middle], pending[middle:]} {
			if childErr := self.readChainBatch(ctx, selector, calls, group, outputs, attempt+1); childErr != nil {
				return errors.Join(err, childErr)
			}
		}
		return evidenceReadContextError(ctx)
	}
}

// A completed batch is inspected even if its attempt deadline has just ended.
// Cancellation or timeout cannot hide a returned semantic or framing failure.
func collectChainBatchReadResults(batch []rpc.BatchElem, raw []hexutil.Bytes, indices []int, outputs [][]byte, callErr, attemptErr error) ([]int, error) {
	if callErr != nil {
		return indices, errors.Join(callErr, attemptErr)
	}
	pending := make([]int, 0, len(indices))
	failures := []error{attemptErr}
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
	// A late attempt deadline cannot cause an empty retry group when all
	// returned members succeeded; the owner receives that exact deadline.
	if len(pending) == 0 && attemptErr != nil {
		return indices, errors.Join(failures...)
	}
	return pending, errors.Join(failures...)
}

// Pinned contract and block reads own one bounded retry budget. Their callers
// still authenticate the returned bytes; retries never replay a submission.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Exhaustion is distinct from one transient attempt, so an enclosing action
// audit cannot silently multiply this already completed retry budget.
type evmReadRpcExhaustedError struct {
	operation      string
	attempts       int
	attemptTimeout time.Duration
	cause          error
}

func (self *evmReadRpcExhaustedError) Error() string {
	return fmt.Sprintf("%s read retries exhausted after %d attempts (per-attempt timeout %s): %v", self.operation, self.attempts, self.attemptTimeout, self.cause)
}

func (self *evmReadRpcExhaustedError) Unwrap() error { return self.cause }

func evmReadRpcRetriesExhausted(err error) bool {
	var exhausted *evmReadRpcExhaustedError
	return errors.As(err, &exhausted)
}

// Accept typed transport failures and explicit temporary provider responses.
// A revert mentioning "timeout", missing archive state, malformed bytes, or a
// mixed integrity/transport error must never become retryable by string match.
func evmReadRpcErrorIsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || evmReadRpcRetriesExhausted(err) {
		return false
	}
	if _, fileError := err.(*os.PathError); fileError {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !evmReadRpcErrorIsTransient(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			return evmReadRpcErrorIsTransient(cause)
		}
	}
	if errors.Is(err, rpc.ErrMissingBatchResponse) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, permanent := range []string{"execution reverted", "pruned", "archive", "state already discarded", "unknown block", "header not found", "missing trie", "state unavailable"} {
		if strings.Contains(message, permanent) {
			return false
		}
	}
	statusCode := 0
	switch httpError := err.(type) {
	case rpc.HTTPError:
		statusCode = httpError.StatusCode
	case *rpc.HTTPError:
		statusCode = httpError.StatusCode
	}
	if statusCode != 0 {
		switch statusCode {
		case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	var rpcError rpc.Error
	if errors.As(err, &rpcError) {
		if rpcError.ErrorCode() == -32000 && (strings.EqualFold(strings.TrimSpace(rpcError.Error()), "upstream overloaded") || strings.EqualFold(strings.TrimSpace(rpcError.Error()), "Historical work rate limit exceeded")) {
			return true
		}
		switch rpcError.ErrorCode() {
		case -32002, -32005, -32016:
			return true
		default:
			return false
		}
	}
	return ownedEvmTransportErrorIsTransient(err)
}

// A managed context belongs to a higher read retry loop. Keep its deadline and
// return its original error; the owner decides whether another attempt fits.
func retryEvmReadRpcCall(ctx context.Context, operation string, policy finalSemanticRPCRetryPolicy, call func(context.Context) error) error {
	if ctx == nil || call == nil || operation == "" {
		return errors.New("EVM read retry context is incomplete")
	}
	if err := policy.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if managed, _ := ctx.Value(ownedEvmRpcRetryBudgetKey{}).(bool); managed {
		return errors.Join(call(ctx), ctx.Err())
	}
	delay := policy.initialRetryDelay
	for attempt := 1; attempt <= policy.maximumAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, policy.attemptTimeout)
		attemptCtx = context.WithValue(attemptCtx, ownedEvmRpcRetryBudgetKey{}, true)
		err := call(attemptCtx)
		if err == nil {
			err = attemptCtx.Err()
		}
		cancel()
		if parentErr := ctx.Err(); parentErr != nil {
			return parentErr
		}
		if err == nil || !evmReadRpcErrorIsTransient(err) {
			return err
		}
		if attempt == policy.maximumAttempts {
			return &evmReadRpcExhaustedError{operation: operation, attempts: attempt, attemptTimeout: policy.attemptTimeout, cause: err}
		}
		fmt.Fprintf(os.Stderr, "sim-testnet: %s transient read attempt %d/%d (timeout %s); retry in %s: %v\n", operation, attempt, policy.maximumAttempts, policy.attemptTimeout, delay, err)
		if err := policy.wait(ctx, delay); err != nil {
			return err
		}
		delay = min(delay*2, policy.maximumRetryDelay)
	}
	return errors.New("EVM read retry budget is empty")
}

// The same block, target and calldata survive every attempt. Failed bytes are
// discarded before decoding so a partial response cannot satisfy a later read.
func readEvmContractAt(ctx context.Context, client *ethclient.Client, address common.Address, data []byte, block uint64) ([]byte, error) {
	if client == nil {
		return nil, errors.New("EVM contract reader is unavailable")
	}
	var output []byte
	err := retryEvmReadRpcCall(ctx, fmt.Sprintf("eth_call block %d", block), defaultFinalSemanticRPCRetryPolicy(), func(attemptCtx context.Context) error {
		var err error
		output, err = client.CallContract(attemptCtx, ethereum.CallMsg{To: &address, Data: data}, new(big.Int).SetUint64(block))
		return err
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

type evmRpcRead struct {
	method string
	args   []any
}

type evmRpcReadResult[T any] struct {
	value T
	err   error
}

// A timed-out archive batch may exceed the provider's per-request work budget.
// Other transient failures retain their request shape: splitting a connection
// reset or a rate limit would add requests without evidence of oversized work.
func evmReadRpcBatchTimedOut(err error) bool {
	if !evmReadRpcErrorIsTransient(err) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if evmReadRpcBatchTimedOut(cause) {
				return true
			}
		}
		return false
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			return evmReadRpcBatchTimedOut(cause)
		}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return true
	}
	var rpcError rpc.Error
	return errors.As(err, &rpcError) && rpcError.ErrorCode() == -32002
}

// Retry only missing or transient elements. A timeout halves the next batch
// width; every chunk in that retry round shares the same attempt deadline, so
// splitting does not multiply the four-attempt budget. Successful and permanent
// results stay in their original positions, including completed chunks before
// a later chunk fails. Failed transports never contribute partially read bytes.
func readEvmRpcBatchWithPolicy[T any](ctx context.Context, operation string, reads []evmRpcRead, policy finalSemanticRPCRetryPolicy, call func(context.Context, []rpc.BatchElem) error) ([]evmRpcReadResult[T], error) {
	return readEvmRpcBatchWithCapacity[T](ctx, operation, reads, policy, call, nil)
}

// Learned route capacity changes grouping only; each operation retains its
// own result array and the existing bounded retry/finality contract.
func readEvmRpcBatchWithCapacity[T any](ctx context.Context, operation string, reads []evmRpcRead, policy finalSemanticRPCRetryPolicy, call func(context.Context, []rpc.BatchElem) error, capacity *evmReadBatchCapacity) ([]evmRpcReadResult[T], error) {
	if ctx == nil || operation == "" || len(reads) == 0 || len(reads) > maximumEVMRPCBatchCalls || call == nil {
		return nil, errors.New("EVM read batch is unavailable or unbounded")
	}
	if err := policy.validate(); err != nil {
		return nil, err
	}
	results := make([]evmRpcReadResult[T], len(reads))
	pending := make([]int, len(reads))
	for index, read := range reads {
		if read.method != "eth_call" && read.method != "eth_getBlockByNumber" {
			return nil, fmt.Errorf("EVM batch method %q is not an approved read", read.method)
		}
		pending[index] = index
	}
	lease := capacity.begin(len(reads))
	batchWidth := lease.width
	priorWidth := batchWidth
	finishRound := func(err error) error {
		if ctx.Err() == nil && evmReadRpcBatchTimedOut(err) && len(pending) > 1 {
			batchWidth = (min(batchWidth, len(pending)) + 1) / 2
			capacity.timedOut(batchWidth)
		}
		return err
	}
	err := retryEvmReadRpcCall(ctx, operation, policy, func(attemptCtx context.Context) error {
		batchWidth = lease.limit(batchWidth)
		if batchWidth < priorWidth {
			fmt.Fprintf(os.Stderr, "sim-testnet: %s retry %d pending reads with batch width %d (previous %d)\n", operation, len(pending), batchWidth, priorWidth)
			priorWidth = batchWidth
		}
		current := pending
		pending = make([]int, 0, len(current))
		var transient []error
		for start := 0; start < len(current); start += batchWidth {
			end := min(start+batchWidth, len(current))
			outputs := make([]T, end-start)
			batch := make([]rpc.BatchElem, end-start)
			for index, resultIndex := range current[start:end] {
				read := reads[resultIndex]
				batch[index] = rpc.BatchElem{Method: read.method, Args: read.args, Result: &outputs[index]}
			}
			callErr := attemptCtx.Err()
			if callErr == nil {
				callErr = call(attemptCtx, batch)
			}
			// A late nil response cannot consume the pending set before the
			// outer retry owner notices its expired attempt. Earlier completed
			// chunks remain retained; this chunk and its suffix must be retried.
			if callErr == nil && errors.Is(attemptCtx.Err(), context.DeadlineExceeded) {
				callErr = attemptCtx.Err()
			}
			if callErr != nil {
				// Earlier completed chunks are durable in results. Neither the
				// failed chunk nor the unstarted suffix has authenticated bytes.
				pending = append(pending, current[start:]...)
				return finishRound(errors.Join(append(transient, callErr)...))
			}
			for index, resultIndex := range current[start:end] {
				result := &results[resultIndex]
				result.err = batch[index].Error
				if result.err == nil {
					result.value = outputs[index]
				} else if evmReadRpcErrorIsTransient(result.err) {
					pending = append(pending, resultIndex)
					transient = append(transient, fmt.Errorf("batch element %d: %w", resultIndex, result.err))
				}
			}
		}
		return finishRound(errors.Join(transient...))
	})
	if err != nil {
		if len(pending) == 0 {
			return nil, err
		}
		for _, index := range pending {
			results[index].err = err
		}
	} else if ctx.Err() == nil {
		complete := true
		for _, result := range results {
			complete = complete && result.err == nil
		}
		if complete {
			lease.succeeded()
		}
	}
	return results, nil
}

// Pinned contract and block reads own one bounded retry budget. Their callers
// still authenticate the returned bytes; retries never replay a submission.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
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

// Retry only missing or transient elements. Successful and permanent results
// stay in their original positions; callers can persist independent successes
// even when another element exhausts its budget. Each attempt decodes into
// fresh storage, never into bytes partially filled by a failed transport.
func readEvmRpcBatchWithPolicy[T any](ctx context.Context, operation string, reads []evmRpcRead, policy finalSemanticRPCRetryPolicy, call func(context.Context, []rpc.BatchElem) error) ([]evmRpcReadResult[T], error) {
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
	err := retryEvmReadRpcCall(ctx, operation, policy, func(attemptCtx context.Context) error {
		outputs := make([]T, len(pending))
		batch := make([]rpc.BatchElem, len(pending))
		for index, resultIndex := range pending {
			read := reads[resultIndex]
			batch[index] = rpc.BatchElem{Method: read.method, Args: read.args, Result: &outputs[index]}
		}
		if err := call(attemptCtx, batch); err != nil {
			return err
		}
		next := make([]int, 0, len(pending))
		var transient []error
		for index, resultIndex := range pending {
			result := &results[resultIndex]
			result.err = batch[index].Error
			if result.err == nil {
				result.value = outputs[index]
			} else if evmReadRpcErrorIsTransient(result.err) {
				next = append(next, resultIndex)
				transient = append(transient, fmt.Errorf("batch element %d: %w", resultIndex, result.err))
			}
		}
		pending = next
		return errors.Join(transient...)
	})
	if err != nil {
		if len(pending) == 0 {
			return nil, err
		}
		for _, index := range pending {
			results[index].err = err
		}
	}
	return results, nil
}

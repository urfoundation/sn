// Deadline controls exercise the real read paths without shortening production
// budgets. A counted refusal is a test clock transition, never retry policy.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

const chainReadTestFailureAttempts = 4

// Each operation owns its logical deadline and refusal count, including when
// several reads share one ChainClient or when a batch subdivides its members.
type chainReadTestBudget struct {
	stateLock sync.Mutex
	deadline  *chainReadRetryDeadline
	waits     int
}

type chainReadTestBudgetKey struct{}

// Retain full production duration requests while advancing the operation only
// after the requested number of completed failures, including body closure.
func chainReadRetryTestHooks(failures int) chainReadRetryHooks {
	return chainReadRetryHooks{
		withTimeout: func(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
			deadline := time.Now().Add(timeout)
			if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
				deadline = parent
			}
			owner := &chainReadRetryDeadline{Context: ctx, deadline: deadline, done: make(chan struct{})}
			budget := &chainReadTestBudget{deadline: owner}
			return context.WithValue(owner, chainReadTestBudgetKey{}, budget), owner.finish
		},
		wait: func(ctx context.Context, _ time.Duration) error {
			budget, ok := ctx.Value(chainReadTestBudgetKey{}).(*chainReadTestBudget)
			if !ok {
				return errors.New("synthetic retry clock has no operation")
			}
			func() {
				budget.stateLock.Lock()
				defer budget.stateLock.Unlock()
				budget.waits++
				if budget.waits >= failures {
					budget.deadline.finish()
				}
			}()
			return ctx.Err()
		},
	}
}

// Immediate refusals must consume the intended recovery window rather than
// exhausting a small request count in a few seconds. Virtual time drives only
// the real context timers and backoff; every request traverses geth and Http.
func TestChainReadBudgetRetriesFastRefusalsForFullOperation(t *testing.T) {
	for _, batch := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			started := time.Now()
			calls := 0
			chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
				requests, actualBatch := chainReadRetryRequests(t, request)
				if actualBatch != batch || len(requests) != 1 {
					t.Fatal("retry changed the exact singleton request")
				}
				calls++
				deadline, bounded := request.Context().Deadline()
				if !bounded || deadline.Sub(time.Now()) > 60*time.Second || deadline.After(started.Add(300*time.Second)) {
					t.Fatal("actual request escaped its attempt or total budget")
				}
				return chainReadRetryResponse(t, http.StatusServiceUnavailable, "synthetic capacity"), nil
			})
			var err error
			if batch {
				_, err = chain.batchCallsAtHashContext(t.Context(), 123, chainBatchTestBlockHash, []chainBatchCall{{address: common.Address{1}, calldata: []byte{7}}})
			} else {
				_, err = chain.ethCallAtHashContext(t.Context(), common.Address{1}, []byte{7}, 123, chainBatchTestBlockHash)
			}
			var status gethrpc.HTTPError
			if time.Since(started) != 300*time.Second || calls <= 4 || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable || !RetryableEvidenceTransportError(err) {
				t.Fatalf("batch=%t elapsed=%s calls=%d error=%v", batch, time.Since(started), calls, err)
			}
		})
	}
}

// A timed-out request closes before another request begins, and a retry still
// uses the originally selected hash, target and calldata through the real Rpc.
func TestChainReadBudgetRecoversAfterFullAttemptTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		calls := 0
		var expired context.Context
		chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
			requests, _ := chainReadRetryRequests(t, request)
			calls++
			if calls == 1 {
				deadline, bounded := request.Context().Deadline()
				if !bounded || deadline.Sub(started) != 60*time.Second {
					t.Fatal("first attempt did not receive sixty seconds")
				}
				expired = request.Context()
				<-expired.Done()
				return nil, expired.Err()
			}
			if expired.Err() != context.DeadlineExceeded {
				t.Fatal("retry started before the prior attempt ended")
			}
			return chainReadRetryResponse(t, http.StatusOK, chainReadRetryReply(t, requests[0])), nil
		})
		chain.readRetryHooks.wait = chainReadRetryNoWait
		output, err := chain.ethCallAtHashContext(t.Context(), common.Address{1}, []byte{7}, 123, chainBatchTestBlockHash)
		if err != nil || !bytes.Equal(output, []byte{7}) || calls != 2 || time.Since(started) != 60*time.Second {
			t.Fatalf("full attempt did not recover: calls=%d output=%x elapsed=%s error=%v", calls, output, time.Since(started), err)
		}
	})
}

// A successful second endpoint is normal ordered failover. Repeated complete
// passes are allowed only while every rejected endpoint has a transport cause.
func TestChainReadBudgetDialRecoversOriginalEndpoint(t *testing.T) {
	var calls int
	server := jsonRpcStub(t, "0x3b1")
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hooks := chainReadRetryTestHooks(2)
	hooks.withAttemptTimeout = func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		calls++
		if timeout != 60*time.Second {
			t.Fatal("dial attempt did not receive sixty seconds")
		}
		if calls == 1 {
			expired := &chainReadRetryDeadline{Context: parent, deadline: time.Now().Add(timeout), done: make(chan struct{})}
			expired.finish()
			return expired, expired.finish
		}
		return context.WithTimeout(parent, timeout)
	}
	chain, err := dialChainWithReadRetryContext(ctx, []string{server.URL}, common.Address{1}, true, hooks)
	if chain != nil {
		defer chain.Close()
	}
	if err != nil || chain == nil || chain.RpcUrl() != server.URL || chain.ChainId().Uint64() != 945 || calls != 2 {
		t.Fatalf("original endpoint did not recover: calls=%d chain=%p error=%v", calls, chain, err)
	}
}

// Legacy height-based views are still read-only calls. A transport retry must
// retain its original explicit block and calldata and run ordinary decoding.
func TestChainReadBudgetRetriesHeightBoundCall(t *testing.T) {
	calls := 0
	chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
		var call chainBatchRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil || call.Method != "eth_call" || len(call.Params) != 2 || string(call.Params[1]) != `"0x7b"` {
			t.Fatalf("height call lost exact selector: %+v %v", call, err)
		}
		calls++
		if calls == 1 {
			return chainReadRetryResponse(t, http.StatusServiceUnavailable, "synthetic capacity"), nil
		}
		return chainReadRetryResponse(t, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": "0x07"}), nil
	})
	chain.readRetryHooks.wait = chainReadRetryNoWait
	output, err := chain.ethCallAtContext(t.Context(), common.Address{1}, []byte{7}, new(big.Int).SetUint64(123))
	if err != nil || !bytes.Equal(output, []byte{7}) || calls != 2 {
		t.Fatalf("height-bound read did not recover: calls=%d output=%x error=%v", calls, output, err)
	}
}

// Error graph shape is provider-controlled input to retry classification.
// Keep counting traversal without recursively formatting synthetic cycles.
type chainReadBudgetCauses struct {
	causes []error
	calls  *int
}

func (self *chainReadBudgetCauses) Error() string { return "synthetic read causes" }

func (self *chainReadBudgetCauses) Unwrap() []error {
	*self.calls++
	return self.causes
}

// Custom matching cannot attest a network error or conceal a hard sibling.
type chainReadBudgetOpaqueCause struct{}

func (*chainReadBudgetOpaqueCause) Error() string { return "synthetic opaque cause" }
func (*chainReadBudgetOpaqueCause) Is(error) bool { panic("custom Is is not read authority") }
func (*chainReadBudgetOpaqueCause) As(any) bool   { panic("custom As is not read authority") }

// Every joined branch must be known and transient. Nil, excessive and cyclic
// graphs terminate without a custom matcher or a retry, even with a timeout.
func TestChainReadBudgetRejectsUnboundedOrMixedCauseTrees(t *testing.T) {
	calls := 0
	cycle := &chainReadBudgetCauses{calls: &calls}
	cycle.causes = []error{cycle}
	wide := &chainReadBudgetCauses{causes: make([]error, 129), calls: &calls}
	for index := range wide.causes {
		wide.causes[index] = syscall.ECONNRESET
	}
	var absent *chainReadBudgetCauses
	for _, cause := range []error{nil, absent, cycle, wide, &chainReadBudgetCauses{causes: []error{nil, syscall.ECONNRESET}, calls: &calls}, &chainReadBudgetOpaqueCause{}, errors.Join(context.DeadlineExceeded, &chainReadBudgetOpaqueCause{}), errors.Join(context.DeadlineExceeded, context.Canceled), io.ErrUnexpectedEOF} {
		before := calls
		if RetryableEvidenceTransportError(cause) || calls-before > 33 {
			t.Fatalf("untrusted read cause received retries: type=%T traversals=%d", cause, calls-before)
		}
	}
	if !RetryableEvidenceTransportError(errors.Join(syscall.ECONNRESET, context.DeadlineExceeded)) {
		t.Fatal("bounded observed transport causes lost retry authority")
	}
}

// Deadline expiry after geth has decoded a complete response must retain the
// semantic member alongside that deadline and any missing sibling response.
func TestChainReadBudgetLateDeadlinePreservesCompletedBatchFailures(t *testing.T) {
	hard := errors.New("synthetic canonical identity conflict")
	for _, failure := range []error{hard, nil} {
		batch := []gethrpc.BatchElem{{Error: gethrpc.ErrMissingBatchResponse}, {Error: failure}}
		_, err := collectChainBatchReadResults(batch, make([]hexutil.Bytes, 2), []int{0, 1}, make([][]byte, 2), nil, context.DeadlineExceeded)
		if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, gethrpc.ErrMissingBatchResponse) || failure != nil && !errors.Is(err, failure) || RetryableEvidenceTransportError(err) {
			t.Fatalf("late attempt deadline concealed a completed hard member: %v", err)
		}
	}
}

// The error-tree bound must still cover every member of the largest admitted
// real batch and the original groups retained along a subdivision failure.
func TestChainReadBudgetClassifiesMaximumMissingBatch(t *testing.T) {
	var failure error
	for count := chainMaximumBatchCalls; count > 0; count /= 2 {
		batch := make([]gethrpc.BatchElem, count)
		indices := make([]int, count)
		for index := range batch {
			batch[index].Error = gethrpc.ErrMissingBatchResponse
			indices[index] = index
		}
		_, err := collectChainBatchReadResults(batch, make([]hexutil.Bytes, count), indices, make([][]byte, count), nil, nil)
		failure = errors.Join(failure, err)
	}
	if !RetryableEvidenceTransportError(errors.Join(failure, context.DeadlineExceeded)) || !retryableProductionSteeringRead(errors.Join(failure, context.DeadlineExceeded)) {
		t.Fatal("bounded legitimate batch exhaustion became a hard failure")
	}
}

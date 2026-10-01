// Adjacent failure cases keep malformed data, semantic failures and expired
// owners outside the bounded read-recovery path.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Observe backoff without delaying the test or changing transport behavior.
func chainReadRetryNoWait(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// A missing first response must not conceal a sibling revert or malformed
// output. Every error is retained and no retry grants a partial census.
func TestChainReadRetryRejectsMixedBatchFailures(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		calls := 0
		chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
			requests, _ := chainReadRetryRequests(t, request)
			calls++
			reply := chainReadRetryReply(t, requests[1])
			if malformed {
				reply["result"] = "0xzz"
			} else {
				delete(reply, "result")
				reply["error"] = map[string]any{"code": -32000, "message": "canonical hash differs: timeout"}
			}
			return chainReadRetryResponse(t, http.StatusOK, []map[string]any{reply}), nil
		})
		chain.readRetryHooks.wait = func(context.Context, time.Duration) error {
			t.Fatal("mixed batch failure retried")
			return nil
		}
		outputs, err := chain.batchCallsAtHashContext(t.Context(), 123, chainBatchTestBlockHash, []chainBatchCall{{address: common.Address{1}, calldata: []byte{1}}, {address: common.Address{1}, calldata: []byte{2}}})
		if err == nil || outputs != nil || calls != 1 || !errors.Is(err, gethrpc.ErrMissingBatchResponse) || RetryableEvidenceTransportError(err) || !strings.Contains(err.Error(), "element 1") {
			t.Fatalf("mixed failure became a retry or partial result: malformed=%t outputs=%x calls=%d error=%v", malformed, outputs, calls, err)
		}
	}
}

// Complete malformed responses and permanent statuses are not interrupted
// reads, even when the diagnostic includes transport-related language.
func TestChainReadRetryRejectsMalformedSingletonResponses(t *testing.T) {
	for _, raw := range []string{"", `{"jsonrpc":`, `{"jsonrpc":"2.0","id":1,"result":"0x"}`, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"signature mismatch: timeout"}}`} {
		calls := 0
		chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
			requests, _ := chainReadRetryRequests(t, request)
			calls++
			body := strings.ReplaceAll(raw, `"id":1`, `"id":`+string(requests[0].ID))
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		chain.readRetryHooks.wait = func(context.Context, time.Duration) error {
			t.Fatal("malformed singleton retried")
			return nil
		}
		output, err := chain.ethCallAtHashContext(t.Context(), common.Address{1}, []byte{1}, 123, chainBatchTestBlockHash)
		if err == nil || output != nil || calls != 1 || RetryableEvidenceTransportError(err) {
			t.Fatalf("malformed response gained transport authority: raw=%q output=%x calls=%d error=%v", raw, output, calls, err)
		}
	}
}

// A real body interruption closes before retry; a separate close-integrity
// error must stop recovery even when the read itself was transient.
func TestChainReadRetryOwnsInterruptedBodyBeforeRetry(t *testing.T) {
	for _, closeFailure := range []bool{false, true} {
		calls, closes := 0, 0
		closeErr := errors.New("response custody close failure")
		chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
			requests, _ := chainReadRetryRequests(t, request)
			if calls != closes {
				t.Fatal("retry began before the previous body closed")
			}
			calls++
			response := chainReadRetryResponse(t, http.StatusOK, chainReadRetryReply(t, requests[0]))
			body := &chainHTTPTestBody{reader: response.Body, afterClose: func() { closes++ }}
			if calls == 1 {
				body.reader = &chainReadRetryBrokenBody{}
				if closeFailure {
					body.closeErr = closeErr
				}
			}
			response.Body = body
			return response, nil
		})
		chain.readRetryHooks.wait = chainReadRetryNoWait
		output, err := chain.ethCallAtHashContext(t.Context(), common.Address{1}, []byte{7}, 123, chainBatchTestBlockHash)
		if closeFailure {
			if output != nil || !errors.Is(err, closeErr) || !errors.Is(err, io.ErrUnexpectedEOF) || calls != 1 || closes != 1 || RetryableEvidenceTransportError(err) {
				t.Fatalf("close failure was hidden: output=%x calls=%d closes=%d error=%v", output, calls, closes, err)
			}
		} else if err != nil || !bytes.Equal(output, []byte{7}) || calls != 2 || closes != 2 {
			t.Fatalf("body interruption did not recover: output=%x calls=%d closes=%d error=%v", output, calls, closes, err)
		}
	}
}

// A deterministic read failure arrives before a complete JSON message.
type chainReadRetryBrokenBody struct{}

// Returning an actual body error distinguishes transport from JSON syntax.
func (self *chainReadRetryBrokenBody) Read(buffer []byte) (int, error) {
	return copy(buffer, []byte(`{"jsonrpc":`)), io.ErrUnexpectedEOF
}

// Persistent failures retain their typed cause while bounding physical calls.
func TestChainReadRetryBoundsEveryFailedMember(t *testing.T) {
	for _, batch := range []bool{false, true} {
		calls, waits := 0, 0
		chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
			requests, _ := chainReadRetryRequests(t, request)
			if len(requests) != 1 {
				t.Fatal("retry duplicated an element")
			}
			calls++
			return chainReadRetryResponse(t, http.StatusServiceUnavailable, "capacity"), nil
		})
		chain.readRetryHooks.wait = func(ctx context.Context, delay time.Duration) error {
			waits++
			minimum := chainReadRetryDelay << (waits - 1)
			if delay < minimum || delay >= minimum+minimum/2 {
				t.Fatalf("retry lost bounded jitter: wait=%d delay=%s", waits, delay)
			}
			return ctx.Err()
		}
		var err error
		if batch {
			_, err = chain.batchCallsAtHashContext(t.Context(), 123, chainBatchTestBlockHash, []chainBatchCall{{address: common.Address{1}, calldata: []byte{1}}})
		} else {
			_, err = chain.ethCallAtHashContext(t.Context(), common.Address{1}, []byte{1}, 123, chainBatchTestBlockHash)
		}
		var status gethrpc.HTTPError
		if err == nil || !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable || !RetryableEvidenceTransportError(err) || calls != chainReadMaximumAttempts || waits != chainReadMaximumAttempts-1 {
			t.Fatalf("retry exceeded its allowance: batch=%t calls=%d waits=%d error=%v", batch, calls, waits, err)
		}
	}
}

// Cancellation after a failed request cannot admit another attempt or split.
func TestChainReadRetryCancellationStopsFurtherWork(t *testing.T) {
	for _, batch := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
			chainReadRetryRequests(t, request)
			calls++
			return chainReadRetryResponse(t, http.StatusServiceUnavailable, "capacity"), nil
		})
		chain.readRetryHooks.wait = func(context.Context, time.Duration) error { cancel(); return nil }
		var err error
		if batch {
			_, err = chain.batchCallsAtHashContext(ctx, 123, chainBatchTestBlockHash, []chainBatchCall{{address: common.Address{1}, calldata: []byte{1}}, {address: common.Address{1}, calldata: []byte{2}}})
		} else {
			_, err = chain.ethCallAtHashContext(ctx, common.Address{1}, []byte{1}, 123, chainBatchTestBlockHash)
		}
		cancel()
		var status gethrpc.HTTPError
		if !errors.Is(err, context.Canceled) || !errors.As(err, &status) || calls != 1 || RetryableEvidenceTransportError(err) {
			t.Fatalf("canceled owner issued more work: batch=%t calls=%d error=%v", batch, calls, err)
		}
	}
}

// A logical deadline is advanced only at the chosen retry boundary.
type chainReadRetryDeadline struct {
	context.Context
	deadline time.Time
	done     chan struct{}
	close    sync.Once
}

// Preserve the selected outer deadline through every nested attempt.
func (self *chainReadRetryDeadline) Deadline() (time.Time, bool) { return self.deadline, true }

// Cancellation is observable by the real HTTP client too.
func (self *chainReadRetryDeadline) Done() <-chan struct{} { return self.done }

// Parent cancellation remains distinct from the simulated deadline.
func (self *chainReadRetryDeadline) Err() error {
	if err := self.Context.Err(); err != nil {
		return err
	}
	select {
	case <-self.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// End the finite operation exactly once, including deferred cleanup.
func (self *chainReadRetryDeadline) finish() { self.close.Do(func() { close(self.done) }) }

// Split retries and nested readers cannot mint another operation deadline.
func TestChainReadRetrySharesOneBudgetAcrossBatchSplits(t *testing.T) {
	var owner *chainReadRetryDeadline
	calls, budgets := 0, 0
	chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
		chainReadRetryRequests(t, request)
		calls++
		if deadline, ok := request.Context().Deadline(); !ok || deadline.After(owner.deadline) || time.Until(deadline) > chainCallTimeout {
			t.Fatal("attempt extended its admitted budget")
		}
		return chainReadRetryResponse(t, http.StatusServiceUnavailable, "capacity"), nil
	})
	chain.readRetryHooks.withTimeout = func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		budgets++
		if duration != chainReadMaximumAttempts*chainCallTimeout {
			t.Fatalf("unexpected operation budget %s", duration)
		}
		owner = &chainReadRetryDeadline{Context: ctx, deadline: time.Now().Add(duration), done: make(chan struct{})}
		return owner, owner.finish
	}
	chain.readRetryHooks.wait = func(context.Context, time.Duration) error { owner.finish(); return nil }
	outputs, err := chain.batchCallsAtHashContext(t.Context(), 123, chainBatchTestBlockHash, []chainBatchCall{{address: common.Address{1}, calldata: []byte{1}}, {address: common.Address{1}, calldata: []byte{2}}})
	var status gethrpc.HTTPError
	if outputs != nil || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &status) || calls != 1 || budgets != 1 {
		t.Fatalf("split replaced an expired budget: outputs=%x calls=%d budgets=%d error=%v", outputs, calls, budgets, err)
	}
}

// Authenticate the header on a retry, then reject a changed hash before any
// contract call. A transport outage never authorizes a different state root.
func TestChainReadRetryStillRejectsChangedBlockIdentity(t *testing.T) {
	calls := 0
	chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
		var call chainBatchRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil || call.Method != "eth_getBlockByHash" {
			t.Fatalf("invalid header read: %+v %v", call, err)
		}
		calls++
		if calls == 1 {
			return chainReadRetryResponse(t, http.StatusServiceUnavailable, "capacity"), nil
		}
		return chainReadRetryResponse(t, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": map[string]any{"number": "0x7b", "hash": common.Hash{9}}}), nil
	})
	chain.blockNumbers = nil
	chain.readRetryHooks.wait = chainReadRetryNoWait
	output, err := chain.ethCallAtHashContext(t.Context(), common.Address{1}, []byte{1}, 123, chainBatchTestBlockHash)
	if output != nil || err == nil || calls != 2 || RetryableEvidenceTransportError(err) || !strings.Contains(err.Error(), "hash response identifies") {
		t.Fatalf("retry bypassed block identity: output=%x calls=%d error=%v", output, calls, err)
	}
}

// The public evidence reader shares one budget for header authentication,
// bytecode and all views. Retrying transport cannot skip the ordinary record
// checks or replace the final evidence with a partial result.
func TestChainReadRetryRecoversCompleteEvidenceReader(t *testing.T) {
	fixture := newEvidenceChainFixture(t)
	prior := fixture.chain
	codeAttempts, batchAttempts, budgets := 0, 0, 0
	client := chainHTTPTestRPC(t, prior.RpcUrl(), chainHTTPResponseLimit, func(base http.RoundTripper) http.RoundTripper {
		return chainHTTPTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			body, err := request.GetBody()
			if err != nil {
				return nil, err
			}
			raw, readErr := io.ReadAll(body)
			if err := errors.Join(readErr, body.Close()); err != nil {
				return nil, err
			}
			if bytes.HasPrefix(raw, []byte("[")) {
				batchAttempts++
				if batchAttempts == 1 {
					return chainReadRetryResponse(t, http.StatusServiceUnavailable, "capacity"), nil
				}
			} else {
				var call chainBatchRPCRequest
				if err := json.Unmarshal(raw, &call); err != nil {
					return nil, err
				}
				if call.Method == "eth_getCode" {
					codeAttempts++
					if codeAttempts == 1 {
						return nil, context.DeadlineExceeded
					}
				}
			}
			return base.RoundTrip(request)
		})
	})
	fixture.chain = &ChainClient{client: ethclient.NewClient(client), coordinator: prior.coordinator, chainId: prior.ChainId(), contractAddr: prior.contractAddr, release: true}
	fixture.chain.readRetryHooks = chainReadRetryHooks{
		wait: chainReadRetryNoWait,
		withTimeout: func(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
			budgets++
			return context.WithTimeout(ctx, timeout)
		},
	}
	publication, err := fixture.readCommitment(t.Context())
	if err != nil || publication.Header != fixture.header || publication.PublishedBlock != 1101 || codeAttempts != 2 || batchAttempts != 3 || budgets != 1 || fixture.codeCalls.Load() != 1 || fixture.viewCalls.Load() != 9 {
		t.Fatalf("composed evidence did not recover: publication=%+v code=%d batch=%d budgets=%d completed-code=%d completed-views=%d error=%v", publication, codeAttempts, batchAttempts, budgets, fixture.codeCalls.Load(), fixture.viewCalls.Load(), err)
	}
}

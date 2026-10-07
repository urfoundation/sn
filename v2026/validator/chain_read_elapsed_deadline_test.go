//go:build linux || darwin

// Pending cancellation is driven explicitly at the real geth request and
// body-close boundaries; original hashes and transport causes stay intact.
package validator

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Exercise the ordinary scalar or singleton batch path without hiding an
// unexpected partial scalar output when the same call returns an error.
func chainReadElapsedTestCall(ctx context.Context, chain *ChainClient, batch bool) ([][]byte, error) {
	if batch {
		return chain.batchCallsAtHashContext(ctx, 123, chainBatchTestBlockHash, []chainBatchCall{{address: common.Address{1}, calldata: []byte{7}}})
	}
	output, err := chain.ethCallAtHashContext(ctx, common.Address{1}, []byte{7}, 123, chainBatchTestBlockHash)
	if output == nil {
		return nil, err
	}
	return [][]byte{output}, err
}

// A complete synthetic response retains every actual request id and calldata.
func chainReadElapsedTestResponse(t *testing.T, request *http.Request) *http.Response {
	t.Helper()
	requests, batch := chainReadRetryRequests(t, request)
	if !batch {
		return chainReadRetryResponse(t, http.StatusOK, chainReadRetryReply(t, requests[0]))
	}
	replies := make([]map[string]any, len(requests))
	for index, call := range requests {
		replies[index] = chainReadRetryReply(t, call)
	}
	return chainReadRetryResponse(t, http.StatusOK, replies)
}

// The same earlier caller deadline refuses both initial admission and a retry
// after pacing, retaining the last service failure without another request.
func TestChainReadElapsedOwnerStopsAdmissionAndRetry(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, before := range []bool{false, true} {
			parent, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			owner := &evidenceReadPendingDeadlineTestContext{Context: parent, deadline: time.Now().Add(time.Minute)}
			if before {
				owner.deadline = time.Now()
			}
			calls, closes, waits := 0, 0, 0
			chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
				chainReadRetryRequests(t, request)
				calls++
				if calls > 1 || calls != closes+1 {
					t.Fatal("expired chain owner acquired another request")
				}
				response := chainReadRetryResponse(t, http.StatusServiceUnavailable, "synthetic capacity")
				response.Body = &chainHTTPTestBody{reader: response.Body, afterClose: func() { closes++ }}
				return response, nil
			})
			chain.readRetryHooks.wait = func(context.Context, time.Duration) error {
				waits++
				owner.deadline = time.Now()
				return nil
			}
			outputs, err := chainReadElapsedTestCall(owner, chain, batch)
			want := 1
			if before {
				want = 0
			}
			if outputs != nil || !errors.Is(err, context.DeadlineExceeded) || owner.Err() != nil || calls != want || closes != want || waits != want {
				t.Fatalf("batch=%t before=%t owner boundary failed: calls=%d closes=%d waits=%d outputs=%x error=%v", batch, before, calls, closes, waits, outputs, err)
			}
			if !before {
				var status gethrpc.HTTPError
				if !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable {
					t.Fatalf("original service failure disappeared: %v", err)
				}
			}
			cancel()
		}
	}
}

// An already elapsed attempt with Err still nil cannot reach transport. Its
// cancellation completes before a fresh attempt uses the same live operation.
func TestChainReadElapsedAttemptStopsTransport(t *testing.T) {
	for _, batch := range []bool{false, true} {
		attempts, calls, closes, waits, budgets := 0, 0, 0, 0, 0
		var previous context.Context
		chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
			calls++
			if attempts != 2 || evidenceReadContextError(request.Context()) != nil {
				t.Fatal("an elapsed attempt reached the physical transport")
			}
			response := chainReadElapsedTestResponse(t, request)
			response.Body = &chainHTTPTestBody{reader: response.Body, afterClose: func() { closes++ }}
			return response, nil
		})
		chain.readRetryHooks.withTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			budgets++
			if duration != 300*time.Second {
				t.Fatal("operation duration changed")
			}
			return context.WithTimeout(parent, duration)
		}
		chain.readRetryHooks.withAttemptTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			attempts++
			if duration != 60*time.Second || previous != nil && previous.Err() != context.Canceled {
				t.Fatal("attempt duration or prior cancellation changed")
			}
			ctx, cancel := context.WithCancel(parent)
			attempt := &evidenceReadPendingDeadlineTestContext{Context: ctx, deadline: time.Now().Add(duration)}
			if attempts == 1 {
				attempt.deadline = time.Now()
			}
			previous = attempt
			return attempt, cancel
		}
		chain.readRetryHooks.wait = func(context.Context, time.Duration) error {
			waits++
			if previous.Err() != context.Canceled {
				t.Fatal("retry pacing preceded attempt cancellation")
			}
			return nil
		}
		outputs, err := chainReadElapsedTestCall(t.Context(), chain, batch)
		if err != nil || len(outputs) != 1 || !bytes.Equal(outputs[0], []byte{7}) || attempts != 2 || calls != 1 || closes != 1 || waits != 1 || budgets != 1 {
			t.Fatalf("batch=%t fresh attempt failed: attempts=%d calls=%d closes=%d waits=%d budgets=%d outputs=%x error=%v", batch, attempts, calls, closes, waits, budgets, outputs, err)
		}
	}
}

// Completed success cannot escape an elapsed owner; a concurrent permanent
// status or physical close failure must still remain in the returned causes.
func TestChainReadElapsedOwnerRejectsCompletedResponses(t *testing.T) {
	broken := errors.New("synthetic chain close failure")
	for _, batch := range []bool{false, true} {
		for _, scenario := range []string{"success", "conflict", "close"} {
			parent, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			owner := &evidenceReadPendingDeadlineTestContext{Context: parent, deadline: time.Now().Add(time.Minute)}
			calls, closes, waits := 0, 0, 0
			chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
				calls++
				response := chainReadElapsedTestResponse(t, request)
				if scenario == "conflict" {
					response.StatusCode = http.StatusConflict
				}
				body := &chainHTTPTestBody{reader: response.Body, afterClose: func() { closes++; owner.deadline = time.Now() }}
				if scenario == "close" {
					body.closeErr = broken
				}
				response.Body = body
				return response, nil
			})
			chain.readRetryHooks.wait = func(context.Context, time.Duration) error { waits++; return errors.New("unexpected wait after expiry") }
			outputs, err := chainReadElapsedTestCall(owner, chain, batch)
			if outputs != nil || !errors.Is(err, context.DeadlineExceeded) || owner.Err() != nil || calls != 1 || closes != 1 || waits != 0 || scenario == "close" && !errors.Is(err, broken) {
				t.Fatalf("batch=%t %s lost completion custody: calls=%d closes=%d waits=%d outputs=%x error=%v", batch, scenario, calls, closes, waits, outputs, err)
			}
			if scenario == "conflict" {
				var status gethrpc.HTTPError
				if !errors.As(err, &status) || status.StatusCode != http.StatusConflict {
					t.Fatalf("completed conflict disappeared behind expiry: %v", err)
				}
			}
			if scenario != "success" && RetryableEvidenceTransportError(err) {
				t.Fatalf("hard response borrowed deadline retry authority: %v", err)
			}
			cancel()
		}
	}
}

// The attempt's own elapsed deadline also preserves hard completed results
// while its enclosing operation remains live and could otherwise retry.
func TestChainReadElapsedAttemptKeepsHardClose(t *testing.T) {
	broken := errors.New("synthetic attempt close failure")
	for _, batch := range []bool{false, true} {
		var attempt *evidenceReadPendingDeadlineTestContext
		calls, closes, waits := 0, 0, 0
		chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
			calls++
			response := chainReadElapsedTestResponse(t, request)
			response.Body = &chainHTTPTestBody{reader: response.Body, closeErr: broken, afterClose: func() {
				closes++
				attempt.deadline = time.Now()
				if attempt.Err() != nil {
					t.Fatal("test did not retain pending cancellation")
				}
			}}
			return response, nil
		})
		chain.readRetryHooks.withAttemptTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(parent)
			attempt = &evidenceReadPendingDeadlineTestContext{Context: ctx, deadline: time.Now().Add(duration)}
			return attempt, cancel
		}
		chain.readRetryHooks.wait = func(context.Context, time.Duration) error {
			waits++
			return errors.New("unexpected hard failure retry")
		}
		outputs, err := chainReadElapsedTestCall(t.Context(), chain, batch)
		if outputs != nil || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, broken) || RetryableEvidenceTransportError(err) || calls != 1 || closes != 1 || waits != 0 || attempt.Err() != context.Canceled {
			t.Fatalf("batch=%t attempt expiry hid a hard cause: calls=%d closes=%d waits=%d outputs=%x error=%v", batch, calls, closes, waits, outputs, err)
		}
	}
}

// An elapsed owner cannot suppress a missing response's contradictory sibling;
// batch inspection must finish before the final deadline refusal is returned.
func TestChainReadElapsedBatchKeepsMixedFailure(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner := &evidenceReadPendingDeadlineTestContext{Context: parent, deadline: time.Now().Add(time.Minute)}
	calls, closes, waits := 0, 0, 0
	chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
		calls++
		requests, batch := chainReadRetryRequests(t, request)
		if !batch || len(requests) != 2 {
			t.Fatal("mixed response lost original batch")
		}
		reply := chainReadRetryReply(t, requests[1])
		delete(reply, "result")
		reply["error"] = map[string]any{"code": -32000, "message": "synthetic canonical hash conflict"}
		response := chainReadRetryResponse(t, http.StatusOK, []map[string]any{reply})
		response.Body = &chainHTTPTestBody{reader: response.Body, afterClose: func() { closes++; owner.deadline = time.Now() }}
		return response, nil
	})
	chain.readRetryHooks.wait = func(context.Context, time.Duration) error {
		waits++
		return errors.New("unexpected mixed response retry")
	}
	outputs, err := chain.batchCallsAtHashContext(owner, 123, chainBatchTestBlockHash, []chainBatchCall{{address: common.Address{1}, calldata: []byte{1}}, {address: common.Address{1}, calldata: []byte{2}}})
	var semantic gethrpc.Error
	if outputs != nil || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, gethrpc.ErrMissingBatchResponse) || !errors.As(err, &semantic) || semantic.ErrorCode() != -32000 || RetryableEvidenceTransportError(err) || owner.Err() != nil || calls != 1 || closes != 1 || waits != 0 {
		t.Fatalf("deadline concealed the completed batch conflict: calls=%d closes=%d waits=%d outputs=%x error=%v", calls, closes, waits, outputs, err)
	}
}

// A completed first child that exhausts the original owner cannot authorize
// its sibling; already successful initial members are never requested again.
func TestChainReadElapsedBatchStopsChildAdmission(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner := &evidenceReadPendingDeadlineTestContext{Context: parent, deadline: time.Now().Add(time.Minute)}
	calls, closes, waits, budgets := 0, 0, 0, 0
	chain := chainReadRetryClient(t, func(request *http.Request) (*http.Response, error) {
		calls++
		requests, batch := chainReadRetryRequests(t, request)
		if !batch || calls > 2 || calls != closes+1 {
			t.Fatal("batch acquired an expired child or overlapped body custody")
		}
		if calls == 1 && len(requests) != 3 || calls == 2 && len(requests) != 1 {
			t.Fatal("retry changed the selected failed group")
		}
		reply := chainReadRetryReply(t, requests[0])
		want := "0x01"
		if calls == 2 {
			want = "0x02"
		}
		if reply["result"] != want {
			t.Fatal("batch repeated a successful member or reordered its failed group")
		}
		response := chainReadRetryResponse(t, http.StatusOK, []map[string]any{reply})
		response.Body = &chainHTTPTestBody{reader: response.Body, afterClose: func() {
			closes++
			if calls == 2 {
				owner.deadline = time.Now()
			}
		}}
		return response, nil
	})
	chain.readRetryHooks.withTimeout = func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		budgets++
		if duration != 300*time.Second {
			t.Fatal("split changed the operation allowance")
		}
		return context.WithTimeout(ctx, duration)
	}
	chain.readRetryHooks.wait = func(context.Context, time.Duration) error { waits++; return nil }
	outputs, err := chain.batchCallsAtHashContext(owner, 123, chainBatchTestBlockHash, []chainBatchCall{{address: common.Address{1}, calldata: []byte{1}}, {address: common.Address{1}, calldata: []byte{2}}, {address: common.Address{1}, calldata: []byte{3}}})
	if outputs != nil || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, gethrpc.ErrMissingBatchResponse) || owner.Err() != nil || calls != 2 || closes != 2 || waits != 1 || budgets != 1 {
		t.Fatalf("split escaped its original owner: calls=%d closes=%d waits=%d budgets=%d outputs=%x error=%v", calls, closes, waits, budgets, outputs, err)
	}
}

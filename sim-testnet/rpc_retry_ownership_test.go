package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
)

func TestFinalSemanticRPCNestedClientsKeepOneRetryOwner(t *testing.T) {
	t.Parallel()
	for _, recoverRead := range []bool{false, true} {
		base := &scriptedFinalSemanticSubstrateClient{errors: []error{io.ErrUnexpectedEOF}, value: "authenticated"}
		if !recoverRead {
			base.errors = []error{io.ErrUnexpectedEOF, io.ErrUnexpectedEOF, io.ErrUnexpectedEOF, io.ErrUnexpectedEOF}
		}
		outerPolicy, innerPolicy := immediateFinalSemanticRetryPolicy(), immediateFinalSemanticRetryPolicy()
		outerWaits, innerWaits := 0, 0
		outerPolicy.wait = func(context.Context, time.Duration) error { outerWaits++; return nil }
		innerPolicy.wait = func(context.Context, time.Duration) error { innerWaits++; return nil }
		client := &resilientFinalSemanticSubstrateClient{base: base, policy: innerPolicy}
		value := ""
		err := retryFinalSemanticRPCCall(t.Context(), nil, outerPolicy, func(ctx context.Context) error {
			return client.CallContext(ctx, &value, "chain_getHeader", "0x1234")
		})
		wantCalls := 4
		if recoverRead {
			wantCalls = 2
			if err != nil || value != "authenticated" {
				t.Fatalf("nested recovery failed: value=%q error=%v", value, err)
			}
		} else if !errors.Is(err, io.ErrUnexpectedEOF) || value != "" {
			t.Fatalf("nested exhaustion was lost: value=%q error=%v", value, err)
		}
		if base.callCount() != wantCalls || outerWaits != wantCalls-1 || innerWaits != 0 {
			t.Fatalf("nested budget multiplied: calls=%d outer waits=%d inner waits=%d", base.callCount(), outerWaits, innerWaits)
		}
	}
}

func TestFinalSemanticRPCManagedAttemptKeepsContextAndGate(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithTimeout(t.Context(), time.Hour)
	defer cancel()
	ctx := context.WithValue(parent, ownedEvmRpcRetryBudgetKey{}, true)
	gate := &rpcRequestGate{interval: time.Nanosecond}
	calls := 0
	err := retryFinalSemanticRPCCall(ctx, gate, immediateFinalSemanticRetryPolicy(), func(actual context.Context) error {
		calls++
		if actual != ctx {
			t.Fatal("managed inner call replaced its owner's attempt context")
		}
		return nil
	})
	if err != nil || calls != 1 || gate.next.IsZero() {
		t.Fatalf("managed call bypassed admission: calls=%d gate=%v error=%v", calls, gate.next, err)
	}
}

// An adapter can return nil after its context expires. Neither read wrapper
// may turn that late callback into a successful authenticated observation.
func TestRPCReadRetryOwnersRejectExpiredSuccess(t *testing.T) {
	t.Parallel()
	for _, evm := range []bool{false, true} {
		policy := immediateFinalSemanticRetryPolicy()
		policy.maximumAttempts = 1
		policy.attemptTimeout = time.Nanosecond
		call := func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}
		var err error
		if evm {
			err = retryEvmReadRpcCall(t.Context(), "late read", policy, call)
		} else {
			err = retryFinalSemanticRPCCall(t.Context(), nil, policy, call)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("evm=%t accepted expired success: %v", evm, err)
		}
	}
}

func TestFinalSemanticRPCRejectsCanceledSuccess(t *testing.T) {
	t.Parallel()
	for _, managed := range []bool{false, true} {
		parent, cancel := context.WithCancel(t.Context())
		ctx := context.Context(parent)
		if managed {
			ctx = context.WithValue(parent, ownedEvmRpcRetryBudgetKey{}, true)
		}
		calls := 0
		err := retryFinalSemanticRPCCall(ctx, nil, immediateFinalSemanticRetryPolicy(), func(context.Context) error {
			calls++
			cancel()
			return nil
		})
		cancel()
		if !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("managed=%t accepted canceled success: calls=%d error=%v", managed, calls, err)
		}
	}
}

func TestFinalSemanticRPCRetryKeepsIntegrityAndProviderCodesStrict(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{
		errors.New("signed evidence digest mismatch after RPC timeout"),
		errors.Join(context.DeadlineExceeded, errors.New("canonical block hash mismatch")),
		fmt.Errorf("wrapped: %w", errors.Join(io.ErrUnexpectedEOF, errors.New("signature mismatch"))),
		&os.PathError{Op: "read", Path: "checkpoint.json", Err: io.ErrUnexpectedEOF},
		finalSemanticTestRPCError{code: -32602, message: "request timed out"},
		finalSemanticTestRPCError{code: 3, message: "Upstream overloaded"},
		finalSemanticTestRPCError{code: -32005, message: "execution reverted: timeout"},
		&gethrpc.HTTPError{StatusCode: http.StatusUnauthorized, Status: "timeout"},
		&websocket.CloseError{Code: websocket.ClosePolicyViolation, Text: "timeout"},
	} {
		calls := 0
		err := retryFinalSemanticRPCCall(t.Context(), nil, immediateFinalSemanticRetryPolicy(), func(context.Context) error {
			calls++
			return failure
		})
		if !errors.Is(err, failure) || calls != 1 {
			t.Fatalf("permanent failure retried: %v calls=%d returned=%v", failure, calls, err)
		}
	}
	for _, failure := range []error{
		fmt.Errorf("wrapped: %w", errors.New("Historical work rate limit exceeded")),
		errors.Join(context.DeadlineExceeded, io.ErrUnexpectedEOF),
		&websocket.CloseError{Code: websocket.CloseAbnormalClosure, Text: "unexpected EOF"},
		&gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable},
	} {
		if !finalSemanticRPCErrorIsTransient(failure) {
			t.Fatalf("known transient no longer retries: %v", failure)
		}
	}
}

func TestOwnedEvmSubmissionAttemptIsBoundedWithoutReplay(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"eth_sendRawTransaction", "unknown_method"} {
		calls := 0
		transport := newOwnedEvmRetryTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			deadline, ok := request.Context().Deadline()
			if !ok || time.Until(deadline) > ownedEVMHTTPTimeout {
				t.Fatal("single-send request has no bounded deadline")
			}
			return nil, context.DeadlineExceeded
		}))
		transport.policy.wait = func(context.Context, time.Duration) error { t.Fatal("submission replayed"); return nil }
		request := ownedEvmRetryTestRequest(t, fmt.Sprintf(`{"method":%q,"id":1}`, method))
		if _, err := transport.RoundTrip(request); !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
			t.Fatalf("unknown submission outcome was replayed: calls=%d error=%v", calls, err)
		}
	}
}

func TestOwnedEvmSubmissionDeadlineOwnsResponseBody(t *testing.T) {
	t.Parallel()
	body := &ownedEvmRetryTestBody{Reader: http.NoBody}
	var attempt context.Context
	transport := newOwnedEvmRetryTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempt = request.Context()
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	}))
	response, err := transport.RoundTrip(ownedEvmRetryTestRequest(t, `{"method":"eth_sendRawTransaction","id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, bounded := attempt.Deadline(); !bounded || attempt.Err() != nil {
		t.Fatalf("response body lost its active attempt: bounded=%t error=%v", bounded, attempt.Err())
	}
	if err := response.Body.Close(); err != nil || !body.closed || !errors.Is(attempt.Err(), context.Canceled) {
		t.Fatalf("response close did not release attempt: body closed=%t context=%v error=%v", body.closed, attempt.Err(), err)
	}
}

func TestOwnedEvmSubmissionRejectsCanceledSuccess(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &ownedEvmRetryTestBody{Reader: http.NoBody}
	calls := 0
	transport := newOwnedEvmRetryTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	}))
	request := ownedEvmRetryTestRequest(t, `{"method":"eth_sendRawTransaction","id":1}`).WithContext(ctx)
	if response, err := transport.RoundTrip(request); !errors.Is(err, context.Canceled) || response != nil || calls != 1 || !body.closed {
		t.Fatalf("canceled submission accepted: calls=%d response=%v body closed=%t error=%v", calls, response, body.closed, err)
	}
}

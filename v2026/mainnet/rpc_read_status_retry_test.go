// Server failures exercise the real read owner without sleeping or contacting
// an external route. Physical failures cannot erase known terminal evidence.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A server failure, including interrupted read or close, repeats only the exact
// read after closing the response within the same five-minute operation owner.
func TestRpcReadServerFailureRecoversPinnedRead(t *testing.T) {
	for _, fault := range []string{"status", "read", "close", "timeout"} {
		client, err := newRpcClient("http://rpc.example", 300*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
		ownerDeadline, _ := ctx.Deadline()
		calls, waits := 0, 0
		var closes atomic.Int32
		var firstRequest []byte
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			raw, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			if request.Method != http.MethodPost || request.URL.String() != "http://rpc.example" {
				t.Errorf("%s: read changed method or route", fault)
			}
			if deadline, ok := request.Context().Deadline(); !ok || deadline.After(ownerDeadline) || time.Until(deadline) > 60*time.Second {
				t.Errorf("%s: physical request escaped its bounded owner", fault)
			}
			if calls == 1 {
				firstRequest = append([]byte(nil), raw...)
				body := &rpcReadOwnerTestBody{ReadCloser: io.NopCloser(strings.NewReader("temporarily unavailable")), closes: &closes}
				switch fault {
				case "read":
					body.readErrorAfter = 1
				case "close":
					body.closeErr = io.ErrUnexpectedEOF
				case "timeout":
					body.closeErr = context.DeadlineExceeded
				}
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: body}, nil
			}
			if !bytes.Equal(firstRequest, raw) {
				t.Errorf("%s: retry changed the pinned request", fault)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: &rpcReadOwnerTestBody{ReadCloser: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"synthetic-value"}`)), closes: &closes}}, nil
		})
		params := []any{"synthetic-storage-key", "synthetic-pinned-block"}
		var observed string
		client.retryWait = func(waitCtx context.Context, delay time.Duration) error {
			waits++
			deadline, ok := waitCtx.Deadline()
			if !ok || deadline != ownerDeadline || calls != 1 || closes.Load() != 1 || observed != "" || delay != 500*time.Millisecond {
				t.Errorf("%s: retry lost original owner, response custody or delay: calls=%d closes=%d value=%q delay=%v", fault, calls, closes.Load(), observed, delay)
			}
			params[1] = "foreign-block"
			return waitCtx.Err()
		}
		err = client.call(ctx, "state_getStorage", params, &observed)
		cancel()
		if err != nil || observed != "synthetic-value" || calls != 2 || waits != 1 || closes.Load() != 2 {
			t.Fatalf("%s: server failure did not recover original read: calls=%d waits=%d closes=%d value=%q err=%v", fault, calls, waits, closes.Load(), observed, err)
		}
	}
}

// A simultaneous hard close or terminal status never gains retry authority
// from the server-failure status classifier or a physical interruption.
func TestRpcReadServerFailurePreservesHardStatusAndClose(t *testing.T) {
	hard := errors.New("synthetic local close failure")
	for _, test := range []struct {
		status   int
		closeErr error
	}{
		{status: http.StatusInternalServerError, closeErr: hard},
		{status: http.StatusInternalServerError, closeErr: errors.Join(io.ErrUnexpectedEOF, hard)},
		{status: http.StatusForbidden, closeErr: io.ErrUnexpectedEOF},
		{status: http.StatusTemporaryRedirect, closeErr: io.ErrUnexpectedEOF},
		{status: http.StatusNotImplemented},
		{status: http.StatusNotImplemented, closeErr: io.ErrUnexpectedEOF},
		{status: http.StatusHTTPVersionNotSupported},
	} {
		client, err := newRpcClient("http://rpc.example", 300*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		calls, waits := 0, 0
		var closes atomic.Int32
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: test.status, Body: &rpcReadOwnerTestBody{ReadCloser: io.NopCloser(strings.NewReader("unavailable")), closes: &closes, closeErr: test.closeErr}}, nil
		})
		client.retryWait = func(context.Context, time.Duration) error {
			waits++
			return errors.New("unexpected retry")
		}
		var observed string
		err = client.call(t.Context(), "system_chain", []any{}, &observed)
		if err == nil || test.closeErr != nil && !errors.Is(err, test.closeErr) || test.status == http.StatusTemporaryRedirect && !errors.Is(err, errRpcIntegrity) || calls != 1 || waits != 0 || closes.Load() != 1 || observed != "" {
			t.Fatalf("status=%d close=%v: terminal cause gained retry authority: calls=%d waits=%d closes=%d value=%q err=%v", test.status, test.closeErr, calls, waits, closes.Load(), observed, err)
		}
	}
}

// Owner cancellation and exhaustion stop before another request and preserve
// transport failure without relabeling it as contradictory canonical evidence.
func TestRpcReadServerFailureExhaustionPreservesAvailabilityCause(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		client, err := newRpcClient("http://rpc.example", 300*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		calls, waits := 0, 0
		var closes atomic.Int32
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: &rpcReadOwnerTestBody{ReadCloser: io.NopCloser(strings.NewReader("unavailable")), closes: &closes, closeErr: io.ErrUnexpectedEOF}}, nil
		})
		client.retryWait = func(waitCtx context.Context, delay time.Duration) error {
			waits++
			if canceled {
				cancel()
				return waitCtx.Err()
			}
			return context.DeadlineExceeded
		}
		var observed string
		err = client.call(ctx, "system_chain", []any{}, &observed)
		cancel()
		want := context.DeadlineExceeded
		if canceled {
			want = context.Canceled
		}
		if !errors.Is(err, want) || !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, errRpcIntegrity) || calls != 1 || waits != 1 || closes.Load() != 1 || observed != "" {
			t.Fatalf("canceled=%t: retry exhaustion lost its transient cause: calls=%d waits=%d closes=%d value=%q err=%v", canceled, calls, waits, closes.Load(), observed, err)
		}
	}
}

// Both production read retry owners inspect faults from actual HTTP response
// closes. The claim owner retains the shared Core wire parser and result type.
package miner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Use real sockets and parser entry points, adding only the chosen close
// failure after physical ownership ends. No callback supplies a retry verdict.
func runMinerDnsReadHttpTest(t *testing.T, claim bool, cause error, retry, cancelAtWait bool) {
	t.Helper()
	var calls, closes atomic.Int32
	var requestLock sync.Mutex
	var first []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		count := calls.Add(1)
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if claim {
			if request.Method != http.MethodGet || request.URL.RequestURI() != "/sn/pool/claim?epoch=7" || request.Header.Get("Authorization") != "Bearer synthetic-dns-read-token" {
				t.Error("claim retry changed its original GET or credential")
			}
			_, _ = io.WriteString(writer, `{"epoch":7}`)
			return
		}
		if request.Method != http.MethodPost || request.URL.Path != "/rpc" {
			t.Error("read-only RPC changed its original method or endpoint")
		}
		func() {
			requestLock.Lock()
			defer requestLock.Unlock()
			if count == 1 {
				first = append([]byte(nil), raw...)
			} else if !bytes.Equal(first, raw) {
				t.Error("RPC retry changed original calldata or selector")
			}
		}()
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`)
	}))
	defer server.Close()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 60 * time.Second, Transport: ethRpcTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(request)
		if err == nil {
			var closeErr error
			if calls.Load() == 1 {
				closeErr = cause
			}
			response.Body = &minerReadOwnerTestBody{ReadCloser: response.Body, closes: &closes, closeErr: closeErr}
		}
		return response, err
	})}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	parentDeadline, _ := ctx.Deadline()
	waits, owners := 0, 0
	withTimeout := func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		owners++
		owner, stop := context.WithTimeout(parent, duration)
		if actual, ok := owner.Deadline(); duration != 300*time.Second || !ok || !actual.Equal(parentDeadline) {
			t.Error("read owner changed its five-minute default or borrowed earlier deadline")
		}
		return owner, stop
	}
	wait := func(owner context.Context, _ time.Duration) error {
		waits++
		if closes.Load() != calls.Load() {
			t.Error("retry started before the prior physical close finished")
		}
		if cancelAtWait {
			cancel()
		}
		return owner.Err()
	}
	var err error
	value := false
	if claim {
		read := func(owner context.Context) (*sdk.SnPoolClaimResult, error) {
			return connect.HttpGetWithRawFunction(owner, func(readCtx context.Context, origin, token string) ([]byte, error) {
				request, err := http.NewRequestWithContext(readCtx, http.MethodGet, origin, nil)
				if err != nil {
					return nil, err
				}
				request.Header.Set("Authorization", "Bearer "+token)
				response, err := client.Do(request)
				if err != nil {
					return nil, err
				}
				body, readErr := io.ReadAll(response.Body)
				return body, errors.Join(readErr, response.Body.Close())
			}, server.URL+"/sn/pool/claim?epoch=7", "synthetic-dns-read-token", &sdk.SnPoolClaimResult{}, connect.NewNoopApiCallback[*sdk.SnPoolClaimResult]())
		}
		var result *sdk.SnPoolClaimResult
		result, err = retryClaimApiRead(ctx, claimReadRetryHooks{withTimeout: withTimeout, wait: wait}, read)
		value = result != nil && result.Epoch == 7
	} else {
		var result string
		result, err = ethRpcHexResultWithRetry(ctx, client, server.URL+"/rpc", "eth_call", []any{map[string]any{"to": "synthetic-contract", "data": "0x1234"}, "finalized"}, ethRpcRetryHooks{withTimeout: withTimeout, wait: wait})
		value = result == "0x1234"
	}
	wantCalls, wantWaits := int32(1), 0
	if retry || cancelAtWait {
		wantWaits = 1
		if !cancelAtWait {
			wantCalls = 2
		}
	}
	if calls.Load() != wantCalls || closes.Load() != wantCalls || waits != wantWaits || owners != 1 {
		t.Fatalf("claim=%t cause=%T changed read ownership: calls=%d closes=%d waits=%d owners=%d", claim, cause, calls.Load(), closes.Load(), waits, owners)
	}
	if retry && !cancelAtWait {
		if err != nil || !value {
			t.Fatalf("claim=%t cause=%T lost complete DNS recovery: %v", claim, cause, err)
		}
	} else if value || err == nil || !errors.Is(err, cause) || cancelAtWait && !errors.Is(err, context.Canceled) {
		t.Fatalf("claim=%t cause=%T lost the hard physical cause or original cancellation", claim, cause)
	}
}

// Valid DNS leaves recover in both owners without renewing their deadlines.
func TestMinerReadHttpRecoversCompleteDnsCauses(t *testing.T) {
	for _, claim := range []bool{false, true} {
		for _, cause := range []error{
			&net.DNSError{Err: "synthetic timeout", Name: "timeout.example", IsTimeout: true},
			&net.DNSError{Err: "synthetic outage", Name: "temporary.example", IsTemporary: true},
			&net.DNSError{Err: "synthetic outage", Name: "temporary.example", IsTemporary: true, UnwrapErr: syscall.ECONNRESET},
			&net.DNSError{Err: "synthetic wrapped outage", Name: "reset.example", UnwrapErr: syscall.ECONNRESET},
			&net.DNSError{Err: "synthetic wrapped deadline", Name: "deadline.example", UnwrapErr: context.DeadlineExceeded},
			errors.Join(context.DeadlineExceeded, &net.DNSError{Err: "synthetic timeout", Name: "timeout.example", IsTimeout: true}),
		} {
			runMinerDnsReadHttpTest(t, claim, cause, true, false)
		}
	}
}

// Neither DNS metadata nor network siblings can hide a local link failure,
// permanent resolver result, explicit custody child or cancellation child.
func TestMinerReadHttpKeepsDnsAndLinkHardCauses(t *testing.T) {
	link := &os.LinkError{Op: "rename", Old: "synthetic-old", New: "synthetic-new", Err: context.DeadlineExceeded}
	for _, claim := range []bool{false, true} {
		for _, cause := range []error{
			link, errors.Join(context.DeadlineExceeded, link),
			&net.DNSError{Err: "synthetic absent name", Name: "absent.example", IsTimeout: true, IsNotFound: true},
			&net.DNSError{Err: "synthetic absent name", Name: "absent.example", IsTemporary: true, IsNotFound: true, UnwrapErr: syscall.ECONNRESET},
			&net.DNSError{Err: "synthetic unclassified resolver result", Name: "unknown.example"},
			&net.DNSError{Err: "synthetic absent name", Name: "absent.example", IsNotFound: true, UnwrapErr: syscall.ECONNRESET},
			&net.DNSError{Err: "synthetic wrapped custody failure", Name: "custody.example", UnwrapErr: link},
			&net.DNSError{Err: "synthetic timeout", Name: "timeout.example", IsTimeout: true, UnwrapErr: link},
			&net.DNSError{Err: "synthetic timeout", Name: "timeout.example", IsTimeout: true, UnwrapErr: &os.PathError{Op: "read", Path: "synthetic-dns-custody", Err: context.DeadlineExceeded}},
			&net.DNSError{Err: "synthetic timeout", Name: "timeout.example", IsTimeout: true, UnwrapErr: errors.Join(syscall.ECONNRESET, context.Canceled)},
		} {
			runMinerDnsReadHttpTest(t, claim, cause, false, false)
		}
	}
}

// DNS child traversal consumes the original finite graph allowance and still
// rejects absent receivers before dispatching a standard-library method.
func TestMinerReadHttpBoundsDnsChildrenWithoutFreshBudget(t *testing.T) {
	var deep error = syscall.ECONNRESET
	for range 40 {
		deep = &net.DNSError{Err: "synthetic nested resolver cause", Name: "nested.example", IsTimeout: true, UnwrapErr: deep}
	}
	wide := make([]error, 129)
	for index := range wide {
		wide[index] = syscall.ECONNRESET
	}
	branch := &net.DNSError{Err: "synthetic resolver branch", Name: "branch.example", IsTimeout: true, UnwrapErr: &minerReadOwnerTestJoin{causes: wide[:64]}}
	branching := &minerReadOwnerTestJoin{causes: []error{branch, branch}}
	for _, claim := range []bool{false, true} {
		for _, child := range []error{deep, &minerReadOwnerTestJoin{causes: wide}, branching, (*net.DNSError)(nil), (*os.LinkError)(nil)} {
			cause := &net.DNSError{Err: "synthetic timeout", Name: "timeout.example", IsTimeout: true, UnwrapErr: child}
			runMinerDnsReadHttpTest(t, claim, cause, false, false)
		}
	}
}

// Cancellation after the first real close prevents any second request while
// retaining the DNS failure that reached the owned retry boundary.
func TestMinerReadHttpDnsCancellationRetainsOriginalCause(t *testing.T) {
	for _, claim := range []bool{false, true} {
		cause := &net.DNSError{Err: "synthetic timeout", Name: "timeout.example", IsTimeout: true}
		runMinerDnsReadHttpTest(t, claim, cause, true, true)
	}
}

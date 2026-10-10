// Physical resolver failures exercise the configured native HTTP adapter and
// public canonical reader. Only the dial cause and retry clock are replaced.
package crv4

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Capture each actual request before its physical dial, including requests
// whose resolver refusal prevents a response from ever reaching the decoder.
type substrateReadDnsRequest struct {
	call        chainContextRPCRequest
	url         string
	readMarked  bool
	deadline    time.Time
	hasDeadline bool
}

// The immutable dial fault runs on the standard transport's dial goroutine.
// All observable connection, request and physical-close facts use stateLock.
type substrateReadDnsFixture struct {
	stateLock sync.Mutex
	client    *contextSubstrateClient
	chain     *Chain
	server    *httptest.Server
	transport *http.Transport
	fault     func(int) error
	requests  []substrateReadDnsRequest
	dials     int
	replies   int
	closes    int
	endpoint  string
}

// A synthetic resolver name routes to the local server only after recovery;
// no external resolution, endpoint reselection or verdict injection occurs.
func newSubstrateReadDnsFixture(t *testing.T, reply types.Hash, fault func(int) error) *substrateReadDnsFixture {
	t.Helper()
	fixture := &substrateReadDnsFixture{fault: fault}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call chainContextRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Errorf("native DNS fixture request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		fixture.stateLock.Lock()
		fixture.replies++
		fixture.stateLock.Unlock()
		writeChainContextRPCResult(writer, call, reply.Hex())
	}))
	_, port, err := net.SplitHostPort(fixture.server.Listener.Addr().String())
	if err != nil {
		fixture.server.Close()
		t.Fatal(err)
	}
	fixture.endpoint = "http://rpc.example:" + port
	fixture.transport = &http.Transport{DialContext: fixture.dial, DisableKeepAlives: true}
	client, err := gsrpcgeth.DialHTTPWithClient(fixture.endpoint, &http.Client{
		Transport: &substrateReadHttpTransport{base: fixture, maximumBytes: substrateReadHttpResponseLimit},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})
	if err != nil {
		fixture.transport.CloseIdleConnections()
		fixture.server.Close()
		t.Fatal(err)
	}
	fixture.client = &contextSubstrateClient{Client: client, url: fixture.endpoint, closeReadHttp: fixture.transport.CloseIdleConnections}
	fixture.chain = &Chain{API: &gsrpc.SubstrateAPI{Client: fixture.client}}
	t.Cleanup(func() {
		fixture.client.Close()
		fixture.server.Close()
	})
	return fixture
}

// Standard HTTP still owns connection creation, framing and cancellation.
func (self *substrateReadDnsFixture) dial(ctx context.Context, network, address string) (net.Conn, error) {
	self.stateLock.Lock()
	self.dials++
	number := self.dials
	self.stateLock.Unlock()
	if self.fault != nil {
		if err := self.fault(number); err != nil {
			return nil, err
		}
	}
	if address != strings.TrimPrefix(self.endpoint, "http://") {
		return nil, errors.New("synthetic resolver changed the original endpoint")
	}
	dialer := net.Dialer{Timeout: time.Minute}
	return dialer.DialContext(ctx, network, self.server.Listener.Addr().String())
}

// Copy only the fixture request bytes, then give the identical body to the
// standard physical transport. The production adapter still owns the response.
func (self *substrateReadDnsFixture) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, readErr := io.ReadAll(request.Body)
	if err := errors.Join(readErr, request.Body.Close()); err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	var call chainContextRPCRequest
	if err := json.Unmarshal(raw, &call); err != nil {
		return nil, err
	}
	deadline, hasDeadline := request.Context().Deadline()
	marked, _ := request.Context().Value(substrateReadHttpContextKey{}).(bool)
	self.stateLock.Lock()
	self.requests = append(self.requests, substrateReadDnsRequest{call: call, url: request.URL.String(), readMarked: marked, deadline: deadline, hasDeadline: hasDeadline})
	self.stateLock.Unlock()
	response, err := self.transport.RoundTrip(request)
	if response != nil && response.Body != nil {
		response.Body = &substrateReadDnsBody{ReadCloser: response.Body, owner: self}
	}
	return response, err
}

// Preserve the physical owner's connection-discard capability.
func (self *substrateReadDnsFixture) CloseIdleConnections() { self.transport.CloseIdleConnections() }

// Observe physical close completion while retaining the real socket body.
type substrateReadDnsBody struct {
	io.ReadCloser
	owner *substrateReadDnsFixture
}

// The production adapter must close a recovered response exactly once.
func (self *substrateReadDnsBody) Close() error {
	err := self.ReadCloser.Close()
	self.owner.stateLock.Lock()
	self.owner.closes++
	self.owner.stateLock.Unlock()
	return err
}

// Snapshots are taken after the public invocation without timing assumptions.
func (self *substrateReadDnsFixture) snapshot() ([]substrateReadDnsRequest, int, int, int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]substrateReadDnsRequest(nil), self.requests...), self.dials, self.replies, self.closes
}

// The public canonical check recovers real resolver timeouts and temporary
// failures, then compares the actual returned hash to its unchanged original.
func TestSubstrateReadDnsPublicCanonicalReadRecovers(t *testing.T) {
	for _, cause := range []*net.DNSError{
		{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true},
		{Err: "synthetic resolver unavailable", Name: "rpc.example", IsTemporary: true},
		{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true, UnwrapErr: context.DeadlineExceeded},
		{Err: "synthetic wrapped resolver deadline", Name: "rpc.example", UnwrapErr: context.DeadlineExceeded},
	} {
		for _, changed := range []bool{false, true} {
			selected, reply := types.Hash{43}, types.Hash{43}
			if changed {
				reply = types.Hash{44}
			}
			fixture := newSubstrateReadDnsFixture(t, reply, func(number int) error {
				if number <= 2 {
					return cause
				}
				return nil
			})
			budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
			fixture.client.readRetry = hooks
			err := fixture.chain.CheckCanonicalBlockAtContext(t.Context(), selected, 100)
			requests, dials, replies, closes := fixture.snapshot()
			if (err != nil) != changed || len(requests) != 3 || dials != 3 || replies != 1 || closes != 1 || budget.attempts != 3 || budget.elapsed != 150*time.Second {
				t.Fatalf("DNS recovery lost its public owner or physical body: changed=%t requests=%d dials=%d replies=%d closes=%d attempts=%d elapsed=%s error=%v", changed, len(requests), dials, replies, closes, budget.attempts, budget.elapsed, err)
			}
			if changed && (!strings.Contains(err.Error(), "canonical block changed") || RetryableSubstrateReadTransportError(err) || substrateRPCDisconnected(err)) {
				t.Fatal("a recovered but different actual canonical hash became transport unavailability")
			}
			for _, request := range requests {
				if request.url != fixture.endpoint || !request.readMarked || request.call.Method != "chain_getBlockHash" || len(request.call.Params) != 1 || string(request.call.Params[0]) != "100" {
					t.Fatal("DNS retry changed the original canonical read, endpoint or physical provenance")
				}
			}
		}
	}
}

// Repeated physical resolver failures spend the original total. Parent
// cancellation and an earlier deadline stop that same owner without replay.
func TestSubstrateReadDnsPublicReadRetainsBudgetAndParent(t *testing.T) {
	for _, mode := range []string{"budget", "canceled", "earlier deadline"} {
		cause := &net.DNSError{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true}
		fixture := newSubstrateReadDnsFixture(t, types.Hash{43}, func(int) error { return cause })
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		t.Cleanup(cancel)
		deadline, _ := parent.Deadline()
		owned := &substrateReadTestDeadline{Context: parent}
		if mode != "budget" {
			hooks.wait = func(ctx context.Context, _ <-chan struct{}, _ time.Duration) error {
				if ctx != budget.owner {
					t.Fatal("DNS retry abandoned its original operation owner")
				}
				if mode == "earlier deadline" {
					owned.expired.Store(true)
				}
				cancel()
				return owned.Err()
			}
		}
		fixture.client.readRetry = hooks
		err := fixture.chain.CheckCanonicalBlockAtContext(owned, types.Hash{43}, 100)
		requests, dials, replies, closes := fixture.snapshot()
		wantCalls, wantElapsed, wantEnd := 1, time.Duration(0), error(context.Canceled)
		if mode == "budget" {
			wantCalls, wantElapsed, wantEnd = 4, 300*time.Second, context.DeadlineExceeded
		} else if mode == "earlier deadline" {
			wantEnd = context.DeadlineExceeded
		}
		if !errors.Is(err, cause) || !errors.Is(err, wantEnd) || !HasSubstrateReadTransportCause(err) || len(requests) != wantCalls || dials != wantCalls || replies != 0 || closes != 0 || budget.attempts != wantCalls || budget.elapsed != wantElapsed {
			t.Fatalf("DNS %s lost original cause or owner: requests=%d dials=%d replies=%d closes=%d attempts=%d elapsed=%s error=%v", mode, len(requests), dials, replies, closes, budget.attempts, budget.elapsed, err)
		}
		if mode == "canceled" && RetryableSubstrateReadTransportError(err) {
			t.Fatal("canceled DNS read acquired replay authority")
		}
		for _, request := range requests {
			if !request.hasDeadline || !request.deadline.Equal(deadline) || !request.readMarked || request.call.Method != "chain_getBlockHash" || len(request.call.Params) != 1 || string(request.call.Params[0]) != "100" {
				t.Fatal("DNS retry extended the earlier parent deadline or changed its canonical request")
			}
		}
	}
}

// Permanent DNS metadata and explicit hard children remain hard despite a
// timeout flag. The public reader returns the same cause after one actual dial.
func TestSubstrateReadDnsPublicReadKeepsOriginalHardCause(t *testing.T) {
	hard := errors.New("synthetic resolver authority contradiction")
	for _, cause := range []*net.DNSError{
		{Err: "synthetic permanent failure", Name: "rpc.example"},
		{Err: "synthetic absent name", Name: "rpc.example", IsNotFound: true, IsTimeout: true},
		{Err: "synthetic absent name", Name: "rpc.example", IsNotFound: true, IsTemporary: true, UnwrapErr: context.DeadlineExceeded},
		{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true, UnwrapErr: hard},
		{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true, UnwrapErr: errors.Join(context.DeadlineExceeded, hard)},
		{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true, UnwrapErr: &os.PathError{Op: "read", Path: "synthetic-resolver-config", Err: context.DeadlineExceeded}},
		{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true, UnwrapErr: &os.LinkError{Op: "rename", Old: "synthetic-resolver-old", New: "synthetic-resolver-new", Err: context.DeadlineExceeded}},
		{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true, UnwrapErr: context.Canceled},
	} {
		fixture := newSubstrateReadDnsFixture(t, types.Hash{43}, func(int) error { return cause })
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		fixture.client.readRetry = hooks
		err := fixture.chain.CheckCanonicalBlockAtContext(t.Context(), types.Hash{43}, 100)
		requests, dials, replies, closes := fixture.snapshot()
		if err == nil || !errors.Is(err, cause) || !HasSubstrateReadTransportCause(err) || RetryableSubstrateReadTransportError(err) || receiptScanUnavailable(err) || len(requests) != 1 || dials != 1 || replies != 0 || closes != 0 || budget.attempts != 1 || budget.elapsed != 0 {
			t.Fatalf("hard original DNS cause was replayed or erased: requests=%d dials=%d replies=%d closes=%d attempts=%d elapsed=%s error=%v", len(requests), dials, replies, closes, budget.attempts, budget.elapsed, err)
		}
	}
}

// A local file operation cannot borrow retry authority from an actual native
// read failure retained below it, including alongside a separate read marker.
func TestSubstrateReadHttpFileLinkCannotBorrowPhysicalMarker(t *testing.T) {
	cause := &net.DNSError{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true}
	fixture := newSubstrateReadDnsFixture(t, types.Hash{43}, func(int) error { return cause })
	budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
	fixture.client.readRetry = hooks
	original := fixture.chain.CheckCanonicalBlockAtContext(t.Context(), types.Hash{43}, 100)
	requests, dials, replies, closes := fixture.snapshot()
	if !errors.Is(original, cause) || !errors.Is(original, context.DeadlineExceeded) || !RetryableSubstrateReadTransportError(original) || len(requests) != 4 || dials != 4 || replies != 0 || closes != 0 || budget.attempts != 4 || budget.elapsed != 300*time.Second {
		t.Fatalf("file-link fixture did not obtain the actual native read failure: requests=%d dials=%d replies=%d closes=%d attempts=%d elapsed=%s error=%v", len(requests), dials, replies, closes, budget.attempts, budget.elapsed, original)
	}
	for _, local := range []error{
		&os.PathError{Op: "read", Path: "synthetic-local-state", Err: original},
		&os.LinkError{Op: "rename", Old: "synthetic-local-old", New: "synthetic-local-new", Err: original},
	} {
		for _, err := range []error{local, errors.Join(original, local), errors.Join(local, original)} {
			if !errors.Is(err, local) || !errors.Is(err, cause) || !HasSubstrateReadTransportCause(err) || RetryableSubstrateReadTransportError(err) || substrateRPCDisconnected(err) || receiptScanUnavailable(err) {
				t.Fatalf("local file cause borrowed an actual native marker: %v", err)
			}
		}
	}
}

// Raw local failures also remain hard in the receipt continuation fallback,
// which has no physical marker to route through the native HTTP classifier.
func TestSubstrateReadReceiptRejectsRawLocalFileCauses(t *testing.T) {
	for _, child := range []error{context.DeadlineExceeded, context.Canceled} {
		if !receiptScanUnavailable(child) {
			t.Fatal("pure caller interruption lost its existing partial-scan semantics")
		}
		for _, local := range []error{
			&os.PathError{Op: "read", Path: "synthetic-receipt-state", Err: child},
			&os.LinkError{Op: "rename", Old: "synthetic-receipt-old", New: "synthetic-receipt-new", Err: child},
		} {
			for _, err := range []error{local, errors.Join(child, local), errors.Join(local, child)} {
				if HasSubstrateReadTransportCause(err) || receiptScanUnavailable(err) {
					t.Fatalf("raw local file cause acquired partial receipt authority: %v", err)
				}
			}
		}
	}
}

// The concrete DNS exception grants no write or subscription retry and cannot
// manufacture the private physical-read marker outside the read allowlist.
func TestSubstrateReadDnsPublicClientDoesNotReplayWrites(t *testing.T) {
	for _, method := range []string{"author_submitExtrinsic", "author_submitAndWatchExtrinsic"} {
		cause := &net.DNSError{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true}
		fixture := newSubstrateReadDnsFixture(t, types.Hash{43}, func(int) error { return cause })
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		fixture.client.readRetry = hooks
		var result string
		err := fixture.client.CallContext(t.Context(), &result, method, "0x0102")
		requests, dials, replies, closes := fixture.snapshot()
		if err == nil || !errors.Is(err, cause) || HasSubstrateReadTransportCause(err) || RetryableSubstrateReadTransportError(err) || len(requests) != 1 || dials != 1 || replies != 0 || closes != 0 || budget.attempts != 0 || budget.owner != nil || result != "" {
			t.Fatalf("DNS failure granted write replay or read provenance: method=%s requests=%d dials=%d attempts=%d error=%v", method, len(requests), dials, budget.attempts, err)
		}
		if requests[0].readMarked || requests[0].call.Method != method || len(requests[0].call.Params) != 1 || string(requests[0].call.Params[0]) != `"0x0102"` {
			t.Fatal("write DNS failure entered or changed the native read request")
		}
	}
}

// A concrete optional-child exception cannot reset the existing shared graph
// allowance or turn a typed nil/foreign child into observed transport evidence.
func TestSubstrateReadDnsExplicitChildKeepsBoundedGraph(t *testing.T) {
	var nilDns *net.DNSError
	cycle := &net.DNSError{Err: "synthetic cyclic resolver timeout", Name: "rpc.example", IsTimeout: true}
	cycle.UnwrapErr = cycle
	wide := &substrateReadCauseNode{children: make([]error, 129)}
	for index := range wide.children {
		wide.children[index] = context.DeadlineExceeded
	}
	deep := error(context.DeadlineExceeded)
	for index := 0; index < 40; index++ {
		deep = &net.DNSError{Err: "synthetic nested resolver timeout", Name: "rpc.example", IsTimeout: true, UnwrapErr: deep}
	}
	for _, child := range []error{nilDns, cycle, wide, &substrateReadCauseMatcher{cause: context.DeadlineExceeded}, deep} {
		err := &net.DNSError{Err: "synthetic resolver timeout", Name: "rpc.example", IsTimeout: true, UnwrapErr: child}
		if substrateRPCDisconnected(err) || RetryableSubstrateReadTransportError(&substrateReadHttpTransportError{cause: err}) {
			t.Fatalf("DNS explicit child %T escaped its original bounded graph", child)
		}
	}
	if substrateRPCDisconnected(nilDns) || RetryableSubstrateReadTransportError(&substrateReadHttpTransportError{cause: nilDns}) {
		t.Fatal("typed nil DNS acquired native transport authority")
	}
}

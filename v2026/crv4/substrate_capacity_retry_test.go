package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	gsrpcstate "github.com/centrifuge/go-substrate-rpc-client/v4/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const substrateHistoricalCapacityMessage = "Historical work rate limit exceeded"

type substrateCapacityFixture struct {
	client   *contextSubstrateClient
	mu       sync.Mutex
	requests []chainContextRPCRequest
	started  chan struct{}
}

func newSubstrateCapacityFixture(t *testing.T, response func(int) string) *substrateCapacityFixture {
	t.Helper()
	f := &substrateCapacityFixture{started: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chainContextRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, request)
		count := len(f.requests)
		f.mu.Unlock()
		if count == 1 {
			close(f.started)
		}
		message := response(count)
		if message == "reset" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetLinger(0)
			}
			_ = conn.Close()
			return
		}
		if message == "stall" {
			<-r.Context().Done()
			return
		}
		if message == "malformed" {
			_, _ = w.Write([]byte("{"))
			return
		}
		if message == "transport_text" {
			http.Error(w, substrateHistoricalCapacityMessage, http.StatusBadGateway)
			return
		}
		payload := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		if message == "" {
			payload["result"] = "0x2a00"
		} else {
			payload["error"] = map[string]any{"code": -32000, "message": message}
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	// A unique path also prevents a later fixture reusing an ephemeral port from
	// inheriting the earlier fixture's deliberately retained provider cooldown.
	endpoint := server.URL + "/" + strings.ReplaceAll(t.Name(), "/", "-")
	client, err := gsrpcgeth.DialContext(context.Background(), endpoint)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	f.client = &contextSubstrateClient{Client: client, url: endpoint}
	t.Cleanup(func() { f.client.Close(); server.Close() })
	return f
}

func (f *substrateCapacityFixture) snapshot() []chainContextRPCRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chainContextRPCRequest(nil), f.requests...)
}

// This exact root compiles against unchanged pre-fix production. It proves
// that the observed typed capacity error enters a cancelable recovery wait.
func TestSubstrateReadCapacityWaitsAndCancels(t *testing.T) {
	f := newSubstrateCapacityFixture(t, func(int) string { return substrateHistoricalCapacityMessage })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var result string
		done <- f.client.CallContext(ctx, &result, "state_getStorage", "0x0102", types.Hash{46}.Hex())
	}()
	<-f.started
	select {
	case err := <-done:
		t.Fatalf("historical capacity returned without a cancelable cooldown: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("capacity waiter did not join canceled: %v", err)
	}
	if requests := f.snapshot(); len(requests) != 1 {
		t.Fatalf("canceled capacity read requests=%d want=1", len(requests))
	}
}

func TestSubstrateReadCapacityPreservesPinnedRequest(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"context", "storage_compatibility"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newSubstrateCapacityFixture(t, func(count int) string {
				if count == 1 {
					return substrateHistoricalCapacityMessage
				}
				return ""
			})
			block := types.Hash{47}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			started := time.Now()
			if mode == "context" {
				var result string
				if err := f.client.CallContext(ctx, &result, "state_getStorage", "0x0102", block.Hex()); err != nil || result != "0x2a00" {
					t.Fatalf("capacity read did not recover: result=%s err=%v", result, err)
				}
			} else {
				var result types.U16
				ok, err := gsrpcstate.NewState(f.client).GetStorage(types.StorageKey{1, 2}, &result, block)
				if err != nil || !ok || result != 42 {
					t.Fatalf("capacity storage did not recover: ok=%t result=%d err=%v", ok, result, err)
				}
			}
			if elapsed := time.Since(started); elapsed < time.Minute {
				t.Fatalf("capacity cooldown shortened to %s", elapsed)
			}
			requests := f.snapshot()
			if len(requests) != 2 {
				t.Fatalf("requests=%d want=2", len(requests))
			}
			for _, request := range requests {
				if request.Method != "state_getStorage" || len(request.Params) != 2 || string(request.Params[0]) != `"0x0102"` || string(request.Params[1]) != fmt.Sprintf("%q", block.Hex()) {
					t.Fatalf("capacity replay changed pinned request: %+v", request)
				}
			}
		})
	}
}

func TestSubstrateReadCapacityKeepsRetryAndWriteBounds(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name, method, message string
		calls                 int
	}{
		{"exhaustion", "state_getStorage", substrateHistoricalCapacityMessage, 5},
		{"near_match", "state_getStorage", substrateHistoricalCapacityMessage + ": invalid proof", 1},
		{"permanent_history", "state_getStorage", "Historical state unavailable", 1},
		{"malformed", "state_getStorage", "malformed", 1},
		{"untyped_transport_message", "state_getStorage", "transport_text", 1},
		{"unknown_method", "state_changeStorage", substrateHistoricalCapacityMessage, 1},
		{"submit", "author_submitExtrinsic", substrateHistoricalCapacityMessage, 1},
		{"watch_call", "author_submitAndWatchExtrinsic", substrateHistoricalCapacityMessage, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			f := newSubstrateCapacityFixture(t, func(int) string { return fixture.message })
			var budget *substrateReadTestBudget
			if fixture.name == "exhaustion" {
				var hooks substrateRpcReadRetryHooks
				budget, hooks = newSubstrateReadTestBudget(t, 0)
				f.client.readRetry = hooks
			}
			ctx, cancel := context.WithTimeout(context.Background(), 230*time.Second)
			defer cancel()
			var result string
			err := f.client.CallContext(ctx, &result, fixture.method, "0x0102", types.Hash{48}.Hex())
			if err == nil || (budget == nil && errors.Is(err, context.DeadlineExceeded)) {
				t.Fatalf("capacity boundary returned %v", err)
			}
			if budget != nil && (!errors.Is(err, context.DeadlineExceeded) || budget.elapsed != 300*time.Second) {
				t.Fatalf("capacity exhaustion lost its full budget: elapsed=%s err=%v", budget.elapsed, err)
			}
			if requests := f.snapshot(); len(requests) != fixture.calls {
				t.Fatalf("capacity attempts=%d want=%d error=%v", len(requests), fixture.calls, err)
			}
		})
	}
}

func TestSubstrateReadCapacitySharesWaitAcrossClientsAndCloses(t *testing.T) {
	t.Parallel()
	f := newSubstrateCapacityFixture(t, func(int) string { return substrateHistoricalCapacityMessage })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() {
		var result string
		first <- f.client.CallContext(ctx, &result, "state_getStorage", "0x0102", types.Hash{49}.Hex())
	}()
	<-f.started
	select {
	case err := <-first:
		t.Fatalf("first capacity read did not wait: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	client, err := gsrpcgeth.DialContext(context.Background(), f.client.URL())
	if err != nil {
		cancel()
		<-first
		t.Fatal(err)
	}
	second := &contextSubstrateClient{Client: client, url: f.client.URL()}
	defer second.Close()
	others := make(chan error, 8)
	othersCtx, othersCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer othersCancel()
	for index := 0; index < 8; index++ {
		go func(index int) {
			var result string
			others <- second.CallContext(othersCtx, &result, "state_getStorage", fmt.Sprintf("0x%02x", index), types.Hash{49}.Hex())
		}(index)
	}
	time.Sleep(100 * time.Millisecond)
	requests := f.snapshot()
	// Closing the second client must wake all eight owners without clearing the
	// endpoint's retained cooldown or closing the first client's connection.
	second.Close()
	closedAt := time.Now()
	for index := 0; index < 8; index++ {
		if err := <-others; !errors.Is(err, gsrpcgeth.ErrClientQuit) {
			t.Errorf("shared capacity reader closed with %v", err)
		}
	}
	if time.Since(closedAt) > 2*time.Second {
		t.Error("shared capacity owners did not join close promptly")
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Errorf("first capacity reader did not retain caller ownership: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("shared endpoint cooldown admitted %d requests want1", len(requests))
	}
}

func TestSubstrateReadCapacityAndDisconnectShareReadBudget(t *testing.T) {
	t.Parallel()
	f := newSubstrateCapacityFixture(t, func(count int) string {
		if count == 7 {
			return ""
		}
		if count%2 == 1 {
			return "reset"
		}
		return substrateHistoricalCapacityMessage
	})
	budget, hooks := newSubstrateReadTestBudget(t, 15*time.Second)
	f.client.readRetry = hooks
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var result string
	err := f.client.CallContext(ctx, &result, "state_getStorage", "0x0102", types.Hash{50}.Hex())
	if err != nil || result != "0x2a00" || budget.elapsed != 225*time.Second {
		t.Fatalf("capacity/disconnect recovery lost its single budget: elapsed=%s result=%s err=%v", budget.elapsed, result, err)
	}
	if requests := f.snapshot(); len(requests) != 7 {
		t.Fatalf("mixed capacity/disconnect attempts=%d want7", len(requests))
	}
}

func TestSubstrateReadCapacityDoesNotReplenishOperationBudget(t *testing.T) {
	t.Parallel()
	budget, hooks := newSubstrateReadTestBudget(t, 0)
	f := newSubstrateCapacityFixture(t, func(count int) string {
		if count == 1 {
			_ = budget.advance(15 * time.Second)
			return substrateHistoricalCapacityMessage
		}
		_ = budget.advance(225 * time.Second)
		return "stall"
	})
	f.client.readRetry = hooks
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	var result string
	err := f.client.CallContext(ctx, &result, "state_getStorage", "0x0102", types.Hash{51}.Hex())
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatalf("network budget error=%v caller=%v", err, ctx.Err())
	}
	if budget.elapsed != 300*time.Second || budget.attempts != 2 {
		t.Fatalf("capacity wait replenished its operation budget: elapsed=%s attempts=%d", budget.elapsed, budget.attempts)
	}
	if requests := f.snapshot(); len(requests) != 2 {
		t.Fatalf("network-budget attempts=%d want2", len(requests))
	}
}

func TestContextSubstrateClientRecognizesOnlyPrivateIPv4AsOwnedRoute(t *testing.T) {
	for _, check := range []struct {
		url  string
		want bool
	}{
		{url: "ws://192.168.1.162:9944", want: true},
		{url: "ws://10.0.0.1:9944", want: true},
		{url: "ws://127.0.0.1:9944", want: false},
		{url: "wss://test.finney.opentensor.ai", want: false},
	} {
		client := &contextSubstrateClient{url: check.url}
		if got := client.ownedPrivateEndpoint(); got != check.want {
			t.Fatalf("owned private route %q=%t, want %t", check.url, got, check.want)
		}
	}
}

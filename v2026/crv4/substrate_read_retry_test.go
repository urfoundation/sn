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
	"github.com/gorilla/websocket"
)

type substrateReconnectFixture struct {
	client      *contextSubstrateClient
	mu          sync.Mutex
	requests    []chainContextRPCRequest
	connections []*websocket.Conn
	release     chan struct{}
	joined      sync.WaitGroup
}

// Every local websocket, including deliberately reset peers, has one joined owner.
func newSubstrateReconnectFixture(t *testing.T, action func(int, int, chainContextRPCRequest) string) *substrateReconnectFixture {
	t.Helper()
	f := &substrateReconnectFixture{release: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		f.joined.Add(1)
		defer f.joined.Done()
		defer conn.Close()
		f.mu.Lock()
		f.connections = append(f.connections, conn)
		number := len(f.connections)
		f.mu.Unlock()
		for {
			var request chainContextRPCRequest
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			f.mu.Lock()
			f.requests = append(f.requests, request)
			count := len(f.requests)
			f.mu.Unlock()
			switch action(number, count, request) {
			case "drop":
				if tcp, ok := conn.UnderlyingConn().(*net.TCPConn); ok {
					_ = tcp.SetLinger(0)
				}
				return
			case "hold":
				continue
			case "stall":
				<-f.release
				return
			case "error":
				_ = conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "connection reset by peer"}})
			default:
				_ = conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": "0x2a00"})
			}
		}
	}))
	t.Cleanup(func() {
		close(f.release)
		if f.client != nil {
			f.client.Close()
		}
		f.mu.Lock()
		for _, conn := range f.connections {
			_ = conn.Close()
		}
		f.mu.Unlock()
		server.Close()
		f.joined.Wait()
	})
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := gsrpcgeth.DialContext(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	f.client = &contextSubstrateClient{Client: client, url: url}
	return f
}

func (f *substrateReconnectFixture) snapshot() ([]chainContextRPCRequest, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chainContextRPCRequest(nil), f.requests...), len(f.connections)
}

func TestSubstrateReadReconnectPreservesPinnedRequest(t *testing.T) {
	for _, mode := range []string{"context", "storage_compatibility"} {
		t.Run(mode, func(t *testing.T) {
			f := newSubstrateReconnectFixture(t, func(_ int, count int, _ chainContextRPCRequest) string {
				if count == 1 {
					return "drop"
				}
				return "success"
			})
			block := types.Hash{42}
			if mode == "context" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				var result string
				if err := f.client.CallContext(ctx, &result, "state_getStorage", "0x0102", block.Hex()); err != nil {
					t.Fatalf("read did not recover from peer reset: %v", err)
				}
				if result != "0x2a00" {
					t.Fatalf("result=%s", result)
				}
			} else {
				var result types.U16
				ok, err := gsrpcstate.NewState(f.client).GetStorage(types.StorageKey{1, 2}, &result, block)
				if err != nil || !ok || result != 42 {
					t.Fatalf("storage read did not recover from peer reset: ok=%t result=%d err=%v", ok, result, err)
				}
			}
			requests, connections := f.snapshot()
			if len(requests) != 2 || connections != 2 {
				t.Fatalf("requests=%d connections=%d", len(requests), connections)
			}
			for _, request := range requests {
				if request.Method != "state_getStorage" || len(request.Params) != 2 || string(request.Params[0]) != `"0x0102"` || string(request.Params[1]) != fmt.Sprintf("%q", block.Hex()) {
					t.Fatalf("replay changed pinned request: %+v", request)
				}
			}
		})
	}
}

func TestSubstrateReadReconnectSharesClientAcrossEightReaders(t *testing.T) {
	f := newSubstrateReconnectFixture(t, func(connection, count int, _ chainContextRPCRequest) string {
		if connection == 1 {
			if count == 8 {
				return "drop"
			}
			return "hold"
		}
		return "success"
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 8)
	for index := 0; index < 8; index++ {
		go func(index int) {
			var result string
			err := f.client.CallContext(ctx, &result, "state_getStorage", fmt.Sprintf("0x%02x", index), types.Hash{43}.Hex())
			if err == nil && result != "0x2a00" {
				err = fmt.Errorf("result=%s", result)
			}
			done <- err
		}(index)
	}
	for index := 0; index < 8; index++ {
		if err := <-done; err != nil {
			t.Errorf("reader failed: %v", err)
		}
	}
	requests, connections := f.snapshot()
	if connections != 2 || len(requests) != 16 {
		t.Fatalf("connections=%d requests=%d", connections, len(requests))
	}
	counts := map[string]int{}
	for _, request := range requests {
		if request.Method != "state_getStorage" || len(request.Params) != 2 || string(request.Params[1]) != fmt.Sprintf("%q", types.Hash{43}.Hex()) {
			t.Fatalf("changed request=%+v", request)
		}
		counts[string(request.Params[0])]++
	}
	for index := 0; index < 8; index++ {
		if counts[fmt.Sprintf(`"0x%02x"`, index)] != 2 {
			t.Fatalf("read identities=%v", counts)
		}
	}
}

func TestSubstrateReadReconnectKeepsRetryAndWriteBounds(t *testing.T) {
	for _, test := range []struct {
		name, method, action string
		attempts             int
		subscribe            bool
	}{
		{"exhaustion", "state_getStorage", "drop", 4, false},
		{"application_error", "state_getStorage", "error", 1, false},
		{"unknown_method", "state_changeStorage", "drop", 1, false},
		{"submit", "author_submitExtrinsic", "drop", 1, false},
		{"watch_call", "author_submitAndWatchExtrinsic", "drop", 1, false},
		{"watch_subscription", "author_submitAndWatchExtrinsic", "drop", 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newSubstrateReconnectFixture(t, func(_ int, _ int, _ chainContextRPCRequest) string { return test.action })
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var result string
			var err error
			if test.subscribe {
				_, err = f.client.Subscribe(ctx, "author", "submitAndWatchExtrinsic", "unwatchExtrinsic", "extrinsicUpdate", make(chan json.RawMessage), "0x0102")
			} else {
				err = f.client.CallContext(ctx, &result, test.method, "0x0102", types.Hash{44}.Hex())
			}
			if err == nil {
				t.Fatal("failed request was accepted")
			}
			requests, _ := f.snapshot()
			if len(requests) != test.attempts {
				t.Fatalf("attempts=%d want=%d err=%v", len(requests), test.attempts, err)
			}
		})
	}
}

func TestSubstrateReadReconnectCancellationAndCloseJoin(t *testing.T) {
	for _, mode := range []string{"cancel_retry", "cancel_inflight", "close_inflight"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			f := newSubstrateReconnectFixture(t, func(_ int, count int, _ chainContextRPCRequest) string {
				if count == 1 {
					close(started)
				}
				if mode == "cancel_retry" {
					return "drop"
				}
				return "stall"
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var result string
				done <- f.client.CallContext(ctx, &result, "state_getStorage", "0x0102", types.Hash{45}.Hex())
			}()
			<-started
			want := error(context.Canceled)
			if mode == "close_inflight" {
				f.client.Close()
				want = gsrpcgeth.ErrClientQuit
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatalf("error=%v want=%v", err, want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("read owner did not join after cancellation/close")
			}
			requests, connections := f.snapshot()
			if len(requests) != 1 || connections != 1 {
				t.Fatalf("canceled/closed client replayed: requests=%d connections=%d", len(requests), connections)
			}
		})
	}
}

// Real WebSocket closure interrupts late production runtime reads. The local
// HTTP fixture supplies the original encoded chain, metadata and header bytes.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/gorilla/websocket"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/evmrpc"
)

// A handler either forwards a real request, drops its socket, or waits at the
// exact late-read barrier. Cleanup owns every upgraded connection and handler.
type fleetEvmRuntimeSocketFixture struct {
	backend     *fleetMainnetTestFixture
	server      *httptest.Server
	connections atomic.Int64
	headers     atomic.Int64
	canonical   atomic.Int64
	networks    atomic.Int64
	chainIds    atomic.Int64
	drop        atomic.Bool
	dropHead    atomic.Bool
	changed     func()
	block       atomic.Bool
	entered     chan struct{}
	release     chan struct{}
	joined      chan struct{}
	releaseOnce sync.Once
	mu          sync.Mutex
	closed      bool
	sockets     map[*websocket.Conn]struct{}
	workers     sync.WaitGroup
}

// The bridge changes transport timing only; it never constructs a runtime
// artifact, admission result, signature, or replacement chain response.
func newFleetEvmRuntimeSocketFixture(t *testing.T) *fleetEvmRuntimeSocketFixture {
	t.Helper()
	self := &fleetEvmRuntimeSocketFixture{backend: newFleetMainnetTestFixture(t), sockets: map[*websocket.Conn]struct{}{}, entered: make(chan struct{}), release: make(chan struct{}), joined: make(chan struct{})}
	upgrader := websocket.Upgrader{}
	self.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		self.mu.Lock()
		if self.closed {
			self.mu.Unlock()
			connection.Close()
			return
		}
		self.sockets[connection] = struct{}{}
		self.workers.Add(1)
		self.mu.Unlock()
		defer self.workers.Done()
		defer connection.Close()
		defer func() { self.mu.Lock(); delete(self.sockets, connection); self.mu.Unlock() }()
		self.connections.Add(1)
		for {
			_, raw, err := connection.ReadMessage()
			if err != nil {
				return
			}
			var input struct {
				Id     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				t.Error(err)
				return
			}
			if input.Method == "system_chain" {
				self.networks.Add(1)
			}
			if input.Method == "eth_chainId" {
				self.chainIds.Add(1)
			}
			if input.Method == "chain_getBlockHash" && len(input.Params) == 1 && string(input.Params[0]) != "0" {
				if self.canonical.Add(1) == 2 && self.dropHead.CompareAndSwap(true, false) {
					if self.changed != nil {
						self.changed()
					}
					return
				}
			}
			if input.Method == "chain_getHeader" {
				if self.headers.Add(1) == 2 && self.drop.CompareAndSwap(true, false) {
					if self.changed != nil {
						self.changed()
					}
					return
				}
				if self.block.CompareAndSwap(true, false) {
					close(self.entered)
					<-self.release
					close(self.joined)
				}
			}
			if input.Method == "state_getRuntimeVersion" || input.Method == "state_getStorageHash" || input.Method == "state_getMetadata" || input.Method == "chain_getHeader" {
				var block string
				if len(input.Params) == 0 || json.Unmarshal(input.Params[len(input.Params)-1], &block) != nil || block != self.backend.head.Hex() {
					t.Errorf("EVM runtime read escaped original native block: %s %s", input.Method, input.Params)
					connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": input.Id, "error": map[string]any{"code": -32000, "message": "runtime query changed its original selected block"}})
					return
				}
			}
			forward, err := http.NewRequestWithContext(request.Context(), http.MethodPost, self.backend.server.URL, bytes.NewReader(raw))
			if err != nil {
				t.Error(err)
				return
			}
			response, err := http.DefaultClient.Do(forward)
			if err != nil {
				t.Error(err)
				return
			}
			encoded, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
			if err = errors.Join(err, response.Body.Close()); err != nil {
				t.Error(err)
				return
			}
			if err := connection.WriteMessage(websocket.TextMessage, encoded); err != nil {
				return
			}
		}
	}))
	t.Cleanup(func() {
		self.releaseOnce.Do(func() { close(self.release) })
		self.server.Close()
		self.mu.Lock()
		self.closed = true
		for connection := range self.sockets {
			connection.Close()
		}
		self.mu.Unlock()
		self.workers.Wait()
	})
	return self
}

// Endpoint ownership stays with the fixture; reconnect uses the same route.
func (self *fleetEvmRuntimeSocketFixture) endpoint() string {
	return "ws" + strings.TrimPrefix(self.server.URL, "http")
}

// A real disconnect during the extra receipt-header read formerly published
// a view assembled across sockets. The first native block must remain fixed.
func TestFleetEvmRuntimeActualWebsocketLateReadRepeatsOriginalView(t *testing.T) {
	f := newFleetEvmRuntimeSocketFixture(t)
	client, err := evmrpc.DialContext(t.Context(), f.endpoint())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	before, tracked := evmrpc.RuntimeTransportGeneration(client.Client())
	f.changed = func() { f.backend.stateLock.Lock(); f.backend.finalizedNumber = 101; f.backend.stateLock.Unlock() }
	f.drop.Store(true)
	err = f.backend.authority.admitEvmPurpose(t.Context(), client, big.NewInt(100), crv4.FleetFrontierRead)
	after, current := evmrpc.RuntimeTransportGeneration(client.Client())
	if err != nil || !tracked || !current || before == 0 || after <= before || f.connections.Load() != 2 || f.networks.Load() != 2 || f.chainIds.Load() != 2 || f.headers.Load() != 5 || f.backend.count("eth_sendRawTransaction") != 0 {
		t.Fatalf("actual EVM late reconnect did not repeat original complete view: generations=%d/%d connections=%d networks=%d chains=%d headers=%d err=%v", before, after, f.connections.Load(), f.networks.Load(), f.chainIds.Load(), f.headers.Load(), err)
	}
}

// Selection from latest finality happens once. A reconnect during the final
// canonical check cannot silently retarget a later native block on retry.
func TestFleetEvmRuntimeWebsocketImplicitHeadRetainsOriginalBlock(t *testing.T) {
	f := newFleetEvmRuntimeSocketFixture(t)
	client, err := evmrpc.DialContext(t.Context(), f.endpoint())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	f.changed = func() { f.backend.stateLock.Lock(); f.backend.finalizedNumber = 101; f.backend.stateLock.Unlock() }
	f.dropHead.Store(true)
	err = f.backend.authority.admitEvmPurpose(t.Context(), client, nil, crv4.FleetFrontierRead)
	if err != nil || f.connections.Load() != 2 || f.networks.Load() != 2 || f.chainIds.Load() != 2 || f.backend.count("chain_getFinalizedHead") != 1 || f.backend.count("eth_sendRawTransaction") != 0 {
		t.Fatalf("EVM reconnect retargeted first finalized runtime block: connections=%d networks=%d finality=%d err=%v", f.connections.Load(), f.networks.Load(), f.backend.count("chain_getFinalizedHead"), err)
	}
}

// A replacement's valid artifact cannot borrow the first socket's genesis.
func TestFleetEvmRuntimeActualWebsocketReplacementRejectsForeignNetwork(t *testing.T) {
	f := newFleetEvmRuntimeSocketFixture(t)
	client, err := evmrpc.DialContext(t.Context(), f.endpoint())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	f.changed = func() { f.backend.stateLock.Lock(); f.backend.genesis = types.Hash{0xfa}; f.backend.stateLock.Unlock() }
	f.drop.Store(true)
	err = f.backend.authority.admitEvmPurpose(t.Context(), client, big.NewInt(100), crv4.FleetFrontierRead)
	if err == nil || f.connections.Load() != 2 || f.networks.Load() != 2 || f.chainIds.Load() != 2 || f.backend.count("eth_sendRawTransaction") != 0 {
		t.Fatalf("EVM replacement borrowed original network identity: connections=%d networks=%d chains=%d err=%v", f.connections.Load(), f.networks.Load(), f.chainIds.Load(), err)
	}
}

// Cancellation joins the request owner while the borrowed socket and a peer
// retain independent lifetimes. The explicit handler barrier also joins.
func TestFleetEvmRuntimeWebsocketRecoveryCancellationLeavesPeerIndependent(t *testing.T) {
	f := newFleetEvmRuntimeSocketFixture(t)
	client, err := evmrpc.DialContext(t.Context(), f.endpoint())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	peer, err := evmrpc.DialContext(t.Context(), f.backend.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(peer.Close)
	f.changed = func() { f.block.Store(true) }
	f.drop.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- f.backend.authority.admitEvmPurpose(ctx, client, big.NewInt(100), crv4.FleetFrontierRead)
	}()
	select {
	case <-f.entered:
	case err := <-done:
		t.Fatalf("EVM read did not own the replacement barrier: %v", err)
	}
	peerErr := f.backend.authority.admitEvmPurpose(t.Context(), peer, big.NewInt(100), crv4.FleetFrontierRead)
	cancel()
	err = <-done
	f.releaseOnce.Do(func() { close(f.release) })
	<-f.joined
	if !errors.Is(err, context.Canceled) || peerErr != nil || f.connections.Load() != 2 || f.backend.count("eth_sendRawTransaction") != 0 {
		t.Fatalf("EVM recovery cancellation poisoned peer or failed to join: peer=%v err=%v", peerErr, err)
	}
	if err := f.backend.authority.admitEvmPurpose(t.Context(), client, big.NewInt(100), crv4.FleetFrontierRead); err != nil {
		t.Fatalf("one canceled view closed its borrowed socket: %v", err)
	}
}

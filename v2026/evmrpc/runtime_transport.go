// Native runtime views borrowed from geth retain the actual socket lifetime.
// HTTP remains a stateless, immutable route with its existing response fence.
package evmrpc

import (
	"context"
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"weak"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
)

// The registry never keeps a client or socket alive. Geth owns reconnect and
// close; its last reachable client also owns this small observation counter.
var runtimeTransports sync.Map

// A zero counter is terminal saturation, never an earlier valid generation.
type runtimeTransport struct {
	generation atomic.Uint64
	failed     atomic.Uint64
	websocket  bool
}

// Socket creation and one terminal event each invalidate prior observations.
func (self *runtimeTransport) advance() uint64 {
	for {
		before := self.generation.Load()
		if before == 0 {
			return 0
		}
		if self.generation.CompareAndSwap(before, before+1) {
			return before + 1
		}
	}
}

// Weak identity prevents address reuse from inheriting another client's epoch.
func retainRuntimeTransport(client *rpc.Client, owner *runtimeTransport) {
	key := weak.Make(client)
	runtimeTransports.Store(key, owner)
	runtime.AddCleanup(client, func(key weak.Pointer[rpc.Client]) { runtimeTransports.Delete(key) }, key)
}

// Production native adapters require this independently installed dial owner.
// The bool distinguishes unsupported/custom clients from a saturated counter.
func RuntimeTransportGeneration(client *rpc.Client) (uint64, bool) {
	if client == nil {
		return 0, false
	}
	value, ok := runtimeTransports.Load(weak.Make(client))
	runtime.KeepAlive(client)
	if !ok {
		return 0, false
	}
	return value.(*runtimeTransport).generation.Load(), true
}

// Only the first terminal event for this actual connection changes its owner.
type runtimeConn struct {
	net.Conn
	owner *runtimeTransport
	once  sync.Once
}

// A physical failure remains distinct from a decoder or application error.
func (self *runtimeConn) failed() {
	self.once.Do(func() { self.owner.failed.Store(self.owner.advance()) })
}

func (self *runtimeConn) Read(value []byte) (int, error) {
	n, err := self.Conn.Read(value)
	if err != nil {
		self.failed()
	}
	return n, err
}

func (self *runtimeConn) Write(value []byte) (int, error) {
	n, err := self.Conn.Write(value)
	if err != nil {
		self.failed()
	}
	return n, err
}

// Explicit closure revokes proofs without making a caller-requested close a
// retryable physical read error. Repeated close never consumes another epoch.
func (self *runtimeConn) Close() error {
	self.once.Do(func() { self.owner.advance() })
	return self.Conn.Close()
}

// Geth's original buffering/proxy/TLS behavior stays with Gorilla. Wrapping
// the underlying connection observes TLS and ordinary WebSocket closure alike.
func runtimeWebsocketDialer(owner *runtimeTransport) websocket.Dialer {
	return websocket.Dialer{
		ReadBufferSize: 1024, WriteBufferSize: 1024, WriteBufferPool: &sync.Pool{}, Proxy: http.ProxyFromEnvironment,
		NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			owner.advance()
			return &runtimeConn{Conn: connection, owner: owner}, nil
		},
	}
}

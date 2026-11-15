// Transport qualification uses actual geth/WebSocket lifetimes and the HTTP
// response boundary, without substituting a decoded admission result.
package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"weak"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
)

// The first physical read disappears before any response; geth owns its next
// connection and the allowlisted caller repeats only that original read.
func TestEvmRuntimeGenerationTracksActualSocketReadRecovery(t *testing.T) {
	var connections, calls atomic.Int64
	upgrader := websocket.Upgrader{}
	joined := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { connection.Close(); joined <- struct{}{} }()
		ordinal := connections.Add(1)
		for {
			var input struct {
				Id json.RawMessage `json:"id"`
			}
			if err := connection.ReadJSON(&input); err != nil {
				return
			}
			calls.Add(1)
			if ordinal == 1 {
				return
			}
			if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": input.Id, "result": "synthetic-network"}); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client, err := DialContext(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	before, tracked := RuntimeTransportGeneration(client.Client())
	var value string
	err = CallRuntimeReadContext(t.Context(), client.Client(), &value, "system_chain")
	after, current := RuntimeTransportGeneration(client.Client())
	if err != nil || !tracked || !current || before == 0 || after <= before || value != "synthetic-network" || connections.Load() != 2 || calls.Load() != 2 {
		t.Fatalf("actual geth socket replacement lost read ownership: generations=%d/%d connections=%d calls=%d value=%q err=%v", before, after, connections.Load(), calls.Load(), value, err)
	}
	if err := CallRuntimeReadContext(t.Context(), client.Client(), &value, "eth_sendRawTransaction", "0x01"); err == nil || calls.Load() != 2 {
		t.Fatal("runtime read owner sent or replayed a signed method")
	}
	client.Close()
	<-joined
	<-joined
	closed, _ := RuntimeTransportGeneration(client.Client())
	if closed <= after {
		t.Fatal("explicit socket close retained the earlier runtime generation")
	}
}

// A decoded service-restart frame ends the WebSocket while its underlying
// TCP read still succeeded. The same read must recover through geth's owner.
func TestEvmRuntimePeerCloseFrameRepeatsOriginalRead(t *testing.T) {
	var connections, calls atomic.Int64
	upgrader := websocket.Upgrader{}
	joined := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { connection.Close(); joined <- struct{}{} }()
		ordinal := connections.Add(1)
		for {
			var input struct {
				Id json.RawMessage `json:"id"`
			}
			if err := connection.ReadJSON(&input); err != nil {
				return
			}
			calls.Add(1)
			if ordinal == 1 {
				if err := connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseServiceRestart, "synthetic-restart"), time.Now().Add(10*time.Second)); err != nil {
					t.Error(err)
					return
				}
				// Receive the peer acknowledgement before closing the TCP socket;
				// the decoded frame, not an injected EOF, ends the first read.
				connection.ReadMessage()
				return
			}
			if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": input.Id, "result": "synthetic-network"}); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client, err := DialContext(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	before, _ := RuntimeTransportGeneration(client.Client())
	var value string
	err = CallRuntimeReadContext(t.Context(), client.Client(), &value, "system_chain")
	after, _ := RuntimeTransportGeneration(client.Client())
	client.Close()
	<-joined
	if err != nil || value != "synthetic-network" || connections.Load() != 2 || calls.Load() != 2 || after <= before {
		t.Fatalf("peer close frame did not repeat the owned runtime read: connections=%d calls=%d generations=%d/%d value=%q err=%v", connections.Load(), calls.Load(), before, after, value, err)
	}
	<-joined
}

// A caller-requested close releases an outstanding read without dialing or
// giving a peer-close retry policy authority over this independent lifecycle.
func TestEvmRuntimeExplicitCloseCannotReconnect(t *testing.T) {
	var connections, calls atomic.Int64
	upgrader := websocket.Upgrader{}
	entered := make(chan struct{})
	joined := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { connection.Close(); close(joined) }()
		connections.Add(1)
		if _, _, err := connection.ReadMessage(); err != nil {
			return
		}
		calls.Add(1)
		close(entered)
		connection.ReadMessage()
	}))
	defer server.Close()
	client, err := DialContext(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		var value string
		done <- CallRuntimeReadContext(t.Context(), client.Client(), &value, "system_chain")
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("owned read did not reach explicit close barrier: %v", err)
	}
	client.Close()
	err = <-done
	<-joined
	if !errors.Is(err, rpc.ErrClientQuit) || connections.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("explicit client close acquired reconnect authority: connections=%d calls=%d err=%v", connections.Load(), calls.Load(), err)
	}
}

// Every HTTP request keeps the same stateless route epoch. A response parser
// failure remains hard and neither lookup nor generation creates a retry.
func TestEvmRuntimeHttpGenerationKeepsStatelessReadProfile(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		var input struct {
			Id json.RawMessage `json:"id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": input.Id, "result": "synthetic-network"})
	}))
	defer server.Close()
	client, err := DialContext(t.Context(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	before, tracked := RuntimeTransportGeneration(client.Client())
	var value string
	if err := CallRuntimeReadContext(t.Context(), client.Client(), &value, "system_chain"); err != nil {
		t.Fatal(err)
	}
	var wrong uint64
	if err := CallRuntimeReadContext(t.Context(), client.Client(), &wrong, "system_chain"); err == nil {
		t.Fatal("malformed decoded runtime response was accepted")
	}
	after, current := RuntimeTransportGeneration(client.Client())
	if !tracked || !current || before != 1 || after != before || calls.Load() != 2 {
		t.Fatalf("HTTP route acquired socket generations or decoder retries: before=%d after=%d calls=%d", before, after, calls.Load())
	}
}

// Saturation is permanent, and each connection contributes at most one
// terminal event even when read failure is followed by repeated owner close.
func TestEvmRuntimeGenerationSaturatesAndClosesOnce(t *testing.T) {
	owner := &runtimeTransport{}
	owner.generation.Store(math.MaxUint64)
	if owner.advance() != 0 || owner.advance() != 0 || owner.generation.Load() != 0 {
		t.Fatal("exhausted EVM transport counter reissued earlier authority")
	}
	left, right := net.Pipe()
	defer left.Close()
	owner = &runtimeTransport{}
	owner.generation.Store(1)
	connection := &runtimeConn{Conn: right, owner: owner}
	left.Close()
	if _, err := connection.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("physical read did not observe peer closure: %v", err)
	}
	connection.Close()
	connection.Close()
	if owner.generation.Load() != 2 || owner.failed.Load() != 2 {
		t.Fatal("one closed socket consumed repeated generations or lost physical origin")
	}
}

// Closing the real client releases every transport reference. Registry keys
// must neither retain that client nor remain after its registered cleanup.
func TestEvmRuntimeWeakRegistryReleasesClosedClient(t *testing.T) {
	makeClient := func() weak.Pointer[rpc.Client] {
		client, err := DialContext(t.Context(), "http://unused-runtime-read.invalid")
		if err != nil {
			t.Fatal(err)
		}
		key := weak.Make(client.Client())
		if _, present := runtimeTransports.Load(key); !present {
			t.Fatal("dial did not retain a weak runtime owner")
		}
		client.Close()
		return key
	}
	key := makeClient()
	deadline := time.Now().Add(10 * time.Second)
	for {
		runtime.GC()
		runtime.Gosched()
		_, retained := runtimeTransports.Load(key)
		if key.Value() == nil && !retained {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("weak runtime registry retained a closed geth client or its owner")
		}
	}
}

// A physically replaced connection does not reinterpret joined integrity,
// cancellation, malformed error graphs, or local custody as transport loss.
func TestEvmRuntimeReadRefusesMixedAndUnboundedTransportCauses(t *testing.T) {
	physical := &websocket.CloseError{Code: websocket.CloseAbnormalClosure}
	if !runtimeReadDisconnected(physical) || runtimeReadDisconnected(errors.Join(physical, errors.New("identity differs"))) || runtimeReadDisconnected(errors.Join(physical, context.Canceled)) {
		t.Fatal("EVM runtime read lost pure physical versus hard/canceled causes")
	}
	err := error(physical)
	for range 33 {
		err = errors.Join(err)
	}
	var absent *websocket.CloseError
	if runtimeReadDisconnected(err) || runtimeReadDisconnected(absent) {
		t.Fatal("unbounded or typed-nil EVM error graph acquired retry authority")
	}
}

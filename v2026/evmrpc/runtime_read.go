// Only allowlisted runtime reads may consume an actual socket disconnect.
// A successful retry still changes generation, invalidating the enclosing view.
package evmrpc

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"syscall"
	"time"
	"weak"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
)

// The pinned geth reconnect marker uses Go's immutable errors.New value.
// Restrict diagnostic access to that concrete type, never a custom Error hook.
var runtimeReconnectMarkerType = reflect.TypeOf(errors.New("client reconnected"))

// Unknown methods and all writes are refused before any transport call.
func runtimeReadMethod(method string) bool {
	switch method {
	case "eth_chainId", "system_chain", "chain_getFinalizedHead", "chain_getHeader", "chain_getBlockHash", "chain_getBlock",
		"state_getRuntimeVersion", "state_getStorageHash", "state_getMetadata", "state_getStorage", "state_queryStorageAt", "state_call":
		return true
	default:
		return false
	}
}

// The physical connection or geth's typed peer-close frame must independently
// report failure. Decoder errors, RPC refusals and mixed graphs remain hard.
func runtimeReadDisconnected(err error) bool {
	remaining := 128
	var visit func(error, int) bool
	visit = func(err error, depth int) bool {
		remaining--
		if err == nil || remaining < 0 || depth > 32 || err == context.Canceled || err == rpc.ErrClientQuit {
			return false
		}
		value := reflect.ValueOf(err)
		switch value.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
			if value.IsNil() {
				return false
			}
		}
		if _, ok := err.(rpc.Error); ok {
			return false
		}
		if _, ok := err.(*os.PathError); ok {
			return false
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			causes := joined.Unwrap()
			if len(causes) == 0 || len(causes) > remaining {
				return false
			}
			for _, cause := range causes {
				if !visit(cause, depth+1) {
					return false
				}
			}
			return true
		}
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			return visit(wrapped.Unwrap(), depth+1)
		}
		if closed, ok := err.(*websocket.CloseError); ok {
			switch closed.Code {
			case websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure,
				websocket.CloseInternalServerErr, websocket.CloseServiceRestart, websocket.CloseTryAgainLater:
				return true
			default:
				return false
			}
		}
		if network, ok := err.(net.Error); ok && (network.Timeout() || network.Temporary()) {
			return true
		}
		// Geth's reconnect marker is private. This exact pinned-client marker
		// is accepted only after the enclosing caller independently observed
		// this socket's physical failure; typed RPC errors were refused above.
		return err == io.EOF || err == io.ErrUnexpectedEOF || err == syscall.ECONNRESET || err == syscall.ECONNREFUSED || err == syscall.EPIPE || err == net.ErrClosed || reflect.TypeOf(err) == runtimeReconnectMarkerType && err.Error() == "client reconnected"
	}
	return visit(err, 0)
}

// The enclosing runtime observation owns the total budget; this read adds a
// finite ceiling for direct callers and a 60-second slice for each RPC attempt.
// Geth alone reconnects its existing client. No signed method enters this loop.
func CallRuntimeReadContext(ctx context.Context, client *rpc.Client, result any, method string, args ...any) error {
	if ctx == nil || client == nil || !runtimeReadMethod(method) {
		return errors.New("EVM runtime read owner or method is unavailable")
	}
	value, ok := runtimeTransports.Load(weak.Make(client))
	if !ok {
		return errors.New("EVM runtime read requires the original tracked dial owner")
	}
	owner := value.(*runtimeTransport)
	operation, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	delay := time.Second
	for {
		if err := operation.Err(); err != nil {
			return err
		}
		before := owner.generation.Load()
		if before == 0 {
			return errors.New("EVM runtime transport generation is exhausted")
		}
		attempt, stop := context.WithTimeout(operation, 60*time.Second)
		err := client.CallContext(attempt, result, method, args...)
		stop()
		if err == nil {
			return operation.Err()
		}
		// A peer's close frame is decoded above net.Conn.Read. Geth releases
		// request waiters before closing that codec, so neither a failed read
		// nor its terminal generation is guaranteed to precede this result.
		// Only geth's direct typed frame may supply this missing physical fact;
		// its code still passes the bounded classifier below. Client.Close
		// returns ErrClientQuit and never receives this recovery authority.
		_, peerClosed := err.(*websocket.CloseError)
		if operation.Err() != nil || !owner.websocket || owner.failed.Load() <= before && !peerClosed || !runtimeReadDisconnected(err) {
			return errors.Join(err, operation.Err())
		}
		timer := time.NewTimer(delay)
		select {
		case <-operation.Done():
			timer.Stop()
			return errors.Join(err, operation.Err())
		case <-timer.C:
		}
		delay = min(delay*2, 5*time.Second)
	}
}

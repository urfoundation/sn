package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/gorilla/websocket"
)

// GSRPC reconnects its shared client after a lost socket, but returns the
// disconnect to in-flight calls. Only exact observation methods may replay.
// Subscriptions and transaction submission retain the original transport path.
func substrateRPCReadMayReplay(method string) bool {
	switch method {
	case "chain_getFinalizedHead", "chain_getHeader", "chain_getBlockHash", "chain_getBlock",
		"state_getStorage", "state_getStorageHash", "state_getMetadata", "state_getRuntimeVersion",
		"state_queryStorageAt", "state_getKeys", "state_getKeysPaged", "state_call", "system_accountNextIndex":
		return true
	default:
		return false
	}
}

func substrateRPCDisconnected(err error) bool {
	var rpcError gsrpcgeth.Error
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, gsrpcgeth.ErrClientQuit) || errors.As(err, &rpcError) {
		return false
	}
	var closed *websocket.CloseError
	if errors.As(err, &closed) {
		return closed.Code == websocket.CloseAbnormalClosure || closed.Code == websocket.CloseGoingAway
	}
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, net.ErrClosed) ||
		err.Error() == "client reconnected"
}

// Route GSRPC's contextless storage helpers through the same bounded read path.
func (self *contextSubstrateClient) Call(result any, method string, args ...any) error {
	return self.CallContext(context.Background(), result, method, args...)
}

// A capacity refusal pauses every read client for the same endpoint. It does
// not reinterpret arbitrary RPC errors, submit writes, or replace the socket.
type substrateRPCCapacityGate struct {
	mu    sync.Mutex
	until time.Time
}

var substrateRPCCapacityGates sync.Map

func substrateCapacityGate(endpoint string) *substrateRPCCapacityGate {
	gate, _ := substrateRPCCapacityGates.LoadOrStore(endpoint, &substrateRPCCapacityGate{})
	return gate.(*substrateRPCCapacityGate)
}

func (gate *substrateRPCCapacityGate) refuse() {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	until := time.Now().Add(time.Minute)
	if until.After(gate.until) {
		gate.until = until
	}
}

func (gate *substrateRPCCapacityGate) wait(ctx context.Context, closed <-chan struct{}) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-closed:
			return gsrpcgeth.ErrClientQuit
		default:
		}
		gate.mu.Lock()
		delay := time.Until(gate.until)
		gate.mu.Unlock()
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-closed:
			timer.Stop()
			return gsrpcgeth.ErrClientQuit
		case <-timer.C:
		}
	}
}

type substrateRPCReadLifecycle struct {
	init   sync.Once
	close  sync.Once
	closed chan struct{}
}

func (self *contextSubstrateClient) readClosed() <-chan struct{} {
	self.readLifecycle.init.Do(func() { self.readLifecycle.closed = make(chan struct{}) })
	return self.readLifecycle.closed
}

// Wake owned cooldown waiters as well as joining the underlying client.
func (self *contextSubstrateClient) Close() {
	self.readClosed()
	self.readLifecycle.close.Do(func() { close(self.readLifecycle.closed) })
	self.Client.Close()
}

func substrateRPCHistoricalCapacity(err error) bool {
	var rpcError gsrpcgeth.Error
	return errors.As(err, &rpcError) && strings.EqualFold(strings.TrimSpace(rpcError.Error()), "Historical work rate limit exceeded")
}

func (self *contextSubstrateClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	if !substrateRPCReadMayReplay(method) {
		return self.Client.CallContext(ctx, result, method, args...)
	}
	// Capacity waits have a separate caller-bounded total. Actual network work
	// and disconnect backoff retain one cumulative thirty-second budget.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	networkRemaining := 30 * time.Second
	frozen := make([]any, len(args))
	for index, argument := range args {
		encoded, err := json.Marshal(argument)
		if err != nil {
			return err
		}
		frozen[index] = json.RawMessage(encoded)
	}
	gate, closed := substrateCapacityGate(self.url), self.readClosed()
	for attempt := 0; ; attempt++ {
		if err := gate.wait(ctx, closed); err != nil {
			return err
		}
		if networkRemaining <= 0 {
			return context.DeadlineExceeded
		}
		callCtx, stop := context.WithTimeout(ctx, networkRemaining)
		started := time.Now()
		err := self.Client.CallContext(callCtx, result, method, frozen...)
		networkRemaining -= time.Since(started)
		stop()
		if substrateRPCHistoricalCapacity(err) {
			gate.refuse()
			if attempt == 3 {
				return err
			}
			continue
		}
		if !substrateRPCDisconnected(err) || attempt == 3 {
			return err
		}
		// Reuse GSRPC's serialized reconnect. Capacity waits do not replenish
		// the remaining network/disconnect budget or the three-replay bound.
		waitCtx, stop := context.WithTimeout(ctx, networkRemaining)
		started = time.Now()
		timer := time.NewTimer(time.Second << attempt)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			stop()
			return waitCtx.Err()
		case <-closed:
			timer.Stop()
			stop()
			return gsrpcgeth.ErrClientQuit
		case <-timer.C:
		}
		networkRemaining -= time.Since(started)
		stop()
	}
}

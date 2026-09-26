package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
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
	return retryableSubstrateRpcReadTransport(err, false)
}

// Every joined cause must be transient. Decoder EOF has no retry authority
// unless the actual URL/socket boundary retained its transport origin.
func retryableSubstrateRpcReadTransport(err error, transportOrigin bool) bool {
	var rpcError gsrpcgeth.Error
	if err == nil || err == context.Canceled || err == gsrpcgeth.ErrClientQuit || errors.As(err, &rpcError) {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !retryableSubstrateRpcReadTransport(cause, transportOrigin) {
				return false
			}
		}
		return true
	}
	switch cause := err.(type) {
	case *url.Error:
		return retryableSubstrateRpcReadTransport(cause.Err, true)
	case *net.OpError:
		return retryableSubstrateRpcReadTransport(cause.Err, true)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return retryableSubstrateRpcReadTransport(wrapped.Unwrap(), transportOrigin)
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
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return transportOrigin
	}
	// GSRPC's reconnect marker is private. Only this exact allowlisted read
	// owner may interpret it; arbitrary RPC application messages cannot retry.
	return err == context.DeadlineExceeded || err == syscall.ECONNRESET || err == syscall.ECONNREFUSED ||
		err == syscall.EPIPE || err == syscall.ETIMEDOUT || err == net.ErrClosed || err.Error() == "client reconnected"
}

const substrateRpcReadRetryTimeout = 300 * time.Second
const substrateRpcReadAttemptTimeout = 60 * time.Second

// Only clock and pacing boundaries are replaceable in deterministic tests.
// Requests still use the real shared GSRPC client and frozen arguments.
type substrateRpcReadRetryHooks struct {
	withTimeout  func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait         func(context.Context, <-chan struct{}, time.Duration) error
	waitCapacity func(context.Context, <-chan struct{}, *substrateRPCCapacityGate) error
}

// Backoff belongs to the same finite read owner as the current request.
func waitSubstrateRpcRead(ctx context.Context, closed <-chan struct{}, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-closed:
		return gsrpcgeth.ErrClientQuit
	case <-timer.C:
		return nil
	}
}

// Route GSRPC's contextless storage helpers through the same bounded read path.
func (self *contextSubstrateClient) Call(result any, method string, args ...any) error {
	return self.CallContext(context.Background(), result, method, args...)
}

// An explicit private-IP endpoint is the operator-owned LAN route. It has no
// provider capacity quota, so a generic historical-capacity cooldown would
// only turn a recoverable response into minutes of idle plan time.
func (self *contextSubstrateClient) ownedPrivateEndpoint() bool {
	if self == nil {
		return false
	}
	parsed, err := url.Parse(self.url)
	if err != nil || parsed == nil {
		return false
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil && ip.IsPrivate()
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
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !substrateRPCHistoricalCapacity(cause) {
				return false
			}
		}
		return true
	}
	if rpcError, ok := err.(gsrpcgeth.Error); ok {
		return strings.EqualFold(strings.TrimSpace(rpcError.Error()), "Historical work rate limit exceeded")
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return substrateRPCHistoricalCapacity(wrapped.Unwrap())
	}
	return false
}

func (self *contextSubstrateClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	if !substrateRPCReadMayReplay(method) {
		return self.Client.CallContext(ctx, result, method, args...)
	}
	withTimeout := self.readRetry.withTimeout
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	wait := self.readRetry.wait
	if wait == nil {
		wait = waitSubstrateRpcRead
	}
	waitCapacity := self.readRetry.waitCapacity
	if waitCapacity == nil {
		waitCapacity = func(ctx context.Context, closed <-chan struct{}, gate *substrateRPCCapacityGate) error {
			return gate.wait(ctx, closed)
		}
	}
	// Expected reads retain five minutes across requests, reconnects and shared
	// provider cooldowns. A single request is bounded so a recovered endpoint
	// can be tried again before the caller-owned total expires.
	ctx, cancel := withTimeout(ctx, substrateRpcReadRetryTimeout)
	defer cancel()
	frozen := make([]any, len(args))
	for index, argument := range args {
		encoded, err := json.Marshal(argument)
		if err != nil {
			return err
		}
		frozen[index] = json.RawMessage(encoded)
	}
	owned, gate, closed := self.ownedPrivateEndpoint(), substrateCapacityGate(self.url), self.readClosed()
	var lastErr error
	delay := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(lastErr, err)
		}
		if !owned {
			if err := waitCapacity(ctx, closed, gate); err != nil {
				return errors.Join(lastErr, err)
			}
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(lastErr, err)
		}
		select {
		case <-closed:
			return errors.Join(lastErr, gsrpcgeth.ErrClientQuit)
		default:
		}
		callCtx, stop := withTimeout(ctx, substrateRpcReadAttemptTimeout)
		err := self.Client.CallContext(callCtx, result, method, frozen...)
		err = errors.Join(err, callCtx.Err())
		stop()
		if ctx.Err() != nil {
			return errors.Join(err, ctx.Err())
		}
		if err == nil {
			return nil
		}
		lastErr = err
		if substrateRPCHistoricalCapacity(err) && !owned {
			gate.refuse()
			continue
		}
		if !substrateRPCDisconnected(err) {
			return err
		}
		// Reuse GSRPC's serialized reconnect without an early attempt-count
		// cutoff. Repeated fast refusals retain the same overall deadline.
		if waitErr := wait(ctx, closed, delay); waitErr != nil {
			return errors.Join(lastErr, waitErr)
		}
		if delay < 8*time.Second {
			delay *= 2
		}
	}
}

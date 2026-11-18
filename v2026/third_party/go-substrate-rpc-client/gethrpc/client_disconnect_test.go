// Dispatch ordering tests keep writes in flight until the reader has closed
// their connection. No scheduler timing selects the failure window.
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// Each channel belongs to one codec generation. Tests supply complete reads
// and release accepted writes independently of transport closure.
type disconnectTestCodec struct {
	reads       chan disconnectTestRead
	writes      chan any
	finishWrite chan error
	closed      chan interface{}
	closeOnce   sync.Once
}

// A read is either a complete response set or a terminal transport failure.
type disconnectTestRead struct {
	messages []*jsonrpcMessage
	batch    bool
	err      error
}

// Cleanup closes both codec directions even when an assertion fails.
func newDisconnectTestCodec(t *testing.T) *disconnectTestCodec {
	t.Helper()
	codec := &disconnectTestCodec{
		reads:       make(chan disconnectTestRead),
		writes:      make(chan any, 8),
		finishWrite: make(chan error, 8),
		closed:      make(chan interface{}),
	}
	t.Cleanup(func() {
		codec.Close()
		close(codec.finishWrite)
	})
	return codec
}

// The caller owns each supplied result until the codec reader receives it.
func (self *disconnectTestCodec) Read() ([]*jsonrpcMessage, bool, error) {
	select {
	case result := <-self.reads:
		return result.messages, result.batch, result.err
	case <-self.closed:
		return nil, false, io.EOF
	}
}

// The peer accepts bytes before completion is released, as a real socket may.
func (self *disconnectTestCodec) Write(ctx context.Context, message any) error {
	self.writes <- message
	select {
	case err := <-self.finishWrite:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// The first close acknowledges that dispatch processed its terminal read.
func (self *disconnectTestCodec) Close() {
	self.closeOnce.Do(func() { close(self.closed) })
}

// This signal also drives the production reconnect decision.
func (self *disconnectTestCodec) Closed() <-chan interface{} { return self.closed }

// The synthetic endpoint never identifies a live service.
func (self *disconnectTestCodec) RemoteAddr() string { return "rpc.example" }

// The watchdog diagnoses a lost positive completion; barriers force order.
func disconnectTestReceive[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("forced dispatch transition did not complete")
		var zero T
		return zero
	}
}

// A private cancellable owner joins its client even on a pre-fix failure.
func newDisconnectTestClient(t *testing.T, codec *disconnectTestCodec) (*Client, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	client := initClient(codec, randomIDGenerator(), new(serviceRegistry))
	t.Cleanup(func() {
		cancel()
		client.Close()
	})
	return client, ctx
}

// The disconnect is consumed while the accepted write still cannot return.
func (self *disconnectTestCodec) disconnectBeforeWriteCompletes(t *testing.T, cause error) any {
	t.Helper()
	message := disconnectTestReceive(t, self.writes)
	self.reads <- disconnectTestRead{err: cause}
	disconnectTestReceive(t, self.closed)
	return message
}

// The pending response retains its owner until send completion or transfer.
func TestDisconnectRetainsPendingRequest(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	handler := newHandler(context.Background(), codec, randomIDGenerator(), new(serviceRegistry))
	op := &requestOp{ids: []json.RawMessage{json.RawMessage("1"), json.RawMessage("2")}, resp: make(chan *jsonrpcMessage, 2)}
	handler.addRequestOp(op)
	handler.close(io.ErrUnexpectedEOF, op)
	defer handler.close(ErrClientQuit, nil)
	if handler.respWait["1"] != op || handler.respWait["2"] != op {
		t.Fatal("disconnect discarded the in-flight response owner")
	}
	select {
	case <-op.resp:
		t.Fatal("disconnect canceled a request that may still transfer to a new connection")
	default:
	}
	handler.cancelAllRequests(io.ErrUnexpectedEOF, nil)
	if _, open := <-op.resp; open || !errors.Is(op.err, io.ErrUnexpectedEOF) || len(handler.respWait) != 0 {
		t.Fatal("completed send did not terminate every pending batch id exactly once")
	}
}

// Ordinary calls must observe the same accepted-write disconnect as watches.
func TestDisconnectBeforeCallWriteCompletion(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, codec)
	done := make(chan error, 1)
	go func() { done <- client.CallContext(ctx, nil, "state_getStorage", "0x0102") }()
	codec.disconnectBeforeWriteCompletes(t, io.ErrUnexpectedEOF)
	codec.finishWrite <- nil
	if err := disconnectTestReceive(t, done); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("lost original transport error: %v", err)
	}
}

// A batch has one waiter shared by several ids and must be released once.
func TestDisconnectBeforeBatchWriteCompletion(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, codec)
	done := make(chan error, 1)
	go func() {
		done <- client.BatchCallContext(ctx, []BatchElem{{Method: "state_getStorage"}, {Method: "chain_getHeader"}})
	}()
	codec.disconnectBeforeWriteCompletes(t, io.ErrUnexpectedEOF)
	codec.finishWrite <- nil
	if err := disconnectTestReceive(t, done); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("lost batch transport error: %v", err)
	}
}

// Transaction acceptance without a subscription id is an uncertain outcome,
// returned promptly without a replay or a fabricated subscription.
func TestDisconnectBeforeSubscriptionWriteCompletion(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, codec)
	done := make(chan error, 1)
	go func() {
		sub, err := client.Subscribe(ctx, "author", "submitAndWatchExtrinsic", "unwatchExtrinsic", "extrinsicUpdate", make(chan json.RawMessage), "0x0102")
		if sub != nil {
			err = errors.New("disconnect fabricated a subscription")
		}
		done <- err
	}()
	message := codec.disconnectBeforeWriteCompletes(t, io.ErrUnexpectedEOF).(*jsonrpcMessage)
	if message.Method != "author_submitAndWatchExtrinsic" || string(message.Params) != `["0x0102"]` {
		t.Fatalf("submission changed: %+v", message)
	}
	codec.finishWrite <- nil
	if err := disconnectTestReceive(t, done); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("lost subscription transport error: %v", err)
	}
	if len(codec.writes) != 0 {
		t.Fatal("uncertain submission was replayed")
	}
}

// A failed write is returned directly, even when the reader failed first.
func TestDisconnectBeforeFailedWriteCompletion(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, codec)
	done := make(chan error, 1)
	go func() { done <- client.CallContext(ctx, nil, "author_submitExtrinsic", "0x0102") }()
	codec.disconnectBeforeWriteCompletes(t, io.ErrUnexpectedEOF)
	codec.finishWrite <- io.ErrClosedPipe
	if err := disconnectTestReceive(t, done); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("lost write failure: %v", err)
	}
}

// Shutdown releases the exempt waiter even before its send has completed.
func TestDisconnectBeforeWriteCompletionThenClientClose(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, codec)
	done := make(chan error, 1)
	go func() { done <- client.CallContext(ctx, nil, "state_getStorage") }()
	codec.disconnectBeforeWriteCompletes(t, io.ErrUnexpectedEOF)
	client.Close()
	codec.finishWrite <- nil
	if err := disconnectTestReceive(t, done); !errors.Is(err, ErrClientQuit) {
		t.Fatalf("shutdown abandoned retained request: %v", err)
	}
}

// A complete response wins over a later disconnect while Write still runs.
func TestDisconnectAfterReplyBeforeWriteCompletion(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, codec)
	var result int
	done := make(chan error, 1)
	go func() { done <- client.CallContext(ctx, &result, "state_getStorage") }()
	message := disconnectTestReceive(t, codec.writes).(*jsonrpcMessage)
	codec.reads <- disconnectTestRead{messages: []*jsonrpcMessage{{Version: vsn, ID: message.ID, Result: json.RawMessage("42")}}}
	codec.reads <- disconnectTestRead{err: io.ErrUnexpectedEOF}
	disconnectTestReceive(t, codec.closed)
	codec.finishWrite <- nil
	if err := disconnectTestReceive(t, done); err != nil || result != 42 {
		t.Fatalf("completed reply was replaced by disconnect: result=%d err=%v", result, err)
	}
}

// An acknowledged subscription receives the failure on its existing stream;
// its already-completed request channel must not be closed twice.
func TestDisconnectAfterSubscriptionReplyBeforeWriteCompletion(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, codec)
	var subscription *ClientSubscription
	done := make(chan error, 1)
	go func() {
		var err error
		subscription, err = client.Subscribe(ctx, "author", "submitAndWatchExtrinsic", "unwatchExtrinsic", "extrinsicUpdate", make(chan json.RawMessage), "0x0102")
		done <- err
	}()
	message := disconnectTestReceive(t, codec.writes).(*jsonrpcMessage)
	codec.reads <- disconnectTestRead{messages: []*jsonrpcMessage{{Version: vsn, ID: message.ID, Result: json.RawMessage(`"accepted"`)}}}
	codec.reads <- disconnectTestRead{err: io.ErrUnexpectedEOF}
	disconnectTestReceive(t, codec.closed)
	codec.finishWrite <- nil
	if err := disconnectTestReceive(t, done); err != nil || subscription == nil {
		t.Fatalf("acknowledged subscription lost its owner: %v", err)
	}
	defer subscription.Unsubscribe()
	if err := disconnectTestReceive(t, subscription.Err()); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("stream lost its terminal error: %v", err)
	}
}

// Buffered batch replies precede the terminal error. Reading that error only
// after channel closure also orders its write against the consumer.
func TestDisconnectPreservesBufferedReplyBeforeTerminalError(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, codec)
	response := &jsonrpcMessage{Version: vsn, ID: json.RawMessage("1"), Result: json.RawMessage("42")}
	op := &requestOp{resp: make(chan *jsonrpcMessage, 2)}
	op.resp <- response
	op.err = io.ErrUnexpectedEOF
	close(op.resp)
	if observed, err := op.wait(ctx, client); observed != response || err != nil {
		t.Fatalf("terminal error replaced a buffered reply: reply=%v err=%v", observed, err)
	}
	if observed, err := op.wait(ctx, client); observed != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("closed response channel lost its terminal error: reply=%v err=%v", observed, err)
	}
}

// Partial batches retain completed elements while surfacing the missing tail.
func TestDisconnectAfterPartialBatchBeforeWriteCompletion(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, codec)
	var first, second int
	done := make(chan error, 1)
	go func() {
		done <- client.BatchCallContext(ctx, []BatchElem{{Method: "state_getStorage", Result: &first}, {Method: "chain_getHeader", Result: &second}})
	}()
	messages := disconnectTestReceive(t, codec.writes).([]*jsonrpcMessage)
	codec.reads <- disconnectTestRead{messages: []*jsonrpcMessage{{Version: vsn, ID: messages[0].ID, Result: json.RawMessage("42")}}}
	codec.reads <- disconnectTestRead{err: io.ErrUnexpectedEOF}
	disconnectTestReceive(t, codec.closed)
	codec.finishWrite <- nil
	if err := disconnectTestReceive(t, done); !errors.Is(err, io.ErrUnexpectedEOF) || first != 42 || second != 0 {
		t.Fatalf("partial batch lost its known prefix or unknown tail: first=%d second=%d err=%v", first, second, err)
	}
}

// Reconnect may be underway when the old reader reports a failure. The
// current request transfers once and receives only its new generation's reply.
func TestDisconnectWhileReconnectingTransfersUnsentRequest(t *testing.T) {
	old := newDisconnectTestCodec(t)
	next := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, old)
	client.writeConn = nil
	connecting, connect := make(chan struct{}), make(chan struct{})
	client.reconnectFunc = func(ctx context.Context) (ServerCodec, error) {
		close(connecting)
		select {
		case <-connect:
			return next, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	var result int
	done := make(chan error, 1)
	go func() { done <- client.CallContext(ctx, &result, "state_getStorage") }()
	disconnectTestReceive(t, connecting)
	old.reads <- disconnectTestRead{err: io.ErrUnexpectedEOF}
	disconnectTestReceive(t, old.closed)
	close(connect)
	message := disconnectTestReceive(t, next.writes).(*jsonrpcMessage)
	next.finishWrite <- nil
	next.reads <- disconnectTestRead{messages: []*jsonrpcMessage{{Version: vsn, ID: message.ID, Result: json.RawMessage("42")}}}
	if err := disconnectTestReceive(t, done); err != nil || result != 42 || len(old.writes) != 0 || len(next.writes) != 0 {
		t.Fatalf("reconnect lost or repeated unsent request: result=%d err=%v", result, err)
	}
}

// A writer can discover the dead connection before its reader reports it.
// Draining that reader must preserve the request for the replacement codec.
func TestDisconnectDiscoveredByReconnectPreservesUnsentRequest(t *testing.T) {
	old := newDisconnectTestCodec(t)
	next := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, old)
	client.writeConn = nil
	client.reconnectFunc = func(context.Context) (ServerCodec, error) { return next, nil }
	var result int
	done := make(chan error, 1)
	go func() { done <- client.CallContext(ctx, &result, "state_getStorage") }()
	message := disconnectTestReceive(t, next.writes).(*jsonrpcMessage)
	disconnectTestReceive(t, old.closed)
	next.finishWrite <- nil
	next.reads <- disconnectTestRead{messages: []*jsonrpcMessage{{Version: vsn, ID: message.ID, Result: json.RawMessage("42")}}}
	if err := disconnectTestReceive(t, done); err != nil || result != 42 {
		t.Fatalf("reader draining canceled the replacement request: result=%d err=%v", result, err)
	}
}

// A second generation has its own terminal cause; an older failure cannot
// mask a newly accepted write or authorize a second reconnect/replay.
func TestDisconnectAfterReconnectBeforeWriteCompletion(t *testing.T) {
	old := newDisconnectTestCodec(t)
	next := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, old)
	client.writeConn = nil
	client.reconnectFunc = func(context.Context) (ServerCodec, error) { return next, nil }
	done := make(chan error, 1)
	go func() { done <- client.CallContext(ctx, nil, "author_submitExtrinsic", "0x0102") }()
	next.disconnectBeforeWriteCompletes(t, io.ErrClosedPipe)
	next.finishWrite <- nil
	if err := disconnectTestReceive(t, done); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("new connection lost its own uncertain outcome: %v", err)
	}
}

// Cancellation during an accepted write still ends the caller's ownership.
func TestDisconnectBeforeWriteCompletionThenCallerCancel(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, owner := newDisconnectTestClient(t, codec)
	ctx, cancel := context.WithCancel(owner)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.CallContext(ctx, nil, "state_getStorage") }()
	codec.disconnectBeforeWriteCompletes(t, io.ErrUnexpectedEOF)
	cancel()
	if err := disconnectTestReceive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation lost: %v", err)
	}
}

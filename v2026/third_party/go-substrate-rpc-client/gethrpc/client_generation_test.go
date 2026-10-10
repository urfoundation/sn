// Actual dispatch and request transfer publish a different transport owner.
package rpc

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

// The next write is held until dispatch has closed the first codec. Both the
// disconnect and replacement must revoke every previously observed generation.
func TestTransportGenerationChangesOnActualDisconnectAndReconnect(t *testing.T) {
	previous := newDisconnectTestCodec(t)
	next := newDisconnectTestCodec(t)
	client, ctx := newDisconnectTestClient(t, previous)
	initial := client.TransportGeneration()
	if initial == 0 {
		t.Fatal("new transport did not publish its owner generation")
	}
	client.reconnectFunc = func(context.Context) (ServerCodec, error) { return next, nil }
	pending := make(chan error, 1)
	go func() { pending <- client.CallContext(ctx, nil, "state_getStorage") }()
	previous.disconnectBeforeWriteCompletes(t, io.ErrUnexpectedEOF)
	if after := client.TransportGeneration(); after <= initial {
		t.Fatal("actual disconnect retained the original transport generation")
	}
	previous.finishWrite <- nil
	if err := disconnectTestReceive(t, pending); err == nil {
		t.Fatal("disconnected read was reported complete")
	}
	disconnected := client.TransportGeneration()
	var value int
	go func() { pending <- client.CallContext(ctx, &value, "state_getStorage") }()
	message := disconnectTestReceive(t, next.writes).(*jsonrpcMessage)
	if after := client.TransportGeneration(); after <= disconnected {
		t.Fatal("actual replacement retained the disconnected transport generation")
	}
	next.finishWrite <- nil
	next.reads <- disconnectTestRead{messages: []*jsonrpcMessage{{Version: vsn, ID: message.ID, Result: json.RawMessage("42")}}}
	if err := disconnectTestReceive(t, pending); err != nil || value != 42 {
		t.Fatalf("replacement lost the unchanged request: value=%d err=%v", value, err)
	}
	client.Close()
	if client.TransportGeneration() != 0 {
		t.Fatal("closed transport retained runtime observation authority")
	}
}

// Compatibility clients copy the RPC container; they must share generation
// custody instead of copying the original counter into an immortal proof.
func TestTransportGenerationCopiesShareClosureAndCannotWrap(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	client, _ := newDisconnectTestClient(t, codec)
	copied := *client
	client.transportGeneration.Store(^uint64(0))
	client.advanceTransportGeneration()
	if copied.TransportGeneration() != 0 {
		t.Fatal("transport generation overflow reused prior authority")
	}
	client.advanceTransportGeneration()
	if client.TransportGeneration() != 0 {
		t.Fatal("closed generation became valid after another transition")
	}
}

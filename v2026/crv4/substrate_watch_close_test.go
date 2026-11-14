// Established watch shutdown is an uncertain transaction outcome, even when
// GSRPC signals clean local termination with a nil or closed error channel.
package crv4

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/gorilla/websocket"
)

// Observe the real transport's completed subscription without replacing it.
type substrateWatchTestClient struct {
	*contextSubstrateClient
	established chan *gsrpcgeth.ClientSubscription
}

// The acknowledgment barrier precedes local shutdown of the registered watch.
func (self *substrateWatchTestClient) Subscribe(ctx context.Context, namespace, subscribe, unsubscribe, notification string, channel any, args ...any) (*gsrpcgeth.ClientSubscription, error) {
	subscription, err := self.contextSubstrateClient.Subscribe(ctx, namespace, subscribe, unsubscribe, notification, channel, args...)
	if err == nil {
		self.established <- subscription
	}
	return subscription, err
}

// One fixture owns its websocket peer and production watch call.
type substrateWatchTestFixture struct {
	ctx          context.Context
	transport    *gsrpcgeth.Client
	requests     chan chainContextRPCRequest
	subscription *gsrpcgeth.ClientSubscription
	done         chan error
}

// Return only after the actual subscription has received its acknowledgment.
func newSubstrateWatchTestFixture(t *testing.T) *substrateWatchTestFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	requests := make(chan chainContextRPCRequest, 8)
	peerStopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer close(peerStopped)
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		for {
			var call chainContextRPCRequest
			if err := connection.ReadJSON(&call); err != nil {
				return
			}
			select {
			case requests <- call:
			case <-ctx.Done():
				return
			}
			var result any = true
			if call.Method == "author_submitAndWatchExtrinsic" {
				result = "synthetic-watch"
			}
			if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	transport, err := gsrpcgeth.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	fixture := &substrateWatchTestFixture{ctx: ctx, transport: transport, requests: requests, done: make(chan error, 1)}
	client := &substrateWatchTestClient{contextSubstrateClient: &contextSubstrateClient{Client: transport, url: "ws://rpc.example"}, established: make(chan *gsrpcgeth.ClientSubscription, 1)}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	finished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		transport.Close()
		<-finished
		<-peerStopped
	})
	go func() {
		defer close(finished)
		receipt, err := chain.SubmitRawAndWatchFinalized(ctx, "0x0102")
		if receipt != nil {
			err = errors.New("watch shutdown fabricated a finalized receipt")
		}
		fixture.done <- err
	}()
	select {
	case fixture.subscription = <-client.established:
	case err := <-fixture.done:
		t.Fatalf("watch ended before acknowledgment: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("subscription acknowledgment was not consumed")
	}
	request := <-requests
	if request.Method != "author_submitAndWatchExtrinsic" {
		t.Fatalf("unexpected subscription request: %+v", request)
	}
	return fixture
}

// A local transport ending provides no chain dispatch verdict or replay right.
func (self *substrateWatchTestFixture) requireUnresolved(t *testing.T) {
	t.Helper()
	select {
	case err := <-self.done:
		var dispatch *FinalizedDispatchError
		if err == nil || !strings.Contains(err.Error(), "closed before finality") || errors.As(err, &dispatch) || self.ctx.Err() != nil {
			t.Fatalf("watch shutdown was not a prompt unresolved outcome: %v", err)
		}
	case request := <-self.requests:
		t.Fatalf("watch shutdown sent another request: %+v", request)
	case <-time.After(5 * time.Second):
		t.Fatal("terminated subscription left its transaction watch blocked")
	}
}

// Client.Close emits a nil subscription error, not a remote dispatch failure.
func TestSubmitRawReturnsWhenEstablishedClientCloses(t *testing.T) {
	fixture := newSubstrateWatchTestFixture(t)
	fixture.transport.Close()
	fixture.requireUnresolved(t)
}

// An explicit unwatch closes Err without a value; repeatedly selecting that
// closed channel must not spin or fabricate transaction finality.
func TestSubmitRawReturnsWhenEstablishedSubscriptionCloses(t *testing.T) {
	fixture := newSubstrateWatchTestFixture(t)
	fixture.subscription.Unsubscribe()
	request := <-fixture.requests
	if request.Method != "author_unwatchExtrinsic" {
		t.Fatalf("unexpected unwatch: %+v", request)
	}
	fixture.requireUnresolved(t)
}

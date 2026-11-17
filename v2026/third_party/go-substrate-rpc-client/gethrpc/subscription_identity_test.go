// Real server response/notification writers must select the same client owner.
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"testing"
)

// Inspect the serialized wire owner directly, so restoring the rune conversion
// fails at the identity assertion rather than waiting for a dropped notification.
func TestSubscriptionNotifierUsesDecimalOwner(t *testing.T) {
	codec := newDisconnectTestCodec(t)
	handler := newHandler(context.Background(), codec, func() ID { return 65 }, new(serviceRegistry))
	defer handler.close(ErrClientQuit, nil)
	notifier := &Notifier{h: handler, namespace: "state", notificationMethodSuffix: "_synthetic"}
	subscription := notifier.CreateSubscription()
	notifier.activated = true
	done := make(chan error, 1)
	go func() { done <- notifier.Notify(subscription.ID, "event") }()
	message := disconnectTestReceive(t, codec.writes).(*jsonrpcMessage)
	codec.finishWrite <- nil
	if err := disconnectTestReceive(t, done); err != nil {
		t.Fatal(err)
	}
	var parameters subscriptionResult
	if err := json.Unmarshal(message.Params, &parameters); err != nil || parameters.ID != "65" || message.Method != "state_synthetic" {
		t.Fatalf("server notification changed decimal owner to rune: %s %v", message.Params, err)
	}
}

// The fork leaves generic subscription service routing disabled. Exercise the
// real Subscription/Notifier writers against Client.Subscribe over a local
// codec, without enabling that unrelated server service registration surface.
func TestSubscriptionDecimalIdentityHandshakeAndNotifications(t *testing.T) {
	for _, id := range []ID{0, 65, ^ID(0)} {
		func() {
			ctx, cancel := context.WithCancel(context.Background())
			serverSide, clientSide := net.Pipe()
			serverCodec := NewJSONCodec(serverSide)
			client := initClient(NewJSONCodec(clientSide), randomIDGenerator(), new(serviceRegistry))
			handler := newHandler(ctx, serverCodec, func() ID { return id }, new(serviceRegistry))
			var peerDone chan struct{}
			defer func() {
				cancel()
				client.Close()
				serverCodec.Close()
				if peerDone != nil {
					<-peerDone
				}
				handler.close(ErrClientQuit, nil)
			}()
			notifier := &Notifier{h: handler, namespace: "state", subscribeMethodSuffix: "_subscribeSynthetic", unsubscribeMethodSuffix: "_unsubscribeSynthetic", notificationMethodSuffix: "_synthetic"}
			subscription := notifier.CreateSubscription()
			handler.serverSubs[id] = subscription
			if err := notifier.Notify(id, "buffered"); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			peerDone = make(chan struct{})
			go func() {
				defer close(peerDone)
				finish := func() error {
					requests, batch, err := serverCodec.Read()
					if err != nil {
						return err
					}
					if batch || len(requests) != 1 || requests[0].Method != "state_subscribeSynthetic" {
						return fmt.Errorf("unexpected subscribe request: %+v", requests)
					}
					if err := serverCodec.Write(ctx, requests[0].response(notifier.takeSubscription())); err != nil {
						return err
					}
					if err := notifier.activate(); err != nil {
						return err
					}
					if err := notifier.Notify(id, "live"); err != nil {
						return err
					}
					requests, batch, err = serverCodec.Read()
					if err != nil {
						return err
					}
					if batch || len(requests) != 1 || requests[0].Method != "state_unsubscribeSynthetic" {
						return fmt.Errorf("unexpected unsubscribe request: %+v", requests)
					}
					var args []json.RawMessage
					if err := json.Unmarshal(requests[0].Params, &args); err != nil || len(args) != 1 {
						return fmt.Errorf("invalid unsubscribe params: %v", err)
					}
					var selected ID
					if err := json.Unmarshal(args[0], &selected); err != nil {
						return err
					}
					if selected != id {
						return fmt.Errorf("unsubscribe changed owner: got %d want %d", selected, id)
					}
					removed, err := handler.unsubscribe(ctx, selected)
					if err != nil || !removed {
						return fmt.Errorf("unsubscribe failed: %v", err)
					}
					return serverCodec.Write(ctx, requests[0].response(removed))
				}
				err := finish()
				if err != nil {
					cancel()
				}
				done <- err
			}()
			values := make(chan string, 2)
			clientSubscription, err := client.Subscribe(ctx, "state", "subscribeSynthetic", "unsubscribeSynthetic", "synthetic", values)
			if err != nil {
				t.Fatalf("numeric server owner %d cannot complete client handshake: %v", id, err)
			}
			if clientSubscription.subid != strconv.FormatUint(uint64(id), 10) {
				t.Fatalf("subscription owner changed: %q", clientSubscription.subid)
			}
			if first, second := disconnectTestReceive(t, values), disconnectTestReceive(t, values); first != "buffered" || second != "live" {
				t.Fatalf("notification order/owner changed: %q %q", first, second)
			}
			clientSubscription.Unsubscribe()
			if err := disconnectTestReceive(t, done); err != nil {
				t.Fatal(err)
			}
			if _, open := <-subscription.Err(); open {
				t.Fatal("server subscription owner was not closed")
			}
		}()
	}
}

func TestSubscriptionIdentityPreservesNumericAndStringUnsubscribe(t *testing.T) {
	for _, id := range []ID{0, 65, ^ID(0)} {
		decimal := strconv.FormatUint(uint64(id), 10)
		for _, wire := range []string{decimal, `"` + decimal + `"`} {
			var decoded ID
			if err := json.Unmarshal([]byte(wire), &decoded); err != nil || decoded != id {
				t.Fatalf("valid subscription id changed: %s -> %d %v", wire, decoded, err)
			}
		}
		encoded, err := json.Marshal(&Subscription{ID: id})
		if err != nil || string(encoded) != `"`+decimal+`"` {
			t.Fatalf("subscription response differs from notification id: %s %v", encoded, err)
		}
	}
}

func TestSubscriptionIdentityRejectsMalformedOwnerWithoutMutation(t *testing.T) {
	for _, wire := range []string{`null`, `true`, `-1`, `1.0`, `4294967296`, `""`, `"+1"`, `"01"`, `"-1"`, `"4294967296"`, `"0x41"`} {
		decoded := ID(73)
		if err := json.Unmarshal([]byte(wire), &decoded); err == nil || decoded != 73 {
			t.Fatalf("invalid subscription owner accepted or mutated: %s -> %d %v", wire, decoded, err)
		}
	}
}

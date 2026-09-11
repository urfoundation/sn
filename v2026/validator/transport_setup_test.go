package validator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/protocol"
)

// Cancel while the real generator awaits the server's processed ClientKey
// response. No packet client exists yet, but the created identity, OOB request
// and platform transport must still retire before PostVerify returns.
func TestTunnelTransportCancellationDuringProcessedRegistration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	claims, err := json.Marshal(map[string]string{"client_id": connect.NewId().String()})
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims) + "."
	keyPending, keyJoined, identityRemoved := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var pendingOnce, joinedOnce, removedOnce sync.Once
	var authCalls, verifyCalls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/network/auth-client":
			authCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"by_client_jwt": token})
		case "/network/remove-client":
			removedOnce.Do(func() { close(identityRemoved) })
			_, _ = io.WriteString(w, `{}`)
		case "/connect/control":
			var args connect.ConnectControlArgs
			if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
				http.Error(w, "invalid control", http.StatusBadRequest)
				return
			}
			raw, err := base64.StdEncoding.DecodeString(args.Pack)
			var pack protocol.Pack
			if err != nil || connect.ProtoUnmarshal(raw, &pack) != nil {
				http.Error(w, "invalid pack", http.StatusBadRequest)
				return
			}
			for _, frame := range pack.Frames {
				message, err := connect.FromFrame(frame)
				if err != nil {
					http.Error(w, "invalid frame", http.StatusBadRequest)
					return
				}
				if _, ok := message.(*protocol.ClientKey); ok {
					pendingOnce.Do(func() { close(keyPending) })
					select {
					case <-r.Context().Done():
					case <-ctx.Done():
					}
					joinedOnce.Do(func() { close(keyJoined) })
					return
				}
			}
			_, _ = io.WriteString(w, `{"pack":"","error":null}`)
		case "/verify":
			verifyCalls.Add(1)
			http.Error(w, "unregistered client admitted", http.StatusInternalServerError)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer endpoint.Close()
	settings := connect.DefaultClientStrategySettings()
	settings.EnableResilient = false
	strategy := connect.NewClientStrategy(ctx, settings)
	defer strategy.Close()
	transport := NewTunnelTransport(ctx, strategy, TunnelTransportConfig{ApiUrl: endpoint.URL, ConnectUrl: endpoint.URL, ByClientJwt: func() string { return token }, SourceClientId: connect.NewId()})
	defer func() {
		if err := transport.CloseAndWait(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	stepCtx, stepCancel := context.WithCancel(ctx)
	defer stepCancel()
	done := make(chan error, 1)
	go func() {
		_, err := transport.PostVerify(stepCtx, connect.NewId(), []byte(`{}`))
		done <- err
	}()
	select {
	case <-keyPending:
	case err := <-done:
		t.Fatalf("setup returned before processed registration: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stepCancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("PostVerify error = %v, want caller cancellation", err)
		}
	case <-ctx.Done():
		t.Fatal("PostVerify did not join partial setup")
	}
	for name, joined := range map[string]<-chan struct{}{"client-key request": keyJoined, "derived identity": identityRemoved} {
		select {
		case <-joined:
		case <-ctx.Done():
			t.Fatalf("%s did not retire", name)
		}
	}
	if authCalls.Load() != 1 || verifyCalls.Load() != 0 {
		t.Fatalf("created identities=%d verify requests=%d, want one identity and no request", authCalls.Load(), verifyCalls.Load())
	}
}

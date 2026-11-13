//go:build linux || darwin

// Lost upload acknowledgements retain their exact immutable retry and must not
// turn an actual Http/2 health-check failure into a production hard failure.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/startifact"
)

// The real server object publisher commits and fetches back the first body,
// then an explicit connection barrier withholds only its acknowledgement.
// A later request must receive a fresh acknowledgement for those same bytes.
func TestAttemptStreamV2HttpWriteLostPingRetainsImmutableRetry(t *testing.T) {
	testAttemptStreamV2HttpLostPing(t, false)
}

// A used shared transport retains its healthy connection while a clone owns
// only the failed upload's health callback and cancellation.
func TestAttemptStreamV2HttpWriteLostPingIsolatesWarmedTransport(t *testing.T) {
	testAttemptStreamV2HttpLostPing(t, true)
}

// Both cases cross the real stored-body/lost-acknowledgement boundary.
func testAttemptStreamV2HttpLostPing(t *testing.T, warmOriginal bool) {
	t.Helper()
	data := []byte("{\"synthetic_sequence\":1}\n")
	hash := attemptHex32(sha256.Sum256(data))
	store := server.NewLocalBlobStore(t.TempDir(), "synthetic-attempts")
	bounds := startifact.AttemptObjectBounds{MetadataBytes: 1024, RecordBytes: 1024, ProofBytes: 1024}
	var calls atomic.Int32
	var listener *releaseAPIHTTPTestListener
	healthyEntered := make(chan struct{})
	healthyRelease := make(chan struct{})
	releaseHealthy := sync.OnceFunc(func() { close(healthyRelease) })
	t.Cleanup(releaseHealthy)
	endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/warm" {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		if request.Method == http.MethodGet && request.URL.Path == "/healthy" {
			close(healthyEntered)
			select {
			case <-healthyRelease:
				_, _ = response.Write([]byte("healthy sibling"))
			case <-request.Context().Done():
				t.Error("an independent upload health check canceled the healthy connection")
			}
			return
		}
		call := calls.Add(1)
		if request.ProtoMajor != 2 || request.Method != http.MethodPost || request.URL.Path != "/sn/attempt-artifact" || request.URL.Query().Get("kind") != AttemptStreamV2Records || request.URL.Query().Get("hash") != hash || request.Header.Get("Authorization") != "Bearer synthetic-session" {
			t.Error("upload changed its transport, object or session identity")
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil || !bytes.Equal(body, data) {
			t.Errorf("actual upload body changed: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := startifact.PublishAttemptObject(request.Context(), store, bounds, AttemptStreamV2Records, hash, body); err != nil {
			t.Errorf("actual immutable publication failed: %v", err)
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		if call == 1 {
			connections := listener.accepted()
			want := 1
			if warmOriginal {
				want++
			}
			if len(connections) != want {
				t.Errorf("first upload established %d connections", len(connections))
				response.WriteHeader(http.StatusInternalServerError)
				return
			}
			// The body is durably stored before the connection drops all later
			// frames, including this acknowledgement and health-check replies.
			connections[want-1].frozen.Store(true)
		}
		response.Header().Set("ETag", `"`+hash+`"`)
		response.WriteHeader(http.StatusNoContent)
	}))
	listener = &releaseAPIHTTPTestListener{Listener: endpoint.Listener}
	endpoint.Listener, endpoint.EnableHTTP2 = listener, true
	endpoint.StartTLS()
	t.Cleanup(endpoint.Close)
	t.Cleanup(listener.closeAll)
	transport := newReleaseAPIHTTPTransport()
	transport.TLSClientConfig = endpoint.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.HTTP2.SendPingTimeout = 100 * time.Millisecond
	transport.HTTP2.PingTimeout = 100 * time.Millisecond
	lostPing := make(chan struct{})
	var pingOnce sync.Once
	transport.HTTP2.CountError = func(code string) {
		if code == "conn_close_lost_ping" {
			pingOnce.Do(func() { close(lostPing) })
		}
	}
	t.Cleanup(transport.CloseIdleConnections)
	// Initialize the shared transport before its clone, then keep that real
	// connection busy while the upload's independent connection loses its ping.
	sharedClient := &http.Client{Transport: transport}
	healthyDone := make(chan error, 1)
	if warmOriginal {
		warm, err := sharedClient.Get(endpoint.URL + "/warm")
		if err != nil {
			t.Fatal(err)
		}
		if err := warm.Body.Close(); err != nil {
			t.Fatal(err)
		}
		go func() {
			response, err := sharedClient.Get(endpoint.URL + "/healthy")
			if err == nil {
				body, readErr := io.ReadAll(response.Body)
				err = errors.Join(readErr, response.Body.Close())
				if err == nil && string(body) != "healthy sibling" {
					err = errors.New("healthy connection returned another body")
				}
			}
			healthyDone <- err
		}()
		<-healthyEntered
	}
	writer := newAttemptStreamV2HTTPTestWriter(t, endpoint.URL, func() string { return "synthetic-session" })
	writer.client.Transport = transport
	err := writer.Write(t.Context(), AttemptStreamV2Records, hash, data)
	select {
	case <-lostPing:
	default:
		t.Fatalf("upload did not reach the real failed-ping boundary: %v", err)
	}
	if err == nil || errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("lost acknowledgement was accepted, retried inline or replaced by request expiry: calls=%d error=%v", calls.Load(), err)
	}
	if !retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
		t.Errorf("actual failed-ping upload became a production hard failure: %v", err)
	}
	if !RetryableEvidenceTransportError(observeReleaseError(err)) {
		t.Errorf("actual failed-ping upload lost evidence transport ownership: %v", err)
	}
	hard := errors.New("synthetic independent immutable refusal")
	if retryableProductionSteeringRead(observeProductionSteeringRead(errors.Join(err, hard), false)) {
		t.Error("failed ping hid an independent hard refusal")
	}
	releaseHealthy()
	if warmOriginal {
		if err := <-healthyDone; err != nil {
			t.Fatalf("the warmed original transport lost its healthy request: %v", err)
		}
	}
	// The first request really committed despite the failed acknowledgement.
	var stored bytes.Buffer
	if count, readErr := startifact.ReadAttemptObjectTo(t.Context(), store, bounds, AttemptStreamV2Records, hash, &stored); readErr != nil || count != uint64(len(data)) || !bytes.Equal(stored.Bytes(), data) {
		t.Fatalf("lost acknowledgement did not follow an actual immutable commit: %v", readErr)
	}
	wantConnections := 2
	if warmOriginal {
		wantConnections++
	}
	if err := writer.Write(t.Context(), AttemptStreamV2Records, hash, data); err != nil || calls.Load() != 2 || len(listener.accepted()) != wantConnections {
		t.Fatalf("exact immutable retry did not obtain its own acknowledgement on a fresh connection: calls=%d error=%v", calls.Load(), err)
	}
	for _, cause := range []error{errors.New("http2: client connection lost"), errors.New("synthetic malformed response")} {
		if retryableProductionSteeringRead(observeProductionSteeringRead(cause, false)) {
			t.Error("unowned diagnostic text acquired transport authority")
		}
	}
	if strings.Contains(err.Error(), "synthetic-session") {
		t.Error("transport failure exposed the upload session")
	}
}

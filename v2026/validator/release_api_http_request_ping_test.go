//go:build linux || darwin

// Real Http/2 failed pings before and after headers retain read ownership,
// discard incomplete evidence and permit an independently authenticated retry.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The Do boundary must preserve the same typed health event as a body read.
func TestReleaseApiHttpRequestLostPingBeforeGetHeaders(t *testing.T) {
	testReleaseApiHttpGetLostPing(t, false)
}

// A successful header transfers cancellation and transport closure to Close;
// a lost body still cannot yield partial authenticated metadata.
func TestReleaseApiHttpRequestLostPingAfterGetHeaders(t *testing.T) {
	testReleaseApiHttpGetLostPing(t, true)
}

// The socket barrier is armed at an exact server boundary, rather than a timed
// race against the request. Only the real transport's positive ping event ends
// the failed read; the request itself keeps its ordinary long I/O allowance.
func testReleaseApiHttpGetLostPing(t *testing.T, headers bool) {
	t.Helper()
	data := []byte("{\"synthetic_complete\":true}\n")
	hash := attemptHex32(sha256.Sum256(data))
	var listener *releaseAPIHTTPTestListener
	var calls atomic.Int32
	endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.ProtoMajor != 2 || request.Method != http.MethodGet || request.URL.Path != "/sn/attempt-artifact" || request.URL.Query().Get("kind") != "metadata" || request.URL.Query().Get("hash") != hash || request.Header.Get("Authorization") != "" {
			t.Error("public read changed its transport or exact object identity")
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if calls.Add(1) == 1 {
			if headers {
				response.WriteHeader(http.StatusOK)
				response.(http.Flusher).Flush()
			}
			connections := listener.accepted()
			if len(connections) != 1 {
				t.Errorf("first read used %d connections", len(connections))
				return
			}
			connections[0].frozen.Store(true)
			return
		}
		_, _ = response.Write(data)
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
	reader := newAttemptStreamV2HTTPTestReader(t, endpoint.URL)
	reader.client.Transport = transport
	value, err := reader.ReadMetadata(t.Context(), hash, uint64(len(data)))
	select {
	case <-lostPing:
	default:
		t.Fatalf("read did not reach its real failed-ping boundary: %v", err)
	}
	if err == nil || value != nil || calls.Load() != 1 {
		t.Fatalf("failed read admitted evidence or retried inline: value=%q calls=%d error=%v", value, calls.Load(), err)
	}
	if !RetryableEvidenceTransportError(observeReleaseError(err)) || !retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
		t.Fatalf("real failed-ping read became hard: %v", err)
	}
	if errors.Is(err, context.Canceled) || t.Context().Err() != nil {
		t.Fatal("the child health event retired the shared test lifecycle")
	}
	value, err = reader.ReadMetadata(t.Context(), hash, uint64(len(data)))
	if err != nil || !bytes.Equal(value, data) || calls.Load() != 2 || len(listener.accepted()) != 2 {
		t.Fatalf("fresh authenticated retry failed: calls=%d value=%q error=%v", calls.Load(), value, err)
	}
}

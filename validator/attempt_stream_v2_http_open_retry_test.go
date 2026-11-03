//go:build linux || darwin

package validator

// One failed attempt-object open used to fail a whole measurement stage. These
// servers fail an object's first opens the way the 11-03 edge stalls did: a
// TLS handshake that never completes, a connection reset before any response,
// and a 503. The reader must try again on a fresh connection, and nothing else.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Holds the first accepted connection without ever answering it, so its TLS
// handshake can only time out. Later connections reach the server.
type attemptStreamV2StallFirstListener struct {
	net.Listener
	once    sync.Once
	stalled chan net.Conn
	accepts atomic.Int32
}

func (self *attemptStreamV2StallFirstListener) Accept() (net.Conn, error) {
	for {
		conn, err := self.Listener.Accept()
		if err != nil {
			return nil, err
		}
		self.accepts.Add(1)
		first := false
		self.once.Do(func() { first = true })
		if !first {
			return conn, nil
		}
		self.stalled <- conn
	}
}

func attemptStreamV2RetryTestHandler(t *testing.T, raw []byte, fail func(attempt int32, response http.ResponseWriter) bool) (http.Handler, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempt := requests.Add(1)
		if fail != nil && fail(attempt, response) {
			return
		}
		contentType := "application/x-ndjson"
		if request.URL.Query().Get("kind") == "metadata" {
			contentType = "application/json"
		}
		response.Header().Set("Content-Type", contentType)
		_, _ = response.Write(raw)
	}), &requests
}

func readAttemptStreamV2RetryObject(t *testing.T, reader *HTTPAttemptStreamV2Reader, raw []byte) ([]byte, error) {
	t.Helper()
	body, err := reader.OpenData(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(raw)), uint64(len(raw)))
	if err != nil {
		return nil, err
	}
	value, readErr := io.ReadAll(body)
	return value, errors.Join(readErr, body.Close())
}

// The production transport, with only its trust roots and handshake bound
// shortened for the test server.
func attemptStreamV2RetryTLSTransport(t *testing.T, server *httptest.Server, handshake time.Duration) *http.Transport {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport := newReleaseAPIHTTPTransport()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	transport.TLSHandshakeTimeout = handshake
	return transport
}

func TestAttemptStreamV2HTTPRetriesAStalledTLSHandshakeOnAFreshConnection(t *testing.T) {
	raw := []byte("{\"stalled\":false}\n")
	handler, requests := attemptStreamV2RetryTestHandler(t, raw, nil)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stall := &attemptStreamV2StallFirstListener{Listener: listener, stalled: make(chan net.Conn, 1)}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = stall
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	defer func() {
		select {
		case conn := <-stall.stalled:
			_ = conn.Close()
		default:
		}
	}()
	reader := newAttemptStreamV2HTTPTestReader(t, server.URL)
	reader.client.Transport = attemptStreamV2RetryTLSTransport(t, server, 200*time.Millisecond)
	value, err := readAttemptStreamV2RetryObject(t, reader, raw)
	if err != nil {
		t.Fatalf("one stalled TLS handshake failed the object open: %v", err)
	}
	if !bytes.Equal(value, raw) || requests.Load() != 1 || stall.accepts.Load() != 2 {
		t.Fatalf("the retry did not use a fresh connection for the exact object: value=%q requests=%d accepts=%d", value, requests.Load(), stall.accepts.Load())
	}
}

func TestAttemptStreamV2HTTPRetriesAResetOpenOnAFreshConnection(t *testing.T) {
	raw := []byte("{\"reset\":false}\n")
	var remotes sync.Map
	handler, requests := attemptStreamV2RetryTestHandler(t, raw, func(attempt int32, response http.ResponseWriter) bool {
		if attempt != 1 {
			return false
		}
		// Close the connection before any response, as a stalled edge's
		// reset does.
		conn, _, err := response.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return true
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = conn.Close()
		return true
	})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		remotes.Store(request.RemoteAddr, true)
		handler.ServeHTTP(response, request)
	}))
	defer server.Close()
	reader := newAttemptStreamV2HTTPTestReader(t, server.URL)
	value, err := readAttemptStreamV2RetryObject(t, reader, raw)
	if err != nil {
		t.Fatalf("one reset open failed the object: %v", err)
	}
	connections := 0
	remotes.Range(func(any, any) bool { connections++; return true })
	if !bytes.Equal(value, raw) || requests.Load() != 2 || connections != 2 {
		t.Fatalf("the retry did not reach the object on a fresh connection: value=%q requests=%d connections=%d", value, requests.Load(), connections)
	}
}

func TestAttemptStreamV2HTTPRetriesAnUnavailableStatus(t *testing.T) {
	raw := []byte("{\"unavailable\":false}\n")
	handler, requests := attemptStreamV2RetryTestHandler(t, raw, func(attempt int32, response http.ResponseWriter) bool {
		if attempt != 1 {
			return false
		}
		response.WriteHeader(http.StatusServiceUnavailable)
		return true
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	value, err := readAttemptStreamV2RetryObject(t, newAttemptStreamV2HTTPTestReader(t, server.URL), raw)
	if err != nil || !bytes.Equal(value, raw) || requests.Load() != 2 {
		t.Fatalf("a 503 failed the object open: value=%q requests=%d err=%v", value, requests.Load(), err)
	}
}

// Only a transport failure before the body earns another try. There are at
// most three tries, a caller's cancellation ends them, and refusals are final.
func TestAttemptStreamV2HTTPOpenRetryKeepsRefusalsAndCancellationFinal(t *testing.T) {
	raw := []byte("{\"final\":true}\n")
	for _, test := range []struct {
		name     string
		fail     func(int32, http.ResponseWriter) bool
		size     uint64
		requests int32
		retry    bool
	}{
		{name: "not found", requests: 1, fail: func(_ int32, response http.ResponseWriter) bool {
			response.WriteHeader(http.StatusNotFound)
			return true
		}},
		{name: "wrong type", requests: 1, fail: func(_ int32, response http.ResponseWriter) bool {
			response.Header().Set("Content-Type", "text/plain")
			_, _ = response.Write(raw)
			return true
		}},
		{name: "wrong length", requests: 1, size: uint64(len(raw)) + 1},
		{name: "always unavailable", requests: 3, retry: true, fail: func(_ int32, response http.ResponseWriter) bool {
			response.WriteHeader(http.StatusBadGateway)
			return true
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, requests := attemptStreamV2RetryTestHandler(t, raw, test.fail)
			server := httptest.NewServer(handler)
			defer server.Close()
			size := uint64(len(raw))
			if test.size != 0 {
				size = test.size
			}
			body, err := newAttemptStreamV2HTTPTestReader(t, server.URL).OpenData(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(raw)), size)
			if body != nil {
				_ = body.Close()
			}
			if err == nil || requests.Load() != test.requests || RetryableEvidenceTransportError(err) != test.retry {
				t.Fatalf("open retry changed a final result: requests=%d retryable=%t err=%v", requests.Load(), RetryableEvidenceTransportError(err), err)
			}
		})
	}
	t.Run("canceled during the pause", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		answered := make(chan struct{})
		handler, requests := attemptStreamV2RetryTestHandler(t, raw, func(attempt int32, response http.ResponseWriter) bool {
			response.WriteHeader(http.StatusServiceUnavailable)
			if attempt == 1 {
				close(answered)
			}
			return true
		})
		server := httptest.NewServer(handler)
		defer server.Close()
		go func() {
			<-answered
			// The reader is then in its pause before the second try.
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()
		started := time.Now()
		body, err := newAttemptStreamV2HTTPTestReader(t, server.URL).OpenData(ctx, AttemptStreamV2Records, attemptHex32(sha256.Sum256(raw)), uint64(len(raw)))
		if body != nil {
			_ = body.Close()
		}
		if err == nil || !errors.Is(err, context.Canceled) || requests.Load() != 1 || time.Since(started) >= 500*time.Millisecond {
			t.Fatalf("a canceled caller kept retrying: requests=%d elapsed=%s err=%v", requests.Load(), time.Since(started), err)
		}
	})
}

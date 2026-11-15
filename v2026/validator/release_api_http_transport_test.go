//go:build linux || darwin

package validator

// A real TLS HTTP/2 server whose established connection is blackholed proves
// the health check drops a severed connection and a later read dials afresh.

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Once frozen, reads block until close and writes are dropped, like a path
// severed by an lb restart without a reset reaching the client.
type releaseAPIHTTPTestFreezableConn struct {
	net.Conn
	frozen    atomic.Bool
	closed    chan struct{}
	closeOnce sync.Once
}

func (self *releaseAPIHTTPTestFreezableConn) Read(value []byte) (int, error) {
	if self.frozen.Load() {
		<-self.closed
		return 0, net.ErrClosed
	}
	count, err := self.Conn.Read(value)
	if self.frozen.Load() {
		<-self.closed
		return 0, net.ErrClosed
	}
	return count, err
}

func (self *releaseAPIHTTPTestFreezableConn) Write(value []byte) (int, error) {
	if self.frozen.Load() {
		return len(value), nil
	}
	return self.Conn.Write(value)
}

func (self *releaseAPIHTTPTestFreezableConn) Close() error {
	self.closeOnce.Do(func() { close(self.closed) })
	return self.Conn.Close()
}

// Records every accepted raw TCP connection beneath the server's TLS layer.
type releaseAPIHTTPTestListener struct {
	net.Listener
	lock  sync.Mutex
	conns []*releaseAPIHTTPTestFreezableConn
}

func (self *releaseAPIHTTPTestListener) Accept() (net.Conn, error) {
	conn, err := self.Listener.Accept()
	if err != nil {
		return nil, err
	}
	wrapped := &releaseAPIHTTPTestFreezableConn{Conn: conn, closed: make(chan struct{})}
	self.lock.Lock()
	defer self.lock.Unlock()
	self.conns = append(self.conns, wrapped)
	return wrapped, nil
}

func (self *releaseAPIHTTPTestListener) accepted() []*releaseAPIHTTPTestFreezableConn {
	self.lock.Lock()
	defer self.lock.Unlock()
	return append([]*releaseAPIHTTPTestFreezableConn(nil), self.conns...)
}

func (self *releaseAPIHTTPTestListener) closeAll() {
	for _, conn := range self.accepted() {
		_ = conn.Close()
	}
}

// Serves one metadata object over TLS HTTP/2 and returns a reader that uses
// the given transport, trusting only the test server's certificate.
func newReleaseAPIHTTPTestServer(t *testing.T, transport *http.Transport) (*HTTPAttemptStreamV2Reader, *releaseAPIHTTPTestListener, string, []byte) {
	t.Helper()
	raw := []byte("{\"complete\":true}\n")
	hash := attemptHex32(sha256.Sum256(raw))
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.ProtoMajor != 2 {
			t.Errorf("request used %s, not HTTP/2", request.Proto)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(raw)
	}))
	listener := &releaseAPIHTTPTestListener{Listener: server.Listener}
	server.Listener = listener
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	t.Cleanup(listener.closeAll)
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	t.Cleanup(transport.CloseIdleConnections)
	reader := newAttemptStreamV2HTTPTestReader(t, server.URL)
	reader.client.Transport = transport
	return reader, listener, hash, raw
}

// Reads once, freezes that connection, then retries like a steering read
// until success or the deadline; returns the elapsed time and last error.
func readAcrossSeveredReleaseAPIConnection(t *testing.T, transport *http.Transport, deadline time.Duration) (time.Duration, error, int) {
	t.Helper()
	reader, listener, hash, raw := newReleaseAPIHTTPTestServer(t, transport)
	if value, err := reader.ReadMetadata(context.Background(), hash, uint64(len(raw))); err != nil || string(value) != string(raw) {
		t.Fatalf("first read failed: %v", err)
	}
	conns := listener.accepted()
	if len(conns) != 1 {
		t.Fatalf("first read used %d connections", len(conns))
	}
	conns[0].frozen.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	started := time.Now()
	var lastErr error
	for ctx.Err() == nil {
		value, err := reader.ReadMetadata(ctx, hash, uint64(len(raw)))
		if err == nil {
			if string(value) != string(raw) {
				t.Fatal("recovered read returned different bytes")
			}
			return time.Since(started), nil, len(listener.accepted())
		}
		lastErr = err
	}
	return time.Since(started), errors.Join(lastErr, ctx.Err()), len(listener.accepted())
}

// The shared production transport carries the health check and the usual
// dial, TLS and pool timeouts.
func TestReleaseAPIHTTPTransportHealthChecksHTTP2(t *testing.T) {
	transport := releaseAPIHTTPTransport
	if transport.HTTP2 == nil || transport.HTTP2.SendPingTimeout != 15*time.Second || transport.HTTP2.PingTimeout != 10*time.Second {
		t.Fatalf("API transport lacks the HTTP/2 health check: %+v", transport.HTTP2)
	}
	if !transport.ForceAttemptHTTP2 || transport.Proxy == nil || transport.DialContext == nil || transport.TLSHandshakeTimeout != 10*time.Second || transport.IdleConnTimeout != 90*time.Second {
		t.Fatal("API transport differs from the default transport's dial, proxy and pool settings")
	}
	if fresh := newReleaseAPIHTTPTransport(); fresh == transport || fresh.HTTP2 == transport.HTTP2 {
		t.Fatal("API transport constructor shares mutable state")
	}
	kept := withReleaseAPIHTTP2Health(&http.Transport{HTTP2: &http.HTTP2Config{MaxReadFrameSize: 1 << 20}})
	if kept.HTTP2.MaxReadFrameSize != 1<<20 || kept.HTTP2.SendPingTimeout != 15*time.Second || kept.HTTP2.PingTimeout != 10*time.Second {
		t.Fatal("health check dropped existing HTTP/2 settings")
	}
}

// Every API client the validator builds uses a health-checked transport and
// still refuses redirects.
func TestReleaseAPIHTTPClientsUseHealthCheckedTransport(t *testing.T) {
	bounds, _ := attemptReplayV2TestBounds()
	reader, err := NewHTTPAttemptStreamV2Reader("https://api.example", bounds)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewHTTPAttemptStreamV2Writer("https://api.example", bounds, func() string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	history, err := NewHTTPClientKeyHistoryReader("https://api.example", func() string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	wallet, err := NewHttpWalletMappingReader("https://api.example")
	if err != nil {
		t.Fatal(err)
	}
	shared := map[string]*http.Client{"attempt reader": reader.client, "attempt writer": writer.client, "client-key history": history.client, "wallet mapping": wallet.client, "server keys": newReleaseServerKeysV2Client()}
	for name, client := range shared {
		if client.Transport != releaseAPIHTTPTransport || client.CheckRedirect == nil {
			t.Errorf("%s client does not use the shared API transport with redirects refused", name)
		}
	}
	artifact, err := NewHTTPArtifactReader("https://api.example", "sn25-mainnet-20261007-01", 25)
	if err != nil {
		t.Fatal(err)
	}
	for name, transport := range map[string]*http.Transport{"artifact": artifact.client.Transport.(*http.Transport), "provider attempt": newProviderAttemptHttpTransport()} {
		if transport.HTTP2 == nil || transport.HTTP2.SendPingTimeout != releaseAPIHTTP2SendPingTimeout || transport.HTTP2.PingTimeout != releaseAPIHTTP2PingTimeout {
			t.Errorf("%s transport lacks the HTTP/2 health check", name)
		}
	}
	if newProviderAttemptHttpTransport().Proxy != nil {
		t.Error("provider attempt transport gained a proxy")
	}
}

// With the health check, a read stuck on a blackholed HTTP/2 connection fails
// once the ping goes unanswered, and the next read dials a new connection.
func TestReleaseAPIHTTPTransportRecoversFromSeveredHTTP2Connection(t *testing.T) {
	transport := newReleaseAPIHTTPTransport()
	transport.HTTP2.SendPingTimeout = 200 * time.Millisecond
	transport.HTTP2.PingTimeout = 200 * time.Millisecond
	elapsed, err, conns := readAcrossSeveredReleaseAPIConnection(t, transport, 10*time.Second)
	if err != nil {
		t.Fatalf("read did not recover from the severed connection: %v", err)
	}
	if conns != 2 {
		t.Fatalf("recovery used %d connections, want a second one", conns)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("recovery took %s", elapsed)
	}
}

// Control: without the health check the same reads stay on the severed
// connection until the caller's deadline.
func TestDefaultHTTPTransportHangsOnSeveredHTTP2Connection(t *testing.T) {
	transport := newReleaseAPIHTTPTransport()
	transport.HTTP2 = nil
	elapsed, err, conns := readAcrossSeveredReleaseAPIConnection(t, transport, time.Second)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read without a health check did not hang until the deadline: %v", err)
	}
	if conns != 1 || elapsed < time.Second {
		t.Fatalf("control dialed %d connections in %s", conns, elapsed)
	}
}

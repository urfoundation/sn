//go:build linux || darwin

// A standard Http/2 configuration may be created by the first request without
// enabling health checks. That initialization must retain the default pool.
package validator

import (
	"net/http"
	"testing"
	"time"
)

// The first real request starts with nil Http/2 configuration. Go fills it in,
// but the next owned API read must still reuse the same healthy connection.
func TestReleaseApiHttpRequestPreservesDefaultHttp2PoolingAfterWarmup(t *testing.T) {
	transport := newReleaseAPIHTTPTransport()
	transport.HTTP2 = nil
	reader, listener, hash, raw := newReleaseAPIHTTPTestServer(t, transport)
	if transport.HTTP2 != nil {
		t.Fatal("the default transport was initialized before its first request")
	}
	for index := range 2 {
		value, err := reader.ReadMetadata(t.Context(), hash, uint64(len(raw)))
		if err != nil || string(value) != string(raw) {
			t.Fatalf("default pooled read%d failed: %v", index, err)
		}
		if transport.HTTP2 == nil || transport.HTTP2.SendPingTimeout != 0 {
			t.Fatal("the real default request did not install its disabled health configuration")
		}
	}
	if count := len(listener.accepted()); count != 1 {
		t.Fatalf("automatic disabled health configuration replaced the default connection pool: connections=%d", count)
	}
}

// An enabled send timer owns its request even with Go's default ping timeout.
// Requiring an explicit ping timeout would revoke valid health configurations.
func TestReleaseApiHttpRequestOwnsHealthWithDefaultPingTimeout(t *testing.T) {
	transport := newReleaseAPIHTTPTransport()
	transport.HTTP2 = &http.HTTP2Config{SendPingTimeout: time.Minute}
	reader, listener, hash, raw := newReleaseAPIHTTPTestServer(t, transport)
	for index := range 2 {
		value, err := reader.ReadMetadata(t.Context(), hash, uint64(len(raw)))
		if err != nil || string(value) != string(raw) {
			t.Fatalf("health-owned read%d failed: %v", index, err)
		}
	}
	if transport.HTTP2.PingTimeout != 0 || transport.HTTP2.SendPingTimeout != time.Minute {
		t.Fatal("the configured send timer or default ping timeout changed")
	}
	if count := len(listener.accepted()); count != 2 {
		t.Fatalf("enabled health with a default ping timeout lost request ownership: connections=%d", count)
	}
}

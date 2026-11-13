//go:build linux || darwin

// Request-owned ping recovery cannot grant authority to unrelated transport,
// caller, authentication, acknowledgement, or certificate failures.
package validator

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// A custom transport retains its exact cause even when its text resembles the
// standard transport's private error; no callback or inline retry is invented.
func TestReleaseApiHttpRequestCustomTransportHasNoPingAuthority(t *testing.T) {
	data := []byte("synthetic immutable bytes")
	hash := attemptHex32(sha256.Sum256(data))
	for _, cause := range []error{errors.New("http2: client connection lost"), errors.New("synthetic independent refusal")} {
		calls := 0
		writer := newAttemptStreamV2HTTPTestWriter(t, "http://127.0.0.1:1", func() string { return "synthetic-session" })
		writer.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, cause
		})
		err := writer.Write(t.Context(), AttemptStreamV2Records, hash, data)
		if calls != 1 || !errors.Is(err, cause) || retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
			t.Fatalf("custom transport acquired ping authority or retry: calls=%d error=%v", calls, err)
		}
	}
}

// A custom protocol hook can own another pool and callback. The helper must
// delegate with the original request context rather than claiming its events.
func TestReleaseApiHttpRequestCustomProtocolHookHasNoPingAuthority(t *testing.T) {
	endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("custom protocol refusal reached the server handler")
	}))
	endpoint.EnableHTTP2 = true
	endpoint.StartTLS()
	t.Cleanup(endpoint.Close)
	transport := newReleaseAPIHTTPTransport()
	transport.TLSClientConfig = endpoint.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.TLSClientConfig.NextProtos = []string{"h2"}
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	callbacks, hooks, calls := 0, 0, 0
	cause := errors.New("http2: client connection lost")
	transport.HTTP2.CountError = func(string) { callbacks++ }
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{
		"h2": func(_ string, connection *tls.Conn) http.RoundTripper {
			hooks++
			_ = connection.Close()
			return attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Context() != ctx {
					t.Error("custom protocol hook acquired a replacement request owner")
				}
				transport.HTTP2.CountError("conn_close_lost_ping")
				return nil, cause
			})
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = doReleaseApiHttpRequest(&http.Client{Transport: transport}, request)
	if hooks != 1 || calls != 1 || callbacks != 1 || !errors.Is(err, cause) || ctx.Err() != nil || retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
		t.Fatalf("custom protocol acquired health authority: hooks=%d calls=%d callbacks=%d error=%v", hooks, calls, callbacks, err)
	}
}

// The real standard Http/2 request remains owned by caller cancellation, never
// by an unrelated ping marker, and the server's request lifetime also ends.
func TestReleaseApiHttpRequestPreservesCallerCancellation(t *testing.T) {
	entered, stopped := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Error("cancellation control did not use the standard Http/2 transport")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(stopped)
	}))
	endpoint.EnableHTTP2 = true
	endpoint.StartTLS()
	t.Cleanup(endpoint.Close)
	transport := newReleaseAPIHTTPTransport()
	transport.TLSClientConfig = endpoint.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	t.Cleanup(transport.CloseIdleConnections)
	writer := newAttemptStreamV2HTTPTestWriter(t, endpoint.URL, func() string { return "synthetic-session" })
	writer.client.Transport = transport
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	data := []byte("synthetic cancel body")
	done := make(chan error, 1)
	go func() { done <- writer.Write(ctx, AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data) }()
	<-entered
	cancel()
	err := <-done
	<-stopped
	var requestError *url.Error
	if !errors.Is(err, context.Canceled) || !errors.As(err, &requestError) || requestError.Err != context.Canceled {
		t.Fatalf("caller cancellation was replaced by a health event: %v", err)
	}
}

// Successful transport is not a valid acknowledgement: the actual writer
// retains authorization/status and exact Etag refusals without retrying them.
func TestReleaseApiHttpRequestRetainsStatusAndAcknowledgementRefusals(t *testing.T) {
	data := []byte("synthetic exact body")
	hash := attemptHex32(sha256.Sum256(data))
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest, http.StatusNoContent} {
		var calls atomic.Int32
		endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("ETag", `"wrong-object"`)
			w.WriteHeader(status)
		}))
		endpoint.EnableHTTP2 = true
		endpoint.StartTLS()
		t.Cleanup(endpoint.Close)
		transport := newReleaseAPIHTTPTransport()
		transport.TLSClientConfig = endpoint.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		t.Cleanup(transport.CloseIdleConnections)
		writer := newAttemptStreamV2HTTPTestWriter(t, endpoint.URL, func() string { return "synthetic-session" })
		writer.client.Transport = transport
		err := writer.Write(t.Context(), AttemptStreamV2Records, hash, data)
		if err == nil || calls.Load() != 1 || retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
			t.Fatalf("status%d acquired transport authority or retry: calls=%d error=%v", status, calls.Load(), err)
		}
	}
}

// An untrusted real server certificate never reaches Http admission and cannot
// be converted into a connection-health retry by the concrete transport path.
func TestReleaseApiHttpRequestRetainsCertificateRefusal(t *testing.T) {
	var calls atomic.Int32
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(endpoint.Close)
	transport := newReleaseAPIHTTPTransport()
	t.Cleanup(transport.CloseIdleConnections)
	writer := newAttemptStreamV2HTTPTestWriter(t, endpoint.URL, func() string { return "synthetic-session" })
	writer.client.Transport = transport
	data := []byte("synthetic certificate body")
	err := writer.Write(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data)
	var certificateError *tls.CertificateVerificationError
	if !errors.As(err, &certificateError) || calls.Load() != 0 || retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
		t.Fatalf("certificate refusal acquired transport authority: calls=%d error=%v", calls.Load(), err)
	}
}

// A malformed response from a real trusted socket remains a protocol failure,
// even though this transport also has the standard ping policy configured.
func TestReleaseApiHttpRequestRetainsMalformedProtocolFailure(t *testing.T) {
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = fmt.Fprint(connection, "HTTP/1.1 not-a-status\r\n\r\n")
	}))
	t.Cleanup(endpoint.Close)
	transport := newReleaseAPIHTTPTransport()
	transport.TLSClientConfig = endpoint.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	t.Cleanup(transport.CloseIdleConnections)
	writer := newAttemptStreamV2HTTPTestWriter(t, endpoint.URL, func() string { return "synthetic-session" })
	writer.client.Transport = transport
	data := []byte("synthetic protocol body")
	err := writer.Write(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data)
	if err == nil || retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
		t.Fatalf("malformed protocol acquired transport authority: %v", err)
	}
}

// A valid concrete network cause cannot hide an independently joined hard
// refusal when the production loop freezes the complete original graph.
func TestReleaseApiHttpRequestMixedHardCauseStaysHard(t *testing.T) {
	transport := &url.Error{Op: "Post", URL: "https://synthetic.invalid/sn/attempt-artifact", Err: &net.OpError{Op: "ping", Net: "http2", Err: net.ErrClosed}}
	hard := errors.New("synthetic independent immutable refusal")
	if !retryableProductionSteeringRead(observeProductionSteeringRead(transport, false)) {
		t.Fatal("known network cause prerequisite was not retryable")
	}
	if retryableProductionSteeringRead(observeProductionSteeringRead(errors.Join(transport, hard), false)) {
		t.Fatal("transport cause hid the independent hard branch")
	}
}

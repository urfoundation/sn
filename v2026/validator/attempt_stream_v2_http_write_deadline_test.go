//go:build linux || darwin

// An upload's progress-measured deadline over real loopback HTTP, with socket
// buffers small enough that its body moves at its reader's pace, and the
// HTTP/1.1 transport uploads use.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"
)

// A loopback HTTP server, which a writer may credential, taking each upload
// body as read wants it, then answering the object's acknowledgement.
func newReleaseSlowUploadServer(t *testing.T, read func(*http.Request) error) *httptest.Server {
	t.Helper()
	endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := read(r); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	listener, err := (&net.ListenConfig{Control: releaseLossyPathSocketControl(syscall.SO_RCVBUF)}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint.Listener.Close()
	endpoint.Listener = listener
	endpoint.Start()
	t.Cleanup(endpoint.Close)
	return endpoint
}

// The writer's own transport, dialing with a small send buffer.
func releaseSlowUploadWriter(t *testing.T, origin string, settings AttemptUploadSettings) *HTTPAttemptStreamV2Writer {
	t.Helper()
	writer := releaseLossyPathWriter(t, origin)
	transport := writer.client.Transport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, Control: releaseLossyPathSocketControl(syscall.SO_SNDBUF)}).DialContext
	t.Cleanup(transport.CloseIdleConnections)
	writer.client.Transport = transport
	writer.settings = settings
	return writer
}

// A body that stops moving ends its request within the stall window, long
// before its cap, as an owned deadline every retry owner may retry.
func TestAttemptUploadStallEndsTheRequest(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	endpoint := newReleaseSlowUploadServer(t, func(r *http.Request) error {
		if _, err := io.CopyN(io.Discard, r.Body, 64*1024); err != nil {
			return err
		}
		<-release
		_, err := io.Copy(io.Discard, r.Body)
		return err
	})
	t.Cleanup(func() { close(release) })
	// a cap no upload reaches: only the stall can end this one
	settings := DefaultAttemptUploadSettings()
	settings.StallTimeout, settings.WatchInterval, settings.FloorBytesPerSecond = 500*time.Millisecond, 20*time.Millisecond, 1
	writer := releaseSlowUploadWriter(t, endpoint.URL, settings)
	data := bytes.Repeat([]byte("{\"stalled\":true}\n"), (1024*1024)/17)
	started := time.Now()
	err := writer.Write(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) || elapsed < 500*time.Millisecond || elapsed > 10*time.Second {
		t.Fatalf("a stalled body did not end at its stall window: elapsed=%s err=%v", elapsed.Round(time.Millisecond), err)
	}
	if !RetryableEvidenceTransportError(err) || !retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) || !releaseAttemptUploadTransient(err) {
		t.Fatalf("a stalled upload became hard: %v", err)
	}
}

// A body that keeps moving, slower than its size's floor rate, ends at its cap.
func TestAttemptUploadSlowerThanItsFloorEndsAtTheCap(t *testing.T) {
	t.Parallel()
	endpoint := newReleaseSlowUploadServer(t, func(r *http.Request) error {
		buffer := make([]byte, 16*1024)
		for {
			if _, err := io.ReadFull(r.Body, buffer); err != nil {
				return err
			}
			select {
			case <-time.After(100 * time.Millisecond):
			case <-r.Context().Done():
				return r.Context().Err()
			}
		}
	})
	// 1 MiB at the floor's 1 MiB/s plus the 3 s stall window: a 4 s cap for
	// a body the server takes in about 6.4 s, moving every few tenths of a second
	settings := DefaultAttemptUploadSettings()
	settings.StallTimeout, settings.WatchInterval, settings.FloorBytesPerSecond = 3*time.Second, 20*time.Millisecond, 1024*1024
	writer := releaseSlowUploadWriter(t, endpoint.URL, settings)
	data := bytes.Repeat([]byte{'x'}, 1024*1024)
	started := time.Now()
	err := writer.Write(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) || elapsed < settings.requestCap(len(data)) || elapsed > settings.requestCap(len(data))+5*time.Second {
		t.Fatalf("a body slower than its floor did not end at the %s cap: elapsed=%s err=%v", settings.requestCap(len(data)), elapsed.Round(time.Millisecond), err)
	}
}

// The caller's own deadline or cancellation keeps its identity: it is not the
// owner's expiry, and the owner does not replace it.
func TestAttemptUploadCallerEndingIsNotTheOwnersDeadline(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	endpoint := newReleaseSlowUploadServer(t, func(r *http.Request) error {
		<-release
		_, err := io.Copy(io.Discard, r.Body)
		return err
	})
	t.Cleanup(func() { close(release) })
	settings := DefaultAttemptUploadSettings()
	settings.StallTimeout, settings.WatchInterval = time.Hour, 20*time.Millisecond
	writer := releaseSlowUploadWriter(t, endpoint.URL, settings)
	data := bytes.Repeat([]byte{'y'}, 1024*1024)
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(200*time.Millisecond, cancel)
	err := writer.Write(ctx, AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data)
	if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the caller's cancellation became something else: %v", err)
	}
}

// Production's upload settings: a 30 s stall window and a 16 KiB/s floor, so
// a 4 MiB object may take 286 s while it keeps moving, six requests per
// object, and one object in flight, the api's limit per owner.
func TestAttemptUploadDefaultSettings(t *testing.T) {
	t.Parallel()
	settings := DefaultAttemptUploadSettings()
	if settings.StallTimeout != 30*time.Second || settings.WatchInterval != time.Second || settings.FloorBytesPerSecond != 16*1024 || settings.ProbeTimeout != 30*time.Second ||
		settings.Attempts != 6 || settings.BackoffBase != 2*time.Second || settings.BackoffCap != time.Minute || settings.InFlight != 1 ||
		settings.IdleConnsPerHost != 8 || settings.IdleConnTimeout != 45*time.Second || settings.WriteBufferBytes != 64*1024 || !settings.Compress {
		t.Fatalf("default upload settings %+v", settings)
	}
	if got := settings.requestCap(4 * 1024 * 1024); got != 286*time.Second {
		t.Fatalf("4 MiB cap %s", got)
	}
	if got := settings.requestCap(1); got <= settings.StallTimeout || got > settings.StallTimeout+time.Millisecond {
		t.Fatalf("1 byte cap %s", got)
	}
	settings.InFlight = 4
	if DefaultAttemptUploadSettings().InFlight != 1 {
		t.Fatal("default settings share state between calls")
	}
}

// A transport that sends the request again after a pooled connection closed
// unread gets the same bytes, and their reads count as the request's progress.
func TestAttemptUploadBodyRewindKeepsProgress(t *testing.T) {
	t.Parallel()
	data := []byte("0123456789")
	body := newAttemptUploadBody(data)
	if body.progress.sent.Load() {
		t.Fatal("an unread body is sent")
	}
	if _, err := io.ReadAll(body); err != nil || !body.progress.sent.Load() {
		t.Fatalf("a read body is not sent: %v", err)
	}
	body.progress.moved.Store(0)
	again, err := body.rewind(data)()
	if err != nil || body.progress.sent.Load() || time.Since(time.Unix(0, body.progress.moved.Load())) > time.Minute {
		t.Fatalf("a rewound body did not start over: %v", err)
	}
	half := make([]byte, 5)
	if _, err := io.ReadFull(again, half); err != nil || body.progress.sent.Load() || !bytes.Equal(half, data[:5]) {
		t.Fatalf("a rewound body read %q: %v", half, err)
	}
	rest, err := io.ReadAll(again)
	if err != nil || !bytes.Equal(rest, data[5:]) || !body.progress.sent.Load() {
		t.Fatalf("a rewound body read %q: %v", rest, err)
	}
}

// Uploads go over HTTP/1.1, through one pooled transport built from the
// upload settings; reads keep the HTTP/2 transport.
func TestAttemptUploadTransportIsHttp1(t *testing.T) {
	t.Parallel()
	protocols := releaseApiUploadHttpTransport.Protocols
	if protocols == nil || !protocols.HTTP1() || protocols.HTTP2() || protocols.UnencryptedHTTP2() {
		t.Fatalf("upload protocols %v", protocols)
	}
	settings := DefaultAttemptUploadSettings()
	if releaseApiUploadHttpTransport.MaxIdleConnsPerHost != settings.IdleConnsPerHost || releaseApiUploadHttpTransport.IdleConnTimeout != settings.IdleConnTimeout || releaseApiUploadHttpTransport.WriteBufferSize != settings.WriteBufferBytes ||
		releaseApiUploadHttpTransport.Proxy == nil || releaseApiUploadHttpTransport.TLSHandshakeTimeout != releaseAPIHTTPTLSHandshakeTimeout {
		t.Fatal("upload transport lost its pool, proxy or handshake settings")
	}
	bounds, _ := attemptReplayV2TestBounds()
	writer, err := NewHTTPAttemptStreamV2Writer("https://api.example", bounds, func() string { return "synthetic-session" })
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewHTTPAttemptStreamV2Reader("https://api.example", bounds)
	if err != nil {
		t.Fatal(err)
	}
	if writer.client.Transport != releaseApiUploadHttpTransport || reader.client.Transport != releaseAPIHTTPTransport || writer.client.CheckRedirect == nil || writer.client.Timeout != 0 {
		t.Fatal("uploads and reads do not use their own transports")
	}
}

//go:build linux || darwin

// The 2026-11-16 incident, reproduced. Hurricane Electric's den1 to sjc2 to
// fmt2 segment lost packets at about 90 ms RTT, and uploads into the api's site
// slowed to about 100 KB/s. Each 4.19 MB records object then needed about 42
// seconds, every upload had 30, and the seals that carried them failed until
// the path recovered: intents finalized 38 to 68 minutes late.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Kernel socket buffers small enough that a body moves at the pace its reader
// takes it, as it does on a loss-limited path, not at loopback speed.
const releaseLossyPathSocketBytes = 16 * 1024

func releaseLossyPathSocketControl(option int) func(string, string, syscall.RawConn) error {
	return func(_, _ string, raw syscall.RawConn) error {
		var setErr error
		err := raw.Control(func(fd uintptr) {
			setErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, option, releaseLossyPathSocketBytes)
		})
		return errors.Join(err, setErr)
	}
}

// What a load balancer on the far side of the lossy path looks like to the
// validator: TLS offering HTTP/2 and HTTP/1.1, an upload body taken a little at
// a time, then 204 with the object's ETag once all of it arrived.
type releaseLossyPathServer struct {
	endpoint  *httptest.Server
	stateLock sync.Mutex
	// each request's HTTP major version, each completely received object, and
	// each request's bytes on the wire
	protos    []int
	bodies    [][]byte
	wireBytes []int
	// acknowledgements advertise zstd request bodies, which the server decodes
	advertiseZstd atomic.Bool
}

func newReleaseLossyPathServer(t *testing.T, step int, interval time.Duration) *releaseLossyPathServer {
	t.Helper()
	self := &releaseLossyPathServer{}
	// An HTTP/1.1 handler does not see its client leave while it still reads
	// the body; the test's end stops it.
	stop := make(chan struct{})
	zstdDecoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(zstdDecoder.Close)
	endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		self.stateLock.Lock()
		self.protos = append(self.protos, r.ProtoMajor)
		self.stateLock.Unlock()
		var body bytes.Buffer
		buffer := make([]byte, step)
		for {
			count, err := r.Body.Read(buffer)
			body.Write(buffer[:count])
			if err == io.EOF {
				break
			}
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			select {
			case <-time.After(interval):
			case <-r.Context().Done():
				return
			case <-stop:
				return
			}
		}
		object := body.Bytes()
		if r.Header.Get("Content-Encoding") == "zstd" {
			decoded, err := zstdDecoder.DecodeAll(object, nil)
			if err != nil || strconv.Itoa(len(decoded)) != r.Header.Get(attemptUploadDecodedLengthHeader) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			object = decoded
		}
		self.stateLock.Lock()
		self.bodies = append(self.bodies, object)
		self.wireBytes = append(self.wireBytes, body.Len())
		self.stateLock.Unlock()
		if self.advertiseZstd.Load() {
			w.Header().Set("Accept-Encoding", "zstd")
		}
		w.Header().Set("ETag", `"`+attemptHex32(sha256.Sum256(object))+`"`)
		w.WriteHeader(http.StatusNoContent)
	}))
	listener, err := (&net.ListenConfig{Control: releaseLossyPathSocketControl(syscall.SO_RCVBUF)}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint.Listener.Close()
	endpoint.Listener = listener
	endpoint.EnableHTTP2 = true
	endpoint.TLS = &tls.Config{NextProtos: []string{"h2", "http/1.1"}}
	endpoint.StartTLS()
	t.Cleanup(endpoint.Close)
	t.Cleanup(func() { close(stop) })
	self.endpoint = endpoint
	return self
}

// The writer's own transport, trusting the test certificate and dialing with
// a small send buffer. Its protocol, pool and timeout settings are unchanged.
func (self *releaseLossyPathServer) attach(t *testing.T, writer *HTTPAttemptStreamV2Writer) {
	t.Helper()
	transport := validatorAPITransportBase(writer.client.Transport).(*http.Transport).Clone()
	transport.TLSClientConfig = self.endpoint.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, Control: releaseLossyPathSocketControl(syscall.SO_SNDBUF)}).DialContext
	t.Cleanup(transport.CloseIdleConnections)
	// under the api identity wrapper, as the writer sends
	writer.client.Transport = validatorAPITransport(transport)
}

func (self *releaseLossyPathServer) received() ([]int, [][]byte) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]int(nil), self.protos...), append([][]byte(nil), self.bodies...)
}

func (self *releaseLossyPathServer) wire() []int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]int(nil), self.wireBytes...)
}

// A records object as large as the production chunk bound allows.
func releaseLossyPathWriter(t *testing.T, origin string) *HTTPAttemptStreamV2Writer {
	t.Helper()
	bounds, _ := attemptReplayV2TestBounds()
	bounds.Records.MaxChunkBytes = 4 * 1024 * 1024
	writer, err := NewHTTPAttemptStreamV2Writer(origin, bounds, func() string { return "synthetic-session" })
	if err != nil {
		t.Fatal(err)
	}
	return writer
}

// The upload outlasts the 30 seconds every upload had, though its body never
// stops moving, and it goes over HTTP/1.1 though the server offers HTTP/2.
// Before the fix the writer cut it off at 30 seconds, every time, over HTTP/2.
func TestAttemptUploadOverALossyPathCompletes(t *testing.T) {
	t.Parallel()
	// 8 KiB every 250 ms is 32 KiB/s: 1.25 MiB takes about 40 seconds.
	server := newReleaseLossyPathServer(t, 8*1024, 250*time.Millisecond)
	writer := releaseLossyPathWriter(t, server.endpoint.URL)
	server.attach(t, writer)
	data := bytes.Repeat([]byte("{\"lossy\":\"path\"}\n"), (5*256*1024)/17)
	hash := attemptHex32(sha256.Sum256(data))
	started := time.Now()
	err := writer.Write(t.Context(), AttemptStreamV2Records, hash, data)
	elapsed := time.Since(started)
	protos, bodies := server.received()
	if err != nil {
		t.Fatalf("a slow upload that kept moving failed after %s (protocols %v): %v", elapsed.Round(time.Second), protos, err)
	}
	if elapsed <= DefaultAttemptUploadSettings().StallTimeout {
		t.Fatalf("the path took the body in %s, not slower than the old fixed deadline; the test lost its shape", elapsed.Round(time.Second))
	}
	if len(protos) != 1 || protos[0] != 1 {
		t.Fatalf("upload protocols = %v, want one HTTP/1.1 request", protos)
	}
	if len(bodies) != 1 || !bytes.Equal(bodies[0], data) {
		t.Fatal("the server did not store the exact bytes once")
	}
}

// One reserved upload owner over a fake load balancer whose first answer is
// lost: the connection resets mid-request. The object goes again on a new
// request. Before the fix the reset failed the upload, and with it the seal.
func TestAttemptUploadCutShortIsSentAgain(t *testing.T) {
	t.Parallel()
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusNoContent, "" })
	var stateLock sync.Mutex
	resets := 0
	fixture.owner.writer.client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		reset := func() bool {
			stateLock.Lock()
			defer stateLock.Unlock()
			if request.Method != http.MethodPost || resets != 0 {
				return false
			}
			resets++
			return true
		}()
		if reset {
			if _, err := io.Copy(io.Discard, request.Body); err != nil {
				return nil, err
			}
			return nil, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
		}
		return fixture.roundTrip(request)
	})
	data := []byte("{\"object\":\"reset\"}\n")
	if err := fixture.write(t.Context(), data); err != nil {
		t.Fatalf("a reset upload failed instead of going again: %v", err)
	}
	if resets != 1 || fixture.requests() != 1 {
		t.Fatalf("resets=%d delivered=%d, want the reset and one delivered request", resets, fixture.requests())
	}
	if len(fixture.waits) != 1 || fixture.waits[0] <= 0 || fixture.waits[0] > 2*time.Second {
		t.Fatalf("waits = %v, want one short backoff", fixture.waits)
	}
	if !bytes.Equal(fixture.bodies[0], data) {
		t.Fatal("the second request carried other bytes")
	}
}

// The api stored the object, and its answer was lost on the way back: the
// load balancer answers 502. The object reads back from the public origin, so
// it is not sent again. Before the fix the 502 failed the upload and the seal.
func TestAttemptUploadLostAcknowledgementIsNotSentAgain(t *testing.T) {
	t.Parallel()
	data := []byte("{\"object\":\"stored\"}\n")
	var fixture *releaseUploadRefusalFixture
	fixture = newReleaseUploadRefusalFixture(t, func(int) (int, string) {
		stored := bytes.Clone(data)
		fixture.stored.Store(&stored)
		return http.StatusBadGateway, "Bad gateway."
	})
	if err := fixture.write(t.Context(), data); err != nil {
		t.Fatalf("a stored object whose acknowledgement was lost failed: %v", err)
	}
	if fixture.requests() != 1 || fixture.probes.Load() != 1 || len(fixture.waits) != 0 {
		t.Fatalf("requests=%d probes=%d waits=%v, want one upload and one read back", fixture.requests(), fixture.probes.Load(), fixture.waits)
	}
}

// Nothing above may let an upload outlive the caller that owns it.
func TestAttemptUploadOverALossyPathEndsWithItsCaller(t *testing.T) {
	t.Parallel()
	server := newReleaseLossyPathServer(t, 8*1024, 250*time.Millisecond)
	writer := releaseLossyPathWriter(t, server.endpoint.URL)
	server.attach(t, writer)
	data := bytes.Repeat([]byte("{\"lossy\":\"path\"}\n"), (5*256*1024)/17)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	err := writer.Write(ctx, AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data)
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() == nil || time.Since(started) > 10*time.Second {
		t.Fatalf("the caller's deadline did not end the upload: elapsed=%s err=%v", time.Since(started).Round(time.Millisecond), err)
	}
}

// The same path with an api that takes zstd. The validator learns that from
// one small object's acknowledgement, and a records object then carries a
// fraction of its bytes over the path. Each object used to take the path's
// full time for every byte.
func TestAttemptUploadOverALossyPathSendsFewerBytes(t *testing.T) {
	t.Parallel()
	server := newReleaseLossyPathServer(t, 8*1024, 250*time.Millisecond)
	server.advertiseZstd.Store(true)
	writer := releaseLossyPathWriter(t, server.endpoint.URL)
	server.attach(t, writer)
	small := []byte("{\"learn\":true}\n")
	if err := writer.Write(t.Context(), "metadata", attemptHex32(sha256.Sum256(small)), small); err != nil {
		t.Fatal(err)
	}
	data := attemptUploadEncodingRecords(5 * 256 * 1024)
	started := time.Now()
	err := writer.Write(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(data)), data)
	elapsed := time.Since(started)
	_, bodies := server.received()
	wire := server.wire()
	if err != nil || len(bodies) != 2 || !bytes.Equal(bodies[1], data) {
		t.Fatalf("the compressed upload failed after %s: %v", elapsed.Round(time.Second), err)
	}
	if wire[1]*4 > len(data) || elapsed >= DefaultAttemptUploadSettings().StallTimeout {
		t.Fatalf("%d of %d bytes crossed the path in %s", wire[1], len(data), elapsed.Round(time.Second))
	}
}

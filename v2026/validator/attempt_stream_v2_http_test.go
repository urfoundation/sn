//go:build linux || darwin

package validator

// Local HTTP servers exercise real framing/cancellation. Fault readers expose
// otherwise hard-to-force close and EOF errors without replacing cut replay.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Explicit fixture budgets are not production defaults or capacity approval.
func newAttemptStreamV2HTTPTestReader(t *testing.T, origin string) *HTTPAttemptStreamV2Reader {
	t.Helper()
	bounds, _ := attemptReplayV2TestBounds()
	reader, err := NewHTTPAttemptStreamV2Reader(origin, bounds)
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

// A private response seam counts real body ownership and preserves errors.
type attemptStreamV2HTTPTestTransport func(*http.Request) (*http.Response, error)

// Requests still pass through the actual public reader and http.Client.
func (self attemptStreamV2HTTPTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

// Single-owner reader controls late errors without scheduler timing.
type attemptStreamV2HTTPTestBody struct {
	read   func([]byte) (int, error)
	close  func() error
	closes int
}

// Delegates one exact read operation to the test-controlled transport.
func (self *attemptStreamV2HTTPTestBody) Read(value []byte) (int, error) { return self.read(value) }

// Counts actual close operations, including response-admission rejection.
func (self *attemptStreamV2HTTPTestBody) Close() error {
	self.closes++
	if self.close != nil {
		return self.close()
	}
	return nil
}

// Real HTTP carries canonical hash/kind and never ambient caller credentials.
func TestAttemptStreamV2HTTPReadsExactMetadataAndTypedData(t *testing.T) {
	raw := []byte("{\"complete\":true}\n")
	hash := attemptHex32(sha256.Sum256(raw))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/sn/attempt-artifact" || request.URL.Query().Get("hash") != hash || len(request.URL.Query()) != 2 || request.Header.Get("Accept-Encoding") != "identity" || request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
			t.Errorf("typed public request differs: method=%s url=%s headers=%v", request.Method, request.URL, request.Header)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		contentType := "application/x-ndjson"
		if request.URL.Query().Get("kind") == "metadata" {
			contentType = "application/json"
		}
		if request.Header.Get("Accept") != contentType {
			t.Error("request lost its typed content negotiation")
		}
		response.Header().Set("Content-Type", contentType)
		_, _ = response.Write(raw)
	}))
	defer server.Close()
	reader := newAttemptStreamV2HTTPTestReader(t, server.URL)
	metadata, err := reader.ReadMetadata(t.Context(), hash, uint64(len(raw)))
	if err != nil || !bytes.Equal(metadata, raw) {
		t.Fatalf("exact metadata: %q %v", metadata, err)
	}
	for _, kind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
		body, err := reader.OpenData(t.Context(), kind, hash, uint64(len(raw)))
		if err != nil {
			t.Fatal(err)
		}
		value, readErr := io.ReadAll(body)
		if err := errors.Join(readErr, body.Close()); err != nil || !bytes.Equal(value, raw) {
			t.Fatalf("exact typed %s: %q %v", kind, value, err)
		}
	}
	if requests.Load() != 3 {
		t.Fatalf("typed request census=%d, want3", requests.Load())
	}
}

// Origin aliases and credentials are refused without echoing private URL data.
func TestAttemptStreamV2HTTPRejectsUntrustedOriginsAndOverflow(t *testing.T) {
	bounds, _ := attemptReplayV2TestBounds()
	for _, origin := range []string{"", "//example.test", "ftp://example.test", "http://user:private-pass@example.test", "http://example.test/path", "http://example.test?token=private-pass", "http://example.test?", "http://example.test/#fragment", "http://example.test/%2f"} {
		reader, err := NewHTTPAttemptStreamV2Reader(origin, bounds)
		if err == nil || reader != nil || strings.Contains(err.Error(), "private-pass") {
			t.Errorf("invalid origin was admitted or echoed credentials: reader=%v error=%v", reader, err)
		}
	}
	for _, mutate := range []func(*AttemptCutV2Bounds){
		func(value *AttemptCutV2Bounds) { value.Records.MaxItems = 0 },
		func(value *AttemptCutV2Bounds) { value.Proofs.MaxPages = 0 },
		func(value *AttemptCutV2Bounds) { value.Records.MaxManifestBytes = math.MaxUint64 },
		func(value *AttemptCutV2Bounds) {
			value.Records.MaxDataBytes = math.MaxUint64
			value.Records.MaxChunkBytes = math.MaxUint64
		},
		func(value *AttemptCutV2Bounds) {
			value.Proofs.MaxDataBytes = math.MaxUint64
			value.Proofs.MaxChunkBytes = math.MaxUint64
		},
	} {
		changed := bounds
		mutate(&changed)
		if reader, err := NewHTTPAttemptStreamV2Reader("http://127.0.0.1", changed); err == nil || reader != nil {
			t.Errorf("invalid transport bounds admitted: %+v %v", changed, err)
		}
	}
}

// Every invalid candidate is refused before RoundTrip, including a canceled
// call and a kind-specific limit that must not borrow another stream's budget.
func TestAttemptStreamV2HTTPAdmissionHasNoNetworkEffects(t *testing.T) {
	reader := newAttemptStreamV2HTTPTestReader(t, "http://127.0.0.1")
	var calls atomic.Int32
	reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected transport invocation")
	})
	hash := attemptHex32(sha256.Sum256([]byte("public")))
	for _, candidate := range []struct {
		kind, hash string
		size       uint64
	}{
		{kind: "metadata", hash: hash, size: 1},
		{kind: "unknown", hash: hash, size: 1},
		{kind: AttemptStreamV2Records, hash: hash, size: 0},
		{kind: AttemptStreamV2Records, hash: hash, size: reader.recordBytes + 1},
		{kind: AttemptStreamV2Proofs, hash: hash, size: reader.proofBytes + 1},
		{kind: AttemptStreamV2Records, hash: zeroAttemptHash(), size: 1},
		{kind: AttemptStreamV2Records, hash: strings.ToUpper(hash), size: 1},
	} {
		if body, err := reader.OpenData(t.Context(), candidate.kind, candidate.hash, candidate.size); err == nil || body != nil {
			t.Errorf("invalid typed request admitted: %+v %v", candidate, err)
		}
	}
	if value, err := reader.ReadMetadata(t.Context(), hash, reader.metadataBytes+1); err == nil || value != nil {
		t.Fatal("oversized metadata was admitted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reader.ReadMetadata(ctx, hash, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled metadata lost cause: %v", err)
	}
	if _, err := reader.OpenData(nil, AttemptStreamV2Records, hash, 1); err == nil {
		t.Fatal("nil context was admitted")
	}
	if calls.Load() != 0 {
		t.Fatalf("refused input caused %d network calls", calls.Load())
	}
}

// A hostile origin cannot redirect proof retrieval to another authority.
func TestAttemptStreamV2HTTPDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	reader := newAttemptStreamV2HTTPTestReader(t, server.URL)
	if value, err := reader.ReadMetadata(t.Context(), attemptHex32(sha256.Sum256([]byte("x"))), 1); err == nil || value != nil {
		t.Fatal("redirect became metadata authority")
	}
	if redirected.Load() != 0 {
		t.Fatalf("redirected request reached a different origin %d times", redirected.Load())
	}
}

// Header rejection closes the actual body once, before reading any content.
func TestAttemptStreamV2HTTPRejectsTransformedAndPartialResponses(t *testing.T) {
	for _, mutate := range []func(*http.Response){
		func(response *http.Response) { response.StatusCode = http.StatusPartialContent },
		func(response *http.Response) { response.Header.Set("Content-Type", "application/x-ndjson") },
		func(response *http.Response) { response.Header.Add("Content-Type", "application/json") },
		func(response *http.Response) { response.Header.Set("Content-Encoding", "gzip") },
		func(response *http.Response) { response.Uncompressed = true },
		func(response *http.Response) { response.Header.Set("Content-Range", "bytes 0-0/2") },
		func(response *http.Response) { response.ContentLength = 2 },
	} {
		reader := newAttemptStreamV2HTTPTestReader(t, "http://127.0.0.1")
		reads := 0
		body := &attemptStreamV2HTTPTestBody{read: func([]byte) (int, error) { reads++; return 0, io.EOF }}
		reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body, ContentLength: 1}
			mutate(response)
			return response, nil
		})
		if value, err := reader.ReadMetadata(t.Context(), attemptHex32(sha256.Sum256([]byte("x"))), 1); err == nil || value != nil || body.closes != 1 || reads != 0 {
			t.Errorf("rejected response retained authority/ownership: value=%q error=%v closes=%d reads=%d", value, err, body.closes, reads)
		}
	}
}

// Chunked framing cannot hide changed bytes, a truncated body or an extra byte.
func TestAttemptStreamV2HTTPRejectsWrongHashLengthAndTrailingData(t *testing.T) {
	for _, raw := range []string{"modified", "origina", "originalx"} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusOK)
			response.(http.Flusher).Flush()
			_, _ = io.WriteString(response, raw)
		}))
		reader := newAttemptStreamV2HTTPTestReader(t, server.URL)
		value, err := reader.ReadMetadata(t.Context(), attemptHex32(sha256.Sum256([]byte("original"))), 8)
		server.Close()
		if err == nil || value != nil {
			t.Errorf("changed chunked bytes were accepted: actual=%q result=%q error=%v", raw, value, err)
		}
	}
}

// A blocked real response is canceled through a request barrier, not a sleep.
func TestAttemptStreamV2HTTPCancellationInterruptsOpen(t *testing.T) {
	entered, left := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(entered)
		<-request.Context().Done()
		close(left)
	}))
	defer server.Close()
	reader := newAttemptStreamV2HTTPTestReader(t, server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := reader.ReadMetadata(ctx, attemptHex32(sha256.Sum256([]byte("x"))), 1); result <- err }()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal("HTTP cancellation fixture never entered its handler")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("HTTP open lost cancellation: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal("HTTP cancellation did not join its request")
	}
	select {
	case <-left:
	case <-t.Context().Done():
		t.Fatal("HTTP server did not observe request cancellation")
	}
}

// Cancellation after a real partial body cannot turn its prefix into a result;
// closing the owned response joins the server-side canceled request.
func TestAttemptStreamV2HTTPCancellationRejectsPartialBody(t *testing.T) {
	left := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(response, "x")
		response.(http.Flusher).Flush()
		<-request.Context().Done()
		close(left)
	}))
	defer server.Close()
	reader := newAttemptStreamV2HTTPTestReader(t, server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body, err := reader.OpenData(ctx, AttemptStreamV2Records, attemptHex32(sha256.Sum256([]byte("xy"))), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	var first [1]byte
	if _, err := io.ReadFull(body, first[:]); err != nil || first[0] != 'x' {
		t.Fatalf("partial HTTP fixture did not reach a real body read: %q %v", first, err)
	}
	cancel()
	if _, err := io.ReadAll(body); !errors.Is(err, context.Canceled) {
		t.Fatalf("partial HTTP read lost cancellation: %v", err)
	}
	if err := body.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("partial HTTP close lost cancellation: %v", err)
	}
	select {
	case <-left:
	case <-t.Context().Done():
		t.Fatal("partial HTTP request did not terminate")
	}
}

// EOF does not erase a late real Close error; no verified bytes escape failure.
func TestAttemptStreamV2HTTPPreservesCloseErrorAndLateCancellation(t *testing.T) {
	for _, cancelAtClose := range []bool{false, true} {
		reader := newAttemptStreamV2HTTPTestReader(t, "http://127.0.0.1")
		ctx, cancel := context.WithCancel(t.Context())
		original := errors.New("actual body close failure")
		data := bytes.NewReader([]byte("x"))
		body := &attemptStreamV2HTTPTestBody{read: data.Read, close: func() error {
			if cancelAtClose {
				cancel()
				return nil
			}
			return original
		}}
		reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body, ContentLength: 1}, nil
		})
		value, err := reader.ReadMetadata(ctx, attemptHex32(sha256.Sum256([]byte("x"))), 1)
		cancel()
		want := original
		if cancelAtClose {
			want = context.Canceled
		}
		if !errors.Is(err, want) || value != nil || body.closes != 1 {
			t.Errorf("late close failure disappeared: value=%q error=%v closes=%d", value, err, body.closes)
		}
	}
}

// Data is not read during OpenData, and early close cannot certify a prefix.
func TestAttemptStreamV2HTTPDataOwnershipIsStreaming(t *testing.T) {
	reader := newAttemptStreamV2HTTPTestReader(t, "http://127.0.0.1")
	reads := 0
	data := bytes.NewReader([]byte("public data"))
	body := &attemptStreamV2HTTPTestBody{read: func(value []byte) (int, error) { reads++; return data.Read(value) }}
	reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-ndjson"}}, Body: body, ContentLength: 11}, nil
	})
	opened, err := reader.OpenData(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256([]byte("public data"))), 11)
	if err != nil || reads != 0 || opened == nil {
		t.Fatalf("OpenData read or failed: reads=%d error=%v", reads, err)
	}
	if err := opened.Close(); err == nil || body.closes != 1 {
		t.Fatalf("early close accepted incomplete data: closes=%d error=%v", body.closes, err)
	}
	if err := opened.Close(); err == nil || body.closes != 1 {
		t.Fatal("idempotent close lost its retained failure")
	}
}

// Transport errors accompanying EOF are not successful EOF; a broken zero-read
// source is refused at a deterministic work bound rather than spinning forever.
func TestAttemptStreamV2HTTPRetainsJoinedEOFErrorAndBoundsNoProgress(t *testing.T) {
	original := errors.New("terminal transport failure")
	for _, noProgress := range []bool{false, true} {
		reader := newAttemptStreamV2HTTPTestReader(t, "http://127.0.0.1")
		reads := 0
		body := &attemptStreamV2HTTPTestBody{read: func(value []byte) (int, error) {
			reads++
			if noProgress {
				return 0, nil
			}
			value[0] = 'x'
			return 1, errors.Join(io.EOF, original)
		}}
		reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body, ContentLength: -1}, nil
		})
		value, err := reader.ReadMetadata(t.Context(), attemptHex32(sha256.Sum256([]byte("x"))), 1)
		want, wantReads := original, 1
		if noProgress {
			want, wantReads = io.ErrNoProgress, 100
		}
		if !errors.Is(err, want) || value != nil || reads != wantReads || body.closes != 1 {
			t.Errorf("transport error/work bound: value=%q error=%v reads=%d closes=%d", value, err, reads, body.closes)
		}
	}
}

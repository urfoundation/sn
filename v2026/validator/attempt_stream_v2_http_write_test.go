//go:build linux || darwin

// Real HTTP observes wire identity, refreshed credentials and cancellation.
// Call-local transports expose late response failures without replacing replay.
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
	"sync/atomic"
	"testing"
)

// Independent existing fixture bounds do not authorize production capacity.
func newAttemptStreamV2HTTPTestWriter(t *testing.T, origin string, credential func() string) *HTTPAttemptStreamV2Writer {
	t.Helper()
	bounds, _ := attemptReplayV2TestBounds()
	writer, err := NewHTTPAttemptStreamV2Writer(origin, bounds, credential)
	if err != nil {
		t.Fatal(err)
	}
	return writer
}

// Every type carries raw exact bytes, its explicit length and the live token.
func TestAttemptStreamV2HTTPWriteExactTypesAndRefreshedSession(t *testing.T) {
	t.Parallel()
	data := []byte("{\"complete\":true}\n")
	hash := attemptHex32(sha256.Sum256(data))
	var calls atomic.Int32
	var credential atomic.Value
	credential.Store("first-private-fixture-token")
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/sn/attempt-artifact" || len(r.URL.Query()) != 2 || r.URL.Query().Get("hash") != hash || r.ContentLength != int64(len(data)) || r.Header.Get("Authorization") != "Bearer "+credential.Load().(string) || r.Header.Get("Accept-Encoding") != "identity" || len(r.TransferEncoding) != 0 {
			t.Error("typed upload request lost exact identity, bytes or current session")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		contentType := "application/x-ndjson"
		if r.URL.Query().Get("kind") == "metadata" {
			contentType = "application/json"
		}
		if r.Header.Get("Content-Type") != contentType {
			t.Error("typed content type changed")
		}
		got, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("raw upload differs: %v", err)
		}
		w.Header().Set("ETag", `"`+hash+`"`)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()
	writer := newAttemptStreamV2HTTPTestWriter(t, endpoint.URL, func() string { return credential.Load().(string) })
	for _, kind := range []string{"metadata", "records", "proofs"} {
		if err := writer.Write(t.Context(), kind, hash, data); err != nil {
			t.Fatal(err)
		}
		credential.Store("refreshed-private-fixture-token")
	}
	if calls.Load() != 3 {
		t.Fatal("typed upload request census differs")
	}
}

// Supplying credentials does not authorize cleartext arbitrary hosts or URL aliases.
func TestAttemptStreamV2HTTPWriteRequiresTrustedCredentialOrigin(t *testing.T) {
	t.Parallel()
	bounds, _ := attemptReplayV2TestBounds()
	for _, origin := range []string{"http://example.invalid", "http://localhost", "https://user:secret@example.invalid", "https://example.invalid/path", "https://example.invalid?hash=other"} {
		if writer, err := NewHTTPAttemptStreamV2Writer(origin, bounds, func() string { t.Fatal("constructor fetched credentials"); return "" }); err == nil || writer != nil {
			t.Fatalf("untrusted origin %q was admitted", origin)
		}
	}
	if writer, err := NewHTTPAttemptStreamV2Writer("https://example.invalid", bounds, nil); err == nil || writer != nil {
		t.Fatal("missing credential owner was admitted")
	}
	for _, origin := range []string{"https://example.invalid", "http://127.0.0.1:1", "http://[::1]:1"} {
		if _, err := NewHTTPAttemptStreamV2Writer(origin, bounds, func() string { return "" }); err != nil {
			t.Fatalf("explicit secure/loopback origin refused: %v", err)
		}
	}
}

// Malformed candidate data has no credential or transport side effects.
func TestAttemptStreamV2HTTPWriteRejectsBadObjectsBeforeCredentials(t *testing.T) {
	t.Parallel()
	data := []byte("data")
	hash := attemptHex32(sha256.Sum256(data))
	credentialCalls, httpCalls := 0, 0
	writer := newAttemptStreamV2HTTPTestWriter(t, "http://127.0.0.1:1", func() string { credentialCalls++; return "fixture" })
	writer.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) { httpCalls++; return nil, errors.New("unexpected HTTP") })
	for _, fault := range []string{"kind", "hash", "alias", "empty", "bytes", "capacity", "cancel", "nil-context"} {
		kind, contentHash, body, ctx := "metadata", hash, data, t.Context()
		switch fault {
		case "kind":
			kind = "other"
		case "hash":
			contentHash = "0x" + strings.Repeat("0", 64)
		case "alias":
			contentHash = strings.ToUpper(hash)
		case "empty":
			body = nil
		case "bytes":
			body = []byte("other")
		case "capacity":
			body = make([]byte, writer.metadataBytes+1)
			contentHash = attemptHex32(sha256.Sum256(body))
		case "cancel":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(t.Context())
			cancel()
		case "nil-context":
			ctx = nil
		}
		if err := writer.Write(ctx, kind, contentHash, body); err == nil {
			t.Fatalf("%s bad upload accepted", fault)
		}
	}
	if credentialCalls != 0 || httpCalls != 0 {
		t.Fatal("bad upload reached credential or network ownership")
	}
}

// The getter may observe or revoke, but it cannot rewrite already admitted bytes.
func TestAttemptStreamV2HTTPWriteOwnsBytesBeforeSessionCallback(t *testing.T) {
	t.Parallel()
	data, expected := []byte("original"), []byte("original")
	hash := attemptHex32(sha256.Sum256(expected))
	writer := newAttemptStreamV2HTTPTestWriter(t, "http://127.0.0.1:1", func() string { copy(data, []byte("tampered")); return "fixture" })
	writer.client.Transport = attemptStreamV2HTTPTestTransport(func(r *http.Request) (*http.Response, error) {
		got, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("credential callback redirected byte authority: %v", err)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{"Etag": {`"` + hash + `"`}}, Body: http.NoBody}, nil
	})
	if err := writer.Write(t.Context(), "metadata", hash, data); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(data, expected) {
		t.Fatal("ownership fixture did not mutate borrowed input")
	}
}

// Refreshed empty/invalid tokens and synchronous cancellation refuse before Do.
func TestAttemptStreamV2HTTPWriteRejectsRevokedOrInvalidSession(t *testing.T) {
	t.Parallel()
	data := []byte("data")
	for _, token := range []string{"", " leading", "line\nbreak", strings.Repeat("x", 16*1024+1), "cancel"} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		writer := newAttemptStreamV2HTTPTestWriter(t, "http://127.0.0.1:1", func() string {
			if token == "cancel" {
				cancel()
			}
			return token
		})
		writer.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected HTTP") })
		err := writer.Write(ctx, "metadata", attemptHex32(sha256.Sum256(data)), data)
		cancel()
		if err == nil || calls != 0 {
			t.Fatal("invalid/revoked session reached transport")
		}
	}
}

// Redirect targets never receive proof bytes or credentials, even on the same host.
func TestAttemptStreamV2HTTPWriteRefusesRedirects(t *testing.T) {
	t.Parallel()
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	writer := newAttemptStreamV2HTTPTestWriter(t, origin.URL, func() string { return "fixture" })
	data := []byte("data")
	if err := writer.Write(t.Context(), "records", attemptHex32(sha256.Sum256(data)), data); err == nil || redirected.Load() != 0 {
		t.Fatal("redirect became an upload acknowledgement")
	}
}

// Every status/header/body refusal closes the actual acquired response once.
func TestAttemptStreamV2HTTPWriteRejectsMalformedAcknowledgements(t *testing.T) {
	t.Parallel()
	data := []byte("data")
	hash := attemptHex32(sha256.Sum256(data))
	for _, fault := range []string{"status", "missing-hash", "wrong-hash", "duplicate-hash", "length", "encoding", "uncompressed", "partial", "body"} {
		body := &attemptStreamV2HTTPTestBody{read: bytes.NewReader(nil).Read}
		response := &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{"Etag": {`"` + hash + `"`}}, Body: body}
		switch fault {
		case "status":
			response.StatusCode = http.StatusTooManyRequests
		case "missing-hash":
			response.Header.Del("ETag")
		case "wrong-hash":
			response.Header.Set("ETag", `"other"`)
		case "duplicate-hash":
			response.Header.Add("ETag", `"`+hash+`"`)
		case "length":
			response.ContentLength = 1
		case "encoding":
			response.Header.Set("Content-Encoding", "gzip")
		case "uncompressed":
			response.Uncompressed = true
		case "partial":
			response.Header.Set("Content-Range", "bytes 0-1/2")
		case "body":
			body.read = bytes.NewReader([]byte("x")).Read
		}
		writer := newAttemptStreamV2HTTPTestWriter(t, "http://127.0.0.1:1", func() string { return "fixture" })
		writer.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) { return response, nil })
		if err := writer.Write(t.Context(), "metadata", hash, data); err == nil || body.closes != 1 {
			t.Fatalf("%s acknowledgement escaped exact closure: %v/%d", fault, err, body.closes)
		}
	}
}

// Late Close failures and cancellation cannot be hidden behind a successful status.
func TestAttemptStreamV2HTTPWriteRetainsLateCloseAndCancellation(t *testing.T) {
	t.Parallel()
	data := []byte("data")
	hash := attemptHex32(sha256.Sum256(data))
	for _, cancelOnClose := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		cause := errors.New("actual upload acknowledgement close failed")
		body := &attemptStreamV2HTTPTestBody{read: bytes.NewReader(nil).Read, close: func() error {
			if cancelOnClose {
				cancel()
			}
			return cause
		}}
		writer := newAttemptStreamV2HTTPTestWriter(t, "http://127.0.0.1:1", func() string { return "fixture" })
		writer.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{"Etag": {`"` + hash + `"`}}, Body: body}, nil
		})
		err := writer.Write(ctx, "metadata", hash, data)
		if !errors.Is(err, cause) || body.closes != 1 || cancelOnClose && !errors.Is(err, context.Canceled) {
			t.Fatalf("late acknowledgement failure escaped: %v", err)
		}
		cancel()
	}
}

// An actual in-flight HTTP request stops when its owning release is canceled.
func TestAttemptStreamV2HTTPWriteCancelsRealRequest(t *testing.T) {
	t.Parallel()
	entered, stopped := make(chan struct{}), make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(stopped)
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
	}))
	defer endpoint.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := newAttemptStreamV2HTTPTestWriter(t, endpoint.URL, func() string { return "fixture" })
	data := []byte("data")
	done := make(chan error, 1)
	go func() { done <- writer.Write(ctx, "metadata", attemptHex32(sha256.Sum256(data)), data) }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("actual request outlived release cancellation: %v", err)
	}
	<-stopped
}

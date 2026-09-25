//go:build linux || darwin

// Explicit logical deadline transitions verify the full get window without
// sleeps; actual readers, response closes and signed observations still run.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/sdk"
)

// A caller-owned deadline is advanced at retry waits, never by the scheduler.
type releaseHttpGetTestDeadline struct {
	context.Context
	deadline time.Time
	done     chan struct{}
}

// Requests still observe the production operation's actual admitted duration.
func (self *releaseHttpGetTestDeadline) Deadline() (time.Time, bool) { return self.deadline, true }

// The test closes this only at the exact chosen deadline transition.
func (self *releaseHttpGetTestDeadline) Done() <-chan struct{} { return self.done }

// Parent cancellation remains distinct from the operation's own expiry.
func (self *releaseHttpGetTestDeadline) Err() error {
	if err := self.Context.Err(); err != nil {
		return err
	}
	select {
	case <-self.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// One explicit step can model a stalled wait when a test is about retained
// negative custody rather than the independently tested retry pacing.
func releaseHttpGetTestDeadlineHooks(t testing.TB, elapsed *time.Duration, step time.Duration) releaseHttpGetRetryHooks {
	t.Helper()
	var owner *releaseHttpGetTestDeadline
	return releaseHttpGetRetryHooks{
		withTimeout: func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			if duration != 5*time.Minute {
				t.Fatalf("get retry window=%s, want five minutes", duration)
			}
			owner = &releaseHttpGetTestDeadline{Context: ctx, deadline: time.Now().Add(duration), done: make(chan struct{})}
			return owner, func() {
				select {
				case <-owner.done:
				default:
					close(owner.done)
				}
			}
		},
		wait: func(ctx context.Context, delay time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if delay < 2*time.Second {
				t.Fatal("retry lost its request pacing")
			}
			*elapsed += max(delay, step)
			if *elapsed >= 5*time.Minute {
				close(owner.done)
				return owner.Err()
			}
			return nil
		},
	}
}

// Persistent capacity responses consume five logical minutes, respect a server
// hint, retain their typed status, and stop without an additional request.
func TestReleaseHttpGetRetryReservesFullBudget(t *testing.T) {
	var elapsed time.Duration
	calls := 0
	refusal := &releaseHttpGetStatusError{endpoint: "https://retry.example", status: 503, retryAfter: 75 * time.Second}
	err := retryReleaseHttpGet(t.Context(), func(context.Context) error { calls++; return refusal }, releaseHttpGetTestDeadlineHooks(t, &elapsed, 0))
	if elapsed != 5*time.Minute || calls != 4 || !errors.Is(err, refusal) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("get budget ended early or lost cause: elapsed=%s calls=%d err=%v", elapsed, calls, err)
	}
}

// A ready retry must not issue a request after caller cancellation; independent
// integrity and close failures cannot be hidden behind transient status codes.
func TestReleaseHttpGetRetryPreservesCancellationAndMixedErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	err := retryReleaseHttpGet(ctx, func(context.Context) error { calls++; return context.DeadlineExceeded }, releaseHttpGetRetryHooks{wait: func(context.Context, time.Duration) error { cancel(); return nil }})
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation issued another get: calls=%d err=%v", calls, err)
	}
	broken := errors.New("synthetic response close failure")
	for _, cause := range []error{errors.Join(context.DeadlineExceeded, broken), &releaseHttpGetStatusError{status: 403}, errors.New("connection reset: invalid signature")} {
		calls = 0
		err := retryReleaseHttpGet(t.Context(), func(context.Context) error { calls++; return cause }, releaseHttpGetRetryHooks{wait: func(context.Context, time.Duration) error { t.Fatal("permanent response retried"); return nil }})
		if calls != 1 || !errors.Is(err, cause) {
			t.Fatalf("permanent cause changed: calls=%d err=%v", calls, err)
		}
	}
}

// Retries preserve the exact history scope, then the ordinary parser verifies
// the authentic content hash and signature. Failed bodies close before retry.
func TestArtifactHttpGetRetriesBeforeCanonicalRead(t *testing.T) {
	artifact, raw := validatorTestArtifact(t)
	history, err := json.Marshal(map[string]any{"schema": "urnetwork-payout-artifact-history-v1", "objects": []map[string]any{{"key": "blob/st/v1/history/test-deployment/521/4/1/" + strings.TrimPrefix(artifact.ContentHash, "sha256:") + ".json", "size": len(raw), "content_hash": artifact.ContentHash}}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewHTTPArtifactReader("https://artifact-retry.example", artifact.DeploymentID, 521)
	if err != nil {
		t.Fatal(err)
	}
	reader.retryHooks.wait = func(context.Context, time.Duration) error { return nil }
	calls, opened, closed := 0, 0, 0
	var historyUrl string
	reader.client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		if opened != closed {
			t.Fatal("retry started before the prior response closed")
		}
		if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > time.Minute || reader.client.Timeout != time.Minute {
			t.Fatal("get attempt did not retain its sixty-second request allowance")
		}
		calls++
		if request.URL.Path == "/sn/artifacts" {
			if historyUrl == "" {
				historyUrl = request.URL.String()
			} else if historyUrl != request.URL.String() {
				t.Fatal("retry changed its immutable history scope")
			}
		}
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		opened++
		status, value := http.StatusOK, history
		if request.URL.Path == "/sn/artifact" {
			value = raw
		}
		body := &attemptStreamV2HTTPTestBody{read: bytes.NewReader(value).Read, close: func() error { closed++; return nil }}
		if calls == 2 {
			status = http.StatusServiceUnavailable
		} else if calls == 3 {
			body.read = func(buffer []byte) (int, error) { return copy(buffer, value[:1]), io.ErrUnexpectedEOF }
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, ContentLength: int64(len(value)), Body: body}, nil
	})
	got, err := reader.Read(t.Context(), 4, 1)
	if err != nil || got == nil || got.ContentHash != artifact.ContentHash || calls != 5 || opened != closed {
		t.Fatalf("canonical artifact retry failed: calls=%d opened=%d closed=%d err=%v", calls, opened, closed, err)
	}
}

// Pre-observation retries may recover, but the resulting signed exchange is
// captured exactly once and later replay cannot consult or revise the network.
func TestArtifactHttpGetRetriesBeforeSignedObservation(t *testing.T) {
	reader := artifactHttpCaptureTest(t, "https://signed-retry.example")
	reader.reader.retryHooks.wait = func(context.Context, time.Duration) error { return nil }
	raw := []byte(`{"schema":"urnetwork-payout-artifact-history-v1","objects":[]}`)
	calls := 0
	reader.reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		status := http.StatusOK
		if calls <= 2 {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, ContentLength: int64(len(raw)), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})
	_, observedErr, hash, err := reader.capture(t.Context(), 1, 9)
	if err != nil || !errors.Is(observedErr, ErrArtifactUnavailable) || hash == "" || calls != 3 {
		t.Fatalf("pre-observation retries failed: calls=%d hash=%s err=%v observed=%v", calls, hash, err, observedErr)
	}
	expected, path, err := releaseArtifactHttpRequestV2(reader.cfg, reader.hotkey.PublicKey(), reader.decision, 9, 1, reader.reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := decodeArtifactHttpObservationV2(t.Context(), encoded, 1024*1024, expected)
	if err != nil || len(value.Exchanges) != 1 || value.Exchanges[0].Status != http.StatusOK || !bytes.Equal(value.Exchanges[0].Body, raw) {
		t.Fatalf("signed observation retained an abandoned retry: %+v %v", value.Exchanges, err)
	}
	reader.reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("signed replay issued another get")
		return nil, nil
	})
	_, repeatedErr, repeatedHash, err := reader.capture(t.Context(), 1, 9)
	if err != nil || repeatedHash != hash || repeatedErr.Error() != observedErr.Error() {
		t.Fatalf("signed observation changed during replay: %v %v", err, repeatedErr)
	}
}

// Server-key get retries close each response and capture only a fully decoded,
// unique key census after recovery; the original public key bytes are retained.
func TestReleaseServerKeyGetRetriesBeforeCapture(t *testing.T) {
	bounds, _ := attemptReplayV2TestBounds()
	cfg := &ReleaseConfig{Operators: []OperatorConfig{{NoID: 1, APIURL: "https://server-keys.example"}}}
	cfg.EvidenceV2.Bounds.Cut, cfg.EvidenceV2.Bounds.MaxOperators, cfg.EvidenceV2.Bounds.MaxControlBytes = bounds, 1, 4096
	public := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	raw, err := json.Marshal(sdk.VerifyKeysResult{Keys: []*sdk.VerifyServerKey{{ServerKeyId: 1, PublicKey: public}}})
	if err != nil {
		t.Fatal(err)
	}
	calls, closed, captures := 0, 0, 0
	client := &http.Client{Timeout: releaseHttpGetAttemptTimeout, Transport: attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/verify/keys" || request.Method != http.MethodGet || calls != closed {
			t.Fatal("server-key retry changed scope or left its response open")
		}
		calls++
		status := http.StatusOK
		if calls <= 2 {
			status = http.StatusTooManyRequests
		}
		body := &attemptStreamV2HTTPTestBody{read: bytes.NewReader(raw).Read, close: func() error { closed++; return nil }}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, ContentLength: int64(len(raw)), Body: body}, nil
	})}
	keys, err := readReleaseServerKeysV2WithCaptureAndRetry(t.Context(), cfg, func(_ ReleaseEvidenceV2CaptureSource, value []byte) error {
		captures++
		if !bytes.Equal(value, raw) || calls != closed {
			t.Fatal("capture preceded complete successful response custody")
		}
		return nil
	}, client, releaseHttpGetRetryHooks{wait: func(context.Context, time.Duration) error { return nil }})
	if err != nil || calls != 3 || closed != calls || captures != 1 || !reflect.DeepEqual(keys[1][1], public) {
		t.Fatalf("server-key retry changed final census: calls=%d closed=%d captures=%d err=%v", calls, closed, captures, err)
	}
}

// Permanent statuses, malformed framing and independent close failures never
// consume retry time or return data, even beside an otherwise transient code.
func TestArtifactHttpGetRejectsPermanentAndMixedResponses(t *testing.T) {
	for _, test := range []struct {
		status int
		media  string
		close  error
	}{
		{status: 400, media: "application/json"}, {status: 401, media: "application/json"},
		{status: 403, media: "application/json"}, {status: 404, media: "application/json"},
		{status: 409, media: "application/json"}, {status: 200, media: "text/plain"},
		{status: 503, media: "application/json", close: errors.New("synthetic independent close failure")},
		{status: 200, media: "application/json", close: errors.New("synthetic independent close failure")},
	} {
		reader, err := NewHTTPArtifactReader("https://permanent.example", "test-deployment", 521)
		if err != nil {
			t.Fatal(err)
		}
		reader.retryHooks.wait = func(context.Context, time.Duration) error { t.Fatal("permanent get response retried"); return nil }
		calls, closes := 0, 0
		reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": {test.media}}, ContentLength: 2, Body: &attemptStreamV2HTTPTestBody{read: bytes.NewReader([]byte("{}")).Read, close: func() error { closes++; return test.close }}}, nil
		})
		value, err := reader.get(t.Context(), "https://permanent.example/sn/artifacts", 100)
		if err == nil || value != nil || calls != 1 || closes != 1 || test.close != nil && !errors.Is(err, test.close) {
			t.Fatalf("permanent get lost failure or custody: status=%d calls=%d closes=%d err=%v", test.status, calls, closes, err)
		}
	}
}

// Captured close failures stay explicit raw evidence. An accompanying 503
// cannot trigger retry and silently replace that independent permanent cause.
func TestArtifactHttpGetCaptureRetainsMixedCloseFailure(t *testing.T) {
	reader := artifactHttpCaptureTest(t, "https://mixed-observation.example")
	reader.reader.retryHooks.wait = func(context.Context, time.Duration) error { t.Fatal("mixed captured failure retried"); return nil }
	broken := errors.New("synthetic independent close failure")
	calls := 0
	reader.reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": {"application/json"}}, Body: &attemptStreamV2HTTPTestBody{read: bytes.NewReader([]byte("{}")).Read, close: func() error { return broken }}}, nil
	})
	_, _, hash, err := reader.capture(t.Context(), 1, 9)
	if err != nil || hash == "" || calls != 1 {
		t.Fatalf("mixed capture was lost: calls=%d hash=%s err=%v", calls, hash, err)
	}
	expected, path, err := releaseArtifactHttpRequestV2(reader.cfg, reader.hotkey.PublicKey(), reader.decision, 9, 1, reader.reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := decodeArtifactHttpObservationV2(t.Context(), encoded, 1024*1024, expected)
	if err != nil || len(value.Exchanges) != 1 || value.Exchanges[0].CloseError != broken.Error() || value.Exchanges[0].Status != http.StatusServiceUnavailable {
		t.Fatalf("signed negative lost independent close failure: %+v %v", value.Exchanges, err)
	}
}

// A server-key outage cannot turn malformed keys or framing into retryable
// authority. Permanent responses and independent close errors remain fatal.
func TestReleaseServerKeyGetRejectsPermanentAndMixedResponses(t *testing.T) {
	bounds, _ := attemptReplayV2TestBounds()
	cfg := &ReleaseConfig{Operators: []OperatorConfig{{NoID: 1, APIURL: "https://server-key-refusal.example"}}}
	cfg.EvidenceV2.Bounds.Cut, cfg.EvidenceV2.Bounds.MaxOperators, cfg.EvidenceV2.Bounds.MaxControlBytes = bounds, 1, 4096
	for _, test := range []struct {
		status int
		media  string
		close  error
	}{
		{status: 403, media: "application/json"}, {status: 404, media: "application/json"},
		{status: 200, media: "text/plain"}, {status: 200, media: "application/json"},
		{status: 503, media: "application/json", close: errors.New("synthetic server-key close failure")},
	} {
		calls, closes := 0, 0
		client := &http.Client{Timeout: releaseHttpGetAttemptTimeout, Transport: attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": {test.media}}, Body: &attemptStreamV2HTTPTestBody{read: bytes.NewReader([]byte(`{"keys":[]}`)).Read, close: func() error { closes++; return test.close }}}, nil
		})}
		keys, err := readReleaseServerKeysV2WithCaptureAndRetry(t.Context(), cfg, func(ReleaseEvidenceV2CaptureSource, []byte) error {
			t.Fatal("invalid server keys were captured")
			return nil
		}, client, releaseHttpGetRetryHooks{wait: func(context.Context, time.Duration) error { t.Fatal("invalid server-key response retried"); return nil }})
		if err == nil || keys != nil || calls != 1 || closes != 1 || test.close != nil && !errors.Is(err, test.close) {
			t.Fatalf("server-key permanent refusal changed: calls=%d closes=%d err=%v", calls, closes, err)
		}
	}
}

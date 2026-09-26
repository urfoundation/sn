//go:build linux || darwin

// Real stream transports expose typed refusal and pacing to lifecycle owners;
// deterministic callbacks prove retry scope without sleeps or live endpoints.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// Both readers and writers preserve refusal status, response ownership and the
// single-request boundary. An untrusted error message cannot soften a hard code.
func TestAttemptStreamHttpRefusalPreservesStatusAndOwnership(t *testing.T) {
	t.Parallel()
	data := []byte("synthetic immutable object")
	hash := attemptHex32(sha256.Sum256(data))
	for _, status := range []int{400, 401, 403, 404, 408, 409, 425, 429, 500, 502, 503, 504} {
		for _, upload := range []bool{false, true} {
			calls := 0
			body := &attemptStreamV2HTTPTestBody{read: bytes.NewReader([]byte("connection reset; synthetic refusal")).Read}
			transport := attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"3600"}}, Body: body}, nil
			})
			var err error
			if upload {
				writer := newAttemptStreamV2HTTPTestWriter(t, "https://publication.example", func() string { return "synthetic-session" })
				writer.client.Transport = transport
				err = writer.Write(t.Context(), "metadata", hash, data)
			} else {
				reader := newAttemptStreamV2HTTPTestReader(t, "https://publication.example")
				reader.client.Transport = transport
				var got []byte
				got, err = reader.ReadMetadata(t.Context(), hash, uint64(len(data)))
				if got != nil {
					t.Fatalf("status %d returned unauthenticated bytes", status)
				}
			}
			var refusal *attemptStreamHttpStatusError
			if !errors.As(err, &refusal) || refusal.status != status || refusal.upload != upload || refusal.retryAfter != time.Hour || calls != 1 || body.closes != 1 {
				t.Fatalf("upload=%t status=%d calls=%d closes=%d err=%v", upload, status, calls, body.closes, err)
			}
			wantRetry := status == 408 || status == 425 || status == 429 || status >= 500
			if transientReleaseSnapshotError(err) != wantRetry {
				t.Fatalf("upload=%t status=%d retry classification differs: %v", upload, status, err)
			}
		}
	}
}

// Capacity must not erase an independent body-close or integrity failure, and
// cancellation is neutral only inside the existing replica publication owner.
func TestAttemptStreamHttpCapacityRetainsMixedFailureBoundary(t *testing.T) {
	t.Parallel()
	capacity := &attemptStreamHttpStatusError{status: http.StatusTooManyRequests, upload: true, retryAfter: time.Hour}
	integrity := errors.New("immutable object content hash differs")
	for _, err := range []error{
		errors.Join(capacity, integrity),
		errors.Join(capacity, context.Canceled),
		&attemptReplicaPublicationError{causes: []error{capacity, integrity, context.Canceled}},
		&attemptReplicaPublicationError{causes: []error{context.Canceled}},
	} {
		if transientReleaseSnapshotError(err) {
			t.Fatalf("mixed or canceled failure became retryable: %v", err)
		}
	}
	if !transientReleaseSnapshotError(&attemptReplicaPublicationError{causes: []error{capacity, context.Canceled}}) {
		t.Fatal("capacity lost its replica-owned sibling cancellation boundary")
	}
	data := []byte("synthetic immutable object")
	for _, upload := range []bool{false, true} {
		body := &attemptStreamV2HTTPTestBody{read: bytes.NewReader(nil).Read, close: func() error { return integrity }}
		transport := attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}, Body: body}, nil
		})
		hash := attemptHex32(sha256.Sum256(data))
		var err error
		if upload {
			writer := newAttemptStreamV2HTTPTestWriter(t, "https://publication.example", func() string { return "synthetic-session" })
			writer.client.Transport = transport
			err = writer.Write(t.Context(), "metadata", hash, data)
		} else {
			reader := newAttemptStreamV2HTTPTestReader(t, "https://publication.example")
			reader.client.Transport = transport
			_, err = reader.ReadMetadata(t.Context(), hash, uint64(len(data)))
		}
		if !errors.Is(err, integrity) || transientReleaseSnapshotError(err) || body.closes != 1 {
			t.Fatalf("upload=%t late close changed failure boundary: %v", upload, err)
		}
	}
}

// Only one canonical positive integer can pace a retry, never beyond one bucket.
func TestAttemptStreamHttpRetryAfterBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		values []string
		want   time.Duration
	}{
		{values: nil, want: 0},
		{values: []string{""}, want: 0},
		{values: []string{"0"}, want: 0},
		{values: []string{"-1"}, want: 0},
		{values: []string{"+12"}, want: 0},
		{values: []string{"012"}, want: 0},
		{values: []string{"12", "13"}, want: 0},
		{values: []string{"12, 13"}, want: 0},
		{values: []string{"Wed, 21 Oct 2015 07:28:00 GMT"}, want: 0},
		{values: []string{"18446744073709551616"}, want: 0},
		{values: []string{" 61 "}, want: 61 * time.Second},
		{values: []string{"3600"}, want: time.Hour},
		{values: []string{"18446744073709551615"}, want: time.Hour},
	} {
		if got := attemptStreamHttpRetryAfter(http.Header{"Retry-After": test.values}); got != test.want {
			t.Errorf("values=%q got=%s want=%s", test.values, got, test.want)
		}
	}
	joined := fmt.Errorf("publication: %w", &attemptReplicaPublicationError{causes: []error{
		&attemptStreamHttpStatusError{status: 429, retryAfter: 3 * time.Minute},
		errors.Join(&attemptStreamHttpStatusError{status: 503, retryAfter: time.Hour}, context.Canceled),
	}})
	if got := releaseSnapshotRetryDelayForError(joined); got != time.Hour {
		t.Fatalf("joined replica pacing=%s want=%s", got, time.Hour)
	}
	if got := releaseSnapshotRetryDelayForError(context.DeadlineExceeded); got != releaseSnapshotStartupRetryDelay {
		t.Fatalf("ordinary transport pacing changed to %s", got)
	}
}

// A real quota refusal retries the same immutable publication only after the
// server's bounded reset hint, then loads a fresh snapshot for its continuation.
func TestInitialReleasePublicationRetriesCapacityAfterResetHint(t *testing.T) {
	t.Parallel()
	initial, fresh := &ReleaseSnapshot{}, &ReleaseSnapshot{}
	advances, loads, waits := 0, 0, 0
	capacity := &attemptStreamHttpStatusError{status: http.StatusTooManyRequests, upload: true, retryAfter: time.Hour}
	err := advanceInitialReleaseWithRetry(t.Context(), initial, func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		return fresh, nil
	}, func(_ context.Context, snapshot *ReleaseSnapshot) error {
		advances++
		if advances == 1 {
			if snapshot != initial {
				t.Fatal("initial publication snapshot changed")
			}
			return &attemptReplicaPublicationError{causes: []error{capacity, context.Canceled}}
		}
		if snapshot != fresh {
			t.Fatal("publication continuation lost fresh snapshot")
		}
		return nil
	}, func(_ context.Context, delay time.Duration) error {
		waits++
		if delay != time.Hour {
			t.Fatalf("capacity retry ignored bucket reset: %s", delay)
		}
		return nil
	})
	if err != nil || advances != 2 || loads != 1 || waits != 1 {
		t.Fatalf("advances=%d loads=%d waits=%d err=%v", advances, loads, waits, err)
	}
}

// Persistent refusal stays finite; cancellation while waiting admits no new
// snapshot or publication. The wait callback controls both without wall time.
func TestInitialReleasePublicationCapacityKeepsAttemptAndCancellationBounds(t *testing.T) {
	t.Parallel()
	for _, cancelWait := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		capacity := &attemptStreamHttpStatusError{status: http.StatusTooManyRequests, retryAfter: time.Hour}
		advances, loads, waits := 0, 0, 0
		err := advanceInitialReleaseWithRetry(ctx, &ReleaseSnapshot{}, func(context.Context) (*ReleaseSnapshot, error) {
			loads++
			return &ReleaseSnapshot{}, nil
		}, func(context.Context, *ReleaseSnapshot) error {
			advances++
			return capacity
		}, func(context.Context, time.Duration) error {
			waits++
			if cancelWait {
				cancel()
				return ctx.Err()
			}
			return nil
		})
		cancel()
		if cancelWait {
			if !errors.Is(err, context.Canceled) || advances != 1 || loads != 0 || waits != 1 {
				t.Fatalf("canceled wait admitted work: advances=%d loads=%d waits=%d err=%v", advances, loads, waits, err)
			}
		} else if !errors.Is(err, capacity) || advances != releaseSnapshotStartupAttempts || loads != advances-1 || waits != advances-1 {
			t.Fatalf("capacity erased finite budget: advances=%d loads=%d waits=%d err=%v", advances, loads, waits, err)
		}
	}
}

// Retained reader replay uses the same refusal pacing as initial publication.
func TestReleaseSteererCapacityRetryUsesServerHint(t *testing.T) {
	t.Parallel()
	want := &ReleaseSteerer{}
	loads, waits := 0, 0
	got, err := loadReleaseSteererV2WithRetry(t.Context(), func() (*ReleaseSteerer, error) {
		loads++
		if loads == 1 {
			return nil, &attemptStreamHttpStatusError{status: http.StatusTooManyRequests, retryAfter: 37 * time.Second}
		}
		return want, nil
	}, func(_ context.Context, delay time.Duration) error {
		waits++
		if delay != 37*time.Second {
			t.Fatalf("reader retry ignored server hint: %s", delay)
		}
		return nil
	})
	if got != want || err != nil || loads != 2 || waits != 1 {
		t.Fatalf("reader replay loads=%d waits=%d result=%p err=%v", loads, waits, got, err)
	}
}

// All steering may retry capacity within its epoch; only explicit provisional
// permission may advance it. Mixed integrity still consumes the failure budget.
func TestProvisionalNativeCapacityWaitPreservesStrictFailureBudget(t *testing.T) {
	t.Parallel()
	capacity := &attemptStreamHttpStatusError{status: http.StatusTooManyRequests, upload: true}
	integrity := errors.New("immutable object content hash differs")
	for _, test := range []struct {
		provisional bool
		cause       error
		wantFailure bool
	}{
		{provisional: true, cause: capacity, wantFailure: false},
		{provisional: false, cause: capacity, wantFailure: false},
		{provisional: true, cause: errors.Join(capacity, integrity), wantFailure: true},
	} {
		attempts := 0
		err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) {
			if test.wantFailure || !test.provisional {
				return 700, nil
			}
			return 700 + uint64(attempts), nil
		}, func() error {
			attempts++
			if attempts <= releaseSteeringFailureLimit+2 {
				return test.cause
			}
			return nil
		}, func() bool { return attempts < releaseSteeringFailureLimit+3 }, test.provisional)
		if test.wantFailure {
			if !errors.Is(err, test.cause) || attempts != releaseSteeringFailureLimit {
				t.Fatalf("mixed integrity boundary changed: attempts=%d err=%v", attempts, err)
			}
		} else if err != nil || attempts != releaseSteeringFailureLimit+3 {
			t.Fatalf("capacity consumed restart budget: attempts=%d err=%v", attempts, err)
		}
	}
}

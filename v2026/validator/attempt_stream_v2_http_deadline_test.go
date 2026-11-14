//go:build linux || darwin

// Explicit parent/child states reproduce cancellation propagation ordering;
// no wall-clock expiry or scheduler race is required for the regression.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

// The parent expires after one buffered row. Its child has not yet received
// cancellation when mandatory Close must preserve the original interruption.
func TestAttemptStreamV2HttpClosePreservesParentDeadlineBeforePropagation(t *testing.T) {
	for _, closeFailure := range []error{nil, errors.New("synthetic independent close failure")} {
		raw := []byte("{}\n{}\n")
		expired := make(chan struct{})
		parent := &chainTestDeadlineContext{Context: t.Context(), done: expired}
		child, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		closed, visited := 0, 0
		body := &attemptStreamV2HTTPBody{ctx: child, parent: parent, cancel: cancel, remainingIO: time.Minute, remaining: uint64(len(raw)), expected: sha256.Sum256(raw), digest: sha256.New(), body: &attemptStreamV2HTTPTestBody{
			read: bytes.NewReader(raw).Read,
			close: func() error {
				closed++
				if child.Err() != nil || parent.Err() != context.DeadlineExceeded {
					t.Fatal("fixture lost the parent-before-child propagation boundary")
				}
				return closeFailure
			},
		}}
		chunk := AttemptStreamV2Chunk{Index: 0, FirstSequence: 1, LastSequence: 2, ItemCount: 2, DataBytes: uint64(len(raw)), ContentHash: attemptHex32(sha256.Sum256(raw))}
		err := walkAttemptStreamV2Chunk(parent, AttemptStreamV2Records, chunk, 32, func(context.Context, string, string, uint64) (io.ReadCloser, error) { return body, nil }, func(uint64, []byte) error {
			visited++
			close(expired)
			return nil
		})
		if !errors.Is(err, context.DeadlineExceeded) || RetryableEvidenceTransportError(err) != (closeFailure == nil) || visited != 1 || closed != 1 || closeFailure != nil && !errors.Is(err, closeFailure) {
			t.Fatalf("deadline cleanup lost retry scope: visited=%d closed=%d err=%v", visited, closed, err)
		}
		if again := body.Close(); closed != 1 || !errors.Is(again, context.DeadlineExceeded) {
			t.Fatalf("second close changed custody or lost deadline: closes=%d err=%v", closed, again)
		}
	}
}

// Actual typed bodies receive five minutes of network allowance and retain the
// direct caller context. Cpu replay does not add an http.Client wall deadline.
func TestAttemptStreamV2HttpReadReservesFiveMinuteAllowance(t *testing.T) {
	raw := []byte("{}\n")
	reader := newAttemptStreamV2HTTPTestReader(t, "https://compact-replay.example")
	reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-ndjson"}}, ContentLength: int64(len(raw)), Body: io.NopCloser(bytes.NewReader(raw))}, nil
	})
	started := time.Now()
	opened, err := reader.OpenData(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(raw)), uint64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	body := opened.(*attemptStreamV2HTTPBody)
	if reader.client.Timeout != 0 || body.parent != t.Context() || body.remainingIO > 5*time.Minute || body.remainingIO+time.Since(started) < 5*time.Minute {
		t.Fatalf("Get lost its caller or five-minute network budget: timeout=%s remaining=%s", reader.client.Timeout, body.remainingIO)
	}
	got, readErr := io.ReadAll(opened)
	if err := errors.Join(readErr, opened.Close()); err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("bounded Get lost exact-body verification: %v", err)
	}
}

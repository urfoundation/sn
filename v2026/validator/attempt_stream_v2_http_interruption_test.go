//go:build linux || darwin

// HTTP/2 reports a lost connection (failed ping), GOAWAY or stream reset
// through values private to net/http. These fixtures return the same shapes
// from a response body behind the actual reader. A live caller retries them
// from the chunk's first byte; a complete body that fails authentication, or
// a caller's own cancellation, stays final.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
)

// Mirrors net/http's private HTTP/2 StreamError: an opaque leaf whose As method
// the generic classifier refuses as foreign matching authority.
type attemptStreamV2HTTPTestStreamReset struct{}

func (attemptStreamV2HTTPTestStreamReset) Error() string {
	return "stream error: stream ID 3; INTERNAL_ERROR; received from peer"
}
func (attemptStreamV2HTTPTestStreamReset) As(any) bool { return false }

// Serves each request a fresh HTTP/2 response body. The first row arrives, then
// fail (when set) ends the body; otherwise the remaining bytes and EOF follow.
type attemptStreamV2HTTPInterruptionFixture struct {
	reader  *HTTPAttemptStreamV2Reader
	opened  *attemptStreamV2HTTPBody
	bodies  []*attemptStreamV2HTTPTestBody
	served  []byte
	fail    func(*attemptStreamV2HTTPInterruptionFixture) error
	visited int
}

func newAttemptStreamV2HTTPInterruptionFixture(t *testing.T, served []byte) *attemptStreamV2HTTPInterruptionFixture {
	fixture := &attemptStreamV2HTTPInterruptionFixture{reader: newAttemptStreamV2HTTPTestReader(t, "https://compact-replay.example"), served: served}
	fixture.reader.client.Transport = attemptStreamV2HTTPTestTransport(func(*http.Request) (*http.Response, error) {
		data, first := bytes.NewReader(fixture.served), bytes.IndexByte(fixture.served, '\n')+1
		fail := fixture.fail
		body := &attemptStreamV2HTTPTestBody{read: func(value []byte) (int, error) {
			if offset := len(fixture.served) - data.Len(); fail != nil && offset >= first {
				return 0, fail(fixture)
			} else if fail != nil {
				return data.Read(value[:min(len(value), first-offset)])
			}
			return data.Read(value)
		}}
		fixture.bodies = append(fixture.bodies, body)
		return &http.Response{StatusCode: http.StatusOK, Proto: "HTTP/2.0", ProtoMajor: 2, Header: http.Header{"Content-Type": {"application/x-ndjson"}}, ContentLength: int64(len(fixture.served)), Body: body}, nil
	})
	return fixture
}

// Walks one two-row chunk through OpenData exactly as compact replay does.
func (self *attemptStreamV2HTTPInterruptionFixture) walk(ctx context.Context, raw []byte, visit func(uint64, []byte) error) error {
	chunk := AttemptStreamV2Chunk{Index: 0, FirstSequence: 1, LastSequence: 2, ItemCount: 2, DataBytes: uint64(len(raw)), ContentHash: attemptHex32(sha256.Sum256(raw))}
	return walkAttemptStreamV2Chunk(ctx, AttemptStreamV2Records, chunk, 32, func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		opened, err := self.reader.OpenData(ctx, kind, hash, size)
		if body, ok := opened.(*attemptStreamV2HTTPBody); ok {
			self.opened = body
		}
		return opened, err
	}, func(index uint64, row []byte) error {
		self.visited++
		if visit != nil {
			return visit(index, row)
		}
		return nil
	})
}

// Startup reached this chunk walk through these wrappers on 2026-10-13.
func attemptStreamV2HTTPStartupError(err error) error {
	return fmt.Errorf("release V2 semantic startup: %w", fmt.Errorf("startup latest ordinary no_id %d: %w", 1, err))
}

// Each transport interruption before authenticated EOF retries; only the
// complete first row was visited, and the partial second row never was.
func TestAttemptStreamV2HTTPMidStreamInterruptionIsRetryable(t *testing.T) {
	raw := []byte("{\"row\":1}\n{\"row\":2}\n")
	for name, fail := range map[string]func(*attemptStreamV2HTTPInterruptionFixture) error{
		"lost ping": func(*attemptStreamV2HTTPInterruptionFixture) error {
			return errors.New("http2: client connection lost")
		},
		"stream reset": func(*attemptStreamV2HTTPInterruptionFixture) error { return attemptStreamV2HTTPTestStreamReset{} },
		"tcp reset": func(*attemptStreamV2HTTPInterruptionFixture) error {
			return &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
		},
		"eof mid-body": func(*attemptStreamV2HTTPInterruptionFixture) error { return io.ErrUnexpectedEOF },
		// HTTP/2 aborts the stream with context.Canceled when this body's own
		// I/O allowance expires; the cause stays DeadlineExceeded.
		"own I/O deadline": func(fixture *attemptStreamV2HTTPInterruptionFixture) error {
			fixture.opened.cancel(context.DeadlineExceeded)
			return context.Canceled
		},
	} {
		fixture := newAttemptStreamV2HTTPInterruptionFixture(t, raw)
		fixture.fail = fail
		err := fixture.walk(t.Context(), raw, nil)
		if err == nil || fixture.visited != 1 || len(fixture.bodies) != 1 || fixture.bodies[0].closes != 1 {
			t.Fatalf("%s: partial chunk escaped or lost custody: visited=%d bodies=%d error=%v", name, fixture.visited, len(fixture.bodies), err)
		}
		if !retryableProductionSteeringRead(err) || !RetryableEvidenceTransportError(err) || !retryableProductionSteeringRead(attemptStreamV2HTTPStartupError(err)) {
			t.Fatalf("%s: transport interruption became hard: %v", name, err)
		}
		if name == "lost ping" && (!strings.Contains(err.Error(), "compact attempt chunk ends before its complete JSONL rows: http2: client connection lost") || !strings.Contains(err.Error(), "attempt stream HTTP body closed before complete authenticated EOF")) {
			t.Fatalf("fixture differs from the production failure: %v", err)
		}
		if name == "own I/O deadline" && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("own deadline lost its cause: %v", err)
		}
	}
}

// The caller's own cancellation is not a transport interruption, and a
// complete body with changed bytes or a malformed row stays an integrity
// refusal under both classifiers.
func TestAttemptStreamV2HTTPInterruptionKeepsCancellationAndIntegrityFinal(t *testing.T) {
	raw := []byte("{\"row\":1}\n{\"row\":2}\n")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	canceled := newAttemptStreamV2HTTPInterruptionFixture(t, raw)
	canceled.fail = func(*attemptStreamV2HTTPInterruptionFixture) error { cancel(); return context.Canceled }
	err := canceled.walk(ctx, raw, nil)
	if !errors.Is(err, context.Canceled) || retryableProductionSteeringRead(err) || RetryableEvidenceTransportError(err) {
		t.Fatalf("caller cancellation authorized a retry: %v", err)
	}
	changed := newAttemptStreamV2HTTPInterruptionFixture(t, []byte("{\"row\":1}\n{\"row\":3}\n"))
	if err := changed.walk(t.Context(), raw, nil); err == nil || retryableProductionSteeringRead(err) || RetryableEvidenceTransportError(err) || retryableProductionSteeringRead(attemptStreamV2HTTPStartupError(err)) {
		t.Fatalf("hash mismatch from a complete body became retryable: %v", err)
	}
	malformed := []byte("{\"row\":1}\n{\"row\"\n")
	rows := newAttemptStreamV2HTTPInterruptionFixture(t, malformed)
	err = rows.walk(t.Context(), malformed, func(_ uint64, row []byte) error {
		var value struct {
			Row int `json:"row"`
		}
		return attemptStoreDecode(row[:len(row)-1], &value)
	})
	if err == nil || rows.visited != 2 || retryableProductionSteeringRead(err) || RetryableEvidenceTransportError(err) {
		t.Fatalf("malformed row from a complete body became retryable: visited=%d error=%v", rows.visited, err)
	}
}

// The production startup stage waits through a mid-stream loss and re-reads
// the chunk from its first byte; an integrity refusal returns at once.
func TestProductionStartupStageRetriesMidStreamLossAndRefusesIntegrity(t *testing.T) {
	raw := []byte("{\"row\":1}\n{\"row\":2}\n")
	cfg := &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion, PollSeconds: 0}
	fixture := newAttemptStreamV2HTTPInterruptionFixture(t, raw)
	attempts := 0
	err := awaitProductionStartupStage(t.Context(), cfg, nil, "disk and intent history", func(stage context.Context) error {
		attempts++
		fixture.fail = nil
		if attempts == 1 {
			fixture.fail = func(*attemptStreamV2HTTPInterruptionFixture) error {
				return errors.New("http2: client connection lost")
			}
		}
		fixture.visited = 0
		if err := fixture.walk(stage, raw, nil); err != nil {
			return fmt.Errorf("startup latest ordinary no_id %d: %w", 1, err)
		}
		return nil
	})
	if err != nil || attempts != 2 || len(fixture.bodies) != 2 || fixture.visited != 2 {
		t.Fatalf("startup did not re-read the chunk after a lost connection: attempts=%d bodies=%d visited=%d error=%v", attempts, len(fixture.bodies), fixture.visited, err)
	}
	changed := newAttemptStreamV2HTTPInterruptionFixture(t, []byte("{\"row\":1}\n{\"row\":3}\n"))
	attempts = 0
	err = awaitProductionStartupStage(t.Context(), cfg, nil, "disk and intent history", func(stage context.Context) error {
		attempts++
		return changed.walk(stage, raw, nil)
	})
	if err == nil || attempts != 1 {
		t.Fatalf("startup retried a hash mismatch: attempts=%d error=%v", attempts, err)
	}
}

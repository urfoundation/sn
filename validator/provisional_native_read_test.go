//go:build linux || darwin

// Force transport failure at the real compact reader boundary, then retry the
// unchanged bytes without a process restart or any native-intent credit.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// The real typed HTTP body retains its interruption through mandatory Close.
func TestFreshProvisionalNativeReadRetriesActualInterruptedChunk(t *testing.T) {
	raw := []byte("{}\n")
	chunk := AttemptStreamV2Chunk{Index: 0, FirstSequence: 1, LastSequence: 1, ItemCount: 1, DataBytes: uint64(len(raw)), ContentHash: attemptHex32(sha256.Sum256(raw))}
	attempts, visited, closed := 0, 0, 0
	err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return 21, nil }, func() error {
		attempts++
		readCtx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		var source io.Reader = bytes.NewReader(raw)
		if attempts <= releaseSteeringFailureLimit+2 {
			source = attemptReplayV2ReadFunc(func(buffer []byte) (int, error) { return copy(buffer, raw[:len(raw)-1]), context.DeadlineExceeded })
		}
		body := &attemptStreamV2HTTPBody{ctx: readCtx, cancel: cancel, remainingIO: time.Hour, remaining: chunk.DataBytes, expected: sha256.Sum256(raw), digest: sha256.New(), body: &attemptReplayV2TestReader{Reader: source, close: func() error { closed++; return nil }}}
		err := walkAttemptStreamV2Chunk(t.Context(), AttemptStreamV2Records, chunk, 32, func(context.Context, string, string, uint64) (io.ReadCloser, error) { return body, nil }, func(_ uint64, row []byte) error {
			if !json.Valid(row) {
				return errors.New("malformed compact JSONL row")
			}
			visited++
			return nil
		})
		if attempts <= releaseSteeringFailureLimit+2 && (!errors.Is(err, context.DeadlineExceeded) || visited != 0) {
			t.Fatalf("partial row was accepted or lost its timeout: %v", err)
		}
		return classifyProvisionalNativeRead(true, 21, err)
	}, func() bool { return attempts < releaseSteeringFailureLimit+3 }, false, true)
	if err != nil || attempts != releaseSteeringFailureLimit+3 || visited != 1 || closed != attempts {
		t.Fatalf("fresh preparation did not recover exact bytes: attempts=%d visited=%d closed=%d err=%v", attempts, visited, closed, err)
	}
}

// Clean EOF before the promised rows is malformed evidence, not a timeout.
// Hash/length/JSON failures and independent close failures remain nonretryable.
func TestFreshProvisionalNativeReadRejectsMalformedChunkAndMixedCauses(t *testing.T) {
	for _, test := range []struct {
		name    string
		raw     []byte
		items   uint64
		badHash bool
	}{
		{name: "missing newline", raw: []byte("{}"), items: 1},
		{name: "missing row", raw: []byte("{}\n"), items: 2},
		{name: "extra row", raw: []byte("{}\n{}\n"), items: 1},
		{name: "invalid JSON", raw: []byte("{broken}\n"), items: 1},
		{name: "wrong hash", raw: []byte("{}\n"), items: 1, badHash: true},
	} {
		chunk := AttemptStreamV2Chunk{Index: 0, FirstSequence: 1, LastSequence: test.items, ItemCount: test.items, DataBytes: uint64(len(test.raw)), ContentHash: attemptHex32(sha256.Sum256(test.raw))}
		if test.badHash {
			chunk.ContentHash = attemptHex32([32]byte{0x35})
		}
		err := walkAttemptStreamV2Chunk(t.Context(), AttemptStreamV2Records, chunk, 32, func(context.Context, string, string, uint64) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(test.raw)), nil
		}, func(_ uint64, raw []byte) error {
			var value any
			return json.Unmarshal(raw, &value)
		})
		var interrupted *provisionalNativeReadInterruption
		if err == nil || errors.As(classifyProvisionalNativeRead(true, 21, err), &interrupted) {
			t.Fatalf("%s became a successful or retryable read: %v", test.name, err)
		}
	}
	for _, cause := range []error{context.Canceled, io.EOF, io.ErrUnexpectedEOF, errors.New("context deadline exceeded"), errors.Join(context.DeadlineExceeded, errors.New("late close failed")), errors.Join(context.DeadlineExceeded, errors.New("compact attempt chunk content hash differs"))} {
		var interrupted *provisionalNativeReadInterruption
		if errors.As(classifyProvisionalNativeRead(true, 21, cause), &interrupted) {
			t.Fatalf("nontransport or mixed cause became retryable: %v", cause)
		}
	}
}

// An authorized interrupted read cannot erase prior durability failure or
// authorize an unrelated epoch, strict owner or mixed error. Unmarked transport
// may retry after an intent exists, while retaining strict epoch continuity.
func TestFreshProvisionalNativeReadKeepsFailureAndIntentGuards(t *testing.T) {
	interrupted := classifyProvisionalNativeRead(true, 21, fmt.Errorf("compact terminal replay: %w", context.DeadlineExceeded))
	for _, test := range []struct {
		name  string
		allow bool
		err   error
	}{
		{name: "strict", err: interrupted},
		{name: "wrong epoch", allow: true, err: classifyProvisionalNativeRead(true, 22, context.DeadlineExceeded)},
		{name: "mixed integrity", allow: true, err: errors.Join(interrupted, errors.New("signed journal changed"))},
	} {
		attempts := 0
		err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return 21, nil }, func() error { attempts++; return test.err }, func() bool { return attempts <= releaseSteeringFailureLimit }, false, test.allow)
		if err == nil || attempts != releaseSteeringFailureLimit {
			t.Fatalf("%s escaped failure budget: attempts=%d err=%v", test.name, attempts, err)
		}
	}
	broken := errors.New("signed journal durability failed")
	reads, attempts := 0, 0
	err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) {
		reads++
		if reads > 2 {
			return 22, nil
		}
		return 21, nil
	}, func() error {
		attempts++
		if attempts == 1 {
			return broken
		}
		return interrupted
	}, func() bool { return reads < 3 }, false, true)
	if !errors.Is(err, broken) || !strings.Contains(err.Error(), "incomplete epoch") || attempts != 2 {
		t.Fatalf("read retry erased an unresolved failure: %v", err)
	}
}

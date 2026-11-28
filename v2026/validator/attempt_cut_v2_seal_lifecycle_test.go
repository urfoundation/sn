//go:build linux || darwin

package validator

// Explicit synchronous barriers force cancellation and real close failures
// at ownership boundaries. No sleep, scheduler race or fake verification is
// used to prove that late errors cannot leak a signed header or partial result.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Nil or already-canceled context is refused before even an empty admission.
func TestAttemptCutV2SealCanceledBeforeIO(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 0)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, canceled} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		cut, result, err := SealAttemptCutV2(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
		if ctx != nil && !errors.Is(err, context.Canceled) || objects.writes != 0 || objects.reads != 0 {
			t.Fatalf("pre-canceled sealer touched storage: %v", err)
		}
		if _, err := os.Lstat(options.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("pre-canceled sealer created scratch: %v", err)
		}
	}
}

// Cancellation at every owned phase is observed before its next effect,
// including after all stream work and actual descriptor closure completes.
func TestAttemptCutV2SealCancellationAtOwnedBoundaries(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, phase := range []string{"head", "scratch-created", "after-spool-open", "before-spool-write", "before-spool-read", "before-fetch-back", "record-decoded", "record-checked", "record-indexed", "closed"} {
		ctx, cancel := context.WithCancel(context.Background())
		options, _ := newAttemptCutV2SealTestOptions(t, fixture)
		reached := false
		stop := func() { reached = true; cancel() }
		hooks := attemptCutV2SealHooks{}
		switch phase {
		case "head":
			hooks.HeadCaptured = func(AttemptLedgerHead) error { stop(); return nil }
		case "scratch-created":
			hooks.ScratchCreated = func(string) error { stop(); return nil }
		case "record-decoded":
			hooks.Replay.RecordDecoded = stop
		case "record-checked":
			hooks.Replay.RecordChecked = stop
		case "record-indexed":
			hooks.Replay.RecordIndexed = stop
		case "closed":
			hooks.Closed = func() error { stop(); return nil }
		default:
			hooks.Step = func(operation, _ string) error {
				if operation == phase {
					stop()
				}
				return nil
			}
		}
		cut, result, err := sealAttemptCutV2WithHooks(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, hooks)
		cancel()
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
		if !reached || !errors.Is(err, context.Canceled) {
			t.Fatalf("%s cancellation was not the actual boundary: reached=%t %v", phase, reached, err)
		}
	}
}

// Callbacks may cancel after a successful durable write or fetch; returning
// nil from that callback cannot authorize continued work or a final signature.
func TestAttemptCutV2SealCallbackCancellationFailsClosed(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, phase := range []string{"records-write", "proofs-write", "metadata-write", "metadata-read", "data-open", "data-close"} {
		ctx, cancel := context.WithCancel(context.Background())
		options, _ := newAttemptCutV2SealTestOptions(t, fixture)
		reached := false
		stop := func() { reached = true; cancel() }
		wrap := func(write AttemptStreamV2ObjectWriter) AttemptStreamV2ObjectWriter {
			return func(ctx context.Context, hash string, raw []byte) error {
				if err := write(ctx, hash, raw); err != nil {
					return err
				}
				stop()
				return nil
			}
		}
		switch phase {
		case "records-write":
			options.WriteRecords = wrap(options.WriteRecords)
		case "proofs-write":
			options.WriteProofs = wrap(options.WriteProofs)
		case "metadata-write":
			options.WriteMetadata = wrap(options.WriteMetadata)
		case "metadata-read":
			read := options.ReadMetadata
			options.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
				raw, err := read(ctx, hash, size)
				stop()
				return raw, err
			}
		case "data-open", "data-close":
			open := options.OpenData
			options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
				reader, err := open(ctx, kind, hash, size)
				if err != nil {
					return reader, err
				}
				if phase == "data-open" {
					stop()
					return reader, nil
				}
				return &attemptCutV2SealTestClosingReader{ReadCloser: reader, afterClose: func() error { stop(); return nil }}, nil
			}
		}
		cut, result, err := SealAttemptCutV2(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		cancel()
		assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
		if !reached || !errors.Is(err, context.Canceled) {
			t.Fatalf("%s callback cancellation was ignored: %v", phase, err)
		}
	}
}

// A wrapper still closes its actual underlying stream before surfacing a
// callback or close error; it never replaces record or projection verification.
type attemptCutV2SealTestClosingReader struct {
	io.ReadCloser
	afterClose func() error
}

// Join both real close and the explicit post-close boundary result.
func (self *attemptCutV2SealTestClosingReader) Close() error {
	return errors.Join(self.ReadCloser.Close(), self.afterClose())
}

// An opener returning both a stream and an error still transfers ownership
// for closure; proof open errors occur only after all record streams close.
func TestAttemptCutV2SealClosesReadersReturnedWithError(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, failedKind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		open, closed, reached := options.OpenData, 0, false
		failure := errors.New("data opener returned owned reader with error")
		options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			if kind != failedKind {
				return open(ctx, kind, hash, size)
			}
			reached = true
			return &attemptCutV2SealTestReader{Reader: bytes.NewReader(objects.dataKVs[kind][hash]), close: func() error { closed++; return nil }}, failure
		}
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, failure.Error())
		if !reached || closed != 1 || !errors.Is(err, failure) {
			t.Fatalf("%s owned reader was not closed exactly once: reached=%t closes=%d error=%v", failedKind, reached, closed, err)
		}
	}
}

// A valid final byte/hash is insufficient when releasing its real stream
// reports failure. The sealer must join that error and retain no acceptance.
func TestAttemptCutV2SealReaderCloseFailurePreventsSignature(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	for _, failedKind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
		options, _ := newAttemptCutV2SealTestOptions(t, fixture)
		open, closed := options.OpenData, 0
		failure := errors.New("staged reader close failed")
		options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			reader, err := open(ctx, kind, hash, size)
			if err != nil || kind != failedKind {
				return reader, err
			}
			return &attemptCutV2SealTestClosingReader{ReadCloser: reader, afterClose: func() error { closed++; return failure }}, nil
		}
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		assertAttemptCutV2SealTestFailure(t, cut, result, err, failure.Error())
		if closed != 1 || !errors.Is(err, failure) {
			t.Fatalf("%s late close failure was not joined exactly once: %v", failedKind, err)
		}
	}
}

// Force an actual os.File double-close rather than replacing the close
// verdict; all other descriptor owners must still be released afterward.
func TestAttemptCutV2SealDescriptorCloseFailurePreventsSignature(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	var files []*os.File
	cut, result, err := sealAttemptCutV2WithHooks(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, attemptCutV2SealHooks{BeforeClose: func(name string, file *os.File) {
		files = append(files, file)
		if name == "records.descriptors" {
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}})
	assertAttemptCutV2SealTestFailure(t, cut, result, err, "")
	if !errors.Is(err, os.ErrClosed) || len(files) != 2 || objects.closes != len(objects.dataKVs[AttemptStreamV2Records])+len(objects.dataKVs[AttemptStreamV2Proofs]) {
		t.Fatalf("real late close failure did not follow complete replay: files=%d closes=%d error=%v", len(files), objects.closes, err)
	}
	for _, file := range files {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("remaining actual descriptor was not closed: %v", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(options.ScratchDirectory, "records.descriptors")); err != nil {
		t.Fatalf("failed close erased staged descriptor evidence: %v", err)
	}
}

// The last owned-close barrier runs after full replay and actual descriptor
// closure, and before the public function can return its newly signed header.
func TestAttemptCutV2SealClosesScratchBeforeReturningHeader(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	var files []*os.File
	closed := false
	cut, result, err := sealAttemptCutV2WithHooks(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options, attemptCutV2SealHooks{BeforeClose: func(_ string, file *os.File) {
		files = append(files, file)
	}, Closed: func() error {
		closed = true
		if len(files) != 2 || objects.closes != len(objects.dataKVs[AttemptStreamV2Records])+len(objects.dataKVs[AttemptStreamV2Proofs]) {
			return errors.New("owned close barrier preceded complete replay")
		}
		for _, file := range files {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("owned close barrier preceded actual descriptor closure")
			}
		}
		return nil
	}})
	if err != nil || !closed {
		t.Fatalf("successful sealer missed final owned-close barrier: %v", err)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, result, fixture.recordTs, 1, 0)
}

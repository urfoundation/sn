//go:build linux || darwin

package validator

// Adjacent replay tests force acquisition, actual descriptor-open and staged
// indexing boundaries. They use the real scratch backend and no timing races.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// Inspect only the already-closed, test-owned scratch index without writes.
func attemptReplayV2ScratchEntryCount(t *testing.T, path string) int {
	t.Helper()
	db, err := leveldb.OpenFile(path, &opt.Options{ReadOnly: true, Strict: opt.StrictAll})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	iterator := db.NewIterator(nil, &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	defer iterator.Release()
	count := 0
	for iterator.Next() {
		count++
	}
	if err := iterator.Error(); err != nil {
		t.Fatal(err)
	}
	return count
}

// The acquired reader closes exactly once even when both opening and closing
// fail and cancellation arrives at acquisition, for either authenticated kind.
func TestAttemptCutV2ReplayOpenFailureRetainsCloseAndCancellation(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	for _, failedKind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
		ctx, cancel := context.WithCancel(context.Background())
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		open := options.OpenData
		openFailure, closeFailure := errors.New("acquired stream failed to open"), errors.New("acquired stream failed to close")
		opened, closed, failedCloses := 0, 0, 0
		options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			reader, err := open(ctx, kind, hash, size)
			if err != nil {
				return reader, err
			}
			opened++
			wrapped := &attemptReplayV2TestReader{Reader: reader, close: func() error {
				closed++
				if kind == failedKind {
					failedCloses++
					return errors.Join(reader.Close(), closeFailure)
				}
				return reader.Close()
			}}
			if kind == failedKind {
				cancel()
				return wrapped, openFailure
			}
			return wrapped, nil
		}
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(ctx, cut, cut.Context, bounds, options)
		cancel()
		if !errors.Is(err, openFailure) || !errors.Is(err, closeFailure) || !errors.Is(err, context.Canceled) || result != (AttemptCutV2ReplayResult{}) || opened != closed || failedCloses != 1 {
			t.Fatalf("%s: opened/closed=%d/%d failed closes=%d result=%+v error=%v", failedKind, opened, closed, failedCloses, result, err)
		}
	}
}

// Neither a nil successful body nor a nil failed body can admit a record.
func TestAttemptCutV2ReplayNilOpenerResultsFailClosed(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	failure := errors.New("no body acquired")
	for _, openErr := range []error{nil, failure} {
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		visits := 0
		options.OpenData = func(context.Context, string, string, uint64) (io.ReadCloser, error) { return nil, openErr }
		options.VisitRecord = func(AttemptRecord) error { visits++; return nil }
		bounds, _ := attemptReplayV2TestBounds()
		result, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, options)
		if err == nil || openErr != nil && !errors.Is(err, openErr) || visits != 0 || result != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("open=%v visits=%d result=%+v error=%v", openErr, visits, result, err)
		}
	}
}

// Swap after the storage path check, before the actual descriptor is opened.
// The unrelated inode must see no parent sync, LOCK creation or backend file.
func TestAttemptCutV2ReplayActualStorageDescriptorPrecedesMutation(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	for _, replaceParent := range []bool{false, true} {
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		parent := filepath.Dir(options.ScratchDirectory)
		preserved := filepath.Join(t.TempDir(), "preserved")
		var swapped atomic.Bool
		var parentSyncs atomic.Int32
		hooks := attemptCutV2ReplayHooks{ScratchStorageStep: func(operation, name string) error {
			if operation == "before-directory-sync" && name == "parent" {
				parentSyncs.Add(1)
			}
			if operation != "after-directory-check" || !swapped.CompareAndSwap(false, true) {
				return nil
			}
			if replaceParent {
				if err := os.Rename(parent, preserved); err != nil {
					return err
				}
				if err := os.Mkdir(parent, 0o700); err != nil {
					return err
				}
			} else if err := os.Rename(options.ScratchDirectory, preserved); err != nil {
				return err
			}
			return os.Mkdir(options.ScratchDirectory, 0o700)
		}}
		bounds, _ := attemptReplayV2TestBounds()
		result, err := replayAttemptCutV2WithHooks(context.Background(), cut, cut.Context, bounds, options, hooks)
		entries, readErr := os.ReadDir(options.ScratchDirectory)
		if err == nil || result != (AttemptCutV2ReplayResult{}) || !swapped.Load() || parentSyncs.Load() != 0 || readErr != nil || len(entries) != 0 {
			t.Fatalf("parent=%t swap=%t parent syncs=%d files=%d result=%+v error=%v read=%v", replaceParent, swapped.Load(), parentSyncs.Load(), len(entries), result, err, readErr)
		}
	}
}

// Cancellation at fresh directory ownership leaves its empty inode intact and
// prevents storage acquisition, metadata reads and data-reader acquisition.
func TestAttemptCutV2ReplayScratchCreationCancellationDoesNoBackendIO(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	metadataReads, dataOpens := 0, 0
	options.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) { metadataReads++; return nil, nil }
	options.OpenData = func(context.Context, string, string, uint64) (io.ReadCloser, error) { dataOpens++; return nil, nil }
	var storageSteps atomic.Int32
	hooks := attemptCutV2ReplayHooks{
		ScratchCreated:     func(string) error { cancel(); return nil },
		ScratchStorageStep: func(string, string) error { storageSteps.Add(1); return nil },
	}
	bounds, _ := attemptReplayV2TestBounds()
	result, err := replayAttemptCutV2WithHooks(ctx, cut, cut.Context, bounds, options, hooks)
	entries, readErr := os.ReadDir(options.ScratchDirectory)
	if !errors.Is(err, context.Canceled) || result != (AttemptCutV2ReplayResult{}) || storageSteps.Load() != 0 || metadataReads != 0 || dataOpens != 0 || readErr != nil || len(entries) != 0 {
		t.Fatalf("storage=%d metadata=%d opens=%d files=%d result=%+v error=%v read=%v", storageSteps.Load(), metadataReads, dataOpens, len(entries), result, err, readErr)
	}
}

// The context is rechecked after a storage scheduling hook, before the actual
// directory acquisition can cause a sync, owner lock or backend write.
func TestAttemptCutV2ReplayStorageCheckCancellationDoesNoMutation(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	var checks atomic.Int32
	hooks := attemptCutV2ReplayHooks{ScratchStorageStep: func(operation, name string) error {
		if operation == "after-directory-check" {
			checks.Add(1)
			cancel()
		}
		return nil
	}}
	bounds, _ := attemptReplayV2TestBounds()
	result, err := replayAttemptCutV2WithHooks(ctx, cut, cut.Context, bounds, options, hooks)
	entries, readErr := os.ReadDir(options.ScratchDirectory)
	if !errors.Is(err, context.Canceled) || result != (AttemptCutV2ReplayResult{}) || checks.Load() != 1 || readErr != nil || len(entries) != 0 {
		t.Fatalf("checks=%d files=%d result=%+v error=%v read=%v", checks.Load(), len(entries), result, err, readErr)
	}
}

// A canceled authenticated record never enters the scratch lifecycle index,
// including cancellation after its prior-record/lifecycle checks complete.
func TestAttemptCutV2ReplayPostAuthenticationCancellationLeavesEmptyIndex(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	for _, afterLifecycle := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		decoded, checked, indexed, visited := 0, 0, 0, 0
		hooks := attemptCutV2ReplayHooks{
			RecordDecoded: func() {
				decoded++
				if !afterLifecycle {
					cancel()
				}
			},
			RecordChecked: func() { checked++; cancel() },
			RecordIndexed: func() { indexed++ },
		}
		options.VisitRecord = func(AttemptRecord) error { visited++; return nil }
		bounds, _ := attemptReplayV2TestBounds()
		result, err := replayAttemptCutV2WithHooks(ctx, cut, cut.Context, bounds, options, hooks)
		cancel()
		wantChecked := 0
		if afterLifecycle {
			wantChecked = 1
		}
		if !errors.Is(err, context.Canceled) || result != (AttemptCutV2ReplayResult{}) || decoded != 1 || checked != wantChecked || indexed != 0 || visited != 0 {
			t.Fatalf("afterLifecycle=%t decoded=%d checked=%d indexed=%d visited=%d result=%+v error=%v", afterLifecycle, decoded, checked, indexed, visited, result, err)
		}
		if count := attemptReplayV2ScratchEntryCount(t, options.ScratchDirectory); count != 0 {
			t.Fatalf("afterLifecycle=%t: canceled record persisted %d scratch entries", afterLifecycle, count)
		}
	}
}

// A record already indexed when cancellation arrives is retained only as
// failed scratch evidence; no caller visitor or successful result is exposed.
func TestAttemptCutV2ReplayPostIndexCancellationPreventsVisitor(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	indexed, visited := 0, 0
	hooks := attemptCutV2ReplayHooks{RecordIndexed: func() { indexed++; cancel() }}
	options.VisitRecord = func(AttemptRecord) error { visited++; return nil }
	bounds, _ := attemptReplayV2TestBounds()
	result, err := replayAttemptCutV2WithHooks(ctx, cut, cut.Context, bounds, options, hooks)
	if !errors.Is(err, context.Canceled) || result != (AttemptCutV2ReplayResult{}) || indexed != 1 || visited != 0 {
		t.Fatalf("indexed=%d visited=%d result=%+v error=%v", indexed, visited, result, err)
	}
	if count := attemptReplayV2ScratchEntryCount(t, options.ScratchDirectory); count != 1 {
		t.Fatalf("expected the one staged pending record, found %d scratch entries", count)
	}
}

// A synchronous reader can deliver cancellation with its row or final EOF.
type attemptReplayV2ReadFunc func([]byte) (int, error)

// Implements only the read boundary; resource closing remains independently counted.
func (self attemptReplayV2ReadFunc) Read(buffer []byte) (int, error) { return self(buffer) }

// Cancellation during a read prevents the next row admission or successful
// EOF/hash acceptance, while the exact acquired reader still closes once.
func TestAttemptCutV2ReplayReadAndEOFCancellationStopsAdmission(t *testing.T) {
	for _, atEOF := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		raw := []byte("{}\n")
		input := bytes.NewReader(raw)
		closed, visits := 0, 0
		reader := &attemptReplayV2TestReader{Reader: attemptReplayV2ReadFunc(func(buffer []byte) (int, error) {
			n, err := input.Read(buffer)
			if !atEOF || errors.Is(err, io.EOF) {
				cancel()
			}
			return n, err
		}), close: func() error { closed++; return nil }}
		chunk := AttemptStreamV2Chunk{Index: 0, FirstSequence: 1, LastSequence: 1, ItemCount: 1, DataBytes: uint64(len(raw)), ContentHash: attemptHex32(sha256.Sum256(raw))}
		err := walkAttemptStreamV2Chunk(ctx, AttemptStreamV2Records, chunk, 32, func(context.Context, string, string, uint64) (io.ReadCloser, error) { return reader, nil }, func(uint64, []byte) error { visits++; return nil })
		cancel()
		wantVisits := 0
		if atEOF {
			wantVisits = 1
		}
		if !errors.Is(err, context.Canceled) || closed != 1 || visits != wantVisits {
			t.Fatalf("EOF=%t closed=%d visits=%d error=%v", atEOF, closed, visits, err)
		}
	}
}

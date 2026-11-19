//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAttemptCutV2ReplaySyncOnceAfterCompleteProofs(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	bounds, _ := attemptReplayV2TestBounds()
	var indexed, syncs atomic.Int32
	hooks := attemptCutV2ReplayHooks{
		RecordIndexed: func() { indexed.Add(1) },
		ScratchStorageStep: func(operation, name string) error {
			if operation == "before-file-sync" && strings.HasSuffix(name, ".log") {
				syncs.Add(1)
				if indexed.Load() != int32(len(records)) {
					return errors.New("replay synced its WAL before completing all records")
				}
			}
			return nil
		},
	}
	result, err := replayAttemptCutV2WithHooks(t.Context(), cut, cut.Context, bounds, options, hooks)
	if err != nil || syncs.Load() != 1 || result.CompleteCount != 2 || result.TrailCount != 2 || result.Records.ItemCount != uint64(len(records)) || result.Proofs.ItemCount != 2 {
		t.Fatalf("synced replay: syncs=%d indexed=%d result=%+v err=%v", syncs.Load(), indexed.Load(), result, err)
	}
	// The final sync rewrites an existing entry, preserving the logical index.
	if entries := attemptReplayV2ScratchEntryCount(t, options.ScratchDirectory); entries != 4 {
		t.Fatalf("final sync changed trail/proof index: %d entries", entries)
	}
}

func TestAttemptCutV2ReplayFinalSyncFailureAndCancellation(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	bounds, _ := attemptReplayV2TestBounds()
	for _, boundary := range []string{"before-file-sync", "after-file-sync", "after-file-close", "cancel-after-file-sync"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
			failure := errors.New("final scratch sync/close failed")
			var injected atomic.Bool
			hooks := attemptCutV2ReplayHooks{ScratchStorageStep: func(operation, name string) error {
				if !strings.HasSuffix(name, ".log") {
					return nil
				}
				if boundary == "cancel-after-file-sync" && operation == "after-file-sync" {
					injected.Store(true)
					cancel()
					return nil
				}
				if operation == boundary {
					injected.Store(true)
					return failure
				}
				return nil
			}}
			result, err := replayAttemptCutV2WithHooks(ctx, cut, cut.Context, bounds, options, hooks)
			want := failure
			if boundary == "cancel-after-file-sync" {
				want = context.Canceled
			}
			if !injected.Load() || !errors.Is(err, want) || result != (AttemptCutV2ReplayResult{}) {
				t.Fatalf("late failure escaped: injected=%t result=%+v err=%v", injected.Load(), result, err)
			}
		})
	}
}

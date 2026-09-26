//go:build linux || darwin

// Real signed ledger cuts retain their reservation and counters through a
// stale snapshot followed by immutable upload outages in the native loop.
package validator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"testing"
)

// The production writer emits actual typed 500/502 and post timeout errors.
// Repeated sealing must reuse its exact first object and rotate only once.
func TestReleaseSteeringTransportV2PreservesReservedCutAndCounters(t *testing.T) {
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	before := fixture.runtime.stats.snapshotStats()
	head, err := fixture.runtime.ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	stale := *fixture.snapshot
	stale.BlockNumber--
	stale.BlockHash = [32]byte{0x71}
	path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 17, 9)
	writer, err := NewHTTPAttemptStreamV2Writer("https://publication.example", fixture.options.Stats.Bounds, func() string { return "synthetic-session" })
	if err != nil {
		t.Fatal(err)
	}
	attempts, interruptions := 0, 0
	var firstHash string
	var firstBytes []byte
	writer.client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		raw, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			return nil, readErr
		}
		if err := request.Body.Close(); err != nil {
			return nil, err
		}
		hash := request.URL.Query().Get("hash")
		if request.Method != http.MethodPost || request.URL.Query().Get("kind") != AttemptStreamV2Records {
			t.Fatal("cut upload changed its typed request")
		}
		if attempts <= releaseSteeringFailureLimit+3 {
			interruptions++
			if firstBytes == nil {
				firstHash, firstBytes = hash, bytes.Clone(raw)
			} else if hash != firstHash || !bytes.Equal(raw, firstBytes) {
				t.Fatal("retry changed the immutable record upload")
			}
			if interruptions%3 == 0 {
				return nil, context.DeadlineExceeded
			}
			status := http.StatusBadGateway
			if interruptions%3 == 1 {
				status = http.StatusInternalServerError
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader([]byte("synthetic dependency: connection refused")))}, nil
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{"Etag": {`"` + hash + `"`}}, Body: http.NoBody}, nil
	})
	var committed ReleaseMeasurementInput
	err = runReleaseSteeringLoopWithWait(t.Context(), func() (uint64, error) { return 17, nil }, func() error {
		attempts++
		options := fixture.fresh(t)
		writeRecords := options.Stats.Seal.WriteRecords
		options.Stats.Seal.WriteRecords = func(ctx context.Context, hash string, raw []byte) error {
			if err := writer.Write(ctx, AttemptStreamV2Records, hash, raw); err != nil {
				return err
			}
			return writeRecords(ctx, hash, raw)
		}
		snapshot := fixture.snapshot
		if attempts == 1 {
			snapshot = &stale
		}
		input, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 17, 200, fixture.nativeHash, snapshot, options)
		if attempts <= releaseSteeringFailureLimit+3 {
			if retryable, _ := classifyReleasePreparationRetry(err); !retryable || !reflect.DeepEqual(input, ReleaseMeasurementInput{}) {
				t.Fatalf("interrupted cut escaped its retry boundary: attempt=%d error=%v", attempts, err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("interrupted upload published a journal: %v", err)
			}
			if !fixture.runtime.stats.attemptCutPending || fixture.runtime.objects.writes != 0 || !reflect.DeepEqual(fixture.runtime.stats.snapshotStats(), before) {
				t.Fatal("interrupted upload changed counters or lost its reserved cut")
			}
			if err := fixture.runtime.stats.beginAttempt(42, fixture.runtime.ledger); !errors.Is(err, errAttemptCutPending) {
				t.Fatalf("new trail crossed the retained cut reservation: %v", err)
			}
		} else {
			committed = input
		}
		return err
	}, func() bool { return attempts < releaseSteeringFailureLimit+4 })
	if err != nil || attempts != releaseSteeringFailureLimit+4 || interruptions != releaseSteeringFailureLimit+2 || committed.AttemptCutV2 == nil || committed.AttemptCutV2.LastSequence != head.LastSequence || committed.AttemptCutV2.Root != head.Root || committed.AttemptCutV2.CompleteCount != 1 || fixture.runtime.stats.attemptCutPending || fixture.runtime.stats.egressGeneration != fixture.runtime.initialEgressGeneration+1 {
		t.Fatalf("cut did not recover its exact evidence once: attempts=%d interruptions=%d error=%v", attempts, interruptions, err)
	}
	encoded, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil {
		t.Fatal(err)
	}
	writes := fixture.runtime.objects.writes
	reused, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2(t.Context(), 9, 17, 200, fixture.nativeHash, fixture.snapshot, fixture.fresh(t))
	if err != nil {
		t.Fatal(err)
	}
	after, err := readReleaseMeasurementInputV2(path, fixture.options.MaxJournalBytes)
	if err != nil || !bytes.Equal(encoded, after) || !reflect.DeepEqual(committed, reused) || fixture.runtime.objects.writes != writes || fixture.runtime.stats.egressGeneration != fixture.runtime.initialEgressGeneration+1 {
		t.Fatalf("recovered input changed bytes or repeated its publication/rotation: %v", err)
	}
}

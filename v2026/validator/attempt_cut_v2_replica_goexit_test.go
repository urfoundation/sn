//go:build linux || darwin

package validator

// Actual replica workers must report completed storage and public verification,
// not merely terminate. These regressions retain real M8/empty sealing and the
// existing HTTP stores; only the second storage callback exits without return.

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
)

// An accepted result is checked against the genuine replay, signature, complete
// first HTTP store and wholly untouched second store before becoming a witness.
func attemptCutV2ReplicaGoexitPublication(t *testing.T, completed, failed int) (*AttemptCutV2Publication, error, int32) {
	t.Helper()
	fixture := newAttemptCutV2SealTestFixture(t, 8, completed, failed)
	bounds := attemptCutV2ReplicaTestBounds()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	var entered, exited atomic.Int32
	stores[1].beforePut = func(context.Context, string, string, []byte) error {
		entered.Add(1)
		defer exited.Add(1)
		runtime.Goexit()
		return nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := SealReplicatedAttemptCutV2(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, bounds, AttemptCutV2ReplicaOptions{
		ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas,
	})
	// The synchronous production call must join every entered callback on
	// either path. This check does not infer completion from elapsed time.
	if entered.Load() == 0 || entered.Load() != exited.Load() {
		t.Fatalf("Goexit control did not enter and join its real callback: entered=%d exited=%d error=%v", entered.Load(), exited.Load(), err)
	}
	absentObjects, absentWrites, absentReads := stores[1].snapshot()
	if len(absentObjects) != 0 || absentWrites != 0 || absentReads != 0 {
		t.Fatalf("Goexit control unexpectedly stored or fetched second replica: objects=%d writes=%d reads=%d", len(absentObjects), absentWrites, absentReads)
	}
	if result == nil {
		if err == nil {
			t.Fatal("replicated sealer returned no publication and no failure")
		}
		return result, err, exited.Load()
	}
	if err != nil || result.Cut == nil {
		t.Fatalf("replicated sealer exposed an inconsistent partial result: publication=%v error=%v", result, err)
	}
	wantRecords := uint64(completed*8 + failed*2)
	if result.Cut.RecordCount != wantRecords || result.Cut.CompleteCount != uint64(completed) || result.Cut.FailedCount != uint64(failed) ||
		result.Replay.Records.ItemCount != wantRecords || result.Replay.CompleteCount != uint64(completed) || result.Replay.FailedCount != uint64(failed) ||
		result.Origins != [2]string{replicas[0].Origin, replicas[1].Origin} {
		t.Fatalf("accepted Goexit control did not retain the genuine census and origins: %+v", result)
	}
	objects, writes, reads := stores[0].snapshot()
	raw := objects["metadata/"+result.ContentHash]
	cut, decodeErr := DecodeAttemptCutV2(raw, fixture.expected, bounds)
	if decodeErr != nil || !reflect.DeepEqual(cut, result.Cut) || uint64(len(raw)) != result.Size || attemptHex32(sha256.Sum256(raw)) != result.ContentHash {
		t.Fatalf("accepted Goexit control lacks its real signed first-replica header: %v", decodeErr)
	}
	if wantRecords == 0 {
		if len(objects) != 1 || writes != 1 || reads != 1 {
			t.Fatalf("empty Goexit control did not publish and fetch exactly its genuine header: objects=%d writes=%d reads=%d", len(objects), writes, reads)
		}
	} else {
		if writes < 5 || reads <= writes {
			t.Fatalf("M8 Goexit control did not reach complete first-replica staging and replay: writes=%d reads=%d", writes, reads)
		}
		for _, kind := range []string{AttemptStreamV2Records, AttemptStreamV2Proofs} {
			found := false
			for key := range objects {
				if len(key) > len(kind) && key[:len(kind)+1] == kind+"/" {
					found = true
				}
			}
			if !found {
				t.Fatalf("M8 Goexit control omitted the genuine %s stream", kind)
			}
		}
	}
	return result, err, exited.Load()
}

// Genuine M8 completed/failed evidence must not authorize a missing replica,
// even when the other origin supplied every object for full policy replay.
func TestAttemptCutV2ReplicaGoexitRejectsRealM8Publication(t *testing.T) {
	t.Parallel()
	result, err, exited := attemptCutV2ReplicaGoexitPublication(t, 1, 1)
	if result != nil || err == nil {
		t.Fatalf("Goexit publisher accepted real M8 cut without second replica write or public readback: joined_callbacks=%d records=%d complete=%d failed=%d", exited, result.Cut.RecordCount, result.Cut.CompleteCount, result.Cut.FailedCount)
	}
}

// Empty cuts still require two actual signed-header publications; absence of
// record/proof streams is not permission to count an exited header callback.
func TestAttemptCutV2ReplicaGoexitRejectsEmptyPublication(t *testing.T) {
	t.Parallel()
	result, err, exited := attemptCutV2ReplicaGoexitPublication(t, 0, 0)
	if result != nil || err == nil {
		t.Fatalf("Goexit publisher accepted empty cut without second replica header or public readback: joined_callbacks=%d records=%d", exited, result.Cut.RecordCount)
	}
}

// Two real workers meet at an explicit barrier, then both leave through
// Goexit. WaitGroup completion alone must not acknowledge their absent object.
// There is no helper goroutine, polling, sleep or negative timeout witness.
func TestAttemptCutV2ReplicaGoexitJoinedWorkersCannotAcknowledgeObject(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	var entered, exited atomic.Int32
	bothEntered := make(chan struct{})
	write := func(ctx context.Context, _ string, _ []byte) error {
		defer exited.Add(1)
		if entered.Add(1) == 2 {
			close(bothEntered)
		}
		select {
		case <-bothEntered:
			runtime.Goexit()
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for index := range replicas {
		replicas[index].WriteMetadata = write
	}
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	raw := []byte("{}\n")
	err = publisher.writer("metadata")(ctx, attemptHex32(sha256.Sum256(raw)), raw)
	if entered.Load() != 2 || exited.Load() != 2 {
		t.Fatalf("Goexit worker barrier did not join both callbacks: entered=%d exited=%d error=%v", entered.Load(), exited.Load(), err)
	}
	for _, store := range stores {
		objects, writes, reads := store.snapshot()
		if len(objects) != 0 || writes != 0 || reads != 0 {
			t.Fatalf("Goexit barrier control unexpectedly touched an object or HTTP origin: objects=%d writes=%d reads=%d", len(objects), writes, reads)
		}
	}
	if err == nil {
		t.Fatal("joined Goexit workers acknowledged an object with zero writes and zero public readbacks")
	}
}

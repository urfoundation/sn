//go:build linux || darwin

package validator

// Publication controls exercise the complete real sealer and both public HTTP
// stores. Transport-bound checks are separate from full policy authentication;
// neither hash-correct storage nor a partial signed header is acceptance.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// Only the canonical signed-header schema selects a final-publication barrier.
// Stream pages/manifests remain ordinary staged metadata objects.
func attemptCutV2ReplicaTestIsHeader(raw []byte) bool {
	var envelope struct {
		Schema string `json:"schema"`
	}
	return json.Unmarshal(raw, &envelope) == nil && envelope.Schema == AttemptCutV2Schema
}

// Server-key admission is deliberately length-valid. Full fetched-byte replay
// must reject it after both complete typed streams were publicly staged.
func TestAttemptCutV2ReplicaPolicyFailureCannotPublishHeader(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 1)
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	keys := fixture.server.serverPublicKeys()
	keys[fixture.server.serverKeyId] = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{93}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	result, err := SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, attemptCutV2ReplicaTestBounds(), AttemptCutV2ReplicaOptions{
		ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: keys, Replicas: replicas,
	})
	if result != nil || err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("full policy failure escaped: publication=%v error=%v", result, err)
	}
	for index, store := range stores {
		objects, writes, reads := store.snapshot()
		var records, proofs int
		for name, raw := range objects {
			if strings.HasPrefix(name, AttemptStreamV2Records+"/") {
				records++
			}
			if strings.HasPrefix(name, AttemptStreamV2Proofs+"/") {
				proofs++
			}
			if attemptCutV2ReplicaTestIsHeader(raw) {
				t.Fatalf("replica %d received a signed header before full policy success", index)
			}
		}
		if records == 0 || proofs == 0 || writes < 5 || reads < writes || index == 0 && reads == writes {
			t.Fatalf("policy control stopped before genuine public staging/replay at replica %d: records=%d proofs=%d writes=%d reads=%d", index, records, proofs, writes, reads)
		}
	}
}

// Unequal typed bounds and a correct hash for every oversized object ensure
// this control cannot pass merely because the hash admission rejects first.
func TestAttemptCutV2ReplicaRejectsCorrectHashOverTypedBounds(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	bounds := attemptCutV2ReplicaTestBounds()
	bounds.Records.MaxChunkBytes, bounds.Proofs.MaxChunkBytes = 512, 256
	publisher, err := newAttemptCutV2Replicas(bounds, replicas)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		kind  string
		limit uint64
	}{
		{kind: "metadata", limit: publisher.readers[0].metadataBytes},
		{kind: AttemptStreamV2Records, limit: bounds.Records.MaxChunkBytes},
		{kind: AttemptStreamV2Proofs, limit: bounds.Proofs.MaxChunkBytes},
	} {
		raw := bytes.Repeat([]byte{'x'}, int(item.limit)+1)
		if err := publisher.writer(item.kind)(t.Context(), attemptHex32(sha256.Sum256(raw)), raw); err == nil {
			t.Errorf("correct-hash %s object exceeded its own %d-byte bound", item.kind, item.limit)
		}
	}
	for _, store := range stores {
		objects, writes, reads := store.snapshot()
		if len(objects) != 0 || writes != 0 || reads != 0 {
			t.Fatalf("over-limit object reached a publisher or public reader: objects=%d writes=%d reads=%d", len(objects), writes, reads)
		}
	}
}

// Exact object limits are inclusive. This framing-only control uses bytes,
// not a fabricated attempt record or a substitute for policy replay.
func TestAttemptCutV2ReplicaAcceptsExactTypedObjectBounds(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	bounds := attemptCutV2ReplicaTestBounds()
	bounds.Records.MaxChunkBytes, bounds.Proofs.MaxChunkBytes = 512, 256
	publisher, err := newAttemptCutV2Replicas(bounds, replicas)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		kind  string
		limit uint64
	}{
		{kind: "metadata", limit: publisher.readers[0].metadataBytes},
		{kind: AttemptStreamV2Records, limit: bounds.Records.MaxChunkBytes},
		{kind: AttemptStreamV2Proofs, limit: bounds.Proofs.MaxChunkBytes},
	} {
		raw := bytes.Repeat([]byte{'x'}, int(item.limit))
		if err := publisher.writer(item.kind)(t.Context(), attemptHex32(sha256.Sum256(raw)), raw); err != nil {
			t.Fatalf("exact-limit %s object was refused: %v", item.kind, err)
		}
	}
	for _, store := range stores {
		objects, writes, reads := store.snapshot()
		if len(objects) != 3 || writes != 3 || reads != 3 {
			t.Fatalf("exact-limit objects were not separately stored and fetched: objects=%d writes=%d reads=%d", len(objects), writes, reads)
		}
	}
}

// Cancellation is forced inside the second actual header GET, after the first
// header GET and both durable puts. It is not a pre-admission cancellation.
func TestAttemptCutV2ReplicaHeaderReadbackCancellationClearsPublication(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 1)
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	firstHeaderRead := make(chan struct{})
	var headerReads [2]atomic.Int32
	stores[0].readBytes = func(kind string, raw []byte) []byte {
		if kind == "metadata" && attemptCutV2ReplicaTestIsHeader(raw) {
			if headerReads[0].Add(1) == 1 {
				close(firstHeaderRead)
			}
		}
		return raw
	}
	stores[1].readBytes = func(kind string, raw []byte) []byte {
		if kind == "metadata" && attemptCutV2ReplicaTestIsHeader(raw) {
			headerReads[1].Add(1)
			select {
			case <-firstHeaderRead:
				cancel()
			case <-ctx.Done():
			}
		}
		return raw
	}
	result, err := SealReplicatedAttemptCutV2(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, attemptCutV2ReplicaTestBounds(), AttemptCutV2ReplicaOptions{
		ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas,
	})
	if result != nil || !errors.Is(err, context.Canceled) || headerReads[0].Load() != 1 || headerReads[1].Load() != 1 {
		t.Fatalf("final public-read cancellation lost its boundary: publication=%v reads=%d/%d error=%v", result, headerReads[0].Load(), headerReads[1].Load(), err)
	}
	for index, store := range stores {
		objects, _, _ := store.snapshot()
		var headers int
		for _, raw := range objects {
			if attemptCutV2ReplicaTestIsHeader(raw) {
				headers++
			}
		}
		if headers != 1 {
			t.Fatalf("replica %d did not retain the exact pre-cancellation signed header", index)
		}
	}
}

// Replica1's signed header is actually stored before replica2 fails. Retry
// preserves those exact bytes and completes both immutable object sets.
func TestAttemptCutV2ReplicaOrderedPartialHeaderRetainsBytesForRetry(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	firstHeaderStored := make(chan struct{})
	var stored atomic.Bool
	writeFirst, writeSecond := replicas[0].WriteMetadata, replicas[1].WriteMetadata
	replicas[0].WriteMetadata = func(ctx context.Context, hash string, raw []byte) error {
		if err := writeFirst(ctx, hash, raw); err != nil {
			return err
		}
		if attemptCutV2ReplicaTestIsHeader(raw) && stored.CompareAndSwap(false, true) {
			close(firstHeaderStored)
		}
		return nil
	}
	injected := errors.New("ordered second-header publication failure")
	replicas[1].WriteMetadata = func(ctx context.Context, hash string, raw []byte) error {
		if attemptCutV2ReplicaTestIsHeader(raw) {
			select {
			case <-firstHeaderStored:
				return injected
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return writeSecond(ctx, hash, raw)
	}
	options := AttemptCutV2ReplicaOptions{ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "failed"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas}
	result, err := SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, attemptCutV2ReplicaTestBounds(), options)
	if result != nil || !errors.Is(err, injected) || !stored.Load() {
		t.Fatalf("ordered partial header was not reached: publication=%v stored=%t error=%v", result, stored.Load(), err)
	}
	before, _, _ := stores[0].snapshot()
	second, _, _ := stores[1].snapshot()
	var headerHash string
	var headerCount int
	for name, raw := range before {
		if attemptCutV2ReplicaTestIsHeader(raw) {
			headerCount++
			headerHash = strings.TrimPrefix(name, "metadata/")
			if _, err := DecodeAttemptCutV2(raw, fixture.expected, attemptCutV2ReplicaTestBounds()); err != nil || headerHash != attemptHex32(sha256.Sum256(raw)) {
				t.Fatalf("partial staging did not retain a genuine exact signed header: %v", err)
			}
		}
	}
	if headerCount != 1 || headerHash == "" || second["metadata/"+headerHash] != nil {
		t.Fatal("ordered failure did not retain exactly the first signed header")
	}
	options.Replicas[1].WriteMetadata = writeSecond
	options.ScratchDirectory = filepath.Join(t.TempDir(), "retry")
	result, err = SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, attemptCutV2ReplicaTestBounds(), options)
	first, _, _ := stores[0].snapshot()
	second, _, _ = stores[1].snapshot()
	if err != nil || result == nil || result.ContentHash != headerHash || !reflect.DeepEqual(first, second) {
		t.Fatalf("partial-header retry changed identity or omitted a replica: publication=%v error=%v", result, err)
	}
	for name, raw := range before {
		if !bytes.Equal(raw, first[name]) {
			t.Fatalf("retry changed previously staged exact bytes at %s", name)
		}
	}
}

// Real replica callbacks may inspect Head and append genuine later records;
// the original source snapshot still fixes the sealed prefix.
func TestAttemptCutV2ReplicaCallbackHeadAndAppendPreservePrefix(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	ledger, err := NewDiskAttemptLedger(t.Context(), newAttemptLedgerDiskTestStateDir(t), fixture.expected.Identity, attemptLedgerDiskTestCoordinator, fixture.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Errorf("callback ledger close: %v", err)
		}
	})
	for _, record := range fixture.recordTs[:8] {
		if _, err := ledger.AppendContext(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	replicas, _ := newAttemptCutV2ReplicaTestStores(t)
	write := replicas[0].WriteRecords
	var appended atomic.Bool
	replicas[0].WriteRecords = func(ctx context.Context, hash string, raw []byte) error {
		if _, err := ledger.Head(); err != nil {
			return err
		}
		if appended.CompareAndSwap(false, true) {
			for _, record := range fixture.recordTs[8:] {
				if _, err := ledger.AppendContext(ctx, record); err != nil {
					return err
				}
			}
		}
		return write(ctx, hash, raw)
	}
	result, err := SealReplicatedAttemptCutV2(t.Context(), ledger, fixture.expected, fixture.policy, fixture.key, attemptCutV2ReplicaTestBounds(), AttemptCutV2ReplicaOptions{
		ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas,
	})
	if err != nil || result == nil || !appended.Load() || result.Cut.LastSequence != 8 || result.Cut.RecordCount != 8 || result.Replay.CompleteCount != 1 {
		t.Fatalf("replica callback changed fixed-prefix authority: publication=%v appended=%t error=%v", result, appended.Load(), err)
	}
	head, err := ledger.Head()
	if err != nil || head.LastSequence != 16 || head.Root != fixture.recordTs[15].RecordHash {
		t.Fatalf("real later callback append was lost: head=%+v error=%v", head, err)
	}
}

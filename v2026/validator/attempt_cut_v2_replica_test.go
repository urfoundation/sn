//go:build linux || darwin

package validator

// Local HTTP/object transport surrounds the real disk ledger, complete M8
// trail engine, signatures, stream codecs and policy-aware cut sealer.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Each server owns a separate typed namespace. Failure controls are configured
// before a call and changed only after its workers and responses have joined.
type attemptCutV2ReplicaTestStore struct {
	stateLock sync.Mutex
	objects   map[string][]byte
	writes    int
	reads     int
	beforePut func(context.Context, string, string, []byte) error
	readBytes func(string, []byte) []byte
	mutatePut bool
}

// Observations detach all objects while holding the same lock as HTTP/storage.
func (self *attemptCutV2ReplicaTestStore) snapshot() (map[string][]byte, int, int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	objects := make(map[string][]byte, len(self.objects))
	for key, raw := range self.objects {
		objects[key] = bytes.Clone(raw)
	}
	return objects, self.writes, self.reads
}

// Independent HTTP origins cannot read another replica's in-memory objects.
func newAttemptCutV2ReplicaTestStores(t *testing.T) ([2]AttemptCutV2Replica, [2]*attemptCutV2ReplicaTestStore) {
	t.Helper()
	var replicas [2]AttemptCutV2Replica
	var stores [2]*attemptCutV2ReplicaTestStore
	for index := range replicas {
		store := &attemptCutV2ReplicaTestStore{objects: map[string][]byte{}}
		stores[index] = store
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet || request.URL.Path != "/sn/attempt-artifact" || len(request.URL.Query()) != 2 {
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			kind, hash := request.URL.Query().Get("kind"), request.URL.Query().Get("hash")
			raw := func() []byte {
				store.stateLock.Lock()
				defer store.stateLock.Unlock()
				store.reads++
				return bytes.Clone(store.objects[kind+"/"+hash])
			}()
			if raw == nil {
				response.WriteHeader(http.StatusNotFound)
				return
			}
			if store.readBytes != nil {
				raw = store.readBytes(kind, raw)
			}
			contentType := "application/x-ndjson"
			if kind == "metadata" {
				contentType = "application/json"
			}
			response.Header().Set("Content-Type", contentType)
			_, _ = response.Write(raw)
		}))
		t.Cleanup(server.Close)
		writer := func(kind string) AttemptStreamV2ObjectWriter {
			return func(ctx context.Context, hash string, raw []byte) error {
				if store.beforePut != nil {
					if err := store.beforePut(ctx, kind, hash, raw); err != nil {
						return err
					}
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				store.stateLock.Lock()
				defer store.stateLock.Unlock()
				key := kind + "/" + hash
				if prior, exists := store.objects[key]; exists && !bytes.Equal(prior, raw) {
					return errors.New("test store immutable conflict")
				}
				store.objects[key] = bytes.Clone(raw)
				store.writes++
				if store.mutatePut {
					clear(raw)
				}
				return nil
			}
		}
		replicas[index] = AttemptCutV2Replica{Origin: server.URL, WriteRecords: writer(AttemptStreamV2Records), WriteProofs: writer(AttemptStreamV2Proofs), WriteMetadata: writer("metadata")}
	}
	return replicas, stores
}

// The public metadata budget explicitly includes the signed header. No
// production budget, stream count, real trail depth or record cap is changed.
func attemptCutV2ReplicaTestBounds() AttemptCutV2Bounds {
	bounds, _ := attemptReplayV2TestBounds()
	bounds.Records.MaxPageBytes = bounds.MaxHeaderBytes
	return bounds
}

// Uses one genuine completed M8 trail and one genuine transport-failed trail.
func TestAttemptCutV2ReplicaSealsRealM8AndFailedTerminal(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 1)
	bounds := attemptCutV2ReplicaTestBounds()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	result, err := SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, bounds, AttemptCutV2ReplicaOptions{
		ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas,
	})
	if err != nil || result == nil {
		t.Fatalf("real replicated cut: %v", err)
	}
	if result.Cut.RecordCount != 10 || result.Cut.CompleteCount != 1 || result.Cut.FailedCount != 1 || result.Size == 0 || result.Origins != [2]string{replicas[0].Origin, replicas[1].Origin} {
		t.Fatalf("real cut census or origins changed: %+v", result)
	}
	for index, store := range stores {
		objects, writes, reads := store.snapshot()
		raw := objects["metadata/"+result.ContentHash]
		cut, err := DecodeAttemptCutV2(raw, fixture.expected, bounds)
		if err != nil || !reflect.DeepEqual(cut, result.Cut) || uint64(len(raw)) != result.Size || attemptHex32(sha256.Sum256(raw)) != result.ContentHash {
			t.Fatalf("replica %d signed header differs: %v", index, err)
		}
		// The first origin also serves the complete replay. Both must serve
		// at least one public read for every staged object.
		if writes < 5 || reads < writes || index == 0 && reads == writes {
			t.Fatalf("replica %d did not serve every staged object: writes=%d reads=%d", index, writes, reads)
		}
	}
	first, _, _ := stores[0].snapshot()
	second, _, _ := stores[1].snapshot()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("replicas retained different complete typed object sets")
	}
}

// A no-trail window still publishes its genuine signed empty cut, not fake data.
func TestAttemptCutV2ReplicaPublishesEmptySignedHeader(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 0)
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	result, err := SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, attemptCutV2ReplicaTestBounds(), AttemptCutV2ReplicaOptions{
		ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "empty"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas,
	})
	if err != nil || result == nil || result.Cut.RecordCount != 0 {
		t.Fatalf("empty replicated cut: %+v %v", result, err)
	}
	for _, store := range stores {
		objects, writes, reads := store.snapshot()
		if len(objects) != 1 || writes != 1 || reads != 1 {
			t.Fatalf("empty cut manufactured data or skipped readback: objects=%d writes=%d reads=%d", len(objects), writes, reads)
		}
	}
}

// A failed object never becomes an accepted cut. Retry uses fresh private
// scratch and the same immutable object stores, without overwriting history.
func TestAttemptCutV2ReplicaPartialWriteFailsAndRetryIsImmutable(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	injected := errors.New("injected second replica storage failure")
	stores[1].beforePut = func(context.Context, string, string, []byte) error { return injected }
	options := AttemptCutV2ReplicaOptions{ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "failed"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas}
	result, err := SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, attemptCutV2ReplicaTestBounds(), options)
	if result != nil || !errors.Is(err, injected) {
		t.Fatalf("partial publication escaped: %+v %v", result, err)
	}
	stores[1].beforePut = nil
	options.ScratchDirectory = filepath.Join(t.TempDir(), "retry")
	result, err = SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, attemptCutV2ReplicaTestBounds(), options)
	first, _, _ := stores[0].snapshot()
	second, _, _ := stores[1].snapshot()
	if err != nil || result == nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("retry did not complete identical replicas: %v", err)
	}
}

// Publishing all chunks is insufficient when the final signed header fails.
func TestAttemptCutV2ReplicaRejectsFailedHeaderPublication(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	injected := errors.New("injected signed header write failure")
	var reached atomic.Bool
	stores[1].beforePut = func(_ context.Context, kind, _ string, raw []byte) error {
		if kind == "metadata" && bytes.Contains(raw, []byte(AttemptCutV2Schema)) {
			reached.Store(true)
			return injected
		}
		return nil
	}
	result, err := SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, attemptCutV2ReplicaTestBounds(), AttemptCutV2ReplicaOptions{
		ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas,
	})
	if !reached.Load() || result != nil || !errors.Is(err, injected) {
		t.Fatalf("header failure lost after complete replay: reached=%t result=%v error=%v", reached.Load(), result, err)
	}
}

// The private write result cannot stand in for the independently fetched bytes.
func TestAttemptCutV2ReplicaRejectsCorruptPublicCopy(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	stores[1].readBytes = func(_ string, raw []byte) []byte { raw[0] ^= 1; return raw }
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("{\"record\":true}\n")
	if err := publisher.writer(AttemptStreamV2Records)(t.Context(), attemptHex32(sha256.Sum256(raw)), raw); err == nil {
		t.Fatal("corrupt second public copy was accepted")
	}
	_, _, reads := stores[1].snapshot()
	if reads != 1 {
		t.Fatal("corrupt replica was not fetched through actual HTTP")
	}
}

// Canonicalized scheme/host/port aliases cannot masquerade as two replicas.
func TestAttemptCutV2ReplicaRejectsOriginAliasesAndMissingWriters(t *testing.T) {
	t.Parallel()
	write := func(context.Context, string, []byte) error { t.Error("admission invoked writer"); return nil }
	for _, origins := range [][2]string{
		{"https://EXAMPLE.test", "https://example.test:443/"},
		{"http://127.0.0.1", "http://127.0.0.1:80"},
		{"http://[::1]:80", "http://[0:0:0:0:0:0:0:1]"},
		{"http://127.0.0.1", "http://[::ffff:127.0.0.1]"},
	} {
		var replicas [2]AttemptCutV2Replica
		for index := range replicas {
			replicas[index] = AttemptCutV2Replica{Origin: origins[index], WriteRecords: write, WriteProofs: write, WriteMetadata: write}
		}
		if publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas); err == nil || publisher != nil {
			t.Errorf("origin aliases admitted: %v", origins)
		}
	}
	replicas := [2]AttemptCutV2Replica{
		{Origin: "https://first.test", WriteRecords: write, WriteProofs: write, WriteMetadata: write},
		{Origin: "https://second.test", WriteRecords: write, WriteProofs: write},
	}
	if publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas); err == nil || publisher != nil {
		t.Fatal("incomplete second publisher was admitted")
	}
}

// Kind, length and canonical content hash are checked before either callback.
func TestAttemptCutV2ReplicaRejectsObjectsBeforeStorage(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	bounds := attemptCutV2ReplicaTestBounds()
	publisher, err := newAttemptCutV2Replicas(bounds, replicas)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("{}\n")
	for _, object := range []struct {
		kind, hash string
		raw        []byte
	}{
		{kind: "other", hash: attemptHex32(sha256.Sum256(raw)), raw: raw},
		{kind: "metadata", hash: zeroAttemptHash(), raw: raw},
		{kind: "metadata", hash: attemptHex32(sha256.Sum256([]byte("other"))), raw: raw},
		{kind: "metadata", hash: attemptHex32(sha256.Sum256(raw))},
		{kind: "metadata", hash: attemptHex32(sha256.Sum256(raw)), raw: bytes.Repeat([]byte{1}, int(bounds.MaxHeaderBytes)+1)},
	} {
		if err := publisher.writer(object.kind)(t.Context(), object.hash, object.raw); err == nil {
			t.Errorf("invalid %s object admitted", object.kind)
		}
	}
	for _, store := range stores {
		_, writes, reads := store.snapshot()
		if writes != 0 || reads != 0 {
			t.Fatal("refused object reached storage or HTTP")
		}
	}
}

// Each callback may alter its owned copy without corrupting its sibling or caller.
func TestAttemptCutV2ReplicaWritersOwnIndependentBytes(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	for _, store := range stores {
		store.mutatePut = true
	}
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("{\"independent\":true}\n")
	want := bytes.Clone(raw)
	hash := attemptHex32(sha256.Sum256(raw))
	for _, kind := range []string{"metadata", AttemptStreamV2Records, AttemptStreamV2Proofs} {
		if err := publisher.writer(kind)(t.Context(), hash, raw); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, want) {
			t.Fatal("callback mutated caller bytes")
		}
		for _, store := range stores {
			objects, _, _ := store.snapshot()
			if !bytes.Equal(objects[kind+"/"+hash], want) {
				t.Fatal("sibling bytes were corrupted")
			}
		}
	}
}

// Both writers reach an explicit barrier before either is permitted to finish.
func TestAttemptCutV2ReplicaWritesRunConcurrently(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	for _, store := range stores {
		store.beforePut = func(ctx context.Context, _, _ string, _ []byte) error {
			entered <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("{}\n")
	result := make(chan error, 1)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		result <- publisher.writer("metadata")(ctx, attemptHex32(sha256.Sum256(raw)), raw)
	}()
	defer joinAttemptCutV2ReplicaTestWorker(cancel, joined)
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

// Failure cancels a blocked sibling and returns only after that callback exits.
func TestAttemptCutV2ReplicaFailureCancelsAndJoinsSibling(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	entered := make(chan struct{})
	injected := errors.New("replica write failure after both entered")
	var joined atomic.Bool
	stores[0].beforePut = func(ctx context.Context, _, _ string, _ []byte) error {
		select {
		case <-entered:
			return injected
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	stores[1].beforePut = func(ctx context.Context, _, _ string, _ []byte) error {
		close(entered)
		<-ctx.Done()
		joined.Store(true)
		return ctx.Err()
	}
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("{}\n")
	err = publisher.writer("metadata")(t.Context(), attemptHex32(sha256.Sum256(raw)), raw)
	if !errors.Is(err, injected) || !joined.Load() {
		t.Fatalf("sibling was lost: joined=%t error=%v", joined.Load(), err)
	}
}

// A body close error remains a failure after all bytes and their hash match.
func TestAttemptCutV2ReplicaPreservesLatePublicCloseFailure(t *testing.T) {
	t.Parallel()
	replicas, _ := newAttemptCutV2ReplicaTestStores(t)
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("late public close failure")
	raw := []byte("{}\n")
	var closes atomic.Int32
	publisher.readers[1].client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		reader := bytes.NewReader(raw)
		body := &attemptStreamV2HTTPTestBody{read: reader.Read, close: func() error { closes.Add(1); return injected }}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-ndjson"}}, ContentLength: int64(len(raw)), Body: body}, nil
	})
	err = publisher.writer(AttemptStreamV2Proofs)(t.Context(), attemptHex32(sha256.Sum256(raw)), raw)
	if !errors.Is(err, injected) || closes.Load() != 1 {
		t.Fatalf("late close failure lost: closes=%d error=%v", closes.Load(), err)
	}
}

// Canceled or absent ownership cannot invoke a publisher or create scratch.
func TestAttemptCutV2ReplicaCanceledAdmissionHasNoEffects(t *testing.T) {
	t.Parallel()
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), replicas)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	raw := []byte("{}\n")
	if err := publisher.writer("metadata")(ctx, attemptHex32(sha256.Sum256(raw)), raw); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := publisher.writer("metadata")(nil, attemptHex32(sha256.Sum256(raw)), raw); err == nil {
		t.Fatal("nil context accepted")
	}
	result, err := SealReplicatedAttemptCutV2(ctx, nil, AttemptCutV2Context{}, exactPolicy(t), nil, attemptCutV2ReplicaTestBounds(), AttemptCutV2ReplicaOptions{Replicas: replicas})
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled seal escaped: %v %v", result, err)
	}
	for _, store := range stores {
		_, writes, reads := store.snapshot()
		if writes != 0 || reads != 0 {
			t.Fatal("canceled admission caused external effects")
		}
	}
}

// The wrapper preserves the real sealer's key and ledger admission before IO.
func TestAttemptCutV2ReplicaWrongKeyCannotPublish(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	key := bytes.Clone(fixture.key)
	key[0] ^= 1
	result, err := SealReplicatedAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, key, attemptCutV2ReplicaTestBounds(), AttemptCutV2ReplicaOptions{
		ReplayBounds: fixture.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: fixture.server.serverPublicKeys(), Replicas: replicas,
	})
	if result != nil || err == nil || !strings.Contains(err.Error(), "private key") {
		t.Fatalf("wrong key admitted: %v %v", result, err)
	}
	for _, store := range stores {
		_, writes, reads := store.snapshot()
		if writes != 0 || reads != 0 {
			t.Fatal("wrong key reached object transport")
		}
	}
}

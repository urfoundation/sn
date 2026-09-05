//go:build linux || darwin

package validator

// The private store is a new feature, not an activated replacement for the
// v1 ledger. These tests use real signed M8 records and explicit I/O barriers;
// injected failures exercise persistence uncertainty without timing guesses.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/urnetwork/connect"
)

// Fixture records retain the production engine's signatures and checkpoints.
type attemptRecordStoreTestFixture struct {
	recordTs     []AttemptRecord
	validatorKey ed25519.PrivateKey
	identity     AttemptLedgerIdentity
}

// Explicit small test limits do not supply or activate production defaults.
func attemptRecordStoreTestBounds() attemptRecordStoreBounds {
	return attemptRecordStoreBounds{
		MaxRecordBytes: 64 * 1024, MaxRecordCount: 128, MaxTrailCount: 16,
		MaxRawRecordBytes: 8 * 1024 * 1024, MaxStorageBytes: 64 * 1024 * 1024,
		MaxStorageFiles: 256,
	}
}

// Produce the requested complete trails through the existing durable engine.
func newAttemptRecordStoreTestFixture(t *testing.T, trails int) attemptRecordStoreTestFixture {
	t.Helper()
	server, validatorKey, clientID := newMockVerifyServer(t, 16)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 8, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, t.TempDir(), &generation)
	for trail := 0; trail < trails; trail++ {
		if _, err := engine.RunTrail(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	recordTs, err := ledger.RecordsAfter(0)
	if err != nil || len(recordTs) != trails*8 {
		t.Fatalf("signed M8 records = %d, error %v", len(recordTs), err)
	}
	for index := range recordTs {
		if err := verifyAttemptRecord(&recordTs[index], ledger.identity, validatorKey.Public().(ed25519.PublicKey), server.serverPublicKeys(), true); err != nil {
			t.Fatalf("fixture record %d: %v", index, err)
		}
	}
	return attemptRecordStoreTestFixture{recordTs: recordTs, validatorKey: validatorKey, identity: ledger.identity}
}

// Open helpers arrange cleanup but individual tests still assert Close errors.
func openAttemptRecordStoreTest(t *testing.T, path string, fixture attemptRecordStoreTestFixture, bounds attemptRecordStoreBounds, hooks attemptRecordStoreHooks) *attemptRecordStore {
	t.Helper()
	store, err := openAttemptRecordStoreWithHooks(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), bounds, hooks)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// Re-sign only test-controlled record fields without changing production code.
func resignAttemptRecordStoreTest(t *testing.T, record AttemptRecord, validatorKey ed25519.PrivateKey) AttemptRecord {
	t.Helper()
	digest, err := attemptRecordHash(&record)
	if err != nil {
		t.Fatal(err)
	}
	record.RecordHash = attemptHex32(digest)
	record.Signature = ed25519.Sign(validatorKey, attemptRecordSignatureMessage(digest))
	return record
}

// Each append must acknowledge the complete expected sequence and root.
func appendAttemptRecordStoreTest(t *testing.T, store *attemptRecordStore, recordTs []AttemptRecord) {
	t.Helper()
	for _, record := range recordTs {
		if err := store.Append(context.Background(), record); err != nil {
			t.Fatalf("append sequence %d: %v", record.Sequence, err)
		}
		head, err := store.Head()
		if err != nil || head.LastSequence != record.Sequence || head.Root != record.RecordHash {
			t.Fatalf("acknowledged head = %+v, error %v", head, err)
		}
	}
}

// Preserve the exact existing JSON, chain hash, signatures and proof contents.
func TestAttemptRecordStorePreservesCanonicalV1Bytes(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	path := filepath.Join(t.TempDir(), "store")
	store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs)
	var expectedBytes uint64
	for _, expected := range fixture.recordTs {
		raw, err := json.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		expectedBytes += uint64(len(raw))
		stored, err := store.db.Get(attemptStoreRecordKey(expected.Sequence), nil)
		if err != nil || !bytes.Equal(stored, raw) {
			t.Fatalf("canonical bytes changed at %d: %v", expected.Sequence, err)
		}
		actual, err := store.Read(context.Background(), expected.Sequence)
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("record changed at %d: %v", expected.Sequence, err)
		}
	}
	head, err := store.Head()
	if err != nil || head.RecordBytes != expectedBytes || head.TrailCount != 2 {
		t.Fatalf("exact counters = %+v, error %v", head, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	visited := 0
	if err := store.Walk(context.Background(), 1, 16, func(record AttemptRecord) error {
		if !reflect.DeepEqual(record, fixture.recordTs[visited]) {
			return fmt.Errorf("reopened record %d differs", visited+1)
		}
		visited++
		return nil
	}); err != nil || visited != 16 {
		t.Fatalf("reopen walk count = %d, error %v", visited, err)
	}
}

// Terminal identity is lifetime-scoped, including after reopen and epoch change.
func TestAttemptRecordStoreTerminalTrailIDsSurviveReopenAndEpochs(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	path := filepath.Join(t.TempDir(), "store")
	store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:8])
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	reused := fixture.recordTs[0]
	reused.Sequence, reused.PreviousHash = 9, fixture.recordTs[7].RecordHash
	reused.Boundary.SettlementEpoch++
	reused = resignAttemptRecordStoreTest(t, reused, fixture.validatorKey)
	if err := store.Append(context.Background(), reused); err == nil {
		t.Fatal("reopened store accepted a terminal trail id in a later epoch")
	}
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[8:])
	pending := 0
	if err := store.Pending(context.Background(), func(AttemptRecord) error { pending++; return nil }); err != nil || pending != 0 {
		t.Fatalf("terminal census retained pending trails = %d, error %v", pending, err)
	}
}

// Interleaved lifecycle replay needs only the last checkpoint of one indexed id.
func TestAttemptRecordStoreReplaysInterleavedPendingTrails(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	recordTs := make([]AttemptRecord, 0, 16)
	root := zeroAttemptHash()
	for checkpoint := 0; checkpoint < 8; checkpoint++ {
		for trail := 0; trail < 2; trail++ {
			record := fixture.recordTs[trail*8+checkpoint]
			record.Sequence, record.PreviousHash = uint64(len(recordTs)+1), root
			record = resignAttemptRecordStoreTest(t, record, fixture.validatorKey)
			recordTs = append(recordTs, record)
			root = record.RecordHash
		}
	}
	path := filepath.Join(t.TempDir(), "store")
	store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, recordTs[:6])
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	pendingKVs := map[connect.Id]AttemptRecord{}
	if err := store.Pending(context.Background(), func(record AttemptRecord) error {
		pendingKVs[record.TrailID] = record
		return nil
	}); err != nil || len(pendingKVs) != 2 {
		t.Fatalf("pending census = %d, error %v", len(pendingKVs), err)
	}
	for _, expected := range recordTs[4:6] {
		if !reflect.DeepEqual(pendingKVs[expected.TrailID], expected) {
			t.Fatalf("pending checkpoint differs for %s", expected.TrailID)
		}
	}
	appendAttemptRecordStoreTest(t, store, recordTs[6:])
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	_ = openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
}

// Caller assertions cannot bypass signed identity, shape, sequence or hash checks.
func TestAttemptRecordStoreRejectsInvalidSignedRecords(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	changes := []struct {
		name   string
		change func(*AttemptRecord)
	}{
		{name: "signature", change: func(record *AttemptRecord) { record.Signature[0] ^= 1 }},
		{name: "hash", change: func(record *AttemptRecord) { record.RecordHash = zeroAttemptHash() }},
		{name: "identity", change: func(record *AttemptRecord) { record.Identity.NoID++ }},
		{name: "vpk", change: func(record *AttemptRecord) { record.VPK[0] ^= 1 }},
		{name: "sequence", change: func(record *AttemptRecord) { record.Sequence++ }},
		{name: "previous", change: func(record *AttemptRecord) { record.PreviousHash = attemptHex32([32]byte{1}) }},
		{name: "assign bytes", change: func(record *AttemptRecord) { record.Assignments[0].AssignMessage[0] ^= 1 }},
		{name: "oversized shape", change: func(record *AttemptRecord) { record.Assignments[0].Trail = make([]connect.Id, 1000) }},
	}
	for _, change := range changes {
		record, err := cloneAttemptRecord(fixture.recordTs[0])
		if err != nil {
			t.Fatal(err)
		}
		change.change(&record)
		if change.name != "signature" && change.name != "hash" {
			record = resignAttemptRecordStoreTest(t, record, fixture.validatorKey)
		}
		if err := store.Append(context.Background(), record); err == nil {
			t.Fatalf("accepted invalid %s", change.name)
		}
		head, err := store.Head()
		if err != nil || head.LastSequence != 0 {
			t.Fatalf("invalid %s mutated or faulted the store: %+v, %v", change.name, head, err)
		}
	}
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:1])
}

// Valid validator signatures cannot move a checkpoint onto another boundary.
func TestAttemptRecordStoreRejectsChangedPendingBoundaryAndPrefix(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:1])
	changed := fixture.recordTs[1]
	changed.Boundary.EVMBlock++
	changed = resignAttemptRecordStoreTest(t, changed, fixture.validatorKey)
	if err := store.Append(context.Background(), changed); err == nil {
		t.Fatal("accepted a re-signed checkpoint on a changed boundary")
	}
	skipped := fixture.recordTs[2]
	skipped.Sequence, skipped.PreviousHash = 2, fixture.recordTs[0].RecordHash
	skipped = resignAttemptRecordStoreTest(t, skipped, fixture.validatorKey)
	if err := store.Append(context.Background(), skipped); err == nil {
		t.Fatal("accepted a missing intermediate checkpoint")
	}
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[1:])
}

// Atomicity checks include same-count swaps, not merely a missing-key census.
func TestAttemptRecordStoreRejectsRecordAndIndexCorruption(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	changes := []struct {
		name   string
		change func(*leveldb.Batch)
	}{
		{name: "missing record", change: func(batch *leveldb.Batch) { batch.Delete(attemptStoreRecordKey(2)) }},
		{name: "torn record", change: func(batch *leveldb.Batch) { batch.Put(attemptStoreRecordKey(2), []byte("{")) }},
		{name: "missing marker", change: func(batch *leveldb.Batch) { batch.Delete(attemptStoreTrailRecordKey(fixture.recordTs[0].TrailID, 1)) }},
		{name: "marker hash", change: func(batch *leveldb.Batch) {
			batch.Put(attemptStoreTrailRecordKey(fixture.recordTs[0].TrailID, 1), []byte(zeroAttemptHash()))
		}},
		{name: "marker id", change: func(batch *leveldb.Batch) {
			batch.Delete(attemptStoreTrailRecordKey(fixture.recordTs[0].TrailID, 1))
			batch.Put(attemptStoreTrailRecordKey(connect.Id{1}, 1), []byte(fixture.recordTs[0].RecordHash))
		}},
		{name: "marker sequence swap", change: func(batch *leveldb.Batch) {
			batch.Put(attemptStoreTrailRecordKey(fixture.recordTs[0].TrailID, 1), []byte(fixture.recordTs[1].RecordHash))
			batch.Put(attemptStoreTrailRecordKey(fixture.recordTs[0].TrailID, 2), []byte(fixture.recordTs[0].RecordHash))
		}},
		{name: "missing final state", change: func(batch *leveldb.Batch) { batch.Delete(attemptStoreTrailKey(fixture.recordTs[0].TrailID)) }},
		{name: "extra final state", change: func(batch *leveldb.Batch) { batch.Put(attemptStoreTrailKey(connect.Id{1}), []byte("{}")) }},
		{name: "wrong final state", change: func(batch *leveldb.Batch) {
			raw, _ := json.Marshal(attemptRecordStoreTrail{LastSequence: 7, RecordHash: fixture.recordTs[6].RecordHash, Terminal: false})
			batch.Put(attemptStoreTrailKey(fixture.recordTs[0].TrailID), raw)
		}},
		{name: "unknown key", change: func(batch *leveldb.Batch) { batch.Put([]byte("record/1"), []byte("{}")) }},
		{name: "missing head", change: func(batch *leveldb.Batch) { batch.Delete([]byte("head")) }},
		{name: "missing identity", change: func(batch *leveldb.Batch) { batch.Delete([]byte("identity")) }},
	}
	for _, change := range changes {
		path := filepath.Join(t.TempDir(), "store")
		store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
		appendAttemptRecordStoreTest(t, store, fixture.recordTs)
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		disk, err := openAttemptRecordStoreStorage(path, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{}, func(error) {})
		if err != nil {
			t.Fatal(err)
		}
		db, err := leveldb.Open(disk, &opt.Options{Strict: opt.StrictAll})
		if err != nil {
			t.Fatal(err)
		}
		batch := new(leveldb.Batch)
		change.change(batch)
		if err := db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := disk.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := openAttemptRecordStore(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds())
		if err == nil {
			_ = reopened.Close()
			t.Fatalf("accepted corrupt %s", change.name)
		}
	}
}

// Each limit is caller-supplied and rejection before a batch leaves head intact.
func TestAttemptRecordStoreEnforcesExplicitLogicalLimits(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	changes := []struct {
		name   string
		bounds attemptRecordStoreBounds
		before int
	}{
		{name: "one record bytes", bounds: attemptRecordStoreTestBounds(), before: 0},
		{name: "record count", bounds: attemptRecordStoreTestBounds(), before: 1},
		{name: "aggregate bytes", bounds: attemptRecordStoreTestBounds(), before: 1},
		{name: "trail count", bounds: attemptRecordStoreTestBounds(), before: 8},
	}
	firstBytes, _ := json.Marshal(fixture.recordTs[0])
	secondBytes, _ := json.Marshal(fixture.recordTs[1])
	for _, change := range changes {
		switch change.name {
		case "one record bytes":
			change.bounds.MaxRecordBytes = uint64(len(firstBytes) - 1)
		case "record count":
			change.bounds.MaxRecordCount, change.bounds.MaxTrailCount = 1, 1
		case "aggregate bytes":
			change.bounds.MaxRecordBytes = uint64(len(secondBytes))
			change.bounds.MaxRawRecordBytes = uint64(len(firstBytes) + len(secondBytes) - 1)
		case "trail count":
			change.bounds.MaxTrailCount = 1
		}
		store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, change.bounds, attemptRecordStoreHooks{})
		appendAttemptRecordStoreTest(t, store, fixture.recordTs[:change.before])
		before, _ := store.Head()
		if err := store.Append(context.Background(), fixture.recordTs[change.before]); !errors.Is(err, errAttemptRecordStoreLimit) {
			t.Fatalf("%s error = %v", change.name, err)
		}
		after, err := store.Head()
		if err != nil || after != before {
			t.Fatalf("%s changed/faulted head: before %+v, after %+v, error %v", change.name, before, after, err)
		}
	}
}

// Input, Read results and iterator values never share mutable record buffers.
func TestAttemptRecordStoreOwnsInputOutputAndVisitBuffers(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	input, err := cloneAttemptRecord(fixture.recordTs[0])
	if err != nil {
		t.Fatal(err)
	}
	appendAttemptRecordStoreTest(t, store, []AttemptRecord{input})
	input.ServerNonce[0] ^= 1
	input.Assignments[0].AssignMessage[0] ^= 1
	input.Signature[0] ^= 1
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[1:])
	for read := 0; read < 2; read++ {
		record, err := store.Read(context.Background(), 1)
		if err != nil || !reflect.DeepEqual(record, fixture.recordTs[0]) {
			t.Fatalf("read ownership changed stored record: %v", err)
		}
		record.ServerNonce[0] ^= 1
		record.Assignments[0].AssignMessage[0] ^= 1
	}
	var retained []AttemptRecord
	if err := store.Walk(context.Background(), 1, 8, func(record AttemptRecord) error {
		retained = append(retained, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for index, record := range retained {
		if !reflect.DeepEqual(record, fixture.recordTs[index]) {
			t.Fatalf("iterator reuse changed retained record %d", index+1)
		}
	}
	retained[7].Proof.FinalSig[0] ^= 1
	actual, err := store.Read(context.Background(), 8)
	if err != nil || !reflect.DeepEqual(actual, fixture.recordTs[7]) {
		t.Fatalf("proof output aliases storage: %v", err)
	}
}

// Reentrant Read/Head from a visitor proves no store state lock is retained.
func TestAttemptRecordStoreCallbacksDoNotHoldStateLock(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:3])
	visitor := func(record AttemptRecord) error {
		head, err := store.Head()
		if err != nil || head.LastSequence != 3 {
			return fmt.Errorf("visitor head: %+v, %v", head, err)
		}
		read, err := store.Read(context.Background(), record.Sequence)
		if err != nil || !reflect.DeepEqual(record, read) {
			return fmt.Errorf("visitor owned read: %v", err)
		}
		return nil
	}
	if err := store.Walk(context.Background(), 1, 3, visitor); err != nil {
		t.Fatal(err)
	}
	if err := store.Pending(context.Background(), visitor); err != nil {
		t.Fatal(err)
	}
}

// A blocked visitor is joined, while cancellation releases queued iterators.
func TestAttemptRecordStoreCloseCancelsAndJoinsWalk(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:2])
	entered, release := make(chan struct{}), make(chan struct{})
	walkDone, closeDone := make(chan error, 1), make(chan error, 1)
	go func() {
		walkDone <- store.Walk(context.Background(), 1, 2, func(AttemptRecord) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	go func() { closeDone <- store.Close() }()
	<-store.ctx.Done()
	select {
	case err := <-closeDone:
		close(release)
		t.Fatalf("Close returned before joining visitor: %v", err)
	default:
	}
	if err := store.Pending(context.Background(), func(AttemptRecord) error { return nil }); !errors.Is(err, errAttemptRecordStoreClosed) {
		t.Fatalf("new iterator after Close = %v", err)
	}
	close(release)
	if err := <-walkDone; !errors.Is(err, errAttemptRecordStoreClosed) {
		t.Fatalf("joined walk = %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

// Callback errors and caller cancellation release the iterator without faulting.
func TestAttemptRecordStoreCancellationAndVisitorFailureLeaveStoreUsable(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:3])
	ctx, cancel := context.WithCancel(context.Background())
	visited := 0
	err := store.Walk(ctx, 1, 3, func(AttemptRecord) error { visited++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || visited != 1 {
		t.Fatalf("cancelled walk count = %d, error %v", visited, err)
	}
	expected := errors.New("visitor refused record")
	if err := store.Pending(context.Background(), func(AttemptRecord) error { return expected }); !errors.Is(err, expected) {
		t.Fatalf("visitor error = %v", err)
	}
	if err := store.Append(ctx, fixture.recordTs[3]); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled append = %v", err)
	}
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[3:])
}

// After durable commit but before acknowledgment, reopen resolves the exact head.
func TestAttemptRecordStoreFaultsAfterAmbiguousCommittedBatch(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	path := filepath.Join(t.TempDir(), "store")
	expected := errors.New("after batch acknowledgement boundary")
	var armed atomic.Bool
	store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{Step: func(operation, _ string) error {
		if armed.Load() && operation == "after-batch" {
			return expected
		}
		return nil
	}})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:1])
	armed.Store(true)
	if err := store.Append(context.Background(), fixture.recordTs[1]); !errors.Is(err, expected) {
		t.Fatalf("ambiguous append = %v", err)
	}
	if _, err := store.Head(); !errors.Is(err, errAttemptRecordStoreFaulted) {
		t.Fatalf("faulted head = %v", err)
	}
	if err := store.Append(context.Background(), fixture.recordTs[1]); !errors.Is(err, errAttemptRecordStoreFaulted) {
		t.Fatalf("uncertain sequence reused before reopen: %v", err)
	}
	armed.Store(false)
	if err := store.Close(); !errors.Is(err, expected) {
		t.Fatalf("faulted close = %v", err)
	}
	store = openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	head, err := store.Head()
	if err != nil || head.LastSequence != 2 || head.Root != fixture.recordTs[1].RecordHash {
		t.Fatalf("persisted batch was rolled back: %+v, %v", head, err)
	}
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[2:])
}

// Every WAL write/sync uncertainty faults the owner and preserves prior acks.
func TestAttemptRecordStoreWALFailuresRequireReopen(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, operation := range []string{"before-write", "after-write", "before-file-sync", "after-file-sync", "before-directory-sync", "after-directory-sync"} {
		path := filepath.Join(t.TempDir(), "store")
		expected := errors.New("injected " + operation)
		var armed, injected atomic.Bool
		store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{Step: func(actual, name string) error {
			if armed.Load() && actual == operation && strings.HasSuffix(name, ".log") && injected.CompareAndSwap(false, true) {
				return expected
			}
			return nil
		}})
		appendAttemptRecordStoreTest(t, store, fixture.recordTs[:1])
		armed.Store(true)
		if err := store.Append(context.Background(), fixture.recordTs[1]); !errors.Is(err, expected) || !injected.Load() {
			t.Fatalf("%s did not inject WAL failure: %v", operation, err)
		}
		if err := store.Append(context.Background(), fixture.recordTs[1]); !errors.Is(err, errAttemptRecordStoreFaulted) {
			t.Fatalf("%s did not latch fault: %v", operation, err)
		}
		armed.Store(false)
		if err := store.Close(); !errors.Is(err, expected) {
			t.Fatalf("%s close = %v", operation, err)
		}
		store = openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
		head, err := store.Head()
		if err != nil || head.LastSequence < 1 || head.LastSequence > 2 || head.Root != fixture.recordTs[head.LastSequence-1].RecordHash {
			t.Fatalf("%s lost acknowledged prefix or recovered partial batch: %+v, %v", operation, head, err)
		}
		appendAttemptRecordStoreTest(t, store, fixture.recordTs[head.LastSequence:])
	}
}

// Reopening with either side of an atomic CURRENT switch preserves all acks.
func TestAttemptRecordStoreCurrentOldAndNewCrashStatesPreserveAcknowledgedPrefix(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, operation := range []string{"before-set-meta", "after-set-meta"} {
		path := filepath.Join(t.TempDir(), "store")
		store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
		appendAttemptRecordStoreTest(t, store, fixture.recordTs[:3])
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		expected := errors.New("injected " + operation)
		var injected atomic.Bool
		opened, err := openAttemptRecordStoreWithHooks(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds(), attemptRecordStoreHooks{Step: func(actual, _ string) error {
			if actual == operation && injected.CompareAndSwap(false, true) {
				return expected
			}
			return nil
		}})
		if err == nil {
			_ = opened.Close()
			t.Fatalf("%s did not reject uncertain manifest selection", operation)
		}
		if !errors.Is(err, expected) || !injected.Load() {
			t.Fatalf("%s failure = %v", operation, err)
		}
		store = openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
		head, err := store.Head()
		if err != nil || head.LastSequence != 3 || head.Root != fixture.recordTs[2].RecordHash {
			t.Fatalf("%s rolled back acknowledged records: %+v, %v", operation, head, err)
		}
		appendAttemptRecordStoreTest(t, store, fixture.recordTs[3:])
	}
}

// Fingerprints exclude only backend lock/log files, never a data/metadata file.
func attemptRecordStoreTestFiles(t *testing.T, path string) map[string][32]byte {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][32]byte{}
	for _, entry := range entries {
		if entry.Name() == "LOCK" || entry.Name() == "LOG" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(path, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = sha256.Sum256(raw)
	}
	return files
}

// Missing/corrupt CURRENT must not bless its backup or orphaned WAL/manifest.
func TestAttemptRecordStoreRejectsMissingOrCorruptCurrentWithoutFallback(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, corrupt := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "store")
		store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
		appendAttemptRecordStoreTest(t, store, fixture.recordTs[:3])
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		currentPath := filepath.Join(path, "CURRENT")
		raw, err := os.ReadFile(currentPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(currentPath+".bak", raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if corrupt {
			err = os.WriteFile(currentPath, []byte("MANIFEST-000000\ntrailing"), 0o600)
		} else {
			err = os.Remove(currentPath)
		}
		if err != nil {
			t.Fatal(err)
		}
		before := attemptRecordStoreTestFiles(t, path)
		opened, err := openAttemptRecordStore(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds())
		if err == nil {
			_ = opened.Close()
			t.Fatalf("accepted missing/corrupt CURRENT, corrupt=%v", corrupt)
		}
		if after := attemptRecordStoreTestFiles(t, path); !reflect.DeepEqual(before, after) {
			t.Fatalf("rejected CURRENT state was modified, corrupt=%v", corrupt)
		}
	}
}

// A leftover file cannot be reinterpreted as a brand-new empty ledger.
func TestAttemptRecordStoreRejectsOrphanedDatabaseFiles(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, name := range []string{"000001.log", "000002.ldb", "MANIFEST-000003", "CURRENT.4", "CURRENT.bak"} {
		path := filepath.Join(t.TempDir(), "store")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, name), []byte("preserve rejected orphan"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := attemptRecordStoreTestFiles(t, path)
		opened, err := openAttemptRecordStore(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds())
		if err == nil {
			_ = opened.Close()
			t.Fatalf("initialized empty ledger over %s", name)
		}
		if after := attemptRecordStoreTestFiles(t, path); !reflect.DeepEqual(before, after) {
			t.Fatalf("orphan %s was changed", name)
		}
	}
}

// A second owner and a changed coordinator may not reuse the private namespace.
func TestAttemptRecordStoreRejectsSecondOwnerAndChangedNamespace(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	path := filepath.Join(t.TempDir(), "store")
	store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:1])
	opened, err := openAttemptRecordStore(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds())
	if err == nil {
		_ = opened.Close()
		t.Fatal("opened a second owner")
	}
	lockPath, retainedLock := filepath.Join(path, "LOCK"), filepath.Join(filepath.Dir(path), "retained-lock")
	if err := os.Rename(lockPath, retainedLock); err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []bool{false, true} {
		if replacement {
			if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		opened, err := openAttemptRecordStore(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds())
		if err == nil {
			_ = opened.Close()
			t.Fatalf("a missing/replaced LOCK granted a second owner, replacement=%v", replacement)
		}
		if !replacement {
			if _, err := os.Lstat(lockPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("second owner mutated LOCK before acquiring the directory: %v", err)
			}
		}
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retainedLock, lockPath); err != nil {
		t.Fatal(err)
	}
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[1:2])
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, coordinator := range []string{"0x2222222222222222222222222222222222222222", "0x0000000000000000000000000000000000000000", "1111111111111111111111111111111111111111"} {
		opened, err := openAttemptRecordStore(context.Background(), path, fixture.identity, coordinator, fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds())
		if err == nil {
			_ = opened.Close()
			t.Fatalf("accepted changed/invalid coordinator %s", coordinator)
		}
	}
	_ = openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
}

// Final-component symlinks, nonprivate modes and replaced anchors fail closed.
func TestAttemptRecordStoreRejectsNonprivateOrChangedDirectory(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	parent := t.TempDir()
	publicPath := filepath.Join(parent, "public")
	if err := os.Mkdir(publicPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(publicPath, 0o755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(parent, "link")
	if err := os.Symlink(publicPath, linkPath); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{publicPath, linkPath} {
		opened, err := openAttemptRecordStore(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds())
		if err == nil {
			_ = opened.Close()
			t.Fatalf("accepted nonprivate or symlink directory %s", path)
		}
	}
	path := filepath.Join(parent, "store")
	store := openAttemptRecordStoreTest(t, path, fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:1])
	if err := os.Rename(path, path+"-preserved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(context.Background(), fixture.recordTs[1]); err == nil {
		t.Fatal("append acknowledged into a replaced directory")
	}
	if _, err := store.Head(); !errors.Is(err, errAttemptRecordStoreFaulted) {
		t.Fatalf("anchor replacement did not latch fault: %v", err)
	}
	_ = store.Close()
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("replacement directory was mutated: %d, %v", len(entries), err)
	}
}

// Every create/file-sync/parent-sync stage can refuse before a new store opens.
func TestAttemptRecordStoreNewDatabaseDurabilityFailuresRemainVisible(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, operation := range []string{"before-create", "after-create", "before-file-sync", "after-file-sync", "before-directory-sync", "after-directory-sync", "before-set-meta", "after-set-meta"} {
		path := filepath.Join(t.TempDir(), "store")
		expected := errors.New("injected initial " + operation)
		var injected atomic.Bool
		opened, err := openAttemptRecordStoreWithHooks(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds(), attemptRecordStoreHooks{Step: func(actual, _ string) error {
			if actual == operation && injected.CompareAndSwap(false, true) {
				return expected
			}
			return nil
		}})
		if err == nil {
			_ = opened.Close()
			t.Fatalf("initial %s unexpectedly succeeded", operation)
		}
		if !errors.Is(err, expected) || !injected.Load() {
			t.Fatalf("initial %s = %v", operation, err)
		}
	}
}

// Storage byte/count reservations are finite, including compaction-created files.
func TestAttemptRecordStoreStorageLimitsAndSameFileRename(t *testing.T) {
	bounds := attemptRecordStoreTestBounds()
	bounds.MaxStorageBytes = attemptStoreMetadataReserve + 4
	bounds.MaxStorageFiles = 8
	var faultLock sync.Mutex
	var fault error
	disk, err := openAttemptRecordStoreStorage(filepath.Join(t.TempDir(), "store"), bounds, attemptRecordStoreHooks{}, func(err error) {
		faultLock.Lock()
		defer faultLock.Unlock()
		if fault == nil {
			fault = err
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	fd := storage.FileDesc{Type: storage.TypeJournal, Num: 1}
	writer, err := disk.Create(fd)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := writer.Write([]byte("four")); err != nil || n != 4 {
		t.Fatalf("bounded write = %d, %v", n, err)
	}
	if err := disk.Rename(fd, fd); err != nil {
		t.Fatal(err)
	}
	if n, err := writer.Write([]byte("!")); n != 0 || !errors.Is(err, errAttemptRecordStoreLimit) {
		t.Fatalf("storage overflow = %d, %v", n, err)
	}
	if err := writer.Close(); !errors.Is(err, errAttemptRecordStoreLimit) {
		t.Fatalf("faulted writer close = %v", err)
	}
	disk.stateLock.Lock()
	used, reserved := disk.used, disk.sizes[fd.String()]
	disk.stateLock.Unlock()
	if used != attemptStoreMetadataReserve+4 || reserved != 4 {
		t.Fatalf("same-file rename corrupted reservations = %d/%d", used, reserved)
	}
	faultLock.Lock()
	latched := fault
	faultLock.Unlock()
	if !errors.Is(latched, errAttemptRecordStoreLimit) {
		t.Fatalf("storage exhaustion did not fault owner: %v", latched)
	}
	countBounds := attemptRecordStoreTestBounds()
	countBounds.MaxStorageFiles = 8
	countDisk, err := openAttemptRecordStoreStorage(filepath.Join(t.TempDir(), "store"), countBounds, attemptRecordStoreHooks{}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer countDisk.Close()
	for number := int64(1); number <= 7; number++ {
		writer, err := countDisk.Create(storage.FileDesc{Type: storage.TypeJournal, Num: number})
		if err != nil {
			t.Fatalf("file %d below limit: %v", number, err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if writer, err := countDisk.Create(storage.FileDesc{Type: storage.TypeJournal, Num: 8}); !errors.Is(err, errAttemptRecordStoreLimit) {
		if writer != nil {
			_ = writer.Close()
		}
		t.Fatalf("eighth data file plus owner file exceeded count limit: %v", err)
	}
}

// The after-batch barrier pins the exact database/head publication ordering.
func TestAttemptRecordStorePendingUsesPublishedHeadSnapshot(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	batchEntered, releaseBatch := make(chan struct{}), make(chan struct{})
	pendingCtx, cancelPending := context.WithCancel(context.Background())
	defer cancelPending()
	var armed, contended atomic.Bool
	store := openAttemptRecordStoreTest(t, filepath.Join(t.TempDir(), "store"), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{Step: func(operation, _ string) error {
		if armed.Load() && operation == "after-batch" {
			close(batchEntered)
			<-releaseBatch
		}
		if armed.Load() && operation == "snapshot-contended" {
			contended.Store(true)
			cancelPending()
		}
		return nil
	}})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:1])
	armed.Store(true)
	appendDone, pendingDone := make(chan error, 1), make(chan error, 1)
	go func() { appendDone <- store.Append(context.Background(), fixture.recordTs[1]) }()
	<-batchEntered
	go func() {
		pendingDone <- store.Pending(pendingCtx, func(record AttemptRecord) error {
			head, err := store.Head()
			return fmt.Errorf("visited unpublished pending record/head = %d/%d, %v", record.Sequence, head.LastSequence, err)
		})
	}()
	// Complete the cancelled wait before releasing the writer. Without the gate,
	// Pending instead visits the unpublished record and returns the error above.
	pendingErr := <-pendingDone
	head, err := store.Head()
	close(releaseBatch)
	if err := <-appendDone; err != nil {
		t.Fatal(err)
	}
	if err != nil || head.LastSequence != 1 || !contended.Load() || !errors.Is(pendingErr, context.Canceled) {
		t.Fatalf("unpublished head/actual contention/cancellation = %+v/%v/%v, head error %v", head, contended.Load(), pendingErr, err)
	}
	armed.Store(false)
	visited := 0
	if err := store.Pending(context.Background(), func(record AttemptRecord) error {
		head, err := store.Head()
		if err != nil || head.LastSequence != 2 || record.Sequence != 2 {
			return fmt.Errorf("published record/head = %d/%d, %v", record.Sequence, head.LastSequence, err)
		}
		visited++
		return nil
	}); err != nil || visited != 1 {
		t.Fatalf("published pending census = %d, error %v", visited, err)
	}
}

// A same-directory file replacement between check and open is rejected by inode
// identity/no-follow, and neither an external target nor its bytes are modified.
func TestAttemptRecordStoreRejectsFileSwapBetweenCheckAndOpen(t *testing.T) {
	for _, hardlink := range []bool{false, true} {
		parent := t.TempDir()
		target := filepath.Join(parent, "outside")
		original := []byte("outside bytes must remain unchanged")
		if err := os.WriteFile(target, original, 0o600); err != nil {
			t.Fatal(err)
		}
		var armed, swapped atomic.Bool
		path := filepath.Join(parent, "store")
		disk, err := openAttemptRecordStoreStorage(path, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{Step: func(operation, name string) error {
			if armed.Load() && operation == "after-file-check" && name == "000001.log" && swapped.CompareAndSwap(false, true) {
				filePath := filepath.Join(path, name)
				if err := os.Rename(filePath, filePath+".preserved"); err != nil {
					return err
				}
				if hardlink {
					return os.Link(target, filePath)
				}
				return os.Symlink(target, filePath)
			}
			return nil
		}}, func(error) {})
		if err != nil {
			t.Fatal(err)
		}
		fd := storage.FileDesc{Type: storage.TypeJournal, Num: 1}
		writer, err := disk.Create(fd)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte("owned database bytes")); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		armed.Store(true)
		reader, err := disk.Open(fd)
		if err == nil || !swapped.Load() {
			if reader != nil {
				_ = reader.Close()
			}
			t.Fatalf("file swap accepted, hardlink=%v, error=%v", hardlink, err)
		}
		if err := disk.Close(); err != nil {
			t.Fatal(err)
		}
		actual, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(actual, original) {
			t.Fatalf("outside file changed, hardlink=%v: %v", hardlink, err)
		}
	}
}

// A root-directory swap after validation cannot redirect the descriptor I/O.
func TestAttemptRecordStoreDirectorySwapCannotRedirectCreate(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "store")
	var armed, swapped atomic.Bool
	disk, err := openAttemptRecordStoreStorage(path, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{Step: func(operation, name string) error {
		if armed.Load() && operation == "after-file-check" && name == "000001.log" && swapped.CompareAndSwap(false, true) {
			if err := os.Rename(path, path+"-preserved"); err != nil {
				return err
			}
			return os.Mkdir(path, 0o700)
		}
		return nil
	}}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	armed.Store(true)
	writer, err := disk.Create(storage.FileDesc{Type: storage.TypeJournal, Num: 1})
	if err == nil || !swapped.Load() {
		if writer != nil {
			_ = writer.Close()
		}
		t.Fatalf("directory swap create = %v", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("replacement directory received files: %d, %v", len(entries), err)
	}
}

// Preexisting aliases, including the owner lock, are rejected before any write.
func TestAttemptRecordStoreRejectsHardlinkedBackendFiles(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, name := range []string{"LOCK", "LOG", "CURRENT", "000001.log"} {
		parent := t.TempDir()
		path, target := filepath.Join(parent, "store"), filepath.Join(parent, "outside")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		original := []byte("external alias must remain intact")
		if err := os.WriteFile(target, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(target, filepath.Join(path, name)); err != nil {
			t.Fatal(err)
		}
		opened, err := openAttemptRecordStore(context.Background(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), attemptRecordStoreTestBounds())
		if err == nil {
			_ = opened.Close()
			t.Fatalf("accepted linked backend %s", name)
		}
		actual, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(actual, original) {
			t.Fatalf("linked %s was modified: %v", name, err)
		}
	}
}

// Exclusive creates preserve both an existing descriptor and its reservation.
func TestAttemptRecordStoreCreateCollisionNeverTruncates(t *testing.T) {
	disk, err := openAttemptRecordStoreStorage(filepath.Join(t.TempDir(), "store"), attemptRecordStoreTestBounds(), attemptRecordStoreHooks{}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	fd := storage.FileDesc{Type: storage.TypeJournal, Num: 1}
	writer, err := disk.Create(fd)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("immutable conflicting descriptor")
	if _, err := writer.Write(original); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	disk.stateLock.Lock()
	before := disk.used
	disk.stateLock.Unlock()
	writer, err = disk.Create(fd)
	if err == nil {
		_ = writer.Close()
		t.Fatal("existing descriptor was silently truncated")
	}
	actual, err := disk.root.ReadFile(fd.String())
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("collision changed bytes: %v", err)
	}
	disk.stateLock.Lock()
	after, reserved := disk.used, disk.sizes[fd.String()]
	disk.stateLock.Unlock()
	if after != before || reserved != uint64(len(original)) {
		t.Fatalf("collision changed reservations: %d/%d/%d", before, after, reserved)
	}
}

// Reopen performs two exact one-record passes; Walk decodes each selected
// record once, independent of how much prior history the store contains.
func TestAttemptRecordStoreBoundsStreamingDecodeWork(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	path := filepath.Join(t.TempDir(), "store")
	bounds := attemptRecordStoreTestBounds()
	store := openAttemptRecordStoreTest(t, path, fixture, bounds, attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var decodes, largest atomic.Uint64
	store = openAttemptRecordStoreTest(t, path, fixture, bounds, attemptRecordStoreHooks{Step: func(operation, name string) error {
		if operation == "decode-record" {
			length, err := strconv.ParseUint(name, 10, 64)
			if err != nil {
				return err
			}
			decodes.Add(1)
			for previous := largest.Load(); previous < length; previous = largest.Load() {
				if largest.CompareAndSwap(previous, length) {
					break
				}
			}
		}
		return nil
	}})
	if decodes.Load() != 32 || largest.Load() == 0 || largest.Load() > bounds.MaxRecordBytes {
		t.Fatalf("reopen work/maximum record bytes = %d/%d", decodes.Load(), largest.Load())
	}
	decodes.Store(0)
	visited := 0
	if err := store.Walk(context.Background(), 9, 16, func(AttemptRecord) error { visited++; return nil }); err != nil || visited != 8 || decodes.Load() != 8 {
		t.Fatalf("selected walk work/visits = %d/%d, error %v", decodes.Load(), visited, err)
	}
}

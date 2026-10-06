//go:build linux || darwin

package validator

// Real signed records and deterministic verifier barriers distinguish avoided
// replay from warm filesystem reads, including failed and concurrent owners.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/syndtr/goleveldb/leveldb/opt"
)

// Keep a real second trail available for append invalidation without creating
// another identity or allowing a new lifetime allowance.
func newStoppedAttemptCapacityCacheTest(t *testing.T, records int) (attemptRecordStoreTestFixture, string, AttemptLedgerDiskLimits, AttemptLedgerHead) {
	t.Helper()
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	stateDir := t.TempDir()
	bounds := attemptRecordStoreTestBounds()
	store := openAttemptRecordStoreTest(t, filepath.Join(stateDir, attemptLedgerStoreName), fixture, bounds, attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[:records])
	head, err := store.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	limits := AttemptLedgerDiskLimits{MaxRecordBytes: bounds.MaxRecordBytes, MaxRecordCount: bounds.MaxRecordCount, MaxTrailCount: bounds.MaxTrailCount, MaxRawRecordBytes: bounds.MaxRawRecordBytes, MaxStorageBytes: bounds.MaxStorageBytes, MaxStorageFiles: bounds.MaxStorageFiles}
	return fixture, stateDir, limits, head
}

// Custom storage deliberately has a no-op logger. The two byte-mutation
// controls explicitly own their diagnostic input before the first fingerprint.
func writeStoppedAttemptCapacityDiagnosticTest(t *testing.T, stateDir string) string {
	t.Helper()
	path := filepath.Join(stateDir, attemptLedgerStoreName, "LOG")
	if err := os.WriteFile(path, []byte("retained diagnostic fixture bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Nine startup boundaries used to perform the complete signed replay each time.
// A fresh owner reproduces that cost; one invocation must perform it only once.
func TestStoppedAttemptLedgerCapacityCacheReusesOneSignedReplay(t *testing.T) {
	fixture, stateDir, limits, prefix := newStoppedAttemptCapacityCacheTest(t, 16)
	var decodes int
	hooks := attemptRecordStoreHooks{Step: func(operation, name string) error {
		if operation == "decode-record" {
			decodes++
		}
		return nil
	}}
	read := func(cache *StoppedAttemptLedgerCapacityCache) StoppedAttemptLedgerCapacity {
		value, err := cache.Read(t.Context(), stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits, prefix)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	var expected StoppedAttemptLedgerCapacity
	for i := 0; i < 9; i++ {
		expected = read(&StoppedAttemptLedgerCapacityCache{hooks: hooks})
	}
	perReplay := int(prefix.LastSequence) + 1
	if decodes != 9*perReplay {
		t.Fatalf("uncached signed replay decodes = %d, want %d", decodes, 9*perReplay)
	}
	decodes = 0
	cache := &StoppedAttemptLedgerCapacityCache{hooks: hooks}
	for i := 0; i < 9; i++ {
		if value := read(cache); value != expected || decodes != perReplay {
			t.Fatalf("boundary %d repeated signed replay or changed capacity: decodes=%d value=%+v", i, decodes, value)
		}
	}
	read(&StoppedAttemptLedgerCapacityCache{hooks: hooks})
	if decodes != 2*perReplay {
		t.Fatal("another startup invocation inherited authenticated capacity")
	}
}

// Actual bytes, including metadata-file bytes, must invalidate reuse even if
// an in-place edit restores its original length, mode, inode and timestamp.
func TestStoppedAttemptLedgerCapacityCacheHashesRestoredMetadataBytes(t *testing.T) {
	fixture, stateDir, limits, prefix := newStoppedAttemptCapacityCacheTest(t, 8)
	logPath := writeStoppedAttemptCapacityDiagnosticTest(t, stateDir)
	var decodes int
	cache := &StoppedAttemptLedgerCapacityCache{hooks: attemptRecordStoreHooks{Step: func(operation, name string) error {
		if operation == "decode-record" {
			decodes++
		}
		return nil
	}}}
	read := func() {
		if _, err := cache.Read(t.Context(), stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits, prefix); err != nil {
			t.Fatal(err)
		}
	}
	read()
	initial := decodes
	root, err := os.OpenRoot(filepath.Join(stateDir, attemptLedgerStoreName))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, _, err := stoppedAttemptFiles(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(logPath)
	if err != nil || info.Size() == 0 {
		t.Fatal("fixture lacks nonempty diagnostic log", err)
	}
	file, err := os.OpenFile(logPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := file.ReadAt(b[:], 0); err != nil {
		t.Fatal(err)
	}
	b[0] ^= 1
	if _, err := file.WriteAt(b[:], 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(logPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, _, err := stoppedAttemptFiles(root, limits)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("test did not preserve the metadata-only cache key", err)
	}
	read()
	if decodes != 2*initial {
		t.Fatal("changed actual bytes reused a metadata-only capacity proof")
	}
	read()
	if decodes != 2*initial {
		t.Fatal("unchanged replacement bytes did not acquire their own successful proof")
	}
}

// A writer can reopen between calls because no cache entry owns a database
// lock. Appending genuine signed records replays and retains the old prefix.
func TestStoppedAttemptLedgerCapacityCacheRechecksAppendAndReleasesLock(t *testing.T) {
	fixture, stateDir, limits, prefix := newStoppedAttemptCapacityCacheTest(t, 8)
	cache := &StoppedAttemptLedgerCapacityCache{}
	read := func() StoppedAttemptLedgerCapacity {
		value, err := cache.Read(t.Context(), stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits, prefix)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if read().Head != prefix {
		t.Fatal("initial prefix differs")
	}
	store := openAttemptRecordStoreTest(t, filepath.Join(stateDir, attemptLedgerStoreName), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs[8:])
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	value := read()
	if value.Head.LastSequence != 16 || value.Head.TrailCount != 2 || value.Head == prefix || read() != value {
		t.Fatal("real append acquired stale capacity or lost its original lifetime")
	}
}

// A prior successful proof must not hide a later forged signed record, even
// when its namespace, caller authority and lifetime head remain unchanged.
func TestStoppedAttemptLedgerCapacityCacheRejectsChangedSignedRecord(t *testing.T) {
	fixture, stateDir, limits, prefix := newStoppedAttemptCapacityCacheTest(t, 8)
	cache := &StoppedAttemptLedgerCapacityCache{}
	read := func() error {
		_, err := cache.Read(t.Context(), stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits, prefix)
		return err
	}
	if err := read(); err != nil {
		t.Fatal(err)
	}
	store := openAttemptRecordStoreTest(t, filepath.Join(stateDir, attemptLedgerStoreName), fixture, attemptRecordStoreTestBounds(), attemptRecordStoreHooks{})
	record := fixture.recordTs[0]
	record.Signature = append([]byte(nil), record.Signature...)
	record.Signature[0] ^= 1
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Put(attemptStoreRecordKey(1), raw, &opt.WriteOptions{Sync: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := read(); err == nil || !strings.Contains(err.Error(), "signed lifetime prefix") {
			t.Fatal("changed signed record acquired cached capacity", err)
		}
	}
}

// A file change forced inside the signed replay cannot publish a proof for
// either byte generation, even when ordinary metadata checks would pass.
func TestStoppedAttemptLedgerCapacityCacheRejectsChangeDuringReplay(t *testing.T) {
	fixture, stateDir, limits, prefix := newStoppedAttemptCapacityCacheTest(t, 8)
	path := writeStoppedAttemptCapacityDiagnosticTest(t, stateDir)
	changed := false
	cache := &StoppedAttemptLedgerCapacityCache{hooks: attemptRecordStoreHooks{Step: func(operation, name string) error {
		if operation != "decode-record" || changed {
			return nil
		}
		changed = true
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) == 0 {
			return errors.Join(errors.New("fixture log is unavailable"), err)
		}
		raw[0] ^= 1
		if err := os.WriteFile(path, raw, info.Mode().Perm()); err != nil {
			return err
		}
		return os.Chtimes(path, info.ModTime(), info.ModTime())
	}}}
	read := func() error {
		_, err := cache.Read(t.Context(), stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits, prefix)
		return err
	}
	if err := read(); err == nil || !strings.Contains(err.Error(), "bytes changed during signed replay") || len(cache.sourceKVs) != 0 {
		t.Fatal("unstable signed replay published cached authority", err)
	}
	if err := read(); err != nil {
		t.Fatal("stable replacement bytes could not be authenticated", err)
	}
}

// Every caller operand is part of the successful proof; a looser valid bound
// also requires a fresh replay instead of silently sharing another authority.
func TestStoppedAttemptLedgerCapacityCacheBindsEveryReadOperand(t *testing.T) {
	fixture, stateDir, limits, prefix := newStoppedAttemptCapacityCacheTest(t, 8)
	var decodes int
	cache := &StoppedAttemptLedgerCapacityCache{hooks: attemptRecordStoreHooks{Step: func(operation, name string) error {
		if operation == "decode-record" {
			decodes++
		}
		return nil
	}}}
	vpk := fixture.validatorKey.Public().(ed25519.PublicKey)
	coordinator := "0x1111111111111111111111111111111111111111"
	if _, err := cache.Read(t.Context(), stateDir, fixture.identity, coordinator, vpk, limits, prefix); err != nil {
		t.Fatal(err)
	}
	identity := fixture.identity
	identity.NoID++
	badPrefix := prefix
	badPrefix.Root = "0x" + strings.Repeat("a", 64)
	tight := limits
	tight.MaxRecordCount = prefix.LastSequence - 1
	tight.MaxTrailCount = prefix.TrailCount
	for _, args := range []struct {
		identity    AttemptLedgerIdentity
		coordinator string
		vpk         ed25519.PublicKey
		limits      AttemptLedgerDiskLimits
		prefix      AttemptLedgerHead
	}{
		{identity: identity, coordinator: coordinator, vpk: vpk, limits: limits, prefix: prefix},
		{identity: fixture.identity, coordinator: "0x2222222222222222222222222222222222222222", vpk: vpk, limits: limits, prefix: prefix},
		{identity: fixture.identity, coordinator: coordinator, vpk: make(ed25519.PublicKey, ed25519.PublicKeySize), limits: limits, prefix: prefix},
		{identity: fixture.identity, coordinator: coordinator, vpk: vpk, limits: tight, prefix: prefix},
		{identity: fixture.identity, coordinator: coordinator, vpk: vpk, limits: limits, prefix: badPrefix},
	} {
		if _, err := cache.Read(t.Context(), stateDir, args.identity, args.coordinator, args.vpk, args.limits, args.prefix); err == nil {
			t.Fatalf("changed authority reused capacity: %+v", args)
		}
	}
	priorDecodes := decodes
	looser := limits
	looser.MaxStorageBytes += 1024
	if _, err := cache.Read(t.Context(), stateDir, fixture.identity, coordinator, vpk, looser, prefix); err != nil || decodes <= priorDecodes {
		t.Fatal("changed valid bounds did not replay", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, ctx} {
		if _, err := cache.Read(ctx, stateDir, fixture.identity, coordinator, vpk, looser, prefix); err == nil {
			t.Fatal("invalid context reused successful capacity")
		}
	}
}

// A deterministic verifier failure leaves no reusable success. The repaired
// read must finish a complete signed replay before later calls can reuse it.
func TestStoppedAttemptLedgerCapacityCacheNeverPublishesFailedReplay(t *testing.T) {
	fixture, stateDir, limits, prefix := newStoppedAttemptCapacityCacheTest(t, 8)
	fail := true
	var decodes int
	cache := &StoppedAttemptLedgerCapacityCache{hooks: attemptRecordStoreHooks{Step: func(operation, name string) error {
		if operation == "decode-record" {
			decodes++
			if fail {
				return errors.New("synthetic signed replay interruption")
			}
		}
		return nil
	}}}
	read := func() error {
		_, err := cache.Read(t.Context(), stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits, prefix)
		return err
	}
	if err := read(); err == nil || len(cache.sourceKVs) != 0 || decodes != 1 {
		t.Fatal("failed signed replay published capacity", err)
	}
	fail = false
	if err := read(); err != nil || decodes != int(prefix.LastSequence)+2 {
		t.Fatal("retry skipped the failed signed replay", err)
	}
	priorDecodes := decodes
	if err := read(); err != nil || decodes != priorDecodes {
		t.Fatal("successful retry did not become reusable", err)
	}
}

// Overlapping cold readers may both verify, but one cancellation must neither
// publish its partial work nor revoke the other reader's completed authority.
func TestStoppedAttemptLedgerCapacityCacheConcurrentCancellation(t *testing.T) {
	fixture, stateDir, limits, prefix := newStoppedAttemptCapacityCacheTest(t, 8)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var decodes atomic.Int64
	cache := &StoppedAttemptLedgerCapacityCache{hooks: attemptRecordStoreHooks{Step: func(operation, name string) error {
		if operation == "decode-record" && decodes.Add(1) <= 2 {
			arrived <- struct{}{}
			<-release
		}
		return nil
	}}}
	read := func(ctx context.Context) error {
		_, err := cache.Read(ctx, stateDir, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), limits, prefix)
		return err
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	canceled := make(chan error, 1)
	success := make(chan error, 1)
	go func() { canceled <- read(ctx) }()
	go func() { success <- read(t.Context()) }()
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case err := <-canceled:
			t.Fatal("canceled owner failed before its verifier barrier", err)
		case err := <-success:
			t.Fatal("successful owner failed before its verifier barrier", err)
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
	cancel()
	releaseOnce.Do(func() { close(release) })
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatal("canceled reader acquired success", err)
	}
	if err := <-success; err != nil {
		t.Fatal(err)
	}
	priorDecodes := decodes.Load()
	if err := read(t.Context()); err != nil || decodes.Load() != priorDecodes || len(cache.sourceKVs) != 1 {
		t.Fatal("concurrent successful owner lost its reusable proof", err)
	}
}

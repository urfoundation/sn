//go:build linux || darwin

package validator

// Real native tables and journals supply compatibility controls. Test-only
// manifest edits are explicit corrupt/unsupported inputs, never new authority.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb/journal"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// Explicit compaction uses the actual unchanged database/options. Close joins
// native workers while its retained snapshot still protects same-key history.
func compactStatsAggregateAdmissionTestFixture(t *testing.T, fixture *statsAggregateTestFixture, copies int) {
	t.Helper()
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	snapshot, err := store.db.GetSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Release()
	raw, err := snapshot.Get([]byte{'h'}, &opt.ReadOptions{Strict: opt.StrictAll})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < copies; index++ {
		// Sync is not the compatibility oracle here; each native single-entry
		// encoding is genuine, and CompactRange/Close complete before reads.
		if err := store.db.Put([]byte{'h'}, raw, &opt.WriteOptions{NoWriteMerge: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.db.CompactRange(util.Range{}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

// Reads only real table footers/blocks to assert the counterexample exists;
// no test assumes that a target table size is a hard native index size cap.
func inspectStatsAggregateAdmissionTestTables(t *testing.T, fixture *statsAggregateTestFixture) (uint64, uint64, uint64) {
	t.Helper()
	disk, err := openAttemptRecordStoreInspection(context.Background(), fixture.path, attemptRecordStoreBounds{MaxStorageBytes: fixture.bounds.MaxStorageBytes, MaxStorageFiles: fixture.bounds.MaxStorageFiles}, attemptRecordStoreHooks{}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := disk.Close(); err != nil {
			t.Errorf("native table inspection close: %v", err)
		}
	}()
	reader := &attemptStoreAdmissionReader{ctx: context.Background(), disk: disk}
	manifest, err := reader.manifest(81)
	if err != nil || manifest == nil || len(manifest.tables) == 0 {
		t.Fatalf("actual compaction produced no admitted tables: %v", err)
	}
	var maximumTable, maximumIndex, rows uint64
	for _, table := range manifest.orderedTables() {
		if table.level > attemptAdmissionHighestLevel {
			t.Fatal("native producer emitted an unsupported level")
		}
		if table.size > maximumTable {
			maximumTable = table.size
		}
		file, err := disk.inspectFile((storage.FileDesc{Type: storage.TypeTable, Num: table.number}).String())
		if err != nil {
			t.Fatal(err)
		}
		var footer [48]byte
		_, err = file.ReadAt(footer[:], file.info.Size()-48)
		_, n, handleErr := attemptAdmissionHandle(footer[:40])
		index, _, indexErr := attemptAdmissionHandle(footer[n:40])
		if err != nil || handleErr != nil || indexErr != nil {
			_ = file.Close()
			t.Fatalf("real native footer: %v", errors.Join(err, handleErr, indexErr))
		}
		block, err := reader.block(file, index)
		if err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if block.length > maximumIndex {
			maximumIndex = block.length
		}
		_, readErr := io.Copy(io.Discard, block.output)
		block.release()
		if err := errors.Join(readErr, file.Close()); err != nil {
			t.Fatal(err)
		}
		if err := reader.table(table, 81, fixture.bounds.MaxHeaderBytes, func(key []byte, _ *io.LimitedReader) error {
			if bytes.Equal(key[:len(key)-8], []byte{'h'}) {
				rows++
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if reader.activeBlocks != 0 || reader.maximumBlocks != 4 {
		t.Fatalf("actual nested index/data ownership differs: active=%d max=%d", reader.activeBlocks, reader.maximumBlocks)
	}
	return maximumTable, maximumIndex, rows
}

// Normal writable recovery agrees with the independently inspected exact head
// after actual SST compaction, preserving complete M8 provider/claim parity.
func TestStatsAggregateAdmissionNativeTableAuthorityMatchesRecovery(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	compactStatsAggregateAdmissionTestFixture(t, fixture, 0)
	inspectStatsAggregateAdmissionTestTables(t, fixture)
	var report statsAggregateAdmissionReport
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{AdmissionChecked: func(value statsAggregateAdmissionReport) error { report = value; return nil }})
	got, err := store.Head(context.Background())
	if err != nil || got != head || report.MaximumActiveBlocks != 4 || report.WindowBytes != 4*attemptAdmissionWindow || report.EncodedBufferBytes != 4*attemptAdmissionBuffer || report.DecodedBufferBytes != 4*attemptAdmissionBuffer || report.ScratchBufferBytes != attemptAdmissionBuffer {
		t.Fatalf("native recovery or fixed allocation report differs: report=%+v err=%v", report, err)
	}
	assertStatsAggregateTestParity(t, fixture, got)
}

// A real retained-snapshot same-key history exceeds the native table target
// and a single 64 KiB index window. Its complete logical authority still opens.
func TestStatsAggregateAdmissionNativeSameKeyHistoryHasBoundedBuffers(t *testing.T) {
	fixture, head := newStatsAggregateOpenTestFixture(t)
	const copies = 24000
	compactStatsAggregateAdmissionTestFixture(t, fixture, copies)
	tableBytes, indexBytes, rows := inspectStatsAggregateAdmissionTestTables(t, fixture)
	if tableBytes <= 2*1024*1024 || indexBytes <= attemptAdmissionWindow || rows < copies {
		t.Fatalf("actual long-history counterexample was not established: table=%d index=%d h_versions=%d", tableBytes, indexBytes, rows)
	}
	var report statsAggregateAdmissionReport
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{AdmissionChecked: func(value statsAggregateAdmissionReport) error { report = value; return nil }})
	got, err := store.Head(context.Background())
	if err != nil || got != head || report.MaximumActiveBlocks != 4 || report.WindowBytes != 4*attemptAdmissionWindow || report.EncodedBufferBytes != 4*attemptAdmissionBuffer || report.DecodedBufferBytes != 4*attemptAdmissionBuffer || report.ScratchBufferBytes != attemptAdmissionBuffer || report.DecodedBytes <= indexBytes {
		t.Fatalf("long-history authority/buffers differ: report=%+v err=%v", report, err)
	}
	assertStatsAggregateTestParity(t, fixture, got)
}

// Namespace refusal from a real SST is as side-effect-free as WAL refusal.
func TestStatsAggregateAdmissionNativeTableWrongNamespaceDoesNotRecover(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	compactStatsAggregateAdmissionTestFixture(t, fixture, 0)
	inspectStatsAggregateAdmissionTestTables(t, fixture)
	expected := fixture.source.expected
	expected.Identity.NoID++
	if observeStatsAggregateOpenTestRefusal(t, fixture, "native-table", expected, fixture.config) {
		t.Fatal("SST authority refusal ran writable recovery")
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

// A later fully replayed generation is persisted after the old SST. The WAL
// wins over that table and promotion must compare the full effective head.
func TestStatsAggregateAdmissionNewestWalOverridesNativeTable(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	compactStatsAggregateAdmissionTestFixture(t, fixture, 0)
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	next, err := store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil || next.Generation != head.Generation+1 {
		t.Fatalf("real later replay did not publish its generation: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	got, err := store.Head(context.Background())
	if err != nil || got != next {
		t.Fatalf("WAL effective header was not preserved exactly: %v", err)
	}
	assertStatsAggregateTestParity(t, fixture, got)
}

// Re-encodes each unchanged native manifest record through its actual pinned
// journal framing and appends only the explicit negative-control record.
func appendStatsAggregateAdmissionTestManifest(t *testing.T, fixture *statsAggregateTestFixture, extra []byte) {
	t.Helper()
	current, err := os.ReadFile(filepath.Join(fixture.path, "CURRENT"))
	if err != nil || len(current) == 0 || current[len(current)-1] != '\n' {
		t.Fatalf("manifest test locator: %v", err)
	}
	path := filepath.Join(fixture.path, string(current[:len(current)-1]))
	raw, err := os.ReadFile(path)
	if err != nil || uint64(len(raw)) > fixture.bounds.MaxStorageBytes {
		t.Fatalf("manifest test source: %v", err)
	}
	reader := journal.NewReader(bytes.NewReader(raw), nil, true, true)
	var encoded bytes.Buffer
	writer := journal.NewWriter(&encoded)
	for {
		frame, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out, err := writer.Next()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, frame); err != nil {
			t.Fatal(err)
		}
	}
	out, err := writer.Next()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write(extra); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Native metadata replay would size arrays from these valid unsigned varints.
// Inspection refuses both compaction pointers and deleted-table ordinals first.
func TestStatsAggregateAdmissionHighNativeOrdinalsDoNotMutate(t *testing.T) {
	t.Parallel()
	for _, value := range []struct{ tag, level uint64 }{
		{tag: 5, level: 12}, {tag: 6, level: 12}, {tag: 7, level: 12},
		{tag: 5, level: uint64(1) << 40}, {tag: 6, level: uint64(1) << 40}, {tag: 7, level: uint64(1) << 40},
	} {
		tag := value.tag
		fixture, _ := newStatsAggregateOpenTestFixture(t)
		extra := binary.AppendUvarint(nil, tag)
		extra = binary.AppendUvarint(extra, value.level)
		if tag == 5 {
			key := append([]byte{'h'}, make([]byte, 8)...)
			key[1] = 1
			extra = binary.AppendUvarint(extra, uint64(len(key)))
			extra = append(extra, key...)
		} else {
			extra = binary.AppendUvarint(extra, 987654)
			if tag == 7 {
				extra = binary.AppendUvarint(extra, 48)
				key := append([]byte{'h'}, make([]byte, 8)...)
				key[1] = 1
				for index := 0; index < 2; index++ {
					extra = binary.AppendUvarint(extra, uint64(len(key)))
					extra = append(extra, key...)
				}
			}
		}
		appendStatsAggregateAdmissionTestManifest(t, fixture, extra)
		before := snapshotStatsAggregateOpenTestStore(t, fixture)
		observer := &statsAggregateOpenTestObserver{}
		store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step})
		if store != nil {
			_ = store.Close()
		}
		if store != nil || err == nil || !strings.Contains(err.Error(), "level is outside the owned producer boundary") {
			t.Fatalf("tag %d did not reach exact native ordinal refusal: %v", tag, err)
		}
		if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
			t.Fatalf("unsupported level mutated files: %v / %v", changes, observer.snapshot())
		}
	}
}

// Empty/sparse native levels and compaction pointers use the same supported
// ordinal domain as populated levels, without narrowing valid private history.
func TestStatsAggregateAdmissionSparseHighestLevelKeepsRealAuthority(t *testing.T) {
	t.Parallel()
	for _, tag := range []uint64{5, 6} {
		fixture, head := newStatsAggregateOpenTestFixture(t)
		extra := binary.AppendUvarint(nil, tag)
		extra = binary.AppendUvarint(extra, attemptAdmissionHighestLevel)
		if tag == 5 {
			key := append([]byte{'h'}, make([]byte, 8)...)
			key[1] = 1
			extra = binary.AppendUvarint(extra, uint64(len(key)))
			extra = append(extra, key...)
		} else {
			extra = binary.AppendUvarint(extra, 987654)
		}
		appendStatsAggregateAdmissionTestManifest(t, fixture, extra)
		assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
	}
}

// Physical files stay small: only correctly framed untrusted size declarations
// are large. No sparse giant file or native oversized allocation is attempted.
func TestStatsAggregateAdmissionNativeSizeSumOverflowDoesNotMutate(t *testing.T) {
	t.Parallel()
	fixture, _ := newStatsAggregateOpenTestFixture(t)
	var extra []byte
	key := append([]byte{'h'}, make([]byte, 8)...)
	key[1] = 1
	for _, number := range []uint64{987653, 987654} {
		extra = binary.AppendUvarint(extra, 7)
		extra = binary.AppendUvarint(extra, 1)
		extra = binary.AppendUvarint(extra, number)
		extra = binary.AppendUvarint(extra, uint64(^uint64(0)>>2)+100)
		for index := 0; index < 2; index++ {
			extra = binary.AppendUvarint(extra, uint64(len(key)))
			extra = append(extra, key...)
		}
	}
	appendStatsAggregateAdmissionTestManifest(t, fixture, extra)
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	observer := &statsAggregateOpenTestObserver{}
	bounds := fixture.bounds
	// A caller's very large byte ceiling is not permission for native signed
	// overflow. Existing ordinary fixture and production ceilings stay intact.
	bounds.MaxStorageBytes = ^uint64(0)
	disk, err := openAttemptRecordStoreInspection(context.Background(), fixture.path, attemptRecordStoreBounds{MaxStorageBytes: bounds.MaxStorageBytes, MaxStorageFiles: bounds.MaxStorageFiles}, attemptRecordStoreHooks{Step: observer.step}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	reader := &attemptStoreAdmissionReader{ctx: context.Background(), disk: disk}
	_, err = reader.manifest(81)
	err = errors.Join(err, disk.Close())
	if err == nil || !strings.Contains(err.Error(), "native table sum overflows") {
		t.Fatalf("native size overflow did not reach bounded admission: %v", err)
	}
	if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
		t.Fatalf("overflow refusal mutated files: %v / %v", changes, observer.snapshot())
	}
}

// Duplicate pointers/deletions cannot inflate native per-record arrays behind
// a small deduplicated inspection map. Native framing remains independently valid.
func TestStatsAggregateAdmissionRejectsDuplicateManifestArrayEntries(t *testing.T) {
	t.Parallel()
	for _, tag := range []uint64{5, 6} {
		fixture, _ := newStatsAggregateOpenTestFixture(t)
		entry := binary.AppendUvarint(nil, tag)
		entry = binary.AppendUvarint(entry, 1)
		if tag == 5 {
			key := append([]byte{'h'}, make([]byte, 8)...)
			key[1] = 1
			entry = binary.AppendUvarint(entry, uint64(len(key)))
			entry = append(entry, key...)
		} else {
			entry = binary.AppendUvarint(entry, 987654)
		}
		appendStatsAggregateAdmissionTestManifest(t, fixture, append(append([]byte(nil), entry...), entry...))
		before := snapshotStatsAggregateOpenTestStore(t, fixture)
		observer := &statsAggregateOpenTestObserver{}
		store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step})
		if store != nil {
			_ = store.Close()
		}
		if store != nil || err == nil || !strings.Contains(err.Error(), "admission duplicate") {
			t.Fatalf("duplicate native array tag %d was accepted: %v", tag, err)
		}
		if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
			t.Fatalf("duplicate metadata refusal mutated files: %v / %v", changes, observer.snapshot())
		}
	}
}

// The exact upper admitted ordinal can replay real backend-produced SST
// bytes; only its test-owned manifest level locator is relocated explicitly.
func TestStatsAggregateAdmissionHighestSupportedLevelKeepsRealAuthority(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	compactStatsAggregateAdmissionTestFixture(t, fixture, 0)
	disk, err := openAttemptRecordStoreInspection(context.Background(), fixture.path, attemptRecordStoreBounds{MaxStorageBytes: fixture.bounds.MaxStorageBytes, MaxStorageFiles: fixture.bounds.MaxStorageFiles}, attemptRecordStoreHooks{}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	reader := &attemptStoreAdmissionReader{ctx: context.Background(), disk: disk}
	manifest, err := reader.manifest(81)
	closeErr := disk.Close()
	if err != nil || closeErr != nil || manifest == nil || len(manifest.tables) != 1 {
		t.Fatalf("small real compaction did not make one table: %v", errors.Join(err, closeErr))
	}
	var extra []byte
	for _, table := range manifest.tables {
		extra = binary.AppendUvarint(extra, 6)
		extra = binary.AppendUvarint(extra, table.level)
		extra = binary.AppendUvarint(extra, uint64(table.number))
		extra = binary.AppendUvarint(extra, 7)
		extra = binary.AppendUvarint(extra, attemptAdmissionHighestLevel)
		extra = binary.AppendUvarint(extra, uint64(table.number))
		extra = binary.AppendUvarint(extra, table.size)
		for _, key := range [][]byte{table.minimum, table.maximum} {
			extra = binary.AppendUvarint(extra, uint64(len(key)))
			extra = append(extra, key...)
		}
	}
	appendStatsAggregateAdmissionTestManifest(t, fixture, extra)
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

// Level12 reaches native threshold overflow during initial computeCompaction,
// even for a small genuine table. Its bytes must stay preserved, never opened.
func TestStatsAggregateAdmissionOverflowThresholdLevelIsPreserved(t *testing.T) {
	t.Parallel()
	fixture, _ := newStatsAggregateOpenTestFixture(t)
	compactStatsAggregateAdmissionTestFixture(t, fixture, 0)
	disk, err := openAttemptRecordStoreInspection(context.Background(), fixture.path, attemptRecordStoreBounds{MaxStorageBytes: fixture.bounds.MaxStorageBytes, MaxStorageFiles: fixture.bounds.MaxStorageFiles}, attemptRecordStoreHooks{}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	reader := &attemptStoreAdmissionReader{ctx: context.Background(), disk: disk}
	manifest, err := reader.manifest(81)
	closeErr := disk.Close()
	if err != nil || closeErr != nil || manifest == nil || len(manifest.tables) != 1 {
		t.Fatalf("small real compaction did not make one table: %v", errors.Join(err, closeErr))
	}
	var extra []byte
	for _, table := range manifest.tables {
		extra = binary.AppendUvarint(extra, 6)
		extra = binary.AppendUvarint(extra, table.level)
		extra = binary.AppendUvarint(extra, uint64(table.number))
		extra = binary.AppendUvarint(extra, 7)
		extra = binary.AppendUvarint(extra, 12)
		extra = binary.AppendUvarint(extra, uint64(table.number))
		extra = binary.AppendUvarint(extra, table.size)
		for _, key := range [][]byte{table.minimum, table.maximum} {
			extra = binary.AppendUvarint(extra, uint64(len(key)))
			extra = append(extra, key...)
		}
	}
	appendStatsAggregateAdmissionTestManifest(t, fixture, extra)
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	observer := &statsAggregateOpenTestObserver{}
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || err == nil || !strings.Contains(err.Error(), "level is outside the owned producer boundary") {
		t.Fatalf("small level12 table reached native promotion: %v", err)
	}
	if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
		t.Fatalf("unsupported level12 table was modified: %v / %v", changes, observer.snapshot())
	}
}

// The arithmetic boundary rejects a configuration, not a silently reduced
// storage cap. It runs before any existing-namespace filesystem operation.
func TestStatsAggregateAdmissionUnsupportedStorageCeilingStopsBeforeIO(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	bounds := fixture.bounds
	bounds.MaxStorageBytes = attemptAdmissionMaximumStorageBytes + 1
	steps := 0
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, bounds, statsAggregateHooks{StorageStep: func(string, string) error { steps++; return nil }})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || err == nil || !strings.Contains(err.Error(), "storage ceiling exceeds the supported native arithmetic domain") || steps != 0 {
		t.Fatalf("unsupported storage budget reached I/O: steps=%d err=%v", steps, err)
	}
	if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 {
		t.Fatalf("unsupported budget modified existing state: %v", changes)
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

// Exact integer/float arithmetic is checked without evaluating the unsafe
// native level12 conversion or relying on any architecture's saturation.
func TestStatsAggregateAdmissionNativeThresholdAndReservationAreExact(t *testing.T) {
	t.Parallel()
	if attemptAdmissionHighestLevel != 11 || attemptAdmissionMaximumStorageBytes != 1_048_576_000_000_000_000 || attemptStoreMetadataReserve != 4096 {
		t.Fatal("pinned native threshold or existing metadata reservation changed")
	}
	options := &opt.Options{Strict: opt.StrictAll, BlockCacheCapacity: 8 * 1024 * 1024, WriteBuffer: 4 * 1024 * 1024, CompactionTableSize: 2 * 1024 * 1024, CompactionTableSizeMultiplier: 1, OpenFilesCacheCapacity: 64, DisableLargeBatchTransaction: true}
	for level := uint64(0); level <= attemptAdmissionHighestLevel; level++ {
		value := options.GetCompactionTotalSize(int(level))
		if value <= 0 {
			t.Fatalf("admitted native threshold is not positive at level%d", level)
		}
		if level == attemptAdmissionHighestLevel && uint64(value) != attemptAdmissionMaximumStorageBytes {
			t.Fatal("actual native level11 threshold differs from the proof")
		}
	}
	threshold := float64(attemptAdmissionMaximumStorageBytes)
	maximumTables := float64(attemptAdmissionMaximumStorageBytes - attemptStoreMetadataReserve)
	if uint64(threshold) != attemptAdmissionMaximumStorageBytes || threshold-maximumTables != attemptStoreMetadataReserve || maximumTables/threshold >= 1 {
		t.Fatal("real reservation permits source level11 to reach overflowing output level12")
	}
}

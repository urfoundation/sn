//go:build linux || darwin

package validator

// Actual native writes establish retained authority histories. Corrupt journal
// cases alter only a test-owned file using the pinned physical record framing.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb/journal"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// A valid later h cannot hide a foreign retained h, in either WAL or SST.
// All surrounding signed M8 data remains the original full fixture.
func TestStatsAggregateAdmissionRetainedForeignHeaderIsPreserved(t *testing.T) {
	t.Parallel()
	for _, compact := range []bool{false, true} {
		fixture, head := newStatsAggregateOpenTestFixture(t)
		store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
		snapshot, err := store.db.GetSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		defer snapshot.Release()
		foreign := head
		foreign.Identity.NoID++
		for _, value := range []statsAggregateHead{foreign, head} {
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.db.Put([]byte{'h'}, raw, &opt.WriteOptions{Sync: true, NoWriteMerge: true}); err != nil {
				t.Fatal(err)
			}
		}
		if compact {
			if err := store.db.CompactRange(util.Range{}); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		before := snapshotStatsAggregateOpenTestStore(t, fixture)
		observer := &statsAggregateOpenTestObserver{}
		opened, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step})
		if opened != nil {
			_ = opened.Close()
		}
		if opened != nil || err == nil || !strings.Contains(err.Error(), "existing namespace or config differs") {
			t.Fatalf("retained foreign header was hidden by its valid successor (table=%v): %v", compact, err)
		}
		if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
			t.Fatalf("foreign retained history was normalized: %v / %v", changes, observer.snapshot())
		}
	}
}

// Native Delete is used, not a fabricated header. The retained snapshot keeps
// the tombstone present when the actual compactor builds its positive-level SST.
func TestStatsAggregateAdmissionAuthorityTombstoneIsPreserved(t *testing.T) {
	t.Parallel()
	for _, compact := range []bool{false, true} {
		fixture, _ := newStatsAggregateOpenTestFixture(t)
		store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
		snapshot, err := store.db.GetSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		defer snapshot.Release()
		if err := store.db.Delete([]byte{'h'}, &opt.WriteOptions{Sync: true, NoWriteMerge: true}); err != nil {
			t.Fatal(err)
		}
		if compact {
			if err := store.db.CompactRange(util.Range{}); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		before := snapshotStatsAggregateOpenTestStore(t, fixture)
		observer := &statsAggregateOpenTestObserver{}
		opened, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step})
		if opened != nil {
			_ = opened.Close()
		}
		if opened != nil || err == nil || !strings.Contains(err.Error(), "admission authority was deleted") {
			t.Fatalf("deleted authority reached writable recovery (table=%v): %v", compact, err)
		}
		if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
			t.Fatalf("deleted authority was normalized: %v / %v", changes, observer.snapshot())
		}
	}
}

// Copies genuine bounded journal frames, then appends one explicitly supplied
// record. The native writer computes its physical checksum independently.
func appendStatsAggregateAdmissionTestJournal(t *testing.T, fixture *statsAggregateTestFixture, appendRecord func(uint64) []byte) {
	t.Helper()
	disk, err := openAttemptRecordStoreInspection(context.Background(), fixture.path, attemptRecordStoreBounds{MaxStorageBytes: fixture.bounds.MaxStorageBytes, MaxStorageFiles: fixture.bounds.MaxStorageFiles}, attemptRecordStoreHooks{}, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	descriptors, listErr := disk.List(storage.TypeJournal)
	err = errors.Join(listErr, disk.Close())
	if err != nil || len(descriptors) == 0 {
		t.Fatalf("native test journal is unavailable: %v", err)
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].Num < descriptors[j].Num })
	path := filepath.Join(fixture.path, descriptors[len(descriptors)-1].String())
	raw, err := os.ReadFile(path)
	if err != nil || uint64(len(raw)) > fixture.bounds.MaxStorageBytes {
		t.Fatalf("native test journal size: %v", err)
	}
	reader := journal.NewReader(bytes.NewReader(raw), nil, true, true)
	var encoded bytes.Buffer
	writer := journal.NewWriter(&encoded)
	var sequence uint64
	for {
		frame, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var header [12]byte
		if _, err := io.ReadFull(frame, header[:]); err != nil {
			t.Fatal(err)
		}
		sequence = binary.LittleEndian.Uint64(header[:8]) + uint64(binary.LittleEndian.Uint32(header[8:]))
		out, err := writer.Next()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write(header[:]); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, frame); err != nil {
			t.Fatal(err)
		}
	}
	if sequence == 0 {
		t.Fatal("native fixture did not establish a nonempty journal clock")
	}
	out, err := writer.Next()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write(appendRecord(sequence)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Correct journal CRC is not authority for malformed native sequence, lengths
// or row/batch shapes. Every case must refuse before a writable operation.
func TestStatsAggregateAdmissionMalformedJournalRecordsArePreserved(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"sequence", "count", "key", "header", "unknown-row", "trailing", "truncated"} {
		fixture, _ := newStatsAggregateOpenTestFixture(t)
		appendStatsAggregateAdmissionTestJournal(t, fixture, func(sequence uint64) []byte {
			header := make([]byte, 12)
			binary.LittleEndian.PutUint64(header[:8], sequence)
			binary.LittleEndian.PutUint32(header[8:], 1)
			switch variant {
			case "sequence":
				binary.LittleEndian.PutUint64(header[:8], sequence-1)
			case "count":
				binary.LittleEndian.PutUint32(header[8:], statsAggregateBatchOperations+1)
			case "key":
				return append(header, 1, 74)
			case "header":
				return binary.AppendUvarint(append(header, 1, 1, 'h'), fixture.bounds.MaxHeaderBytes+1)
			case "unknown-row":
				return append(header, 1, 1, 'x', 0)
			case "trailing":
				binary.LittleEndian.PutUint32(header[8:], 0)
				return append(header, 1)
			case "truncated":
				return header[:11]
			}
			return header
		})
		before := snapshotStatsAggregateOpenTestStore(t, fixture)
		observer := &statsAggregateOpenTestObserver{}
		opened, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step})
		if opened != nil {
			_ = opened.Close()
		}
		literal := map[string]string{"sequence": "WAL sequence or batch count", "count": "WAL sequence or batch count", "key": "WAL key is oversized", "header": "WAL header shape differs", "unknown-row": "contains an unknown row", "trailing": "WAL batch has extra records", "truncated": "unexpected EOF"}[variant]
		if opened != nil || err == nil || !strings.Contains(err.Error(), literal) {
			t.Fatalf("malformed journal %s missed exact framing refusal %q: %v", variant, literal, err)
		}
		if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 || len(observer.snapshot()) != 0 {
			t.Fatalf("malformed journal %s was rewritten: %v / %v", variant, changes, observer.snapshot())
		}
	}
}

// A caller may use the exact proven ceiling; admission never substitutes it
// for the existing smaller fixture or production policy values.
func TestStatsAggregateAdmissionExactStorageCeilingKeepsRealAuthority(t *testing.T) {
	t.Parallel()
	fixture, head := newStatsAggregateOpenTestFixture(t)
	bounds := fixture.bounds
	bounds.MaxStorageBytes = attemptAdmissionMaximumStorageBytes
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, bounds, statsAggregateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := store.Head(context.Background())
	err = errors.Join(readErr, store.Close())
	if err != nil || got != head {
		t.Fatalf("exact admitted storage ceiling lost real authority: %v", err)
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, head)
}

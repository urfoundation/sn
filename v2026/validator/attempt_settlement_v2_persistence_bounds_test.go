//go:build linux || darwin

package validator

// Exact byte controls compare the actual unchanged JSON codecs, not a second
// expected-size estimate. Runtime controls use real signed M8 journals and
// independently smaller allowances without changing the replay policy.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// The side effect proves counting refuses rather than evaluating an encoder
// and checking its already allocated output afterward.
type attemptSettlementV2CountCustom struct{ calls *int }

// This private test encoder must never be called by byte admission.
func (self attemptSettlementV2CountCustom) MarshalJSON() ([]byte, error) {
	*self.calls = *self.calls + 1
	return []byte(`"custom"`), nil
}

// Text marshalers are also external encoder implementations, even without a
// JSON-specific method. They have the same no-evaluation admission contract.
type attemptSettlementV2CountText struct{ calls *int }

// The counter must refuse this method without allocating its encoded bytes.
func (self attemptSettlementV2CountText) MarshalText() ([]byte, error) {
	*self.calls = *self.calls + 1
	return []byte("custom text"), nil
}

// Even a uint8 element can change a slice from base64 into custom JSON values.
type attemptSettlementV2CountByte uint8

// The counter refuses this custom element type instead of guessing base64.
func (self attemptSettlementV2CountByte) MarshalJSON() ([]byte, error) { return []byte(`"byte"`), nil }

// Real file Close counts leaf acquisition without replacing bytes or decoding.
func runtimeAttemptSettlementV2CountMetadataReads(physical *attemptSettlementV2IO, name string, reads *int) {
	physical.closeFile = func(file *os.File) error {
		if filepath.Base(file.Name()) == name {
			*reads = *reads + 1
		}
		return file.Close()
	}
}

// A finite sparse file exceeds the supplied allowance before any read buffer
// is allocated. The original real checkpoint remains separately recoverable.
func runtimeAttemptSettlementV2OversizeSnapshot(t *testing.T, participant AttemptSettlementRuntimeV2Participant, limit uint64) {
	t.Helper()
	path := filepath.Join(participant.StateDir, "stats.json")
	if _, err := os.Lstat(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".before-bound-control"); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(int64(limit+1)), file.Close()); err != nil {
		t.Fatal(err)
	}
}

// Journal creation is genuine; only the first later snapshot write fails.
func runtimeAttemptSettlementV2ActivationJournal(t *testing.T, fixture *attemptSettlementRuntimeV2TestFixture) []byte {
	t.Helper()
	physical := attemptSettlementV2PhysicalIO()
	cause := errors.New("bounded fixture activation snapshot refusal")
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error { return cause }
	if err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); !errors.Is(err, cause) {
		t.Fatalf("actual activation journal prerequisite: %v", err)
	}
	encoded, err := os.ReadFile(attemptSettlementTransactionV2Path(fixture.coordinator))
	if err != nil || len(encoded) < 2 {
		t.Fatalf("actual activation journal missing: %v", err)
	}
	return encoded
}

// Three actual runtime entry points reject zero/unsafe independent bounds
// before opening roots, reserving owners or calling replay/persistence hooks.
func TestAttemptSettlementRuntimeV2PersistenceLimitsBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	for _, persistence := range []AttemptSettlementRuntimeV2PersistenceBounds{
		{}, {MaxSnapshotBytes: 1, MaxJournalBytes: 0}, {MaxSnapshotBytes: 0, MaxJournalBytes: 1},
		{MaxSnapshotBytes: uint64(^uint(0) >> 1), MaxJournalBytes: 1}, {MaxSnapshotBytes: 1, MaxJournalBytes: ^uint64(0)},
	} {
		calls := 0
		physical := attemptSettlementV2PhysicalIO()
		physical.step = func(string) error { calls++; return nil }
		physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
			calls++
			return errors.New("unexpected snapshot")
		}
		physical.writeJournal = func(*attemptPrivateDirectory, string, []byte) error { calls++; return errors.New("unexpected journal") }
		options := fixture.options(t)
		options.Persistence = persistence
		if err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, options.Authority, persistence, physical); err == nil {
			t.Fatal("invalid persistence allowance entered initialization")
		}
		if closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, options, physical, true); err == nil || closure != nil {
			t.Fatal("invalid persistence allowance entered advance")
		}
		fresh := append([]AttemptSettlementRuntimeV2Participant(nil), fixture.participants...)
		for index := range fresh {
			fresh[index].Stats = NewStatsEngine(fresh[index].Stats.cfg)
		}
		if err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fresh, options.Authority, persistence, physical); err == nil {
			t.Fatal("invalid persistence allowance entered recovery")
		}
		if calls != 0 || !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
			t.Fatalf("invalid persistence bound reached IO or changed state: %d", calls)
		}
		for _, participant := range fixture.participants {
			if participant.Stats.attemptCutPending {
				t.Fatal("invalid persistence bound reserved a live owner")
			}
		}
	}
}

// The low-level v2 reader does not inherit legacy's explicit zero-limit mode.
func TestAttemptSettlementRuntimeV2PersistenceReaderRejectsZeroBeforeOpen(t *testing.T) {
	t.Parallel()
	dir := newAttemptSettlementRuntimeV2TestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, "stats.json"), []byte("real finite bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openAttemptSettlementV2Root(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.close()
	physical, reads := attemptSettlementV2PhysicalIO(), 0
	runtimeAttemptSettlementV2CountMetadataReads(&physical, "stats.json", &reads)
	for _, limit := range []uint64{0, uint64(^uint(0) >> 1), ^uint64(0)} {
		data, _, err := readAttemptSettlementV2File(root, "stats.json", limit, physical)
		if err == nil || data != nil || reads != 0 {
			t.Fatalf("unsafe v2 read bound reached leaf IO: %d/%d/%v", limit, reads, err)
		}
	}
}

// The same real journal succeeds at its exact allowance after one-short
// refusal; the smaller attempt cannot write or release the retained gate.
func TestAttemptSettlementRuntimeV2PersistenceInitJournalReadBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	encoded := runtimeAttemptSettlementV2ActivationJournal(t, fixture)
	persistence := runtimeAttemptSettlementV2TestPersistence()
	persistence.MaxJournalBytes = uint64(len(encoded) - 1)
	physical, reads, writes := attemptSettlementV2PhysicalIO(), 0, 0
	runtimeAttemptSettlementV2CountMetadataReads(&physical, "settlement-transaction-v2.json", &reads)
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected bounded init write")
	}
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, persistence, physical)
	if err == nil || reads != 0 || writes != 0 {
		t.Fatalf("oversized initialization journal reached IO: %v/%d/%d", err, reads, writes)
	}
	fixture.assertReserved(t, 42)
	persistence.MaxJournalBytes++
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, persistence); err != nil {
		t.Fatalf("exact journal allowance refused: %v", err)
	}
}

// A real partial terminal retains its original immutable streams and journal.
func TestAttemptSettlementRuntimeV2PersistenceAdvanceJournalReadBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	transaction := fixture.partialWrite(t)
	encoded, err := os.ReadFile(attemptSettlementTransactionV2Path(fixture.coordinator))
	if err != nil {
		t.Fatal(err)
	}
	options := fixture.options(t)
	options.Persistence.MaxJournalBytes = uint64(len(encoded) - 1)
	physical, reads, writes := attemptSettlementV2PhysicalIO(), 0, 0
	runtimeAttemptSettlementV2CountMetadataReads(&physical, "settlement-transaction-v2.json", &reads)
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected bounded retry write")
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, options, physical, true)
	if err == nil || closure != nil || reads != 0 || writes != 0 {
		t.Fatalf("oversized terminal journal reached IO: %v/%d/%d", err, reads, writes)
	}
	fixture.assertReserved(t, 43)
	options = fixture.options(t)
	options.Persistence.MaxJournalBytes = uint64(len(encoded))
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, options); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// Fresh startup cannot attach even one operator after an oversized journal.
func TestAttemptSettlementRuntimeV2PersistenceRecoverJournalReadBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	transaction := fixture.partialWrite(t)
	encoded, err := os.ReadFile(attemptSettlementTransactionV2Path(fixture.coordinator))
	if err != nil {
		t.Fatal(err)
	}
	restarted := fixture.reopen(t)
	persistence := runtimeAttemptSettlementV2TestPersistence()
	persistence.MaxJournalBytes = uint64(len(encoded) - 1)
	physical, reads, writes := attemptSettlementV2PhysicalIO(), 0, 0
	runtimeAttemptSettlementV2CountMetadataReads(&physical, "settlement-transaction-v2.json", &reads)
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected bounded recovery write")
	}
	err = recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, persistence, physical)
	if err == nil || reads != 0 || writes != 0 {
		t.Fatalf("oversized startup journal reached IO: %v/%d/%d", err, reads, writes)
	}
	for _, participant := range restarted {
		if participant.Stats.attemptLedger != nil {
			t.Fatal("bounded startup refusal attached an operator")
		}
	}
	persistence.MaxJournalBytes++
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, persistence); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, restarted, transaction)
}

// Each read-site variant exceeds only the file allowance. Its real in-memory
// checkpoint remains small, so live-output admission cannot mask this edge.
func TestAttemptSettlementRuntimeV2PersistenceCurrentActivationSnapshotReadBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	persistence := runtimeAttemptSettlementV2TestPersistence()
	runtimeAttemptSettlementV2OversizeSnapshot(t, fixture.participants[0], persistence.MaxSnapshotBytes)
	physical, reads := attemptSettlementV2PhysicalIO(), 0
	runtimeAttemptSettlementV2CountMetadataReads(&physical, "stats.json", &reads)
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, persistence, physical)
	if err == nil || reads != 0 {
		t.Fatalf("oversized current activation snapshot was opened: %v/%d", err, reads)
	}
}

// Complete real replay of the existing immutable terminal still precedes the
// current retry's bounded on-disk successor check.
func TestAttemptSettlementRuntimeV2PersistenceCurrentTerminalSnapshotReadBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	options := fixture.options(t)
	runtimeAttemptSettlementV2OversizeSnapshot(t, fixture.participants[0], options.Persistence.MaxSnapshotBytes)
	physical, reads := attemptSettlementV2PhysicalIO(), 0
	runtimeAttemptSettlementV2CountMetadataReads(&physical, "stats.json", &reads)
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, options, physical, true)
	if err == nil || closure != nil || reads != 0 {
		t.Fatalf("oversized current terminal snapshot was opened: %v/%d", err, reads)
	}
	fixture.assertReserved(t, 43)
}

// This is the no-journal startup branch, not the transaction parser above.
func TestAttemptSettlementRuntimeV2PersistenceRecoverySnapshotReadBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	persistence := runtimeAttemptSettlementV2TestPersistence()
	runtimeAttemptSettlementV2OversizeSnapshot(t, fixture.participants[0], persistence.MaxSnapshotBytes)
	restarted := fixture.reopen(t)
	physical, reads := attemptSettlementV2PhysicalIO(), 0
	runtimeAttemptSettlementV2CountMetadataReads(&physical, "stats.json", &reads)
	err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, persistence, physical)
	if err == nil || reads != 0 {
		t.Fatalf("oversized startup snapshot was opened: %v/%d", err, reads)
	}
	for _, participant := range restarted {
		if participant.Stats.attemptLedger != nil {
			t.Fatal("oversized snapshot startup attached a partial operator")
		}
	}
}

// Original-disk capture has the same independent bound as recovery, even
// though its real complete terminal has just been sealed successfully.
func TestAttemptSettlementRuntimeV2PersistenceOriginalSnapshotReadBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	options := fixture.options(t)
	runtimeAttemptSettlementV2OversizeSnapshot(t, fixture.participants[0], options.Persistence.MaxSnapshotBytes)
	physical, reads := attemptSettlementV2PhysicalIO(), 0
	runtimeAttemptSettlementV2CountMetadataReads(&physical, "stats.json", &reads)
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, options, physical, true)
	if err == nil || closure != nil || reads != 0 {
		t.Fatalf("oversized original snapshot was opened: %v/%d", err, reads)
	}
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused original image published journal: %v", err)
	}
}

// A callback grows a previously admitted disk file after journal durability;
// the second exact-known-image pass must admit its size again before reading.
func TestAttemptSettlementRuntimeV2PersistenceKnownImageSnapshotReadBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	options := fixture.options(t)
	physical, reads, journals, writes := attemptSettlementV2PhysicalIO(), 0, 0, 0
	runtimeAttemptSettlementV2CountMetadataReads(&physical, "stats.json", &reads)
	physical.writeJournal = func(root *attemptPrivateDirectory, name string, data []byte) error {
		if err := writeAttemptSettlementV2OwnedState(root, name, data); err != nil {
			return err
		}
		journals++
		runtimeAttemptSettlementV2OversizeSnapshot(t, fixture.participants[0], options.Persistence.MaxSnapshotBytes)
		reads = 0
		return nil
	}
	physical.writeSnapshot = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected post-growth write")
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, options, physical, true)
	if err == nil || closure != nil || journals != 1 || reads != 0 || writes != 0 {
		t.Fatalf("oversized known image reached IO: %v/%d/%d/%d", err, journals, reads, writes)
	}
	fixture.assertReserved(t, 43)
}

// Journal framing cannot grant each nested decoded snapshot a larger bound.
func TestAttemptSettlementRuntimeV2PersistenceNestedSnapshotBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	transaction := fixture.partialWrite(t)
	encoded, err := os.ReadFile(attemptSettlementTransactionV2Path(fixture.coordinator))
	if err != nil {
		t.Fatal(err)
	}
	persistence := runtimeAttemptSettlementV2TestPersistence()
	var largest int
	for _, entry := range transaction.Snapshots {
		for _, image := range [][]byte{entry.OriginalJSON, entry.PreImageJSON, entry.StatsJSON} {
			if len(image) > largest {
				largest = len(image)
			}
		}
	}
	if largest < 2 {
		t.Fatal("real nested snapshot prerequisite missing")
	}
	persistence.MaxSnapshotBytes = uint64(largest - 1)
	if result, _, _, err := decodeAttemptSettlementTransactionV2(t.Context(), encoded, fixture.participants, persistence); err == nil || result != nil {
		t.Fatal("nested snapshot exceeded independent allowance")
	}
	persistence.MaxSnapshotBytes++
	if _, _, _, err := decodeAttemptSettlementTransactionV2(t.Context(), encoded, fixture.participants, persistence); err != nil {
		t.Fatalf("exact nested snapshot allowance refused: %v", err)
	}
}

// Activation's larger postimage is counted before any JSON output or journal
// write, even when the entire original live image fits exactly.
func TestAttemptSettlementRuntimeV2PersistenceSnapshotOutputBeforeWrite(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	persistence := runtimeAttemptSettlementV2TestPersistence()
	persistence.MaxSnapshotBytes = 0
	for _, data := range before {
		if uint64(len(data)) > persistence.MaxSnapshotBytes {
			persistence.MaxSnapshotBytes = uint64(len(data))
		}
	}
	physical, writes := attemptSettlementV2PhysicalIO(), 0
	physical.writeJournal = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected oversized output journal")
	}
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, persistence, physical)
	if err == nil || writes != 0 || !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatalf("oversized activation postimage reached persistence: %v/%d", err, writes)
	}
}

// The independent journal allowance includes three nested images' base64
// expansion; a snapshot allowance cannot act as an implicit journal default.
func TestAttemptSettlementRuntimeV2PersistenceJournalOutputBeforeWrite(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	persistence := runtimeAttemptSettlementV2TestPersistence()
	persistence.MaxJournalBytes = 1
	physical, writes := attemptSettlementV2PhysicalIO(), 0
	physical.writeJournal = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected oversized journal output")
	}
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, persistence, physical)
	if err == nil || writes != 0 {
		t.Fatalf("oversized journal output reached persistence: %v/%d", err, writes)
	}
}

// Full real signed legacy history and compact successors use the unchanged
// indented codec; byte-exact and one-short limits are tested independently.
func TestAttemptSettlementRuntimeV2PersistenceRealSnapshotCountsExact(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2LegacyHistoryFixture(t, true)
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	for _, participant := range fixture.participants {
		snapshot := participant.Stats.snapshotStats()
		actual, err := encodeStatsSnapshot(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		size, err := countAttemptSettlementV2JSON(t.Context(), snapshot, uint64(len(actual)), true, true)
		if err != nil || size != uint64(len(actual)) {
			t.Fatalf("real indented snapshot count differs: %d/%d/%v", size, len(actual), err)
		}
		encoded, err := encodeAttemptSettlementV2Stats(t.Context(), snapshot, uint64(len(actual)))
		if err != nil || !bytes.Equal(encoded, actual) {
			t.Fatalf("bounded snapshot changed existing bytes: %v", err)
		}
		if encoded, err := encodeAttemptSettlementV2Stats(t.Context(), snapshot, uint64(len(actual)-1)); err == nil || encoded != nil {
			t.Fatal("one-short indented snapshot allowance accepted")
		}
	}
}

// Genuine M8 terminal bytes, original disk and raw live/post images all enter
// the journal's exact compact base64 reservation before its marshal.
func TestAttemptSettlementRuntimeV2PersistenceRealJournalCountsExact(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 15, 1)
	fixture.trails(t, 1, 2, 1)
	transaction := fixture.partialWrite(t)
	actual, err := json.Marshal(transaction)
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	size, err := countAttemptSettlementV2JSON(t.Context(), transaction, uint64(len(actual)), false, true)
	if err != nil || size != uint64(len(actual)) {
		t.Fatalf("real nested journal count differs: %d/%d/%v", size, len(actual), err)
	}
	encoded, err := canonicalAttemptSettlementTransactionV2(t.Context(), transaction, uint64(len(actual)))
	if err != nil || !bytes.Equal(encoded, actual) {
		t.Fatalf("bounded journal changed actual bytes: %v", err)
	}
	if encoded, err := canonicalAttemptSettlementTransactionV2(t.Context(), transaction, uint64(len(actual)-1)); err == nil || encoded != nil {
		t.Fatal("one-short nested journal allowance accepted")
	}
}

// These values exercise invalid UTF-8, HTML escapes, line separators, finite
// float formatting, zero values, nil/empty fields, UUIDs and base64 padding.
func TestAttemptSettlementRuntimeV2PersistenceEncodingAdjacentExact(t *testing.T) {
	t.Parallel()
	type sample struct {
		Text      string              `json:"text"`
		Bytes     []byte              `json:"bytes"`
		Omitted   []byte              `json:"omitted,omitempty"`
		ID        connect.Id          `json:"id"`
		Pointer   *connect.Id         `json:"pointer,omitempty"`
		Numbers   []float64           `json:"numbers"`
		Numbers32 []float32           `json:"numbers32"`
		Fixed     [4]byte             `json:"fixed"`
		Values    map[string][]string `json:"values"`
		Zero      uint64              `json:"zero,omitempty"`
	}
	id := connect.Id{1, 2, 3}
	for _, value := range []sample{
		{},
		{Text: "\"\\\b\f\n\r\t\x00<>&\u2028\u2029\xff世界", Bytes: []byte{}, Omitted: []byte{}, ID: id, Pointer: &id, Numbers: []float64{0, math.Copysign(0, -1), 1e-7, 1e-6, 1e20, 1e21, math.MaxFloat64, math.SmallestNonzeroFloat64}, Values: map[string][]string{"z": nil, "a<\xff": {}, "empty": {"", "\x01"}}},
		{Bytes: []byte{0}, Numbers32: []float32{0, 1e-7, 1e-6, 1e20, 1e21, math.MaxFloat32, math.SmallestNonzeroFloat32}, Fixed: [4]byte{0, 1, 2, 255}, Values: map[string][]string{}}, {Bytes: []byte{0, 1}}, {Bytes: []byte{0, 1, 2}}, {Bytes: []byte{0, 1, 2, 3}},
	} {
		for _, indent := range []bool{false, true} {
			var actual []byte
			var err error
			if indent {
				actual, err = json.MarshalIndent(value, "", "  ")
			} else {
				actual, err = json.Marshal(value)
			}
			if err != nil {
				t.Fatal(err)
			}
			actual = append(actual, '\n')
			encoded, err := marshalAttemptSettlementV2JSON(t.Context(), value, uint64(len(actual)), indent, true)
			if err != nil || !bytes.Equal(encoded, actual) {
				t.Fatalf("adjacent JSON count/encoding differs indent=%t: %v", indent, err)
			}
			if _, err := countAttemptSettlementV2JSON(t.Context(), value, uint64(len(actual)-1), indent, true); err == nil {
				t.Fatal("one-short adjacent JSON allowance accepted")
			}
		}
	}
}

// Unsupported dynamic or custom encoders are refused without calling them.
func TestAttemptSettlementRuntimeV2PersistenceCustomEncodersRefused(t *testing.T) {
	t.Parallel()
	calls := 0
	for _, value := range []any{attemptSettlementV2CountCustom{calls: &calls}, &attemptSettlementV2CountCustom{calls: &calls}, (*attemptSettlementV2CountCustom)(nil), attemptSettlementV2CountText{calls: &calls}, []attemptSettlementV2CountByte{1}, json.RawMessage(`{"x":1}`), json.Number("1"), map[string]any{"dynamic": "value"}} {
		if encoded, err := marshalAttemptSettlementV2JSON(t.Context(), value, 1024, false, true); err == nil || encoded != nil || calls != 0 {
			t.Fatalf("unsupported encoder was evaluated: %T/%v/%d", value, err, calls)
		}
	}
}

// Size+1 overflow, cancellation, cycles and non-finite values are rejected by
// admission before any encoder output is allocated.
func TestAttemptSettlementRuntimeV2PersistenceCounterUnsafeInputsRefused(t *testing.T) {
	t.Parallel()
	for _, limit := range []uint64{0, uint64(^uint(0) >> 1), ^uint64(0)} {
		if encoded, err := marshalAttemptSettlementV2JSON(t.Context(), "", limit, false, true); err == nil || encoded != nil {
			t.Fatal("unsafe metadata arithmetic allowance accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := countAttemptSettlementV2JSON(ctx, "actual", 1024, false, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("counter cancellation lost: %v", err)
	}
	type cycle struct {
		Next *cycle `json:"next"`
	}
	loop := &cycle{}
	loop.Next = loop
	for _, value := range []any{loop, math.Inf(1), math.NaN()} {
		if encoded, err := marshalAttemptSettlementV2JSON(t.Context(), value, 1024, false, true); err == nil || encoded != nil {
			t.Fatalf("unsafe metadata value accepted: %T", value)
		}
	}
}

//go:build linux || darwin

package validator

// These controls force re-observation after actual closes and filesystem
// changes. Signed runtime cases retain the existing M8 fixtures and replay;
// physical cases separately exercise explicit image and removal transitions.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Preserve the original inode while installing independent exact bytes. The
// replacement cannot accidentally reuse an inode freed by the test itself.
func runtimeAttemptSettlementV2ReplaceRetainedLeaf(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".retained-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(path)
	if err != nil || os.SameFile(before, after) {
		t.Fatalf("replacement inode prerequisite differs: %v", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("replacement byte prerequisite differs: %v", err)
	}
}

// Each standalone physical control starts with an actual bounded ordinary
// read, including explicit absence; no expected registry entry is fabricated.
func runtimeAttemptSettlementV2RetainedLeaf(t *testing.T, name string, data []byte) (attemptSettlementV2IO, *attemptPrivateDirectory, attemptSettlementV2LeafWitness) {
	t.Helper()
	dir := newAttemptSettlementRuntimeV2TestStateDir(t)
	if data != nil {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	physical := attemptSettlementV2PhysicalIO().withWitnesses(t.Context())
	root, release, err := physical.openRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release(); _ = physical.closeRoots() })
	actual, exists, err := readAttemptSettlementV2File(root, name, 64, physical)
	if err != nil || exists != (data != nil) || !bytes.Equal(actual, data) {
		t.Fatalf("initial retained leaf prerequisite differs: %v/%t", err, exists)
	}
	return physical, root, physical.witnesses.roots[dir].leaves[name]
}

// The journal readback's real Close changes an earlier stats inode only after
// the original-disk capture. All image hashes still match on the later read.
func TestAttemptSettlementRuntimeV2RetainedJournalReadbackAdvance(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	fixture.trails(t, 1, 1, 0)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	path := filepath.Join(fixture.participants[0].StateDir, "stats.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	physical, statsCloses, journalCloses, writes := attemptSettlementV2PhysicalIO(), 0, 0, 0
	physical.closeFile = func(file *os.File) error {
		if err := file.Close(); err != nil {
			return err
		}
		if file.Name() == path {
			statsCloses++
		}
		if file.Name() == attemptSettlementTransactionV2Path(fixture.coordinator) {
			journalCloses++
			if statsCloses != 1 || journalCloses != 1 {
				t.Fatal("advance retained read ordering prerequisite differs")
			}
			runtimeAttemptSettlementV2ReplaceRetainedLeaf(t, path, original, 0o600)
		}
		return nil
	}
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, data []byte) error {
		writes++
		return writeAttemptSettlementV2OwnedState(root, name, data)
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err == nil || closure != nil || journalCloses != 1 || writes != 0 || !strings.Contains(err.Error(), "compact retained leaf witness changed") {
		t.Fatalf("journal readback advance re-adopted an earlier same-byte stats inode: %v/%d/%d", err, journalCloses, writes)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("refused retained advance changed the live image")
	}
	fixture.assertReserved(t, 43)
	transaction := fixture.transaction(t)
	if !bytes.Equal(transaction.Snapshots[0].OriginalJSON, original) {
		t.Fatal("retained journal lost its exact original image")
	}
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// Empty activation also captures existing exact checkpoints before creating
// its real journal, so it must retain the same inode across journal readback.
func TestAttemptSettlementRuntimeV2RetainedJournalReadbackInitialization(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	for _, participant := range fixture.participants {
		if err := participant.Stats.Save(participant.StateDir); err != nil {
			t.Fatal(err)
		}
	}
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	path := filepath.Join(fixture.participants[0].StateDir, "stats.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	physical, statsCloses, journalCloses, writes := attemptSettlementV2PhysicalIO(), 0, 0, 0
	physical.closeFile = func(file *os.File) error {
		if err := file.Close(); err != nil {
			return err
		}
		if file.Name() == path {
			statsCloses++
		}
		if file.Name() == attemptSettlementTransactionV2Path(fixture.coordinator) {
			journalCloses++
			if statsCloses != 1 || journalCloses != 1 {
				t.Fatal("initialization retained read ordering prerequisite differs")
			}
			runtimeAttemptSettlementV2ReplaceRetainedLeaf(t, path, original, 0o600)
		}
		return nil
	}
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, data []byte) error {
		writes++
		return writeAttemptSettlementV2OwnedState(root, name, data)
	}
	err = initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if err == nil || journalCloses != 1 || writes != 0 || !strings.Contains(err.Error(), "compact retained leaf witness changed") {
		t.Fatalf("journal readback initialization re-adopted an earlier same-byte stats inode: %v/%d/%d", err, journalCloses, writes)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("refused retained initialization changed the live image")
	}
	fixture.assertReserved(t, 42)
	transaction := fixture.transaction(t)
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// Current retry closes its read owner before opening the same child again for
// immutable resync. The second stable read cannot replace the first witness.
func TestAttemptSettlementRuntimeV2RetainedCurrentRetryClosureResync(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	accepted := runtimeAttemptSettlementV2CloseForWitness(t, fixture)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	path := AttemptSettlementClosureV2Path(fixture.coordinator, 42)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	physical, childCloses, childSyncs := attemptSettlementV2PhysicalIO(), 0, 0
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		pathOfRoot := root.path
		if err := root.close(); err != nil {
			return err
		}
		if pathOfRoot == filepath.Dir(path) {
			childCloses++
			if childCloses == 1 {
				runtimeAttemptSettlementV2ReplaceRetainedLeaf(t, path, original, 0o400)
			}
		}
		return nil
	}
	physical.syncDirectory = func(file *os.File) error {
		if file.Name() == filepath.Dir(path) {
			childSyncs++
		}
		return file.Sync()
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err == nil || closure != nil || childCloses != 2 || childSyncs != 0 || !strings.Contains(err.Error(), "compact retained leaf witness changed") {
		t.Fatalf("current retry resync re-adopted a same-byte immutable inode: %v/%d/%d", err, childCloses, childSyncs)
	}
	fixture.assertReserved(t, 43)
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("refused retained current retry refolded live state")
	}
	retry, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil || !reflect.DeepEqual(retry, accepted) || !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatalf("fresh exact current retry differs: %v", err)
	}
}

// Snapshot mutation is admitted after the external before-write boundary;
// exact postimage bytes cannot authorize overwriting an already replaced leaf.
func TestAttemptSettlementRuntimeV2RetainedSnapshotMutationBeforeWrite(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	path := filepath.Join(fixture.participants[0].StateDir, "stats.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	physical, mutations, writes := attemptSettlementV2PhysicalIO(), 0, 0
	physical.step = func(step string) error {
		if step == "before-snapshot-9" {
			mutations++
			runtimeAttemptSettlementV2ReplaceRetainedLeaf(t, path, original, 0o600)
		}
		return nil
	}
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, data []byte) error {
		writes++
		return writeAttemptSettlementV2OwnedState(root, name, data)
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err == nil || closure != nil || mutations != 1 || writes != 0 || !strings.Contains(err.Error(), "compact retained leaf witness changed") {
		t.Fatalf("snapshot write erased a pre-callback retained inode mismatch: %v/%d/%d", err, mutations, writes)
	}
	fixture.assertReserved(t, 43)
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("refused retained snapshot write published live state")
	}
	_ = fixture.transaction(t)
}

// A recreated but byte-identical journal must remain available when the
// retained-removal precondition fails, after coherent reserved publication.
func TestAttemptSettlementRuntimeV2RetainedJournalMutationBeforeRemoval(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	path := attemptSettlementTransactionV2Path(fixture.coordinator)
	physical, mutations, removals := attemptSettlementV2PhysicalIO(), 0, 0
	physical.step = func(step string) error {
		if step == "before-journal-removal" {
			original, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			mutations++
			runtimeAttemptSettlementV2ReplaceRetainedLeaf(t, path, original, 0o600)
		}
		return nil
	}
	physical.removeJournal = func(root *attemptPrivateDirectory, name string) error {
		removals++
		return removeAttemptSettlementV2OwnedTransaction(root, name)
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err == nil || closure != nil || mutations != 1 || removals != 0 || !strings.Contains(err.Error(), "compact retained leaf witness changed") {
		t.Fatalf("journal removal erased a retained inode mismatch: %v/%d/%d", err, mutations, removals)
	}
	fixture.assertReserved(t, 43)
	transaction := fixture.transaction(t)
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// A second ordinary read of exact bytes still belongs to the first inode.
func TestAttemptSettlementRuntimeV2RetainedOrdinarySameBytes(t *testing.T) {
	data := []byte("prior\n")
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", data)
	runtimeAttemptSettlementV2ReplaceRetainedLeaf(t, filepath.Join(root.path, "stats.json"), data, 0o600)
	actual, _, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if err == nil || actual != nil || retained != prior || closeErr == nil {
		t.Fatalf("ordinary exact reread re-adopted a replacement inode: read=%v close=%v", err, closeErr)
	}
}

// An explicit timestamp transition is deterministic without relying on clock
// resolution, sleeps or scheduler ordering. Bytes and inode remain identical.
func TestAttemptSettlementRuntimeV2RetainedOrdinaryWriteState(t *testing.T) {
	data := []byte("prior\n")
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", data)
	path := filepath.Join(root.path, "stats.json")
	stamp := time.Unix(prior.state.modifySeconds+37, prior.state.modifyNanoseconds)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	changed, err := root.stat("stats.json")
	if err != nil || changed.ino != prior.state.ino || changed == prior.state {
		t.Fatalf("write-state transition prerequisite differs: %v", err)
	}
	actual, _, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if err == nil || actual != nil || retained != prior || closeErr == nil {
		t.Fatalf("ordinary reread re-adopted changed same-inode write-state: read=%v close=%v", err, closeErr)
	}
}

// Both modes remain private and locally readable; only the retained complete
// metadata requirement distinguishes the changed permission state.
func TestAttemptSettlementRuntimeV2RetainedOrdinaryMode(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", []byte("prior\n"))
	if err := os.Chmod(filepath.Join(root.path, "stats.json"), 0o400); err != nil {
		t.Fatal(err)
	}
	actual, _, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if err == nil || actual != nil || retained != prior || closeErr == nil {
		t.Fatalf("ordinary reread re-adopted changed private permissions: read=%v close=%v", err, closeErr)
	}
}

// Growth stays below the same explicit read allowance, so a size-limit refusal
// cannot mask loss of the earlier retained write-state.
func TestAttemptSettlementRuntimeV2RetainedOrdinaryGrowth(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", []byte("prior\n"))
	path := filepath.Join(root.path, "stats.json")
	if err := os.WriteFile(path, []byte("prior plus bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := root.stat("stats.json")
	if err != nil || changed.ino != prior.state.ino || changed.size <= prior.state.size || changed.size > 64 {
		t.Fatalf("bounded growth prerequisite differs: %v", err)
	}
	actual, _, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if err == nil || actual != nil || retained != prior || closeErr == nil {
		t.Fatalf("ordinary reread re-adopted bounded same-inode growth: read=%v close=%v", err, closeErr)
	}
}

// Stable local absence on a later call cannot erase an earlier occupied leaf.
func TestAttemptSettlementRuntimeV2RetainedOrdinaryDisappearance(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", []byte("prior\n"))
	path := filepath.Join(root.path, "stats.json")
	if err := os.Rename(path, path+".retained-original"); err != nil {
		t.Fatal(err)
	}
	actual, exists, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if err == nil || actual != nil || exists || retained != prior || closeErr == nil {
		t.Fatalf("ordinary absent reread erased a retained occupied leaf: read=%v close=%v", err, closeErr)
	}
}

// A reader is not a creator: appearance after witnessed absence is refused.
func TestAttemptSettlementRuntimeV2RetainedOrdinaryArrival(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", nil)
	if err := os.WriteFile(filepath.Join(root.path, "stats.json"), []byte("arrival\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	actual, exists, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if err == nil || actual != nil || !exists || retained != prior || closeErr == nil {
		t.Fatalf("ordinary reread erased retained absence after arrival: read=%v close=%v", err, closeErr)
	}
}

// A different native type is still refused before an open; retaining an older
// witness must not weaken the existing no-blocking regular-file admission.
func TestAttemptSettlementRuntimeV2RetainedOrdinaryFIFO(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", []byte("prior\n"))
	path := filepath.Join(root.path, "stats.json")
	if err := os.Rename(path, path+".retained-original"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	opens := 0
	physical.step = func(step string) error {
		if step == "before-open" {
			opens++
		}
		return nil
	}
	actual, _, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if err == nil || actual != nil || opens != 0 || retained != prior || closeErr == nil {
		t.Fatalf("retained FIFO refusal lost native admission or prior witness: %v/%v/%d", err, closeErr, opens)
	}
}

// Repeated actual reads can retain either one unchanged file or absence.
func TestAttemptSettlementRuntimeV2RetainedOrdinaryStable(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("prior\n")} {
		physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", data)
		for range 2 {
			actual, exists, err := readAttemptSettlementV2File(root, "stats.json", 64, physical)
			if err != nil || exists != prior.exists || !bytes.Equal(actual, data) || physical.witnesses.roots[root.path].leaves["stats.json"] != prior {
				t.Fatalf("unchanged repeated leaf observation differs: %v", err)
			}
		}
		if err := physical.closeRoots(); err != nil {
			t.Fatalf("unchanged repeated leaf completion differs: %v", err)
		}
	}
}

// An authorized exact atomic replacement advances a present witness, and a
// later ordinary read must preserve that newly committed physical identity.
func TestAttemptSettlementRuntimeV2RetainedExactWriteExisting(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", []byte("prior\n"))
	data := []byte("after\n")
	if err := physical.writeImage(root, "stats.json", data, uint64(len(data)), physical.writeSnapshot); err != nil {
		t.Fatal(err)
	}
	expected := physical.witnesses.roots[root.path].leaves["stats.json"]
	actual, exists, err := readAttemptSettlementV2File(root, "stats.json", uint64(len(data)), physical)
	closeErr := physical.closeRoots()
	if err != nil || closeErr != nil || !exists || !bytes.Equal(actual, data) || !expected.exists || expected.state.ino == prior.state.ino {
		t.Fatalf("authorized exact replacement failed to retain its postimage: %v/%v", err, closeErr)
	}
}

// Explicit writer ownership can transition previously witnessed absence to
// the exact bounded image; ordinary observation alone has no such authority.
func TestAttemptSettlementRuntimeV2RetainedExactWriteAbsent(t *testing.T) {
	physical, root, _ := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", nil)
	data := []byte("after\n")
	if err := physical.writeImage(root, "stats.json", data, uint64(len(data)), physical.writeSnapshot); err != nil {
		t.Fatal(err)
	}
	actual, exists, err := readAttemptSettlementV2File(root, "stats.json", uint64(len(data)), physical)
	closeErr := physical.closeRoots()
	if err != nil || closeErr != nil || !exists || !bytes.Equal(actual, data) {
		t.Fatalf("authorized exact creation failed to retain its postimage: %v/%v", err, closeErr)
	}
}

// Byte comparison must precede registry commitment. Returning nil after a
// real wrong-byte write cannot replace the retained pre-write witness.
func TestAttemptSettlementRuntimeV2RetainedWrongByteWrite(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", []byte("prior\n"))
	writes := 0
	write := func(root *attemptPrivateDirectory, name string, data []byte) error {
		writes++
		return writeAttemptSettlementV2OwnedState(root, name, []byte("other\n"))
	}
	err := physical.writeImage(root, "stats.json", []byte("after\n"), 6, write)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if err == nil || writes != 1 || retained != prior || closeErr == nil {
		t.Fatalf("wrong-byte writer readback replaced its retained preimage: %v/%v/%d", err, closeErr, writes)
	}
}

// A failed physical acknowledgement preserves the old witness even when the
// actual atomic write occurred. The original error and final mismatch survive.
func TestAttemptSettlementRuntimeV2RetainedFailedWrite(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", []byte("prior\n"))
	cause := errors.New("retained test actual-write acknowledgement refusal")
	writes := 0
	write := func(root *attemptPrivateDirectory, name string, data []byte) error {
		writes++
		return errors.Join(writeAttemptSettlementV2OwnedState(root, name, data), cause)
	}
	err := physical.writeImage(root, "stats.json", []byte("after\n"), 6, write)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if !errors.Is(err, cause) || writes != 1 || retained != prior || closeErr == nil {
		t.Fatalf("failed physical write lost its cause or retained preimage: %v/%v/%d", err, closeErr, writes)
	}
}

// Writer admission also guards absence: a new occupant arriving before the
// writer call cannot be overwritten under its earlier absent observation.
func TestAttemptSettlementRuntimeV2RetainedWriteAfterArrival(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", nil)
	path := filepath.Join(root.path, "stats.json")
	arrival := []byte("arrival\n")
	if err := os.WriteFile(path, arrival, 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	write := func(root *attemptPrivateDirectory, name string, data []byte) error {
		writes++
		return writeAttemptSettlementV2OwnedState(root, name, data)
	}
	err := physical.writeImage(root, "stats.json", []byte("after\n"), 6, write)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	actual, readErr := os.ReadFile(path)
	closeErr := physical.closeRoots()
	if err == nil || writes != 0 || retained != prior || readErr != nil || !bytes.Equal(actual, arrival) || closeErr == nil {
		t.Fatalf("writer overwrote an arrival after retained absence: %v/%v/%d", err, closeErr, writes)
	}
}

// Exact bytes written initially do not suffice when their real readback Close
// replaces the same target before local read custody has completed.
func TestAttemptSettlementRuntimeV2RetainedWriteReadbackClose(t *testing.T) {
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, "stats.json", []byte("prior\n"))
	path := filepath.Join(root.path, "stats.json")
	closes := 0
	physical.closeFile = func(file *os.File) error {
		if err := file.Close(); err != nil {
			return err
		}
		closes++
		runtimeAttemptSettlementV2ReplaceRetainedLeaf(t, path, []byte("after\n"), 0o600)
		return nil
	}
	err := physical.writeImage(root, "stats.json", []byte("after\n"), 6, physical.writeSnapshot)
	retained := physical.witnesses.roots[root.path].leaves["stats.json"]
	closeErr := physical.closeRoots()
	if err == nil || closes != 1 || retained != prior || closeErr == nil {
		t.Fatalf("unstable exact writer readback replaced its prior witness: %v/%v/%d", err, closeErr, closes)
	}
}

// Successful actual removal establishes absence, including its later exact
// idempotent removal and ordinary read within the same retained operation.
func TestAttemptSettlementRuntimeV2RetainedExactRemoval(t *testing.T) {
	name := "settlement-transaction-v2.json"
	physical, root, _ := runtimeAttemptSettlementV2RetainedLeaf(t, name, []byte("journal\n"))
	for range 2 {
		if err := physical.removeOwnedJournal(root, name); err != nil {
			t.Fatal(err)
		}
	}
	actual, exists, err := readAttemptSettlementV2File(root, name, 64, physical)
	closeErr := physical.closeRoots()
	if err != nil || closeErr != nil || exists || actual != nil {
		t.Fatalf("successful retained removal did not establish stable absence: %v/%v", err, closeErr)
	}
}

// An actual unlink followed by an acknowledgement error cannot erase the
// previously retained occupied state during failure cleanup.
func TestAttemptSettlementRuntimeV2RetainedFailedRemoval(t *testing.T) {
	name := "settlement-transaction-v2.json"
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, name, []byte("journal\n"))
	cause := errors.New("retained test actual-removal acknowledgement refusal")
	removals := 0
	physical.removeJournal = func(root *attemptPrivateDirectory, name string) error {
		removals++
		return errors.Join(removeAttemptSettlementV2OwnedTransaction(root, name), cause)
	}
	err := physical.removeOwnedJournal(root, name)
	retained := physical.witnesses.roots[root.path].leaves[name]
	closeErr := physical.closeRoots()
	if !errors.Is(err, cause) || removals != 1 || retained != prior || closeErr == nil {
		t.Fatalf("failed removal lost its cause or occupied witness: %v/%v/%d", err, closeErr, removals)
	}
}

// A nil-returning no-op hook cannot manufacture absence while the actual
// journal remains. The unchanged retained leaf still passes final custody.
func TestAttemptSettlementRuntimeV2RetainedNoopRemoval(t *testing.T) {
	name := "settlement-transaction-v2.json"
	physical, root, prior := runtimeAttemptSettlementV2RetainedLeaf(t, name, []byte("journal\n"))
	removals := 0
	physical.removeJournal = func(*attemptPrivateDirectory, string) error { removals++; return nil }
	err := physical.removeOwnedJournal(root, name)
	retained := physical.witnesses.roots[root.path].leaves[name]
	closeErr := physical.closeRoots()
	if err == nil || removals != 1 || retained != prior || closeErr != nil {
		t.Fatalf("no-op removal manufactured absence or lost its witness: %v/%v/%d", err, closeErr, removals)
	}
}

// The native immutable create transition advances absence exactly once;
// reopening and resyncing existing bytes must retain that same final inode.
func TestAttemptSettlementRuntimeV2RetainedImmutableCreateAndResync(t *testing.T) {
	coordinator := newAttemptSettlementRuntimeV2TestStateDir(t)
	physical := attemptSettlementV2PhysicalIO().withWitnesses(t.Context())
	t.Cleanup(func() { _ = physical.closeRoots() })
	data := []byte("immutable\n")
	if err := publishAttemptSettlementClosureV2(coordinator, 42, data, uint64(len(data)), physical); err != nil {
		t.Fatal(err)
	}
	path := AttemptSettlementClosureV2Path(coordinator, 42)
	prior := physical.witnesses.roots[filepath.Dir(path)].leaves[filepath.Base(path)]
	if err := publishAttemptSettlementClosureV2(coordinator, 42, data, uint64(len(data)), physical); err != nil {
		t.Fatal(err)
	}
	retained := physical.witnesses.roots[filepath.Dir(path)].leaves[filepath.Base(path)]
	closeErr := physical.closeRoots()
	if !prior.exists || retained != prior || closeErr != nil {
		t.Fatalf("immutable create or exact resync lost its committed inode: %v", closeErr)
	}
}

// The child sync occurs after the actual immutable rename. A different real
// image at that boundary must never replace the originally witnessed absence.
func TestAttemptSettlementRuntimeV2RetainedImmutableWrongReadback(t *testing.T) {
	coordinator := newAttemptSettlementRuntimeV2TestStateDir(t)
	physical := attemptSettlementV2PhysicalIO().withWitnesses(t.Context())
	t.Cleanup(func() { _ = physical.closeRoots() })
	path := AttemptSettlementClosureV2Path(coordinator, 42)
	mutations := 0
	physical.syncDirectory = func(file *os.File) error {
		if err := file.Sync(); err != nil {
			return err
		}
		if file.Name() == filepath.Dir(path) {
			mutations++
			runtimeAttemptSettlementV2ReplaceRetainedLeaf(t, path, []byte("different\n"), 0o400)
		}
		return nil
	}
	err := publishAttemptSettlementClosureV2(coordinator, 42, []byte("immutable\n"), 10, physical)
	retained := physical.witnesses.roots[filepath.Dir(path)].leaves[filepath.Base(path)]
	closeErr := physical.closeRoots()
	if err == nil || mutations != 1 || retained.exists || closeErr == nil {
		t.Fatalf("wrong immutable readback erased its retained absence: %v/%v/%d", err, closeErr, mutations)
	}
}

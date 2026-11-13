//go:build linux || darwin

package validator

// Real ordinary runtime produces every source journal. Filesystem controls
// then force exact acquisition transitions without replacing signed evidence,
// replay verdicts or actual I/O. FIFO flag controls never wait for a timeout.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Capture real immutable bytes through an independent ordinary file read.
func newReleaseMeasurementInputV2CustodyFixture(t *testing.T) (*releaseMeasurementInputV2TestFixture, string, []byte) {
	t.Helper()
	fixture := newReleaseMeasurementInputV2TestFixture(t)
	fixture.detach(t)
	path := releaseMeasurementInputV2Path(fixture.steerer.cfg.StateDir, 7, 9)
	encoded, err := os.ReadFile(path)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("actual ordinary journal is absent: %v", err)
	}
	return fixture, path, encoded
}

// Independent copies keep mutations separate while retaining the exact real
// journal bytes. A copied file is a custody contract, not a new signed trail.
func releaseMeasurementInputV2CustodyPath(t *testing.T, encoded []byte) string {
	t.Helper()
	path := filepath.Join(newAttemptLedgerDiskTestStateDir(t), "input.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Real detach and disk restart retain the signed input, full replay and exact
// cursor ownership; the new byte reader is not a replacement verifier.
func TestReleaseMeasurementInputV2CustodyGenuineRuntimeExactBytes(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyGenuineRuntimeExactBytes")
		}
	})
	fixture, path, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	encoded, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{})
	if err != nil || !bytes.Equal(encoded, expected) {
		t.Fatalf("exact genuine journal read differs: %v", err)
	}
	journal, err := decodeReleaseMeasurementInputV2(t.Context(), encoded, fixture.fresh(t))
	if err != nil {
		t.Fatal(err)
	}
	fixture.restart(t)
	result := fixture.detach(t)
	if !reflect.DeepEqual(result, journal.MeasurementInput) {
		t.Fatal("custody restart changed the genuinely signed input")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, expected) {
		t.Fatalf("custody restart replaced immutable bytes: %v", err)
	}
}

// A symlink to the very same preserved inode must fail at the actual native
// open, not be followed and rejected only after consuming the target bytes.
func TestReleaseMeasurementInputV2CustodyRejectsLeafSymlinkBeforeOpen(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyRejectsLeafSymlinkBeforeOpen")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	path := releaseMeasurementInputV2CustodyPath(t, expected)
	observed, opened := false, 0
	encoded, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
		if operation == "leaf-opened" {
			opened++
		}
		if operation != "leaf-observed" {
			return nil
		}
		observed = true
		if err := os.Rename(path, path+"-preserved"); err != nil {
			return err
		}
		return os.Symlink(filepath.Base(path)+"-preserved", path)
	}})
	if !observed || opened != 0 || !errors.Is(err, unix.ELOOP) || encoded != nil {
		t.Fatalf("same-inode alias was followed: observed=%t opened=%d error=%v", observed, opened, err)
	}
	after, readErr := os.ReadFile(path + "-preserved")
	if readErr != nil || !bytes.Equal(after, expected) {
		t.Fatalf("symlink control changed preserved bytes: %v", readErr)
	}
}

// The causal before-open flag contract refuses to enter the known blocking
// old syscall. The repair actually opens the real FIFO without a peer writer,
// observes its native O_NONBLOCK descriptor, and rejects its non-regular type.
// This is not a claimed observed old hang or a timeout-based negative proof.
func TestReleaseMeasurementInputV2CustodyNonblockingFIFOAcquisition(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyNonblockingFIFOAcquisition")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	path := releaseMeasurementInputV2CustodyPath(t, expected)
	refusedBlocking := errors.New("test refuses the actual old blocking-open flag contract")
	observed, opened, flagsObserved := false, 0, 0
	encoded, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(operation string, file *os.File, flags int) error {
		switch operation {
		case "leaf-observed":
			observed = true
			if err := os.Rename(path, path+"-preserved"); err != nil {
				return err
			}
			return unix.Mkfifo(path, 0o600)
		case "leaf-opening":
			flagsObserved = flags
			if flags&unix.O_NONBLOCK == 0 || flags&unix.O_NOFOLLOW == 0 {
				return refusedBlocking
			}
		case "leaf-opened":
			opened++
			actual, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
			if err != nil {
				return err
			}
			if actual&unix.O_NONBLOCK == 0 {
				return errors.New("actual FIFO descriptor lost nonblocking mode")
			}
		}
		return nil
	}})
	if !observed || flagsObserved&unix.O_NONBLOCK == 0 || flagsObserved&unix.O_NOFOLLOW == 0 || errors.Is(err, refusedBlocking) || err == nil || opened != 1 || encoded != nil {
		t.Fatalf("FIFO custody did not reach a safe native type refusal: observed=%t opened=%d flags=%d error=%v", observed, opened, flagsObserved, err)
	}
}

// A parent alias back to the same real inode escapes a leaf-only SameFile
// check. Reacquiring the exact directory namespace must reject that alias.
func TestReleaseMeasurementInputV2CustodyRejectsParentAliasAfterObservation(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyRejectsParentAliasAfterObservation")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	path := releaseMeasurementInputV2CustodyPath(t, expected)
	parent := filepath.Dir(path)
	observed := false
	encoded, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
		if operation != "leaf-observed" {
			return nil
		}
		observed = true
		if err := os.Rename(parent, parent+"-preserved"); err != nil {
			return err
		}
		return os.Symlink(filepath.Base(parent)+"-preserved", parent)
	}})
	if !observed || err == nil || encoded != nil {
		t.Fatalf("same-inode parent alias retained read authority: observed=%t error=%v", observed, err)
	}
	after, readErr := os.ReadFile(filepath.Join(parent+"-preserved", filepath.Base(path)))
	if readErr != nil || !bytes.Equal(after, expected) {
		t.Fatalf("parent alias lost genuine journal bytes: %v", readErr)
	}
}

// Actual FIFO and new-directory replacements remain fail-closed. Directory
// acquisition itself must not add a new blocking FIFO path while fixing leaf I/O.
func TestReleaseMeasurementInputV2CustodyRejectsParentFIFOAndReplacement(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyRejectsParentFIFOAndReplacement")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	for _, fifo := range []bool{false, true} {
		path := releaseMeasurementInputV2CustodyPath(t, expected)
		parent := filepath.Dir(path)
		observed := false
		encoded, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
			if operation != "leaf-observed" {
				return nil
			}
			observed = true
			if err := os.Rename(parent, parent+"-preserved"); err != nil {
				return err
			}
			if fifo {
				return unix.Mkfifo(parent, 0o600)
			}
			if err := os.Mkdir(parent, 0o700); err != nil {
				return err
			}
			return os.WriteFile(path, expected, 0o600)
		}})
		if !observed || err == nil || encoded != nil {
			t.Fatalf("parent replacement was accepted: fifo=%t observed=%t error=%v", fifo, observed, err)
		}
	}
}

// Same-size rewrites and newly shared modes are real mutations after the read.
// An explicit old timestamp makes the write-state witness clock-independent.
func TestReleaseMeasurementInputV2CustodyRejectsSameInodeWriteStateChanges(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyRejectsSameInodeWriteStateChanges")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	for _, shared := range []bool{false, true} {
		path := releaseMeasurementInputV2CustodyPath(t, expected)
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		observed := false
		encoded, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
			if operation != "leaf-read" {
				return nil
			}
			observed = true
			if shared {
				return os.Chmod(path, 0o640)
			}
			changed := bytes.Clone(expected)
			changed[len(changed)/2] ^= 1
			if err := os.WriteFile(path, changed, 0o600); err != nil {
				return err
			}
			return os.Chtimes(path, time.Unix(1, 0), time.Unix(1, 0))
		}})
		after, statErr := os.Lstat(path)
		if !observed || err == nil || encoded != nil || statErr != nil || !os.SameFile(before, after) || before.Size() != after.Size() {
			t.Fatalf("same-inode changed state was accepted: shared=%t error=%v/%v", shared, err, statErr)
		}
	}
}

// Real late ENOENT is retained, but cannot become initial creation authority.
func TestReleaseMeasurementInputV2CustodyLateDisappearanceIsNotInitialAbsence(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyLateDisappearanceIsNotInitialAbsence")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	path := releaseMeasurementInputV2CustodyPath(t, expected)
	observed := false
	encoded, err := readReleaseMeasurementInputV2Context(t.Context(), path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
		if operation != "leaf-observed" {
			return nil
		}
		observed = true
		return os.Rename(path, path+"-preserved")
	}})
	if !observed || !errors.Is(err, os.ErrNotExist) || errors.Is(err, errReleaseMeasurementInputV2InitiallyMissing) || encoded != nil {
		t.Fatalf("late disappearance became creation authority: observed=%t error=%v", observed, err)
	}
}

// The actual immutable writer must not recreate a name lost after observing
// a committed journal. No read callback replaces Link, Sync or cryptography.
func TestReleaseMeasurementInputV2CustodyWriterDoesNotRepublishAfterDisappearance(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyWriterDoesNotRepublishAfterDisappearance")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	path := releaseMeasurementInputV2CustodyPath(t, expected)
	observed := false
	err := writeReleaseMeasurementInputV2WithReadHooks(path, expected, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
		if operation != "leaf-observed" || observed {
			return nil
		}
		observed = true
		return os.Rename(path, path+"-preserved")
	}})
	_, statErr := os.Lstat(path)
	if !observed || !errors.Is(err, os.ErrNotExist) || errors.Is(err, errReleaseMeasurementInputV2InitiallyMissing) || !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("immutable writer republished after late disappearance: observed=%t error=%v stat=%v", observed, err, statErr)
	}
	after, readErr := os.ReadFile(path + "-preserved")
	if readErr != nil || !bytes.Equal(after, expected) {
		t.Fatalf("writer lost preserved immutable bytes: %v", readErr)
	}
}

// A genuine M8 Stats owner cannot rotate another generation merely because
// its already-published same-epoch journal disappears during acquisition.
func TestReleaseMeasurementInputV2CustodyRuntimeDoesNotRotateAfterDisappearance(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyRuntimeDoesNotRotateAfterDisappearance")
		}
	})
	fixture, path, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	fixture.runtime.stats.mu.Lock()
	generation := fixture.runtime.stats.egressGeneration
	fixture.runtime.stats.mu.Unlock()
	observed := false
	result, err := fixture.steerer.loadOrDetachReleaseMeasurementInputV2WithReadHooks(t.Context(), 9, 7, 200, fixture.nativeHash, fixture.snapshot, fixture.fresh(t), releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
		if operation != "leaf-observed" || observed {
			return nil
		}
		observed = true
		return os.Rename(path, path+"-preserved")
	}})
	fixture.runtime.stats.mu.Lock()
	afterGeneration := fixture.runtime.stats.egressGeneration
	fixture.runtime.stats.mu.Unlock()
	if !observed || !errors.Is(err, os.ErrNotExist) || errors.Is(err, errReleaseMeasurementInputV2InitiallyMissing) || !reflect.DeepEqual(result, ReleaseMeasurementInput{}) || generation != afterGeneration {
		t.Fatalf("late disappearance rotated the real Stats owner: observed=%t generation=%d/%d error=%v", observed, generation, afterGeneration, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime recreated the raced-away journal: %v", err)
	}
	after, readErr := os.ReadFile(path + "-preserved")
	if readErr != nil || !bytes.Equal(after, expected) {
		t.Fatalf("runtime lost preserved genuine input: %v", readErr)
	}
}

// Cancellation is forced before admission and at actual read/check boundaries;
// no delay or scheduler race supplies the evidence.
func TestReleaseMeasurementInputV2CustodyCanceledAdmissionAndLateCancellation(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyCanceledAdmissionAndLateCancellation")
		}
	})
	_, path, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	for _, stage := range []string{"before", "path-admitted", "leaf-observed", "leaf-opening", "leaf-opened", "leaf-read", "leaf-checked"} {
		ctx, cancel := context.WithCancel(t.Context())
		if stage == "before" {
			cancel()
		}
		observed, closed := 0, 0
		fired := stage == "before"
		encoded, err := readReleaseMeasurementInputV2Context(ctx, path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
			observed++
			if operation == stage {
				fired = true
				cancel()
			}
			return nil
		}, afterClose: func(file *os.File) error {
			closed++
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("observer received an open descriptor")
			}
			return nil
		}})
		cancel()
		expectedClosed := 2
		if stage == "leaf-observed" || stage == "leaf-opening" {
			expectedClosed = 1
		}
		if stage == "before" || stage == "path-admitted" {
			expectedClosed = 0
		}
		if !fired || !errors.Is(err, context.Canceled) || encoded != nil || closed != expectedClosed || stage == "before" && observed != 0 {
			t.Fatalf("canceled %s custody reached output or lost descriptor ownership: fired=%t observed=%d closed=%d/%d error=%v", stage, fired, observed, closed, expectedClosed, err)
		}
	}
	observed := 0
	encoded, err := readReleaseMeasurementInputV2Context(nil, path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(string, *os.File, int) error { observed++; return nil }})
	if err == nil || encoded != nil || observed != 0 {
		t.Fatalf("nil context acquired input custody: observed=%d error=%v", observed, err)
	}
}

// A genuine file Close precedes each injected error. The occupied first case
// witnesses the old lost-cancellation bug; the missing-leaf case checks that
// new directory ownership cannot leak creation authority after a close error.
func TestReleaseMeasurementInputV2CustodyRetainsLateCloseAndCanceledAbsence(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyRetainsLateCloseAndCanceledAbsence")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	for _, missing := range []bool{false, true} {
		path := releaseMeasurementInputV2CustodyPath(t, expected)
		if missing {
			if err := os.Rename(path, path+"-preserved"); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		failure := errors.New("test-owned injected post-close custody failure")
		closed := 0
		encoded, err := readReleaseMeasurementInputV2Context(ctx, path, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
			closed++
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("actual Close did not precede its observer")
			}
			cancel()
			return failure
		}})
		cancel()
		expectedClosed := 2
		if missing {
			expectedClosed = 1
		}
		if closed != expectedClosed || !errors.Is(err, failure) || !errors.Is(err, context.Canceled) || errors.Is(err, errReleaseMeasurementInputV2InitiallyMissing) || encoded != nil || missing && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("late close/cancellation lost authority or cause: missing=%t closed=%d/%d error=%v", missing, closed, expectedClosed, err)
		}
	}
}

// Existing private owner-bit modes and hardlink observations retain their old
// grammar. A stable transient publication alias is not silently reclassified
// under the terminal reader's distinct single-link policy.
func TestReleaseMeasurementInputV2CustodyPreservesModesLinksAndExactBounds(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyPreservesModesLinksAndExactBounds")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	for _, mode := range []os.FileMode{0o400, 0o600, 0o700} {
		path := releaseMeasurementInputV2CustodyPath(t, expected)
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, path+"-alias"); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, exact := range []bool{true, false} {
			bound := uint64(len(expected))
			if !exact {
				bound--
			}
			encoded, err := readReleaseMeasurementInputV2Context(t.Context(), path, bound, releaseMeasurementInputV2ReadHooks{})
			if exact && (err != nil || !bytes.Equal(encoded, expected)) || !exact && (err == nil || encoded != nil) {
				t.Fatalf("private mode/link/exact bound semantics changed: mode=%o exact=%t error=%v", mode, exact, err)
			}
		}
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Fatalf("reader repaired an existing inode or mode: %v", err)
		}
	}
}

// The new v2 boundary requires physical ancestry, unlike the old lexical
// path check. Resolve only test provisioning before authority is supplied;
// candidate admission itself must never normalize or follow an alias.
func TestReleaseMeasurementInputV2CustodyPhysicalNamespaceAdmission(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ORDINARY-JOURNAL-CUSTODY-v1 PASS TestReleaseMeasurementInputV2CustodyPhysicalNamespaceAdmission")
		}
	})
	_, _, expected := newReleaseMeasurementInputV2CustodyFixture(t)
	provisioned := releaseMeasurementInputV2CustodyPath(t, expected)
	parent, err := filepath.EvalSymlinks(filepath.Dir(provisioned))
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(parent, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	physical := filepath.Join(nested, filepath.Base(provisioned))
	if err := os.Rename(provisioned, physical); err != nil {
		t.Fatal(err)
	}
	encoded, err := readReleaseMeasurementInputV2Context(t.Context(), physical, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{})
	if err != nil || !bytes.Equal(encoded, expected) {
		t.Fatalf("physical namespace refused genuine bytes: %v", err)
	}
	aliasParent := parent + "-alias"
	if err := os.Symlink(filepath.Base(parent), aliasParent); err != nil {
		t.Fatal(err)
	}
	aliasNested := nested + "-alias"
	if err := os.Symlink(filepath.Base(nested), aliasNested); err != nil {
		t.Fatal(err)
	}
	for _, aliasDirectory := range []string{aliasNested, filepath.Join(aliasParent, "nested")} {
		alias := filepath.Join(aliasDirectory, filepath.Base(physical))
		opened := 0
		encoded, err = readReleaseMeasurementInputV2Context(t.Context(), alias, uint64(len(expected)), releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
			if operation == "leaf-opened" {
				opened++
			}
			return nil
		}})
		if err == nil || !strings.Contains(err.Error(), "physical ancestor") || encoded != nil || opened != 0 {
			t.Fatalf("initial ancestor alias retained candidate authority: directory=%s opened=%d error=%v", aliasDirectory, opened, err)
		}
	}
	after, readErr := os.ReadFile(physical)
	if readErr != nil || !bytes.Equal(after, expected) {
		t.Fatalf("alias admission changed physical journal: %v", readErr)
	}
}

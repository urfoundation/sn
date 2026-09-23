//go:build linux || darwin

package validator

// Nil-returning callbacks run after the actual directory Close. A final
// operation-wide witness must refuse their late namespace/leaf changes before
// returning a closure, opening admission or attaching fresh startup engines.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Only a newly provisioned test namespace is moved. Its full original bytes
// remain recoverable and are restored before the fixture's ledger cleanup.
func runtimeAttemptSettlementV2RetargetDirectory(t *testing.T, path string) func() {
	t.Helper()
	preserved := path + ".final-witness-preserved"
	if err := os.Rename(path, preserved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	restored := false
	return func() {
		if restored {
			return
		}
		restored = true
		if err := os.Remove(path); err != nil {
			t.Error(err)
			return
		}
		if err := os.Rename(preserved, path); err != nil {
			t.Error(err)
		}
	}
}

// Close the full real M8 batch without changing its original sealer/replay
// limits. This helper returns only an actually accepted immutable closure.
func runtimeAttemptSettlementV2CloseForWitness(t *testing.T, fixture *attemptSettlementRuntimeV2TestFixture) *AttemptSettlementClosureV2 {
	t.Helper()
	fixture.trails(t, 0, 2, 1)
	fixture.trails(t, 1, 1, 0)
	closure, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil || closure == nil {
		t.Fatalf("actual signed witness closure prerequisite: %v", err)
	}
	return closure
}

// The last close retargets a different already-closed root. Per-root checks
// performed immediately after each close cannot establish this postcondition.
func TestAttemptSettlementRuntimeV2FinalWitnessInitializationCrossRoot(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	var earlier string
	var restore func()
	defer func() {
		if restore != nil {
			restore()
		}
	}()
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		path := root.path
		err := root.close()
		closes++
		if closes == 1 {
			earlier = path
		}
		if closes == 3 {
			if earlier == "" || earlier == path {
				t.Fatal("cross-root close prerequisite differs")
			}
			restore = runtimeAttemptSettlementV2RetargetDirectory(t, earlier)
		}
		return err
	}
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if restore != nil {
		restore()
	}
	if err == nil || closes != 3 || !strings.Contains(err.Error(), "compact final directory witness") {
		t.Fatalf("last initialization close escaped complete-registry witness: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 42)
}

// The immutable child was already closed before coordinator/operator cleanup.
// The final root callback can still replace it without changing either parent.
func TestAttemptSettlementRuntimeV2FinalWitnessAdvanceClosureChild(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	fixture.trails(t, 1, 1, 0)
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	var child string
	var restore func()
	defer func() {
		if restore != nil {
			restore()
		}
	}()
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		path := root.path
		err := root.close()
		closes++
		if filepath.Base(path) == "settlement-closures-v2" {
			child = path
		}
		if closes == 4 {
			if child == "" || child == path {
				t.Fatal("closed child prerequisite missing")
			}
			restore = runtimeAttemptSettlementV2RetargetDirectory(t, child)
		}
		return err
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if restore != nil {
		restore()
	}
	if err == nil || closure != nil || closes != 4 || !strings.Contains(err.Error(), "compact final directory witness") {
		t.Fatalf("last advance close lost immutable-child witness: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 43)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("final witness retry refolded actual completed state")
	}
}

// Current retry closes the same child through read and immutable resync. The
// final pass must also retain earlier operator identities across both uses.
func TestAttemptSettlementRuntimeV2FinalWitnessCurrentRetryCrossRoot(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	_ = runtimeAttemptSettlementV2CloseForWitness(t, fixture)
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	var earlier string
	var restore func()
	defer func() {
		if restore != nil {
			restore()
		}
	}()
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		path := root.path
		err := root.close()
		closes++
		if earlier == "" && filepath.Base(path) != "settlement-closures-v2" {
			earlier = path
		}
		if closes == 5 {
			if earlier == "" || earlier == path {
				t.Fatal("current retry cross-root prerequisite differs")
			}
			restore = runtimeAttemptSettlementV2RetargetDirectory(t, earlier)
		}
		return err
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if restore != nil {
		restore()
	}
	if err == nil || closure != nil || closes != 5 || !strings.Contains(err.Error(), "compact final directory witness") {
		t.Fatalf("last current retry close escaped cross-root witness: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 43)
}

// Actual disk restart remains fresh and unattached after the last Close
// retargets an earlier operator, despite every snapshot write having finished.
func TestAttemptSettlementRuntimeV2FinalWitnessRecoveryBeforeAttachment(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	restarted := fixture.reopen(t)
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	var earlier string
	var restore func()
	defer func() {
		if restore != nil {
			restore()
		}
	}()
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		path := root.path
		err := root.close()
		closes++
		if closes == 1 {
			earlier = path
		}
		if closes == 3 {
			if earlier == path {
				t.Fatal("fresh recovery cross-root prerequisite differs")
			}
			restore = runtimeAttemptSettlementV2RetargetDirectory(t, earlier)
		}
		return err
	}
	err := recoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if restore != nil {
		restore()
	}
	if err == nil || closes != 3 || !strings.Contains(err.Error(), "compact final directory witness") {
		t.Fatalf("last recovery close escaped before attachment: %v/%d", err, closes)
	}
	for _, participant := range restarted {
		if participant.Stats.attemptLedger != nil || participant.Stats.attemptV2 != nil {
			t.Fatal("late recovery close published a partial fresh owner")
		}
	}
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
}

// Public full replay has completed before the child's final close retargets
// its already-closed coordinator. No accepted closure or verifier result leaks.
func TestAttemptSettlementRuntimeV2FinalWitnessPublicReadClearsReplayResult(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	_ = runtimeAttemptSettlementV2CloseForWitness(t, fixture)
	options := fixture.options(t).Authority
	proofCloses := 0
	for noID, operator := range options.Operators {
		operator.Measurement.Replay.OpenData = runtimeAttemptSettlementV2TestLateClose(operator.Measurement.Replay.OpenData, nil, &proofCloses)
		options.Operators[noID] = operator
	}
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	var restore func()
	defer func() {
		if restore != nil {
			restore()
		}
	}()
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		path := root.path
		err := root.close()
		closes++
		if closes == 2 {
			if filepath.Base(path) != "settlement-closures-v2" || proofCloses == 0 {
				t.Fatal("public read did not actually replay before final child close")
			}
			restore = runtimeAttemptSettlementV2RetargetDirectory(t, fixture.coordinator)
		}
		return err
	}
	closure, verified, err := readAttemptSettlementClosureV2(t.Context(), fixture.coordinator, 42, options, physical)
	if restore != nil {
		restore()
	}
	if err == nil || closure != nil || !reflect.DeepEqual(verified, VerifiedAttemptSettlementV2{}) || closes != 2 || proofCloses == 0 {
		t.Fatalf("public replay result escaped last-close retarget: %v/%d/%d", err, closes, proofCloses)
	}
}

// Replacing a snapshot with byte-identical private contents changes its inode
// and cannot be blessed by recapturing metadata after the final close callback.
func TestAttemptSettlementRuntimeV2FinalWitnessLateSameBytesLeafReplacement(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	path := filepath.Join(fixture.participants[0].StateDir, "stats.json")
	var actual []byte
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		err := root.close()
		closes++
		if closes == 4 {
			var readErr error
			actual, readErr = os.ReadFile(path)
			if readErr != nil {
				return errors.Join(err, readErr)
			}
			if writeErr := os.WriteFile(path+".same-bytes-replacement", actual, 0o600); writeErr != nil {
				return errors.Join(err, writeErr)
			}
			return errors.Join(err, os.Rename(path+".same-bytes-replacement", path))
		}
		return err
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err == nil || closure != nil || closes != 4 || !strings.Contains(err.Error(), "compact final leaf witness") {
		t.Fatalf("same-byte late leaf replacement escaped witness: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 43)
	persisted, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(actual, persisted) {
		t.Fatalf("replacement prerequisite changed actual bytes: %v", readErr)
	}
}

// The final callback moves the actual immutable file, retaining every original
// byte. A late missing occupied leaf is not an accepted absence result.
func TestAttemptSettlementRuntimeV2FinalWitnessLateClosureLeafAbsence(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	path := AttemptSettlementClosureV2Path(fixture.coordinator, 42)
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		err := root.close()
		closes++
		if closes == 4 {
			return errors.Join(err, os.Rename(path, path+".preserved"))
		}
		return err
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err == nil || !errors.Is(err, os.ErrNotExist) || closure != nil || closes != 4 || !strings.Contains(err.Error(), "compact final leaf witness") {
		t.Fatalf("late immutable leaf absence escaped witness: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 43)
	if err := os.Rename(path+".preserved", path); err != nil {
		t.Fatal(err)
	}
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
}

// Recreating the exact genuine transaction after its removal violates the
// final expected absence even though every recreated byte was once authentic.
func TestAttemptSettlementRuntimeV2FinalWitnessJournalAbsenceRecreated(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 2, 1)
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	var journal []byte
	physical.writeJournal = func(root *attemptPrivateDirectory, name string, data []byte) error {
		journal = append([]byte(nil), data...)
		return writeAttemptSettlementV2OwnedState(root, name, data)
	}
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		err := root.close()
		closes++
		if closes == 4 {
			if len(journal) == 0 {
				t.Fatal("actual journal recreation prerequisite missing")
			}
			return errors.Join(err, os.WriteFile(attemptSettlementTransactionV2Path(fixture.coordinator), journal, 0o600))
		}
		return err
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t), physical, true)
	if err == nil || closure != nil || closes != 4 || !strings.Contains(err.Error(), "compact final leaf witness") {
		t.Fatalf("late recreated journal escaped absence witness: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 43)
	transaction := fixture.transaction(t)
	if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	assertAttemptSettlementRuntimeV2Postimages(t, fixture.coordinator, fixture.participants, transaction)
}

// A callback can mutate the object it receives after Close. The operation's
// copied original path/anchor, not that mutable object, owns the final witness.
func TestAttemptSettlementRuntimeV2FinalWitnessAnchorIsOwnedCopy(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	var restore func()
	defer func() {
		if restore != nil {
			restore()
		}
	}()
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		path := root.path
		err := root.close()
		closes++
		if closes == 3 {
			restore = runtimeAttemptSettlementV2RetargetDirectory(t, path)
			replacement, openErr := openAttemptPrivateDirectory(path)
			if openErr != nil {
				return errors.Join(err, openErr)
			}
			root.anchor = replacement.anchor
			root.path = path + ".final-witness-preserved"
			return errors.Join(err, replacement.close())
		}
		return err
	}
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if restore != nil {
		restore()
	}
	if err == nil || closes != 3 || !strings.Contains(err.Error(), "compact final directory witness") {
		t.Fatalf("callback-mutated anchor replaced owned final witness: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 42)
}

// Explicit error-contract causes are added only after successful real closes;
// all closes, both causes, cancellation and the independent retarget remain.
func TestAttemptSettlementRuntimeV2FinalWitnessJoinsAllLateCauses(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	firstCause, lastCause := errors.New("first real-close acknowledgement refusal"), errors.New("last real-close acknowledgement refusal")
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	var earlier string
	var restore func()
	defer func() {
		if restore != nil {
			restore()
		}
	}()
	physical.closeRoot = func(root *attemptPrivateDirectory) error {
		path := root.path
		err := root.close()
		closes++
		if closes == 1 {
			earlier = path
			return errors.Join(err, firstCause)
		}
		if closes == 3 {
			restore = runtimeAttemptSettlementV2RetargetDirectory(t, earlier)
			cancel()
			return errors.Join(err, lastCause)
		}
		return err
	}
	err := initializeAttemptSettlementEpochV2(ctx, fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if restore != nil {
		restore()
	}
	if !errors.Is(err, firstCause) || !errors.Is(err, lastCause) || !errors.Is(err, context.Canceled) || closes != 3 || !strings.Contains(err.Error(), "compact final directory witness") {
		t.Fatalf("final witness lost a close/cancellation/namespace cause: %v/%d", err, closes)
	}
	fixture.assertReserved(t, 42)
}

// Unhooked witnesses do not invoke the observable closer again. Real complete
// publication, readback and fresh restart remain supported under exact limits.
func TestAttemptSettlementRuntimeV2FinalWitnessStableFullLifecycle(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	physical, closes := attemptSettlementV2PhysicalIO(), 0
	physical.closeRoot = func(root *attemptPrivateDirectory) error { closes++; return root.close() }
	if err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); err != nil || closes != 3 {
		t.Fatalf("stable initialization witness changed close ownership: %v/%d", err, closes)
	}
	closure := runtimeAttemptSettlementV2CloseForWitness(t, fixture)
	read, _, err := ReadAttemptSettlementClosureV2(t.Context(), fixture.coordinator, 42, fixture.options(t).Authority)
	if err != nil || !reflect.DeepEqual(read, closure) {
		t.Fatalf("stable full closure read differs: %v", err)
	}
	restarted := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, restarted, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	for _, participant := range restarted {
		if participant.Stats.attemptLedger != participant.Ledger || participant.Stats.attemptCutPending {
			t.Fatal("stable final witness failed coherent fresh attachment")
		}
	}
}

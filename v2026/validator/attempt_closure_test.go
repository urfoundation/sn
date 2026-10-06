package validator

// Forces journal/export/restart order with production transaction hooks.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Journal removal must observe the complete durable batch and closed admission.
func TestAttemptSettlementClosurePrecedesJournalRemoval(t *testing.T) {
	root := t.TempDir()
	first, _ := newAttemptSettlementTestParticipant(t, 1)
	second, _ := newAttemptSettlementTestParticipant(t, 2)
	participants := []AttemptSettlementParticipant{first, second}
	stop := errors.New("hold journal removal")
	var published []byte
	err := advanceAttemptSettlementEpochWithIO(root, 43, attemptLedgerTestBoundary(), participants, func(path string, data []byte) error { return atomicStateWrite(path, data, 0o600) }, func(string) error {
		var err error
		published, err = ReadAttemptSettlementClosure(root, 42)
		if err != nil {
			t.Fatal(err)
		}
		closure, err := DecodeAttemptSettlementClosure(published)
		if err != nil || len(closure.Transitions) != 2 {
			t.Fatalf("closed batch: %v", err)
		}
		for _, participant := range participants {
			if !participant.Stats.attemptCutPending || participant.Stats.settlementEpoch != 43 {
				t.Fatal("admission reopened before journal removal")
			}
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("journal boundary: %v", err)
	}
	if err := AdvanceAttemptSettlementEpoch(root, 43, AttemptBoundary{}, participants); err != nil {
		t.Fatal(err)
	}
	again, err := ReadAttemptSettlementClosure(root, 42)
	if err != nil || !bytes.Equal(published, again) {
		t.Fatalf("retry changed signed export: %v", err)
	}
	if _, err := os.Stat(attemptSettlementTransactionPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal not removed: %v", err)
	}
}

// A crash between operator snapshots is completed before any producer reload.
func TestAttemptSettlementClosureRecoveryPublishesExactBatch(t *testing.T) {
	root := t.TempDir()
	first, _ := newAttemptSettlementTestParticipant(t, 1)
	second, _ := newAttemptSettlementTestParticipant(t, 2)
	participants := []AttemptSettlementParticipant{first, second}
	writes := 0
	stop := errors.New("second operator interrupted")
	err := advanceAttemptSettlementEpochWithWrite(root, 43, attemptLedgerTestBoundary(), participants, func(path string, data []byte) error {
		writes++
		if writes == 2 {
			return stop
		}
		return atomicStateWrite(path, data, 0o600)
	})
	if !errors.Is(err, stop) {
		t.Fatalf("partial write: %v", err)
	}
	if _, err := ReadAttemptSettlementClosure(root, 42); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial transaction exported early: %v", err)
	}
	recovery := []AttemptSettlementParticipant{{NoID: 1, StateDir: first.StateDir}, {NoID: 2, StateDir: second.StateDir}}
	var published []byte
	err = recoverAttemptSettlementEpochWithRemove(root, recovery, func(string) error {
		var err error
		published, err = ReadAttemptSettlementClosure(root, 42)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeAttemptSettlementClosure(published); err != nil {
			t.Fatal(err)
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("recovery removal: %v", err)
	}
	if err := RecoverAttemptSettlementEpoch(root, recovery); err != nil {
		t.Fatal(err)
	}
	again, err := ReadAttemptSettlementClosure(root, 42)
	if err != nil || !bytes.Equal(published, again) {
		t.Fatalf("recovered export changed: %v", err)
	}
}

// Existing immutable bytes and both leaf and directory aliases fail closed.
func TestAttemptSettlementClosureRejectsChangedBytesAndAliases(t *testing.T) {
	for _, kind := range []string{"bytes", "leaf-symlink", "directory-symlink", "hardlink"} {
		root := t.TempDir()
		participant, _ := newAttemptSettlementTestParticipant(t, 1)
		participants := []AttemptSettlementParticipant{participant}
		if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), participants); err != nil {
			t.Fatal(err)
		}
		path := AttemptSettlementClosurePath(root, 42)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "bytes":
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, 'x'), 0o600); err != nil {
				t.Fatal(err)
			}
		case "leaf-symlink":
			other := filepath.Join(t.TempDir(), "closure.json")
			if err := os.WriteFile(other, data, 0o400); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other, path); err != nil {
				t.Fatal(err)
			}
		case "directory-symlink":
			directory := filepath.Dir(path)
			other := filepath.Join(t.TempDir(), "closures")
			if err := os.Rename(directory, other); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other, directory); err != nil {
				t.Fatal(err)
			}
		case "hardlink":
			if err := os.Link(path, filepath.Join(t.TempDir(), "alias")); err != nil {
				t.Fatal(err)
			}
		}
		if err := AdvanceAttemptSettlementEpoch(root, 43, AttemptBoundary{}, participants); err == nil {
			t.Fatalf("%s accepted conflicting or aliased closure", kind)
		}
		if !participant.Stats.attemptCutPending {
			t.Fatalf("%s reopened admission after export failure", kind)
		}
	}
}

// Canonical framing and complete signed participants remain independently checked.
func TestAttemptSettlementClosureRejectsOmittedParticipantAndSuffix(t *testing.T) {
	root := t.TempDir()
	first, _ := newAttemptSettlementTestParticipant(t, 1)
	second, _ := newAttemptSettlementTestParticipant(t, 2)
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{first, second}); err != nil {
		t.Fatal(err)
	}
	data, err := ReadAttemptSettlementClosure(root, 42)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"{}", "null", "junk"} {
		if _, err := DecodeAttemptSettlementClosure(append(append([]byte(nil), data...), suffix...)); err == nil {
			t.Fatalf("accepted suffix %s", suffix)
		}
	}
	closure, err := DecodeAttemptSettlementClosure(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttemptSettlementBatch(closure.Transitions[:1]); err == nil {
		t.Fatal("omitted operator batch verified")
	}
}

// The primitive's same-epoch native retry cannot reopen an ordinary cut gate.
func TestAttemptSettlementSameEpochPreservesOrdinaryCutOwnership(t *testing.T) {
	participant, ledger := newAttemptSettlementTestParticipant(t, 1)
	if err := participant.Stats.beginAttempt(42, ledger); err != nil {
		t.Fatal(err)
	}
	if _, err := participant.Stats.detachReleaseStatsMeasurementWithAttemptCut(participant.StateDir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { t.Error("active cut persisted"); return nil }); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("ordinary barrier: %v", err)
	}
	if err := AdvanceAttemptSettlementEpoch(t.TempDir(), 42, AttemptBoundary{}, []AttemptSettlementParticipant{participant}); err != nil {
		t.Fatal(err)
	}
	if err := participant.Stats.beginAttempt(42, ledger); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("same epoch reopened ordinary admission: %v", err)
	}
	participant.Stats.abortAttempt()
	if _, err := participant.Stats.detachReleaseStatsMeasurementWithAttemptCut(participant.StateDir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if participant.Stats.attemptCutPending {
		t.Fatal("ordinary owner could not release its completed cut")
	}
}

// Old native folds and cut journals cannot clear a pending next-epoch transition.
func TestAttemptSettlementPendingTransitionSurvivesOldNativeCut(t *testing.T) {
	root := t.TempDir()
	participant, ledger := newAttemptSettlementTestParticipant(t, 1)
	participants := []AttemptSettlementParticipant{participant}
	if err := participant.Stats.beginAttempt(42, ledger); err != nil {
		t.Fatal(err)
	}
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), participants); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("settlement barrier: %v", err)
	}
	if err := AdvanceAttemptSettlementEpoch(root, 42, AttemptBoundary{}, participants); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("old native fold cleared new transition: %v", err)
	}
	if err := participant.Stats.reconcileReleaseStatsCut(participant.StateDir, 0); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("old cut reconciliation cleared transition: %v", err)
	}
	if !participant.Stats.attemptCutPending || !participant.Stats.attemptSettlementCutPending {
		t.Fatal("pending transition lost ownership")
	}
	participant.Stats.abortAttempt()
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), participants); err != nil {
		t.Fatal(err)
	}
	if _, err := participant.Stats.detachReleaseStatsMeasurementWithAttemptCut(participant.StateDir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { t.Error("stale cut persisted"); return nil }); err == nil {
		t.Fatal("stale ordinary cut accepted")
	}
	if participant.Stats.attemptCutPending || participant.Stats.attemptSettlementCutPending {
		t.Fatal("stale cut changed new epoch admission")
	}
}

// A first mkdir's parent entry is synced before its immutable child can appear.
func TestAttemptSettlementClosureSyncsParentBeforePublication(t *testing.T) {
	root := t.TempDir()
	participant, _ := newAttemptSettlementTestParticipant(t, 1)
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{participant}); err != nil {
		t.Fatal(err)
	}
	statsJSON, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	transaction := &attemptSettlementTransaction{Schema: attemptSettlementTransactionSchema, Epoch: 43, Snapshots: []attemptSettlementSnapshot{{NoID: 1, StatsPath: filepath.Join(participant.StateDir, "stats.json"), StatsJSON: statsJSON}}}
	path := AttemptSettlementClosurePath(root, 42)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	parentInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = publishAttemptSettlementClosureWithSync(root, transaction, func(directory *os.File) error {
		calls++
		info, err := directory.Stat()
		if err != nil {
			return err
		}
		if calls == 1 {
			if !os.SameFile(info, parentInfo) {
				t.Fatal("first durability event did not sync coordinator parent")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("closure published before parent sync: %v", err)
			}
		} else if calls == 2 {
			if os.SameFile(info, parentInfo) {
				t.Fatal("second durability event did not sync closure child")
			}
			if _, err := ReadAttemptSettlementClosure(root, 42); err != nil {
				t.Fatal(err)
			}
		} else {
			t.Fatal("unexpected directory durability event")
		}
		return directory.Sync()
	})
	if err != nil || calls != 2 {
		t.Fatalf("directory durability calls=%d: %v", calls, err)
	}
}

// A durable-file/unsynced-rename retry must resync both directory entries even
// when the exact immutable filename already exists after the first attempt.
func TestAttemptSettlementClosureRetrySyncsExistingPublication(t *testing.T) {
	root := t.TempDir()
	participant, _ := newAttemptSettlementTestParticipant(t, 1)
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{participant}); err != nil {
		t.Fatal(err)
	}
	statsJSON, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	transaction := &attemptSettlementTransaction{Schema: attemptSettlementTransactionSchema, Epoch: 43, Snapshots: []attemptSettlementSnapshot{{NoID: 1, StatsJSON: statsJSON}}}
	path := AttemptSettlementClosurePath(root, 42)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("interrupt child directory sync")
	calls := 0
	err = publishAttemptSettlementClosureWithSync(root, transaction, func(directory *os.File) error {
		calls++
		if calls == 2 {
			return stop
		}
		return directory.Sync()
	})
	if !errors.Is(err, stop) || calls != 2 {
		t.Fatalf("first publication durability boundary calls=%d error=%v", calls, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	calls = 0
	if err := publishAttemptSettlementClosureWithSync(root, transaction, func(directory *os.File) error { calls++; return directory.Sync() }); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || calls != 2 || !bytes.Equal(before, after) {
		t.Fatalf("existing publication retry skipped durability or changed bytes: calls=%d error=%v", calls, err)
	}
}

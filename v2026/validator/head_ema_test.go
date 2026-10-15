package validator

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

func TestHeadEMAPersistsAndSeparatesGenerations(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewHeadEMAStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key1 := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 8}
	alpha := protocol.Rational{Numerator: 1, Denominator: 4}
	got, err := store.Fold(map[FleetScoreKey]*big.Rat{key1: big.NewRat(4, 1)}, alpha)
	if err != nil || got[8].Cmp(big.NewRat(4, 1)) != 0 {
		t.Fatalf("first fold = %v, %v", got, err)
	}
	// A new process restores the exact numerator/denominator.
	restarted, err := NewHeadEMAStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err = restarted.Fold(map[FleetScoreKey]*big.Rat{key1: big.NewRat(0, 1)}, alpha)
	if err != nil || got[8].Cmp(big.NewRat(3, 1)) != 0 {
		t.Fatalf("decayed fold = %v, %v", got, err)
	}
	// Generation two starts from its raw score; it does not inherit generation
	// one's state even though the live UID is the same. The inactive generation
	// continues decaying in history but cannot contribute to live output.
	key2 := key1
	key2.Generation = 2
	got, err = restarted.Fold(map[FleetScoreKey]*big.Rat{key2: big.NewRat(2, 1)}, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if got[8].Cmp(big.NewRat(2, 1)) != 0 {
		t.Fatalf("generation-isolated aggregate = %s", got[8])
	}
}

// Preview must be side-effect free until the corresponding intent exists;
// Commit then persists exactly once and rejects a changed transcript.
func TestHeadEMAPreviewCommitProtocolIsTransactional(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHeadEMAStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	raw := map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}
	preview, records, err := store.PreviewForEpoch(10, raw, alpha)
	if err != nil || preview[key.UID].Cmp(big.NewRat(4, 1)) != 0 {
		t.Fatalf("preview = %v records=%v err=%v", preview, records, err)
	}
	restarted, err := NewHeadEMAStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.values) != 0 || restarted.lastSubnetEpoch != nil {
		t.Fatal("preview mutated durable state before intent commit")
	}
	if err := restarted.CommitForEpoch(10, records, alpha); err != nil {
		t.Fatal(err)
	}
	if err := restarted.CommitForEpoch(10, records, alpha); err != nil {
		t.Fatalf("idempotent commit: %v", err)
	}
	changed := append([]HeadEMAMeasurement(nil), records...)
	changed[0].Next.Numerator = "3"
	if err := restarted.CommitForEpoch(10, changed, alpha); err == nil {
		t.Fatal("same-epoch commit accepted a changed transcript")
	}
}

// A failed durable commit must restore the exact in-memory prior so a retry
// reconstructs the same transcript rather than applying alpha twice.
func TestHeadEMACommitFailureRollsBackState(t *testing.T) {
	store, err := NewHeadEMAStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	raw := map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}
	_, records, err := store.PreviewForEpoch(10, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	validPath := store.path
	store.path = filepath.Join(blocker, "head-ema.json")
	if err := store.CommitForEpoch(10, records, alpha); err == nil {
		t.Fatal("commit unexpectedly persisted below a regular file")
	}
	if len(store.values) != 0 || store.lastSubnetEpoch != nil {
		t.Fatal("failed commit changed in-memory EMA state")
	}
	store.path = validPath
	_, retry, err := store.PreviewForEpoch(10, raw, alpha)
	if err != nil || !equalHeadEMAFolds(retry, records) {
		t.Fatalf("retry transcript changed: retry=%v err=%v", retry, err)
	}
}

func TestHeadEMADecaysMissingIdentityWithoutReturningItsStaleUID(t *testing.T) {
	store, err := NewHeadEMAStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	if _, err := store.Fold(map[FleetScoreKey]*big.Rat{key: big.NewRat(8, 1)}, protocol.Rational{Numerator: 1, Denominator: 2}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Fold(map[FleetScoreKey]*big.Rat{}, protocol.Rational{Numerator: 1, Denominator: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("missing identity leaked stale live weights: %v", got)
	}
	if len(store.values) != 1 || entryRat(store.values[key.String()]).Cmp(big.NewRat(4, 1)) != 0 {
		t.Fatalf("missing identity did not decay in persisted history: %+v", store.values)
	}
}

// A crash after the EMA file is committed but before the native extrinsic is
// prepared must not apply alpha a second time on restart.
func TestHeadEMAFoldForEpochIsRestartIdempotent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHeadEMAStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	if _, _, err := store.FoldForEpoch(10, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewHeadEMAStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	replayed, records, err := restarted.FoldForEpoch(10, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha)
	if err != nil || replayed[key.UID].Cmp(big.NewRat(4, 1)) != 0 || len(records) != 1 {
		t.Fatalf("same-epoch replay = %v records=%v err=%v", replayed, records, err)
	}
	if _, _, err := restarted.FoldForEpoch(10, map[FleetScoreKey]*big.Rat{key: big.NewRat(3, 1)}, alpha); err == nil {
		t.Fatal("same epoch accepted different raw inputs")
	}
	next, _, err := restarted.FoldForEpoch(11, map[FleetScoreKey]*big.Rat{key: big.NewRat(0, 1)}, alpha)
	if err != nil || next[key.UID].Cmp(big.NewRat(2, 1)) != 0 {
		t.Fatalf("next epoch fold = %v, %v", next, err)
	}
}

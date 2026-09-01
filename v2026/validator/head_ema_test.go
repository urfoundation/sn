package validator

import (
	"math/big"
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

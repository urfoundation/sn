package validator

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/protocol"
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
	// one's state even though the live UID is the same.
	key2 := key1
	key2.Generation = 2
	got, err = restarted.Fold(map[FleetScoreKey]*big.Rat{key2: big.NewRat(2, 1)}, alpha)
	if err != nil {
		t.Fatal(err)
	}
	// gen1 decays to 9/4 and gen2 starts at 2, aggregated UID = 17/4.
	if got[8].Cmp(big.NewRat(17, 4)) != 0 {
		t.Fatalf("generation-isolated aggregate = %s", got[8])
	}
}

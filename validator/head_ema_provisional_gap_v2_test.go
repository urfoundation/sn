//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

// This reproduces the actual retained 1401 -> next measured 1403 gap through
// real bounded stores. FoldForEpochV2 supplies the established arithmetic;
// skipped epochs do not supply fabricated samples or extra decay operations.
func TestHeadEMAProvisionalGapV2MatchesActualFoldAndRestart(t *testing.T) {
	t.Parallel()
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	first := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	second := FleetScoreKey{FleetID: [32]byte{3}, Hotkey: [32]byte{4}, Generation: 1, UID: 8}
	prior := map[FleetScoreKey]*big.Rat{first: big.NewRat(4, 1), second: big.NewRat(6, 1)}
	raw := map[FleetScoreKey]*big.Rat{first: big.NewRat(7, 2)}
	limits := headEMAStoreV2TestLimits()
	newPrior := func() *HeadEMAStore {
		t.Helper()
		store, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.FoldForEpochV2(t.Context(), 1401, prior, alpha); err != nil {
			t.Fatal(err)
		}
		return store
	}
	reference, store := newPrior(), newPrior()
	want, wantRecords, err := reference.FoldForEpochV2(t.Context(), 1403, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	// Ordinary bounded construction never enables provisional continuation.
	if store.allowsHeadEMAEpochGaps() {
		t.Fatal("ordinary constructor enabled provisional epoch gaps")
	}
	if _, _, err := store.PreviewForEpochV2(t.Context(), 1403, raw, alpha); err == nil {
		t.Fatal("strict preview accepted a skipped epoch")
	}
	if _, _, err := store.previewForEpochV2(t.Context(), 1403, raw, alpha, limits.MaxEntries, limits.MaxControlBytes); err == nil {
		t.Fatal("strict compact admission accepted a skipped epoch")
	}
	if err := store.CommitForEpochV2(t.Context(), 1403, wantRecords, alpha); err == nil {
		t.Fatal("strict commit accepted a skipped epoch")
	}
	store.v2.provisionalEpochGaps = true
	got, records, err := store.PreviewForEpochV2(t.Context(), 1403, raw, alpha)
	if err != nil || !reflect.DeepEqual(got, want) || !equalHeadEMAFolds(records, wantRecords) {
		t.Fatalf("provisional preview differs from existing gap fold: %v", err)
	}
	compact, compactRecords, err := store.previewForEpochV2(t.Context(), 1403, raw, alpha, limits.MaxEntries, limits.MaxControlBytes)
	if err != nil || !reflect.DeepEqual(compact, want) || !equalHeadEMAFolds(compactRecords, wantRecords) {
		t.Fatalf("compact provisional preview differs from existing gap fold: %v", err)
	}
	if len(got) != 1 || got[7].Cmp(big.NewRat(15, 4)) != 0 || len(records) != 2 || records[1].HasRaw || records[1].Prior != (RationalJSON{Numerator: "6", Denominator: "1"}) || records[1].Next != (RationalJSON{Numerator: "3", Denominator: "1"}) {
		t.Fatalf("gap inserted a missing-epoch observation or extra decay: %v %v", got, records)
	}
	bad := append([]HeadEMAMeasurement(nil), records...)
	bad[0].Next.Numerator = "19"
	if err := store.CommitForEpochV2(t.Context(), 1403, bad, alpha); err == nil {
		t.Fatal("gap allowed a transcript that changes exact scores")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := store.PreviewForEpochV2(cancelled, 1403, raw, alpha); !errors.Is(err, context.Canceled) {
		t.Fatalf("gap ignored preview cancellation: %v", err)
	}
	if err := store.CommitForEpochV2(cancelled, 1403, records, alpha); !errors.Is(err, context.Canceled) {
		t.Fatalf("gap ignored commit cancellation: %v", err)
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(before, after) || *store.lastSubnetEpoch != 1401 {
		t.Fatalf("preview/refusal advanced retained state: %v", err)
	}
	if err := store.CommitForEpochV2(t.Context(), 1403, records, alpha); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.values, reference.values) || *store.lastSubnetEpoch != 1403 {
		t.Fatal("provisional commit differs from existing actual fold")
	}
	committed, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewHeadEMAStoreV2(t.Context(), filepath.Dir(store.path), limits)
	if err != nil || reloaded.allowsHeadEMAEpochGaps() {
		t.Fatalf("reload failed or file data enabled provisional mode: %v", err)
	}
	reloaded.v2.provisionalEpochGaps = true
	again, retry, err := reloaded.PreviewForEpochV2(t.Context(), 1403, raw, alpha)
	if err != nil || !reflect.DeepEqual(again, got) || !equalHeadEMAFolds(retry, records) {
		t.Fatalf("same-epoch restart repeated the fold: %v", err)
	}
	if err := reloaded.CommitForEpochV2(t.Context(), 1403, records, alpha); err != nil {
		t.Fatal(err)
	}
	for _, epoch := range []uint64{1402, 0} {
		if _, _, err := reloaded.PreviewForEpochV2(t.Context(), epoch, raw, alpha); err == nil {
			t.Fatalf("provisional preview accepted regression to %d", epoch)
		}
		if err := reloaded.CommitForEpochV2(t.Context(), epoch, records, alpha); err == nil {
			t.Fatalf("provisional commit accepted regression to %d", epoch)
		}
	}
	changed := map[FleetScoreKey]*big.Rat{first: big.NewRat(8, 1)}
	if _, _, err := reloaded.PreviewForEpochV2(t.Context(), 1403, changed, alpha); err == nil {
		t.Fatal("same-epoch retry changed its raw scores")
	}
	if _, _, err := reloaded.PreviewForEpochV2(t.Context(), 1403, raw, protocol.Rational{Numerator: 1, Denominator: 3}); err == nil {
		t.Fatal("same-epoch retry changed its policy")
	}
	if err := reloaded.CommitForEpochV2(t.Context(), 1403, bad, alpha); err == nil {
		t.Fatal("same-epoch commit changed its transcript")
	}
	after, err = os.ReadFile(store.path)
	finalIdentity, statErr := os.Stat(store.path)
	if err != nil || statErr != nil || !bytes.Equal(committed, after) || !os.SameFile(identity, finalIdentity) {
		t.Fatalf("retry/refusal replaced committed state: %v %v", err, statErr)
	}
}

func TestHeadEMAProvisionalGapV2RetainsFileCustody(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	store.v2.provisionalEpochGaps = true
	_, records, err := store.PreviewForEpochV2(t.Context(), 12, fixture.raw, fixture.alpha)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fixture.path, fixture.path+".held"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, fixture.encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitForEpochV2(t.Context(), 12, records, fixture.alpha); err == nil {
		t.Fatal("provisional gap commit overwrote a replacement file")
	}
	after, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(fixture.encoded, after) || *store.lastSubnetEpoch != 10 {
		t.Fatalf("custody refusal changed replacement or prior state: %v", err)
	}
}

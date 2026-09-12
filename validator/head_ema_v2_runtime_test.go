//go:build linux || darwin

package validator

// These controls use genuine persisted rational folds and real native file
// operations. They are not M8/activation/on-chain witnesses. Observer-injected
// errors are labeled separately from actual filesystem and syscall failures.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

// Load the real fixture with caller-selected immutable runtime allowances.
func loadHeadEMAStoreV2RuntimeTest(t *testing.T, fixture *headEMAStoreV2TestFixture, limits HeadEMAStoreV2Limits) *HeadEMAStore {
	t.Helper()
	store, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, limits)
	if err != nil {
		t.Fatal(err)
	}
	if store.v2 == nil {
		t.Fatal("bounded constructor did not retain a runtime owner")
	}
	return store
}

// Native publication must not erase an uncommitted caller's memory or disk.
func assertHeadEMAStoreV2Unchanged(t *testing.T, fixture *headEMAStoreV2TestFixture, store *HeadEMAStore) {
	t.Helper()
	encoded, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(encoded, fixture.encoded) {
		t.Fatalf("uncommitted head changed: %v", err)
	}
	if store.lastSubnetEpoch == nil || *store.lastSubnetEpoch != 10 || !equalHeadEMAFolds(store.lastFold, fixture.records) {
		t.Fatal("uncommitted in-memory epoch/transcript changed")
	}
}

// An uncertain operation returns no speculative state, refuses later entry
// before callbacks and leaves a marker which a fresh loader cannot re-bless.
func assertHeadEMAStoreV2Fault(t *testing.T, fixture *headEMAStoreV2TestFixture, store *HeadEMAStore) {
	t.Helper()
	if store.v2.fault == nil {
		t.Fatal("uncertain durable operation left a reusable owner")
	}
	if store.lastSubnetEpoch == nil || *store.lastSubnetEpoch != 10 || !equalHeadEMAFolds(store.lastFold, fixture.records) {
		t.Fatal("uncertain write published speculative memory")
	}
	marker, err := os.Lstat(filepath.Join(fixture.stateDir, headEMAStoreV2Marker))
	if err != nil || !marker.Mode().IsRegular() {
		t.Fatalf("uncertain write lost its marker: %v", err)
	}
	callbacks := 0
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2Preview, 10, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(string, *os.File) error { callbacks++; return nil }})
	if err == nil || out != nil || records != nil || callbacks != 0 {
		t.Fatalf("faulted owner continued: callbacks=%d error=%v", callbacks, err)
	}
	parsed := 0
	reloaded, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(),
		headEMAStoreV2LoadHooks{beforeRational: func() error { parsed++; return nil }})
	if err == nil || reloaded != nil || parsed != 0 {
		t.Fatalf("unresolved marker was accepted on restart: parsed=%d error=%v", parsed, err)
	}
}

// This old public entry point previously bypassed the loader's input census.
func TestHeadEMAStoreV2RuntimeLegacyPreviewRetainsLimits(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	limits := headEMAStoreV2TestLimits()
	limits.MaxEntries = 2
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, limits)
	raw := map[FleetScoreKey]*big.Rat{fixture.keys[0]: big.NewRat(1, 1), fixture.keys[1]: big.NewRat(2, 1),
		FleetScoreKey{FleetID: [32]byte{8}, UID: 11}: big.NewRat(3, 1)}
	out, records, err := store.PreviewForEpoch(11, raw, fixture.alpha)
	if err == nil || out != nil || records != nil {
		t.Fatalf("legacy preview bypassed v2 census: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeLegacyPreviewRetainsLimits")
}

// A genuine independently reconstructed three-row transcript must not evade
// the bounded constructor by entering its original CommitForEpoch signature.
func TestHeadEMAStoreV2RuntimeLegacyCommitRetainsLimits(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	limits := headEMAStoreV2TestLimits()
	limits.MaxEntries = 2
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, limits)
	raw := map[FleetScoreKey]*big.Rat{fixture.keys[0]: big.NewRat(1, 1), fixture.keys[1]: big.NewRat(2, 1),
		FleetScoreKey{FleetID: [32]byte{8}, UID: 11}: big.NewRat(3, 1)}
	_, records, err := fixture.store.PreviewForEpoch(11, raw, fixture.alpha)
	if err != nil || len(records) != 3 {
		t.Fatalf("real transcript fixture failed: %v", err)
	}
	if err := store.CommitForEpoch(11, records, fixture.alpha); err == nil {
		t.Fatal("legacy commit bypassed v2 census")
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeLegacyCommitRetainsLimits")
}

// Integer words and generated decimal growth are bounded before Set/folding,
// including callers of the legacy unepoched Fold signature.
func TestHeadEMAStoreV2RuntimeLegacyFoldRetainsRationalLimits(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	limits := headEMAStoreV2TestLimits()
	limits.MaxControlBytes = 32768
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, limits)
	large := new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 4096))
	out, err := store.Fold(map[FleetScoreKey]*big.Rat{fixture.keys[0]: large}, fixture.alpha)
	if err == nil || out != nil {
		t.Fatalf("legacy fold bypassed v2 arithmetic allowance: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeLegacyFoldRetainsRationalLimits")
}

// Exact encoded output is checked before any native write, even when admitted
// input and arithmetic fit. The existing file length is the caller's limit.
func TestHeadEMAStoreV2RuntimeLegacyFoldEpochRetainsOutputLimits(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	limits := headEMAStoreV2TestLimits()
	limits.MaxFileBytes = uint64(len(fixture.encoded))
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, limits)
	large := new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 512))
	out, records, err := store.FoldForEpoch(11, map[FleetScoreKey]*big.Rat{fixture.keys[0]: large}, fixture.alpha)
	if err == nil || out != nil || records != nil {
		t.Fatalf("legacy epoch fold bypassed v2 wire allowance: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	if _, err := os.Lstat(filepath.Join(fixture.stateDir, headEMAStoreV2Marker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output refusal performed native publication: %v", err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeLegacyFoldEpochRetainsOutputLimits")
}

// The old pathname commit could overwrite a replacement after v2 loading;
// now its actual loader-owned inode remains the publication authority.
func TestHeadEMAStoreV2RuntimeLegacyCommitRetainsLoadedIdentity(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	_, records, err := fixture.store.PreviewForEpoch(11, fixture.raw, fixture.alpha)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fixture.path, fixture.path+".held"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, fixture.encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitForEpoch(11, records, fixture.alpha); err == nil {
		t.Fatal("legacy commit replaced a different loaded inode")
	}
	encoded, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(encoded, fixture.encoded) {
		t.Fatalf("identity refusal overwrote replacement: %v", err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeLegacyCommitRetainsLoadedIdentity")
}

// Clean native commits preserve exact decay, empty-live membership, genuine
// reload and same-epoch idempotency without another file replacement.
func TestHeadEMAStoreV2RuntimeCommitsReloadsAndRetriesExactFold(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	raw := map[FleetScoreKey]*big.Rat{fixture.keys[0]: big.NewRat(7, 2)}
	out, records, err := store.PreviewForEpochV2(context.Background(), 11, raw, fixture.alpha)
	if err != nil || len(out) != 1 || out[7].Cmp(big.NewRat(15, 4)) != 0 || len(records) != 2 {
		t.Fatalf("exact preview differs: %v %v", out, err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	if err := store.CommitForEpochV2(context.Background(), 11, records, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	status, err := os.Stat(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	again, retained, err := reloaded.FoldForEpochV2(context.Background(), 11, raw, fixture.alpha)
	if err != nil || !reflect.DeepEqual(again, out) || !equalHeadEMAFolds(retained, records) {
		t.Fatalf("restart repeated/changed fold: %v", err)
	}
	if err := reloaded.CommitForEpochV2(context.Background(), 11, records, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(fixture.path)
	afterStatus, statErr := os.Stat(fixture.path)
	if err != nil || statErr != nil || !bytes.Equal(after, encoded) || !os.SameFile(status, afterStatus) {
		t.Fatalf("same-epoch retry rewrote durable head: %v %v", err, statErr)
	}
	next, nextRecords, err := reloaded.FoldForEpochV2(context.Background(), 12, map[FleetScoreKey]*big.Rat{fixture.keys[0]: new(big.Rat)}, fixture.alpha)
	if err != nil || len(next) != 1 || next[7].Cmp(big.NewRat(15, 8)) != 0 || len(nextRecords) != 2 {
		t.Fatalf("successor decay differs: %v %v", next, err)
	}
	for _, name := range []string{headEMAStoreV2Marker, headEMAStoreV2Candidate} {
		if _, err := os.Lstat(filepath.Join(fixture.stateDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("clean commit left unresolved evidence %s: %v", name, err)
		}
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeCommitsReloadsAndRetriesExactFold")
}

// Existing operation-specific epoch semantics are retained, not silently
// homogenized: preview rejects jumps; FoldForEpoch accepts a forward jump.
func TestHeadEMAStoreV2RuntimePreservesEpochAndUnscopedSemantics(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	if out, records, err := store.PreviewForEpochV2(context.Background(), 12, fixture.raw, fixture.alpha); err == nil || out != nil || records != nil {
		t.Fatal("preview accepted an epoch jump")
	}
	if _, _, err := store.FoldForEpochV2(context.Background(), 12, fixture.raw, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FoldV2(context.Background(), nil, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	if store.lastSubnetEpoch != nil || store.lastAlpha != nil || store.lastFold != nil {
		t.Fatal("unscoped fold retained scoped metadata")
	}
	reloaded := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	if reloaded.lastSubnetEpoch != nil || reloaded.lastAlpha != nil || reloaded.lastFold != nil || len(reloaded.values) != 2 {
		t.Fatal("unscoped exact state changed across restart")
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimePreservesEpochAndUnscopedSemantics")
}

// Legacy-created objects retain their original methods; context APIs cannot
// retag them as bounded merely because a schema-two file exists at their path.
func TestHeadEMAStoreV2RuntimeDoesNotRetagLegacyCreatedStores(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	if fixture.store.v2 != nil {
		t.Fatal("legacy constructor acquired v2 ownership")
	}
	if _, _, err := fixture.store.PreviewForEpochV2(context.Background(), 11, fixture.raw, fixture.alpha); err == nil {
		t.Fatal("new API accepted legacy authority")
	}
	if _, _, err := fixture.store.PreviewForEpoch(11, fixture.raw, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.store.FoldForEpoch(12, fixture.raw, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeDoesNotRetagLegacyCreatedStores")
}

// Known policy, epoch and nil/canceled context refusal precedes all callbacks.
func TestHeadEMAStoreV2RuntimeRejectsKnownInvalidAdmissionBeforeCallbacks(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		ctx   context.Context
		epoch uint64
		alpha protocol.Rational
	}{
		{ctx: nil, epoch: 11, alpha: fixture.alpha},
		{ctx: canceled, epoch: 11, alpha: fixture.alpha},
		{ctx: context.Background(), epoch: 9, alpha: fixture.alpha},
		{ctx: context.Background(), epoch: 12, alpha: fixture.alpha},
		{ctx: context.Background(), epoch: 11, alpha: protocol.Rational{}},
		{ctx: context.Background(), epoch: 10, alpha: protocol.Rational{Numerator: 1, Denominator: 3}},
	}
	for index, candidate := range cases {
		callbacks := 0
		out, records, err := store.runHeadEMAStoreV2(candidate.ctx, headEMAStoreV2Preview, candidate.epoch, fixture.raw, nil, candidate.alpha,
			headEMAStoreV2RuntimeHooks{step: func(string, *os.File) error { callbacks++; return nil }})
		if err == nil || out != nil || records != nil || callbacks != 0 {
			t.Fatalf("known invalid case %d reached callbacks: %d %v", index, callbacks, err)
		}
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeRejectsKnownInvalidAdmissionBeforeCallbacks")
}

// This source-contract boundary pins equality/one-less for the completed
// reservation, then exercises actual copying, full arithmetic and final custody.
// It is not an independently derived proof of math/big allocator memory.
func TestHeadEMAStoreV2RuntimeExactSharedReservationBoundary(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	limits := headEMAStoreV2TestLimits()
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, limits)
	budget, err := func() (headEMAStoreV2Budget, error) {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.admitHeadEMAStoreV2WithLock(context.Background(), headEMAStoreV2Preview, 11, fixture.raw, nil, fixture.alpha, limits)
	}()
	if err != nil || budget.used == 0 {
		t.Fatalf("complete reservation failed: %v", err)
	}
	for _, bound := range []uint64{budget.used - 1, budget.used} {
		limits.MaxControlBytes = bound
		bounded := loadHeadEMAStoreV2RuntimeTest(t, fixture, limits)
		owned := 0
		out, records, err := bounded.runHeadEMAStoreV2(context.Background(), headEMAStoreV2Preview, 11, fixture.raw, nil, fixture.alpha,
			headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
				if step == "operands-owned" {
					owned++
				}
				return nil
			}})
		if bound < budget.used {
			if err == nil || out != nil || records != nil || owned != 0 {
				t.Fatalf("one-short shared reservation reached math: %d %v", owned, err)
			}
		} else if err != nil || len(out) != 2 || len(records) != 2 || owned != 1 {
			t.Fatalf("exact shared reservation failed: %d %v", owned, err)
		}
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeExactSharedReservationBoundary")
}

// A genuine empty-value state still owns its nonempty last-fold history.
func TestHeadEMAStoreV2RuntimePreservesTranscriptOnlyHistory(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	alpha := protocol.Rational{Numerator: 1, Denominator: 1}
	if _, _, err := fixture.store.FoldForEpoch(11, nil, alpha); err != nil {
		t.Fatal(err)
	}
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	if len(store.values) != 0 || len(store.lastFold) != 2 {
		t.Fatal("fixture lost actual transcript-only history")
	}
	out, records, err := store.PreviewForEpochV2(context.Background(), 11, nil, alpha)
	if err != nil || len(out) != 0 || len(records) != 2 {
		t.Fatalf("empty-live retry lost retained history: %v", err)
	}
	if err := store.CommitForEpochV2(context.Background(), 11, records, alpha); err != nil {
		t.Fatal(err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimePreservesTranscriptOnlyHistory")
}

// Callback mutation cannot alter raw values copied before the first external
// boundary, and returned map/transcript owners cannot mutate retained state.
func TestHeadEMAStoreV2RuntimeOwnsInputsAndReturnedValues(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	raw := map[FleetScoreKey]*big.Rat{fixture.keys[0]: big.NewRat(7, 2)}
	owned := 0
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2Preview, 11, raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
			if step == "operands-owned" {
				owned++
				raw[fixture.keys[0]].SetInt64(99)
				delete(raw, fixture.keys[0])
			}
			return nil
		}})
	if err != nil || owned != 1 || out[7].Cmp(big.NewRat(15, 4)) != 0 {
		t.Fatalf("raw ownership changed preview: %v", err)
	}
	out[7].SetInt64(123)
	records[0].Next = RationalJSON{Numerator: "999", Denominator: "1"}
	again, retained, err := store.PreviewForEpochV2(context.Background(), 10, fixture.raw, fixture.alpha)
	if err != nil || again[7].Cmp(big.NewRat(4, 1)) != 0 || !equalHeadEMAFolds(retained, fixture.records) {
		t.Fatalf("returned owner mutated durable state: %v", err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeOwnsInputsAndReturnedValues")
}

// Commit copies the exact genuine transcript before any observer can change
// its caller-owned slice; the saved transcript is still fully reconstructed.
func TestHeadEMAStoreV2RuntimeOwnsCommitTranscriptBeforeCallbacks(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	_, records, err := fixture.store.PreviewForEpoch(11, fixture.raw, fixture.alpha)
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]HeadEMAMeasurement(nil), records...)
	_, _, err = store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2Commit, 11, nil, records, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
			if step == "operands-owned" {
				records[0].Next.Numerator = "999"
			}
			return nil
		}})
	if err != nil || !equalHeadEMAFolds(store.lastFold, expected) {
		t.Fatalf("caller mutation changed committed transcript: %v", err)
	}
	reloaded := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	if !equalHeadEMAFolds(reloaded.lastFold, expected) {
		t.Fatal("restart changed owned commit transcript")
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeOwnsCommitTranscriptBeforeCallbacks")
}

// Invalid exact transcripts never mutate live state, and no durable phase has
// begun, so a subsequent genuine commit remains safe and succeeds.
func TestHeadEMAStoreV2RuntimeValidationRollbackAllowsGenuineRetry(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	_, records, err := fixture.store.PreviewForEpoch(11, fixture.raw, fixture.alpha)
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]HeadEMAMeasurement(nil), records...)
	bad[0].Next = RationalJSON{Numerator: "999", Denominator: "1"}
	if err := store.CommitForEpochV2(context.Background(), 11, bad, fixture.alpha); err == nil {
		t.Fatal("invalid transcript was accepted")
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	if store.v2.fault != nil {
		t.Fatal("pure validation failure became durable uncertainty")
	}
	if err := store.CommitForEpochV2(context.Background(), 11, records, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeValidationRollbackAllowsGenuineRetry")
}

// A callback can inspect state and try another operation without a mutex
// deadlock; the independent operation gate refuses that reentrant ownership.
func TestHeadEMAStoreV2RuntimeCallbacksOutsideMutexRejectReentry(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	observed := false
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2Preview, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
			if step != "operands-owned" {
				return nil
			}
			observed = true
			if !store.mu.TryLock() {
				t.Error("external callback ran under EMA state mutex")
			} else {
				store.mu.Unlock()
			}
			nested, transcript, err := store.PreviewForEpochV2(context.Background(), 11, fixture.raw, fixture.alpha)
			if !errors.Is(err, errHeadEMAStoreV2Busy) || nested != nil || transcript != nil {
				t.Errorf("reentrant owner did not refuse promptly: %v", err)
			}
			return nil
		}})
	if err != nil || !observed || len(out) != 2 || len(records) != 2 {
		t.Fatalf("outer operation changed after safe reentry refusal: %v", err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeCallbacksOutsideMutexRejectReentry")
}

// Explicit channels hold one real operation at its owned-input boundary.
// Another caller cannot commit through that ownership; no sleep proves this.
func TestHeadEMAStoreV2RuntimeConcurrentOwnerAdmission(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2Preview, 11, fixture.raw, nil, fixture.alpha,
			headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
				if step == "operands-owned" {
					close(entered)
					<-release
				}
				return nil
			}})
		done <- err
	}()
	<-entered
	_, _, err := store.FoldForEpochV2(context.Background(), 11, fixture.raw, fixture.alpha)
	close(release)
	outerErr := <-done
	if !errors.Is(err, errHeadEMAStoreV2Busy) || outerErr != nil {
		t.Fatalf("concurrent ownership differs: inner=%v outer=%v", err, outerErr)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeConcurrentOwnerAdmission")
}

// Cancellation before native acquisition leaves no durable uncertainty and
// does not prevent a later valid preview on the same bounded owner.
func TestHeadEMAStoreV2RuntimeCancellationBeforeAcquisitionAllowsRetry(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acquired := false
	out, records, err := store.runHeadEMAStoreV2(ctx, headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
			if step == "operands-owned" {
				cancel()
			}
			if step == "directory-opened" {
				acquired = true
			}
			return nil
		}})
	if !errors.Is(err, context.Canceled) || acquired || out != nil || records != nil || store.v2.fault != nil {
		t.Fatalf("pre-I/O cancellation changed ownership: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	if _, _, err := store.PreviewForEpochV2(context.Background(), 11, fixture.raw, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeCancellationBeforeAcquisitionAllowsRetry")
}

// An individually admitted current census cannot spend another full allowance
// after retained state; the union refuses before any external math observer.
func TestHeadEMAStoreV2RuntimeCurrentPriorUnionPrecedesMath(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	limits := headEMAStoreV2TestLimits()
	limits.MaxEntries = 2
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, limits)
	raw := map[FleetScoreKey]*big.Rat{FleetScoreKey{FleetID: [32]byte{8}, UID: 11}: big.NewRat(3, 17)}
	callbacks := 0
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2Preview, 11, raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(string, *os.File) error { callbacks++; return nil }})
	if err == nil || !strings.Contains(err.Error(), "union") || out != nil || records != nil || callbacks != 0 {
		t.Fatalf("current/history union was not admitted first: %d %v", callbacks, err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeCurrentPriorUnionPrecedesMath")
}

// Distinct generations on one UID retain independent prior ownership while
// the actual output sums exact rationals over the full untruncated transcript.
func TestHeadEMAStoreV2RuntimeRetainsGenerationAndUIDArithmetic(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	newKey := fixture.keys[0]
	newKey.Generation++
	raw := map[FleetScoreKey]*big.Rat{fixture.keys[0]: big.NewRat(1, 17), newKey: big.NewRat(3, 19), fixture.keys[1]: big.NewRat(5, 23)}
	want, expected, err := fixture.store.PreviewForEpoch(11, raw, fixture.alpha)
	if err != nil || len(expected) != 3 {
		t.Fatalf("independent genuine fold fixture differs: %v", err)
	}
	out, records, err := store.FoldForEpochV2(context.Background(), 11, raw, fixture.alpha)
	if err != nil || !reflect.DeepEqual(out, want) || !equalHeadEMAFolds(records, expected) {
		t.Fatalf("bounded full fold changed generation/UID arithmetic: %v", err)
	}
	for _, record := range records {
		if record.Key == newKey && (record.HasPrior || record.Next != (RationalJSON{Numerator: "3", Denominator: "19"})) {
			t.Fatal("new generation inherited prior UID ownership")
		}
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeRetainsGenerationAndUIDArithmetic")
}

// Exact canonical wire bytes match the actual old MarshalIndent encoding.
// This is an independent codec/boundary control, not a claimed persisted-fold
// verification for the deliberately varied ignored timestamp string.
func TestHeadEMAStoreV2RuntimeExactWireAndEscapedOutputBoundary(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	var original headEMAFile
	if err := json.Unmarshal(fixture.encoded, &original); err != nil {
		t.Fatal(err)
	}
	for _, timestamp := range []string{"2026-09-06T00:00:00Z", "<>&\t\\\"\u2028\u2029☃", string([]byte{'x', 0xff, 0, 'y'})} {
		file := original
		file.UpdatedAt = timestamp
		want, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, '\n')
		for _, limit := range []uint64{uint64(len(want) - 1), uint64(len(want))} {
			budget := headEMAStoreV2Budget{limit: 4 * 1024 * 1024}
			encoded, err := marshalHeadEMAStoreV2File(context.Background(), file, &budget, limit)
			if limit < uint64(len(want)) {
				if err == nil || encoded != nil || budget.used != 0 {
					t.Fatalf("one-short output was allocated/admitted: %v", err)
				}
			} else if err != nil || !bytes.Equal(encoded, want) || budget.used != uint64(len(want)) {
				t.Fatalf("exact legacy wire differs: %v", err)
			}
		}
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeExactWireAndEscapedOutputBoundary")
}

// Empty/null encoding semantics stay distinct exactly as in saveLocked.
func TestHeadEMAStoreV2RuntimePreservesNilEmptyWireSemantics(t *testing.T) {
	t.Parallel()
	files := []headEMAFile{
		{Schema: headEMASchemaV2, UpdatedAt: "fixed"},
		{Schema: headEMASchemaV2, UpdatedAt: "fixed", Entries: []headEMAEntry{}, LastFold: []HeadEMAMeasurement{}},
	}
	for index, file := range files {
		want, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, '\n')
		budget := headEMAStoreV2Budget{limit: 1024 * 1024}
		encoded, err := marshalHeadEMAStoreV2File(context.Background(), file, &budget, uint64(len(want)))
		if err != nil || !bytes.Equal(encoded, want) {
			t.Fatalf("nil/empty wire case %d changed: %v", index, err)
		}
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimePreservesNilEmptyWireSemantics")
}

// New mutable/schema fields require an explicit copy/admission/codec audit;
// reflection here is a field-census contract, not behavioral qualification.
func TestHeadEMAStoreV2RuntimeFieldCensusPinsAdmissionOwners(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value  reflect.Type
		fields string
	}{
		{value: reflect.TypeFor[HeadEMAStore](), fields: "mu path values lastSubnetEpoch lastAlpha lastFold v2"},
		{value: reflect.TypeFor[headEMAFile](), fields: "Schema UpdatedAt Entries LastSubnetEpoch LastAlpha LastFold"},
		{value: reflect.TypeFor[headEMAEntry](), fields: "Key Numerator Denominator"},
		{value: reflect.TypeFor[HeadEMAMeasurement](), fields: "Key HasRaw Raw HasPrior Prior Next"},
		{value: reflect.TypeFor[FleetScoreKey](), fields: "FleetID Hotkey Generation UID"},
		{value: reflect.TypeFor[headEMAStoreV2Owner](), fields: "limits namespace active fault provisionalEpochGaps"},
	}
	for _, candidate := range cases {
		var fields []string
		for index := 0; index < candidate.value.NumField(); index++ {
			fields = append(fields, candidate.value.Field(index).Name)
		}
		if strings.Join(fields, " ") != candidate.fields {
			t.Errorf("%s field census changed: %v", candidate.value.Name(), fields)
		}
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeFieldCensusPinsAdmissionOwners")
}

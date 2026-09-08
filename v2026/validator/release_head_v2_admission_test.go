//go:build linux || darwin

package validator

// Causal roots call the existing collector/preview entrypoints with real M8
// evidence or real persisted EMA state. Separate numeric/planner contracts
// exercise exact arithmetic without claiming additional signed trail coverage.

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// Use the real old control-storage convention as an independent input-only
// budget oracle; it deliberately excludes newly generated output ownership.
func releaseHeadV2AccountingInputBudget(t *testing.T, store *HeadEMAStore, raw map[FleetScoreKey]*big.Rat) uint64 {
	t.Helper()
	remaining := uint64(1024 * 1024)
	store.mu.Lock()
	defer store.mu.Unlock()
	for key, value := range store.values {
		if uint64(len(key)) > remaining {
			t.Fatal("input budget fixture exceeds its existing allowance")
		}
		remaining -= uint64(len(key))
		if err := releaseMeasurementV2ControlStorage(t.Context(), reflect.ValueOf(&value), &remaining); err != nil {
			t.Fatal(err)
		}
	}
	if err := releaseMeasurementV2ControlStorage(t.Context(), reflect.ValueOf(store.lastFold), &remaining); err != nil {
		t.Fatal(err)
	}
	used := uint64(1024*1024) - remaining + releaseHeadV2TestOwnerControlBytes(store)
	for _, value := range raw {
		used += (uint64(len(value.Num().Bits())) + uint64(len(value.Denom().Bits()))) * uint64(reflect.TypeFor[big.Word]().Size())
	}
	return used
}

// Reconstruct the unfinished draft/census, copied replay options and native
// owner reservation, leaving no allowance for retained/current/generated EMA.
// Frozen isolated causal fixtures retain their original pre-composition formula.
func releaseHeadV2AccountingCollectionBudget(t *testing.T, fixture *releaseHeadV2TestFixture, options ReleaseMeasurementV2Options) uint64 {
	t.Helper()
	draft := cloneReleaseMeasurementArtifact(t, fixture.measurement.artifact)
	draft.Bindings, draft.HeadEMA = []ReleaseBindingMeasurement{}, []HeadEMAMeasurement{}
	draft.Pools, draft.DepositAudits = []ReleasePoolMeasurement{}, []DepositAudit{}
	remaining := uint64(1024 * 1024)
	if err := releaseMeasurementV2ControlStorage(t.Context(), reflect.ValueOf(draft), &remaining); err != nil {
		t.Fatal(err)
	}
	used := uint64(1024*1024) - remaining + uint64(len(fixture.hotkeys))*(32+2) + uint64(len(draft.Inputs))*(8+uint64(reflect.TypeFor[ClientKeyFunc]().Size()))
	for _, input := range draft.Inputs {
		used += uint64(len(input.Stats.Providers)) * (uint64(reflect.TypeFor[ReleaseBindingMeasurement]().Size()) + 36 + 5*66 + 16 + 1)
	}
	budget, err := reserveReleaseHeadV2CollectionDerived(t.Context(), draft, releaseHeadV2Budget{limit: 1024 * 1024, used: used})
	if err != nil {
		t.Fatal(err)
	}
	budget, err = reserveReleaseHeadV2OperatorControls(t.Context(), options, budget)
	if err != nil {
		t.Fatal(err)
	}
	return budget.used + releaseHeadV2TestOwnerControlBytes(fixture.steerer.headEMA)
}

// Actual existing preview/commit/reload supplies valid local history. These
// fixtures do not claim that public-chain history authentication has occurred.
func releaseHeadV2AccountingCommit(t *testing.T, fixture *releaseHeadV2TestFixture, epoch uint64, raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational) []byte {
	t.Helper()
	store := fixture.steerer.headEMA
	_, transcript, err := store.PreviewForEpochV2(t.Context(), epoch, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitForEpochV2(t.Context(), epoch, transcript, alpha); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := newReleaseHeadV2EMAStore(t, filepath.Dir(store.path))
	if err != nil {
		t.Fatal(err)
	}
	fixture.steerer.headEMA = reloaded
	return encoded
}

// Independent fixed identities cannot overlap the genuine collector fleets.
func releaseHeadV2AccountingRaw(count int) map[FleetScoreKey]*big.Rat {
	raw := map[FleetScoreKey]*big.Rat{}
	for index := 0; index < count; index++ {
		key := FleetScoreKey{FleetID: [32]byte{0xe1, byte(index)}, Hotkey: [32]byte{0xe2, byte(index)}, Generation: 1, UID: uint16(100 + index)}
		raw[key] = big.NewRat(int64(index+1), 3)
	}
	return raw
}

// A current one-word rational never authorizes a much larger generated record.
func TestReleaseHeadV2AccountingRejectsUnreservedFixedOutput(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingRejectsUnreservedFixedOutput")
		}
	})
	store, err := newReleaseHeadV2EMAStore(t, newReleaseHeadV2TestStateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	raw := releaseHeadV2AccountingRaw(1)
	for key := range raw {
		raw[key] = big.NewRat(1, 1)
	}
	wordBytes := 2 * uint64(reflect.TypeFor[big.Word]().Size())
	bound := releaseHeadV2TestOwnerControlBytes(store) + wordBytes
	out, head, err := store.previewForEpochV2(t.Context(), 7, raw, exactPolicy(t).Steering.HeadScoreEMA, 1, bound)
	if err == nil || out != nil || head != nil {
		t.Fatalf("input-only word allowance produced an unreserved transcript: bytes=%d error=%v", bound, err)
	}
	if uint64(reflect.TypeFor[HeadEMAMeasurement]().Size()) <= wordBytes {
		t.Fatal("fixed-output witness no longer distinguishes input/output storage")
	}
}

// An exact collection-phase allowance must not restart as a fresh EMA budget.
// The repaired ordinary positive control still executes the same full M8 replay.
func TestReleaseHeadV2AccountingKeepsOneGenuineCollectionBudget(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingKeepsOneGenuineCollectionBudget")
		}
	})
	fixture := newReleaseHeadV2TestFixture(t, 15)
	options := fixture.options(t)
	options.MaxControlBytes = releaseHeadV2AccountingCollectionBudget(t, fixture, options)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, _ := fixture.rpc.counts()
	if err == nil || requests == 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("collection restarted its consumed allowance: requests=%d reads=%d error=%v", requests, *reads, err)
	}
	options = fixture.options(t)
	reads = observeReleaseMeasurementV2SettlementTest(&options)
	result, err = fixture.gather(t.Context(), options)
	if err != nil || *reads == 0 || !reflect.DeepEqual(result.Weights, fixture.measurement.want.SelectedHead) || !reflect.DeepEqual(result.Bindings, fixture.measurement.artifact.Bindings) {
		t.Fatalf("bounded repair bypassed genuine complete replay or exact math: reads=%d error=%v", *reads, err)
	}
	fixture.assertNoEMACommit(t)
}

// A valid persisted census over the current allowance is known before RPC.
func TestReleaseHeadV2AccountingAdmitsPersistedCountBeforeCallbacks(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingAdmitsPersistedCountBeforeCallbacks")
		}
	})
	fixture := newReleaseHeadV2TestFixture(t, 2)
	before := releaseHeadV2AccountingCommit(t, fixture, fixture.measurement.artifact.SubnetEpoch-1, releaseHeadV2AccountingRaw(3), fixture.steerer.cfg.Policy.Steering.HeadScoreEMA)
	options := fixture.options(t)
	options.MaxHeadEntries = 2
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, _ := fixture.rpc.counts()
	if err == nil || requests != 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("known retained census reached callbacks: requests=%d reads=%d error=%v", requests, *reads, err)
	}
	after, readErr := os.ReadFile(fixture.steerer.headEMA.path)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("retained census refusal changed disk: %v", readErr)
	}
}

// The actual store epoch/alpha is independent of a current signed cut. Every
// impossible transition is refused without querying current bindings or proofs.
func TestReleaseHeadV2AccountingAdmitsKnownEpochAndAlphaBeforeCallbacks(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingAdmitsKnownEpochAndAlphaBeforeCallbacks")
		}
	})
	for _, kind := range []string{"regressed", "jumped", "same alpha"} {
		fixture := newReleaseHeadV2TestFixture(t, 2)
		epoch, alpha := fixture.measurement.artifact.SubnetEpoch, fixture.steerer.cfg.Policy.Steering.HeadScoreEMA
		switch kind {
		case "regressed":
			epoch++
		case "jumped":
			epoch -= 2
		case "same alpha":
			replacement := protocol.Rational{Numerator: 1, Denominator: 1}
			if alpha == replacement {
				replacement.Numerator = 0
			}
			alpha = replacement
		}
		before := releaseHeadV2AccountingCommit(t, fixture, epoch, releaseHeadV2AccountingRaw(1), alpha)
		options := fixture.options(t)
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		result, err := fixture.gather(t.Context(), options)
		requests, _ := fixture.rpc.counts()
		if err == nil || requests != 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
			t.Fatalf("%s known state reached callbacks: requests=%d reads=%d error=%v", kind, requests, *reads, err)
		}
		after, readErr := os.ReadFile(fixture.steerer.headEMA.path)
		if readErr != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s refusal changed disk: %v", kind, readErr)
		}
	}
}

// Current keys are learned from actual binding RPC, but the current/history
// union is known before the first complete proof reader can be opened.
func TestReleaseHeadV2AccountingAdmitsRealUnionBeforeProofReplay(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingAdmitsRealUnionBeforeProofReplay")
		}
	})
	fixture := newReleaseHeadV2TestFixture(t, 2)
	releaseHeadV2AccountingCommit(t, fixture, fixture.measurement.artifact.SubnetEpoch-1, releaseHeadV2AccountingRaw(1), fixture.steerer.cfg.Policy.Steering.HeadScoreEMA)
	if len(fixture.measurement.artifact.HeadEMA) != 1 {
		t.Fatal("genuine two-operator fixture no longer shares one live fleet")
	}
	options := fixture.options(t)
	options.MaxHeadEntries = 1
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, batches := fixture.rpc.counts()
	if err == nil || requests == 0 || batches != 2 || *reads != 0 || !strings.Contains(err.Error(), "current/history union exceeds") || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("real current/history union was delayed until proof replay: requests=%d batches=%d reads=%d error=%v", requests, batches, *reads, err)
	}
}

// Already-owned draft data and real retained rational text share one budget;
// each being small enough on its own cannot authorize their combined copy.
func TestReleaseHeadV2AccountingAdmitsRetainedStorageBeforeCallbacks(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingAdmitsRetainedStorageBeforeCallbacks")
		}
	})
	fixture := newReleaseHeadV2TestFixture(t, 2)
	raw := releaseHeadV2AccountingRaw(1)
	large, ok := new(big.Int).SetString(strings.Repeat("7", 256), 10)
	if !ok {
		t.Fatal("large integer fixture is invalid")
	}
	for key := range raw {
		raw[key] = new(big.Rat).SetInt(large)
	}
	before := releaseHeadV2AccountingCommit(t, fixture, fixture.measurement.artifact.SubnetEpoch-1, raw, fixture.steerer.cfg.Policy.Steering.HeadScoreEMA)
	options := fixture.options(t)
	options.MaxControlBytes = releaseHeadV2AccountingCollectionBudget(t, fixture, options)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, _ := fixture.rpc.counts()
	if err == nil || requests != 0 || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("retained string storage acquired another full budget: requests=%d reads=%d error=%v", requests, *reads, err)
	}
	after, readErr := os.ReadFile(fixture.steerer.headEMA.path)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("combined storage refusal changed disk: %v", readErr)
	}
}

// Same-epoch replay still clones its complete transcript and parses output
// rationals; the old retained/input-only budget omitted those new owners.
func TestReleaseHeadV2AccountingReservesSameEpochGeneratedOutput(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingReservesSameEpochGeneratedOutput")
		}
	})
	fixture := newReleaseHeadV2TestFixture(t, 2)
	raw, alpha := releaseHeadV2AccountingRaw(1), fixture.steerer.cfg.Policy.Steering.HeadScoreEMA
	before := releaseHeadV2AccountingCommit(t, fixture, 7, raw, alpha)
	store := fixture.steerer.headEMA
	bound := releaseHeadV2AccountingInputBudget(t, store, raw)
	out, head, err := store.previewForEpochV2(t.Context(), 7, raw, alpha, 8, bound)
	if err == nil || out != nil || head != nil {
		t.Fatalf("same-epoch output bypassed consumed input allowance: %v", err)
	}
	want, wantHead, err := store.PreviewForEpochV2(t.Context(), 7, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	out, head, err = store.previewForEpochV2(t.Context(), 7, raw, alpha, 8, 1024*1024)
	if err != nil || !reflect.DeepEqual(out, want) || !reflect.DeepEqual(head, wantHead) {
		t.Fatalf("same-epoch exact transcript changed: %v", err)
	}
	after, readErr := os.ReadFile(store.path)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("same-epoch admission rewrote history: %v", readErr)
	}
}

// A real prior rational followed by another denominator remains exact, but
// successor growth/output cannot use a second copy of the same allowance.
func TestReleaseHeadV2AccountingReservesSuccessorRationalOutput(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingReservesSuccessorRationalOutput")
		}
	})
	fixture := newReleaseHeadV2TestFixture(t, 2)
	raw, alpha := releaseHeadV2AccountingRaw(1), protocol.Rational{Numerator: 1, Denominator: 3}
	before := releaseHeadV2AccountingCommit(t, fixture, 7, raw, alpha)
	for key := range raw {
		raw[key] = big.NewRat(5, 7)
	}
	store := fixture.steerer.headEMA
	bound := releaseHeadV2AccountingInputBudget(t, store, raw)
	out, head, err := store.previewForEpochV2(t.Context(), 8, raw, alpha, 8, bound)
	if err == nil || out != nil || head != nil {
		t.Fatalf("successor output bypassed consumed input allowance: %v", err)
	}
	want, wantHead, err := store.PreviewForEpochV2(t.Context(), 8, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	out, head, err = store.previewForEpochV2(t.Context(), 8, raw, alpha, 8, 1024*1024)
	if err != nil || !reflect.DeepEqual(out, want) || !reflect.DeepEqual(head, wantHead) {
		t.Fatalf("successor rational formula changed: %v", err)
	}
	after, readErr := os.ReadFile(store.path)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("successor preview rewrote history: %v", readErr)
	}
}

// A separately loaded bounded writer represents an independent durable owner,
// not reentry into the collector's held token. Its real replacement is refused
// before proof reads or at the final witness, without an EMA state mutex.
func TestReleaseHeadV2AccountingRechecksRealHistoryAfterCallbacks(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingRechecksRealHistoryAfterCallbacks")
		}
	})
	for _, phase := range []string{"client key", "proof"} {
		fixture := newReleaseHeadV2TestFixture(t, 2)
		options := fixture.options(t)
		options.MaxHeadEntries = 2
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		writer, err := newReleaseHeadV2EMAStore(t, filepath.Dir(fixture.steerer.headEMA.path))
		if err != nil {
			t.Fatal(err)
		}
		epoch, alpha := fixture.measurement.artifact.SubnetEpoch-1, fixture.steerer.cfg.Policy.Steering.HeadScoreEMA
		_, records, err := writer.PreviewForEpochV2(t.Context(), epoch, releaseHeadV2AccountingRaw(3), alpha)
		if err != nil {
			t.Fatal(err)
		}
		mutated := false
		mutate := func() {
			if mutated {
				return
			}
			mutated = true
			if err := writer.CommitForEpochV2(t.Context(), epoch, records, alpha); err != nil {
				t.Fatal(err)
			}
		}
		if phase == "client key" {
			lookup := fixture.steerer.contexts[9].ClientKey
			fixture.steerer.contexts[9].ClientKey = func(id connect.Id) ([32]byte, bool, error) { mutate(); return lookup(id) }
		} else {
			operator := options.Operators[9]
			read := operator.Measurement.Replay.ReadMetadata
			operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
				mutate()
				return read(ctx, hash, size)
			}
			options.Operators[9] = operator
		}
		result, err := fixture.gather(t.Context(), options)
		if !mutated || err == nil || phase == "client key" && *reads != 0 || phase == "proof" && *reads == 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
			t.Fatalf("%s history mutation escaped fresh admission: changed=%t reads=%d error=%v", phase, mutated, *reads, err)
		}
	}
}

// This helper compares bound arithmetic with independent exact math/big
// values. It is an arithmetic contract, never an acceptance/proof substitute.
func assertReleaseHeadV2AccountingFraction(t *testing.T, label string, bound releaseHeadV2FractionBound, value *big.Rat) {
	t.Helper()
	if uint64(value.Num().BitLen()) > bound.numerator || uint64(value.Denom().BitLen()) > bound.denominator {
		t.Fatalf("%s exact rational exceeds planned bit growth: n=%d/%d d=%d/%d", label, value.Num().BitLen(), bound.numerator, value.Denom().BitLen(), bound.denominator)
	}
	budget := releaseHeadV2Budget{limit: 1024 * 1024}
	if err := bound.reserveText(&budget); err != nil {
		t.Fatal(err)
	}
	if uint64(len(value.Num().String())+len(value.Denom().String())) > budget.output {
		t.Fatalf("%s generated decimal bytes exceed their reservation", label)
	}
}

// Decimal scanning, products, sums and zero branches bound real integer
// growth before parsing/folding. These are numeric contracts, not M8 claims.
func TestReleaseHeadV2AccountingBoundsArithmeticBeforeConstruction(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingBoundsArithmeticBeforeConstruction")
		}
	})
	for _, pair := range []struct{ current, prior RationalJSON }{
		{current: RationalJSON{Numerator: "0", Denominator: "1"}, prior: RationalJSON{Numerator: "0", Denominator: "1"}},
		{current: RationalJSON{Numerator: "5", Denominator: "7"}, prior: RationalJSON{Numerator: "1", Denominator: "3"}},
		{current: RationalJSON{Numerator: strings.Repeat("7", 80), Denominator: "13"}, prior: RationalJSON{Numerator: strings.Repeat("3", 63), Denominator: "11"}},
	} {
		currentBound, err := releaseHeadV2TextBound(pair.current)
		if err != nil {
			t.Fatal(err)
		}
		priorBound, err := releaseHeadV2TextBound(pair.prior)
		if err != nil {
			t.Fatal(err)
		}
		parse := func(encoded RationalJSON) *big.Rat {
			numerator, nOK := new(big.Int).SetString(encoded.Numerator, 10)
			denominator, dOK := new(big.Int).SetString(encoded.Denominator, 10)
			if !nOK || !dOK {
				t.Fatal("independent numeric fixture is invalid")
			}
			return new(big.Rat).SetFrac(numerator, denominator)
		}
		current, prior := parse(pair.current), parse(pair.prior)
		assertReleaseHeadV2AccountingFraction(t, "current", currentBound, current)
		assertReleaseHeadV2AccountingFraction(t, "prior", priorBound, prior)
		for _, alpha := range []protocol.Rational{{Numerator: 0, Denominator: 1}, {Numerator: 1, Denominator: 1}, {Numerator: 1, Denominator: 3}, {Numerator: ^uint64(0) - 1, Denominator: ^uint64(0)}} {
			left, err := currentBound.multiply(alpha.Numerator, alpha.Denominator)
			if err != nil {
				t.Fatal(err)
			}
			right, err := priorBound.multiply(alpha.Denominator-alpha.Numerator, alpha.Denominator)
			if err != nil {
				t.Fatal(err)
			}
			next, err := left.add(right)
			if err != nil {
				t.Fatal(err)
			}
			a := new(big.Rat).SetFrac(new(big.Int).SetUint64(alpha.Numerator), new(big.Int).SetUint64(alpha.Denominator))
			l := new(big.Rat).Mul(a, current)
			r := new(big.Rat).Mul(new(big.Rat).Sub(big.NewRat(1, 1), a), prior)
			assertReleaseHeadV2AccountingFraction(t, "alpha current", left, l)
			assertReleaseHeadV2AccountingFraction(t, "one-minus prior", right, r)
			assertReleaseHeadV2AccountingFraction(t, "next", next, new(big.Rat).Add(l, r))
		}
	}
	for _, value := range []RationalJSON{{Numerator: "", Denominator: "1"}, {Numerator: "-1", Denominator: "1"}, {Numerator: "1", Denominator: "0"}, {Numerator: "1", Denominator: "a"}} {
		if _, err := releaseHeadV2TextBound(value); err == nil {
			t.Fatal("invalid decimal shape passed numeric planning")
		}
	}
	if _, err := releaseHeadV2BitSum(^uint64(0), 1); err == nil {
		t.Fatal("arithmetic growth overflow was accepted")
	}
}

// Every repeated shared prefix is counted by actual map membership. The
// reduced denominator needs distinct multiplicities, not one multiplying
// factor per prefix. This is a numeric projection contract, not 128 M8 trails.
func TestReleaseHeadV2AccountingPlansSharedPrefixesWithoutExponentInflation(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingPlansSharedPrefixesWithoutExponentInflation")
		}
	})
	fleets := map[FleetScoreKey]map[[32]byte]bool{}
	var copied uint64
	for index := 0; index < 4; index++ {
		key := FleetScoreKey{FleetID: [32]byte{0xf1, byte(index)}, Hotkey: [32]byte{0xf2, byte(index)}, Generation: uint64(index + 1), UID: 9}
		prefixes := map[[32]byte]bool{}
		for prefix := 0; prefix < 128; prefix++ {
			prefixes[[32]byte{1, byte(prefix)}] = true
		}
		if index < 2 {
			for prefix := 0; prefix < 4; prefix++ {
				prefixes[[32]byte{2, byte(prefix)}] = true
			}
		}
		prefixes[[32]byte{3, byte(index)}] = true
		copied += uint64(len(prefixes)) * (32 + 1)
		fleets[key] = prefixes
	}
	store, err := newReleaseHeadV2EMAStore(t, newReleaseHeadV2TestStateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	alpha := exactPolicy(t).Steering.HeadScoreEMA
	budget, err := store.admitReleaseHeadV2KnownWithLock(t.Context(), 7, alpha, 4, releaseHeadV2Budget{limit: 1024 * 1024, used: copied})
	if err != nil {
		t.Fatal(err)
	}
	budget, err = admitReleaseHeadV2CurrentWithLock(t.Context(), store, 7, fleets, 4, budget)
	if err != nil {
		t.Fatal(err)
	}
	claims, planned, charged, err := planReleaseHeadV2Raw(t.Context(), fleets, budget)
	if err != nil {
		t.Fatal(err)
	}
	if claims[[32]byte{1}] != 4 || claims[[32]byte{2}] != 2 || claims[[32]byte{3}] != 1 {
		t.Fatal("actual shared-prefix multiplicities changed")
	}
	raw, err := buildReleaseHeadV2Raw(t.Context(), fleets, claims)
	if err != nil || !reflect.DeepEqual(raw, releaseRawHeadScores(fleets)) {
		t.Fatalf("planned raw formula differs from independent existing implementation: %v", err)
	}
	for key, value := range raw {
		assertReleaseHeadV2AccountingFraction(t, "shared prefix score", planned[key], value)
		if planned[key].denominator > 4 {
			t.Fatal("repeated equal multiplicities inflated denominator growth")
		}
	}
	reversed := map[FleetScoreKey]map[[32]byte]bool{}
	keys := orderedReleaseHeadV2Keys(fleets)
	for index := len(keys) - 1; index >= 0; index-- {
		reversed[keys[index]] = fleets[keys[index]]
	}
	againClaims, again, againBudget, err := planReleaseHeadV2Raw(t.Context(), reversed, budget)
	if err != nil || charged != againBudget || !reflect.DeepEqual(claims, againClaims) || !reflect.DeepEqual(planned, again) {
		t.Fatalf("insertion order changed the same exact plan: %v", err)
	}
	budget, err = store.planReleaseHeadV2PreviewWithLock(t.Context(), 7, planned, alpha, charged)
	if err != nil {
		t.Fatal(err)
	}
	out, head, err := store.previewForEpochWithLock(7, raw, alpha)
	if err != nil || len(out) != 1 {
		t.Fatalf("shared UID numeric output differs: %v", err)
	}
	if err := checkReleaseHeadV2Output(t.Context(), out, head, budget); err != nil {
		t.Fatal(err)
	}
}

// This source-helper boundary contract derives the actual complete reservation,
// then proves exact success/one-byte refusal and parity with the real unchanged
// fold. The causal old collector does not call this new helper.
func TestReleaseHeadV2AccountingExactPlannedBudgetKeepsUIDMath(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingExactPlannedBudgetKeepsUIDMath")
		}
	})
	store, err := newReleaseHeadV2EMAStore(t, newReleaseHeadV2TestStateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, bounds := map[FleetScoreKey]*big.Rat{}, map[FleetScoreKey]releaseHeadV2FractionBound{}
	for index, generation := range []uint64{2, 10, 1} {
		key := FleetScoreKey{FleetID: [32]byte{0xf3}, Hotkey: [32]byte{0xf4}, Generation: generation, UID: 9}
		score := big.NewRat(int64(index+1), int64(7+index*4))
		raw[key] = score
		bounds[key] = releaseHeadV2FractionBound{numerator: uint64(score.Num().BitLen()), denominator: uint64(score.Denom().BitLen())}
	}
	alpha := protocol.Rational{Numerator: 1, Denominator: 3}
	plan, err := store.admitReleaseHeadV2KnownWithLock(t.Context(), 7, alpha, 3, releaseHeadV2Budget{limit: 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = admitReleaseHeadV2CurrentWithLock(t.Context(), store, 7, raw, 3, plan)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = store.planReleaseHeadV2PreviewWithLock(t.Context(), 7, bounds, alpha, plan)
	if err != nil || plan.used == 0 || plan.output == 0 {
		t.Fatalf("complete numeric plan is absent: %v", err)
	}
	want, wantHead, err := store.PreviewForEpochV2(t.Context(), 7, raw, alpha)
	if err != nil {
		t.Fatal(err)
	}
	out, head, err := store.previewForEpochV2WithBudget(t.Context(), 7, raw, alpha, 3, releaseHeadV2Budget{limit: plan.used})
	if err != nil || !reflect.DeepEqual(out, want) || !reflect.DeepEqual(head, wantHead) || len(out) != 1 {
		t.Fatalf("exact admitted numeric plan differs from original UID fold: %v", err)
	}
	out, head, err = store.previewForEpochV2WithBudget(t.Context(), 7, raw, alpha, 3, releaseHeadV2Budget{limit: plan.used - 1})
	if err == nil || out != nil || head != nil {
		t.Fatalf("one-byte-short growth plan returned partial output: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out, head, err = store.previewForEpochV2WithBudget(ctx, 7, raw, alpha, 3, releaseHeadV2Budget{limit: plan.used})
	if !errors.Is(err, context.Canceled) || out != nil || head != nil {
		t.Fatalf("canceled numeric admission returned partial output: %v", err)
	}
	if len(store.values) != 0 || store.lastSubnetEpoch != nil {
		t.Fatal("numeric boundary preview committed live state")
	}
}

// A completed result has more owners than its transcript: both score slices,
// UID lists, bindings and membership maps count. The exact byte oracle here
// uses the documented control-payload convention, not serialized/RSS bytes.
func TestReleaseHeadV2AccountingMeasuresExactCompletedOutput(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("HEAD-ACCOUNTING-v1 PASS TestReleaseHeadV2AccountingMeasuresExactCompletedOutput")
		}
	})
	fixture := newReleaseHeadV2TestFixture(t, 2)
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Weights) == 0 || len(result.Eligible) == 0 || len(result.HeadEMA) == 0 {
		t.Fatal("genuine output witness lost its positive rational owners")
	}
	exact := uint64(reflect.TypeFor[releaseHeadResult]().Size())
	remaining := uint64(1024 * 1024)
	for _, value := range []any{result.Inputs, result.Bindings, result.HeadEMA, result.StaleBindings, result.EligibleUIDs, result.SelectedUIDs, result.RejectedUIDs} {
		if err := releaseMeasurementV2ControlStorage(t.Context(), reflect.ValueOf(value), &remaining); err != nil {
			t.Fatal(err)
		}
	}
	exact += 1024*1024 - remaining
	for _, rows := range [][]ExactWeightInput{result.Weights, result.Eligible} {
		exact += uint64(len(rows)) * uint64(reflect.TypeFor[ExactWeightInput]().Size())
		for _, row := range rows {
			exact += uint64(reflect.TypeFor[big.Rat]().Size()) + (uint64(len(row.Score.Num().Bits()))+uint64(len(row.Score.Denom().Bits())))*uint64(reflect.TypeFor[big.Word]().Size())
		}
	}
	exact += uint64(len(result.Bound))*(8+uint64(reflect.TypeFor[map[connect.Id]bool]().Size())) + uint64(len(result.Controlled))*(2+1)
	for _, members := range result.Bound {
		exact += uint64(len(members)) * (16 + 1)
	}
	if err := checkReleaseHeadV2Result(t.Context(), result, releaseHeadV2Budget{limit: exact, used: exact}); err != nil {
		t.Fatalf("exact completed result was refused: %v", err)
	}
	if err := checkReleaseHeadV2Result(t.Context(), result, releaseHeadV2Budget{limit: exact - 1, used: exact - 1}); err == nil {
		t.Fatal("one-byte-short completed result allowance passed")
	}
	if err := checkReleaseHeadV2Result(t.Context(), result, releaseHeadV2Budget{limit: exact - 1, used: exact}); err == nil {
		t.Fatal("completed result escaped combined budget invariant")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := checkReleaseHeadV2Result(ctx, result, releaseHeadV2Budget{limit: exact, used: exact}); !errors.Is(err, context.Canceled) {
		t.Fatalf("completed output ignored cancellation: %v", err)
	}
	fixture.assertNoEMACommit(t)
}

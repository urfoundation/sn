//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
)

// Only a complete success is remembered, under the exact inputs it had.
func TestReleaseStartupVerifiedRemembersOnlyCompleteSuccess(t *testing.T) {
	t.Parallel()
	owner := newReleaseStartupVerified()
	runs := 0
	verify := func() error { runs++; return nil }
	for range 2 {
		if err := owner.once("unit", map[string]int{"a": 1}, verify); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 || owner.skippedUnits() != 1 {
		t.Fatalf("an identical verified unit ran again: runs=%d skipped=%d", runs, owner.skippedUnits())
	}
	if err := owner.once("unit", map[string]int{"a": 2}, verify); err != nil || runs != 2 {
		t.Fatalf("changed inputs reused another unit's verdict: runs=%d %v", runs, err)
	}
	if err := owner.once("other", map[string]int{"a": 1}, verify); err != nil || runs != 3 {
		t.Fatalf("another kind reused the verdict: runs=%d %v", runs, err)
	}
	failure := errors.New("synthetic refusal")
	for range 2 {
		if err := owner.once("failing", 1, func() error { runs++; return failure }); !errors.Is(err, failure) {
			t.Fatalf("a failure was not returned: %v", err)
		}
	}
	if runs != 5 {
		t.Fatalf("a failed unit was remembered: runs=%d", runs)
	}
	for range 2 {
		if err := owner.onceIf("custody", 1, func() (bool, error) { runs++; return false, nil }); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 7 {
		t.Fatalf("a success that may not be remembered was skipped: runs=%d", runs)
	}
	if err := owner.once("unencodable", func() {}, verify); err != nil || runs != 8 {
		t.Fatalf("unencodable inputs did not verify: runs=%d %v", runs, err)
	}
	if err := owner.once("unencodable", func() {}, verify); err != nil || runs != 9 {
		t.Fatalf("unencodable inputs were remembered: runs=%d %v", runs, err)
	}
	var absent *releaseStartupVerified
	for range 2 {
		if err := absent.once("unit", map[string]int{"a": 1}, verify); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 11 {
		t.Fatalf("an absent owner skipped verification: runs=%d", runs)
	}
	if releaseStartupVerifiedFrom(t.Context()) != nil || releaseStartupVerifiedFrom(withReleaseStartupVerified(t.Context(), owner)) != owner {
		t.Fatal("the owner is not carried by exactly its context")
	}
}

// The eec99590 failure: one historical read outlasts its budget late in the
// stage. The retry waits as before but skips every unit it already verified.
func TestReleaseStartupVerifiedStageRetryResumes(t *testing.T) {
	t.Parallel()
	timeout := fmt.Errorf("eth_call batch at canonical block 9270543 (0x01): %w", &url.Error{Op: "Post", URL: "http://172.28.208.185:9944", Err: context.DeadlineExceeded})
	if !retryableProductionSteeringRead(timeout) {
		t.Fatal("the fixture timeout is not a retryable startup read")
	}
	run := func(ctx context.Context) (int, map[int]int, error) {
		attempts, verifications := 0, map[int]int{}
		err := awaitProductionStartupStage(ctx, &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}, nil, "disk and intent history", func(stage context.Context) error {
			attempts++
			for unit := range 6 {
				if err := releaseStartupVerifiedFrom(stage).once("test", unit, func() error {
					verifications[unit]++
					if attempts == 1 && unit == 4 {
						return timeout
					}
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		})
		return attempts, verifications, err
	}
	attempts, verifications, err := run(withReleaseStartupVerified(t.Context(), newReleaseStartupVerified()))
	if err != nil || attempts != 2 {
		t.Fatalf("stage did not retry once: attempts=%d %v", attempts, err)
	}
	for unit := range 6 {
		if want := map[bool]int{true: 2, false: 1}[unit == 4]; verifications[unit] != want {
			t.Fatalf("unit %d verified %d times, want %d", unit, verifications[unit], want)
		}
	}
	// Without the owner the retry starts again from nothing.
	attempts, verifications, err = run(t.Context())
	if err != nil || attempts != 2 || verifications[0] != 2 || verifications[3] != 2 {
		t.Fatalf("the plain stage changed: attempts=%d verifications=%v %v", attempts, verifications, err)
	}
}

// A fresh intent store reuses exactly what the in-process memo would.
func TestReleaseStartupVerifiedIntentPrefixFollowsMemoRules(t *testing.T) {
	t.Parallel()
	base := intentV2VerifiedHistoryTestEntries(10, 11, 12, 13, 14)
	owner := newReleaseStartupVerified()
	owner.rememberIntents(intentV2VerifiedHistoryTestDigests(t, base), true)
	clone := func(entries []*SteeringIntent) []*SteeringIntent {
		out := make([]*SteeringIntent, len(entries))
		for index, entry := range entries {
			copy := *entry
			out[index] = &copy
		}
		return out
	}
	appended := append(clone(base), nativeGapBridgeTestIntent(15, "pending"))
	appended[4].Status = "pending"
	lastChanged := clone(base)
	lastChanged[4].Status = "finalized"
	middleChanged := clone(base)
	middleChanged[1].SettlementEpoch = 2
	for _, test := range []struct {
		name string
		all  []*SteeringIntent
		want int
	}{
		{"identical", clone(base), 5},
		{"appended successor", appended, 5},
		{"current intent changed", lastChanged, 4},
		{"earlier entry changed", middleChanged, 0},
		{"last entry removed", clone(base)[:4], 0},
		{"entry inserted before", append([]*SteeringIntent{nativeGapBridgeTestIntent(9, "applied")}, clone(base)...), 0},
	} {
		if got := owner.intentPrefix(intentV2VerifiedHistoryTestDigests(t, test.all)); got != test.want {
			t.Fatalf("%s: reusable prefix %d, want %d", test.name, got, test.want)
		}
	}
	// A failed read's verified prefix never shortens a chain that extends it,
	// and a later complete read replaces the chain.
	owner.rememberIntents(intentV2VerifiedHistoryTestDigests(t, base)[:3], false)
	if got := owner.intentPrefix(intentV2VerifiedHistoryTestDigests(t, base)); got != 5 {
		t.Fatalf("a failed read shortened the verified chain: %d", got)
	}
	owner.rememberIntents(intentV2VerifiedHistoryTestDigests(t, middleChanged)[:3], false)
	if got := owner.intentPrefix(intentV2VerifiedHistoryTestDigests(t, middleChanged)); got != 3 {
		t.Fatalf("a failed read's verified prefix was not resumed: %d", got)
	}
	owner.rememberIntents(intentV2VerifiedHistoryTestDigests(t, base)[:4], true)
	if got := owner.intentPrefix(intentV2VerifiedHistoryTestDigests(t, base)[:4]); got != 4 {
		t.Fatalf("a complete read did not replace the chain: %d", got)
	}
	if got := (*releaseStartupVerified)(nil).intentPrefix(intentV2VerifiedHistoryTestDigests(t, base)); got != 0 {
		t.Fatalf("an absent owner reused %d entries", got)
	}
}

// The disk stage: a second start in the same process reads no public cut
// object and reaches the same state; without the owner it replays again.
func TestReleaseStartupVerifiedSkipsRepeatedStartupReplay(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	fixture.ordinary(t, 0, 1, false)
	fixture.trail(t, 0)
	fixture.ordinary(t, 0, 2, false)
	reads := func() int {
		total := 0
		for _, store := range fixture.stores {
			_, _, count := store.snapshot()
			total += count
		}
		return total
	}
	owner := newReleaseStartupVerified()
	ctx := withReleaseStartupVerified(t.Context(), owner)
	fixture.reopen(t)
	before := reads()
	if err := fixture.start(ctx, attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	full := reads() - before
	want := releaseStartupV2TestDiskImages(t, fixture.disk)
	if full == 0 || owner.skippedUnits() == 0 {
		t.Fatalf("first start did not replay, or its own reconciliation repeated a verified replay: reads=%d skipped=%d", full, owner.skippedUnits())
	}
	fixture.reopen(t)
	before = reads()
	if err := fixture.start(ctx, attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	if resumed := reads() - before; resumed != 0 {
		t.Fatalf("a repeated start in one process read %d public objects again", resumed)
	}
	for index, image := range releaseStartupV2TestDiskImages(t, fixture.disk) {
		if !bytes.Equal(image, want[index]) {
			t.Fatal("the resumed start reached another state")
		}
	}
	fixture.reopen(t)
	before = reads()
	if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	if plain := reads() - before; plain < full {
		t.Fatalf("a start without the owner skipped public replay: reads=%d of %d", plain, full)
	}
}

// The steering stage: a new intent store, as after a failed startup attempt
// or at the next start, resumes the verified prefix and replays nothing.
func TestIntentV2FreshStoreResumesVerifiedStartupPrefix(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	store := fixture.steerer.intents
	intent, err := store.beginV2(t.Context(), *fixture.intent)
	if err != nil {
		t.Fatalf("actual nonempty V2 begin: %v", err)
	}
	admission := fixture.production.operator.measurement.admission
	read := func(ctx context.Context) (native, objects int) {
		t.Helper()
		owner, err := newReleaseIntentStoreV2(fixture.runtime)
		if err != nil {
			t.Fatal(err)
		}
		calls, requests := len(admission.calls), fixture.artifactReads.Load()
		current, err := owner.currentV2(ctx)
		if err != nil || current == nil || current.VectorHash != intent.VectorHash {
			t.Fatalf("actual V2 read: %v", err)
		}
		return len(admission.calls) - calls, int(fixture.artifactReads.Load() - requests)
	}
	verified := newReleaseStartupVerified()
	ctx := withReleaseStartupVerified(t.Context(), verified)
	fullNative, fullObjects := read(ctx)
	resumedNative, resumedObjects := read(ctx)
	t.Logf("fresh store: complete read %d native calls and %d attempt-object requests; resumed read %d and %d", fullNative, fullObjects, resumedNative, resumedObjects)
	if fullObjects == 0 || resumedObjects != 0 || resumedNative >= fullNative {
		t.Fatalf("a fresh store repeated verified work: native %d of %d, objects %d of %d", resumedNative, fullNative, resumedObjects, fullObjects)
	}
	if native, objects := read(t.Context()); native != fullNative || objects != fullObjects {
		t.Fatalf("a store without the owner reused a verdict: native %d, objects %d", native, objects)
	}
	// The only entry changed: nothing is reused.
	if err := store.updateV2(t.Context(), intent.VectorHash, "pending", nil); err != nil {
		t.Fatalf("actual V2 update: %v", err)
	}
	if native, objects := read(ctx); native != fullNative || objects != fullObjects {
		t.Fatalf("a changed entry was not verified in full: native %d, objects %d", native, objects)
	}
}

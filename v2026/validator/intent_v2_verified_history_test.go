//go:build linux || darwin

package validator

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Distinct inert entries; only their canonical bytes and order matter here.
func intentV2VerifiedHistoryTestEntries(epochs ...uint64) []*SteeringIntent {
	all := make([]*SteeringIntent, len(epochs))
	for index, epoch := range epochs {
		all[index] = nativeGapBridgeTestIntent(epoch, "applied")
	}
	all[len(all)-1].Status = "pending"
	return all
}

func intentV2VerifiedHistoryTestDigests(t *testing.T, all []*SteeringIntent) [][32]byte {
	t.Helper()
	digests, ok := releaseIntentV2EntryDigests(all)
	if !ok {
		t.Fatal("inert entries were not digestible")
	}
	return digests
}

// The memo keeps its options only for the tail it was given.
func intentV2VerifiedHistoryTestMemo(t *testing.T, authority releaseIntentV2VerifiedAuthority, all []*SteeringIntent, withOptions ...int) *releaseIntentV2Owner {
	t.Helper()
	owner := &releaseIntentV2Owner{}
	options := map[int]ReleaseMeasurementV2Options{}
	for _, index := range withOptions {
		options[index] = ReleaseMeasurementV2Options{MaxOperators: uint64(index + 1)}
	}
	owner.rememberVerifiedHistory(authority, intentV2VerifiedHistoryTestDigests(t, all), options)
	return owner
}

func TestIntentV2VerifiedHistoryReusesOnlyAnUnchangedPrefix(t *testing.T) {
	t.Parallel()
	authority := releaseIntentV2VerifiedAuthority{retainedStartup: false}
	base := intentV2VerifiedHistoryTestEntries(10, 11, 12, 13, 14)
	owner := intentV2VerifiedHistoryTestMemo(t, authority, base, 0, 1, 2, 3, 4)
	clone := func(entries []*SteeringIntent) []*SteeringIntent {
		out := make([]*SteeringIntent, len(entries))
		for index, entry := range entries {
			copy := *entry
			out[index] = &copy
		}
		return out
	}
	identical := clone(base)
	appended := append(clone(base), nativeGapBridgeTestIntent(15, "pending"))
	appended[4].Status = "pending"
	lastChanged := clone(base)
	lastChanged[4].Status = "finalized"
	lastChangedAppended := append(clone(lastChanged), nativeGapBridgeTestIntent(15, "pending"))
	middleChanged := clone(base)
	middleChanged[1].SettlementEpoch = 2
	reordered := clone(base)
	reordered[1], reordered[2] = reordered[2], reordered[1]
	truncated := clone(base)[:4]
	shifted := append([]*SteeringIntent{nativeGapBridgeTestIntent(9, "applied")}, clone(base)...)
	for _, test := range []struct {
		name string
		all  []*SteeringIntent
		want int
	}{
		{"identical", identical, 5},
		{"appended successor", appended, 5},
		{"current intent changed", lastChanged, 4},
		{"current changed and successor appended", lastChangedAppended, 4},
		{"earlier entry changed", middleChanged, 0},
		{"entries reordered", reordered, 0},
		{"last entry removed", truncated, 0},
		{"entry inserted before", shifted, 0},
		{"history emptied", nil, 0},
	} {
		digests, ok := releaseIntentV2EntryDigests(test.all)
		if !ok {
			t.Fatalf("%s: entries were not digestible", test.name)
		}
		if got := owner.verified.reusablePrefix(authority, digests); got != test.want {
			t.Fatalf("%s: reusable prefix %d, want %d", test.name, got, test.want)
		}
	}
	// Reuse needs the predecessor's options for the lineage it replays next.
	withoutPredecessor := intentV2VerifiedHistoryTestMemo(t, authority, base, 4)
	if got := withoutPredecessor.verified.reusablePrefix(authority, intentV2VerifiedHistoryTestDigests(t, lastChanged)); got != 0 {
		t.Fatalf("changed current reused a prefix without its predecessor's options: %d", got)
	}
	if got := withoutPredecessor.verified.reusablePrefix(authority, intentV2VerifiedHistoryTestDigests(t, appended)); got != 5 {
		t.Fatalf("appended successor lost its memoized predecessor: %d", got)
	}
	// One memoized entry that changes has nothing left to reuse.
	single := intentV2VerifiedHistoryTestMemo(t, authority, base[:1], 0)
	changedSingle := clone(base[:1])
	changedSingle[0].Status = "finalized"
	if got := single.verified.reusablePrefix(authority, intentV2VerifiedHistoryTestDigests(t, changedSingle)); got != 0 {
		t.Fatalf("a changed sole entry reused itself: %d", got)
	}
	// An absent memo reuses nothing.
	if got := (*releaseIntentV2VerifiedHistory)(nil).reusablePrefix(authority, intentV2VerifiedHistoryTestDigests(t, base)); got != 0 {
		t.Fatalf("absent memo reused %d entries", got)
	}
}

func TestIntentV2VerifiedHistoryDigestsBindPositionAndPredecessors(t *testing.T) {
	t.Parallel()
	base := intentV2VerifiedHistoryTestDigests(t, intentV2VerifiedHistoryTestEntries(10, 11, 12))
	again := intentV2VerifiedHistoryTestDigests(t, intentV2VerifiedHistoryTestEntries(10, 11, 12))
	if !slices.Equal(base, again) {
		t.Fatal("equal histories produced different digests")
	}
	changed := intentV2VerifiedHistoryTestEntries(10, 11, 12)
	changed[0].SettlementEpoch = 2
	later := intentV2VerifiedHistoryTestDigests(t, changed)
	for index := range base {
		if later[index] == base[index] {
			t.Fatalf("a changed first entry left digest %d unchanged", index)
		}
	}
	if _, ok := releaseIntentV2EntryDigests([]*SteeringIntent{nil}); ok {
		t.Fatal("a nil entry was digestible")
	}
}

// The bridge, adoption and startup history are part of every lineage verdict.
// A memo made under one never reaches a read under another, so a bridged
// edge is only reused while that exact bridge owner remains in force.
func TestIntentV2VerifiedHistoryFollowsAuthority(t *testing.T) {
	t.Parallel()
	const L, F = nativeGapBridgeTestLast, nativeGapBridgeTestFirst
	bridge := nativeGapBridgeTest()
	all := []*SteeringIntent{nativeGapBridgeTestIntent(L-1, "applied"), nativeGapBridgeTestIntent(L, "applied"), nativeGapBridgeTestIntent(F, "applied"), nativeGapBridgeTestIntent(F+1, "pending")}
	owner, err := bridge.matchHistory(t.Context(), all)
	if err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{nativeGapBridge: owner}
	bridged := &releaseIntentV2Owner{runtime: &releaseRuntimeV2{history: history}, nativeGapBridge: owner}
	authority := bridged.verifiedAuthority()
	digests := intentV2VerifiedHistoryTestDigests(t, all)
	bridged.rememberVerifiedHistory(authority, digests, map[int]ReleaseMeasurementV2Options{2: {}, 3: {}})
	if got := bridged.verified.reusablePrefix(bridged.verifiedAuthority(), digests); got != len(all) {
		t.Fatalf("unchanged bridged history was not reused: %d", got)
	}
	// The reused edge still passes the same cheap successor rule the read applies.
	if err := validateSteeringIntentSuccessorWithGapsV2(all[1], all[2], bridged.allowsIntentGap(all[1], all[2])); err != nil {
		t.Fatalf("bridged edge refused under its own owner: %v", err)
	}
	unbridged := &releaseIntentV2Owner{runtime: bridged.runtime}
	if got := bridged.verified.reusablePrefix(unbridged.verifiedAuthority(), digests); got != 0 {
		t.Fatalf("a memo made under the bridge survived its removal: %d", got)
	}
	if err := validateSteeringIntentSuccessorWithGapsV2(all[1], all[2], unbridged.allowsIntentGap(all[1], all[2])); err == nil {
		t.Fatal("unbridged read admitted the missed native epochs")
	}
	other, err := bridge.matchHistory(t.Context(), all)
	if err != nil {
		t.Fatal(err)
	}
	replaced := &releaseIntentV2Owner{runtime: bridged.runtime, nativeGapBridge: other}
	if got := bridged.verified.reusablePrefix(replaced.verifiedAuthority(), digests); got != 0 {
		t.Fatalf("a memo survived a different bridge owner: %d", got)
	}
	retained := &releaseIntentV2Owner{nativeGapBridge: owner, runtime: &releaseRuntimeV2{history: &releaseEvidenceV2StartupHistory{nativeGapBridge: owner, retainedStartup: true}}}
	if got := bridged.verified.reusablePrefix(retained.verifiedAuthority(), digests); got != 0 {
		t.Fatalf("a memo survived a different startup history: %d", got)
	}
}

// The actual owner: a repeated read of an unchanged history makes no public
// attempt-object request and far fewer native calls, a changed current intent
// is verified again, and a fresh owner verifies everything.
func TestIntentV2VerifiedHistorySkipsRepeatedReplay(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	store := fixture.steerer.intents
	intent, err := store.beginV2(t.Context(), *fixture.intent)
	if err != nil {
		t.Fatalf("actual nonempty V2 begin: %v", err)
	}
	admission := fixture.production.operator.measurement.admission
	read := func(owner *IntentStore) (native, objects int) {
		t.Helper()
		calls, requests := len(admission.calls), fixture.artifactReads.Load()
		current, err := owner.currentV2(t.Context())
		if err != nil || current == nil || current.VectorHash != intent.VectorHash {
			t.Fatalf("actual V2 read: %v", err)
		}
		return len(admission.calls) - calls, int(fixture.artifactReads.Load() - requests)
	}
	fresh := func() *IntentStore {
		t.Helper()
		owner, err := newReleaseIntentStoreV2(fixture.runtime)
		if err != nil {
			t.Fatal(err)
		}
		return owner
	}
	owner := fresh()
	fullNative, fullObjects := read(owner)
	reusedNative, reusedObjects := read(owner)
	t.Logf("one retained intent: complete read %d native calls and %d attempt-object requests; repeated read %d and %d", fullNative, fullObjects, reusedNative, reusedObjects)
	if fullNative == 0 {
		t.Fatal("the complete read made no native call")
	}
	if reusedObjects != 0 || reusedNative >= fullNative {
		t.Fatalf("repeated read repeated verification: native %d of %d, objects %d of %d", reusedNative, fullNative, reusedObjects, fullObjects)
	}
	// A changed current intent is verified again in full, then reused.
	if err := owner.updateV2(t.Context(), intent.VectorHash, "pending", nil); err != nil {
		t.Fatalf("actual V2 update: %v", err)
	}
	changedNative, changedObjects := read(owner)
	if changedNative != fullNative || changedObjects != fullObjects {
		t.Fatalf("changed current was not verified in full: native %d of %d, objects %d of %d", changedNative, fullNative, changedObjects, fullObjects)
	}
	if native, objects := read(owner); objects != 0 || native != reusedNative {
		t.Fatalf("verified changed current was not reused: native %d, objects %d", native, objects)
	}
	// A new owner, as at startup, verifies everything.
	if native, objects := read(fresh()); native != fullNative || objects != fullObjects {
		t.Fatalf("fresh owner reused another owner's verdict: native %d, objects %d", native, objects)
	}
	// A changed gap authority discards the memo.
	owner.v2.nativeGapBridge = &releaseNativeGapBridgeV2{}
	if native, objects := read(owner); native != fullNative || objects != fullObjects {
		t.Fatalf("memo survived a changed bridge authority: native %d, objects %d", native, objects)
	}
	// Reuse never skips the retained files: a reused entry whose measurement
	// no longer matches the hash its bytes pin is refused.
	if owner.v2.verified == nil {
		t.Fatal("a complete read left no memo")
	}
	path := intent.MeasurementArtifactPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(fixture.runtime.cfg.StateDir, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-2] ^= 1
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	calls, requests := len(admission.calls), fixture.artifactReads.Load()
	if _, err := owner.currentV2(t.Context()); err == nil {
		t.Fatal("reused entry accepted a changed retained measurement")
	}
	if len(admission.calls) != calls || fixture.artifactReads.Load() != requests {
		t.Fatal("the changed retained measurement was not refused by the local reuse path")
	}
}

// A reused entry's artifact is decoded only when its fields are needed. The
// undecoded read still pins the exact retained bytes through custody and
// still checks the source commitment and hotkey.
func TestIntentV2VerifiedMeasurementDecodesOnlyOnRequest(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	store := fixture.steerer.intents
	intent, err := store.beginV2(t.Context(), *fixture.intent)
	if err != nil {
		t.Fatalf("actual nonempty V2 begin: %v", err)
	}
	read := func(decode bool) (*ReleaseMeasurementArtifact, []byte, error) {
		t.Helper()
		custody := &releaseEvidenceV2StartupReferences{remaining: fixture.runtime.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
		artifact, measurement, _, err := store.readVerifiedMeasurementV2(t.Context(), custody, intent, nil, decode, false)
		return artifact, measurement, errors.Join(err, custody.close())
	}
	decoded, decodedBytes, err := read(true)
	if err != nil || decoded == nil || decoded.SubnetEpoch != intent.SubnetEpoch {
		t.Fatalf("decoded verified read: %v", err)
	}
	undecoded, undecodedBytes, err := read(false)
	if err != nil || undecoded != nil || !bytes.Equal(undecodedBytes, decodedBytes) {
		t.Fatalf("undecoded verified read: artifact %v, same bytes %t, %v", undecoded != nil, bytes.Equal(undecodedBytes, decodedBytes), err)
	}
	// A hotkey other than the prepared one is still refused without decoding.
	changed := *intent
	prepared := *intent.Prepared
	prepared.HotkeyHex = releaseHex32([32]byte{1})
	changed.Prepared = &prepared
	custody := &releaseEvidenceV2StartupReferences{remaining: fixture.runtime.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	_, _, _, err = store.readVerifiedMeasurementV2(t.Context(), custody, &changed, nil, false, false)
	if err = errors.Join(err, custody.close()); err == nil {
		t.Fatal("undecoded verified read accepted a different prepared hotkey")
	}
	// Changed retained bytes no longer match the content address.
	path := intent.MeasurementArtifactPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(fixture.runtime.cfg.StateDir, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-2] ^= 1
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := read(false); err == nil {
		t.Fatal("undecoded verified read accepted a changed retained measurement")
	}
}

// An entry of a history read checks only the custody it acquired: the intent
// store's reused and complete reads, and startup's decision sources, which
// share one custody across every retained intent. Identical bytes under a new
// inode at an earlier entry's path are visible only to that earlier owner's
// check. The next entry's read no longer walks every earlier owner again,
// which made each history read quadratic in its entries, and the read owner's
// final check still refuses the replaced file.
func TestIntentV2EntryReadChecksOnlyItsOwnCustody(t *testing.T) {
	fixture := newProductionContinuationTestFixture(t)
	store := fixture.steerer.intents
	intent, err := store.beginV2(t.Context(), *fixture.intent)
	if err != nil {
		t.Fatalf("actual nonempty V2 begin: %v", err)
	}
	path := intent.MeasurementArtifactPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(fixture.runtime.cfg.StateDir, path)
	}
	bounds := fixture.runtime.cfg.EvidenceV2.Bounds
	measurement, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(t.Context(), measurement, bounds.MaxArtifactBytes, bounds.MaxOperators)
	if err != nil {
		t.Fatal(err)
	}
	history := fixture.runtime.history
	for _, read := range []struct {
		name  string
		entry func(*releaseEvidenceV2StartupReferences) error
	}{
		{name: "reused", entry: func(custody *releaseEvidenceV2StartupReferences) error {
			_, _, _, err := store.readVerifiedMeasurementV2(t.Context(), custody, intent, nil, false, false)
			return err
		}},
		{name: "complete", entry: func(custody *releaseEvidenceV2StartupReferences) error {
			_, _, _, _, err := store.readMeasurementV2(t.Context(), custody, intent)
			return err
		}},
		{name: "startup decision sources", entry: func(custody *releaseEvidenceV2StartupReferences) error {
			// Startup reads each intent's files into the shared custody first.
			if _, err := history.readContentReference(t.Context(), custody, intent.MeasurementArtifactPath, intent.MeasurementArtifactHash, intent.MeasurementArtifactSize, bounds.MaxArtifactBytes, false); err != nil {
				return err
			}
			_, err := history.readIntentDecisionSourcesV2(t.Context(), fixture.runtime.chain, fixture.runtime.native, releaseRuntimeIdentityV2(&fixture.runtime.cfg), intent, artifact, custody)
			return err
		}},
	} {
		custody := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes}
		if err := read.entry(custody); err != nil {
			t.Fatalf("%s earlier entry read: %v", read.name, err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		replacement := path + ".synthetic-replacement"
		if err := os.WriteFile(replacement, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, path); err != nil {
			t.Fatal(err)
		}
		if err := read.entry(custody); err != nil {
			t.Errorf("%s next entry read checked the earlier entry's custody again: %v", read.name, err)
		}
		if err := custody.check(); err == nil {
			t.Errorf("%s read owner's final check accepted a replaced earlier file", read.name)
		}
		if err := custody.close(); err == nil {
			t.Errorf("%s read owner's close accepted a replaced earlier file", read.name)
		}
	}
}

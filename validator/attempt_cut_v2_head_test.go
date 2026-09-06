//go:build linux || darwin

package validator

// The head projection uses genuine signed M8 streams. Its independent oracle
// is the existing legacy generation-attributed claim extractor, not a provider
// hash total that could incorrectly follow a provider through rebinding.

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/urnetwork/connect"
)

// Converts an already authenticated fixture binding without changing its key.
func attemptCutV2HeadTestFleetKey(t *testing.T, binding AttemptBinding) FleetScoreKey {
	t.Helper()
	fleet, err := canonicalAttemptHex32("fixture fleet", binding.FleetID, false)
	if err != nil {
		t.Fatal(err)
	}
	hotkey, err := canonicalAttemptHex32("fixture hotkey", binding.Hotkey, false)
	if err != nil {
		t.Fatal(err)
	}
	return FleetScoreKey{FleetID: fleet, Hotkey: hotkey, Generation: binding.Generation, UID: binding.UID}
}

// Each call owns a new current-state map and a fresh actual replay directory.
// These explicit small test bounds are not production defaults.
func attemptCutV2HeadTestOptions(t *testing.T, fixture *attemptCutV2StatsTestFixture) AttemptCutV2HeadOptions {
	t.Helper()
	currentKVs := map[connect.Id]FleetScoreKey{}
	for _, record := range fixture.seal.recordTs {
		for _, assignment := range record.Assignments {
			binding := assignment.Binding
			if binding.Active && binding.UIDFound {
				currentKVs[binding.ClientID] = attemptCutV2HeadTestFleetKey(t, binding)
			}
		}
	}
	return AttemptCutV2HeadOptions{CurrentBindingKVs: currentKVs, MaxProviders: 32, MaxFleetPrefixes: 64, Replay: fixture.options(t, fixture.measurement).Replay}
}

// Legacy claims retain exact sequence, binding and prefix provenance. The
// tuple lookup is independent of the compact projection's field comparisons.
func attemptCutV2HeadTestExpected(t *testing.T, fixture *attemptCutV2StatsTestFixture, currentKVs map[connect.Id]FleetScoreKey, firstSequence uint64) map[FleetScoreKey]map[[32]byte]bool {
	t.Helper()
	expectedKVs := map[FleetScoreKey]map[[32]byte]bool{}
	for _, key := range currentKVs {
		expectedKVs[key] = map[[32]byte]bool{}
	}
	claims, err := AttemptCutEgressClaims(&AttemptLedgerCut{Records: fixture.seal.recordTs, EgressFirstSequence: firstSequence})
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range claims {
		key := attemptCutV2HeadTestFleetKey(t, claim.Binding)
		if current, exists := currentKVs[claim.Binding.ClientID]; exists && current == key {
			hash, err := canonicalAttemptHex32("fixture egress", claim.EgressIPHash, false)
			if err != nil {
				t.Fatal(err)
			}
			expectedKVs[key][hash] = true
		}
	}
	return expectedKVs
}

// Every signed record replays, including the real failed terminal, while only
// confirmed non-seed prefixes with the current binding contribute to the head.
func TestAttemptCutV2HeadMatchesRealLegacyClaims(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	options := attemptCutV2HeadTestOptions(t, fixture)
	expected := attemptCutV2HeadTestExpected(t, fixture, options.CurrentBindingKVs, fixture.cut.Context.EgressFirstSequence)
	fleets, replayed, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil || !reflect.DeepEqual(fleets, expected) || replayed.Records.ItemCount != 18 || replayed.CompleteCount != 2 || replayed.FailedCount != 1 {
		t.Fatalf("compact head differs from genuine legacy claims: %+v error=%v", replayed, err)
	}
	prefixes := 0
	for _, hashes := range fleets {
		prefixes += len(hashes)
	}
	if prefixes == 0 || !reflect.DeepEqual(releaseRawHeadScores(fleets), releaseRawHeadScores(expected)) {
		t.Fatal("real prefix replay lost its positive exact head-score input")
	}
}

// Current identity cannot inherit a prefix proved for another fleet, hotkey,
// generation or UID. An absent current binding also earns exactly zero.
func TestAttemptCutV2HeadRejectsRebindingInheritance(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	for field := 0; field < 5; field++ {
		options := attemptCutV2HeadTestOptions(t, fixture)
		for id, key := range options.CurrentBindingKVs {
			switch field {
			case 0:
				key.FleetID[0] ^= 1
			case 1:
				key.Hotkey[0] ^= 1
			case 2:
				key.Generation++
			case 3:
				key.UID = 0
			case 4:
				delete(options.CurrentBindingKVs, id)
				continue
			}
			options.CurrentBindingKVs[id] = key
		}
		fleets, replayed, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
		if err != nil || replayed.CompleteCount != 2 || replayed.FailedCount != 1 {
			t.Fatalf("changed current binding %d skipped full replay or became invalid: %v", field, err)
		}
		for _, hashes := range fleets {
			if len(hashes) != 0 {
				t.Fatalf("changed current binding %d inherited signed prior-owner prefixes", field)
			}
		}
	}
}

// The authenticated native cursor excludes earlier prefixes without dropping
// their records or settlement accounting from complete stream verification.
func TestAttemptCutV2HeadUsesSignedNativeCursor(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	for _, first := range []uint64{9, fixture.cut.LastSequence + 1} {
		cut := fixture.cut
		cut.Context.EgressFirstSequence = first
		var err error
		cut.Signature, err = cut.Sign(fixture.seal.key, fixture.seal.bounds)
		if err != nil {
			t.Fatal(err)
		}
		options := attemptCutV2HeadTestOptions(t, fixture)
		fleets, replayed, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), cut, cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
		if err != nil || replayed.Records.ItemCount != 18 || !reflect.DeepEqual(fleets, attemptCutV2HeadTestExpected(t, fixture, options.CurrentBindingKVs, first)) {
			t.Fatalf("signed native cursor %d changed full record or prefix coverage: %v", first, err)
		}
	}
}

// Exact unique-pair bounds count repeated observations only once; one fewer
// slot refuses the whole projection instead of silently truncating evidence.
func TestAttemptCutV2HeadEnforcesDistinctPrefixBound(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	options := attemptCutV2HeadTestOptions(t, fixture)
	expected := attemptCutV2HeadTestExpected(t, fixture, options.CurrentBindingKVs, fixture.cut.Context.EgressFirstSequence)
	var pairs uint64
	for _, hashes := range expected {
		pairs += uint64(len(hashes))
	}
	if pairs < 2 {
		t.Fatal("real M8 fixture lacks multiple distinct prefixes")
	}
	options.MaxFleetPrefixes = pairs
	fleets, _, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil || !reflect.DeepEqual(fleets, expected) {
		t.Fatalf("exact distinct-prefix bound refused genuine complete evidence: %v", err)
	}
	options = attemptCutV2HeadTestOptions(t, fixture)
	options.MaxFleetPrefixes = pairs - 1
	fleets, replayed, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err == nil || fleets != nil || replayed != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("over-budget prefixes published a partial head result: %v", err)
	}
}

// This is a projection-unit test, not a claim of signed-stream validity for
// the edited inputs. Public authentication is exercised by the real-trail
// tests above. One shared prefix counts once per fleet, not once globally or
// once per provider/observation, and the projection must not mutate its input.
func TestAttemptCutV2HeadCountsSharedPrefixesPerFleet(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	var record AttemptRecord
	for _, candidate := range fixture.seal.recordTs {
		if candidate.Disposition == AttemptDispositionComplete {
			record = candidate
			break
		}
	}
	if record.Proof == nil || len(record.Assignments) != 7 {
		t.Fatal("real M8 projection fixture is incomplete")
	}
	record.Assignments = slices.Clone(record.Assignments)
	proof := *record.Proof
	proof.Hops = slices.Clone(proof.Hops)
	record.Proof = &proof
	currentKVs := map[connect.Id]FleetScoreKey{}
	for index := range record.Assignments {
		assignment := &record.Assignments[index]
		assignment.Binding = attemptLedgerTestBinding(assignment.NextHop, uint64(1+index%2))
		currentKVs[assignment.NextHop] = attemptCutV2HeadTestFleetKey(t, assignment.Binding)
	}
	shared := [32]byte{0x51}
	for index := range proof.Hops {
		proof.Hops[index].EgressIpHash = shared
	}
	options := AttemptCutV2HeadOptions{CurrentBindingKVs: currentKVs, MaxProviders: 7, MaxFleetPrefixes: 2}
	projection, err := newAttemptCutV2HeadProjection(t.Context(), 1, options)
	if err != nil {
		t.Fatal(err)
	}
	before, err := attemptRecordHash(&record)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := projection.visitRecord(record); err != nil {
			t.Fatal(err)
		}
	}
	after, err := attemptRecordHash(&record)
	if err != nil || before != after || projection.fleetPrefixes != 2 || len(projection.fleetsKVs) != 2 {
		t.Fatalf("shared-prefix projection changed input or distinct-pair count: count=%d error=%v", projection.fleetPrefixes, err)
	}
	for _, hashes := range projection.fleetsKVs {
		if len(hashes) != 1 || !hashes[shared] {
			t.Fatal("shared prefix was counted by provider or lost by global deduplication")
		}
	}
	options.MaxFleetPrefixes = 1
	limited, err := newAttemptCutV2HeadProjection(t.Context(), 1, options)
	if err != nil || limited.visitRecord(record) == nil {
		t.Fatalf("cross-fleet shared prefix bypassed its distinct-pair bound: %v", err)
	}
}

// Invalid independent limits/identities and foreign replay visitors never
// acquire a public object or create scratch, including canceled/nil contexts.
func TestAttemptCutV2HeadAdmissionPrecedesIO(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	for field := 0; field < 8; field++ {
		options := attemptCutV2HeadTestOptions(t, fixture)
		options.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) { t.Fatal("invalid head admission read metadata"); return nil, nil }
		options.Replay.OpenData = func(context.Context, string, string, uint64) (io.ReadCloser, error) { t.Fatal("invalid head admission opened data"); return nil, nil }
		switch field {
		case 0:
			options.MaxProviders = 0
		case 1:
			options.MaxFleetPrefixes = 0
		case 2:
			options.MaxProviders = uint64(len(options.CurrentBindingKVs)) - 1
		case 3:
			options.Replay.VisitRecord = func(AttemptRecord) error { t.Fatal("foreign head visitor invoked"); return nil }
		default:
			for id, key := range options.CurrentBindingKVs {
				switch field {
				case 4:
					delete(options.CurrentBindingKVs, id)
					id = connect.Id{}
				case 5:
					key.FleetID = [32]byte{}
				case 6:
					key.Hotkey = [32]byte{}
				case 7:
					key.Generation = 0
				}
				options.CurrentBindingKVs[id] = key
				break
			}
		}
		fleets, replayed, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
		if err == nil || fleets != nil || replayed != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("invalid head admission %d returned authority: %v", field, err)
		}
		if _, err := os.Lstat(options.Replay.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid head admission %d changed scratch: %v", field, err)
		}
	}
	for _, canceled := range []bool{false, true} {
		var ctx context.Context
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(t.Context())
			cancel()
		}
		fleets, replayed, err := ReplayAttemptCutV2HeadWithPolicy(ctx, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, attemptCutV2HeadTestOptions(t, fixture))
		if err == nil || canceled && !errors.Is(err, context.Canceled) || fleets != nil || replayed != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("invalid head context returned authority: %v", err)
		}
	}
}

// External reads cannot mutate the copied current-state view. A later call
// must read the changed input anew rather than reuse the preceding verdict.
func TestAttemptCutV2HeadOwnsBindingsAndRechecksNextCall(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	options := attemptCutV2HeadTestOptions(t, fixture)
	expected := attemptCutV2HeadTestExpected(t, fixture, options.CurrentBindingKVs, fixture.cut.Context.EgressFirstSequence)
	changed := false
	options.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if !changed {
			for id, key := range options.CurrentBindingKVs {
				key.Generation++
				options.CurrentBindingKVs[id] = key
			}
			changed = true
		}
		return fixture.metadata(ctx, hash, size)
	}
	fleets, _, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil || !changed || !reflect.DeepEqual(fleets, expected) {
		t.Fatalf("external metadata read changed owned head attribution: %v", err)
	}
	options.Replay = fixture.options(t, fixture.measurement).Replay
	fleets, _, err = ReplayAttemptCutV2HeadWithPolicy(t.Context(), fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, hashes := range fleets {
		if len(hashes) != 0 {
			t.Fatal("new invocation reused a prior binding generation's verdict")
		}
	}
}

// A real proof stream Close failure occurs after every record was consumed.
// No valid-looking prefix subset or partial replay census may escape it.
func TestAttemptCutV2HeadLateProofCloseDiscardsPrefixes(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	options := attemptCutV2HeadTestOptions(t, fixture)
	failure := errors.New("head proof close failure")
	proofOpens := 0
	options.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := fixture.data(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		proofOpens++
		return &attemptCutV2StatsCloseFailure{ReadCloser: reader, failure: failure}, nil
	}
	fleets, replayed, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if !errors.Is(err, failure) || proofOpens == 0 || fleets != nil || replayed != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("late proof close published head authority: proof-opens=%d error=%v", proofOpens, err)
	}
}

// An eligible but unobserved fleet remains explicit with an empty set. No
// synthetic trail is necessary to represent a fully signed empty window.
func TestAttemptCutV2HeadEmptyWindowKeepsEligibleZero(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 0, 0)
	options := attemptCutV2HeadTestOptions(t, fixture)
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 0}
	options.CurrentBindingKVs[connect.Id{1}] = key
	fleets, replayed, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil || replayed.Records.ItemCount != 0 || len(fleets) != 1 || fleets[key] == nil || len(fleets[key]) != 0 {
		t.Fatalf("signed empty window lost its eligible zero-score fleet: %+v error=%v", replayed, err)
	}
}

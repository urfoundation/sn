//go:build linux || darwin

package validator

// Joint projection tests use the real disk ledger, signed M8 streams and
// scoring engine. Transport observers delegate all reads and closes; no test
// substitutes cryptographic, lifecycle, statistics or policy acceptance.

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

// Each operation owns fresh scratch and explicit test-only count limits.
func attemptCutV2MeasurementTestOptions(t *testing.T, fixture *attemptCutV2StatsTestFixture) AttemptCutV2MeasurementOptions {
	t.Helper()
	stats := fixture.options(t, fixture.measurement)
	head := attemptCutV2HeadTestOptions(t, fixture)
	return AttemptCutV2MeasurementOptions{
		ExpectedConfig: stats.ExpectedConfig, CurrentBindingKVs: head.CurrentBindingKVs,
		MaxProviders: max(stats.MaxProviders, head.MaxProviders), MaxEgressHashes: stats.MaxEgressHashes,
		MaxFleetPrefixes: head.MaxFleetPrefixes, Replay: stats.Replay,
	}
}

// Record exact real object fetches, including any implementation-required
// repeated metadata reads. Both comparison operations use the same cut.
func attemptCutV2MeasurementTestObserveReads(fixture *attemptCutV2StatsTestFixture, options *AttemptCutV2ReplayOptions) map[string]int {
	readKVs := map[string]int{}
	options.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		readKVs["metadata/"+hash]++
		return fixture.metadata(ctx, hash, size)
	}
	options.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		readKVs[kind+"/"+hash]++
		return fixture.data(ctx, kind, hash, size)
	}
	return readKVs
}

// Every error must clear all jointly owned outputs, including the replay
// census: a valid-looking half-result is not an accepted measurement.
func assertAttemptCutV2MeasurementTestEmpty(t *testing.T, result VerifiedAttemptCutV2Measurement) {
	t.Helper()
	if result.Stats.Providers != nil || result.HeadPrefixes != nil || result.Replay != (AttemptCutV2ReplayResult{}) {
		t.Fatal("failed joint replay published a partial measurement")
	}
}

// A real above-minimum population produces positive quality and exact legacy
// head attribution with precisely one standalone replay's public read census.
func TestAttemptCutV2MeasurementSharesOneRealReplay(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsQualityTestFixture(t)
	standalone := fixture.options(t, fixture.measurement)
	standaloneReads := attemptCutV2MeasurementTestObserveReads(fixture, &standalone.Replay)
	stats, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, standalone)
	if err != nil {
		t.Fatal(err)
	}
	options := attemptCutV2MeasurementTestOptions(t, fixture)
	jointReads := attemptCutV2MeasurementTestObserveReads(fixture, &options.Replay)
	wantHead := attemptCutV2HeadTestExpected(t, fixture, options.CurrentBindingKVs, fixture.cut.Context.EgressFirstSequence)
	result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil || !reflect.DeepEqual(result.Stats, stats) || !reflect.DeepEqual(result.Stats, fixture.legacy) || !reflect.DeepEqual(result.HeadPrefixes, wantHead) || result.Replay != replayed {
		t.Fatalf("joint replay differs from actual standalone/legacy evidence: %v", err)
	}
	if len(standaloneReads) == 0 || !reflect.DeepEqual(jointReads, standaloneReads) || result.Replay.Records.ItemCount != 122 || result.Replay.CompleteCount != 15 || result.Replay.FailedCount != 1 {
		t.Fatalf("joint replay duplicated or omitted real stream work: standalone=%v joint=%v replay=%+v", standaloneReads, jointReads, result.Replay)
	}
	qualities, prefixes := 0, 0
	for _, provider := range result.Stats.Providers {
		if provider.HasQuality && provider.QualityPPM > 0 {
			qualities++
		}
	}
	for _, hashes := range result.HeadPrefixes {
		prefixes += len(hashes)
	}
	if qualities == 0 || prefixes == 0 {
		t.Fatal("real above-minimum joint fixture lost positive pool or head inputs")
	}
}

// Well-formed but invented raw exposure is detected after complete replay;
// its otherwise valid head prefixes cannot be returned on that failure.
func TestAttemptCutV2MeasurementRejectsRawDriftBeforeHeadPublication(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
	measurement.Providers[0].Assignments++
	options := attemptCutV2MeasurementTestOptions(t, fixture)
	reads := attemptCutV2MeasurementTestObserveReads(fixture, &options.Replay)
	result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err == nil || len(reads) == 0 {
		t.Fatalf("invented raw exposure was not refused after real replay: %v", err)
	}
	assertAttemptCutV2MeasurementTestEmpty(t, result)
}

// The reciprocal failure is atomic too: a complete raw statistics input
// cannot escape when its genuine head evidence exceeds the independent cap.
func TestAttemptCutV2MeasurementRefusesHeadBudgetWithoutStatsPublication(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	options := attemptCutV2MeasurementTestOptions(t, fixture)
	options.MaxFleetPrefixes = 1
	result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err == nil {
		t.Fatal("genuine M8 distinct fleet/prefix pairs bypassed their cap")
	}
	assertAttemptCutV2MeasurementTestEmpty(t, result)
}

// Rebinding changes head ownership, not already authenticated operator work.
// Valid UID zero differs from the fixture's historical UID and inherits none.
func TestAttemptCutV2MeasurementRebindingCannotInheritHeadOrEraseStats(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	options := attemptCutV2MeasurementTestOptions(t, fixture)
	for id, key := range options.CurrentBindingKVs {
		key.UID = 0
		options.CurrentBindingKVs[id] = key
	}
	result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil || !reflect.DeepEqual(result.Stats, fixture.legacy) || result.Replay.Records.ItemCount != 18 {
		t.Fatalf("rebinding erased real operator exposure: %v", err)
	}
	for _, hashes := range result.HeadPrefixes {
		if len(hashes) != 0 {
			t.Fatal("new UID inherited old binding head prefixes")
		}
	}
}

// A proof-reader Close error follows actual signed record consumption and
// cannot publish either positive scores or generation-attributed prefixes.
func TestAttemptCutV2MeasurementLateCloseDiscardsBoth(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsQualityTestFixture(t)
	options := attemptCutV2MeasurementTestOptions(t, fixture)
	failure := errors.New("joint measurement proof close failure")
	proofOpens := 0
	options.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := fixture.data(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		proofOpens++
		return &attemptCutV2StatsCloseFailure{ReadCloser: reader, failure: failure}, nil
	}
	result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if !errors.Is(err, failure) || proofOpens == 0 {
		t.Fatalf("real late close refusal disappeared: proof-opens=%d error=%v", proofOpens, err)
	}
	assertAttemptCutV2MeasurementTestEmpty(t, result)
}

// Cancellation is an explicit state transition after the real reader joins;
// it is not a sleep, deadline race or manufactured stream-verification result.
type attemptCutV2MeasurementCancelClose struct {
	io.ReadCloser
	cancel context.CancelFunc
	closes *int
}

// Preserve the real close result, then make late cancellation observable.
func (self *attemptCutV2MeasurementCancelClose) Close() error {
	err := self.ReadCloser.Close()
	(*self.closes)++
	self.cancel()
	return err
}

// Even successful real stream Close cannot publish evidence after cancellation.
func TestAttemptCutV2MeasurementCancellationAfterRealCloseDiscardsBoth(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	options := attemptCutV2MeasurementTestOptions(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closes := 0
	options.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := fixture.data(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &attemptCutV2MeasurementCancelClose{ReadCloser: reader, cancel: cancel, closes: &closes}, nil
	}
	result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(ctx, fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if !errors.Is(err, context.Canceled) || closes == 0 {
		t.Fatalf("real-close cancellation was ignored: closes=%d error=%v", closes, err)
	}
	assertAttemptCutV2MeasurementTestEmpty(t, result)
}

// Metadata transport runs only after both bounded input projections are owned.
// Mutating caller maps/slices then cannot change either pending computation.
func TestAttemptCutV2MeasurementOwnsInputsBeforeExternalIO(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
	options := attemptCutV2MeasurementTestOptions(t, fixture)
	wantHead := attemptCutV2HeadTestExpected(t, fixture, options.CurrentBindingKVs, fixture.cut.Context.EgressFirstSequence)
	changed := false
	options.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if !changed {
			measurement.Providers[0].Assignments++
			measurement.Providers[0].LatencyBuckets[0]++
			for id, key := range options.CurrentBindingKVs {
				key.Generation++
				options.CurrentBindingKVs[id] = key
			}
			changed = true
		}
		return fixture.metadata(ctx, hash, size)
	}
	result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil || !changed || !reflect.DeepEqual(result.Stats, fixture.legacy) || !reflect.DeepEqual(result.HeadPrefixes, wantHead) {
		t.Fatalf("external read changed owned joint projections: changed=%v error=%v", changed, err)
	}
}

// A second operation must validate changed raw evidence again, not inherit a
// verdict through the same cut hash or caller-mutated returned result maps.
func TestAttemptCutV2MeasurementRechecksSecondCall(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	first, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, attemptCutV2MeasurementTestOptions(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	clear(first.Stats.Providers)
	clear(first.HeadPrefixes)
	measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
	measurement.Providers[0].Assignments++
	result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, attemptCutV2MeasurementTestOptions(t, fixture))
	if err == nil {
		t.Fatal("new operation reused the preceding measurement verdict")
	}
	assertAttemptCutV2MeasurementTestEmpty(t, result)
	third, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, attemptCutV2MeasurementTestOptions(t, fixture))
	if err != nil || !reflect.DeepEqual(third.Stats, fixture.legacy) || len(third.HeadPrefixes) == 0 {
		t.Fatalf("mutated prior result contaminated a fresh real replay: %v", err)
	}
}

// Every independent shape/count/config/ownership refusal precedes transport
// and scratch creation. Nil/canceled contexts follow the same empty contract.
func TestAttemptCutV2MeasurementAdmissionPrecedesIO(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	for field := 0; field < 10; field++ {
		measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
		options := attemptCutV2MeasurementTestOptions(t, fixture)
		options.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
			t.Fatal("invalid joint admission read metadata")
			return nil, nil
		}
		options.Replay.OpenData = func(context.Context, string, string, uint64) (io.ReadCloser, error) {
			t.Fatal("invalid joint admission opened data")
			return nil, nil
		}
		switch field {
		case 0:
			options.MaxProviders = 0
		case 1:
			options.MaxEgressHashes = 0
		case 2:
			options.MaxFleetPrefixes = 0
		case 3:
			options.ExpectedConfig.AMin++
		case 4:
			measurement.Providers[0].ClientID = strings.Repeat("x", 4096)
		case 5:
			measurement.Providers[0].LatencyBuckets = make([]uint64, statsLatencyBuckets+1)
		case 6:
			measurement.Providers[0].EgressIPHashHexes = []string{strings.Repeat("A", 4096)}
		case 7:
			options.Replay.VisitRecord = func(AttemptRecord) error { t.Fatal("foreign joint visitor invoked"); return nil }
		case 8:
			measurement.AttemptCut = &AttemptLedgerCut{}
		case 9:
			options.CurrentBindingKVs[connect.Id{}] = FleetScoreKey{}
		}
		result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
		if err == nil {
			t.Fatalf("invalid joint admission %d was accepted", field)
		}
		assertAttemptCutV2MeasurementTestEmpty(t, result)
		if _, err := os.Lstat(options.Replay.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid joint admission %d changed scratch: %v", field, err)
		}
	}
	for _, canceled := range []bool{false, true} {
		var ctx context.Context
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(t.Context())
			cancel()
		}
		result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(ctx, fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, attemptCutV2MeasurementTestOptions(t, fixture))
		if err == nil || canceled && !errors.Is(err, context.Canceled) {
			t.Fatalf("invalid joint context was accepted: %v", err)
		}
		assertAttemptCutV2MeasurementTestEmpty(t, result)
	}
}

// Empty signed streams retain an eligible zero without fabricating work.
func TestAttemptCutV2MeasurementEmptyWindowRetainsEligibleZero(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 0, 0)
	options := attemptCutV2MeasurementTestOptions(t, fixture)
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 0}
	options.CurrentBindingKVs[connect.Id{1}] = key
	result, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil || !reflect.DeepEqual(result.Stats, fixture.legacy) || result.Replay.Records.ItemCount != 0 || len(result.HeadPrefixes) != 1 || result.HeadPrefixes[key] == nil || len(result.HeadPrefixes[key]) != 0 {
		t.Fatalf("joint signed empty window lost its eligible zero: %v", err)
	}
}

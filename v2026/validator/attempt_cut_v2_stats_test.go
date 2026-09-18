//go:build linux || darwin

package validator

// Real M8 trails, disk records and signed stream objects are shared with the
// sealer tests. Statistics come from the same real engine, not invented totals.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// Each replay gets fresh owned scratch; staged objects remain immutable.
type attemptCutV2StatsTestFixture struct {
	seal        *attemptCutV2SealTestFixture
	cut         AttemptCutV2
	measurement ReleaseStatsMeasurement
	legacy      VerifiedReleaseStats
	metadata    AttemptStreamV2MetadataReader
	data        AttemptStreamV2DataOpener
}

// Both old and compact cuts cover the exact same authenticated ledger prefix.
func newAttemptCutV2StatsTestFixture(t *testing.T, completed, failed int) *attemptCutV2StatsTestFixture {
	t.Helper()
	fixture := newAttemptCutV2SealTestFixture(t, 8, completed, failed)
	measurement := func() ReleaseStatsMeasurement {
		fixture.engine.stats.mu.Lock()
		defer fixture.engine.stats.mu.Unlock()
		return fixture.engine.stats.releaseStatsMeasurementWithLock()
	}()
	// Disk ledgers deliberately refuse whole-history BuildCut. The bounded
	// test oracle replays the same real signed rows into an independent v1 store.
	legacyLedger, err := NewAttemptLedger(newAttemptLedgerDiskTestStateDir(t), fixture.expected.Identity, fixture.key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacyLedger.Close() })
	for _, record := range fixture.recordTs {
		appended, err := legacyLedger.AppendContext(t.Context(), record)
		if err != nil || !reflect.DeepEqual(appended, &record) {
			t.Fatalf("legacy oracle changed a real signed record: %v", err)
		}
	}
	legacyCut, err := legacyLedger.BuildCut(fixture.expected.Boundary, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	measurement.AttemptCut = legacyCut
	legacy, err := VerifyReleaseStatsMeasurement(measurement)
	if err != nil {
		t.Fatalf("real legacy score replay: %v", err)
	}
	measurement.AttemptCut = nil
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil || cut == nil {
		t.Fatalf("real compact score input: %v", err)
	}
	return &attemptCutV2StatsTestFixture{seal: fixture, cut: *cut, measurement: measurement, legacy: legacy, metadata: options.ReadMetadata, data: options.OpenData}
}

// Explicit test budgets are the exact input census, with a positive empty cap.
func (self *attemptCutV2StatsTestFixture) options(t *testing.T, measurement ReleaseStatsMeasurement) AttemptCutV2StatsOptions {
	t.Helper()
	var hashes uint64
	for _, provider := range measurement.Providers {
		hashes += uint64(len(provider.EgressIPHashHexes))
	}
	return AttemptCutV2StatsOptions{
		ExpectedConfig: measurement.Config, MaxProviders: max(1, uint64(len(measurement.Providers))), MaxEgressHashes: max(1, hashes),
		Replay: AttemptCutV2ReplayOptions{Bounds: self.seal.replay, ScratchDirectory: filepath.Join(t.TempDir(), "stats-replay"), ServerKeys: self.seal.server.serverPublicKeys(), ReadMetadata: self.metadata, OpenData: self.data},
	}
}

// Own a fresh small measurement for each mutation without changing the source.
func cloneAttemptCutV2StatsTestMeasurement(t *testing.T, measurement ReleaseStatsMeasurement) ReleaseStatsMeasurement {
	t.Helper()
	raw, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	var result ReleaseStatsMeasurement
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// Score, counter, quality and full egress sets agree with the existing v1 path.
func TestAttemptCutV2StatsMatchesRealM8AndFailedAttempts(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	before, _ := json.Marshal(fixture.measurement)
	verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, fixture.measurement))
	if err != nil || !reflect.DeepEqual(verified, fixture.legacy) || replayed.CompleteCount != 2 || replayed.FailedCount != 1 || replayed.Records.ItemCount != 18 {
		t.Fatalf("real stream score replay differs: %+v error=%v", replayed, err)
	}
	after, _ := json.Marshal(fixture.measurement)
	if !bytes.Equal(before, after) {
		t.Fatal("stream score replay mutated its measurement")
	}
}

// Empty windows retain a genuine empty signed stream; no fake trail is needed.
func TestAttemptCutV2StatsEmptyWindowAndIdlePriorQuality(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 0, 0)
	measurement := fixture.measurement
	measurement.Providers = []ReleaseProviderMeasurement{{ClientID: connect.Id{1}.String(), HasPriorQuality: true, PriorQualityPPM: 12345, LatencyBuckets: make([]uint64, statsLatencyBuckets)}}
	verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, measurement))
	provider := verified.Providers[connect.Id{1}]
	if err != nil || replayed.CompleteCount != 0 || replayed.FailedCount != 0 || provider.QualityPPM != 12345 || !provider.HasQuality || provider.Exposure != 0 {
		t.Fatalf("empty signed window lost idle prior quality: %+v error=%v", provider, err)
	}
}

// Every mutation remains well-formed raw statistics; stream joining rejects it.
func TestAttemptCutV2StatsRejectsMissingAndInventedInputs(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 1)
	for _, edit := range []func(*ReleaseStatsMeasurement){
		func(value *ReleaseStatsMeasurement) { value.Providers = value.Providers[1:] },
		func(value *ReleaseStatsMeasurement) { value.Providers[0].Assignments++ },
		func(value *ReleaseStatsMeasurement) {
			for index := range value.Providers {
				provider := &value.Providers[index]
				if provider.Assignments > provider.Confirmations {
					provider.Assignments--
					return
				}
			}
			t.Fatal("fixture lacks its genuine failed exposure")
		},
		func(value *ReleaseStatsMeasurement) {
			for index := range value.Providers {
				provider := &value.Providers[index]
				for bucket, count := range provider.LatencyBuckets {
					if count != 0 {
						provider.LatencyBuckets[bucket]--
						provider.LatencyBuckets[(bucket+1)%statsLatencyBuckets]++
						return
					}
				}
			}
			t.Fatal("fixture lacks actual confirmed latency")
		},
		func(value *ReleaseStatsMeasurement) {
			for index := range value.Providers {
				if len(value.Providers[index].EgressIPHashHexes) != 0 {
					value.Providers[index].EgressIPHashHexes = value.Providers[index].EgressIPHashHexes[1:]
					return
				}
			}
			t.Fatal("fixture lacks actual routed egress")
		},
		func(value *ReleaseStatsMeasurement) {
			for index := range value.Providers {
				provider := &value.Providers[index]
				for bucket, count := range provider.LatencyBuckets {
					if count != 0 {
						provider.Confirmations--
						provider.LatencyBuckets[bucket]--
						return
					}
				}
			}
			t.Fatal("fixture lacks actual confirmed exposure")
		},
		func(value *ReleaseStatsMeasurement) {
			hash := "0x" + strings.Repeat("f", 64)
			for _, existing := range value.Providers[0].EgressIPHashHexes {
				if existing == hash {
					t.Fatal("fixture contains the extra-hash sentinel")
				}
			}
			value.Providers[0].EgressIPHashHexes = append(value.Providers[0].EgressIPHashHexes, hash)
			sort.Strings(value.Providers[0].EgressIPHashHexes)
		},
		func(value *ReleaseStatsMeasurement) {
			value.Providers = append(value.Providers, ReleaseProviderMeasurement{ClientID: connect.Id{0xff}.String(), Assignments: 1, LatencyBuckets: make([]uint64, statsLatencyBuckets)})
			sort.Slice(value.Providers, func(i, j int) bool { return value.Providers[i].ClientID < value.Providers[j].ClientID })
		},
	} {
		measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
		edit(&measurement)
		if _, err := VerifyReleaseStatsMeasurement(measurement); err != nil {
			t.Fatalf("mutation did not reach stream comparison: %v", err)
		}
		verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, measurement))
		if err == nil || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("changed raw inputs accepted or partial result returned: %v", err)
		}
	}
}

// All admission refusals precede public reads and replay scratch creation.
func TestAttemptCutV2StatsRejectsAuthorityAndBoundsBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	for _, edit := range []func(*ReleaseStatsMeasurement, *AttemptCutV2StatsOptions){
		func(value *ReleaseStatsMeasurement, _ *AttemptCutV2StatsOptions) { value.AttemptCut = &AttemptLedgerCut{} },
		func(value *ReleaseStatsMeasurement, _ *AttemptCutV2StatsOptions) { value.SettlementTransition = &AttemptSettlementTransition{} },
		func(_ *ReleaseStatsMeasurement, options *AttemptCutV2StatsOptions) { options.Replay.VisitRecord = func(AttemptRecord) error { return nil } },
		func(_ *ReleaseStatsMeasurement, options *AttemptCutV2StatsOptions) { options.MaxProviders = 0 },
		func(_ *ReleaseStatsMeasurement, options *AttemptCutV2StatsOptions) { options.MaxProviders-- },
		func(_ *ReleaseStatsMeasurement, options *AttemptCutV2StatsOptions) { options.MaxEgressHashes = 0 },
		func(_ *ReleaseStatsMeasurement, options *AttemptCutV2StatsOptions) { options.MaxEgressHashes-- },
		func(_ *ReleaseStatsMeasurement, options *AttemptCutV2StatsOptions) { options.ExpectedConfig.LatRefMillis++ },
		func(value *ReleaseStatsMeasurement, options *AttemptCutV2StatsOptions) { value.Config.AMin++; options.ExpectedConfig.AMin++ },
	} {
		measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
		options := fixture.options(t, measurement)
		options.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) { t.Fatal("invalid admission reached metadata"); return nil, nil }
		options.Replay.OpenData = func(context.Context, string, string, uint64) (io.ReadCloser, error) { t.Fatal("invalid admission reached data"); return nil, nil }
		edit(&measurement, &options)
		verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
		if err == nil || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("invalid admission returned authority: %v", err)
		}
		if _, err := os.Lstat(options.Replay.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid admission changed replay storage: %v", err)
		}
	}
	for _, canceled := range []bool{false, true} {
		var ctx context.Context
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(t.Context())
			cancel()
		}
		verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(ctx, fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, fixture.measurement))
		if err == nil || canceled && !errors.Is(err, context.Canceled) || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("invalid context returned authority: %v", err)
		}
	}
}

// A cancellation after matching record inputs still invalidates every score.
func TestAttemptCutV2StatsLateCancellationDiscardsScores(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options := fixture.options(t, fixture.measurement)
	proofOpens := 0
	options.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := fixture.data(ctx, kind, hash, size)
		if kind == AttemptStreamV2Proofs && err == nil {
			proofOpens++
			cancel()
		}
		return reader, err
	}
	verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(ctx, fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if !errors.Is(err, context.Canceled) || proofOpens == 0 || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("canceled complete record input returned score authority: %v", err)
	}
}

// The copied scoring state is stable across external reads, but each new call
// must authenticate its own changed inputs rather than reuse a prior verdict.
func TestAttemptCutV2StatsOwnsInputsAndRechecksNextCall(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
	options := fixture.options(t, measurement)
	changed := false
	options.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if !changed {
			measurement.Providers[0].Assignments++
			changed = true
		}
		return fixture.metadata(ctx, hash, size)
	}
	verified, _, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err != nil || !changed || !reflect.DeepEqual(verified, fixture.legacy) {
		t.Fatalf("external read changed owned score inputs: %v", err)
	}
	verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, measurement))
	if err == nil || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("new invocation reused an earlier score verdict: %v", err)
	}
}

// Counters span the settlement cut; egress uses only its signed native cursor.
func TestAttemptCutV2StatsUsesSignedEgressCursor(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 2, 0)
	cut := fixture.cut
	cut.Context.EgressFirstSequence = cut.LastSequence + 1
	var err error
	cut.Signature, err = cut.Sign(fixture.seal.key, fixture.seal.bounds)
	if err != nil {
		t.Fatal(err)
	}
	measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
	for index := range measurement.Providers {
		measurement.Providers[index].EgressIPHashHexes = nil
	}
	verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), measurement, cut, cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, measurement))
	if err != nil || replayed.CompleteCount != 2 || len(verified.Providers) != len(fixture.legacy.Providers) {
		t.Fatalf("signed cursor lost settlement counters: %v", err)
	}
	for id, provider := range verified.Providers {
		if provider.Exposure != fixture.legacy.Providers[id].Exposure || len(provider.EgressIPHashes) != 0 {
			t.Fatalf("provider %s ignored the signed cursor", id)
		}
	}
	verified, replayed, err = VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), fixture.measurement, cut, cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, fixture.measurement))
	if err == nil || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("old egress leaked into a new native cut: %v", err)
	}
}

// The wrapped reader still closes its actual stream before adding the failure.
type attemptCutV2StatsCloseFailure struct {
	io.ReadCloser
	failure error
}

// Late close errors are not a successful end of the authenticated input.
func (self *attemptCutV2StatsCloseFailure) Close() error {
	return errors.Join(self.ReadCloser.Close(), self.failure)
}

// Matching all record counters is insufficient without the complete proof EOF.
func TestAttemptCutV2StatsLateReplayFailureDiscardsScores(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	failure := errors.New("owned proof stream failure")
	for _, failClose := range []bool{false, true} {
		options := fixture.options(t, fixture.measurement)
		proofOpens := 0
		options.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			if kind != AttemptStreamV2Proofs {
				return fixture.data(ctx, kind, hash, size)
			}
			proofOpens++
			if !failClose {
				return nil, failure
			}
			reader, err := fixture.data(ctx, kind, hash, size)
			if err != nil {
				return reader, err
			}
			return &attemptCutV2StatsCloseFailure{ReadCloser: reader, failure: failure}, nil
		}
		verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
		if !errors.Is(err, failure) || proofOpens == 0 || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
			t.Fatalf("late proof failure returned partial score authority: %v", err)
		}
	}
}

// Public record-byte integrity is still verified even when declared scores fit.
func TestAttemptCutV2StatsRetainsStreamAuthentication(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsTestFixture(t, 1, 0)
	options := fixture.options(t, fixture.measurement)
	reads := 0
	options.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		reads++
		raw, err := fixture.metadata(ctx, hash, size)
		if err != nil {
			return nil, err
		}
		raw = bytes.Clone(raw)
		raw[len(raw)-1] ^= 1
		return raw, nil
	}
	verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if err == nil || !strings.Contains(err.Error(), "manifest bytes differ from their signed reference") || reads == 0 || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("metadata substitution escaped full replay: %v", err)
	}
}

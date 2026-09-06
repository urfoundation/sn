//go:build linux || darwin

package validator

// Full signed M8 histories must exercise scored providers, not only sparse
// zero-quality inputs. The existing fixture and all storage/policy limits stay
// unchanged; the assignment census guarantees reaching the real minimum.

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/urnetwork/connect"
)

// Fifteen complete M8 trails plus one failed extension produce 106 exposures
// across at most fifteen non-seed providers. At minimum8, at least one provider
// must be scored regardless of random assignment order: 106 > 15*(8-1).
// The sixteen trails and 122 records fit the original 16/128 disk-store caps.
func newAttemptCutV2StatsQualityTestFixture(t *testing.T) *attemptCutV2StatsTestFixture {
	t.Helper()
	fixture := newAttemptCutV2StatsTestFixture(t, 15, 1)
	if fixture.measurement.Config.AMin != 8 || len(fixture.measurement.Providers) > 15 {
		t.Fatal("real policy or eligible provider census changed; review the threshold fixture")
	}
	var assignments, confirmations uint64
	for _, provider := range fixture.measurement.Providers {
		assignments += provider.Assignments
		confirmations += provider.Confirmations
	}
	if assignments != 106 || confirmations != 105 || fixture.cut.RecordCount != 122 || fixture.cut.CompleteCount != 15 || fixture.cut.FailedCount != 1 {
		t.Fatalf("real signed threshold census changed: assignments=%d confirmations=%d records=%d complete=%d failed=%d", assignments, confirmations, fixture.cut.RecordCount, fixture.cut.CompleteCount, fixture.cut.FailedCount)
	}
	if len(fixture.seal.engine.stats.QualityPPM()) == 0 {
		t.Fatal("guaranteed above-minimum exposures produced no scored provider")
	}
	return fixture
}

// Public streaming verification agrees with both the live statistics engine
// and the independent signed v1 replay for actual above-minimum providers.
func TestAttemptCutV2StatsScoresRealAboveMinimumProviders(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsQualityTestFixture(t)
	engineScores := fixture.seal.engine.stats.QualityPPM()
	verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, fixture.measurement))
	if err != nil || !reflect.DeepEqual(verified, fixture.legacy) || replayed.Records.ItemCount != 122 || replayed.CompleteCount != 15 || replayed.FailedCount != 1 {
		t.Fatalf("above-minimum public replay differs from signed legacy replay: %+v error=%v", replayed, err)
	}
	actualScores := map[connect.Id]uint32{}
	for id, provider := range verified.Providers {
		if provider.HasQuality {
			if provider.Exposure < fixture.measurement.Config.AMin || provider.QualityPPM == 0 {
				t.Fatalf("provider %s has no genuine positive above-minimum score", id)
			}
			actualScores[id] = provider.QualityPPM
		}
	}
	if len(actualScores) == 0 || !reflect.DeepEqual(actualScores, engineScores) {
		t.Fatalf("stream scores differ from the actual engine: got=%v want=%v", actualScores, engineScores)
	}
}

// A caller-authenticated prior must enter the exact integer EMA once. This
// checks the scoring transform, not authentication of historical prior values;
// the release lineage verifier remains responsible for that separate proof.
func TestAttemptCutV2StatsBlendsPriorWithRealAboveMinimumInputs(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsQualityTestFixture(t)
	measurement := cloneAttemptCutV2StatsTestMeasurement(t, fixture.measurement)
	const prior = uint32(1_000_000)
	for index := range measurement.Providers {
		measurement.Providers[index].HasPriorQuality = true
		measurement.Providers[index].PriorQualityPPM = prior
	}
	verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, fixture.options(t, measurement))
	if err != nil || replayed.Records.ItemCount != 122 || len(verified.Providers) != len(fixture.legacy.Providers) {
		t.Fatalf("prior-quality stream replay differs: %+v error=%v", replayed, err)
	}
	blended := 0
	for _, input := range measurement.Providers {
		id, err := connect.ParseId(input.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		actual := verified.Providers[id]
		baseline := fixture.legacy.Providers[id]
		want := prior
		if input.Assignments >= measurement.Config.AMin {
			config := measurement.Config
			want = uint32((config.AlphaNumerator*uint64(baseline.QualityPPM) + (config.AlphaDenominator-config.AlphaNumerator)*uint64(prior)) / config.AlphaDenominator)
			blended++
		}
		if !actual.HasQuality || actual.QualityPPM != want || actual.Exposure != baseline.Exposure || !reflect.DeepEqual(actual.EgressIPHashes, baseline.EgressIPHashes) {
			t.Fatalf("provider %s lost its exact EMA, signed exposures or egress: got=%d want=%d", id, actual.QualityPPM, want)
		}
	}
	if blended == 0 {
		t.Fatal("prior-quality replay did not exercise an actual EMA blend")
	}
}

// Nonzero, above-minimum scores still cannot escape before proof EOF and Close.
// The failing wrapper closes the real authenticated stream before adding its
// error; matching every signed record is not a partial-publication boundary.
func TestAttemptCutV2StatsLateCloseDiscardsAboveMinimumScores(t *testing.T) {
	t.Parallel()
	fixture := newAttemptCutV2StatsQualityTestFixture(t)
	failure := errors.New("above-minimum proof close failure")
	options := fixture.options(t, fixture.measurement)
	proofOpens := 0
	options.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := fixture.data(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		proofOpens++
		return &attemptCutV2StatsCloseFailure{ReadCloser: reader, failure: failure}, nil
	}
	verified, replayed, err := VerifyReleaseStatsMeasurementWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, options)
	if !errors.Is(err, failure) || proofOpens == 0 || verified.Providers != nil || replayed != (AttemptCutV2ReplayResult{}) {
		t.Fatalf("late proof close published scored provider authority: proof-opens=%d error=%v", proofOpens, err)
	}
}

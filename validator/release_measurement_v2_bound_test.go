//go:build linux || darwin

package validator

// Existing envelope limits apply equally to compact verification, sealing,
// intent comparison and live head admission. Real M8 readers are observed,
// never replaced by an accepted verdict; every accepted call gets fresh scratch.

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/urnetwork/connect"
)

// Exercise the actual public APIs with the independent legacy decision oracle.
func releaseMeasurementArtifactBoundCall(t *testing.T, ctx context.Context, name string, fixture *releaseMeasurementV2TestFixture, encoded []byte, options ReleaseMeasurementV2Options) error {
	t.Helper()
	check := func(result VerifiedReleaseMeasurementV2, err error) {
		if err != nil {
			assertReleaseMeasurementV2Empty(t, result)
			return
		}
		if !reflect.DeepEqual(result.Decision, fixture.want) || len(result.ReplayByNO) != len(fixture.operators) {
			t.Fatal("ceiling admission changed the real complete measurement decision")
		}
		for noID, replay := range result.ReplayByNO {
			if replay.Records.ItemCount != uint64(len(fixture.operators[noID].seal.recordTs)) || replay.CompleteCount != 2 || replay.FailedCount != 1 {
				t.Fatalf("ceiling admission omitted actual M8 records for operator %d: %+v", noID, replay)
			}
		}
	}
	switch name {
	case "verify":
		result, err := VerifyReleaseMeasurementArtifactV2(ctx, fixture.artifact, options)
		check(result, err)
		return err
	case "seal":
		actual, hash, err := SealReleaseMeasurementArtifactV2(ctx, fixture.artifact, options)
		if err != nil {
			if actual != nil || hash != "" {
				t.Fatal("failed ceiling admission returned partial sealed bytes")
			}
		} else if !bytes.Equal(actual, encoded) || hash != ReleaseMeasurementContentHash(encoded) {
			t.Fatal("ceiling admission changed canonical signed-evidence bytes")
		}
		return err
	case "decode":
		artifact, result, err := DecodeReleaseMeasurementArtifactV2(ctx, encoded, options)
		check(result, err)
		if err != nil && artifact != nil || err == nil && !reflect.DeepEqual(artifact, fixture.artifact) {
			t.Fatal("ceiling decode returned a partial or different artifact")
		}
		return err
	case "intent":
		return VerifyReleaseMeasurementIntentV2(ctx, encoded, options, fixture.intent(t, encoded))
	default:
		t.Fatalf("unknown artifact bound API %q", name)
		return nil
	}
}

// The full existing ceiling is admitted, but only the small genuine fixture
// is allocated/replayed. This is not a 64 MiB workload or performance claim.
func TestReleaseMeasurementV2ArtifactBoundExactCeilingRetainsRealReplay(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	encoded, err := canonicalReleaseMeasurementBytes(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	if maxReleaseMeasurementArtifactBytes != releaseMeasurementEnvelopeMaxArtifactSize || maxReleaseMeasurementArtifactBytes != 64*1024*1024 {
		t.Fatal("compact admission no longer uses the original envelope ceiling")
	}
	for _, name := range []string{"verify", "seal", "decode", "intent"} {
		options := fixture.options(t)
		options.MaxArtifactBytes, options.MaxControlBytes = releaseMeasurementEnvelopeMaxArtifactSize, releaseMeasurementEnvelopeMaxArtifactSize
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		if err := releaseMeasurementArtifactBoundCall(t, t.Context(), name, fixture, encoded, options); err != nil || *reads == 0 {
			t.Fatalf("%s refused the exact existing ceiling or skipped actual replay: reads=%d error=%v", name, *reads, err)
		}
	}
	t.Log("ARTIFACT-BOUND-v1 PASS TestReleaseMeasurementV2ArtifactBoundExactCeilingRetainsRealReplay")
}

// Zero remains invalid and ceiling+1 is refused before any real metadata/data
// callback or scratch creation. No default is manufactured for omitted limits.
func TestReleaseMeasurementV2ArtifactBoundRefusesBeforeProofIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	encoded, err := canonicalReleaseMeasurementBytes(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"verify", "seal", "decode", "intent"} {
		for _, field := range []string{"artifact", "control"} {
			for _, bound := range []uint64{0, releaseMeasurementEnvelopeMaxArtifactSize + 1} {
				options := fixture.options(t)
				if field == "artifact" {
					options.MaxArtifactBytes = bound
				} else {
					options.MaxControlBytes = bound
				}
				reads := observeReleaseMeasurementV2SettlementTest(&options)
				if err := releaseMeasurementArtifactBoundCall(t, t.Context(), name, fixture, encoded, options); err == nil || *reads != 0 {
					t.Fatalf("%s %s bound %d reached proof I/O: reads=%d error=%v", name, field, bound, *reads, err)
				}
				for _, operator := range options.Operators {
					if _, err := os.Lstat(operator.Measurement.Replay.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("%s %s invalid ceiling created scratch: %v", name, field, err)
					}
				}
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		options := fixture.options(t)
		options.MaxArtifactBytes, options.MaxControlBytes = releaseMeasurementEnvelopeMaxArtifactSize, releaseMeasurementEnvelopeMaxArtifactSize
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		if err := releaseMeasurementArtifactBoundCall(t, ctx, name, fixture, encoded, options); !errors.Is(err, context.Canceled) || *reads != 0 {
			t.Fatalf("%s canceled ceiling admission reached proof I/O: reads=%d error=%v", name, *reads, err)
		}
	}
	t.Log("ARTIFACT-BOUND-v1 PASS TestReleaseMeasurementV2ArtifactBoundRefusesBeforeProofIO")
}

// The common wire ceiling is also the live collector's control ceiling;
// neither invalid dimension can reach its pinned RPC or a proof transport.
func TestReleaseHeadV2ArtifactBoundRefusesBeforeCallbacks(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	clientCalls := 0
	for _, owner := range fixture.steerer.contexts {
		clientKey := owner.ClientKey
		owner.ClientKey = func(clientID connect.Id) ([32]byte, bool, error) {
			clientCalls++
			return clientKey(clientID)
		}
	}
	for _, field := range []string{"artifact", "control"} {
		for _, bound := range []uint64{0, releaseMeasurementEnvelopeMaxArtifactSize + 1} {
			options := fixture.options(t)
			if field == "artifact" {
				options.MaxArtifactBytes = bound
			} else {
				options.MaxControlBytes = bound
			}
			reads := observeReleaseMeasurementV2SettlementTest(&options)
			result, err := fixture.gather(t.Context(), options)
			requests, batches := fixture.rpc.counts()
			if err == nil || *reads != 0 || clientCalls != 0 || requests != 0 || batches != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
				t.Fatalf("live %s bound %d reached callbacks or returned output: reads=%d clients=%d requests=%d error=%v", field, bound, *reads, clientCalls, requests, err)
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	options := fixture.options(t)
	options.MaxArtifactBytes, options.MaxControlBytes = releaseMeasurementEnvelopeMaxArtifactSize, releaseMeasurementEnvelopeMaxArtifactSize
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(ctx, options)
	requests, batches := fixture.rpc.counts()
	if !errors.Is(err, context.Canceled) || *reads != 0 || clientCalls != 0 || requests != 0 || batches != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("canceled live ceiling reached callbacks or returned output: reads=%d clients=%d requests=%d error=%v", *reads, clientCalls, requests, err)
	}
	fixture.assertNoEMACommit(t)
	t.Log("ARTIFACT-BOUND-v1 PASS TestReleaseHeadV2ArtifactBoundRefusesBeforeCallbacks")
}

// The accepted boundary still performs genuine two-operator RPC and complete
// M8 replay, preserving the original exact head and no-early-commit behavior.
func TestReleaseHeadV2ArtifactBoundExactCeilingRetainsRealCollection(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	options := fixture.options(t)
	options.MaxArtifactBytes, options.MaxControlBytes = releaseMeasurementEnvelopeMaxArtifactSize, releaseMeasurementEnvelopeMaxArtifactSize
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	requests, batches := fixture.rpc.counts()
	if err != nil || *reads == 0 || requests == 0 || batches != 2 || !reflect.DeepEqual(result.Weights, fixture.measurement.want.SelectedHead) || !reflect.DeepEqual(result.HeadEMA, fixture.measurement.artifact.HeadEMA) || !reflect.DeepEqual(result.Bindings, fixture.measurement.artifact.Bindings) {
		t.Fatalf("exact live ceiling changed real collection: reads=%d requests=%d batches=%d error=%v", *reads, requests, batches, err)
	}
	fixture.assertNoEMACommit(t)
	t.Log("ARTIFACT-BOUND-v1 PASS TestReleaseHeadV2ArtifactBoundExactCeilingRetainsRealCollection")
}

// These are direct owner-admission contracts, not M8 or filesystem workload
// evidence. Each inherited preview owner must resolve the very same ceiling.
func TestReleaseHeadV2ArtifactBoundAllPreviewOwnersShareCeiling(t *testing.T) {
	t.Parallel()
	store, err := NewHeadEMAStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alpha := exactPolicy(t).Steering.HeadScoreEMA
	emptyRaw := map[FleetScoreKey]*big.Rat{}
	emptyFleets := map[FleetScoreKey]map[[32]byte]bool{}
	for _, name := range []string{"known", "current", "preview-budget", "preview-fleets", "preview-entry"} {
		for _, bound := range []uint64{0, releaseMeasurementEnvelopeMaxArtifactSize, releaseMeasurementEnvelopeMaxArtifactSize + 1} {
			budget := releaseHeadV2Budget{limit: bound}
			var out map[uint16]*big.Rat
			var head []HeadEMAMeasurement
			var err error
			switch name {
			case "known":
				err = store.admitReleaseHeadV2Known(t.Context(), 7, alpha, 1, budget)
			case "current":
				err = store.admitReleaseHeadV2Current(t.Context(), 7, emptyFleets, alpha, 1, budget)
			case "preview-budget":
				out, head, err = store.previewForEpochV2WithBudget(t.Context(), 7, emptyRaw, alpha, 1, budget)
			case "preview-fleets":
				out, head, _, err = store.previewReleaseHeadV2Fleets(t.Context(), 7, emptyFleets, alpha, 1, budget)
			case "preview-entry":
				out, head, err = store.previewForEpochV2(t.Context(), 7, emptyRaw, alpha, 1, bound)
			}
			valid := bound == releaseMeasurementEnvelopeMaxArtifactSize
			if valid && err != nil || !valid && (err == nil || out != nil || head != nil) {
				t.Fatalf("%s ceiling %d admission/output differs: %v", name, bound, err)
			}
		}
	}
	if _, err := os.Lstat(store.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview ceiling admission wrote durable state: %v", err)
	}
	t.Log("ARTIFACT-BOUND-v1 PASS TestReleaseHeadV2ArtifactBoundAllPreviewOwnersShareCeiling")
}

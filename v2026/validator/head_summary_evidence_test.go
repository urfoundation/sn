//go:build linux || darwin

package validator

// Genuine compact cuts prove that summary publication preserves byte identity,
// historical binding eligibility and isolation between operator recipients.

import (
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol/headsummary"
	"github.com/urnetwork/connect/v2026"
)

// The locator must name the actual published bytes, including the canonical
// newline. Replay uses the exact eligible generation and excludes a binding
// whose coordinator key differs from the measured client's key.
func TestHeadSummaryEvidenceReplaysThePublishedCut(t *testing.T) {
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	excluded := ""
	for index := range fixture.artifact.Bindings {
		binding := &fixture.artifact.Bindings[index]
		if binding.Active {
			binding.LocalClientKey = releaseHex32([32]byte{0x79})
			excluded = binding.ClientID
			break
		}
	}
	cuts, err := headSummaryEvidence(fixture.artifact)
	if err != nil || len(cuts) != 2 || excluded == "" {
		t.Fatalf("evidence construction: %v", err)
	}
	options := fixture.options(t)
	for index, cut := range cuts {
		input := fixture.artifact.Inputs[index]
		operator := options.Operators[cut.NoId]
		encoded, err := input.AttemptCutV2.CanonicalJSON(operator.Bounds)
		if err != nil || cut.ContentHash != headsummary.Hex32(sha256.Sum256(encoded)) || cut.NativeBlock != input.CutNativeBlock || cut.NativeBlockHash != input.CutNativeBlockHash {
			t.Fatalf("evidence points away from the published cut: %v", err)
		}
		current := map[connect.Id]FleetScoreKey{}
		for _, binding := range cut.Bindings {
			if binding.ClientId == excluded {
				t.Fatal("a different key acquired the provider's binding")
			}
			id, err := connect.ParseId(binding.ClientId)
			if err != nil {
				t.Fatal(err)
			}
			fleet, err := headsummary.ParseHex32(binding.FleetId)
			if err != nil {
				t.Fatal(err)
			}
			hotkey, err := headsummary.ParseHex32(binding.Hotkey)
			if err != nil {
				t.Fatal(err)
			}
			current[id] = FleetScoreKey{FleetID: fleet, Hotkey: hotkey, Generation: binding.Generation, UID: binding.Uid}
		}
		if !reflect.DeepEqual(current, operator.Measurement.CurrentBindingKVs) {
			t.Fatal("published eligibility differs from the actual measurement")
		}
		_, replayed, err := ReplayAttemptCutV2HeadWithPolicy(t.Context(), *input.AttemptCutV2, operator.Expected, fixture.artifact.Policy, operator.Bounds, AttemptCutV2HeadOptions{
			CurrentBindingKVs: current, MaxProviders: operator.Measurement.MaxProviders, MaxFleetPrefixes: operator.Measurement.MaxFleetPrefixes, Replay: operator.Measurement.Replay,
		})
		if err != nil || replayed.CompleteCount != 2 {
			t.Fatalf("published cut could not fully replay: %+v %v", replayed, err)
		}
	}
}

// The full measurement verifier allows a first intent to inherit prior EMA
// state. Its publisher must retain the validator score without claiming an
// empty bootstrap, including prior identities absent from the current census.
func TestHeadSummaryEvidenceDoesNotInventEmptyMigratedPrior(t *testing.T) {
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	raw, err := rawHeadEMAInputs(fixture.artifact.HeadEMA)
	if err != nil || len(raw) == 0 {
		t.Fatalf("fixture has no live raw head input: %v", err)
	}
	keypair := headSummaryTestKeypair(t)
	for _, migrated := range []bool{false, true} {
		store := &HeadEMAStore{values: map[string]headEMAEntry{}}
		if migrated {
			// A historical identity need not be live to affect future folds.
			key := FleetScoreKey{FleetID: [32]byte{0x74}, Hotkey: [32]byte{0x75}, Generation: 1, UID: 300}
			prior, err := encodeRationalJSON(big.NewRat(5, 1))
			if err != nil {
				t.Fatal(err)
			}
			store.values[key.String()] = headEMAEntry{Key: key, Numerator: prior.Numerator, Denominator: prior.Denominator}
		}
		_, records, err := store.foldWithLock(raw, fixture.artifact.Policy.Steering.HeadScoreEMA)
		if err != nil {
			t.Fatal(err)
		}
		fixture.artifact.HeadEMA = records
		verified, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
		if err != nil {
			t.Fatalf("valid first measurement with migrated=%v: %v", migrated, err)
		}
		fixture.want = verified.Decision
		encoded, err := canonicalReleaseMeasurementBytes(fixture.artifact)
		if err != nil {
			t.Fatal(err)
		}
		intent := fixture.intent(t, encoded)
		target := newHeadSummaryTestOperator(t, nil)
		finished := make(chan error, 1)
		uploader := startHeadSummaryUploader(t.Context(), keypair.PublicKey(), keypair.Sign, fixture.artifact.Policy.PolicyID, fixture.artifact.Policy.Steering.MaximumHeadFleets,
			[]*headSummaryTarget{{noID: 9, endpoint: target.server.URL + headsummary.Path, credential: headSummaryTestSessionGetter}},
			headSummaryHooks{afterUpload: func(_, _ uint64, err error) { finished <- err }})
		t.Cleanup(uploader.close)
		uploader.offer(intent, fixture.artifact)
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(headSummaryTestDeadline):
			t.Fatal("first measurement summary did not upload")
		}
		uploader.close()
		summary, err := headsummary.Decode(target.body(0))
		if err != nil || summary.PreviousMeasurementArtifactHash != "" || summary.EvidenceEmptyPrior == migrated || len(summary.Entries) == 0 {
			t.Fatalf("migrated=%v received incorrect bootstrap evidence: %+v %v", migrated, summary, err)
		}
	}
}

// Two operators get the same fleet result, but each receives only its own
// client census. An explicit builder barrier proves post-offer mutations and
// delayed signing cannot change the accepted cut or historical binding snapshot.
func TestHeadSummaryEvidenceKeepsItsCutAndOperatorScope(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	artifact := fixture.artifact
	artifact.PreviousArtifactHash = "sha256:" + headSummaryTestHash(0x71)[2:]
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	intent := fixture.intent(t, encoded)
	wantCuts, err := headSummaryEvidence(artifact)
	if err != nil {
		t.Fatal(err)
	}
	first, second := newHeadSummaryTestOperator(t, nil), newHeadSummaryTestOperator(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	releaseBuilder := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseBuilder)
	finished := make(chan error, 2)
	uploader := startHeadSummaryUploader(t.Context(), keypair.PublicKey(), keypair.Sign, artifact.Policy.PolicyID, artifact.Policy.Steering.MaximumHeadFleets,
		[]*headSummaryTarget{
			{noID: 9, endpoint: first.server.URL + headsummary.Path, credential: headSummaryTestSessionGetter},
			{noID: 10, endpoint: second.server.URL + headsummary.Path, credential: headSummaryTestSessionGetter},
		}, headSummaryHooks{
			beforeBuild: func() { enterOnce.Do(func() { close(entered); <-release }) },
			now:         func() time.Time { return time.Unix(1_800_000_000, 0) },
			afterUpload: func(_, _ uint64, err error) { finished <- err },
		})
	t.Cleanup(func() { releaseBuilder(); uploader.close() })
	(&ReleaseSteerer{headSummary: uploader}).offerHeadSummary(intent, artifact)
	select {
	case <-entered:
	case <-time.After(headSummaryTestDeadline):
		t.Fatal("evidence builder did not enter")
	}
	artifact.Inputs[0].CutNativeBlock++
	artifact.Inputs[0].AttemptCutV2.Root = headSummaryTestHash(0x77)
	artifact.Bindings[0].Generation++
	artifact.Policy.Deposit.Tiers[0].RateDenominator++
	releaseBuilder()
	for range 2 {
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(headSummaryTestDeadline):
			t.Fatal("evidence summary did not upload")
		}
	}
	for index, target := range []*headSummaryTestOperator{first, second} {
		summary, err := headsummary.Decode(target.body(0))
		if err != nil || summary.Schema != headsummary.SchemaV2 || summary.PreviousMeasurementArtifactHash != artifact.PreviousArtifactHash || summary.EvidenceRecipient != uint64(9+index) || !slices.Equal(summary.EvidenceOperators, []uint64{9, 10}) || len(summary.EvidenceCuts) != 1 {
			t.Fatalf("operator %d did not receive its signed evidence: %v", index, err)
		}
		if !reflect.DeepEqual(summary.EvidenceCuts[0], wantCuts[index]) {
			t.Fatalf("operator %d received a changed or foreign cut census", index)
		}
	}
}

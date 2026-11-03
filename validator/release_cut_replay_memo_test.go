//go:build linux || darwin

package validator

import (
	"context"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

const releaseCutReplayMemoTestOrigin = "https://operator.example"

// Each call's options read through one shared counter, under the configured
// public origin a production reader has, with their own fresh scratch.
func releaseCutReplayMemoTestOptions(options ReleaseMeasurementV2Options, reads *int) ReleaseMeasurementV2Options {
	observe := func(replay *AttemptCutV2ReplayOptions) {
		metadata, data := replay.ReadMetadata, replay.OpenData
		replay.origin = releaseCutReplayMemoTestOrigin
		replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			*reads++
			return metadata(ctx, hash, size)
		}
		replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			*reads++
			return data(ctx, kind, hash, size)
		}
	}
	for noID, operator := range options.Operators {
		observe(&operator.Measurement.Replay)
		options.Operators[noID] = operator
	}
	if options.Settlement != nil {
		for noID, operator := range options.Settlement.Operators {
			observe(&operator.Measurement.Replay)
			options.Settlement.Operators[noID] = operator
		}
	}
	return options
}

// One steering operation verifies the same artifact at its seal, its decision,
// the production stage, the envelope, intent admission and each lifecycle write.
// On 11-01 that was 14 replays of the cut and its terminal in one operation, and
// 2 more on every poll of a retained intent. The epoch's records are read once.
func TestReleaseCutReplayMemoReadsEachCutOnceAcrossAnOperation(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TerminalTestFixture(t, true)
	ctx := withReleaseCutReplayMemo(t.Context(), newReleaseCutReplayMemo())
	reads := 0
	next := func() ReleaseMeasurementV2Options { return releaseCutReplayMemoTestOptions(fixture.options(), &reads) }
	sealed, _, err := SealReleaseMeasurementArtifactV2(ctx, fixture.artifact, next())
	if err != nil {
		t.Fatal(err)
	}
	first := reads
	if first == 0 {
		t.Fatal("the first replay read no objects")
	}
	var want VerifiedReleaseMeasurementV2
	// The decision, the production stage, intent admission's two and the two of
	// each lifecycle write: the same bytes under the same authority.
	for attempt := range 8 {
		_, result, err := DecodeReleaseMeasurementArtifactV2(ctx, sealed, next())
		if err != nil {
			t.Fatal(err)
		}
		if attempt == 0 {
			want = result
		} else if !reflect.DeepEqual(result, want) {
			t.Fatal("a repeated verification returned a different result")
		}
	}
	raw, _, envelope, err := SealReleaseMeasurementEnvelopeV2(ctx, sealed, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0), next())
	if err != nil || len(raw) == 0 {
		t.Fatalf("envelope seal: %v", err)
	}
	if _, _, err := VerifyReleaseMeasurementEnvelopeV2(ctx, envelope, sealed, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, next()); err != nil {
		t.Fatalf("envelope verify: %v", err)
	}
	if reads != first {
		t.Fatalf("the operation read its cuts again: %d object reads for the first replay, %d after ten more verifications", first, reads)
	}
	// Without the memo in its context a caller still replays, as every caller
	// outside a production run does.
	if _, _, err := DecodeReleaseMeasurementArtifactV2(t.Context(), sealed, next()); err != nil {
		t.Fatal(err)
	}
	if reads != 2*first {
		t.Fatalf("a caller without a memo did not replay: %d reads, want %d", reads, 2*first)
	}
}

// A remembered result gives the same decision as a real replay, and nothing a
// caller does to its copy reaches the next caller.
func TestReleaseCutReplayMemoResultIsIsolatedAndEqual(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TerminalTestFixture(t, true)
	reads := 0
	next := func() ReleaseMeasurementV2Options { return releaseCutReplayMemoTestOptions(fixture.options(), &reads) }
	_, replayed, err := DecodeReleaseMeasurementArtifactV2(t.Context(), fixture.measurement, next())
	if err != nil {
		t.Fatal(err)
	}
	ctx := withReleaseCutReplayMemo(t.Context(), newReleaseCutReplayMemo())
	_, first, err := DecodeReleaseMeasurementArtifactV2(ctx, fixture.measurement, next())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, replayed) {
		t.Fatal("the memo's first pass differs from a plain replay")
	}
	for _, stats := range first.Decision.StatsByNO {
		for id, provider := range stats.Providers {
			clear(provider.EgressIPHashes)
			delete(stats.Providers, id)
		}
	}
	_, second, err := DecodeReleaseMeasurementArtifactV2(ctx, fixture.measurement, next())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, replayed) {
		t.Fatal("a caller's mutation reached the memo, or a remembered result differs from a plain replay")
	}
}

// A lineage join answers its prior checkpoint from what the replay saw, and a
// rewritten prefix is refused exactly as the replay's own visit refuses it.
func TestReleaseCutReplayMemoAnswersLineageCheckpoints(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	previous, err := canonicalReleaseMeasurementBytes(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	current := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	current.PreviousArtifactHash = ReleaseMeasurementContentHash(previous)
	ctx := withReleaseCutReplayMemo(t.Context(), newReleaseCutReplayMemo())
	reads := 0
	next := func() ReleaseMeasurementV2Options { return releaseCutReplayMemoTestOptions(fixture.options(t), &reads) }
	currentOptions := func() ReleaseMeasurementV2Options {
		options := next()
		options.Expected = releaseMeasurementV2Decision(current)
		return options
	}
	if _, err := VerifyReleaseMeasurementArtifactV2(ctx, current, currentOptions()); err != nil {
		t.Fatal(err)
	}
	first := reads
	result, err := VerifyReleaseMeasurementLineageV2(ctx, previous, next(), current, currentOptions())
	if err != nil || !reflect.DeepEqual(result.Decision, fixture.want) {
		t.Fatalf("lineage through remembered replays: %v", err)
	}
	if reads != first {
		t.Fatalf("the lineage join read remembered cuts again: %d then %d", first, reads)
	}
	cut := fixture.artifact.Inputs[0].AttemptCutV2
	hash, err := canonicalAttemptHex32("test root", cut.Root, false)
	if err != nil {
		t.Fatal(err)
	}
	entry := &releaseCutReplayMemoEntry{firstSequence: cut.Context.FirstSequence, hashes: make([]byte, 32*cut.RecordCount)}
	copy(entry.hashes[32*(cut.RecordCount-1):], hash[:])
	good := &releaseMeasurementV2Checkpoint{sequence: cut.LastSequence, root: cut.Root}
	if err := entry.answer(good); err != nil || !good.seen {
		t.Fatalf("the prefix's own root was refused: seen=%t %v", good.seen, err)
	}
	if err := entry.answer(good); err == nil {
		t.Fatal("a checkpoint answered twice must refuse, as its visit does")
	}
	other := append([]byte(nil), hash[:]...)
	other[0] ^= 1
	rewritten := &releaseMeasurementV2Checkpoint{sequence: cut.LastSequence, root: "0x" + hex.EncodeToString(other)}
	if err := entry.answer(rewritten); err == nil || !strings.Contains(err.Error(), "rewrites its authenticated prior prefix") || rewritten.seen {
		t.Fatalf("a rewritten prefix passed: %v", err)
	}
	for _, sequence := range []uint64{cut.Context.FirstSequence - 1, cut.LastSequence + 1} {
		absent := &releaseMeasurementV2Checkpoint{sequence: sequence, root: cut.Root}
		if err := entry.answer(absent); err != nil || absent.seen {
			t.Fatalf("sequence %d is outside the cut: seen=%t %v", sequence, absent.seen, err)
		}
	}
}

// Everything a verdict depends on changes the digest, and only the namespace
// and the reader functions do not.
func TestReleaseCutReplayMemoDigestCoversEveryInput(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	reads := 0
	options := releaseCutReplayMemoTestOptions(fixture.options(t), &reads)
	input := fixture.artifact.Inputs[0]
	operator := options.Operators[input.NoID]
	type inputs struct {
		measurement ReleaseStatsMeasurement
		cut         AttemptCutV2
		expected    AttemptCutV2Context
		bounds      AttemptCutV2Bounds
		options     AttemptCutV2MeasurementOptions
	}
	base := func() inputs {
		owned := operator.Measurement
		owned.Replay.ServerKeys = maps.Clone(owned.Replay.ServerKeys)
		owned.CurrentBindingKVs = maps.Clone(owned.CurrentBindingKVs)
		return inputs{measurement: input.Stats, cut: *input.AttemptCutV2, expected: operator.Expected, bounds: operator.Bounds, options: owned}
	}
	digest := func(value inputs) [32]byte {
		t.Helper()
		result, ok := releaseCutReplayMemoDigest(value.measurement, value.cut, value.expected, fixture.artifact.Policy, value.bounds, value.options)
		if !ok {
			t.Fatal("a production replay must be memoizable")
		}
		return result
	}
	want := digest(base())
	if digest(base()) != want {
		t.Fatal("the digest is not deterministic")
	}
	same := base()
	same.options.Replay.ScratchDirectory = "/elsewhere"
	same.options.Replay.ReadMetadata, same.options.Replay.OpenData = nil, nil
	if digest(same) != want {
		t.Fatal("the scratch namespace or a reader function changed the digest")
	}
	for name, change := range map[string]func(*inputs){
		"signed cut": func(value *inputs) {
			value.cut.Signature = append([]byte{value.cut.Signature[0] ^ 1}, value.cut.Signature[1:]...)
		},
		"cut extent":    func(value *inputs) { value.cut.LastSequence++ },
		"context":       func(value *inputs) { value.expected.EgressGeneration++ },
		"statistics":    func(value *inputs) { value.measurement.Config.AMin++ },
		"bounds":        func(value *inputs) { value.bounds.MaxHeaderBytes++ },
		"replay bounds": func(value *inputs) { value.options.Replay.Bounds.MaxScratchBytes++ },
		"config":        func(value *inputs) { value.options.ExpectedConfig.AMin++ },
		"max providers": func(value *inputs) { value.options.MaxProviders++ },
		"max egress":    func(value *inputs) { value.options.MaxEgressHashes++ },
		"max prefixes":  func(value *inputs) { value.options.MaxFleetPrefixes++ },
		"origin":        func(value *inputs) { value.options.Replay.origin = "https://other.example" },
		"server key": func(value *inputs) {
			for id, key := range value.options.Replay.ServerKeys {
				key = slices.Clone(key)
				key[0] ^= 1
				value.options.Replay.ServerKeys[id] = key
				break
			}
		},
		"binding": func(value *inputs) {
			value.options.CurrentBindingKVs = maps.Clone(value.options.CurrentBindingKVs)
			if value.options.CurrentBindingKVs == nil {
				value.options.CurrentBindingKVs = map[connect.Id]FleetScoreKey{}
			}
			value.options.CurrentBindingKVs[connect.NewId()] = FleetScoreKey{UID: 7}
		},
	} {
		changed := base()
		change(&changed)
		if digest(changed) == want {
			t.Fatalf("%s did not change the digest", name)
		}
	}
	policy := fixture.artifact.Policy
	policy.Verify.TrailDepth++
	if got, ok := releaseCutReplayMemoDigest(input.Stats, *input.AttemptCutV2, operator.Expected, policy, operator.Bounds, operator.Measurement); !ok || got == want {
		t.Fatal("the policy did not change the digest")
	}
	noOrigin := base()
	noOrigin.options.Replay.origin = ""
	if _, ok := releaseCutReplayMemoDigest(noOrigin.measurement, noOrigin.cut, noOrigin.expected, fixture.artifact.Policy, noOrigin.bounds, noOrigin.options); ok {
		t.Fatal("a reader bound to no origin has no identity to remember")
	}
}

// A failed or canceled replay is never remembered, and a remembered verdict
// never answers a canceled caller.
func TestReleaseCutReplayMemoRemembersOnlyCompletedReplays(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	encoded, err := canonicalReleaseMeasurementBytes(fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	memo := newReleaseCutReplayMemo()
	ctx := withReleaseCutReplayMemo(t.Context(), memo)
	reads := 0
	failure := errors.New("a read that failed")
	failing := releaseCutReplayMemoTestOptions(fixture.options(t), &reads)
	for noID, operator := range failing.Operators {
		operator.Measurement.Replay.OpenData = func(context.Context, string, string, uint64) (io.ReadCloser, error) { return nil, failure }
		failing.Operators[noID] = operator
	}
	if _, _, err := DecodeReleaseMeasurementArtifactV2(ctx, encoded, failing); !errors.Is(err, failure) {
		t.Fatalf("the failed read vanished: %v", err)
	}
	if len(memo.entries) != 0 {
		t.Fatalf("a failed replay was remembered: %d entries", len(memo.entries))
	}
	reads = 0
	if _, _, err := DecodeReleaseMeasurementArtifactV2(ctx, encoded, releaseCutReplayMemoTestOptions(fixture.options(t), &reads)); err != nil || reads == 0 {
		t.Fatalf("the retry did not replay: %d reads: %v", reads, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	first := reads
	if _, _, err := DecodeReleaseMeasurementArtifactV2(canceled, encoded, releaseCutReplayMemoTestOptions(fixture.options(t), &reads)); !errors.Is(err, context.Canceled) || reads != first {
		t.Fatalf("a canceled caller got a verdict: %d reads: %v", reads-first, err)
	}
	// A reader bound to no origin has no identity to remember.
	reads = 0
	plain := fixture.options(t)
	for noID, operator := range plain.Operators {
		metadata := operator.Measurement.Replay.ReadMetadata
		operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
			reads++
			return metadata(ctx, hash, size)
		}
		plain.Operators[noID] = operator
	}
	if _, _, err := DecodeReleaseMeasurementArtifactV2(ctx, encoded, plain); err != nil || reads == 0 {
		t.Fatalf("a reader without an origin was remembered: %d reads: %v", reads, err)
	}
}

// The memo keeps a fixed number of whole entries, oldest first out.
func TestReleaseCutReplayMemoBound(t *testing.T) {
	t.Parallel()
	memo := newReleaseCutReplayMemo()
	for index := range releaseCutReplayMemoEntries + 3 {
		memo.remember([32]byte{byte(index)}, &releaseCutReplayMemoEntry{})
	}
	if len(memo.entries) != releaseCutReplayMemoEntries || len(memo.order) != releaseCutReplayMemoEntries {
		t.Fatalf("%d entries, %d ordered", len(memo.entries), len(memo.order))
	}
	if memo.lookup([32]byte{0}) != nil || memo.lookup([32]byte{byte(releaseCutReplayMemoEntries + 2)}) == nil {
		t.Fatal("the oldest entry must leave first")
	}
	memo.remember([32]byte{byte(releaseCutReplayMemoEntries + 2)}, &releaseCutReplayMemoEntry{})
	if len(memo.order) != releaseCutReplayMemoEntries {
		t.Fatal("remembering the same digest twice grew the census")
	}
}

// Production owns one memo for the run. A test helper cannot stand in for it.
func TestReleaseRunOwnsTheCutReplayMemo(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "release_run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if identifier, ok := call.Fun.(*ast.Ident); ok && identifier.Name == "withReleaseCutReplayMemo" {
				found = true
			}
		}
		return true
	})
	if !found {
		t.Fatal("the release run does not own a cut replay memo")
	}
}

// The read after each lifecycle write is what timed out first: four replays
// under a fixed 290-second attempt, restarting from nothing each time. With the
// memo the retained intent's reads touch no attempt object after the first.
func TestIntentLifecycleReadsAttemptObjectsOnceWithTheMemo(t *testing.T) {
	lifecycle := func(ctx context.Context) int64 {
		fixture := newProductionContinuationTestFixture(t)
		installProductionContinuationNative(t, fixture)
		store := fixture.steerer.intents
		intent, err := store.beginV2(ctx, *fixture.intent)
		if err != nil {
			t.Fatal(err)
		}
		first := fixture.artifactReads.Load()
		if first == 0 {
			t.Fatal("beginning an intent read no attempt objects")
		}
		for range 3 {
			if err := store.updateV2(ctx, intent.VectorHash, "pending", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := store.currentV2(ctx); err != nil {
				t.Fatal(err)
			}
		}
		return fixture.artifactReads.Load() - first
	}
	if again := lifecycle(t.Context()); again == 0 {
		t.Fatal("the control read nothing after its first replay, so this proves nothing")
	}
	if again := lifecycle(withReleaseCutReplayMemo(t.Context(), newReleaseCutReplayMemo())); again != 0 {
		t.Fatalf("a retained intent's reads replayed its cuts again: %d more object reads", again)
	}
}

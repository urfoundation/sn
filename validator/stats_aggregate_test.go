//go:build linux || darwin

package validator

// Aggregate fixtures use genuine M8 production trails, the real durable
// ledger, writer, signed header and full public replay. Test-only maps retain
// independent expected censuses; production aggregate rows never do.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

// Explicit local fixture ceilings do not alter any production admission cap.
func statsAggregateTestBounds() statsAggregateBounds {
	return statsAggregateBounds{MaxHeaderBytes: 16 * 1024, MaxProviders: 128, MaxEgressHashes: 256, MaxEgressClaims: 256, MaxStorageBytes: 64 * 1024 * 1024, MaxStorageFiles: 256}
}

// A fixture owns its source, staged transport objects and independent aggregate.
type statsAggregateTestFixture struct {
	source  *attemptCutV2SealTestFixture
	cut     AttemptCutV2
	seal    AttemptCutV2SealOptions
	objects *attemptCutV2SealTestObjects
	store   *statsAggregateStore
	path    string
	config  StatsConfig
	bounds  statsAggregateBounds
}

// Each new fixture traverses the existing complete M8 writer/sealer workflow.
func newStatsAggregateTestFixture(t *testing.T, complete, failed int, hooks statsAggregateHooks) *statsAggregateTestFixture {
	t.Helper()
	source := newAttemptCutV2SealTestFixture(t, 8, complete, failed)
	return newStatsAggregateTestFixtureFromSource(t, source, hooks)
}

// Both explicitly provisioned fixture sizes retain identical full sealing,
// private aggregate construction and cleanup; only ledger capacity differs.
func newStatsAggregateTestFixtureFromSource(t *testing.T, source *attemptCutV2SealTestFixture, hooks statsAggregateHooks) *statsAggregateTestFixture {
	t.Helper()
	seal, objects := newAttemptCutV2SealTestOptions(t, source)
	cut, _, err := SealAttemptCutV2(context.Background(), source.ledger, source.expected, source.policy, source.key, source.bounds, seal)
	if err != nil || cut == nil {
		t.Fatalf("real M8 aggregate source: %v", err)
	}
	config := source.engine.stats.cfg
	bounds := statsAggregateTestBounds()
	path := filepath.Join(newAttemptLedgerDiskTestStateDir(t), "aggregate")
	store, err := openStatsAggregateStore(context.Background(), path, source.expected, source.policy, config, bounds, hooks)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &statsAggregateTestFixture{source: source, cut: *cut, seal: seal, objects: objects, store: store, path: path, config: config, bounds: bounds}
}

// Every public replay attempt owns a fresh scratch namespace.
func (self *statsAggregateTestFixture) replayOptions(t *testing.T) AttemptCutV2ReplayOptions {
	t.Helper()
	return AttemptCutV2ReplayOptions{Bounds: self.source.replay, ScratchDirectory: filepath.Join(t.TempDir(), "aggregate-replay"), ServerKeys: self.seal.ServerKeys, ReadMetadata: self.seal.ReadMetadata, OpenData: self.seal.OpenData}
}

// Collection is test-only and checks canonical order before creating lookup maps.
func statsAggregateTestRows(t *testing.T, store *statsAggregateStore, generation uint64) []statsAggregateProvider {
	t.Helper()
	var rows []statsAggregateProvider
	err := store.WalkProviders(context.Background(), generation, func(row statsAggregateProvider) error {
		if len(rows) != 0 && !rows[len(rows)-1].ClientID.LessThan(row.ClientID) {
			return errors.New("aggregate provider order is not canonical")
		}
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// Production Stats provides an independent oracle over exactly the same signed
// records, including failed denominators and the seed-hop egress exclusion.
func assertStatsAggregateTestParity(t *testing.T, fixture *statsAggregateTestFixture, head statsAggregateHead) {
	t.Helper()
	legacy := fixture.source.engine.stats
	rows := statsAggregateTestRows(t, fixture.store, head.Generation)
	measurement := legacy.currentReleaseStatsMeasurement()
	if len(rows) != len(measurement.Providers) {
		t.Fatalf("provider census %d != %d", len(rows), len(measurement.Providers))
	}
	ppm, ema := legacy.QualityPPM(), legacy.Quality()
	for index, row := range rows {
		expected := measurement.Providers[index]
		if row.ClientID.String() != expected.ClientID || row.Window.Assignments != expected.Assignments || row.Window.Confirmations != expected.Confirmations || !reflect.DeepEqual(row.Window.LatencyBuckets[:], expected.LatencyBuckets) || row.HasPriorQuality != expected.HasPriorQuality || row.PriorQualityPPM != expected.PriorQualityPPM {
			t.Fatalf("complete provider row %s differs from existing Stats", row.ClientID)
		}
		gotPPM, gotHasPPM, gotEMA, gotHasEMA, err := row.quality(fixture.config)
		wantPPM, wantHasPPM := ppm[row.ClientID]
		wantEMA, wantHasEMA := ema[row.ClientID]
		if err != nil || gotPPM != wantPPM || gotHasPPM != wantHasPPM || math.Float64bits(gotEMA) != math.Float64bits(wantEMA) || gotHasEMA != wantHasEMA {
			t.Fatalf("provider %s exact/legacy quality differs: %v", row.ClientID, err)
		}
	}
	hashes := map[connect.Id]map[[32]byte]bool{}
	var previousClient connect.Id
	var previousHash [32]byte
	err := fixture.store.WalkEgress(context.Background(), head.Generation, func(clientID connect.Id, hash [32]byte) error {
		if previousClient != (connect.Id{}) && (clientID.LessThan(previousClient) || clientID == previousClient && bytes.Compare(previousHash[:], hash[:]) >= 0) {
			return errors.New("egress order is not canonical")
		}
		previousClient, previousHash = clientID, hash
		if hashes[clientID] == nil {
			hashes[clientID] = map[[32]byte]bool{}
		}
		hashes[clientID][hash] = true
		return nil
	})
	if err != nil || !reflect.DeepEqual(hashes, legacy.EgressIpHashes()) {
		t.Fatalf("complete native egress differs: %v", err)
	}
	var claims []AttemptEgressClaim
	if err := fixture.store.WalkClaims(context.Background(), head.Generation, func(claim AttemptEgressClaim) error { claims = append(claims, claim); return nil }); err != nil {
		t.Fatal(err)
	}
	wantClaims, err := AttemptCutEgressClaims(&AttemptLedgerCut{EgressFirstSequence: fixture.source.expected.EgressFirstSequence, Records: fixture.source.recordTs})
	if err != nil || !reflect.DeepEqual(claims, wantClaims) {
		t.Fatalf("generation-separated egress claims differ: %v", err)
	}
}

// Pending checkpoints contribute no repeated counter delta; every terminal,
// including real transport failure, contributes its full signed assignments.
func TestStatsAggregateRealM8CompleteAndFailedParity(t *testing.T) {
	t.Parallel()
	writes := map[byte]int{}
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{PointStaged: func(kind byte, keyBytes, valueBytes int) error {
		writes[kind]++
		if keyBytes > 73 || valueBytes > statsAggregateProviderBytes {
			return errors.New("point update exceeded fixed row sizes")
		}
		return nil
	}})
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	wantAssignments := 0
	for _, record := range fixture.source.recordTs {
		if record.Disposition != AttemptDispositionPending {
			wantAssignments += len(record.Assignments)
		}
	}
	if head.LastAppliedSequence != fixture.cut.LastSequence || head.LastAppliedRoot != fixture.cut.Root || writes['p'] != wantAssignments || writes['e'] != 7 || writes['c'] != 7 {
		t.Fatalf("actual terminal point work=%v, assignments=%d, head=%+v", writes, wantAssignments, head)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}

// Same-context retries still authenticate every input byte but never recount
// already applied terminal rows or move either independent clock.
func TestStatsAggregateSameCutRetryDoesNotRecount(t *testing.T) {
	t.Parallel()
	pointWrites := 0
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{PointStaged: func(byte, int, int) error { pointWrites++; return nil }})
	first, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	beforeWrites, beforeReads := pointWrites, fixture.objects.reads
	second, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	if pointWrites != beforeWrites || fixture.objects.reads <= beforeReads || second.Generation != first.Generation+1 {
		t.Fatal("same-cut retry skipped public replay or repeated terminal point updates")
	}
	second.Generation = first.Generation
	if second != first {
		t.Fatal("same-cut retry changed counters, roots or either clock")
	}
	second.Generation++
	assertStatsAggregateTestParity(t, fixture, second)
}

// One native rotation clears only native evidence. Exposure and both prior EMA
// representations remain exactly unchanged; no settlement fold is inferred.
func TestStatsAggregateNativeRotationPreservesSettlementWindow(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{})
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{RotateNative: true})
	if err != nil {
		t.Fatal(err)
	}
	if head.SettlementEpoch != fixture.source.expected.Boundary.SettlementEpoch || head.SettlementFirstSequence != 1 || head.EgressGeneration != fixture.source.expected.EgressGeneration+1 || head.EgressFirstSequence != fixture.cut.LastSequence+1 || head.EgressHashCount != 0 || head.EgressClaimCount != 0 {
		t.Fatalf("native rotation changed wrong clock: %+v", head)
	}
	rows := statsAggregateTestRows(t, fixture.store, head.Generation)
	for _, row := range rows {
		a, c := fixture.source.engine.stats.WindowCounts(row.ClientID)
		if row.Window.Assignments != a || row.Window.Confirmations != c || !row.WindowPresent || row.HasPriorQuality || row.HasPriorEMA {
			t.Fatal("native rotation folded or erased settlement exposure")
		}
	}
	visits := 0
	if err := fixture.store.WalkEgress(context.Background(), head.Generation, func(connect.Id, [32]byte) error { visits++; return nil }); err != nil || visits != 0 {
		t.Fatalf("rotated native evidence remained visible: %v", err)
	}
}

// The attempt-backed terminal operation requests both transitions explicitly.
// Sparse rows retain their identities and never gain invented EMA priors.
func TestStatsAggregateTerminalFoldPreservesSparseProviderIdentity(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{})
	expectedClientIDs := map[connect.Id]bool{}
	complete, failed := 0, 0
	for _, record := range fixture.source.recordTs {
		if record.Disposition == AttemptDispositionPending {
			continue
		}
		if record.Disposition == AttemptDispositionComplete {
			complete++
		} else {
			failed++
		}
		for _, assignment := range record.Assignments {
			expectedClientIDs[assignment.NextHop] = true
		}
	}
	if complete != 1 || failed != 1 || len(expectedClientIDs) < 7 {
		t.Fatalf("real sparse signed-terminal census differs: complete=%d failed=%d providers=%d", complete, failed, len(expectedClientIDs))
	}
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{RotateNative: true, ToSettlementEpoch: 43})
	if err != nil {
		t.Fatal(err)
	}
	if head.SettlementEpoch != 43 || head.SettlementFirstSequence != fixture.cut.LastSequence+1 || head.SettlementPriorRoot != fixture.cut.Root || head.EgressGeneration != fixture.source.expected.EgressGeneration+1 || head.EgressFirstSequence != fixture.cut.LastSequence+1 {
		t.Fatalf("terminal clocks differ: %+v", head)
	}
	rows := statsAggregateTestRows(t, fixture.store, head.Generation)
	if len(rows) != len(expectedClientIDs) {
		t.Fatalf("terminal fold dropped sparse identities: got=%d signed-union=%d", len(rows), len(expectedClientIDs))
	}
	for _, row := range rows {
		if !expectedClientIDs[row.ClientID] || row.WindowPresent || row.Window != (ProviderWindow{}) || row.HasPriorQuality || row.HasPriorEMA {
			t.Fatal("sparse fold invented quality or retained an old window")
		}
		delete(expectedClientIDs, row.ClientID)
	}
	if len(expectedClientIDs) != 0 {
		t.Fatal("sparse fold omitted a signed terminal provider")
	}
}

// These scalar controls use the unchanged policy A_min and exact production
// math, with an independent existing-engine preview for both representations.
func TestStatsAggregateQualityPreservesSeparateExactAndLegacyPriors(t *testing.T) {
	t.Parallel()
	config := StatsConfig{AMin: exactPolicy(t).Verify.ReliabilityAMin}.withDefaults()
	clientID := connect.NewId()
	cases := []statsAggregateProvider{
		{ClientID: clientID},
		{ClientID: clientID, WindowPresent: true, Window: ProviderWindow{Assignments: config.AMin - 1}, HasPriorQuality: true, PriorQualityPPM: 0, HasPriorEMA: true, PriorEMA: 0},
		{ClientID: clientID, WindowPresent: true, Window: ProviderWindow{Assignments: config.AMin - 1}, HasPriorQuality: true, PriorQualityPPM: 123456, HasPriorEMA: true, PriorEMA: 0.123456789},
		{ClientID: clientID, WindowPresent: true, Window: ProviderWindow{Assignments: config.AMin}, HasPriorQuality: true, PriorQualityPPM: 333333, HasPriorEMA: true, PriorEMA: 1.0 / 3.0},
		{ClientID: clientID, WindowPresent: true, Window: ProviderWindow{Assignments: config.AMin}, HasPriorEMA: true, PriorEMA: 0.9},
	}
	for _, row := range cases {
		legacy := NewStatsEngine(config)
		if row.WindowPresent {
			window := row.Window
			legacy.window[clientID] = &window
		}
		if row.HasPriorQuality {
			legacy.emaPPM[clientID] = row.PriorQualityPPM
		}
		if row.HasPriorEMA {
			legacy.ema[clientID] = row.PriorEMA
		}
		ppm, hasPPM, ema, hasEMA, err := row.quality(config)
		wantPPM, wantHasPPM := legacy.QualityPPM()[clientID]
		wantEMA, wantHasEMA := legacy.Quality()[clientID]
		if err != nil || ppm != wantPPM || hasPPM != wantHasPPM || math.Float64bits(ema) != math.Float64bits(wantEMA) || hasEMA != wantHasEMA {
			t.Fatalf("independent prior comparison differs for %+v: %v", row, err)
		}
		decoded, err := decodeStatsAggregateProvider(clientID, row.encode(), config)
		if err != nil || decoded != row {
			t.Fatalf("fixed row lost independent EMA bits: %v", err)
		}
	}
}

// Histogram, prior and config overflows are refusals, never saturated scores.
func TestStatsAggregateQualityRejectsOverflowAndMalformedRows(t *testing.T) {
	t.Parallel()
	config := StatsConfig{AMin: exactPolicy(t).Verify.ReliabilityAMin}.withDefaults()
	clientID := connect.NewId()
	overflow := statsAggregateProvider{ClientID: clientID, WindowPresent: true, Window: ProviderWindow{Assignments: ^uint64(0), Confirmations: ^uint64(0)}}
	overflow.Window.LatencyBuckets[0], overflow.Window.LatencyBuckets[1] = ^uint64(0), 1
	cases := []statsAggregateProvider{
		overflow,
		{ClientID: clientID, WindowPresent: true, Window: ProviderWindow{Assignments: 1, Confirmations: 2}},
		{ClientID: clientID, HasPriorQuality: true, PriorQualityPPM: 1_000_001},
		{ClientID: clientID, PriorQualityPPM: 1},
		{ClientID: clientID, HasPriorEMA: true, PriorEMA: math.NaN()},
		{ClientID: clientID, HasPriorEMA: true, PriorEMA: math.Inf(1)},
		{ClientID: clientID, PriorEMA: 0.25},
		{ClientID: clientID, Window: ProviderWindow{Assignments: 1}},
	}
	for _, row := range cases {
		if err := row.validate(config); err == nil {
			t.Fatalf("invalid row accepted: %+v", row)
		}
	}
	oversized := config
	oversized.AlphaDenominator = ^uint64(0)
	if _, _, _, _, err := cases[0].quality(oversized); err == nil {
		t.Fatal("overflowing quality policy accepted")
	}
	raw := statsAggregateProvider{ClientID: clientID}.encode()
	for _, candidate := range [][]byte{raw[:len(raw)-1], append(bytes.Clone(raw), 0)} {
		if _, err := decodeStatsAggregateProvider(clientID, candidate, config); err == nil {
			t.Fatal("non-fixed-width provider row accepted")
		}
	}
}

// Existing domain/config authority is checked before any public object read or
// scratch create, even when the candidate header itself has a genuine signature.
func TestStatsAggregateRejectsChangedDomainAndClockBeforeReplay(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	for _, change := range []func(*AttemptCutV2Context){
		func(context *AttemptCutV2Context) { context.Identity.NoID++ },
		func(context *AttemptCutV2Context) { context.EgressGeneration++ },
		func(context *AttemptCutV2Context) { context.Boundary.EVMBlockHash = attemptHex32([32]byte{0x99}) },
	} {
		expected := fixture.source.expected
		change(&expected)
		cut := fixture.cut
		cut.Context = expected
		var err error
		cut.Signature, err = cut.Sign(fixture.source.key, fixture.source.bounds)
		if err != nil {
			t.Fatal(err)
		}
		options := fixture.replayOptions(t)
		beforeReads := fixture.objects.reads
		if _, err := fixture.store.Replay(context.Background(), cut, expected, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{}); err == nil {
			t.Fatal("changed namespace or native clock admitted")
		}
		if fixture.objects.reads != beforeReads {
			t.Fatal("wrong namespace reached public object I/O")
		}
		if _, err := os.Lstat(options.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("wrong namespace mutated replay scratch: %v", err)
		}
	}
}

// Wrong decoded policy cannot borrow a candidate's legal generic depth.
func TestStatsAggregateRejectsPolicyMismatchBeforeScratch(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{})
	policy := fixture.source.policy
	policy.Verify.TrailDepth = 4
	options := fixture.replayOptions(t)
	beforeReads := fixture.objects.reads
	if _, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, policy, fixture.source.bounds, options, statsAggregateAdvance{}); err == nil || !strings.Contains(err.Error(), "decoded policy") {
		t.Fatalf("mismatched authenticated policy accepted: %v", err)
	}
	if fixture.objects.reads != beforeReads {
		t.Fatal("wrong decoded policy reached data I/O")
	}
	if _, err := os.Lstat(options.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrong decoded policy created scratch: %v", err)
	}
}

// Visitors own detached values and cannot alter a future committed read.
func TestStatsAggregateIteratorsOwnRowsAndBindGeneration(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	before := statsAggregateTestRows(t, fixture.store, head.Generation)
	if err := fixture.store.WalkProviders(context.Background(), head.Generation, func(row statsAggregateProvider) error {
		row.ClientID = connect.Id{}
		row.Window.LatencyBuckets[0] = ^uint64(0)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, statsAggregateTestRows(t, fixture.store, head.Generation)) {
		t.Fatal("provider visitor mutated durable state")
	}
	visits := 0
	if err := fixture.store.WalkProviders(context.Background(), head.Generation+1, func(statsAggregateProvider) error { visits++; return nil }); err == nil || visits != 0 {
		t.Fatal("stale generation reached a visitor")
	}
	if err := fixture.store.WalkClaims(context.Background(), head.Generation, func(claim AttemptEgressClaim) error { claim.Binding.Generation++; claim.EgressIPHash = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}

// Header binding and row checks survive a real backend close and reopen.
func TestStatsAggregateReopenPreservesCompleteCensusAndPriors(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{})
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	before := statsAggregateTestRows(t, fixture.store, head.Generation)
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got, err := reopened.Head(context.Background())
	if err != nil || got != head || !reflect.DeepEqual(before, statsAggregateTestRows(t, reopened, head.Generation)) {
		t.Fatalf("reopen differs: %v", err)
	}
	fixture.store = reopened
	assertStatsAggregateTestParity(t, fixture, head)
}

// A complete genuinely signed alternative chain may be valid in isolation but
// cannot replace the aggregate's already-applied root under the same identity.
func TestStatsAggregateRejectsFullySignedAppliedPrefixReplacement(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 0, statsAggregateHooks{})
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil {
		t.Fatal(err)
	}
	records := make([]AttemptRecord, len(fixture.source.recordTs))
	for index, original := range fixture.source.recordTs {
		record, err := cloneAttemptRecord(original)
		if err != nil {
			t.Fatal(err)
		}
		record.Boundary.EVMBlock--
		records[index] = record
	}
	records = attemptReplayV2TestRechain(t, records, fixture.source.key)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, fixture.source.key, nil, nil)
	cut.Context = fixture.source.expected
	cut.Signature, err = cut.Sign(fixture.source.key, fixture.source.bounds)
	if err != nil {
		t.Fatal(err)
	}
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, fixture.source.server.serverPublicKeys())
	if _, err := ReplayAttemptCutV2WithPolicy(context.Background(), cut, cut.Context, fixture.source.policy, fixture.source.bounds, options); err != nil {
		t.Fatalf("signed alternative control is invalid: %v", err)
	}
	options = attemptReplayV2TestOptions(t, recordStream, proofStream, fixture.source.server.serverPublicKeys())
	if _, err := fixture.store.Replay(context.Background(), cut, cut.Context, fixture.source.policy, fixture.source.bounds, options, statsAggregateAdvance{}); err == nil || !strings.Contains(err.Error(), "prior applied root") {
		t.Fatalf("signed applied-root replacement accepted: %v", err)
	}
	after, err := fixture.store.Head(context.Background())
	if err != nil || after != head {
		t.Fatalf("rejected prefix replacement changed visible head: %v", err)
	}
}

// Test-only JSON captures prove caller ownership across full public replay.
func TestStatsAggregateReplayDoesNotMutateSignedInputs(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{})
	before, err := json.Marshal(fixture.cut)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[byte][]byte{}
	for id, key := range fixture.seal.ServerKeys {
		keys[id] = bytes.Clone(key)
	}
	if _, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{}); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(fixture.cut)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("signed header mutated: %v", err)
	}
	for id, key := range fixture.seal.ServerKeys {
		if !bytes.Equal(key, keys[id]) {
			t.Fatal("server public key input mutated")
		}
	}
}

// Confirms the independently sorted raw-id order is the same as public ids.
func TestStatsAggregateProviderKeyOrderingMatchesCanonicalIDs(t *testing.T) {
	t.Parallel()
	ids := []connect.Id{{0xff}, {0x01}, {0x01, 0xff}, {0x01, 0x01}}
	sort.Slice(ids, func(i, j int) bool { return ids[i].LessThan(ids[j]) })
	for index := 1; index < len(ids); index++ {
		if bytes.Compare(statsAggregateProviderKey(ids[index-1]), statsAggregateProviderKey(ids[index])) >= 0 || ids[index-1].String() >= ids[index].String() {
			t.Fatal("private key order differs from canonical provider order")
		}
	}
}

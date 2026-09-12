//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

func TestReleaseHistoryAdoptionV2BridgesHeadEMAOnceWithoutReset(t *testing.T) {
	t.Parallel()
	limits := headEMAStoreV2TestLimits()
	store, err := NewHeadEMAStoreV2(t.Context(), newReleaseHeadV2TestStateDir(t), limits)
	if err != nil { t.Fatal(err) }
	key := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 7}
	alpha := protocol.Rational{Numerator: 1, Denominator: 2}
	if _, _, err := store.FoldForEpochV2(t.Context(), 1405, map[FleetScoreKey]*big.Rat{key: big.NewRat(4, 1)}, alpha); err != nil { t.Fatal(err) }
	before, err := os.ReadFile(store.path)
	if err != nil { t.Fatal(err) }
	owner := &releaseHistoryAdoptionV2{request: ReleaseHistoryAdoptionV2{LastNativeEpoch: 1405, FirstNativeEpoch: 1410}}
	store.v2.historyAdoption = owner
	raw := map[FleetScoreKey]*big.Rat{key: big.NewRat(2, 1)}
	for _, epoch := range []uint64{1407, 1409, 1411} {
		if _, _, err := store.PreviewForEpochV2(t.Context(), epoch, raw, alpha); err == nil { t.Fatalf("unapproved bridge epoch %d passed", epoch) }
	}
	got, records, err := store.PreviewForEpochV2(t.Context(), 1410, raw, alpha)
	if err != nil || got[7].Cmp(big.NewRat(3, 1)) != 0 { t.Fatalf("one real EMA fold differs: %v %v", got, err) }
	compact, compactRecords, err := store.previewForEpochV2(t.Context(), 1410, raw, alpha, limits.MaxEntries, limits.MaxControlBytes)
	if err != nil || !reflect.DeepEqual(got, compact) || !equalHeadEMAFolds(records, compactRecords) { t.Fatalf("compact bridge disagrees: %v", err) }
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(before, after) { t.Fatalf("bridge preview changed durable EMA: %v", err) }
	if err := store.CommitForEpochV2(t.Context(), 1410, records, alpha); err != nil { t.Fatal(err) }
	restarted, err := NewHeadEMAStoreV2(t.Context(), filepath.Dir(store.path), limits)
	if err != nil { t.Fatal(err) }
	restarted.v2.historyAdoption = owner
	if restarted.allowsHeadEMAEpochGaps() || restarted.allowsHeadEMAEpochGapTo(1412) { t.Fatal("restart widened the consumed bridge") }
	if _, _, err := restarted.PreviewForEpochV2(t.Context(), 1412, raw, alpha); err == nil { t.Fatal("future skipped epoch passed after adoption") }
	if _, _, err := restarted.PreviewForEpochV2(t.Context(), 1411, raw, alpha); err != nil { t.Fatalf("strict consecutive successor failed: %v", err) }
}

// Four genuine empty settlement closures span the actual 1401/302→1404/306
// shape. Empty streams contain no fabricated remote objects. Full strict replay
// must still authenticate each interior signature and independent cut context.
func TestReleaseHistoryAdoptionV2ReplaysEveryInteriorTerminal(t *testing.T) {
	t.Parallel()
	previous := newProvisionalMeasurementLineageV2Initial(t)
	previousBytes, err := canonicalReleaseMeasurementBytes(previous.artifact)
	if err != nil { t.Fatal(err) }
	history := &releaseEvidenceV2StartupHistory{terminals: map[uint64]*AttemptSettlementClosureV2{}, terminalContexts: map[uint64]map[uint64]AttemptCutV2Context{}, keys: map[uint64]map[byte]ed25519.PublicKey{}}
	current := previous
	var last *releaseMeasurementV2SettlementTestFixture
	for step := 0; step < 4; step++ {
		var terminalInputs []*attemptCutV2StatsTestFixture
		contexts := map[uint64]AttemptCutV2Context{}
		for _, input := range current.artifact.Inputs {
			operator := current.operators[input.NoID]
			terminalInputs = append(terminalInputs, &attemptCutV2StatsTestFixture{seal: operator.seal, cut: *input.AttemptCutV2, measurement: input.Stats, metadata: operator.metadata, data: operator.data})
			contexts[input.NoID] = operator.seal.expected
		}
		last = newReleaseMeasurementV2SettlementTestFromTerminal(t, current, terminalInputs, step == 3, 1)
		history.terminals[current.artifact.SettlementEpoch] = last.current.artifact.SettlementClosureV2
		history.terminalContexts[current.artifact.SettlementEpoch] = contexts
		current = last.current
	}
	current.artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(previousBytes)
	first := previous.operators[9].seal
	cfg := ReleaseConfig{ChainID: 945, Policy: previous.artifact.Policy, EvidenceV2: ReleaseEvidenceV2Config{Bounds: ReleaseEvidenceV2Bounds{
		Cut: first.bounds, Replay: first.replay, MaxParticipants: 2, MaxTransitionBytes: 256*1024, MaxClosureBytes: 1024*1024, MaxProviders: 16, MaxEgressHashes: 16, MaxFleetPrefixes: 64}}}
	for _, input := range previous.artifact.Inputs {
		operator := previous.operators[input.NoID]
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: input.NoID, Ledger: operator.seal.ledger})
		history.keys[input.NoID] = operator.seal.server.serverPublicKeys()
		cfg.EvidenceV2.Operators = append(cfg.EvidenceV2.Operators, ReleaseEvidenceV2OperatorConfig{NoID: input.NoID, ReplayScratchRoot: newReleaseHeadV2TestStateDir(t), SealScratchRoot: newReleaseHeadV2TestStateDir(t)})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	reader, err := NewHTTPAttemptStreamV2Reader(server.URL, first.bounds)
	if err != nil { t.Fatal(err) }
	history.readers[0], history.readers[1] = reader, reader
	currentBytes, err := canonicalReleaseMeasurementBytes(current.artifact)
	if err != nil { t.Fatal(err) }
	owner := &releaseHistoryAdoptionV2{prefix: []releaseHistoryAdoptionIntentV2{{artifact: ReleaseMeasurementContentHash(previousBytes)}, {artifact: ReleaseMeasurementContentHash(currentBytes)}}}
	history.historyAdoption = owner
	runtime := &releaseRuntimeV2{cfg: cfg, history: history, gate: make(chan struct{}, 1)}
	store := &IntentStore{v2: &releaseIntentV2Owner{runtime: runtime, historyAdoption: owner}}
	verify := func(ctx context.Context) error { return store.verifyMeasurementLineageV2(ctx, previousBytes, previous.options(t), current.artifact, last.options(t)) }
	if err := verify(t.Context()); err != nil { t.Fatalf("strict real terminal gap replay failed: %v", err) }
	if history.retainedStartup || cfg.ProvisionalDeferClosedNativeInput { t.Fatal("strict gap used a provisional admission") }
	history.terminals[303].Transitions[0].Signature[0] ^= 1
	if err := verify(t.Context()); err == nil { t.Fatal("strict gap accepted a changed interior signature") }
	history.terminals[303].Transitions[0].Signature[0] ^= 1
	original := history.terminals[303]
	delete(history.terminals,303)
	if err := verify(t.Context()); err == nil { t.Fatal("strict gap accepted a missing interior terminal") }
	history.terminals[303] = original
	store.v2.historyAdoption = nil
	if err := verify(t.Context()); err == nil { t.Fatal("ordinary strict owner gained the explicit adoption permission") }
	store.v2.historyAdoption = owner
	foreign := *owner
	history.historyAdoption = &foreign
	if err := verify(t.Context()); err == nil { t.Fatal("another startup owner supplied adoption authority") }
	after, err := canonicalReleaseMeasurementBytes(current.artifact)
	if err != nil || !bytes.Equal(currentBytes,after) { t.Fatalf("lineage rewrote signed current history: %v",err) }
}

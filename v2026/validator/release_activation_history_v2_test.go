//go:build linux || darwin

// Real legacy transactions supply the initial history. Mutations retain real
// signatures where the history's complete lineage, not syntax, must reject them.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The existing M8/a_min8 fixture closes a genuine nonempty two-operator batch.
// Optional later windows are produced by the actual legacy settlement owner,
// preserving the original signed rows and saved exact EMA through migration.
type releaseActivationHistoryV2TestFixture struct {
	runtime *attemptSettlementRuntimeV2TestFixture
	history ReleaseEvidenceV2ActivationHistory
	options releaseActivationHistoryV2Options
}

func newReleaseActivationHistoryV2TestFixture(t *testing.T, windows int) releaseActivationHistoryV2TestFixture {
	t.Helper()
	runtime := newAttemptSettlementRuntimeV2LegacyHistoryFixture(t, true)
	encoded, err := ReadAttemptSettlementClosure(runtime.coordinator, 42)
	if err != nil {
		t.Fatal(err)
	}
	limits := attemptLedgerDiskTestLimits()
	fixture := releaseActivationHistoryV2TestFixture{
		runtime: runtime,
		history: ReleaseEvidenceV2ActivationHistory{Schema: ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{encoded}},
		options: releaseActivationHistoryV2Options{Stats: ReleaseStatsConfig{AMin: 8, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000}, ServerKeys: map[uint64]map[byte]ed25519.PublicKey{}, MaxBytes: 8 * 1024 * 1024, MaxOperators: 2, MaxProviders: 32, MaxRecords: limits.MaxRecordCount, MaxTrails: limits.MaxTrailCount},
	}
	for _, operator := range runtime.fixtures {
		fixture.options.Expected = append(fixture.options.Expected, operator.expected)
		fixture.options.ServerKeys[operator.expected.Identity.NoID] = operator.server.serverPublicKeys()
	}
	if windows == 1 {
		return fixture
	}
	if windows != 2 {
		t.Fatal("fixture requires one or two complete real windows")
	}
	participants := make([]AttemptSettlementParticipant, len(runtime.participants))
	coordinator := newAttemptSettlementRuntimeV2TestStateDir(t)
	for index, participant := range runtime.participants {
		head, err := participant.Ledger.Head()
		if err != nil {
			t.Fatal(err)
		}
		var records []AttemptRecord
		if err := participant.Ledger.Walk(t.Context(), 1, head.LastSequence, func(record AttemptRecord) error { records = append(records, record); return nil }); err != nil {
			t.Fatal(err)
		}
		state := newAttemptSettlementRuntimeV2TestStateDir(t)
		image, err := encodeStatsSnapshot(participant.Stats.snapshotStats())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "stats.json"), image, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(state, "attempt-ledger.jsonl"), attemptLedgerDiskTestJSONL(t, records), 0o600); err != nil {
			t.Fatal(err)
		}
		ledger, err := NewAttemptLedger(state, runtime.fixtures[index].expected.Identity, runtime.fixtures[index].key)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := ledger.Close(); err != nil {
				t.Error(err)
			}
		})
		stats := NewStatsEngine(participant.Stats.cfg)
		if err := stats.Load(state); err != nil {
			t.Fatal(err)
		}
		if err := stats.AttachAttemptLedger(ledger, state); err != nil {
			t.Fatal(err)
		}
		participants[index] = AttemptSettlementParticipant{NoID: participant.NoID, StateDir: state, Stats: stats}
	}
	boundary := fixture.options.Expected[0].Boundary
	boundary.EVMBlock += 10
	boundary.EVMBlockHash = attemptHex32([32]byte{0x71})
	if err := AdvanceAttemptSettlementEpoch(coordinator, 44, boundary, participants); err != nil {
		t.Fatal(err)
	}
	second, err := ReadAttemptSettlementClosure(coordinator, 43)
	if err != nil {
		t.Fatal(err)
	}
	fixture.history.LegacyClosures = append(fixture.history.LegacyClosures, second)
	for index := range fixture.options.Expected {
		expected := &fixture.options.Expected[index]
		expected.Activation.Domain.ActivationEpoch = 44
		expected.Activation.Domain.ActivationHash = [32]byte{0x72}
		expected.Boundary = AttemptBoundary{SettlementEpoch: 44, EVMBlock: boundary.EVMBlock + 1, EVMBlockHash: attemptHex32([32]byte{0x73})}
		expected.EgressGeneration = participants[index].Stats.egressGeneration
	}
	return fixture
}

func (self releaseActivationHistoryV2TestFixture) encoded(t *testing.T) []byte {
	t.Helper()
	encoded, err := self.history.CanonicalJSON(self.options.MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// Re-sign every member's real post-fold core and the same complete batch.
// This controls a dishonest signer, not a verifier callback or accepted flag.
func resignReleaseActivationHistoryV2TestClosure(t *testing.T, fixture releaseActivationHistoryV2TestFixture, closure *AttemptSettlementClosure) []byte {
	t.Helper()
	members := make([]AttemptSettlementMember, len(closure.Transitions))
	for index, transition := range closure.Transitions {
		qualities, err := sortedAttemptSettlementQualities(transition.PreFold, verifyAttemptLedgerCut)
		if err != nil {
			t.Fatal(err)
		}
		transition.PostFold = qualities
		digest, err := attemptSettlementTransitionDigest(transition)
		if err != nil {
			t.Fatal(err)
		}
		members[index] = AttemptSettlementMember{NoID: transition.Identity.NoID, Digest: attemptHex32(digest)}
	}
	for index, transition := range closure.Transitions {
		transition.Batch = append([]AttemptSettlementMember(nil), members...)
		message, err := attemptSettlementTransitionMessage(transition)
		if err != nil {
			t.Fatal(err)
		}
		transition.Signature = ed25519.Sign(fixture.runtime.fixtures[index].key, message)
	}
	encoded, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func TestReleaseActivationHistoryV2CanonicalEmptyAndExactBound(t *testing.T) {
	t.Parallel()
	history := ReleaseEvidenceV2ActivationHistory{Schema: ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}
	encoded, err := history.CanonicalJSON(1024)
	if err != nil {
		t.Fatal(err)
	}
	exact, err := history.CanonicalJSON(uint64(len(encoded)))
	if err != nil || !bytes.Equal(encoded, exact) {
		t.Fatalf("exact canonical history bound: %v", err)
	}
	if got, err := history.CanonicalJSON(uint64(len(encoded) - 1)); err == nil || got != nil {
		t.Fatal("history crossed exact byte ceiling")
	}
	for _, invalid := range []ReleaseEvidenceV2ActivationHistory{{Schema: history.Schema}, {Schema: "other", LegacyClosures: [][]byte{}}, {Schema: history.Schema, LegacyClosures: [][]byte{nil}}} {
		if got, err := invalid.CanonicalJSON(1024); err == nil || got != nil {
			t.Fatal("ambiguous history was encoded")
		}
	}
}

func TestReleaseActivationHistoryV2FreshRealRuntimeStaysUnchanged(t *testing.T) {
	t.Parallel()
	runtime := newAttemptSettlementRuntimeV2TestFixture(t, false)
	before := runtimeAttemptSettlementV2TestImages(t, runtime.participants)
	authority := runtime.options(t).Authority
	options := releaseActivationHistoryV2Options{Stats: authority.Operators[runtime.participants[0].NoID].Measurement.ExpectedConfig, MaxBytes: 1024, MaxOperators: 2, MaxProviders: 32, MaxRecords: 128, MaxTrails: 16}
	for _, operator := range runtime.fixtures {
		options.Expected = append(options.Expected, operator.expected)
	}
	history := ReleaseEvidenceV2ActivationHistory{Schema: ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}
	encoded, err := history.CanonicalJSON(options.MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	last, err := replayReleaseActivationHistoryV2(t.Context(), encoded, options)
	if err != nil || last != nil {
		t.Fatalf("actual generation-one origin: %v", err)
	}
	after := runtimeAttemptSettlementV2TestImages(t, runtime.participants)
	for index := range before {
		if !bytes.Equal(before[index], after[index]) {
			t.Fatal("read-only history initialized or rewrote Stats")
		}
	}
}

func TestReleaseActivationHistoryV2ReplaysRealPositiveLegacyPrefix(t *testing.T) {
	t.Parallel()
	fixture := newReleaseActivationHistoryV2TestFixture(t, 1)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.runtime.participants)
	last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options)
	if err != nil || last == nil || len(last.Transitions) != 2 {
		t.Fatalf("real complete legacy prefix: %v", err)
	}
	if len(last.Transitions[0].PostFold) == 0 {
		t.Fatal("real a_min8 history did not carry positive EMA")
	}
	for index, transition := range last.Transitions {
		if err := matchReleaseActivationHistoryV2Transition(transition, fixture.runtime.participants[index].Stats.settlementTransition); err != nil {
			t.Fatal(err)
		}
	}
	after := runtimeAttemptSettlementV2TestImages(t, fixture.runtime.participants)
	for index := range before {
		if !bytes.Equal(before[index], after[index]) {
			t.Fatal("history replay replaced actual retained state")
		}
	}
}

func TestReleaseActivationHistoryV2ReplaysConsecutiveRealLegacyFolds(t *testing.T) {
	t.Parallel()
	fixture := newReleaseActivationHistoryV2TestFixture(t, 2)
	first, err := DecodeAttemptSettlementClosureWithServerKeys(fixture.history.LegacyClosures[0], fixture.options.ServerKeys)
	if err != nil || first == nil || len(first.Transitions) != 2 {
		t.Fatalf("original complete signed predecessor: %v", err)
	}
	last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options)
	if err != nil || last == nil || last.Epoch != 43 || len(last.Transitions) != len(first.Transitions) {
		t.Fatalf("complete two-window legacy lineage: %v", err)
	}
	positivePrior, absentPrior := false, false
	for index, transition := range last.Transitions {
		prior := first.Transitions[index]
		if transition.Identity != prior.Identity || transition.PreFold.AttemptCut.RecordCount != 0 || len(transition.PreFold.AttemptCut.Records) != 0 || !slices.Equal(transition.PostFold, prior.PostFold) || len(transition.PreFold.Providers) != len(prior.PostFold) {
			t.Fatalf("operator %d empty successor changed its own authenticated EMA census", transition.Identity.NoID)
		}
		// The actual fixture has one a_min8-scored operator and one with only
		// one trail. An empty window must preserve both value and absence.
		absentPrior = absentPrior || len(prior.PostFold) == 0
		for providerIndex, provider := range transition.PreFold.Providers {
			quality := prior.PostFold[providerIndex]
			if provider.ClientID != quality.ClientID || !quality.HasQuality || !provider.HasPriorQuality || provider.PriorQualityPPM != quality.QualityPPM || provider.Assignments != 0 || provider.Confirmations != 0 || len(provider.EgressIPHashHexes) != 0 {
				t.Fatalf("operator %d provider %s did not carry exact prior without new attempts", transition.Identity.NoID, provider.ClientID)
			}
			for _, count := range provider.LatencyBuckets {
				if count != 0 {
					t.Fatalf("operator %d retained a latency sample in its empty successor", transition.Identity.NoID)
				}
			}
			positivePrior = positivePrior || quality.QualityPPM > 0
		}
	}
	if !positivePrior || !absentPrior {
		t.Fatal("real two-operator history lacks positive-EMA or absent-EMA control")
	}
}

func TestReleaseActivationHistoryV2RefusesMissingOriginAndWindow(t *testing.T) {
	t.Parallel()
	original := newReleaseActivationHistoryV2TestFixture(t, 2)
	encoded := original.encoded(t)
	for _, fault := range []string{"empty", "missing-first", "missing-last", "duplicate", "reverse"} {
		// Every case owns its list, while the genuine signed bytes remain
		// immutable. None needs another ledger/M8 construction or state write.
		fixture := original
		fixture.history.LegacyClosures = slices.Clone(original.history.LegacyClosures)
		switch fault {
		case "empty":
			fixture.history.LegacyClosures = [][]byte{}
		case "missing-first":
			fixture.history.LegacyClosures = fixture.history.LegacyClosures[1:]
		case "missing-last":
			fixture.history.LegacyClosures = fixture.history.LegacyClosures[:1]
		case "duplicate":
			fixture.history.LegacyClosures[1] = fixture.history.LegacyClosures[0]
		case "reverse":
			fixture.history.LegacyClosures[0], fixture.history.LegacyClosures[1] = fixture.history.LegacyClosures[1], fixture.history.LegacyClosures[0]
		}
		if last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options); err == nil || last != nil {
			t.Fatalf("%s accepted incomplete history", fault)
		}
		if !bytes.Equal(encoded, original.encoded(t)) {
			t.Fatalf("%s mutated another case's complete history", fault)
		}
	}
}

func TestReleaseActivationHistoryV2RefusesResignedInventedOriginEMA(t *testing.T) {
	t.Parallel()
	fixture := newReleaseActivationHistoryV2TestFixture(t, 1)
	closure, err := DecodeAttemptSettlementClosureWithServerKeys(fixture.history.LegacyClosures[0], fixture.options.ServerKeys)
	if err != nil {
		t.Fatal(err)
	}
	provider := &closure.Transitions[0].PreFold.Providers[0]
	provider.HasPriorQuality, provider.PriorQualityPPM = true, 500000
	forged := resignReleaseActivationHistoryV2TestClosure(t, fixture, closure)
	if _, err := DecodeAttemptSettlementClosureWithServerKeys(forged, fixture.options.ServerKeys); err != nil {
		t.Fatalf("real signer/cut positive control: %v", err)
	}
	fixture.history.LegacyClosures[0] = forged
	last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options)
	if last != nil || err == nil || !strings.Contains(err.Error(), "prior EMA is not derived") {
		t.Fatalf("signed invented origin EMA escaped complete history: %v", err)
	}
}

func TestReleaseActivationHistoryV2RefusesResignedChangedPredecessorEMA(t *testing.T) {
	t.Parallel()
	fixture := newReleaseActivationHistoryV2TestFixture(t, 2)
	closure, err := DecodeAttemptSettlementClosureWithServerKeys(fixture.history.LegacyClosures[1], fixture.options.ServerKeys)
	if err != nil {
		t.Fatal(err)
	}
	provider := &closure.Transitions[0].PreFold.Providers[0]
	if !provider.HasPriorQuality {
		t.Fatal("actual successor lacks its real prior")
	}
	provider.PriorQualityPPM = (provider.PriorQualityPPM + 1) % 1000001
	forged := resignReleaseActivationHistoryV2TestClosure(t, fixture, closure)
	if _, err := DecodeAttemptSettlementClosureWithServerKeys(forged, fixture.options.ServerKeys); err != nil {
		t.Fatalf("real signed successor prerequisite: %v", err)
	}
	fixture.history.LegacyClosures[1] = forged
	if last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options); last != nil || err == nil || !strings.Contains(err.Error(), "prior EMA is not derived") {
		t.Fatalf("changed predecessor EMA was accepted: %v", err)
	}
}

func TestReleaseActivationHistoryV2RefusesChangedAuthorityAndBounds(t *testing.T) {
	t.Parallel()
	original := newReleaseActivationHistoryV2TestFixture(t, 1)
	encoded := original.encoded(t)
	before, err := json.Marshal(original.options)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"census", "prefix", "generation", "key", "record-bound", "trail-bound", "provider-bound", "bytes"} {
		fixture := original
		fixture.options.Expected = slices.Clone(original.options.Expected)
		fixture.options.ServerKeys = maps.Clone(original.options.ServerKeys)
		switch fault {
		case "census":
			fixture.options.Expected = fixture.options.Expected[:1]
		case "prefix":
			item := &fixture.options.Expected[1]
			item.FirstSequence++
			item.EgressFirstSequence++
			item.Activation.FirstSequence++
		case "generation":
			fixture.options.Expected[1].EgressGeneration = 1
		case "key":
			fixture.options.ServerKeys[fixture.options.Expected[1].Identity.NoID] = map[byte]ed25519.PublicKey{}
		case "record-bound":
			fixture.options.MaxRecords, fixture.options.MaxTrails = 1, 1
		case "trail-bound":
			fixture.options.MaxTrails = 1
		case "provider-bound":
			fixture.options.MaxProviders = 1
		case "bytes":
			fixture.options.MaxBytes = uint64(len(encoded) - 1)
		}
		if last, err := replayReleaseActivationHistoryV2(t.Context(), encoded, fixture.options); err == nil || last != nil {
			t.Fatalf("%s crossed independent history authority", fault)
		}
		after, err := json.Marshal(original.options)
		if err != nil || !bytes.Equal(before, after) || !bytes.Equal(encoded, original.encoded(t)) {
			t.Fatalf("%s leaked authority into another case: %v", fault, err)
		}
	}
}

func TestReleaseActivationHistoryV2RefusesNoncanonicalAndRecursiveWire(t *testing.T) {
	t.Parallel()
	fixture := newReleaseActivationHistoryV2TestFixture(t, 1)
	encoded := fixture.encoded(t)
	for _, changed := range [][]byte{append(bytes.Clone(encoded), '\n'), bytes.Replace(encoded, []byte(`"legacy_closures":`), []byte(`"legacy_closures":null,"legacy_closures":`), 1), []byte(`{"schema":"urnetwork-validator-activation-history-v2","legacy_closures":null}` + "\n")} {
		if last, err := replayReleaseActivationHistoryV2(t.Context(), changed, fixture.options); err == nil || last != nil {
			t.Fatal("noncanonical activation history was accepted")
		}
	}
	original := fixture.history.LegacyClosures[0]
	fixture.history.LegacyClosures[0] = bytes.Replace(original, []byte(`"pre_fold":{`), []byte(`"pre_fold":{"settlement_transition":{},`), 1)
	if bytes.Equal(original, fixture.history.LegacyClosures[0]) {
		t.Fatal("recursive authority fixture did not change real bytes")
	}
	if last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options); err == nil || last != nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("nested legacy authority reached replay: %v", err)
	}
}

func TestReleaseActivationHistoryV2BootstrapRequiresIdenticalCompleteHistory(t *testing.T) {
	t.Parallel()
	fixture := newReleaseBootstrapV2TestFixture(t)
	history := ReleaseEvidenceV2ActivationHistory{Schema: ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}
	encoded, err := history.CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes)
	if err != nil {
		t.Fatal(err)
	}
	for index := range fixture.cfg.EvidenceV2.Operators {
		reference := &fixture.cfg.EvidenceV2.Operators[index].History
		*reference = writeReleaseBootstrapV2TestFile(t, reference.Path, encoded)
	}
	inputs, err := fixture.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if last, err := replayReleaseEvidenceV2ActivationHistories(t.Context(), &fixture.cfg, inputs, nil); err != nil || last != nil {
		t.Fatalf("real configured empty census: %v", err)
	}
	inputs[1].HistoryBytes = append(bytes.Clone(inputs[1].HistoryBytes), '\n')
	if last, err := replayReleaseEvidenceV2ActivationHistories(t.Context(), &fixture.cfg, inputs, nil); err == nil || last != nil {
		t.Fatal("operator bootstrap accepted disagreeing history")
	}
}

func TestReleaseActivationHistoryV2CanceledInputHasNoResult(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if last, err := replayReleaseActivationHistoryV2(ctx, []byte("invalid"), releaseActivationHistoryV2Options{}); last != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled history performed admission: %v", err)
	}
}

func TestReleaseActivationHistoryV2RetainedTransitionRequiresExactBytes(t *testing.T) {
	t.Parallel()
	fixture := newReleaseActivationHistoryV2TestFixture(t, 1)
	last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	changed := *last.Transitions[0]
	changed.Signature = bytes.Clone(changed.Signature)
	changed.Signature[0] ^= 1
	if err := matchReleaseActivationHistoryV2Transition(last.Transitions[0], &changed); err == nil {
		t.Fatal("retained transition was replaced with different signed bytes")
	}
	if err := matchReleaseActivationHistoryV2Transition(last.Transitions[0], nil); err == nil {
		t.Fatal("retained transition was omitted")
	}
	if err := matchReleaseActivationHistoryV2Transition(nil, nil); err != nil {
		t.Fatalf("explicit empty history differs: %v", err)
	}
}

// The loader proves real signatures and historical RPC before any mutation.
// Agreement among every returned member cannot replace the configured pins.
func TestReleaseActivationHistoryV2BootstrapRejectsChangedPinnedBytes(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"history-content", "history-size", "context-size", "context-journal", "context-runtime", "context-observer", "context-historical-uid"} {
		fixture := newReleaseBootstrapV2TestFixture(t)
		history := ReleaseEvidenceV2ActivationHistory{Schema: ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}
		encoded, err := history.CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes)
		if err != nil {
			t.Fatal(err)
		}
		if fault != "history-content" {
			for index := range fixture.cfg.EvidenceV2.Operators {
				reference := &fixture.cfg.EvidenceV2.Operators[index].History
				*reference = writeReleaseBootstrapV2TestFile(t, reference.Path, encoded)
			}
		}
		inputs, err := fixture.read(t.Context())
		if err != nil {
			t.Fatalf("%s real historical bootstrap prerequisite: %v", fault, err)
		}
		before, beforeErr := replayReleaseEvidenceV2ActivationHistories(t.Context(), &fixture.cfg, inputs, nil)
		if before != nil || fault == "history-content" && beforeErr == nil || fault != "history-content" && beforeErr != nil {
			t.Fatalf("%s original pinned history prerequisite: %v", fault, beforeErr)
		}
		for index := range inputs {
			input := &inputs[index]
			switch fault {
			case "history-content":
				if bytes.Equal(input.HistoryBytes, encoded) {
					t.Fatal("history substitution did not change pinned bytes")
				}
				input.HistoryBytes = bytes.Clone(encoded)
			case "history-size":
				fixture.cfg.EvidenceV2.Operators[index].History.Bytes++
			case "context-size":
				fixture.cfg.EvidenceV2.Operators[index].Context.Bytes++
			case "context-journal":
				input.Context.Journal[19] ^= 0x80
			case "context-runtime":
				input.Context.RuntimeHash[31] ^= 0x80
			case "context-observer":
				input.Context.ObservedEVMBlock++
			case "context-historical-uid":
				input.Context.ValidatorUID++
				input.Context.InitialCut.Identity.ValidatorUID = input.Context.ValidatorUID
			}
			input.Config = fixture.cfg.EvidenceV2.Operators[index]
			if _, err := input.Context.CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes); err != nil {
				t.Fatalf("%s well-shaped context prerequisite: %v", fault, err)
			}
			if err := input.Candidate.Verify(input.Context.Activation, input.VPKSignature, input.HotkeySignature); err != nil {
				t.Fatalf("%s retained real consent prerequisite: %v", fault, err)
			}
			if index > 0 && !bytes.Equal(inputs[0].HistoryBytes, input.HistoryBytes) {
				t.Fatalf("%s did not preserve all-member history agreement", fault)
			}
		}
		if err := fixture.cfg.Validate(); err != nil {
			t.Fatalf("%s well-shaped reference prerequisite: %v", fault, err)
		}
		last, err := replayReleaseEvidenceV2ActivationHistories(t.Context(), &fixture.cfg, inputs, nil)
		if last != nil || err == nil || !strings.Contains(err.Error(), "configured reference") {
			t.Errorf("%s all-member substitution crossed configured byte authority: %v", fault, err)
		}
	}
}

// Distinct pending IDs consume disk trail slots before the legacy lifecycle
// verifier allocates. Repeated genuine checkpoints consume only one slot.
func TestReleaseActivationHistoryV2PendingIDsRespectLifetimeTrailBound(t *testing.T) {
	t.Parallel()
	fixture := newReleaseActivationHistoryV2TestFixture(t, 1)
	closure, err := DecodeAttemptSettlementClosureWithServerKeys(fixture.history.LegacyClosures[0], fixture.options.ServerKeys)
	if err != nil {
		t.Fatal(err)
	}
	var exactTrails uint64
	for _, transition := range closure.Transitions {
		seen := map[string]bool{}
		for _, record := range transition.PreFold.AttemptCut.Records {
			seen[record.TrailID.String()] = true
		}
		exactTrails = max(exactTrails, uint64(len(seen)))
	}
	if exactTrails < 2 {
		t.Fatal("real complete history lacks distinct trails")
	}
	fixture.options.MaxTrails = exactTrails
	if last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options); err != nil || last == nil {
		t.Fatalf("exact distinct-trail capacity rejected complete signed history: %v", err)
	}

	cut := closure.Transitions[0].PreFold.AttemptCut
	firstTrail := cut.Records[0].TrailID
	selected := []AttemptRecord{}
	for _, record := range cut.Records {
		if record.Disposition != AttemptDispositionPending {
			continue
		}
		if record.TrailID == firstTrail && len(selected) < 2 || record.TrailID != firstTrail && len(selected) == 2 {
			owned, err := cloneAttemptRecord(record)
			if err != nil {
				t.Fatal(err)
			}
			selected = append(selected, owned)
		}
		if len(selected) == 3 {
			break
		}
	}
	if len(selected) != 3 || selected[0].TrailID != selected[1].TrailID || selected[1].TrailID == selected[2].TrailID {
		t.Fatal("real pending prefix lacks repeated and distinct identities")
	}
	key := fixture.runtime.fixtures[0].key
	vpk := key.Public().(ed25519.PublicKey)
	previous := zeroAttemptHash()
	for index := range selected {
		record := &selected[index]
		record.Sequence, record.PreviousHash = uint64(index+1), previous
		digest, err := attemptRecordHash(record)
		if err != nil {
			t.Fatal(err)
		}
		record.RecordHash, record.Signature = attemptHex32(digest), ed25519.Sign(key, attemptRecordSignatureMessage(digest))
		if err := VerifyAttemptRecord(record, cut.Identity, vpk, fixture.options.ServerKeys[cut.Identity.NoID]); err != nil {
			t.Fatalf("actual server/validator pending-row prerequisite: %v", err)
		}
		previous = record.RecordHash
	}
	cut.Records, cut.RecordCount, cut.LastSequence = selected, uint64(len(selected)), uint64(len(selected))
	cut.FirstSequence, cut.EgressFirstSequence, cut.PriorRoot, cut.Root = 1, 1, zeroAttemptHash(), previous
	message, err := attemptCutSignatureMessage(cut)
	if err != nil {
		t.Fatal(err)
	}
	cut.Signature = ed25519.Sign(key, message)
	if !ed25519.Verify(vpk, message, cut.Signature) {
		t.Fatal("actual pending cut signature prerequisite")
	}
	if err := VerifyAttemptLedgerCut(cut, vpk, fixture.options.ServerKeys[cut.Identity.NoID]); err == nil || !strings.Contains(err.Error(), "unfinished trail") {
		t.Fatalf("signed pending lifecycle prerequisite: %v", err)
	}

	// A dishonest validator can sign a pending closure, but cannot bypass
	// resource admission or turn that signature into a completed lifecycle.
	members := make([]AttemptSettlementMember, len(closure.Transitions))
	for index, transition := range closure.Transitions {
		digest, err := attemptSettlementTransitionDigest(transition)
		if err != nil {
			t.Fatal(err)
		}
		members[index] = AttemptSettlementMember{NoID: transition.Identity.NoID, Digest: attemptHex32(digest)}
	}
	for index, transition := range closure.Transitions {
		transition.Batch = append([]AttemptSettlementMember(nil), members...)
		message, err := attemptSettlementTransitionMessage(transition)
		if err != nil {
			t.Fatal(err)
		}
		transition.Signature = ed25519.Sign(fixture.runtime.fixtures[index].key, message)
	}
	raw, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	fixture.history.LegacyClosures[0] = append(raw, '\n')
	for _, item := range []struct {
		capacity uint64
		refusal  string
	}{
		{capacity: 1, refusal: "lifetime trail bound exceeded"},
		{capacity: 2, refusal: "unfinished trail"},
	} {
		fixture.options.MaxTrails = item.capacity
		last, err := replayReleaseActivationHistoryV2(t.Context(), fixture.encoded(t), fixture.options)
		if last != nil || err == nil || !strings.Contains(err.Error(), item.refusal) {
			t.Errorf("pending distinct-trail capacity %d crossed its admission boundary: %v", item.capacity, err)
		}
	}
}

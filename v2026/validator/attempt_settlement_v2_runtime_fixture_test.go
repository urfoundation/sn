//go:build linux || darwin

package validator

// These controls use genuine M8 trails, root-owned disk ledgers and typed
// object codecs. Only immutable transport storage and physical failure seams
// are in memory; no authentication, projection or fold verdict is replaced.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// The operator identities and ledger paths are selected before any append.
// Seal/replay namespaces are fresh per operation; the object bytes persist.
type attemptSettlementRuntimeV2TestFixture struct {
	coordinator  string
	participants []AttemptSettlementRuntimeV2Participant
	fixtures     []*attemptCutV2SealTestFixture
	sealers      map[uint64]AttemptCutV2SealOptions
	objects      map[uint64]*attemptCutV2SealTestObjects
}

// Explicit test authority, not a runtime default or an increase to any
// existing record, trail, participant, transition or replay allowance.
func runtimeAttemptSettlementV2TestPersistence() AttemptSettlementRuntimeV2PersistenceBounds {
	return AttemptSettlementRuntimeV2PersistenceBounds{MaxSnapshotBytes: 4 * 1024 * 1024, MaxJournalBytes: 32 * 1024 * 1024}
}

// Provisioners may resolve their newly created test directory before it is
// supplied as authority. Runtime candidate paths never receive this fallback;
// this also keeps Darwin's /var or /tmp aliases out of valid-input controls.
func newAttemptSettlementRuntimeV2TestStateDir(t *testing.T) string {
	t.Helper()
	physical, err := filepath.EvalSymlinks(newAttemptLedgerDiskTestStateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return physical
}

// Delegates the real byte stream and Close before adding one explicit late
// failure. No bytes, signature, projection or acceptance result is fabricated.
type runtimeAttemptSettlementV2TestReader struct {
	io.ReadCloser
	cause  error
	closes *int
}

// The close boundary remains observable even after successful complete reads.
func (self runtimeAttemptSettlementV2TestReader) Close() error {
	*self.closes = *self.closes + 1
	return errors.Join(self.ReadCloser.Close(), self.cause)
}

// Typed proof reads reach their actual reader Close before refusing a result.
func runtimeAttemptSettlementV2TestLateClose(open AttemptStreamV2DataOpener, cause error, closes *int) AttemptStreamV2DataOpener {
	return func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return runtimeAttemptSettlementV2TestReader{ReadCloser: reader, cause: cause, closes: closes}, nil
	}
}

// Advances only independently pinned finalized context and the actual engine
// boundary resolver. Cursor/root/generation come from the previously accepted
// real signed closure; no field is copied out of a candidate under test.
func (self *attemptSettlementRuntimeV2TestFixture) nextWindow(t *testing.T, closure *AttemptSettlementClosureV2) {
	t.Helper()
	for index, fixture := range self.fixtures {
		terminal := closure.Transitions[index]
		expected := fixture.expected
		expected.Boundary = AttemptBoundary{SettlementEpoch: terminal.ToEpoch, EVMBlock: terminal.FromBoundary.EVMBlock + 1, EVMBlockHash: attemptHex32([32]byte{byte(terminal.ToEpoch), 77})}
		expected.FirstSequence, expected.EgressFirstSequence = terminal.Cut.LastSequence+1, terminal.Cut.LastSequence+1
		expected.EgressGeneration, expected.PriorRoot = terminal.Cut.Context.EgressGeneration+1, terminal.Cut.Root
		fixture.expected = expected
		fixture.engine.epochFn = func() uint64 { return expected.Boundary.SettlementEpoch }
		resolver := func(_ context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			if pinned != nil && *pinned != expected.Boundary {
				return AttemptBoundary{}, nil, errors.New("runtime successor fixture changed finalized pin")
			}
			bindings := make([]AttemptBinding, len(ids))
			for index, id := range ids {
				bindings[index] = attemptLedgerTestBinding(id, 1)
			}
			return expected.Boundary, bindings, nil
		}
		fixture.engine.cfg.AttemptBoundaryResolver = resolver
		fixture.engine.resolve = resolver
	}
}

// The original physical store caps remain 128 records and sixteen trails;
// policy M8, a_min8 and all production timeout/retry parameters are untouched.
func newAttemptSettlementRuntimeV2TestFixture(t *testing.T, activate bool) *attemptSettlementRuntimeV2TestFixture {
	t.Helper()
	fixture := &attemptSettlementRuntimeV2TestFixture{coordinator: newAttemptSettlementRuntimeV2TestStateDir(t), sealers: map[uint64]AttemptCutV2SealOptions{}, objects: map[uint64]*attemptCutV2SealTestObjects{}}
	for _, noID := range []uint64{9, 11} {
		operator := newAttemptCutV2SealTestFixtureForOperatorWithEpochOrder(t, 8, 0, 0, noID, true)
		// The actual first epoch advances the attached empty ledger to one.
		// Neither the independent pin nor the durable snapshot is synthesized.
		initial := operator.engine.stats.snapshotStats()
		head, err := operator.ledger.Head()
		if err != nil || head.LastSequence != 0 || head.Root != zeroAttemptHash() || head.RecordBytes != 0 || head.TrailCount != 0 || initial.EgressGeneration != 1 || operator.expected.EgressGeneration != 1 || initial.SettlementEpoch == nil || *initial.SettlementEpoch != 42 || initial.AttemptLastAppliedSequence != 0 || initial.AttemptSettlementFirstSequence != 1 || initial.AttemptEgressFirstSequence != 1 || initial.SettlementTransition != nil || initial.AttemptV2 != nil || len(initial.Window) != 0 || len(initial.Ema) != 0 || len(initial.EmaPPM) != 0 || len(initial.Egress) != 0 {
			t.Fatalf("real initial runtime fixture is not the exact generation-one empty prefix: %+v/%v", head, err)
		}
		image, err := encodeStatsSnapshot(initial)
		if err != nil {
			t.Fatal(err)
		}
		durable, err := os.ReadFile(filepath.Join(filepath.Dir(operator.ledger.path), "stats.json"))
		if err != nil || !bytes.Equal(durable, image) {
			t.Fatalf("initial epoch did not durably establish its exact generation: %v", err)
		}
		if operator.policy.Verify.ReliabilityAMin != 8 || operator.policy.Verify.TrailDepth != 8 {
			t.Fatal("runtime fixture changed the real policy")
		}
		operator.bounds.Records.MaxItems, operator.bounds.Proofs.MaxItems, operator.replay.MaxTrails = 128, 16, 16
		fixture.fixtures = append(fixture.fixtures, operator)
		fixture.participants = append(fixture.participants, AttemptSettlementRuntimeV2Participant{NoID: noID, StateDir: filepath.Dir(operator.ledger.path), Stats: operator.engine.stats, Ledger: operator.ledger})
		fixture.sealers[noID], fixture.objects[noID] = newAttemptCutV2SealTestOptions(t, operator)
	}
	if activate {
		if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
			t.Fatalf("actual empty activation: %v", err)
		}
	}
	return fixture
}

// Independent mock server bindings are known before the first attempt. The
// existing head test budgets (32 providers/64 prefixes) and sixteen actual
// routable provider hashes cover the real fixed provider census.
func (self *attemptSettlementRuntimeV2TestFixture) options(t *testing.T) AttemptSettlementRuntimeV2Options {
	t.Helper()
	options := AttemptSettlementRuntimeV2Options{Persistence: runtimeAttemptSettlementV2TestPersistence(), Authority: AttemptSettlementV2Options{Operators: map[uint64]AttemptSettlementV2OperatorOptions{}, MaxParticipants: 2, MaxTransitionBytes: 256 * 1024, MaxClosureBytes: 1024 * 1024}, Seal: map[uint64]AttemptCutV2SealOptions{}, PrivateKeys: map[uint64]ed25519.PrivateKey{}}
	for index, fixture := range self.fixtures {
		noID := self.participants[index].NoID
		seal := self.sealers[noID]
		seal.ScratchDirectory = filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "seal")
		options.Seal[noID], options.PrivateKeys[noID] = seal, append(ed25519.PrivateKey(nil), fixture.key...)
		bindings := map[connect.Id]FleetScoreKey{}
		for _, id := range fixture.server.providers {
			bindings[id] = attemptCutV2HeadTestFleetKey(t, attemptLedgerTestBinding(id, 1))
		}
		options.Authority.Operators[noID] = AttemptSettlementV2OperatorOptions{Expected: fixture.expected, Policy: fixture.policy, Bounds: fixture.bounds, Measurement: AttemptCutV2MeasurementOptions{
			ExpectedConfig: ReleaseStatsConfig{AMin: 8, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000}, CurrentBindingKVs: bindings, MaxProviders: 32, MaxEgressHashes: 16, MaxFleetPrefixes: 64,
			Replay: AttemptCutV2ReplayOptions{Bounds: fixture.replay, ScratchDirectory: filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "replay"), ServerKeys: fixture.server.serverPublicKeys(), ReadMetadata: seal.ReadMetadata, OpenData: seal.OpenData},
		}}
	}
	return options
}

// The failure is a real extension refusal after signed seed admission. It
// produces two WAL records without inventing a terminal or scored provider.
func (self *attemptSettlementRuntimeV2TestFixture) trails(t *testing.T, index, completed, failed int) {
	t.Helper()
	fixture := self.fixtures[index]
	for range completed {
		proof, err := fixture.engine.RunTrail(t.Context())
		if err != nil || proof == nil || proof.M != 8 || len(proof.Hops) != 8 {
			t.Fatalf("actual runtime M8 trail: %v", err)
		}
	}
	fixture.engine.transport = attemptCutV2SealTestTransport(func(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
		var request struct {
			TrailID *connect.Id `json:"trail_id"`
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
		if request.TrailID != nil {
			return nil, errors.New("runtime fixture actual extension refusal")
		}
		return fixture.server.PostVerify(ctx, hop, raw)
	})
	for range failed {
		proof, err := fixture.engine.RunTrail(t.Context())
		if err == nil || proof != nil {
			t.Fatal("actual refused extension produced a proof")
		}
	}
	fixture.engine.transport = fixture.server
}

// A canonical snapshot is an exact comparison, not a projected quality map.
func runtimeAttemptSettlementV2TestImages(t *testing.T, participants []AttemptSettlementRuntimeV2Participant) [][]byte {
	t.Helper()
	images := make([][]byte, len(participants))
	for index, participant := range participants {
		encoded, err := encodeStatsSnapshot(participant.Stats.snapshotStats())
		if err != nil {
			t.Fatal(err)
		}
		images[index] = encoded
	}
	return images
}

// All failure controls inspect the public engines, not the private candidate.
func (self *attemptSettlementRuntimeV2TestFixture) assertReserved(t *testing.T, epoch uint64) {
	t.Helper()
	for _, participant := range self.participants {
		stats := participant.Stats
		stats.mu.Lock()
		reserved := stats.attemptCutPending && stats.attemptSettlementCutPending && stats.attemptSettlementCutEpoch == epoch
		stats.mu.Unlock()
		if !reserved {
			t.Fatalf("no_id %d was not reserved for %d", participant.NoID, epoch)
		}
	}
}

// Reads the actual retained journal so restart assertions use its own bytes.
func (self *attemptSettlementRuntimeV2TestFixture) transaction(t *testing.T) *attemptSettlementTransactionV2 {
	t.Helper()
	raw, err := os.ReadFile(attemptSettlementTransactionV2Path(self.coordinator))
	if err != nil {
		t.Fatal(err)
	}
	transaction, _, _, err := decodeAttemptSettlementTransactionV2(t.Context(), raw, self.participants, runtimeAttemptSettlementV2TestPersistence())
	if err != nil {
		t.Fatal(err)
	}
	return transaction
}

// Reopens actual root-owned ledger files and constructs explicitly fresh
// Stats. Returning an already-attached engine would miss startup's contract.
func (self *attemptSettlementRuntimeV2TestFixture) reopen(t *testing.T) []AttemptSettlementRuntimeV2Participant {
	t.Helper()
	participants := make([]AttemptSettlementRuntimeV2Participant, len(self.participants))
	for index, participant := range self.participants {
		if err := participant.Ledger.Close(); err != nil {
			t.Fatal(err)
		}
		ledger, err := NewDiskAttemptLedger(t.Context(), participant.StateDir, self.fixtures[index].expected.Identity, attemptLedgerDiskTestCoordinator, self.fixtures[index].key, attemptLedgerDiskTestLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ledger.Close() })
		participants[index] = AttemptSettlementRuntimeV2Participant{NoID: participant.NoID, StateDir: participant.StateDir, Stats: NewStatsEngine(participant.Stats.cfg), Ledger: ledger}
	}
	return participants
}

// The canonical closure and every durable/live postimage must be identical
// to the journal, not merely the same epoch or apparently equal quality.
func assertAttemptSettlementRuntimeV2Postimages(t *testing.T, coordinator string, participants []AttemptSettlementRuntimeV2Participant, transaction *attemptSettlementTransactionV2) {
	t.Helper()
	for index, participant := range participants {
		data, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil || !bytes.Equal(data, transaction.Snapshots[index].StatsJSON) {
			t.Fatalf("no_id %d durable postimage differs: %v", participant.NoID, err)
		}
		live, err := encodeStatsSnapshot(participant.Stats.snapshotStats())
		if err != nil || !bytes.Equal(live, data) || participant.Stats.attemptCutPending || participant.Stats.attemptSettlementCutPending || participant.Stats.attemptLedger != participant.Ledger {
			t.Fatalf("no_id %d live postimage or attachment differs: %v", participant.NoID, err)
		}
	}
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed journal remains: %v", err)
	}
	if len(transaction.ClosureJSON) != 0 {
		closure, err := os.ReadFile(AttemptSettlementClosureV2Path(coordinator, transaction.Epoch-1))
		if err != nil || !bytes.Equal(closure, transaction.ClosureJSON) {
			t.Fatalf("immutable postimage differs: %v", err)
		}
	}
}

// One known physical snapshot succeeds; the next writer really fails. The
// old public state and the complete canonical transaction must remain intact.
func (self *attemptSettlementRuntimeV2TestFixture) partialWrite(t *testing.T) *attemptSettlementTransactionV2 {
	t.Helper()
	physical := attemptSettlementV2PhysicalIO()
	calls := 0
	physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, raw []byte) error {
		calls++
		if calls == 2 {
			return errors.New("runtime second snapshot failure")
		}
		return writeAttemptSettlementV2OwnedState(root, name, raw)
	}
	before := runtimeAttemptSettlementV2TestImages(t, self.participants)
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), self.coordinator, self.participants, 43, self.fixtures[0].expected.Boundary, self.options(t), physical, true)
	if err == nil || closure != nil || calls != 2 || !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, self.participants)) {
		t.Fatalf("partial durability published a partial generation: %v", err)
	}
	self.assertReserved(t, 43)
	return self.transaction(t)
}

// Preserve the exact pre-checkpoint v6 image and import only the first real
// signed M8 pending row into a new root-owned disk namespace for each member.
func (self *attemptSettlementRuntimeV2TestFixture) pendingRestart(t *testing.T) []AttemptSettlementRuntimeV2Participant {
	t.Helper()
	participants := make([]AttemptSettlementRuntimeV2Participant, len(self.participants))
	for index, participant := range self.participants {
		initial, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil {
			t.Fatal(err)
		}
		self.trails(t, index, 1, 0)
		var record AttemptRecord
		if err := participant.Ledger.Walk(t.Context(), 1, 1, func(actual AttemptRecord) error { record = actual; return nil }); err != nil {
			t.Fatal(err)
		}
		if record.Disposition != AttemptDispositionPending || record.M != 8 {
			t.Fatal("real pending M8 prefix prerequisite differs")
		}
		if err := participant.Ledger.Close(); err != nil {
			t.Fatal(err)
		}
		dir := newAttemptSettlementRuntimeV2TestStateDir(t)
		if err := os.WriteFile(filepath.Join(dir, "attempt-ledger.jsonl"), attemptLedgerDiskTestJSONL(t, []AttemptRecord{record}), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stats.json"), initial, 0o600); err != nil {
			t.Fatal(err)
		}
		ledger, err := NewDiskAttemptLedger(t.Context(), dir, participant.Ledger.identity, attemptLedgerDiskTestCoordinator, self.fixtures[index].key, attemptLedgerDiskTestLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ledger.Close() })
		participants[index] = AttemptSettlementRuntimeV2Participant{NoID: participant.NoID, StateDir: dir, Stats: NewStatsEngine(participant.Stats.cfg), Ledger: ledger}
	}
	return participants
}

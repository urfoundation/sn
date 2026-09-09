//go:build linux || darwin

// Real M8 trails, signed disk ledgers, complete terminal sealing and two
// independent HTTP stores exercise the production publication boundary.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Transport storage is isolated; no test substitutes an accepted cut, fold,
// signature, finality claim or verifier result. The hotkey is an inert seed.
type evidenceCensusV2TestFixture struct {
	operators []*attemptCutV2StatsTestFixture
	closure   *AttemptSettlementClosureV2
	hotkey    *crv4.Keypair
	replicas  [2]AttemptCutV2Replica
	stores    [2]*attemptCutV2ReplicaTestStore
}

// The first operator may have real successful/failed trails. A second genuine
// empty operator forces complete idle/no-payout coverage in every test.
func newEvidenceCensusV2TestFixture(t *testing.T, completed, failed int) *evidenceCensusV2TestFixture {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x37})
	if err != nil {
		t.Fatal(err)
	}
	replicas, stores := newAttemptCutV2ReplicaTestStores(t)
	fixture := &evidenceCensusV2TestFixture{hotkey: hotkey, replicas: replicas, stores: stores}
	for index, noID := range []uint64{9, 11} {
		complete, fail := 0, 0
		if index == 0 {
			complete, fail = completed, failed
		}
		seal := newAttemptCutV2SealTestFixtureForOperator(t, 8, complete, fail, noID)
		seal.expected.Activation.Hotkey = hotkey.PublicKey()
		seal.expected.Activation.Domain.ActivationHash = sha256.Sum256([]byte(fmt.Sprintf("evidence-census-test/%d", noID)))
		// This fixture explicitly budgets typed metadata for the terminal
		// payload; production caps and the real M8/a_min8 policy are untouched.
		seal.bounds.Records.MaxPageBytes = 256 * 1024
		publication, err := SealReplicatedAttemptCutV2(t.Context(), seal.ledger, seal.expected, seal.policy, seal.key, seal.bounds, AttemptCutV2ReplicaOptions{ReplayBounds: seal.replay, ScratchDirectory: filepath.Join(t.TempDir(), "seal"), ServerKeys: seal.server.serverPublicKeys(), Replicas: replicas})
		if err != nil || publication == nil {
			t.Fatalf("real replicated fixture: %v", err)
		}
		measurement := func() ReleaseStatsMeasurement {
			seal.engine.stats.mu.Lock()
			defer seal.engine.stats.mu.Unlock()
			return seal.engine.stats.releaseStatsMeasurementWithLock()
		}()
		reader, err := NewHTTPAttemptStreamV2Reader(replicas[0].Origin, seal.bounds)
		if err != nil {
			t.Fatal(err)
		}
		fixture.operators = append(fixture.operators, &attemptCutV2StatsTestFixture{seal: seal, cut: *publication.Cut, measurement: measurement, metadata: reader.ReadMetadata, data: reader.OpenData})
	}
	fixture.closure = sealAttemptSettlementV2Test(t, fixture.operators...)
	return fixture
}

// Every retry and replica receives a fresh explicit replay name. Expected
// contexts come from the configured producer fixture, not candidate contents.
func (self *evidenceCensusV2TestFixture) options(t *testing.T) ValidatorEvidenceCensusV2Options {
	t.Helper()
	authority := attemptSettlementV2TestOptions(t, self.operators...)
	keys := make(map[uint64]ed25519.PrivateKey, len(self.operators))
	second := make(map[uint64]string, len(self.operators))
	for _, operator := range self.operators {
		noID := operator.seal.expected.Identity.NoID
		keys[noID] = bytes.Clone(operator.seal.key)
		second[noID] = filepath.Join(t.TempDir(), "replica-two")
	}
	boundary := self.operators[0].seal.expected.Boundary
	return ValidatorEvidenceCensusV2Options{Settlement: authority, Window: protocol.ValidatorEvidenceWindow{Epoch: boundary.SettlementEpoch, StartBlock: 1, EndBlock: boundary.EVMBlock + 1, FinalizedBlock: boundary.EVMBlock + 1}, PrivateKeys: keys, Hotkey: self.hotkey, Replicas: self.replicas, SecondReplicaScratchDirectories: second}
}

// Counts distinguish admission refusal from an otherwise hidden public read
// or immutable write. The fixture's earlier genuine publication is retained.
func (self *evidenceCensusV2TestFixture) counts() [2][2]int {
	var counts [2][2]int
	for index, store := range self.stores {
		_, writes, reads := store.snapshot()
		counts[index] = [2]int{writes, reads}
	}
	return counts
}

// Each independently readable hash leads back to the full signed transition;
// both consents bind the same complete census and exact generated ABI tuple.
func TestValidatorEvidenceCensusV2PublishesRealCompleteBatch(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 1, 1)
	options := fixture.options(t)
	for noID, operator := range options.Settlement.Operators {
		operator.Measurement.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
			return nil, errors.New("local metadata reader must not replace public replay")
		}
		operator.Measurement.Replay.OpenData = func(context.Context, string, string, uint64) (io.ReadCloser, error) {
			return nil, errors.New("local data reader must not replace public replay")
		}
		options.Settlement.Operators[noID] = operator
	}
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
	if err != nil || publication == nil {
		t.Fatalf("real complete evidence publication: %v", err)
	}
	if len(publication.Members) != 2 || publication.CensusHash != sha256.Sum256(publication.Census) || publication.Origins != [2]string{fixture.replicas[0].Origin, fixture.replicas[1].Origin} {
		t.Fatal("publication lost its exact complete census")
	}
	var census ValidatorEvidenceCensusV2
	if err := json.Unmarshal(publication.Census, &census); err != nil {
		t.Fatal(err)
	}
	if census.Schema != ValidatorEvidenceCensusV2Schema || census.Hotkey != fixture.hotkey.PublicKey() || len(census.Members) != 2 || census.Members[0].RecordCount != 10 || census.Members[0].CompleteCount != 1 || census.Members[0].FailedCount != 1 || census.Members[1].RecordCount != 0 {
		t.Fatalf("real complete/failed/idle census changed: %+v", census)
	}
	if bytes.Contains(publication.Census, []byte("census_hash")) || bytes.Contains(publication.Census, []byte("signature")) {
		t.Fatal("unsigned census recursively contains a header or consent")
	}
	contractABI, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	decoded := &AttemptSettlementClosureV2{Schema: AttemptSettlementClosureV2Schema, Epoch: fixture.closure.Epoch}
	for index, member := range publication.Members {
		header := member.Evidence.Header
		expected := options.Settlement.Operators[header.NoID].Expected
		if err := header.Verify(expected.Activation.Domain, options.Window, member.Evidence.VPKSignature, member.Evidence.HotkeySignature); err != nil {
			t.Fatalf("real dual consent: %v", err)
		}
		if header.CensusHash != publication.CensusHash || header.PayloadHash != sha256.Sum256(member.Payload) || header.PayloadBytes != uint64(len(member.Payload)) || member.SignedArtifactHash != sha256.Sum256(member.SignedArtifact) || census.Members[index].PayloadHash != header.PayloadHash || census.Members[index].PayloadBytes != header.PayloadBytes {
			t.Fatal("contract hashes lost exact public bytes")
		}
		if len(member.Calldata) < 4 || !bytes.Equal(member.Calldata[:4], contractABI.Methods["commitEvidence"].ID) {
			t.Fatal("publication did not produce actual evidence calldata")
		}
		if values, err := contractABI.Methods["commitEvidence"].Inputs.Unpack(member.Calldata[4:]); err != nil || len(values) != 3 {
			t.Fatalf("actual generated calldata decode: %v", err)
		}
		var transition AttemptSettlementTransitionV2
		if err := json.Unmarshal(member.Payload, &transition); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(&transition, fixture.closure.Transitions[index]) {
			t.Fatal("public payload changed signed terminal evidence")
		}
		decoded.Transitions = append(decoded.Transitions, &transition)
		for _, store := range fixture.stores {
			objects, _, _ := store.snapshot()
			if !bytes.Equal(objects["metadata/"+attemptHex32(header.PayloadHash)], member.Payload) || !bytes.Equal(objects["metadata/"+attemptHex32(publication.CensusHash)], publication.Census) || !bytes.Equal(objects["metadata/"+attemptHex32(member.SignedArtifactHash)], member.SignedArtifact) {
				t.Fatal("a public replica lacks exact payload, census or consents")
			}
		}
	}
	verified, err := VerifyAttemptSettlementClosureV2(t.Context(), decoded, fixture.options(t).Settlement)
	if err != nil || len(verified.Operators) != 2 || verified.Operators[9].Replay.CompleteCount != 1 || verified.Operators[9].Replay.FailedCount != 1 || verified.Operators[11].Replay.Records.ItemCount != 0 {
		t.Fatalf("independent complete public replay: %v", err)
	}
}

// A complete closed census is required even without trails, entitlements,
// payout roots or a cooperative operator-specific publishing decision.
func TestValidatorEvidenceCensusV2PublishesIdleNoPayoutMembers(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, fixture.options(t))
	if err != nil || publication == nil || len(publication.Members) != 2 {
		t.Fatalf("idle no-payout publication: %v", err)
	}
	for _, member := range publication.Members {
		if member.Evidence.Header.Kind != protocol.ValidatorEvidenceClosedCensus || member.Evidence.Header.Subject != (protocol.ValidatorEvidenceSubject{}) || len(member.Calldata) == 0 {
			t.Fatal("idle member omitted or moved into a later audit")
		}
	}
	for _, store := range fixture.stores {
		objects, _, _ := store.snapshot()
		for key := range objects {
			if !bytes.HasPrefix([]byte(key), []byte("metadata/")) {
				t.Fatal("idle evidence manufactured record or proof bytes")
			}
		}
	}
}

// A locally signed subset cannot authorize publication on behalf of the
// configured complete all-operator census, even if every included member is real.
func TestValidatorEvidenceCensusV2RejectsMissingMemberBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	candidate := cloneAttemptSettlementV2Test(t, fixture.closure)
	candidate.Transitions = candidate.Transitions[:1]
	before := fixture.counts()
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), candidate, fixture.options(t))
	if err == nil || publication != nil || fixture.counts() != before {
		t.Fatalf("incomplete census crossed admission: %v", err)
	}
}

// Missing, foreign and internally inconsistent private keys all fail before
// public reads. Valid candidate signatures never replace producer key custody.
func TestValidatorEvidenceCensusV2RejectsSignerChangesBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	foreign, err := crv4.KeypairFromSeed([32]byte{0x38})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ValidatorEvidenceCensusV2Options){
		func(options *ValidatorEvidenceCensusV2Options) { options.Hotkey = nil },
		func(options *ValidatorEvidenceCensusV2Options) { options.Hotkey = foreign },
		func(options *ValidatorEvidenceCensusV2Options) { options.Hotkey = &crv4.Keypair{} },
		func(options *ValidatorEvidenceCensusV2Options) { delete(options.PrivateKeys, 11) },
		func(options *ValidatorEvidenceCensusV2Options) {
			options.PrivateKeys[11] = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x48}, ed25519.SeedSize))
		},
		func(options *ValidatorEvidenceCensusV2Options) { options.PrivateKeys[11][0] ^= 1 },
	} {
		options := fixture.options(t)
		change(&options)
		before := fixture.counts()
		publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
		if err == nil || publication != nil || fixture.counts() != before {
			t.Fatalf("signer change crossed publication admission: %v", err)
		}
	}
}

// Finalized geometry is independently supplied. A signed terminal cannot
// choose a different end, an open observation or a later audit's subject.
func TestValidatorEvidenceCensusV2RejectsWindowChangesBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	for _, change := range []func(*protocol.ValidatorEvidenceWindow){
		func(window *protocol.ValidatorEvidenceWindow) { window.Epoch++ },
		func(window *protocol.ValidatorEvidenceWindow) { window.EndBlock++ },
		func(window *protocol.ValidatorEvidenceWindow) { window.FinalizedBlock-- },
		func(window *protocol.ValidatorEvidenceWindow) { window.Subject.ObservationEpoch = window.Epoch + 1 },
	} {
		options := fixture.options(t)
		change(&options.Window)
		before := fixture.counts()
		publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
		if err == nil || publication != nil || fixture.counts() != before {
			t.Fatalf("changed window crossed admission: %v", err)
		}
	}
}

// Each replica needs independent physical replay scratch. One borrowed map
// cannot silently select another member's path or reuse a completed verifier.
func TestValidatorEvidenceCensusV2RejectsScratchAliasesBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	for _, change := range []func(*ValidatorEvidenceCensusV2Options){
		func(options *ValidatorEvidenceCensusV2Options) {
			options.SecondReplicaScratchDirectories[11] = options.Settlement.Operators[9].Measurement.Replay.ScratchDirectory
		},
		func(options *ValidatorEvidenceCensusV2Options) {
			options.SecondReplicaScratchDirectories[11] = filepath.Join(options.Settlement.Operators[9].Measurement.Replay.ScratchDirectory, "nested")
		},
		func(options *ValidatorEvidenceCensusV2Options) { delete(options.SecondReplicaScratchDirectories, 11) },
	} {
		options := fixture.options(t)
		change(&options)
		before := fixture.counts()
		publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
		if err == nil || publication != nil || fixture.counts() != before {
			t.Fatalf("scratch alias crossed admission: %v", err)
		}
	}
}

// The second origin must serve every real proof through EOF; a good first
// copy or local reader cannot compensate for a truncated/corrupt public copy.
func TestValidatorEvidenceCensusV2RejectsSecondReplicaProofCorruption(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 1, 0)
	fixture.stores[1].readBytes = func(kind string, raw []byte) []byte {
		if kind == AttemptStreamV2Proofs {
			return append(raw, '\n')
		}
		return raw
	}
	before := fixture.counts()
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, fixture.options(t))
	after := fixture.counts()
	if err == nil || publication != nil || after[0][0] != before[0][0] || after[1][0] != before[1][0] || after[1][1] <= before[1][1] {
		t.Fatalf("bad public proof produced new publication: %v", err)
	}
}

// Re-signing forged statistics leaves the hash/signature structure valid but
// cannot change the assignments derived from both real public record streams.
func TestValidatorEvidenceCensusV2RejectsResignedStatistics(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 1, 0)
	candidate := cloneAttemptSettlementV2Test(t, fixture.closure)
	if len(candidate.Transitions[0].PreFold.Providers) == 0 {
		t.Fatal("real fixture lacks assigned providers")
	}
	candidate.Transitions[0].PreFold.Providers[0].Assignments++
	resignAttemptSettlementV2Test(t, candidate, fixture.operators...)
	before := fixture.counts()
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), candidate, fixture.options(t))
	after := fixture.counts()
	if err == nil || publication != nil || after[0][0] != before[0][0] || after[1][0] != before[1][0] {
		t.Fatalf("resigned false statistics produced a publication: %v", err)
	}
}

// A failed immutable payload write leaves staged evidence intact. A retry
// replays both public sources afresh and republishes the same content hashes.
func TestValidatorEvidenceCensusV2RetriesPartialPayloadPublication(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	injected := errors.New("second replica rejected signed terminal payload")
	fixture.stores[1].beforePut = func(_ context.Context, kind, _ string, raw []byte) error {
		if kind == "metadata" && bytes.Contains(raw, []byte(AttemptSettlementTransitionV2Schema)) {
			return injected
		}
		return nil
	}
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, fixture.options(t))
	if publication != nil || !errors.Is(err, injected) {
		t.Fatalf("partial payload publication escaped: %v", err)
	}
	before, _, _ := fixture.stores[0].snapshot()
	fixture.stores[1].beforePut = nil
	publication, err = PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, fixture.options(t))
	if err != nil || publication == nil {
		t.Fatalf("fresh immutable retry: %v", err)
	}
	after, _, _ := fixture.stores[0].snapshot()
	for key, raw := range before {
		if !bytes.Equal(raw, after[key]) {
			t.Fatal("retry replaced an existing immutable object")
		}
	}
	for _, store := range fixture.stores {
		objects, _, _ := store.snapshot()
		if !bytes.Equal(objects["metadata/"+attemptHex32(publication.CensusHash)], publication.Census) {
			t.Fatal("retry did not complete the census replicas")
		}
	}
}

// A late failure after real signatures exist is still an all-member refusal.
// No caller receives partially authorized calldata or an availability claim.
func TestValidatorEvidenceCensusV2RejectsSignedArtifactPublicationFailure(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	injected := errors.New("signed evidence artifact refused")
	fixture.stores[1].beforePut = func(_ context.Context, kind, _ string, raw []byte) error {
		if kind == "metadata" && bytes.Contains(raw, []byte(ValidatorEvidenceSignedV2Schema)) {
			return injected
		}
		return nil
	}
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, fixture.options(t))
	if publication != nil || !errors.Is(err, injected) {
		t.Fatalf("late consent artifact failure escaped: %v", err)
	}
}

// The first metadata callback runs only after admission and complete replay.
// Mutating all caller containers and private keys cannot change later members,
// while writer-owned buffers may be changed after immutable storage succeeds.
func TestValidatorEvidenceCensusV2OwnsInputsBeforePublisherCallbacks(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	options := fixture.options(t)
	wantHotkey := fixture.hotkey.PublicKey()
	want := cloneAttemptSettlementV2Test(t, fixture.closure)
	var once sync.Once
	fixture.stores[0].beforePut = func(context.Context, string, string, []byte) error {
		once.Do(func() {
			for _, key := range options.PrivateKeys {
				clear(key)
			}
			clear(options.PrivateKeys)
			clear(options.Settlement.Operators)
			clear(options.SecondReplicaScratchDirectories)
			fixture.closure.Transitions[1].Identity.NoID = 9
			clear(fixture.closure.Transitions[0].Signature)
			*fixture.hotkey = crv4.Keypair{}
		})
		return nil
	}
	fixture.stores[0].mutatePut, fixture.stores[1].mutatePut = true, true
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
	if err != nil || publication == nil || len(publication.Members) != 2 {
		t.Fatalf("borrowed mutation changed publication: %v", err)
	}
	for index, member := range publication.Members {
		var transition AttemptSettlementTransitionV2
		if err := json.Unmarshal(member.Payload, &transition); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(&transition, want.Transitions[index]) || member.Evidence.Header.Hotkey != wantHotkey || !member.Evidence.Header.VerifyHotkey(member.Evidence.HotkeySignature) || !member.Evidence.Header.VerifyVPK(member.Evidence.VPKSignature) {
			t.Fatal("borrowed callback changed admitted evidence or signer")
		}
	}
}

// Existing metadata allowances remain mandatory before any public access.
func TestValidatorEvidenceCensusV2RejectsMetadataCapacityBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	options := fixture.options(t)
	options.Settlement.MaxTransitionBytes = 1
	before := fixture.counts()
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
	if err == nil || publication != nil || fixture.counts() != before {
		t.Fatalf("metadata capacity silently expanded: %v", err)
	}
}

// A pre-cancelled operation cannot read or write either public store.
func TestValidatorEvidenceCensusV2PrecancelHasNoIO(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := fixture.counts()
	publication, err := PublishValidatorEvidenceClosedCensusV2(ctx, fixture.closure, fixture.options(t))
	if !errors.Is(err, context.Canceled) || publication != nil || fixture.counts() != before {
		t.Fatalf("precancel crossed publication admission: %v", err)
	}
}

// Cancellation during the final immutable consent phase clears the complete
// result only after both replica workers finish their owned operations.
func TestValidatorEvidenceCensusV2LateCancellationClearsCalldata(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.stores[1].beforePut = func(_ context.Context, kind, _ string, raw []byte) error {
		if kind == "metadata" && bytes.Contains(raw, []byte(ValidatorEvidenceSignedV2Schema)) {
			cancel()
		}
		return nil
	}
	publication, err := PublishValidatorEvidenceClosedCensusV2(ctx, fixture.closure, fixture.options(t))
	if !errors.Is(err, context.Canceled) || publication != nil {
		t.Fatalf("late cancellation returned authorized calldata: %v", err)
	}
}

// A worker exiting without a return is not a successful public acknowledgment.
func TestValidatorEvidenceCensusV2LostPublisherCompletionClearsCalldata(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 0, 0)
	fixture.stores[1].beforePut = func(_ context.Context, kind, _ string, raw []byte) error {
		if kind == "metadata" && bytes.Contains(raw, []byte(ValidatorEvidenceSignedV2Schema)) {
			runtime.Goexit()
		}
		return nil
	}
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, fixture.options(t))
	if err == nil || publication != nil {
		t.Fatalf("nonreturning publisher authorized a commitment: %v", err)
	}
}

// Missing context is a refusal before accessing absent configuration or keys.
func TestValidatorEvidenceCensusV2RejectsNilContext(t *testing.T) {
	publication, err := PublishValidatorEvidenceClosedCensusV2(nil, nil, ValidatorEvidenceCensusV2Options{})
	if err == nil || publication != nil {
		t.Fatalf("missing context admitted publication: %v", err)
	}
}

// Both actual public HTTP readers reach an explicit barrier independently.
// Publication returns only after they are released and all real replay joins.
func TestValidatorEvidenceCensusV2ReplaysPublicReplicasConcurrently(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceCensusV2TestFixture(t, 1, 0)
	entered := make(chan int, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var observed [2]sync.Once
	for index, store := range fixture.stores {
		store.readBytes = func(_ string, raw []byte) []byte {
			observed[index].Do(func() { entered <- index; <-release })
			return raw
		}
	}
	type outcome struct {
		publication *ValidatorEvidenceCensusV2Publication
		err         error
	}
	done := make(chan outcome, 1)
	options := fixture.options(t)
	go func() {
		publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.closure, options)
		done <- outcome{publication: publication, err: err}
	}()
	var seen [2]bool
	for count := 0; count < 2; count++ {
		select {
		case index := <-entered:
			if seen[index] {
				t.Fatal("one replica replaced the independent reader")
			}
			seen[index] = true
		case result := <-done:
			t.Fatalf("publication returned before both public readers joined the barrier: %v", result.err)
		}
	}
	releaseOnce.Do(func() { close(release) })
	result := <-done
	if result.err != nil || result.publication == nil {
		t.Fatalf("joined independent public replays: %v", result.err)
	}
}

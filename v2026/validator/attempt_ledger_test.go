package validator

// These tests pin the durable measurement transaction: server assignments and
// outcomes enter the signed ledger before derived statistics, cuts are exact
// replays, and binding generations cannot inherit one another's work.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

func attemptLedgerTestBoundary() AttemptBoundary {
	return AttemptBoundary{SettlementEpoch: 42, EVMBlock: 100, EVMBlockHash: attemptHex32([32]byte{1})}
}

func attemptLedgerTestBinding(clientID connect.Id, generation uint64) AttemptBinding {
	fleetID := [32]byte{2}
	hotkey := [32]byte{3}
	fleetID[31] = byte(generation)
	hotkey[31] = byte(generation)
	return AttemptBinding{
		ClientID: clientID, Active: true, FleetID: attemptHex32(fleetID),
		Hotkey: attemptHex32(hotkey), Generation: generation, UIDFound: true,
		UID: uint16(generation),
	}
}

func TestAttemptRecordHashKnownDomainVector(t *testing.T) {
	record := AttemptRecord{
		Schema: attemptLedgerRecordSchema,
		Identity: AttemptLedgerIdentity{
			DeploymentID: "known-vector", ChainID: 945, GenesisHash: attemptHex32([32]byte{1}),
			Netuid: 521, ValidatorID: 2, ValidatorUID: 7, NoID: 3, ValidatorVPK: attemptHex32([32]byte{2}),
		},
		Sequence: 1, PreviousHash: zeroAttemptHash(), Boundary: attemptLedgerTestBoundary(),
		TrailID: connect.Id{1}, ServerNonce: []byte{1, 2, 3}, VPK: []byte{4, 5}, M: 4,
		Disposition: AttemptDispositionPending,
	}
	digest, err := attemptRecordHash(&record)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "0x6915586a0d44659ffb389add1c288ce0ac26ab2c5a77732b299c8ccbac1a5466"
	if attemptHex32(digest) != expected {
		t.Fatalf("attempt record domain vector = %s", attemptHex32(digest))
	}
}

func configureAttemptLedgerTestEngine(t *testing.T, engine *TrailEngine, stats *StatsEngine, stateDir string, generation *uint64) *AttemptLedger {
	t.Helper()
	if err := stats.AdvanceSettlementEpoch(42, stateDir); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewAttemptLedger(stateDir, AttemptLedgerIdentity{
		DeploymentID: "attempt-ledger-test", ChainID: 945,
		GenesisHash: attemptHex32([32]byte{4}), Netuid: 521,
		ValidatorID: 1, ValidatorUID: 7, NoID: 9,
	}, engine.vsk)
	if err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(ledger, stateDir); err != nil {
		t.Fatal(err)
	}
	boundary := attemptLedgerTestBoundary()
	engine.ledger = ledger
	engine.resolve = func(_ context.Context, pinned *AttemptBoundary, clientIDs []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
		if pinned != nil && *pinned != boundary {
			return AttemptBoundary{}, nil, errors.New("test pin changed")
		}
		bindings := make([]AttemptBinding, len(clientIDs))
		for index, clientID := range clientIDs {
			bindings[index] = attemptLedgerTestBinding(clientID, *generation)
		}
		return boundary, bindings, nil
	}
	return ledger
}

func cloneAttemptLedgerCut(t *testing.T, cut *AttemptLedgerCut) *AttemptLedgerCut {
	t.Helper()
	encoded, err := json.Marshal(cut)
	if err != nil {
		t.Fatal(err)
	}
	var cloned AttemptLedgerCut
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		t.Fatal(err)
	}
	return &cloned
}

func resignAttemptRecordProof(t *testing.T, record *AttemptRecord, validatorKey, serverKey ed25519.PrivateKey) {
	t.Helper()
	proof := record.Proof
	finalMessage, err := connect.BuildVerifyFinalMessage(proof.ServerKeyId, proof.TrailId, proof.ServerNonce, proof.Vpk, byte(proof.M), proof.Hops)
	if err != nil {
		t.Fatal(err)
	}
	digest := connect.VerifyFinalDigest(finalMessage)
	proof.FinalDigest = append([]byte(nil), digest[:]...)
	proof.FinalSig = ed25519.Sign(serverKey, finalMessage)
	proof.VpkSig = ed25519.Sign(validatorKey, finalMessage)
	walked := make([]connect.Id, len(proof.Hops))
	for index := range proof.Hops {
		walked[index] = proof.Hops[index].ClientId
	}
	extendMessage, err := connect.BuildVerifyExtendMessage(proof.TrailId, proof.ServerNonce, proof.Vpk, byte(proof.M), walked)
	if err != nil {
		t.Fatal(err)
	}
	proof.VerifierSig = ed25519.Sign(validatorKey, extendMessage)
	hash, err := attemptRecordHash(record)
	if err != nil {
		t.Fatal(err)
	}
	record.RecordHash = attemptHex32(hash)
	record.Signature = ed25519.Sign(validatorKey, attemptRecordSignatureMessage(hash))
}

func TestAttemptLedgerHappyPathReplaysExactStatsAndProofs(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 5, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)

	proof, err := engine.RunTrail(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if proof.Epoch != 42 || ledger.LastSequence() != 5 {
		t.Fatalf("proof epoch/ledger sequence = %d/%d, want 42/5", proof.Epoch, ledger.LastSequence())
	}
	records, err := ledger.RecordsAfter(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 || records[4].Disposition != AttemptDispositionComplete {
		t.Fatalf("attempt checkpoint/terminal records = %+v", records)
	}
	for index := 0; index < 4; index++ {
		if records[index].Disposition != AttemptDispositionPending || len(records[index].Assignments) != index+1 {
			t.Fatalf("attempt checkpoint %d = disposition %q assignments %d", index, records[index].Disposition, len(records[index].Assignments))
		}
	}
	measurement, err := stats.detachReleaseStatsMeasurementWithAttemptCut(stateDir, AttemptBoundary{
		SettlementEpoch: 42, EVMBlock: 101, EVMBlockHash: attemptHex32([32]byte{5}),
	}, func(ReleaseStatsMeasurement, uint64) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if measurement.AttemptCut == nil {
		t.Fatal("release measurement omitted its signed attempt cut")
	}
	vpk := validatorKey.Public().(ed25519.PublicKey)
	if err := VerifyAttemptLedgerCut(measurement.AttemptCut, vpk, server.serverPublicKeys()); err != nil {
		t.Fatalf("independent attempt cut verification: %v", err)
	}
	verified, err := VerifyReleaseStatsMeasurement(measurement)
	if err != nil {
		t.Fatal(err)
	}
	var assignments, confirmations uint64
	for _, provider := range measurement.Providers {
		assignments += provider.Assignments
		confirmations += provider.Confirmations
	}
	if assignments != 4 || confirmations != 4 || len(verified.Providers) != 4 {
		t.Fatalf("replayed assignments/confirmations/providers = %d/%d/%d, want 4/4/4", assignments, confirmations, len(verified.Providers))
	}
	claims, err := AttemptCutEgressClaims(measurement.AttemptCut)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 4 {
		t.Fatalf("generation-bound egress claims = %d, want 4", len(claims))
	}
	for _, claim := range claims {
		if claim.Binding.Generation != 1 {
			t.Fatalf("egress claim inherited generation %d, want 1", claim.Binding.Generation)
		}
	}
}

func TestAttemptLedgerAppendFailureLeavesStatsUnchanged(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	expected := errors.New("synthetic ledger append failure")
	ledger.appendFn = func(string, []byte) error { return expected }

	if _, err := engine.RunTrail(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("trail error = %v, want append failure", err)
	}
	if ledger.LastSequence() != 0 || len(stats.ProviderIDs()) != 0 {
		t.Fatalf("failed append mutated sequence/providers = %d/%v", ledger.LastSequence(), stats.ProviderIDs())
	}
}

func TestAttemptLedgerProofProjectionFailurePreservesAuthoritativeCompletion(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	store.path = stateDir

	if _, err := engine.RunTrail(context.Background()); err == nil {
		t.Fatal("proof append failure unexpectedly succeeded")
	}
	if ledger.LastSequence() != 4 || len(stats.ProviderIDs()) != 3 {
		t.Fatalf("failed proof projection sequence/providers = %d/%v, want 4/3", ledger.LastSequence(), stats.ProviderIDs())
	}
	restartedStore, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := restartedStore.ReconcileAttemptProofs(ledger); err != nil {
		t.Fatal(err)
	}
	proofs, skipped, err := restartedStore.Load()
	if err != nil || skipped != 0 || len(proofs) != 1 {
		t.Fatalf("reconciled proof projection = proofs %d skipped %d err %v", len(proofs), skipped, err)
	}
}

func TestAttemptLedgerTerminalAppendFailureCannotPublishOrphanProof(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	appendLedger := ledger.appendFn
	expected := errors.New("synthetic terminal append failure")
	ledger.appendFn = func(path string, payload []byte) error {
		if bytes.Contains(payload, []byte(`"disposition":"complete"`)) {
			return expected
		}
		return appendLedger(path, payload)
	}

	if _, err := engine.RunTrail(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("trail error = %v, want terminal append failure", err)
	}
	proofs, skipped, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(proofs) != 0 || skipped != 0 || ledger.LastSequence() != 3 || len(stats.ProviderIDs()) != 0 {
		t.Fatalf("failed terminal append published state: proofs=%d skipped=%d sequence=%d providers=%v", len(proofs), skipped, ledger.LastSequence(), stats.ProviderIDs())
	}
}

func TestAttemptProofProjectionRepairsSuffixAndRejectsConflict(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	expected, err := attemptProofProjection(ledger)
	if err != nil || len(expected) < 4 {
		t.Fatalf("attempt proof projection = %d bytes, err %v", len(expected), err)
	}

	if err := os.Remove(store.path); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileAttemptProofs(ledger); err != nil {
		t.Fatalf("missing projection repair: %v", err)
	}
	actual, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("missing projection repair differs: %v", err)
	}

	if err := os.WriteFile(store.path, append(append([]byte(nil), expected...), expected...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileAttemptProofs(ledger); err == nil {
		t.Fatal("duplicate proof projection unexpectedly reconciled")
	}
	if err := os.WriteFile(store.path, expected, 0o600); err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), expected...)
	tampered[len(tampered)/2] ^= 1
	if err := os.WriteFile(store.path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileAttemptProofs(ledger); err == nil {
		t.Fatal("tampered proof projection unexpectedly reconciled")
	}
	if err := os.WriteFile(store.path, expected[:len(expected)-3], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileAttemptProofs(ledger); err != nil {
		t.Fatalf("torn projection suffix repair: %v", err)
	}

	if err := os.Remove(store.path); err != nil {
		t.Fatal(err)
	}
	expectedWriteError := errors.New("synthetic projection write failure")
	if err := store.reconcileAttemptProofsWithWrite(ledger, func(string, []byte) error { return expectedWriteError }); !errors.Is(err, expectedWriteError) {
		t.Fatalf("projection write failure = %v", err)
	}
}

func TestAttemptLedgerPersistsServerAssignmentForHopFailure(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 8)
	for _, providerID := range server.providers[1:] {
		server.failHops[providerID] = true
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if _, err := engine.RunTrail(context.Background()); err == nil {
		t.Fatal("unreachable assigned hop unexpectedly completed")
	}
	cut, err := ledger.BuildCut(AttemptBoundary{SettlementEpoch: 42, EVMBlock: 101, EVMBlockHash: attemptHex32([32]byte{5})}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttemptLedgerCut(cut, validatorKey.Public().(ed25519.PublicKey), server.serverPublicKeys()); err != nil {
		t.Fatalf("failed-attempt assignment verification: %v", err)
	}
	if len(cut.Records) != 2 || cut.Records[1].Disposition != AttemptDispositionHopFailure || len(cut.Records[1].Assignments[0].AssignMessage) == 0 || len(cut.Records[1].Assignments[0].AssignSignature) != ed25519.SignatureSize {
		t.Fatalf("failed attempt did not retain signed ASSIGN: %+v", cut.Records)
	}
	measurement := stats.currentReleaseStatsMeasurement()
	if len(measurement.Providers) != 1 || measurement.Providers[0].Assignments != 1 || measurement.Providers[0].Confirmations != 0 {
		t.Fatalf("failed-hop derived stats = %+v", measurement.Providers)
	}
}

func TestAttemptLedgerCountsStaleHotkeyExposureWithoutHeadCredit(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	resolve := engine.resolve
	engine.resolve = func(ctx context.Context, pinned *AttemptBoundary, clientIDs []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
		boundary, bindings, err := resolve(ctx, pinned, clientIDs)
		for index := range bindings {
			bindings[index].UIDFound = false
			bindings[index].UID = 0
		}
		return boundary, bindings, err
	}
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	cut, err := ledger.BuildCut(AttemptBoundary{SettlementEpoch: 42, EVMBlock: 101, EVMBlockHash: attemptHex32([32]byte{5})}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := AttemptCutEgressClaims(cut)
	if err != nil {
		t.Fatal(err)
	}
	var assignments uint64
	for _, provider := range stats.currentReleaseStatsMeasurement().Providers {
		assignments += provider.Assignments
	}
	if assignments != 3 || len(claims) != 0 {
		t.Fatalf("stale binding assignments/head claims = %d/%d, want 3/0", assignments, len(claims))
	}
}

func TestAttemptLedgerRestartConservativelyFinalizesPendingCheckpoint(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	appendLedger := ledger.appendFn
	ledger.appendFn = func(path string, payload []byte) error {
		if bytes.Contains(payload, []byte(`"disposition":"complete"`)) {
			return errors.New("synthetic terminal append failure")
		}
		return appendLedger(path, payload)
	}
	if _, err := engine.RunTrail(context.Background()); err == nil || ledger.LastSequence() != 3 {
		t.Fatalf("terminal append failure/last checkpoint = %v/%d", err, ledger.LastSequence())
	}

	restartedStats := NewStatsEngine(StatsConfig{AMin: 1})
	if err := restartedStats.Load(stateDir); err != nil {
		t.Fatal(err)
	}
	restartedLedger, err := NewAttemptLedger(stateDir, ledger.identity, validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := restartedStats.AttachAttemptLedger(restartedLedger, stateDir); err != nil {
		t.Fatal(err)
	}
	var assignments, confirmations uint64
	for _, provider := range restartedStats.currentReleaseStatsMeasurement().Providers {
		assignments += provider.Assignments
		confirmations += provider.Confirmations
		if len(provider.EgressIPHashHexes) != 0 {
			t.Fatal("recovered pending attempt manufactured egress evidence")
		}
	}
	if assignments != 3 || confirmations != 2 || restartedLedger.LastSequence() != 4 {
		t.Fatalf("recovered pending assignments/confirmations/sequence = %d/%d/%d, want 3/2/4", assignments, confirmations, restartedLedger.LastSequence())
	}
}

func TestAttemptLedgerCutBarrierDoesNotSplitTrail(t *testing.T) {
	stateDir := t.TempDir()
	_, validatorKey, _ := newMockVerifyServer(t, 4)
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	if err := stats.AdvanceSettlementEpoch(42, stateDir); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewAttemptLedger(stateDir, AttemptLedgerIdentity{
		DeploymentID: "attempt-ledger-test", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}),
		Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: 9,
	}, validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(ledger, stateDir); err != nil {
		t.Fatal(err)
	}
	if err := stats.beginAttempt(42, ledger); err != nil {
		t.Fatal(err)
	}
	boundary := attemptLedgerTestBoundary()
	if _, err := stats.detachReleaseStatsMeasurementWithAttemptCut(stateDir, boundary, func(ReleaseStatsMeasurement, uint64) error { return nil }); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("active-attempt cut error = %v", err)
	}
	if err := stats.beginAttempt(42, ledger); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("new attempt entered pending cut: %v", err)
	}
	stats.abortAttempt()
	measurement, err := stats.detachReleaseStatsMeasurementWithAttemptCut(stateDir, boundary, func(ReleaseStatsMeasurement, uint64) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if measurement.AttemptCut == nil || measurement.AttemptCut.RecordCount != 0 {
		t.Fatalf("empty cut = %+v", measurement.AttemptCut)
	}
}

func TestAttemptLedgerReconciliationReopensTrailsAfterSaveFailure(t *testing.T) {
	stateDir := t.TempDir()
	_, validatorKey, _ := newMockVerifyServer(t, 4)
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	if err := stats.AdvanceSettlementEpoch(42, stateDir); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewAttemptLedger(stateDir, AttemptLedgerIdentity{
		DeploymentID: "attempt-ledger-test", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}),
		Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: 9,
	}, validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(ledger, stateDir); err != nil {
		t.Fatal(err)
	}
	notDirectory := stateDir + "/not-a-directory"
	if err := os.WriteFile(notDirectory, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var persisted ReleaseStatsMeasurement
	var generation uint64
	_, err = stats.detachReleaseStatsMeasurementWithAttemptCut(notDirectory, attemptLedgerTestBoundary(), func(measurement ReleaseStatsMeasurement, value uint64) error {
		persisted, generation = measurement, value
		return nil
	})
	if err == nil || persisted.AttemptCut == nil || !stats.attemptCutPending {
		t.Fatalf("cut save failure = err %v cut %v pending %t", err, persisted.AttemptCut, stats.attemptCutPending)
	}
	if err := stats.reconcileReleaseStatsCut(stateDir, generation, persisted.AttemptCut); err != nil {
		t.Fatal(err)
	}
	if err := stats.beginAttempt(42, ledger); err != nil {
		t.Fatalf("verified reconciliation left trails blocked: %v", err)
	}
	stats.abortAttempt()
}

func TestAttemptLedgerCutRejectsMutationReplayOmissionAndReorder(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 16)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	cut, err := ledger.BuildCut(AttemptBoundary{SettlementEpoch: 42, EVMBlock: 101, EVMBlockHash: attemptHex32([32]byte{5})}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	vpk := validatorKey.Public().(ed25519.PublicKey)
	if err := VerifyAttemptLedgerCut(cut, vpk, server.serverPublicKeys()); err != nil {
		t.Fatal(err)
	}

	mutatedEpoch := cloneAttemptLedgerCut(t, cut)
	mutatedEpoch.Records[0].Boundary.SettlementEpoch++
	omitted := cloneAttemptLedgerCut(t, cut)
	omitted.Records = omitted.Records[1:]
	replayed := cloneAttemptLedgerCut(t, cut)
	replayed.Records = append(replayed.Records, replayed.Records[1])
	reordered := cloneAttemptLedgerCut(t, cut)
	reordered.Records[0], reordered.Records[1] = reordered.Records[1], reordered.Records[0]
	wrongNO := cloneAttemptLedgerCut(t, cut)
	wrongNO.Identity.NoID++
	wrongProofEpoch := cloneAttemptLedgerCut(t, cut)
	for index := range wrongProofEpoch.Records {
		if wrongProofEpoch.Records[index].Proof != nil {
			wrongProofEpoch.Records[index].Proof.Epoch++
			break
		}
	}
	for name, invalid := range map[string]*AttemptLedgerCut{
		"epoch": mutatedEpoch, "omission": omitted, "replay": replayed,
		"reorder": reordered, "no_id": wrongNO, "proof_epoch": wrongProofEpoch,
	} {
		if err := VerifyAttemptLedgerCut(invalid, vpk, server.serverPublicKeys()); err == nil {
			t.Errorf("%s mutation unexpectedly verified", name)
		}
	}
}

func TestAttemptLedgerRejectsCryptographicallyValidSplicedProofPath(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 16)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	records, err := ledger.RecordsAfter(0)
	if err != nil {
		t.Fatal(err)
	}
	terminal := records[len(records)-1]
	for hopIndex := range terminal.Proof.Hops {
		candidate, err := cloneAttemptRecord(terminal)
		if err != nil {
			t.Fatal(err)
		}
		candidate.Proof.Hops[hopIndex].ClientId = connect.NewId()
		resignAttemptRecordProof(t, &candidate, validatorKey, server.serverKey)
		if err := VerifyAttemptRecord(&candidate, ledger.identity, validatorKey.Public().(ed25519.PublicKey), server.serverPublicKeys()); err == nil {
			t.Errorf("cryptographically valid proof splice at hop %d unexpectedly verified", hopIndex)
		}
	}
}

func TestAttemptLedgerRejectsTrailIDReuseAfterTerminalOutcome(t *testing.T) {
	for _, fail := range []bool{false, true} {
		stateDir := t.TempDir()
		server, validatorKey, clientID := newMockVerifyServer(t, 12)
		if fail {
			for _, providerID := range server.providers[1:] {
				server.failHops[providerID] = true
			}
		}
		engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, nil)
		generation := uint64(1)
		ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
		_, _ = engine.RunTrail(context.Background())
		records, err := ledger.RecordsAfter(0)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) < 2 {
			t.Fatal("trail did not produce a terminal lifecycle")
		}
		reused := records[0]
		reused.Disposition = AttemptDispositionPending
		reused.Proof = nil
		if _, err := ledger.Append(reused); err == nil {
			t.Errorf("terminal fail=%t trail id reuse unexpectedly succeeded", fail)
		}
	}
}

func TestAttemptLedgerRestartReplaysFsyncedStatsExactlyOnce(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ledger.LastSequence() != 4 {
		t.Fatal("attempt record was not durable")
	}

	restartedStats := NewStatsEngine(StatsConfig{AMin: 1})
	if err := restartedStats.Load(stateDir); err != nil {
		t.Fatal(err)
	}
	restartedLedger, err := NewAttemptLedger(stateDir, ledger.identity, validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := restartedStats.AttachAttemptLedger(restartedLedger, stateDir); err != nil {
		t.Fatal(err)
	}
	var assignments uint64
	for _, provider := range restartedStats.currentReleaseStatsMeasurement().Providers {
		assignments += provider.Assignments
	}
	if assignments != 3 {
		t.Fatalf("replayed assignments = %d, want 3", assignments)
	}

	secondRestart := NewStatsEngine(StatsConfig{AMin: 1})
	if err := secondRestart.Load(stateDir); err != nil {
		t.Fatal(err)
	}
	if err := secondRestart.AttachAttemptLedger(restartedLedger, stateDir); err != nil {
		t.Fatal(err)
	}
	assignments = 0
	for _, provider := range secondRestart.currentReleaseStatsMeasurement().Providers {
		assignments += provider.Assignments
	}
	if assignments != 3 {
		t.Fatalf("second restart duplicated assignments: %d", assignments)
	}
}

func TestAttemptLedgerKeepsReboundGenerationsSeparate(t *testing.T) {
	stateDir := t.TempDir()
	server, validatorKey, clientID := newMockVerifyServer(t, 16)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 4, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	generation = 2
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	cut, err := ledger.BuildCut(AttemptBoundary{SettlementEpoch: 42, EVMBlock: 101, EVMBlockHash: attemptHex32([32]byte{5})}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := AttemptCutEgressClaims(cut)
	if err != nil {
		t.Fatal(err)
	}
	generations := map[uint64]bool{}
	for _, claim := range claims {
		generations[claim.Binding.Generation] = true
	}
	if !generations[1] || !generations[2] {
		t.Fatalf("generation-separated claims = %v, want generations 1 and 2", generations)
	}

	encoded, err := os.ReadFile(ledger.path)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("durable attempt ledger: bytes=%d err=%v", len(encoded), err)
	}
}

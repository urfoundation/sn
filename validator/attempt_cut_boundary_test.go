package validator

// Fully signed M8 histories distinguish contradictory block hashes from bad
// signatures at record, cut, measurement and consecutive-cut boundaries.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

// Owns one complete real trail, its durable ledger and the unchanged policy
// statistics. No proof, checkpoint or provider exposure is synthesized.
type attemptCutBoundaryFixtureState struct {
	ledger       *AttemptLedger
	stats        *StatsEngine
	cut          *AttemptLedgerCut
	validatorKey ed25519.PrivateKey
	serverKeyKVs map[byte]ed25519.PublicKey
}

// Completes all seven ASSIGN checkpoints and FINAL through the real engine.
func attemptCutBoundaryFixture(t *testing.T, noID uint64) attemptCutBoundaryFixtureState {
	t.Helper()
	server, _, clientID := newMockVerifyServer(t, 12)
	validatorKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x6a}, ed25519.SeedSize))
	server.validatorVpk = validatorKey.Public().(ed25519.PublicKey)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 8, nil)
	stats.cfg = (StatsConfig{AMin: exactPolicy(t).Verify.ReliabilityAMin}).withDefaults()
	stateDir := t.TempDir()
	if err := stats.AdvanceSettlementEpoch(42, stateDir); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewAttemptLedger(stateDir, AttemptLedgerIdentity{
		DeploymentID: "attempt-cut-boundary-test", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}),
		Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: noID,
	}, validatorKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(ledger, stateDir); err != nil {
		t.Fatal(err)
	}
	engine.ledger = ledger
	engine.resolve = func(_ context.Context, pinned *AttemptBoundary, clientIDs []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
		boundary := attemptLedgerTestBoundary()
		if pinned != nil && *pinned != boundary {
			return AttemptBoundary{}, nil, errors.New("cut-boundary fixture changed its admission view")
		}
		bindings := make([]AttemptBinding, len(clientIDs))
		for index, id := range clientIDs {
			bindings[index] = attemptLedgerTestBinding(id, noID)
			bindings[index].UID = uint16(10 + noID)
		}
		return boundary, bindings, nil
	}
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	cut, err := ledger.BuildCut(attemptLedgerTestBoundary(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	serverKeys := server.serverPublicKeys()
	if err := VerifyAttemptLedgerCut(cut, validatorKey.Public().(ed25519.PublicKey), serverKeys); err != nil {
		t.Fatalf("complete M8 fixture failed full public authentication: %v", err)
	}
	assignmentCopies := 0
	for index, record := range cut.Records {
		assignmentCopies += len(record.Assignments)
		if record.M != 8 || record.Boundary != attemptLedgerTestBoundary() || index < 7 && record.Disposition != AttemptDispositionPending || index == 7 && record.Disposition != AttemptDispositionComplete {
			t.Fatal("fixture omitted or changed a real M8 lifecycle record")
		}
	}
	if len(cut.Records) != 8 || assignmentCopies != 35 {
		t.Fatalf("fixture census = %d records / %d ASSIGN copies, want 8 / 35", len(cut.Records), assignmentCopies)
	}
	return attemptCutBoundaryFixtureState{ledger: ledger, stats: stats, cut: cut, validatorKey: validatorKey, serverKeyKVs: serverKeys}
}

// Re-signs only the cut boundary. Every record hash, record signature, server
// ASSIGN and completed proof remains exactly as produced by the engine.
func attemptCutBoundaryResign(t *testing.T, original *AttemptLedgerCut, key ed25519.PrivateKey, boundary AttemptBoundary) *AttemptLedgerCut {
	t.Helper()
	cut := cloneAttemptLedgerCut(t, original)
	cut.Boundary = boundary
	message, err := attemptCutSignatureMessage(cut)
	if err != nil {
		t.Fatal(err)
	}
	cut.Signature = ed25519.Sign(key, message)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), message, cut.Signature) {
		t.Fatal("boundary control did not retain a genuine cut signature")
	}
	return cut
}

// A validator signature over a contradictory outer view cannot make the
// unchanged, independently signed records belong to that block hash.
func TestAttemptCutBoundaryPublicRejectsSameHeightHash(t *testing.T) {
	fixture := attemptCutBoundaryFixture(t, 1)
	boundary := fixture.cut.Boundary
	boundary.EVMBlockHash = attemptHex32([32]byte{0x51})
	cut := attemptCutBoundaryResign(t, fixture.cut, fixture.validatorKey, boundary)
	if err := VerifyAttemptLedgerCut(cut, fixture.validatorKey.Public().(ed25519.PublicKey), fixture.serverKeyKVs); err == nil || !strings.Contains(err.Error(), "boundary") {
		t.Fatalf("public cut accepted a contradictory same-height hash: %v", err)
	}
}

// Refusal must precede cut publication and leave the durable history usable
// for a valid same-height retry without rewriting any byte or cursor.
func TestAttemptCutBoundaryBuildRejectsSameHeightHash(t *testing.T) {
	fixture := attemptCutBoundaryFixture(t, 1)
	before, err := os.ReadFile(fixture.ledger.path)
	if err != nil {
		t.Fatal(err)
	}
	boundary := fixture.cut.Boundary
	boundary.EVMBlockHash = attemptHex32([32]byte{0x51})
	cut, buildErr := fixture.ledger.BuildCut(boundary, 1, 1)
	if buildErr == nil || !strings.Contains(buildErr.Error(), "boundary") || cut != nil {
		t.Errorf("BuildCut published a contradictory same-height hash: cut=%t error=%v", cut != nil, buildErr)
	}
	after, err := os.ReadFile(fixture.ledger.path)
	if err != nil || !bytes.Equal(before, after) || fixture.ledger.LastSequence() != 8 {
		t.Fatalf("boundary refusal changed durable history: %v", err)
	}
	retry, err := fixture.ledger.BuildCut(fixture.cut.Boundary, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttemptLedgerCut(retry, fixture.validatorKey.Public().(ed25519.PublicKey), fixture.serverKeyKVs); err != nil {
		t.Fatalf("valid retry after boundary refusal failed: %v", err)
	}
}

// Exact equality at the current block remains a valid public replay.
func TestAttemptCutBoundaryPublicAcceptsSameHash(t *testing.T) {
	fixture := attemptCutBoundaryFixture(t, 1)
	cut := attemptCutBoundaryResign(t, fixture.cut, fixture.validatorKey, fixture.cut.Boundary)
	if err := VerifyAttemptLedgerCut(cut, fixture.validatorKey.Public().(ed25519.PublicKey), fixture.serverKeyKVs); err != nil {
		t.Fatal(err)
	}
}

// Earlier records normally have a different block hash and must be retained.
func TestAttemptCutBoundaryPublicAcceptsEarlierHeight(t *testing.T) {
	fixture := attemptCutBoundaryFixture(t, 1)
	boundary := fixture.cut.Boundary
	boundary.EVMBlock++
	boundary.EVMBlockHash = attemptHex32([32]byte{0x52})
	cut := attemptCutBoundaryResign(t, fixture.cut, fixture.validatorKey, boundary)
	if err := VerifyAttemptLedgerCut(cut, fixture.validatorKey.Public().(ed25519.PublicKey), fixture.serverKeyKVs); err != nil {
		t.Fatal(err)
	}
}

// The producer may publish the exact block that admitted the complete trail.
func TestAttemptCutBoundaryBuildAcceptsSameHash(t *testing.T) {
	fixture := attemptCutBoundaryFixture(t, 1)
	cut, err := fixture.ledger.BuildCut(fixture.cut.Boundary, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttemptLedgerCut(cut, fixture.validatorKey.Public().(ed25519.PublicKey), fixture.serverKeyKVs); err != nil {
		t.Fatal(err)
	}
}

// Advancing the cut retains older signed records without relabeling them.
func TestAttemptCutBoundaryBuildAcceptsEarlierHeight(t *testing.T) {
	fixture := attemptCutBoundaryFixture(t, 1)
	boundary := fixture.cut.Boundary
	boundary.EVMBlock++
	boundary.EVMBlockHash = attemptHex32([32]byte{0x52})
	cut, err := fixture.ledger.BuildCut(boundary, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttemptLedgerCut(cut, fixture.validatorKey.Public().(ed25519.PublicKey), fixture.serverKeyKVs); err != nil {
		t.Fatal(err)
	}
}

// The new equality rule cannot weaken the existing epoch or height checks.
func TestAttemptCutBoundaryRetainsEpochAndFutureRecordRejections(t *testing.T) {
	fixture := attemptCutBoundaryFixture(t, 1)
	for _, boundary := range []AttemptBoundary{
		{SettlementEpoch: 43, EVMBlock: 100, EVMBlockHash: fixture.cut.Boundary.EVMBlockHash},
		{SettlementEpoch: 42, EVMBlock: 99, EVMBlockHash: attemptHex32([32]byte{0x53})},
	} {
		cut := attemptCutBoundaryResign(t, fixture.cut, fixture.validatorKey, boundary)
		if err := VerifyAttemptLedgerCut(cut, fixture.validatorKey.Public().(ed25519.PublicKey), fixture.serverKeyKVs); err == nil || !strings.Contains(err.Error(), "boundary") {
			t.Fatalf("invalid public epoch/height was accepted: %+v %v", boundary, err)
		}
		if cut, err := fixture.ledger.BuildCut(boundary, 1, 1); err == nil || cut != nil || !strings.Contains(err.Error(), "boundary") {
			t.Fatalf("invalid producer epoch/height was accepted: %+v %v", boundary, err)
		}
	}
}

// Builds two real operator inputs and reconstructs their complete positive
// head census from the actual proofs. No reduced policy or fake counter is
// needed: routable proof coverage is independent of the quality a-min gate.
func attemptCutBoundaryMeasurementFixture(t *testing.T) (*ReleaseMeasurementArtifact, ed25519.PrivateKey) {
	t.Helper()
	policy := exactPolicy(t)
	policyHash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	artifact := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchema, DeploymentID: "attempt-cut-boundary-test", ChainID: 945,
		GenesisHash: attemptHex32([32]byte{4}), Coordinator: "0x0000000000000000000000000000000000000001",
		SettlementVault: "0x0000000000000000000000000000000000000002", ValidatorID: 1, Netuid: 521,
		SubnetEpoch: 7, NativeSnapshotBlock: 200, NativeSnapshotHash: attemptHex32([32]byte{0x20}),
		EVMSnapshotBlock: 104, EVMSnapshotHash: attemptHex32([32]byte{0x24}), SettlementEpoch: 42,
		PolicyHash: attemptHex32(policyHash), Policy: policy, SelfUID: 7,
		ControlledNOIDs: []uint64{}, Inputs: []ReleaseMeasurementInput{}, Bindings: []ReleaseBindingMeasurement{},
		HeadEMA: []HeadEMAMeasurement{}, Pools: []ReleasePoolMeasurement{}, DepositAudits: []DepositAudit{},
	}
	var validatorKey ed25519.PrivateKey
	for noID := uint64(1); noID <= 2; noID++ {
		fixture := attemptCutBoundaryFixture(t, noID)
		validatorKey = fixture.validatorKey
		boundary := AttemptBoundary{SettlementEpoch: 42, EVMBlock: 102, EVMBlockHash: attemptHex32([32]byte{0x22})}
		cut := attemptCutBoundaryResign(t, fixture.cut, validatorKey, boundary)
		if err := VerifyAttemptLedgerCut(cut, validatorKey.Public().(ed25519.PublicKey), fixture.serverKeyKVs); err != nil {
			t.Fatal(err)
		}
		stats := fixture.stats.currentReleaseStatsMeasurement()
		stats.AttemptCut = cut
		artifact.Inputs = append(artifact.Inputs, ReleaseMeasurementInput{
			NoID: noID, SettlementEpoch: 42, CutNativeBlock: 190, CutNativeBlockHash: attemptHex32([32]byte{0x19}),
			CutEVMSnapshotBlock: boundary.EVMBlock, CutEVMSnapshotHash: boundary.EVMBlockHash, Stats: stats,
		})
		bindings, err := SortedAttemptBindings(cut)
		if err != nil || len(bindings) != 7 {
			t.Fatalf("M8 binding census: %d %v", len(bindings), err)
		}
		for _, binding := range bindings {
			clientKey := attemptHex32(sha256.Sum256(binding.ClientID[:]))
			artifact.Bindings = append(artifact.Bindings, ReleaseBindingMeasurement{
				NoID: noID, ClientID: binding.ClientID.String(), Active: true,
				FleetID: binding.FleetID, Hotkey: binding.Hotkey, Generation: binding.Generation,
				ClientKey: clientKey, LocalClientKey: clientKey, CommitmentHash: clientKey,
				ValidFromEpoch: 42, ValidToEpoch: 42, RecordUID: binding.UID, LiveUIDFound: true, LiveUID: binding.UID,
			})
		}
	}
	statsByNO, err := releaseMeasurementStats(artifact)
	if err != nil {
		t.Fatal(err)
	}
	fleets, _, _, _, err := releaseMeasurementBindings(artifact, statsByNO)
	if err != nil {
		t.Fatal(err)
	}
	for key, score := range releaseRawHeadScores(fleets) {
		raw, err := encodeRationalJSON(score)
		if err != nil {
			t.Fatal(err)
		}
		artifact.HeadEMA = append(artifact.HeadEMA, HeadEMAMeasurement{
			Key: key, HasRaw: true, Raw: raw, Prior: RationalJSON{Numerator: "0", Denominator: "1"}, Next: raw,
		})
	}
	sort.Slice(artifact.HeadEMA, func(i, j int) bool { return artifact.HeadEMA[i].Key.String() < artifact.HeadEMA[j].Key.String() })
	verified, err := VerifyReleaseMeasurementArtifact(artifact)
	if err != nil || len(verified.EligibleHead) != 2 || len(artifact.Bindings) != 14 {
		t.Fatalf("complete signed measurement control: %v", err)
	}
	return artifact, validatorKey
}

// An input cut and decision at one EVM height must name one block hash.
func TestAttemptCutBoundaryMeasurementRejectsSameHeightHash(t *testing.T) {
	artifact, _ := attemptCutBoundaryMeasurementFixture(t)
	artifact.EVMSnapshotBlock = artifact.Inputs[0].CutEVMSnapshotBlock
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil || !strings.Contains(err.Error(), "EVM cut") {
		t.Fatalf("measurement accepted contradictory same-height EVM hashes: %v", err)
	}
}

// Equality is valid when the signed input and decision name the same EVM view.
func TestAttemptCutBoundaryMeasurementAcceptsSameHash(t *testing.T) {
	artifact, _ := attemptCutBoundaryMeasurementFixture(t)
	artifact.EVMSnapshotBlock = artifact.Inputs[0].CutEVMSnapshotBlock
	artifact.EVMSnapshotHash = artifact.Inputs[0].CutEVMSnapshotHash
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err != nil {
		t.Fatal(err)
	}
}

// Frozen input cuts remain reusable at a later finalized decision snapshot.
func TestAttemptCutBoundaryMeasurementAcceptsEarlierHeight(t *testing.T) {
	artifact, _ := attemptCutBoundaryMeasurementFixture(t)
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err != nil {
		t.Fatal(err)
	}
}

// The native side of the same atomic input must not contradict its decision.
func TestAttemptCutBoundaryNativeMeasurementRejectsSameHeightHash(t *testing.T) {
	artifact, _ := attemptCutBoundaryMeasurementFixture(t)
	artifact.NativeSnapshotBlock = artifact.Inputs[0].CutNativeBlock
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil || !strings.Contains(err.Error(), "native cut") {
		t.Fatalf("measurement accepted contradictory same-height native hashes: %v", err)
	}
}

// An atomic native input may coincide exactly with the decision snapshot.
func TestAttemptCutBoundaryNativeMeasurementAcceptsSameHash(t *testing.T) {
	artifact, _ := attemptCutBoundaryMeasurementFixture(t)
	artifact.NativeSnapshotBlock = artifact.Inputs[0].CutNativeBlock
	artifact.NativeSnapshotHash = artifact.Inputs[0].CutNativeBlockHash
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err != nil {
		t.Fatal(err)
	}
}

// Older native inputs retain their own authenticated hash at a later decision.
func TestAttemptCutBoundaryNativeMeasurementAcceptsEarlierHeight(t *testing.T) {
	artifact, _ := attemptCutBoundaryMeasurementFixture(t)
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err != nil {
		t.Fatal(err)
	}
}

// Both chain domains retain their original refusal of a future input cut.
func TestAttemptCutBoundaryMeasurementRetainsFutureCutRejections(t *testing.T) {
	original, _ := attemptCutBoundaryMeasurementFixture(t)
	for _, native := range []bool{false, true} {
		artifact := cloneReleaseMeasurementArtifact(t, original)
		if native {
			artifact.NativeSnapshotBlock = artifact.Inputs[0].CutNativeBlock - 1
		} else {
			artifact.EVMSnapshotBlock = artifact.Inputs[0].CutEVMSnapshotBlock - 1
		}
		if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil {
			t.Fatalf("future input cut was accepted, native=%t", native)
		}
	}
}

// A same-native-epoch retry keeps its original EMA base and may retain the
// same records and egress cursor. Only the signed cut snapshot is changed.
func attemptCutBoundaryLineageFixture(t *testing.T, block uint64, blockHash string) ([]byte, *ReleaseMeasurementArtifact) {
	t.Helper()
	previous, key := attemptCutBoundaryMeasurementFixture(t)
	encoded, contentHash, _, err := SealReleaseMeasurementArtifact(previous)
	if err != nil {
		t.Fatal(err)
	}
	current := cloneReleaseMeasurementArtifact(t, previous)
	current.PreviousArtifactHash = contentHash
	for index := range current.Inputs {
		input := &current.Inputs[index]
		boundary := input.Stats.AttemptCut.Boundary
		boundary.EVMBlock, boundary.EVMBlockHash = block, blockHash
		input.Stats.AttemptCut = attemptCutBoundaryResign(t, input.Stats.AttemptCut, key, boundary)
		input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash = block, blockHash
	}
	if _, err := VerifyReleaseMeasurementArtifact(current); err != nil {
		t.Fatalf("lineage mutation damaged independent artifact authentication: %v", err)
	}
	return encoded, current
}

// Two individually valid cumulative cuts cannot equivocate at one height.
func TestAttemptCutBoundaryLineageRejectsSameHeightHash(t *testing.T) {
	previous, current := attemptCutBoundaryLineageFixture(t, 102, attemptHex32([32]byte{0x62}))
	if err := VerifyReleaseMeasurementLineage(previous, current); err == nil || !strings.Contains(err.Error(), "cut") {
		t.Fatalf("lineage accepted contradictory same-height cut hashes: %v", err)
	}
}

// A later cumulative cut cannot move behind its predecessor even when every
// record is older than both snapshots and every signed record is unchanged.
func TestAttemptCutBoundaryLineageRejectsBackwardsCut(t *testing.T) {
	previous, current := attemptCutBoundaryLineageFixture(t, 101, attemptHex32([32]byte{0x61}))
	if err := VerifyReleaseMeasurementLineage(previous, current); err == nil || !strings.Contains(err.Error(), "cut") {
		t.Fatalf("lineage accepted a backwards cut boundary: %v", err)
	}
}

// An unchanged cut and cursor are valid during a retry of one native epoch.
func TestAttemptCutBoundaryLineageAcceptsSameHash(t *testing.T) {
	previous, current := attemptCutBoundaryLineageFixture(t, 102, attemptHex32([32]byte{0x22}))
	if err := VerifyReleaseMeasurementLineage(previous, current); err != nil {
		t.Fatal(err)
	}
}

// A later finalized cut view can retain the exact same complete record range.
func TestAttemptCutBoundaryLineageAcceptsLaterHeight(t *testing.T) {
	previous, current := attemptCutBoundaryLineageFixture(t, 103, attemptHex32([32]byte{0x63}))
	if err := VerifyReleaseMeasurementLineage(previous, current); err != nil {
		t.Fatal(err)
	}
}

// Persists the actual canonical journal-first input while leaving its real
// M8 statistics at generation zero, before interrupted rotation is reconciled.
func attemptCutBoundaryJournalFixture(t *testing.T) (*ReleaseSteerer, ReleaseMeasurementInput, *ReleaseSnapshot) {
	t.Helper()
	fixture := attemptCutBoundaryFixture(t, 1)
	identity := fixture.cut.Identity
	policy := exactPolicy(t)
	policyHash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ReleaseConfig{
		StateDir: t.TempDir(), DeploymentID: identity.DeploymentID, ChainID: identity.ChainID,
		GenesisHash: identity.GenesisHash, Coordinator: "0x0000000000000000000000000000000000000001",
		ValidatorID: identity.ValidatorID, Netuid: identity.Netuid, PolicyHash: attemptHex32(policyHash), Policy: policy,
		Operators: []OperatorConfig{{NoID: 1, StateDir: filepath.Dir(fixture.ledger.path)}},
	}
	stats := fixture.stats.currentReleaseStatsMeasurement()
	stats.AttemptCut = fixture.cut
	input := ReleaseMeasurementInput{
		NoID: 1, SettlementEpoch: 42, CutNativeBlock: 190, CutNativeBlockHash: attemptHex32([32]byte{0x19}),
		CutEVMSnapshotBlock: fixture.cut.Boundary.EVMBlock, CutEVMSnapshotHash: fixture.cut.Boundary.EVMBlockHash,
		EgressGeneration: fixture.stats.egressGeneration, Stats: stats,
	}
	journal := releaseMeasurementInputJournal{
		Schema: releaseMeasurementInputSchema, DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID,
		GenesisHash: cfg.GenesisHash, Coordinator: cfg.Coordinator, ValidatorID: cfg.ValidatorID,
		Netuid: cfg.Netuid, SubnetEpoch: 7, PolicyHash: cfg.PolicyHash, MeasurementInput: input,
	}
	encoded, err := canonicalReleaseMeasurementInputBytes(&journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicStateWrite(releaseMeasurementInputPath(cfg.StateDir, 7, 1), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if fixture.stats.egressGeneration != 0 || fixture.stats.attemptEgressFirstSequence != 1 || len(fixture.stats.egress) != 7 {
		t.Fatal("journal fixture did not stop before the real egress rotation")
	}
	steerer := &ReleaseSteerer{cfg: cfg, contexts: map[uint64]*ReleaseMeasurementContext{1: {NoID: 1, Stats: fixture.stats}}}
	return steerer, input, &ReleaseSnapshot{BlockNumber: 100, BlockHash: [32]byte{1}, Epoch: big.NewInt(42)}
}

// A recovered input cannot be attached to another hash at the same height or
// to an older decision. Refusal must precede destructive egress reconciliation.
func TestAttemptCutBoundaryJournalRejectsDecisionContradictions(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, sameHeight := range []bool{false, true} {
			steerer, input, snapshot := attemptCutBoundaryJournalFixture(t)
			before, err := os.ReadFile(filepath.Join(measurementStateDir(steerer.cfg, 1), "stats.json"))
			if err != nil {
				t.Fatal(err)
			}
			current := *snapshot
			nativeBlock, nativeHash := input.CutNativeBlock, input.CutNativeBlockHash
			if native {
				if sameHeight {
					nativeHash = attemptHex32([32]byte{0x39})
				} else {
					nativeBlock--
				}
			} else if sameHeight {
				current.BlockHash = [32]byte{0x31}
			} else {
				current.BlockNumber--
			}
			got, err := steerer.loadOrDetachReleaseMeasurementInput(1, 7, nativeBlock, nativeHash, &current)
			if err == nil {
				t.Errorf("recovered journal accepted contradictory decision: native=%t same-height=%t", native, sameHeight)
				continue
			}
			after, readErr := os.ReadFile(filepath.Join(measurementStateDir(steerer.cfg, 1), "stats.json"))
			stats := steerer.contexts[1].Stats
			if readErr != nil || got.NoID != 0 || got.Stats.AttemptCut != nil || !bytes.Equal(before, after) || stats.egressGeneration != 0 || stats.attemptEgressFirstSequence != 1 || len(stats.egress) != 7 {
				t.Fatalf("journal refusal rotated or published input: native=%t same-height=%t read=%v", native, sameHeight, readErr)
			}
			if _, err := steerer.loadOrDetachReleaseMeasurementInput(1, 7, input.CutNativeBlock, input.CutNativeBlockHash, snapshot); err != nil || stats.egressGeneration != 1 || stats.attemptEgressFirstSequence != 9 {
				t.Fatalf("journal refusal poisoned a valid retry: %v", err)
			}
		}
	}
}

// Exact and later snapshots both complete the same journal-first transaction
// once, retaining its original hashes and every recorded provider exposure.
func TestAttemptCutBoundaryJournalAcceptsSameAndLaterDecisions(t *testing.T) {
	for _, later := range []bool{false, true} {
		steerer, input, snapshot := attemptCutBoundaryJournalFixture(t)
		nativeBlock, nativeHash := input.CutNativeBlock, input.CutNativeBlockHash
		if later {
			nativeBlock++
			nativeHash = attemptHex32([32]byte{0x29})
			snapshot.BlockNumber++
			snapshot.BlockHash = [32]byte{0x11}
		}
		for replay := 0; replay < 2; replay++ {
			got, err := steerer.loadOrDetachReleaseMeasurementInput(1, 7, nativeBlock, nativeHash, snapshot)
			stats := steerer.contexts[1].Stats
			if err != nil || got.CutNativeBlockHash != input.CutNativeBlockHash || got.CutEVMSnapshotHash != input.CutEVMSnapshotHash || got.Stats.AttemptCut == nil || got.Stats.AttemptCut.RecordCount != 8 || got.EgressGeneration != 0 || stats.egressGeneration != 1 || stats.attemptEgressFirstSequence != 9 {
				t.Fatalf("valid journal retry changed its cut: later=%t replay=%d error=%v", later, replay, err)
			}
		}
	}
}

// The in-memory same-epoch path must enforce the same decision relation as
// durable recovery. Populate it through real reconciliation before changing
// the caller snapshot, so neither path nor its lifecycle is fabricated.
func TestAttemptCutBoundaryCachedInputRejectsDecisionContradictions(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, sameHeight := range []bool{false, true} {
			steerer, input, snapshot := attemptCutBoundaryJournalFixture(t)
			if _, err := steerer.takeHeadEvidence(7, input.CutNativeBlock, input.CutNativeBlockHash, snapshot); err != nil {
				t.Fatal(err)
			}
			current := *snapshot
			nativeBlock, nativeHash := input.CutNativeBlock, input.CutNativeBlockHash
			if native {
				if sameHeight {
					nativeHash = attemptHex32([32]byte{0x39})
				} else {
					nativeBlock--
				}
			} else if sameHeight {
				current.BlockHash = [32]byte{0x31}
			} else {
				current.BlockNumber--
			}
			got, err := steerer.takeHeadEvidence(7, nativeBlock, nativeHash, &current)
			if err == nil {
				t.Errorf("cached input accepted contradictory decision: native=%t same-height=%t", native, sameHeight)
				continue
			}
			stats := steerer.contexts[1].Stats
			if got != nil || stats.egressGeneration != 1 || stats.attemptEgressFirstSequence != 9 {
				t.Fatal("cached-input refusal changed the completed egress transaction")
			}
			if _, err := steerer.takeHeadEvidence(7, input.CutNativeBlock, input.CutNativeBlockHash, snapshot); err != nil {
				t.Fatalf("cached-input refusal poisoned a valid retry: %v", err)
			}
		}
	}
}

// Cached same-epoch inputs retain their immutable signed cut when a valid
// caller reuses the same view or advances both finalized chain snapshots.
func TestAttemptCutBoundaryCachedInputAcceptsSameAndLaterDecisions(t *testing.T) {
	for _, later := range []bool{false, true} {
		steerer, input, snapshot := attemptCutBoundaryJournalFixture(t)
		if _, err := steerer.takeHeadEvidence(7, input.CutNativeBlock, input.CutNativeBlockHash, snapshot); err != nil {
			t.Fatal(err)
		}
		nativeBlock, nativeHash := input.CutNativeBlock, input.CutNativeBlockHash
		if later {
			nativeBlock++
			nativeHash = attemptHex32([32]byte{0x29})
			snapshot.BlockNumber++
			snapshot.BlockHash = [32]byte{0x11}
		}
		got, err := steerer.takeHeadEvidence(7, nativeBlock, nativeHash, snapshot)
		stats := steerer.contexts[1].Stats
		if err != nil || len(got) != 1 || got[1].CutNativeBlockHash != input.CutNativeBlockHash || got[1].CutEVMSnapshotHash != input.CutEVMSnapshotHash || got[1].Stats.AttemptCut == nil || got[1].Stats.AttemptCut.RecordCount != 8 || stats.egressGeneration != 1 || stats.attemptEgressFirstSequence != 9 {
			t.Fatalf("valid cached retry changed its cut: later=%t error=%v", later, err)
		}
	}
}

package validator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urnetwork/connect/v2026"
	"golang.org/x/crypto/blake2b"
)

func testIntentHotkey(t *testing.T) *crv4.Keypair {
	t.Helper()
	var seed [32]byte
	for index := range seed {
		seed[index] = byte(index + 17)
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return hotkey
}

func testPreparedSubmission(t *testing.T, epoch uint64, uids []uint16) *crv4.PreparedSubmission {
	t.Helper()
	hotkey := testIntentHotkey(t).PublicKey()
	values := []uint16{32768, 65535}
	payload, err := (&crv4.Payload{Hotkey: hotkey, Uids: uids, Values: values, VersionKey: 1}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := []byte{0xde, 0xad, 0xbe, 0xef}
	body := append([]byte{0x84, 0xaa}, ciphertext...)
	raw := append([]byte{byte(len(body) << 2)}, body...)
	txHash := blake2b.Sum256(raw)
	cipherHash := sha256.Sum256(ciphertext)
	return &crv4.PreparedSubmission{
		Schema: crv4.PreparedSubmissionSchema, Netuid: 7, HotkeyHex: "0x" + hex.EncodeToString(hotkey[:]),
		VersionKey: 1, CommitRevealVersion: 4, AccountNonce: 3,
		PreparedAtBlock: 100, PreparedAtBlockHash: types.NewHash([]byte{1}).Hex(), SubnetEpoch: epoch,
		RevealRound: 8, RevealBlock: 120, UIDs: append([]uint16(nil), uids...), Values: values,
		PayloadHex: codec.HexEncodeToString(payload), CiphertextHex: codec.HexEncodeToString(ciphertext),
		CiphertextSHA256: "0x" + hex.EncodeToString(cipherHash[:]),
		ExtrinsicHex:     codec.HexEncodeToString(raw), ExtrinsicHash: types.Hash(txHash).Hex(),
	}
}

func testSteeringIntent(t *testing.T, stateDir string, epoch uint64, previousArtifactHash string) SteeringIntent {
	t.Helper()
	policy := exactPolicy(t)
	policyHash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	firstClientID := connect.Id{1}
	secondClientID := connect.Id{2}
	statsConfig := ReleaseStatsConfig{AMin: policy.Verify.ReliabilityAMin, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000}
	provider := func(clientID connect.Id, quality uint32) ReleaseProviderMeasurement {
		return ReleaseProviderMeasurement{ClientID: clientID.String(), LatencyBuckets: make([]uint64, statsLatencyBuckets), HasPriorQuality: true, PriorQualityPPM: quality}
	}
	zeroHash := releaseHex32([32]byte{})
	audits := []DepositAudit{releaseMeasurementDepositAudit(t, policy, 1), releaseMeasurementDepositAudit(t, policy, 2)}
	artifact := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchema, DeploymentID: "intent-test", ChainID: 945,
		GenesisHash: releaseHex32([32]byte{1}), Coordinator: "0x0000000000000000000000000000000000000001",
		SettlementVault: "0x0000000000000000000000000000000000000002", ValidatorID: 1, Netuid: 7,
		SubnetEpoch: epoch, NativeSnapshotBlock: 100, NativeSnapshotHash: releaseHex32([32]byte{2}),
		EVMSnapshotBlock: 98, EVMSnapshotHash: releaseHex32([32]byte{3}), SettlementEpoch: 4,
		PolicyHash: fmt.Sprintf("0x%x", policyHash), Policy: policy, PreviousArtifactHash: previousArtifactHash,
		ControlledNOIDs: []uint64{}, HeadEMA: []HeadEMAMeasurement{},
		Inputs: []ReleaseMeasurementInput{
			{NoID: 1, SettlementEpoch: 4, CutNativeBlock: 90, CutNativeBlockHash: releaseHex32([32]byte{4}), CutEVMSnapshotBlock: 90, CutEVMSnapshotHash: releaseHex32([32]byte{5}), Stats: ReleaseStatsMeasurement{Config: statsConfig, Providers: []ReleaseProviderMeasurement{provider(firstClientID, 800_000)}}},
			{NoID: 2, SettlementEpoch: 4, CutNativeBlock: 90, CutNativeBlockHash: releaseHex32([32]byte{4}), CutEVMSnapshotBlock: 90, CutEVMSnapshotHash: releaseHex32([32]byte{5}), Stats: ReleaseStatsMeasurement{Config: statsConfig, Providers: []ReleaseProviderMeasurement{provider(secondClientID, 900_000)}}},
		},
		Bindings: []ReleaseBindingMeasurement{
			{NoID: 1, ClientID: firstClientID.String(), FleetID: zeroHash, Hotkey: zeroHash, ClientKey: zeroHash, LocalClientKey: zeroHash, CommitmentHash: zeroHash},
			{NoID: 2, ClientID: secondClientID.String(), FleetID: zeroHash, Hotkey: zeroHash, ClientKey: zeroHash, LocalClientKey: zeroHash, CommitmentHash: zeroHash},
		},
		Pools: []ReleasePoolMeasurement{
			{NoID: 1, UID: 2, PoolHotkey: releaseHex32([32]byte{11})},
			{NoID: 2, UID: 9, PoolHotkey: releaseHex32([32]byte{12})},
		},
		DepositAudits: audits, SelfUID: 5,
	}
	attachReleaseMeasurementAttemptCuts(t, artifact)
	encoded, contentHash, verified, err := SealReleaseMeasurementArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifactPath, artifactSize, err := persistReleaseMeasurementArtifact(stateDir, encoded, contentHash)
	if err != nil {
		t.Fatal(err)
	}
	scores, err := rationalJSON(verified.Scores)
	if err != nil {
		t.Fatal(err)
	}
	intent := SteeringIntent{
		ValidatorID:             1,
		Netuid:                  7,
		SubnetEpoch:             epoch,
		NativeSnapshotBlock:     100,
		NativeSnapshotHash:      artifact.NativeSnapshotHash,
		EVMSnapshotBlock:        98,
		EVMSnapshotHash:         artifact.EVMSnapshotHash,
		SettlementEpoch:         4,
		PolicyHash:              artifact.PolicyHash,
		MeasurementArtifactPath: artifactPath,
		MeasurementArtifactHash: contentHash,
		MeasurementArtifactSize: artifactSize,
		SelfUID:                 5,
		MaskedUIDs:              verified.MaskedUIDs,
		DepositAudits:           audits,
		UIDs:                    verified.UIDs,
		Scores:                  scores,
	}
	intent.Prepared = testPreparedSubmission(t, epoch, intent.UIDs)
	envelopeBytes, envelopeHash, _, err := SealReleaseMeasurementEnvelope(encoded, intent.SelfUID, testIntentHotkey(t), strings.ToLower(intent.Prepared.ExtrinsicHash), time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeSize, err = persistReleaseMeasurementEnvelope(stateDir, envelopeBytes, envelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	intent.MeasurementEnvelopeHash = envelopeHash
	return intent
}

func TestIntentStoreDurableLifecycleAndRestartGuard(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := store.Begin(testSteeringIntent(t, dir, 3, ""))
	if err != nil {
		t.Fatal(err)
	}
	if intent.VectorHash == "" || intent.Status != "pending" {
		t.Fatalf("intent = %+v", intent)
	}

	// A fresh process sees the pending write and cannot double-submit it.
	restarted, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Begin(testSteeringIntent(t, dir, 3, "")); !errors.Is(err, ErrSteeringIntentPending) {
		t.Fatalf("pending restart guard = %v", err)
	}
	if err := restarted.MarkFinalized(intent.VectorHash, intent.Prepared.ExtrinsicHash, 105, "0xfinal", intent.Prepared.RevealBlock, intent.Prepared.Values); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Begin(testSteeringIntent(t, dir, 3, "")); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("finalized restart guard = %v", err)
	}
	if err := restarted.MarkApplied(intent.VectorHash, 121, "0xapplied"); err != nil {
		t.Fatal(err)
	}
	current, err := restarted.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "applied" || current.ApplicationBlock != 121 || current.SelfUID != 5 || len(current.MaskedUIDs) != 1 {
		t.Fatalf("current = %+v", current)
	}

	// The next native epoch archives the complete prior record.
	next, err := restarted.Begin(testSteeringIntent(t, dir, 4, current.MeasurementArtifactHash))
	if err != nil || next.SubnetEpoch != 4 {
		t.Fatalf("next = %+v, %v", next, err)
	}
}

// Intent persistence must reject a claimed top set that does not follow the
// recorded score order, and the immutable vector hash must bind every eligible
// score used to reconstruct that boundary.
func TestIntentStoreBindsAndValidatesCompleteHeadSelectionEvidence(t *testing.T) {
	canonicalDir := filepath.Join(t.TempDir(), "canonical")
	canonical := testSteeringIntent(t, canonicalDir, 3, "")
	store, err := NewIntentStore(canonicalDir)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Begin(canonical)
	if err != nil {
		t.Fatal(err)
	}
	created.EligibleHeadUIDs = []uint16{10}
	if err := created.VerifyVectorHash(); err == nil {
		t.Fatal("eligible head mutation preserved the intent hash")
	}
	substitutedDir := filepath.Join(t.TempDir(), "substituted")
	substituted := testSteeringIntent(t, substitutedDir, 3, "")
	substituted.EligibleHeadUIDs = []uint16{10, 11, 12}
	substituted.EligibleHeadScores = []RationalJSON{{Numerator: "3", Denominator: "1"}, {Numerator: "2", Denominator: "1"}, {Numerator: "1", Denominator: "1"}}
	substituted.SelectedHeadUIDs = []uint16{10, 11}
	substituted.RejectedHeadUIDs = []uint16{12}
	store, err = NewIntentStore(substitutedDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin(substituted); err == nil {
		t.Fatal("substituted selected set passed unchanged score evidence")
	}
	partialDir := filepath.Join(t.TempDir(), "partial")
	partial := testSteeringIntent(t, partialDir, 3, "")
	partial.EligibleHeadUIDs = []uint16{10}
	partial.EligibleHeadScores = nil
	store, err = NewIntentStore(partialDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin(partial); err == nil {
		t.Fatal("head selection without eligible scores was accepted")
	}
}

func TestIntentStoreRejectsRelativeState(t *testing.T) {
	if _, err := NewIntentStore("relative"); err == nil {
		t.Fatal("relative state accepted")
	}
}

func TestSteeringIntentHashBindsProvenSelfMask(t *testing.T) {
	dir := t.TempDir()
	a := testSteeringIntent(t, dir, 3, "")
	b := a
	b.SelfUID = 6
	b.MaskedUIDs = []uint16{6, 7}
	ha, err := a.computeVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	hb, err := b.computeVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	if ha == hb {
		t.Fatal("steering intent hash did not bind the validator self-mask")
	}
}

func TestSteeringIntentVectorHashCommitsDepositAuditEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Begin(testSteeringIntent(t, dir, 3, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := created.VerifyVectorHash(); err != nil {
		t.Fatal(err)
	}
	created.DepositAudits[0].ObservedDepositRao = "2"
	if err := created.VerifyVectorHash(); err == nil {
		t.Fatal("deposit-audit mutation retained the original steering vector hash")
	}
}

func TestIntentRejectsPreparedAndReceiptMismatch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := testSteeringIntent(t, dir, 3, "")
	mismatch.Prepared.Netuid++
	if _, err := store.Begin(mismatch); err == nil {
		t.Fatal("intent accepted a prepared submission for another netuid")
	}

	intent, err := store.Begin(testSteeringIntent(t, dir, 3, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFinalized(intent.VectorHash, "0xwrong", 105, "0xfinal", intent.Prepared.RevealBlock, intent.Prepared.Values); err == nil {
		t.Fatal("intent accepted a mismatched finalized receipt")
	}
	current, err := store.Current()
	if err != nil || current.Status != "pending" {
		t.Fatalf("failed receipt mutated pending intent: %+v, %v", current, err)
	}
}

// A failed same-epoch transaction may be rebuilt, but it must retain the
// original pre-epoch EMA base. Unfinished or successful intents cannot be
// displaced, and a skipped epoch cannot silently reset lineage.
func TestIntentStoreAllowsFailedSameEpochSuccessor(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Begin(testSteeringIntent(t, dir, 3, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFailed(first.VectorHash, errors.New("submission rejected")); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFinalized(first.VectorHash, first.Prepared.ExtrinsicHash, 105, "0xfinal", first.Prepared.RevealBlock, first.Prepared.Values); err == nil {
		t.Fatal("failed intent was later finalized")
	}
	next, err := store.Begin(testSteeringIntent(t, dir, 3, first.MeasurementArtifactHash))
	if err != nil || next.SubnetEpoch != first.SubnetEpoch || next.VectorHash == first.VectorHash {
		t.Fatalf("same-epoch successor = %+v, %v", next, err)
	}
}

func TestIntentStoreRejectsAdvanceFromUnfinishedIntent(t *testing.T) {
	for _, status := range []string{"pending", "finalized"} {
		dir := filepath.Join(t.TempDir(), "state")
		store, err := NewIntentStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		first, err := store.Begin(testSteeringIntent(t, dir, 3, ""))
		if err != nil {
			t.Fatal(err)
		}
		if status == "finalized" {
			if err := store.MarkFinalized(first.VectorHash, first.Prepared.ExtrinsicHash, 105, "0xfinal", first.Prepared.RevealBlock, first.Prepared.Values); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.Begin(testSteeringIntent(t, dir, 4, first.MeasurementArtifactHash)); err == nil {
			t.Fatalf("%s intent was displaced by a later epoch", status)
		}
	}
}

func TestIntentStoreRejectsSkippedEpochSuccessor(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Begin(testSteeringIntent(t, dir, 3, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFinalized(first.VectorHash, first.Prepared.ExtrinsicHash, 105, "0xfinal", first.Prepared.RevealBlock, first.Prepared.Values); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkApplied(first.VectorHash, 121, "0xapplied"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin(testSteeringIntent(t, dir, 5, first.MeasurementArtifactHash)); err == nil {
		t.Fatal("skipped native epoch was accepted")
	}
}

func authenticatedIntentHistoryFixture(t *testing.T) (*IntentStore, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Begin(testSteeringIntent(t, dir, 3, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFinalized(first.VectorHash, first.Prepared.ExtrinsicHash, 105, releaseHex32([32]byte{31}), first.Prepared.RevealBlock, first.Prepared.Values); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkApplied(first.VectorHash, first.Prepared.RevealBlock, releaseHex32([32]byte{32})); err != nil {
		t.Fatal(err)
	}
	second, err := store.Begin(testSteeringIntent(t, dir, 4, first.MeasurementArtifactHash))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFinalized(second.VectorHash, second.Prepared.ExtrinsicHash, 106, releaseHex32([32]byte{33}), second.Prepared.RevealBlock, second.Prepared.Values); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkApplied(second.VectorHash, second.Prepared.RevealBlock, releaseHex32([32]byte{34})); err != nil {
		t.Fatal(err)
	}
	return store, filepath.Join(dir, "steering-intents.json")
}

func writeIntentHistoryFixture(t *testing.T, path string, state steeringIntentFile) {
	t.Helper()
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestIntentStoreAuthenticatedIntentsReturnsCanonicalLifecycle(t *testing.T) {
	store, _ := authenticatedIntentHistoryFixture(t)
	intents, err := store.AuthenticatedIntents()
	if err != nil || len(intents) != 2 || intents[0].SubnetEpoch != 3 || intents[1].SubnetEpoch != 4 {
		t.Fatalf("authenticated intents=%+v error=%v", intents, err)
	}
}

func TestIntentStoreAuthenticatedIntentsRejectsRemovedPredecessor(t *testing.T) {
	store, path := authenticatedIntentHistoryFixture(t)
	state, err := store.readLocked()
	if err != nil {
		t.Fatal(err)
	}
	state.History = nil
	writeIntentHistoryFixture(t, path, *state)
	if _, err := store.AuthenticatedIntents(); err == nil {
		t.Fatal("intent history with a removed predecessor was accepted")
	}
}

func TestIntentStoreAuthenticatedIntentsRejectsReorderedHistory(t *testing.T) {
	store, path := authenticatedIntentHistoryFixture(t)
	state, err := store.readLocked()
	if err != nil {
		t.Fatal(err)
	}
	old := state.History[0]
	state.History[0], state.Current = *state.Current, &old
	writeIntentHistoryFixture(t, path, *state)
	if _, err := store.AuthenticatedIntents(); err == nil {
		t.Fatal("reordered intent history was accepted")
	}
}

func TestIntentStoreAuthenticatedIntentsRejectsDuplicateHistory(t *testing.T) {
	store, path := authenticatedIntentHistoryFixture(t)
	state, err := store.readLocked()
	if err != nil {
		t.Fatal(err)
	}
	state.History = append(state.History, state.History[0])
	writeIntentHistoryFixture(t, path, *state)
	if _, err := store.AuthenticatedIntents(); err == nil {
		t.Fatal("duplicate intent history was accepted")
	}
}

// The intent store is part of the public evidence chain. Canonical JSON with a
// valid vector hash must still fail if its mutable lifecycle fields describe an
// impossible transition.
func TestIntentStoreRejectsImpossiblePersistedLifecycle(t *testing.T) {
	mutations := map[string]func(*SteeringIntent){
		"unknown status":            func(intent *SteeringIntent) { intent.Status = "unknown" },
		"applied without receipt":   func(intent *SteeringIntent) { intent.Status = "applied" },
		"failed without cause":      func(intent *SteeringIntent) { intent.Status = "failed" },
		"noncanonical created time": func(intent *SteeringIntent) { intent.CreatedAt = "2026-09-03T01:00:00+00:00" },
	}
	for name, mutate := range mutations {
		dir := filepath.Join(t.TempDir(), "state")
		store, err := NewIntentStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Begin(testSteeringIntent(t, dir, 3, "")); err != nil {
			t.Fatal(err)
		}
		encoded, err := os.ReadFile(store.path)
		if err != nil {
			t.Fatal(err)
		}
		var persisted steeringIntentFile
		if err := json.Unmarshal(encoded, &persisted); err != nil {
			t.Fatal(err)
		}
		mutate(persisted.Current)
		encoded, err = json.MarshalIndent(&persisted, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.path, append(encoded, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Current(); err == nil {
			t.Fatalf("%s impossible persisted lifecycle was accepted", name)
		}
	}

	dir := filepath.Join(t.TempDir(), "state")
	store, err := NewIntentStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := store.Begin(testSteeringIntent(t, dir, 3, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFailed(intent.VectorHash, nil); err == nil {
		t.Fatal("nil failure cause was accepted")
	}
}

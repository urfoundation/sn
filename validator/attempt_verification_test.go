package validator

// Counts real complete-cut and ASSIGN authentication at the replay boundary,
// retaining canonical signed trails and the full all-operator transaction.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

// Completes one real M8 trail for each operator and closes them together.
func attemptVerificationClosureFixture(t *testing.T) ([]byte, map[uint64]map[byte]ed25519.PublicKey, map[uint64]ed25519.PrivateKey) {
	t.Helper()
	root := t.TempDir()
	participants := make([]AttemptSettlementParticipant, 0, 2)
	serverKeyKVs := make(map[uint64]map[byte]ed25519.PublicKey)
	validatorKeyKVs := make(map[uint64]ed25519.PrivateKey)
	validatorKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	for noID := uint64(1); noID <= 2; noID++ {
		server, _, clientID := newMockVerifyServer(t, 12)
		server.validatorVpk = validatorKey.Public().(ed25519.PublicKey)
		engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, 8, nil)
		stateDir := t.TempDir()
		if err := stats.AdvanceSettlementEpoch(42, stateDir); err != nil {
			t.Fatal(err)
		}
		ledger, err := NewAttemptLedger(stateDir, AttemptLedgerIdentity{
			DeploymentID: "attempt-verification-test", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}),
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
				return AttemptBoundary{}, nil, errors.New("verification fixture changed its pinned boundary")
			}
			bindings := make([]AttemptBinding, len(clientIDs))
			for index, id := range clientIDs {
				bindings[index] = attemptLedgerTestBinding(id, 1)
			}
			return boundary, bindings, nil
		}
		if _, err := engine.RunTrail(context.Background()); err != nil {
			t.Fatal(err)
		}
		participants = append(participants, AttemptSettlementParticipant{NoID: noID, StateDir: stateDir, Stats: stats})
		serverKeyKVs[noID], validatorKeyKVs[noID] = server.serverPublicKeys(), validatorKey
	}
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), participants); err != nil {
		t.Fatal(err)
	}
	data, err := ReadAttemptSettlementClosure(root, 42)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := DecodeAttemptSettlementClosureWithServerKeys(data, serverKeyKVs)
	if err != nil || len(closure.Transitions) != 2 {
		t.Fatalf("complete two-operator closure: %v", err)
	}
	for _, transition := range closure.Transitions {
		if len(transition.PreFold.AttemptCut.Records) != 8 {
			t.Fatal("closure fixture dropped pending checkpoints")
		}
	}
	return data, serverKeyKVs, validatorKeyKVs
}

// Statistics and external server proof checks must authenticate one cut once,
// not repeat all its hashes, record signatures, lifecycle and cut signature.
func TestAttemptSettlementClosureVerifiesEachCutOnce(t *testing.T) {
	data, serverKeys, _ := attemptVerificationClosureFixture(t)
	cutCount, serverCutCount := 0, 0
	closure, err := decodeAttemptSettlementClosureWithServerKeysAndVerifier(data, serverKeys, func(cut *AttemptLedgerCut, vpk ed25519.PublicKey, keys map[byte]ed25519.PublicKey, requireServerKeys bool) error {
		cutCount++
		if requireServerKeys {
			serverCutCount++
		}
		return verifyAttemptLedgerCut(cut, vpk, keys, requireServerKeys)
	})
	if err != nil {
		t.Fatal(err)
	}
	if cutCount != len(closure.Transitions) || serverCutCount != len(closure.Transitions) {
		t.Fatalf("complete cut passes=%d server-authenticated passes=%d, want exactly %d complete server-authenticated cuts", cutCount, serverCutCount, len(closure.Transitions))
	}
}

// Every retained prefix keeps its record validation, but its server-signed
// assignment tuple is identical to the earlier checkpoint's authenticated one.
func TestAttemptCutVerifiesEachAssignmentOnce(t *testing.T) {
	cut, validatorKey, serverKeys := attemptBoundaryLifecycleFixture(t, 8)
	assignmentCopies := 0
	for _, record := range cut.Records {
		assignmentCopies += len(record.Assignments)
	}
	if assignmentCopies != 35 {
		t.Fatalf("M8 checkpoint fixture retained %d assignment copies, want 35", assignmentCopies)
	}
	verifyCount := 0
	if err := verifyAttemptLedgerCutWithAssignVerifier(cut, validatorKey.Public().(ed25519.PublicKey), serverKeys, true, func(key, message, signature []byte) bool {
		verifyCount++
		return connect.VerifyVerifyMessageSignature(key, message, signature)
	}); err != nil {
		t.Fatal(err)
	}
	if verifyCount != 7 {
		t.Fatalf("authenticated %d ASSIGN signature tuples for seven unique M8 assignments, want 7", verifyCount)
	}
}

// A key-ID alias is not server-key identity. Change its bytes synchronously
// after one genuine signature so the following repeated prefix must reject.
func TestAttemptCutAssignmentVerificationRechecksServerKeys(t *testing.T) {
	cut, validatorKey, serverKeys := attemptBoundaryLifecycleFixture(t, 8)
	keyID := cut.Records[0].Assignments[0].ServerKeyID
	replacement := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	verifyCount := 0
	var secondMessage []byte
	err := verifyAttemptLedgerCutWithAssignVerifier(cut, validatorKey.Public().(ed25519.PublicKey), serverKeys, true, func(key, message, signature []byte) bool {
		verifyCount++
		if verifyCount == 2 {
			secondMessage = bytes.Clone(message)
		}
		ok := connect.VerifyVerifyMessageSignature(key, message, signature)
		if verifyCount == 1 {
			copy(serverKeys[keyID], replacement)
		}
		return ok
	})
	if err == nil || !strings.Contains(err.Error(), "server signature is invalid") {
		t.Fatalf("changed key bytes for a repeated ASSIGN were accepted: %v", err)
	}
	if verifyCount != 2 || !bytes.Equal(secondMessage, cut.Records[0].Assignments[0].AssignMessage) {
		t.Fatal("changed server key skipped authentication of the first repeated prefix")
	}
}

// Statistics-only callers retain their documented validator authentication
// mode and do not unexpectedly require an unavailable server-key history.
func TestAttemptCutValidatorOnlyRetainsKeyMode(t *testing.T) {
	cut, validatorKey, _ := attemptBoundaryLifecycleFixture(t, 8)
	if err := verifyAttemptLedgerCutWithAssignVerifier(cut, validatorKey.Public().(ed25519.PublicKey), nil, false, func([]byte, []byte, []byte) bool {
		t.Fatal("validator-only cut invoked server authentication")
		return false
	}); err != nil {
		t.Fatal(err)
	}
}

// Re-signs validator-owned envelopes after a server-signature mutation, so
// only the independently required server authentication can reject the forgery.
func attemptVerificationResignClosure(t *testing.T, closure *AttemptSettlementClosure, validatorKeys map[uint64]ed25519.PrivateKey) []byte {
	t.Helper()
	batch := make([]AttemptSettlementMember, 0, len(closure.Transitions))
	for _, transition := range closure.Transitions {
		key := validatorKeys[transition.Identity.NoID]
		cut := transition.PreFold.AttemptCut
		previous := cut.PriorRoot
		for index := range cut.Records {
			record := &cut.Records[index]
			record.PreviousHash = previous
			digest, err := attemptRecordHash(record)
			if err != nil {
				t.Fatal(err)
			}
			record.RecordHash = attemptHex32(digest)
			record.Signature = ed25519.Sign(key, attemptRecordSignatureMessage(digest))
			previous = record.RecordHash
		}
		cut.Root = previous
		message, err := attemptCutSignatureMessage(cut)
		if err != nil {
			t.Fatal(err)
		}
		cut.Signature = ed25519.Sign(key, message)
		digest, err := attemptSettlementTransitionDigest(transition)
		if err != nil {
			t.Fatal(err)
		}
		batch = append(batch, AttemptSettlementMember{NoID: transition.Identity.NoID, Digest: attemptHex32(digest)})
	}
	for _, transition := range closure.Transitions {
		transition.Batch = append([]AttemptSettlementMember(nil), batch...)
		message, err := attemptSettlementTransitionMessage(transition)
		if err != nil {
			t.Fatal(err)
		}
		transition.Signature = ed25519.Sign(validatorKeys[transition.Identity.NoID], message)
	}
	data, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

// A valid validator must not be able to launder a forged server assignment
// through the statistics pass and its fresh record/cut/transition signatures.
func TestAttemptSettlementClosureFullVerificationRejectsServerForgery(t *testing.T) {
	data, serverKeys, validatorKeys := attemptVerificationClosureFixture(t)
	closure, err := DecodeAttemptSettlementClosure(data)
	if err != nil {
		t.Fatal(err)
	}
	for index := range closure.Transitions[0].PreFold.AttemptCut.Records {
		closure.Transitions[0].PreFold.AttemptCut.Records[index].Assignments[0].AssignSignature[0] ^= 1
	}
	data = attemptVerificationResignClosure(t, closure, validatorKeys)
	if _, err := DecodeAttemptSettlementClosure(data); err != nil {
		t.Fatalf("server forgery broke validator-only control: %v", err)
	}
	if _, err := DecodeAttemptSettlementClosureWithServerKeys(data, serverKeys); err == nil || !strings.Contains(err.Error(), "server signature is invalid") {
		t.Fatalf("validator-resigned server forgery error = %v", err)
	}
}

// Full verification retains exact operator keys and every prior signed
// boundary; it does not accept a malformed batch after a successful call.
func TestAttemptSettlementClosureFullVerificationRetainsRejections(t *testing.T) {
	data, serverKeys, _ := attemptVerificationClosureFixture(t)
	for _, mutation := range []struct {
		name string
		edit func(*AttemptSettlementClosure)
	}{
		{name: "cut range", edit: func(closure *AttemptSettlementClosure) { closure.Transitions[0].PreFold.AttemptCut.FirstSequence++ }},
		{name: "cut signature", edit: func(closure *AttemptSettlementClosure) { closure.Transitions[0].PreFold.AttemptCut.Signature[0] ^= 1 }},
		{name: "record signature", edit: func(closure *AttemptSettlementClosure) {
			closure.Transitions[0].PreFold.AttemptCut.Records[0].Signature[0] ^= 1
		}},
		{name: "folded quality", edit: func(closure *AttemptSettlementClosure) { closure.Transitions[0].PostFold[0].QualityPPM++ }},
		{name: "transition signature", edit: func(closure *AttemptSettlementClosure) { closure.Transitions[0].Signature[0] ^= 1 }},
		{name: "participant omission", edit: func(closure *AttemptSettlementClosure) { closure.Transitions = closure.Transitions[:1] }},
		{name: "wrong epoch", edit: func(closure *AttemptSettlementClosure) { closure.Epoch++ }},
	} {
		closure, err := DecodeAttemptSettlementClosure(data)
		if err != nil {
			t.Fatal(err)
		}
		mutation.edit(closure)
		tampered, err := json.Marshal(closure)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeAttemptSettlementClosureWithServerKeys(append(tampered, '\n'), serverKeys); err == nil {
			t.Errorf("%s was accepted", mutation.name)
		}
	}
	wrongKeys := map[uint64]map[byte]ed25519.PublicKey{1: serverKeys[2], 2: serverKeys[1]}
	if _, err := DecodeAttemptSettlementClosureWithServerKeys(data, wrongKeys); err == nil {
		t.Fatal("swapped operator server keys were accepted")
	}
	if _, err := DecodeAttemptSettlementClosureWithServerKeys(data, nil); err == nil {
		t.Fatal("absent server-key census was accepted")
	}
	if _, err := DecodeAttemptSettlementClosureWithServerKeys(append(bytes.Clone(data), '{', '}'), serverKeys); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

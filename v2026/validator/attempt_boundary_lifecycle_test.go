package validator

// Real server/validator signatures isolate lifecycle context from hash damage.
// A trail's first pending boundary must survive every extension and terminal.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Produces all real pending checkpoints and the terminal proof through RunTrail.
func attemptBoundaryLifecycleFixture(t *testing.T, depth int) (*AttemptLedgerCut, ed25519.PrivateKey, map[byte]ed25519.PublicKey) {
	t.Helper()
	server, validatorKey, clientID := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientID, depth, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, t.TempDir(), &generation)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	boundary := attemptLedgerTestBoundary()
	boundary.EVMBlock = 102
	boundary.EVMBlockHash = attemptHex32([32]byte{5})
	cut, err := ledger.BuildCut(boundary, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	serverKeys := server.serverPublicKeys()
	if err := VerifyAttemptLedgerCut(cut, validatorKey.Public().(ed25519.PublicKey), serverKeys); err != nil {
		t.Fatalf("real signed lifecycle control: %v", err)
	}
	if len(cut.Records) != depth || cut.Records[1].Disposition != AttemptDispositionPending || cut.Records[depth-1].Disposition != AttemptDispositionComplete {
		t.Fatal("fixture omitted real intermediate pending checkpoints")
	}
	for _, record := range cut.Records {
		if record.Boundary != attemptLedgerTestBoundary() {
			t.Fatal("fixture changed its original pinned boundary")
		}
	}
	return cut, validatorKey, serverKeys
}

// All three drifts remain in the same settlement epoch and below the cut block.
type attemptBoundaryLifecycleDrift struct {
	name   string
	mutate func(*AttemptBoundary)
}

// Separate block/hash controls prevent guarding only half the canonical view.
func attemptBoundaryLifecycleDrifts() []attemptBoundaryLifecycleDrift {
	return []attemptBoundaryLifecycleDrift{
		{name: "block", mutate: func(boundary *AttemptBoundary) { boundary.EVMBlock++ }},
		{name: "hash", mutate: func(boundary *AttemptBoundary) { boundary.EVMBlockHash = attemptHex32([32]byte{9}) }},
		{name: "block and hash", mutate: func(boundary *AttemptBoundary) {
			boundary.EVMBlock++
			boundary.EVMBlockHash = attemptHex32([32]byte{9})
		}},
	}
}

// The terminal matches the changed pending view. Every record/link and the
// outer cut are freshly signed; all individual proof/assignment checks pass.
func attemptBoundaryLifecycleChangedCut(t *testing.T, original *AttemptLedgerCut, key ed25519.PrivateKey, serverKeys map[byte]ed25519.PublicKey, drift attemptBoundaryLifecycleDrift) *AttemptLedgerCut {
	t.Helper()
	cut := cloneAttemptLedgerCut(t, original)
	previousHash := cut.PriorRoot
	for index := range cut.Records {
		record := &cut.Records[index]
		if index > 0 {
			drift.mutate(&record.Boundary)
		}
		record.PreviousHash = previousHash
		digest, err := attemptRecordHash(record)
		if err != nil {
			t.Fatal(err)
		}
		record.RecordHash = attemptHex32(digest)
		record.Signature = ed25519.Sign(key, attemptRecordSignatureMessage(digest))
		if err := VerifyAttemptRecord(record, cut.Identity, key.Public().(ed25519.PublicKey), serverKeys); err != nil {
			t.Fatalf("%s drift broke individual signed record %d: %v", drift.name, index, err)
		}
		if record.Boundary.SettlementEpoch != cut.Boundary.SettlementEpoch || record.Boundary.EVMBlock > cut.Boundary.EVMBlock {
			t.Fatal("drift escaped the ordinary cut geometry")
		}
		previousHash = record.RecordHash
	}
	cut.Root = previousHash
	message, err := attemptCutSignatureMessage(cut)
	if err != nil {
		t.Fatal(err)
	}
	cut.Signature = ed25519.Sign(key, message)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), message, cut.Signature) {
		t.Fatal("changed cut was not authentically re-signed")
	}
	return cut
}

// Reject before disk admission or in-memory head/pending publication; a valid
// retry must still extend the original checkpoint without recovery or rollback.
func TestAttemptBoundaryLifecycleAppendRejectsPendingDrift(t *testing.T) {
	original, key, serverKeys := attemptBoundaryLifecycleFixture(t, 4)
	for _, drift := range attemptBoundaryLifecycleDrifts() {
		changed := attemptBoundaryLifecycleChangedCut(t, original, key, serverKeys, drift)
		ledger, err := NewAttemptLedger(filepath.Join(t.TempDir(), "state"), original.Identity, key)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := ledger.Close(); err != nil {
				t.Error(err)
			}
		})
		if _, err := ledger.Append(original.Records[0]); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(ledger.path)
		if err != nil {
			t.Fatal(err)
		}
		diskCalls := 0
		ledger.appendFn = func(path string, payload []byte) error {
			diskCalls++
			return appendAttemptLedgerFile(path, payload)
		}
		committed, appendErr := ledger.Append(changed.Records[1])
		after, err := os.ReadFile(ledger.path)
		if err != nil {
			t.Fatal(err)
		}
		if appendErr == nil {
			t.Errorf("pending boundary drift reached durable append: %s, disk calls=%d sequence=%d", drift.name, diskCalls, ledger.LastSequence())
			continue
		}
		if committed != nil || diskCalls != 0 || ledger.LastSequence() != 1 || !bytes.Equal(before, after) {
			t.Fatalf("%s rejection changed durable head/disk: %v", drift.name, appendErr)
		}
		for _, record := range original.Records[1:] {
			if _, err := ledger.Append(record); err != nil {
				t.Fatalf("%s rejection poisoned valid retry: %v", drift.name, err)
			}
		}
		cut, err := ledger.BuildCut(original.Boundary, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), serverKeys); err != nil {
			t.Fatalf("%s valid retry did not retain the first boundary: %v", drift.name, err)
		}
	}
}

// An authentic but inconsistent durable history must not reopen or be silently
// rewritten. Canonical framing, all hashes and all individual signatures pass.
func TestAttemptBoundaryLifecycleReopenRejectsPendingDrift(t *testing.T) {
	original, key, serverKeys := attemptBoundaryLifecycleFixture(t, 4)
	for _, drift := range attemptBoundaryLifecycleDrifts() {
		changed := attemptBoundaryLifecycleChangedCut(t, original, key, serverKeys, drift)
		stateDir := filepath.Join(t.TempDir(), "state")
		empty, err := NewAttemptLedger(stateDir, original.Identity, key)
		if err != nil {
			t.Fatal(err)
		}
		if err := empty.Close(); err != nil {
			t.Fatal(err)
		}
		var encoded []byte
		for _, record := range changed.Records {
			line, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			encoded = append(encoded, line...)
			encoded = append(encoded, '\n')
		}
		if err := os.WriteFile(empty.path, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		reopened, reopenErr := NewAttemptLedger(stateDir, original.Identity, key)
		if reopened != nil {
			t.Cleanup(func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			})
		}
		if reopenErr == nil {
			t.Errorf("pending boundary drift survived durable reopen: %s", drift.name)
		} else if reopened != nil {
			t.Fatalf("%s invalid ledger escaped through a nonnil return", drift.name)
		}
		after, err := os.ReadFile(empty.path)
		if err != nil || !bytes.Equal(encoded, after) {
			t.Fatalf("%s malformed durable history was rewritten: %v", drift.name, err)
		}
	}
}

// Public replay must join checkpoint context, not merely validate each signed
// record and allow the later terminal to match an already changed checkpoint.
func TestAttemptBoundaryLifecyclePublicCutRejectsPendingDrift(t *testing.T) {
	original, key, serverKeys := attemptBoundaryLifecycleFixture(t, 5)
	for _, drift := range attemptBoundaryLifecycleDrifts() {
		changed := attemptBoundaryLifecycleChangedCut(t, original, key, serverKeys, drift)
		if err := VerifyAttemptLedgerCut(changed, key.Public().(ed25519.PublicKey), serverKeys); err == nil {
			t.Errorf("pending boundary drift survived public cut replay: %s", drift.name)
		}
	}
}

// Unchanged views retain every M4/M5 checkpoint, ordinary restart, exact public
// replay and conservative partial-trail recovery without duplicate attribution.
func TestAttemptBoundaryLifecycleUnchangedViewSurvivesRecovery(t *testing.T) {
	for _, depth := range []int{4, 5} {
		original, key, serverKeys := attemptBoundaryLifecycleFixture(t, depth)
		stateDir := filepath.Join(t.TempDir(), "state")
		ledger, err := NewAttemptLedger(stateDir, original.Identity, key)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range original.Records {
			if _, err := ledger.Append(record); err != nil {
				t.Fatal(err)
			}
		}
		if err := ledger.Close(); err != nil {
			t.Fatal(err)
		}
		ledger, err = NewAttemptLedger(stateDir, original.Identity, key)
		if err != nil || ledger.LastSequence() != uint64(depth) {
			t.Fatalf("M%d unchanged complete reopen: %v", depth, err)
		}
		t.Cleanup(func() {
			if err := ledger.Close(); err != nil {
				t.Error(err)
			}
		})
		if recovered, err := ledger.RecoverPending(); err != nil || len(recovered) != 0 {
			t.Fatalf("M%d completed trail was recovered again: %v", depth, err)
		}
		cut, err := ledger.BuildCut(original.Boundary, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), serverKeys); err != nil {
			t.Fatal(err)
		}
		partialDir := filepath.Join(t.TempDir(), "state")
		partial, err := NewAttemptLedger(partialDir, original.Identity, key)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range original.Records[:2] {
			if _, err := partial.Append(record); err != nil {
				t.Fatal(err)
			}
		}
		if err := partial.Close(); err != nil {
			t.Fatal(err)
		}
		partial, err = NewAttemptLedger(partialDir, original.Identity, key)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := partial.Close(); err != nil {
				t.Error(err)
			}
		})
		recovered, err := partial.RecoverPending()
		if err != nil || len(recovered) != 1 || partial.LastSequence() != 3 {
			t.Fatalf("M%d partial recovery: %v, count=%d", depth, err, len(recovered))
		}
		if recovered[0].Boundary != original.Records[0].Boundary || recovered[0].Disposition != AttemptDispositionValidatorError || recovered[0].Proof != nil || !attemptAssignmentsEqual(recovered[0].Assignments, original.Records[1].Assignments) {
			t.Fatalf("M%d recovery changed original boundary/exposure", depth)
		}
		if repeated, err := partial.RecoverPending(); err != nil || len(repeated) != 0 || partial.LastSequence() != 3 {
			t.Fatalf("M%d partial recovery duplicated an outcome: %v", depth, err)
		}
		cut, err = partial.BuildCut(original.Boundary, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), serverKeys); err != nil {
			t.Fatalf("M%d recovered public cut: %v", depth, err)
		}
	}
}

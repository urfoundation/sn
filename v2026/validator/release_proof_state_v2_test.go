//go:build linux || darwin

// Actual V2 activation, M8 trails, disk projection and production operator
// admission provide the assertions. Observers never replace replay verdicts.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// The real initializer supplies attached V2 Stats; the root starts with no
// public ProofStore, matching semantic startup's worker handoff boundary.
func newReleaseEvidenceV2ProofStateTestFixture(t *testing.T, trails bool) (*attemptSettlementRuntimeV2TestFixture, *releaseEvidenceV2DiskState, [][]byte) {
	t.Helper()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	disk := &releaseEvidenceV2DiskState{states: map[uint64]*releaseAttemptState{}, participants: append([]AttemptSettlementRuntimeV2Participant(nil), fixture.participants...)}
	want := make([][]byte, len(fixture.participants))
	for index, participant := range fixture.participants {
		disk.states[participant.NoID] = &releaseAttemptState{stats: participant.Stats, ledger: participant.Ledger}
		if trails {
			proof, err := fixture.fixtures[index].engine.RunTrail(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			want[index] = append(encoded, '\n')
		}
	}
	return fixture, disk, want
}

// Both genuine complete proofs become exact per-operator projections. The
// same retained object then supports the real engine's incremental append.
func TestReleaseEvidenceV2ProofStateProjectsRealM8AndContinues(t *testing.T) {
	t.Parallel()
	fixture, disk, want := newReleaseEvidenceV2ProofStateTestFixture(t, true)
	if err := prepareReleaseEvidenceV2ProofStores(t.Context(), disk); err != nil {
		t.Fatal(err)
	}
	for index, participant := range disk.participants {
		store := disk.states[participant.NoID].store
		if store == nil || store.projectionLedger != participant.Ledger || store.projectionProofCount != 1 {
			t.Fatal("complete disk proof owner was not published")
		}
		actual, err := os.ReadFile(filepath.Join(participant.StateDir, "proofs.jsonl"))
		if err != nil || !bytes.Equal(actual, want[index]) {
			t.Fatalf("real M8 projection differs: %v", err)
		}
		fixture.fixtures[index].engine.store = store
		proof, err := fixture.fixtures[index].engine.RunTrail(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(proof)
		if err != nil {
			t.Fatal(err)
		}
		want[index] = append(want[index], append(encoded, '\n')...)
		actual, err = os.ReadFile(filepath.Join(participant.StateDir, "proofs.jsonl"))
		if err != nil || !bytes.Equal(actual, want[index]) || store.projectionProofCount != 2 {
			t.Fatalf("real incremental proof owner did not continue: %v", err)
		}
	}
}

// This is the actual missing startup-to-worker join, not a substitute worker.
// A synchronous identity observer stops before any external API is created.
func TestReleaseEvidenceV2ProofStateConnectsActualOperatorAdmission(t *testing.T) {
	t.Parallel()
	fixture, disk, _ := newReleaseEvidenceV2ProofStateTestFixture(t, false)
	participant := disk.participants[0]
	identity := participant.Ledger.identity
	seedPath := filepath.Join(participant.StateDir, "client.seed")
	if err := os.WriteFile(seedPath, fixture.fixtures[0].key.Seed(), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &ReleaseConfig{DeploymentID: identity.DeploymentID, ChainID: identity.ChainID, GenesisHash: identity.GenesisHash, Netuid: identity.Netuid, ValidatorID: identity.ValidatorID, Policy: fixture.fixtures[0].policy}
	op := OperatorConfig{NoID: participant.NoID, StateDir: participant.StateDir, ClientKeySeedFile: seedPath}
	stop := errors.New("actual operator identity admitted before external runtime")
	admitted := 0
	observe := func([32]byte) error { admitted++; return stop }
	_, err := startReleaseOperatorWithAdmission(t.Context(), cfg, op, fixture.fixtures[0].engine.epochFn, fixture.fixtures[0].engine.resolve, disk.states[participant.NoID], observe)
	if err == nil || !strings.Contains(err.Error(), "prepared attempt state is incomplete") || admitted != 0 {
		t.Fatalf("missing proof-store prerequisite was not the actual worker failure: %v", err)
	}
	if err := prepareReleaseEvidenceV2ProofStores(t.Context(), disk); err != nil {
		t.Fatal(err)
	}
	_, err = startReleaseOperatorWithAdmission(t.Context(), cfg, op, fixture.fixtures[0].engine.epochFn, fixture.fixtures[0].engine.resolve, disk.states[participant.NoID], observe)
	if !errors.Is(err, stop) || admitted != 1 {
		t.Fatalf("prepared disk batch did not reach actual operator identity admission: %v", err)
	}
}

// A conflicting otherwise private projection must not publish either pointer.
// Original signed ledgers and the conflicting bytes remain for investigation.
func TestReleaseEvidenceV2ProofStateRejectsConflictWithoutPartialPublication(t *testing.T) {
	t.Parallel()
	_, disk, _ := newReleaseEvidenceV2ProofStateTestFixture(t, true)
	path := filepath.Join(disk.participants[1].StateDir, "proofs.jsonl")
	conflict := []byte("not the actual signed proof\n")
	if err := os.WriteFile(path, conflict, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareReleaseEvidenceV2ProofStores(t.Context(), disk); err == nil {
		t.Fatal("conflicting derived proof was accepted")
	}
	for _, state := range disk.states {
		if state.store != nil {
			t.Fatal("failed census published a partial ProofStore")
		}
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, conflict) {
		t.Fatalf("failed proof verification replaced conflicting bytes: %v", err)
	}
}

// Completed derived files remain valid retry inputs after late cancellation;
// no canceled first call attaches even the already completed first operator.
func TestReleaseEvidenceV2ProofStateLateCancelPreservesVerifiedRetry(t *testing.T) {
	t.Parallel()
	_, disk, want := newReleaseEvidenceV2ProofStateTestFixture(t, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := prepareReleaseEvidenceV2ProofStoresObserved(ctx, disk, releaseEvidenceV2ProofObserver{beforePublish: func() error { cancel(); return nil }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late cancellation escaped proof publication: %v", err)
	}
	for _, state := range disk.states {
		if state.store != nil {
			t.Fatal("late canceled batch published a ProofStore")
		}
	}
	if err := prepareReleaseEvidenceV2ProofStores(t.Context(), disk); err != nil {
		t.Fatal(err)
	}
	for index, participant := range disk.participants {
		actual, err := os.ReadFile(filepath.Join(participant.StateDir, "proofs.jsonl"))
		if err != nil || !bytes.Equal(actual, want[index]) || disk.states[participant.NoID].store == nil {
			t.Fatalf("verified retry changed immutable proof projection: %v", err)
		}
	}
}

// Bad routing and fresh/unrecovered Stats are refused before constructors.
func TestReleaseEvidenceV2ProofStateRejectsUnreadyOrAliasedCensusBeforeIO(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"missing-member", "repeated-ledger", "wrong-path", "fresh-stats", "already-published"} {
		_, disk, _ := newReleaseEvidenceV2ProofStateTestFixture(t, false)
		first, second := disk.participants[0], disk.participants[1]
		switch fault {
		case "missing-member":
			delete(disk.states, second.NoID)
		case "repeated-ledger":
			disk.participants[1].Ledger = first.Ledger
		case "wrong-path":
			disk.participants[1].StateDir = first.StateDir
		case "fresh-stats":
			fresh := NewStatsEngine(second.Stats.cfg)
			disk.participants[1].Stats, disk.states[second.NoID].stats = fresh, fresh
		case "already-published":
			disk.states[second.NoID].store = &ProofStore{}
		}
		opened := 0
		err := prepareReleaseEvidenceV2ProofStoresObserved(t.Context(), disk, releaseEvidenceV2ProofObserver{opened: func(uint64, *ProofStore) error { opened++; return nil }})
		if err == nil || opened != 0 {
			t.Fatalf("%s invalid startup census reached a proof constructor: %v", fault, err)
		}
	}
}

// A lost worker completion cannot masquerade as an empty successful result.
func TestReleaseEvidenceV2ProofStateJoinsLostWorkerCompletion(t *testing.T) {
	t.Parallel()
	_, disk, _ := newReleaseEvidenceV2ProofStateTestFixture(t, false)
	first := disk.participants[0].NoID
	err := prepareReleaseEvidenceV2ProofStoresObserved(t.Context(), disk, releaseEvidenceV2ProofObserver{opened: func(noID uint64, _ *ProofStore) error {
		if noID == first {
			runtime.Goexit()
		}
		return nil
	}})
	if err == nil || !strings.Contains(err.Error(), "did not complete") {
		t.Fatalf("lost proof worker outcome escaped: %v", err)
	}
	for _, state := range disk.states {
		if state.store != nil {
			t.Fatal("lost worker partially published proof owners")
		}
	}
}

// Callback mutation cannot redirect an already admitted acquisition or make a
// shortened map become the apparent complete publication census.
func TestReleaseEvidenceV2ProofStateRefusesChangedOwnedDestination(t *testing.T) {
	t.Parallel()
	_, disk, _ := newReleaseEvidenceV2ProofStateTestFixture(t, false)
	original := append([]AttemptSettlementRuntimeV2Participant(nil), disk.participants...)
	states := []*releaseAttemptState{disk.states[original[0].NoID], disk.states[original[1].NoID]}
	err := prepareReleaseEvidenceV2ProofStoresObserved(t.Context(), disk, releaseEvidenceV2ProofObserver{beforePublish: func() error {
		delete(disk.states, original[1].NoID)
		disk.participants[0].StateDir = original[1].StateDir
		return nil
	}})
	if err == nil {
		t.Fatal("changed startup destination census was accepted")
	}
	for _, state := range states {
		if state.store != nil {
			t.Fatal("borrowed destination mutation partially published proof owners")
		}
	}
}

// At two or more effective cores both actual constructors reach the barrier
// before either is released. The single-core profile remains explicitly serial.
func TestReleaseEvidenceV2ProofStateRunsIndependentProjectionsConcurrently(t *testing.T) {
	t.Parallel()
	_, disk, _ := newReleaseEvidenceV2ProofStateTestFixture(t, false)
	if runtime.GOMAXPROCS(0) < 2 {
		if err := prepareReleaseEvidenceV2ProofStores(t.Context(), disk); err != nil {
			t.Fatal(err)
		}
		return
	}
	ctx, cancel := context.WithCancel(t.Context())
	entered, release := make(chan uint64, 2), make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	done := make(chan error, 1)
	go func() {
		done <- prepareReleaseEvidenceV2ProofStoresObserved(ctx, disk, releaseEvidenceV2ProofObserver{opened: func(noID uint64, _ *ProofStore) error {
			entered <- noID
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}})
	}()
	finished := false
	defer func() {
		cancel()
		unlock()
		if !finished {
			<-done
		}
	}()
	seen := map[uint64]bool{}
	for len(seen) != 2 {
		select {
		case noID := <-entered:
			if seen[noID] {
				t.Fatal("constructor repeated the same operator")
			}
			seen[noID] = true
		case err := <-done:
			finished = true
			t.Fatalf("proof preparation ended before both constructors overlapped: %v", err)
		}
	}
	unlock()
	err := <-done
	finished = true
	if err != nil {
		t.Fatal(err)
	}
}

// Precancellation has no filesystem or pointer side effects.
func TestReleaseEvidenceV2ProofStatePrecancelHasNoIO(t *testing.T) {
	t.Parallel()
	_, disk, _ := newReleaseEvidenceV2ProofStateTestFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opened := 0
	err := prepareReleaseEvidenceV2ProofStoresObserved(ctx, disk, releaseEvidenceV2ProofObserver{opened: func(uint64, *ProofStore) error { opened++; return nil }})
	if !errors.Is(err, context.Canceled) || opened != 0 {
		t.Fatalf("pre-canceled proof preparation reached I/O: %v", err)
	}
	for _, state := range disk.states {
		if state.store != nil {
			t.Fatal("pre-canceled startup changed proof ownership")
		}
	}
}

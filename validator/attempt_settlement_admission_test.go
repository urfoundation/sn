package validator

// Force the resolver/admission handoff through the real protocol engine before
// any durable checkpoint exists. Only admitted state corruption is fatal.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/urnetwork/connect"
)

// Uses the signed mock server and production ledger; a real settlement advance
// is forced between the first binding lookup and beginAttempt.
func assertAttemptSettlementAdmissionRetry(t *testing.T, advanceDuringResolve bool) {
	t.Helper()
	stateDir, root := t.TempDir(), t.TempDir()
	server, key, clientID := newMockVerifyServer(t, 12)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, key, clientID, 4, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	participants := []AttemptSettlementParticipant{{NoID: 9, StateDir: stateDir, Stats: stats}}
	old := attemptLedgerTestBoundary()
	current := old
	current.SettlementEpoch = 43
	current.EVMBlock++
	first := true
	engine.resolve = func(ctx context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
		boundary := current
		if first {
			first = false
			if advanceDuringResolve {
				boundary = old
				if err := AdvanceAttemptSettlementEpoch(root, 43, old, participants); err != nil {
					return AttemptBoundary{}, nil, err
				}
			}
		}
		if pinned != nil {
			boundary = *pinned
		}
		bindings := make([]AttemptBinding, len(ids))
		for index, id := range ids {
			bindings[index] = attemptLedgerTestBinding(id, generation)
		}
		return boundary, bindings, nil
	}
	proof, err := engine.RunTrail(context.Background())
	var fatal *TrailFatalError
	if err == nil || proof != nil {
		t.Fatal("unadmitted boundary unexpectedly completed")
	}
	if errors.As(err, &fatal) {
		t.Fatalf("unadmitted finalized-boundary handoff killed the trail lifecycle: %v", err)
	}
	if ledger.LastSequence() != 0 || stats.activeAttemptCount != 0 {
		t.Fatal("unadmitted boundary appended or retained ownership")
	}
	server.mu.Lock()
	assigned := len(server.trails)
	server.mu.Unlock()
	if assigned != 0 {
		t.Fatal("retry dropped a server assignment instead of reserving before seed")
	}
	if !advanceDuringResolve {
		if err := AdvanceAttemptSettlementEpoch(root, 43, old, participants); err != nil {
			t.Fatal(err)
		}
	}
	proof, err = engine.RunTrail(context.Background())
	if err != nil || proof == nil || proof.Epoch != 43 {
		t.Fatalf("retry in owned epoch did not complete: proof=%v error=%v", proof, err)
	}
	records, err := ledger.RecordsAfter(0)
	if err != nil || len(records) != 4 {
		t.Fatalf("retry ledger records=%d error=%v", len(records), err)
	}
	for _, record := range records {
		if record.Boundary != current {
			t.Fatal("retry crossed or backdated the signed boundary")
		}
	}
}

// Replaces only the transport boundary while retaining real protocol signing.
type attemptSettlementTestTransport func(context.Context, connect.Id, []byte) ([]byte, error)

// Forwards each bounded request through the explicitly controlled test seam.
func (self attemptSettlementTestTransport) PostVerify(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
	return self(ctx, hop, body)
}

// A delivered server assignment is already owned before either cut can close;
// its pinned trail and every exposure enter the actual terminal export.
func TestAttemptSettlementRunTrailReservesBeforeFirstAssignment(t *testing.T) {
	stateDir, root := t.TempDir(), t.TempDir()
	if err := ensurePrivateStateDir(root); err != nil {
		t.Fatal(err)
	}
	server, key, clientID := newMockVerifyServer(t, 12)
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, key, clientID, 4, store)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	participants := []AttemptSettlementParticipant{{NoID: 9, StateDir: stateDir, Stats: stats}}
	assigned, release := make(chan struct{}), make(chan struct{})
	first := true
	engine.transport = attemptSettlementTestTransport(func(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
		response, err := server.PostVerify(ctx, hop, body)
		if err != nil || !first {
			return response, err
		}
		first = false
		close(assigned)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return response, nil
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	var proof *ProofRecord
	var trailErr error
	go func() { defer close(finished); proof, trailErr = engine.RunTrail(ctx) }()
	defer func() { cancel(); <-finished }()
	select {
	case <-assigned:
	case <-finished:
		t.Fatalf("trail ended before the forced assignment barrier: %v", trailErr)
	}
	stats.mu.Lock()
	active := stats.activeAttemptCount
	stats.mu.Unlock()
	if active != 1 {
		t.Fatalf("first assignment has %d settlement owners, want 1", active)
	}
	if _, err := stats.detachReleaseStatsMeasurementWithAttemptCut(stateDir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { t.Error("ordinary cut passed active seed"); return nil }); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("ordinary cut: %v", err)
	}
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), participants); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("terminal cut: %v", err)
	}
	if _, err := ReadAttemptSettlementClosure(root, 42); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unfinished assigned trail exported: %v", err)
	}
	close(release)
	<-finished
	if trailErr != nil || proof == nil || proof.Epoch != 42 {
		t.Fatalf("reserved completion: proof=%v error=%v", proof, trailErr)
	}
	if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), participants); err != nil {
		t.Fatal(err)
	}
	data, err := ReadAttemptSettlementClosure(root, 42)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := DecodeAttemptSettlementClosure(data)
	if err != nil {
		t.Fatal(err)
	}
	cut := closure.Transitions[0].PreFold.AttemptCut
	if len(cut.Records) != 4 || cut.Records[3].Disposition != AttemptDispositionComplete || len(cut.Records[3].Assignments) != 3 {
		t.Fatalf("terminal export lost assigned work: %+v", cut.Records)
	}
	for _, record := range cut.Records {
		if record.Boundary != attemptLedgerTestBoundary() {
			t.Fatal("reserved trail crossed its epoch boundary")
		}
	}
	var exposures uint64
	for _, provider := range closure.Transitions[0].PreFold.Providers {
		exposures += provider.Assignments
	}
	if exposures != 3 {
		t.Fatalf("closed measured exposure=%d, want 3", exposures)
	}
	proofJSON, err := json.Marshal(cut.Records[3].Proof)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(projection, append(proofJSON, '\n')) {
		t.Fatalf("completed projection differs from terminal authority: %v", err)
	}
	if ledger.LastSequence() != 4 {
		t.Fatal("closure appended synthetic attempt records")
	}
}

// Failures before a response release the reservation without inventing an
// attributable hop; caller cancellation also joins the outstanding request.
func TestAttemptSettlementRunTrailReleasesUnassignedSeedReservation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		stateDir, root := t.TempDir(), t.TempDir()
		server, key, clientID := newMockVerifyServer(t, 12)
		engine, stats, _ := newTestEngine(t, server, key, clientID, 4, nil)
		engine.cfg.ExtendAttempts = 1
		generation := uint64(1)
		ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
		ctx, cancel := context.WithCancel(context.Background())
		entered, finished := make(chan struct{}), make(chan struct{})
		engine.transport = attemptSettlementTestTransport(func(requestCtx context.Context, _ connect.Id, _ []byte) ([]byte, error) {
			close(entered)
			if canceled {
				<-requestCtx.Done()
				return nil, requestCtx.Err()
			}
			return nil, errors.New("seed unavailable before any assignment")
		})
		var trailErr error
		go func() { defer close(finished); _, trailErr = engine.RunTrail(ctx) }()
		select {
		case <-entered:
		case <-finished:
			cancel()
			t.Fatalf("trail ended before seed request: %v", trailErr)
		}
		cancel()
		<-finished
		if trailErr == nil || stats.activeAttemptCount != 0 || ledger.LastSequence() != 0 {
			t.Fatalf("canceled=%t seed ownership leaked: %v", canceled, trailErr)
		}
		var fatal *TrailFatalError
		if errors.As(trailErr, &fatal) {
			t.Fatalf("unassigned seed became fatal: %v", trailErr)
		}
		if err := AdvanceAttemptSettlementEpoch(root, 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{{NoID: 9, StateDir: stateDir, Stats: stats}}); err != nil {
			t.Fatal(err)
		}
	}
}

// An ordinary measurement barrier also rejects admission before the server is
// asked to assign work, then permits the identical lifecycle to resume.
func TestAttemptSettlementRunTrailWaitsBeforeSeedOnOrdinaryCut(t *testing.T) {
	stateDir := t.TempDir()
	server, key, clientID := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, key, clientID, 4, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if err := stats.beginAttempt(42, ledger); err != nil {
		t.Fatal(err)
	}
	if _, err := stats.detachReleaseStatsMeasurementWithAttemptCut(stateDir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { return nil }); !errors.Is(err, errAttemptCutPending) {
		t.Fatal(err)
	}
	if _, err := engine.RunTrail(context.Background()); !errors.Is(err, errAttemptCutPending) {
		t.Fatalf("ordinary cut admission: %v", err)
	}
	server.mu.Lock()
	assigned := len(server.trails)
	server.mu.Unlock()
	if assigned != 0 || ledger.LastSequence() != 0 {
		t.Fatal("ordinary cut discarded an assigned exposure")
	}
	stats.abortAttempt()
	if _, err := stats.detachReleaseStatsMeasurementWithAttemptCut(stateDir, attemptLedgerTestBoundary(), func(ReleaseStatsMeasurement, uint64) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if proof, err := engine.RunTrail(context.Background()); err != nil || proof == nil {
		t.Fatalf("ordinary cut did not reopen: %v", err)
	}
}

// Losing pinned binding authority after receiving an authentic first assignment
// is not a retryable pre-admission event that could silently omit exposure.
func TestAttemptSettlementRunTrailKeepsAssignedBindingFailureFatal(t *testing.T) {
	stateDir := t.TempDir()
	server, key, clientID := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, key, clientID, 4, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	resolve := engine.resolve
	engine.resolve = func(ctx context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
		if len(ids) != 0 {
			if pinned == nil || *pinned != attemptLedgerTestBoundary() {
				t.Fatal("first assignment has no reserved pin")
			}
			return AttemptBoundary{}, nil, errors.New("lost finalized binding authority")
		}
		return resolve(ctx, pinned, ids)
	}
	_, err := engine.RunTrail(context.Background())
	var fatal *TrailFatalError
	if !errors.As(err, &fatal) || stats.activeAttemptCount != 0 || ledger.LastSequence() != 0 {
		t.Fatalf("assigned binding failure was hidden: %v", err)
	}
	server.mu.Lock()
	assigned := len(server.trails)
	server.mu.Unlock()
	if assigned != 1 {
		t.Fatal("binding-failure control did not receive a real assignment")
	}
}

func TestAttemptSettlementRunTrailRetriesAfterConcurrentAdvance(t *testing.T) {
	assertAttemptSettlementAdmissionRetry(t, true)
}

func TestAttemptSettlementRunTrailRetriesBeforePollingAdvance(t *testing.T) {
	assertAttemptSettlementAdmissionRetry(t, false)
}

// A future skipped epoch and a detached ledger remain genuine local corruption.
func TestAttemptSettlementRunTrailKeepsSkippedEpochAndLedgerCorruptionFatal(t *testing.T) {
	for _, corruptLedger := range []bool{false, true} {
		stateDir := t.TempDir()
		server, key, clientID := newMockVerifyServer(t, 12)
		store, err := NewProofStore(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		engine, stats, _ := newTestEngine(t, server, key, clientID, 4, store)
		generation := uint64(1)
		ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
		if corruptLedger {
			stats.attemptLedger = nil
		} else {
			engine.resolve = func(_ context.Context, _ *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
				boundary := attemptLedgerTestBoundary()
				boundary.SettlementEpoch = 44
				bindings := make([]AttemptBinding, len(ids))
				for index, id := range ids {
					bindings[index] = attemptLedgerTestBinding(id, generation)
				}
				return boundary, bindings, nil
			}
		}
		_, err = engine.RunTrail(context.Background())
		var fatal *TrailFatalError
		if !errors.As(err, &fatal) || ledger.LastSequence() != 0 || stats.activeAttemptCount != 0 {
			t.Fatalf("corruptLedger=%t durable failure lost fatal/no-write boundary: %v", corruptLedger, err)
		}
	}
}

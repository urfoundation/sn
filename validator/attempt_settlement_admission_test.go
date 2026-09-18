package validator

// Force the resolver/admission handoff through the real protocol engine before
// any durable checkpoint exists. Only admitted state corruption is fatal.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/stabi"
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

// Exposes the exact production cache seam without invoking unrelated snapshot
// or hotkey reads in the binding-error provenance tests.
type attemptSettlementBindingRpc struct {
	binding      stabi.BindingAtOutput
	bindingError error
	bindingCalls int
}

// Refuses an unrelated snapshot read so each test fails at the wrong seam.
func (self *attemptSettlementBindingRpc) Snapshot(context.Context) (AttemptBoundary, error) {
	return AttemptBoundary{}, errors.New("unexpected attempt snapshot")
}

// Refuses an unrelated boundary validation so each test fails at the wrong seam.
func (self *attemptSettlementBindingRpc) Validate(context.Context, AttemptBoundary) error {
	return errors.New("unexpected attempt boundary validation")
}

// Refuses an unrelated hotkey scan so each test fails at the wrong seam.
func (self *attemptSettlementBindingRpc) Hotkeys(context.Context, AttemptBoundary) (map[[32]byte]uint16, error) {
	return nil, errors.New("unexpected attempt hotkey scan")
}

// Returns the controlled chain result and records the isolated binding read.
func (self *attemptSettlementBindingRpc) Binding(context.Context, AttemptBoundary, connect.Id) (stabi.BindingAtOutput, error) {
	self.bindingCalls++
	return self.binding, self.bindingError
}

// Only the chain read gets retry provenance. Decoded binding inconsistency and
// pinned-block conflicts remain hard even though they share the same resolver.
func TestAttemptBoundaryCacheTypesOnlyBindingRpcReadFailures(t *testing.T) {
	boundary := attemptLedgerTestBoundary()
	clientId := connect.NewId()
	newResolver := func(rpc *attemptSettlementBindingRpc) *cachedAttemptBoundaryResolver {
		resolver := newCachedAttemptBoundaryResolver(rpc)
		resolver.blocks[boundary.EVMBlock] = &attemptBoundaryBlock{
			boundary: boundary, hotkeys: map[[32]byte]uint16{}, bindings: map[connect.Id]AttemptBinding{},
		}
		return resolver
	}
	for _, test := range []struct {
		name          string
		rpc           *attemptSettlementBindingRpc
		boundary      AttemptBoundary
		wantTyped     bool
		wantRetryable bool
		wantCalls     int
	}{
		{name: "rpc timeout", rpc: &attemptSettlementBindingRpc{bindingError: context.DeadlineExceeded}, boundary: boundary, wantTyped: true, wantRetryable: true, wantCalls: 1},
		{name: "rpc authorization", rpc: &attemptSettlementBindingRpc{bindingError: errors.New("binding authorization denied")}, boundary: boundary, wantTyped: true, wantCalls: 1},
		{name: "decoded binding mismatch", rpc: &attemptSettlementBindingRpc{binding: stabi.BindingAtOutput{Active: true}}, boundary: boundary, wantCalls: 1},
		{name: "pinned block conflict", rpc: &attemptSettlementBindingRpc{}, boundary: func() AttemptBoundary {
			conflict := boundary
			conflict.EVMBlockHash = attemptHex32([32]byte{9})
			return conflict
		}()},
	} {
		resolver := newResolver(test.rpc)
		_, _, err := resolver.Resolve(context.Background(), &test.boundary, []connect.Id{clientId})
		var readErr *attemptBindingReadError
		if err == nil || errors.As(err, &readErr) != test.wantTyped || retryableAttemptBindingReadError(err) != test.wantRetryable || test.rpc.bindingCalls != test.wantCalls {
			t.Fatalf("%s classification typed/retryable/calls=%t/%t/%d, want %t/%t/%d: %v", test.name, readErr != nil, retryableAttemptBindingReadError(err), test.rpc.bindingCalls, test.wantTyped, test.wantRetryable, test.wantCalls, err)
		}
		resolver.close()
	}
}

// A timeout marker is necessary but not sufficient: every joined leaf must be
// a recognized transport failure, and owner cancellation never starts a retry.
func TestAttemptBindingReadRetryRequiresEveryJoinedCauseTransient(t *testing.T) {
	hard := errors.New("binding integrity mismatch")
	for _, test := range []struct {
		name      string
		err       error
		retryable bool
	}{
		{name: "typed timeout", err: &attemptBindingReadError{cause: context.DeadlineExceeded}, retryable: true},
		{name: "typed joined transport", err: &attemptBindingReadError{cause: errors.Join(context.DeadlineExceeded, io.ErrUnexpectedEOF)}, retryable: true},
		{name: "wrapped url transport", err: &attemptBindingReadError{cause: &url.Error{Op: "read", URL: "https://rpc.example", Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}}}, retryable: true},
		{name: "wrapped geth http capacity", err: &attemptBindingReadError{cause: fmt.Errorf("binding rpc response: %w", gethrpc.HTTPError{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable"})}, retryable: true},
		{name: "wrapped geth http contract", err: &attemptBindingReadError{cause: fmt.Errorf("binding rpc response: %w", gethrpc.HTTPError{StatusCode: http.StatusConflict, Status: "409 Conflict"})}},
		{name: "untyped timeout", err: context.DeadlineExceeded},
		{name: "owner cancellation", err: &attemptBindingReadError{cause: context.Canceled}},
		{name: "typed integrity", err: &attemptBindingReadError{cause: hard}},
		{name: "typed mixed tree", err: &attemptBindingReadError{cause: errors.Join(context.DeadlineExceeded, hard)}},
		{name: "outer mixed tree", err: errors.Join(&attemptBindingReadError{cause: context.DeadlineExceeded}, hard)},
	} {
		if retryable := retryableAttemptBindingReadError(test.err); retryable != test.retryable {
			t.Fatalf("%s retryable=%t, want %t: %v", test.name, retryable, test.retryable, test.err)
		}
	}
	if !onlyAttemptContextError(fmt.Errorf("resolver: %w", &attemptBindingReadError{cause: errors.Join(context.Canceled, fmt.Errorf("read: %w", context.Canceled))}), context.Canceled) {
		t.Fatal("pure joined owner cancellation was not recognized")
	}
	if onlyAttemptContextError(&attemptBindingReadError{cause: errors.Join(context.Canceled, hard)}, context.Canceled) {
		t.Fatal("owner cancellation hid an independent integrity branch")
	}
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

// The authenticated assignment and reserved boundary stay in memory while only
// the failed binding read repeats. This covers both capture sites without
// replaying seed, extend, or a durable pending checkpoint.
func TestAttemptSettlementRunTrailRetriesTransientAssignedBindingRead(t *testing.T) {
	for _, test := range []struct {
		name                 string
		failedBindingCall    int
		transportCallsAtWait int
		sequencesAtWait      uint64
	}{
		{name: "first assignment", failedBindingCall: 1, transportCallsAtWait: 1},
		{name: "later assignment", failedBindingCall: 2, transportCallsAtWait: 2, sequencesAtWait: 1},
	} {
		stateDir := t.TempDir()
		server, key, clientId := newMockVerifyServer(t, 12)
		engine, stats, _ := newTestEngine(t, server, key, clientId, 4, nil)
		transportCalls := 0
		engine.transport = attemptSettlementTestTransport(func(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
			transportCalls++
			return server.PostVerify(ctx, hop, body)
		})
		generation := uint64(1)
		ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
		resolve := engine.resolve
		bindingCalls := 0
		var retryHop connect.Id
		engine.resolve = func(ctx context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			if len(ids) == 0 {
				return resolve(ctx, pinned, ids)
			}
			bindingCalls++
			if pinned == nil || *pinned != attemptLedgerTestBoundary() || len(ids) != 1 {
				t.Fatalf("%s binding call %d changed pin or coverage: pin=%v ids=%v", test.name, bindingCalls, pinned, ids)
			}
			if bindingCalls == test.failedBindingCall {
				retryHop = ids[0]
				return AttemptBoundary{}, nil, &attemptBindingReadError{cause: errors.Join(context.DeadlineExceeded, io.ErrUnexpectedEOF)}
			}
			if bindingCalls == test.failedBindingCall+1 && ids[0] != retryHop {
				t.Fatalf("%s binding retry changed assigned hop from %s to %s", test.name, retryHop, ids[0])
			}
			return resolve(ctx, pinned, ids)
		}
		waits := 0
		engine.bindingRetryWait = func(ctx context.Context, delay time.Duration) error {
			waits++
			if err := ctx.Err(); err != nil {
				return err
			}
			if delay != 2*time.Second || transportCalls != test.transportCallsAtWait || ledger.LastSequence() != test.sequencesAtWait || stats.activeAttemptCount != 1 {
				t.Fatalf("%s retry wait delay/transport/sequence/owners=%s/%d/%d/%d", test.name, delay, transportCalls, ledger.LastSequence(), stats.activeAttemptCount)
			}
			return nil
		}

		proof, err := engine.RunTrail(context.Background())
		if err != nil || proof == nil {
			t.Fatalf("%s binding retry did not complete: proof=%v error=%v", test.name, proof, err)
		}
		if waits != 1 || bindingCalls != 4 || transportCalls != 4 || stats.activeAttemptCount != 0 {
			t.Fatalf("%s retry calls waits/bindings/transport/owners=%d/%d/%d/%d", test.name, waits, bindingCalls, transportCalls, stats.activeAttemptCount)
		}
		server.mu.Lock()
		trails, extends := len(server.trails), server.extendCount
		server.mu.Unlock()
		if trails != 1 || extends != 3 {
			t.Fatalf("%s binding retry replayed protocol work: trails=%d extends=%d", test.name, trails, extends)
		}
		records, err := ledger.RecordsAfter(0)
		if err != nil || len(records) != 4 {
			t.Fatalf("%s binding retry ledger records=%d error=%v", test.name, len(records), err)
		}
		for index, wantAssignments := range []int{1, 2, 3, 3} {
			if len(records[index].Assignments) != wantAssignments || records[index].Boundary != attemptLedgerTestBoundary() {
				t.Fatalf("%s binding retry record %d=%+v", test.name, index, records[index])
			}
		}
	}
}

// Losing pinned binding authority after receiving an authentic first assignment
// is not a retryable pre-admission event that could silently omit exposure.
func TestAttemptSettlementRunTrailKeepsAssignedBindingFailureFatal(t *testing.T) {
	hard := errors.New("lost finalized binding authority")
	for _, test := range []struct {
		name    string
		resolve func(connect.Id) (AttemptBoundary, []AttemptBinding, error)
	}{
		{name: "authority", resolve: func(connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			return AttemptBoundary{}, nil, hard
		}},
		{name: "untyped timeout", resolve: func(connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			return AttemptBoundary{}, nil, context.DeadlineExceeded
		}},
		{name: "typed integrity", resolve: func(connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			return AttemptBoundary{}, nil, &attemptBindingReadError{cause: hard}
		}},
		{name: "mixed transport and integrity", resolve: func(connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			return AttemptBoundary{}, nil, &attemptBindingReadError{cause: errors.Join(context.DeadlineExceeded, hard)}
		}},
		{name: "changed pin", resolve: func(id connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			changed := attemptLedgerTestBoundary()
			changed.EVMBlock++
			return changed, []AttemptBinding{attemptLedgerTestBinding(id, 1)}, nil
		}},
		{name: "binding mismatch", resolve: func(connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			return attemptLedgerTestBoundary(), []AttemptBinding{attemptLedgerTestBinding(connect.NewId(), 1)}, nil
		}},
	} {
		stateDir := t.TempDir()
		server, key, clientId := newMockVerifyServer(t, 12)
		engine, stats, _ := newTestEngine(t, server, key, clientId, 4, nil)
		transportCalls := 0
		engine.transport = attemptSettlementTestTransport(func(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
			transportCalls++
			return server.PostVerify(ctx, hop, body)
		})
		generation := uint64(1)
		ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
		resolve := engine.resolve
		bindingCalls, waits := 0, 0
		engine.resolve = func(ctx context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			if len(ids) == 0 {
				return resolve(ctx, pinned, ids)
			}
			bindingCalls++
			if pinned == nil || *pinned != attemptLedgerTestBoundary() || len(ids) != 1 {
				t.Fatalf("%s first assignment has no exact reserved pin: pin=%v ids=%v", test.name, pinned, ids)
			}
			return test.resolve(ids[0])
		}
		engine.bindingRetryWait = func(context.Context, time.Duration) error {
			waits++
			return nil
		}
		_, err := engine.RunTrail(context.Background())
		var fatal *TrailFatalError
		if !errors.As(err, &fatal) || stats.activeAttemptCount != 0 || ledger.LastSequence() != 0 || bindingCalls != 1 || waits != 0 || transportCalls != 1 {
			t.Fatalf("%s assigned binding failure was hidden: error=%v owners=%d sequence=%d bindings=%d waits=%d transport=%d", test.name, err, stats.activeAttemptCount, ledger.LastSequence(), bindingCalls, waits, transportCalls)
		}
		server.mu.Lock()
		assigned := len(server.trails)
		server.mu.Unlock()
		if assigned != 1 {
			t.Fatalf("%s binding-failure control did not receive one real assignment", test.name)
		}
	}
}

// Owner cancellation ends the retained assignment retry without converting it
// into fatal validator state. No additional request or checkpoint is emitted.
func TestAttemptSettlementRunTrailCancelsAssignedBindingRetry(t *testing.T) {
	for _, test := range []struct {
		name              string
		failedBindingCall int
		sequences         uint64
	}{
		{name: "first assignment", failedBindingCall: 1},
		{name: "later assignment", failedBindingCall: 2, sequences: 1},
	} {
		stateDir := t.TempDir()
		server, key, clientId := newMockVerifyServer(t, 12)
		engine, stats, _ := newTestEngine(t, server, key, clientId, 4, nil)
		transportCalls := 0
		engine.transport = attemptSettlementTestTransport(func(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
			transportCalls++
			return server.PostVerify(ctx, hop, body)
		})
		generation := uint64(1)
		ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
		resolve := engine.resolve
		bindingCalls := 0
		engine.resolve = func(ctx context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			if len(ids) == 0 {
				return resolve(ctx, pinned, ids)
			}
			bindingCalls++
			if pinned == nil || *pinned != attemptLedgerTestBoundary() || len(ids) != 1 {
				t.Fatalf("%s canceled binding changed pin or coverage: pin=%v ids=%v", test.name, pinned, ids)
			}
			if bindingCalls == test.failedBindingCall {
				return AttemptBoundary{}, nil, &attemptBindingReadError{cause: context.DeadlineExceeded}
			}
			return resolve(ctx, pinned, ids)
		}
		ctx, cancel := context.WithCancel(context.Background())
		waits := 0
		engine.bindingRetryWait = func(waitCtx context.Context, delay time.Duration) error {
			waits++
			if delay != 2*time.Second || ledger.LastSequence() != test.sequences || transportCalls != test.failedBindingCall {
				t.Fatalf("%s cancel wait delay/sequence/transport=%s/%d/%d", test.name, delay, ledger.LastSequence(), transportCalls)
			}
			cancel()
			<-waitCtx.Done()
			return waitCtx.Err()
		}
		proof, err := engine.RunTrail(ctx)
		var fatal *TrailFatalError
		if proof != nil || err != context.Canceled || errors.As(err, &fatal) || waits != 1 || bindingCalls != test.failedBindingCall || transportCalls != test.failedBindingCall || ledger.LastSequence() != test.sequences || stats.activeAttemptCount != 0 {
			t.Fatalf("%s canceled binding retry proof/error/waits/bindings/transport/sequence/owners=%v/%v/%d/%d/%d/%d/%d", test.name, proof, err, waits, bindingCalls, transportCalls, ledger.LastSequence(), stats.activeAttemptCount)
		}
		server.mu.Lock()
		trails, extends := len(server.trails), server.extendCount
		server.mu.Unlock()
		if trails != 1 || extends != test.failedBindingCall-1 {
			t.Fatalf("%s canceled binding replayed protocol work: trails=%d extends=%d", test.name, trails, extends)
		}
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

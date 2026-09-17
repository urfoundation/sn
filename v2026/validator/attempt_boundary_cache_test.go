package validator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/stabi"
)

// Blocks the actual shared census, after the finalized boundary was read.
// Its first call observes the preparation owner's cancellation explicitly.
type attemptBoundaryOwnedRPC struct {
	*attemptBoundaryRPCCounters
	started  chan struct{}
	resume   chan struct{}
	finished chan error
	failure  error
}

func (self *attemptBoundaryOwnedRPC) Hotkeys(ctx context.Context, boundary AttemptBoundary) (map[[32]byte]uint16, error) {
	self.stateLock.Lock()
	self.scans++
	first := self.scans == 1
	hotkeys := self.hotkeys
	self.stateLock.Unlock()
	if first {
		close(self.started)
		var err error
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-self.resume:
			err = self.failure
		}
		self.finished <- err
		if err != nil {
			return nil, err
		}
	}
	return hotkeys, nil
}

func newAttemptBoundaryOwnedRPC() *attemptBoundaryOwnedRPC {
	return &attemptBoundaryOwnedRPC{
		attemptBoundaryRPCCounters: &attemptBoundaryRPCCounters{
			boundary: attemptLedgerTestBoundary(), hotkeys: map[[32]byte]uint16{{1}: 7},
			bindings: map[connect.Id]stabi.BindingAtOutput{}, reads: map[connect.Id]int{},
		},
		started: make(chan struct{}), resume: make(chan struct{}), finished: make(chan error, 1),
	}
}

func TestAttemptBoundaryCacheWaiterDeadlinePreservesSharedPreparation(t *testing.T) {
	rpc := newAttemptBoundaryOwnedRPC()
	resolver := newCachedAttemptBoundaryResolverWithLifecycle(context.Background(), rpc, time.Second)
	defer resolver.close()
	firstCtx, firstCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer firstCancel()
	firstResult := make(chan error, 1)
	go func() {
		_, _, err := resolver.Resolve(firstCtx, nil, nil)
		firstResult <- err
	}()
	<-rpc.started
	if err := <-firstResult; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first waiter error = %v", err)
	}
	select {
	case err := <-rpc.finished:
		t.Fatalf("waiter canceled shared preparation: %v", err)
	default:
	}
	close(rpc.resume)
	boundary, bindings, err := resolver.Resolve(context.Background(), nil, nil)
	if err != nil || boundary != rpc.boundary || len(bindings) != 0 {
		t.Fatalf("later waiter lost prepared boundary: %+v %v %v", boundary, bindings, err)
	}
	if _, _, err := resolver.Resolve(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	rpc.stateLock.Lock()
	defer rpc.stateLock.Unlock()
	if rpc.snapshots != 1 || rpc.scans != 1 {
		t.Fatalf("completed work repeated: snapshots=%d scans=%d", rpc.snapshots, rpc.scans)
	}
}

func TestAttemptBoundaryCacheLifecycleShutdownCancelsAndJoinsPreparation(t *testing.T) {
	rpc := newAttemptBoundaryOwnedRPC()
	owner, cancel := context.WithCancel(context.Background())
	defer cancel()
	resolver := newCachedAttemptBoundaryResolverWithLifecycle(owner, rpc, time.Minute)
	result := make(chan error, 1)
	go func() {
		_, _, err := resolver.Resolve(context.Background(), nil, nil)
		result <- err
	}()
	<-rpc.started
	cancel()
	joined := make(chan struct{})
	go func() { resolver.close(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("lifecycle shutdown did not join preparation")
	}
	if err := <-rpc.finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("RPC owner error = %v", err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter shutdown error = %v", err)
	}
	if len(resolver.blocks) != 0 || resolver.latest != (AttemptBoundary{}) {
		t.Fatal("canceled preparation entered the success cache")
	}
}

func TestAttemptBoundaryCacheFailedOwnedPreparationRetries(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint("deadline=", deadline), func(t *testing.T) {
			rpc := newAttemptBoundaryOwnedRPC()
			limit := time.Second
			want := errors.New("temporary canonical census failure")
			if deadline {
				limit, want = 30*time.Millisecond, context.DeadlineExceeded
			} else {
				rpc.failure = want
				close(rpc.resume)
			}
			resolver := newCachedAttemptBoundaryResolverWithLifecycle(context.Background(), rpc, limit)
			defer resolver.close()
			if _, _, err := resolver.Resolve(context.Background(), nil, nil); !errors.Is(err, want) {
				t.Fatalf("first preparation error = %v, want %v", err, want)
			}
			// A deadline can release the snapshot waiter before its census
			// observes the same cancellation; wait for that failed load to retire.
			<-rpc.finished
			resolver.preparations.Wait()
			if _, _, err := resolver.Resolve(context.Background(), nil, nil); err != nil {
				t.Fatalf("retry did not recover: %v", err)
			}
			if _, _, err := resolver.Resolve(context.Background(), nil, nil); err != nil {
				t.Fatal(err)
			}
			rpc.stateLock.Lock()
			defer rpc.stateLock.Unlock()
			if rpc.snapshots != 2 || rpc.scans != 2 {
				t.Fatalf("failed/successful work reused incorrectly: snapshots=%d scans=%d", rpc.snapshots, rpc.scans)
			}
		})
	}
}

func TestAttemptBoundaryCacheInvalidationDuringOwnedCensus(t *testing.T) {
	rpc := newAttemptBoundaryOwnedRPC()
	resolver := newCachedAttemptBoundaryResolverWithLifecycle(context.Background(), rpc, time.Second)
	defer resolver.close()
	type outcome struct {
		boundary AttemptBoundary
		err      error
	}
	result := make(chan outcome, 1)
	go func() {
		boundary, _, err := resolver.Resolve(context.Background(), nil, nil)
		result <- outcome{boundary, err}
	}()
	<-rpc.started
	rpc.stateLock.Lock()
	rpc.boundary.SettlementEpoch++
	rpc.boundary.EVMBlock++
	rpc.boundary.EVMBlockHash = attemptHex32([32]byte{32})
	want := rpc.boundary
	rpc.stateLock.Unlock()
	resolver.invalidateLatest()
	close(rpc.resume)
	got := <-result
	if got.err != nil || got.boundary != want {
		t.Fatalf("invalidated preparation escaped: %+v %v, want %+v", got.boundary, got.err, want)
	}
	rpc.stateLock.Lock()
	defer rpc.stateLock.Unlock()
	if rpc.snapshots != 2 || rpc.scans != 2 {
		t.Fatalf("new settlement was not authenticated: snapshots=%d scans=%d", rpc.snapshots, rpc.scans)
	}
}

type attemptBoundaryRPCCounters struct {
	stateLock sync.Mutex
	boundary  AttemptBoundary
	hotkeys   map[[32]byte]uint16
	bindings  map[connect.Id]stabi.BindingAtOutput
	snapshots int
	validates int
	scans     int
	reads     map[connect.Id]int
}

type attemptBoundaryBlockingRPC struct {
	stateLock   sync.Mutex
	boundary    AttemptBoundary
	hotkeys     map[[32]byte]uint16
	bindings    map[connect.Id]stabi.BindingAtOutput
	firstStart  chan struct{}
	firstResume chan struct{}
	snapshots   int
	scans       int
}

func (self *attemptBoundaryBlockingRPC) Snapshot(context.Context) (AttemptBoundary, error) {
	self.stateLock.Lock()
	self.snapshots++
	call := self.snapshots
	boundary := self.boundary
	self.stateLock.Unlock()
	if call == 1 {
		close(self.firstStart)
		<-self.firstResume
	}
	return boundary, nil
}

func (self *attemptBoundaryBlockingRPC) Validate(context.Context, AttemptBoundary) error {
	return nil
}

func (self *attemptBoundaryBlockingRPC) Hotkeys(context.Context, AttemptBoundary) (map[[32]byte]uint16, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.scans++
	return self.hotkeys, nil
}

func (self *attemptBoundaryBlockingRPC) Binding(_ context.Context, _ AttemptBoundary, clientID connect.Id) (stabi.BindingAtOutput, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.bindings[clientID], nil
}

func (self *attemptBoundaryRPCCounters) Snapshot(context.Context) (AttemptBoundary, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.snapshots++
	return self.boundary, nil
}

func (self *attemptBoundaryRPCCounters) Validate(context.Context, AttemptBoundary) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.validates++
	return nil
}

func (self *attemptBoundaryRPCCounters) Hotkeys(context.Context, AttemptBoundary) (map[[32]byte]uint16, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.scans++
	return self.hotkeys, nil
}

func (self *attemptBoundaryRPCCounters) Binding(_ context.Context, _ AttemptBoundary, clientID connect.Id) (stabi.BindingAtOutput, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.reads[clientID]++
	return self.bindings[clientID], nil
}

func TestAttemptBoundaryCacheScansUIDsAndBindingsOncePerBlock(t *testing.T) {
	boundary := attemptLedgerTestBoundary()
	firstID, secondID := connect.NewId(), connect.NewId()
	firstHotkey, secondHotkey := [32]byte{1}, [32]byte{2}
	rpc := &attemptBoundaryRPCCounters{
		boundary: boundary, hotkeys: map[[32]byte]uint16{firstHotkey: 7, secondHotkey: 8},
		bindings: map[connect.Id]stabi.BindingAtOutput{
			firstID:  {Active: true, Record: stabi.STCoordinatorBindingRecord{FleetId: [32]byte{3}, Hotkey: firstHotkey, Generation: 1, ValidFromEpoch: 1, ValidToEpoch: 100, Uid: 7}},
			secondID: {Active: true, Record: stabi.STCoordinatorBindingRecord{FleetId: [32]byte{4}, Hotkey: secondHotkey, Generation: 2, ValidFromEpoch: 1, ValidToEpoch: 100, Uid: 8}},
		},
		reads: map[connect.Id]int{},
	}
	resolver := newCachedAttemptBoundaryResolver(rpc)
	if _, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{firstID}); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, bindings, err := resolver.Resolve(context.Background(), &boundary, []connect.Id{firstID, secondID})
			if err != nil {
				t.Errorf("cached resolve: %v", err)
				return
			}
			if !bindings[0].UIDFound || !bindings[1].UIDFound {
				t.Error("cached resolve lost live UID membership")
			}
		}()
	}
	wait.Wait()
	rpc.stateLock.Lock()
	defer rpc.stateLock.Unlock()
	if rpc.snapshots != 1 || rpc.validates != 0 || rpc.scans != 1 || rpc.reads[firstID] != 1 || rpc.reads[secondID] != 1 {
		t.Fatalf("RPC counts = snapshots %d validates %d scans %d reads %v", rpc.snapshots, rpc.validates, rpc.scans, rpc.reads)
	}
}

func TestAttemptBoundaryCacheRetainsStaleBindingWithoutHeadUID(t *testing.T) {
	boundary := attemptLedgerTestBoundary()
	clientID := connect.NewId()
	hotkey := [32]byte{9}
	rpc := &attemptBoundaryRPCCounters{
		boundary: boundary, hotkeys: map[[32]byte]uint16{}, reads: map[connect.Id]int{},
		bindings: map[connect.Id]stabi.BindingAtOutput{
			clientID: {Active: true, Record: stabi.STCoordinatorBindingRecord{FleetId: [32]byte{3}, Hotkey: hotkey, Generation: 1, ValidFromEpoch: 1, ValidToEpoch: 100, Uid: 7}},
		},
	}
	resolver := newCachedAttemptBoundaryResolver(rpc)
	_, bindings, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID})
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || !bindings[0].Active || bindings[0].UIDFound || bindings[0].UID != 0 {
		t.Fatalf("stale active binding = %+v", bindings)
	}
	if err := validateAttemptBinding(bindings[0], clientID); err != nil {
		t.Fatalf("stale binding cannot enter the signed attempt ledger: %v", err)
	}
}

func TestAttemptBoundaryCacheCoalescesFirstHopSnapshotAndRefresh(t *testing.T) {
	boundary := attemptLedgerTestBoundary()
	clientID := connect.NewId()
	hotkey := [32]byte{1}
	rpc := &attemptBoundaryRPCCounters{
		boundary: boundary, hotkeys: map[[32]byte]uint16{hotkey: 7}, reads: map[connect.Id]int{},
		bindings: map[connect.Id]stabi.BindingAtOutput{
			clientID: {Active: true, Record: stabi.STCoordinatorBindingRecord{FleetId: [32]byte{3}, Hotkey: hotkey, Generation: 1, ValidFromEpoch: 1, ValidToEpoch: 100, Uid: 7}},
		},
	}
	now := time.Unix(1_800_000_000, 0)
	resolver := newCachedAttemptBoundaryResolverWithClock(rpc, time.Second, func() time.Time { return now })
	var wait sync.WaitGroup
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID}); err != nil {
				t.Errorf("concurrent first-hop resolve: %v", err)
			}
		}()
	}
	wait.Wait()
	rpc.stateLock.Lock()
	if rpc.snapshots != 1 || rpc.scans != 1 || rpc.reads[clientID] != 1 {
		t.Fatalf("coalesced RPC counts = snapshots %d scans %d reads %v", rpc.snapshots, rpc.scans, rpc.reads)
	}
	rpc.stateLock.Unlock()

	resolver.invalidateLatest()
	if _, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID}); err != nil {
		t.Fatal(err)
	}
	rpc.stateLock.Lock()
	if rpc.snapshots != 2 || rpc.scans != 1 || rpc.reads[clientID] != 1 {
		t.Fatalf("invalidated same-block RPC counts = snapshots %d scans %d reads %v", rpc.snapshots, rpc.scans, rpc.reads)
	}
	rpc.stateLock.Unlock()

	now = now.Add(time.Second)
	if _, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID}); err != nil {
		t.Fatal(err)
	}
	rpc.stateLock.Lock()
	if rpc.snapshots != 3 || rpc.scans != 1 || rpc.reads[clientID] != 1 {
		t.Fatalf("same-block refresh RPC counts = snapshots %d scans %d reads %v", rpc.snapshots, rpc.scans, rpc.reads)
	}
	rpc.boundary.EVMBlock++
	rpc.boundary.EVMBlockHash = attemptHex32([32]byte{8})
	rpc.stateLock.Unlock()
	now = now.Add(time.Second)
	if _, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID}); err != nil {
		t.Fatal(err)
	}
	rpc.stateLock.Lock()
	defer rpc.stateLock.Unlock()
	if rpc.snapshots != 4 || rpc.scans != 2 || rpc.reads[clientID] != 2 {
		t.Fatalf("changed-block refresh RPC counts = snapshots %d scans %d reads %v", rpc.snapshots, rpc.scans, rpc.reads)
	}
}

func TestAttemptBoundaryCacheDefaultDelayAvoidsPerBlockUIDScans(t *testing.T) {
	boundary := attemptLedgerTestBoundary()
	clientID := connect.NewId()
	hotkey := [32]byte{1}
	rpc := &attemptBoundaryRPCCounters{
		boundary: boundary, hotkeys: map[[32]byte]uint16{hotkey: 7}, reads: map[connect.Id]int{},
		bindings: map[connect.Id]stabi.BindingAtOutput{
			clientID: {Active: true, Record: stabi.STCoordinatorBindingRecord{FleetId: [32]byte{3}, Hotkey: hotkey, Generation: 1, ValidFromEpoch: 1, ValidToEpoch: 100, Uid: 7}},
		},
	}
	now := time.Unix(1_800_000_000, 0)
	resolver := newCachedAttemptBoundaryResolverWithClock(rpc, attemptBoundaryRefreshDelay, func() time.Time { return now })
	for blockOffset := uint64(0); blockOffset < 10; blockOffset++ {
		rpc.stateLock.Lock()
		rpc.boundary.EVMBlock = boundary.EVMBlock + blockOffset
		rpc.boundary.EVMBlockHash = attemptHex32([32]byte{byte(20 + blockOffset)})
		rpc.stateLock.Unlock()
		if _, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(12 * time.Second)
	}
	rpc.stateLock.Lock()
	if rpc.snapshots != 1 || rpc.scans != 1 || rpc.reads[clientID] != 1 {
		t.Fatalf("per-block activity escaped refresh cache: snapshots %d scans %d reads %v", rpc.snapshots, rpc.scans, rpc.reads)
	}
	rpc.stateLock.Unlock()

	// The tenth increment reaches the exact two-minute refresh boundary.
	rpc.stateLock.Lock()
	rpc.boundary.EVMBlock = boundary.EVMBlock + 10
	rpc.boundary.EVMBlockHash = attemptHex32([32]byte{30})
	rpc.stateLock.Unlock()
	if _, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID}); err != nil {
		t.Fatal(err)
	}
	rpc.stateLock.Lock()
	defer rpc.stateLock.Unlock()
	if rpc.snapshots != 2 || rpc.scans != 2 || rpc.reads[clientID] != 2 {
		t.Fatalf("refresh boundary RPC counts = snapshots %d scans %d reads %v", rpc.snapshots, rpc.scans, rpc.reads)
	}
}

func TestAttemptBoundaryCacheSettlementInvalidationRefreshesImmediately(t *testing.T) {
	boundary := attemptLedgerTestBoundary()
	clientID := connect.NewId()
	hotkey := [32]byte{1}
	rpc := &attemptBoundaryRPCCounters{
		boundary: boundary, hotkeys: map[[32]byte]uint16{hotkey: 7}, reads: map[connect.Id]int{},
		bindings: map[connect.Id]stabi.BindingAtOutput{
			clientID: {Active: true, Record: stabi.STCoordinatorBindingRecord{FleetId: [32]byte{3}, Hotkey: hotkey, Generation: 1, ValidFromEpoch: 1, ValidToEpoch: 100, Uid: 7}},
		},
	}
	now := time.Unix(1_800_000_000, 0)
	resolver := newCachedAttemptBoundaryResolverWithClock(rpc, attemptBoundaryRefreshDelay, func() time.Time { return now })
	if _, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID}); err != nil {
		t.Fatal(err)
	}
	rpc.stateLock.Lock()
	rpc.boundary.SettlementEpoch++
	rpc.boundary.EVMBlock++
	rpc.boundary.EVMBlockHash = attemptHex32([32]byte{31})
	rpc.stateLock.Unlock()
	resolver.invalidateLatest()
	refreshed, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.SettlementEpoch != boundary.SettlementEpoch+1 {
		t.Fatalf("invalidated settlement epoch = %d, want %d", refreshed.SettlementEpoch, boundary.SettlementEpoch+1)
	}
	rpc.stateLock.Lock()
	defer rpc.stateLock.Unlock()
	if rpc.snapshots != 2 || rpc.scans != 2 || rpc.reads[clientID] != 2 {
		t.Fatalf("settlement invalidation RPC counts = snapshots %d scans %d reads %v", rpc.snapshots, rpc.scans, rpc.reads)
	}
}

func TestAttemptBoundaryCacheInvalidationDuringSnapshotDiscardsStaleResult(t *testing.T) {
	boundary := attemptLedgerTestBoundary()
	clientID := connect.NewId()
	hotkey := [32]byte{1}
	rpc := &attemptBoundaryBlockingRPC{
		boundary: boundary, hotkeys: map[[32]byte]uint16{hotkey: 7},
		bindings: map[connect.Id]stabi.BindingAtOutput{
			clientID: {Active: true, Record: stabi.STCoordinatorBindingRecord{FleetId: [32]byte{3}, Hotkey: hotkey, Generation: 1, ValidFromEpoch: 1, ValidToEpoch: 100, Uid: 7}},
		},
		firstStart: make(chan struct{}), firstResume: make(chan struct{}),
	}
	resolver := newCachedAttemptBoundaryResolver(rpc)
	type resolveResult struct {
		boundary AttemptBoundary
		err      error
	}
	result := make(chan resolveResult, 1)
	go func() {
		resolved, _, err := resolver.Resolve(context.Background(), nil, []connect.Id{clientID})
		result <- resolveResult{boundary: resolved, err: err}
	}()
	<-rpc.firstStart
	rpc.stateLock.Lock()
	rpc.boundary.SettlementEpoch++
	rpc.boundary.EVMBlock++
	rpc.boundary.EVMBlockHash = attemptHex32([32]byte{32})
	want := rpc.boundary
	rpc.stateLock.Unlock()
	resolver.invalidateLatest()
	close(rpc.firstResume)
	resolved := <-result
	if resolved.err != nil {
		t.Fatal(resolved.err)
	}
	if resolved.boundary != want {
		t.Fatalf("snapshot invalidation returned %+v, want %+v", resolved.boundary, want)
	}
	rpc.stateLock.Lock()
	defer rpc.stateLock.Unlock()
	if rpc.snapshots != 2 || rpc.scans != 1 {
		t.Fatalf("in-flight invalidation RPC counts = snapshots %d scans %d", rpc.snapshots, rpc.scans)
	}
}

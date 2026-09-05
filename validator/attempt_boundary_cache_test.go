package validator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/stabi"
)

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

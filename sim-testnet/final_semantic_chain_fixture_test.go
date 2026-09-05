package main

// Keeps fixture-only public replay work proportional to its distinct address
// bytes. Every state query and full production verifier still runs normally.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/ss58"
)

// Counts actual decoder calls across repeated queries through all three
// ordinary-fleet surfaces; elapsed time and scheduler order are irrelevant.
func TestFinalSemanticFixtureHotkeyDecodeWorkIsBounded(t *testing.T) {
	var hotkey [32]byte
	hotkey[0] = 1
	encoded, err := ss58.Encode(hotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	publicHotkey := fmt.Sprintf("0x%x", hotkey[:])
	head := ChainHead{Number: 7, Hash: finalTestHex(7)}
	evidence := &FinalSemanticEvidence{HeadFleets: []FinalHeadFleetEvidence{{
		FleetID: 1, Hotkey: encoded, CommitmentHash: finalTestHex(1),
		Members: []FinalHeadFleetMemberEvidence{{ClientID: "member", ValidFromEpoch: 1, ValidToEpoch: 9}},
	}}}
	queries := []struct {
		name string
		read func(*finalTestChainReader) error
	}{
		{name: "native commitment", read: func(reader *finalTestChainReader) error {
			state, exchanges, err := reader.NativeFleetCommitment(context.Background(), 1, publicHotkey, head)
			if err != nil || state.Hotkey != publicHotkey || state.Block != head || len(exchanges) != 1 {
				return fmt.Errorf("commitment query: state=%+v exchanges=%d error=%v", state, len(exchanges), err)
			}
			return nil
		}},
		{name: "mirror", read: func(reader *finalTestChainReader) error {
			state, exchanges, err := reader.FleetMirror(context.Background(), publicHotkey, head)
			if err != nil || state.Hotkey != publicHotkey || state.Block != head || len(exchanges) != 1 {
				return fmt.Errorf("mirror query: state=%+v exchanges=%d error=%v", state, len(exchanges), err)
			}
			return nil
		}},
		{name: "binding", read: func(reader *finalTestChainReader) error {
			state, exchanges, err := reader.FleetBinding(context.Background(), "member", 7, head)
			if err != nil || state.Hotkey != publicHotkey || state.Block != head || !state.Active || len(exchanges) != 1 {
				return fmt.Errorf("binding query: state=%+v exchanges=%d error=%v", state, len(exchanges), err)
			}
			return nil
		}},
	}
	for _, query := range queries {
		calls := 0
		reader := &finalTestChainReader{evidence: evidence, decodeHotkey: func(value string) ([32]byte, uint16, error) {
			calls++
			return ss58.Decode(value)
		}}
		for range 8 {
			if err := query.read(reader); err != nil {
				t.Fatal(err)
			}
		}
		if calls != 1 {
			t.Errorf("%s decoded the same address %d times, want 1", query.name, calls)
		}
	}
}

// Address-only memoization must observe mutable query state and new address
// bytes, including a formerly valid address replaced by invalid or wrong-prefix
// bytes. A cached query verdict would violate these transitions.
func TestFinalSemanticFixtureHotkeyDecodeTracksExactBytes(t *testing.T) {
	var firstHotkey, nextHotkey [32]byte
	firstHotkey[0], nextHotkey[0] = 1, 2
	firstEncoded, err := ss58.Encode(firstHotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	nextEncoded, err := ss58.Encode(nextHotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	wrongPrefix, err := ss58.Encode(nextHotkey, 0)
	if err != nil {
		t.Fatal(err)
	}
	head := ChainHead{Number: 7, Hash: finalTestHex(7)}
	evidence := &FinalSemanticEvidence{HeadFleets: []FinalHeadFleetEvidence{{
		FleetID: 1, Hotkey: firstEncoded, CommitmentHash: finalTestHex(1),
		Members: []FinalHeadFleetMemberEvidence{{ClientID: "member", ValidFromEpoch: 1, ValidToEpoch: 9}},
	}}}
	reader := &finalTestChainReader{evidence: evidence}
	for _, encoded := range []string{firstEncoded, nextEncoded, "invalid address", wrongPrefix, firstEncoded} {
		evidence.HeadFleets[0].Hotkey = encoded
		evidence.HeadFleets[0].CommitmentHash += "x"
		state, _, err := reader.FleetBinding(context.Background(), "member", 7, head)
		if encoded == "invalid address" || encoded == wrongPrefix {
			if err == nil || !strings.Contains(err.Error(), "unknown lifecycle binding checkpoint") {
				t.Fatalf("changed invalid address %q was accepted: %v", encoded, err)
			}
			continue
		}
		wantHotkey := firstHotkey
		if encoded == nextEncoded {
			wantHotkey = nextHotkey
		}
		if err != nil || state.Hotkey != fmt.Sprintf("0x%x", wantHotkey[:]) || state.CommitmentHash != evidence.HeadFleets[0].CommitmentHash {
			t.Fatalf("changed address/query state was hidden: state=%+v error=%v", state, err)
		}
	}
	returned, prefix, err := reader.fleetHotkey(firstEncoded)
	if err != nil || prefix != ss58.BittensorPrefix || returned != firstHotkey {
		t.Fatalf("decoded key=%x prefix=%d error=%v", returned, prefix, err)
	}
	returned[0]++
	again, _, err := reader.fleetHotkey(firstEncoded)
	if err != nil || again != firstHotkey {
		t.Fatalf("caller mutated cached public key: %x, %v", again, err)
	}
}

// One address has one decoder even under shared fleet-audit workers. A second
// address must finish while the first decoder is held at an explicit barrier,
// proving the map lock is not held over conversion or an observer callback.
func TestFinalSemanticFixtureHotkeyDecodeJoinsConcurrentReaders(t *testing.T) {
	var firstHotkey, secondHotkey [32]byte
	firstHotkey[0], secondHotkey[0] = 1, 2
	firstEncoded, err := ss58.Encode(firstHotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	secondEncoded, err := ss58.Encode(secondHotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var firstCalls atomic.Int32
	reader := &finalTestChainReader{decodeHotkey: func(encoded string) ([32]byte, uint16, error) {
		if encoded == firstEncoded {
			if firstCalls.Add(1) == 1 {
				close(entered)
			}
			<-release
		}
		return ss58.Decode(encoded)
	}}
	const readers = 8
	results := make(chan error, readers)
	var joined sync.WaitGroup
	defer func() {
		unblock()
		joined.Wait()
	}()
	for range readers {
		joined.Add(1)
		go func() {
			defer joined.Done()
			key, prefix, err := reader.fleetHotkey(firstEncoded)
			if err != nil || key != firstHotkey || prefix != ss58.BittensorPrefix {
				results <- fmt.Errorf("concurrent decoded key=%x prefix=%d error=%v", key, prefix, err)
				return
			}
			results <- nil
		}()
	}
	<-entered
	secondDone := make(chan error, 1)
	joined.Add(1)
	go func() {
		defer joined.Done()
		key, prefix, err := reader.fleetHotkey(secondEncoded)
		if err != nil || key != secondHotkey || prefix != ss58.BittensorPrefix {
			secondDone <- fmt.Errorf("independent decoded key=%x prefix=%d error=%v", key, prefix, err)
			return
		}
		secondDone <- nil
	}()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an independent address was blocked behind the held decoder")
	}
	unblock()
	joined.Wait()
	for range readers {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls := firstCalls.Load(); calls != 1 {
		t.Fatalf("concurrent readers decoded one address %d times, want 1", calls)
	}
}

// A historical lookup allocates only its returned exchange and two owned raw
// JSON byte slices. It must not allocate a copy of every unqueried version.
func TestFinalSemanticFixtureGenerationLookupDoesNotAllocateCensus(t *testing.T) {
	head := ChainHead{Number: 7, Hash: finalTestHex(7)}
	version := FinalFleetGenerationVersionEvidence{Hotkey: finalTestHex(1), CommitmentHash: finalTestHex(2), NativeHead: head}
	evidence := &FinalSemanticEvidence{FleetGeneration: &FinalFleetGenerationLineageEvidence{
		SetupFleets: make([]FinalFleetGenerationFleetEvidence, finalFleetGenerationSetupFleetCount),
	}}
	evidence.FleetGeneration.SetupFleets[0].Initial = version
	evidence.FleetGeneration.SetupFleets[1].Initial = version
	evidence.FleetGeneration.SetupFleets[1].Initial.CommitmentHash = finalTestHex(3)
	reader := &finalTestChainReader{evidence: evidence}
	var state FinalNativeFleetCommitmentState
	var exchanges []FinalRPCExchange
	var readErr error
	allocations := testing.AllocsPerRun(8, func() {
		state, exchanges, readErr = reader.NativeFleetCommitment(context.Background(), 1, version.Hotkey, head)
	})
	if readErr != nil || state.CommitmentHash != version.CommitmentHash || state.Block != head || len(exchanges) != 1 {
		t.Fatalf("historical lookup state=%+v exchanges=%d error=%v", state, len(exchanges), readErr)
	}
	if allocations > 3 {
		t.Errorf("historical lookup allocated %g objects, want at most its 3 owned transcript buffers", allocations)
	}
	evidence.FleetGeneration.SetupFleets[0].Initial.Hotkey = finalTestHex(4)
	state, _, readErr = reader.NativeFleetCommitment(context.Background(), 1, version.Hotkey, head)
	if readErr != nil || state.CommitmentHash != finalTestHex(3) {
		t.Fatalf("changed historical lookup reused an earlier match: state=%+v error=%v", state, readErr)
	}
}

// Identifies exactly one of the historical reader mutations without relying on
// a test-case name or callback arrival order; multiple active faults are invalid.
func finalSemanticChainFixtureReaderCase(reader *finalTestChainReader) (int, error) {
	index := 0
	for fault, enabled := range []bool{
		reader.failCanonical, reader.corruptWeights, reader.corruptCustody,
		reader.corruptSettlement, reader.corruptOwnerStake, reader.corruptReserveReceipt,
		reader.corruptEpochDeposit, reader.corruptOperatorVersion, reader.corruptPoolExpiry,
	} {
		if !enabled {
			continue
		}
		if index != 0 {
			return 0, errors.New("one chain reader contains multiple faults")
		}
		index = fault + 1
	}
	return index, nil
}

// Pins every original replay and rejection reason, with a private reader for
// each callback. Every case must call the supplied verifier exactly once.
func TestFinalSemanticFixtureChainCasesKeepExactCensusAndPrivateReaders(t *testing.T) {
	wantNames := []string{"valid", "archive-unavailable", "applied-weight", "custody", "settlement-accounting", "owner-stake", "reserve-receipt", "epoch-deposit", "operator-version", "pool-expiry"}
	wantErrors := []string{"", "archive unavailable", "applied vector", "custody/policy state mismatch", "settlement-vault accounting mismatch", "owner-pair stake mismatch", "ReservePrincipalAdded receipt", "cumulative deposit differs from signed audit", "terminal pool evidence", "pool epoch"}
	evidence := &FinalSemanticEvidence{DeploymentID: "immutable-fixture"}
	var stateLock sync.Mutex
	readers := make(map[*finalTestChainReader]bool)
	completed := make([]int, len(wantNames))
	cases := finalSemanticChainVerificationTestCases(evidence, func(ctx context.Context, got *FinalSemanticEvidence, value FinalSemanticChainReader) error {
		reader, ok := value.(*finalTestChainReader)
		if !ok || ctx == nil || got != evidence || reader.evidence != evidence {
			return errors.New("chain case lost its verification context or evidence")
		}
		index, err := finalSemanticChainFixtureReaderCase(reader)
		if err != nil {
			return err
		}
		stateLock.Lock()
		defer stateLock.Unlock()
		if readers[reader] {
			return errors.New("chain cases share one mutable reader")
		}
		readers[reader] = true
		completed[index]++
		if wantErrors[index] == "" {
			return nil
		}
		return errors.New(wantErrors[index])
	})
	if len(cases) != len(wantNames) {
		t.Fatalf("chain cases=%d, want all %d", len(cases), len(wantNames))
	}
	for index, testCase := range cases {
		if testCase.name != wantNames[index] {
			t.Fatalf("chain case %d=%q, want %q", index, testCase.name, wantNames[index])
		}
	}
	results := runFinalSemanticTestCases(context.Background(), cases)
	if len(results) != len(wantNames) || len(readers) != len(wantNames) {
		t.Fatalf("chain cases returned %d results from %d readers", len(results), len(readers))
	}
	for index, err := range results {
		if err != nil || completed[index] != 1 {
			t.Errorf("chain case %s completed %d times: %v", wantNames[index], completed[index], err)
		}
	}
}

// Cancellation must reach all ten callbacks and still join their work. Held
// first-wave callbacks force useful overlap; results retain source order even
// when each verifier returns a different unexpected failure.
func TestFinalSemanticFixtureChainCasesJoinAllFailuresAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, finalSemanticTestCaseWorkers)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	completed := make([]atomic.Int32, 10)
	cases := finalSemanticChainVerificationTestCases(&FinalSemanticEvidence{}, func(ctx context.Context, _ *FinalSemanticEvidence, value FinalSemanticChainReader) error {
		index, err := finalSemanticChainFixtureReaderCase(value.(*finalTestChainReader))
		if err != nil {
			return err
		}
		if index < finalSemanticTestCaseWorkers {
			entered <- struct{}{}
			<-release
		}
		completed[index].Add(1)
		if !errors.Is(ctx.Err(), context.Canceled) {
			return fmt.Errorf("case %d lost cancellation", index)
		}
		return fmt.Errorf("unexpected case %d: %w", index, ctx.Err())
	})
	done := make(chan []error, 1)
	go func() { done <- runFinalSemanticTestCases(ctx, cases) }()
	for range finalSemanticTestCaseWorkers {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			cancel()
			unblock()
			<-done
			t.Fatal("chain cases did not reach the concurrent callback barrier")
		}
	}
	cancel()
	unblock()
	results := <-done
	if len(results) != len(completed) {
		t.Fatalf("joined chain results=%d, want %d", len(results), len(completed))
	}
	for index, err := range results {
		if completed[index].Load() != 1 || err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unexpected case %d: context canceled", index)) {
			t.Errorf("case %d completed %d times with %v", index, completed[index].Load(), err)
		}
	}
}

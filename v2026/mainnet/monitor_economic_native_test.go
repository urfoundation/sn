package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestEconomicNativeFeesAttributeExactExtrinsicAndPreserveSigningGuard(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	raw := economicNativeFeeFixture(t, fixture, 3)
	observation, err := observeEconomicEmission(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy))
	if err != nil || !observation.Complete || len(observation.Blocks) != 2 || len(observation.Blocks[1].Fees) != 1 {
		t.Fatal("actual archive did not retain its exact native fee", err)
	}
	fee := observation.Blocks[1].Fees[0]
	if fee.EventIndex != 0 || fee.Phase != "ApplyExtrinsic" || fee.ExtrinsicIndex != 0 || fee.ExtrinsicHash != "0xac1df047e36531a712df2d4b5054ce80d6ab11665896c97daf1ae7e99e17ea1c" || fee.Payer != fixture.policy.FeePayers[0] || fee.ActualFeeRao != "12" || fee.TipRao != "3" || !fee.DispatchSuccess {
		t.Fatal("fee borrowed a balance, transaction or tip", fee)
	}
	economicEmissionAssertUnresolved(t, observation)
	if _, err := nativeDecodeReceiptEvents(fixture.chain.metadata, raw, 0, 1, fixture.policy.FeePayers[0], nil); err == nil {
		t.Fatal("observational fee decoding weakened signing's zero-tip guard")
	}
	raw = economicNativeFeeFixture(t, fixture, 0)
	if receipt, err := nativeDecodeReceiptEvents(fixture.chain.metadata, raw, 0, 1, fixture.policy.FeePayers[0], nil); err != nil || receipt.ActualFeeRao != 12 || !receipt.Success {
		t.Fatal("shared traversal changed valid zero-tip receipt", receipt, err)
	}
}

func TestEconomicNativeFeesRefuseBorrowedPhaseAndDuplicatedOutcome(t *testing.T) {
	for _, mode := range []string{"initialization", "foreign-dispatch", "duplicate-fee", "duplicate-outcome", "outside-body"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newEconomicEmissionFixture(t)
			payer := bytes.Repeat([]byte{0x11}, 32)
			fee := rootReceiptEventFixture(t, fixture.chain.metadata, "TransactionPayment.TransactionFeePaid", 0, payer, binary.LittleEndian.AppendUint64(nil, 12), make([]byte, 8))
			terminal := rootReceiptEventFixture(t, fixture.chain.metadata, "System.ExtrinsicSuccess", 0)
			count := uint64(2)
			switch mode {
			case "initialization":
				fee = append([]byte{2}, fee[5:]...)
			case "foreign-dispatch":
				terminal = rootReceiptEventFixture(t, fixture.chain.metadata, "System.ExtrinsicSuccess", 1)
			case "duplicate-fee":
				fee = append(fee, fee...)
				count++
			case "duplicate-outcome":
				terminal = append(terminal, terminal...)
				count++
			case "outside-body":
				binary.LittleEndian.PutUint32(fee[1:5], 2)
			}
			raw := append(append(rootCompact(count), fee...), terminal...)
			if fees, err := decodeEconomicNativeFees(fixture.chain.metadata, raw, [][]byte{{8, 4, 0}, {8, 5, 0}}, []string{"0x" + strings.Repeat("11", 32)}); err == nil || fees != nil {
				t.Fatal("ambiguous native fee became accepted evidence", mode, fees, err)
			}
		})
	}
}

func TestMonitorEconomicNativePublicRestartKeepsOriginalCursorAndFees(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	fixture.policy.Observation.Through = economicEmissionBoundary{Number: 101, Hash: fixture.source.chain.byHeight[101]}
	fixture.policy.BatchBlocks = 1
	first := fixture.start(t, monitorServiceHooks{})
	event := first.next(t)
	if !event.Current || event.State.Cursor.Number != 101 || event.State.BatchCount != 1 || event.State.ObservedAlpha != "10" || monitorEconomicTestFee(event) != "0" {
		t.Fatal("first contiguous page lost its original evidence", event)
	}
	first.stop(t)
	second := fixture.start(t, monitorServiceHooks{})
	event = second.next(t)
	if !event.Current || event.State.Cursor.Number != 102 || event.State.BatchCount != 2 || event.State.ObservedAlpha != "10" || monitorEconomicTestFee(event) != "12" || event.State.HistoryEntries != 2 {
		t.Fatal("restart repeated or skipped retained economic history", event)
	}
	if event.State.Authority != "owned-rpc-assertion" || event.State.NativeMinerAllocationAlpha != nil || event.State.ProviderEntitlementAlpha != nil || event.State.OwnerRecycledAlpha != nil || event.State.ActualNativeOutcomeVerified {
		t.Fatal("RPC assertion became independently proven economics", event)
	}
	second.stop(t)
	third := fixture.start(t, monitorServiceHooks{})
	event = third.next(t)
	if !event.Current || event.Status != "caught-up" || event.State.BatchCount != 2 || event.State.HistoryEntries != 2 {
		t.Fatal("same finalized head repeated a retained economic event", event)
	}
	third.stop(t)
	if first.exit != 0 || second.exit != 0 || third.exit != 0 {
		t.Fatal("public restart did not join cleanly", first.exit, second.exit, third.exit)
	}
}

func TestMonitorEconomicNativePublicPartialOutageKeepsCursorUntilRecovery(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	header, hash := rootReceiptHeaderFixture(t, "0x"+strings.Repeat("ab", 32), 6000, nil, false)
	fixture.source.chain.headers[hash], fixture.source.chain.byHeight[6000], fixture.source.chain.finalized = header, hash, hash
	fixture.unavailable.Store(true)
	var waits, closed atomic.Int32
	hooks := monitorServiceHooks{
		rpcWait: func(ctx context.Context, _ string, _ time.Duration) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 300*time.Second || time.Until(deadline) < time.Minute {
				return errors.New("logical read lost its single 300-second budget")
			}
			waits.Add(1)
			// A real 503 reached the owned wait. Only deadline exhaustion is
			// injected; this is not a 300-second wall-clock test.
			return context.DeadlineExceeded
		},
		afterClose: func(role, kind string, _ *os.File) error {
			if role == fixture.policy.Role && kind == "checkpoint" {
				closed.Add(1)
			}
			return nil
		},
	}
	run := fixture.start(t, hooks)
	first := run.next(t)
	if first.Current || first.Status != "unavailable" || first.State.Cursor.Number != 100 || first.State.HistoryEntries != 0 || first.State.BatchCount != 0 || waits.Load() != 1 {
		t.Fatal("partial read advanced cursor or invented a sample", first, waits.Load())
	}
	if record := fixture.record(t); record.State.Cursor.Number != 100 || len(record.State.History) != 0 {
		t.Fatal("partial page reached durable checkpoint", record)
	}
	fixture.unavailable.Store(false)
	run.resume <- struct{}{}
	second := run.next(t)
	if !second.Current || second.State.Cursor.Number != 102 || second.State.ObservedAlpha != "10" || monitorEconomicTestFee(second) != "12" || second.State.BatchCount != 1 || closed.Load() != 0 {
		t.Fatal("temporary outage replaced owner or lost original page", second, closed.Load())
	}
	run.stop(t)
	if run.exit != 0 || closed.Load() != 1 {
		t.Fatal("cancellation did not join retained native owner", run.exit, closed.Load())
	}
}

func TestMonitorEconomicNativePublicLostAckReopensOriginalCompletedPage(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	var failed atomic.Bool
	closed := make(chan struct{}, 2)
	hooks := monitorServiceHooks{
		syncDirectory: func(role, kind string, file *os.File) error {
			err := file.Sync()
			if role == fixture.policy.Role && kind == "checkpoint" && failed.CompareAndSwap(false, true) {
				return errors.Join(err, syscall.EIO)
			}
			return err
		},
		afterClose: func(role, kind string, file *os.File) error {
			if role == fixture.policy.Role && kind == "checkpoint" {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("uncertain native owner was not joined")
				}
				closed <- struct{}{}
			}
			return nil
		},
	}
	run := fixture.start(t, hooks)
	first := run.next(t)
	if first.Current || first.CheckpointCurrent || first.State.Cursor.Number != 100 || first.State.BatchCount != 0 {
		t.Fatal("lost acknowledgement advanced published native cursor", first)
	}
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("uncertain native owner did not close")
	}
	record := fixture.record(t)
	if record.State.Cursor.Number != 102 || record.State.BatchCount != 1 || record.State.ObservedFeesRao != "12" {
		t.Fatal("uncertain write lost actual completed native bytes", record)
	}
	run.resume <- struct{}{}
	second := run.next(t)
	if !second.Current || second.State.Cursor.Number != 102 || second.State.BatchCount != 1 || second.State.HistoryEntries != 2 {
		t.Fatal("joined native recovery repeated or discarded committed events", second)
	}
	run.stop(t)
	if run.exit != 0 {
		t.Fatal("native lost acknowledgement continuation failed", run.exit, run.diagnostic.String())
	}
}

func TestMonitorEconomicNativePublicCapacityCommitsOnlyCompleteSubpage(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	fixture.policy.HistoryEntries = 1
	run := fixture.start(t, monitorServiceHooks{})
	first := run.next(t)
	if !first.Current || first.State.Cursor.Number != 101 || first.State.PendingThrough == nil || first.State.PendingThrough.Number != 102 || first.State.HistoryEntries != 1 {
		t.Fatal("bounded native subpage did not retain original requested high-water", first)
	}
	run.resume <- struct{}{}
	second := run.next(t)
	if second.Current || second.Status != "capacity-held" || second.State.Cursor.Number != 101 || second.State.ObservedAlpha != "10" || monitorEconomicTestFee(second) != "0" || second.State.HistoryEntries != 1 {
		t.Fatal("native capacity deleted history or skipped the unread event", second)
	}
	run.stop(t)
	record := fixture.record(t)
	if record.State.PendingThrough == nil || record.State.PendingThrough.Number != 102 || len(record.State.History) != 1 {
		t.Fatal("capacity hold lost original pending range", record)
	}
}

func TestMonitorEconomicNativePublicLostCustodyStopsOnlyAffectedRole(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, true)
	terminal := make(chan int, 1)
	run := fixture.start(t, monitorServiceHooks{afterWorker: func(role string, exit int) {
		if role == fixture.policy.Role {
			terminal <- exit
		}
	}})
	if first := run.next(t); !first.Current {
		t.Fatal("native first page did not complete", first)
	}
	select {
	case <-run.sink.peers:
	case <-time.After(10 * time.Second):
		t.Fatal("independent validator peer never sampled")
	}
	checkpoint, _ := monitorEconomicNativePaths(fixture.services.checkpointPath, fixture.services.metricsPath, fixture.policy.Role)
	if err := os.Remove(checkpoint); err != nil {
		t.Fatal(err)
	}
	run.resume <- struct{}{}
	if event := run.next(t); event.Current || event.Status != "identity-conflict" || event.State.BatchCount != 1 {
		t.Fatal("lost checkpoint became fresh native history", event)
	}
	select {
	case exit := <-terminal:
		if exit != 3 {
			t.Fatal("native custody loss was not terminal", exit)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("lost native owner did not stop")
	}
	run.peerResume <- struct{}{}
	select {
	case peer := <-run.sink.peers:
		if peer.Role != "validator-a" {
			t.Fatal("wrong independent peer", peer.Role)
		}
	case <-run.done:
		t.Fatal("native loss stopped unrelated monitor")
	case <-time.After(10 * time.Second):
		t.Fatal("independent peer did not continue")
	}
	run.stop(t)
	if run.exit != 3 {
		t.Fatal("affected role failure was not retained", run.exit)
	}
}

func TestMonitorEconomicNativePublicHistoricalCatchupBeyondAncestryLimit(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	fixture.policy.Observation.Through = economicEmissionBoundary{Number: 101, Hash: fixture.source.chain.byHeight[101]}
	fixture.policy.BatchBlocks = 1
	header, hash := rootReceiptHeaderFixture(t, "0x"+strings.Repeat("ab", 32), 6000, nil, false)
	fixture.source.chain.headers[hash], fixture.source.chain.byHeight[6000], fixture.source.chain.finalized = header, hash, hash
	first := fixture.start(t, monitorServiceHooks{})
	if event := first.next(t); !event.Current || event.State.Cursor.Number != 101 || event.State.Finalized == nil || event.State.Finalized.Number != 6000 {
		t.Fatal("healthy archive could not commit bounded page behind distant head", event)
	}
	first.stop(t)
	second := fixture.start(t, monitorServiceHooks{})
	if event := second.next(t); !event.Current || event.State.Cursor.Number != 102 || event.State.BatchCount != 2 || monitorEconomicTestFee(event) != "12" {
		t.Fatal("restart skipped original historical cursor", event)
	}
	second.stop(t)
	fixture.source.chain.stateLock.Lock()
	reads := fixture.source.chain.counts["chain_getHeader"]
	fixture.source.chain.stateLock.Unlock()
	if reads > 40 {
		t.Fatal("historical page walked an unbounded head-to-cursor bridge", reads)
	}
}

func TestMonitorEconomicNativePublicHistoricalCanonicalChangeRefusesReset(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	header, hash := rootReceiptHeaderFixture(t, "0x"+strings.Repeat("ab", 32), 6000, nil, false)
	fixture.source.chain.headers[hash], fixture.source.chain.byHeight[6000], fixture.source.chain.finalized = header, hash, hash
	fixture.policy.Observation.Through = economicEmissionBoundary{Number: 101, Hash: fixture.source.chain.byHeight[101]}
	fixture.policy.BatchBlocks = 1
	first := fixture.start(t, monitorServiceHooks{})
	if event := first.next(t); !event.Current {
		t.Fatal(event)
	}
	first.stop(t)
	fixture.source.chain.stateLock.Lock()
	fixture.source.chain.byHeight[101] = "0x" + strings.Repeat("ef", 32)
	fixture.source.chain.stateLock.Unlock()
	second := fixture.start(t, monitorServiceHooks{})
	event := second.next(t)
	if event.Current || event.Status != "identity-conflict" || event.State.Cursor.Hash != fixture.policy.Observation.Through.Hash || event.State.BatchCount != 1 || event.State.ObservedAlpha != "10" {
		t.Fatal("changed historical cursor silently moved to latest", event)
	}
	second.stop(t)
	if second.exit != 3 {
		t.Fatal("canonical conflict did not stop affected native reader", second.exit)
	}
}

func TestMonitorEconomicNativePublicLargeCensusShrinksWithoutSkipping(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	source := fixture.source
	// Every block is a legitimate 4096-UID event. The complete 128-block
	// request exceeds evidence bytes before it exceeds event-count policy.
	fixture.policy.Observation.MaximumUids = 4096
	fixture.policy.Observation.FeePayers = nil
	fixture.policy.BatchBlocks, fixture.policy.HistoryEntries = 128, 2048
	source.set(t, 100, "SubnetworkN", binary.LittleEndian.AppendUint16(nil, 4096))
	base := source.storageKVs[source.chain.byHeight[100]]
	amounts := make([]uint64, 4096)
	for index := range amounts {
		amounts[index] = math.MaxUint64
	}
	parent := source.chain.byHeight[100]
	for number := uint64(101); number <= 228; number++ {
		header, hash := rootReceiptHeaderFixture(t, parent, number, nil, false)
		source.chain.headers[hash], source.chain.byHeight[number], source.chain.bodies[hash] = header, hash, []string{}
		values := map[string]*string{}
		for key, value := range base {
			values[key] = value
		}
		source.storageKVs[hash] = values
		source.set(t, number, "SubnetEpochIndex", binary.LittleEndian.AppendUint64(nil, 7+number-100))
		source.set(t, number, "LastEpochBlock", binary.LittleEndian.AppendUint64(nil, number))
		source.set(t, number, "PendingServerEmission", make([]byte, 8))
		source.incentive(t, number, 25, amounts...)
		parent = hash
	}
	source.chain.finalized = parent
	fixture.policy.Observation.Through = economicEmissionBoundary{Number: 228, Hash: parent}
	run := fixture.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number <= 100 || event.State.Cursor.Number >= 228 || event.State.PendingThrough == nil || event.State.PendingThrough.Number != 228 || event.State.HistoryEntries != int(event.State.Cursor.Number-100) || event.State.BatchCount != 1 || event.State.ObservedFeesRao != nil {
		t.Fatal("large legitimate census stalled, dropped events or abandoned original high-water", event)
	}
	run.stop(t)
	record := fixture.record(t)
	if record.State.Cursor != event.State.Cursor || len(record.State.History) != event.State.HistoryEntries || record.State.CapacityBytesRemaining == 0 {
		t.Fatal("adaptive complete page was not durably retained", record.State.Cursor)
	}
	for index, item := range record.State.History {
		if item.Block.Number != uint64(101+index) || item.Incentive == nil || len(item.Incentive.AlphaByUid) != 4096 {
			t.Fatal("adaptive page skipped original full-census block", index)
		}
	}
}

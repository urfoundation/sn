// The public continuous role keeps its original checkpoint and live owner
// through lower-only RPC heads, while authenticated canonical conflicts stop it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// A successful page, lagging next sample, and recovered sample run through one
// public command. Explicit sample barriers prove no history loss or owner reset.
func TestMonitorEconomicNativeLowerHeadKeepsOwnerAndOriginalHistory(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	fixture.policy.Observation.Through = economicEmissionBoundary{Number: 101, Hash: fixture.source.chain.byHeight[101]}
	fixture.policy.BatchBlocks = 1
	prior := fixture.source.chain.fault
	var lower atomic.Bool
	fixture.source.chain.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if lower.Load() && method == "chain_getFinalizedHead" {
			return fixture.source.chain.byHeight[101], true
		}
		return prior(method, params, count)
	}
	var waits, closes atomic.Int32
	chainReady := make(chan struct{}, 1)
	run := fixture.start(t, monitorServiceHooks{
		afterEvent: func(_ context.Context, role string) {
			if role == "chain" {
				chainReady <- struct{}{}
			}
		},
		rpcWait: func(ctx context.Context, role string, _ time.Duration) error {
			deadline, ok := ctx.Deadline()
			if role != fixture.policy.Role || !ok || time.Until(deadline) > 300*time.Second || time.Until(deadline) < 60*time.Second {
				return errors.New("lower-head read lost original 300-second owner")
			}
			waits.Add(1)
			return context.DeadlineExceeded
		},
		afterClose: func(role, kind string, _ *os.File) error {
			if role == fixture.policy.Role && kind == "checkpoint" {
				closes.Add(1)
			}
			return nil
		},
	})
	first := run.next(t)
	if !first.Current || first.State.Cursor.Number != 101 || first.State.BatchCount != 1 || first.State.Finalized == nil || first.State.Finalized.Number != 102 {
		t.Fatal("original page did not publish", first)
	}
	select {
	case <-chainReady:
	case <-time.After(60 * time.Second):
		t.Fatal("independent chain owner did not reach its sample barrier")
	}
	original := fixture.record(t).State
	lower.Store(true)
	run.resume <- struct{}{}
	second := run.next(t)
	retained := fixture.record(t).State
	if second.Current || second.Status != "unavailable" || waits.Load() != 1 || closes.Load() != 0 || retained.Cursor != original.Cursor || retained.BatchCount != original.BatchCount || retained.BatchChainHash != original.BatchChainHash || !reflect.DeepEqual(retained.Finalized, original.Finalized) || !reflect.DeepEqual(retained.History, original.History) {
		t.Fatalf("lower head replaced owner or retained history: event=%+v waits=%d closes=%d", second, waits.Load(), closes.Load())
	}
	lower.Store(false)
	run.resume <- struct{}{}
	third := run.next(t)
	if !third.Current || third.State.Cursor.Number != 102 || third.State.BatchCount != 2 || third.State.HistoryEntries != 2 || third.State.ObservedAlpha != first.State.ObservedAlpha || monitorEconomicTestFee(third) != "12" || closes.Load() != 0 {
		t.Fatal("recovery repeated custody or changed fee evidence", third, closes.Load())
	}
	run.resume <- struct{}{}
	fourth := run.next(t)
	if !fourth.Current || fourth.Status != "caught-up" || fourth.State.BatchCount != 2 || fourth.State.HistoryEntries != 2 || monitorEconomicTestFee(fourth) != "12" {
		t.Fatal("caught-up sample repeated completed native evidence", fourth)
	}
	run.stop(t)
	if run.exit != 0 || closes.Load() != 1 {
		t.Fatal("original continuous owner did not join", run.exit, closes.Load())
	}
}

// Initial reviewed boundaries also wait for coverage before reading their
// body/state. A later sample consumes the same exact initial range once.
func TestMonitorEconomicNativeInitialLowerHeadRecoversOriginalRange(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	prior := fixture.source.chain.fault
	var lower atomic.Bool
	lower.Store(true)
	fixture.source.chain.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if lower.Load() && method == "chain_getFinalizedHead" {
			return fixture.source.chain.byHeight[100], true
		}
		return prior(method, params, count)
	}
	var waits atomic.Int32
	run := fixture.start(t, monitorServiceHooks{rpcWait: func(context.Context, string, time.Duration) error { waits.Add(1); return context.DeadlineExceeded }})
	first := run.next(t)
	if first.Current || first.Status != "unavailable" || first.State.Cursor.Number != 100 || first.State.BatchCount != 0 || first.State.HistoryEntries != 0 || waits.Load() != 1 {
		t.Fatal("uncovered initial range fabricated history", first, waits.Load())
	}
	lower.Store(false)
	run.resume <- struct{}{}
	second := run.next(t)
	if !second.Current || second.State.Cursor.Number != 102 || second.State.BatchCount != 1 || second.State.ObservedAlpha != "10" || monitorEconomicTestFee(second) != "12" {
		t.Fatal("recovered initial range was replaced", second)
	}
	run.stop(t)
}

// A changed original cursor is a contradiction even when the current head is
// also behind it. Public output and exit keep the hard cause, with no wait.
func TestMonitorEconomicNativeLowerHeadCannotMaskOriginalConflict(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	prior := fixture.source.chain.fault
	var conflict atomic.Bool
	fixture.source.chain.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if conflict.Load() {
			if method == "chain_getFinalizedHead" {
				return fixture.source.chain.byHeight[100], true
			}
			if method == "chain_getBlockHash" && len(params) == 1 && string(params[0]) == "102" {
				return testGenesisHash, true
			}
		}
		return prior(method, params, count)
	}
	var waits atomic.Int32
	chainReady := make(chan struct{}, 1)
	workerExit := make(chan int, 1)
	run := fixture.start(t, monitorServiceHooks{
		rpcWait: func(context.Context, string, time.Duration) error { waits.Add(1); return context.DeadlineExceeded },
		afterEvent: func(_ context.Context, role string) {
			if role == "chain" {
				chainReady <- struct{}{}
			}
		},
		afterWorker: func(role string, exit int) {
			if role == fixture.policy.Role {
				workerExit <- exit
			}
		},
	})
	first := run.next(t)
	if !first.Current {
		t.Fatal("original page did not publish", first)
	}
	select {
	case <-chainReady:
	case <-time.After(60 * time.Second):
		t.Fatal("independent chain owner did not reach its sample barrier")
	}
	original := fixture.record(t).State
	conflict.Store(true)
	run.resume <- struct{}{}
	second := run.next(t)
	if second.Current || second.Status != "identity-conflict" || waits.Load() != 0 || second.State.Cursor != original.Cursor || second.State.BatchCount != original.BatchCount {
		t.Fatal("canonical conflict became retryable head lag", second, waits.Load())
	}
	select {
	case exit := <-workerExit:
		if exit != 3 {
			t.Fatal("hard canonical conflict did not stop its own role", exit)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("hard canonical conflict did not join affected role")
	}
	run.stop(t)
	if run.exit != 3 || !reflect.DeepEqual(fixture.record(t).State.History, original.History) {
		t.Fatal("hard canonical conflict changed custody or exit", run.exit)
	}
}

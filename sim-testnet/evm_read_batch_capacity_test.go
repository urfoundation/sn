// Forced typed timeouts and an explicit clock verify learned grouping and
// recovery without sleeping, live endpoints or scheduler-dependent outcomes.
package main

import (
	"context"
	"errors"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

// The first operation discovers capacity seven; all later blocks begin there
// while retaining every original calldata/result position and the four rounds.
func TestEvmReadBatchCapacityRetainsTimeoutLearningAcrossOperations(t *testing.T) {
	capacity := &evmReadBatchCapacity{width: maximumEVMRPCBatchCalls, now: func() time.Time { return time.Unix(10, 0) }}
	var widths []int
	var retries int
	policy := immediateFinalSemanticRetryPolicy()
	policy.wait = func(context.Context, time.Duration) error { retries++; return nil }
	for operation := 0; operation < 2; operation++ {
		widths, retries = nil, 0
		reads := adaptiveEvmBatchTestReads(50)
		for index := range reads {
			reads[index].args[1] = operation
		}
		results, err := readEvmRpcBatchWithCapacity[hexutil.Bytes](t.Context(), "synthetic coordinator block", reads, policy, func(_ context.Context, batch []rpc.BatchElem) error {
			widths = append(widths, len(batch))
			if len(batch) > 7 {
				return context.DeadlineExceeded
			}
			for _, element := range batch {
				if element.Method != "eth_call" || element.Args[1] != operation {
					t.Fatalf("pinned operation changed: %+v", element)
				}
				*element.Result.(*hexutil.Bytes) = []byte{byte(element.Args[0].(int) + 1)}
			}
			return nil
		}, capacity)
		wantWidths, wantRetries := []int{50, 25, 13, 7, 7, 7, 7, 7, 7, 7, 1}, 3
		if operation == 1 {
			wantWidths, wantRetries = []int{7, 7, 7, 7, 7, 7, 7, 1}, 0
		}
		if err != nil || retries != wantRetries || !slices.Equal(widths, wantWidths) || len(results) != len(reads) {
			t.Fatalf("operation%d re-learned archive capacity: widths=%v retries=%d err=%v", operation, widths, retries, err)
		}
		for index, result := range results {
			if result.err != nil || !slices.Equal(result.value, []byte{byte(index + 1)}) {
				t.Fatalf("operation%d result%d changed: %+v", operation, index, result)
			}
		}
	}
}

// Exhausted batches retain their smaller hint for the next recovery, but no
// failed output becomes successful. Rate/connection/hard errors do not teach it.
func TestEvmReadBatchCapacityLearnsOnlyTimeoutsWithoutAcceptingFailures(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, syscall.ECONNRESET, errors.New("state already discarded")} {
		capacity := &evmReadBatchCapacity{width: maximumEVMRPCBatchCalls, now: func() time.Time { return time.Unix(10, 0) }}
		results, err := readEvmRpcBatchWithCapacity[hexutil.Bytes](t.Context(), "synthetic rejection", adaptiveEvmBatchTestReads(50), immediateFinalSemanticRetryPolicy(), func(_ context.Context, batch []rpc.BatchElem) error {
			for _, element := range batch {
				*element.Result.(*hexutil.Bytes) = []byte{1}
			}
			return failure
		}, capacity)
		if err != nil || len(results) != 50 {
			t.Fatalf("batch rejection result: %v", err)
		}
		for _, result := range results {
			if !errors.Is(result.err, failure) || len(result.value) != 0 {
				t.Fatalf("failed transport contributed bytes: %+v", result)
			}
		}
		want := maximumEVMRPCBatchCalls
		if errors.Is(failure, context.DeadlineExceeded) {
			want = 4
		}
		if got := capacity.begin(50).width; got != want {
			t.Fatalf("failure %v taught width%d want%d", failure, got, want)
		}
	}
}

// Expiration grants exactly one wider probe, and newer failure evidence wins
// over an older in-flight success. Successful probes recover progressively.
func TestEvmReadBatchCapacityExpiresWithOneProbeAndRejectsStaleSuccess(t *testing.T) {
	now := time.Unix(10, 0)
	capacity := &evmReadBatchCapacity{width: maximumEVMRPCBatchCalls, now: func() time.Time { return now }}
	capacity.timedOut(7)
	if lease := capacity.begin(50); lease.width != 7 || lease.probe {
		t.Fatalf("capacity probed before quiet interval: %+v", lease)
	}
	now = now.Add(evmReadBatchRecoveryInterval)
	probe := capacity.begin(50)
	ordinary := capacity.begin(50)
	if !probe.probe || probe.width != 14 || ordinary.probe || ordinary.width != 7 {
		t.Fatalf("expiry did not reserve one gradual probe: probe=%+v ordinary=%+v", probe, ordinary)
	}
	capacity.timedOut(4)
	probe.succeeded()
	if got := probe.limit(14); got != 4 {
		t.Fatalf("stale success overwrote newer timeout: %d", got)
	}
	for _, width := range []int{8, 16, 32, 50} {
		now = now.Add(evmReadBatchRecoveryInterval)
		probe = capacity.begin(50)
		if !probe.probe || probe.width != width {
			t.Fatalf("gradual recovery width=%d want%d", probe.width, width)
		}
		probe.succeeded()
		if got := capacity.begin(50).width; got != width {
			t.Fatalf("successful capacity probe was lost: %d want%d", got, width)
		}
	}
}

// Exact connection identity and RPC method fence hints. Bounded eviction never
// changes a request or shares a learned limit with a newly dialed connection.
func TestEvmReadBatchCapacityScopesConnectionsAndMethodsWithBoundedStorage(t *testing.T) {
	now := time.Unix(10, 0)
	registry := &evmReadBatchCapacityRegistry{now: func() time.Time { return now }}
	client := new(rpc.Client)
	calls := adaptiveEvmBatchTestReads(50)
	capacity := registry.forReads(client, calls)
	capacity.timedOut(7)
	if got := registry.forReads(client, calls).begin(50).width; got != 7 {
		t.Fatalf("same route lost learned grouping: %d", got)
	}
	blocks := []evmRpcRead{{method: "eth_getBlockByNumber"}}
	if got := registry.forReads(client, blocks).begin(50).width; got != 50 {
		t.Fatalf("eth_call slowdown leaked to block reader: %d", got)
	}
	if got := registry.forReads(new(rpc.Client), calls).begin(50).width; got != 50 {
		t.Fatalf("different connection inherited learned width: %d", got)
	}
	for index := 0; index < evmReadBatchMaximumRoutes; index++ {
		now = now.Add(time.Second)
		registry.forReads(new(rpc.Client), calls)
	}
	if len(registry.capacityKVs) != evmReadBatchMaximumRoutes {
		t.Fatalf("route hints are unbounded: %d", len(registry.capacityKVs))
	}
	if got := registry.forReads(client, calls).begin(50).width; got != 50 {
		t.Fatalf("evicted route did not cold-start: %d", got)
	}
	if got := registry.forReads(client, []evmRpcRead{{method: "eth_call"}, {method: "eth_getBlockByNumber"}}); got != nil {
		t.Fatal("mixed operations acquired a guessed capacity domain")
	}
}

// Parallel callers share at most one recovery probe and cannot race a newer
// capacity reduction into being overwritten by old successful work.
func TestEvmReadBatchCapacityConcurrentRecoveryKeepsLatestReduction(t *testing.T) {
	capacity := &evmReadBatchCapacity{width: 7, recoverAfter: time.Unix(1, 0), now: func() time.Time { return time.Unix(2, 0) }}
	leases := make(chan evmReadBatchCapacityLease, 16)
	var wait sync.WaitGroup
	for index := 0; index < cap(leases); index++ {
		wait.Go(func() { leases <- capacity.begin(50) })
	}
	wait.Wait()
	close(leases)
	capacity.timedOut(4)
	probes := 0
	for lease := range leases {
		if lease.probe {
			probes++
		}
		wait.Go(lease.succeeded)
	}
	wait.Wait()
	if got := capacity.begin(50).width; probes != 1 || got != 4 {
		t.Fatalf("concurrent recovery overwrote latest reduction: probes=%d width=%d", probes, got)
	}
}

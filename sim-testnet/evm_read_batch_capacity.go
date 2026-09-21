// Batch capacity is an expiring scheduling hint for one RPC client and read
// method. It changes only request grouping; every pinned input, result check
// and retry deadline remains owned by the normal verifier.
package main

import (
	"context"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
)

const (
	evmReadBatchRecoveryInterval = 5 * time.Minute
	evmReadBatchMaximumRoutes    = 256
)

type evmReadBatchCapacityKey struct {
	client *rpc.Client
	method string
}

type evmReadBatchCapacityEntry struct {
	capacity *evmReadBatchCapacity
	lastUsed time.Time
}

// The bounded registry never retains RPC results, credentials or authority.
// A new connection is a new route; eviction simply makes a later batch cold.
type evmReadBatchCapacityRegistry struct {
	stateLock   sync.Mutex
	capacityKVs map[evmReadBatchCapacityKey]evmReadBatchCapacityEntry
	now         func() time.Time
}

var evmReadBatchCapacities = evmReadBatchCapacityRegistry{now: time.Now}

// This hint is safe to share concurrently; failed wider requests can only
// lower capacity. A stale wider success cannot undo a newer timeout.
type evmReadBatchCapacity struct {
	stateLock    sync.Mutex
	width        int
	generation   uint64
	recoverAfter time.Time
	now          func() time.Time
}

type evmReadBatchCapacityLease struct {
	capacity   *evmReadBatchCapacity
	width      int
	generation uint64
	probe      bool
}

// Distinct RPC methods do not share their work limit. Mixed methods retain
// ordinary adaptation within their own call rather than guessing a domain.
func (self *evmReadBatchCapacityRegistry) forReads(client *rpc.Client, reads []evmRpcRead) *evmReadBatchCapacity {
	if client == nil || len(reads) == 0 {
		return nil
	}
	method := reads[0].method
	if method != "eth_call" && method != "eth_getBlockByNumber" {
		return nil
	}
	for _, read := range reads {
		if read.method != method {
			return nil
		}
	}
	now := self.now()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	key := evmReadBatchCapacityKey{client: client, method: method}
	if entry, ok := self.capacityKVs[key]; ok {
		entry.lastUsed = now
		self.capacityKVs[key] = entry
		return entry.capacity
	}
	if self.capacityKVs == nil {
		self.capacityKVs = map[evmReadBatchCapacityKey]evmReadBatchCapacityEntry{}
	}
	if len(self.capacityKVs) >= evmReadBatchMaximumRoutes {
		var oldestKey evmReadBatchCapacityKey
		var oldestTime time.Time
		for candidate, entry := range self.capacityKVs {
			if oldestTime.IsZero() || entry.lastUsed.Before(oldestTime) {
				oldestKey, oldestTime = candidate, entry.lastUsed
			}
		}
		delete(self.capacityKVs, oldestKey)
	}
	capacity := &evmReadBatchCapacity{width: maximumEVMRPCBatchCalls, now: self.now}
	self.capacityKVs[key] = evmReadBatchCapacityEntry{capacity: capacity, lastUsed: now}
	return capacity
}

// After a quiet interval, one caller may probe twice the learned width.
// Reserving that probe immediately prevents a concurrent recovery stampede.
func (self *evmReadBatchCapacity) begin(requested int) evmReadBatchCapacityLease {
	lease := evmReadBatchCapacityLease{capacity: self, width: requested}
	if self == nil {
		return lease
	}
	now := self.now()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	lease.width, lease.generation = min(requested, self.width), self.generation
	if requested > self.width && !self.recoverAfter.IsZero() && !now.Before(self.recoverAfter) {
		lease.width = min(requested, self.width*2)
		lease.probe = true
		self.recoverAfter = now.Add(evmReadBatchRecoveryInterval)
	}
	return lease
}

// Adopt concurrent timeout discoveries before the next request. Only the
// explicitly reserved recovery probe may exceed the current learned ceiling.
func (self evmReadBatchCapacityLease) limit(width int) int {
	if self.capacity == nil {
		return width
	}
	self.capacity.stateLock.Lock()
	defer self.capacity.stateLock.Unlock()
	if self.probe && self.generation == self.capacity.generation {
		return width
	}
	return min(width, self.capacity.width)
}

// Learn from typed request timeouts only. A smaller pending suffix is still a
// safe conservative grouping hint and never grants verification authority.
func (self *evmReadBatchCapacity) timedOut(nextWidth int) {
	if self == nil {
		return
	}
	now := self.now()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.width = max(1, min(self.width, nextWidth))
	self.generation++
	self.recoverAfter = now.Add(evmReadBatchRecoveryInterval)
}

// Only a complete successful probe may raise capacity. Newer timeout evidence
// dominates an older in-flight success, including another caller's probe.
func (self evmReadBatchCapacityLease) succeeded() {
	if self.capacity == nil || !self.probe {
		return
	}
	now := self.capacity.now()
	self.capacity.stateLock.Lock()
	defer self.capacity.stateLock.Unlock()
	if self.generation == self.capacity.generation {
		self.capacity.width = max(self.capacity.width, self.width)
		self.capacity.generation++
		self.capacity.recoverAfter = now.Add(evmReadBatchRecoveryInterval)
	}
}

// Production readers share the learned grouping through their exact client.
// Callback-only tests and callers keep the ordinary per-call retry contract.
func readEvmRpcBatchForClientWithPolicy[T any](ctx context.Context, operation string, reads []evmRpcRead, policy finalSemanticRPCRetryPolicy, client *rpc.Client) ([]evmRpcReadResult[T], error) {
	if client == nil {
		return readEvmRpcBatchWithPolicy[T](ctx, operation, reads, policy, nil)
	}
	return readEvmRpcBatchWithCapacity[T](ctx, operation, reads, policy, client.BatchCallContext, evmReadBatchCapacities.forReads(client, reads))
}

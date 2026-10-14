// Economic archive reads preserve completed original evidence while the same
// read owner waits for its finalized-head coverage to become available again.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Both archive profiles wait after the actual original range was read. The
// dependency barrier is the closing finalized-head request, not wall time.
func TestEconomicEmissionLowerClosingHeadRetriesOnlyFinality(t *testing.T) {
	for _, historical := range []bool{false, true} {
		fixture := newEconomicEmissionFixture(t)
		prior := fixture.chain.fault
		var recovered atomic.Bool
		var storageReads atomic.Int64
		fixture.chain.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
			if method == "state_getStorage" {
				storageReads.Add(1)
			}
			if method == "chain_getFinalizedHead" && count >= 2 && !recovered.Load() {
				return fixture.chain.byHeight[100], true
			}
			return prior(method, params, count)
		}
		waits := 0
		var completedReads int64
		fixture.client.retryWait = func(context.Context, time.Duration) error {
			waits++
			completedReads = storageReads.Load()
			recovered.Store(true)
			return nil
		}
		result, err := observeEconomicEmissionPage(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy), historical)
		if err != nil || waits != 1 || !result.Complete || len(result.Blocks) != 2 || result.Finalized != fixture.policy.Through || result.ClosingFinalized != fixture.policy.Through || result.ObservedIncentiveTotalAlpha != "10" || completedReads == 0 || storageReads.Load() != completedReads {
			t.Fatalf("historical=%t closing wait changed original archive: %+v waits=%d storage=%d/%d err=%v", historical, result, waits, completedReads, storageReads.Load(), err)
		}
		economicEmissionAssertUnresolved(t, result)
	}
}

// Exhaustion retains the original two-block evidence but cannot publish a
// complete observation or mislabel temporary head lag as an integrity failure.
func TestEconomicEmissionLowerClosingHeadRetainsUnavailablePrefix(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	prior := fixture.chain.fault
	fixture.chain.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method == "chain_getFinalizedHead" && count >= 2 {
			return fixture.chain.byHeight[100], true
		}
		return prior(method, params, count)
	}
	waits := 0
	fixture.client.retryWait = func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }
	result, err := observeEconomicEmissionPage(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy), true)
	if waits != 1 || !errors.Is(err, errRpcObservationUnavailable) || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errRpcIntegrity) || result.Complete || len(result.Blocks) != 2 || result.ObservedIncentiveTotalAlpha != "10" || result.ClosingFinalized.Hash != "" {
		t.Fatalf("lower closing head erased originals or became a contradiction: %+v waits=%d err=%v", result, waits, err)
	}
	economicEmissionAssertUnresolved(t, result)
}

// A genuine returned conflict wins even when the same closing read reports a
// lower head. The complete already-read prefix remains merely partial evidence.
func TestEconomicEmissionLowerClosingHeadCannotMaskCanonicalConflict(t *testing.T) {
	fixture := newEconomicEmissionFixture(t)
	prior := fixture.chain.fault
	var closing atomic.Bool
	fixture.chain.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method == "chain_getFinalizedHead" && count >= 2 {
			closing.Store(true)
			return fixture.chain.byHeight[100], true
		}
		if closing.Load() && method == "chain_getBlockHash" && len(params) == 1 && string(params[0]) == "102" {
			return testGenesisHash, true
		}
		return prior(method, params, count)
	}
	waits := 0
	fixture.client.retryWait = func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }
	result, err := observeEconomicEmissionPage(t.Context(), fixture.client, fixture.policy, rootObjectHash(fixture.policy), true)
	if !errors.Is(err, errRpcIntegrity) || errors.Is(err, errRpcObservationUnavailable) || waits != 0 || result.Complete || len(result.Blocks) != 2 {
		t.Fatalf("lower head hid exact economic canonical conflict: waits=%d blocks=%d err=%v", waits, len(result.Blocks), err)
	}
}

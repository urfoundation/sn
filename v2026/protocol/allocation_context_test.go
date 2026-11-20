// Work-count contexts cancel the actual owner without timing or scheduler assumptions.
package protocol

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
)

// The inherited Done/Err belongs to a real cancellation, triggered at a fixed work boundary.
type allocationWorkContext struct {
	context.Context
	cancel context.CancelFunc
	calls  atomic.Int32
	stop   int32
}

// Cancellation never depends on elapsed time or goroutine scheduling.
func (self *allocationWorkContext) Err() error {
	if self.calls.Add(1) == self.stop {
		self.cancel()
	}
	return self.Context.Err()
}

// Each test owns and releases the genuine operation context.
func allocationCancelAt(t *testing.T, stop int32) *allocationWorkContext {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return &allocationWorkContext{Context: ctx, cancel: cancel, stop: stop}
}

// Include shared recipients and unequal weights so the healthy result exercises exact apportionment.
func allocationContextInputs() []ProviderAllocation {
	return []ProviderAllocation{
		{ClientID: [16]byte{1}, Coldkey: [32]byte{1}, UsageBytes: 7, ReliabilityPPM: 500000, Eligible: true},
		{ClientID: [16]byte{2}, Coldkey: [32]byte{2}, UsageBytes: 11, ReliabilityPPM: 1000000, Eligible: true},
		{ClientID: [16]byte{3}, Coldkey: [32]byte{1}, UsageBytes: 13, ReliabilityPPM: 1000000, Eligible: true},
	}
}

// A mid-input cancellation publishes neither a prefix nor detached arithmetic work.
func TestAllocateSharesOwnedInputCancellation(t *testing.T) {
	ctx := allocationCancelAt(t, 3)
	shares, err := AllocateSharesWithContext(ctx, allocationContextInputs())
	if shares != nil || !errors.Is(err, context.Canceled) || ctx.calls.Load() != 3 {
		t.Fatal("allocation ignored owned input cancellation", shares, err, ctx.calls.Load())
	}
}

// The legacy wrapper and explicit owner compute the same full rounding result.
func TestAllocateSharesOwnedCanonicalCompatibility(t *testing.T) {
	inputs := allocationContextInputs()
	legacy, err := AllocateShares(inputs)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := AllocateSharesWithContext(t.Context(), inputs)
	if err != nil || !reflect.DeepEqual(legacy, owned) {
		t.Fatal("owned allocation changed exact shares", owned, err)
	}
}

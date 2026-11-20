// Complete-census controls retain the original exact-block reader and mutate
// actual runtime/storage replies, never a replacement stake verdict.
package crv4

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Peer entries include non-permitted and zero-stake seats; single-validator
// observations remain comparable and no returned slice aliases another read.
func TestValidatorStakeCensusPreservesCompleteRuntimeObservation(t *testing.T) {
	f := newValidatorStakeTestFixture(t)
	prior, err := f.read()
	if err != nil {
		t.Fatal(err)
	}
	observed, err := ReadValidatorStakeCensusAtContext(f.identity.ctx, f.identity.chain, f.identity.query, f.identity.allowed...)
	if err != nil || observed.Selected != prior || len(observed.Entries) != 3 || f.runtimeCalls != 2 {
		t.Fatal("complete runtime census differs from original reader", observed, err)
	}
	for i, stake := range []uint64{0, 150, math.MaxInt64} {
		if observed.Entries[i].TotalStakeFloorRao != stake || observed.Entries[i].ValidatorPermit != (i == 1) {
			t.Fatal("peer stake or permit omitted", i, observed.Entries[i])
		}
	}
	observed.Entries[1].Hotkey[0] ^= 1
	again, err := ReadValidatorStakeCensusAtContext(f.identity.ctx, f.identity.chain, f.identity.query, f.identity.allowed...)
	if err != nil || again.Entries[1].Hotkey != prior.Identity.Hotkey || again.Selected != prior {
		t.Fatal("returned census aliases later observations", err)
	}
}

// Admission bounds precede even the first identity RPC.
func TestValidatorStakeCensusRejectsUnboundedScopeBeforeReads(t *testing.T) {
	f := newValidatorStakeTestFixture(t)
	for _, maximum := range []uint32{0, 4097, math.MaxUint32} {
		query := f.identity.query
		query.MaximumSubnetUIDs = maximum
		observed, err := ReadValidatorStakeCensusAtContext(f.identity.ctx, f.identity.chain, query, f.identity.allowed...)
		if err == nil || len(observed.Entries) != 0 || observed.Selected != (ValidatorStakeObservation{}) || len(f.identity.calls) != 0 {
			t.Fatal("unbounded census acquired reads or partial facts", maximum, observed, err)
		}
	}
}

// An unselected malformed peer or owner reverse mapping cannot disappear when
// the selected validator itself still has correct storage and sufficient stake.
func TestValidatorStakeCensusRejectsPeerAndOwnerContradictions(t *testing.T) {
	for _, fault := range []string{"duplicate", "zero", "wrong-owner", "unregistered-owner"} {
		f := newValidatorStakeTestFixture(t)
		switch fault {
		case "duplicate":
			copy(f.fields[52][1:33], f.identity.hotkey[:])
		case "zero":
			clear(f.fields[52][1:33])
		case "wrong-owner":
			uid := uint16(0)
			f.setOwner(t, [32]byte{44}, &uid)
		case "unregistered-owner":
			f.setOwner(t, [32]byte{31}, nil)
		}
		f.publish(t)
		observed, err := ReadValidatorStakeCensusAtContext(f.identity.ctx, f.identity.chain, f.identity.query, f.identity.allowed...)
		if err == nil || len(observed.Entries) != 0 || observed.Selected != (ValidatorStakeObservation{}) || f.runtimeCalls != 1 {
			t.Fatal("invalid peer or owner retained a census", fault, observed, err)
		}
	}
}

// Complete decoded entries are discarded if the final canonical read changes
// or cancellation occurs after the runtime API has already answered.
func TestValidatorStakeCensusDiscardsClosingChangeAndCancellation(t *testing.T) {
	for _, cancelRead := range []bool{false, true} {
		f := newValidatorStakeTestFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		f.identity.ctx = ctx
		original := f.identity.hook
		f.identity.hook = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
			handled, err := original(ctx, result, method, args...)
			if method == "state_call" {
				if cancelRead {
					cancel()
				} else {
					f.identity.blockHashes[f.identity.query.BlockNumber] = types.Hash{99}
				}
			}
			return handled, err
		}
		observed, err := ReadValidatorStakeCensusAtContext(ctx, f.identity.chain, f.identity.query, f.identity.allowed...)
		if err == nil || cancelRead && !errors.Is(err, context.Canceled) || len(observed.Entries) != 0 || observed.Selected != (ValidatorStakeObservation{}) || f.runtimeCalls != 1 {
			t.Fatal("late failure retained a complete or partial census", cancelRead, observed, err)
		}
	}
}

// Exact transport barriers exercise public identity, mapping and runtime
// readers without replacing their finality decisions or waiting wall time.
package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A genuine selected header above a stale RPC head remains unavailable until
// that same owner observes coverage, without selecting another historical hash.
func TestIdentityHistoricalFinalityWaitsForLowerOpeningHead(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	f.client.retryWindow = 300 * time.Second
	f.head = 90
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	waits := 0
	f.client.retryWait = func(waitCtx context.Context, _ time.Duration) error {
		waits++
		if actual, ok := waitCtx.Deadline(); !ok || actual != deadline {
			t.Fatal("coverage replaced the original observation deadline", actual, deadline)
		}
		f.head = 150
		return nil
	}
	identity, err := f.client.readIdentityAt(ctx, f.mapping.nativeHash)
	if err != nil || waits != 1 || identity.FinalizedNumber != 100 || identity.FinalizedHash != f.mapping.nativeHash || identity.finalityWitness.Number != 150 || f.counts["state_getRuntimeVersion"] != 1 {
		t.Fatalf("lower opening head replaced or refused original history: %+v waits=%d reads=%v err=%v", identity, waits, f.counts, err)
	}
}

// Runtime bytes are already complete when coverage becomes unavailable. The
// wait retries finality only and cannot spend a new sample budget.
func TestIdentityFinalityWaitsWithoutRepeatingRuntime(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	f.after = func(method string, _ []any) {
		if method == "state_getRuntimeVersion" {
			f.head = 100
		}
	}
	waits := 0
	f.client.retryWait = func(context.Context, time.Duration) error {
		waits++
		f.head = 180
		return nil
	}
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil || waits != 1 || identity.FinalizedHash != f.mapping.nativeHash || identity.finalityWitness.Number != 150 || f.counts["state_getRuntimeVersion"] != 1 {
		t.Fatalf("closing coverage replaced the opening witness or runtime: %+v waits=%d reads=%v err=%v", identity, waits, f.counts, err)
	}
}

// Covering selected100 is insufficient while original finalized150 is still
// unavailable. No EVM artifact is read until the original witness is covered.
func TestFinalizedMappingWaitsForOriginalWitnessBeforeEvm(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	f.head = 100
	waits := 0
	f.client.retryWait = func(context.Context, time.Duration) error {
		waits++
		if f.counts["debug_getRawHeader"] != 0 {
			t.Fatal("mapping read EVM bytes before original finality coverage")
		}
		f.head = 180
		return nil
	}
	mapping, err := f.client.readFinalizedMappingAtIdentity(t.Context(), identity)
	if err != nil || waits != 1 || mapping.Identity != identity || mapping.EvmHeader.Hash != f.mapping.evmHash || mapping.FinalityAuthority != "owned-rpc-assertion" || f.counts["debug_getRawHeader"] != 1 {
		t.Fatalf("recovered mapping changed original clocks or authority: %+v waits=%d err=%v", mapping, waits, err)
	}
}

// A lower reply after EVM decoding retries only native closure. The completed
// original EVM read and selected native block are never restarted or replaced.
func TestFinalizedMappingClosingCoverageDoesNotRepeatEvm(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	f.after = func(method string, _ []any) {
		if method == "debug_getRawHeader" {
			f.head = 90
		}
	}
	waits := 0
	f.client.retryWait = func(context.Context, time.Duration) error { waits++; f.head = 180; return nil }
	mapping, err := f.client.readFinalizedMappingAtIdentity(t.Context(), identity)
	if err != nil || waits != 1 || mapping.Identity != identity || f.counts["debug_getRawHeader"] != 1 {
		t.Fatalf("closing coverage repeated or replaced completed evidence: %+v waits=%d reads=%v err=%v", mapping, waits, f.counts, err)
	}
}

// Cancellation retains both the unavailable condition and the exact original
// owner cause; no partial mapping or synthetic integrity verdict escapes.
func TestFinalizedMappingLowerHeadPreservesCancellationCause(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	f.head = 90
	cause := errors.New("synthetic original finality owner canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	waits := 0
	f.client.retryWait = func(waitCtx context.Context, _ time.Duration) error { waits++; cancel(cause); return waitCtx.Err() }
	mapping, err := f.client.readFinalizedMappingAtIdentity(ctx, identity)
	if waits != 1 || !errors.Is(err, errRpcObservationUnavailable) || !errors.Is(err, context.Canceled) || !errors.Is(err, cause) || errors.Is(err, errRpcIntegrity) || mapping.Schema != "" || f.counts["debug_getRawHeader"] != 0 {
		t.Fatalf("lower head lost cancellation or published partial mapping: %+v waits=%d err=%v", mapping, waits, err)
	}
}

// A lower head cannot suppress an actually returned contradiction to either
// original hash. No retry is authorized once the canonical conflict is known.
func TestFinalizedMappingLowerHeadCannotMaskCanonicalConflict(t *testing.T) {
	for _, number := range []uint64{100, 150} {
		f := newIdentityFinalityFixture(t)
		identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
		if err != nil {
			t.Fatal(err)
		}
		f.head, f.canonicalKVs[number] = 90, testGenesisHash
		waits := 0
		f.client.retryWait = func(context.Context, time.Duration) error { waits++; return context.DeadlineExceeded }
		mapping, err := f.client.readFinalizedMappingAtIdentity(t.Context(), identity)
		if !errors.Is(err, errRpcIntegrity) || errors.Is(err, errRpcObservationUnavailable) || waits != 0 || mapping.Schema != "" || f.counts["debug_getRawHeader"] != 0 {
			t.Fatalf("lower head concealed changed block%d: %+v waits=%d err=%v", number, mapping, waits, err)
		}
	}
}

// A lagging archive may temporarily omit an exact retained height. Only this
// retained read uses the nullable profile; returned malformed hashes stay hard.
func TestFinalizedMappingLowerHeadRetriesMissingRetainedHash(t *testing.T) {
	f := newIdentityFinalityFixture(t)
	identity, err := f.client.readIdentityAt(t.Context(), f.mapping.nativeHash)
	if err != nil {
		t.Fatal(err)
	}
	f.head = 90
	missing := true
	f.reply = func(method string, params []any) (any, bool) {
		return nil, missing && method == "chain_getBlockHash" && params[0] == float64(100)
	}
	waits := 0
	f.client.retryWait = func(context.Context, time.Duration) error {
		waits++
		missing, f.head = false, 180
		return nil
	}
	mapping, err := f.client.readFinalizedMappingAtIdentity(t.Context(), identity)
	if err != nil || waits != 2 || mapping.Identity != identity || f.counts["debug_getRawHeader"] != 1 {
		t.Fatalf("missing retained hash became contradiction or replaced history: %+v waits=%d err=%v", mapping, waits, err)
	}
}

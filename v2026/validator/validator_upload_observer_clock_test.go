//go:build linux || darwin

// The real refresh must judge captured chain timestamps against a clock read
// after both observer reads, retaining its original start and absolute expiry.
package validator

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Advance the owned clock during the actual native observer read, after the
// EVM header was captured. No timer, sleep or admission verdict is injected.
func validatorUploadObserverClockTestRead(fixture *validatorUploadAuthorityTestFixture, observed *time.Time, at time.Time, timestamp uint64, cancel context.CancelFunc) {
	original := fixture.native.chain.API.Client
	advanced := false
	fixture.native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if method == "chain_getFinalizedHead" && !advanced {
			advanced = true
			*observed = at
			fixture.currentMillis = timestamp
			if cancel != nil {
				cancel()
			}
		}
		return original.CallContext(ctx, result, method, args...)
	}}
}

// A healthy finalized block can be minted while an earlier observer RPC is in
// flight. Its timestamp exceeds refresh start without exceeding capture time.
func TestValidatorUploadAdmissionUsesClockAfterObserverReads(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner, reports := validatorUploadSilentDropTestState(t, fixture)
	started, observed := fixture.now, fixture.now
	captured := started.Add(5 * time.Second)
	owner.clock = func() time.Time { return observed }
	validatorUploadObserverClockTestRead(fixture, &observed, captured, uint64(captured.UnixMilli()), nil)
	if err := owner.refresh(t.Context(), started); err != nil {
		t.Fatalf("healthy native block minted during observer reads was refused: %v", err)
	}
	header, sessionHash, contentHash := validatorUploadAdmissionTestHeader(t, fixture, fixture.authority.Expected, 0x41, "current-api-client", []byte("object"))
	lease, err := owner.beginAt(t.Context(), header, sessionHash, 1, contentHash, 6, captured)
	if err != nil || lease == nil {
		t.Fatalf("fresh observed authority could not admit the actual request: %v", err)
	}
	lease.Close()
	activationHash, err := fixture.authority.Expected.Digest()
	if err != nil {
		t.Fatal(err)
	}
	expires := started.Add(time.Duration(owner.config.MaximumHeadAgeSeconds) * time.Second)
	entry := owner.entries[activationHash]
	if entry == nil || !entry.validUntil.Equal(expires) || !owner.validUntil.Equal(expires) {
		t.Fatalf("observer clock correction extended absolute head expiry: entry=%v expiry=%s", entry, owner.validUntil)
	}
	if len(*reports) != 1 || !(*reports)[0].started.Equal(started) || !(*reports)[0].finished.Equal(captured) ||
		(*reports)[0].evmHeadAge != 5*time.Second || (*reports)[0].nativeHeadAge != 0 || (*reports)[0].err != nil {
		t.Fatalf("refresh start, duration or capture-time ages differ: %+v", *reports)
	}
}

// Sampling after reads neither grants future timestamps nor renews old heads.
// Cancellation during observation still prevents every publication and lease.
func TestValidatorUploadAdmissionObserverClockKeepsFreshnessAndCancellation(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"native-future", "native-stale", "evm-future", "evm-stale", "cancel"} {
		fixture := newValidatorUploadAuthorityTestFixture(t)
		owner, reports := validatorUploadSilentDropTestState(t, fixture)
		started, observed := fixture.now, fixture.now
		captured := started.Add(5 * time.Second)
		timestamp := uint64(captured.UnixMilli())
		window := time.Duration(owner.config.MaximumHeadAgeSeconds) * time.Second
		switch fault {
		case "native-future":
			timestamp++
		case "native-stale":
			timestamp = uint64(captured.Add(-window).UnixMilli())
		case "evm-future":
			fixture.now = captured.Add(time.Second)
		case "evm-stale":
			fixture.now = captured.Add(-window)
		}
		owner.clock = func() time.Time { return observed }
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)
		var duringRead context.CancelFunc
		if fault == "cancel" {
			duringRead = cancel
		}
		validatorUploadObserverClockTestRead(fixture, &observed, captured, timestamp, duringRead)
		err := owner.refresh(ctx, started)
		if err == nil || len(owner.entries) != 0 || !owner.validUntil.IsZero() {
			t.Fatalf("%s granted authority: error=%v entries=%d expiry=%s", fault, err, len(owner.entries), owner.validUntil)
		}
		if fault == "cancel" && !errors.Is(err, context.Canceled) {
			t.Fatalf("observer cancellation lost its cause: %v", err)
		}
		if len(*reports) != 1 || (*reports)[0].err == nil || (*reports)[0].admitted != 0 {
			t.Fatalf("%s refusal was not reported: %+v", fault, *reports)
		}
	}
}

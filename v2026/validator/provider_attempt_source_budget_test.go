//go:build linux

// Actual public reads prove that a caller reserve clips original acquisition
// without changing its independently pinned authority or historical bytes.
package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// A larger admitted ceiling does not require allocating its entire capacity.
func TestProviderAttemptSourceCallerReservePreservesOriginalAuthority(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, true)
	want, first, err := fixture.source.Read(t.Context(), fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	maximum := uint64(len(want)) + 8
	if maximum >= fixture.source.authority.MaxOriginalBytes {
		t.Fatal("fixture does not exercise a smaller caller reserve")
	}
	raw, actual, err := fixture.source.ReadBounded(t.Context(), fixture.artifact, maximum)
	if err != nil || actual == nil || !bytes.Equal(raw, want) || actual.AuthorityHash != first.AuthorityHash || actual.OriginalHash != first.OriginalHash {
		t.Fatalf("bounded actual originals changed authority or bytes: %v", err)
	}
}

// The signed window may fit while its replay objects do not. Refuse before
// opening either original metadata replica, not after retaining its payload.
func TestProviderAttemptSourceCallerReserveStopsBeforeObjectTransport(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	initial := ProviderAttemptOriginal{Schema: ProviderAttemptOriginalSchema, AuthorityHash: fixture.source.hash, Response: fixture.response, Objects: []ProviderAttemptOriginalObject{}, Receipts: []ProviderAttemptOriginalResponse{}}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	before := [2][2][2]int{fixture.base.owners[0].fixture.counts(), fixture.base.owners[1].fixture.counts()}
	if raw, result, err := fixture.source.ReadBounded(t.Context(), fixture.artifact, uint64(len(raw))+8); raw != nil || result != nil || !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
		t.Fatalf("original transport exceeded caller reserve: %v", err)
	}
	if after := [2][2][2]int{fixture.base.owners[0].fixture.counts(), fixture.base.owners[1].fixture.counts()}; after != before {
		t.Fatal("over-capacity replay opened an original object")
	}
}

// An empty remaining physical frame performs no discovery request at all.
func TestProviderAttemptSourceZeroCallerReserveDoesNotRead(t *testing.T) {
	fixture := newProviderAttemptSourceTestFixture(t, false)
	fixture.stateLock.Lock()
	before := fixture.reads
	fixture.stateLock.Unlock()
	if raw, result, err := fixture.source.ReadBounded(t.Context(), fixture.artifact, 0); raw != nil || result != nil || !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
		t.Fatalf("zero original reserve returned authority: %v", err)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if fixture.reads != before {
		t.Fatal("zero original reserve opened discovery transport")
	}
}

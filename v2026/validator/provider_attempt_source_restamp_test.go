//go:build linux || darwin

// VALIDATOR.md §7.5, §8.2. A hop the server confirms after a 421 records the
// step's last 421 as its assigned_ms, so its latency counts from the
// attributed attempt. The receipt census accepts exactly that move of a
// pending hop's assigned_ms, and still counts the assignment at its own time.
package validator

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// A pending hop assigned at 1000ms, as the census first saw it.
func restampTestPending() providerAttemptHopEvent {
	network := connect.Id{71}
	hop := protocol.ProviderAttemptReceiptHop{ClientId: connect.Id{9}, NetworkId: &network, AssignedMs: 1000, AssignN: 5}
	return providerAttemptHopEvent{hop: hop, assignedMs: hop.AssignedMs}
}

// The same hop confirmed at 1025ms, with its assigned_ms as given.
func restampTestConfirmed(assignedMs uint64) *protocol.ProviderAttemptReceiptHop {
	hop := restampTestPending().hop
	hop.AssignedMs = assignedMs
	hop.ConfirmedMs = 1025
	hop.EgressIpHash = [32]byte{3}
	return &hop
}

// Confirming a pending hop may move its assigned_ms forward, anywhere from
// where it was to the confirmation: the hop keeps the moved time, which its
// latency counts from, and the assignment keeps its own.
func TestProviderAttemptHopAssignedMsMayMoveForwardAtConfirmation(t *testing.T) {
	for _, moved := range []uint64{1000, 1010, 1025} {
		confirmed, err := restampTestPending().next(restampTestConfirmed(moved), true)
		if err != nil {
			t.Fatalf("assigned_ms moved to %d at a confirmation at 1025: %v", moved, err)
		}
		if !confirmed.confirmed || confirmed.hop.AssignedMs != moved || confirmed.hop.ConfirmedMs != 1025 {
			t.Fatalf("confirmed hop = %+v, want it confirmed with assigned_ms %d", confirmed.hop, moved)
		}
		if confirmed.assignedMs != 1000 {
			t.Fatalf("the assignment counts at %d after the move to %d, want its own 1000", confirmed.assignedMs, moved)
		}
		// the confirmed hop is seen again on every later receipt of the trail
		again, err := confirmed.next(restampTestConfirmed(moved), true)
		if err != nil || again.assignedMs != 1000 || again.hop.AssignedMs != moved {
			t.Fatalf("the confirmed hop seen again = %+v, %v, want it unchanged", again, err)
		}
	}
}

// A confirmation may not move a pending hop's assigned_ms back.
func TestProviderAttemptHopAssignedMsMovedBackIsRefused(t *testing.T) {
	if _, err := restampTestPending().next(restampTestConfirmed(999), true); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("assigned_ms moved back from 1000 to 999 = %v, want an integrity error", err)
	}
}

// A confirmation may not move a pending hop's assigned_ms past itself.
func TestProviderAttemptHopAssignedMsMovedPastTheConfirmationIsRefused(t *testing.T) {
	if _, err := restampTestPending().next(restampTestConfirmed(1026), true); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("assigned_ms moved to 1026 past the confirmation at 1025 = %v, want an integrity error", err)
	}
}

// The move changes nothing else: a confirmation that also changes another
// field of the pending hop is refused, however valid its assigned_ms.
func TestProviderAttemptHopAssignedMsMoveWithAnotherFieldChangedIsRefused(t *testing.T) {
	other := connect.Id{72}
	for name, change := range map[string]func(*protocol.ProviderAttemptReceiptHop){
		"client_id":     func(hop *protocol.ProviderAttemptReceiptHop) { hop.ClientId = connect.Id{10} },
		"network_id":    func(hop *protocol.ProviderAttemptReceiptHop) { hop.NetworkId = &other },
		"network_issue": func(hop *protocol.ProviderAttemptReceiptHop) { hop.NetworkIssue = "changed" },
		"assign_n":      func(hop *protocol.ProviderAttemptReceiptHop) { hop.AssignN += 1 },
		"seed":          func(hop *protocol.ProviderAttemptReceiptHop) { hop.Seed = true },
	} {
		for _, moved := range []uint64{1000, 1010} {
			hop := restampTestConfirmed(moved)
			change(hop)
			if _, err := restampTestPending().next(hop, true); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
				t.Fatalf("%s changed with assigned_ms at %d = %v, want an integrity error", name, moved, err)
			}
		}
	}
}

// Only the confirmation moves it: a hop still pending, and a hop already
// confirmed, keep the assigned_ms they have.
func TestProviderAttemptHopAssignedMsMovesOnlyAtConfirmation(t *testing.T) {
	stillPending := restampTestPending().hop
	stillPending.AssignedMs = 1010
	if _, err := restampTestPending().next(&stillPending, false); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatalf("a pending hop's assigned_ms moved while pending = %v, want an integrity error", err)
	}
	confirmed, err := restampTestPending().next(restampTestConfirmed(1010), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, moved := range []uint64{1000, 1020} {
		if _, err := confirmed.next(restampTestConfirmed(moved), true); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
			t.Fatalf("a confirmed hop's assigned_ms moved from 1010 to %d = %v, want an integrity error", moved, err)
		}
	}
}

// Through the whole census, over real M8 originals whose Server stamped
// every confirmed hop's assigned_ms 20ms forward, as after a 421: the
// originals are accepted, every confirmation's latency counts from the moved
// time (5ms, not the 25ms since the assignment), and each assignment counts
// at its own time. The first assigned hop of every trail was assigned 5ms
// before the window, so although its moved assigned_ms falls inside the
// window, its assignment is not counted in it; its confirmation is.
func TestProviderAttemptSourceCountsAResentHopAtItsOwnAssignment(t *testing.T) {
	const windowStartMs = uint64(1700000000) * 1000
	providerAttemptSourceTestReceiptTrailEdit = func(trail *protocol.ProviderAttemptReceiptTrail) {
		for index, hop := range trail.Hops {
			if 0 < index {
				hop.AssignedMs += 20
			}
		}
		if len(trail.Hops) == 1 && trail.Pending != nil {
			trail.Pending.AssignedMs = windowStartMs - 5
		}
	}
	defer func() { providerAttemptSourceTestReceiptTrailEdit = nil }()
	fixture := newProviderAttemptSourceTestFixtureWithCompleted(t, true, true)
	var start types.Header
	if err := json.Unmarshal(fixture.response.Authority.StartHeader, &start); err != nil || start.Time*1000 != windowStartMs {
		t.Fatal("the fixture's window does not start where the first hops were assigned before", err)
	}
	_, result, err := fixture.source.Read(t.Context(), fixture.artifact)
	if err != nil || result == nil {
		t.Fatal("originals with assigned_ms moved forward at confirmation were refused", err)
	}
	aMin := result.ReliabilityAMin
	var assignments, confirmations, movedLatency, unmovedLatency uint64
	for _, row := range result.Providers {
		assignments += row.Assignments
		confirmations += row.Confirmations
		movedLatency += row.LatencyBuckets[latencyBucket(5)]
		unmovedLatency += row.LatencyBuckets[latencyBucket(25)]
	}
	// seven assigned hops in each of aMin complete trails and one in the
	// failed trail, less each trail's first, assigned before the window
	if confirmations != 7*aMin || assignments != 6*aMin {
		t.Fatalf("census counted %d assignments and %d confirmations, want %d and %d", assignments, confirmations, 6*aMin, 7*aMin)
	}
	if movedLatency != confirmations || unmovedLatency != 0 {
		t.Fatalf("%d of %d confirmations counted their latency from the moved assigned_ms, %d from the original", movedLatency, confirmations, unmovedLatency)
	}
}

// Through the whole census: originals whose Server moved a confirmed hop's
// assigned_ms back are refused.
func TestProviderAttemptSourceRefusesAssignedMsMovedBack(t *testing.T) {
	providerAttemptSourceTestReceiptTrailEdit = func(trail *protocol.ProviderAttemptReceiptTrail) {
		for index, hop := range trail.Hops {
			if 0 < index {
				hop.AssignedMs -= 1
			}
		}
	}
	defer func() { providerAttemptSourceTestReceiptTrailEdit = nil }()
	fixture := newProviderAttemptSourceTestFixtureWithCompleted(t, true, true)
	if _, result, err := fixture.source.Read(t.Context(), fixture.artifact); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || result != nil {
		t.Fatalf("originals with assigned_ms moved back = %v, want an integrity error", err)
	}
}

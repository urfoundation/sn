// A live open observer and the cold consumer authenticate the same retained
// original clock; neither a SQL window label nor an unsigned artifact supplies it.
package payoutartifact

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// Both the ordinary EVM profile and the declared Frontier millisecond profile
// retain exact raw hashes. The public helper never changes their original bytes.
func TestWholeWorkPublicClockAuthenticatesOriginalBoundaryProfiles(t *testing.T) {
	for _, profile := range []string{"", FrontierWindowClockProfile} {
		fixture := newWholeWorkTestFixture(t)
		clock := fixture.inventory.Clock
		if profile != "" {
			clock.HeaderProfile = profile
			fixture.authority.ClockProfile = profile
			for _, side := range []struct {
				raw      *[]byte
				boundary *Boundary
			}{{raw: &clock.StartHeader, boundary: &clock.Start}, {raw: &clock.EndHeader, boundary: &clock.End}} {
				var header types.Header
				if err := rlp.DecodeBytes(*side.raw, &header); err != nil {
					t.Fatal(err)
				}
				header.Time = header.Time*1000 + 123
				raw, err := rlp.EncodeToBytes(&header)
				if err != nil {
					t.Fatal(err)
				}
				*side.raw, side.boundary.Hash = raw, header.Hash().Hex()
			}
			fixture.authority.Start, fixture.authority.End = clock.Start, clock.End
		}
		beforeStart, beforeEnd := bytes.Clone(clock.StartHeader), bytes.Clone(clock.EndHeader)
		if err := VerifyWholeWorkWindowClock(t.Context(), fixture.authority, clock); err != nil {
			t.Fatal("public observer lost its exact original boundary profile", profile, err)
		}
		if !bytes.Equal(beforeStart, clock.StartHeader) || !bytes.Equal(beforeEnd, clock.EndHeader) {
			t.Fatal("public clock authentication rewrote retained header bytes")
		}
	}
}

// Missing or changed physical headers cannot be replaced by caller times.
// Boundary/profile contradictions and actual cancellation retain their causes.
func TestWholeWorkPublicClockRefusesUnprovedBoundaryAndCancellation(t *testing.T) {
	for _, mutate := range []func(*ClosedWorkWindowClock){
		func(c *ClosedWorkWindowClock) { c.StartHeader = nil },
		func(c *ClosedWorkWindowClock) { c.EndHeader = append(bytes.Clone(c.EndHeader), 0) },
		func(c *ClosedWorkWindowClock) { c.EndTime = c.EndTime.Add(time.Second) },
		func(c *ClosedWorkWindowClock) { c.EndHeader = bytes.Repeat([]byte{1}, 64*1024+1) },
	} {
		fixture := newWholeWorkTestFixture(t)
		mutate(fixture.inventory.Clock)
		if err := VerifyWholeWorkWindowClock(t.Context(), fixture.authority, fixture.inventory.Clock); !errors.Is(err, ErrClosedWorkUnavailable) {
			t.Fatal("declared boundary time replaced missing original headers", err)
		}
	}
	for _, mutate := range []func(*WholeWorkAuthority){
		func(a *WholeWorkAuthority) { a.End.Number++ },
		func(a *WholeWorkAuthority) { a.Start.Hash = a.End.Hash },
		func(a *WholeWorkAuthority) { a.ClockProfile = FrontierWindowClockProfile },
	} {
		fixture := newWholeWorkTestFixture(t)
		mutate(&fixture.authority)
		if err := VerifyWholeWorkWindowClock(t.Context(), fixture.authority, fixture.inventory.Clock); !errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatal("public observer accepted another admitted boundary", err)
		}
	}
	fixture := newWholeWorkTestFixture(t)
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("synthetic original observation owner stopped")
	cancel(cause)
	if err := VerifyWholeWorkWindowClock(ctx, fixture.authority, fixture.inventory.Clock); !errors.Is(err, context.Canceled) || !errors.Is(err, cause) {
		t.Fatal("public clock observation lost its original stop cause", err)
	}
}

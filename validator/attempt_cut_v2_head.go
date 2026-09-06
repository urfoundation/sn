//go:build linux || darwin

package validator

// Compact head evidence retains only prefixes attributable to each provider's
// current eligible fleet. Signed work for another binding generation cannot
// follow a provider into its new fleet. Complete stream replay remains required.

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// Current bindings come from the caller's independently authenticated native
// and coordinator view, not from the candidate cut. Limits are explicit policy
// over provider count and distinct fleet/prefix pairs; no defaults are installed.
// Inputs must remain unchanged during the initial synchronous copy.
type AttemptCutV2HeadOptions struct {
	CurrentBindingKVs map[connect.Id]FleetScoreKey
	MaxProviders      uint64
	MaxFleetPrefixes  uint64
	Replay            AttemptCutV2ReplayOptions
}

// Pre-rendered fixed-width identities avoid encoding them again for every
// historical hop. All fields and the corresponding fleet key are owned values.
type attemptCutV2HeadBinding struct {
	fleetKey FleetScoreKey
	fleetID  string
	hotkey   string
}

// One invocation owns the finite current binding census and distinct prefix
// sets. This projection can share a single replay visitor with other internal
// projections; it is never a substitute for full record and proof verification.
type attemptCutV2HeadProjection struct {
	egressFirstSequence uint64
	maxFleetPrefixes    uint64
	fleetPrefixes       uint64
	currentKVs          map[connect.Id]attemptCutV2HeadBinding
	fleetsKVs           map[FleetScoreKey]map[[32]byte]bool
}

// Admission uses only fixed-width typed keys and independently bounded counts.
// Empty eligible sets are valid: every record still replays but earns no head
// weight. UID zero remains valid; zero client, fleet or hotkey identities do not.
func newAttemptCutV2HeadProjection(ctx context.Context, egressFirstSequence uint64, options AttemptCutV2HeadOptions) (*attemptCutV2HeadProjection, error) {
	if options.MaxProviders == 0 || options.MaxFleetPrefixes == 0 || uint64(len(options.CurrentBindingKVs)) > options.MaxProviders || options.Replay.VisitRecord != nil {
		return nil, errors.New("compact head projection bounds or replay ownership are invalid")
	}
	projection := &attemptCutV2HeadProjection{
		egressFirstSequence: egressFirstSequence, maxFleetPrefixes: options.MaxFleetPrefixes,
		currentKVs: make(map[connect.Id]attemptCutV2HeadBinding, len(options.CurrentBindingKVs)),
		fleetsKVs: make(map[FleetScoreKey]map[[32]byte]bool),
	}
	for clientID, fleetKey := range options.CurrentBindingKVs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if clientID == (connect.Id{}) || fleetKey.FleetID == ([32]byte{}) || fleetKey.Hotkey == ([32]byte{}) || fleetKey.Generation == 0 {
			return nil, errors.New("compact head current binding identity is incomplete")
		}
		projection.currentKVs[clientID] = attemptCutV2HeadBinding{fleetKey: fleetKey, fleetID: attemptHex32(fleetKey.FleetID), hotkey: attemptHex32(fleetKey.Hotkey)}
		if projection.fleetsKVs[fleetKey] == nil {
			projection.fleetsKVs[fleetKey] = map[[32]byte]bool{}
		}
	}
	return projection, nil
}

// The full replay authenticates the record before visiting it. Projection is
// staged until proof EOF/Close succeeds; pending checkpoints and the seed hop
// never contribute. Work for stale, absent or reassigned bindings stays zero.
func (self *attemptCutV2HeadProjection) visitRecord(record AttemptRecord) error {
	if record.Sequence < self.egressFirstSequence || record.Disposition != AttemptDispositionComplete || record.Proof == nil {
		return nil
	}
	for index := 1; index < len(record.Proof.Hops); index++ {
		hop := record.Proof.Hops[index]
		var binding *AttemptBinding
		for assignmentIndex := range record.Assignments {
			assignment := &record.Assignments[assignmentIndex]
			if assignment.NextHop == hop.ClientId {
				binding = &assignment.Binding
				break
			}
		}
		if binding == nil {
			return fmt.Errorf("compact head attempt %d provider %s has no signed binding", record.Sequence, hop.ClientId)
		}
		current, found := self.currentKVs[hop.ClientId]
		if !found || hop.EgressIpHash == ([32]byte{}) || !binding.Active || !binding.UIDFound || binding.FleetID != current.fleetID || binding.Hotkey != current.hotkey || binding.Generation != current.fleetKey.Generation || binding.UID != current.fleetKey.UID {
			continue
		}
		prefixes := self.fleetsKVs[current.fleetKey]
		if !prefixes[hop.EgressIpHash] {
			if self.fleetPrefixes == self.maxFleetPrefixes {
				return errors.New("compact head distinct fleet prefixes exceed their bound")
			}
			self.fleetPrefixes++
			prefixes[hop.EgressIpHash] = true
		}
	}
	return nil
}

// Reconstructs the exact generation-attributed head prefix sets through full
// signed record/proof replay. Historical binding correctness and current native
// eligibility remain independent outer checks, as with the legacy head path.
// No result survives cancellation, missing data or a late proof Close failure.
func ReplayAttemptCutV2HeadWithPolicy(ctx context.Context, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2HeadOptions) (fleets map[FleetScoreKey]map[[32]byte]bool, replayed AttemptCutV2ReplayResult, resultErr error) {
	if ctx == nil {
		return nil, replayed, errors.New("compact head replay context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			fleets, replayed = nil, AttemptCutV2ReplayResult{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, replayed, err
	}
	projection, err := newAttemptCutV2HeadProjection(ctx, expected.EgressFirstSequence, options)
	if err != nil {
		return nil, replayed, err
	}
	replayOptions := options.Replay
	replayOptions.VisitRecord = projection.visitRecord
	replayed, err = ReplayAttemptCutV2WithPolicy(ctx, cut, expected, policy, bounds, replayOptions)
	if err != nil {
		return nil, replayed, err
	}
	return projection.fleetsKVs, replayed, nil
}

// Endpoint replay preserves the original empty baseline and every subsequent
// connection transition before a reservation may claim extender absence.
package protocol

import (
	"context"
	"errors"
)

// A state authenticates one exact prefix. Active extenders require additional
// activation and ownership originals before they can become earning parties.
type ProviderWorkEndpointState struct {
	Head                ProviderWorkEndpointHead
	ObservedAtUnixMicro int64
	ActiveConnections   uint32
	ActiveExtenders     uint32
}

// Return states only for complete, valid prefixes. An unavailable suffix does
// not erase an earlier complete prefix; an integrity error invalidates all.
func ReplayProviderWorkEndpoint(ctx context.Context, authority ProviderWorkSourceAuthority, originals []ProviderWorkReceipt) ([]ProviderWorkEndpointState, error) {
	if ctx == nil {
		return nil, ErrProviderWorkUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := authority.Validate(); err != nil {
		return nil, err
	}
	if len(originals) > int(authority.MaxEndpointEvents) {
		return nil, ErrProviderWorkCapacity
	}
	if len(originals) == 0 {
		return nil, ErrProviderWorkUnavailable
	}
	states := make([]ProviderWorkEndpointState, 0, len(originals))
	activeKVs := map[string]bool{}
	usedKVs := map[string]bool{}
	var previous ProviderWorkEndpointState
	for index, original := range originals {
		if err := VerifyProviderWorkReceiptAuthority(ctx, original, authority); err != nil {
			if errors.Is(err, ErrProviderWorkUnavailable) {
				return states, err
			}
			return nil, err
		}
		event := original.Session
		if event == nil {
			return nil, ErrProviderWorkIntegrity
		}
		if index != 0 && (event.ClientId != previous.Head.ClientId || event.NetworkId != previous.Head.NetworkId) {
			return nil, ErrProviderWorkIntegrity
		}
		if event.Sequence < uint64(index+1) {
			return nil, ErrProviderWorkIntegrity
		}
		if event.Sequence != uint64(index+1) {
			return states, ErrProviderWorkUnavailable
		}
		if event.PreviousHash != previous.Head.HeadHash || index == 0 && event.Kind != "baseline" || index != 0 && event.Kind == "baseline" {
			return nil, ErrProviderWorkIntegrity
		}
		if event.ObservedAtUnixMicro < previous.ObservedAtUnixMicro {
			return states, ErrProviderWorkUnavailable
		}
		state := previous
		switch event.Kind {
		case "baseline":
		case "admit":
			if usedKVs[event.ConnectionId] {
				return nil, ErrProviderWorkIntegrity
			}
			usedKVs[event.ConnectionId] = true
			activeKVs[event.ConnectionId] = event.Extender != nil || event.ExtenderId != ""
			state.ActiveConnections++
			if event.Extender != nil || event.ExtenderId != "" {
				state.ActiveExtenders++
			}
		case "retire":
			extender, exists := activeKVs[event.ConnectionId]
			if !exists {
				return nil, ErrProviderWorkIntegrity
			}
			delete(activeKVs, event.ConnectionId)
			state.ActiveConnections--
			if extender {
				state.ActiveExtenders--
			}
		default:
			return nil, ErrProviderWorkIntegrity
		}
		hash, err := original.ContentHash(ctx)
		if err != nil {
			return nil, err
		}
		state.Head = ProviderWorkEndpointHead{ClientId: event.ClientId, NetworkId: event.NetworkId, Sequence: event.Sequence, HeadHash: hash}
		state.ObservedAtUnixMicro = event.ObservedAtUnixMicro
		states = append(states, state)
		previous = state
	}
	return states, ctx.Err()
}

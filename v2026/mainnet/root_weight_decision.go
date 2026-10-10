// Root weight decisions compare an independently approved basket with one
// finalized existing-seat view. They select an intent, never live authority.
package main

import (
	"errors"
	"slices"
	"sort"
)

const rootWeightObservationSchema = "urnetwork-mainnet-root-weight-observation-v1"
const rootWeightDecisionSchema = "urnetwork-mainnet-root-weight-decision-v1"

// Stored weights are destination netuids, not subnet miner scores or validator indices.
type rootStoredWeight struct {
	Netuid uint16 `json:"netuid"`
	Weight uint16 `json:"weight"`
}

// The separately admitted observer owns finality and exact-block storage trust.
// This view intentionally makes no effective-stake or global-custody assertion.
type rootWeightObservation struct {
	Schema           string                `json:"schema"`
	Position         rootActionObservation `json:"position"`
	Enabled          bool                  `json:"root_weight_setting_enabled"`
	ActiveNetworks   []uint16              `json:"active_networks"`
	StoredWeights    []rootStoredWeight    `json:"stored_weights"`
	LastUpdate       uint64                `json:"last_update"`
	RateLimitBlocks  uint64                `json:"rate_limit_blocks"`
	ConcentrationCap uint16                `json:"concentration_cap_u16"`
	StorageHash      string                `json:"storage_evidence_hash"`
}

// A decision retains its complete typed basis and lifetime observation number.
// The intent outcome still needs the independent action authority before effects.
type rootWeightDecision struct {
	Schema      string                `json:"schema"`
	RequestHash string                `json:"request_hash"`
	Attempt     uint32                `json:"observation_attempt"`
	Observation rootWeightObservation `json:"observation"`
	Outcome     string                `json:"outcome"`
	ContentHash string                `json:"content_hash"`
}

// Matches the pinned I32F32 max-upscale branches, including the divide-first
// branch above 32768 and positive round-to-nearest. All intermediates fit u64.
func rootMaxUpscale(weights []uint16) []uint16 {
	result := make([]uint16, len(weights))
	maximum := uint64(0)
	for _, weight := range weights {
		maximum = max(maximum, uint64(weight))
	}
	if maximum == 0 {
		return result
	}
	for index, weight := range weights {
		var fixed uint64
		if maximum > 32768 {
			fixed = uint64(weight) * ((uint64(65535) << 32) / maximum)
		} else {
			fixed = ((uint64(weight) * 65535) << 32) / maximum
		}
		result[index] = uint16((fixed + (uint64(1) << 31)) >> 32)
	}
	return result
}

// Port callers cannot mutate retained observation vectors after handing them off.
func copyRootWeightObservation(observation rootWeightObservation) rootWeightObservation {
	observation.ActiveNetworks = slices.Clone(observation.ActiveNetworks)
	observation.StoredWeights = slices.Clone(observation.StoredWeights)
	return observation
}

// Validates complete bounded sets, including ignored zero-weight entries. An
// absent field cannot become an empty basket or an invented destination census.
func (self rootWeightObservation) validate(action rootAction) error {
	if err := action.validate(); err != nil {
		return err
	}
	if err := self.Position.matches(action, true); err != nil {
		return err
	}
	if self.Schema != rootWeightObservationSchema || !planSha256(self.StorageHash) || self.LastUpdate > self.Position.FinalizedNumber ||
		self.ActiveNetworks == nil || len(self.ActiveNetworks) == 0 || len(self.ActiveNetworks) > rootCensusLimit ||
		self.StoredWeights == nil || len(self.StoredWeights) > rootCensusLimit {
		return errors.New("root weight view is absent, future-dated or outside its bounds")
	}
	for index, netuid := range self.ActiveNetworks {
		if index == 0 && netuid != 0 || index > 0 && self.ActiveNetworks[index-1] >= netuid {
			return errors.New("root weight view requires sorted unique active networks including root")
		}
	}
	seen := map[uint16]bool{}
	for _, weight := range self.StoredWeights {
		if seen[weight.Netuid] {
			return errors.New("root weight view has duplicate stored destinations")
		}
		seen[weight.Netuid] = true
	}
	return nil
}

// Source-specific guards are necessary conditions only. Stake attribution,
// economic transition approval and inclusion-time custody remain separate gates.
func decideRootWeights(action rootAction, observation rootWeightObservation, attempt uint32) (rootWeightDecision, error) {
	if attempt == 0 || attempt > 10000 {
		return rootWeightDecision{}, errors.New("root weight decision requires a bounded lifetime observation number")
	}
	if err := observation.validate(action); err != nil {
		return rootWeightDecision{}, err
	}
	decision := rootWeightDecision{Schema: rootWeightDecisionSchema, RequestHash: action.RequestHash, Attempt: attempt, Observation: copyRootWeightObservation(observation)}
	stored := make([]rootStoredWeight, 0, len(observation.StoredWeights))
	for _, weight := range observation.StoredWeights {
		if weight.Weight != 0 {
			stored = append(stored, weight)
		}
	}
	sort.Slice(stored, func(i, j int) bool { return stored[i].Netuid < stored[j].Netuid })
	normalized := rootMaxUpscale(action.Weights)
	target := make([]rootStoredWeight, len(action.Dests))
	for index, netuid := range action.Dests {
		target[index] = rootStoredWeight{Netuid: netuid, Weight: normalized[index]}
	}
	decision.Outcome = "intent"
	switch {
	case slices.Equal(stored, target):
		decision.Outcome = "target-observed"
	case !observation.Enabled:
		decision.Outcome = "setter-disabled"
	case observation.LastUpdate != 0 && observation.Position.FinalizedNumber-observation.LastUpdate < observation.RateLimitBlocks:
		decision.Outcome = "rate-limited"
	case len(action.Dests) > len(observation.ActiveNetworks) || len(action.Dests) < min(8, len(observation.ActiveNetworks)):
		decision.Outcome = "destination-count"
	default:
		for _, netuid := range action.Dests {
			if _, found := slices.BinarySearch(observation.ActiveNetworks, netuid); !found {
				decision.Outcome = "destination-unavailable"
				break
			}
		}
		cap := uint64(observation.ConcentrationCap)
		minimum := (uint64(65535) + max(cap, 1) - 1) / max(cap, 1)
		if decision.Outcome == "intent" && uint64(len(observation.ActiveNetworks)) >= minimum {
			sum := uint64(0)
			for _, weight := range action.Weights {
				sum += uint64(weight)
			}
			for _, weight := range action.Weights {
				if uint64(weight)*65535 > cap*sum {
					decision.Outcome = "concentration-cap"
					break
				}
			}
		}
	}
	decision.ContentHash = rootObjectHash(decision)
	return decision, nil
}

// Recompute the decision from retained typed input; a checksum alone is not a
// proof that an action intent followed the supported decision rules.
func (self rootWeightDecision) validate(action rootAction) error {
	expected, err := decideRootWeights(action, self.Observation, self.Attempt)
	if err != nil || self.Schema != expected.Schema || self.RequestHash != expected.RequestHash || self.Outcome != expected.Outcome || self.ContentHash != expected.ContentHash {
		return errors.Join(errors.New("root weight decision differs from its retained basis"), err)
	}
	return nil
}

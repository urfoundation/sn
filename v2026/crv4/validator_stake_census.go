// A bounded complete weighted-stake census supports conservative share bounds.
// Integer runtime API floors are not exact fractional consensus stake weights.
package crv4

import (
	"context"
	"errors"
)

// Index is the current UID at the authenticated block. The runtime-derived
// stake includes parent/child and TAO weighting; raw alpha is not a substitute.
type ValidatorStakeCensusEntry struct {
	Hotkey             [32]byte `json:"hotkey"`
	ValidatorPermit    bool     `json:"validator_permit"`
	TotalStakeFloorRao uint64   `json:"total_stake_floor_rao"`
}

// The selected identity authenticates the query while Entries retains every
// registered slot. Registration generations still require the caller's census.
type ValidatorStakeCensusObservation struct {
	Selected ValidatorStakeObservation   `json:"selected"`
	Entries  []ValidatorStakeCensusEntry `json:"entries"`
}

// Retains no more than 4096 entries and rejects duplicate or absent hotkeys and
// an owner reverse mapping that contradicts the full census. The original
// artifact, exact block, threshold and canonical closing checks are shared
// with the per-validator reader; an error discards the entire projection.
func ReadValidatorStakeCensusAtContext(ctx context.Context, chain *Chain, query ValidatorIdentityQuery, allowed ...RuntimeArtifactIdentity) (ValidatorStakeCensusObservation, error) {
	if len(allowed) > maximumRuntimeMetadataArtifactsPerChain {
		return ValidatorStakeCensusObservation{}, errors.New("validator stake census runtime allowlist exceeds its bound")
	}
	allowed = append([]RuntimeArtifactIdentity(nil), allowed...)
	return readRuntimeObservation(ctx, chain, func(ctx context.Context) (ValidatorStakeCensusObservation, error) {
		return readValidatorStakeCensusAttempt(ctx, chain, query, allowed...)
	})
}

// One attempt preserves the original query and rejects any partial result.
func readValidatorStakeCensusAttempt(ctx context.Context, chain *Chain, query ValidatorIdentityQuery, allowed ...RuntimeArtifactIdentity) (ValidatorStakeCensusObservation, error) {
	empty := ValidatorStakeCensusObservation{}
	if query.MaximumSubnetUIDs == 0 || query.MaximumSubnetUIDs > 4096 {
		return empty, errors.New("validator stake census bound must be within 1..4096")
	}
	var entries []ValidatorStakeCensusEntry
	selected, err := readValidatorStakeAtContext(ctx, chain, query, &entries, allowed...)
	if err != nil {
		return empty, err
	}
	hotkeys := map[[32]byte]bool{}
	for _, entry := range entries {
		if entry.Hotkey == ([32]byte{}) || hotkeys[entry.Hotkey] {
			return empty, errors.New("validator stake census contains absent or duplicate hotkeys")
		}
		hotkeys[entry.Hotkey] = true
	}
	if len(entries) != int(selected.Identity.SubnetUIDs) || selected.SubnetOwnerRegistered &&
		(int(selected.SubnetOwnerUID) >= len(entries) || entries[selected.SubnetOwnerUID].Hotkey != selected.SubnetOwnerHotkey) ||
		selected.SubnetOwnerPresent && !selected.SubnetOwnerRegistered && hotkeys[selected.SubnetOwnerHotkey] {
		return empty, errors.New("validator stake owner registration or complete census differs")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return ValidatorStakeCensusObservation{Selected: selected, Entries: entries}, nil
}

//go:build linux || darwin

package main

import (
	"errors"
	"fmt"
)

func finalNativePayoutBoundaryV2(evidence *FinalSemanticEvidence, epoch uint64) (ChainHead, uint64, error) {
	var head, parent ChainHead
	var nativeEpoch uint64
	for _, validator := range evidence.Validators {
		checkpoint, err := finalNativePayoutCheckpointV2(evidence, validator.ValidatorID, epoch)
		if err != nil {
			return ChainHead{}, 0, err
		}
		if head == (ChainHead{}) {
			head, parent, nativeEpoch = checkpoint.PayoutHead, checkpoint.PayoutParent, checkpoint.Identity.SubnetEpochIndex
		} else if head != checkpoint.PayoutHead || parent != checkpoint.PayoutParent || nativeEpoch != checkpoint.Identity.SubnetEpochIndex {
			return ChainHead{}, 0, errors.New("settlement native payout census has conflicting validator clocks")
		}
		if _, err := finalCoverageCycleAtV2(evidence, validator.ValidatorID, head.Number); err != nil {
			return ChainHead{}, 0, err
		}
	}
	if verifyFinalHead("actual native payout boundary", head) != nil || parent.Number+1 != head.Number || nativeEpoch == 0 {
		return ChainHead{}, 0, errors.New("settlement lacks an actual native payout boundary")
	}
	return head, nativeEpoch, nil
}

func finalSemanticRewardCycles(evidence *FinalSemanticEvidence, epoch uint64) ([]*FinalCRv4Cycle, error) {
	var result []*FinalCRv4Cycle
	if len(evidence.ValidatorReplayV2) != 0 {
		head, _, err := finalNativePayoutBoundaryV2(evidence, epoch)
		if err != nil {
			return nil, err
		}
		for _, validator := range evidence.Validators {
			cycle, err := finalCoverageCycleAtV2(evidence, validator.ValidatorID, head.Number)
			if err != nil {
				return nil, err
			}
			result = append(result, cycle)
		}
		return result, nil
	}
	for index := range evidence.Validators {
		validator := &evidence.Validators[index]
		found := false
		for index := range validator.Cycles {
			if validator.Cycles[index].SettlementEpoch == epoch {
				if found {
					return nil, errors.New("native reward repeats a legacy settlement decision")
				}
				found = true
				result = append(result, &validator.Cycles[index])
			}
		}
		if !found {
			return nil, fmt.Errorf("validator %d lacks cycle for reward epoch %d", validator.ValidatorID, epoch)
		}
	}
	return result, nil
}

// Lifecycle ownership changes at the canonical registration extrinsic, while
// acceptance labels use the independent settlement clock. Reuse the verified
// historical role mapping at the actual native payout block.
func finalNativeRewardOwnershipEpochV2(evidence *FinalSemanticEvidence, epoch uint64) (uint64, error) {
	if len(evidence.ValidatorReplayV2) == 0 || evidence.FleetLifecycle == nil {
		return epoch, nil
	}
	head, _, err := finalNativePayoutBoundaryV2(evidence, epoch)
	if err != nil {
		return 0, err
	}
	state := &evidence.FleetLifecycle.State
	selected := state.TakeoverEffectiveEpoch
	previous := uint64(0)
	for _, item := range []struct {
		epoch        uint64
		registration *FleetLifecycleRegistrationEvidence
	}{
		{state.FallbackEffectiveEpoch, state.FallbackRegistration},
		{state.ProviderEffectiveEpoch, state.ProviderRegistration},
		{state.TerminalEffectiveEpoch, state.TerminalRegistration},
	} {
		if item.registration == nil || item.registration.BlockNumber <= previous {
			return 0, errors.New("native reward lifecycle registration order is incomplete")
		}
		previous = item.registration.BlockNumber
		if head.Number >= item.registration.BlockNumber {
			selected = item.epoch
		}
	}
	return selected, nil
}

func (a *finalSemanticArchive) finalNativeRewardPairV2(source *FinalSemanticEvidence, epoch uint64) (*NativeRewardObservation, *NativeRewardObservation, error) {
	head, _, err := finalNativePayoutBoundaryV2(source, epoch)
	if err != nil {
		return nil, nil, err
	}
	var before, after *NativeRewardObservation
	for _, validator := range source.Validators {
		checkpoint, err := finalNativePayoutCheckpointV2(source, validator.ValidatorID, epoch)
		if err != nil {
			return nil, nil, err
		}
		owner := a.validatorReplayV2[validator.ValidatorID]
		if owner == nil || owner.nativeRewards[head] == nil || owner.nativeRewards[checkpoint.PayoutParent] == nil {
			return nil, nil, errors.New("exact native payout or parent source is absent")
		}
		if before == nil {
			before, after = owner.nativeRewards[checkpoint.PayoutParent], owner.nativeRewards[head]
		} else if !finalJSONEqual(before, owner.nativeRewards[checkpoint.PayoutParent]) || !finalJSONEqual(after, owner.nativeRewards[head]) {
			return nil, nil, errors.New("independent captured native payout sources conflict")
		}
	}
	if !finalSemanticRewardSnapshotValid(source, after, epoch) {
		return nil, nil, errors.New("actual native payout channels differ from active signed application intervals")
	}
	return before, after, nil
}

func finalNativeRewardEVMHeadV2(evidence *FinalSemanticEvidence, native ChainHead) (ChainHead, error) {
	var result ChainHead
	for _, entry := range evidence.ValidatorReplayV2 {
		if entry.Coverage == nil {
			return ChainHead{}, errors.New("native reward mapping owner is absent")
		}
		found := false
		for _, mapping := range entry.Coverage.RewardMappings {
			if mapping.Query.NativeNumber != native.Number {
				continue
			}
			if found || mapping.Query.NativeHash.Hex() != native.Hash {
				return ChainHead{}, errors.New("native reward maps conflicting native identities")
			}
			head := ChainHead{Number: mapping.Query.EVMNumber, Hash: mapping.Query.EVMHash.Hex()}
			if result != (ChainHead{}) && result != head {
				return ChainHead{}, errors.New("native reward independent EVM mappings conflict")
			}
			result, found = head, true
		}
		if !found {
			return ChainHead{}, errors.New("native reward has no authenticated EVM execution mapping")
		}
	}
	if verifyFinalHead("native reward mapped EVM", result) != nil {
		return ChainHead{}, errors.New("native reward mapped EVM head is incomplete")
	}
	return result, nil
}

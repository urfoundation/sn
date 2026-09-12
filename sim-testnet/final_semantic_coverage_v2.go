//go:build linux || darwin

package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// Settlement and native clocks are independent. Every settlement endpoint is
// retained; each application interval and commit is derived from the complete
// original signed history, then independently reproduced from canonical RPC.
type FinalNativeCoverageV2 struct {
	RewardMappings []crv4.EVMCheckpointObservation    `json:"reward_mappings"`
	Checkpoints    []FinalNativeCheckpointV2          `json:"checkpoints"`
	Intervals      []FinalNativeApplicationIntervalV2 `json:"application_intervals"`
	Commits        []FinalNativeCoverageCommitV2      `json:"commits"`
}

type FinalNativeApplicationIntervalV2 struct {
	MeasurementHash   string `json:"measurement_hash"`
	FromBlock         uint64 `json:"from_native_block"`
	ThroughBlock      uint64 `json:"through_native_block"`
	CommitNativeEpoch uint64 `json:"commit_native_epoch"`
	RevealNativeEpoch uint64 `json:"reveal_native_epoch"`
}

type FinalNativeCoverageCommitV2 struct {
	MeasurementHash string    `json:"measurement_hash"`
	Block           ChainHead `json:"block"`
	NativeEpoch     uint64    `json:"native_epoch"`
}

func finalCoverageForValidatorV2(evidence *FinalSemanticEvidence, id uint64) *FinalNativeCoverageV2 {
	if evidence != nil {
		for index := range evidence.ValidatorReplayV2 {
			if evidence.ValidatorReplayV2[index].ValidatorID == id {
				return evidence.ValidatorReplayV2[index].Coverage
			}
		}
	}
	return nil
}

func deriveFinalNativeCoverageV2(intents []validatorpkg.SteeringIntent, observations []validatorpkg.ReleaseEvidenceV2DecisionObservation, checkpoints []FinalNativeCheckpointV2) (*FinalNativeCoverageV2, error) {
	if len(intents) == 0 || len(intents) != len(observations) || len(checkpoints) < 2 {
		return nil, errors.New("native coverage original history or endpoints are incomplete")
	}
	captured := make([]validatorpkg.ReleaseEvidenceV2CapturedIntent, len(intents))
	for index, intent := range intents {
		if observations[index].MeasurementHash != intent.MeasurementArtifactHash {
			return nil, errors.New("native coverage decision observations differ from original order")
		}
		captured[index] = validatorpkg.ReleaseEvidenceV2CapturedIntent{Sequence: uint64(index + 1), Intent: intent}
	}
	selected, err := selectFinalCoverageIntentsV2(captured, checkpoints)
	if err != nil {
		return nil, err
	}
	result := &FinalNativeCoverageV2{Checkpoints: slices.Clone(checkpoints)}
	last := checkpoints[len(checkpoints)-1].Mapping.Query.NativeNumber
	firstCommit := ^uint64(0)
	lastCommit := last
	for index, intent := range intents {
		if !selected[uint64(index+1)] {
			continue
		}
		observation := observations[index]
		if intent.FinalizedBlock == 0 || observation.CommitNativeEpoch == 0 || observation.RevealNativeEpoch == 0 || observation.ApplicationNativeEpoch < observation.RevealNativeEpoch {
			return nil, errors.New("native coverage application lacks actual lifecycle epoch observations")
		}
		through := last
		for _, next := range intents[index+1:] {
			if next.Status == "applied" {
				through = min(through, next.RevealBlock-1)
				break
			}
		}
		result.Intervals = append(result.Intervals, FinalNativeApplicationIntervalV2{MeasurementHash: intent.MeasurementArtifactHash, FromBlock: intent.RevealBlock, ThroughBlock: through, CommitNativeEpoch: observation.CommitNativeEpoch, RevealNativeEpoch: observation.RevealNativeEpoch})
		firstCommit = min(firstCommit, intent.FinalizedBlock)
		lastCommit = max(lastCommit, intent.ApplicationBlock)
	}
	for index, intent := range intents {
		if intent.FinalizedBlock < firstCommit || intent.FinalizedBlock > lastCommit {
			continue
		}
		if observations[index].CommitNativeEpoch == 0 {
			return nil, errors.New("native coverage finalized commit lacks its observed native epoch")
		}
		result.Commits = append(result.Commits, FinalNativeCoverageCommitV2{MeasurementHash: intent.MeasurementArtifactHash, Block: ChainHead{Number: intent.FinalizedBlock, Hash: strings.ToLower(intent.FinalizedBlockHash)}, NativeEpoch: observations[index].CommitNativeEpoch})
	}
	return result, nil
}

func finalCoverageIntervalAtV2(coverage *FinalNativeCoverageV2, block uint64) (*FinalNativeApplicationIntervalV2, error) {
	if coverage == nil {
		return nil, errors.New("native coverage is absent")
	}
	var result *FinalNativeApplicationIntervalV2
	for index := range coverage.Intervals {
		interval := &coverage.Intervals[index]
		if interval.FromBlock <= block && block <= interval.ThroughBlock {
			if result != nil {
				return nil, errors.New("native coverage has conflicting application intervals")
			}
			result = interval
		}
	}
	if result == nil {
		return nil, fmt.Errorf("native coverage has no application interval at block %d", block)
	}
	return result, nil
}

func finalCoverageCycleAtV2(evidence *FinalSemanticEvidence, validatorID, block uint64) (*FinalCRv4Cycle, error) {
	interval, err := finalCoverageIntervalAtV2(finalCoverageForValidatorV2(evidence, validatorID), block)
	if err != nil {
		return nil, err
	}
	validator := finalValidatorByID(evidence, validatorID)
	if validator == nil {
		return nil, errors.New("native coverage validator identity is absent")
	}
	for index := range validator.Cycles {
		if validator.Cycles[index].MeasurementArtifact.ContentHash == interval.MeasurementHash {
			return &validator.Cycles[index], nil
		}
	}
	return nil, errors.New("native coverage interval has no complete signed decision")
}

func finalNativeCoverageFirstBlockV2(checkpoints []FinalNativeCheckpointV2) uint64 {
	if len(checkpoints) == 0 {
		return 0
	}
	first := checkpoints[0].Mapping.Query.NativeNumber
	for _, checkpoint := range checkpoints[1:] {
		if checkpoint.PayoutHead.Number != 0 {
			first = min(first, checkpoint.PayoutHead.Number)
		}
	}
	return first
}

func finalCoverageBaselineCycleV2(evidence *FinalSemanticEvidence, id uint64, cycle *FinalCRv4Cycle) bool {
	coverage := finalCoverageForValidatorV2(evidence, id)
	if coverage == nil || len(coverage.Checkpoints) == 0 || cycle == nil {
		return false
	}
	for _, interval := range coverage.Intervals {
		if interval.MeasurementHash == cycle.MeasurementArtifact.ContentHash && interval.FromBlock == cycle.Reveal.Block.Number && interval.FromBlock <= coverage.Checkpoints[0].Mapping.Query.NativeNumber && interval.ThroughBlock >= finalNativeCoverageFirstBlockV2(coverage.Checkpoints) {
			return true
		}
	}
	return false
}

func finalNativePayoutCheckpointV2(evidence *FinalSemanticEvidence, id, epoch uint64) (*FinalNativeCheckpointV2, error) {
	coverage := finalCoverageForValidatorV2(evidence, id)
	if coverage == nil || epoch < evidence.Window.FirstEpoch || epoch-evidence.Window.FirstEpoch >= evidence.Window.EpochCount || uint64(len(coverage.Checkpoints)) != evidence.Window.EpochCount+1 {
		return nil, errors.New("native payout lacks its complete settlement checkpoint census")
	}
	return &coverage.Checkpoints[epoch-evidence.Window.FirstEpoch+1], nil
}

// LastUpdate is written by commit, independently of the applied row. Runtime
// 67dcf7f reveal retry remains legal only within the current reveal epoch. A
// previous-epoch row therefore needs the canonical signed commit for today's
// target; it cannot silently stand in for a missing epoch of validator work.
func verifyFinalCoverageFreshnessV2(coverage *FinalNativeCoverageV2, checkpoint FinalNativeCheckpointV2, active *FinalNativeApplicationIntervalV2) error {
	number, epoch := checkpoint.Mapping.Query.NativeNumber, checkpoint.Identity.SubnetEpochIndex
	if active == nil || checkpoint.RevealPeriodEpochs == 0 || epoch == 0 {
		return errors.New("native coverage freshness inputs are incomplete")
	}
	reveal, ok := checkedAdd(active.CommitNativeEpoch, checkpoint.RevealPeriodEpochs)
	if !ok || active.RevealNativeEpoch != reveal || reveal > epoch || epoch-reveal > 1 {
		return errors.New("native coverage applied interval is stale or has conflicting commit/reveal epochs")
	}
	var latest *FinalNativeCoverageCommitV2
	retryTarget := false
	for index := range coverage.Commits {
		commit := &coverage.Commits[index]
		if commit.Block.Number > number {
			break
		}
		latest = commit
		if epoch >= checkpoint.RevealPeriodEpochs && commit.NativeEpoch == epoch-checkpoint.RevealPeriodEpochs {
			retryTarget = true
		}
	}
	if latest == nil || latest.Block.Number != checkpoint.Weights.LastUpdate || latest.NativeEpoch > epoch {
		return errors.New("native coverage LastUpdate differs from latest canonical signed commit")
	}
	if epoch != reveal && !retryTarget {
		return errors.New("native coverage old row lacks the current reveal epoch's canonical retry commitment")
	}
	return nil
}

func verifyFinalNativeCoverageV2(evidence *FinalSemanticEvidence, validator *FinalValidatorIdentityEvidence, coverage *FinalNativeCoverageV2) error {
	if evidence == nil || validator == nil || coverage == nil {
		return errors.New("native coverage owner is incomplete")
	}
	blocks, err := finalNativeCoverageEVMBlocksV2(evidence.Window)
	if err != nil || len(coverage.Checkpoints) != len(blocks) || len(coverage.Intervals) == 0 || len(coverage.Intervals) != len(validator.Cycles) || len(coverage.Commits) == 0 {
		return errors.Join(errors.New("native coverage endpoint, interval or commit census is incomplete"), err)
	}
	first := finalNativeCoverageFirstBlockV2(coverage.Checkpoints)
	last := coverage.Checkpoints[len(coverage.Checkpoints)-1].Mapping.Query.NativeNumber
	if first == 0 || last <= first {
		return errors.New("native coverage mapped window is not increasing")
	}
	for index, interval := range coverage.Intervals {
		cycle := &validator.Cycles[index]
		if requireFinalSHA256("native coverage measurement", interval.MeasurementHash) != nil || interval.MeasurementHash != cycle.MeasurementArtifact.ContentHash || interval.FromBlock != cycle.Reveal.Block.Number || interval.FromBlock > interval.ThroughBlock || interval.ThroughBlock > last || interval.CommitNativeEpoch == 0 || interval.RevealNativeEpoch <= interval.CommitNativeEpoch {
			return errors.New("native coverage interval differs from its complete signed cycle")
		}
		if index == 0 {
			if interval.FromBlock > first || interval.ThroughBlock < first {
				return errors.New("native coverage lacks the active baseline interval")
			}
		} else {
			prior := coverage.Intervals[index-1]
			nextBlock, blockOK := checkedAdd(prior.ThroughBlock, 1)
			nextEpoch, epochOK := checkedAdd(prior.RevealNativeEpoch, 1)
			if !blockOK || !epochOK || interval.FromBlock != nextBlock || interval.RevealNativeEpoch != nextEpoch {
				return errors.New("native coverage is missing a consecutive application interval or native epoch")
			}
		}
	}
	if coverage.Intervals[len(coverage.Intervals)-1].ThroughBlock != last {
		return errors.New("native coverage does not reach the final settlement endpoint")
	}
	seen := map[string]bool{}
	for index, commit := range coverage.Commits {
		if requireFinalSHA256("native coverage commit measurement", commit.MeasurementHash) != nil || verifyFinalHead("native coverage commit", commit.Block) != nil || commit.NativeEpoch == 0 || commit.Block.Number > evidence.NativeTerminalHead.Number || seen[commit.MeasurementHash] || index > 0 && (commit.Block.Number <= coverage.Commits[index-1].Block.Number || commit.NativeEpoch < coverage.Commits[index-1].NativeEpoch) {
			return errors.New("native coverage commits are incomplete, duplicated or non-canonical")
		}
		seen[commit.MeasurementHash] = true
	}
	requiredMappings := map[ChainHead]bool{}
	for _, checkpoint := range coverage.Checkpoints[1:] {
		requiredMappings[checkpoint.PayoutHead] = true
		requiredMappings[checkpoint.PayoutParent] = true
	}
	if len(requiredMappings) != len(coverage.RewardMappings) {
		return errors.New("native payout mapping census is incomplete")
	}
	for index, mapping := range coverage.RewardMappings {
		native := ChainHead{Number: mapping.Query.NativeNumber, Hash: mapping.Query.NativeHash.Hex()}
		if !requiredMappings[native] || index > 0 && mapping.Query.NativeNumber <= coverage.RewardMappings[index-1].Query.NativeNumber || !strings.EqualFold(mapping.Query.GenesisHash.Hex(), evidence.GenesisHash) || mapping.Runtime != coverage.Checkpoints[0].Mapping.Runtime || mapping.ParentRuntime != mapping.Runtime || verifyFinalHead("native payout mapped EVM", ChainHead{Number: mapping.Query.EVMNumber, Hash: mapping.Query.EVMHash.Hex()}) != nil {
			return errors.New("native payout mapping changes a source runtime, native identity or canonical census")
		}
		delete(requiredMappings, native)
	}
	hotkey, err := finalNativeAccountHex(validator.Hotkey)
	if err != nil {
		return err
	}
	for index, checkpoint := range coverage.Checkpoints {
		mapping, identity := checkpoint.Mapping.Query, checkpoint.Identity.Stake.Identity
		native := ChainHead{Number: mapping.NativeNumber, Hash: mapping.NativeHash.Hex()}
		if mapping.EVMNumber != blocks[index] || verifyFinalHead("native coverage EVM", ChainHead{Number: mapping.EVMNumber, Hash: mapping.EVMHash.Hex()}) != nil || verifyFinalHead("native coverage native", native) != nil || native.Number < evidence.NativeStartHead.Number || native.Number > evidence.NativeTerminalHead.Number || !strings.EqualFold(mapping.GenesisHash.Hex(), evidence.GenesisHash) || index > 0 && native.Number <= coverage.Checkpoints[index-1].Mapping.Query.NativeNumber {
			return errors.New("native coverage checkpoint differs from the independent window or chain")
		}
		if identity.GenesisHash != mapping.GenesisHash || identity.BlockHash != mapping.NativeHash || identity.BlockNumber != mapping.NativeNumber || identity.Netuid != evidence.Netuid || identity.UID != validator.UID || fmt.Sprintf("0x%x", identity.Hotkey) != hotkey || !checkpoint.Identity.Stake.MeetsNonSelfStakeAndPermit() || identity.Runtime != checkpoint.Mapping.Runtime || checkpoint.Mapping.ParentRuntime != checkpoint.Mapping.Runtime || checkpoint.Schedule.CurrentBlock != mapping.NativeNumber || checkpoint.Schedule.SubnetEpochIndex != checkpoint.Identity.SubnetEpochIndex || checkpoint.Schedule.Tempo == 0 || checkpoint.Weights.Block != native || checkpoint.Weights.ValidatorUID != validator.UID || checkpoint.Weights.ValidatorHotkey != hotkey {
			return errors.New("native coverage schedule, runtime, signer or applied-row owner differs")
		}
		if checkpoint.PayoutHead.Number != checkpoint.Schedule.LastEpochBlock || checkpoint.PayoutHead.Number > native.Number || checkpoint.PayoutHead.Number <= 1 || checkpoint.PayoutParent.Number+1 != checkpoint.PayoutHead.Number || verifyFinalHead("native coverage payout", checkpoint.PayoutHead) != nil || verifyFinalHead("native coverage payout parent", checkpoint.PayoutParent) != nil || identity.FinalizedNumber < native.Number {
			return errors.New("native coverage latest payout or its exact parent differs from schedule")
		}
		interval, err := finalCoverageIntervalAtV2(coverage, native.Number)
		if err != nil {
			return err
		}
		cycle, err := finalCoverageCycleAtV2(evidence, validator.ValidatorID, native.Number)
		if err != nil {
			return err
		}
		uids, values := finalSubmittedValues(cycle.Submitted)
		if !slices.Equal(uids, checkpoint.Weights.UIDs) || !slices.Equal(values, checkpoint.Weights.Values) {
			return errors.New("native coverage full applied row differs from its active signed interval")
		}
		if err := verifyFinalCoverageFreshnessV2(coverage, checkpoint, interval); err != nil {
			return err
		}
	}
	return nil
}

// Historical state is immutable, but a later independent verification observes
// a newer finalized tip. Both readers prove finality; only that live bookkeeping
// is removed from the exact immutable observation comparison.
func finalNativeCheckpointEqualV2(left, right FinalNativeCheckpointV2) bool {
	for _, checkpoint := range []FinalNativeCheckpointV2{left, right} {
		identity := checkpoint.Identity.Stake.Identity
		if identity.FinalizedNumber < checkpoint.Mapping.Query.NativeNumber || requireFinalHex32("native checkpoint finality", identity.FinalizedHash.Hex()) != nil {
			return false
		}
	}
	left.Identity.Stake.Identity.FinalizedNumber = 0
	right.Identity.Stake.Identity.FinalizedNumber = 0
	left.Identity.Stake.Identity.FinalizedHash = right.Identity.Stake.Identity.FinalizedHash
	return finalJSONEqual(left, right)
}

func verifyFinalNativeApplicationV2(evidence *FinalSemanticEvidence, validatorID uint64, cycle *FinalCRv4Cycle, state FinalNativeWeightState) error {
	coverage := finalCoverageForValidatorV2(evidence, validatorID)
	validator := finalValidatorByID(evidence, validatorID)
	if coverage == nil || validator == nil || cycle == nil || cycle.Application.Call == nil {
		return errors.New("native application lacks its original V2 coverage owner")
	}
	var latest uint64
	for _, commit := range coverage.Commits {
		if commit.Block.Number <= cycle.Application.Block.Number {
			latest = max(latest, commit.Block.Number)
		}
	}
	uids, values := finalSubmittedValues(cycle.Submitted)
	return verifyFinalNativeApplicationWithLatestCommit(state, *cycle.Application.Call, cycle.Application.Block, validator.UID, validator.Hotkey, uids, values, latest)
}

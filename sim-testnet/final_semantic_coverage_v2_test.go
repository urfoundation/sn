//go:build linux || darwin

package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

func finalCoverageTestHashV2(number uint64) types.Hash {
	return types.Hash(common.BigToHash(new(big.Int).SetUint64(number)))
}

// Actual 360-block native cadence crosses five 300-block settlement windows.
// The original decision counter, commit counter, reveal and EVM hashes remain
// distinct. This fixture supplies observations to the pure consumer; the real
// HTTP/SCALE reader and complete signed archive have separate regression roots.
func finalCoverageTestEvidenceV2(t *testing.T, firstReveal uint64) (*FinalSemanticEvidence, []validatorpkg.SteeringIntent, []validatorpkg.ReleaseEvidenceV2DecisionObservation) {
	t.Helper()
	source := &FinalSemanticEvidence{GenesisHash: finalCoverageTestHashV2(1).Hex(), Netuid: 521, ExpectedValidators: 2,
		NativeStartHead: ChainHead{Number: 100, Hash: finalCoverageTestHashV2(100).Hex()}, NativeTerminalHead: ChainHead{Number: 2600, Hash: finalCoverageTestHashV2(2600).Hex()},
		Window: ScenarioAcceptanceWindow{FirstEpoch: 10, EpochCount: 5, EpochBlocks: 300, StartBlock: 1000, EndBlock: 2500}}
	runtime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1, StateVersion: 1}, CodeHash: finalCoverageTestHashV2(20).Hex(), MetadataHash: finalCoverageTestHashV2(21).Hex()}
	mapping := func(number uint64) crv4.EVMCheckpointObservation {
		return crv4.EVMCheckpointObservation{Query: crv4.EVMCheckpointQuery{GenesisHash: finalCoverageTestHashV2(1), NativeNumber: number, NativeHash: finalCoverageTestHashV2(number), EVMNumber: number, EVMHash: finalCoverageTestHashV2(number + 100000)}, NativeParentHash: finalCoverageTestHashV2(number - 1), Runtime: runtime, ParentRuntime: runtime}
	}
	var original []validatorpkg.SteeringIntent
	var observations []validatorpkg.ReleaseEvidenceV2DecisionObservation
	for index := uint64(0); firstReveal+index*360-350 <= 2499; index++ {
		reveal := firstReveal + index*360
		status := "applied"
		application := reveal + 2
		if reveal > 2499 {
			status = "finalized"
			application = 0
		}
		intent := validatorpkg.SteeringIntent{ValidatorID: 1, SelfUID: 1, Status: status, SubnetEpoch: 99 + index, SettlementEpoch: 7 + index,
			MeasurementArtifactHash: fmt.Sprintf("sha256:%064x", index+1), RevealBlock: reveal, ApplicationBlock: application,
			FinalizedBlock: reveal - 350, FinalizedBlockHash: finalCoverageTestHashV2(reveal - 350).Hex(), UIDs: []uint16{10, 11}, Values: []uint16{uint16(30000 + index), uint16(35535 - index)}}
		observation := validatorpkg.ReleaseEvidenceV2DecisionObservation{MeasurementHash: intent.MeasurementArtifactHash, CommitNativeEpoch: 99 + index}
		if status == "applied" {
			observation.RevealNativeEpoch = 100 + index
			observation.ApplicationNativeEpoch = 100 + index
		}
		original = append(original, intent)
		observations = append(observations, observation)
	}
	blocks, err := finalNativeCoverageEVMBlocksV2(source.Window)
	if err != nil {
		t.Fatal(err)
	}
	for id := uint64(1); id <= 2; id++ {
		hotkey := finalCoverageTestHashV2(10000 + id)
		validator := FinalValidatorIdentityEvidence{ValidatorID: id, UID: uint16(id), Hotkey: hotkey.Hex()}
		var checkpoints []FinalNativeCheckpointV2
		for _, block := range blocks {
			applied := (block - firstReveal) / 360
			payout := firstReveal + applied*360
			latest := original[0].FinalizedBlock
			for _, intent := range original {
				if intent.FinalizedBlock <= block {
					latest = intent.FinalizedBlock
				}
			}
			checkpoint := FinalNativeCheckpointV2{Mapping: mapping(block), RevealPeriodEpochs: 1,
				Schedule:   crv4.EpochScheduleState{CurrentBlock: block, LastEpochBlock: payout, SubnetEpochIndex: 100 + applied, Tempo: 360},
				PayoutHead: ChainHead{Number: payout, Hash: finalCoverageTestHashV2(payout).Hex()}, PayoutParent: ChainHead{Number: payout - 1, Hash: finalCoverageTestHashV2(payout - 1).Hex()},
				Weights: FinalNativeWeightState{ValidatorUID: uint16(id), ValidatorHotkey: hotkey.Hex(), LastUpdate: latest, UIDs: slices.Clone(original[applied].UIDs), Values: slices.Clone(original[applied].Values), Block: ChainHead{Number: block, Hash: finalCoverageTestHashV2(block).Hex()}}}
			checkpoint.Identity = crv4.ValidatorScheduleObservation{SubnetEpochIndex: 100 + applied, Stake: crv4.ValidatorStakeObservation{TotalStakeRao: 10, StakeThresholdRao: 1, Identity: crv4.ValidatorIdentityObservation{GenesisHash: finalCoverageTestHashV2(1), BlockNumber: block, BlockHash: finalCoverageTestHashV2(block), FinalizedNumber: 2600, FinalizedHash: finalCoverageTestHashV2(2600), Netuid: 521, UID: uint16(id), Hotkey: [32]byte(hotkey), ValidatorPermit: true, Runtime: runtime}}}
			checkpoints = append(checkpoints, checkpoint)
		}
		coverage, err := deriveFinalNativeCoverageV2(original, observations, checkpoints)
		if err != nil {
			t.Fatal(err)
		}
		needed := map[uint64]bool{}
		for _, point := range checkpoints[1:] {
			needed[point.PayoutHead.Number] = true
			needed[point.PayoutParent.Number] = true
		}
		var numbers []uint64
		for n := range needed {
			numbers = append(numbers, n)
		}
		slices.Sort(numbers)
		for _, n := range numbers {
			coverage.RewardMappings = append(coverage.RewardMappings, mapping(n))
		}
		for _, interval := range coverage.Intervals {
			for _, intent := range original {
				if intent.MeasurementArtifactHash == interval.MeasurementHash {
					validator.Cycles = append(validator.Cycles, FinalCRv4Cycle{SettlementEpoch: intent.SettlementEpoch, SubnetEpoch: intent.SubnetEpoch, MeasurementArtifact: FinalArtifactLocator{ContentHash: intent.MeasurementArtifactHash}, Reveal: FinalNativeReceipt{Block: ChainHead{Number: intent.RevealBlock, Hash: finalCoverageTestHashV2(intent.RevealBlock).Hex()}}, Application: FinalNativeReceipt{Block: ChainHead{Number: intent.ApplicationBlock, Hash: finalCoverageTestHashV2(intent.ApplicationBlock).Hex()}}, Submitted: []FinalSubmittedWeight{{UID: 10, Value: intent.Values[0]}, {UID: 11, Value: intent.Values[1]}}})
				}
			}
		}
		source.Validators = append(source.Validators, validator)
		source.ValidatorReplayV2 = append(source.ValidatorReplayV2, FinalValidatorReplayV2{ValidatorID: id, Coverage: coverage})
	}
	return source, original, observations
}

func TestFinalNativeCoverageV2UsesActualIntervalsAcrossIndependentClocks(t *testing.T) {
	source, _, _ := finalCoverageTestEvidenceV2(t, 660)
	if len(source.Validators[0].Cycles) != 6 || source.Window.EpochCount != 5 {
		t.Fatal("fixture lost unequal clock census")
	}
	for index := range source.Validators {
		if err := verifyFinalNativeCoverageV2(source, &source.Validators[index], source.ValidatorReplayV2[index].Coverage); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint := source.ValidatorReplayV2[0].Coverage.Checkpoints[0]
	if checkpoint.Weights.LastUpdate == source.ValidatorReplayV2[0].Coverage.Commits[0].Block.Number {
		t.Fatal("fixture lost pending newer commit")
	}
	if !finalCoverageBaselineCycleV2(source, 1, &source.Validators[0].Cycles[0]) {
		t.Fatal("original active baseline was not retained")
	}
}

func TestFinalNativeCoverageV2RejectsMissingStaleAndConflictingHistory(t *testing.T) {
	for _, mutation := range []string{"missing-interval", "wrong-row", "wrong-latest-commit", "stale-epoch", "conflicting-runtime", "wrong-evm-map", "missing-payout-parent", "same-height-wrong-hash"} {
		t.Run(mutation, func(t *testing.T) {
			source, _, _ := finalCoverageTestEvidenceV2(t, 660)
			coverage := source.ValidatorReplayV2[0].Coverage
			switch mutation {
			case "missing-interval":
				coverage.Intervals = append(coverage.Intervals[:2], coverage.Intervals[3:]...)
			case "wrong-row":
				coverage.Checkpoints[2].Weights.Values[0]++
			case "wrong-latest-commit":
				coverage.Checkpoints[1].Weights.LastUpdate--
			case "stale-epoch":
				coverage.Checkpoints[1].Identity.SubnetEpochIndex += 2
				coverage.Checkpoints[1].Schedule.SubnetEpochIndex += 2
			case "conflicting-runtime":
				coverage.Checkpoints[1].Mapping.Runtime.CodeHash = finalCoverageTestHashV2(90).Hex()
			case "wrong-evm-map":
				coverage.Checkpoints[1].Mapping.Query.EVMNumber++
			case "missing-payout-parent":
				coverage.RewardMappings = coverage.RewardMappings[1:]
			case "same-height-wrong-hash":
				coverage.RewardMappings[0].Query.NativeHash = finalCoverageTestHashV2(999)
			}
			if err := verifyFinalNativeCoverageV2(source, &source.Validators[0], coverage); err == nil {
				t.Fatal("changed source admitted")
			}
		})
	}
}

func TestFinalNativeCoverageV2RetryNeedsActualTargetCommit(t *testing.T) {
	source, _, _ := finalCoverageTestEvidenceV2(t, 660)
	coverage := source.ValidatorReplayV2[0].Coverage
	checkpoint := coverage.Checkpoints[1]
	active := coverage.Intervals[0]
	// At epoch101 the previous epoch's row can remain during the exact retry
	// window, but only with a canonical epoch100 commitment.
	if err := verifyFinalCoverageFreshnessV2(coverage, checkpoint, &active); err != nil {
		t.Fatal(err)
	}
	for index := range coverage.Commits {
		if coverage.Commits[index].NativeEpoch == 100 {
			coverage.Commits[index].NativeEpoch = 99
		}
	}
	if err := verifyFinalCoverageFreshnessV2(coverage, checkpoint, &active); err == nil {
		t.Fatal("missing current-target commitment accepted old row")
	}
}

func TestFinalNativeCoverageV2ReproducesOnlyImmutableCheckpointState(t *testing.T) {
	source, _, _ := finalCoverageTestEvidenceV2(t, 900)
	original := source.ValidatorReplayV2[0].Coverage.Checkpoints[0]
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var later FinalNativeCheckpointV2
	if err := json.Unmarshal(raw, &later); err != nil {
		t.Fatal(err)
	}
	later.Identity.Stake.Identity.FinalizedNumber = 2700
	later.Identity.Stake.Identity.FinalizedHash = finalCoverageTestHashV2(2700)
	if !finalNativeCheckpointEqualV2(original, later) {
		t.Fatal("new live finality tip changed immutable historical comparison")
	}
	later.Weights.Values[0]++
	if finalNativeCheckpointEqualV2(original, later) {
		t.Fatal("different historical row survived finality normalization")
	}
}

func TestFinalNativeCoverageV2CountsSharedPayoutOnceAndRetainsEverySettlement(t *testing.T) {
	source, _, _ := finalCoverageTestEvidenceV2(t, 900)
	var states []FinalNativeEpochPayoutState
	for epoch := source.Window.FirstEpoch; epoch < source.Window.FirstEpoch+source.Window.EpochCount; epoch++ {
		head, nativeEpoch, err := finalNativeEpochReveal(source, epoch)
		if err != nil {
			t.Fatal(err)
		}
		parent := ChainHead{Number: head.Number - 1, Hash: finalCoverageTestHashV2(head.Number - 1).Hex()}
		source.NativeRewards = append(source.NativeRewards, FinalNativeRewardDelta{Epoch: epoch, Role: "head", SubjectID: 1, UID: 10, Hotkey: finalCoverageTestHashV2(10).Hex(), Before: parent, After: head, AfterRao: "7", AfterIncentiveU16: 1, Expected: "positive"})
		states = append(states, FinalNativeEpochPayoutState{SettlementEpoch: epoch, SubnetEpoch: nativeEpoch, Netuid: 521, Parent: parent, Block: head, UIDs: []FinalNativeEpochPayoutUIDState{{UID: 10, StakeAfterRao: "7"}}})
	}
	audit, err := finalPublicNativePayoutAuditForEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	if audit.Epochs != 5 || audit.ParentTransitions != 4 || audit.UIDRows != 5 {
		t.Fatalf("settlement/payout counts: %+v", audit)
	}
	if err := verifyFinalPublicNativePayoutObservationShape(audit, states); err != nil {
		t.Fatal(err)
	}
	states[1].UIDs[0].StakeAfterRao = "8"
	if err := verifyFinalPublicNativePayoutObservationShape(audit, states); err == nil {
		t.Fatal("shared native transition changed between settlement labels")
	}
}

func TestFinalNativeCoverageV2ApplicationSeparatesNewerCommitFromAppliedRow(t *testing.T) {
	commit, _, _ := finalNativeTestCommitEvidence(t)
	head := ChainHead{Number: commit.RevealBlock + 3, Hash: finalTestHex(0xa1)}
	application, err := finalNativeAutomaticCallEvidence(commit, finalNativeOperationApplication, head.Number)
	if err != nil {
		t.Fatal(err)
	}
	cycle := FinalCRv4Cycle{Application: FinalNativeReceipt{Block: head, Call: &application}, Submitted: []FinalSubmittedWeight{{UID: 7, Value: 100}, {UID: 8, Value: 50}}}
	source := &FinalSemanticEvidence{Validators: []FinalValidatorIdentityEvidence{{ValidatorID: 1, UID: commit.UID, Hotkey: commit.Signer}}, ValidatorReplayV2: []FinalValidatorReplayV2{{ValidatorID: 1, Coverage: &FinalNativeCoverageV2{Commits: []FinalNativeCoverageCommitV2{{Block: ChainHead{Number: commit.CommitBlock}}, {Block: ChainHead{Number: commit.RevealBlock + 1}}}}}}}
	state := FinalNativeWeightState{ValidatorUID: commit.UID, ValidatorHotkey: commit.Signer, LastUpdate: commit.RevealBlock + 1, UIDs: []uint16{7, 8}, Values: []uint16{100, 50}, Block: head}
	if err := verifyFinalNativeApplicationV2(source, 1, &cycle, state); err != nil {
		t.Fatal(err)
	}
	state.LastUpdate = commit.CommitBlock
	if err := verifyFinalNativeApplicationV2(source, 1, &cycle, state); err == nil {
		t.Fatal("stale latest-commit observation was accepted")
	}
	state.LastUpdate = commit.RevealBlock + 1
	state.Values[0]++
	if err := verifyFinalNativeApplicationV2(source, 1, &cycle, state); err == nil {
		t.Fatal("pending commit admitted a different applied row")
	}
}

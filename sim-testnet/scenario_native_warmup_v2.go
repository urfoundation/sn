//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// Readiness owns no accepted epoch. Original signed references and actual
// native state decide when the next complete settlement window may be chosen;
// the final archive still replays every cut, decision and payout event.
type ScenarioNativeWarmupV2 struct {
	Schema            string                            `json:"schema"`
	Phase             string                            `json:"phase"`
	Ready             bool                              `json:"ready"`
	Detail            string                            `json:"detail"`
	Validators        []ScenarioNativeWarmupValidatorV2 `json:"validators"`
	Before            *NativeRewardObservation          `json:"payout_parent,omitempty"`
	After             *NativeRewardObservation          `json:"payout,omitempty"`
	PreparationWindow *ScenarioNativeWarmupBudgetV2     `json:"preparation_window,omitempty"`
}

// The funded relay creates this window before scenario preparation. Its EVM
// block ceiling and wall deadline are fixed independently; passing either
// ceiling without actual readiness ends the attempt, even if preparation has
// just completed. The head is an EVM admission clock, not a native mapping.
type ScenarioNativeWarmupBudgetV2 struct {
	Phase     string    `json:"phase"`
	StartHead ChainHead `json:"start_head"`
	EndBlock  uint64    `json:"end_block"`
	StartedAt time.Time `json:"started_at"`
	Deadline  time.Time `json:"deadline"`
}

func newScenarioNativeWarmupBudgetV2(cfg *ResolvedConfig, phase string, prepared bool, head ChainHead, started time.Time) (*ScenarioNativeWarmupBudgetV2, error) {
	if !scenarioNeedsNativeWarmupV2(cfg, phase) {
		return nil, nil
	}
	if started.IsZero() || verifyFinalHead("native readiness preparation", head) != nil || cfg.Public == nil {
		return nil, errors.New("native readiness preparation has no actual clock anchor")
	}
	work, err := evidenceRelayConfiguredWork(cfg)
	if err != nil {
		return nil, err
	}
	blocks, err := work.preparation(phase, prepared)
	if err != nil {
		return nil, err
	}
	end, ok := checkedAdd(head.Number, blocks)
	if !ok {
		return nil, errors.New("native readiness preparation block deadline overflows")
	}
	duration, err := scenarioNativeBlockDurationV2(cfg, blocks)
	if err != nil {
		return nil, err
	}
	return &ScenarioNativeWarmupBudgetV2{Phase: phase, StartHead: head, EndBlock: end, StartedAt: started.UTC(), Deadline: started.Add(duration).UTC()}, nil
}

func (self *ScenarioNativeWarmupBudgetV2) remaining(block uint64, now time.Time) (uint64, error) {
	if self == nil || self.StartedAt.IsZero() || !self.Deadline.After(self.StartedAt) || self.EndBlock <= self.StartHead.Number || block < self.StartHead.Number {
		return 0, errors.New("native readiness has no original preparation deadline")
	}
	if !now.Before(self.Deadline) || block > self.EndBlock {
		return 0, fmt.Errorf("native readiness exhausted its shared preparation deadline: start=%d end=%d observed=%d deadline=%s", self.StartHead.Number, self.EndBlock, block, self.Deadline.Format(time.RFC3339Nano))
	}
	return self.EndBlock - block, nil
}

type ScenarioNativeWarmupValidatorV2 struct {
	ValidatorID uint64                  `json:"validator_id"`
	VectorHash  string                  `json:"vector_hash"`
	Measurement string                  `json:"measurement_hash"`
	Checkpoint  FinalNativeCheckpointV2 `json:"checkpoint"`
	Payout      FinalNativeCheckpointV2 `json:"payout_checkpoint"`
}

type scenarioNativeWarmupReadV2 func(context.Context, *ScenarioObservation, *ScenarioObservation) (*ScenarioNativeWarmupV2, error)

func scenarioNeedsNativeWarmupV2(cfg *ResolvedConfig, phase string) bool {
	return cfg != nil && cfg.Config != nil && !provisionalResumeEnabled(cfg) && (finalUsesEvidenceV2(cfg) || cfg.Config.ProvisionValidatorEvidenceV2) && (phase == "release-1.0" || phase == "production-soak")
}

// A complete fresh settlement source, a commit inclusion boundary, its
// configured reveal delay and one later coinbase boundary form the finite
// watchdog. Actual readiness ends the wait immediately; this is no ETA.
func scenarioNativeWarmupBlocksV2(cfg *ResolvedConfig, phase string) (uint64, error) {
	if !scenarioNeedsNativeWarmupV2(cfg, phase) {
		return 0, nil
	}
	if cfg.Policy == nil || cfg.Hyperparameters == nil {
		return 0, errors.New("native warm-up has no independently configured clocks")
	}
	tempo := hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"])
	period := hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["commit_reveal_period"])
	if tempo == 0 || period == 0 {
		return 0, errors.New("native warm-up cadence or reveal period is absent")
	}
	boundaries, ok := checkedAdd(period, 2)
	if !ok {
		return 0, errors.New("native warm-up reveal count overflows")
	}
	native, ok := checkedMul(boundaries, tempo)
	if !ok {
		return 0, errors.New("native warm-up native span overflows")
	}
	epoch, finalize := cfg.Policy.Settlement.EpochBlocks, cfg.Policy.Settlement.FinalizeOffsetBlocks
	if phase == "production-soak" {
		epoch, finalize = cfg.Policy.ProductionCadence.EpochBlocks, cfg.Policy.ProductionCadence.FinalizeOffsetBlocks
	}
	settlement, ok := checkedAdd(epoch, finalize)
	if !ok || epoch == 0 {
		return 0, errors.New("native warm-up settlement span is absent or overflows")
	}
	span, ok := checkedAdd(native, settlement)
	if !ok {
		return 0, errors.New("native warm-up combined span overflows")
	}
	return span, nil
}

func scenarioNativeBlockDurationV2(cfg *ResolvedConfig, blocks uint64) (time.Duration, error) {
	if cfg == nil || blocks == 0 || cfg.Public == nil || cfg.Public.Chain.ExpectedBlockSeconds == 0 {
		return 0, errors.New("native warm-up has no finite watchdog")
	}
	seconds, ok := checkedMul(blocks, cfg.Public.Chain.ExpectedBlockSeconds)
	if !ok {
		return 0, errors.New("native warm-up seconds overflow")
	}
	nanos, ok := checkedMul(seconds, uint64(time.Second))
	if !ok || nanos > uint64(^uint64(0)>>1) {
		return 0, errors.New("native warm-up duration overflows")
	}
	return time.Duration(nanos), nil
}

// Both signatures must describe a fresh full decision from this actual
// campaign. Old native applications remain in history without becoming a new
// baseline. Later commits and the applied row use separate clocks.
func scenarioNativeWarmupDecisionV2(cfg *ResolvedConfig, baseline, current *ScenarioObservation, validator ValidatorObservation, checkpoint FinalNativeCheckpointV2) (*HeadDecisionObservation, string, error) {
	if cfg == nil || cfg.Config == nil || cfg.Policy == nil || baseline == nil || baseline.NativeRewards == nil || current == nil || current.Status == nil || current.Status.Contracts == nil {
		return nil, "", errors.New("native warm-up source observation is incomplete")
	}
	if validator.ValidatorID < 1 || validator.ValidatorID > cfg.Config.Topology.Validators || validator.SelfUID != checkpoint.Weights.ValidatorUID {
		return nil, "", errors.New("native warm-up validator owner differs")
	}
	if validator.NativeSourceScopeV2 != "signed-source-and-canonical-native-receipts" || requireFinalSHA256("warm-up original intent store", validator.NativeSourceStoreSHA256) != nil {
		return nil, "", errors.New("native warm-up lacks the original signed V2 source and canonical receipt reader")
	}
	var active *HeadDecisionObservation
	for index := range validator.HeadDecisions {
		candidate := &validator.HeadDecisions[index]
		if candidate.RevealBlock > checkpoint.Mapping.Query.NativeNumber {
			continue
		}
		if active == nil || candidate.RevealBlock > active.RevealBlock {
			active = candidate
		} else if candidate.RevealBlock == active.RevealBlock {
			return nil, "", errors.New("native warm-up has conflicting applied decisions")
		}
	}
	if active == nil || active.NativeSnapshot.Number < baseline.NativeRewards.FinalizedHead.Number || active.FinalizedBlock <= baseline.NativeRewards.FinalizedHead.Number {
		return nil, fmt.Sprintf("validator %d awaits a fresh native application after block %d", validator.ValidatorID, baseline.NativeRewards.FinalizedHead.Number), nil
	}
	if active.Error != "" || requireFinalSHA256("warm-up signed measurement", active.MeasurementArtifactHash) != nil || verifyFinalHead("warm-up application", ChainHead{Number: active.ApplicationBlock, Hash: active.ApplicationBlockHash}) != nil || active.ApplicationBlock < active.RevealBlock {
		return nil, "", errors.New("native warm-up signed application reference is invalid")
	}
	uids, values := make([]uint16, len(active.AppliedWeights)), make([]uint16, len(active.AppliedWeights))
	for index, weight := range active.AppliedWeights {
		uids[index], values[index] = weight.UID, weight.Value
	}
	if !slices.Equal(uids, checkpoint.Weights.UIDs) || !slices.Equal(values, checkpoint.Weights.Values) {
		return nil, fmt.Sprintf("validator %d awaits its complete observed native row", validator.ValidatorID), nil
	}
	coverage := &FinalNativeCoverageV2{Commits: validator.NativeCommitsV2}
	interval := &FinalNativeApplicationIntervalV2{MeasurementHash: active.MeasurementArtifactHash, FromBlock: active.RevealBlock, CommitNativeEpoch: active.CommitNativeEpoch, RevealNativeEpoch: active.RevealNativeEpoch}
	if err := verifyFinalCoverageFreshnessV2(coverage, checkpoint, interval); err != nil {
		return nil, fmt.Sprintf("validator %d awaits current native coverage: %v", validator.ValidatorID, err), nil
	}
	projected := validator
	projected.MaskedUIDs, projected.EligibleHeadUIDs, projected.EligibleHeadScores = active.MaskedUIDs, active.EligibleHeadUIDs, active.EligibleHeadScores
	projected.SelectedHeadUIDs, projected.RejectedHeadUIDs, projected.StaleHeadBindings, projected.AppliedWeights = active.SelectedHeadUIDs, active.RejectedHeadUIDs, active.StaleHeadBindings, active.AppliedWeights
	pools := map[uint16]bool{}
	for _, operator := range current.Status.Contracts.Operators {
		if operator.PoolLive {
			pools[operator.PoolUID] = true
		}
	}
	if len(pools) != cfg.Config.Topology.Operators {
		return nil, "awaiting the complete live operator pool census", nil
	}
	if _, _, _, err := validateValidatorHeadWeightDecision(cfg, uint16Set(current.CandidateFleetUIDs), pools, projected); err != nil {
		return nil, fmt.Sprintf("validator %d awaits its complete fleet decision: %v", validator.ValidatorID, err), nil
	}
	if ok, _, _ := weightValuesRespectCap(active.AppliedWeights, cfg.Policy.Steering.MaxWeightLimitU16); !ok {
		return nil, "", errors.New("native warm-up vector violates the approved native weight cap")
	}
	if checkpoint.PayoutHead.Number < active.RevealBlock {
		return nil, fmt.Sprintf("validator %d awaits a native payout after reveal block %d", validator.ValidatorID, active.RevealBlock), nil
	}
	return active, "", nil
}

func scenarioNativeWarmupPayoutV2(before, after *NativeRewardObservation, decisions []*HeadDecisionObservation) (bool, error) {
	if before == nil || after == nil || before.FinalizedHead.Number == ^uint64(0) || before.FinalizedHead.Number+1 != after.FinalizedHead.Number || len(decisions) == 0 {
		return false, errors.New("native warm-up payout has no exact parent transition")
	}
	if len(before.TotalHotkeyAlphaRao) != len(after.TotalHotkeyAlphaRao) || len(after.EmissionRao) != len(after.TotalHotkeyAlphaRao) || len(after.Incentive) != len(after.EmissionRao) {
		return false, errors.New("native warm-up payout UID census differs")
	}
	for _, decision := range decisions {
		positive := false
		for _, weight := range decision.AppliedWeights {
			if weight.Value == 0 {
				continue
			}
			uid := int(weight.UID)
			if uid >= len(after.EmissionRao) {
				return false, errors.New("native warm-up weighted UID is absent from payout")
			}
			prior, ok := new(big.Int).SetString(before.TotalHotkeyAlphaRao[uid], 10)
			if !ok || prior.Sign() < 0 {
				return false, errors.New("native warm-up parent stake is invalid")
			}
			current, ok := new(big.Int).SetString(after.TotalHotkeyAlphaRao[uid], 10)
			if !ok || current.Sign() < 0 {
				return false, errors.New("native warm-up payout stake is invalid")
			}
			emission, ok := new(big.Int).SetString(after.EmissionRao[uid], 10)
			if !ok || emission.Sign() < 0 {
				return false, errors.New("native warm-up emission is invalid")
			}
			if current.Cmp(prior) > 0 && emission.Sign() > 0 && after.Incentive[uid] > 0 {
				positive = true
			}
		}
		if !positive {
			return false, nil
		}
	}
	return true, nil
}

// Reuse the already owned relay/native clients. The actual EVM hash and the
// reviewed runtime's first-insertion mapping authenticate both payout clocks.
func liveScenarioNativeWarmupV2(cfg *ResolvedConfig, executor *Executor, chain *validatorpkg.ChainClient, phase string) scenarioNativeWarmupReadV2 {
	return func(ctx context.Context, baseline, current *ScenarioObservation) (*ScenarioNativeWarmupV2, error) {
		result := &ScenarioNativeWarmupV2{Schema: "urnetwork-sim-native-warmup-v2", Phase: phase}
		if ctx == nil || executor == nil || executor.substrate == nil || executor.substrate.chain == nil || executor.roles == nil || chain == nil || current == nil || current.Status == nil || current.Status.Contracts == nil || len(current.Validators) != cfg.Config.Topology.Validators {
			return nil, errors.New("native warm-up has no complete actual executor and validator census")
		}
		if !current.FleetCommitmentValid || !current.FleetBindingsValid || len(uint16Set(current.CandidateFleetUIDs)) != cfg.Config.Topology.fleetCandidates() {
			result.Detail = "awaiting every current approved fleet commitment and binding"
			return result, nil
		}
		native, runtime := executor.substrate.chain, executor.runtimeEvidenceNativeIdentityV2()
		var decisions []*HeadDecisionObservation
		seen := map[int]bool{}
		var payout, parent ChainHead
		for _, validator := range current.Validators {
			if validator.ValidatorID < 1 || validator.ValidatorID > cfg.Config.Topology.Validators || seen[validator.ValidatorID] {
				return nil, errors.New("native warm-up repeats a validator")
			}
			seen[validator.ValidatorID] = true
			hotkey, _, err := runtimeEvidenceActivationKeysV2(executor.roles, uint64(validator.ValidatorID), 1)
			if err != nil {
				return nil, err
			}
			if len(validator.HeadDecisions) == 0 {
				result.Detail = fmt.Sprintf("validator %d awaits its first strict signed application", validator.ValidatorID)
				return result, nil
			}
			checkpoint, err := readFinalNativeCheckpointV2(ctx, native, current.Status.Contracts.FinalizedHead, cfg.Netuid, validator.SelfUID, hotkey.PublicKey(), runtime)
			if err != nil {
				return nil, err
			}
			if !checkpoint.Identity.Stake.MeetsNonSelfStakeAndPermit() {
				return nil, errors.New("native warm-up validator lacks actual permit and non-self stake")
			}
			decision, detail, err := scenarioNativeWarmupDecisionV2(cfg, baseline, current, validator, checkpoint)
			if err != nil {
				return nil, err
			}
			if decision == nil {
				result.Detail = detail
				return result, nil
			}
			evmHash, err := chain.BlockHashContext(ctx, checkpoint.PayoutHead.Number)
			if err != nil {
				return nil, err
			}
			payoutPoint, err := readFinalNativeCheckpointV2(ctx, native, ChainHead{Number: checkpoint.PayoutHead.Number, Hash: fmt.Sprintf("0x%x", evmHash)}, cfg.Netuid, validator.SelfUID, hotkey.PublicKey(), runtime)
			if err != nil {
				return nil, err
			}
			if payoutPoint.Weights.Block != checkpoint.PayoutHead || !slices.Equal(payoutPoint.Weights.UIDs, checkpoint.Weights.UIDs) || !slices.Equal(payoutPoint.Weights.Values, checkpoint.Weights.Values) {
				result.Detail = fmt.Sprintf("validator %d awaits a payout under its newly applied row", validator.ValidatorID)
				return result, nil
			}
			if payout != (ChainHead{}) && (payout != checkpoint.PayoutHead || parent != checkpoint.PayoutParent) {
				return nil, errors.New("native warm-up validators disagree on the canonical payout boundary")
			}
			payout, parent = checkpoint.PayoutHead, checkpoint.PayoutParent
			decisions = append(decisions, decision)
			result.Validators = append(result.Validators, ScenarioNativeWarmupValidatorV2{ValidatorID: uint64(validator.ValidatorID), VectorHash: decision.VectorHash, Measurement: decision.MeasurementArtifactHash, Checkpoint: checkpoint, Payout: payoutPoint})
		}
		var err error
		result.Before, err = readFinalNativeRewardAtV2(ctx, native, parent, cfg.Netuid, runtime)
		if err != nil {
			return nil, err
		}
		result.After, err = readFinalNativeRewardAtV2(ctx, native, payout, cfg.Netuid, runtime)
		if err != nil {
			return nil, err
		}
		result.Ready, err = scenarioNativeWarmupPayoutV2(result.Before, result.After, decisions)
		if err != nil {
			return nil, err
		}
		result.Detail = "awaiting positive native payout under both fresh signed vectors"
		if result.Ready {
			result.Detail = fmt.Sprintf("both fresh native vectors are applied and canonical payout block %d has positive emission and stake growth", payout.Number)
			latest, _, err := chain.FinalizedBlockContext(ctx)
			if err != nil {
				return nil, err
			}
			if latest >= current.Status.Contracts.CurrentEpochEnd {
				result.Ready = false
				result.Detail = "refreshing the settlement baseline after native readiness crossed its earlier boundary"
			}
		}
		return result, ctx.Err()
	}
}

// Every observation, including incomplete readiness, remains in the campaign
// history. A timeout/cancellation cannot create the irreversible acceptance
// boundary, and no injected fault can consume the warm-up window.
func waitScenarioNativeWarmupV2(ctx context.Context, cfg *ResolvedConfig, phase string, baseline, current *ScenarioObservation, probe scenarioProbe, options scenarioRunOptions, retain func(*ScenarioObservation) error) (*ScenarioObservation, error) {
	if !scenarioNeedsNativeWarmupV2(cfg, phase) {
		return current, nil
	}
	if options.NativeWarmupV2 == nil || options.NativeWarmupBudgetV2 == nil || options.NativeWarmupBudgetV2.Phase != phase || retain == nil || probe == nil {
		return current, errors.New("strict V2 scenario lacks native readiness admission")
	}
	bounded, cancel := context.WithDeadline(ctx, options.NativeWarmupBudgetV2.Deadline)
	defer cancel()
	for {
		if err := bounded.Err(); err != nil {
			return current, err
		}
		if current == nil || current.Status == nil || current.Status.Contracts == nil {
			return current, errors.New("native readiness lost its actual preparation clock")
		}
		if _, err := options.NativeWarmupBudgetV2.remaining(current.Status.Contracts.FinalizedHead.Number, time.Now()); err != nil {
			return current, err
		}
		warmup, err := options.NativeWarmupV2(bounded, baseline, current)
		if err != nil {
			return current, err
		}
		if warmup == nil || warmup.Schema != "urnetwork-sim-native-warmup-v2" || warmup.Phase != phase {
			return current, errors.New("native warm-up response owner differs")
		}
		window := *options.NativeWarmupBudgetV2
		warmup.PreparationWindow = &window
		detached := *current
		current = &detached
		current.NativeWarmupV2 = warmup
		current.ObservationHash = ""
		current.ObservationHash, err = canonicalHashHex(current)
		if err != nil {
			return current, err
		}
		if err := retain(current); err != nil {
			return current, err
		}
		if warmup.Ready {
			if err := bounded.Err(); err != nil {
				return current, err
			}
			if options.NativeWarmupCompleteV2 != nil {
				if err := options.NativeWarmupCompleteV2(bounded); err != nil {
					return current, err
				}
			}
			return current, nil
		}
		timer := time.NewTimer(options.PollInterval)
		select {
		case <-bounded.Done():
			timer.Stop()
			return current, fmt.Errorf("native warm-up ended before acceptance: %s: %w", warmup.Detail, bounded.Err())
		case <-timer.C:
		}
		current, err = probe.Snapshot(bounded)
		if err != nil {
			return current, err
		}
	}
}

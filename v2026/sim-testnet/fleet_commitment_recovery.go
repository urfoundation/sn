// Fleet commitment recovery keeps native attestations fresh until their first
// EVM consumer. A verified commitment is historical evidence, not a perpetual
// authorization: the coordinator deliberately rejects it after two setup
// epochs, so a revised plan must approve and budget an exact replacement.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	fleetCommitmentRecoveryBlockParameter = "recovery_after_commitment_block"
	fleetCommitmentRecoveryCountParameter = "commitment_recovery_count"
	fleetCommitmentRecoveryFeeParameter   = "maximum_fee_rao"
	fleetCommitmentFundingCountParameter  = "commitment_recovery_funding_count"
	fleetCommitmentReserveCountParameter  = "commitment_recovery_reserve_count"

	// Leave enough lifetime for preparation, estimation, signing, propagation,
	// inclusion and ordinary public-RPC head skew. This is stricter than the
	// contract's exact expiry boundary and never changes that on-chain bound.
	fleetCommitmentInclusionSafetyBlocks uint64 = 30
)

// Identifies one generation and the action which first consumes it. Once that
// consumer is verified, later native writes no longer need the old attestation.
type fleetCommitmentCoordinates struct {
	Fleet        int
	Generation   uint64
	ActionID     string
	ConsumerID   string
	ManifestName string
	EvidenceName string
}

// Reports the exact plan consumer which made a generation-specific native
// commitment historical. Before that consumer verifies, every preparation and
// postcondition continues to require the commitment to be the current pallet
// value. Afterwards, the canonical finalized write is the durable evidence;
// later approved generations and the precompile round trip may replace it.
func (self *Executor) consumedFleetCommitmentGeneration(fleet int, generation uint64) (string, bool, error) {
	if self == nil || self.cfg == nil || self.plan == nil || self.journal == nil {
		return "", false, errors.New("fleet commitment consumer context is unavailable")
	}
	actionID := fmt.Sprintf("fleet.commitment.%d", fleet)
	if generation == 2 {
		actionID = fmt.Sprintf("fleet.refresh.commitment.%d", fleet)
	} else if generation != 1 {
		return "", false, fmt.Errorf("fleet %d commitment generation %d is unsupported", fleet, generation)
	}
	action, err := exactPlanActionByID(self.plan, actionID)
	if err != nil {
		return "", false, err
	}
	coordinates, err := fleetCommitmentActionCoordinates(self.cfg, action, true)
	if err != nil {
		return "", false, err
	}
	return coordinates.ConsumerID, exactVerifiedPlanAction(self.plan, self.journal.Entries(), coordinates.ConsumerID), nil
}

// Parses only canonical initial and refresh action ids from the configured
// fleet topology. Legacy source plans may omit batching parameters, so callers
// which authenticate ancestry separately can request the basic shape only.
func fleetCommitmentActionCoordinates(cfg *ResolvedConfig, action Action, requireCurrentShape bool) (fleetCommitmentCoordinates, error) {
	coordinates := fleetCommitmentCoordinates{}
	if cfg == nil || cfg.Config == nil {
		return coordinates, errors.New("fleet commitment topology is unavailable")
	}
	prefix := "fleet.commitment."
	coordinates.Generation = 1
	coordinates.ManifestName = "fleet-%d.json"
	coordinates.EvidenceName = "fleet-%d.commitment.json"
	if strings.HasPrefix(action.ID, "fleet.refresh.commitment.") {
		prefix = "fleet.refresh.commitment."
		coordinates.Generation = 2
		coordinates.ManifestName = "fleet-%d.refresh.json"
		coordinates.EvidenceName = "fleet-%d.refresh.commitment.json"
	} else if !strings.HasPrefix(action.ID, prefix) {
		return coordinates, fmt.Errorf("action %q is not a fleet commitment", action.ID)
	}
	fleet, err := strconv.Atoi(strings.TrimPrefix(action.ID, prefix))
	maximum := cfg.Config.Topology.fleetCandidates()
	if coordinates.Generation == 2 {
		maximum = cfg.Config.Topology.HeadFleets
	}
	if err != nil || fleet < 1 || fleet > maximum || action.ID != prefix+strconv.Itoa(fleet) {
		return coordinates, fmt.Errorf("fleet commitment action %q is out of range", action.ID)
	}
	coordinates.Fleet = fleet
	coordinates.ActionID = action.ID
	coordinates.ManifestName = fmt.Sprintf(coordinates.ManifestName, fleet)
	coordinates.EvidenceName = fmt.Sprintf(coordinates.EvidenceName, fleet)
	if coordinates.Generation == 2 {
		coordinates.ConsumerID = fmt.Sprintf("fleet.refresh.batch.%d", 1+(fleet-1)/fleetRefreshBatchSize)
	} else if fleet <= cfg.Config.Topology.HeadFleets {
		coordinates.ConsumerID = fmt.Sprintf("fleet.install.batch.%d", 1+(fleet-1)/fleetRefreshBatchSize)
	} else {
		coordinates.ConsumerID = fmt.Sprintf("fleet.mirror.%d", fleet)
	}
	wantTarget := fmt.Sprintf("head-fleet:%d", fleet)
	if coordinates.Generation == 1 && fleet > cfg.Config.Topology.HeadFleets {
		wantTarget = fmt.Sprintf("challenger-fleet:%d", fleet)
	}
	if action.Kind != "substrate-extrinsic" || action.Target != wantTarget || action.Parameters[fleetCommitmentStorageParameter] != fleetCommitmentStorageV2 {
		return fleetCommitmentCoordinates{}, fmt.Errorf("fleet commitment action %s has a foreign kind, target, or storage schema", action.ID)
	}
	if coordinates.Generation == 2 && action.Parameters["generation"] != "2" {
		return fleetCommitmentCoordinates{}, fmt.Errorf("fleet refresh commitment %s does not bind generation 2", action.ID)
	}
	if !requireCurrentShape {
		return coordinates, nil
	}
	batch := 1 + (fleet-1)/fleetRefreshBatchSize
	if coordinates.Generation == 2 {
		if action.Parameters[fleetCommitmentParallelGroupParameter] != fmt.Sprintf("refresh-%d", batch) {
			return fleetCommitmentCoordinates{}, fmt.Errorf("fleet refresh commitment %s has a foreign parallel group", action.ID)
		}
	} else if fleet <= cfg.Config.Topology.HeadFleets {
		if action.Parameters[fleetCommitmentParallelGroupParameter] != fmt.Sprintf("install-%d", batch) {
			return fleetCommitmentCoordinates{}, fmt.Errorf("fleet commitment %s has a foreign parallel group", action.ID)
		}
	} else if action.Parameters[fleetCommitmentParallelGroupParameter] != "" {
		return fleetCommitmentCoordinates{}, fmt.Errorf("challenger commitment %s cannot join a head-fleet parallel group", action.ID)
	}
	return coordinates, nil
}

// Decodes the all-or-none recovery envelope. The floor names the evidence a
// newly approved action must replace; count makes repeated recovery cumulative.
func fleetCommitmentRecoveryEnvelope(action Action) (uint64, uint64, uint64, bool, error) {
	blockValue := action.Parameters[fleetCommitmentRecoveryBlockParameter]
	countValue := action.Parameters[fleetCommitmentRecoveryCountParameter]
	feeValue := action.Parameters[fleetCommitmentRecoveryFeeParameter]
	commitmentAction := strings.HasPrefix(action.ID, "fleet.commitment.") || strings.HasPrefix(action.ID, "fleet.refresh.commitment.")
	present := blockValue != "" || countValue != "" || commitmentAction && feeValue != ""
	if !present {
		return 0, 0, 0, false, nil
	}
	block, blockErr := strconv.ParseUint(blockValue, 10, 64)
	count, countErr := strconv.ParseUint(countValue, 10, 64)
	fee, feeErr := strconv.ParseUint(feeValue, 10, 64)
	if blockErr != nil || countErr != nil || feeErr != nil || block == 0 || count == 0 || fee == 0 ||
		blockValue != strconv.FormatUint(block, 10) || countValue != strconv.FormatUint(count, 10) || feeValue != strconv.FormatUint(fee, 10) {
		return 0, 0, 0, true, fmt.Errorf("fleet commitment recovery %s is not canonical", action.ID)
	}
	return block, count, fee, true, nil
}

// Restricts the exception to a current-schema revised native commitment. The
// normal action shape and zero spend remain unchanged; only its replacement
// floor, cumulative count, and global per-transaction fee ceiling are added.
func validateFleetCommitmentRecoveryPlanAction(plan *SetupPlan, action Action) error {
	floor, _, fee, recovery, err := fleetCommitmentRecoveryEnvelope(action)
	if err != nil || !recovery {
		return err
	}
	prefix := "fleet.commitment."
	refresh := strings.HasPrefix(action.ID, "fleet.refresh.commitment.")
	if refresh {
		prefix = "fleet.refresh.commitment."
	}
	fleet, parseErr := strconv.Atoi(strings.TrimPrefix(action.ID, prefix))
	canonicalID := prefix + strconv.Itoa(fleet)
	wantHeadTarget := fmt.Sprintf("head-fleet:%d", fleet)
	wantChallengerTarget := fmt.Sprintf("challenger-fleet:%d", fleet)
	targetOK := action.Target == wantHeadTarget || !refresh && action.Target == wantChallengerTarget
	if plan == nil || plan.Schema != currentSetupPlanSchema || len(plan.PriorPlanHashes) == 0 || parseErr != nil || fleet < 1 || action.ID != canonicalID ||
		action.Kind != "substrate-extrinsic" || !targetOK || action.Parameters[fleetCommitmentStorageParameter] != fleetCommitmentStorageV2 ||
		refresh && action.Parameters["generation"] != "2" || fee != plan.NativeTransactionFeeLimitRao || floor > max(plan.LiveFacts.FinalizedBlock, plan.LiveFacts.EVMFinalizedBlock) ||
		len(action.AcceptedPriorIntentHashes) != 0 || !spendIsZero(action.Spend) {
		return fmt.Errorf("fleet commitment recovery %s is outside the approved revision envelope", action.ID)
	}
	return nil
}

// Proves that cumulative retry funding and the global native-write reserve
// exactly match every recovery count carried by the plan.
func validateFleetCommitmentRecoveryBudget(plan *SetupPlan) error {
	if plan == nil || plan.Schema != currentSetupPlanSchema {
		return nil
	}
	counts := map[int]uint64{}
	challenger := map[int]bool{}
	var total uint64
	for _, action := range plan.Actions {
		_, count, _, recovery, err := fleetCommitmentRecoveryEnvelope(action)
		if err != nil {
			return err
		}
		if !recovery {
			continue
		}
		if err := validateFleetCommitmentRecoveryPlanAction(plan, action); err != nil {
			return err
		}
		prefix := "fleet.commitment."
		if strings.HasPrefix(action.ID, "fleet.refresh.commitment.") {
			prefix = "fleet.refresh.commitment."
		}
		fleet, _ := strconv.Atoi(strings.TrimPrefix(action.ID, prefix))
		var addOK bool
		counts[fleet], addOK = checkedAdd(counts[fleet], count)
		if !addOK {
			return errors.New("fleet commitment recovery funding count overflows")
		}
		challenger[fleet] = challenger[fleet] || strings.HasPrefix(action.Target, "challenger-fleet:")
		total, addOK = checkedAdd(total, count)
		if !addOK {
			return errors.New("fleet commitment total recovery count overflows")
		}
	}
	for _, action := range plan.Actions {
		if strings.HasPrefix(action.ID, "fleet.fund-hotkey.") && action.Parameters[fleetCommitmentFundingCountParameter] != "" {
			fleet, err := strconv.Atoi(strings.TrimPrefix(action.ID, "fleet.fund-hotkey."))
			if err != nil || counts[fleet] == 0 {
				return fmt.Errorf("fleet commitment recovery funding marker on unrelated action %s", action.ID)
			}
		}
		if action.ID == "wallet.native-fee-reserve" && action.Parameters[fleetCommitmentReserveCountParameter] != "" && total == 0 {
			return errors.New("wallet native fee reserve has a recovery marker without commitment recoveries")
		}
	}
	for fleet, count := range counts {
		funding, err := exactPlanActionByID(plan, fmt.Sprintf("fleet.fund-hotkey.%d", fleet))
		if err != nil {
			return err
		}
		baseWrites := uint64(2)
		wantTarget := fmt.Sprintf("head-fleet-hotkey:%d", fleet)
		if challenger[fleet] {
			baseWrites = 1
			wantTarget = fmt.Sprintf("challenger-fleet-hotkey:%d", fleet)
		} else if fleet == 1 {
			baseWrites = 4
		}
		writes, addOK := checkedAdd(baseWrites, count)
		fees, multiplyOK := checkedMul(writes, plan.NativeTransactionFeeLimitRao)
		wantFunding, fundingOK := checkedAdd(fees, plan.LiveFacts.ExistentialDepositRao)
		encodedCount, countErr := strconv.ParseUint(funding.Parameters[fleetCommitmentFundingCountParameter], 10, 64)
		encodedFees, feeErr := strconv.ParseUint(funding.Parameters["maximum_fee_rao"], 10, 64)
		if !addOK || !multiplyOK || !fundingOK || countErr != nil || feeErr != nil || encodedCount != count || encodedFees != fees ||
			funding.Kind != "substrate-extrinsic" || funding.Target != wantTarget || funding.Spend.TAORao != wantFunding {
			return fmt.Errorf("fleet commitment recovery funding %s does not bind %d cumulative retries", funding.ID, count)
		}
	}
	if total == 0 {
		return nil
	}
	reserve, err := exactPlanActionByID(plan, "wallet.native-fee-reserve")
	if err != nil {
		return err
	}
	encodedWrites, writesErr := strconv.ParseUint(reserve.Parameters["native_writes"], 10, 64)
	encodedTotal, totalErr := strconv.ParseUint(reserve.Parameters[fleetCommitmentReserveCountParameter], 10, 64)
	wantReserve, multiplyOK := checkedMul(encodedWrites, plan.NativeTransactionFeeLimitRao)
	if totalErr != nil || encodedTotal != total {
		return errors.New("wallet native fee reserve recovery count differs from commitment actions")
	}
	if !multiplyOK || writesErr != nil || encodedWrites <= total || reserve.Kind != "budget-reserve" || reserve.Spend.TAORao != wantReserve {
		return errors.New("wallet native fee reserve does not exactly cover commitment recoveries")
	}
	return nil
}

// Proves the evidence was replaced after the exact block named by the action.
func validateFleetCommitmentRecoveryEvidence(action Action, evidence *FleetCommitmentEvidence) error {
	floor, _, _, recovery, err := fleetCommitmentRecoveryEnvelope(action)
	if err != nil {
		return err
	}
	observedBlock := uint64(0)
	if evidence != nil {
		observedBlock = evidence.CommitmentBlock
	}
	if recovery && observedBlock <= floor {
		return fmt.Errorf("fleet commitment recovery %s still uses block %d, want greater than %d", action.ID, observedBlock, floor)
	}
	return nil
}

// Computes the coordinator's immutable setup-policy lifetime without overflow.
func fleetCommitmentMaximumAgeBlocks(cfg *ResolvedConfig) (uint64, error) {
	if cfg == nil || cfg.Policy == nil || cfg.Policy.Settlement.EpochBlocks == 0 || cfg.Policy.Settlement.EpochBlocks > ^uint64(0)/2 {
		return 0, errors.New("fleet commitment maximum age is unavailable")
	}
	return cfg.Policy.Settlement.EpochBlocks * 2, nil
}

// Uses inclusive boundaries: the contract accepts the exact expiry block, and
// preparation is safe only when that boundary is at least safety blocks ahead.
func fleetCommitmentHasInclusionLifetime(commitmentBlock, headBlock, maximumAgeBlocks, safetyBlocks uint64) bool {
	expires, expiresOK := checkedAdd(commitmentBlock, maximumAgeBlocks)
	required, requiredOK := checkedAdd(headBlock, safetyBlocks)
	return commitmentBlock != 0 && headBlock != 0 && maximumAgeBlocks != 0 && expiresOK && requiredOK && expires >= required
}

// Fails before signatures or EVM intent when too little commitment lifetime
// remains for deterministic inclusion at the selected latest-head snapshot.
func validateFleetCommitmentInclusionLifetime(cfg *ResolvedConfig, action Action, evidence *FleetCommitmentEvidence, headBlock uint64) error {
	if evidence == nil {
		return fmt.Errorf("fleet commitment %s has no finalized evidence", action.ID)
	}
	if err := validateFleetCommitmentRecoveryEvidence(action, evidence); err != nil {
		return err
	}
	maximumAge, err := fleetCommitmentMaximumAgeBlocks(cfg)
	if err != nil {
		return err
	}
	if !fleetCommitmentHasInclusionLifetime(evidence.FinalizedBlock, headBlock, maximumAge, fleetCommitmentInclusionSafetyBlocks) {
		expires, _ := checkedAdd(evidence.FinalizedBlock, maximumAge)
		return fmt.Errorf("fleet commitment %s at block %d expires at %d without the required %d-block inclusion margin from head %d", action.ID, evidence.FinalizedBlock, expires, fleetCommitmentInclusionSafetyBlocks, headBlock)
	}
	return nil
}

// Locates one exact action in a plan without silently choosing a duplicate.
func exactPlanActionByID(plan *SetupPlan, actionID string) (Action, error) {
	if plan == nil || actionID == "" {
		return Action{}, errors.New("plan action lookup is unavailable")
	}
	var result *Action
	for index := range plan.Actions {
		if plan.Actions[index].ID != actionID {
			continue
		}
		if result != nil {
			return Action{}, fmt.Errorf("plan %s has duplicate action %s", plan.PlanHash, actionID)
		}
		copy := plan.Actions[index]
		result = &copy
	}
	if result == nil {
		return Action{}, fmt.Errorf("plan %s has no action %s", plan.PlanHash, actionID)
	}
	return *result, nil
}

// Authenticates the exact finalized transaction named by public evidence and a
// later verified postcondition in the same action-intent lineage.
func verifiedFleetCommitmentEvidenceAction(stateDir string, cfg *ResolvedConfig, prior *SetupPlan, entries []JournalEntry, coordinates fleetCommitmentCoordinates, evidence *FleetCommitmentEvidence) (Action, error) {
	if prior == nil || evidence == nil || stateDir == "" {
		return Action{}, errors.New("fleet commitment evidence lineage is unavailable")
	}
	allowedPlans := prior.allowedPlanHashes()
	for finalizedIndex, finalized := range entries {
		if !allowedPlans[finalized.PlanHash] || finalized.ActionID != coordinates.ActionID || finalized.Stage != StageFinalized ||
			!strings.EqualFold(finalized.TransactionHash, evidence.ExtrinsicHash) || finalized.BlockNumber != evidence.FinalizedBlock ||
			!strings.EqualFold(finalized.BlockHash, evidence.FinalizedBlockHash) {
			continue
		}
		source := prior
		if !strings.EqualFold(finalized.PlanHash, prior.PlanHash) {
			var err error
			source, err = readPersistedPlanFile(filepath.Join(stateDir, "plans", stringsTrim0x(finalized.PlanHash)+".json"))
			if err != nil {
				return Action{}, fmt.Errorf("read fleet commitment source plan %s: %w", finalized.PlanHash, err)
			}
		}
		if !strings.EqualFold(source.PlanHash, finalized.PlanHash) || !allowedPlans[source.PlanHash] {
			return Action{}, errors.New("fleet commitment evidence source is outside the approved lineage")
		}
		action, err := exactPlanActionByID(source, coordinates.ActionID)
		if err != nil || !actionAcceptsIntent(action, finalized.IntentHash) {
			return Action{}, stateMismatchError(err, "fleet commitment evidence has no exact source intent")
		}
		sourceCoordinates, err := fleetCommitmentActionCoordinates(cfg, action, false)
		if err != nil || sourceCoordinates.Fleet != coordinates.Fleet || sourceCoordinates.Generation != coordinates.Generation {
			return Action{}, stateMismatchError(err, "fleet commitment evidence source action differs from its generation")
		}
		for verifiedIndex := finalizedIndex + 1; verifiedIndex < len(entries); verifiedIndex++ {
			verified := entries[verifiedIndex]
			if verified.PlanHash == finalized.PlanHash && verified.ActionID == finalized.ActionID && verified.IntentHash == finalized.IntentHash && verified.Stage == StageVerified {
				return action, nil
			}
		}
		return Action{}, errors.New("fleet commitment finalized evidence has no later durable postcondition verification")
	}
	return Action{}, errors.New("fleet commitment evidence does not name an approved finalized transaction")
}

// Preserves a pending recovery, carries a still-fresh verified recovery, or
// emits the next exact recovery intent when the current evidence is unsafe.
func applyFleetCommitmentRecoveries(cfg *ResolvedConfig, stateDir string, revised, prior *SetupPlan, current *SetupFacts, entries []JournalEntry) error {
	if cfg == nil || cfg.Config == nil || revised == nil || prior == nil || current == nil {
		return errors.New("fleet commitment recovery plan context is unavailable")
	}
	maximumAge, err := fleetCommitmentMaximumAgeBlocks(cfg)
	if err != nil {
		return err
	}
	headBlock := max(current.FinalizedBlock, current.EVMFinalizedBlock)
	if headBlock == 0 {
		return errors.New("fleet commitment recovery has no finalized head")
	}
	recoveryCounts := make(map[int]uint64, cfg.Config.Topology.fleetCandidates())
	coordinatesList := make([]fleetCommitmentCoordinates, 0, 2*cfg.Config.Topology.HeadFleets+cfg.Config.Topology.ChallengerFleets)
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		action, actionErr := exactPlanActionByID(prior, fmt.Sprintf("fleet.commitment.%d", fleet))
		if actionErr != nil {
			return actionErr
		}
		coordinates, coordinateErr := fleetCommitmentActionCoordinates(cfg, action, true)
		if coordinateErr != nil {
			return coordinateErr
		}
		coordinatesList = append(coordinatesList, coordinates)
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		action, actionErr := exactPlanActionByID(prior, fmt.Sprintf("fleet.refresh.commitment.%d", fleet))
		if actionErr != nil {
			return actionErr
		}
		coordinates, coordinateErr := fleetCommitmentActionCoordinates(cfg, action, true)
		if coordinateErr != nil {
			return coordinateErr
		}
		coordinatesList = append(coordinatesList, coordinates)
	}
	for _, coordinates := range coordinatesList {
		priorAction, err := exactPlanActionByID(prior, coordinates.ActionID)
		if err != nil {
			return err
		}
		floor, priorCount, fee, recovery, err := fleetCommitmentRecoveryEnvelope(priorAction)
		if err != nil {
			return err
		}
		if recovery {
			if fee != prior.NativeTransactionFeeLimitRao {
				return fmt.Errorf("fleet commitment recovery %s fee %d differs from prior plan limit %d", priorAction.ID, fee, prior.NativeTransactionFeeLimitRao)
			}
			var addOK bool
			recoveryCounts[coordinates.Fleet], addOK = checkedAdd(recoveryCounts[coordinates.Fleet], priorCount)
			if !addOK {
				return errors.New("fleet commitment recovery count overflows")
			}
		}
		if exactVerifiedPlanAction(prior, entries, coordinates.ConsumerID) {
			if recovery {
				for index := range revised.Actions {
					if revised.Actions[index].ID == coordinates.ActionID {
						revised.Actions[index] = priorAction
						break
					}
				}
			}
			continue
		}
		actionVerified := exactVerifiedPlanAction(prior, entries, coordinates.ActionID)
		if !actionVerified && !recovery {
			continue
		}
		evidence, err := loadFleetCommitmentEvidenceGeneration(stateDir, coordinates.Fleet, coordinates.Generation)
		if err != nil {
			return fmt.Errorf("load %s recovery evidence: %w", coordinates.ActionID, err)
		}
		if _, err := verifiedFleetCommitmentEvidenceAction(stateDir, cfg, prior, entries, coordinates, evidence); err != nil {
			return fmt.Errorf("authenticate %s recovery evidence: %w", coordinates.ActionID, err)
		}
		if recovery && !actionVerified {
			if floor != evidence.CommitmentBlock {
				return fmt.Errorf("pending fleet commitment recovery %s floor %d differs from evidence block %d", coordinates.ActionID, floor, evidence.CommitmentBlock)
			}
			for index := range revised.Actions {
				if revised.Actions[index].ID == coordinates.ActionID {
					revised.Actions[index] = priorAction
					break
				}
			}
			continue
		}
		fresh := fleetCommitmentHasInclusionLifetime(evidence.FinalizedBlock, headBlock, maximumAge, fleetCommitmentInclusionSafetyBlocks)
		if fresh {
			if recovery {
				for index := range revised.Actions {
					if revised.Actions[index].ID == coordinates.ActionID {
						revised.Actions[index] = priorAction
						break
					}
				}
			}
			continue
		}
		newCount, addOK := checkedAdd(priorCount, 1)
		if !addOK {
			return fmt.Errorf("fleet commitment recovery %s count overflows", coordinates.ActionID)
		}
		recoveryCounts[coordinates.Fleet]++
		updated := false
		for index := range revised.Actions {
			action := &revised.Actions[index]
			if action.ID != coordinates.ActionID {
				continue
			}
			action.Parameters = cloneStrings(action.Parameters)
			action.Parameters[fleetCommitmentRecoveryBlockParameter] = strconv.FormatUint(evidence.CommitmentBlock, 10)
			action.Parameters[fleetCommitmentRecoveryCountParameter] = strconv.FormatUint(newCount, 10)
			action.Parameters[fleetCommitmentRecoveryFeeParameter] = strconv.FormatUint(revised.NativeTransactionFeeLimitRao, 10)
			action.AcceptedPriorIntentHashes = nil
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				return err
			}
			updated = true
			break
		}
		if !updated {
			return fmt.Errorf("revised plan has no recovery action %s", coordinates.ActionID)
		}
	}
	var totalRecoveries uint64
	for fleet, count := range recoveryCounts {
		if count == 0 {
			continue
		}
		extraFees, multiplyOK := checkedMul(count, revised.NativeTransactionFeeLimitRao)
		if !multiplyOK {
			return fmt.Errorf("fleet %d recovery fee reserve overflows", fleet)
		}
		fundingID := fmt.Sprintf("fleet.fund-hotkey.%d", fleet)
		updated := false
		for index := range revised.Actions {
			action := &revised.Actions[index]
			if action.ID != fundingID {
				continue
			}
			baseFees, parseErr := strconv.ParseUint(action.Parameters["maximum_fee_rao"], 10, 64)
			fees, addOK := checkedAdd(baseFees, extraFees)
			funding, fundingOK := checkedAdd(fees, revised.LiveFacts.ExistentialDepositRao)
			if parseErr != nil || baseFees == 0 || !addOK || !fundingOK || action.Kind != "substrate-extrinsic" || action.Spend.TAORao != baseFees+revised.LiveFacts.ExistentialDepositRao {
				return fmt.Errorf("fleet recovery funding %s has an invalid base envelope", fundingID)
			}
			action.Parameters = cloneStrings(action.Parameters)
			action.Parameters["maximum_fee_rao"] = strconv.FormatUint(fees, 10)
			action.Parameters[fleetCommitmentFundingCountParameter] = strconv.FormatUint(count, 10)
			action.Spend.TAORao = funding
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				return err
			}
			updated = true
			break
		}
		if !updated {
			return fmt.Errorf("revised plan has no fleet recovery funding %s", fundingID)
		}
		var addOK bool
		totalRecoveries, addOK = checkedAdd(totalRecoveries, count)
		if !addOK {
			return errors.New("total fleet commitment recovery count overflows")
		}
	}
	if totalRecoveries > 0 {
		updated := false
		for index := range revised.Actions {
			action := &revised.Actions[index]
			if action.ID != "wallet.native-fee-reserve" {
				continue
			}
			baseWrites, writesErr := strconv.ParseUint(action.Parameters["native_writes"], 10, 64)
			fee, feeErr := strconv.ParseUint(action.Parameters["maximum_fee_rao"], 10, 64)
			writes, addOK := checkedAdd(baseWrites, totalRecoveries)
			reserve, multiplyOK := checkedMul(writes, fee)
			if writesErr != nil || feeErr != nil || baseWrites == 0 || fee != revised.NativeTransactionFeeLimitRao || !addOK || !multiplyOK || action.Kind != "budget-reserve" {
				return errors.New("wallet native fee reserve has an invalid recovery base")
			}
			action.Parameters = cloneStrings(action.Parameters)
			action.Parameters["native_writes"] = strconv.FormatUint(writes, 10)
			action.Parameters[fleetCommitmentReserveCountParameter] = strconv.FormatUint(totalRecoveries, 10)
			action.Spend.TAORao = reserve
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				return err
			}
			updated = true
			break
		}
		if !updated {
			return errors.New("revised plan has no wallet native fee reserve")
		}
	}
	revised.MaximumSpend, err = maximumActionSpend(revised.Actions)
	return err
}

// Reports whether a same-release plan needs a formal temporal revision. A
// pending recovery already has its own approved intent and must simply resume.
func fleetCommitmentRecoveryRequiredAt(cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, headBlock uint64) (bool, error) {
	if cfg == nil || cfg.Config == nil || plan == nil || headBlock == 0 {
		return false, errors.New("fleet commitment revision check is unavailable")
	}
	maximumAge, err := fleetCommitmentMaximumAgeBlocks(cfg)
	if err != nil {
		return false, err
	}
	for _, action := range plan.Actions {
		coordinates, coordinateErr := fleetCommitmentActionCoordinates(cfg, action, true)
		if coordinateErr != nil {
			if strings.HasPrefix(action.ID, "fleet.commitment.") || strings.HasPrefix(action.ID, "fleet.refresh.commitment.") {
				return false, coordinateErr
			}
			continue
		}
		if exactVerifiedPlanAction(plan, entries, coordinates.ConsumerID) || !exactVerifiedPlanAction(plan, entries, action.ID) {
			continue
		}
		_, _, _, pendingRecovery, envelopeErr := fleetCommitmentRecoveryEnvelope(action)
		if envelopeErr != nil {
			return false, envelopeErr
		}
		if pendingRecovery {
			// A verified recovery can itself expire and requires the next
			// revision; an unverified one is still the approved replacement.
			if !exactVerifiedPlanAction(plan, entries, action.ID) {
				continue
			}
		}
		evidence, loadErr := loadFleetCommitmentEvidenceGeneration(stateDir, coordinates.Fleet, coordinates.Generation)
		if loadErr != nil {
			return false, loadErr
		}
		if !fleetCommitmentHasInclusionLifetime(evidence.FinalizedBlock, headBlock, maximumAge, fleetCommitmentInclusionSafetyBlocks) {
			return true, nil
		}
	}
	return false, nil
}

// Detects whether any relevant commitment evidence exists before doing a live
// head read for same-release temporal revision checks.
func hasFleetCommitmentRecoveryEvidence(stateDir string) bool {
	publicDir := filepath.Join(stateDir, "public")
	entries, err := os.ReadDir(publicDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".commitment.json") || strings.HasSuffix(entry.Name(), ".refresh.commitment.json")) {
			return true
		}
	}
	return false
}

// Reads live finalized facts only after local evidence shows temporal recovery
// could apply. The returned revision is still produced by the ordinary doctor,
// transaction-recovery, lineage, budget, and two-build approval path.
func fleetCommitmentRecoveryRequired(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry) (bool, error) {
	if !hasFleetCommitmentRecoveryEvidence(stateDir) {
		return false, nil
	}
	current, err := ReadSetupFacts(ctx, cfg)
	if err != nil {
		return false, fmt.Errorf("read fleet commitment recovery facts: %w", err)
	}
	return fleetCommitmentRecoveryRequiredAt(cfg, stateDir, plan, entries, max(current.FinalizedBlock, current.EVMFinalizedBlock))
}

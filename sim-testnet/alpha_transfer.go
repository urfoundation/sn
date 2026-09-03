package main

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const (
	alphaPriceQ9Scale                         = uint64(1_000_000_000)
	basisPointScale                           = uint64(10_000)
	alphaTransferDestinationRoundingAllowance = uint64(1)
	reserveRuntimeShareTransitionCount        = uint64(2)
	reserveRoundingAllowancePerCallRao        = reserveRuntimeShareTransitionCount * alphaTransferDestinationRoundingAllowance
	alphaRecoveryPlanHashParameter            = "recovery_plan_hash"
	alphaRecoveryIntentHashParameter          = "recovery_intent_hash"
	alphaRecoveryTransactionHashParameter     = "recovery_transaction_hash"
	alphaRecoveryBlockParameter               = "recovery_block"
	alphaRecoveryBlockHashParameter           = "recovery_block_hash"
	alphaRepairForActionParameter             = "repair_for_action"
	alphaRepairMinimumIncrementParameter      = "minimum_increment_from_recovered_prestate_rao"
	alphaRepairMinimumDestinationParameter    = "minimum_destination_stake_rao"
	alphaRepairReserveShareParameter          = "reserve_share_repair"
	alphaRepairCumulativeBeforeParameter      = "cumulative_alpha_before_repair_rao"
	alphaRepairCumulativeLimitParameter       = "cumulative_alpha_limit_rao"
	alphaRepairMaximumTrancheParameter        = "maximum_reserve_repair_tranche_rao"
)

// A reserve-share repair is a fixed, approval-bound transfer tranche rather
// than an amount sized from a moving emission snapshot. The live target is
// still checked immediately before signing and at the finalized transaction
// block. This keeps plan review stable without turning the spend cap into a
// best-effort or runtime-selected amount.
func reserveShareRepairTerms(action Action) (uint16, uint16, bool, error) {
	encoded, present := action.Parameters[alphaRepairReserveShareParameter]
	if !present {
		return 0, 0, false, nil
	}
	if encoded != "true" {
		return 0, 0, false, fmt.Errorf("alpha repair %s has invalid reserve-share mode %q", action.ID, encoded)
	}
	kind, index, err := alphaTransferTargetFromActionID(action.ID)
	if err != nil || kind != "validator" || index != 1 || action.Parameters[alphaRepairForActionParameter] != "alpha.transfer.validator.1" {
		return 0, 0, false, stateMismatchError(err, "alpha repair %s is not the reserve-validator repair", action.ID)
	}
	target, targetErr := strconv.ParseUint(action.Parameters["reserve_target_share_bps"], 10, 16)
	minimum, minimumErr := strconv.ParseUint(action.Parameters["reserve_minimum_share_bps"], 10, 16)
	if targetErr != nil || minimumErr != nil || minimum <= 5_000 || target <= minimum || target > 9_000 {
		return 0, 0, false, fmt.Errorf("alpha repair %s has invalid reserve-share bounds", action.ID)
	}
	if action.Parameters[alphaRepairMinimumIncrementParameter] != "" || action.Parameters[alphaRepairMinimumDestinationParameter] != "" {
		return 0, 0, false, fmt.Errorf("alpha repair %s mixes reserve-share and destination repair modes", action.ID)
	}
	return uint16(target), uint16(minimum), true, nil
}

// A destination-floor repair may converge without a transaction when its
// postcondition was already true before execution. If it did broadcast, the
// exact finalized delta remains mandatory even when later stake also satisfies
// the floor.
func alphaRepairPostconditionRequiresTransaction(current, minimum uint64, hasTransaction bool) (bool, error) {
	if minimum == 0 {
		return false, errors.New("alpha repair postcondition minimum is zero")
	}
	if hasTransaction {
		return true, nil
	}
	if current < minimum {
		return false, fmt.Errorf("alpha repair converged without a transaction at %d, below required stake %d", current, minimum)
	}
	return false, nil
}

func alphaTransferTargetFromActionID(id string) (string, int, error) {
	prefixes := []struct {
		prefix, kind  string
		allowSequence bool
	}{
		{prefix: "alpha.transfer.operator-deposit.", kind: "operator-deposit"},
		{prefix: "alpha.transfer.validator.", kind: "validator"},
		{prefix: "alpha.repair.operator-deposit.", kind: "operator-deposit", allowSequence: true},
		{prefix: "alpha.repair.validator.", kind: "validator", allowSequence: true},
	}
	for _, candidate := range prefixes {
		if !strings.HasPrefix(id, candidate.prefix) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(id, candidate.prefix), ".")
		if len(parts) == 0 || (!candidate.allowSequence && len(parts) != 1) || (candidate.allowSequence && len(parts) > 2) {
			return "", 0, fmt.Errorf("invalid alpha-transfer action id %q", id)
		}
		index, err := strconv.Atoi(parts[0])
		if err != nil || index <= 0 {
			return "", 0, fmt.Errorf("invalid alpha-transfer action id %q", id)
		}
		if len(parts) == 2 {
			sequence, sequenceErr := strconv.Atoi(parts[1])
			if sequenceErr != nil || sequence < 2 {
				return "", 0, fmt.Errorf("invalid alpha-transfer action id %q", id)
			}
		}
		return candidate.kind, index, nil
	}
	return "", 0, fmt.Errorf("unsupported alpha-transfer action id %q", id)
}

// alphaTransferCreditedRao validates the point-in-time destination entitlement
// change produced by runtime 453. transfer_stake_and_hotkey conserves the exact
// integer amount in TotalHotkeyAlpha, while the destination coldkey is represented
// by a SafeFloat share and getStake floors that entitlement. The pinned runtime's
// precision contract therefore permits at most one rao of destination under-report.
// An increase above the signed amount is also rejected so an unrelated transfer in
// the same block cannot be mistaken for our postcondition.
func alphaTransferCreditedRao(before, after, requested, maximumShortfall uint64) (uint64, error) {
	if requested == 0 || maximumShortfall > requested {
		return 0, errors.New("invalid alpha-transfer rounding envelope")
	}
	if after < before {
		return 0, fmt.Errorf("alpha-transfer destination regressed from %d to %d", before, after)
	}
	credited := after - before
	minimum := requested - maximumShortfall
	if credited < minimum || credited > requested {
		return 0, fmt.Errorf("alpha-transfer destination credit %d is outside [%d,%d]", credited, minimum, requested)
	}
	return credited, nil
}

func alphaTransferMinimumCreditRao(requested uint64) (uint64, error) {
	if requested <= alphaTransferDestinationRoundingAllowance {
		return 0, errors.New("alpha-transfer amount does not exceed its rounding allowance")
	}
	return requested - alphaTransferDestinationRoundingAllowance, nil
}

// Decide whether a new repair transaction is still necessary and prove its
// bounded minimum credit can reach the approved destination floor. A recorded
// current-intent transaction must be recovered even when its effect is live.
func alphaRepairPrebroadcast(current, minimum, minimumCredit uint64, resumed bool) (bool, error) {
	if minimum == 0 || minimumCredit == 0 {
		return false, errors.New("alpha repair prebroadcast bound is zero")
	}
	if !resumed && current >= minimum {
		return true, nil
	}
	after, ok := checkedAdd(current, minimumCredit)
	if !ok || after < minimum {
		return false, fmt.Errorf("bounded alpha repair cannot reach minimum stake %d from %d with credit %d", minimum, current, minimumCredit)
	}
	return false, nil
}

func alphaTransferRoundingShortfall(action Action) (uint64, error) {
	value := action.Parameters["maximum_destination_rounding_shortfall_rao"]
	if value == "" {
		// V5-v7 plans predate the explicit field and may be persisted v452
		// evidence. Runtime 453 retains the same share-pool semantics, so their
		// migration envelope remains the one-rao bound, never an unbounded tolerance.
		return alphaTransferDestinationRoundingAllowance, nil
	}
	shortfall, err := strconv.ParseUint(value, 10, 64)
	if err != nil || shortfall != alphaTransferDestinationRoundingAllowance {
		return 0, fmt.Errorf("alpha transfer %s has invalid destination rounding shortfall %q", action.ID, value)
	}
	minimumCredit, err := strconv.ParseUint(action.Parameters["minimum_destination_credit_rao"], 10, 64)
	if err != nil || action.Spend.AlphaRao <= shortfall || minimumCredit != action.Spend.AlphaRao-shortfall {
		return 0, fmt.Errorf("alpha transfer %s has invalid minimum destination credit", action.ID)
	}
	return shortfall, nil
}

// alphaTransferTAOEquivalentRao mirrors runtime 453's fixed-point floor:
// floor(alpha_rao * current_alpha_price_q9 / 1e9).
func alphaTransferTAOEquivalentRao(alphaRao, priceQ9 uint64) (uint64, error) {
	if alphaRao == 0 || priceQ9 == 0 {
		return 0, errors.New("alpha transfer amount and price must be nonzero")
	}
	value := new(big.Int).Mul(new(big.Int).SetUint64(alphaRao), new(big.Int).SetUint64(priceQ9))
	value.Quo(value, new(big.Int).SetUint64(alphaPriceQ9Scale))
	if !value.IsUint64() {
		return 0, errors.New("alpha transfer TAO equivalent exceeds uint64")
	}
	return value.Uint64(), nil
}

// minimumAlphaTransferRao returns the smallest alpha amount whose runtime
// TAO-equivalent floor reaches DefaultMinTransfer plus the configured basis-point
// margin. All arithmetic is unbounded until the final checked conversion.
func minimumAlphaTransferRao(defaultMinTransferRao, priceQ9 uint64, marginBPS uint16) (uint64, error) {
	if defaultMinTransferRao == 0 || priceQ9 == 0 {
		return 0, errors.New("runtime alpha-transfer minimum and price must be nonzero")
	}
	if marginBPS > 5_000 {
		return 0, fmt.Errorf("alpha-transfer margin %d exceeds 5000 basis points", marginBPS)
	}
	marginScale := basisPointScale + uint64(marginBPS)
	minimumTAO := ceilBigQuotient(
		new(big.Int).Mul(new(big.Int).SetUint64(defaultMinTransferRao), new(big.Int).SetUint64(marginScale)),
		new(big.Int).SetUint64(basisPointScale),
	)
	minimumAlpha := ceilBigQuotient(
		new(big.Int).Mul(minimumTAO, new(big.Int).SetUint64(alphaPriceQ9Scale)),
		new(big.Int).SetUint64(priceQ9),
	)
	if !minimumAlpha.IsUint64() || minimumAlpha.Sign() <= 0 {
		return 0, errors.New("minimum alpha transfer exceeds uint64")
	}
	return minimumAlpha.Uint64(), nil
}

func ceilBigQuotient(numerator, denominator *big.Int) *big.Int {
	if numerator == nil || denominator == nil || numerator.Sign() < 0 || denominator.Sign() <= 0 {
		return new(big.Int)
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

func alphaShareMeets(totalAlphaRao, participantAlphaRao uint64, minimumShareBPS uint16) bool {
	if totalAlphaRao == 0 || participantAlphaRao > totalAlphaRao || minimumShareBPS == 0 || uint64(minimumShareBPS) > basisPointScale {
		return false
	}
	left := new(big.Int).Mul(new(big.Int).SetUint64(participantAlphaRao), new(big.Int).SetUint64(basisPointScale))
	right := new(big.Int).Mul(new(big.Int).SetUint64(totalAlphaRao), new(big.Int).SetUint64(uint64(minimumShareBPS)))
	return left.Cmp(right) >= 0
}

// Derive the conservative amount which can leave one source position without
// moving conviction lock mass or violating either position-local or
// coldkey-wide miner collateral. This mirrors all same-subnet transfer guards
// while deliberately treating unrolled stored lock mass as unavailable.
func alphaTransferCapacity(sourcePositionRao, coldkeyTotalRao, storedLockRao, positionCollateralRao, coldkeyCollateralRao uint64) (uint64, error) {
	if sourcePositionRao == 0 || coldkeyTotalRao < sourcePositionRao {
		return 0, errors.New("invalid alpha source position or coldkey total")
	}
	if storedLockRao > coldkeyTotalRao {
		return 0, errors.New("alpha source lock exceeds coldkey total")
	}
	if positionCollateralRao > sourcePositionRao || coldkeyCollateralRao > coldkeyTotalRao || positionCollateralRao > coldkeyCollateralRao {
		return 0, errors.New("invalid alpha source collateral")
	}
	positionFree := sourcePositionRao - positionCollateralRao
	coldkeyCollateralFree := coldkeyTotalRao - coldkeyCollateralRao
	coldkeyUnlocked := coldkeyTotalRao - storedLockRao
	return min(positionFree, coldkeyCollateralFree, coldkeyUnlocked), nil
}

// reserveValidatorTransferRao computes an exact transfer which reaches the
// configured share at the approved registered-alpha snapshot. The runtime
// minimum can only increase the reserve's resulting share.
func reserveValidatorTransferRao(totalAlphaRao, existingReserveAlphaRao, minimumTransferAlphaRao uint64, targetShareBPS uint16) (uint64, uint64, error) {
	if totalAlphaRao == 0 || existingReserveAlphaRao > totalAlphaRao || targetShareBPS <= 5_000 || targetShareBPS > 9_000 || minimumTransferAlphaRao == 0 {
		return 0, 0, errors.New("invalid reserve-validator transfer inputs")
	}
	desired := ceilBigQuotient(
		new(big.Int).Mul(new(big.Int).SetUint64(totalAlphaRao), new(big.Int).SetUint64(uint64(targetShareBPS))),
		new(big.Int).SetUint64(basisPointScale),
	)
	if !desired.IsUint64() {
		return 0, 0, errors.New("reserve-validator target exceeds uint64")
	}
	desiredRao := desired.Uint64()
	minimumCredit := uint64(0)
	if desiredRao > existingReserveAlphaRao {
		minimumCredit = desiredRao - existingReserveAlphaRao
	}
	if minimumCredit < minimumTransferAlphaRao {
		minimumCredit = minimumTransferAlphaRao
	}
	transfer, ok := checkedAdd(minimumCredit, alphaTransferDestinationRoundingAllowance)
	if !ok {
		return 0, 0, errors.New("reserve-validator rounding allowance overflows uint64")
	}
	// Size the target from the minimum share-pool credit, not the signed amount.
	// A one-rao entitlement floor must not turn the reserve into a minority.
	finalReserve, ok := checkedAdd(existingReserveAlphaRao, minimumCredit)
	if !ok || finalReserve > totalAlphaRao || !alphaShareMeets(totalAlphaRao, finalReserve, targetShareBPS) {
		return 0, 0, errors.New("reserve-validator transfer cannot reach its target share")
	}
	return transfer, finalReserve, nil
}

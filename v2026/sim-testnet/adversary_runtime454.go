// Runtime 454 decision models preserve the exact security and accounting
// boundaries reviewed from the pinned Subtensor source. They do not replace
// FRAME execution; live actors use them as bounded oracles while independently
// pinning the deployed runtime artifact.
package main

import (
	"errors"
	"math/big"
)

// Classifies the small runtime-call surface reachable from pallet-contracts.
type runtime454ContractCallClass uint8

const (
	runtime454ContractCallUnknown runtime454ContractCallClass = iota
	runtime454ContractProxyEnvelope
	runtime454ContractTransferStake
	runtime454ContractMoveStake
	runtime454ContractAddStake
	runtime454ContractUtilityBatch
)

// Mirrors the outer pallet-contracts filter after the v453 proxy-filter
// intersection repair. Only the proxy envelope and transfer_stake are admitted.
func runtime454ContractCallAllowed(call runtime454ContractCallClass) bool {
	return call == runtime454ContractProxyEnvelope || call == runtime454ContractTransferStake
}

// Requires the contract filter at both the proxy envelope and inherited inner
// dispatch, plus the user's independently configured proxy delegation.
func runtime454ContractProxyDispatchAllowed(finalCall runtime454ContractCallClass, delegationAllowsFinal bool) bool {
	return runtime454ContractCallAllowed(runtime454ContractProxyEnvelope) &&
		runtime454ContractCallAllowed(finalCall) && delegationAllowsFinal
}

// Carries only the state consulted while selecting and charging one relationship.
type runtime454RootRelationship struct {
	HasRootStake    bool
	BasketWatermark int64
	RawAlphaRows    uint32
	RawAlphaV2Rows  uint32
}

// Selects root-relevant relationships and enforces the independent candidate
// scan and raw-row work limits used by a coldkey-wide claim.
func runtime454SelectRootRelationships(relationships []runtime454RootRelationship, workLimit uint32) ([]int, error) {
	if workLimit == 0 {
		return nil, errors.New("root claim work limit is zero")
	}
	if uint64(len(relationships)) > uint64(workLimit) {
		return nil, errors.New("root claim candidate scan exceeds its declared work")
	}
	selectedIndexes := make([]int, 0, len(relationships))
	work := uint64(0)
	for index, relationship := range relationships {
		if !relationship.HasRootStake && relationship.BasketWatermark >= 0 {
			continue
		}
		selectedIndexes = append(selectedIndexes, index)
		work++
		work += uint64(relationship.RawAlphaRows)
		work += uint64(relationship.RawAlphaV2Rows)
		if work > uint64(workLimit) {
			return nil, errors.New("root claim raw basket rows exceed their declared work")
		}
	}
	return selectedIndexes, nil
}

// Chooses one atomic alpha when a positive marked entitlement would otherwise
// be stranded by proportional integer flooring on a live non-root holding.
func runtime454MinimumAlphaTake(proportionalTake, markedEntitlement uint64, root, terminal bool) uint64 {
	if proportionalTake == 0 && markedEntitlement > 0 && !root && !terminal {
		return 1
	}
	return proportionalTake
}

// Caps a nonterminal claimant at its marked entitlement while allowing the
// final shareholder to drain every realized rao from an otherwise empty fund.
func runtime454ClaimPayout(realizedTAO, markedEntitlement uint64, finalShareholder bool) uint64 {
	if finalShareholder || realizedTAO <= markedEntitlement {
		return realizedTAO
	}
	return markedEntitlement
}

// Describes the post-withdrawal share state needed to detect sub-rao residue.
type runtime454ShareWithdrawal struct {
	UpdatedSharedValue uint64
	CurrentShare       uint64
	Denominator        uint64
}

// Removes a positive share whose exact post-withdrawal integer value is zero,
// preserving the equality between remaining shares and their denominator.
func runtime454CanonicalizeWithdrawalDust(withdrawal runtime454ShareWithdrawal) (uint64, uint64, bool, error) {
	if withdrawal.Denominator == 0 || withdrawal.CurrentShare > withdrawal.Denominator {
		return withdrawal.CurrentShare, withdrawal.Denominator, false, errors.New("share withdrawal denominator is invalid")
	}
	if withdrawal.CurrentShare == 0 {
		return 0, withdrawal.Denominator, false, nil
	}
	valueNumerator := new(big.Int).Mul(
		new(big.Int).SetUint64(withdrawal.UpdatedSharedValue),
		new(big.Int).SetUint64(withdrawal.CurrentShare),
	)
	value := new(big.Int).Quo(valueNumerator, new(big.Int).SetUint64(withdrawal.Denominator))
	if value.Sign() != 0 {
		return withdrawal.CurrentShare, withdrawal.Denominator, false, nil
	}
	return 0, withdrawal.Denominator - withdrawal.CurrentShare, true, nil
}

// Starts dependent relationship cleanup only after the positive completion
// marker exists; cursor absence is deliberately not evidence of completion.
func runtime454DependentCleanupAllowed(seedInProgress, storageCleanupComplete bool) bool {
	return !seedInProgress && storageCleanupComplete
}

// Prices both the full-claim and scan dimensions without integer wraparound.
func runtime454DeclaredClaimWeight(fullClaimWeight, scanWeight uint64) uint64 {
	if ^uint64(0)-fullClaimWeight < scanWeight {
		return ^uint64(0)
	}
	return fullClaimWeight + scanWeight
}

// Exercises every changed boundary as part of each concurrent custody sample,
// returning a stable case count suitable for the immutable actor transcript.
func runtime454AdversaryBoundaryMetrics(sequence uint64) (uint64, error) {
	var passed uint64
	if !runtime454ContractProxyDispatchAllowed(runtime454ContractTransferStake, true) ||
		runtime454ContractProxyDispatchAllowed(runtime454ContractMoveStake, true) {
		return 0, errors.New("runtime 454 contract call filter widened or blocked transfer_stake")
	}
	passed++
	relationships := []runtime454RootRelationship{
		{HasRootStake: true, RawAlphaRows: 127, RawAlphaV2Rows: 128},
		{RawAlphaRows: uint32(sequence % 17)},
	}
	selectedIndexes, err := runtime454SelectRootRelationships(relationships, 256)
	if err != nil || len(selectedIndexes) != 1 || selectedIndexes[0] != 0 {
		return 0, errors.New("runtime 454 root claim selection or exact raw-row boundary failed")
	}
	passed++
	if _, err := runtime454SelectRootRelationships([]runtime454RootRelationship{{HasRootStake: true, RawAlphaRows: 128, RawAlphaV2Rows: 128}}, 256); err == nil {
		return 0, errors.New("runtime 454 root claim admitted raw work above its bound")
	}
	passed++
	if runtime454MinimumAlphaTake(0, 1+sequence%1_000, false, false) != 1 ||
		runtime454ClaimPayout(1_000, 91, false) != 91 {
		return 0, errors.New("runtime 454 minimum-unit payout boundary failed")
	}
	passed++
	share, denominator, removed, err := runtime454CanonicalizeWithdrawalDust(runtime454ShareWithdrawal{
		UpdatedSharedValue: 3, CurrentShare: 1, Denominator: 4,
	})
	if err != nil || !removed || share != 0 || denominator != 3 {
		return 0, errors.New("runtime 454 withdrawal dust was not canonicalized")
	}
	passed++
	if runtime454DependentCleanupAllowed(false, false) ||
		!runtime454DependentCleanupAllowed(false, true) ||
		runtime454DependentCleanupAllowed(true, true) {
		return 0, errors.New("runtime 454 migration completion marker boundary failed")
	}
	passed++
	if runtime454DeclaredClaimWeight(10, 7) != 17 || runtime454DeclaredClaimWeight(^uint64(0)-2, 7) != ^uint64(0) {
		return 0, errors.New("runtime 454 declared claim weight omitted or wrapped its scan dimension")
	}
	passed++
	return passed, nil
}

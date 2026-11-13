// Production scheduling and recovery share one pinned policy inventory. An
// approved rate amendment adds one authenticated version, never an arbitrary
// extension of the historical-policy allowance.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
)

// All snapshots belong to the caller's finalized block. The last snapshot is
// either the active accelerated policy or its already scheduled production one.
type productionPolicyHistory struct {
	currentEpoch uint64
	policies     []stabi.STCoordinatorPolicySnapshot
	active       stabi.STCoordinatorPolicySnapshot
	scheduled    bool
}

// Preserve the fresh/migrated allowance, with exactly one extra slot when the
// approved amendment binds the preceding policy and the current policy document.
func validateProductionPolicyHistory(cfg *ResolvedConfig, plan *SetupPlan, history *productionPolicyHistory) error {
	if cfg == nil || cfg.Policy == nil || history == nil || len(history.policies) == 0 {
		return errors.New("production policy history is incomplete")
	}
	lastIndex := len(history.policies) - 1
	last := history.policies[lastIndex]
	history.scheduled = productionPolicyMatches(cfg, last)
	acceleratedIndex := lastIndex
	if history.scheduled {
		acceleratedIndex--
	}
	minimumIndex, maximumIndex := 0, 1
	var priorCfg *ResolvedConfig
	if plan != nil && plan.PolicyRateAmendment != nil {
		amended, err := configWithPolicyRateAmendment(cfg, plan)
		if err != nil {
			return fmt.Errorf("production policy amendment approval: %w", err)
		}
		previous := *amended
		previous.Policy = amended.previousPolicy
		previous.PolicyHash, err = previous.Policy.HashHex()
		if err != nil {
			return err
		}
		priorCfg = &previous
		minimumIndex, maximumIndex = 1, 2
	}
	if acceleratedIndex < minimumIndex || acceleratedIndex > maximumIndex || !bootstrapPolicyMatches(cfg, history.policies[acceleratedIndex]) {
		return fmt.Errorf("coordinator has an unreviewed %d-version production policy history", len(history.policies))
	}
	if priorCfg != nil && !bootstrapPolicyMatches(priorCfg, history.policies[acceleratedIndex-1]) {
		return errors.New("production policy history lacks the exact approved rate predecessor")
	}
	for index := 1; index < len(history.policies); index++ {
		prior, next := history.policies[index-1], history.policies[index]
		if next.EffectiveEpoch <= prior.EffectiveEpoch || next.EffectiveBlock <= prior.EffectiveBlock {
			return errors.New("production policy history has non-increasing effective coordinates")
		}
	}
	accelerated := history.policies[acceleratedIndex]
	if history.currentEpoch < accelerated.EffectiveEpoch {
		return errors.New("production transition requires the approved accelerated policy to be active")
	}
	wantActive := accelerated
	if history.scheduled && last.EffectiveEpoch <= history.currentEpoch {
		wantActive = last
	}
	if !policySnapshotEqual(history.active, wantActive) {
		return errors.New("active production-transition policy differs from the pinned inventory")
	}
	return nil
}

// Read at most the original/migrated pair, the approved amendment, and the
// production version. Both scheduling and receipt verification use this reader.
func readProductionPolicyHistory(ctx context.Context, cfg *ResolvedConfig, plan *SetupPlan, owner *EvmTxManager, address common.Address, block uint64) (*productionPolicyHistory, error) {
	if owner == nil || owner.client == nil || block == 0 {
		return nil, errors.New("production policy history reader is unavailable")
	}
	coordinator := stabi.NewSTCoordinator()
	count, err := rawCoordinatorCallAt(ctx, owner, address, coordinator.PackPolicyCount(), coordinator.UnpackPolicyCount, block)
	if err != nil {
		return nil, fmt.Errorf("read production policy count: %w", err)
	}
	if count == nil || !count.IsUint64() || count.Sign() <= 0 || count.Uint64() > 4 {
		return nil, fmt.Errorf("coordinator has an unreviewed production policy count %v", count)
	}
	current, err := rawCoordinatorCallAt(ctx, owner, address, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch, block)
	if err != nil {
		return nil, fmt.Errorf("read production transition epoch: %w", err)
	}
	if current == nil || !current.IsUint64() {
		return nil, errors.New("production transition current epoch is not uint64")
	}
	history := &productionPolicyHistory{currentEpoch: current.Uint64(), policies: make([]stabi.STCoordinatorPolicySnapshot, count.Uint64())}
	for index := range history.policies {
		policy, err := rawCoordinatorCallAt(ctx, owner, address, coordinator.PackPolicyByIndex(new(big.Int).SetUint64(uint64(index))), coordinator.UnpackPolicyByIndex, block)
		if err != nil {
			return nil, fmt.Errorf("read production policy index %d: %w", index, err)
		}
		history.policies[index] = policy
	}
	history.active, err = rawCoordinatorCallAt(ctx, owner, address, coordinator.PackPolicyAt(current), coordinator.UnpackPolicyAt, block)
	if err != nil {
		return nil, fmt.Errorf("read active production-transition policy: %w", err)
	}
	if err := validateProductionPolicyHistory(cfg, plan, history); err != nil {
		return nil, err
	}
	return history, nil
}

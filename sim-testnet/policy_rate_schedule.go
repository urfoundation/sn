// Rate amendments reuse coordinator governance while authenticating the full
// existing policy inventory. No reserve or epoch accounting is rewritten.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Accept the exact predecessor, a future successor, or that same successor
// after activation. A foreign pending policy cannot be overwritten or skipped.
func validatePolicyRateSchedule(cfg *ResolvedConfig, previous *protocol.Policy, current, count uint64, active, last stabi.STCoordinatorPolicySnapshot) error {
	if cfg == nil || validateFuturePolicyRateAmendment(previous, cfg.Policy) != nil || count == 0 || count > 64 {
		return errors.New("rate amendment policy context or inventory is invalid")
	}
	prior := *cfg
	prior.Policy = previous
	var err error
	prior.PolicyHash, err = previous.HashHex()
	if err != nil {
		return err
	}
	activeOld, activeNew := bootstrapPolicyMatches(&prior, active), bootstrapPolicyMatches(cfg, active)
	lastOld, lastNew := bootstrapPolicyMatches(&prior, last), bootstrapPolicyMatches(cfg, last)
	if active.EffectiveEpoch > current || last.EffectiveEpoch < active.EffectiveEpoch {
		return errors.New("rate amendment policy epochs are inconsistent")
	}
	if activeNew {
		if !lastNew || last.EffectiveEpoch != active.EffectiveEpoch || last.EffectiveBlock != active.EffectiveBlock {
			return errors.New("rate amendment active successor has a foreign pending policy")
		}
		return nil
	}
	if !activeOld {
		return errors.New("rate amendment active policy is not its authenticated predecessor")
	}
	if lastNew {
		if last.EffectiveEpoch <= current || last.EffectiveBlock <= active.EffectiveBlock {
			return errors.New("rate amendment successor is not future-effective")
		}
		return nil
	}
	if !lastOld || last.EffectiveEpoch != active.EffectiveEpoch || last.EffectiveBlock != active.EffectiveBlock || count == 64 {
		return errors.New("rate amendment has a foreign pending policy or no remaining policy slot")
	}
	return nil
}

// All observations use one finalized block and the configured authenticated
// EVM route; the scheduling transaction remains owned by the approved journal.
func readPolicyRateSchedule(ctx context.Context, cfg *ResolvedConfig, client *ethclient.Client, proxy common.Address, block uint64, previous *protocol.Policy) (uint64, uint64, stabi.STCoordinatorPolicySnapshot, stabi.STCoordinatorPolicySnapshot, error) {
	var active, last stabi.STCoordinatorPolicySnapshot
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return 0, 0, active, last, err
	}
	readUint := func(method string) (uint64, error) {
		values, err := contractCallAt(ctx, client, proxy, parsed, method, block)
		if err != nil || len(values) != 1 {
			return 0, stateMismatchError(err, "rate amendment %s result", method)
		}
		value, ok := values[0].(*big.Int)
		if !ok || !value.IsUint64() {
			return 0, fmt.Errorf("rate amendment %s is not uint64", method)
		}
		return value.Uint64(), nil
	}
	current, err := readUint("currentEpoch")
	if err != nil {
		return 0, 0, active, last, err
	}
	count, err := readUint("policyCount")
	if err != nil || count == 0 || count > 64 {
		return 0, 0, active, last, stateMismatchError(err, "rate amendment policy count %d", count)
	}
	values, err := contractCallAt(ctx, client, proxy, parsed, "policyAt", block, new(big.Int).SetUint64(current))
	if err != nil {
		return 0, 0, active, last, err
	}
	active, err = coordinatorPolicy(values)
	if err != nil {
		return 0, 0, active, last, err
	}
	values, err = contractCallAt(ctx, client, proxy, parsed, "policyByIndex", block, new(big.Int).SetUint64(count-1))
	if err != nil {
		return 0, 0, active, last, err
	}
	last, err = coordinatorPolicy(values)
	if err != nil {
		return 0, 0, active, last, err
	}
	err = validatePolicyRateSchedule(cfg, previous, current, count, active, last)
	return current, count, active, last, err
}

// Scheduling has no authority to change balances, roots or prior policies.
// Reentry verifies the exact new policy instead of scheduling another copy.
func (self *Executor) schedulePolicyRateAmendment(ctx context.Context, action Action) error {
	if err := validatePolicyRateAmendmentPlan(self.plan); err != nil {
		return err
	}
	if self.plan.PolicyHash != self.cfg.PolicyHash {
		return errors.New("rate amendment executor policy differs from approval")
	}
	head, err := finalizedEVMHead(ctx, self.owner.client)
	if err != nil {
		return err
	}
	proxy := self.payloads.Manifest.CoordinatorProxy
	_, _, active, last, err := readPolicyRateSchedule(ctx, self.cfg, self.owner.client, proxy, head.Number, &self.plan.PolicyRateAmendment.Previous)
	if err != nil {
		return err
	}
	if bootstrapPolicyMatches(self.cfg, active) || bootstrapPolicyMatches(self.cfg, last) {
		return nil
	}
	window, err := waitFutureEpochTransactionWindow(ctx, self.owner, proxy, stabi.NewSTCoordinator())
	if err != nil {
		return err
	}
	// Read again after any boundary wait; another scheduled generation must
	// not change the predecessor or consume its final slot unnoticed.
	head, err = finalizedEVMHead(ctx, self.owner.client)
	if err != nil {
		return err
	}
	current, _, active, last, err := readPolicyRateSchedule(ctx, self.cfg, self.owner.client, proxy, head.Number, &self.plan.PolicyRateAmendment.Previous)
	if err != nil {
		return err
	}
	if bootstrapPolicyMatches(self.cfg, active) || bootstrapPolicyMatches(self.cfg, last) {
		return nil
	}
	if window.EffectiveEpoch <= current {
		return errors.New("rate amendment future transaction window expired")
	}
	next := active
	next.PolicyHash, err = decodeHash(self.cfg.PolicyHash)
	if err != nil {
		return err
	}
	next.EffectiveEpoch, next.EffectiveBlock = window.EffectiveEpoch, 0
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	data, err := parsed.Pack("schedulePolicy", next)
	if err != nil {
		return err
	}
	_, err = self.owner.Send(ctx, self.plan.PlanHash, action, &proxy, big.NewInt(0), data)
	return err
}

package chain

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/urfoundation/sn/crv4"
)

// AddStakeRequest stakes AmountRao TAO (in rao) from the signing Coldkey's
// free balance onto Hotkey on Netuid. LimitPriceRao selects add_stake_limit
// with that maximum pool price (TAO rao per alpha); zero selects add_stake.
type AddStakeRequest struct {
	Command       string
	Netuid        uint16
	Hotkey        [32]byte
	Coldkey       *crv4.Keypair
	AmountRao     uint64
	LimitPriceRao uint64
	AllowPartial  bool
	FeeLimitRao   uint64
	Allowed       []crv4.RuntimeArtifactIdentity
	Journal       *Journal
	Apply         bool
	Output        io.Writer
}

// AddStakeResult reports the pool economics and the hotkey's alpha before and
// (when applied) after the finalized extrinsic.
type AddStakeResult struct {
	Runtime          FinalizedRuntime
	UID              uint16
	Pool             PoolReserves
	FreeBalanceRao   uint64
	HotkeyAlphaRao   uint64
	HotkeyAlphaAfter uint64
	Submit           *SubmitResult
}

// AddStakeDecision applies the pure staking rules: the hotkey must be
// registered, the coldkey must hold the amount, and an explicit limit price
// must not already be below the pool price.
func AddStakeDecision(registered bool, freeBalanceRao, amountRao uint64, pool PoolReserves, limitPriceRao uint64) error {
	if !registered {
		return errors.New("hotkey has no UID on this netuid; register it before staking")
	}
	if amountRao == 0 {
		return errors.New("stake amount is zero")
	}
	if freeBalanceRao < amountRao {
		return fmt.Errorf("coldkey free balance %d rao is below the %d rao stake amount", freeBalanceRao, amountRao)
	}
	if limitPriceRao != 0 && pool.AlphaInRao != 0 && limitPriceRao < pool.PriceQ9() {
		return fmt.Errorf("limit price %d rao/alpha is below the pool price %d rao/alpha; the runtime would reject the purchase", limitPriceRao, pool.PriceQ9())
	}
	return nil
}

// AddStake authenticates the finalized runtime against the caller's pin,
// prints the pool economics, applies AddStakeDecision, and (only with Apply)
// submits add_stake / add_stake_limit and reports the hotkey's alpha delta.
func AddStake(ctx context.Context, chain *crv4.Chain, req AddStakeRequest) (AddStakeResult, error) {
	var result AddStakeResult
	if req.Coldkey == nil || req.Output == nil || req.Netuid == 0 || req.Hotkey == ([32]byte{}) {
		return result, errors.New("stake request is incomplete")
	}
	bound, runtime, err := AuthenticateFinalizedRuntimeContext(ctx, chain, req.Allowed...)
	if err != nil {
		return result, err
	}
	result.Runtime = runtime
	fmt.Fprintf(req.Output, "runtime: %s/%d/%d/%d at finalized block %d (%s)\n", runtime.Artifact.Version.SpecName, runtime.Artifact.Version.SpecVersion, runtime.Artifact.Version.TransactionVersion, runtime.Artifact.Version.StateVersion, runtime.Number, runtime.Hash.Hex())
	uid, registered, err := UIDAtContext(ctx, bound, req.Netuid, req.Hotkey, runtime.Hash)
	if err != nil {
		return result, err
	}
	result.UID = uid
	pool, err := PoolReservesAtContext(ctx, bound, req.Netuid, runtime.Hash)
	if err != nil {
		return result, err
	}
	result.Pool = pool
	coldkey := req.Coldkey.PublicKey()
	balance, err := FreeBalanceAtContext(ctx, bound, coldkey, runtime.Hash)
	if err != nil {
		return result, err
	}
	result.FreeBalanceRao = balance
	before, err := TotalHotkeyAlphaAtContext(ctx, bound, req.Hotkey, req.Netuid, runtime.Hash)
	if err != nil {
		return result, err
	}
	result.HotkeyAlphaRao = before
	fmt.Fprintf(req.Output, "pool (netuid %d): %s TAO in, %s alpha in, price %s TAO/alpha (%d rao/alpha)\n", req.Netuid, FormatRao(pool.TaoRao), FormatRao(pool.AlphaInRao), FormatRao(pool.PriceQ9()), pool.PriceQ9())
	fmt.Fprintf(req.Output, "hotkey: %s (uid %d registered=%t, staked alpha %s)\ncoldkey: %s (free balance %s TAO)\n", Hex32(req.Hotkey), uid, registered, FormatRao(before), req.Coldkey.Address(), FormatRao(balance))
	fmt.Fprintf(req.Output, "stake: %s TAO (%d rao)", FormatRao(req.AmountRao), req.AmountRao)
	if req.LimitPriceRao != 0 {
		fmt.Fprintf(req.Output, " via add_stake_limit, limit price %d rao/alpha, allow_partial=%t\n", req.LimitPriceRao, req.AllowPartial)
	} else {
		fmt.Fprintf(req.Output, " via add_stake at the pool price\n")
	}
	if err := AddStakeDecision(registered, balance, req.AmountRao, pool, req.LimitPriceRao); err != nil {
		return result, err
	}
	stakeCall, err := AddStakeCall(bound.Meta, req.Netuid, req.Hotkey, req.AmountRao)
	if req.LimitPriceRao != 0 {
		stakeCall, err = AddStakeLimitCall(bound.Meta, req.Netuid, req.Hotkey, req.AmountRao, req.LimitPriceRao, req.AllowPartial)
	}
	if err != nil {
		return result, err
	}
	submit, err := SubmitCall(ctx, bound, SubmitRequest{Command: req.Command, Netuid: req.Netuid, Hotkey: req.Hotkey, Signer: req.Coldkey, Call: stakeCall, FeeLimitRao: req.FeeLimitRao, Journal: req.Journal, Apply: req.Apply, Output: req.Output})
	result.Submit = &submit
	if err != nil || submit.Receipt == nil {
		return result, err
	}
	after, err := TotalHotkeyAlphaAtContext(ctx, bound, req.Hotkey, req.Netuid, submit.Receipt.BlockHash)
	if err != nil {
		return result, err
	}
	result.HotkeyAlphaAfter = after
	if after <= before {
		return result, fmt.Errorf("hotkey alpha did not increase after the finalized extrinsic (%d -> %d rao)", before, after)
	}
	fmt.Fprintf(req.Output, "staked: hotkey alpha %s -> %s (+%s alpha)\n", FormatRao(before), FormatRao(after), FormatRao(after-before))
	return result, nil
}

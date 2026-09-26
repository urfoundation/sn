package chain

import (
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/crv4"
)

// AddStakeCall builds SubtensorModule.add_stake(hotkey, netuid, amount_staked).
// amountRao is TAO in rao taken from the signing coldkey's free balance and
// swapped into the subnet's alpha at the pool price.
func AddStakeCall(meta *types.Metadata, netuid uint16, hotkey [32]byte, amountRao uint64) (types.Call, error) {
	if meta == nil || netuid == 0 || hotkey == ([32]byte{}) || amountRao == 0 {
		return types.Call{}, errors.New("add_stake metadata, netuid, hotkey or amount is missing")
	}
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(meta, crv4.PalletName+".add_stake", *account, types.NewU16(netuid), types.NewU64(amountRao))
}

// AddStakeLimitCall builds SubtensorModule.add_stake_limit(hotkey, netuid,
// amount_staked, limit_price, allow_partial): a Dynamic TAO purchase with an
// explicit maximum pool price in TAO rao per alpha (fill-or-kill unless
// allowPartial).
func AddStakeLimitCall(meta *types.Metadata, netuid uint16, hotkey [32]byte, amountRao, limitPriceRao uint64, allowPartial bool) (types.Call, error) {
	if meta == nil || netuid == 0 || hotkey == ([32]byte{}) || amountRao == 0 || limitPriceRao == 0 {
		return types.Call{}, errors.New("add_stake_limit metadata, netuid, hotkey, amount or limit price is missing")
	}
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(
		meta,
		crv4.PalletName+".add_stake_limit",
		*account,
		types.NewU16(netuid),
		types.NewU64(amountRao),
		types.NewU64(limitPriceRao),
		types.NewBool(allowPartial),
	)
}

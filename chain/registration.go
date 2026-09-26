package chain

import (
	"context"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/crv4"
)

// RegistrationEconomics captures the runtime burn auction state needed to
// prove a bounded registration.
type RegistrationEconomics struct {
	BurnRao             uint64
	MinBurnRao          uint64
	MaxBurnRao          uint64
	BurnHalfLifeBlocks  uint16
	BurnIncreaseMultQ64 string
}

// ReadRegistrationEconomicsAtContext reads every auction parameter from one
// finalized state root through the chain's bound (authenticated) metadata.
func ReadRegistrationEconomicsAtContext(ctx context.Context, chain *crv4.Chain, netuid uint16, finalized types.Hash) (RegistrationEconomics, error) {
	var result RegistrationEconomics
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || chain.Meta == nil {
		return result, errors.New("registration economics chain dependencies are unavailable")
	}
	readU64 := func(storage string) (uint64, error) {
		key, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, storage, NetuidArg(netuid))
		if err != nil {
			return 0, err
		}
		var value types.U64
		if err := ReadRequiredStorageAtContext(ctx, chain, key, crv4.PalletName, storage, &value, finalized); err != nil {
			return 0, err
		}
		return uint64(value), nil
	}
	var err error
	if result.BurnRao, err = readU64("Burn"); err != nil {
		return result, fmt.Errorf("read Burn: %w", err)
	} else if result.BurnRao == 0 {
		return result, errors.New("Burn is zero")
	}
	if result.MinBurnRao, err = readU64("MinBurn"); err != nil {
		return result, fmt.Errorf("read MinBurn: %w", err)
	} else if result.MinBurnRao == 0 {
		return result, errors.New("MinBurn is zero")
	}
	if result.MaxBurnRao, err = readU64("MaxBurn"); err != nil {
		return result, fmt.Errorf("read MaxBurn: %w", err)
	} else if result.MaxBurnRao == 0 {
		return result, errors.New("MaxBurn is zero")
	}
	halfLifeKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "BurnHalfLife", NetuidArg(netuid))
	if err != nil {
		return result, err
	}
	var halfLife types.U16
	if err := ReadRequiredStorageAtContext(ctx, chain, halfLifeKey, crv4.PalletName, "BurnHalfLife", &halfLife, finalized); err != nil {
		return result, fmt.Errorf("read BurnHalfLife: %w", err)
	} else if halfLife == 0 {
		return result, errors.New("BurnHalfLife is zero")
	}
	result.BurnHalfLifeBlocks = uint16(halfLife)
	multiplierKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "BurnIncreaseMult", NetuidArg(netuid))
	if err != nil {
		return result, err
	}
	var multiplier types.U128
	if err := ReadRequiredStorageAtContext(ctx, chain, multiplierKey, crv4.PalletName, "BurnIncreaseMult", &multiplier, finalized); err != nil {
		return result, fmt.Errorf("read BurnIncreaseMult: %w", err)
	} else if multiplier.Int == nil || multiplier.Sign() <= 0 {
		return result, errors.New("BurnIncreaseMult is zero")
	}
	result.BurnIncreaseMultQ64 = multiplier.String()
	return result, nil
}

// ReadRegistrationEconomicsAt preserves the contextless harness surface.
func ReadRegistrationEconomicsAt(chain *crv4.Chain, netuid uint16, finalized types.Hash) (RegistrationEconomics, error) {
	return ReadRegistrationEconomicsAtContext(context.Background(), chain, netuid, finalized)
}

// BurnRegisterLimitCall builds SubtensorModule.register_limit(netuid, hotkey,
// limit_price): a runtime-enforced registration ceiling so a moving burn
// auction cannot charge more than the approved limit between observation and
// the block. The metadata must be the authenticated artifact for the signing
// block.
func BurnRegisterLimitCall(meta *types.Metadata, netuid uint16, hotkey [32]byte, limitPrice uint64) (types.Call, error) {
	if meta == nil || netuid == 0 || hotkey == ([32]byte{}) || limitPrice == 0 {
		return types.Call{}, errors.New("register_limit metadata, netuid, hotkey or limit is missing")
	}
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(meta, crv4.PalletName+".register_limit", types.NewU16(netuid), *account, types.NewU64(limitPrice))
}

// ValidateRegistrationBurn refuses to construct a registration whose live burn
// already exceeds the caller's ceiling.
func ValidateRegistrationBurn(burn, limit uint64) error {
	if limit == 0 {
		return errors.New("registration burn limit is zero")
	}
	if burn > limit {
		return fmt.Errorf("live registration burn %d rao exceeds the burn limit %d rao", burn, limit)
	}
	return nil
}

package chain

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/urfoundation/sn/v2026/crv4"
)

// RegisterRequest is one burned registration of Hotkey under the signing
// Coldkey on Netuid. BurnLimitRao zero means "the burn observed at the
// finalized read"; the runtime rejects a price above the limit either way.
type RegisterRequest struct {
	Command      string
	Netuid       uint16
	Hotkey       [32]byte
	Coldkey      *crv4.Keypair
	BurnLimitRao uint64
	FeeLimitRao  uint64
	Allowed      []crv4.RuntimeArtifactIdentity
	Journal      *Journal
	Apply        bool
	Output       io.Writer
}

// RegisterResult reports the economics, the decision, and the submit outcome.
type RegisterResult struct {
	Runtime           FinalizedRuntime
	Economics         RegistrationEconomics
	BurnLimitRao      uint64
	AlreadyRegistered bool
	UID               uint16
	FreeBalanceRao    uint64
	Submit            *SubmitResult
}

// RegistrationDecision applies the pure registration rules: an existing
// registration must belong to the signing coldkey (idempotent success), the
// live burn must not exceed the ceiling, and the coldkey must be able to pay
// the burn before a fee is even quoted.
func RegistrationDecision(economics RegistrationEconomics, limit uint64, registered bool, owner, coldkey [32]byte, freeBalanceRao uint64) (uint64, error) {
	if registered {
		if owner != coldkey {
			return 0, fmt.Errorf("hotkey is already registered under coldkey %s, not the signing coldkey %s", Hex32(owner), Hex32(coldkey))
		}
		return limit, nil
	}
	if limit == 0 {
		limit = economics.BurnRao
	}
	if err := ValidateRegistrationBurn(economics.BurnRao, limit); err != nil {
		return limit, err
	}
	if freeBalanceRao < economics.BurnRao {
		return limit, fmt.Errorf("coldkey free balance %d rao cannot pay the %d rao burn", freeBalanceRao, economics.BurnRao)
	}
	return limit, nil
}

// RegisterHotkey authenticates the finalized runtime against the caller's pin,
// prints the live registration economics, applies RegistrationDecision, and
// (only with Apply) submits register_limit and reports the resulting UID.
func RegisterHotkey(ctx context.Context, chain *crv4.Chain, req RegisterRequest) (RegisterResult, error) {
	var result RegisterResult
	if req.Coldkey == nil || req.Output == nil || req.Netuid == 0 || req.Hotkey == ([32]byte{}) {
		return result, errors.New("registration request is incomplete")
	}
	bound, runtime, err := AuthenticateFinalizedRuntimeContext(ctx, chain, req.Allowed...)
	if err != nil {
		return result, err
	}
	result.Runtime = runtime
	fmt.Fprintf(req.Output, "runtime: %s/%d/%d/%d at finalized block %d (%s)\n", runtime.Artifact.Version.SpecName, runtime.Artifact.Version.SpecVersion, runtime.Artifact.Version.TransactionVersion, runtime.Artifact.Version.StateVersion, runtime.Number, runtime.Hash.Hex())
	economics, err := ReadRegistrationEconomicsAtContext(ctx, bound, req.Netuid, runtime.Hash)
	if err != nil {
		return result, err
	}
	result.Economics = economics
	fmt.Fprintf(req.Output, "registration economics (netuid %d): burn %s TAO (%d rao), min %d rao, max %d rao, half-life %d blocks, increase x%s/2^64\n", req.Netuid, FormatRao(economics.BurnRao), economics.BurnRao, economics.MinBurnRao, economics.MaxBurnRao, economics.BurnHalfLifeBlocks, economics.BurnIncreaseMultQ64)
	coldkey := req.Coldkey.PublicKey()
	uid, registered, err := UIDAtContext(ctx, bound, req.Netuid, req.Hotkey, runtime.Hash)
	if err != nil {
		return result, err
	}
	var owner [32]byte
	if registered {
		if owner, err = HotkeyOwnerAtContext(ctx, bound, req.Hotkey, runtime.Hash); err != nil {
			return result, err
		}
	}
	balance, err := FreeBalanceAtContext(ctx, bound, coldkey, runtime.Hash)
	if err != nil {
		return result, err
	}
	result.FreeBalanceRao = balance
	fmt.Fprintf(req.Output, "hotkey: %s\ncoldkey: %s (free balance %s TAO)\n", Hex32(req.Hotkey), req.Coldkey.Address(), FormatRao(balance))
	limit, err := RegistrationDecision(economics, req.BurnLimitRao, registered, owner, coldkey, balance)
	result.BurnLimitRao = limit
	if err != nil {
		return result, err
	}
	if registered {
		result.AlreadyRegistered, result.UID = true, uid
		fmt.Fprintf(req.Output, "already registered: uid %d on netuid %d under this coldkey; nothing to submit\n", uid, req.Netuid)
		return result, nil
	}
	fmt.Fprintf(req.Output, "burn limit: %d rao (register_limit refuses a higher live burn)\n", limit)
	call, err := BurnRegisterLimitCall(bound.Meta, req.Netuid, req.Hotkey, limit)
	if err != nil {
		return result, err
	}
	submit, err := SubmitCall(ctx, bound, SubmitRequest{Command: req.Command, Netuid: req.Netuid, Hotkey: req.Hotkey, Signer: req.Coldkey, Call: call, FeeLimitRao: req.FeeLimitRao, Journal: req.Journal, Apply: req.Apply, Output: req.Output})
	result.Submit = &submit
	if err != nil || submit.Receipt == nil {
		return result, err
	}
	uid, registered, err = UIDAtContext(ctx, bound, req.Netuid, req.Hotkey, submit.Receipt.BlockHash)
	if err != nil {
		return result, err
	}
	if !registered {
		return result, errors.New("hotkey not registered after the finalized extrinsic")
	}
	owner, err = HotkeyOwnerAtContext(ctx, bound, req.Hotkey, submit.Receipt.BlockHash)
	if err != nil {
		return result, err
	}
	if owner != coldkey {
		return result, fmt.Errorf("registered hotkey owner is %s, want %s", Hex32(owner), Hex32(coldkey))
	}
	result.UID = uid
	fmt.Fprintf(req.Output, "registered: uid %d on netuid %d (hotkey %s, coldkey %s)\n", uid, req.Netuid, Hex32(req.Hotkey), req.Coldkey.Address())
	return result, nil
}

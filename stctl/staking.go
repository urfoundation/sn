package main

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// stakingPrecompileAddress is the subtensor StakingV2 precompile (IStaking),
// vendored at evm/src/interfaces/stakingV2.sol (subtensor v3.2.7).
// ABI unverified against the live runtime: SP-1 gated (PLAN.md §2/§10).
var stakingPrecompileAddress = common.HexToAddress("0x0000000000000000000000000000000000000805")

// transferStakeSignature is the canonical signature from stakingV2.sol:
//
//	function transferStake(
//	    bytes32 destination_coldkey,
//	    bytes32 hotkey,
//	    uint256 origin_netuid,
//	    uint256 destination_netuid,
//	    uint256 amount   // rao
//	) external;
const transferStakeSignature = "transferStake(bytes32,bytes32,uint256,uint256,uint256)"

var transferStakeArguments = func() abi.Arguments {
	bytes32Type, err := abi.NewType("bytes32", "", nil)
	if err != nil {
		panic(fmt.Errorf("stctl: abi bytes32 type: %w", err))
	}
	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		panic(fmt.Errorf("stctl: abi uint256 type: %w", err))
	}
	return abi.Arguments{
		{Name: "destination_coldkey", Type: bytes32Type},
		{Name: "hotkey", Type: bytes32Type},
		{Name: "origin_netuid", Type: uint256Type},
		{Name: "destination_netuid", Type: uint256Type},
		{Name: "amount", Type: uint256Type},
	}
}()

// packTransferStake hand-packs an IStaking.transferStake call: move `amount`
// rao of the caller's stake on `hotkey` in `originNetuid` to
// `destinationColdkey` on the same hotkey in `destinationNetuid`.
//
// The push-then-credit deposit flow (evm/README.md deviation 3) uses
// transferStake(mirror(proxy), treasuryHotkey, netuid, netuid, amount).
func packTransferStake(destinationColdkey, hotkey [32]byte, originNetuid, destinationNetuid, amount *big.Int) ([]byte, error) {
	packed, err := transferStakeArguments.Pack(
		destinationColdkey,
		hotkey,
		originNetuid,
		destinationNetuid,
		amount,
	)
	if err != nil {
		return nil, fmt.Errorf("pack transferStake: %w", err)
	}
	selector := crypto.Keccak256([]byte(transferStakeSignature))[:4]
	return append(selector, packed...), nil
}

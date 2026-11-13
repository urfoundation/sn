package demand

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// The Bittensor EVM precompiles the alpha wallet calls.
var (
	StakingPrecompile = common.HexToAddress("0x0000000000000000000000000000000000000805")
	AlphaPrecompile   = common.HexToAddress("0x0000000000000000000000000000000000000808")
)

// WeiPerRao converts Bittensor EVM's 18-decimal balances to rao.
var WeiPerRao = big.NewInt(1_000_000_000)

func selector(signature string) []byte { return crypto.Keccak256([]byte(signature))[:4] }

var (
	withdrawSelector      = selector("withdraw(uint256)")
	balanceOfSelector     = selector("balanceOf(address)")
	peersSelector         = selector("peers(uint32)")
	addStakeLimitSelector = selector("addStakeLimit(bytes32,uint256,uint256,bool,uint256)")
	getStakeSelector      = selector("getStake(bytes32,bytes32,uint256)")
	alphaPriceSelector    = selector("getAlphaPrice(uint16)")
	taoInPoolSelector     = selector("getTaoInPool(uint16)")
	simSwapSelector       = selector("simSwapTaoForAlpha(uint16,uint64)")
)

func word(value *big.Int) []byte { return common.LeftPadBytes(value.Bytes(), 32) }

func wordUint(value uint64) []byte { return word(new(big.Int).SetUint64(value)) }

func packCall(selector []byte, words ...[]byte) []byte {
	data := append([]byte(nil), selector...)
	for _, w := range words {
		data = append(data, w...)
	}
	return data
}

func packWithdraw(wei *big.Int) []byte { return packCall(withdrawSelector, word(wei)) }

func packBalanceOf(owner common.Address) []byte {
	return packCall(balanceOfSelector, common.LeftPadBytes(owner[:], 32))
}

func packPeers(eid uint32) []byte { return packCall(peersSelector, wordUint(uint64(eid))) }

func packAddStakeLimit(hotkey [32]byte, amountRao, limitPrice uint64, netuid uint16) []byte {
	// allow_partial is false: the stake fills whole or not at all.
	return packCall(addStakeLimitSelector, hotkey[:], wordUint(amountRao), wordUint(limitPrice), make([]byte, 32), wordUint(uint64(netuid)))
}

func packGetStake(hotkey, coldkey [32]byte, netuid uint16) []byte {
	return packCall(getStakeSelector, hotkey[:], coldkey[:], wordUint(uint64(netuid)))
}

func packAlphaPrice(netuid uint16) []byte {
	return packCall(alphaPriceSelector, wordUint(uint64(netuid)))
}

func packTaoInPool(netuid uint16) []byte {
	return packCall(taoInPoolSelector, wordUint(uint64(netuid)))
}

func packSimSwap(netuid uint16, taoRao uint64) []byte {
	return packCall(simSwapSelector, wordUint(uint64(netuid)), wordUint(taoRao))
}

func unpackWord(name string, raw []byte) (*big.Int, error) {
	if len(raw) != 32 {
		return nil, fmt.Errorf("demand: %s returned %d bytes, want 32", name, len(raw))
	}
	return new(big.Int).SetBytes(raw), nil
}

// AllowedAlphaWalletCall reports whether a transaction is one the conversion
// sends from the alpha wallet: unwrapping its own wTAO, or staking its own TAO
// whole on its own deposit hotkey on this subnet. Neither can move value out of
// the wallet, so a signer may admit them on their form alone.
func AllowedAlphaWalletCall(to common.Address, data []byte, wtao common.Address, hotkey [32]byte, netuid uint16) bool {
	switch {
	case to == wtao:
		return len(data) == 36 && bytes.HasPrefix(data, withdrawSelector)
	case to == StakingPrecompile:
		return len(data) == 164 && bytes.HasPrefix(data, addStakeLimitSelector) &&
			bytes.Equal(data[4:36], hotkey[:]) &&
			bytes.Equal(data[100:132], make([]byte, 32)) &&
			bytes.Equal(data[132:164], wordUint(uint64(netuid)))
	}
	return false
}

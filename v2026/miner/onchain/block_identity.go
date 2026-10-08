// EVM block identities retain the explicit RPC hash used by receipts and
// historical calls, including Subtensor's synthetic EVM blocks.
package onchain

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Carries a validated selector response without recomputing Header.Hash.
type EVMBlockIdentity struct {
	Number uint64
	Hash   common.Hash
}

// Reads a numbered or finalized block in the RPC hash domain. Subtensor's
// explicit EVM hash differs from both its native hash and Header.Hash().
func ReadEVMBlockIdentity(ctx context.Context, client *ethclient.Client, number *big.Int) (EVMBlockIdentity, error) {
	if client == nil || number == nil {
		return EVMBlockIdentity{}, errors.New("EVM block identity reader is unavailable")
	}
	var selector string
	if number.Sign() < 0 {
		if !number.IsInt64() || rpc.BlockNumber(number.Int64()) != rpc.FinalizedBlockNumber {
			return EVMBlockIdentity{}, fmt.Errorf("unsupported EVM block selector %s", number)
		}
		selector = rpc.FinalizedBlockNumber.String()
	} else {
		if !number.IsUint64() {
			return EVMBlockIdentity{}, fmt.Errorf("EVM block number %s exceeds uint64", number)
		}
		selector = hexutil.EncodeUint64(number.Uint64())
	}
	var block *struct {
		Number *hexutil.Uint64 `json:"number"`
		Hash   *common.Hash    `json:"hash"`
	}
	if err := client.Client().CallContext(ctx, &block, "eth_getBlockByNumber", selector, false); err != nil {
		return EVMBlockIdentity{}, err
	}
	if block == nil {
		return EVMBlockIdentity{}, ethereum.NotFound
	}
	if block.Number == nil || block.Hash == nil || *block.Hash == (common.Hash{}) {
		return EVMBlockIdentity{}, errors.New("EVM block identity has a missing number or missing/zero hash")
	}
	identity := EVMBlockIdentity{Number: uint64(*block.Number), Hash: *block.Hash}
	if number.Sign() >= 0 && identity.Number != number.Uint64() {
		return EVMBlockIdentity{}, fmt.Errorf("EVM block response number %d does not match request %s", identity.Number, number)
	}
	return identity, nil
}

package commitmentoracle

// EVM (chain 964) access: the coordinator views the oracle reads at the
// finalized block, and the transaction calls the sender makes. Block hashes
// are taken from the RPC's own "hash" fields rather than recomputed from
// header fields. Methods are not safe for concurrent use.

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/stabi"
)

// The EVM calls the oracle makes. A nil block means latest.
type evmClient interface {
	chainId(ctx context.Context) (*big.Int, error)
	// eth_getBlockByNumber("finalized"): number and hash.
	finalizedBlock(ctx context.Context) (uint64, common.Hash, error)
	latestBlock(ctx context.Context) (uint64, error)
	// The canonical hash at number.
	blockHash(ctx context.Context, number uint64) (common.Hash, error)
	callContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error)
	balanceAt(ctx context.Context, account common.Address, block *big.Int) (*big.Int, error)
	nonceAt(ctx context.Context, account common.Address, block *big.Int) (uint64, error)
	pendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	suggestGasPrice(ctx context.Context) (*big.Int, error)
	estimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	sendTransaction(ctx context.Context, tx *types.Transaction) error
	// nil, ethereum.NotFound when the transaction is unknown or not mined.
	transactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)
	close()
}

// Custom error selectors of STCoordinator that the oracle classifies.
var (
	selectorUnauthorized         = evmSelector("Unauthorized()")
	selectorStaleCommitment      = evmSelector("StaleCommitment()")
	selectorInvalidConfiguration = evmSelector("InvalidConfiguration()")
	selectorPaused               = evmSelector("Paused()")
)

func evmSelector(signature string) [4]byte {
	return [4]byte(crypto.Keccak256([]byte(signature))[:4])
}

// The revert selector of a preflight or gas estimate error, when it carries
// revert data.
func revertSelector(err error) ([4]byte, bool) {
	var dataError gethrpc.DataError
	if !errors.As(err, &dataError) {
		return [4]byte{}, false
	}
	encoded, _ := dataError.ErrorData().(string)
	data, decodeErr := hexutil.Decode(encoded)
	if decodeErr != nil || len(data) < 4 {
		return [4]byte{}, false
	}
	return [4]byte(data[:4]), true
}

// The ethclient-backed client.
type ethEvmClient struct {
	endpoint string
	client   *ethclient.Client
}

func dialEthEvmClient(ctx context.Context, endpoint string) (*ethEvmClient, error) {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("dial %s failed", endpointLabel(endpoint))
	}
	return &ethEvmClient{endpoint: endpointLabel(endpoint), client: client}, nil
}

func (self *ethEvmClient) close() {
	self.client.Close()
}

func (self *ethEvmClient) chainId(ctx context.Context) (*big.Int, error) {
	return self.client.ChainID(ctx)
}

type evmBlockIdentity struct {
	Number *hexutil.Big `json:"number"`
	Hash   *common.Hash `json:"hash"`
}

func (self *ethEvmClient) block(ctx context.Context, tag string) (uint64, common.Hash, error) {
	var block *evmBlockIdentity
	if err := self.client.Client().CallContext(ctx, &block, "eth_getBlockByNumber", tag, false); err != nil {
		return 0, common.Hash{}, fmt.Errorf("%s eth_getBlockByNumber(%s): %w", self.endpoint, tag, err)
	}
	if block == nil || block.Number == nil || block.Hash == nil || !(*big.Int)(block.Number).IsUint64() {
		return 0, common.Hash{}, fmt.Errorf("%s has no complete %s block", self.endpoint, tag)
	}
	return (*big.Int)(block.Number).Uint64(), *block.Hash, nil
}

func (self *ethEvmClient) finalizedBlock(ctx context.Context) (uint64, common.Hash, error) {
	return self.block(ctx, "finalized")
}

func (self *ethEvmClient) latestBlock(ctx context.Context) (uint64, error) {
	return self.client.BlockNumber(ctx)
}

func (self *ethEvmClient) blockHash(ctx context.Context, number uint64) (common.Hash, error) {
	actual, hash, err := self.block(ctx, hexutil.EncodeUint64(number))
	if err != nil {
		return common.Hash{}, err
	}
	if actual != number {
		return common.Hash{}, fmt.Errorf("%s answered block %d for %d", self.endpoint, actual, number)
	}
	return hash, nil
}

func (self *ethEvmClient) callContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	return self.client.CallContract(ctx, msg, block)
}

func (self *ethEvmClient) balanceAt(ctx context.Context, account common.Address, block *big.Int) (*big.Int, error) {
	return self.client.BalanceAt(ctx, account, block)
}

func (self *ethEvmClient) nonceAt(ctx context.Context, account common.Address, block *big.Int) (uint64, error) {
	return self.client.NonceAt(ctx, account, block)
}

func (self *ethEvmClient) pendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	return self.client.PendingNonceAt(ctx, account)
}

func (self *ethEvmClient) suggestGasPrice(ctx context.Context) (*big.Int, error) {
	return self.client.SuggestGasPrice(ctx)
}

func (self *ethEvmClient) estimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	return self.client.EstimateGas(ctx, msg)
}

func (self *ethEvmClient) sendTransaction(ctx context.Context, tx *types.Transaction) error {
	return self.client.SendTransaction(ctx, tx)
}

func (self *ethEvmClient) transactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return self.client.TransactionReceipt(ctx, hash)
}

// Typed coordinator views at one block.
type coordinatorReader struct {
	client      evmClient
	coordinator common.Address
	abi         *stabi.STCoordinator
}

func newCoordinatorReader(client evmClient, coordinator common.Address) *coordinatorReader {
	return &coordinatorReader{client: client, coordinator: coordinator, abi: stabi.NewSTCoordinator()}
}

func (self *coordinatorReader) call(ctx context.Context, data []byte, block uint64) ([]byte, error) {
	to := self.coordinator
	return self.client.callContract(ctx, ethereum.CallMsg{To: &to, Data: data}, new(big.Int).SetUint64(block))
}

func (self *coordinatorReader) netuid(ctx context.Context, block uint64) (uint16, error) {
	out, err := self.call(ctx, self.abi.PackNetuid(), block)
	if err != nil {
		return 0, err
	}
	return self.abi.UnpackNetuid(out)
}

func (self *coordinatorReader) activeCommitmentOracle(ctx context.Context, block uint64) (common.Address, error) {
	out, err := self.call(ctx, self.abi.PackActiveCommitmentOracle(), block)
	if err != nil {
		return common.Address{}, err
	}
	return self.abi.UnpackActiveCommitmentOracle(out)
}

func (self *coordinatorReader) paused(ctx context.Context, block uint64) (bool, error) {
	out, err := self.call(ctx, self.abi.PackPaused(), block)
	if err != nil {
		return false, err
	}
	return self.abi.UnpackPaused(out)
}

// policyAt(currentEpoch()).commitmentMaxAgeBlocks, the bound bindFleetMember
// applies.
func (self *coordinatorReader) commitmentMaxAgeBlocks(ctx context.Context, block uint64) (uint64, error) {
	out, err := self.call(ctx, self.abi.PackCurrentEpoch(), block)
	if err != nil {
		return 0, err
	}
	epoch, err := self.abi.UnpackCurrentEpoch(out)
	if err != nil {
		return 0, err
	}
	out, err = self.call(ctx, self.abi.PackPolicyAt(epoch), block)
	if err != nil {
		return 0, err
	}
	policy, err := self.abi.UnpackPolicyAt(out)
	if err != nil {
		return 0, err
	}
	if policy.CommitmentMaxAgeBlocks == 0 {
		return 0, fmt.Errorf("policy of epoch %s has no commitmentMaxAgeBlocks", epoch)
	}
	return policy.CommitmentMaxAgeBlocks, nil
}

func (self *coordinatorReader) mirroredCommitment(ctx context.Context, hotkey [32]byte, block uint64) (mirroredRecord, error) {
	out, err := self.call(ctx, self.abi.PackMirroredCommitments(hotkey), block)
	if err != nil {
		return mirroredRecord{}, err
	}
	record, err := self.abi.UnpackMirroredCommitments(out)
	if err != nil {
		return mirroredRecord{}, err
	}
	return mirroredRecord{commitmentHash: record.CommitmentHash, finalizedBlockHash: record.FinalizedBlockHash, finalizedBlock: record.FinalizedBlock}, nil
}

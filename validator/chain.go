package validator

// chain.go — the validator's EVM chain client (PLAN.md §7.2 / §11.1):
// ordered-failover ethclient dialing + STSubnet reads/writes through the
// sn/stabi abigen-v2 bindings (Pack/Unpack style, cribbed from stctl), plus
// the auxiliary piece the validator needs beyond the contract:
//
//   - IMetagraph (0x802) eth_calls with hand-encoded calldata (getUidCount /
//     getHotkey) to resolve an operator's minerHotkey -> live UID for
//     steering (the vendored interface in evm/src/interfaces/metagraph.sol
//     is the ABI source; unverified against the live runtime — SP-1).
//
// The effort-bounty wrappers (registerValidator / submitTrails / prove /
// reseed / claimValidator and their views) are deferred to the bounty phase
// (WHITEPAPER §9.3, D23); implementation parked at docs/parked/. The v1
// validator sends no transactions — sendAndWait stays as the generic signed
// tx path for when that phase lands.

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/crypto/sha3"

	"github.com/urfoundation/sn/stabi"
)

const (
	chainDialTimeout                = 15 * time.Second
	chainCallTimeout                = 30 * time.Second
	chainSendTimeout                = 60 * time.Second
	chainWaitMinedTimeout           = 5 * time.Minute
	chainMaximumBatchCalls          = 50
	chainBlockIdentityCacheCapacity = 256
)

// Preserves the hash reported by the EVM RPC. Subtensor's synthetic block
// identity cannot be reconstructed with the standard Ethereum header hash.
type chainRPCBlock struct {
	Number *hexutil.Big `json:"number"`
	Hash   common.Hash  `json:"hash"`
}

// Validates the complete explicit RPC identity, including genesis when a
// numbered historical scan requests it.
func (self *chainRPCBlock) identity() (uint64, [32]byte, error) {
	if self == nil || self.Number == nil || !(*big.Int)(self.Number).IsUint64() || self.Hash == (common.Hash{}) {
		return 0, [32]byte{}, errors.New("EVM block header is empty or has an invalid number/hash")
	}
	return (*big.Int)(self.Number).Uint64(), [32]byte(self.Hash), nil
}

// metagraphAddress is the IMetagraph precompile (0x802).
var metagraphAddress = common.HexToAddress("0x0000000000000000000000000000000000000802")

// ChainClient is a live connection to one answering EVM rpc endpoint with
// the STSubnet binding attached. Its immutable block identity cache is safe
// for concurrent reads; Close remains an owner-only lifecycle operation.
type ChainClient struct {
	stateLock    sync.Mutex
	blockNumbers map[[32]byte]uint64
	client       *ethclient.Client
	rpcUrl       string
	chainId      *big.Int
	st           *stabi.STSubnet
	coordinator  *stabi.STCoordinator
	contract     *bind.BoundContract
	contractAddr common.Address
	release      bool
}

// DialChain tries rpcUrls in order until one answers eth_chainId
// (§11.1: every chain consumer takes an ordered endpoint list).
func DialChain(rpcUrls []string, contractAddr common.Address) (*ChainClient, error) {
	return DialChainContext(context.Background(), rpcUrls, contractAddr)
}

// DialChainContext tries ordered endpoints while retaining the caller's
// lifecycle deadline across both the transport dial and eth_chainId probe.
func DialChainContext(ctx context.Context, rpcUrls []string, contractAddr common.Address) (*ChainClient, error) {
	return dialChainContext(ctx, rpcUrls, contractAddr, false)
}

// DialReleaseChain binds the immutable release-1.0 coordinator ABI. Keeping
// this constructor distinct from DialChain prevents a legacy STSubnet address
// from being accepted accidentally by production configuration.
func DialReleaseChain(rpcUrls []string, contractAddr common.Address) (*ChainClient, error) {
	return DialReleaseChainContext(context.Background(), rpcUrls, contractAddr)
}

// DialReleaseChainContext constructs the immutable release binding without
// allowing startup to outlive its caller while a public provider stalls.
func DialReleaseChainContext(ctx context.Context, rpcUrls []string, contractAddr common.Address) (*ChainClient, error) {
	return dialChainContext(ctx, rpcUrls, contractAddr, true)
}

// dialChainContext shares the bounded ordered endpoint implementation used
// by development and release constructors.
func dialChainContext(ctx context.Context, rpcUrls []string, contractAddr common.Address, release bool) (*ChainClient, error) {
	return dialChainWithEndpointContext(ctx, rpcUrls, contractAddr, release, context.WithTimeout)
}

// chainEndpointContext supplies one per-provider deadline. Production uses
// WithTimeout; the injection keeps public-provider failover deterministic in
// tests without changing global time.
type chainEndpointContext func(context.Context, time.Duration) (context.Context, context.CancelFunc)

// dialChainWithEndpointContext closes every rejected endpoint client before
// advancing. Its injected context factory is only a deterministic test seam.
func dialChainWithEndpointContext(ctx context.Context, rpcUrls []string, contractAddr common.Address, release bool, endpointContext chainEndpointContext) (*ChainClient, error) {
	if ctx == nil || endpointContext == nil {
		return nil, errors.New("EVM dial context is unavailable")
	}
	if len(rpcUrls) == 0 {
		return nil, fmt.Errorf("no --rpc endpoints configured")
	}
	if release && contractAddr == (common.Address{}) {
		return nil, fmt.Errorf("contract address is zero")
	}
	var errs []error
	for _, url := range rpcUrls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		endpointCtx, cancel := endpointContext(ctx, chainDialTimeout)
		if endpointCtx == nil || cancel == nil {
			return nil, errors.New("EVM endpoint context is unavailable")
		}
		client, err := ethclient.DialContext(endpointCtx, url)
		if err != nil {
			cancel()
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		chainId, err := client.ChainID(endpointCtx)
		cancel()
		if err != nil {
			client.Close()
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		c := &ChainClient{
			client:       client,
			rpcUrl:       url,
			chainId:      chainId,
			st:           stabi.NewSTSubnet(),
			coordinator:  stabi.NewSTCoordinator(),
			contractAddr: contractAddr,
			release:      release,
		}
		if release {
			c.contract = c.coordinator.Instance(client, contractAddr)
		} else {
			c.contract = c.st.Instance(client, contractAddr)
		}
		return c, nil
	}
	return nil, fmt.Errorf("no rpc endpoint answered: %w", errors.Join(errs...))
}

// FinalizedBlockContext identifies the canonical EVM head used by every
// production view in one steering iteration and observes caller cancellation.
func (self *ChainClient) FinalizedBlockContext(ctx context.Context) (uint64, [32]byte, error) {
	if ctx == nil {
		return 0, [32]byte{}, errors.New("finalized EVM head context is nil")
	}
	if self == nil || self.client == nil {
		return 0, [32]byte{}, errors.New("finalized EVM head client is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	defer cancel()
	var header *chainRPCBlock
	err := self.client.Client().CallContext(ctx, &header, "eth_getBlockByNumber", "finalized", false)
	if err != nil {
		return 0, [32]byte{}, fmt.Errorf("finalized EVM head: %w", err)
	}
	block, hash, err := header.identity()
	if err != nil || block == 0 {
		return 0, [32]byte{}, fmt.Errorf("finalized EVM head is empty or invalid: %v", err)
	}
	if err := self.rememberBlockIdentity(block, hash); err != nil {
		return 0, [32]byte{}, fmt.Errorf("finalized EVM head identity: %w", err)
	}
	return block, hash, nil
}

// FinalizedBlock retains the background-context API for command callers.
func (self *ChainClient) FinalizedBlock() (uint64, [32]byte, error) {
	return self.FinalizedBlockContext(context.Background())
}

// Records the immutable number encoded by a block hash. A conflicting pair
// is always an error, including when two callers race to validate it.
func (self *ChainClient) rememberBlockIdentity(block uint64, blockHash [32]byte) error {
	if self == nil || blockHash == ([32]byte{}) {
		return errors.New("EVM block identity is incomplete")
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if knownBlock, ok := self.blockNumbers[blockHash]; ok {
		if knownBlock != block {
			return fmt.Errorf("EVM block hash 0x%x identifies block %d, not %d", blockHash, knownBlock, block)
		}
		return nil
	}
	if self.blockNumbers == nil {
		self.blockNumbers = map[[32]byte]uint64{}
	}
	// Pairing is immutable, so a wholesale reset only trades a later header
	// revalidation for a fixed memory bound. Canonicality is never cached.
	if len(self.blockNumbers) >= chainBlockIdentityCacheCapacity {
		clear(self.blockNumbers)
	}
	self.blockNumbers[blockHash] = block
	return nil
}

// Returns a previously authenticated number/hash pairing.
func (self *ChainClient) knownBlockIdentity(blockHash [32]byte) (uint64, bool) {
	if self == nil {
		return 0, false
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	block, ok := self.blockNumbers[blockHash]
	return block, ok
}

// Authenticates an uncached hash through eth_getBlockByHash exactly once in
// the ordinary sequential release path. Hash-bound eth_call still checks
// requireCanonical on every use, so caching only the immutable hash/number
// relation cannot conceal a later reorg.
func (self *ChainClient) validateBlockIdentityContext(ctx context.Context, block uint64, blockHash [32]byte) error {
	if ctx == nil || self == nil || self.client == nil {
		return errors.New("EVM block identity validator is unavailable")
	}
	if _, err := chainBlockHashSelector(block, blockHash); err != nil {
		return err
	}
	if knownBlock, ok := self.knownBlockIdentity(blockHash); ok {
		if knownBlock != block {
			return fmt.Errorf("EVM block hash 0x%x identifies block %d, not %d", blockHash, knownBlock, block)
		}
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	var header *chainRPCBlock
	err := self.client.Client().CallContext(callCtx, &header, "eth_getBlockByHash", common.Hash(blockHash), false)
	cancel()
	if err != nil {
		return fmt.Errorf("EVM block %d hash 0x%x header: %w", block, blockHash, err)
	}
	actualBlock, actualHash, err := header.identity()
	if err != nil || actualBlock == 0 {
		return fmt.Errorf("EVM block %d hash 0x%x header is empty or invalid", block, blockHash)
	}
	if actualHash != blockHash {
		return fmt.Errorf("EVM block %d hash response identifies 0x%x, not 0x%x", block, actualHash, blockHash)
	}
	if actualBlock != block {
		return fmt.Errorf("EVM block hash 0x%x identifies block %d, not %d", blockHash, actualBlock, block)
	}
	return self.rememberBlockIdentity(block, blockHash)
}

// Reads one contract value through the legacy height-only compatibility path.
// Release decisions use chainViewAtHashContext.
func chainViewAtContext[T any](ctx context.Context, c *ChainClient, block uint64, calldata []byte, unpack func([]byte) (T, error)) (T, error) {
	if ctx == nil {
		var out T
		return out, errors.New("chain view context is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	defer cancel()
	return bind.Call(c.contract, &bind.CallOpts{Context: ctx, BlockNumber: new(big.Int).SetUint64(block)}, calldata, unpack)
}

// Retains the background-context helper for existing narrow read methods.
func chainViewAt[T any](c *ChainClient, block uint64, calldata []byte, unpack func([]byte) (T, error)) (T, error) {
	return chainViewAtContext(context.Background(), c, block, calldata, unpack)
}

// Builds the EIP-1898 selector used by release reads. Requiring canonicality
// makes an endpoint reject a captured hash after a reorg instead of silently
// serving whichever state later occupies the same height.
func chainBlockHashSelector(block uint64, blockHash [32]byte) (rpc.BlockNumberOrHash, error) {
	if block == 0 || blockHash == ([32]byte{}) {
		return rpc.BlockNumberOrHash{}, errors.New("exact-block EVM identity is incomplete")
	}
	return rpc.BlockNumberOrHashWithHash(common.Hash(blockHash), true), nil
}

// Executes one call against the captured canonical hash. The block number is
// retained as part of the caller's identity and diagnostics; EIP-1898 itself
// intentionally selects by hash rather than by number.
func (self *ChainClient) ethCallAtHashContext(ctx context.Context, to common.Address, calldata []byte, block uint64, blockHash [32]byte) ([]byte, error) {
	if ctx == nil || self == nil || self.client == nil || to == (common.Address{}) || len(calldata) == 0 {
		return nil, errors.New("exact-block EVM call is unavailable")
	}
	if err := self.validateBlockIdentityContext(ctx, block, blockHash); err != nil {
		return nil, err
	}
	selector, err := chainBlockHashSelector(block, blockHash)
	if err != nil {
		return nil, err
	}
	var output hexutil.Bytes
	callCtx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	err = self.client.Client().CallContext(callCtx, &output, "eth_call", map[string]any{
		"to":    to,
		"input": hexutil.Bytes(calldata),
	}, selector)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("eth_call at canonical block %d (0x%x): %w", block, blockHash, err)
	}
	if len(output) == 0 {
		return nil, fmt.Errorf("eth_call at canonical block %d (0x%x) returned empty output", block, blockHash)
	}
	return append([]byte(nil), output...), nil
}

// Decodes one coordinator value from the captured canonical hash without
// routing through bind.Call's height-only selector.
func chainViewAtHashContext[T any](ctx context.Context, c *ChainClient, block uint64, blockHash [32]byte, calldata []byte, unpack func([]byte) (T, error)) (T, error) {
	var zero T
	if c == nil || unpack == nil {
		return zero, errors.New("exact-block chain view is unavailable")
	}
	output, err := c.ethCallAtHashContext(ctx, c.contractAddr, calldata, block, blockHash)
	if err != nil {
		return zero, err
	}
	value, err := unpack(output)
	if err != nil {
		return zero, fmt.Errorf("decode canonical block %d (0x%x) view: %w", block, blockHash, err)
	}
	return value, nil
}

// One exact-block EVM view in a bounded JSON-RPC batch. Keeping the target in
// every element lets one batch safely include coordinator and precompile calls.
type chainBatchCall struct {
	address  common.Address
	calldata []byte
}

// Shares one HTTP request among at most fifty hash-bound views while retaining
// response ordering by JSON-RPC id and checking every element independently.
func (self *ChainClient) batchCallsAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, calls []chainBatchCall) ([][]byte, error) {
	if ctx == nil || self == nil || self.client == nil {
		return nil, errors.New("exact-block EVM batch is unavailable")
	}
	if err := self.validateBlockIdentityContext(ctx, block, blockHash); err != nil {
		return nil, err
	}
	selector, err := chainBlockHashSelector(block, blockHash)
	if err != nil {
		return nil, err
	}
	outputs := make([][]byte, len(calls))
	for start := 0; start < len(calls); start += chainMaximumBatchCalls {
		end := min(start+chainMaximumBatchCalls, len(calls))
		raw := make([]hexutil.Bytes, end-start)
		batch := make([]rpc.BatchElem, end-start)
		for index := start; index < end; index++ {
			call := calls[index]
			if call.address == (common.Address{}) || len(call.calldata) == 0 {
				return nil, fmt.Errorf("exact-block EVM batch element %d is incomplete", index)
			}
			batch[index-start] = rpc.BatchElem{
				Method: "eth_call",
				Args: []any{
					map[string]any{"to": call.address, "input": hexutil.Bytes(call.calldata)},
					selector,
				},
				Result: &raw[index-start],
			}
		}
		callCtx, cancel := context.WithTimeout(ctx, chainCallTimeout)
		err := self.client.Client().BatchCallContext(callCtx, batch)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("eth_call batch at canonical block %d (0x%x): %w", block, blockHash, err)
		}
		for index := range batch {
			absolute := start + index
			if batch[index].Error != nil {
				return nil, fmt.Errorf("exact-block EVM batch element %d: %w", absolute, batch[index].Error)
			}
			if len(raw[index]) == 0 {
				return nil, fmt.Errorf("exact-block EVM batch element %d is empty", absolute)
			}
			outputs[absolute] = append([]byte(nil), raw[index]...)
		}
	}
	return outputs, nil
}

func (self *ChainClient) requireRelease() error {
	if !self.release || self.coordinator == nil {
		return errors.New("release coordinator binding is not active")
	}
	return nil
}

// ReleaseSnapshot is the minimal coordinator state read at one finalized EVM
// block. Its policy hash is compared with the locally canonical policy before
// any native-chain submission.
type ReleaseSnapshot struct {
	BlockNumber uint64
	BlockHash   [32]byte
	Epoch       *big.Int
	Policy      stabi.STCoordinatorPolicySnapshot
}

// ReleaseSnapshotContext reads one complete release snapshot while honoring
// caller cancellation across the finalized head and both contract views.
func (self *ChainClient) ReleaseSnapshotContext(ctx context.Context) (*ReleaseSnapshot, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	block, hash, err := self.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	epoch, err := chainViewAtHashContext(ctx, self, block, hash, self.coordinator.PackCurrentEpoch(), self.coordinator.UnpackCurrentEpoch)
	if err != nil {
		return nil, fmt.Errorf("currentEpoch at finalized block %d: %w", block, err)
	}
	if epoch == nil || epoch.Sign() < 0 || epoch.BitLen() > 256 {
		return nil, fmt.Errorf("currentEpoch at finalized block %d is outside uint256", block)
	}
	policy, err := chainViewAtHashContext(ctx, self, block, hash, self.coordinator.PackPolicyAt(epoch), self.coordinator.UnpackPolicyAt)
	if err != nil {
		return nil, fmt.Errorf("policyAt(%s) at finalized block %d: %w", epoch, block, err)
	}
	return &ReleaseSnapshot{BlockNumber: block, BlockHash: hash, Epoch: epoch, Policy: policy}, nil
}

// ReleaseSnapshot retains the background-context API for command callers.
func (self *ChainClient) ReleaseSnapshot() (*ReleaseSnapshot, error) {
	return self.ReleaseSnapshotContext(context.Background())
}

func (self *ChainClient) ReleaseNetuidAt(block uint64) (uint16, error) {
	return self.ReleaseNetuidAtContext(context.Background(), block)
}

// ReleaseNetuidAtContext retains the height-only compatibility read.
// Production decisions use ReleaseNetuidAtHashContext.
func (self *ChainClient) ReleaseNetuidAtContext(ctx context.Context, block uint64) (uint16, error) {
	if err := self.requireRelease(); err != nil {
		return 0, err
	}
	return chainViewAtContext(ctx, self, block, self.coordinator.PackNetuid(), self.coordinator.UnpackNetuid)
}

// ReleaseNetuidAtHashContext binds the coordinator netuid to one captured
// canonical block identity.
func (self *ChainClient) ReleaseNetuidAtHashContext(ctx context.Context, block uint64, blockHash [32]byte) (uint16, error) {
	if err := self.requireRelease(); err != nil {
		return 0, err
	}
	return chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackNetuid(), self.coordinator.UnpackNetuid)
}

func (self *ChainClient) ReleaseOperatorCountAt(block uint64) (*big.Int, error) {
	return self.ReleaseOperatorCountAtContext(context.Background(), block)
}

// ReleaseOperatorCountAtContext retains the height-only compatibility read.
// Production decisions use ReleaseOperatorCountAtHashContext.
func (self *ChainClient) ReleaseOperatorCountAtContext(ctx context.Context, block uint64) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtContext(ctx, self, block, self.coordinator.PackOperatorCount(), self.coordinator.UnpackOperatorCount)
}

// ReleaseOperatorCountAtHashContext binds the registry cardinality to one
// captured canonical block identity.
func (self *ChainClient) ReleaseOperatorCountAtHashContext(ctx context.Context, block uint64, blockHash [32]byte) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackOperatorCount(), self.coordinator.UnpackOperatorCount)
}

func (self *ChainClient) ReleaseOperatorIDAt(block uint64, index *big.Int) (*big.Int, error) {
	return self.ReleaseOperatorIDAtContext(context.Background(), block, index)
}

// ReleaseOperatorIDAtContext retains the height-only compatibility read.
func (self *ChainClient) ReleaseOperatorIDAtContext(ctx context.Context, block uint64, index *big.Int) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtContext(ctx, self, block, self.coordinator.PackOperatorIdAt(index), self.coordinator.UnpackOperatorIdAt)
}

// ReleaseOperatorIDAtHashContext binds one registry slot to one captured
// canonical block identity.
func (self *ChainClient) ReleaseOperatorIDAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, index *big.Int) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackOperatorIdAt(index), self.coordinator.UnpackOperatorIdAt)
}

func (self *ChainClient) ReleaseOperatorAt(block uint64, noID, epoch *big.Int) (stabi.STCoordinatorOperatorVersion, error) {
	return self.ReleaseOperatorAtContext(context.Background(), block, noID, epoch)
}

// ReleaseOperatorAtContext retains the height-only compatibility read.
func (self *ChainClient) ReleaseOperatorAtContext(ctx context.Context, block uint64, noID, epoch *big.Int) (stabi.STCoordinatorOperatorVersion, error) {
	if err := self.requireRelease(); err != nil {
		return stabi.STCoordinatorOperatorVersion{}, err
	}
	return chainViewAtContext(ctx, self, block, self.coordinator.PackOperatorAt(noID, epoch), self.coordinator.UnpackOperatorAt)
}

// ReleaseOperatorAtHashContext binds one operator version to one captured
// canonical block identity.
func (self *ChainClient) ReleaseOperatorAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, noID, epoch *big.Int) (stabi.STCoordinatorOperatorVersion, error) {
	if err := self.requireRelease(); err != nil {
		return stabi.STCoordinatorOperatorVersion{}, err
	}
	return chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackOperatorAt(noID, epoch), self.coordinator.UnpackOperatorAt)
}

func (self *ChainClient) ReleaseEpochDepositAt(block uint64, epoch, noID *big.Int) (*big.Int, error) {
	return self.ReleaseEpochDepositAtContext(context.Background(), block, epoch, noID)
}

// ReleaseEpochDepositAtContext retains the height-only compatibility read.
func (self *ChainClient) ReleaseEpochDepositAtContext(ctx context.Context, block uint64, epoch, noID *big.Int) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtContext(ctx, self, block, self.coordinator.PackEpochDeposits(epoch, noID), self.coordinator.UnpackEpochDeposits)
}

// ReleaseEpochDepositAtHashContext binds one deposit total to one captured
// canonical block identity.
func (self *ChainClient) ReleaseEpochDepositAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, epoch, noID *big.Int) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackEpochDeposits(epoch, noID), self.coordinator.UnpackEpochDeposits)
}

func (self *ChainClient) ReleaseEpochConvictionAddedAt(block uint64, epoch, noID *big.Int) (*big.Int, error) {
	return self.ReleaseEpochConvictionAddedAtContext(context.Background(), block, epoch, noID)
}

// ReleaseEpochConvictionAddedAtContext retains the height-only compatibility read.
func (self *ChainClient) ReleaseEpochConvictionAddedAtContext(ctx context.Context, block uint64, epoch, noID *big.Int) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtContext(ctx, self, block, self.coordinator.PackEpochConvictionAdded(epoch, noID), self.coordinator.UnpackEpochConvictionAdded)
}

// ReleaseEpochConvictionAddedAtHashContext binds one epoch increment to one
// captured canonical block identity.
func (self *ChainClient) ReleaseEpochConvictionAddedAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, epoch, noID *big.Int) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackEpochConvictionAdded(epoch, noID), self.coordinator.UnpackEpochConvictionAdded)
}

// ReleaseEpochStartBlockAt reads one epoch's immutable lower boundary at the
// same finalized EVM snapshot as the rest of a steering decision.
func (self *ChainClient) ReleaseEpochStartBlockAt(block uint64, epoch *big.Int) (uint64, error) {
	return self.ReleaseEpochStartBlockAtContext(context.Background(), block, epoch)
}

// ReleaseEpochStartBlockAtContext retains the height-only compatibility read.
func (self *ChainClient) ReleaseEpochStartBlockAtContext(ctx context.Context, block uint64, epoch *big.Int) (uint64, error) {
	if err := self.requireRelease(); err != nil {
		return 0, err
	}
	value, err := chainViewAtContext(ctx, self, block, self.coordinator.PackEpochStartBlock(epoch), self.coordinator.UnpackEpochStartBlock)
	if err != nil {
		return 0, err
	}
	if value == nil || !value.IsUint64() {
		return 0, errors.New("epoch start block exceeds uint64")
	}
	return value.Uint64(), nil
}

// ReleaseEpochStartBlockAtHashContext binds one epoch lower boundary to one
// captured canonical block identity.
func (self *ChainClient) ReleaseEpochStartBlockAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, epoch *big.Int) (uint64, error) {
	if err := self.requireRelease(); err != nil {
		return 0, err
	}
	value, err := chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackEpochStartBlock(epoch), self.coordinator.UnpackEpochStartBlock)
	if err != nil {
		return 0, err
	}
	if value == nil || !value.IsUint64() {
		return 0, errors.New("epoch start block exceeds uint64")
	}
	return value.Uint64(), nil
}

// ReleaseEpochEndBlockAt reads one rolled epoch's immutable upper boundary.
func (self *ChainClient) ReleaseEpochEndBlockAt(block uint64, epoch *big.Int) (uint64, error) {
	return self.ReleaseEpochEndBlockAtContext(context.Background(), block, epoch)
}

// ReleaseEpochEndBlockAtContext retains the height-only compatibility read.
func (self *ChainClient) ReleaseEpochEndBlockAtContext(ctx context.Context, block uint64, epoch *big.Int) (uint64, error) {
	if err := self.requireRelease(); err != nil {
		return 0, err
	}
	value, err := chainViewAtContext(ctx, self, block, self.coordinator.PackEpochEndBlock(epoch), self.coordinator.UnpackEpochEndBlock)
	if err != nil {
		return 0, err
	}
	if value == nil || !value.IsUint64() {
		return 0, errors.New("epoch end block exceeds uint64")
	}
	return value.Uint64(), nil
}

// ReleaseEpochEndBlockAtHashContext binds one epoch upper boundary to one
// captured canonical block identity.
func (self *ChainClient) ReleaseEpochEndBlockAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, epoch *big.Int) (uint64, error) {
	if err := self.requireRelease(); err != nil {
		return 0, err
	}
	value, err := chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackEpochEndBlock(epoch), self.coordinator.UnpackEpochEndBlock)
	if err != nil {
		return 0, err
	}
	if value == nil || !value.IsUint64() {
		return 0, errors.New("epoch end block exceeds uint64")
	}
	return value.Uint64(), nil
}

// ReleaseRootCommitmentAt reads the on-chain payout/content commitment used to
// bind a public signed artifact to the entitlement path.
func (self *ChainClient) ReleaseRootCommitmentAt(block uint64, epoch, noID *big.Int) (stabi.RootCommitmentsOutput, error) {
	return self.ReleaseRootCommitmentAtContext(context.Background(), block, epoch, noID)
}

// ReleaseRootCommitmentAtContext retains the height-only compatibility read.
func (self *ChainClient) ReleaseRootCommitmentAtContext(ctx context.Context, block uint64, epoch, noID *big.Int) (stabi.RootCommitmentsOutput, error) {
	if err := self.requireRelease(); err != nil {
		return stabi.RootCommitmentsOutput{}, err
	}
	return chainViewAtContext(ctx, self, block, self.coordinator.PackRootCommitments(epoch, noID), self.coordinator.UnpackRootCommitments)
}

// ReleaseRootCommitmentAtHashContext binds one settlement commitment to one
// captured canonical block identity.
func (self *ChainClient) ReleaseRootCommitmentAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, epoch, noID *big.Int) (stabi.RootCommitmentsOutput, error) {
	if err := self.requireRelease(); err != nil {
		return stabi.RootCommitmentsOutput{}, err
	}
	return chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackRootCommitments(epoch, noID), self.coordinator.UnpackRootCommitments)
}

func (self *ChainClient) ReleaseConvictionAt(block uint64, noID *big.Int) (*big.Int, error) {
	return self.ReleaseConvictionAtContext(context.Background(), block, noID)
}

// ReleaseConvictionAtContext retains the height-only compatibility read.
func (self *ChainClient) ReleaseConvictionAtContext(ctx context.Context, block uint64, noID *big.Int) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtContext(ctx, self, block, self.coordinator.PackCumulativeConviction(noID), self.coordinator.UnpackCumulativeConviction)
}

// ReleaseConvictionAtHashContext binds cumulative conviction to one captured
// canonical block identity.
func (self *ChainClient) ReleaseConvictionAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, noID *big.Int) (*big.Int, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	return chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackCumulativeConviction(noID), self.coordinator.UnpackCumulativeConviction)
}

func (self *ChainClient) ReleaseBindingAt(block uint64, clientID [16]byte, epoch *big.Int) (stabi.BindingAtOutput, error) {
	return self.ReleaseBindingAtContext(context.Background(), block, clientID, epoch)
}

// ReleaseBindingAtContext retains the height-only compatibility read.
func (self *ChainClient) ReleaseBindingAtContext(ctx context.Context, block uint64, clientID [16]byte, epoch *big.Int) (stabi.BindingAtOutput, error) {
	if err := self.requireRelease(); err != nil {
		return stabi.BindingAtOutput{}, err
	}
	return chainViewAtContext(ctx, self, block, self.coordinator.PackBindingAt(clientID, epoch), self.coordinator.UnpackBindingAt)
}

// ReleaseBindingAtHashContext binds one fleet binding to one captured
// canonical block identity.
func (self *ChainClient) ReleaseBindingAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, clientID [16]byte, epoch *big.Int) (stabi.BindingAtOutput, error) {
	if err := self.requireRelease(); err != nil {
		return stabi.BindingAtOutput{}, err
	}
	return chainViewAtHashContext(ctx, self, block, blockHash, self.coordinator.PackBindingAt(clientID, epoch), self.coordinator.UnpackBindingAt)
}

// Reads an ordered fleet-binding census at one finalized block using bounded
// JSON-RPC batches. Every result is decoded independently in caller order.
func (self *ChainClient) ReleaseBindingsAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, clientIDs [][16]byte, epoch *big.Int) ([]stabi.BindingAtOutput, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	if ctx == nil || block == 0 || epoch == nil || epoch.Sign() < 0 || epoch.BitLen() > 256 {
		return nil, errors.New("release binding batch identity is invalid")
	}
	calls := make([]chainBatchCall, len(clientIDs))
	for index, clientID := range clientIDs {
		calls[index] = chainBatchCall{
			address:  self.contractAddr,
			calldata: self.coordinator.PackBindingAt(clientID, epoch),
		}
	}
	outputs, err := self.batchCallsAtHashContext(ctx, block, blockHash, calls)
	if err != nil {
		return nil, err
	}
	bindings := make([]stabi.BindingAtOutput, len(outputs))
	for index, output := range outputs {
		binding, err := self.coordinator.UnpackBindingAt(output)
		if err != nil {
			return nil, fmt.Errorf("release binding batch element %d: %w", index, err)
		}
		bindings[index] = binding
	}
	return bindings, nil
}

// ReleaseBindingsAtContext retains the height-based compatibility surface.
// Production release decisions use ReleaseBindingsAtHashContext.
func (self *ChainClient) ReleaseBindingsAtContext(ctx context.Context, block uint64, clientIDs [][16]byte, epoch *big.Int) ([]stabi.BindingAtOutput, error) {
	if err := self.requireRelease(); err != nil {
		return nil, err
	}
	if ctx == nil || block == 0 || epoch == nil || epoch.Sign() < 0 || epoch.BitLen() > 256 {
		return nil, errors.New("release binding batch identity is invalid")
	}
	bindings := make([]stabi.BindingAtOutput, len(clientIDs))
	for index, clientID := range clientIDs {
		binding, err := self.ReleaseBindingAtContext(ctx, block, clientID, epoch)
		if err != nil {
			return nil, fmt.Errorf("release binding element %d: %w", index, err)
		}
		bindings[index] = binding
	}
	return bindings, nil
}

func (self *ChainClient) Close() {
	self.client.Close()
}

func (self *ChainClient) ChainId() *big.Int {
	return new(big.Int).Set(self.chainId)
}

func (self *ChainClient) RpcUrl() string {
	return self.rpcUrl
}

// chainView performs one read against the bound STSubnet contract.
func chainView[T any](c *ChainClient, calldata []byte, unpack func([]byte) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), chainCallTimeout)
	defer cancel()
	return bind.Call(c.contract, &bind.CallOpts{Context: ctx}, calldata, unpack)
}

func (self *ChainClient) BlockNumber() (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), chainCallTimeout)
	defer cancel()
	return self.client.BlockNumber(ctx)
}

// BlockHash returns the hash of a block by number.
func (self *ChainClient) BlockHash(number uint64) ([32]byte, error) {
	return self.BlockHashContext(context.Background(), number)
}

// BlockHashContext returns one numbered block hash under caller cancellation.
func (self *ChainClient) BlockHashContext(ctx context.Context, number uint64) ([32]byte, error) {
	if ctx == nil {
		return [32]byte{}, errors.New("block hash context is nil")
	}
	if self == nil || self.client == nil {
		return [32]byte{}, errors.New("block hash client or number is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	defer cancel()
	var header *chainRPCBlock
	err := self.client.Client().CallContext(ctx, &header, "eth_getBlockByNumber", hexutil.EncodeUint64(number), false)
	if err != nil {
		return [32]byte{}, err
	}
	if header == nil {
		return [32]byte{}, ethereum.NotFound
	}
	block, blockHash, err := header.identity()
	if err != nil || block != number {
		return [32]byte{}, fmt.Errorf("block %d header is empty or identifies another height", number)
	}
	if err := self.rememberBlockIdentity(number, blockHash); err != nil {
		return [32]byte{}, err
	}
	return blockHash, nil
}

// --- STSubnet views ---

func (self *ChainClient) Epoch() (*big.Int, error) {
	return chainView(self, self.st.PackEpoch(), self.st.UnpackEpoch)
}

func (self *ChainClient) PendingEpoch() (*big.Int, error) {
	return chainView(self, self.st.PackPendingEpoch(), self.st.UnpackPendingEpoch)
}

func (self *ChainClient) Netuid() (uint16, error) {
	return chainView(self, self.st.PackNetuid(), self.st.UnpackNetuid)
}

func (self *ChainClient) EpochCloseBlock(e *big.Int) (uint64, error) {
	return chainView(self, self.st.PackEpochCloseBlock(e), self.st.UnpackEpochCloseBlock)
}

// EpochStartBlock reads epochStartBlock() — the block the open epoch began at.
// It is the lower bound of the open epoch's deposit window (§8.1, D25): every
// Deposited event at or after it belongs to the current epoch's demand signal.
func (self *ChainClient) EpochStartBlock() (uint64, error) {
	return chainView(self, self.st.PackEpochStartBlock(), self.st.UnpackEpochStartBlock)
}

func (self *ChainClient) TrailsWindowBlocks() (uint64, error) {
	return chainView(self, self.st.PackTrailsWindowBlocks(), self.st.UnpackTrailsWindowBlocks)
}

func (self *ChainClient) Finalized(e *big.Int) (bool, error) {
	return chainView(self, self.st.PackFinalized(e), self.st.UnpackFinalized)
}

func (self *ChainClient) OperatorCount() (*big.Int, error) {
	return chainView(self, self.st.PackOperatorCount(), self.st.UnpackOperatorCount)
}

func (self *ChainClient) OperatorIds(i *big.Int) (*big.Int, error) {
	return chainView(self, self.st.PackOperatorIds(i), self.st.UnpackOperatorIds)
}

func (self *ChainClient) Operators(noId *big.Int) (stabi.OperatorsOutput, error) {
	return chainView(self, self.st.PackOperators(noId), self.st.UnpackOperators)
}

// --- Deposited event log (D25) — the per-NO deposit record ---
//
// v0.4/D25 dropped the on-chain deposit ledger (DT/totalDT): the contract stakes
// each deposit into the locked reserve and emits Deposited(e, noId, from, amount)
// but computes no weight. The event log IS the authoritative, published per-NO
// deposit record (WHITEPAPER §7.5) — validators sum it themselves to weight the
// pools (§8.1): the open epoch's deposits for the demand signal, the all-time
// cumulative for the conviction tier (§7.2).

// depositedTopic0 is topic0 of the Deposited log — keccak256 of the canonical
// event signature (the goldens in chain_test cross-check it).
var depositedTopic0 = keccak256([]byte("Deposited(uint256,uint256,address,uint256)"))

// getLogsChunkBlocks caps a single inclusive eth_getLogs block span at the
// official public endpoint's documented and enforced maximum. All-time scans
// are chunked and cached incrementally across tempos by the caller.
const getLogsChunkBlocks = uint64(1_000)

// DepositSums are per-NO summed deposit amounts (rao), keyed by noId.String()
// (a *big.Int is not a map key). Missing NO ⇒ zero.
type DepositSums map[string]*big.Int

// add folds one deposit into the per-NO running total.
func (self DepositSums) add(noId *big.Int, amount *big.Int) {
	key := noId.String()
	if self[key] == nil {
		self[key] = new(big.Int)
	}
	self[key].Add(self[key], amount)
}

// Get returns the summed deposits for noId (zero when the NO never deposited).
func (self DepositSums) Get(noId *big.Int) *big.Int {
	if v, ok := self[noId.String()]; ok {
		return v
	}
	return new(big.Int)
}

// DepositedSums scans the Deposited event log over [fromBlock, toBlock] and sums
// `amount` per noId (WHITEPAPER §7.5). When epochFilter is non-nil only that
// epoch's events are summed (topic1 = e), which is exactly the open epoch's
// demand signal; nil sums every epoch (the all-time conviction total, §7.2).
// The range is chunked so a range-capped RPC still answers.
func (self *ChainClient) DepositedSums(fromBlock uint64, toBlock uint64, epochFilter *big.Int) (DepositSums, error) {
	if toBlock < fromBlock {
		return DepositSums{}, nil
	}
	if self == nil || self.client == nil || self.st == nil || self.contractAddr == (common.Address{}) {
		return nil, errors.New("Deposited event scanner is unavailable")
	}
	if epochFilter != nil && (epochFilter.Sign() < 0 || epochFilter.BitLen() > 256) {
		return nil, errors.New("Deposited event epoch filter is outside uint256")
	}
	ctx := context.Background()
	finalizedBlock, finalizedHash, err := self.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	return self.depositedSumsAtFinalizedContext(ctx, fromBlock, toBlock, epochFilter, finalizedBlock, finalizedHash)
}

// Scans against a caller-captured finalized checkpoint. Every returned event
// must match its canonical numbered header, and the checkpoint is revalidated
// after all chunks so a changing branch cannot be committed into a cache.
func (self *ChainClient) depositedSumsAtFinalizedContext(ctx context.Context, fromBlock, toBlock uint64, epochFilter *big.Int, finalizedBlock uint64, finalizedHash [32]byte) (DepositSums, error) {
	if ctx == nil || self == nil || self.client == nil || self.st == nil || self.contractAddr == (common.Address{}) || finalizedBlock == 0 || finalizedHash == ([32]byte{}) || toBlock > finalizedBlock {
		return nil, errors.New("Deposited scan has no valid finalized checkpoint or exceeds it")
	}
	if epochFilter != nil && (epochFilter.Sign() < 0 || epochFilter.BitLen() > 256) {
		return nil, errors.New("Deposited event epoch filter is outside uint256")
	}
	sums := DepositSums{}
	if toBlock < fromBlock {
		return sums, nil
	}
	canonicalHash, err := self.BlockHashContext(ctx, finalizedBlock)
	if err != nil || canonicalHash != finalizedHash {
		return nil, fmt.Errorf("Deposited finalized checkpoint changed before scan: %v", err)
	}
	topics := [][]common.Hash{{common.Hash(depositedTopic0)}}
	if epochFilter != nil {
		topics = append(topics, []common.Hash{common.BigToHash(epochFilter)})
	}
	seenLogs := map[string]bool{}
	blockHashes := map[uint64][32]byte{}
	for from := fromBlock; ; {
		to := toBlock
		if toBlock-from >= getLogsChunkBlocks {
			to = from + getLogsChunkBlocks - 1
		}
		callCtx, cancel := context.WithTimeout(ctx, chainCallTimeout)
		logs, err := self.client.FilterLogs(callCtx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: []common.Address{self.contractAddr},
			Topics:    topics,
		})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("eth_getLogs Deposited [%d,%d]: %w", from, to, err)
		}
		for i := range logs {
			log := &logs[i]
			if log.Removed || log.Address != self.contractAddr || log.BlockNumber < from || log.BlockNumber > to || log.BlockHash == (common.Hash{}) || log.TxHash == (common.Hash{}) || len(log.Topics) != 3 || log.Topics[0] != common.Hash(depositedTopic0) || len(log.Data) != 64 {
				return nil, fmt.Errorf("eth_getLogs Deposited [%d,%d] returned malformed log %d", from, to, i)
			}
			if epochFilter != nil && log.Topics[1] != common.BigToHash(epochFilter) {
				return nil, fmt.Errorf("eth_getLogs Deposited [%d,%d] returned log %d outside the requested epoch", from, to, i)
			}
			blockHash := [32]byte(log.BlockHash)
			if previousHash, found := blockHashes[log.BlockNumber]; found && previousHash != blockHash {
				return nil, fmt.Errorf("eth_getLogs Deposited returned conflicting hashes for block %d", log.BlockNumber)
			}
			blockHashes[log.BlockNumber] = blockHash
			// Log index is block-global, so changing only the transaction hash
			// cannot turn one duplicate log into two independent deposits.
			identity := fmt.Sprintf("%s/%d", log.BlockHash, log.Index)
			if seenLogs[identity] {
				return nil, fmt.Errorf("eth_getLogs Deposited [%d,%d] repeated log %s", from, to, identity)
			}
			seenLogs[identity] = true
			event, err := self.st.UnpackDepositedEvent(log)
			if err != nil {
				return nil, fmt.Errorf("eth_getLogs Deposited [%d,%d] cannot decode log %d: %w", from, to, i, err)
			}
			if event.E == nil || event.NoId == nil || event.Amount == nil || event.NoId.Sign() <= 0 || event.Amount.Sign() <= 0 {
				return nil, fmt.Errorf("eth_getLogs Deposited [%d,%d] decoded log %d into invalid values", from, to, i)
			}
			if epochFilter != nil && event.E.Cmp(epochFilter) != 0 {
				return nil, fmt.Errorf("eth_getLogs Deposited [%d,%d] decoded log %d into another epoch", from, to, i)
			}
			sums.add(event.NoId, event.Amount)
		}
		if to == toBlock {
			break
		}
		from = to + 1
	}
	blockNumbers := make([]uint64, 0, len(blockHashes))
	for number := range blockHashes {
		blockNumbers = append(blockNumbers, number)
	}
	sort.Slice(blockNumbers, func(i, j int) bool { return blockNumbers[i] < blockNumbers[j] })
	for _, number := range blockNumbers {
		canonicalHash, err := self.BlockHashContext(ctx, number)
		if err != nil || canonicalHash != blockHashes[number] {
			return nil, fmt.Errorf("Deposited log block %d differs from its canonical finalized identity: %v", number, err)
		}
	}
	canonicalHash, err = self.BlockHashContext(ctx, finalizedBlock)
	if err != nil || canonicalHash != finalizedHash {
		return nil, fmt.Errorf("Deposited finalized checkpoint changed during scan: %v", err)
	}
	return sums, nil
}

// HeadClientIdToHotkey reads the head binding headClientIdToHotkey(clientId)
// (§11.2/§11.4). clientId is a provider's 32-byte client Ed25519 key (ckey);
// the returned hotkey is the zero word when the ckey is not bound to a
// top-level miner. The reverse map headHotkeyToClientId exists too but is not
// needed at steer time — we walk from the measured provider's ckey inward.
func (self *ChainClient) HeadClientIdToHotkey(clientId [32]byte) ([32]byte, error) {
	return chainView(self, self.st.PackHeadClientIdToHotkey(clientId), self.st.UnpackHeadClientIdToHotkey)
}

// --- STSubnet transactions (no v1 callers — the effort-bounty writes are
// deferred to the bounty phase; the generic path stays) ---

// sendAndWait signs + broadcasts calldata to the contract and waits for the
// receipt, decoding revert reasons best-effort (stctl conventions).
func (self *ChainClient) sendAndWait(key *ecdsa.PrivateKey, calldata []byte) (*types.Receipt, error) {
	opts := bind.NewKeyedTransactor(key, self.chainId)
	sendCtx, cancelSend := context.WithTimeout(context.Background(), chainSendTimeout)
	defer cancelSend()
	opts.Context = sendCtx

	from := crypto.PubkeyToAddress(key.PublicKey)
	tx, err := self.contract.RawTransact(opts, calldata)
	if err != nil {
		return nil, fmt.Errorf("send tx to %s: %w", self.contractAddr, self.explainCallError(err))
	}
	fmt.Printf("tx sent: %s (from %s nonce %d, %d byte calldata)\n", tx.Hash(), from, tx.Nonce(), len(calldata))

	waitCtx, cancelWait := context.WithTimeout(context.Background(), chainWaitMinedTimeout)
	defer cancelWait()
	receipt, err := bind.WaitMined(waitCtx, self.client, tx.Hash())
	if err != nil {
		return nil, fmt.Errorf("wait mined %s: %w", tx.Hash(), err)
	}
	status := "SUCCESS"
	if receipt.Status != types.ReceiptStatusSuccessful {
		status = "REVERTED"
	}
	fmt.Printf("tx mined: block %d status %s gas %d\n", receipt.BlockNumber, status, receipt.GasUsed)
	if receipt.Status != types.ReceiptStatusSuccessful {
		reason := self.replayRevertReason(from, calldata, receipt.BlockNumber)
		if reason != "" {
			return receipt, fmt.Errorf("transaction %s reverted: %s", tx.Hash(), reason)
		}
		return receipt, fmt.Errorf("transaction %s reverted", tx.Hash())
	}
	return receipt, nil
}

func (self *ChainClient) replayRevertReason(from common.Address, calldata []byte, blockNumber *big.Int) string {
	ctx, cancel := context.WithTimeout(context.Background(), chainCallTimeout)
	defer cancel()
	msg := ethereum.CallMsg{From: from, To: &self.contractAddr, Data: calldata}
	_, err := self.client.CallContract(ctx, msg, blockNumber)
	if err == nil {
		return ""
	}
	return decodeRevertError(self.st, err)
}

func (self *ChainClient) explainCallError(err error) error {
	if reason := decodeRevertError(self.st, err); reason != "" {
		return fmt.Errorf("%w (revert: %s)", err, reason)
	}
	return err
}

// decodeRevertError extracts a revert reason from an rpc error:
// Error(string) via abi.UnpackRevert, then STSubnet custom errors.
func decodeRevertError(st *stabi.STSubnet, err error) string {
	var dataErr rpc.DataError
	if !errors.As(err, &dataErr) {
		return ""
	}
	dataHex, ok := dataErr.ErrorData().(string)
	if !ok {
		return ""
	}
	raw := common.FromHex(dataHex)
	if len(raw) < 4 {
		return ""
	}
	if reason, unpackErr := abi.UnpackRevert(raw); unpackErr == nil {
		return fmt.Sprintf("%q", reason)
	}
	if custom, unpackErr := st.UnpackError(raw); unpackErr == nil {
		return fmt.Sprintf("%T%+v", custom, custom)
	}
	return fmt.Sprintf("raw revert data 0x%x", raw)
}

// --- IMetagraph (0x802) reads — hand-encoded eth_calls ---

// keccak256 with legacy Keccak-256 (the EVM hash).
func keccak256(data ...[]byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	for _, d := range data {
		h.Write(d)
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

// evmSelector returns the 4-byte selector of a canonical abi signature.
func evmSelector(signature string) [4]byte {
	hash := keccak256([]byte(signature))
	var selector [4]byte
	copy(selector[:], hash[:4])
	return selector
}

// evmUint16Word encodes a uint16 as one abi word.
func evmUint16Word(v uint16) [32]byte {
	var word [32]byte
	word[30] = byte(v >> 8)
	word[31] = byte(v)
	return word
}

func (self *ChainClient) ethCall(to common.Address, calldata []byte) ([]byte, error) {
	return self.ethCallAt(to, calldata, nil)
}

func (self *ChainClient) ethCallAt(to common.Address, calldata []byte, blockNumber *big.Int) ([]byte, error) {
	return self.ethCallAtContext(context.Background(), to, calldata, blockNumber)
}

func (self *ChainClient) ethCallAtContext(ctx context.Context, to common.Address, calldata []byte, blockNumber *big.Int) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("EVM call context is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	defer cancel()
	return self.client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: calldata}, blockNumber)
}

// MetagraphUidCount calls IMetagraph.getUidCount(netuid) on 0x802.
func (self *ChainClient) MetagraphUidCount(netuid uint16) (uint16, error) {
	selector := evmSelector("getUidCount(uint16)")
	arg := evmUint16Word(netuid)
	out, err := self.ethCall(metagraphAddress, append(selector[:], arg[:]...))
	if err != nil {
		return 0, fmt.Errorf("metagraph getUidCount: %w", err)
	}
	if len(out) < 32 {
		return 0, fmt.Errorf("metagraph getUidCount: short return (%d bytes)", len(out))
	}
	return uint16(out[30])<<8 | uint16(out[31]), nil
}

// MetagraphHotkey calls IMetagraph.getHotkey(netuid, uid) on 0x802.
func (self *ChainClient) MetagraphHotkey(netuid uint16, uid uint16) ([32]byte, error) {
	selector := evmSelector("getHotkey(uint16,uint16)")
	argNetuid := evmUint16Word(netuid)
	argUid := evmUint16Word(uid)
	calldata := append(selector[:], argNetuid[:]...)
	calldata = append(calldata, argUid[:]...)
	out, err := self.ethCall(metagraphAddress, calldata)
	if err != nil {
		return [32]byte{}, fmt.Errorf("metagraph getHotkey: %w", err)
	}
	if len(out) < 32 {
		return [32]byte{}, fmt.Errorf("metagraph getHotkey: short return (%d bytes)", len(out))
	}
	var hotkey [32]byte
	copy(hotkey[:], out[:32])
	return hotkey, nil
}

// FindUidByHotkey linearly scans the metagraph for a hotkey — the same
// bounded scan STSubnet._findUid performs (max_uids <= 256). Returns
// (uid, true) when found.
func (self *ChainClient) FindUidByHotkey(netuid uint16, hotkey [32]byte) (uint16, bool, error) {
	return self.FindUidByHotkeyAt(0, netuid, hotkey)
}

// FindUidByHotkeyAt resolves a live UID against one canonical EVM block.
// block=0 preserves the development/latest-state behavior.
func (self *ChainClient) FindUidByHotkeyAt(block uint64, netuid uint16, hotkey [32]byte) (uint16, bool, error) {
	return self.FindUidByHotkeyAtContext(context.Background(), block, netuid, hotkey)
}

// FindUidByHotkeyAtContext retains the height-only compatibility scan while
// honoring caller cancellation.
func (self *ChainClient) FindUidByHotkeyAtContext(ctx context.Context, block uint64, netuid uint16, hotkey [32]byte) (uint16, bool, error) {
	if ctx == nil {
		return 0, false, errors.New("metagraph UID context is nil")
	}
	if block != 0 {
		hotkeys, err := self.MetagraphHotkeysAtContext(ctx, block, netuid)
		if err != nil {
			return 0, false, err
		}
		uid, found := hotkeys[hotkey]
		return uid, found, nil
	}
	var blockNumber *big.Int
	selector := evmSelector("getUidCount(uint16)")
	argNetuid := evmUint16Word(netuid)
	out, err := self.ethCallAtContext(ctx, metagraphAddress, append(selector[:], argNetuid[:]...), blockNumber)
	if err != nil {
		return 0, false, err
	}
	if len(out) < 32 {
		return 0, false, fmt.Errorf("metagraph getUidCount: short return (%d bytes)", len(out))
	}
	n := uint16(out[len(out)-2])<<8 | uint16(out[len(out)-1])
	for uid := uint16(0); uid < n; uid++ {
		selector := evmSelector("getHotkey(uint16,uint16)")
		argUID := evmUint16Word(uid)
		calldata := make([]byte, 0, 68)
		calldata = append(calldata, selector[:]...)
		calldata = append(calldata, argNetuid[:]...)
		calldata = append(calldata, argUID[:]...)
		out, err := self.ethCallAtContext(ctx, metagraphAddress, calldata, blockNumber)
		if err != nil {
			return 0, false, err
		}
		if len(out) < 32 {
			return 0, false, fmt.Errorf("metagraph getHotkey: short return (%d bytes)", len(out))
		}
		var hk [32]byte
		copy(hk[:], out[len(out)-32:])
		if hk == hotkey {
			return uid, true, nil
		}
	}
	return 0, false, nil
}

// FindUidByHotkeyAtHashContext resolves one live UID from the metagraph state
// bound to a captured canonical block hash.
func (self *ChainClient) FindUidByHotkeyAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, netuid uint16, hotkey [32]byte) (uint16, bool, error) {
	hotkeys, err := self.MetagraphHotkeysAtHashContext(ctx, block, blockHash, netuid)
	if err != nil {
		return 0, false, err
	}
	uid, found := hotkeys[hotkey]
	return uid, found, nil
}

// MetagraphHotkeysAtContext retains the height-only compatibility census.
func (self *ChainClient) MetagraphHotkeysAtContext(ctx context.Context, block uint64, netuid uint16) (map[[32]byte]uint16, error) {
	if ctx == nil || block == 0 {
		return nil, errors.New("metagraph snapshot context or block is invalid")
	}
	blockNumber := new(big.Int).SetUint64(block)
	selector := evmSelector("getUidCount(uint16)")
	argNetuid := evmUint16Word(netuid)
	out, err := self.ethCallAtContext(ctx, metagraphAddress, append(selector[:], argNetuid[:]...), blockNumber)
	if err != nil {
		return nil, fmt.Errorf("metagraph getUidCount: %w", err)
	}
	if len(out) != 32 {
		return nil, fmt.Errorf("metagraph getUidCount: noncanonical return (%d bytes)", len(out))
	}
	for _, value := range out[:30] {
		if value != 0 {
			return nil, errors.New("metagraph getUidCount exceeds uint16")
		}
	}
	count := uint16(out[len(out)-2])<<8 | uint16(out[len(out)-1])
	hotkeys := make(map[[32]byte]uint16, count)
	for uidValue := uint32(0); uidValue < uint32(count); uidValue++ {
		uid := uint16(uidValue)
		selector := evmSelector("getHotkey(uint16,uint16)")
		argUID := evmUint16Word(uid)
		calldata := make([]byte, 0, 68)
		calldata = append(calldata, selector[:]...)
		calldata = append(calldata, argNetuid[:]...)
		calldata = append(calldata, argUID[:]...)
		output, err := self.ethCallAtContext(ctx, metagraphAddress, calldata, blockNumber)
		if err != nil {
			return nil, fmt.Errorf("metagraph getHotkey uid %d: %w", uid, err)
		}
		if len(output) != 32 {
			return nil, fmt.Errorf("metagraph getHotkey uid %d: noncanonical return (%d bytes)", uid, len(output))
		}
		var hotkey [32]byte
		copy(hotkey[:], output)
		if hotkey == ([32]byte{}) {
			return nil, fmt.Errorf("metagraph hotkey is zero at uid %d", uid)
		}
		if _, duplicated := hotkeys[hotkey]; duplicated {
			return nil, fmt.Errorf("metagraph hotkey is duplicated at uid %d", uid)
		}
		hotkeys[hotkey] = uid
	}
	return hotkeys, nil
}

// MetagraphHotkeysAtHashContext snapshots every live hotkey with all count and
// member calls bound to one EIP-1898 canonical block hash.
func (self *ChainClient) MetagraphHotkeysAtHashContext(ctx context.Context, block uint64, blockHash [32]byte, netuid uint16) (map[[32]byte]uint16, error) {
	selector := evmSelector("getUidCount(uint16)")
	argNetuid := evmUint16Word(netuid)
	output, err := self.ethCallAtHashContext(ctx, metagraphAddress, append(selector[:], argNetuid[:]...), block, blockHash)
	if err != nil {
		return nil, fmt.Errorf("metagraph getUidCount: %w", err)
	}
	if len(output) != 32 {
		return nil, fmt.Errorf("metagraph getUidCount: noncanonical return (%d bytes)", len(output))
	}
	for _, value := range output[:30] {
		if value != 0 {
			return nil, errors.New("metagraph getUidCount exceeds uint16")
		}
	}
	count := uint16(output[30])<<8 | uint16(output[31])
	calls := make([]chainBatchCall, count)
	for uidValue := uint32(0); uidValue < uint32(count); uidValue++ {
		uid := uint16(uidValue)
		selector := evmSelector("getHotkey(uint16,uint16)")
		argUID := evmUint16Word(uid)
		calldata := make([]byte, 0, 68)
		calldata = append(calldata, selector[:]...)
		calldata = append(calldata, argNetuid[:]...)
		calldata = append(calldata, argUID[:]...)
		calls[uid] = chainBatchCall{address: metagraphAddress, calldata: calldata}
	}
	outputs, err := self.batchCallsAtHashContext(ctx, block, blockHash, calls)
	if err != nil {
		return nil, fmt.Errorf("metagraph getHotkey batch: %w", err)
	}
	hotkeys := make(map[[32]byte]uint16, count)
	for uidValue, output := range outputs {
		uid := uint16(uidValue)
		if len(output) != 32 {
			return nil, fmt.Errorf("metagraph getHotkey uid %d: noncanonical return (%d bytes)", uid, len(output))
		}
		var hotkey [32]byte
		copy(hotkey[:], output)
		if hotkey == ([32]byte{}) {
			return nil, fmt.Errorf("metagraph hotkey is zero at uid %d", uid)
		}
		if _, duplicated := hotkeys[hotkey]; duplicated {
			return nil, fmt.Errorf("metagraph hotkey is duplicated at uid %d", uid)
		}
		hotkeys[hotkey] = uid
	}
	return hotkeys, nil
}

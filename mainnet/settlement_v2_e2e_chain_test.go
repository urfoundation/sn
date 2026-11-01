// The settlement v2 end-to-end chain (VERSION2-FINAL.md §15, "End-to-end in
// Go"): exact creation bytecode runs in geth's EVM over a StateDB, beside Go
// fakes of runtime 475's neuron (0x804), staking (0x805) and alpha (0x808)
// precompiles. Stake lives in the StateDB journal, so a Solidity revert rolls
// a runtime move back exactly. geth hands a precompile no caller, so a tracer
// OnEnter hook attributes each runtime call to its contract's mapped coldkey.
// The chain serves the JSON-RPC the claim daemon, snclaim, the operator server
// and the validator read, with EIP-1898 block selectors and the native
// genesis hash. Every mined block is final at once; blocks without
// transactions are virtual, so time can jump a whole epoch.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/ss58"
)

const (
	e2eChainID      = 964
	e2eNetuid       = 25
	e2eGasPrice     = 2
	e2eBaseFee      = 1
	e2eBlockGas     = 30_000_000
	e2eTxGas        = 15_000_000
	e2eGenesisTime  = 1_790_000_000
	e2eBlockSeconds = 12
	// Runtime 475 refuses a same-subnet move or transfer worth less than
	// 100,000 rao of TAO (AmountTooLow), and reads an amount of 2^64 − 1 or
	// more as the whole position (scripts/runtime-rehearsal/README.md).
	e2eTransferFloorTaoRao = 100_000
	e2eWholePosition       = ^uint64(0)
	// One rao is 10^9 wei of native balance; a registration burns
	// e2eBurnRao from the registering contract's balance.
	e2eRaoWei  = 1_000_000_000
	e2eBurnRao = 500_000_000
)

var (
	e2eNeuron  = common.HexToAddress("0x0000000000000000000000000000000000000804")
	e2eStaking = common.HexToAddress("0x0000000000000000000000000000000000000805")
	e2eAlpha   = common.HexToAddress("0x0000000000000000000000000000000000000808")
)

// The precompile surface the contracts call, with the gas the R battery
// measured inside a contract for each call.
var e2eRuntimeABI = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(`[
{"type":"function","name":"getStake","stateMutability":"view","inputs":[{"name":"hotkey","type":"bytes32"},{"name":"coldkey","type":"bytes32"},{"name":"netuid","type":"uint256"}],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"getMinerCollateral","stateMutability":"view","inputs":[{"name":"netuid","type":"uint16"},{"name":"hotkey","type":"bytes32"},{"name":"coldkey","type":"bytes32"}],"outputs":[{"name":"exists","type":"bool"},{"name":"locked","type":"uint64"},{"name":"drainRatio","type":"uint128"},{"name":"minLocked","type":"uint64"},{"name":"earned","type":"uint64"}]},
{"type":"function","name":"moveStake","stateMutability":"nonpayable","inputs":[{"name":"originHotkey","type":"bytes32"},{"name":"destinationHotkey","type":"bytes32"},{"name":"originNetuid","type":"uint256"},{"name":"destinationNetuid","type":"uint256"},{"name":"amount","type":"uint256"}],"outputs":[]},
{"type":"function","name":"transferStake","stateMutability":"nonpayable","inputs":[{"name":"destinationColdkey","type":"bytes32"},{"name":"hotkey","type":"bytes32"},{"name":"originNetuid","type":"uint256"},{"name":"destinationNetuid","type":"uint256"},{"name":"amount","type":"uint256"}],"outputs":[]},
{"type":"function","name":"registerLimit","stateMutability":"payable","inputs":[{"name":"netuid","type":"uint16"},{"name":"hotkey","type":"bytes32"},{"name":"limitPrice","type":"uint64"}],"outputs":[]},
{"type":"function","name":"getUid","stateMutability":"view","inputs":[{"name":"netuid","type":"uint16"},{"name":"hotkey","type":"bytes32"}],"outputs":[{"name":"exists","type":"bool"},{"name":"uid","type":"uint16"}]},
{"type":"function","name":"getAlphaPrice","stateMutability":"view","inputs":[{"name":"netuid","type":"uint16"}],"outputs":[{"name":"","type":"uint256"}]}
]`))
	if err != nil {
		panic(err)
	}
	return parsed
}()

var e2eRuntimeGas = map[string]uint64{
	"getStake": 4_748, "getMinerCollateral": 1_013, "moveStake": 81_753, "transferStake": 79_321,
	"registerLimit": 1_409_550, "getUid": 995, "getAlphaPrice": 1_000,
}

// Runtime 475 charges a dispatch's actual weight but admits it only if the gas
// forwarded to the precompile covers its declared weight, which for a move or
// a transfer walks a full 256-entry StakingHotkeys list. These are the
// forwarded gas the R battery's eth_estimateGas limits for a probe's call
// leave at the call (2,427,799 and 2,420,524 less intrinsic, calldata, the
// probe's own work and 1/64), so a transaction moving stake needs a limit of
// about 2.4M while it uses about 105,000.
var e2eRuntimeDeclaredGas = map[string]uint64{"moveStake": 2_366_000, "transferStake": 2_359_000}

// e2eBlockHash is block n's hash in the RPC domain. As on Subtensor, it is not
// Header.Hash(); readers take the explicit hash.
func e2eBlockHash(number uint64) common.Hash {
	return crypto.Keccak256Hash([]byte("urnetwork-e2e-block"), binary.BigEndian.AppendUint64(nil, number))
}

// e2eGenesisHash is the native chain's genesis hash, which the deployment's
// identity names and chain_getBlockHash(0) answers.
var e2eGenesisHash = crypto.Keccak256Hash([]byte("urnetwork-e2e-native-genesis"))

func e2eBlockTime(number uint64) uint64 { return e2eGenesisTime + e2eBlockSeconds*number }

// e2eRuntime holds the precompiles' knobs and, during one execution, its
// StateDB and the address of the contract calling a precompile with the gas it
// forwarded. The chain lock guards it.
type e2eRuntime struct {
	db        *state.StateDB
	caller    common.Address
	forwarded uint64
	// shortfall makes every move and transfer credit its destination one rao
	// short, as a share pool above price 1 can.
	shortfall bool
}

func e2eRuntimeSlot(kind string, words ...[32]byte) common.Hash {
	data := []byte(kind)
	for _, word := range words {
		data = append(data, word[:]...)
	}
	return crypto.Keccak256Hash(data)
}

func (self *e2eRuntime) word(address common.Address, slot common.Hash) *big.Int {
	return self.db.GetState(address, slot).Big()
}

func (self *e2eRuntime) setWord(address common.Address, slot common.Hash, value *big.Int) {
	self.db.SetState(address, slot, common.BigToHash(value))
}

func (self *e2eRuntime) stake(hotkey, coldkey [32]byte) *big.Int {
	return self.word(e2eStaking, e2eRuntimeSlot("stake", hotkey, coldkey))
}

func (self *e2eRuntime) setStake(hotkey, coldkey [32]byte, amount *big.Int) {
	self.setWord(e2eStaking, e2eRuntimeSlot("stake", hotkey, coldkey), amount)
}

func (self *e2eRuntime) collateral(hotkey, coldkey [32]byte) *big.Int {
	return self.word(e2eStaking, e2eRuntimeSlot("collateral", hotkey, coldkey))
}

func (self *e2eRuntime) conviction(hotkey, coldkey [32]byte) *big.Int {
	return self.word(e2eStaking, e2eRuntimeSlot("conviction", hotkey, coldkey))
}

func (self *e2eRuntime) price() *big.Int {
	return self.word(e2eAlpha, e2eRuntimeSlot("price"))
}

// uid returns the hotkey's UID, if registered.
func (self *e2eRuntime) uid(hotkey [32]byte) (uint16, bool) {
	value := self.word(e2eNeuron, e2eRuntimeSlot("uid", hotkey)).Uint64()
	if value == 0 {
		return 0, false
	}
	return uint16(value - 1), true
}

// position checks a move of amount off (hotkey, coldkey) as runtime 475 does:
// the whole-position sentinel, the stake, the collateral lock and the floor.
func (self *e2eRuntime) position(hotkey, coldkey [32]byte, amount *big.Int) (*big.Int, error) {
	held := self.stake(hotkey, coldkey)
	if amount.Cmp(new(big.Int).SetUint64(e2eWholePosition)) >= 0 {
		amount = new(big.Int).Set(held)
	}
	if amount.Cmp(held) > 0 {
		return nil, errors.New("NotEnoughStakeToWithdraw")
	}
	if new(big.Int).Sub(held, amount).Cmp(self.collateral(hotkey, coldkey)) < 0 {
		return nil, errors.New("StakeUnavailable")
	}
	tao := new(big.Int).Div(new(big.Int).Mul(amount, self.price()), big.NewInt(1e18))
	if amount.Sign() == 0 || tao.Cmp(big.NewInt(e2eTransferFloorTaoRao)) < 0 {
		return nil, errors.New("AmountTooLow")
	}
	return amount, nil
}

// credit adds amount to a destination position, one rao short when the
// shortfall knob is set.
func (self *e2eRuntime) credit(hotkey, coldkey [32]byte, amount *big.Int) {
	if self.shortfall {
		amount = new(big.Int).Sub(amount, common.Big1)
	}
	self.setStake(hotkey, coldkey, new(big.Int).Add(self.stake(hotkey, coldkey), amount))
}

// e2ePrecompile is one runtime precompile address.
type e2ePrecompile struct {
	runtime *e2eRuntime
	address common.Address
	methods []string
}

var _ vm.PrecompiledContract = (*e2ePrecompile)(nil)

func (self *e2ePrecompile) Name() string { return fmt.Sprintf("e2e-runtime-%x", self.address[18:]) }

func (self *e2ePrecompile) method(input []byte) (*abi.Method, error) {
	if len(input) < 4 {
		return nil, errors.New("e2e runtime: no selector")
	}
	method, err := e2eRuntimeABI.MethodById(input[:4])
	if err != nil || !slices.Contains(self.methods, method.Name) {
		return nil, fmt.Errorf("e2e runtime: %s has no method 0x%x", self.Name(), input[:4])
	}
	return method, nil
}

func (self *e2ePrecompile) RequiredGas(input []byte) uint64 {
	method, err := self.method(input)
	if err != nil {
		return 100
	}
	return e2eRuntimeGas[method.Name]
}

// Run answers one runtime call for the contract the tracer saw call it. A
// refusal returns an error, which the EVM turns into a failed call with empty
// return data that consumes the gas forwarded to it, as the runtime does.
func (self *e2ePrecompile) Run(input []byte) ([]byte, error) {
	method, err := self.method(input)
	if err != nil {
		return nil, err
	}
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, err
	}
	rt := self.runtime
	if rt.forwarded < e2eRuntimeDeclaredGas[method.Name] {
		return nil, vm.ErrOutOfGas
	}
	coldkey := ss58.EvmMirrorPubkey(rt.caller)
	netuid := func(value any) error {
		var n uint64
		switch v := value.(type) {
		case uint16:
			n = uint64(v)
		case *big.Int:
			if !v.IsUint64() {
				return errors.New("SubnetNotExists")
			}
			n = v.Uint64()
		}
		if n != e2eNetuid {
			return errors.New("SubnetNotExists")
		}
		return nil
	}
	switch method.Name {
	case "getStake":
		if err := netuid(args[2]); err != nil {
			return nil, err
		}
		return method.Outputs.Pack(rt.stake(args[0].([32]byte), args[1].([32]byte)))
	case "getMinerCollateral":
		if err := netuid(args[0]); err != nil {
			return nil, err
		}
		locked := rt.collateral(args[1].([32]byte), args[2].([32]byte))
		if locked.Sign() == 0 {
			return method.Outputs.Pack(false, uint64(0), new(big.Int), uint64(0), uint64(0))
		}
		return method.Outputs.Pack(true, locked.Uint64(), new(big.Int).Lsh(common.Big1, 64), uint64(0), uint64(0))
	case "moveStake":
		origin, destination := args[0].([32]byte), args[1].([32]byte)
		if err := errors.Join(netuid(args[2]), netuid(args[3])); err != nil {
			return nil, err
		}
		amount, err := rt.position(origin, coldkey, args[4].(*big.Int))
		if err != nil {
			return nil, err
		}
		rt.setStake(origin, coldkey, new(big.Int).Sub(rt.stake(origin, coldkey), amount))
		rt.credit(destination, coldkey, amount)
		return nil, nil
	case "transferStake":
		destination, hotkey := args[0].([32]byte), args[1].([32]byte)
		if err := errors.Join(netuid(args[2]), netuid(args[3])); err != nil {
			return nil, err
		}
		amount, err := rt.position(hotkey, coldkey, args[4].(*big.Int))
		if err != nil {
			return nil, err
		}
		// No coldkey opts in to conviction-locked alpha (SPEC A4): a transfer
		// to another coldkey may carry only the unlocked part.
		left := new(big.Int).Sub(rt.stake(hotkey, coldkey), amount)
		if destination != coldkey && left.Cmp(rt.conviction(hotkey, coldkey)) < 0 {
			return nil, errors.New("AccountRejectsLockedAlpha")
		}
		rt.setStake(hotkey, coldkey, left)
		rt.credit(hotkey, destination, amount)
		return nil, nil
	case "registerLimit":
		hotkey, limit := args[1].([32]byte), args[2].(uint64)
		if err := netuid(args[0]); err != nil {
			return nil, err
		}
		if _, exists := rt.uid(hotkey); exists {
			return nil, errors.New("HotKeyAlreadyRegisteredInSubNet")
		}
		owner := rt.db.GetState(e2eNeuron, e2eRuntimeSlot("owner", hotkey))
		if owner != (common.Hash{}) && owner != common.Hash(coldkey) {
			return nil, errors.New("NonAssociatedColdKey")
		}
		if e2eBurnRao > limit {
			return nil, errors.New("RegistrationPriceLimitExceeded")
		}
		// The burn is paid from the caller's mapped account, which is its
		// EVM balance; call value only funds that balance beforehand.
		price := new(uint256.Int).Mul(uint256.NewInt(e2eBurnRao), uint256.NewInt(e2eRaoWei))
		if rt.db.GetBalance(rt.caller).Lt(price) {
			return nil, errors.New("NotEnoughBalanceToStake")
		}
		rt.db.SubBalance(rt.caller, price, tracing.BalanceChangeUnspecified)
		next := rt.word(e2eNeuron, e2eRuntimeSlot("next-uid"))
		rt.setWord(e2eNeuron, e2eRuntimeSlot("next-uid"), new(big.Int).Add(next, common.Big1))
		rt.setWord(e2eNeuron, e2eRuntimeSlot("uid", hotkey), new(big.Int).Add(next, common.Big1))
		rt.db.SetState(e2eNeuron, e2eRuntimeSlot("owner", hotkey), common.Hash(coldkey))
		return nil, nil
	case "getUid":
		if err := netuid(args[0]); err != nil {
			return nil, err
		}
		uid, exists := rt.uid(args[1].([32]byte))
		return method.Outputs.Pack(exists, uid)
	default: // getAlphaPrice
		if err := netuid(args[0]); err != nil {
			return nil, err
		}
		return method.Outputs.Pack(rt.price())
	}
}

// e2eBlock is a block with a state change; virtual blocks between two of
// them hold the earlier one's state.
type e2eBlock struct {
	number   uint64
	root     common.Hash
	txs      types.Transactions
	receipts types.Receipts
}

type e2eIncluded struct {
	block *e2eBlock
	index int
}

// e2eChain serializes every execution, mining and RPC answer under one lock.
type e2eChain struct {
	t        testing.TB
	lock     sync.Mutex
	config   *params.ChainConfig
	signer   types.Signer
	database state.Database
	runtime  e2eRuntime
	head     uint64
	blocks   []*e2eBlock
	included map[common.Hash]*e2eIncluded
	logs     []*types.Log
	// pool holds sent transactions not yet mined: all of them while hold is
	// set, and any whose nonce leaves a gap.
	pool []*types.Transaction
	hold bool
	// nonceView answers eth_getTransactionCount with a stale nonce.
	nonceView map[common.Address]uint64
	// sent counts every transaction accepted from each sender over RPC.
	sent map[common.Address][]*types.Transaction
	// hashes maps each block hash the chain has served, in a block or a
	// receipt, to its number, for EIP-1898 hash selectors.
	hashes map[common.Hash]uint64
	server *httptest.Server
}

// newE2EChain starts a chain whose genesis funds the given accounts with the
// given wei and marks the runtime precompiles as code, which Solidity checks
// before a call without return data.
func newE2EChain(t testing.TB, funded map[common.Address]*big.Int) *e2eChain {
	t.Helper()
	config := *params.AllDevChainProtocolChanges
	config.ChainID = big.NewInt(e2eChainID)
	// The pinned toolchain targets cancun, and so does the runtime's EVM.
	config.PragueTime, config.OsakaTime = nil, nil
	config.BlobScheduleConfig = &params.BlobScheduleConfig{Cancun: params.DefaultCancunBlobConfig}
	chain := &e2eChain{t: t, config: &config, signer: types.LatestSignerForChainID(config.ChainID), database: state.NewDatabaseForTesting(),
		included: map[common.Hash]*e2eIncluded{}, nonceView: map[common.Address]uint64{}, sent: map[common.Address][]*types.Transaction{}, hashes: map[common.Hash]uint64{}}
	db, err := state.New(types.EmptyRootHash, chain.database)
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []common.Address{e2eNeuron, e2eStaking, e2eAlpha} {
		db.SetCode(address, []byte{byte(vm.STOP)}, tracing.CodeChangeUnspecified)
	}
	// Price 1 TAO per alpha until a test sets another.
	db.SetState(e2eAlpha, e2eRuntimeSlot("price"), common.BigToHash(big.NewInt(1e18)))
	for address, wei := range funded {
		db.SetBalance(address, uint256.MustFromBig(wei), tracing.BalanceChangeUnspecified)
	}
	root, err := db.Commit(0, true, false)
	if err != nil {
		t.Fatal(err)
	}
	chain.blocks = []*e2eBlock{{number: 0, root: root}}
	chain.server = httptest.NewServer(chain)
	t.Cleanup(chain.server.Close)
	return chain
}

// stateLocked opens the state after block number.
func (self *e2eChain) stateLocked(number uint64) *state.StateDB {
	index := sort.Search(len(self.blocks), func(i int) bool { return self.blocks[i].number > number }) - 1
	db, err := state.New(self.blocks[index].root, self.database)
	if err != nil {
		panic(err)
	}
	return db
}

// evmLocked executes in block number over db, with the runtime fakes and the
// tracer that attributes their calls.
func (self *e2eChain) evmLocked(db *state.StateDB, number uint64, call bool) *vm.EVM {
	context := vm.BlockContext{
		CanTransfer: core.CanTransfer, Transfer: core.Transfer, GetHash: e2eBlockHash,
		BlockNumber: new(big.Int).SetUint64(number), Time: e2eBlockTime(number), Difficulty: new(big.Int),
		GasLimit: e2eBlockGas, BaseFee: big.NewInt(e2eBaseFee), BlobBaseFee: common.Big1, Random: &common.Hash{},
	}
	hooks := &tracing.Hooks{OnEnter: func(depth int, typ byte, from, to common.Address, input []byte, gas uint64, value *big.Int) {
		if to == e2eNeuron || to == e2eStaking || to == e2eAlpha {
			self.runtime.caller, self.runtime.forwarded = from, gas
		}
	}}
	evm := vm.NewEVM(context, db, self.config, vm.Config{Tracer: hooks, NoBaseFee: call})
	precompiles := vm.ActivePrecompiledContracts(self.config.Rules(context.BlockNumber, true, context.Time))
	precompiles[e2eNeuron] = &e2ePrecompile{runtime: &self.runtime, address: e2eNeuron, methods: []string{"registerLimit", "getUid"}}
	precompiles[e2eStaking] = &e2ePrecompile{runtime: &self.runtime, address: e2eStaking, methods: []string{"getStake", "getMinerCollateral", "moveStake", "transferStake"}}
	precompiles[e2eAlpha] = &e2ePrecompile{runtime: &self.runtime, address: e2eAlpha, methods: []string{"getAlphaPrice"}}
	evm.SetPrecompiles(precompiles)
	self.runtime.db = db
	return evm
}

// commitLocked closes db as block number: the next block after the head.
func (self *e2eChain) commitLocked(db *state.StateDB, number uint64, txs types.Transactions, receipts types.Receipts) *e2eBlock {
	root, err := db.Commit(number, true, false)
	if err != nil {
		panic(err)
	}
	block := &e2eBlock{number: number, root: root, txs: txs, receipts: receipts}
	self.blocks = append(self.blocks, block)
	self.head = number
	self.hashes[e2eBlockHash(number)] = number
	for index, tx := range txs {
		self.included[tx.Hash()] = &e2eIncluded{block: block, index: index}
		self.logs = append(self.logs, receipts[index].Logs...)
	}
	return block
}

// mineLocked includes one transaction in the next block. A transaction the
// chain refuses before execution (nonce, funds) is not mined.
func (self *e2eChain) mineLocked(tx *types.Transaction) (*types.Receipt, []byte, error) {
	number := self.head + 1
	db := self.stateLocked(self.head)
	message, err := core.TransactionToMessage(tx, self.signer, big.NewInt(e2eBaseFee))
	if err != nil {
		return nil, nil, err
	}
	evm := self.evmLocked(db, number, false)
	db.SetTxContext(tx.Hash(), 0)
	result, err := core.ApplyMessage(evm, message, new(core.GasPool).AddGas(e2eBlockGas))
	if err != nil {
		return nil, nil, err
	}
	db.Finalise(true)
	receipt := &types.Receipt{Type: tx.Type(), Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: result.UsedGas, TxHash: tx.Hash(), GasUsed: result.UsedGas,
		EffectiveGasPrice: message.GasPrice, BlockHash: e2eBlockHash(number), BlockNumber: new(big.Int).SetUint64(number)}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed
	}
	if tx.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(message.From, tx.Nonce())
	}
	if receipt.Logs = db.GetLogs(tx.Hash(), number, receipt.BlockHash, e2eBlockTime(number)); receipt.Logs == nil {
		receipt.Logs = []*types.Log{}
	}
	receipt.Bloom = types.CreateBloom(receipt)
	self.commitLocked(db, number, types.Transactions{tx}, types.Receipts{receipt})
	return receipt, result.Revert(), nil
}

// minePoolLocked mines every pooled transaction whose nonce is next for its
// sender, oldest first, until none is.
func (self *e2eChain) minePoolLocked() {
	for {
		mined := false
		for index, tx := range self.pool {
			from, _ := types.Sender(self.signer, tx)
			if tx.Nonce() != self.stateLocked(self.head).GetNonce(from) {
				continue
			}
			self.pool = slices.Delete(self.pool, index, index+1)
			if _, _, err := self.mineLocked(tx); err != nil {
				self.t.Logf("e2e chain dropped transaction %s: %v", tx.Hash(), err)
			}
			mined = true
			break
		}
		if !mined {
			return
		}
	}
}

// do runs f under the chain lock.
func (self *e2eChain) do(f func()) {
	self.lock.Lock()
	defer self.lock.Unlock()
	f()
}

// rollTo makes number the next block: blocks up to number − 1 are virtual.
func (self *e2eChain) rollTo(number uint64) {
	self.t.Helper()
	self.do(func() {
		if number <= self.head {
			self.t.Fatalf("e2e chain cannot roll back to block %d from head %d", number, self.head)
		}
		self.head = number - 1
	})
}

// next returns the number of the block a transaction mines in now.
func (self *e2eChain) next() uint64 {
	var number uint64
	self.do(func() { number = self.head + 1 })
	return number
}

// native applies a runtime state change, such as emission, as its own block.
func (self *e2eChain) native(change func(*e2eRuntime)) {
	self.do(func() {
		db := self.stateLocked(self.head)
		self.runtime.db = db
		change(&self.runtime)
		db.Finalise(true)
		self.commitLocked(db, self.head+1, nil, nil)
	})
}

// emit stakes amount of new emission on (hotkey, coldkey), as Yuma pays a
// hotkey's owner.
func (self *e2eChain) emit(hotkey, coldkey [32]byte, amount *big.Int) {
	self.native(func(rt *e2eRuntime) {
		rt.setStake(hotkey, coldkey, new(big.Int).Add(rt.stake(hotkey, coldkey), amount))
	})
}

// stake reads a position at the head.
func (self *e2eChain) stake(hotkey, coldkey [32]byte) *big.Int {
	var amount *big.Int
	self.do(func() {
		self.runtime.db = self.stateLocked(self.head)
		amount = self.runtime.stake(hotkey, coldkey)
	})
	return amount
}

func (self *e2eChain) balance(address common.Address) *big.Int {
	var wei *big.Int
	self.do(func() { wei = self.stateLocked(self.head).GetBalance(address).ToBig() })
	return wei
}

func (self *e2eChain) nonce(address common.Address) uint64 {
	var nonce uint64
	self.do(func() { nonce = self.stateLocked(self.head).GetNonce(address) })
	return nonce
}

// send signs data to to (nil creates) from key and mines it at once in the
// next block, returning the receipt and any revert data.
func (self *e2eChain) send(key *ecdsa.PrivateKey, to *common.Address, value *big.Int, data []byte) (*types.Receipt, []byte) {
	self.t.Helper()
	var receipt *types.Receipt
	var revert []byte
	var err error
	self.do(func() {
		from := crypto.PubkeyToAddress(key.PublicKey)
		for _, pooled := range self.pool {
			if sender, _ := types.Sender(self.signer, pooled); sender == from {
				err = fmt.Errorf("e2e chain: %s has pooled transactions", from)
				return
			}
		}
		var tx *types.Transaction
		tx, err = types.SignNewTx(key, self.signer, &types.LegacyTx{Nonce: self.stateLocked(self.head).GetNonce(from), GasPrice: big.NewInt(e2eGasPrice), Gas: e2eTxGas, To: to, Value: value, Data: data})
		if err == nil {
			receipt, revert, err = self.mineLocked(tx)
		}
	})
	if err != nil {
		self.t.Fatal(err)
	}
	return receipt, revert
}

// include mines a signed transaction at once, as if a broadcast whose
// outcome its sender never recorded had landed.
func (self *e2eChain) include(tx *types.Transaction) *types.Receipt {
	self.t.Helper()
	var receipt *types.Receipt
	var err error
	self.do(func() { receipt, _, err = self.mineLocked(tx) })
	if err != nil {
		self.t.Fatal(err)
	}
	return receipt
}

// call runs a view at the head.
func (self *e2eChain) call(to common.Address, data []byte) ([]byte, error) {
	var out []byte
	var err error
	self.do(func() { out, _, err = self.callLocked(common.Address{}, &to, nil, data, e2eTxGas, self.head) })
	return out, err
}

// e2eRevertError is a call that reverted, with its data.
type e2eRevertError struct{ data []byte }

func (self *e2eRevertError) Error() string { return fmt.Sprintf("execution reverted: 0x%x", self.data) }

// callLocked executes a message without a transaction in block number, as
// eth_call does, and returns its output and gas used.
func (self *e2eChain) callLocked(from common.Address, to *common.Address, value *big.Int, data []byte, gas uint64, number uint64) ([]byte, uint64, error) {
	if value == nil {
		value = new(big.Int)
	}
	db := self.stateLocked(number)
	message := &core.Message{From: from, To: to, Value: value, GasLimit: gas, GasPrice: new(big.Int), GasFeeCap: new(big.Int), GasTipCap: new(big.Int), Data: data,
		SkipNonceChecks: true, SkipTransactionChecks: true}
	result, err := core.ApplyMessage(self.evmLocked(db, number, true), message, new(core.GasPool).AddGas(gas))
	if err != nil {
		return nil, 0, err
	}
	if errors.Is(result.Err, vm.ErrExecutionReverted) {
		return nil, result.UsedGas, &e2eRevertError{data: result.Revert()}
	}
	if result.Err != nil {
		return nil, result.UsedGas, result.Err
	}
	return result.ReturnData, result.UsedGas, nil
}

// estimateLocked finds the least gas limit within 1.5% that runs the message,
// as eth_estimateGas does.
func (self *e2eChain) estimateLocked(from common.Address, to *common.Address, value *big.Int, data []byte) (uint64, error) {
	_, used, err := self.callLocked(from, to, value, data, e2eTxGas, self.head)
	if err != nil {
		return 0, err
	}
	succeeds := func(gas uint64) bool {
		_, _, err := self.callLocked(from, to, value, data, gas, self.head)
		return err == nil
	}
	low, high := used-1, uint64(e2eTxGas)
	if optimistic := used * 64 / 63; optimistic < high {
		if succeeds(optimistic) {
			high = optimistic
		} else {
			low = optimistic
		}
	}
	for low+1 < high && (high-low)*1000 > high*15 {
		mid := (low + high) / 2
		if succeeds(mid) {
			high = mid
		} else {
			low = mid
		}
	}
	return high, nil
}

// holdTransactions keeps sent transactions in the pool until release.
func (self *e2eChain) holdTransactions() { self.do(func() { self.hold = true }) }

// release mines the pool and stops holding.
func (self *e2eChain) release() {
	self.do(func() {
		self.hold = false
		self.minePoolLocked()
	})
}

// pooled returns the transactions waiting in the pool.
func (self *e2eChain) pooled() []*types.Transaction {
	var txs []*types.Transaction
	self.do(func() { txs = slices.Clone(self.pool) })
	return txs
}

// sentBy returns every transaction the RPC accepted from address.
func (self *e2eChain) sentBy(address common.Address) []*types.Transaction {
	var txs []*types.Transaction
	self.do(func() { txs = slices.Clone(self.sent[address]) })
	return txs
}

// receipt returns a mined transaction's receipt.
func (self *e2eChain) receipt(hash common.Hash) *types.Receipt {
	var receipt *types.Receipt
	self.do(func() {
		if included := self.included[hash]; included != nil {
			receipt = included.block.receipts[included.index]
		}
	})
	return receipt
}

type e2eRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

type e2eRPCRequest struct {
	Version string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

// ServeHTTP answers one JSON-RPC request or a batch.
func (self *e2eChain) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	answer := func(call e2eRPCRequest) map[string]any {
		result, rpcErr := self.rpc(call.Method, call.Params)
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
		if rpcErr != nil {
			response["error"] = rpcErr
		} else {
			response["result"] = result
		}
		return response
	}
	var out any
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		var calls []e2eRPCRequest
		if err := json.Unmarshal(trimmed, &calls); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		responses := []map[string]any{}
		for _, call := range calls {
			responses = append(responses, answer(call))
		}
		out = responses
	} else {
		var call e2eRPCRequest
		if err := json.Unmarshal(trimmed, &call); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		out = answer(call)
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(out); err != nil {
		self.t.Errorf("e2e chain response: %v", err)
	}
}

// e2eCallArgs is an eth_call or eth_estimateGas message.
type e2eCallArgs struct {
	From  *common.Address `json:"from"`
	To    *common.Address `json:"to"`
	Gas   *hexutil.Uint64 `json:"gas"`
	Value *hexutil.Big    `json:"value"`
	Data  *hexutil.Bytes  `json:"data"`
	Input *hexutil.Bytes  `json:"input"`
}

func (self *e2eCallArgs) message() (common.Address, *big.Int, []byte) {
	var from common.Address
	if self.From != nil {
		from = *self.From
	}
	value := new(big.Int)
	if self.Value != nil {
		value = self.Value.ToInt()
	}
	var data []byte
	if self.Input != nil {
		data = *self.Input
	} else if self.Data != nil {
		data = *self.Data
	}
	return from, value, data
}

func e2eInvalidParams(err error) *e2eRPCError {
	return &e2eRPCError{Code: -32602, Message: "invalid params: " + err.Error()}
}

// blockLocked resolves a block selector to a number at or below the head;
// ok is false for a block beyond it. An EIP-1898 selector names a block by a
// hash the chain served, or by its number.
func (self *e2eChain) blockLocked(raw json.RawMessage) (uint64, bool, error) {
	if len(raw) == 0 {
		return self.head, true, nil
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '{' {
		var object struct {
			BlockNumber *hexutil.Uint64 `json:"blockNumber"`
			BlockHash   *common.Hash    `json:"blockHash"`
		}
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return 0, false, err
		}
		if object.BlockHash != nil {
			number, known := self.hashes[*object.BlockHash]
			if !known {
				return 0, false, fmt.Errorf("header for hash %s not found", object.BlockHash)
			}
			return number, true, nil
		}
		if object.BlockNumber == nil {
			return 0, false, errors.New("block selector names neither a hash nor a number")
		}
		return uint64(*object.BlockNumber), uint64(*object.BlockNumber) <= self.head, nil
	}
	var selector string
	if err := json.Unmarshal(raw, &selector); err != nil {
		return 0, false, err
	}
	switch selector {
	case "latest", "finalized", "safe", "pending":
		return self.head, true, nil
	}
	number, err := hexutil.DecodeUint64(selector)
	if err != nil {
		return 0, false, err
	}
	return number, number <= self.head, nil
}

func (self *e2eChain) rpc(method string, params []json.RawMessage) (any, *e2eRPCError) {
	self.lock.Lock()
	defer self.lock.Unlock()
	param := func(index int, out any) error {
		if index >= len(params) {
			return fmt.Errorf("missing parameter %d", index)
		}
		return json.Unmarshal(params[index], out)
	}
	block := func(index int) (uint64, bool, error) {
		if index >= len(params) {
			return self.head, true, nil
		}
		return self.blockLocked(params[index])
	}
	switch method {
	case "eth_chainId":
		return hexutil.EncodeUint64(e2eChainID), nil
	case "eth_blockNumber":
		return hexutil.EncodeUint64(self.head), nil
	case "eth_gasPrice":
		return hexutil.EncodeUint64(e2eGasPrice), nil
	case "eth_maxPriorityFeePerGas":
		return hexutil.EncodeUint64(e2eGasPrice - e2eBaseFee), nil
	case "eth_getBlockByNumber":
		number, ok, err := block(0)
		if err != nil {
			return nil, e2eInvalidParams(err)
		}
		if !ok {
			return nil, nil
		}
		return self.blockJSONLocked(number), nil
	case "eth_getTransactionCount":
		var address common.Address
		if err := param(0, &address); err != nil {
			return nil, e2eInvalidParams(err)
		}
		if view, stale := self.nonceView[address]; stale {
			return hexutil.EncodeUint64(view), nil
		}
		var selector string
		if len(params) > 1 {
			_ = json.Unmarshal(params[1], &selector)
		}
		number, ok, err := block(1)
		if err != nil || !ok {
			return nil, e2eInvalidParams(fmt.Errorf("block %v", err))
		}
		nonce := self.stateLocked(number).GetNonce(address)
		if selector == "pending" {
			for again := true; again; {
				again = false
				for _, tx := range self.pool {
					if from, _ := types.Sender(self.signer, tx); from == address && tx.Nonce() == nonce {
						nonce, again = nonce+1, true
					}
				}
			}
		}
		return hexutil.EncodeUint64(nonce), nil
	case "eth_call", "eth_estimateGas":
		var args e2eCallArgs
		if err := param(0, &args); err != nil {
			return nil, e2eInvalidParams(err)
		}
		from, value, data := args.message()
		if method == "eth_estimateGas" {
			gas, err := self.estimateLocked(from, args.To, value, data)
			if err != nil {
				return nil, e2eExecutionError(err)
			}
			return hexutil.EncodeUint64(gas), nil
		}
		number, ok, err := block(1)
		if err != nil || !ok {
			return nil, e2eInvalidParams(fmt.Errorf("block %v", err))
		}
		gas := uint64(e2eTxGas)
		if args.Gas != nil {
			gas = uint64(*args.Gas)
		}
		out, _, err := self.callLocked(from, args.To, value, data, gas, number)
		if err != nil {
			return nil, e2eExecutionError(err)
		}
		return hexutil.Encode(out), nil
	case "eth_getCode":
		var address common.Address
		if err := param(0, &address); err != nil {
			return nil, e2eInvalidParams(err)
		}
		number, ok, err := block(1)
		if err != nil || !ok {
			return nil, e2eInvalidParams(fmt.Errorf("block %v", err))
		}
		return hexutil.Encode(self.stateLocked(number).GetCode(address)), nil
	case "eth_getStorageAt":
		var address common.Address
		var slot common.Hash
		if err := errors.Join(param(0, &address), param(1, &slot)); err != nil {
			return nil, e2eInvalidParams(err)
		}
		number, ok, err := block(2)
		if err != nil || !ok {
			return nil, e2eInvalidParams(fmt.Errorf("block %v", err))
		}
		return self.stateLocked(number).GetState(address, slot).Hex(), nil
	case "eth_sendRawTransaction":
		return self.acceptLocked(params)
	case "eth_getTransactionReceipt":
		var hash common.Hash
		if err := param(0, &hash); err != nil {
			return nil, e2eInvalidParams(err)
		}
		if included := self.included[hash]; included != nil {
			return included.block.receipts[included.index], nil
		}
		return nil, nil
	case "eth_getLogs":
		return self.logsLocked(params)
	case "chain_getBlockHash":
		var number uint64
		if err := param(0, &number); err != nil || number != 0 {
			return nil, e2eInvalidParams(fmt.Errorf("only the native genesis block hash is served: %v", err))
		}
		return e2eGenesisHash, nil
	}
	return nil, &e2eRPCError{Code: -32601, Message: "the method " + method + " does not exist/is not available"}
}

// e2eExecutionError renders an execution failure as geth does: a revert is
// code 3 with its data.
func e2eExecutionError(err error) *e2eRPCError {
	var revert *e2eRevertError
	if errors.As(err, &revert) {
		message := "execution reverted"
		if reason, unpackErr := abi.UnpackRevert(revert.data); unpackErr == nil {
			message += ": " + reason
		}
		return &e2eRPCError{Code: 3, Message: message, Data: hexutil.Encode(revert.data)}
	}
	return &e2eRPCError{Code: -32000, Message: err.Error()}
}

// acceptLocked admits a signed transaction to the pool and, unless the chain
// holds transactions, mines what it can.
func (self *e2eChain) acceptLocked(params []json.RawMessage) (any, *e2eRPCError) {
	var raw hexutil.Bytes
	if len(params) != 1 || json.Unmarshal(params[0], &raw) != nil {
		return nil, e2eInvalidParams(errors.New("one raw transaction"))
	}
	var tx types.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, &e2eRPCError{Code: -32000, Message: "rlp: " + err.Error()}
	}
	from, err := types.Sender(self.signer, &tx)
	if err != nil {
		return nil, &e2eRPCError{Code: -32000, Message: "invalid sender: " + err.Error()}
	}
	if self.included[tx.Hash()] != nil || slices.ContainsFunc(self.pool, func(pooled *types.Transaction) bool { return pooled.Hash() == tx.Hash() }) {
		return nil, &e2eRPCError{Code: -32000, Message: "already known"}
	}
	if tx.Nonce() < self.stateLocked(self.head).GetNonce(from) {
		return nil, &e2eRPCError{Code: -32000, Message: "nonce too low"}
	}
	self.pool = append(self.pool, &tx)
	self.sent[from] = append(self.sent[from], &tx)
	if !self.hold {
		self.minePoolLocked()
	}
	return tx.Hash().Hex(), nil
}

// blockJSONLocked renders block number in geth's shape.
func (self *e2eChain) blockJSONLocked(number uint64) map[string]any {
	index := sort.Search(len(self.blocks), func(i int) bool { return self.blocks[i].number > number }) - 1
	mined := self.blocks[index]
	var txs types.Transactions
	var receipts types.Receipts
	if mined.number == number {
		txs, receipts = mined.txs, mined.receipts
	}
	transactions := []common.Hash{}
	var gasUsed uint64
	for k, tx := range txs {
		gasUsed += receipts[k].GasUsed
		transactions = append(transactions, tx.Hash())
	}
	txRoot, receiptRoot := types.EmptyTxsHash, types.EmptyReceiptsHash
	if len(txs) > 0 {
		txRoot, receiptRoot = types.DeriveSha(txs, trie.NewStackTrie(nil)), types.DeriveSha(receipts, trie.NewStackTrie(nil))
	}
	parent := common.Hash{}
	if number > 0 {
		parent = e2eBlockHash(number - 1)
	}
	self.hashes[e2eBlockHash(number)] = number
	return map[string]any{
		"number": hexutil.EncodeUint64(number), "hash": e2eBlockHash(number), "parentHash": parent, "nonce": types.BlockNonce{},
		"sha3Uncles": types.EmptyUncleHash, "logsBloom": types.MergeBloom(receipts), "transactionsRoot": txRoot, "stateRoot": mined.root,
		"receiptsRoot": receiptRoot, "miner": common.Address{}, "difficulty": "0x0", "totalDifficulty": "0x0", "extraData": "0x",
		"size": "0x0", "gasLimit": hexutil.EncodeUint64(e2eBlockGas), "gasUsed": hexutil.EncodeUint64(gasUsed),
		"timestamp": hexutil.EncodeUint64(e2eBlockTime(number)), "baseFeePerGas": hexutil.EncodeUint64(e2eBaseFee), "mixHash": common.Hash{},
		"withdrawalsRoot": types.EmptyWithdrawalsHash, "uncles": []any{}, "transactions": transactions,
	}
}

// logsLocked answers eth_getLogs over a block range, addresses and topics.
func (self *e2eChain) logsLocked(params []json.RawMessage) (any, *e2eRPCError) {
	var filter struct {
		FromBlock json.RawMessage   `json:"fromBlock"`
		ToBlock   json.RawMessage   `json:"toBlock"`
		Address   json.RawMessage   `json:"address"`
		Topics    []json.RawMessage `json:"topics"`
	}
	if len(params) != 1 || json.Unmarshal(params[0], &filter) != nil {
		return nil, e2eInvalidParams(errors.New("one filter"))
	}
	from, _, err := self.blockLocked(filter.FromBlock)
	if err != nil {
		return nil, e2eInvalidParams(err)
	}
	to, _, err := self.blockLocked(filter.ToBlock)
	if err != nil {
		return nil, e2eInvalidParams(err)
	}
	var addresses []common.Address
	if len(filter.Address) > 0 && string(filter.Address) != "null" {
		var single common.Address
		if json.Unmarshal(filter.Address, &single) == nil {
			addresses = []common.Address{single}
		} else if err := json.Unmarshal(filter.Address, &addresses); err != nil {
			return nil, e2eInvalidParams(err)
		}
	}
	topics := make([][]common.Hash, len(filter.Topics))
	for k, raw := range filter.Topics {
		if string(raw) == "null" {
			continue
		}
		var single common.Hash
		if json.Unmarshal(raw, &single) == nil {
			topics[k] = []common.Hash{single}
		} else if err := json.Unmarshal(raw, &topics[k]); err != nil {
			return nil, e2eInvalidParams(err)
		}
	}
	logs := []*types.Log{}
	for _, log := range self.logs {
		if log.BlockNumber < from || log.BlockNumber > to || len(addresses) > 0 && !slices.Contains(addresses, log.Address) {
			continue
		}
		matches := len(log.Topics) >= len(topics)
		for k := 0; matches && k < len(topics); k++ {
			matches = len(topics[k]) == 0 || slices.Contains(topics[k], log.Topics[k])
		}
		if matches {
			logs = append(logs, log)
		}
	}
	return logs, nil
}

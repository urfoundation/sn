//go:build linux || darwin

// The auditor sender drives the validator's real EVM submission against an
// in-process JSON-RPC chain. The v1 coordinator, STSettlement2 and vault2 are
// small Go models of the rules SPEC.md pins for the calls the sender makes;
// checkPath reads a configured fault instead of walking the path.
package validator

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/stabi"
)

const settlementV2TestChainID = 964

var (
	settlementV2TestCoordinator = common.HexToAddress("0xc0000000000000000000000000000000000000c1")
	settlementV2TestSettlement  = common.HexToAddress("0x5e77100000000000000000000000000000000001")
	settlementV2TestVault       = common.HexToAddress("0x7a17000000000000000000000000000000000001")
	settlementV2TestFee         = big.NewInt(2_000_000_000)
)

// The audited root: epoch 3, committed at block 405, over a finalized root of
// four accounts for epoch 2. Epoch e ends at block 100(e+1), so the approval
// deadline end(4) is block 500; the 50-block challenge window closes at 455.
func settlementV2TestPending() stabi.ISTSettlementVault2Pending {
	return stabi.ISTSettlementVault2Pending{Seq: 7, Root: [32]byte{0xa0}, Total: big.NewInt(150), LeafCount: 5, Epoch: 3, LedgerHash: [32]byte{0xa1}, CommittedBlock: 405}
}

func settlementV2TestRoot(epoch uint64) stabi.ISTSettlementVault2Root {
	return stabi.ISTSettlementVault2Root{Root: [32]byte{0xf0, byte(epoch)}, Total: big.NewInt(100), LeafCount: 4, Epoch: epoch, LedgerHash: [32]byte{0xf1, byte(epoch)}}
}

func settlementV2TestBlockHash(number uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(0xb10c_0000_0000 + number))
}

type settlementV2TestApproval struct {
	noID, seq, version uint64
	auditor            common.Address
}

// Demand state is kept per (seq, rootIndex, index).
type settlementV2TestDemand struct {
	noID, seq, rootIndex, index uint64
}

// vault2's DemandState (SPEC A1).
const (
	settlementV2TestDemandNone uint8 = iota
	settlementV2TestDemandOpen
	settlementV2TestDemandAnswered
)

// One block's contract state. Pending values are replaced, never mutated.
type settlementV2TestState struct {
	nonces   map[common.Address]uint64
	pending  map[uint64]stabi.ISTSettlementVault2Pending
	roots    map[uint64][]stabi.ISTSettlementVault2Root
	approved map[settlementV2TestApproval]bool
	demanded map[settlementV2TestDemand]uint8
	version  uint64
	auditors map[common.Address]bool
}

func (self *settlementV2TestState) clone() *settlementV2TestState {
	roots := map[uint64][]stabi.ISTSettlementVault2Root{}
	for noID, list := range self.roots {
		roots[noID] = slices.Clone(list)
	}
	return &settlementV2TestState{nonces: maps.Clone(self.nonces), pending: maps.Clone(self.pending), roots: roots, approved: maps.Clone(self.approved), demanded: maps.Clone(self.demanded), version: self.version, auditors: maps.Clone(self.auditors)}
}

// The eth_call and eth_estimateGas arguments the sender's client encodes.
type settlementV2TestCallArgs struct {
	From  common.Address `json:"from"`
	To    common.Address `json:"to"`
	Input hexutil.Bytes  `json:"input"`
	Value *hexutil.Big   `json:"value"`
}

// A Solidity custom error, as geth's JSON-RPC reports a revert.
type settlementV2TestRevert struct{ data []byte }

func (self *settlementV2TestRevert) Error() string  { return "execution reverted" }
func (self *settlementV2TestRevert) ErrorCode() int { return 3 }
func (self *settlementV2TestRevert) ErrorData() any { return hexutil.Encode(self.data) }

// Each transaction executes in its own block; finality trails by lag. The
// knobs change what the endpoint does with a send, never the sender's logic.
type settlementV2TestChain struct {
	lock        sync.Mutex
	url         string
	latest      uint64
	lag         uint64
	states      map[uint64]*settlementV2TestState
	receipts    map[common.Hash]*types.Receipt
	held        []*types.Transaction
	sent        []*types.Transaction
	calls       []settlementV2TestCallArgs
	counts      map[string]int
	failures    map[string]int
	faults      map[uint64]bool
	hold        bool
	loseAck     bool
	reject      int
	moveReceipt bool
	before      func(name string)
}

func newSettlementV2TestChain(t *testing.T, auditor common.Address) *settlementV2TestChain {
	t.Helper()
	state := &settlementV2TestState{nonces: map[common.Address]uint64{}, pending: map[uint64]stabi.ISTSettlementVault2Pending{1: settlementV2TestPending()},
		roots: map[uint64][]stabi.ISTSettlementVault2Root{1: {settlementV2TestRoot(2)}}, approved: map[settlementV2TestApproval]bool{}, demanded: map[settlementV2TestDemand]uint8{},
		version: 1, auditors: map[common.Address]bool{auditor: true}}
	chain := &settlementV2TestChain{latest: 410, states: map[uint64]*settlementV2TestState{410: state}, receipts: map[common.Hash]*types.Receipt{}, counts: map[string]int{}, failures: map[string]int{}, faults: map[uint64]bool{}}
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", chain); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() { httpServer.Close(); server.Stop() })
	chain.url = httpServer.URL
	return chain
}

// Counts one RPC or view by name and returns its configured failure, which
// starts at the configured call number.
func (self *settlementV2TestChain) hit(name string) error {
	if self.before != nil {
		self.before(name)
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	self.counts[name]++
	if from, found := self.failures[name]; found && self.counts[name] >= from {
		return fmt.Errorf("synthetic %s failure", name)
	}
	return nil
}

func (self *settlementV2TestChain) count(name string) int {
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.counts[name]
}

func (self *settlementV2TestChain) stateAt(selector gethrpc.BlockNumberOrHash) (*settlementV2TestState, uint64, error) {
	if hash, ok := selector.Hash(); ok {
		for number := range self.states {
			if settlementV2TestBlockHash(number) == hash {
				return self.states[number], number, nil
			}
		}
		return nil, 0, errors.New("unknown block hash")
	}
	number, _ := selector.Number()
	switch {
	case number == gethrpc.LatestBlockNumber || number == gethrpc.PendingBlockNumber:
		return self.states[self.latest], self.latest, nil
	case number == gethrpc.FinalizedBlockNumber:
		return self.states[self.latest-self.lag], self.latest - self.lag, nil
	case number >= 0 && self.states[uint64(number)] != nil:
		return self.states[uint64(number)], uint64(number), nil
	}
	return nil, 0, errors.New("unknown block")
}

func settlementV2TestABI(to common.Address) *abi.ABI {
	metadata := map[common.Address]*bind.MetaData{settlementV2TestCoordinator: &stabi.STCoordinatorMetaData, settlementV2TestSettlement: &stabi.STSettlement2MetaData, settlementV2TestVault: &stabi.STSettlementVault2MetaData}[to]
	if metadata == nil {
		return nil
	}
	contractABI, err := metadata.ParseABI()
	if err != nil {
		panic(err)
	}
	return contractABI
}

// Runs one call against state at block number as the contracts pinned in
// SPEC.md would. It mutates state; eth_call passes a clone.
func (self *settlementV2TestChain) execute(state *settlementV2TestState, number uint64, from, to common.Address, input []byte, value *big.Int) ([]byte, error) {
	contractABI := settlementV2TestABI(to)
	if contractABI == nil {
		return nil, nil
	}
	method, err := contractABI.MethodById(input)
	if err != nil {
		return nil, err
	}
	arguments, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, err
	}
	if value == nil {
		value = new(big.Int)
	}
	if value.Sign() != 0 && !method.IsPayable() {
		return nil, &settlementV2TestRevert{}
	}
	revert := func(name string) ([]byte, error) {
		id := contractABI.Errors[name].ID
		return nil, &settlementV2TestRevert{data: id[:4]}
	}
	switch method.Name {
	case "epochEndBlock":
		return method.Outputs.Pack(new(big.Int).Mul(new(big.Int).Add(arguments[0].(*big.Int), big.NewInt(1)), big.NewInt(100)))
	case "auditorSetVersion":
		return method.Outputs.Pack(state.version)
	case "CHALLENGE_BLOCKS":
		return method.Outputs.Pack(uint64(50))
	case "demandFeeWei":
		return method.Outputs.Pack(settlementV2TestFee)
	}
	noID := arguments[0].(*big.Int).Uint64()
	pending, roots := state.pending[noID], state.roots[noID]
	switch method.Name {
	case "pending":
		return method.Outputs.Pack(pending)
	case "rootCount":
		return method.Outputs.Pack(big.NewInt(int64(len(roots))))
	case "roots":
		return method.Outputs.Pack(roots[arguments[1].(*big.Int).Uint64()])
	case "hasApproved":
		return method.Outputs.Pack(pending.Seq != 0 && state.approved[settlementV2TestApproval{noID: noID, seq: pending.Seq, version: state.version, auditor: arguments[1].(common.Address)}])
	case "demanded":
		return method.Outputs.Pack(state.demanded[settlementV2TestDemand{noID: noID, seq: arguments[1].(uint64), rootIndex: arguments[2].(*big.Int).Uint64(), index: arguments[3].(*big.Int).Uint64()}])
	case "approveDistribution":
		key := settlementV2TestApproval{noID: noID, seq: pending.Seq, version: state.version, auditor: from}
		switch {
		case !state.auditors[from]:
			return revert("Unauthorized")
		case pending.Seq == 0:
			return revert("NoPendingRoot")
		case arguments[1].(uint64) != pending.Seq || arguments[2].([32]byte) != pending.Root || arguments[3].([32]byte) != pending.LedgerHash:
			return revert("PendingMismatch")
		case number >= 100*(pending.Epoch+2):
			return revert("ApprovalClosed")
		case state.approved[key]:
			return revert("AlreadyApproved")
		}
		state.approved[key] = true
		return nil, nil
	case "demandPath":
		rootIndex := arguments[1].(*big.Int)
		account := *abi.ConvertType(arguments[2], new(stabi.LedgerTreeAccount)).(*stabi.LedgerTreeAccount)
		key := settlementV2TestDemand{noID: noID, seq: pending.Seq, rootIndex: rootIndex.Uint64(), index: account.Index.Uint64()}
		switch {
		case value.Cmp(settlementV2TestFee) != 0:
			return revert("InvalidDemandFee")
		case pending.Seq == 0:
			return revert("NoPendingRoot")
		case rootIndex.Cmp(big.NewInt(int64(len(roots)))) >= 0:
			return revert("UnknownRoot")
		case number >= pending.CommittedBlock+50:
			return revert("ChallengeWindowClosed")
		case account.Index.Uint64() >= roots[rootIndex.Uint64()].LeafCount:
			return revert("IndexOutOfRange")
		case state.demanded[key] != settlementV2TestDemandNone:
			return revert("AlreadyDemanded")
		}
		state.demanded[key] = settlementV2TestDemandOpen
		pending.OpenDemands++
		state.pending[noID] = pending
		return nil, nil
	case "checkPath":
		account := *abi.ConvertType(arguments[2], new(stabi.LedgerTreeAccount)).(*stabi.LedgerTreeAccount)
		key := settlementV2TestDemand{noID: noID, seq: pending.Seq, rootIndex: arguments[1].(*big.Int).Uint64(), index: account.Index.Uint64()}
		switch {
		case pending.Seq == 0:
			return revert("NoPendingRoot")
		case self.faults[account.Index.Uint64()]:
			state.pending[noID] = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
			return method.Outputs.Pack(uint8(1))
		case state.demanded[key] == settlementV2TestDemandOpen:
			state.demanded[key] = settlementV2TestDemandAnswered
			pending.OpenDemands--
			state.pending[noID] = pending
			return method.Outputs.Pack(uint8(0))
		}
		return revert("NoFault")
	}
	return nil, fmt.Errorf("settlement v2 test chain has no %s", method.Name)
}

// Mines one transaction in a new block; a revert keeps only the nonce.
func (self *settlementV2TestChain) mineLocked(transaction *types.Transaction) {
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(settlementV2TestChainID)), transaction)
	if err != nil {
		panic(err)
	}
	number, state := self.latest+1, self.states[self.latest].clone()
	state.nonces[sender]++
	status := types.ReceiptStatusSuccessful
	if executed := state.clone(); func() error {
		_, err := self.execute(executed, number, sender, *transaction.To(), transaction.Data(), transaction.Value())
		return err
	}() != nil {
		status = types.ReceiptStatusFailed
	} else {
		state = executed
	}
	self.latest, self.states[number] = number, state
	self.receipts[transaction.Hash()] = &types.Receipt{Type: transaction.Type(), Status: status, CumulativeGasUsed: 60_000, Logs: []*types.Log{}, TxHash: transaction.Hash(),
		GasUsed: 60_000, EffectiveGasPrice: big.NewInt(1), BlockHash: settlementV2TestBlockHash(number), BlockNumber: new(big.Int).SetUint64(number)}
}

// Another party's transaction: one new block applying change.
func (self *settlementV2TestChain) mutate(change func(*settlementV2TestState)) {
	self.lock.Lock()
	defer self.lock.Unlock()
	state := self.states[self.latest].clone()
	change(state)
	self.latest++
	self.states[self.latest] = state
}

// Empty blocks until latest is the given block.
func (self *settlementV2TestChain) advanceTo(latest uint64) {
	for self.latest < latest {
		self.mutate(func(*settlementV2TestState) {})
	}
}

func (self *settlementV2TestChain) mineHeld() {
	self.lock.Lock()
	defer self.lock.Unlock()
	for _, transaction := range self.held {
		self.mineLocked(transaction)
	}
	self.held = nil
}

func (self *settlementV2TestChain) current() *settlementV2TestState {
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.states[self.latest]
}

func (self *settlementV2TestChain) accepted() []*types.Transaction {
	self.lock.Lock()
	defer self.lock.Unlock()
	return slices.Clone(self.sent)
}

func (self *settlementV2TestChain) ChainId() (*hexutil.Big, error) {
	return (*hexutil.Big)(big.NewInt(settlementV2TestChainID)), self.hit("eth_chainId")
}

func (self *settlementV2TestChain) BlockNumber() (hexutil.Uint64, error) {
	if err := self.hit("eth_blockNumber"); err != nil {
		return 0, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	return hexutil.Uint64(self.latest), nil
}

func (self *settlementV2TestChain) GetBlockByNumber(number gethrpc.BlockNumber, full bool) (map[string]any, error) {
	name := "eth_getBlockByNumber"
	if number == gethrpc.FinalizedBlockNumber {
		name += ":finalized"
	}
	if err := self.hit(name); err != nil {
		return nil, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	_, block, err := self.stateAt(gethrpc.BlockNumberOrHashWithNumber(number))
	if err != nil {
		return nil, err
	}
	return map[string]any{"number": hexutil.Uint64(block), "hash": settlementV2TestBlockHash(block)}, nil
}

func (self *settlementV2TestChain) GetBlockByHash(hash common.Hash, full bool) (map[string]any, error) {
	if err := self.hit("eth_getBlockByHash"); err != nil {
		return nil, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	_, block, err := self.stateAt(gethrpc.BlockNumberOrHashWithHash(hash, true))
	if err != nil {
		return nil, err
	}
	return map[string]any{"number": hexutil.Uint64(block), "hash": hash}, nil
}

// Views and preflights run as the next block would, at the selected state.
func (self *settlementV2TestChain) Call(args settlementV2TestCallArgs, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	name := "eth_call"
	if contractABI := settlementV2TestABI(args.To); contractABI != nil && len(args.Input) >= 4 {
		if method, err := contractABI.MethodById(args.Input); err == nil {
			name = method.Name
		}
	}
	if err := self.hit(name); err != nil {
		return nil, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	if args.Value != nil {
		self.calls = append(self.calls, args)
	}
	state, number, err := self.stateAt(selector)
	if err != nil {
		return nil, err
	}
	return self.execute(state.clone(), number+1, args.From, args.To, args.Input, (*big.Int)(args.Value))
}

func (self *settlementV2TestChain) EstimateGas(args settlementV2TestCallArgs, selector *gethrpc.BlockNumberOrHash) (hexutil.Uint64, error) {
	if err := self.hit("eth_estimateGas"); err != nil {
		return 0, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	self.calls = append(self.calls, args)
	_, err := self.execute(self.states[self.latest].clone(), self.latest+1, args.From, args.To, args.Input, (*big.Int)(args.Value))
	return 50_000, err
}

func (self *settlementV2TestChain) GetTransactionCount(address common.Address, selector gethrpc.BlockNumberOrHash) (hexutil.Uint64, error) {
	if err := self.hit("eth_getTransactionCount"); err != nil {
		return 0, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	state, _, err := self.stateAt(selector)
	if err != nil {
		return 0, err
	}
	nonce := state.nonces[address]
	if number, ok := selector.Number(); ok && number == gethrpc.PendingBlockNumber {
		nonce += uint64(len(self.held))
	}
	return hexutil.Uint64(nonce), nil
}

func (self *settlementV2TestChain) GasPrice() (*hexutil.Big, error) {
	return (*hexutil.Big)(big.NewInt(1)), self.hit("eth_gasPrice")
}

// Accepts a transaction at the account's next nonce. A refused send is never
// accepted; a lost acknowledgement is accepted, then reported as a failure.
func (self *settlementV2TestChain) SendRawTransaction(raw hexutil.Bytes) (common.Hash, error) {
	if err := self.hit("eth_sendRawTransaction"); err != nil {
		return common.Hash{}, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	transaction := new(types.Transaction)
	if err := transaction.UnmarshalBinary(raw); err != nil {
		return common.Hash{}, err
	}
	if self.reject > 0 {
		self.reject--
		return common.Hash{}, errors.New("synthetic send refused before acceptance")
	}
	if self.receipts[transaction.Hash()] != nil || slices.ContainsFunc(self.held, func(held *types.Transaction) bool { return held.Hash() == transaction.Hash() }) {
		return common.Hash{}, errors.New("already known")
	}
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(settlementV2TestChainID)), transaction)
	if err != nil {
		return common.Hash{}, err
	}
	if next := self.states[self.latest].nonces[sender] + uint64(len(self.held)); transaction.Nonce() != next {
		return common.Hash{}, fmt.Errorf("nonce %d, want %d", transaction.Nonce(), next)
	}
	self.sent = append(self.sent, transaction)
	if self.hold {
		self.held = append(self.held, transaction)
	} else {
		self.mineLocked(transaction)
	}
	if self.loseAck {
		return common.Hash{}, errors.New("synthetic acknowledgement lost after acceptance")
	}
	return transaction.Hash(), nil
}

func (self *settlementV2TestChain) GetTransactionReceipt(hash common.Hash) (*types.Receipt, error) {
	if err := self.hit("eth_getTransactionReceipt"); err != nil {
		return nil, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	receipt := self.receipts[hash]
	if receipt == nil {
		return nil, nil
	}
	copied := *receipt
	if self.moveReceipt {
		copied.BlockHash = common.Hash{0xde, 0xad}
	}
	return &copied, nil
}

// The chain, the auditor key and the record directory survive a restart.
type settlementV2SenderTestFixture struct {
	chain    *settlementV2TestChain
	sender   *SettlementV2Sender
	key      *ecdsa.PrivateKey
	keyFile  string
	stateDir string
}

func newSettlementV2SenderTestFixture(t *testing.T) *settlementV2SenderTestFixture {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("2b", 32))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &settlementV2SenderTestFixture{key: key, keyFile: filepath.Join(t.TempDir(), "auditor.key"), stateDir: t.TempDir()}
	if err := os.WriteFile(fixture.keyFile, []byte("0x"+strings.Repeat("2b", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.chain = newSettlementV2TestChain(t, crypto.PubkeyToAddress(key.PublicKey))
	fixture.restart(t)
	return fixture
}

// A new process: a fresh client and sender over the same records.
func (self *settlementV2SenderTestFixture) restart(t *testing.T) {
	t.Helper()
	client, err := ethclient.Dial(self.chain.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	evm := &ChainClient{client: client, rpcUrl: self.chain.url, coordinator: stabi.NewSTCoordinator(), contractAddr: settlementV2TestCoordinator, chainId: big.NewInt(settlementV2TestChainID), release: true}
	self.sender, err = NewSettlementV2Sender(evm, []string{self.chain.url}, settlementV2TestChainID, SettlementV2Addresses{Settlement: settlementV2TestSettlement, Vault: settlementV2TestVault}, self.keyFile, self.stateDir)
	if err != nil {
		t.Fatal(err)
	}
}

func (self *settlementV2SenderTestFixture) auditor() common.Address {
	return crypto.PubkeyToAddress(self.key.PublicKey)
}

func (self *settlementV2SenderTestFixture) approve(t *testing.T) error {
	return self.sender.Approve(t.Context(), 1, settlementV2TestPending())
}

// demand demands index of the latest (and only) finalized root, root 0.
func (self *settlementV2SenderTestFixture) demand(t *testing.T, index uint64) error {
	return self.demandAt(t, 0, index)
}

func (self *settlementV2SenderTestFixture) demandAt(t *testing.T, rootIndex, index uint64) error {
	account, oldProof, _ := settlementV2TestOpening(index)
	return self.sender.Demand(t.Context(), 1, settlementV2TestPending(), rootIndex, account, oldProof)
}

// The account at index of the finalized root, its proof and the pending path.
func settlementV2TestOpening(index uint64) (stabi.LedgerTreeAccount, []stabi.LedgerTreeProofNode, []stabi.LedgerTreePathNode) {
	return stabi.LedgerTreeAccount{Index: new(big.Int).SetUint64(index), Coldkey: [32]byte{0xc0}, Cumulative: big.NewInt(25)},
		[]stabi.LedgerTreeProofNode{{Hash: [32]byte{0xd0}, Sum: big.NewInt(30)}, {Hash: [32]byte{0xd1}, Sum: big.NewInt(45)}},
		[]stabi.LedgerTreePathNode{{LeftHash: [32]byte{0xe0}, LeftSum: big.NewInt(70), RightHash: [32]byte{0xe1}, RightSum: big.NewInt(80)}, {LeftHash: [32]byte{0xe2}, LeftSum: big.NewInt(30), RightHash: [32]byte{0xe3}, RightSum: big.NewInt(40)}}
}

func (self *settlementV2SenderTestFixture) challenge(t *testing.T, index uint64) error {
	account, oldProof, newPath := settlementV2TestOpening(index)
	return self.sender.Challenge(t.Context(), 1, settlementV2TestPending(), 0, account, oldProof, newPath)
}

// recordPath names the record of an action against root 0, which is also an
// approval's.
func (self *settlementV2SenderTestFixture) recordPath(action string, index uint64) string {
	return self.recordPathAt(action, 0, index)
}

func (self *settlementV2SenderTestFixture) recordPathAt(action string, rootIndex, index uint64) string {
	return filepath.Join(self.stateDir, "settlement-v2-sends", fmt.Sprintf("1-7-%s-%d-%d.json", action, rootIndex, index))
}

func (self *settlementV2SenderTestFixture) record(t *testing.T, action string, index uint64) settlementV2SendRecord {
	t.Helper()
	return self.recordAt(t, action, 0, index)
}

func (self *settlementV2SenderTestFixture) recordAt(t *testing.T, action string, rootIndex, index uint64) settlementV2SendRecord {
	t.Helper()
	encoded, err := os.ReadFile(self.recordPathAt(action, rootIndex, index))
	if err != nil {
		t.Fatal(err)
	}
	var record settlementV2SendRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

// One accepted transaction from the auditor to target with exactly data and value.
func (self *settlementV2SenderTestFixture) requireSent(t *testing.T, index int, target common.Address, data []byte, value *big.Int) *types.Transaction {
	t.Helper()
	sent := self.chain.accepted()
	if len(sent) <= index {
		t.Fatalf("%d transactions accepted, want index %d", len(sent), index)
	}
	transaction := sent[index]
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(settlementV2TestChainID)), transaction)
	if err != nil || sender != self.auditor() || *transaction.To() != target || !slices.Equal(transaction.Data(), data) || transaction.Value().Cmp(value) != 0 || transaction.ChainId().Uint64() != settlementV2TestChainID {
		t.Fatalf("transaction %d from %s to %s value %s differs: %v", index, sender, transaction.To(), transaction.Value(), err)
	}
	return transaction
}

// VA-3: an approval sent while the next block precedes end(e+1) counts on
// chain, carries exactly the audited root and no value, and is final; a
// second send of the same action signs nothing.
func TestSettlementV2ApprovalSentBeforeDeadline(t *testing.T) {
	t.Parallel()
	for _, latest := range []uint64{410, 498} {
		t.Run(fmt.Sprint(latest), func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			fixture.chain.advanceTo(latest)
			if err := fixture.approve(t); err != nil {
				t.Fatal(err)
			}
			pending := settlementV2TestPending()
			fixture.requireSent(t, 0, settlementV2TestSettlement, stabi.NewSTSettlement2().PackApproveDistribution(big.NewInt(1), pending.Seq, pending.Root, pending.LedgerHash), new(big.Int))
			if !fixture.chain.current().approved[settlementV2TestApproval{noID: 1, seq: 7, version: 1, auditor: fixture.auditor()}] || !fixture.record(t, "approve", 1).Finalized {
				t.Fatal("approval or its finalized record is absent")
			}
			if err := fixture.approve(t); err != nil || len(fixture.chain.accepted()) != 1 || fixture.chain.count("eth_estimateGas") != 1 {
				t.Fatalf("a repeated approval sent again: %v", err)
			}
		})
	}
}

// VA-3: once the next block is end(e+1), no approval is signed or sent.
func TestSettlementV2ApprovalRefusedAtDeadline(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	fixture.chain.advanceTo(499)
	if err := fixture.approve(t); !errors.Is(err, ErrSettlementV2ApprovalClosed) {
		t.Fatalf("approval at the deadline: %v", err)
	}
	if fixture.chain.count("eth_estimateGas")+fixture.chain.count("eth_sendRawTransaction") != 0 {
		t.Fatal("a closed approval reached submission")
	}
	if _, err := os.Stat(fixture.recordPath("approve", 1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a closed approval left a record: %v", err)
	}
}

// VA-3: a restart reconciles the recorded signed approval and never signs
// another, whether the endpoint lost the acknowledgement of a mined send,
// never received the send, or still holds it unmined.
func TestSettlementV2ApprovalIdempotentAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name  string
		setup func(*settlementV2TestChain)
	}{
		{name: "acknowledgement lost", setup: func(chain *settlementV2TestChain) { chain.loseAck = true }},
		{name: "never broadcast", setup: func(chain *settlementV2TestChain) { chain.reject = 1 }},
		{name: "unmined", setup: func(chain *settlementV2TestChain) { chain.hold, chain.loseAck = true, true }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			testCase.setup(fixture.chain)
			if err := fixture.approve(t); err == nil || errors.Is(err, ErrSettlementV2SendPending) {
				t.Fatalf("first send: %v", err)
			}
			prepared := fixture.record(t, "approve", 1)
			raw, err := hexutil.Decode(prepared.Transaction)
			if err != nil || prepared.Finalized {
				t.Fatalf("prepared record %+v: %v", prepared, err)
			}
			fixture.chain.lock.Lock()
			fixture.chain.loseAck = false
			fixture.chain.lock.Unlock()
			for attempt := 0; ; attempt++ {
				fixture.restart(t)
				err := fixture.approve(t)
				if err == nil {
					break
				}
				if !errors.Is(err, ErrSettlementV2SendPending) || attempt > 1 {
					t.Fatalf("restart %d: %v", attempt, err)
				}
				fixture.chain.mineHeld()
			}
			sent := fixture.chain.accepted()
			encoded, _ := sent[0].MarshalBinary()
			if len(sent) != 1 || !slices.Equal(encoded, raw) || !fixture.record(t, "approve", 1).Finalized {
				t.Fatalf("%d transactions accepted; the first is the recorded one: %t", len(sent), slices.Equal(encoded, raw))
			}
		})
	}
}

// VA-3: an approval already counted at the current auditor set version is
// success without a send; a new auditor set version needs a new approval.
func TestSettlementV2ApprovalFollowsAuditorSetVersion(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	fixture.chain.mutate(func(state *settlementV2TestState) {
		state.approved[settlementV2TestApproval{noID: 1, seq: 7, version: 1, auditor: fixture.auditor()}] = true
	})
	if err := fixture.approve(t); err != nil || len(fixture.chain.accepted()) != 0 {
		t.Fatalf("a counted approval was sent again: %v", err)
	}
	fixture.chain.mutate(func(state *settlementV2TestState) { state.version = 2 })
	if err := fixture.approve(t); err != nil || len(fixture.chain.accepted()) != 1 || !fixture.record(t, "approve", 2).Finalized {
		t.Fatalf("the new auditor set version's approval: %v", err)
	}
	if !fixture.chain.current().approved[settlementV2TestApproval{noID: 1, seq: 7, version: 2, auditor: fixture.auditor()}] {
		t.Fatal("the approval does not count at the new version")
	}
}

// VA-3: an approval or demand for a root that is no longer pending, or not
// the audited one, is refused before submission.
func TestSettlementV2RefusesRootNotPending(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*stabi.ISTSettlementVault2Pending){
		"deleted": func(pending *stabi.ISTSettlementVault2Pending) {
			*pending = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
		},
		"replaced":   func(pending *stabi.ISTSettlementVault2Pending) { pending.Seq, pending.Epoch = 8, 4 },
		"root":       func(pending *stabi.ISTSettlementVault2Pending) { pending.Root[1] = 1 },
		"total":      func(pending *stabi.ISTSettlementVault2Pending) { pending.Total = big.NewInt(151) },
		"leaf count": func(pending *stabi.ISTSettlementVault2Pending) { pending.LeafCount++ },
		"epoch":      func(pending *stabi.ISTSettlementVault2Pending) { pending.Epoch++ },
		"ledger":     func(pending *stabi.ISTSettlementVault2Pending) { pending.LedgerHash[1] = 1 },
		"commit":     func(pending *stabi.ISTSettlementVault2Pending) { pending.CommittedBlock++ },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			fixture.chain.mutate(func(state *settlementV2TestState) {
				pending := state.pending[1]
				change(&pending)
				state.pending[1] = pending
			})
			if err := fixture.approve(t); !errors.Is(err, ErrSettlementV2RootNotPending) {
				t.Fatalf("approval: %v", err)
			}
			if err := fixture.demand(t, 1); !errors.Is(err, ErrSettlementV2RootNotPending) {
				t.Fatalf("demand: %v", err)
			}
			if fixture.chain.count("eth_estimateGas") != 0 {
				t.Fatal("a refused action reached submission")
			}
		})
	}
	fixture := newSettlementV2SenderTestFixture(t)
	audited := settlementV2TestPending()
	audited.Total = nil
	if err := fixture.sender.Approve(t.Context(), 1, audited); !errors.Is(err, ErrSettlementV2RootNotPending) {
		t.Fatalf("an audited root without a total: %v", err)
	}
}

// VA-4: the demand proves the account against the finalized root and pays
// exactly demandFeeWei, in the preflight, the estimate and the signed transaction; it
// opens the index's demand and is final; a second send signs nothing.
func TestSettlementV2DemandPaysExactlyTheFee(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	if err := fixture.demand(t, 3); err != nil {
		t.Fatal(err)
	}
	account, oldProof, _ := settlementV2TestOpening(3)
	fixture.requireSent(t, 0, settlementV2TestVault, stabi.NewSTSettlementVault2().PackDemandPath(big.NewInt(1), big.NewInt(0), account, oldProof), settlementV2TestFee)
	fixture.chain.lock.Lock()
	calls := slices.Clone(fixture.chain.calls)
	fixture.chain.lock.Unlock()
	if len(calls) != 2 || (*big.Int)(calls[0].Value).Cmp(settlementV2TestFee) != 0 || (*big.Int)(calls[1].Value).Cmp(settlementV2TestFee) != 0 {
		t.Fatalf("preflight and estimate values %+v", calls)
	}
	if fixture.chain.current().demanded[settlementV2TestDemand{noID: 1, seq: 7, index: 3}] != settlementV2TestDemandOpen || fixture.chain.current().pending[1].OpenDemands != 1 || !fixture.record(t, "demand", 3).Finalized {
		t.Fatal("the demand or its record is absent")
	}
	if err := fixture.demand(t, 3); err != nil || len(fixture.chain.accepted()) != 1 {
		t.Fatalf("a repeated demand sent again: %v", err)
	}
}

// VA-4: an index already demanded, still open or answered, is success
// without a send: each index is demanded at most once per pending root (SPEC A1).
func TestSettlementV2DemandAlreadyDemanded(t *testing.T) {
	t.Parallel()
	for _, state := range []uint8{settlementV2TestDemandOpen, settlementV2TestDemandAnswered} {
		fixture := newSettlementV2SenderTestFixture(t)
		fixture.chain.mutate(func(current *settlementV2TestState) {
			current.demanded[settlementV2TestDemand{noID: 1, seq: 7, index: 3}] = state
		})
		if err := fixture.demand(t, 3); err != nil || len(fixture.chain.accepted()) != 0 {
			t.Fatalf("an index in demand state %d was demanded again: %v", state, err)
		}
	}
}

// VA-4: demand state and send records are kept per (seq, rootIndex, index):
// one index demanded against two finalized roots is two demands, each sent
// once with its own root index and recorded under it.
func TestSettlementV2DemandKeyedByRootIndex(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	fixture.chain.mutate(func(state *settlementV2TestState) {
		state.roots[1] = []stabi.ISTSettlementVault2Root{settlementV2TestRoot(1), settlementV2TestRoot(2)}
	})
	account, oldProof, _ := settlementV2TestOpening(3)
	for k, rootIndex := range []uint64{1, 0} {
		if err := fixture.demandAt(t, rootIndex, 3); err != nil {
			t.Fatal(err)
		}
		fixture.requireSent(t, k, settlementV2TestVault, stabi.NewSTSettlementVault2().PackDemandPath(big.NewInt(1), new(big.Int).SetUint64(rootIndex), account, oldProof), settlementV2TestFee)
		record := fixture.recordAt(t, "demand", rootIndex, 3)
		if !record.Finalized || record.RootIndex != rootIndex || record.Index != 3 ||
			fixture.chain.current().demanded[settlementV2TestDemand{noID: 1, seq: 7, rootIndex: rootIndex, index: 3}] != settlementV2TestDemandOpen {
			t.Fatalf("the demand against root %d or its record is absent: %+v", rootIndex, record)
		}
	}
	for _, rootIndex := range []uint64{1, 0} {
		if err := fixture.demandAt(t, rootIndex, 3); err != nil || len(fixture.chain.accepted()) != 2 {
			t.Fatalf("a repeated demand against root %d sent again: %v", rootIndex, err)
		}
	}
}

// VA-4: a demand is sent while the next block precedes commit +
// CHALLENGE_BLOCKS and refused from that block on.
func TestSettlementV2DemandRefusedAfterChallengeWindow(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	fixture.chain.advanceTo(453)
	if err := fixture.demand(t, 1); err != nil {
		t.Fatal(err)
	}
	fixture.chain.advanceTo(454)
	if err := fixture.demand(t, 2); !errors.Is(err, ErrSettlementV2ChallengeWindowClosed) || len(fixture.chain.accepted()) != 1 {
		t.Fatalf("a demand at the window's end: %v", err)
	}
}

// VA-8: a challenge that proves a fault deletes the pending root and
// succeeds; it carries exactly the audited opening and no value.
func TestSettlementV2ChallengeDeletesFaultedRoot(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	fixture.chain.faults[2] = true
	if err := fixture.challenge(t, 2); err != nil {
		t.Fatal(err)
	}
	account, oldProof, newPath := settlementV2TestOpening(2)
	fixture.requireSent(t, 0, settlementV2TestVault, stabi.NewSTSettlementVault2().PackCheckPath(big.NewInt(1), big.NewInt(0), account, oldProof, newPath), new(big.Int))
	if fixture.chain.current().pending[1].Seq != 0 || !fixture.record(t, "challenge", 2).Finalized {
		t.Fatal("the faulted root is still pending")
	}
	if err := fixture.challenge(t, 2); err != nil || len(fixture.chain.accepted()) != 1 {
		t.Fatalf("a repeated challenge sent again: %v", err)
	}
}

// VA-8: a challenge against a root already deleted or replaced counts as
// success without a send, including after a finalized later root.
func TestSettlementV2ChallengeAgainstGoneRootSucceeds(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*settlementV2TestState){
		"deleted": func(state *settlementV2TestState) {
			state.pending[1] = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
		},
		"replaced": func(state *settlementV2TestState) {
			state.pending[1] = stabi.ISTSettlementVault2Pending{Seq: 8, Total: big.NewInt(160), Epoch: 4}
		},
		"replaced and later finalized": func(state *settlementV2TestState) {
			state.pending[1] = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
			state.roots[1] = append(state.roots[1], settlementV2TestRoot(4))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			fixture.chain.faults[2] = true
			fixture.chain.mutate(change)
			if err := fixture.challenge(t, 2); err != nil || len(fixture.chain.accepted()) != 0 {
				t.Fatalf("a challenge against a gone root: %v", err)
			}
		})
	}
}

// VA-8: when another challenger deletes the root first, the recorded
// challenge reverts and the next send counts the deletion as success.
func TestSettlementV2ChallengeLosingRaceSucceeds(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	fixture.chain.faults[2] = true
	fixture.chain.hold, fixture.chain.loseAck = true, true
	if err := fixture.challenge(t, 2); err == nil {
		t.Fatal("the unacknowledged challenge reported success")
	}
	fixture.chain.mutate(func(state *settlementV2TestState) {
		state.pending[1] = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
	})
	fixture.chain.mineHeld()
	if receipt := fixture.chain.receipts[fixture.chain.accepted()[0].Hash()]; receipt.Status != types.ReceiptStatusFailed {
		t.Fatal("the late challenge did not revert")
	}
	if err := fixture.challenge(t, 2); err != nil || len(fixture.chain.accepted()) != 1 {
		t.Fatalf("a deleted root after a reverted challenge: %v", err)
	}
	if _, err := os.Stat(fixture.recordPath("challenge", 2)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the reverted challenge's record remains: %v", err)
	}
}

// VA-8: a challenged root that vault2 finalized is refused as a failure,
// whether it is the latest root or a later one followed it.
func TestSettlementV2ChallengeRefusesFinalizedRoot(t *testing.T) {
	t.Parallel()
	pending := settlementV2TestPending()
	finalized := stabi.ISTSettlementVault2Root{Root: pending.Root, Total: pending.Total, LeafCount: pending.LeafCount, Epoch: pending.Epoch, LedgerHash: pending.LedgerHash}
	for name, roots := range map[string][]stabi.ISTSettlementVault2Root{
		"latest":   {settlementV2TestRoot(2), finalized},
		"followed": {settlementV2TestRoot(2), finalized, settlementV2TestRoot(4)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			fixture.chain.mutate(func(state *settlementV2TestState) {
				state.pending[1] = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
				state.roots[1] = roots
			})
			if err := fixture.challenge(t, 2); !errors.Is(err, ErrSettlementV2RootFinalized) || len(fixture.chain.accepted()) != 0 {
				t.Fatalf("a challenge against a finalized root: %v", err)
			}
		})
	}
}

// VA-8: a challenge whose opening shows no fault answers the open demand;
// that is a failure, reported again without a second send.
func TestSettlementV2ChallengeAnsweringDemandFails(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	fixture.chain.mutate(func(state *settlementV2TestState) {
		state.demanded[settlementV2TestDemand{noID: 1, seq: 7, index: 2}] = settlementV2TestDemandOpen
	})
	for range 2 {
		if err := fixture.challenge(t, 2); !errors.Is(err, ErrSettlementV2ChallengeAnswered) || len(fixture.chain.accepted()) != 1 {
			t.Fatalf("an answering challenge: %v", err)
		}
	}
	if fixture.chain.current().pending[1].Seq != 7 || fixture.chain.current().demanded[settlementV2TestDemand{noID: 1, seq: 7, index: 2}] != settlementV2TestDemandAnswered {
		t.Fatal("the answer did not close the demand of the pending root")
	}
}

// VA-3, VA-4, VA-8: every refusal of the sender's own inputs and setup
// happens before anything is signed.
func TestSettlementV2SenderRefusesIncompleteInput(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	evm, addresses, rpcs := fixture.sender.chain, fixture.sender.addresses, []string{fixture.chain.url}
	stateFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(stateFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, construct := range map[string]func() (*SettlementV2Sender, error){
		"chain": func() (*SettlementV2Sender, error) {
			return NewSettlementV2Sender(nil, rpcs, 964, addresses, fixture.keyFile, fixture.stateDir)
		},
		"rpcs": func() (*SettlementV2Sender, error) {
			return NewSettlementV2Sender(evm, nil, 964, addresses, fixture.keyFile, fixture.stateDir)
		},
		"chain id": func() (*SettlementV2Sender, error) {
			return NewSettlementV2Sender(evm, rpcs, 0, addresses, fixture.keyFile, fixture.stateDir)
		},
		"settlement": func() (*SettlementV2Sender, error) {
			return NewSettlementV2Sender(evm, rpcs, 964, SettlementV2Addresses{Vault: addresses.Vault}, fixture.keyFile, fixture.stateDir)
		},
		"vault": func() (*SettlementV2Sender, error) {
			return NewSettlementV2Sender(evm, rpcs, 964, SettlementV2Addresses{Settlement: addresses.Settlement}, fixture.keyFile, fixture.stateDir)
		},
		"relative": func() (*SettlementV2Sender, error) {
			return NewSettlementV2Sender(evm, rpcs, 964, addresses, fixture.keyFile, "state")
		},
		"key": func() (*SettlementV2Sender, error) {
			return NewSettlementV2Sender(evm, rpcs, 964, addresses, stateFile, fixture.stateDir)
		},
		"directory": func() (*SettlementV2Sender, error) {
			return NewSettlementV2Sender(evm, rpcs, 964, addresses, fixture.keyFile, stateFile)
		},
	} {
		if sender, err := construct(); err == nil || sender != nil {
			t.Errorf("%s: an incomplete sender was constructed", name)
		}
	}
	account, oldProof, newPath := settlementV2TestOpening(2)
	for _, testCase := range []struct {
		name   string
		change func()
		demand bool
	}{
		{name: "index", change: func() { account.Index = nil }, demand: true},
		{name: "large index", change: func() { account.Index = new(big.Int).Lsh(big.NewInt(1), 64) }, demand: true},
		{name: "cumulative", change: func() { account.Cumulative = nil }, demand: true},
		{name: "proof sum", change: func() { oldProof[1].Sum = nil }, demand: true},
		{name: "path left sum", change: func() { newPath[0].LeftSum = nil }},
		{name: "path right sum", change: func() { newPath[1].RightSum = nil }},
	} {
		account, oldProof, newPath = settlementV2TestOpening(2)
		testCase.change()
		if err := fixture.sender.Challenge(t.Context(), 1, settlementV2TestPending(), 0, account, oldProof, newPath); err == nil || !strings.Contains(err.Error(), "challenge account or path has a missing or oversized integer") {
			t.Errorf("challenge %s: %v", testCase.name, err)
		}
		if !testCase.demand {
			continue
		}
		if err := fixture.sender.Demand(t.Context(), 1, settlementV2TestPending(), 0, account, oldProof); err == nil || !strings.Contains(err.Error(), "demand account or proof has a missing or oversized integer") {
			t.Errorf("demand %s: %v", testCase.name, err)
		}
	}
	account, oldProof, _ = settlementV2TestOpening(2)
	unsequenced := settlementV2TestPending()
	unsequenced.Seq = 0
	if err := fixture.sender.Demand(t.Context(), 1, unsequenced, 0, account, oldProof); err == nil || !strings.Contains(err.Error(), "no commit sequence") {
		t.Fatalf("a root without a sequence: %v", err)
	}
	if fixture.chain.count("eth_estimateGas") != 0 || len(fixture.chain.accepted()) != 0 {
		t.Fatal("refused input reached submission")
	}
}

// VA-8 (SPEC A6): a front-running revert at the preflight is success without
// a send: the auditor's approval already counted, the index already demanded,
// or the challenged root already deleted, by a transaction the finalized head
// does not show yet.
func TestSettlementV2FrontRunningIsSuccess(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name, preflight string
		change          func(*settlementV2TestState, common.Address)
		send            func(*testing.T, *settlementV2SenderTestFixture) error
	}{
		{name: "approve AlreadyApproved", preflight: "approveDistribution", change: func(state *settlementV2TestState, auditor common.Address) {
			state.approved[settlementV2TestApproval{noID: 1, seq: 7, version: 1, auditor: auditor}] = true
		}, send: func(t *testing.T, fixture *settlementV2SenderTestFixture) error { return fixture.approve(t) }},
		{name: "demand AlreadyDemanded", preflight: "demandPath", change: func(state *settlementV2TestState, _ common.Address) {
			state.demanded[settlementV2TestDemand{noID: 1, seq: 7, index: 2}] = settlementV2TestDemandOpen
		}, send: func(t *testing.T, fixture *settlementV2SenderTestFixture) error { return fixture.demand(t, 2) }},
		{name: "challenge NoPendingRoot", preflight: "checkPath", change: func(state *settlementV2TestState, _ common.Address) {
			state.pending[1] = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
		}, send: func(t *testing.T, fixture *settlementV2SenderTestFixture) error { return fixture.challenge(t, 2) }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			fixture.chain.faults[2] = true
			fixture.chain.lag = 1
			fixture.chain.mutate(func(state *settlementV2TestState) { testCase.change(state, fixture.auditor()) })
			if err := testCase.send(t, fixture); err != nil {
				t.Fatalf("a front-run action: %v", err)
			}
			if fixture.chain.count(testCase.preflight) != 1 || fixture.chain.count("eth_estimateGas") != 0 || len(fixture.chain.accepted()) != 0 {
				t.Fatal("a front-run action did not stop at its preflight")
			}
		})
	}
}

// VA-8 (SPEC A6): only revert data that is exactly the action's custom error
// selector is front-running.
func TestSettlementV2RevertedMatchesOnlyTheExactError(t *testing.T) {
	t.Parallel()
	selector := evmSelector("NoPendingRoot()")
	other := evmSelector("NoFault()")
	for name, testCase := range map[string]struct {
		err  error
		want bool
	}{
		"exact":     {err: fmt.Errorf("preflight eth_call failed: %w", &settlementV2TestRevert{data: selector[:]}), want: true},
		"nil":       {err: nil},
		"plain":     {err: errors.New("execution reverted")},
		"other":     {err: &settlementV2TestRevert{data: other[:]}},
		"arguments": {err: &settlementV2TestRevert{data: append(selector[:], make([]byte, 32)...)}},
		"empty":     {err: &settlementV2TestRevert{}},
		"not hex":   {err: settlementV2TestRevertData{"0xzz"}},
		"not text":  {err: settlementV2TestRevertData{7}},
	} {
		if settlementV2Reverted(testCase.err, selector) != testCase.want {
			t.Errorf("%s: matched %t", name, !testCase.want)
		}
	}
}

// Revert data of an arbitrary JSON shape.
type settlementV2TestRevertData struct{ data any }

func (self settlementV2TestRevertData) Error() string  { return "execution reverted" }
func (self settlementV2TestRevertData) ErrorData() any { return self.data }

// VA-3, VA-4, VA-8: every chain read the sender depends on fails closed:
// nothing is signed.
func TestSettlementV2SenderReadFailures(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		action string
		read   string
		from   int
	}{
		{action: "approve", read: "eth_getBlockByNumber:finalized", from: 1},
		{action: "approve", read: "auditorSetVersion", from: 1},
		{action: "approve", read: "eth_getBlockByNumber:finalized", from: 2},
		{action: "approve", read: "pending", from: 1},
		{action: "approve", read: "hasApproved", from: 1},
		{action: "approve", read: "epochEndBlock", from: 1},
		{action: "approve", read: "eth_blockNumber", from: 1},
		{action: "demand", read: "eth_getBlockByNumber:finalized", from: 1},
		{action: "demand", read: "demandFeeWei", from: 1},
		{action: "demand", read: "pending", from: 1},
		{action: "demand", read: "demanded", from: 1},
		{action: "demand", read: "CHALLENGE_BLOCKS", from: 1},
		{action: "challenge", read: "pending", from: 1},
		{action: "challenge", read: "eth_getBlockByNumber:finalized", from: 2},
		{action: "challenge", read: "rootCount", from: 1},
		{action: "challenge", read: "roots", from: 1},
	} {
		t.Run(testCase.action+" "+testCase.read+fmt.Sprint(testCase.from), func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			if testCase.action == "challenge" && testCase.read != "pending" {
				fixture.chain.mutate(func(state *settlementV2TestState) {
					state.pending[1] = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
				})
			}
			fixture.chain.failures[testCase.read] = testCase.from
			var err error
			switch testCase.action {
			case "approve":
				err = fixture.approve(t)
			case "demand":
				err = fixture.demand(t, 1)
			default:
				err = fixture.challenge(t, 2)
			}
			if err == nil || !strings.Contains(err.Error(), "synthetic "+testCase.read+" failure") || fixture.chain.count("eth_estimateGas") != 0 {
				t.Fatalf("a failed %s read: %v", testCase.read, err)
			}
		})
	}
}

// VA-3, VA-4, VA-8: a preflight revert other than front-running signs nothing
// and leaves no record: an approval by an address outside the auditor set, a
// demand naming a finalized root vault2 does not have (UnknownRoot), and a
// challenge whose opening shows no fault while nothing is demanded.
func TestSettlementV2PreflightRevertSendsNothing(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2SenderTestFixture(t)
	fixture.chain.mutate(func(state *settlementV2TestState) { state.auditors = map[common.Address]bool{} })
	if err := fixture.approve(t); err == nil || !strings.Contains(err.Error(), "preflight eth_call failed") {
		t.Fatalf("an unauthorized approval: %v", err)
	}
	if err := fixture.demandAt(t, 1, 2); err == nil || !strings.Contains(err.Error(), "preflight eth_call failed") || !settlementV2Reverted(err, evmSelector("UnknownRoot()")) {
		t.Fatalf("a demand against an unknown root: %v", err)
	}
	if err := fixture.challenge(t, 2); err == nil || !strings.Contains(err.Error(), "preflight eth_call failed") {
		t.Fatalf("a challenge without a fault: %v", err)
	}
	if fixture.chain.count("eth_sendRawTransaction") != 0 {
		t.Fatal("a reverting action was sent")
	}
	for _, path := range []string{fixture.recordPath("approve", 1), fixture.recordPathAt("demand", 1, 2), fixture.recordPath("challenge", 2)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a reverting action left a record: %v", err)
		}
	}
}

// VA-3: a record that cannot be read, or holds another action or another
// key's transaction, is refused rather than reconciled or replaced.
func TestSettlementV2SendRefusesForeignRecord(t *testing.T) {
	t.Parallel()
	other, err := crypto.HexToECDSA(strings.Repeat("3c", 32))
	if err != nil {
		t.Fatal(err)
	}
	signed := func(key *ecdsa.PrivateKey, to common.Address) string {
		transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{To: &to, Gas: 50_000, GasPrice: big.NewInt(1)}), types.LatestSignerForChainID(big.NewInt(settlementV2TestChainID)), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := transaction.MarshalBinary()
		return hexutil.Encode(raw)
	}
	fixture := newSettlementV2SenderTestFixture(t)
	record := settlementV2SendRecord{Schema: settlementV2SendSchema, NoID: 1, Seq: 7, Action: "demand", Index: 1, Transaction: signed(fixture.key, settlementV2TestVault)}
	for name, change := range map[string]func(*settlementV2SendRecord){
		"schema":     func(record *settlementV2SendRecord) { record.Schema = "other" },
		"no id":      func(record *settlementV2SendRecord) { record.NoID = 2 },
		"seq":        func(record *settlementV2SendRecord) { record.Seq = 8 },
		"action":     func(record *settlementV2SendRecord) { record.Action = "approve" },
		"root index": func(record *settlementV2SendRecord) { record.RootIndex = 1 },
		"index":      func(record *settlementV2SendRecord) { record.Index = 2 },
		"hex":        func(record *settlementV2SendRecord) { record.Transaction = "0xzz" },
		"rlp":        func(record *settlementV2SendRecord) { record.Transaction = "0x01" },
		"other key":  func(record *settlementV2SendRecord) { record.Transaction = signed(other, settlementV2TestVault) },
		"other target": func(record *settlementV2SendRecord) {
			record.Transaction = signed(fixture.key, settlementV2TestSettlement)
		},
	} {
		changed := record
		change(&changed)
		if err := writeSettlementV2SendRecord(fixture.recordPath("demand", 1), changed); err != nil {
			t.Fatal(err)
		}
		if err := fixture.demand(t, 1); err == nil || !strings.Contains(err.Error(), "is not this auditor's demand transaction") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := os.WriteFile(fixture.recordPath("demand", 1), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.demand(t, 1); err == nil || !strings.Contains(err.Error(), "is not this auditor's demand transaction") {
		t.Fatalf("an undecodable record: %v", err)
	}
	if err := os.Remove(fixture.recordPath("demand", 1)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fixture.recordPath("demand", 1), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fixture.demand(t, 1); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("an unreadable record: %v", err)
	}
	if len(fixture.chain.accepted()) != 0 {
		t.Fatal("a foreign record was replaced by a new send")
	}
}

// VA-3: a record that cannot be written before the broadcast stops the
// send; one that cannot be finalized after it reports the failure.
func TestSettlementV2SendRecordWriteFailures(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		at      string
		message string
		sent    int
	}{
		{at: "eth_gasPrice", message: "persist prepared transaction", sent: 0},
		{at: "eth_getTransactionReceipt", message: "file exists", sent: 1},
	} {
		t.Run(testCase.at, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			path := fixture.recordPath("demand", 1)
			fixture.chain.before = func(name string) {
				if name == testCase.at {
					_ = os.Remove(path)
					_ = os.MkdirAll(filepath.Join(path, "occupied"), 0o700)
				}
			}
			if err := fixture.demand(t, 1); err == nil || !strings.Contains(err.Error(), testCase.message) || len(fixture.chain.accepted()) != testCase.sent {
				t.Fatalf("a record write failure: %v", err)
			}
		})
	}
}

// VA-3: reconciliation of a recorded transaction: an unfinalized or non-canonical
// receipt stays pending, a failed read is an error, a transaction whose nonce
// another one consumed is dropped and the action sent afresh, and a failed
// rebroadcast stays pending with its cause.
func TestSettlementV2ReconcileRecordedTransaction(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		before  func(*settlementV2TestChain)
		after   func(*settlementV2SenderTestFixture)
		pending bool
		message string
		sent    int
	}{
		{name: "receipt above finality", before: func(chain *settlementV2TestChain) { chain.loseAck = true }, after: func(fixture *settlementV2SenderTestFixture) { fixture.chain.lag = 1 }, pending: true, sent: 1},
		{name: "receipt not canonical", before: func(chain *settlementV2TestChain) { chain.loseAck = true }, after: func(fixture *settlementV2SenderTestFixture) { fixture.chain.moveReceipt = true }, pending: true, sent: 1},
		{name: "receipt read", before: func(chain *settlementV2TestChain) { chain.loseAck = true }, after: func(fixture *settlementV2SenderTestFixture) {
			fixture.chain.failures["eth_getTransactionReceipt"] = 1
		}, message: "synthetic eth_getTransactionReceipt failure", sent: 1},
		{name: "receipt block read", before: func(chain *settlementV2TestChain) { chain.loseAck = true }, after: func(fixture *settlementV2SenderTestFixture) {
			fixture.chain.failures["eth_getBlockByNumber"] = 1
		}, message: "synthetic eth_getBlockByNumber failure", sent: 1},
		{name: "finalized head read", before: func(chain *settlementV2TestChain) { chain.loseAck = true }, after: func(fixture *settlementV2SenderTestFixture) {
			fixture.chain.failures["eth_getBlockByNumber:finalized"] = 1
		}, message: "synthetic eth_getBlockByNumber:finalized failure", sent: 1},
		{name: "nonce read", before: func(chain *settlementV2TestChain) { chain.reject = 1 }, after: func(fixture *settlementV2SenderTestFixture) {
			fixture.chain.failures["eth_getTransactionCount"] = 1
		}, message: "synthetic eth_getTransactionCount failure", sent: 0},
		{name: "rebroadcast refused", before: func(chain *settlementV2TestChain) { chain.reject = 1 }, after: func(fixture *settlementV2SenderTestFixture) { fixture.chain.reject = 1 }, pending: true, message: "synthetic send refused before acceptance", sent: 0},
		{name: "nonce consumed", before: func(chain *settlementV2TestChain) { chain.reject = 1 }, after: func(fixture *settlementV2SenderTestFixture) {
			fixture.chain.mutate(func(state *settlementV2TestState) { state.nonces[fixture.auditor()]++ })
		}, sent: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			fixture.chain.faults[2] = true
			testCase.before(fixture.chain)
			if err := fixture.challenge(t, 2); err == nil {
				t.Fatal("the first send reported success")
			}
			prepared := fixture.record(t, "challenge", 2)
			fixture.chain.lock.Lock()
			fixture.chain.loseAck = false
			fixture.chain.lock.Unlock()
			testCase.after(fixture)
			fixture.restart(t)
			err := fixture.challenge(t, 2)
			if errors.Is(err, ErrSettlementV2SendPending) != testCase.pending || testCase.message != "" && (err == nil || !strings.Contains(err.Error(), testCase.message)) || testCase.message == "" && !testCase.pending && err != nil {
				t.Fatalf("reconciliation: %v", err)
			}
			if sent := fixture.chain.accepted(); len(sent) != testCase.sent {
				t.Fatalf("%d transactions accepted, want %d", len(sent), testCase.sent)
			}
			if testCase.name == "nonce consumed" {
				if replaced := fixture.record(t, "challenge", 2); replaced.Transaction == prepared.Transaction || !replaced.Finalized {
					t.Fatal("the dropped transaction was not replaced by a final one")
				}
			}
		})
	}
}

// VA-3: a recorded transaction that can no longer land is dropped by
// removing its record, and a final one finalizes it; a record that cannot be removed or
// rewritten is an error.
func TestSettlementV2ReconcileRecordUpdateFailures(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		before  func(*settlementV2TestChain)
		message string
	}{
		{name: "reverted", before: func(chain *settlementV2TestChain) { chain.hold, chain.loseAck = true, true }, message: "permission denied"},
		{name: "consumed", before: func(chain *settlementV2TestChain) { chain.reject = 1 }, message: "permission denied"},
		{name: "finalized", before: func(chain *settlementV2TestChain) { chain.loseAck = true }, message: "file exists"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2SenderTestFixture(t)
			fixture.chain.faults[2] = true
			testCase.before(fixture.chain)
			if err := fixture.challenge(t, 2); err == nil {
				t.Fatal("the first send reported success")
			}
			path := fixture.recordPath("challenge", 2)
			switch testCase.name {
			case "reverted":
				fixture.chain.mutate(func(state *settlementV2TestState) {
					state.pending[1] = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
				})
				fixture.chain.mineHeld()
			case "consumed":
				fixture.chain.mutate(func(state *settlementV2TestState) { state.nonces[fixture.auditor()]++ })
			case "finalized":
				// The record is read before the receipt; its rewrite then meets a directory.
				fixture.chain.before = func(name string) {
					if name == "eth_getTransactionReceipt" {
						_ = os.Remove(path)
						_ = os.MkdirAll(filepath.Join(path, "occupied"), 0o700)
					}
				}
			}
			if testCase.name != "finalized" {
				// Removal needs a writable directory; a rewrite would restore it.
				if err := os.Chmod(filepath.Dir(path), 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0o700) })
			}
			fixture.chain.lock.Lock()
			fixture.chain.loseAck = false
			fixture.chain.lock.Unlock()
			if err := fixture.challenge(t, 2); err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("an unwritable record: %v", err)
			}
		})
	}
}

package commitmentoracle

// Deterministic in-memory chains for the oracle tests. fakeNative serves
// native storage per block hash and records every hash it was asked to read.
// fakeEvm models the coordinator's mirrorCommitment, mirroredCommitments,
// activeCommitmentOracle, paused, netuid and policy views with the contract's
// own rules, a mempool, explicit mining and explicit finality, and per-block
// nonces, so finality, restarts and nonces can be driven step by step.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/stabi"
)

type fakeNative struct {
	finalized     nativeHead
	registrations map[[32]byte]map[[32]byte][]byte
	lasts         map[[32]byte]map[[32]byte][]byte
	hashes        map[uint64][32]byte
	headers       map[[32]byte]uint64
	stateReads    [][32]byte
	headErr       error
	// Every native read by method name.
	reads map[string]int
}

func newFakeNative(finalized nativeHead) *fakeNative {
	return &fakeNative{
		finalized:     finalized,
		registrations: map[[32]byte]map[[32]byte][]byte{},
		lasts:         map[[32]byte]map[[32]byte][]byte{},
		hashes:        map[uint64][32]byte{finalized.number: finalized.hash},
		headers:       map[[32]byte]uint64{finalized.hash: finalized.number},
	}
}

// Records one read by kind. nativeReader is the oracle's only native access,
// so reads is every native read a cycle made.
func (self *fakeNative) read(kind string) {
	if self.reads == nil {
		self.reads = map[string]int{}
	}
	self.reads[kind] += 1
}

// Puts a registration at state blockHash, with a matching LastCommitment.
func (self *fakeNative) put(blockHash [32]byte, hotkey [32]byte, raw []byte, last []byte) {
	if self.registrations[blockHash] == nil {
		self.registrations[blockHash] = map[[32]byte][]byte{}
		self.lasts[blockHash] = map[[32]byte][]byte{}
	}
	self.registrations[blockHash][hotkey] = raw
	self.lasts[blockHash][hotkey] = last
}

func (self *fakeNative) block(number uint64, hash [32]byte) {
	self.hashes[number] = hash
	self.headers[hash] = number
}

func (self *fakeNative) finalizedHead(ctx context.Context) (nativeHead, error) {
	self.read("finalizedHead")
	return self.finalized, self.headErr
}

func (self *fakeNative) commitments(ctx context.Context, netuid uint16, blockHash [32]byte) ([]rawCommitment, error) {
	self.read("commitments")
	self.stateReads = append(self.stateReads, blockHash)
	entries := []rawCommitment{}
	for hotkey, raw := range self.registrations[blockHash] {
		entries = append(entries, rawCommitment{hotkey: hotkey, raw: raw})
	}
	slices.SortFunc(entries, func(a rawCommitment, b rawCommitment) int { return bytes.Compare(a.hotkey[:], b.hotkey[:]) })
	return entries, nil
}

func (self *fakeNative) commitment(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash [32]byte) ([]byte, error) {
	self.read("commitment")
	self.stateReads = append(self.stateReads, blockHash)
	return self.registrations[blockHash][hotkey], nil
}

func (self *fakeNative) lastCommitment(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash [32]byte) ([]byte, error) {
	self.read("lastCommitment")
	self.stateReads = append(self.stateReads, blockHash)
	return self.lasts[blockHash][hotkey], nil
}

func (self *fakeNative) blockHash(ctx context.Context, number uint64) ([32]byte, error) {
	self.read("blockHash")
	hash, ok := self.hashes[number]
	if !ok {
		return [32]byte{}, fmt.Errorf("no block %d", number)
	}
	return hash, nil
}

func (self *fakeNative) headerNumber(ctx context.Context, blockHash [32]byte) (uint64, error) {
	self.read("headerNumber")
	number, ok := self.headers[blockHash]
	if !ok {
		return 0, errors.New("no header")
	}
	return number, nil
}

func (self *fakeNative) close() {}

// A revert carrying custom error data, as an RPC returns it.
type fakeRevert struct {
	data string
}

func (self fakeRevert) Error() string  { return "execution reverted" }
func (self fakeRevert) ErrorData() any { return self.data }

func revertWith(selector [4]byte) error {
	return fakeRevert{data: hexutil.Encode(selector[:])}
}

type fakeRecordWrite struct {
	block  uint64
	record mirroredRecord
}

type fakeMined struct {
	transaction *types.Transaction
	from        common.Address
	block       uint64
	success     bool
}

type fakeEvm struct {
	t            *testing.T
	abi          *abi.ABI
	chain        uint64
	netuid       uint16
	coordinator  common.Address
	activeOracle common.Address
	paused       bool
	maxAge       uint64
	latest       uint64
	finalized    uint64
	gasPrice     *big.Int
	balances     map[common.Address]*big.Int
	writes       map[[32]byte][]fakeRecordWrite
	mined        []fakeMined
	mempool      []*types.Transaction
	broadcasts   []*types.Transaction
	// Drops broadcasts instead of pooling them, like a node that lost them.
	dropBroadcasts bool
}

func newFakeEvm(t *testing.T, coordinator common.Address, oracle common.Address, block uint64) *fakeEvm {
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	return &fakeEvm{
		t:            t,
		abi:          parsed,
		chain:        964,
		netuid:       25,
		coordinator:  coordinator,
		activeOracle: oracle,
		maxAge:       100800,
		latest:       block,
		finalized:    block,
		gasPrice:     big.NewInt(5 * 1000 * 1000 * 1000),
		balances:     map[common.Address]*big.Int{oracle: new(big.Int).Mul(big.NewInt(5), big.NewInt(10*1000*1000*1000*1000*1000))},
		writes:       map[[32]byte][]fakeRecordWrite{},
	}
}

func (self *fakeEvm) hashOf(number uint64) common.Hash {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], number)
	return crypto.Keccak256Hash([]byte("synthetic-evm-block"), encoded[:])
}

func (self *fakeEvm) blockOrLatest(block *big.Int) uint64 {
	if block == nil {
		return self.latest
	}
	return block.Uint64()
}

// The record visible at block.
func (self *fakeEvm) recordAt(hotkey [32]byte, block uint64) mirroredRecord {
	var record mirroredRecord
	for _, write := range self.writes[hotkey] {
		if write.block <= block {
			record = write.record
		}
	}
	return record
}

// Writes a record directly, as a mirror by another sender would.
func (self *fakeEvm) setRecord(hotkey [32]byte, record mirroredRecord, block uint64) {
	self.writes[hotkey] = append(self.writes[hotkey], fakeRecordWrite{block: block, record: record})
}

// STCoordinator.mirrorCommitment against the latest state.
func (self *fakeEvm) mirror(from common.Address, args mirrorArgs, block uint64) error {
	if self.paused {
		return revertWith(selectorPaused)
	}
	if from != self.activeOracle {
		return revertWith(selectorUnauthorized)
	}
	if args.hotkey == ([32]byte{}) || args.commitmentHash == ([32]byte{}) || args.finalizedBlockHash == ([32]byte{}) || args.finalizedBlock > block {
		return revertWith(selectorInvalidConfiguration)
	}
	prior := self.recordAt(args.hotkey, self.latest)
	if prior.finalizedBlock != 0 {
		if args.finalizedBlock < prior.finalizedBlock {
			return revertWith(selectorStaleCommitment)
		}
		if args.finalizedBlock == prior.finalizedBlock {
			if prior.commitmentHash != args.commitmentHash || prior.finalizedBlockHash != args.finalizedBlockHash {
				return revertWith(selectorStaleCommitment)
			}
			return nil
		}
	}
	self.setRecord(args.hotkey, mirroredRecord{commitmentHash: args.commitmentHash, finalizedBlockHash: args.finalizedBlockHash, finalizedBlock: args.finalizedBlock}, block)
	return nil
}

func (self *fakeEvm) decodeMirror(data []byte) mirrorArgs {
	method, err := self.abi.MethodById(data[:4])
	if err != nil || method.Name != "mirrorCommitment" {
		self.t.Fatalf("not a mirrorCommitment call: %x", data)
	}
	values, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		self.t.Fatal(err)
	}
	return mirrorArgs{hotkey: values[0].([32]byte), commitmentHash: values[1].([32]byte), finalizedBlock: values[2].(uint64), finalizedBlockHash: values[3].([32]byte)}
}

// Includes the mempool in the next block.
func (self *fakeEvm) mine() {
	self.latest += 1
	signer := types.LatestSignerForChainID(new(big.Int).SetUint64(self.chain))
	for _, transaction := range self.mempool {
		from, err := types.Sender(signer, transaction)
		if err != nil {
			self.t.Fatal(err)
		}
		if transaction.Nonce() != self.nonce(from, self.latest) {
			continue
		}
		success := self.mirror(from, self.decodeMirror(transaction.Data()), self.latest) == nil
		self.mined = append(self.mined, fakeMined{transaction: transaction, from: from, block: self.latest, success: success})
		fee := new(big.Int).Mul(transaction.GasPrice(), big.NewInt(90000))
		self.balances[from] = new(big.Int).Sub(self.balances[from], fee)
	}
	self.mempool = nil
}

func (self *fakeEvm) finalize() {
	self.finalized = self.latest
}

func (self *fakeEvm) nonce(account common.Address, block uint64) uint64 {
	nonce := uint64(0)
	for _, mined := range self.mined {
		if mined.from == account && mined.block <= block {
			nonce += 1
		}
	}
	return nonce
}

func (self *fakeEvm) chainId(ctx context.Context) (*big.Int, error) {
	return new(big.Int).SetUint64(self.chain), nil
}

func (self *fakeEvm) finalizedBlock(ctx context.Context) (uint64, common.Hash, error) {
	return self.finalized, self.hashOf(self.finalized), nil
}

func (self *fakeEvm) latestBlock(ctx context.Context) (uint64, error) {
	return self.latest, nil
}

func (self *fakeEvm) blockHash(ctx context.Context, number uint64) (common.Hash, error) {
	if number > self.latest {
		return common.Hash{}, errors.New("future block")
	}
	return self.hashOf(number), nil
}

func (self *fakeEvm) callContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	if msg.To == nil || *msg.To != self.coordinator {
		return nil, errors.New("call to another contract")
	}
	at := self.blockOrLatest(block)
	method, err := self.abi.MethodById(msg.Data[:4])
	if err != nil {
		return nil, err
	}
	inputs, err := method.Inputs.Unpack(msg.Data[4:])
	if err != nil {
		return nil, err
	}
	switch method.Name {
	case "netuid":
		return method.Outputs.Pack(self.netuid)
	case "activeCommitmentOracle":
		return method.Outputs.Pack(self.activeOracle)
	case "paused":
		return method.Outputs.Pack(self.paused)
	case "currentEpoch":
		return method.Outputs.Pack(big.NewInt(1))
	case "policyAt":
		return method.Outputs.Pack(stabi.STCoordinatorPolicySnapshot{EffectiveEpoch: 1, EpochBlocks: 50400, MaximumBindingValidityEpochs: 12, CommitmentMaxAgeBlocks: self.maxAge, EpochDepositCapRao: new(big.Int), CampaignDepositCapRao: new(big.Int)})
	case "mirroredCommitments":
		record := self.recordAt(inputs[0].([32]byte), at)
		return method.Outputs.Pack(record.commitmentHash, record.finalizedBlockHash, record.finalizedBlock)
	case "mirrorCommitment":
		// A preflight: the contract's checks without a state change.
		writes := self.writes
		self.writes = map[[32]byte][]fakeRecordWrite{}
		for hotkey, list := range writes {
			self.writes[hotkey] = slices.Clone(list)
		}
		err := self.mirror(msg.From, self.decodeMirror(msg.Data), at+1)
		self.writes = writes
		return nil, err
	}
	return nil, fmt.Errorf("unexpected call %s", method.Name)
}

func (self *fakeEvm) balanceAt(ctx context.Context, account common.Address, block *big.Int) (*big.Int, error) {
	if balance, ok := self.balances[account]; ok {
		return new(big.Int).Set(balance), nil
	}
	return new(big.Int), nil
}

func (self *fakeEvm) nonceAt(ctx context.Context, account common.Address, block *big.Int) (uint64, error) {
	return self.nonce(account, self.blockOrLatest(block)), nil
}

func (self *fakeEvm) pendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	nonce := self.nonce(account, self.latest)
	signer := types.LatestSignerForChainID(new(big.Int).SetUint64(self.chain))
	for _, transaction := range self.mempool {
		if from, _ := types.Sender(signer, transaction); from == account {
			nonce += 1
		}
	}
	return nonce, nil
}

func (self *fakeEvm) suggestGasPrice(ctx context.Context) (*big.Int, error) {
	return new(big.Int).Set(self.gasPrice), nil
}

func (self *fakeEvm) estimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	if _, err := self.callContract(ctx, msg, nil); err != nil {
		return 0, err
	}
	return 90000, nil
}

func (self *fakeEvm) sendTransaction(ctx context.Context, transaction *types.Transaction) error {
	self.broadcasts = append(self.broadcasts, transaction)
	if self.dropBroadcasts {
		return nil
	}
	for _, pooled := range self.mempool {
		if pooled.Hash() == transaction.Hash() {
			return errors.New("already known")
		}
	}
	for _, mined := range self.mined {
		if mined.transaction.Hash() == transaction.Hash() {
			return errors.New("already known")
		}
	}
	self.mempool = append(self.mempool, transaction)
	return nil
}

func (self *fakeEvm) transactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	for _, mined := range self.mined {
		if mined.transaction.Hash() == hash {
			status := types.ReceiptStatusFailed
			if mined.success {
				status = types.ReceiptStatusSuccessful
			}
			return &types.Receipt{Status: status, TxHash: hash, BlockNumber: new(big.Int).SetUint64(mined.block), BlockHash: self.hashOf(mined.block)}, nil
		}
	}
	return nil, ethereum.NotFound
}

func (self *fakeEvm) close() {}

// The distinct signed transactions ever broadcast.
func (self *fakeEvm) distinctBroadcasts() []common.Hash {
	hashes := []common.Hash{}
	for _, transaction := range self.broadcasts {
		if !slices.Contains(hashes, transaction.Hash()) {
			hashes = append(hashes, transaction.Hash())
		}
	}
	return hashes
}

// A synthetic test-only oracle key; never a production key.
func testOracleKey(t *testing.T) *ecdsa.PrivateKey {
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte("synthetic commitment oracle test key")))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

type testClock struct {
	now time.Time
}

func (self *testClock) get() time.Time {
	return self.now
}

type testLog struct {
	lines []string
}

func (self *testLog) logf(format string, args ...any) {
	self.lines = append(self.lines, fmt.Sprintf(format, args...))
}

func (self *testLog) contains(text string) bool {
	for _, line := range self.lines {
		if strings.Contains(line, text) {
			return true
		}
	}
	return false
}

// The oracle under test, wired to the fakes.
type testHarness struct {
	native *fakeNative
	evm    *fakeEvm
	clock  *testClock
	log    *testLog
	oracle *oracle
	dir    string
}

func testSettings(stateDir string) Settings {
	return Settings{
		Netuid:           25,
		ChainId:          964,
		Coordinator:      common.HexToAddress("0x00000000000000000000000000000000c0047d17"),
		StateDir:         stateDir,
		PollInterval:     30 * time.Second,
		MaxBackoff:       10 * time.Minute,
		ConfirmTimeout:   time.Minute,
		ConfirmPoll:      6 * time.Second,
		MaxGasPrice:      big.NewInt(20 * 1000 * 1000 * 1000),
		MaxGasLimit:      250000,
		LowBalanceWei:    big.NewInt(10 * 1000 * 1000 * 1000 * 1000 * 1000),
		MaxSendsPerCycle: 10,
		Once:             true,
	}
}

// Builds an oracle over native and evm. A nil key is an address-only dry run.
func newTestHarness(t *testing.T, native *fakeNative, evm *fakeEvm, settings Settings, key *ecdsa.PrivateKey, address common.Address, clock *testClock, log *testLog) *testHarness {
	send := &sender{
		chainId:     new(big.Int).SetUint64(settings.ChainId),
		coordinator: settings.Coordinator,
		oracle:      address,
		key:         key,
		dryRun:      settings.DryRun,
		fees:        feeSettings{maxGasPrice: settings.MaxGasPrice, maxGasLimit: settings.MaxGasLimit},
		directory:   settings.StateDir,
		now:         clock.get,
		logf:        log.logf,
	}
	oracle := newOracle(settings, address, connections{primary: native, evm: evm}, send, clock.get, log.logf)
	// Each confirmation poll advances the clock; tests choose whether the
	// chain moves.
	oracle.sleep = func(ctx context.Context, duration time.Duration) error {
		clock.now = clock.now.Add(duration)
		return nil
	}
	return &testHarness{native: native, evm: evm, clock: clock, log: log, oracle: oracle, dir: settings.StateDir}
}

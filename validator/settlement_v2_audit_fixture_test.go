//go:build linux || darwin

// The settlement v2 audit runs against the sender tests' in-process chain,
// extended with what the audit reads: vault2's pools and fundedThrough, the
// coordinator's epoch starts and archive bindingAt at any boundary block,
// every block header up to the head, and eth_getLogs. Ledger objects come
// from an in-process operator endpoint, and the auditor transactions go
// through the real sender.
package validator

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

const settlementV2AuditTestNoID = 1

// A boundary's archive bindingAt answer: client, epoch and block.
type settlementV2AuditTestBinding struct {
	client       [16]byte
	epoch, block uint64
}

// settlementV2AuditTestChain answers the audit's reads beside the sender
// tests' chain. Epoch e spans blocks [100e, 100(e+1)).
type settlementV2AuditTestChain struct {
	*settlementV2TestChain
	pools  map[uint64]stabi.ISTSettlementVault2Pool
	funded map[uint64]map[uint64]*big.Int
	// active holds bindingAt's answers; absent is inactive.
	active map[settlementV2AuditTestBinding]bool
	logs   []types.Log
	// served, when set, replaces the logs one eth_getLogs returns.
	served func([]types.Log) []types.Log
	// entitlements holds the v1 vault's entitlement(epoch, 1) answers.
	entitlements map[uint64]stabi.STSettlementVaultEntitlement
}

// settlementV2AuditTestV1Vault is the v1 vault the recovery reads.
var settlementV2AuditTestV1Vault = common.HexToAddress("0x7a10000000000000000000000000000000000001")

func newSettlementV2AuditTestChain(t *testing.T, auditor common.Address) *settlementV2AuditTestChain {
	t.Helper()
	chain := &settlementV2AuditTestChain{settlementV2TestChain: newSettlementV2TestChain(t, auditor), pools: map[uint64]stabi.ISTSettlementVault2Pool{},
		funded: map[uint64]map[uint64]*big.Int{}, active: map[settlementV2AuditTestBinding]bool{}, entitlements: map[uint64]stabi.STSettlementVaultEntitlement{}}
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", chain); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() { httpServer.Close(); server.Stop() })
	chain.url = httpServer.URL
	return chain
}

// The block a test hash names: settlementV2TestBlockHash's inverse.
func settlementV2AuditTestBlockOf(hash common.Hash) (uint64, bool) {
	number := new(big.Int).Sub(hash.Big(), big.NewInt(0xb10c_0000_0000))
	return number.Uint64(), number.Sign() >= 0 && number.IsUint64()
}

func (self *settlementV2AuditTestChain) Call(args settlementV2TestCallArgs, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if args.To == settlementV2AuditTestV1Vault && len(args.Input) >= 4 {
		contractABI, err := stabi.STSettlementVaultMetaData.ParseABI()
		if err != nil {
			return nil, err
		}
		method, err := contractABI.MethodById(args.Input)
		if err != nil || method.Name != "entitlement" {
			return nil, errors.New("the test v1 vault answers only entitlement")
		}
		if err := self.hit("entitlement"); err != nil {
			return nil, err
		}
		arguments, err := method.Inputs.Unpack(args.Input[4:])
		if err != nil {
			return nil, err
		}
		self.lock.Lock()
		defer self.lock.Unlock()
		entitlement := self.entitlements[arguments[0].(*big.Int).Uint64()]
		if arguments[1].(*big.Int).Uint64() != settlementV2AuditTestNoID {
			entitlement = stabi.STSettlementVaultEntitlement{}
		}
		for _, value := range []**big.Int{&entitlement.Funded, &entitlement.Total, &entitlement.Claimed} {
			if *value == nil {
				*value = new(big.Int)
			}
		}
		return method.Outputs.Pack(entitlement)
	}
	if contractABI := settlementV2TestABI(args.To); contractABI != nil && len(args.Input) >= 4 {
		if method, err := contractABI.MethodById(args.Input); err == nil && slices.Contains([]string{"pools", "fundedThrough", "epochStartBlock", "bindingAt"}, method.Name) {
			if err := self.hit(method.Name); err != nil {
				return nil, err
			}
			arguments, err := method.Inputs.Unpack(args.Input[4:])
			if err != nil {
				return nil, err
			}
			self.lock.Lock()
			defer self.lock.Unlock()
			switch method.Name {
			case "pools":
				return method.Outputs.Pack(self.pools[arguments[0].(*big.Int).Uint64()])
			case "fundedThrough":
				funded := self.funded[arguments[0].(*big.Int).Uint64()][arguments[1].(*big.Int).Uint64()]
				if funded == nil {
					funded = new(big.Int)
				}
				return method.Outputs.Pack(funded)
			case "epochStartBlock":
				return method.Outputs.Pack(new(big.Int).Mul(arguments[0].(*big.Int), big.NewInt(100)))
			}
			hash, _ := selector.Hash()
			block, _ := settlementV2AuditTestBlockOf(hash)
			key := settlementV2AuditTestBinding{client: arguments[0].([16]byte), epoch: arguments[1].(*big.Int).Uint64(), block: block}
			return method.Outputs.Pack(self.active[key], stabi.STCoordinatorBindingRecord{})
		}
	}
	return self.settlementV2TestChain.Call(args, selector)
}

// Every block up to the head has a header, so archive reads can pin any.
func (self *settlementV2AuditTestChain) GetBlockByNumber(number gethrpc.BlockNumber, full bool) (map[string]any, error) {
	if number < 0 {
		return self.settlementV2TestChain.GetBlockByNumber(number, full)
	}
	if err := self.hit("eth_getBlockByNumber"); err != nil {
		return nil, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	if uint64(number) > self.latest {
		return nil, nil
	}
	return map[string]any{"number": hexutil.Uint64(number), "hash": settlementV2TestBlockHash(uint64(number))}, nil
}

func (self *settlementV2AuditTestChain) GetBlockByHash(hash common.Hash, full bool) (map[string]any, error) {
	if err := self.hit("eth_getBlockByHash"); err != nil {
		return nil, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	block, ok := settlementV2AuditTestBlockOf(hash)
	if !ok || block > self.latest {
		return nil, errors.New("unknown block hash")
	}
	return map[string]any{"number": hexutil.Uint64(block), "hash": hash}, nil
}

// The eth_getLogs filter ethclient sends.
type settlementV2AuditTestLogQuery struct {
	FromBlock *hexutil.Big     `json:"fromBlock"`
	ToBlock   *hexutil.Big     `json:"toBlock"`
	Address   []common.Address `json:"address"`
	Topics    [][]common.Hash  `json:"topics"`
}

func (self *settlementV2AuditTestChain) GetLogs(query settlementV2AuditTestLogQuery) ([]types.Log, error) {
	if err := self.hit("eth_getLogs"); err != nil {
		return nil, err
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	from, to := query.FromBlock.ToInt().Uint64(), query.ToBlock.ToInt().Uint64()
	out := []types.Log{}
	for _, log := range self.logs {
		matches := log.BlockNumber >= from && log.BlockNumber <= to && slices.Contains(query.Address, log.Address)
		for i, alternatives := range query.Topics {
			matches = matches && (len(alternatives) == 0 || i < len(log.Topics) && slices.Contains(alternatives, log.Topics[i]))
		}
		if matches {
			out = append(out, log)
		}
	}
	if self.served != nil {
		out = self.served(out)
	}
	return out, nil
}

// addLog appends an event of contract, its arguments in ABI order, at block.
func (self *settlementV2AuditTestChain) addLog(t *testing.T, contract *abi.ABI, address common.Address, name string, block uint64, arguments ...any) {
	t.Helper()
	event := contract.Events[name]
	var indexed [][]any
	var data []any
	for i, input := range event.Inputs {
		if input.Indexed {
			indexed = append(indexed, []any{arguments[i]})
		} else {
			data = append(data, arguments[i])
		}
	}
	topics, err := abi.MakeTopics(indexed...)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		t.Fatal(err)
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	log := types.Log{Address: address, Topics: []common.Hash{event.ID}, Data: encoded, BlockNumber: block, BlockHash: settlementV2TestBlockHash(block), Index: uint(len(self.logs))}
	for _, topic := range topics {
		log.Topics = append(log.Topics, topic[0])
	}
	binary.BigEndian.PutUint64(log.TxHash[24:], uint64(len(self.logs)+1))
	self.logs = append(self.logs, log)
	slices.SortStableFunc(self.logs, func(a, b types.Log) int { return int(a.BlockNumber) - int(b.BlockNumber) })
}

// fleetBound adds a FleetBound event for client with validity [from, to].
func (self *settlementV2AuditTestChain) fleetBound(t *testing.T, block uint64, client [16]byte, from, to uint64) {
	t.Helper()
	self.addLog(t, settlementV2CoordinatorABI, settlementV2TestCoordinator, "FleetBound", block, client, [32]byte{0xf1}, [32]byte{0xf2}, uint16(9), uint64(1), from, to)
}

// setActive sets bindingAt(client, epoch)'s answer at block.
func (self *settlementV2AuditTestChain) setActive(client [16]byte, epoch, block uint64, active bool) {
	self.lock.Lock()
	defer self.lock.Unlock()
	self.active[settlementV2AuditTestBinding{client: client, epoch: epoch, block: block}] = active
}

// settlementV2AuditTestObjects is an operator's GET /sn/ledger/object.
type settlementV2AuditTestObjects struct {
	lock     sync.Mutex
	url      string
	objects  ledger.Objects
	requests map[ledger.Ref]int
}

func newSettlementV2AuditTestObjects(t *testing.T) *settlementV2AuditTestObjects {
	t.Helper()
	self := &settlementV2AuditTestObjects{objects: ledger.Objects{}, requests: map[ledger.Ref]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ref, err := ledger.ParseRef(r.URL.Query().Get("hash"))
		if r.URL.Path != "/sn/ledger/object" || err != nil {
			http.Error(w, "malformed", http.StatusBadRequest)
			return
		}
		self.lock.Lock()
		data, ok := self.objects[ref]
		self.requests[ref]++
		self.lock.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	}))
	t.Cleanup(server.Close)
	self.url = server.URL
	return self
}

func (self *settlementV2AuditTestObjects) publish(objects ledger.Objects) {
	self.lock.Lock()
	defer self.lock.Unlock()
	for ref, data := range objects {
		self.objects[ref] = data
	}
}

func (self *settlementV2AuditTestObjects) withdraw(refs ...ledger.Ref) {
	self.lock.Lock()
	defer self.lock.Unlock()
	for _, ref := range refs {
		delete(self.objects, ref)
	}
}

// fetched counts the requests for ref, or for every object without one.
func (self *settlementV2AuditTestObjects) fetched(refs ...ledger.Ref) int {
	self.lock.Lock()
	defer self.lock.Unlock()
	n := 0
	for ref, count := range self.requests {
		if len(refs) == 0 || slices.Contains(refs, ref) {
			n += count
		}
	}
	return n
}

// Client n's id and coldkey n; both order as n. Coldkey 0 is no wallet.
func settlementV2AuditTestClient(n int) [16]byte {
	var b [16]byte
	binary.BigEndian.PutUint64(b[8:], uint64(n))
	return b
}

func settlementV2AuditTestColdkey(n int) [32]byte {
	var b [32]byte
	if n != 0 {
		binary.BigEndian.PutUint64(b[:8], uint64(n))
		b[31] = 0x5a
	}
	return b
}

func settlementV2AuditTestRow(client, cold int, usage uint64, head bool) protocol.LedgerUsageRow {
	return protocol.LedgerUsageRow{ClientID: settlementV2AuditTestClient(client), Coldkey: settlementV2AuditTestColdkey(cold), UsageBytes: usage, HeadExcluded: head}
}

// The standard scenario. Pool 1's first epoch is 3. Its finalized root F
// covers epoch 3 (seq 1, committed at block 405, finalized at 460 as root
// 0); the pending root P covers epochs 4 and 5 (seq 2, committed at 605).
// Clients 5 and 7 are head-excluded in epoch 4 (5 bound at both boundaries,
// 7 at the end only); client 6 is a candidate without an active binding and
// client 8 a fleet member whose binding does not cover epoch 4. Client 4 and
// 11 have no wallet. F has 4 accounts; P adds coldkeys 4, 6, 9 and 10.
func settlementV2AuditTestRows3() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{
		settlementV2AuditTestRow(1, 1, 300, false), settlementV2AuditTestRow(2, 1, 100, false), settlementV2AuditTestRow(3, 2, 200, false),
		settlementV2AuditTestRow(4, 0, 500, false), settlementV2AuditTestRow(9, 7, 400, false), settlementV2AuditTestRow(10, 8, 1, false),
	}
}

func settlementV2AuditTestRows4() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{
		settlementV2AuditTestRow(1, 1, 250, false), settlementV2AuditTestRow(3, 2, 250, false), settlementV2AuditTestRow(5, 3, 400, true),
		settlementV2AuditTestRow(6, 4, 50, false), settlementV2AuditTestRow(7, 5, 70, true), settlementV2AuditTestRow(8, 6, 300, false),
		settlementV2AuditTestRow(11, 0, 50, false),
	}
}

func settlementV2AuditTestRows5() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{
		settlementV2AuditTestRow(3, 2, 500, false), settlementV2AuditTestRow(12, 9, 500, false), settlementV2AuditTestRow(13, 6, 10, false),
		settlementV2AuditTestRow(14, 10, 7, false),
	}
}

func settlementV2AuditTestFunding() map[uint64]uint256.Int {
	return map[uint64]uint256.Int{3: *uint256.NewInt(1_000_003), 4: *uint256.NewInt(1_600_000), 5: *uint256.NewInt(2_500_000)}
}

// settlementV2AuditTestBuild builds pool 1's root for the last covered epoch.
func settlementV2AuditTestBuild(t *testing.T, previous *ledger.Ledger, epochs []ledger.EpochInput, funded map[uint64]uint256.Int) *ledger.Ledger {
	t.Helper()
	in := ledger.BuildInput{NoID: settlementV2AuditTestNoID, Epoch: epochs[len(epochs)-1].Epoch, FirstEpoch: 3, Epochs: epochs, FundedThrough: funded}
	if previous != nil {
		in.Previous = &ledger.Previous{Root: previous.Commit, Accounts: previous.Accounts}
	}
	built, err := ledger.Build(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func settlementV2AuditTestF(t *testing.T) *ledger.Ledger {
	return settlementV2AuditTestBuild(t, nil, []ledger.EpochInput{{Epoch: 3, Rows: settlementV2AuditTestRows3()}}, settlementV2AuditTestFunding())
}

func settlementV2AuditTestP(t *testing.T, f *ledger.Ledger) *ledger.Ledger {
	return settlementV2AuditTestBuild(t, f, []ledger.EpochInput{{Epoch: 4, Rows: settlementV2AuditTestRows4()}, {Epoch: 5, Rows: settlementV2AuditTestRows5()}}, settlementV2AuditTestFunding())
}

// settlementV2AuditTestPublication is a pending root's published objects,
// open to mutation.
type settlementV2AuditTestPublication struct {
	header  *ledger.Header
	objects ledger.Objects
	commit  ledger.Root
}

func settlementV2AuditTestPublish(t *testing.T, built *ledger.Ledger) *settlementV2AuditTestPublication {
	t.Helper()
	header, err := ledger.DecodeHeader(built.Header)
	if err != nil {
		t.Fatal(err)
	}
	objects := ledger.Objects{}
	for ref, data := range built.Objects {
		objects[ref] = data
	}
	return &settlementV2AuditTestPublication{header: header, objects: objects, commit: built.Commit}
}

func (self *settlementV2AuditTestPublication) put(chunks [][]byte) []ledger.Ref {
	refs := make([]ledger.Ref, len(chunks))
	for j, chunk := range chunks {
		refs[j] = ledger.RefOf(chunk)
		self.objects[refs[j]] = chunk
	}
	return refs
}

// setChunks publishes covered epoch i's provider chunks, and setRows its rows.
func (self *settlementV2AuditTestPublication) setChunks(i int, chunks [][]byte) {
	self.header.Epochs[i].Providers = self.put(chunks)
}

func (self *settlementV2AuditTestPublication) setRows(i int, rows []protocol.LedgerUsageRow) {
	self.setChunks(i, ledger.EncodeProviderChunks(rows))
}

// setAccounts publishes accounts and commits their tree, as an operator
// committing a mutated ledger would.
func (self *settlementV2AuditTestPublication) setAccounts(t *testing.T, accounts []merkle.LedgerAccount) {
	t.Helper()
	chunks, err := ledger.EncodeAccountChunks(accounts)
	if err != nil {
		t.Fatal(err)
	}
	self.header.Accounts = self.put(chunks)
	tree, err := merkle.BuildLedgerSumTree(t.Context(), accounts)
	if err != nil {
		t.Fatal(err)
	}
	self.commit.Root, self.commit.Total = tree.Root()
	self.commit.LeafCount = tree.LeafCount()
}

// seal publishes the header and commits its hash.
func (self *settlementV2AuditTestPublication) seal(t *testing.T) ledger.Root {
	t.Helper()
	data, err := ledger.EncodeHeader(self.header)
	if err != nil {
		t.Fatal(err)
	}
	return self.sealBytes(data)
}

func (self *settlementV2AuditTestPublication) sealBytes(data []byte) ledger.Root {
	ref := ledger.RefOf(data)
	self.objects[ref] = data
	self.commit.LedgerHash = ref
	return self.commit
}

// settlementV2AuditTestRandom is the audit's fixed randomness; a second
// stream from the same seed replays it.
func settlementV2AuditTestRandom() *rand.ChaCha8 {
	return rand.NewChaCha8([32]byte{0x5a, 0x17})
}

// settlementV2AuditTestFixture is one validator over the test chain and an
// operator's object endpoint. The chain, the endpoint, the auditor key and
// the state directory survive a restart.
type settlementV2AuditTestFixture struct {
	chain    *settlementV2AuditTestChain
	objects  *settlementV2AuditTestObjects
	key      *ecdsa.PrivateKey
	keyFile  string
	stateDir string
	client   *ChainClient
	audit    *settlementV2Audit
}

func newSettlementV2AuditTestFixture(t *testing.T) *settlementV2AuditTestFixture {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("2b", 32))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &settlementV2AuditTestFixture{objects: newSettlementV2AuditTestObjects(t), key: key, keyFile: filepath.Join(t.TempDir(), "auditor.key"), stateDir: t.TempDir()}
	if err := os.WriteFile(fixture.keyFile, []byte("0x"+strings.Repeat("2b", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.chain = newSettlementV2AuditTestChain(t, crypto.PubkeyToAddress(key.PublicKey))
	fixture.chain.pools[settlementV2AuditTestNoID] = stabi.ISTSettlementVault2Pool{Hotkey: [32]byte{0x9a}, FirstEpoch: 3, Registered: true}
	fixture.chain.funded[settlementV2AuditTestNoID] = map[uint64]*big.Int{}
	for epoch, funded := range settlementV2AuditTestFunding() {
		fixture.chain.funded[settlementV2AuditTestNoID][epoch] = funded.ToBig()
	}
	// Fleet bindings of the standard scenario's epoch 4.
	fixture.chain.fleetBound(t, 150, settlementV2AuditTestClient(5), 4, 6)
	fixture.chain.fleetBound(t, 160, settlementV2AuditTestClient(6), 4, 4)
	fixture.chain.fleetBound(t, 170, settlementV2AuditTestClient(7), 4, 5)
	fixture.chain.fleetBound(t, 180, settlementV2AuditTestClient(8), 1, 2)
	fixture.chain.setActive(settlementV2AuditTestClient(5), 4, 400, true)
	fixture.chain.setActive(settlementV2AuditTestClient(5), 4, 500, true)
	fixture.chain.setActive(settlementV2AuditTestClient(7), 4, 500, true)
	// A probe: client 8's binding does not cover epoch 4, so the audit never
	// reads it, and its row stays unexcluded.
	fixture.chain.setActive(settlementV2AuditTestClient(8), 4, 400, true)
	fixture.restart(t)
	return fixture
}

// restart is a new validator process over the same state directory.
func (self *settlementV2AuditTestFixture) restart(t *testing.T) {
	t.Helper()
	client, err := ethclient.Dial(self.chain.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	self.client = &ChainClient{client: client, rpcUrl: self.chain.url, coordinator: stabi.NewSTCoordinator(), contractAddr: settlementV2TestCoordinator, chainId: big.NewInt(settlementV2TestChainID), release: true}
	addresses := SettlementV2Addresses{Settlement: settlementV2TestSettlement, Vault: settlementV2TestVault}
	sender, err := NewSettlementV2Sender(self.client, []string{self.chain.url}, settlementV2TestChainID, addresses, self.keyFile, self.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	custody, err := openSettlementV2Custody(self.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := newSettlementV2Objects(self.objects.url)
	if err != nil {
		t.Fatal(err)
	}
	reader := &settlementV2AuditChain{chain: self.client, vault: settlementV2TestVault, deployBlock: 1}
	self.audit = newSettlementV2Audit(reader, custody, sender, []settlementV2AuditPool{{noID: settlementV2AuditTestNoID, objects: objects}}, DefaultLedgerMaxBytes)
	self.audit.random = settlementV2AuditTestRandom()
}

func (self *settlementV2AuditTestFixture) auditor() common.Address {
	return crypto.PubkeyToAddress(self.key.PublicKey)
}

// pass runs one audit step on pool 1.
func (self *settlementV2AuditTestFixture) pass(t *testing.T) error {
	t.Helper()
	return self.audit.audit(t.Context(), self.audit.pools[0])
}

func (self *settlementV2AuditTestFixture) mustPass(t *testing.T) {
	t.Helper()
	if err := self.pass(t); err != nil {
		t.Fatal(err)
	}
}

func settlementV2AuditTestPending(seq uint64, root ledger.Root, committed uint64) stabi.ISTSettlementVault2Pending {
	return stabi.ISTSettlementVault2Pending{Seq: seq, Root: root.Root, Total: root.Total.ToBig(), LeafCount: root.LeafCount, Epoch: root.Epoch, LedgerHash: root.LedgerHash, CommittedBlock: committed}
}

func settlementV2AuditTestFinalized(root ledger.Root) stabi.ISTSettlementVault2Root {
	return stabi.ISTSettlementVault2Root{Root: root.Root, Total: root.Total.ToBig(), LeafCount: root.LeafCount, Epoch: root.Epoch, LedgerHash: root.LedgerHash}
}

// setPool makes the chain's head a new block whose pool 1 has pending (seq 0
// for none) over the finalized roots.
func (self *settlementV2AuditTestFixture) setPool(pending stabi.ISTSettlementVault2Pending, finalized ...ledger.Root) {
	self.chain.mutate(func(state *settlementV2TestState) {
		state.pending[settlementV2AuditTestNoID] = pending
		state.roots[settlementV2AuditTestNoID] = nil
		for _, root := range finalized {
			state.roots[settlementV2AuditTestNoID] = append(state.roots[settlementV2AuditTestNoID], settlementV2AuditTestFinalized(root))
		}
	})
}

// useScenario puts the standard scenario on chain at head: F finalized at
// block 460 as root 0 and pending as seq 2, committed at 605, with pending's
// objects published. heldF records that this validator audited F.
func (self *settlementV2AuditTestFixture) useScenario(t *testing.T, f *ledger.Ledger, pending ledger.Root, objects ledger.Objects, heldF bool, head uint64) {
	t.Helper()
	self.committed(t, 605, 2, pending, 0)
	self.useScenarioAt(t, f, pending, objects, heldF, 2, 605, head)
}

// useScenarioAt has pending committed as seq at block committed instead; its
// caller adds the commit's events.
func (self *settlementV2AuditTestFixture) useScenarioAt(t *testing.T, f *ledger.Ledger, pending ledger.Root, objects ledger.Objects, heldF bool, seq, committed, head uint64) {
	t.Helper()
	self.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, "DistributionFinalized", 460, big.NewInt(settlementV2AuditTestNoID), uint64(1), big.NewInt(0), f.Commit.Epoch, f.Commit.Root, f.Commit.Total.ToBig(), f.Commit.LeafCount, [32]byte(f.Commit.LedgerHash))
	self.objects.publish(objects)
	if heldF {
		self.holdF(t, f)
	} else {
		self.objects.publish(f.Objects)
	}
	self.chain.advanceTo(head - 1)
	self.setPool(settlementV2AuditTestPending(seq, pending, committed), f.Commit)
}

// committed adds a DistributionCommitted of root as seq at block.
func (self *settlementV2AuditTestFixture) committed(t *testing.T, block, seq uint64, root ledger.Root, replacedSeq uint64) {
	t.Helper()
	self.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, "DistributionCommitted", block, big.NewInt(settlementV2AuditTestNoID), seq, root.Epoch, root.Root, root.Total.ToBig(), root.LeafCount, [32]byte(root.LedgerHash), replacedSeq)
}

// useFunding replaces pool 1's fundedThrough.
func (self *settlementV2AuditTestFixture) useFunding(funded map[uint64]uint256.Int) {
	self.chain.lock.Lock()
	defer self.chain.lock.Unlock()
	self.chain.funded[settlementV2AuditTestNoID] = map[uint64]*big.Int{}
	for epoch, value := range funded {
		self.chain.funded[settlementV2AuditTestNoID][epoch] = value.ToBig()
	}
}

// holdF keeps F's objects and its epoch's valid rows in custody, as the
// audit of F left them.
func (self *settlementV2AuditTestFixture) holdF(t *testing.T, f *ledger.Ledger) {
	t.Helper()
	ctx := context.Background()
	for ref, data := range f.Objects {
		if err := self.audit.custody.Keep(ctx, ref, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := self.audit.custody.KeepValidRows(ctx, settlementV2AuditTestNoID, 3, f.Epochs[0].Providers); err != nil {
		t.Fatal(err)
	}
}

// record reads the audit record of pool 1's seq.
func (self *settlementV2AuditTestFixture) record(t *testing.T, seq uint64) *settlementV2AuditRecord {
	t.Helper()
	var record settlementV2AuditRecord
	ok, err := self.audit.custody.readJSON(self.audit.custody.path("audits", fmt.Sprintf("%d-%d.json", settlementV2AuditTestNoID, seq)), &record)
	if err != nil || !ok {
		t.Fatalf("audit record of seq %d: %v %v", seq, ok, err)
	}
	return &record
}

// settlementV2AuditTestSender records what the audit asks a sender to do,
// runs before, when set, and answers each call with err.
type settlementV2AuditTestSender struct {
	before      func()
	approvals   []stabi.ISTSettlementVault2Pending
	challenges  []stabi.LedgerTreeAccount
	demands     []stabi.LedgerTreeAccount
	rootIndexes []uint64
	proofs      [][]stabi.LedgerTreeProofNode
	paths       [][]stabi.LedgerTreePathNode
	err         error
}

func (self *settlementV2AuditTestSender) call() {
	if self.before != nil {
		self.before()
	}
}

func (self *settlementV2AuditTestSender) Approve(ctx context.Context, noID uint64, pending stabi.ISTSettlementVault2Pending) error {
	self.call()
	self.approvals = append(self.approvals, pending)
	return self.err
}

func (self *settlementV2AuditTestSender) Challenge(ctx context.Context, noID uint64, pending stabi.ISTSettlementVault2Pending, rootIndex uint64, account stabi.LedgerTreeAccount, oldProof []stabi.LedgerTreeProofNode, newPath []stabi.LedgerTreePathNode) error {
	self.call()
	self.challenges, self.rootIndexes = append(self.challenges, account), append(self.rootIndexes, rootIndex)
	self.proofs, self.paths = append(self.proofs, oldProof), append(self.paths, newPath)
	return self.err
}

func (self *settlementV2AuditTestSender) Demand(ctx context.Context, noID uint64, pending stabi.ISTSettlementVault2Pending, rootIndex uint64, account stabi.LedgerTreeAccount, oldProof []stabi.LedgerTreeProofNode) error {
	self.call()
	self.demands, self.rootIndexes, self.proofs = append(self.demands, account), append(self.rootIndexes, rootIndex), append(self.proofs, oldProof)
	return self.err
}

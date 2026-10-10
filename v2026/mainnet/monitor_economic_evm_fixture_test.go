// Synthetic signed transactions, ABI logs and trie roots exercise the actual
// public HTTP reader. No production identity, funds or signing route is used.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/urfoundation/sn/v2026/stabi"
)

type monitorEvmFixtureBlock struct {
	header       *types.Header
	transactions types.Transactions
	receipts     []map[string]any
	snapshot     monitorEconomicEvmSnapshot
	funded       map[string]string
}

type monitorEvmFixture struct {
	mapping                *finalizedMappingFixture
	services               *monitorServicesFixture
	policy                 monitorEconomicEvmPolicy
	contract               *abi.ABI
	blocks                 map[uint64]*monitorEvmFixtureBlock
	byHash                 map[string]*monitorEvmFixtureBlock
	byTransaction          map[string]map[string]any
	url                    string
	ctx                    context.Context
	code                   []byte
	stateLock              sync.Mutex
	counts                 map[string]int
	fixtureGetters         map[string][]any
	fault                  func(string, []any, any) (any, bool)
	unavailable            atomic.Bool
	unavailableTransaction string
}

func monitorEvmTestLog(t *testing.T, contract *abi.ABI, address common.Address, name string, values ...any) *types.Log {
	t.Helper()
	event := contract.Events[name]
	if len(values) != len(event.Inputs) {
		t.Fatal("synthetic event arguments differ", name)
	}
	log := &types.Log{Address: address, Topics: []common.Hash{event.ID}}
	data := []any{}
	for index, field := range event.Inputs {
		if field.Indexed {
			topics, err := abi.MakeTopics([]any{values[index]})
			if err != nil {
				t.Fatal(err)
			}
			log.Topics = append(log.Topics, topics[0][0])
		} else {
			data = append(data, values[index])
		}
	}
	var err error
	log.Data, err = event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		t.Fatal(err)
	}
	return log
}

func monitorEvmVaultSnapshot(captured, paid, pending, liability, live, carry, credit string, coldkey string) monitorEconomicEvmSnapshot {
	return monitorEconomicEvmSnapshot{Counters: map[string]string{"totalCaptured": captured, "totalPaid": paid, "pendingFunding": pending, "outstandingLiability": liability, "escrowAccounted": new(big.Int).Add(mustMonitorEvmInteger(pending), mustMonitorEvmInteger(liability)).String(), "liveEscrowStake": live}, Pools: map[string]string{"1": carry}, Credits: map[string]string{coldkey: credit}}
}

func mustMonitorEvmInteger(value string) *big.Int {
	result, ok := new(big.Int).SetString(value, 10)
	if !ok {
		panic("invalid synthetic integer")
	}
	return result
}

func newMonitorEvmFixture(t *testing.T, kind string, peer bool) *monitorEvmFixture {
	t.Helper()
	_, mapping := newFinalizedMappingFixture(t, 3, 0)
	roles := []string{}
	if peer {
		roles = append(roles, "validator-a")
	}
	f := &monitorEvmFixture{mapping: mapping, services: newMonitorServicesFixture(t, roles...), blocks: map[uint64]*monitorEvmFixtureBlock{}, byHash: map[string]*monitorEvmFixtureBlock{}, byTransaction: map[string]map[string]any{}, code: []byte{0x60, 0x00, 0x60, 0x00, 0xf3}, counts: map[string]int{}}
	var err error
	f.contract, err = monitorEvmAbi(kind)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	address := common.HexToAddress("0x0000000000000000000000000000000000001234")
	coldkey := "0x" + strings.Repeat("71", 32)
	f.policy = monitorEconomicEvmPolicy{Role: "evm-a", Network: planNetwork{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}, ContractKind: kind, Address: address.Hex(), CodeHash: crypto.Keccak256Hash(f.code).Hex(), Netuid: 25, PoolIds: []string{"1"}, FeePayers: []string{sender.Hex()}, BatchBlocks: 3, HistoryEntries: 64, StallSeconds: 60, Finality: "owned-rpc-assertion"}
	if kind == "settlement-vault" {
		f.policy.Coldkeys = []string{coldkey}
	}
	snapshot := monitorEconomicEvmSnapshot{Counters: map[string]string{"principal": "100", "liveStake": "120"}, Pools: map[string]string{"1": "40"}, Credits: map[string]string{}}
	if kind == "settlement-vault" {
		snapshot = monitorEvmVaultSnapshot("100", "0", "0", "100", "120", "30", "5", coldkey)
	}
	parent := common.Hash{}
	for _, number := range []uint64{0, 10, 11, 12, 13} {
		logs := []*types.Log{}
		funded := map[string]string{}
		if number == 11 && kind == "settlement-vault" {
			logs = append(logs, monitorEvmTestLog(t, f.contract, address, "EmissionCaptured", big.NewInt(2), big.NewInt(1), [32]byte(common.HexToHash("0x"+strings.Repeat("72", 32))), big.NewInt(20)))
			logs = append(logs, monitorEvmTestLog(t, f.contract, address, "RootMissed", big.NewInt(2), big.NewInt(1), big.NewInt(20)))
			logs = append(logs, monitorEvmTestLog(t, f.contract, address, "Claimed", big.NewInt(1), big.NewInt(1), [32]byte(common.HexToHash(coldkey)), big.NewInt(700), big.NewInt(7), sender))
			logs = append(logs, monitorEvmTestLog(t, f.contract, address, "ClaimPaymentDeferred", [32]byte(common.HexToHash(coldkey)), big.NewInt(12), big.NewInt(1), uint64(10), uint8(0)))
			snapshot = monitorEvmVaultSnapshot("120", "0", "0", "120", "140", "50", "12", coldkey)
			funded["2/1"] = "20"
		} else if number == 12 && kind == "settlement-vault" {
			logs = append(logs, monitorEvmTestLog(t, f.contract, address, "EntitlementFinalized", big.NewInt(3), big.NewInt(1), [32]byte(common.HexToHash("0x"+strings.Repeat("73", 32))), [32]byte(common.HexToHash("0x"+strings.Repeat("74", 32))), big.NewInt(50), uint64(900)))
			logs = append(logs, monitorEvmTestLog(t, f.contract, address, "Claimed", big.NewInt(3), big.NewInt(1), [32]byte(common.HexToHash(coldkey)), big.NewInt(600), big.NewInt(3), sender))
			logs = append(logs, monitorEvmTestLog(t, f.contract, address, "ClaimPaid", [32]byte(common.HexToHash(coldkey)), big.NewInt(15), sender))
			snapshot = monitorEvmVaultSnapshot("120", "15", "0", "105", "125", "0", "0", coldkey)
			funded["3/1"] = "0"
		} else if number == 11 && kind == "reserve-sink" {
			logs = append(logs, monitorEvmTestLog(t, f.contract, address, "ReservePrincipalAdded", big.NewInt(2), big.NewInt(1), big.NewInt(20), big.NewInt(60), big.NewInt(120), big.NewInt(140)))
			snapshot = monitorEconomicEvmSnapshot{Counters: map[string]string{"principal": "120", "liveStake": "140"}, Pools: map[string]string{"1": "60"}, Credits: map[string]string{}}
		}
		block := &monitorEvmFixtureBlock{header: &types.Header{ParentHash: parent, UncleHash: types.EmptyUncleHash, Root: crypto.Keccak256Hash([]byte(fmt.Sprint("synthetic-state-", number))), TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: new(big.Int).SetUint64(number), GasLimit: 10000000, Time: 1700000000001 + number*12000, Extra: []byte{}}, transactions: types.Transactions{}, receipts: []map[string]any{}, snapshot: snapshot.clone(), funded: funded}
		if len(logs) != 0 {
			tx, err := types.SignNewTx(key, types.LatestSignerForChainID(big.NewInt(964)), &types.LegacyTx{Nonce: number, GasPrice: big.NewInt(2), Gas: 100000, To: &address, Value: big.NewInt(0)})
			if err != nil {
				t.Fatal(err)
			}
			block.transactions = types.Transactions{tx}
			receipt := &types.Receipt{Type: types.LegacyTxType, Status: 1, CumulativeGasUsed: 21000, Logs: logs, TxHash: tx.Hash(), GasUsed: 21000, EffectiveGasPrice: big.NewInt(2), BlockNumber: new(big.Int).SetUint64(number), TransactionIndex: 0}
			receipt.Bloom = types.CreateBloom(receipt)
			block.header.TxHash = types.DeriveSha(block.transactions, trie.NewStackTrie(nil))
			block.header.ReceiptHash = types.DeriveSha(types.Receipts{receipt}, trie.NewStackTrie(nil))
			block.header.GasUsed = 21000
			block.header.Bloom = receipt.Bloom
			receipt.BlockHash = block.header.Hash()
			for index, log := range logs {
				log.BlockHash = receipt.BlockHash
				log.BlockNumber = number
				log.TxHash = tx.Hash()
				log.TxIndex = 0
				log.Index = uint(index)
			}
			raw, err := json.Marshal(receipt)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			fields["from"], fields["to"], fields["contractAddress"] = sender.Hex(), address.Hex(), nil
			block.receipts = append(block.receipts, fields)
			f.byTransaction[tx.Hash().Hex()] = fields
		}
		f.blocks[number], f.byHash[block.header.Hash().Hex()] = block, block
		parent = block.header.Hash()
	}
	f.policy.From = economicEmissionBoundary{Number: 10, Hash: f.blocks[10].header.Hash().Hex()}
	f.policy.EvmGenesisHash = f.blocks[0].header.Hash().Hex()
	f.mapping.evmHeader = f.blocks[13].header
	f.mapping.evmHash = f.mapping.evmHeader.Hash().Hex()
	f.mapping.transactionHashes = []string{}
	raw, err := rlp.EncodeToBytes(f.mapping.evmHeader)
	if err != nil {
		t.Fatal(err)
	}
	f.mapping.rawEvmHeader = hexutil.Encode(raw)
	f.mapping.replaceNativeLogs(t, []string{mappingTestDigest(t, 3, f.mapping.evmHash, nil)})
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	f.url = server.URL
	return f
}

func (self *monitorEvmFixture) serve(w http.ResponseWriter, request *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(request.Body, 1024*1024))
	if err != nil {
		http.Error(w, "read", 400)
		return
	}
	var call struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	if json.Unmarshal(raw, &call) != nil {
		http.Error(w, "json", 400)
		return
	}
	self.stateLock.Lock()
	self.counts[call.Method]++
	fault := self.fault
	self.stateLock.Unlock()
	outageTransaction := self.unavailableTransaction
	if outageTransaction == "" && len(self.blocks[12].transactions) != 0 {
		outageTransaction = self.blocks[12].transactions[0].Hash().Hex()
	}
	if self.unavailable.Load() && call.Method == "eth_getTransactionReceipt" && len(call.Params) != 0 && call.Params[0] == outageTransaction {
		http.Error(w, "synthetic receipt outage", 503)
		return
	}
	var result any
	getBlock := func(value any) *monitorEvmFixtureBlock {
		if text, ok := value.(string); ok {
			if block, ok := self.byHash[text]; ok {
				return block
			}
			number, err := hexutil.DecodeUint64(text)
			if err == nil {
				return self.blocks[number]
			}
		}
		if selector, ok := value.(map[string]any); ok && selector["requireCanonical"] == true {
			if text, ok := selector["blockHash"].(string); ok {
				return self.byHash[text]
			}
		}
		return nil
	}
	switch call.Method {
	case "system_chain":
		result = "fixture-mainnet"
	case "eth_getBlockByNumber", "eth_getBlockByHash":
		block := getBlock(call.Params[0])
		if block == nil {
			break
		}
		transactions := []any{}
		for _, tx := range block.transactions {
			if call.Params[1] == true {
				transactions = append(transactions, tx)
			} else {
				transactions = append(transactions, tx.Hash().Hex())
			}
		}
		result = map[string]any{"hash": block.header.Hash().Hex(), "number": hexutil.EncodeUint64(block.header.Number.Uint64()), "transactions": transactions}
	case "debug_getRawHeader":
		block := getBlock(call.Params[0])
		if block == nil {
			break
		}
		raw, err := rlp.EncodeToBytes(block.header)
		if err != nil {
			http.Error(w, "rlp", 500)
			return
		}
		result = hexutil.Encode(raw)
	case "eth_getTransactionReceipt":
		result = self.byTransaction[call.Params[0].(string)]
	case "eth_getCode":
		if call.Params[0] != self.policy.Address || getBlock(call.Params[1]) == nil {
			http.Error(w, "unpinned code", 400)
			return
		}
		result = hexutil.Encode(self.code)
	case "eth_call":
		block := getBlock(call.Params[1])
		input, ok := call.Params[0].(map[string]any)
		if !ok || block == nil || input["to"] != self.policy.Address {
			http.Error(w, "unpinned call", 400)
			return
		}
		data, err := hexutil.Decode(input["data"].(string))
		if err != nil || len(data) < 4 {
			http.Error(w, "call data", 400)
			return
		}
		method, err := self.contract.MethodById(data[:4])
		if err != nil {
			http.Error(w, "method", 400)
			return
		}
		args, err := method.Inputs.Unpack(data[4:])
		if err != nil {
			http.Error(w, "args", 400)
			return
		}
		if values, exists := self.fixtureGetters[method.Name]; exists {
			encoded, err := method.Outputs.Pack(values...)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			result = hexutil.Encode(encoded)
			break
		}
		var value any
		switch method.Name {
		case "netuid":
			value = uint16(25)
		case "carry", "operatorPrincipal":
			value = mustMonitorEvmInteger(block.snapshot.Pools[args[0].(*big.Int).String()])
		case "claimCredit":
			value = mustMonitorEvmInteger(block.snapshot.Credits[common.Hash(args[0].([32]byte)).Hex()])
		case "entitlement":
			key := args[0].(*big.Int).String() + "/" + args[1].(*big.Int).String()
			funded, ok := block.funded[key]
			if !ok {
				http.Error(w, "funding", 400)
				return
			}
			value = stabi.STSettlementVaultEntitlement{Funded: mustMonitorEvmInteger(funded), Total: big.NewInt(50), Claimed: big.NewInt(0), ExpiryBlock: 900, Status: 3}
		default:
			amount, ok := block.snapshot.Counters[method.Name]
			if !ok {
				http.Error(w, "getter", 400)
				return
			}
			value = mustMonitorEvmInteger(amount)
		}
		encoded, err := method.Outputs.Pack(value)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		result = hexutil.Encode(encoded)
	default:
		request.Body = io.NopCloser(bytes.NewReader(raw))
		response, err := self.mapping.roundTrip(request)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
		return
	}
	if fault != nil {
		if replacement, replace := fault(call.Method, call.Params, result); replace {
			result = replacement
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
}

type monitorEvmTestEvent struct {
	Schema            string                    `json:"schema"`
	Role              string                    `json:"role"`
	Status            string                    `json:"status"`
	Current           bool                      `json:"current"`
	CheckpointCurrent bool                      `json:"checkpoint_current"`
	State             monitorEconomicEvmSummary `json:"state"`
	Issue             string                    `json:"issue"`
}
type monitorEvmTestSink struct {
	events chan monitorEvmTestEvent
	peers  chan monitorServiceEvent
}

func (self *monitorEvmTestSink) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}
func (self *monitorEvmTestSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	var event monitorEvmTestEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0, err
	}
	if event.Schema == "urnetwork-mainnet-evm-economic-event-v1" {
		select {
		case self.events <- event:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	} else if event.Schema == "urnetwork-mainnet-validator-event-v1" {
		var peer monitorServiceEvent
		if err := json.Unmarshal(raw, &peer); err != nil {
			return 0, err
		}
		select {
		case self.peers <- peer:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return len(raw), nil
}

type monitorEvmTestRun struct {
	cancel     context.CancelFunc
	done       chan struct{}
	resume     chan struct{}
	peerResume chan struct{}
	sink       *monitorEvmTestSink
	exit       int
	diagnostic bytes.Buffer
}

func (self *monitorEvmFixture) start(t *testing.T, hooks monitorServiceHooks) *monitorEvmTestRun {
	t.Helper()
	if self.ctx == nil {
		self.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{self.policy}
		self.services.writePolicy(t)
		checkpoint, _ := monitorEconomicEvmPaths(self.services.checkpointPath, self.services.metricsPath, self.policy.Role)
		provisionMonitorTestCustody(t, checkpoint)
		self.ctx = monitorTestStorageContext(t, t.Context(), self.services.args(self.url))
	}
	ctx, cancel := context.WithCancel(self.ctx)
	run := &monitorEvmTestRun{cancel: cancel, done: make(chan struct{}), resume: make(chan struct{}, 2), peerResume: make(chan struct{}, 2), sink: &monitorEvmTestSink{events: make(chan monitorEvmTestEvent, 8), peers: make(chan monitorServiceEvent, 8)}}
	if hooks.wait == nil {
		hooks.wait = func(ctx context.Context, role string, _ time.Duration) bool {
			var resume <-chan struct{}
			if role == self.policy.Role {
				resume = run.resume
			} else if role == "validator-a" {
				resume = run.peerResume
			}
			select {
			case <-ctx.Done():
				return false
			case <-resume:
				return true
			}
		}
	}
	go func() {
		defer close(run.done)
		run.exit = runMainWithMonitorHooks(ctx, self.services.args(self.url), run.sink, &run.diagnostic, self.services.clock.now, hooks)
	}()
	t.Cleanup(func() { run.stop(t) })
	return run
}

func (self *monitorEvmTestRun) next(t *testing.T) monitorEvmTestEvent {
	t.Helper()
	select {
	case event := <-self.sink.events:
		return event
	case <-self.done:
		t.Fatal("public EVM role ended before sample", self.exit, self.diagnostic.String())
	case <-time.After(60 * time.Second):
		t.Fatal("public EVM sample did not arrive")
	}
	return monitorEvmTestEvent{}
}
func (self *monitorEvmTestRun) stop(t *testing.T) {
	t.Helper()
	self.cancel()
	select {
	case <-self.done:
	case <-time.After(10 * time.Second):
		t.Fatal("public EVM monitor failed to join")
	}
}
func (self *monitorEvmFixture) record(t *testing.T) monitorEconomicEvmCheckpoint {
	t.Helper()
	path, _ := monitorEconomicEvmPaths(self.services.checkpointPath, self.services.metricsPath, self.policy.Role)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record monitorEconomicEvmCheckpoint
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

// Fault replacement uses a copied callback; the fixture lock never encloses a
// potentially blocking callback or network operation.
func (self *monitorEvmFixture) setFault(fault func(string, []any, any) (any, bool)) {
	self.stateLock.Lock()
	self.fault = fault
	self.stateLock.Unlock()
}

func monitorEvmFixtureClient(t *testing.T, fixture *monitorEvmFixture) *rpcClient {
	t.Helper()
	client, err := newRpcClient(fixture.url, 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	return client
}

// Head selection is done before starting the public command. Native and EVM
// heights deliberately differ, including a large retained EVM backlog.
func (self *monitorEvmFixture) setFinalized(t *testing.T, number uint64) {
	t.Helper()
	block := self.blocks[number]
	self.mapping.evmHeader = block.header
	self.mapping.evmHash = block.header.Hash().Hex()
	encoded, err := rlp.EncodeToBytes(block.header)
	if err != nil {
		t.Fatal(err)
	}
	self.mapping.rawEvmHeader = hexutil.Encode(encoded)
	self.mapping.replaceNativeLogs(t, []string{mappingTestDigest(t, 3, self.mapping.evmHash, nil)})
}

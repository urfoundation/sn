// The `snclaim v2` tests (CL-1, CL-3, E-4, VA-4) run the real usage and
// commands against v2TestChain: a JSON-RPC fake of the chain and of vault2,
// whose model runs the calls snclaim makes with vault2's checks in their
// pinned order (SPEC "vault2", A1, A2), and an httptest operator API serving
// GET /sn/ledger/account and GET /sn/ledger/object. Ledgers and trees are
// built with the ledger and merkle packages.
package onchain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/stabi"
)

const (
	v2TestVault   = "0x2222222222222222222222222222222222222222"
	v2TestChainID = 945
	v2TestPool    = 3
	// v2TestHead is the finalized block, and the latest block unless a test
	// moves v2TestChain.head.
	v2TestHead = 200_000
	// v2TestFee is demandFeeWei: 3 gwei, a multiple of one rao (SPEC A5).
	v2TestFee = 3_000_000_000
	// v2TestChallengeBlocks is vault2's CHALLENGE_BLOCKS.
	v2TestChallengeBlocks = 14_400
)

// v2TestABI is vault2's ABI.
var v2TestABI = func() *abi.ABI {
	parsed, err := stabi.STSettlementVault2MetaData.ParseABI()
	if err != nil {
		panic(err)
	}
	return parsed
}()

// v2TestError is the revert data of a custom error without arguments.
func v2TestError(name string) []byte {
	return keccak256x([]byte(name + "()"))[:4]
}

func v2TestBlockHash(number uint64) common.Hash {
	return common.BytesToHash(append([]byte{0xb1}, new(big.Int).SetUint64(number).Bytes()...))
}

// v2TestChain is the chain, vault2 and the operator API.
type v2TestChain struct {
	t       *testing.T
	rpc     *httptest.Server
	api     *httptest.Server
	keyFile string
	vault   common.Address

	lock     sync.Mutex
	calls    map[string]int
	blocks   []string // eth_call block tags, in order
	spans    [][2]uint64
	head     uint64 // the latest block
	pool     stabi.ISTSettlementVault2Pool
	roots    []stabi.ISTSettlementVault2Root
	pending  stabi.ISTSettlementVault2Pending
	funded   map[uint64]*big.Int
	claimed  map[[2]uint64]*big.Int // by pool and index
	demanded map[[3]uint64]uint8    // by seq, root and index
	fee      *big.Int
	logs     []types.Log
	sent     []*types.Transaction
	receipts map[common.Hash]*types.Receipt
	emitted  []*types.Log // logs of the transactions snclaim sent

	// The operator API: account replies by coldkey query, and objects.
	accounts      map[string]string
	accountStatus int
	objects       ledger.Objects
	apiCalls      map[string]int

	// mute drops the events of the transactions snclaim sends.
	mute bool
	// batcher, when set, is an STClaimBatcher (v2_batch_test.go) over the
	// model at that address; pools holds the roots of pools other than
	// v2TestPool, and gasLimit is the latest block's gas limit.
	batcher  common.Address
	pools    map[uint64][]stabi.ISTSettlementVault2Root
	gasLimit uint64
	// batcherCode, when set, is the batcher address's code in place of the
	// release runtime.
	batcherCode []byte
	// exhaust makes a claim of (pool, index) fail as a runtime refusal
	// does: without revert data, and in a batch consuming its whole gas cap.
	// itemGas, when set, replaces the gas a paid batch item uses;
	// revertBatch reverts a sent batch and muteVault drops vault2's events
	// from its receipt.
	exhaust     map[[2]uint64]bool
	itemGas     uint64
	revertBatch bool
	muteVault   bool
	// claimEstimate, when set, answers eth_estimateGas of a claim;
	// batchBase, when set, is the batch model's gas outside its items, and
	// batchAnswer, when set, answers every eth_call of claimMany.
	claimEstimate uint64
	batchBase     uint64
	batchAnswer   *big.Int
	// failAt fails the nth answer to a key (an RPC method, or method:function)
	// with an RPC error; garbage answers an eth_call of a function with no
	// data; revert refuses a preflight and estimate of a function with an
	// error; before runs under the lock before each answer.
	failAt  map[string]int
	garbage map[string]bool
	revert  map[string]string
	before  func(key string)
}

func newV2TestChain(t *testing.T) *v2TestChain {
	t.Helper()
	c := &v2TestChain{
		t:        t,
		keyFile:  filepath.Join(t.TempDir(), "relayer.key"),
		vault:    common.HexToAddress(v2TestVault),
		calls:    map[string]int{},
		head:     v2TestHead,
		pool:     stabi.ISTSettlementVault2Pool{Hotkey: [32]byte{0x77}, FirstEpoch: 10, Registered: true},
		funded:   map[uint64]*big.Int{},
		claimed:  map[[2]uint64]*big.Int{},
		demanded: map[[3]uint64]uint8{},
		fee:      big.NewInt(v2TestFee),
		receipts: map[common.Hash]*types.Receipt{},
		accounts: map[string]string{},
		objects:  ledger.Objects{},
		apiCalls: map[string]int{},
		failAt:   map[string]int{},
		garbage:  map[string]bool{},
		revert:   map[string]string{},
		pools:    map[uint64][]stabi.ISTSettlementVault2Root{},
		gasLimit: 75_000_000,
		exhaust:  map[[2]uint64]bool{},
	}
	if err := os.WriteFile(c.keyFile, []byte(testKeyHex), 0o600); err != nil {
		t.Fatal(err)
	}
	c.rpc = httptest.NewServer(http.HandlerFunc(c.serveRPC))
	t.Cleanup(c.rpc.Close)
	c.api = httptest.NewServer(http.HandlerFunc(c.serveAPI))
	t.Cleanup(c.api.Close)
	return c
}

// rpcError is a JSON-RPC error; data, when set, is revert data.
func rpcError(code int, message string, data []byte) map[string]any {
	out := map[string]any{"code": code, "message": message}
	if data != nil {
		out["data"] = hexutil.Bytes(data)
	}
	return out
}

func (c *v2TestChain) serveRPC(writer http.ResponseWriter, request *http.Request) {
	var call struct {
		Id     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		c.t.Error(err)
		return
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	reply := map[string]any{"jsonrpc": "2.0", "id": call.Id}
	if result, failure := c.answer(call.Method, call.Params); failure != nil {
		reply["error"] = failure
	} else {
		reply["result"] = result
	}
	_ = json.NewEncoder(writer).Encode(reply)
}

// count counts an answer to key and reports whether it is to fail.
func (c *v2TestChain) count(key string) bool {
	c.calls[key]++
	if c.before != nil {
		c.before(key)
	}
	return c.failAt[key] == c.calls[key]
}

// function names the vault2 function input calls.
func (c *v2TestChain) function(input []byte) string {
	if len(input) >= 4 {
		if method, err := v2TestABI.MethodById(input[:4]); err == nil {
			return method.Name
		}
	}
	return "unknown"
}

func (c *v2TestChain) answer(method string, params []json.RawMessage) (any, map[string]any) {
	switch method {
	case "eth_chainId":
		if c.count(method) {
			return nil, rpcError(-32000, "synthetic chain id refusal", nil)
		}
		return hexutil.EncodeUint64(v2TestChainID), nil
	case "eth_getBlockByNumber":
		var selector string
		if len(params) != 2 || json.Unmarshal(params[0], &selector) != nil {
			c.t.Errorf("block request has no selector: %s", params)
		}
		if c.count(method + ":" + selector) {
			return nil, rpcError(-32601, "synthetic block refusal", nil)
		}
		number := uint64(v2TestHead)
		switch selector {
		case "finalized":
		case "latest":
			number = c.head
		default:
			number = hexutil.MustDecodeUint64(selector)
		}
		block := syntheticBlockIdentityFixture(c.t, number, v2TestBlockHash(number))
		block["gasLimit"] = hexutil.EncodeUint64(c.gasLimit)
		if c.gasLimit == 0 {
			delete(block, "gasLimit")
		}
		return block, nil
	case "eth_call", "eth_estimateGas":
		var message struct {
			From  common.Address  `json:"from"`
			To    common.Address  `json:"to"`
			Input hexutil.Bytes   `json:"input"`
			Value *hexutil.Big    `json:"value"`
			Gas   *hexutil.Uint64 `json:"gas"`
		}
		if len(params) == 0 || json.Unmarshal(params[0], &message) != nil || message.To != c.vault && (message.To != c.batcher || c.batcher == common.Address{}) {
			c.t.Errorf("%s does not address vault2: %s", method, params)
		}
		if message.To == c.batcher {
			return c.answerBatch(method, message.Gas, message.Input)
		}
		function := c.function(message.Input)
		key := method + ":" + function
		if method == "eth_call" {
			var block string
			if len(params) != 2 || json.Unmarshal(params[1], &block) != nil {
				c.t.Errorf("eth_call has no block: %s", params)
			}
			c.blocks = append(c.blocks, function+"@"+block)
		}
		if c.count(key) {
			return nil, rpcError(-32000, "synthetic refusal of "+key, nil)
		}
		if c.garbage[function] {
			return hexutil.Bytes{}, nil
		}
		if name, ok := c.revert[function]; ok {
			return nil, rpcError(3, "execution reverted", v2TestError(name))
		}
		out, _, revert := c.execute(message.From, (*big.Int)(message.Value), message.Input, false)
		if revert != nil {
			return nil, rpcError(3, "execution reverted", revert)
		}
		if method == "eth_estimateGas" {
			if c.claimEstimate != 0 && function == "claim" {
				return hexutil.EncodeUint64(c.claimEstimate), nil
			}
			return "0xc350", nil
		}
		return hexutil.Bytes(out), nil
	case "eth_getCode":
		var account common.Address
		if len(params) != 2 || json.Unmarshal(params[0], &account) != nil {
			c.t.Errorf("code request: %s", params)
		}
		if c.count(method) {
			return nil, rpcError(-32000, "synthetic code refusal", nil)
		}
		if account != c.batcher || c.batcher == (common.Address{}) {
			return "0x", nil
		}
		if c.batcherCode != nil {
			return hexutil.Bytes(c.batcherCode), nil
		}
		return ClaimBatcherRuntime, nil
	case "eth_getTransactionCount":
		var account common.Address
		if len(params) != 2 || json.Unmarshal(params[0], &account) != nil || account != common.HexToAddress(testKeyAddr) || string(params[1]) != `"pending"` {
			c.t.Errorf("nonce is not the relayer's pending nonce: %s", params)
		}
		c.count(method)
		return hexutil.EncodeUint64(7 + uint64(len(c.sent))), nil
	case "eth_gasPrice":
		if c.count(method) {
			return nil, rpcError(-32000, "synthetic gas price refusal", nil)
		}
		return "0x2", nil
	case "eth_sendRawTransaction":
		var raw hexutil.Bytes
		tx := new(types.Transaction)
		if len(params) != 1 || json.Unmarshal(params[0], &raw) != nil || tx.UnmarshalBinary(raw) != nil {
			c.t.Errorf("send lacks one signed transaction: %s", params)
			return nil, rpcError(-32000, "malformed send", nil)
		}
		sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(v2TestChainID)), tx)
		if err != nil || tx.To() == nil || *tx.To() != c.vault && (*tx.To() != c.batcher || c.batcher == common.Address{}) {
			c.t.Errorf("send from %s (%v) to %v", sender, err, tx.To())
		}
		if *tx.To() == c.batcher {
			return c.sendBatch(tx), nil
		}
		c.sent = append(c.sent, tx)
		c.count("send:" + c.function(tx.Data()))
		_, logs, revert := c.execute(sender, tx.Value(), tx.Data(), true)
		receipt := &types.Receipt{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 50_000, TxHash: tx.Hash(), GasUsed: 50_000, EffectiveGasPrice: big.NewInt(2), BlockHash: v2TestBlockHash(v2TestHead), BlockNumber: big.NewInt(v2TestHead)}
		if revert != nil {
			receipt.Status, logs = types.ReceiptStatusFailed, nil
		}
		if c.mute {
			logs = nil
		}
		for _, log := range logs {
			log.TxHash, log.BlockHash, log.BlockNumber, log.Index = tx.Hash(), receipt.BlockHash, v2TestHead, uint(len(c.logs))
			c.logs = append(c.logs, *log)
			c.emitted = append(c.emitted, log)
		}
		receipt.Logs = append([]*types.Log{}, logs...)
		c.receipts[tx.Hash()] = receipt
		return tx.Hash(), nil
	case "eth_getTransactionReceipt":
		var hash common.Hash
		if len(params) != 1 || json.Unmarshal(params[0], &hash) != nil || c.receipts[hash] == nil {
			c.t.Errorf("receipt of an unknown transaction: %s", params)
		}
		c.count(method)
		return c.receipts[hash], nil
	case "eth_getLogs":
		var filter struct {
			FromBlock hexutil.Uint64   `json:"fromBlock"`
			ToBlock   hexutil.Uint64   `json:"toBlock"`
			Address   []common.Address `json:"address"`
			Topics    [][]common.Hash  `json:"topics"`
		}
		if len(params) != 1 || json.Unmarshal(params[0], &filter) != nil || !slices.Equal(filter.Address, []common.Address{c.vault}) || filter.ToBlock < filter.FromBlock || filter.ToBlock-filter.FromBlock >= v2LogChunkBlocks || filter.ToBlock > v2TestHead {
			c.t.Errorf("eth_getLogs filter is not one span of vault2 below the finalized head: %s", params)
		}
		c.spans = append(c.spans, [2]uint64{uint64(filter.FromBlock), uint64(filter.ToBlock)})
		if c.count(method) {
			return nil, rpcError(-32005, "synthetic log refusal", nil)
		}
		matched := []types.Log{}
		for _, log := range c.logs {
			if log.BlockNumber < uint64(filter.FromBlock) || log.BlockNumber > uint64(filter.ToBlock) {
				continue
			}
			match := true
			for i, topics := range filter.Topics {
				if len(topics) > 0 && (i >= len(log.Topics) || !slices.Contains(topics, log.Topics[i])) {
					match = false
				}
			}
			if match {
				matched = append(matched, log)
			}
		}
		return matched, nil
	}
	c.t.Errorf("unexpected method %s", method)
	return nil, rpcError(-32601, "unexpected method", nil)
}

// execute runs a vault2 call on the model, changing state only to commit a
// sent transaction. It returns the call's output, its logs, or revert data.
func (c *v2TestChain) execute(from common.Address, value *big.Int, input []byte, commit bool) ([]byte, []*types.Log, []byte) {
	method, err := v2TestABI.MethodById(input[:4])
	if err != nil {
		c.t.Errorf("call of an unknown vault2 function 0x%x", input)
		return nil, nil, v2TestError("Unknown")
	}
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		c.t.Errorf("%s arguments do not decode: %v", method.Name, err)
		return nil, nil, v2TestError("Unknown")
	}
	ours := len(args) > 0 && args[0].(*big.Int).Cmp(big.NewInt(v2TestPool)) == 0
	pack := func(values ...any) []byte {
		out, err := method.Outputs.Pack(values...)
		if err != nil {
			c.t.Errorf("%s outputs do not pack: %v", method.Name, err)
		}
		return out
	}
	switch method.Name {
	case "pools":
		if !ours {
			return pack(stabi.ISTSettlementVault2Pool{}), nil, nil
		}
		return pack(c.pool), nil, nil
	case "pending":
		return pack(c.pending), nil, nil
	case "rootCount":
		return pack(big.NewInt(int64(len(c.roots)))), nil, nil
	case "roots":
		index, roots := args[1].(*big.Int), c.rootsOf(args[0].(*big.Int))
		if !index.IsUint64() || index.Uint64() >= uint64(len(roots)) {
			return nil, nil, v2TestError("UnknownRoot")
		}
		return pack(roots[index.Uint64()]), nil, nil
	case "fundedThrough":
		funded := c.funded[args[1].(*big.Int).Uint64()]
		if funded == nil {
			funded = new(big.Int)
		}
		return pack(funded), nil, nil
	case "claimed":
		claimed := c.claimed[[2]uint64{args[0].(*big.Int).Uint64(), args[1].(*big.Int).Uint64()}]
		if claimed == nil {
			claimed = new(big.Int)
		}
		return pack(claimed), nil, nil
	case "demanded":
		return pack(c.demanded[[3]uint64{args[1].(uint64), args[2].(*big.Int).Uint64(), args[3].(*big.Int).Uint64()}]), nil, nil
	case "demandFeeWei":
		return pack(c.fee), nil, nil
	case "CHALLENGE_BLOCKS":
		return pack(uint64(v2TestChallengeBlocks)), nil, nil
	case "claim":
		account := *abi.ConvertType(args[2], new(stabi.LedgerTreeAccount)).(*stabi.LedgerTreeAccount)
		proof := *abi.ConvertType(args[3], new([]stabi.LedgerTreeProofNode)).(*[]stabi.LedgerTreeProofNode)
		return c.claim(from, args[0].(*big.Int), args[1].(*big.Int), account, proof, commit, pack)
	case "checkPath":
		account := *abi.ConvertType(args[2], new(stabi.LedgerTreeAccount)).(*stabi.LedgerTreeAccount)
		proof := *abi.ConvertType(args[3], new([]stabi.LedgerTreeProofNode)).(*[]stabi.LedgerTreeProofNode)
		path := *abi.ConvertType(args[4], new([]stabi.LedgerTreePathNode)).(*[]stabi.LedgerTreePathNode)
		return c.checkPath(from, args[1].(*big.Int), account, proof, path, commit, pack)
	case "demandPath":
		account := *abi.ConvertType(args[2], new(stabi.LedgerTreeAccount)).(*stabi.LedgerTreeAccount)
		proof := *abi.ConvertType(args[3], new([]stabi.LedgerTreeProofNode)).(*[]stabi.LedgerTreeProofNode)
		return c.demandPath(from, value, args[1].(*big.Int), account, proof, commit)
	}
	c.t.Errorf("unexpected vault2 call %s", method.Name)
	return nil, nil, v2TestError("Unknown")
}

// verify is LedgerTree.verifyLeaf of a vault2 root.
func v2TestVerify(root stabi.ISTSettlementVault2Root, account stabi.LedgerTreeAccount, proof []stabi.LedgerTreeProofNode) bool {
	nodes := make([]merkle.LedgerNode, len(proof))
	for k := range proof {
		nodes[k] = merkle.LedgerNode{Hash: proof[k].Hash, Sum: *uint256.MustFromBig(proof[k].Sum)}
	}
	return account.Index.IsUint64() && merkle.VerifyLedgerProof(root.Root, uint256.MustFromBig(root.Total), root.LeafCount, account.Index.Uint64(), account.Coldkey, uint256.MustFromBig(account.Cumulative), nodes)
}

// rootsOf returns the finalized roots of a pool.
func (c *v2TestChain) rootsOf(noID *big.Int) []stabi.ISTSettlementVault2Root {
	if noID.Cmp(big.NewInt(v2TestPool)) == 0 {
		return c.roots
	}
	if !noID.IsUint64() {
		return nil
	}
	return c.pools[noID.Uint64()]
}

func (c *v2TestChain) claim(from common.Address, noID, rootIndex *big.Int, account stabi.LedgerTreeAccount, proof []stabi.LedgerTreeProofNode, commit bool, pack func(...any) []byte) ([]byte, []*types.Log, []byte) {
	roots := c.rootsOf(noID)
	if !rootIndex.IsUint64() || rootIndex.Uint64() >= uint64(len(roots)) {
		return nil, nil, v2TestError("UnknownRoot")
	}
	if account.Coldkey == ([32]byte{}) {
		return nil, nil, v2TestError("InvalidColdkey")
	}
	if !v2TestVerify(roots[rootIndex.Uint64()], account, proof) {
		return nil, nil, v2TestError("InvalidProof")
	}
	key := [2]uint64{noID.Uint64(), account.Index.Uint64()}
	claimed := c.claimed[key]
	if claimed == nil {
		claimed = new(big.Int)
	}
	if account.Cumulative.Cmp(claimed) <= 0 {
		return nil, nil, v2TestError("NothingToClaim")
	}
	if c.exhaust[key] {
		return nil, nil, []byte{}
	}
	amount := new(big.Int).Sub(account.Cumulative, claimed)
	if !commit {
		return pack(amount), nil, nil
	}
	c.claimed[key] = account.Cumulative
	log := c.event("Claimed", []common.Hash{common.BigToHash(noID), common.BigToHash(account.Index), account.Coldkey}, rootIndex, account.Cumulative, amount, from)
	return pack(amount), []*types.Log{log}, nil
}

// disputed is vault2's _disputedRoots and _requireProven: a pending root,
// a finalized root rootIndex, and the account proven against it.
func (c *v2TestChain) disputed(rootIndex *big.Int, account stabi.LedgerTreeAccount, proof []stabi.LedgerTreeProofNode, window bool) []byte {
	if c.pending.Seq == 0 {
		return v2TestError("NoPendingRoot")
	}
	if !rootIndex.IsUint64() || rootIndex.Uint64() >= uint64(len(c.roots)) {
		return v2TestError("UnknownRoot")
	}
	if window && v2TestHead >= c.pending.CommittedBlock+v2TestChallengeBlocks {
		return v2TestError("ChallengeWindowClosed")
	}
	if proven := c.roots[rootIndex.Uint64()]; !account.Index.IsUint64() || account.Index.Uint64() >= proven.LeafCount {
		return v2TestError("IndexOutOfRange")
	} else if !v2TestVerify(proven, account, proof) {
		return v2TestError("InvalidProof")
	}
	return nil
}

func (c *v2TestChain) checkPath(from common.Address, rootIndex *big.Int, account stabi.LedgerTreeAccount, proof []stabi.LedgerTreeProofNode, path []stabi.LedgerTreePathNode, commit bool, pack func(...any) []byte) ([]byte, []*types.Log, []byte) {
	if revert := c.disputed(rootIndex, account, proof, false); revert != nil {
		return nil, nil, revert
	}
	nodes := make([]merkle.LedgerPathNode, len(path))
	for k := range path {
		nodes[k] = merkle.LedgerPathNode{LeftHash: path[k].LeftHash, LeftSum: *uint256.MustFromBig(path[k].LeftSum), RightHash: path[k].RightHash, RightSum: *uint256.MustFromBig(path[k].RightSum)}
	}
	index := account.Index.Uint64()
	fault, level, err := merkle.WalkLedgerPath(c.pending.Root, uint256.MustFromBig(c.pending.Total), c.pending.LeafCount, index, account.Coldkey, uint256.MustFromBig(account.Cumulative), nodes)
	if err != nil {
		return nil, nil, v2TestError("MalformedPath")
	}
	seq := c.pending.Seq
	key := [3]uint64{seq, rootIndex.Uint64(), index}
	switch {
	case fault != merkle.LedgerFaultNone:
		if !commit {
			return pack(uint8(fault)), nil, nil
		}
		c.pending = stabi.ISTSettlementVault2Pending{Total: new(big.Int)}
		return pack(uint8(fault)), []*types.Log{c.event("DistributionDeleted", c.seqTopics(seq), rootIndex, account.Index, uint8(fault), big.NewInt(int64(level)), from)}, nil
	case c.demanded[key] == 1:
		if !commit {
			return pack(uint8(0)), nil, nil
		}
		c.demanded[key] = 2
		c.pending.OpenDemands--
		return pack(uint8(0)), []*types.Log{c.event("PathAnswered", c.seqTopics(seq), rootIndex, account.Index, from)}, nil
	}
	return nil, nil, v2TestError("NoFault")
}

func (c *v2TestChain) demandPath(from common.Address, value *big.Int, rootIndex *big.Int, account stabi.LedgerTreeAccount, proof []stabi.LedgerTreeProofNode, commit bool) ([]byte, []*types.Log, []byte) {
	if value == nil || value.Cmp(c.fee) != 0 {
		return nil, nil, v2TestError("InvalidDemandFee")
	}
	if revert := c.disputed(rootIndex, account, proof, true); revert != nil {
		return nil, nil, revert
	}
	key := [3]uint64{c.pending.Seq, rootIndex.Uint64(), account.Index.Uint64()}
	if c.demanded[key] != 0 {
		return nil, nil, v2TestError("AlreadyDemanded")
	}
	if !commit {
		return nil, nil, nil
	}
	c.demanded[key] = 1
	c.pending.OpenDemands++
	return nil, []*types.Log{c.event("PathDemanded", c.seqTopics(c.pending.Seq), rootIndex, account.Index, from)}, nil
}

func (c *v2TestChain) seqTopics(seq uint64) []common.Hash {
	return []common.Hash{common.BigToHash(big.NewInt(v2TestPool)), common.BigToHash(new(big.Int).SetUint64(seq))}
}

// event encodes a vault2 event with its indexed topics and its data.
func (c *v2TestChain) event(name string, indexed []common.Hash, data ...any) *types.Log {
	event := v2TestABI.Events[name]
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		c.t.Fatalf("%s data does not pack: %v", name, err)
	}
	return &types.Log{Address: c.vault, Topics: append([]common.Hash{event.ID}, indexed...), Data: packed}
}

// addLog records a vault2 event at a block, as history.
func (c *v2TestChain) addLog(block uint64, log *types.Log) {
	log.BlockNumber, log.BlockHash, log.TxHash, log.Index = block, v2TestBlockHash(block), common.Hash{0x7e, byte(len(c.logs))}, uint(len(c.logs))
	c.logs = append(c.logs, *log)
}

// committed and finalized record a commit and a finalization in history.
func (c *v2TestChain) committed(block, seq uint64, root ledger.Root, replacedSeq uint64) {
	c.addLog(block, c.event("DistributionCommitted", c.seqTopics(seq), root.Epoch, root.Root, root.Total.ToBig(), root.LeafCount, root.LedgerHash, replacedSeq))
}

func (c *v2TestChain) finalized(block, seq, rootIndex uint64, root ledger.Root) {
	c.addLog(block, c.event("DistributionFinalized", c.seqTopics(seq), new(big.Int).SetUint64(rootIndex), root.Epoch, root.Root, root.Total.ToBig(), root.LeafCount, root.LedgerHash))
}

func (c *v2TestChain) serveAPI(writer http.ResponseWriter, request *http.Request) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.apiCalls[request.URL.Path]++
	switch request.URL.Path {
	case "/sn/ledger/account":
		if c.accountStatus != 0 {
			http.Error(writer, "synthetic account refusal", c.accountStatus)
			return
		}
		coldkey := request.URL.Query().Get("coldkey")
		reply, ok := c.accounts[coldkey]
		if !ok {
			reply = fmt.Sprintf(`{"coldkey":%q,"accounts":[]}`, coldkey)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, reply)
	case "/sn/ledger/object":
		ref, err := ledger.ParseRef(request.URL.Query().Get("hash"))
		if err != nil {
			http.Error(writer, "malformed hash", http.StatusBadRequest)
			return
		}
		data, ok := c.objects[ref]
		if !ok {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write(data)
	default:
		c.t.Errorf("unexpected API path %s", request.URL.Path)
		http.NotFound(writer, request)
	}
}

// observed lists the answered calls with their counts, in name order;
// prefixes keep only keys that start with one of them.
func (c *v2TestChain) observed(prefixes ...string) string {
	c.lock.Lock()
	defer c.lock.Unlock()
	var keys []string
	for key, count := range c.calls {
		if len(prefixes) == 0 || slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(key, prefix) }) {
			keys = append(keys, fmt.Sprintf("%s=%d", key, count))
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, " ")
}

func (c *v2TestChain) transactions() []*types.Transaction {
	c.lock.Lock()
	defer c.lock.Unlock()
	return slices.Clone(c.sent)
}

// do runs f under the lock, for a test acting on the chain between calls.
func (c *v2TestChain) do(f func()) {
	c.lock.Lock()
	defer c.lock.Unlock()
	f()
}

// argv completes a v2 command for this fixture: each flag given replaces
// the flag of the same name or is appended, and "!name" removes it.
func (c *v2TestChain) argv(command []string, flags ...string) []string {
	argv := slices.Clone(command)
	for _, flag := range flags {
		named := func(name string) func(string) bool {
			return func(arg string) bool { return arg == name || strings.HasPrefix(arg, name+"=") }
		}
		if name, ok := strings.CutPrefix(flag, "!"); ok {
			argv = slices.DeleteFunc(argv, named(name))
			continue
		}
		name, _, _ := strings.Cut(flag, "=")
		if index := slices.IndexFunc(argv, named(name)); index >= 0 {
			argv[index] = flag
		} else {
			argv = append(argv, flag)
		}
	}
	return argv
}

// The commands with this fixture's endpoints, relayer key and alice.
func (c *v2TestChain) statusArgv(flags ...string) []string {
	return c.argv([]string{"v2", "status", "--coldkey=" + aliceSS58, "--vault=" + v2TestVault, "--rpc=" + c.rpc.URL, "--api_url=" + c.api.URL}, flags...)
}

func (c *v2TestChain) claimArgv(flags ...string) []string {
	return c.argv([]string{"v2", "claim", "--coldkey=" + aliceSS58, "--vault=" + v2TestVault, "--rpc=" + c.rpc.URL, "--key_file=" + c.keyFile, "--api_url=" + c.api.URL}, flags...)
}

func (c *v2TestChain) checkArgv(flags ...string) []string {
	return c.argv([]string{"v2", "check", "--all", "--vault=" + v2TestVault, fmt.Sprintf("--no_id=%d", v2TestPool), "--rpc=" + c.rpc.URL, "--api_url=" + c.api.URL, "--key_file=" + c.keyFile}, flags...)
}

// run parses argv with the real usage and runs the v2 command, returning
// what it printed. A read retry fails at once instead of waiting. Taking the
// lock before and after orders the test's setup before the handlers and
// their writes before the test's reads.
func (c *v2TestChain) run(argv []string) (string, error) {
	c.t.Helper()
	opts, err := parseArgv(c.t, argv)
	if err != nil {
		c.t.Fatalf("usage refused %q: %v", argv, err)
	}
	c.do(func() {})
	defer c.do(func() {})
	parent, cancel := context.WithTimeout(c.t.Context(), 120*time.Second)
	defer cancel()
	ctx, _ := onchainReadTestContext(c.t, parent, func(context.Context, time.Duration) error {
		return errors.New("synthetic v2 fixture forbids a retry wait")
	})
	return v2Capture(c.t, func() error { return cmdV2(ctx, opts) })
}

// v2Capture returns what f prints to stdout, and its error.
func v2Capture(t *testing.T, f func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = writer
	output := make(chan string)
	go func() {
		data, _ := io.ReadAll(reader)
		output <- string(data)
	}()
	defer func() { os.Stdout = stdout }()
	err = f()
	os.Stdout = stdout
	writer.Close()
	return <-output, err
}

// v2TestKey returns coldkey n of a fixture ledger; keys order as their n.
func v2TestKey(n int) [32]byte {
	var key [32]byte
	key[0], key[1], key[31] = 0x10, byte(n>>8), byte(n)
	return key
}

// v2TestAccounts returns accounts of the given values, coldkey i+1 at i.
func v2TestAccounts(values ...uint64) []merkle.LedgerAccount {
	accounts := make([]merkle.LedgerAccount, len(values))
	for i, value := range values {
		accounts[i] = merkle.LedgerAccount{Coldkey: v2TestKey(i + 1), Cumulative: *uint256.NewInt(value)}
	}
	return accounts
}

func v2TestTree(t *testing.T, accounts []merkle.LedgerAccount) *merkle.LedgerSumTree {
	t.Helper()
	tree, err := merkle.BuildLedgerSumTree(context.Background(), accounts)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// v2TestRoot is a tree's root as vault2 holds it.
func v2TestRoot(tree *merkle.LedgerSumTree, epoch uint64, ledgerHash [32]byte) stabi.ISTSettlementVault2Root {
	root, total := tree.Root()
	return stabi.ISTSettlementVault2Root{Root: root, Total: total.ToBig(), LeafCount: tree.LeafCount(), Epoch: epoch, LedgerHash: ledgerHash}
}

// v2TestRawTree is a full sum tree whose node sums a test may misstate.
type v2TestRawTree struct{ levels [][]merkle.LedgerNode }

func newV2TestRawTree(t *testing.T, accounts []merkle.LedgerAccount) *v2TestRawTree {
	t.Helper()
	depth, err := merkle.LedgerTreeDepth(uint64(len(accounts)))
	if err != nil {
		t.Fatal(err)
	}
	leaves := make([]merkle.LedgerNode, 1<<depth)
	for i := range accounts {
		leaves[i] = merkle.LedgerNode{Hash: merkle.LedgerLeafHash(accounts[i].Coldkey, &accounts[i].Cumulative), Sum: accounts[i].Cumulative}
	}
	tree := &v2TestRawTree{levels: [][]merkle.LedgerNode{leaves}}
	for level := 1; level <= depth; level++ {
		tree.levels = append(tree.levels, make([]merkle.LedgerNode, len(tree.levels[level-1])/2))
		for j := range tree.levels[level] {
			tree.recompute(level, j)
		}
	}
	return tree
}

func (r *v2TestRawTree) recompute(level, j int) {
	left, right := r.levels[level-1][2*j], r.levels[level-1][2*j+1]
	node := &r.levels[level][j]
	node.Hash = merkle.LedgerNodeHash(left.Hash, &left.Sum, right.Hash, &right.Sum)
	node.Sum.Add(&left.Sum, &right.Sum)
}

// misstate makes node j of level state sum, keeping its children: an
// inconsistent node. The nodes above it are recomputed from it.
func (r *v2TestRawTree) misstate(level, j int, sum uint256.Int) {
	r.levels[level][j].Sum = sum
	for level++; level < len(r.levels); level++ {
		j /= 2
		r.recompute(level, j)
	}
}

func (r *v2TestRawTree) root() ([32]byte, uint256.Int) {
	root := r.levels[len(r.levels)-1][0]
	return root.Hash, root.Sum
}

// path returns the top-down preimages above index, as LedgerSumTree.Path.
func (r *v2TestRawTree) path(index uint64) []merkle.LedgerPathNode {
	depth := len(r.levels) - 1
	path := make([]merkle.LedgerPathNode, depth)
	for i := range depth {
		level := depth - i
		j := index >> level
		left, right := r.levels[level-1][2*j], r.levels[level-1][2*j+1]
		path[i] = merkle.LedgerPathNode{LeftHash: left.Hash, LeftSum: left.Sum, RightHash: right.Hash, RightSum: right.Sum}
	}
	return path
}

// requireV2Tx checks a sent transaction's sender, nonce, fee, gas, value and
// calldata, signed for the fixture's chain as a legacy transaction.
func requireV2Tx(t *testing.T, tx *types.Transaction, nonce uint64, gas uint64, value int64, calldata []byte) {
	t.Helper()
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(v2TestChainID)), tx)
	if err != nil || sender != common.HexToAddress(testKeyAddr) || tx.Type() != types.LegacyTxType || tx.To() == nil || *tx.To() != common.HexToAddress(v2TestVault) ||
		tx.Nonce() != nonce || tx.GasPrice().Cmp(big.NewInt(2)) != 0 || tx.Gas() != gas || tx.Value().Cmp(big.NewInt(value)) != 0 || !bytes.Equal(tx.Data(), calldata) {
		t.Fatalf("signed the wrong transaction: sender %s (%v) type %d to %v nonce %d gasPrice %s gas %d value %s data 0x%x, want nonce %d gas %d value %d data 0x%x",
			sender, err, tx.Type(), tx.To(), tx.Nonce(), tx.GasPrice(), tx.Gas(), tx.Value(), tx.Data(), nonce, gas, value, calldata)
	}
}

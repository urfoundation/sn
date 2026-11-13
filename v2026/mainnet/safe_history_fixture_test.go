// Synthetic archive fixtures use independently encoded SCALE/RLP headers and
// geth's indexed tries. No fixture talks to a live chain or creates live keys.
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	substrate "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"golang.org/x/crypto/blake2b"
)

// Fault callbacks run after releasing the counter lock and may cancel a read.
// Witness maps are immutable while a command runs, including under the race tool.
type safeHistoryFixture struct {
	scope     safeHistoryScope
	expected  identityExpectation
	witnesses []safeHistoryWitness
	advance   bool
	stateLock sync.Mutex
	counts    map[string]int
	fault     func(*http.Request, string, int, any) (any, error)
}

// The independent SCALE encoder hashes header fields and exact digest bytes.
func safeHistoryTestSealNative(t *testing.T, witness *safeHistoryWitness) {
	t.Helper()
	header := witness.NativeHeader
	parent, _ := substrate.NewHashFromHexString(header.ParentHash)
	state, _ := substrate.NewHashFromHexString(header.StateRoot)
	body, _ := substrate.NewHashFromHexString(header.ExtrinsicsRoot)
	raw, err := codec.Encode(substrate.Header{ParentHash: parent, Number: substrate.BlockNumber(witness.Native.Number), StateRoot: state, ExtrinsicsRoot: body})
	if err != nil {
		t.Fatal(err)
	}
	count, err := codec.Encode(substrate.NewUCompactFromUInt(uint64(len(header.Digest.Logs))))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], count...)
	for _, encoded := range header.Digest.Logs {
		log, err := hex.DecodeString(encoded[2:])
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, log...)
	}
	digest := blake2b.Sum256(raw)
	witness.Native.Hash = "0x" + hex.EncodeToString(digest[:])
}

// Geth's independent trie builder commits every raw typed transaction/receipt.
// Native and EVM heights deliberately differ; foreign traffic stays in both.
func newSafeHistoryFixture(t *testing.T) (*rpcClient, *safeHistoryFixture) {
	t.Helper()
	fixture := &safeHistoryFixture{expected: identityExpectation{NativeChain: "synthetic-chain", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId}, counts: map[string]int{}}
	safe := common.HexToAddress("0x1234567890123456789012345678901234567890")
	foreign := common.HexToAddress("0x2345678901234567890123456789012345678901")
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte("synthetic archive census signer")))
	if err != nil {
		t.Fatal(err)
	}
	nativeParent := testGenesisHash
	evmParent := common.HexToHash("0x" + strings.Repeat("ac", 32))
	for blockIndex := 0; blockIndex < 3; blockIndex++ {
		transactions := types.Transactions{}
		receipts := types.Receipts{}
		hashes := []string{}
		for index := 0; index < 3; index++ {
			to, status := foreign, uint64(types.ReceiptStatusSuccessful)
			if index == 1 {
				to, status = safe, types.ReceiptStatusFailed
			}
			transaction := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(mainnetEvmChainId), Nonce: uint64(blockIndex*3 + index),
				To: &to, Gas: 100000, GasFeeCap: big.NewInt(2), GasTipCap: big.NewInt(1), Value: new(big.Int), Data: []byte{byte(index), byte(blockIndex)}})
			transaction, err = types.SignTx(transaction, types.LatestSignerForChainID(big.NewInt(mainnetEvmChainId)), key)
			if err != nil {
				t.Fatal(err)
			}
			logs := []*types.Log{}
			if index == 2 {
				logs = append(logs, &types.Log{Address: safe, Topics: []common.Hash{crypto.Keccak256Hash([]byte("synthetic unknown event"))}, Data: []byte{7, 8, 9}})
			}
			transactions = append(transactions, transaction)
			receipts = append(receipts, &types.Receipt{Type: transaction.Type(), Status: status, CumulativeGasUsed: uint64(index+1) * 23000, Logs: logs})
			hashes = append(hashes, transaction.Hash().Hex())
		}
		header := &types.Header{ParentHash: evmParent, UncleHash: types.EmptyUncleHash, Root: common.HexToHash("0x" + strings.Repeat("cd", 32)),
			TxHash: types.DeriveSha(transactions, trie.NewStackTrie(nil)), ReceiptHash: types.DeriveSha(receipts, trie.NewStackTrie(nil)),
			Number: big.NewInt(int64(37 + blockIndex)), Difficulty: new(big.Int), GasLimit: 75000000, GasUsed: 69000, Time: uint64(1700000000001 + blockIndex*12000)}
		rawHeader, err := rlp.EncodeToBytes(header)
		if err != nil {
			t.Fatal(err)
		}
		rawBlock, err := rlp.EncodeToBytes(types.NewBlockWithHeader(header).WithBody(types.Body{Transactions: transactions}))
		if err != nil {
			t.Fatal(err)
		}
		witness := safeHistoryWitness{Native: safeHistoryBoundary{Number: uint64(100 + blockIndex)}, EvmHeader: "0x" + hex.EncodeToString(rawHeader), EvmBlock: "0x" + hex.EncodeToString(rawBlock), Extrinsics: []string{}, EvmReceipts: []string{}}
		for _, receipt := range receipts {
			raw, err := receipt.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			witness.EvmReceipts = append(witness.EvmReceipts, "0x"+hex.EncodeToString(raw))
		}
		body := [][]byte{}
		for index := 0; index < 2; index++ {
			// Opaque native calls are retained, never semantically whitelisted.
			payload := []byte{4, byte(30 + index), 0, byte(blockIndex)}
			prefix, err := codec.Encode(substrate.NewUCompactFromUInt(uint64(len(payload))))
			if err != nil {
				t.Fatal(err)
			}
			raw := append(prefix, payload...)
			body = append(body, raw)
			witness.Extrinsics = append(witness.Extrinsics, "0x"+hex.EncodeToString(raw))
		}
		bodyRoot, err := rootExtrinsicsRoot(body, 0)
		if err != nil {
			t.Fatal(err)
		}
		witness.NativeHeader = rootReceiptHeader{ParentHash: nativeParent, Number: fmt.Sprintf("0x%x", witness.Native.Number), StateRoot: "0x" + strings.Repeat("ab", 32), ExtrinsicsRoot: bodyRoot}
		variant := byte(1)
		if blockIndex == 1 {
			variant = 3
		}
		witness.NativeHeader.Digest.Logs = []string{mappingTestDigest(t, variant, header.Hash().Hex(), hashes)}
		safeHistoryTestSealNative(t, &witness)
		fixture.witnesses = append(fixture.witnesses, witness)
		nativeParent, evmParent = witness.Native.Hash, header.Hash()
	}
	fixture.scope = safeHistoryScope{Safe: strings.ToLower(safe.Hex()), From: fixture.witnesses[0].Native, Through: fixture.witnesses[1].Native}
	client, err := newRpcClient("http://rpc.example", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = roundTripFunc(fixture.roundTrip)
	return client, fixture
}

// Exact hash selectors and the read-only method census are enforced by the
// transport independently of the production verifier's derived projection.
func (self *safeHistoryFixture) roundTrip(request *http.Request) (*http.Response, error) {
	var call struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		return nil, err
	}
	self.stateLock.Lock()
	self.counts[call.Method]++
	count := self.counts[call.Method]
	self.stateLock.Unlock()
	var result any
	switch call.Method {
	case "system_chain":
		result = self.expected.NativeChain
	case "eth_chainId":
		result = "0x3c4"
	case "chain_getFinalizedHead":
		index := min(1, len(self.witnesses)-1)
		if self.advance && count > 1 {
			index = 2
		}
		result = self.witnesses[index].Native.Hash
	case "chain_getBlockHash":
		number := uint64(call.Params[0].(float64))
		if number == 0 {
			result = self.expected.GenesisHash
		}
		for _, witness := range self.witnesses {
			if witness.Native.Number == number {
				result = witness.Native.Hash
			}
		}
	case "chain_getHeader", "chain_getBlock":
		for _, witness := range self.witnesses {
			if len(call.Params) == 1 && witness.Native.Hash == call.Params[0] {
				result = witness.NativeHeader
				if call.Method == "chain_getBlock" {
					result = map[string]any{"block": map[string]any{"header": witness.NativeHeader, "extrinsics": witness.Extrinsics}}
				}
			}
		}
	case "debug_getRawHeader", "debug_getRawBlock", "debug_getRawReceipts":
		selector, ok := call.Params[0].(map[string]any)
		if !ok || len(selector) != 2 || selector["requireCanonical"] != true {
			return nil, fmt.Errorf("archive fixture received an unpinned raw selector")
		}
		for _, witness := range self.witnesses {
			raw, _ := hex.DecodeString(witness.EvmHeader[2:])
			if crypto.Keccak256Hash(raw).Hex() == selector["blockHash"] {
				switch call.Method {
				case "debug_getRawHeader":
					result = witness.EvmHeader
				case "debug_getRawBlock":
					result = witness.EvmBlock
				case "debug_getRawReceipts":
					result = witness.EvmReceipts
				}
			}
		}
	default:
		return nil, fmt.Errorf("archive fixture refused method %s", call.Method)
	}
	if result == nil {
		return nil, fmt.Errorf("archive fixture has no exact selection for %s", call.Method)
	}
	if self.fault != nil {
		var err error
		result, err = self.fault(request, call.Method, count, result)
		if err != nil {
			return nil, err
		}
	}
	envelope := map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}
	if failure, ok := result.(mappingFixtureRpcError); ok {
		delete(envelope, "result")
		envelope["error"] = map[string]any{"code": failure.code, "message": "synthetic unsupported archive method"}
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
}

// The local contract fixture executes reviewed creation bytecode in geth's EVM.
// Synthetic owned-RPC headers model Frontier's independent native/EVM clocks.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	native "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/urfoundation/sn/crv4"
	"golang.org/x/crypto/blake2b"
)

// Read the maintained reviewed catalog directly, without copying bytecode into
// another fixture or importing the simulator's package-main transaction manager.
func evmTestRelease(t *testing.T) contractReleaseArtifacts {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../sim-testnet/contracts_gen.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]string{}
	var entries *ast.CompositeLit
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			if literal, ok := value.Values[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
				decoded, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				constants[value.Names[0].Name] = decoded
			}
			if value.Names[0].Name == "ReleaseContractArtifacts" {
				entries = value.Values[0].(*ast.CompositeLit)
			}
		}
	}
	if entries == nil {
		t.Fatal("reviewed release artifact projection is missing")
	}
	stringValue := func(expression ast.Expr) string {
		if id, ok := expression.(*ast.Ident); ok {
			return constants[id.Name]
		}
		value, err := strconv.Unquote(expression.(*ast.BasicLit).Value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	result := contractReleaseArtifacts{Schema: "urnetwork-contract-release-artifacts-v1"}
	for _, entry := range entries.Elts {
		artifact := contractReleaseArtifact{ImmutableReferences: map[string][]int{}}
		fields := map[string]*string{"Name": &artifact.Name, "ABI": &artifact.Abi, "CreationBytecode": &artifact.Creation, "RuntimeBytecode": &artifact.Runtime, "RuntimeBytecodeHash": &artifact.RuntimeHash, "FoundryArtifactHash": &artifact.ArtifactHash, "StorageLayoutHash": &artifact.StorageLayoutHash}
		for _, field := range entry.(*ast.CompositeLit).Elts {
			pair := field.(*ast.KeyValueExpr)
			name := pair.Key.(*ast.Ident).Name
			if target, ok := fields[name]; ok {
				*target = stringValue(pair.Value)
				continue
			}
			if name != "ImmutableReferences" {
				t.Fatalf("unknown reviewed artifact field %s", name)
			}
			for _, reference := range pair.Value.(*ast.CompositeLit).Elts {
				pair := reference.(*ast.KeyValueExpr)
				key := stringValue(pair.Key)
				for _, offset := range pair.Value.(*ast.CompositeLit).Elts {
					value, err := strconv.Atoi(offset.(*ast.BasicLit).Value)
					if err != nil {
						t.Fatal(err)
					}
					artifact.ImmutableReferences[key] = append(artifact.ImmutableReferences[key], value)
				}
			}
		}
		result.Artifacts = append(result.Artifacts, artifact)
	}
	return result
}

// Native header commitments are encoded independently through the SCALE codec.
func evmTestNativeHeader(t *testing.T, parent string, number uint64, logs []string) (rootReceiptHeader, string) {
	t.Helper()
	parentHash, err := native.NewHashFromHexString(parent)
	if err != nil {
		t.Fatal(err)
	}
	stateHash := native.Hash{4}
	bodyHash := native.Hash{5}
	header := native.Header{ParentHash: parentHash, Number: native.BlockNumber(number), StateRoot: stateHash, ExtrinsicsRoot: bodyHash}
	raw, err := codec.Encode(header)
	if err != nil {
		t.Fatal(err)
	}
	count, err := codec.Encode(native.NewUCompactFromUInt(uint64(len(logs))))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], count...)
	for _, log := range logs {
		value, err := hex.DecodeString(log[2:])
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, value...)
	}
	digest := blake2b.Sum256(raw)
	result := rootReceiptHeader{ParentHash: parent, Number: fmt.Sprintf("0x%x", number), StateRoot: stateHash.Hex(), ExtrinsicsRoot: bodyHash.Hex()}
	result.Digest.Logs = logs
	return result, "0x" + hex.EncodeToString(digest[:])
}

// HTTP fixture mutation and counters share one lock. Tests install faults only
// between completed commands or through a deterministic barrier callback.
type evmCreateFixture struct {
	stateLock  sync.Mutex
	t          *testing.T
	server     *httptest.Server
	config     evmPhaseConfig
	configPath string
	signedPath string
	signedHash string
	plan       evmCreatePlan
	tx         *types.Transaction
	raw        []byte
	head       uint64
	headers    map[string]rootReceiptHeader
	hashes     map[uint64]string
	evmHeaders map[string]*types.Header
	rawHeaders map[string]string
	metadata   []byte
	code       []byte
	storageKey string
	state      *state.StateDB
	vm         runtime.Config
	receipt    map[string]any
	writes     [][]byte
	counts     map[string]int
	mine       bool
	loseReply  bool
	gasFailure bool
	override   func(string, []any, any) any
	history    *evmCreateHistory
}

// Two-action fixtures retain real execution state at each inclusion hash. The
// legacy one-action fixture leaves this nil and retains its original behavior.
type evmCreateHistory struct {
	receipts     map[string]map[string]any
	transactions map[string]*types.Transaction
	states       map[string]*state.StateDB
	storage      map[string]map[string]string
}

// All keys, approvals and balances are synthetic and stay inside this fixture.
func newEvmCreateFixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(directory, "release.json")
	artifacts := evmTestRelease(t)
	artifactRaw, err := json.Marshal(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, artifactRaw, 0600); err != nil {
		t.Fatal(err)
	}
	f := &evmCreateFixture{t: t, head: 100, headers: map[string]rootReceiptHeader{}, hashes: map[uint64]string{}, evmHeaders: map[string]*types.Header{}, rawHeaders: map[string]string{}, counts: map[string]int{}, mine: true, code: []byte{0, 97, 115, 109, 1, 0, 0, 0}}
	metadata := native.NewMetadataV14()
	metadata.MagicNumber = native.MagicNumber
	metadata.AsMetadataV14.Pallets = []native.PalletMetadataV14{{Name: "Ethereum", HasStorage: true, Storage: native.StorageMetadataV14{Prefix: "Ethereum", Items: []native.StorageEntryMetadataV14{{Name: "BlockHash", Modifier: native.StorageFunctionModifierV0{IsDefault: true}, Type: native.StorageEntryTypeV14{IsMap: true, AsMap: native.MapTypeV14{Hashers: []native.StorageHasherV10{{IsTwox64Concat: true}}}}}}}}}
	f.metadata, err = codec.Encode(metadata)
	if err != nil {
		t.Fatal(err)
	}
	arg := make([]byte, 32)
	binary.LittleEndian.PutUint64(arg, 38)
	key, err := native.CreateStorageKey(metadata, "Ethereum", "BlockHash", arg)
	if err != nil {
		t.Fatal(err)
	}
	f.storageKey = key.Hex()
	_, mapping := newFinalizedMappingFixture(t, 1, 0)
	f.evmHeaders[mapping.evmHash], f.rawHeaders[mapping.evmHash] = mapping.evmHeader, mapping.rawEvmHeader
	parent, parentHash := evmTestNativeHeader(t, testGenesisHash, 99, []string{})
	f.headers[parentHash], f.hashes[99] = parent, parentHash
	head, headHash := evmTestNativeHeader(t, parentHash, 100, []string{mappingTestDigest(t, 1, mapping.evmHash, []string{})})
	f.headers[headHash], f.hashes[100] = head, headHash
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	private, err := crypto.HexToECDSA(strings.Repeat("17", 32))
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(private.PublicKey)
	var reserve contractReleaseArtifact
	for _, artifact := range artifacts.Artifacts {
		if artifact.Name == "ReserveSink" {
			reserve = artifact
		}
	}
	hotkey := [32]byte{31}
	data, _, _, err := contractReservePayload(reserve, 25, hotkey, sender, 0)
	if err != nil {
		t.Fatal(err)
	}
	artifactDigest := sha256.Sum256(artifactRaw)
	codeDigest := blake2b.Sum256(f.code)
	metadataDigest := blake2b.Sum256(f.metadata)
	f.config = evmPhaseConfig{Schema: evmPhaseConfigSchema, Plan: evmPhasePlan{Schema: evmPhaseSchema, DeploymentId: "synthetic-install", Network: planNetwork{NativeChain: "synthetic-chain", GenesisHash: testGenesisHash, EvmChainId: 964}, Runtime: rootReceiptProfile{RuntimeSourceCommit: frontierMappingSourceCommit, RuntimeVersion: crv4.RuntimeVersionIdentity{SpecName: "synthetic-runtime", SpecVersion: 991, TransactionVersion: 1, StateVersion: 1}, RuntimeCodeHash: "0x" + hex.EncodeToString(codeDigest[:]), RuntimeMetadataHash: "0x" + hex.EncodeToString(metadataDigest[:])}, Route: ownedSubmissionRoute{RpcUrl: f.server.URL, ReadRetrySeconds: 60, SendTimeoutSeconds: 1}, RunDirectory: directory, Artifacts: planFileReference{Path: artifactPath, Sha256: "sha256:" + hex.EncodeToString(artifactDigest[:])}, SourceLockHash: "sha256:" + strings.Repeat("10", 32), CustodyId: "synthetic-exclusive-deployer", CustodyFenceHash: "sha256:" + strings.Repeat("11", 32), CutoverEvidenceHash: "sha256:" + strings.Repeat("12", 32), StartNativeNumber: 100, StartNativeHash: headHash, ValidThroughNative: 110, MaximumAttempts: 2, MaximumTotalWei: "20000000", Netuid: 25, ReserveHotkey: "0x" + hex.EncodeToString(hotkey[:]), Actions: []evmPhaseAction{{Id: "reserve-create", Sender: sender, Nonce: 0, Data: "0x" + hex.EncodeToString(data), ValueWei: "0", Gas: 2_000_000, FeeCapWei: "10", TipCapWei: "1"}}}}
	f.configPath = filepath.Join(directory, "phase.json")
	f.publishConfig()
	unsigned, err := f.config.Plan.Actions[0].unsigned()
	if err != nil {
		t.Fatal(err)
	}
	f.tx, err = types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(964)), private)
	if err != nil {
		t.Fatal(err)
	}
	f.raw, err = f.tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	f.signedPath = filepath.Join(directory, "signed.bin")
	if err := os.WriteFile(f.signedPath, f.raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(f.raw)
	f.signedHash = "sha256:" + hex.EncodeToString(digest[:])
	f.state, err = state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	if err != nil {
		t.Fatal(err)
	}
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = big.NewInt(964)
	f.vm = runtime.Config{ChainConfig: &chainConfig, State: f.state, Origin: sender, BlockNumber: big.NewInt(38), GasLimit: 2_000_000, GasPrice: big.NewInt(2), Value: big.NewInt(0), BaseFee: big.NewInt(1)}
	f.state.SetNonce(sender, 0, tracing.NonceChangeUnspecified)
	return f
}

// Re-sign fixture approvals only; production never gains an approval private key.
func (self *evmCreateFixture) publishConfig() {
	self.t.Helper()
	seed := sha256.Sum256([]byte("synthetic contract independent approval"))
	key := ed25519.NewKeyFromSeed(seed[:])
	self.config.ApprovalPublicKey = "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))
	message, err := self.config.Plan.signingBytes()
	if err != nil {
		self.t.Fatal(err)
	}
	self.config.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	raw, err := json.Marshal(self.config)
	if err != nil {
		self.t.Fatal(err)
	}
	if err := os.WriteFile(self.configPath, raw, 0600); err != nil {
		self.t.Fatal(err)
	}
	self.plan, err = loadEvmCreatePlan(context.Background(), self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
}

// Every test reaches the actual top-level command dispatcher.
func (self *evmCreateFixture) command(command string, extra ...string) (evmCreateResult, int, string) {
	self.t.Helper()
	args := []string{"bootstrap-contracts", command, "--config", self.configPath}
	if command != "plan" {
		args = append(args, "--run-dir", self.config.Plan.RunDirectory, "--accept-plan-hash", self.config.Plan.hash())
	}
	args = append(args, extra...)
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), args, &stdout, &stderr)
	var result evmCreateResult
	if code == 0 && command != "plan" {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			self.t.Fatal(err)
		}
	}
	return result, code, stderr.String()
}

// A transaction executes the actual reviewed constructor, after exact custody
// validation, and returns live EVM code/getter results rather than canned bytes.
func (self *evmCreateFixture) execute() error {
	nextEvm := uint64(0)
	var parentHash string
	for hash, header := range self.evmHeaders {
		if number := header.Number.Uint64(); number >= nextEvm {
			nextEvm = number + 1
			parentHash = hash
		}
	}
	self.vm.BlockNumber = new(big.Int).SetUint64(nextEvm)
	code, address, left, err := runtime.Create(self.tx.Data(), &self.vm)
	if self.gasFailure {
		if !errors.Is(err, vm.ErrOutOfGas) && !errors.Is(err, vm.ErrCodeStoreOutOfGas) {
			return fmt.Errorf("expected genuine CREATE gas failure: %w", err)
		}
		if len(self.state.GetCode(self.plan.Address)) != 0 || self.state.GetNonce(self.config.Plan.Actions[0].Sender) != self.tx.Nonce()+1 {
			return errors.New("failed CREATE did not consume only its original nonce")
		}
	} else if err != nil {
		return err
	} else if address != self.plan.Address || !bytes.Equal(code, self.plan.Runtime) {
		return fmt.Errorf("genuine EVM constructor differs from approved immutable projection")
	}
	gasUsed := self.vm.GasLimit - left
	header := &types.Header{ParentHash: common.HexToHash(parentHash), UncleHash: types.EmptyUncleHash, Root: self.state.IntermediateRoot(true), TxHash: types.DeriveSha(types.Transactions{self.tx}, trie.NewStackTrie(nil)), ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: new(big.Int).SetUint64(nextEvm), GasLimit: 75_000_000, GasUsed: gasUsed, Time: 1700000001001}
	raw, err := rlp.EncodeToBytes(header)
	if err != nil {
		return err
	}
	hash := header.Hash().Hex()
	self.evmHeaders[hash], self.rawHeaders[hash] = header, "0x"+hex.EncodeToString(raw)
	nativeHeader, nativeHash := evmTestNativeHeader(self.t, self.hashes[self.head], self.head+1, []string{mappingTestDigest(self.t, 1, hash, []string{self.tx.Hash().Hex()})})
	self.headers[nativeHash], self.hashes[self.head+1], self.head = nativeHeader, nativeHash, self.head+1
	metadata, _, decodeErr := crv4.DecodeRuntimeMetadata("0x" + hex.EncodeToString(self.metadata))
	if decodeErr != nil {
		return decodeErr
	}
	arg := make([]byte, 32)
	binary.LittleEndian.PutUint64(arg, nextEvm)
	key, keyErr := native.CreateStorageKey(metadata, "Ethereum", "BlockHash", arg)
	if keyErr != nil {
		return keyErr
	}
	self.storageKey = key.Hex()
	self.receipt = map[string]any{"transactionHash": self.tx.Hash().Hex(), "blockHash": hash, "blockNumber": fmt.Sprintf("0x%x", nextEvm), "transactionIndex": "0x0", "status": "0x1", "gasUsed": fmt.Sprintf("0x%x", gasUsed), "effectiveGasPrice": "0x2", "contractAddress": strings.ToLower(address.Hex())}
	if self.gasFailure {
		self.receipt["status"], self.receipt["contractAddress"] = "0x0", nil
	}
	if self.history != nil {
		self.history.receipts[self.tx.Hash().Hex()] = self.receipt
		self.history.transactions[hash] = self.tx
		self.history.states[hash] = self.state.Copy()
		self.history.storage[self.storageKey] = map[string]string{nativeHash: hash}
	}
	return nil
}

// Find the first native commitment of this synthetic receipt, even after the
// fixture advances to another runtime. No equality of the two heights is used.
func (self *evmCreateFixture) receiptNativeNumber() string {
	if self.receipt == nil {
		return ""
	}
	for number := uint64(100); number <= self.head; number++ {
		header := self.headers[self.hashes[number]]
		post, err := finalizedFrontierPostLog(header)
		if err == nil && post.BlockHash == self.receipt["blockHash"] {
			return header.Number
		}
	}
	return ""
}

// Advance a healthy empty block during a read barrier without changing custody,
// original transaction bytes, sender nonce or current approved runtime.
func (self *evmCreateFixture) advanceEmpty() {
	next := uint64(0)
	var parentHash string
	for hash, header := range self.evmHeaders {
		if number := header.Number.Uint64(); number >= next {
			next = number + 1
			parentHash = hash
		}
	}
	header := &types.Header{ParentHash: common.HexToHash(parentHash), UncleHash: types.EmptyUncleHash, Root: types.EmptyRootHash, TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: new(big.Int).SetUint64(next), GasLimit: 75_000_000, Time: 1700000000000 + next}
	raw, err := rlp.EncodeToBytes(header)
	if err != nil {
		self.t.Fatal(err)
	}
	hash := header.Hash().Hex()
	self.evmHeaders[hash], self.rawHeaders[hash] = header, "0x"+hex.EncodeToString(raw)
	nativeHeader, nativeHash := evmTestNativeHeader(self.t, self.hashes[self.head], self.head+1, []string{mappingTestDigest(self.t, 1, hash, []string{})})
	self.headers[nativeHash], self.hashes[self.head+1], self.head = nativeHeader, nativeHash, self.head+1
	if self.history != nil {
		self.history.states[hash] = self.state.Copy()
	}
}

// Synchronous HTTP dispatch is the only place that executes or mutates the EVM.
func (self *evmCreateFixture) serve(writer http.ResponseWriter, request *http.Request) {
	var call struct {
		Method string `json:"method"`
		Params []any  `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		http.Error(writer, err.Error(), 400)
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.counts[call.Method]++
	var result any
	switch call.Method {
	case "system_chain":
		result = "synthetic-chain"
	case "system_version":
		result = "synthetic-node"
	case "eth_chainId":
		result = "0x3c4"
	case "chain_getFinalizedHead":
		result = self.hashes[self.head]
	case "chain_getBlockHash":
		number := uint64(call.Params[0].(float64))
		if number == 0 {
			result = testGenesisHash
		} else {
			result = self.hashes[number]
		}
	case "chain_getHeader":
		result = self.headers[call.Params[0].(string)]
	case "state_getRuntimeVersion":
		result = self.config.Plan.Runtime.RuntimeVersion
	case "state_getMetadata":
		result = "0x" + hex.EncodeToString(self.metadata)
	case "state_getStorageHash":
		digest := blake2b.Sum256(self.code)
		result = "0x" + hex.EncodeToString(digest[:])
	case "state_getStorage":
		if call.Params[0] == runtimeCodeStorageKey {
			result = "0x" + hex.EncodeToString(self.code)
		} else if self.history != nil {
			if value := self.history.storage[call.Params[0].(string)][call.Params[1].(string)]; value != "" {
				result = value
			}
		} else if call.Params[0] == self.storageKey && self.receipt != nil && self.headers[call.Params[1].(string)].Number == self.receiptNativeNumber() {
			result = self.receipt["blockHash"]
		} else if call.Params[0] != self.storageKey {
			http.Error(writer, "unexpected storage key", 400)
			return
		}
	case "debug_getRawHeader":
		selector := call.Params[0].(map[string]any)
		if selector["requireCanonical"] != true {
			http.Error(writer, "unfenced raw header", 400)
			return
		}
		result = self.rawHeaders[selector["blockHash"].(string)]
	case "eth_getBlockByNumber":
		for hash, header := range self.evmHeaders {
			if fmt.Sprintf("0x%x", header.Number) == call.Params[0] {
				hashes := []string{}
				if self.receipt != nil && hash == self.receipt["blockHash"] {
					hashes = append(hashes, self.tx.Hash().Hex())
				}
				if self.history != nil {
					hashes = []string{}
					if transaction := self.history.transactions[hash]; transaction != nil {
						hashes = append(hashes, transaction.Hash().Hex())
					}
				}
				result = map[string]any{"hash": hash, "number": call.Params[0], "transactions": hashes}
			}
		}
	case "eth_getTransactionReceipt":
		result = self.receipt
		if self.history != nil {
			result = self.history.receipts[call.Params[0].(string)]
		}
	case "eth_getTransactionByBlockHashAndIndex":
		if self.receipt != nil && call.Params[0] == self.receipt["blockHash"] && call.Params[1] == "0x0" {
			result = self.tx
		}
		if self.history != nil && call.Params[1] == "0x0" {
			result = self.history.transactions[call.Params[0].(string)]
		}
	case "eth_getTransactionCount":
		result = fmt.Sprintf("0x%x", self.state.GetNonce(self.config.Plan.Actions[0].Sender))
		if self.history != nil {
			observed := self.historicalState(call.Params[1])
			result = fmt.Sprintf("0x%x", observed.GetNonce(common.HexToAddress(call.Params[0].(string))))
		}
	case "eth_getBalance":
		result = "0xffffffffffff"
	case "eth_getCode":
		result = "0x" + hex.EncodeToString(self.state.GetCode(self.plan.Address))
		if self.history != nil {
			observed := self.historicalState(call.Params[1])
			result = "0x" + hex.EncodeToString(observed.GetCode(common.HexToAddress(call.Params[0].(string))))
		}
	case "eth_call":
		input := call.Params[0].(map[string]any)
		data, err := hex.DecodeString(input["data"].(string)[2:])
		if err != nil {
			http.Error(writer, err.Error(), 400)
			return
		}
		config := &self.vm
		if self.history != nil {
			historical := self.vm
			historical.State = self.historicalState(call.Params[1]).Copy()
			config = &historical
		}
		value, _, err := runtime.Call(common.HexToAddress(input["to"].(string)), data, config)
		if err != nil {
			http.Error(writer, err.Error(), 400)
			return
		}
		result = "0x" + hex.EncodeToString(value)
	case "eth_sendRawTransaction":
		raw, err := hex.DecodeString(call.Params[0].(string)[2:])
		if err != nil || !bytes.Equal(raw, self.raw) {
			http.Error(writer, "not original signed bytes", 400)
			return
		}
		self.writes = append(self.writes, raw)
		if self.mine && self.receipt == nil {
			if err := self.execute(); err != nil {
				http.Error(writer, err.Error(), 400)
				return
			}
		}
		if self.loseReply {
			http.Error(writer, "synthetic lost acknowledgement", 503)
			return
		}
		result = self.tx.Hash().Hex()
	default:
		http.Error(writer, "unexpected RPC method", 400)
		return
	}
	if self.override != nil {
		result = self.override(call.Method, call.Params, result)
	}
	reply := map[string]any{"jsonrpc": "2.0", "id": 1, "result": result}
	if fault, ok := result.(mappingFixtureRpcError); ok {
		delete(reply, "result")
		reply["error"] = map[string]any{"code": fault.code, "message": "synthetic historical read unavailable"}
	}
	_ = json.NewEncoder(writer).Encode(reply)
}

// Exact canonical selectors read the captured EVM state; pending reads use the
// active state. Unknown historical hashes fail the fixture instead of guessing.
func (self *evmCreateFixture) historicalState(selector any) *state.StateDB {
	if block, ok := selector.(map[string]any); ok {
		if block["requireCanonical"] != true || self.history.states[block["blockHash"].(string)] == nil {
			panic("synthetic EVM historical selector is not canonical or known")
		}
		return self.history.states[block["blockHash"].(string)]
	}
	if selector != "pending" {
		panic("synthetic EVM state selector is not pending")
	}
	return self.state
}

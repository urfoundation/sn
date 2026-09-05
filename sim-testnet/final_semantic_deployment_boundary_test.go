package main

// Deployment observations use separate release and coordinator-history
// boundaries. These fixtures force crash/resume and malformed evidence without
// timing, a live chain, or a transaction-capable endpoint.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Reads deterministic finalized blocks, CREATE receipts, and code. The same
// state backs direct reader tests and the production executor's HTTP client.
type deploymentBoundaryFixture struct {
	finalized          ChainHead
	blockHeads         map[uint64]ChainHead
	receipts           map[common.Hash]*types.Receipt
	transactions       map[common.Hash]*types.Transaction
	pending            bool
	codes              map[common.Address][]byte
	codeError          error
	unexpectedRequests atomic.Int64
}

// Returns the explicit RPC hash, including on chains with synthetic headers.
func (self *deploymentBoundaryFixture) EVMBlockByNumber(_ context.Context, number *big.Int) (ChainHead, error) {
	if number != nil && number.IsInt64() && number.Int64() == -3 {
		return self.finalized, nil
	}
	if number == nil || !number.IsUint64() {
		return ChainHead{}, errors.New("invalid fixture block")
	}
	if head, found := self.blockHeads[number.Uint64()]; found {
		return head, nil
	}
	return ChainHead{}, ethereum.NotFound
}

// A missing receipt is an authoritative missing-evidence result.
func (self *deploymentBoundaryFixture) TransactionReceipt(_ context.Context, hash common.Hash) (*types.Receipt, error) {
	if receipt, found := self.receipts[hash]; found {
		return receipt, nil
	}
	return nil, ethereum.NotFound
}

// Returns the actual signed bytes whose hash is bound by the CREATE receipt.
func (self *deploymentBoundaryFixture) TransactionByHash(_ context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	if transaction, found := self.transactions[hash]; found {
		return transaction, self.pending, nil
	}
	return nil, false, ethereum.NotFound
}

// Code observations must use the same finalized height as the receipt audit.
func (self *deploymentBoundaryFixture) CodeAt(_ context.Context, address common.Address, block *big.Int) ([]byte, error) {
	if block == nil || !block.IsUint64() || block.Uint64() != self.finalized.Number {
		return nil, errors.New("fixture code read is not finalized")
	}
	return self.codes[address], self.codeError
}

// Exposes only the read calls needed by the production resume path; any
// attempted fee estimate, nonce allocation, or send is counted and refused.
func (self *deploymentBoundaryFixture) client(t *testing.T) *ethclient.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		var result any
		var callErr error
		var selector string
		if len(call.Params) > 0 {
			callErr = json.Unmarshal(call.Params[0], &selector)
		}
		if callErr == nil {
			switch call.Method {
			case "eth_getBlockByNumber":
				number := big.NewInt(-3)
				if selector != "finalized" {
					value, err := strconv.ParseUint(strings.TrimPrefix(selector, "0x"), 16, 64)
					callErr = err
					number = new(big.Int).SetUint64(value)
				}
				if callErr == nil {
					var head ChainHead
					head, callErr = self.EVMBlockByNumber(request.Context(), number)
					result = map[string]any{"number": fmt.Sprintf("0x%x", head.Number), "hash": head.Hash}
				}
			case "eth_getTransactionReceipt":
				result, callErr = self.TransactionReceipt(request.Context(), common.HexToHash(selector))
			case "eth_getTransactionByHash":
				transaction, pending, err := self.TransactionByHash(request.Context(), common.HexToHash(selector))
				callErr = err
				if callErr == nil {
					encoded, err := json.Marshal(transaction)
					callErr = err
					if callErr == nil {
						var fields map[string]any
						callErr = json.Unmarshal(encoded, &fields)
						if !pending {
							receipt := self.receipts[common.HexToHash(selector)]
							fields["blockNumber"], fields["blockHash"] = fmt.Sprintf("0x%x", receipt.BlockNumber), receipt.BlockHash.Hex()
						}
						result = fields
					}
				}
			case "eth_getCode":
				var block string
				if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &block) != nil {
					callErr = errors.New("fixture code read has no block")
					break
				}
				value, err := strconv.ParseUint(strings.TrimPrefix(block, "0x"), 16, 64)
				callErr = err
				if callErr == nil {
					var code []byte
					code, callErr = self.CodeAt(request.Context(), common.HexToAddress(selector), new(big.Int).SetUint64(value))
					result = fmt.Sprintf("0x%x", code)
				}
			default:
				self.unexpectedRequests.Add(1)
				callErr = fmt.Errorf("unexpected RPC %s", call.Method)
			}
		}
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result}
		if callErr != nil {
			delete(response, "result")
			response["error"] = map[string]any{"code": -32000, "message": callErr.Error()}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
	}))
	t.Cleanup(server.Close)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

// Builds a later full-release checkpoint around one earlier proxy CREATE.
func deploymentBoundaryTestEvidence() (ContractDeployment, *SetupPlan, []JournalEntry, *deploymentBoundaryFixture) {
	key, _ := crypto.HexToECDSA(strings.Repeat("11", 32))
	signer := crypto.PubkeyToAddress(key.PublicKey)
	proxy := crypto.CreateAddress(signer, 4)
	transaction, _ := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: 4, GasPrice: big.NewInt(1), Gas: 100_000, Value: new(big.Int), Data: []byte{0x60, 0x01}}), types.LatestSignerForChainID(new(big.Int).SetUint64(testnetChainID)), key)
	proxyHead, releaseHead, finalized := testEVMHead(100, 0x10), testEVMHead(140, 0x14), testEVMHead(200, 0x20)
	code := []byte{0x60, 0x00}
	manifest := ContractDeployment{
		Schema: "urnetwork-contract-deployment-v1", DeploymentID: "boundary-fixture", CoordinatorProxy: proxy,
		DeployBlock: releaseHead.Number, DeployBlockHash: releaseHead.Hash,
		RuntimeHashes: map[string]string{proxy.Hex(): crypto.Keccak256Hash(code).Hex()},
	}
	action := Action{ID: "evm.coordinator-proxy", IntentHash: finalTestHex(0x31), Parameters: map[string]string{"expected_signer": signer.Hex(), "expected_nonce": "4", "expected_transaction_to": "create", "expected_created_address": proxy.Hex(), "expected_value_wei": "0", "expected_data_keccak256": crypto.Keccak256Hash(transaction.Data()).Hex()}}
	plan := &SetupPlan{DeploymentID: manifest.DeploymentID, PlanHash: finalTestHex(0x32), Deployment: contractDeploymentIdentity(manifest), Actions: []Action{action}}
	receipt := &types.Receipt{Status: types.ReceiptStatusSuccessful, TxHash: transaction.Hash(), BlockNumber: new(big.Int).SetUint64(proxyHead.Number), BlockHash: common.HexToHash(proxyHead.Hash), ContractAddress: proxy, Logs: []*types.Log{}}
	entries := []JournalEntry{{DeploymentID: manifest.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: receipt.TxHash.Hex(), BlockNumber: proxyHead.Number, BlockHash: proxyHead.Hash}}
	reader := &deploymentBoundaryFixture{finalized: finalized, blockHeads: map[uint64]ChainHead{proxyHead.Number: proxyHead, releaseHead.Number: releaseHead}, receipts: map[common.Hash]*types.Receipt{receipt.TxHash: receipt}, transactions: map[common.Hash]*types.Transaction{transaction.Hash(): transaction}, codes: map[common.Address][]byte{proxy: code}}
	return manifest, plan, entries, reader
}

// Pure payload-identity tests still exercise a real finalized empty-code read.
func undeployedBoundaryTestExecutor(t *testing.T, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, roles *RoleSecrets) *Executor {
	t.Helper()
	reader := &deploymentBoundaryFixture{finalized: testEVMHead(200, 0x20), blockHeads: map[uint64]ChainHead{}, receipts: map[common.Hash]*types.Receipt{}, codes: map[common.Address][]byte{}}
	return &Executor{cfg: cfg, stateDir: stateDir, plan: plan, roles: roles, journal: &Journal{}, deployer: &EVMTxManager{client: reader.client(t)}}
}

// The first coordinator event never moves when later CREATEs complete the
// graph, and disposable replacement probes change neither observation.
func TestFinalSemanticDeploymentBoundarySeparatesProxyHistoryFromReleaseGraph(t *testing.T) {
	manifest, _, entries, reader := deploymentBoundaryTestEvidence()
	manifest.DeployBlock, manifest.DeployBlockHash = 0, ""
	identityHash, _ := contractDeploymentIdentityHash(manifest)
	proxyReceipt := reader.receipts[common.HexToHash(entries[0].TransactionHash)]
	if err := recordContractDeploymentReceipt(&manifest, "evm.coordinator-proxy", proxyReceipt, false); err != nil {
		t.Fatal(err)
	}
	for index, actionID := range []string{"evm.governance-drill-implementation", "precompile.probe-deploy", "evm.coordinator-upgrade-implementation", "fleet.refresh.deploy-batcher"} {
		receipt := *proxyReceipt
		receipt.BlockNumber = big.NewInt(int64(110 + index*10))
		receipt.BlockHash = common.BigToHash(receipt.BlockNumber)
		receipt.ContractAddress = common.BigToAddress(big.NewInt(int64(10 + index)))
		if err := recordContractDeploymentReceipt(&manifest, actionID, &receipt, false); err != nil {
			t.Fatal(err)
		}
		if manifest.DeployBlock != receipt.BlockNumber.Uint64() || manifest.CoordinatorEventStartBlock != 100 || manifest.CoordinatorEventStartBlockHash != proxyReceipt.BlockHash.Hex() {
			t.Fatalf("%s changed the event boundary or missed the release boundary: %+v", actionID, manifest)
		}
	}
	beforeHash, _ := canonicalHashHex(manifest)
	replacement := *proxyReceipt
	replacement.BlockNumber, replacement.BlockHash = big.NewInt(150), common.HexToHash(finalTestHex(0x50))
	replacement.ContractAddress = common.HexToAddress("0x99")
	if err := recordContractDeploymentReceipt(&manifest, "precompile.probe-deploy", &replacement, true); err != nil {
		t.Fatal(err)
	}
	replacement.ContractAddress = common.Address{}
	if err := recordContractDeploymentReceipt(&manifest, "evm.vault-fix-coordinator", &replacement, false); err != nil {
		t.Fatal(err)
	}
	afterHash, _ := canonicalHashHex(manifest)
	afterIdentityHash, _ := contractDeploymentIdentityHash(manifest)
	if afterHash != beforeHash || afterIdentityHash != identityHash {
		t.Fatal("replacement probe, ordinary call, or mutable boundary changed immutable deployment identity")
	}
}

// Legacy state migrates once from its exact CREATE without moving the later
// release checkpoint or changing the immutable approval hash.
func TestFinalSemanticDeploymentBoundaryMigratesLegacyStateIdempotently(t *testing.T) {
	manifest, plan, entries, reader := deploymentBoundaryTestEvidence()
	beforeIdentityHash, _ := contractDeploymentIdentityHash(manifest)
	beforePlanHash, _ := plan.hash()
	for attempt := 0; attempt < 2; attempt++ {
		changed, err := reconcileContractDeploymentEventBoundary(context.Background(), reader, &manifest, entries, plan)
		if err != nil || changed != (attempt == 0) {
			t.Fatalf("migration %d changed=%t err=%v", attempt, changed, err)
		}
		if manifest.DeployBlock != 140 || manifest.DeployBlockHash != testEVMHead(140, 0x14).Hash || manifest.CoordinatorEventStartBlock != 100 || manifest.CoordinatorEventStartBlockHash != entries[0].BlockHash {
			t.Fatalf("migration lost either observation boundary: %+v", manifest)
		}
	}
	afterIdentityHash, _ := contractDeploymentIdentityHash(manifest)
	afterPlanHash, _ := plan.hash()
	if afterIdentityHash != beforeIdentityHash || afterPlanHash != beforePlanHash {
		t.Fatal("observation migration changed immutable deployment or plan identity")
	}
}

// Zero and partial observations both recover from the exact CREATE after a
// crash; a partial deployment before proxy creation remains resumable.
func TestFinalSemanticDeploymentBoundaryRecoversCrashWithoutSavedObservation(t *testing.T) {
	for _, releaseBlock := range []uint64{0, 90} {
		manifest, plan, entries, reader := deploymentBoundaryTestEvidence()
		manifest.DeployBlock, manifest.DeployBlockHash = releaseBlock, ""
		if releaseBlock != 0 {
			head := testEVMHead(releaseBlock, 0x09)
			manifest.DeployBlockHash = head.Hash
			reader.blockHeads[head.Number] = head
		}
		changed, err := reconcileContractDeploymentEventBoundary(context.Background(), reader, &manifest, entries, plan)
		if err != nil || !changed || manifest.DeployBlock != 100 || manifest.CoordinatorEventStartBlock != 100 {
			t.Fatalf("crash at release block %d: changed=%t manifest=%+v err=%v", releaseBlock, changed, manifest, err)
		}
	}
	manifest, plan, _, reader := deploymentBoundaryTestEvidence()
	manifest.DeployBlock, manifest.DeployBlockHash = 90, testEVMHead(90, 0x09).Hash
	reader.blockHeads[90] = testEVMHead(90, 0x09)
	delete(reader.codes, manifest.CoordinatorProxy)
	changed, err := reconcileContractDeploymentEventBoundary(context.Background(), reader, &manifest, nil, plan)
	if err != nil || changed || manifest.CoordinatorEventStartBlock != 0 || manifest.DeployBlock != 90 {
		t.Fatalf("pre-proxy partial deployment became unresumable: changed=%t manifest=%+v err=%v", changed, manifest, err)
	}
}

// Every ambiguity is rejected atomically even when the proxy code is already
// present. No later CREATE may use a guessed start height.
func TestFinalSemanticDeploymentBoundaryRejectsMissingForeignAndConflictingEvidence(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ContractDeployment, *SetupPlan, *[]JournalEntry, *deploymentBoundaryFixture)
	}{
		{name: "missing journal", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, _ *deploymentBoundaryFixture) {
			*entries = nil
		}},
		{name: "wrong deployment", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, _ *deploymentBoundaryFixture) {
			(*entries)[0].DeploymentID = "foreign"
		}},
		{name: "wrong plan", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, _ *deploymentBoundaryFixture) {
			(*entries)[0].PlanHash = finalTestHex(0x77)
		}},
		{name: "wrong intent", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, _ *deploymentBoundaryFixture) {
			(*entries)[0].IntentHash = finalTestHex(0x77)
		}},
		{name: "wrong action", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, _ *deploymentBoundaryFixture) {
			(*entries)[0].ActionID = "evm.settlement-vault"
		}},
		{name: "only included", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, _ *deploymentBoundaryFixture) {
			(*entries)[0].Stage = StageIncluded
		}},
		{name: "duplicate finalized", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, _ *deploymentBoundaryFixture) {
			*entries = append(*entries, (*entries)[0])
		}},
		{name: "conflicting finalized", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, _ *deploymentBoundaryFixture) {
			duplicate := (*entries)[0]
			duplicate.BlockNumber++
			*entries = append(*entries, duplicate)
		}},
		{name: "missing receipt", mutate: func(_ *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.receipts = nil
		}},
		{name: "missing signed transaction", mutate: func(_ *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.transactions = nil
		}},
		{name: "pending signed transaction", mutate: func(_ *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.pending = true
		}},
		{name: "foreign receipt", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.receipts[common.HexToHash((*entries)[0].TransactionHash)].ContractAddress = common.HexToAddress("0x99")
		}},
		{name: "reverted receipt", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.receipts[common.HexToHash((*entries)[0].TransactionHash)].Status = types.ReceiptStatusFailed
		}},
		{name: "mismatched transaction", mutate: func(_ *ContractDeployment, _ *SetupPlan, entries *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.receipts[common.HexToHash((*entries)[0].TransactionHash)].TxHash = common.HexToHash(finalTestHex(0x77))
		}},
		{name: "orphan proxy checkpoint", mutate: func(_ *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.blockHeads[100] = testEVMHead(100, 0x77)
		}},
		{name: "orphan release checkpoint", mutate: func(_ *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.blockHeads[140] = testEVMHead(140, 0x77)
		}},
		{name: "unfinalized checkpoint", mutate: func(_ *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.finalized = testEVMHead(99, 0x77)
		}},
		{name: "no proxy code", mutate: func(_ *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.codes = nil
		}},
		{name: "unreadable proxy code", mutate: func(_ *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.codeError = errors.New("RPC unavailable")
		}},
		{name: "foreign proxy runtime", mutate: func(manifest *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, reader *deploymentBoundaryFixture) {
			reader.codes[manifest.CoordinatorProxy] = []byte{0x99}
		}},
		{name: "conflicting saved boundary", mutate: func(manifest *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, _ *deploymentBoundaryFixture) {
			manifest.CoordinatorEventStartBlock = 101
			manifest.CoordinatorEventStartBlockHash = finalTestHex(0x11)
		}},
		{name: "partial saved boundary", mutate: func(manifest *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, _ *deploymentBoundaryFixture) {
			manifest.CoordinatorEventStartBlock = 100
		}},
		{name: "zero saved hash", mutate: func(manifest *ContractDeployment, _ *SetupPlan, _ *[]JournalEntry, _ *deploymentBoundaryFixture) {
			manifest.CoordinatorEventStartBlock = 100
			manifest.CoordinatorEventStartBlockHash = common.Hash{}.Hex()
		}},
	}
	for _, testCase := range cases {
		manifest, plan, entries, reader := deploymentBoundaryTestEvidence()
		testCase.mutate(&manifest, plan, &entries, reader)
		beforeHash, _ := canonicalHashHex(manifest)
		changed, err := reconcileContractDeploymentEventBoundary(context.Background(), reader, &manifest, entries, plan)
		afterHash, _ := canonicalHashHex(manifest)
		if err == nil || changed || afterHash != beforeHash {
			t.Errorf("%s: changed=%t mutated=%t err=%v", testCase.name, changed, afterHash != beforeHash, err)
		}
	}
}

// Identical proxy runtime and CREATE receipt coordinates do not prove the
// initializer storage. Signed bytes must match every approved envelope field.
func TestFinalSemanticDeploymentBoundaryRejectsForeignSignedCreateEnvelopes(t *testing.T) {
	for _, name := range []string{"initializer", "signer", "nonce", "value", "call", "chain", "unprotected", "unsigned"} {
		manifest, plan, entries, reader := deploymentBoundaryTestEvidence()
		key, _ := crypto.HexToECDSA(strings.Repeat("11", 32))
		chainID := new(big.Int).SetUint64(testnetChainID)
		payload := types.LegacyTx{Nonce: 4, GasPrice: big.NewInt(1), Gas: 100_000, Value: new(big.Int), Data: []byte{0x60, 0x01}}
		switch name {
		case "initializer":
			payload.Data = []byte{0x60, 0x02}
		case "signer":
			key, _ = crypto.HexToECDSA(strings.Repeat("22", 32))
		case "nonce":
			payload.Nonce++
		case "value":
			payload.Value = big.NewInt(1)
		case "call":
			payload.To = &manifest.CoordinatorProxy
		case "chain":
			chainID = big.NewInt(1)
		}
		transaction := types.NewTx(&payload)
		if name != "unsigned" {
			var signer types.Signer = types.LatestSignerForChainID(chainID)
			if name == "unprotected" {
				signer = types.HomesteadSigner{}
			}
			var err error
			transaction, err = types.SignTx(transaction, signer, key)
			if err != nil {
				t.Fatal(err)
			}
		}
		receipt := reader.receipts[common.HexToHash(entries[0].TransactionHash)]
		receipt.TxHash, entries[0].TransactionHash = transaction.Hash(), transaction.Hash().Hex()
		reader.receipts = map[common.Hash]*types.Receipt{transaction.Hash(): receipt}
		reader.transactions = map[common.Hash]*types.Transaction{transaction.Hash(): transaction}
		beforeHash, _ := canonicalHashHex(manifest)
		changed, err := reconcileContractDeploymentEventBoundary(context.Background(), reader, &manifest, entries, plan)
		afterHash, _ := canonicalHashHex(manifest)
		if err == nil || changed || beforeHash != afterHash {
			t.Errorf("%s CREATE envelope accepted or mutated manifest: changed=%t err=%v", name, changed, err)
		}
	}
}

// Creates a real approved payload/role graph and hash-chained finalized entry,
// but leaves its saved observation empty to reproduce a post-CREATE crash.
func deploymentBoundaryTestExecution(t *testing.T) (*Executor, *deploymentBoundaryFixture) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(cfg, roles, plan.Deployment.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	_, _, entries, reader := deploymentBoundaryTestEvidence()
	action, err := exactPlanActionByID(plan, "evm.coordinator-proxy")
	if err != nil {
		t.Fatal(err)
	}
	entries[0].DeploymentID, entries[0].PlanHash, entries[0].IntentHash = plan.DeploymentID, plan.PlanHash, action.IntentHash
	key, err := crypto.HexToECDSA(roles.EVM["deployer"].PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: payloads.Manifest.InitialNonce + 4, GasPrice: big.NewInt(1), Gas: 1_000_000, Value: new(big.Int), Data: payloads.Proxy}), types.LatestSignerForChainID(new(big.Int).SetUint64(testnetChainID)), key)
	if err != nil {
		t.Fatal(err)
	}
	receipt := reader.receipts[common.HexToHash(entries[0].TransactionHash)]
	receipt.ContractAddress, receipt.TxHash = payloads.Manifest.CoordinatorProxy, transaction.Hash()
	entries[0].TransactionHash = transaction.Hash().Hex()
	reader.receipts = map[common.Hash]*types.Receipt{transaction.Hash(): receipt}
	reader.transactions = map[common.Hash]*types.Transaction{transaction.Hash(): transaction}
	reader.codes = map[common.Address][]byte{payloads.Manifest.CoordinatorProxy: payloads.ExpectedRuntime[payloads.Manifest.CoordinatorProxy]}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveContractDeployment(stateDir, payloads.Manifest); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	if err := journal.Append(entries[0]); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, roles: roles, journal: journal, deployer: &EVMTxManager{client: reader.client(t)}}
	return executor, reader
}

// Production ensurePayloads must persist the recovered observation before
// retaining executable payloads, including an entirely zero manifest.
func TestFinalSemanticDeploymentBoundaryExecutorPersistsCrashRecovery(t *testing.T) {
	executor, reader := deploymentBoundaryTestExecution(t)
	plan, stateDir, entries := executor.plan, executor.stateDir, executor.journal.Entries()
	beforePlanHash, _ := plan.hash()
	if err := executor.ensurePayloads(context.Background()); err != nil {
		t.Fatal(err)
	}
	saved, err := loadContractDeployment(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	afterPlanHash, _ := plan.hash()
	if saved.DeployBlock != 100 || saved.CoordinatorEventStartBlock != 100 || saved.CoordinatorEventStartBlockHash != entries[0].BlockHash || executor.payloads.Manifest.CoordinatorEventStartBlock != 100 || afterPlanHash != beforePlanHash || reader.unexpectedRequests.Load() != 0 {
		t.Fatalf("executor did not persist exact crash recovery before execution: saved=%+v unexpected RPCs=%d", saved, reader.unexpectedRequests.Load())
	}
	// The persisted journal was hash authenticated by OpenJournal/Append. A
	// second executor must revalidate the same receipt without altering it.
	beforeJournal, err := os.ReadFile(filepath.Join(stateDir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	executor.payloads = nil
	if err := executor.ensurePayloads(context.Background()); err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(filepath.Join(stateDir, "journal.jsonl"))
	if err != nil || string(afterJournal) != string(beforeJournal) {
		t.Fatalf("migration rewrote journal: %v", err)
	}
	// Retirement replaces action IDs and its approval hash. Match the real
	// handoff: complete setup authentication before installing that namespace.
	retirementPlan := *plan
	retirementPlan.PlanHash = finalTestHex(0x90)
	retirementPlan.Actions = []Action{{ID: "retire.disable-deposits", IntentHash: finalTestHex(0x91)}}
	executor.plan = &retirementPlan
	if err := executor.ensurePayloads(context.Background()); err != nil {
		t.Fatalf("authenticated retirement handoff lost setup payloads: %v", err)
	}
	if executor.payloads.Manifest.CoordinatorEventStartBlock != 100 || plan.PlanHash == retirementPlan.PlanHash {
		t.Fatal("retirement changed original setup identity or proxy event boundary")
	}
}

// Later deployment actions cannot allocate a nonce or send when crash-recovery
// evidence is absent, foreign, duplicated, or independently contradicted.
func TestFinalSemanticDeploymentBoundaryRejectsLaterMutationBeforeRecovery(t *testing.T) {
	for _, name := range []string{"missing", "foreign", "wrong plan", "duplicate", "independent"} {
		executor, reader := deploymentBoundaryTestExecution(t)
		switch name {
		case "missing":
			reader.receipts = nil
		case "foreign":
			for _, receipt := range reader.receipts {
				receipt.ContractAddress = common.HexToAddress("0x99")
			}
		case "wrong plan":
			executor.journal.entries[0].PlanHash = finalTestHex(0x99)
		case "duplicate":
			executor.journal.entries = append(executor.journal.entries, executor.journal.entries[0])
		case "independent":
			_, _, _, independent := deploymentBoundaryTestEvidence()
			independent.codes = map[common.Address][]byte{executor.plan.Deployment.CoordinatorProxy: []byte{0x99}}
			executor.independentEVM = independent.client(t)
		}
		before, err := os.ReadFile(filepath.Join(executor.stateDir, "public", "contracts.json"))
		if err != nil {
			t.Fatal(err)
		}
		action, err := exactPlanActionByID(executor.plan, "evm.governance-drill-implementation")
		if err != nil {
			t.Fatal(err)
		}
		if err := executor.executeDeployment(context.Background(), action); err == nil {
			t.Errorf("%s evidence allowed later deployment", name)
		}
		after, err := os.ReadFile(filepath.Join(executor.stateDir, "public", "contracts.json"))
		if err != nil || string(before) != string(after) || executor.payloads != nil || reader.unexpectedRequests.Load() != 0 {
			t.Errorf("%s failure retained payloads, mutated manifest, or sent RPC: payloads=%t unexpected=%d err=%v", name, executor.payloads != nil, reader.unexpectedRequests.Load(), err)
		}
	}
}

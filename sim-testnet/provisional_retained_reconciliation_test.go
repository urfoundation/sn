// Synthetic signed transactions, real durable journals and bounded read-only
// readers reproduce the startup cycle without accessing any live deployment.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type retainedOutcomeTestReader struct {
	evmFinalityFixture
	signed      *types.Transaction
	includedErr error
	afterRead   func()
	firstError  error
	bodyReads   int
}

func (self *retainedOutcomeTestReader) TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	if self.firstError != nil {
		err := self.firstError
		self.firstError = nil
		self.receiptRequests = append(self.receiptRequests, hash)
		return nil, err
	}
	return self.evmFinalityFixture.TransactionReceipt(ctx, hash)
}

func (self *retainedOutcomeTestReader) TransactionInBlock(_ context.Context, hash common.Hash, index uint) (*types.Transaction, error) {
	self.bodyReads++
	if self.afterRead != nil {
		self.afterRead()
	}
	if hash != self.receipt.BlockHash || index != self.receipt.TransactionIndex {
		return nil, errors.New("unexpected included transaction coordinate")
	}
	return self.signed, self.includedErr
}

type retainedOutcomeTestFixture struct {
	executor  *Executor
	reader    *retainedOutcomeTestReader
	broadcast JournalEntry
	included  JournalEntry
	raw       []byte
	rawPath   string
}

// Supplying an executor lets the same exact incident enter full startup's
// authenticated approval path. Mutations occur before journal authentication.
func newRetainedOutcomeTestFixture(t *testing.T, self *Executor, mutate func(*retainedOutcomeTestFixture)) *retainedOutcomeTestFixture {
	t.Helper()
	if self == nil {
		stateDir := t.TempDir()
		self = &Executor{stateDir: stateDir, journal: openCampaignTestJournal(t, stateDir), plan: &SetupPlan{DeploymentID: "synthetic-retained-outcome", ChainID: 945, PlanHash: "0x" + strings.Repeat("11", 32), PriorPlanHashes: []string{"0x" + strings.Repeat("22", 32)}}}
	}
	key, err := crypto.HexToECDSA(strings.Repeat("6", 64))
	if err != nil {
		t.Fatal(err)
	}
	chain := new(big.Int).SetUint64(self.plan.ChainID)
	to := common.Address{0x17}
	signed, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{ChainID: chain, Nonce: 7, Gas: 100_000, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2), To: &to, Value: new(big.Int), Data: []byte{1, 2, 3}}), types.LatestSignerForChainID(chain), key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	owner := self.plan.PlanHash
	if len(self.plan.PriorPlanHashes) != 0 {
		owner = self.plan.PriorPlanHashes[0]
	}
	checkpoint, inclusion, head := testEVMHead(100, 0xa1), testEVMHead(105, 0xa2), testEVMHead(110, 0xa3)
	broadcast := JournalEntry{DeploymentID: self.plan.DeploymentID, PlanHash: owner, ActionID: "evidence.relay." + strings.Repeat("33", 32), IntentHash: "0x" + strings.Repeat("44", 32), Stage: StageBroadcast, Signer: crypto.PubkeyToAddress(key.PublicKey).Hex(), Nonce: "7", TransactionHash: signed.Hash().Hex(), RecoveryBlock: checkpoint.Number, RecoveryBlockHash: checkpoint.Hash}
	included := broadcast
	included.Stage, included.BlockNumber, included.BlockHash = StageIncluded, inclusion.Number, inclusion.Hash
	reader := &retainedOutcomeTestReader{evmFinalityFixture: evmFinalityFixture{finalized: head, canonical: map[uint64]ChainHead{100: checkpoint, 105: inclusion}, receipt: &types.Receipt{Type: signed.Type(), Status: types.ReceiptStatusSuccessful, TxHash: signed.Hash(), BlockNumber: big.NewInt(105), BlockHash: common.HexToHash(inclusion.Hash), TransactionIndex: 2, Logs: []*types.Log{}}}, signed: signed}
	fixture := &retainedOutcomeTestFixture{executor: self, reader: reader, broadcast: broadcast, included: included, raw: raw, rawPath: filepath.Join(self.stateDir, "transactions", strings.TrimPrefix(signed.Hash().Hex(), "0x")+".rlp")}
	if mutate != nil {
		mutate(fixture)
	}
	if err := atomicWrite(fixture.rawPath, fixture.raw, 0o600); err != nil {
		t.Fatal(err)
	}
	intent := fixture.broadcast
	intent.Stage, intent.TransactionHash, intent.Signer, intent.Nonce = StageIntent, "", "", ""
	for _, entry := range []JournalEntry{intent, fixture.broadcast, fixture.included} {
		if err := self.journal.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

// A normal startup used to reject this included transaction before reaching
// any reconciliation. No transport method can sign, send or allocate a nonce.
func TestRetainedProvisionalOutcomeReconcilesBeforeFullStartup(t *testing.T) {
	var fixture *retainedOutcomeTestFixture
	var reads, writes atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			http.Error(w, "invalid synthetic request", 400)
			return
		}
		reads.Add(1)
		var result any
		switch call.Method {
		case "eth_chainId":
			result = "0x" + new(big.Int).SetUint64(fixture.executor.plan.ChainID).Text(16)
		case "eth_getBlockByNumber":
			var selector string
			if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
				http.Error(w, "invalid synthetic block", 400)
				return
			}
			head := fixture.reader.finalized
			if selector == "0x64" {
				head = fixture.reader.canonical[100]
			} else if selector == "0x69" {
				head = fixture.reader.canonical[105]
			} else if selector != "finalized" {
				http.Error(w, "unexpected synthetic block", 400)
				return
			}
			result = map[string]any{"number": "0x" + new(big.Int).SetUint64(head.Number).Text(16), "hash": head.Hash}
		case "eth_getTransactionReceipt":
			result = fixture.reader.receipt
		case "eth_getTransactionByBlockHashAndIndex":
			result = fixture.reader.signed
		default:
			writes.Add(1)
			http.Error(w, "unexpected mutable or unrelated request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
	}))
	t.Cleanup(endpoint.Close)
	self, original := provisionalAllowanceAdoptionFixtureWithConfig(t, func(cfg *ResolvedConfig) {
		configureRetainedOutcomeTestEndpoint(t, cfg, endpoint.URL)
	})
	if err := self.activateProvisionalSetupRevision(t.Context(), original, func(context.Context, Action) error { return errors.New("unexpected setup action") }); err != nil {
		t.Fatal(err)
	}
	if err := prepareProvisionalResume(t.Context(), self.cfg, self.stateDir, "resume", cliOptions{Apply: true, ProvisionalResume: true, PlanHash: self.plan.PlanHash}, self.plan); err != nil {
		t.Fatal(err)
	}
	fixture = newRetainedOutcomeTestFixture(t, self, nil)
	if self.deployer != nil {
		t.Fatal("stopped topology fixture unexpectedly has a transaction manager")
	}
	starts := 0
	start := func(context.Context, *Executor, *provisionalStoppedTopology, map[string]string) error {
		if err := validateRetainedProvisionalTransactionOutcomes(self.plan, self.journal.Entries()); err != nil {
			return err
		}
		starts++
		return nil
	}
	before := self.journal.Entries()
	if err := executeRetainedProvisionalResume(t.Context(), self, &provisionalStoppedTopology{}, nil, start); err != nil || starts != 1 || writes.Load() != 0 || self.deployer != nil {
		t.Fatalf("retained startup did not reconcile first: starts=%d writes=%d error=%v", starts, writes.Load(), err)
	}
	after, err := readJournalEntries(self.stateDir)
	if err != nil || len(after) != len(before)+1 || !reflect.DeepEqual(before, after[:len(before)]) {
		t.Fatalf("reconciliation rewrote retained progress: %v", err)
	}
	last := after[len(after)-1]
	if last.Stage != StageFinalized || last.PlanHash != fixture.broadcast.PlanHash || last.IntentHash != fixture.broadcast.IntentHash || last.TransactionHash != fixture.broadcast.TransactionHash || last.RecoveryBlock != fixture.broadcast.RecoveryBlock || last.RecoveryBlockHash != fixture.broadcast.RecoveryBlockHash {
		t.Fatalf("outcome lost original ownership: %+v", last)
	}
	readCount := reads.Load()
	if err := executeRetainedProvisionalResume(t.Context(), self, &provisionalStoppedTopology{}, nil, start); err != nil || starts != 2 || reads.Load() != readCount || len(self.journal.Entries()) != len(after) {
		t.Fatalf("retained outcome was not idempotent: starts=%d reads=%d error=%v", starts, reads.Load(), err)
	}
}

// Select the test-owned transport before deriving immutable plan identities.
func configureRetainedOutcomeTestEndpoint(t *testing.T, cfg *ResolvedConfig, endpoint string) {
	t.Helper()
	cfg.Config.LaunchInputs.PublicEVMRPCOverride = endpoint
	cfg.Config.LaunchInputs.PublicSubstrateRPCOverride = "ws" + strings.TrimPrefix(endpoint, "http")
	cfg.Config.LaunchInputs.PublicEVMMaximumRequestsPerMinute = 0
	cfg.Public.Chain.EVMPublicReadEndpoint = endpoint
	cfg.Public.Chain.SubstratePublicReadEndpoint = cfg.Config.LaunchInputs.PublicSubstrateRPCOverride
	var err error
	cfg.OperationalSubstrate, cfg.OperationalEVM, cfg.OperationalRPCMode, err = resolveOperationalRPCs(cfg.Authority, cfg.Config.LaunchInputs.PublicSubstrateRPCOverride, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConfigHash, err = releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
}

// The stopped path needs no reader for an empty journal or native-only work.
// Native uncertainty is still rejected by the unchanged admission gate.
func TestRetainedProvisionalOutcomeSkipsReaderWithoutPendingEvm(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	self := &Executor{stateDir: stateDir, journal: openCampaignTestJournal(t, stateDir), plan: &SetupPlan{DeploymentID: "synthetic-no-reader", ChainID: 945, PlanHash: "0x" + strings.Repeat("11", 32)}}
	if err := self.reconcileRetainedProvisionalTransactionOutcomes(t.Context()); err != nil {
		t.Fatal("empty stopped topology needed a reader", err)
	}
	fixture := newRetainedOutcomeTestFixture(t, nil, func(f *retainedOutcomeTestFixture) {
		f.broadcast.Signer = "0x" + strings.Repeat("77", 32)
		f.included.Signer = f.broadcast.Signer
	})
	before := fixture.executor.journal.Entries()
	if err := fixture.executor.reconcileRetainedProvisionalTransactionOutcomes(t.Context()); err != nil || !reflect.DeepEqual(before, fixture.executor.journal.Entries()) {
		t.Fatal("native-only uncertainty opened an EVM reader or changed progress", err)
	}
	if err := validateRetainedProvisionalTransactionOutcomes(fixture.executor.plan, before); err == nil {
		t.Fatal("native uncertainty stopped blocking admission")
	}
}

// Chain identity cannot be truncated or bypassed by opening a temporary reader.
func TestRetainedProvisionalOutcomeRejectsTemporaryReaderIdentity(t *testing.T) {
	t.Parallel()
	for _, chain := range []*big.Int{big.NewInt(946), new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(945))} {
		var calls, unrelated atomic.Int64
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			defer request.Body.Close()
			var call struct {
				Id     json.RawMessage `json:"id"`
				Method string
			}
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				http.Error(w, "invalid synthetic request", http.StatusBadRequest)
				return
			}
			calls.Add(1)
			if call.Method != "eth_chainId" {
				unrelated.Add(1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": "0x" + chain.Text(16)})
		}))
		t.Cleanup(endpoint.Close)
		fixture := newRetainedOutcomeTestFixture(t, nil, nil)
		cfg := testResolvedConfig(t)
		configureRetainedOutcomeTestEndpoint(t, cfg, endpoint.URL)
		fixture.executor.cfg = cfg
		before := fixture.executor.journal.Entries()
		err := fixture.executor.reconcileRetainedProvisionalTransactionOutcomesWithPolicy(t.Context(), immediateFinalSemanticRetryPolicy())
		if err == nil || !strings.Contains(err.Error(), "chain id") || calls.Load() != 1 || unrelated.Load() != 0 || fixture.executor.deployer != nil || !reflect.DeepEqual(before, fixture.executor.journal.Entries()) {
			t.Fatalf("wrong chain %s reached outcome proof or changed progress: calls=%d unrelated=%d error=%v", chain, calls.Load(), unrelated.Load(), err)
		}
	}
}

// A refused transport and an explicitly canceled invocation retain all prior
// journal rows, allowing later continuation to reconcile the same signed bytes.
func TestRetainedProvisionalOutcomeTemporaryReaderFailurePreservesJournal(t *testing.T) {
	t.Parallel()
	endpoint := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint.Close()
	fixture := newRetainedOutcomeTestFixture(t, nil, nil)
	cfg := testResolvedConfig(t)
	configureRetainedOutcomeTestEndpoint(t, cfg, endpoint.URL)
	fixture.executor.cfg = cfg
	before := fixture.executor.journal.Entries()
	if err := fixture.executor.reconcileRetainedProvisionalTransactionOutcomesWithPolicy(t.Context(), immediateFinalSemanticRetryPolicy()); err == nil || fixture.executor.deployer != nil || !reflect.DeepEqual(before, fixture.executor.journal.Entries()) {
		t.Fatal("refused temporary transport changed retained progress", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := fixture.executor.reconcileRetainedProvisionalTransactionOutcomesWithPolicy(ctx, immediateFinalSemanticRetryPolicy()); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, fixture.executor.journal.Entries()) {
		t.Fatal("canceled temporary reader changed retained progress", err)
	}
}

// Missing, empty and substituted artifacts are rejected before any chain read.
func TestRetainedProvisionalOutcomeRejectsSignedArtifactFailures(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"missing", "empty", "malformed", "signer", "nonce", "chain"} {
		fixture := newRetainedOutcomeTestFixture(t, nil, func(f *retainedOutcomeTestFixture) {
			switch failure {
			case "empty":
				f.raw = nil
			case "malformed":
				f.raw = []byte("not a signed transaction")
			case "signer":
				f.broadcast.Signer = common.Address{0x32}.Hex()
				f.included.Signer = f.broadcast.Signer
			case "nonce":
				f.broadcast.Nonce = "8"
				f.included.Nonce = "8"
			case "chain":
				f.executor.plan.ChainID++
			}
		})
		if failure == "missing" {
			if err := os.Remove(fixture.rawPath); err != nil {
				t.Fatal(err)
			}
		}
		before := fixture.executor.journal.Entries()
		err := reconcileRetainedProvisionalEVMOutcomes(t.Context(), fixture.executor, fixture.reader, immediateFinalSemanticRetryPolicy())
		if err == nil || len(fixture.reader.headerRequests) != 0 || !reflect.DeepEqual(before, fixture.executor.journal.Entries()) {
			t.Fatalf("%s changed progress or queried chain: %v", failure, err)
		}
	}
}

// Inclusion, body and recovery-checkpoint authority are independent checks.
func TestRetainedProvisionalOutcomeRejectsAdjacentReceiptSubstitution(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"hash", "height", "overflow", "included", "reorg", "unfinalized", "checkpoint", "status", "body", "missing", "before-broadcast"} {
		fixture := newRetainedOutcomeTestFixture(t, nil, nil)
		switch failure {
		case "hash":
			fixture.reader.receipt.TxHash[0] ^= 1
		case "height":
			fixture.reader.receipt.BlockNumber = nil
		case "overflow":
			fixture.reader.receipt.BlockNumber = new(big.Int).Lsh(big.NewInt(1), 65)
		case "included":
			fixture.reader.receipt.BlockNumber = big.NewInt(106)
		case "reorg":
			fixture.reader.canonical[105] = testEVMHead(105, 0xb1)
		case "unfinalized":
			fixture.reader.finalized = testEVMHead(104, 0xb2)
		case "checkpoint":
			fixture.reader.canonical[100] = testEVMHead(100, 0xb3)
		case "status":
			fixture.reader.receipt.Status = 2
		case "body":
			fixture.reader.signed = types.NewTx(&types.LegacyTx{Nonce: 99})
		case "missing":
			fixture.reader.receiptError = ethereum.NotFound
		case "before-broadcast":
			fixture.reader.receipt.BlockNumber = big.NewInt(99)
		}
		before := fixture.executor.journal.Entries()
		if err := reconcileRetainedProvisionalEVMOutcomes(t.Context(), fixture.executor, fixture.reader, immediateFinalSemanticRetryPolicy()); err == nil || !reflect.DeepEqual(before, fixture.executor.journal.Entries()) {
			t.Fatalf("%s admitted an unproved outcome: %v", failure, err)
		}
	}
}

// A confirmed revert closes the nonce only; it remains visibly failed and is
// never promoted to a verified effect, success receipt or a new transaction.
func TestRetainedProvisionalOutcomeRetainsFinalizedRevert(t *testing.T) {
	t.Parallel()
	fixture := newRetainedOutcomeTestFixture(t, nil, nil)
	fixture.reader.receipt.Status = types.ReceiptStatusFailed
	if err := reconcileRetainedProvisionalEVMOutcomes(t.Context(), fixture.executor, fixture.reader, immediateFinalSemanticRetryPolicy()); err != nil {
		t.Fatal(err)
	}
	entries := fixture.executor.journal.Entries()
	last := entries[len(entries)-1]
	if len(entries) != 4 || last.Stage != StageFinalized || !strings.Contains(last.Error, "reverted") || last.PostconditionHash != "" || last.TransactionHash != fixture.broadcast.TransactionHash {
		t.Fatalf("reverted effect was promoted: %+v", entries)
	}
	if err := validateRetainedProvisionalTransactionOutcomes(fixture.executor.plan, entries); err != nil {
		t.Fatal("known failed nonce stayed unresolved", err)
	}
}

// A transient read repeats the read-only proof and produces one durable row.
func TestRetainedProvisionalOutcomeRetriesTransportWithoutDuplicateFinality(t *testing.T) {
	t.Parallel()
	fixture := newRetainedOutcomeTestFixture(t, nil, nil)
	fixture.reader.firstError = io.ErrUnexpectedEOF
	if err := reconcileRetainedProvisionalEVMOutcomes(t.Context(), fixture.executor, fixture.reader, immediateFinalSemanticRetryPolicy()); err != nil {
		t.Fatal(err)
	}
	if len(fixture.reader.receiptRequests) != 2 || fixture.reader.bodyReads != 1 || len(fixture.executor.journal.Entries()) != 4 {
		t.Fatal("retry duplicated finality or skipped exact body authentication")
	}
}

// Cancellation at the final read cannot append a stale successful outcome;
// a later continuation repeats the proof and retains the original journal.
func TestRetainedProvisionalOutcomeCancellationPreservesRecovery(t *testing.T) {
	t.Parallel()
	fixture := newRetainedOutcomeTestFixture(t, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.reader.afterRead = cancel
	before := fixture.executor.journal.Entries()
	if err := reconcileRetainedProvisionalEVMOutcomes(ctx, fixture.executor, fixture.reader, immediateFinalSemanticRetryPolicy()); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, fixture.executor.journal.Entries()) {
		t.Fatalf("canceled proof changed journal: %v", err)
	}
	fixture.reader.afterRead = nil
	if err := reconcileRetainedProvisionalEVMOutcomes(t.Context(), fixture.executor, fixture.reader, immediateFinalSemanticRetryPolicy()); err != nil || len(fixture.executor.journal.Entries()) != len(before)+1 {
		t.Fatalf("continuation lost its exact source: %v", err)
	}
}

// Startup may examine all broadcasts, but a native domain has no EVM outcome.
func TestRetainedProvisionalOutcomeLeavesNativeAndForeignBroadcastsUnresolved(t *testing.T) {
	t.Parallel()
	for _, foreign := range []bool{false, true} {
		fixture := newRetainedOutcomeTestFixture(t, nil, func(f *retainedOutcomeTestFixture) {
			if foreign {
				f.broadcast.PlanHash = "0x" + strings.Repeat("79", 32)
				f.included.PlanHash = f.broadcast.PlanHash
			} else {
				f.broadcast.Signer = "synthetic-native-signer"
				f.included.Signer = f.broadcast.Signer
			}
		})
		before := fixture.executor.journal.Entries()
		if err := reconcileRetainedProvisionalEVMOutcomes(t.Context(), fixture.executor, nil, immediateFinalSemanticRetryPolicy()); err != nil || !reflect.DeepEqual(before, fixture.executor.journal.Entries()) {
			t.Fatalf("non-EVM or foreign history was dispatched: %v", err)
		}
		if err := validateRetainedProvisionalTransactionOutcomes(fixture.executor.plan, before); !foreign && err == nil {
			t.Fatal("native unresolved transaction passed startup")
		}
	}
}

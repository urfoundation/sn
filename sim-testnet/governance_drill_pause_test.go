// The real pause dispatcher, signed RLP and journal survive interruption after
// chain finality. Only the local RPC peer and atomic-writer boundary are faulted.
package main

import (
	"bytes"
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
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/stabi"
)

// Immutable baseline responses use one historical hash after the live pause.
// All mutable RPC fixture state is guarded because handlers run concurrently.
type governancePauseTest struct {
	t                *testing.T
	executor         *Executor
	action           Action
	stateLock        sync.Mutex
	paused           bool
	postReadFailures int
	sends            int
	nonceReads       int
	checkpointAtSend bool
	transaction      *types.Transaction
	responses        map[string]string
	pausedSelector   string
	baseline         ChainHead
	after            ChainHead
	inclusion        ChainHead
}

// Provides real custody snapshots and manager finality through an in-process
// RPC server; no signer, snapshot parser or journal operation is replaced.
func newGovernancePauseTest(t *testing.T) *governancePauseTest {
	t.Helper()
	f := &governancePauseTest{t: t, responses: map[string]string{}, baseline: ChainHead{Number: 100, Hash: common.Hash{0xa1}.Hex()}, inclusion: ChainHead{Number: 101, Hash: common.Hash{0xa2}.Hex()}, after: ChainHead{Number: 102, Hash: common.Hash{0xa3}.Hex()}}
	cfg := testResolvedConfig(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, err := crypto.HexToECDSA(strings.Repeat("6", 64))
	if err != nil {
		t.Fatal(err)
	}
	guardianKey, err := crypto.HexToECDSA(strings.Repeat("7", 64))
	if err != nil {
		t.Fatal(err)
	}
	f.action = Action{ID: "governance.guardian-pause", Kind: "evm-transaction", Target: "guardian", Parameters: map[string]string{evmMaximumGasUnitsParameter: "100000", evmMaximumFeePerGasParameter: "100"}, Spend: Spend{EVMGasWei: "10000000"}}
	f.action.IntentHash, err = actionIntentHash(f.action)
	if err != nil {
		t.Fatal(err)
	}
	plan := &SetupPlan{PlanHash: common.Hash{0xb1}.Hex(), Actions: []Action{f.action}}
	payloads := &DeploymentPayloads{Manifest: ContractDeployment{CoordinatorProxy: common.Address{0xc1}, SettlementVault: common.Address{0xc2}, ReserveSink: common.Address{0xc3}}, CoordinatorUpgrade: CoordinatorUpgrade{Implementation: common.Address{0xc4}}}
	endpoint := httptest.NewServer(f)
	client, err := ethclient.Dial(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	owner := &EvmTxManager{client: client, key: ownerKey}
	guardian := &EvmTxManager{client: client, chainID: big.NewInt(945), deploymentID: cfg.Config.Deployment.DeploymentID, stateDir: stateDir, journal: journal, key: guardianKey}
	f.executor = &Executor{cfg: cfg, plan: plan, payloads: payloads, stateDir: stateDir, journal: journal, owner: owner, guardian: guardian}
	t.Cleanup(func() { client.Close(); endpoint.Close(); _ = f.executor.journal.Close() })
	if err := journal.Append(JournalEntry{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: plan.PlanHash, ActionID: f.action.ID, IntentHash: f.action.IntentHash, Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	coordinatorAbi, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	vaultAbi, err := abi.JSON(strings.NewReader(SettlementVaultABI))
	if err != nil {
		t.Fatal(err)
	}
	reserveAbi, err := abi.JSON(strings.NewReader(ReserveSinkABI))
	if err != nil {
		t.Fatal(err)
	}
	set := func(contract *abi.ABI, name string, to common.Address, data []byte, value any) {
		raw, err := contract.Methods[name].Outputs.Pack(value)
		if err != nil {
			t.Fatal(err)
		}
		f.responses[strings.ToLower(to.Hex())+":"+hexutil.Encode(data)] = hexutil.Encode(raw)
	}
	coordinator, vault, reserve := stabi.NewSTCoordinator(), stabi.NewSTSettlementVault(), stabi.NewSTReserveSink()
	proxy := payloads.Manifest.CoordinatorProxy
	set(&coordinatorAbi, "owner", proxy, coordinator.PackOwner(), crypto.PubkeyToAddress(ownerKey.PublicKey))
	set(&coordinatorAbi, "guardian", proxy, coordinator.PackGuardian(), crypto.PubkeyToAddress(guardianKey.PublicKey))
	f.pausedSelector = hexutil.Encode(coordinator.PackPaused())
	set(&coordinatorAbi, "currentEpoch", proxy, coordinator.PackCurrentEpoch(), big.NewInt(7))
	policy := stabi.STCoordinatorPolicySnapshot{PolicyHash: [32]byte{7}, EpochDepositCapRao: big.NewInt(100), CampaignDepositCapRao: big.NewInt(1000)}
	set(&coordinatorAbi, "policyAt", proxy, coordinator.PackPolicyAt(big.NewInt(7)), policy)
	set(&vaultAbi, "coordinator", payloads.Manifest.SettlementVault, vault.PackCoordinator(), proxy)
	set(&reserveAbi, "recorder", payloads.Manifest.ReserveSink, reserve.PackRecorder(), proxy)
	set(&reserveAbi, "principal", payloads.Manifest.ReserveSink, reserve.PackPrincipal(), big.NewInt(100))
	set(&reserveAbi, "liveStake", payloads.Manifest.ReserveSink, reserve.PackLiveStake(), big.NewInt(110))
	entitlement := stabi.STSettlementVaultEntitlement{PayoutRoot: [32]byte{0xe1}, ArtifactHash: [32]byte{0xe2}, Funded: big.NewInt(90), Total: big.NewInt(80), Claimed: big.NewInt(20), ExpiryBlock: 1000, Status: 2}
	set(&vaultAbi, "entitlement", payloads.Manifest.SettlementVault, vault.PackEntitlement(big.NewInt(7), big.NewInt(1)), entitlement)
	return f
}

// The send observes the actual disk checkpoint before applying the pause;
// recovery serves the exact receipt and rejects unexpected mutation methods.
func (self *governancePauseTest) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request struct {
		Id     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	var result any
	var responseErr error
	switch request.Method {
	case "eth_getTransactionCount":
		self.nonceReads++
		result = "0x7"
		if self.paused {
			result = "0x8"
		}
	case "eth_maxPriorityFeePerGas":
		result = "0x2"
	case "eth_estimateGas":
		result = "0x61a8"
	case "eth_getBalance":
		result = "0xde0b6b3a7640000"
	case "eth_getBlockByNumber":
		var selector string
		_ = json.Unmarshal(request.Params[0], &selector)
		switch selector {
		case "latest":
			result = &types.Header{Number: big.NewInt(100), Difficulty: new(big.Int), GasLimit: 30000000, Time: 1700000021, BaseFee: big.NewInt(4), Extra: []byte{}}
		case "finalized":
			head := self.baseline
			if self.paused {
				head = self.after
			}
			result = map[string]any{"number": hexutil.EncodeUint64(head.Number), "hash": head.Hash}
		case hexutil.EncodeUint64(self.inclusion.Number):
			result = map[string]any{"number": hexutil.EncodeUint64(self.inclusion.Number), "hash": self.inclusion.Hash}
		default:
			responseErr = fmt.Errorf("unexpected block selector %s", selector)
		}
	case "eth_getStorageAt":
		result = hexutil.Encode(common.LeftPadBytes(self.executor.payloads.CoordinatorUpgrade.Implementation.Bytes(), 32))
	case "eth_call":
		var message map[string]string
		var selector finalEVMBlockSelector
		_ = json.Unmarshal(request.Params[0], &message)
		_ = json.Unmarshal(request.Params[1], &selector)
		if !selector.RequireCanonical || selector.BlockHash != self.baseline.Hash && selector.BlockHash != self.after.Hash {
			responseErr = errors.New("snapshot has no exact canonical block")
			break
		}
		if self.paused && selector.BlockHash == self.after.Hash && self.postReadFailures > 0 {
			self.postReadFailures--
			responseErr = errors.New("temporary connection read timeout")
			break
		}
		if message["data"] == self.pausedSelector {
			value := byte(0)
			if self.paused && selector.BlockHash == self.after.Hash {
				value = 1
			}
			result = hexutil.Encode(common.LeftPadBytes([]byte{value}, 32))
		} else {
			value, found := self.responses[strings.ToLower(message["to"])+":"+message["data"]]
			if !found {
				responseErr = errors.New("unexpected snapshot call")
			}
			result = value
		}
	case "eth_sendRawTransaction":
		var raw hexutil.Bytes
		_ = json.Unmarshal(request.Params[0], &raw)
		var transaction types.Transaction
		if err := transaction.UnmarshalBinary(raw); err != nil {
			responseErr = err
			break
		}
		var checkpoint GovernanceDrillEvidence
		if err := readJSONFile(self.executor.governanceEvidencePath(), &checkpoint); err == nil {
			self.checkpointAtSend = checkpoint.Stage == "pause-prepared" && checkpoint.PauseIntent != nil && checkpoint.PauseIntent.TransactionHash == transaction.Hash().Hex() && !checkpoint.Before.Paused
		}
		self.transaction = &transaction
		self.sends++
		self.paused = true
		result = transaction.Hash()
	case "eth_getTransactionReceipt":
		var hash common.Hash
		_ = json.Unmarshal(request.Params[0], &hash)
		if self.transaction != nil && hash == self.transaction.Hash() {
			result = &types.Receipt{Type: self.transaction.Type(), Status: types.ReceiptStatusSuccessful, TxHash: hash, BlockNumber: new(big.Int).SetUint64(self.inclusion.Number), BlockHash: common.HexToHash(self.inclusion.Hash), GasUsed: 40000, CumulativeGasUsed: 40000, EffectiveGasPrice: big.NewInt(6), Logs: []*types.Log{}}
		}
	default:
		responseErr = fmt.Errorf("unexpected RPC method %s", request.Method)
	}
	w.Header().Set("Content-Type", "application/json")
	if responseErr != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.Id, "error": map[string]any{"code": -32000, "message": responseErr.Error()}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.Id, "result": result})
}

// Reopening the journal models an owner restart without changing any evidence.
func (self *governancePauseTest) reopen(t *testing.T) {
	t.Helper()
	if err := self.executor.journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(self.executor.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	self.executor.journal, self.executor.guardian.journal = journal, journal
}

// Force the old failure window: chain pause is finalized, but the paused
// evidence write fails. The durable prepared snapshot must remain recoverable.
func (self *governancePauseTest) interrupt(t *testing.T) GovernanceDrillEvidence {
	t.Helper()
	crash := errors.New("synthetic interruption after finalized pause")
	err := self.executor.governancePauseWithWriter(t.Context(), self.action, func(evidence *GovernanceDrillEvidence) error {
		if evidence.Stage == "paused" {
			return crash
		}
		return self.executor.writeGovernanceEvidence(evidence)
	})
	if !errors.Is(err, crash) {
		t.Fatalf("pause did not reach the post-finality interruption: %v", err)
	}
	evidence, err := self.executor.loadGovernanceEvidence()
	if err != nil || evidence.Stage != "pause-prepared" || evidence.PauseIntent == nil {
		t.Fatalf("pause lost its durable pre-send baseline: %+v %v", evidence, err)
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if !self.paused || self.sends != 1 || !self.checkpointAtSend {
		t.Fatalf("write-ahead ordering differs: paused=%t sends=%d checkpoint=%t", self.paused, self.sends, self.checkpointAtSend)
	}
	return *evidence
}

// Successful recovery preserves the original custody snapshot, signed bytes,
// current plan, nonce and single broadcast after reopening the durable owner.
func TestGovernancePauseRecoversAfterFinalityBeforeEvidence(t *testing.T) {
	f := newGovernancePauseTest(t)
	before := f.interrupt(t)
	f.reopen(t)
	if err := f.executor.governancePause(t.Context(), f.action); err != nil {
		t.Fatal(err)
	}
	after, err := f.executor.loadGovernanceEvidence()
	if err != nil || after.Stage != "paused" || after.Before != before.Before || *after.PauseIntent != *before.PauseIntent || after.Transactions[f.action.ID] != before.PauseIntent.TransactionHash {
		t.Fatalf("recovered evidence changed original custody: %+v %v", after, err)
	}
	f.stateLock.Lock()
	defer f.stateLock.Unlock()
	if f.sends != 1 || f.nonceReads != 2 {
		t.Fatalf("recovery repeated nonce selection or broadcast: sends=%d nonce_reads=%d", f.sends, f.nonceReads)
	}
	raw, err := os.ReadFile(filepath.Join(f.executor.stateDir, "transactions", strings.TrimPrefix(before.PauseIntent.TransactionHash, "0x")+".rlp"))
	want, marshalErr := f.transaction.MarshalBinary()
	if err != nil || marshalErr != nil || !bytes.Equal(raw, want) {
		t.Fatal("recovery changed retained signed transaction", err, marshalErr)
	}
}

// A transient post-state read never discards the durable pre-mutation view.
func TestGovernancePauseRecoversAfterPostStateReadFailure(t *testing.T) {
	f := newGovernancePauseTest(t)
	f.postReadFailures = 1
	if err := f.executor.governancePause(t.Context(), f.action); err == nil || !strings.Contains(err.Error(), "temporary connection read timeout") {
		t.Fatalf("post-state failure was hidden: %v", err)
	}
	evidence, err := f.executor.loadGovernanceEvidence()
	if err != nil || evidence.Stage != "pause-prepared" {
		t.Fatal("transient read discarded the baseline", err)
	}
	f.reopen(t)
	if err := f.executor.governancePause(t.Context(), f.action); err != nil {
		t.Fatal(err)
	}
	f.stateLock.Lock()
	defer f.stateLock.Unlock()
	if f.sends != 1 {
		t.Fatal("post-state retry repeated pause broadcast")
	}
}

// A saved baseline cannot be rebound to another plan, guardian, nonce, action,
// transaction, or historical snapshot; every refusal precedes network mutation.
func TestGovernancePauseRejectsChangedRecoveryAuthority(t *testing.T) {
	f := newGovernancePauseTest(t)
	original := f.interrupt(t)
	for _, fault := range []string{"plan", "guardian", "nonce", "action", "transaction", "baseline"} {
		t.Run(fault, func(t *testing.T) {
			changed := original
			intent := *original.PauseIntent
			changed.PauseIntent = &intent
			switch fault {
			case "plan":
				changed.PlanHash = common.Hash{0xd1}.Hex()
			case "guardian":
				intent.Signer = common.Address{0xd2}.Hex()
			case "nonce":
				intent.Nonce++
			case "action":
				intent.IntentHash = common.Hash{0xd3}.Hex()
			case "transaction":
				intent.TransactionHash = common.Hash{0xd4}.Hex()
			case "baseline":
				changed.Before.ReservePrincipalRao = "999"
			}
			if err := f.executor.writeGovernanceEvidence(&changed); err != nil {
				t.Fatal(err)
			}
			if err := f.executor.governancePause(t.Context(), f.action); err == nil {
				t.Fatal("changed recovery authority was admitted")
			}
		})
	}
	if err := f.executor.writeGovernanceEvidence(&original); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.governancePause(t.Context(), f.action); err != nil {
		t.Fatal("rejected mutation damaged original recovery", err)
	}
	f.stateLock.Lock()
	defer f.stateLock.Unlock()
	if f.sends != 1 || f.nonceReads != 2 {
		t.Fatal("authority refusal acquired another nonce or broadcast")
	}
}

// Replayed signed bytes are checked independently of a syntactically matching
// checkpoint and broadcast row, including the recovered guardian and nonce.
func TestGovernancePauseRejectsForeignSignedTransaction(t *testing.T) {
	f := newGovernancePauseTest(t)
	original := f.interrupt(t)
	for _, fault := range []string{"guardian", "nonce", "target", "data", "value", "fee"} {
		t.Run(fault, func(t *testing.T) {
			key := f.executor.guardian.key
			target := f.executor.payloads.Manifest.CoordinatorProxy
			data := bytes.Clone(f.transaction.Data())
			nonce, fee, value := original.PauseIntent.Nonce, big.NewInt(10), new(big.Int)
			switch fault {
			case "guardian":
				key = f.executor.owner.key
			case "nonce":
				nonce++
			case "target":
				target[0] ^= 1
			case "data":
				data[len(data)-1] ^= 1
			case "value":
				value.SetUint64(1)
			case "fee":
				fee.SetUint64(101)
			}
			signed, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(945), Nonce: nonce, GasTipCap: big.NewInt(2), GasFeeCap: fee, Gas: 55000, To: &target, Value: value, Data: data}), types.LatestSignerForChainID(big.NewInt(945)), key)
			if err != nil {
				t.Fatal(err)
			}
			intent := *original.PauseIntent
			intent.TransactionHash = signed.Hash().Hex()
			if fault != "nonce" {
				intent.Nonce = nonce
			}
			executor := *f.executor
			executor.journal = &Journal{entries: []JournalEntry{{DeploymentID: executor.cfg.Config.Deployment.DeploymentID, PlanHash: executor.plan.PlanHash, ActionID: f.action.ID, IntentHash: f.action.IntentHash, Stage: StageBroadcast, TransactionHash: intent.TransactionHash, Signer: intent.Signer, Nonce: strconv.FormatUint(intent.Nonce, 10)}}}
			if err := executor.validateGovernancePauseTransaction(f.action, &intent, signed, f.transaction.Data()); err == nil {
				t.Fatal("foreign signed transaction acquired pause authority")
			}
		})
	}
}

// Failure to persist the checkpoint cannot send; absence and corruption are
// distinct so a damaged existing report cannot be overwritten by fresh work.
func TestGovernancePauseRefusesMissingWriteAheadAndCorruptEvidence(t *testing.T) {
	f := newGovernancePauseTest(t)
	want := errors.New("synthetic checkpoint write failure")
	err := f.executor.governancePauseWithWriter(context.Background(), f.action, func(*GovernanceDrillEvidence) error { return want })
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	if err := atomicWrite(f.executor.governanceEvidencePath(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.executor.governancePause(t.Context(), f.action); err == nil {
		t.Fatal("corrupt evidence was replaced")
	}
	f.stateLock.Lock()
	defer f.stateLock.Unlock()
	if f.sends != 0 || f.paused {
		t.Fatal("failed write-ahead preparation changed the coordinator")
	}
}

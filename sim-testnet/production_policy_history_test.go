// Synthetic policy histories exercise the complete production transition,
// including signed transaction persistence, pinned readback, and crash recovery.
package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The predecessor, amendment, and production policy keep complete custody
// fields. The optional original snapshot represents an already migrated setup.
func productionHistoryTestPolicies(t *testing.T, cfg *ResolvedConfig, plan *SetupPlan, migrated bool) []stabi.STCoordinatorPolicySnapshot {
	t.Helper()
	prior := *cfg
	prior.Policy = &plan.PolicyRateAmendment.Previous
	var err error
	prior.PolicyHash, err = prior.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	epochBlocks := cfg.Policy.Settlement.EpochBlocks
	policies := []stabi.STCoordinatorPolicySnapshot{
		rateAmendmentTestSnapshot(&prior, 10, 10*epochBlocks+1),
		rateAmendmentTestSnapshot(cfg, 20, 20*epochBlocks+1),
	}
	if migrated {
		original := rateAmendmentTestSnapshot(&prior, 0, 1)
		original.PolicyHash[0] ^= 1
		policies = append([]stabi.STCoordinatorPolicySnapshot{original}, policies...)
	}
	production := rateAmendmentTestSnapshot(cfg, 33, 33*epochBlocks+1)
	p := cfg.Policy.ProductionCadence
	production.EpochBlocks, production.RootCommitWindowBlocks = p.EpochBlocks, p.RootCommitWindowBlocks
	production.FinalizeOffsetBlocks, production.CloseGraceBlocks = p.FinalizeOffsetBlocks, p.CloseGraceBlocks
	production.CommitmentMaxAgeBlocks = p.EpochBlocks * 2
	return append(policies, production)
}

// The exact approved amendment is the only new history allowance; fresh and
// migrated deployments both remain usable before and after activation.
func TestProductionPolicyHistoryAcceptsApprovedAmendment(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		cfg, _, plan := rateAmendmentTestPlans(t)
		policies := productionHistoryTestPolicies(t, cfg, plan, migrated)
		for _, scheduled := range []bool{false, true} {
			count := len(policies) - 1
			if scheduled {
				count++
			}
			history := &productionPolicyHistory{currentEpoch: 32, policies: policies[:count], active: policies[len(policies)-2]}
			if err := validateProductionPolicyHistory(cfg, plan, history); err != nil || history.scheduled != scheduled {
				t.Fatalf("approved history rejected: migrated=%t scheduled=%t error=%v", migrated, scheduled, err)
			}
			if scheduled {
				history.currentEpoch, history.active = 33, policies[len(policies)-1]
				if err := validateProductionPolicyHistory(cfg, plan, history); err != nil {
					t.Fatalf("activated production rejected: migrated=%t error=%v", migrated, err)
				}
			}
		}
	}
}

// No amendment is necessary for the original fresh/migrated transition.
func TestProductionPolicyHistoryPreservesOriginalAllowance(t *testing.T) {
	cfg := testResolvedConfig(t)
	policy := rateAmendmentTestSnapshot(cfg, 0, 1)
	for _, migrated := range []bool{false, true} {
		policies := []stabi.STCoordinatorPolicySnapshot{policy}
		if migrated {
			policies[0].PolicyHash[0] ^= 1
			current := policy
			current.EffectiveEpoch, current.EffectiveBlock = 10, 10*current.EpochBlocks+1
			policies = append(policies, current)
		}
		active := policies[len(policies)-1]
		history := &productionPolicyHistory{currentEpoch: 32, policies: policies, active: active}
		if err := validateProductionPolicyHistory(cfg, nil, history); err != nil || history.scheduled {
			t.Fatalf("original history rejected: migrated=%t error=%v", migrated, err)
		}
		production := active
		p := cfg.Policy.ProductionCadence
		production.EffectiveEpoch, production.EffectiveBlock = 33, 33*active.EpochBlocks+1
		production.EpochBlocks, production.RootCommitWindowBlocks = p.EpochBlocks, p.RootCommitWindowBlocks
		production.FinalizeOffsetBlocks, production.CloseGraceBlocks = p.FinalizeOffsetBlocks, p.CloseGraceBlocks
		production.CommitmentMaxAgeBlocks = p.EpochBlocks * 2
		history.policies = append(policies, production)
		if err := validateProductionPolicyHistory(cfg, nil, history); err != nil || !history.scheduled {
			t.Fatalf("original production rejected: migrated=%t error=%v", migrated, err)
		}
	}
}

// A plausible final snapshot cannot conceal unapproved lineage, mutated caps,
// a skipped predecessor, or a pending/active policy from another generation.
func TestProductionPolicyHistoryRejectsUnapprovedAndMixedInventory(t *testing.T) {
	for _, fault := range []string{"approval", "lineage", "document", "predecessor-hash", "predecessor-cap", "predecessor-ttl", "current-cap", "current-hash", "extra-version", "missing-predecessor", "epoch-order", "block-order", "active-policy", "not-active", "production-cap"} {
		cfg, _, plan := rateAmendmentTestPlans(t)
		policies := productionHistoryTestPolicies(t, cfg, plan, true)
		history := &productionPolicyHistory{currentEpoch: 32, policies: policies, active: policies[2]}
		switch fault {
		case "approval":
			plan.PolicyRateAmendment = nil
		case "lineage":
			plan.PriorPlanHashes = nil
		case "document":
			cfg.Policy.PolicyID++
		case "predecessor-hash":
			history.policies[1].PolicyHash[0] ^= 1
		case "predecessor-cap":
			history.policies[1].CampaignDepositCapRao = big.NewInt(1)
		case "predecessor-ttl":
			history.policies[1].ClaimTTLEpochs++
		case "current-cap":
			history.policies[2].EpochDepositCapRao = big.NewInt(1)
		case "current-hash":
			history.policies[2].PolicyHash[0] ^= 1
		case "extra-version":
			history.policies = append([]stabi.STCoordinatorPolicySnapshot{policies[0]}, policies...)
		case "missing-predecessor":
			history.policies = policies[2:]
		case "epoch-order":
			history.policies[2].EffectiveEpoch = policies[1].EffectiveEpoch
		case "block-order":
			history.policies[3].EffectiveBlock = policies[2].EffectiveBlock
		case "active-policy":
			history.active.PolicyHash[0] ^= 1
		case "not-active":
			history.currentEpoch = policies[2].EffectiveEpoch - 1
		case "production-cap":
			history.policies[3].EpochDepositCapRao = nil
		}
		if err := validateProductionPolicyHistory(cfg, plan, history); err == nil {
			t.Fatalf("unapproved %s was accepted", fault)
		}
	}
	if err := validateProductionPolicyHistory(&ResolvedConfig{}, nil, &productionPolicyHistory{}); err == nil {
		t.Fatal("incomplete policy context was accepted")
	}
}

// The first receipt observation advances the synthetic chain after a real
// signed transaction has been persisted. Unpinned reads are counted so an old
// scheduler reaches the count failure and the fixed path must prove pinning.
type productionPolicyRpcFixture struct {
	t             *testing.T
	stateLock     sync.Mutex
	before        ChainHead
	after         ChainHead
	current       ChainHead
	callKVs       map[uint64]map[string]string
	receipt       *ethTypes.Receipt
	nonceReads    int
	receiptReads  int
	unpinnedReads int
}

// Refuse unexpected writes and record contract observation selectors. Transaction
// preparation uses the same deterministic gas envelope as the signed fixture.
func (self *productionPolicyRpcFixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var call struct {
		Id     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := func() (any, error) {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		switch call.Method {
		case "eth_blockNumber":
			return hexutil.EncodeUint64(self.current.Number), nil
		case "eth_getBlockByNumber":
			var selector string
			if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
				return nil, fmt.Errorf("invalid block request")
			}
			if selector == "latest" {
				return &ethTypes.Header{Number: new(big.Int).SetUint64(self.current.Number), Difficulty: new(big.Int), GasLimit: 30_000_000, Time: 1_700_000_000, BaseFee: big.NewInt(4), Extra: []byte{}}, nil
			}
			head := self.current
			if selector != "finalized" {
				block, err := hexutil.DecodeUint64(selector)
				if err != nil || block != self.before.Number && block != self.after.Number {
					return nil, fmt.Errorf("unexpected block selector %q", selector)
				}
				if block == self.before.Number {
					head = self.before
				} else {
					head = self.after
				}
			}
			return map[string]any{"number": hexutil.EncodeUint64(head.Number), "hash": head.Hash}, nil
		case "eth_call":
			var selector string
			var envelope struct {
				Input string `json:"input"`
				Data  string `json:"data"`
			}
			if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil || json.Unmarshal(call.Params[0], &envelope) != nil {
				return nil, fmt.Errorf("invalid contract call")
			}
			block, err := hexutil.DecodeUint64(selector)
			if selector == "latest" {
				self.unpinnedReads++
				block = self.current.Number
			} else if err != nil {
				return nil, fmt.Errorf("contract read is not pinned: %q", selector)
			}
			data := envelope.Input
			if data == "" {
				data = envelope.Data
			}
			output, ok := self.callKVs[block][strings.ToLower(data)]
			if !ok {
				return nil, fmt.Errorf("unplanned contract read at %d: %s", block, data)
			}
			return output, nil
		case "eth_getTransactionCount":
			self.nonceReads++
			return "0x4", nil
		case "eth_maxPriorityFeePerGas":
			return "0x2", nil
		case "eth_estimateGas":
			return "0x61a8", nil
		case "eth_getBalance":
			return hexutil.EncodeBig(big.NewInt(1_000_000_000_000_000_000)), nil
		case "eth_getTransactionReceipt":
			var hash common.Hash
			if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &hash) != nil || hash != self.receipt.TxHash {
				return nil, fmt.Errorf("unexpected transaction receipt")
			}
			self.current = self.after
			self.receiptReads++
			return self.receipt, nil
		default:
			return nil, fmt.Errorf("unexpected rpc method %s", call.Method)
		}
	}()
	response := map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result}
	if err != nil {
		self.t.Errorf("production policy fixture: %v", err)
		delete(response, "result")
		response["error"] = map[string]any{"code": -32602, "message": err.Error()}
	}
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		self.t.Errorf("encode production policy response: %v", err)
	}
}

// The real scheduler signs exactly once, appends a fourth policy, verifies its
// receipt at that index, and recovers missing local evidence without a new send.
func TestProductionPolicyAmendmentSchedulesAndRecoversFourthVersion(t *testing.T) {
	cfg, _, plan := rateAmendmentTestPlans(t)
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_, roles, _ := writeReleaseCampaignFixture(t, cfg, stateDir, 26, 32)
	gate, err := loadReleaseCampaignGate(cfg, stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	plan.Deployment.CoordinatorProxy = common.Address{0x71}
	policies := productionHistoryTestPolicies(t, cfg, plan, true)
	production := policies[3]
	input := production
	input.EffectiveBlock = 0
	data, err := parsed.Pack("schedulePolicy", input)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := ethTypes.SignTx(ethTypes.NewTx(&ethTypes.DynamicFeeTx{ChainID: big.NewInt(945), Nonce: 4, GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(10), Gas: 55000, To: &plan.Deployment.CoordinatorProxy, Value: new(big.Int), Data: data}), ethTypes.LatestSignerForChainID(big.NewInt(945)), key)
	if err != nil {
		t.Fatal(err)
	}
	start := 32*cfg.Policy.Settlement.EpochBlocks + 1
	before := ChainHead{Number: start + 20, Hash: common.Hash{0x11}.Hex()}
	after := ChainHead{Number: before.Number + 1, Hash: common.Hash{0x12}.Hex()}
	fixture := &productionPolicyRpcFixture{t: t, before: before, after: after, current: before, callKVs: map[uint64]map[string]string{}}
	for index, head := range []ChainHead{before, after} {
		outputs := map[string]string{}
		count := 3 + index
		addPolicyRevisionRPCOutput(t, outputs, parsed, "currentEpoch", nil, big.NewInt(32))
		addPolicyRevisionRPCOutput(t, outputs, parsed, "policyCount", nil, big.NewInt(int64(count)))
		addPolicyRevisionRPCOutput(t, outputs, parsed, "policyAt", []any{big.NewInt(32)}, policies[2])
		addPolicyRevisionRPCOutput(t, outputs, parsed, "epochStartBlock", []any{big.NewInt(32)}, new(big.Int).SetUint64(start))
		addPolicyRevisionRPCOutput(t, outputs, parsed, "epochEndBlock", []any{big.NewInt(32)}, new(big.Int).SetUint64(start+cfg.Policy.Settlement.EpochBlocks))
		for policyIndex := 0; policyIndex < count; policyIndex++ {
			addPolicyRevisionRPCOutput(t, outputs, parsed, "policyByIndex", []any{big.NewInt(int64(policyIndex))}, policies[policyIndex])
		}
		fixture.callKVs[head.Number] = outputs
	}
	event := parsed.Events["PolicyScheduled"]
	eventData, err := event.Inputs.NonIndexed().Pack(production.EffectiveBlock)
	if err != nil {
		t.Fatal(err)
	}
	fixture.receipt = &ethTypes.Receipt{Type: transaction.Type(), Status: ethTypes.ReceiptStatusSuccessful, TxHash: transaction.Hash(), BlockNumber: new(big.Int).SetUint64(after.Number), BlockHash: common.HexToHash(after.Hash), GasUsed: 40000, CumulativeGasUsed: 40000, EffectiveGasPrice: big.NewInt(6), Logs: []*ethTypes.Log{{Address: plan.Deployment.CoordinatorProxy, Topics: []common.Hash{event.ID, common.BigToHash(big.NewInt(3)), common.BytesToHash(production.PolicyHash[:]), common.BigToHash(new(big.Int).SetUint64(production.EffectiveEpoch))}, Data: eventData}}}
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	action := Action{ID: "policy.schedule-production", IntentHash: common.Hash{0x72}.Hex(), Kind: "evm-transaction", Parameters: map[string]string{evmMaximumGasUnitsParameter: "100000", evmMaximumFeePerGasParameter: "10"}, Spend: Spend{EVMGasWei: DecimalUint("1000000")}}
	if err := journal.Append(JournalEntry{DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	owner := &EvmTxManager{client: client, chainID: big.NewInt(945), deploymentID: cfg.Config.Deployment.DeploymentID, stateDir: stateDir, journal: journal, key: key}
	executor := &Executor{cfg: cfg, plan: plan, roles: roles, stateDir: stateDir, journal: journal, owner: owner, releaseGate: gate, payloads: &DeploymentPayloads{Manifest: plan.Deployment}}
	for range 2 {
		if err := executor.scheduleProductionPolicy(t.Context(), action); err != nil {
			t.Fatalf("approved three-to-four transition failed: %v", err)
		}
	}
	evidencePath := filepath.Join(stateDir, "public", "production-policy.json")
	if err := os.Remove(evidencePath); err != nil {
		t.Fatal(err)
	}
	if err := executor.scheduleProductionPolicy(t.Context(), action); err != nil {
		t.Fatalf("missing evidence could not recover the approved receipt: %v", err)
	}
	for range 2 {
		state, err := executor.verifyProductionPolicyPostState(t.Context(), after, map[string]any{})
		if err != nil || state["policy_count"] != "4" || state["effective_epoch"] != production.EffectiveEpoch {
			t.Fatalf("four-policy postcondition failed: state=%v error=%v", state, err)
		}
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil || len(entries) != 4 {
		t.Fatalf("reentry changed the transaction journal: entries=%d error=%v", len(entries), err)
	}
	for index, stage := range []JournalStage{StageIntent, StageBroadcast, StageIncluded, StageFinalized} {
		if entries[index].Stage != stage || index > 0 && entries[index].TransactionHash != transaction.Hash().Hex() {
			t.Fatalf("transaction history changed at entry %d: %+v", index, entries[index])
		}
	}
	fixture.stateLock.Lock()
	nonceReads := fixture.nonceReads
	unpinnedReads := fixture.unpinnedReads
	fixture.stateLock.Unlock()
	if nonceReads != 1 {
		t.Fatalf("idempotent recovery acquired %d transaction nonces", nonceReads)
	}
	if unpinnedReads != 0 {
		t.Fatalf("production transition made %d unpinned contract reads", unpinnedReads)
	}
	var evidence ProductionPolicyEvidence
	if err := decodeStrictJSONFile(evidencePath, &evidence); err != nil || evidence.TransactionHash != transaction.Hash().Hex() {
		t.Fatalf("recovered evidence lost its exact transaction: %+v %v", evidence, err)
	}
	// A verifier cannot adopt a plausible last policy after losing amendment
	// authorization. This is the same reader used by already-scheduled reentry.
	plan.PolicyRateAmendment = nil
	if _, err := executor.verifyProductionPolicyPostState(t.Context(), after, map[string]any{}); err == nil {
		t.Fatal("receipt verification accepted four policies without approval")
	}
	if err := executor.scheduleProductionPolicy(t.Context(), action); err == nil {
		t.Fatal("reentry accepted four policies without approval")
	}
}

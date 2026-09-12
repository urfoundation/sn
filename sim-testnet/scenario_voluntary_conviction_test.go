package main

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

func scenarioVoluntaryConvictionFixture(t *testing.T) (*ResolvedConfig, *SetupPlan, *SetupPlan, VoluntaryConvictionEvidence, []JournalEntry) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	current := *source
	current.PriorPlanHashes = []string{source.PlanHash}
	current.PolicyHash = "0x" + strings.Repeat("ab", 32)
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	cfg.PolicyHash = current.PolicyHash
	evidence := VoluntaryConvictionEvidence{
		Schema: "urnetwork-voluntary-conviction-evidence-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		NoID: 1, Epoch: 2, AmountRao: "1000000000", BeforeConvictionRao: "0", AfterConvictionRao: "1000000000", Nonce: "0",
		Funder: source.Roles.OperatorDepositSigners[0], PolicyHash: source.PolicyHash,
		TransactionHash: "0x" + strings.Repeat("11", 32), FinalizedBlock: 9, FinalizedHash: "0x" + strings.Repeat("22", 32),
	}
	action := actionByID(t, source, voluntaryConvictionActionID)
	entries := []JournalEntry{
		{Sequence: 1, DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: evidence.TransactionHash, BlockNumber: evidence.FinalizedBlock, BlockHash: evidence.FinalizedHash},
		{Sequence: 2, DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified},
	}
	return cfg, source, &current, evidence, entries
}

// Exercise the actual scenario observer with hash-authenticated plan/journal
// files and finalized JSON-RPC receipts, including the incident's policy change.
func TestScenarioVoluntaryConvictionUsesFinalizedAncestorPolicy(t *testing.T) {
	cfg, source, current, evidence, entries := scenarioVoluntaryConvictionFixture(t)
	root := t.TempDir()
	writeJSON := func(path string, value any) {
		t.Helper()
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(filepath.Join(root, "plan.json"), current)
	writeJSON(filepath.Join(root, "plans", stringsTrim0x(source.PlanHash)+".json"), source)
	writeJSON(filepath.Join(root, "public", "voluntary-conviction.json"), evidence)
	executor := &Executor{cfg: cfg, plan: current, stateDir: root}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: source.DeploymentID, PlanHash: source.PlanHash,
		ActionID: entries[1].ActionID, IntentHash: entries[1].IntentHash,
		OperationalRPCMode: cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(cfg),
		EVMHashDomain: "evm-rpc", IndependentEVMHashDomain: "evm-rpc",
		SubstrateFinalized: ChainHead{Number: 10, Hash: evidence.FinalizedHash}, EVMFinalized: ChainHead{Number: 10, Hash: evidence.FinalizedHash},
		IndependentSubstrateFinalized: ChainHead{Number: 10, Hash: evidence.FinalizedHash}, IndependentEVMFinalized: ChainHead{Number: 10, Hash: evidence.FinalizedHash},
		Observed: map[string]any{"transaction_hash": evidence.TransactionHash}, IndependentObserved: map[string]any{"transaction_hash": evidence.TransactionHash},
	}
	var err error
	entries[1].PostconditionPath, entries[1].PostconditionHash, err = executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := journal.Append(entry); err != nil {
			_ = journal.Close()
			t.Fatal(err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := decodeHash(evidence.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	event := parsed.Events["ConvictionAdded"]
	data, err := event.Inputs.NonIndexed().Pack(big.NewInt(1_000_000_000), policy, big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	receipt := &ethTypes.Receipt{
		Status: ethTypes.ReceiptStatusSuccessful, TxHash: common.HexToHash(evidence.TransactionHash), BlockNumber: big.NewInt(9), BlockHash: common.HexToHash(evidence.FinalizedHash),
		Logs: []*ethTypes.Log{{Address: source.Deployment.CoordinatorProxy, Topics: []common.Hash{event.ID, common.BigToHash(big.NewInt(1)), common.BigToHash(big.NewInt(2)), common.BytesToHash(common.HexToAddress(evidence.Funder).Bytes())}, Data: data}},
	}
	var badCanonical atomic.Bool
	var receiptReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var call struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Error(err)
			http.Error(w, "invalid RPC", http.StatusBadRequest)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
		switch call.Method {
		case "eth_getBlockByNumber":
			number, hash := "0xa", evidence.FinalizedHash
			if len(call.Params) == 2 && string(call.Params[0]) == `"0x9"` {
				number = "0x9"
				if badCanonical.Load() {
					hash = "0x" + strings.Repeat("33", 32)
				}
			}
			response["result"] = map[string]any{"number": number, "hash": hash}
		case "eth_getTransactionReceipt":
			receiptReads.Add(1)
			response["result"] = receipt
		case "eth_call":
			response["result"] = "0x" + common.Bytes2Hex(common.LeftPadBytes(big.NewInt(1_000_000_000).Bytes(), 32))
		default:
			t.Errorf("unexpected RPC %s", call.Method)
			response["error"] = map[string]any{"code": -32601, "message": "unexpected RPC"}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	cfg.OperationalEVM = server.URL
	contracts := &ContractView{Deployment: &source.Deployment}
	_, valid, detail := inspectVoluntaryConviction(t.Context(), cfg, root, contracts)
	if !valid || detail != "" || receiptReads.Load() != 1 {
		t.Fatalf("finalized ancestor conviction failed after policy revision: valid=%t detail=%q receipt_reads=%d", valid, detail, receiptReads.Load())
	}
	badCanonical.Store(true)
	if _, valid, detail := inspectVoluntaryConviction(t.Context(), cfg, root, contracts); valid || !strings.Contains(detail, "receipt mismatch") {
		t.Fatalf("noncanonical ancestor receipt accepted: valid=%t detail=%q", valid, detail)
	}
}

func TestScenarioVoluntaryConvictionRejectsUnrelatedLineage(t *testing.T) {
	cfg, source, current, evidence, entries := scenarioVoluntaryConvictionFixture(t)
	load := func(hash string) (*SetupPlan, error) {
		if hash != source.PlanHash {
			t.Fatalf("loaded an unrelated source plan %s", hash)
		}
		return source, nil
	}
	if _, err := voluntaryConvictionObservationSourcePlan(cfg, current, entries, evidence, load); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"policy", "transaction", "checkpoint", "unverified", "reversed", "unapproved", "signer", "coordinator"} {
		mutatedEvidence, mutatedPlan, mutatedSource := evidence, *current, *source
		mutatedEntries := append([]JournalEntry(nil), entries...)
		switch mutation {
		case "policy":
			mutatedEvidence.PolicyHash = current.PolicyHash
		case "transaction":
			mutatedEvidence.TransactionHash = "0x" + strings.Repeat("44", 32)
		case "checkpoint":
			mutatedEntries[0].BlockNumber++
		case "unverified":
			mutatedEntries = mutatedEntries[:1]
		case "reversed":
			mutatedEntries[1].Sequence = mutatedEntries[0].Sequence
		case "unapproved":
			mutatedPlan.PriorPlanHashes = nil
		case "signer":
			mutatedSource.Roles.OperatorDepositSigners = []string{common.HexToAddress("0x1234").Hex()}
		case "coordinator":
			mutatedSource.Deployment.CoordinatorProxy = common.HexToAddress("0x5678")
		}
		if _, err := voluntaryConvictionObservationSourcePlan(cfg, &mutatedPlan, mutatedEntries, mutatedEvidence, func(string) (*SetupPlan, error) { return &mutatedSource, nil }); err == nil {
			t.Errorf("accepted %s mutation of the historical conviction", mutation)
		}
	}
}

package main

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestCheckpointVisibilityRequiresIndependentCanonicalFinality(t *testing.T) {
	private := ChainHead{Number: 10, Hash: "0xabc"}
	if ready, err := checkpointVisibility(private, ChainHead{Number: 9, Hash: "0xdef"}, "0xabc"); err != nil || ready {
		t.Fatalf("lagging independent checkpoint ready=%t err=%v", ready, err)
	}
	if ready, err := checkpointVisibility(private, ChainHead{Number: 10, Hash: "0xdef"}, "0xAbC"); err != nil || !ready {
		t.Fatalf("canonical independent checkpoint ready=%t err=%v", ready, err)
	}
	if _, err := checkpointVisibility(private, ChainHead{Number: 11, Hash: "0xdef"}, "0x999"); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("mismatched independent history accepted: %v", err)
	}
	if _, err := checkpointVisibility(ChainHead{}, ChainHead{Number: 1, Hash: "0x1"}, "0x1"); err == nil {
		t.Fatal("incomplete private checkpoint accepted")
	}
}

func TestIndependentReadExecutorRoutesEveryChainReader(t *testing.T) {
	privateClient := new(ethclient.Client)
	independentClient := new(ethclient.Client)
	privateSubstrate := new(SubstrateManager)
	independentSubstrate := new(SubstrateManager)
	manager := func() *EVMTxManager { return &EVMTxManager{client: privateClient} }
	e := &Executor{
		substrate: privateSubstrate, independentSubstrate: independentSubstrate,
		independentEVM: independentClient, deployer: manager(), owner: manager(), guardian: manager(),
		oracle: manager(), keeper: manager(), deposits: map[int]*EVMTxManager{1: manager(), 2: manager()},
	}
	observed := e.independentReadExecutor()
	if observed == e || observed.substrate != independentSubstrate || observed.deployer.client != independentClient || observed.owner.client != independentClient || observed.guardian.client != independentClient || observed.oracle.client != independentClient || observed.keeper.client != independentClient {
		t.Fatal("independent executor retained a private chain reader")
	}
	for id, value := range observed.deposits {
		if value.client != independentClient || value == e.deposits[id] {
			t.Fatalf("deposit manager %d was not independently cloned", id)
		}
	}
	if e.deployer.client != privateClient || e.substrate != privateSubstrate {
		t.Fatal("building an independent reader mutated the write executor")
	}
}

func TestPersistedPostconditionRequiresIndependentEvidence(t *testing.T) {
	cfg := testResolvedConfig(t)
	plan := &SetupPlan{PlanHash: "0xplan"}
	e := &Executor{cfg: cfg, plan: plan, stateDir: t.TempDir()}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: plan.PlanHash, ActionID: "safe.action", IntentHash: "0xintent",
		OperationalRPCMode: rpcModePrivateAuthority, IndependentRPC: true,
		SubstrateFinalized:            ChainHead{Number: 9, Hash: "0xprivate-substrate"},
		EVMFinalized:                  ChainHead{Number: 9, Hash: "0xprivate-evm"},
		Observed:                      map[string]any{"ready": true},
		IndependentSubstrateFinalized: ChainHead{Number: 10, Hash: "0xpublic-substrate"},
		IndependentEVMFinalized:       ChainHead{Number: 10, Hash: "0xpublic-evm"},
		IndependentObserved:           map[string]any{"ready": true},
	}
	path, hash, err := e.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{ActionID: record.ActionID, IntentHash: record.IntentHash, PostconditionPath: path, PostconditionHash: hash}
	if err := e.verifyPersistedPostcondition(entry); err != nil {
		t.Fatalf("complete independent postcondition was rejected: %v", err)
	}

	record.IndependentObserved = nil
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.stateDir, filepath.FromSlash(path)), b, 0o600); err != nil {
		t.Fatal(err)
	}
	entry.PostconditionHash, err = canonicalHashHex(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.verifyPersistedPostcondition(entry); err == nil || !strings.Contains(err.Error(), "independent RPC evidence") {
		t.Fatalf("postcondition without independent observations was accepted: %v", err)
	}
}

func TestVoluntaryConvictionPostconditionIdentityAndEventAreExact(t *testing.T) {
	cfg := testResolvedConfig(t)
	funder := common.HexToAddress("0x0000000000000000000000000000000000001234")
	plan := &SetupPlan{Roles: PublicRoles{OperatorDepositSigners: []string{funder.Hex()}}}
	policy, err := decodeHash(cfg.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	evidence := VoluntaryConvictionEvidence{
		Schema: "urnetwork-voluntary-conviction-evidence-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		NoID: 1, Epoch: 3, AmountRao: "1000000000", BeforeConvictionRao: "0", AfterConvictionRao: "1000000000",
		Nonce: "4", Funder: funder.Hex(), PolicyHash: cfg.PolicyHash,
		TransactionHash: "0x" + strings.Repeat("11", 32), FinalizedBlock: 9, FinalizedHash: "0x" + strings.Repeat("22", 32),
	}
	if err := voluntaryConvictionEvidenceMatches(cfg, plan, evidence); err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	event := parsed.Events["ConvictionAdded"]
	data, err := event.Inputs.NonIndexed().Pack(big.NewInt(1_000_000_000), policy, big.NewInt(4))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := common.HexToAddress("0x0000000000000000000000000000000000005678")
	receipt := &ethTypes.Receipt{Logs: []*ethTypes.Log{{Address: coordinator, Topics: []common.Hash{event.ID, common.BigToHash(big.NewInt(1)), common.BigToHash(big.NewInt(3)), common.BytesToHash(common.LeftPadBytes(funder.Bytes(), 32))}, Data: data}}}
	if err := voluntaryConvictionReceiptMatches(receipt, coordinator, evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Nonce = "5"
	if err := voluntaryConvictionReceiptMatches(receipt, coordinator, evidence); err == nil {
		t.Fatal("wrong voluntary conviction nonce was accepted")
	}
	evidence.Nonce = "4"
	evidence.Funder = common.HexToAddress("0x9999").Hex()
	if err := voluntaryConvictionEvidenceMatches(cfg, plan, evidence); err == nil {
		t.Fatal("wrong voluntary conviction funder was accepted")
	}
}

func TestProductionPolicyEvidenceRequiresCompleteCanonicalCadence(t *testing.T) {
	cfg := testResolvedConfig(t)
	p := cfg.Policy.ProductionCadence
	evidence := ProductionPolicyEvidence{
		Schema: "urnetwork-production-policy-evidence-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PolicyHash: cfg.PolicyHash, EffectiveEpoch: p.AfterAcceleratedEpochs, EffectiveBlock: 100,
		PriorEpochBlocks: cfg.Policy.Settlement.EpochBlocks, EpochBlocks: p.EpochBlocks,
		RootCommitWindowBlocks: p.RootCommitWindowBlocks, FinalizeOffsetBlocks: p.FinalizeOffsetBlocks, CloseGraceBlocks: p.CloseGraceBlocks,
	}
	if !productionPolicyEvidenceMatches(cfg, evidence) {
		t.Fatal("canonical production cadence evidence was rejected")
	}
	evidence.CloseGraceBlocks++
	if productionPolicyEvidenceMatches(cfg, evidence) {
		t.Fatal("mutated production cadence evidence was accepted")
	}
}

func TestFinalizedActionBlockBindsPlanActionAndIntent(t *testing.T) {
	action := Action{ID: "operator.register.1", IntentHash: "intent-a"}
	entries := []JournalEntry{
		{PlanHash: "plan-a", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIncluded, BlockNumber: 10},
		{PlanHash: "plan-b", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, BlockNumber: 11},
		{PlanHash: "plan-a", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, BlockNumber: 12},
	}
	if block, err := finalizedActionBlock(entries, "plan-a", action); err != nil || block != 12 {
		t.Fatalf("finalized block = %d, %v", block, err)
	}
	action.IntentHash = "intent-b"
	if _, err := finalizedActionBlock(entries, "plan-a", action); err == nil {
		t.Fatal("wrong action intent found a finalized transaction")
	}
}

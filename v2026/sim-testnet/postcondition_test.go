package main

import (
	"context"
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
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

func TestStateMismatchErrorPreservesCausesWithoutFormattingNilWraps(t *testing.T) {
	semantic := stateMismatchError(nil, "value=%d differs", 7)
	if semantic.Error() != "value=7 differs" || strings.Contains(semantic.Error(), "%!") {
		t.Fatalf("semantic mismatch diagnostic = %q", semantic)
	}
	cause := errors.New("read failed")
	wrapped := stateMismatchError(cause, "value unavailable")
	if !errors.Is(wrapped, cause) || wrapped.Error() != "value unavailable: read failed" {
		t.Fatalf("wrapped mismatch diagnostic = %q", wrapped)
	}
}

// Guard every production source file against the incident pattern: a single
// `if err != nil || semanticMismatch` branch returning fmt.Errorf with `%w`
// can format a nil cause when only the semantic half is true.
func TestSemanticMismatchBranchesNeverWrapPotentiallyNilErrors(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	containsOr := func(expression ast.Expr) bool {
		found := false
		ast.Inspect(expression, func(node ast.Node) bool {
			if binary, ok := node.(*ast.BinaryExpr); ok && binary.Op == token.LOR {
				found = true
				return false
			}
			return true
		})
		return found
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		set := token.NewFileSet()
		file, parseErr := parser.ParseFile(set, entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", entry.Name(), parseErr)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			statement, ok := node.(*ast.IfStmt)
			if !ok || !containsOr(statement.Cond) {
				return true
			}
			potentiallyNil := map[string]bool{}
			ast.Inspect(statement.Cond, func(condition ast.Node) bool {
				binary, ok := condition.(*ast.BinaryExpr)
				if !ok || binary.Op != token.NEQ {
					return true
				}
				left, leftOK := binary.X.(*ast.Ident)
				right, rightOK := binary.Y.(*ast.Ident)
				if leftOK && rightOK && right.Name == "nil" {
					potentiallyNil[left.Name] = true
				}
				return true
			})
			for _, bodyStatement := range statement.Body.List {
				returned, ok := bodyStatement.(*ast.ReturnStmt)
				if !ok {
					continue
				}
				for _, result := range returned.Results {
					call, ok := result.(*ast.CallExpr)
					if !ok || len(call.Args) < 2 {
						continue
					}
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					packageName, packageOK := selector.X.(*ast.Ident)
					formatLiteral, formatOK := call.Args[0].(*ast.BasicLit)
					if !packageOK || packageName.Name != "fmt" || selector.Sel.Name != "Errorf" || !formatOK {
						continue
					}
					format, unquoteErr := strconv.Unquote(formatLiteral.Value)
					if unquoteErr != nil || !strings.Contains(format, "%w") {
						continue
					}
					for _, argument := range call.Args[1:] {
						if identifier, identifierOK := argument.(*ast.Ident); identifierOK && potentiallyNil[identifier.Name] {
							t.Errorf("%s uses %%w with potentially nil %s in an OR mismatch branch", set.Position(call.Pos()), identifier.Name)
						}
					}
				}
			}
			return true
		})
	}
}

type historicalFundingRPCFixture struct {
	t             *testing.T
	finalized     ChainHead
	historical    ChainHead
	currentWei    *big.Int
	historicalWei *big.Int
	balanceBlocks []string
}

func (f *historicalFundingRPCFixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	f.t.Helper()
	defer request.Body.Close()
	var call struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
	switch call.Method {
	case "eth_getBlockByNumber":
		var selector string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
			f.t.Errorf("malformed block request: %s", call.Params)
			response["error"] = map[string]any{"code": -32602, "message": "malformed block request"}
			break
		}
		head := f.finalized
		if selector != "finalized" {
			head = f.historical
			want := "0x" + new(big.Int).SetUint64(head.Number).Text(16)
			if selector != want {
				f.t.Errorf("numbered block selector = %q, want %q", selector, want)
			}
		}
		response["result"] = map[string]any{"number": "0x" + new(big.Int).SetUint64(head.Number).Text(16), "hash": head.Hash}
	case "eth_getBalance":
		var selector string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[1], &selector) != nil {
			f.t.Errorf("malformed balance request: %s", call.Params)
			response["error"] = map[string]any{"code": -32602, "message": "malformed balance request"}
			break
		}
		f.balanceBlocks = append(f.balanceBlocks, selector)
		balance := f.currentWei
		if selector == "0x"+new(big.Int).SetUint64(f.historical.Number).Text(16) {
			balance = f.historicalWei
		}
		response["result"] = "0x" + balance.Text(16)
	default:
		response["error"] = map[string]any{"code": -32601, "message": "unexpected method"}
	}
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		f.t.Errorf("encode RPC response: %v", err)
	}
}

func fundingPostconditionObservation(action Action, usableRao, existentialDepositRao uint64, balanceWei *big.Int) map[string]any {
	return map[string]any{
		"kind":                    action.Kind,
		"target":                  action.Target,
		"address":                 common.HexToAddress(action.Target).Hex(),
		"balance_wei":             balanceWei.String(),
		"minimum_wei":             new(big.Int).Mul(new(big.Int).SetUint64(usableRao), new(big.Int).SetUint64(evmWeiPerRao)).String(),
		"existential_deposit_rao": existentialDepositRao,
	}
}

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

func TestRegistrationBalanceObservationAllowsOnlyOneHiddenRuntimeDeposit(t *testing.T) {
	valid := []registrationBalanceObservation{
		{Address: "fresh", EVMBeforeWei: "0", EVMAfterWei: "0", NativeBeforeRao: 0, NativeAfterRao: 500},
		{Address: "sub-deposit", EVMBeforeWei: "0", EVMAfterWei: "0", NativeBeforeRao: 0, NativeAfterRao: 499},
		{Address: "unchanged-empty", EVMBeforeWei: "0", EVMAfterWei: "0", NativeBeforeRao: 0, NativeAfterRao: 0},
		{Address: "existing-liquid", EVMBeforeWei: "100000000000", EVMAfterWei: "100000000000", NativeBeforeRao: 600, NativeAfterRao: 600},
	}
	for _, observation := range valid {
		if err := validateRegistrationBalanceObservation(observation, 500); err != nil {
			t.Errorf("valid %s observation was rejected: %v", observation.Address, err)
		}
	}
}

func TestRegistrationBalanceObservationRejectsEveryAdjacentAccountingDrift(t *testing.T) {
	tests := []registrationBalanceObservation{
		{Address: "liquid retained", EVMBeforeWei: "0", EVMAfterWei: "1", NativeBeforeRao: 0, NativeAfterRao: 500},
		{Address: "native view mismatch", EVMBeforeWei: "0", EVMAfterWei: "0", NativeBeforeRao: 0, NativeAfterRao: 501},
		{Address: "existing consumed", EVMBeforeWei: "100000000000", EVMAfterWei: "0", NativeBeforeRao: 600, NativeAfterRao: 500},
		{Address: "multiple deposits retained", EVMBeforeWei: "0", EVMAfterWei: "0", NativeBeforeRao: 0, NativeAfterRao: 1_000},
		{Address: "malformed EVM", EVMBeforeWei: "x", EVMAfterWei: "0", NativeBeforeRao: 0, NativeAfterRao: 0},
	}
	for _, observation := range tests {
		if err := validateRegistrationBalanceObservation(observation, 500); err == nil {
			t.Errorf("%s observation was accepted", observation.Address)
		}
	}
}

// Records which runtime policy was selected without requiring a live RPC.
// Distinct return ranges make an accidental route visible to the caller too.
type registrationNativeBalanceFixture struct {
	currentBlocks        []uint64
	releaseHistoryBlocks []uint64
}

func (self *registrationNativeBalanceFixture) FreeBalanceAtBlock(_ [32]byte, block uint64) (uint64, error) {
	self.currentBlocks = append(self.currentBlocks, block)
	return block + 1_000_000, nil
}

func (self *registrationNativeBalanceFixture) releaseHistoryFreeBalanceAtBlock(_ [32]byte, block uint64) (uint64, error) {
	self.releaseHistoryBlocks = append(self.releaseHistoryBlocks, block)
	return block + 2_000_000, nil
}

func TestCarriedRegistrationBalanceUsesReviewedRuntimeHistoryAtUpgradeBoundary(t *testing.T) {
	action := Action{ID: "evm.vault-register-escrow", IntentHash: "ancestor-intent"}
	ancestorPlanHash := "0x" + strings.Repeat("aa", 32)
	activePlanHash := "0x" + strings.Repeat("bb", 32)
	entries := []JournalEntry{
		{PlanHash: ancestorPlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, BlockNumber: 7_895_362},
		{PlanHash: ancestorPlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/ancestor.json"},
	}
	executor := &Executor{plan: &SetupPlan{PlanHash: activePlanHash, PriorPlanHashes: []string{ancestorPlanHash}}, journal: &Journal{entries: entries}}
	block, releaseHistory, err := executor.finalizedRegistrationActionCheckpoint(action)
	if err != nil || block != 7_895_362 || !releaseHistory {
		t.Fatalf("carried registration checkpoint = %d history=%t, want 7895362/true: %v", block, releaseHistory, err)
	}
	reader := &registrationNativeBalanceFixture{}
	before, err := readRegistrationNativeBalance(reader, releaseHistory, [32]byte{1}, block-1)
	if err != nil {
		t.Fatal(err)
	}
	after, err := readRegistrationNativeBalance(reader, releaseHistory, [32]byte{1}, block)
	if err != nil {
		t.Fatal(err)
	}
	if before != 9_895_361 || after != 9_895_362 || len(reader.currentBlocks) != 0 || !slices.Equal(reader.releaseHistoryBlocks, []uint64{7_895_361, 7_895_362}) {
		t.Fatalf("carried registration reads current=%v history=%v values=%d/%d", reader.currentBlocks, reader.releaseHistoryBlocks, before, after)
	}
}

func TestCurrentRegistrationBalanceRemainsStrictlyReleaseRuntimeBound(t *testing.T) {
	action := Action{ID: "operator.register.1", IntentHash: "current-intent"}
	activePlanHash := "0x" + strings.Repeat("cc", 32)
	entries := []JournalEntry{
		{PlanHash: activePlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, BlockNumber: 8_000_000},
		{PlanHash: activePlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/current.json"},
	}
	executor := &Executor{plan: &SetupPlan{PlanHash: activePlanHash}, journal: &Journal{entries: entries}}
	block, releaseHistory, err := executor.finalizedRegistrationActionCheckpoint(action)
	if err != nil || block != 8_000_000 || releaseHistory {
		t.Fatalf("current registration checkpoint = %d history=%t, want 8000000/false: %v", block, releaseHistory, err)
	}
	reader := &registrationNativeBalanceFixture{}
	value, err := readRegistrationNativeBalance(reader, releaseHistory, [32]byte{2}, block)
	if err != nil || value != 9_000_000 || !slices.Equal(reader.currentBlocks, []uint64{8_000_000}) || len(reader.releaseHistoryBlocks) != 0 {
		t.Fatalf("current registration reads current=%v history=%v value=%d err=%v", reader.currentBlocks, reader.releaseHistoryBlocks, value, err)
	}
}

func TestRegistrationBalanceReaderRejectsMissingDependency(t *testing.T) {
	if _, err := readRegistrationNativeBalance(nil, true, [32]byte{}, 7_895_361); err == nil {
		t.Fatal("missing registration native balance reader was accepted")
	}
}

func TestEVMCheckpointComparesOnlyCanonicalEVMRPCHashes(t *testing.T) {
	canonical := testEVMHead(20, 0x20)
	fixture := &evmFinalityFixture{canonical: map[uint64]ChainHead{20: canonical}}
	recorded := canonical
	finalized := testEVMHead(21, 0x21)
	if err := verifyEVMCheckpointFromReader(context.Background(), fixture, finalized, recorded); err != nil {
		t.Fatal(err)
	}
	substrateDomain := recorded
	substrateDomain.Hash = "0x49ad70213ee4795f1c8ecdef0f32717ccce04dedac3eb63b523d14b82a823935"
	if err := verifyEVMCheckpointFromReader(context.Background(), fixture, finalized, substrateDomain); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("cross-domain checkpoint error = %v", err)
	}
	behind := testEVMHead(19, 0x19)
	if err := verifyEVMCheckpointFromReader(context.Background(), fixture, behind, recorded); err == nil || !strings.Contains(err.Error(), "not finalized") {
		t.Fatalf("unfinalized checkpoint error = %v", err)
	}
}

func TestConsumedEVMFundingHistoryReplaysConvergedBalanceAfterGasSpend(t *testing.T) {
	const usableRao = uint64(1_000)
	const existentialDepositRao = uint64(500)
	wei := func(rao uint64) *big.Int {
		return new(big.Int).Mul(new(big.Int).SetUint64(rao), new(big.Int).SetUint64(evmWeiPerRao))
	}
	fixture := &historicalFundingRPCFixture{
		t:         t,
		finalized: testEVMHead(12, 0x12), historical: testEVMHead(10, 0x10),
		currentWei: wei(999), historicalWei: wei(1_100),
	}
	server := httptest.NewServer(fixture)
	defer server.Close()
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	ancestorHash := "0x" + strings.Repeat("aa", 32)
	action := Action{
		ID: "evm.fund-operator-2-deposit", Kind: "substrate-extrinsic",
		Target: common.HexToAddress("0x521").Hex(), IntentHash: "same-intent",
		Parameters: map[string]string{"usable_evm_rao": "1000", "existential_deposit_rao": "500"},
		Spend:      Spend{TAORao: usableRao + existentialDepositRao},
	}
	plan := &SetupPlan{
		PlanHash: "0x" + strings.Repeat("bb", 32), PriorPlanHashes: []string{ancestorHash},
		LiveFacts: SetupFacts{ExistentialDepositRao: existentialDepositRao}, Actions: []Action{action},
	}
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	executor := &Executor{
		cfg: cfg, stateDir: stateDir, plan: plan, journal: journal,
		deployer: &EVMTxManager{client: client}, independentEVM: client,
	}
	observed := fundingPostconditionObservation(action, usableRao, existentialDepositRao, fixture.historicalWei)
	independentObserved, err := cloneObservedPostState(observed)
	if err != nil {
		t.Fatal(err)
	}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: ancestorHash, ActionID: action.ID, IntentHash: action.IntentHash,
		OperationalRPCMode: rpcModePublicOverride, IndependentRPC: false,
		SubstrateFinalized: ChainHead{Number: 10, Hash: "0x" + strings.Repeat("31", 32)},
		EVMFinalized:       fixture.historical, EVMHashDomain: "evm-rpc", Observed: observed,
		IndependentSubstrateFinalized: ChainHead{Number: 10, Hash: "0x" + strings.Repeat("31", 32)},
		IndependentEVMFinalized:       fixture.historical, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: independentObserved,
	}
	path, digest, err := executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{
		DeploymentID: cfg.Config.Deployment.DeploymentID, PlanHash: ancestorHash,
		ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified,
		PostconditionPath: path, PostconditionHash: digest,
	}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	authenticated, err := executor.readPersistedPostcondition(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.verifyConsumedActionHistory(context.Background(), action, entry, authenticated); err != nil {
		t.Fatalf("converged funding history was not replayed: %v", err)
	}
	historicalSelector := "0x" + new(big.Int).SetUint64(fixture.historical.Number).Text(16)
	if len(fixture.balanceBlocks) != 1 || fixture.balanceBlocks[0] != historicalSelector {
		t.Fatalf("historical balance selectors = %v, want one shared-provider read at %s", fixture.balanceBlocks, historicalSelector)
	}

	// Reproduce the live-state half of the incident and ensure its diagnostic
	// contains no fmt wrapping artifact when the RPC itself succeeded.
	if _, err := executor.actionPostState(context.Background(), action, fixture.finalized); err == nil || !strings.Contains(err.Error(), "balance=999000000000") || strings.Contains(err.Error(), "%!") {
		t.Fatalf("consumed current balance diagnostic = %v", err)
	}
}

func TestConsumedEVMFundingHistoryRejectsEveryAdjacentEvidenceGap(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	action := Action{
		ID: "evm.fund-owner", Kind: "substrate-extrinsic", Target: common.HexToAddress("0x521").Hex(), IntentHash: "same-intent",
		Parameters: map[string]string{"usable_evm_rao": "1000", "existential_deposit_rao": "500"}, Spend: Spend{TAORao: 1_500},
	}
	plan := &SetupPlan{
		PlanHash: "0x" + strings.Repeat("aa", 32), LiveFacts: SetupFacts{ExistentialDepositRao: 500},
		Actions: []Action{action},
	}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		EVMFinalized:            testEVMHead(10, 0x10),
		IndependentEVMFinalized: testEVMHead(10, 0x10),
		Observed:                fundingPostconditionObservation(action, 1_000, 500, new(big.Int).Mul(big.NewInt(1_100), new(big.Int).SetUint64(evmWeiPerRao))),
	}
	record.IndependentObserved, _ = cloneObservedPostState(record.Observed)

	for name, mutate := range map[string]func(*historicalFundingRPCFixture, *ActionPostcondition){
		"historical balance below approval": func(f *historicalFundingRPCFixture, _ *ActionPostcondition) {
			f.historicalWei = new(big.Int).Mul(big.NewInt(999), new(big.Int).SetUint64(evmWeiPerRao))
		},
		"recorded block is noncanonical": func(_ *historicalFundingRPCFixture, r *ActionPostcondition) {
			r.EVMFinalized.Hash = testEVMHead(10, 0x44).Hash
		},
		"recorded observation differs": func(_ *historicalFundingRPCFixture, r *ActionPostcondition) {
			r.Observed["balance_wei"] = "1000000000000"
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := &historicalFundingRPCFixture{
				t: t, finalized: testEVMHead(12, 0x12), historical: testEVMHead(10, 0x10),
				currentWei: big.NewInt(0), historicalWei: new(big.Int).Mul(big.NewInt(1_100), new(big.Int).SetUint64(evmWeiPerRao)),
			}
			cloned, err := durableActionPostcondition(record)
			if err != nil {
				t.Fatal(err)
			}
			mutate(fixture, cloned)
			server := httptest.NewServer(fixture)
			defer server.Close()
			client, err := ethclient.Dial(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			executor := &Executor{cfg: cfg, plan: plan, deployer: &EVMTxManager{client: client}, independentEVM: client}
			if err := executor.verifyConsumedEVMFundingPostcondition(context.Background(), action, cloned); err == nil {
				t.Fatal("invalid historical EVM funding evidence was accepted")
			}
		})
	}

	legacy := *record
	legacy.Schema = "urnetwork-sim-action-postcondition-v2"
	if err := (&Executor{}).verifyConsumedEVMFundingPostcondition(context.Background(), action, &legacy); err == nil || !strings.Contains(err.Error(), "v3+") {
		t.Fatalf("legacy non-RPC-domain history was accepted: %v", err)
	}
	cfg.OperationalRPCMode = rpcModePrivateAuthority
	if err := (&Executor{cfg: cfg, plan: plan}).verifyConsumedEVMFundingPostcondition(context.Background(), action, record); err == nil || !strings.Contains(err.Error(), "operational EVM history client") {
		t.Fatalf("missing operational history reader was accepted: %v", err)
	}
}

func TestUnmutatedSetupTopologyRequiresEveryPlannedIdentityField(t *testing.T) {
	facts := *testSetupFacts()
	owner, err := decodeHex32("owner", facts.SubnetOwnerHotkey)
	if err != nil {
		t.Fatal(err)
	}
	canonical := SubnetTopologyFacts{UIDCount: facts.ExistingUIDCount, OwnerHotkey: owner, UIDZero: owner}
	if err := validateUnmutatedSetupTopology(canonical, facts); err != nil {
		t.Fatal(err)
	}
	tests := []SubnetTopologyFacts{
		{UIDCount: 1, OwnerHotkey: owner, UIDZero: owner},
		{UIDCount: 2, OwnerHotkey: [32]byte{1}, UIDZero: owner},
		{UIDCount: 2, OwnerHotkey: owner, UIDZero: [32]byte{1}},
	}
	for index, changed := range tests {
		if err := validateUnmutatedSetupTopology(changed, facts); err == nil {
			t.Fatalf("changed topology %d was accepted: %+v", index, changed)
		}
	}
	if err := validateUnmutatedExistingUIDs(facts.ExistingUIDs, facts.ExistingUIDs); err != nil {
		t.Fatal(err)
	}
	missing := append([]ExistingUIDFact(nil), facts.ExistingUIDs[:1]...)
	if err := validateUnmutatedExistingUIDs(missing, facts.ExistingUIDs); err == nil {
		t.Fatal("missing existing UID was accepted")
	}
	changedIdentity := append([]ExistingUIDFact(nil), facts.ExistingUIDs...)
	changedIdentity[1].RegistrationBlock++
	if err := validateUnmutatedExistingUIDs(changedIdentity, facts.ExistingUIDs); err == nil {
		t.Fatal("changed existing UID identity was accepted")
	}
}

func TestTopologyRoleSetsExactlySwapChurnFloorForChallengers(t *testing.T) {
	cfg := testResolvedConfig(t)
	initial := initialTopologyRoleLabels(cfg.Config.Topology, 0)
	tournament := tournamentTopologyRoleLabels(cfg.Config.Topology, 0)
	wantControlled := 256 - len(testSetupFacts().ExistingUIDs)
	if len(initial) != wantControlled || len(tournament) != wantControlled {
		t.Fatalf("role set sizes initial=%d tournament=%d, want %d", len(initial), len(tournament), wantControlled)
	}
	toSet := func(labels []string) map[string]bool {
		out := make(map[string]bool, len(labels))
		for _, label := range labels {
			if out[label] {
				t.Fatalf("duplicate topology role %q", label)
			}
			out[label] = true
		}
		return out
	}
	initialSet, tournamentSet := toSet(initial), toSet(tournament)
	removed, added := map[string]bool{}, map[string]bool{}
	for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
		fleet := cfg.Config.Topology.HeadFleets + challenger
		removed[churnHotkeyLabel(challenger)] = true
		added[fleetHotkeyLabel(fleet)] = true
		if !initialSet[churnHotkeyLabel(challenger)] || initialSet[fleetHotkeyLabel(fleet)] {
			t.Fatalf("initial topology does not contain churn %d exclusively", challenger)
		}
		if tournamentSet[churnHotkeyLabel(challenger)] || !tournamentSet[fleetHotkeyLabel(fleet)] {
			t.Fatalf("tournament topology did not replace churn %d with fleet %d", challenger, fleet)
		}
	}
	for label := range initialSet {
		if removed[label] {
			continue
		}
		if !tournamentSet[label] {
			t.Fatalf("initial role %q disappeared from tournament", label)
		}
	}
	for label := range tournamentSet {
		if added[label] {
			continue
		}
		if !initialSet[label] {
			t.Fatalf("unexpected role %q appeared in tournament", label)
		}
	}
}

// Build an exact active-plan journal without involving either chain so phase
// selection failures are deterministic.
func renderedConfigTopologyFixture(t *testing.T, generation uint64, verifiedActionIDs []string) *Executor {
	t.Helper()
	cfg := testResolvedConfig(t)
	actionIDs := []string{"evm.vault-register-escrow"}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		actionIDs = append(actionIDs, fmt.Sprintf("operator.register.%d", operator))
	}
	for challenger := 1; challenger <= cfg.Config.Topology.ChallengerFleets; challenger++ {
		actionIDs = append(actionIDs, fmt.Sprintf("fleet.register.%d", cfg.Config.Topology.HeadFleets+challenger))
	}
	actionIDs = append(actionIDs, "churn.tournament-complete")
	plan := &SetupPlan{
		PlanHash: "active-plan",
		Deployment: ContractDeployment{
			RegistrationRoleGeneration: generation,
		},
	}
	for _, actionID := range actionIDs {
		plan.Actions = append(plan.Actions, Action{ID: actionID, IntentHash: "intent-" + actionID})
	}
	journal := &Journal{}
	for index, actionID := range verifiedActionIDs {
		action := actionByID(t, plan, actionID)
		journal.entries = append(journal.entries, JournalEntry{
			Sequence:   uint64(index + 1),
			PlanHash:   plan.PlanHash,
			ActionID:   action.ID,
			IntentHash: action.IntentHash,
			Stage:      StageVerified,
		})
	}
	return &Executor{cfg: cfg, plan: plan, journal: journal}
}

// Model finalized challenger UID pairs at an exact contiguous progress point.
func renderedConfigChallengerStates(t *testing.T, topology TopologyConfig, registrations int) []challengerChurnState {
	t.Helper()
	if registrations < 0 || registrations > topology.ChallengerFleets {
		t.Fatalf("challenger registrations=%d", registrations)
	}
	maximum := uint16(hyperparameterUint64(testResolvedConfig(t).Hyperparameters.OwnerControlled["max_allowed_uids"]))
	states := make([]challengerChurnState, topology.ChallengerFleets)
	for index := range states {
		expectedUID := uint16(100 + index)
		states[index] = challengerChurnState{
			ExpectedUID: expectedUID, RuntimePruneUID: expectedUID, UIDCount: maximum, MaximumUIDs: maximum,
			ChallengerUID: expectedUID, ChallengerFound: index < registrations,
			ChurnUID: expectedUID, ChurnFound: index >= registrations,
		}
	}
	return states
}

// Reproduce the live source-rerender boundary after both challengers replaced
// generation-one churn identities.
func TestRenderedConfigTopologyUsesCompletedTournamentLineage(t *testing.T) {
	verifiedActionIDs := []string{
		"evm.vault-register-escrow", "operator.register.1", "operator.register.2",
		"fleet.register.201", "fleet.register.202", "churn.tournament-complete",
	}
	executor := renderedConfigTopologyFixture(t, 1, verifiedActionIDs)
	states := renderedConfigChallengerStates(t, executor.cfg.Config.Topology, 2)
	labels, err := executor.renderedConfigTopologyRoleLabelsFromStates(states)
	want := tournamentTopologyRoleLabels(executor.cfg.Config.Topology, 1)
	if err != nil || !slices.Equal(labels, want) {
		t.Fatalf("completed tournament labels differ: labels=%v want=%v err=%v", labels, want, err)
	}
	for _, retired := range []string{churnHotkeyLabel(4), churnHotkeyLabel(5)} {
		if slices.Contains(labels, retired) {
			t.Fatalf("completed tournament still requires replaced identity %s", retired)
		}
	}
}

// Preserve the one-challenger recovery boundary without admitting a later
// registration out of order.
func TestRenderedConfigTopologyUsesVerifiedChallengerPrefix(t *testing.T) {
	verifiedActionIDs := []string{
		"evm.vault-register-escrow", "operator.register.1", "operator.register.2", "fleet.register.201",
	}
	executor := renderedConfigTopologyFixture(t, 1, verifiedActionIDs)
	states := renderedConfigChallengerStates(t, executor.cfg.Config.Topology, 1)
	labels, err := executor.renderedConfigTopologyRoleLabelsFromStates(states)
	want, wantErr := topologyRoleLabelsAtProgress(executor.cfg.Config.Topology, 1, 3, 1)
	if err != nil || wantErr != nil || !slices.Equal(labels, want) {
		t.Fatalf("challenger-prefix labels differ: labels=%v want=%v errors=%v/%v", labels, want, err, wantErr)
	}
}

// Reject journal shapes that cannot arise from the approved sequential
// registration and tournament dependency chain.
func TestRenderedConfigTopologyRejectsUnverifiedOrOutOfOrderLineage(t *testing.T) {
	cfg := testResolvedConfig(t)
	initialStates := renderedConfigChallengerStates(t, cfg.Config.Topology, 0)
	outOfOrderStates := renderedConfigChallengerStates(t, cfg.Config.Topology, 0)
	outOfOrderStates[1] = renderedConfigChallengerStates(t, cfg.Config.Topology, 2)[1]
	prefixStates := renderedConfigChallengerStates(t, cfg.Config.Topology, 1)
	cases := []struct {
		name              string
		verifiedActionIDs []string
		states            []challengerChurnState
		wantError         string
	}{
		{
			name:              "missing contract registration",
			verifiedActionIDs: []string{"evm.vault-register-escrow", "operator.register.1"},
			states:            initialStates,
			wantError:         "operator.register.2",
		},
		{
			name: "out-of-order challenger",
			verifiedActionIDs: []string{
				"evm.vault-register-escrow", "operator.register.1", "operator.register.2", "fleet.register.202",
			},
			states:    outOfOrderStates,
			wantError: "live after an unregistered predecessor",
		},
		{
			name: "unauthorized live challenger",
			verifiedActionIDs: []string{
				"evm.vault-register-escrow", "operator.register.1", "operator.register.2",
			},
			states:    prefixStates,
			wantError: "without an exact transaction",
		},
		{
			name: "premature tournament barrier",
			verifiedActionIDs: []string{
				"evm.vault-register-escrow", "operator.register.1", "operator.register.2",
				"fleet.register.201", "churn.tournament-complete",
			},
			states:    prefixStates,
			wantError: "only 1/2",
		},
	}
	for _, testCase := range cases {
		executor := renderedConfigTopologyFixture(t, 1, testCase.verifiedActionIDs)
		_, err := executor.renderedConfigTopologyRoleLabelsFromStates(testCase.states)
		if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
			t.Errorf("%s error=%v, want %q", testCase.name, err, testCase.wantError)
		}
	}
}

// Do not infer a replacement from a foreign plan, wrong intent, or a journal
// entry that carries no transaction identity.
func TestInterruptedReplacementRequiresExactPlanIntentTransaction(t *testing.T) {
	verifiedActionIDs := []string{"evm.vault-register-escrow", "operator.register.1", "operator.register.2"}
	cases := []JournalEntry{
		{PlanHash: "foreign-plan", ActionID: "fleet.register.201", IntentHash: "intent-fleet.register.201", Stage: StageFinalized, TransactionHash: "0x" + strings.Repeat("11", 32)},
		{PlanHash: "active-plan", ActionID: "fleet.register.201", IntentHash: "wrong-intent", Stage: StageFinalized, TransactionHash: "0x" + strings.Repeat("22", 32)},
		{PlanHash: "active-plan", ActionID: "fleet.register.201", IntentHash: "intent-fleet.register.201", Stage: StageFinalized},
	}
	for index, entry := range cases {
		executor := renderedConfigTopologyFixture(t, 1, verifiedActionIDs)
		executor.journal.entries = append(executor.journal.entries, entry)
		if replacement, found := executor.transactionReplacementForChurn(4); found {
			t.Errorf("case %d accepted replacement %q", index, replacement)
		}
	}
}

// Recover both possible finalized states after a transaction was recorded but
// before its postcondition entry was durable.
func TestRenderedConfigTopologyRecoversInterruptedChallengerTransaction(t *testing.T) {
	verifiedActionIDs := []string{"evm.vault-register-escrow", "operator.register.1", "operator.register.2"}
	executor := renderedConfigTopologyFixture(t, 1, verifiedActionIDs)
	action := actionByID(t, executor.plan, "fleet.register.201")
	executor.journal.entries = append(executor.journal.entries, JournalEntry{
		Sequence: 4, PlanHash: executor.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		Stage: StageFinalized, TransactionHash: "0x" + strings.Repeat("12", 32),
	})
	postStates := renderedConfigChallengerStates(t, executor.cfg.Config.Topology, 1)
	labels, err := executor.renderedConfigTopologyRoleLabelsFromStates(postStates)
	wantPost, wantPostErr := topologyRoleLabelsAtProgress(executor.cfg.Config.Topology, 1, 3, 1)
	if err != nil || wantPostErr != nil || !slices.Equal(labels, wantPost) {
		t.Fatalf("finalized interrupted challenger labels=%v want=%v errors=%v/%v", labels, wantPost, err, wantPostErr)
	}
	preStates := renderedConfigChallengerStates(t, executor.cfg.Config.Topology, 0)
	labels, err = executor.renderedConfigTopologyRoleLabelsFromStates(preStates)
	wantPre := initialTopologyRoleLabels(executor.cfg.Config.Topology, 1)
	if err != nil || !slices.Equal(labels, wantPre) {
		t.Fatalf("dropped interrupted challenger labels=%v want=%v err=%v", labels, wantPre, err)
	}
	replacement, found := executor.transactionReplacementForChurn(4)
	if !found || replacement != fleetHotkeyLabel(201) {
		t.Fatalf("interrupted churn replacement=%q found=%t", replacement, found)
	}
}

// Exercise the exact persisted deployment lineage without making an RPC call.
func TestLiveRenderedConfigTopologyLineage(t *testing.T) {
	if os.Getenv("SIM_TESTNET_LIVE_TOPOLOGY_LINEAGE") != "1" {
		t.Skip("set SIM_TESTNET_LIVE_TOPOLOGY_LINEAGE=1 to verify the persisted live topology phase")
	}
	stateDir := os.Getenv("SIM_TESTNET_STATE_DIR")
	if stateDir == "" {
		t.Fatal("SIM_TESTNET_STATE_DIR is required")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := readPersistedPlan(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: cfg, plan: plan, journal: &Journal{entries: entries}}
	states := renderedConfigChallengerStates(t, cfg.Config.Topology, cfg.Config.Topology.ChallengerFleets)
	labels, err := executor.renderedConfigTopologyRoleLabelsFromStates(states)
	if err != nil {
		t.Fatal(err)
	}
	want := tournamentTopologyRoleLabels(cfg.Config.Topology, plan.Deployment.RegistrationRoleGeneration)
	if !slices.Equal(labels, want) {
		t.Fatalf("persisted topology phase differs from the completed tournament: labels=%v want=%v", labels, want)
	}
	t.Logf("plan=%s journal_entries=%d registration_generation=%d controlled_roles=%d", plan.PlanHash, len(entries), plan.Deployment.RegistrationRoleGeneration, len(labels))
}

// Re-run the failed config-render postcondition against the finalized network
// and existing immutable runtime files without executing an action.
func TestLiveRenderedConfigPostcondition(t *testing.T) {
	if os.Getenv("SIM_TESTNET_LIVE_RENDERED_CONFIG_POSTCONDITION") != "1" {
		t.Skip("set SIM_TESTNET_LIVE_RENDERED_CONFIG_POSTCONDITION=1 to verify the full live config postcondition")
	}
	stateDir := os.Getenv("SIM_TESTNET_STATE_DIR")
	if stateDir == "" {
		t.Fatal("SIM_TESTNET_STATE_DIR is required")
	}
	cfg, err := LoadResolved(LoadOptions{ConfigPath: "testnet.yml", RequireSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := readPersistedPlan(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	var roles RoleSecrets
	if err := readJSONFile(filepath.Join(stateDir, "secrets", "roles.json"), &roles); err != nil {
		t.Fatal(err)
	}
	substrate, err := DialSubstrateManager(cfg, stateDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer substrate.Close()
	executor := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, journal: &Journal{entries: entries}, roles: &roles, substrate: substrate}
	state, err := executor.actionPostState(context.Background(), actionByID(t, plan, "config.render"), ChainHead{})
	if err != nil {
		t.Fatal(err)
	}
	wantRoles := len(tournamentTopologyRoleLabels(cfg.Config.Topology, plan.Deployment.RegistrationRoleGeneration))
	wantUIDCount := uint16(wantRoles + len(plan.LiveFacts.ExistingUIDs))
	if state["initial_registered_roles"] != wantRoles || state["uid_count"] != wantUIDCount {
		t.Fatalf("live runtime topology state=%v", state)
	}
	t.Logf("plan=%s roles=%v uid_count=%v manifest=%v", plan.PlanHash, state["initial_registered_roles"], state["uid_count"], state["runtime_config_manifest_hash"])
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
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("ab", 32)}
	e := &Executor{cfg: cfg, plan: plan, stateDir: t.TempDir()}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v3", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: plan.PlanHash, ActionID: "safe.action", IntentHash: "0xintent",
		OperationalRPCMode: rpcModePrivateAuthority, IndependentRPC: true,
		SubstrateFinalized:            ChainHead{Number: 9, Hash: "0xprivate-substrate"},
		EVMFinalized:                  ChainHead{Number: 9, Hash: "0xprivate-evm"},
		EVMHashDomain:                 "evm-rpc",
		Observed:                      map[string]any{"ready": true},
		IndependentSubstrateFinalized: ChainHead{Number: 10, Hash: "0xpublic-substrate"},
		IndependentEVMFinalized:       ChainHead{Number: 10, Hash: "0xpublic-evm"},
		IndependentEVMHashDomain:      "evm-rpc",
		IndependentObserved:           map[string]any{"ready": true},
	}
	path, hash, err := e.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{PlanHash: plan.PlanHash, ActionID: record.ActionID, IntentHash: record.IntentHash, PostconditionPath: path, PostconditionHash: hash}
	if err := e.verifyPersistedPostcondition(entry); err != nil {
		t.Fatalf("complete independent postcondition was rejected: %v", err)
	}

	record.EVMHashDomain = ""
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
	if err := e.verifyPersistedPostcondition(entry); err == nil || !strings.Contains(err.Error(), "hash domain") {
		t.Fatalf("postcondition without an EVM RPC hash domain was accepted: %v", err)
	}
	record.EVMHashDomain = "evm-rpc"
	record.IndependentObserved = nil
	b, err = json.Marshal(record)
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

func TestPersistedV2PostconditionRetainsAuthenticatedRecoveryIdentity(t *testing.T) {
	cfg := testResolvedConfig(t)
	plan := &SetupPlan{PlanHash: "0x" + strings.Repeat("cd", 32)}
	executor := &Executor{cfg: cfg, plan: plan, stateDir: t.TempDir()}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v2", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: plan.PlanHash, ActionID: "evm.reserve-sink", IntentHash: "same-intent",
		OperationalRPCMode: rpcModePrivateAuthority, IndependentRPC: true,
		SubstrateFinalized:            ChainHead{Number: 9, Hash: "0xprivate-substrate"},
		EVMFinalized:                  ChainHead{Number: 9, Hash: "0xlocally-recomputed-header"},
		EVMHashDomain:                 "ethereum",
		Observed:                      map[string]any{"runtime_code_matches": true},
		IndependentSubstrateFinalized: ChainHead{Number: 10, Hash: "0xpublic-substrate"},
		IndependentEVMFinalized:       ChainHead{Number: 10, Hash: "0xlocally-recomputed-header"},
		IndependentEVMHashDomain:      "ethereum",
		IndependentObserved:           map[string]any{"runtime_code_matches": true},
	}
	path, hash, err := executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{PlanHash: plan.PlanHash, ActionID: record.ActionID, IntentHash: record.IntentHash, PostconditionPath: path, PostconditionHash: hash}
	if err := executor.verifyPersistedPostcondition(entry); err != nil {
		t.Fatalf("authenticated v2 recovery receipt was rejected: %v", err)
	}
	record.EVMHashDomain = "evm-rpc"
	recordBytes, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(executor.stateDir, filepath.FromSlash(path)), recordBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	entry.PostconditionHash, err = canonicalHashHex(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.verifyPersistedPostcondition(entry); err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("v2 receipt with a rewritten hash domain was accepted: %v", err)
	}
}

func TestPlanScopedPostconditionsPreserveAndVerifyAncestorEvidence(t *testing.T) {
	cfg := testResolvedConfig(t)
	ancestorHash := "0x" + strings.Repeat("aa", 32)
	activeHash := "0x" + strings.Repeat("bb", 32)
	stateDir := t.TempDir()
	executor := &Executor{cfg: cfg, plan: &SetupPlan{PlanHash: ancestorHash}, stateDir: stateDir}
	buildRecord := func(planHash, observed string) *ActionPostcondition {
		return &ActionPostcondition{
			Schema: "urnetwork-sim-action-postcondition-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
			PlanHash: planHash, ActionID: "same.action", IntentHash: "same-intent",
			OperationalRPCMode: rpcModePrivateAuthority, IndependentRPC: true,
			SubstrateFinalized:            ChainHead{Number: 9, Hash: "0xprivate-substrate"},
			EVMFinalized:                  ChainHead{Number: 9, Hash: "0xprivate-evm"},
			Observed:                      map[string]any{"state": observed},
			IndependentSubstrateFinalized: ChainHead{Number: 10, Hash: "0xpublic-substrate"},
			IndependentEVMFinalized:       ChainHead{Number: 10, Hash: "0xpublic-evm"},
			IndependentObserved:           map[string]any{"state": observed},
		}
	}
	ancestorPath, ancestorDigest, err := executor.persistActionPostcondition(buildRecord(ancestorHash, "ancestor"))
	if err != nil {
		t.Fatal(err)
	}
	ancestorBytes, err := os.ReadFile(filepath.Join(stateDir, filepath.FromSlash(ancestorPath)))
	if err != nil {
		t.Fatal(err)
	}
	executor.plan = &SetupPlan{PlanHash: activeHash, PriorPlanHashes: []string{ancestorHash}}
	activePath, activeDigest, err := executor.persistActionPostcondition(buildRecord(activeHash, "active"))
	if err != nil {
		t.Fatal(err)
	}
	if ancestorPath == activePath {
		t.Fatalf("ancestor and active receipts share %q", activePath)
	}
	unchanged, err := os.ReadFile(filepath.Join(stateDir, filepath.FromSlash(ancestorPath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != string(ancestorBytes) {
		t.Fatal("active receipt overwrote ancestor evidence")
	}
	for _, entry := range []JournalEntry{
		{PlanHash: ancestorHash, ActionID: "same.action", IntentHash: "same-intent", PostconditionPath: ancestorPath, PostconditionHash: ancestorDigest},
		{PlanHash: activeHash, ActionID: "same.action", IntentHash: "same-intent", PostconditionPath: activePath, PostconditionHash: activeDigest},
	} {
		if err := executor.verifyPersistedPostcondition(entry); err != nil {
			t.Errorf("plan-scoped receipt %s was rejected: %v", entry.PlanHash, err)
		}
	}
}

func TestDurablePostconditionHashSurvivesNestedObservationRoundTrip(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	planHash := "0x" + strings.Repeat("ac", 32)
	executor := &Executor{cfg: cfg, plan: &SetupPlan{PlanHash: planHash}, stateDir: t.TempDir()}
	observed := map[string]any{
		"kind": "evm-transaction",
		"registration_balances": []registrationBalanceObservation{{
			Address: common.HexToAddress("0x521").Hex(), EVMBeforeWei: "0", EVMAfterWei: "0", NativeBeforeRao: 0, NativeAfterRao: 500,
		}},
	}
	independentObserved, err := cloneObservedPostState(observed)
	if err != nil {
		t.Fatal(err)
	}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: planHash, ActionID: "evm.vault-register-escrow", IntentHash: "same-intent",
		OperationalRPCMode: rpcModePublicOverride, IndependentRPC: false,
		SubstrateFinalized:            ChainHead{Number: 9, Hash: "0xsubstrate"},
		EVMFinalized:                  ChainHead{Number: 9, Hash: "0xevm"},
		EVMHashDomain:                 "evm-rpc",
		Observed:                      observed,
		IndependentSubstrateFinalized: ChainHead{Number: 9, Hash: "0xsubstrate"},
		IndependentEVMFinalized:       ChainHead{Number: 9, Hash: "0xevm"},
		IndependentEVMHashDomain:      "evm-rpc",
		IndependentObserved:           independentObserved,
	}
	inMemoryHash, err := canonicalHashHex(record)
	if err != nil {
		t.Fatal(err)
	}
	path, durableHash, err := executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	if inMemoryHash == durableHash {
		t.Fatal("nested struct observation did not reproduce the pre-serialization hash discrepancy")
	}
	entry := JournalEntry{PlanHash: planHash, ActionID: record.ActionID, IntentHash: record.IntentHash, PostconditionPath: path, PostconditionHash: durableHash}
	if err := executor.verifyPersistedPostcondition(entry); err != nil {
		t.Fatalf("durable nested postcondition was rejected after round-trip: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(executor.stateDir, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	durable, err := decodeActionPostcondition(raw)
	if err != nil {
		t.Fatal(err)
	}
	readHash, err := canonicalHashHex(durable)
	if err != nil || readHash != durableHash {
		t.Fatalf("on-disk postcondition hash = %s, want %s: %v", readHash, durableHash, err)
	}
}

func TestLegacyRegistrationBalancePostconditionRecoversExactJournalHash(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	planHash := "0x" + strings.Repeat("ad", 32)
	executor := &Executor{cfg: cfg, plan: &SetupPlan{PlanHash: planHash}, stateDir: t.TempDir()}
	balances := []registrationBalanceObservation{{
		Address: common.HexToAddress("0x521").Hex(), EVMBeforeWei: "0", EVMAfterWei: "0", NativeBeforeRao: 0, NativeAfterRao: 500,
	}}
	observed := map[string]any{"kind": "evm-transaction", "registration_balances": balances}
	independentObserved, err := cloneObservedPostState(observed)
	if err != nil {
		t.Fatal(err)
	}
	record := &ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v3", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PlanHash: planHash, ActionID: "evm.vault-register-escrow", IntentHash: "legacy-intent",
		OperationalRPCMode: rpcModePublicOverride, IndependentRPC: false,
		SubstrateFinalized:            ChainHead{Number: 9, Hash: "0xsubstrate"},
		EVMFinalized:                  ChainHead{Number: 9, Hash: "0xevm"},
		EVMHashDomain:                 "evm-rpc",
		Observed:                      observed,
		IndependentSubstrateFinalized: ChainHead{Number: 9, Hash: "0xsubstrate"},
		IndependentEVMFinalized:       ChainHead{Number: 9, Hash: "0xevm"},
		IndependentEVMHashDomain:      "evm-rpc",
		IndependentObserved:           independentObserved,
	}
	legacyHash, err := canonicalHashHex(record)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path, err := postconditionRelativePath(planHash, record.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(executor.stateDir, filepath.FromSlash(path)), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	durable, err := durableActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	durableHash, err := canonicalHashHex(durable)
	if err != nil || durableHash == legacyHash {
		t.Fatalf("legacy incident was not reproduced: durable=%s legacy=%s error=%v", durableHash, legacyHash, err)
	}
	entry := JournalEntry{PlanHash: planHash, ActionID: record.ActionID, IntentHash: record.IntentHash, PostconditionPath: path, PostconditionHash: legacyHash}
	if err := executor.verifyPersistedPostcondition(entry); err != nil {
		t.Fatalf("exact legacy registration-balance evidence was rejected: %v", err)
	}
	record.Observed["registration_balances"] = []registrationBalanceObservation{{
		Address: common.HexToAddress("0x521").Hex(), EVMBeforeWei: "0", EVMAfterWei: "1", NativeBeforeRao: 0, NativeAfterRao: 500,
	}}
	tampered, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(executor.stateDir, filepath.FromSlash(path)), append(tampered, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executor.verifyPersistedPostcondition(entry); err == nil || !strings.Contains(err.Error(), "journal requires") {
		t.Fatalf("tampered legacy registration-balance evidence was accepted: %v", err)
	}
}

func TestVoluntaryConvictionPostconditionIdentityAndEventAreExact(t *testing.T) {
	cfg := testResolvedConfig(t)
	funder := common.HexToAddress("0x0000000000000000000000000000000000001234")
	plan := &SetupPlan{PolicyHash: cfg.PolicyHash, Roles: PublicRoles{OperatorDepositSigners: []string{funder.Hex()}}}
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
	historicalPolicyHash := "0x" + strings.Repeat("ab", 32)
	historicalPlan := *plan
	historicalPlan.PolicyHash = historicalPolicyHash
	historicalEvidence := evidence
	historicalEvidence.PolicyHash = historicalPolicyHash
	if err := voluntaryConvictionEvidenceMatches(cfg, &historicalPlan, historicalEvidence); err != nil {
		t.Fatalf("historical evidence was compared to the current config policy: %v", err)
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
	gate := &ReleaseCampaignGate{
		Schema: releaseCampaignGateSchema, RunID: "release-run", ResultHash: "0x" + strings.Repeat("11", 32), CompleteContentHash: "sha256:" + strings.Repeat("22", 32), StartEpoch: 26, EndEpoch: 46,
		LifecycleHandoff: ScenarioLifecycleHandoff{Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: "release-run", Stage: fleetLifecycleStageReleaseHandoff, File: scenarioLifecycleHandoffFilename, ContentHash: "sha256:" + strings.Repeat("33", 32), SizeBytes: 123},
	}
	evidence := ProductionPolicyEvidence{
		Schema: "urnetwork-production-policy-evidence-v2", DeploymentID: cfg.Config.Deployment.DeploymentID,
		PolicyHash: cfg.PolicyHash, ReleaseRunID: gate.RunID, ReleaseResultHash: gate.ResultHash, ReleaseCompleteHash: gate.CompleteContentHash,
		ReleaseHandoffHash: gate.LifecycleHandoff.ContentHash, ReleaseHandoffSize: gate.LifecycleHandoff.SizeBytes,
		CampaignStartEpoch: gate.StartEpoch, CampaignEndEpoch: gate.EndEpoch, ScheduledFromEpoch: 52, EffectiveEpoch: 53, EffectiveBlock: 100,
		PriorEpochBlocks: cfg.Policy.Settlement.EpochBlocks, EpochBlocks: p.EpochBlocks,
		RootCommitWindowBlocks: p.RootCommitWindowBlocks, FinalizeOffsetBlocks: p.FinalizeOffsetBlocks, CloseGraceBlocks: p.CloseGraceBlocks,
		TransactionHash: "0x" + strings.Repeat("44", 32), FinalizedBlock: 101, FinalizedBlockHash: "0x" + strings.Repeat("55", 32),
	}
	if !productionPolicyEvidenceMatches(cfg, evidence, gate) {
		t.Fatal("canonical production cadence evidence was rejected")
	}
	evidence.ScheduledFromEpoch++
	evidence.EffectiveEpoch++
	if !productionPolicyEvidenceMatches(cfg, evidence, gate) {
		t.Fatal("later campaign-relative production cadence was rejected")
	}
	evidence.ScheduledFromEpoch = gate.EndEpoch - 1
	evidence.EffectiveEpoch = gate.EndEpoch
	if productionPolicyEvidenceMatches(cfg, evidence, gate) {
		t.Fatal("production cadence before the release boundary was accepted")
	}
	evidence.ScheduledFromEpoch = 52
	evidence.EffectiveEpoch = 54
	if productionPolicyEvidenceMatches(cfg, evidence, gate) {
		t.Fatal("non-consecutive production effective epoch was accepted")
	}
	evidence.EffectiveEpoch = 53
	evidence.ReleaseResultHash = "0x" + strings.Repeat("33", 32)
	if productionPolicyEvidenceMatches(cfg, evidence, gate) {
		t.Fatal("wrong release result hash was accepted")
	}
	evidence.ReleaseResultHash = gate.ResultHash
	evidence.ReleaseHandoffHash = "sha256:" + strings.Repeat("66", 32)
	if productionPolicyEvidenceMatches(cfg, evidence, gate) {
		t.Fatal("wrong release lifecycle handoff was accepted")
	}
	evidence.ReleaseHandoffHash = gate.LifecycleHandoff.ContentHash
	evidence.ScheduledFromEpoch = ^uint64(0)
	evidence.EffectiveEpoch++
	if productionPolicyEvidenceMatches(cfg, evidence, gate) {
		t.Fatal("overflowing production schedule was accepted")
	}
	evidence.ScheduledFromEpoch = 52
	evidence.EffectiveEpoch = 53
	evidence.CloseGraceBlocks++
	if productionPolicyEvidenceMatches(cfg, evidence, gate) {
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

func TestFinalizedRegistrationActionBlockUsesExactVerifiedAncestor(t *testing.T) {
	action := Action{ID: "evm.vault-register-escrow", IntentHash: "intent-a"}
	entries := []JournalEntry{
		{PlanHash: "plan-a", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, BlockNumber: 12},
		{PlanHash: "plan-a", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/legacy.json"},
		{PlanHash: "foreign", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, BlockNumber: 13},
		{PlanHash: "plan-b", ActionID: action.ID, IntentHash: "adjacent-intent", Stage: StageFinalized, BlockNumber: 14},
	}
	executor := &Executor{plan: &SetupPlan{PlanHash: "plan-b", PriorPlanHashes: []string{"plan-a"}}, journal: &Journal{entries: entries}}
	block, err := executor.finalizedRegistrationActionBlock(action)
	if err != nil || block != 12 {
		t.Fatalf("carried registration block = %d, want exact ancestor block 12: %v", block, err)
	}
}

func TestFinalizedRegistrationActionBlockUsesAcceptedAncestorIntent(t *testing.T) {
	action := Action{ID: "evm.vault-register-escrow", IntentHash: "current-intent", AcceptedPriorIntentHashes: []string{"ancestor-intent"}}
	entries := []JournalEntry{
		{PlanHash: "ancestor", ActionID: action.ID, IntentHash: "ancestor-intent", Stage: StageFinalized, BlockNumber: 12},
		{PlanHash: "ancestor", ActionID: action.ID, IntentHash: "ancestor-intent", Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/ancestor.json"},
		{PlanHash: "ancestor", ActionID: action.ID, IntentHash: "unaccepted-intent", Stage: StageFinalized, BlockNumber: 13},
	}
	executor := &Executor{plan: &SetupPlan{PlanHash: "active", PriorPlanHashes: []string{"ancestor"}}, journal: &Journal{entries: entries}}
	block, releaseHistory, err := executor.finalizedRegistrationActionCheckpoint(action)
	if err != nil || block != 12 || !releaseHistory {
		t.Fatalf("accepted ancestor registration checkpoint = %d history=%t, want 12/true: %v", block, releaseHistory, err)
	}
}

func TestFinalizedRegistrationActionBlockUsesOnlyActiveUnverifiedTransaction(t *testing.T) {
	action := Action{ID: "operator.register.1", IntentHash: "intent-a"}
	entries := []JournalEntry{
		{PlanHash: "ancestor", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, BlockNumber: 11},
		{PlanHash: "foreign", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionHash: "0xpost", PostconditionPath: "receipts/postconditions/foreign.json"},
		{PlanHash: "active", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, BlockNumber: 15},
	}
	executor := &Executor{plan: &SetupPlan{PlanHash: "active", PriorPlanHashes: []string{"ancestor"}}, journal: &Journal{entries: entries}}
	block, err := executor.finalizedRegistrationActionBlock(action)
	if err != nil || block != 15 {
		t.Fatalf("unverified registration block = %d, want active block 15: %v", block, err)
	}
	executor.journal = &Journal{entries: entries[:2]}
	if _, err := executor.finalizedRegistrationActionBlock(action); err == nil {
		t.Fatal("an active unverified registration adopted an ancestor or foreign transaction")
	}
}

func TestActionPostStateEVMCheckpointClassification(t *testing.T) {
	native := []Action{
		{ID: "subnet.verify-owner"},
		{ID: "subnet.hyperparameter.tempo"},
		{ID: "production.hyperparameter.immunity_period"},
		{ID: "fleet.fund.1"},
		{ID: "fleet.fund-hotkey.1"},
		{ID: "churn.fund.1"},
		{ID: "validator.fund.1"},
		{ID: "fleet.register.1"},
		{ID: "churn.register.1"},
		{ID: "validator.register.1"},
		{ID: "operator.deposit.register.1"},
		{ID: "validator.take-zero.1"},
		{ID: "validator.reserve-majority"},
		{ID: "fleet.commitment.1"},
		{ID: "fleet.refresh.commitment.1"},
		{ID: "wallet.native-fee-reserve", Kind: "budget-reserve"},
		{ID: "config.render"},
		{ID: "accounts.provision"},
		{ID: "topology.launch"},
		{ID: "churn.tournament-complete"},
	}
	for _, action := range native {
		if actionPostStateRequiresEVMCheckpoint(action) {
			t.Errorf("native/local action %s unnecessarily requires an EVM checkpoint", action.ID)
		}
	}
	evm := []Action{
		{ID: "evm.fund-owner"},
		{ID: "evm.coordinator-proxy"},
		{ID: "alpha.transfer.validator.1"},
		{ID: "alpha.transfer.operator-deposit.1"},
		{ID: "operator.register.1"},
		{ID: "fleet.mirror.1"},
		{ID: "fleet.bind.1.1"},
		{ID: "campaign.voluntary-conviction.1"},
		{ID: "campaign.dishonest-deposit.2"},
		{ID: "precompile.read-battery"},
		{ID: "governance.guardian-pause"},
		{ID: "production.schedule-policy"},
		{ID: "operator.retire.1"},
		{ID: "future.unclassified-action"},
	}
	for _, action := range evm {
		if !actionPostStateRequiresEVMCheckpoint(action) {
			t.Errorf("EVM or unclassified action %s lacks a checkpoint", action.ID)
		}
	}
}

func TestCurrentNativePostStateDoesNotRequireEitherChainCheckpoint(t *testing.T) {
	action := Action{ID: "wallet.native-fee-reserve", Kind: "budget-reserve", Spend: Spend{TAORao: 123}}
	executor := &Executor{plan: &SetupPlan{Limits: Spend{TAORao: 456}}}
	if err := executor.verifyCurrentActionPostState(context.Background(), action, nil); err != nil {
		t.Fatalf("local carried postcondition reached an unrelated RPC: %v", err)
	}

	evmAction := Action{ID: "evm.fund-owner", Kind: "substrate-extrinsic"}
	if err := executor.verifyCurrentActionPostState(context.Background(), evmAction, nil); err == nil || !strings.Contains(err.Error(), "EVM postcondition client is unavailable") {
		t.Fatalf("EVM postcondition did not fail closed without a client: %v", err)
	}
}

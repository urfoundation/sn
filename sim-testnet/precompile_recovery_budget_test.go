// Synthetic caps exercise supplemental authorization without chain access or
// changing any retained setup action, custody identity or signed receipt.
package main

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Tiny wei amounts make native-rao rounding and each lifetime dimension visible.
func precompileRecoveryBudgetTestValues() (*SetupPlan, *PrecompileRecoveryBudget) {
	plan := &SetupPlan{
		PlanHash:        common.Hash{91}.Hex(),
		MaximumSpend:    Spend{TAORao: 11, EVMGasWei: "500"},
		SupersededSpend: Spend{TAORao: 7, EVMGasWei: "100"},
		Limits:          Spend{TAORao: 19, EVMGasWei: "610"},
	}
	budget := &PrecompileRecoveryBudget{
		Schema: "urnetwork-precompile-recovery-budget-v1", PlanHash: plan.PlanHash,
		JournalHash: common.Hash{92}.Hex(), CampaignReserveWei: "100",
		CommittedOrPendingMaxWei: "90", MaximumRecoveryWei: "20", MaximumReseedWei: "0", Verified: true,
	}
	return plan, budget
}

// An exact boundary remains usable while both retained and superseded spending
// are charged. The independent total-TAO dimension rounds a fractional rao up.
func TestPrecompileRecoverySupplementalUsesExactShortfallWithinLifetimeCaps(t *testing.T) {
	plan, budget := precompileRecoveryBudgetTestValues()
	original, err := canonicalHashHex(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := allocatePrecompileRecoveryBudget(plan, budget, big.NewInt(100), big.NewInt(90), big.NewInt(20)); err != nil {
		t.Fatal(err)
	}
	if budget.Schema != "urnetwork-precompile-recovery-budget-v2" || budget.Supplemental == nil || budget.Supplemental.Spend.EVMGasWei != "10" || budget.Supplemental.Spend.TAORao != 1 || budget.Supplemental.RetainedSpend.TAORao != 18 || budget.Supplemental.RetainedSpend.EVMGasWei != "600" || budget.AvailableWei != "20" {
		t.Fatalf("incorrect exact allocation: %+v %+v", budget, budget.Supplemental)
	}
	got, err := precompileRecoveryEffectiveReserve(*budget)
	if err != nil || got.Cmp(big.NewInt(110)) != 0 {
		t.Fatalf("effective reserve = %v, %v", got, err)
	}
	after, err := canonicalHashHex(plan)
	if err != nil || after != original {
		t.Fatal("supplemental approval changed retained plan", err)
	}
}

// Existing signed liabilities may already consume all local reserve. Their
// shortfall is visible in the proposal rather than omitted from the new budget.
func TestPrecompileRecoverySupplementalIncludesExistingReserveDeficit(t *testing.T) {
	plan, budget := precompileRecoveryBudgetTestValues()
	plan.Limits.EVMGasWei = "670"
	budget.CommittedOrPendingMaxWei = "150"
	if err := allocatePrecompileRecoveryBudget(plan, budget, big.NewInt(100), big.NewInt(150), big.NewInt(20)); err != nil {
		t.Fatal(err)
	}
	if budget.Supplemental.Spend.EVMGasWei != "70" || budget.AvailableWei != "20" {
		t.Fatalf("existing liability was lost: %+v", budget)
	}
}

// Legacy budgets preserve their signed JSON identity. An unused lifetime cap
// does not authorize an unnecessary supplement while local reserve suffices.
func TestPrecompileRecoverySupplementalPreservesExistingReserveAuthority(t *testing.T) {
	plan, budget := precompileRecoveryBudgetTestValues()
	budget.CommittedOrPendingMaxWei = "80"
	if err := allocatePrecompileRecoveryBudget(plan, budget, big.NewInt(100), big.NewInt(80), big.NewInt(20)); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(budget)
	if err != nil || strings.Contains(string(raw), "supplemental") || budget.Schema != "urnetwork-precompile-recovery-budget-v1" || budget.AvailableWei != "20" {
		t.Fatalf("legacy authority changed: %s %v", raw, err)
	}
}

// Cap exhaustion, integer overflow and independently altered dimensions must
// remain hard even when enough room exists in another dimension.
func TestPrecompileRecoverySupplementalRejectsEveryLifetimeOverflow(t *testing.T) {
	for _, fault := range []string{"evm", "total", "native overflow", "alpha", "registration", "subnet"} {
		plan, budget := precompileRecoveryBudgetTestValues()
		switch fault {
		case "evm":
			plan.Limits.EVMGasWei = "609"
		case "total":
			plan.Limits.TAORao = 18
		case "native overflow":
			plan.MaximumSpend.TAORao = ^uint64(0)
		case "alpha":
			plan.SupersededSpend.AlphaRao = 1
		case "registration":
			plan.SupersededSpend.Registrations = 1
		case "subnet":
			plan.SupersededSpend.SubnetCreations = 1
		}
		if err := allocatePrecompileRecoveryBudget(plan, budget, big.NewInt(100), big.NewInt(90), big.NewInt(20)); err == nil {
			t.Fatalf("accepted %s", fault)
		}
	}
	tooLarge := new(big.Int).Mul(new(big.Int).SetUint64(^uint64(0)), big.NewInt(1_000_000_000))
	tooLarge.Add(tooLarge, big.NewInt(1))
	if _, err := precompileRecoverySupplementalTao(tooLarge); err == nil {
		t.Fatal("supplemental native amount wrapped")
	}
}

// Re-signing a new artifact cannot reinterpret its old plan or use the repair
// permission to gain unrelated spend or an arbitrary reserve margin.
func TestPrecompileRecoverySupplementalRejectsSubstitutionAndExtraMargin(t *testing.T) {
	for _, fault := range []string{"extra wei", "missing wei", "extra native", "alpha", "registration", "subnet", "legacy schema", "missing record", "lower retained", "larger cap"} {
		plan, budget := precompileRecoveryBudgetTestValues()
		if err := allocatePrecompileRecoveryBudget(plan, budget, big.NewInt(100), big.NewInt(90), big.NewInt(20)); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "extra wei":
			budget.Supplemental.Spend.EVMGasWei = "11"
		case "missing wei":
			budget.Supplemental.Spend.EVMGasWei = "9"
		case "extra native":
			budget.Supplemental.Spend.TAORao++
		case "alpha":
			budget.Supplemental.Spend.AlphaRao = 1
		case "registration":
			budget.Supplemental.Spend.Registrations = 1
		case "subnet":
			budget.Supplemental.Spend.SubnetCreations = 1
		case "legacy schema":
			budget.Schema = "urnetwork-precompile-recovery-budget-v1"
		case "missing record":
			budget.Supplemental = nil
		case "lower retained":
			budget.Supplemental.RetainedSpend.EVMGasWei = "500"
		case "larger cap":
			budget.Supplemental.ApprovedLimits.EVMGasWei = "999"
		}
		_, envelopeErr := precompileRecoveryEffectiveReserve(*budget)
		planErr := validatePrecompileRecoverySupplementalPlan(plan, *budget)
		if envelopeErr == nil && planErr == nil {
			t.Fatalf("accepted %s", fault)
		}
	}
}

// The complete signed approval binds v2 to the same custody, plan and two owners.
// No executable action changes and no unsigned extra budget gains authority.
func TestPrecompileRecoverySupplementalProposalAndDualSignatures(t *testing.T) {
	f := newPrecompileRecoveryTestFixture(t)
	for index := range f.plan.Actions {
		if f.plan.Actions[index].ID == "campaign.evm-gas-reserve" {
			f.plan.Actions[index].Spend.EVMGasWei = "0"
			var err error
			f.plan.Actions[index].IntentHash, err = actionIntentHash(f.plan.Actions[index])
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	f.plan.Limits.TAORao = ^uint64(0)
	f.plan.Limits.EVMGasWei = "512000000000000000000"
	budget, err := newPrecompileRecoveryBudget(f.cfg, f.stateDir, f.plan, f.entries)
	if err != nil {
		t.Fatal(err)
	}
	if budget.Supplemental == nil || budget.AvailableWei != budget.MaximumRecoveryWei {
		t.Fatalf("exhausted reserve did not produce an exact signed proposal: %+v", budget)
	}
	request, err := newPrecompileRecoveryRequest(f.plan, f.original, *budget)
	if err != nil {
		t.Fatal(err)
	}
	authority := PrecompileRecoveryAuthorization{Request: request}
	f.sign(t, request, &authority.Hash, &authority.OwnerSignature, &authority.DeployerSignature)
	if err := validatePrecompileRecoveryPlan(f.plan, f.original, &authority); err != nil {
		t.Fatal(err)
	}
	authority.Request.Budget.Supplemental.Spend.EVMGasWei = "1"
	if err := validatePrecompileRecoveryPlan(f.plan, f.original, &authority); err == nil {
		t.Fatal("unsigned supplemental budget substitution was accepted")
	}
}

// The current exposure census consumes the exact signed extension. A saved
// repair shifts from unsigned reserve into signed liability exactly once.
func TestPrecompileRecoverySupplementalRechecksLiabilityWithoutDoubleCharging(t *testing.T) {
	plan, budget := precompileRecoveryBudgetTestValues()
	plan.ChainID = 945
	if err := allocatePrecompileRecoveryBudget(plan, budget, big.NewInt(100), big.NewInt(90), big.NewInt(20)); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "transactions"), 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	journal := &Journal{}
	appendSigned := func(actionId, intent string, nonce, gas uint64) {
		t.Helper()
		to := common.Address{23}
		transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: nonce, To: &to, Gas: gas, GasPrice: big.NewInt(1)}), types.LatestSignerForChainID(big.NewInt(945)), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := transaction.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "transactions", stringsTrim0x(transaction.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		journal.entries = append(journal.entries, JournalEntry{PlanHash: plan.PlanHash, ActionID: actionId, IntentHash: intent, Stage: StageBroadcast, Signer: crypto.PubkeyToAddress(key.PublicKey).Hex(), Nonce: strconv.FormatUint(nonce, 10), TransactionHash: transaction.Hash().Hex(), EntryHash: budget.JournalHash})
	}
	appendSigned("synthetic-existing", common.Hash{71}.Hex(), 1, 90)
	evidence := &PrecompileConformanceEvidence{Recovery: &PrecompileRecoveryEvidence{Authorization: PrecompileRecoveryAuthorization{Request: PrecompileRecoveryRequest{Budget: *budget}}}}
	cfg := testResolvedConfig(t)
	owner := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, journal: journal}
	if err := owner.validatePrecompileRecoveryBudget(evidence); err != nil {
		t.Fatal(err)
	}
	// Repeating a receipt row for the same bytes does not create another spend.
	journal.entries = append(journal.entries, journal.entries[0])
	if err := owner.validatePrecompileRecoveryBudget(evidence); err != nil {
		t.Fatal("duplicate observation was charged twice", err)
	}
	action := Action{ID: precompileRecoveryActionPrefix + "1", IntentHash: common.Hash{72}.Hex(), Spend: Spend{EVMGasWei: "10"}}
	appendSigned(action.ID, action.IntentHash, 2, 10)
	evidence.Recovery.Steps = []PrecompileRecoveryStep{{Action: action}}
	if err := owner.validatePrecompileRecoveryBudget(evidence); err != nil {
		t.Fatal("retained signed repair was charged against both reserve and liability", err)
	}
	appendSigned("synthetic-new-liability", common.Hash{73}.Hex(), 3, 1)
	if err := owner.validatePrecompileRecoveryBudget(evidence); err == nil {
		t.Fatal("stale supplemental headroom authorized an unreserved extra liability")
	}
}

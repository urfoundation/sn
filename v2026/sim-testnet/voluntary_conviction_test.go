// Voluntary-conviction tests lock one-time mutation and recovery semantics.
package main

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Reproduces the live incident: an epoch change must not make a prior
// cumulative conviction look eligible for a new plan intent.
func TestVoluntaryConvictionPrestateRejectsCumulativeMutationBeforeNewIntent(t *testing.T) {
	if err := validateVoluntaryConvictionPrestate(big.NewInt(1_000_000_000), false); err == nil || !strings.Contains(err.Error(), "want zero") {
		t.Fatalf("new intent accepted a prior cumulative conviction: %v", err)
	}
}

// An exact current-intent resume is safe because immutable transaction bytes
// are recovered rather than recreated with a fresh nonce.
func TestVoluntaryConvictionPrestateAllowsZeroOrExactIntentResume(t *testing.T) {
	for _, test := range []struct {
		cumulative *big.Int
		resumed    bool
	}{
		{cumulative: big.NewInt(0), resumed: false},
		{cumulative: big.NewInt(0), resumed: true},
		{cumulative: big.NewInt(1_000_000_000), resumed: true},
	} {
		if err := validateVoluntaryConvictionPrestate(test.cumulative, test.resumed); err != nil {
			t.Fatalf("valid cumulative/resume state %v/%t rejected: %v", test.cumulative, test.resumed, err)
		}
	}
}

// Invalid decoded values fail closed before transaction construction.
func TestVoluntaryConvictionPrestateRejectsInvalidValues(t *testing.T) {
	for _, cumulative := range []*big.Int{nil, big.NewInt(-1)} {
		if err := validateVoluntaryConvictionPrestate(cumulative, false); err == nil {
			t.Fatalf("invalid cumulative value %v was accepted", cumulative)
		}
	}
}

func TestVoluntaryConvictionRepairIDReservesEveryPriorSequence(t *testing.T) {
	revised := &SetupPlan{Actions: []Action{{ID: "alpha.repair.operator-deposit.1"}, {ID: "alpha.repair.operator-deposit.2.9"}}}
	prior := &SetupPlan{Actions: []Action{{ID: "alpha.repair.operator-deposit.1.2"}, {ID: "alpha.repair.operator-deposit.1.4"}}}
	got, err := nextVoluntaryConvictionRepairActionID(revised, prior)
	if err != nil || got != "alpha.repair.operator-deposit.1.5" {
		t.Fatalf("next repair id=%q want alpha.repair.operator-deposit.1.5: %v", got, err)
	}
	invalid := &SetupPlan{Actions: []Action{{ID: "alpha.repair.operator-deposit.1.1"}}}
	if _, err := nextVoluntaryConvictionRepairActionID(invalid); err == nil {
		t.Fatal("invalid ancestor repair sequence was ignored")
	}
}

// Build a gas-only ancestor/source pair matching the live incident without
// requiring a network endpoint.
func testVoluntaryConvictionDuplicateRecovery(t *testing.T) (*ResolvedConfig, string, *SetupPlan, *SetupFacts, []JournalEntry, voluntaryConvictionDuplicateRecovery) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ancestor, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	var ancestorVoluntary, ancestorReserve *Action
	for index := range ancestor.Actions {
		switch ancestor.Actions[index].ID {
		case voluntaryConvictionActionID:
			ancestorVoluntary = &ancestor.Actions[index]
		case "campaign.evm-gas-reserve":
			ancestorReserve = &ancestor.Actions[index]
		}
	}
	if ancestorVoluntary == nil || ancestorReserve == nil {
		t.Fatal("test plan lacks voluntary conviction or campaign reserve")
	}
	ancestorVoluntary.Parameters[evmMaximumGasUnitsParameter] = "30505000"
	ancestorVoluntary.Spend.EVMGasWei = multiplyUint64Decimal(30_505_000, cfg.Config.Budgets.MaximumEVMFeePerGasWei)
	ancestorVoluntary.IntentHash, err = actionIntentHash(*ancestorVoluntary)
	if err != nil {
		t.Fatal(err)
	}
	ancestorReserve.Spend.EVMGasWei, err = subtractDecimalUint(ancestorReserve.Spend.EVMGasWei, "50500000000000000")
	if err != nil {
		t.Fatal(err)
	}
	ancestorReserve.IntentHash, err = actionIntentHash(*ancestorReserve)
	if err != nil {
		t.Fatal(err)
	}
	ancestor.MaximumSpend, err = maximumActionSpend(ancestor.Actions)
	if err != nil {
		t.Fatal(err)
	}
	ancestor.PlanHash, err = ancestor.hash()
	if err != nil || validatePlanBudget(ancestor) != nil {
		t.Fatalf("construct original voluntary plan: hash=%v validate=%v", err, validatePlanBudget(ancestor))
	}
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "plans"), 0o700); err != nil {
		t.Fatal(err)
	}
	wire, err := json.MarshalIndent(ancestor, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "plans", stringsTrim0x(ancestor.PlanHash)+".json"), append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	prior.PriorPlanHashes = []string{ancestor.PlanHash}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	duplicate := actionByID(t, prior, voluntaryConvictionActionID)
	original := actionByID(t, ancestor, voluntaryConvictionActionID)
	originalTransaction := "0x" + strings.Repeat("ab", 32)
	originalBlockHash := "0x" + strings.Repeat("bc", 32)
	duplicateTransaction := "0x" + strings.Repeat("cd", 32)
	duplicateBlockHash := "0x" + strings.Repeat("de", 32)
	evidence := VoluntaryConvictionEvidence{
		Schema: "urnetwork-voluntary-conviction-evidence-v1", DeploymentID: cfg.Config.Deployment.DeploymentID,
		NoID: 1, Epoch: 2, AmountRao: strconv.FormatUint(cfg.Config.Scenarios.VoluntaryConvictionRao, 10),
		BeforeConvictionRao: "0", AfterConvictionRao: strconv.FormatUint(cfg.Config.Scenarios.VoluntaryConvictionRao, 10), Nonce: "1",
		Funder: prior.Roles.OperatorDepositSigners[0], PolicyHash: cfg.PolicyHash,
		TransactionHash: originalTransaction, FinalizedBlock: 100, FinalizedHash: originalBlockHash,
	}
	entries := []JournalEntry{
		{PlanHash: ancestor.PlanHash, ActionID: original.ID, IntentHash: original.IntentHash, Stage: StageFinalized, TransactionHash: originalTransaction, BlockNumber: 100, BlockHash: originalBlockHash},
		{PlanHash: ancestor.PlanHash, ActionID: original.ID, IntentHash: original.IntentHash, Stage: StageVerified},
		{PlanHash: prior.PlanHash, ActionID: duplicate.ID, IntentHash: duplicate.IntentHash, Stage: StageFinalized, TransactionHash: duplicateTransaction, BlockNumber: 110, BlockHash: duplicateBlockHash},
	}
	amount := cfg.Config.Scenarios.VoluntaryConvictionRao
	after, _ := checkedMul(amount, 2)
	recovery := voluntaryConvictionDuplicateRecovery{
		DuplicateTransaction: planRevisionTransaction{PlanHash: prior.PlanHash, ActionID: duplicate.ID, IntentHash: duplicate.IntentHash, TransactionHash: duplicateTransaction, BlockNumber: 110, BlockHash: duplicateBlockHash},
		DuplicateAction:      duplicate, OriginalAction: original, OriginalPlanHash: ancestor.PlanHash, OriginalIntentHash: original.IntentHash, OriginalEvidence: evidence,
		DuplicateEpoch: 3, DuplicateNonce: "2", Funder: evidence.Funder, PolicyHash: evidence.PolicyHash,
		AmountRao: amount, CumulativeBeforeRao: amount, CumulativeAfterRao: after, OperatorPrincipalAfterRao: after,
		SupersededGasBefore: prior.SupersededSpend.EVMGasWei,
	}
	current := *testSetupFacts()
	return cfg, stateDir, prior, &current, entries, recovery
}

// The live successful duplicate is adopted once, charged at its approved gas
// ceiling, repaired in alpha, and placed ahead of the first unverified mirror.
func TestPlanRevisionReconcilesExactDuplicateVoluntaryConvictionOnce(t *testing.T) {
	cfg, stateDir, prior, current, entries, recovery := testVoluntaryConvictionDuplicateRecovery(t)
	persistPlan := func(plan *SetupPlan) {
		wire, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "plans", stringsTrim0x(plan.PlanHash)+".json"), append(wire, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	persistPlan(prior)
	operatorTransfer := actionByID(t, prior, "alpha.transfer.operator-deposit.1")
	entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: operatorTransfer.ID, IntentHash: operatorTransfer.IntentHash, Stage: StageVerified})
	revised, err := buildPlanRevisionFromFactsWithMigrationAndRecoveries(cfg, stateDir, prior, current, entries, time.Unix(3, 0), nil, []voluntaryConvictionDuplicateRecovery{recovery})
	if err != nil {
		t.Fatal(err)
	}
	reconciliation := actionByID(t, revised, voluntaryConvictionReconciliationActionID)
	repair := actionByID(t, revised, reconciliation.Parameters[voluntaryRecoveryRepairActionParameter])
	if actionByID(t, revised, voluntaryConvictionActionID).IntentHash != recovery.OriginalAction.IntentHash {
		t.Fatal("the verified original voluntary-conviction intent was not carried")
	}
	if repair.Spend.AlphaRao != recovery.AmountRao+reserveRoundingAllowancePerCallRao+alphaTransferDestinationRoundingAllowance ||
		repair.Parameters[alphaRepairMinimumDestinationParameter] != "250000000050" || !strings.Contains(strings.Join(actionByID(t, revised, "fleet.mirror.1").DependsOn, ","), repair.ID) {
		t.Fatalf("duplicate recovery repair/barrier is invalid: repair=%+v mirror=%+v", repair, actionByID(t, revised, "fleet.mirror.1"))
	}
	wantSuperseded, err := addDecimalUint(recovery.SupersededGasBefore, recovery.DuplicateAction.Spend.EVMGasWei)
	if err != nil || revised.SupersededSpend.EVMGasWei != wantSuperseded {
		t.Fatalf("duplicate gas accounting=%s want=%s error=%v", revised.SupersededSpend.EVMGasWei, wantSuperseded, err)
	}
	total, err := addSpends(revised.MaximumSpend, revised.SupersededSpend)
	if err != nil || total.EVMGasWei != revised.Limits.EVMGasWei || validatePlanBudget(revised) != nil {
		t.Fatalf("recovered cumulative budget=%+v limits=%+v add_error=%v validate=%v", total, revised.Limits, err, validatePlanBudget(revised))
	}
	wire, err := json.Marshal(revised)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := decodePersistedPlanBytes(wire)
	if err != nil {
		t.Fatalf("recovery plan failed its persisted round trip: %v", err)
	}
	if actionByID(t, persisted, reconciliation.ID).IntentHash != reconciliation.IntentHash {
		t.Fatal("persisted recovery action changed identity")
	}

	entries = append(entries,
		JournalEntry{PlanHash: revised.PlanHash, ActionID: reconciliation.ID, IntentHash: reconciliation.IntentHash, Stage: StageVerified},
		JournalEntry{PlanHash: revised.PlanHash, ActionID: repair.ID, IntentHash: repair.IntentHash, Stage: StageVerified},
	)
	persistPlan(revised)
	recovery.AlreadyPlanned = true
	continued, err := buildPlanRevisionFromFactsWithMigrationAndRecoveries(cfg, stateDir, revised, current, entries, time.Unix(4, 0), nil, []voluntaryConvictionDuplicateRecovery{recovery})
	if err != nil {
		t.Fatal(err)
	}
	reconciliationCount, repairCount := 0, 0
	for _, action := range continued.Actions {
		if action.ID == reconciliation.ID {
			reconciliationCount++
		}
		if action.ID == repair.ID {
			repairCount++
		}
	}
	if reconciliationCount != 1 || repairCount != 1 || continued.SupersededSpend.EVMGasWei != revised.SupersededSpend.EVMGasWei {
		t.Fatalf("continued recovery duplicated accounting/actions: reconciliation=%d repair=%d spend=%s/%s", reconciliationCount, repairCount, continued.SupersededSpend.EVMGasWei, revised.SupersededSpend.EVMGasWei)
	}
	reconciliationIndex, repairIndex, mirrorIndex := -1, -1, -1
	for index, action := range continued.Actions {
		switch action.ID {
		case reconciliation.ID:
			reconciliationIndex = index
		case repair.ID:
			repairIndex = index
		case "fleet.mirror.1":
			mirrorIndex = index
		}
	}
	if reconciliationIndex < 0 || repairIndex <= reconciliationIndex || mirrorIndex <= repairIndex {
		t.Fatalf("continued recovery is not topologically ordered: reconciliation=%d repair=%d mirror=%d", reconciliationIndex, repairIndex, mirrorIndex)
	}
	if err := validatePlanBudget(continued); err != nil {
		t.Fatalf("continued recovery plan is invalid: %v", err)
	}
}

// A same-id action placed by another recovery cannot borrow verified custody
// credit unless its complete executable intent is the authenticated ancestor.
func TestOperatorRepairPreservationRejectsConflictingSpecializedPlacement(t *testing.T) {
	cfg, stateDir, prior, current, entries, recovery := testVoluntaryConvictionDuplicateRecovery(t)
	wire, err := json.MarshalIndent(prior, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "plans", stringsTrim0x(prior.PlanHash)+".json"), append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	operatorTransfer := actionByID(t, prior, "alpha.transfer.operator-deposit.1")
	entries = append(entries, JournalEntry{PlanHash: prior.PlanHash, ActionID: operatorTransfer.ID, IntentHash: operatorTransfer.IntentHash, Stage: StageVerified})
	revised, err := buildPlanRevisionFromFactsWithMigrationAndRecoveries(cfg, stateDir, prior, current, entries, time.Unix(3, 0), nil, []voluntaryConvictionDuplicateRecovery{recovery})
	if err != nil {
		t.Fatal(err)
	}
	reconciliation := actionByID(t, revised, voluntaryConvictionReconciliationActionID)
	repair := actionByID(t, revised, reconciliation.Parameters[voluntaryRecoveryRepairActionParameter])
	entries = append(entries, JournalEntry{PlanHash: revised.PlanHash, ActionID: repair.ID, IntentHash: repair.IntentHash, Stage: StageVerified})
	cloneWire, err := json.Marshal(revised)
	if err != nil {
		t.Fatal(err)
	}
	conflicting, err := decodePersistedPlanBytes(cloneWire)
	if err != nil {
		t.Fatal(err)
	}
	for index := range conflicting.Actions {
		if conflicting.Actions[index].ID != repair.ID {
			continue
		}
		conflicting.Actions[index].Description += " with conflicting semantics"
		conflicting.Actions[index].IntentHash, err = actionIntentHash(conflicting.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := preserveVerifiedOperatorAlphaTransfers(conflicting, revised, entries); err == nil || !strings.Contains(err.Error(), "differs from its verified ancestor") {
		t.Fatalf("conflicting placed repair was accepted: %v", err)
	}
}

func TestPlanRevisionDuplicateRecoveryDoesNotCollideWithVerifiedAlphaRepair(t *testing.T) {
	cfg, stateDir, prior, current, entries, recovery := testVoluntaryConvictionDuplicateRecovery(t)
	transfer := actionByID(t, prior, "alpha.transfer.operator-deposit.1")
	minimum, err := minimumAlphaTransferRao(prior.LiveFacts.DefaultMinTransferRao, prior.LiveFacts.AlphaPriceQ9, prior.AlphaTransferMarginBPS)
	if err != nil {
		t.Fatal(err)
	}
	parameters := alphaTransferActionParameters(minimum, 0, minimum, &prior.LiveFacts, prior.AlphaTransferMarginBPS)
	parameters[alphaRepairForActionParameter] = transfer.ID
	parameters[alphaRepairMinimumIncrementParameter] = strconv.FormatUint(minimum-1, 10)
	parameters["campaign_policy_hash"] = prior.PolicyHash
	parameters[deploymentManifestHashParameter] = transfer.Parameters[deploymentManifestHashParameter]
	existingRepair := Action{
		ID: "alpha.repair.operator-deposit.1.2", Kind: "substrate-extrinsic", Target: transfer.Target,
		Description: "verified ancestor repair", Parameters: parameters, Spend: Spend{AlphaRao: minimum}, DependsOn: []string{transfer.ID},
	}
	existingRepair.IntentHash, err = actionIntentHash(existingRepair)
	if err != nil {
		t.Fatal(err)
	}
	for index, action := range prior.Actions {
		if action.ID == transfer.ID {
			prior.Actions = append(prior.Actions[:index+1], append([]Action{existingRepair}, prior.Actions[index+1:]...)...)
			break
		}
	}
	prior.MaximumSpend, err = maximumActionSpend(prior.Actions)
	if err != nil {
		t.Fatal(err)
	}
	oldPriorHash := prior.PlanHash
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	for index := range entries {
		if entries[index].PlanHash == oldPriorHash {
			entries[index].PlanHash = prior.PlanHash
		}
	}
	recovery.DuplicateTransaction.PlanHash = prior.PlanHash
	entries = append(entries,
		JournalEntry{PlanHash: prior.PlanHash, ActionID: transfer.ID, IntentHash: transfer.IntentHash, Stage: StageVerified},
		JournalEntry{PlanHash: prior.PlanHash, ActionID: existingRepair.ID, IntentHash: existingRepair.IntentHash, Stage: StageVerified},
	)

	revised, err := buildPlanRevisionFromFactsWithMigrationAndRecoveries(cfg, stateDir, prior, current, entries, time.Unix(3, 0), nil, []voluntaryConvictionDuplicateRecovery{recovery})
	if err != nil {
		t.Fatal(err)
	}
	reconciliation := actionByID(t, revised, voluntaryConvictionReconciliationActionID)
	if reconciliation.Parameters[voluntaryRecoveryRepairActionParameter] != "alpha.repair.operator-deposit.1.3" {
		t.Fatalf("duplicate recovery reused an ancestor repair id: %+v", reconciliation.Parameters)
	}
	counts := map[string]int{}
	for _, action := range revised.Actions {
		counts[action.ID]++
	}
	if counts[existingRepair.ID] != 1 || counts[reconciliation.Parameters[voluntaryRecoveryRepairActionParameter]] != 1 {
		t.Fatalf("verified and recovery repairs were not unique: %v", counts)
	}
	if err := validatePlanBudget(revised); err != nil {
		t.Fatalf("collision-free recovery plan is invalid: %v", err)
	}
}

// Every semantic field around the special recovery remains fail-closed; this
// exception cannot admit an unrelated successful EVM transaction.
func TestDuplicateVoluntaryConvictionRecoveryRejectsSemanticTampering(t *testing.T) {
	cfg, _, _, _, _, baseline := testVoluntaryConvictionDuplicateRecovery(t)
	mutations := []func(*voluntaryConvictionDuplicateRecovery){
		func(value *voluntaryConvictionDuplicateRecovery) {
			value.DuplicateTransaction.ActionID = "fleet.mirror.1"
		},
		func(value *voluntaryConvictionDuplicateRecovery) {
			value.DuplicateTransaction.IntentHash = "0x" + strings.Repeat("01", 32)
		},
		func(value *voluntaryConvictionDuplicateRecovery) { value.DuplicateAction.Target = "no:2" },
		func(value *voluntaryConvictionDuplicateRecovery) {
			value.DuplicateAction.Parameters["amount_rao"] = "2"
		},
		func(value *voluntaryConvictionDuplicateRecovery) {
			value.OriginalAction.Parameters[evmMaximumFeePerGasParameter] = "1"
		},
		func(value *voluntaryConvictionDuplicateRecovery) { value.OriginalPlanHash = "0x01" },
		func(value *voluntaryConvictionDuplicateRecovery) { value.OriginalEvidence.FinalizedBlock = 111 },
		func(value *voluntaryConvictionDuplicateRecovery) { value.DuplicateNonce = "3" },
		func(value *voluntaryConvictionDuplicateRecovery) {
			value.Funder = "0x0000000000000000000000000000000000000001"
		},
		func(value *voluntaryConvictionDuplicateRecovery) { value.PolicyHash = "0x" + strings.Repeat("02", 32) },
		func(value *voluntaryConvictionDuplicateRecovery) { value.CumulativeBeforeRao-- },
		func(value *voluntaryConvictionDuplicateRecovery) { value.CumulativeAfterRao++ },
		func(value *voluntaryConvictionDuplicateRecovery) { value.OperatorPrincipalAfterRao++ },
		func(value *voluntaryConvictionDuplicateRecovery) { value.DuplicateAction.Spend.EVMGasWei = "0" },
	}
	for index, mutate := range mutations {
		candidate := baseline
		candidate.DuplicateAction.Parameters = cloneStrings(baseline.DuplicateAction.Parameters)
		candidate.OriginalAction.Parameters = cloneStrings(baseline.OriginalAction.Parameters)
		mutate(&candidate)
		if err := validateVoluntaryConvictionDuplicateRecovery(cfg, candidate); err == nil {
			t.Errorf("semantic duplicate-recovery mutation %d was accepted", index)
		}
	}
}

// The recovery authenticates the signed call envelope as well as its event;
// each adjacent signer/calldata/value/gas mutation is rejected.
func TestDuplicateVoluntaryConvictionSignedTransactionBindsEveryExecutableField(t *testing.T) {
	cfg := testResolvedConfig(t)
	key, err := crypto.HexToECDSA(strings.Repeat("11", 32))
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := crypto.HexToECDSA(strings.Repeat("22", 32))
	if err != nil {
		t.Fatal(err)
	}
	funder := crypto.PubkeyToAddress(key.PublicKey)
	coordinator := common.HexToAddress("0x0000000000000000000000000000000000000521")
	plan := &SetupPlan{ChainID: cfg.ChainID, Deployment: ContractDeployment{CoordinatorProxy: coordinator}, Roles: PublicRoles{OperatorDepositSigners: []string{funder.Hex()}}}
	action := Action{Parameters: map[string]string{evmMaximumGasUnitsParameter: "200000", evmMaximumFeePerGasParameter: "100000000000"}}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	event := voluntaryConvictionEvent{NoID: big.NewInt(1), Epoch: big.NewInt(3), Funder: funder, Amount: big.NewInt(1_000_000_000), Nonce: big.NewInt(2)}
	receipt := &ethTypes.Receipt{BlockNumber: big.NewInt(100)}
	type transactionFields struct {
		key                     string
		chainID, gas, fee, noID uint64
		amount, nonce, deadline uint64
		value                   uint64
		to                      common.Address
		wrongMethod             bool
	}
	valid := transactionFields{key: strings.Repeat("11", 32), chainID: cfg.ChainID, gas: 200_000, fee: 100_000_000_000, noID: 1, amount: 1_000_000_000, nonce: 2, deadline: 110, to: coordinator}
	sign := func(fields transactionFields) *ethTypes.Transaction {
		var data []byte
		if fields.wrongMethod {
			data, err = parsed.Pack("currentEpoch")
		} else {
			data, err = parsed.Pack("addConviction", new(big.Int).SetUint64(fields.noID), new(big.Int).SetUint64(fields.amount), new(big.Int).SetUint64(fields.nonce), fields.deadline)
		}
		if err != nil {
			t.Fatal(err)
		}
		privateKey := key
		if fields.key == strings.Repeat("22", 32) {
			privateKey = otherKey
		}
		transaction := ethTypes.NewTx(&ethTypes.DynamicFeeTx{
			ChainID: new(big.Int).SetUint64(fields.chainID), Nonce: 7,
			GasTipCap: big.NewInt(1), GasFeeCap: new(big.Int).SetUint64(fields.fee), Gas: fields.gas,
			To: &fields.to, Value: new(big.Int).SetUint64(fields.value), Data: data,
		})
		signed, signErr := ethTypes.SignTx(transaction, ethTypes.LatestSignerForChainID(transaction.ChainId()), privateKey)
		if signErr != nil {
			t.Fatal(signErr)
		}
		return signed
	}
	if err := validateDuplicateVoluntaryConvictionTransaction(cfg, plan, action, sign(valid), receipt, event); err != nil {
		t.Fatalf("valid signed duplicate transaction was rejected: %v", err)
	}
	mutations := []func(*transactionFields){
		func(value *transactionFields) { value.key = strings.Repeat("22", 32) },
		func(value *transactionFields) { value.chainID-- },
		func(value *transactionFields) { value.to = common.HexToAddress("0x1") },
		func(value *transactionFields) { value.value = 1 },
		func(value *transactionFields) { value.gas++ },
		func(value *transactionFields) { value.fee++ },
		func(value *transactionFields) { value.noID = 2 },
		func(value *transactionFields) { value.amount++ },
		func(value *transactionFields) { value.nonce++ },
		func(value *transactionFields) { value.deadline = 99 },
		func(value *transactionFields) { value.wrongMethod = true },
	}
	for index, mutate := range mutations {
		fields := valid
		mutate(&fields)
		if err := validateDuplicateVoluntaryConvictionTransaction(cfg, plan, action, sign(fields), receipt, event); err == nil {
			t.Errorf("signed duplicate transaction mutation %d was accepted", index)
		}
	}
}

// Event recovery accepts one exact no-id log and rejects ambiguity or a
// sibling operator event in the same receipt.
func TestVoluntaryConvictionEventDecoderRequiresExactlyOneMatchingLog(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	event := parsed.Events["ConvictionAdded"]
	policy := [32]byte{0x52, 0x1}
	data, err := event.Inputs.NonIndexed().Pack(big.NewInt(1_000_000_000), policy, big.NewInt(2))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := common.HexToAddress("0x521")
	funder := common.HexToAddress("0x1234")
	log := &ethTypes.Log{Address: coordinator, Topics: []common.Hash{event.ID, common.BigToHash(big.NewInt(1)), common.BigToHash(big.NewInt(3)), common.BytesToHash(common.LeftPadBytes(funder.Bytes(), 32))}, Data: data}
	decoded, err := decodeVoluntaryConvictionEvent(parsed, &ethTypes.Receipt{Logs: []*ethTypes.Log{log}}, coordinator, 1)
	if err != nil || decoded.Amount.Uint64() != 1_000_000_000 || decoded.Nonce.Uint64() != 2 || decoded.Funder != funder || decoded.PolicyHash != policy {
		t.Fatalf("exact event decode=%+v error=%v", decoded, err)
	}
	duplicate := *log
	if _, err := decodeVoluntaryConvictionEvent(parsed, &ethTypes.Receipt{Logs: []*ethTypes.Log{log, &duplicate}}, coordinator, 1); err == nil {
		t.Fatal("ambiguous duplicate ConvictionAdded logs were accepted")
	}
	sibling := *log
	sibling.Topics = append([]common.Hash(nil), log.Topics...)
	sibling.Topics[1] = common.BigToHash(big.NewInt(2))
	if _, err := decodeVoluntaryConvictionEvent(parsed, &ethTypes.Receipt{Logs: []*ethTypes.Log{&sibling}}, coordinator, 1); err == nil {
		t.Fatal("sibling operator ConvictionAdded log was accepted")
	}
}

// Reconciliation cannot be verified from a receipt alone; it requires the
// exact original finalized+verified pair and duplicate finalized checkpoint.
func TestVoluntaryConvictionReconciliationRequiresEveryJournalProof(t *testing.T) {
	cfg, stateDir, prior, current, entries, recovery := testVoluntaryConvictionDuplicateRecovery(t)
	revised, err := buildPlanRevisionFromFactsWithMigrationAndRecoveries(cfg, stateDir, prior, current, entries, time.Unix(3, 0), nil, []voluntaryConvictionDuplicateRecovery{recovery})
	if err != nil {
		t.Fatal(err)
	}
	action := actionByID(t, revised, voluntaryConvictionReconciliationActionID)
	if !hasVoluntaryConvictionReconciliationJournalEvidence(revised, action, entries) {
		t.Fatal("complete reconciliation journal evidence was not recognized")
	}
	for removed := range entries {
		candidate := append([]JournalEntry(nil), entries[:removed]...)
		candidate = append(candidate, entries[removed+1:]...)
		if hasVoluntaryConvictionReconciliationJournalEvidence(revised, action, candidate) {
			t.Errorf("reconciliation remained valid after removing journal entry %d", removed)
		}
	}
}

// A successful transaction outside the exact voluntary action never reaches
// the special recovery path.
func TestSuccessfulUnrelatedEVMTransactionHasNoDuplicateRecovery(t *testing.T) {
	transaction := planRevisionTransaction{ActionID: "fleet.mirror.1", BlockNumber: 10, BlockHash: "0x" + strings.Repeat("11", 32)}
	if _, err := detectVoluntaryConvictionDuplicateRecovery(t.Context(), nil, "", nil, nil, nil, nil, transaction); err == nil || !strings.Contains(err.Error(), "not a recoverable voluntary conviction") {
		t.Fatalf("unrelated successful EVM transaction entered duplicate recovery: %v", err)
	}
}

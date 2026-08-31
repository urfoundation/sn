// Fleet commitment recovery tests reproduce the live batch-4 expiry and lock
// the adjacent install, refresh, challenger, lineage, and budget boundaries.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fleetCommitmentRecoveryFixture struct {
	cfg        *ResolvedConfig
	stateDir   string
	plan       *SetupPlan
	action     Action
	evidence   FleetCommitmentEvidence
	entries    []JournalEntry
	maximumAge uint64
}

func newFleetCommitmentRecoveryFixture(t *testing.T, fleet int, generation uint64) fleetCommitmentRecoveryFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	actionID := fmt.Sprintf("fleet.commitment.%d", fleet)
	manifestURI := fmt.Sprintf("fleet-%d.json", fleet)
	evidenceURI := fmt.Sprintf("fleet-%d.commitment.json", fleet)
	if generation == 2 {
		actionID = fmt.Sprintf("fleet.refresh.commitment.%d", fleet)
		manifestURI = fmt.Sprintf("fleet-%d.refresh.json", fleet)
		evidenceURI = fmt.Sprintf("fleet-%d.refresh.commitment.json", fleet)
	}
	action := actionByID(t, plan, actionID)
	evidence := FleetCommitmentEvidence{
		Schema: fleetCommitmentEvidenceSchemaV2, ManifestURI: manifestURI,
		CommitmentHash: "0x" + strings.Repeat("11", 32), Hotkey: "0x" + strings.Repeat("22", 32),
		ExtrinsicHash: "0x" + strings.Repeat("33", 32), CommitmentBlock: 100, FinalizedBlock: 100,
		FinalizedBlockHash: "0x" + strings.Repeat("44", 32),
	}
	stateDir := t.TempDir()
	if err := writePublicJSON(filepath.Join(stateDir, "public", evidenceURI), evidence); err != nil {
		t.Fatal(err)
	}
	persistFleetCommitmentRecoveryTestPlan(t, stateDir, plan)
	entries := []JournalEntry{
		{Sequence: 1, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: evidence.ExtrinsicHash, BlockNumber: evidence.FinalizedBlock, BlockHash: evidence.FinalizedBlockHash},
		{Sequence: 2, PlanHash: plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified},
	}
	maximumAge, err := fleetCommitmentMaximumAgeBlocks(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return fleetCommitmentRecoveryFixture{cfg: cfg, stateDir: stateDir, plan: plan, action: action, evidence: evidence, entries: entries, maximumAge: maximumAge}
}

func persistFleetCommitmentRecoveryTestPlan(t *testing.T, stateDir string, plan *SetupPlan) {
	t.Helper()
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plans", stringsTrim0x(plan.PlanHash)+".json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (self fleetCommitmentRecoveryFixture) reviseAt(t *testing.T, block uint64) *SetupPlan {
	t.Helper()
	current := *testSetupFacts()
	current.FinalizedBlock = block
	current.EVMFinalizedBlock = block
	revised, err := buildPlanRevisionFromFacts(self.cfg, self.stateDir, self.plan, &current, self.entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	return revised
}

func TestFleetCommitmentRecoveryStartsOneBlockInsideUnsafeWindowAndBudgetsRetry(t *testing.T) {
	fixture := newFleetCommitmentRecoveryFixture(t, 31, 1)
	safeHead := fixture.evidence.FinalizedBlock + fixture.maximumAge - fleetCommitmentInclusionSafetyBlocks
	safe := fixture.reviseAt(t, safeHead)
	if action := actionByID(t, safe, fixture.action.ID); action.IntentHash != fixture.action.IntentHash || action.Parameters[fleetCommitmentRecoveryBlockParameter] != "" {
		t.Fatalf("exact inclusion-margin boundary was revised: %+v", action)
	}

	unsafe := fixture.reviseAt(t, safeHead+1)
	recovery := actionByID(t, unsafe, fixture.action.ID)
	if recovery.IntentHash == fixture.action.IntentHash || recovery.Parameters[fleetCommitmentRecoveryBlockParameter] != "100" ||
		recovery.Parameters[fleetCommitmentRecoveryCountParameter] != "1" || recovery.Parameters[fleetCommitmentRecoveryFeeParameter] != "3000000" ||
		len(recovery.AcceptedPriorIntentHashes) != 0 {
		t.Fatalf("unsafe commitment did not become one exact recovery: %+v", recovery)
	}
	priorFunding := actionByID(t, fixture.plan, "fleet.fund-hotkey.31")
	recoveryFunding := actionByID(t, unsafe, "fleet.fund-hotkey.31")
	if recoveryFunding.Spend.TAORao-priorFunding.Spend.TAORao != fixture.plan.NativeTransactionFeeLimitRao ||
		recoveryFunding.Parameters[fleetCommitmentFundingCountParameter] != "1" {
		t.Fatalf("fleet recovery funding=%+v prior=%+v", recoveryFunding, priorFunding)
	}
	priorReserve := actionByID(t, fixture.plan, "wallet.native-fee-reserve")
	recoveryReserve := actionByID(t, unsafe, "wallet.native-fee-reserve")
	priorWrites, _ := strconv.ParseUint(priorReserve.Parameters["native_writes"], 10, 64)
	recoveryWrites, _ := strconv.ParseUint(recoveryReserve.Parameters["native_writes"], 10, 64)
	if recoveryWrites != priorWrites+1 || recoveryReserve.Spend.TAORao-priorReserve.Spend.TAORao != fixture.plan.NativeTransactionFeeLimitRao ||
		recoveryReserve.Parameters[fleetCommitmentReserveCountParameter] != "1" {
		t.Fatalf("native recovery reserve=%+v prior=%+v", recoveryReserve, priorReserve)
	}
	if unsafe.MaximumSpend.TAORao-fixture.plan.MaximumSpend.TAORao != 2*fixture.plan.NativeTransactionFeeLimitRao {
		t.Fatalf("recovery TAO ceiling increased by %d, want funding plus fee reserve %d", unsafe.MaximumSpend.TAORao-fixture.plan.MaximumSpend.TAORao, 2*fixture.plan.NativeTransactionFeeLimitRao)
	}
}

func TestFleetCommitmentInclusionLifetimeUsesInclusiveBoundaryAndRecoveryFloor(t *testing.T) {
	cfg := testResolvedConfig(t)
	maximumAge, err := fleetCommitmentMaximumAgeBlocks(cfg)
	if err != nil {
		t.Fatal(err)
	}
	action := Action{ID: "fleet.commitment.31", Parameters: map[string]string{}}
	evidence := &FleetCommitmentEvidence{CommitmentBlock: 100, FinalizedBlock: 100}
	safeHead := evidence.FinalizedBlock + maximumAge - fleetCommitmentInclusionSafetyBlocks
	if err := validateFleetCommitmentInclusionLifetime(cfg, action, evidence, safeHead); err != nil {
		t.Fatalf("inclusive lifetime boundary rejected: %v", err)
	}
	if err := validateFleetCommitmentInclusionLifetime(cfg, action, evidence, safeHead+1); err == nil || !strings.Contains(err.Error(), "inclusion margin") {
		t.Fatalf("first unsafe lifetime block accepted: %v", err)
	}
	if fleetCommitmentHasInclusionLifetime(^uint64(0)-10, safeHead, maximumAge, fleetCommitmentInclusionSafetyBlocks) {
		t.Fatal("overflowing commitment expiry was accepted")
	}
	action.Parameters = map[string]string{
		fleetCommitmentRecoveryBlockParameter: "100", fleetCommitmentRecoveryCountParameter: "1", fleetCommitmentRecoveryFeeParameter: "3000000",
	}
	if err := validateFleetCommitmentInclusionLifetime(cfg, action, evidence, 101); err == nil || !strings.Contains(err.Error(), "want greater than 100") {
		t.Fatalf("recovery reused its superseded evidence: %v", err)
	}
	evidence.CommitmentBlock = 101
	evidence.FinalizedBlock = 101
	if err := validateFleetCommitmentInclusionLifetime(cfg, action, evidence, 101); err != nil {
		t.Fatalf("strictly later recovery evidence rejected: %v", err)
	}
}

func TestFleetCommitmentSameReleaseRevisionTriggersOnlyForVerifiedUnsafeEvidence(t *testing.T) {
	fixture := newFleetCommitmentRecoveryFixture(t, 31, 1)
	safeHead := fixture.evidence.FinalizedBlock + fixture.maximumAge - fleetCommitmentInclusionSafetyBlocks
	required, err := fleetCommitmentRecoveryRequiredAt(fixture.cfg, fixture.stateDir, fixture.plan, fixture.entries, safeHead)
	if err != nil || required {
		t.Fatalf("fresh commitment required a temporal revision: required=%t err=%v", required, err)
	}
	required, err = fleetCommitmentRecoveryRequiredAt(fixture.cfg, fixture.stateDir, fixture.plan, fixture.entries, safeHead+1)
	if err != nil || !required {
		t.Fatalf("unsafe commitment did not require a temporal revision: required=%t err=%v", required, err)
	}

	pending := fixture.reviseAt(t, safeHead+1)
	required, err = fleetCommitmentRecoveryRequiredAt(fixture.cfg, fixture.stateDir, pending, fixture.entries, safeHead+2)
	if err != nil || required {
		t.Fatalf("approved pending recovery recursively revised: required=%t err=%v", required, err)
	}
	consumer := actionByID(t, fixture.plan, "fleet.install.batch.4")
	consumedEntries := append(append([]JournalEntry(nil), fixture.entries...), JournalEntry{
		Sequence: 3, PlanHash: fixture.plan.PlanHash, ActionID: consumer.ID, IntentHash: consumer.IntentHash, Stage: StageVerified,
	})
	required, err = fleetCommitmentRecoveryRequiredAt(fixture.cfg, fixture.stateDir, fixture.plan, consumedEntries, safeHead+2)
	if err != nil || required {
		t.Fatalf("consumed commitment required a temporal revision: required=%t err=%v", required, err)
	}
}

func TestFleetRefreshAndChallengerCommitmentsUseTheSameRecoveryBoundary(t *testing.T) {
	fixtures := []fleetCommitmentRecoveryFixture{
		newFleetCommitmentRecoveryFixture(t, 31, 2),
		newFleetCommitmentRecoveryFixture(t, 201, 1),
	}
	for _, fixture := range fixtures {
		unsafeHead := fixture.evidence.FinalizedBlock + fixture.maximumAge - fleetCommitmentInclusionSafetyBlocks + 1
		revised := fixture.reviseAt(t, unsafeHead)
		recovery := actionByID(t, revised, fixture.action.ID)
		if recovery.Parameters[fleetCommitmentRecoveryBlockParameter] != "100" || recovery.Parameters[fleetCommitmentRecoveryCountParameter] != "1" {
			t.Errorf("%s did not recover at the shared boundary: %+v", fixture.action.ID, recovery)
		}
		funding := actionByID(t, revised, fmt.Sprintf("fleet.fund-hotkey.%d", suffixInt(fixture.action.ID)))
		if funding.Parameters[fleetCommitmentFundingCountParameter] != "1" {
			t.Errorf("%s did not fund its recovery: %+v", fixture.action.ID, funding)
		}
	}
}

func TestFleetCommitmentRecoverySkipsAlreadyConsumedGeneration(t *testing.T) {
	fixture := newFleetCommitmentRecoveryFixture(t, 31, 1)
	consumer := actionByID(t, fixture.plan, "fleet.install.batch.4")
	fixture.entries = append(fixture.entries, JournalEntry{
		Sequence: 3, PlanHash: fixture.plan.PlanHash, ActionID: consumer.ID, IntentHash: consumer.IntentHash, Stage: StageVerified,
	})
	if err := os.Remove(filepath.Join(fixture.stateDir, "public", "fleet-31.commitment.json")); err != nil {
		t.Fatal(err)
	}
	revised := fixture.reviseAt(t, fixture.evidence.FinalizedBlock+fixture.maximumAge+100)
	if action := actionByID(t, revised, fixture.action.ID); action.Parameters[fleetCommitmentRecoveryBlockParameter] != "" || action.IntentHash != fixture.action.IntentHash {
		t.Fatalf("consumed commitment was needlessly recovered: %+v", action)
	}
}

func TestFleetCommitmentConsumptionRequiresTheExactGenerationConsumer(t *testing.T) {
	fixture := newFleetCommitmentRecoveryFixture(t, 31, 1)
	tests := []struct {
		fleet      int
		generation uint64
		consumerID string
	}{
		{fleet: 31, generation: 1, consumerID: "fleet.install.batch.4"},
		{fleet: 31, generation: 2, consumerID: "fleet.refresh.batch.4"},
		{fleet: 201, generation: 1, consumerID: "fleet.mirror.201"},
	}
	for _, test := range tests {
		consumer := actionByID(t, fixture.plan, test.consumerID)
		wrongPlan := JournalEntry{PlanHash: "0x" + strings.Repeat("99", 32), ActionID: consumer.ID, IntentHash: consumer.IntentHash, Stage: StageVerified}
		wrongIntent := JournalEntry{PlanHash: fixture.plan.PlanHash, ActionID: consumer.ID, IntentHash: "0x" + strings.Repeat("98", 32), Stage: StageVerified}
		wrongConsumer := actionByID(t, fixture.plan, "fleet.install.batch.3")
		adjacent := JournalEntry{PlanHash: fixture.plan.PlanHash, ActionID: wrongConsumer.ID, IntentHash: wrongConsumer.IntentHash, Stage: StageVerified}
		executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, journal: &Journal{entries: []JournalEntry{wrongPlan, wrongIntent, adjacent}}}
		consumerID, consumed, err := executor.consumedFleetCommitmentGeneration(test.fleet, test.generation)
		if err != nil || consumed || consumerID != test.consumerID {
			t.Fatalf("generation %d fleet %d accepted adjacent consumer: id=%s consumed=%t err=%v", test.generation, test.fleet, consumerID, consumed, err)
		}
		executor.journal.entries = append(executor.journal.entries, JournalEntry{
			PlanHash: fixture.plan.PlanHash, ActionID: consumer.ID, IntentHash: consumer.IntentHash, Stage: StageVerified,
		})
		consumerID, consumed, err = executor.consumedFleetCommitmentGeneration(test.fleet, test.generation)
		if err != nil || !consumed || consumerID != test.consumerID {
			t.Fatalf("generation %d fleet %d exact consumer rejected: id=%s consumed=%t err=%v", test.generation, test.fleet, consumerID, consumed, err)
		}
	}
	if _, _, err := (&Executor{cfg: fixture.cfg, plan: fixture.plan, journal: &Journal{}}).consumedFleetCommitmentGeneration(1, 3); err == nil {
		t.Fatal("unsupported fleet commitment generation was accepted")
	}
}

func TestFleetCommitmentRecoveryRejectsMissingOrUnauthenticatedEvidence(t *testing.T) {
	fixture := newFleetCommitmentRecoveryFixture(t, 31, 1)
	unsafeHead := fixture.evidence.FinalizedBlock + fixture.maximumAge + 1
	evidencePath := filepath.Join(fixture.stateDir, "public", "fleet-31.commitment.json")
	if err := os.Remove(evidencePath); err != nil {
		t.Fatal(err)
	}
	current := *testSetupFacts()
	current.FinalizedBlock = unsafeHead
	if _, err := buildPlanRevisionFromFacts(fixture.cfg, fixture.stateDir, fixture.plan, &current, fixture.entries, time.Unix(2, 0)); err == nil || !strings.Contains(err.Error(), "load fleet.commitment.31 recovery evidence") {
		t.Fatalf("missing recovery evidence was accepted: %v", err)
	}
	if err := writePublicJSON(evidencePath, fixture.evidence); err != nil {
		t.Fatal(err)
	}
	fixture.entries[0].TransactionHash = "0x" + strings.Repeat("55", 32)
	if _, err := buildPlanRevisionFromFacts(fixture.cfg, fixture.stateDir, fixture.plan, &current, fixture.entries, time.Unix(2, 0)); err == nil || !strings.Contains(err.Error(), "does not name an approved finalized transaction") {
		t.Fatalf("unauthenticated recovery evidence was accepted: %v", err)
	}
}

func TestFleetCommitmentRecoveryCarriesFreshReplacementAndRepeatsFromNewEvidence(t *testing.T) {
	fixture := newFleetCommitmentRecoveryFixture(t, 31, 1)
	firstHead := fixture.evidence.FinalizedBlock + fixture.maximumAge + 1
	first := fixture.reviseAt(t, firstHead)
	firstAction := actionByID(t, first, fixture.action.ID)
	persistFleetCommitmentRecoveryTestPlan(t, fixture.stateDir, first)

	fixture.plan = first
	fixture.evidence.ExtrinsicHash = "0x" + strings.Repeat("66", 32)
	fixture.evidence.CommitmentBlock = 800
	fixture.evidence.FinalizedBlock = 800
	fixture.evidence.FinalizedBlockHash = "0x" + strings.Repeat("77", 32)
	if err := writePublicJSON(filepath.Join(fixture.stateDir, "public", "fleet-31.commitment.json"), fixture.evidence); err != nil {
		t.Fatal(err)
	}
	fixture.entries = append(fixture.entries,
		JournalEntry{Sequence: 3, PlanHash: first.PlanHash, ActionID: firstAction.ID, IntentHash: firstAction.IntentHash, Stage: StageFinalized, TransactionHash: fixture.evidence.ExtrinsicHash, BlockNumber: 800, BlockHash: fixture.evidence.FinalizedBlockHash},
		JournalEntry{Sequence: 4, PlanHash: first.PlanHash, ActionID: firstAction.ID, IntentHash: firstAction.IntentHash, Stage: StageVerified},
	)
	fresh := fixture.reviseAt(t, 800+fixture.maximumAge-fleetCommitmentInclusionSafetyBlocks)
	freshAction := actionByID(t, fresh, firstAction.ID)
	if freshAction.IntentHash != firstAction.IntentHash || freshAction.Parameters[fleetCommitmentRecoveryBlockParameter] != "100" || freshAction.Parameters[fleetCommitmentRecoveryCountParameter] != "1" {
		t.Fatalf("fresh replacement was not carried exactly: %+v", freshAction)
	}

	persistFleetCommitmentRecoveryTestPlan(t, fixture.stateDir, fresh)
	fixture.plan = fresh
	second := fixture.reviseAt(t, 800+fixture.maximumAge-fleetCommitmentInclusionSafetyBlocks+1)
	secondAction := actionByID(t, second, firstAction.ID)
	if secondAction.IntentHash == firstAction.IntentHash || secondAction.Parameters[fleetCommitmentRecoveryBlockParameter] != "800" || secondAction.Parameters[fleetCommitmentRecoveryCountParameter] != "2" {
		t.Fatalf("repeated recovery did not bind the new evidence: %+v", secondAction)
	}
	if actionByID(t, second, "fleet.fund-hotkey.31").Parameters[fleetCommitmentFundingCountParameter] != "2" ||
		actionByID(t, second, "wallet.native-fee-reserve").Parameters[fleetCommitmentReserveCountParameter] != "2" {
		t.Fatal("repeated recovery did not preserve cumulative funding and fee reserve counts")
	}
}

func TestFleetCommitmentRecoveryPlanValidationRejectsAdjacentExceptions(t *testing.T) {
	fixture := newFleetCommitmentRecoveryFixture(t, 31, 1)
	plan := fixture.reviseAt(t, fixture.evidence.FinalizedBlock+fixture.maximumAge+1)
	mutations := []func(*SetupPlan){
		func(value *SetupPlan) {
			action := &value.Actions[0]
			action.Parameters = cloneStrings(action.Parameters)
			action.Parameters[fleetCommitmentRecoveryBlockParameter] = "100"
			action.Parameters[fleetCommitmentRecoveryCountParameter] = "1"
			action.Parameters[fleetCommitmentRecoveryFeeParameter] = "3000000"
		},
		func(value *SetupPlan) {
			action := findMutableAction(value, fixture.action.ID)
			action.Parameters[fleetCommitmentRecoveryCountParameter] = "01"
		},
		func(value *SetupPlan) {
			action := findMutableAction(value, fixture.action.ID)
			delete(action.Parameters, fleetCommitmentRecoveryBlockParameter)
			delete(action.Parameters, fleetCommitmentRecoveryCountParameter)
		},
		func(value *SetupPlan) {
			action := findMutableAction(value, fixture.action.ID)
			action.AcceptedPriorIntentHashes = []string{"0x" + strings.Repeat("99", 32)}
		},
		func(value *SetupPlan) { action := findMutableAction(value, fixture.action.ID); action.Spend.TAORao = 1 },
		func(value *SetupPlan) {
			action := findMutableAction(value, "fleet.fund-hotkey.31")
			action.Parameters[fleetCommitmentFundingCountParameter] = "2"
		},
		func(value *SetupPlan) {
			action := findMutableAction(value, "wallet.native-fee-reserve")
			action.Parameters[fleetCommitmentReserveCountParameter] = "2"
		},
	}
	for index, mutate := range mutations {
		candidate := *plan
		candidate.Actions = append([]Action(nil), plan.Actions...)
		for actionIndex := range candidate.Actions {
			candidate.Actions[actionIndex].Parameters = cloneStrings(candidate.Actions[actionIndex].Parameters)
			candidate.Actions[actionIndex].DependsOn = append([]string(nil), candidate.Actions[actionIndex].DependsOn...)
			candidate.Actions[actionIndex].AcceptedPriorIntentHashes = append([]string(nil), candidate.Actions[actionIndex].AcceptedPriorIntentHashes...)
		}
		mutate(&candidate)
		for actionIndex := range candidate.Actions {
			candidate.Actions[actionIndex].IntentHash, _ = actionIntentHash(candidate.Actions[actionIndex])
		}
		candidate.MaximumSpend, _ = maximumActionSpend(candidate.Actions)
		candidate.PlanHash, _ = candidate.hash()
		if err := validatePlanBudget(&candidate); err == nil {
			t.Errorf("adjacent recovery mutation %d was accepted", index)
		}
	}
	baseMutations := []func(*SetupPlan){
		func(value *SetupPlan) {
			findMutableAction(value, "fleet.fund-hotkey.31").Parameters[fleetCommitmentFundingCountParameter] = "1"
		},
		func(value *SetupPlan) {
			findMutableAction(value, "wallet.native-fee-reserve").Parameters[fleetCommitmentReserveCountParameter] = "1"
		},
	}
	for index, mutate := range baseMutations {
		candidate := *fixture.plan
		candidate.Actions = append([]Action(nil), fixture.plan.Actions...)
		for actionIndex := range candidate.Actions {
			candidate.Actions[actionIndex].Parameters = cloneStrings(candidate.Actions[actionIndex].Parameters)
		}
		mutate(&candidate)
		if err := validatePlanBudget(&candidate); err == nil {
			t.Errorf("unmatched base recovery marker %d was accepted", index)
		}
	}
}

func findMutableAction(plan *SetupPlan, actionID string) *Action {
	for index := range plan.Actions {
		if plan.Actions[index].ID == actionID {
			return &plan.Actions[index]
		}
	}
	panic("test plan action not found: " + actionID)
}

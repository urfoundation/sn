package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	servercontroller "github.com/urnetwork/server/v2026/controller"
)

func fleetLifecycleFilterDriverFixture(t *testing.T) (*ResolvedConfig, *liveScenarioFaultDriver, scenarioFaultSpec, scenarioFaultSpec) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, fleet := range []int{validatorLocalHeadBoundaryFleet, fleetLifecycleTargetFleet, fleetLifecycleCompanionFleet} {
		for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			miner := fleetMemberMinerIndex(cfg, fleet, member)
			role := roles.Clients[fmt.Sprintf("miner-%d", miner)]
			role.ClientIDHex = fmt.Sprintf("%032x", miner)
			roles.Clients[fmt.Sprintf("miner-%d", miner)] = role
		}
	}
	stateDir := t.TempDir()
	if err := saveRoleSecrets(filepath.Join(stateDir, "secrets", "roles.json"), roles); err != nil {
		t.Fatal(err)
	}
	planHash := "0x" + strings.Repeat("11", 32)
	processes := make([]ProcessSpec, 0, cfg.Config.Topology.Operators)
	states := make([]ProcessState, 0, cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		process := ProcessSpec{
			ID: fmt.Sprintf("operator-%d-api", operator), Role: "operator-api", Identity: fmt.Sprintf("no:%d", operator),
			Env: map[string]string{
				servercontroller.VerifySimulationAssignmentFilterFileEnv:     verifyAssignmentFilterPath(stateDir, operator),
				servercontroller.VerifySimulationAssignmentFilterPlanHashEnv: planHash,
			},
		}
		processes = append(processes, process)
		states = append(states, ProcessState{ID: process.ID, Role: process.Role, Identity: process.Identity, PID: os.Getpid(), Healthy: true})
	}
	manifest := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, BinaryHash: "hash", Specs: processes}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", SupervisorPID: os.Getpid(), ManifestHash: manifestHash, Processes: states}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.state.json"), state); err != nil {
		t.Fatal(err)
	}
	headFault, err := releaseHeadBoundaryFault(cfg, 30)
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := validatorLocalHeadBoundaryFault(cfg, headFault)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := releaseFleetLifecycleFaults(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(lifecycle) != 2 || operatorForMiner(cfg, fleetMemberMinerIndex(cfg, fleetLifecycleCompanionFleet, 1)) != 2 {
		t.Fatal("fleet lifecycle filter fixture has unexpected operator geometry")
	}
	driver := &liveScenarioFaultDriver{stateDir: stateDir, cfg: cfg, planHash: planHash, coordinator: "0x" + strings.Repeat("22", 20)}
	return cfg, driver, boundary, lifecycle[1]
}

func TestFleetLifecycleFilterComposesWithBoundaryRuleAndRemovesExactly(t *testing.T) {
	_, driver, boundary, companion := fleetLifecycleFilterDriverFixture(t)
	if _, err := driver.applyValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.applyValidatorViewFilter(context.Background(), boundary); err != nil {
		t.Fatal(err)
	}
	path := verifyAssignmentFilterPath(driver.stateDir, 2)
	var filter validatorViewFilterFile
	if err := readJSONFile(path, &filter); err != nil {
		t.Fatal(err)
	}
	if len(filter.Rules) != 2 || !sort.SliceIsSorted(filter.Rules, func(i, j int) bool { return filter.Rules[i].RuleID < filter.Rules[j].RuleID }) || filter.Rules[0].RuleID != companion.ID || filter.Rules[1].RuleID != boundary.ID {
		t.Fatalf("composed rules are not canonical: %+v", filter.Rules)
	}
	if len(filter.Rules[0].ValidatorVPKs) != driver.cfg.Config.Topology.Validators || len(filter.Rules[1].ValidatorVPKs) != 1 {
		t.Fatalf("composed rule validator censuses=%d/%d", len(filter.Rules[0].ValidatorVPKs), len(filter.Rules[1].ValidatorVPKs))
	}
	if _, err := driver.restoreValidatorViewFilter(context.Background(), boundary); err != nil {
		t.Fatal(err)
	}
	if err := readJSONFile(path, &filter); err != nil {
		t.Fatal(err)
	}
	if len(filter.Rules) != 1 || filter.Rules[0].RuleID != companion.ID {
		t.Fatalf("boundary removal damaged lifecycle rule: %+v", filter.Rules)
	}
	if _, err := driver.restoreValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("final filter survived exact removals: %v", err)
	}
}

func TestFleetLifecycleFilterConcurrentRulesCannotLoseAnUpdate(t *testing.T) {
	_, driver, boundary, companion := fleetLifecycleFilterDriverFixture(t)
	start := make(chan struct{})
	errorsByRule := make(chan error, 2)
	var workers sync.WaitGroup
	for _, fault := range []scenarioFaultSpec{boundary, companion} {
		fault := fault
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := driver.applyValidatorViewFilter(context.Background(), fault)
			errorsByRule <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errorsByRule)
	for err := range errorsByRule {
		if err != nil {
			t.Fatal(err)
		}
	}
	var filter validatorViewFilterFile
	if err := readJSONFile(verifyAssignmentFilterPath(driver.stateDir, 2), &filter); err != nil {
		t.Fatal(err)
	}
	if len(filter.Rules) != 2 || filter.Rules[0].RuleID != companion.ID || filter.Rules[1].RuleID != boundary.ID {
		t.Fatalf("concurrent filter writes lost or reordered a rule: %+v", filter.Rules)
	}
}

func TestFleetLifecycleFilterRejectsForeignDeploymentWithoutMutation(t *testing.T) {
	_, driver, _, companion := fleetLifecycleFilterDriverFixture(t)
	operator, _, rule, err := driver.validatorViewRule(companion)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := driver.validatorViewFilterIdentity(operator)
	if err != nil {
		t.Fatal(err)
	}
	identity.DeploymentID = "foreign-deployment"
	identity.Rules = []validatorViewFilterRule{rule}
	path := verifyAssignmentFilterPath(driver.stateDir, operator)
	encoded, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := atomicWrite(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.applyValidatorViewFilter(context.Background(), companion); err == nil {
		t.Fatal("foreign deployment filter was silently replaced")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(encoded) {
		t.Fatal("rejected foreign filter was mutated")
	}
}

func TestFleetLifecycleFilterRejectsAmbiguousCrossRulePair(t *testing.T) {
	_, driver, boundary, _ := fleetLifecycleFilterDriverFixture(t)
	operator, _, rule, err := driver.validatorViewRule(boundary)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := driver.validatorViewFilterIdentity(operator)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := rule
	duplicate.RuleID = "different-rule"
	identity.Rules = []validatorViewFilterRule{duplicate, rule}
	sort.Slice(identity.Rules, func(i, j int) bool { return identity.Rules[i].RuleID < identity.Rules[j].RuleID })
	if err := validateValidatorViewFilter(identity, identity); err == nil {
		t.Fatal("duplicate validator/client cross-product was accepted across rules")
	}
}

func TestFleetLifecycleFilterExactRemovalRejectsRuleContentDrift(t *testing.T) {
	_, driver, boundary, companion := fleetLifecycleFilterDriverFixture(t)
	if _, err := driver.applyValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.applyValidatorViewFilter(context.Background(), boundary); err != nil {
		t.Fatal(err)
	}
	path := verifyAssignmentFilterPath(driver.stateDir, 2)
	var filter validatorViewFilterFile
	if err := readJSONFile(path, &filter); err != nil {
		t.Fatal(err)
	}
	for index := range filter.Rules {
		if filter.Rules[index].RuleID == companion.ID {
			filter.Rules[index].ExcludedClientIDs = append([]string(nil), filter.Rules[index].ExcludedClientIDs[1:]...)
		}
	}
	if err := writePublicJSON(path, filter); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.restoreValidatorViewFilter(context.Background(), companion); err == nil {
		t.Fatal("content-drifted lifecycle rule was removed")
	}
	if err := readJSONFile(path, &filter); err != nil || len(filter.Rules) != 2 {
		t.Fatalf("failed exact removal damaged peer rules: rules=%+v error=%v", filter.Rules, err)
	}
}

func TestFleetLifecycleFilterRestoreResumesAfterRuleRemovalBeforeLedgerCommit(t *testing.T) {
	_, driver, _, companion := fleetLifecycleFilterDriverFixture(t)
	if _, err := driver.applyValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.restoreValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	operator, _, rule, err := driver.validatorViewRule(companion)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := validatorViewRestoreReceiptPath(driver.stateDir, operator, rule.RuleID)
	if _, err := os.Stat(receiptPath); err != nil {
		t.Fatalf("durable restore receipt: %v", err)
	}
	// This is the exact crash state: the rule mutation is durable while the
	// active-fault ledger still asks Recover to restore the same fault.
	if _, err := driver.restoreValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatalf("resume exact completed removal: %v", err)
	}
}

func TestFleetLifecycleFilterRestoreRejectsAbsentRuleWithoutDurableIntent(t *testing.T) {
	_, driver, _, companion := fleetLifecycleFilterDriverFixture(t)
	if _, err := driver.applyValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	operator, _, _, err := driver.validatorViewRule(companion)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(verifyAssignmentFilterPath(driver.stateDir, operator)); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.restoreValidatorViewFilter(context.Background(), companion); err == nil {
		t.Fatal("absent rule without a durable exact removal intent was accepted")
	}
}

func TestFleetLifecycleFilterRestoreRejectsTamperedDurableIntent(t *testing.T) {
	_, driver, _, companion := fleetLifecycleFilterDriverFixture(t)
	if _, err := driver.applyValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.restoreValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	operator, _, rule, err := driver.validatorViewRule(companion)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := validatorViewRestoreReceiptPath(driver.stateDir, operator, rule.RuleID)
	var receipt validatorViewRestoreReceipt
	if err := readJSONFile(receiptPath, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.PlanHash = "0x" + strings.Repeat("ff", 32)
	if err := writeValidatorViewRestoreReceipt(receiptPath, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.restoreValidatorViewFilter(context.Background(), companion); err == nil {
		t.Fatal("tampered restore intent authorized an absent rule")
	}
}

func TestFleetLifecycleFilterReapplyClearsPriorRestoreIntent(t *testing.T) {
	_, driver, _, companion := fleetLifecycleFilterDriverFixture(t)
	if _, err := driver.applyValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.restoreValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	operator, _, rule, err := driver.validatorViewRule(companion)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := validatorViewRestoreReceiptPath(driver.stateDir, operator, rule.RuleID)
	if _, err := driver.applyValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(receiptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reapplication retained a prior restore intent: %v", err)
	}
	var filter validatorViewFilterFile
	if err := readJSONFile(verifyAssignmentFilterPath(driver.stateDir, operator), &filter); err != nil || len(filter.Rules) != 1 || filter.Rules[0].RuleID != companion.ID {
		t.Fatalf("reapplied exact rule=%+v error=%v", filter.Rules, err)
	}
}

func TestFleetLifecycleFilterCleanRecoveryRemovesRestoreIntents(t *testing.T) {
	_, driver, _, companion := fleetLifecycleFilterDriverFixture(t)
	if _, err := driver.applyValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.restoreValidatorViewFilter(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	operator, _, rule, err := driver.validatorViewRule(companion)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := validatorViewRestoreReceiptPath(driver.stateDir, operator, rule.RuleID)
	if err := driver.removeOrphanValidatorViewFilters(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(receiptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clean recovery retained a restore receipt: %v", err)
	}
}

func TestFleetLifecycleOperatorProcessesBindExactApprovedPlanHash(t *testing.T) {
	cfg := testResolvedConfig(t)
	planHash := "0x" + strings.Repeat("ab", 32)
	stateDir := t.TempDir()
	specs, err := buildServerSpecs(cfg, stateDir, map[string]string{
		"sim-testnet": "/release/sim-testnet", connectServerBinaryName: "/release/sim-testnet-connect",
	}, planHash)
	if err != nil {
		t.Fatal(err)
	}
	operatorAPIs := 0
	for _, spec := range specs {
		if spec.Role != "operator-api" {
			continue
		}
		operatorAPIs++
		if spec.Env[servercontroller.VerifySimulationAssignmentFilterPlanHashEnv] != planHash || spec.Env[servercontroller.VerifySimulationAssignmentFilterFileEnv] != verifyAssignmentFilterPath(stateDir, operatorAPIs) {
			t.Fatalf("operator API %s filter environment=%+v", spec.ID, spec.Env)
		}
	}
	if operatorAPIs != cfg.Config.Topology.Operators {
		t.Fatalf("operator API census=%d, want %d", operatorAPIs, cfg.Config.Topology.Operators)
	}
}

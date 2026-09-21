package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	validatorpkg "github.com/urfoundation/sn/validator"
)

func provisionalEpochFixture(t *testing.T) (*ResolvedConfig, scenarioDefinition, *ScenarioObservation, *ScenarioObservation) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}
	definition, err := scenarioDefinitionFor(cfg, "epoch")
	if err != nil {
		t.Fatal(err)
	}
	start, current := testScenarioObservation(cfg, 525), testScenarioObservation(cfg, 526)
	for _, observation := range []*ScenarioObservation{start, current} {
		observation.PublicIdentitiesValid = true
		observation.ReserveValidatorRegistered, observation.EscrowHotkeyRegistered = true, true
		observation.Status.Healthy = false
		observation.Status.Supervisor = &SupervisorState{SupervisorPID: 123, SupervisorStartTimeTicks: 456}
		for swarm := 1; swarm <= 20; swarm++ {
			observation.Status.Supervisor.Processes = append(observation.Status.Supervisor.Processes, ProcessState{ID: fmt.Sprintf("miner-swarm-%d", swarm), PID: 1_000 + swarm})
		}
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			observation.Operators = append(observation.Operators, OperatorObservation{NoID: noID, Healthy: true, Error: "stats: context deadline exceeded; proofs: context deadline exceeded"})
		}
		for id := 1; id <= cfg.Config.Topology.Validators; id++ {
			counts := map[int]int{}
			for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
				counts[noID] = int(observation.Status.Contracts.CurrentEpoch)
			}
			observation.Validators = append(observation.Validators, ValidatorObservation{ValidatorID: id, PathProofCounts: counts})
		}
		observation.PrecompileConformanceError = "retained precompile evidence belongs to earlier plan"
	}
	return cfg, definition, start, current
}

func TestProvisionalEpochCompletesWithDeferredFindingsWithoutAcceptance(t *testing.T) {
	t.Parallel()
	cfg, definition, start, current := provisionalEpochFixture(t)
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runScenarioWithProbe(ctx, cfg, dir, definition, &staticScenarioProbe{observations: []*ScenarioObservation{start, current}}, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, Publish: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result != "fail" || result.FailedAssertionCount == 0 || result.ProvisionalEpoch == nil || result.ProvisionalEpoch.Status != "complete" || result.FinalAcceptance == nil || *result.FinalAcceptance || result.Anomalies.Status != "open" {
		t.Fatalf("operational completion promoted failed release evidence: %+v", result)
	}
	for _, id := range []string{"topology_healthy", "operator_public_apis", "fleet_commitment_and_bindings", "per_no_verify_coverage", "payout_artifacts_reconstruct", "validator_local_v2_receipts_finalized_and_applied", "evidence_publication"} {
		index := slices.IndexFunc(result.Assertions, func(record AssertionRecord) bool { return record.ID == id })
		if index < 0 || result.Assertions[index].Passed || !result.Assertions[index].DeferredToFinalAcceptance || !slices.Contains(result.ProvisionalEpoch.DeferredAssertions, id) {
			t.Fatalf("lost honest deferred finding %s", id)
		}
	}
	if len(result.ProvisionalEpoch.BlockingAnomalies) != 0 || len(result.ProvisionalEpoch.DeferredAnomalies) == 0 || assertionsPass(result.Assertions) {
		t.Fatal("strict evaluator accepted deferred observations")
	}
	runDir := filepath.Join(dir, "runs", result.RunID)
	if _, err := os.Stat(filepath.Join(runDir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provisional observation wrote a strict completion marker: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(runDir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted ScenarioResult
	if err := json.Unmarshal(raw, &persisted); err != nil || persisted.ProvisionalEpoch.Status != "complete" || persisted.Result != "fail" || persisted.EvidenceHash != result.EvidenceHash {
		t.Fatalf("persisted outcome lost evidence: %v", err)
	}
	if hash, err := canonicalScenarioResultHash(&persisted); err != nil || hash != result.EvidenceHash {
		t.Fatalf("operational outcome is not bound by the result hash: %v", err)
	}
	if err := validateScenarioFinalSemanticSource(nil, nil, result, nil); err == nil {
		t.Fatal("strict acceptance admitted provisional completion")
	}
}

func TestProvisionalEpochCoreFailuresAndHistoryRemainBlocking(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"epoch", "traffic", "identity", "conservation", "history", "unknown"} {
		t.Run(fault, func(t *testing.T) {
			cfg, definition, start, current := provisionalEpochFixture(t)
			switch fault {
			case "epoch":
				current.Status.Contracts.CurrentEpoch = start.Status.Contracts.CurrentEpoch
			case "traffic":
				current.Validators[0].PathProofCounts = start.Validators[0].PathProofCounts
			case "identity":
				current.Status.Contracts.RuntimeCodeMatches = false
			case "conservation":
				current.Status.Contracts.ConservationHolds = false
			case "history":
				start.Status.Contracts.TotalCaptured = "11"
			}
			assertions := evaluateScenario(cfg, definition, start, current, nil, time.Now())
			if fault == "unknown" {
				assertions = append(assertions, AssertionRecord{ID: "new_unknown_gate", Passed: false, DeferredToFinalAcceptance: true, Message: "must not bypass unknown assertion"})
			}
			result := &ScenarioResult{Name: "epoch", Assertions: assertions}
			applyProvisionalScenarioProvenance(cfg, result)
			attachScenarioAnomalyGate(result, time.Now(), start, current)
			if result.ProvisionalEpoch.Status != "incomplete" || result.Result != "fail" {
				t.Fatalf("accepted %s: %+v", fault, result.ProvisionalEpoch)
			}
		})
	}
}

type cancelProvisionalEpochProbe struct {
	start  *ScenarioObservation
	cancel context.CancelFunc
	calls  int
}

func (p *cancelProvisionalEpochProbe) Snapshot(context.Context) (*ScenarioObservation, error) {
	p.calls++
	if p.calls == 1 {
		return p.start, nil
	}
	p.cancel()
	return nil, context.Canceled
}

func TestProvisionalEpochCancellationIsInterruptedAndSkipsPublication(t *testing.T) {
	t.Parallel()
	cfg, definition, start, _ := provisionalEpochFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &cancelProvisionalEpochProbe{start: start, cancel: cancel}
	result, err := runScenarioWithProbe(ctx, cfg, t.TempDir(), definition, probe, scenarioRunOptions{PollInterval: time.Microsecond, Timeout: time.Hour, Publish: true})
	if err == nil || result == nil || result.ProvisionalEpoch.Status != "interrupted" || result.ProvisionalEpoch.Interruption == "" || result.Result != "fail" {
		t.Fatalf("cancellation promoted to completion: %v %+v", err, result)
	}
	index := slices.IndexFunc(result.Assertions, func(record AssertionRecord) bool { return record.ID == "evidence_publication" })
	if index < 0 || !strings.Contains(result.Assertions[index].Message, "context canceled") || !result.Assertions[index].DeferredToFinalAcceptance {
		t.Fatal("interrupted publication attempted or not retained as deferred")
	}
}

func TestProvisionalEpochNativeActivityRequiresFreshAppliedReceipt(t *testing.T) {
	t.Parallel()
	cfg, definition, start, current := provisionalEpochFixture(t)
	start.NativeRewards = &NativeRewardObservation{FinalizedHead: ChainHead{Number: 100, Hash: "0x" + strings.Repeat("ab", 32)}}
	for index := range current.Validators {
		current.Validators[index].PathProofCounts = nil
		current.Validators[index].LocalRuntimeIntents = &validatorpkg.ProvisionalIntentObservationV2{
			Scope: "local-runtime-observation", State: "observed", StoreSHA256: "sha256:" + strings.Repeat("ab", 32), HandoffSHA256: "sha256:" + strings.Repeat("cd", 32),
			Receipts: []validatorpkg.ProvisionalIntentReceiptV2{{Status: "applied", FinalizedBlock: 101, ApplicationBlock: 102, ExtrinsicHash: "0x" + strings.Repeat("ef", 32), FinalizedBlockHash: "0x" + strings.Repeat("12", 32), ApplicationBlockHash: "0x" + strings.Repeat("34", 32)}},
		}
	}
	evaluation := &scenarioEvaluation{Cfg: cfg, Definition: definition, Start: start, Current: current}
	if ok, message := provisionalEpochActivity(evaluation); !ok {
		t.Fatal(message)
	}
	current.Validators[0].LocalRuntimeIntents.Receipts[0].ApplicationBlock = 100
	if ok, _ := provisionalEpochActivity(evaluation); ok {
		t.Fatal("historical native receipt was counted as fresh activity")
	}
}

func TestProvisionalEpochDeferralDoesNotChangeStrictOrFullCampaignChecks(t *testing.T) {
	t.Parallel()
	cfg, definition, start, current := provisionalEpochFixture(t)
	assertions := evaluateScenario(cfg, definition, start, current, nil, time.Now())
	if !scenarioAssertionsComplete(cfg, definition, assertions) {
		t.Fatal("fixture did not satisfy operational core")
	}
	for _, name := range []string{"release-1.0", "production-soak", "smoke"} {
		if scenarioAssertionsComplete(cfg, scenarioDefinition{Name: name}, assertions) {
			t.Fatalf("provisional epoch deferral escaped into %s", name)
		}
	}
	cfg.provisionalResume = nil
	if scenarioAssertionsComplete(cfg, definition, assertions) {
		t.Fatal("strict epoch accepted deferred failures")
	}
}

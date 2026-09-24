package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type provisionalProductionTestFixture struct {
	history *historicalLifecycleFixture
	result  *ScenarioResult
	runDir  string
	source  map[string][]byte
}

// The real recovery fixture retains a different original lifecycle plan. The
// terminal observations and current source are signed with the actual owner;
// failures remain in the result rather than being turned into clean receipts.
func newProvisionalProductionTestFixture(t *testing.T, terminalEdits ...func(*ScenarioObservation)) *provisionalProductionTestFixture {
	t.Helper()
	f := newHistoricalLifecycleFixture(t)
	if err := f.lifecycle.BeginPhase("release-1.0", f.attempt.payload.RunID); err != nil {
		t.Fatal(err)
	}
	runDir := bindFailedRecoveryGeneration(t, f.campaign, f.attempt, 200)
	boundary := f.attempt.payload.AcceptanceBoundary
	started, err := time.Parse(time.RFC3339Nano, f.attempt.payload.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	completed := started.Add(8 * time.Hour)
	terminalBlock := boundary.AcceptanceWindow.TerminalBlock + 1
	for index := range boundary.Faults {
		fault := &boundary.Faults[index]
		fault.Status, fault.AppliedBlock, fault.AppliedBlockHash = "restored", fault.TriggerBlock, "0x"+strings.Repeat("b1", 32)
		fault.RestoredBlock, fault.RestoredBlockHash = fault.RestoreBlock, "0x"+strings.Repeat("b2", 32)
		for index, target := range fault.Targets {
			fault.Processes = append(fault.Processes, FaultProcessEvidence{ID: target, Role: "fixture", Identity: target, PID: 100 + index})
			fault.RestoredProcesses = append(fault.RestoredProcesses, FaultProcessEvidence{ID: target, Role: "fixture", Identity: target, PID: 200 + index})
		}
		if fault.RestoredBlock >= terminalBlock {
			terminalBlock = fault.RestoredBlock + 1
		}
	}
	terminalEpoch := boundary.AcceptanceWindow.FirstEpoch + (terminalBlock-boundary.AcceptanceWindow.StartBlock)/boundary.AcceptanceWindow.EpochBlocks
	terminal := testScenarioObservation(f.campaign.cfg, terminalEpoch)
	terminal.PublicIdentitiesValid = true
	terminal.ReserveValidatorRegistered, terminal.EscrowHotkeyRegistered = true, true
	terminal.Status.Contracts.FinalizedHead = ChainHead{Number: terminalBlock, Hash: "0x" + strings.Repeat("a7", 32)}
	terminal.ObservedAt = completed.Add(-time.Second).Format(time.RFC3339Nano)
	for _, edit := range terminalEdits {
		edit(terminal)
	}
	terminal.ObservationHash = ""
	terminal.ObservationHash, err = canonicalHashHex(terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), terminal); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(runDir, "observations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	boundary.LastObservationHead, boundary.LastObservationEpoch, boundary.LastObservationHash = terminal.Status.Contracts.FinalizedHead, terminal.Status.Contracts.CurrentEpoch, terminal.ObservationHash
	boundary.ObservationLogContentHash, boundary.ObservationLogBytes = bytesSHA256(log), uint64(len(log))
	f.attempt.payload.AcceptanceInvalidatedAt = completed.Add(time.Second).Format(time.RFC3339Nano)
	if err := writeScenarioCampaignAttempt(f.attempt); err != nil {
		t.Fatal(err)
	}
	binding, err := captureScenarioLifecycleHandoff(f.campaign.cfg, f.campaign.stateDir, runDir, f.attempt.payload.RunID, f.attempt)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := scenarioDefinitionFor(f.campaign.cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	assertions := []AssertionRecord{}
	for _, check := range definition.Checks {
		assertions = append(assertions, AssertionRecord{ID: check.ID, Passed: true, Message: "fixture observation"})
	}
	assertions = append(assertions, AssertionRecord{ID: "retained_native_receipt_failure", Passed: false, Message: "original local native intent evidence was absent"}, AssertionRecord{ID: "retained_operator_timeout", Passed: false, Message: "original operator verification GET timed out"})
	accepted := false
	result := &ScenarioResult{Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: f.attempt.payload.RunID, Name: "release-1.0", DeploymentID: f.campaign.cfg.Config.Deployment.DeploymentID, ChainID: f.campaign.cfg.ChainID, GenesisHash: f.campaign.cfg.Public.Chain.GenesisHash, Netuid: f.campaign.cfg.Netuid, ConfigHash: f.campaign.cfg.ConfigHash, PolicyHash: f.campaign.cfg.PolicyHash, ScenarioDefinition: boundary.ScenarioDefinitionHash, ScenarioMatrix: definition.MatrixHash, AdversarialMatrix: definition.AdversarialMatrixHash, StartedAt: f.attempt.payload.StartedAt, CompletedAt: completed.Format(time.RFC3339Nano), Provisional: true, FinalAcceptance: &accepted, Result: "fail", CampaignStartHead: boundary.CampaignStartHead, CampaignStartEpoch: boundary.CampaignStartEpoch, StartHead: boundary.AcceptanceWindow.BaselineHead, StartEpoch: boundary.AcceptanceWindow.BaselineEpoch, EndHead: boundary.LastObservationHead, EndEpoch: boundary.LastObservationEpoch, AcceptanceWindow: &boundary.AcceptanceWindow, Assertions: assertions, Faults: cloneScenarioFaultRecords(boundary.Faults), LifecycleHandoff: binding, Adversaries: &AdversaryCampaignEvidence{Schema: "urnetwork-adversary-campaign-v1", Release: "1.0", MatrixHash: definition.AdversarialMatrixHash, Status: "stopped", StartedAt: boundary.AdversaryStartedAt, HappyPathStartedAt: boundary.AdversaryHappyPathStartedAt}}
	attachScenarioAnomalyGate(result, completed, nil, terminal)
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeScenarioOutputs(f.campaign.cfg, runDir, result, terminal); err != nil {
		t.Fatal(err)
	}
	if err := writeScenarioFaultEvidence(runDir, result.Faults); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "adversaries.json"), result.Adversaries); err != nil {
		t.Fatal(err)
	}
	f.campaign.cfg.provisionalProductionSourceRunID = result.RunID
	sources := map[string][]byte{}
	for _, path := range []string{f.attempt.path(), filepath.Join(runDir, "result.json"), filepath.Join(runDir, "assertions.json"), filepath.Join(runDir, "anomalies.json"), filepath.Join(runDir, "observations.jsonl"), filepath.Join(runDir, scenarioLifecycleHandoffFilename), filepath.Join(runDir, scenarioCampaignStartFilename)} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources[path] = raw
	}
	return &provisionalProductionTestFixture{history: f, result: result, runDir: runDir, source: sources}
}

func (f *provisionalProductionTestFixture) prepare(t *testing.T) (*ReleaseCampaignGate, error) {
	t.Helper()
	c := f.history.campaign
	return prepareProvisionalProductionHandoff(t.Context(), c.cfg, c.stateDir, c.roles, c.current, c.journal, f.result.RunID)
}

func TestProvisionalProductionHandoffRetainsFailedTerminalAndHistoricalLifecycle(t *testing.T) {
	f := newProvisionalProductionTestFixture(t)
	c := f.history.campaign
	gate, err := f.prepare(t)
	if err != nil {
		t.Fatal(err)
	}
	if gate.Schema != provisionalProductionGateSchema || gate.CompleteContentHash != "" || gate.ProvisionalHandoffHash == "" {
		t.Fatal("provisional predecessor claimed a clean completion")
	}
	if _, err := os.Stat(filepath.Join(f.runDir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed source acquired complete.json")
	}
	verified, _, err := validateExactReleaseCampaignGateContext(t.Context(), c.cfg, c.stateDir, c.roles, gate)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Result != "fail" || !reflect.DeepEqual(verified.Assertions, f.result.Assertions) || verified.FinalAcceptance == nil || *verified.FinalAcceptance {
		t.Fatal("provisional handoff changed source failure verdict")
	}
	production, err := loadOrCreateScenarioCampaignAttempt(c.cfg, c.stateDir, c.roles, c.current.PlanHash, "production-soak", gate, c.now.Add(24*time.Hour), c.journal)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &liveFleetLifecycle{cfg: c.cfg, stateDir: c.stateDir, attempt: production, executor: &Executor{cfg: c.cfg, stateDir: c.stateDir, plan: c.current, roles: c.roles, journal: c.journal}}
	preparationCalls := 0
	if err := beginScenarioCampaignPreparation(t.Context(), "production-soak", production.payload.RunID, scenarioRunOptions{Attempt: production, FleetLifecycle: lifecycle, Prepare: func(_ context.Context) error { preparationCalls++; return nil }}); err != nil {
		t.Fatal(err)
	}
	if preparationCalls != 1 || lifecycle.evidence.PlanHash != f.history.evidence.PlanHash || lifecycle.evidence.RunID != f.history.evidence.RunID || lifecycle.evidence.ProvisionalBypass == nil || lifecycle.evidence.ProvisionalBypass.FinalAcceptance || lifecycle.evidence.ProductionRunID != production.payload.RunID {
		t.Fatal("production rewrote source ownership or bypass provenance")
	}
	window := &ScenarioAcceptanceWindow{FirstEpoch: 300, EpochCount: 3, EpochBlocks: 360, StartBlock: 10000, EndBlock: 11080, FinalizeOffsetBlocks: 180, TerminalBlock: 11260}
	if err := lifecycle.BindAcceptanceWindowForPhase("production-soak", window); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Advance(t.Context(), testScenarioObservation(c.cfg, 300), nil); err != nil {
		t.Fatal(err)
	}
	// A newly constructed process has no warm historical-source cache.
	restarted := &liveFleetLifecycle{cfg: c.cfg, stateDir: c.stateDir, attempt: production, executor: lifecycle.executor}
	_, raw, err := validateExactReleaseCampaignGateContext(t.Context(), c.cfg, c.stateDir, c.roles, gate)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.AuthenticateReleaseHandoff(raw, gate.LifecycleHandoff.ContentHash, gate.LifecycleHandoff.InheritedReleaseRunID); err != nil {
		t.Fatal(err)
	}
	if err := restarted.BeginPhase("production-soak", production.payload.RunID); err != nil {
		t.Fatal(err)
	}
	for path, want := range f.source {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("provisional successor rewrote source %s: %v", path, err)
		}
	}
	strict := *c.cfg
	strict.provisionalResume = nil
	if _, _, err := validateExactReleaseCampaignGateContext(t.Context(), &strict, c.stateDir, c.roles, gate); err == nil {
		t.Fatal("strict gate accepted failed provisional source")
	}
	if err := validateReleaseCampaignResult(c.cfg, verified); err == nil {
		t.Fatal("ordinary release verifier accepted original failure")
	}
	policyEvidence := ProductionPolicyEvidence{ReleaseRunID: gate.RunID, ReleaseResultHash: gate.ResultHash, ReleaseGate: gate, ProvisionalReleaseHandoffHash: gate.ProvisionalHandoffHash}
	if !releaseCampaignGatesEqual(productionPolicyReleaseGate(policyEvidence), gate) {
		t.Fatal("policy postcondition lost inherited/provisional gate provenance")
	}
}

func TestProvisionalProductionHandoffRejectsUnsafeOrChangedSource(t *testing.T) {
	for _, mode := range []string{"strict", "not-terminal", "core-ownership-failure", "forged-core-pass", "changed-observation", "changed-start-signature", "changed-handoff", "different-source", "no-writer"} {
		t.Run(mode, func(t *testing.T) {
			f := newProvisionalProductionTestFixture(t, func(observation *ScenarioObservation) {
				if mode == "forged-core-pass" {
					observation.NativeCustodyError = "retained signed custody mismatch"
				}
			})
			c := f.history.campaign
			switch mode {
			case "strict":
				c.cfg.provisionalResume = nil
			case "not-terminal":
				f.result.EndHead.Number = f.result.AcceptanceWindow.TerminalBlock - 1
			case "core-ownership-failure":
				for i := range f.result.Assertions {
					if f.result.Assertions[i].ID == "native_custody_hotkeys" {
						f.result.Assertions[i].Passed = false
						f.result.FailedAssertionCount++
					}
				}
			case "changed-observation":
				path := filepath.Join(f.runDir, "observations.jsonl")
				raw, _ := os.ReadFile(path)
				raw[0] = ' '
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			case "changed-start-signature":
				path := filepath.Join(f.runDir, scenarioCampaignStartFilename)
				var e ReleaseEvidenceEnvelope
				if err := decodeStrictJSONFile(path, &e); err != nil {
					t.Fatal(err)
				}
				e.Signature = "0x" + strings.Repeat("00", 65)
				if err := writePublicJSON(path, e); err != nil {
					t.Fatal(err)
				}
			case "changed-handoff":
				path := filepath.Join(f.runDir, scenarioLifecycleHandoffFilename)
				if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "different-source":
				c.cfg.provisionalProductionSourceRunID = "another-release-1.0"
			case "no-writer":
				c.journal = &Journal{}
			}
			if mode == "not-terminal" || mode == "core-ownership-failure" {
				var err error
				f.result.EvidenceHash, err = canonicalScenarioResultHash(f.result)
				if err != nil {
					t.Fatal(err)
				}
				if err := writePublicJSON(filepath.Join(f.runDir, "result.json"), f.result); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.prepare(t); err == nil {
				t.Fatal("unsafe source admitted")
			}
			if _, err := os.Stat(provisionalProductionHandoffPath(c.stateDir, f.result.RunID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed preflight published provisional authority")
			}
		})
	}
}

func TestProvisionalProductionHandoffRequiresRestoredFaults(t *testing.T) {
	for _, mode := range []string{"signed-active", "signed-pending", "result-active", "future-restoration", "active-recovery-ledger"} {
		t.Run(mode, func(t *testing.T) {
			f := newProvisionalProductionTestFixture(t)
			c := f.history.campaign
			boundary := f.history.attempt.payload.AcceptanceBoundary
			index := len(boundary.Faults) - 1
			signedFault, resultFault := &boundary.Faults[index], &f.result.Faults[index]
			clearRestoration := func(fault *ScenarioFaultRecord) {
				fault.Status, fault.RestoredBlock, fault.RestoredBlockHash = "active", 0, ""
				fault.RestoredProcesses = nil
			}
			switch mode {
			case "signed-active":
				clearRestoration(signedFault)
			case "signed-pending":
				clearRestoration(signedFault)
				signedFault.Status, signedFault.AppliedBlock, signedFault.AppliedBlockHash = "pending", 0, ""
				signedFault.Processes = nil
			case "result-active":
				clearRestoration(resultFault)
			case "future-restoration":
				signedFault.RestoredBlock, resultFault.RestoredBlock = boundary.LastObservationHead.Number+1, boundary.LastObservationHead.Number+1
			case "active-recovery-ledger":
				active := activeFaultFile{Schema: "urnetwork-sim-active-faults-v1", Faults: []scenarioFaultSpec{{ID: signedFault.ID, Kind: signedFault.Kind, Targets: signedFault.Targets}}, Processes: signedFault.Processes}
				if err := writePublicJSON(filepath.Join(c.stateDir, "active-faults.json"), active); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeScenarioCampaignAttempt(f.history.attempt); err != nil {
				t.Fatal(err)
			}
			f.result.EvidenceHash, _ = canonicalScenarioResultHash(f.result)
			if err := writePublicJSON(filepath.Join(f.runDir, "result.json"), f.result); err != nil {
				t.Fatal(err)
			}
			if err := writeScenarioFaultEvidence(f.runDir, f.result.Faults); err != nil {
				t.Fatal(err)
			}
			if _, err := f.prepare(t); err == nil || !strings.Contains(err.Error(), "fault") {
				t.Fatalf("unrestored release fault admitted: %v", err)
			}
			for _, path := range []string{provisionalProductionHandoffPath(c.stateDir, f.result.RunID), scenarioCampaignAttemptPath(c.stateDir, "production-soak")} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unrestored fault published production authority at %s", path)
				}
			}
		})
	}
}

func TestProvisionalProductionHandoffRetryPinsEveryFailure(t *testing.T) {
	f := newProvisionalProductionTestFixture(t)
	c := f.history.campaign
	gate, err := f.prepare(t)
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(provisionalProductionHandoffPath(c.stateDir, f.result.RunID))
	if err != nil {
		t.Fatal(err)
	}
	retry, err := f.prepare(t)
	if err != nil || !releaseCampaignGatesEqual(gate, retry) {
		t.Fatalf("exact retry: %v", err)
	}
	second, _ := os.ReadFile(provisionalProductionHandoffPath(c.stateDir, f.result.RunID))
	if !bytes.Equal(first, second) {
		t.Fatal("retry replaced the signed handoff")
	}
	var e ReleaseEvidenceEnvelope
	if err := json.Unmarshal(first, &e); err != nil {
		t.Fatal(err)
	}
	var payload provisionalProductionHandoff
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Provisional || payload.FinalAcceptance || len(payload.FailedAssertions) != f.result.FailedAssertionCount {
		t.Fatal("handoff omitted failure or nonacceptance provenance")
	}
	f.result.Assertions[0].Message += " substituted"
	f.result.EvidenceHash, _ = canonicalScenarioResultHash(f.result)
	if err := writePublicJSON(filepath.Join(f.runDir, "result.json"), f.result); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateExactReleaseCampaignGateContext(t.Context(), c.cfg, c.stateDir, c.roles, gate); err == nil {
		t.Fatal("signed handoff accepted rehashed source substitution")
	}
}

func TestProvisionalProductionHandoffCLIRequiresExplicitScope(t *testing.T) {
	base := []string{"scenario", "--name", "production-soak", "--apply", "--provisional-resume", "--plan-hash", "0x" + strings.Repeat("a1", 32), "--provisional-release-run-id", "20260924T181314.486418853Z-release-1.0"}
	if _, _, err := parseCLI(base); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--apply", "--provisional-resume"} {
		args := []string{}
		for _, arg := range base {
			if arg != flag {
				args = append(args, arg)
			}
		}
		if _, _, err := parseCLI(args); err == nil {
			t.Fatalf("accepted without %s", flag)
		}
	}
	options := cliOptions{Name: "release-1.0", Apply: true, ProvisionalResume: true, ProvisionalReleaseRunID: "20260924T181314.486418853Z-release-1.0"}
	if err := validateProvisionalProductionOptions("scenario", options); err == nil {
		t.Fatal("source override admitted outside production")
	}
}

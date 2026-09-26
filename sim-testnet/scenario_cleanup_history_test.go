// Signed request/removal/completion writes reproduce the non-adjacent reader
// regression without modifying a service, chain or retained campaign file.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cleanupHistoryFixture struct {
	cleanup       *lifecycleCleanupTestFixture
	result        *ScenarioResult
	startRaw      []byte
	checkpointRaw []byte
	history       []*ScenarioObservation
	owner         string
}

func newCleanupHistoryFixture(t *testing.T) *cleanupHistoryFixture {
	t.Helper()
	f := newLifecycleCleanupTestFixture(t)
	attempt := f.history.attempt
	cfg := f.history.campaign.cfg
	runDir := filepath.Join(attempt.stateDir, "runs", attempt.payload.RunID)
	for index := range f.records {
		for _, original := range attempt.payload.AcceptanceBoundary.Faults {
			if original.ID == f.records[index].ID {
				f.records[index].ArmedBlock, f.records[index].ArmedBlockHash = original.ArmedBlock, original.ArmedBlockHash
			}
		}
	}
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), f.current); err != nil {
		t.Fatal(err)
	}
	checkpoint := func() error {
		full := cloneScenarioFaultRecords(attempt.payload.AcceptanceBoundary.Faults)
		for index := range full {
			for _, record := range f.records {
				if record.ID == full[index].ID {
					full[index] = record
				}
			}
		}
		return attempt.updateAuthenticatedRuntime(runDir, full)
	}
	driver := &lifecycleCleanupTestDriver{}
	if err := advanceScenarioLifecycleCleanup(t.Context(), cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err != nil {
		t.Fatal(err)
	}
	f.nextObservation(t)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), f.current); err != nil {
		t.Fatal(err)
	}
	if err := advanceScenarioLifecycleCleanup(t.Context(), cfg, f.window, f.current, f.binding, f.records, driver, checkpoint); err != nil {
		t.Fatal(err)
	}
	if !faultsComplete(f.records) || len(driver.restored) != 2 {
		t.Fatal("synthetic two-filter cleanup did not complete")
	}
	history, _, _, _, _, err := attempt.loadAuthenticatedRuntimeForensics(runDir)
	if err != nil {
		t.Fatal(err)
	}
	startRaw, err := os.ReadFile(filepath.Join(runDir, scenarioCampaignStartFilename))
	if err != nil {
		t.Fatal(err)
	}
	checkpointRaw, err := os.ReadFile(attempt.path())
	if err != nil {
		t.Fatal(err)
	}
	boundary := attempt.payload.AcceptanceBoundary
	finalAcceptance := false
	result := &ScenarioResult{Schema: "urnetwork-sim-scenario-result-v1", Release: "1.0", RunID: attempt.payload.RunID, Name: "release-1.0", Result: "fail", Provisional: true, FinalAcceptance: &finalAcceptance,
		DeploymentID: cfg.Config.Deployment.DeploymentID, ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, Netuid: cfg.Netuid, GenesisHash: cfg.Public.Chain.GenesisHash,
		StartedAt: attempt.payload.StartedAt, CompletedAt: f.current.ObservedAt, CampaignStartHead: boundary.CampaignStartHead, CampaignStartEpoch: boundary.CampaignStartEpoch,
		AcceptanceWindow: &boundary.AcceptanceWindow, ScenarioDefinition: boundary.ScenarioDefinitionHash, AdversarialMatrix: boundary.AdversarialMatrixHash,
		Faults: cloneScenarioFaultRecords(boundary.Faults), LifecycleHandoff: f.binding,
		Adversaries: &AdversaryCampaignEvidence{StartedAt: boundary.AdversaryStartedAt, HappyPathStartedAt: boundary.AdversaryHappyPathStartedAt, StartedBeforeHappyPath: true, StoppedAfterHappyPath: true, Status: "stopped"},
	}
	return &cleanupHistoryFixture{cleanup: f, result: result, startRaw: startRaw, checkpointRaw: checkpointRaw, history: history, owner: attempt.roles.EVM["testnet-owner"].Address}
}

func (self *cleanupHistoryFixture) verify() error {
	return validateScenarioCampaignStartMarkerHistory(self.cleanup.history.campaign.cfg, self.result, "release-1.0", self.owner, self.startRaw, self.checkpointRaw, self.history)
}

// The old start-only reader fails although every actual adjacent checkpoint
// passed. The cumulative reader authenticates those completed endpoint bytes.
func TestScenarioCleanupHistoryAcceptsSignedMultiCheckpointCompletion(t *testing.T) {
	f := newCleanupHistoryFixture(t)
	if err := validateScenarioCampaignStartMarkerBytes(f.cleanup.history.campaign.cfg, f.result, "release-1.0", f.owner, f.startRaw); err == nil || !strings.Contains(err.Error(), "without its prior signed request") {
		t.Fatal("fixture did not reproduce original non-adjacent comparison", err)
	}
	if err := f.verify(); err != nil {
		t.Fatal("legitimate signed cleanup completion was refused", err)
	}
	if f.result.Result != "fail" || !f.result.Provisional || *f.result.FinalAcceptance {
		t.Fatal("diagnostic history granted acceptance")
	}
	raw, err := os.ReadFile(f.cleanup.history.attempt.path())
	if err != nil || string(raw) != string(f.checkpointRaw) {
		t.Fatal("history read mutated signed source", err)
	}
}

// A request-only or unsigned endpoint cannot authorize the completed result;
// the direct live writer must still refuse jumping across its required write.
func TestScenarioCleanupHistoryRejectsMissingCheckpointAndLateAuthority(t *testing.T) {
	f := newCleanupHistoryFixture(t)
	original := f.checkpointRaw
	f.checkpointRaw = f.startRaw
	if err := f.verify(); err == nil {
		t.Fatal("original pending checkpoint authorized completed cleanup")
	}
	f.checkpointRaw = append([]byte(nil), original...)
	f.checkpointRaw[len(f.checkpointRaw)/2] ^= 1
	if err := f.verify(); err == nil {
		t.Fatal("tampered endpoint authorized completed cleanup")
	}
	f.checkpointRaw = original
	before := cloneScenarioFaultRecords(f.result.Faults)
	for index := range before {
		before[index].LifecycleCleanup = nil
	}
	if err := validateScenarioFaultProgress(before, f.result.Faults); err == nil {
		t.Fatal("live adjacent ordering was weakened")
	}
	if err := validateScenarioFaultProgressWithMode(before, f.result.Faults, false); err == nil {
		t.Fatal("cumulative reader invented authority after an already restored checkpoint")
	}
	if err := f.verify(); err != nil {
		t.Fatal("negative controls changed original authenticated proof", err)
	}
}

// Exact request/completion hashes and handoff bytes cannot be borrowed from
// another observation, another fault, or an unsigned result-only annotation.
func TestScenarioCleanupHistoryRejectsChangedObservationAndResultProof(t *testing.T) {
	f := newCleanupHistoryFixture(t)
	last := f.history[len(f.history)-1]
	f.history = f.history[:len(f.history)-1]
	if err := f.verify(); err == nil {
		t.Fatal("missing completion observation passed")
	}
	f.history = append(f.history, last)
	oldTime := last.ObservedAt
	last.ObservedAt = "2026-09-04T12:00:00Z"
	if err := f.verify(); err == nil {
		t.Fatal("rewritten signed observation passed")
	}
	last.ObservedAt = oldTime
	for i := range f.result.Faults {
		proof := f.result.Faults[i].LifecycleCleanup
		if proof == nil {
			continue
		}
		old := proof.RequestedObservationHash
		proof.RequestedObservationHash = "0x" + strings.Repeat("a7", 32)
		if err := f.verify(); err == nil {
			t.Fatal("result replaced exact signed request")
		}
		proof.RequestedObservationHash = old
		break
	}
	oldHash := f.result.LifecycleHandoff.ContentHash
	f.result.LifecycleHandoff.ContentHash = "sha256:" + strings.Repeat("a8", 32)
	if err := f.verify(); err == nil {
		t.Fatal("result replaced exact handoff bytes")
	}
	f.result.LifecycleHandoff.ContentHash = oldHash
	if err := f.verify(); err != nil {
		t.Fatal("negative controls changed original authenticated proof", err)
	}
}

// A valid owner signature does not permit a later checkpoint to switch the
// original plan or process session while retaining the same cleanup objects.
func TestScenarioCleanupHistoryRejectsResignedForeignIdentity(t *testing.T) {
	f := newCleanupHistoryFixture(t)
	original := append([]byte(nil), f.checkpointRaw...)
	for _, field := range []string{"plan", "session"} {
		var envelope ReleaseEvidenceEnvelope
		if err := decodeStrictJSONBytes(original, &envelope); err != nil {
			t.Fatal(err)
		}
		var payload scenarioCampaignAttemptPayload
		if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if field == "plan" {
			payload.PlanHash = "0x" + strings.Repeat("b7", 32)
		} else {
			payload.AcceptanceBoundary.ProcessSessionID = "0x" + strings.Repeat("b8", 32)
		}
		attempt := f.cleanup.history.attempt
		signed, err := signEvidence(attempt.cfg, scenarioCampaignAttemptEvidenceKind, payload.RunID, payload, attempt.roles.EVM["testnet-owner"])
		if err != nil {
			t.Fatal(err)
		}
		f.checkpointRaw, err = json.Marshal(signed)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.verify(); err == nil || !strings.Contains(err.Error(), "replaced its signed start") {
			t.Fatalf("re-signed %s replacement was not rejected by exact start identity: %v", field, err)
		}
	}
	f.checkpointRaw = original
	if err := f.verify(); err != nil {
		t.Fatal("negative controls changed original authenticated proof", err)
	}
}

// Recovery reads the same complete signed prefix. Its historical endpoint
// comparison must not reuse the adjacent writer rule or discard raw sources.
func TestScenarioCleanupHistoryRecoveryRetainsExactSignedSources(t *testing.T) {
	f := newCleanupHistoryFixture(t)
	prior := f.cleanup.history.attempt
	runDir := filepath.Join(prior.stateDir, "runs", prior.payload.RunID)
	f.result.Assertions = []AssertionRecord{{ID: "fleet_lifecycle_complete", Passed: false, Message: "approved bypass is not strict mutation evidence"}}
	f.result.AssertionCount, f.result.FailedAssertionCount = 1, 1
	var err error
	f.result.EvidenceHash, err = canonicalScenarioResultHash(f.result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), f.result); err != nil {
		t.Fatal(err)
	}
	completed, err := time.Parse(time.RFC3339Nano, f.result.CompletedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := prior.invalidateAcceptance("execution-exited-before-completion", completed.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	prior.historicalEvidence = true
	_, relative, err := scenarioCampaignRecoveryPredecessorIdentity(prior)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(prior.path())
	if err != nil {
		t.Fatal(err)
	}
	recovery, _, err := readScenarioCampaignRecoverySources(prior, prior, relative, raw)
	if err != nil {
		t.Fatal("completed cleanup prevented exact failed-source recovery", err)
	}
	if recovery.PriorAttemptSha256 != bytesSHA256(raw) || recovery.PriorCampaignStartSha256 != bytesSHA256(f.startRaw) || recovery.PriorRunID != f.result.RunID {
		t.Fatal("recovery substituted original failed source evidence", recovery)
	}
	if f.result.Result != "fail" || *f.result.FinalAcceptance {
		t.Fatal("recovery changed strict acceptance")
	}
}

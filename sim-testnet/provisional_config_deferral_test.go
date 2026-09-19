package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Reproduce the repair -> reserve-read -> changed-render sequence using real
// archived approval, journal and receipt authentication. Rendering remains
// pending while the fresh reserve observation completes beside live inputs.
func TestProvisionalSetupDefersRenderingAfterReserveReconciliation(t *testing.T) {
	fixture, repairs := provisionalSetupRepairFixture(t)
	self := fixture.executor
	sourceAction := Action{ID: "config.render", Kind: "local", Target: self.plan.DeploymentID,
		Parameters: map[string]string{"deployment_manifest_hash": "0x" + strings.Repeat("31", 32)}}
	var err error
	sourceAction.IntentHash, err = actionIntentHash(sourceAction)
	if err != nil {
		t.Fatal(err)
	}
	source := *fixture.source
	source.Actions = append(append([]Action(nil), source.Actions...), sourceAction)
	source.PlanHash, err = source.hash()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(self.stateDir, "plans", stringsTrim0x(source.PlanHash)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := RuntimeConfigManifest{Schema: runtimeConfigManifestSchema, DeploymentID: source.DeploymentID,
		ConfigHash: source.ConfigHash, PolicyHash: source.PolicyHash, Files: []RuntimeConfigFile{}}
	manifest.ManifestHash, err = runtimeConfigManifestHash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(runtimeConfigManifestPath(self.stateDir), manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	record := *fixture.records[0]
	record.PlanHash, record.ActionID, record.IntentHash = source.PlanHash, sourceAction.ID, sourceAction.IntentHash
	record.Observed = map[string]any{"runtime_config_manifest_hash": "0x" + strings.Repeat("42", 32)}
	record.IndependentObserved = map[string]any{"runtime_config_manifest_hash": "0x" + strings.Repeat("42", 32)}
	path, hash, err := self.persistActionPostcondition(&record)
	if err != nil {
		t.Fatal(err)
	}
	if err := self.journal.Append(JournalEntry{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash,
		ActionID: sourceAction.ID, IntentHash: sourceAction.IntentHash, Stage: StageVerified,
		PostconditionPath: path, PostconditionHash: hash}); err != nil {
		t.Fatal(err)
	}
	reserve := Action{ID: "validator.reserve-majority", Kind: "substrate-read", DependsOn: []string{repairs[1].ID}}
	reserve.IntentHash, err = actionIntentHash(reserve)
	if err != nil {
		t.Fatal(err)
	}
	currentRender := sourceAction
	currentRender.DependsOn = []string{reserve.ID}
	currentRender.IntentHash, err = actionIntentHash(currentRender)
	if err != nil {
		t.Fatal(err)
	}
	self.plan.Actions = append(self.plan.Actions[:4:4], reserve, currentRender, Action{ID: "topology.launch", Kind: "local"})
	self.plan.PriorPlanHashes = append(self.plan.PriorPlanHashes, source.PlanHash)
	self.plan.PlanHash, err = self.plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.provisionalResume.Record.PlanHash = self.plan.PlanHash
	self.cfg.provisionalResume.RecordPath = filepath.Join(self.stateDir, "provisional-resumes", "synthetic-invocation", "provenance.json")
	calls := []string{}
	for range 2 {
		_, err := self.reconcileProvisionalSetupPrefix(t.Context(), self.plan, func(_ context.Context, action Action) error {
			if action.ID == "config.render" {
				t.Fatal("live adoption dispatched the renderer")
			}
			calls = append(calls, action.ID)
			persistProvisionalRepairTestReceipt(t, fixture, action)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(calls, []string{repairs[0].ID, repairs[1].ID, reserve.ID}) {
		t.Fatal("recovery repeated completed work or skipped the reserve read", calls)
	}
	if _, verified := self.verifiedActionEntry(currentRender); verified {
		t.Fatal("deferral falsely verified the current render intent")
	}
	var deferred provisionalConfigDeferral
	if err := readJSONFile(filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "config-render-deferred.json"), &deferred); err != nil {
		t.Fatal(err)
	}
	if !deferred.Provisional || deferred.FinalAcceptance || deferred.CurrentActionVerified || deferred.SourceReceipt.PlanHash != source.PlanHash ||
		deferred.SourceManifestHash == deferred.ObservedManifestHash || deferred.ObservedManifestHash != manifest.ManifestHash || deferred.Action.IntentHash != currentRender.IntentHash {
		t.Fatal("deferral omitted retained/current provenance or claimed acceptance", deferred)
	}
	after, err := os.ReadFile(runtimeConfigManifestPath(self.stateDir))
	if err != nil || string(after) != string(manifestRaw) {
		t.Fatal("deferral rewrote retained runtime inputs", err)
	}
	if _, err := self.authenticateProvisionalSetupPrefix(t.Context(), self.plan, self.journal.Entries, readValidatorEvidenceHistoricalPlan); err == nil {
		t.Fatal("strict prefix authentication accepted the deferred render")
	}
	manifest.ManifestHash = "0x" + strings.Repeat("99", 32)
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(runtimeConfigManifestPath(self.stateDir), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := self.reconcileProvisionalSetupPrefix(t.Context(), self.plan, func(context.Context, Action) error { t.Fatal("corrupt manifest dispatched work"); return nil }); err == nil {
		t.Fatal("deferral accepted a malformed retained manifest")
	}
}

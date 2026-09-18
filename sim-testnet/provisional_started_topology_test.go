//go:build linux

package main

// Fresh provisional startup uses real generation, proof and journal readers.
// The test process owns the synthetic supervisor identity; nothing is signaled.

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The zero-cost synthetic tournament barrier exercises actual executor and
// receipt persistence without modeling another registration protocol here.
func startedProvisionalTopologyTest(t *testing.T) (*Executor, processLogGateFixture, map[string]int) {
	t.Helper()
	rpc := newRuntimeEvidenceActivationRpcV2TestFixture(t)
	// Finalized postconditions require the real reviewed metadata artifact.
	// The tiny activation fixture cannot satisfy that stricter entrypoint.
	encoded, err := os.ReadFile("../miner/testdata/runtime461-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	fixture := newProcessLogGateFixture(t, "", "")
	if err := os.Chmod(fixture.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	executor := rpc.executor
	executor.stateDir, executor.deployer = fixture.dir, executor.keeper
	executor.cfg.OperationalRPCMode = rpcModePublicOverride
	executor.cfg.Release.Runtime = testResolvedConfig(t).Release.Runtime
	executor.cfg.Public.Chain.ExpectedRuntimeSpec = reviewedRuntimeSpecVersion
	executor.cfg.Public.Chain.ExpectedTransactionVersion = reviewedRuntimeTransactionVersion
	executor.cfg.Public.Chain.ExpectedStateVersion = reviewedRuntimeStateVersion
	executor.substrate.cfg = executor.cfg
	rpc.stateLock.Lock()
	rpc.metadataHex = "0x" + hex.EncodeToString(metadata)
	rpc.nativeRuntime = currentReleaseRuntimeArtifact(executor.cfg)
	rpc.stateLock.Unlock()
	plan := *executor.plan
	plan.Actions = []Action{
		{ID: "topology.launch", Kind: "local"},
		{ID: "churn.tournament-complete", Kind: "budget-reserve", DependsOn: []string{"topology.launch"}},
	}
	for index := range plan.Actions {
		var err error
		plan.Actions[index].IntentHash, err = actionIntentHash(plan.Actions[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	executor.plan = &plan
	executor.journal, err = OpenJournal(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { executor.journal.Close() })
	provisional, _, _, options := provisionalResumeTestContext(t)
	executor.cfg.provisionalResume = provisional.provisionalResume
	options.PlanHash = plan.PlanHash
	if err := prepareProvisionalResume(t.Context(), executor.cfg, fixture.dir, "resume", options, &plan); err != nil {
		t.Fatal(err)
	}
	fixture.manifest.DeploymentID = plan.DeploymentID
	fixture.manifest.BinaryHash, err = fileSHA256("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	fixture.supervisor.ManifestHash, err = canonicalHashHex(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"supervisor.json": fixture.manifest, "supervisor.state.json": fixture.supervisor} {
		if err := writePublicJSON(filepath.Join(fixture.dir, name), value); err != nil {
			t.Fatal(err)
		}
	}
	baseline, err := releaseTopologyProofCounts(executor.cfg, fixture.dir)
	if err != nil || len(baseline) != 4 {
		t.Fatal("synthetic validator/operator proof domains unavailable", err)
	}
	return executor, fixture, baseline
}

// Strict mode still needs fresh proofs. Provisional startup records zero
// observations, executes the tournament boundary, and refreshes scenario handoff.
func TestStartedProvisionalTopologyAdoptsWithoutInventingFreshProofs(t *testing.T) {
	executor, fixture, baseline := startedProvisionalTopologyTest(t)
	state := executor.cfg.provisionalResume
	executor.cfg.provisionalResume = nil
	if adopted, err := adoptStartedProvisionalTopology(t.Context(), executor.cfg, fixture.dir, executor.plan, executor.roles, executor, fixture.supervisor, baseline); adopted || err != nil || len(executor.journal.Entries()) != 0 {
		t.Fatal("strict launch was provisionally adopted", adopted, err)
	}
	ready, err := releaseTopologyReady(fixture.supervisor, fixture.supervisor.ManifestHash, fixture.manifest.Specs, fixture.supervisor.SupervisorPID, fixture.supervisor.SupervisorStartTimeTicks, baseline, baseline)
	if err != nil || ready {
		t.Fatal("strict startup accepted missing fresh proofs", ready, err)
	}
	executor.cfg.provisionalResume = state
	oldPointer := filepath.Join(fixture.dir, "provisional-resumes", "live-topology.json")
	if err := atomicWrite(oldPointer, []byte("previous generation pointer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.ReadFile(filepath.Join(fixture.dir, "supervisor.json"))
	if err != nil {
		t.Fatal(err)
	}
	if adopted, err := adoptStartedProvisionalTopology(t.Context(), executor.cfg, fixture.dir, executor.plan, executor.roles, executor, fixture.supervisor, baseline); !adopted || err != nil {
		t.Fatal("healthy provisional generation could not hand off without proofs", adopted, err)
	}
	var adoption provisionalLiveTopology
	if err := readJSONFile(oldPointer, &adoption); err != nil {
		t.Fatal(err)
	}
	if !adoption.Provisional || adoption.FinalAcceptance || !adoption.FreshProofStartupWaived || adoption.ObservedProofsVerified || len(adoption.VerifiedProofCounts) != 0 || adoption.CompletedAt == "" || adoption.PlanHash != executor.plan.PlanHash || !reflect.DeepEqual(adoption.ProofBaseline, baseline) || !reflect.DeepEqual(adoption.ObservedProofCounts, baseline) {
		t.Fatal("provisional handoff changed generation, proof facts or final acceptance")
	}
	if err := provisionalAdoptionGeneration(&adoption, fixture.supervisor); err != nil {
		t.Fatal(err)
	}
	if provisionalProofsAdvanced(adoption.ProofBaseline, adoption.ObservedProofCounts) {
		t.Fatal("missing proofs were promoted into progress")
	}
	for _, action := range executor.plan.Actions {
		entry, ok := executor.verifiedActionEntry(action)
		if !ok {
			t.Fatalf("actual approved executor skipped %s", action.ID)
		}
		record, err := executor.readPersistedPostcondition(entry)
		if err != nil {
			t.Fatal(err)
		}
		if action.ID == "topology.launch" && (record.Observed["readiness_scope"] != "live-processes-and-non-provider-health" || record.Observed["final_acceptance"] != false) {
			t.Fatal("topology receipt claimed full semantic readiness")
		}
	}
	if gate, err := loadProvisionalOrStrictProcessLogGate(executor.cfg, fixture.dir); err != nil || gate == nil || gate.path != adoption.ProcessLogGatePath {
		t.Fatal("next scenario cannot adopt the current generation directly", err)
	}
	manifestAfter, err := os.ReadFile(filepath.Join(fixture.dir, "supervisor.json"))
	if err != nil || !bytes.Equal(manifestBefore, manifestAfter) {
		t.Fatal("adoption changed retained launch inputs", err)
	}
	for _, path := range releaseTopologyProofPaths(executor.cfg, fixture.dir) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("adoption manufactured a missing proof file", err)
		}
	}
}

// A controlled supervisor replacement must publish the new adoption before a
// scenario opens its gate. The prior cursor is carried across the append-only
// logs so shutdown/startup failures cannot disappear between generations.
func TestProvisionalScenarioAdoptsChangedGenerationWithoutLogGap(t *testing.T) {
	executor, fixture, baseline := startedProvisionalTopologyTest(t)
	if adopted, err := adoptStartedProvisionalTopology(t.Context(), executor.cfg, fixture.dir, executor.plan, executor.roles, executor, fixture.supervisor, baseline); !adopted || err != nil {
		t.Fatal("initial provisional generation was not adopted", adopted, err)
	}
	var prior provisionalLiveTopology
	pointer := filepath.Join(fixture.dir, "provisional-resumes", "live-topology.json")
	if err := readJSONFile(pointer, &prior); err != nil {
		t.Fatal(err)
	}
	priorGate, err := os.ReadFile(prior.ProcessLogGatePath)
	if err != nil {
		t.Fatal(err)
	}
	var priorState processLogGateState
	if err := json.Unmarshal(priorGate, &priorState); err != nil {
		t.Fatal(err)
	}
	stderrRelative, err := processLogRelativePath(fixture.dir, fixture.stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	var priorStderr processLogCursor
	for _, cursor := range priorState.Cursors {
		if cursor.Path == stderrRelative {
			priorStderr = cursor
		}
	}
	if priorStderr.Path == "" {
		t.Fatal("prior gate omitted the fixture stderr cursor")
	}
	appendProcessLog(t, fixture.stderrPath, "panic: retained generation rollover tail\n")

	fixture.manifest.ProviderStartupWaveSize++
	fixture.supervisor.ManifestHash, err = canonicalHashHex(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"supervisor.json": fixture.manifest, "supervisor.state.json": fixture.supervisor} {
		if err := writePublicJSON(filepath.Join(fixture.dir, name), value); err != nil {
			t.Fatal(err)
		}
	}
	current, err := prepareProvisionalLiveTopology(executor.cfg, fixture.dir, "scenario")
	if err != nil || current == nil {
		t.Fatal("changed live generation was not prepared", err)
	}
	if ready, err := provisionalLiveTopologyAdoptionCurrent(executor.cfg, fixture.dir, current); err != nil || ready {
		t.Fatal("prior generation was reused by the scenario", ready, err)
	}
	if err := adoptProvisionalLiveTopology(t.Context(), executor.cfg, fixture.dir, executor.plan, executor.roles, executor, current); err != nil {
		t.Fatal("changed live generation was not adopted", err)
	}
	if ready, err := provisionalLiveTopologyAdoptionCurrent(executor.cfg, fixture.dir, current); err != nil || !ready {
		t.Fatal("current generation was not published before scenario use", ready, err)
	}
	retainedPriorGate, err := os.ReadFile(prior.ProcessLogGatePath)
	if err != nil || !bytes.Equal(retainedPriorGate, priorGate) {
		t.Fatal("generation rollover changed its prior gate", err)
	}
	var adopted provisionalLiveTopology
	if err := readJSONFile(pointer, &adopted); err != nil || adopted.ProcessLogGatePath == prior.ProcessLogGatePath {
		t.Fatal("generation rollover did not publish a distinct gate", err)
	}
	var adoptedState processLogGateState
	if err := readJSONFile(adopted.ProcessLogGatePath, &adoptedState); err != nil {
		t.Fatal(err)
	}
	if len(adoptedState.Findings) != 1 || adoptedState.Findings[0].Class != "panic" || adoptedState.Findings[0].FirstOffset != priorStderr.Offset {
		t.Fatalf("rollover gate lost the exact prior tail: %+v", adoptedState.Findings)
	}
	gate, err := loadProvisionalOrStrictProcessLogGate(executor.cfg, fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := gate.Scan(false)
	if err != nil || len(result.Findings) != 1 || result.Findings[0].Blocking || result.Findings[0].Disposition != "provisional-observation" {
		t.Fatalf("scenario gate did not retain the rollover observation: %+v, %v", result.Findings, err)
	}
}

// A real approved tournament dependency failure must abort adoption after
// topology execution, without publishing a completed generation pointer.
func TestStartedProvisionalTopologyPropagatesTournamentFailure(t *testing.T) {
	executor, fixture, baseline := startedProvisionalTopologyTest(t)
	executor.plan.Actions[1].DependsOn = []string{"missing-approved-tournament-write"}
	var err error
	executor.plan.Actions[1].IntentHash, err = actionIntentHash(executor.plan.Actions[1])
	if err != nil {
		t.Fatal(err)
	}
	executor.plan.PlanHash, err = executor.plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	executor.cfg.provisionalResume.Record.PlanHash = executor.plan.PlanHash
	if adopted, err := adoptStartedProvisionalTopology(t.Context(), executor.cfg, fixture.dir, executor.plan, executor.roles, executor, fixture.supervisor, baseline); !adopted || err == nil || !strings.Contains(err.Error(), "missing-approved-tournament-write") {
		t.Fatal("fresh proof waiver skipped the approved tournament executor", adopted, err)
	}
	if _, verified := executor.verifiedActionEntry(executor.plan.Actions[0]); !verified {
		t.Fatal("failure occurred before the actual tournament boundary")
	}
	if _, verified := executor.verifiedActionEntry(executor.plan.Actions[1]); verified {
		t.Fatal("failed tournament was recorded as successful")
	}
	if _, err := os.Stat(filepath.Join(fixture.dir, "provisional-resumes", "live-topology.json")); !os.IsNotExist(err) {
		t.Fatal("failed adoption published a completed handoff", err)
	}
	var adoption provisionalLiveTopology
	if err := readJSONFile(filepath.Join(filepath.Dir(executor.cfg.provisionalResume.RecordPath), "live-topology.json"), &adoption); err != nil || adoption.CompletedAt != "" || adoption.FinalAcceptance {
		t.Fatal("failed tournament lost its incomplete provisional record", err)
	}
}

// Readiness waivers cannot hide durable malformed proofs or a different
// process generation. Both fail before any approved action is dispatched.
func TestStartedProvisionalTopologyRejectsCorruptionAndGenerationChange(t *testing.T) {
	executor, fixture, baseline := startedProvisionalTopologyTest(t)
	started := fixture.supervisor
	started.SupervisorStartTimeTicks++
	if adopted, err := adoptStartedProvisionalTopology(t.Context(), executor.cfg, fixture.dir, executor.plan, executor.roles, executor, started, baseline); !adopted || err == nil || !strings.Contains(err.Error(), "generation changed") {
		t.Fatal("fresh startup adopted another process generation", adopted, err)
	}
	path := releaseTopologyProofPaths(executor.cfg, fixture.dir)["validator-1/no-1"]
	if err := atomicWrite(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if adopted, err := adoptStartedProvisionalTopology(t.Context(), executor.cfg, fixture.dir, executor.plan, executor.roles, executor, fixture.supervisor, baseline); !adopted || err == nil || !strings.Contains(err.Error(), "proof line 1 is malformed") {
		t.Fatal("provisional startup hid durable proof corruption", adopted, err)
	}
	if len(executor.journal.Entries()) != 0 {
		t.Fatal("invalid startup reached approved action dispatch")
	}
	raw, err := json.Marshal(fixture.supervisor)
	if err != nil {
		t.Fatal(err)
	}
	var current SupervisorState
	if err := readJSONFile(filepath.Join(fixture.dir, "supervisor.state.json"), &current); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(current)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("refused handoff rewrote its supervisor", err)
	}
}

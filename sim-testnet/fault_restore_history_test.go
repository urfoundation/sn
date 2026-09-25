// These fixtures preserve signed retry checkpoints and terminal evidence bytes;
// accepting a known historical field must not weaken signature or state checks.
package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestScenarioCampaignRecoveryRetainsRestoreRetryHistory(t *testing.T) {
	fixture := newCampaignSuccessionFixture(t)
	_, _ = bindCampaignRecoveryFixture(t, fixture)
	prior, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	runDir := bindFailedRecoveryGeneration(t, fixture, prior, 8)
	index := -1
	for i, record := range prior.payload.AcceptanceBoundary.Faults {
		if record.Kind == "process-restart" {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("fixture has no process restart")
	}
	record := &prior.payload.AcceptanceBoundary.Faults[index]
	record.Status, record.AppliedBlock, record.AppliedBlockHash = "active", record.TriggerBlock, "0x"+strings.Repeat("a1", 32)
	record.RestoreStartedBlock, record.RestoreStartedBlockHash, record.RestorePendingRounds = record.RestoreBlock, "0x"+strings.Repeat("b2", 32), 2
	record.Error = "synthetic replacement readiness is pending"
	for i, target := range record.Targets {
		record.Processes = append(record.Processes, FaultProcessEvidence{ID: target, Role: "synthetic-worker", Identity: "synthetic-instance", PID: 100 + i})
	}
	if err := writeScenarioCampaignAttempt(prior); err != nil {
		t.Fatal(err)
	}
	readPrior, signedRaw, err := readScenarioCampaignAttemptAt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", prior.path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(readPrior.payload.AcceptanceBoundary.Faults[index], *record) {
		t.Fatal("signed retry checkpoint lost historical fields")
	}
	result, _, err := readScenarioCampaignRecoveryResult(fixture.cfg, fixture.stateDir, prior)
	if err != nil {
		t.Fatal(err)
	}
	result.Faults = cloneScenarioFaultRecords(prior.payload.AcceptanceBoundary.Faults)
	terminal := &result.Faults[index]
	terminal.Status, terminal.Error = "restored", ""
	terminal.RestoredBlock, terminal.RestoredBlockHash = terminal.RestoreStartedBlock+1, "0x"+strings.Repeat("c3", 32)
	terminal.RestoredProcesses = append([]FaultProcessEvidence(nil), terminal.Processes...)
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(runDir, "result.json"), result); err != nil {
		t.Fatal(err)
	}
	if err := writeScenarioFaultEvidence(runDir, result.Faults); err != nil {
		t.Fatal(err)
	}
	readResult, resultRaw, err := readScenarioCampaignRecoveryResult(fixture.cfg, fixture.stateDir, prior)
	if err != nil || !reflect.DeepEqual(readResult, result) {
		t.Fatalf("terminal restore history failed exact hash replay: %v", err)
	}
	faultPath := filepath.Join(runDir, "faults.json")
	faultRaw, err := os.ReadFile(faultPath)
	if err != nil {
		t.Fatal(err)
	}
	next, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(2*time.Hour), fixture.journal)
	if err != nil {
		t.Fatal(err)
	}
	if next.payload.Recovery == nil || next.payload.Recovery.PriorRunID != prior.payload.RunID || next.payload.Recovery.PriorAttemptSha256 != bytesSHA256(signedRaw) || next.payload.Recovery.PriorResultSha256 != bytesSHA256(resultRaw) || next.payload.AcceptanceBoundary != nil {
		t.Fatal("successor failed to pin exact prior restore evidence")
	}
	for _, original := range []struct {
		path string
		raw  []byte
	}{{path: prior.path(), raw: signedRaw}, {path: filepath.Join(runDir, "result.json"), raw: resultRaw}, {path: faultPath, raw: faultRaw}} {
		raw, err := os.ReadFile(original.path)
		if err != nil || !bytes.Equal(raw, original.raw) {
			t.Fatalf("recovery changed historical evidence %s: %v", original.path, err)
		}
	}
	if _, err := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0"); err != nil {
		t.Fatal(err)
	}

	// Editing recognized metadata still invalidates the owner signature.
	altered := bytes.Replace(signedRaw, []byte(`"restore_pending_rounds": 2`), []byte(`"restore_pending_rounds": 3`), 1)
	if bytes.Equal(altered, signedRaw) {
		t.Fatal("fixture did not contain the expected retry count")
	}
	if err := os.WriteFile(prior.path(), altered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readScenarioCampaignAttemptAt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", prior.path()); err == nil {
		t.Fatal("unsigned restore retry substitution was accepted")
	}
}

func TestScenarioFaultRestoreHistoryWireCompatibility(t *testing.T) {
	legacy := `{"id":"synthetic-restart","kind":"process-restart","targets":["synthetic-worker"],"trigger_block":10,"restore_block":20,"status":"pending"}`
	historical := strings.TrimSuffix(legacy, "}") + `,"restore_started_block":20,"restore_started_block_hash":"0x` + strings.Repeat("ab", 32) + `","restore_pending_rounds":2}`
	for _, raw := range []string{legacy, historical} {
		var record ScenarioFaultRecord
		if err := decodeStrictJSONBytes([]byte(raw), &record); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(record)
		if err != nil || string(encoded) != raw {
			t.Fatalf("historical wire representation changed: %v\ngot %s\nwant %s", err, encoded, raw)
		}
	}
	var record ScenarioFaultRecord
	unknown := strings.TrimSuffix(historical, "}") + `,"restore_unknown_boundary":1}`
	if err := decodeStrictJSONBytes([]byte(unknown), &record); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown historical fields were silently discarded: %v", err)
	}
}

func TestScenarioFaultRestoreHistoryRejectsMalformedStateAndProgress(t *testing.T) {
	record := ScenarioFaultRecord{
		ID: "synthetic-restart", Kind: "process-restart", Targets: []string{"synthetic-worker"},
		TriggerBlock: 10, RestoreBlock: 20, AppliedBlock: 11, AppliedBlockHash: "0x" + strings.Repeat("a1", 32),
		RestoreStartedBlock: 21, RestoreStartedBlockHash: "0x" + strings.Repeat("b2", 32), RestorePendingRounds: 2,
		Processes: []FaultProcessEvidence{{ID: "synthetic-worker", Role: "worker", Identity: "synthetic-instance", PID: 100}},
		Status:    "active", Error: "synthetic readiness is pending",
	}
	window := &ScenarioAcceptanceWindow{StartBlock: 10}
	for _, kind := range []string{"process-restart", "process-pause", "container-restart", "validator-view-filter"} {
		valid := record
		valid.Kind = kind
		if err := validateScenarioCampaignFaultState(window, valid); err != nil {
			t.Fatalf("supported retry kind %s rejected: %v", kind, err)
		}
	}
	for _, mutation := range []struct {
		name   string
		change func(*ScenarioFaultRecord)
	}{
		{name: "unsupported kind", change: func(r *ScenarioFaultRecord) { r.Kind = "unknown-control" }},
		{name: "pending state", change: func(r *ScenarioFaultRecord) { r.Status = "pending" }},
		{name: "signed failed state", change: func(r *ScenarioFaultRecord) { r.Status = "failed" }},
		{name: "early restore", change: func(r *ScenarioFaultRecord) { r.RestoreStartedBlock-- }},
		{name: "invalid hash", change: func(r *ScenarioFaultRecord) { r.RestoreStartedBlockHash = "bad" }},
		{name: "missing count", change: func(r *ScenarioFaultRecord) { r.RestorePendingRounds = 0 }},
		{name: "missing boundary", change: func(r *ScenarioFaultRecord) { r.RestoreStartedBlock = 0 }},
		{name: "missing reason", change: func(r *ScenarioFaultRecord) { r.Error = "" }},
		{name: "invalid schedule", change: func(r *ScenarioFaultRecord) { r.RestoreBlock = 9 }},
		{name: "overflow", change: func(r *ScenarioFaultRecord) { r.AppliedBlock = math.MaxUint64 }},
		{name: "conditional early boundary", change: func(r *ScenarioFaultRecord) { r.RestoreCondition = "native-application"; r.MinimumDurationBlocks = 11 }},
		{name: "restored before retry", change: func(r *ScenarioFaultRecord) {
			r.Status, r.Error, r.RestoredBlock, r.RestoredBlockHash = "restored", "", 20, "0x"+strings.Repeat("c3", 32)
		}},
	} {
		invalid := record
		mutation.change(&invalid)
		if err := validateScenarioCampaignFaultState(window, invalid); err == nil {
			t.Errorf("accepted malformed restore state: %s", mutation.name)
		}
	}
	for _, mutation := range []struct {
		name   string
		change func(*ScenarioFaultRecord)
	}{
		{name: "counter rollback", change: func(r *ScenarioFaultRecord) { r.RestorePendingRounds-- }},
		{name: "boundary substitution", change: func(r *ScenarioFaultRecord) { r.RestoreStartedBlock++ }},
		{name: "hash substitution", change: func(r *ScenarioFaultRecord) { r.RestoreStartedBlockHash = "0x" + strings.Repeat("c3", 32) }},
	} {
		after := record
		mutation.change(&after)
		if err := validateScenarioFaultProgress([]ScenarioFaultRecord{record}, []ScenarioFaultRecord{after}); err == nil {
			t.Errorf("accepted restore history rewrite: %s", mutation.name)
		}
	}
	after := record
	after.RestorePendingRounds++
	if err := validateScenarioFaultProgress([]ScenarioFaultRecord{record}, []ScenarioFaultRecord{after}); err != nil {
		t.Fatal(err)
	}
	record.Status, after.Status = "restored", "restored"
	if err := validateScenarioFaultProgress([]ScenarioFaultRecord{record}, []ScenarioFaultRecord{after}); err == nil {
		t.Fatal("completed restore gained another retry")
	}
}

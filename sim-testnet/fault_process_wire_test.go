// Historical process generations remain signed evidence, not new permission to
// signal a process. Exercise the real checkpoint reader and adjacent ledgers.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Build a real signed checkpoint, then use the historical wire spelling rather
// than the new field name so the old reader reproduces the original refusal.
func faultProcessWireAttempt(t *testing.T) (*scenarioCampaignAttempt, string, int, []byte) {
	t.Helper()
	cfg := testResolvedConfig(t)
	attempt, runDir, window, faults := bindCampaignAttemptBoundaryFixture(t, cfg, t.TempDir())
	index := -1
	for i, record := range faults {
		if record.Kind == "process-restart" && !record.PreAcceptance && record.ActivationCondition == "" {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("fixture has no ordinary process restart")
	}
	record := &faults[index]
	record.Status, record.AppliedBlock, record.RestoredBlock = "restored", record.TriggerBlock, record.RestoreBlock
	record.AppliedBlockHash, record.RestoredBlockHash = "0x"+strings.Repeat("de", 32), "0x"+strings.Repeat("ef", 32)
	for i, target := range record.Targets {
		record.Processes = append(record.Processes, FaultProcessEvidence{ID: target, Role: "synthetic-worker", Identity: "synthetic-generation", PID: 731 + i})
		record.RestoredProcesses = append(record.RestoredProcesses, FaultProcessEvidence{ID: target, Role: "synthetic-worker", Identity: "synthetic-generation", PID: 831 + i})
	}
	later := testScenarioObservation(cfg, window.BaselineEpoch)
	later.ObservedAt = time.Date(2026, 9, 3, 7, 3, 0, 0, time.UTC).Format(time.RFC3339Nano)
	later.Status.Contracts.FinalizedHead = ChainHead{Number: record.RestoredBlock, Hash: record.RestoredBlockHash}
	later.ObservationHash, _ = canonicalHashHex(later)
	if err := appendObservation(filepath.Join(runDir, "observations.jsonl"), later); err != nil {
		t.Fatal(err)
	}
	if err := attempt.updateAuthenticatedRuntime(runDir, faults); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(attempt.payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ before, after string }{
		{before: `"pid":731`, after: `"pid":731,"start_time_ticks":123456`},
		{before: `"pid":831`, after: `"pid":831,"start_time_ticks":234567`},
	} {
		if bytes.Count(payload, []byte(change.before)) != 1 {
			t.Fatal("fixture process identity is not unique")
		}
		payload = bytes.Replace(payload, []byte(change.before), []byte(change.after), 1)
	}
	envelope, err := signEvidence(cfg, scenarioCampaignAttemptEvidenceKind, attempt.payload.RunID, json.RawMessage(payload), attempt.roles.EVM["testnet-owner"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(attempt.path(), envelope); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(attempt.path())
	if err != nil {
		t.Fatal(err)
	}
	return attempt, runDir, index, raw
}

func TestFaultProcessWireSignedCheckpointPreservesGenerations(t *testing.T) {
	attempt, runDir, index, raw := faultProcessWireAttempt(t)
	loaded, retained, err := readScenarioCampaignAttemptAt(attempt.cfg, attempt.stateDir, attempt.roles, campaignTestPlanHash, "release-1.0", attempt.path())
	if err != nil {
		t.Fatalf("signed historical process checkpoint failed: %v", err)
	}
	if !bytes.Equal(retained, raw) {
		t.Fatal("reader changed the exact signed envelope")
	}
	_, _, _, _, faults, err := loaded.loadAuthenticatedRuntimeForensics(runDir)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(faults[index])
	if err != nil || !bytes.Contains(encoded, []byte(`"start_time_ticks":123456`)) || !bytes.Contains(encoded, []byte(`"start_time_ticks":234567`)) {
		t.Fatalf("original or restored generation was discarded: %s, %v", encoded, err)
	}
	for _, historical := range []bool{false, true} {
		if _, retained, err := readScenarioCampaignAttemptAtContext(attempt.cfg, attempt.stateDir, attempt.roles, campaignTestPlanHash, "release-1.0", attempt.path(), historical); err != nil || !bytes.Equal(raw, retained) {
			t.Fatalf("historical=%t changed the authenticated source: %v", historical, err)
		}
	}
	after, err := os.ReadFile(attempt.path())
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("forensic read wrote back the source", err)
	}
	// Recognition must not bypass signature verification for either generation.
	for _, ticks := range []string{"123456", "234567"} {
		changed := bytes.Replace(raw, []byte(`"start_time_ticks": `+ticks), []byte(`"start_time_ticks": 345678`), 1)
		if bytes.Equal(changed, raw) {
			t.Fatal("fixture lacks the expected signed generation")
		}
		if err := os.WriteFile(attempt.path(), changed, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readScenarioCampaignAttemptAt(attempt.cfg, attempt.stateDir, attempt.roles, campaignTestPlanHash, "release-1.0", attempt.path()); err == nil || !strings.Contains(err.Error(), "signature") {
			t.Fatalf("unsigned generation replacement was accepted: %v", err)
		}
	}
}

func TestFaultProcessWireLegacyAndStrictUint64(t *testing.T) {
	legacy := `{"id":"synthetic-worker","role":"worker","identity":"synthetic-generation","pid":731}`
	for _, suffix := range []string{"", `,"start_time_ticks":1`, `,"start_time_ticks":18446744073709551615`} {
		raw := strings.TrimSuffix(legacy, "}") + suffix + "}"
		var process FaultProcessEvidence
		if err := decodeStrictJSONBytes([]byte(raw), &process); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(process)
		if err != nil || string(encoded) != raw {
			t.Fatalf("historical process bytes changed: %s, %v", encoded, err)
		}
	}
	for _, suffix := range []string{
		`,"start_time_ticks":-1`, `,"start_time_ticks":1.5`, `,"start_time_ticks":"1"`,
		`,"start_time_ticks":18446744073709551616`, `,"start_time_ticks":1,"unknown_generation":2`,
	} {
		var process FaultProcessEvidence
		if err := decodeStrictJSONBytes([]byte(strings.TrimSuffix(legacy, "}")+suffix+"}"), &process); err == nil {
			t.Errorf("accepted malformed generation evidence: %s", suffix)
		}
	}
}

func TestFaultProcessWireHistoryRejectsGenerationRewrite(t *testing.T) {
	for _, field := range []string{"processes", "restored_processes"} {
		raw := `{"id":"synthetic-restart","kind":"process-restart","targets":["synthetic-worker"],"trigger_block":10,"restore_block":20,"applied_block":10,"restored_block":20,"status":"restored","` + field + `":[{"id":"synthetic-worker","role":"worker","identity":"synthetic-generation","pid":731,"start_time_ticks":123456}]}`
		var before ScenarioFaultRecord
		if err := decodeStrictJSONBytes([]byte(raw), &before); err != nil {
			t.Fatal(err)
		}
		if err := validateScenarioFaultProgress([]ScenarioFaultRecord{before}, cloneScenarioFaultRecords([]ScenarioFaultRecord{before})); err != nil {
			t.Fatal("unchanged historical generation was rejected", err)
		}
		for _, changed := range []string{strings.Replace(raw, ",\"start_time_ticks\":123456", "", 1), strings.Replace(raw, "123456", "234567", 1)} {
			var after ScenarioFaultRecord
			if err := decodeStrictJSONBytes([]byte(changed), &after); err != nil {
				t.Fatal(err)
			}
			if err := validateScenarioFaultProgress([]ScenarioFaultRecord{before}, []ScenarioFaultRecord{after}); err == nil {
				t.Errorf("accepted %s generation rewrite", field)
			}
		}
	}
}

func TestFaultProcessWireActiveRecoveryPreservesGeneration(t *testing.T) {
	for _, schema := range []string{"urnetwork-sim-active-faults-v1", "urnetwork-sim-active-faults-v2", minerControlProgressSchema} {
		raw := `{"schema":"` + schema + `","faults":[{"id":"synthetic-restart","kind":"process-restart","targets":["synthetic-worker"],"trigger_offset_blocks":10,"duration_blocks":20}],"processes":[{"id":"synthetic-worker","role":"worker","identity":"synthetic-generation","pid":731,"start_time_ticks":123456}]}`
		path := filepath.Join(t.TempDir(), "active-faults.json")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		active, err := readActiveFaultFile(path)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(active)
		if err != nil || string(encoded) != raw {
			t.Fatalf("active recovery discarded generation evidence: %s, %v", encoded, err)
		}
	}
}

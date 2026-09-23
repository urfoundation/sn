// Actor diagnostics retain causal error samples under independent record/byte
// bounds without changing the acceptance counters or leaking configured secrets.
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

// Later success must not erase the exact earlier sample, scope, or error count.
func TestAdversaryErrorChronologyRetainsOnlyErrors(t *testing.T) {
	dir := t.TempDir()
	log, err := newAdversaryErrorLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2030, 1, 2, 3, 4, 5, 600, time.UTC)
	window := newAdversaryFaultWindow(time.Minute)
	window.now = func() time.Time { return at }
	window.Update([]string{"restored.example"})
	window.Update([]string{"active.example"})
	state := &adversaryActorState{evidence: AdversaryActorEvidence{ID: "actor.example"}}
	campaign := &liveAdversaryCampaign{states: map[string]*adversaryActorState{"actor.example": state}, faultWindow: window, errorLog: log}
	for sequence, outcome := range []string{adversaryOutcomeSuccess, adversaryOutcomeExpectedRejection, adversaryOutcomeSkipped} {
		campaign.record("actor.example", adversaryControlPhase, uint64(sequence), at, time.Millisecond, adversarySampleResult{Outcome: outcome, Detail: "healthy"})
	}
	if _, err := os.Stat(log.path); !os.IsNotExist(err) {
		t.Fatalf("nonerror sample wrote chronology: %v", err)
	}
	campaign.record("actor.example", adversaryAttackPhase, 3, at, 7*time.Millisecond, adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "exact failing branch", Requests: 8})
	campaign.record("actor.example", adversaryControlPhase, 4, at.Add(time.Second), time.Millisecond, adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: "later success"})
	data, err := os.ReadFile(log.path)
	if err != nil {
		t.Fatal(err)
	}
	var record adversaryErrorObservation
	if err := json.Unmarshal(bytes.TrimSpace(data), &record); err != nil {
		t.Fatal(err)
	}
	if record.ActorId != "actor.example" || record.Sequence != 3 || record.Phase != adversaryAttackPhase || record.SampledAt != at.Format(time.RFC3339Nano) || record.DurationMillis != 7 || record.Requests != 8 || record.ErrorCount != 1 || record.Detail != "exact failing branch" {
		t.Fatalf("lost sample identity: %+v", record)
	}
	if strings.Join(record.ActiveFaultTargets, ",") != "active.example" || strings.Join(record.GraceFaultTargets, ",") != "restored.example" {
		t.Fatalf("lost exact fault scope: %+v", record)
	}
	if state.evidence.Errors != 1 || state.evidence.Samples != 4 || state.evidence.Successful != 2 || state.evidence.ExpectedRejections != 1 || state.evidence.Skipped != 1 || state.evidence.LastDetail != "later success" {
		t.Fatalf("diagnostics changed counters: %+v", state.evidence)
	}
	info, err := os.Stat(log.path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("chronology permissions: %v %v", info, err)
	}
}

// Redaction precedes truncation; capped files retain earlier bytes after reopen.
func TestAdversaryErrorChronologyBoundsAndReopen(t *testing.T) {
	dir := t.TempDir()
	secret := "synthetic-secret.example"
	log, err := newAdversaryErrorLog(dir, secret)
	if err != nil {
		t.Fatal(err)
	}
	log.maxRecords = 1
	log.append(adversaryErrorObservation{ActorId: "actor.example", Detail: strings.Repeat(secret+" ", 400)})
	before, err := os.ReadFile(log.path)
	if err != nil {
		t.Fatal(err)
	}
	var record adversaryErrorObservation
	if err := json.Unmarshal(bytes.TrimSpace(before), &record); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), secret) || !record.DetailTruncated || len(record.Detail) != adversaryErrorDetailMaxBytes {
		t.Fatalf("redaction/truncation failed: length=%d truncated=%t", len(record.Detail), record.DetailTruncated)
	}
	log.append(adversaryErrorObservation{ActorId: "actor.example", Detail: "omitted by record bound"})
	if err := log.finish(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAdversaryErrorLog(dir, secret)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.recorded != 1 || reopened.omitted != 1 || reopened.byteCount != int64(len(before)) {
		t.Fatalf("reopen lost bounds: %+v", reopened)
	}
	reopened.maxBytes = reopened.byteCount
	reopened.append(adversaryErrorObservation{ActorId: "actor.example", Detail: "omitted by byte bound"})
	if err := reopened.finish(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(log.path)
	if err != nil || !bytes.Equal(before, after) || reopened.omitted != 2 {
		t.Fatalf("cap replaced earlier evidence: err=%v omitted=%d", err, reopened.omitted)
	}
}

// A substituted path fails visibly and cannot redirect diagnostic writes.
func TestAdversaryErrorChronologyRejectsReplacedWriter(t *testing.T) {
	dir := t.TempDir()
	log, err := newAdversaryErrorLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.jsonl")
	if err := os.WriteFile(other, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, log.path); err != nil {
		t.Fatal(err)
	}
	state := &adversaryActorState{evidence: AdversaryActorEvidence{ID: "actor.example"}}
	campaign := &liveAdversaryCampaign{states: map[string]*adversaryActorState{"actor.example": state}, errorLog: log}
	campaign.record("actor.example", adversaryAttackPhase, 1, time.Now(), time.Millisecond, adversarySampleResult{Outcome: adversaryOutcomeError, Detail: "still blocking"})
	if state.evidence.Errors != 1 || log.finish() == nil {
		t.Fatalf("write failure lost actor error or was hidden: %+v", state.evidence)
	}
	if _, err := newAdversaryErrorLog(dir); err == nil {
		t.Fatal("reopen followed substituted link")
	}
	data, err := os.ReadFile(other)
	if err != nil || string(data) != "retained" {
		t.Fatalf("substituted destination changed: %q %v", data, err)
	}
}

// Clock regression increments acceptance errors even when the actor succeeds.
func TestAdversaryErrorChronologyRecordsClockRegression(t *testing.T) {
	log, err := newAdversaryErrorLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	state := &adversaryActorState{evidence: AdversaryActorEvidence{ID: "actor.example"}, lastSampleAt: at}
	campaign := &liveAdversaryCampaign{states: map[string]*adversaryActorState{"actor.example": state}, errorLog: log}
	campaign.record("actor.example", adversaryControlPhase, 1, at.Add(-time.Second), time.Millisecond, adversarySampleResult{Outcome: adversaryOutcomeSuccess, Detail: "actor succeeded"})
	data, err := os.ReadFile(log.path)
	if err != nil || !bytes.Contains(data, []byte("sample clock moved backwards")) || state.evidence.Errors != 1 || state.evidence.Successful != 1 {
		t.Fatalf("clock error omitted: errors=%d successful=%d err=%v", state.evidence.Errors, state.evidence.Successful, err)
	}
}

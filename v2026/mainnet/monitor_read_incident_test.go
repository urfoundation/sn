// Physical command reads and cold checkpoint reloads exercise incident
// continuity. All ordering comes from owned read, publication and wait barriers.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/diagnostics"
)

// Decode the actual bounded checkpoint without using the production loader.
func monitorReadTestCheckpoint(t testing.TB, path string) ([]byte, monitorServiceCheckpointRecord) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record monitorServiceCheckpointRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return raw, record
}

// Deliberately rehash altered checkpoints to reach semantic admission checks.
func monitorReadTestWriteCheckpoint(t testing.TB, path string, record monitorServiceCheckpointRecord) {
	t.Helper()
	var err error
	record.ContentHash, err = hashMonitorServiceCheckpoint(record)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishMonitorFile(path, raw, 0600, nil); err != nil {
		t.Fatal(err)
	}
}

// A positive publication barrier supplies the complete actual incident record.
func monitorReadTestHistory(t testing.TB, event monitorServiceEvent) *monitorReadIncidentHistory {
	t.Helper()
	if event.State == nil || event.State.ReadIncidents == nil {
		t.Fatal("completed service observation omitted read history", event)
	}
	return event.State.ReadIncidents
}

// Repeated failed reads remain one incident through restart. Recovery retains
// the failure, and a later outage owns a new id while the first evidence survives.
func TestMonitorReadIncidentCommandRecoveryRecurrenceAndRestart(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	policy := fixture.policy.Validators[0]
	original := monitorServicesTestRecord(fixture.clock.now(), 1)
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	first := monitorReadTestHistory(t, run.next(t))
	if first.Incidents != 1 || first.FailedReads != 1 || first.PriorHistoryUnknown || first.FirstIncident == nil || first.LastIncident == nil || first.LastIncident.Recovery != nil {
		t.Fatal("initial missing read did not open exactly one incident", first)
	}
	id, failedAt := first.LastIncident.Id, first.FirstIncident.FirstFailure.ObservedAt
	run.again(t, "alpha")
	repeated := monitorReadTestHistory(t, run.next(t))
	if repeated.Incidents != 1 || repeated.FailedReads != 2 || repeated.LastIncident.Id != id {
		t.Fatal("duplicate outage observation opened another incident", repeated)
	}
	run.cancel()
	<-run.done
	fixture.clock.seconds.Add(301)
	restarted := fixture.start(t, url, monitorServiceHooks{})
	continued := monitorReadTestHistory(t, restarted.next(t))
	if continued.LastIncident.Id != id || continued.FailedReads != 3 || continued.FirstIncident.FirstFailure.ObservedAt != failedAt {
		t.Fatal("cold restart reset incident identity or first failure", continued)
	}
	// The readable record remains stale. Closing its read outage cannot turn
	// producer heartbeat, native state or settlement into fresh evidence.
	monitorServicesTestWrite(t, policy.ProgressFile, original)
	restarted.again(t, "alpha")
	recoveredEvent := restarted.next(t)
	recovered := monitorReadTestHistory(t, recoveredEvent)
	if recoveredEvent.Status != "stale" || recoveredEvent.Severity != "critical" || recovered.LastIncident.Recovery == nil || recovered.LastRecovery == nil {
		t.Fatal("read recovery either vanished or fabricated service health", recoveredEvent)
	}
	raw, err := original.Encode()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	recovery := *recovered.LastRecovery
	if recovery.IncidentId != id || recovery.RecordHash != "sha256:"+hex.EncodeToString(digest[:]) || recovery.HeartbeatAt != original.HeartbeatAt || recovery.ConfigHash != original.Source.ConfigHash {
		t.Fatal("recovery did not bind the exact decoded record and original ages", recovery)
	}
	_, metricsPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	metrics := monitorServicesGauges(t, metricsPath, policy.Role)
	if metrics["read_incident_open"] != 0 || metrics["read_incident_count"] != 1 || metrics["read_current"] != 1 || metrics["source_current"] != 0 || metrics["native_current"] != 0 {
		t.Fatal("read recovery erased history or upgraded stale evidence", metrics)
	}
	fixture.clock.seconds.Add(1)
	restarted.again(t, "alpha")
	duplicate := monitorReadTestHistory(t, restarted.next(t))
	if duplicate.Incidents != 1 || duplicate.FailedReads != 3 || *duplicate.LastRecovery != recovery {
		t.Fatal("repeated recovery rewrote first successful read evidence", duplicate)
	}
	fixture.clock.seconds.Add(1)
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	restarted.again(t, "alpha")
	recurred := monitorReadTestHistory(t, restarted.next(t))
	if recurred.Incidents != 2 || recurred.FailedReads != 4 || recurred.LastIncident.Id == id ||
		recurred.FirstIncident.Id != id || recurred.FirstIncident.Recovery == nil || *recurred.LastRecovery != recovery {
		t.Fatal("recurrence replaced first failure or its recovery", recurred)
	}
	secondId := recurred.LastIncident.Id
	restarted.cancel()
	<-restarted.done
	fixture.clock.seconds.Add(1)
	secondRestart := fixture.start(t, url, monitorServiceHooks{})
	retained := monitorReadTestHistory(t, secondRestart.next(t))
	if retained.LastIncident.Id != secondId || retained.LastIncident.Sequence != 2 || retained.FailedReads != 5 || retained.LastIncident.FailedReads != 2 {
		t.Fatal("recurring incident lost its durable sequence", retained)
	}
	monitorServicesTestWrite(t, policy.ProgressFile, monitorServicesTestRecord(fixture.clock.now(), 1))
	secondRestart.again(t, "alpha")
	closed := monitorReadTestHistory(t, secondRestart.next(t))
	if closed.LastRecovery.IncidentId != secondId || closed.FirstIncident.Id != id || *closed.FirstIncident.Recovery != recovery {
		t.Fatal("second recovery replaced the original incident", closed)
	}
}

// Failure classes can change inside one outage. An independently configured
// renewal retains the original incident config and the actual recovered config.
func TestMonitorReadIncidentCommandRenewalKeepsOriginalAuthority(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	policy := fixture.policy.Validators[0]
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	first := monitorReadTestHistory(t, run.next(t))
	value := monitorServicesTestRecord(fixture.clock.now(), 2)
	monitorServicesTestWrite(t, policy.ProgressFile, value)
	run.again(t, "alpha")
	mismatch := run.next(t)
	history := monitorReadTestHistory(t, mismatch)
	if mismatch.Status != "identity" || mismatch.Severity != "critical" || history.Incidents != 1 || history.LastIncident.Id != first.LastIncident.Id ||
		history.FirstIncident.FirstFailure.Status != "missing" || history.LastIncident.LastFailure.Status != "identity" || history.LastRecovery != nil {
		t.Fatal("wrong source recovered or reclassified the original incident", mismatch)
	}
	run.cancel()
	<-run.done
	fixture.clock.seconds.Add(1)
	value = monitorServicesTestRecord(fixture.clock.now(), 1)
	value.Source.ConfigHash = "sha256:" + strings.Repeat("4", 64)
	value.Native = nil
	fixture.policy.Validators[0].ExpectedSource = value.Source
	fixture.writePolicy(t)
	monitorServicesTestWrite(t, policy.ProgressFile, value)
	renewed := fixture.start(t, url, monitorServiceHooks{})
	event := renewed.next(t)
	history = monitorReadTestHistory(t, event)
	if event.Status != "unknown" || history.LastIncident.Id != first.LastIncident.Id || history.LastIncident.ExpectedConfigHash != policy.ExpectedSource.ConfigHash ||
		history.LastRecovery == nil || history.LastRecovery.ConfigHash != value.Source.ConfigHash || history.Producer != policy.ExpectedSource {
		t.Fatal("renewal rewrote original authority or invented domain health", event)
	}
}

// Cancel after the real source descriptor has read bytes but before completion.
// Joined cancellation must not checkpoint or emit a manufactured recovery.
func TestMonitorReadIncidentCommandCancellationPreservesOpenCheckpoint(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	policy := fixture.policy.Validators[0]
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	opened := monitorReadTestHistory(t, run.next(t))
	run.cancel()
	<-run.done
	checkpointPath, metricsPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	before, _ := monitorReadTestCheckpoint(t, checkpointPath)
	metricsBefore, err := os.ReadFile(metricsPath)
	if err != nil {
		t.Fatal(err)
	}
	monitorServicesTestWrite(t, policy.ProgressFile, monitorServicesTestRecord(fixture.clock.now(), 1))
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	hooks := monitorServiceHooks{read: func(string) monitorServiceReadHooks {
		return monitorServiceReadHooks{afterRead: func(*os.File) error {
			close(entered)
			<-release
			return nil
		}}
	}}
	blocked := fixture.start(t, url, hooks)
	defer once.Do(func() { close(release) })
	<-entered
	blocked.cancel()
	once.Do(func() { close(release) })
	<-blocked.done
	after, record := monitorReadTestCheckpoint(t, checkpointPath)
	metricsAfter, err := os.ReadFile(metricsPath)
	if err != nil || blocked.exit != 0 || !bytes.Equal(before, after) || !bytes.Equal(metricsBefore, metricsAfter) ||
		record.State.ReadIncidents.LastIncident.Id != opened.LastIncident.Id || record.State.ReadIncidents.LastIncident.Recovery != nil {
		t.Fatal("cancellation acknowledged an unfinished recovery", blocked.exit, err)
	}
}

// A local corruption cannot reset history. Even a rehashed object must satisfy
// the incident/source/count invariants before a sampling worker is launched.
func TestMonitorReadIncidentCheckpointRejectsCorruptAndForgedContinuity(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	policy := fixture.policy.Validators[0]
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	monitorReadTestHistory(t, run.next(t))
	run.cancel()
	<-run.done
	path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	raw, original := monitorReadTestCheckpoint(t, path)
	owner, err := openMonitorServiceCheckpoint(path, monitorTestExpectation(), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.owner.close() })
	if _, err := owner.load(t.Context()); err != nil {
		t.Fatal("unaltered incident did not load", err)
	}
	corrupt := bytes.Replace(raw, []byte(original.State.ReadIncidents.LastIncident.Id), []byte("sha256:"+strings.Repeat("f", 64)), 1)
	if err := publishMonitorFile(path, corrupt, 0600, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.load(t.Context()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal("corrupt incident identity bypassed checkpoint checksum", err)
	}
	for _, c := range []struct {
		name   string
		change func(*monitorServiceCheckpointRecord)
	}{
		{name: "history omitted", change: func(r *monitorServiceCheckpointRecord) { r.State.ReadIncidents = nil }},
		{name: "foreign producer", change: func(r *monitorServiceCheckpointRecord) { r.State.ReadIncidents.Producer.ValidatorId++ }},
		{name: "changed incident id", change: func(r *monitorServiceCheckpointRecord) {
			r.State.ReadIncidents.LastIncident.Id = "sha256:" + strings.Repeat("f", 64)
		}},
		{name: "missing failures", change: func(r *monitorServiceCheckpointRecord) { r.State.ReadIncidents.LastIncident.FailedReads = 0 }},
		{name: "duplicate sequence", change: func(r *monitorServiceCheckpointRecord) { r.State.ReadIncidents.Incidents = 2 }},
		{name: "empty history", change: func(r *monitorServiceCheckpointRecord) { r.State.ReadIncidents.Incidents = 0 }},
		{name: "invented recovery", change: func(r *monitorServiceCheckpointRecord) {
			h := r.State.ReadIncidents
			recovery := &monitorReadRecovery{IncidentId: h.LastIncident.Id, Sequence: 1, ObservedAt: fixture.clock.now(), ConfigHash: policy.ExpectedSource.ConfigHash,
				InstanceId: strings.Repeat("2", 32), HeartbeatAt: fixture.clock.now().Format(time.RFC3339Nano), RecordHash: "sha256:" + strings.Repeat("3", 64)}
			h.FirstIncident.Recovery, h.LastIncident.Recovery, h.LastRecovery = recovery, recovery, recovery
		}},
		{name: "v2 downgrade", change: func(r *monitorServiceCheckpointRecord) { r.Schema = "urnetwork-mainnet-validator-checkpoint-v2" }},
		{name: "v1 downgrade", change: func(r *monitorServiceCheckpointRecord) { r.Schema = "urnetwork-mainnet-validator-checkpoint-v1" }},
	} {
		var record monitorServiceCheckpointRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		c.change(&record)
		monitorReadTestWriteCheckpoint(t, path, record)
		if _, err := owner.load(t.Context()); err == nil {
			t.Fatal("rehashed malformed incident was admitted", c.name)
		}
	}
}

// A closed incident retains only a read that could actually have been accepted.
// Rehashing all duplicate summaries cannot invent a future recovery or heartbeat.
func TestMonitorReadIncidentCheckpointRejectsImpossibleRecoveryClocks(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	policy := fixture.policy.Validators[0]
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	monitorReadTestHistory(t, run.next(t))
	monitorServicesTestWrite(t, policy.ProgressFile, monitorServicesTestRecord(fixture.clock.now(), 1))
	run.again(t, "alpha")
	monitorReadTestHistory(t, run.next(t))
	run.cancel()
	<-run.done
	path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	raw, _ := monitorReadTestCheckpoint(t, path)
	owner, err := openMonitorServiceCheckpoint(path, monitorTestExpectation(), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.owner.close() })
	if _, err := owner.load(t.Context()); err != nil {
		t.Fatal("actual recovery did not survive admission", err)
	}
	for _, c := range []struct {
		name   string
		change func(*monitorReadRecovery)
		want   string
	}{
		{name: "unaccepted future heartbeat", change: func(r *monitorReadRecovery) {
			r.HeartbeatAt = r.ObservedAt.Add(monitorServiceClockAllowance + time.Second).Format(time.RFC3339Nano)
		}, want: "future heartbeat"},
		{name: "unobserved recovery", change: func(r *monitorReadRecovery) {
			r.ObservedAt = fixture.clock.now().Add(time.Second)
		}, want: "clock high-water"},
	} {
		var record monitorServiceCheckpointRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		history := record.State.ReadIncidents
		for _, recovery := range []*monitorReadRecovery{history.FirstIncident.Recovery, history.LastIncident.Recovery, history.LastRecovery} {
			c.change(recovery)
		}
		monitorReadTestWriteCheckpoint(t, path, record)
		if _, err := owner.load(t.Context()); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatal("rehashed impossible recovery passed its actual read predicate", c.name, err)
		}
	}
}

// The legacy last failed read and outage boundary survive an immediate recovery.
// Counts explicitly exclude unknown earlier history; migration creates no health.
func TestMonitorReadIncidentCommandMigratesLegacyOpenOutage(t *testing.T) {
	for _, schema := range []string{"urnetwork-mainnet-validator-checkpoint-v1", "urnetwork-mainnet-validator-checkpoint-v2"} {
		fixture := newMonitorServicesFixture(t, "alpha")
		policy := fixture.policy.Validators[0]
		prior := fixture.clock.now().Add(-10 * time.Minute)
		state := monitorValidatorState{}
		state.observe(prior, prior.Add(time.Minute), nil, "missing")
		path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
		record := monitorServiceCheckpointRecord{Schema: schema, Role: policy.Role, Expected: policy.ExpectedSource, State: state}
		monitorReadTestWriteCheckpoint(t, path, record)
		url, _, _ := monitorServicesBlockedChain(t)
		run := fixture.start(t, url, monitorServiceHooks{})
		event := run.next(t)
		history := monitorReadTestHistory(t, event)
		if !history.PriorHistoryUnknown || history.Incidents != 1 || history.FailedReads != 1 ||
			history.FirstIncident.OutageSince != prior || history.FirstIncident.FirstFailure.ObservedAt != prior.Add(time.Minute) || history.LastRecovery == nil {
			t.Fatal("legacy migration lost the retained outage or invented prehistory", schema, history)
		}
		run.cancel()
		<-run.done
		_, upgraded := monitorReadTestCheckpoint(t, path)
		if upgraded.Schema != monitorServiceCheckpointSchema || upgraded.State.ReadIncidents == nil {
			t.Fatal("legacy continuity was not checkpointed in the new schema", schema)
		}
	}
}

// Healthy legacy state proves no previous outage history. Its first new failure
// begins now, with an explicit unknown-history marker and original last success.
func TestMonitorReadIncidentCommandMigratesLegacyHealthyCheckpoint(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	policy := fixture.policy.Validators[0]
	prior := fixture.clock.now().Add(-time.Hour)
	value := monitorServicesTestRecord(prior, 1)
	state := monitorValidatorState{}
	state.observe(prior, prior, &value, "ok")
	path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	monitorReadTestWriteCheckpoint(t, path, monitorServiceCheckpointRecord{Schema: "urnetwork-mainnet-validator-checkpoint-v2", Role: policy.Role, Expected: policy.ExpectedSource, State: state})
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	event := run.next(t)
	history := monitorReadTestHistory(t, event)
	if !history.PriorHistoryUnknown || history.Incidents != 1 || history.FailedReads != 1 || history.FirstIncident.OutageSince != fixture.clock.now() ||
		event.State.LastReadSuccessAt != prior || history.LastRecovery != nil {
		t.Fatal("healthy legacy checkpoint invented prior outage timing", history)
	}
}

// Repeated physical checkpoint publications after a sync ambiguity keep the
// same incident. Publication failure cannot acknowledge its own durable export.
func TestMonitorReadIncidentCommandPublicationRetryKeepsIdentity(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	policy := fixture.policy.Validators[0]
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	var attempts int
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if kind == "checkpoint" {
			attempts++
			if attempts == 1 {
				return errors.Join(err, errors.New("synthetic checkpoint acknowledgment loss"))
			}
		}
		return err
	}}
	run := fixture.start(t, url, hooks)
	first := run.next(t)
	history := monitorReadTestHistory(t, first)
	_, metricsPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	metrics := monitorServicesGauges(t, metricsPath, policy.Role)
	if first.Publication != "retrying" || metrics["checkpoint_current"] != 0 || metrics["export_last_success_timestamp_seconds"] != 0 {
		t.Fatal("uncertain checkpoint became confirmed incident durability", first, metrics)
	}
	run.again(t, "alpha")
	next := run.next(t)
	continued := monitorReadTestHistory(t, next)
	if next.Publication != "published" || continued.Incidents != 1 || continued.FailedReads != 2 || continued.LastIncident.Id != history.LastIncident.Id {
		t.Fatal("publication retry created another incident", next)
	}
}

// Counter saturation cannot wrap incident counts or collide a new identity.
// Injected near-exhaustion state goes through real private checkpoint admission.
func TestMonitorReadIncidentCommandSaturatesCountsAndRefusesSequenceExhaustion(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	policy := fixture.policy.Validators[0]
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	monitorReadTestHistory(t, run.next(t))
	run.cancel()
	<-run.done
	path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	_, record := monitorReadTestCheckpoint(t, path)
	history := record.State.ReadIncidents
	history.FailedReads, history.FirstIncident.FailedReads, history.LastIncident.FailedReads = math.MaxUint64, math.MaxUint64, math.MaxUint64
	monitorReadTestWriteCheckpoint(t, path, record)
	restarted := fixture.start(t, url, monitorServiceHooks{})
	event := restarted.next(t)
	if continued := monitorReadTestHistory(t, event); continued.FailedReads != math.MaxUint64 || continued.LastIncident.FailedReads != math.MaxUint64 || continued.Incidents != 1 {
		t.Fatal("failure count wrapped instead of saturating", continued)
	}
	monitorServicesTestWrite(t, policy.ProgressFile, monitorServicesTestRecord(fixture.clock.now(), 1))
	restarted.again(t, "alpha")
	monitorReadTestHistory(t, restarted.next(t))
	restarted.cancel()
	<-restarted.done
	_, record = monitorReadTestCheckpoint(t, path)
	history = record.State.ReadIncidents
	history.Incidents, history.LastIncident.Sequence = math.MaxUint64, math.MaxUint64
	var err error
	history.LastIncident.Id, err = monitorReadIncidentId(policy, *history.LastIncident)
	if err != nil {
		t.Fatal(err)
	}
	history.LastIncident.Recovery.IncidentId, history.LastIncident.Recovery.Sequence = history.LastIncident.Id, math.MaxUint64
	history.LastRecovery = history.LastIncident.Recovery
	monitorReadTestWriteCheckpoint(t, path, record)
	before, _ := monitorReadTestCheckpoint(t, path)
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	exhausted := fixture.start(t, url, monitorServiceHooks{})
	<-exhausted.done
	after, _ := monitorReadTestCheckpoint(t, path)
	if exhausted.exit != 3 || !strings.Contains(exhausted.stderr.String(), "sequence is exhausted") || !bytes.Equal(before, after) {
		t.Fatal("exhausted sequence wrapped or altered its durable history", exhausted.exit, exhausted.stderr.String())
	}
}

// Worst-case escaped deployment identity, diagnostic counters and retained native
// misses share the unchanged output budgets through many actual outage cycles.
func TestMonitorReadIncidentCommandHistoryFitsExistingBudgets(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	value := monitorDeadlineTestRecord(fixture.clock.now(), 1, 201, 9)
	value.Source.DeploymentId = strings.Repeat("\x00", 256)
	value.Source.ValidatorId, value.Source.Netuid = math.MaxUint64, math.MaxUint16
	value.Diagnostics = monitorDiagnosticTestObservation(fixture.clock.now(), math.MaxUint64)
	diagnostic := value.Diagnostics.Startup
	diagnostic.Dropped, diagnostic.DroppedBytes, diagnostic.Unavailable = math.MaxUint64, math.MaxUint64, math.MaxUint64
	value.Diagnostics.Startup, value.Diagnostics.Steering, value.Diagnostics.Progress, value.Diagnostics.Operator, value.Diagnostics.Runtime = diagnostic, diagnostic, diagnostic, diagnostic, diagnostic
	fixture.policy.Validators[0].ExpectedSource = value.Source
	fixture.writePolicy(t)
	policy := fixture.policy.Validators[0]
	monitorServicesTestWrite(t, policy.ProgressFile, value)
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	if event := run.next(t); event.State.NativeDeadline == nil {
		t.Fatal("fixture omitted the independent native incident")
	}
	path, metricsPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	var firstId string
	for episode := uint64(1); episode <= 25; episode++ {
		if err := os.Remove(policy.ProgressFile); err != nil {
			t.Fatal(err)
		}
		run.again(t, "alpha")
		opened := monitorReadTestHistory(t, run.next(t))
		if episode == 1 {
			firstId = opened.FirstIncident.Id
		}
		monitorServicesTestWrite(t, policy.ProgressFile, value)
		run.again(t, "alpha")
		event := run.next(t)
		history := monitorReadTestHistory(t, event)
		checkpointRaw, _ := monitorReadTestCheckpoint(t, path)
		eventRaw, err := json.Marshal(event)
		if err != nil || len(checkpointRaw) > 16*1024 || len(eventRaw)+1 > diagnostics.MaximumRecordBytes ||
			history.Incidents != episode || history.FailedReads != episode || history.FirstIncident.Id != firstId ||
			event.Status != "native-window-missed" || event.State.NativeDeadline == nil {
			t.Fatal("bounded read history lost continuity or cleared a native incident", episode, len(checkpointRaw), len(eventRaw), err)
		}
		metrics := monitorServicesGauges(t, metricsPath, policy.Role)
		if metrics["native_deadline_unresolved"] != 1 || metrics["read_incident_open"] != 0 || metrics["read_incident_count"] != float64(episode) {
			t.Fatal("recovered read incident changed independent deadline authority", metrics)
		}
	}
}

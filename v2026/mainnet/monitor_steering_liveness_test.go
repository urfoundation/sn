// Physical command observations and explicit clocks separate producer heartbeat,
// returned steering outcomes, durable incidents, and unauthorised repair actions.
package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Synthetic process start and last outcome stay fixed while other owners work.
func monitorSteeringTestRecord(now, started, outcome time.Time, id uint64) protocol.ValidatorProgress {
	value := monitorServicesTestRecord(now, id)
	value.StartedAt = started.Format(time.RFC3339Nano)
	stamp := outcome.Format(time.RFC3339Nano)
	value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: stamp, LastSuccessAt: stamp,
		Current: true, EpochKnown: true, NativeEpoch: 8, Outcome: "reveal_wait"}
	return value
}

// Each role independently opts into finite observation margins.
func monitorSteeringTestPolicy(t *testing.T, roles ...string) *monitorServicesFixture {
	t.Helper()
	fixture := newMonitorServicesFixture(t, roles...)
	for index := range fixture.policy.Validators {
		fixture.policy.Validators[index].SteeringLiveness = &monitorSteeringLivenessPolicy{WarningAfterSeconds: 120, CriticalAfterSeconds: 300}
		monitorServicesTestWrite(t, fixture.policy.Validators[index].ProgressFile,
			monitorSteeringTestRecord(fixture.clock.now(), fixture.clock.now().Add(-time.Hour), fixture.clock.now(), uint64(index+1)))
	}
	fixture.writePolicy(t)
	return fixture
}

// An active publisher and other real-domain observations cannot hide a wedged
// steering owner. Recovery, recurrence, missing input and restart keep their cuts.
func TestMonitorSteeringLivenessCommandIncidentRecoveryAndRestart(t *testing.T) {
	fixture := monitorSteeringTestPolicy(t, "alpha")
	policy := fixture.policy.Validators[0]
	initial := fixture.clock.now()
	started := initial.Add(-time.Hour)
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	first := run.next(t)
	if first.Status != "observed" || first.SteeringLiveness == nil || first.SteeringLiveness.Status != "responsive" || first.State.SteeringLiveness == nil || first.State.SteeringLiveness.Baseline == nil {
		t.Fatal("responsive baseline not admitted", first)
	}
	fixture.clock.seconds.Add(120)
	monitorServicesTestWrite(t, policy.ProgressFile, monitorSteeringTestRecord(fixture.clock.now(), started, initial, 1))
	run.again(t, "alpha")
	warning := run.next(t)
	if warning.Status != "steering-stalled" || warning.Severity != "warning" || warning.State.SteeringLiveness.Incidents != 0 {
		t.Fatal("aging loop was hidden by fresh independent domains", warning)
	}
	fixture.clock.seconds.Add(180)
	monitorServicesTestWrite(t, policy.ProgressFile, monitorSteeringTestRecord(fixture.clock.now(), started, initial, 1))
	run.again(t, "alpha")
	stalled := run.next(t)
	history := stalled.State.SteeringLiveness
	if stalled.Status != "steering-stalled" || stalled.Severity != "critical" || !history.unresolved() || history.Incidents != 1 || history.LastIncident.Failure.Steering.ObservedAt != initial.Format(time.RFC3339Nano) {
		t.Fatal("fresh publisher concealed a stopped steering owner", stalled)
	}
	id, detected := history.LastIncident.Id, history.LastIncident.Failure.ObservedAt
	checkpointPath, metricsPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	gauges := monitorServicesGauges(t, metricsPath, policy.Role)
	if gauges["source_current"] != 1 || gauges["intent_current"] != 1 || gauges["native_current"] != 1 || gauges["settlement_current"] != 1 || gauges["steering_liveness_unresolved"] != 1 || gauges["steering_liveness_status"] != 5 || gauges["severity"] != 2 || gauges["protocol_deadline_known"] != 0 {
		t.Fatal("liveness and protocol evidence collapsed", gauges)
	}
	run.cancel()
	<-run.done
	if run.exit != 0 {
		t.Fatal("monitor did not join", run.exit, run.stderr.String())
	}
	if err := os.Remove(policy.ProgressFile); err != nil {
		t.Fatal(err)
	}
	fixture.clock.seconds.Add(1)
	restarted := fixture.start(t, url, monitorServiceHooks{})
	missing := restarted.next(t)
	if !missing.State.SteeringLiveness.unresolved() || missing.State.SteeringLiveness.LastIncident.Id != id || missing.Severity != "critical" || missing.State.ReadIncidents.LastIncident == nil {
		t.Fatal("restart or failed read erased steering incident", missing)
	}
	// A new publisher instance with restored/missing steering cannot resolve it.
	value := monitorSteeringTestRecord(fixture.clock.now(), fixture.clock.now(), initial, 1)
	value.InstanceId, value.Steering = strings.Repeat("3", 32), nil
	monitorServicesTestWrite(t, policy.ProgressFile, value)
	restarted.again(t, "alpha")
	newPublisher := restarted.next(t)
	if !newPublisher.State.SteeringLiveness.unresolved() || newPublisher.State.ReadIncidents.LastIncident.Recovery == nil {
		t.Fatal("read recovery or new heartbeat resolved the loop incident", newPublisher)
	}
	// A returned transport wait is actual responsiveness, while success remains unknown.
	fixture.clock.seconds.Add(1)
	value.HeartbeatAt, value.Publisher.LastSuccessAt = fixture.clock.now().Format(time.RFC3339Nano), fixture.clock.now().Format(time.RFC3339Nano)
	value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: value.HeartbeatAt, Outcome: "read_wait"}
	monitorServicesTestWrite(t, policy.ProgressFile, value)
	restarted.again(t, "alpha")
	recovered := restarted.next(t)
	if recovered.State.SteeringLiveness.unresolved() || recovered.State.SteeringLiveness.LastIncident.Id != id || recovered.State.SteeringLiveness.LastIncident.Recovery == nil || !recovered.State.SteeringLiveness.LastIncident.Failure.ObservedAt.Equal(detected) || recovered.State.Record.Steering.Current {
		t.Fatal("real returned wait did not recover only responsiveness", recovered)
	}
	gauges = monitorServicesGauges(t, metricsPath, policy.Role)
	if gauges["steering_liveness_unresolved"] != 0 || gauges["steering_liveness_status"] != 3 || gauges["steering_current"] != 0 {
		t.Fatal("responsive retry was mistaken for successful steering", gauges)
	}
	fixture.clock.seconds.Add(300)
	value.HeartbeatAt, value.Publisher.LastSuccessAt = fixture.clock.now().Format(time.RFC3339Nano), fixture.clock.now().Format(time.RFC3339Nano)
	monitorServicesTestWrite(t, policy.ProgressFile, value)
	restarted.again(t, "alpha")
	second := restarted.next(t)
	if second.State.SteeringLiveness.Incidents != 2 || second.State.SteeringLiveness.LastIncident.Id == id || !second.State.SteeringLiveness.FirstDetectedAt.Equal(detected) {
		t.Fatal("recurrence lost distinct incident identity", second)
	}
	_, saved := monitorReadTestCheckpoint(t, checkpointPath)
	if saved.Schema != monitorServiceCheckpointSchema || saved.State.SteeringLiveness.LastIncident.Id != second.State.SteeringLiveness.LastIncident.Id {
		t.Fatal("published incident differs from durable cut")
	}
}

// One hung role cannot block another role, chain cancellation, or fresh waits.
func TestMonitorSteeringLivenessCommandRoleIsolationAndWaiting(t *testing.T) {
	fixture := monitorSteeringTestPolicy(t, "alpha", "beta")
	initial := fixture.clock.now()
	url, entered, left := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	run.next(t)
	run.next(t)
	<-entered
	fixture.clock.seconds.Add(300)
	for index, policy := range fixture.policy.Validators {
		outcome := initial
		if index == 1 {
			outcome = fixture.clock.now()
		}
		value := monitorSteeringTestRecord(fixture.clock.now(), initial.Add(-time.Hour), outcome, uint64(index+1))
		monitorServicesTestWrite(t, policy.ProgressFile, value)
		run.again(t, policy.Role)
		event := run.next(t)
		if index == 0 && !event.State.SteeringLiveness.unresolved() || index == 1 && (event.Status != "observed" || event.State.SteeringLiveness.unresolved()) {
			t.Fatal("role liveness was not isolated", event)
		}
	}
	run.cancel()
	<-run.done
	<-left
	if run.exit != 0 {
		t.Fatal("blocked chain did not join", run.exit)
	}
}

// No prior live outcome means unknown, even after a long startup. Policy is
// explicit; long unchanged economic state with fresh loop outcomes is healthy.
func TestMonitorSteeringLivenessStartupWaitsAndDisabledPolicy(t *testing.T) {
	fixture := monitorSteeringTestPolicy(t, "alpha")
	policy := fixture.policy.Validators[0]
	now := fixture.clock.now()
	started := now.Add(-2 * time.Hour)
	value := monitorSteeringTestRecord(now, now.Add(-time.Hour), now.Add(-time.Hour), 1)
	state := &monitorValidatorState{}
	state.observe(now, now, &value, "ok")
	if err := state.retainSteeringLiveness(policy); err != nil || state.SteeringLiveness != nil || state.steeringLiveness(policy).Status != "unknown" {
		t.Fatal("first stale observation invented an armed loop", err, state)
	}
	for _, outcome := range []string{"epoch_wait", "reveal_wait", "receipt_pending", "read_wait", "receipt_transport_wait", "hard_error"} {
		now = now.Add(time.Minute)
		value = monitorSteeringTestRecord(now, started, now, 1)
		value.Steering.Outcome = outcome
		value.Steering.Current, value.Steering.LastSuccessAt = false, ""
		state.observe(now, now, &value, "ok")
		if err := state.retainSteeringLiveness(policy); err != nil || state.SteeringLiveness.unresolved() || state.steeringLiveness(policy).Status != "responsive" {
			t.Fatal("returned loop outcome was mistaken for a hang", outcome, err, state)
		}
	}
	policy.SteeringLiveness = nil
	if state.steeringLiveness(policy).Status != "disabled" {
		t.Fatal("missing operator policy granted an implicit budget")
	}
}

// Build a real causal baseline and incident without relying on wall-clock waits.
func monitorSteeringTestIncident(t testing.TB) (monitorValidatorPolicy, *monitorValidatorState) {
	t.Helper()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := monitorSteeringTestRecord(now, now.Add(-time.Hour), now, 1)
	policy := monitorValidatorPolicy{Role: "alpha", ExpectedSource: value.Source,
		SteeringLiveness: &monitorSteeringLivenessPolicy{WarningAfterSeconds: 120, CriticalAfterSeconds: 300}}
	state := &monitorValidatorState{}
	state.observe(now, now, &value, "ok")
	if err := state.retainSteeringLiveness(policy); err != nil {
		t.Fatal(err)
	}
	value = monitorSteeringTestRecord(now.Add(5*time.Minute), now.Add(-time.Hour), now, 1)
	state.observe(now, now.Add(5*time.Minute), &value, "ok")
	if err := state.retainSteeringLiveness(policy); err != nil || !state.SteeringLiveness.unresolved() {
		t.Fatal("causal incident not retained", err)
	}
	return policy, state
}

// Policy edits, clock rollback and source errors cannot launder a retained cut.
func TestMonitorSteeringLivenessIncidentSurvivesUncertaintyAndPolicyChange(t *testing.T) {
	for _, change := range []string{"missing", "invalid", "clock", "future", "policy-removed", "policy-relaxed", "missing-steering"} {
		policy, state := monitorSteeringTestIncident(t)
		id := state.SteeringLiveness.LastIncident.Id
		now := state.SampleAt.Add(time.Second)
		value := monitorSteeringTestRecord(now, monitorProgressTime(state.Record.StartedAt), now, 1)
		switch change {
		case "missing", "invalid":
			state.observe(now, now, nil, change)
		case "clock":
			state.observe(now, now.Add(-time.Minute), &value, "ok")
		case "future":
			value.Steering.ObservedAt, value.Steering.LastSuccessAt = now.Add(time.Second).Format(time.RFC3339Nano), ""
			value.Steering.Current = false
			state.observe(now, now, &value, "ok")
		case "policy-removed":
			policy.SteeringLiveness = nil
			state.observe(now, now, &value, "ok")
		case "policy-relaxed":
			policy.SteeringLiveness = &monitorSteeringLivenessPolicy{WarningAfterSeconds: 600, CriticalAfterSeconds: 900}
			value.Steering = state.Record.Steering
			state.observe(now, now, &value, "ok")
		case "missing-steering":
			value.Steering = nil
			state.observe(now, now, &value, "ok")
		}
		if err := state.retainSteeringLiveness(policy); err != nil || !state.SteeringLiveness.unresolved() || state.SteeringLiveness.LastIncident.Id != id {
			t.Fatal("uncertainty or policy change resolved the incident", change, err)
		}
		_, severity := state.condition(state.SampleAt, policy)
		if severity != "critical" {
			t.Fatal("unresolved incident lost severity", change, severity)
		}
	}
}

// Missing output from an already observed loop ages its retained baseline.
func TestMonitorSteeringLivenessMissingOutcomeAfterBaseline(t *testing.T) {
	for _, change := range []string{"missing", "starting"} {
		policy, state := monitorSteeringTestIncident(t)
		state.SteeringLiveness.LastIncident = nil
		state.SteeringLiveness.Incidents, state.SteeringLiveness.FirstDetectedAt = 0, time.Time{}
		if change == "missing" {
			state.Record.Steering = nil
		} else {
			state.Record.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: state.Record.HeartbeatAt, Outcome: "starting"}
		}
		if err := state.retainSteeringLiveness(policy); err != nil || !state.SteeringLiveness.unresolved() {
			t.Fatal("absent loop completion erased an observed baseline", change, err)
		}
		if err := state.SteeringLiveness.validate(policy, state.HighWaterAt); err != nil {
			t.Fatal("retained absence failed semantic validation", change, err)
		}
	}
}

// An armed process cannot move its startup boundary or rewind its last outcome
// while preserving the same instance id. Both are rejected before recovery.
func TestMonitorSteeringLivenessRejectsSameInstanceBoundaryRewrite(t *testing.T) {
	for _, change := range []string{"start", "outcome"} {
		policy, state := monitorSteeringTestIncident(t)
		now := state.SampleAt.Add(time.Second)
		value := monitorSteeringTestRecord(now, monitorProgressTime(state.Record.StartedAt), now, 1)
		want := "invalid"
		if change == "start" {
			value.StartedAt = now.Format(time.RFC3339Nano)
		} else {
			value.Steering.ObservedAt, value.Steering.LastSuccessAt = monitorProgressTime(state.Record.Steering.ObservedAt).Add(-time.Second).Format(time.RFC3339Nano), ""
			value.Steering.Current = false
			want = "clock"
		}
		state.observe(now, now, &value, "ok")
		if err := state.retainSteeringLiveness(policy); err != nil || state.ReadStatus != want || state.readCurrent || !state.SteeringLiveness.unresolved() {
			t.Fatal("same-instance boundary rewrite admitted", change, state, err)
		}
	}
}

// A fresh replacement can carry a completion from before the detection cut.
// Only a real returned outcome after that cut resolves this particular episode.
func TestMonitorSteeringLivenessRecoveryRequiresLaterOutcome(t *testing.T) {
	policy, state := monitorSteeringTestIncident(t)
	detected := state.SampleAt
	now := detected.Add(time.Second)
	value := monitorSteeringTestRecord(now, detected.Add(-10*time.Second), detected.Add(-time.Second), 1)
	value.InstanceId = strings.Repeat("3", 32)
	state.observe(now, now, &value, "ok")
	if err := state.retainSteeringLiveness(policy); err != nil || !state.SteeringLiveness.unresolved() {
		t.Fatal("a completion before detection resolved the retained incident", err)
	}
	value.Steering.ObservedAt, value.Steering.LastSuccessAt = now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)
	state.observe(now, now, &value, "ok")
	if err := state.retainSteeringLiveness(policy); err != nil || state.SteeringLiveness.unresolved() {
		t.Fatal("real later outcome did not resolve responsiveness", err)
	}
}

// Rehashed checkpoint corruption still cannot fabricate recovery or an episode.
func TestMonitorSteeringLivenessCheckpointSemanticRefusalAndLegacy(t *testing.T) {
	fixture := monitorSteeringTestPolicy(t, "alpha")
	policy, original := monitorSteeringTestIncident(t)
	policy.ProgressFile = fixture.policy.Validators[0].ProgressFile
	checkpoint, err := openMonitorServiceCheckpoint(fixture.checkpointPath, identityExpectation{NativeChain: "fixture-mainnet", EvmChainId: 964, GenesisHash: testGenesisHash}, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.owner.close()
	if err := checkpoint.save(original); err != nil {
		t.Fatal(err)
	}
	raw, record := monitorReadTestCheckpoint(t, fixture.checkpointPath)
	if len(raw) > maxMonitorServiceCheckpointBytes {
		t.Fatal("incident exceeds checkpoint bound")
	}
	for _, change := range []string{"budget", "baseline", "failure", "recovery", "role", "schema"} {
		var candidate monitorServiceCheckpointRecord
		if err := json.Unmarshal(raw, &candidate); err != nil {
			t.Fatal(err)
		}
		incident := candidate.State.SteeringLiveness.LastIncident
		switch change {
		case "budget":
			incident.Policy.CriticalAfterSeconds++
		case "baseline":
			incident.Baseline.Steering = nil
		case "failure":
			incident.Failure.Steering.ObservedAt = incident.Failure.HeartbeatAt
		case "recovery":
			incident.Recovery = &incident.Baseline
		case "role":
			candidate.Role = "beta"
		case "schema":
			candidate.Schema = "urnetwork-mainnet-validator-checkpoint-v3"
		}
		monitorReadTestWriteCheckpoint(t, fixture.checkpointPath, candidate)
		if loaded, err := checkpoint.load(t.Context()); err == nil || loaded != nil {
			t.Fatal("rehashed invalid liveness evidence admitted", change)
		}
	}
	record.Schema, record.State.SteeringLiveness = "urnetwork-mainnet-validator-checkpoint-v3", nil
	monitorReadTestWriteCheckpoint(t, fixture.checkpointPath, record)
	loaded, err := checkpoint.load(t.Context())
	if err != nil || loaded.SteeringLiveness != nil || loaded.readCurrent {
		t.Fatal("legacy checkpoint fabricated a live baseline", loaded, err)
	}
}

// The actual strict service policy loader rejects malformed or unbounded margins.
func TestMonitorSteeringLivenessPolicyBounds(t *testing.T) {
	fixture := monitorSteeringTestPolicy(t, "alpha")
	for _, budget := range []monitorSteeringLivenessPolicy{
		{WarningAfterSeconds: 0, CriticalAfterSeconds: 300}, {WarningAfterSeconds: 89, CriticalAfterSeconds: 300},
		{WarningAfterSeconds: 120, CriticalAfterSeconds: 120}, {WarningAfterSeconds: 120, CriticalAfterSeconds: 86401},
	} {
		fixture.policy.Validators[0].SteeringLiveness = &budget
		fixture.writePolicy(t)
		if policy, err := loadMonitorServices(t.Context(), fixture.policyPath, identityExpectation{EvmChainId: 964, GenesisHash: testGenesisHash}, fixture.checkpointPath, fixture.metricsPath); err == nil || policy != nil {
			t.Fatal("unbounded steering policy admitted", budget)
		}
	}
}

// A stall supplies diagnostics only. Neither the existing independent repair
// signature nor a stalled monitor record grants authority to stop an active unit.
func TestMonitorSteeringLivenessDoesNotAuthorizeRepair(t *testing.T) {
	fixture := newRepairValidatorFixture(t)
	fixture.claim()
	fixture.manager["MainPID"], fixture.manager["ActiveState"], fixture.manager["SubState"] = "71", "active", "running"
	result, exit, detail := fixture.command("resume")
	if exit == 0 || fixture.starts != 0 || result.StartConsumed || result.Status != "generation-changed" {
		t.Fatal("active generation acquired start authority", result, exit, detail)
	}
	policy, state := monitorSteeringTestIncident(t)
	if err := state.retainReadIncident(policy); err != nil {
		t.Fatal(err)
	}
	plan := fixture.approval.Plan
	plan.Role, plan.Source = policy.Role, policy.ExpectedSource
	record := monitorServiceCheckpointRecord{Schema: monitorServiceCheckpointSchema, Role: policy.Role, Expected: policy.ExpectedSource, State: *state}
	record.ContentHash, _ = hashMonitorServiceCheckpoint(record)
	plan.IncidentId = state.SteeringLiveness.LastIncident.Id
	if plan.incident(record) == nil {
		t.Fatal("readable steering stall became a stopped availability incident")
	}
}

// Existing independently signed v3 incidents keep their original stopped-only
// scope; the new checkpoint schema does not invalidate prior repair custody.
func TestMonitorSteeringLivenessPreservesSignedLegacyRepair(t *testing.T) {
	fixture := newRepairValidatorFixture(t)
	original := &fixture.approval.Plan.Original
	original.Schema = "urnetwork-mainnet-validator-checkpoint-v3"
	original.ContentHash, _ = hashMonitorServiceCheckpoint(*original)
	fixture.sign()
	monitorReadTestWriteCheckpoint(t, fixture.approval.Plan.MonitorCheckpoint, *original)
	fixture.claim()
	result, exit, detail := fixture.command("resume")
	if exit != 0 || fixture.starts != 1 || result.Completed == nil {
		t.Fatal("original signed v3 stopped incident lost its authority", result, exit, detail)
	}
}

// Physical producer files, command workers and cold checkpoints exercise the
// native deadline observer. Synthetic observations carry no signing authority.
package main

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The timestamps reflect the actual production ordering: retained intent read,
// canonical receipt/schedule read, then a completed receipt-pending loop result.
func monitorDeadlineTestRecord(now time.Time, id, block, epoch uint64) protocol.ValidatorProgress {
	value := monitorServicesTestRecord(now, id)
	created := "2026-01-01T00:00:00Z"
	value.Intent.ObservedAt = now.Add(-3 * time.Second).Format(time.RFC3339Nano)
	value.Intent.LastSuccessAt = value.Intent.ObservedAt
	value.Intent.Value = &protocol.ValidatorIntentProgress{ConfigHash: value.Source.ConfigHash, VectorHash: "0x" + strings.Repeat("3", 64), NativeEpoch: 8, SettlementEpoch: 7,
		Status: "pending", CreatedAt: created, ProgressAt: created, PreparedAtBlock: 110, RevealBlock: 500}
	value.Native.ObservedAt = now.Add(-2 * time.Second).Format(time.RFC3339Nano)
	value.Native.LastSuccessAt = value.Native.ObservedAt
	value.Native.Block, value.Native.Epoch, value.Native.Tempo = block, epoch, 100
	value.Native.LastEpochBlock = 100 + (epoch-8)*100
	value.Native.BlocksSinceLastStep = block - value.Native.LastEpochBlock
	stamp := now.Add(-time.Second).Format(time.RFC3339Nano)
	value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: stamp, LastSuccessAt: stamp, Current: true, EpochKnown: true, NativeEpoch: epoch, Outcome: "receipt_pending"}
	return value
}

// A nonzero explicit measured margin enables only this role's inference.
func (self *monitorServicesFixture) enableNativeDeadline(t *testing.T) {
	t.Helper()
	for index := range self.policy.Validators {
		self.policy.Validators[index].NativeDeadline = &monitorNativeDeadlinePolicy{CompletionMarginBlocks: 5}
	}
	self.writePolicy(t)
}

// Exact block thresholds, deferral and the eventual observed epoch crossing
// travel through the actual monitor command despite its blocked chain worker.
func TestMonitorNativeDeadlineCommandForecastsAndCrosses(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	value := monitorDeadlineTestRecord(fixture.clock.now(), 1, 159, 8)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	for index, c := range []struct {
		block     uint64
		epoch     uint64
		status    string
		remaining uint64
		severity  string
	}{
		{block: 159, epoch: 8, status: "pending", remaining: 41},
		{block: 180, epoch: 8, status: "warning", remaining: 20, severity: "warning"},
		{block: 195, epoch: 8, status: "critical", remaining: 5, severity: "critical"},
		{block: 200, epoch: 8, status: "critical", remaining: 1, severity: "critical"},
		{block: 201, epoch: 9, status: "missed-window", severity: "critical"},
		{block: 202, epoch: 9, status: "missed-window", severity: "critical"},
	} {
		if index != 0 {
			fixture.clock.seconds.Add(10)
			value = monitorDeadlineTestRecord(fixture.clock.now(), 1, c.block, c.epoch)
			monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
			run.again(t, "alpha")
		}
		event := run.next(t)
		if event.NativeDeadline == nil || event.NativeDeadline.Status != c.status || !event.NativeDeadline.Known || !event.NativeDeadline.Current || event.NativeDeadline.BlocksRemaining != c.remaining || event.Severity != c.severity {
			t.Fatalf("native deadline boundary %d/%d was not observed: %+v", c.block, c.epoch, event)
		}
		_, metricsPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
		metrics := monitorServicesGauges(t, metricsPath, "alpha")
		if metrics["protocol_deadline_known"] != 1 || metrics["native_deadline_blocks_remaining"] != float64(c.remaining) || metrics["native_deadline_status"] != float64(monitorNativeDeadlineStatusCodes[c.status]) {
			t.Fatal("native deadline command omitted its actual metric boundary", metrics)
		}
		if c.epoch == 8 && (event.State.NativeDeadline != nil || metrics["native_deadline_unresolved"] != 0) {
			t.Fatal("a projected or deferred boundary invented a missed epoch")
		}
		if c.epoch == 9 && (event.State.NativeDeadline == nil || event.State.NativeDeadline.MissedWindows != 1 || metrics["native_deadline_unresolved"] != 1) {
			t.Fatal("actual epoch crossing was not retained exactly once")
		}
	}
}

// Missing schedules, stale/failing receipt observations and mixed loop samples
// cannot create a historical incident merely because numeric epochs differ.
func TestMonitorNativeDeadlineCommandUnavailableCannotInventMiss(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	url, _, _ := monitorServicesBlockedChain(t)
	var run *monitorServicesTestRun
	for _, c := range []struct {
		name   string
		change func(*protocol.ValidatorProgress)
		status string
	}{
		{name: "missing schedule", change: func(v *protocol.ValidatorProgress) { v.Native = nil }, status: "unknown"},
		{name: "missing steering", change: func(v *protocol.ValidatorProgress) { v.Steering = nil }, status: "unknown"},
		{name: "receipt transport", change: func(v *protocol.ValidatorProgress) {
			v.Steering.Current = false
			v.Steering.Outcome = "receipt_transport_wait"
		}, status: "unavailable"},
		{name: "native unavailable", change: func(v *protocol.ValidatorProgress) { v.Native.Current = false }, status: "unavailable"},
		{name: "intent unavailable", change: func(v *protocol.ValidatorProgress) { v.Intent.Current = false }, status: "unavailable"},
		{name: "stale native success", change: func(v *protocol.ValidatorProgress) {
			v.Native.LastSuccessAt = fixture.clock.now().Add(-90 * time.Second).Format(time.RFC3339Nano)
		}, status: "unavailable"},
		{name: "stale heartbeat", change: func(v *protocol.ValidatorProgress) {
			v.HeartbeatAt = fixture.clock.now().Add(-90 * time.Second).Format(time.RFC3339Nano)
		}, status: "unavailable"},
		{name: "preceding loop", change: func(v *protocol.ValidatorProgress) {
			v.Steering.ObservedAt = v.Intent.ObservedAt
			v.Steering.LastSuccessAt = v.Steering.ObservedAt
		}, status: "incoherent"},
		{name: "equal schedule time", change: func(v *protocol.ValidatorProgress) {
			v.Steering.ObservedAt = v.Native.ObservedAt
			v.Steering.LastSuccessAt = v.Steering.ObservedAt
		}, status: "incoherent"},
		{name: "other epoch", change: func(v *protocol.ValidatorProgress) { v.Steering.NativeEpoch++ }, status: "incoherent"},
		{name: "unknown epoch", change: func(v *protocol.ValidatorProgress) { v.Steering.EpochKnown = false; v.Steering.NativeEpoch = 0 }, status: "incoherent"},
		{name: "wrong source", change: func(v *protocol.ValidatorProgress) { v.Source.ConfigHash = "sha256:" + strings.Repeat("9", 64) }, status: "unavailable"},
	} {
		fixture.clock.seconds.Add(10)
		value := monitorDeadlineTestRecord(fixture.clock.now(), 1, 201, 9)
		c.change(&value)
		monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
		if run == nil {
			run = fixture.start(t, url, monitorServiceHooks{})
		} else {
			run.again(t, "alpha")
		}
		event := run.next(t)
		if event.NativeDeadline == nil || event.NativeDeadline.Status != c.status || event.NativeDeadline.Known || event.State.NativeDeadline != nil {
			t.Fatalf("%s fabricated a missed window: %+v", c.name, event)
		}
	}
	fixture.clock.seconds.Add(10)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, monitorDeadlineTestRecord(fixture.clock.now(), 1, 201, 9))
	run.again(t, "alpha")
	if event := run.next(t); event.NativeDeadline.Status != "missed-window" || event.State.NativeDeadline == nil {
		t.Fatal("complete ordered receipt evidence never established its reported miss", event)
	}
}

// A fresh heartbeat/intent/loop cannot re-date an older authenticated native
// sample. Its epoch numbers remain retained data until that read succeeds again.
func TestMonitorNativeDeadlineCommandStaleNativeCannotInventMiss(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	value := monitorDeadlineTestRecord(fixture.clock.now(), 1, 201, 9)
	value.Native.ObservedAt = fixture.clock.now().Add(-90 * time.Second).Format(time.RFC3339Nano)
	value.Native.LastSuccessAt = value.Native.ObservedAt
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	event := run.next(t)
	if event.NativeDeadline == nil || event.NativeDeadline.Status != "unavailable" || event.NativeDeadline.Known || event.State.NativeDeadline != nil {
		t.Fatal("stale native evidence fabricated a missed window", event)
	}
	fixture.clock.seconds.Add(10)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, monitorDeadlineTestRecord(fixture.clock.now(), 1, 201, 9))
	run.again(t, "alpha")
	if event := run.next(t); event.State.NativeDeadline == nil || event.NativeDeadline.Status != "missed-window" {
		t.Fatal("fresh native evidence failed to report its actual crossed window", event)
	}
}

// The first miss survives a cold read outage, approved-source renewal, later
// unsigned application reports and disabled future inference. No ack is implied.
func TestMonitorNativeDeadlineCommandRestartRetainsIncident(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	value := monitorDeadlineTestRecord(fixture.clock.now(), 1, 201, 9)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	first := run.next(t)
	if first.State.NativeDeadline == nil {
		t.Fatal("missed window was not retained before restart")
	}
	run.cancel()
	<-run.done
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	fixture.clock.seconds.Add(10)
	restarted := fixture.start(t, url, monitorServiceHooks{})
	event := restarted.next(t)
	if event.NativeDeadline.Status != "unavailable" || event.NativeDeadline.Current || event.State.NativeDeadline == nil || event.State.NativeDeadline.FirstMiss.DetectedAt != first.State.NativeDeadline.FirstMiss.DetectedAt || event.Severity != "critical" {
		t.Fatal("cold missing source erased the missed-window incident", event)
	}
	restarted.cancel()
	<-restarted.done
	fixture.clock.seconds.Add(10)
	value = monitorDeadlineTestRecord(fixture.clock.now(), 1, 520, 12)
	value.Source.ConfigHash = "sha256:" + strings.Repeat("4", 64)
	value.InstanceId = strings.Repeat("5", 32)
	value.Intent.Value.Status, value.Intent.Value.FinalizedBlock, value.Intent.Value.ApplicationBlock = "applied", 210, 520
	value.Intent.Value.ProgressAt = value.Intent.ObservedAt
	value.Steering.Outcome = "complete"
	fixture.policy.Validators[0].ExpectedSource = value.Source
	fixture.writePolicy(t)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	renewed := fixture.start(t, url, monitorServiceHooks{})
	event = renewed.next(t)
	if event.NativeDeadline.Status != "not-pending" || event.NativeDeadline.Known || event.State.NativeDeadline == nil || event.State.NativeDeadline.MissedWindows != 1 || event.Severity != "critical" || event.State.NativeDeadline.FirstMiss.Intent.Value.ConfigHash == value.Source.ConfigHash {
		t.Fatal("late unsigned application or config renewal cleared historical evidence", event)
	}
	renewed.cancel()
	<-renewed.done
	fixture.policy.Validators[0].NativeDeadline = nil
	fixture.writePolicy(t)
	disabled := fixture.start(t, url, monitorServiceHooks{})
	event = disabled.next(t)
	if event.NativeDeadline == nil || event.NativeDeadline.Status != "disabled" || event.State.NativeDeadline == nil || event.Severity != "critical" {
		t.Fatal("disabling new inference cleared a retained incident", event)
	}
}

// A fixed first/last census retains repeated historical failures without
// multiplying incidents on every successful read of the same pending liability.
func TestMonitorNativeDeadlineCommandRetainsFirstAndLastMisses(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, monitorDeadlineTestRecord(fixture.clock.now(), 1, 201, 9))
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	first := run.next(t)
	if first.State.NativeDeadline == nil {
		t.Fatal("first incident absent")
	}
	// Native epoch order remains authoritative within the existing consumer
	// clock allowance; original detection times are retained without rewriting.
	fixture.clock.seconds.Add(-1)
	value := monitorDeadlineTestRecord(fixture.clock.now(), 1, 301, 10)
	value.Intent.Value.NativeEpoch, value.Intent.Value.PreparedAtBlock = 9, 210
	value.Intent.Value.VectorHash = "0x" + strings.Repeat("6", 64)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	run.again(t, "alpha")
	event := run.next(t)
	if event.State.NativeDeadline == nil || event.State.NativeDeadline.MissedWindows != 2 || event.State.NativeDeadline.FirstMiss.Intent.Value.NativeEpoch != 8 || event.State.NativeDeadline.LastMiss.Intent.Value.NativeEpoch != 9 || event.State.NativeDeadline.FirstMiss.DetectedAt != first.State.NativeDeadline.FirstMiss.DetectedAt {
		t.Fatal("later missed window replaced first evidence or lost its bounded count", event)
	}
	run.cancel()
	<-run.done
	restarted := fixture.start(t, url, monitorServiceHooks{})
	if event := restarted.next(t); event.State.NativeDeadline == nil || event.State.NativeDeadline.MissedWindows != 2 {
		t.Fatal("cold replay counted the same incident twice", event)
	}
}

// Atomic rename may precede an ambiguous directory sync. Cold restart retains
// the visible incident, but a failed durable checkpoint never confirms export.
func TestMonitorNativeDeadlineCommandAmbiguousCheckpointRetainsIncident(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, monitorDeadlineTestRecord(fixture.clock.now(), 1, 201, 9))
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{syncDirectory: func(role, kind string, _ *os.File) error {
		if role == "alpha" && kind == "checkpoint" {
			return errors.New("synthetic checkpoint sync failure")
		}
		return nil
	}})
	event := run.next(t)
	_, metricsPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, "alpha")
	metrics := monitorServicesGauges(t, metricsPath, "alpha")
	if event.State.NativeDeadline == nil || event.Publication != "retrying" || metrics["checkpoint_current"] != 0 || metrics["export_last_success_timestamp_seconds"] != 0 || metrics["native_deadline_unresolved"] != 1 {
		t.Fatal("ambiguous incident checkpoint claimed acknowledged success", event, metrics)
	}
	run.cancel()
	<-run.done
	if err := os.Remove(fixture.policy.Validators[0].ProgressFile); err != nil {
		t.Fatal(err)
	}
	restarted := fixture.start(t, url, monitorServiceHooks{})
	if event := restarted.next(t); event.State.NativeDeadline == nil || event.Severity != "critical" {
		t.Fatal("cold recovery lost the renamed incident after ambiguous sync", event)
	}
}

// One role's past miss must not become its peer's failure, even with one shared
// blocked RPC and exporter. Each actual metrics file retains its own result.
func TestMonitorNativeDeadlineCommandKeepsRoleIncidentsSeparate(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha", "beta")
	fixture.enableNativeDeadline(t)
	for index, policy := range fixture.policy.Validators {
		block, epoch := uint64(201), uint64(9)
		if policy.Role == "beta" {
			block, epoch = 159, 8
		}
		monitorServicesTestWrite(t, policy.ProgressFile, monitorDeadlineTestRecord(fixture.clock.now(), uint64(index+1), block, epoch))
	}
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	events := map[string]monitorServiceEvent{}
	for range 2 {
		event := run.next(t)
		events[event.Role] = event
	}
	if events["alpha"].State.NativeDeadline == nil || events["alpha"].Severity != "critical" || events["beta"].State.NativeDeadline != nil || events["beta"].Severity != "" || events["beta"].NativeDeadline.Status != "pending" {
		t.Fatal("a role's native incident crossed its independent owner", events)
	}
	for _, role := range []string{"alpha", "beta"} {
		_, metricsPath := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, role)
		metrics := monitorServicesGauges(t, metricsPath, role)
		want := float64(0)
		if role == "alpha" {
			want = 1
		}
		if metrics["native_deadline_unresolved"] != want {
			t.Fatal("native incident routed to wrong role", role, metrics)
		}
	}
}

// Source scheduler rules cover owner triggers, normal cadence, strict safety
// rollover, deferred epochs and bounded arithmetic without a simulation loop.
func TestMonitorNativeDeadlineForecastUsesNativeScheduleRules(t *testing.T) {
	for _, c := range []struct {
		name   string
		native protocol.ValidatorNativeObservation
		want   uint64
		known  bool
	}{
		{name: "tempo", native: protocol.ValidatorNativeObservation{Block: 180, LastEpochBlock: 100, Tempo: 100, BlocksSinceLastStep: 80}, want: 200, known: true},
		{name: "owner earlier", native: protocol.ValidatorNativeObservation{Block: 180, LastEpochBlock: 100, Tempo: 100, PendingEpochAt: 190, BlocksSinceLastStep: 80}, want: 190, known: true},
		{name: "owner later", native: protocol.ValidatorNativeObservation{Block: 180, LastEpochBlock: 100, Tempo: 100, PendingEpochAt: 210, BlocksSinceLastStep: 80}, want: 200, known: true},
		{name: "deferred", native: protocol.ValidatorNativeObservation{Block: 202, LastEpochBlock: 100, Tempo: 100, BlocksSinceLastStep: 102}, want: 203, known: true},
		{name: "safety exact", native: protocol.ValidatorNativeObservation{Block: 60_000, LastEpochBlock: 59_900, Tempo: 1000, BlocksSinceLastStep: crv4.MaxTempo}, want: 60_001, known: true},
		{name: "safety increment", native: protocol.ValidatorNativeObservation{Block: 60_000, LastEpochBlock: 59_900, Tempo: 1000, BlocksSinceLastStep: crv4.MaxTempo - 1}, want: 60_002, known: true},
		{name: "disabled tempo", native: protocol.ValidatorNativeObservation{Block: 180, LastEpochBlock: 100}},
		{name: "future last", native: protocol.ValidatorNativeObservation{Block: 180, LastEpochBlock: 181, Tempo: 100}},
		{name: "invalid counter", native: protocol.ValidatorNativeObservation{Block: 180, Tempo: 100, BlocksSinceLastStep: 181}},
		{name: "height overflow", native: protocol.ValidatorNativeObservation{Block: math.MaxUint32, Tempo: 100}},
		{name: "boundary overflow", native: protocol.ValidatorNativeObservation{Block: math.MaxUint32 - 1, LastEpochBlock: math.MaxUint32 - 1, Tempo: 100}},
	} {
		got, known := monitorNativeEpochBoundary(c.native)
		if known != c.known || known && got != c.want {
			t.Errorf("%s forecast = %d/%t, want %d/%t", c.name, got, known, c.want, c.known)
		}
	}
}

// Malformed configured margins are rejected before a progress read or output
// acquisition; no implicit or overflowed margin enables inference.
func TestMonitorNativeDeadlinePolicyRequiresBoundedExplicitMargin(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	for _, margin := range []uint64{0, crv4.MaxTempo + 1, math.MaxUint64} {
		fixture.policy.Validators[0].NativeDeadline = &monitorNativeDeadlinePolicy{CompletionMarginBlocks: margin}
		fixture.writePolicy(t)
		if _, err := loadMonitorServices(t.Context(), fixture.policyPath, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath); err == nil {
			t.Fatal("invalid native completion margin admitted", margin)
		}
	}
	if _, err := os.Stat(fixture.checkpointPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("policy refusal created an output", err)
	}
}

// Legacy observation records upgrade without inventing a missed interval. A
// checksum-valid v1 carrying new incident data cannot disguise a schema rollback.
func TestMonitorNativeDeadlineCheckpointMigratesLegacyAndRejectsDowngrade(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	policy := fixture.policy.Validators[0]
	path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	owner, err := openMonitorServiceCheckpoint(path, monitorTestExpectation(), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.owner.close() })
	now := fixture.clock.now()
	value := monitorDeadlineTestRecord(now, 1, 201, 9)
	state := &monitorValidatorState{}
	state.observe(now, now, &value, "ok")
	record := monitorServiceCheckpointRecord{Schema: "urnetwork-mainnet-validator-checkpoint-v1", Role: policy.Role, Expected: policy.ExpectedSource, State: *state}
	write := func() {
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
	write()
	loaded, err := owner.load(t.Context())
	if err != nil || loaded.NativeDeadline != nil || loaded.readCurrent {
		t.Fatal("legacy migration fabricated current deadline evidence", err)
	}
	if err := owner.save(loaded); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), monitorServiceCheckpointSchema) {
		t.Fatal("legacy checkpoint was not upgraded", err)
	}
	state.retainNativeDeadline(policy, now)
	if state.NativeDeadline == nil {
		t.Fatal("fixture lacked actual missed-window evidence")
	}
	record.State = *state
	write()
	if _, err := owner.load(t.Context()); err == nil {
		t.Fatal("legacy checkpoint accepted a hidden deadline incident")
	}
	record.Schema = monitorServiceCheckpointSchema
	base := *state.NativeDeadline
	for _, c := range []struct {
		name   string
		change func(*monitorNativeDeadlineHistory)
		want   string
	}{
		{name: "no crossing", change: func(h *monitorNativeDeadlineHistory) { h.FirstMiss.Native.Epoch = 8 }, want: "observed epoch crossing"},
		{name: "regressed epoch", change: func(h *monitorNativeDeadlineHistory) { h.MissedWindows = 2; h.LastMiss.Intent.Value.NativeEpoch = 7 }, want: "history is inconsistent"},
		{name: "regressed clock outside allowance", change: func(h *monitorNativeDeadlineHistory) {
			older := now.Add(-monitorServiceClockAllowance - time.Second)
			value := monitorDeadlineTestRecord(older, 1, 301, 10)
			value.Intent.Value.NativeEpoch, value.Intent.Value.PreparedAtBlock = 9, 210
			h.MissedWindows = 2
			h.LastMiss = monitorNativeDeadlineIncident{DetectedAt: older, Intent: *value.Intent, Native: *value.Native, Steering: *value.Steering}
		}, want: "history is inconsistent"},
	} {
		history := base
		first, last := *base.FirstMiss.Intent.Value, *base.LastMiss.Intent.Value
		history.FirstMiss.Intent.Value, history.LastMiss.Intent.Value = &first, &last
		c.change(&history)
		record.State.NativeDeadline = &history
		write()
		if _, err := owner.load(t.Context()); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatal("checksum-valid incident chronology was not rejected at its actual predicate", c.name, err)
		}
	}
}

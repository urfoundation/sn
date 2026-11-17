// An explicitly reviewed scheduler profile reaches the real monitor command;
// a forecast remains distinct from an observed epoch crossing or chain success.
package main

import (
	"encoding/json"
	"testing"
)

// Source-bound profile selection must warn at the actual next possible epoch,
// even when the cycle anchor was just reset and still suggests 100 blocks away.
func TestMonitorTempoDriftCommandUsesApprovedProfile(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	policy := fixture.policy.Validators[0].NativeDeadline
	if err := json.Unmarshal([]byte(`{"epoch_schedule_profile":"urnetwork-subtensor-tempo-drift-v1"}`), policy); err != nil {
		t.Fatal(err)
	}
	fixture.writePolicy(t)
	value := monitorDeadlineTestRecord(fixture.clock.now(), 1, 180, 8)
	value.Native.LastEpochBlock, value.Native.BlocksSinceLastStep = 180, 100
	monitorServicesTestWrite(t, fixture.policy.Validators[0].ProgressFile, value)
	url, _, _ := monitorServicesBlockedChain(t)
	run := fixture.start(t, url, monitorServiceHooks{})
	event := run.next(t)
	if event.NativeDeadline == nil || !event.NativeDeadline.Known || !event.NativeDeadline.Current || event.NativeDeadline.ProjectedBoundaryBlock != 181 || event.NativeDeadline.BlocksRemaining != 1 || event.NativeDeadline.Status != "critical" || event.State.NativeDeadline != nil {
		t.Fatalf("approved tempo drift forecast hid imminent epoch or invented miss: deadline=%+v state=%+v", event.NativeDeadline, event.State.NativeDeadline)
	}
}

// A root-set tempo is valid native state even above the owner setter cap. Its
// observed missed epoch must survive the actual checkpoint publication/reopen.
func TestMonitorTempoDriftRootTempoIncidentSurvivesCheckpoint(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	fixture.enableNativeDeadline(t)
	policy := fixture.policy.Validators[0]
	if err := json.Unmarshal([]byte(`{"epoch_schedule_profile":"urnetwork-subtensor-tempo-drift-v1"}`), policy.NativeDeadline); err != nil {
		t.Fatal(err)
	}
	now := fixture.clock.now()
	value := monitorDeadlineTestRecord(now, 1, 70001, 9)
	value.Native.Tempo, value.Native.LastEpochBlock, value.Native.BlocksSinceLastStep = 65535, 70000, 1
	state := &monitorValidatorState{}
	state.observe(now, now, &value, "ok")
	state.retainNativeDeadline(policy, now)
	if state.NativeDeadline == nil || state.NativeDeadline.MissedWindows != 1 {
		t.Fatal("root-set tempo hid an observed missed epoch")
	}
	path, _ := monitorValidatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	owner, err := openMonitorServiceCheckpoint(path, monitorTestExpectation(), policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.owner.close() })
	if err := owner.save(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := owner.load(t.Context())
	if err != nil || loaded.NativeDeadline == nil || loaded.NativeDeadline.MissedWindows != 1 || loaded.NativeDeadline.FirstMiss.Native != *value.Native || loaded.readCurrent {
		t.Fatalf("root-set tempo incident could not be recovered exactly: %v", err)
	}
}

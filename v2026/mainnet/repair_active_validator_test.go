// Synthetic signed host fixtures force each irreversible custody cut. No test
// addresses a production identity, executable, service manager or signing key.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Files, signatures and custody are real; only manager observations/effects and
// clocks are injected. Each fake command returns after its effect is complete.
type repairActiveValidatorFixture struct {
	*repairValidatorFixture
	active            repairActiveValidatorApproval
	stops             int
	stopError         error
	stopCompletes     bool
	keepDescendant    bool
	stopProperties    map[string]string
	prerequisiteStops bool
	stopHook          func(context.Context) error
}

// A responsive baseline followed by unchanged outcomes creates the actual v4
// monitor incident while heartbeat, publication and other domain reads advance.
func newRepairActiveValidatorFixture(t *testing.T) *repairActiveValidatorFixture {
	t.Helper()
	base := newRepairValidatorFixture(t)
	p := base.approval.Plan
	baseline := base.now.Add(-5 * time.Minute)
	policy := monitorValidatorPolicy{Role: p.Role, ProgressFile: p.Unit.ProgressFile, ExpectedSource: p.Source, SteeringLiveness: &monitorSteeringLivenessPolicy{WarningAfterSeconds: 120, CriticalAfterSeconds: 300}}
	value := monitorSteeringTestRecord(baseline, base.now.Add(-time.Hour), baseline, 71)
	var state monitorValidatorState
	state.observe(baseline, baseline, &value, "ok")
	if err := errors.Join(state.retainReadIncident(policy), state.retainSteeringLiveness(policy)); err != nil {
		t.Fatal(err)
	}
	value = monitorSteeringTestRecord(base.now, base.now.Add(-time.Hour), baseline, 71)
	state.observe(baseline, base.now, &value, "ok")
	if err := errors.Join(state.retainReadIncident(policy), state.retainSteeringLiveness(policy)); err != nil {
		t.Fatal(err)
	}
	p.Original = monitorServiceCheckpointRecord{Schema: monitorServiceCheckpointSchema, Role: p.Role, Expected: p.Source, State: state}
	p.Original.ContentHash, _ = hashMonitorServiceCheckpoint(p.Original)
	p.IncidentId = state.SteeringLiveness.LastIncident.Id
	p.CommandTimeoutSeconds = 35
	policyRaw, err := json.Marshal(monitorServicesPolicy{Schema: monitorServicesSchema, Validators: []monitorValidatorPolicy{policy}})
	if err != nil {
		t.Fatal(err)
	}
	services := planFileReference{Path: filepath.Join(base.directory, "services.json"), Sha256: monitorReadDigest(policyRaw)}
	repairValidatorTestWrite(t, services.Path, policyRaw, 0600)
	self := &repairActiveValidatorFixture{repairValidatorFixture: base, active: repairActiveValidatorApproval{Schema: repairActiveValidatorSchema, Plan: repairActiveValidatorPlan{Process: p, MonitorServices: services, MaximumStops: 1, JoinWindowSeconds: 60}}, stopCompletes: true}
	self.stopProperties = map[string]string{"Names": p.Unit.Name, "SendSIGKILL": "yes", "SendSIGHUP": "no", "KillSignal": "15", "FinalKillSignal": "9", "TimeoutStopUSec": "30s", "TimeoutStartUSec": "30s", "TimeoutStopFailureMode": "terminate", "NotifyAccess": "none", "RefuseManualStart": "no", "RefuseManualStop": "no", "CanStart": "yes", "CanStop": "yes", "StopWhenUnneeded": "no"}
	base.host.execute = self.executeActive
	base.manager["ActiveState"], base.manager["SubState"], base.manager["MainPID"] = "active", "running", "71"
	base.progress.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: base.now.Format(time.RFC3339Nano), Outcome: "read_wait"}
	self.writeOldProgress(value)
	self.writeCheckpoint(p.Original)
	self.group(true)
	self.signActive()
	return self
}

// Mutable monitor copies never overwrite the independently signed original.
func (self *repairActiveValidatorFixture) writeCheckpoint(record monitorServiceCheckpointRecord) {
	self.t.Helper()
	record.ContentHash, _ = hashMonitorServiceCheckpoint(record)
	raw, err := json.Marshal(record)
	if err != nil {
		self.t.Fatal(err)
	}
	repairValidatorTestWrite(self.t, self.active.Plan.Process.MonitorCheckpoint, raw, 0600)
}

// Producer files retain their separate uid/gid and strict wire validation.
func (self *repairActiveValidatorFixture) writeOldProgress(value protocol.ValidatorProgress) {
	self.t.Helper()
	raw, err := value.Encode()
	if err != nil {
		self.t.Fatal(err)
	}
	p := self.active.Plan.Process
	repairValidatorTestWrite(self.t, p.Unit.ProgressFile, raw, 0644)
	if os.Getuid() == 0 {
		if err := os.Chown(p.Unit.ProgressFile, int(p.Unit.Uid), int(p.Unit.Gid)); err != nil {
			self.t.Fatal(err)
		}
	}
}

// Descendant population remains independently controllable after main-pid exit.
func (self *repairActiveValidatorFixture) group(populated bool) {
	self.t.Helper()
	path := filepath.Join(self.host.cgroupRoot, "system.slice", self.active.Plan.Process.Unit.Name)
	events, procs := "populated 0\nfrozen 0\n", ""
	if populated {
		events, procs = "populated 1\nfrozen 0\n", "71\n"
	}
	repairValidatorTestWrite(self.t, filepath.Join(path, "cgroup.events"), []byte(events), 0644)
	repairValidatorTestWrite(self.t, filepath.Join(path, "cgroup.procs"), []byte(procs), 0644)
}

// This test signer cannot be invoked by any production command path.
func (self *repairActiveValidatorFixture) signActive() {
	self.t.Helper()
	raw, err := self.active.signingBytes()
	if err != nil {
		self.t.Fatal(err)
	}
	self.active.Signature = hex.EncodeToString(ed25519.Sign(self.privateKey, raw))
	if err := self.active.validate(self.publicKey); err != nil {
		self.t.Fatal("active fixture approval refused", err)
	}
	raw, err = json.Marshal(self.active)
	if err != nil {
		self.t.Fatal(err)
	}
	repairValidatorTestWrite(self.t, self.approvalPath, raw, 0600)
}

// Stop can return uncertainty before or after its simulated effect; the caller
// has no ability to infer that difference from the transport error alone.
func (self *repairActiveValidatorFixture) executeActive(ctx context.Context, path string, args []string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--property=Id,StopWhenUnneeded ") {
		value := "no"
		if self.prerequisiteStops {
			value = "yes"
		}
		return []byte("Id=" + args[len(args)-1] + "\nStopWhenUnneeded=" + value + "\n"), nil
	}
	if strings.Contains(joined, "--property=Names,") {
		var out strings.Builder
		for _, key := range repairActiveValidatorProperties {
			fmt.Fprintf(&out, "%s=%s\n", key, self.stopProperties[key])
		}
		return []byte(out.String()), nil
	}
	if joined == "--system --no-pager --no-ask-password --job-mode=fail stop -- "+self.active.Plan.Process.Unit.Name {
		self.stops++
		if self.stopHook != nil {
			return nil, self.stopHook(ctx)
		}
		if self.stopCompletes {
			self.manager["ActiveState"], self.manager["SubState"], self.manager["MainPID"] = "inactive", "dead", "0"
			if !self.keepDescendant {
				self.group(false)
			} else {
				path := filepath.Join(self.host.cgroupRoot, "system.slice", self.active.Plan.Process.Unit.Name, "cgroup.procs")
				repairValidatorTestWrite(self.t, path, nil, 0644)
			}
		}
		return nil, self.stopError
	}
	return self.repairValidatorFixture.execute(ctx, path, args)
}

// The real CLI's supplied independent key, hash and JSON parsing are exercised.
func (self *repairActiveValidatorFixture) commandActive(operation string) (repairActiveValidatorResult, int, string) {
	self.t.Helper()
	raw, err := os.ReadFile(self.approvalPath)
	if err != nil {
		self.t.Fatal(err)
	}
	args := []string{operation, "--approval", self.approvalPath, "--accept-approval-hash", monitorReadDigest(raw), "--independent-public-key", self.publicKey}
	var stdout, stderr bytes.Buffer
	exit := runRepairActiveValidatorCommandWithHost(self.storage.Context, args, &stdout, &stderr, func() time.Time { return self.now }, self.host)
	var result repairActiveValidatorResult
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			self.t.Fatal(err, stdout.String())
		}
	}
	return result, exit, stderr.String()
}

// A claim creates lifetime ownership without issuing any service command.
func (self *repairActiveValidatorFixture) claimActive() {
	self.t.Helper()
	result, exit, detail := self.commandActive("claim")
	if exit != 0 || result.Status != "claimed" || self.stops != 0 || self.starts != 0 {
		self.t.Fatal("active claim refused", exit, result, detail)
	}
}

// Open the real owner to install deterministic publication-boundary hooks.
func (self *repairActiveValidatorFixture) openActive() *repairActiveValidatorStore {
	self.t.Helper()
	store, err := openRepairActiveValidatorStore(self.storage.Context, self.active, self.publicKey, false, self.now)
	if err != nil {
		self.t.Fatal(err)
	}
	self.t.Cleanup(func() {
		if err := store.close(); err != nil {
			self.t.Error(err)
		}
	})
	return store
}

// The exact independent envelope cannot borrow stopped-repair signatures or
// change a role, release, margin, original incident or command allowance.
func TestRepairActiveValidatorIndependentAuthority(t *testing.T) {
	fixture := newRepairActiveValidatorFixture(t)
	for _, change := range []func(*repairActiveValidatorApproval){
		func(a *repairActiveValidatorApproval) { a.Plan.MaximumStops = 2 },
		func(a *repairActiveValidatorApproval) { a.Plan.JoinWindowSeconds = 0 },
		func(a *repairActiveValidatorApproval) { a.Plan.Process.Previous.Pid++ },
		func(a *repairActiveValidatorApproval) { a.Plan.Process.CommandTimeoutSeconds = 1 },
		func(a *repairActiveValidatorApproval) {
			a.Plan.MonitorServices.Sha256 = monitorReadDigest([]byte("changed"))
		},
		func(a *repairActiveValidatorApproval) { a.Signature = fixture.approval.Signature },
	} {
		a := fixture.active
		change(&a)
		if a.validate(fixture.publicKey) == nil {
			t.Fatal("modified active authority admitted")
		}
	}
	var stdout, stderr bytes.Buffer
	if exit := runMainWithClock(t.Context(), []string{"repair-active-validator", "resume"}, &stdout, &stderr, func() time.Time { return fixture.now }); exit != 2 || fixture.starts != 0 || fixture.stops != 0 {
		t.Fatal("public command acquired implicit authority", exit, stderr.String())
	}
}

// A complete admitted episode consumes one stop and start, keeps original
// evidence unchanged and proves returned steering even when its read failed.
func TestRepairActiveValidatorCommandStopsJoinsAndStartsOnce(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	original := rootObjectHash(f.active.Plan.Process.Original)
	f.claimActive()
	r, exit, detail := f.commandActive("resume")
	if exit != 0 || r.Completed == nil || !r.StopConsumed || !r.StartConsumed || r.Generation == nil || r.JoinedAt.IsZero() || r.ChainSuccessProven || f.stops != 1 || f.starts != 1 {
		t.Fatal("bounded repair did not complete", exit, r, detail)
	}
	r, exit, detail = f.commandActive("resume")
	if exit != 0 || r.Completed == nil || f.stops != 1 || f.starts != 1 {
		t.Fatal("reopen repeated a command", exit, r, detail)
	}
	store := f.openActive()
	record, err := store.load(t.Context())
	if err != nil || rootObjectHash(record.Approval.Plan.Process.Original) != original || record.Completed.RecordHash == "" {
		t.Fatal("original evidence changed", err)
	}
}

// Current direct progress closes the monitor-lag race before the stop boundary.
func TestRepairActiveValidatorRefusesRecoveredOrUnknownSource(t *testing.T) {
	for _, change := range []string{"direct-progress", "recovered-checkpoint", "missing-monitor-read", "unavailable-monitor-read", "stale-publisher", "future-producer", "changed-policy", "changed-generation", "changed-release"} {
		f := newRepairActiveValidatorFixture(t)
		f.claimActive()
		p := f.active.Plan.Process
		value := *p.Original.State.Record
		switch change {
		case "direct-progress":
			value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: f.now.Format(time.RFC3339Nano), Outcome: "read_wait"}
			f.writeOldProgress(value)
		case "recovered-checkpoint":
			f.now = f.now.Add(time.Second)
			value.HeartbeatAt, value.Publisher.LastSuccessAt = f.now.Format(time.RFC3339Nano), f.now.Format(time.RFC3339Nano)
			value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: value.HeartbeatAt, Outcome: "read_wait"}
			current := p.Original
			current.State.observe(f.now, f.now, &value, "ok")
			if err := current.State.retainSteeringLiveness(f.active.Plan.policy()); err != nil {
				t.Fatal(err)
			}
			f.writeCheckpoint(current)
		case "missing-monitor-read", "unavailable-monitor-read":
			record := p.Original
			code := "missing"
			if change == "unavailable-monitor-read" {
				code = "unavailable"
			}
			record.State.observe(f.now, f.now, nil, code)
			if err := record.State.retainReadIncident(f.active.Plan.policy()); err != nil {
				t.Fatal(err)
			}
			f.writeCheckpoint(record)
		case "stale-publisher":
			f.now = f.now.Add(90 * time.Second)
		case "future-producer":
			value.HeartbeatAt = f.now.Add(time.Second).Format(time.RFC3339Nano)
			f.writeOldProgress(value)
		case "changed-policy":
			repairValidatorTestWrite(t, f.active.Plan.MonitorServices.Path, []byte("{}"), 0600)
		case "changed-generation":
			f.manager["InvocationID"] = strings.Repeat("8", 32)
		case "changed-release":
			repairValidatorTestWrite(t, p.Unit.Binary.Path, []byte("changed release"), 0755)
		}
		r, exit, detail := f.commandActive("resume")
		if exit != 3 || r.StopConsumed || f.stops != 0 || f.starts != 0 {
			t.Fatal("unsafe active source admitted", change, exit, r, detail)
		}
	}
}

// Fsync completion does not freeze identity or liveness. Each final recheck
// consumes its reserved allowance without issuing a now-unauthorised command.
func TestRepairActiveValidatorStopReservationRechecksSourceAndCustody(t *testing.T) {
	for _, change := range []string{"progress", "missing-read", "policy", "generation", "expiry", "clock", "claim-loss", "marker-change", "state-loss", "state-tamper"} {
		f := newRepairActiveValidatorFixture(t)
		f.claimActive()
		store := f.openActive()
		changed := false
		store.syncDirectory = func(dir *os.File) error {
			if err := dir.Sync(); err != nil {
				return err
			}
			raw, err := os.ReadFile(store.path)
			if err != nil {
				return err
			}
			var record repairActiveValidatorRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				return err
			}
			if changed || record.Status != "stop-consumed" {
				return nil
			}
			changed = true
			p := f.active.Plan.Process
			switch change {
			case "progress":
				value := *p.Original.State.Record
				value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: f.now.Format(time.RFC3339Nano), Outcome: "read_wait"}
				f.writeOldProgress(value)
			case "missing-read":
				current := p.Original
				current.State.observe(f.now, f.now, nil, "missing")
				if err := current.State.retainReadIncident(f.active.Plan.policy()); err != nil {
					return err
				}
				f.writeCheckpoint(current)
			case "policy":
				repairValidatorTestWrite(t, f.active.Plan.MonitorServices.Path, []byte("{}"), 0600)
			case "generation":
				f.manager["MainPID"] = "74"
			case "expiry":
				f.now = p.ExpiresAt
			case "clock":
				f.now = f.now.Add(-time.Second)
			case "claim-loss":
				return os.Remove(f.active.Plan.claimPath())
			case "marker-change":
				return os.WriteFile(store.path+".lock", []byte("changed\n"), 0600)
			case "state-loss":
				return os.Remove(store.path)
			case "state-tamper":
				return os.WriteFile(store.path, []byte("{}\n"), 0600)
			}
			return nil
		}
		r, err := resumeRepairActiveValidator(t.Context(), store, f.host, func() time.Time { return f.now })
		if !changed || err == nil || !r.StopConsumed || f.stops != 0 || f.starts != 0 {
			t.Fatal("post-reservation guard failed", change, r, err)
		}
	}
}

// Lost stop acknowledgement may later join the same old generation, but a
// reopen never repeats the irreversible stop command.
func TestRepairActiveValidatorLostStopAcknowledgementReconcilesOnce(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	f.stopError = errors.New("synthetic lost stop acknowledgement")
	r, exit, detail := f.commandActive("resume")
	if exit != 3 || r.Status != "waiting-join" || !r.StopConsumed || r.StartConsumed || f.stops != 1 {
		t.Fatal("stop uncertainty not retained", exit, r, detail)
	}
	f.stopError = nil
	r, exit, detail = f.commandActive("resume")
	if exit != 0 || r.Completed == nil || f.stops != 1 || f.starts != 1 {
		t.Fatal("exact stopped generation did not reconcile", exit, r, detail)
	}
}

// An empty main-pid view cannot hide a remaining child signer in its cgroup.
func TestRepairActiveValidatorDescendantJoinIsRequired(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	f.keepDescendant = true
	r, exit, detail := f.commandActive("resume")
	if exit != 3 || r.Status != "waiting-join" || f.stops != 1 || f.starts != 0 {
		t.Fatal("populated descendants admitted", exit, r, detail)
	}
	f.group(false)
	r, exit, detail = f.commandActive("resume")
	if exit != 0 || r.Completed == nil || f.stops != 1 || f.starts != 1 {
		t.Fatal("joined descendants did not resume", exit, r, detail)
	}
}

// A stop that never finishes remains a bounded manual disposition; elapsed
// wall or monotonic limits cannot be reset by another controller process.
func TestRepairActiveValidatorJoinWindowAndObservationLimit(t *testing.T) {
	for _, limit := range []string{"wall", "monotonic", "observation"} {
		f := newRepairActiveValidatorFixture(t)
		if limit == "observation" {
			f.active.Plan.Process.MaximumObservations = 1
			f.signActive()
		}
		f.claimActive()
		f.stopCompletes = false
		if _, exit, detail := f.commandActive("resume"); exit != 3 {
			t.Fatal(exit, detail)
		}
		if limit == "wall" {
			f.now = f.now.Add(61 * time.Second)
		}
		if limit == "monotonic" {
			f.host.monotonic = func() (uint64, error) { return 61000151, nil }
		}
		r, exit, detail := f.commandActive("resume")
		if exit != 3 || f.stops != 1 || f.starts != 0 || limit == "observation" && r.Status != "observation-limit" || limit != "observation" && r.Status != "join-window-closed" {
			t.Fatal("finite join bound escaped", limit, exit, r, detail)
		}
	}
}

// Unacknowledged starts cannot adopt a new invocation or trigger another stop.
func TestRepairActiveValidatorLostStartAcknowledgementNeverRetries(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	f.startError = errors.New("synthetic lost start acknowledgement")
	r, exit, detail := f.commandActive("resume")
	if exit != 3 || !r.StartConsumed || r.Generation != nil || r.Status != "uncertain-consumed-start" {
		t.Fatal("start uncertainty changed", exit, r, detail)
	}
	f.startError = nil
	r, exit, detail = f.commandActive("resume")
	if exit != 3 || r.Generation != nil || f.stops != 1 || f.starts != 1 {
		t.Fatal("unacknowledged effect retried or adopted", exit, r, detail)
	}
}

// Another independent signature and journal cannot renew this old generation's
// allowance, including after the first command fails without changing the unit.
func TestRepairActiveValidatorDuplicateEnvelopeCannotRenewGeneration(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	f.stopCompletes = false
	if _, exit, detail := f.commandActive("resume"); exit != 3 {
		t.Fatal(exit, detail)
	}
	f.active.Plan.Process.StatePath = filepath.Join(f.directory, "another-repair.json")
	f.signActive()
	if _, exit, detail := f.commandActive("claim"); exit != 3 || !strings.Contains(detail, "already claimed") || f.stops != 1 || f.starts != 0 {
		t.Fatal("duplicate envelope obtained custody", exit, detail)
	}
}

// A new publisher alone cannot complete repair; a returned read wait may do so
// while leaving steering success and all economic progress unproven.
func TestRepairActiveValidatorCompletionRequiresActualSteering(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	f.progress.Steering = nil
	r, exit, detail := f.commandActive("resume")
	if exit != 3 || r.Status != "waiting-progress" || r.Generation == nil || r.Completed != nil {
		t.Fatal("heartbeat completed repair", exit, r, detail)
	}
	f.progress.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: f.now.Format(time.RFC3339Nano), Outcome: "read_wait"}
	f.writeOldProgress(f.progress)
	r, exit, detail = f.commandActive("resume")
	if exit != 0 || r.Completed == nil || r.ChainSuccessProven || f.stops != 1 || f.starts != 1 {
		t.Fatal("returned wait did not complete liveness", exit, r, detail)
	}
}

// Every reverse stop edge or loaded kill-profile mismatch is an independent
// refusal, despite exact canonical unit bytes on disk.
func TestRepairActiveValidatorStopPropagationAndKillProfile(t *testing.T) {
	for _, key := range repairActiveValidatorProperties {
		f := newRepairActiveValidatorFixture(t)
		f.stopProperties[key] = "synthetic-difference"
		if _, exit, detail := f.commandActive("claim"); exit != 3 || f.stops != 0 || f.starts != 0 {
			t.Fatal("loaded stop difference admitted", key, exit, detail)
		}
	}
	f := newRepairActiveValidatorFixture(t)
	f.prerequisiteStops = true
	if _, exit, detail := f.commandActive("claim"); exit != 3 || f.stops != 0 || f.starts != 0 {
		t.Fatal("stop-when-unused prerequisite admitted", exit, detail)
	}
}

// Either missing generation artifact rejects both reopen and a fresh envelope;
// journal loss and marker loss separately remain liabilities without capacity.
func TestRepairActiveValidatorLostCustodyCannotRenew(t *testing.T) {
	for _, lost := range []string{"generation", "generation-lock", "journal", "journal-lock"} {
		f := newRepairActiveValidatorFixture(t)
		f.claimActive()
		path := f.active.Plan.claimPath()
		if lost == "generation-lock" {
			path += ".lock"
		}
		if lost == "journal" {
			path = f.active.Plan.Process.StatePath
		}
		if lost == "journal-lock" {
			path = f.active.Plan.Process.StatePath + ".lock"
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if _, exit, detail := f.commandActive("resume"); exit != 3 || f.stops != 0 || f.starts != 0 {
			t.Fatal("missing custody admitted", lost, exit, detail)
		}
		f.active.Plan.Process.StatePath = filepath.Join(f.directory, "replacement.json")
		f.signActive()
		if _, exit, detail := f.commandActive("claim"); exit != 3 || f.stops != 0 || f.starts != 0 {
			t.Fatal("missing custody renewed capacity", lost, exit, detail)
		}
	}
}

// Each ambiguous publication is reopened from its actual retained bytes. A
// reserved but unacknowledged start never executes on the next process.
func TestRepairActiveValidatorAmbiguousPublicationsRetainCuts(t *testing.T) {
	for _, phase := range []string{"stop-consumed", "join-observed", "start-consumed", "waiting-progress"} {
		f := newRepairActiveValidatorFixture(t)
		f.claimActive()
		store := f.openActive()
		injected := false
		store.syncDirectory = func(dir *os.File) error {
			if err := dir.Sync(); err != nil {
				return err
			}
			raw, err := os.ReadFile(store.path)
			if err != nil {
				return err
			}
			var record repairActiveValidatorRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				return err
			}
			if !injected && record.Status == phase {
				injected = true
				return errors.New("synthetic post-rename fsync acknowledgement loss")
			}
			return nil
		}
		r, err := resumeRepairActiveValidator(t.Context(), store, f.host, func() time.Time { return f.now })
		if !injected || err == nil || !r.StopConsumed {
			t.Fatal("publication cut not forced", phase, r, err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		r, exit, detail := f.commandActive("resume")
		if phase == "join-observed" || phase == "waiting-progress" {
			if exit != 0 || r.Completed == nil || f.stops != 1 || f.starts != 1 {
				t.Fatal("retained acknowledged cut did not recover", phase, exit, r, detail)
			}
		} else if exit != 3 || f.starts != 0 || phase == "stop-consumed" && f.stops != 0 || phase == "start-consumed" && f.stops != 1 {
			t.Fatal("ambiguous reservation acquired a retry", phase, exit, r, detail)
		}
	}
}

// Identity and integrity are checked again after the separate start cut, even
// though the old group was observed empty before journal publication.
func TestRepairActiveValidatorStartReservationRechecksOldJoin(t *testing.T) {
	for _, change := range []string{"descendant", "generation", "recovered", "policy", "claim", "expiry"} {
		f := newRepairActiveValidatorFixture(t)
		f.claimActive()
		store := f.openActive()
		changed := false
		store.syncDirectory = func(dir *os.File) error {
			if err := dir.Sync(); err != nil {
				return err
			}
			raw, err := os.ReadFile(store.path)
			if err != nil {
				return err
			}
			var record repairActiveValidatorRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				return err
			}
			if changed || record.Status != "start-consumed" {
				return nil
			}
			changed = true
			switch change {
			case "descendant":
				f.group(true)
			case "generation":
				f.manager["ExecMainPID"] = "74"
			case "recovered":
				value := *f.active.Plan.Process.Original.State.Record
				value.Steering = &protocol.ValidatorSteeringObservation{ObservedAt: f.now.Format(time.RFC3339Nano), Outcome: "read_wait"}
				f.writeOldProgress(value)
			case "policy":
				repairValidatorTestWrite(t, f.active.Plan.MonitorServices.Path, []byte("{}"), 0600)
			case "claim":
				return os.Remove(f.active.Plan.claimPath())
			case "expiry":
				f.now = f.active.Plan.Process.ExpiresAt
			}
			return nil
		}
		r, err := resumeRepairActiveValidator(t.Context(), store, f.host, func() time.Time { return f.now })
		if !changed || err == nil || !r.StartConsumed || r.Generation != nil || f.stops != 1 || f.starts != 0 {
			t.Fatal("post-start-reservation change admitted", change, r, err)
		}
	}
}

// A failed read after this controller stopped the process is expected; its
// retained unresolved steering incident remains exact and never becomes health.
func TestRepairActiveValidatorPostStopAvailabilityLossRetainsIncident(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	f.stopError = errors.New("synthetic lost acknowledgement")
	if _, exit, detail := f.commandActive("resume"); exit != 3 {
		t.Fatal(exit, detail)
	}
	current := f.active.Plan.Process.Original
	current.State.observe(f.now, f.now, nil, "missing")
	if err := current.State.retainReadIncident(f.active.Plan.policy()); err != nil {
		t.Fatal(err)
	}
	f.writeCheckpoint(current)
	if err := os.Remove(f.active.Plan.Process.Unit.ProgressFile); err != nil {
		t.Fatal(err)
	}
	r, exit, detail := f.commandActive("resume")
	if exit != 0 || r.Completed == nil || f.stops != 1 || f.starts != 1 {
		t.Fatal("post-stop outage erased exact incident", exit, r, detail)
	}
}

// Cancellation reaches the owned command and joins it before returning. A
// later owner can observe a stop effect, but cannot issue that command again.
func TestRepairActiveValidatorCancellationJoinsConsumedStop(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	store := f.openActive()
	entered, joined := make(chan struct{}), make(chan struct{})
	f.stopHook = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		f.manager["ActiveState"], f.manager["SubState"], f.manager["MainPID"] = "inactive", "dead", "0"
		f.group(false)
		close(joined)
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan struct{})
	var result repairActiveValidatorResult
	var resultErr error
	go func() {
		result, resultErr = resumeRepairActiveValidator(ctx, store, f.host, func() time.Time { return f.now })
		close(finished)
	}()
	<-entered
	cancel()
	<-finished
	select {
	case <-joined:
	default:
		t.Fatal("controller returned before command joined")
	}
	if !errors.Is(resultErr, context.Canceled) || !result.StopConsumed || result.StartConsumed || f.stops != 1 || f.starts != 0 {
		t.Fatal("cancellation lost cut", result, resultErr)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	f.stopHook = nil
	r, exit, detail := f.commandActive("resume")
	if exit != 0 || r.Completed == nil || f.stops != 1 || f.starts != 1 {
		t.Fatal("canceled stop was repeated", exit, r, detail)
	}
}

// Controllers with different incident journals still share one installed-unit
// owner. The stopped-repair path participates without gaining stop authority.
func TestRepairActiveValidatorUnitControlExcludesAdjacentRepair(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	control, err := f.host.control(t.Context(), f.active.Plan.Process.Unit)
	if err != nil {
		t.Fatal(err)
	}
	r, exit, detail := f.commandActive("resume")
	if exit != 3 || r.StopConsumed || f.stops != 0 || f.starts != 0 {
		t.Fatal("active owner bypassed shared lock", exit, r, detail)
	}
	if err := control.close(); err != nil {
		t.Fatal(err)
	}
	stopped := newRepairValidatorFixture(t)
	stopped.claim()
	control, err = stopped.host.control(t.Context(), stopped.approval.Plan.Unit)
	if err != nil {
		t.Fatal(err)
	}
	_, exit, detail = stopped.command("resume")
	if exit != 3 || stopped.starts != 0 {
		t.Fatal("stopped owner bypassed shared lock", exit, detail)
	}
	if err := control.close(); err != nil {
		t.Fatal(err)
	}
	if _, exit, detail := stopped.command("resume"); exit != 0 || stopped.starts != 1 {
		t.Fatal("shared lock did not release", exit, detail)
	}
}

// Separate stopped-repair approval cannot take custody after an active repair
// owner has exited. The permanent generation claim survives the process lock.
func TestRepairActiveValidatorLifetimeClaimExcludesStoppedTakeover(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	f.stopError = errors.New("synthetic lost acknowledgement")
	if _, exit, detail := f.commandActive("resume"); exit != 3 {
		t.Fatal(exit, detail)
	}
	p := f.approval.Plan
	raw, err := json.Marshal(p.Original)
	if err != nil {
		t.Fatal(err)
	}
	repairValidatorTestWrite(t, p.MonitorCheckpoint, raw, 0600)
	f.approval.Plan.StatePath = filepath.Join(f.directory, "stopped-takeover.json")
	f.repairValidatorFixture.sign()
	f.repairValidatorFixture.claim()
	r, exit, detail := f.repairValidatorFixture.command("resume")
	if exit != 3 || r.StartConsumed || f.starts != 0 || !strings.Contains(detail, "active-repair custody") {
		t.Fatal("stopped repair took another controller's old generation", exit, r, detail)
	}
}

// The adjacent activation owner also acquires the installed-unit lock before
// any start reservation and releases it after all joined commands return.
func TestRepairActiveValidatorUnitControlExcludesActivation(t *testing.T) {
	f := newValidatorActivationFixture(t)
	f.installed()
	control, err := f.host.host.control(t.Context(), f.approval.Plan.Units[0].Unit)
	if err != nil {
		t.Fatal(err)
	}
	r, exit, detail := f.command(t.Context(), "start", f)
	if exit != 3 || f.starts != [2]int{} || !r.Units[0].StartAt.IsZero() {
		t.Fatal("activation bypassed common unit owner", exit, r, detail)
	}
	if err := control.close(); err != nil {
		t.Fatal(err)
	}
	r, exit, detail = f.command(t.Context(), "start", f)
	if exit != 0 || f.starts != [2]int{1, 1} {
		t.Fatal("activation common owner did not release", exit, r, detail)
	}
}

// A still-fresh, valid stall cannot admit a stop near expiry, including when
// the approval had enough time before an explicitly delayed reservation sync.
func TestRepairActiveValidatorStopRequiresRecoveryHeadroom(t *testing.T) {
	for _, afterReservation := range []bool{false, true} {
		f := newRepairActiveValidatorFixture(t)
		remaining := 129 * time.Second
		if afterReservation {
			remaining = 130 * time.Second
		}
		f.active.Plan.Process.ExpiresAt = f.now.Add(remaining)
		f.signActive()
		f.claimActive()
		store := f.openActive()
		changed := false
		if afterReservation {
			store.syncDirectory = func(dir *os.File) error {
				if err := dir.Sync(); err != nil {
					return err
				}
				raw, err := os.ReadFile(store.path)
				if err != nil {
					return err
				}
				var record repairActiveValidatorRecord
				if err := json.Unmarshal(raw, &record); err != nil {
					return err
				}
				if !changed && record.Status == "stop-consumed" {
					changed = true
					f.now = f.now.Add(time.Second)
				}
				return nil
			}
		}
		r, err := resumeRepairActiveValidator(t.Context(), store, f.host, func() time.Time { return f.now })
		if err == nil || !strings.Contains(err.Error(), "headroom") || r.StopConsumed != afterReservation || changed != afterReservation || f.stops != 0 || f.starts != 0 {
			t.Fatal("near expiry stop lost bounded recovery headroom", afterReservation, r, err)
		}
	}
}

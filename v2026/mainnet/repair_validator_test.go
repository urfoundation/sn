// Synthetic host assertions exercise the real command, signatures, files and
// durable controller. A separate actual child verifies the exec/join boundary.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/sys/unix"
)

// The fake manager is deliberately separate from real filesystem custody.
// No service manager, chain, real binary or production identity is touched.
type repairValidatorFixture struct {
	t             *testing.T
	directory     string
	approvalPath  string
	approval      repairValidatorApproval
	publicKey     string
	privateKey    ed25519.PrivateKey
	host          *repairValidatorHost
	now           time.Time
	manager       map[string]string
	starts        int
	shows         int
	startError    error
	writeProgress bool
	beforeShow    func()
	progress      protocol.ValidatorProgress
}

// Explicit errors are fatal at their original fixture operation.
func repairValidatorTestWrite(t testing.TB, path string, raw []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatal(err)
	}
}

// Test identities and approvals are independent of production launch authority.
func newRepairValidatorFixture(t *testing.T) *repairValidatorFixture {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	if uid == 0 {
		uid, gid = 65534, 65534
	}
	state := filepath.Join(directory, "validator-state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	if os.Getuid() == 0 {
		if err := os.Chown(state, int(uid), int(gid)); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	progress := monitorServicesTestRecord(now.Add(-time.Minute), 71)
	policy := monitorValidatorPolicy{Role: "synthetic", ExpectedSource: progress.Source, ProgressFile: filepath.Join(state, "progress.json")}
	var observed monitorValidatorState
	observed.observe(now.Add(-time.Minute), now.Add(-time.Minute), &progress, "ok")
	if err := observed.retainReadIncident(policy); err != nil {
		t.Fatal(err)
	}
	observed.observe(now.Add(-time.Minute), now, nil, "missing")
	if err := observed.retainReadIncident(policy); err != nil {
		t.Fatal(err)
	}
	original := monitorServiceCheckpointRecord{Schema: monitorServiceCheckpointSchema, Role: policy.Role, Expected: policy.ExpectedSource, State: observed}
	original.ContentHash, _ = hashMonitorServiceCheckpoint(original)
	unit := repairValidatorUnit{Name: "sn-mainnet-validator-synthetic.service", File: planFileReference{Path: filepath.Join(directory, "sn-mainnet-validator-synthetic.service")}, Binary: planFileReference{Path: filepath.Join(directory, "validator")}, Config: planFileReference{Path: filepath.Join(directory, "validator.json")}, StateDirectory: state, ProgressFile: policy.ProgressFile, Uid: uid, Gid: gid}
	unit.File.Sha256 = monitorReadDigest(unit.render())
	unit.Binary.Sha256, unit.Config.Sha256 = monitorReadDigest([]byte("synthetic validator release\n")), monitorReadDigest([]byte("{\"synthetic_config\":true}\n"))
	plan := repairValidatorPlan{Role: policy.Role, Source: policy.ExpectedSource, MachineId: strings.Repeat("3", 32), BootId: "44444444-4444-4444-4444-444444444444", Unit: unit, Systemctl: planFileReference{Path: filepath.Join(directory, "systemctl"), Sha256: monitorReadDigest([]byte("synthetic systemctl transport\n"))}, RequiredMounts: []string{"-.mount"}, Previous: repairValidatorGeneration{InvocationId: strings.Repeat("5", 32), Pid: 71, StartedUsec: 100}, Original: original, IncidentId: observed.ReadIncidents.LastIncident.Id, MonitorCheckpoint: filepath.Join(directory, "monitor.json"), MonitorUid: uint32(os.Getuid()), StatePath: filepath.Join(directory, "repair.json"), ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MaximumStarts: 1, MaximumObservations: 8, CommandTimeoutSeconds: 5, MaximumSampleAgeSeconds: 120}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	self := &repairValidatorFixture{t: t, directory: directory, approvalPath: filepath.Join(directory, "approval.json"), approval: repairValidatorApproval{Schema: repairValidatorSchema, Plan: plan}, publicKey: "0x" + hex.EncodeToString(public), privateKey: private, now: now, writeProgress: true}
	self.host = &repairValidatorHost{rootUid: uint32(os.Getuid()), trustRoot: directory, machinePath: filepath.Join(directory, "machine-id"), bootPath: filepath.Join(directory, "boot-id"), cgroupRoot: filepath.Join(directory, "cgroup"), execute: self.execute, monotonic: func() (uint64, error) { return 150, nil }}
	self.host.cgroupType = func(path string) (int64, error) {
		if path != filepath.Join(directory, "cgroup") {
			return 0, errors.New("synthetic cgroup root changed")
		}
		return unix.CGROUP2_SUPER_MAGIC, nil
	}
	repairValidatorTestWrite(t, unit.File.Path, unit.render(), 0644)
	repairValidatorTestWrite(t, unit.Binary.Path, []byte("synthetic validator release\n"), 0755)
	repairValidatorTestWrite(t, unit.Config.Path, []byte("{\"synthetic_config\":true}\n"), 0600)
	repairValidatorTestWrite(t, plan.Systemctl.Path, []byte("synthetic systemctl transport\n"), 0755)
	repairValidatorTestWrite(t, self.host.machinePath, []byte(plan.MachineId+"\n"), 0644)
	repairValidatorTestWrite(t, self.host.bootPath, []byte(plan.BootId+"\n"), 0644)
	raw, _ := json.Marshal(original)
	repairValidatorTestWrite(t, plan.MonitorCheckpoint, raw, 0600)
	group := filepath.Join(self.host.cgroupRoot, "system.slice", unit.Name)
	if err := os.MkdirAll(group, 0700); err != nil {
		t.Fatal(err)
	}
	repairValidatorTestWrite(t, filepath.Join(group, "cgroup.events"), []byte("populated 0\nfrozen 0\n"), 0644)
	repairValidatorTestWrite(t, filepath.Join(group, "cgroup.procs"), nil, 0644)
	self.manager = map[string]string{}
	for _, key := range repairValidatorProperties {
		self.manager[key] = ""
	}
	for key, value := range map[string]string{"Id": unit.Name, "LoadState": "loaded", "FragmentPath": unit.File.Path, "NeedDaemonReload": "no", "Transient": "no", "Type": "exec", "User": strconv.FormatUint(uint64(uid), 10), "Group": strconv.FormatUint(uint64(gid), 10), "WorkingDirectory": unit.StateDirectory, "Restart": "no", "KillMode": "control-group", "Delegate": "no", "Job": "0", "ControlPID": "0", "MainPID": "0", "ExecMainPID": "71", "ExecMainStartTimestampMonotonic": "100", "ActiveState": "inactive", "SubState": "dead", "InvocationID": plan.Previous.InvocationId, "ControlGroup": "/system.slice/" + unit.Name, "ExecStart": "{ path=" + unit.Binary.Path + " ; argv[]=" + unit.Binary.Path + " run --config=" + unit.Config.Path + " --progress-file=" + unit.ProgressFile + " ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=71 ; code=exited ; status=0 }"} {
		self.manager[key] = value
	}
	self.progress = monitorServicesTestRecord(now, 71)
	self.manager["Requires"] = "system.slice -.mount"
	self.manager["Slice"] = "system.slice"
	self.progress.InstanceId = strings.Repeat("6", 32)
	self.progress.StartedAt = now.Format(time.RFC3339Nano)
	self.sign()
	return self
}

// Every mutation of signed fields must be explicitly re-approved in the fixture.
func (self *repairValidatorFixture) sign() {
	self.t.Helper()
	raw, err := self.approval.signingBytes()
	if err != nil {
		self.t.Fatal(err)
	}
	self.approval.Signature = hex.EncodeToString(ed25519.Sign(self.privateKey, raw))
	if err := self.approval.validate(self.publicKey); err != nil {
		self.t.Fatal("original signed repair fixture refused", err)
	}
	raw, err = json.Marshal(self.approval)
	if err != nil {
		self.t.Fatal(err)
	}
	repairValidatorTestWrite(self.t, self.approvalPath, raw, 0600)
}

// Only this synthetic manager transition creates new operational progress.
func (self *repairValidatorFixture) execute(ctx context.Context, path string, args []string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if path != self.approval.Plan.Systemctl.Path {
		return nil, errors.New("synthetic transport path changed")
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, " show ") {
		unit := args[len(args)-1]
		if unit != self.approval.Plan.Unit.Name {
			return []byte("Id=" + unit + "\nLoadState=loaded\nActiveState=active\nJob=0\n"), nil
		}
		self.shows++
		if self.beforeShow != nil {
			self.beforeShow()
		}
		var output strings.Builder
		for _, key := range repairValidatorProperties {
			fmt.Fprintf(&output, "%s=%s\n", key, self.manager[key])
		}
		return []byte(output.String()), nil
	}
	expected := "--system --no-pager --no-ask-password --job-mode=fail start -- " + self.approval.Plan.Unit.Name
	if joined != expected {
		return nil, errors.New("synthetic transport refused non-start operation")
	}
	self.starts++
	self.manager["ActiveState"], self.manager["SubState"], self.manager["MainPID"], self.manager["ExecMainPID"], self.manager["ExecMainStartTimestampMonotonic"], self.manager["InvocationID"] = "active", "running", "72", "72", "200", strings.Repeat("7", 32)
	if self.writeProgress {
		raw, err := self.progress.Encode()
		if err != nil {
			return nil, err
		}
		repairValidatorTestWrite(self.t, self.approval.Plan.Unit.ProgressFile, raw, 0644)
		if os.Getuid() == 0 {
			if err := os.Chown(self.approval.Plan.Unit.ProgressFile, int(self.approval.Plan.Unit.Uid), int(self.approval.Plan.Unit.Gid)); err != nil {
				return nil, err
			}
		}
	}
	return nil, self.startError
}

// Real command parsing, signature validation, ownership and stores are retained.
func (self *repairValidatorFixture) command(operation string) (repairValidatorResult, int, string) {
	self.t.Helper()
	raw, err := os.ReadFile(self.approvalPath)
	if err != nil {
		self.t.Fatal(err)
	}
	args := []string{operation, "--approval", self.approvalPath, "--accept-approval-hash", monitorReadDigest(raw), "--independent-public-key", self.publicKey}
	var stdout, stderr bytes.Buffer
	exit := runRepairValidatorCommandWithHost(self.t.Context(), args, &stdout, &stderr, func() time.Time { return self.now }, self.host)
	var result repairValidatorResult
	if stdout.Len() != 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			self.t.Fatal("repair result is not structured", err, stdout.String())
		}
	}
	return result, exit, stderr.String()
}

// Claim reads the stopped generation but can never perform a start.
func (self *repairValidatorFixture) claim() {
	self.t.Helper()
	result, exit, detail := self.command("claim")
	if exit != 0 || result.Status != "claimed" || self.starts != 0 {
		self.t.Fatal("signed stopped generation claim refused", exit, result, detail)
	}
}

// Signature, original incident, unit profile and all finite limits are authority.
func TestRepairValidatorIndependentEnvelope(t *testing.T) {
	fixture := newRepairValidatorFixture(t)
	for _, change := range []func(*repairValidatorApproval){func(value *repairValidatorApproval) { value.Plan.MaximumStarts = 2 }, func(value *repairValidatorApproval) { value.Plan.ExpiresAt = value.Plan.ValidFrom.Add(25 * time.Hour) }, func(value *repairValidatorApproval) {
		value.Plan.Unit.Config.Sha256 = monitorReadDigest([]byte("different"))
	}, func(value *repairValidatorApproval) { value.Plan.Previous.Pid++ }, func(value *repairValidatorApproval) { value.Plan.Unit.ProgressFile += ".changed" }, func(value *repairValidatorApproval) {
		value.Plan.Original.ContentHash = monitorReadDigest([]byte("different"))
	}} {
		approval := fixture.approval
		change(&approval)
		if err := approval.validate(fixture.publicKey); err == nil {
			t.Fatal("changed repair authority accepted")
		}
	}
	if err := fixture.approval.validate("0x" + strings.Repeat("a", 64)); err == nil {
		t.Fatal("independent approval key substitution accepted")
	}
	if strings.Contains(string(fixture.approval.Plan.Unit.render()), "Restart=always") || !strings.Contains(string(fixture.approval.Plan.Unit.render()), " run --config=") {
		t.Fatal("unit is not the fixed standard validator profile")
	}
}

// A completed incident remains spent across reopen and approval expiry.
func TestRepairValidatorCommandResumesStoppedGenerationOnce(t *testing.T) {
	fixture := newRepairValidatorFixture(t)
	fixture.claim()
	result, exit, detail := fixture.command("resume")
	if exit != 0 || fixture.starts != 1 || !result.StartConsumed || result.Completed == nil || result.Generation == nil || result.ChainSuccessProven || result.Completed.InstanceId != fixture.progress.InstanceId {
		t.Fatal("approved stopped generation did not resume once", result, exit, detail, fixture.starts)
	}
	fixture.now = fixture.approval.Plan.ExpiresAt.Add(time.Hour)
	for range 2 {
		repeated, exit, detail := fixture.command("resume")
		if exit != 0 || fixture.starts != 1 || repeated.Completed == nil || *repeated.Completed != *result.Completed || repeated.Observations != result.Observations {
			t.Fatal("completed incident consumed another start", repeated, exit, detail, fixture.starts)
		}
	}
}

// systemd may clear an inactive InvocationID, but retained ExecMainPID/start
// fields must still identify the signed prior generation. New starts cannot
// acknowledge an invocation that predates the consumed monotonic boundary.
func TestRepairValidatorSystemdGenerationFields(t *testing.T) {
	fixture := newRepairValidatorFixture(t)
	fixture.manager["InvocationID"] = ""
	fixture.claim()
	result, exit, detail := fixture.command("resume")
	if exit != 0 || result.Completed == nil || fixture.starts != 1 {
		t.Fatal("retained stopped generation could not resume", result, exit, detail)
	}
	for _, field := range []string{"ExecMainPID", "ExecMainStartTimestampMonotonic"} {
		other := newRepairValidatorFixture(t)
		other.manager[field] = "0"
		result, exit, detail := other.command("claim")
		if exit == 0 || other.starts != 0 {
			t.Fatal("missing prior generation field admitted", field, result, exit, detail)
		}
	}
	other := newRepairValidatorFixture(t)
	other.claim()
	other.beforeShow = func() {
		if other.shows == 3 {
			other.manager["ExecMainStartTimestampMonotonic"] = "125"
		}
	}
	result, exit, detail = other.command("resume")
	if exit == 0 || result.Generation != nil || result.Status != "uncertain-consumed-start" || other.starts != 1 {
		t.Fatal("pre-existing new invocation was attributed to start", result, exit, detail)
	}
}

// Changed bytes and hidden descendant processes block the real action boundary.
func TestRepairValidatorReleaseAndCgroupRefusal(t *testing.T) {
	for _, field := range []string{"binary", "config", "unit", "systemctl", "child-cgroup", "process", "active", "invocation", "drop-in", "execution", "pending-job", "extra-dependency", "slice"} {
		fixture := newRepairValidatorFixture(t)
		fixture.claim()
		plan := fixture.approval.Plan
		switch field {
		case "binary":
			repairValidatorTestWrite(t, plan.Unit.Binary.Path, []byte("changed"), 0755)
		case "config":
			repairValidatorTestWrite(t, plan.Unit.Config.Path, []byte("changed"), 0600)
		case "unit":
			repairValidatorTestWrite(t, plan.Unit.File.Path, []byte("changed"), 0644)
		case "systemctl":
			repairValidatorTestWrite(t, plan.Systemctl.Path, []byte("changed"), 0755)
		case "child-cgroup":
			repairValidatorTestWrite(t, filepath.Join(fixture.host.cgroupRoot, "system.slice", plan.Unit.Name, "cgroup.events"), []byte("populated 1\nfrozen 0\n"), 0644)
		case "process":
			repairValidatorTestWrite(t, filepath.Join(fixture.host.cgroupRoot, "system.slice", plan.Unit.Name, "cgroup.procs"), []byte("73\n"), 0644)
		case "active":
			fixture.manager["MainPID"], fixture.manager["ActiveState"] = "71", "active"
		case "invocation":
			fixture.manager["InvocationID"] = strings.Repeat("9", 32)
		case "drop-in":
			fixture.manager["DropInPaths"] = "/synthetic/override.conf"
		case "execution":
			fixture.manager["ExecStart"] = strings.Replace(fixture.manager["ExecStart"], " run ", " unrelated ", 1)
		case "pending-job":
			fixture.manager["Job"] = "71"
		case "extra-dependency":
			fixture.manager["Requires"] += " unrelated.service"
		case "slice":
			fixture.manager["Slice"] = "unrelated.slice"
		}
		result, exit, detail := fixture.command("resume")
		if exit == 0 || fixture.starts != 0 || result.StartConsumed {
			t.Fatal("changed release or occupied generation consumed start", field, result, exit, detail)
		}
	}
}

// An absent unit directory is empty only inside the declared v2 hierarchy.
// Prerequisite reads independently refuse an inactive mount without a start.
func TestRepairValidatorUnifiedCgroupAndPrerequisites(t *testing.T) {
	for _, change := range []string{"v1", "unmounted", "unavailable", "inactive-mount", "v2-removed"} {
		fixture := newRepairValidatorFixture(t)
		fixture.claim()
		if err := os.RemoveAll(filepath.Join(fixture.host.cgroupRoot, "system.slice", fixture.approval.Plan.Unit.Name)); err != nil {
			t.Fatal(err)
		}
		switch change {
		case "v1":
			fixture.host.cgroupType = func(string) (int64, error) { return unix.CGROUP_SUPER_MAGIC, nil }
		case "unmounted":
			fixture.host.cgroupType = func(string) (int64, error) { return 0, nil }
		case "unavailable":
			fixture.host.cgroupType = func(string) (int64, error) { return 0, os.ErrNotExist }
		case "inactive-mount":
			original := fixture.host.execute
			fixture.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
				raw, err := original(ctx, path, args)
				if args[len(args)-1] == "-.mount" {
					raw = bytes.Replace(raw, []byte("ActiveState=active"), []byte("ActiveState=inactive"), 1)
				}
				return raw, err
			}
		}
		result, exit, detail := fixture.command("resume")
		if change == "v2-removed" {
			if exit != 0 || fixture.starts != 1 || result.Completed == nil {
				t.Fatal("verified removed v2 group could not resume", result, exit, detail)
			}
		} else if exit == 0 || fixture.starts != 0 || result.StartConsumed {
			t.Fatal("unknown cgroup filesystem or inactive prerequisite admitted start", change, result, exit, detail, fixture.starts)
		}
		if filesystem, err := repairValidatorCgroupType(fixture.directory); err != nil || filesystem == unix.CGROUP2_SUPER_MAGIC {
			t.Fatal("actual ordinary filesystem was classified as cgroup v2", filesystem, err)
		}
	}
}

// An acknowledged command is different from a consumed command whose outcome
// was lost. A later unrelated invocation cannot turn uncertainty into success.
func TestRepairValidatorUncertainStartNeverRetries(t *testing.T) {
	fixture := newRepairValidatorFixture(t)
	fixture.claim()
	fixture.startError = errors.New("synthetic connection lost after start")
	result, exit, detail := fixture.command("resume")
	if exit == 0 || fixture.starts != 1 || result.Status != "uncertain-consumed-start" || result.Generation != nil || !strings.Contains(result.OperatorDisposition, "Manual host reconciliation") {
		t.Fatal("lost start acknowledgement did not retain uncertainty", result, exit, detail)
	}
	fixture.startError = nil
	fixture.manager["InvocationID"] = strings.Repeat("9", 32)
	repeated, exit, detail := fixture.command("resume")
	if exit == 0 || fixture.starts != 1 || repeated.Status != "uncertain-consumed-start" || repeated.Completed != nil || repeated.Generation != nil {
		t.Fatal("uncertain consumed start was retried or misattributed", repeated, exit, detail)
	}
	// A crash immediately after reservation can precede the actual command.
	other := newRepairValidatorFixture(t)
	other.claim()
	store, err := openRepairValidatorStore(t.Context(), other.approval, other.publicKey, false, other.now)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	record.Observations, record.StartAt, record.StartMonotonicUsec, record.Status = 1, other.now, 150, "start-consumed"
	if err := errors.Join(store.save(record), store.close()); err != nil {
		t.Fatal(err)
	}
	repeated, exit, detail = other.command("resume")
	if exit == 0 || other.starts != 0 || repeated.Status != "uncertain-consumed-start" {
		t.Fatal("crashed consumed reservation restored start allowance", repeated, exit, detail)
	}
}

// Postcondition failure preserves the acknowledged generation and never creates
// a second action. Reopen can verify that same process after progress appears.
func TestRepairValidatorAcknowledgedGenerationAndProgress(t *testing.T) {
	for _, change := range []string{"missing", "source", "old-instance", "old-start", "future", "generation"} {
		fixture := newRepairValidatorFixture(t)
		fixture.claim()
		switch change {
		case "missing":
			fixture.writeProgress = false
		case "source":
			fixture.progress.Source.ValidatorId++
		case "old-instance":
			fixture.progress.InstanceId = fixture.approval.Plan.Original.State.Record.InstanceId
		case "old-start":
			fixture.progress.StartedAt = fixture.now.Add(-time.Second).Format(time.RFC3339Nano)
		case "future":
			fixture.progress.HeartbeatAt = fixture.now.Add(time.Second).Format(time.RFC3339Nano)
		case "generation":
			fixture.beforeShow = func() {
				if fixture.shows == 4 {
					fixture.manager["InvocationID"] = strings.Repeat("9", 32)
				}
			}
		}
		result, exit, detail := fixture.command("resume")
		if exit == 0 || fixture.starts != 1 || result.Generation == nil || result.Completed != nil {
			t.Fatal("unverified source or generation completed repair", change, result, exit, detail)
		}
		if change == "missing" {
			raw, _ := fixture.progress.Encode()
			repairValidatorTestWrite(t, fixture.approval.Plan.Unit.ProgressFile, raw, 0644)
			if os.Getuid() == 0 {
				if err := os.Chown(fixture.approval.Plan.Unit.ProgressFile, int(fixture.approval.Plan.Unit.Uid), int(fixture.approval.Plan.Unit.Gid)); err != nil {
					t.Fatal(err)
				}
			}
			result, exit, detail = fixture.command("resume")
			if exit != 0 || fixture.starts != 1 || result.Completed == nil {
				t.Fatal("acknowledged generation could not reconcile", result, exit, detail)
			}
		} else {
			fixture.manager["InvocationID"] = strings.Repeat("a", 32)
			result, exit, detail = fixture.command("resume")
			if exit == 0 || fixture.starts != 1 || result.Completed != nil || result.Status != "generation-changed" {
				t.Fatal("unrelated later invocation completed repair", result, exit, detail)
			}
		}
	}
}

// The original incident must still be open, fresh and from its approved owner.
func TestRepairValidatorIncidentWindowAndLimits(t *testing.T) {
	for _, change := range []string{"incident", "stale", "expiry", "rollback", "capacity"} {
		fixture := newRepairValidatorFixture(t)
		if change == "capacity" {
			fixture.approval.Plan.MaximumObservations = 1
			fixture.sign()
		}
		fixture.claim()
		switch change {
		case "incident":
			current := fixture.approval.Plan.Original
			current.Role = "unrelated"
			current.ContentHash, _ = hashMonitorServiceCheckpoint(current)
			raw, _ := json.Marshal(current)
			repairValidatorTestWrite(t, fixture.approval.Plan.MonitorCheckpoint, raw, 0600)
		case "stale":
			fixture.now = fixture.now.Add(121 * time.Second)
		case "expiry":
			fixture.now = fixture.approval.Plan.ExpiresAt
		case "rollback":
			fixture.now = fixture.now.Add(-time.Second)
		case "capacity":
			fixture.manager["LoadState"] = "not-found"
			_, _, _ = fixture.command("resume")
			fixture.manager["LoadState"] = "loaded"
		}
		result, exit, detail := fixture.command("resume")
		if exit == 0 || fixture.starts != 0 || result.StartConsumed || change == "capacity" && result.Status != "observation-limit" {
			t.Fatal("incident freshness, window or count gate escaped", change, result, exit, detail)
		}
	}
}

// An fsync ambiguity after the consumed reservation must stop before the action.
// After a completed publication, reopen retains the receipt instead of retrying.
func TestRepairValidatorPublicationAmbiguityAndReopen(t *testing.T) {
	for _, failAt := range []int{2, 4} {
		fixture := newRepairValidatorFixture(t)
		fixture.claim()
		store, err := openRepairValidatorStore(t.Context(), fixture.approval, fixture.publicKey, false, fixture.now)
		if err != nil {
			t.Fatal(err)
		}
		writes := 0
		store.syncDirectory = func(file *os.File) error {
			writes++
			err := file.Sync()
			if writes == failAt {
				return errors.Join(err, errors.New("synthetic directory-sync uncertainty"))
			}
			return err
		}
		_, err = resumeRepairValidator(t.Context(), store, fixture.host, func() time.Time { return fixture.now })
		if err == nil || failAt == 2 && fixture.starts != 0 || failAt == 4 && fixture.starts != 1 {
			t.Fatal("ambiguous publication crossed its action boundary", failAt, err, fixture.starts)
		}
		if _, err := resumeRepairValidator(t.Context(), store, fixture.host, func() time.Time { return fixture.now }); err == nil {
			t.Fatal("ambiguous repair owner was reused")
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		result, exit, detail := fixture.command("resume")
		if failAt == 2 && (exit == 0 || result.Status != "uncertain-consumed-start" || fixture.starts != 0) || failAt == 4 && (exit != 0 || result.Completed == nil || fixture.starts != 1) {
			t.Fatal("reopen lost consumed or completed repair custody", failAt, result, exit, detail)
		}
	}
}

// A positive barrier inside the real directory sync advances each authority
// clock independently. A spent reservation may refuse, never refund or start.
func TestRepairValidatorPostSyncAuthorityRecheck(t *testing.T) {
	for _, change := range []string{"expiry", "incident-age", "rollback", "cancellation"} {
		fixture := newRepairValidatorFixture(t)
		if change == "expiry" {
			fixture.approval.Plan.ExpiresAt = fixture.now.Add(10 * time.Second)
			fixture.sign()
		}
		fixture.claim()
		store, err := openRepairValidatorStore(t.Context(), fixture.approval, fixture.publicKey, false, fixture.now)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		writes := 0
		store.syncDirectory = func(file *os.File) error {
			err := file.Sync()
			writes++
			if writes == 2 {
				switch change {
				case "expiry":
					fixture.now = fixture.approval.Plan.ExpiresAt
				case "incident-age":
					fixture.now = fixture.now.Add(time.Duration(fixture.approval.Plan.MaximumSampleAgeSeconds+1) * time.Second)
				case "rollback":
					fixture.now = fixture.now.Add(-time.Second)
				case "cancellation":
					cancel()
				}
			}
			return err
		}
		result, err := resumeRepairValidator(ctx, store, fixture.host, func() time.Time { return fixture.now })
		cancel()
		if err == nil || fixture.starts != 0 || !result.StartConsumed || result.Status != "uncertain-consumed-start" || result.Generation != nil {
			t.Fatal("post-sync authority change crossed start boundary", change, result, err, fixture.starts)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		result, exit, detail := fixture.command("resume")
		if exit == 0 || fixture.starts != 0 || !result.StartConsumed || result.Status != "uncertain-consumed-start" {
			t.Fatal("post-sync refusal refunded start allowance", change, result, exit, detail, fixture.starts)
		}
	}
}

// Permanent marker and process ownership prevent implicit reset of a liability.
func TestRepairValidatorStoreOwnershipAndMissingState(t *testing.T) {
	fixture := newRepairValidatorFixture(t)
	fixture.claim()
	store, err := openRepairValidatorStore(t.Context(), fixture.approval, fixture.publicKey, false, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	if _, err := openRepairValidatorStore(t.Context(), fixture.approval, fixture.publicKey, false, fixture.now); err == nil {
		t.Fatal("duplicate repair journal owner acquired")
	}
	if err := os.Remove(fixture.approval.Plan.StatePath); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(t.Context()); err == nil {
		t.Fatal("missing repair journal restored allowance")
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openRepairValidatorStore(t.Context(), fixture.approval, fixture.publicKey, true, fixture.now); err == nil {
		t.Fatal("permanent incident marker allowed fresh claim")
	}
	if _, err := resumeRepairValidator(nil, store, fixture.host, time.Now); err == nil {
		t.Fatal("nil context was admitted")
	}
}

// A real synthetic child proves cancellation joins the actual process. It never
// invokes systemctl or starts a production validator.
func TestRepairValidatorCommandCancellationJoinsProcess(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(t.TempDir(), "ready")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := executeRepairValidatorCommand(ctx, path, []string{"-test.run=^TestRepairValidatorProcessHelper$", "--", "blocked", fifo})
		done <- err
	}()
	file, err := os.Open(fifo)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(file)
	if err := errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		t.Fatal("synthetic child readiness missing", string(raw), err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("canceled child did not report cancellation", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("canceled command returned before child teardown", pid, err)
	}
}

// Real stdout overflow and strict manager keys cannot become success evidence.
func TestRepairValidatorManagerWireAndOutputBounds(t *testing.T) {
	fixture := newRepairValidatorFixture(t)
	for _, change := range []string{"duplicate", "missing", "old-monotonic"} {
		original := fixture.host.execute
		fixture.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
			raw, err := original(ctx, path, args)
			if change == "duplicate" {
				raw = append(raw, []byte("MainPID=0\n")...)
			} else if change == "missing" {
				raw = bytes.Replace(raw, []byte("MainPID=0\n"), nil, 1)
			} else {
				raw = bytes.Replace(raw, []byte("ExecMainStartTimestampMonotonic=100"), []byte("ExecMainStartTimestampMonotonic=0100"), 1)
			}
			return raw, err
		}
		if _, err := fixture.host.inspect(t.Context(), fixture.approval.Plan); err == nil {
			t.Fatal("ambiguous manager evidence accepted", change)
		}
		fixture.host.execute = original
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err := executeRepairValidatorCommand(ctx, path, []string{"-test.run=^TestRepairValidatorProcessHelper$", "--", "overflow"}); err == nil {
		t.Fatal("unbounded command output accepted")
	}
}

// Subprocess entry selected only by explicit synthetic arguments. The parent
// owns cancellation; the readiness fifo is a positive barrier, not a sleep.
func TestRepairValidatorProcessHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i
			break
		}
	}
	if index < 0 || index+1 >= len(os.Args) {
		t.Skip("synthetic subprocess entry")
	}
	switch os.Args[index+1] {
	case "blocked":
		if index+2 >= len(os.Args) {
			t.Fatal("readiness fifo missing")
		}
		file, err := os.OpenFile(os.Args[index+2], os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := fmt.Fprintf(file, "%d\n", os.Getpid())
		if err := errors.Join(writeErr, file.Close()); err != nil {
			t.Fatal(err)
		}
		for {
			_ = syscall.Pause()
		}
	case "overflow":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 64*1024))
	default:
		t.Fatal("unknown synthetic process operation")
	}
}

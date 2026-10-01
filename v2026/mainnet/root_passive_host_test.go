// Synthetic complete v4 preparation drives real approvals, files and finalized
// reads. A private manager transport cannot execute any real system service.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type rootPassiveHostFixture struct {
	t           *testing.T
	chain       *bootstrapChainFixture
	host        *rootPassiveHost
	approval    rootPassiveHostApproval
	private     ed25519.PrivateKey
	key         string
	path        string
	now         time.Time
	manager     map[string]string
	starts      int
	reloads     int
	startError  error
	reloadError error
	omitted     string
}

func newRootPassiveHostFixture(t *testing.T) *rootPassiveHostFixture {
	t.Helper()
	chain := newBootstrapRootPassiveFixture(t)
	directory := filepath.Dir(chain.path)
	root := filepath.Dir(directory)
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	trustRoot := filepath.Dir(root)
	if err := os.Chmod(trustRoot, 0755); err != nil {
		t.Fatal(err)
	}
	checkpointDirectory := filepath.Join(chain.config.RunDirectory, rootPassiveCheckpointDirectory)
	if err := os.Mkdir(checkpointDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	service := *chain.root.plan.PassiveService
	service.CheckpointPath = filepath.Join(checkpointDirectory, "checkpoint.json")
	chain.root.config.RootService = bootstrapRootTestWrite(t, chain.root.config.RootService.Path, service)
	chain.config.Root = bootstrapRootTestWrite(t, chain.root.configPath, chain.root.config)
	var err error
	chain.root.plan, err = loadBootstrapRootPlan(t.Context(), chain.root.configPath)
	if err != nil {
		t.Fatal(err)
	}
	chain.rootRole.approval.RootPlanHash = chain.root.plan.ContentHash
	chain.rootRole.approval.ServiceConfigHash = chain.root.plan.serviceHash()
	chain.rootRole.sign(t)
	bootstrapRootTestWrite(t, chain.path, chain.config)
	chain.preparation, err = loadBootstrapChainPreparation(t.Context(), chain.path)
	if err != nil {
		t.Fatal(err)
	}
	chain.result(t, "apply")
	runtime := bootstrapRootTestWrite(t, filepath.Join(directory, "passive-runtime.json"), rootPassiveRuntimeConfig{Schema: rootPassiveRuntimeSchema, Root: chain.config.Root, Role: *chain.config.RootValidator})
	unitDirectory := filepath.Join(root, "systemd")
	if err := os.Mkdir(unitDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	binary := planFileReference{Path: filepath.Join(root, "mainnet"), Sha256: monitorReadDigest([]byte("synthetic exact mainnet binary\n"))}
	systemctl := planFileReference{Path: filepath.Join(root, "systemctl"), Sha256: monitorReadDigest([]byte("synthetic exact systemctl\n"))}
	repairValidatorTestWrite(t, binary.Path, []byte("synthetic exact mainnet binary\n"), 0755)
	repairValidatorTestWrite(t, systemctl.Path, []byte("synthetic exact systemctl\n"), 0755)
	now := time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x45}, ed25519.SeedSize))
	f := &rootPassiveHostFixture{t: t, chain: chain, private: private, key: "0x" + hex.EncodeToString(private.Public().(ed25519.PublicKey)), path: filepath.Join(directory, "passive-host-approval.json"), now: now, manager: map[string]string{}}
	p := rootPassiveHostPlan{Preparation: planFileReference{Path: chain.path, Sha256: chain.preparation.Plan.ConfigSha256}, PlanHash: chain.preparation.Plan.ContentHash, Runtime: runtime, RootPlanHash: chain.root.plan.ContentHash,
		Unit: planFileReference{Path: filepath.Join(unitDirectory, rootPassiveHostUnitName)}, Binary: binary, Systemctl: systemctl, MachineId: strings.Repeat("3", 32), BootId: "44444444-4444-4444-4444-444444444444", CheckpointDirectory: checkpointDirectory, RequiredMounts: []string{"-.mount"}, StatePath: filepath.Join(directory, "passive-host-state.json"), ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MaximumOperations: 64, CommandTimeoutSeconds: 5, MaximumSampleAgeSeconds: 120, InstallStaticUnit: true, AuthorizeOnePassiveStart: true}
	p.Unit.Sha256 = monitorReadDigest(p.render())
	f.approval = rootPassiveHostApproval{Schema: rootPassiveHostSchema, Plan: p}
	h := &repairValidatorHost{rootUid: uint32(os.Geteuid()), trustRoot: trustRoot, machinePath: filepath.Join(directory, "machine-id"), bootPath: filepath.Join(directory, "boot-id"), cgroupRoot: filepath.Join(directory, "cgroup"), cgroupType: func(string) (int64, error) { return unix.CGROUP2_SUPER_MAGIC, nil }, execute: f.execute, monotonic: func() (uint64, error) { return 150, nil }}
	f.host = &rootPassiveHost{files: &validatorActivationHost{host: h, unitDirectory: unitDirectory}, gid: uint32(os.Getegid())}
	repairValidatorTestWrite(t, h.machinePath, []byte(p.MachineId+"\n"), 0644)
	repairValidatorTestWrite(t, h.bootPath, []byte(p.BootId+"\n"), 0644)
	if err := os.Mkdir(h.cgroupRoot, 0700); err != nil {
		t.Fatal(err)
	}
	for _, key := range repairValidatorProperties {
		f.manager[key] = ""
	}
	for key, value := range p.sandbox() {
		f.manager[key] = value
	}
	for key, value := range map[string]string{"Id": rootPassiveHostUnitName, "LoadState": "loaded", "FragmentPath": p.Unit.Path, "NeedDaemonReload": "no", "Transient": "no", "Type": "exec", "User": strconv.Itoa(os.Geteuid()), "Group": strconv.Itoa(os.Getegid()), "WorkingDirectory": p.CheckpointDirectory, "Restart": "no", "KillMode": "control-group", "Delegate": "no", "Slice": "system.slice", "Requires": "system.slice -.mount", "Job": "0", "ControlPID": "0", "MainPID": "0", "ExecMainPID": "0", "ExecMainStartTimestampMonotonic": "0", "ActiveState": "inactive", "SubState": "dead", "ExecStart": "{ path=" + p.Binary.Path + " ; argv[]=" + p.Binary.Path + " " + p.arguments() + " ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0 }"} {
		f.manager[key] = value
	}
	f.sign()
	// The composed fixtures allocate independent temporary roots. Bring every
	// private fixture ancestor under the same protected synthetic host boundary.
	if err := filepath.WalkDir(trustRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode().Perm()&^0022)
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (self *rootPassiveHostFixture) sign() {
	self.t.Helper()
	raw, err := self.approval.signingBytes()
	if err != nil {
		self.t.Fatal(err)
	}
	self.approval.Signature = hex.EncodeToString(ed25519.Sign(self.private, raw))
	if err := self.approval.validate(self.key); err != nil {
		self.t.Fatal(err)
	}
	bootstrapRootTestWrite(self.t, self.path, self.approval)
}

func (self *rootPassiveHostFixture) execute(ctx context.Context, path string, args []string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if path != self.approval.Plan.Systemctl.Path {
		return nil, errors.New("synthetic manager binary differs")
	}
	joined := strings.Join(args, " ")
	if joined == "--system --no-pager --no-ask-password daemon-reload" {
		self.reloads++
		return nil, self.reloadError
	}
	if joined == "--system --no-pager --no-ask-password --job-mode=fail start -- "+rootPassiveHostUnitName {
		self.starts++
		for key, value := range map[string]string{"ActiveState": "active", "SubState": "running", "MainPID": "4321", "ExecMainPID": "4321", "ExecMainStartTimestampMonotonic": "151", "InvocationID": strings.Repeat("5", 32), "ControlGroup": "/system.slice/" + rootPassiveHostUnitName} {
			self.manager[key] = value
		}
		return nil, self.startError
	}
	name := args[len(args)-1]
	if strings.Contains(joined, " show ") {
		if name == rootPassiveHostUnitName {
			var output strings.Builder
			for _, arg := range args {
				if strings.HasPrefix(arg, "--property=") {
					for _, key := range strings.Split(strings.TrimPrefix(arg, "--property="), ",") {
						if key != self.omitted {
							value, ok := self.manager[key]
							if !ok {
								return nil, fmt.Errorf("synthetic manager lacks %s", key)
							}
							fmt.Fprintf(&output, "%s=%s\n", key, value)
						}
					}
				}
			}
			return []byte(output.String()), nil
		}
		if name == "system.slice" || name == "-.mount" {
			return []byte("Id=" + name + "\nLoadState=loaded\nActiveState=active\nJob=0\n"), nil
		}
	}
	return nil, fmt.Errorf("unapproved synthetic manager command: %s", joined)
}

func (self *rootPassiveHostFixture) command(operation string) (rootPassiveHostResult, int, string) {
	self.t.Helper()
	raw, err := os.ReadFile(self.path)
	if err != nil {
		self.t.Fatal(err)
	}
	args := []string{operation, "--approval", self.path, "--accept-approval-hash", monitorReadDigest(raw), "--independent-public-key", self.key}
	if operation == "start" {
		args = append(args, "--execute-approved-start")
	}
	var out, diagnostic bytes.Buffer
	code := runRootPassiveHostCommandWithHost(self.t.Context(), args, &out, &diagnostic, func() time.Time { return self.now }, self.host)
	var result rootPassiveHostResult
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			self.t.Fatal(err, out.String())
		}
	}
	return result, code, diagnostic.String()
}

func (self *rootPassiveHostFixture) require(operation, status string) rootPassiveHostResult {
	self.t.Helper()
	result, code, diagnostic := self.command(operation)
	if code != 0 || result.Status != status {
		self.t.Fatalf("%s: code=%d result=%+v diagnostic=%s", operation, code, result, diagnostic)
	}
	if result.NativeSigning || result.NetworkSubmission || result.RootServiceReady || result.ActivationReady {
		self.t.Fatal("passive process acquired native or full readiness authority", result)
	}
	return result
}

func (self *rootPassiveHostFixture) store() *rootPassiveHostStore {
	self.t.Helper()
	custody, err := openBootstrapChainReadinessState(self.t.Context(), self.chain.preparation)
	if err != nil {
		self.t.Fatal(err)
	}
	defer custody.close()
	store, err := openRootPassiveHostStore(self.t.Context(), self.approval, self.key, false, self.now, *custody, rootObjectHash(self.chain.root.plan.PassiveService.Policy))
	if err != nil {
		self.t.Fatal(err)
	}
	return store
}

func TestRootPassiveHostInstallsStartsAndRecovers(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	before := f.chain.journals(t)
	f.require("claim", "claimed")
	f.require("install", "installed")
	f.require("admit", "admitted-read-only")
	result := f.require("start", "acknowledged-running")
	if f.starts != 1 || result.Generation == nil || !result.StartConsumed || !result.CurrentProcessRunning {
		t.Fatal("start lacked exact acknowledgment", result, f.starts)
	}
	recovered := f.require("resume", "acknowledged-running")
	if !recovered.CurrentProcessRunning || !reflect.DeepEqual(result.Generation, recovered.Generation) || f.starts != 1 {
		t.Fatal("reopen renewed or changed invocation", recovered, f.starts)
	}
	historical := f.require("status", "acknowledged-running")
	if historical.CurrentProcessRunning {
		t.Fatal("historical status claimed a live process")
	}
	// The actual observer can use this exact private runtime and isolated path;
	// the synthetic host acknowledgment itself never claims monitor health.
	p := f.approval.Plan
	if code := runRootPassiveServiceCommand(t.Context(), []string{"run", "--config", p.Runtime.Path, "--accept-runtime-sha256", p.Runtime.Sha256}, io.Discard, io.Discard); code != 0 {
		t.Fatal("installed runtime cannot read original authority", code)
	}
	if _, err := os.Stat(f.chain.root.plan.PassiveService.CheckpointPath); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, f.chain.journals(t)) {
		t.Fatal("passive owner changed original bootstrap custody")
	}
	f.manager["MainPID"] = "0"
	f.manager["ActiveState"] = "inactive"
	f.manager["SubState"] = "dead"
	if result := f.require("resume", "acknowledged-stopped"); result.CurrentProcessRunning {
		t.Fatal("completed finite observer remained live")
	}
	if _, code, _ := f.command("start"); code == 0 || f.starts != 1 {
		t.Fatal("finite exit renewed initial start")
	}
}

func TestRootPassiveHostPreservesUncertainStartAcrossReopen(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	f.require("claim", "claimed")
	f.require("install", "installed")
	f.startError = errors.New("synthetic transport lost acknowledgment after actual start")
	result, code, detail := f.command("start")
	if code != 3 || !result.StartConsumed || result.Generation != nil || result.Status != "uncertain-consumed-start" || f.starts != 1 {
		t.Fatal("ambiguous start was released", result, code, detail)
	}
	f.startError = nil
	for _, operation := range []string{"resume", "start", "install"} {
		result, code, detail = f.command(operation)
		if code != 3 || !result.StartConsumed || result.Generation != nil || f.starts != 1 {
			t.Fatal("uncertain invocation was adopted or retried", operation, result, code, detail)
		}
	}
	if err := os.Remove(f.approval.Plan.StatePath); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := f.command("claim"); code == 0 || f.starts != 1 {
		t.Fatal("deleted journal replenished allowance")
	}
}

func TestRootPassiveHostPartialInstallationRecoversExactFile(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	f.require("claim", "claimed")
	f.reloadError = errors.New("synthetic interrupted reload")
	if result, code, _ := f.command("install"); code != 3 || result.StartConsumed || f.starts != 0 {
		t.Fatal("partial installation started a process", result, code)
	}
	before, err := os.ReadFile(f.approval.Plan.Unit.Path)
	if err != nil {
		t.Fatal(err)
	}
	f.reloadError = nil
	f.require("install", "installed")
	after, err := os.ReadFile(f.approval.Plan.Unit.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("partial install did not reconcile exact bytes", err)
	}
	f.require("start", "acknowledged-running")
	if f.starts != 1 {
		t.Fatal("partial installation changed start allowance")
	}
}

func TestRootPassiveHostRefusesConflictingPartialInstallation(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	f.require("claim", "claimed")
	f.reloadError = errors.New("synthetic interrupted reload")
	f.command("install")
	f.reloadError = nil
	replacement := []byte("synthetic unapproved static unit\n")
	repairValidatorTestWrite(t, f.approval.Plan.Unit.Path, replacement, 0644)
	if _, code, _ := f.command("install"); code != 3 || f.starts != 0 {
		t.Fatal("conflicting partial install admitted")
	}
	after, err := os.ReadFile(f.approval.Plan.Unit.Path)
	if err != nil || !bytes.Equal(after, replacement) {
		t.Fatal("install replaced conflicting existing unit", err)
	}
}

func TestRootPassiveHostRejectsManagerOmissionAndSandboxChange(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	f.require("claim", "claimed")
	f.require("install", "installed")
	for _, key := range []string{"ProtectSystem", "ReadWritePaths", "CapabilityBoundingSet", "NoNewPrivileges", "User", "Environment", "ExecStart", "InvocationID"} {
		f.omitted = key
		if result, code, _ := f.command("start"); code != 3 || result.StartConsumed || f.starts != 0 {
			t.Fatal("manager omission authorized start", key, result, code)
		}
		f.omitted = ""
		old := f.manager[key]
		f.manager[key] = "synthetic-changed"
		if result, code, _ := f.command("start"); code != 3 || result.StartConsumed || f.starts != 0 {
			t.Fatal("changed loaded profile authorized start", key, result, code)
		}
		f.manager[key] = old
	}
	f.require("start", "acknowledged-running")
}

func TestRootPassiveHostPublicationFailureConsumesWithoutStarting(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	f.require("claim", "claimed")
	f.require("install", "installed")
	store := f.store()
	fired := false
	store.syncDirectory = func(directory *os.File) error {
		raw, err := os.ReadFile(f.approval.Plan.StatePath)
		if err != nil {
			return err
		}
		var record rootPassiveHostRecord
		if err := decodePlanJson(raw, &record); err != nil {
			return err
		}
		if !record.StartAt.IsZero() {
			fired = true
			return errors.New("synthetic post-rename sync failure")
		}
		return directory.Sync()
	}
	_, err := advanceRootPassiveHost(t.Context(), store, f.host, f.chain.preparation, "start", func() time.Time { return f.now })
	store.close()
	if err == nil || !fired || f.starts != 0 {
		t.Fatal("ambiguous reservation reached systemd", err, fired, f.starts)
	}
	result, code, _ := f.command("resume")
	if code != 3 || !result.StartConsumed || result.Generation != nil || f.starts != 0 {
		t.Fatal("reopen replenished ambiguous reservation", result, code)
	}
}

func TestRootPassiveHostPostSyncExpiryConsumesWithoutStarting(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	f.require("claim", "claimed")
	f.require("install", "installed")
	store := f.store()
	fired := false
	store.syncDirectory = func(directory *os.File) error {
		raw, err := os.ReadFile(f.approval.Plan.StatePath)
		if err != nil {
			return err
		}
		var record rootPassiveHostRecord
		if err := decodePlanJson(raw, &record); err != nil {
			return err
		}
		if !record.StartAt.IsZero() {
			fired = true
			f.now = f.approval.Plan.ExpiresAt
		}
		return directory.Sync()
	}
	_, err := advanceRootPassiveHost(t.Context(), store, f.host, f.chain.preparation, "start", func() time.Time { return f.now })
	store.close()
	if err == nil || !fired || f.starts != 0 {
		t.Fatal("expired durable reservation reached systemd", err, fired, f.starts)
	}
	result := f.require("status", "uncertain-consumed-start")
	if !result.StartConsumed || result.Generation != nil {
		t.Fatal("post-sync expiry lost liability", result)
	}
}

func TestRootPassiveHostRejectsChangedOriginalAuthority(t *testing.T) {
	for _, change := range []string{"runtime", "passive-approval", "ur-config", "root-marker", "boot"} {
		f := newRootPassiveHostFixture(t)
		f.require("claim", "claimed")
		before, err := os.ReadFile(f.approval.Plan.StatePath)
		if err != nil {
			t.Fatal(err)
		}
		path := ""
		switch change {
		case "runtime":
			path = f.approval.Plan.Runtime.Path
		case "passive-approval":
			path = f.chain.config.RootValidator.Approval.Path
		case "ur-config":
			path = f.chain.config.Validators[1].Config.Path
		case "root-marker":
			path = filepath.Join(f.chain.root.plan.RunDirectory, bootstrapRootProgressFile) + ".lock"
		case "boot":
			path = f.host.files.host.bootPath
		}
		if err := os.WriteFile(path, []byte("synthetic changed original authority\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, code, _ := f.command("install"); code != 3 || f.starts != 0 || f.reloads != 0 {
			t.Fatal("changed original input admitted host effect", change, code)
		}
		after, err := os.ReadFile(f.approval.Plan.StatePath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("invalid original authority changed host custody", change, err)
		}
		if _, err := os.Stat(f.approval.Plan.Unit.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("wrong authority installed a unit", change, err)
		}
	}
}

func TestRootPassiveHostRejectsWritableAuthorityAndCheckpointLinks(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	original := f.approval.Plan.StatePath
	f.approval.Plan.StatePath = filepath.Join(f.approval.Plan.CheckpointDirectory, "host-state.json")
	f.sign()
	if _, code, _ := f.command("claim"); code != 3 {
		t.Fatal("host journal was admitted inside worker write authority", code)
	}
	f.approval.Plan.StatePath = original
	f.sign()
	checkpoint := f.chain.root.plan.PassiveService.CheckpointPath
	if err := os.Link(f.approval.Plan.Runtime.Path, checkpoint); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := f.command("claim"); code != 3 {
		t.Fatal("hard-linked checkpoint could write authority", code)
	}
	if err := os.Remove(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.approval.Plan.Runtime.Path, checkpoint); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := f.command("claim"); code != 3 {
		t.Fatal("linked checkpoint could write authority", code)
	}
	if err := os.Remove(checkpoint); err != nil {
		t.Fatal(err)
	}
	f.require("claim", "claimed")
}

func TestRootPassiveHostRequiresFreshRuntimeAndExactInvocation(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	f.require("claim", "claimed")
	f.require("install", "installed")
	original := f.chain.census.version
	f.chain.census.version.SpecVersion++
	result, code, _ := f.command("start")
	if code != 3 || result.StartConsumed || f.starts != 0 {
		t.Fatal("changed current runtime admitted process", result, code)
	}
	f.chain.census.version = original
	f.require("start", "acknowledged-running")
	for _, field := range []string{"InvocationID", "ExecMainPID", "ExecMainStartTimestampMonotonic", "ControlGroup"} {
		old := f.manager[field]
		switch field {
		case "InvocationID":
			f.manager[field] = strings.Repeat("6", 32)
		case "ExecMainPID":
			f.manager[field] = "4322"
		case "ExecMainStartTimestampMonotonic":
			f.manager[field] = "152"
		case "ControlGroup":
			f.manager[field] = "/system.slice/foreign.service"
		}
		if result, code, _ := f.command("resume"); code != 3 || result.CurrentProcessRunning || f.starts != 1 {
			t.Fatal("resume adopted another invocation", field, result, code)
		}
		f.manager[field] = old
	}
	f.require("resume", "acknowledged-running")
}

func TestRootPassiveHostRejectsUnapprovedEnvelopeAndStartFlags(t *testing.T) {
	f := newRootPassiveHostFixture(t)
	original := f.approval
	f.approval.Plan.MaximumOperations++
	bootstrapRootTestWrite(t, f.path, f.approval)
	if _, code, _ := f.command("claim"); code != 2 {
		t.Fatal("repinned unsigned host allowance admitted", code)
	}
	f.approval = original
	f.sign()
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"start", "--approval", f.path, "--accept-approval-hash", monitorReadDigest(raw), "--independent-public-key", f.key}
	if code := runRootPassiveHostCommandWithHost(t.Context(), args, io.Discard, io.Discard, func() time.Time { return f.now }, f.host); code != 2 {
		t.Fatal("public start lacks separate effect acknowledgment", code)
	}
	if code := runMain(t.Context(), []string{"activate-root-passive", "start", "--native-signature", "synthetic"}, io.Discard, io.Discard); code != 2 {
		t.Fatal("passive host exposed native authority flags", code)
	}
	if _, err := os.Stat(f.approval.Plan.StatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected authority created a claim", err)
	}
}

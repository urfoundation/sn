// The Linux action boundary uses a pinned systemctl without a shell. Local
// protected files and manager properties are assertions by the approved host.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/sys/unix"
)

// Only the private fixture changes filesystem roots and the command transport.
// Public selection always uses root-owned releases and the system manager.
type repairValidatorHost struct {
	rootUid     uint32
	trustRoot   string
	machinePath string
	bootPath    string
	cgroupRoot  string
	cgroupType  func(string) (int64, error)
	execute     func(context.Context, string, []string) ([]byte, error)
	monotonic   func() (uint64, error)
}

// Output is bounded independently of process lifetime. A writer error cancels
// the command through its copy/Wait path; cancellation kills the process group.
type repairValidatorOutput struct {
	buffer bytes.Buffer
}

// The systemctl response and diagnostic stream each have a fixed bound.
func (self *repairValidatorOutput) Write(raw []byte) (int, error) {
	if len(raw) > 32*1024-self.buffer.Len() {
		return 0, errors.New("validator repair command output exceeds its bound")
	}
	return self.buffer.Write(raw)
}

// This owns one actual child until Wait joins it, including cancellation. A
// canceled start may already have reached systemd and stays consumed upstream.
func executeRepairValidatorCommand(ctx context.Context, path string, args []string) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, errors.New("validator repair command context is unavailable")
	}
	command := exec.CommandContext(ctx, path, args...)
	command.Env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "SYSTEMD_PAGER=", "SYSTEMD_COLORS=0"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = time.Second
	var stdout, stderr repairValidatorOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, errors.Join(errors.New("validator repair systemctl failed or was interrupted"), err, ctx.Err())
	}
	return stdout.buffer.Bytes(), ctx.Err()
}

// No environment-controlled socket, remote host or user manager is selectable.
func newRepairValidatorHost() *repairValidatorHost {
	return &repairValidatorHost{rootUid: 0, trustRoot: "/", machinePath: "/etc/machine-id", bootPath: "/proc/sys/kernel/random/boot_id", cgroupRoot: "/sys/fs/cgroup", cgroupType: repairValidatorCgroupType, execute: executeRepairValidatorCommand, monotonic: func() (uint64, error) {
		var stamp unix.Timespec
		if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &stamp); err != nil {
			return 0, err
		}
		return uint64(stamp.Sec)*1000000 + uint64(stamp.Nsec)/1000, nil
	}}
}

// A missing path in v1, a hybrid layout or an unmounted filesystem proves
// nothing about a descendant process. Public selection requires genuine v2.
func repairValidatorCgroupType(path string) (int64, error) {
	var state unix.Statfs_t
	if err := unix.Statfs(path, &state); err != nil {
		return 0, err
	}
	return int64(state.Type), nil
}

// Every ancestor inside the trusted root must be a protected physical directory.
// A producer may own its output directory, never a release or custody directory.
func (self *repairValidatorHost) parents(path string, owner uint32) error {
	directory := filepath.Dir(path)
	if self.trustRoot != "/" && directory != self.trustRoot && !strings.HasPrefix(directory, self.trustRoot+"/") {
		return errors.New("validator repair path escapes the trusted host root")
	}
	for {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return errors.New("validator repair path has an unprotected ancestor")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != self.rootUid && stat.Uid != owner {
			return errors.New("validator repair path owner differs")
		}
		if directory == self.trustRoot {
			return nil
		}
		if directory == "/" {
			return errors.New("validator repair trusted root is unavailable")
		}
		directory = filepath.Dir(directory)
	}
}

// Hash large binaries without retaining them. Size, inode, mode, owner and
// modification time are checked around the read; deployment owns immutability.
func (self *repairValidatorHost) pin(ctx context.Context, reference planFileReference, limit int64, executable bool) (resultErr error) {
	if ctx == nil || ctx.Err() != nil {
		return errors.New("validator repair release read canceled")
	}
	if err := self.parents(reference.Path, self.rootUid); err != nil {
		return err
	}
	fd, err := syscall.Open(reference.Path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), reference.Path)
	defer func() { resultErr = errors.Join(resultErr, file.Close(), ctx.Err()) }()
	before, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != self.rootUid || !before.Mode().IsRegular() || before.Mode().Perm()&0022 != 0 || before.Size() < 1 || before.Size() > limit || executable && before.Mode().Perm()&0111 == 0 || before.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return errors.New("validator repair release file is not protected")
	}
	hash := sha256.New()
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := file.Read(buffer)
		total += int64(n)
		if total > limit {
			return errors.New("validator repair release grew beyond its bound")
		}
		_, _ = hash.Write(buffer[:n])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	after, err := file.Stat()
	named, nameErr := os.Lstat(reference.Path)
	if err != nil || nameErr != nil || !os.SameFile(before, named) || !os.SameFile(before, after) || before.Mode() != named.Mode() || before.Mode() != after.Mode() || before.Size() != total || before.Size() != after.Size() || before.ModTime() != after.ModTime() || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != reference.Sha256 {
		return errors.New("validator repair release pin changed")
	}
	return self.parents(reference.Path, self.rootUid)
}

// Operational records retain the producer's distinct owner and strict decoder.
func (self *repairValidatorHost) read(ctx context.Context, path string, owner uint32, limit int64, private bool) ([]byte, error) {
	if err := self.parents(path, owner); err != nil {
		return nil, err
	}
	return readMonitorServiceFile(ctx, path, limit, private, monitorServiceReadHooks{afterRead: func(file *os.File) error {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != owner {
			return errors.New("validator repair evidence owner differs")
		}
		return nil
	}})
}

// Properties are requested explicitly and duplicates/omissions are refused.
var repairValidatorProperties = []string{"Id", "LoadState", "FragmentPath", "DropInPaths", "NeedDaemonReload", "Transient", "Type", "User", "Group", "WorkingDirectory", "Restart", "KillMode", "Delegate", "Slice", "ExecStart", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "ExecReload", "Environment", "EnvironmentFiles", "PassEnvironment", "RootDirectory", "RootImage", "Wants", "Requires", "Requisite", "BindsTo", "Conflicts", "OnFailure", "OnSuccess", "Triggers", "TriggeredBy", "PartOf", "Upholds", "Job", "ControlPID", "MainPID", "ExecMainPID", "ExecMainStartTimestampMonotonic", "ActiveState", "SubState", "InvocationID", "ControlGroup"}

// A current manager snapshot is scoped to this host boot and fixed unit.
type repairValidatorManager struct {
	Generation repairValidatorGeneration
	MainPid    uint32
	Active     string
	SubState   string
	Cgroup     string
}

// Each manager call has its own bounded deadline under caller cancellation.
func (self *repairValidatorHost) command(ctx context.Context, plan repairValidatorPlan, args ...string) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, errors.New("validator repair manager context unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(plan.CommandTimeoutSeconds)*time.Second)
	defer cancel()
	return self.execute(ctx, plan.Systemctl.Path, args)
}

// A canonical on-disk unit is insufficient if systemd has stale or overridden
// configuration. Check the manager's resolved execution and dependency profile.
func (self *repairValidatorHost) inspect(ctx context.Context, plan repairValidatorPlan) (repairValidatorManager, error) {
	var result repairValidatorManager
	for _, reference := range []planFileReference{plan.Unit.File, plan.Unit.Binary, plan.Unit.Config, plan.Systemctl} {
		limit := int64(16 * 1024 * 1024)
		executable := reference.Path == plan.Unit.Binary.Path || reference.Path == plan.Systemctl.Path
		if executable {
			limit = 512 * 1024 * 1024
		}
		if err := self.pin(ctx, reference, limit, executable); err != nil {
			return result, err
		}
	}
	machine, err := self.read(ctx, self.machinePath, self.rootUid, 128, false)
	if err != nil || strings.TrimSpace(string(machine)) != plan.MachineId {
		return result, errors.New("validator repair machine identity differs")
	}
	// procfs pseudo-files report size zero; the kernel-owned fixed path has its
	// own finite read instead of pretending to be an ordinary evidence file.
	boot, err := os.Open(self.bootPath)
	if err != nil {
		return result, err
	}
	bootRaw, readErr := io.ReadAll(io.LimitReader(boot, 129))
	if err := errors.Join(readErr, boot.Close(), ctx.Err()); err != nil || len(bootRaw) > 128 || strings.TrimSpace(string(bootRaw)) != plan.BootId {
		return result, errors.New("validator repair host boot differs")
	}
	if err := self.parents(filepath.Join(plan.Unit.StateDirectory, "owned"), plan.Unit.Uid); err != nil {
		return result, err
	}
	state, err := os.Lstat(plan.Unit.StateDirectory)
	if err != nil {
		return result, err
	}
	stateStat, ok := state.Sys().(*syscall.Stat_t)
	if !ok || stateStat.Uid != plan.Unit.Uid || stateStat.Gid != plan.Unit.Gid || !state.IsDir() || state.Mode().Perm()&0077 != 0 {
		return result, errors.New("validator repair state directory owner differs")
	}
	raw, err := self.command(ctx, plan, "--system", "--no-pager", "show", "--all", "--property="+strings.Join(repairValidatorProperties, ","), "--", plan.Unit.Name)
	if err != nil || len(raw) > 32*1024 {
		return result, errors.Join(errors.New("validator repair manager snapshot unavailable"), err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		key, value, found := strings.Cut(line, "=")
		if _, exists := values[key]; !found || exists {
			return result, errors.New("validator repair manager snapshot is ambiguous")
		}
		values[key] = value
	}
	if len(values) != len(repairValidatorProperties) {
		return result, errors.New("validator repair manager snapshot is incomplete")
	}
	for _, key := range repairValidatorProperties {
		if _, exists := values[key]; !exists {
			return result, errors.New("validator repair manager property is missing")
		}
	}
	expected := map[string]string{"Id": plan.Unit.Name, "LoadState": "loaded", "FragmentPath": plan.Unit.File.Path, "NeedDaemonReload": "no", "Transient": "no", "Type": "exec", "User": strconv.FormatUint(uint64(plan.Unit.Uid), 10), "Group": strconv.FormatUint(uint64(plan.Unit.Gid), 10), "WorkingDirectory": plan.Unit.StateDirectory, "Restart": "no", "KillMode": "control-group", "Delegate": "no", "Job": "0", "ControlPID": "0"}
	expected["Slice"] = "system.slice"
	for _, key := range []string{"DropInPaths", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "ExecReload", "Environment", "EnvironmentFiles", "PassEnvironment", "RootDirectory", "RootImage", "Wants", "Requisite", "BindsTo", "Conflicts", "OnFailure", "OnSuccess", "Triggers", "TriggeredBy", "PartOf", "Upholds"} {
		expected[key] = ""
	}
	for key, value := range expected {
		if values[key] != value {
			return result, fmt.Errorf("validator repair loaded unit property differs: %s", key)
		}
	}
	// WorkingDirectory and the default slice add implicit requirements even
	// with DefaultDependencies=no. Only signed mounts and the system slice
	// may appear, and every prerequisite must already be active without a job.
	required := append(slices.Clone(plan.RequiredMounts), "system.slice")
	slices.Sort(required)
	actual := strings.Fields(values["Requires"])
	slices.Sort(actual)
	if !slices.Equal(required, actual) {
		return result, errors.New("validator repair loaded requirements differ")
	}
	for _, dependency := range required {
		raw, err := self.command(ctx, plan, "--system", "--no-pager", "show", "--all", "--property=Id,LoadState,ActiveState,Job", "--", dependency)
		lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
		slices.Sort(lines)
		if err != nil || !slices.Equal(lines, []string{"ActiveState=active", "Id=" + dependency, "Job=0", "LoadState=loaded"}) {
			return result, errors.New("validator repair prerequisite is not already active")
		}
	}
	expectedStart := "{ path=" + plan.Unit.Binary.Path + " ; argv[]=" + plan.Unit.Binary.Path + " run --config=" + plan.Unit.Config.Path + " --progress-file=" + plan.Unit.ProgressFile + " ; ignore_errors=no ;"
	if !strings.HasPrefix(values["ExecStart"], expectedStart) || strings.Count(values["ExecStart"], "{") != 1 || strings.Count(values["ExecStart"], "}") != 1 || !strings.HasSuffix(values["ExecStart"], " }") {
		return result, errors.New("validator repair loaded execution differs")
	}
	pid, pidErr := strconv.ParseUint(values["ExecMainPID"], 10, 32)
	mainPid, mainErr := strconv.ParseUint(values["MainPID"], 10, 32)
	started, startErr := strconv.ParseUint(values["ExecMainStartTimestampMonotonic"], 10, 64)
	if pidErr != nil || mainErr != nil || startErr != nil || strconv.FormatUint(pid, 10) != values["ExecMainPID"] || strconv.FormatUint(mainPid, 10) != values["MainPID"] || strconv.FormatUint(started, 10) != values["ExecMainStartTimestampMonotonic"] {
		return result, errors.New("validator repair manager generation is invalid")
	}
	result = repairValidatorManager{Generation: repairValidatorGeneration{InvocationId: values["InvocationID"], Pid: uint32(pid), StartedUsec: started}, MainPid: uint32(mainPid), Active: values["ActiveState"], SubState: values["SubState"], Cgroup: values["ControlGroup"]}
	return result, nil
}

// cgroup.events reports descendant population too; an empty cgroup.procs alone
// would miss a live signer in a child group. A removed stopped group is empty.
func (self *repairValidatorHost) stopped(ctx context.Context, plan repairValidatorPlan, manager repairValidatorManager) error {
	if self.cgroupType == nil {
		return errors.New("validator repair cgroup filesystem verifier is unavailable")
	}
	filesystem, err := self.cgroupType(self.cgroupRoot)
	if err != nil || filesystem != unix.CGROUP2_SUPER_MAGIC {
		return errors.New("validator repair requires a genuine unified cgroup filesystem")
	}
	prior := plan.Previous
	if manager.MainPid != 0 || manager.Active != "inactive" && manager.Active != "failed" || manager.SubState != "dead" && manager.SubState != "failed" || manager.Generation.Pid != prior.Pid || manager.Generation.StartedUsec != prior.StartedUsec || manager.Generation.InvocationId != "" && manager.Generation.InvocationId != prior.InvocationId {
		return errors.New("validator repair did not observe the approved stopped generation")
	}
	expected := "/system.slice/" + plan.Unit.Name
	if manager.Cgroup != "" && manager.Cgroup != expected {
		return errors.New("validator repair cgroup differs")
	}
	path := filepath.Join(self.cgroupRoot, expected)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ctx.Err()
	}
	if err != nil || !info.IsDir() {
		return errors.New("validator repair cgroup is unavailable")
	}
	read := func(name string) ([]byte, error) {
		fd, err := syscall.Open(filepath.Join(path, name), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(fd), name)
		raw, readErr := io.ReadAll(io.LimitReader(file, 4097))
		err = errors.Join(readErr, file.Close(), ctx.Err())
		if len(raw) > 4096 {
			err = errors.Join(err, errors.New("validator repair cgroup exceeds read bound"))
		}
		return raw, err
	}
	events, eventsErr := read("cgroup.events")
	procs, procsErr := read("cgroup.procs")
	populated := ""
	for _, line := range strings.Split(strings.TrimSpace(string(events)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[0] == "populated" && populated != "" {
			return errors.New("validator repair cgroup population is ambiguous")
		}
		if parts[0] == "populated" {
			populated = parts[1]
		}
	}
	if eventsErr != nil || procsErr != nil || populated != "0" || len(bytes.TrimSpace(procs)) != 0 {
		return errors.New("validator repair cgroup is populated or unavailable")
	}
	return nil
}

// The current monitor must retain the same open episode and prior producer;
// neither a fresh file nor an arbitrary HTTP success grants service authority.
func (self *repairValidatorHost) incident(ctx context.Context, plan repairValidatorPlan, now time.Time) (time.Time, error) {
	raw, err := self.read(ctx, plan.MonitorCheckpoint, plan.MonitorUid, maxMonitorServiceCheckpointBytes, true)
	if err != nil {
		return time.Time{}, err
	}
	var current monitorServiceCheckpointRecord
	if err := decodePlanJson(raw, &current); err != nil {
		return time.Time{}, err
	}
	if err := plan.incident(current); err != nil {
		return time.Time{}, err
	}
	if current.State.Record.InstanceId != plan.Original.State.Record.InstanceId || current.State.SampleAt.Before(plan.Original.State.SampleAt) || current.State.SampleAt.After(now) || now.Sub(current.State.SampleAt) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second || !current.State.ClockFaultAt.IsZero() {
		return time.Time{}, errors.New("validator repair incident is stale or changed")
	}
	return current.State.SampleAt, nil
}

// A finite acknowledged command is the only concrete process mutation. No
// restart, stop, daemon reload, enable, dependency repair or reset-failed exists.
func (self *repairValidatorHost) start(ctx context.Context, plan repairValidatorPlan) error {
	_, err := self.command(ctx, plan, "--system", "--no-pager", "--no-ask-password", "--job-mode=fail", "start", "--", plan.Unit.Name)
	return err
}

// Success means this new exact producer published fresh process progress. Its
// chain observations and pending economic liabilities keep their own meaning.
func (self *repairValidatorHost) progress(ctx context.Context, plan repairValidatorPlan, startedAt, now time.Time) (*repairValidatorPostcondition, error) {
	raw, err := self.read(ctx, plan.Unit.ProgressFile, plan.Unit.Uid, protocol.MaxValidatorProgressBytes, false)
	if err != nil {
		return nil, err
	}
	value, err := protocol.DecodeValidatorProgress(raw)
	if err != nil || value.Source != plan.Source || value.InstanceId == plan.Original.State.Record.InstanceId || monitorProgressTime(value.StartedAt).Before(startedAt) || monitorProgressMaximumTime(value).After(now) || now.Sub(monitorProgressTime(value.HeartbeatAt)) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second || value.Publisher.Outcome != "published" || monitorProgressTime(value.Publisher.LastSuccessAt).Before(startedAt) {
		return nil, errors.New("validator repair fresh exact producer postcondition is unavailable")
	}
	canonical, err := value.Encode()
	if err != nil {
		return nil, err
	}
	return &repairValidatorPostcondition{ObservedAt: now, InstanceId: value.InstanceId, RecordHash: monitorReadDigest(canonical)}, nil
}

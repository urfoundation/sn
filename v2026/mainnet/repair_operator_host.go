// Actual operator postconditions join the acknowledged taskworker generation,
// its owned listening socket and /status readiness to retained original custody.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durableinspect"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The separately pinned SN inspector runs read-only under the taskworker's
// credentials. The taskworker binary never receives an invented SN subcommand.
func (self *repairOperatorCustody) inspectStorage(ctx context.Context) (resultErr error) {
	p := self.envelope.original.Plan
	if err := requireUnitDurableReference(ctx, &p.DurableVolumes); err != nil {
		return err
	}
	paths := make([]string, 0, len(p.WritableRoots))
	for _, root := range p.WritableRoots {
		paths = append(paths, root.Path)
	}
	file, err := self.host.openPinned(ctx, p.Inspector, 512*1024*1024, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	args := []string{"storage-inspect", "--durable-volumes", p.DurableVolumes.Path, "--durable-volumes-sha256", p.DurableVolumes.Sha256}
	for _, path := range paths {
		args = append(args, "--directory", path)
	}
	owner, cancel := context.WithTimeout(ctx, time.Duration(self.envelope.approval.Plan.timeoutSeconds())*time.Second)
	defer cancel()
	command := exec.CommandContext(owner, "/proc/self/fd/3", args...)
	command.Args[0], command.ExtraFiles = p.Inspector.Path, []*os.File{file}
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: &syscall.Credential{Uid: p.Uid, Gid: p.Gid, Groups: []uint32{}}}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = time.Second
	stdout, stderr := serviceStorageInspectionOutput{cancel: cancel}, serviceStorageInspectionOutput{cancel: cancel}
	command.Stdout, command.Stderr = &stdout, &stderr
	var commandErr error
	if self.host.storageCommand != nil {
		commandErr = self.host.storageCommand(owner, command)
	} else if os.Geteuid() != 0 {
		return errors.New("operator storage inspection requires the approved root service controller")
	} else {
		commandErr = command.Run()
	}
	if err := errors.Join(commandErr, stdout.err, stderr.err, owner.Err()); err != nil {
		var exit *exec.ExitError
		if errors.As(commandErr, &exit) {
			if exit.ExitCode() == 3 {
				return errors.Join(durablevolume.ErrIdentity, err)
			}
			if exit.ExitCode() == 4 {
				return errors.Join(&durablevolume.UnavailableError{Reason: "operator original storage is temporarily unavailable"}, err)
			}
		}
		return errors.Join(errors.New("operator original credential-scoped storage inspection failed"), err)
	}
	if err := durableinspect.Validate(stdout.buffer.Bytes(), p.DurableVolumes, p.Uid, p.Gid, paths); err != nil {
		return err
	}
	return errors.Join(requireUnitDurableReference(ctx, &p.DurableVolumes), self.host.pin(ctx, p.Inspector, 512*1024*1024, true), ctx.Err())
}

// Proc pseudo-files have zero reported size. This finite owner reads their
// actual bytes and joins close; it never treats an unread value as a mismatch.
func readRepairOperatorProc(ctx context.Context, path string, maximum int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, repairValidatorObservationError("cannot read operator process evidence", err, false)
	}
	file := os.NewFile(uintptr(fd), path)
	raw, readErr := io.ReadAll(io.LimitReader(file, maximum+1))
	if err := errors.Join(readErr, file.Close(), ctx.Err()); err != nil {
		return nil, repairValidatorObservationError("cannot read operator process evidence", err, false)
	}
	if int64(len(raw)) > maximum {
		return nil, errors.Join(errRpcIntegrity, errors.New("operator process evidence exceeds its bound"))
	}
	return raw, nil
}

// The same PID must retain its kernel start identity, actual executable, exact
// argv, explicit environment and approved cgroup during the live observation.
func (self *repairOperatorCustody) processIdentity(ctx context.Context, manager repairValidatorManager) (string, error) {
	p := self.envelope.original.Plan
	root := filepath.Join(self.envelope.procRoot, strconv.FormatUint(uint64(manager.MainPid), 10))
	stamp, err := readRepairOperatorProc(ctx, filepath.Join(root, "stat"), 16*1024)
	if err != nil {
		return "", err
	}
	end := bytes.LastIndex(stamp, []byte(") "))
	if end < 1 {
		return "", errors.Join(errRpcIntegrity, errors.New("operator process start identity is invalid"))
	}
	fields := strings.Fields(string(stamp[end+2:]))
	pid := strings.SplitN(string(stamp), " ", 2)[0]
	if len(fields) < 20 || pid != strconv.FormatUint(uint64(manager.MainPid), 10) {
		return "", errors.Join(errRpcIntegrity, errors.New("operator process start identity differs"))
	}
	started, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || started == 0 {
		return "", errors.Join(errRpcIntegrity, errors.New("operator process lifetime is invalid"))
	}
	actual, actualErr := os.Stat(filepath.Join(root, "exe"))
	expected, expectedErr := os.Lstat(p.Binary.Path)
	if err := errors.Join(actualErr, expectedErr, ctx.Err()); err != nil {
		return "", repairValidatorObservationError("cannot observe operator executable", err, false)
	}
	if !os.SameFile(actual, expected) {
		return "", errors.Join(errRpcIntegrity, errors.New("operator generation executes a different binary"))
	}
	argv, err := readRepairOperatorProc(ctx, filepath.Join(root, "cmdline"), 8192)
	if err != nil {
		return "", err
	}
	expectedArgs := append([]string{p.Binary.Path}, strings.Fields(p.arguments())...)
	if !bytes.Equal(argv, []byte(strings.Join(expectedArgs, "\x00")+"\x00")) {
		return "", errors.Join(errRpcIntegrity, errors.New("operator actual argv differs from the original taskworker"))
	}
	environment, err := readRepairOperatorProc(ctx, filepath.Join(root, "environ"), 128*1024)
	if err != nil {
		return "", err
	}
	values := map[string]string{}
	for _, word := range strings.Split(strings.TrimSuffix(string(environment), "\x00"), "\x00") {
		name, value, found := strings.Cut(word, "=")
		if !found || name == "" {
			return "", errors.Join(errRpcIntegrity, errors.New("operator actual environment is invalid"))
		}
		if _, exists := values[name]; exists {
			return "", errors.Join(errRpcIntegrity, errors.New("operator actual environment repeats an input"))
		}
		values[name] = value
		if (strings.HasPrefix(name, "WARP_") || strings.HasPrefix(name, "BRINGYOUR_") || strings.HasPrefix(name, "URNETWORK_") || strings.HasPrefix(name, "APEX_") || strings.HasPrefix(name, "PG") || name == "LD_PRELOAD" || name == "LD_LIBRARY_PATH") && p.env(name) != value {
			return "", errors.Join(errRpcIntegrity, errors.New("operator actual environment contains an unapproved source override"))
		}
	}
	if values["INVOCATION_ID"] != manager.Generation.InvocationId {
		return "", errors.Join(errRpcIntegrity, errors.New("operator process does not retain its acknowledged systemd invocation"))
	}
	for _, value := range p.Environment {
		if values[value.Name] != value.Value {
			return "", errors.Join(errRpcIntegrity, errors.New("operator actual process lost an original environment input"))
		}
	}
	credentials, err := readRepairOperatorProc(ctx, filepath.Join(root, "status"), 64*1024)
	if err != nil {
		return "", err
	}
	for name, expected := range map[string]uint32{"Uid": p.Uid, "Gid": p.Gid} {
		found := false
		for _, line := range strings.Split(string(credentials), "\n") {
			key, value, exists := strings.Cut(line, ":")
			if !exists || key != name {
				continue
			}
			fields := strings.Fields(value)
			if found || len(fields) != 4 {
				return "", errors.Join(errRpcIntegrity, errors.New("operator process credential evidence is ambiguous"))
			}
			found = true
			for _, field := range fields {
				if field != strconv.FormatUint(uint64(expected), 10) {
					return "", errors.Join(errRpcIntegrity, errors.New("operator process no longer holds its original credentials"))
				}
			}
		}
		if !found {
			return "", errors.Join(errRpcIntegrity, errors.New("operator process credentials are absent"))
		}
	}
	// Loopback in another network namespace cannot attest this connection.
	// Join the taskworker and observer namespace before accepting its listener.
	namespace, namespaceErr := os.Stat(filepath.Join(root, "ns", "net"))
	observer, observerErr := os.Stat(filepath.Join(self.envelope.procRoot, "self", "ns", "net"))
	if err := errors.Join(namespaceErr, observerErr, ctx.Err()); err != nil {
		return "", repairValidatorObservationError("cannot observe operator readiness network namespace", err, false)
	}
	if !os.SameFile(namespace, observer) {
		return "", errors.Join(errRpcIntegrity, errors.New("operator readiness listener belongs to another network namespace"))
	}
	cgroup, err := readRepairOperatorProc(ctx, filepath.Join(root, "cgroup"), 4096)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(cgroup)) != "0::/system.slice/"+p.unitName() {
		return "", errors.Join(errRpcIntegrity, errors.New("operator process is outside its original service cgroup"))
	}
	return pid + ":" + fields[19], nil
}

// A healthy response from an unrelated local listener is not this taskworker's
// progress. Kernel socket ownership must join the port to the actual main PID.
func (self *repairOperatorCustody) ownsStatusSocket(ctx context.Context, manager repairValidatorManager) error {
	p := self.envelope.original.Plan
	address, selectedPort, err := p.statusEndpoint()
	if err != nil {
		return err
	}
	ip := net.ParseIP(address).To4()
	if ip == nil {
		return errors.Join(errRpcIntegrity, errors.New("operator readiness endpoint is not literal IPv4"))
	}
	local := fmt.Sprintf("%02X%02X%02X%02X", ip[3], ip[2], ip[1], ip[0])
	root := filepath.Join(self.envelope.procRoot, strconv.FormatUint(uint64(manager.MainPid), 10))
	fd, err := syscall.Open(filepath.Join(root, "fd"), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return repairValidatorObservationError("cannot observe operator listener ownership", err, false)
	}
	directory := os.NewFile(uintptr(fd), root)
	files, readErr := directory.ReadDir(4097)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, directory.Close(), ctx.Err()); err != nil {
		return repairValidatorObservationError("cannot observe operator listener ownership", err, false)
	}
	if len(files) > 4096 {
		return errors.Join(errRepairProcessPending, errors.New("operator descriptor census exceeds its observation bound"))
	}
	sockets := map[string]bool{}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		value, err := os.Readlink(filepath.Join(root, "fd", file.Name()))
		if err != nil {
			return repairValidatorObservationError("cannot read operator descriptor identity", err, false)
		}
		if strings.HasPrefix(value, "socket:[") && strings.HasSuffix(value, "]") {
			sockets[strings.TrimSuffix(strings.TrimPrefix(value, "socket:["), "]")] = true
		}
	}
	for _, family := range []string{"tcp", "tcp6"} {
		raw, err := readRepairOperatorProc(ctx, filepath.Join(root, "net", family), 1024*1024)
		if err != nil {
			return err
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 {
				return errors.Join(errRpcIntegrity, errors.New("operator kernel listener record is incomplete"))
			}
			address, port, found := strings.Cut(fields[1], ":")
			if !found || fields[3] != "0A" || port != fmt.Sprintf("%04X", selectedPort) || !sockets[fields[9]] {
				continue
			}
			if family == "tcp" && slices.Contains([]string{"00000000", local}, address) || family == "tcp6" && address == strings.Repeat("0", 32) {
				return nil
			}
		}
	}
	return errors.Join(errRepairProcessPending, errors.New("operator original generation does not own its readiness listener"))
}

// This is the taskworker's actual router.WarpStatusResult wire projection.
// Its latched readiness follows real DB/Redis admission and runtime construction.
type repairOperatorStatus struct {
	Version       *string `json:"version,omitempty"`
	ConfigVersion *string `json:"config_version,omitempty"`
	Status        string  `json:"status"`
	ClientAddress string  `json:"client_address"`
	Host          string  `json:"host"`
	Service       string  `json:"service"`
	Block         string  `json:"block"`
}

// The literal owned host address, no proxy, no redirect and one bounded body
// tie readiness to the inspected kernel listener under the same read owner.
func (self *repairOperatorCustody) status(ctx context.Context) (string, error) {
	p := self.envelope.original.Plan
	address, port, err := p.statusEndpoint()
	if err != nil {
		return "", errors.Join(errRpcIntegrity, err)
	}
	dialer := &net.Dialer{Timeout: 60 * time.Second}
	transport := &http.Transport{Proxy: nil, DialContext: dialer.DialContext, DisableKeepAlives: true, MaxConnsPerHost: 1, ResponseHeaderTimeout: 60 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(address, strconv.FormatUint(uint64(port), 10))+"/status", nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", errors.Join(errRepairProcessPending, err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
	if err := errors.Join(readErr, response.Body.Close(), ctx.Err()); err != nil {
		return "", errors.Join(errRepairProcessPending, err)
	}
	if response.StatusCode != http.StatusOK {
		return "", errRepairProcessPending
	}
	if len(raw) > 16*1024 {
		return "", errors.Join(errRpcIntegrity, errors.New("operator readiness response exceeds its bound"))
	}
	var status repairOperatorStatus
	if err := decodePlanJson(raw, &status); err != nil {
		return "", errors.Join(errRpcIntegrity, err)
	}
	if status.Version == nil || status.ConfigVersion == nil || *status.Version != p.env("WARP_VERSION") || *status.ConfigVersion != p.env("WARP_CONFIG_VERSION") || status.Host != p.env("WARP_HOST") || status.Service != "taskworker" || status.Block != p.env("WARP_BLOCK") {
		return "", errors.Join(errRpcIntegrity, errors.New("operator readiness reports a different installed source"))
	}
	if status.Status != "ok" {
		return "", errRepairProcessPending
	}
	return monitorReadDigest(raw), nil
}

// Completion means one acknowledged original process resumed and retained its
// signed history. It is neither canonical transaction success nor incident-clear
// authority for the independent monitor.
func (self *repairOperatorCustody) progress(ctx context.Context, startedAt, now time.Time) (string, error) {
	if now.Before(startedAt) {
		return "", errors.Join(errRpcIntegrity, errors.New("operator process observation clock moved backwards"))
	}
	if self.host.monotonic == nil {
		return "", errors.New("operator process observation clock is unavailable")
	}
	observedAt, err := self.host.monotonic()
	if err != nil {
		return "", repairValidatorObservationError("cannot read operator observation clock", err, false)
	}
	manager, err := self.inspect(ctx)
	if err != nil {
		return "", err
	}
	if !repairValidatorRunning(self.envelope.profile(), manager, manager.Generation) {
		return "", errRepairProcessPending
	}
	identity, err := self.processIdentity(ctx, manager)
	if err != nil {
		return "", err
	}
	if err := self.ownsStatusSocket(ctx, manager); err != nil {
		return "", err
	}
	status, err := self.status(ctx)
	if err != nil {
		return "", err
	}
	current, err := self.continuedCensus(ctx)
	if err != nil {
		return "", err
	}
	if err := inspectRepairOperatorRetained(ctx, self.host, self.envelope.original.Plan, self.envelope.approval.Plan.OriginalFiles, false); err != nil {
		return "", err
	}
	after, err := self.processIdentity(ctx, manager)
	if err != nil {
		return "", err
	}
	if identity != after {
		return "", errors.Join(errRpcIntegrity, errors.New("operator process changed while observing actual readiness"))
	}
	if err := self.ownsStatusSocket(ctx, manager); err != nil {
		return "", err
	}
	closedAt, err := self.host.monotonic()
	if err != nil {
		return "", repairValidatorObservationError("cannot close operator observation clock", err, false)
	}
	if closedAt < observedAt {
		return "", errors.Join(errRpcIntegrity, errors.New("operator process observation clock moved backwards"))
	}
	if closedAt-observedAt > uint64(self.envelope.approval.Plan.MaximumSampleAgeSeconds)*1000000 {
		return "", errors.Join(errRepairProcessPending, errors.New("operator complete process and database observation exceeds its original age bound"))
	}
	return rootObjectHash(struct {
		Generation repairValidatorGeneration `json:"generation"`
		Process    string                    `json:"process"`
		Status     string                    `json:"status"`
		Original   string                    `json:"original_census"`
		Current    string                    `json:"current_census"`
		Resources  string                    `json:"original_resources"`
	}{manager.Generation, identity, status, self.original.CensusHash, current.CensusHash, repairOperatorResourceHash(self.envelope.original.Plan)}), nil
}

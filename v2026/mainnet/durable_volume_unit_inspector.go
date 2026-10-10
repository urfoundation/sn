// Actual service credentials perform physical preflight through the approved
// release executable. The root installer never waives filesystem ownership.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durableinspect"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

type serviceStorageInspectionOutput struct {
	buffer bytes.Buffer
	cancel context.CancelFunc
	err    error
}

func (self *serviceStorageInspectionOutput) Write(raw []byte) (int, error) {
	if len(raw) > durableinspect.MaximumReportBytes-self.buffer.Len() {
		self.err = errors.New("service storage inspector output exceeds its fixed bound")
		self.cancel()
		return 0, self.err
	}
	return self.buffer.Write(raw)
}

// The approved runtime config determines every validator operator directory.
// Passive root monitoring owns only the checkpoint directory in its unit plan.
func (self *repairValidatorHost) serviceStorageDirectories(ctx context.Context, unit repairValidatorUnit, source []byte) ([]string, error) {
	if unit.Name == rootPassiveHostUnitName {
		return []string{unit.StateDirectory}, nil
	}
	if !strings.HasPrefix(unit.Name, "sn-mainnet-validator-") || !strings.HasSuffix(unit.Name, ".service") {
		return nil, errors.New("storage preflight service kind is not approved")
	}
	if source == nil {
		file, err := self.openPinned(ctx, unit.Config, 2*1024*1024, false)
		if err != nil {
			return nil, err
		}
		// The progress reader has a distinct 128 KiB limit. Read config
		// bytes from their pinned descriptor under its own finite bound.
		info, err := file.Stat()
		if err != nil || info.Size() < 1 || info.Size() > 2*1024*1024 {
			return nil, errors.Join(errors.New("storage inspector config changed its bounded size"), err, file.Close())
		}
		source = make([]byte, info.Size())
		for offset := 0; offset < len(source); {
			if err = ctx.Err(); err != nil {
				break
			}
			end := min(offset+64*1024, len(source))
			var count int
			count, err = file.ReadAt(source[offset:end], int64(offset))
			offset += count
			if err != nil {
				break
			}
		}
		if err := errors.Join(err, file.Close(), ctx.Err()); err != nil {
			return nil, err
		}
	}
	if monitorReadDigest(source) != unit.Config.Sha256 {
		return nil, errors.New("storage preflight runtime config differs from its exact unit pin")
	}
	paths, err := validator.ReleaseDurableDirectories(unit.Config.Path, source)
	if err != nil || len(paths) == 0 {
		return nil, errors.Join(errors.New("storage preflight runtime directories differ from the approved unit"), err)
	}
	if !slices.Contains(paths, unit.StateDirectory) {
		paths = append(paths, unit.StateDirectory)
	}
	return paths, nil
}

// No shell, inherited environment, extra groups, signer or route is selected.
// The child joins before installation/start can continue. Its pinned descriptor
// is read-only, and the only supported command opens ReadOnly physical guards.
func (self *repairValidatorHost) inspectServiceStorage(ctx context.Context, unit repairValidatorUnit, source []byte) (resultErr error) {
	if err := requireUnitDurableReference(ctx, unit.DurableVolumes); err != nil {
		return err
	}
	paths, err := self.serviceStorageDirectories(ctx, unit, source)
	if err != nil {
		return err
	}
	file, err := self.openPinned(ctx, unit.Binary, 512*1024*1024, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	args := []string{"storage-inspect", "--durable-volumes", unit.DurableVolumes.Path, "--durable-volumes-sha256", unit.DurableVolumes.Sha256}
	for _, path := range paths {
		args = append(args, "--directory", path)
	}
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, "/proc/self/fd/3", args...)
	command.Args[0] = unit.Binary.Path
	command.ExtraFiles = []*os.File{file}
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: &syscall.Credential{Uid: unit.Uid, Gid: unit.Gid, Groups: []uint32{}}}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = time.Second
	stdout := serviceStorageInspectionOutput{cancel: cancel}
	stderr := serviceStorageInspectionOutput{cancel: cancel}
	command.Stdout, command.Stderr = &stdout, &stderr
	var commandErr error
	if self.storageCommand != nil {
		commandErr = self.storageCommand(commandCtx, command)
	} else if os.Geteuid() != 0 {
		return errors.New("service credential preflight requires the root installer")
	} else {
		commandErr = command.Run()
	}
	if err := errors.Join(commandErr, stdout.err, stderr.err); err != nil {
		cause := errors.Join(err, commandCtx.Err(), errors.New(strings.TrimSpace(stderr.buffer.String())))
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if exit.ExitCode() == 3 {
				return errors.Join(durablevolume.ErrIdentity, cause)
			}
			if exit.ExitCode() == 4 {
				return errors.Join(&durablevolume.UnavailableError{Reason: "service storage inspection is temporarily unavailable"}, cause)
			}
		}
		return errors.Join(errors.New("approved service storage inspector failed"), cause)
	}
	if err := errors.Join(commandCtx.Err(), durableinspect.Validate(stdout.buffer.Bytes(), *unit.DurableVolumes, unit.Uid, unit.Gid, paths)); err != nil {
		return err
	}
	return errors.Join(requireUnitDurableReference(ctx, unit.DurableVolumes), self.pin(ctx, unit.Binary, 512*1024*1024, true))
}

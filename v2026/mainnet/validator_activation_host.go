// Static installation and the actual system manager transport share the
// qualified repair host. No dynamic Warp unit or shell is used.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type validatorActivationHost struct {
	host          *repairValidatorHost
	unitDirectory string
	afterStage    func(string) error
}

func newValidatorActivationHost() *validatorActivationHost {
	return &validatorActivationHost{host: newRepairValidatorHost(), unitDirectory: "/etc/systemd/system"}
}

// Both roles must be installed and either genuinely unused or the exact
// acknowledged invocation. A prior unacknowledged effect cannot be adopted.
func (self *validatorActivationHost) admit(ctx context.Context, plan validatorActivationPlan, record validatorActivationRecord) error {
	for i, unit := range record.Units {
		if !unit.Installed || filepath.Dir(plan.Units[i].Unit.File.Path) != self.unitDirectory {
			return errors.New("validator activation requires both exact installed static units")
		}
		profile := plan.hostPlan(i)
		if err := self.configReadable(profile.Unit); err != nil {
			return err
		}
		manager, err := self.host.inspect(ctx, profile)
		if err != nil {
			return err
		}
		if unit.StartAt.IsZero() {
			if err := self.host.stopped(ctx, profile, manager); err != nil {
				return err
			}
		} else if unit.Generation != nil {
			if !repairValidatorRunning(profile, manager, *unit.Generation) {
				return errors.New("validator activation retained unit generation differs")
			}
		}
	}
	return ctx.Err()
}

// Installation creates only signed static files, without enable/start. Existing
// exact files reconcile a crash after publication; different bytes never do.
func (self *validatorActivationHost) install(ctx context.Context, plan validatorActivationPlan, index int, source planFileReference) (resultErr error) {
	u := plan.Units[index].Unit
	if err := requireUnitDurableReference(ctx, u.DurableVolumes); err != nil {
		return err
	}
	if filepath.Dir(u.File.Path) != self.unitDirectory {
		return errors.New("validator activation unit is outside the fixed system directory")
	}
	for _, file := range []planFileReference{u.Binary, plan.Systemctl, plan.Preparation} {
		if err := self.host.pin(ctx, file, 512*1024*1024, file == u.Binary || file == plan.Systemctl); err != nil {
			return err
		}
	}
	machine, err := self.host.read(ctx, self.host.machinePath, self.host.rootUid, 128, false)
	if err != nil || strings.TrimSpace(string(machine)) != plan.MachineId {
		return errors.New("validator activation installation machine differs")
	}
	boot, err := os.Open(self.host.bootPath)
	if err != nil {
		return err
	}
	bootRaw, readErr := io.ReadAll(io.LimitReader(boot, 129))
	if err := errors.Join(readErr, boot.Close(), ctx.Err()); err != nil || len(bootRaw) > 128 || strings.TrimSpace(string(bootRaw)) != plan.BootId {
		return errors.New("validator activation installation boot differs")
	}
	raw, err := readBootstrapChainInput(ctx, source, 2*1024*1024)
	if err != nil || source.Sha256 != u.Config.Sha256 {
		return errors.Join(errors.New("validator activation runtime config source changed"), err)
	}
	if err := self.host.inspectServiceStorage(ctx, u, raw); err != nil {
		return err
	}
	if err := self.installFile(ctx, u.Config, raw, 0440, u.Gid); err != nil {
		return err
	}
	if err := self.configReadable(u); err != nil {
		return err
	}
	return self.installFile(ctx, u.File, u.render(), 0644, uint32(os.Getegid()))
}

// The copy is root-owned and readable only by its approved role group. Each
// physical parent must allow this service to traverse without gaining writes.
func (self *validatorActivationHost) configReadable(unit repairValidatorUnit) error {
	info, err := os.Lstat(unit.Config.Path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != self.host.rootUid || stat.Gid != unit.Gid || !info.Mode().IsRegular() || info.Mode().Perm() != 0440 {
		return errors.New("validator activation runtime config is not root-owned and role-group readable")
	}
	for directory := filepath.Dir(unit.Config.Path); ; directory = filepath.Dir(directory) {
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() {
			return errors.New("validator activation runtime config parent is unavailable")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("validator activation runtime config parent ownership unavailable")
		}
		mask := os.FileMode(0001)
		if stat.Uid == unit.Uid {
			mask = 0100
		} else if stat.Gid == unit.Gid {
			mask = 0010
		}
		if info.Mode().Perm()&mask == 0 {
			return errors.New("validator activation runtime config cannot be traversed by its service")
		}
		if directory == self.host.trustRoot {
			return nil
		}
		if directory == "/" {
			return errors.New("validator activation runtime config escaped its host root")
		}
	}
}

// Exact-byte create-only publication serves both runtime configs and units.
// Partial writes never acquire the final name; post-rename errors stay visible.
func (self *validatorActivationHost) installFile(ctx context.Context, reference planFileReference, raw []byte, mode os.FileMode, gid uint32) (resultErr error) {
	if len(raw) == 0 || len(raw) > 2*1024*1024 || monitorReadDigest(raw) != reference.Sha256 {
		return errors.New("validator activation install bytes differ from their independent pin")
	}
	if err := self.host.parents(reference.Path, self.host.rootUid); err != nil {
		return err
	}
	if _, err := os.Lstat(reference.Path); err == nil {
		return self.host.pin(ctx, reference, 2*1024*1024, false)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	directoryPath := filepath.Dir(reference.Path)
	fd, err := unix.Open(directoryPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), directoryPath)
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Uid != self.host.rootUid || stat.Mode&0022 != 0 {
		return errors.Join(errors.New("validator activation unit directory is unprotected"), err)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	stage := ".sn-validator-install-" + hex.EncodeToString(random[:]) + ".tmp"
	stageFd, err := unix.Openat(fd, stage, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(stageFd), stage)
	defer func() {
		file.Close()
		if err := unix.Unlinkat(fd, stage, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := errors.Join(file.Chown(int(self.host.rootUid), int(gid)), file.Chmod(mode)); err != nil {
		return err
	}
	n, writeErr := file.Write(raw)
	if n != len(raw) {
		writeErr = errors.Join(writeErr, io.ErrShortWrite)
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close(), ctx.Err()); err != nil {
		return err
	}
	if self.afterStage != nil {
		if err := self.afterStage(reference.Path); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Renameat2(fd, stage, fd, filepath.Base(reference.Path), unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	// A failure after rename is retained as install intent and re-admits exact
	// bytes on reopen. It never removes or overwrites the installed candidate.
	return errors.Join(directory.Sync(), self.host.pin(ctx, reference, 2*1024*1024, false), ctx.Err())
}

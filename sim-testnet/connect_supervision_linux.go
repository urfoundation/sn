package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type supervisedExecutableFileIdentity struct {
	Device uint64
	Inode  uint64
}

// Follow the kernel's mapped image, never the pathname returned by readlink.
// An unlinked or atomically replaced executable remains mapped to its original
// device/inode until this process exits or execs another image.
func readSupervisedProcessExecutableFile(pid int) (supervisedExecutableFileIdentity, error) {
	path := fmt.Sprintf("/proc/%d/exe", pid)
	info, err := os.Stat(path)
	if err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Ino == 0 {
			return supervisedExecutableFileIdentity{}, errors.New("mapped executable has no kernel file identity")
		}
		return supervisedExecutableFileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
	}
	if !os.IsPermission(err) {
		return supervisedExecutableFileIdentity{}, err
	}
	if err := requireSameUserSupervisedProcess(pid); err != nil {
		return supervisedExecutableFileIdentity{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "sudo", "-n", "--", "/usr/bin/stat", "--dereference", "--printf=%d:%i\\n", "--", path).Output()
	if err != nil {
		return supervisedExecutableFileIdentity{}, fmt.Errorf("stat same-user capability process %d executable: %w", pid, err)
	}
	return parseSupervisedProcessExecutableFile(output)
}

func parseSupervisedProcessExecutableFile(output []byte) (supervisedExecutableFileIdentity, error) {
	fields := strings.Split(strings.TrimSuffix(string(output), "\n"), ":")
	if len(fields) != 2 {
		return supervisedExecutableFileIdentity{}, errors.New("privileged executable stat returned an invalid file identity")
	}
	device, deviceErr := strconv.ParseUint(fields[0], 10, 64)
	inode, inodeErr := strconv.ParseUint(fields[1], 10, 64)
	if deviceErr != nil || inodeErr != nil || inode == 0 || strconv.FormatUint(device, 10) != fields[0] || strconv.FormatUint(inode, 10) != fields[1] {
		return supervisedExecutableFileIdentity{}, errors.New("privileged executable stat returned an invalid file identity")
	}
	return supervisedExecutableFileIdentity{Device: device, Inode: inode}, nil
}

// Linux denies /proc/PID/exe access for the private file-capability Connect
// image even to its same-user supervisor. Observe the same kernel link through
// a fixed, read-only privileged command after checking the process owner.
// This keeps its bind capability and the supervisor's real identity checks.
func readSupervisedProcessExecutable(pid int) (string, error) {
	path := fmt.Sprintf("/proc/%d/exe", pid)
	executable, err := os.Readlink(path)
	if err == nil || !os.IsPermission(err) {
		return executable, err
	}
	if err := requireSameUserSupervisedProcess(pid); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, privilegedErr := exec.CommandContext(ctx, "sudo", "-n", "--", "/usr/bin/readlink", "--", path).Output()
	if privilegedErr != nil {
		return "", fmt.Errorf("read same-user capability process %d executable: %w", pid, privilegedErr)
	}
	executable = strings.TrimSuffix(string(output), "\n")
	if executable == "" || strings.ContainsAny(executable, "\n\r\x00") {
		return "", errors.New("privileged executable observation returned an invalid path")
	}
	return executable, nil
}

func requireSameUserSupervisedProcess(pid int) error {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return err
	}
	wantUID := strconv.Itoa(os.Geteuid())
	owned := false
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 5 && fields[0] == "Uid:" {
			owned = fields[1] == wantUID && fields[2] == wantUID && fields[3] == wantUID && fields[4] == wantUID
			break
		}
	}
	if !owned {
		return fmt.Errorf("refuse privileged executable observation for process %d owned by another user", pid)
	}
	return nil
}

func copyProvisionalSimulatorBinary(cfg *ResolvedConfig, destination string) error {
	if !provisionalResumeEnabled(cfg) {
		return errors.New("provisional simulator image requires explicit resume admission")
	}
	driver := cfg.provisionalResume.Driver
	if driver.ExecutablePath != destination {
		if err := copyFile(driver.ExecutablePath, destination, 0o700); err != nil {
			return fmt.Errorf("copy admitted provisional simulator image: %w", err)
		}
	}
	hash, err := fileSHA256(destination)
	if err != nil {
		return err
	}
	if hash != driver.ExecutableSHA256 {
		return errors.New("provisional simulator image differs from admitted running executable")
	}
	return nil
}

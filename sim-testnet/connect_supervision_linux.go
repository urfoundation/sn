package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

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
	status, statusErr := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if statusErr != nil {
		return "", statusErr
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
		return "", fmt.Errorf("refuse privileged executable observation for process %d owned by another user", pid)
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

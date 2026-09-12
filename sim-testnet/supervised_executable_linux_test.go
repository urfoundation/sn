package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSupervisorShutdownJoinsReplacedExecutable(t *testing.T) {
	assertSupervisorShutdownJoinsUnlinkedExecutable(t, true)
}

func TestSupervisorShutdownJoinsDeletedExecutable(t *testing.T) {
	assertSupervisorShutdownJoinsUnlinkedExecutable(t, false)
}

// The old process keeps its mapped inode after its launch path disappears or
// refers to another inode. Exercise the real observer, phased shutdown, and
// Wait owner; a cleanup fallback cannot turn a missed SIGTERM into a pass.
func assertSupervisorShutdownJoinsUnlinkedExecutable(t *testing.T, replace bool) {
	t.Helper()
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "supervised-image")
	if err := copyFile("/bin/sleep", imagePath, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	spec := ProcessSpec{
		ID: "unlinked-image", Role: "validator", Command: imagePath, Args: []string{"300"}, WorkDir: dir,
		StdoutPath: filepath.Join(dir, "stdout.log"), StderrPath: filepath.Join(dir, "stderr.log"),
	}
	command, exited, err := startSpecWithExit(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		if !joined {
			_ = command.Process.Kill()
			select {
			case <-exited:
				joined = true
			case <-time.After(5 * time.Second):
				t.Error("failed regression child could not be joined")
			}
		}
	}()
	recorded, err := observeStartedSupervisedProcessIdentity(ctx, command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Executable != imagePath || recorded.ExecutableFile.Inode == 0 {
		t.Fatalf("initial executable identity: %+v", recorded)
	}
	if replace {
		replacement := filepath.Join(dir, "replacement-image")
		if err := copyFile("/bin/sleep", replacement, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, imagePath); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(imagePath)
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || recorded.ExecutableFile == (supervisedExecutableFileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}) {
			t.Fatal("replacement did not create a distinct executable inode")
		}
	} else if err := os.Remove(imagePath); err != nil {
		t.Fatal(err)
	}
	observed, err := observeSupervisedProcessIdentity(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Executable == recorded.Executable || !strings.HasSuffix(observed.Executable, " (deleted)") || observed.ExecutableFile != recorded.ExecutableFile {
		t.Fatalf("mapped executable replacement was not reproduced: before=%+v after=%+v", recorded, observed)
	}
	owned := supervisedCommand{spec: spec, cmd: command, identity: recorded}
	if !supervisedCommandAlive(owned) {
		t.Fatal("same running executable became ineligible after its launch path changed")
	}
	stopSupervisorCommands([]supervisedCommand{owned})
	select {
	case exitErr := <-exited:
		joined = true
		var status *exec.ExitError
		if !errors.As(exitErr, &status) {
			t.Fatalf("shutdown did not join a signalled child: %v", exitErr)
		}
		wait, ok := status.Sys().(syscall.WaitStatus)
		if !ok || !wait.Signaled() || wait.Signal() != syscall.SIGTERM {
			t.Fatalf("shutdown required a fallback instead of its owned SIGTERM: %v", exitErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor skipped the original executable and did not join it")
	}
	if supervisedCommandAlive(owned) || supervisedProcessGroupHasMembers(recorded) {
		t.Fatal("shutdown left the joined child or its process group live")
	}
}

func TestSupervisorShutdownBindsMappedExecutableAndKernelIdentity(t *testing.T) {
	recorded := supervisedProcessIdentity{
		PID: 31337, ProcessGroupID: 31337, StartTimeTicks: 100,
		Executable: "/owned/image", ExecutableFile: supervisedExecutableFileIdentity{Device: 7, Inode: 11},
		CommandLineHash: "owned-argv",
	}
	command := supervisedCommand{cmd: &exec.Cmd{Process: &os.Process{Pid: recorded.PID}}, identity: recorded}
	observed := recorded
	signals := 0
	observe := func(pid int) (supervisedProcessIdentity, error) {
		if pid != recorded.PID {
			t.Fatalf("unexpected observation pid=%d", pid)
		}
		return observed, nil
	}
	signalGroup := func(group int, signal syscall.Signal) error {
		signals++
		if group != -recorded.ProcessGroupID || signal != syscall.SIGTERM {
			t.Fatalf("unexpected signal group=%d signal=%v", group, signal)
		}
		return nil
	}
	for _, path := range []string{recorded.Executable, recorded.Executable + " (deleted)", "/renamed/image"} {
		observed = recorded
		observed.Executable = path
		if !signalSupervisedCommandWithObserver(command, syscall.SIGTERM, observe, signalGroup) || !supervisedCommandAliveWithObserver(command, observe) {
			t.Fatalf("same mapped image rejected after display path became %q", path)
		}
	}
	if signals != 3 {
		t.Fatalf("same-image signal count=%d", signals)
	}
	for _, changed := range []struct {
		name  string
		apply func(*supervisedProcessIdentity)
	}{
		{"pid", func(id *supervisedProcessIdentity) { id.PID++ }},
		{"start time", func(id *supervisedProcessIdentity) { id.StartTimeTicks++ }},
		{"process group", func(id *supervisedProcessIdentity) { id.ProcessGroupID++ }},
		{"argv", func(id *supervisedProcessIdentity) { id.CommandLineHash = "foreign-argv" }},
		{"mapped device", func(id *supervisedProcessIdentity) { id.ExecutableFile.Device++ }},
		{"mapped inode", func(id *supervisedProcessIdentity) { id.ExecutableFile.Inode++ }},
		{"missing mapped inode", func(id *supervisedProcessIdentity) { id.ExecutableFile.Inode = 0 }},
	} {
		observed = recorded
		changed.apply(&observed)
		if signalSupervisedCommandWithObserver(command, syscall.SIGTERM, observe, signalGroup) || supervisedCommandAliveWithObserver(command, observe) {
			t.Fatalf("changed %s authorized the original process group", changed.name)
		}
		if signals != 3 {
			t.Fatalf("changed %s received a signal", changed.name)
		}
	}
	// Two missing identities must not become authority just because they match.
	command.identity.ExecutableFile = supervisedExecutableFileIdentity{}
	observed = command.identity
	if signalSupervisedCommandWithObserver(command, syscall.SIGTERM, observe, signalGroup) || supervisedCommandAliveWithObserver(command, observe) || signals != 3 {
		t.Fatal("matching absent mapped identities authorized shutdown")
	}
	// The observed process must also be the command's originally recorded PID.
	command.identity = recorded
	command.identity.PID++
	command.identity.ProcessGroupID++
	observed = command.identity
	if signalSupervisedCommandWithObserver(command, syscall.SIGTERM, observe, signalGroup) || supervisedCommandAliveWithObserver(command, observe) || signals != 3 {
		t.Fatal("matching foreign observation bypassed the command PID fence")
	}
}

func TestSupervisorExecutableStatRejectsInexactIdentity(t *testing.T) {
	for _, valid := range []string{"7:11\n", "0:18446744073709551615\n"} {
		identity, err := parseSupervisedProcessExecutableFile([]byte(valid))
		if err != nil || identity.Inode == 0 {
			t.Fatalf("valid mapped file identity %q: %+v, %v", valid, identity, err)
		}
	}
	for _, invalid := range []string{"", "7", "7:0\n", "7:11:12\n", "7:11\n\n", "7:11\r\n", "+7:11\n", "07:11\n", "7:-11\n", "7:11 extra\n", "7:18446744073709551616\n"} {
		identity, err := parseSupervisedProcessExecutableFile([]byte(invalid))
		if err == nil || identity != (supervisedExecutableFileIdentity{}) {
			t.Fatalf("inexact mapped file identity %q accepted: %+v, %v", invalid, identity, err)
		}
	}
}

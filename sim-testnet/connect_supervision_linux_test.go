package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Reproduce the kernel consequence of exec'ing a file-capability binary in an
// isolated child; no privileged host changes, network, or server fixtures.
func TestConnectSupervisorInspection(t *testing.T) {
	if os.Getenv("SN_CONNECT_SUPERVISION_TEST_CHILD") == "1" {
		if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		fmt.Println("private")
		input := bufio.NewReader(os.Stdin)
		if _, err := input.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		if err := unix.Prctl(unix.PR_SET_DUMPABLE, 1, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		fmt.Println("inspectable")
		if _, err := input.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	withFileCapability := os.Getenv("SN_TEST_CONNECT_FILE_CAPABILITY") == "1"
	if withFileCapability {
		copyPath := filepath.Join(t.TempDir(), "capability-child")
		if err := copyFile(executable, copyPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := installConnectBindServiceCapability(ctx, copyPath); err != nil {
			t.Fatal(err)
		}
		executable = copyPath
	}
	command := exec.CommandContext(ctx, executable, "-test.run=^TestConnectSupervisorInspection$")
	command.Env = append(os.Environ(), "SN_CONNECT_SUPERVISION_TEST_CHILD=1")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		if !joined {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	reader := bufio.NewReader(output)
	readStage := func(want string) {
		t.Helper()
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != want {
			t.Fatalf("child stage=%q want=%q err=%v stderr=%s", line, want, err, stderr.String())
		}
	}
	readStage("private")
	_, privateErr := os.Readlink(fmt.Sprintf("/proc/%d/exe", command.Process.Pid))
	if privateErr != nil && !os.IsPermission(privateErr) {
		t.Fatalf("unexpected private executable observation: %v", privateErr)
	}
	if os.Geteuid() != 0 && !os.IsPermission(privateErr) {
		t.Fatal("non-dumpable child did not reproduce the same-user observation failure")
	}
	if _, err := fmt.Fprintln(input, "inspect"); err != nil {
		t.Fatal(err)
	}
	readStage("inspectable")
	if withFileCapability && os.Geteuid() != 0 {
		if _, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", command.Process.Pid)); !os.IsPermission(err) {
			t.Fatalf("file-capability child did not retain the commoncap observation restriction: %v", err)
		}
	}
	identity, err := observeSupervisedProcessIdentity(command.Process.Pid)
	if err != nil {
		t.Fatalf("supervisor cannot identify restored child: %v", err)
	}
	if identity.Executable != executable || identity.ProcessGroupID != command.Process.Pid || identity.StartTimeTicks == 0 || identity.CommandLineHash == "" {
		t.Fatalf("wrong child identity: %+v", identity)
	}
	if _, err := fmt.Fprintln(input, "exit"); err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	err = command.Wait()
	joined = true
	if err != nil {
		t.Fatalf("child exit: %v stderr=%s", err, stderr.String())
	}
}

func TestProvisionalSimulatorCopiesOnlyAdmittedImage(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "driver"), filepath.Join(dir, "component")
	if err := os.WriteFile(source, []byte("admitted image"), 0o700); err != nil {
		t.Fatal(err)
	}
	hash, err := fileSHA256(source)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ResolvedConfig{provisionalResume: &provisionalResumeState{
		Record: &provisionalResumeRecord{Provisional: true},
		Driver: provisionalDriverProvenance{ExecutablePath: source, ExecutableSHA256: hash},
	}}
	if err := copyProvisionalSimulatorBinary(cfg, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != "admitted image" {
		t.Fatalf("copied image=%q err=%v", got, err)
	}
	if err := os.WriteFile(source, []byte("changed image"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyProvisionalSimulatorBinary(cfg, destination); err == nil {
		t.Fatal("changed image was admitted")
	}
	if err := copyProvisionalSimulatorBinary(&ResolvedConfig{}, destination); err == nil {
		t.Fatal("strict invocation accepted a provisional image")
	}
}

//go:build linux

package nativefee

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func init() {
	if len(os.Args) != 0 && os.Args[0] == "urnetwork-native-fee-supervisor" {
		if len(os.Args) != 12 || os.Args[1] != "verify-native-fee-outcome" || os.Args[2] != "--request" || os.Args[4] != "--request-sha256" || os.Args[6] != "--transaction" || os.Args[8] != "--policy-json" || os.Args[10] != "--budget" {
			os.Exit(1)
		}
		os.Exit(supervise())
	}
}

// Subreaper state belongs to this fresh process, never the embedding server.
// Original runtime workers can use their own groups; orphan cleanup therefore
// enumerates our actual children instead of assuming a single process group.
func supervise() (exit int) {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	parent := os.Getppid()
	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(syscall.SIGTERM), 0, 0, 0); err != nil || parent == 1 || os.Getppid() != parent {
		return 1
	}
	verifier := os.NewFile(3, "retained-native-fee-verifier")
	defer func() {
		if verifier.Close() != nil {
			exit = 1
		}
	}()
	seals, err := unix.FcntlInt(verifier.Fd(), unix.F_GET_SEALS, 0)
	required := unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
	if err != nil || seals&required != required {
		return 1
	}
	command := exec.CommandContext(ctx, "/proc/self/fd/3", os.Args[1:]...)
	command.Args[0] = "urnetwork-native-fee-verifier"
	command.ExtraFiles = []*os.File{verifier}
	command.Env = []string{"LANG=C", "LC_ALL=C", "RUST_BACKTRACE=0"}
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := command.Process.Signal(syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 5 * time.Second
	if err := command.Start(); err != nil {
		return 1
	}
	result := command.Wait()
	hadDescendant, err := reapDescendants()
	if result != nil || err != nil || ctx.Err() != nil || hadDescendant {
		return 1
	}
	return 0
}

func reapDescendants() (bool, error) {
	hadDescendant := false
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if errors.Is(err, syscall.ECHILD) {
			return hadDescendant, nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return true, err
		}
		hadDescendant = true
		if pid != 0 {
			continue
		}
		// Reparented children may have created their own process groups. Only
		// children of this dedicated supervisor are eligible for termination.
		tasks, err := os.ReadDir("/proc/self/task")
		if err != nil {
			return true, err
		}
		for _, task := range tasks {
			raw, err := os.ReadFile("/proc/self/task/" + task.Name() + "/children")
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return true, err
			}
			for _, value := range strings.Fields(string(raw)) {
				child, err := strconv.Atoi(value)
				if err != nil || child <= 1 {
					return true, errors.New("native fee supervisor child identity invalid")
				}
				if err := syscall.Kill(child, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
					return true, err
				}
			}
		}
		// Every enumerated live child was signalled. Wait for progress without
		// polling; newly adopted grandchildren are handled on the next pass.
		for {
			_, err = syscall.Wait4(-1, &status, 0, nil)
			if !errors.Is(err, syscall.EINTR) {
				break
			}
		}
		if err != nil && !errors.Is(err, syscall.ECHILD) {
			return true, err
		}
	}
}

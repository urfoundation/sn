// A dedicated reexecuted process owns orphan reaping for one replay. Changing
// subreaper state in the main observer would capture unrelated service children.
package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// This fixed internal invocation accepts only an already retained executable
// descriptor. It grants no file selection, signer, network or approval policy.
func init() {
	if os.Args[0] == "urnetwork-historical-replay-supervisor" {
		if len(os.Args) != 2 {
			os.Exit(1)
		}
		switch os.Args[1] {
		case "--retained-engine-fd3":
			os.Exit(superviseHistoricalProof(false, false))
		case "--retained-capture-engine-fd3-nodes-fd5":
			os.Exit(superviseHistoricalProof(true, false))
		case "--retained-capture-engine-fd3-nodes-fd5-feed-fd6-fd7":
			os.Exit(superviseHistoricalProof(true, true))
		default:
			os.Exit(1)
		}
	}
}

// The child cannot pass a successful result while another process still owns
// its pipes. Kill and reap every descendant in this owned group before exit.
func superviseHistoricalReplay(capture bool) int {
	return superviseHistoricalProof(capture, false)
}

func superviseHistoricalProof(capture, feed bool) (exit int) {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	parent := os.Getppid()
	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(syscall.SIGTERM), 0, 0, 0); err != nil || parent == 1 || os.Getppid() != parent {
		return 1
	}
	engine := os.NewFile(3, "retained-replay-engine")
	defer func() {
		if engine.Close() != nil {
			exit = 1
		}
	}()
	argument := "--historical-proof-replay-v1"
	if capture {
		argument = "--historical-proof-capture-v1"
	}
	if feed {
		if !capture {
			return 1
		}
		argument = "--historical-proof-capture-feed-v1"
	}
	command := exec.CommandContext(ctx, "/proc/self/fd/3", argument)
	command.Args[0] = "urnetwork-historical-replay"
	command.ExtraFiles = []*os.File{engine}
	if capture {
		nodes := os.NewFile(5, "retained-capture-nodes")
		defer func() {
			if nodes.Close() != nil {
				exit = 1
			}
		}()
		info, err := nodes.Stat()
		if err != nil || !info.IsDir() {
			return 1
		}
		command.ExtraFiles = append(command.ExtraFiles, nodes)
	}
	if feed {
		request := os.NewFile(6, "retained-capture-request-pipe")
		response := os.NewFile(7, "retained-capture-response-pipe")
		defer func() {
			if errors.Join(request.Close(), response.Close()) != nil {
				exit = 1
			}
		}()
		for _, file := range []*os.File{request, response} {
			info, err := file.Stat()
			if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
				return 1
			}
		}
		command.ExtraFiles = append(command.ExtraFiles, request, response)
	}
	command.Env = []string{"LANG=C", "LC_ALL=C", "RUST_BACKTRACE=0"}
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if err := command.Start(); err != nil {
		return 1
	}
	result := command.Wait()
	hadDescendant := false
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if errors.Is(err, syscall.ECHILD) {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return 1
		}
		hadDescendant = true
		// The only producer of children in this fresh process is this exact
		// engine. Its descendants inherit that group; no shell is involved.
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return 1
		}
		if pid == 0 {
			for {
				_, err = syscall.Wait4(-1, &status, 0, nil)
				if !errors.Is(err, syscall.EINTR) {
					break
				}
			}
			if err != nil && !errors.Is(err, syscall.ECHILD) {
				return 1
			}
		}
	}
	if result != nil || ctx.Err() != nil || hadDescendant {
		return 1
	}
	return 0
}

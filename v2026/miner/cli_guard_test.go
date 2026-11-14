package miner

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
)

func TestServiceRefusesASecondMinerOnTheSameIdentity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd")
	}
	phase2State(t)
	fakeExecutable(t)
	var calls []string
	cliRun = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	}
	defer func() { cliRun = defaultCliRun }()
	// a foreground provider start (this process) is mining this state folder
	writeLocalStatus(t, true, time.Minute)
	var out, errOut bytes.Buffer
	if code := serviceCmd(docopt.Opts{"install": true}, &out, &errOut); code == 0 {
		t.Fatal("installed a service beside a running miner")
	}
	if !strings.Contains(errOut.String(), "already running") {
		t.Fatalf("unexpected message: %s", errOut.String())
	}
	if newSystemdService(false).installed() || 0 < len(calls) {
		t.Fatalf("something was changed: %v", calls)
	}
}

func TestServiceValuesCantAddLines(t *testing.T) {
	if problem := serviceValueProblem("/opt/urnetwork/bin/provider", "URNETWORK_UPDATE_URL=https://example.test/feed.json"); problem != "" {
		t.Fatal(problem)
	}
	for _, value := range []string{"/tmp/x\nExecStartPre=/bin/sh", "URNETWORK_UPDATE_URL=a\r\nUser=root", "a\x00b"} {
		if serviceValueProblem("/bin/provider", value) == "" {
			t.Fatalf("%q was accepted", value)
		}
	}
	if runtime.GOOS != "linux" {
		return
	}
	phase2State(t)
	fakeExecutable(t)
	cliRun = func(name string, args ...string) (string, error) { return "", nil }
	defer func() { cliRun = defaultCliRun }()
	t.Setenv("URNETWORK_UPDATE_URL", "x\nExecStartPre=/bin/false")
	var out, errOut bytes.Buffer
	if code := serviceCmd(docopt.Opts{"install": true}, &out, &errOut); code == 0 || newSystemdService(false).installed() {
		t.Fatal("a value with a line break went into the unit file")
	}
}

func TestRootDoesntUpdateAnotherUsersMiner(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("unix ownership")
	}
	if os.Geteuid() != 0 {
		if problem := rootUpdateProblem("/nonexistent/provider"); problem != "" {
			t.Fatal("only root is held to this:", problem)
		}
		t.Skip("needs root to see root's view")
	}
	phase2State(t)
	executable := fakeExecutable(t)
	if problem := rootUpdateProblem(executable); problem != "" {
		t.Fatal("root's own binary refused:", problem)
	}
	// the binary's folder belongs to another user
	if err := os.Chown(filepath.Dir(executable), 1000, 1000); err != nil {
		t.Skip(err)
	}
	if rootUpdateProblem(executable) == "" {
		t.Fatal("root would update a binary another user can replace")
	}
	var out, errOut bytes.Buffer
	if code := updateCmd(docopt.Opts{"--check": true}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "Not run as root") {
		t.Fatalf("update ran as root: %d %s", code, errOut.String())
	}
}

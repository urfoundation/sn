//go:build unix

package miner

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// SIGUSR1 writes a goroutine dump and the process keeps running: the reporter
// of urnetwork/connect#227 had no dump, since SIGQUIT shuts the miner down.
func TestGoroutineDumpOnSIGUSR1KeepsRunning(t *testing.T) {
	writes := make(chan string, 4)
	stop := startGoroutineDumpSignal(writerFunc(func(b []byte) (int, error) {
		writes <- string(b)
		return len(b), nil
	}))
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	select {
	case dump := <-writes:
		if !strings.HasPrefix(dump, "==== goroutine dump (SIGUSR1) ") || !strings.Contains(dump, "\ngoroutine profile: total ") {
			t.Fatalf("dump:\n%.400s", dump)
		}
		// this test's own goroutine, waiting for the dump, is in it
		if !strings.Contains(dump, "miner.TestGoroutineDumpOnSIGUSR1KeepsRunning") {
			t.Fatalf("dump does not include this test's goroutine:\n%.2000s", dump)
		}
	case <-time.After(time.Minute):
		t.Fatal("no goroutine dump after SIGUSR1")
	}
}

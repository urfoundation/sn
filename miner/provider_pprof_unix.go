//go:build unix

package miner

import (
	"io"
	"os"
	"os/signal"
	"syscall"
)

// startGoroutineDumpSignal writes a goroutine dump to w on each SIGUSR1 until
// the returned stop runs, and the miner keeps running. A Go program that has
// not asked for SIGUSR1 ignores it, so sending it to an older miner is
// harmless. Signals that arrive while a dump is being written collapse into
// one more dump.
func startGoroutineDumpSignal(w io.Writer) (stop func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	stopDumps := goroutineDumps(signals, "SIGUSR1", w)
	return func() {
		signal.Stop(signals)
		stopDumps()
	}
}

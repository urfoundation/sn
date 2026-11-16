//go:build !unix

package miner

import "io"

// There is no SIGUSR1 here (Windows); --pprof serves the same dump at
// /debug/pprof/goroutine?debug=1.
func startGoroutineDumpSignal(io.Writer) (stop func()) {
	return func() {}
}

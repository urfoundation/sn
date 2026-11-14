//go:build !linux && !darwin && !windows

package miner

// Without a signal-0 probe, a fresh status file is taken as a live miner.
func minerProcessAlive(pid int) bool {
	return 0 < pid
}

// Elsewhere there is no update lock; updates are not supported there yet.
func lockMinerUpdate() (func(), error) {
	return func() {}, nil
}

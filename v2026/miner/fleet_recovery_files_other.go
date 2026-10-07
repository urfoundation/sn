//go:build !(linux || darwin || freebsd || openbsd || netbsd || dragonfly)

// Testnet and read-only commands retain their portable builds. Mainnet writes
// require a platform with qualified descriptor-relative custody primitives.
package miner

import (
	"errors"
	"os"
)

// No process-lock or no-follow fallback is silently assumed.
func fleetRecoveryPlatformSupported() error {
	return errors.New("mainnet fleet recovery custody is unsupported on this platform")
}

// Unsupported platforms never create a transaction journal.
func fleetRecoveryOpenFile(*os.File, string, int) (*os.File, error) {
	return nil, fleetRecoveryPlatformSupported()
}

// Unsupported platforms never acquire a custody namespace.
func fleetRecoveryOpenDirectory(string) (*os.File, error) {
	return nil, fleetRecoveryPlatformSupported()
}

// A missing interprocess lock cannot authorize signing.
func fleetRecoveryLock(*os.File) error { return fleetRecoveryPlatformSupported() }

// There is no weaker rename protocol on an unsupported platform.
func fleetRecoveryRename(*os.File, string, string) error { return fleetRecoveryPlatformSupported() }

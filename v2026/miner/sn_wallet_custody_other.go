//go:build !(linux || darwin || freebsd || openbsd || netbsd || dragonfly)

// Wallet custody has no fallback without relative no-follow I/O and a process
// lease. Other read-only and legacy commands retain their portable builds.
package miner

import "errors"

// Unsupported platforms never acquire or initialize wallet consent custody.
type snWalletConsentStore struct{}

// An explicit invocation cannot silently weaken retained-original custody.
func openSnWalletConsentStore(string) (*snWalletConsentStore, bool, error) {
	return nil, false, errors.New("wallet consent custody is unsupported on this platform")
}

// No synthetic empty history can replace unavailable custody primitives.
func (self *snWalletConsentStore) read() ([]byte, error) {
	return nil, errors.New("wallet consent custody is unsupported on this platform")
}

// No unsigned or memory-only persistence fallback is permitted.
func (self *snWalletConsentStore) write([]byte, [32]byte, bool) error {
	return errors.New("wallet consent custody is unsupported on this platform")
}

// Unsupported owners never hold resources.
func (self *snWalletConsentStore) close() error { return nil }

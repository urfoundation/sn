//go:build linux

package validator

import "golang.org/x/sys/unix"

// Atomic exchange preserves both physical inodes for crash-safe validation.
func exchangeEvidenceConsentRecoveryFiles(from int, old string, to int, next string) error {
	return unix.Renameat2(from, old, to, next, unix.RENAME_EXCHANGE)
}

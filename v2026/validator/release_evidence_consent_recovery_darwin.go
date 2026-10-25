//go:build darwin

package validator

import "golang.org/x/sys/unix"

// The native swap supplies the same retained-displaced-inode contract.
func exchangeEvidenceConsentRecoveryFiles(from int, old string, to int, next string) error {
	return unix.RenameatxNp(from, old, to, next, unix.RENAME_SWAP)
}

//go:build linux

package validator

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func readAttemptLedgerCustodyAttribute(file *os.File) ([]byte, error) {
	raw := make([]byte, 4096)
	n, err := unix.Fgetxattr(int(file.Fd()), attemptLedgerCustodyAttribute, raw)
	if err != nil {
		if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ERANGE) {
			return nil, attemptLedgerCustodyLoss("preprovisioned attempt ledger custody anchor is absent or invalid", err)
		}
		return nil, attemptLedgerCustodyObservation("cannot observe attempt ledger custody anchor", err)
	}
	return raw[:n], nil
}

// XATTR_REPLACE is essential: deletion cannot silently enroll a fresh owner.
func replaceAttemptLedgerCustodyAttribute(file *os.File, raw []byte) error {
	return unix.Fsetxattr(int(file.Fd()), attemptLedgerCustodyAttribute, raw, unix.XATTR_REPLACE)
}

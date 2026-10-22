//go:build linux || darwin

package validator

import (
	"errors"
	"os"

	"github.com/urnetwork/connect/v2026/durablesys"
	"golang.org/x/sys/unix"
)

func readAttemptLedgerCustodyAttribute(file *os.File) ([]byte, error) {
	raw := make([]byte, 4096)
	n, err := durablesys.GetAttribute(int(file.Fd()), attemptLedgerCustodyAttribute, raw)
	if err != nil {
		if errors.Is(err, durablesys.ErrNoAttribute) || errors.Is(err, unix.ERANGE) {
			return nil, attemptLedgerCustodyLoss("preprovisioned attempt ledger custody anchor is absent or invalid", err)
		}
		return nil, attemptLedgerCustodyObservation("cannot observe attempt ledger custody anchor", err)
	}
	return raw[:n], nil
}

// Replace-only is essential: deletion cannot silently enroll a fresh owner.
// durablesys keeps the condition on Darwin, where x/sys's Fsetxattr drops it.
func replaceAttemptLedgerCustodyAttribute(file *os.File, raw []byte) error {
	return durablesys.SetAttribute(int(file.Fd()), attemptLedgerCustodyAttribute, raw, durablesys.AttributeReplace)
}

//go:build darwin

package validator

import (
	"errors"
	"os"
)

// Historical unguarded parsing remains available; guarded production admission
// already requires a separately qualified Linux filesystem declaration.
func readAttemptLedgerCustodyAttribute(*os.File) ([]byte, error) {
	return nil, errors.New("guarded attempt ledger custody is not qualified on this platform")
}

func replaceAttemptLedgerCustodyAttribute(*os.File, []byte) error {
	return errors.New("guarded attempt ledger custody is not qualified on this platform")
}

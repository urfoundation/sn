//go:build darwin

// Production guarded request custody requires the qualified Linux owner.
package validator

import (
	"errors"
	"os"
)

func readProviderAttemptRequestAttribute(*os.File) ([]byte, bool, error) {
	return nil, false, errors.New("provider request custody is unavailable on this platform")
}
func writeProviderAttemptRequestAttribute(*os.File, []byte, bool) error {
	return errors.New("provider request custody is unavailable on this platform")
}
func sameProviderAttemptRequestFileStat(os.FileInfo, os.FileInfo) bool { return false }

//go:build linux || darwin

// The request extension uses one small inode-bound xattr. All record bytes live
// in ordinary protected files and remain inside the complete owner inventory.
package validator

import (
	"errors"
	"os"

	"github.com/urnetwork/connect/v2026/durablesys"
)

func readProviderAttemptRequestAttribute(file *os.File) ([]byte, bool, error) {
	raw := make([]byte, 4096)
	n, err := durablesys.GetAttribute(int(file.Fd()), ProviderAttemptRequestAttribute, raw)
	if errors.Is(err, durablesys.ErrNoAttribute) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return raw[:n], false, nil
}
func writeProviderAttemptRequestAttribute(file *os.File, raw []byte, create bool) error {
	flags := durablesys.AttributeReplace
	if create {
		flags = durablesys.AttributeCreate
	}
	return durablesys.SetAttribute(int(file.Fd()), ProviderAttemptRequestAttribute, raw, flags)
}

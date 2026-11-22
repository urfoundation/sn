//go:build linux || darwin

// The request extension uses one small inode-bound xattr. All record bytes live
// in ordinary protected files and remain inside the complete owner inventory.
package validator

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablesys"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The request budget counts exactly four owner attributes on the ledger root.
// A fresh native journal, SDK work outbox or successor-members owner may share
// that root and attach its own there, outside the budget, so a request owner
// refuses a root carrying any other attribute in the owner namespace.
func checkProviderAttemptRequestRootAttributes(root *os.File) error {
	raw := make([]byte, 64*1024)
	n, err := durablesys.ListAttributes(int(root.Fd()), raw)
	if err != nil {
		return err
	}
	for _, name := range strings.Split(string(raw[:n]), "\x00") {
		switch name {
		case attemptLedgerCustodyAttribute, durablevolume.PreparationAttribute, durablevolume.RootGenerationAttribute, ProviderAttemptRequestAttribute:
			continue
		}
		if strings.HasPrefix(name, "user.urnetwork.") {
			return errors.Join(protocol.ErrProviderAttemptsCapacity, fmt.Errorf("provider request root also carries %s outside its attribute budget", name))
		}
	}
	return nil
}

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

// Synthetic host composition replaces only the process transport. The exact
// read-only inspector still opens real retained roots under the current UID.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durableinspect"
)

// Kernel facts come from the explicitly supplied fixture context. Actual
// ownership, descriptor ancestry and the inspector report are never waived.
func serviceStorageTestTransport(t *testing.T) func(context.Context, *exec.Cmd) error {
	t.Helper()
	return func(ctx context.Context, command *exec.Cmd) error {
		if command.Path != "/proc/self/fd/3" || len(command.ExtraFiles) != 1 || len(command.Args) < 2 || command.Args[1] != "storage-inspect" || command.SysProcAttr == nil || command.SysProcAttr.Credential == nil {
			return fmt.Errorf("synthetic inspector lost the approved descriptor/command")
		}
		credential := command.SysProcAttr.Credential
		if credential.Uid != uint32(os.Geteuid()) || credential.Gid != uint32(os.Getegid()) || len(credential.Groups) != 0 {
			return fmt.Errorf("synthetic inspector cannot impersonate another uid")
		}
		if code := durableinspect.Run(ctx, command.Args[2:], command.Stdout, command.Stderr); code != 0 {
			return fmt.Errorf("synthetic process transport: actual read-only inspector exit %d", code)
		}
		return nil
	}
}

//go:build darwin

// Darwin fixture volumes claim qualified APFS and widen the 32-bit dev_t.
package durablefixture

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// The synthetic volume claims the platform's qualified filesystem.
const filesystemType = "apfs"

// The stand-in magic the Darwin host reports for qualified APFS.
const filesystemMagic = 0x61706673

// Darwin reports a 32-bit dev_t; widen it without sign extension.
func rawDevice(stat *unix.Stat_t) uint64 { return uint64(uint32(stat.Dev)) }

// Custody walks never follow symlinks, and Darwin's default temporary
// directory sits below the /var alias of /private/var. Fixture-backed tests
// therefore allocate below its physical path. This package is test-only.
func init() {
	if physical, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		os.Setenv("TMPDIR", physical)
	}
}

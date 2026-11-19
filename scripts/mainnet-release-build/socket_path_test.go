package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// testSocketPath is a path for a test's Unix socket named name. A socket path
// must fit sun_path, 108 bytes on Linux, and t.TempDir under the release gate's
// private storage, with the test's name in it, is longer than that. A path that
// would not fit uses a short private runtime directory. Keep /tmp as the
// fallback when the configured root cannot provide a usable socket path.
func testSocketPath(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if len(path) < 104 {
		return path
	}
	dir, configuredErr := os.MkdirTemp(os.TempDir(), "sn-sock-")
	if configuredErr == nil && len(filepath.Join(dir, name)) >= 104 {
		overlong := dir
		if err := os.Remove(overlong); err != nil {
			t.Cleanup(func() {
				if err := os.RemoveAll(overlong); err != nil {
					t.Error(err)
				}
			})
			t.Fatal(err)
		}
		dir = ""
	}
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("/tmp", "sn-sock-")
		if err != nil {
			t.Fatal(errors.Join(configuredErr, err))
		}
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

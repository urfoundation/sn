package main

import (
	"os"
	"path/filepath"
	"testing"
)

// testSocketPath is a path for a test's Unix socket named name. A socket path
// must fit sun_path, 108 bytes on Linux, and t.TempDir under the release gate's
// private storage, with the test's name in it, is longer than that. A path that
// would not fit goes in a private directory under /tmp instead, removed when
// the test ends.
func testSocketPath(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if len(path) < 104 {
		return path
	}
	dir, err := os.MkdirTemp("/tmp", "sn-sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

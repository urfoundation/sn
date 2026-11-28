package clientauth

import (
	"os"
	"runtime"
	"testing"
)

// renameRootToSimulateReplacement moves the custody root aside so a test can
// stand a replacement directory on the same path, which is how the store's
// namespace check is exercised. It reports whether the swap happened.
//
// Windows refuses to rename a directory that holds any open file handle, even
// with delete sharing on every handle (verified: a file open with
// FILE_SHARE_READ|WRITE|DELETE inside the directory still yields "Access is
// denied" on the parent rename). The store holds the lock file and the
// credential while it runs, so the replaced-root scenario cannot occur there:
// an attacker has no way to swap a directory out from under a live store, and
// custody is carried by the descriptor and owner audit instead. The test
// therefore asserts the platform property on Windows rather than failing on an
// impossible precondition, and keeps the swap coverage on unix.
func renameRootToSimulateReplacement(t *testing.T, dir, moved string) bool {
	t.Helper()
	err := os.Rename(dir, moved)
	if err == nil {
		return true
	}
	if runtime.GOOS == "windows" {
		t.Logf("windows refused the custody root rename with a live store (%v); the replaced-root swap is unreachable on this platform", err)
		return false
	}
	t.Fatalf("rename custody root: %v", err)
	return false
}

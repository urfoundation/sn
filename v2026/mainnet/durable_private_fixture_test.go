package main

// Durable fixtures own their numbered testing.TempDir child and make that
// ancestor private explicitly. They never repair an external ancestor or
// relax production admission under a permissive shell umask.

import (
	"os"
	"testing"
)

// Go creates numbered TempDir children with 0777 before applying the process
// umask. Only this just-created child is changed to the fixture's private mode.
func mainnetPrivateTestDir(t testing.TB) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

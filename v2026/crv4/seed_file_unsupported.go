//go:build !linux && !darwin

package crv4

// Unsupported platforms refuse custody I/O instead of falling back to a
// replacing or symlink-following key writer. Pure signing remains portable.

import (
	"errors"
	"os"
)

func seedFilesSupported() error {
	return errors.New("private seed file custody is unsupported on this platform")
}
func seedFileOpenFlags() int                               { return 0 }
func seedFilePrivate(os.FileInfo) bool                     { return false }
func seedFileOwnedParent(os.FileInfo) bool                 { return false }
func seedFileSameWriteState(os.FileInfo, os.FileInfo) bool { return false }
func seedFilePublish(*os.File, string, string) error       { return seedFilesSupported() }

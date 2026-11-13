//go:build !linux && !darwin

package validator

// Unsupported systems refuse custody instead of using a blocking fallback.

import (
	"errors"
	"os"
)

var errAttemptPrivateDirectoryPlatform = errors.New("private metadata custody supports Linux and Darwin only")

// There is no unqualified pathname fallback.
func openAttemptPrivateDirectoryFile(string) (*os.File, error) {
	return nil, errAttemptPrivateDirectoryPlatform
}

// Native no-follow observation is unavailable.
func (self *attemptPrivateDirectory) stat(string) (attemptPrivateFileState, error) {
	return attemptPrivateFileState{}, errAttemptPrivateDirectoryPlatform
}

// Native no-follow acquisition is unavailable.
func (self *attemptPrivateDirectory) openFile(string, int, uint32) (*os.File, error) {
	return nil, errAttemptPrivateDirectoryPlatform
}

// A native descriptor cannot be qualified here.
func statAttemptPrivateFile(*os.File) (attemptPrivateFileState, error) {
	return attemptPrivateFileState{}, errAttemptPrivateDirectoryPlatform
}

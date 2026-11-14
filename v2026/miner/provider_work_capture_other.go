//go:build !linux && !darwin && !freebsd

// Unsupported lifetime custody cannot satisfy an explicitly complete launch.
// Legacy providing still works without a whole-work capture profile.
package miner

import (
	"errors"
	"os"
)

// Match the Core worker's supported descriptor-lease platforms at admission.
func validateProviderWorkCaptureDirectory(os.FileInfo) error {
	return errors.New("whole-work launch custody is unsupported on this platform")
}

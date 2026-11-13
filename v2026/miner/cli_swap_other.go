//go:build !windows

package miner

import (
	"fmt"
	"os"
	"path/filepath"
)

// replaceMinerBinary keeps the old binary under a second name, then takes
// the binary's name in one rename: at every moment there is a whole binary
// at executable.
func replaceMinerBinary(newPath string, executable string) error {
	previous := executable + ".previous"
	_ = os.Remove(previous)
	if err := os.Link(executable, previous); err != nil {
		if err := copyFile(executable, previous); err != nil {
			return fmt.Errorf("can't keep the previous version beside %s: %w", executable, err)
		}
	}
	if err := os.Rename(newPath, executable); err != nil {
		return fmt.Errorf("can't replace %s: %w", executable, err)
	}
	syncDir(filepath.Dir(executable))
	return nil
}

// rollBackMiner puts executable.previous back.
func rollBackMiner(executable string) error {
	previous := executable + ".previous"
	if _, err := os.Stat(previous); err != nil {
		return err
	}
	return os.Rename(previous, executable)
}

// Monitor publications replace complete snapshots. A directory sync failure
// after rename is ambiguous durability. Callers report failure; a supervising
// domain may retry a complete snapshot without acknowledging the failed write.
package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Only ownership faults are terminal to a composed observer. Ordinary local
// I/O failures preserve aging output and retry without stopping other domains.
type monitorOutputOwnershipError struct{ reason string }

// The public role event uses a closed outcome rather than this local detail.
func (self *monitorOutputOwnershipError) Error() string { return self.reason }

// Resolve the existing parent once before acquiring ownership. Keeping the
// physical destination also prevents a later alias change redirecting writes.
func resolveMonitorDestination(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." {
		return "", errors.New("monitor destination must be an absolute canonical file path")
	}
	directory, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, filepath.Base(path)), nil
}

// The owner validates and locks the destination before publication. Temporary
// files have no .prom suffix, so a textfile collector cannot read partial bytes.
func publishMonitorFile(path string, raw []byte, mode os.FileMode, syncDirectory func(*os.File) error) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".sn-mainnet-monitor-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(mode); err != nil {
		return errors.Join(err, file.Close())
	}
	written, writeErr := file.Write(raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	return errors.Join(syncDirectory(dir), dir.Close())
}

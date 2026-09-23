// Full approvals retain each authorized fleet generation. Their bounded wire
// size is independent of an individual receipt/proof, and is shared by output,
// import, active-plan reload and immutable historical-plan readers.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Leave more than twice the observed full-fleet approval size without raising
// ordinary evidence limits or accepting an unbounded approval history.
const maximumSetupPlanFileBytes = 128 * 1024 * 1024

// Producers include their trailing newline in the same bound import enforces.
func validateSetupPlanWireSize(size int) error {
	if size <= 0 || size > maximumSetupPlanFileBytes {
		return fmt.Errorf("setup plan size %d exceeds its bounded file capacity %d", size, maximumSetupPlanFileBytes)
	}
	return nil
}

// Plan-only callers retain the same no-follow descriptor and size protections
// as ordinary historical evidence, with the explicit approval-file capacity.
func readSetupPlanBytes(stateDir, name string) ([]byte, error) {
	return readSetupPlanBytesObserved(stateDir, name, nil)
}

// Tests may observe the acquired descriptor; they cannot replace its opener.
func readSetupPlanBytesObserved(stateDir, name string, opened func(*os.File) error) ([]byte, error) {
	return readBoundedHistoricalFileObserved(stateDir, name, maximumSetupPlanFileBytes, opened)
}

// External imports walk every absolute component without following symlinks.
// No stat/read gap can exchange a checked regular file for a pipe or alias.
func readSetupPlanFileBytes(path string) ([]byte, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root := filepath.VolumeName(absolute) + string(filepath.Separator)
	return readSetupPlanBytes(root, filepath.ToSlash(strings.TrimPrefix(absolute, root)))
}

// Open first, admit and read the owned descriptor, then confirm its size. A
// FIFO is opened nonblocking and rejected before any read can wait for input.
func readBoundedHistoricalFileObserved(stateDir, name string, maximum int64, opened func(*os.File) error) (result []byte, resultErr error) {
	if err := validateCampaignEvidencePath(name); err != nil {
		return nil, err
	}
	if maximum <= 0 || maximum > maximumSetupPlanFileBytes {
		return nil, errors.New("historical file bound is invalid")
	}
	file, err := openFinalCollectedFile(stateDir, filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if resultErr != nil {
			result = nil
		}
	}()
	if opened != nil {
		if err := opened(file); err != nil {
			return nil, err
		}
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum {
		return nil, fmt.Errorf("historical file %s is not regular or exceeds its %d-byte bound", name, maximum)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != info.Size() || int64(len(raw)) > maximum {
		return nil, errors.New("historical file changed size or exceeds its bound")
	}
	return raw, nil
}

// Publish an absent predecessor only after its whole failure record is durable.
// The caller holds the deployment journal lock; existing run directories remain
// immutable even when empty or incomplete.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Isolate every partial output in a private sibling. A failed writer leaves
// the authoritative run absent, and a retry starts from the same signed owner.
func publishAbsentCampaignRecoveryRun(runDir string, write func(string) error) (err error) {
	if write == nil {
		return errors.New("campaign recovery publication has no output writer")
	}
	if _, err := os.Lstat(runDir); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errors.New("campaign recovery cannot replace an existing run"), err)
	}
	parent := filepath.Dir(runDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".campaign-recovery-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(staging)) }()
	if err := write(staging); err != nil {
		return err
	}
	result, err := os.Lstat(filepath.Join(staging, "result.json"))
	if err != nil || !result.Mode().IsRegular() || result.Size() == 0 {
		return errors.Join(errors.New("campaign recovery output has no complete regular result"), err)
	}
	if _, err := os.Lstat(runDir); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errors.New("campaign recovery destination appeared before publication"), err)
	}
	if err := os.Rename(staging, runDir); err != nil {
		return fmt.Errorf("publish complete campaign recovery run: %w", err)
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

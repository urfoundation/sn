package validator

// Actual private files survive failed observations and resume on the same owner.
// Proven name replacement is a separate control, never inferred from nil info.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func TestAttemptLedgerOpenObservationKeepsCauseAndSameOwner(t *testing.T) {
	for _, stage := range []string{"descriptor", "name"} {
		func() {
			path := newAttemptLedgerDiskTestStateDir(t)
			directory, err := openAttemptLedgerDirectory(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer directory.Close()
			raw := []byte("retained original\n")
			if err := os.WriteFile(filepath.Join(path, attemptLedgerLegacyName), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if stage == "descriptor" {
				directory.statFile = func(file *os.File) (os.FileInfo, error) {
					if _, err := file.Stat(); err != nil {
						t.Fatal(err)
					}
					return nil, syscall.EIO
				}
			} else {
				directory.statName = func(root *os.Root, name string) (os.FileInfo, error) {
					if _, err := root.Lstat(name); err != nil {
						t.Fatal(err)
					}
					return nil, syscall.EMFILE
				}
			}
			file, err := directory.openFile(attemptLedgerLegacyName, os.O_RDONLY, false)
			cause := error(syscall.EIO)
			if stage == "name" {
				cause = syscall.EMFILE
			}
			if file != nil || !errors.Is(err, cause) || !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrIdentity) {
				t.Fatal("failed observation became changed evidence or lost its cause", file, err)
			}
			directory.statFile, directory.statName = nil, nil
			actual, err := directory.readSmall(attemptLedgerLegacyName, uint64(len(raw)))
			if err != nil || !bytes.Equal(actual, raw) {
				t.Fatal("same owner did not recover original bytes", err)
			}
		}()
	}
}

func TestAttemptLedgerMarkerObservationResumesExactPublication(t *testing.T) {
	for _, stage := range []string{"before-rename", "acknowledgement"} {
		func() {
			path := newAttemptLedgerDiskTestStateDir(t)
			directory, err := openAttemptLedgerDirectory(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer directory.Close()
			raw := []byte("original public migration marker")
			calls := 0
			directory.statName = func(root *os.Root, name string) (os.FileInfo, error) {
				info, err := root.Lstat(name)
				if err != nil {
					return nil, err
				}
				if stage == "before-rename" && name == attemptLedgerImportName+".tmp" {
					calls++
					if calls == 2 {
						return nil, syscall.EIO
					}
				}
				if stage == "acknowledgement" && name == attemptLedgerImportName {
					calls++
					return nil, syscall.EIO
				}
				return info, nil
			}
			err = directory.publishMarker(attemptLedgerImportName, raw)
			if !errors.Is(err, syscall.EIO) || !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrIdentity) || calls == 0 {
				t.Fatal("failed publication observation became a contradiction", calls, err)
			}
			directory.statName = nil
			retainedName := attemptLedgerImportName
			if stage == "before-rename" {
				retainedName += ".tmp"
			}
			retained, err := os.ReadFile(filepath.Join(path, retainedName))
			if err != nil || !bytes.Equal(retained, raw) {
				t.Fatal("failed acknowledgement lost original bytes", err)
			}
			if err := directory.Close(); err != nil {
				t.Fatal(err)
			}
			directory, err = openAttemptLedgerDirectory(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer directory.Close()
			if err := directory.publishMarker(attemptLedgerImportName, raw); err != nil {
				t.Fatal("joined restart did not resume exact marker", err)
			}
			actual, err := directory.readSmall(attemptLedgerImportName, uint64(len(raw)))
			if err != nil || !bytes.Equal(actual, raw) {
				t.Fatal("resumed publication changed original", err)
			}
		}()
	}
}

func TestAttemptLedgerMarkerPositiveReplacementKeepsIdentityVerdict(t *testing.T) {
	path := newAttemptLedgerDiskTestStateDir(t)
	directory, err := openAttemptLedgerDirectory(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	raw := []byte("original marker")
	replaced := false
	directory.step = func(stage, name string) error {
		if stage == "marker-sync" && name == attemptLedgerImportName && !replaced {
			replaced = true
			old := filepath.Join(path, name+".tmp")
			if err := os.Rename(old, old+".retained"); err != nil {
				return err
			}
			return os.WriteFile(old, raw, 0600)
		}
		return nil
	}
	if err := directory.publishMarker(attemptLedgerImportName, raw); !replaced || !errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, durablevolume.ErrUnavailable) {
		t.Fatal("actual replacement was not a custody contradiction", replaced, err)
	}
	if _, err := os.Lstat(filepath.Join(path, attemptLedgerImportName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replaced original was published", err)
	}
}

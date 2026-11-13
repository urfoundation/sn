// Descriptor observation failures are retryable; an observed different inode
// permanently stops the actual private-directory owner, even after restoration.
package validator

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

func TestDurableValidatorDirectoryObservationFailureCanRetry(t *testing.T) {
	root := newAttemptLedgerDiskTestStateDir(t)
	fixture := durablefixture.New(t, t.Context(), root)
	owner, err := openAttemptPrivateDirectory(root, fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	for _, observed := range []*os.File{owner.file, owner.storage.File()} {
		owner.statForTest = func(file *os.File) (os.FileInfo, error) {
			if file == observed {
				return nil, syscall.EIO
			}
			return file.Stat()
		}
		if err := owner.check(); !errors.Is(err, durablevolume.ErrUnavailable) || !errors.Is(err, syscall.EIO) || errors.Is(err, durablevolume.ErrIdentity) || owner.failure != nil {
			t.Fatalf("unobservable descriptor poisoned identity: %v retained=%v", err, owner.failure)
		}
		owner.statForTest = nil
		if err := owner.check(); err != nil {
			t.Fatal("same directory owner did not recover", err)
		}
	}
}

func TestDurableValidatorDirectoryDifferentDescriptorIsSticky(t *testing.T) {
	root := newAttemptLedgerDiskTestStateDir(t)
	fixture := durablefixture.New(t, t.Context(), root)
	owner, err := openAttemptPrivateDirectory(root, fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	differentPath := filepath.Join(root, "different")
	if err := os.Mkdir(differentPath, 0700); err != nil {
		t.Fatal(err)
	}
	different, err := os.Open(differentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer different.Close()
	original := owner.file
	owner.file = different
	if err := owner.check(); !errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, durablevolume.ErrUnavailable) {
		t.Fatal("proven unequal descriptor was not identity loss", err)
	}
	owner.file = original
	if err := owner.check(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("restoration revived a directory whose actual descriptor changed", err)
	}
}

func TestDurableValidatorDirectoryClosedDescriptorIsNotIdentity(t *testing.T) {
	root := newAttemptLedgerDiskTestStateDir(t)
	fixture := durablefixture.New(t, t.Context(), root)
	owner, err := openAttemptPrivateDirectory(root, fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	closed, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	for _, file := range []*os.File{nil, closed} {
		if err := checkValidatorDurableDirectory(owner.storage, file); err == nil || errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, durablevolume.ErrUnavailable) {
			t.Fatal("caller descriptor misuse became a filesystem transition", err)
		}
	}
	if err := owner.check(); err != nil {
		t.Fatal("unrelated owner lost custody", err)
	}
}

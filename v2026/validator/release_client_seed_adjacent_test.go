//go:build linux || darwin

package validator

// Runtime admission must honor a second read's custody, cancellation and key
// identity without constructing API, JWT or transport resources in these tests.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// A first good preparation cannot waive private custody on the second load.
// Each hook observes the real production loader after a deliberate mutation.
func TestReleaseClientSeedContinuityRejectsUnsafeSecondRead(t *testing.T) {
	for _, mutation := range []string{"symlink", "hardlink", "file-mode", "parent-mode", "invalid-format", "oversize"} {
		fixture := newReleaseClientSeedContinuityFixture(t)
		path := fixture.op.ClientKeySeedFile
		switch mutation {
		case "symlink":
			if err := os.Rename(path, path+"-preserved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+"-preserved", path); err != nil {
				t.Fatal(err)
			}
		case "hardlink":
			if err := os.Link(path, path+"-alias"); err != nil {
				t.Fatal(err)
			}
		case "file-mode":
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		case "parent-mode":
			if err := os.Chmod(filepath.Dir(path), 0o777); err != nil {
				t.Fatal(err)
			}
		case "invalid-format":
			if err := os.WriteFile(path, []byte("0x"+strings.Repeat("11", 32)), 0o600); err != nil {
				t.Fatal(err)
			}
		case "oversize":
			if err := os.WriteFile(path, bytes.Repeat([]byte{' '}, 4097), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, entries, err := observeReleaseClientSeedRuntimeAdmission(t, fixture)
		if entries != 0 || err == nil || !strings.Contains(err.Error(), "client key:") {
			t.Fatalf("%s second read crossed its custody boundary: entries=%d error=%v", mutation, entries, err)
		}
	}
}

// A disappeared provisioned key remains missing. The first prepared ledger is
// not permission to regenerate the file or retain stale key authority.
func TestReleaseClientSeedContinuityRejectsMissingSecondRead(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	path := fixture.op.ClientKeySeedFile
	if err := os.Rename(path, path+"-preserved"); err != nil {
		t.Fatal(err)
	}
	head, err := fixture.state.ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	entries := 0
	runtime, err := startReleaseOperatorWithAdmission(context.Background(), fixture.cfg, fixture.op, func() uint64 { return 42 },
		func(context.Context, *AttemptBoundary, []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			return AttemptBoundary{}, nil, errors.New("unexpected missing-key boundary resolution")
		}, fixture.state, func([32]byte) error {
			entries++
			return errors.New("unexpected missing-key runtime admission")
		})
	if runtime != nil || entries != 0 || !errors.Is(err, os.ErrNotExist) {
		if runtime != nil && runtime.close != nil {
			runtime.close()
		}
		t.Fatalf("missing second key returned runtime authority: entries=%d error=%v", entries, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing provisioned key was recreated: %v", err)
	}
	preserved, err := os.ReadFile(path + "-preserved")
	after, headErr := fixture.state.ledger.Head()
	if err != nil || headErr != nil || !bytes.Equal(preserved, fixture.seed[:]) || after != head {
		t.Fatalf("missing second read changed retained authority: %v/%v", err, headErr)
	}
}

// Path replacement is acceptable when independently checked custody still
// yields the exact prepared key. Continuity is not an inode or format pin.
func TestReleaseClientSeedContinuityAcceptsSameKeyReplacement(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	path := fixture.op.ClientKeySeedFile
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(filepath.Dir(path), "same-key-replacement")
	if err := os.WriteFile(replacement, fixture.seed[:], 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(path)
	if err != nil || os.SameFile(before, after) || after.Mode() != 0o400 {
		t.Fatalf("fixture did not replace its key with a private read-only inode: %v", err)
	}
	observed, entries, err := observeReleaseClientSeedRuntimeAdmission(t, fixture)
	if entries != 1 || observed != fixture.publicKey {
		t.Fatalf("same-key replacement lost identity continuity: entries=%d error=%v", entries, err)
	}
}

// The loader normalizes the stored genesis to canonical lowercase; an equally
// valid configuration spelling must not invent a different ledger domain.
func TestReleaseClientSeedContinuityAcceptsCanonicalGenesisSpelling(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	fixture.cfg.GenesisHash = "0x" + strings.ToUpper(fixture.cfg.GenesisHash[2:])
	if err := fixture.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	observed, entries, err := observeReleaseClientSeedRuntimeAdmission(t, fixture)
	if entries != 1 || observed != fixture.publicKey {
		t.Fatalf("canonical-equivalent genesis lost prepared identity: entries=%d error=%v", entries, err)
	}
}

// Cancellation must precede both a second file load and external ownership.
// An invalid artifact URL is an immediate, service-free witness if the latter
// checkpoint is accidentally removed; it cannot become a network test.
func TestReleaseClientSeedContinuityHonorsCancellationBeforeOwnership(t *testing.T) {
	for _, cancelAt := range []string{"before-read", "after-admission"} {
		fixture := newReleaseClientSeedContinuityFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fixture.op.APIURL = ":test-owned-invalid-artifact-url"
		if cancelAt == "before-read" {
			cancel()
			fixture.op.ClientKeySeedFile = filepath.Join(fixture.op.StateDir, "missing.key")
		}
		entries := 0
		runtime, err := startReleaseOperatorWithAdmission(ctx, fixture.cfg, fixture.op, func() uint64 { return 42 },
			func(context.Context, *AttemptBoundary, []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
				return AttemptBoundary{}, nil, errors.New("unexpected canceled boundary resolution")
			}, fixture.state, func(publicKey [32]byte) error {
				entries++
				if publicKey != fixture.publicKey {
					return errors.New("cancellation changed prepared key identity")
				}
				cancel()
				return nil
			})
		wantEntries := 1
		if cancelAt == "before-read" {
			wantEntries = 0
		}
		if runtime != nil || entries != wantEntries || err != context.Canceled {
			if runtime != nil && runtime.close != nil {
				runtime.close()
			}
			t.Fatalf("%s cancellation crossed ownership: entries=%d error=%v", cancelAt, entries, err)
		}
	}
}

//go:build linux

// Offline native checkpoint construction cannot label retained or canceled
// observations as fresh authority. No runtime enrollment occurs in this helper.
package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Real empty files are prepared as explicit test inputs, without any xattr.
func nativePreparationInputFixture(t *testing.T) (*os.File, NativeJournalPreparationScope) {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, journalRawDir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, journalFileName), nil, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	return file, NativeJournalPreparationScope{Schema: NativeJournalPreparationSchema, MaximumJournalBytes: 16 * 1024 * 1024,
		MaximumRawBytes: 64 * 1024 * 1024, MaximumRawMembers: 10000, MaximumRawRecordBytes: 1024 * 1024}
}

// Data, aliases and unprotected original members cannot become an empty head.
func TestNativePreparationCheckpointRefusesRetainedIntent(t *testing.T) {
	for _, mode := range []string{"log-data", "raw-data", "linked-log", "symlink-log", "public-log"} {
		func() {
			root, scope := nativePreparationInputFixture(t)
			if raw, err := BuildFreshNativeJournalPreparationCheckpoint(t.Context(), root, scope); err != nil || len(raw) == 0 {
				t.Fatal("original explicit empty checkpoint is invalid", err)
			}
			log := filepath.Join(root.Name(), journalFileName)
			switch mode {
			case "log-data":
				if err := os.WriteFile(log, []byte("retained original intent"), 0600); err != nil {
					t.Fatal(err)
				}
			case "raw-data":
				if err := os.WriteFile(filepath.Join(root.Name(), journalRawDir, "original.scale"), []byte("original signed bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "linked-log":
				if err := os.Link(log, filepath.Join(root.Name(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "symlink-log":
				if err := os.Rename(log, log+".retained"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(log+".retained", log); err != nil {
					t.Fatal(err)
				}
			case "public-log":
				if err := os.Chmod(log, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if raw, err := BuildFreshNativeJournalPreparationCheckpoint(t.Context(), root, scope); len(raw) != 0 || !errors.Is(err, durablevolume.ErrIdentity) {
				t.Fatal("changed original members became fresh checkpoint authority", mode, err)
			}
			if _, err := unix.Getxattr(root.Name(), NativeJournalCustodyAttribute, make([]byte, 4096)); !errors.Is(err, unix.ENODATA) {
				t.Fatal("read-only checkpoint construction enrolled the root", mode, err)
			}
		}()
	}
}

// This context cancels at a real post-observation boundary without changing
// descriptors or inventing filesystem facts.
type nativePreparationCancelAfterRead struct {
	context.Context
	count  atomic.Uint64
	cancel context.CancelFunc
}

// The first observation admits entry; the second follows actual member reads.
func (self *nativePreparationCancelAfterRead) Err() error {
	if self.count.Add(1) == 2 {
		self.cancel()
	}
	return self.Context.Err()
}

// Canceled physical observations admit no checkpoint bytes or root mutation.
func TestNativePreparationCanceledReadAdmitsNoAuthority(t *testing.T) {
	root, scope := nativePreparationInputFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checked := &nativePreparationCancelAfterRead{Context: ctx, cancel: cancel}
	if raw, err := BuildFreshNativeJournalPreparationCheckpoint(checked, root, scope); len(raw) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled checkpoint construction admitted authority", err)
	}
	if _, err := unix.Getxattr(root.Name(), NativeJournalCustodyAttribute, make([]byte, 4096)); !errors.Is(err, unix.ENODATA) {
		t.Fatal("canceled checkpoint construction enrolled the root", err)
	}
}

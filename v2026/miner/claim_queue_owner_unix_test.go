//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

// An unsafe queue entry cannot hold the exclusive startup owner indefinitely.
package miner

import (
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// There is deliberately no fifo writer: an ordinary blocking open would never
// reach the regular-file check. The real read must refuse the entry directly.
func TestClaimQueueOwnerRejectsFifoBeforeReading(t *testing.T) {
	store := newClaimQueueTestStore(t, filepath.Join(t.TempDir(), "claims"))
	if err := unix.Mkfifo(store.path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); !errors.Is(err, errClaimQueueUnsafeFile) {
		t.Fatalf("non-regular queue supplied custody: %v", err)
	}
}

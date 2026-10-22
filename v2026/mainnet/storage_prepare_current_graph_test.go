//go:build linux || darwin

// Tracked module consumption must retain the actual public owner constructor's
// retry semantics after the offline dispatcher publishes its original custody.
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// An instance owns one unavailable observation; descriptors, byte reads and
// admission remain production operations on the reviewed original namespace.
type storagePreparationStartupHost struct {
	durablevolume.Host
	cause       error
	unavailable atomic.Bool
}

// The constructor observes an actual unavailable fact, not changed identity.
func (self *storagePreparationStartupHost) Filesystem(file *os.File) (durablevolume.Filesystem, error) {
	if self.unavailable.Load() {
		return durablevolume.Filesystem{}, self.cause
	}
	return self.Host.Filesystem(file)
}

// The image separates physical member identity from immutable authority bytes.
type storagePreparationNativeImage struct {
	members []os.FileInfo
	anchor  []byte
	log     []byte
	control []byte
}

// Reads every member of this fixed empty native layout without enrolling it.
func storagePreparationNativeCustody(t *testing.T, f *storagePreparationCommandFixture) storagePreparationNativeImage {
	t.Helper()
	var image storagePreparationNativeImage
	for _, name := range []string{".", "native-transactions.jsonl", "native-transactions"} {
		info, err := os.Stat(filepath.Join(f.root, name))
		if err != nil {
			t.Fatal(err)
		}
		image.members = append(image.members, info)
	}
	image.anchor = make([]byte, 4096)
	n, err := unix.Getxattr(f.root, chain.NativeJournalCustodyAttribute, image.anchor)
	if err != nil {
		t.Fatal(err)
	}
	image.anchor = image.anchor[:n]
	image.log, err = os.ReadFile(filepath.Join(f.root, "native-transactions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	image.control, err = os.ReadFile(filepath.Join(f.metadata, "preparation.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return image
}

// Unavailable startup cannot change the physical generation or completed plan.
func storagePreparationAssertNativeCustody(t *testing.T, f *storagePreparationCommandFixture, before storagePreparationNativeImage) {
	t.Helper()
	after := storagePreparationNativeCustody(t, f)
	for i, info := range before.members {
		if !os.SameFile(info, after.members[i]) {
			t.Fatal("startup replaced original native member", i)
		}
	}
	if !bytes.Equal(before.anchor, after.anchor) || !bytes.Equal(before.log, after.log) || !bytes.Equal(before.control, after.control) {
		t.Fatal("startup rewrote original checkpoint, native bytes or completed preparation control")
	}
}

// Both production constructors refuse EIO/EMFILE without asserting identity
// loss, join the failed attempt, and later open the same prepared custody.
func TestStoragePreparationNativeStartupObservationRetainsOriginalCustody(t *testing.T) {
	for _, ownerLocal := range []bool{false, true} {
		for _, cause := range []error{unix.EIO, unix.EMFILE} {
			func() {
				f := newStoragePreparationCommandFixture(t)
				scope, command := "daemon", "storage-prepare"
				open := chain.OpenDurableJournal
				if ownerLocal {
					scope, command, open = "owner-local", "storage-owner-prepare", chain.OpenOwnerLocalJournal
				}
				storagePreparationOwnerRequest(t, f, scope, []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
				ctx := storagePreparationApplyOwnerCommand(t, f, command)
				before := storagePreparationNativeCustody(t, f)
				host := &storagePreparationStartupHost{Host: f.storage.Host, cause: cause}
				host.unavailable.Store(true)
				ctx = durablepath.WithHost(ctx, host)
				journal, err := open(ctx, f.root)
				if journal != nil {
					journal.Close()
					t.Fatal("unobservable startup returned a writer")
				}
				if !errors.Is(err, cause) || !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrIdentity) {
					t.Fatal("startup lost retryable observation classification", ownerLocal, cause, err)
				}
				storagePreparationAssertNativeCustody(t, f, before)
				host.unavailable.Store(false)
				journal, err = open(ctx, f.root)
				if err != nil {
					t.Fatal("joined original owner cannot recover the same filesystem facts", ownerLocal, cause, err)
				}
				entries, err := journal.Entries()
				if closeErr := journal.Close(); err != nil || closeErr != nil || len(entries) != 0 {
					t.Fatal("recovered startup lost original empty journal", err, closeErr)
				}
				storagePreparationAssertNativeCustody(t, f, before)
			}()
		}
	}
}

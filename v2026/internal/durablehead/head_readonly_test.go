//go:build linux

// Passive composition exercises independent real shared lock descriptions.
// No callback supplies custody bytes or replaces a kernel lock operation.
package durablehead

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

func openReadFixture(t *testing.T, self *fixture) (*durablepath.Directory, *os.File) {
	t.Helper()
	directory, err := durablepath.Open(self.volume.Context, self.root, durablevolume.ReadOnly, false)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.Open(filepath.Join(self.root, testSpec.LockName))
	if err != nil {
		directory.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close(); _ = directory.Close() })
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return directory, lock
}

// Two already-held shared views must stay shared. An exclusive opener is
// refused until both original reader descriptions, not merely Owners, close.
func TestSnapshotHeadReadOnlyViewsComposeWithoutUpgrade(t *testing.T) {
	self := newFixture(t)
	want := []byte("original-retained-signed-history")
	if err := self.owner.Publish(want, nil); err != nil {
		t.Fatal(err)
	}
	if err := self.close(); err != nil {
		t.Fatal(err)
	}
	aDirectory, aLock := openReadFixture(t, self)
	bDirectory, bLock := openReadFixture(t, self)
	a, err := OpenReadOnly(self.volume.Context, aDirectory, aLock, testSpec)
	if err != nil {
		t.Fatalf("first shared composition upgraded its lease: %v", err)
	}
	defer a.Close()
	b, err := OpenReadOnly(self.volume.Context, bDirectory, bLock, testSpec)
	if err != nil {
		t.Fatalf("second shared composition was excluded: %v", err)
	}
	defer b.Close()
	for _, reader := range []*Owner{a, b} {
		raw, present, err := reader.Read()
		if err != nil || !present || !bytes.Equal(raw, want) {
			t.Fatalf("shared read lost exact retained bytes: %t %v", present, err)
		}
	}
	writerLock, err := os.OpenFile(filepath.Join(self.root, testSpec.LockName), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writerLock.Close()
	if owner, err := Open(self.volume.Context, aDirectory, writerLock, testSpec); owner != nil || !errors.Is(err, durablevolume.ErrBusy) {
		t.Fatalf("writer bypassed shared custody: %v", err)
	}
	if err := errors.Join(a.Close(), aLock.Close()); err != nil {
		t.Fatal(err)
	}
	if owner, err := Open(self.volume.Context, bDirectory, writerLock, testSpec); owner != nil || !errors.Is(err, durablevolume.ErrBusy) {
		t.Fatalf("one reader close released another reader: %v", err)
	}
	if err := errors.Join(b.Close(), bLock.Close()); err != nil {
		t.Fatal(err)
	}
	owner, err := Open(self.volume.Context, bDirectory, writerLock, testSpec)
	if err != nil {
		t.Fatalf("joined readers stranded the original writer lock: %v", err)
	}
	_ = owner.Close()
}

// A read-only filesystem and exhausted write reserve permit exact retained
// inspection. Every mutation refuses before its callback or first I/O effect.
func TestSnapshotHeadReadOnlyOwnerNeverMutates(t *testing.T) {
	self := newFixture(t)
	want := []byte("committed-original")
	if err := self.owner.Publish(want, nil); err != nil {
		t.Fatal(err)
	}
	if err := self.close(); err != nil {
		t.Fatal(err)
	}
	self.volume.Host.SetReserve(0, 0)
	self.volume.Host.SetReadOnly(true)
	directory, lock := openReadFixture(t, self)
	owner, err := OpenReadOnly(self.volume.Context, directory, lock, testSpec)
	if err != nil {
		t.Fatalf("retained inspection required writer resources: %v", err)
	}
	defer owner.Close()
	raw, present, err := owner.Read()
	if err != nil || !present || !bytes.Equal(raw, want) {
		t.Fatalf("read-only read: %v", err)
	}
	used, maximum, err := owner.Capacity()
	if err != nil || used != int64(len(want)) || maximum != testSpec.MaximumBytes {
		t.Fatalf("read-only capacity: %d/%d %v", used, maximum, err)
	}
	called := false
	for _, err := range []error{
		owner.CheckWrite(), owner.Publish([]byte("forbidden"), nil),
		owner.PublishWithHooks([]byte("forbidden"), PublicationHooks{AfterFileSync: func() error { called = true; return nil }}),
		owner.WithAuxiliary(testSpec.LockName, true, func(*os.File) error { called = true; return nil }),
	} {
		if !errors.Is(err, ErrReadOnly) {
			t.Fatalf("read-only mutation was not refused explicitly: %v", err)
		}
	}
	if called {
		t.Fatal("read-only refusal reached a writer callback")
	}
	var marker []byte
	if err := owner.WithAuxiliary(testSpec.LockName, false, func(file *os.File) error {
		var err error
		marker, err = io.ReadAll(file)
		return err
	}); err != nil || !bytes.Equal(marker, []byte("prepared\n")) {
		t.Fatalf("passive auxiliary inspection failed: %q %v", marker, err)
	}
	after, present, err := owner.Read()
	if err != nil || !present || !bytes.Equal(after, want) {
		t.Fatalf("refused mutators poisoned or changed the reader: %v", err)
	}
}

// Even complete next bytes do not give a passive view repair authority. The
// pending attribute and actual data remain unchanged after refused admission.
func TestSnapshotHeadReadOnlyRefusesPendingWithoutReconciliation(t *testing.T) {
	self := newFixture(t)
	err := self.owner.PublishWithHooks([]byte("pending-original"), PublicationHooks{AfterDirectorySync: func(*os.File) error { return unix.EIO }})
	if !errors.Is(err, ErrUncertain) {
		t.Fatalf("fixture did not retain a pending real publication: %v", err)
	}
	checkpoint := append([]byte(nil), self.owner.checkpointRaw...)
	if err := self.close(); err != nil {
		t.Fatal(err)
	}
	directory, lock := openReadFixture(t, self)
	if owner, err := OpenReadOnly(self.volume.Context, directory, lock, testSpec); owner != nil || !errors.Is(err, ErrUncertain) {
		t.Fatalf("passive reader reconciled a pending publication: %v", err)
	}
	actual := make([]byte, 4096)
	n, err := unix.Fgetxattr(int(lock.Fd()), Attribute(testSpec.Kind, testSpec.Name), actual)
	if err != nil || !bytes.Equal(actual[:n], checkpoint) {
		t.Fatalf("passive refusal modified pending authority: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(self.root, testSpec.Name))
	if err != nil || string(raw) != "pending-original" {
		t.Fatalf("passive refusal changed pending bytes: %v", err)
	}
}

// A read-only holder retains the same sticky named-leaf loss contract as a
// writer. Restoring a removed original does not revive that invalidated owner.
func TestSnapshotHeadReadOnlyRetainsObservedLeafLoss(t *testing.T) {
	self := newFixture(t)
	if err := self.owner.Publish([]byte("retained-original"), nil); err != nil {
		t.Fatal(err)
	}
	if err := self.close(); err != nil {
		t.Fatal(err)
	}
	directory, lock := openReadFixture(t, self)
	owner, err := OpenReadOnly(self.volume.Context, directory, lock, testSpec)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	path := filepath.Join(self.root, testSpec.Name)
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := owner.Read(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("read-only holder missed its named leaf loss: %v", err)
	}
	if err := os.Rename(path+".retained", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := owner.Read(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("read-only holder revived its invalidated generation: %v", err)
	}
}

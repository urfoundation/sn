// Test fixture storage faults stay explicit at every object-store boundary.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/server/v2026"
)

// fixtureFailureBlobStore keeps fault injection explicit at every BlobStore
// boundary so promoted delegate methods cannot bypass the intended failure.
type fixtureFailureBlobStore struct {
	store        server.BlobStore
	writeErr     error
	readErr      error
	listErr      error
	lifecycleErr error
}

var _ server.BlobStore = (*fixtureFailureBlobStore)(nil)

// Reject an ordinary write before it reaches the backing store.
func (self *fixtureFailureBlobStore) Put(ctx context.Context, key, localPath, contentType string) error {
	if self.writeErr != nil {
		return self.writeErr
	}
	return self.store.Put(ctx, key, localPath, contentType)
}

// Reject an atomic create at the same injected write boundary.
func (self *fixtureFailureBlobStore) PutIfAbsent(ctx context.Context, key, localPath, contentType string) (bool, error) {
	if self.writeErr != nil {
		return false, self.writeErr
	}
	return self.store.PutIfAbsent(ctx, key, localPath, contentType)
}

// Reject object read-back without changing other store behavior.
func (self *fixtureFailureBlobStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if self.readErr != nil {
		return nil, self.readErr
	}
	return self.store.Get(ctx, key)
}

// Reject prefix enumeration without changing other store behavior.
func (self *fixtureFailureBlobStore) List(ctx context.Context, keyPrefix string) ([]server.BlobObject, error) {
	if self.listErr != nil {
		return nil, self.listErr
	}
	return self.store.List(ctx, keyPrefix)
}

// Reject lifecycle configuration without changing object behavior.
func (self *fixtureFailureBlobStore) SetLifecycle(ctx context.Context, rules []server.BlobLifecycleRule) error {
	if self.lifecycleErr != nil {
		return self.lifecycleErr
	}
	return self.store.SetLifecycle(ctx, rules)
}

// Preserve the backing bucket identity used in publication receipts.
func (self *fixtureFailureBlobStore) Bucket() string {
	return self.store.Bucket()
}

// Preserve the authenticated operator namespace.
func (self *fixtureFailureBlobStore) Prefix() string {
	return self.store.Prefix()
}

// Preserve the backing endpoint identity.
func (self *fixtureFailureBlobStore) Authority() string {
	return self.store.Authority()
}

// Proves fresh writes, idempotent creates, conflicting creates, and every
// injectable interface operation pass through the explicit fixture boundary.
func TestFixtureFailureBlobStoreCoversEveryBehaviorBoundary(t *testing.T) {
	ctx := context.Background()
	backing := server.NewLocalBlobStore(t.TempDir(), "blob")
	firstPath := filepath.Join(t.TempDir(), "first")
	secondPath := filepath.Join(t.TempDir(), "second")
	first := []byte("first immutable value")
	second := []byte("conflicting immutable value")
	if err := os.WriteFile(firstPath, first, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, second, 0o600); err != nil {
		t.Fatal(err)
	}

	writeErr := errors.New("injected write failure")
	failingWrite := &fixtureFailureBlobStore{store: backing, writeErr: writeErr}
	if err := failingWrite.Put(ctx, "blob/ordinary", firstPath, "application/octet-stream"); !errors.Is(err, writeErr) {
		t.Fatalf("ordinary write failure=%v", err)
	}
	if created, err := failingWrite.PutIfAbsent(ctx, "blob/immutable", firstPath, "application/octet-stream"); created || !errors.Is(err, writeErr) {
		t.Fatalf("conditional write failure created=%t err=%v", created, err)
	}
	if objects, err := backing.List(ctx, "blob/"); err != nil || len(objects) != 0 {
		t.Fatalf("failed writes changed backing store: objects=%v err=%v", objects, err)
	}

	store := &fixtureFailureBlobStore{store: backing}
	created, err := store.PutIfAbsent(ctx, "blob/immutable", firstPath, "application/octet-stream")
	if err != nil || !created {
		t.Fatalf("fresh conditional write created=%t err=%v", created, err)
	}
	if created, err = store.PutIfAbsent(ctx, "blob/immutable", firstPath, "application/octet-stream"); err != nil || created {
		t.Fatalf("idempotent conditional write created=%t err=%v", created, err)
	}
	if created, err = store.PutIfAbsent(ctx, "blob/immutable", secondPath, "application/octet-stream"); err != nil || created {
		t.Fatalf("conflicting conditional write created=%t err=%v", created, err)
	}
	if err := store.Put(ctx, "blob/ordinary", secondPath, "application/octet-stream"); err != nil {
		t.Fatalf("ordinary delegated write=%v", err)
	}
	reader, err := store.Get(ctx, "blob/immutable")
	if err != nil {
		t.Fatal(err)
	}
	stored, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(stored, first) {
		t.Fatalf("conditional conflict changed winner=%q read=%v close=%v", stored, readErr, closeErr)
	}
	if store.Bucket() != backing.Bucket() || store.Prefix() != backing.Prefix() || store.Authority() != backing.Authority() {
		t.Fatal("fixture wrapper changed backing store identity")
	}
	if objects, err := store.List(ctx, "blob/"); err != nil || len(objects) != 2 {
		t.Fatalf("delegated list objects=%v err=%v", objects, err)
	}
	if err := store.SetLifecycle(ctx, nil); err != nil {
		t.Fatalf("delegated lifecycle configuration=%v", err)
	}

	readErr = errors.New("injected read failure")
	if reader, err := (&fixtureFailureBlobStore{store: backing, readErr: readErr}).Get(ctx, "blob/immutable"); reader != nil || !errors.Is(err, readErr) {
		t.Fatalf("read failure reader=%v err=%v", reader, err)
	}
	listErr := errors.New("injected list failure")
	if objects, err := (&fixtureFailureBlobStore{store: backing, listErr: listErr}).List(ctx, "blob/"); objects != nil || !errors.Is(err, listErr) {
		t.Fatalf("list failure objects=%v err=%v", objects, err)
	}
	lifecycleErr := errors.New("injected lifecycle failure")
	if err := (&fixtureFailureBlobStore{store: backing, lifecycleErr: lifecycleErr}).SetLifecycle(ctx, nil); !errors.Is(err, lifecycleErr) {
		t.Fatalf("lifecycle failure=%v", err)
	}
}

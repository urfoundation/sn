// Test fixture storage faults stay explicit at every object-store boundary.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urnetwork/server/v2026"
)

// fixtureFailureBlobStore keeps fault injection explicit at every BlobStore
// boundary so promoted delegate methods cannot bypass the intended failure.
type fixtureFailureBlobStore struct {
	store         server.BlobStore
	writeErr      error
	readErr       error
	streamReadErr error
	closeErr      error
	listErr       error
	lifecycleErr  error
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

// Reject object open or wrap its returned stream with an injected read/close
// failure without changing other store behavior.
func (self *fixtureFailureBlobStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if self.readErr != nil {
		return nil, self.readErr
	}
	reader, err := self.store.Get(ctx, key)
	if err != nil || self.streamReadErr == nil && self.closeErr == nil {
		return reader, err
	}
	return &fixtureFailureReadCloser{ReadCloser: reader, readErr: self.streamReadErr, closeErr: self.closeErr}, nil
}

// Keeps stream faults deterministic while still closing the backing object.
type fixtureFailureReadCloser struct {
	io.ReadCloser
	readErr  error
	closeErr error
}

// Reject a stream read before consuming backing bytes.
func (self *fixtureFailureReadCloser) Read(buffer []byte) (int, error) {
	if self.readErr != nil {
		return 0, self.readErr
	}
	return self.ReadCloser.Read(buffer)
}

// Close the backing object, then surface the injected close failure.
func (self *fixtureFailureReadCloser) Close() error {
	err := self.ReadCloser.Close()
	if self.closeErr != nil {
		return errors.Join(err, self.closeErr)
	}
	return err
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

// fixtureMemoryBlobStore is one isolated operator replica held in memory. It
// keeps the contract a publisher relies on, an atomic first-writer
// PutIfAbsent and exact readback, without the local backend's census of every
// stored object on each write, which makes a release-scale publication
// quadratic in its own object count.
type fixtureMemoryBlobStore struct {
	prefix    string
	stateLock sync.Mutex
	objects   map[string][]byte
}

var _ server.BlobStore = (*fixtureMemoryBlobStore)(nil)

func newFixtureMemoryBlobStore(prefix string) *fixtureMemoryBlobStore {
	return &fixtureMemoryBlobStore{prefix: prefix, objects: map[string][]byte{}}
}

// Replace the object with the source file's exact current bytes.
func (self *fixtureMemoryBlobStore) Put(_ context.Context, key, localPath, _ string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.objects[key] = data
	return nil
}

// Create the object only when no writer owns its key. An earlier winner keeps
// its bytes, as an immutable create requires.
func (self *fixtureMemoryBlobStore) PutIfAbsent(_ context.Context, key, localPath, _ string) (bool, error) {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return false, err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if _, exists := self.objects[key]; exists {
		return false, nil
	}
	self.objects[key] = data
	return true, nil
}

// Open the stored bytes, or report a missing object as nonexistent.
func (self *fixtureMemoryBlobStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	data, ok := self.objects[key]
	if !ok {
		return nil, fmt.Errorf("fixture memory object %s: %w", key, os.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Enumerate the objects under a key prefix in key order.
func (self *fixtureMemoryBlobStore) List(_ context.Context, keyPrefix string) ([]server.BlobObject, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	objects := []server.BlobObject{}
	for key, data := range self.objects {
		if strings.HasPrefix(key, keyPrefix) {
			objects = append(objects, server.BlobObject{Key: key, Size: int64(len(data))})
		}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	return objects, nil
}

// Refuse expiry rules, which a replica without a reaper cannot honor.
func (self *fixtureMemoryBlobStore) SetLifecycle(_ context.Context, rules []server.BlobLifecycleRule) error {
	if len(rules) != 0 {
		return errors.New("fixture memory store does not expire objects")
	}
	return nil
}

// Name the in-memory backing bucket used in publication receipts.
func (self *fixtureMemoryBlobStore) Bucket() string {
	return "memory"
}

// Preserve the operator namespace used for key composition.
func (self *fixtureMemoryBlobStore) Prefix() string {
	return self.prefix
}

// Name the in-memory endpoint.
func (self *fixtureMemoryBlobStore) Authority() string {
	return "memory"
}

// Proves the in-memory replica keeps the first immutable winner, replaces only
// through Put, reads back exact bytes detached from the source file, and
// refuses missing sources, missing objects and expiry rules.
func TestFixtureMemoryBlobStoreKeepsImmutableWinnerAndExactBytes(t *testing.T) {
	ctx := context.Background()
	store := newFixtureMemoryBlobStore("blob")
	directory := t.TempDir()
	firstPath := filepath.Join(directory, "first")
	secondPath := filepath.Join(directory, "second")
	missingPath := filepath.Join(directory, "missing")
	first := []byte("first immutable value")
	second := []byte("conflicting immutable value")
	if err := os.WriteFile(firstPath, first, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, second, 0o600); err != nil {
		t.Fatal(err)
	}
	read := func(key string) []byte {
		t.Helper()
		reader, err := store.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		stored, readErr := io.ReadAll(reader)
		if err := errors.Join(readErr, reader.Close()); err != nil {
			t.Fatal(err)
		}
		return stored
	}

	if created, err := store.PutIfAbsent(ctx, "blob/immutable", missingPath, "application/octet-stream"); created || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-source conditional write created=%t err=%v", created, err)
	}
	if err := store.Put(ctx, "blob/ordinary", missingPath, "application/octet-stream"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-source ordinary write=%v", err)
	}
	if reader, err := store.Get(ctx, "blob/immutable"); reader != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing object reader=%v err=%v", reader, err)
	}
	if created, err := store.PutIfAbsent(ctx, "blob/immutable", firstPath, "application/octet-stream"); err != nil || !created {
		t.Fatalf("fresh conditional write created=%t err=%v", created, err)
	}
	if created, err := store.PutIfAbsent(ctx, "blob/immutable", secondPath, "application/octet-stream"); err != nil || created {
		t.Fatalf("conflicting conditional write created=%t err=%v", created, err)
	}
	if err := os.WriteFile(firstPath, second, 0o600); err != nil {
		t.Fatal(err)
	}
	if stored := read("blob/immutable"); !bytes.Equal(stored, first) {
		t.Fatalf("conditional winner=%q, want detached first bytes", stored)
	}
	if err := store.Put(ctx, "blob/ordinary", secondPath, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, first, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "blob/ordinary", secondPath, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if stored := read("blob/ordinary"); !bytes.Equal(stored, first) {
		t.Fatalf("ordinary replacement=%q, want the latest source bytes", stored)
	}
	objects, err := store.List(ctx, "blob/")
	if err != nil || len(objects) != 2 || objects[0] != (server.BlobObject{Key: "blob/immutable", Size: int64(len(first))}) || objects[1] != (server.BlobObject{Key: "blob/ordinary", Size: int64(len(first))}) {
		t.Fatalf("prefix list objects=%v err=%v", objects, err)
	}
	if objects, err := store.List(ctx, "other/"); err != nil || len(objects) != 0 {
		t.Fatalf("disjoint prefix list objects=%v err=%v", objects, err)
	}
	if err := store.SetLifecycle(ctx, nil); err != nil {
		t.Fatalf("empty lifecycle configuration=%v", err)
	}
	if err := store.SetLifecycle(ctx, []server.BlobLifecycleRule{{KeyPrefix: "blob/", TTL: time.Hour}}); err == nil || !strings.Contains(err.Error(), "does not expire") {
		t.Fatalf("expiry rule was accepted: %v", err)
	}
	if store.Bucket() != "memory" || store.Prefix() != "blob" || store.Authority() != "memory" {
		t.Fatalf("memory store identity bucket=%q prefix=%q authority=%q", store.Bucket(), store.Prefix(), store.Authority())
	}
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
	streamReadErr := errors.New("injected stream read failure")
	reader, err = (&fixtureFailureBlobStore{store: backing, streamReadErr: streamReadErr}).Get(ctx, "blob/immutable")
	if err != nil {
		t.Fatal(err)
	}
	if value, err := io.ReadAll(reader); len(value) != 0 || !errors.Is(err, streamReadErr) {
		t.Fatalf("stream read failure value=%q err=%v", value, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("stream read failure close=%v", err)
	}
	injectedCloseErr := errors.New("injected close failure")
	reader, err = (&fixtureFailureBlobStore{store: backing, closeErr: injectedCloseErr}).Get(ctx, "blob/immutable")
	if err != nil {
		t.Fatal(err)
	}
	if value, err := io.ReadAll(reader); err != nil || !bytes.Equal(value, first) {
		t.Fatalf("close failure read value=%q err=%v", value, err)
	}
	if err := reader.Close(); !errors.Is(err, injectedCloseErr) {
		t.Fatalf("close failure=%v", err)
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

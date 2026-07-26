package miner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect"
)

func TestStoredClientIdRoundTrips(t *testing.T) {
	dir := t.TempDir()

	id, err := readStoredClientId(dir)
	if err != nil {
		t.Fatalf("read on empty dir: %s", err)
	}
	if id != nil {
		t.Fatalf("expected no stored id, got %s", id)
	}

	want := connect.NewId()
	if err := writeStoredClientId(dir, want); err != nil {
		t.Fatalf("write: %s", err)
	}

	got, err := readStoredClientId(dir)
	if err != nil {
		t.Fatalf("read after write: %s", err)
	}
	if got == nil {
		t.Fatal("expected a stored id after write, got nil")
	}
	if *got != want {
		t.Errorf("stored id = %s, want %s", got, want)
	}
}

func TestStoredClientIdIgnoresCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "client_id"), []byte("not-a-uuid"), 0600); err != nil {
		t.Fatal(err)
	}

	// a corrupt file must not wedge the provider: it re-auths and overwrites
	// rather than refusing to start
	id, err := readStoredClientId(dir)
	if err != nil {
		t.Fatalf("read corrupt file: %s", err)
	}
	if id != nil {
		t.Errorf("expected nil for a corrupt file, got %s", id)
	}
}

func TestStoredClientIdFileIsNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	if err := writeStoredClientId(dir, connect.NewId()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "client_id"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0077 != 0 {
		t.Errorf("client_id mode = %o, want no group/other access", perm)
	}
}

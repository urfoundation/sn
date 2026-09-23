package crv4

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSeedCustodyExpectedSeedPublicationNeverReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "payout.seed")
	seed := [32]byte{9}
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := EnsureSeedFile(path, seed); err != nil {
				t.Errorf("publish or reload expected seed: %v", err)
			}
		}()
	}
	group.Wait()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("seed was not published as a private regular file: %v", err)
	}
	if actual, err := LoadSeedFile(path); err != nil || actual != seed {
		t.Fatalf("published seed differs: %v", err)
	}
	if err := EnsureSeedFile(path, [32]byte{10}); err == nil {
		t.Fatal("a different payout seed replaced occupied custody")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("refused replacement changed existing bytes: %v", err)
	}
	if err := os.WriteFile(path, []byte("incomplete"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSeedFile(path, seed); err == nil {
		t.Fatal("malformed occupied seed was regenerated")
	}
	after, err = os.ReadFile(path)
	if err != nil || string(after) != "incomplete" {
		t.Fatalf("malformed occupied seed was changed: %v", err)
	}
}

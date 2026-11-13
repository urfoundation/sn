// Custody tests use real filesystem operations and process locks so restart,
// tamper, and competing-writer guarantees are observable outside one instance.
package payoutroster

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	"golang.org/x/sys/unix"
)

// Tests explicitly satisfy the production private-directory setup contract.
func storeTestDirectory(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

// All three durable stages survive reopening and refuse competing bytes.
func TestStoreRetainsImmutableWorkflowAcrossRestart(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	domain, _ := authority.Domain.Digest()
	path := storeTestDirectory(t)
	store, err := OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Load(t.Context(), domain, authority.Epoch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent authority: %v", err)
	}
	request := []byte(`{"synthetic_review":"complete"}`)
	if err := store.RetainRequest(t.Context(), domain, authority.Epoch, request); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.Retain(t.Context(), domain, authority.Epoch, raw); err != nil {
			t.Fatal(err)
		}
	}
	_, competing := transportTestOriginal(t)
	if err := store.Retain(t.Context(), domain, authority.Epoch, competing); !errors.Is(err, ErrStoreConflict) {
		t.Fatalf("competing signed original: %v", err)
	}
	if err := store.RetainRequest(t.Context(), domain, authority.Epoch, []byte(`{"synthetic_review":"different"}`)); !errors.Is(err, ErrStoreConflict) {
		t.Fatalf("competing reviewed input: %v", err)
	}
	hash := sha256.Sum256(raw)
	if published, err := store.IsPublished(t.Context(), domain, authority.Epoch, hash); err != nil || published {
		t.Fatalf("unsigned acknowledgement: published=%v error=%v", published, err)
	}
	if err := store.RetainPublished(t.Context(), domain, authority.Epoch, hash); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	retained, err := store.Load(t.Context(), domain, authority.Epoch)
	if err != nil || !bytes.Equal(retained, raw) {
		t.Fatalf("retained original differs after restart: %v", err)
	}
	retainedRequest, err := store.LoadRequest(t.Context(), domain, authority.Epoch)
	if err != nil || !bytes.Equal(retainedRequest, request) {
		t.Fatalf("retained review differs after restart: %v", err)
	}
	if published, err := store.IsPublished(t.Context(), domain, authority.Epoch, hash); err != nil || !published {
		t.Fatalf("lost acknowledgement: published=%v error=%v", published, err)
	}
	otherHash := hash
	otherHash[0] ^= 1
	if _, err := store.IsPublished(t.Context(), domain, authority.Epoch, otherHash); !errors.Is(err, ErrStoreConflict) {
		t.Fatalf("acknowledgement accepted competing digest: %v", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 3 {
		t.Fatalf("unexpected retained files: %d, %v", len(entries), err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || strings.HasPrefix(entry.Name(), ".pending-") {
			t.Fatalf("invalid custody leaf %s: %v", entry.Name(), err)
		}
	}
}

// A surviving acknowledgement remains inspectable before either missing review
// or original is restored, so mismatched recovery cannot poison those slots.
func TestStorePublishedHashSurvivesOriginalLoss(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	domain, _ := authority.Domain.Digest()
	path := storeTestDirectory(t)
	store, err := OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if hash, exists, err := store.PublishedHash(t.Context(), domain, authority.Epoch); err != nil || exists || hash != ([32]byte{}) {
		t.Fatalf("absent acknowledgement: hash=%x exists=%v error=%v", hash, exists, err)
	}
	if err := store.Retain(t.Context(), domain, authority.Epoch, raw); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(raw)
	if err := store.RetainPublished(t.Context(), domain, authority.Epoch, want); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	name, _ := storeName(domain, authority.Epoch, "authority.json")
	if err := os.Remove(filepath.Join(path, name)); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if hash, exists, err := store.PublishedHash(t.Context(), domain, authority.Epoch); err != nil || !exists || hash != want {
		t.Fatalf("lost independent recovery pin: hash=%x exists=%v error=%v", hash, exists, err)
	}
	if _, err := store.IsPublished(t.Context(), domain, authority.Epoch, want); !errors.Is(err, ErrStoreIntegrity) {
		t.Fatalf("missing original acknowledged: %v", err)
	}
}

// A truncated or zero acknowledgement never supplies a recovery hash.
func TestStoreRejectsMalformedPublishedHash(t *testing.T) {
	domain := [32]byte{1}
	for _, raw := range [][]byte{make([]byte, sha256.Size), []byte("short synthetic hash"), bytes.Repeat([]byte{1}, sha256.Size+1)} {
		path := storeTestDirectory(t)
		name, _ := storeName(domain, 7, "published")
		if err := os.WriteFile(filepath.Join(path, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
		store, err := OpenStore(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		hash, exists, readErr := store.PublishedHash(t.Context(), domain, 7)
		store.Close()
		if !errors.Is(readErr, ErrStoreIntegrity) || exists || hash != ([32]byte{}) {
			t.Errorf("invalid acknowledgement accepted: size=%d hash=%x exists=%v error=%v", len(raw), hash, exists, readErr)
		}
	}
}

// A crash between no-clobber link and cleanup leaves two links to the same synced
// original. Reopening removes only the temporary name and recovers exact bytes.
func TestStoreRecoversInterruptedNoClobberCommit(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	domain, _ := authority.Domain.Digest()
	path := storeTestDirectory(t)
	store, err := OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Retain(t.Context(), domain, authority.Epoch, raw); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	name, _ := storeName(domain, authority.Epoch, "authority.json")
	linked := filepath.Join(path, ".pending-"+strings.Repeat("12", 16))
	if err := os.Link(filepath.Join(path, name), linked); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(path, ".pending-"+strings.Repeat("34", 16))
	if err := os.WriteFile(orphan, []byte("interrupted synthetic original"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	retained, err := store.Load(t.Context(), domain, authority.Epoch)
	if err != nil || !bytes.Equal(retained, raw) {
		t.Fatalf("crash recovery changed original: %v", err)
	}
	for _, pending := range []string{linked, orphan} {
		if _, err := os.Lstat(pending); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("pending name survived recovery: %v", err)
		}
	}
}

// Corrupted retained bytes cannot become a missing-file path that invokes a key.
func TestStoreRejectsTamperingAcrossRestart(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	domain, _ := authority.Domain.Digest()
	path := storeTestDirectory(t)
	store, err := OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Retain(t.Context(), domain, authority.Epoch, raw); err != nil {
		t.Fatal(err)
	}
	name, _ := storeName(domain, authority.Epoch, "authority.json")
	authority.Signature[0] ^= 1
	corrupt, _ := json.Marshal(authority)
	if err := os.WriteFile(filepath.Join(path, name), corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(t.Context(), domain, authority.Epoch); !errors.Is(err, ErrStoreIntegrity) || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("in-session tamper: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Load(t.Context(), domain, authority.Epoch); !errors.Is(err, ErrStoreIntegrity) || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("post-restart tamper: %v", err)
	}
}

// Deletion after custody was observed is integrity loss, not permission to sign.
func TestStoreRejectsDeletedOriginal(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	domain, _ := authority.Domain.Digest()
	path := storeTestDirectory(t)
	store, err := OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Retain(t.Context(), domain, authority.Epoch, raw); err != nil {
		t.Fatal(err)
	}
	name, _ := storeName(domain, authority.Epoch, "authority.json")
	if err := os.Remove(filepath.Join(path, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(t.Context(), domain, authority.Epoch); !errors.Is(err, ErrStoreIntegrity) || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted original was treated as first signing: %v", err)
	}
	if err := store.Retain(t.Context(), domain, authority.Epoch, raw); !errors.Is(err, ErrStoreIntegrity) {
		t.Fatalf("deleted original was recreated: %v", err)
	}
}

// Directory setup is explicit; symlink components and permissive custody fail.
func TestStoreRejectsUnsafeDirectories(t *testing.T) {
	parent := t.TempDir()
	private := filepath.Join(parent, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(private, alias); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(private, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{alias, filepath.Join(alias, "child"), filepath.Join(parent, "missing")} {
		if store, err := OpenStore(t.Context(), path); err == nil {
			store.Close()
			t.Errorf("unsafe directory accepted: %s", path)
		}
	}
	if err := os.Chmod(private, 0750); err != nil {
		t.Fatal(err)
	}
	if store, err := OpenStore(t.Context(), private); !errors.Is(err, ErrStoreIntegrity) {
		if store != nil {
			store.Close()
		}
		t.Fatalf("permissive custody accepted: %v", err)
	}
}

// Bounded no-follow descriptors reject dangerous leaf types before reading them.
func TestStoreRejectsUnsafeLeaves(t *testing.T) {
	authority, raw := transportTestOriginal(t)
	domain, _ := authority.Domain.Digest()
	for _, kind := range []string{"symlink", "fifo", "oversize", "permissions", "hardlink"} {
		path := storeTestDirectory(t)
		name, _ := storeName(domain, authority.Epoch, "authority.json")
		leaf := filepath.Join(path, name)
		switch kind {
		case "symlink", "hardlink":
			target := filepath.Join(t.TempDir(), "synthetic-original.json")
			if err := os.WriteFile(target, raw, 0600); err != nil {
				t.Fatal(err)
			}
			link := os.Symlink
			if kind == "hardlink" {
				link = os.Link
			}
			if err := link(target, leaf); err != nil {
				t.Fatal(err)
			}
		case "fifo":
			if err := unix.Mkfifo(leaf, 0600); err != nil {
				t.Fatal(err)
			}
		case "oversize":
			file, err := os.OpenFile(leaf, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(file.Truncate(payoutartifact.MaxWholeWorkAuthorityBytes+1), file.Close()); err != nil {
				t.Fatal(err)
			}
		case "permissions":
			if err := os.WriteFile(leaf, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(leaf, 0644); err != nil {
				t.Fatal(err)
			}
		}
		store, err := OpenStore(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		_, loadErr := store.Load(t.Context(), domain, authority.Epoch)
		store.Close()
		if !errors.Is(loadErr, ErrStoreIntegrity) || errors.Is(loadErr, os.ErrNotExist) {
			t.Errorf("unsafe %s leaf accepted: %v", kind, loadErr)
		}
	}
}

// Cancellation belongs to the instance and stops further custody mutations.
func TestStoreCancellationStopsWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	path := storeTestDirectory(t)
	store, err := OpenStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cancel()
	if err := store.RetainRequest(t.Context(), [32]byte{1}, 7, []byte("synthetic request")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled owner wrote request: %v", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancellation changed custody: files=%d error=%v", len(entries), err)
	}
	if _, err := OpenStore(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lock admission: %v", err)
	}
}

// A separate process observes the actual store lock, then acquires after Close.
func TestStoreSerializesIndependentProcesses(t *testing.T) {
	path := storeTestDirectory(t)
	store, err := OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestStoreProcessLockHelper$")
	command.Env = append(os.Environ(), "ROSTER_STORE_LOCK_CHILD="+path)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "blocked" {
		t.Fatalf("child did not observe the process lock: %q %s", scanner.Text(), stderr.String())
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(stdin, "acquire"); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	if !scanner.Scan() || scanner.Text() != "acquired" {
		t.Fatalf("child did not acquire released lock: %q %s", scanner.Text(), stderr.String())
	}
	for scanner.Scan() {
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("child failed: %v %s", err, stderr.String())
	}
}

// The subprocess uses direct flock as an independent observer of lock custody.
func TestStoreProcessLockHelper(t *testing.T) {
	path := os.Getenv("ROSTER_STORE_LOCK_CHILD")
	if path == "" {
		return
	}
	directory, err := openRosterDirectory(path, true)
	if err != nil {
		t.Fatal(err)
	}
	err = unix.Flock(int(directory.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	directory.Close()
	if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("store did not own cross-process lock: %v", err)
	}
	fmt.Println("blocked")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() || scanner.Text() != "acquire" {
		t.Fatal("missing lock transfer instruction")
	}
	store, err := OpenStore(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RetainRequest(t.Context(), [32]byte{1}, 7, []byte("synthetic process request")); err != nil {
		t.Fatal(err)
	}
	fmt.Println("acquired")
}

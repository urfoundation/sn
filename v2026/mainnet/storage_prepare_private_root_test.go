//go:build linux || darwin

// Public creation prepares one reviewed private inode in staging, then moves
// that exact root. Neither planning nor a missing old root grants enrollment.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The fixture supplies explicit parent identity and zero former owner history;
// only its own never-used root is removed to exercise genuine absence.
func storagePreparationPrivateRootRequest(t *testing.T, f *storagePreparationCommandFixture) {
	t.Helper()
	if err := os.Remove(f.root); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(filepath.Dir(f.root), "private-root-parent")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	f.root = filepath.Join(parent, "new-runtime-root")
	raw, err := os.ReadFile(f.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]json.RawMessage
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	request["root_creation"] = json.RawMessage(`"create-private"`)
	request["root_path"], err = json.Marshal(f.root)
	if err != nil {
		t.Fatal(err)
	}
	var ref durablevolume.Reference
	if err := json.Unmarshal(request["former_writer_fence"], &ref); err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Stat(parent, &stat); err != nil {
		t.Fatal(err)
	}
	fence, err := json.Marshal(map[string]any{"schema": durablevolume.PreparationFenceSchema, "root_path": f.root, "root_inode": 0, "parent_inode": stat.Ino, "purpose": "fresh", "former_writers_stopped": true, "no_previous_owner_state": true, "evidence": "synthetic new private namespace; no previous owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref.Path, fence, 0600); err != nil {
		t.Fatal(err)
	}
	ref.Sha256 = durablefixture.Digest(fence)
	request["former_writer_fence"], err = json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.requestHash = durablefixture.Digest(raw)
}

// Plan returns its exact staged root while the live target remains absent.
func storagePreparationPrivateRootPlan(t *testing.T, f *storagePreparationCommandFixture, command string) (string, string, os.FileInfo) {
	t.Helper()
	path, hash := storagePreparationFreezeOwnerPlan(t, f, command)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		RootSource string `json:"root_source"`
		Root       struct {
			Inode uint64 `json:"inode"`
		} `json:"root"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil || plan.RootSource == "" || plan.Root.Inode == 0 {
		t.Fatal("private root plan omitted exact source identity", err)
	}
	if _, err := os.Lstat(f.root); !os.IsNotExist(err) {
		t.Fatal("private root plan populated live target", err)
	}
	source, err := os.Stat(plan.RootSource)
	if err != nil || !source.IsDir() || source.Mode().Perm() != 0700 || source.Sys().(*syscall.Stat_t).Ino != plan.Root.Inode {
		t.Fatal("private root staging lost exact private inode", err)
	}
	return path, hash, source
}

// Applying the plan returns a declaration for the same inode and no restart.
func storagePreparationApplyPrivateRoot(t *testing.T, f *storagePreparationCommandFixture, command, path, hash string, original os.FileInfo) context.Context {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, []string{command, "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("accepted private root cannot publish", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("private root result lost scope", err)
	}
	retained, err := os.Stat(f.root)
	if err != nil || !os.SameFile(original, retained) || retained.Mode().Perm() != 0700 {
		t.Fatal("apply did not retain reviewed private inode", err)
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), f.storage.Host)
}

// Real daemon native custody opens only after the reviewed new root is moved.
func TestStoragePreparationCommandCreatesReviewedPrivateRoot(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
	storagePreparationPrivateRootRequest(t, f)
	path, hash, original := storagePreparationPrivateRootPlan(t, f, "storage-prepare")
	ctx := storagePreparationApplyPrivateRoot(t, f, "storage-prepare", path, hash, original)
	owner, err := chain.OpenDurableJournal(ctx, f.root)
	if err != nil {
		t.Fatal("new root cannot enter actual daemon owner", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

// Owner-local creation stays a separately selected profile on the same device.
func TestStoragePreparationOwnerCommandCreatesReviewedPrivateRoot(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	storagePreparationOwnerRequest(t, f, "owner-local", []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
	storagePreparationPrivateRootRequest(t, f)
	path, hash, original := storagePreparationPrivateRootPlan(t, f, "storage-owner-prepare")
	ctx := storagePreparationApplyPrivateRoot(t, f, "storage-owner-prepare", path, hash, original)
	owner, err := chain.OpenOwnerLocalJournal(ctx, f.root)
	if err != nil {
		t.Fatal("new root cannot enter actual owner-local custody", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if daemon, err := chain.OpenDurableJournal(ctx, f.root); err == nil {
		daemon.Close()
		t.Fatal("private owner root gained daemon authority")
	}
}

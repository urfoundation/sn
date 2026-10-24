//go:build linux || darwin

// Snapshot restore runs through the actual public command and the original
// owner constructors. Restored logical paths stay unchanged where original
// signed/request bindings include them; only physical generations are new.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// No runtime guard is manufactured by this helper. It copies a real exported
// source, then creates an explicitly empty replacement root at the old path.
type storageSnapshotRestoreFixture struct {
	target     *storagePreparationCommandFixture
	command    string
	heldSource string
	archive    string
	owner      durablevolume.PreparationOwner
}

// Source export follows the caller's joined writer. All original files and
// custody attributes remain both in the held tree and in the copied archive.
func storageSnapshotRestoreTarget(t *testing.T, source *storagePreparationCommandFixture, sourceContext context.Context, owner durablevolume.PreparationOwner, ownerLocal bool) *storageSnapshotRestoreFixture {
	t.Helper()
	return storageSnapshotRestoreTargetWithLimits(t, source, sourceContext, owner, ownerLocal, nil)
}

// Large accepted namespaces retain explicit exporter bounds as well as their
// restore bounds; no successful small-profile report can stand in for them.
func storageSnapshotRestoreTargetWithLimits(t *testing.T, source *storagePreparationCommandFixture, sourceContext context.Context, owner durablevolume.PreparationOwner, ownerLocal bool, limits *durablevolume.InventoryLimits) *storageSnapshotRestoreFixture {
	t.Helper()
	command := "storage-prepare"
	if ownerLocal {
		command = "storage-owner-prepare"
	}
	reference, present := durablevolume.ReferenceFromContext(sourceContext)
	if !present {
		t.Fatal("source runtime declaration is missing")
	}
	fence := storagePreparationExportFence(t, source, reference, ownerLocal)
	var output, diagnostic bytes.Buffer
	args := []string{command, "export", "--root", source.root, "--former-writer-fence", fence.Path, "--former-writer-fence-sha256", fence.Sha256}
	if limits != nil {
		args = append(args, "--max-entries", fmt.Sprint(limits.MaxEntries), "--max-bytes", fmt.Sprint(limits.MaxBytes), "--max-depth", fmt.Sprint(limits.MaxDepth), "--max-owner-attributes", fmt.Sprint(limits.MaxOwnerAttributes), "--max-owner-attribute-bytes", fmt.Sprint(limits.MaxOwnerAttributeBytes))
	}
	if code := runMain(sourceContext, args, &output, &diagnostic); code != 0 {
		t.Fatal("joined snapshot source cannot export", code, diagnostic.String())
	}
	var report durablevolume.Inventory
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(source.root)
	archive, metadata, staging := filepath.Join(parent, "snapshot-archive"), filepath.Join(parent, "restore-metadata"), filepath.Join(parent, "restore-staging")
	for _, path := range []string{archive, metadata, staging} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range report.Entries {
		path := filepath.Join(archive, entry.Path)
		if entry.Path != "" {
			if entry.Kind == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				raw, err := os.ReadFile(filepath.Join(source.root, entry.Path))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, os.FileMode(entry.Mode)); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, attribute := range entry.OwnerAttributes {
			if err := unix.Setxattr(path, attribute.Name, attribute.Value, unix.XATTR_CREATE); err != nil {
				t.Fatal(err)
			}
		}
	}
	nonce, err := hex.DecodeString(report.RootGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(archive, durablevolume.RootGenerationAttribute, nonce, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	held := source.root + ".original-held"
	if err := os.Rename(source.root, held); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(source.root, 0700); err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	raw, err := os.ReadFile(source.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(metadata, "original-inventory.json")
	if err := os.WriteFile(reportPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	request.Purpose, owner.Purpose = "restore", "restore"
	request.Owners = []durablevolume.PreparationOwner{owner}
	request.StagingDirectory = staging
	request.MarkerPath, request.LeasePath = filepath.Join(metadata, "identity"), filepath.Join(metadata, "lease")
	request.DeclarationPath, request.ControlPath = filepath.Join(metadata, "durable-volumes.json"), filepath.Join(metadata, "preparation.jsonl")
	request.RestoreSource = &durablevolume.PreparationRestoreSource{Directory: archive, Inventory: durablevolume.Reference{Path: reportPath, Sha256: durablefixture.Digest(output.Bytes())}, FormerWriterFence: fence}
	var root unix.Stat_t
	if err := unix.Stat(source.root, &root); err != nil {
		t.Fatal(err)
	}
	targetFence, err := json.Marshal(durablevolume.PreparationFence{Schema: durablevolume.PreparationFenceSchema, RootPath: source.root, RootInode: root.Ino, Purpose: "restore", FormerWritersStopped: true, NoPreviousTargetState: true, Evidence: "synthetic new physical target retains known historical logical path"})
	if err != nil {
		t.Fatal(err)
	}
	fencePath := filepath.Join(metadata, "target-fence.json")
	if err := os.WriteFile(fencePath, targetFence, 0600); err != nil {
		t.Fatal(err)
	}
	request.FormerWriterFence = durablevolume.Reference{Path: fencePath, Sha256: durablefixture.Digest(targetFence)}
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(metadata, "request.json")
	if err := os.WriteFile(requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	target := &storagePreparationCommandFixture{ctx: durablepath.WithHost(t.Context(), source.storage.Host), storage: source.storage, root: source.root, metadata: metadata, requestPath: requestPath, requestHash: durablefixture.Digest(raw)}
	return &storageSnapshotRestoreFixture{target: target, command: command, heldSource: held, archive: archive, owner: owner}
}

// Applying an approved plan cannot itself authorize a service or device call.
func (self *storageSnapshotRestoreFixture) apply(t *testing.T) context.Context {
	t.Helper()
	path, hash := storagePreparationFreezeOwnerPlan(t, self.target, self.command)
	var output, diagnostic bytes.Buffer
	if code := runMain(self.target.ctx, []string{self.command, "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public snapshot restore apply failed", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("snapshot restore acquired restart authority", err)
	}
	for _, member := range []string{self.ownerName(t), self.ownerName(t) + ".lock"} {
		before, err := os.ReadFile(filepath.Join(self.heldSource, member))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(self.target.root, member))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("snapshot restore rewrote original record or marker", member, err)
		}
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), self.target.storage.Host)
}

// All current fixed snapshot profiles contain the original literal basename.
func (self *storageSnapshotRestoreFixture) ownerName(t *testing.T) string {
	t.Helper()
	var scope struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(self.owner.Inputs, &scope); err != nil || scope.Name == "" {
		t.Fatal("snapshot fixture lost its fixed name", err)
	}
	return scope.Name
}

// Actual monitor startup consumes the restored original checkpoint and keeps
// finality/outage history; no rpc or service loop is started by this test.
func TestStoragePreparationRestoreMonitorKeepsActualContinuation(t *testing.T) {
	source := newStoragePreparationCommandFixture(t)
	owner := storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "monitor.json", maxRpcReplyBytes)
	storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{owner})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	expected := identityExpectation{NativeChain: "synthetic-chain", GenesisHash: "0x" + strings.Repeat("25", 32), EvmChainId: mainnetEvmChainId}
	store, err := openMonitorCheckpoint(filepath.Join(source.root, "monitor.json"), expected, ctx)
	if err != nil {
		t.Fatal(err)
	}
	state := &monitorState{lastNumber: 71, lastHash: "0x" + strings.Repeat("26", 32), lastProgressAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), lastSuccessAt: time.Date(2020, 1, 1, 0, 1, 0, 0, time.UTC)}
	if err := errors.Join(store.save(state), store.close()); err != nil {
		t.Fatal(err)
	}
	f := storageSnapshotRestoreTarget(t, source, ctx, owner, false)
	restored := f.apply(t)
	store, err = openMonitorCheckpoint(filepath.Join(f.target.root, "monitor.json"), expected, restored)
	if err != nil {
		t.Fatal("restored monitor cannot open actual original continuation", err)
	}
	actual, err := store.load()
	if closeErr := store.close(); err != nil || closeErr != nil || actual == nil || actual.lastNumber != state.lastNumber || actual.lastHash != state.lastHash || !actual.lastProgressAt.Equal(state.lastProgressAt) || !actual.lastSuccessAt.Equal(state.lastSuccessAt) {
		t.Fatal("restore lost actual finalized monitor continuity", err, closeErr)
	}
}

// Owner-local reservation includes its absolute path. Restoring the original
// logical path preserves that binding without editing a signed request or
// invoking the hardware adapter. A different binding must still be refused.
func TestStoragePreparationRestoreOwnerReservationKeepsOriginalBinding(t *testing.T) {
	source := newStoragePreparationCommandFixture(t)
	owner := storagePreparationSnapshotOwner(t, "mainnet-owner-signing", "signing.json", ownerSigningReplyLimit)
	storagePreparationOwnerRequest(t, source, "owner-local", []durablevolume.PreparationOwner{owner})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-owner-prepare")
	config := ownerSigningDeviceConfig{StatePath: filepath.Join(source.root, "signing.json"), PythonPath: "/usr/bin/python3", HelperPath: "/synthetic/helper.py", BackendPath: "/synthetic/backend.py", HelperHash: "sha256:" + strings.Repeat("21", 32), BackendHash: "sha256:" + strings.Repeat("22", 32), AppVersion: [3]uint16{100, 0, 5}}
	request := ownerSigningRequest{ContentHash: "sha256:" + strings.Repeat("23", 32)}
	request.Config.Action.StatePath = filepath.Join(source.metadata, "separate-action.json")
	store, err := openOwnerSigningDeviceStore(ctx, config, request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.load()
	if closeErr := store.close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	f := storageSnapshotRestoreTarget(t, source, ctx, owner, true)
	restored := f.apply(t)
	store, err = openOwnerSigningDeviceStore(restored, config, request)
	if err != nil {
		t.Fatal("restored owner cannot retain original request reservation", err)
	}
	after, err := store.load()
	if closeErr := store.close(); err != nil || closeErr != nil || before.ContentHash != after.ContentHash || after.Phase != "reserved" || after.Reply != nil {
		t.Fatal("restored owner changed original device intent", err, closeErr)
	}
	config.BackendHash = "sha256:" + strings.Repeat("24", 32)
	changed, err := openOwnerSigningDeviceStore(restored, config, request)
	if changed != nil {
		changed.close()
	}
	if err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatal("restore authorized a different device binding", err)
	}
}

// The existing shared owner supplies real pending heads before and after its
// atomic exchange. Restoring either state keeps the original next payload and
// refuses passive admission until the explicit joined reconciliation runs.
func TestStoragePreparationRestorePendingSnapshotKeepsExactReconciliation(t *testing.T) {
	for _, boundary := range []string{"before-rename", "after-rename"} {
		func() {
			source := newStoragePreparationCommandFixture(t)
			owner := storagePreparationSnapshotOwner(t, "mainnet-evm-action", "action.json", 512*1024)
			storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{owner})
			ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
			spec, _, err := storagePreparationSnapshotSpec(false, owner)
			if err != nil {
				t.Fatal(err)
			}
			directory, err := durablepath.Open(ctx, source.root, durablevolume.ReadWrite, false)
			if err != nil {
				t.Fatal(err)
			}
			lock, err := os.OpenFile(filepath.Join(source.root, spec.LockName), os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			head, err := durablehead.Open(ctx, directory, lock, spec)
			if err != nil {
				t.Fatal(err)
			}
			old, next := []byte(`{"synthetic":"original predecessor"}`), []byte(`{"synthetic":"original pending intent"}`)
			if err := head.Publish(old, nil); err != nil {
				t.Fatal(err)
			}
			called := false
			stop := func() error { called = true; return syscall.EIO }
			hooks := durablehead.PublicationHooks{AfterFileSync: stop}
			if boundary == "after-rename" {
				hooks = durablehead.PublicationHooks{AfterRename: stop}
			}
			err = head.PublishWithHooks(next, hooks)
			if !called || !errors.Is(err, durablehead.ErrUncertain) || !errors.Is(err, syscall.EIO) {
				t.Fatal("original snapshot did not retain actual pending exchange", boundary, err)
			}
			if err := errors.Join(head.Close(), lock.Close(), directory.Close()); err != nil {
				t.Fatal(err)
			}
			f := storageSnapshotRestoreTarget(t, source, ctx, owner, false)
			restored := f.apply(t)
			directory, err = durablepath.Open(restored, f.target.root, durablevolume.ReadWrite, false)
			if err != nil {
				t.Fatal(err)
			}
			defer directory.Close()
			lock, err = os.OpenFile(filepath.Join(f.target.root, spec.LockName), os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			head, err = durablehead.OpenReadOnly(restored, directory, lock, spec)
			if head != nil || !errors.Is(err, durablehead.ErrUncertain) {
				t.Fatal("restore completed pending snapshot without original reconciliation", boundary, err)
			}
			head, err = durablehead.Reconcile(restored, directory, lock, spec)
			if err != nil {
				t.Fatal("restored pending snapshot cannot reconcile exact next bytes", boundary, err)
			}
			actual, present, readErr := head.Read()
			if closeErr := head.Close(); readErr != nil || closeErr != nil || !present || !bytes.Equal(actual, next) {
				t.Fatal("restored pending snapshot lost original next bytes", boundary, readErr, closeErr)
			}
		}()
	}
}

// A missing original checkpoint never turns into fresh custody, and the
// daemon entry point cannot enroll owner-device history by changing its scope.
func TestStoragePreparationRestoreSnapshotRefusesMissingHeadAndWrongScope(t *testing.T) {
	for _, mode := range []string{"missing-head", "wrong-scope"} {
		func() {
			source := newStoragePreparationCommandFixture(t)
			owner := storagePreparationSnapshotOwner(t, "mainnet-owner-signing", "signing.json", ownerSigningReplyLimit)
			storagePreparationOwnerRequest(t, source, "owner-local", []durablevolume.PreparationOwner{owner})
			ctx := storagePreparationApplyOwnerCommand(t, source, "storage-owner-prepare")
			f := storageSnapshotRestoreTarget(t, source, ctx, owner, true)
			command, want := f.command, "missing original owner authority"
			if mode == "missing-head" {
				if err := unix.Removexattr(filepath.Join(f.archive, "signing.json.lock"), durablehead.Attribute(owner.Kind, "signing.json")); err != nil {
					t.Fatal(err)
				}
			} else {
				command, want = "storage-prepare", "explicit scope"
			}
			var output, diagnostic bytes.Buffer
			code := runMain(f.target.ctx, []string{command, "plan", "--request", f.target.requestPath, "--request-sha256", f.target.requestHash}, &output, &diagnostic)
			if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), want) {
				t.Fatal("snapshot refusal missed its intended authority boundary", mode, code, diagnostic.String())
			}
			names, err := os.ReadDir(f.target.root)
			if err != nil || len(names) != 0 {
				t.Fatal("refused snapshot restore created target state", mode, err)
			}
		}()
	}
}

// A physically complete archive can still hold an incomplete application
// operation. Its exact bytes are retained, but no usable head is fabricated.
func TestStoragePreparationRestoreSnapshotRefusesPartialPendingPayload(t *testing.T) {
	source := newStoragePreparationCommandFixture(t)
	owner := storagePreparationSnapshotOwner(t, "mainnet-evm-action", "action.json", 512*1024)
	storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{owner})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	spec, _, err := storagePreparationSnapshotSpec(false, owner)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := durablepath.Open(ctx, source.root, durablevolume.ReadWrite, false)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(source.root, spec.LockName), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	head, err := durablehead.Open(ctx, directory, lock, spec)
	if err != nil {
		t.Fatal(err)
	}
	err = head.PublishWithHooks([]byte(`{"synthetic":"known pending bytes"}`), durablehead.PublicationHooks{AfterFileSync: func() error { return syscall.EIO }})
	if !errors.Is(err, durablehead.ErrUncertain) {
		t.Fatal("source did not enter actual pending state", err)
	}
	if err := errors.Join(head.Close(), lock.Close(), directory.Close()); err != nil {
		t.Fatal(err)
	}
	var checkpoint durablehead.Checkpoint
	raw := storagePreparationOwnerAttribute(t, filepath.Join(source.root, spec.LockName), durablehead.Attribute(spec.Kind, spec.Name))
	if err := json.Unmarshal(raw, &checkpoint); err != nil || checkpoint.Pending == nil {
		t.Fatal("source pending authority missing", err)
	}
	if err := os.Truncate(filepath.Join(source.root, checkpoint.Pending.Temporary), checkpoint.Pending.Next.Size-1); err != nil {
		t.Fatal(err)
	}
	f := storageSnapshotRestoreTarget(t, source, ctx, owner, false)
	var output, diagnostic bytes.Buffer
	code := runMain(f.target.ctx, []string{f.command, "plan", "--request", f.target.requestPath, "--request-sha256", f.target.requestHash}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "lacks complete original pending or exchanged bytes") {
		t.Fatal("partial original snapshot was reset or admitted", code, diagnostic.String())
	}
	names, err := os.ReadDir(f.target.root)
	if err != nil || len(names) != 0 {
		t.Fatal("partial snapshot refusal created target state", err)
	}
	retained := storagePreparationOwnerAttribute(t, filepath.Join(f.heldSource, spec.LockName), durablehead.Attribute(spec.Kind, spec.Name))
	if !bytes.Equal(raw, retained) {
		t.Fatal("failed restore rewrote original pending checkpoint")
	}
}

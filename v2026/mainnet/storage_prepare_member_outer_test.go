//go:build linux

// An unfinished census checkpoint retains both original metadata images.
// Public preparation must rebind them without choosing or losing history.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// These hooks follow actual snapshot syscalls; no census or checkpoint is
// installed directly. The old owner joins before its physical export.
func newStorageMemberOuterFixture(t *testing.T, registry bool, boundary string) *storageMemberRestoreFixture {
	t.Helper()
	source := newStoragePreparationCommandFixture(t)
	spec := bootstrapSuccessorMemberSpec(registry)
	profile := storagePreparationDirectoryOwner(t, spec.Kind)
	storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{profile})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	root, err := bootstrapSuccessorPhysicalRoot(source.root)
	if err != nil {
		t.Fatal(err)
	}
	claim := safeReleaseHash([]byte("synthetic original outer-head claimant"))
	name := bootstrapSuccessorExecutionPrefix + "synthetic-before.json"
	if registry {
		name = "relayer-outer-synthetic-before.json"
	}
	active := openStorageMemberRestoreTestOwner(t, ctx, source.root, root, claim, registry, nil)
	if err := active.publish(name, "reserved", []byte(`{"nonce":41,"signature":"synthetic original signed bytes"}`)); err != nil {
		t.Fatal(err)
	}
	name += ".next"
	payload := []byte(`{"nonce":42,"signature":"synthetic retained next signed bytes"}`)
	next := active.members.census
	next.Pending = &bootstrapSuccessorMemberPending{Name: name, Stage: active.stageName(name, "reserved"), Size: int64(len(payload)), Sha256: safeReleaseHash(payload), Payload: base64.StdEncoding.EncodeToString(payload)}
	raw, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("synthetic outer census lost acknowledgement")
	reached := false
	stop := func() error { reached = true; return cause }
	hooks := durablehead.PublicationHooks{}
	switch boundary {
	case "file-sync":
		hooks.AfterFileSync = stop
	case "exchange":
		hooks.AfterRename = stop
	case "directory-sync":
		hooks.AfterDirectorySync = func(*os.File) error { return stop() }
	default:
		t.Fatal("unknown synthetic outer-head boundary", boundary)
	}
	if err := active.members.head.PublishWithHooks(raw, hooks); !reached || !errors.Is(err, cause) || !errors.Is(err, durablehead.ErrUncertain) {
		t.Fatal("real pending census was not retained", boundary, reached, err)
	}
	if err := active.close(); err != nil {
		t.Fatal(err)
	}
	storage := storageSnapshotRestoreTarget(t, source, ctx, profile, false)
	return &storageMemberRestoreFixture{storage: storage, registry: registry, spec: spec, original: root, claim: claim, name: name, raw: payload, stop: boundary}
}

// Exact metadata is retained outside the target and each reviewed staging
// inode is transferred. The actual writer then reconciles only its old head.
func storageMemberOuterControl(t *testing.T, boundary string) {
	t.Helper()
	for _, registry := range []bool{false, true} {
		f := newStorageMemberOuterFixture(t, registry, boundary)
		before := bootstrapSuccessorPreparationTestFiles(t, f.storage.heldSource)
		path, hash := storagePreparationFreezeOwnerPlan(t, f.storage.target, "storage-prepare")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var plan durablevolume.PreparationPlan
		if err := json.Unmarshal(raw, &plan); err != nil || len(plan.Derivations) != 2 {
			t.Fatal("public outer-head restore lost either original metadata image", err)
		}
		for _, derivation := range plan.Derivations {
			original, err := os.ReadFile(derivation.Original.Path)
			if err != nil || string(original) != before[derivation.Original.File.Path] {
				t.Fatal("outer-head plan lost original metadata bytes", err)
			}
		}
		ctx := storageMemberRestoreApply(t, f, path, hash)
		for _, source := range plan.Sources {
			var physical unix.Stat_t
			if err := unix.Stat(filepath.Join(f.storage.target.root, source.File.Path), &physical); err != nil || physical.Ino != source.Identity.Inode || physical.Dev != source.Identity.Device {
				t.Fatal("paired metadata did not transfer exact reviewed inode", source.File.Path, err)
			}
		}
		root, err := bootstrapSuccessorPhysicalRoot(f.storage.target.root)
		if err != nil {
			t.Fatal(err)
		}
		active := openStorageMemberRestoreTestOwner(t, ctx, f.storage.target.root, root, f.claim, registry, nil)
		if active.members.census.Pending == nil || active.members.census.Pending.Payload != base64.StdEncoding.EncodeToString(f.raw) || len(active.members.census.Members) != 1 {
			t.Fatal("actual writer lost original pending member while reconciling outer head")
		}
		if err := active.publish(f.name, "reserved", f.raw); err != nil {
			t.Fatal("exact original pending member cannot finish", err)
		}
		if err := active.close(); err != nil {
			t.Fatal(err)
		}
		active = openStorageMemberRestoreTestOwner(t, ctx, f.storage.target.root, root, f.claim, registry, nil)
		if active.members.census.Pending != nil || len(active.members.census.Members) != 2 {
			t.Fatal("reopened restored owner lost completed original census")
		}
		if err := active.close(); err != nil {
			t.Fatal(err)
		}
		if !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.storage.heldSource)) {
			t.Fatal("restore changed original held custody")
		}
		retained, err := os.ReadFile(filepath.Join(f.storage.target.root, f.name))
		if err != nil || !bytes.Equal(retained, f.raw) {
			t.Fatal("restored continuation changed original pending signed bytes", err)
		}
	}
}

func TestStoragePreparationRestoresPendingMemberHeadBeforeExchange(t *testing.T) {
	storageMemberOuterControl(t, "file-sync")
}

func TestStoragePreparationRestoresPendingMemberHeadAfterExchange(t *testing.T) {
	storageMemberOuterControl(t, "exchange")
}

func TestStoragePreparationRestoresPendingMemberHeadAfterDirectorySync(t *testing.T) {
	storageMemberOuterControl(t, "directory-sync")
}

// A newly pinned malformed export remains malformed; updating its outer hash
// cannot turn partial next bytes into a complete original snapshot image.
func TestStoragePreparationOuterMemberHeadRefusesPartialImage(t *testing.T) {
	f := newStorageMemberOuterFixture(t, true, "file-sync")
	before := bootstrapSuccessorPreparationTestFiles(t, f.storage.heldSource)
	raw, err := os.ReadFile(f.storage.target.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	report, err := durablevolume.LoadPhysicalInventory(t.Context(), request.RestoreSource.Inventory)
	if err != nil {
		t.Fatal(err)
	}
	var head durablehead.Checkpoint
	for _, attribute := range report.Entries[0].OwnerAttributes {
		if attribute.Name == durablehead.Attribute(f.spec.Kind, f.spec.Name) {
			if err := json.Unmarshal(attribute.Value, &head); err != nil {
				t.Fatal(err)
			}
		}
	}
	if head.Pending == nil {
		t.Fatal("fixture lost original pending head")
	}
	path := filepath.Join(f.storage.archive, head.Pending.Temporary)
	raw, err = os.ReadFile(path)
	if err != nil || len(raw) < 2 {
		t.Fatal(err)
	}
	raw = raw[:len(raw)-1]
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for index := range report.Entries {
		if report.Entries[index].Path == head.Pending.Temporary {
			report.Entries[index].Size = uint64(len(raw))
			report.Entries[index].Sha256 = safeReleaseHash(raw)
		}
	}
	raw, err = json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.RestoreSource.Inventory.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	request.RestoreSource.Inventory.Sha256 = safeReleaseHash(raw)
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.storage.target.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.storage.target.requestHash = safeReleaseHash(raw)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.storage.target.ctx, []string{"storage-prepare", "plan", "--request", f.storage.target.requestPath, "--request-sha256", f.storage.target.requestHash}, &output, &diagnostic); code == 0 || output.Len() != 0 {
		t.Fatal("partial original outer-head image received an accepted plan", code, diagnostic.String())
	}
	names, err := os.ReadDir(f.storage.target.root)
	if err != nil || len(names) != 0 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.storage.heldSource)) {
		t.Fatal("partial image refusal changed target or original custody", err)
	}
}

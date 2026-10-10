//go:build linux || darwin

// Public preparation transfers reviewed staging inodes and opens the actual
// member owner. Signed execution-root authority is a separate control below;
// a storage result is never permission to rewrite the old approval.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

type storageMemberRestoreFixture struct {
	storage  *storageSnapshotRestoreFixture
	registry bool
	spec     durablehead.Spec
	original bootstrapSuccessorRootIdentity
	claim    string
	name     string
	raw      []byte
	stop     string
}

// The nonce case uses its actual constructor. Local member custody borrows
// the same guarded/flocked directory as its enclosing preparation owner.
func openStorageMemberRestoreTestOwner(t *testing.T, ctx context.Context, path string, root bootstrapSuccessorRootIdentity, claim string, registry bool, hook func(string) error) *bootstrapSuccessorExecutionDirectory {
	t.Helper()
	if registry {
		owner, err := openBootstrapSuccessorExecutionDirectory(ctx, path, root, claim, hook)
		if err != nil {
			t.Fatal(err)
		}
		return owner
	}
	storage, err := openMainnetDurableDirectory(ctx, path, durablevolume.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), path)
	if err := mainnetDurableFlock(fd, unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	members, err := openBootstrapSuccessorMembers(storage, file, false, false)
	if err != nil {
		t.Fatal(err)
	}
	return &bootstrapSuccessorExecutionDirectory{storage: storage, members: members, ctx: ctx, path: path, root: root, file: file, claim: claim, hook: hook}
}

// Every original census/head comes from real publication. Stops follow an
// explicit completed reservation, stage acknowledgement or no-replace rename.
func newStorageMemberRestoreFixture(t *testing.T, registry bool, stop string) *storageMemberRestoreFixture {
	t.Helper()
	source := newStoragePreparationCommandFixture(t)
	spec := bootstrapSuccessorMemberSpec(registry)
	owner := storagePreparationDirectoryOwner(t, spec.Kind)
	storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{owner})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	root, err := bootstrapSuccessorPhysicalRoot(source.root)
	if err != nil {
		t.Fatal(err)
	}
	claim := safeReleaseHash([]byte("synthetic original claimant"))
	name := bootstrapSuccessorExecutionPrefix + "synthetic-retained.json"
	if registry {
		name = "relayer-outer-synthetic-retained.json"
	}
	raw := []byte(`{"nonce":17,"signature":"synthetic original signed payload bytes"}`)
	active := openStorageMemberRestoreTestOwner(t, ctx, source.root, root, claim, registry, nil)
	if err := active.publish(name, "reserved", raw); err != nil {
		t.Fatal(err)
	}
	if stop != "" {
		name += ".next"
		raw = []byte(`{"nonce":18,"signature":"synthetic retained pending payload bytes"}`)
		forced := errors.New("synthetic lost acknowledgement after original member boundary")
		called := false
		active.hook = func(stage string) error {
			if stage == name+":"+stop {
				called = true
				return forced
			}
			return nil
		}
		if err := active.publish(name, "reserved", raw); !called || !errors.Is(err, forced) {
			t.Fatal("source did not reach the forced original pending boundary", stop, called, err)
		}
	}
	if err := active.close(); err != nil {
		t.Fatal(err)
	}
	storage := storageSnapshotRestoreTarget(t, source, ctx, owner, false)
	return &storageMemberRestoreFixture{storage: storage, registry: registry, spec: spec, original: root, claim: claim, name: name, raw: raw, stop: stop}
}

func storageMemberRestorePlan(t *testing.T, f *storageMemberRestoreFixture) (durablevolume.PreparationPlan, string, string) {
	t.Helper()
	path, hash := storagePreparationFreezeOwnerPlan(t, f.storage.target, "storage-prepare")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan durablevolume.PreparationPlan
	if err := json.Unmarshal(raw, &plan); err != nil || len(plan.Derivations) != 1 {
		t.Fatal("public member plan lost original and derived census lineage", err)
	}
	return plan, path, hash
}

func storageMemberRestoreApply(t *testing.T, f *storageMemberRestoreFixture, path, hash string) context.Context {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(f.storage.target.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public member restore apply failed", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("member storage preparation gained execution authority", err)
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), f.storage.target.storage.Host)
}

// Exact unsigned inode derivation does not allow any other byte change.
func assertStorageMemberRestoreBytes(t *testing.T, f *storageMemberRestoreFixture, plan durablevolume.PreparationPlan) {
	t.Helper()
	original, err := os.ReadFile(filepath.Join(f.storage.heldSource, f.spec.Name))
	if err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(plan.Derivations[0].Original.Path)
	if err != nil || !bytes.Equal(retained, original) || plan.Derivations[0].Original.File.Sha256 != safeReleaseHash(original) {
		t.Fatal("original census was not retained exactly", err)
	}
	for _, source := range plan.Sources {
		var stat unix.Stat_t
		path := filepath.Join(f.storage.target.root, source.File.Path)
		if err := unix.Stat(path, &stat); err != nil || stat.Ino != source.Identity.Inode || durablesys.StatDevice(&stat) != source.Identity.Device {
			t.Fatal("target did not receive the exact reviewed staging inode", source.File.Path, err)
		}
		if _, err := os.Lstat(source.Path); !os.IsNotExist(err) {
			t.Fatal("transferred member retained a source alias", source.File.Path, err)
		}
		raw, err := os.ReadFile(path)
		if err != nil || safeReleaseHash(raw) != source.File.Sha256 {
			t.Fatal("target differs from the reviewed transferred bytes", err)
		}
		if source.File.Path != f.spec.Name {
			before, err := os.ReadFile(filepath.Join(f.storage.heldSource, source.File.Path))
			if err != nil || !bytes.Equal(before, raw) {
				t.Fatal("restore rewrote original signed member or stage bytes", source.File.Path, err)
			}
		}
	}
}

// Public plan/apply restores both fixed census kinds. The actual owner admits
// the transferred members without discarding history or creating a new claim.
func TestStoragePreparationRestoreMemberCensusOpensOriginalHistory(t *testing.T) {
	for _, registry := range []bool{false, true} {
		f := newStorageMemberRestoreFixture(t, registry, "")
		plan, path, hash := storageMemberRestorePlan(t, f)
		ctx := storageMemberRestoreApply(t, f, path, hash)
		assertStorageMemberRestoreBytes(t, f, plan)
		root, err := bootstrapSuccessorPhysicalRoot(f.storage.target.root)
		if err != nil {
			t.Fatal(err)
		}
		active := openStorageMemberRestoreTestOwner(t, ctx, f.storage.target.root, root, f.claim, registry, nil)
		before, err := os.ReadFile(filepath.Join(f.storage.target.root, f.spec.Name))
		if err != nil || len(active.members.census.Members) != 1 || active.members.census.Pending != nil {
			t.Fatal("restored owner lost acknowledged history", err)
		}
		if err := errors.Join(active.checkpoint("restored-inspection"), active.close()); err != nil {
			t.Fatal("actual member owner rejected restored history", err)
		}
		after, err := os.ReadFile(filepath.Join(f.storage.target.root, f.spec.Name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("readmission changed retained progress", err)
		}
		storageMemberRestoreApply(t, f, path, hash)
	}
}

// A pending reservation stays pending after storage apply. Only its unchanged
// original claimant/payload can complete it through the real publisher.
func TestStoragePreparationRestoreMemberPendingKeepsOriginalPayload(t *testing.T) {
	for _, stop := range []string{"reserved", "name-synced", "stage-synced", "published"} {
		f := newStorageMemberRestoreFixture(t, true, stop)
		plan, path, hash := storageMemberRestorePlan(t, f)
		ctx := storageMemberRestoreApply(t, f, path, hash)
		assertStorageMemberRestoreBytes(t, f, plan)
		root, err := bootstrapSuccessorPhysicalRoot(f.storage.target.root)
		if err != nil {
			t.Fatal(err)
		}
		active := openStorageMemberRestoreTestOwner(t, ctx, f.storage.target.root, root, f.claim, true, nil)
		if pending := active.members.census.Pending; pending == nil || pending.Name != f.name || pending.Sha256 != safeReleaseHash(f.raw) {
			t.Fatal("storage restore acknowledged or changed the pending application action", stop)
		}
		if err := active.publish(f.name, "reserved", f.raw); err != nil {
			t.Fatal("original pending member cannot resume", stop, err)
		}
		if active.members.census.Pending != nil || len(active.members.census.Members) != 2 {
			t.Fatal("original completion lost retained history", stop)
		}
		if err := active.close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Cancellation occurs after the real no-replace transfer but before its
// acknowledgement. Reopening the same plan must retain and finish that inode.
func TestStoragePreparationRestoreMemberMoveResumesExactInode(t *testing.T) {
	f := newStorageMemberRestoreFixture(t, true, "")
	plan, path, hash := storageMemberRestorePlan(t, f)
	ctx, cancel := context.WithCancel(f.storage.target.ctx)
	called := false
	host := &storagePreparationObservedHost{Host: f.storage.target.storage.Host, observe: func(*os.File) {
		if called {
			return
		}
		var target unix.Stat_t
		if err := unix.Stat(filepath.Join(f.storage.target.root, f.name), &target); err != nil {
			return
		}
		for _, source := range plan.Sources {
			if source.File.Path == f.name && target.Ino == source.Identity.Inode {
				called = true
				cancel()
			}
		}
	}}
	var output, diagnostic bytes.Buffer
	code := runMain(durablepath.WithHost(ctx, host), []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic)
	cancel()
	if !called || code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrPreparationUncertain.Error()) {
		t.Fatal("public transferred-member uncertainty was not retained", called, code, diagnostic.String())
	}
	before, err := os.ReadFile(filepath.Join(f.storage.target.metadata, "preparation.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	storageMemberRestoreApply(t, f, path, hash)
	assertStorageMemberRestoreBytes(t, f, plan)
	after, err := os.ReadFile(filepath.Join(f.storage.target.metadata, "preparation.jsonl"))
	if err != nil || !bytes.HasPrefix(after, before) {
		t.Fatal("joined restore discarded original transfer progress", err)
	}
}

// A changed or omitted staged member cannot publish a plausible new owner
// checkpoint. The reviewed original bytes remain available independently.
func TestStoragePreparationRestoreMemberRejectsChangedStageBeforeHead(t *testing.T) {
	for _, change := range []string{"missing", "replacement", "payload"} {
		f := newStorageMemberRestoreFixture(t, true, "")
		plan, path, hash := storageMemberRestorePlan(t, f)
		var source durablevolume.PreparationSource
		for _, candidate := range plan.Sources {
			if candidate.File.Path == f.name {
				source = candidate
			}
		}
		if source.Path == "" {
			t.Fatal("test lost its original staged member")
		}
		switch change {
		case "missing", "replacement":
			if err := os.Rename(source.Path, source.Path+".held"); err != nil {
				t.Fatal(err)
			}
			if change == "replacement" {
				if err := os.WriteFile(source.Path, f.raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
		case "payload":
			if err := os.WriteFile(source.Path, bytes.Repeat([]byte{'x'}, len(f.raw)), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var output, diagnostic bytes.Buffer
		if code := runMain(f.storage.target.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("changed staged member was admitted", change, code, diagnostic.String())
		}
		if _, err := unix.Getxattr(f.storage.target.root, durablehead.Attribute(f.spec.Kind, f.spec.Name), nil); !errors.Is(err, durablesys.ErrNoAttribute) {
			t.Fatal("refused staged member published target custody head", change, err)
		}
		retained, err := os.ReadFile(filepath.Join(f.storage.heldSource, f.name))
		if err != nil || !bytes.Equal(retained, f.raw) {
			t.Fatal("refused restore changed original signed member", change, err)
		}
	}
}

// The storage layer's physical rebind does not rewrite a separately signed
// root approval. The later explicit approval-rebind workflow must bridge it.
func TestStoragePreparationRestoreMemberRetainsOriginalExecutionRootGate(t *testing.T) {
	f := newStorageMemberRestoreFixture(t, true, "")
	_, path, hash := storageMemberRestorePlan(t, f)
	ctx := storageMemberRestoreApply(t, f, path, hash)
	before, err := os.ReadFile(filepath.Join(f.storage.target.root, f.spec.Name))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := openBootstrapSuccessorExecutionDirectory(ctx, f.storage.target.root, f.original, f.claim, nil)
	if owner != nil || err == nil || !strings.Contains(err.Error(), "physical directory changed") {
		t.Fatal("storage rebind silently rewrote signed original root authority", err)
	}
	after, err := os.ReadFile(filepath.Join(f.storage.target.root, f.spec.Name))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused old root changed original census", err)
	}
}

// Changing the derived census and rehashing a plan is not permission to
// rewrite the old signed member hash, claimant or pending payload.
func TestStoragePreparationRestoreMemberRejectsDerivedAuthorityChange(t *testing.T) {
	f := newStorageMemberRestoreFixture(t, true, "reserved")
	plan, path, _ := storageMemberRestorePlan(t, f)
	var metadata *durablevolume.PreparationSource
	for index := range plan.Sources {
		if plan.Sources[index].File.Path == f.spec.Name {
			metadata = &plan.Sources[index]
		}
	}
	if metadata == nil {
		t.Fatal("test lost derived metadata")
	}
	raw, err := os.ReadFile(metadata.Path)
	if err != nil {
		t.Fatal(err)
	}
	var census bootstrapSuccessorMemberCensus
	if err := json.Unmarshal(raw, &census); err != nil || census.Pending == nil {
		t.Fatal(err)
	}
	census.Pending.Name += ".other"
	raw, err = json.Marshal(census)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	metadata.File.Bytes, metadata.File.Sha256 = uint64(len(raw)), safeReleaseHash(raw)
	plan.Derivations[0].Derived = metadata.File
	for index := range plan.Owners[0].Files {
		if plan.Owners[0].Files[index].Path == f.spec.Name {
			plan.Owners[0].Files[index] = metadata.File
		}
	}
	raw, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	if code := runMain(f.storage.target.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", durablefixture.Digest(raw)}, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "derivation no longer matches") {
		t.Fatal("reviewed plan changed authority beyond physical metadata", code, diagnostic.String())
	}
	if _, err := os.Lstat(filepath.Join(f.storage.target.metadata, "preparation.jsonl")); !os.IsNotExist(err) {
		t.Fatal("invalid lineage began target publication", err)
	}
}

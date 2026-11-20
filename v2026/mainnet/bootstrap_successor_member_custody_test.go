// A retained directory generation does not imply survival of its acknowledged
// members. These controls join real owners before changing only named members.
package main

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Deleting every completed preparation member cannot turn the same physical
// root into an unused preparation or renew the original approval's custody.
func TestBootstrapSuccessorPreparationRetainsCompletedMemberCensus(t *testing.T) {
	approval, _, storage := newBootstrapSuccessorPreparationStorageTestApproval(t)
	owner, err := openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	root := approval.Plan.Proposal.OriginalRunDirectory
	for _, name := range []string{bootstrapSuccessorPreparationFile, bootstrapSuccessorPreparationFile + ".lock"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	before := bootstrapSuccessorPreparationTestFiles(t, root)
	owner, err = openBootstrapSuccessorPreparationStore(storage.Context, approval.Plan, approval, true, nil)
	if owner != nil {
		_ = owner.close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) {
		t.Fatal("completed preparation member loss became a fresh claim", err)
	}
}

// The completed execution namespace has a distinct lifetime from preparation.
// Losing all of it cannot lower the counted attempt floor on a new claim.
func TestBootstrapSuccessorExecutionRetainsCompletedMemberCensus(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil || owner.last.CumulativeAttempts != 9 || len(f.writes) != 1 {
		t.Fatal("original counted execution was not retained", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	root := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	for name := range bootstrapSuccessorPreparationTestFiles(t, root) {
		if strings.HasPrefix(name, bootstrapSuccessorExecutionPrefix) || strings.HasPrefix(name, bootstrapSuccessorExecutionStagePrefix) {
			if err := os.Remove(filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	before := bootstrapSuccessorPreparationTestFiles(t, root)
	nonces := bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)
	owner, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, true, nil)
	if owner != nil {
		_ = owner.close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, f.approval.Plan.Request.RegistryDirectory)) || len(f.writes) != 1 {
		t.Fatal("completed execution member loss renewed original attempt allowance", err)
	}
}

// A valid prefix is insufficient after its later counted intent and event both
// disappear. Restart must not reinterpret that old prefix as the complete head.
func TestBootstrapSuccessorExecutionRetainsCountedTailCensus(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil || owner.last.CumulativeAttempts != 9 || len(f.writes) != 1 {
		t.Fatal("original counted execution was not retained", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	root := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	for _, suffix := range []string{".intent", ".json"} {
		if err := os.Remove(filepath.Join(root, bootstrapSuccessorExecutionEventName(1)+suffix)); err != nil {
			t.Fatal(err)
		}
	}
	before := bootstrapSuccessorPreparationTestFiles(t, root)
	owner, err := openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, false, nil)
	if owner != nil {
		_ = owner.close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) || len(f.writes) != 1 {
		t.Fatal("lost counted tail became an earlier valid execution prefix", err)
	}
}

// Each global nonce domain survives its original claimant joining. Removing
// its file cannot let another approved local root acquire the consumed nonce.
func TestBootstrapSuccessorNonceRegistryRetainsMemberCensus(t *testing.T) {
	for _, competing := range []struct {
		safe  string
		outer uint64
		lost  int
	}{{safe: "17", outer: 43, lost: 0}, {safe: "18", outer: 42, lost: 1}} {
		first := newBootstrapSuccessorExecutionFixture(t)
		owner := first.open(true, nil)
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
		registry := first.approval.Plan.Request.RegistryDirectory
		if err := os.Remove(filepath.Join(registry, first.approval.Plan.nonceNames()[competing.lost])); err != nil {
			t.Fatal(err)
		}
		second := newBootstrapSuccessorExecutionNonceFixture(t, competing.safe, competing.outer)
		second.approval.Plan.Request.RegistryDirectory, second.approval.Plan.Registry = registry, first.approval.Plan.Registry
		root := second.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
		second.storage = durablefixture.New(t, t.Context(), root, registry)
		second.approval = bootstrapSuccessorExecutionTestSign(t, second.approval.Plan, second.key, second.profile)
		before, nonces := bootstrapSuccessorPreparationTestFiles(t, root), bootstrapSuccessorPreparationTestFiles(t, registry)
		owner, err := openBootstrapSuccessorExecutionStore(second.storageContext(t.Context()), second.approval.Plan, second.approval, second.profile, true, nil)
		if owner != nil {
			_ = owner.close()
		}
		if !errors.Is(err, durablevolume.ErrIdentity) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, registry)) || len(second.writes) != 0 {
			t.Fatal("missing global nonce member became new claimant authority", competing.lost, err)
		}
	}
}

// Replacing an acknowledged event with byte-identical content still loses its
// physical custody. The directory and every signed byte deliberately survive.
func TestBootstrapSuccessorExecutionRetainsMemberInode(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	root := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
	path := filepath.Join(root, bootstrapSuccessorExecutionEventName(0)+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(t.TempDir(), "synthetic-retained-original-event")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	next, err := os.Stat(path)
	if err != nil || os.SameFile(prior, next) {
		t.Fatal("member replacement control retained original inode", err)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, root)
	owner, err = openBootstrapSuccessorExecutionStore(f.storageContext(t.Context()), f.approval.Plan, f.approval, f.profile, false, nil)
	if owner != nil {
		_ = owner.close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, root)) || len(f.writes) != 0 {
		t.Fatal("byte-identical replacement became original member custody", err)
	}
}

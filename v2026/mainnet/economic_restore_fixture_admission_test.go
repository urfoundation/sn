//go:build linux

// These controls run actual public preparation with explicit adverse starting
// modes and exact future roots. Fixture repair never relaxes runtime custody.
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func TestEconomicConservationRestoreProtectsFreshAncestorBeforePreparation(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0770); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(parent)
	if err != nil || before.Mode().Perm() != 0770 {
		t.Fatal("explicit unprotected fresh parent was not created", err)
	}
	fixture := newEconomicConservationRestoreParentFixture(t, parent, 2, 1, nil)
	after, err := os.Stat(parent)
	if err != nil || after.Mode().Perm() != 0700 {
		t.Fatal("fresh ancestor remained unprotected", err)
	}
	plan := fixture.plan(t)
	if plan.Path == "" || !planSha256(plan.Sha256) {
		t.Fatal("public complete-union preparation produced no plan")
	}
	fixture.unchanged(t)
}

func TestNativeProducerRestorePreparesFreshTargetWithoutPriorGeneration(t *testing.T) {
	parent := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, parent)
	var sources []*storagePreparationCommandFixture
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(parent, name)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, newStoragePreparationCommandFixtureAt(t, path))
	}
	physical := nativeProducerRestoreObservationFixture(t, sources)
	for _, source := range sources {
		if entries, err := os.ReadDir(source.root); err != nil || len(entries) != 0 {
			t.Fatal("fresh target was populated before public plan", err)
		}
		source.storage, source.ctx = physical, physical.Context
		owner := storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "original.json", maxRpcReplyBytes)
		storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{owner})
		ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
		if _, present := durablevolume.ReferenceFromContext(ctx); !present {
			t.Fatal("fresh target did not acquire authority through public preparation")
		}
	}
}

// Native and host action snapshots retain their original allowance across a
// closed process. These fixtures never call a real signer or service manager.
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Removing both original members is distinct from a valid restart. The
// externally provisioned lock is never implicitly re-created by a new claim.
func TestRootActionDurableLostMembersCannotRenewClaim(t *testing.T) {
	action, _, _ := rootActionFixture(t)
	path := action.Scope.StatePath
	prepareMainnetSnapshotTest(t, path, "mainnet-root-action", rootActionStoreLimit)
	fixture := durablefixture.New(t, t.Context(), filepath.Dir(path))
	store, err := openRootActionStore(path, &action, fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	store, err = openRootActionStore(path, nil, fixture.Context)
	if err != nil {
		t.Fatal("valid original restart", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + ".lock"} {
		if err := os.Rename(name, name+".retained"); err != nil {
			t.Fatal(err)
		}
	}
	store, err = openRootActionStore(path, &action, fixture.Context)
	if store != nil {
		_ = store.close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("missing root action members recreated a claim", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused root action changed state", err)
	}
}

// A signed recycle handoff remains the same after close. A valid older
// reserved JSON file cannot turn that completed handoff back into unused state.
func TestOwnerRecycleDurableSignedRollbackRefusedAfterClose(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	path := f.config.Action.StatePath
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := custody.importSignature(request.ContentHash, f.signature(t)); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	store, err = openOwnerRecycleStore(f.config, f.key, false, f.storage.Context)
	if err != nil {
		t.Fatal("valid signed restart", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	store, err = openOwnerRecycleStore(f.config, f.key, false, f.storage.Context)
	if store != nil {
		_ = store.close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("earlier valid recycle record renewed custody", err)
	}
	if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, before) {
		t.Fatal("rollback refusal rewrote the action", err)
	}
}

// Both validator roles share one operation ledger. Removing its members must
// not recreate the counter while the signed activation plan still validates.
func TestValidatorActivationDurableLostMembersCannotRenewOperations(t *testing.T) {
	f := newValidatorActivationFixture(t)
	path := f.approval.Plan.StatePath
	prepareMainnetSnapshotTest(t, path, "mainnet-validator-activation", 128*1024)
	fixture := durablefixture.New(t, t.Context(), filepath.Dir(path))
	store, err := openValidatorActivationStore(fixture.Context, f.approval, f.key, true, f.now)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load(fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	record.Operations, record.Status = 1, "operation-reserved"
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	store, err = openValidatorActivationStore(fixture.Context, f.approval, f.key, false, f.now)
	if err != nil {
		t.Fatal("original activation restart", err)
	}
	record, err = store.load(fixture.Context)
	if err != nil || record.Operations != 1 {
		t.Fatal("restart lost the reserved operation", record.Operations, err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + ".lock"} {
		if err := os.Rename(name, name+".retained"); err != nil {
			t.Fatal(err)
		}
	}
	store, err = openValidatorActivationStore(fixture.Context, f.approval, f.key, true, f.now)
	if store != nil {
		_ = store.close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) || f.reloads != 0 || f.starts != [2]int{} {
		t.Fatal("lost activation members renewed operations or reached manager", err, f.reloads, f.starts)
	}
}

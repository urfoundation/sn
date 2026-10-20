// Original native intent remains usable only while its physical custody and
// preceding journal survive. These boundaries use local synthetic signatures.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Invoke an explicit boundary after canonical observation, before publication.
type ownerRecycleReconcileBoundary func(context.Context, ownerRecycleSigningRequest, []byte) (ownerRecycleReconciliation, error)

// The callback never gains signing or submission authority.
func (self ownerRecycleReconcileBoundary) reconcile(ctx context.Context, request ownerRecycleSigningRequest, raw []byte) (ownerRecycleReconciliation, error) {
	return self(ctx, request, raw)
}

// Retain an exact valid marker on another inode, as an atomic path replacement
// would. Equal bytes do not transfer the original owner's flock.
func ownerRecycleReplaceMarker(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".detached"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// A detached local owner cannot hand an otherwise approved request to a signer.
func TestOwnerRecycleCustodyRefusesDetachedMarkerBeforeExport(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	ownerRecycleReplaceMarker(t, f.config.Action.StatePath+".lock")
	request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata)
	if !errors.Is(err, durablevolume.ErrIdentity) || request.ContentHash != "" {
		t.Fatal("detached recycle marker released a signing request", err)
	}
}

// A read-only receipt can neither restore deleted original custody nor return
// finalized mode readiness after its signed predecessor disappears mid-read.
func TestOwnerRecycleCustodyRefusesJournalLossDuringReconciliation(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	chain, _, request, _ := ownerRecycleTestChain(t, f, 2, true)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	if _, err := custody.export(f.input.Metadata, f.input.LedgerMetadata); err != nil {
		t.Fatal(err)
	}
	if _, err := custody.importSignature(request.ContentHash, f.signature(t)); err != nil {
		t.Fatal(err)
	}
	boundary := ownerRecycleReconcileBoundary(func(ctx context.Context, request ownerRecycleSigningRequest, raw []byte) (ownerRecycleReconciliation, error) {
		evidence, err := chain.reconcile(ctx, request, raw)
		if err != nil {
			return evidence, err
		}
		if err := os.Remove(f.config.Action.StatePath); err != nil {
			t.Fatal(err)
		}
		return evidence, nil
	})
	result, err := custody.reconcile(t.Context(), boundary)
	if !errors.Is(err, durablevolume.ErrIdentity) || result.TransactionFinalized || result.RecycleModeObserved {
		t.Fatal("lost completed recycle journal became finalized mode readiness", err)
	}
	if _, err := os.Lstat(f.config.Action.StatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reconciliation recreated deleted signed custody", err)
	}
}

// A valid earlier record cannot roll an exported request back to unused
// signing custody merely because its config and checksum still authenticate.
func TestOwnerRecycleCustodyRefusesJournalRollbackDuringOwnership(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	reserved, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := custody.importSignature(request.ContentHash, f.signature(t)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.config.Action.StatePath, reserved, 0600); err != nil {
		t.Fatal(err)
	}
	if replay, err := custody.export(f.input.Metadata, f.input.LedgerMetadata); !errors.Is(err, durablevolume.ErrIdentity) || replay.ContentHash != "" {
		t.Fatal("valid predecessor rollback released a second signing handoff", err)
	}
}

// Successful directory sync is not proof that the named marker is still the
// acquired lock. A restored marker cannot revive an already poisoned instance.
func TestOwnerRecycleCustodyRefusesMarkerLossAfterDurableExport(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	marker := f.config.Action.StatePath + ".lock"
	store.syncDirectory = func(directory *os.File) error {
		ownerRecycleReplaceMarker(t, marker)
		return directory.Sync()
	}
	if request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata); !errors.Is(err, durablevolume.ErrIdentity) || request.ContentHash != "" {
		t.Fatal("post-rename marker loss released a signing request", err)
	}
	if err := os.Rename(marker+".detached", marker); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("restoring marker revived poisoned custody", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := openOwnerRecycleStore(f.config, f.key, false, f.storage.Context)
	if err != nil {
		t.Fatal("original durable export could not reopen", err)
	}
	defer resumed.close()
	recovered := ownerRecycleCustody{config: f.config, key: f.key, store: resumed}
	request, err := recovered.export(f.input.Metadata, f.input.LedgerMetadata)
	if err != nil || request.Config.Action.RequestHash != f.config.Action.RequestHash {
		t.Fatal("reopen lost original exported intent", err)
	}
}

// A private replacement parent with byte-identical files cannot inherit a
// writer already holding the original parent and marker descriptors.
func TestOwnerRecycleCustodyRefusesReplacedParentAfterPublication(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	directory := filepath.Dir(f.config.Action.StatePath)
	detached := directory + ".detached"
	t.Cleanup(func() { os.RemoveAll(detached) })
	store.syncDirectory = func(file *os.File) error {
		if err := os.Rename(directory, detached); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{ownerRecycleStateFile, ownerRecycleStateFile + ".lock"} {
			raw, err := os.ReadFile(filepath.Join(detached, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, name), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		return file.Sync()
	}
	if request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata); !errors.Is(err, durablevolume.ErrIdentity) || request.ContentHash != "" {
		t.Fatal("replacement parent inherited original signing custody", err)
	}
}

// Import recovery, status and a cached finalized result all need the original
// local owner, even though they would return already retained public evidence.
func TestOwnerRecycleCustodyRefusesDetachedMarkerForRetainedResults(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	chain, _, request, _ := ownerRecycleTestChain(t, f, 2, true)
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	if _, err := custody.export(f.input.Metadata, f.input.LedgerMetadata); err != nil {
		t.Fatal(err)
	}
	signature := f.signature(t)
	if _, err := custody.importSignature(request.ContentHash, signature); err != nil {
		t.Fatal(err)
	}
	if result, err := custody.reconcile(t.Context(), chain); err != nil || !result.RecycleModeObserved {
		t.Fatal("original transition did not reconcile", err)
	}
	ownerRecycleReplaceMarker(t, f.config.Action.StatePath+".lock")
	if result, err := custody.reconcile(t.Context(), chain); !errors.Is(err, durablevolume.ErrIdentity) || result.RecycleModeObserved || result.TransactionFinalized {
		t.Fatal("detached terminal owner claimed original receipt readiness", err)
	}
	// Fresh supervisors over the same poisoned store cannot bypass its refusal.
	custody = ownerRecycleCustody{config: f.config, key: f.key, store: store}
	if result, err := custody.importSignature(request.ContentHash, signature); !errors.Is(err, durablevolume.ErrIdentity) || result.RecycleModeObserved {
		t.Fatal("cached signature import bypassed original custody", err)
	}
	if record, err := store.load(); !errors.Is(err, durablevolume.ErrIdentity) || record.ContentHash != "" {
		t.Fatal("status returned a detached terminal record", err)
	}
}

// Alias links can let another name mutate an otherwise private file. Both
// active custody and reopening refuse marker or journal hardlinks.
func TestOwnerRecycleCustodyRefusesHardlinkedStateAndMarker(t *testing.T) {
	for _, suffix := range []string{"", ".lock"} {
		f := newOwnerRecycleTestFixture(t, true)
		store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		path := f.config.Action.StatePath + suffix
		if err := os.Link(path, path+".alias"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.load(); !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("hardlink retained active recycle custody", suffix, err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		if reopened, err := openOwnerRecycleStore(f.config, f.key, false, f.storage.Context); err == nil {
			reopened.close()
			t.Fatal("hardlinked recycle custody reopened", suffix)
		}
	}
}

// Actual pre-completion boundaries preserve the acknowledged physical head.
// Recovery must never be simulated by deleting an already committed journal.
func TestOwnerRecycleCustodyRecoversOnlyUnfinishedReservation(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "progress-synced"} {
		f := newOwnerRecycleTestFixture(t, true)
		path := f.config.Action.StatePath
		originalMarker, err := os.Stat(path + ".lock")
		if err != nil {
			t.Fatal(err)
		}
		interrupted := errors.New("synthetic recycle initial claim interruption")
		store, err := openOwnerRecycleStoreWithClaimHook(f.config, f.key, true, func(observed string) error {
			if observed == boundary {
				return interrupted
			}
			return nil
		}, f.storage.Context)
		if store != nil || !errors.Is(err, interrupted) {
			if store != nil {
				store.close()
			}
			t.Fatal("claim did not stop at its actual durable boundary", boundary, err)
		}
		marker := rootObjectHash(f.config) + "\n" + f.key + "\n"
		raw, err := os.ReadFile(path + ".lock")
		if err != nil || !bytes.Equal(raw, []byte(marker)) {
			t.Fatal("interrupted claim acquired a completion marker", boundary, err)
		}
		_, err = os.Stat(path)
		if boundary == "marker-synced" && !errors.Is(err, os.ErrNotExist) || boundary == "progress-synced" && err != nil {
			t.Fatal("interruption changed its original journal presence", boundary, err)
		}
		// A second interrupted resume must retain the same acknowledged row,
		// without consuming a one-shot permission to complete the claim.
		store, err = openOwnerRecycleStoreWithClaimHook(f.config, f.key, false, func(observed string) error {
			if observed != "progress-synced" {
				t.Fatal("resume reached a fresh-marker boundary", observed)
			}
			return interrupted
		}, f.storage.Context)
		if store != nil || !errors.Is(err, interrupted) {
			if store != nil {
				store.close()
			}
			t.Fatal("unfinished original claim could not reach retained progress", boundary, err)
		}
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		resumed, err := openOwnerRecycleStore(f.config, f.key, false, f.storage.Context)
		if err != nil {
			t.Fatal("unfinished unused claim could not recover", boundary, err)
		}
		record, err := resumed.load()
		if err != nil || record.Phase != "reserved" || record.Request != nil {
			t.Fatal("unfinished claim changed original phase", boundary, err)
		}
		if err := resumed.close(); err != nil {
			t.Fatal(err)
		}
		raw, err = os.ReadFile(path + ".lock")
		if err != nil || !bytes.Equal(raw, []byte(marker+bootstrapRootClaimComplete)) {
			t.Fatal("recovered claim lacks original completion marker", boundary, err)
		}
		current, err := os.ReadFile(path)
		retainedMarker, statErr := os.Stat(path + ".lock")
		if err != nil || statErr != nil || !bytes.Equal(current, original) || !os.SameFile(originalMarker, retainedMarker) {
			t.Fatal("completion replaced original reservation or physical marker", boundary, err, statErr)
		}
	}
}

// Prefix-only application bytes cannot erase a committed physical generation
// or make an exported original request recover as an unused reservation.
func TestOwnerRecycleCompletedClaimCannotBecomeUnfinished(t *testing.T) {
	for _, progress := range []string{"missing", "exported"} {
		f := newOwnerRecycleTestFixture(t, true)
		store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		if progress == "exported" {
			custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
			if _, err := custody.export(f.input.Metadata, f.input.LedgerMetadata); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		path := f.config.Action.StatePath
		if progress == "missing" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		marker := rootObjectHash(f.config) + "\n" + f.key + "\n"
		if err := os.WriteFile(path+".lock", []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
		before := mainnetNamespaceTest(t, filepath.Dir(path))
		resumed, err := openOwnerRecycleStore(f.config, f.key, false, f.storage.Context)
		if resumed != nil {
			resumed.close()
		}
		if err == nil || progress == "missing" && !errors.Is(err, durablevolume.ErrIdentity) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(path))) {
			t.Fatal("completed original custody was recreated or downgraded", progress, err)
		}
	}
}

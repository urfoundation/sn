// Root registration retains its original operator request across real physical
// custody interruptions. All identities and signatures are synthetic test data.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Tests stop at a concrete read-only boundary without acquiring send authority.
type rootRegisterReconcileBoundary func(context.Context, rootRegisterSigningRequest, []byte) (rootRegisterReconciliation, error)

// Only caller-provided public reconciliation evidence escapes this adapter.
func (self rootRegisterReconcileBoundary) reconcile(ctx context.Context, request rootRegisterSigningRequest, raw []byte) (rootRegisterReconciliation, error) {
	return self(ctx, request, raw)
}

// Exercise actual marker, export and signature custody before later transitions.
func rootRegisterSignedTestCustody(t *testing.T, f *rootRegisterTestFixture) (*rootRegisterCustody, *rootRegisterStore, rootRegisterSigningRequest) {
	t.Helper()
	store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.close() })
	custody := &rootRegisterCustody{config: f.config, key: f.key, store: store}
	request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := custody.importSignature(request.ContentHash, f.signature(t)); err != nil {
		t.Fatal(err)
	}
	return custody, store, request
}

// Retain an exact valid marker on another inode, as an atomic path replacement
// would. Equal bytes do not transfer the original owner's flock.
func rootRegisterReplaceMarker(t *testing.T, path string) {
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
func TestRootRegisterCustodyRefusesDetachedMarkerBeforeExport(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := rootRegisterCustody{config: f.config, key: f.key, store: store}
	rootRegisterReplaceMarker(t, f.config.Action.StatePath+".lock")
	request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata)
	if !errors.Is(err, durablevolume.ErrIdentity) || request.ContentHash != "" {
		t.Fatal("detached root registration marker released a signing request", err)
	}
}

// A valid earlier record cannot roll an exported request back to unused
// signing custody merely because its config and checksum still authenticate.
func TestRootRegisterCustodyRefusesJournalRollbackDuringOwnership(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := rootRegisterCustody{config: f.config, key: f.key, store: store}
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
func TestRootRegisterCustodyRefusesMarkerLossAfterDurableExport(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := rootRegisterCustody{config: f.config, key: f.key, store: store}
	marker := f.config.Action.StatePath + ".lock"
	store.syncDirectory = func(directory *os.File) error {
		rootRegisterReplaceMarker(t, marker)
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
	resumed, err := openRootRegisterStore(f.config, f.key, false, f.storage.Context)
	if err != nil {
		t.Fatal("original durable export could not reopen", err)
	}
	defer resumed.close()
	recovered := rootRegisterCustody{config: f.config, key: f.key, store: resumed}
	request, err := recovered.export(f.input.Metadata, f.input.LedgerMetadata)
	if err != nil || request.Config.Action.RequestHash != f.config.Action.RequestHash {
		t.Fatal("reopen lost original exported intent", err)
	}
}

// Alias links can let another name mutate an otherwise private file. Both
// active custody and reopening refuse marker or journal hardlinks.
func TestRootRegisterCustodyRefusesHardlinkedStateAndMarker(t *testing.T) {
	for _, suffix := range []string{"", ".lock"} {
		f := newRootRegisterTestFixture(t, true)
		store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		path := f.config.Action.StatePath + suffix
		if err := os.Link(path, path+".alias"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.load(); !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("hardlink retained active root registration custody", suffix, err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		if reopened, err := openRootRegisterStore(f.config, f.key, false, f.storage.Context); err == nil {
			reopened.close()
			t.Fatal("hardlinked root registration custody reopened", suffix)
		}
	}
}

// Actual pre-completion boundaries preserve the acknowledged physical head.
// Recovery must never be simulated by deleting an already committed journal.
func TestRootRegisterCustodyRecoversOnlyUnfinishedReservation(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "progress-synced"} {
		f := newRootRegisterTestFixture(t, true)
		path := f.config.Action.StatePath
		originalMarker, err := os.Stat(path + ".lock")
		if err != nil {
			t.Fatal(err)
		}
		interrupted := errors.New("synthetic root registration initial claim interruption")
		store, err := openRootRegisterStoreWithClaimHook(f.config, f.key, true, func(observed string) error {
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
		store, err = openRootRegisterStoreWithClaimHook(f.config, f.key, false, func(observed string) error {
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
		resumed, err := openRootRegisterStore(f.config, f.key, false, f.storage.Context)
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
func TestRootRegisterCompletedClaimCannotBecomeUnfinished(t *testing.T) {
	for _, progress := range []string{"missing", "exported"} {
		f := newRootRegisterTestFixture(t, true)
		store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		if progress == "exported" {
			custody := rootRegisterCustody{config: f.config, key: f.key, store: store}
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
		resumed, err := openRootRegisterStore(f.config, f.key, false, f.storage.Context)
		if resumed != nil {
			resumed.close()
		}
		if err == nil || progress == "missing" && !errors.Is(err, durablevolume.ErrIdentity) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(path))) {
			t.Fatal("completed original custody was recreated or downgraded", progress, err)
		}
	}
}

// Real filesystem custody forces failure after durable rename, then resumes the
// exact export/signature and refuses replacement or disappearance of signed state.
func TestRootRegisterDurableExportImportRecovery(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := rootRegisterCustody{config: f.config, key: f.key, store: store}
	if _, err := openRootRegisterStore(f.config, f.key, false, f.storage.Context); err == nil {
		t.Fatal("second local owner admitted")
	}
	store.syncDirectory = func(*os.File) error { return errors.New("synthetic post-rename sync failure") }
	if _, err := custody.export(f.input.Metadata, ""); err == nil {
		t.Fatal("export acknowledged ambiguous durability")
	}
	if _, err := custody.export(f.input.Metadata, ""); err == nil {
		t.Fatal("poisoned export continued")
	}
	store.close()
	store, err = openRootRegisterStore(f.config, f.key, false, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody = rootRegisterCustody{config: f.config, key: f.key, store: store}
	request, err := custody.export(f.input.Metadata, "")
	if err != nil {
		t.Fatal(err)
	}
	signature := f.signature(t)
	store.syncDirectory = func(*os.File) error { return errors.New("synthetic lost import acknowledgement") }
	if _, err := custody.importSignature(request.ContentHash, signature); err == nil {
		t.Fatal("import acknowledged ambiguous durability")
	}
	store.close()
	store, err = openRootRegisterStore(f.config, f.key, false, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody = rootRegisterCustody{config: f.config, key: f.key, store: store}
	result, err := custody.importSignature(request.ContentHash, signature)
	if err != nil || result.Phase != "signed" {
		t.Fatal("lost import did not recover", err)
	}
	record, err := custody.load()
	if err != nil {
		t.Fatal(err)
	}
	original := record.RawExtrinsic
	if _, err := custody.importSignature(request.ContentHash, f.signature(t)); err == nil {
		t.Fatal("replacement randomized signature admitted")
	}
	if _, err := custody.export(f.input.Metadata, ""); err == nil {
		t.Fatal("signed custody re-exported for signing")
	}
	record, err = custody.load()
	if err != nil || record.RawExtrinsic != original {
		t.Fatal("original bytes changed", err)
	}
	store.close()
	if err := os.Remove(f.config.Action.StatePath); err != nil {
		t.Fatal(err)
	}
	if _, err := openRootRegisterStore(f.config, f.key, false, f.storage.Context); err == nil {
		t.Fatal("lost completed state became fresh custody")
	}
}

// Missing public signature output remains unresolved even after a caller asks
// for receipt recovery. Neither expiry nor another request is inferred locally.
func TestRootRegisterExportedRequestCannotBecomeUnsignedExpiry(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	custody := rootRegisterCustody{config: f.config, key: f.key, store: store}
	request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	chain := rootRegisterReconcileBoundary(func(context.Context, rootRegisterSigningRequest, []byte) (rootRegisterReconciliation, error) {
		reads++
		return rootRegisterReconciliation{}, nil
	})
	result, err := custody.reconcile(t.Context(), chain)
	if err == nil || reads != 0 || result.Phase != "exported" || result.TransactionFinalized || result.RootSeatObserved || result.ActivationReady {
		t.Fatal("unreturned original request entered unsigned recovery", result, reads, err)
	}
	retained, err := custody.load()
	if err != nil || retained.Request.ContentHash != request.ContentHash || retained.Phase != "exported" || retained.Signature != "" {
		t.Fatal("unreturned original request changed during refused recovery", err)
	}
}

// Even a newly valid approval cannot erase consumed post reservations through
// the custody or raw store API. Both must compare their actual durable predecessor.
func TestRootRegisterSubmissionContinuationCannotResetAllowance(t *testing.T) {
	for _, access := range []string{"custody", "store"} {
		for _, mutation := range []string{"erase", "decrease", "replace"} {
			f := newRootRegisterTestFixture(t, true)
			custody, store, _ := rootRegisterSignedTestCustody(t, f)
			record, err := custody.load()
			if err != nil {
				t.Fatal(err)
			}
			approval, err := rootRegisterSubmissionTemplate(record)
			if err != nil {
				t.Fatal(err)
			}
			approval.AuthorityHash = rootObjectHash("synthetic independently reviewed root registration authority")
			approval.Signature = hex.EncodeToString(ed25519.Sign(f.approval, approval.signingBytes()))
			record.Submission = &rootRegisterSubmissionRecord{Approval: approval, ApprovalKey: f.key, Attempts: 1}
			if err := custody.persist(record); err != nil {
				t.Fatal(err)
			}
			prior, err := os.ReadFile(f.config.Action.StatePath)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "erase":
				record.Submission = nil
			case "decrease":
				record.Submission.Attempts = 0
			case "replace":
				record.Submission.Approval.MaximumAttempts = 2
				record.Submission.Approval.Signature = hex.EncodeToString(ed25519.Sign(f.approval, record.Submission.Approval.signingBytes()))
			}
			record.ContentHash = ""
			record.ContentHash = rootObjectHash(record)
			if err := record.validate(f.config, f.key); err != nil {
				t.Fatal("candidate should be independently valid but an invalid successor", mutation, err)
			}
			if access == "custody" {
				err = custody.persist(record)
			} else {
				err = store.save(record)
			}
			if err == nil {
				t.Fatal("consumed root registration allowance was reset", access, mutation)
			}
			retained, err := os.ReadFile(f.config.Action.StatePath)
			if err != nil || !bytes.Equal(prior, retained) {
				t.Fatal("refused root registration successor replaced original journal", access, mutation, err)
			}
		}
	}
}

// Original native bytes and the numbered attempt stay under one physical owner.
// Deterministic callbacks replace custody at the actual effect boundaries.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The authority boundary is synchronous, so no scheduler timing selects a fault.
type ownerTrimCustodyTestAuthority struct{ before func() }

func (self *ownerTrimCustodyTestAuthority) authorize(context.Context, ownerTrimExecutionConfig, ownerTrimActionReconciliation) error {
	self.before()
	return nil
}

// The real sixth journal retains an imported synthetic signature; the execution
// owner has no signing port and can only send those original bytes.
func newOwnerTrimCustodyTestOwner(t *testing.T) (*ownerTrimExecutor, *ownerTrimStore, *ownerTrimTestChain, []byte) {
	t.Helper()
	template := newOwnerTrimActionTestFixture(t)
	preparation, f := ownerTrimPreparedTestFixture(t, template.pair.Public())
	store, err := openOwnerTrimStore(f.storage.Context, preparation.preparation, f.config, f.key, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.close() })
	original, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := hex.DecodeString(f.config.Action.Payload[2:])
	signature, err := f.pair.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.config.Action.signed(signature)
	if err != nil {
		t.Fatal(err)
	}
	record.Phase, record.Signature = "signed", hex.EncodeToString(signature)
	record.RawExtrinsic, record.ExtrinsicHash = "0x"+hex.EncodeToString(raw), rootExtrinsicHash(raw)
	owner, _, _, chain := ownerTrimTestOwner(t, f)
	owner.store, owner.signer = store, nil
	if err := owner.persist(record); err != nil {
		t.Fatal(err)
	}
	return owner, store, chain, original
}

// Identical marker bytes do not retain the inode carrying the exclusive flock.
func TestOwnerTrimExclusiveMarkerReplacementRefusesSend(t *testing.T) {
	owner, store, chain, _ := newOwnerTrimCustodyTestOwner(t)
	var restore func()
	owner.authority = &ownerTrimCustodyTestAuthority{before: func() {
		restore = bootstrapReadinessTestReplace(t, store.config.Action.StatePath+".lock")
	}}
	_, err := owner.step(t.Context())
	if restore == nil {
		t.Fatal("authority boundary was not reached", err)
	}
	restore()
	if !errors.Is(err, durablevolume.ErrIdentity) || chain.sends != 0 {
		t.Fatalf("replaced exclusive marker admitted native send: sends=%d error=%v", chain.sends, err)
	}
	if _, err := store.load(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("restored marker renewed a failed native owner", err)
	}
}

// A cached signed record cannot recreate completed custody after deletion.
func TestOwnerTrimDeletedSignedJournalIsNeverRecreated(t *testing.T) {
	owner, store, chain, _ := newOwnerTrimCustodyTestOwner(t)
	owner.authority = &ownerTrimCustodyTestAuthority{before: func() {
		if err := os.Remove(store.config.Action.StatePath); err != nil {
			t.Fatal(err)
		}
	}}
	_, err := owner.step(t.Context())
	_, statErr := os.Lstat(store.config.Action.StatePath)
	if !errors.Is(err, durablevolume.ErrIdentity) || chain.sends != 0 || !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("deleted signed custody was recreated or sent: sends=%d error=%v state=%v", chain.sends, err, statErr)
	}
}

// Loss after the counted record's directory sync must still stop transport.
func TestOwnerTrimLostCountedJournalRefusesSend(t *testing.T) {
	owner, store, chain, _ := newOwnerTrimCustodyTestOwner(t)
	fired := false
	store.syncDirectory = func(directory *os.File) error {
		raw, err := os.ReadFile(store.config.Action.StatePath)
		var record ownerTrimRecord
		if err == nil {
			err = decodePlanJson(raw, &record)
		}
		if err != nil {
			return err
		}
		if record.Broadcasts == 1 {
			fired = true
			if err := os.Remove(store.config.Action.StatePath); err != nil {
				return err
			}
		}
		return directory.Sync()
	}
	_, err := owner.step(t.Context())
	if !fired || !errors.Is(err, durablevolume.ErrIdentity) || chain.sends != 0 {
		t.Fatalf("lost counted native custody admitted send: fault=%t sends=%d error=%v", fired, chain.sends, err)
	}
}

// A correctly hashed earlier record cannot erase this owner's retained bytes.
func TestOwnerTrimEarlierValidJournalIsIntegrityFailure(t *testing.T) {
	_, store, _, earlier := newOwnerTrimCustodyTestOwner(t)
	current, err := os.ReadFile(store.config.Action.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.config.Action.StatePath, earlier, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = store.load()
	if !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("earlier valid reservation erased retained native signature", err)
	}
	if err := os.WriteFile(store.config.Action.StatePath, current, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("restoring signed bytes erased native integrity failure", err)
	}
}

// Permission, link and byte faults share the same final exclusive-owner guard.
func TestOwnerTrimExclusiveCustodyRejectsFilesystemChanges(t *testing.T) {
	for _, kind := range []string{"marker-bytes", "marker-mode", "marker-link", "journal-mode", "journal-link", "journal-bytes"} {
		owner, store, chain, _ := newOwnerTrimCustodyTestOwner(t)
		owner.authority = &ownerTrimCustodyTestAuthority{before: func() {
			path := store.config.Action.StatePath
			if strings.HasPrefix(kind, "marker-") {
				path += ".lock"
			}
			var err error
			switch {
			case strings.HasSuffix(kind, "-mode"):
				err = os.Chmod(path, 0644)
			case strings.HasSuffix(kind, "-link"):
				err = os.Link(path, path+".synthetic-alias")
			default:
				var raw []byte
				raw, err = os.ReadFile(path)
				if err == nil {
					err = os.WriteFile(path, append(raw, '\n'), 0600)
				}
			}
			if err != nil {
				t.Fatal(kind, err)
			}
		}}
		_, err := owner.step(t.Context())
		if !errors.Is(err, durablevolume.ErrIdentity) || chain.sends != 0 {
			t.Fatalf("%s retained native effect authority: sends=%d error=%v", kind, chain.sends, err)
		}
	}
}

// Stop actual initial claiming before completion rather than rolling back an
// acknowledged head. Repeated resume preserves the original reserved bytes.
func TestOwnerTrimIncompleteExclusiveClaimRemainsRecoverable(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "progress-synced"} {
		preparation, f := ownerTrimPreparedTestFixture(t)
		path := f.config.Action.StatePath
		originalMarker, err := os.Stat(path + ".lock")
		if err != nil {
			t.Fatal(err)
		}
		interrupted := errors.New("synthetic trim initial claim interruption")
		store, err := openOwnerTrimStoreWithClaimHook(f.storage.Context, preparation.preparation, f.config, f.key, true, func(observed string) error {
			if observed == boundary {
				return interrupted
			}
			return nil
		})
		if store != nil || !errors.Is(err, interrupted) {
			if store != nil {
				store.close()
			}
			t.Fatal("claim did not stop at its actual durable boundary", boundary, err)
		}
		marker := rootObjectHash(f.config) + "\n" + f.key + "\n"
		raw, err := os.ReadFile(path + ".lock")
		if err != nil || !bytes.Equal(raw, []byte(marker)) {
			t.Fatal("interrupted trim acquired a completion marker", boundary, err)
		}
		_, err = os.Stat(path)
		if boundary == "marker-synced" && !errors.Is(err, os.ErrNotExist) || boundary == "progress-synced" && err != nil {
			t.Fatal("interruption changed original trim journal presence", boundary, err)
		}
		store, err = openOwnerTrimStoreWithClaimHook(f.storage.Context, preparation.preparation, f.config, f.key, false, func(observed string) error {
			if observed != "progress-synced" {
				t.Fatal("trim resume rewrote its original marker", observed)
			}
			return interrupted
		})
		if store != nil || !errors.Is(err, interrupted) {
			if store != nil {
				store.close()
			}
			t.Fatal("unfinished trim could not retain its original row", boundary, err)
		}
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		store, err = openOwnerTrimStore(f.storage.Context, preparation.preparation, f.config, f.key, false)
		if err != nil {
			t.Fatal("intact original incomplete claim could not resume", boundary, err)
		}
		record, err := store.load()
		if err != nil || record.Phase != "reserved" || record.Signature != "" || record.Broadcasts != 0 {
			t.Fatal("incomplete claim invented native progress", boundary, record.Phase, err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		if _, err := store.load(); err == nil {
			t.Fatal("closed native owner retained custody")
		}
		raw, err = os.ReadFile(path + ".lock")
		current, readErr := os.ReadFile(path)
		retainedMarker, statErr := os.Stat(path + ".lock")
		if err != nil || readErr != nil || statErr != nil || !bytes.Equal(raw, []byte(marker+bootstrapRootClaimComplete)) || !bytes.Equal(current, original) || !os.SameFile(originalMarker, retainedMarker) {
			t.Fatal("trim completion replaced original bytes or physical marker", boundary, err, readErr, statErr)
		}
	}
}

// A removed committed row or retained unknown signing intent never acquires
// initial-reservation authority from a truncated application marker.
func TestOwnerTrimCompletedClaimCannotBecomeUnfinished(t *testing.T) {
	for _, progress := range []string{"missing", "signing"} {
		preparation, f := ownerTrimPreparedTestFixture(t)
		store, err := openOwnerTrimStore(f.storage.Context, preparation.preparation, f.config, f.key, true)
		if err != nil {
			t.Fatal(err)
		}
		if progress == "signing" {
			record, err := store.load()
			if err != nil {
				t.Fatal(err)
			}
			record.Phase, record.ContentHash = "signing", ""
			record.ContentHash = rootObjectHash(record)
			if err := store.save(record); err != nil {
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
		resumed, err := openOwnerTrimStore(f.storage.Context, preparation.preparation, f.config, f.key, false)
		if resumed != nil {
			resumed.close()
		}
		if err == nil || progress == "missing" && !errors.Is(err, durablevolume.ErrIdentity) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(path))) {
			t.Fatal("completed trim custody was recreated or downgraded", progress, err)
		}
	}
}

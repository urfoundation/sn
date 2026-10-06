//go:build linux

package validator

// Fixture enrollment is explicit and test-only. It prepares a complete empty
// or imported database before the public runtime constructor is exercised.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func prepareAttemptLedgerCustodyTest(t *testing.T, ctx context.Context, root string, identity AttemptLedgerIdentity, key ed25519.PrivateKey) {
	t.Helper()
	public := key.Public().(ed25519.PublicKey)
	identity.ValidatorVPK = attemptHex32(*(*[32]byte)(public))
	limits := attemptLedgerDiskTestLimits()
	directory, err := openAttemptLedgerDirectory(root, nil, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, name := range []string{attemptLedgerStoreName, attemptLedgerImportName, attemptLedgerReadyName, attemptLedgerPendingName} {
		if _, err := directory.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("fixture provisioning requires new ledger members", name, err)
		}
	}
	marker := attemptLedgerImport{Schema: attemptLedgerImportSchema, Identity: identity, Coordinator: attemptLedgerDiskTestCoordinator, LegacySHA256: attemptHex32(sha256.Sum256(nil))}
	legacy, err := os.ReadFile(filepath.Join(root, attemptLedgerLegacyName))
	var legacyMember *attemptLedgerCustodyMember
	if err == nil {
		info, err := os.Stat(filepath.Join(root, attemptLedgerLegacyName))
		if err != nil {
			t.Fatal(err)
		}
		marker.LocalDevice, marker.LocalInode, err = attemptLedgerLocalFileID(info)
		if err != nil {
			t.Fatal(err)
		}
		marker.LegacyPresent, marker.LegacyBytes, marker.LegacySHA256 = true, uint64(len(legacy)), attemptHex32(sha256.Sum256(legacy))
		legacyMember = &attemptLedgerCustodyMember{Inode: marker.LocalInode, Bytes: uint64(len(legacy)), Sha256: attemptLedgerCustodyDigest(legacy)}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	store, err := openAttemptRecordStoreWithHooks(ctx, filepath.Join(root, attemptLedgerStoreName), identity, attemptLedgerDiskTestCoordinator, public, attemptRecordStoreBounds{
		MaxRecordBytes: limits.MaxRecordBytes, MaxRecordCount: limits.MaxRecordCount, MaxTrailCount: limits.MaxTrailCount,
		MaxRawRecordBytes: limits.MaxRawRecordBytes, MaxStorageBytes: limits.MaxStorageBytes, MaxStorageFiles: limits.MaxStorageFiles,
	}, attemptRecordStoreHooks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range bytes.Split(legacy, []byte{'\n'}) {
		if len(raw) == 0 {
			continue
		}
		var record AttemptRecord
		if err := attemptStoreDecode(raw, &record); err != nil {
			t.Fatal(err)
		}
		if err := store.Append(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	head, err := store.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	markerRaw, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	readyRaw, err := json.Marshal(attemptLedgerImportReady{Schema: attemptLedgerImportSchema, ImportSHA256: attemptHex32(sha256.Sum256(markerRaw)), LastSequence: head.LastSequence, Root: head.Root})
	if err != nil {
		t.Fatal(err)
	}
	if err := directory.publishMarker(attemptLedgerImportName, markerRaw); err != nil {
		t.Fatal(err)
	}
	if err := directory.publishMarker(attemptLedgerReadyName, readyRaw); err != nil {
		t.Fatal(err)
	}
	_, imported, err := attemptLedgerCustodyRead(ctx, directory, attemptLedgerImportName, uint64(len(markerRaw)))
	if err != nil {
		t.Fatal(err)
	}
	_, ready, err := attemptLedgerCustodyRead(ctx, directory, attemptLedgerReadyName, uint64(len(readyRaw)))
	if err != nil {
		t.Fatal(err)
	}
	database, err := os.Stat(filepath.Join(root, attemptLedgerStoreName))
	if err != nil {
		t.Fatal(err)
	}
	_, directoryInode, err := attemptLedgerLocalFileID(directory.anchor)
	if err != nil {
		t.Fatal(err)
	}
	_, databaseInode, err := attemptLedgerLocalFileID(database)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := attemptLedgerCustodyCheckpoint{Schema: attemptLedgerCustodySchema, Identity: identity, Coordinator: attemptLedgerDiskTestCoordinator,
		DirectoryInode: directoryInode, DatabaseInode: databaseInode, Import: imported, Ready: ready, Legacy: legacyMember, Committed: head}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Fsetxattr(int(directory.directory.Fd()), attemptLedgerCustodyAttribute, raw, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	if err := directory.directory.Sync(); err != nil {
		t.Fatal(err)
	}
}

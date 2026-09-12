//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// A stopped-source continuation uses the same signed record/index verifier as
// production reopen. These are lifetime counters, not a new ledger or reset.
// Server signatures remain the separate public-cut replay authority.
type StoppedAttemptLedgerCapacity struct {
	Identity           AttemptLedgerIdentity `json:"identity"`
	Coordinator        string                `json:"coordinator"`
	Head               AttemptLedgerHead     `json:"head"`
	StorageBytes       uint64                `json:"storage_bytes"`
	StorageFiles       uint64                `json:"storage_files"`
	FileMetadataSHA256 string                `json:"file_metadata_sha256"`
}

type stoppedAttemptFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Mode     uint32 `json:"mode"`
	Modified int64  `json:"modified_ns"`
}

func stoppedAttemptFiles(root *os.Root, limits AttemptLedgerDiskLimits) ([]stoppedAttemptFile, uint64, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, 0, err
	}
	entries, readErr := directory.ReadDir(int(limits.MaxStorageFiles) + 1)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, directory.Close()); err != nil {
		return nil, 0, err
	}
	if uint64(len(entries)) > limits.MaxStorageFiles {
		return nil, 0, errAttemptRecordStoreLimit
	}
	files := make([]stoppedAttemptFile, 0, len(entries))
	used, metadataBytes := uint64(attemptStoreMetadataReserve), uint64(0)
	for _, entry := range entries {
		info, err := root.Lstat(entry.Name())
		if err != nil {
			return nil, 0, err
		}
		if !info.Mode().IsRegular() || info.Size() < 0 {
			return nil, 0, errors.New("stopped ledger contains a nonregular storage entry")
		}
		if !attemptStoreMetadataName(entry.Name()) {
			if uint64(info.Size()) > limits.MaxStorageBytes || used > limits.MaxStorageBytes-uint64(info.Size()) {
				return nil, 0, errAttemptRecordStoreLimit
			}
			used += uint64(info.Size())
		} else {
			if uint64(info.Size()) > attemptStoreMetadataReserve || metadataBytes > attemptStoreMetadataReserve-uint64(info.Size()) {
				return nil, 0, errAttemptRecordStoreLimit
			}
			metadataBytes += uint64(info.Size())
		}
		files = append(files, stoppedAttemptFile{entry.Name(), info.Size(), uint32(info.Mode()), info.ModTime().UnixNano()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, used, nil
}

// The caller must own the stopped validator and supply its independently
// authenticated original activation identity, VPK and unchanged disk limits.
// Missing stores, mutable path aliases and incomplete metadata fail; nothing
// creates a database, migrates legacy files or writes a control checkpoint.
func ReadStoppedAttemptLedgerCapacity(ctx context.Context, stateDir string, identity AttemptLedgerIdentity, coordinator string, vpk ed25519.PublicKey, limits AttemptLedgerDiskLimits, prefix ...AttemptLedgerHead) (result StoppedAttemptLedgerCapacity, resultErr error) {
	if ctx == nil || ctx.Err() != nil || !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || validateAttemptLedgerIdentity(identity, vpk) != nil {
		return result, errors.New("stopped ledger capacity owner is invalid")
	}
	address, err := hex.DecodeString(strings.TrimPrefix(coordinator, "0x"))
	if err != nil || len(address) != 20 || coordinator != "0x"+hex.EncodeToString(address) || bytes.Equal(address, make([]byte, 20)) {
		return result, errors.New("stopped ledger coordinator is invalid")
	}
	if limits.MaxRecordBytes == 0 || limits.MaxRecordBytes > uint64(^uint(0)>>1)/8 || limits.MaxRecordCount == 0 || limits.MaxTrailCount == 0 || limits.MaxTrailCount > limits.MaxRecordCount || limits.MaxRawRecordBytes < limits.MaxRecordBytes || limits.MaxStorageBytes <= attemptStoreMetadataReserve || limits.MaxStorageFiles < 8 || limits.MaxStorageFiles >= uint64(^uint(0)>>1) {
		return result, errors.New("stopped ledger capacity bounds are incomplete")
	}
	path := filepath.Join(stateDir, attemptLedgerStoreName)
	physical, err := filepath.EvalSymlinks(path)
	if err != nil || physical != path {
		return result, errors.Join(errors.New("stopped ledger path is missing or aliased"), err)
	}
	pinned, err := os.Lstat(path)
	if err != nil || !pinned.IsDir() || pinned.Mode().Perm()&0o077 != 0 {
		return result, errors.Join(errors.New("stopped ledger is not a private original directory"), err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	before, used, err := stoppedAttemptFiles(root, limits)
	if err != nil {
		return result, err
	}
	db, err := leveldb.OpenFile(path, &opt.Options{ReadOnly: true, ErrorIfMissing: true, Strict: opt.StrictAll, BlockCacheCapacity: 8 * 1024 * 1024, OpenFilesCacheCapacity: 64})
	if err != nil {
		return result, err
	}
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, db.Close())
		}
	}()
	store := &attemptRecordStore{db: db, identity: attemptRecordStoreIdentity{Schema: attemptStoreSchema, Identity: identity, Coordinator: coordinator}, vpk: append(ed25519.PublicKey(nil), vpk...), disk: &attemptRecordStoreStorage{}, bounds: attemptRecordStoreBounds{MaxRecordBytes: limits.MaxRecordBytes, MaxRecordCount: limits.MaxRecordCount, MaxTrailCount: limits.MaxTrailCount, MaxRawRecordBytes: limits.MaxRawRecordBytes, MaxStorageBytes: limits.MaxStorageBytes, MaxStorageFiles: limits.MaxStorageFiles}}
	raw, err := db.Get([]byte("identity"), nil)
	var storedIdentity attemptRecordStoreIdentity
	if err != nil || attemptStoreDecode(raw, &storedIdentity) != nil || storedIdentity != store.identity {
		return result, errors.Join(errors.New("stopped ledger differs from original activation identity"), err)
	}
	raw, err = db.Get([]byte("head"), nil)
	if err != nil || attemptStoreDecode(raw, &store.head) != nil {
		return result, errors.Join(errors.New("stopped ledger lifetime head is unavailable"), err)
	}
	if err := store.verifyContents(ctx); err != nil {
		return result, fmt.Errorf("stopped ledger signed lifetime prefix: %w", err)
	}
	if len(prefix) > 1 {
		return result, errors.New("stopped ledger has more than one original lifetime checkpoint")
	}
	if len(prefix) == 1 {
		prior := prefix[0]
		if prior.LastSequence > store.head.LastSequence || prior.RecordBytes > store.head.RecordBytes || prior.TrailCount > store.head.TrailCount {
			return result, errors.New("stopped ledger lost its original lifetime checkpoint")
		}
		if prior.LastSequence == 0 {
			if prior.Root != zeroAttemptHash() || prior.RecordBytes != 0 || prior.TrailCount != 0 {
				return result, errors.New("stopped ledger original empty checkpoint differs")
			}
		} else {
			record, err := store.readRecord(prior.LastSequence)
			if err != nil || record.RecordHash != prior.Root {
				return result, errors.Join(errors.New("stopped ledger changed its authenticated original record prefix"), err)
			}
		}
		if prior.LastSequence == store.head.LastSequence && prior != store.head {
			return result, errors.New("stopped ledger changed unchanged-prefix lifetime counters")
		}
	}
	if err := db.Close(); err != nil {
		closed = true
		return result, err
	}
	closed = true
	after, afterUsed, err := stoppedAttemptFiles(root, limits)
	if err != nil {
		return result, err
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(pinned, current) || used != afterUsed || !reflect.DeepEqual(before, after) {
		return result, errors.Join(errors.New("stopped ledger storage changed during read-only replay"), err)
	}
	metadata, err := json.Marshal(before)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(metadata)
	result = StoppedAttemptLedgerCapacity{Identity: identity, Coordinator: coordinator, Head: store.head, StorageBytes: used, StorageFiles: uint64(len(before)), FileMetadataSHA256: "sha256:" + hex.EncodeToString(digest[:])}
	return result, ctx.Err()
}

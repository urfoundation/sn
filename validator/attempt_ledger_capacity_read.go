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
	"sync"

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

// One stopped startup invocation may reuse authenticated capacity. Every read
// still locks the database and hashes its actual storage bytes. Methods are
// concurrent-safe; simultaneous cold reads may independently verify. No file,
// database lock, failure or durable cache survives a read.
type StoppedAttemptLedgerCapacityCache struct {
	stateLock sync.Mutex
	sourceKVs map[string]stoppedAttemptCapacityVerification
	hooks     attemptRecordStoreHooks
}

// A successful replay binds both caller authority and the complete physical
// contents. File timestamps alone never authorize reuse.
type stoppedAttemptCapacityVerification struct {
	inputSha256    [32]byte
	contentsSha256 [32]byte
	directory      os.FileInfo
	capacity       StoppedAttemptLedgerCapacity
}

// The zero value owns an empty invocation cache. Callers retain stopped-writer
// exclusion and revalidate their surrounding history, configuration and runway.
func (self *StoppedAttemptLedgerCapacityCache) Read(ctx context.Context, stateDir string, identity AttemptLedgerIdentity, coordinator string, vpk ed25519.PublicKey, limits AttemptLedgerDiskLimits, prefix ...AttemptLedgerHead) (StoppedAttemptLedgerCapacity, error) {
	return readStoppedAttemptLedgerCapacity(ctx, stateDir, identity, coordinator, vpk, limits, self, prefix...)
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
	return readStoppedAttemptLedgerCapacity(ctx, stateDir, identity, coordinator, vpk, limits, nil, prefix...)
}

// Reuse changes only the expensive record replay. Owner validation, current
// storage limits, byte identity, locking, close errors and cancellation remain
// admission conditions on every call.
func readStoppedAttemptLedgerCapacity(ctx context.Context, stateDir string, identity AttemptLedgerIdentity, coordinator string, vpk ed25519.PublicKey, limits AttemptLedgerDiskLimits, reuse *StoppedAttemptLedgerCapacityCache, prefix ...AttemptLedgerHead) (result StoppedAttemptLedgerCapacity, resultErr error) {
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
	var verified *stoppedAttemptCapacityVerification
	// Registered before file/database cleanup: a close failure cannot publish
	// success, and a canceled invocation cannot turn partial work into a hit.
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if reuse != nil && verified != nil && resultErr == nil {
			func() {
				reuse.stateLock.Lock()
				defer reuse.stateLock.Unlock()
				if reuse.sourceKVs == nil {
					reuse.sourceKVs = map[string]stoppedAttemptCapacityVerification{}
				}
				reuse.sourceKVs[stateDir] = *verified
			}()
		}
	}()
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
	var inputSha256, contentsSha256 [32]byte
	var priorVerification stoppedAttemptCapacityVerification
	reused := false
	hooks := attemptRecordStoreHooks{}
	if reuse != nil {
		input, err := json.Marshal(struct {
			StateDir    string
			Identity    AttemptLedgerIdentity
			Coordinator string
			Vpk         ed25519.PublicKey
			Limits      AttemptLedgerDiskLimits
			Prefix      []AttemptLedgerHead
		}{StateDir: stateDir, Identity: identity, Coordinator: coordinator, Vpk: vpk, Limits: limits, Prefix: prefix})
		if err != nil {
			return result, err
		}
		inputSha256 = sha256.Sum256(input)
		contentsSha256, err = stoppedAttemptContentsSha256(ctx, root, before)
		if err != nil {
			return result, err
		}
		func() {
			reuse.stateLock.Lock()
			defer reuse.stateLock.Unlock()
			priorVerification, reused = reuse.sourceKVs[stateDir]
		}()
		reused = reused && priorVerification.inputSha256 == inputSha256 && priorVerification.contentsSha256 == contentsSha256 && os.SameFile(priorVerification.directory, pinned)
		hooks = reuse.hooks
	}
	store := &attemptRecordStore{db: db, identity: attemptRecordStoreIdentity{Schema: attemptStoreSchema, Identity: identity, Coordinator: coordinator}, vpk: append(ed25519.PublicKey(nil), vpk...), disk: &attemptRecordStoreStorage{path: path, anchor: pinned, root: root, hooks: hooks}, bounds: attemptRecordStoreBounds{MaxRecordBytes: limits.MaxRecordBytes, MaxRecordCount: limits.MaxRecordCount, MaxTrailCount: limits.MaxTrailCount, MaxRawRecordBytes: limits.MaxRawRecordBytes, MaxStorageBytes: limits.MaxStorageBytes, MaxStorageFiles: limits.MaxStorageFiles}}
	store.disk.fault = store.latchFault
	raw, err := db.Get([]byte("identity"), nil)
	var storedIdentity attemptRecordStoreIdentity
	if err != nil || attemptStoreDecode(raw, &storedIdentity) != nil || storedIdentity != store.identity {
		return result, errors.Join(errors.New("stopped ledger differs from original activation identity"), err)
	}
	raw, err = db.Get([]byte("head"), nil)
	if err != nil || attemptStoreDecode(raw, &store.head) != nil {
		return result, errors.Join(errors.New("stopped ledger lifetime head is unavailable"), err)
	}
	if !reused {
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
		if reuse != nil {
			afterSha256, err := stoppedAttemptContentsSha256(ctx, root, before)
			if err != nil || afterSha256 != contentsSha256 {
				return result, errors.Join(errors.New("stopped ledger bytes changed during signed replay"), err)
			}
		}
	} else if store.head != priorVerification.capacity.Head {
		return result, errors.New("stopped ledger cached head differs from current storage")
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
	if reuse != nil {
		verified = &stoppedAttemptCapacityVerification{inputSha256: inputSha256, contentsSha256: contentsSha256, directory: pinned, capacity: result}
	}
	return result, ctx.Err()
}

// Hash the sorted manifest and every bounded file byte under the read-only
// database lock. Each descriptor must still name the original regular entry;
// cancellation and same-size, restored-timestamp changes remain observable.
func stoppedAttemptContentsSha256(ctx context.Context, root *os.Root, files []stoppedAttemptFile) (result [32]byte, resultErr error) {
	digest := sha256.New()
	if err := json.NewEncoder(digest).Encode(files); err != nil {
		return result, err
	}
	buffer := make([]byte, 64*1024)
	for _, entry := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := func() (resultErr error) {
			pinned, err := root.Lstat(entry.Name)
			if err != nil || !pinned.Mode().IsRegular() || pinned.Size() != entry.Size || uint32(pinned.Mode()) != entry.Mode || pinned.ModTime().UnixNano() != entry.Modified {
				return errors.Join(errors.New("stopped ledger entry changed before content read"), err)
			}
			file, err := root.Open(entry.Name)
			if err != nil {
				return err
			}
			defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
			opened, err := file.Stat()
			if err != nil || !os.SameFile(pinned, opened) {
				return errors.Join(errors.New("stopped ledger content descriptor changed identity"), err)
			}
			for remaining := entry.Size; remaining > 0; {
				if err := ctx.Err(); err != nil {
					return err
				}
				chunk := buffer[:min(int64(len(buffer)), remaining)]
				if _, err := io.ReadFull(file, chunk); err != nil {
					return err
				}
				_, _ = digest.Write(chunk)
				remaining -= int64(len(chunk))
			}
			n, err := file.Read(buffer[:1])
			if n != 0 || !errors.Is(err, io.EOF) {
				return errors.Join(errors.New("stopped ledger file grew during content read"), err)
			}
			current, err := root.Lstat(entry.Name)
			if err != nil || !os.SameFile(pinned, current) || current.Size() != entry.Size || uint32(current.Mode()) != entry.Mode || current.ModTime().UnixNano() != entry.Modified {
				return errors.Join(errors.New("stopped ledger entry changed during content read"), err)
			}
			return ctx.Err()
		}(); err != nil {
			return result, err
		}
	}
	copy(result[:], digest.Sum(nil))
	return result, ctx.Err()
}

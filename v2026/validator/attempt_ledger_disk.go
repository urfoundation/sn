//go:build linux || darwin

package validator

// Explicit disk construction imports an unchanged v1 JSONL source under a
// descriptor-owned migration gate. Immutable private receipts distinguish an
// interrupted local import from a complete portable copy of the signed prefix.

import (
	"bufio"
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
	"strings"
)

const attemptLedgerImportSchema = "urnetwork-private-attempt-ledger-import-v1"

// The byte digest is portable authority; device and inode bind only local
// resumption before the complete imported-prefix receipt has become durable.
type attemptLedgerImport struct {
	Schema        string                `json:"schema"`
	Identity      AttemptLedgerIdentity `json:"identity"`
	Coordinator   string                `json:"coordinator"`
	LegacyPresent bool                  `json:"legacy_present"`
	LegacyBytes   uint64                `json:"legacy_bytes"`
	LegacySHA256  string                `json:"legacy_sha256"`
	LocalDevice   uint64                `json:"local_device"`
	LocalInode    uint64                `json:"local_inode"`
}

// The complete receipt commits the original import marker and exact imported
// sequence/root. Later appends may extend this prefix without changing it.
type attemptLedgerImportReady struct {
	Schema       string `json:"schema"`
	ImportSHA256 string `json:"import_sha256"`
	LastSequence uint64 `json:"last_sequence"`
	Root         string `json:"root"`
}

// Tests force cancellation and ambiguity at observed persistence boundaries.
type attemptLedgerDiskHooks struct {
	Step  func(string, string) error
	Store attemptRecordStoreHooks
}

// This is deliberately opt-in. No production constructor or public schema is
// switched until compact cuts and complete streaming replay are available.
func NewDiskAttemptLedger(ctx context.Context, stateDir string, identity AttemptLedgerIdentity, coordinator string, vsk ed25519.PrivateKey, limits AttemptLedgerDiskLimits) (*AttemptLedger, error) {
	return newDiskAttemptLedgerWithHooks(ctx, stateDir, identity, coordinator, vsk, limits, attemptLedgerDiskHooks{})
}

// A failed constructor closes the backend and publishes no live partial owner.
// Cancellation leaves the original JSONL and durable imported prefix intact.
func newDiskAttemptLedgerWithHooks(ctx context.Context, stateDir string, identity AttemptLedgerIdentity, coordinator string, vsk ed25519.PrivateKey, limits AttemptLedgerDiskLimits, hooks attemptLedgerDiskHooks) (result *AttemptLedger, resultErr error) {
	if ctx == nil {
		return nil, errors.New("attempt ledger opening context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(vsk) != ed25519.PrivateKeySize || !bytes.Equal(vsk, ed25519.NewKeyFromSeed(vsk[:ed25519.SeedSize])) {
		return nil, errors.New("attempt ledger validator private key is invalid")
	}
	vpk := vsk.Public().(ed25519.PublicKey)
	identity.ValidatorVPK = attemptHex32(*(*[32]byte)(vpk))
	if err := validateAttemptLedgerIdentity(identity, vpk); err != nil {
		return nil, err
	}
	address, err := hex.DecodeString(strings.TrimPrefix(coordinator, "0x"))
	if err != nil || len(address) != 20 || coordinator != "0x"+hex.EncodeToString(address) || bytes.Equal(address, make([]byte, 20)) {
		return nil, errors.New("attempt ledger coordinator is not canonical")
	}
	maxInt := uint64(^uint(0) >> 1)
	if limits.MaxRecordBytes == 0 || limits.MaxRecordBytes > maxInt/8 || limits.MaxRecordCount == 0 || limits.MaxRecordCount > maxInt || limits.MaxTrailCount == 0 || limits.MaxTrailCount > limits.MaxRecordCount || limits.MaxRawRecordBytes < limits.MaxRecordBytes || limits.MaxStorageBytes <= attemptStoreMetadataReserve || limits.MaxStorageFiles < 8 || uint64(len(identity.DeploymentID)) > limits.MaxRecordBytes || limits.MaxLegacyBytes == 0 || limits.MaxLegacyBytes >= maxInt || limits.MaxProofBytes == 0 || limits.MaxProofBytes >= maxInt {
		return nil, errors.New("attempt ledger disk limits are incomplete or inconsistent")
	}
	directory, err := openAttemptLedgerDirectory(stateDir, hooks.Step)
	if err != nil {
		return nil, err
	}
	var store *attemptRecordStore
	var source *os.File
	entered := false
	defer func() {
		if source != nil {
			resultErr = errors.Join(resultErr, source.Close())
		}
		if entered {
			resultErr = errors.Join(resultErr, directory.leave())
		}
		if resultErr != nil || result == nil {
			result = nil
			if store != nil {
				resultErr = errors.Join(resultErr, store.Close())
			}
			resultErr = errors.Join(resultErr, directory.Close())
		}
	}()
	if err := directory.enter(ctx); err != nil {
		return nil, err
	}
	entered = true
	metadataLimit := limits.MaxRecordBytes*6 + 4096
	markerRaw, err := directory.readSmall(attemptLedgerImportName, metadataLimit)
	markerPresent := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	readyRaw, err := directory.readSmall(attemptLedgerReadyName, 4096)
	readyPresent := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if readyPresent && !markerPresent {
		return nil, errors.New("attempt ledger complete receipt has no import provenance")
	}
	marker := attemptLedgerImport{Schema: attemptLedgerImportSchema, Identity: identity, Coordinator: coordinator, LegacySHA256: attemptHex32(sha256.Sum256(nil))}
	source, err = directory.openFile(attemptLedgerLegacyName, os.O_RDONLY, false)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var sourceInfo os.FileInfo
	if source != nil {
		marker.LegacyPresent = true
		sourceInfo, err = source.Stat()
		if err != nil || sourceInfo.Size() < 0 || uint64(sourceInfo.Size()) > limits.MaxLegacyBytes {
			return nil, errors.Join(errors.New("attempt ledger legacy file exceeds its explicit byte bound"), err)
		}
		marker.LocalDevice, marker.LocalInode, err = attemptLedgerLocalFileID(sourceInfo)
		if err != nil {
			return nil, err
		}
		marker.LegacyBytes, marker.LegacySHA256, err = attemptLedgerSourceDigest(ctx, source, limits.MaxLegacyBytes)
		if err != nil {
			return nil, err
		}
		if marker.LegacyBytes != uint64(sourceInfo.Size()) {
			return nil, errors.New("attempt ledger legacy file changed while hashing")
		}
		if _, err := source.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	if markerPresent {
		var prior attemptLedgerImport
		if err := attemptStoreDecode(markerRaw, &prior); err != nil {
			return nil, fmt.Errorf("attempt ledger import provenance: %w", err)
		}
		comparable := marker
		if readyPresent {
			comparable.LocalDevice, comparable.LocalInode = prior.LocalDevice, prior.LocalInode
		}
		if prior != comparable {
			return nil, errors.New("attempt ledger import provenance differs from the exact legacy source or namespace")
		}
		marker = prior
	} else {
		if _, err := directory.root.Lstat(attemptLedgerStoreName); err == nil {
			return nil, errors.New("attempt ledger database has no durable import provenance")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		markerRaw, err = json.Marshal(marker)
		if err != nil {
			return nil, err
		}
	}
	if err := directory.publishMarker(attemptLedgerImportName, markerRaw); err != nil {
		return nil, err
	}
	var ready attemptLedgerImportReady
	if readyPresent {
		if err := attemptStoreDecode(readyRaw, &ready); err != nil || ready.Schema != attemptLedgerImportSchema || ready.ImportSHA256 != attemptHex32(sha256.Sum256(markerRaw)) {
			return nil, errors.Join(errors.New("attempt ledger complete import receipt is invalid"), err)
		}
	}
	store, err = openAttemptRecordStoreWithParent(ctx, filepath.Join(directory.path, attemptLedgerStoreName), identity, coordinator, vpk, attemptRecordStoreBounds{
		MaxRecordBytes: limits.MaxRecordBytes, MaxRecordCount: limits.MaxRecordCount,
		MaxTrailCount: limits.MaxTrailCount, MaxRawRecordBytes: limits.MaxRawRecordBytes,
		MaxStorageBytes: limits.MaxStorageBytes, MaxStorageFiles: limits.MaxStorageFiles,
	}, hooks.Store, directory.root)
	if err != nil {
		return nil, err
	}
	head, err := store.Head()
	if err != nil {
		return nil, err
	}
	var importedSequence uint64
	importedRoot := zeroAttemptHash()
	if source != nil {
		// ReadSlice bounds each input fragment; the full line never exceeds
		// the caller's single-record allowance plus one JSONL delimiter.
		reader := bufio.NewReaderSize(io.LimitReader(source, int64(limits.MaxLegacyBytes)+1), 64*1024)
		digest := sha256.New()
		var rawBytes uint64
		var line []byte
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			part, readErr := reader.ReadSlice('\n')
			if uint64(len(part)) > limits.MaxLegacyBytes-rawBytes {
				return nil, errors.New("attempt ledger legacy stream exceeds its byte bound")
			}
			rawBytes += uint64(len(part))
			_, _ = digest.Write(part)
			if uint64(len(line))+uint64(len(part)) > limits.MaxRecordBytes+1 {
				return nil, errors.New("attempt ledger legacy line exceeds its record bound")
			}
			line = append(line, part...)
			if errors.Is(readErr, bufio.ErrBufferFull) {
				continue
			}
			if errors.Is(readErr, io.EOF) {
				if len(line) != 0 {
					return nil, errors.New("attempt ledger legacy source has a torn final line")
				}
				break
			}
			if readErr != nil {
				return nil, readErr
			}
			canonical := line[:len(line)-1]
			if len(canonical) != 0 {
				if importedSequence == limits.MaxRecordCount {
					return nil, errors.New("attempt ledger legacy source exceeds its record count")
				}
				var record AttemptRecord
				if err := attemptStoreDecode(canonical, &record); err != nil {
					return nil, fmt.Errorf("attempt ledger legacy sequence %d: %w", importedSequence+1, err)
				}
				if record.Sequence != importedSequence+1 || record.PreviousHash != importedRoot {
					return nil, errors.New("attempt ledger legacy sequence or root differs")
				}
				if record.Sequence <= head.LastSequence {
					stored, err := store.Read(ctx, record.Sequence)
					if err != nil {
						return nil, err
					}
					storedRaw, err := json.Marshal(stored)
					if err != nil || !bytes.Equal(storedRaw, canonical) {
						return nil, errors.Join(errors.New("attempt ledger database is not the exact signed legacy prefix"), err)
					}
				} else {
					if readyPresent {
						return nil, errors.New("attempt ledger database omits a completed import record")
					}
					if err := store.Append(ctx, record); err != nil {
						return nil, err
					}
				}
				importedSequence, importedRoot = record.Sequence, record.RecordHash
				if hooks.Step != nil {
					if err := hooks.Step("import-record", fmt.Sprint(importedSequence)); err != nil {
						return nil, err
					}
				}
			}
			line = line[:0]
		}
		if rawBytes != marker.LegacyBytes || "0x"+hex.EncodeToString(digest.Sum(nil)) != marker.LegacySHA256 {
			return nil, errors.New("attempt ledger legacy bytes changed during import")
		}
		current, err := directory.root.Lstat(attemptLedgerLegacyName)
		if err != nil || !attemptLedgerPrivateFile(current) || !os.SameFile(sourceInfo, current) || current.Size() != sourceInfo.Size() {
			return nil, errors.New("attempt ledger legacy inode changed during import")
		}
	}
	head, err = store.Head()
	if err != nil {
		return nil, err
	}
	expectedReady := attemptLedgerImportReady{Schema: attemptLedgerImportSchema, ImportSHA256: attemptHex32(sha256.Sum256(markerRaw)), LastSequence: importedSequence, Root: importedRoot}
	if readyPresent {
		if ready != expectedReady || head.LastSequence < ready.LastSequence {
			return nil, errors.New("attempt ledger completed import prefix differs")
		}
	} else {
		if head.LastSequence != importedSequence || head.Root != importedRoot {
			return nil, errors.New("attempt ledger incomplete import has records outside the legacy prefix")
		}
		readyRaw, err = json.Marshal(expectedReady)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := directory.publishMarker(attemptLedgerReadyName, readyRaw); err != nil {
		return nil, err
	}
	if err := directory.check(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ledger := &AttemptLedger{path: filepath.Join(directory.path, attemptLedgerLegacyName), identity: identity,
		vsk: append(ed25519.PrivateKey(nil), vsk...), vpk: append(ed25519.PublicKey(nil), vpk...),
		disk: store, diskLimits: limits, directory: directory, durableSequence: head.LastSequence}
	ledger.initLifetime()
	return ledger, nil
}

// One fixed scratch buffer hashes the preserved source independently of the
// decoded-record pass. An oversize source is rejected without materialization.
func attemptLedgerSourceDigest(ctx context.Context, source *os.File, limit uint64) (uint64, string, error) {
	digest := sha256.New()
	buffer := make([]byte, 64*1024)
	var total uint64
	for {
		if err := ctx.Err(); err != nil {
			return 0, "", err
		}
		n, err := source.Read(buffer)
		if uint64(n) > limit-total {
			return 0, "", errors.New("attempt ledger source exceeds its byte bound")
		}
		total += uint64(n)
		_, _ = digest.Write(buffer[:n])
		if errors.Is(err, io.EOF) {
			return total, "0x" + hex.EncodeToString(digest.Sum(nil)), nil
		}
		if err != nil {
			return 0, "", err
		}
	}
}

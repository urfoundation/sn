//go:build linux || darwin

// Recovery recreates existing public objects from a stopped, authenticated
// record store. It accepts no private key and never changes source history.
package validator

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Callbacks stage operation-local public objects. They must not publish them
// until the complete recovery succeeds, including source close and replay.
// The caller retains the stopped-writer fence and fresh scratch directory.
type AttemptCutV2RecoveryOptions struct {
	LedgerLimits     AttemptLedgerDiskLimits
	ReplayBounds     AttemptCutV2ReplayBounds
	ScratchDirectory string
	ServerKeys       map[byte]ed25519.PublicKey
	WriteRecords     AttemptStreamV2ObjectWriter
	WriteProofs      AttemptStreamV2ObjectWriter
	WriteMetadata    AttemptStreamV2ObjectWriter
	ReadMetadata     AttemptStreamV2MetadataReader
	OpenData         AttemptStreamV2DataOpener
}

// Returns only after both original signed stream references are reproduced
// exactly and their staged bytes pass the complete policy-aware replay. A
// later ledger head is allowed, but cannot extend the original cut's range.
// Source access uses the existing strict read-only stopped-ledger verifier;
// no legacy import, signing owner or control checkpoint is opened.
func RecoverStoppedAttemptCutV2Objects(ctx context.Context, stateDir string, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2RecoveryOptions) (result AttemptCutV2ReplayResult, resultErr error) {
	if ctx == nil || options.WriteRecords == nil || options.WriteProofs == nil || options.WriteMetadata == nil || options.ReadMetadata == nil || options.OpenData == nil {
		return result, errors.New("attempt object recovery ownership is incomplete")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = AttemptCutV2ReplayResult{}
		}
	}()
	if err := cut.VerifyHeader(expected, bounds); err != nil {
		return result, err
	}
	depth, err := attemptCutV2PolicyDepth(expected, policy)
	if err != nil {
		return result, err
	}
	if err := options.ReplayBounds.validate(bounds); err != nil {
		return result, err
	}
	if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir {
		return result, errors.New("attempt recovery source path is not canonical")
	}
	directory, err := os.OpenFile(filepath.Join(stateDir, attemptLedgerStoreName), os.O_RDONLY|attemptStoreNoFollowFlag(), 0)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	anchor, err := directory.Stat()
	if err != nil || !attemptStorePrivateDirectory(anchor) {
		return result, errors.Join(errors.New("attempt recovery source is not the original private backend"), err)
	}
	if err := attemptStoreLockFile(directory); err != nil {
		return result, fmt.Errorf("attempt recovery source still has a writer: %w", err)
	}
	vpk, err := canonicalAttemptHex32("recovery validator public key", expected.Identity.ValidatorVPK, false)
	if err != nil {
		return result, err
	}
	coordinator := "0x" + hex.EncodeToString(expected.Activation.Domain.Coordinator[:])
	_, err = withStoppedAttemptLedger(ctx, stateDir, expected.Identity, coordinator, vpk[:], options.LedgerLimits, nil, func(store *attemptRecordStore) (visitErr error) {
		if !os.SameFile(anchor, store.disk.anchor) {
			return errors.New("attempt recovery source changed after acquiring its writer fence")
		}
		if cut.LastSequence > store.head.LastSequence {
			return errors.New("attempt recovery lost the original signed range")
		}
		if cut.RecordCount == 0 {
			result, visitErr = ReplayAttemptCutV2WithPolicy(ctx, cut, expected, policy, bounds, AttemptCutV2ReplayOptions{Bounds: options.ReplayBounds, ScratchDirectory: filepath.Join(options.ScratchDirectory, "replay"), ServerKeys: options.ServerKeys, ReadMetadata: options.ReadMetadata, OpenData: options.OpenData})
			return visitErr
		}
		scratch, err := newAttemptCutV2SealScratch(ctx, options.ScratchDirectory, attemptCutV2SealHooks{})
		if err != nil {
			return err
		}
		defer func() { visitErr = errors.Join(visitErr, scratch.close()) }()
		head := AttemptLedgerHead{LastSequence: cut.LastSequence, Root: cut.Root}
		for index, stream := range []struct {
			kind      string
			bounds    AttemptStreamV2Bounds
			maximum   uint64
			reference AttemptStreamV2Reference
			write     AttemptStreamV2ObjectWriter
		}{
			{kind: AttemptStreamV2Records, bounds: bounds.Records, maximum: options.ReplayBounds.MaxRecordBytes, reference: cut.Records, write: options.WriteRecords},
			{kind: AttemptStreamV2Proofs, bounds: bounds.Proofs, maximum: options.ReplayBounds.MaxProofBytes, reference: cut.Proofs, write: options.WriteProofs},
		} {
			spool, err := scratch.openSpool(index, stream.bounds.MaxChunks)
			if err != nil {
				return err
			}
			source := func(ctx context.Context, visit func(uint64, []byte) error) error {
				return walkAttemptCutV2Prefix(ctx, store.readRecord, head, expected, depth, func(record AttemptRecord) error {
					var value any = record
					if stream.kind == AttemptStreamV2Proofs {
						if record.Disposition != AttemptDispositionComplete {
							return nil
						}
						value = record.Proof
					}
					raw, err := json.Marshal(value)
					if err != nil || uint64(len(raw))+1 > stream.maximum {
						return errors.Join(errors.New("attempt recovery row exceeds its admitted bound"), err)
					}
					return visit(record.Sequence, append(raw, '\n'))
				})
			}
			reference, _, err := WriteAttemptStreamV2(ctx, stream.kind, source, AttemptStreamV2WriteOptions{Bounds: stream.bounds, MaxRowBytes: stream.maximum, Spool: spool, WriteData: stream.write, WriteMetadata: options.WriteMetadata})
			if err != nil {
				return err
			}
			if reference != stream.reference {
				return fmt.Errorf("recovered %s objects differ from the original signed reference", stream.kind)
			}
		}
		result, err = ReplayAttemptCutV2WithPolicy(ctx, cut, expected, policy, bounds, AttemptCutV2ReplayOptions{Bounds: options.ReplayBounds, ScratchDirectory: filepath.Join(options.ScratchDirectory, "replay"), ServerKeys: options.ServerKeys, ReadMetadata: options.ReadMetadata, OpenData: options.OpenData})
		return err
	})
	return result, err
}

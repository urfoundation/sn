//go:build linux || darwin

package validator

// Cut sealing snapshots one checked disk-ledger prefix, stages complete typed
// streams, and fetches them back through the full policy-aware content replay.
// Only a successful replay and every owned close permit the final signature.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/urfoundation/sn/protocol"
)

// Callbacks stage durable immutable objects and fetch those exact objects
// back; they never receive a signed header. Inputs must remain unchanged for
// the call. Callbacks may append to the ledger but must not close it or start
// a nested Walk, because the source walks one fixed snapshot at a time.
// ScratchDirectory must be a fresh clean absolute path. The caller retains
// all failed/successful scratch and staged objects; this API erases none.
// Scratch data is bounded by ReplayBounds.MaxScratchBytes plus 104 bytes per
// permitted record/proof chunk, with two spool files and one replay directory.
// The existing ledger independently bounds each decoded input record. No new
// default, publication, replica, control-graph or total capacity budget is set.
type AttemptCutV2SealOptions struct {
	ReplayBounds     AttemptCutV2ReplayBounds
	ScratchDirectory string
	ServerKeys       map[byte]ed25519.PublicKey
	WriteRecords     AttemptStreamV2ObjectWriter
	WriteProofs      AttemptStreamV2ObjectWriter
	WriteMetadata    AttemptStreamV2ObjectWriter
	ReadMetadata     AttemptStreamV2MetadataReader
	OpenData         AttemptStreamV2DataOpener
}

// Returns only an independently replayed, signed header and verified census.
// This does not publish an on-chain decision, activate a producer, establish
// cross-cut terminal-ID history, or replace the caller's cut/drain ownership.
func SealAttemptCutV2(ctx context.Context, ledger *AttemptLedger, expected AttemptCutV2Context, policy protocol.Policy, privateKey ed25519.PrivateKey, bounds AttemptCutV2Bounds, options AttemptCutV2SealOptions) (*AttemptCutV2, AttemptCutV2ReplayResult, error) {
	return sealAttemptCutV2WithHooks(ctx, ledger, expected, policy, privateKey, bounds, options, attemptCutV2SealHooks{})
}

// All errors clear both results, including cancellation or a late real close
// failure. External callbacks never run under a ledger metadata/append mutex.
func sealAttemptCutV2WithHooks(ctx context.Context, ledger *AttemptLedger, expected AttemptCutV2Context, policy protocol.Policy, privateKey ed25519.PrivateKey, bounds AttemptCutV2Bounds, options AttemptCutV2SealOptions, hooks attemptCutV2SealHooks) (result *AttemptCutV2, verified AttemptCutV2ReplayResult, resultErr error) {
	if ctx == nil || ledger == nil || options.WriteRecords == nil || options.WriteProofs == nil || options.WriteMetadata == nil || options.ReadMetadata == nil || options.OpenData == nil {
		return nil, verified, errors.New("compact attempt sealer authority is incomplete")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result, verified = nil, AttemptCutV2ReplayResult{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, verified, err
	}
	depth, err := attemptCutV2PolicyDepth(expected, policy)
	if err != nil {
		return nil, verified, err
	}
	if !filepath.IsAbs(options.ScratchDirectory) || filepath.Clean(options.ScratchDirectory) != options.ScratchDirectory || filepath.Dir(options.ScratchDirectory) == options.ScratchDirectory {
		return nil, verified, errors.New("compact attempt sealer scratch path is invalid")
	}
	store, disk := ledger.disk.(*attemptRecordStore)
	if !disk || store == nil || ledger.identity != expected.Identity || store.identity.Identity != expected.Identity || store.identity.Coordinator != "0x"+hex.EncodeToString(expected.Activation.Domain.Coordinator[:]) {
		return nil, verified, errors.New("compact attempt sealer requires the expected disk-ledger namespace")
	}
	vpk, err := canonicalAttemptHex32("compact attempt sealer validator", expected.Identity.ValidatorVPK, false)
	if err != nil {
		return nil, verified, err
	}
	if err := attemptCutV2PrivateKey(privateKey, vpk[:]); err != nil {
		return nil, verified, err
	}
	privateKey = append(ed25519.PrivateKey(nil), privateKey...)
	serverKeys := make(map[byte]ed25519.PublicKey, len(options.ServerKeys))
	for keyID, key := range options.ServerKeys {
		if len(key) != ed25519.PublicKeySize {
			return nil, verified, errors.New("compact attempt sealer server key is invalid")
		}
		serverKeys[keyID] = bytes.Clone(key)
	}
	if bounds.MaxHeaderBytes == 0 {
		return nil, verified, errors.New("compact attempt sealer header bound is missing")
	}
	if err := options.ReplayBounds.validate(bounds); err != nil {
		return nil, verified, err
	}
	for _, stream := range []struct {
		kind   string
		bounds AttemptStreamV2Bounds
		row    uint64
	}{
		{kind: AttemptStreamV2Records, bounds: bounds.Records, row: options.ReplayBounds.MaxRecordBytes},
		{kind: AttemptStreamV2Proofs, bounds: bounds.Proofs, row: options.ReplayBounds.MaxProofBytes},
	} {
		if _, err := attemptStreamV2WritePageCapacity(stream.kind, AttemptStreamV2WriteOptions{Bounds: stream.bounds, MaxRowBytes: stream.row}); err != nil {
			return nil, verified, err
		}
	}
	spoolBytes := (bounds.Records.MaxChunks + bounds.Proofs.MaxChunks) * attemptStreamV2DescriptorBytes
	if options.ReplayBounds.MaxScratchBytes > ^uint64(0)-spoolBytes {
		return nil, verified, errors.New("compact attempt sealer aggregate scratch bound overflows")
	}
	head, err := ledger.Head()
	if err != nil {
		return nil, verified, err
	}
	if head.LastSequence == ^uint64(0) || expected.FirstSequence > head.LastSequence+1 || expected.EgressFirstSequence > head.LastSequence+1 {
		return nil, verified, errors.New("compact attempt sealer cursors exceed the checked durable prefix")
	}
	if _, err := canonicalAttemptHex32("compact attempt durable root", head.Root, true); err != nil {
		return nil, verified, err
	}
	count := head.LastSequence + 1 - expected.FirstSequence
	if count > bounds.Records.MaxItems {
		return nil, verified, errors.New("compact attempt checked prefix exceeds the record count bound")
	}
	if hooks.HeadCaptured != nil {
		if err := hooks.HeadCaptured(head); err != nil {
			return nil, verified, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, verified, err
	}
	cut := AttemptCutV2{Schema: AttemptCutV2Schema, Context: expected, LastSequence: head.LastSequence, RecordCount: count, Root: head.Root,
		Records: AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}, Proofs: AttemptStreamV2Reference{ManifestHash: zeroAttemptHash()}}
	replayOptions := AttemptCutV2ReplayOptions{Bounds: options.ReplayBounds, ScratchDirectory: filepath.Join(options.ScratchDirectory, "replay"), ServerKeys: serverKeys, ReadMetadata: options.ReadMetadata, OpenData: options.OpenData}
	if count == 0 {
		if expected.PriorRoot != head.Root {
			return nil, verified, errors.New("empty compact attempt cut differs from the checked prior root")
		}
		verified, err = replayAttemptCutV2Contents(ctx, cut, expected, bounds, replayOptions, depth, hooks.Replay, nil)
		if err != nil {
			return nil, verified, err
		}
	} else {
		scratch, err := newAttemptCutV2SealScratch(ctx, options.ScratchDirectory, hooks)
		if err != nil {
			return nil, verified, err
		}
		defer func() { resultErr = errors.Join(resultErr, scratch.close()) }()
		recordSpool, err := scratch.openSpool(0, bounds.Records.MaxChunks)
		if err != nil {
			return nil, verified, err
		}
		recordSource := func(ctx context.Context, visit func(uint64, []byte) error) error {
			return walkAttemptCutV2SealPrefix(ctx, ledger, head, expected, depth, func(record AttemptRecord) error {
				encoded, err := json.Marshal(record)
				if err != nil || uint64(len(encoded))+1 > options.ReplayBounds.MaxRecordBytes {
					return errors.Join(errors.New("compact attempt source record exceeds its byte bound"), err)
				}
				if err := visit(record.Sequence, append(encoded, '\n')); err != nil {
					return err
				}
				if record.Disposition == AttemptDispositionComplete {
					cut.CompleteCount++
				} else if record.Disposition != AttemptDispositionPending {
					cut.FailedCount++
				}
				return nil
			})
		}
		cut.Records, _, err = WriteAttemptStreamV2(ctx, AttemptStreamV2Records, recordSource, AttemptStreamV2WriteOptions{Bounds: bounds.Records, MaxRowBytes: options.ReplayBounds.MaxRecordBytes, Spool: recordSpool, WriteData: options.WriteRecords, WriteMetadata: options.WriteMetadata})
		if err != nil {
			return nil, verified, err
		}
		proofSpool, err := scratch.openSpool(1, bounds.Proofs.MaxChunks)
		if err != nil {
			return nil, verified, err
		}
		proofSource := func(ctx context.Context, visit func(uint64, []byte) error) error {
			return walkAttemptCutV2SealPrefix(ctx, ledger, head, expected, depth, func(record AttemptRecord) error {
				if record.Disposition != AttemptDispositionComplete {
					return nil
				}
				encoded, err := json.Marshal(record.Proof)
				if err != nil || uint64(len(encoded))+1 > options.ReplayBounds.MaxProofBytes {
					return errors.Join(errors.New("compact attempt source proof exceeds its byte bound"), err)
				}
				return visit(record.Sequence, append(encoded, '\n'))
			})
		}
		cut.Proofs, _, err = WriteAttemptStreamV2(ctx, AttemptStreamV2Proofs, proofSource, AttemptStreamV2WriteOptions{Bounds: bounds.Proofs, MaxRowBytes: options.ReplayBounds.MaxProofBytes, Spool: proofSpool, WriteData: options.WriteProofs, WriteMetadata: options.WriteMetadata})
		if err != nil {
			return nil, verified, err
		}
		if err := scratch.step("before-fetch-back", "replay"); err != nil {
			return nil, verified, err
		}
		verified, err = replayAttemptCutV2Contents(ctx, cut, expected, bounds, replayOptions, depth, hooks.Replay, scratch.root)
		if err != nil {
			return nil, verified, err
		}
		if err := scratch.check(); err != nil {
			return nil, verified, err
		}
		if err := scratch.close(); err != nil {
			return nil, verified, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, verified, err
	}
	cut.Signature, err = cut.Sign(privateKey, bounds)
	if err != nil {
		return nil, verified, err
	}
	return &cut, verified, nil
}

// The local reader authenticates each bounded canonical VPK-signed record.
// Both passes additionally prove exactly the same policy, boundary and hash
// prefix. Full server/proof/lifecycle checks run against fetched staged bytes,
// never a producer-only in-memory projection. Appends after Head are excluded.
func walkAttemptCutV2SealPrefix(ctx context.Context, ledger *AttemptLedger, head AttemptLedgerHead, expected AttemptCutV2Context, depth int, visit func(AttemptRecord) error) error {
	previous, count := expected.PriorRoot, uint64(0)
	err := ledger.Walk(ctx, expected.FirstSequence, head.LastSequence, func(record AttemptRecord) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if record.Identity != expected.Identity || record.Sequence != expected.FirstSequence+count || record.PreviousHash != previous || record.M != depth {
			return fmt.Errorf("compact attempt record %d differs from the expected identity, sequence, prior root or policy depth", record.Sequence)
		}
		if record.Boundary.SettlementEpoch != expected.Boundary.SettlementEpoch || record.Boundary.EVMBlock > expected.Boundary.EVMBlock || record.Boundary.EVMBlock == expected.Boundary.EVMBlock && record.Boundary.EVMBlockHash != expected.Boundary.EVMBlockHash {
			return fmt.Errorf("compact attempt record %d differs from the expected cut boundary", record.Sequence)
		}
		if err := visit(record); err != nil {
			return err
		}
		previous = record.RecordHash
		count++
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	if count != head.LastSequence+1-expected.FirstSequence || previous != head.Root {
		return errors.New("compact attempt walked prefix differs from the checked head")
	}
	return ctx.Err()
}

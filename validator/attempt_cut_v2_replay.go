//go:build linux || darwin

package validator

// Public cut replay keeps one bounded descriptor page and one bounded record
// in memory. A fresh private scratch index holds interleaved trail state and
// the ordered proof projection; it is never a substitute for signed input.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/urnetwork/connect"
)

// A data reader must honor cancellation while opening, reading and closing.
// The replay additionally limits reads to the authenticated length plus one
// byte, then proves EOF. The kind is explicit; it is not a generic graph-cap
// exemption for manifests, descriptor pages or unrelated public artifacts.
type AttemptStreamV2DataOpener func(context.Context, string, string, uint64) (io.ReadCloser, error)

// All limits come from authenticated caller policy, not candidate metadata.
// Row byte limits include the canonical newline. Scratch limits independently
// bound physical file lengths/count; no trail-count-sized map is allocated.
type AttemptCutV2ReplayBounds struct {
	MaxRecordBytes  uint64
	MaxProofBytes   uint64
	MaxTrails       uint64
	MaxScratchBytes uint64
	MaxScratchFiles uint64
}

// Inputs must not be mutated during replay. VisitRecord receives owned data
// after its record authentication, but must stage any derived state until the
// entire replay succeeds: a later missing chunk or proof invalidates the cut.
// ScratchDirectory must not exist; replay closes but does not erase it, so its
// caller can preserve failed evidence or retire a successful scratch index.
type AttemptCutV2ReplayOptions struct {
	Bounds           AttemptCutV2ReplayBounds
	ScratchDirectory string
	ServerKeys       map[byte]ed25519.PublicKey
	ReadMetadata     AttemptStreamV2MetadataReader
	OpenData         AttemptStreamV2DataOpener
	VisitRecord      func(AttemptRecord) error
}

// A result is returned only after every byte and terminal outcome has been
// replayed, its proof projection matched, and all scratch resources closed.
// This proves cut contents, not historical on-chain membership or activation;
// those are independently authenticated inputs supplied by the outer reader.
type AttemptCutV2ReplayResult struct {
	Records             AttemptStreamV2Census
	Proofs              AttemptStreamV2Census
	CompleteCount       uint64
	FailedCount         uint64
	TrailCount          uint64
	ProofProjectionHash string
}

// Operation-local scheduling hooks expose ownership boundaries without
// substituting record authentication, storage operations or an acceptance.
type attemptCutV2ReplayHooks struct {
	ScratchCreated     func(string) error
	ScratchStorageStep func(string, string) error
	RecordDecoded      func()
	RecordChecked      func()
	RecordIndexed      func()
}

// Storage allocation and length conversions are checked before any fetch or
// filesystem mutation. One JSON row cannot consume a whole-history budget.
func (self AttemptCutV2ReplayBounds) validate(bounds AttemptCutV2Bounds) error {
	maximumInt := uint64(^uint(0) >> 1)
	maximumInt64 := uint64(1<<63 - 1)
	if self.MaxRecordBytes == 0 || self.MaxProofBytes == 0 || self.MaxRecordBytes > maximumInt/8 || self.MaxProofBytes > maximumInt/8 || self.MaxRecordBytes > bounds.Records.MaxChunkBytes || self.MaxProofBytes > bounds.Proofs.MaxChunkBytes || self.MaxTrails == 0 || self.MaxTrails > bounds.Records.MaxItems || self.MaxScratchBytes <= attemptStoreMetadataReserve || self.MaxScratchFiles < 8 || bounds.Records.MaxChunkBytes >= maximumInt64 || bounds.Proofs.MaxChunkBytes >= maximumInt64 || bounds.Records.MaxItems > maximumInt {
		return errors.New("compact attempt replay bounds are incomplete or inconsistent")
	}
	return nil
}

// Applies real record, server ASSIGN, FINAL/EXTEND and validator signature
// verification before any record can contribute to the staged result.
func decodeAttemptCutV2Record(raw []byte, limit uint64, identity AttemptLedgerIdentity, vpk ed25519.PublicKey, keys map[byte]ed25519.PublicKey, requireServerKeys bool) (AttemptRecord, error) {
	var record AttemptRecord
	if uint64(len(raw)) > limit || len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return record, errors.New("compact attempt record is oversized or lacks its canonical newline")
	}
	if err := attemptStoreDecode(raw[:len(raw)-1], &record); err != nil {
		return record, err
	}
	if err := verifyAttemptRecord(&record, identity, vpk, keys, requireServerKeys); err != nil {
		return record, err
	}
	return record, nil
}

// A chunk is neither trusted nor accepted until exact EOF, byte count and
// hash match. Records cannot cross chunk boundaries. The row limit is checked
// before allocation/decoding; extra rows are refused before decoding them.
func walkAttemptStreamV2Chunk(ctx context.Context, kind string, chunk AttemptStreamV2Chunk, maxRowBytes uint64, open AttemptStreamV2DataOpener, visit func(uint64, []byte) error) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	reader, err := open(ctx, kind, chunk.ContentHash, chunk.DataBytes)
	if reader != nil {
		defer func() { resultErr = errors.Join(resultErr, reader.Close(), ctx.Err()) }()
	}
	if err != nil {
		return errors.Join(err, ctx.Err())
	}
	if reader == nil {
		return errors.New("compact attempt chunk reader is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	digest := sha256.New()
	buffer := bufio.NewReaderSize(io.TeeReader(io.LimitReader(reader, int64(chunk.DataBytes)+1), digest), 4096)
	var dataBytes uint64
	for index := uint64(0); index < chunk.ItemCount; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row []byte
		for {
			fragment, readErr := buffer.ReadSlice('\n')
			if err := ctx.Err(); err != nil {
				return err
			}
			if uint64(len(fragment)) > maxRowBytes-uint64(len(row)) {
				return errors.New("compact attempt stream row exceeds its byte bound")
			}
			row = append(row, fragment...)
			if readErr == nil {
				break
			}
			if !errors.Is(readErr, bufio.ErrBufferFull) {
				return errors.Join(errors.New("compact attempt chunk ends before its complete JSONL rows"), readErr)
			}
		}
		if uint64(len(row)) > chunk.DataBytes-dataBytes {
			return errors.New("compact attempt chunk exceeds its authenticated byte count")
		}
		dataBytes += uint64(len(row))
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := visit(index, row); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := buffer.ReadByte(); !errors.Is(err, io.EOF) {
		return errors.Join(errors.New("compact attempt chunk contains an extra row or suffix"), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if dataBytes != chunk.DataBytes || attemptHex32(*(*[32]byte)(digest.Sum(nil))) != chunk.ContentHash {
		return errors.New("compact attempt chunk byte count or content hash differs")
	}
	return ctx.Err()
}

// Only the backend fault callback is concurrent; all index methods belong to
// one replay. A completed trail occupies a fixed marker, not a retained record.
type attemptCutV2ReplayScratch struct {
	stateLock sync.Mutex
	fault     error
	db        *leveldb.DB
	disk      *attemptRecordStoreStorage
	trails    uint64
	pending   uint64
}

// Reuses the qualified descriptor-relative, private, bounded storage adapter.
// The fresh-directory requirement prevents reuse of partially accepted input
// or confusion with a durable producer ledger, even when the cut is replayed.
func newAttemptCutV2ReplayScratch(ctx context.Context, path string, bounds AttemptCutV2ReplayBounds, hooks attemptCutV2ReplayHooks) (result *attemptCutV2ReplayScratch, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return nil, errors.New("compact attempt scratch path is not a clean absolute directory")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close(), ctx.Err())
		if resultErr != nil && result != nil {
			resultErr = errors.Join(resultErr, result.close())
			result = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	if err := root.Mkdir(name, 0o700); err != nil {
		return nil, fmt.Errorf("compact attempt scratch must be a fresh directory: %w", err)
	}
	anchor, err := root.Lstat(name)
	if err != nil || !attemptStorePrivateDirectory(anchor) {
		return nil, errors.Join(errors.New("compact attempt scratch is not private and owned"), err)
	}
	createdDirectory, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, createdDirectory.Close()) }()
	opened, err := createdDirectory.Stat()
	if err != nil || !attemptStorePrivateDirectory(opened) || !os.SameFile(anchor, opened) {
		return nil, errors.Join(errors.New("compact attempt scratch changed during creation"), err)
	}
	if hooks.ScratchCreated != nil {
		if err := hooks.ScratchCreated(filepath.Join(parent, name)); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self := &attemptCutV2ReplayScratch{}
	storageHooks := attemptRecordStoreHooks{Step: func(operation, name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hooks.ScratchStorageStep != nil {
			if err := hooks.ScratchStorageStep(operation, name); err != nil {
				return err
			}
		}
		return ctx.Err()
	}}
	self.disk, err = openAttemptRecordStoreStorageAtWithAnchor(root, filepath.Join(parent, name), opened, attemptRecordStoreBounds{MaxStorageBytes: bounds.MaxScratchBytes, MaxStorageFiles: bounds.MaxScratchFiles}, storageHooks, func(err error) {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.fault == nil {
			self.fault = err
		}
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, self.disk.Close())
	}
	self.db, err = leveldb.Open(self.disk, &opt.Options{Strict: opt.StrictAll, BlockCacheCapacity: 8 * 1024 * 1024, WriteBuffer: 4 * 1024 * 1024, CompactionTableSize: 2 * 1024 * 1024, CompactionTableSizeMultiplier: 1, OpenFilesCacheCapacity: 64, DisableLargeBatchTransaction: true})
	if err != nil {
		return nil, errors.Join(err, self.disk.Close())
	}
	iterator := self.db.NewIterator(nil, &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	nonempty, iterateErr := iterator.Next(), iterator.Error()
	iterator.Release()
	if nonempty || iterateErr != nil {
		return nil, errors.Join(errors.New("compact attempt scratch was not empty"), iterateErr, self.close())
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, self.close())
	}
	return self, nil
}

// Observes asynchronous compaction/storage failures before accepting a result.
func (self *attemptCutV2ReplayScratch) check() error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.fault
}

// Closes and joins the backend and retains any earlier asynchronous error.
func (self *attemptCutV2ReplayScratch) close() error {
	err := self.db.Close()
	return errors.Join(err, self.disk.Close(), self.check())
}

// Ordinal proof keys are independent of gapped original ledger sequences.
func attemptCutV2ProofIndexKey(ordinal uint64) []byte {
	key := make([]byte, len("proof/")+8)
	copy(key, "proof/")
	binary.BigEndian.PutUint64(key[len("proof/"):], ordinal)
	return key
}

// Validates the whole per-trail lifecycle with one point lookup. Previous
// pending records remain signed; terminal markers forbid all later reuse.
// The proof index atomically binds its exact canonical bytes and source row.
func (self *attemptCutV2ReplayScratch) append(ctx context.Context, record AttemptRecord, raw, proof []byte, proofOrdinal uint64, identity AttemptLedgerIdentity, vpk ed25519.PublicKey, bounds AttemptCutV2ReplayBounds, index int, checked func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	key := append([]byte("trail/"), record.TrailID[:]...)
	priorBytes, err := self.db.Get(key, &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	newTrail := errors.Is(err, leveldb.ErrNotFound)
	if err != nil && !newTrail {
		return err
	}
	if newTrail && self.trails >= bounds.MaxTrails {
		return errors.New("compact attempt replay exceeds its trail bound")
	}
	pending := map[connect.Id]AttemptRecord{}
	terminal := map[connect.Id]bool{}
	if !newTrail {
		if len(priorBytes) == 1 && priorBytes[0] == 1 {
			terminal[record.TrailID] = true
		} else if len(priorBytes) > 1 && priorBytes[0] == 0 {
			prior, err := decodeAttemptCutV2Record(priorBytes[1:], bounds.MaxRecordBytes, identity, vpk, nil, false)
			if err != nil || prior.TrailID != record.TrailID || prior.Disposition != AttemptDispositionPending || prior.Sequence >= record.Sequence {
				return errors.Join(errors.New("compact attempt scratch pending record differs"), err)
			}
			pending[record.TrailID] = prior
		} else {
			return errors.New("compact attempt scratch trail state is invalid")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := attemptStoreCheckLifecycle(pending, terminal, record, index); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	batch := new(leveldb.Batch)
	if record.Disposition == AttemptDispositionPending {
		batch.Put(key, append([]byte{0}, raw...))
	} else {
		batch.Put(key, []byte{1})
	}
	if proof != nil {
		proofHash := sha256.Sum256(proof)
		value := make([]byte, 8+len(proofHash))
		binary.BigEndian.PutUint64(value, record.Sequence)
		copy(value[8:], proofHash[:])
		batch.Put(attemptCutV2ProofIndexKey(proofOrdinal), value)
	}
	if checked != nil {
		checked()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := self.db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if newTrail {
		self.trails++
		self.pending++
	}
	if record.Disposition != AttemptDispositionPending {
		self.pending--
	}
	return self.check()
}

// Matches proof bytes individually to the signed record-derived projection;
// a proof cannot borrow another row's sequence coordinates or ordinal.
func (self *attemptCutV2ReplayScratch) proof(ctx context.Context, ordinal uint64, raw []byte) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := self.check(); err != nil {
		return 0, err
	}
	value, err := self.db.Get(attemptCutV2ProofIndexKey(ordinal), &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	if cancelErr := ctx.Err(); cancelErr != nil {
		return 0, errors.Join(err, cancelErr)
	}
	hash := sha256.Sum256(raw)
	if err != nil || len(value) != 40 || !bytes.Equal(value[8:], hash[:]) {
		return 0, errors.Join(errors.New("compact attempt proof differs from its signed record projection"), err)
	}
	return binary.BigEndian.Uint64(value), nil
}

// Replays the complete authenticated record and proof streams. No successful
// result survives a late digest/census failure, cancellation or close error.
// This is a read-only public-data verifier apart from its new scratch index;
// it neither activates producers nor writes an on-chain acceptance decision.
func ReplayAttemptCutV2(ctx context.Context, cut AttemptCutV2, expected AttemptCutV2Context, bounds AttemptCutV2Bounds, options AttemptCutV2ReplayOptions) (result AttemptCutV2ReplayResult, resultErr error) {
	return replayAttemptCutV2WithHooks(ctx, cut, expected, bounds, options, attemptCutV2ReplayHooks{})
}

// Runs the same public replay with deterministic operation-owned boundaries.
func replayAttemptCutV2WithHooks(ctx context.Context, cut AttemptCutV2, expected AttemptCutV2Context, bounds AttemptCutV2Bounds, options AttemptCutV2ReplayOptions, hooks attemptCutV2ReplayHooks) (result AttemptCutV2ReplayResult, resultErr error) {
	if ctx == nil || options.ReadMetadata == nil || options.OpenData == nil {
		return result, errors.New("compact attempt replay authority is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := cut.VerifyHeader(expected, bounds); err != nil {
		return result, err
	}
	if err := options.Bounds.validate(bounds); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = AttemptCutV2ReplayResult{}
		}
	}()
	if cut.RecordCount == 0 {
		result.ProofProjectionHash = attemptHex32(sha256.Sum256(nil))
		return result, ctx.Err()
	}
	scratch, err := newAttemptCutV2ReplayScratch(ctx, options.ScratchDirectory, options.Bounds, hooks)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, scratch.close()) }()
	vpk, err := canonicalAttemptHex32("compact attempt validator vpk", expected.Identity.ValidatorVPK, false)
	if err != nil {
		return result, err
	}
	previousHash := expected.PriorRoot
	var recordCount uint64
	expectedProofHash := sha256.New()
	result.Records, err = WalkAttemptStreamV2Descriptors(ctx, AttemptStreamV2Records, cut.Records, bounds.Records, options.ReadMetadata, func(chunk AttemptStreamV2Chunk) error {
		return walkAttemptStreamV2Chunk(ctx, AttemptStreamV2Records, chunk, options.Bounds.MaxRecordBytes, options.OpenData, func(index uint64, raw []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			record, err := decodeAttemptCutV2Record(raw, options.Bounds.MaxRecordBytes, expected.Identity, vpk[:], options.ServerKeys, true)
			if err != nil {
				return err
			}
			if hooks.RecordDecoded != nil {
				hooks.RecordDecoded()
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if recordCount >= cut.RecordCount || record.Sequence != expected.FirstSequence+recordCount || record.Sequence != chunk.FirstSequence+index || record.PreviousHash != previousHash || record.Boundary.SettlementEpoch != expected.Boundary.SettlementEpoch || record.Boundary.EVMBlock > expected.Boundary.EVMBlock || record.Boundary.EVMBlock == expected.Boundary.EVMBlock && record.Boundary.EVMBlockHash != expected.Boundary.EVMBlockHash {
				return errors.New("compact attempt record breaks its chain, descriptor range or boundary")
			}
			var proof []byte
			if record.Disposition == AttemptDispositionComplete {
				proof, err = json.Marshal(record.Proof)
				if err != nil || uint64(len(proof))+1 > options.Bounds.MaxProofBytes {
					return errors.Join(errors.New("compact attempt projected proof exceeds its byte bound"), err)
				}
				proof = append(proof, '\n')
			}
			if err := scratch.append(ctx, record, raw, proof, result.CompleteCount, expected.Identity, vpk[:], options.Bounds, int(recordCount), hooks.RecordChecked); err != nil {
				return err
			}
			if hooks.RecordIndexed != nil {
				hooks.RecordIndexed()
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if proof != nil {
				_, _ = expectedProofHash.Write(proof)
				result.CompleteCount++
			} else if record.Disposition != AttemptDispositionPending {
				result.FailedCount++
			}
			recordCount++
			previousHash = record.RecordHash
			if err := ctx.Err(); err != nil {
				return err
			}
			if options.VisitRecord != nil {
				return errors.Join(options.VisitRecord(record), ctx.Err())
			}
			return nil
		})
	})
	if err != nil {
		return result, err
	}
	if recordCount != cut.RecordCount || result.Records.FirstSequence != expected.FirstSequence || result.Records.LastSequence != cut.LastSequence || previousHash != cut.Root || result.CompleteCount != cut.CompleteCount || result.FailedCount != cut.FailedCount || scratch.pending != 0 {
		return result, errors.New("compact attempt record root, complete census or pending lifecycle differs")
	}
	var proofCount uint64
	actualProofHash := sha256.New()
	result.Proofs, err = WalkAttemptStreamV2Descriptors(ctx, AttemptStreamV2Proofs, cut.Proofs, bounds.Proofs, options.ReadMetadata, func(chunk AttemptStreamV2Chunk) error {
		return walkAttemptStreamV2Chunk(ctx, AttemptStreamV2Proofs, chunk, options.Bounds.MaxProofBytes, options.OpenData, func(index uint64, raw []byte) error {
			sequence, err := scratch.proof(ctx, proofCount, raw)
			if err != nil {
				return err
			}
			if index == 0 && sequence != chunk.FirstSequence || index == chunk.ItemCount-1 && sequence != chunk.LastSequence || sequence < chunk.FirstSequence || sequence > chunk.LastSequence {
				return errors.New("compact attempt proof descriptor differs from its original record sequence")
			}
			_, _ = actualProofHash.Write(raw)
			proofCount++
			return nil
		})
	})
	if err != nil {
		return result, err
	}
	if proofCount != result.CompleteCount || !bytes.Equal(expectedProofHash.Sum(nil), actualProofHash.Sum(nil)) {
		return result, errors.New("compact attempt complete proof projection differs")
	}
	result.TrailCount = scratch.trails
	result.ProofProjectionHash = attemptHex32(*(*[32]byte)(actualProofHash.Sum(nil)))
	return result, scratch.check()
}

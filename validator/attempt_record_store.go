//go:build linux || darwin

package validator

// This private store preserves canonical v1 records without retaining the
// history in memory. It is not wired into producers or public cut schemas.
// Record bytes, an immutable per-trail sequence index, final trail state and
// the head commit atomically. Local checks retain the existing ledger's VPK
// signature trust level; public replay must still verify server signatures.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/util"
	"github.com/urnetwork/connect"
)

var (
	errAttemptRecordStoreClosed  = errors.New("attempt record store is closing or closed")
	errAttemptRecordStoreFaulted = errors.New("attempt record store requires reopen after a persistence or integrity failure")
	errAttemptRecordStoreLimit   = errors.New("attempt record store limit exceeded")
)

const (
	attemptStoreSchema            = "urnetwork-private-attempt-record-store-v1"
	attemptStoreRecordPrefix      = "record/"
	attemptStoreTrailPrefix       = "trail/"
	attemptStoreTrailRecordPrefix = "trail-record/"
	attemptStoreMetadataReserve   = 4096
)

// Limits are explicit caller-owned resource bounds, not activated protocol
// limits. MaxRecordBytes bounds one canonical record; MaxRawRecordBytes bounds
// their aggregate. MaxStorageBytes bounds file lengths plus reserved metadata,
// not filesystem allocation blocks or an unmeasured disk-amplification ratio.
type attemptRecordStoreBounds struct {
	MaxRecordBytes    uint64
	MaxRecordCount    uint64
	MaxTrailCount     uint64
	MaxRawRecordBytes uint64
	MaxStorageBytes   uint64
	MaxStorageFiles   uint64
}

// These counters describe one complete committed prefix, never a pending batch.
type attemptRecordStoreHead = AttemptLedgerHead

// The coordinator is local namespace context; unchanged v1 records do not
// acquire a new signed field. Future public v2 headers bind it independently.
type attemptRecordStoreIdentity struct {
	Schema      string                `json:"schema"`
	Identity    AttemptLedgerIdentity `json:"identity"`
	Coordinator string                `json:"coordinator"`
}

// Only the last sequence is needed to recover an exact pending checkpoint.
type attemptRecordStoreTrail struct {
	LastSequence uint64 `json:"last_sequence"`
	RecordHash   string `json:"record_hash"`
	Terminal     bool   `json:"terminal"`
}

// Hooks are a private deterministic storage-fault seam. They must not call
// store methods. Production uses nil; data and visitor callbacks never use it.
type attemptRecordStoreHooks struct {
	Step func(operation, name string) error
}

// Methods are safe concurrently. Close cancels and joins active operations
// and backend workers. A visitor may call Read/Head, but must return promptly
// and must not call Close or start another Walk/Pending on this same store.
type attemptRecordStore struct {
	stateLock  sync.Mutex
	appendGate chan struct{}
	active     sync.WaitGroup
	closing    bool
	fault      error
	closeError error
	closeDone  chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	walkGate   chan struct{}
	identity   attemptRecordStoreIdentity
	vpk        ed25519.PublicKey
	bounds     attemptRecordStoreBounds
	head       attemptRecordStoreHead
	db         *leveldb.DB
	disk       *attemptRecordStoreStorage
}

// Opens a single-owner private namespace. Reopen verifies records and the
// entire exact index bijection; corrupt state is never silently recovered.
func openAttemptRecordStore(ctx context.Context, path string, identity AttemptLedgerIdentity, coordinator string, vpk ed25519.PublicKey, bounds attemptRecordStoreBounds) (*attemptRecordStore, error) {
	return openAttemptRecordStoreWithHooks(ctx, path, identity, coordinator, vpk, bounds, attemptRecordStoreHooks{})
}

// Adds deterministic failure points without changing the production path.
func openAttemptRecordStoreWithHooks(ctx context.Context, path string, identity AttemptLedgerIdentity, coordinator string, vpk ed25519.PublicKey, bounds attemptRecordStoreBounds, hooks attemptRecordStoreHooks) (*attemptRecordStore, error) {
	return openAttemptRecordStoreWithParent(ctx, path, identity, coordinator, vpk, bounds, hooks, nil)
}

// Migration passes its already opened parent root; no pathname re-resolution
// can redirect the database away from the state-directory migration gate.
func openAttemptRecordStoreWithParent(ctx context.Context, path string, identity AttemptLedgerIdentity, coordinator string, vpk ed25519.PublicKey, bounds attemptRecordStoreBounds, hooks attemptRecordStoreHooks, parent *os.Root) (*attemptRecordStore, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, errors.New("attempt record store opening context is unavailable")
	}
	if len(vpk) != ed25519.PublicKeySize || validateAttemptLedgerIdentity(identity, vpk) != nil {
		return nil, errors.New("attempt record store identity is invalid")
	}
	address, err := hex.DecodeString(strings.TrimPrefix(coordinator, "0x"))
	if err != nil || len(address) != 20 || coordinator != "0x"+hex.EncodeToString(address) || bytes.Equal(address, make([]byte, 20)) {
		return nil, errors.New("attempt record store coordinator is not canonical")
	}
	if bounds.MaxRecordBytes == 0 || bounds.MaxRecordBytes > uint64(^uint(0)>>1)/8 || bounds.MaxRecordCount == 0 || bounds.MaxRecordCount > uint64(^uint(0)>>1) || bounds.MaxTrailCount == 0 || bounds.MaxTrailCount > bounds.MaxRecordCount || bounds.MaxRawRecordBytes < bounds.MaxRecordBytes || bounds.MaxStorageBytes <= attemptStoreMetadataReserve || bounds.MaxStorageFiles < 8 || uint64(len(identity.DeploymentID)) > bounds.MaxRecordBytes {
		return nil, errors.New("attempt record store bounds are incomplete or inconsistent")
	}
	storeCtx, cancel := context.WithCancel(context.Background())
	self := &attemptRecordStore{
		ctx: storeCtx, cancel: cancel, closeDone: make(chan struct{}), walkGate: make(chan struct{}, 1), appendGate: make(chan struct{}, 1),
		identity: attemptRecordStoreIdentity{Schema: attemptStoreSchema, Identity: identity, Coordinator: coordinator},
		vpk:      append(ed25519.PublicKey(nil), vpk...), bounds: bounds,
	}
	var disk *attemptRecordStoreStorage
	if parent == nil {
		disk, err = openAttemptRecordStoreStorage(path, bounds, hooks, self.latchFault)
	} else {
		disk, err = openAttemptRecordStoreStorageAt(parent, path, bounds, hooks, self.latchFault)
	}
	if err != nil {
		cancel()
		return nil, err
	}
	self.disk = disk
	db, err := leveldb.Open(disk, &opt.Options{
		Strict: opt.StrictAll, BlockCacheCapacity: 8 * 1024 * 1024, WriteBuffer: 4 * 1024 * 1024,
		CompactionTableSize: 2 * 1024 * 1024, CompactionTableSizeMultiplier: 1,
		OpenFilesCacheCapacity: 64, DisableLargeBatchTransaction: true,
	})
	if err != nil {
		cancel()
		return nil, errors.Join(err, disk.Close())
	}
	self.db = db
	if err := self.openContents(ctx); err != nil {
		return nil, errors.Join(err, self.Close())
	}
	return self, nil
}

// A storage error can arrive from a backend worker after the immediate call.
func (self *attemptRecordStore) latchFault(err error) {
	if err == nil {
		return
	}
	self.stateLock.Lock()
	if self.fault == nil {
		self.fault = err
	}
	self.stateLock.Unlock()
}

// Admission and Close share this lock, so Wait cannot race a later Add.
func (self *attemptRecordStore) begin(ctx context.Context) error {
	if ctx == nil {
		return errors.New("attempt record store context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closing {
		return errAttemptRecordStoreClosed
	}
	if self.fault != nil {
		return errors.Join(errAttemptRecordStoreFaulted, self.fault)
	}
	self.active.Add(1)
	return nil
}

// Cancelling a caller before mutation is not an ambiguous persistence error.
func (self *attemptRecordStore) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closing {
		return errAttemptRecordStoreClosed
	}
	if self.fault != nil {
		return errors.Join(errAttemptRecordStoreFaulted, self.fault)
	}
	return nil
}

// A queued append/snapshot can cancel without waiting for the current disk
// write. The contention seam lets tests prove the actual gate, not scheduling.
func (self *attemptRecordStore) acquireAppend(ctx context.Context, operation string) error {
	select {
	case self.appendGate <- struct{}{}:
	default:
		if self.disk.hooks.Step != nil {
			if err := self.disk.step(operation+"-contended", ""); err != nil {
				return err
			}
		}
		select {
		case self.appendGate <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		case <-self.ctx.Done():
			return errAttemptRecordStoreClosed
		}
	}
	if err := self.check(ctx); err != nil {
		<-self.appendGate
		return err
	}
	return nil
}

// Canonical sequence keys preserve ordering without decimal-width ambiguity.
func attemptStoreRecordKey(sequence uint64) []byte {
	key := make([]byte, len(attemptStoreRecordPrefix)+8)
	copy(key, attemptStoreRecordPrefix)
	binary.BigEndian.PutUint64(key[len(attemptStoreRecordPrefix):], sequence)
	return key
}

// Trail keys preserve the full identifier, never a probabilistic membership bit.
func attemptStoreTrailKey(trailID connect.Id) []byte {
	return append([]byte(attemptStoreTrailPrefix), trailID[:]...)
}

// An immutable per-record marker permits bounded exact lifecycle replay.
func attemptStoreTrailRecordKey(trailID connect.Id, sequence uint64) []byte {
	key := append([]byte(attemptStoreTrailRecordPrefix), trailID[:]...)
	return binary.BigEndian.AppendUint64(key, sequence)
}

// Rejects unknown fields, alternate JSON encodings and trailing documents.
func attemptStoreDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("attempt record store value has trailing JSON")
	}
	canonical, err := json.Marshal(target)
	if err != nil || !bytes.Equal(raw, canonical) {
		return errors.New("attempt record store value is not canonical")
	}
	return nil
}

// A pending extension cannot change the pinned boundary while retaining the
// same trail. This also makes the complete indexed lifecycle check symmetric.
func attemptStoreCheckLifecycle(pending map[connect.Id]AttemptRecord, terminal map[connect.Id]bool, record AttemptRecord, index int) error {
	if prior, exists := pending[record.TrailID]; exists && (record.Boundary != prior.Boundary || record.M != prior.M || !bytes.Equal(record.ServerNonce, prior.ServerNonce)) {
		return errors.New("attempt record store checkpoint changed its pinned identity")
	}
	return validateAttemptLifecycleRecord(pending, terminal, record, index)
}

// Shape checks bound serialization before json.Marshal allocates its buffer.
// DeploymentID may require six output bytes per input byte when JSON-escaped;
// this bounded factor precedes the exact MaxRecordBytes check. These checks
// do not replace the subsequent record hash and signature verification.
func (self *attemptRecordStore) encodeRecord(record AttemptRecord) ([]byte, error) {
	if record.Identity != self.identity.Identity || record.M < connect.VerifyMMin || record.M > connect.VerifyMMax || len(record.Assignments) == 0 || len(record.Assignments) >= record.M || len(record.ServerNonce) != connect.VerifyNonceSize || len(record.VPK) != ed25519.PublicKeySize || len(record.Signature) != ed25519.SignatureSize || len(record.RecordHash) != 66 || len(record.PreviousHash) != 66 || len(record.Boundary.EVMBlockHash) != 66 || len(record.Schema) > 128 || len(record.Disposition) > 64 {
		return nil, errors.New("attempt record store record shape is invalid")
	}
	for _, assignment := range record.Assignments {
		if len(assignment.Trail) >= record.M || len(assignment.AssignMessage) > 1024 || len(assignment.AssignSignature) != ed25519.SignatureSize || len(assignment.Binding.FleetID) != 66 || len(assignment.Binding.Hotkey) != 66 {
			return nil, errors.New("attempt record store assignment shape is invalid")
		}
	}
	if proof := record.Proof; proof != nil {
		if len(proof.Hops) != record.M || len(proof.ServerNonce) != connect.VerifyNonceSize || len(proof.Vpk) != ed25519.PublicKeySize || len(proof.FinalSig) != ed25519.SignatureSize || len(proof.VerifierSig) != ed25519.SignatureSize || len(proof.VpkSig) != ed25519.SignatureSize || len(proof.FinalDigest) != 32 || len(proof.PathId) != 32 {
			return nil, errors.New("attempt record store proof shape is invalid")
		}
	}
	raw, err := json.Marshal(&record)
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) > self.bounds.MaxRecordBytes {
		return nil, fmt.Errorf("%w: per-record bytes", errAttemptRecordStoreLimit)
	}
	return raw, nil
}

// Returned records own their byte slices and do not alias an iterator or caller.
func (self *attemptRecordStore) decodeRecord(raw []byte) (AttemptRecord, error) {
	var record AttemptRecord
	if uint64(len(raw)) > self.bounds.MaxRecordBytes {
		return record, fmt.Errorf("%w: stored record bytes", errAttemptRecordStoreLimit)
	}
	if err := attemptStoreDecode(raw, &record); err != nil {
		return record, err
	}
	if _, err := self.encodeRecord(record); err != nil {
		return record, err
	}
	if err := verifyAttemptRecord(&record, self.identity.Identity, self.vpk, nil, false); err != nil {
		return record, err
	}
	if self.disk.hooks.Step != nil {
		if err := self.disk.step("decode-record", strconv.Itoa(len(raw))); err != nil {
			return record, err
		}
	}
	return record, nil
}

// Missing an in-range record is corruption, not an empty successful read.
func (self *attemptRecordStore) readRecord(sequence uint64) (AttemptRecord, error) {
	raw, err := self.db.Get(attemptStoreRecordKey(sequence), &opt.ReadOptions{DontFillCache: true, Strict: opt.StrictAll})
	if err != nil {
		return AttemptRecord{}, err
	}
	record, err := self.decodeRecord(raw)
	if err == nil && record.Sequence != sequence {
		err = errors.New("attempt record store key and sequence differ")
	}
	return record, err
}

// Initializes only a genuinely empty database; partial metadata is corruption.
func (self *attemptRecordStore) openContents(ctx context.Context) error {
	raw, err := self.db.Get([]byte("identity"), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		iterator := self.db.NewIterator(nil, &opt.ReadOptions{DontFillCache: true, Strict: opt.StrictAll})
		nonempty := iterator.Next()
		iterateErr := iterator.Error()
		iterator.Release()
		if nonempty || iterateErr != nil {
			return errors.New("attempt record store metadata is missing from nonempty state")
		}
		identityBytes, err := json.Marshal(self.identity)
		if err != nil {
			return err
		}
		self.head = attemptRecordStoreHead{Root: zeroAttemptHash()}
		headBytes, _ := json.Marshal(self.head)
		batch := new(leveldb.Batch)
		batch.Put([]byte("identity"), identityBytes)
		batch.Put([]byte("head"), headBytes)
		if err := self.db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
			self.latchFault(err)
			return err
		}
	} else {
		if err != nil {
			return err
		}
		var identity attemptRecordStoreIdentity
		if attemptStoreDecode(raw, &identity) != nil || identity != self.identity {
			return errors.New("attempt record store namespace differs")
		}
		raw, err = self.db.Get([]byte("head"), nil)
		if err != nil || attemptStoreDecode(raw, &self.head) != nil {
			return errors.New("attempt record store head is missing or invalid")
		}
	}
	if err := self.verifyContents(ctx); err != nil {
		return err
	}
	return self.check(ctx)
}

// Two ordered scans verify the global chain and each complete trail history
// without retaining either collection. Marker identity and exact counts make
// record -> marker -> terminal/pending state a bijection, not a count heuristic.
func (self *attemptRecordStore) verifyContents(ctx context.Context) error {
	if self.head.LastSequence > self.bounds.MaxRecordCount || self.head.RecordBytes > self.bounds.MaxRawRecordBytes || self.head.TrailCount > self.bounds.MaxTrailCount {
		return errAttemptRecordStoreLimit
	}
	readOptions := &opt.ReadOptions{DontFillCache: true, Strict: opt.StrictAll}
	iterator := self.db.NewIterator(util.BytesPrefix([]byte(attemptStoreRecordPrefix)), readOptions)
	var count, recordBytes uint64
	root := zeroAttemptHash()
	for iterator.Next() {
		if err := ctx.Err(); err != nil {
			iterator.Release()
			return err
		}
		if count >= self.head.LastSequence || !bytes.Equal(iterator.Key(), attemptStoreRecordKey(count+1)) {
			iterator.Release()
			return errors.New("attempt record store record sequence is incomplete")
		}
		raw := iterator.Value()
		record, err := self.decodeRecord(raw)
		if err != nil || record.Sequence != count+1 || record.PreviousHash != root || uint64(len(raw)) > self.bounds.MaxRawRecordBytes-recordBytes {
			iterator.Release()
			return errors.Join(errors.New("attempt record store record chain differs"), err)
		}
		root = record.RecordHash
		count++
		recordBytes += uint64(len(raw))
	}
	err := iterator.Error()
	iterator.Release()
	if err != nil || count != self.head.LastSequence || root != self.head.Root || recordBytes != self.head.RecordBytes {
		return errors.Join(errors.New("attempt record store head does not match records"), err)
	}
	iterator = self.db.NewIterator(util.BytesPrefix([]byte(attemptStoreTrailRecordPrefix)), readOptions)
	var prior AttemptRecord
	var indexed, trails uint64
	checkTrail := func() error {
		if prior.Sequence == 0 {
			return nil
		}
		stateBytes, err := self.db.Get(attemptStoreTrailKey(prior.TrailID), readOptions)
		var state attemptRecordStoreTrail
		if err != nil || attemptStoreDecode(stateBytes, &state) != nil || state != (attemptRecordStoreTrail{LastSequence: prior.Sequence, RecordHash: prior.RecordHash, Terminal: prior.Disposition != AttemptDispositionPending}) {
			return errors.New("attempt record store final trail state differs")
		}
		return nil
	}
	for iterator.Next() {
		if err := ctx.Err(); err != nil {
			iterator.Release()
			return err
		}
		key := iterator.Key()
		if indexed >= count || len(key) != len(attemptStoreTrailRecordPrefix)+24 {
			iterator.Release()
			return errors.New("attempt record store trail marker key is invalid")
		}
		var trailID connect.Id
		copy(trailID[:], key[len(attemptStoreTrailRecordPrefix):])
		sequence := binary.BigEndian.Uint64(key[len(key)-8:])
		record, err := self.readRecord(sequence)
		if err != nil || record.TrailID != trailID || !bytes.Equal(iterator.Value(), []byte(record.RecordHash)) {
			iterator.Release()
			return errors.Join(errors.New("attempt record store marker does not identify its signed record"), err)
		}
		pending := map[connect.Id]AttemptRecord{}
		terminal := map[connect.Id]bool{}
		if prior.Sequence != 0 && prior.TrailID == trailID {
			if prior.Disposition == AttemptDispositionPending {
				pending[trailID] = prior
			} else {
				terminal[trailID] = true
			}
		} else {
			if err := checkTrail(); err != nil {
				iterator.Release()
				return err
			}
			trails++
		}
		if err := attemptStoreCheckLifecycle(pending, terminal, record, int(indexed)); err != nil {
			iterator.Release()
			return err
		}
		prior = record
		indexed++
	}
	err = iterator.Error()
	iterator.Release()
	if err != nil || indexed != count || trails != self.head.TrailCount {
		return errors.Join(errors.New("attempt record store trail marker census differs"), err)
	}
	if err := checkTrail(); err != nil {
		return err
	}
	iterator = self.db.NewIterator(nil, readOptions)
	var recordKeys, markerKeys, trailKeys, metadataKeys uint64
	for iterator.Next() {
		if err := ctx.Err(); err != nil {
			iterator.Release()
			return err
		}
		key := iterator.Key()
		switch {
		case bytes.Equal(key, []byte("identity")), bytes.Equal(key, []byte("head")):
			metadataKeys++
		case bytes.HasPrefix(key, []byte(attemptStoreRecordPrefix)) && len(key) == len(attemptStoreRecordPrefix)+8:
			recordKeys++
		case bytes.HasPrefix(key, []byte(attemptStoreTrailRecordPrefix)) && len(key) == len(attemptStoreTrailRecordPrefix)+24:
			markerKeys++
		case bytes.HasPrefix(key, []byte(attemptStoreTrailPrefix)) && len(key) == len(attemptStoreTrailPrefix)+16:
			trailKeys++
		default:
			iterator.Release()
			return errors.New("attempt record store contains an unknown key")
		}
		if recordKeys > count || markerKeys > count || trailKeys > trails {
			iterator.Release()
			return errors.New("attempt record store has extra index keys")
		}
	}
	err = iterator.Error()
	iterator.Release()
	if err != nil || metadataKeys != 2 || recordKeys != count || markerKeys != count || trailKeys != trails {
		return errors.Join(errors.New("attempt record store key census differs"), err)
	}
	return nil
}

// Invalid input and pre-write limits leave the store usable. Any DB Write
// error may have persisted; it faults this owner until explicit reopen.
func (self *attemptRecordStore) Append(ctx context.Context, record AttemptRecord) error {
	if err := self.begin(ctx); err != nil {
		return err
	}
	defer self.active.Done()
	if err := self.acquireAppend(ctx, "append"); err != nil {
		return err
	}
	defer func() { <-self.appendGate }()
	raw, err := self.encodeRecord(record)
	if err != nil {
		return err
	}
	owned, err := self.decodeRecord(raw)
	if err != nil {
		return err
	}
	self.stateLock.Lock()
	head := self.head
	self.stateLock.Unlock()
	if head.LastSequence == ^uint64(0) || owned.Sequence != head.LastSequence+1 || owned.PreviousHash != head.Root {
		return errors.New("attempt record store append does not extend the head")
	}
	if head.LastSequence >= self.bounds.MaxRecordCount || uint64(len(raw)) > self.bounds.MaxRawRecordBytes-head.RecordBytes {
		return errAttemptRecordStoreLimit
	}
	pending := map[connect.Id]AttemptRecord{}
	terminal := map[connect.Id]bool{}
	stateBytes, err := self.db.Get(attemptStoreTrailKey(owned.TrailID), nil)
	newTrail := errors.Is(err, leveldb.ErrNotFound)
	if !newTrail {
		var state attemptRecordStoreTrail
		if err != nil || attemptStoreDecode(stateBytes, &state) != nil {
			self.latchFault(errors.New("attempt record store trail state is unreadable"))
			return self.check(ctx)
		}
		prior, err := self.readRecord(state.LastSequence)
		if err != nil || prior.TrailID != owned.TrailID || prior.RecordHash != state.RecordHash || state.Terminal != (prior.Disposition != AttemptDispositionPending) {
			self.latchFault(errors.New("attempt record store trail state and record differ"))
			return self.check(ctx)
		}
		if state.Terminal {
			terminal[owned.TrailID] = true
		} else {
			pending[owned.TrailID] = prior
		}
	} else if head.TrailCount >= self.bounds.MaxTrailCount {
		return errAttemptRecordStoreLimit
	}
	if err := attemptStoreCheckLifecycle(pending, terminal, owned, int(head.LastSequence)); err != nil {
		return err
	}
	head.LastSequence = owned.Sequence
	head.Root = owned.RecordHash
	head.RecordBytes += uint64(len(raw))
	if newTrail {
		head.TrailCount++
	}
	stateBytes, _ = json.Marshal(attemptRecordStoreTrail{LastSequence: owned.Sequence, RecordHash: owned.RecordHash, Terminal: owned.Disposition != AttemptDispositionPending})
	headBytes, _ := json.Marshal(head)
	batch := new(leveldb.Batch)
	batch.Put(attemptStoreRecordKey(owned.Sequence), raw)
	batch.Put(attemptStoreTrailRecordKey(owned.TrailID, owned.Sequence), []byte(owned.RecordHash))
	batch.Put(attemptStoreTrailKey(owned.TrailID), stateBytes)
	batch.Put([]byte("head"), headBytes)
	if err := self.check(ctx); err != nil {
		return err
	}
	if err := self.disk.step("before-batch", ""); err != nil {
		return err
	}
	if err := self.db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
		self.latchFault(err)
		return errors.Join(errAttemptRecordStoreFaulted, err)
	}
	if err := self.disk.step("after-batch", ""); err != nil {
		return err
	}
	if err := self.check(context.Background()); err != nil {
		return err
	}
	self.stateLock.Lock()
	self.head = head
	self.stateLock.Unlock()
	return nil
}

// The returned head is an owned value, not a mutable cached-verdict handle.
func (self *attemptRecordStore) Head() (attemptRecordStoreHead, error) {
	if err := self.begin(context.Background()); err != nil {
		return attemptRecordStoreHead{}, err
	}
	defer self.active.Done()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closing {
		return attemptRecordStoreHead{}, errAttemptRecordStoreClosed
	}
	if self.fault != nil {
		return attemptRecordStoreHead{}, errors.Join(errAttemptRecordStoreFaulted, self.fault)
	}
	return self.head, nil
}

// Reads one owned, signature-checked record from an already committed range.
func (self *attemptRecordStore) Read(ctx context.Context, sequence uint64) (AttemptRecord, error) {
	if err := self.begin(ctx); err != nil {
		return AttemptRecord{}, err
	}
	defer self.active.Done()
	self.stateLock.Lock()
	last := self.head.LastSequence
	self.stateLock.Unlock()
	if sequence == 0 || sequence > last {
		return AttemptRecord{}, errors.New("attempt record store sequence is outside the committed range")
	}
	record, err := self.readRecord(sequence)
	if err != nil {
		self.latchFault(err)
		return AttemptRecord{}, errors.Join(errAttemptRecordStoreFaulted, err)
	}
	if err := self.check(ctx); err != nil {
		return AttemptRecord{}, err
	}
	return record, nil
}

// Walk callbacks run without any store lock; each record is independently
// owned. A callback failure does not fault storage or acknowledge partial work.
func (self *attemptRecordStore) Walk(ctx context.Context, first, last uint64, visit func(AttemptRecord) error) error {
	return self.walk(ctx, first, last, false, visit)
}

// Pending streams one exact final checkpoint per unfinished trail.
func (self *attemptRecordStore) Pending(ctx context.Context, visit func(AttemptRecord) error) error {
	return self.walk(ctx, 0, 0, true, visit)
}

// A single iterator bounds backend snapshot and decoded-record ownership.
func (self *attemptRecordStore) walk(ctx context.Context, first, last uint64, pendingOnly bool, visit func(AttemptRecord) error) error {
	if visit == nil {
		return errors.New("attempt record store visitor is nil")
	}
	if err := self.begin(ctx); err != nil {
		return err
	}
	defer self.active.Done()
	select {
	case self.walkGate <- struct{}{}:
		defer func() { <-self.walkGate }()
	case <-ctx.Done():
		return ctx.Err()
	case <-self.ctx.Done():
		return errAttemptRecordStoreClosed
	}
	if err := self.check(ctx); err != nil {
		return err
	}
	if err := self.disk.step("before-snapshot", ""); err != nil {
		return err
	}
	// Pair the backend snapshot with the published head. Append retains this
	// gate through atomic commit and head publication; no visitor holds it.
	if err := self.acquireAppend(ctx, "snapshot"); err != nil {
		return err
	}
	self.stateLock.Lock()
	head := self.head
	self.stateLock.Unlock()
	snapshot, err := self.db.GetSnapshot()
	<-self.appendGate
	if err != nil {
		self.latchFault(err)
		return err
	}
	defer snapshot.Release()
	var keyRange *util.Range
	if pendingOnly {
		keyRange = util.BytesPrefix([]byte(attemptStoreTrailPrefix))
	} else {
		if first == 0 || first > last || last > head.LastSequence {
			return errors.New("attempt record store walk range is invalid")
		}
		keyRange = &util.Range{Start: attemptStoreRecordKey(first)}
		if last != ^uint64(0) {
			keyRange.Limit = attemptStoreRecordKey(last + 1)
		} else {
			keyRange.Limit = util.BytesPrefix([]byte(attemptStoreRecordPrefix)).Limit
		}
	}
	iterator := snapshot.NewIterator(keyRange, &opt.ReadOptions{DontFillCache: true, Strict: opt.StrictAll})
	defer iterator.Release()
	sequence := first
	for iterator.Next() {
		if err := self.check(ctx); err != nil {
			return err
		}
		var record AttemptRecord
		var err error
		if pendingOnly {
			var state attemptRecordStoreTrail
			err = attemptStoreDecode(iterator.Value(), &state)
			if err == nil && state.Terminal {
				continue
			}
			if err == nil {
				var raw []byte
				raw, err = snapshot.Get(attemptStoreRecordKey(state.LastSequence), &opt.ReadOptions{DontFillCache: true, Strict: opt.StrictAll})
				if err == nil {
					record, err = self.decodeRecord(raw)
				}
			}
			if err == nil && (!bytes.Equal(iterator.Key(), attemptStoreTrailKey(record.TrailID)) || record.Sequence != state.LastSequence || record.Sequence > head.LastSequence || record.RecordHash != state.RecordHash || record.Disposition != AttemptDispositionPending) {
				err = errors.New("attempt record store pending state differs")
			}
		} else {
			record, err = self.decodeRecord(iterator.Value())
			if err == nil && (!bytes.Equal(iterator.Key(), attemptStoreRecordKey(sequence)) || record.Sequence != sequence) {
				err = errors.New("attempt record store walk has a gap")
			}
			sequence++
		}
		if err != nil {
			self.latchFault(err)
			return err
		}
		if err := visit(record); err != nil {
			return err
		}
	}
	if err := iterator.Error(); err != nil {
		self.latchFault(err)
		return err
	}
	if !pendingOnly && sequence != last+1 {
		err := errors.New("attempt record store walk ended before its committed range")
		self.latchFault(err)
		return err
	}
	return self.check(ctx)
}

// Close is idempotent and joins admitted operations before closing the DB,
// which itself joins compaction workers. It must not be called by a visitor.
func (self *attemptRecordStore) Close() error {
	self.stateLock.Lock()
	if self.closing {
		done := self.closeDone
		self.stateLock.Unlock()
		<-done
		return self.closeError
	}
	self.closing = true
	self.cancel()
	self.stateLock.Unlock()
	self.active.Wait()
	err := self.db.Close()
	err = errors.Join(err, self.disk.Close())
	self.stateLock.Lock()
	self.closeError = errors.Join(err, self.fault)
	close(self.closeDone)
	self.stateLock.Unlock()
	return self.closeError
}

// Descriptor-relative storage adds strict CURRENT selection, no-alias private
// files, bounded reservations and parent durability to the LevelDB engine.
type attemptRecordStoreStorage struct {
	stateLock sync.Mutex
	metaLock  sync.Mutex
	path      string
	anchor    os.FileInfo
	root      *os.Root
	directory *os.File
	ownerFile *os.File
	locked    bool
	closed    bool
	failure   error
	sizes     map[string]uint64
	used      uint64
	bounds    attemptRecordStoreBounds
	hooks     attemptRecordStoreHooks
	fault     func(error)
}

// Creates only the final private directory through an anchored parent. All
// subsequent data I/O is relative to the retained root, never the pathname.
func openAttemptRecordStoreStorage(path string, bounds attemptRecordStoreBounds, hooks attemptRecordStoreHooks, fault func(error)) (*attemptRecordStoreStorage, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return nil, errors.New("attempt record store path is not a clean absolute directory")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer parentRoot.Close()
	return openAttemptRecordStoreStorageAt(parentRoot, filepath.Join(parent, filepath.Base(path)), bounds, hooks, fault)
}

// A retained parent root can be supplied by a migration or scratch owner.
func openAttemptRecordStoreStorageAt(parentRoot *os.Root, path string, bounds attemptRecordStoreBounds, hooks attemptRecordStoreHooks, fault func(error)) (*attemptRecordStoreStorage, error) {
	return openAttemptRecordStoreStorageAtWithAnchor(parentRoot, path, nil, bounds, hooks, fault)
}

// A fresh-directory owner binds its earlier inode observation to the actual
// storage descriptor before parent fsync, owner locking or backend mutation.
func openAttemptRecordStoreStorageAtWithAnchor(parentRoot *os.Root, path string, expected os.FileInfo, bounds attemptRecordStoreBounds, hooks attemptRecordStoreHooks, fault func(error)) (*attemptRecordStoreStorage, error) {
	if parentRoot == nil || expected != nil && !attemptStorePrivateDirectory(expected) {
		return nil, errors.New("attempt record store directory authority is incomplete")
	}
	name := filepath.Base(path)
	if info, err := parentRoot.Lstat(name); errors.Is(err, os.ErrNotExist) {
		if expected != nil {
			return nil, errors.New("attempt record store owned directory is missing")
		}
		if err := parentRoot.Mkdir(name, 0o700); err != nil {
			return nil, err
		}
	} else if err != nil || !attemptStorePrivateDirectory(info) {
		return nil, errors.New("attempt record store directory is not private and regular")
	}
	anchor, err := parentRoot.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !attemptStorePrivateDirectory(anchor) || expected != nil && !os.SameFile(expected, anchor) {
		return nil, errors.New("attempt record store owned directory changed before open")
	}
	self := &attemptRecordStoreStorage{path: path, anchor: anchor, bounds: bounds, hooks: hooks, fault: fault, sizes: map[string]uint64{}, used: attemptStoreMetadataReserve}
	if err := self.step("after-directory-check", ""); err != nil {
		return nil, err
	}
	self.root, err = parentRoot.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = self.Close()
		}
	}()
	self.directory, err = self.root.Open(".")
	if err != nil {
		return nil, err
	}
	opened, err := self.directory.Stat()
	if err != nil || !attemptStorePrivateDirectory(opened) || !os.SameFile(anchor, opened) || expected != nil && !os.SameFile(expected, opened) {
		return nil, errors.New("attempt record store directory changed during open")
	}
	parentFile, err := parentRoot.Open(".")
	if err != nil {
		return nil, err
	}
	err = self.syncDirectoryFile(parentFile, "parent")
	err = errors.Join(err, parentFile.Close())
	if err != nil {
		return nil, err
	}
	// The directory inode, not replaceable LOCK-file contents, is authority.
	if err := attemptStoreLockFile(self.directory); err != nil {
		return nil, err
	}
	self.ownerFile, err = self.openFile("LOCK", os.O_RDWR|os.O_CREATE, true)
	if err != nil {
		return nil, err
	}
	if err := self.syncDirectoryFile(self.directory, "owner"); err != nil {
		return nil, err
	}
	directory, err := self.root.Open(".")
	if err != nil {
		return nil, err
	}
	var entries, metadataBytes uint64
	for {
		infos, readErr := directory.ReadDir(128)
		for _, entry := range infos {
			entries++
			info, infoErr := entry.Info()
			if infoErr != nil || !attemptStorePrivateFile(info) || entries > bounds.MaxStorageFiles {
				directory.Close()
				return nil, errors.New("attempt record store contains nonregular or excessive files")
			}
			self.sizes[entry.Name()] = uint64(info.Size())
			if !attemptStoreMetadataName(entry.Name()) {
				if uint64(info.Size()) > bounds.MaxStorageBytes-self.used {
					directory.Close()
					return nil, errAttemptRecordStoreLimit
				}
				self.used += uint64(info.Size())
			} else {
				metadataBytes += uint64(info.Size())
				if metadataBytes > attemptStoreMetadataReserve {
					directory.Close()
					return nil, errors.New("attempt record store metadata files exceed their reservation")
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			directory.Close()
			return nil, readErr
		}
	}
	if err := directory.Close(); err != nil {
		return nil, err
	}
	complete = true
	return self, nil
}

// Lock is the engine's single-session lock; the process lock outlives it.
func (self *attemptRecordStoreStorage) Lock() (storage.Locker, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closed {
		return nil, storage.ErrClosed
	}
	if self.locked {
		return nil, storage.ErrLocked
	}
	self.locked = true
	return &attemptRecordStoreLocker{disk: self}, nil
}

// One session can release only its own lock once.
type attemptRecordStoreLocker struct {
	disk *attemptRecordStoreStorage
	once sync.Once
}

// The retained OS lock is released only when storage itself closes.
func (self *attemptRecordStoreLocker) Unlock() {
	self.once.Do(func() {
		self.disk.stateLock.Lock()
		self.disk.locked = false
		self.disk.stateLock.Unlock()
	})
}

// The first persistence failure also stops background backend mutations.
func (self *attemptRecordStoreStorage) fail(err error) error {
	if err != nil {
		self.stateLock.Lock()
		if self.failure == nil {
			self.failure = err
		}
		self.stateLock.Unlock()
		self.fault(err)
	}
	return err
}

// Fixed-size backend metadata has a conservative independent reservation.
func attemptStoreMetadataName(name string) bool {
	return name == "CURRENT" || name == "CURRENT.bak" || strings.HasPrefix(name, "CURRENT.") || name == "LOCK" || name == "LOG" || name == "LOG.old"
}

// Fault injection and path replacement checks precede external filesystem I/O.
func (self *attemptRecordStoreStorage) step(operation, name string) error {
	self.stateLock.Lock()
	failure, closed := self.failure, self.closed
	self.stateLock.Unlock()
	if closed {
		return storage.ErrClosed
	}
	if failure != nil {
		return failure
	}
	info, err := os.Lstat(self.path)
	if err != nil || !attemptStorePrivateDirectory(info) || !os.SameFile(info, self.anchor) {
		err = errors.New("attempt record store directory anchor changed")
		return self.fail(err)
	}
	// Hooks run after the pathname check so swap tests exercise the actual
	// descriptor-relative operation, not merely another pre-operation check.
	if self.hooks.Step != nil {
		if err := self.hooks.Step(operation, name); err != nil {
			return self.fail(err)
		}
	}
	return nil
}

// Fsync the directory containing every newly created durable file name.
func (self *attemptRecordStoreStorage) syncDirectoryFile(directory *os.File, stage string) error {
	if err := self.step("before-directory-sync", stage); err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		return self.fail(err)
	}
	return self.step("after-directory-sync", stage)
}

// Never follow a symlink or accept a directory in place of backend data.
func (self *attemptRecordStoreStorage) checkFile(name string, missingAllowed bool) error {
	if err := self.step("file-check", name); err != nil {
		return err
	}
	info, err := self.root.Lstat(name)
	if missingAllowed && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !attemptStorePrivateFile(info) {
		return self.fail(errors.New("attempt record store backend file is missing, linked or nonprivate"))
	}
	return nil
}

// No-follow descriptor-relative open checks the actual opened inode as well as
// both directory observations. A swapped path never redirects outside root.
func (self *attemptRecordStoreStorage) openFile(name string, flags int, missingAllowed bool) (*os.File, error) {
	if err := self.checkFile(name, missingAllowed); err != nil {
		return nil, err
	}
	before, beforeErr := self.root.Lstat(name)
	if beforeErr != nil && !(missingAllowed && errors.Is(beforeErr, os.ErrNotExist)) {
		return nil, self.fail(beforeErr)
	}
	if err := self.step("after-file-check", name); err != nil {
		return nil, err
	}
	file, err := self.root.OpenFile(name, flags|attemptStoreNoFollowFlag(), 0o600)
	if err != nil {
		return nil, self.fail(err)
	}
	opened, openedErr := file.Stat()
	after, afterErr := self.root.Lstat(name)
	if openedErr != nil || afterErr != nil || !attemptStorePrivateFile(opened) || !attemptStorePrivateFile(after) || !os.SameFile(opened, after) || (beforeErr == nil && !os.SameFile(before, opened)) {
		err := errors.Join(errors.New("attempt record store file identity changed during open"), file.Close())
		return nil, self.fail(err)
	}
	if err := self.step("after-open", name); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// Logging is intentionally external to this data store's bounded files.
func (self *attemptRecordStoreStorage) Log(string) {}

// Do not use fileStorage's fallback/repair of CURRENT.bak or CURRENT.N.
func (self *attemptRecordStoreStorage) GetMeta() (storage.FileDesc, error) {
	if err := self.step("get-meta", "CURRENT"); err != nil {
		return storage.FileDesc{}, err
	}
	if _, err := self.root.Lstat("CURRENT"); errors.Is(err, os.ErrNotExist) {
		directory, err := self.root.Open(".")
		if err != nil {
			return storage.FileDesc{}, err
		}
		defer directory.Close()
		for {
			entries, listErr := directory.ReadDir(128)
			for _, entry := range entries {
				if entry.Name() != "LOCK" && entry.Name() != "LOG" {
					return storage.FileDesc{}, errors.New("attempt record store CURRENT is missing from existing state")
				}
			}
			if errors.Is(listErr, io.EOF) {
				break
			}
			if listErr != nil {
				return storage.FileDesc{}, listErr
			}
		}
		return storage.FileDesc{}, os.ErrNotExist
	}
	if err := self.checkFile("CURRENT", false); err != nil {
		return storage.FileDesc{}, err
	}
	file, err := self.openFile("CURRENT", os.O_RDONLY, false)
	if err != nil {
		return storage.FileDesc{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, 128))
	err = errors.Join(err, file.Close())
	if err != nil || !bytes.HasPrefix(raw, []byte("MANIFEST-")) || len(raw) == 128 || len(raw) < 11 || raw[len(raw)-1] != '\n' {
		return storage.FileDesc{}, errors.New("attempt record store CURRENT is malformed")
	}
	number, err := strconv.ParseInt(string(raw[len("MANIFEST-"):len(raw)-1]), 10, 64)
	fd := storage.FileDesc{Type: storage.TypeManifest, Num: number}
	if err != nil || !storage.FileDescOk(fd) || string(raw) != fd.String()+"\n" {
		return storage.FileDesc{}, errors.New("attempt record store CURRENT is not canonical")
	}
	if err := self.checkFile(fd.String(), false); err != nil {
		return storage.FileDesc{}, err
	}
	return fd, nil
}

// CURRENT replacement is durable before its manifest is treated as selected.
func (self *attemptRecordStoreStorage) SetMeta(fd storage.FileDesc) error {
	if !storage.FileDescOk(fd) || fd.Type != storage.TypeManifest {
		return storage.ErrInvalidFile
	}
	self.metaLock.Lock()
	defer self.metaLock.Unlock()
	if err := self.step("before-set-meta", fd.String()); err != nil {
		return err
	}
	if err := self.checkFile("CURRENT", true); err != nil {
		return err
	}
	name := "CURRENT." + strconv.FormatInt(fd.Num, 10)
	raw := []byte(fd.String() + "\n")
	self.stateLock.Lock()
	var metadataBytes uint64
	for name, size := range self.sizes {
		if attemptStoreMetadataName(name) {
			metadataBytes += size
		}
	}
	_, exists := self.sizes[name]
	if exists || uint64(len(self.sizes))+1 > self.bounds.MaxStorageFiles || metadataBytes+uint64(len(raw)) > attemptStoreMetadataReserve {
		self.stateLock.Unlock()
		return self.fail(errors.New("attempt record store metadata collision or limit"))
	}
	self.sizes[name] = uint64(len(raw))
	self.stateLock.Unlock()
	file, err := self.openFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, true)
	if err != nil {
		return err
	}
	// Metadata creation has the same explicit file-count and byte reservation.
	if err := self.step("before-meta-write", name); err != nil {
		return errors.Join(err, file.Close())
	}
	n, err := file.Write(raw)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = self.step("before-meta-sync", name)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return self.fail(err)
	}
	if err := self.step("before-meta-rename", name); err != nil {
		return err
	}
	if err := self.root.Rename(name, "CURRENT"); err != nil {
		return self.fail(err)
	}
	self.stateLock.Lock()
	delete(self.sizes, name)
	self.sizes["CURRENT"] = uint64(len(raw))
	self.stateLock.Unlock()
	if err := self.syncDirectoryFile(self.directory, "meta"); err != nil {
		return err
	}
	return self.step("after-set-meta", fd.String())
}

// Readers are checked against the anchored private directory before opening.
func (self *attemptRecordStoreStorage) Open(fd storage.FileDesc) (storage.Reader, error) {
	if !storage.FileDescOk(fd) {
		return nil, storage.ErrInvalidFile
	}
	return self.openFile(fd.String(), os.O_RDONLY, false)
}

// Only canonical bounded descriptor names enter the engine's recovery census.
func (self *attemptRecordStoreStorage) List(types storage.FileType) ([]storage.FileDesc, error) {
	if err := self.step("list", ""); err != nil {
		return nil, err
	}
	directory, err := self.root.Open(".")
	if err != nil {
		return nil, self.fail(err)
	}
	defer directory.Close()
	var descriptors []storage.FileDesc
	var count uint64
	for {
		entries, err := directory.ReadDir(128)
		for _, entry := range entries {
			count++
			if count > self.bounds.MaxStorageFiles {
				return nil, self.fail(errAttemptRecordStoreLimit)
			}
			name := entry.Name()
			if attemptStoreMetadataName(name) {
				continue
			}
			matched := false
			for _, candidate := range []struct {
				kind           storage.FileType
				prefix, suffix string
			}{
				{kind: storage.TypeManifest, prefix: "MANIFEST-"},
				{kind: storage.TypeJournal, suffix: ".log"},
				{kind: storage.TypeTable, suffix: ".ldb"},
				{kind: storage.TypeTemp, suffix: ".tmp"},
			} {
				if !strings.HasPrefix(name, candidate.prefix) || !strings.HasSuffix(name, candidate.suffix) {
					continue
				}
				number, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(name, candidate.prefix), candidate.suffix), 10, 64)
				fd := storage.FileDesc{Type: candidate.kind, Num: number}
				if err == nil && storage.FileDescOk(fd) && fd.String() == name {
					matched = true
					if candidate.kind&types != 0 {
						descriptors = append(descriptors, fd)
					}
					break
				}
			}
			if !matched {
				return nil, self.fail(errors.New("attempt record store contains a noncanonical backend file"))
			}
		}
		if errors.Is(err, io.EOF) {
			return descriptors, nil
		}
		if err != nil {
			return nil, self.fail(err)
		}
	}
}

// New files reserve a bounded descriptor before creation; their names are
// parent-fsynced immediately and again after syncing record-bearing contents.
func (self *attemptRecordStoreStorage) Create(fd storage.FileDesc) (storage.Writer, error) {
	if !storage.FileDescOk(fd) {
		return nil, storage.ErrInvalidFile
	}
	name := fd.String()
	if err := self.checkFile(name, true); err != nil {
		return nil, err
	}
	if err := self.step("before-create", name); err != nil {
		return nil, err
	}
	self.stateLock.Lock()
	_, exists := self.sizes[name]
	if exists || uint64(len(self.sizes))+1 > self.bounds.MaxStorageFiles {
		self.stateLock.Unlock()
		if exists {
			return nil, self.fail(errors.New("attempt record store refuses to truncate an existing descriptor"))
		}
		return nil, self.fail(errAttemptRecordStoreLimit)
	}
	self.sizes[name] = 0
	self.stateLock.Unlock()
	writer, err := self.openFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, true)
	if err != nil {
		return nil, err
	}
	if err := self.syncDirectoryFile(self.directory, "create:"+name); err != nil {
		return nil, errors.Join(err, writer.Close())
	}
	if err := self.step("after-create", name); err != nil {
		return nil, errors.Join(err, writer.Close())
	}
	return &attemptRecordStoreWriter{file: writer, disk: self, name: name}, nil
}

// Backend compaction may remove only its own validated file descriptors.
func (self *attemptRecordStoreStorage) Remove(fd storage.FileDesc) error {
	if err := self.checkFile(fd.String(), false); err != nil {
		return err
	}
	if err := self.root.Remove(fd.String()); err != nil {
		return self.fail(err)
	}
	self.stateLock.Lock()
	self.used -= self.sizes[fd.String()]
	delete(self.sizes, fd.String())
	self.stateLock.Unlock()
	return self.syncDirectoryFile(self.directory, "remove:"+fd.String())
}

// Renamed backend files retain the same conservative byte reservation.
func (self *attemptRecordStoreStorage) Rename(old, next storage.FileDesc) error {
	if old == next {
		return self.checkFile(old.String(), false)
	}
	if err := self.checkFile(old.String(), false); err != nil {
		return err
	}
	if err := self.checkFile(next.String(), true); err != nil {
		return err
	}
	if _, err := self.root.Lstat(next.String()); !errors.Is(err, os.ErrNotExist) {
		return self.fail(errors.New("attempt record store rename destination exists or is unavailable"))
	}
	if err := attemptStoreRenameNoReplace(self.directory, old.String(), next.String()); err != nil {
		return self.fail(err)
	}
	self.stateLock.Lock()
	self.used -= self.sizes[next.String()]
	self.sizes[next.String()] = self.sizes[old.String()]
	delete(self.sizes, old.String())
	self.stateLock.Unlock()
	return self.syncDirectoryFile(self.directory, "rename:"+next.String())
}

// The DB has joined workers before storage closes and releases its file lock.
func (self *attemptRecordStoreStorage) Close() error {
	self.stateLock.Lock()
	if self.closed {
		self.stateLock.Unlock()
		return nil
	}
	self.closed = true
	self.stateLock.Unlock()
	var err error
	if self.ownerFile != nil {
		err = errors.Join(err, self.ownerFile.Close())
	}
	if self.directory != nil {
		err = errors.Join(err, self.directory.Close())
	}
	if self.root != nil {
		err = errors.Join(err, self.root.Close())
	}
	return self.fail(err)
}

// Writer reservations include data that a failed write may have persisted.
type attemptRecordStoreWriter struct {
	file *os.File
	disk *attemptRecordStoreStorage
	name string
}

// An already-open writer also rejects a subsequently linked or replaced inode.
func (self *attemptRecordStoreWriter) check() error {
	opened, openedErr := self.file.Stat()
	current, currentErr := self.disk.root.Lstat(self.name)
	if openedErr != nil || currentErr != nil || !attemptStorePrivateFile(opened) || !attemptStorePrivateFile(current) || !os.SameFile(opened, current) {
		return self.disk.fail(errors.New("attempt record store writer inode changed or acquired an alias"))
	}
	return nil
}

// Reserve before writing and never free an ambiguous partially written range.
func (self *attemptRecordStoreWriter) Write(data []byte) (int, error) {
	if err := self.disk.step("before-write", self.name); err != nil {
		return 0, err
	}
	if err := self.check(); err != nil {
		return 0, err
	}
	self.disk.stateLock.Lock()
	if uint64(len(data)) > self.disk.bounds.MaxStorageBytes-self.disk.used {
		self.disk.stateLock.Unlock()
		return 0, self.disk.fail(errAttemptRecordStoreLimit)
	}
	self.disk.used += uint64(len(data))
	self.disk.sizes[self.name] += uint64(len(data))
	self.disk.stateLock.Unlock()
	n, err := self.file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return n, self.disk.fail(err)
	}
	if err := self.check(); err != nil {
		return n, err
	}
	return n, self.disk.step("after-write", self.name)
}

// A synced WAL also requires its containing directory entry to be durable.
func (self *attemptRecordStoreWriter) Sync() error {
	if err := self.disk.step("before-file-sync", self.name); err != nil {
		return err
	}
	if err := self.check(); err != nil {
		return err
	}
	if err := self.file.Sync(); err != nil {
		return self.disk.fail(err)
	}
	if err := self.disk.syncDirectoryFile(self.disk.directory, "file:"+self.name); err != nil {
		return err
	}
	return self.disk.step("after-file-sync", self.name)
}

// A close error is also ambiguous and faults the owner rather than being lost.
func (self *attemptRecordStoreWriter) Close() error {
	err := self.file.Close()
	if err != nil {
		return self.disk.fail(err)
	}
	return self.disk.step("after-file-close", self.name)
}

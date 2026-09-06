//go:build linux || darwin

package validator

// Private aggregate publication is a single synced header, not a bulk map
// replacement. At most one unpublished generation exists. Snapshot readers
// never observe it; bounded recovery removes it before another writer starts.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// Hooks distinguish owned point admission from actual completed batch Sync.
// They are immutable, operation-owned and nil in production. They may read
// committed snapshots but must not recursively write or close their owner.
type statsAggregateHooks struct {
	StorageStep      func(string, string) error
	PointStaged      func(byte, int, int) error
	BatchSynced      func(string, int, int) error
	BeforeCommit     func(statsAggregateHead) error
	AdmissionChecked func(statsAggregateAdmissionReport) error
}

// Methods are safe for concurrent use. One cancellable writer owns a stage;
// readers retain independent DB snapshots, not the state lock, across callbacks.
// Close cancels and joins all admitted work. Visitors must not call Close.
type statsAggregateStore struct {
	stateLock     sync.Mutex
	active        sync.WaitGroup
	writeGate     chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	closing       bool
	closeDone     chan struct{}
	closeError    error
	fault         error
	needsRecovery bool
	bounds        statsAggregateBounds
	hooks         statsAggregateHooks
	db            *leveldb.DB
	disk          *attemptRecordStoreStorage
}

// Both live databases and read snapshots provide bounded ordered lookups.
type statsAggregateReader interface {
	NewIterator(*util.Range, *opt.ReadOptions) iterator.Iterator
	Get([]byte, *opt.ReadOptions) ([]byte, error)
}

// Opens only this private schema. The empty bootstrap is intentionally limited
// to a genuinely empty ledger prefix: migration of prior EMA authority requires
// a separately reviewed importer, not an implicit zero-quality assumption.
func openStatsAggregateStore(ctx context.Context, path string, expected AttemptCutV2Context, policy protocol.Policy, config StatsConfig, bounds statsAggregateBounds, hooks statsAggregateHooks) (result *statsAggregateStore, resultErr error) {
	if ctx == nil {
		return nil, errors.New("private aggregate context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bounds.MaxStorageBytes > attemptAdmissionMaximumStorageBytes {
		return nil, errors.New("private aggregate storage ceiling exceeds the supported native arithmetic domain")
	}
	config = config.withDefaults()
	if err := statsAggregatePolicyConfig(expected, policy, config); err != nil {
		return nil, err
	}
	if expected.Activation.FirstSequence != 1 || expected.FirstSequence != 1 || expected.EgressFirstSequence != 1 || expected.PriorRoot != zeroAttemptHash() {
		return nil, errors.New("private aggregate initial prior-state migration is not implemented")
	}
	initial := statsAggregateHead{Schema: statsAggregateSchema, Identity: expected.Identity, Activation: expected.Activation, Config: config, SettlementEpoch: expected.Boundary.SettlementEpoch, SettlementFirstSequence: 1, SettlementPriorRoot: expected.PriorRoot, EgressGeneration: expected.EgressGeneration, EgressFirstSequence: 1, LastAppliedRoot: expected.PriorRoot, Boundary: expected.Boundary}
	if err := initial.validate(bounds); err != nil {
		return nil, err
	}
	storeCtx, cancel := context.WithCancel(context.Background())
	self := &statsAggregateStore{ctx: storeCtx, cancel: cancel, closeDone: make(chan struct{}), writeGate: make(chan struct{}, 1), bounds: bounds, hooks: hooks}
	complete := false
	defer func() {
		if !complete {
			resultErr = errors.Join(resultErr, self.Close())
			result = nil
		}
	}()
	var err error
	self.disk, err = openAttemptRecordStoreInspection(ctx, path, attemptRecordStoreBounds{MaxStorageBytes: bounds.MaxStorageBytes, MaxStorageFiles: bounds.MaxStorageFiles}, attemptRecordStoreHooks{Step: hooks.StorageStep}, self.latchFault)
	if err != nil {
		return nil, err
	}
	admitted, err := self.inspectHead(ctx, initial)
	if err != nil {
		return nil, err
	}
	if err := self.disk.promoteInspection(); err != nil {
		return nil, err
	}
	self.db, err = leveldb.Open(self.disk, &opt.Options{Strict: opt.StrictAll, BlockCacheCapacity: 8 * 1024 * 1024, WriteBuffer: 4 * 1024 * 1024, CompactionTableSize: 2 * 1024 * 1024, CompactionTableSizeMultiplier: 1, OpenFilesCacheCapacity: 64, DisableLargeBatchTransaction: true})
	if err != nil {
		return nil, err
	}
	head, err := self.readHead(self.db)
	if admitted != nil && (err != nil || head != *admitted) {
		return nil, errors.Join(errors.New("private aggregate writable head differs from admitted authority"), err)
	}
	if admitted == nil && err == nil {
		return nil, errors.New("private aggregate writable authority appeared after empty admission")
	}
	if errors.Is(err, leveldb.ErrNotFound) {
		cursor := self.db.NewIterator(nil, &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
		nonempty, scanErr := cursor.Next(), cursor.Error()
		cursor.Release()
		if nonempty || scanErr != nil {
			return nil, errors.Join(errors.New("private aggregate has rows without an authority header"), scanErr)
		}
		if err := self.check(ctx); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(initial)
		if err != nil {
			return nil, err
		}
		if err := self.db.Put([]byte{'h'}, raw, &opt.WriteOptions{Sync: true}); err != nil {
			return nil, err
		}
		head = initial
	} else if err != nil {
		return nil, err
	}
	if head.Identity != initial.Identity || head.Activation != initial.Activation || head.Config != initial.Config {
		return nil, errors.New("private aggregate existing namespace or config differs")
	}
	if err := self.recoverRows(ctx, head); err != nil {
		return nil, err
	}
	if err := self.check(ctx); err != nil {
		return nil, err
	}
	if err := self.disk.finishOpening(); err != nil {
		return nil, err
	}
	complete = true
	return self, nil
}

// Physical I/O failures and observed persisted-state corruption permanently
// stop this live owner; operation-local cancellation does not.
func (self *statsAggregateStore) latchFault(err error) {
	if err == nil {
		return
	}
	self.stateLock.Lock()
	if self.fault == nil {
		self.fault = err
	}
	self.stateLock.Unlock()
}

// Admission and Close share one short mutex so Wait cannot race a later Add.
func (self *statsAggregateStore) begin(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, errors.New("private aggregate context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	self.stateLock.Lock()
	if self.closing || self.fault != nil {
		err := errors.Join(errors.New("private aggregate is closed or faulted"), self.fault)
		self.stateLock.Unlock()
		return nil, nil, err
	}
	self.active.Add(1)
	self.stateLock.Unlock()
	operationCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(self.ctx, cancel)
	return operationCtx, func() { stop(); cancel(); self.active.Done() }, nil
}

// No external visitor or filesystem hook executes under this metadata lock.
func (self *statsAggregateStore) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closing || self.fault != nil {
		return errors.Join(errors.New("private aggregate is closed or faulted"), self.fault)
	}
	return nil
}

// Header reads are bounded by the private schema, not public artifact limits.
func (self *statsAggregateStore) readHead(reader statsAggregateReader) (statsAggregateHead, error) {
	var head statsAggregateHead
	raw, err := reader.Get([]byte{'h'}, &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	if err != nil {
		return head, err
	}
	if uint64(len(raw)) > self.bounds.MaxHeaderBytes {
		return head, errors.New("private aggregate stored header is oversized")
	}
	if err := attemptStoreDecode(raw, &head); err != nil {
		return head, err
	}
	return head, head.validate(self.bounds)
}

// Returns a detached committed generation; no caller callback runs here.
func (self *statsAggregateStore) Head(ctx context.Context) (statsAggregateHead, error) {
	operationCtx, done, err := self.begin(ctx)
	if err != nil {
		return statsAggregateHead{}, err
	}
	defer done()
	head, err := self.readHead(self.db)
	if err != nil {
		return statsAggregateHead{}, err
	}
	if err := self.check(operationCtx); err != nil {
		return statsAggregateHead{}, err
	}
	return head, nil
}

// Inverted generation suffixes keep newest rows first within one exact key.
func statsAggregateVersionKey(base []byte, generation uint64) []byte {
	key := make([]byte, len(base)+8)
	copy(key, base)
	binary.BigEndian.PutUint64(key[len(base):], ^generation)
	return key
}

// Provider identities are ordered using their canonical binary ULID bytes.
func statsAggregateProviderKey(clientID connect.Id) []byte {
	key := make([]byte, 17)
	key[0] = 'p'
	copy(key[1:], clientID[:])
	return key
}

// Native hashes deduplicate only exact provider/hash pairs within one clock.
func statsAggregateEgressKey(clock uint64, clientID connect.Id, hash [32]byte) []byte {
	key := make([]byte, 57)
	key[0] = 'e'
	binary.BigEndian.PutUint64(key[1:9], clock)
	copy(key[9:25], clientID[:])
	copy(key[25:], hash[:])
	return key
}

// Complete claims preserve original sequence, binding identity and hash. Their
// byte ordering matches the existing v1 sequence/provider/hash projection.
func statsAggregateClaimKey(clock, sequence uint64, clientID connect.Id, hash [32]byte) []byte {
	key := make([]byte, 65)
	key[0] = 'c'
	binary.BigEndian.PutUint64(key[1:9], clock)
	binary.BigEndian.PutUint64(key[9:17], sequence)
	copy(key[17:33], clientID[:])
	copy(key[33:], hash[:])
	return key
}

// Exact lexicographic successor skips old versions without parent chasing.
func statsAggregateNextPrefix(prefix []byte) []byte {
	next := bytes.Clone(prefix)
	for index := len(next) - 1; index >= 0; index-- {
		if next[index] != 255 {
			next[index]++
			return next[:index+1]
		}
	}
	return nil
}

// One seek selects one visible point value, independent of generation history.
func statsAggregatePoint(reader statsAggregateReader, base []byte, generation uint64) ([]byte, bool, error) {
	cursor := reader.NewIterator(util.BytesPrefix(base), &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	defer cursor.Release()
	if !cursor.Seek(statsAggregateVersionKey(base, generation)) {
		return nil, false, cursor.Error()
	}
	if len(cursor.Key()) != len(base)+8 || !bytes.HasPrefix(cursor.Key(), base) {
		return nil, false, errors.New("private aggregate point key is malformed")
	}
	value := bytes.Clone(cursor.Value())
	return value, true, cursor.Error()
}

// A reader snapshots the header and every row atomically before invoking a
// visitor. A caller-selected stale generation fails before the first callback.
func (self *statsAggregateStore) snapshot(ctx context.Context, generation uint64, visit func(context.Context, *leveldb.Snapshot, statsAggregateHead) error) error {
	operationCtx, done, err := self.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	snapshot, err := self.db.GetSnapshot()
	if err != nil {
		return err
	}
	defer snapshot.Release()
	head, err := self.readHead(snapshot)
	if err != nil {
		return err
	}
	if head.Generation != generation {
		return errors.New("private aggregate snapshot generation differs")
	}
	if err := self.check(operationCtx); err != nil {
		return err
	}
	if err := visit(operationCtx, snapshot, head); err != nil {
		return err
	}
	return self.check(operationCtx)
}

// Ordered walks retain one key/value at a time. Seeking to the next logical key
// skips old versions rather than retaining or traversing a parent-generation map.
func walkStatsAggregateRows(ctx context.Context, reader statsAggregateReader, prefix []byte, baseBytes int, generation uint64, visit func([]byte, []byte) error) error {
	cursor := reader.NewIterator(util.BytesPrefix(prefix), &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	defer cursor.Release()
	for valid := cursor.First(); valid; {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(cursor.Key()) != baseBytes+8 {
			return errors.New("private aggregate iterator key length differs")
		}
		base := bytes.Clone(cursor.Key()[:baseBytes])
		if cursor.Seek(statsAggregateVersionKey(base, generation)) && bytes.HasPrefix(cursor.Key(), base) {
			if len(cursor.Key()) != baseBytes+8 {
				return errors.New("private aggregate iterator version key is malformed")
			}
			if err := visit(base, cursor.Value()); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		next := statsAggregateNextPrefix(base)
		if next == nil {
			break
		}
		valid = cursor.Seek(next)
	}
	return errors.Join(cursor.Error(), ctx.Err())
}

// Includes every retained identity; callers later choose public sparse-provider
// presentation without deleting authoritative private history.
func (self *statsAggregateStore) WalkProviders(ctx context.Context, generation uint64, visit func(statsAggregateProvider) error) error {
	if visit == nil {
		return errors.New("private aggregate provider visitor is nil")
	}
	return self.snapshot(ctx, generation, func(ctx context.Context, reader *leveldb.Snapshot, head statsAggregateHead) error {
		var count uint64
		err := walkStatsAggregateRows(ctx, reader, []byte{'p'}, 17, generation, func(key, value []byte) error {
			var clientID connect.Id
			copy(clientID[:], key[1:])
			row, err := decodeStatsAggregateProvider(clientID, value, head.Config)
			if err != nil {
				return err
			}
			if count == head.ProviderCount {
				return errors.New("private aggregate provider census exceeds header")
			}
			count++
			return visit(row)
		})
		if err != nil {
			return err
		}
		if count != head.ProviderCount {
			return errors.New("private aggregate provider census differs")
		}
		return nil
	})
}

// Provider/hash order is canonical and the native clock comes from the same
// committed snapshot, never the caller's currently mutable routing state.
func (self *statsAggregateStore) WalkEgress(ctx context.Context, generation uint64, visit func(connect.Id, [32]byte) error) error {
	if visit == nil {
		return errors.New("private aggregate egress visitor is nil")
	}
	return self.snapshot(ctx, generation, func(ctx context.Context, reader *leveldb.Snapshot, head statsAggregateHead) error {
		prefix := statsAggregateEgressKey(head.EgressGeneration, connect.Id{}, [32]byte{})[:9]
		var count uint64
		err := walkStatsAggregateRows(ctx, reader, prefix, 57, generation, func(key, value []byte) error {
			if len(value) != 0 || count == head.EgressHashCount {
				return errors.New("private aggregate egress value or census differs")
			}
			count++
			var clientID connect.Id
			var hash [32]byte
			copy(clientID[:], key[9:25])
			copy(hash[:], key[25:])
			if clientID == (connect.Id{}) || hash == ([32]byte{}) {
				return errors.New("private aggregate egress identity is zero")
			}
			return visit(clientID, hash)
		})
		if err != nil {
			return err
		}
		if count != head.EgressHashCount {
			return errors.New("private aggregate egress census differs")
		}
		return nil
	})
}

// Only genuine active, UID-resolved successful claims enter this projection,
// matching AttemptCutEgressClaims; unbound hashes still remain in WalkEgress.
func (self *statsAggregateStore) WalkClaims(ctx context.Context, generation uint64, visit func(AttemptEgressClaim) error) error {
	if visit == nil {
		return errors.New("private aggregate claim visitor is nil")
	}
	return self.snapshot(ctx, generation, func(ctx context.Context, reader *leveldb.Snapshot, head statsAggregateHead) error {
		prefix := statsAggregateClaimKey(head.EgressGeneration, 0, connect.Id{}, [32]byte{})[:9]
		var count uint64
		err := walkStatsAggregateRows(ctx, reader, prefix, 65, generation, func(key, value []byte) error {
			binding, err := decodeStatsAggregateClaim(value)
			if err != nil {
				return err
			}
			var clientID connect.Id
			copy(clientID[:], key[17:33])
			hash := *(*[32]byte)(key[33:65])
			sequence := binary.BigEndian.Uint64(key[9:17])
			if binding.ClientID != clientID || !binding.Active || !binding.UIDFound || hash == ([32]byte{}) || sequence < head.EgressFirstSequence || sequence > head.LastAppliedSequence || count == head.EgressClaimCount {
				return errors.New("private aggregate claim key, binding or census differs")
			}
			count++
			return visit(AttemptEgressClaim{Sequence: sequence, Binding: binding, EgressIPHash: attemptHex32(hash)})
		})
		if err != nil {
			return err
		}
		if count != head.EgressClaimCount {
			return errors.New("private aggregate claim census differs")
		}
		return nil
	})
}

// Bounded recovery validates exact row shapes and removes only uncommitted,
// obsolete-version or retired-clock rows. DB snapshots retain old visible
// bytes until their owners release them; no generation chain remains.
func (self *statsAggregateStore) recoverRows(ctx context.Context, head statsAggregateHead) error {
	if err := self.disk.step("aggregate-before-recovery", ""); err != nil {
		return err
	}
	if err := self.check(ctx); err != nil {
		return err
	}
	// Corrupt persisted state faults this owner; cancellation or a metadata
	// observer refusal only leaves recovery incomplete. Physical I/O faults
	// are separately latched by the storage and bounded-write owners.
	refuseStoredState := func(err error) error {
		self.latchFault(err)
		return err
	}
	cursor := self.db.NewIterator(nil, &opt.ReadOptions{Strict: opt.StrictAll, DontFillCache: true})
	defer cursor.Release()
	deletions := newStatsAggregateBatch(self, true)
	var previousBase []byte
	var kept bool
	var providers, egress, claims uint64
	for cursor.Next() {
		if err := self.check(ctx); err != nil {
			return err
		}
		key, value := cursor.Key(), cursor.Value()
		if bytes.Equal(key, []byte{'h'}) {
			continue
		}
		baseBytes := 0
		switch {
		case len(key) == 25 && key[0] == 'p':
			baseBytes = 17
			var clientID connect.Id
			copy(clientID[:], key[1:17])
			if _, err := decodeStatsAggregateProvider(clientID, value, head.Config); err != nil {
				return refuseStoredState(err)
			}
		case len(key) == 65 && key[0] == 'e':
			baseBytes = 57
			if len(value) != 0 || bytes.Equal(key[9:25], make([]byte, 16)) || bytes.Equal(key[25:57], make([]byte, 32)) {
				return refuseStoredState(errors.New("private aggregate stored egress row is malformed"))
			}
		case len(key) == 73 && key[0] == 'c':
			baseBytes = 65
			binding, err := decodeStatsAggregateClaim(value)
			if err != nil || !binding.Active || !binding.UIDFound || !bytes.Equal(binding.ClientID[:], key[17:33]) || bytes.Equal(key[33:65], make([]byte, 32)) {
				return refuseStoredState(errors.Join(errors.New("private aggregate stored claim row is malformed"), err))
			}
		default:
			return refuseStoredState(errors.New("private aggregate contains an unknown row"))
		}
		base := key[:baseBytes]
		if !bytes.Equal(previousBase, base) {
			previousBase = bytes.Clone(base)
			kept = false
		}
		version := ^binary.BigEndian.Uint64(key[baseBytes:])
		currentClock := key[0] == 'p' || binary.BigEndian.Uint64(key[1:9]) == head.EgressGeneration
		visible := version <= head.Generation && !kept && currentClock
		if visible {
			kept = true
			switch key[0] {
			case 'p':
				providers++
			case 'e':
				egress++
			case 'c':
				sequence := binary.BigEndian.Uint64(key[9:17])
				if sequence < head.EgressFirstSequence || sequence > head.LastAppliedSequence {
					return refuseStoredState(errors.New("private aggregate stored claim exceeds native prefix"))
				}
				claims++
			}
			if providers > head.ProviderCount || egress > head.EgressHashCount || claims > head.EgressClaimCount {
				return refuseStoredState(errors.New("private aggregate recovery census exceeds header"))
			}
		} else {
			if err := deletions.append(ctx, key, nil); err != nil {
				return err
			}
		}
	}
	if err := cursor.Error(); err != nil {
		return refuseStoredState(err)
	}
	if providers != head.ProviderCount || egress != head.EgressHashCount || claims != head.EgressClaimCount {
		return refuseStoredState(fmt.Errorf("private aggregate recovery census differs: %d/%d/%d", providers, egress, claims))
	}
	return deletions.flush(ctx)
}

// Close is idempotent. Admitted snapshots, replay readers and backend workers
// all join before retained directory descriptors and the file lock are released.
func (self *statsAggregateStore) Close() error {
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
	var err error
	if self.db != nil {
		err = self.db.Close()
	}
	if self.disk != nil {
		err = errors.Join(err, self.disk.Close())
	}
	self.stateLock.Lock()
	self.closeError = errors.Join(err, self.fault)
	close(self.closeDone)
	self.stateLock.Unlock()
	return self.closeError
}

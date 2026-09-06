//go:build linux || darwin

package validator

// One admitted owner stages fixed-width rows in bounded synchronous batches.
// Every row WAL is durable before later rotation or header publication; the
// copied pending lookup contains only the current batch, never history.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

const (
	statsAggregateBatchOperations  = 128
	statsAggregateBatchBytes       = 64 * 1024
	statsAggregateBatchHeaderBytes = 8 + 4
)

// A stage or recovery operation exclusively owns this object. Metadata hooks
// are synchronous; no background flush or retry outlives the admitted owner.
type statsAggregateBatch struct {
	store            *statsAggregateStore
	deletions        bool
	batch            *leveldb.Batch
	pendingKeyValues map[string][]byte
	failure          error
}

// Pinned LevelDB Dump excludes its journal batch header. Preallocation also
// covers appendRec's temporary varint reservation without growing the buffer.
func newStatsAggregateBatch(store *statsAggregateStore, deletions bool) *statsAggregateBatch {
	self := &statsAggregateBatch{store: store, deletions: deletions, batch: leveldb.MakeBatch(statsAggregateBatchBytes)}
	if !deletions {
		self.pendingKeyValues = make(map[string][]byte, statsAggregateBatchOperations)
	}
	return self
}

// Rejects malformed fixed-width storage operations before copying or flushing
// anything. Provider/claim semantic checks stay at the existing typed boundary.
func statsAggregateBatchRecordBytes(key, value []byte, deletions bool) (int, error) {
	if len(key) == 0 {
		return 0, errors.New("private aggregate batch key is empty")
	}
	valid := false
	switch key[0] {
	case 'p':
		valid = len(key) == 25 && (deletions || len(value) == statsAggregateProviderBytes)
	case 'e':
		valid = len(key) == 65 && len(value) == 0
	case 'c':
		valid = len(key) == 73 && (deletions || len(value) == statsAggregateClaimBytes)
	}
	if !valid || deletions && len(value) != 0 {
		return 0, errors.New("private aggregate batch key or value shape differs")
	}
	var encoded [binary.MaxVarintLen64]byte
	size := 1 + binary.PutUvarint(encoded[:], uint64(len(key))) + len(key)
	if !deletions {
		size += binary.PutUvarint(encoded[:], uint64(len(value))) + len(value)
	}
	if size > statsAggregateBatchBytes-statsAggregateBatchHeaderBytes {
		return 0, errors.New("private aggregate batch point exceeds its byte bound")
	}
	return size, nil
}

// An operation-local refusal cannot be retried or published through this stage;
// physical DB errors additionally fault the live storage owner.
func (self *statsAggregateBatch) fail(err error) error {
	if self.failure == nil {
		self.failure = err
	}
	return self.failure
}

// Retains every admitted operation, including repeated writes to one key.
// The read-through map stores only the latest copied value of that bounded set.
func (self *statsAggregateBatch) append(ctx context.Context, key, value []byte) error {
	if self.failure != nil {
		return self.failure
	}
	if err := self.store.check(ctx); err != nil {
		return self.fail(err)
	}
	size, err := statsAggregateBatchRecordBytes(key, value, self.deletions)
	if err != nil {
		return self.fail(err)
	}
	if self.batch.Len() == statsAggregateBatchOperations || len(self.batch.Dump())+statsAggregateBatchHeaderBytes+size > statsAggregateBatchBytes {
		if err := self.flush(ctx); err != nil {
			return err
		}
	}
	if self.deletions {
		self.batch.Delete(key)
	} else {
		self.batch.Put(key, value)
		self.pendingKeyValues[string(key)] = bytes.Clone(value)
		if self.store.hooks.PointStaged != nil {
			if err := self.store.hooks.PointStaged(key[0], len(key), len(value)); err != nil {
				return self.fail(err)
			}
		}
	}
	if err := self.store.check(ctx); err != nil {
		return self.fail(err)
	}
	if self.batch.Len() == statsAggregateBatchOperations {
		return self.flush(ctx)
	}
	return nil
}

// A present empty egress value is distinct from absence. Neither returned keys
// nor values borrow the batch buffer or another caller's mutable storage.
func (self *statsAggregateBatch) pending(key []byte) ([]byte, bool, error) {
	if self.failure != nil {
		return nil, false, self.failure
	}
	value, found := self.pendingKeyValues[string(key)]
	return bytes.Clone(value), found, nil
}

// Each bounded batch completes its own original-WAL Sync before another batch
// may rotate the journal. Only then may the final independent header be written.
func (self *statsAggregateBatch) flush(ctx context.Context) error {
	if self.failure != nil {
		return self.failure
	}
	if err := self.store.check(ctx); err != nil {
		return self.fail(err)
	}
	if self.batch.Len() == 0 {
		return nil
	}
	operations, encodedBytes := self.batch.Len(), len(self.batch.Dump())+statsAggregateBatchHeaderBytes
	if operations > statsAggregateBatchOperations || encodedBytes > statsAggregateBatchBytes {
		return self.fail(errors.New("private aggregate batch exceeded its fixed bounds"))
	}
	if err := self.store.db.Write(self.batch, &opt.WriteOptions{Sync: true, NoWriteMerge: true}); err != nil {
		self.store.latchFault(err)
		return self.fail(err)
	}
	self.batch.Reset()
	clear(self.pendingKeyValues)
	if self.store.hooks.BatchSynced != nil {
		kind := "rows"
		if self.deletions {
			kind = "recovery-deletes"
		}
		if err := self.store.hooks.BatchSynced(kind, operations, encodedBytes); err != nil {
			return self.fail(err)
		}
	}
	// A canceled post-Sync operation still has durable but unpublished rows.
	if err := self.store.check(ctx); err != nil {
		return self.fail(err)
	}
	return nil
}

//go:build linux || darwin

package validator

// Sealing copies a finite immutable range before handing records to visitors.
// Each copy is authenticated and enclosed by fresh physical custody checks;
// no backend owner, iterator or store lock survives the returned batch.

import (
	"context"
	"errors"

	"github.com/syndtr/goleveldb/leveldb/opt"
)

// Byte/count limits bound owned memory, not the signed stream's wire shape.
// One already-admitted oversized record may occupy a batch by itself; its
// existing per-record store bound remains authoritative.
func (self *attemptRecordStore) readBatch(ctx context.Context, first, last, maximumRecords, maximumBytes uint64) ([]AttemptRecord, error) {
	if maximumRecords == 0 || maximumBytes == 0 || maximumRecords > uint64(^uint(0)>>1) || maximumBytes > uint64(^uint(0)>>1) {
		return nil, errors.New("attempt record store batch bounds are invalid")
	}
	if err := self.begin(ctx); err != nil {
		return nil, err
	}
	defer self.active.Done()
	self.stateLock.Lock()
	committed := self.head.LastSequence
	self.stateLock.Unlock()
	if first == 0 || first > last || last > committed || last == ^uint64(0) {
		return nil, errors.New("attempt record store batch range is invalid")
	}
	if err := self.check(ctx); err != nil {
		return nil, err
	}
	recordTs := make([]AttemptRecord, 0, min(maximumRecords, last-first+1, 64))
	var copiedBytes uint64
	for sequence := first; sequence <= last && uint64(len(recordTs)) < maximumRecords; sequence++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case <-self.ctx.Done():
			return nil, errAttemptRecordStoreClosed
		default:
		}
		if copiedBytes >= maximumBytes {
			break
		}
		raw, err := self.db.Get(attemptStoreRecordKey(sequence), &opt.ReadOptions{Strict: opt.StrictAll})
		if err != nil {
			self.latchFault(err)
			return nil, errors.Join(errAttemptRecordStoreFaulted, err)
		}
		if len(recordTs) != 0 && uint64(len(raw)) > maximumBytes-copiedBytes {
			break
		}
		record, err := self.decodeRecord(raw)
		if err == nil && record.Sequence != sequence {
			err = errors.New("attempt record store key and sequence differ")
		}
		if err != nil {
			self.latchFault(err)
			return nil, errors.Join(errAttemptRecordStoreFaulted, err)
		}
		recordTs = append(recordTs, record)
		copiedBytes += uint64(len(raw))
	}
	if err := self.check(ctx); err != nil {
		return nil, err
	}
	return recordTs, nil
}

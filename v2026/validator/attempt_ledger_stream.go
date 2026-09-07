package validator

// Streaming access separates acknowledged disk history from the legacy
// materialized v1 API. Selecting disk mode is explicit and does not activate
// a producer or invent a compact-cut wire format.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

var (
	ErrAttemptLedgerStreamingRequired = errors.New("disk attempt ledger requires the streaming API and compact cuts")
	ErrAttemptLedgerDiskMigration     = errors.New("attempt ledger directory is reserved for disk migration")
	errAttemptLedgerClosed            = errors.New("attempt ledger is closing or closed")
	errAttemptLedgerFaulted           = errors.New("attempt ledger requires close and verified reopen")
)

// Every bound is explicit. Legacy bytes include blank lines and delimiters;
// proof bytes bound the complete projection spool. The single-record bound
// also bounds a proof entry; no M8 bound is silently applied to M16 records.
type AttemptLedgerDiskLimits struct {
	MaxRecordBytes    uint64
	MaxRecordCount    uint64
	MaxTrailCount     uint64
	MaxRawRecordBytes uint64
	MaxStorageBytes   uint64
	MaxStorageFiles   uint64
	MaxLegacyBytes    uint64
	MaxProofBytes     uint64
}

// All fields belong to one acknowledged prefix. Call Head for checked control
// decisions; LastSequence remains a compatibility display of the last ack.
type AttemptLedgerHead struct {
	LastSequence uint64 `json:"last_sequence"`
	Root         string `json:"root"`
	RecordBytes  uint64 `json:"record_bytes"`
	TrailCount   uint64 `json:"trail_count"`
}

// Platform storage remains private while the ledger API compiles everywhere.
type attemptLedgerDiskBackend interface {
	Head() (AttemptLedgerHead, error)
	Append(context.Context, AttemptRecord) error
	Walk(context.Context, uint64, uint64, func(AttemptRecord) error) error
	Pending(context.Context, func(AttemptRecord) error) error
	Close() error
	encodeRecord(AttemptRecord) ([]byte, error)
}

// A constructor installs lifecycle state before publishing the ledger.
func (self *AttemptLedger) initLifetime() {
	self.ctx, self.cancel = context.WithCancel(context.Background())
	self.appendGate = make(chan struct{}, 1)
	self.closeDone = make(chan struct{})
}

// Admission and Close share the state lock, so no Add races the final Wait.
func (self *AttemptLedger) begin(ctx context.Context) error {
	if ctx == nil {
		return errors.New("attempt ledger context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closing {
		return errAttemptLedgerClosed
	}
	if self.fault != nil {
		return errors.Join(errAttemptLedgerFaulted, self.fault)
	}
	self.active.Add(1)
	return nil
}

// Gates signing and persistence without retaining a mutex during callbacks.
func (self *AttemptLedger) acquireAppend(ctx context.Context) error {
	select {
	case self.appendGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-self.ctx.Done():
		return errAttemptLedgerClosed
	}
	if err := ctx.Err(); err != nil {
		<-self.appendGate
		return err
	}
	if self.ctx.Err() != nil {
		<-self.appendGate
		return errAttemptLedgerClosed
	}
	return nil
}

// Head refuses a faulted or closing owner instead of inventing an empty root.
func (self *AttemptLedger) Head() (AttemptLedgerHead, error) {
	return self.checkedHead()
}

// Legacy callers can migrate control decisions without assuming disk mode.
func (self *AttemptLedger) checkedHead() (AttemptLedgerHead, error) {
	if err := self.begin(context.Background()); err != nil {
		return AttemptLedgerHead{}, err
	}
	defer self.active.Done()
	if self.disk != nil {
		return self.disk.Head()
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return AttemptLedgerHead{LastSequence: uint64(len(self.records)), Root: self.lastRootWithLock()}, nil
}

// One owned record is delivered at a time through a fixed inclusive range.
// Visitors must return promptly and must not call Close or another Walk.
func (self *AttemptLedger) Walk(ctx context.Context, first, last uint64, visit func(AttemptRecord) error) error {
	if visit == nil {
		return errors.New("attempt ledger visitor is nil")
	}
	if err := self.begin(ctx); err != nil {
		return err
	}
	defer self.active.Done()
	if self.disk != nil {
		return self.disk.Walk(ctx, first, last, func(record AttemptRecord) error {
			if self.ctx.Err() != nil {
				return errAttemptLedgerClosed
			}
			if err := visit(record); err != nil {
				return err
			}
			if self.ctx.Err() != nil {
				return errAttemptLedgerClosed
			}
			return ctx.Err()
		})
	}
	self.stateLock.Lock()
	valid := first != 0 && first <= last && last <= uint64(len(self.records))
	self.stateLock.Unlock()
	if !valid {
		return errors.New("attempt ledger walk range is outside its durable prefix")
	}
	for sequence := first; ; sequence++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if self.ctx.Err() != nil {
			return errAttemptLedgerClosed
		}
		self.stateLock.Lock()
		record, err := cloneAttemptRecord(self.records[sequence-1])
		self.stateLock.Unlock()
		if err != nil {
			return err
		}
		if err := visit(record); err != nil {
			return err
		}
		if sequence == last {
			return ctx.Err()
		}
	}
}

// The context can cancel while queued. A started durability operation runs
// to its acknowledged result; cancellation does not hide a successful commit.
func (self *AttemptLedger) AppendContext(ctx context.Context, record AttemptRecord) (*AttemptRecord, error) {
	if err := self.begin(ctx); err != nil {
		return nil, err
	}
	defer self.active.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(self.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := self.acquireAppend(ctx); err != nil {
		return nil, err
	}
	defer func() { <-self.appendGate }()
	if self.disk != nil {
		return self.appendDiskWithLock(ctx, record)
	}
	directory, err := openAttemptLedgerDirectory(filepath.Dir(self.path), self.legacyDirectoryStep)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(directory.anchor, self.legacyAnchor) {
		return nil, errors.Join(errors.New("attempt ledger JSONL directory was replaced"), directory.Close())
	}
	if err := directory.enter(ctx); err != nil {
		return nil, errors.Join(err, directory.Close())
	}
	self.legacyWriteDirectory = directory
	defer func() { self.legacyWriteDirectory = nil }()
	var committed *AttemptRecord
	err = directory.requireLegacy()
	if err == nil {
		committed, err = self.appendLegacyWithLock(record)
	}
	gateErr := errors.Join(directory.leave(), directory.Close())
	if gateErr != nil {
		self.stateLock.Lock()
		self.fault = gateErr
		self.stateLock.Unlock()
	}
	err = errors.Join(err, gateErr)
	if err != nil {
		return nil, err
	}
	return committed, nil
}

// The append gate pins the checked head until the canonical signed batch is
// durable. The backend validates the exact lifecycle itself.
func (self *AttemptLedger) appendDiskWithLock(ctx context.Context, record AttemptRecord) (*AttemptRecord, error) {
	head, err := self.disk.Head()
	if err != nil {
		return nil, err
	}
	if head.LastSequence == ^uint64(0) {
		return nil, errors.New("attempt ledger sequence overflow")
	}
	record.Schema, record.Identity = attemptLedgerRecordSchema, self.identity
	record.Sequence, record.PreviousHash = head.LastSequence+1, head.Root
	record.VPK = append([]byte(nil), self.vpk...)
	// Validate bounded shape before hashing can allocate from caller slices.
	record.RecordHash, record.Signature = zeroAttemptHash(), make([]byte, ed25519.SignatureSize)
	raw, err := self.disk.encodeRecord(record)
	if err != nil {
		return nil, err
	}
	var owned AttemptRecord
	if err := json.Unmarshal(raw, &owned); err != nil {
		return nil, err
	}
	record = owned
	hash, err := attemptRecordHash(&record)
	if err != nil {
		return nil, err
	}
	record.RecordHash = attemptHex32(hash)
	record.Signature = ed25519.Sign(self.vsk, attemptRecordSignatureMessage(hash))
	if err := self.disk.Append(ctx, record); err != nil {
		return nil, err
	}
	self.stateLock.Lock()
	self.durableSequence = record.Sequence
	self.stateLock.Unlock()
	cloned, err := cloneAttemptRecord(record)
	return &cloned, err
}

// Recovery streams exact terminal checkpoints in trail-ID order. A returned
// error can follow committed recoveries; restart resumes from durable state.
// Visitors may read Head, but must not append, recover, walk or close this owner.
func (self *AttemptLedger) RecoverPendingContext(ctx context.Context, visit func(AttemptRecord) error) error {
	if visit == nil {
		return errors.New("attempt ledger recovery visitor is nil")
	}
	if self.disk == nil {
		if ctx == nil {
			return errors.New("attempt ledger context is nil")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		records, err := self.RecoverPending()
		if err != nil {
			return err
		}
		for _, record := range records {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := visit(record); err != nil {
				return err
			}
		}
		return nil
	}
	if err := self.begin(ctx); err != nil {
		return err
	}
	defer self.active.Done()
	if err := self.acquireAppend(ctx); err != nil {
		return err
	}
	defer func() { <-self.appendGate }()
	return self.disk.Pending(ctx, func(record AttemptRecord) error {
		if self.ctx.Err() != nil {
			return errAttemptLedgerClosed
		}
		record.Disposition, record.Proof = AttemptDispositionValidatorError, nil
		committed, err := self.appendDiskWithLock(ctx, record)
		if err != nil {
			return err
		}
		return visit(*committed)
	})
}

// Close cancels queued work and joins admitted visitors and backend workers.
// A visitor must never close the owner it is currently visiting.
func (self *AttemptLedger) Close() error {
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
	var err error
	if self.disk != nil {
		err = self.disk.Close()
	}
	self.active.Wait()
	err = errors.Join(err, self.directory.Close())
	self.stateLock.Lock()
	self.closeError = errors.Join(err, self.fault)
	close(self.closeDone)
	self.stateLock.Unlock()
	return self.closeError
}

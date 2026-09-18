package validator

// Proof JSONL is a derived projection. Disk-ledger recovery verifies every
// existing byte against the signed census before atomically replacing a
// missing or matching torn suffix from a private bounded spool.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var errAttemptProofProjectionFaulted = errors.New("attempt proof projection requires a new owner and verified reconciliation")

// The mutex initializes a single operation token and is released before any
// wait, I/O or hook. Disk callers can cancel while another projection is busy.
func (self *ProofStore) acquireOperation(ctx context.Context) (func(), error) {
	if ctx == nil {
		return nil, errors.New("proof store context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self.mu.Lock()
	if self.operationGate == nil {
		self.operationGate = make(chan struct{}, 1)
	}
	gate, step := self.operationGate, self.projectionStep
	self.mu.Unlock()
	select {
	case gate <- struct{}{}:
	default:
		if step != nil {
			if err := step("proof-gate-contended", ""); err != nil {
				return nil, err
			}
		}
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		<-gate
		return nil, err
	}
	return func() { <-gate }, nil
}

// A new ProofStore can reconcile after uncertainty; an existing faulted owner
// cannot guess whether its previous replacement or append became durable.
func (self *ProofStore) ReconcileAttemptProofsContext(ctx context.Context, ledger *AttemptLedger) error {
	if ledger == nil || ctx == nil {
		return errors.New("attempt proof reconciliation context or ledger is nil")
	}
	if ledger.disk == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return self.ReconcileAttemptProofs(ledger)
	}
	return self.reconcileDiskAttemptProofs(ctx, ledger, false)
}

// Normal completion visits only records after the checked projection cursor.
// Startup and a new owner always verify the complete existing prefix first.
func (self *ProofStore) projectDiskAttemptProofs(ctx context.Context, ledger *AttemptLedger) error {
	return self.reconcileDiskAttemptProofs(ctx, ledger, true)
}

// One cancellable token serializes the projection owner; the directory gate
// excludes separate upgraded ProofStore objects and the migration constructor.
// No callback can call this ProofStore while it owns reconciliation.
func (self *ProofStore) reconcileDiskAttemptProofs(ctx context.Context, ledger *AttemptLedger, incremental bool) (resultErr error) {
	release, err := self.acquireOperation(ctx)
	if err != nil {
		return err
	}
	defer release()
	if self.projectionFault != nil {
		return errors.Join(errAttemptProofProjectionFaulted, self.projectionFault)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	self.projectionDirty = true
	mutatedAuthority := false
	defer func() {
		if resultErr != nil && (mutatedAuthority || !errors.Is(resultErr, context.Canceled) && !errors.Is(resultErr, context.DeadlineExceeded)) {
			self.projectionFault = resultErr
		}
	}()
	directory, err := openAttemptLedgerDirectory(filepath.Dir(self.path), self.projectionStep)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	if err := directory.enter(ctx); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.leave()) }()
	head, err := ledger.checkedHead()
	if err != nil {
		return err
	}
	name := filepath.Base(self.path)
	actual, err := directory.openFile(name, os.O_RDONLY, false)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var actualInfo os.FileInfo
	if actual != nil {
		defer func() { resultErr = errors.Join(resultErr, actual.Close()) }()
		actualInfo, err = actual.Stat()
		if err != nil || actualInfo.Size() < 0 || uint64(actualInfo.Size()) > ledger.diskLimits.MaxProofBytes {
			return errors.Join(errors.New("attempt proof file exceeds its explicit byte bound"), err)
		}
	}
	if incremental && self.projectionLedger == ledger {
		if head.LastSequence < self.projectionSequence || !attemptLedgerSameFileState(actualInfo, self.projectionFileInfo) || uint64(actualInfo.Size()) != self.projectionBytes {
			return errors.New("attempt proof projection cursor or file changed since its last acknowledgement")
		}
		if head.LastSequence == self.projectionSequence {
			self.projectionDirty = false
			return nil
		}
		file, err := directory.openFile(name, os.O_WRONLY|os.O_APPEND, false)
		if err != nil {
			return err
		}
		defer func() {
			if file != nil {
				resultErr = errors.Join(resultErr, file.Close())
			}
		}()
		proofBytes, proofCount := self.projectionBytes, self.projectionProofCount
		if err := ledger.Walk(ctx, self.projectionSequence+1, head.LastSequence, func(record AttemptRecord) error {
			if record.Disposition != AttemptDispositionComplete {
				return nil
			}
			raw, err := attemptProjectionRecord(record, ledger.diskLimits, proofBytes, proofCount)
			if err != nil {
				return err
			}
			mutatedAuthority = true
			if self.projectionStep != nil {
				if err := self.projectionStep("proof-append", name); err != nil {
					return err
				}
			}
			if n, err := file.Write(raw); err != nil {
				return err
			} else if n != len(raw) {
				return io.ErrShortWrite
			}
			proofBytes += uint64(len(raw))
			proofCount++
			return nil
		}); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil || info.Size() < 0 || uint64(info.Size()) != proofBytes {
			return errors.Join(errors.New("attempt proof append byte count differs"), err)
		}
		err = file.Close()
		file = nil
		if err != nil {
			return err
		}
		if err := directory.sync("proof-append"); err != nil {
			return err
		}
		self.projectionSequence, self.projectionBytes, self.projectionProofCount = head.LastSequence, proofBytes, proofCount
		self.projectionFileInfo, self.projectionDirty = info, false
		return nil
	}
	spoolName := name + ".attempt-spool"
	spool, err := directory.openFile(spoolName, os.O_RDWR|os.O_CREATE|os.O_EXCL, true)
	if errors.Is(err, os.ErrExist) {
		spool, err = directory.openFile(spoolName, os.O_RDWR, false)
	}
	if err != nil {
		return err
	}
	defer func() {
		if spool != nil {
			resultErr = errors.Join(resultErr, spool.Close())
		}
	}()
	spoolInfo, err := spool.Stat()
	if err != nil || spoolInfo.Size() < 0 || uint64(spoolInfo.Size()) > ledger.diskLimits.MaxProofBytes {
		return errors.Join(errors.New("attempt proof scratch spool exceeds its bound"), err)
	}
	// Only this designated derived scratch inode is reset. The original proof
	// file remains untouched until the complete exact byte census succeeds.
	if err := spool.Truncate(0); err != nil {
		return err
	}
	var proofBytes, proofCount uint64
	if head.LastSequence != 0 {
		if err := ledger.Walk(ctx, 1, head.LastSequence, func(record AttemptRecord) error {
			if record.Disposition != AttemptDispositionComplete {
				return nil
			}
			raw, err := attemptProjectionRecord(record, ledger.diskLimits, proofBytes, proofCount)
			if err != nil {
				return err
			}
			if actual != nil {
				part := make([]byte, len(raw))
				n, err := io.ReadFull(actual, part)
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
					return err
				}
				if !bytes.Equal(raw[:n], part[:n]) {
					return errors.New("attempt proof projection conflicts with the signed ledger")
				}
			}
			if n, err := spool.Write(raw); err != nil {
				return err
			} else if n != len(raw) {
				return io.ErrShortWrite
			}
			proofBytes += uint64(len(raw))
			proofCount++
			if self.projectionStep != nil {
				return self.projectionStep("proof-spool-record", name)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if actual != nil {
		var extra [1]byte
		if n, err := actual.Read(extra[:]); n != 0 || !errors.Is(err, io.EOF) {
			return errors.Join(errors.New("attempt proof projection contains orphan or duplicate trailing bytes"), err)
		}
		current, err := directory.root.Lstat(name)
		if err != nil || !attemptLedgerSameFileState(current, actualInfo) {
			return errors.New("attempt proof projection changed while verifying its prefix")
		}
	} else if _, err := directory.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errors.New("attempt proof projection appeared during reconciliation"), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if self.projectionStep != nil {
		if err := self.projectionStep("proof-spool-sync", spoolName); err != nil {
			return err
		}
	}
	if err := spool.Sync(); err != nil {
		return err
	}
	spoolInfo, err = spool.Stat()
	if err != nil || spoolInfo.Size() < 0 || uint64(spoolInfo.Size()) != proofBytes {
		return errors.Join(errors.New("attempt proof spool byte count differs"), err)
	}
	err = spool.Close()
	spool = nil
	if err != nil {
		return err
	}
	current, err := directory.root.Lstat(spoolName)
	if err != nil || !attemptLedgerPrivateFile(current) || !os.SameFile(current, spoolInfo) {
		return errors.New("attempt proof scratch inode changed before publication")
	}
	if err := directory.check(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if self.projectionStep != nil {
		if err := self.projectionStep("proof-rename", name); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	mutatedAuthority = true
	if err := directory.root.Rename(spoolName, name); err != nil {
		return err
	}
	if err := directory.sync("proof-replace"); err != nil {
		return err
	}
	publishedInfo, err := directory.root.Lstat(name)
	if err != nil || !attemptLedgerPrivateFile(publishedInfo) || !os.SameFile(publishedInfo, spoolInfo) || publishedInfo.Size() != spoolInfo.Size() {
		return errors.Join(errors.New("attempt proof replacement inode changed before acknowledgement"), err)
	}
	self.projectionLedger, self.projectionSequence = ledger, head.LastSequence
	self.projectionBytes, self.projectionProofCount = proofBytes, proofCount
	self.projectionFileInfo, self.projectionDirty = publishedInfo, false
	return nil
}

// The record has already passed the store's bounded shape/signature checks.
// Both the individual entry and complete projection have explicit byte/count
// limits; retries never drop a proof to fit a budget.
func attemptProjectionRecord(record AttemptRecord, limits AttemptLedgerDiskLimits, priorBytes, priorCount uint64) ([]byte, error) {
	if record.Proof == nil || priorCount >= limits.MaxTrailCount {
		return nil, errors.New("attempt proof is missing or exceeds the complete census count")
	}
	raw, err := json.Marshal(record.Proof)
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) > limits.MaxRecordBytes || priorBytes > limits.MaxProofBytes || uint64(len(raw))+1 > limits.MaxProofBytes-priorBytes {
		return nil, errors.New("attempt proof projection exceeds its explicit byte bound")
	}
	return append(raw, '\n'), nil
}

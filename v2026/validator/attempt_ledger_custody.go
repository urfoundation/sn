//go:build linux || darwin

package validator

// The external checkpoint belongs to the retained ledger directory, not to
// LevelDB's replaceable files. It fences acknowledged-prefix rollback across
// process lifetimes and retains an uncertain record without signing it again.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

const attemptLedgerCustodySchema = "urnetwork-validator-attempt-ledger-custody-v1"
const attemptLedgerCustodyAttribute = "user.urnetwork.attempt-ledger-custody"
const attemptLedgerPendingName = "attempt-ledger-pending.json"

type attemptLedgerCustodyMember struct {
	Inode  uint64 `json:"inode"`
	Bytes  uint64 `json:"bytes"`
	Sha256 string `json:"sha256"`
}

// A zero pending inode means the intended bytes were pinned before publication.
// Its absence or a partial file cannot authorize a different record or reset.
type attemptLedgerCustodyPending struct {
	Head   AttemptLedgerHead          `json:"head"`
	Record attemptLedgerCustodyMember `json:"record"`
}

type attemptLedgerCustodyCheckpoint struct {
	Schema         string                       `json:"schema"`
	Identity       AttemptLedgerIdentity        `json:"identity"`
	Coordinator    string                       `json:"coordinator"`
	DirectoryInode uint64                       `json:"directory_inode"`
	DatabaseInode  uint64                       `json:"database_inode"`
	Import         attemptLedgerCustodyMember   `json:"import"`
	Ready          attemptLedgerCustodyMember   `json:"ready"`
	Legacy         *attemptLedgerCustodyMember  `json:"legacy,omitempty"`
	Committed      AttemptLedgerHead            `json:"committed"`
	Pending        *attemptLedgerCustodyPending `json:"pending,omitempty"`
}

// The mutex serializes checkpoint reads and replacements, including concurrent
// public Head calls. The store's append gate owns the complete mutation sequence.
type attemptLedgerCustody struct {
	stateLock  sync.Mutex
	directory  *attemptLedgerDirectory
	checkpoint attemptLedgerCustodyCheckpoint
	raw        []byte
	limit      uint64
}

func attemptLedgerCustodyDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func attemptLedgerCustodyLoss(detail string, cause error) error {
	return errors.Join(durablevolume.ErrIdentity, errors.New(detail), cause)
}

// Failed observations do not prove loss. Missing retained members do.
func attemptLedgerCustodyObservation(detail string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return attemptLedgerCustodyLoss(detail, err)
	}
	return errors.Join(&durablevolume.UnavailableError{Reason: detail}, err)
}

// Hashing is bounded by both the caller's exact member limit and cancellation.
func attemptLedgerCustodyRead(ctx context.Context, directory *attemptLedgerDirectory, name string, limit uint64) (_ []byte, _ attemptLedgerCustodyMember, resultErr error) {
	if err := errors.Join(ctx.Err(), directory.check()); err != nil {
		return nil, attemptLedgerCustodyMember{}, err
	}
	file, err := directory.openFile(name, os.O_RDONLY, false)
	if err != nil {
		return nil, attemptLedgerCustodyMember{}, attemptLedgerCustodyObservation("cannot observe retained ledger member", err)
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, attemptLedgerCustodyMember{}, attemptLedgerCustodyObservation("cannot observe retained ledger descriptor", err)
	}
	if info.Size() < 0 || uint64(info.Size()) > limit {
		return nil, attemptLedgerCustodyMember{}, attemptLedgerCustodyLoss("retained ledger member exceeds its bound", nil)
	}
	_, inode, err := attemptLedgerLocalFileID(info)
	if err != nil {
		return nil, attemptLedgerCustodyMember{}, err
	}
	raw := make([]byte, 0, int(info.Size()))
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, attemptLedgerCustodyMember{}, err
		}
		n, readErr := file.Read(buffer)
		if uint64(n) > limit-uint64(len(raw)) {
			return nil, attemptLedgerCustodyMember{}, attemptLedgerCustodyLoss("retained ledger member grew past its bound", nil)
		}
		raw = append(raw, buffer[:n]...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, attemptLedgerCustodyMember{}, attemptLedgerCustodyObservation("cannot read retained ledger member", readErr)
		}
	}
	current, err := directory.root.Lstat(name)
	if err != nil {
		return nil, attemptLedgerCustodyMember{}, attemptLedgerCustodyObservation("cannot reobserve retained ledger member", err)
	}
	if !attemptLedgerPrivateFile(current) || !os.SameFile(info, current) || info.Size() != current.Size() || uint64(len(raw)) != uint64(info.Size()) {
		return nil, attemptLedgerCustodyMember{}, attemptLedgerCustodyLoss("retained ledger member changed while reading", nil)
	}
	return raw, attemptLedgerCustodyMember{Inode: inode, Bytes: uint64(len(raw)), Sha256: attemptLedgerCustodyDigest(raw)}, errors.Join(ctx.Err(), directory.check())
}

// Runtime admission never creates or repairs a missing checkpoint. The offline
// provisioner must have joined former writers and retained the imported prefix.
func openAttemptLedgerCustody(ctx context.Context, directory *attemptLedgerDirectory, identity AttemptLedgerIdentity, coordinator string, limits AttemptLedgerDiskLimits) (*attemptLedgerCustody, error) {
	if directory.storage == nil {
		return nil, nil
	}
	raw, err := readAttemptLedgerCustodyAttribute(directory.directory)
	if err != nil {
		return nil, err
	}
	var checkpoint attemptLedgerCustodyCheckpoint
	if err := attemptStoreDecode(raw, &checkpoint); err != nil || checkpoint.Schema != attemptLedgerCustodySchema || checkpoint.Identity != identity || checkpoint.Coordinator != coordinator {
		return nil, attemptLedgerCustodyLoss("attempt ledger custody namespace is absent or invalid", err)
	}
	_, inode, err := attemptLedgerLocalFileID(directory.anchor)
	if err != nil || inode != checkpoint.DirectoryInode {
		return nil, attemptLedgerCustodyLoss("attempt ledger custody directory differs", err)
	}
	info, err := directory.root.Lstat(attemptLedgerStoreName)
	if err != nil {
		return nil, attemptLedgerCustodyObservation("attempt ledger database directory is missing", err)
	}
	if !attemptLedgerPrivateDirectory(info) {
		return nil, attemptLedgerCustodyLoss("attempt ledger database directory is not private", nil)
	}
	_, inode, err = attemptLedgerLocalFileID(info)
	if err != nil || inode != checkpoint.DatabaseInode {
		return nil, attemptLedgerCustodyLoss("attempt ledger database directory differs", err)
	}
	for _, member := range []struct {
		name string
		want attemptLedgerCustodyMember
		max  uint64
	}{{attemptLedgerImportName, checkpoint.Import, limits.MaxRecordBytes*6 + 4096}, {attemptLedgerReadyName, checkpoint.Ready, 4096}} {
		_, observed, err := attemptLedgerCustodyRead(ctx, directory, member.name, member.max)
		if err != nil {
			return nil, err
		}
		if observed != member.want {
			return nil, attemptLedgerCustodyLoss("attempt ledger import custody differs", nil)
		}
	}
	if checkpoint.Legacy == nil {
		if _, err := directory.root.Lstat(attemptLedgerLegacyName); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return nil, attemptLedgerCustodyObservation("cannot observe legacy ledger absence", err)
			}
			return nil, attemptLedgerCustodyLoss("unexpected legacy ledger appeared", nil)
		}
	} else {
		// The unchanged import implementation streams and checks its exact legacy
		// digest below. Here only its retained physical member is admitted.
		info, err := directory.root.Lstat(attemptLedgerLegacyName)
		if err != nil {
			return nil, attemptLedgerCustodyObservation("cannot observe retained legacy ledger", err)
		}
		_, inode, err := attemptLedgerLocalFileID(info)
		if err != nil || !attemptLedgerPrivateFile(info) || info.Size() < 0 || uint64(info.Size()) != checkpoint.Legacy.Bytes || inode != checkpoint.Legacy.Inode {
			return nil, attemptLedgerCustodyLoss("legacy ledger physical custody differs", err)
		}
	}
	if checkpoint.Committed.LastSequence > limits.MaxRecordCount || checkpoint.Committed.RecordBytes > limits.MaxRawRecordBytes || checkpoint.Committed.TrailCount > limits.MaxTrailCount {
		return nil, attemptLedgerCustodyLoss("attempt ledger custody head exceeds its approved bounds", nil)
	}
	if _, err := canonicalAttemptHex32("custody committed root", checkpoint.Committed.Root, true); err != nil {
		return nil, attemptLedgerCustodyLoss("attempt ledger custody head is invalid", err)
	}
	if pending := checkpoint.Pending; pending != nil {
		if checkpoint.Committed.LastSequence == ^uint64(0) || pending.Head.LastSequence != checkpoint.Committed.LastSequence+1 || pending.Record.Bytes == 0 || pending.Record.Bytes > limits.MaxRecordBytes || pending.Head.RecordBytes != checkpoint.Committed.RecordBytes+pending.Record.Bytes || pending.Head.TrailCount < checkpoint.Committed.TrailCount || pending.Head.TrailCount > checkpoint.Committed.TrailCount+1 {
			return nil, attemptLedgerCustodyLoss("pending ledger custody does not extend its retained head", nil)
		}
	}
	return &attemptLedgerCustody{directory: directory, checkpoint: checkpoint, raw: raw, limit: limits.MaxRecordBytes}, nil
}

func (self *attemptLedgerCustody) check(ctx context.Context) error {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.checkWithLock(ctx)
}

func (self *attemptLedgerCustody) checkWithLock(ctx context.Context) error {
	if err := errors.Join(ctx.Err(), self.directory.check()); err != nil {
		return err
	}
	raw, err := readAttemptLedgerCustodyAttribute(self.directory.directory)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, self.raw) {
		return attemptLedgerCustodyLoss("attempt ledger custody changed outside its owner", nil)
	}
	return nil
}

// A pre-admission failure is retryable. Once xattr replacement starts, only a
// joined reopen may decide which exact checkpoint survived the write barrier.
func (self *attemptLedgerCustody) publish(ctx context.Context, next attemptLedgerCustodyCheckpoint, stage string) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := self.checkWithLock(ctx); err != nil {
		return err
	}
	if err := self.directory.storage.CheckWrite(); err != nil {
		return err
	}
	raw, err := json.Marshal(next)
	if err != nil || len(raw) > 4096 {
		return errors.Join(errors.New("attempt ledger custody checkpoint exceeds its bound"), err)
	}
	if err := replaceAttemptLedgerCustodyAttribute(self.directory.directory, raw); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	self.checkpoint, self.raw = next, raw
	if self.directory.step != nil {
		if err := self.directory.step("custody-"+stage+"-written", ""); err != nil {
			return errors.Join(ErrDurablePublicationUncertain, err)
		}
	}
	if err := errors.Join(ctx.Err(), self.directory.directory.Sync()); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	if self.directory.step != nil {
		if err := self.directory.step("custody-"+stage+"-synced", ""); err != nil {
			return errors.Join(ErrDurablePublicationUncertain, err)
		}
	}
	if err := self.checkWithLock(ctx); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	return nil
}

// The original signed record is retained before the database sees it. A crash
// before complete publication leaves the expected hash explicit and blocked.
func (self *attemptLedgerCustody) begin(ctx context.Context, head AttemptLedgerHead, raw []byte) error {
	if self == nil {
		return nil
	}
	if pending := self.checkpoint.Pending; pending != nil {
		if pending.Head != head || pending.Record.Bytes != uint64(len(raw)) || pending.Record.Sha256 != attemptLedgerCustodyDigest(raw) {
			return attemptLedgerCustodyLoss("replayed ledger record differs from original pending bytes", nil)
		}
		return self.check(ctx)
	}
	if _, err := self.directory.root.Lstat(attemptLedgerPendingName); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(ErrDurablePublicationUncertain, errors.New("retained ledger pending member requires reconciliation"), err)
	}
	next := self.checkpoint
	next.Pending = &attemptLedgerCustodyPending{Head: head, Record: attemptLedgerCustodyMember{Bytes: uint64(len(raw)), Sha256: attemptLedgerCustodyDigest(raw)}}
	if err := self.publish(ctx, next, "pending"); err != nil {
		return err
	}
	file, err := self.directory.openFile(attemptLedgerPendingName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, true)
	if err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	for offset := 0; offset < len(raw); {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrDurablePublicationUncertain, err, file.Close())
		}
		n, writeErr := file.Write(raw[offset:min(offset+64*1024, len(raw))])
		offset += n
		if writeErr != nil || n == 0 {
			return errors.Join(ErrDurablePublicationUncertain, writeErr, io.ErrShortWrite, file.Close())
		}
	}
	err = errors.Join(file.Sync(), file.Close(), self.directory.sync("custody-record"))
	if err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	_, member, err := attemptLedgerCustodyRead(ctx, self.directory, attemptLedgerPendingName, self.limit)
	if err != nil || member.Bytes != uint64(len(raw)) || member.Sha256 != attemptLedgerCustodyDigest(raw) {
		return errors.Join(ErrDurablePublicationUncertain, errors.New("pending ledger publication differs"), err)
	}
	next.Pending = &attemptLedgerCustodyPending{Head: head, Record: member}
	return self.publish(ctx, next, "record")
}

// Cleanup follows the committed checkpoint barrier. A failed cleanup is still
// uncertain to this instance; reopen authenticates the already committed tail.
func (self *attemptLedgerCustody) complete(ctx context.Context, head AttemptLedgerHead) error {
	if self == nil {
		return nil
	}
	next := self.checkpoint
	if next.Pending == nil || next.Pending.Head != head {
		return attemptLedgerCustodyLoss("ledger completion has no matching pending checkpoint", nil)
	}
	member := next.Pending.Record
	next.Committed, next.Pending = head, nil
	if err := self.publish(ctx, next, "committed"); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	return self.removePending(ctx, member)
}

func (self *attemptLedgerCustody) removePending(ctx context.Context, member attemptLedgerCustodyMember) error {
	if err := self.check(ctx); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	if err := self.directory.storage.CheckWrite(); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	if self.directory.step != nil {
		if err := self.directory.step("custody-before-cleanup", ""); err != nil {
			return errors.Join(ErrDurablePublicationUncertain, err)
		}
	}
	info, err := self.directory.root.Lstat(attemptLedgerPendingName)
	if err != nil {
		return attemptLedgerCustodyObservation("cannot observe ledger cleanup member", err)
	}
	_, inode, err := attemptLedgerLocalFileID(info)
	if err != nil || !attemptLedgerPrivateFile(info) || info.Size() < 0 || inode != member.Inode || uint64(info.Size()) != member.Bytes {
		return attemptLedgerCustodyLoss("ledger cleanup member was replaced", err)
	}
	if err := self.directory.root.Remove(attemptLedgerPendingName); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	if err := errors.Join(self.directory.sync("custody-cleanup"), ctx.Err()); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	return nil
}

// Startup performs at most one original-record replay. Unknown/partial bytes
// remain retained; neither a new signature nor a reset repairs that uncertainty.
func (self *attemptLedgerCustody) reconcile(ctx context.Context, store *attemptRecordStore) error {
	if self == nil {
		return nil
	}
	pending := self.checkpoint.Pending
	if pending == nil && store.head != self.checkpoint.Committed {
		return attemptLedgerCustodyLoss("database lost its externally acknowledged ledger head", nil)
	}
	if pending != nil && store.head != self.checkpoint.Committed && store.head != pending.Head {
		return attemptLedgerCustodyLoss("database is neither retained ledger checkpoint", nil)
	}
	_, err := self.directory.root.Lstat(attemptLedgerPendingName)
	if errors.Is(err, os.ErrNotExist) {
		if pending == nil {
			return nil
		}
		if pending.Record.Inode != 0 {
			return attemptLedgerCustodyLoss("retained original pending ledger record is missing", err)
		}
		return errors.Join(ErrDurablePublicationUncertain, errors.New("original pending ledger record publication did not finish"), err)
	}
	if err != nil {
		return attemptLedgerCustodyObservation("cannot observe pending ledger record", err)
	}
	raw, member, err := attemptLedgerCustodyRead(ctx, self.directory, attemptLedgerPendingName, self.limit)
	if err != nil {
		return err
	}
	record, err := store.decodeRecord(raw)
	if err != nil {
		return errors.Join(ErrDurablePublicationUncertain, errors.New("pending ledger record is not complete original signed bytes"), err)
	}
	if pending == nil {
		if record.Sequence != store.head.LastSequence || record.RecordHash != store.head.Root {
			return attemptLedgerCustodyLoss("unexpected ledger record exists outside its custody checkpoint", nil)
		}
		retained, err := store.readRecord(record.Sequence)
		if err != nil {
			return err
		}
		retainedRaw, err := json.Marshal(retained)
		if err != nil || !bytes.Equal(raw, retainedRaw) {
			return attemptLedgerCustodyLoss("completed pending ledger bytes differ from the database", err)
		}
		return self.removePending(ctx, member)
	}
	if pending.Record.Bytes != member.Bytes || pending.Record.Sha256 != member.Sha256 || pending.Record.Inode != 0 && pending.Record.Inode != member.Inode || record.Sequence != pending.Head.LastSequence || record.RecordHash != pending.Head.Root || record.PreviousHash != self.checkpoint.Committed.Root {
		return attemptLedgerCustodyLoss("original pending ledger record differs from its checkpoint", nil)
	}
	if pending.Record.Inode == 0 {
		next := self.checkpoint
		next.Pending = &attemptLedgerCustodyPending{Head: pending.Head, Record: member}
		if err := self.publish(ctx, next, "record-recovered"); err != nil {
			return err
		}
	}
	if store.head == pending.Head {
		retained, err := store.readRecord(record.Sequence)
		if err != nil {
			return err
		}
		retainedRaw, err := json.Marshal(retained)
		if err != nil || !bytes.Equal(raw, retainedRaw) {
			return attemptLedgerCustodyLoss("committed pending ledger record differs from original bytes", err)
		}
		return self.complete(ctx, pending.Head)
	}
	return store.Append(ctx, record)
}

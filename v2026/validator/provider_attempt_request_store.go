// Original request publication uses the already protected validator directory.
// Its independent xattr never changes the assignment ledger's signed grammar.
package validator

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"sync"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Append/Walk/Head are serialized through the retained directory's flock and
// an instance lock. Close runs only after the containing trail owner has joined.
type ProviderAttemptRequestJournal struct {
	stateLock   sync.Mutex
	directory   *attemptLedgerDirectory
	identity    ProviderAttemptRequestIdentity
	limits      ProviderAttemptRequestLimits
	key         ed25519.PrivateKey
	checkpoint  *ProviderAttemptRequestCheckpoint
	file        *os.File
	observed    os.FileInfo
	failure     error
	dirty       bool
	active      map[uint64]uint64
	preparation ProviderAttemptRequestPreparation
	step        func(string) error
}

// Open only an explicitly prepared original owner. Even complete absence is
// refusal: a restart never mints a replacement birth or request allowance.
func OpenProviderAttemptRequestJournal(ctx context.Context, path string, preparation ProviderAttemptRequestPreparation, key ed25519.PrivateKey) (_ *ProviderAttemptRequestJournal, resultErr error) {
	if ctx == nil {
		return nil, errors.New("provider request owner context absent")
	}
	identity, limits := preparation.Identity, preparation.Limits
	if err := errors.Join(ctx.Err(), preparation.Validate()); err != nil {
		return nil, err
	}
	public, err := canonicalAttemptHex32("provider request signing key", identity.Ledger.ValidatorVPK, false)
	if err != nil {
		return nil, err
	}
	if len(key) != ed25519.PrivateKeySize || !bytes.Equal(key, ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])) || !bytes.Equal(key[ed25519.SeedSize:], public[:]) {
		return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request private key differs from original lane"))
	}
	directory, err := openAttemptLedgerDirectory(path, nil, ctx)
	if err != nil {
		return nil, err
	}
	self := &ProviderAttemptRequestJournal{directory: directory, identity: identity, limits: limits, key: slices.Clone(key), preparation: preparation}
	complete := false
	defer func() {
		if !complete {
			resultErr = errors.Join(resultErr, self.Close())
		}
	}()
	if err := directory.enter(ctx); err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.leave()) }()
	raw, absent, err := readProviderAttemptRequestAttribute(directory.directory)
	if err != nil {
		return nil, err
	}
	if absent {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("provider request original preparation anchor is absent"))
	}
	var checkpoint ProviderAttemptRequestCheckpoint
	if err := attemptStoreDecode(raw, &checkpoint); err != nil {
		return nil, err
	}
	if err := self.validateCheckpoint(checkpoint); err != nil {
		return nil, err
	}
	self.checkpoint = &checkpoint
	if err := self.openFileAndRecover(ctx); err != nil {
		return nil, err
	}
	if err := self.walk(ctx, nil); err != nil {
		return nil, err
	}

	complete = true
	return self, nil
}

// The portable head is accepted only inside the same actual directory inode.
func (self *ProviderAttemptRequestJournal) validateCheckpoint(value ProviderAttemptRequestCheckpoint) error {
	_, inode, err := attemptLedgerLocalFileID(self.directory.anchor)
	if err != nil {
		return err
	}
	if value.DirectoryInode != inode || value.FileInode == 0 {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request original physical preparation differs"))
	}
	if err := validateProviderAttemptRequestCheckpoint(value, self.preparation); err != nil {
		return err
	}

	return validateAttemptBoundary(value.Birth)
}

// Persisting the checkpoint includes the directory barrier and exact readback.
func (self *ProviderAttemptRequestJournal) saveCheckpoint(create bool) error {
	raw, err := json.Marshal(self.checkpoint)
	if err != nil {
		return err
	}
	if len(raw) > 4096 {
		return protocol.ErrProviderAttemptsCapacity
	}
	if err := writeProviderAttemptRequestAttribute(self.directory.directory, raw, create); err != nil {
		return err
	}
	if err := self.directory.sync("provider-request-checkpoint"); err != nil {
		return err
	}
	observed, absent, err := readProviderAttemptRequestAttribute(self.directory.directory)
	if err != nil {
		return err
	}
	if absent || !bytes.Equal(observed, raw) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request checkpoint publication differs"))
	}
	return nil
}

// Original pending bytes may be replayed only as the exact next sequence.
func (self *ProviderAttemptRequestJournal) openFileAndRecover(ctx context.Context) error {
	if _, err := self.directory.root.Lstat(ProviderAttemptRequestPendingName + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return errors.Join(protocol.ErrProviderAttemptsUnavailable, errors.New("provider request incomplete original publication requires retained recovery"))
	}
	file, err := self.directory.openFile(ProviderAttemptRequestJournalName, os.O_RDWR, false)
	if err != nil {
		return err
	}
	if self.file != nil {
		if err := self.file.Close(); err != nil {
			_ = file.Close()
			return err
		}
	}
	self.file = file
	info, err := file.Stat()
	if err != nil {
		return err
	}
	_, inode, err := attemptLedgerLocalFileID(info)
	if err != nil {
		return err
	}
	if self.checkpoint.FileInode != inode {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request file inode differs"))
	}
	self.observed = info
	// Authenticate the old prefix before any interrupted append is advanced.
	if err := self.walk(ctx, nil); err != nil {
		return err
	}
	pending, err := self.directory.readSmall(ProviderAttemptRequestPendingName, self.limits.MaxRecordBytes)
	if errors.Is(err, os.ErrNotExist) {
		if self.checkpoint.Pending != nil {
			return errors.Join(durablevolume.ErrIdentity, errors.New("provider request pending original disappeared"))
		}
		if info.Size() != int64(self.checkpoint.Committed.Bytes) {
			return errors.Join(durablevolume.ErrIdentity, errors.New("provider request committed byte prefix differs"))
		}
		return nil
	}
	if err != nil {
		return err
	}
	var record ProviderAttemptRequestRecord
	if err := attemptStoreDecode(pending, &record); err != nil {
		return err
	}
	hash, err := record.Hash()
	if err != nil {
		return err
	}
	if self.checkpoint.Pending == nil && record.Sequence == self.checkpoint.Committed.Sequence && hash == self.checkpoint.Committed.Hash {
		if info.Size() != int64(self.checkpoint.Committed.Bytes) {
			return errors.Join(durablevolume.ErrIdentity, errors.New("provider request committed recovery length differs"))
		}
		return self.removePending(pending)
	}
	if err := VerifyProviderAttemptRequest(ctx, record, self.identity, self.checkpoint.Committed, self.limits); err != nil {
		return err
	}
	if err := verifyProviderRequestAfterClose(self.checkpoint.Closed, record); err != nil {
		return err
	}
	if self.checkpoint.Pending == nil {
		self.checkpoint.Pending = &ProviderAttemptRequestPending{Bytes: uint64(len(pending)), Hash: sha256.Sum256(pending)}
		if err := self.saveCheckpoint(false); err != nil {
			return err
		}
	}
	if self.checkpoint.Pending.Bytes != uint64(len(pending)) || self.checkpoint.Pending.Hash != sha256.Sum256(pending) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request pending bytes differ"))
	}
	return self.commitPending(ctx, record, pending)
}

// Detect name replacement, rewritten bytes/metadata, and lost physical head
// before a live owner may append or export its retained original sequence.
func (self *ProviderAttemptRequestJournal) check() error {
	if self.failure != nil {
		return self.failure
	}
	if self.checkpoint == nil || self.file == nil {
		return protocol.ErrProviderAttemptsUnavailable
	}
	if err := self.directory.check(); err != nil {
		return err
	}
	raw, absent, err := readProviderAttemptRequestAttribute(self.directory.directory)
	if err != nil {
		return err
	}
	expected, encodeErr := json.Marshal(self.checkpoint)
	if encodeErr != nil || absent || !bytes.Equal(raw, expected) {
		return errors.Join(durablevolume.ErrIdentity, encodeErr, errors.New("provider request custody changed after admission"))
	}
	info, err := self.file.Stat()
	if err != nil {
		return err
	}
	named, err := self.directory.root.Lstat(ProviderAttemptRequestJournalName)
	if err != nil {
		return err
	}
	if !attemptLedgerPrivateFile(info) || !os.SameFile(info, named) || !os.SameFile(info, self.observed) || info.Size() != self.observed.Size() || !info.ModTime().Equal(self.observed.ModTime()) || !sameProviderAttemptRequestFileStat(info, self.observed) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request physical original changed after admission"))
	}
	return nil
}

// Commit only the exact interrupted byte prefix. Unexpected trailing bytes
// are never truncated or hidden by rewriting a new journal generation.
func (self *ProviderAttemptRequestJournal) commitPending(ctx context.Context, record ProviderAttemptRequestRecord, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	line := append(slices.Clone(raw), '\n')
	info, err := self.file.Stat()
	if err != nil {
		return err
	}
	base := self.checkpoint.Committed.Bytes
	if info.Size() < int64(base) || uint64(info.Size()) > base+uint64(len(line)) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request pending suffix exceeds original"))
	}
	suffix := make([]byte, uint64(info.Size())-base)
	if len(suffix) > 0 {
		if _, err := self.file.ReadAt(suffix, int64(base)); err != nil {
			return err
		}
		if !bytes.HasPrefix(line, suffix) {
			return errors.Join(durablevolume.ErrIdentity, errors.New("provider request interrupted suffix conflicts"))
		}
	}
	if n, err := self.file.WriteAt(line[len(suffix):], int64(base)+int64(len(suffix))); err != nil {
		return err
	} else if n != len(line)-len(suffix) {
		return io.ErrShortWrite
	}
	if err := self.file.Sync(); err != nil {
		return err
	}
	if self.step != nil {
		if err := self.step("after-request-file-sync"); err != nil {
			return err
		}
	}
	hash, err := record.Hash()
	if err != nil {
		return err
	}
	self.checkpoint.Committed = ProviderAttemptRequestHead{Sequence: record.Sequence, Hash: hash, Bytes: base + uint64(len(line)), LastBoundary: record.Boundary}
	self.checkpoint.Pending = nil
	if err := self.saveCheckpoint(false); err != nil {
		return err
	}
	self.observed, err = self.file.Stat()
	if err != nil {
		return err
	}
	return self.removePending(raw)
}

// Removal follows committed readback and compares the exact retained pending
// original. Interrupted cleanup is harmless and resumes before the next append.
func (self *ProviderAttemptRequestJournal) removePending(raw []byte) error {
	observed, err := self.directory.readSmall(ProviderAttemptRequestPendingName, self.limits.MaxRecordBytes)
	if err != nil {
		return err
	}
	if !bytes.Equal(observed, raw) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request pending cleanup conflicts"))
	}
	if err := self.directory.root.Remove(ProviderAttemptRequestPendingName); err != nil {
		return err
	}
	return self.directory.sync("provider-request-pending-remove")
}

// This returns only after original bytes and the signed sequence are durable.
// The caller keeps the returned record across all identical transport retries.
func (self *ProviderAttemptRequestJournal) Append(ctx context.Context, boundary AttemptBoundary, hop connect.Id, body, message, signature []byte) (_ *ProviderAttemptRequestRecord, resultErr error) {
	if ctx == nil {
		return nil, errors.New("provider request context absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := self.directory.enter(ctx); err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, self.directory.leave())
		if errors.Is(resultErr, durablevolume.ErrIdentity) || errors.Is(resultErr, protocol.ErrProviderAttemptsIntegrity) {
			self.failure = resultErr
		}
	}()
	if err := self.ready(ctx); err != nil {
		return nil, err
	}
	if !providerAttemptRequestAtOrAfter(boundary, self.preparation.Birth) {
		return nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider original request predates original birth"))
	}
	if self.checkpoint.Closed != nil && boundary.SettlementEpoch <= self.checkpoint.Closed.Window.Epoch {
		return nil, errors.Join(protocol.ErrProviderAttemptsUnavailable, errors.New("provider request window is already closed"))
	}
	prior := self.checkpoint.Committed
	record := ProviderAttemptRequestRecord{Schema: ProviderAttemptRequestSchema, Identity: self.identity, Sequence: prior.Sequence + 1, PreviousHash: prior.Hash, Boundary: boundary, Hop: hop, Body: slices.Clone(body), Message: slices.Clone(message), RequestSignature: slices.Clone(signature)}
	unsigned, err := record.signingBytes()
	if err != nil {
		return nil, err
	}
	record.Signature = ed25519.Sign(self.key, unsigned)
	if err := VerifyProviderAttemptRequest(ctx, record, self.identity, prior, self.limits); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	self.dirty = true
	if err := self.directory.publishMarker(ProviderAttemptRequestPendingName, raw); err != nil {
		return nil, err
	}
	self.checkpoint.Pending = &ProviderAttemptRequestPending{Bytes: uint64(len(raw)), Hash: sha256.Sum256(raw)}
	if err := self.saveCheckpoint(false); err != nil {
		return nil, err
	}
	if err := self.commitPending(ctx, record, raw); err != nil {
		return nil, err
	}
	self.dirty = false
	return &record, nil
}

// The complete bounded prefix is replayed once on open/export. Live appends
// retain original inode/metadata custody instead of rescanning an old prefix.
func (self *ProviderAttemptRequestJournal) walk(ctx context.Context, visit func(ProviderAttemptRequestRecord) error) error {
	if err := self.check(); err != nil {
		return err
	}
	if self.checkpoint.Closed != nil {
		if err := verifyProviderAttemptRequestClosedHead(ctx, *self.checkpoint.Closed, self.preparation); err != nil {
			return err
		}
	}
	if _, err := self.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	scanner := bufio.NewScanner(io.NewSectionReader(self.file, 0, int64(self.checkpoint.Committed.Bytes)))
	scanner.Buffer(make([]byte, 1024), int(self.limits.MaxRecordBytes))
	closedReplay := newProviderRequestClosedReplay(self.checkpoint.Closed)
	var head ProviderAttemptRequestHead
	if err := verifyProviderAttemptClosedPrefix(self.checkpoint.Closed, head); err != nil {
		return err
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw := scanner.Bytes()
		var record ProviderAttemptRequestRecord
		if err := attemptStoreDecode(raw, &record); err != nil {
			return err
		}
		if err := VerifyProviderAttemptRequest(ctx, record, self.identity, head, self.limits); err != nil {
			return err
		}
		canonical, err := json.Marshal(record)
		if err != nil || !bytes.Equal(raw, canonical) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, err, errors.New("provider original request encoding differs"))
		}
		hash, err := record.Hash()
		if err != nil {
			return err
		}
		head = ProviderAttemptRequestHead{Sequence: record.Sequence, Hash: hash, Bytes: head.Bytes + uint64(len(raw)) + 1, LastBoundary: record.Boundary}
		if err := verifyProviderAttemptClosedPrefix(self.checkpoint.Closed, head); err != nil {
			return err
		}
		if !providerAttemptRequestAtOrAfter(record.Boundary, self.checkpoint.Birth) {
			return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider original request predates original birth"))
		}
		if err := closedReplay.record(record, raw); err != nil {
			return err
		}
		if visit != nil {
			if err := visit(record); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if head != self.checkpoint.Committed {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider original request prefix differs from retained head"))
	}
	return errors.Join(closedReplay.finish(), self.check(), ctx.Err())
}

// Borrow each original for the callback; it receives owned decoded byte slices.
func (self *ProviderAttemptRequestJournal) Walk(ctx context.Context, visit func(ProviderAttemptRequestRecord) error) (resultErr error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := self.directory.enter(ctx); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, self.directory.leave()) }()
	if err := self.ready(ctx); err != nil {
		return err
	}
	return self.walk(ctx, visit)
}

// Export a copied immutable head after real local custody validation.
func (self *ProviderAttemptRequestJournal) Head(ctx context.Context) (head ProviderAttemptRequestHead, birth AttemptBoundary, resultErr error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if ctx == nil {
		return head, birth, errors.New("provider request context absent")
	}
	if err := ctx.Err(); err != nil {
		return ProviderAttemptRequestHead{}, AttemptBoundary{}, err
	}
	if err := self.directory.enter(ctx); err != nil {
		return head, birth, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, self.directory.leave())
		if resultErr != nil {
			head, birth = ProviderAttemptRequestHead{}, AttemptBoundary{}
		}
	}()
	if err := self.ready(ctx); err != nil {
		return ProviderAttemptRequestHead{}, AttemptBoundary{}, err
	}
	return self.checkpoint.Committed, self.checkpoint.Birth, nil
}

// The containing owner joins all users before closing these retained handles.
func (self *ProviderAttemptRequestJournal) Close() error {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	var err error
	if self.file != nil {
		err = self.file.Close()
		self.file = nil
	}
	clear(self.key)
	if self.directory != nil {
		err = errors.Join(err, self.directory.Close())
		self.directory = nil
	}
	return err
}

// A failed publication keeps one recoverable original, not a sticky capacity
// or cancellation verdict. Only this instance's interrupted append can reload
// its head; clean instances still reject another writer's changed custody.
func (self *ProviderAttemptRequestJournal) ready(ctx context.Context) error {
	if self.failure != nil {
		return self.failure
	}
	if !self.dirty {
		return self.check()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, absent, err := readProviderAttemptRequestAttribute(self.directory.directory)
	if err != nil {
		return err
	}
	if absent {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request interrupted head disappeared"))
	}
	var original ProviderAttemptRequestCheckpoint
	if err := attemptStoreDecode(raw, &original); err != nil {
		return err
	}
	if err := self.validateCheckpoint(original); err != nil {
		return err
	}
	// File publication may precede the head barrier, but no other sequence is
	// allowed to appear while this one retained operation owns recovery.
	if original.Committed.Sequence > self.checkpoint.Committed.Sequence || self.checkpoint.Committed.Sequence-original.Committed.Sequence > 1 {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider request interrupted sequence differs"))
	}
	self.checkpoint = &original
	if err := self.openFileAndRecover(ctx); err != nil {
		return err
	}
	self.dirty = false
	return self.check()
}

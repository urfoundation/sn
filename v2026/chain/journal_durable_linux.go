//go:build linux

// Native custody retains context, physical leaves and explicit storage policy.
// Actual write uncertainty pauses the owner; it never rewrites a partial log.
package chain

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// A leaf generation survives descriptor close between serial operations.
type nativeJournalLeaf struct {
	device uint64
	inode  uint64
}

// The externally driven owner is serial; Close follows joined submission.
type guardedNativeJournal struct {
	ctx          context.Context
	directory    *durablepath.Directory
	rawDirectory *durablepath.Directory
	failed       error
	leaves       map[string]nativeJournalLeaf
	absent       map[string]bool
	rawCount     int
	rawBytes     int64
	rawMembers   map[string]nativeRawMember
	rawStamps    map[string]unix.Stat_t
	pendingRaw   string
	custody      nativeJournalCustody
	custodyBytes []byte
	journalHash  hash.Hash
	journalStamp unix.Stat_t
	checkpoint   func(string, *os.File) error
	writeFile    func(*os.File, []byte) (int, error)
}

// Mainnet daemons accept only the strict non-system-volume policy.
func OpenDurableJournal(ctx context.Context, dir string) (*Journal, error) {
	return openGuardedNativeJournal(ctx, dir, durablepath.Open)
}

// Signing owners select this independent local schema explicitly.
func OpenOwnerLocalJournal(ctx context.Context, dir string) (*Journal, error) {
	return openGuardedNativeJournal(ctx, dir, durablepath.OpenOwnerLocal)
}

// The declared root, journal, raw directory and checkpoint are preprovisioned.
func openGuardedNativeJournal(ctx context.Context, dir string, open func(context.Context, string, durablevolume.Access, bool) (*durablepath.Directory, error)) (*Journal, error) {
	return openNativeJournal(ctx, dir, open, false)
}

// Only explicit recovery may finish a proven pending checkpoint after join.
func openNativeJournal(ctx context.Context, dir string, open func(context.Context, string, durablevolume.Access, bool) (*durablepath.Directory, error), reconcile bool) (*Journal, error) {
	directory, err := open(ctx, dir, durablevolume.ReadWrite, false)
	if err != nil {
		return nil, err
	}
	self := &guardedNativeJournal{ctx: ctx, directory: directory, leaves: map[string]nativeJournalLeaf{}, absent: map[string]bool{}}
	fail := func(err error) (*Journal, error) { return nil, errors.Join(err, self.close()) }
	info, err := directory.File().Stat()
	if err != nil {
		return fail(nativeObservation(err, false))
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return fail(errors.Join(durablevolume.ErrIdentity, errors.New("native journal requires a private directory")))
	}
	if err := unix.Flock(int(directory.File().Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return fail(errors.Join(&durablevolume.BusyError{Root: dir, Access: durablevolume.ReadWrite}, err))
		}
		return fail(nativeObservation(err, false))
	}
	self.rawDirectory, err = open(ctx, filepath.Join(dir, journalRawDir), durablevolume.ReadWrite, false)
	if err != nil {
		return fail(err)
	}
	if err := self.loadCustody(reconcile); err != nil {
		return fail(err)
	}
	return &Journal{dir: dir, guard: self}, nil
}

// Observation failure alone never proves a changed physical generation.
func nativeObservation(err error, retainedName bool) error {
	if err == nil || errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrClosed) || errors.Is(err, os.ErrInvalid) || errors.Is(err, unix.EBADF) {
		return err
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) || retainedName && errors.Is(err, os.ErrNotExist) {
		return errors.Join(durablevolume.ErrIdentity, err)
	}
	return errors.Join(&durablevolume.UnavailableError{Reason: "native journal filesystem observation failed"}, err)
}

// Proven identity loss stays sticky even if the old pathname is restored.
func (self *guardedNativeJournal) retain(err error) error {
	if errors.Is(err, durablevolume.ErrIdentity) {
		self.failed = errors.Join(self.failed, err)
	}
	if errors.Is(self.failed, durablevolume.ErrIdentity) {
		return errors.Join(err, self.failed)
	}
	return err
}

// Publication may already have happened; a later call cannot acknowledge it.
func (self *guardedNativeJournal) uncertain(err error) error {
	if err == nil {
		return nil
	}
	self.failed = errors.Join(self.failed, ErrJournalUncertain, err)
	return self.failed
}

// The retained context is checked on every operation, never replaced by ambient scope.
func (self *guardedNativeJournal) admit(directory *durablepath.Directory, write bool) error {
	if self == nil || self.ctx == nil || directory == nil || directory.File() == nil {
		return durablevolume.ErrClosed
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if self.failed != nil && (write || errors.Is(self.failed, durablevolume.ErrIdentity)) {
		return self.failed
	}
	var err error
	if write {
		err = directory.CheckWrite()
	} else {
		err = directory.CheckRead()
	}
	return errors.Join(self.retain(err), self.ctx.Err())
}

// Instance-only seams force ordering/failures while successful I/O remains real.
func (self *guardedNativeJournal) at(stage string, file *os.File) error {
	if self.checkpoint != nil {
		return self.checkpoint(stage, file)
	}
	return nil
}

// The default always writes the retained descriptor; tests can force partial I/O.
func (self *guardedNativeJournal) write(file *os.File, raw []byte) (int, error) {
	if self.writeFile != nil {
		return self.writeFile(file, raw)
	}
	return file.Write(raw)
}

// Every leaf is compared through its retained parent before and after actual I/O.
func (self *guardedNativeJournal) named(directory *durablepath.Directory, name string, file *os.File) error {
	if err := self.admit(directory, false); err != nil {
		return err
	}
	var opened, named unix.Stat_t
	err := self.at("leaf-stat", file)
	if err == nil {
		err = unix.Fstat(int(file.Fd()), &opened)
	}
	if err != nil {
		return self.retain(nativeObservation(err, false))
	}
	if opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Mode&0777 != 0600 || opened.Nlink != 1 || opened.Uid != uint32(os.Geteuid()) {
		return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal leaf is not private owned custody")))
	}
	err = self.at("leaf-name", file)
	if err == nil {
		err = unix.Fstatat(int(directory.File().Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW)
	}
	if err != nil {
		return self.retain(nativeObservation(err, true))
	}
	if opened.Dev != named.Dev || opened.Ino != named.Ino || opened.Mode != named.Mode || opened.Uid != named.Uid || named.Nlink != 1 {
		return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal named leaf generation changed")))
	}
	key := filepath.Join(directory.File().Name(), name)
	identity := nativeJournalLeaf{device: uint64(opened.Dev), inode: opened.Ino}
	if prior, ok := self.leaves[key]; ok && prior != identity {
		return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal retained leaf was replaced between operations")))
	}
	self.leaves[key] = identity
	delete(self.absent, key)
	return self.admit(directory, false)
}

// O_CREAT never recreates a previously retained leaf, even before its first write.
func (self *guardedNativeJournal) open(directory *durablepath.Directory, name string, flags int) (*os.File, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return nil, errors.New("native journal leaf name is invalid")
	}
	write := flags&(unix.O_WRONLY|unix.O_RDWR|unix.O_CREAT) != 0
	if err := self.admit(directory, write); err != nil {
		return nil, err
	}
	key := filepath.Join(directory.File().Name(), name)
	if prior, ok := self.leaves[key]; ok {
		var named unix.Stat_t
		if err := unix.Fstatat(int(directory.File().Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return nil, self.retain(nativeObservation(err, true))
		}
		if uint64(named.Dev) != prior.device || named.Ino != prior.inode {
			return nil, self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal retained leaf disappeared or changed")))
		}
	}
	if self.absent[key] && flags&unix.O_CREAT != 0 {
		flags |= unix.O_EXCL
	}
	fd, err := unix.Openat(int(directory.File().Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			self.absent[key] = true
			return nil, err
		}
		if self.absent[key] && errors.Is(err, unix.EEXIST) {
			return nil, self.retain(errors.Join(durablevolume.ErrIdentity, err))
		}
		return nil, self.retain(nativeObservation(err, false))
	}
	file := os.NewFile(uintptr(fd), name)
	if err := self.named(directory, name, file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if err := self.admit(directory, write); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// Finite descriptor reads check context, leaf identity and size on both sides.
func (self *guardedNativeJournal) read(directory *durablepath.Directory, name string, file *os.File, maximum int, stage string) ([]byte, error) {
	if err := self.at(stage+"-opened", file); err != nil {
		return nil, self.retain(nativeObservation(err, false))
	}
	if err := self.named(directory, name, file); err != nil {
		return nil, err
	}
	var before unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &before); err != nil {
		return nil, nativeObservation(err, false)
	}
	if before.Size < 0 || before.Size > int64(maximum) {
		return nil, self.uncertain(errors.New("native journal file exceeds its read bound"))
	}
	raw := make([]byte, int(before.Size))
	for offset := 0; offset < len(raw); {
		if err := self.named(directory, name, file); err != nil {
			return nil, err
		}
		end := min(offset+64*1024, len(raw))
		n, err := file.ReadAt(raw[offset:end], int64(offset))
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, nativeObservation(err, false)
		}
		if n != end-offset {
			return nil, self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal changed during bounded read"), err))
		}
		offset += n
		if err := self.named(directory, name, file); err != nil {
			return nil, err
		}
	}
	if err := self.at(stage+"-read", file); err != nil {
		return nil, self.retain(nativeObservation(err, false))
	}
	if err := self.named(directory, name, file); err != nil {
		return nil, err
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &after); err != nil {
		return nil, nativeObservation(err, false)
	}
	if before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return nil, self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal metadata changed during read")))
	}
	return raw, nil
}

// Full/read-only volumes permit inspection, but an incomplete tail blocks mutation.
func (self *guardedNativeJournal) entries() ([]JournalEntry, error) {
	if err := self.checkCustody(); err != nil {
		return nil, err
	}
	file, err := self.open(self.directory, journalFileName, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	raw, readErr := self.read(self.directory, journalFileName, file, 16*1024*1024, "entries")
	if err := errors.Join(readErr, nativeObservation(file.Close(), false)); err != nil {
		return nil, err
	}
	if int64(len(raw)) != self.custody.Committed.JournalSize || nativeDigest(raw) != self.custody.Committed.JournalSha256 {
		if self.custody.Pending != nil {
			return nil, errors.Join(ErrJournalUncertain, errors.New("pending native journal differs from its acknowledged checkpoint"))
		}
		return nil, self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal differs from its acknowledged checkpoint")))
	}
	entries, err := parseNativeJournalEntries(self.ctx, raw)
	if err != nil {
		return nil, self.uncertain(err)
	}
	if err := errors.Join(self.checkCustody(), self.admit(self.directory, false)); err != nil {
		return nil, err
	}
	return entries, nil
}

// Parsing never repairs, truncates or accepts a partial journal line.
func parseNativeJournalEntries(ctx context.Context, raw []byte) ([]JournalEntry, error) {
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		return nil, errors.New("native journal has an unterminated retained line")
	}
	var entries []JournalEntry
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var entry JournalEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("native journal line %d: %w", line, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, ctx.Err()
}

// Actual append/sync uncertainty stops this owner from extending a partial tail.
func (self *guardedNativeJournal) append(raw []byte) error {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return errors.New("native journal entry exceeds byte bound")
	}
	if err := self.admit(self.directory, true); err != nil {
		return err
	}
	if err := self.checkCustody(); err != nil {
		return err
	}
	file, err := self.open(self.directory, journalFileName, unix.O_WRONLY|unix.O_APPEND)
	if err != nil {
		return err
	}
	if err := self.at("append-opened", file); err != nil {
		return errors.Join(self.retain(nativeObservation(err, false)), file.Close())
	}
	if err := self.named(self.directory, journalFileName, file); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := self.admit(self.directory, true); err != nil {
		return errors.Join(err, file.Close())
	}
	var current unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &current); err != nil {
		return errors.Join(nativeObservation(err, false), file.Close())
	}
	if current.Size < 0 || current.Size > 16*1024*1024-int64(len(raw)) {
		return errors.Join(&durablevolume.UnavailableError{Reason: "native journal retained-byte bound is exhausted"}, file.Close())
	}
	if current.Size != self.journalStamp.Size || current.Mtim != self.journalStamp.Mtim || current.Ctim != self.journalStamp.Ctim {
		return errors.Join(self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal metadata changed between acknowledged appends"))), file.Close())
	}
	nextHash, err := nativeExtendedHash(self.journalHash, raw)
	if err != nil {
		return errors.Join(err, file.Close())
	}
	next := self.custody.Committed
	next.JournalSize += int64(len(raw))
	next.JournalSha256 = hex.EncodeToString(nextHash.Sum(nil))
	if err := self.beginCustody(next); err != nil {
		return errors.Join(err, file.Close())
	}
	written, writeErr := self.write(file, raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	postErr := errors.Join(self.at("append-written", file), self.named(self.directory, journalFileName, file))
	if err := errors.Join(writeErr, postErr); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	if err := errors.Join(file.Sync(), self.at("append-synced", file), self.named(self.directory, journalFileName, file)); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	if err := errors.Join(self.directory.File().Sync(), self.at("append-directory-synced", file), self.named(self.directory, journalFileName, file)); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	if err := nativeObservation(unix.Fstat(int(file.Fd()), &self.journalStamp), false); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	if self.journalStamp.Size != next.JournalSize {
		return self.uncertain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal length changed during append"), file.Close()))
	}
	if err := errors.Join(file.Close(), self.admit(self.directory, true)); err != nil {
		return self.uncertain(err)
	}
	if err := self.commitCustody(); err != nil {
		return self.uncertain(err)
	}
	self.journalHash = nextHash
	return nil
}

// Exact existing raw bytes can be inspected under pressure without a new write.
func (self *guardedNativeJournal) saveRaw(hash types.Hash, raw []byte) error {
	if len(raw) == 0 || len(raw) > 1024*1024 || ExtrinsicHash(raw) != hash {
		return errors.New("native journal raw bytes or hash differ")
	}
	if self.failed != nil {
		return self.failed
	}
	if err := self.checkCustody(); err != nil {
		return err
	}
	name := strings.TrimPrefix(hash.Hex(), "0x") + ".scale"
	prior, err := self.open(self.rawDirectory, name, unix.O_RDONLY)
	if err == nil {
		retained, readErr := self.read(self.rawDirectory, name, prior, 1024*1024, "raw")
		if err := errors.Join(readErr, nativeObservation(prior.Close(), false)); err != nil {
			return err
		}
		if !bytes.Equal(retained, raw) {
			return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal refuses raw custody replacement")))
		}
		return self.admit(self.rawDirectory, false)
	}
	if !errors.Is(err, os.ErrNotExist) || errors.Is(err, durablevolume.ErrIdentity) {
		return err
	}
	if err := self.admit(self.rawDirectory, true); err != nil {
		return err
	}
	if self.rawCount >= 10000 || self.rawBytes > 64*1024*1024-int64(len(raw)) {
		return &durablevolume.UnavailableError{Reason: "native raw custody entry bound is exhausted"}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := name + ".pending-" + hex.EncodeToString(nonce[:])
	file, err := self.open(self.rawDirectory, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return err
	}
	if err := self.at("raw-created", file); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	var created unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &created); err != nil {
		return self.uncertain(errors.Join(nativeObservation(err, false), file.Close()))
	}
	member := nativeRawMember{Name: name, Inode: created.Ino, Size: int64(len(raw)), Sha256: nativeDigest(raw)}
	self.rawMembers[name] = member
	next := self.custody.Committed
	next.RawCount++
	next.RawSha256 = nativeRawDigest(self.rawMembers)
	if err := self.beginCustody(next); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	written, writeErr := self.write(file, raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, self.at("raw-written", file), self.named(self.rawDirectory, temporary, file), file.Sync()); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	if err := self.at("raw-synced", file); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	if err := self.admit(self.rawDirectory, true); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	fd := int(self.rawDirectory.File().Fd())
	if err := unix.Renameat2(fd, temporary, fd, name, unix.RENAME_NOREPLACE); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	self.rawCount++
	delete(self.leaves, filepath.Join(self.rawDirectory.File().Name(), temporary))
	delete(self.absent, filepath.Join(self.rawDirectory.File().Name(), name))
	if err := errors.Join(self.at("raw-renamed", file), self.named(self.rawDirectory, name, file)); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	if err := errors.Join(self.rawDirectory.File().Sync(), self.named(self.rawDirectory, name, file)); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	var completed unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &completed); err != nil {
		return self.uncertain(errors.Join(nativeObservation(err, false), file.Close()))
	}
	if completed.Size != int64(len(raw)) {
		return self.uncertain(errors.Join(durablevolume.ErrIdentity, errors.New("native raw length changed during publication"), file.Close()))
	}
	self.rawStamps[name] = completed
	if err := errors.Join(file.Close(), self.admit(self.rawDirectory, true)); err != nil {
		return self.uncertain(err)
	}
	if err := self.commitCustody(); err != nil {
		return self.uncertain(err)
	}
	self.rawBytes += int64(len(raw))
	return nil
}

// External send/sign boundaries re-admit the actual retained journal owner.
func (self *guardedNativeJournal) checkWrite() error {
	if err := self.admit(self.directory, true); err != nil {
		return err
	}
	if err := self.admit(self.rawDirectory, true); err != nil {
		return err
	}
	if err := self.checkCustody(); err != nil {
		return err
	}
	for key := range self.leaves {
		if err := self.ctx.Err(); err != nil {
			return err
		}
		directory := self.directory
		if filepath.Dir(key) == self.rawDirectory.File().Name() {
			directory = self.rawDirectory
		}
		file, err := self.open(directory, filepath.Base(key), unix.O_RDONLY)
		if err != nil {
			return err
		}
		if err := errors.Join(self.named(directory, filepath.Base(key), file), nativeObservation(file.Close(), false)); err != nil {
			return err
		}
		var current unix.Stat_t
		if err := unix.Fstatat(int(directory.File().Fd()), filepath.Base(key), &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return self.retain(nativeObservation(err, true))
		}
		expected := self.journalStamp
		if directory == self.rawDirectory {
			expected = self.rawStamps[filepath.Base(key)]
		}
		if current.Ino != expected.Ino || current.Dev != expected.Dev || current.Size != expected.Size || current.Mtim != expected.Mtim || current.Ctim != expected.Ctim {
			return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native custody metadata changed before handoff")))
		}
	}
	return errors.Join(self.admit(self.directory, true), self.admit(self.rawDirectory, true))
}

// Closing releases only this joined owner's descriptors and never alters bytes.
func (self *guardedNativeJournal) close() error {
	if self == nil {
		return nil
	}
	raw, directory := self.rawDirectory, self.directory
	self.rawDirectory, self.directory = nil, nil
	var err error
	if raw != nil {
		err = raw.Close()
	}
	if directory != nil {
		err = errors.Join(err, directory.Close())
	}
	return err
}

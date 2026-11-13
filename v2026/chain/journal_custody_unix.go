//go:build linux || darwin

// A mandatory preprovisioned checkpoint distinguishes pristine and retained
// custody. It detects local loss, not replay of every valid predecessor byte.
package chain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"

	"github.com/urnetwork/connect/v2026/durablesys"
)

// Enrollment is an external, joined provisioning operation, never an opener.
const NativeJournalCustodyAttribute = "user.urnetwork.native-journal-custody"

const nativeJournalCustodySchema = "urnetwork-native-journal-custody-v1"

type nativeJournalCheckpoint struct {
	JournalInode  uint64 `json:"journal_inode"`
	JournalSize   int64  `json:"journal_size"`
	JournalSha256 string `json:"journal_sha256"`
	RawCount      int    `json:"raw_count"`
	RawSha256     string `json:"raw_sha256"`
}

// Pending retains both the acknowledged prefix and the exact intended result.
type nativeJournalCustody struct {
	Schema            string                   `json:"schema"`
	DirectoryInode    uint64                   `json:"directory_inode"`
	RawDirectoryInode uint64                   `json:"raw_directory_inode"`
	Committed         nativeJournalCheckpoint  `json:"committed"`
	Pending           *nativeJournalCheckpoint `json:"pending,omitempty"`
}

type nativeRawMember struct {
	Name   string `json:"name"`
	Inode  uint64 `json:"inode"`
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256"`
}

// Only the affected, joined owner is reopened; no signing or broadcast occurs.
func ReconcileDurableJournal(ctx context.Context, dir string) (*Journal, error) {
	return openNativeJournal(ctx, dir, durablepath.Open, true)
}

// Local signers choose the independent owner-device declaration explicitly.
func ReconcileOwnerLocalJournal(ctx context.Context, dir string) (*Journal, error) {
	return openNativeJournal(ctx, dir, durablepath.OpenOwnerLocal, true)
}

// Digest records are small and deterministic; no raw file is reread per append.
func nativeRawDigest(members map[string]nativeRawMember) string {
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	hasher := sha256.New()
	encoder := json.NewEncoder(hasher)
	for _, name := range names {
		_ = encoder.Encode(members[name])
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func nativeDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// Clone the incremental digest without replaying the acknowledged journal.
func nativeExtendedHash(previous hash.Hash, raw []byte) (hash.Hash, error) {
	state, err := previous.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return nil, err
	}
	next := sha256.New()
	if err := next.(encoding.BinaryUnmarshaler).UnmarshalBinary(state); err != nil {
		return nil, err
	}
	_, _ = next.Write(raw)
	return next, nil
}

// Missing/changed authority is distinct from an unavailable xattr observation.
func (self *guardedNativeJournal) readCustody() ([]byte, error) {
	if err := self.admit(self.directory, false); err != nil {
		return nil, err
	}
	if err := self.at("custody-observe", self.directory.File()); err != nil {
		return nil, self.retain(nativeObservation(err, false))
	}
	raw := make([]byte, 4096)
	n, err := durablesys.GetAttribute(int(self.directory.File().Fd()), NativeJournalCustodyAttribute, raw)
	if err != nil {
		if errors.Is(err, durablesys.ErrNoAttribute) || errors.Is(err, unix.ERANGE) {
			return nil, self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal custody anchor is absent or invalid"), err))
		}
		return nil, nativeObservation(err, false)
	}
	if err := self.admit(self.directory, false); err != nil {
		return nil, err
	}
	return raw[:n], nil
}

// A runtime never installs a missing checkpoint or treats one as empty state.
func (self *guardedNativeJournal) checkCustody() error {
	if err := self.admit(self.rawDirectory, false); err != nil {
		return err
	}
	raw, err := self.readCustody()
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, self.custodyBytes) {
		return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal custody checkpoint changed outside this owner")))
	}
	return nil
}

// The checkpoint is changed atomically and synchronized before acknowledgement.
// Any error after the xattr mutation starts leaves a stopped, closable owner.
func (self *guardedNativeJournal) writeCustody(next nativeJournalCustody, stage string) error {
	if err := self.admit(self.directory, true); err != nil {
		return err
	}
	if err := self.admit(self.rawDirectory, true); err != nil {
		return err
	}
	if err := self.checkCustody(); err != nil {
		return err
	}
	raw, err := json.Marshal(next)
	if err != nil || len(raw) > 4096 {
		return errors.Join(err, errors.New("invalid native custody checkpoint size"))
	}
	if err := durablesys.SetAttribute(int(self.directory.File().Fd()), NativeJournalCustodyAttribute, raw, durablesys.AttributeReplace); err != nil {
		return self.uncertain(nativeObservation(err, false))
	}
	self.custody, self.custodyBytes = next, raw
	if err := errors.Join(self.at("custody-"+stage+"-written", self.directory.File()), self.directory.File().Sync()); err != nil {
		return self.uncertain(err)
	}
	if err := self.at("custody-"+stage+"-synced", self.directory.File()); err != nil {
		return self.uncertain(err)
	}
	if err := errors.Join(self.checkCustody(), self.admit(self.directory, true), self.admit(self.rawDirectory, true)); err != nil {
		return self.uncertain(err)
	}
	return nil
}

func (self *guardedNativeJournal) beginCustody(next nativeJournalCheckpoint) error {
	pending := self.custody
	pending.Pending = &next
	return self.writeCustody(pending, "pending")
}

func (self *guardedNativeJournal) commitCustody() error {
	if self.custody.Pending == nil {
		return errors.New("native journal has no pending checkpoint")
	}
	next := self.custody
	next.Committed, next.Pending = *next.Pending, nil
	return self.writeCustody(next, "committed")
}

// One bounded restart scan authenticates all retained members and seeds hashes.
func (self *guardedNativeJournal) loadCustody(reconcile bool) error {
	raw, err := self.readCustody()
	if err != nil {
		return err
	}
	var anchor nativeJournalCustody
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&anchor); err != nil {
		return self.retain(errors.Join(durablevolume.ErrIdentity, err))
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native custody anchor has trailing data"), err))
	}
	valid := func(checkpoint nativeJournalCheckpoint) bool {
		if checkpoint.JournalInode == 0 || checkpoint.JournalSize < 0 || checkpoint.JournalSize > 16*1024*1024 || checkpoint.RawCount < 0 || checkpoint.RawCount > 10000 {
			return false
		}
		for _, digest := range []string{checkpoint.JournalSha256, checkpoint.RawSha256} {
			decoded, err := hex.DecodeString(digest)
			if err != nil || len(decoded) != 32 || strings.ToLower(digest) != digest {
				return false
			}
		}
		return true
	}
	if anchor.Schema != nativeJournalCustodySchema || !valid(anchor.Committed) || anchor.Pending != nil && !valid(*anchor.Pending) {
		return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native custody anchor schema or bounds are invalid")))
	}
	var directory, rawDirectory unix.Stat_t
	if err := unix.Fstat(int(self.directory.File().Fd()), &directory); err != nil {
		return nativeObservation(err, false)
	}
	if err := unix.Fstat(int(self.rawDirectory.File().Fd()), &rawDirectory); err != nil {
		return nativeObservation(err, false)
	}
	if anchor.DirectoryInode != directory.Ino || anchor.RawDirectoryInode != rawDirectory.Ino {
		return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal anchored directory generation differs")))
	}
	self.custody, self.custodyBytes = anchor, raw
	file, err := self.open(self.directory, journalFileName, unix.O_RDONLY)
	if err != nil {
		return self.retain(nativeObservation(err, true))
	}
	journalBytes, readErr := self.read(self.directory, journalFileName, file, 16*1024*1024, "entries")
	if err := errors.Join(readErr, nativeObservation(unix.Fstat(int(file.Fd()), &self.journalStamp), false), nativeObservation(file.Close(), false)); err != nil {
		return err
	}
	if _, err := parseNativeJournalEntries(self.ctx, journalBytes); err != nil {
		return errors.Join(ErrJournalUncertain, err)
	}
	self.journalHash = sha256.New()
	_, _ = self.journalHash.Write(journalBytes)
	if err := self.scanRawCustody(reconcile); err != nil {
		return err
	}
	actual := nativeJournalCheckpoint{JournalInode: self.journalStamp.Ino, JournalSize: int64(len(journalBytes)), JournalSha256: nativeDigest(journalBytes), RawCount: len(self.rawMembers), RawSha256: nativeRawDigest(self.rawMembers)}
	if anchor.Pending == nil {
		if actual != anchor.Committed {
			return self.retain(errors.Join(durablevolume.ErrIdentity, errors.New("native journal completed custody census differs")))
		}
		return self.checkCustody()
	}
	if !reconcile {
		return errors.Join(ErrJournalUncertain, errors.New("native journal retains a pending checkpoint"))
	}
	if actual != anchor.Committed && actual != *anchor.Pending {
		return errors.Join(ErrJournalUncertain, errors.New("native journal pending bytes match neither exact checkpoint"))
	}
	if self.pendingRaw != "" {
		if actual != *anchor.Pending {
			return errors.Join(ErrJournalUncertain, errors.New("unfinished raw publication does not match the intended checkpoint"))
		}
		if err := self.finishPendingRaw(); err != nil {
			return err
		}
	}
	// Re-sync the proven bytes before clearing an interrupted acknowledgement.
	for key := range self.leaves {
		if err := self.ctx.Err(); err != nil {
			return err
		}
		dir := self.directory
		name := journalFileName
		if key != self.directory.File().Name()+"/"+journalFileName {
			dir = self.rawDirectory
			name = strings.TrimPrefix(key, dir.File().Name()+"/")
		}
		retained, err := self.open(dir, name, unix.O_RDONLY)
		if err != nil {
			return err
		}
		if err := errors.Join(retained.Sync(), self.named(dir, name, retained), retained.Close()); err != nil {
			return self.uncertain(err)
		}
	}
	if err := errors.Join(self.rawDirectory.File().Sync(), self.directory.File().Sync()); err != nil {
		return self.uncertain(err)
	}
	anchor.Committed, anchor.Pending = actual, nil
	return self.writeCustody(anchor, "reconciled")
}

// Limits bound both work and bytes, including malformed or interrupted entries.
func (self *guardedNativeJournal) scanRawCustody(reconcile bool) error {
	if err := self.admit(self.rawDirectory, false); err != nil {
		return err
	}
	fd, err := unix.Openat(int(self.rawDirectory.File().Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nativeObservation(err, false)
	}
	file := os.NewFile(uintptr(fd), journalRawDir)
	entries, readErr := file.ReadDir(10001)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(nativeObservation(readErr, false), nativeObservation(file.Close(), false)); err != nil {
		return err
	}
	if len(entries) > 10000 {
		return errors.Join(ErrJournalUncertain, errors.New("native raw custody exceeds its entry bound"))
	}
	self.rawMembers = map[string]nativeRawMember{}
	self.rawStamps = map[string]unix.Stat_t{}
	self.rawBytes = 0
	for _, entry := range entries {
		if err := self.ctx.Err(); err != nil {
			return err
		}
		actualName := entry.Name()
		name := actualName
		if strings.Contains(name, ".pending-") {
			parts := strings.Split(name, ".pending-")
			if !reconcile || self.custody.Pending == nil || self.pendingRaw != "" || len(parts) != 2 || len(parts[1]) != 32 {
				return errors.Join(ErrJournalUncertain, errors.New("native raw custody has ambiguous or unanchored pending members"))
			}
			if decoded, err := hex.DecodeString(parts[1]); err != nil || len(decoded) != 16 || strings.ToLower(parts[1]) != parts[1] {
				return errors.Join(ErrJournalUncertain, errors.New("native raw custody pending identity is malformed"))
			}
			name = parts[0]
			self.pendingRaw = actualName
		}
		if len(name) != 70 || !strings.HasSuffix(name, ".scale") || strings.ToLower(name) != name {
			return errors.Join(ErrJournalUncertain, errors.New("native raw custody retains an unknown or unfinished publication"))
		}
		if decoded, err := hex.DecodeString(name[:64]); err != nil || len(decoded) != 32 {
			return errors.Join(ErrJournalUncertain, errors.New("native raw member name is invalid"))
		}
		if _, present := self.rawMembers[name]; present {
			return errors.Join(ErrJournalUncertain, errors.New("native raw pending target collides with retained custody"))
		}
		retained, err := self.open(self.rawDirectory, actualName, unix.O_RDONLY)
		if err != nil {
			return self.retain(nativeObservation(err, true))
		}
		var stat unix.Stat_t
		if err := unix.Fstat(int(retained.Fd()), &stat); err != nil {
			return errors.Join(nativeObservation(err, false), retained.Close())
		}
		if stat.Size < 1 || stat.Size > 1024*1024 || self.rawBytes > 64*1024*1024-stat.Size {
			return errors.Join(ErrJournalUncertain, errors.New("native raw custody exceeds its retained byte bound"), retained.Close())
		}
		raw, readErr := self.read(self.rawDirectory, actualName, retained, 1024*1024, "raw")
		if err := errors.Join(readErr, retained.Close()); err != nil {
			return err
		}
		if strings.TrimPrefix(ExtrinsicHash(raw).Hex(), "0x") != name[:64] {
			return self.retain(errors.Join(durablevolume.ErrIdentity, fmt.Errorf("native raw member hash differs: %s", name)))
		}
		self.rawBytes += int64(len(raw))
		self.rawMembers[name] = nativeRawMember{Name: name, Inode: stat.Ino, Size: stat.Size, Sha256: nativeDigest(raw)}
		self.rawStamps[name] = stat
	}
	self.rawCount = len(self.rawMembers)
	if self.pendingRaw != "" {
		finalName := strings.Split(self.pendingRaw, ".pending-")[0]
		member := self.rawMembers[finalName]
		delete(self.rawMembers, finalName)
		originalDigest := nativeRawDigest(self.rawMembers)
		self.rawMembers[finalName] = member
		committed, pending := self.custody.Committed, *self.custody.Pending
		if pending.JournalInode != committed.JournalInode || pending.JournalSize != committed.JournalSize || pending.JournalSha256 != committed.JournalSha256 || pending.RawCount != committed.RawCount+1 || len(self.rawMembers) != pending.RawCount || originalDigest != committed.RawSha256 || nativeRawDigest(self.rawMembers) != pending.RawSha256 {
			return errors.Join(ErrJournalUncertain, errors.New("pending raw publication does not preserve the exact original and intended censuses"))
		}
	}
	return self.admit(self.rawDirectory, false)
}

// Complete only the anchored, exact retained temporary under a no-replace rename.
func (self *guardedNativeJournal) finishPendingRaw() error {
	name := self.pendingRaw
	finalName := strings.Split(name, ".pending-")[0]
	if err := self.admit(self.rawDirectory, true); err != nil {
		return err
	}
	if err := self.checkCustody(); err != nil {
		return err
	}
	file, err := self.open(self.rawDirectory, name, unix.O_RDONLY)
	if err != nil {
		return err
	}
	if err := errors.Join(file.Sync(), self.named(self.rawDirectory, name, file)); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	var target unix.Stat_t
	err = unix.Fstatat(int(self.rawDirectory.File().Fd()), finalName, &target, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		return errors.Join(ErrJournalUncertain, errors.New("pending raw target already exists"), file.Close())
	}
	if !errors.Is(err, unix.ENOENT) {
		return errors.Join(nativeObservation(err, false), file.Close())
	}
	if err := self.admit(self.rawDirectory, true); err != nil {
		return errors.Join(err, file.Close())
	}
	fd := int(self.rawDirectory.File().Fd())
	if err := durablesys.RenameNoReplace(fd, name, fd, finalName); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	delete(self.leaves, self.rawDirectory.File().Name()+"/"+name)
	if err := errors.Join(self.named(self.rawDirectory, finalName, file), self.rawDirectory.File().Sync()); err != nil {
		return self.uncertain(errors.Join(err, file.Close()))
	}
	var completed unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &completed); err != nil {
		return self.uncertain(errors.Join(nativeObservation(err, false), file.Close()))
	}
	self.rawStamps[finalName] = completed
	if err := errors.Join(file.Close(), self.checkCustody(), self.admit(self.rawDirectory, true)); err != nil {
		return self.uncertain(err)
	}
	self.pendingRaw = ""
	return nil
}

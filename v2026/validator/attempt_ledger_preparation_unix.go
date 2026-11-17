//go:build linux || darwin

// Every preparation read stays relative to the borrowed original directory.
// The read-only backend never repairs names or creates its lock file.
package validator

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Original metadata permits cheap unchanged-member checks after initial hashing.
type attemptPreparationObservation struct {
	file AttemptLedgerPreparationFile
	stat unix.Stat_t
}

// The caller joins all backend users before close; it retains the borrowed
// root and its external plan/fence. Only the backend descriptor is owned here.
type attemptPreparationView struct {
	ctx                 context.Context
	root                *os.File
	rootStat            unix.Stat_t
	backend             *os.File
	backendStat         unix.Stat_t
	limits              AttemptLedgerDiskLimits
	members             map[string]attemptPreparationObservation
	anchor              []byte
	anchorAbsent        bool
	requestAnchor       []byte
	requestAnchorAbsent bool
}

// Preparation operates only on private files owned by the exact calling identity.
func attemptPreparationPrivate(file *os.File, directory bool) error {
	if file == nil {
		return errors.New("preparation descriptor is absent")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return attemptLedgerCustodyObservation("cannot inspect preparation descriptor", err)
	}
	want := uint32(unix.S_IFREG)
	if directory {
		want = unix.S_IFDIR
	}
	if uint32(stat.Mode)&unix.S_IFMT != want || uint32(stat.Mode)&0077 != 0 || stat.Uid != uint32(os.Geteuid()) || !directory && stat.Nlink != 1 {
		return attemptLedgerCustodyLoss("preparation member is not private original custody", nil)
	}
	return nil
}

// Child descriptors are opened without following links and bound to their names.
func attemptPreparationOpen(parent *os.File, name string, directory bool) (*os.File, error) {
	if parent == nil || name == "" || name == ".." || strings.ContainsAny(name, "/\x00") {
		return nil, errors.New("preparation relative member is invalid")
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(int(parent.Fd()), name, flags, 0)
	if err != nil {
		return nil, attemptLedgerCustodyObservation("cannot open preparation member", err)
	}
	file := os.NewFile(uintptr(fd), name)
	if err := attemptPreparationPrivate(file, directory); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	var opened, named, root unix.Stat_t
	if err := errors.Join(unix.Fstat(fd, &opened), unix.Fstat(int(parent.Fd()), &root), unix.Fstatat(int(parent.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW)); err != nil {
		return nil, errors.Join(attemptLedgerCustodyObservation("cannot bind preparation member", err), file.Close())
	}
	if !attemptPreparationSameStat(opened, named) || opened.Dev != root.Dev {
		return nil, errors.Join(attemptLedgerCustodyLoss("preparation member changed filesystem or named identity", nil), file.Close())
	}
	return file, nil
}

// Content-affecting metadata joins physical identity, mode and ownership checks.
func attemptPreparationSameStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

// Staging creation is exclusive and explicit; failures retain all partial
// bytes. The caller never turns an unknown staging child into a fresh bundle.
func attemptPreparationCreate(ctx context.Context, parent *os.File, name string, raw []byte) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	for offset := 0; offset < len(raw); {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(len(raw), offset+64*1024)
		n, err := file.Write(raw[offset:end])
		if err != nil || n != end-offset {
			return errors.Join(io.ErrShortWrite, err)
		}
		offset = end
	}
	return errors.Join(file.Sync(), parent.Sync(), ctx.Err())
}

// Borrows the already exclusive root; only its backend descriptor is owned here.
func openAttemptPreparationView(ctx context.Context, root *os.File, limits AttemptLedgerDiskLimits) (_ *attemptPreparationView, resultErr error) {
	if ctx == nil {
		return nil, errors.New("preparation context is absent")
	}
	if err := errors.Join(ctx.Err(), attemptPreparationPrivate(root, true)); err != nil {
		return nil, err
	}
	// Assert the caller's exclusive borrowed open description without releasing
	// it here. An active backend additionally retains its own directory lock.
	if err := unix.Flock(int(root.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(durablevolume.ErrBusy, err)
	}
	self := &attemptPreparationView{ctx: ctx, root: root, limits: limits, members: map[string]attemptPreparationObservation{}}
	if err := unix.Fstat(int(root.Fd()), &self.rootStat); err != nil {
		return nil, attemptLedgerCustodyObservation("cannot inspect preparation root", err)
	}
	self.backend, resultErr = attemptPreparationOpen(root, attemptLedgerStoreName, true)
	if resultErr != nil {
		return nil, resultErr
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	if err := unix.Flock(int(self.backend.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(durablevolume.ErrBusy, err)
	}
	if err := unix.Fstat(int(self.backend.Fd()), &self.backendStat); err != nil {
		return nil, attemptLedgerCustodyObservation("cannot inspect preparation backend", err)
	}
	self.anchor, resultErr = readAttemptLedgerCustodyAttribute(root)
	self.anchorAbsent = errors.Is(resultErr, durablesys.ErrNoAttribute)
	if self.anchorAbsent {
		resultErr = nil
	}
	if resultErr == nil {
		self.requestAnchor, self.requestAnchorAbsent, resultErr = readProviderAttemptRequestAttribute(root)
	}
	return self, resultErr
}

// Callers join all read-only backend users before releasing this one descriptor.
func (self *attemptPreparationView) close() error {
	if self == nil || self.backend == nil {
		return nil
	}
	return self.backend.Close()
}

// Names are sorted and strictly finite. No unknown backend name is skipped.
// Namespace enumeration is bounded independently from file-byte verification.
func (self *attemptPreparationView) names(directory *os.File, maximum uint64) ([]string, error) {
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	view, err := attemptPreparationOpen(directory, ".", true)
	if err != nil {
		return nil, err
	}
	names, readErr := view.Readdirnames(int(maximum) + 1)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, view.Close()); err != nil {
		return nil, attemptLedgerCustodyObservation("cannot census preparation namespace", err)
	}
	if uint64(len(names)) > maximum {
		return nil, errors.Join(durablevolume.ErrUnavailable, errAttemptRecordStoreLimit)
	}
	sort.Strings(names)
	return names, nil
}

// Only the backend's canonical file names belong to the reviewed database census.
func attemptPreparationDescriptor(name string) (storage.FileDesc, bool) {
	for _, candidate := range []struct {
		kind           storage.FileType
		prefix, suffix string
	}{{kind: storage.TypeManifest, prefix: "MANIFEST-"}, {kind: storage.TypeJournal, suffix: ".log"}, {kind: storage.TypeTable, suffix: ".ldb"}} {
		if !strings.HasPrefix(name, candidate.prefix) || !strings.HasSuffix(name, candidate.suffix) {
			continue
		}
		number, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(name, candidate.prefix), candidate.suffix), 10, 64)
		descriptor := storage.FileDesc{Type: candidate.kind, Num: number}
		if err == nil && storage.FileDescOk(descriptor) && descriptor.String() == name {
			return descriptor, true
		}
	}
	return storage.FileDesc{}, false
}

// One bounded full hash establishes each retained member's exact initial bytes.
func (self *attemptPreparationView) observe(parent *os.File, name, relative string, maximum uint64) (_ attemptPreparationObservation, resultErr error) {
	file, err := attemptPreparationOpen(parent, name, false)
	if err != nil {
		return attemptPreparationObservation{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	var before, after, named unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &before); err != nil {
		return attemptPreparationObservation{}, attemptLedgerCustodyObservation("cannot inspect preparation file", err)
	}
	if before.Size < 0 || uint64(before.Size) > maximum {
		return attemptPreparationObservation{}, errAttemptRecordStoreLimit
	}
	digest := sha256.New()
	buffer := make([]byte, 64*1024)
	reader := io.NewSectionReader(file, 0, before.Size)
	for {
		if err := self.ctx.Err(); err != nil {
			return attemptPreparationObservation{}, err
		}
		n, readErr := reader.Read(buffer)
		_, _ = digest.Write(buffer[:n])
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return attemptPreparationObservation{}, attemptLedgerCustodyObservation("cannot hash preparation file", readErr)
		}
	}
	if err := errors.Join(unix.Fstat(int(file.Fd()), &after), unix.Fstatat(int(parent.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW)); err != nil {
		return attemptPreparationObservation{}, attemptLedgerCustodyObservation("cannot reobserve preparation file", err)
	}
	if !attemptPreparationSameStat(before, after) || !attemptPreparationSameStat(after, named) {
		return attemptPreparationObservation{}, attemptLedgerCustodyLoss("preparation file changed during read", nil)
	}
	return attemptPreparationObservation{stat: after, file: AttemptLedgerPreparationFile{Path: relative, Kind: "file", Mode: uint32(after.Mode) & 0777, Bytes: uint64(after.Size), Sha256: "sha256:" + hex.EncodeToString(digest.Sum(nil))}}, nil
}

// Every allowed backend and migration member is included; unknown names refuse.
func (self *attemptPreparationView) census(scope AttemptLedgerPreparationScope) ([]AttemptLedgerPreparationFile, error) {
	files, err := self.censusMembers(scope, false)
	if err != nil {
		return nil, err
	}
	if !self.anchorAbsent {
		var checkpoint attemptLedgerCustodyCheckpoint
		if err := attemptStoreDecode(self.anchor, &checkpoint); err != nil || !bytes.Equal(self.anchor, mustAttemptPreparationJSON(self.checkpoint(scope))) {
			return nil, errors.Join(attemptLedgerCustodyLoss("preparation checkpoint is pending or differs from original physical custody", nil), err)
		}
	}
	if _, err := self.requestCheckpoint(scope, nil, false); err != nil {
		return nil, err
	}
	return files, self.checkCensus()
}

// Restore's separately authenticated checkpoint may include one complete
// pending record. Fresh inspection retains its original strict no-pending rule.
func (self *attemptPreparationView) censusMembers(scope AttemptLedgerPreparationScope, retainedPending bool) ([]AttemptLedgerPreparationFile, error) {
	rootNames, err := self.names(self.root, self.limits.MaxStorageFiles+16)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{attemptLedgerStoreName: true, attemptLedgerImportName: true, attemptLedgerReadyName: true}
	if scope.Requests != nil {
		allowed[ProviderAttemptRequestJournalName] = true
	} else if !self.requestAnchorAbsent {
		return nil, attemptLedgerCustodyLoss("preparation cannot omit original request custody", nil)
	}
	if scope.Legacy != nil {
		allowed[attemptLedgerLegacyName] = true
	}
	if retainedPending {
		for _, name := range rootNames {
			if name == attemptLedgerPendingName {
				allowed[name] = true
			}
			if scope.Requests != nil && name == ProviderAttemptRequestPendingName {
				allowed[name] = true
			}
		}
	}
	for _, name := range rootNames {
		if (strings.HasPrefix(name, "attempt-ledger") || strings.HasPrefix(name, "provider-attempt-request")) && !allowed[name] {
			return nil, attemptLedgerCustodyLoss("preparation retains unknown or pending ledger custody", nil)
		}
	}
	files := []AttemptLedgerPreparationFile{{Path: attemptLedgerStoreName, Kind: "directory", Mode: uint32(self.backendStat.Mode) & 0777}}
	for _, name := range []string{attemptLedgerImportName, attemptLedgerReadyName, attemptLedgerLegacyName, attemptLedgerPendingName, ProviderAttemptRequestJournalName, ProviderAttemptRequestPendingName} {
		if !allowed[name] {
			continue
		}
		maximum := uint64(4096)
		if name == attemptLedgerImportName {
			maximum = scope.Limits.MaxRecordBytes*6 + 4096
		} else if name == attemptLedgerLegacyName {
			maximum = scope.Limits.MaxLegacyBytes
		} else if name == attemptLedgerPendingName {
			maximum = scope.Limits.MaxRecordBytes
		} else if name == ProviderAttemptRequestJournalName {
			maximum = scope.Requests.Preparation.Limits.MaxJournalBytes
		} else if name == ProviderAttemptRequestPendingName {
			maximum = scope.Requests.Preparation.Limits.MaxRecordBytes
		}
		observation, err := self.observe(self.root, name, name, maximum)
		if err != nil {
			return nil, err
		}
		self.members[name] = observation
		files = append(files, observation.file)
	}
	names, err := self.names(self.backend, self.limits.MaxStorageFiles)
	if err != nil {
		return nil, err
	}
	used, metadata := uint64(attemptStoreMetadataReserve), uint64(0)
	for _, name := range names {
		if _, known := attemptPreparationDescriptor(name); !known && name != "CURRENT" && name != "CURRENT.bak" && name != "LOCK" && name != "LOG" && name != "LOG.old" {
			return nil, attemptLedgerCustodyLoss("preparation backend contains an unknown or partial member", nil)
		}
		observation, err := self.observe(self.backend, name, attemptLedgerStoreName+"/"+name, self.limits.MaxStorageBytes)
		if err != nil {
			return nil, err
		}
		if attemptStoreMetadataName(name) {
			if observation.file.Bytes > attemptStoreMetadataReserve-metadata {
				return nil, errAttemptRecordStoreLimit
			}
			metadata += observation.file.Bytes
		} else {
			if observation.file.Bytes > self.limits.MaxStorageBytes-used {
				return nil, errAttemptRecordStoreLimit
			}
			used += observation.file.Bytes
		}
		self.members[observation.file.Path] = observation
		files = append(files, observation.file)
	}
	for _, name := range []string{"LOCK", "CURRENT"} {
		if _, present := self.members[attemptLedgerStoreName+"/"+name]; !present {
			return nil, attemptLedgerCustodyLoss("preparation backend lost original LOCK or CURRENT", nil)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, self.checkCensus()
}

// This struct contains only deterministic JSON-safe scalar fields.
// This fixed checkpoint struct has no values that can fail JSON serialization.
func mustAttemptPreparationJSON(value attemptLedgerCustodyCheckpoint) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

// Portable reviewed bytes gain only the actual inspected target's inode bindings.
func (self *attemptPreparationView) checkpoint(scope AttemptLedgerPreparationScope) attemptLedgerCustodyCheckpoint {
	member := func(name string) attemptLedgerCustodyMember {
		observed := self.members[name]
		return attemptLedgerCustodyMember{Inode: observed.stat.Ino, Bytes: observed.file.Bytes, Sha256: observed.file.Sha256}
	}
	result := attemptLedgerCustodyCheckpoint{Schema: attemptLedgerCustodySchema, Identity: scope.Identity, Coordinator: scope.Coordinator,
		DirectoryInode: self.rootStat.Ino, DatabaseInode: self.backendStat.Ino, Import: member(attemptLedgerImportName), Ready: member(attemptLedgerReadyName), Committed: scope.ExpectedHead}
	if scope.Legacy != nil {
		legacy := member(attemptLedgerLegacyName)
		result.Legacy = &legacy
	}
	return result
}

// A complete initial hash is followed by metadata/name rechecks. No unchanged
// backend is rehashed on every LevelDB block read during signature verification.
// Later checks compare original members without rehashing unchanged history.
func (self *attemptPreparationView) checkCensus() error {
	if err := self.ctx.Err(); err != nil {
		return err
	}
	var root, database unix.Stat_t
	if err := errors.Join(unix.Fstat(int(self.root.Fd()), &root), unix.Fstatat(int(self.root.Fd()), attemptLedgerStoreName, &database, unix.AT_SYMLINK_NOFOLLOW)); err != nil {
		return attemptLedgerCustodyObservation("cannot reobserve preparation directory", err)
	}
	if !attemptPreparationSameStat(root, self.rootStat) || !attemptPreparationSameStat(database, self.backendStat) {
		return attemptLedgerCustodyLoss("preparation directory changed during inspection", nil)
	}
	names, err := self.names(self.backend, self.limits.MaxStorageFiles)
	if err != nil {
		return err
	}
	backendCount := 0
	for name, observation := range self.members {
		parent, leaf := self.root, name
		if strings.HasPrefix(name, attemptLedgerStoreName+"/") {
			parent, leaf = self.backend, strings.TrimPrefix(name, attemptLedgerStoreName+"/")
			backendCount++
		}
		var current unix.Stat_t
		if err := unix.Fstatat(int(parent.Fd()), leaf, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return attemptLedgerCustodyObservation("cannot reobserve preparation census member", err)
		}
		if !attemptPreparationSameStat(observation.stat, current) {
			return attemptLedgerCustodyLoss("preparation census member changed during inspection", nil)
		}
	}
	if len(names) != backendCount {
		return attemptLedgerCustodyLoss("preparation backend member census changed", nil)
	}
	anchor, err := readAttemptLedgerCustodyAttribute(self.root)
	if err != nil && !(self.anchorAbsent && errors.Is(err, durablesys.ErrNoAttribute)) {
		return err
	}
	if self.anchorAbsent != errors.Is(err, durablesys.ErrNoAttribute) || !self.anchorAbsent && !bytes.Equal(anchor, self.anchor) {
		return attemptLedgerCustodyLoss("preparation custody checkpoint changed during inspection", nil)
	}
	requestAnchor, absent, err := readProviderAttemptRequestAttribute(self.root)
	if err != nil {
		return err
	}
	if absent != self.requestAnchorAbsent || !bytes.Equal(requestAnchor, self.requestAnchor) {
		return attemptLedgerCustodyLoss("preparation request custody changed during inspection", nil)
	}
	return nil
}

// Small public migration receipts are read only from their retained root name.
func (self *attemptPreparationView) readRoot(name string) (_ []byte, resultErr error) {
	observed, present := self.members[name]
	if !present {
		return nil, attemptLedgerCustodyLoss("preparation root member is not in the reviewed census", nil)
	}
	file, err := attemptPreparationOpen(self.root, name, false)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	raw := make([]byte, 0, observed.file.Bytes)
	buffer := make([]byte, 64*1024)
	for uint64(len(raw)) <= observed.file.Bytes {
		if err := self.ctx.Err(); err != nil {
			return nil, err
		}
		part := buffer[:min(uint64(len(buffer)), observed.file.Bytes+1-uint64(len(raw)))]
		n, err := file.Read(part)
		raw = append(raw, part[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, attemptLedgerCustodyObservation("cannot read preparation receipt", err)
		}
		if n == 0 {
			return nil, io.ErrNoProgress
		}
	}
	if uint64(len(raw)) != observed.file.Bytes || attemptLedgerCustodyDigest(raw) != observed.file.Sha256 {
		return nil, attemptLedgerCustodyLoss("preparation receipt differs from retained bytes", nil)
	}
	return raw, self.ctx.Err()
}

// Original legacy/import facts must match the exact reviewed signed prefix.
func (self *attemptPreparationView) verifyReceipts(scope AttemptLedgerPreparationScope, store *attemptRecordStore) error {
	markerRaw, err := self.readRoot(attemptLedgerImportName)
	if err != nil {
		return err
	}
	readyRaw, err := self.readRoot(attemptLedgerReadyName)
	if err != nil {
		return err
	}
	var marker attemptLedgerImport
	var ready attemptLedgerImportReady
	if err := errors.Join(attemptStoreDecode(markerRaw, &marker), attemptStoreDecode(readyRaw, &ready)); err != nil {
		return err
	}
	canonicalMarker, markerErr := json.Marshal(marker)
	canonicalReady, readyErr := json.Marshal(ready)
	if markerErr != nil || readyErr != nil || !bytes.Equal(canonicalMarker, markerRaw) || !bytes.Equal(canonicalReady, readyRaw) ||
		marker.Schema != attemptLedgerImportSchema || marker.Identity != scope.Identity || marker.Coordinator != scope.Coordinator || marker.LegacyPresent != (scope.Legacy != nil) ||
		ready.Schema != attemptLedgerImportSchema || ready.ImportSHA256 != attemptHex32(sha256.Sum256(markerRaw)) {
		return errors.New("preparation import or ready receipt differs from original namespace")
	}
	sequence, root := uint64(0), zeroAttemptHash()
	if scope.Legacy == nil {
		if marker.LegacyBytes != 0 || marker.LegacySHA256 != attemptHex32(sha256.Sum256(nil)) || marker.LocalDevice != 0 || marker.LocalInode != 0 {
			return errors.New("preparation import invents an absent legacy member")
		}
	} else {
		observed := self.members[attemptLedgerLegacyName].file
		if observed.Bytes != scope.Legacy.Bytes || observed.Sha256 != scope.Legacy.Sha256 || marker.LegacyBytes != observed.Bytes || marker.LegacySHA256 != "0x"+strings.TrimPrefix(observed.Sha256, "sha256:") {
			return errors.New("preparation import lost its exact reviewed legacy bytes")
		}
		file, err := attemptPreparationOpen(self.root, attemptLedgerLegacyName, false)
		if err != nil {
			return err
		}
		err = func() error {
			reader := bufio.NewReaderSize(io.LimitReader(file, int64(observed.Bytes)), 64*1024)
			var line []byte
			for {
				if err := self.ctx.Err(); err != nil {
					return err
				}
				part, readErr := reader.ReadSlice('\n')
				if uint64(len(line)+len(part)) > scope.Limits.MaxRecordBytes+1 {
					return errAttemptRecordStoreLimit
				}
				line = append(line, part...)
				if errors.Is(readErr, bufio.ErrBufferFull) {
					continue
				}
				if errors.Is(readErr, io.EOF) {
					if len(line) != 0 {
						return errors.New("preparation legacy member has a torn final record")
					}
					return nil
				}
				if readErr != nil {
					return readErr
				}
				if len(line) > 1 {
					if sequence == store.head.LastSequence {
						return errors.New("preparation database omits retained legacy records")
					}
					record, err := store.readRecord(sequence + 1)
					if err != nil {
						return err
					}
					raw, err := json.Marshal(record)
					if err != nil || !bytes.Equal(raw, line[:len(line)-1]) || record.PreviousHash != root {
						return errors.Join(errors.New("preparation legacy record differs from original signed prefix"), err)
					}
					sequence, root = record.Sequence, record.RecordHash
				}
				line = line[:0]
			}
		}()
		if err := errors.Join(err, file.Close()); err != nil {
			return err
		}
	}
	if ready.LastSequence != sequence || ready.Root != root || sequence > store.head.LastSequence {
		return errors.New("preparation ready receipt does not retain the original import prefix")
	}
	return self.checkCensus()
}

// Only the engine's small session state is mutable. All persistent methods
// deny writes; original LOCK is observed by census rather than created here.
type attemptPreparationReadOnlyStorage struct {
	stateLock sync.Mutex
	view      *attemptPreparationView
	locked    bool
	closed    bool
}

// The engine's session lock is independent of the borrowed physical owner lock.
type attemptPreparationReadOnlyLock struct {
	owner *attemptPreparationReadOnlyStorage
	once  sync.Once
}

// A second engine release cannot clear a later session's ownership.
func (self *attemptPreparationReadOnlyLock) Unlock() {
	self.once.Do(func() {
		self.owner.stateLock.Lock()
		defer self.owner.stateLock.Unlock()
		self.owner.locked = false
	})
}

// Engine initialization acquires only this bounded read-only session flag.
func (self *attemptPreparationReadOnlyStorage) Lock() (storage.Locker, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closed {
		return nil, storage.ErrClosed
	}
	if self.locked {
		return nil, storage.ErrLocked
	}
	self.locked = true
	return &attemptPreparationReadOnlyLock{owner: self}, nil
}

// Diagnostic logging cannot create a database LOG file during inspection.
func (self *attemptPreparationReadOnlyStorage) Log(string) {}

// CURRENT is immutable throughout offline read-only inspection.
func (self *attemptPreparationReadOnlyStorage) SetMeta(storage.FileDesc) error {
	return os.ErrPermission
}

// No backend output descriptor can be acquired through the read-only adapter.
func (self *attemptPreparationReadOnlyStorage) Create(storage.FileDesc) (storage.Writer, error) {
	return nil, os.ErrPermission
}

// Inspection cannot reap original database members.
func (self *attemptPreparationReadOnlyStorage) Remove(storage.FileDesc) error {
	return os.ErrPermission
}

// Inspection cannot replace an original named member.
func (self *attemptPreparationReadOnlyStorage) Rename(storage.FileDesc, storage.FileDesc) error {
	return os.ErrPermission
}

// The caller joins engine users before closing the borrowed physical view.
func (self *attemptPreparationReadOnlyStorage) check() error {
	self.stateLock.Lock()
	closed := self.closed
	self.stateLock.Unlock()
	if closed {
		return storage.ErrClosed
	}
	return self.view.ctx.Err()
}

// Only a canonical CURRENT pointing at the retained manifest is accepted.
func (self *attemptPreparationReadOnlyStorage) GetMeta() (storage.FileDesc, error) {
	if err := self.check(); err != nil {
		return storage.FileDesc{}, err
	}
	file, err := attemptPreparationOpen(self.view.backend, "CURRENT", false)
	if err != nil {
		return storage.FileDesc{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 128))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return storage.FileDesc{}, err
	}
	descriptor, valid := attemptPreparationDescriptor(strings.TrimSuffix(string(raw), "\n"))
	if !valid || descriptor.Type != storage.TypeManifest || descriptor.String()+"\n" != string(raw) {
		return storage.FileDesc{}, errors.New("preparation CURRENT is absent or not canonical")
	}
	if _, present := self.view.members[attemptLedgerStoreName+"/"+descriptor.String()]; !present {
		return storage.FileDesc{}, attemptLedgerCustodyLoss("preparation CURRENT selects an absent manifest", nil)
	}
	return descriptor, nil
}

// Stable enumeration comes from the bounded initial physical census.
func (self *attemptPreparationReadOnlyStorage) List(types storage.FileType) ([]storage.FileDesc, error) {
	if err := self.check(); err != nil {
		return nil, err
	}
	var descriptors []storage.FileDesc
	for path := range self.view.members {
		if !strings.HasPrefix(path, attemptLedgerStoreName+"/") {
			continue
		}
		descriptor, valid := attemptPreparationDescriptor(strings.TrimPrefix(path, attemptLedgerStoreName+"/"))
		if valid && descriptor.Type&types != 0 {
			descriptors = append(descriptors, descriptor)
		}
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].String() < descriptors[j].String() })
	return descriptors, nil
}

// Returned readers retain original member identity and bounded cancellation checks.
func (self *attemptPreparationReadOnlyStorage) Open(descriptor storage.FileDesc) (storage.Reader, error) {
	if err := self.check(); err != nil {
		return nil, err
	}
	observed, present := self.view.members[attemptLedgerStoreName+"/"+descriptor.String()]
	if !storage.FileDescOk(descriptor) || !present {
		return nil, storage.ErrInvalidFile
	}
	file, err := attemptPreparationOpen(self.view.backend, descriptor.String(), false)
	if err != nil {
		return nil, err
	}
	var current unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &current); err != nil {
		return nil, errors.Join(attemptLedgerCustodyObservation("cannot inspect preparation backend read", err), file.Close())
	}
	if !attemptPreparationSameStat(observed.stat, current) {
		return nil, errors.Join(attemptLedgerCustodyLoss("preparation backend changed before read", nil), file.Close())
	}
	return &attemptPreparationReader{File: file, ctx: self.view.ctx}, nil
}

// Engine closure changes no persistent bytes and does not own the caller's root.
func (self *attemptPreparationReadOnlyStorage) Close() error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.closed = true
	return nil
}

// Each LevelDB block read observes cancellation without abandoning a worker.
type attemptPreparationReader struct {
	*os.File
	ctx context.Context
}

// Chunked reads preserve naked EOF and admit no bytes after a post-read failure.
func (self *attemptPreparationReader) Read(raw []byte) (int, error) {
	if err := self.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := self.File.Read(raw[:min(len(raw), 64*1024)])
	if checkErr := self.ctx.Err(); checkErr != nil {
		return 0, errors.Join(err, checkErr)
	}
	return n, err
}

// Random reads admit the entire result only while every bounded chunk remains
// authorized. Cancellation does not rewind the descriptor or admit a prefix.
func (self *attemptPreparationReader) ReadAt(raw []byte, offset int64) (int, error) {
	total := 0
	for total < len(raw) {
		if err := self.ctx.Err(); err != nil {
			return 0, err
		}
		end := min(len(raw), total+64*1024)
		n, err := self.File.ReadAt(raw[total:end], offset+int64(total))
		total += n
		if checkErr := self.ctx.Err(); checkErr != nil {
			return 0, errors.Join(err, checkErr)
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrNoProgress
		}
	}
	if err := self.ctx.Err(); err != nil {
		return 0, err
	}
	return total, nil
}

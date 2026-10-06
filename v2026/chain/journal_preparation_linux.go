//go:build linux

// Native preparation describes public empty custody. It never opens a writer,
// rewrites a retained journal, initializes an xattr, or authorizes an extrinsic.
package chain

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

const NativeJournalPreparationKind = "native-journal"
const NativeJournalPreparationSchema = "urnetwork-native-journal-preparation-v1"

// The reviewed capacity vector matches the existing runtime's independent
// journal, raw-member, total-byte and per-record limits. It grants no expansion.
type NativeJournalPreparationScope struct {
	Schema                string `json:"schema"`
	MaximumJournalBytes   uint64 `json:"maximum_journal_bytes"`
	MaximumRawBytes       uint64 `json:"maximum_raw_bytes"`
	MaximumRawMembers     uint64 `json:"maximum_raw_members"`
	MaximumRawRecordBytes uint64 `json:"maximum_raw_record_bytes"`
}

// Fresh preparation supports exactly the current native runtime profile.
func (self NativeJournalPreparationScope) validate() error {
	if self.Schema != NativeJournalPreparationSchema || self.MaximumJournalBytes != 16*1024*1024 ||
		self.MaximumRawBytes != 64*1024*1024 || self.MaximumRawMembers != 10000 || self.MaximumRawRecordBytes != 1024*1024 {
		return errors.New("native preparation capacity vector differs from the fixed runtime profile")
	}
	return nil
}

// The fixed portable census contains no account, nonce, signature or call.
func NativeJournalPreparationLayout(scope NativeJournalPreparationScope) ([]durablevolume.PreparationFile, error) {
	if err := scope.validate(); err != nil {
		return nil, err
	}
	return []durablevolume.PreparationFile{
		{Path: journalRawDir, Kind: "directory", Mode: 0700},
		{Path: journalFileName, Kind: "file", Mode: 0600, Sha256: "sha256:" + nativeDigest(nil)},
	}, nil
}

// The borrowed descriptor must already be private and physically admitted by
// the base plan. Observation failure is not proof of changed identity.
func nativePreparationStat(file *os.File, directory bool) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if file == nil {
		return stat, errors.New("native preparation requires its borrowed descriptor")
	}
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return stat, nativeObservation(err, false)
	}
	kind, mode := uint32(unix.S_IFREG), uint32(0600)
	if directory {
		kind, mode = unix.S_IFDIR, 0700
	}
	if stat.Mode&unix.S_IFMT != kind || stat.Mode&07777 != mode || stat.Uid != uint32(os.Geteuid()) || !directory && (stat.Size != 0 || stat.Nlink != 1) {
		return stat, errors.Join(durablevolume.ErrIdentity, errors.New("fresh native member is not an original private empty member"))
	}
	return stat, nil
}

// Named and opened members must remain the same physical generation throughout
// read-only checkpoint construction. No failed observation invents a mismatch.
func nativePreparationNamed(root *os.File, name string, file *os.File, before unix.Stat_t, directory bool) error {
	after, err := nativePreparationStat(file, directory)
	if err != nil {
		return err
	}
	var named unix.Stat_t
	if err := unix.Fstatat(int(root.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nativeObservation(err, true)
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim ||
		named.Dev != after.Dev || named.Ino != after.Ino || named.Mode != after.Mode || named.Size != after.Size || named.Uid != after.Uid || named.Gid != after.Gid || named.Nlink != after.Nlink {
		return errors.Join(durablevolume.ErrIdentity, errors.New("fresh native member changed during checkpoint construction"))
	}
	return nil
}

// A fresh checkpoint is computed only from the actual empty log/raw inodes.
// The caller publishes it with XATTR_CREATE after its plan-owned file steps;
// this read-only adapter cannot enroll an arbitrary root or repair old custody.
func BuildFreshNativeJournalPreparationCheckpoint(ctx context.Context, root *os.File, scope NativeJournalPreparationScope) (result []byte, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = nil
		}
	}()
	if ctx == nil {
		return nil, errors.New("native preparation requires cancellation context")
	}
	if err := errors.Join(ctx.Err(), scope.validate()); err != nil {
		return nil, err
	}
	rootStat, err := nativePreparationStat(root, true)
	if err != nil {
		return nil, err
	}
	open := func(name string, directory bool) (*os.File, unix.Stat_t, error) {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if directory {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(int(root.Fd()), name, flags, 0)
		if err != nil {
			return nil, unix.Stat_t{}, nativeObservation(err, true)
		}
		file := os.NewFile(uintptr(fd), name)
		stat, err := nativePreparationStat(file, directory)
		if err != nil {
			return nil, stat, errors.Join(err, file.Close())
		}
		return file, stat, nil
	}
	journal, journalStat, err := open(journalFileName, false)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, journal.Close()) }()
	raw, rawStat, err := open(journalRawDir, true)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, raw.Close()) }()
	names, err := raw.Readdirnames(1)
	if err != nil && err != io.EOF {
		return nil, nativeObservation(err, false)
	}
	if len(names) != 0 {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("fresh native raw directory retains existing intent"))
	}
	if err := errors.Join(ctx.Err(), nativePreparationNamed(root, journalFileName, journal, journalStat, false), nativePreparationNamed(root, journalRawDir, raw, rawStat, true)); err != nil {
		return nil, err
	}
	after, err := nativePreparationStat(root, true)
	if err != nil {
		return nil, err
	}
	if after.Dev != rootStat.Dev || after.Ino != rootStat.Ino || journalStat.Dev != rootStat.Dev || rawStat.Dev != rootStat.Dev {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("native preparation root or member device changed"))
	}
	checkpoint := nativeJournalCustody{Schema: nativeJournalCustodySchema, DirectoryInode: rootStat.Ino, RawDirectoryInode: rawStat.Ino,
		Committed: nativeJournalCheckpoint{JournalInode: journalStat.Ino, JournalSha256: nativeDigest(nil), RawSha256: nativeRawDigest(nil)}}
	encoded, err := json.Marshal(checkpoint)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return encoded, nil
}

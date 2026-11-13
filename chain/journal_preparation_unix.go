//go:build linux || darwin

// Native preparation describes public empty custody. It never opens a writer,
// rewrites a retained journal, initializes an xattr, or authorizes an extrinsic.
package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect/durablevolume"
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
	if uint32(stat.Mode)&unix.S_IFMT != kind || uint32(stat.Mode)&07777 != mode || stat.Uid != uint32(os.Geteuid()) || !directory && (stat.Size != 0 || stat.Nlink != 1) {
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

// FreshNativeJournalPreparationScope is the one capacity vector validate admits,
// so a request can name it without repeating its numbers.
func FreshNativeJournalPreparationScope() NativeJournalPreparationScope {
	return NativeJournalPreparationScope{Schema: NativeJournalPreparationSchema, MaximumJournalBytes: 16 * 1024 * 1024, MaximumRawBytes: 64 * 1024 * 1024, MaximumRawMembers: 10000, MaximumRawRecordBytes: 1024 * 1024}
}

// Public inputs are one strict object, with nothing after it.
func nativePreparationDecode(raw []byte, target any) error {
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("native preparation input has trailing JSON")
	}
	return nil
}

// BuildFreshNativeJournalPreparation is the native journal's entry in a fixed
// owner registry, shared by every command that prepares one. It creates only
// public empty staging bytes, which the base planner reads and binds itself.
func BuildFreshNativeJournalPreparation(ctx context.Context, staging *os.File, name string, owner durablevolume.PreparationOwner) (durablevolume.PreparationOwnerPlan, error) {
	if owner.Kind != NativeJournalPreparationKind || owner.Purpose != "fresh" || owner.RelativePath != "." {
		return durablevolume.PreparationOwnerPlan{}, errors.New("fixed preparation supports explicitly fresh custody only")
	}
	var scope NativeJournalPreparationScope
	if err := nativePreparationDecode(owner.Inputs, &scope); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	files, err := NativeJournalPreparationLayout(scope)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	raw, err := json.Marshal(scope)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if err := nativePreparationStage(ctx, staging, name, files); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, Files: files, Census: raw,
		Attributes: []durablevolume.PreparationAttributeSpec{{Path: ".", Name: NativeJournalCustodyAttribute}}}, nil
}

// InspectFreshNativeJournalPreparation derives the only permitted format again
// from the original request, then observes the published members for its checkpoint.
func InspectFreshNativeJournalPreparation(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan) ([]durablevolume.PreparedAttribute, error) {
	if owner.Owner.Kind != NativeJournalPreparationKind || owner.Owner.Purpose != "fresh" || owner.Owner.RelativePath != "." {
		return nil, errors.New("fixed preparation cannot reinterpret retained or restored custody as fresh")
	}
	var scope, census NativeJournalPreparationScope
	if err := errors.Join(nativePreparationDecode(owner.Owner.Inputs, &scope), nativePreparationDecode(owner.Census, &census)); err != nil {
		return nil, err
	}
	if scope != census {
		return nil, errors.New("native preparation census changed its reviewed capacity vector")
	}
	files, err := NativeJournalPreparationLayout(scope)
	if err != nil {
		return nil, err
	}
	spec := durablevolume.PreparationAttributeSpec{Path: ".", Name: NativeJournalCustodyAttribute}
	if !reflect.DeepEqual(files, owner.Files) || len(owner.Attributes) != 1 || owner.Attributes[0] != spec {
		return nil, errors.New("fresh owner plan changed its fixed member or checkpoint census")
	}
	raw, err := BuildFreshNativeJournalPreparationCheckpoint(ctx, root, scope)
	if err != nil {
		return nil, err
	}
	return []durablevolume.PreparedAttribute{{Spec: spec, Raw: raw}}, nil
}

// Exclusive and private: every member is created new under a new directory.
func nativePreparationStage(ctx context.Context, parent *os.File, name string, files []durablevolume.PreparationFile) (resultErr error) {
	if ctx == nil || parent == nil || name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsRune(name, 0) {
		return errors.New("fresh owner staging requires its named borrowed parent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Mkdirat(int(parent.Fd()), name, 0700); err != nil {
		return err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), name)
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	for _, member := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		flags := unix.O_RDWR | unix.O_CREAT | unix.O_EXCL | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if member.Kind == "directory" {
			if err := unix.Mkdirat(fd, member.Path, member.Mode); err != nil {
				return err
			}
			flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		}
		child, err := unix.Openat(fd, member.Path, flags, member.Mode)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(child), member.Path)
		if err := errors.Join(file.Sync(), file.Close()); err != nil {
			return err
		}
	}
	return errors.Join(directory.Sync(), parent.Sync(), ctx.Err())
}

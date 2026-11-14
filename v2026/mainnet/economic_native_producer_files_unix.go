//go:build linux || darwin

// Derived jobs are immutable, bounded evidence under an explicitly declared
// private root. Their existence grants no cursor or signing authority. The
// existing native checkpoint names completed evidence and owns accounting.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urnetwork/connect/v2026/durablesys"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

type nativeProducerSyncKey struct{}
type nativeProducerFileSyncKey struct{}

// A failed owner close must survive a simultaneous caller cancellation. The
// role cannot safely reacquire this producer until cleanup is resolved.
var errNativeProducerCleanup = errors.New("native execution producer cleanup is unresolved")

func nativeProducerCloseError(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(errNativeProducerCleanup, err)
}

// The instance-only hook can refuse an actual directory sync; CLI inputs cannot select it.
func (self *nativeProducerFiles) sync(directory *os.File, relative string) error {
	if hook, ok := self.ctx.Value(nativeProducerSyncKey{}).(func(string, *os.File) error); ok {
		if err := hook(relative, directory); err != nil {
			return err
		}
	}
	return errors.Join(directory.Sync(), self.checkChild(directory, filepath.Dir(relative)))
}

func (self *nativeProducerFiles) syncFile(file *os.File, relative string) error {
	if hook, ok := self.ctx.Value(nativeProducerFileSyncKey{}).(func(string, *os.File) error); ok {
		if err := hook(relative, file); err != nil {
			return err
		}
	}
	return file.Sync()
}

// Reopen the complete checked relative path to retain its named directory inode.
func (self *nativeProducerFiles) checkChild(directory *os.File, relative string) error {
	named, err := self.child(relative, false)
	if err != nil {
		return err
	}
	var before, after unix.Stat_t
	err = errors.Join(unix.Fstat(int(directory.Fd()), &before), unix.Fstat(int(named.Fd()), &after), named.Close())
	if err != nil {
		return err
	}
	if before.Dev != after.Dev || before.Ino != after.Ino {
		return durablevolume.ErrIdentity
	}
	return self.check()
}

type nativeProducerFiles struct {
	ctx       context.Context
	directory *durablepath.Directory
	path      string
	policy    nativeExecutionProducerPolicy
	forecast  *nativeProducerResourceForecast
}

func openNativeProducerFiles(ctx context.Context, execution *nativeExecutionPolicy) (_ *nativeProducerFiles, resultErr error) {
	files, err := openNativeEvidenceFiles(ctx, execution.Directory, execution.Producer.MaximumBytes, execution.Producer.MaximumEntries)
	if err != nil {
		return nil, err
	}
	files.policy = *execution.Producer
	return files, nil
}

// The file owner grants only exclusive bounded evidence custody. An unsigned
// capture may use it without constructing a producer or financial authority.
func openNativeEvidenceFiles(ctx context.Context, path string, maximumBytes, maximumEntries uint64) (_ *nativeProducerFiles, resultErr error) {
	return openNativeEvidenceFilesInScope(ctx, path, maximumBytes, maximumEntries, false)
}

// Only the unsigned archive command can explicitly choose owner-local evidence.
// The producer constructor above always retains strict daemon admission.
func openNativeEvidenceFilesInScope(ctx context.Context, path string, maximumBytes, maximumEntries uint64, ownerLocal bool) (_ *nativeProducerFiles, resultErr error) {
	open := durablepath.Open
	if ownerLocal {
		open = durablepath.OpenOwnerLocal
	}
	directory, err := open(ctx, path, durablevolume.ReadWrite, false)
	if err != nil {
		return nil, err
	}
	self := &nativeProducerFiles{ctx: ctx, directory: directory, path: path, policy: nativeExecutionProducerPolicy{MaximumBytes: maximumBytes, MaximumEntries: maximumEntries}}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	if err := unix.Flock(int(directory.File().Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(durablevolume.ErrBusy, err)
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	return self, nil
}

func (self *nativeProducerFiles) close() error {
	if self == nil || self.directory == nil {
		return nil
	}
	err := nativeProducerCloseError(self.directory.Close())
	self.directory = nil
	return err
}

func (self *nativeProducerFiles) check() error {
	if self == nil || self.directory == nil {
		return os.ErrClosed
	}
	return errors.Join(self.ctx.Err(), self.directory.CheckWrite())
}

// Every intermediate name is checked without following links. These are
// producer-owned derived directories, never application state enrollment.
func (self *nativeProducerFiles) child(relative string, create bool) (_ *os.File, resultErr error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return nil, errors.New("native evidence child path is not canonical")
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	fd, err := unix.Dup(int(self.directory.File().Fd()))
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), self.path)
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, current.Close())
		}
	}()
	if relative == "." {
		return current, nil
	}
	for _, name := range strings.Split(relative, "/") {
		if name == "." || name == ".." || name == "" || len(name) > 160 {
			return nil, errors.New("native evidence child component is invalid")
		}
		if create {
			if err := unix.Mkdirat(int(current.Fd()), name, 0700); err == nil {
				// A restrictive umask may remove required bits; only our new inode gets
				// its explicit private mode before any payload is published beneath it.
				fresh, openErr := unix.Openat(int(current.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				if openErr != nil {
					return nil, openErr
				}
				file := os.NewFile(uintptr(fresh), name)
				modeErr := file.Chmod(0700)
				syncErr := file.Sync()
				closeErr := file.Close()
				if err := errors.Join(modeErr, syncErr, closeErr, current.Sync()); err != nil {
					return nil, err
				}
			} else if !errors.Is(err, unix.EEXIST) {
				return nil, err
			}
		}
		nextFd, err := unix.Openat(int(current.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		next := os.NewFile(uintptr(nextFd), filepath.Join(current.Name(), name))
		var stat unix.Stat_t
		statErr := unix.Fstat(nextFd, &stat)
		if statErr == nil && (stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&07777 != 0700 || stat.Uid != uint32(os.Geteuid())) {
			statErr = errors.New("native evidence directory is not private")
		}
		closeErr := current.Close()
		current = next
		if err := errors.Join(statErr, closeErr); err != nil {
			return nil, err
		}
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	return current, nil
}

// One stat census reserves all three dimensions before starting another job.
// It reads no historical payload and is bounded separately from job count.
func (self *nativeProducerFiles) admit(relative string) error {
	return self.admitMargin(1)
}

func (self *nativeProducerFiles) admitMargin(margin uint64) error {
	if margin == 0 || margin > 2 {
		return errors.New("native producer resource margin is invalid")
	}
	if err := self.check(); err != nil {
		return err
	}
	var entries, used uint64
	var scan func(*os.File, int) error
	scan = func(directory *os.File, depth int) error {
		if depth > 3 {
			return errors.New("native evidence namespace depth is invalid")
		}
		names, err := directory.Readdirnames(int(self.policy.MaximumEntries-entries) + 1)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		for _, name := range names {
			if err := self.ctx.Err(); err != nil {
				return err
			}
			entries++
			if entries > self.policy.MaximumEntries {
				return errMonitorEconomicCapacity
			}
			var stat unix.Stat_t
			if err := unix.Fstatat(int(directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return err
			}
			if stat.Uid != uint32(os.Geteuid()) || stat.Mode&0077 != 0 || stat.Size < 0 {
				return errors.New("native evidence member ownership or size changed")
			}
			switch stat.Mode & unix.S_IFMT {
			case unix.S_IFDIR:
				fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				if err != nil {
					return err
				}
				child := os.NewFile(uintptr(fd), filepath.Join(directory.Name(), name))
				if err := errors.Join(scan(child, depth+1), child.Close()); err != nil {
					return err
				}
			case unix.S_IFREG:
				if stat.Nlink != 1 {
					return errors.New("native evidence file acquired a second link")
				}
				if uint64(stat.Size) > self.policy.MaximumBytes-used {
					return errMonitorEconomicCapacity
				}
				used += uint64(stat.Size)
			default:
				return errors.New("native evidence contains an unsupported member")
			}
		}
		return nil
	}
	// Dup shares the enumeration offset; openat(".") owns a fresh cursor.
	fd, err := unix.Openat(int(self.directory.File().Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	root := os.NewFile(uintptr(fd), self.path)
	if err := errors.Join(scan(root, 0), root.Close()); err != nil {
		return err
	}
	if self.policy.MaximumBytes < margin*nativeProducerBoundaryReserve || self.policy.MaximumEntries < margin*nativeProducerBoundaryEntries || used > self.policy.MaximumBytes-margin*nativeProducerBoundaryReserve || entries > self.policy.MaximumEntries-margin*nativeProducerBoundaryEntries {
		return errMonitorEconomicCapacity
	}
	if err := self.check(); err != nil {
		return err
	}
	self.forecast = &nativeProducerResourceForecast{BytesUpperBound: used + nativeProducerBoundaryReserve, EntriesUpperBound: entries + nativeProducerBoundaryEntries}
	return nil
}

func nativeProducerPrivateFile(file *os.File, maximum int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0077 != 0 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || stat.Size < 0 || stat.Size > int64(maximum) {
		return errors.New("native evidence file custody or bound differs")
	}
	return nil
}

// A read returns no bytes if its closing identity/context check fails.
func (self *nativeProducerFiles) read(relative string, maximum int) (result []byte, resultErr error) {
	directory, err := self.child(filepath.Dir(relative), false)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, nativeProducerCloseError(directory.Close()))
		if resultErr != nil {
			result = nil
		}
	}()
	fd, err := unix.Openat(int(directory.Fd()), filepath.Base(relative), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(self.path, relative))
	if err := nativeProducerPrivateFile(file, maximum); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	var before unix.Stat_t
	err = unix.Fstat(fd, &before)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	var named unix.Stat_t
	namedErr := unix.Fstatat(int(directory.Fd()), filepath.Base(relative), &named, unix.AT_SYMLINK_NOFOLLOW)
	var after unix.Stat_t
	statErr := unix.Fstat(fd, &after)
	if statErr == nil && (before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid || before.Nlink != after.Nlink || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim || after.Size != int64(len(raw))) {
		statErr = durablevolume.ErrIdentity
	}
	if namedErr == nil {
		var held unix.Stat_t
		namedErr = unix.Fstat(fd, &held)
		if namedErr == nil && (named.Dev != held.Dev || named.Ino != held.Ino) {
			namedErr = errors.New("native evidence named inode changed during read")
		}
	}
	if err := errors.Join(readErr, statErr, namedErr, file.Close(), self.checkChild(directory, filepath.Dir(relative))); err != nil {
		return nil, err
	}
	if len(raw) > maximum {
		return nil, errMonitorEconomicCapacity
	}
	return raw, nil
}

// A deterministic, exact-byte pending inode handles lost publication acks.
// Partial or different pending bytes remain unresolved; nothing is truncated.
func (self *nativeProducerFiles) publish(relative string, raw []byte, maximum int) (result planFileReference, resultErr error) {
	if len(raw) == 0 || len(raw) > maximum {
		return planFileReference{}, errMonitorEconomicCapacity
	}
	ref := planFileReference{Path: filepath.Join(self.path, relative), Sha256: monitorReadDigest(raw)}
	if original, err := self.read(relative, maximum); err == nil {
		if !bytes.Equal(original, raw) {
			return planFileReference{}, errors.Join(errRpcIntegrity, errors.New("native evidence publication differs from original bytes"))
		}
		directory, err := self.child(filepath.Dir(relative), false)
		if err != nil {
			return planFileReference{}, err
		}
		if err := errors.Join(self.sync(directory, relative), directory.Close(), self.check()); err != nil {
			return planFileReference{}, err
		}
		return ref, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return planFileReference{}, err
	}
	directory, err := self.child(filepath.Dir(relative), true)
	if err != nil {
		return planFileReference{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, nativeProducerCloseError(directory.Close()))
		if resultErr != nil {
			result = planFileReference{}
		}
	}()
	name := filepath.Base(relative)
	pending := name + ".pending"
	fd, err := unix.Openat(int(directory.Fd()), pending, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	created := err == nil
	if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Openat(int(directory.Fd()), pending, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return planFileReference{}, err
	}
	file := os.NewFile(uintptr(fd), pending)
	defer func() {
		resultErr = errors.Join(resultErr, nativeProducerCloseError(file.Close()))
		if resultErr != nil {
			result = planFileReference{}
		}
	}()
	if created {
		if err := file.Chmod(0600); err != nil {
			return planFileReference{}, err
		}
		n, err := file.Write(raw)
		if err != nil || n != len(raw) {
			if err == nil {
				err = io.ErrShortWrite
			}
			return planFileReference{}, err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return planFileReference{}, err
		}
	}
	if err := nativeProducerPrivateFile(file, maximum); err != nil {
		return planFileReference{}, err
	}
	var original unix.Stat_t
	if err := unix.Fstat(fd, &original); err != nil {
		return planFileReference{}, err
	}
	visible, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return planFileReference{}, err
	}
	if !bytes.Equal(visible, raw) {
		return planFileReference{}, errors.Join(errRpcIntegrity, errors.New("native evidence pending publication is incomplete or changed"))
	}
	checkPending := func(name string) error {
		var held, named unix.Stat_t
		if err := errors.Join(unix.Fstat(fd, &held), unix.Fstatat(int(directory.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW)); err != nil {
			return err
		}
		if original.Dev != held.Dev || original.Ino != held.Ino || original.Mode != held.Mode || original.Uid != held.Uid || original.Gid != held.Gid || original.Nlink != held.Nlink || original.Size != held.Size || original.Mtim != held.Mtim || original.Ctim != held.Ctim || held.Size != int64(len(raw)) || named.Dev != held.Dev || named.Ino != held.Ino {
			return durablevolume.ErrIdentity
		}
		return self.checkChild(directory, filepath.Dir(relative))
	}
	if err := checkPending(pending); err != nil {
		return planFileReference{}, err
	}
	// Equal visible bytes after a lost file-sync acknowledgement do not
	// establish durability. Re-sync this same inode before publishing its name.
	if err := self.syncFile(file, relative); err != nil {
		return planFileReference{}, err
	}
	if err := checkPending(pending); err != nil {
		return planFileReference{}, err
	}
	if err := durablesys.RenameNoReplace(int(directory.Fd()), pending, int(directory.Fd()), name); err != nil {
		return planFileReference{}, err
	}
	// Rename changes ctime on some filesystems. The final name must still
	// identify the held, synced inode; payload admission was checked above.
	var final unix.Stat_t
	if err := unix.Fstatat(int(directory.Fd()), name, &final, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return planFileReference{}, err
	}
	if final.Dev != original.Dev || final.Ino != original.Ino {
		return planFileReference{}, durablevolume.ErrIdentity
	}
	if err := errors.Join(self.sync(directory, relative), self.check()); err != nil {
		return planFileReference{}, err
	}
	return ref, nil
}

func (self *nativeProducerFiles) relative(ref planFileReference) (string, error) {
	relative, err := filepath.Rel(self.path, ref.Path)
	if err != nil || !planSha256(ref.Sha256) || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || filepath.IsAbs(relative) {
		return "", fmt.Errorf("native evidence reference is outside the original declared artifact root")
	}
	return relative, nil
}

func (self *nativeProducerFiles) readReference(ref planFileReference, maximum int) ([]byte, error) {
	relative, err := self.relative(ref)
	if err != nil {
		return nil, err
	}
	raw, err := self.read(relative, maximum)
	if err != nil {
		return nil, err
	}
	if monitorReadDigest(raw) != ref.Sha256 {
		return nil, errors.Join(errRpcIntegrity, errors.New("native acknowledged evidence digest changed"))
	}
	return raw, nil
}

// Durable-volume owners supplement the original local journal and signed
// inode bindings. They never replace those existing custody checks.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
)

// Constructors require an explicit operational declaration independently of
// command dispatch. Historical wire bytes never select a filesystem fallback.
// The enclosing journal owner serializes all methods and joins before close;
// reference contexts never share this mutable owner between commands.
type mainnetDurableDirectory struct {
	ctx            context.Context
	directory      *durablepath.Directory
	path           string
	marker         *os.File
	markerRaw      []byte
	retainedPath   string
	retainedHash   string
	retainedDevice uint64
	retainedInode  uint64
	failed         error
	head           *durablehead.Owner
	headPath       string
}

// This requires close/reopen reconciliation of this original owner. It is
// neither a proven identity loss nor permission to retry a mutation in place.
var errMainnetDurablePublicationUncertain = errors.New("durable journal publication is uncertain; reopen original custody")

// Proven custody loss cannot be repaired within the same owner by restoring
// a pathname. An explicit close and original-state recovery is required.
func (self *mainnetDurableDirectory) identity(message string, cause error) error {
	self.failed = errors.Join(durablevolume.ErrIdentity, errors.New(message), cause)
	return self.failed
}

// A failed observation refuses admission without claiming an identity change.
func mainnetDurableUnavailable(message string, cause error) error {
	return errors.Join(&durablevolume.UnavailableError{Reason: message}, cause)
}

// Application locks share the guard's admission classes. Contention consumes
// no allowance and remains retryable after the existing owner has joined.
func mainnetDurableFlock(fd, mode int) error {
	err := unix.Flock(fd, mode|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		access := durablevolume.ReadOnly
		if mode&unix.LOCK_EX != 0 {
			access = durablevolume.ReadWrite
		}
		return errors.Join(&durablevolume.BusyError{Access: access}, err)
	}
	if err != nil {
		return mainnetDurableUnavailable("cannot acquire application custody lock", err)
	}
	return nil
}

// Only admission failures can be retried in place. A proven integrity loss
// always wins over a joined capacity, cancellation, or lease observation.
func mainnetDurableAdmissionPending(err error) bool {
	if err == nil || errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, errRpcIntegrity) || errors.Is(err, errMainnetDurablePublicationUncertain) {
		return false
	}
	return errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrBusy) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func openMainnetDurableDirectory(ctx context.Context, path string, access durablevolume.Access) (*mainnetDurableDirectory, error) {
	if ctx == nil {
		return nil, errors.New("durable mainnet owner context is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory, err := durablepath.Open(ctx, path, access, false)
	if err != nil {
		return nil, err
	}
	return &mainnetDurableDirectory{ctx: ctx, directory: directory, path: path}, nil
}

// The owner's own computer is independent of daemon deployment volumes. Only
// this signing-store constructor selects the separately approved local schema.
func openOwnerLocalDurableDirectory(ctx context.Context, path string) (*mainnetDurableDirectory, error) {
	if ctx == nil {
		return nil, errors.New("owner-local storage context is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory, err := durablepath.OpenOwnerLocal(ctx, path, durablevolume.ReadWrite, false)
	if err != nil {
		return nil, err
	}
	return &mainnetDurableDirectory{ctx: ctx, directory: directory, path: path}, nil
}

// A retained descriptor must still name the same directory as the volume
// guard. The application's original stronger leaf/inode checks remain active.
func (self *mainnetDurableDirectory) check(file *os.File) error {
	if self == nil {
		return errors.New("mainnet custody requires an admitted physical owner")
	}
	if self.failed != nil {
		return self.failed
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	if err := self.directory.CheckRead(); err != nil {
		return err
	}
	if self.head != nil {
		if err := self.snapshotError(self.head.Check()); err != nil {
			return err
		}
	}
	if self.marker != nil {
		var opened, named unix.Stat_t
		if err := unix.Fstat(int(self.marker.Fd()), &opened); err != nil {
			return mainnetDurableUnavailable("cannot inspect retained custody marker", err)
		}
		if err := unix.Fstatat(int(self.directory.File().Fd()), filepath.Base(self.marker.Name()), &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) {
				return self.identity("durable custody marker disappeared", err)
			}
			return mainnetDurableUnavailable("cannot inspect named custody marker", err)
		}
		if opened.Dev != named.Dev || opened.Ino != named.Ino || named.Mode&unix.S_IFMT != unix.S_IFREG || named.Mode&0077 != 0 || named.Nlink != 1 {
			return self.identity("durable custody marker identity changed", nil)
		}
		if self.markerRaw != nil {
			raw, err := io.ReadAll(io.NewSectionReader(self.marker, 0, int64(len(self.markerRaw)+1)))
			if err != nil {
				return mainnetDurableUnavailable("cannot read retained custody marker", err)
			}
			if !bytes.Equal(raw, self.markerRaw) {
				return self.identity("durable custody marker bytes changed", nil)
			}
		}
	}
	if file == nil {
		return nil
	}
	opened, err := file.Stat()
	guarded, guardErr := self.directory.File().Stat()
	if err != nil || guardErr != nil {
		return mainnetDurableUnavailable("cannot compare application directory", errors.Join(err, guardErr))
	}
	if !os.SameFile(opened, guarded) {
		return self.identity("application directory differs from durable-volume owner", nil)
	}
	return nil
}

// The original lock remains the application fence. This guard borrows it and
// checks its physical identity; completion bytes are captured only after the
// original constructor has finished its existing interrupted-claim protocol.
func (self *mainnetDurableDirectory) bindMarker(marker *os.File, complete bool) error {
	if self == nil {
		return errors.New("marker custody requires an admitted physical owner")
	}
	self.marker = marker
	if err := self.check(nil); err != nil {
		return err
	}
	if complete {
		raw, err := io.ReadAll(io.NewSectionReader(marker, 0, 4097))
		if err != nil || len(raw) == 0 || len(raw) > 4096 {
			return errors.Join(errors.New("durable custody marker exceeds its bound"), err)
		}
		self.markerRaw = raw
	}
	return self.check(nil)
}

// Actual opens stay relative to the retained directory. Missing admission is
// an error at this boundary, including for callers outside command dispatch.
func (self *mainnetDurableDirectory) openFile(path string, flags int, mode uint32) (int, error) {
	if self == nil {
		return -1, errors.New("file access requires an admitted physical owner")
	}
	if filepath.Dir(path) != self.path {
		return -1, errors.New("durable custody file is outside its owned directory")
	}
	if flags&(unix.O_WRONLY|unix.O_RDWR|unix.O_CREAT|unix.O_TRUNC) != 0 {
		if err := self.checkWrite(nil); err != nil {
			return -1, err
		}
	} else if err := self.check(nil); err != nil {
		return -1, err
	}
	return unix.Openat(int(self.directory.File().Fd()), filepath.Base(path), flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, mode)
}

// A guarded read tracks the original journal, including disappearance after a
// successful read. Its bounded bytes remain the existing signed wire format.
func (self *mainnetDurableDirectory) readFile(ctx context.Context, path string, maximum int) ([]byte, string, error) {
	return self.readOwnedFile(ctx, path, maximum, true)
}

// Auxiliary responses have their own signed request binding. They share the
// same physical directory without replacing the tracked journal predecessor.
func (self *mainnetDurableDirectory) readAuxiliaryFile(ctx context.Context, path string, maximum int) ([]byte, string, error) {
	return self.readOwnedFile(ctx, path, maximum, false)
}

func (self *mainnetDurableDirectory) readOwnedFile(ctx context.Context, path string, maximum int, retain bool) ([]byte, string, error) {
	if self == nil {
		return nil, "", errors.New("custody read requires an admitted physical owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if retain && self.head != nil {
		if path != self.headPath {
			return nil, "", errors.New("snapshot read differs from the admitted journal")
		}
		if err := self.check(nil); err != nil {
			return nil, "", err
		}
		raw, present, err := self.head.Read()
		if err != nil {
			return nil, "", self.snapshotError(err)
		}
		if !present {
			return nil, "", os.ErrNotExist
		}
		if len(raw) == 0 || len(raw) > maximum {
			return nil, "", self.identity("snapshot journal exceeds its application bound", nil)
		}
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		return raw, monitorReadDigest(raw), nil
	}
	fd, err := self.openFile(path, unix.O_RDONLY, 0)
	if err != nil {
		if retain && self.retainedPath == path && (errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP)) {
			err = self.identity("retained durable journal disappeared", err)
		}
		return nil, "", err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return nil, "", mainnetDurableUnavailable("cannot inspect durable journal", err)
	}
	if opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Mode&0077 != 0 || opened.Nlink != 1 {
		return nil, "", self.identity("durable journal is not a private single-link regular file", nil)
	}
	if retain && self.retainedPath == path && (uint64(opened.Dev) != self.retainedDevice || opened.Ino != self.retainedInode) {
		return nil, "", self.identity("retained durable journal inode changed", nil)
	}
	raw, err := io.ReadAll(io.LimitReader(&mainnetDurableReader{ctx: ctx, owner: self.ctx, file: file}, int64(maximum)+1))
	if err != nil {
		return nil, "", mainnetDurableUnavailable("cannot read durable journal", err)
	}
	if len(raw) == 0 || len(raw) > maximum {
		return nil, "", self.identity("durable journal exceeds its byte bound", nil)
	}
	if err := errors.Join(self.check(nil), ctx.Err()); err != nil {
		return nil, "", err
	}
	var named unix.Stat_t
	if err := unix.Fstatat(int(self.directory.File().Fd()), filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) {
			return nil, "", self.identity("durable journal disappeared while reading", err)
		}
		return nil, "", mainnetDurableUnavailable("cannot inspect named durable journal", err)
	}
	if opened.Dev != named.Dev || opened.Ino != named.Ino {
		return nil, "", self.identity("durable journal changed while reading", nil)
	}
	hash := monitorReadDigest(raw)
	if retain && self.retainedPath != "" && (self.retainedPath != path || self.retainedHash != hash) {
		return nil, "", self.identity("durable journal differs from retained custody", nil)
	}
	if retain {
		self.retainedPath, self.retainedHash = path, hash
		self.retainedDevice, self.retainedInode = uint64(opened.Dev), opened.Ino
	}
	return raw, hash, nil
}

// File data, the atomic publication and directory durability are acknowledged
// together. Refusal before publication remains retryable; once replacement is
// attempted a fresh owner reconciles original bytes and any lost sync receipt.
func (self *mainnetDurableDirectory) publish(path string, raw []byte, syncDirectory func(*os.File) error) (resultErr error) {
	if self == nil {
		return errors.New("custody publication requires an admitted physical owner")
	}
	if self.head != nil {
		if path != self.headPath {
			return errors.New("publication differs from the admitted snapshot journal")
		}
		if err := self.checkWrite(nil); err != nil {
			return err
		}
		return self.snapshotError(self.head.Publish(raw, syncDirectory))
	}
	publicationAttempted := false
	defer func() {
		if resultErr != nil && publicationAttempted {
			self.failed = errors.Join(errMainnetDurablePublicationUncertain, resultErr)
			resultErr = self.failed
		}
	}()
	checkPrevious := func() error {
		if err := self.checkWrite(nil); err != nil {
			return err
		}
		if self.retainedPath != "" {
			_, _, err := self.readFile(self.ctx, path, 64*1024*1024)
			return err
		}
		return nil
	}
	if err := checkPrevious(); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	stage := filepath.Join(filepath.Dir(path), ".durable-custody-"+hex.EncodeToString(nonce[:]))
	fd, err := self.openFile(stage, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0600)
	if err != nil {
		return err
	}
	directoryFd := int(self.directory.File().Fd())
	defer unix.Unlinkat(directoryFd, filepath.Base(stage), 0)
	file := os.NewFile(uintptr(fd), stage)
	var staged unix.Stat_t
	if err := unix.Fstat(fd, &staged); err != nil {
		return errors.Join(err, file.Close())
	}
	written, err := file.Write(raw)
	if err == nil && written != len(raw) {
		err = io.ErrShortWrite
	}
	if err := errors.Join(err, file.Sync(), file.Close(), checkPrevious()); err != nil {
		return err
	}
	publicationAttempted = true
	if err := unix.Renameat(directoryFd, filepath.Base(stage), directoryFd, filepath.Base(path)); err != nil {
		return err
	}
	if syncDirectory == nil {
		syncDirectory = (*os.File).Sync
	}
	if err := errors.Join(syncDirectory(self.directory.File()), self.checkWrite(nil)); err != nil {
		return err
	}
	var published unix.Stat_t
	if err := unix.Fstatat(directoryFd, filepath.Base(path), &published, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if published.Dev != staged.Dev || published.Ino != staged.Ino {
		return self.identity("published durable journal inode changed", nil)
	}
	self.retainedPath, self.retainedHash = path, monitorReadDigest(raw)
	self.retainedDevice, self.retainedInode = uint64(staged.Dev), staged.Ino
	return nil
}

// Regular-file reads have a finite byte budget and check both caller and
// retained owner cancellation between bounded chunks. No background read can
// silently extend the lifetime of its physical owner.
type mainnetDurableReader struct {
	ctx   context.Context
	owner context.Context
	file  *os.File
}

func (self *mainnetDurableReader) Read(raw []byte) (int, error) {
	if err := errors.Join(self.ctx.Err(), self.owner.Err()); err != nil {
		return 0, err
	}
	return self.file.Read(raw[:min(len(raw), 64*1024)])
}

func (self *mainnetDurableDirectory) checkWrite(file *os.File) error {
	if err := self.check(file); err != nil || self == nil {
		return err
	}
	return self.directory.CheckWrite()
}

func (self *mainnetDurableDirectory) close() error {
	if self == nil {
		return nil
	}
	var err error
	if self.head != nil {
		err = self.head.Close()
	}
	return errors.Join(err, self.directory.Close())
}

// An omitted context carries no admission. The physical constructor rejects it
// even for direct private callers; tests provision explicit declarations.
func mainnetStorageContext(contexts []context.Context) context.Context {
	if len(contexts) == 1 {
		return contexts[0]
	}
	if len(contexts) != 0 {
		return nil
	}
	return context.Background()
}

// Retained observations and effects require local custody. Only explicit pure
// review modes may run before provisioning; their own parsers still bind scope.
func mainnetRequiresDurableVolumes(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "treasury":
		if len(args) > 1 {
			switch args[1] {
			case "describe", "observe", "plan", "policy-plan", "inspect-request", "ledger-plan":
				return false
			}
		}
		return true
	case "owner-signing":
		return len(args) > 1 && args[1] == "sign"
	case "owner-recycle", "root-register":
		if len(args) > 1 {
			switch args[1] {
			case "observe", "plan", "inspect-request", "ledger-plan":
				return false
			}
		}
		return true
	case "bootstrap-chain":
		if len(args) > 1 {
			switch args[1] {
			case "root-registration":
				return mainnetRequiresDurableVolumes(append([]string{"root-register"}, args[2:]...))
			case "plan", "contract-plan", "contract-role-plan":
				return false
			}
		}
		return true
	case "bootstrap", "root-service", "root-passive-service":
		return len(args) < 2 || args[1] != "plan"
	case "bootstrap-contracts":
		return len(args) < 2 || args[1] != "plan" && args[1] != "preview"
	case "activate-root-passive", "activate-validators", "repair-validator", "repair-active-validator",
		"storage-inventory", "storage-verify", "storage-owner-inventory", "storage-owner-verify", "storage-inspect", "validator-capacity-preview", "monitor-native-archive", "monitor-native-catalog", "monitor-evm-archive", "monitor-evm-catalog", "monitor-claim-archive", "monitor-claim-catalog", "observe-economic-conservation", "economic-conservation-archive":
		return true
	}
	return false
}

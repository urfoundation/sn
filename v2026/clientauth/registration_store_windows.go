//go:build windows

package clientauth

// One owner-private directory handle and nonblocking owner lock retain
// registration custody across its request and credential handoff. Every write
// syncs both the file and directory before its caller may use the new state.
// Descriptor-relative custody holds here as on Unix: leaf operations name the
// directory through its open handle's RootDirectory, and every open tolerates
// no reparse point where the Unix store tolerates no symlink.
//
// Ownership is deliberately more lenient here than the Unix euid check. A
// custody object may be owned by the process account or by the token's default
// owner, which is BUILTIN\Administrators for any elevated process, and every
// access check allowlists Administrators and SYSTEM beside those two accounts.
// An administrator's process really does own what it creates through its
// default owner, and an administrator is the local equivalent of root, so a
// directory an administrator planted is custody the same way a root-owned
// directory is on Unix. Nothing outside that set is ever accepted, and a
// refusal names the account and right that is outside it.

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const maximumRegistrationBytes = 32 * 1024

// Alternate leaf open mode, in place of the Unix store's flag argument.
const leafOpenRead = iota + 1

// Every open shares read, write and delete, exactly as the Win32 handles the
// Go runtime produces do, so LockFileEx is the only exclusion and a leaf open
// while another handle holds it is never a sharing violation.
const custodyShareAccess = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

// ACE, ACL and descriptor values the x/sys build in use does not name. The
// custody audit classifies every ACE type it can meet on a file object, so it
// needs the full set rather than the allowed and denied pair that package
// declares.
const (
	aceAllowedCallback       = 9
	aceAllowedObject         = 5
	aceAllowedCallbackObject = 11
	aceDeniedCallback        = 10
	aceDeniedObject          = 6
	aceDeniedCallbackObject  = 12
	aceSystemAudit           = 2
	aceSystemAlarm           = 3
	aceSystemAuditObject     = 7
	aceSystemAlarmObject     = 8
	aceSystemMandatoryLabel  = 0x11
	aceSystemResourceAttr    = 0x12
	aceSystemTrustLabel      = 0x13
	aceSystemScopedPolicy    = 0x14
	aceSystemAccessFilter    = 0x15

	aclRevision               = 2
	fileAllAccess             = 0x001F01FF
	descriptorRevision        = 1
	fileDeleteChildAccess     = 0x00010000
	renameReplaceIfExistsFlag = 0x00000001
	renamePosixSemanticsFlag  = 0x00000002
	// the two ignore-read-only bits are not in this x/sys version. Both need
	// Windows 10 1809 or later; an older host answers the call with an invalid
	// parameter or an unknown information class, which the existing fallbacks
	// already handle.
	dispositionIgnoreReadOnlyFlag = 0x00000010
	renameIgnoreReadOnlyFlag      = 0x00000040
)

// fileIDInfo has no host type in this x/sys version; the volume serial and the
// 128-bit file id together form the directory anchor, the Dev/Ino equivalent.
type fileIDInfo struct {
	VolumeSerialNumber uint64
	FileId             [16]byte
}

type custodyAnchor struct {
	volumeSerial uint64
	fileId       [16]byte
}

type registrationStore struct {
	directory *os.File
	lock      *os.File
	path      string
	client    string
	anchor    custodyAnchor
}

// Refuse namespace redirection before opening secrets or a durable request.
func openRegistrationStore(clientPath string) (_ *registrationStore, returnErr error) {
	if !filepath.IsAbs(clientPath) || filepath.Clean(clientPath) != clientPath || filepath.Dir(clientPath) == clientPath {
		return nil, errors.New("registration credential path is not canonical absolute")
	}
	dir := filepath.Dir(clientPath)
	// Parents of the custody directory are created with the mode the OS
	// chooses, as the Unix store's MkdirAll does for a path whose parents are
	// not yet there; only the custody directory itself is created private.
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return nil, err
	}
	// The walk descends through each parent's own handle and refuses a reparse
	// point at every component, the Windows parity of the Unix walk that
	// refuses symlink components; the final handle is the custody handle every
	// leaf open anchors to through RootDirectory.
	directory, err := openCustodyDirectory(dir)
	if err != nil {
		return nil, err
	}
	self := &registrationStore{directory: directory, path: dir, client: filepath.Base(clientPath)}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, self.close())
		}
	}()
	if err := self.adoptAnchor(); err != nil {
		return nil, err
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	self.lock, err = self.ensureLeaf(self.client+".registration.lock", custodyLockAccess)
	if err != nil {
		return nil, err
	}
	if err := self.acquireOwnerLock(); err != nil {
		return nil, err
	}
	return self, nil
}

// Provider slots borrow the key owner's physical directory, never a fresh
// pathname selection. The handle remains tied to that directory identity
// across reads, writes and retries; check refuses a renamed or replaced
// visible namespace.
//
// The parent comparison is an exact string comparison, so a case or 8.3 short
// name alias of the owner's directory refuses: the alias names the same
// directory object, and failing closed there costs nothing.
func openRegistrationStoreForOwner(clientPath string, owner *registrationStore) (_ *registrationStore, returnErr error) {
	if owner == nil || owner.directory == nil || owner.lock == nil || !filepath.IsAbs(clientPath) || filepath.Clean(clientPath) != clientPath || filepath.Dir(clientPath) != owner.path {
		return nil, errors.New("provider client custody differs from its retained directory owner")
	}
	if err := owner.check(); err != nil {
		return nil, err
	}
	// The child duplicates the owner's own handle: the same directory object,
	// with no second pathname selection and no re-open of the directory by an
	// empty object name that older NT versions resolve differently.
	directory, err := selfChildOf(owner.directory)
	if err != nil {
		return nil, err
	}
	self := &registrationStore{directory: directory, path: owner.path, client: filepath.Base(clientPath), anchor: owner.anchor}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, self.close())
		}
	}()
	if err := self.check(); err != nil {
		return nil, err
	}
	self.lock, err = self.ensureLeaf(self.client+".registration.lock", custodyLockAccess)
	if err != nil {
		return nil, err
	}
	if err := self.acquireOwnerLock(); err != nil {
		return nil, err
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	return self, nil
}

// acquireOwnerLock takes the client's exclusive owner lock over the lock
// handle's first byte without waiting; the lock file is per client
// (<client>.registration.lock), the same lock the Unix store takes, so a
// second store for the same client means the active owner keeps its
// registration custody. Every open shares read, write and delete, so a held
// lock is the only way this call loses: a lock violation alone is a held
// owner, and any other failure is reported as itself rather than as a busy
// owner.
func (self *registrationStore) acquireOwnerLock() error {
	if err := windows.LockFileEx(windows.Handle(self.lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}); err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return errRegistrationOwnerActive
		}
		return err
	}
	return nil
}

// openLeaf opens one existing leaf through the directory handle and accepts it
// only on safe descriptor metadata, then applies the Unix no-symlink parity
// audit. The name is validated here as it is on every other leaf operation, so
// the read path refuses what the writing paths refuse.
func (self *registrationStore) openLeaf(name string, access uint32) (*os.File, error) {
	if invalidLeaf(name) {
		return nil, errors.New("registration leaf is invalid")
	}
	file, err := ntOpenRelative(self.directory, name, access, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
	if err != nil {
		return nil, err
	}
	if err := auditLeafCustody(file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// createLeaf creates one leaf through the directory handle with the private
// descriptor, so the leaf inherits nothing from where its directory lives. It
// does not audit: the caller audits inside its own cleanup scope, so a leaf
// that fails its audit is still removed.
func (self *registrationStore) createLeaf(name string, access uint32) (*os.File, error) {
	if invalidLeaf(name) {
		return nil, errors.New("registration leaf is invalid")
	}
	raw, err := privateCustodyDescriptor()
	if err != nil {
		return nil, err
	}
	file, createErr := ntOpenRelativeDescriptor(self.directory, name, access, windows.FILE_CREATE, (*windows.SECURITY_DESCRIPTOR)(unsafe.Pointer(&raw[0])))
	runtime.KeepAlive(raw)
	return file, createErr
}

// ensureLeaf opens one leaf, creating it with the private descriptor when it is
// absent, and audits the result.
func (self *registrationStore) ensureLeaf(name string, access uint32) (*os.File, error) {
	if invalidLeaf(name) {
		return nil, errors.New("registration leaf is invalid")
	}
	raw, err := privateCustodyDescriptor()
	if err != nil {
		return nil, err
	}
	file, openErr := ntOpenRelativeDescriptor(self.directory, name, access, windows.FILE_OPEN_IF, (*windows.SECURITY_DESCRIPTOR)(unsafe.Pointer(&raw[0])))
	runtime.KeepAlive(raw)
	if openErr != nil {
		return nil, openErr
	}
	if err := auditLeafCustody(file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// invalidLeaf refuses a name that is not one plain component of the custody
// directory. The NT object name space also resolves a stream of a leaf by
// separating it with a colon and a path by a backslash, and filepath.Base does
// not treat either as a separator on Windows, so both are refused here: a
// name that named a stream would read or replace data beside a custody file
// rather than the custody file itself.
func invalidLeaf(name string) bool {
	return name == "" || filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, `:\/`)
}

func (self *registrationStore) open(name string, flags int) (*os.File, error) {
	switch flags {
	case leafOpenRead:
		return self.openLeaf(name, custodyReadAccess)
	default:
		return nil, errors.New("registration leaf open mode is unsupported")
	}
}

// The leaf and component masks. A plain generic mask does not include the
// attribute and security reads that the leaf's own audit and the walk's
// reparse refusal query, so every mask names them.
const (
	custodyReadAccess  = windows.FILE_GENERIC_READ | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE
	custodyWriteAccess = windows.FILE_GENERIC_WRITE | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE
	custodyLockAccess  = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE
	// custodyTraverseAccess is what a component of the walk asks for: enough to
	// traverse it and stat it for the reparse refusal, and nothing that reads
	// its contents or writes. An ancestor where the account holds traverse but
	// not list rights is enough, as an execute-only directory is on unix.
	custodyTraverseAccess = windows.FILE_TRAVERSE | windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE
)

// Reopen checks only authenticate namespace identity; operations keep using
// the original handle even if a path is maliciously changed in between.
func (self *registrationStore) check() error {
	info, err := os.Lstat(self.path)
	if err != nil {
		return err
	}
	owned, err := self.directory.Stat()
	if err != nil {
		return err
	}
	current, err := self.anchorOf(windows.Handle(self.directory.Fd()))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, owned) || current != self.anchor {
		return errors.New("registration custody directory identity changed")
	}
	if err := self.auditDirectoryCustody(); err != nil {
		return err
	}
	return nil
}

func (self *registrationStore) anchorOf(handle windows.Handle) (custodyAnchor, error) {
	var raw fileIDInfo
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, (*byte)(unsafe.Pointer(&raw)), uint32(unsafe.Sizeof(raw))); err != nil {
		return custodyAnchor{}, err
	}
	return custodyAnchor{volumeSerial: raw.VolumeSerialNumber, fileId: raw.FileId}, nil
}

// adoptAnchor takes the anchor at custody open: directory identity and one
// private owner. The reparse refusal happens earlier, on the walk that opened
// this handle, and on the attributes of the very handle checked here.
func (self *registrationStore) adoptAnchor() error {
	anchor, err := self.anchorOf(windows.Handle(self.directory.Fd()))
	if err != nil {
		return err
	}
	if err := self.auditDirectoryCustody(); err != nil {
		return err
	}
	self.anchor = anchor
	return nil
}

func (self *registrationStore) read(name string) ([]byte, error) {
	if err := self.check(); err != nil {
		return nil, err
	}
	file, err := self.open(name, leafOpenRead)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maximumRegistrationBytes+1))
	closeErr := file.Close()
	if len(raw) > maximumRegistrationBytes {
		readErr = errors.Join(readErr, errors.New("registration data exceeds its finite bound"))
	}
	if err := errors.Join(readErr, closeErr, self.check()); err != nil {
		return nil, err
	}
	return raw, nil
}

// A single bounded census detects old identity files before first-key creation.
func (self *registrationStore) names(maximum int) ([]string, error) {
	if maximum < 1 || maximum > 4096 {
		return nil, errors.New("registration directory census bound differs")
	}
	if err := self.check(); err != nil {
		return nil, err
	}
	if _, err := self.directory.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	names, err := self.directory.Readdirnames(maximum + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > maximum {
		return nil, errors.New("registration directory census exceeds its bound")
	}
	return names, self.check()
}

// Deletion uses the same owned directory as publication: one handle opened
// through the custody handle, marked for deletion with POSIX semantics before
// its close. A leaf that is a symlink is deleted as the link, exactly as unix
// unlinkat removes it. A missing leaf is already absent; a successful removal
// is durable before the caller proceeds.
func (self *registrationStore) remove(name string) error {
	if invalidLeaf(name) {
		return errors.New("registration removal leaf is invalid")
	}
	if err := self.check(); err != nil {
		return err
	}
	file, err := self.openForDelete(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return self.check()
		}
		return err
	}
	closeErr := markDeleteDisposition(file)
	if err := errors.Join(closeErr, file.Close()); err != nil {
		return err
	}
	return errors.Join(self.syncDirectory(), self.check())
}

// removeHeld removes one leaf through the custody handle with no custody check
// of its own. It exists for the cleanup of a temporary this store created: the
// check that refused the operation which produced the temporary would refuse
// its cleanup in exactly the same way, and the temporary would be left behind
// for good. Unix cleans up with an unconditional unlinkat on the held
// directory descriptor for the same reason.
func (self *registrationStore) removeHeld(name string) error {
	if invalidLeaf(name) {
		return errors.New("registration removal leaf is invalid")
	}
	file, err := self.openForDelete(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	closeErr := markDeleteDisposition(file)
	return errors.Join(closeErr, file.Close())
}

// openForDelete opens one leaf for removal or rename through the custody
// handle. Go's own path opens share read and write but not delete, so a
// concurrent reader that opened the token by pathname (ReadToken,
// readNetworkCredential) makes this open fail with a sharing violation for the
// moment it holds the file. Unix has no such failure, and a removal there is
// never refused by a reader, so the open is retried briefly before the
// violation is reported.
func (self *registrationStore) openForDelete(name string) (*os.File, error) {
	const attempts = 20
	const interval = 25 * time.Millisecond
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		var file *os.File
		file, err = ntOpenRelative(self.directory, name, windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, err
		}
		// the last attempt returns its own failure rather than waiting first
		if attempt < attempts-1 {
			time.Sleep(interval)
		}
	}
	return nil, err
}

// Rename publishes complete bytes; a failed directory sync remains an error
// and never licenses a second operation or a credential before durable handoff.
func (self *registrationStore) write(name string, raw []byte) (returnErr error) {
	if invalidLeaf(name) || len(raw) == 0 || len(raw) > maximumRegistrationBytes {
		return errors.New("registration write exceeds its finite bound")
	}
	if err := self.check(); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := ".registration-" + hex.EncodeToString(random[:])
	file, err := self.createLeaf(temporary, custodyWriteAccess)
	if err != nil {
		return err
	}
	closed := false
	// The cleanup is registered before the leaf is audited, so a leaf that
	// fails its own audit is removed rather than left in the directory.
	defer func() {
		if !closed {
			returnErr = errors.Join(returnErr, file.Close())
		}
		if returnErr != nil {
			returnErr = errors.Join(returnErr, self.removeHeld(temporary))
		}
	}()
	if err := auditLeafCustody(file); err != nil {
		return err
	}
	_, writeErr := io.Copy(file, bytes.NewReader(raw))
	syncErr := file.Sync()
	closeErr := file.Close()
	closed = true
	if err := errors.Join(writeErr, syncErr, closeErr, self.check()); err != nil {
		return err
	}
	if err := self.renameLeaf(temporary, name); err != nil {
		return err
	}
	return errors.Join(self.syncDirectory(), self.check())
}

// renameLeaf publishes one same-directory rename through the custody handle:
// the source leaf is opened through that handle and the target is named
// relative to it, so publication is descriptor-relative as on unix and a
// component swapped in the visible namespace between check() and the rename is
// never consulted.
func (self *registrationStore) renameLeaf(from, to string) error {
	if invalidLeaf(from) || invalidLeaf(to) {
		return errors.New("registration rename leaf is invalid")
	}
	if err := self.check(); err != nil {
		return err
	}
	leaf, err := self.openForDelete(from)
	if err != nil {
		return err
	}
	renameErr := renameRelative(leaf, self.directory, to)
	if err := errors.Join(renameErr, leaf.Close()); err != nil {
		return err
	}
	return self.check()
}

func (self *registrationStore) syncDirectory() error {
	return windows.FlushFileBuffers(windows.Handle(self.directory.Fd()))
}

// auditDirectoryCustody is the Windows parity of the Unix Mode&0022 bound: a
// directory owned by the process account, not writable by anyone outside its
// custody allowlist. Windows hosts already carry a .urnetwork directory that
// an earlier tool created with inherited access, so a refusal here names the
// account and right that are outside custody and the command that repairs the
// directory in place, rather than reporting that custody is impossible.
func (self *registrationStore) auditDirectoryCustody() error {
	return auditPrivateOwnership(windows.Handle(self.directory.Fd()), true,
		func(sid *windows.SID, mask uint32) error { return custodyDirectoryRefusal(self.path, sid, mask) })
}

// custodyDirectoryRefusal names what is outside custody and how to repair the
// directory. An administrator can apply the private descriptor to a directory
// the account owns without losing what is already in it.
//
// The grant carries (OI)(CI) because the repaired directory has to pass that
// access on to the files created inside it: a grant without the inherit flags
// leaves the directory itself private while every child created by pathname
// takes the creating token's default DACL instead, which is the state this
// audit exists to refuse.
func custodyDirectoryRefusal(dir string, sid *windows.SID, mask uint32) error {
	remedy := fmt.Sprintf(`repair it with: icacls "%s" /inheritance:r /grant:r "%%USERNAME%%":(OI)(CI)F SYSTEM:(OI)(CI)F Administrators:(OI)(CI)F`, dir)
	switch {
	case sid != nil && mask != 0:
		return fmt.Errorf("registration custody directory %s grants %s to %s; %s", dir, custodyRightNames(mask), sid.String(), remedy)
	case sid != nil:
		return fmt.Errorf("registration custody directory %s is owned by %s rather than by this account; %s", dir, sid.String(), remedy)
	default:
		return fmt.Errorf("registration custody directory %s is not private: its DACL allows every account access, or the process account cannot be resolved; %s", dir, remedy)
	}
}

// custodyRightNames names the audited rights an ACE carries, so a refusal can
// say what the foreign account may do.
func custodyRightNames(mask uint32) string {
	names := make([]string, 0, 8)
	for _, right := range []struct {
		bit  uint32
		name string
	}{
		{windows.FILE_READ_DATA, "read"},
		{windows.FILE_WRITE_DATA, "write"},
		{windows.FILE_APPEND_DATA, "append"},
		{fileDeleteChildAccess, "delete-child"},
		{windows.DELETE, "delete"},
		{windows.WRITE_DAC, "change-permissions"},
		{windows.WRITE_OWNER, "change-owner"},
		{windows.MAXIMUM_ALLOWED, "maximum-allowed"},
	} {
		if mask&right.bit != 0 {
			names = append(names, right.name)
		}
	}
	if len(names) == 0 {
		return fmt.Sprintf("access 0x%08x", mask)
	}
	return strings.Join(names, ", ")
}

// Releasing the lock and directory is part of the caller's completed result.
func (self *registrationStore) close() error {
	if self == nil {
		return nil
	}
	var err error
	if self.lock != nil {
		err = self.lock.Close()
		self.lock = nil
	}
	if self.directory != nil {
		err = errors.Join(err, self.directory.Close())
		self.directory = nil
	}
	return err
}

// openCustodyRoot opens the volume root that the descending walk starts from.
// The root of a volume cannot be a reparse point, so it is the one component
// that needs no attribute refusal of its own.
func openCustodyRoot(volume string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(volume + string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name,
		windows.FILE_LIST_DIRECTORY|windows.FILE_TRAVERSE|windows.READ_CONTROL|windows.SYNCHRONIZE,
		custodyShareAccess, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), volume+string(filepath.Separator)), nil
}

// openCustodyDirectory walks dir from its volume root one component at a time,
// opening each through its parent's own handle and refusing a reparse point on
// the handle that comes back before descending further. That is the Windows
// form of the Unix Openat(O_NOFOLLOW) chain: a junction or symlink anywhere on
// the path is refused rather than followed, and every component is named
// relative to the handle above it instead of by absolute pathname.
//
// Intermediate components ask only for traverse, list and read, so a standard
// account reaches a directory deep in its profile without holding write rights
// on the volume or on its parents. Only the final custody directory is created
// if absent, carries the private descriptor, and holds the write access its
// FlushFileBuffers needs.
func openCustodyDirectory(dir string) (*os.File, error) {
	volume := filepath.VolumeName(dir)
	if volume == "" {
		return nil, errors.New("registration custody path names no volume")
	}
	components := strings.Split(strings.TrimPrefix(strings.TrimPrefix(dir, volume), string(filepath.Separator)), string(filepath.Separator))
	current, err := openCustodyRoot(volume)
	if err != nil {
		return nil, err
	}
	for index, component := range components {
		if component == "" {
			continue
		}
		var next *os.File
		if index == len(components)-1 {
			next, err = createCustodyDirectory(current, component)
		} else {
			next, err = ntOpenDirectoryRelative(current, component,
				custodyTraverseAccess,
				windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT)
		}
		closeErr := current.Close()
		if err != nil {
			return nil, errors.Join(err, closeErr)
		}
		if closeErr != nil {
			return nil, errors.Join(closeErr, next.Close())
		}
		if err := refuseReparse(next); err != nil {
			return nil, errors.Join(err, next.Close())
		}
		current = next
	}
	return current, nil
}

// createCustodyDirectory opens or creates the final custody component through
// its parent's handle with the inheritable private descriptor, so a directory
// this store creates inherits nothing from where it happens to live and passes
// its own allowlist on to every child created inside it by pathname. An
// existing one is opened unchanged and audited by the caller, which refuses it
// unless it is already private.
func createCustodyDirectory(parent *os.File, name string) (*os.File, error) {
	descriptor, err := privateCustodyDirectoryDescriptor()
	if err != nil {
		return nil, err
	}
	return ntOpenObject(parent, name,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.SYNCHRONIZE,
		windows.FILE_OPEN_IF,
		windows.FILE_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT,
		windows.OBJ_CASE_INSENSITIVE|windows.OBJ_DONT_REPARSE, (*windows.SECURITY_DESCRIPTOR)(unsafe.Pointer(&descriptor[0])))
}

// refuseReparse is the walk's per-component refusal: the handle just opened
// names a reparse point itself, so the tree beyond it is never entered.
func refuseReparse(file *os.File) error {
	var attributes uint32
	if err := openAttributes(windows.Handle(file.Fd()), &attributes); err != nil {
		return err
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("registration custody directory is a reparse point")
	}
	return nil
}

// selfChildOf duplicates the owner's custody handle, so the child store names
// the same directory object without a second pathname selection. A duplicate
// is used rather than a re-open of the directory by an empty object name
// because the duplicate cannot pick a different object on any NT version, and
// because the child takes its own lock file handle: closing this one releases
// nothing the owner holds.
//
// The duplicate shares the owner's FILE_OBJECT, including the directory
// enumeration cursor that names() continues after its first call. Unix gets an
// independent open file description from openat(".") instead. That is safe
// here because names() seeks to the start before every census and is only ever
// called on a store whose own handle is the one it enumerates.
func selfChildOf(parent *os.File) (*os.File, error) {
	process := windows.CurrentProcess()
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(process, windows.Handle(parent.Fd()), process, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(duplicate), parent.Name()), nil
}

// ntOpenObject opens one object through the parent's own RootDirectory. The
// attributes decide whether a reparse encounter is refused from this root on
// (directory descent) or opened as the object it is (leaf operations, which
// audit the attribute themselves).
// OBJ_CASE_INSENSITIVE means a leaf name aliases by case, unlike a
// case-sensitive unix filesystem. Only the owner can write the directory and an
// alias resolves to the same file, so the exact-string census over these names
// is unaffected.
func ntOpenObject(parent *os.File, name string, access uint32, disposition uint32, options uint32, attributes uint32, descriptor *windows.SECURITY_DESCRIPTOR) (*os.File, error) {
	w, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	oa := &windows.OBJECT_ATTRIBUTES{
		Length:             uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory:      windows.Handle(parent.Fd()),
		ObjectName:         w,
		Attributes:         attributes,
		SecurityDescriptor: descriptor,
	}
	var iosb windows.IO_STATUS_BLOCK
	var handle windows.Handle
	status := windows.NtCreateFile(&handle, access, oa, &iosb, nil, windows.FILE_ATTRIBUTE_NORMAL, custodyShareAccess, disposition, options|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if status != nil {
		if nt, ok := status.(windows.NTStatus); ok {
			return nil, nt.Errno()
		}
		return nil, status
	}
	return os.NewFile(uintptr(handle), name), nil
}

// ntOpenRelative opens one leaf through the custody handle, keeping the leaf
// itself rather than what it points at, so a symlink leaf is removed as a link
// and a substituted one is refused by the caller's attribute audit.
func ntOpenRelative(parent *os.File, name string, access uint32, disposition uint32, options uint32) (*os.File, error) {
	return ntOpenObject(parent, name, access, disposition, options|windows.FILE_OPEN_REPARSE_POINT, windows.OBJ_CASE_INSENSITIVE, nil)
}

// ntOpenRelativeDescriptor is the creating form: a leaf this store creates
// carries the private descriptor, so it inherits nothing from its directory.
func ntOpenRelativeDescriptor(parent *os.File, name string, access uint32, disposition uint32, descriptor *windows.SECURITY_DESCRIPTOR) (*os.File, error) {
	return ntOpenObject(parent, name, access, disposition, windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT, windows.OBJ_CASE_INSENSITIVE, descriptor)
}

// ntOpenDirectoryRelative descends one directory component and refuses to
// traverse a reparse point. OBJ_DONT_REPARSE needs Windows 10 1709 or later; an
// older host fails the open, which refuses custody rather than following a
// junction.
func ntOpenDirectoryRelative(parent *os.File, name string, access uint32, disposition uint32, options uint32) (*os.File, error) {
	return ntOpenObject(parent, name, access, disposition, options, windows.OBJ_CASE_INSENSITIVE|windows.OBJ_DONT_REPARSE, nil)
}

func openAttributes(handle windows.Handle, attributes *uint32) error {
	var basic fileBasicInfoFields
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))); err != nil {
		return err
	}
	*attributes = basic.FileAttributes
	return nil
}

// fileBasicInfoFields is the buffer the FileBasicInfo class takes. The Win32
// FILE_BASIC_INFO it is named for is 36 bytes, but kernel32 forwards the class
// to NtQueryInformationFile and NtSetInformationFile as FileBasicInformation,
// whose NT layout carries a trailing reserved word: a 36-byte buffer is
// refused with ERROR_BAD_LENGTH on a current host, measured, while 40 is
// accepted. The reserved word is therefore carried explicitly rather than left
// to the Go struct's alignment padding, which pads differently on 386.
type fileBasicInfoFields struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
	Reserved       uint32
}

// lastWriteNanos returns the file's own last-write time, the Windows parity of
// the Unix fingerprint's modification timestamp.
func lastWriteNanos(handle windows.Handle) (int64, error) {
	var basic fileBasicInfoFields
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))); err != nil {
		return 0, err
	}
	filetime := windows.Filetime{
		LowDateTime:  uint32(basic.LastWriteTime),
		HighDateTime: uint32(basic.LastWriteTime >> 32),
	}
	return filetime.Nanoseconds(), nil
}

// setLastWrite sets a leaf's last-write time through an open handle, which an
// explicit set makes final: a file system updates the timestamp at cleanup for
// a file it saw modified, and it leaves a timestamp the caller set alone.
func setLastWrite(handle windows.Handle, nanos int64) error {
	filetime := windows.NsecToFiletime(nanos)
	basic := fileBasicInfoFields{
		LastWriteTime: int64(filetime.HighDateTime)<<32 | int64(filetime.LowDateTime),
	}
	return windows.SetFileInformationByHandle(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic)))
}

// lastWriteOf answers the last-write time of a closed leaf through a fresh
// handle, which is the value every later reader of the file sees.
func (self *registrationStore) lastWriteOf(name string) (int64, error) {
	file, err := self.open(name, leafOpenRead)
	if err != nil {
		return 0, err
	}
	writeTime, statErr := lastWriteNanos(windows.Handle(file.Fd()))
	return writeTime, errors.Join(statErr, file.Close())
}

// markDeleteDisposition marks an open leaf's handle for deletion; its close
// completes the removal without walking a pathname. The extended class is
// asked first with POSIX semantics: the name leaves the directory at once, so
// a maintenance handle that shares delete cannot keep it visible as
// STATUS_DELETE_PENDING while every later open answers access-denied instead
// of not-found. Unix unlinkat has the same immediate semantics. FAT and exFAT,
// which answer the extended class with an invalid-parameter error, and hosts
// before Windows 10 1709 fall back to the legacy delete-on-close class.
func markDeleteDisposition(file *os.File) error {
	handle := windows.Handle(file.Fd())
	// the class is the Win32 FILE_INFO_BY_HANDLE_CLASS value that
	// SetFileInformationByHandle takes, not the NT FILE_INFORMATION_CLASS value
	// NtSetInformationFile takes
	var extended struct{ Flags uint32 }
	// the ignore-read-only bit is what makes removal match unix unlinkat, which
	// does not consult the file's own mode: without it an operator's attrib +r,
	// a tool copying a 0444 mode, or a foreign holder of FILE_WRITE_ATTRIBUTES
	// wedges removal for good.
	extended.Flags = windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | dispositionIgnoreReadOnlyFlag
	err := windows.SetFileInformationByHandle(handle, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&extended)), uint32(unsafe.Sizeof(extended)))
	if err == nil {
		return nil
	}
	if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) && !errors.Is(err, windows.ERROR_NOT_SUPPORTED) &&
		!errors.Is(err, windows.ERROR_INVALID_FUNCTION) && !errors.Is(err, windows.ERROR_CALL_NOT_IMPLEMENTED) {
		return err
	}
	var legacy struct{ DeleteFile uint8 }
	legacy.DeleteFile = 1
	return windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo, (*byte)(unsafe.Pointer(&legacy)), uint32(unsafe.Sizeof(legacy)))
}

// fileRenameInfoEx is the FILE_RENAME_INFO layout of the extended class: the
// target name is relative to RootDirectory, and the flags carry the replace and
// open-target semantics the plain class has no room for.
type fileRenameInfoEx struct {
	flags          uint32
	rootDirectory  windows.Handle
	fileNameLength uint32
	fileName       [1]uint16
}

// fileRenameInfo is the same layout without the flags word, which is the class
// Windows before 10 1607 understands.
type fileRenameInfo struct {
	replaceIfExists uint8
	rootDirectory   windows.Handle
	fileNameLength  uint32
	fileName        [1]uint16
}

// NT information classes for the two rename forms. The extended class carries
// the replace and open-target semantics; the plain class is what Windows before
// 10 1607 understands.
const (
	ntFileRenameInformation   = 10
	ntFileRenameInformationEx = 65
)

// A publication replaces an existing leaf, and a reader that opened that leaf
// by pathname (ReadToken, readNetworkCredential) shares read and write but not
// delete, so the replace answers a sharing violation for the moment the reader
// holds the file. Unix rename replaces a held target without a retry; the
// Windows replace gets the same brief bound the open for delete has.
const (
	renameAttempts = 20
	renameInterval = 25 * time.Millisecond
)

// renameRelative renames an open leaf to a name relative to the custody
// handle, so the target is bound to that handle's directory object rather than
// to any pathname. The extended class is asked first; a file system that does
// not offer it gets the plain class, which is equally handle-rooted and only
// lacks the POSIX replace flags.
//
// The call goes through NtSetInformationFile rather than
// SetFileInformationByHandle because kernel32's FileRenameInfoEx path answers
// ERROR_INVALID_PARAMETER for this exact buffer on a current Windows host,
// where the NT class performs the same rename. Both are handle-rooted, so the
// publication stays descriptor-relative either way.
func renameRelative(leaf *os.File, parent *os.File, name string) error {
	units, err := windows.UTF16FromString(name)
	if err != nil {
		return err
	}
	nameBytes := (len(units) - 1) * 2
	// the two layouts differ only in their first word, so they put the name at
	// the same offset on every architecture: 20 on amd64 and arm64, 12 on 386.
	// It is taken from the layout rather than written down.
	header := int(unsafe.Offsetof(fileRenameInfoEx{}.fileName))
	buffer := make([]byte, header+nameBytes)
	info := (*fileRenameInfoEx)(unsafe.Pointer(&buffer[0]))
	// the ignore-read-only bit keeps a rename working over a leaf whose own mode
	// says read-only, which unix renameat does not consult either, so a copied
	// 0444 mode or an attrib +r cannot wedge publication
	info.flags = renameReplaceIfExistsFlag | renamePosixSemanticsFlag | renameIgnoreReadOnlyFlag
	info.rootDirectory = windows.Handle(parent.Fd())
	info.fileNameLength = uint32(nameBytes)
	copy(buffer[header:], unsafe.Slice((*byte)(unsafe.Pointer(&units[0])), len(units)*2))
	iosb := &windows.IO_STATUS_BLOCK{}
	var status error
	for attempt := 0; attempt < renameAttempts; attempt++ {
		status = renameRelativeOnce(leaf, parent, buffer, iosb)
		if status == nil {
			return nil
		}
		if !errors.Is(status, windows.STATUS_SHARING_VIOLATION) {
			break
		}
		// a final failed attempt has nobody left to wait for
		if attempt < renameAttempts-1 {
			time.Sleep(renameInterval)
		}
	}
	return ntStatusError(status)
}

// renameRelativeOnce is one publication attempt: the extended class, then the
// plain class for the file system or host that does not offer it. The flags
// word is rewritten on every attempt because the plain class reads it as
// ReplaceIfExists alone, and a retried attempt needs the extended flags again.
func renameRelativeOnce(leaf, parent *os.File, buffer []byte, iosb *windows.IO_STATUS_BLOCK) error {
	info := (*fileRenameInfoEx)(unsafe.Pointer(&buffer[0]))
	info.flags = renameReplaceIfExistsFlag | renamePosixSemanticsFlag | renameIgnoreReadOnlyFlag
	status := windows.NtSetInformationFile(windows.Handle(leaf.Fd()), iosb, &buffer[0], uint32(len(buffer)), ntFileRenameInformationEx)
	if status == nil {
		return nil
	}
	// the plain class is the FAT and exFAT path, and the path of a host older
	// than the extended class: such a file system answers with an invalid
	// parameter, with a not-supported status, or with an information class it
	// does not know.
	if !errors.Is(status, windows.STATUS_INVALID_PARAMETER) && !errors.Is(status, windows.STATUS_NOT_SUPPORTED) && !errors.Is(status, windows.STATUS_INVALID_INFO_CLASS) {
		return status
	}
	// the plain class reads the first word as ReplaceIfExists alone. The
	// ignore-read-only bit is deliberately not carried here: the class has no
	// room for it, and a file system old enough to answer with an unknown
	// information class does not need it.
	buffer[0], buffer[1], buffer[2], buffer[3] = 1, 0, 0, 0
	plain := (*fileRenameInfo)(unsafe.Pointer(&buffer[0]))
	plain.rootDirectory = windows.Handle(parent.Fd())
	plain.fileNameLength = info.fileNameLength
	return windows.NtSetInformationFile(windows.Handle(leaf.Fd()), iosb, &buffer[0], uint32(len(buffer)), ntFileRenameInformation)
}

// ntStatusError answers a rename failure as the Win32 errno every other
// failure in this file does, ntOpenObject's mapping included, so a caller and
// its log see ERROR_SHARING_VIOLATION rather than its NTSTATUS spelling. The
// raw status is what the attempt decision and the retry compare against, so
// only the value that leaves the function is mapped.
func ntStatusError(status error) error {
	if status == nil {
		return nil
	}
	if nt, ok := status.(windows.NTStatus); ok {
		return nt.Errno()
	}
	return status
}

// auditLeafCustody is the Windows parity of the Unix leaf stat bound: a
// private, single-linked, bounded regular file owned by the process account,
// readable and writable by nobody outside its custody allowlist.
func auditLeafCustody(file *os.File) error {
	handle := windows.Handle(file.Fd())
	var bh windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &bh); err != nil {
		return err
	}
	size := int64(bh.FileSizeHigh)<<32 | int64(bh.FileSizeLow)
	if bh.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || bh.NumberOfLinks != 1 || size > maximumRegistrationBytes || size < 0 {
		return errors.New("registration file has unsafe ownership, mode, links or size")
	}
	return auditPrivateOwnership(handle, false, func(*windows.SID, uint32) error {
		return errors.New("registration file has unsafe ownership, mode, links or size")
	})
}

// auditPrivateOwnership refuses an owner outside the custody accounts and any
// allowlist ACE granting the audited data access to a SID outside {owner
// account, SYSTEM, Administrators}. wholeDirectory audits writes and the add
// and delete-child rights, the Unix Mode&0022 bound; leaves audit reads,
// writes, delete and the two rights that confer further access, the 0600
// bound. The refusal names the account and right through its caller, which
// knows whether it can also offer a repair.
func auditPrivateOwnership(handle windows.Handle, wholeDirectory bool, refusal func(sid *windows.SID, mask uint32) error) error {
	self, err := custodySelfSid()
	if err != nil {
		return refusal(nil, 0)
	}
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !custodyOwnerAcceptable(owner, self) {
		return refusal(owner, 0)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	// A descriptor whose DACL pointer is nil carries a NULL DACL, which grants
	// every account full access: the Unix 0777 case, refused here too.
	if dacl == nil {
		return refusal(nil, 0)
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return err
		}
		if ace == nil {
			return refusal(nil, 0)
		}
		granted, err := custodyAceGrant(ace, wholeDirectory)
		if err != nil {
			return errors.Join(refusal(nil, 0), err)
		}
		if granted == 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if custodySidOutside(sid, self) {
			return refusal(sid, granted)
		}
	}
	return nil
}

// custodyAceGrant classifies one ACE by its own header before anything is read
// as a grant: only an allowed ACE confers access, and an ACE whose layout is not
// an allowed ACE's is never read as one.
//
// What one ACE grants is audited by what it confers, not by the object it is
// written on. An entry without INHERIT_ONLY_ACE confers its rights on this
// object, so it is audited with this object's mask: the directory mask for a
// directory, the leaf mask for a leaf. An entry that also carries
// OBJECT_INHERIT_ACE is copied to a plain-path file child, where it confers the
// leaf mask; one that carries CONTAINER_INHERIT_ACE is copied to a subdirectory,
// where it confers the directory mask plus, through that subdirectory's own
// inheritable entries, the leaf mask on the files below it. Every one of those
// is a way for an account outside the allowlist to reach a file the store's
// plain-path writers create, so each is reported as a grant and the caller
// refuses that account.
//
// The earlier reading audited a directory ACE only with the directory mask,
// which carries no FILE_READ_DATA: an adopted directory whose DACL held
// Users:(OI)(CI)(R) or Users:(OI)(CI)(IO)(GR) was accepted as custody, and every
// file a plain-path writer then created in it, client JWTs and session tokens
// included, was readable by every local user with no signal at all. On unix the
// same directory is harmless because such a child carries its own 0600 mode; on
// Windows an inherited ACE is the only thing that decides who may read it.
func custodyAceGrant(ace *windows.ACCESS_ALLOWED_ACE, wholeDirectory bool) (uint32, error) {
	var mask uint32
	switch ace.Header.AceType {
	case windows.ACCESS_ALLOWED_ACE_TYPE, aceAllowedCallback:
		mask = maskThroughGenerics(uint32(ace.Mask))
	case windows.ACCESS_DENIED_ACE_TYPE, aceDeniedCallback, aceDeniedObject, aceDeniedCallbackObject,
		aceSystemAudit, aceSystemAlarm, aceSystemAuditObject, aceSystemAlarmObject,
		aceSystemMandatoryLabel, aceSystemResourceAttr, aceSystemTrustLabel, aceSystemScopedPolicy, aceSystemAccessFilter:
		// a denial or a system entry confers nothing on a foreign account
		return 0, nil
	case aceAllowedObject, aceAllowedCallbackObject:
		return 0, errors.New("registration custody DACL has an object ACE it cannot audit")
	default:
		return 0, errors.New("registration custody DACL has an unknown ACE type")
	}
	if mask == 0 {
		return 0, nil
	}
	var audited uint32
	if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE == 0 {
		audited |= custodyAuditMask(wholeDirectory)
	}
	if wholeDirectory {
		if ace.Header.AceFlags&windows.OBJECT_INHERIT_ACE != 0 {
			// a plain-path file child takes this entry as its own
			audited |= custodyAuditMask(false)
		}
		if ace.Header.AceFlags&windows.CONTAINER_INHERIT_ACE != 0 {
			// a subdirectory takes it, and hands it on to the files below
			audited |= custodyAuditMask(true) | custodyAuditMask(false)
		}
	}
	return mask & audited, nil
}

// maskThroughGenerics maps the generic rights an explicit ACE may carry to the
// file rights the access check would grant from them.
func maskThroughGenerics(mask uint32) uint32 {
	if mask&windows.GENERIC_ALL != 0 {
		mask |= fileAllAccess
	}
	if mask&windows.GENERIC_READ != 0 {
		mask |= windows.FILE_GENERIC_READ
	}
	if mask&windows.GENERIC_WRITE != 0 {
		mask |= windows.FILE_GENERIC_WRITE
	}
	if mask&windows.GENERIC_EXECUTE != 0 {
		mask |= windows.FILE_GENERIC_EXECUTE
	}
	return mask
}

// custodyAuditMask selects the rights no foreign account may hold: data read
// and write, delete, and the rights that would let the holder grant themselves
// the rest or rewrite what a reader sees.
//
// A leaf's mask carries FILE_WRITE_ATTRIBUTES and FILE_WRITE_EA as well. The
// network token's lineage fingerprint, and the rejection marker keyed on it, is
// sha256(token + LastWriteTime), so an account holding only
// FILE_WRITE_ATTRIBUTES on a token could restamp that time and unblock a
// rejection marker, which is the safety feature that stops a revoked client
// being recreated. The same right also sets FILE_ATTRIBUTE_READONLY. On unix an
// arbitrary mtime needs ownership, so the mask matches that.
func custodyAuditMask(wholeDirectory bool) uint32 {
	conferring := uint32(windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.MAXIMUM_ALLOWED)
	if wholeDirectory {
		return uint32(windows.FILE_WRITE_DATA|windows.FILE_APPEND_DATA|fileDeleteChildAccess) | conferring
	}
	return uint32(windows.FILE_READ_DATA|windows.FILE_WRITE_DATA|windows.FILE_APPEND_DATA|
		windows.FILE_WRITE_ATTRIBUTES|windows.FILE_WRITE_EA) | conferring
}

// privateCustodyDescriptor builds the descriptor a leaf is created with: the
// process account as owner, and a protected DACL granting full control to that
// account, SYSTEM and Administrators only. Its ACEs carry no inheritance flags
// because a leaf has no children.
func privateCustodyDescriptor() ([]byte, error) {
	return privateCustodyDescriptorFor(false)
}

// privateCustodyDirectoryDescriptor is the descriptor a custody directory is
// created with: the same allowlist, but every ACE inheritable. A protected
// DACL with no inheritable entry propagates nothing, so a file created inside
// the directory by plain pathname would take the creating token's default DACL
// instead of custody's, which leaves it unwritable to a later reader or open to
// an account outside the allowlist. Directory ACEs carry
// OBJECT_INHERIT_ACE|CONTAINER_INHERIT_ACE for that reason; leaves keep none.
func privateCustodyDirectoryDescriptor() ([]byte, error) {
	return privateCustodyDescriptorFor(true)
}

func privateCustodyDescriptorFor(inheritable bool) ([]byte, error) {
	self, err := custodySelfSid()
	if err != nil {
		return nil, err
	}
	var flags uint8
	if inheritable {
		flags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	acl := privateCustodyACL([]*windows.SID{self, custodyAccounts.systemSid, custodyAccounts.administrators}, flags)
	owner := custodySidBytes(self)
	ownerOffset := uint32(20)
	daclOffset := ownerOffset + uint32(len(owner))
	raw := make([]byte, int(daclOffset)+len(acl))
	raw[0] = descriptorRevision
	binary.LittleEndian.PutUint16(raw[2:], uint16(windows.SE_SELF_RELATIVE|windows.SE_DACL_PRESENT|windows.SE_DACL_PROTECTED))
	binary.LittleEndian.PutUint32(raw[4:], ownerOffset)
	binary.LittleEndian.PutUint32(raw[16:], daclOffset)
	copy(raw[ownerOffset:], owner)
	copy(raw[daclOffset:], acl)
	return raw, nil
}

// privateCustodyACL builds one allowed-ACE ACL with full control for each
// account, in the order given, every ACE carrying the given inheritance flags.
func privateCustodyACL(accounts []*windows.SID, flags uint8) []byte {
	size := 8
	for _, account := range accounts {
		size += aclAlignment(len(custodySidBytes(account)) + 8)
	}
	acl := make([]byte, size)
	acl[0] = aclRevision
	binary.LittleEndian.PutUint16(acl[2:], uint16(size))
	binary.LittleEndian.PutUint16(acl[4:], uint16(len(accounts)))
	at := 8
	for _, account := range accounts {
		sid := custodySidBytes(account)
		entry := aclAlignment(len(sid) + 8)
		acl[at] = byte(windows.ACCESS_ALLOWED_ACE_TYPE)
		acl[at+1] = flags
		binary.LittleEndian.PutUint16(acl[at+2:], uint16(entry))
		binary.LittleEndian.PutUint32(acl[at+4:], fileAllAccess)
		copy(acl[at+8:], sid)
		at += entry
	}
	return acl
}

func aclAlignment(size int) int { return (size + 3) &^ 3 }

// custodySidBytes views a SID's own storage; a SID is one contiguous
// revision, authority and subauthority block of GetLengthSid bytes.
func custodySidBytes(sid *windows.SID) []byte {
	length := int(windows.GetLengthSid(sid))
	return (*[256]byte)(unsafe.Pointer(sid))[:length:length]
}

var custodyAccounts struct {
	once           sync.Once
	processSid     *windows.SID
	defaultOwner   *windows.SID
	systemSid      *windows.SID
	administrators *windows.SID
	// the two creator placeholders a volume root or ProgramData carries. They
	// are not accounts: on inheritance the kernel substitutes the creating
	// account's SID for CREATOR OWNER, and the owner of the object for OWNER
	// RIGHTS, so neither can name an account outside the allowlist. Unix has no
	// such entry, which is why they are accepted here rather than refused.
	creatorOwner       *windows.SID
	creatorOwnerRights *windows.SID
	ready              bool
}

// custodySelfSid resolves the process token's user account once; the token's
// default owner and the two known-system accounts resolve beside it as the
// custody allowlist. A failed resolution keeps custody refusing rather than
// loosening.
func custodySelfSid() (*windows.SID, error) {
	custodyAccounts.once.Do(loadCustodyAccounts)
	if !custodyAccounts.ready || custodyAccounts.processSid == nil {
		return nil, errors.New("registration account identity is unavailable")
	}
	return custodyAccounts.processSid, nil
}

func loadCustodyAccounts() {
	token := windows.GetCurrentProcessToken()
	if user, err := token.GetTokenUser(); err == nil && user.User.Sid != nil {
		custodyAccounts.processSid = user.User.Sid
	}
	custodyAccounts.defaultOwner = tokenDefaultOwner(token)
	custodyAccounts.systemSid, _ = windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	custodyAccounts.administrators, _ = windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	custodyAccounts.creatorOwner, _ = windows.CreateWellKnownSid(windows.WinCreatorOwnerSid)
	custodyAccounts.creatorOwnerRights, _ = windows.CreateWellKnownSid(windows.WinCreatorOwnerRightsSid)
	custodyAccounts.ready = custodyAccounts.processSid != nil && custodyAccounts.systemSid != nil && custodyAccounts.administrators != nil
}

// custodyTokenOwner is the TOKEN_OWNER layout: one SID pointer into the query
// buffer.
type custodyTokenOwner struct {
	Owner *windows.SID
}

// tokenDefaultOwner answers the token's default owner, which is who owns what
// an elevated administrator's process creates when no owner is named. The
// answer is a heap copy: the queried TOKEN_OWNER holds a pointer into the query
// buffer itself, so answering with that pointer would leave custody comparing
// every later owner and ACE against a frame that has already been reused.
func tokenDefaultOwner(token windows.Token) *windows.SID {
	// the required length comes from a probe call, so a host whose default
	// owner SID is longer than any fixed buffer still answers it rather than
	// silently losing default-owner adoption
	var needed uint32
	_ = windows.GetTokenInformation(token, uint32(windows.TokenOwner), nil, 0, &needed)
	if needed < uint32(unsafe.Sizeof(custodyTokenOwner{})) {
		return nil
	}
	buffer := make([]byte, needed)
	if err := windows.GetTokenInformation(token, uint32(windows.TokenOwner), &buffer[0], uint32(len(buffer)), &needed); err != nil {
		return nil
	}
	owner := (*custodyTokenOwner)(unsafe.Pointer(&buffer[0])).Owner
	if owner == nil || !owner.IsValid() {
		return nil
	}
	copied, err := owner.Copy()
	if err != nil || copied == nil || !copied.IsValid() {
		return nil
	}
	return copied
}

// custodyOwnerAcceptable accepts the two owners a private custody object can
// legitimately carry: the process account itself, and the token's default
// owner. Any other owner is refused, so a directory planted by another account
// never becomes custody.
func custodyOwnerAcceptable(owner *windows.SID, self *windows.SID) bool {
	if windows.EqualSid(owner, self) {
		return true
	}
	return custodyAccounts.defaultOwner != nil && windows.EqualSid(owner, custodyAccounts.defaultOwner)
}

// custodySidOutside reports whether an ACE's SID is nobody's account this
// custody lets in: only the owner account, SYSTEM, Administrators, and the two
// creator placeholders, which stand for the creating account rather than for an
// account of their own.
func custodySidOutside(sid *windows.SID, self *windows.SID) bool {
	if windows.EqualSid(sid, self) {
		return false
	}
	if custodyAccounts.defaultOwner != nil && windows.EqualSid(sid, custodyAccounts.defaultOwner) {
		return false
	}
	if custodyAccounts.systemSid != nil && windows.EqualSid(sid, custodyAccounts.systemSid) {
		return false
	}
	if custodyAccounts.administrators != nil && windows.EqualSid(sid, custodyAccounts.administrators) {
		return false
	}
	if custodyAccounts.creatorOwner != nil && windows.EqualSid(sid, custodyAccounts.creatorOwner) {
		return false
	}
	if custodyAccounts.creatorOwnerRights != nil && windows.EqualSid(sid, custodyAccounts.creatorOwnerRights) {
		return false
	}
	return true
}

//go:build windows

package wincustody

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrDirectoryNotPrivate is a directory whose owner is not trusted or that
// another account can change (add, delete or rename entries, or change its
// attributes, DACL or owner).
var ErrDirectoryNotPrivate = errors.New("directory is not owned and private to this user")

// ErrFileNotPrivate is a leaf that is not a regular file with one link and a
// bounded size, or whose owner is not trusted, or that grants another account
// any access.
var ErrFileNotPrivate = errors.New("file is not a private regular file")

// ErrLocked is an exclusive lock another handle holds, in this process or
// another.
var ErrLocked = errors.New("lock is held by another owner")

// Leaf open modes.
const (
	// an existing file, read only
	OpenRead = iota + 1
	// an existing file, read and write (a write handle can be flushed)
	OpenReadWrite
	// a file opened or created, read and write, as a lock file is
	OpenLock
	// a new file only (O_CREAT|O_EXCL), write
	OpenCreate
)

// Identity is a file's volume and file index, which a rename keeps and a
// replacement changes.
type Identity struct {
	Volume    uint32
	IndexHigh uint32
	IndexLow  uint32
}

// SplitPath accepts only a canonical local drive path: a UNC share or a \\?\
// form is refused, and each component must be a plain name the Win32 and NT
// namespaces agree on, so a path a caller later checks with os.Lstat names
// the directory that was opened.
func SplitPath(dir string) (string, []string, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return "", nil, errors.New("custody path is not canonical absolute")
	}
	volume := filepath.VolumeName(dir)
	if len(volume) != 2 || volume[1] != ':' || !('A' <= volume[0] && volume[0] <= 'Z' || 'a' <= volume[0] && volume[0] <= 'z') {
		return "", nil, errors.New("custody path must be on a local drive")
	}
	rest := strings.TrimPrefix(dir[len(volume):], `\`)
	if rest == "" {
		return volume, nil, nil
	}
	components := strings.Split(rest, `\`)
	for _, component := range components {
		if !ValidName(component) {
			return "", nil, fmt.Errorf("custody path component %q is not a plain name", component)
		}
	}
	return volume, components, nil
}

// ValidName is a plain leaf name: no separators, stream or device syntax, and
// nothing the Win32 layer would trim or alias (a trailing dot or space, a
// reserved device name).
func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return false
		}
	}
	device, _, _ := strings.Cut(strings.ToUpper(name), ".")
	device = strings.TrimRight(device, " ")
	switch device {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	if strings.HasPrefix(device, "COM") || strings.HasPrefix(device, "LPT") {
		switch device[3:] {
		case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
			return false
		}
	}
	return true
}

// OpenPath opens a directory one component at a time from its drive's root,
// each relative to the last and none a reparse point.
func OpenPath(dir string) (windows.Handle, error) {
	volume, components, err := SplitPath(dir)
	if err != nil {
		return windows.InvalidHandle, err
	}
	current, err := OpenDirectory(0, `\??\`+volume+`\`)
	if err != nil {
		return windows.InvalidHandle, err
	}
	for _, component := range components {
		next, openErr := OpenDirectory(current, component)
		closeErr := windows.CloseHandle(current)
		if openErr != nil {
			return windows.InvalidHandle, errors.Join(openErr, closeErr)
		}
		current = next
		if closeErr != nil {
			return windows.InvalidHandle, errors.Join(closeErr, windows.CloseHandle(current))
		}
	}
	return current, nil
}

// OpenDirectory opens one directory relative to parent (an NT path when
// parent is 0) without following a reparse point and refuses it if it is
// one, the way O_NOFOLLOW|O_DIRECTORY refuses a symlink. An empty name
// reopens parent itself. Sharing allows what Unix allows: the path may be
// renamed, which a caller's identity check sees.
func OpenDirectory(parent windows.Handle, name string) (windows.Handle, error) {
	if parent != 0 && name != "" && !ValidName(name) {
		return windows.InvalidHandle, fmt.Errorf("custody directory %q is not a plain name", name)
	}
	attributes, err := objectAttributes(parent, name, nil)
	if err != nil {
		return windows.InvalidHandle, err
	}
	var handle windows.Handle
	err = windows.NtCreateFile(
		&handle,
		windows.FILE_LIST_DIRECTORY|windows.FILE_TRAVERSE|windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL|windows.SYNCHRONIZE,
		attributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		return windows.InvalidHandle, ntError("open", name, err)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return windows.InvalidHandle, errors.Join(err, windows.CloseHandle(handle))
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return windows.InvalidHandle, errors.Join(errors.New("custody path contains a link or a non-directory"), windows.CloseHandle(handle))
	}
	return handle, nil
}

// MakeDirectory creates dir like mkdir -m 0700: its missing parents as
// Windows makes them, dir itself with the private descriptor, so it never
// takes a wider DACL from where it is placed. An existing directory is left
// as it is; opening it checks it.
func MakeDirectory(dir string) error {
	if _, _, err := SplitPath(dir); err != nil {
		return err
	}
	if info, err := os.Lstat(dir); err == nil {
		if !info.IsDir() {
			return &os.PathError{Op: "mkdir", Path: dir, Err: errors.New("not a directory")}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return err
	}
	sd, err := PrivateDescriptor(true)
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if err := windows.CreateDirectory(name, &attributes); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return &os.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	return nil
}

// MakeDirectoryAt creates a private directory relative to parent, like
// mkdirat(parent, name, 0700). An existing entry answers os.ErrExist.
func MakeDirectoryAt(parent windows.Handle, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("custody directory %q is not a plain name", name)
	}
	sd, err := PrivateDescriptor(true)
	if err != nil {
		return err
	}
	attributes, err := objectAttributes(parent, name, sd)
	if err != nil {
		return err
	}
	var handle windows.Handle
	err = windows.NtCreateFile(
		&handle,
		windows.FILE_LIST_DIRECTORY|windows.SYNCHRONIZE,
		attributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_CREATE,
		windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		return ntError("mkdir", name, err)
	}
	return windows.CloseHandle(handle)
}

func objectAttributes(root windows.Handle, name string, sd *windows.SECURITY_DESCRIPTOR) (*windows.OBJECT_ATTRIBUTES, error) {
	unicode, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	return &windows.OBJECT_ATTRIBUTES{
		Length:             uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory:      root,
		ObjectName:         unicode,
		Attributes:         windows.OBJ_CASE_INSENSITIVE,
		SecurityDescriptor: sd,
	}, nil
}

// ntError maps an NTSTATUS to the Win32 error os and errors.Is understand
// (STATUS_OBJECT_NAME_NOT_FOUND is os.ErrNotExist, a collision os.ErrExist).
func ntError(op string, name string, err error) error {
	if status, ok := err.(windows.NTStatus); ok {
		err = status.Errno()
	}
	return &os.PathError{Op: op, Path: name, Err: err}
}

// HandleIdentity is the open file's volume and file index.
func HandleIdentity(handle windows.Handle) (Identity, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return Identity{}, err
	}
	return Identity{Volume: info.VolumeSerialNumber, IndexHigh: info.FileIndexHigh, IndexLow: info.FileIndexLow}, nil
}

// Rights another account may hold on a private directory: the read and
// traverse rights the Unix rule (mode&022 == 0) leaves to others.
const directoryReadRights = windows.FILE_LIST_DIRECTORY | windows.FILE_READ_EA | windows.FILE_TRAVERSE | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE | windows.GENERIC_READ | windows.GENERIC_EXECUTE

// Privacy reads the owner and DACL of an open handle and refuses them unless
// DescriptorPrivate accepts them; strict holds a directory to the file rule
// (others get nothing, the Unix mode 0700). It returns the descriptor's SDDL,
// which a caller compares to notice a change while it holds custody.
func Privacy(handle windows.Handle, directory bool, strict bool) (string, error) {
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	if err := DescriptorPrivate(sd, directory && !strict); err != nil {
		if directory {
			return "", ErrDirectoryNotPrivate
		}
		return "", err
	}
	return sd.String(), nil
}

// Descriptor is the owner and DACL of an open handle in SDDL.
func Descriptor(handle windows.Handle) (string, error) {
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	return sd.String(), nil
}

// DescriptorPrivate accepts a trusted owner and a DACL in which no other
// account is granted more than a directory's read rights (readable) or
// anything at all, and no other account is granted anything files created
// inside would inherit.
func DescriptorPrivate(sd *windows.SECURITY_DESCRIPTOR, readable bool) error {
	refused := ErrFileNotPrivate
	if readable {
		refused = ErrDirectoryNotPrivate
	}
	trusted, err := trustedSids()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !sidTrusted(owner, trusted) {
		return errors.Join(refused, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		// no DACL grants everyone everything
		return errors.Join(refused, err)
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return err
		}
		switch ace.Header.AceType {
		case accessDeniedAce, accessDeniedObjectAce, accessDeniedCallbackAce, accessDeniedCallbackObjectAce:
			// a deny entry only takes access away
			continue
		case accessAllowedAce, accessAllowedCallbackAce:
		default:
			// object and unknown allow layouts are not read here: refuse
			return refused
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sidTrusted(sid, trusted) || sid.Equals(ownerRightsSid) {
			continue
		}
		mask := uint32(ace.Mask)
		if mask == 0 {
			continue
		}
		if ace.Header.AceFlags&windows.OBJECT_INHERIT_ACE != 0 {
			// a file created inside by path (os.CreateTemp) would inherit
			// it, and Unix files are private whatever their directory says
			return refused
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			// reaches only subdirectories, which are each checked themselves
			continue
		}
		if readable && mask&^uint32(directoryReadRights) == 0 {
			continue
		}
		return refused
	}
	return nil
}

// The ACE layouts DescriptorPrivate reads.
const (
	accessAllowedAce              = 0x0
	accessDeniedAce               = 0x1
	accessDeniedObjectAce         = 0x6
	accessAllowedCallbackAce      = 0x9
	accessDeniedCallbackAce       = 0xa
	accessDeniedCallbackObjectAce = 0xc
)

// Owner Rights (S-1-3-4) applies to the object's owner, already trusted.
var ownerRightsSid = mustSid("S-1-3-4")

// This user, SYSTEM and Administrators: the accounts the Unix stores' owner
// and root stand for. An elevated process names Administrators as the owner
// of what it creates, so a trusted owner is any of them.
func trustedSids() ([]*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}
	return []*windows.SID{user.User.Sid, system, administrators}, nil
}

func sidTrusted(sid *windows.SID, trusted []*windows.SID) bool {
	for _, each := range trusted {
		if sid.Equals(each) {
			return true
		}
	}
	return false
}

func mustSid(value string) *windows.SID {
	sid, err := windows.StringToSid(value)
	if err != nil {
		panic(err)
	}
	return sid
}

// PrivateDescriptor is the descriptor new custody is created with: owned by
// this user, a protected DACL (nothing inherited) granting only this user,
// SYSTEM and Administrators. A directory's entries pass the same grant to
// what is created inside it.
func PrivateDescriptor(directory bool) (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	inherit := ""
	if directory {
		inherit = "OICI"
	}
	return windows.SecurityDescriptorFromString(fmt.Sprintf("O:%sD:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)(A;%s;FA;;;BA)", sid, inherit, sid, inherit, inherit))
}

// OpenLeaf opens name relative to dir without following a reparse point and
// then requires the Unix leaf rule of the handle: a regular file with one
// link, a size within maximum, a trusted owner and no access for anyone else.
// A created file gets the private descriptor. Sharing allows what Unix
// allows (another handle may rename or replace the name).
func OpenLeaf(dir windows.Handle, path string, name string, mode int, maximum int64) (*os.File, error) {
	if !ValidName(name) {
		return nil, errors.New("custody leaf is invalid")
	}
	var access, disposition uint32
	switch mode {
	case OpenRead:
		access, disposition = windows.FILE_GENERIC_READ, windows.FILE_OPEN
	case OpenReadWrite:
		access, disposition = windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_OPEN
	case OpenLock:
		access, disposition = windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, windows.FILE_OPEN_IF
	case OpenCreate:
		access, disposition = windows.FILE_GENERIC_WRITE|windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, windows.FILE_CREATE
	default:
		return nil, errors.New("custody leaf open mode is invalid")
	}
	var sd *windows.SECURITY_DESCRIPTOR
	if disposition != windows.FILE_OPEN {
		var err error
		sd, err = PrivateDescriptor(false)
		if err != nil {
			return nil, err
		}
	}
	attributes, err := objectAttributes(dir, name, sd)
	if err != nil {
		return nil, err
	}
	var handle windows.Handle
	err = windows.NtCreateFile(
		&handle,
		access|windows.SYNCHRONIZE,
		attributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		disposition,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		return nil, ntError("open", filepath.Join(path, name), err)
	}
	file := os.NewFile(uintptr(handle), filepath.Join(path, name))
	if err := leafSafe(handle, maximum); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func leafSafe(handle windows.Handle, maximum int64) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	size := int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
	kind, err := windows.GetFileType(handle)
	if err != nil {
		return err
	}
	if kind != windows.FILE_TYPE_DISK || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || info.NumberOfLinks != 1 || size < 0 || size > maximum {
		return ErrFileNotPrivate
	}
	_, err = Privacy(handle, false, false)
	return err
}

// Exists reports whether name is present relative to dir, as fstatat with
// AT_SYMLINK_NOFOLLOW would: a link counts as present.
func Exists(dir windows.Handle, name string) (bool, error) {
	if !ValidName(name) {
		return false, errors.New("custody leaf is invalid")
	}
	attributes, err := objectAttributes(dir, name, nil)
	if err != nil {
		return false, err
	}
	var handle windows.Handle
	err = windows.NtCreateFile(
		&handle,
		windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		attributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if err != nil {
		err = ntError("stat", name, err)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, windows.CloseHandle(handle)
}

// Lock takes a nonblocking exclusive LockFileEx lock on the file; another
// holder is ErrLocked. The lock ends when the handle closes.
func Lock(file *os.File) error {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return ErrLocked
	}
	return err
}

// Rename replaces to with from inside dir (renameat) and flushes the renamed
// file, which is how Windows makes a rename durable. Only the open and the
// rename itself are retried on a sharing conflict, each only while it has
// not happened; a step that ran is never repeated.
func Rename(dir windows.Handle, path string, from string, to string) error {
	if !ValidName(to) {
		return errors.New("custody rename leaf is invalid")
	}
	handle, err := openForRename(dir, path, from)
	if err != nil {
		return err
	}
	// a failed rename changed nothing, so it alone may be tried again while
	// a reader without delete sharing holds the replaced file
	renameErr := retrySharing(func() error { return renameHandle(dir, path, handle, to, true) })
	if renameErr == nil {
		renameErr = flushHandle(handle)
	}
	return errors.Join(renameErr, windows.CloseHandle(handle))
}

// Remove deletes name inside dir (unlinkat) with its name durably gone first:
// it is renamed to aside, a fresh name the caller chose, with a flush, and the
// leftover is then deleted and the deletion flushed through flush, a write
// handle on the same volume (the caller's lock file). A missing name is
// already absent (nil).
func Remove(dir windows.Handle, path string, name string, aside string, flush *os.File) error {
	if !ValidName(aside) {
		return errors.New("custody removal leaf is invalid")
	}
	handle, err := openForRename(dir, path, name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	renameErr := renameHandle(dir, path, handle, aside, false)
	if renameErr == nil {
		renameErr = flushHandle(handle)
	}
	if renameErr != nil {
		return errors.Join(renameErr, windows.CloseHandle(handle))
	}
	if err := errors.Join(deleteHandle(handle), windows.CloseHandle(handle)); err != nil {
		return err
	}
	return flushFile(flush)
}

// Delete deletes name inside dir and flushes the deletion through flush, as
// Remove does, without the rename aside; for a temporary that never held a
// published name. A missing name is os.ErrNotExist.
func Delete(dir windows.Handle, path string, name string, flush *os.File) error {
	handle, err := openForRename(dir, path, name)
	if err != nil {
		return err
	}
	if err := errors.Join(deleteHandle(handle), windows.CloseHandle(handle)); err != nil {
		return err
	}
	return flushFile(flush)
}

// flushHandle commits a file the caller holds without write access: a write
// handle is reopened from it (its attributes cleared of read-only first, as
// a Unix rename never minds a file's mode) and flushed.
func flushHandle(handle windows.Handle) error {
	var writer windows.Handle
	// a reader without write sharing (a scanner, a backup) only delays it
	err := retrySharing(func() error {
		var err error
		writer, err = reopenForFlush(handle)
		return err
	})
	if err != nil {
		return err
	}
	return errors.Join(windows.FlushFileBuffers(writer), windows.CloseHandle(writer))
}

func reopenForFlush(handle windows.Handle) (windows.Handle, error) {
	writer, err := reOpenFile(handle, windows.FILE_WRITE_DATA|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		if clearErr := clearReadOnly(handle); clearErr != nil {
			return windows.InvalidHandle, errors.Join(err, clearErr)
		}
		writer, err = reOpenFile(handle, windows.FILE_WRITE_DATA|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_FLAG_OPEN_REPARSE_POINT)
	}
	return writer, err
}

// fileBasicInfo is FILE_BASIC_INFO.
type fileBasicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
	_              uint32
}

var procReOpenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

// reOpenFile is kernel32's ReOpenFile: a new handle to the same file, never
// a fresh path lookup.
func reOpenFile(handle windows.Handle, access uint32, share uint32, flags uint32) (windows.Handle, error) {
	reopened, _, err := procReOpenFile.Call(uintptr(handle), uintptr(access), uintptr(share), uintptr(flags))
	if windows.Handle(reopened) == windows.InvalidHandle {
		return windows.InvalidHandle, err
	}
	return windows.Handle(reopened), nil
}

func clearReadOnly(handle windows.Handle) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_READONLY == 0 {
		return windows.ERROR_ACCESS_DENIED
	}
	// zero times leave the file's times as they are
	basic := fileBasicInfo{FileAttributes: info.FileAttributes &^ windows.FILE_ATTRIBUTE_READONLY}
	if basic.FileAttributes == 0 {
		basic.FileAttributes = windows.FILE_ATTRIBUTE_NORMAL
	}
	return windows.SetFileInformationByHandle(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic)))
}

// flushFile commits the volume's log through any write handle on it: NTFS
// writes its log in order, so a flush covers every earlier metadata change.
func flushFile(file *os.File) error {
	if file == nil {
		return errors.New("custody removal needs a write handle to flush")
	}
	return windows.FlushFileBuffers(windows.Handle(file.Fd()))
}

// Another process reading a file without FILE_SHARE_DELETE (os.Open, a virus
// scanner, an indexer) makes a rename onto or of it fail for that moment. The
// rename either happened or not, so it is retried briefly, then the error is
// the caller's.
var SharingWait = 2 * time.Second

func retrySharing(operation func() error) error {
	deadline := time.Now().Add(SharingWait)
	poll := 5 * time.Millisecond
	for {
		err := operation()
		if err == nil || !sharingConflict(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(poll)
		poll = min(poll*2, 200*time.Millisecond)
	}
}

func sharingConflict(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// The handle a rename or removal acts on: the leaf itself, never a link's
// target. Only this open is retried on a sharing conflict.
func openForRename(dir windows.Handle, path string, name string) (windows.Handle, error) {
	if !ValidName(name) {
		return windows.InvalidHandle, errors.New("custody rename leaf is invalid")
	}
	attributes, err := objectAttributes(dir, name, nil)
	if err != nil {
		return windows.InvalidHandle, err
	}
	var handle windows.Handle
	err = retrySharing(func() error {
		err := windows.NtCreateFile(
			&handle,
			windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.FILE_WRITE_ATTRIBUTES|windows.SYNCHRONIZE,
			attributes,
			&windows.IO_STATUS_BLOCK{},
			nil,
			0,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			windows.FILE_OPEN,
			windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
			0,
			0,
		)
		if err != nil {
			return ntError("rename", filepath.Join(path, name), err)
		}
		return nil
	})
	if err != nil {
		return windows.InvalidHandle, err
	}
	return handle, nil
}

// fileRenameInformation is FILE_RENAME_INFORMATION(_EX) with room for a leaf.
type fileRenameInformation struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [windows.MAX_PATH]uint16
}

const (
	fileRenameInformationClass   = 10
	fileRenameInformationExClass = 65
)

// renameHandle renames an open leaf to a name relative to dir, with POSIX
// semantics where the volume has them.
func renameHandle(dir windows.Handle, path string, handle windows.Handle, to string, replace bool) error {
	name, err := windows.UTF16FromString(to)
	if err != nil {
		return err
	}
	if len(name) > windows.MAX_PATH {
		return errors.New("custody rename leaf is invalid")
	}
	info := fileRenameInformation{RootDirectory: dir, FileNameLength: uint32((len(name) - 1) * 2)}
	copy(info.FileName[:], name)
	info.Flags = windows.FILE_RENAME_POSIX_SEMANTICS
	if replace {
		// a read-only file in the way doesn't stop a Unix rename either
		info.Flags |= windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_IGNORE_READONLY_ATTRIBUTE
	}
	err = windows.NtSetInformationFile(handle, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), fileRenameInformationExClass)
	if err == windows.STATUS_INVALID_PARAMETER && replace {
		// Windows 10 1607-1803 has POSIX renames but not the read-only flag
		info.Flags &^= windows.FILE_RENAME_IGNORE_READONLY_ATTRIBUTE
		err = windows.NtSetInformationFile(handle, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), fileRenameInformationExClass)
	}
	if err != nil {
		// before Windows 10 1607, a volume without POSIX renames (FAT), or a
		// filter or redirector without the extended class: the plain rename
		// acts on the same handle and relative name
		info.Flags = 0
		if replace {
			// the union's first byte is ReplaceIfExists
			info.Flags = 1
		}
		err = windows.NtSetInformationFile(handle, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), fileRenameInformationClass)
		if err == nil {
			return nil
		}
		return ntError("rename", filepath.Join(path, to), err)
	}
	return nil
}

type fileDispositionInformationEx struct {
	Flags uint32
}

type fileDispositionInformation struct {
	DeleteFile byte
}

const (
	fileDispositionInformationClass   = 13
	fileDispositionInformationExClass = 64
)

// deleteHandle marks an open leaf deleted, with POSIX semantics (the name
// goes when this handle closes) where the volume has them.
func deleteHandle(handle windows.Handle) error {
	info := fileDispositionInformationEx{Flags: windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE}
	err := windows.NtSetInformationFile(handle, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), fileDispositionInformationExClass)
	switch err {
	case nil:
		return nil
	case windows.STATUS_INVALID_INFO_CLASS, windows.STATUS_INVALID_PARAMETER, windows.STATUS_NOT_SUPPORTED:
		// the legacy disposition refuses a read-only file; Unix doesn't mind
		_ = clearReadOnly(handle)
		legacy := fileDispositionInformation{DeleteFile: 1}
		err = windows.NtSetInformationFile(handle, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&legacy)), uint32(unsafe.Sizeof(legacy)), fileDispositionInformationClass)
		if err == nil {
			return nil
		}
	}
	return ntError("remove", "", err)
}

// CheckPrivateDirectory applies the directory rule to an existing folder, for
// diagnostics: nil when custody would accept it.
func CheckPrivateDirectory(dir string) error {
	handle, err := OpenPath(dir)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	_, err = Privacy(handle, true, false)
	return err
}

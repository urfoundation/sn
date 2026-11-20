//go:build windows

package clientauth

// Real private directories and files under a per-test temporary directory
// prove the Windows custody contract against the Unix store's: the
// descriptor-relative component walk and its reparse refusal, the owner lock,
// the leaf and directory audits, and the durable publish and remove. The
// tokens and credentials here are synthetic.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// newRegistrationStorePath names a credential under a custody directory the
// store has to create for itself, so every test also exercises the walk.
func newRegistrationStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".urnetwork", "client.jwt")
}

func openTestStore(t *testing.T, path string) *registrationStore {
	t.Helper()
	store, err := openRegistrationStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return store
}

// The directory this store creates is private on its own account: it is owned
// by the process account and its DACL is protected, so nothing it inherits
// from where it lives can widen it. An administrator's process owns what it
// creates through the token's default owner, which the store accepts.
func TestRegistrationStoreCreatesPrivateDirectory(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	handle := windows.Handle(store.directory.Fd())
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	self, err := custodySelfSid()
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	if !custodyOwnerAcceptable(owner, self) {
		t.Fatal("custody directory is not owned by the process account")
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("custody directory DACL is not protected from inheritance")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount != 3 {
		t.Fatalf("custody directory DACL does not hold exactly its allowlist: %v", dacl)
	}
}

// The directory the store creates carries its allowlist on to its children. A
// protected DACL with no inheritable entry propagates nothing, so a file
// created inside the directory by plain pathname (session.go's WriteToken, the
// miner's writeFileAtomic siblings) would take the creating token's default
// DACL: a later reader can fail to open it, or it can land with a foreign
// default grant. The directory's ACEs are therefore inheritable, and a plain
// os.WriteFile child reads back through a store.
func TestRegistrationStoreChildrenInheritCustodyAccess(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)

	// every allow ACE on the directory itself is inheritable
	handle := windows.Handle(store.directory.Fd())
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount == 0 {
		t.Fatal("custody directory carries no DACL")
	}
	inherit := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			t.Fatal(err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("custody directory ACE %d is not an allowed ACE: %d", index, ace.Header.AceType)
		}
		if ace.Header.AceFlags&inherit != inherit {
			t.Fatalf("custody directory ACE %d of SID %s carries flags 0x%02x, not OBJECT_INHERIT|CONTAINER_INHERIT",
				index, (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String(), ace.Header.AceFlags)
		}
	}

	// a sibling created by plain pathname inherits that allowlist
	sibling := filepath.Join(store.path, "sibling.jwt")
	if err := os.WriteFile(sibling, []byte("synthetic-sibling"), 0600); err != nil {
		t.Fatal(err)
	}
	inherited, err := inheritedAceCount(t, sibling)
	if err != nil {
		t.Fatal(err)
	}
	if inherited == 0 {
		t.Fatal("a sibling created by plain pathname inherited no access entry from the custody directory")
	}

	// the same store and a second one both read it through the leaf audit
	raw, err := store.read("sibling.jwt")
	if err != nil || string(raw) != "synthetic-sibling" {
		t.Fatalf("the creating store cannot read a plain sibling: %q %v", raw, err)
	}
	second := openTestStore(t, filepath.Join(store.path, "second.jwt"))
	if raw, err := second.read("sibling.jwt"); err != nil || string(raw) != "synthetic-sibling" {
		t.Fatalf("a second store cannot read a plain sibling: %q %v", raw, err)
	}
}

// inheritedAceCount answers how many ACEs of one file were inherited from its
// parent rather than written on the file itself.
func inheritedAceCount(t *testing.T, path string) (int, error) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(handle)
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return 0, err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return 0, err
	}
	count := 0
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return 0, err
		}
		if ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			count++
		}
	}
	return count, nil
}

// A leaf name is one component: the NT object name space separates a stream of
// a leaf with a colon and a path with a backslash, and both are refused on
// every operation, not only on the one that writes.
func TestRegistrationStoreRefusesStreamAndPathLeafNames(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	for _, name := range []string{"leaf.jwt:hidden", `leaf\sub`, "leaf/sub"} {
		// the predicate itself, which every operation consults
		if !invalidLeaf(name) {
			t.Fatalf("%q is not refused as an invalid leaf", name)
		}
		if err := store.write(name, []byte("synthetic")); err == nil {
			t.Fatalf("write of %q was accepted", name)
		}
		if _, err := store.open(name, leafOpenRead); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("open of %q: %v, want an invalid leaf refusal", name, err)
		}
		if _, err := store.read(name); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("read of %q: %v, want an invalid leaf refusal", name, err)
		}
		if err := store.remove(name); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("remove of %q: %v, want an invalid leaf refusal", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(store.path, "leaf.jwt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused stream write left a file behind: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(store.path, "leaf")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused path write left a directory behind: %v", err)
	}
}

// An inherit-only grant of write to an account outside the allowlist confers
// nothing on the directory itself but everything on the files created inside
// it, which the Unix store never exposes: 0700 keeps a foreign account out of
// both. The audit refuses the entry and names the account.
func TestRegistrationStoreRefusesInheritedOnlyForeignWrite(t *testing.T) {
	path := newRegistrationStorePath(t)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	self, err := custodySelfSid()
	if err != nil {
		t.Fatal(err)
	}
	system := testWellKnownSid(t, windows.WinLocalSystemSid)
	administrators := testWellKnownSid(t, windows.WinBuiltinAdministratorsSid)
	users := testWellKnownSid(t, windows.WinBuiltinUsersSid)
	acl := custodyTestAcl(
		custodyTestAce{sid: self, mask: fileAllAccess},
		custodyTestAce{sid: system, mask: fileAllAccess},
		custodyTestAce{sid: administrators, mask: fileAllAccess},
		custodyTestAce{
			sid:   users,
			mask:  windows.FILE_GENERIC_WRITE,
			flags: windows.INHERIT_ONLY_ACE | windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE,
		},
	)
	setTestDirectorySecurity(t, dir, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, (*windows.ACL)(unsafe.Pointer(&acl[0])))
	_, err = openRegistrationStore(path)
	if err == nil {
		t.Fatal("adopted a directory that hands its children to a foreign account")
	}
	if !strings.Contains(err.Error(), "S-1-5-32-545") || !strings.Contains(err.Error(), "icacls") {
		t.Fatalf("the refusal does not name the account and the repair: %v", err)
	}
	// the repair it teaches must itself be inheritable, or following it
	// reproduces the same exposure for the next child created by pathname
	if !strings.Contains(err.Error(), "(OI)(CI)") {
		t.Fatalf("the refusal teaches a grant that is not inheritable: %v", err)
	}

	// the same inherit-only entry for an account inside the allowlist is not an
	// exposure: those accounts already hold full control of the directory
	root := t.TempDir()
	inside := filepath.Join(root, ".urnetwork")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	allowlisted := custodyTestAcl(
		custodyTestAce{sid: self, mask: fileAllAccess},
		custodyTestAce{sid: system, mask: fileAllAccess},
		custodyTestAce{sid: administrators, mask: fileAllAccess},
		custodyTestAce{
			sid:   administrators,
			mask:  windows.FILE_GENERIC_WRITE,
			flags: windows.INHERIT_ONLY_ACE | windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE,
		},
	)
	setTestDirectorySecurity(t, inside, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, (*windows.ACL)(unsafe.Pointer(&allowlisted[0])))
	adopted := openTestStore(t, filepath.Join(inside, "client.jwt"))
	if err := adopted.write("client.jwt", []byte("synthetic")); err != nil {
		t.Fatalf("an inherit-only ACE inside the allowlist refused custody: %v", err)
	}
}
func TestRegistrationStoreRoundTrip(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	if err := store.write("client.jwt", []byte("first")); err != nil {
		t.Fatal(err)
	}
	raw, err := store.read("client.jwt")
	if err != nil || string(raw) != "first" {
		t.Fatalf("read after write: %q %v", raw, err)
	}
	names, err := store.names(16)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(names, "client.jwt") {
		t.Fatalf("census lost the published leaf: %v", names)
	}
	// publication replaces the complete bytes of an existing leaf
	if err := store.write("client.jwt", []byte("second")); err != nil {
		t.Fatal(err)
	}
	if raw, err := store.read("client.jwt"); err != nil || string(raw) != "second" {
		t.Fatalf("read after replace: %q %v", raw, err)
	}
	if err := store.remove("client.jwt"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.read("client.jwt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed leaf still reads: %v", err)
	}
	// a missing leaf is already absent
	if err := store.remove("client.jwt"); err != nil {
		t.Fatalf("missing leaf removal: %v", err)
	}
	// the census is taken after the replace and the removal, not before them:
	// a snapshot from before the second write cannot see the temporary that
	// write would leak
	after, err := store.names(16)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range after {
		if strings.HasPrefix(name, ".registration-") {
			t.Fatalf("temporary leaf survived publication: %s", name)
		}
	}
}

// A publication that meets a leaf another handle holds open for a moment still
// completes: the rename is retried while the holder shares no delete, which is
// what Go's own path opens do, so a reader cannot make a write fail.
func TestRegistrationStoreReplaceWaitsForAContendedLeaf(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	if err := store.write("client.jwt", []byte("first")); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(filepath.Join(store.path, "client.jwt"))
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(150 * time.Millisecond)
		_ = held.Close()
	}()
	writeErr := store.write("client.jwt", []byte("second"))
	<-released
	if writeErr != nil {
		t.Fatalf("replace of a leaf a reader held briefly: %v", writeErr)
	}
	if raw, err := store.read("client.jwt"); err != nil || string(raw) != "second" {
		t.Fatalf("read after replace: %q %v", raw, err)
	}
}

// A holder that never closes leaves the publication refused, and the refusal is
// the Win32 sharing violation rather than the NTSTATUS spelling of it, as every
// other failure this store reports is.
func TestRegistrationStoreReplaceReportsAPersistentlyHeldLeaf(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	if err := store.write("client.jwt", []byte("first")); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(filepath.Join(store.path, "client.jwt"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := store.write("client.jwt", []byte("second")); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("replace of a leaf another handle holds: %v, want a sharing violation", err)
	}
	// a refused publication leaves the previous bytes and no temporary behind
	if raw, err := store.read("client.jwt"); err != nil || string(raw) != "first" {
		t.Fatalf("read after a refused replace: %q %v", raw, err)
	}
	names, err := store.names(16)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, ".registration-") {
			t.Fatalf("temporary leaf survived a refused replace: %s", name)
		}
	}
}

// A held owner lock is the one thing a second store meets: its own open shares
// read, write and delete, so contention surfaces as the typed busy owner rather
// than as a raw sharing violation.
func TestRegistrationStoreOwnerLockContention(t *testing.T) {
	path := newRegistrationStorePath(t)
	owner := openTestStore(t, path)
	if _, err := openRegistrationStore(path); !errors.Is(err, errRegistrationOwnerActive) {
		t.Fatalf("second owner acquired custody: %v", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	next, err := openRegistrationStore(path)
	if err != nil {
		t.Fatalf("custody stayed held after its owner closed: %v", err)
	}
	if err := next.close(); err != nil {
		t.Fatal(err)
	}
}

// The provider slot borrows the owner's directory rather than selecting a
// pathname again, and takes its own lock file so the owner keeps its own.
func TestRegistrationStoreProviderSlot(t *testing.T) {
	clientPath := newRegistrationStorePath(t)
	providerPath := filepath.Join(filepath.Dir(clientPath), "provider.jwt")
	owner := openTestStore(t, clientPath)
	child, err := openRegistrationStoreForOwner(providerPath, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.write("provider.jwt", []byte("provider")); err != nil {
		t.Fatal(err)
	}
	if raw, err := owner.read("provider.jwt"); err != nil || string(raw) != "provider" {
		t.Fatalf("owner cannot read the slot it lent: %q %v", raw, err)
	}
	if err := child.close(); err != nil {
		t.Fatal(err)
	}
	second, err := openRegistrationStoreForOwner(providerPath, owner)
	if err != nil {
		t.Fatalf("released slot stayed held: %v", err)
	}
	if err := second.close(); err != nil {
		t.Fatal(err)
	}
}

// Every component is opened through its parent's handle, so a junction or
// symlink anywhere on the path is refused instead of followed.
func TestRegistrationStoreRefusesReparseComponent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		// a junction needs no privilege where a symlink does
		if out, junctionErr := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); junctionErr != nil {
			t.Skipf("no reparse point available: %v %s", err, out)
		}
	}
	path := filepath.Join(link, "custody", "client.jwt")
	if _, err := openRegistrationStore(path); err == nil || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("walk entered a reparse component: %v", err)
	}
}

// A descriptor with no DACL grants every account full access, so custody
// refuses it exactly as it refuses a world-writable Unix directory.
func TestRegistrationStoreRefusesNullDacl(t *testing.T) {
	path := newRegistrationStorePath(t)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	setTestDirectorySecurity(t, dir, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil)
	if _, err := openRegistrationStore(path); err == nil || !strings.Contains(err.Error(), "icacls") {
		t.Fatalf("adopted a null DACL: %v", err)
	}
}

// One allow ACE for an account outside the custody allowlist is enough to
// refuse the whole directory.
func TestRegistrationStoreRefusesForeignDacl(t *testing.T) {
	path := newRegistrationStorePath(t)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	self, err := custodySelfSid()
	if err != nil {
		t.Fatal(err)
	}
	world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	acl := privateCustodyACL([]*windows.SID{self, world}, 0)
	setTestDirectorySecurity(t, dir, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, (*windows.ACL)(unsafe.Pointer(&acl[0])))
	if _, err := openRegistrationStore(path); err == nil || !strings.Contains(err.Error(), "icacls") {
		t.Fatalf("adopted a DACL that grants a foreign account: %v", err)
	}
}

// A symlink leaf is a leaf: reading it is refused, and removing it deletes the
// link itself, as unix unlinkat removes the link rather than its target.
func TestRegistrationStoreRefusesAndRemovesSymlinkLeaf(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	outside := filepath.Join(t.TempDir(), "outside.jwt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(store.path, "leaf.jwt")
	if err := os.Symlink(outside, leaf); err != nil {
		t.Skipf("no symlink available: %v", err)
	}
	if _, err := store.read("leaf.jwt"); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("read followed a symlink leaf: %v", err)
	}
	if err := store.remove("leaf.jwt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(leaf); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink leaf survived its removal: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("removal reached the symlink target: %v", err)
	}
}

// A publication that cannot complete still leaves the directory as it was: the
// temporary leaf is removed rather than leaked.
func TestRegistrationStoreRemovesTemporaryLeafWhenPublishFails(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	if err := os.Mkdir(filepath.Join(store.path, "blocked.jwt"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.write("blocked.jwt", []byte("x")); err == nil {
		t.Fatal("published over a directory")
	}
	names, err := store.names(64)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, ".registration-") {
			t.Fatalf("temporary leaf survived a failed publication: %s", name)
		}
	}
}

func setTestDirectorySecurity(t *testing.T, dir string, information windows.SECURITY_INFORMATION, acl *windows.ACL) {
	t.Helper()
	if err := trySetTestPathSecurity(dir, information, acl, nil); err != nil {
		t.Fatal(err)
	}
}

// seRestorePrivilege is the privilege that lets a process name an account
// outside its own as an object's owner.
const seRestorePrivilege = "SeRestorePrivilege"

// enableTestPrivilege enables one privilege on this process's token and
// answers the function that puts it back. An elevated runner holds
// SeRestorePrivilege but leaves it disabled, so the owner write a test plants
// is refused with access denied until the privilege is enabled here.
func enableTestPrivilege(name string) (func(), error) {
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(name), &luid); err != nil {
		return nil, fmt.Errorf("look up %s: %w", name, err)
	}
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return nil, fmt.Errorf("open this process's token: %w", err)
	}
	set := func(attributes uint32) error {
		state := windows.Tokenprivileges{
			PrivilegeCount: 1,
			Privileges:     [1]windows.LUIDAndAttributes{{Luid: luid, Attributes: attributes}},
		}
		return windows.AdjustTokenPrivileges(token, false, &state, uint32(unsafe.Sizeof(state)), nil, nil)
	}
	if err := set(windows.SE_PRIVILEGE_ENABLED); err != nil {
		windows.CloseHandle(windows.Handle(token))
		return nil, fmt.Errorf("enable %s: %w", name, err)
	}
	// AdjustTokenPrivileges answers success for a privilege the token does not
	// hold, so the state is read back rather than trusted: a developer's token
	// outside the administrators group holds no SeRestorePrivilege at all
	enabled, err := testPrivilegeEnabled(token, luid)
	if err != nil || !enabled {
		windows.CloseHandle(windows.Handle(token))
		if err == nil {
			err = errors.New("the token does not hold it")
		}
		return nil, fmt.Errorf("enable %s: %w", name, err)
	}
	return func() {
		_ = set(0)
		windows.CloseHandle(windows.Handle(token))
	}, nil
}

// testPrivilegeEnabled reads one privilege's state back from the token, so a
// privilege the token never held is not mistaken for an enabled one.
func testPrivilegeEnabled(token windows.Token, luid windows.LUID) (bool, error) {
	var needed uint32
	if err := windows.GetTokenInformation(token, windows.TokenPrivileges, nil, 0, &needed); err == nil || needed == 0 {
		return false, errors.New("the token answers no privilege list")
	}
	buffer := make([]byte, needed)
	if err := windows.GetTokenInformation(token, windows.TokenPrivileges, &buffer[0], needed, &needed); err != nil {
		return false, err
	}
	for _, entry := range (*windows.Tokenprivileges)(unsafe.Pointer(&buffer[0])).AllPrivileges() {
		if entry.Luid == luid {
			return entry.Attributes&windows.SE_PRIVILEGE_ENABLED != 0, nil
		}
	}
	return false, errors.New("the token does not hold it")
}

// trySetTestPathSecurity rewrites the descriptor of one file or directory, so a
// test can plant the inherited, permissive or foreign ACE that a real host
// carries.
func trySetTestPathSecurity(path string, information windows.SECURITY_INFORMATION, acl *windows.ACL, owner *windows.SID) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, information, owner, nil, acl, nil)
}

// custodyTestAce is one ACE a test plants: a SID, the rights it grants and its
// inheritance flags.
type custodyTestAce struct {
	sid   *windows.SID
	mask  uint32
	flags uint8
}

// custodyTestAcl builds an ACL of the given entries, so a test can write the
// exact descriptor a host's inherited or foreign access produces.
func custodyTestAcl(entries ...custodyTestAce) []byte {
	size := 8
	for _, entry := range entries {
		size += aclAlignment(len(custodySidBytes(entry.sid)) + 8)
	}
	acl := make([]byte, size)
	acl[0] = aclRevision
	binary.LittleEndian.PutUint16(acl[2:], uint16(size))
	binary.LittleEndian.PutUint16(acl[4:], uint16(len(entries)))
	at := 8
	for _, entry := range entries {
		sid := custodySidBytes(entry.sid)
		length := aclAlignment(len(sid) + 8)
		acl[at] = byte(windows.ACCESS_ALLOWED_ACE_TYPE)
		acl[at+1] = entry.flags
		binary.LittleEndian.PutUint16(acl[at+2:], uint16(length))
		binary.LittleEndian.PutUint32(acl[at+4:], entry.mask)
		copy(acl[at+8:], sid)
		at += length
	}
	return acl
}

func testWellKnownSid(t *testing.T, kind windows.WELL_KNOWN_SID_TYPE) *windows.SID {
	t.Helper()
	sid, err := windows.CreateWellKnownSid(kind)
	if err != nil {
		t.Fatal(err)
	}
	return sid
}

// A junction is refused on the component the walk opens whatever else the host
// allows. The earlier test tries a symlink first and an administrator's runner
// always succeeds at that, so this is the junction case on its own.
func TestRegistrationStoreRefusesJunctionComponent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "junction")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("no junction available: %v %s", err, out)
	}
	if _, err := openRegistrationStore(filepath.Join(link, "custody", "client.jwt")); err == nil || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("the walk entered a junction component: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "custody")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the walk wrote through the junction it refused: %v", err)
	}
}

// The custody directory itself as a junction is refused, never adopted: an
// adopted reparse point would place the private files wherever it points.
func TestRegistrationStoreRefusesReparseCustodyDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	custody := filepath.Join(root, ".urnetwork")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", custody, target).CombinedOutput(); err != nil {
		t.Skipf("no junction available: %v %s", err, out)
	}
	if _, err := openRegistrationStore(filepath.Join(custody, "client.jwt")); err == nil || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("a junction custody directory was adopted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "client.jwt.registration.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the store wrote through the junction it refused: %v", err)
	}
}

// A leaf the store created can still gain a foreign ACE later: the leaf audit
// refuses a read any account outside custody holds, the 0600 bound.
func TestRegistrationStoreRefusesLeafWithForeignRead(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	if err := store.write("client.jwt", []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	self, err := custodySelfSid()
	if err != nil {
		t.Fatal(err)
	}
	acl := custodyTestAcl(
		custodyTestAce{sid: self, mask: fileAllAccess},
		custodyTestAce{sid: testWellKnownSid(t, windows.WinWorldSid), mask: windows.FILE_GENERIC_READ},
	)
	if err := trySetTestPathSecurity(filepath.Join(store.path, "client.jwt"),
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, (*windows.ACL)(unsafe.Pointer(&acl[0])), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.read("client.jwt"); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("read a leaf a foreign account may read: %v", err)
	}
}

// The realistic adoption failure: .urnetwork already exists with an inheritable
// ACE for BUILTIN\Users from its parent, which a data drive, ProgramData or a
// domain profile grants. The refusal must name the account, the right and the
// command that repairs the directory.
func TestRegistrationStoreRefusesInheritedForeignAce(t *testing.T) {
	root := t.TempDir()
	self, err := custodySelfSid()
	if err != nil {
		t.Fatal(err)
	}
	// both ACEs are inheritable, so the child carries them as inherited: the
	// account keeps full control, and the foreign account holds write
	acl := custodyTestAcl(
		custodyTestAce{sid: self, mask: fileAllAccess, flags: windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE},
		custodyTestAce{
			sid:   testWellKnownSid(t, windows.WinBuiltinUsersSid),
			mask:  windows.FILE_GENERIC_WRITE,
			flags: windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE,
		},
	)
	if err := trySetTestPathSecurity(root, windows.DACL_SECURITY_INFORMATION, (*windows.ACL)(unsafe.Pointer(&acl[0])), nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".urnetwork")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = openRegistrationStore(filepath.Join(dir, "client.jwt"))
	if err == nil {
		t.Fatal("adopted a directory whose DACL grants a foreign account")
	}
	message := err.Error()
	if !strings.Contains(message, "S-1-5-32-545") || !strings.Contains(message, "write") || !strings.Contains(message, "icacls") {
		t.Fatalf("the refusal is not actionable: %v", err)
	}
}

// A leaf owned by an account outside custody is refused, whatever its DACL
// says. Naming another account as an owner needs SeRestorePrivilege: an
// elevated runner holds the privilege but leaves it disabled, so the test
// enables it for the write and disables it again. A token that holds no such
// privilege, which is every unelevated developer host, skips with that reason.
func TestRegistrationStoreRefusesLeafWithForeignOwner(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	if err := store.write("client.jwt", []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	restore, err := enableTestPrivilege(seRestorePrivilege)
	if err != nil {
		t.Skipf("changing an owner to LocalSystem needs SeRestorePrivilege, which this token cannot enable: %v", err)
	}
	defer restore()
	foreign := testWellKnownSid(t, windows.WinLocalSystemSid)
	if err := trySetTestPathSecurity(filepath.Join(store.path, "client.jwt"),
		windows.OWNER_SECURITY_INFORMATION, nil, foreign); err != nil {
		t.Skipf("changing an owner to LocalSystem needs an elevated token: %v", err)
	}
	if _, err := store.read("client.jwt"); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("read a leaf a foreign account owns: %v", err)
	}
}

// The owner lock excludes another process, not only another handle: a second
// process that opens the same custody answers errRegistrationOwnerActive.
func TestRegistrationStoreOwnerLockExcludesAnotherProcess(t *testing.T) {
	path := newRegistrationStorePath(t)
	owner := openTestStore(t, path)
	command := exec.Command(os.Args[0], "-test.run=^TestRegistrationStoreOwnerLockHelper$", "-test.timeout=2m")
	command.Env = append(os.Environ(), "URNETWORK_CUSTODY_LOCK_HELPER="+path)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("the second process did not meet the held owner lock: %v %s", err, out)
	}
	if !strings.Contains(string(out), "owner-lock-held") {
		t.Fatalf("the second process answered something else: %s", out)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	command = exec.Command(os.Args[0], "-test.run=^TestRegistrationStoreOwnerLockHelper$", "-test.timeout=2m")
	command.Env = append(os.Environ(), "URNETWORK_CUSTODY_LOCK_HELPER="+path, "URNETWORK_CUSTODY_LOCK_HELPER_OPEN=1")
	if out, err := command.CombinedOutput(); err != nil || !strings.Contains(string(out), "owner-lock-free") {
		t.Fatalf("the released lock did not free the custody: %v %s", err, out)
	}
}

// TestRegistrationStoreOwnerLockHelper is the second process of the lock test.
// It runs only when that test re-executes the binary with a path to open.
func TestRegistrationStoreOwnerLockHelper(t *testing.T) {
	path := os.Getenv("URNETWORK_CUSTODY_LOCK_HELPER")
	if path == "" {
		t.Skip("helper process of TestRegistrationStoreOwnerLockExcludesAnotherProcess")
	}
	_, err := openRegistrationStore(path)
	if os.Getenv("URNETWORK_CUSTODY_LOCK_HELPER_OPEN") != "" {
		if err != nil {
			t.Fatalf("the released custody did not open: %v", err)
		}
		fmt.Println("owner-lock-free")
		return
	}
	if !errors.Is(err, errRegistrationOwnerActive) {
		t.Fatalf("the second process did not meet the held owner lock: %v", err)
	}
	fmt.Println("owner-lock-held")
}

// A removal unlinks the name at once, as unix unlinkat does. Another handle
// that shares delete cannot keep the name visible as delete-pending, where a
// legacy delete-on-close removal makes the next open answer access-denied
// instead of not-found and every ErrNotExist branch takes the error path.
func TestRegistrationStoreRemoveLeavesNoPendingName(t *testing.T) {
	path := newRegistrationStorePath(t)
	store := openTestStore(t, path)
	if err := store.write("client.jwt", []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	held, err := store.open("client.jwt", leafOpenRead)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := store.remove("client.jwt"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.read("client.jwt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a removed name is still visible while another handle holds it: %v", err)
	}
	names, err := store.names(64)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(names, "client.jwt") {
		t.Fatalf("a removed name is still in the census: %v", names)
	}
}

// A renewal fingerprints the file exactly as every later reader does, so the
// renewed token stays in its own lineage. A fingerprint read before the close
// can describe a last-write time the file no longer carries, and the lineage
// then matches no reader: the marker stops blocking and a revoked client is
// unblocked again.
func TestRenewNetworkTokenFingerprintStaysInItsLineage(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".urnetwork", "jwt")
	if err := WriteNetworkToken(path, "first-sign-in"); err != nil {
		t.Fatal(err)
	}
	previous := "first-sign-in"
	for round := range 3 {
		renewed := fmt.Sprintf("renewed-sign-in-%d", round)
		if ok, err := RenewNetworkToken(path, previous, renewed); err != nil || !ok {
			t.Fatalf("renewal %d: ok=%t err=%v", round, ok, err)
		}
		token, fingerprint, err := readNetworkCredential(path)
		if err != nil {
			t.Fatal(err)
		}
		if token != renewed {
			t.Fatalf("renewal %d left %q", round, token)
		}
		raw, err := os.ReadFile(path + ".lineage")
		if err != nil {
			t.Fatal(err)
		}
		members, err := parseNetworkCredentialLineage(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(members, fingerprint) {
			t.Fatalf("renewal %d: the reader's fingerprint %.12s is in no lineage of %d members", round, fingerprint, len(members))
		}
		previous = renewed
	}
}

// defaultOwnerMarker is the pattern that overwrites a dead stack frame: nonzero
// and not a SID revision, so a SID read from a clobbered frame is never
// mistaken for a real one.
const defaultOwnerMarker = 0xa5

// clobberSink keeps the marker write of clobberDeadFrames observable, so the
// writes into each dead frame are not elided.
var clobberSink byte

// clobberDeadFrames overwrites the stack region a returned call used. Each
// frame holds a non-escaping array larger than the token query buffer plus that
// frame's own overhead, and the recursion grows the stack, so the region the
// first token query occupied is written with the marker. A pointer left into
// that dead frame then reads the marker instead of the SID, while a copy of the
// SID still answers the same.
//
//go:noinline
func clobberDeadFrames(depth int) {
	var frame [1024]byte
	for index := range frame {
		frame[index] = defaultOwnerMarker
	}
	if depth > 0 {
		clobberDeadFrames(depth - 1)
	}
	clobberSink = frame[depth%len(frame)] ^ byte(depth)
}

// The token's default owner is a copy of the SID, not a pointer into the query
// buffer's frame: the predicates answer the same after other work has reused
// that frame and the stack has moved.
func TestCustodyDefaultOwnerSurvivesOtherWork(t *testing.T) {
	if _, err := custodySelfSid(); err != nil {
		t.Fatal(err)
	}
	owner := custodyAccounts.defaultOwner
	if owner == nil {
		t.Skip("this host's token names no default owner")
	}
	if !owner.IsValid() {
		t.Fatalf("the default owner is not a valid SID: %q", owner.String())
	}
	want := owner.String()
	self, err := custodySelfSid()
	if err != nil {
		t.Fatal(err)
	}
	if !custodyOwnerAcceptable(owner, self) || custodySidOutside(owner, self) {
		t.Fatalf("the predicate disagrees about the default owner %s", want)
	}
	// a fresh query, then a recursion that reuses and overwrites the dead frame
	// with a marker: a pointer left into the query buffer now reads the marker,
	// and only a real copy still answers the SID
	fresh := tokenDefaultOwner(windows.GetCurrentProcessToken())
	if fresh == nil || fresh.String() != want {
		t.Fatalf("a second query answered %v, want %s", fresh, want)
	}
	clobberDeadFrames(32)
	again := custodyAccounts.defaultOwner
	if again.String() != want {
		t.Fatalf("the default owner changed to %s after other work, want %s", again.String(), want)
	}
	if !custodyOwnerAcceptable(again, self) {
		t.Fatal("the default owner stopped being acceptable after other work")
	}
	if custodySidOutside(again, self) {
		t.Fatalf("the default owner %s is outside its own allowlist", want)
	}
}

//go:build windows

package clientauth

// Deterministic tests of the Windows custody contract. Every property here is
// exercised against the real kernel object it depends on (a DACL the kernel
// propagates, a byte-range lock, a delete disposition, a rename over a held
// target), never against a mock, and no test waits on the wall clock or on
// scheduling: where two parties must interleave, one waits for the other's
// explicit signal. Each test names, in its comment, the exact regression it
// exists to catch.
//
// Every case here runs unconditionally. pinnedDefect is left in place as the
// switch for the next open finding: a case that pins one calls it and is
// skipped unless URNETWORK_CUSTODY_PIN_DEFECTS=1 is set, naming the finding in
// the skip message, so the suite can stay green while that finding is open.
// The three cases this file was written around (F1 twice, F2, F3) no longer
// gate: the audit and the two rename/disposition flags satisfy them.

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pinnedDefect skips a case that pins an open finding unless the operator asks
// for the defect pins explicitly. No case pins an open finding at present.
func pinnedDefect(t *testing.T, finding string) {
	t.Helper()
	if os.Getenv("URNETWORK_CUSTODY_PIN_DEFECTS") != "1" {
		t.Skipf("pins open finding %s, which production does not yet satisfy; set URNETWORK_CUSTODY_PIN_DEFECTS=1 to run it", finding)
	}
}

// detAce is one ACE a test plants, of any of the two simple layouts.
type detAce struct {
	kind  uint8
	flags uint8
	mask  uint32
	sid   *windows.SID
}

func detAllow(sid *windows.SID, mask uint32, flags uint8) detAce {
	return detAce{kind: windows.ACCESS_ALLOWED_ACE_TYPE, flags: flags, mask: mask, sid: sid}
}

func detDeny(sid *windows.SID, mask uint32, flags uint8) detAce {
	return detAce{kind: windows.ACCESS_DENIED_ACE_TYPE, flags: flags, mask: mask, sid: sid}
}

// detAcl lays out allowed and denied ACEs exactly as given, in order. Denied
// entries are placed wherever the caller puts them: the audit must classify by
// type, not by canonical position.
func detAcl(entries ...detAce) []byte {
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
		acl[at] = entry.kind
		acl[at+1] = entry.flags
		binary.LittleEndian.PutUint16(acl[at+2:], uint16(length))
		binary.LittleEndian.PutUint32(acl[at+4:], entry.mask)
		copy(acl[at+8:], sid)
		at += length
	}
	return acl
}

// detAceView is one ACE as the kernel stored it, with its SID copied out of
// the descriptor so it outlives the query.
type detAceView struct {
	kind  uint8
	flags uint8
	mask  uint32
	sid   *windows.SID
}

// detPathSecurity reads the owner, control bits and every DACL entry of one
// path through a handle that does not follow a reparse point.
func detPathSecurity(t *testing.T, path string) (*windows.SID, windows.SECURITY_DESCRIPTOR_CONTROL, []detAceView) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL, custodyShareAccess, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatalf("open %s for its descriptor: %v", path, err)
	}
	defer windows.CloseHandle(handle)
	return detHandleSecurity(t, handle)
}

func detHandleSecurity(t *testing.T, handle windows.Handle) (*windows.SID, windows.SECURITY_DESCRIPTOR_CONTROL, []detAceView) {
	t.Helper()
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	ownerCopy, err := owner.Copy()
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil {
		return ownerCopy, control, nil
	}
	views := make([]detAceView, 0, dacl.AceCount)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			t.Fatal(err)
		}
		sid, err := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).Copy()
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, detAceView{kind: ace.Header.AceType, flags: ace.Header.AceFlags, mask: uint32(ace.Mask), sid: sid})
	}
	return ownerCopy, control, views
}

// detAllowlist is the exact account set custody grants: the process account,
// SYSTEM and Administrators, named by explicit well-known SIDs.
func detAllowlist(t *testing.T) []*windows.SID {
	t.Helper()
	self, err := custodySelfSid()
	if err != nil {
		t.Fatal(err)
	}
	return []*windows.SID{self, testWellKnownSid(t, windows.WinLocalSystemSid), testWellKnownSid(t, windows.WinBuiltinAdministratorsSid)}
}

// detRequireExactAllowlist asserts that a DACL holds exactly one full-control
// allowed ACE per allowlisted account, each with exactly the given flags.
func detRequireExactAllowlist(t *testing.T, what string, aces []detAceView, allowlist []*windows.SID, flags uint8) {
	t.Helper()
	if len(aces) != len(allowlist) {
		t.Fatalf("%s carries %d ACEs, want exactly the %d allowlisted accounts: %+v", what, len(aces), len(allowlist), aces)
	}
	seen := make([]bool, len(allowlist))
	for index, ace := range aces {
		if ace.kind != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("%s ACE %d has type %d, want an allowed ACE", what, index, ace.kind)
		}
		if ace.flags != flags {
			t.Fatalf("%s ACE %d for %s carries flags 0x%02x, want exactly 0x%02x", what, index, ace.sid.String(), ace.flags, flags)
		}
		if ace.mask&fileAllAccess != fileAllAccess {
			t.Fatalf("%s ACE %d for %s grants 0x%08x, want full control", what, index, ace.sid.String(), ace.mask)
		}
		matched := false
		for slot, account := range allowlist {
			if !seen[slot] && windows.EqualSid(ace.sid, account) {
				seen[slot], matched = true, true
				break
			}
		}
		if !matched {
			t.Fatalf("%s ACE %d names %s, outside the allowlist or repeated", what, index, ace.sid.String())
		}
	}
}

// detAdoptedDirectory makes a custody directory the store did not create, so
// the store adopts it rather than creating it with its own descriptor, and
// plants the given DACL on it, protected from its parent.
func detAdoptedDirectory(t *testing.T, acl []byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".urnetwork")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var pointer *windows.ACL
	if acl != nil {
		pointer = (*windows.ACL)(unsafe.Pointer(&acl[0]))
	}
	if err := trySetTestPathSecurity(dir, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, pointer, nil); err != nil {
		t.Fatalf("plant the adopted directory's DACL: %v", err)
	}
	return dir
}

// detCensus answers the names in a directory by plain pathname, independent of
// the store under test.
func detCensus(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

func detRequireNoPrefix(t *testing.T, dir string, prefixes ...string) {
	t.Helper()
	for _, name := range detCensus(t, dir) {
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("leftover %s survived in %s", name, dir)
			}
		}
	}
}

// detHoldWithoutShareDelete opens a leaf the way a pathname reader does that
// shares read and write but not delete. The share mode is written down here
// rather than taken from os.Open, so the test does not depend on what the Go
// runtime chooses.
func detHoldWithoutShareDelete(t *testing.T, path string) windows.Handle {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("hold %s: %v", path, err)
	}
	return handle
}

// detReadHandle reads a whole small file through a handle from its start.
func detReadHandle(t *testing.T, handle windows.Handle) string {
	t.Helper()
	if _, err := windows.Seek(handle, 0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 256)
	var read uint32
	if err := windows.ReadFile(handle, buffer, &read, nil); err != nil {
		t.Fatal(err)
	}
	return string(buffer[:read])
}

// ---------------------------------------------------------------------------
// Directory descriptor and inheritance
// ---------------------------------------------------------------------------

// A directory the store creates hands exactly its allowlist to every child a
// plain-pathname writer creates inside it (session.go WriteToken, the miner's
// client JWT store, proxy files), at every depth, and to nothing else.
//
// Regression caught: the directory descriptor built without
// OBJECT_INHERIT_ACE|CONTAINER_INHERIT_ACE (privateCustodyDescriptorFor(false)
// for the directory), which leaves every plain child with the creating token's
// default DACL instead of custody's; also any extra, missing or
// inherit-only-flagged entry on the directory, and a directory DACL that is
// not protected. Demonstrated failing against the non-inheritable descriptor.
func TestCustodyDetCreatedDirectoryHandsExactAllowlistToPlainChildren(t *testing.T) {
	store := openTestStore(t, newRegistrationStorePath(t))
	allowlist := detAllowlist(t)
	self := allowlist[0]

	owner, control, aces := detHandleSecurity(t, windows.Handle(store.directory.Fd()))
	if !custodyOwnerAcceptable(owner, self) {
		t.Fatalf("the created directory is owned by %s", owner.String())
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("the created directory's DACL is not protected from its parent")
	}
	inherit := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
	detRequireExactAllowlist(t, "created directory", aces, allowlist, inherit)

	file := filepath.Join(store.path, "plain.jwt")
	if err := os.WriteFile(file, []byte("synthetic-plain"), 0600); err != nil {
		t.Fatal(err)
	}
	subdirectory := filepath.Join(store.path, "provider")
	if err := os.Mkdir(subdirectory, 0700); err != nil {
		t.Fatal(err)
	}
	grandchild := filepath.Join(subdirectory, "nested.json")
	if err := os.WriteFile(grandchild, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, child := range []struct {
		path  string
		flags uint8
	}{
		// a file has no children, so it keeps only the inherited marker
		{file, windows.INHERITED_ACE},
		// a directory keeps passing the same entries on
		{subdirectory, windows.INHERITED_ACE | inherit},
		{grandchild, windows.INHERITED_ACE},
	} {
		childOwner, childControl, childAces := detPathSecurity(t, child.path)
		if childControl&windows.SE_DACL_PROTECTED != 0 {
			t.Fatalf("%s is protected, so it inherited nothing", child.path)
		}
		if !custodyOwnerAcceptable(childOwner, self) {
			t.Fatalf("%s is owned by %s", child.path, childOwner.String())
		}
		detRequireExactAllowlist(t, child.path, childAces, allowlist, child.flags)
	}

	// and the plain child passes the store's own leaf audit
	if raw, err := store.read("plain.jwt"); err != nil || string(raw) != "synthetic-plain" {
		t.Fatalf("the store refuses a plain child of its own directory: %q %v", raw, err)
	}
}

// A store-created leaf carries its own protected descriptor with no
// inheritance flags, so it never depends on, or passes on, what its directory
// holds.
//
// Regression caught: a leaf created with the inheritable directory descriptor,
// or with no explicit descriptor at all (then it would inherit whatever an
// adopted directory carries). Reasoned; it is the leaf twin of the directory
// test above.
func TestCustodyDetStoreLeafCarriesItsOwnProtectedDescriptor(t *testing.T) {
	store := openTestStore(t, newRegistrationStorePath(t))
	if err := store.write("client.jwt", []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"client.jwt", "client.jwt.registration.lock"} {
		owner, control, aces := detPathSecurity(t, filepath.Join(store.path, name))
		if control&windows.SE_DACL_PROTECTED == 0 {
			t.Fatalf("%s is not protected from its directory", name)
		}
		if !custodyOwnerAcceptable(owner, detAllowlist(t)[0]) {
			t.Fatalf("%s is owned by %s", name, owner.String())
		}
		detRequireExactAllowlist(t, name, aces, detAllowlist(t), 0)
	}
}

// ---------------------------------------------------------------------------
// Adoption audit: DACL shapes on a directory the store did not create
// ---------------------------------------------------------------------------

// Every DACL shape an adopted directory can carry, against what custody must
// answer. The base grants the allowlist full control, inheritably, which is
// the icacls repair the refusal teaches; each case adds or replaces entries.
//
// Regressions caught, per case: an inherit-only grant skipped on a directory
// (custodyAceGrant's INHERIT_ONLY branch returning 0), generic rights not
// mapped (maskThroughGenerics), WRITE_DAC, WRITE_OWNER or DELETE dropped from
// the audited mask, a NULL DACL read as empty, an ACE carrying the inherited
// flag skipped, a denied entry read as a grant, an allowlisted inherit-only
// entry refused, and directory read access refused (the unix 0755 parity).
// Demonstrated failing with the inherit-only branch returning 0 (four
// cases) and with maskThroughGenerics disabled (the inherit-only generic-all
// case: the kernel maps generic bits to file rights when it stores an
// effective ACE, so only an inherit-only entry still carries them to the
// audit). The "foreign inheritable read" cases pin finding F1.
func TestCustodyDetAdoptedDirectoryDaclShapes(t *testing.T) {
	allowlist := detAllowlist(t)
	self, system, administrators := allowlist[0], allowlist[1], allowlist[2]
	users := testWellKnownSid(t, windows.WinBuiltinUsersSid)
	inherit := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
	inheritOnly := inherit | windows.INHERIT_ONLY_ACE
	base := []detAce{
		detAllow(self, fileAllAccess, inherit),
		detAllow(system, fileAllAccess, inherit),
		detAllow(administrators, fileAllAccess, inherit),
	}
	with := func(extra ...detAce) []byte { return detAcl(append(slices.Clone(base), extra...)...) }

	for _, test := range []struct {
		name    string
		acl     []byte
		nullAcl bool
		refuse  bool
		// the account a refusal must name, when it names one
		names *windows.SID
	}{
		{name: "allowlist only", acl: with(), refuse: false},
		{name: "NULL DACL", nullAcl: true, refuse: true},
		{name: "foreign explicit write", acl: with(detAllow(users, windows.FILE_WRITE_DATA, 0)), refuse: true, names: users},
		{name: "foreign explicit add-subdirectory", acl: with(detAllow(users, windows.FILE_APPEND_DATA, 0)), refuse: true, names: users},
		{name: "foreign explicit delete-child", acl: with(detAllow(users, fileDeleteChildAccess, 0)), refuse: true, names: users},
		{name: "foreign explicit delete", acl: with(detAllow(users, windows.DELETE, 0)), refuse: true, names: users},
		{name: "foreign explicit change-permissions", acl: with(detAllow(users, windows.WRITE_DAC, 0)), refuse: true, names: users},
		{name: "foreign explicit change-owner", acl: with(detAllow(users, windows.WRITE_OWNER, 0)), refuse: true, names: users},
		{name: "foreign generic write", acl: with(detAllow(users, windows.GENERIC_WRITE, 0)), refuse: true, names: users},
		{name: "foreign generic all", acl: with(detAllow(users, windows.GENERIC_ALL, 0)), refuse: true, names: users},
		{name: "foreign inherit-only write", acl: with(detAllow(users, windows.FILE_WRITE_DATA, inheritOnly)), refuse: true, names: users},
		{name: "foreign inherit-only change-permissions", acl: with(detAllow(users, windows.WRITE_DAC, inheritOnly)), refuse: true, names: users},
		{name: "foreign inherit-only generic all", acl: with(detAllow(users, windows.GENERIC_ALL, inheritOnly)), refuse: true, names: users},
		{name: "foreign object-inherit-only write", acl: with(detAllow(users, windows.FILE_WRITE_DATA, windows.OBJECT_INHERIT_ACE|windows.INHERIT_ONLY_ACE)), refuse: true, names: users},
		{name: "foreign write carrying the inherited flag", acl: with(detAllow(users, windows.FILE_GENERIC_WRITE, inherit|windows.INHERITED_ACE)), refuse: true, names: users},
		{name: "foreign denied write", acl: with(detDeny(users, windows.FILE_GENERIC_WRITE|windows.WRITE_DAC, inherit)), refuse: false},
		{name: "foreign directory read without inheritance", acl: with(detAllow(users, windows.FILE_GENERIC_READ|windows.FILE_TRAVERSE, 0)), refuse: false},
		{name: "allowlisted inherit-only generic all", acl: with(detAllow(system, windows.GENERIC_ALL, inheritOnly)), refuse: false},
		{name: "foreign inheritable read", acl: with(detAllow(users, windows.FILE_GENERIC_READ, inherit)), refuse: true, names: users},
		{name: "foreign inherit-only generic read", acl: with(detAllow(users, windows.GENERIC_READ, inheritOnly)), refuse: true, names: users},
	} {
		t.Run(test.name, func(t *testing.T) {
			acl := test.acl
			if test.nullAcl {
				acl = nil
			}
			dir := detAdoptedDirectory(t, acl)
			store, err := openRegistrationStore(filepath.Join(dir, "client.jwt"))
			if !test.refuse {
				if err != nil {
					t.Fatalf("custody refused a private directory: %v", err)
				}
				defer store.close()
				if err := store.write("client.jwt", []byte("synthetic")); err != nil {
					t.Fatalf("custody adopted the directory but cannot publish in it: %v", err)
				}
				return
			}
			if err == nil {
				store.close()
				t.Fatal("custody adopted a directory that is not private")
			}
			if errors.Is(err, errRegistrationOwnerActive) {
				t.Fatalf("a refused directory reads as a busy owner: %v", err)
			}
			if !strings.Contains(err.Error(), "icacls") {
				t.Fatalf("the refusal teaches no repair: %v", err)
			}
			if test.names != nil && !strings.Contains(err.Error(), test.names.String()) {
				t.Fatalf("the refusal does not name %s: %v", test.names.String(), err)
			}
			// the audit runs before anything is created: a refused directory
			// gains no lock file and no temporary
			if names := detCensus(t, dir); len(names) != 0 {
				t.Fatalf("a refused adoption left %v behind", names)
			}
		})
	}
}

// A DACL whose every entry is inherit-only grants nothing on the directory
// itself, so the account cannot even open it for custody: the store fails,
// and it never answers a busy owner or adopts it.
//
// Regression caught: an inherit-only allowlisted DACL treated as private and
// the walk's open errors swallowed. Reasoned: the kernel's access check denies
// the open before the audit runs.
func TestCustodyDetInheritOnlyDaclRefusesCustody(t *testing.T) {
	allowlist := detAllowlist(t)
	inheritOnly := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE | windows.INHERIT_ONLY_ACE)
	dir := detAdoptedDirectory(t, detAcl(
		detAllow(allowlist[0], fileAllAccess, inheritOnly),
		detAllow(allowlist[1], fileAllAccess, inheritOnly),
		detAllow(allowlist[2], fileAllAccess, inheritOnly),
	))
	// no restore is needed for cleanup: the directory stays empty, and the
	// test's temporary parent grants delete-child, which removes it whatever
	// its own DACL denies
	store, err := openRegistrationStore(filepath.Join(dir, "client.jwt"))
	if err == nil {
		store.close()
		t.Fatal("custody adopted a directory whose DACL grants nothing on itself")
	}
	if errors.Is(err, errRegistrationOwnerActive) {
		t.Fatalf("an inaccessible directory reads as a busy owner: %v", err)
	}
}

// The audit is not a one-time adoption check: every operation re-verifies the
// directory, so a foreign entry planted after custody opened refuses the next
// read, write, census and removal.
//
// Regression caught: auditDirectoryCustody dropped from check(), which would
// leave a live store publishing into a directory that already hands its
// children to another account. Demonstrated failing with the audit removed
// from check().
func TestCustodyDetLiveStoreRefusesAForeignAcePlantedAfterOpen(t *testing.T) {
	store := openTestStore(t, newRegistrationStorePath(t))
	if err := store.write("client.jwt", []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	allowlist := detAllowlist(t)
	inherit := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
	planted := detAcl(
		detAllow(allowlist[0], fileAllAccess, inherit),
		detAllow(allowlist[1], fileAllAccess, inherit),
		detAllow(allowlist[2], fileAllAccess, inherit),
		detAllow(testWellKnownSid(t, windows.WinBuiltinUsersSid), windows.FILE_WRITE_DATA, inherit|windows.INHERIT_ONLY_ACE),
	)
	if err := trySetTestPathSecurity(store.path, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		(*windows.ACL)(unsafe.Pointer(&planted[0])), nil); err != nil {
		t.Fatal(err)
	}
	operations := map[string]func() error{
		"read":   func() error { _, err := store.read("client.jwt"); return err },
		"write":  func() error { return store.write("client.jwt", []byte("replaced")) },
		"census": func() error { _, err := store.names(16); return err },
		"remove": func() error { return store.remove("client.jwt") },
	}
	for _, name := range []string{"read", "write", "census", "remove"} {
		err := operations[name]()
		if err == nil || !strings.Contains(err.Error(), "S-1-5-32-545") {
			t.Fatalf("%s after a foreign ACE was planted: %v, want a refusal naming BUILTIN\\Users", name, err)
		}
	}
	// nothing the refused operations attempted reached the directory
	raw, err := os.ReadFile(filepath.Join(store.path, "client.jwt"))
	if err != nil || string(raw) != "synthetic" {
		t.Fatalf("a refused operation changed the leaf: %q %v", raw, err)
	}
	detRequireNoPrefix(t, store.path, ".registration-")
}

// ---------------------------------------------------------------------------
// Leaf audit
// ---------------------------------------------------------------------------

// Every leaf shape against what the leaf audit must answer, the 0600 bound.
//
// Regressions caught, per case: FILE_READ_DATA, DELETE or WRITE_DAC dropped
// from the leaf mask, a generic grant (stored by the kernel as the mapped file
// rights) slipping through, an inherit-only entry on a
// file read as a grant (it confers nothing), a denied entry read as a grant,
// a NULL DACL leaf accepted, the NumberOfLinks check dropped (a hard link
// outside custody's naming), and the size bound dropped. Demonstrated failing
// with the link check removed. The write-attributes case covers finding F2: a
// foreign holder of FILE_WRITE_ATTRIBUTES could restamp the last-write time the
// lineage fingerprint is taken over, so the leaf audit now refuses that right.
func TestCustodyDetLeafAuditShapes(t *testing.T) {
	allowlist := detAllowlist(t)
	self := allowlist[0]
	users := testWellKnownSid(t, windows.WinBuiltinUsersSid)
	owned := func(extra ...detAce) []byte {
		return detAcl(append([]detAce{detAllow(self, fileAllAccess, 0)}, extra...)...)
	}
	type leafCase struct {
		name   string
		plant  func(t *testing.T, store *registrationStore, leaf string)
		refuse bool
	}
	withDacl := func(acl []byte) func(*testing.T, *registrationStore, string) {
		return func(t *testing.T, store *registrationStore, leaf string) {
			var pointer *windows.ACL
			if acl != nil {
				pointer = (*windows.ACL)(unsafe.Pointer(&acl[0]))
			}
			if err := trySetTestPathSecurity(filepath.Join(store.path, leaf),
				windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, pointer, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, test := range []leafCase{
		{name: "owner only", plant: withDacl(owned()), refuse: false},
		{name: "foreign read", plant: withDacl(owned(detAllow(users, windows.FILE_READ_DATA, 0))), refuse: true},
		{name: "foreign generic read", plant: withDacl(owned(detAllow(users, windows.GENERIC_READ, 0))), refuse: true},
		{name: "foreign append", plant: withDacl(owned(detAllow(users, windows.FILE_APPEND_DATA, 0))), refuse: true},
		{name: "foreign delete", plant: withDacl(owned(detAllow(users, windows.DELETE, 0))), refuse: true},
		{name: "foreign change-permissions", plant: withDacl(owned(detAllow(users, windows.WRITE_DAC, 0))), refuse: true},
		{name: "foreign inherit-only read confers nothing", plant: withDacl(owned(detAllow(users, windows.FILE_READ_DATA, windows.OBJECT_INHERIT_ACE|windows.INHERIT_ONLY_ACE))), refuse: false},
		{name: "foreign denied read", plant: withDacl(owned(detDeny(users, windows.FILE_READ_DATA, 0))), refuse: false},
		{name: "allowlisted SYSTEM read", plant: withDacl(owned(detAllow(allowlist[1], windows.FILE_GENERIC_READ, 0))), refuse: false},
		{name: "NULL DACL", plant: withDacl(nil), refuse: true},
		{name: "second hard link", refuse: true, plant: func(t *testing.T, store *registrationStore, leaf string) {
			if err := os.Link(filepath.Join(store.path, leaf), filepath.Join(store.path, "alias.jwt")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "beyond the size bound", refuse: true, plant: func(t *testing.T, store *registrationStore, leaf string) {
			if err := os.WriteFile(filepath.Join(store.path, leaf), bytes.Repeat([]byte("x"), maximumRegistrationBytes+1), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "foreign write-attributes", plant: withDacl(owned(detAllow(users, windows.FILE_WRITE_ATTRIBUTES, 0))), refuse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := openTestStore(t, newRegistrationStorePath(t))
			if err := store.write("client.jwt", []byte("synthetic")); err != nil {
				t.Fatal(err)
			}
			test.plant(t, store, "client.jwt")
			_, err := store.read("client.jwt")
			if test.refuse {
				if err == nil || !strings.Contains(err.Error(), "unsafe") {
					t.Fatalf("read a leaf outside the 0600 bound: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused a private leaf: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Default owner lifetime
// ---------------------------------------------------------------------------

// detQueryOwnerBelow queries the default owner a fixed number of frames below
// its caller, so the query buffer's frame lies in the stack region the caller
// then overwrites.
//
//go:noinline
func detQueryOwnerBelow(depth int) *windows.SID {
	if depth > 0 {
		return detQueryOwnerBelow(depth - 1)
	}
	return tokenDefaultOwner(windows.GetCurrentProcessToken())
}

// detReferenceOwner answers the token's default owner bytes through a buffer
// the test owns on the heap, independent of the code under test.
func detReferenceOwner(t *testing.T) []byte {
	t.Helper()
	var needed uint32
	_ = windows.GetTokenInformation(windows.GetCurrentProcessToken(), uint32(windows.TokenOwner), nil, 0, &needed)
	if needed == 0 {
		t.Fatal("the token answers no owner length")
	}
	buffer := make([]byte, needed)
	if err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), uint32(windows.TokenOwner), &buffer[0], needed, &needed); err != nil {
		t.Fatal(err)
	}
	owner := (*custodyTokenOwner)(unsafe.Pointer(&buffer[0])).Owner
	return slices.Clone(custodySidBytes(owner))
}

// detSidBytesIfValid answers a SID's bytes only when its header is valid, so
// a pointer into a clobbered frame is reported rather than walked.
func detSidBytesIfValid(sid *windows.SID) []byte {
	if sid == nil || !sid.IsValid() {
		return nil
	}
	return slices.Clone(custodySidBytes(sid))
}

// The default owner tokenDefaultOwner answers survives the death and reuse of
// the frame that queried it. The goroutine's stack is grown first, so the
// later overwrite reuses the very region the query's frame occupied rather
// than moving the stack; then the dead frame is filled with a marker that is
// not a SID revision, and the answer must still equal the token's owner byte
// for byte. The package's cached default owner, loaded once on some other
// goroutine whose stack is long gone, must equal it too.
//
// Regression caught: tokenDefaultOwner returning TOKEN_OWNER.Owner, the
// pointer the kernel writes into a stack-resident query buffer (the pre-fix
// `var buffer [128]byte` form, which escape analysis leaves on the stack).
// Demonstrated failing against that form.
func TestCustodyDetDefaultOwnerSurvivesItsDeadFrame(t *testing.T) {
	want := detReferenceOwner(t)
	type answer struct{ fresh, cached []byte }
	done := make(chan answer)
	go func() {
		// grow the stack past what the overwrite needs, so it is not copied
		// between the query and the overwrite
		clobberDeadFrames(64)
		owner := detQueryOwnerBelow(2)
		clobberDeadFrames(16)
		var got answer
		got.fresh = detSidBytesIfValid(owner)
		if _, err := custodySelfSid(); err == nil {
			got.cached = detSidBytesIfValid(custodyAccounts.defaultOwner)
		}
		done <- got
	}()
	got := <-done
	if !bytes.Equal(got.fresh, want) {
		t.Fatalf("the default owner read %x after its query frame was reused, want %x", got.fresh, want)
	}
	if !bytes.Equal(got.cached, want) {
		t.Fatalf("the cached default owner reads %x, want %x", got.cached, want)
	}
}

// ---------------------------------------------------------------------------
// Delete disposition
// ---------------------------------------------------------------------------

// A removal takes the name out of the directory at once while another handle
// that shares delete still holds the file: the next open answers not-found
// (ERROR_FILE_NOT_FOUND, never the access-denied a delete-pending name
// answers), the census no longer lists it, the same name can be published
// again immediately, and the holder still reads the bytes it opened.
//
// Regression caught: markDeleteDisposition using the legacy
// FileDispositionInfo class (or dropping FILE_DISPOSITION_POSIX_SEMANTICS),
// which leaves the name delete-pending until the holder closes. Demonstrated
// failing against the legacy class.
func TestCustodyDetRemoveLeavesNoDeletePendingName(t *testing.T) {
	store := openTestStore(t, newRegistrationStorePath(t))
	if err := store.write("client.jwt", []byte("original")); err != nil {
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
	// the raw status of a fresh open through the custody handle
	probe, err := ntOpenRelative(store.directory, "client.jwt", windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE)
	if err == nil {
		probe.Close()
		t.Fatal("a removed name still opens")
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("a removed name answers %v, want ERROR_FILE_NOT_FOUND (a delete-pending name answers access denied)", err)
	}
	if _, err := store.read("client.jwt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read of a removed name: %v, want ErrNotExist", err)
	}
	names, err := store.names(64)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(names, "client.jwt") {
		t.Fatalf("the census still lists a removed name: %v", names)
	}
	// the name is free for a new publication while the old file is still open
	if err := store.write("client.jwt", []byte("republished")); err != nil {
		t.Fatalf("republishing a removed name while its old file is held: %v", err)
	}
	if raw, err := store.read("client.jwt"); err != nil || string(raw) != "republished" {
		t.Fatalf("read after republication: %q %v", raw, err)
	}
	if got := detReadHandle(t, windows.Handle(held.Fd())); got != "original" {
		t.Fatalf("the holder of the removed file reads %q, want its original bytes", got)
	}
}

// A holder that shares no delete keeps the file, and the removal says so: the
// error is the sharing violation, never ErrNotExist (which every caller reads
// as "already absent" and proceeds on), and the bytes are still there.
//
// Regression caught: a removal whose sharing violation is mapped to, or
// swallowed as, an absent leaf. Reasoned; it is the error-mapping twin of the
// test above.
func TestCustodyDetRemoveRefusedByANonSharingHolder(t *testing.T) {
	store := openTestStore(t, newRegistrationStorePath(t))
	if err := store.write("client.jwt", []byte("original")); err != nil {
		t.Fatal(err)
	}
	holder := detHoldWithoutShareDelete(t, filepath.Join(store.path, "client.jwt"))
	defer windows.CloseHandle(holder)
	err := store.remove("client.jwt")
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("removal under a non-sharing holder: %v, want a sharing violation", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a held leaf reads as absent: %v", err)
	}
	if raw, err := store.read("client.jwt"); err != nil || string(raw) != "original" {
		t.Fatalf("a refused removal changed the leaf: %q %v", raw, err)
	}
}

// A leaf carrying the read-only attribute is still removable and replaceable
// by its owner, as unix unlinkat and rename ignore a file's own mode and Go's
// os.Remove clears the attribute. An operator's attrib +r or a tool that
// copies a 0444 mode must not wedge custody.
//
// Regression caught: the delete disposition and the rename without their
// ignore-read-only flags, where both answered access denied. Demonstrated
// failing against the code before the flags were added.
func TestCustodyDetReadOnlyLeafIsRemovableAndReplaceable(t *testing.T) {
	store := openTestStore(t, newRegistrationStorePath(t))
	for _, name := range []string{"removed.jwt", "replaced.jwt"} {
		if err := store.write(name, []byte("original")); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(store.path, name), 0400); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.remove("removed.jwt"); err != nil {
		t.Errorf("removal of a read-only leaf: %v", err)
	}
	if err := store.write("replaced.jwt", []byte("replacement")); err != nil {
		t.Errorf("replacement of a read-only leaf: %v", err)
	}
	// the store's own temporaries never outlive a refused replacement
	detRequireNoPrefix(t, store.path, ".registration-")
}

// ---------------------------------------------------------------------------
// Rename contention
// ---------------------------------------------------------------------------

// A publication over a target another handle holds while sharing delete (every
// handle this store opens shares delete) resolves at once, on the first
// attempt: POSIX rename semantics unlink the old name from under its holder,
// and the holder keeps reading the bytes it opened while a fresh read sees
// the new ones.
//
// Regression caught: the rename asked without FILE_RENAME_POSIX_SEMANTICS,
// where replacing any open target fails with access denied, so the store's own
// readers and lock-holding peers would wedge publication. Demonstrated failing
// with the POSIX flag removed.
func TestCustodyDetRenameOverAShareDeleteHolderResolves(t *testing.T) {
	store := openTestStore(t, newRegistrationStorePath(t))
	if err := store.write("client.jwt", []byte("original")); err != nil {
		t.Fatal(err)
	}
	held, err := store.open("client.jwt", leafOpenRead)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := store.write("client.jwt", []byte("replacement")); err != nil {
		t.Fatalf("publication over a holder that shares delete: %v", err)
	}
	if raw, err := store.read("client.jwt"); err != nil || string(raw) != "replacement" {
		t.Fatalf("read after publication: %q %v", raw, err)
	}
	if got := detReadHandle(t, windows.Handle(held.Fd())); got != "original" {
		t.Fatalf("the holder reads %q after the replace, want its original bytes", got)
	}
	detRequireNoPrefix(t, store.path, ".registration-")
}

// A publication over a target a non-sharing holder never releases stays
// refused: the error is the Win32 sharing violation (not the NTSTATUS type),
// the previous bytes stay, the holder reads them, and no temporary is left.
// The same holds for a renewal, whose temporary and lineage were already
// written when its rename was refused.
//
// Regressions caught: a refused rename reported as success or as an
// NTSTATUS-typed error, the temporary leaked by a refused publication, and a
// renewal's .<name>.renew- temporary leaked when its rename fails. The
// temporary cases were demonstrated failing with removeHeld disabled.
func TestCustodyDetRenameUnderAPersistentHolderStaysRefused(t *testing.T) {
	t.Run("write", func(t *testing.T) {
		store := openTestStore(t, newRegistrationStorePath(t))
		if err := store.write("client.jwt", []byte("original")); err != nil {
			t.Fatal(err)
		}
		holder := detHoldWithoutShareDelete(t, filepath.Join(store.path, "client.jwt"))
		defer windows.CloseHandle(holder)
		err := store.write("client.jwt", []byte("replacement"))
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			t.Fatalf("publication under a persistent holder: %v, want a sharing violation", err)
		}
		var status windows.NTStatus
		if errors.As(err, &status) {
			t.Fatalf("the refusal leaks its NTSTATUS spelling: %v", err)
		}
		if raw, err := store.read("client.jwt"); err != nil || string(raw) != "original" {
			t.Fatalf("a refused publication changed the leaf: %q %v", raw, err)
		}
		if got := detReadHandle(t, holder); got != "original" {
			t.Fatalf("the holder reads %q", got)
		}
		detRequireNoPrefix(t, store.path, ".registration-")
	})
	t.Run("renewal", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".urnetwork", "jwt")
		if err := WriteNetworkToken(path, "first-sign-in"); err != nil {
			t.Fatal(err)
		}
		holder := detHoldWithoutShareDelete(t, path)
		defer windows.CloseHandle(holder)
		ok, err := RenewNetworkToken(path, "first-sign-in", "renewed-sign-in")
		if ok || !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			t.Fatalf("renewal under a persistent holder: ok=%t err=%v, want a refused sharing violation", ok, err)
		}
		if got := detReadHandle(t, holder); got != "first-sign-in" {
			t.Fatalf("a refused renewal changed the token to %q", got)
		}
		detRequireNoPrefix(t, filepath.Dir(path), ".jwt.renew-", ".registration-")
	})
}

// ---------------------------------------------------------------------------
// Temporary cleanup
// ---------------------------------------------------------------------------

// A publication that fails after its temporary exists removes the temporary,
// whatever stage failed: the rename meeting a directory, or a temporary that
// fails its own leaf audit. Unix unlinks the temporary unconditionally on the
// held directory descriptor; the Windows store must leave the directory as
// the census found it.
//
// Regression caught: the deferred removeHeld in write() dropped or skipped on
// an error path, or replaced by remove(), whose own custody check would refuse
// the cleanup after a refused check. Demonstrated failing with removeHeld
// disabled.
func TestCustodyDetTemporaryRemovedWhenPublishFailsAfterItExists(t *testing.T) {
	t.Run("rename meets a directory", func(t *testing.T) {
		store := openTestStore(t, newRegistrationStorePath(t))
		if err := os.Mkdir(filepath.Join(store.path, "blocked.jwt"), 0700); err != nil {
			t.Fatal(err)
		}
		before := detCensus(t, store.path)
		if err := store.write("blocked.jwt", []byte("synthetic")); err == nil {
			t.Fatal("published over a directory")
		}
		if after := detCensus(t, store.path); !slices.Equal(before, after) {
			t.Fatalf("a failed publication changed the directory from %v to %v", before, after)
		}
	})
	t.Run("renewal rename meets a directory", func(t *testing.T) {
		// the token is a directory by the time the renewal renames over it:
		// the renewal's own temporary and the lineage were already written
		path := filepath.Join(t.TempDir(), ".urnetwork", "jwt")
		if err := WriteNetworkToken(path, "first-sign-in"); err != nil {
			t.Fatal(err)
		}
		networkRenewalTestHooks.beforeRename = func() {
			if err := os.Remove(path); err != nil {
				t.Error(err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Error(err)
			}
		}
		defer func() { networkRenewalTestHooks.beforeRename = nil }()
		if ok, err := RenewNetworkToken(path, "first-sign-in", "renewed-sign-in"); ok || err == nil {
			t.Fatalf("a renewal published over a directory: ok=%t err=%v", ok, err)
		}
		detRequireNoPrefix(t, filepath.Dir(path), ".jwt.renew-", ".registration-")
	})
}

// ---------------------------------------------------------------------------
// Leaf names
// ---------------------------------------------------------------------------

// A leaf name is exactly one plain component on every operation. A colon names
// a stream of another file (client.jwt::$DATA is client.jwt's own data), and a
// backslash or slash a path, and neither filepath.Base nor the NT object
// manager treats them as invalid by themselves.
//
// Regression caught: invalidLeaf losing its colon or separator refusal, or an
// operation that skips invalidLeaf. Without the colon refusal ensureLeaf opens
// a stream beside client.jwt and remove("client.jwt::$DATA") deletes
// client.jwt itself. Demonstrated failing with the `:\/` refusal removed.
func TestCustodyDetLeafNameRejection(t *testing.T) {
	store := openTestStore(t, newRegistrationStorePath(t))
	if err := store.write("client.jwt", []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	before := detCensus(t, store.path)
	operations := []struct {
		name string
		call func(name string) error
	}{
		{"write", func(name string) error { return store.write(name, []byte("planted")) }},
		{"read", func(name string) error { _, err := store.read(name); return err }},
		{"open", func(name string) error { return closeIfOpened(store.open(name, leafOpenRead)) }},
		{"openLeaf", func(name string) error { return closeIfOpened(store.openLeaf(name, custodyReadAccess)) }},
		{"createLeaf", func(name string) error { return closeIfOpened(store.createLeaf(name, custodyWriteAccess)) }},
		{"ensureLeaf", func(name string) error { return closeIfOpened(store.ensureLeaf(name, custodyLockAccess)) }},
		{"remove", func(name string) error { return store.remove(name) }},
		{"removeHeld", func(name string) error { return store.removeHeld(name) }},
		{"rename from", func(name string) error { return store.renameLeaf(name, "target.jwt") }},
		{"rename to", func(name string) error { return store.renameLeaf("client.jwt", name) }},
	}
	for _, name := range []string{
		"", ".", "..",
		"client.jwt::$DATA", "client.jwt:hidden", "client.jwt:hidden:$DATA", ":$DATA", "leaf:",
		`client.jwt\`, `sub\leaf.jwt`, "sub/leaf.jwt", `..\client.jwt`, "../client.jwt", `\client.jwt`,
	} {
		if !invalidLeaf(name) {
			t.Errorf("invalidLeaf(%q) accepts it", name)
		}
		for _, operation := range operations {
			if err := operation.call(name); err == nil || !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "bound") {
				t.Errorf("%s(%q): %v, want an invalid-leaf refusal", operation.name, name, err)
			}
		}
	}
	if after := detCensus(t, store.path); !slices.Equal(before, after) {
		t.Fatalf("refused names changed the directory from %v to %v", before, after)
	}
	if raw, err := store.read("client.jwt"); err != nil || string(raw) != "synthetic" {
		t.Fatalf("refused names reached client.jwt: %q %v", raw, err)
	}
}

func closeIfOpened(file *os.File, err error) error {
	if file != nil {
		file.Close()
	}
	return err
}

// ---------------------------------------------------------------------------
// Renewal lineage
// ---------------------------------------------------------------------------

// After several renewals the lineage is exactly the sign-in's own fingerprint
// followed by each renewal's, in order, every one of them what a pathname
// reader computes from the file the renewal left; a marker taken at any
// generation keeps blocking every later generation, and an explicit sign-in
// removes the lineage and unblocks on purpose.
//
// Regression caught: the renewal fingerprinting a last-write time other than
// the one the closed file carries (the pre-fix read through the writing handle
// before its close, which the file system's cleanup stamp then overrides), and
// the lineage losing its root or reordering members. NOT demonstrated: on the
// NTFS host this was run on, the pre-fix fingerprint (read through the writing
// handle before its close) equals what a later reader sees, so this test and
// TestRenewNetworkTokenFingerprintStaysInItsLineage both pass against it. The
// ordering, root retention and marker semantics are pinned regardless.
func TestCustodyDetRenewalLineageAfterSeveralRenewals(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".urnetwork", "jwt")
	if err := WriteNetworkToken(path, "sign-in-0"); err != nil {
		t.Fatal(err)
	}
	_, root, err := readNetworkCredential(path)
	if err != nil {
		t.Fatal(err)
	}
	generations := []string{root}
	previous := "sign-in-0"
	const renewals = 5
	for round := 1; round <= renewals; round++ {
		renewed := fmt.Sprintf("sign-in-0-renewal-%d", round)
		if ok, err := RenewNetworkToken(path, previous, renewed); err != nil || !ok {
			t.Fatalf("renewal %d: ok=%t err=%v", round, ok, err)
		}
		token, fingerprint, err := readNetworkCredential(path)
		if err != nil || token != renewed {
			t.Fatalf("renewal %d left %q: %v", round, token, err)
		}
		generations = append(generations, fingerprint)
		members, err := readNetworkCredentialLineage(path)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(members, generations) {
			t.Fatalf("renewal %d: lineage %v, want the reader's generations %v", round, short(members), short(generations))
		}
		for index, marker := range generations {
			blocks, err := networkCredentialMarkerBlocks(path, fingerprint, marker)
			if err != nil || !blocks {
				t.Fatalf("renewal %d: a marker from generation %d stopped blocking: %v", round, index, err)
			}
		}
		previous = renewed
	}
	if err := WriteNetworkToken(path, "sign-in-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path + ".lineage"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an explicit sign-in left the lineage: %v", err)
	}
	_, fresh, err := readNetworkCredential(path)
	if err != nil {
		t.Fatal(err)
	}
	if blocks, err := networkCredentialMarkerBlocks(path, fresh, root); err != nil || blocks {
		t.Fatalf("an explicit sign-in still blocked by the old sign-in's marker: %t %v", blocks, err)
	}
}

func short(fingerprints []string) []string {
	out := make([]string, len(fingerprints))
	for index, fingerprint := range fingerprints {
		out[index] = fingerprint[:min(12, len(fingerprint))]
	}
	return out
}

// ---------------------------------------------------------------------------
// Census
// ---------------------------------------------------------------------------

// A census always restarts its enumeration: a publication made after an
// earlier census, by the same store or by a provider slot that shares its
// directory object, appears in the next census of either; and the census bound
// is exact.
//
// Regression caught: names() without its seek to the start, which on Windows
// leaves the cached enumeration at its end so a second census misses every
// later publication; and a slot's census disturbing its owner's. Demonstrated
// failing with the seek removed.
func TestCustodyDetCensusSeesALaterPublish(t *testing.T) {
	ownerPath := newRegistrationStorePath(t)
	owner := openTestStore(t, ownerPath)
	slot, err := openRegistrationStoreForOwner(filepath.Join(owner.path, "provider.jwt"), owner)
	if err != nil {
		t.Fatal(err)
	}
	defer slot.close()

	census := func(store *registrationStore) []string {
		t.Helper()
		names, err := store.names(64)
		if err != nil {
			t.Fatal(err)
		}
		return names
	}
	if names := census(owner); slices.Contains(names, "first.jwt") {
		t.Fatalf("census before any publication: %v", names)
	}
	_ = census(slot)
	if err := owner.write("first.jwt", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if names := census(owner); !slices.Contains(names, "first.jwt") {
		t.Fatalf("the owner's census missed its own later publication: %v", names)
	}
	if names := census(slot); !slices.Contains(names, "first.jwt") {
		t.Fatalf("the slot's census missed the owner's later publication: %v", names)
	}
	if err := slot.write("second.jwt", []byte("2")); err != nil {
		t.Fatal(err)
	}
	if names := census(owner); !slices.Contains(names, "second.jwt") || !slices.Contains(names, "first.jwt") {
		t.Fatalf("the owner's census missed the slot's later publication: %v", names)
	}
	if err := owner.remove("first.jwt"); err != nil {
		t.Fatal(err)
	}
	if names := census(slot); slices.Contains(names, "first.jwt") {
		t.Fatalf("the slot's census still lists a removed leaf: %v", names)
	}

	// the bound: exactly as many names as allowed passes, one more refuses
	exact := len(census(owner))
	if _, err := owner.names(exact); err != nil {
		t.Fatalf("a census of exactly its bound (%d) refused: %v", exact, err)
	}
	if _, err := owner.names(exact - 1); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("a census one over its bound: %v, want a refusal", err)
	}
	for _, bad := range []int{0, -1, 4097} {
		if _, err := owner.names(bad); err == nil {
			t.Fatalf("a census bound of %d was accepted", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// Owner lock
// ---------------------------------------------------------------------------

// Within one process the owner lock excludes every second holder of the same
// client, whichever way it is reached, and nothing else: an owner and a slot
// of a different client coexist, a slot for the owner's own client is busy,
// two slots of one client exclude each other, a failed acquisition leaves the
// holder fully working, and a released lock is free again.
//
// Regression caught: the lock taken shared (LOCKFILE_EXCLUSIVE_LOCK dropped),
// not taken at all, keyed by directory instead of by client, or a contention
// reported as anything but errRegistrationOwnerActive. Demonstrated failing
// with the lock taken shared.
func TestCustodyDetOwnerLockWithinOneProcess(t *testing.T) {
	clientPath := newRegistrationStorePath(t)
	owner := openTestStore(t, clientPath)
	busy := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, errRegistrationOwnerActive) {
			t.Fatalf("%s: %v, want errRegistrationOwnerActive", what, err)
		}
	}
	_, err := openRegistrationStore(clientPath)
	busy("a second store of the same client", err)
	_, err = openRegistrationStoreForOwner(clientPath, owner)
	busy("a slot for the owner's own client", err)

	providerPath := filepath.Join(owner.path, "provider.jwt")
	slot, err := openRegistrationStoreForOwner(providerPath, owner)
	if err != nil {
		t.Fatalf("a slot of a different client: %v", err)
	}
	_, err = openRegistrationStoreForOwner(providerPath, owner)
	busy("a second slot of the same client", err)
	_, err = openRegistrationStore(providerPath)
	busy("a store opened by pathname for a held slot", err)

	// a failed acquisition costs the holders nothing
	if err := owner.write("client.jwt", []byte("owner")); err != nil {
		t.Fatalf("the owner after contention: %v", err)
	}
	if err := slot.write("provider.jwt", []byte("slot")); err != nil {
		t.Fatalf("the slot after contention: %v", err)
	}
	if err := slot.close(); err != nil {
		t.Fatal(err)
	}
	again, err := openRegistrationStoreForOwner(providerPath, owner)
	if err != nil {
		t.Fatalf("a released slot stayed held: %v", err)
	}
	if err := again.close(); err != nil {
		t.Fatal(err)
	}
}

// The lock excludes another process in both directions, interleaved by
// explicit signals rather than timing: the helper reports that it holds the
// lock before this process tries, and releases it only when told, closing its
// store before it says so.
//
// Regression caught: a lock that is only per-process (a mutex, or a lock
// scoped to the handle table) or released only at process exit. Demonstrated
// failing with the lock taken shared.
func TestCustodyDetOwnerLockAcrossProcesses(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := newRegistrationStorePath(t)

	t.Run("helper holds", func(t *testing.T) {
		helper := exec.Command(executable, "-test.run=^TestCustodyDetLockHelperProcess$", "-test.count=1")
		helper.Env = append(os.Environ(), "URNETWORK_CUSTODY_DET_HELPER=hold", "URNETWORK_CUSTODY_DET_PATH="+path)
		stdin, err := helper.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := helper.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := helper.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			stdin.Close()
			_ = helper.Process.Kill()
			_ = helper.Wait()
		})
		lines := bufio.NewScanner(stdout)
		detAwaitLine(t, lines, "custody-det-held")
		if _, err := openRegistrationStore(path); !errors.Is(err, errRegistrationOwnerActive) {
			t.Fatalf("this process acquired a lock another process holds: %v", err)
		}
		if err := stdin.Close(); err != nil {
			t.Fatal(err)
		}
		detAwaitLine(t, lines, "custody-det-released")
		store, err := openRegistrationStore(path)
		if err != nil {
			t.Fatalf("a lock the other process released stayed held: %v", err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("this process holds", func(t *testing.T) {
		owner := openTestStore(t, path)
		try := func() string {
			helper := exec.Command(executable, "-test.run=^TestCustodyDetLockHelperProcess$", "-test.count=1")
			helper.Env = append(os.Environ(), "URNETWORK_CUSTODY_DET_HELPER=try", "URNETWORK_CUSTODY_DET_PATH="+path)
			out, err := helper.CombinedOutput()
			if err != nil {
				t.Fatalf("helper: %v %s", err, out)
			}
			return string(out)
		}
		if out := try(); !strings.Contains(out, "custody-det-busy") {
			t.Fatalf("another process did not meet the held lock: %s", out)
		}
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
		if out := try(); !strings.Contains(out, "custody-det-acquired") {
			t.Fatalf("another process did not acquire the released lock: %s", out)
		}
	})
}

func detAwaitLine(t *testing.T, lines *bufio.Scanner, want string) {
	t.Helper()
	for lines.Scan() {
		if strings.TrimSpace(lines.Text()) == want {
			return
		}
	}
	t.Fatalf("the helper ended before it said %q: %v", want, lines.Err())
}

// TestCustodyDetLockHelperProcess is the other process of the lock test; it
// does nothing unless that test runs it.
func TestCustodyDetLockHelperProcess(t *testing.T) {
	mode, path := os.Getenv("URNETWORK_CUSTODY_DET_HELPER"), os.Getenv("URNETWORK_CUSTODY_DET_PATH")
	if mode == "" || path == "" {
		t.Skip("helper process of TestCustodyDetOwnerLockAcrossProcesses")
	}
	store, err := openRegistrationStore(path)
	switch mode {
	case "hold":
		if err != nil {
			t.Fatalf("the helper could not take the lock: %v", err)
		}
		fmt.Println("custody-det-held")
		// hold until the parent closes stdin
		_, _ = io.Copy(io.Discard, os.Stdin)
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		fmt.Println("custody-det-released")
	case "try":
		switch {
		case errors.Is(err, errRegistrationOwnerActive):
			fmt.Println("custody-det-busy")
		case err == nil:
			fmt.Println("custody-det-acquired")
			if err := store.close(); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("the helper failed for another reason: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Unique temporaries
// ---------------------------------------------------------------------------

// A temporary name the store did not pick is never reused: createLeaf refuses
// an existing name rather than opening it, so a leftover or planted file of
// the temporary's name cannot become the bytes a publication renames into
// place.
//
// Regression caught: createLeaf asked with FILE_OPEN_IF or FILE_SUPERSEDE
// instead of FILE_CREATE, the O_EXCL parity. Reasoned.
func TestCustodyDetCreateLeafRefusesAnExistingName(t *testing.T) {
	store := openTestStore(t, newRegistrationStorePath(t))
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := ".registration-" + hex.EncodeToString(random[:])
	if err := os.WriteFile(filepath.Join(store.path, name), []byte("planted"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := store.createLeaf(name, custodyWriteAccess)
	if err == nil {
		file.Close()
		t.Fatal("createLeaf opened an existing name")
	}
	if !errors.Is(err, windows.ERROR_FILE_EXISTS) && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		t.Fatalf("createLeaf over an existing name: %v, want already-exists", err)
	}
	if raw, err := os.ReadFile(filepath.Join(store.path, name)); err != nil || string(raw) != "planted" {
		t.Fatalf("the refused create touched the existing file: %q %v", raw, err)
	}
}

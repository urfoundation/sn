//go:build linux

// Offline directory-head adapters build only fresh public staging members.
// A fixed application registry selects each Spec; this API never enrolls the
// live target, replaces an attribute, starts a runtime owner or grants restart.
package durablehead

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Empty auxiliary members retain the application's original marker grammar.
func freshDirectoryPreparationFiles(spec Spec) []durablevolume.PreparationFile {
	names := append([]string(nil), spec.AuxiliaryNames...)
	sort.Strings(names)
	var files []durablevolume.PreparationFile
	sum := sha256.Sum256(nil)
	for _, name := range names {
		files = append(files, durablevolume.PreparationFile{Path: name, Kind: "file", Mode: 0600, Sha256: "sha256:" + hex.EncodeToString(sum[:])})
	}
	return files
}

// Canonical fixed-profile data is bounded independently of the payload limit.
func freshDirectoryPreparationScope(owner durablevolume.PreparationOwner, spec Spec, census json.RawMessage) error {
	if err := validateSpec(spec); err != nil {
		return err
	}
	if spec.LockName != "" || owner.Kind != spec.Kind || owner.RelativePath != "." || owner.Purpose != "fresh" || len(census) == 0 || len(census) > 64*1024 || !json.Valid(census) {
		return errors.New("directory-head preparation requires one fixed bounded fresh profile")
	}
	return nil
}

// Build may create its exclusive staging namespace only. The base stage reads
// and binds those sources; the target's original generation stays untouched.
func BuildFreshDirectoryPreparation(ctx context.Context, parent *os.File, name string, owner durablevolume.PreparationOwner, spec Spec, census json.RawMessage, exclusiveRoot bool) (result durablevolume.PreparationOwnerPlan, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = durablevolume.PreparationOwnerPlan{}
		}
	}()
	if ctx == nil || parent == nil || !simpleName(name) {
		return result, errors.New("directory-head staging requires a named borrowed parent")
	}
	if err := errors.Join(ctx.Err(), freshDirectoryPreparationScope(owner, spec, census)); err != nil {
		return result, err
	}
	if err := unix.Mkdirat(int(parent.Fd()), name, 0700); err != nil {
		return result, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return result, err
	}
	directory := os.NewFile(uintptr(fd), name)
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	files := freshDirectoryPreparationFiles(spec)
	for _, member := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		child, err := unix.Openat(fd, member.Path, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
		if err != nil {
			return result, err
		}
		file := os.NewFile(uintptr(child), member.Path)
		if err := errors.Join(file.Sync(), file.Close()); err != nil {
			return result, err
		}
	}
	if err := errors.Join(directory.Sync(), parent.Sync(), ctx.Err()); err != nil {
		return result, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, ExclusiveRoot: exclusiveRoot, Files: files, Census: append(json.RawMessage(nil), census...),
		Attributes: []durablevolume.PreparationAttributeSpec{{Path: ".", Name: Attribute(spec.Kind, spec.Name)}}}, nil
}

// Inspect borrows a real target descriptor and returns checkpoint bytes only.
// Existing payload or nonempty auxiliaries cannot be reclassified as fresh.
func InspectFreshDirectoryPreparation(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, spec Spec, census json.RawMessage, exclusiveRoot bool) (result []durablevolume.PreparedAttribute, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = nil
		}
	}()
	if ctx == nil || root == nil {
		return nil, errors.New("directory-head inspection requires a borrowed root")
	}
	if err := errors.Join(ctx.Err(), freshDirectoryPreparationScope(owner.Owner, spec, census)); err != nil {
		return nil, err
	}
	expected := durablevolume.PreparationAttributeSpec{Path: ".", Name: Attribute(spec.Kind, spec.Name)}
	var actualRaw, expectedRaw bytes.Buffer
	if err := errors.Join(json.Compact(&actualRaw, owner.Census), json.Compact(&expectedRaw, census)); err != nil {
		return nil, err
	}
	if !bytes.Equal(actualRaw.Bytes(), expectedRaw.Bytes()) || owner.ExclusiveRoot != exclusiveRoot || !reflect.DeepEqual(owner.Files, freshDirectoryPreparationFiles(spec)) || len(owner.Attributes) != 1 || owner.Attributes[0] != expected {
		return nil, errors.New("directory-head plan changed its fixed profile or exact member census")
	}
	var parent unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &parent); err != nil {
		return nil, observation(err, false)
	}
	if parent.Mode&unix.S_IFMT != unix.S_IFDIR || parent.Mode&07777 != 0700 || parent.Uid != uint32(os.Geteuid()) {
		return nil, identity(errors.New("directory-head preparation root is not private"))
	}
	var absent unix.Stat_t
	if err := unix.Fstatat(int(root.Fd()), spec.Name, &absent, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		if err != nil {
			return nil, observation(err, false)
		}
		return nil, identity(errors.New("fresh directory head already contains retained progress"))
	}
	checkpoint := Checkpoint{Schema: Schema, Kind: spec.Kind, Name: spec.Name, MaximumBytes: spec.MaximumBytes, DirectoryInode: parent.Ino}
	for _, member := range freshDirectoryPreparationFiles(spec) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		auxiliary, err := inspectFreshDirectoryAuxiliary(root, parent, member.Path)
		if err != nil {
			return nil, err
		}
		checkpoint.Auxiliaries = append(checkpoint.Auxiliaries, auxiliary)
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &after); err != nil {
		return nil, observation(err, false)
	}
	if after.Dev != parent.Dev || after.Ino != parent.Ino || after.Mode != parent.Mode || after.Uid != parent.Uid || after.Gid != parent.Gid {
		return nil, identity(errors.New("directory-head preparation root changed during inspection"))
	}
	raw, err := json.Marshal(checkpoint)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return []durablevolume.PreparedAttribute{{Spec: expected, Raw: raw}}, nil
}

// A failed stat remains an observation failure. Positive alias/protection or
// nonempty-marker facts reject fresh enrollment without discarding any bytes.
func inspectFreshDirectoryAuxiliary(root *os.File, parent unix.Stat_t, name string) (result Auxiliary, resultErr error) {
	fd, err := unix.Openat(int(root.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return result, observation(err, true)
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	var opened, named unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return result, observation(err, true)
	}
	if err := unix.Fstatat(int(root.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return result, observation(err, true)
	}
	if opened.Dev != parent.Dev || opened.Dev != named.Dev || opened.Ino != named.Ino || opened.Mode != named.Mode || opened.Uid != named.Uid || opened.Gid != named.Gid || opened.Size != 0 || named.Size != 0 || opened.Nlink != 1 || named.Nlink != 1 || opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Mode&07777 != 0600 || opened.Uid != uint32(os.Geteuid()) {
		return result, identity(errors.New("fresh directory-head auxiliary is claimed, replaced, aliased or unprotected"))
	}
	return Auxiliary{Name: name, Inode: opened.Ino}, nil
}

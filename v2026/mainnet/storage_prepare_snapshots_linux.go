//go:build linux

// Fresh snapshot preparation installs only an empty original marker and the
// explicit absent head. Runtime approval and marker claims remain separate.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// The exact request binds the new namespace and its fixed existing capacity.
// It cannot supply checkpoint bytes, auxiliary payloads or a retained claim.
type storageSnapshotPreparationScope struct {
	Schema       string `json:"schema"`
	Name         string `json:"name"`
	MaximumBytes int64  `json:"maximum_bytes"`
}

// Only existing mainnet kinds and their original runtime bounds are supported.
// Owner-device custody never inherits a daemon declaration or vice versa.
func storagePreparationSnapshotSpec(ownerLocal bool, owner durablevolume.PreparationOwner) (durablehead.Spec, storageSnapshotPreparationScope, error) {
	var scope storageSnapshotPreparationScope
	maximum := int64(0)
	switch owner.Kind {
	case "mainnet-root-action":
		maximum = rootActionStoreLimit
	case "mainnet-root-service", "mainnet-bootstrap-chain":
		maximum = rootServiceStoreLimit
	case "mainnet-root-submission":
		maximum = rootSubmissionStoreLimit
	case "mainnet-root-offline":
		maximum = rootOfflineStoreLimit
	case "mainnet-owner-signing":
		maximum = ownerSigningReplyLimit
	case "mainnet-owner-recycle":
		maximum = ownerRecycleStoreLimit
	case "mainnet-root-register":
		maximum = rootRegisterStoreLimit
	case "mainnet-native-treasury":
		maximum = treasuryStoreLimit
	case "mainnet-owner-trim":
		maximum = ownerTrimStoreLimit
	case "mainnet-evm-action":
		maximum = 512 * 1024
	case "mainnet-bootstrap-root":
		maximum = 16 * 1024
	case "mainnet-validator-activation":
		maximum = 128 * 1024
	case "mainnet-host-action":
		maximum = 64 * 1024
	case "mainnet-monitor-checkpoint":
		maximum = maxRpcReplyBytes
	case economicConservationStorageKind:
		maximum = economicConservationStorageMaximum
	}
	if maximum == 0 {
		return durablehead.Spec{}, scope, errors.New("storage preparation owner kind is not in the implemented fixed registry")
	}
	if err := decodePlanJson(owner.Inputs, &scope); err != nil {
		return durablehead.Spec{}, scope, err
	}
	name := scope.Name
	if owner.Purpose != "fresh" && owner.Purpose != "restore" || owner.RelativePath != "." || maximum == 0 || scope.Schema != "urnetwork-snapshot-preparation-v1" || scope.MaximumBytes != maximum ||
		ownerLocal != (owner.Kind == "mainnet-owner-signing") || name == "" || len(name) > 155 || name == "." || name == ".." || filepath.Base(name) != name ||
		strings.ContainsAny(name, "\x00\n\r") || strings.HasPrefix(name, ".durable-head-") || owner.Kind == "mainnet-bootstrap-root" && name != bootstrapRootProgressFile {
		return durablehead.Spec{}, scope, errors.New("snapshot preparation kind, scope, fresh name or capacity differs from its fixed runtime profile")
	}
	return durablehead.Spec{Kind: owner.Kind, Name: name, MaximumBytes: maximum, LockName: name + ".lock", AuxiliaryNames: []string{name + ".lock"}}, scope, nil
}

// A shared empty-file helper validates opened facts separately from named
// facts, preserving observation errors rather than calling them identity loss.
func storagePreparationEmptySnapshotMarker(ctx context.Context, root *os.File, spec durablehead.Spec) (result []byte, resultErr error) {
	defer func() {
		if resultErr != nil {
			result = nil
		}
	}()
	if ctx == nil || root == nil {
		return nil, errors.New("snapshot preparation requires a borrowed root and context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var parent unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &parent); err != nil {
		return nil, mainnetDurableUnavailable("cannot observe preparation root", err)
	}
	if parent.Mode&unix.S_IFMT != unix.S_IFDIR || parent.Mode&07777 != 0700 || parent.Uid != uint32(os.Geteuid()) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("snapshot preparation root is not private"))
	}
	var absent unix.Stat_t
	if err := unix.Fstatat(int(root.Fd()), spec.Name, &absent, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		if err != nil {
			return nil, mainnetDurableUnavailable("cannot observe fresh snapshot absence", err)
		}
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("fresh snapshot already contains retained progress"))
	}
	fd, err := unix.Openat(int(root.Fd()), spec.LockName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, errors.Join(durablevolume.ErrIdentity, err)
		}
		return nil, mainnetDurableUnavailable("cannot open prepared snapshot marker", err)
	}
	file := os.NewFile(uintptr(fd), spec.LockName)
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	var opened, named unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return nil, mainnetDurableUnavailable("cannot observe prepared snapshot marker", err)
	}
	if err := unix.Fstatat(int(root.Fd()), spec.LockName, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, errors.Join(durablevolume.ErrIdentity, err)
		}
		return nil, mainnetDurableUnavailable("cannot reobserve prepared snapshot marker", err)
	}
	if opened.Dev != parent.Dev || opened.Ino != named.Ino || opened.Dev != named.Dev || opened.Mode != named.Mode || opened.Uid != named.Uid || opened.Gid != named.Gid ||
		opened.Size != 0 || named.Size != 0 || opened.Nlink != 1 || named.Nlink != 1 || opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Mode&07777 != 0600 || opened.Uid != uint32(os.Geteuid()) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("snapshot marker is changed, claimed, aliased or unprotected"))
	}
	checkpoint := durablehead.Checkpoint{Schema: durablehead.Schema, Kind: spec.Kind, Name: spec.Name, MaximumBytes: spec.MaximumBytes,
		DirectoryInode: parent.Ino, LockName: spec.LockName, Auxiliaries: []durablehead.Auxiliary{{Name: spec.LockName, Inode: opened.Ino}}}
	raw, err := json.Marshal(checkpoint)
	if err := errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return raw, nil
}

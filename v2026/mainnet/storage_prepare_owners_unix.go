//go:build linux || darwin

// Fixed fresh adapters create public staging bytes. Only the shared accepted
// plan publishes target members and their inode-bound checkpoints.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/miner"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Fixed formats can require an empty member without accepting arbitrary bytes.
func storagePreparationEmptyFile(name string) durablevolume.PreparationFile {
	digest := sha256.Sum256(nil)
	return durablevolume.PreparationFile{Path: name, Kind: "file", Mode: 0600, Sha256: "sha256:" + hex.EncodeToString(digest[:])}
}

// Fresh staging is exclusive and private. No runtime target, existing file,
// checkpoint or signed object is opened for writing by an adapter.
func storagePreparationStageEmpty(ctx context.Context, parent *os.File, name string, files []durablevolume.PreparationFile) (resultErr error) {
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
		if member.Path == "." || member.Path == ".." || filepath.Base(member.Path) != member.Path || member.Bytes != 0 {
			return errors.New("fixed fresh staging member is not an empty direct child")
		}
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		switch member.Kind {
		case "directory":
			if member.Mode != 0700 || member.Sha256 != "" {
				return errors.New("fresh owner directory changed its fixed mode")
			}
			if err := unix.Mkdirat(fd, member.Path, 0700); err != nil {
				return err
			}
			flags |= unix.O_DIRECTORY
		case "file":
			if member != storagePreparationEmptyFile(member.Path) {
				return errors.New("fresh owner file changed its fixed empty content")
			}
			flags = unix.O_RDWR | unix.O_CREAT | unix.O_EXCL | unix.O_NOFOLLOW | unix.O_CLOEXEC
		default:
			return errors.New("unknown fresh owner member kind")
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

// All selectors are fixed application kinds. Fresh native custody is shared
// in format, but daemon and local-owner command admission remain independent.
func buildStoragePreparationFixedOwner(ctx context.Context, staging *os.File, name string, owner durablevolume.PreparationOwner, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	if owner.Purpose != "fresh" || owner.RelativePath != "." {
		return durablevolume.PreparationOwnerPlan{}, errors.New("fixed preparation supports explicitly fresh custody only")
	}
	if owner.Kind == validator.AttemptLedgerPreparationKind {
		if ownerLocal {
			return durablevolume.PreparationOwnerPlan{}, errors.New("validator ledger requires daemon scope")
		}
		return buildStoragePreparationOwner(ctx, staging, name, owner)
	}
	switch owner.Kind {
	case storageSdkWorkKind:
		return buildStorageSdkWork(ctx, staging, name, owner, ownerLocal)
	case storageOriginalContractKind:
		return buildStorageOriginalContract(ctx, staging, name, owner, ownerLocal)
	case storageProviderPublicationKind:
		return buildStorageProviderPublication(ctx, staging, name, owner, ownerLocal)
	case "fleet-recovery", "provider-claim-queue":
		return miner.BuildFreshStoragePreparation(ctx, staging, name, owner, ownerLocal)
	case "mainnet-successor-local-members", "mainnet-successor-nonce-members":
		return buildStoragePreparationMembers(ctx, staging, name, owner, ownerLocal)
	}
	var files []durablevolume.PreparationFile
	var attributes []durablevolume.PreparationAttributeSpec
	var census any
	if owner.Kind == chain.NativeJournalPreparationKind {
		var scope chain.NativeJournalPreparationScope
		if err := decodePlanJson(owner.Inputs, &scope); err != nil {
			return durablevolume.PreparationOwnerPlan{}, err
		}
		var err error
		files, err = chain.NativeJournalPreparationLayout(scope)
		if err != nil {
			return durablevolume.PreparationOwnerPlan{}, err
		}
		census = scope
		attributes = []durablevolume.PreparationAttributeSpec{{Path: ".", Name: chain.NativeJournalCustodyAttribute}}
	} else {
		spec, scope, err := storagePreparationSnapshotSpec(ownerLocal, owner)
		if err != nil {
			return durablevolume.PreparationOwnerPlan{}, err
		}
		files = []durablevolume.PreparationFile{storagePreparationEmptyFile(spec.LockName)}
		attributes = []durablevolume.PreparationAttributeSpec{{Path: spec.LockName, Name: durablehead.Attribute(spec.Kind, spec.Name)}}
		census = scope
	}
	raw, err := json.Marshal(census)
	if err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	if err := storagePreparationStageEmpty(ctx, staging, name, files); err != nil {
		return durablevolume.PreparationOwnerPlan{}, err
	}
	return durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name, Files: files, Census: raw, Attributes: attributes}, nil
}

// Accepted portable bytes do not determine their own checkpoint grammar.
// Inspect derives the only permitted format again from the original request.
func inspectStoragePreparationFixedOwner(ctx context.Context, target *os.File, owner durablevolume.PreparationOwnerPlan, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	if owner.Owner.Purpose != "fresh" || owner.Owner.RelativePath != "." {
		return nil, errors.New("fixed preparation cannot reinterpret retained or restored custody as fresh")
	}
	if owner.Owner.Kind == validator.AttemptLedgerPreparationKind {
		if ownerLocal {
			return nil, errors.New("owner-local preparation cannot reinterpret daemon ledger custody")
		}
		return inspectStoragePreparationOwner(ctx, target, owner)
	}
	switch owner.Owner.Kind {
	case storageSdkWorkKind:
		return inspectStorageSdkWork(ctx, target, owner, ownerLocal)
	case storageOriginalContractKind:
		return inspectStorageOriginalContract(ctx, target, owner, ownerLocal)
	case storageProviderPublicationKind:
		return inspectStorageProviderPublication(ctx, target, owner, ownerLocal)
	case "fleet-recovery", "provider-claim-queue":
		return miner.InspectFreshStoragePreparation(ctx, target, owner, ownerLocal)
	case "mainnet-successor-local-members", "mainnet-successor-nonce-members":
		return inspectStoragePreparationMembers(ctx, target, owner, ownerLocal)
	}
	var files []durablevolume.PreparationFile
	var spec durablevolume.PreparationAttributeSpec
	var checkpoint func() ([]byte, error)
	if owner.Owner.Kind == chain.NativeJournalPreparationKind {
		var scope, census chain.NativeJournalPreparationScope
		if err := errors.Join(decodePlanJson(owner.Owner.Inputs, &scope), decodePlanJson(owner.Census, &census)); err != nil {
			return nil, err
		}
		if scope != census {
			return nil, errors.New("native preparation census changed its reviewed capacity vector")
		}
		var err error
		files, err = chain.NativeJournalPreparationLayout(scope)
		if err != nil {
			return nil, err
		}
		spec = durablevolume.PreparationAttributeSpec{Path: ".", Name: chain.NativeJournalCustodyAttribute}
		checkpoint = func() ([]byte, error) { return chain.BuildFreshNativeJournalPreparationCheckpoint(ctx, target, scope) }
	} else {
		headSpec, scope, err := storagePreparationSnapshotSpec(ownerLocal, owner.Owner)
		if err != nil {
			return nil, err
		}
		var census storageSnapshotPreparationScope
		if err := decodePlanJson(owner.Census, &census); err != nil {
			return nil, err
		}
		if scope != census {
			return nil, errors.New("snapshot preparation census changed its fixed fresh format")
		}
		files = []durablevolume.PreparationFile{storagePreparationEmptyFile(headSpec.LockName)}
		spec = durablevolume.PreparationAttributeSpec{Path: headSpec.LockName, Name: durablehead.Attribute(headSpec.Kind, headSpec.Name)}
		checkpoint = func() ([]byte, error) { return storagePreparationEmptySnapshotMarker(ctx, target, headSpec) }
	}
	if !reflect.DeepEqual(files, owner.Files) || len(owner.Attributes) != 1 || owner.Attributes[0] != spec {
		return nil, errors.New("fresh owner plan changed its fixed member or checkpoint census")
	}
	raw, err := checkpoint()
	if err != nil {
		return nil, err
	}
	return []durablevolume.PreparedAttribute{{Spec: spec, Raw: raw}}, nil
}

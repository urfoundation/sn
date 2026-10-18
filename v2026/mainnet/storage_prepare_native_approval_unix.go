//go:build linux || darwin

// Signed producer approvals have their own fixed restore owner. Exact original
// references select the files; no arbitrary configuration-file copying occurs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

const storageNativeApprovalKind = "mainnet-native-execution-approvals"

type storageNativeApprovalScope struct {
	Native            storageNativeProducerScope `json:"native"`
	SharedDirectories []string                   `json:"shared_original_ancestors,omitempty"`
}

type storageNativeApprovalContent struct {
	Bytes  uint64
	Sha256 string
}

// The semantic loader has already verified these exact original digests.
// Member census does not embed or reinterpret the large signed documents.
func storageNativeApprovalMembers(scope storageNativeProducerScope, root string) map[string]storageNativeApprovalContent {
	members := map[string]storageNativeApprovalContent{}
	if scope.Policy.Execution == nil || scope.Policy.Execution.Producer == nil {
		return members
	}
	references := append([]planFileReference{scope.Policy.Execution.Producer.Authority}, scope.Policy.Execution.Producer.Renewals...)
	for index, reference := range references {
		path, found := storageNativeRestoreRelative(root, reference.Path)
		if !found {
			continue
		}
		if scope.ApprovalSources != nil && index < len(scope.ApprovalSources) {
			source := scope.ApprovalSources[index]
			members[path] = storageNativeApprovalContent{Bytes: source.Bytes, Sha256: source.Reference.Sha256}
		} else if scope.ApprovalSources == nil && index < len(scope.Approvals) {
			raw := scope.Approvals[index]
			members[path] = storageNativeApprovalContent{Bytes: uint64(len(raw)), Sha256: monitorReadDigest(raw)}
		}
	}
	return members
}

func planStorageNativeApprovalRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	var empty durablevolume.PreparationOwnerPlan
	var scope storageNativeApprovalScope
	if ownerLocal || owner.Kind != storageNativeApprovalKind || owner.Purpose != "restore" || owner.RelativePath != "." || owner.RestoreCoverage != durablevolume.PreparationCompleteUnion {
		return empty, errors.New("native approvals require original daemon complete-union custody")
	}
	if err := decodePlanJson(owner.Inputs, &scope); err != nil {
		return empty, err
	}
	expanded, _, err := loadStorageNativeProducerScope(ctx, scope.Native)
	if err != nil {
		return empty, err
	}
	scope.Native = expanded
	members := storageNativeApprovalMembers(scope.Native, report.StateRoot.Path)
	if len(members) == 0 {
		return empty, errors.New("native approval owner has no original signed member")
	}
	directories := map[string]bool{}
	for path := range members {
		for path = filepath.Dir(path); path != "."; path = filepath.Dir(path) {
			directories[path] = true
		}
	}
	for index, path := range scope.SharedDirectories {
		if !directories[path] || index > 0 && scope.SharedDirectories[index-1] >= path {
			return empty, errors.New("native approvals changed shared ancestor coverage")
		}
		delete(directories, path)
	}
	result := durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name}
	seen := map[string]bool{}
	for _, entry := range report.Entries {
		original, file := members[entry.Path]
		if !file && !directories[entry.Path] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if seen[entry.Path] || entry.Physical == nil || entry.Physical.Device != report.PhysicalRoot.Device || entry.Physical.Inode == 0 || len(entry.OwnerAttributes) != 0 {
			return empty, errors.New("native approval member changed original physical custody")
		}
		seen[entry.Path] = true
		if file {
			if entry.Kind != "file" || entry.Mode != 0600 && entry.Mode != 0400 || entry.Size != original.Bytes || entry.Sha256 != original.Sha256 {
				return empty, errors.New("native approval inventory changed original signed bytes")
			}
		} else if entry.Kind != "directory" || entry.Mode != 0700 || entry.Size != 0 || entry.Sha256 != "" {
			return empty, errors.New("native approval ancestor changed original custody")
		}
		result.Files = append(result.Files, durablevolume.PreparationFile{Path: entry.Path, Kind: entry.Kind, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256})
	}
	if len(seen) != len(members)+len(directories) {
		return empty, errors.New("native approval restore omitted original member or ancestor")
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	result.Census, err = json.Marshal(struct {
		Schema    string `json:"schema"`
		Scope     string `json:"scope_hash"`
		Inventory string `json:"inventory_hash"`
	}{Schema: storageNativeProducerSchema, Scope: rootObjectHash(scope), Inventory: rootObjectHash(report)})
	return result, err
}

func inspectStorageNativeApprovalRestore(ctx context.Context, root *os.File, plan durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	expected, err := planStorageNativeApprovalRestore(ctx, plan.StagingName, plan.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected, plan) {
		return nil, errors.New("native approval restore changed the original owner plan")
	}
	var scope storageNativeApprovalScope
	if err := decodePlanJson(plan.Owner.Inputs, &scope); err != nil {
		return nil, err
	}
	members := storageNativeApprovalMembers(scope.Native, report.StateRoot.Path)
	for _, member := range plan.Files {
		if member.Kind != "file" {
			continue
		}
		raw, err := readStorageNativeProducerMember(ctx, root, member)
		if err != nil {
			return nil, err
		}
		original := members[member.Path]
		if uint64(len(raw)) != original.Bytes || monitorReadDigest(raw) != original.Sha256 {
			return nil, errors.New("native restored approval differs from original signed authority")
		}
	}
	return nil, ctx.Err()
}

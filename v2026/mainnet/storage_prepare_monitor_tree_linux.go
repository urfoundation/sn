//go:build linux

// A monitor tree is a fixed collection of snapshot owners selected by exact
// original paths. It preserves directory structure without granting a generic
// restore kind permission to enroll files or discard unrelated checkpoints.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

const storageMonitorTreeKind = "mainnet-monitor-checkpoint-tree"
const storageMonitorTreeSchema = "urnetwork-monitor-checkpoint-tree-restore-v1"

// The original list keeps the one-MiB monitor profile. A separately declared
// combined owner may select its fixed larger profile for exact listed paths;
// neither a byte count nor a filename can infer a different physical owner.
type storageMonitorTreeScope struct {
	Schema    string                              `json:"schema"`
	Snapshots []string                            `json:"snapshots"`
	Profiles  []storageMonitorTreeSnapshotProfile `json:"snapshot_profiles,omitempty"`
}

type storageMonitorTreeSnapshotProfile struct {
	Path         string `json:"path"`
	Kind         string `json:"kind"`
	MaximumBytes uint64 `json:"maximum_bytes"`
}

// Build once per bounded restore admission. Repeated or unlisted overrides
// cannot change an already selected owner or borrow a sibling's authority.
func (self storageMonitorTreeScope) profiles() (map[string]storageMonitorTreeSnapshotProfile, error) {
	result := make(map[string]storageMonitorTreeSnapshotProfile, len(self.Snapshots))
	for _, path := range self.Snapshots {
		result[path] = storageMonitorTreeSnapshotProfile{Path: path, Kind: "mainnet-monitor-checkpoint", MaximumBytes: maxRpcReplyBytes}
	}
	for index, profile := range self.Profiles {
		if _, present := result[profile.Path]; !present || index > 0 && self.Profiles[index-1].Path >= profile.Path || profile.Kind != economicConservationStorageKind || profile.MaximumBytes != economicConservationStorageMaximum {
			return nil, errors.New("monitor tree profile is not an exact distinct listed combined owner")
		}
		result[profile.Path] = profile
	}
	return result, nil
}

type storageMonitorTreeHead struct {
	Path       string `json:"path"`
	PlanSha256 string `json:"plan_sha256"`
}

type storageMonitorTreeDirectory struct {
	Path     string                     `json:"path"`
	Physical durablevolume.PhysicalRoot `json:"physical"`
}

// Original checkpoint bytes remain in the hash-bound complete inventory.
// These digests bind the independently reconstructed fixed snapshot plans;
// the source census is never inferred from the replacement directory bytes.
type storageMonitorTreeCensus struct {
	Schema      string                        `json:"schema"`
	Heads       []storageMonitorTreeHead      `json:"heads"`
	Directories []storageMonitorTreeDirectory `json:"directories"`
}

func storageMonitorTreePath(path string) bool {
	// Runtime history already bounds the full absolute reference at 1024
	// bytes. Relative names retain that upper bound and the physical
	// inventory's 32-component profile, including the leaf itself.
	if path == "" || path == "." || path == ".." || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.HasPrefix(path, "../") || strings.ContainsAny(path, "\x00\n\r") || len(path) > maximumMonitorHistoryPath || strings.Count(path, "/") >= 32 {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if len(part) > 255 {
			return false
		}
	}
	name := filepath.Base(path)
	return len(name) <= 155 && !strings.HasPrefix(name, ".durable-head-")
}

func storageMonitorTreeProfile(owner durablevolume.PreparationOwner, ownerLocal bool) (storageMonitorTreeScope, error) {
	var scope storageMonitorTreeScope
	if ownerLocal || owner.Kind != storageMonitorTreeKind || owner.Purpose != "restore" || owner.RelativePath != "." || owner.RestoreCoverage != durablevolume.PreparationCompleteUnion {
		return scope, errors.New("monitor tree requires its explicit daemon complete-union restore profile")
	}
	if err := decodePlanJson(owner.Inputs, &scope); err != nil {
		return scope, err
	}
	if scope.Schema != storageMonitorTreeSchema || len(scope.Snapshots) == 0 || len(scope.Snapshots) > 2048 {
		return scope, errors.New("monitor tree schema or fixed snapshot count is invalid")
	}
	for index, path := range scope.Snapshots {
		if !storageMonitorTreePath(path) || index > 0 && scope.Snapshots[index-1] >= path {
			return scope, errors.New("monitor tree paths must be exact, bounded, distinct and sorted")
		}
	}
	_, err := scope.profiles()
	return scope, err
}

// This private view is not a replacement exported inventory. The unchanged
// core authenticates the complete original report and disjoint union. The
// existing snapshot adapter then checks its original immediate parent inode.
func storageMonitorTreeHeadPlan(ctx context.Context, stagingName, path string, report durablevolume.Inventory) (durablevolume.PreparationOwnerPlan, durablevolume.Inventory, durablehead.Spec, json.RawMessage, error) {
	return storageMonitorTreeHeadPlanProfile(ctx, stagingName, path, report, "mainnet-monitor-checkpoint", maxRpcReplyBytes)
}

// The pair is a fixed registered snapshot format, not a caller-supplied parser
// limit. Original attributes must match it during both planning and readback.
func storageMonitorTreeHeadPlanProfile(ctx context.Context, stagingName, path string, report durablevolume.Inventory, kind string, maximum int) (durablevolume.PreparationOwnerPlan, durablevolume.Inventory, durablehead.Spec, json.RawMessage, error) {
	var empty durablevolume.PreparationOwnerPlan
	if err := validateMonitorCheckpointProfile(kind, maximum); err != nil {
		return empty, durablevolume.Inventory{}, durablehead.Spec{}, nil, err
	}
	if ctx == nil || !storageMonitorTreePath(path) {
		return empty, durablevolume.Inventory{}, durablehead.Spec{}, nil, errors.New("monitor tree head requires an exact relative snapshot path")
	}
	if err := ctx.Err(); err != nil {
		return empty, durablevolume.Inventory{}, durablehead.Spec{}, nil, err
	}
	directory := filepath.Dir(path)
	parentPath := directory
	if parentPath == "." {
		parentPath = ""
	}
	view := report
	view.Entries = nil
	var parent *durablevolume.InventoryEntry
	for _, entry := range report.Entries {
		if entry.Path == parentPath {
			if parent != nil || entry.Kind != "directory" || entry.Mode != 0700 || entry.Physical == nil || entry.Physical.Device != report.PhysicalRoot.Device || entry.Physical.Inode == 0 {
				return empty, view, durablehead.Spec{}, nil, errors.Join(durablevolume.ErrIdentity, errors.New("monitor tree original parent is missing, repeated or unprotected"))
			}
			copy := entry
			copy.Path = ""
			parent = &copy
		}
	}
	if parent == nil {
		return empty, view, durablehead.Spec{}, nil, errors.Join(durablevolume.ErrIdentity, errors.New("monitor tree original parent is absent"))
	}
	view.PhysicalRoot = *parent.Physical
	view.Entries = append(view.Entries, *parent)
	for _, entry := range report.Entries {
		if entry.Path == "" || filepath.Dir(entry.Path) != directory {
			continue
		}
		entry.Path = filepath.Base(entry.Path)
		view.Entries = append(view.Entries, entry)
	}
	profile, err := json.Marshal(storageSnapshotPreparationScope{Schema: "urnetwork-snapshot-preparation-v1", Name: filepath.Base(path), MaximumBytes: int64(maximum)})
	if err != nil {
		return empty, view, durablehead.Spec{}, nil, err
	}
	owner := durablevolume.PreparationOwner{Kind: kind, RelativePath: ".", Purpose: "restore", RestoreCoverage: durablevolume.PreparationCompleteUnion, Inputs: profile}
	spec, _, err := storagePreparationSnapshotSpec(false, owner)
	if err != nil {
		return empty, view, spec, profile, err
	}
	plan, err := durablehead.PlanRestore(ctx, stagingName, owner, spec, profile, view)
	return plan, view, spec, profile, err
}

// Every structural directory is included once in this bundle, so the core's
// one-owner-per-member contract and existing parent-first publisher are kept.
func planStorageMonitorTreeRestore(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory, ownerLocal bool) (durablevolume.PreparationOwnerPlan, error) {
	var empty durablevolume.PreparationOwnerPlan
	scope, err := storageMonitorTreeProfile(owner, ownerLocal)
	if err != nil {
		return empty, err
	}
	profiles, err := scope.profiles()
	if err != nil {
		return empty, err
	}
	entries := map[string]durablevolume.InventoryEntry{}
	for _, entry := range report.Entries {
		if _, present := entries[entry.Path]; present {
			return empty, errors.New("monitor tree original inventory repeats a member")
		}
		entries[entry.Path] = entry
	}
	files := map[string]durablevolume.PreparationFile{}
	attributes := map[durablevolume.PreparationAttributeSpec]bool{}
	directories := map[string]durablevolume.PhysicalRoot{}
	census := storageMonitorTreeCensus{Schema: storageMonitorTreeSchema}
	for _, path := range scope.Snapshots {
		profile := profiles[path]
		plan, _, _, _, err := storageMonitorTreeHeadPlanProfile(ctx, name, path, report, profile.Kind, int(profile.MaximumBytes))
		if err != nil {
			return empty, err
		}
		raw, err := json.Marshal(plan)
		if err != nil {
			return empty, err
		}
		census.Heads = append(census.Heads, storageMonitorTreeHead{Path: path, PlanSha256: monitorReadDigest(raw)})
		directory := filepath.Dir(path)
		for ancestor := directory; ancestor != "."; ancestor = filepath.Dir(ancestor) {
			entry, present := entries[ancestor]
			if !present || entry.Kind != "directory" || entry.Mode != 0700 || entry.Size != 0 || entry.Sha256 != "" || entry.Physical == nil || entry.Physical.Device != report.PhysicalRoot.Device || entry.Physical.Inode == 0 {
				return empty, errors.Join(durablevolume.ErrIdentity, errors.New("monitor tree original directory ancestry differs"))
			}
			directories[ancestor] = *entry.Physical
		}
		for _, file := range plan.Files {
			file.Path = filepath.Join(directory, file.Path)
			if _, exists := files[file.Path]; exists {
				return empty, errors.New("monitor tree heads overlap an original member")
			}
			files[file.Path] = file
		}
		for _, attribute := range plan.Attributes {
			attribute.Path = filepath.Join(directory, attribute.Path)
			if attributes[attribute] {
				return empty, errors.New("monitor tree heads overlap an original checkpoint")
			}
			attributes[attribute] = true
		}
	}
	for path, physical := range directories {
		if _, found := files[path]; found {
			return empty, errors.New("monitor tree directory collides with a snapshot member")
		}
		files[path] = durablevolume.PreparationFile{Path: path, Kind: "directory", Mode: 0700}
		census.Directories = append(census.Directories, storageMonitorTreeDirectory{Path: path, Physical: physical})
	}
	sort.Slice(census.Directories, func(i, j int) bool { return census.Directories[i].Path < census.Directories[j].Path })
	result := durablevolume.PreparationOwnerPlan{Owner: owner, StagingName: name}
	for _, file := range files {
		result.Files = append(result.Files, file)
	}
	for attribute := range attributes {
		result.Attributes = append(result.Attributes, attribute)
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	sort.Slice(result.Attributes, func(i, j int) bool {
		a, b := result.Attributes[i], result.Attributes[j]
		return a.Path < b.Path || a.Path == b.Path && a.Name < b.Name
	})
	result.Census, err = json.Marshal(census)
	if err != nil {
		return empty, err
	}
	return result, ctx.Err()
}

// An inspector holds each component until it has checked the head and the
// named ancestry again. Intermediate symlinks and physical replacements are
// refused here; complete mount admission remains the preparation core's job.
type storageMonitorTreeTarget struct {
	files []*os.File
	names []string
	stats []unix.Stat_t
}

func storageMonitorTreeObservation(err error) error {
	if errors.Is(err, unix.EBADF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return errors.Join(durablevolume.ErrIdentity, err)
	}
	return mainnetDurableUnavailable("monitor tree physical observation failed", err)
}

func openStorageMonitorTreeTarget(ctx context.Context, root *os.File, directory string) (_ *storageMonitorTreeTarget, resultErr error) {
	if ctx == nil || root == nil || directory != "." && !storageMonitorTreePath(filepath.Join(directory, "head")) {
		return nil, errors.New("monitor tree inspection requires its borrowed target root")
	}
	return openStorageRestoreDirectory(ctx, root, directory)
}

// Native approvals and artifact roots use the original volume namespace;
// their copied-source reader retains the same descriptor and custody checks.
func openStorageNativeRestoreDirectory(ctx context.Context, root *os.File, directory string) (*storageMonitorTreeTarget, error) {
	if ctx == nil || root == nil || directory != "." && !storageNativeRestorePath(filepath.Join(directory, "head")) {
		return nil, errors.New("native restore inspection requires its borrowed target root")
	}
	return openStorageRestoreDirectory(ctx, root, directory)
}

// Callers select their fixed bounded namespace before borrowing the root.
// Traversal keeps real no-follow descriptors and exact original generations.
func openStorageRestoreDirectory(ctx context.Context, root *os.File, directory string) (_ *storageMonitorTreeTarget, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	self := &storageMonitorTreeTarget{files: []*os.File{root}}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	var initial unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &initial); err != nil {
		return nil, storageMonitorTreeObservation(err)
	}
	if initial.Mode&unix.S_IFMT != unix.S_IFDIR || initial.Mode&07777 != 0700 || initial.Uid != uint32(os.Geteuid()) {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("monitor tree target root is not private"))
	}
	self.stats = append(self.stats, initial)
	if directory != "." {
		for _, name := range strings.Split(directory, "/") {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			parent := self.files[len(self.files)-1]
			fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if err != nil {
				return nil, storageMonitorTreeObservation(err)
			}
			file := os.NewFile(uintptr(fd), name)
			self.files = append(self.files, file)
			self.names = append(self.names, name)
			var opened unix.Stat_t
			if err := unix.Fstat(fd, &opened); err != nil {
				return nil, storageMonitorTreeObservation(err)
			}
			if opened.Dev != initial.Dev || opened.Mode&unix.S_IFMT != unix.S_IFDIR || opened.Mode&07777 != 0700 || opened.Uid != initial.Uid {
				return nil, errors.Join(durablevolume.ErrIdentity, errors.New("monitor tree target ancestry is not private on the admitted device"))
			}
			self.stats = append(self.stats, opened)
		}
	}
	if err := self.check(ctx); err != nil {
		return nil, err
	}
	return self, nil
}

func (self *storageMonitorTreeTarget) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for index, file := range self.files {
		var current unix.Stat_t
		if err := unix.Fstat(int(file.Fd()), &current); err != nil {
			return storageMonitorTreeObservation(err)
		}
		original := self.stats[index]
		if current.Dev != original.Dev || current.Ino != original.Ino || current.Mode != original.Mode || current.Uid != original.Uid || current.Gid != original.Gid {
			return errors.Join(durablevolume.ErrIdentity, errors.New("monitor tree target descriptor changed"))
		}
		if index == 0 {
			continue
		}
		var named unix.Stat_t
		if err := unix.Fstatat(int(self.files[index-1].Fd()), self.names[index-1], &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return storageMonitorTreeObservation(err)
		}
		if named.Dev != current.Dev || named.Ino != current.Ino || named.Mode != current.Mode || named.Uid != current.Uid || named.Gid != current.Gid {
			return errors.Join(durablevolume.ErrIdentity, errors.New("monitor tree target named ancestry changed"))
		}
	}
	return ctx.Err()
}

// The declared root is borrowed. Every descendant opened by this inspector is
// joined synchronously and its close result remains part of the outcome.
func (self *storageMonitorTreeTarget) close() error {
	var result error
	for index := len(self.files) - 1; index > 0; index-- {
		result = errors.Join(result, self.files[index].Close())
	}
	self.files = self.files[:min(1, len(self.files))]
	return result
}

func inspectStorageMonitorTreeRestore(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory, ownerLocal bool) ([]durablevolume.PreparedAttribute, error) {
	expected, err := planStorageMonitorTreeRestore(ctx, owner.StagingName, owner.Owner, report, ownerLocal)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected, owner) {
		return nil, errors.New("monitor tree target plan differs from the exact original census")
	}
	scope, err := storageMonitorTreeProfile(owner.Owner, ownerLocal)
	if err != nil {
		return nil, err
	}
	profiles, err := scope.profiles()
	if err != nil {
		return nil, err
	}
	result := make([]durablevolume.PreparedAttribute, 0, len(owner.Attributes))
	for _, path := range scope.Snapshots {
		selected := profiles[path]
		plan, view, spec, profile, err := storageMonitorTreeHeadPlanProfile(ctx, owner.StagingName, path, report, selected.Kind, int(selected.MaximumBytes))
		if err != nil {
			return nil, err
		}
		directory := filepath.Dir(path)
		target, err := openStorageMonitorTreeTarget(ctx, root, directory)
		if err != nil {
			return nil, err
		}
		attributes, inspectErr := durablehead.InspectRestore(ctx, target.files[len(target.files)-1], plan, spec, profile, view)
		if err := errors.Join(inspectErr, target.check(ctx), target.close()); err != nil {
			return nil, err
		}
		for _, attribute := range attributes {
			attribute.Spec.Path = filepath.Join(directory, attribute.Spec.Path)
			result = append(result, attribute)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i].Spec, result[j].Spec
		return a.Path < b.Path || a.Path == b.Path && a.Name < b.Name
	})
	if len(result) != len(owner.Attributes) {
		return nil, errors.New("monitor tree inspection omitted an original head")
	}
	for index, attribute := range result {
		if attribute.Spec != owner.Attributes[index] {
			return nil, errors.New("monitor tree inspection changed a checkpoint destination")
		}
	}
	return result, ctx.Err()
}

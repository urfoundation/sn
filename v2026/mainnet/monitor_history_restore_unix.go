//go:build linux || darwin

// Fixed snapshot owners are selected by exact original history references.
// This shared source review never infers ownership from a filename pattern.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// This in-memory review borrows immutable original inventories and byte refs.
type monitorHistoryRestoreRootReview struct {
	request          durablevolume.PreparationRequest
	inventory        durablevolume.Inventory
	entries          map[string]durablevolume.InventoryEntry
	seen             map[string]bool
	nested           []string
	profiles         map[string]storageMonitorTreeSnapshotProfile
	admittedProfiles map[string]storageMonitorTreeSnapshotProfile
}

// An additional co-owner has exact explicit coverage; unknown files never get
// assigned by suffix or erased to make a single-role history appear complete.
func newMonitorHistoryRestoreRootReview(ctx context.Context, request durablevolume.PreparationRequest, declared map[string]durablevolume.StateRootSpec) (*monitorHistoryRestoreRootReview, error) {
	if request.Purpose != "restore" || request.Scope != "daemon" || request.RestoreSource == nil {
		return nil, errors.New("monitor history requires explicit daemon restore requests")
	}
	report, err := durablevolume.LoadPhysicalInventory(ctx, request.RestoreSource.Inventory)
	if err != nil {
		return nil, err
	}
	if report.StateRoot.Path != request.RootPath || declared[request.RootPath] != report.StateRoot || !monitorHistoryPath(request.RestoreSource.Directory) {
		return nil, errors.New("monitor history changed an original declared logical root")
	}
	self := &monitorHistoryRestoreRootReview{request: request, inventory: report, entries: map[string]durablevolume.InventoryEntry{}, seen: map[string]bool{}, profiles: map[string]storageMonitorTreeSnapshotProfile{}, admittedProfiles: map[string]storageMonitorTreeSnapshotProfile{}}
	self.request.Owners = nil
	for _, entry := range report.Entries {
		if _, present := self.entries[entry.Path]; present {
			return nil, errors.New("monitor history inventory repeats an original member")
		}
		self.entries[entry.Path] = entry
	}
	for _, owner := range request.Owners {
		if owner.RestoreCoverage != durablevolume.PreparationCompleteUnion || owner.Purpose != "restore" {
			return nil, errors.New("monitor history additional owner lacks complete retained union coverage")
		}
		if owner.Kind == "mainnet-monitor-checkpoint" || owner.Kind == economicConservationStorageKind {
			_, scope, err := storagePreparationSnapshotSpec(false, owner)
			if err != nil || self.seen[scope.Name] {
				return nil, errors.Join(errors.New("monitor history repeats an additional snapshot"), err)
			}
			self.seen[scope.Name] = true
		}
		if owner.Kind == storageMonitorTreeKind {
			scope, err := storageMonitorTreeProfile(owner, false)
			if err != nil {
				return nil, err
			}
			profiles, err := scope.profiles()
			if err != nil {
				return nil, err
			}
			for _, path := range scope.Snapshots {
				if self.seen[path] {
					return nil, errors.New("monitor history repeats an additional tree snapshot")
				}
				self.seen[path] = true
				self.nested = append(self.nested, path)
				self.profiles[path] = profiles[path]
			}
			continue
		}
		self.request.Owners = append(self.request.Owners, owner)
	}
	return self, nil
}

// Containment is component-based and must identify one original declared root.
// A nearby prefix or an overlapping root cannot silently select other custody.
func monitorHistoryRestoreRelative(root, path string) (string, bool) {
	if !monitorHistoryPath(path) || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", false
	}
	relative, err := filepath.Rel(root, path)
	return relative, err == nil && storageMonitorTreePath(relative)
}

func monitorHistoryRestoreRoot(roots []*monitorHistoryRestoreRootReview, reference monitorHistoryReference) (*monitorHistoryRestoreRootReview, error) {
	return monitorHistoryRestoreRootLimit(roots, reference, maxRpcReplyBytes)
}

// Only the original combined policy supplies the larger reference limit.
// Standalone monitor callers retain the legacy wrapper and physical format.
func monitorHistoryRestoreRootLimit(roots []*monitorHistoryRestoreRootReview, reference monitorHistoryReference, maximum uint64) (*monitorHistoryRestoreRootReview, error) {
	if err := reference.validateLimit(maximum); err != nil {
		return nil, err
	}
	var found *monitorHistoryRestoreRootReview
	for _, root := range roots {
		if _, present := monitorHistoryRestoreRelative(root.request.RootPath, reference.Path); !present {
			continue
		}
		if found != nil {
			return nil, errors.New("monitor history reference has overlapping declared roots")
		}
		found = root
	}
	if found == nil {
		return nil, errors.New("monitor history cohort omits an original reference root")
	}
	return found, nil
}

// Only the exact signed-history reference selects a new fixed snapshot owner.
func (self *monitorHistoryRestoreRootReview) read(ctx context.Context, reference monitorHistoryReference) ([]byte, error) {
	return self.readProfile(ctx, reference, "mainnet-monitor-checkpoint", maxRpcReplyBytes)
}

// The fixed owner profile is authenticated against original physical metadata
// before copied bytes are decoded. Larger parsers cannot upgrade legacy heads.
func (self *monitorHistoryRestoreRootReview) readProfile(ctx context.Context, reference monitorHistoryReference, kind string, maximum int) ([]byte, error) {
	if err := validateMonitorCheckpointProfile(kind, maximum); err != nil {
		return nil, err
	}
	if err := reference.validateLimit(uint64(maximum)); err != nil {
		return nil, err
	}
	name, contained := monitorHistoryRestoreRelative(self.request.RootPath, reference.Path)
	if !contained {
		return nil, errors.New("monitor history reference changed its original root")
	}
	entry, present := self.entries[name]
	if !present || entry.Kind != "file" || entry.Size != reference.Bytes || entry.Sha256 != reference.Sha256 || self.seen[name] {
		return nil, errors.New("monitor history inventory omits, repeats or changes an original member")
	}
	inputs, err := json.Marshal(storageSnapshotPreparationScope{Schema: "urnetwork-snapshot-preparation-v1", Name: filepath.Base(name), MaximumBytes: int64(maximum)})
	if err != nil {
		return nil, err
	}
	owner := durablevolume.PreparationOwner{Kind: kind, RelativePath: ".", Purpose: "restore", RestoreCoverage: durablevolume.PreparationCompleteUnion, Inputs: inputs}
	spec, _, err := storagePreparationSnapshotSpec(false, owner)
	if err != nil {
		return nil, err
	}
	if filepath.Dir(name) == "." {
		if _, err := durablehead.PlanRestore(ctx, "monitor-history-review", owner, spec, inputs, self.inventory); err != nil {
			return nil, err
		}
	} else {
		if _, _, _, _, err := storageMonitorTreeHeadPlanProfile(ctx, "monitor-history-review", name, self.inventory, kind, maximum); err != nil {
			return nil, err
		}
	}
	raw, err := readBootstrapChainInput(ctx, planFileReference{Path: filepath.Join(self.request.RestoreSource.Directory, name), Sha256: reference.Sha256}, maximum)
	if err != nil {
		return nil, fmt.Errorf("monitor history copied member read: %w", err)
	}
	if uint64(len(raw)) != reference.Bytes {
		return nil, errors.New("monitor history copied bytes differ from retained reference")
	}
	self.seen[name] = true
	self.admittedProfiles[name] = storageMonitorTreeSnapshotProfile{Path: name, Kind: kind, MaximumBytes: uint64(maximum)}
	if filepath.Dir(name) == "." {
		self.request.Owners = append(self.request.Owners, owner)
	} else {
		self.nested = append(self.nested, name)
		self.profiles[name] = storageMonitorTreeSnapshotProfile{Path: name, Kind: kind, MaximumBytes: uint64(maximum)}
	}
	return raw, nil
}

// Lazy restore validation can revisit an already admitted original without
// creating a second coverage owner. Only descriptors stay resident; bytes are
// reread under the same copied directory, inventory digest and fixed profile.
func (self *monitorHistoryRestoreRootReview) rereadProfile(ctx context.Context, reference monitorHistoryReference, kind string, maximum int) ([]byte, error) {
	if err := errors.Join(ctx.Err(), validateMonitorCheckpointProfile(kind, maximum), reference.validateLimit(uint64(maximum))); err != nil {
		return nil, err
	}
	name, contained := monitorHistoryRestoreRelative(self.request.RootPath, reference.Path)
	profile, admitted := self.admittedProfiles[name]
	entry, present := self.entries[name]
	if !contained || !admitted || !self.seen[name] || profile.Kind != kind || profile.MaximumBytes != uint64(maximum) || !present || entry.Kind != "file" || entry.Size != reference.Bytes || entry.Sha256 != reference.Sha256 {
		return nil, errors.New("monitor history reread requires the exact already admitted original profile")
	}
	raw, err := readBootstrapChainInput(ctx, planFileReference{Path: filepath.Join(self.request.RestoreSource.Directory, name), Sha256: reference.Sha256}, maximum)
	if err != nil {
		return nil, fmt.Errorf("monitor history copied member reread: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uint64(len(raw)) != reference.Bytes {
		return nil, errors.New("monitor history reread bytes differ from retained reference")
	}
	return raw, nil
}

// Shared ancestors are carried once by one explicit fixed tree owner. Each
// checkpoint still has its original per-file grammar and physical head. This
// final review occurs before any staging or target publication.
func (self *monitorHistoryRestoreRootReview) finish(ctx context.Context) error {
	if len(self.nested) != 0 {
		sort.Strings(self.nested)
		scope := storageMonitorTreeScope{Schema: storageMonitorTreeSchema, Snapshots: self.nested}
		for _, path := range self.nested {
			profile, present := self.profiles[path]
			if !present {
				return errors.New("monitor history lost an original nested snapshot profile")
			}
			if profile.Kind != "mainnet-monitor-checkpoint" {
				scope.Profiles = append(scope.Profiles, profile)
			}
		}
		inputs, err := json.Marshal(scope)
		if err != nil {
			return err
		}
		owner := durablevolume.PreparationOwner{Kind: storageMonitorTreeKind, RelativePath: ".", Purpose: "restore", RestoreCoverage: durablevolume.PreparationCompleteUnion, Inputs: inputs}
		if _, err := planStorageMonitorTreeRestore(ctx, "monitor-history-tree-review", owner, self.inventory, false); err != nil {
			return err
		}
		self.request.Owners = append(self.request.Owners, owner)
		self.nested = nil
		self.profiles = nil
	}
	return validateMonitorHistoryRestoreCapacity(self.request, self.inventory)
}

// Read-only declaration admission keeps original root generations bound even
// while replacement targets exist at the same independently approved paths.
func monitorHistoryRestoreDeclaration(ctx context.Context) (durablevolume.Reference, map[string]durablevolume.StateRootSpec, error) {
	reference, present := durablevolume.ReferenceFromContext(ctx)
	if !present {
		return reference, nil, errors.New("monitor history restore declaration is absent")
	}
	declaration, err := durablevolume.Load(reference)
	if err != nil {
		return reference, nil, err
	}
	declared := map[string]durablevolume.StateRootSpec{}
	for _, volume := range declaration.Volumes {
		for _, root := range volume.StateRoots {
			declared[root.Path] = root
		}
	}
	return reference, declared, nil
}

// Counts, actual encoded bytes and two-times reserve remain independent. A
// larger owner count does not silently increase any per-root physical budget.
func validateMonitorHistoryRestoreCapacity(preparation durablevolume.PreparationRequest, report durablevolume.Inventory) error {
	// The complete inventory includes independently declared co-owners. Their
	// exact semantic coverage is enforced again by storage-prepare before effects.
	limits := preparation.Limits
	if limits.MaxEntries < 2*uint64(len(report.Entries)) || limits.MaxBytes < 2*report.TotalBytes ||
		limits.MaxOwnerAttributes < 2*report.TotalOwnerAttributes || limits.MaxOwnerAttributeBytes < 2*report.TotalOwnerAttributes*4096 ||
		preparation.MinAvailableBytes < 2*(report.TotalBytes+report.TotalOwnerAttributes*4096) || preparation.MinAvailableInodes < 2*uint64(len(report.Entries)) {
		return errors.New("monitor history restore requires explicit two-times complete-namespace byte, head and inode reserves")
	}
	if len(preparation.Owners) > 32 || limits.MaxOwnerAttributes > 128 || limits.MaxOwnerAttributeBytes > 128*4096 {
		if preparation.CapacityProfile != "urnetwork-preparation-many-owners-v1" {
			return errors.New("monitor history restore requires the explicit many-owner preparation capacity profile")
		}
	}
	encoded, err := json.Marshal(preparation)
	if err != nil || len(encoded)+1 > maxRpcReplyBytes {
		return errors.Join(errors.New("monitor history restore request exceeds its complete serialized byte bound"), err)
	}
	return nil
}

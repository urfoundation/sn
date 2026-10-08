// Automatic principal retention runs under the existing checkpoint writer,
// between native attempts. Each preprovisioned slot stores the exact old head
// before the compacted head can be published; no stopped-writer fence is invented.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// This optional operational selection grants no cause or spending authority.
// Slots and archive/index capacities remain finite; exhaustion preserves the
// last original checkpoint. Nil keeps the legacy policy hash and manual behavior.
type economicConservationPrincipalRetentionPolicy struct {
	Schema       string   `json:"schema"`
	TriggerFacts uint64   `json:"trigger_principal_facts"`
	ArchivePaths []string `json:"preprovisioned_archive_paths"`
}

// Leave at least half the active fact budget available for the next bounded
// native batch and sibling domains. An individually oversized batch still holds.
func (self *economicConservationPrincipalRetentionPolicy) validate(policy economicConservationPolicy) error {
	if self == nil {
		return nil
	}
	if self.Schema != economicPrincipalRetentionSchema || self.TriggerFacts == 0 || self.TriggerFacts > policy.MaximumFacts/2 || len(self.ArchivePaths) == 0 || uint64(len(self.ArchivePaths)) > policy.initialResources().ArchiveSegments || policy.Native.Observation.Execution == nil || policy.Native.Observation.Execution.Principal == nil || policy.Native.Observation.Execution.Principal.Effects == nil {
		return errors.New("automatic principal retention requires bounded slots, early threshold and original principal authority")
	}
	for index, path := range self.ArchivePaths {
		if !monitorHistoryPath(path) {
			return errors.New("automatic principal archive path is not a bounded absolute owner")
		}
		for _, prior := range self.ArchivePaths[:index] {
			if monitorHistoryPathsAlias(prior, path) {
				return errors.New("automatic principal archive slots alias an original owner")
			}
		}
	}
	return nil
}

// Runtime paths are unavailable at policy parsing, so reject aliases before
// starting any observation or opening a prospective archive writer.
func (self *economicConservationPrincipalRetentionPolicy) validatePaths(paths ...string) error {
	if self == nil {
		return nil
	}
	for _, archive := range self.ArchivePaths {
		for _, path := range paths {
			if path != "" && monitorHistoryPathsAlias(archive, path) {
				return errors.New("automatic principal archive aliases a live input or checkpoint")
			}
		}
	}
	return nil
}

// Only actual retained principal records trigger compaction. Other unresolved
// obligations retain their existing limits and cannot be retired by this selector.
func (self *economicConservationPrincipalRetentionPolicy) due(policy economicConservationPolicy, state *economicConservationState) bool {
	return self != nil && len(state.PrincipalExecutions) != 0 && (state.principalEffectFacts() >= self.TriggerFacts || state.facts() >= policy.MaximumFacts/2 || state.Native.CapacityRemaining <= policy.Native.BatchBlocks)
}

// The existing checkpoint remains the recovery authority until the last
// publication. An orphaned slot from an interrupted attempt is reusable only
// when it contains those identical original bytes, never a reminted snapshot.
func maintainEconomicPrincipalArchive(ctx context.Context, policy economicConservationPolicy, owner *monitorCheckpointStore, state *economicConservationState, view *economicConservationArchiveView, hooks monitorServiceHooks) (_ *economicConservationState, _ *economicConservationArchiveView, resultErr error) {
	operating, err := state.operatingPolicy(policy)
	if err != nil {
		return state, view, err
	}
	if !policy.PrincipalRetention.due(operating, state) {
		return state, view, nil
	}
	if err := errors.Join(policy.PrincipalRetention.validate(policy), policy.PrincipalRetention.validatePaths(owner.path), owner.requireOwner(), view.check()); err != nil {
		return state, view, err
	}
	lifecycle := ctx
	ctx, cancel := context.WithTimeout(ctx, time.Duration(monitorEconomicReadSeconds(operating.ReadBudgetSeconds))*time.Second)
	defer cancel()
	used := map[string]bool{}
	paths := []string{owner.path}
	retainedBytes := uint64(0)
	if state.Archive != nil {
		for _, reference := range state.Archive.Segments {
			used[reference.Path], used[reference.Path+".lock"] = true, true
			paths = append(paths, reference.Path)
			retainedBytes += reference.Bytes
		}
	}
	path := ""
	for _, candidate := range policy.PrincipalRetention.ArchivePaths {
		if !used[candidate] && !used[candidate+".lock"] {
			path = candidate
			break
		}
	}
	if path == "" {
		return state, view, errors.Join(errMonitorEconomicCapacity, errors.New("automatic principal retention exhausted its preprovisioned slots"))
	}
	declaration, present := durablevolume.ReferenceFromContext(ctx)
	if !present {
		return state, view, errors.New("automatic principal retention lost its durable volume owner")
	}
	paths = append(paths, path)
	if err := monitorHistoryDeclarationForecast(declaration, paths, 2*(retainedBytes+2*policy.storageMaximum()), 4*uint64(len(paths))); err != nil {
		return state, view, err
	}
	raw, err := owner.directory.read(filepath.Base(owner.path), int(policy.storageMaximum()), true)
	if err != nil {
		return state, view, err
	}
	original, err := decodeEconomicConservation(ctx, raw, policy)
	if err != nil {
		return state, view, err
	}
	if original.ContentHash != state.ContentHash || original.ContentHash != state.hash() {
		return state, view, errors.New("automatic principal retention changed the live original checkpoint")
	}
	original.archiveView = view
	reference := monitorHistoryReference{Path: path, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}
	next, err := compactEconomicConservationWithPrincipalRetention(ctx, policy, original, reference, nil, false, nil, true)
	if err != nil {
		return state, view, err
	}
	archive, err := policy.openHistorySnapshot(ctx, path, true)
	if err != nil {
		return state, view, err
	}
	defer func() { resultErr = errors.Join(resultErr, archive.close()) }()
	retained, archived, err := archive.read()
	if err != nil {
		return state, view, err
	}
	if archived && !bytes.Equal(retained, raw) {
		return state, view, errors.New("automatic principal slot contains different original bytes")
	}
	if err := errors.Join(ctx.Err(), owner.requireOwner(), view.check(), archive.check()); err != nil {
		return state, view, err
	}
	if !archived {
		var syncDirectory func(*os.File) error
		if hooks.syncDirectory != nil {
			syncDirectory = func(file *os.File) error { return hooks.syncDirectory(economicConservationRole, "archive", file) }
		}
		if err := archive.publish(raw, syncDirectory); err != nil {
			return state, view, err
		}
	}
	retained, archived, err = archive.read()
	if err != nil || !archived || !bytes.Equal(retained, raw) {
		return state, view, errors.Join(err, errors.New("automatic principal archive did not retain the exact original"))
	}
	if err := archive.close(); err != nil {
		return state, view, err
	}
	// Authenticate the complete candidate in a new private index. A failed
	// read/capacity/custody check cannot mutate the currently published view.
	nextView, err := readEconomicConservationArchive(ctx, policy, next, hooks, func(readContext context.Context, reference monitorHistoryReference) (*monitorHistorySnapshot, []byte, error) {
		if err := readContext.Err(); err != nil {
			return nil, nil, err
		}
		// Reader custody lasts for the command, beyond this maintenance
		// attempt's deadline. Admission itself still observes readContext.
		return policy.openHistoryReader(lifecycle, reference)
	}, nil)
	if err != nil {
		return state, view, err
	}
	next.archiveView = nextView
	next.principalProvisional = nil
	if err := saveEconomicConservation(ctx, owner, policy, next); err != nil {
		return state, view, errors.Join(err, nextView.close())
	}
	return next, nextView, view.close()
}

//go:build linux || darwin

// Nested original paths remain unchanged through public request/plan/apply and
// actual EVM restart. No fixture moves a signed reference after its creation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func TestMonitorNativeRestoreNestedCohortRetainsPendingOutcome(t *testing.T) {
	f := newMonitorNativeCohortFixtureLayout(t, 1, true)
	original := f.record.State.Archive.Segments[0]
	if filepath.Dir(original.Path) == f.sources[1].root {
		t.Fatal("native fixture did not retain a descendant archive")
	}
	f.native.ctx = f.apply(t, f.plan(t), true)
	if record := f.native.record(t); !reflect.DeepEqual(record, f.record) || record.State.Archive.Segments[0] != original {
		t.Fatal("native nested restore changed original authority or economics")
	}
	run := f.native.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 102 || event.State.BatchCount != 2 || event.State.PendingThrough != nil || event.State.ArchiveSegments != 1 || monitorEconomicTestFee(event) != "12" {
		t.Fatal("native descendant restore lost its original pending outcome", event)
	}
	run.stop(t)
}

func TestMonitorEvmRestoreNestedSingleRootRetainsPendingOutcome(t *testing.T) {
	f := newMonitorEvmRestoreFixtureLayout(t, 1, 1, true)
	original := f.record.State.Archive.Segments[0]
	if filepath.Dir(original.Path) == f.sources[0].root {
		t.Fatal("fixture did not exercise an original descendant snapshot")
	}
	f.apply(t, f.plan(t), true)
	if f.evm.record(t).State.Archive.Segments[0] != original {
		t.Fatal("nested restoration relocated original signed history")
	}
	monitorEvmRestorePendingContinues(t, f)
}

func TestMonitorEvmRestoreNestedCohortRetainsParentAndPendingOutcome(t *testing.T) {
	f := newMonitorEvmRestoreFixtureLayout(t, 2, 1, true)
	before := f.record
	f.apply(t, f.plan(t), true)
	if !reflect.DeepEqual(f.evm.record(t), before) {
		t.Fatal("nested cohort rewrote original approval or financial state")
	}
	monitorEvmRestorePendingContinues(t, f)
}

// A complete valid plan comes first. Losing the later copied parent or adding
// an unknown sibling must refuse before any earlier target is reserved. Only
// restoring the exact held original source permits the same plan to continue.
func TestMonitorEvmRestoreNestedCohortRefusesChangedSourceBeforeEffects(t *testing.T) {
	f := newMonitorEvmRestoreFixtureLayout(t, 2, 1, true)
	reference := f.plan(t)
	parent := filepath.Join(f.targets[1].archive, "history", "epoch")
	held := filepath.Join(f.targets[1].target.metadata, "held-original-parent")
	args := []string{"storage-prepare", "cohort-apply", "--cohort", reference.Path, "--cohort-sha256", reference.Sha256}
	for _, fault := range []string{"absent-parent", "symlink-parent", "unknown-member"} {
		if fault == "unknown-member" {
			if err := os.WriteFile(filepath.Join(parent, "unknown.json"), []byte("unknown retained owner\n"), 0600); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Rename(parent, held); err != nil {
				t.Fatal(err)
			}
			if fault == "symlink-parent" {
				if err := os.Symlink(held, parent); err != nil {
					t.Fatal(err)
				}
			}
		}
		var output, diagnostic bytes.Buffer
		if code := runMain(f.evm.ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("changed original descendant custody was admitted", fault, code, diagnostic.String())
		}
		f.unmodifiedTargets(t)
		if fault == "unknown-member" {
			if err := os.Remove(filepath.Join(parent, "unknown.json")); err != nil {
				t.Fatal(err)
			}
		} else {
			if fault == "symlink-parent" {
				if err := os.Remove(parent); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Rename(held, parent); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.apply(t, reference, false)
	monitorEvmRestorePendingContinues(t, f)
}

// Independent snapshot heads authenticate the original parent inode. The new
// physical parent can only be derived after that old relation is validated.
func TestStoragePreparationMonitorTreeBindsOriginalParentAndExactMembers(t *testing.T) {
	f := newMonitorEvmRestoreFixtureLayout(t, 1, 2, true)
	request, err := buildMonitorEvmRestoreRequest(f.evm.ctx, monitorEvmRestoreRequest{Schema: monitorEvmRestoreRequestSchema, Expected: f.request.Expected, Policy: f.request.Policy, Original: f.request.Original, Preparation: f.request.Preparations[0]})
	if err != nil {
		t.Fatal("valid nested producer failed before the fault matrix", err)
	}
	var owner durablevolume.PreparationOwner
	for _, current := range request.Owners {
		if current.Kind == storageMonitorTreeKind {
			owner = current
		}
	}
	if owner.Kind == "" {
		t.Fatal("public nested request omitted its fixed tree owner")
	}
	report, err := durablevolume.LoadPhysicalInventory(f.evm.ctx, request.RestoreSource.Inventory)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planStorageMonitorTreeRestore(f.evm.ctx, "tree-review", owner, report, false)
	if err != nil {
		t.Fatal("valid original fixed tree plan failed", err)
	}
	counts := map[string]int{}
	for _, file := range plan.Files {
		counts[file.Path]++
	}
	for _, name := range []string{"history", "history/epoch", "history/epoch/a000.json", "history/epoch/a001.json"} {
		if counts[name] != 1 {
			t.Fatal("shared parent or exact signed member was not covered once", name, counts)
		}
	}
	for _, fault := range []string{"parent-inode", "missing-parent", "duplicate-path", "escape", "owner-local", "fresh"} {
		encoded, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		var changed durablevolume.Inventory
		if err := json.Unmarshal(encoded, &changed); err != nil {
			t.Fatal(err)
		}
		current := owner
		local := false
		switch fault {
		case "parent-inode", "missing-parent":
			found := false
			for index := range changed.Entries {
				if changed.Entries[index].Path != "history/epoch" {
					continue
				}
				found = true
				if fault == "parent-inode" {
					changed.Entries[index].Physical.Inode++
				} else {
					changed.Entries = append(changed.Entries[:index], changed.Entries[index+1:]...)
				}
				break
			}
			if !found {
				t.Fatal("fixture lacks selected original parent")
			}
		case "duplicate-path", "escape":
			scope, err := storageMonitorTreeProfile(current, false)
			if err != nil {
				t.Fatal(err)
			}
			if fault == "duplicate-path" {
				scope.Snapshots = append(scope.Snapshots, scope.Snapshots[0])
			} else {
				scope.Snapshots[0] = "../foreign.json"
			}
			current.Inputs, err = json.Marshal(scope)
			if err != nil {
				t.Fatal(err)
			}
		case "owner-local":
			local = true
		case "fresh":
			current.Purpose = "fresh"
		}
		if _, err := planStorageMonitorTreeRestore(f.evm.ctx, "tree-review", current, changed, local); err == nil {
			t.Fatal("fixed monitor tree acquired unreviewed original authority", fault)
		}
		f.unmodifiedTargets(t)
	}
}

func TestMonitorHistoryRestoreRootUsesExactUnambiguousContainment(t *testing.T) {
	root := &monitorHistoryRestoreRootReview{request: durablevolume.PreparationRequest{RootPath: "/reviewed/root"}}
	reference := monitorHistoryReference{Path: "/reviewed/root/history/a.json", Sha256: "sha256:" + strings.Repeat("a", 64), Bytes: 1}
	if found, err := monitorHistoryRestoreRoot([]*monitorHistoryRestoreRootReview{root}, reference); err != nil || found != root {
		t.Fatal("exact original descendant root was not selected", err)
	}
	for _, path := range []string{"/reviewed/rooted/history/a.json", "/reviewed/root/../foreign.json", "/reviewed/root"} {
		changed := reference
		changed.Path = path
		if _, err := monitorHistoryRestoreRoot([]*monitorHistoryRestoreRootReview{root}, changed); err == nil {
			t.Fatal("path alias selected unreviewed custody", path)
		}
	}
	overlap := &monitorHistoryRestoreRootReview{request: durablevolume.PreparationRequest{RootPath: "/reviewed/root/history"}}
	if _, err := monitorHistoryRestoreRoot([]*monitorHistoryRestoreRootReview{root, overlap}, reference); err == nil {
		t.Fatal("overlapping declared roots chose one arbitrarily")
	}
}

// Closed descriptors are observation/caller failures, not positive evidence of
// different custody. A replaced named descendant is an actual identity loss.
func TestStoragePreparationMonitorTreeTargetKeepsReadAndIdentityCauses(t *testing.T) {
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(path, "history")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if owner, err := openStorageMonitorTreeTarget(ctx, root, "history"); owner != nil || !errors.Is(err, context.Canceled) || errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("canceled nested read claimed changed identity", owner, err)
	}
	closed, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if owner, err := openStorageMonitorTreeTarget(t.Context(), closed, "history"); owner != nil || !errors.Is(err, syscall.EBADF) || errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, durablevolume.ErrUnavailable) {
		t.Fatal("closed borrowed descriptor claimed changed physical custody", owner, err)
	}
	owner, err := openStorageMonitorTreeTarget(t.Context(), root, "history")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(child, filepath.Join(path, "original-held")); err != nil {
		t.Fatal(errors.Join(err, owner.close()))
	}
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(errors.Join(err, owner.close()))
	}
	if err := errors.Join(owner.check(t.Context()), owner.close()); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("replaced named descendant retained original authority", err)
	}
}

//go:build linux

// Copied namespaces retain original cold evidence through the actual restore
// reader without borrowing live snapshot custody or repeating owner admission.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The original Wasm and vault receipt force capture reconciliation to request
// a cold interval. Both logical references are served from separate copied files.
func TestEconomicConservationRestoreCopiedPrincipalOriginalsRemainUnknown(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture-unclassified", false, nil)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	f.request.RetainPrincipalOriginals = true
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	before := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if len(state.PrincipalExecutions) != 0 || state.Archive.PrincipalRetained.UnresolvedBlocks != 1 || len(state.Captures) != 1 || state.Captures[0].Event.CaptureIdentity == nil || state.Captures[0].PrincipalEffects != nil {
		t.Fatal("restore fixture did not retain an unresolved original cold capture")
	}
	raw, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	original := monitorHistoryReference{Path: f.source.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}
	copied := t.TempDir()
	paths := map[monitorHistoryReference]string{}
	for index, reference := range append([]monitorHistoryReference{original}, state.Archive.Segments...) {
		raw, err := os.ReadFile(reference.Path)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(copied, fmt.Sprintf("original-%d.json", index))
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		paths[reference] = path
	}
	for _, fault := range []string{"", "changed-original", "cancel-after-reread"} {
		ctx, cancel := context.WithCancel(f.ctx)
		admitted := map[monitorHistoryReference]bool{}
		rereads := 0
		err := validateEconomicConservationRestoreHistory(ctx, f.source.policy, original, func(reference monitorHistoryReference) ([]byte, error) {
			path, present := paths[reference]
			if !present || admitted[reference] {
				return nil, errors.New("copied original admission repeated or changed its reference")
			}
			raw, err := os.ReadFile(path)
			if err == nil {
				admitted[reference] = true
			}
			return raw, err
		}, func(_ context.Context, reference monitorHistoryReference) ([]byte, error) {
			if !admitted[reference] || reference == original {
				return nil, errors.New("cold reread lacks its original segment admission")
			}
			rereads++
			raw, err := os.ReadFile(paths[reference])
			if fault == "changed-original" {
				raw = append(raw, ' ')
			}
			if fault == "cancel-after-reread" {
				cancel()
			}
			return raw, err
		}, monitorServiceHooks{})
		cancel()
		if len(admitted) != len(paths) || rereads == 0 || fault == "" && err != nil || fault == "changed-original" && (err == nil || !strings.Contains(err.Error(), "changed original checkpoint bytes")) || fault == "cancel-after-reread" && !errors.Is(err, context.Canceled) {
			t.Fatal("copied restore lost the original cold interval or accepted changed custody", fault, len(admitted), rereads, err)
		}
	}
	after := f.sample(t, monitorServiceHooks{})
	if after.NativeCursor != before.NativeCursor || after.CausallyJoinedCaptures != 0 || after.PrincipalEffects.Current || after.PrincipalEffects.Retained.ChainHash != before.PrincipalEffects.Retained.ChainHash || f.source.state(t).Captures[0].PrincipalEffects != nil {
		t.Fatal("copied restore changed live original state or resolved unknown causes", after)
	}
}

// The physical exporter supplies the inventory and copied namespace. Only
// first admission grows coverage; reread preserves its exact fixed profile.
func TestEconomicConservationRestorePrincipalRereadKeepsOriginalCoverage(t *testing.T) {
	f := newEconomicConservationRestoreFixture(t, 1, 1, nil)
	_, declared, err := monitorHistoryRestoreDeclaration(f.archive.ctx)
	if err != nil {
		t.Fatal(err)
	}
	root, err := newMonitorHistoryRestoreRootReview(f.archive.ctx, f.request.Preparations[0], declared)
	if err != nil {
		t.Fatal(err)
	}
	reference := f.request.Original
	kind, maximum := f.request.Policy.storageKind(), int(f.request.Policy.storageMaximum())
	if _, err := root.rereadProfile(f.archive.ctx, reference, kind, maximum); err == nil {
		t.Fatal("unadmitted copied member acquired reread authority")
	}
	original, err := root.readProfile(f.archive.ctx, reference, kind, maximum)
	if err != nil {
		t.Fatal(err)
	}
	owners, nested, seen := len(root.request.Owners), len(root.nested), len(root.seen)
	for index := 0; index < 3; index++ {
		raw, err := root.rereadProfile(f.archive.ctx, reference, kind, maximum)
		if err != nil || !bytes.Equal(raw, original) || len(root.request.Owners) != owners || len(root.nested) != nested || len(root.seen) != seen {
			t.Fatal("copied reread duplicated owner coverage or changed bytes", index, err)
		}
	}
	if _, err := root.readProfile(f.archive.ctx, reference, kind, maximum); err == nil {
		t.Fatal("reread relaxed the original duplicate admission census")
	}
	changed := reference
	changed.Sha256 = monitorReadDigest([]byte("synthetic different copied original"))
	if _, err := root.rereadProfile(f.archive.ctx, changed, kind, maximum); err == nil {
		t.Fatal("reread accepted a substituted original digest")
	}
	name, _ := monitorHistoryRestoreRelative(root.request.RootPath, reference.Path)
	profile := root.admittedProfiles[name]
	profile.MaximumBytes++
	root.admittedProfiles[name] = profile
	if _, err := root.rereadProfile(f.archive.ctx, reference, kind, maximum); err == nil {
		t.Fatal("reread replaced the fixed original physical profile")
	}
	profile.MaximumBytes--
	root.admittedProfiles[name] = profile
	if err := os.WriteFile(filepath.Join(root.request.RestoreSource.Directory, name), append(original, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := root.rereadProfile(f.archive.ctx, reference, kind, maximum); err == nil || len(root.request.Owners) != owners || len(root.seen) != seen {
		t.Fatal("changed copied bytes survived the exact original inventory", err)
	}
	f.unchanged(t)
}

// A two-segment copied view has the same bounded cache and cancellation fence,
// while live work must reacquire actual original physical snapshot owners.
func TestEconomicPrincipalRetentionCopiedViewCannotBecomeLiveCustody(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	view := newEconomicConservationArchiveView(economicConservationResources{IndexEntries: 64, IndexBytes: 4 * 1024 * 1024})
	view.copiedSourceOnly, view.copiedSourceContext = true, ctx
	t.Cleanup(func() {
		if err := view.close(); err != nil {
			t.Error(err)
		}
	})
	originals := map[monitorHistoryReference][]byte{}
	parent := economicEmissionBoundary{Number: 90, Hash: fmt.Sprintf("0x%064x", 90)}
	var head *economicConservationPrincipalRetained
	for index := 0; index < 2; index++ {
		value := economicPrincipalRetentionTestValue(parent, index == 0)
		original := &economicConservationState{PrincipalExecutions: []economicConservationPrincipalExecution{value}}
		raw, reference := economicPrincipalRetentionTestSegment(t, original, filepath.Join(t.TempDir(), "original.json"))
		var err error
		head, err = mergeEconomicPrincipalRetained(head, value)
		if err != nil {
			t.Fatal(err)
		}
		compacted := &economicConservationState{Archive: &economicConservationArchive{Segments: []monitorHistoryReference{reference}, PrincipalRetained: head, PrincipalRetentions: []monitorHistoryReference{reference}}}
		if err := view.indexPrincipalRetentions(original, compacted); err != nil {
			t.Fatal(err)
		}
		originals[reference] = raw
		parent = value.Projection.Boundary
	}
	reads := 0
	view.copiedPrincipalRead = func(_ context.Context, reference monitorHistoryReference) ([]byte, error) {
		reads++
		raw, present := originals[reference]
		if !present {
			return nil, os.ErrNotExist
		}
		return raw, nil
	}
	for _, number := range []uint64{91, 92, 91} {
		value, found, err := view.principalExecution(ctx, number)
		if err != nil || !found || value.Projection.Boundary.Number != number || len(view.principalExecutions) != 0 || len(view.principalExecutionHashes) != 0 || len(view.principalCache.Values) != 1 {
			t.Fatal("copied cold lookup lost a segment or retained an unbounded payload map", number, found, err)
		}
	}
	if reads != 3 || len(view.owners) != 0 || !reflect.DeepEqual(view.principalRetained, head) {
		t.Fatal("copied cache substituted physical custody or changed original lineage", reads)
	}
	state := &economicConservationState{archiveView: view}
	if err := saveEconomicConservation(ctx, nil, economicConservationPolicy{}, state); err == nil || !strings.Contains(err.Error(), "copied-source") {
		t.Fatal("copied evidence acquired checkpoint publication authority", err)
	}
	if _, _, _, err := sampleEconomicConservation(ctx, economicConservationPolicy{}, state, nil, nil, time.Time{}, monitorServiceHooks{}); err == nil || !strings.Contains(err.Error(), "copied-source") {
		t.Fatal("copied evidence acquired a new public source read", err)
	}
	worker := newEconomicConservationNativeWorker(ctx, economicConservationPolicy{}, nil, monitorServiceHooks{})
	if err := worker.start(state); err == nil || !strings.Contains(err.Error(), "copied-source") || worker.active {
		t.Fatal("copied evidence started a live native worker", err)
	}
	view.principalCache = nil
	view.copiedSourceOnly = false
	if _, found, err := view.principalExecution(ctx, 91); err == nil || found || reads != 3 {
		t.Fatal("live missing custody fell back to copied originals", found, err, reads)
	}
	view.copiedSourceOnly = true
	cancel()
	if _, found, err := view.principalExecution(t.Context(), 91); !errors.Is(err, context.Canceled) || found || reads != 3 {
		t.Fatal("expired copied-source review retained lookup authority", found, err, reads)
	}
}

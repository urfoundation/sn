//go:build linux

// The combined adapter restores a real large original witness and the exact
// producer namespace that made it. Independent fixed owners retain their own
// profile; a large neighboring checkpoint cannot authorize another format.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The principal fixture has already admitted its original physical roots.
// These are restore templates only: no fresh request is applied to those roots
// and no declaration, original approval, job or checkpoint is re-enrolled.
func economicConservationLargeRestoreSource(t *testing.T, f *economicConservationArchiveFixture, root string, volume durablevolume.VolumeSpec) *storagePreparationCommandFixture {
	t.Helper()
	metadata := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, metadata)
	request := durablevolume.PreparationRequest{
		Schema: durablevolume.PreparationRequestSchema, Purpose: "restore", Scope: "daemon",
		MountPath: volume.MountPath, FilesystemUuid: volume.FilesystemUuid, FilesystemType: volume.FilesystemType,
		MinAvailableBytes: 256 * 1024 * 1024, MinAvailableInodes: 8192, RootPath: root,
		Limits: durablevolume.PreparationLimits{MaxEntries: 8192, MaxBytes: 256 * 1024 * 1024, MaxDepth: 8, MaxOwnerAttributes: 128, MaxOwnerAttributeBytes: 128 * 4096, MaxPlanBytes: 8 * 1024 * 1024},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(metadata, "restore-template.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return &storagePreparationCommandFixture{ctx: f.ctx, storage: f.storage, root: root, metadata: metadata, requestPath: path, requestHash: monitorReadDigest(raw)}
}

func economicConservationTestCompleteLargeRestore(t *testing.T, cold bool) {
	t.Helper()
	f, producer := newEconomicConservationPrincipalFixture(t, "yuma-capacity-2048", true, nil)
	// The ordinary fixture places its unsigned command input beside its head.
	// Keep that input outside the exported owner namespace before first use.
	originalPolicyPath := f.source.path
	f.source.path = filepath.Join(f.metadata, "original-policy.json")
	f.source.writePolicy(t)
	if err := os.Remove(originalPolicyPath); err != nil {
		t.Fatal(err)
	}
	first := f.sample(t, monitorServiceHooks{})
	original, err := os.ReadFile(f.source.checkpoint)
	if err != nil || len(original) <= maxRpcReplyBytes || len(original) >= economicConservationStorageMaximum || first.Yuma == nil || len(first.Yuma.Active) != 1 || len(first.Yuma.Active[0].Allocations) != 2048 {
		t.Fatal("complete restore did not begin with the original large witness", len(original), err, first.Yuma)
	}
	if cold {
		f.reset(t)
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("large source archive before restore", code, issue)
		}
	}
	state := f.source.state(t)
	if state.Native.ExecutionProducer == nil || state.Native.ExecutionProducer.Completed != 1 || cold && (state.Archive == nil || len(state.Archive.Segments) != 1 || state.Archive.Segments[0].Bytes != uint64(len(original))) || !cold && state.Archive != nil {
		t.Fatal("source lost its exact cold witness or producer completion")
	}
	active, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	reference, present := durablevolume.ReferenceFromContext(f.ctx)
	if !present || f.storage == nil {
		t.Fatal("large source has no retained original physical declaration")
	}
	declaration, err := durablevolume.Load(reference)
	if err != nil || len(declaration.Volumes) != 1 || len(declaration.Volumes[0].StateRoots) != 2 {
		t.Fatal("large source does not have its two exact owner roots", err)
	}
	combined := &economicConservationRestoreFixture{archive: f, state: state, retained: declaration, readFiles: nativeProducerRestoreTestFiles}
	combined.request = economicConservationRestoreRequest{Schema: economicConservationRestoreSchema, Policy: f.source.policy,
		Original: monitorHistoryReference{Path: f.source.checkpoint, Sha256: monitorReadDigest(active), Bytes: uint64(len(active))},
		Limits:   durablevolume.PreparationCohortLimits{MaxRoots: 2, MaxPlanBytes: 32 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024, MaxEntries: 16384, MaxBytes: 512 * 1024 * 1024, MaxOwnerAttributes: 256, MaxOwnerAttributeBytes: 256 * 4096}}
	roots := append([]durablevolume.StateRootSpec(nil), declaration.Volumes[0].StateRoots...)
	sort.Slice(roots, func(i, j int) bool { return roots[i].Path < roots[j].Path })
	limits := durablevolume.InventoryLimits{MaxEntries: 8192, MaxBytes: 256 * 1024 * 1024, MaxDepth: 8, MaxOwnerAttributes: 128, MaxOwnerAttributeBytes: 128 * 4096}
	for _, root := range roots {
		source := economicConservationLargeRestoreSource(t, f, root.Path, declaration.Volumes[0])
		combined.sources = append(combined.sources, source)
		var peer durablevolume.PreparationOwner
		if root.Path == filepath.Dir(f.source.checkpoint) {
			peer = storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "independent-peer.json", maxRpcReplyBytes)
			path := filepath.Join(root.Path, "independent-peer.json")
			provisionMonitorTestCustody(t, path)
			owner, err := openMonitorHistorySnapshot(f.ctx, path, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(owner.publish([]byte("synthetic independent legacy owner\n"), nil), owner.close()); err != nil {
				t.Fatal(err)
			}
		}
		combined.files = append(combined.files, nativeProducerRestoreTestFiles(t, root.Path))
		// A zero additional owner on the artifact root is deliberate. The public
		// combined review must derive its complete native owner from the state.
		target := storageSnapshotRestoreTargetWithLimits(t, source, f.ctx, peer, false, &limits)
		combined.targets = append(combined.targets, target)
		raw, err := os.ReadFile(target.target.requestPath)
		if err != nil {
			t.Fatal(err)
		}
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(raw, &request); err != nil {
			t.Fatal(err)
		}
		request.Owners = nil
		if peer.Kind != "" {
			peer.Purpose, peer.RestoreCoverage = "restore", durablevolume.PreparationCompleteUnion
			request.Owners = []durablevolume.PreparationOwner{peer}
		}
		combined.request.Preparations = append(combined.request.Preparations, request)
	}
	work := map[string]uint64{}
	f.ctx = context.WithValue(f.ctx, nativeProducerRestoreWorkKey{}, func(stage string, units uint64) { work[stage] += units })
	plan := combined.plan(t)
	if work["completion-decoded"] != 1 || work["job-decoded"] != 1 || work["trie-node-verified"] == 0 || work["finality-window-decoded"] != 1 {
		t.Fatal("large restore omitted or repeated original producer admission", work)
	}
	proofs, blocks := producer.proofs.Load(), producer.blocks.Load()
	combined.apply(t, plan, true)
	reopened := f.sample(t, monitorServiceHooks{})
	if reopened.PolicyHash != first.PolicyHash || reopened.NativeCursor != first.NativeCursor || reopened.Yuma == nil || reopened.Yuma.MinerDenominator == nil || *reopened.Yuma.MinerDenominator != "98" || reopened.TargetMet != nil || reopened.ActualNativeOutcomeVerified || reopened.ActivationReady {
		t.Fatal("complete large restore reset provenance or invented economic authority", reopened)
	}
	if cold && (reopened.Yuma.Archived == nil || reopened.Yuma.Archived.Blocks != 1 || len(reopened.Yuma.Active) != 0) || !cold && (reopened.Yuma.Archived != nil || len(reopened.Yuma.Active) != 1 || len(reopened.Yuma.Active[0].Allocations) != 2048) {
		t.Fatal("complete large restore changed hot or cold original census", cold, reopened.Yuma)
	}
	if producer.proofs.Load() != proofs || producer.blocks.Load() != blocks {
		t.Fatal("restored accounted job repeated original proof acquisition", producer.proofs.Load()-proofs, producer.blocks.Load()-blocks)
	}
	if !cold {
		// The restored original owner can still publish its first exact archive.
		// This also checks that physical restore did not narrow future capacity.
		f.reset(t)
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("restored large original cannot continue its archive", code, issue)
		}
		state = f.source.state(t)
	}
	archived := state.Archive.Segments[0]
	reader, raw, err := f.source.policy.openHistoryReader(f.ctx, archived)
	if err != nil {
		t.Fatal("complete large restore lost original archive custody", err)
	}
	if err := reader.close(); err != nil || cold && !bytes.Equal(raw, original) || uint64(len(raw)) <= maxRpcReplyBytes {
		t.Fatal("complete large restore changed cold witness bytes", err)
	}
}

func TestEconomicConservationPublicLargeProfileRestoresCompleteProducerAndActiveWitness(t *testing.T) {
	economicConservationTestCompleteLargeRestore(t, false)
}

func TestEconomicConservationPublicLargeProfileRestoresCompleteProducerAndColdWitness(t *testing.T) {
	economicConservationTestCompleteLargeRestore(t, true)
}

// Real owner attributes, source inventories and public plans enforce each
// profile independently even when the checkpoints share directory ancestors.
func newEconomicConservationMixedTreeRestore(t *testing.T) (*storageSnapshotRestoreFixture, context.Context, durablevolume.PreparationRequest, []monitorHistoryReference) {
	t.Helper()
	source := newStoragePreparationCommandFixture(t)
	peer := storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "independent-peer.json", maxRpcReplyBytes)
	storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{peer})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	parent := filepath.Join(source.root, "history", "epoch")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	var references []monitorHistoryReference
	for index, name := range []string{"combined.json", "legacy.json"} {
		kind, maximum := "mainnet-monitor-checkpoint", maxRpcReplyBytes
		if index == 0 {
			kind, maximum = economicConservationStorageKind, economicConservationStorageMaximum
		}
		path := filepath.Join(parent, name)
		provisionMonitorTestCustodyProfile(t, path, kind, maximum)
		owner, err := openMonitorHistorySnapshotProfile(ctx, path, true, kind, maximum)
		if err != nil {
			t.Fatal(err)
		}
		raw := []byte("synthetic original fixed snapshot " + name + "\n")
		if err := errors.Join(owner.publish(raw, nil), owner.close()); err != nil {
			t.Fatal(err)
		}
		references = append(references, monitorHistoryReference{Path: path, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))})
	}
	target := storageSnapshotRestoreTarget(t, source, ctx, peer, false)
	raw, err := os.ReadFile(target.target.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := decodeMonitorHistoryInput(raw, &request); err != nil {
		t.Fatal(err)
	}
	request.Owners[0].RestoreCoverage = durablevolume.PreparationCompleteUnion
	request.MinAvailableBytes, request.MinAvailableInodes = 128*1024*1024, 4096
	request.Limits = durablevolume.PreparationLimits{MaxEntries: 256, MaxBytes: 128 * 1024 * 1024, MaxDepth: 8, MaxOwnerAttributes: 128, MaxOwnerAttributeBytes: 128 * 4096, MaxPlanBytes: 8 * 1024 * 1024}
	_, declared, err := monitorHistoryRestoreDeclaration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	review, err := newMonitorHistoryRestoreRootReview(ctx, request, declared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := review.readProfile(ctx, references[0], economicConservationStorageKind, economicConservationStorageMaximum); err != nil {
		t.Fatal("original nested large profile admission", err)
	}
	if _, err := review.read(ctx, references[1]); err != nil {
		t.Fatal("original nested legacy profile admission", err)
	}
	if err := review.finish(ctx); err != nil {
		t.Fatal("original mixed tree admission", err)
	}
	return target, ctx, review.request, references
}

func economicConservationWriteMixedTreeRequest(t *testing.T, target *storageSnapshotRestoreFixture, request durablevolume.PreparationRequest) {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target.target.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	target.target.requestHash = monitorReadDigest(raw)
}

func TestEconomicConservationRestoreMixedNestedProfilesKeepExactPhysicalOwners(t *testing.T) {
	target, _, request, references := newEconomicConservationMixedTreeRestore(t)
	economicConservationWriteMixedTreeRequest(t, target, request)
	restored := target.apply(t)
	for index, reference := range references {
		kind, maximum := "mainnet-monitor-checkpoint", maxRpcReplyBytes
		if index == 0 {
			kind, maximum = economicConservationStorageKind, economicConservationStorageMaximum
		}
		owner, err := openMonitorHistorySnapshotProfile(restored, reference.Path, false, kind, maximum)
		if err != nil {
			t.Fatal("restored mixed owner lost its original profile", index, err)
		}
		raw, present, err := owner.read()
		closeErr := owner.close()
		if err != nil || closeErr != nil || !present || monitorReadDigest(raw) != reference.Sha256 || uint64(len(raw)) != reference.Bytes {
			t.Fatal("restored mixed owner changed original bytes", index, err, closeErr)
		}
		if index == 0 {
			kind, maximum = "mainnet-monitor-checkpoint", maxRpcReplyBytes
		} else {
			kind, maximum = economicConservationStorageKind, economicConservationStorageMaximum
		}
		changed, err := openMonitorHistorySnapshotProfile(restored, reference.Path, false, kind, maximum)
		if changed != nil {
			if closeErr := changed.close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		if err == nil {
			t.Fatal("neighbor profile reinterpreted an original physical owner", index)
		}
	}
}

func TestEconomicConservationRestoreMixedProfileForgeryRefusesBeforeEffects(t *testing.T) {
	target, _, accepted, _ := newEconomicConservationMixedTreeRestore(t)
	economicConservationWriteMixedTreeRequest(t, target, accepted)
	storagePreparationFreezeOwnerPlan(t, target.target, "storage-prepare")
	original, err := json.Marshal(accepted)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"omitted", "swapped", "maximum", "duplicate", "unlisted"} {
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(original, &request); err != nil {
			t.Fatal(err)
		}
		found := false
		for index := range request.Owners {
			owner := &request.Owners[index]
			if owner.Kind != storageMonitorTreeKind {
				continue
			}
			found = true
			var scope storageMonitorTreeScope
			if err := decodeMonitorHistoryInput(owner.Inputs, &scope); err != nil || len(scope.Profiles) != 1 {
				t.Fatal("original mixed profile census absent", err)
			}
			switch fault {
			case "omitted":
				scope.Profiles = nil
			case "swapped":
				scope.Profiles[0].Path = "history/epoch/legacy.json"
			case "maximum":
				scope.Profiles[0].MaximumBytes = maxRpcReplyBytes
			case "duplicate":
				scope.Profiles = append(scope.Profiles, scope.Profiles[0])
			case "unlisted":
				scope.Profiles[0].Path = "history/epoch/unlisted.json"
			}
			owner.Inputs, err = json.Marshal(scope)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !found {
			t.Fatal("mixed original namespace omitted its tree owner")
		}
		economicConservationWriteMixedTreeRequest(t, target, request)
		before := mainnetNamespaceTest(t, target.target.root)
		var output, diagnostic bytes.Buffer
		code := runMain(target.target.ctx, []string{"storage-prepare", "plan", "--request", target.target.requestPath, "--request-sha256", target.target.requestHash}, &output, &diagnostic)
		if code == 0 || output.Len() != 0 || !reflect.DeepEqual(before, mainnetNamespaceTest(t, target.target.root)) {
			t.Fatal("forged neighboring profile acquired physical restore authority", fault, code, diagnostic.String())
		}
	}
	economicConservationWriteMixedTreeRequest(t, target, accepted)
	target.apply(t)
}

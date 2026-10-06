//go:build linux

// Real public preparation/export/restore surrounds the existing synthetic
// native replay, vault RPC and Claim HTTP sources. Copied files retain exact
// logical paths, source authority and financial obligations across new roots.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

type economicConservationRestoreFixture struct {
	archive   *economicConservationArchiveFixture
	sources   []*storagePreparationCommandFixture
	targets   []*storageSnapshotRestoreFixture
	request   economicConservationRestoreRequest
	state     economicConservationState
	files     []map[string]string
	retained  durablevolume.Config
	future    string
	readFiles func(*testing.T, string) map[string]string
}

// All actual and one future archive head are enrolled through public storage
// preparation before use. Each root also has an independent explicit co-owner.
func newEconomicConservationRestoreFixture(t *testing.T, rootCount, reviews int, mutate func(*economicConservationArchiveFixture)) *economicConservationRestoreFixture {
	t.Helper()
	return newEconomicConservationRestoreParentFixture(t, t.TempDir(), rootCount, reviews, mutate)
}

// The explicitly fresh parent belongs to this fixture. Production admission
// still rejects every unprotected ancestor; no adopted root is chmodded.
func newEconomicConservationRestoreParentFixture(t *testing.T, parent string, rootCount, reviews int, mutate func(*economicConservationArchiveFixture)) *economicConservationRestoreFixture {
	t.Helper()
	return newEconomicConservationRestoreNamespaceFixture(t, parent, rootCount, reviews, "", mutate)
}

// Nested heads are born at their final logical paths. Test-only fresh
// enrollment permits the runtime's accepted depth32 namespace before any
// signed checkpoint exists; public restore must preserve every original path.
func newEconomicConservationRestoreNamespaceFixture(t *testing.T, parent string, rootCount, reviews int, profile string, mutate func(*economicConservationArchiveFixture)) *economicConservationRestoreFixture {
	t.Helper()
	if rootCount < 1 || rootCount > 2 || reviews < 1 || reviews > 129 {
		t.Fatal("invalid explicit combined restore fixture bounds")
	}
	f := &economicConservationRestoreFixture{readFiles: bootstrapSuccessorPreparationTestFiles}
	if profile != "" {
		f.readFiles = func(t *testing.T, root string) map[string]string {
			return monitorHistoryRestoreTestFiles(t, root, true)
		}
	}
	protectFreshEconomicConservationTestRoot(t, parent)
	for _, name := range []string{"a", "b"}[:rootCount] {
		path := filepath.Join(parent, name, "state")
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		f.sources = append(f.sources, newStoragePreparationCommandFixtureAt(t, path))
	}
	healthyRoots := make([]string, rootCount)
	for index, source := range f.sources {
		healthyRoots[index] = source.storage.Roots[0]
	}
	physical := durablefixture.New(t, t.Context(), healthyRoots...)
	declaration, err := durablevolume.Load(physical.Reference)
	if err != nil {
		t.Fatal(err)
	}
	source := newEconomicConservationFixture(t, false)
	source.checkpoint, source.path = filepath.Join(f.sources[0].root, "combined.json"), filepath.Join(f.sources[0].metadata, "economic-policy.json")
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))
	source.policy.Continuation = &economicConservationContinuationPolicy{Schema: economicConservationResourcesSchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: monitorReadDigest([]byte("synthetic original combined restore capacity")), Initial: economicConservationResources{ActiveFacts: 256, ReadBudgetSeconds: 300, ArchiveSegments: 512, IndexEntries: 65536, IndexBytes: 64 * 1024 * 1024}}
	claimKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x75}, ed25519.SeedSize))
	claim := &source.policy.Claims[0]
	claim.EpochCapacity = 128
	claim.HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: "0x" + hex.EncodeToString(claimKey.Public().(ed25519.PublicKey)), ReviewSha256: monitorReadDigest([]byte("synthetic original combined restore Claim review")), InitialCapacity: monitorHistoryCapacity{Segments: 512, CatalogBytes: maximumMonitorHistoryCatalogBytes, HeldReaders: 512}}
	for epoch := int64(2); epoch <= 128; epoch++ {
		claim.Epochs = append(claim.Epochs, monitorClaimEpochPolicy{Epoch: epoch, ShareBps: 700, AcceptBy: source.now.Add(time.Hour).Format(time.RFC3339Nano)})
	}
	source.writePolicy(t)
	owners := make([][]durablevolume.PreparationOwner, rootCount)
	for index := range owners {
		owners[index] = []durablevolume.PreparationOwner{storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "independent-peer.json", maxRpcReplyBytes)}
	}
	owners[0] = append(owners[0], storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(source.checkpoint), maxRpcReplyBytes))
	paths := make([]string, reviews+1)
	for index := range paths {
		name := fmt.Sprintf("economic-archive-%03d.json", index+1)
		if profile != "" {
			paths[index] = monitorHistoryRestoreTestPath(t, f.sources[rootCount-1].root, name, profile)
		} else {
			paths[index] = filepath.Join(f.sources[rootCount-1].root, name)
			owners[rootCount-1] = append(owners[rootCount-1], storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", name, maxRpcReplyBytes))
		}
	}
	f.future = paths[reviews]
	var prepared []durablevolume.Config
	for index, original := range f.sources {
		original.storage, original.ctx = physical, physical.Context
		storagePreparationOwnerRequest(t, original, "daemon", owners[index])
		raw, err := os.ReadFile(original.requestPath)
		if err != nil {
			t.Fatal(err)
		}
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(raw, &request); err != nil {
			t.Fatal(err)
		}
		request.MountPath, request.CapacityProfile = declaration.Volumes[0].MountPath, "urnetwork-preparation-many-owners-v1"
		request.Limits = durablevolume.PreparationLimits{MaxEntries: 4096, MaxBytes: 256 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 2048, MaxOwnerAttributeBytes: 8 * 1024 * 1024, MaxPlanBytes: 8 * 1024 * 1024}
		request.MinAvailableBytes, request.MinAvailableInodes = 256*1024*1024, 4096
		raw, err = json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(original.requestPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
		original.requestHash = monitorReadDigest(raw)
		ctx := storagePreparationApplyOwnerCommand(t, original, "storage-prepare")
		reference, present := durablevolume.ReferenceFromContext(ctx)
		if !present {
			t.Fatal("combined prepared declaration is absent")
		}
		config, err := durablevolume.Load(reference)
		if err != nil {
			t.Fatal(err)
		}
		prepared = append(prepared, config)
	}
	if profile != "" {
		for _, path := range paths {
			directory, name := filepath.Dir(path), filepath.Base(path)
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			durablefixture.ProvisionSnapshot(t, directory, "mainnet-monitor-checkpoint", name, maxRpcReplyBytes, name+".lock", map[string][]byte{name + ".lock": nil})
		}
	}
	f.retained = prepared[0]
	for _, config := range prepared[1:] {
		f.retained.Volumes[0].StateRoots = append(f.retained.Volumes[0].StateRoots, config.Volumes[0].StateRoots...)
	}
	f.retained.Volumes[0].StateRoots = append(f.retained.Volumes[0].StateRoots, declaration.Volumes[0].StateRoots...)
	raw, err := json.Marshal(f.retained)
	if err != nil {
		t.Fatal(err)
	}
	reference := durablevolume.Reference{Path: filepath.Join(parent, "original-volumes.json"), Sha256: monitorReadDigest(raw)}
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.archive = &economicConservationArchiveFixture{source: source, ctx: durablepath.WithHost(durablevolume.WithReference(t.Context(), reference), physical.Host), metadata: economicConservationTestRestoreMetadata(t, f.sources[0]), key: key}
	f.archive.sample(t, monitorServiceHooks{})
	for index := 0; index < reviews; index++ {
		f.nextArchive(t, paths[index])
		_, _, args := economicConservationClaimWindowTestPlan(t, f.archive, economicConservationClaimWindowTestNext(t, f.archive), monitorReadDigest([]byte(fmt.Sprintf("synthetic original combined restore review %d", index+1))))
		if code, issue := f.archive.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("original full-window adoption failed before export", index, code, issue)
		}
	}
	if mutate != nil {
		mutate(f.archive)
	}
	f.state = source.state(t)
	raw, err = os.ReadFile(source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	f.request = economicConservationRestoreRequest{Schema: economicConservationRestoreSchema, Policy: source.policy, Original: monitorHistoryReference{Path: source.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, Limits: durablevolume.PreparationCohortLimits{MaxRoots: uint64(rootCount), MaxPlanBytes: 32 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024, MaxEntries: 8192, MaxBytes: 512 * 1024 * 1024, MaxOwnerAttributes: 4096, MaxOwnerAttributeBytes: 16 * 1024 * 1024}}
	limits := durablevolume.InventoryLimits{MaxEntries: 4096, MaxBytes: 256 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 2048, MaxOwnerAttributeBytes: 8 * 1024 * 1024}
	if profile != "" {
		limits.MaxDepth = 32
	}
	for index, original := range f.sources {
		requireEconomicConservationTestPreparation(t, original)
		f.files = append(f.files, f.readFiles(t, original.root))
		target := storageSnapshotRestoreTargetWithLimits(t, original, f.archive.ctx, owners[index][0], false, &limits)
		f.targets = append(f.targets, target)
		raw, err := os.ReadFile(target.target.requestPath)
		if err != nil {
			t.Fatal(err)
		}
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(raw, &request); err != nil {
			t.Fatal(err)
		}
		request.Limits.MaxDepth = limits.MaxDepth
		request.Owners[0].RestoreCoverage = durablevolume.PreparationCompleteUnion
		if index == rootCount-1 {
			future := owners[index][len(owners[index])-1]
			if profile != "" {
				relative, err := filepath.Rel(original.root, f.future)
				if err != nil {
					t.Fatal(err)
				}
				inputs, err := json.Marshal(storageMonitorTreeScope{Schema: storageMonitorTreeSchema, Snapshots: []string{relative}})
				if err != nil {
					t.Fatal(err)
				}
				future = durablevolume.PreparationOwner{Kind: storageMonitorTreeKind, RelativePath: ".", Inputs: inputs}
			}
			future.Purpose, future.RestoreCoverage = "restore", durablevolume.PreparationCompleteUnion
			request.Owners = append(request.Owners, future)
		}
		f.request.Preparations = append(f.request.Preparations, request)
	}
	return f
}

// Future owners were independently prepared before original use. This helper
// only retains the exact current checkpoint and writer-fence request metadata.
func (self *economicConservationRestoreFixture) nextArchive(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(self.archive.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	self.archive.sequence++
	original := monitorHistoryReference{Path: self.archive.source.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}
	fence := monitorHistoryWriterFence{Schema: monitorHistoryWriterFenceSchema, Original: original, PolicyHash: self.archive.source.policy.identityHash(), StoppedAndJoined: true}
	fencePath, pin := self.archive.document(t, fmt.Sprintf("writer-fence-%d.json", self.archive.sequence), fence)
	self.archive.request = economicConservationArchiveRequest{Schema: economicConservationArchiveRequestSchema, Policy: self.archive.source.policy, Original: original, ArchivePath: path, FormerWriterFence: planFileReference{Path: fencePath, Sha256: pin}, FutureSegments: 1, FutureIndexEntries: 16, FutureIndexBytes: 4096}
}

func (self *economicConservationRestoreFixture) args(t *testing.T, request economicConservationRestoreRequest) []string {
	t.Helper()
	path, pin := self.archive.document(t, "complete-restore-request.json", request)
	mode := "restore-request"
	if len(self.targets) > 1 {
		mode = "restore-cohort-plan"
	}
	return []string{"economic-conservation-archive", mode, "--request", path, "--request-sha256", pin}
}

func (self *economicConservationRestoreFixture) unchanged(t *testing.T) {
	t.Helper()
	for _, target := range self.targets {
		entries, err := os.ReadDir(target.target.root)
		if err != nil || len(entries) != 0 {
			t.Fatal("combined restore review changed a target before apply", err)
		}
	}
}

func (self *economicConservationRestoreFixture) plan(t *testing.T) durablevolume.Reference {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(self.archive.ctx, self.args(t, self.request), &output, &diagnostic); code != 0 {
		t.Fatal("complete combined restore plan failed", code, diagnostic.String())
	}
	self.unchanged(t)
	if len(self.targets) == 1 {
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(output.Bytes(), &request); err != nil || len(request.Owners) != len(self.state.Archive.Segments)+3 {
			t.Fatal("single combined restore omitted current, archive or independent owner", err, request)
		}
		if err := os.WriteFile(self.targets[0].target.requestPath, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		self.targets[0].target.requestHash = monitorReadDigest(output.Bytes())
		path, pin := storagePreparationFreezeOwnerPlan(t, self.targets[0].target, "storage-prepare")
		return durablevolume.Reference{Path: path, Sha256: pin}
	}
	var result durablevolume.PreparationCohortResult
	if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || result.Applied || result.Complete || result.RestartAuthorized || len(result.Roots) != len(self.targets) {
		t.Fatal("combined restore cohort granted target or restart authority", err, result)
	}
	return result.Cohort
}

// Exact same retained apply reconciles a lost output after publication. The
// complete declaration keeps every untouched original reserve and root intact.
func (self *economicConservationRestoreFixture) apply(t *testing.T, reference durablevolume.Reference, short bool) {
	t.Helper()
	mode, name := "apply", "plan"
	if len(self.targets) > 1 {
		mode, name = "cohort-apply", "cohort"
	}
	args := []string{"storage-prepare", mode, "--" + name, reference.Path, "--" + name + "-sha256", reference.Sha256}
	var output, diagnostic bytes.Buffer
	if short {
		if code := runMain(self.archive.ctx, args, storagePreparationShortOutput{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "not fully delivered") {
			t.Fatal("combined restore did not reach actual lost output boundary", code, diagnostic.String())
		}
		diagnostic.Reset()
	}
	if code := runMain(self.archive.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("combined restore exact apply failed", code, diagnostic.String())
	}
	if len(self.targets) == 1 {
		var result durablevolume.PreparationResult
		if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || result.RestartAuthorized {
			t.Fatal("single combined restore changed restart authority", err)
		}
		self.archive.ctx = monitorNativeRestoreContext(t, self.sources[0], durablevolume.WithReference(self.targets[0].target.ctx, result.Declaration))
	} else {
		var result durablevolume.PreparationCohortResult
		if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || !result.Applied || !result.Complete || result.RestartAuthorized {
			t.Fatal("combined restore did not complete exact reviewed cohort", err, result)
		}
		output.Reset()
		diagnostic.Reset()
		args[1] = "cohort-config"
		if code := runMain(self.archive.ctx, args, &output, &diagnostic); code != 0 || output.String() != result.DeclarationDocument {
			t.Fatal("combined restore declaration differs from completed cohort", code, diagnostic.String())
		}
		path := filepath.Join(self.targets[0].target.metadata, "restored-volumes.json")
		if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		var restored durablevolume.Config
		if err := decodeMonitorHistoryInput(output.Bytes(), &restored); err != nil {
			t.Fatal(err)
		}
		// Reuse the exact untouched-root reserve assertion from the qualified
		// complete Claim cohort fixture; it reads declarations only.
		check := &monitorClaimRestoreFixture{retained: self.retained, request: monitorClaimRestoreCohortRequest{Preparations: self.request.Preparations}}
		check.unchangedHealthyAuthority(t, restored)
		self.archive.ctx = durablepath.WithHost(durablevolume.WithReference(t.Context(), durablevolume.Reference{Path: path, Sha256: result.DeclarationSha256}), self.sources[0].storage.Host)
	}
	for index, target := range self.targets {
		readFiles := self.readFiles
		if readFiles == nil {
			readFiles = bootstrapSuccessorPreparationTestFiles
		}
		if files := readFiles(t, target.target.root); !reflect.DeepEqual(files, self.files[index]) {
			t.Fatal("combined restore changed original checkpoint or co-owner bytes", index)
		}
	}
	if !reflect.DeepEqual(self.archive.source.state(t), self.state) {
		t.Fatal("combined restore changed original economic liabilities or approvals")
	}
}

func TestEconomicConservationRestorePublicSingleRootKeepsFullClaimWindow(t *testing.T) {
	f := newEconomicConservationRestoreFixture(t, 1, 1, nil)
	f.apply(t, f.plan(t), true)
	summary := f.archive.sample(t, monitorServiceHooks{})
	state := f.archive.source.state(t)
	if summary.TargetMet != nil || summary.MatchedReceipts != 1 || summary.AggregatePayments != 1 || len(state.ClaimStates[0].Epochs) != 128 || state.ClaimStates[0].Epochs[0].Epoch != 2 || state.ClaimWindows[0].Ordinal != 1 {
		t.Fatal("restored combined owner lost full-window receipt, unknown epochs or deferred credit", summary, state)
	}
}

func TestEconomicConservationRestorePublicCohortCrosses128ReviewsThenContinues(t *testing.T) {
	f := newEconomicConservationRestoreFixture(t, 2, 129, nil)
	if f.state.ClaimWindows[0].Ordinal != 129 || f.state.Archive.ClaimHeads[0].Retired != 1 || len(f.state.ClaimStates[0].Epochs) != 128 {
		t.Fatal("original full-history restore fixture did not cross both real bounds")
	}
	f.apply(t, f.plan(t), true)
	var reads, decodes atomic.Uint64
	summary := f.archive.sample(t, monitorServiceHooks{historyRead: func(role, stage string) {
		if role == economicConservationRole && stage == "conservation-archive-admission" {
			reads.Add(1)
		}
	}, economicClaimWork: func(_ string, stage string, units uint64) {
		if stage == "archive-checkpoint-decoded" {
			decodes.Add(units)
		}
	}})
	if reads.Load() != 129 || decodes.Load() != 129 || summary.MatchedReceipts != 1 || summary.TargetMet != nil || summary.AggregatePayments != 1 || !summary.NativeCurrent || !summary.VaultCurrent {
		t.Fatal("restored combined history repeated or skipped original admission", reads.Load(), decodes.Load(), summary)
	}
	f.nextArchive(t, f.future)
	_, _, args := economicConservationClaimWindowTestPlan(t, f.archive, economicConservationClaimWindowTestNext(t, f.archive), monitorReadDigest([]byte("synthetic review after complete cohort restore")))
	if code, issue := f.archive.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("restored combined owner cannot adopt next original-key window", code, issue)
	}
	if state := f.archive.source.state(t); state.ClaimWindows[0].Ordinal != 130 || len(state.ClaimStates[0].Epochs) != 128 || state.PolicyHash != f.state.PolicyHash || state.Native.Cursor.Number < f.state.Native.Cursor.Number || state.Vault.Cursor.Number < f.state.Vault.Cursor.Number {
		t.Fatal("post-restore continuation reset original signed lineage or progress", state)
	}
	f.archive.sample(t, monitorServiceHooks{})
}

func TestEconomicConservationRestorePublicRequiresEverySourceAndCoowner(t *testing.T) {
	for _, fault := range []string{"late-source", "original-root", "independent-owner"} {
		f := newEconomicConservationRestoreFixture(t, 2, 1, nil)
		request := f.request
		request.Preparations = append([]durablevolume.PreparationRequest(nil), request.Preparations...)
		switch fault {
		case "late-source":
			if err := os.Remove(filepath.Join(f.targets[1].archive, filepath.Base(f.state.Archive.Segments[0].Path))); err != nil {
				t.Fatal(err)
			}
		case "original-root":
			request.Preparations = request.Preparations[:1]
		case "independent-owner":
			request.Preparations[0].Owners = nil
		}
		var output, diagnostic bytes.Buffer
		if code := runMain(f.archive.ctx, f.args(t, request), &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("combined restore omitted original source or independent co-owner", fault, code, diagnostic.String())
		}
		f.unchanged(t)
	}
}

func TestEconomicConservationRestorePublicRejectsChangedAuthorityAndReserve(t *testing.T) {
	f := newEconomicConservationRestoreFixture(t, 2, 1, nil)
	for _, fault := range []string{"claim-key", "reserve", "cancel"} {
		request := f.request
		request.Preparations = append([]durablevolume.PreparationRequest(nil), request.Preparations...)
		request.Policy.Claims = append([]monitorClaimPolicy(nil), request.Policy.Claims...)
		ctx, cancel := context.WithCancel(f.archive.ctx)
		switch fault {
		case "claim-key":
			catalog := *request.Policy.Claims[0].HistoryCatalog
			catalog.ApprovalPublicKey = "0x" + strings.Repeat("6a", 32)
			request.Policy.Claims[0].HistoryCatalog = &catalog
		case "reserve":
			request.Preparations[0].MinAvailableBytes++
		case "cancel":
			cancel()
		}
		var output, diagnostic bytes.Buffer
		code := runMain(ctx, f.args(t, request), &output, &diagnostic)
		cancel()
		if code == 0 || output.Len() != 0 {
			t.Fatal("combined restore changed original authority or untouched reserve", fault, code, diagnostic.String())
		}
		f.unchanged(t)
	}
}

// Read causes remain observations, while fully returned different bytes are
// an actual contradiction. Cancellation after a real source read stops replay.
func TestEconomicConservationRestoreReadFailureCannotInventHistoryMismatch(t *testing.T) {
	f := newEconomicConservationClaimWindowFixture(t, true, false)
	_, _, args := economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte("synthetic copied-source causal review")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	raw, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	original := monitorHistoryReference{Path: f.source.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}
	for _, fault := range []error{syscall.EIO, context.Canceled, nil} {
		ctx, cancel := context.WithCancel(f.ctx)
		calls := 0
		err := validateEconomicConservationRestoreHistory(ctx, f.source.policy, original, func(reference monitorHistoryReference) ([]byte, error) {
			calls++
			raw, err := os.ReadFile(reference.Path)
			if err != nil {
				return nil, err
			}
			if calls == 2 {
				if fault == context.Canceled {
					cancel()
					return raw, nil
				}
				if fault != nil {
					return nil, fault
				}
				return append(raw, ' '), nil
			}
			return raw, nil
		}, nil, monitorServiceHooks{})
		cancel()
		if calls != 2 || fault != nil && (!errors.Is(err, fault) || strings.Contains(fmt.Sprint(err), "differ")) || fault == nil && (err == nil || !strings.Contains(err.Error(), "returned bytes differ")) {
			t.Fatal("economic copied-source read lost its observed cause", calls, fault, err)
		}
	}
}

func TestEconomicConservationRestorePublicReconstructsOriginalClaimHead(t *testing.T) {
	f := newEconomicConservationRestoreFixture(t, 2, 2, func(f *economicConservationArchiveFixture) {
		state := f.source.state(t)
		state.Archive.ClaimHeads[0].Retired++
		state.ContentHash = state.hash()
		raw, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := openMonitorHistorySnapshot(f.ctx, f.source.checkpoint, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(owner.publish(append(raw, '\n'), nil), owner.close()); err != nil {
			t.Fatal(err)
		}
	})
	var output, diagnostic bytes.Buffer
	if code := runMain(f.archive.ctx, f.args(t, f.request), &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "archive summary differs") {
		t.Fatal("combined restore trusted a self-sealed substituted Claim head", code, diagnostic.String())
	}
	f.unchanged(t)
}

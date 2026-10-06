//go:build linux

// Whole-history restoration uses actual public preparation, checkpoint writers
// and monitor reopening. Large synthetic history is not a chain observation claim.
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
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

type monitorNativeRestoreFixture struct {
	native   *monitorEconomicTestFixture
	catalog  *monitorNativeCatalogFixture
	source   *storagePreparationCommandFixture
	storage  *storageSnapshotRestoreFixture
	request  monitorNativeRestoreRequest
	original map[string]string
	record   monitorEconomicNativeCheckpoint
}

// Metrics retain their own declared root. No disposable configuration or
// metrics file is hidden from the complete original checkpoint-root census.
func monitorNativeRestoreContext(t *testing.T, source *storagePreparationCommandFixture, ctx context.Context) context.Context {
	t.Helper()
	reference, _ := durablevolume.ReferenceFromContext(ctx)
	declaration, err := durablevolume.Load(reference)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := durablevolume.Load(source.storage.Reference)
	if err != nil {
		t.Fatal(err)
	}
	declaration.Volumes[0].StateRoots = append(declaration.Volumes[0].StateRoots, metrics.Volumes[0].StateRoots[0])
	raw, err := json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(reference.Path), "monitor-complete-volumes.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), durablevolume.Reference{Path: path, Sha256: durablefixture.Digest(raw)}), source.storage.Host)
}

// Public fresh preparation supplies every snapshot marker, including all 512
// segments in the capacity case. Test helpers never enroll a retained head.
func newMonitorNativeRestoreFixture(t *testing.T, segments int) *monitorNativeRestoreFixture {
	t.Helper()
	parent, err := os.MkdirTemp(os.TempDir(), "native-restore-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(parent); err != nil {
			t.Error(err)
		}
	})
	source := newStoragePreparationCommandFixtureAt(t, parent)
	native := newMonitorEconomicTestFixture(t, false)
	native.services.checkpointPath = filepath.Join(source.root, "monitor.json")
	native.services.metricsPath = filepath.Join(source.storage.Roots[0], "monitor.prom")
	native.services.policyPath = filepath.Join(source.metadata, "services.json")
	native.policy.HistoryEntries = 1
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x4a}, ed25519.SeedSize))
	native.policy.HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema,
		ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: "sha256:" + strings.Repeat("9", 64),
		InitialCapacity: monitorHistoryCapacity{Segments: 128, CatalogBytes: 64 * 1024, HeldReaders: 128}}
	checkpoint, _ := monitorEconomicNativePaths(native.services.checkpointPath, native.services.metricsPath, native.policy.Role)
	owners := []durablevolume.PreparationOwner{
		storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(native.services.checkpointPath), maxRpcReplyBytes),
		storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(checkpoint), maxRpcReplyBytes),
	}
	for index := 0; index < segments; index++ {
		owners = append(owners, storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", fmt.Sprintf("a%03d.json", index), maxRpcReplyBytes))
	}
	storagePreparationOwnerRequest(t, source, "daemon", owners)
	raw, err := os.ReadFile(source.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var preparation durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &preparation); err != nil {
		t.Fatal(err)
	}
	preparation.CapacityProfile = "urnetwork-preparation-many-owners-v1"
	preparation.Limits = durablevolume.PreparationLimits{MaxEntries: 4096, MaxBytes: 256 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 2048, MaxOwnerAttributeBytes: 2048 * 4096, MaxPlanBytes: 8 * 1024 * 1024}
	preparation.MinAvailableBytes, preparation.MinAvailableInodes = 256*1024*1024, 4096
	raw, err = json.Marshal(preparation)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	source.requestHash = durablefixture.Digest(raw)
	preparationRaw := append([]byte(nil), raw...)
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	native.ctx = monitorNativeRestoreContext(t, source, ctx)
	native.services.policy.NativeEconomics = []monitorEconomicNativePolicy{native.policy}
	native.services.writePolicy(t)
	run := native.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current || event.State.Cursor.Number != 101 || event.State.PendingThrough == nil {
		t.Fatal("native fixture did not reach actual retained pending range", event)
	}
	run.stop(t)
	archiveMetadata := filepath.Join(source.metadata, "native-archive-operation")
	if err := os.Mkdir(archiveMetadata, 0700); err != nil {
		t.Fatal(err)
	}
	archive := &monitorNativeArchiveFixture{native: native, checkpoint: checkpoint, archive: filepath.Join(source.root, "a000.json"), metadata: archiveMetadata}
	archive.resetRequest(t)
	catalog := &monitorNativeCatalogFixture{archive: archive, key: key, request: monitorNativeCatalogRequest{Schema: monitorNativeCatalogRequestSchema,
		Expected: archive.request.Expected, Policy: native.policy, Original: archive.request.Original, FormerWriterFence: archive.request.FormerWriterFence,
		Capacity: monitorHistoryCapacity{Segments: 512, CatalogBytes: 128 * 1024, HeldReaders: 512}, FutureSegments: 2}}
	plan := catalog.plan(t)
	catalog.apply(t, plan, catalog.approve(t, plan))
	if segments == 1 {
		archive.resetRequest(t)
		_, args := archive.plan(t)
		archive.apply(t, args)
	} else {
		monitorNativeRestoreSeedFullHistory(t, native, checkpoint, segments)
	}
	retainedPreparation, err := os.ReadFile(source.requestPath)
	if err != nil || !bytes.Equal(retainedPreparation, preparationRaw) {
		t.Fatal("archive operation replaced its independent original preparation input", err)
	}
	record := native.record(t)
	original := bootstrapSuccessorPreparationTestFiles(t, source.root)
	limits := durablevolume.InventoryLimits{MaxEntries: 4096, MaxBytes: 256 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 2048, MaxOwnerAttributeBytes: 8 * 1024 * 1024}
	storage := storageSnapshotRestoreTargetWithLimits(t, source, native.ctx, owners[0], false, &limits)
	raw, err = os.ReadFile(storage.target.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	preparation = durablevolume.PreparationRequest{}
	if err := decodeMonitorHistoryInput(raw, &preparation); err != nil {
		t.Fatal(err)
	}
	if preparation.Schema != durablevolume.PreparationRequestSchema || preparation.Purpose != "restore" || preparation.Scope != "daemon" || preparation.RestoreSource == nil {
		t.Fatal("public restore fixture did not retain its exact daemon request", preparation.Schema, preparation.Purpose, preparation.Scope)
	}
	preparation.Owners[0].RestoreCoverage = "complete-union-v1"
	raw = []byte(original[filepath.Base(checkpoint)])
	request := monitorNativeRestoreRequest{Schema: monitorNativeRestoreRequestSchema, Expected: archive.request.Expected, Policy: native.policy,
		Original: monitorHistoryReference{Path: checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, Preparation: preparation}
	return &monitorNativeRestoreFixture{native: native, catalog: catalog, source: source, storage: storage, request: request, original: original, record: record}
}

// The capacity boundary uses 512 synthetic retained observations passed through
// the real state transition, canonical encoder, compactor and snapshot writer.
// Public RPC reopening then checks their final canonical cursor. The separate
// pending-range control uses wholly RPC-produced original economics.
func monitorNativeRestoreSeedFullHistory(t *testing.T, native *monitorEconomicTestFixture, checkpoint string, count int) {
	t.Helper()
	paths := make([]string, count)
	for index := range paths {
		paths[index] = filepath.Join(filepath.Dir(checkpoint), fmt.Sprintf("a%03d.json", index))
	}
	monitorNativeRestoreSeedHistoryPaths(t, native, checkpoint, paths)
}

// Explicit original paths allow a separately reviewed cross-root fixture.
// The state machine and all emitted signed/catalog bytes are otherwise shared.
func monitorNativeRestoreSeedHistoryPaths(t *testing.T, native *monitorEconomicTestFixture, checkpoint string, paths []string) {
	t.Helper()
	record := native.record(t)
	event := *record.State.History[0].Incentive
	for index, path := range paths {
		if index != 0 {
			number := record.State.Cursor.Number + 1
			hash := native.source.chain.byHeight[number]
			header := native.source.chain.headers[hash]
			if hash == "" {
				header, hash = rootReceiptHeaderFixture(t, record.State.Cursor.Hash, number, nil, false)
				native.source.chain.headers[hash], native.source.chain.byHeight[number], native.source.chain.bodies[hash] = header, hash, []string{}
			}
			boundary := economicEmissionBoundary{Number: number, Hash: hash}
			window := native.policy.Observation
			window.From, window.Through = record.State.Cursor, boundary
			observation := economicEmissionObservation{Complete: true, Policy: window, ClosingFinalized: boundary,
				Blocks: []economicEmissionBlock{{Boundary: boundary, Header: header, EventsHash: rootExtrinsicHash([]byte(fmt.Sprint(number))),
					ExecutionRuntime: &native.source.chain.profile, PostStateRuntime: &native.source.chain.profile, Events: []economicEmissionEvent{event}}}}
			observation.ContentHash = rootObjectHash(observation)
			next, err := record.State.append(native.policy, &observation, native.services.clock.now())
			if err != nil {
				t.Fatal("synthetic original append failed", index, err)
			}
			record.State = *next
		}
		raw, err := encodeMonitorNativeCheckpoint(record)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := openMonitorHistorySnapshot(native.ctx, path, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(owner.publish(raw, nil), owner.close()); err != nil {
			t.Fatal(err)
		}
		record, err = compactMonitorEconomicNative(record, monitorHistoryReference{Path: path, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, native.policy)
		if err != nil {
			t.Fatal("full accepted retained catalog does not fit declared bytes", index, err)
		}
	}
	native.source.chain.finalized = record.State.Cursor.Hash
	raw, err := encodeMonitorNativeCheckpoint(record)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := openMonitorHistorySnapshot(native.ctx, checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(owner.publish(raw, nil), owner.close()); err != nil {
		t.Fatal(err)
	}
}

func (self *monitorNativeRestoreFixture) requestArgs(t *testing.T, request monitorNativeRestoreRequest) []string {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.storage.target.metadata, "native-history-request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return []string{"monitor-native-archive", "restore-request", "--request", path, "--request-sha256", monitorReadDigest(raw)}
}

func (self *monitorNativeRestoreFixture) plan(t *testing.T) (string, string) {
	t.Helper()
	before := mainnetNamespaceTest(t, self.storage.target.root)
	var output, diagnostic bytes.Buffer
	if code := runMain(self.native.ctx, self.requestArgs(t, self.request), &output, &diagnostic); code != 0 {
		t.Fatal("whole native history request failed", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, self.storage.target.root)) {
		t.Fatal("request export mutated restore target")
	}
	var request durablevolume.PreparationRequest
	if err := decodeMonitorHistoryInput(output.Bytes(), &request); err != nil || len(request.Owners) != len(self.record.State.Archive.Segments)+2 {
		t.Fatal("derived restore omitted an original archive or active head", err)
	}
	if err := os.WriteFile(self.storage.target.requestPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	self.storage.target.requestHash = monitorReadDigest(output.Bytes())
	return storagePreparationFreezeOwnerPlan(t, self.storage.target, "storage-prepare")
}

func (self *monitorNativeRestoreFixture) apply(t *testing.T, path, hash string, short bool) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	args := []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}
	if short {
		if code := runMain(self.storage.target.ctx, args, storagePreparationShortOutput{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "not fully delivered") {
			t.Fatal("valid restore did not reach completed output loss", code, diagnostic.String())
		}
		diagnostic.Reset()
	}
	if code := runMain(self.storage.target.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("whole native restore apply failed", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("restore acquired runtime authority", err)
	}
	for name, original := range self.original {
		raw, err := os.ReadFile(filepath.Join(self.storage.target.root, name))
		if err != nil || string(raw) != original {
			t.Fatal("restore changed original member bytes", name, err)
		}
	}
	self.native.ctx = monitorNativeRestoreContext(t, self.source, durablevolume.WithReference(self.storage.target.ctx, result.Declaration))
}

func TestMonitorNativeRestoreRetainsSignedCapacityAndPendingOutcome(t *testing.T) {
	f := newMonitorNativeRestoreFixture(t, 1)
	path, hash := f.plan(t)
	f.apply(t, path, hash, true)
	before := f.native.record(t)
	if !reflect.DeepEqual(before, f.record) {
		t.Fatal("physical restore rewrote signed approval or original economics")
	}
	run := f.native.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 102 || event.State.PendingThrough != nil || event.State.ObservedAlpha != "10" || monitorEconomicTestFee(event) != "12" || event.State.ArchiveSegmentCapacity != 512 {
		t.Fatal("restored actual public role lost its original pending outcome", event)
	}
	run.stop(t)
	if !reflect.DeepEqual(f.native.record(t).State.Catalog, f.record.State.Catalog) {
		t.Fatal("continued monitor discarded original signed capacity")
	}
}

func TestMonitorNativeRestoreFull512HistoryAndBothActiveHeads(t *testing.T) {
	f := newMonitorNativeRestoreFixture(t, 512)
	path, hash := f.plan(t)
	f.apply(t, path, hash, false)
	admission := monitorNativeObserveAdmission(t, f.record.State.Archive.Segments)
	run := f.native.start(t, monitorServiceHooks{})
	admission(run)
	event := run.next(t)
	if !event.Current || event.State.ArchiveSegments != 512 || event.State.ArchivedEvents != 512 || event.State.BatchCount != 512 || event.State.Cursor.Number != 612 || event.State.ObservedAlpha != "5120" || event.State.ArchiveSegmentCapacity != 512 {
		t.Fatal("full accepted archive did not reopen through actual public monitor", event)
	}
	run.stop(t)
}

func TestMonitorNativeRestoreRefusesOmittedChangedAndUnreviewedHistory(t *testing.T) {
	f := newMonitorNativeRestoreFixture(t, 1)
	path, hash := f.plan(t)
	_ = path
	_ = hash
	before := mainnetNamespaceTest(t, f.storage.target.root)
	for _, fault := range []string{"wrong-policy", "wrong-original", "relocated-root", "profile", "bytes", "inodes", "attributes", "canceled"} {
		request, ctx := f.request, f.native.ctx
		switch fault {
		case "wrong-policy":
			request.Policy.Role = "foreign-native"
		case "wrong-original":
			request.Original.Sha256 = "sha256:" + strings.Repeat("8", 64)
		case "relocated-root":
			request.Preparation.RootPath += "-other"
		case "profile":
			request.Preparation.CapacityProfile = ""
		case "bytes":
			request.Preparation.MinAvailableBytes = 1
		case "inodes":
			request.Preparation.MinAvailableInodes = 1
		case "attributes":
			request.Preparation.Limits.MaxOwnerAttributes = 1
		case "canceled":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		var output, diagnostic bytes.Buffer
		if code := runMain(ctx, f.requestArgs(t, request), &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Errorf("%s admitted an incomplete or unreviewed native restore: %d %s", fault, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.storage.target.root)) {
			t.Fatal("refused native restore changed target", fault)
		}
	}
	segment := filepath.Join(f.storage.archive, filepath.Base(f.record.State.Archive.Segments[0].Path))
	raw, err := os.ReadFile(segment)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(segment, append([]byte(nil), raw[:len(raw)-1]...), 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	if code := runMain(f.native.ctx, f.requestArgs(t, f.request), &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "monitor history copied member read") || !strings.Contains(diagnostic.String(), "differs from its exact pin") {
		t.Fatal("changed copied segment did not fail its exact-byte admission", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.storage.target.root)) {
		t.Fatal("changed segment wrote target custody")
	}
	if err := os.WriteFile(segment, raw, 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	diagnostic.Reset()
	if code := runMain(f.native.ctx, f.requestArgs(t, f.request), &output, &diagnostic); code != 0 || output.Len() == 0 {
		t.Fatal("restored exact copied bytes did not restore request admission", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.storage.target.root)) {
		t.Fatal("read-only request recovery changed target custody")
	}
}

// Read-only request derivation is not physical restore authorization. The real
// core plan rechecks copied owner attributes before any target publication.
func TestMonitorNativeRestoreActualPlanRefusesLostCopiedArchiveHead(t *testing.T) {
	f := newMonitorNativeRestoreFixture(t, 1)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.native.ctx, f.requestArgs(t, f.request), &output, &diagnostic); code != 0 {
		t.Fatal("positive restore-request baseline failed", code, diagnostic.String())
	}
	if err := os.WriteFile(f.storage.target.requestPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	hash := monitorReadDigest(output.Bytes())
	name := filepath.Base(f.record.State.Archive.Segments[0].Path)
	if err := unix.Removexattr(filepath.Join(f.storage.archive, name+".lock"), durablehead.Attribute("mainnet-monitor-checkpoint", name)); err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, f.storage.target.root)
	output.Reset()
	diagnostic.Reset()
	code := runMain(f.storage.target.ctx, []string{"storage-prepare", "plan", "--request", f.storage.target.requestPath, "--request-sha256", hash}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 {
		t.Fatal("actual storage plan accepted a lost original archive head", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.storage.target.root)) {
		t.Fatal("lost archive head published target custody")
	}
}

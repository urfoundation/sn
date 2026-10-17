//go:build linux || darwin

// The original native checkpoint and its archive occupy separate real roots.
// Public preparation and restore retain exact paths and all co-owner metadata.
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
	"syscall"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A single synthetic mount has two restored roots and untouched metrics roots.
type monitorNativeCohortFixture struct {
	native  *monitorEconomicTestFixture
	sources []*storagePreparationCommandFixture
	targets []*storageSnapshotRestoreFixture
	request monitorNativeRestoreCohortRequest
	record  monitorEconomicNativeCheckpoint
	files   []map[string]string
	nested  bool
}

// Fresh owner enrollment is through the actual dispatcher. Archive references
// are born on the second root; no copied signed path is subsequently relocated.
func newMonitorNativeCohortFixture(t *testing.T, segments int) *monitorNativeCohortFixture {
	t.Helper()
	return newMonitorNativeCohortFixtureLayout(t, segments, false)
}

func newMonitorNativeCohortFixtureLayout(t *testing.T, segments int, nested bool) *monitorNativeCohortFixture {
	t.Helper()
	profile := ""
	if nested {
		profile = "nested"
	}
	return newMonitorNativeCohortFixtureNamespace(t, segments, profile)
}

func newMonitorNativeCohortFixtureNamespace(t *testing.T, segments int, profile string) *monitorNativeCohortFixture {
	t.Helper()
	nested := profile != ""
	parent, err := os.MkdirTemp(os.TempDir(), "nc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(parent); err != nil {
			t.Error(err)
		}
	})
	f := &monitorNativeCohortFixture{nested: nested}
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(parent, name)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		f.sources = append(f.sources, newStoragePreparationCommandFixtureAt(t, path))
	}
	physical := durablefixture.New(t, t.Context(), f.sources[0].storage.Roots[0], f.sources[1].storage.Roots[0])
	declaration, err := durablevolume.Load(physical.Reference)
	if err != nil {
		t.Fatal(err)
	}
	native := newMonitorEconomicTestFixture(t, false)
	f.native = native
	native.services.checkpointPath = filepath.Join(f.sources[0].root, "monitor.json")
	native.services.metricsPath = filepath.Join(physical.Roots[0], "monitor.prom")
	native.services.policyPath = filepath.Join(f.sources[0].metadata, "services.json")
	native.policy.HistoryEntries = 1
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x4a}, ed25519.SeedSize))
	native.policy.HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: "sha256:" + strings.Repeat("9", 64), InitialCapacity: monitorHistoryCapacity{Segments: 128, CatalogBytes: 64 * 1024, HeldReaders: 128}}
	checkpoint, _ := monitorEconomicNativePaths(native.services.checkpointPath, native.services.metricsPath, native.policy.Role)
	owners := [][]durablevolume.PreparationOwner{{storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(native.services.checkpointPath), maxRpcReplyBytes), storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(checkpoint), maxRpcReplyBytes)}, nil}
	paths := make([]string, segments)
	for index := range paths {
		name := fmt.Sprintf("a%03d.json", index)
		if nested {
			paths[index] = monitorHistoryRestoreTestPath(t, f.sources[1].root, name, profile)
		} else {
			paths[index] = filepath.Join(f.sources[1].root, name)
			owners[1] = append(owners[1], storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", name, maxRpcReplyBytes))
		}
	}
	if nested {
		owners[1] = []durablevolume.PreparationOwner{storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "retained-peer.json", maxRpcReplyBytes)}
	}
	var prepared []durablevolume.Config
	for index, source := range f.sources {
		source.storage, source.ctx = physical, physical.Context
		storagePreparationOwnerRequest(t, source, "daemon", owners[index])
		raw, err := os.ReadFile(source.requestPath)
		if err != nil {
			t.Fatal(err)
		}
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(raw, &request); err != nil {
			t.Fatal(err)
		}
		request.MountPath = declaration.Volumes[0].MountPath
		request.CapacityProfile = "urnetwork-preparation-many-owners-v1"
		request.Limits = durablevolume.PreparationLimits{MaxEntries: 4096, MaxBytes: 256 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 2048, MaxOwnerAttributeBytes: 8 * 1024 * 1024, MaxPlanBytes: 8 * 1024 * 1024}
		request.MinAvailableBytes, request.MinAvailableInodes = 256*1024*1024, 4096
		raw, err = json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source.requestPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
		source.requestHash = monitorReadDigest(raw)
		ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
		reference, _ := durablevolume.ReferenceFromContext(ctx)
		config, err := durablevolume.Load(reference)
		if err != nil {
			t.Fatal(err)
		}
		prepared = append(prepared, config)
	}
	if nested {
		for _, path := range paths {
			directory, name := filepath.Dir(path), filepath.Base(path)
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			durablefixture.ProvisionSnapshot(t, directory, "mainnet-monitor-checkpoint", name, maxRpcReplyBytes, name+".lock", map[string][]byte{name + ".lock": nil})
		}
	}
	combined := prepared[0]
	combined.Volumes[0].StateRoots = append(combined.Volumes[0].StateRoots, prepared[1].Volumes[0].StateRoots...)
	combined.Volumes[0].StateRoots = append(combined.Volumes[0].StateRoots, declaration.Volumes[0].StateRoots...)
	raw, err := json.Marshal(combined)
	if err != nil {
		t.Fatal(err)
	}
	ref := durablevolume.Reference{Path: filepath.Join(parent, "original-volumes.json"), Sha256: monitorReadDigest(raw)}
	if err := os.WriteFile(ref.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	native.ctx = durablepath.WithHost(durablevolume.WithReference(t.Context(), ref), physical.Host)
	native.services.policy.NativeEconomics = []monitorEconomicNativePolicy{native.policy}
	native.services.writePolicy(t)
	run := native.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current || event.State.Cursor.Number != 101 || event.State.PendingThrough == nil {
		t.Fatal("original pending native prefix absent", event)
	}
	run.stop(t)
	metadata := filepath.Join(parent, "archive-operation")
	if err := os.Mkdir(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	archive := &monitorNativeArchiveFixture{native: native, checkpoint: checkpoint, archive: paths[0], metadata: metadata}
	archive.resetRequest(t)
	catalog := &monitorNativeCatalogFixture{archive: archive, key: key, request: monitorNativeCatalogRequest{Schema: monitorNativeCatalogRequestSchema, Expected: archive.request.Expected, Policy: native.policy, Original: archive.request.Original, FormerWriterFence: archive.request.FormerWriterFence, Capacity: monitorHistoryCapacity{Segments: 512, CatalogBytes: 128 * 1024, HeldReaders: 512}, FutureSegments: 2}}
	plan := catalog.plan(t)
	catalog.apply(t, plan, catalog.approve(t, plan))
	if segments == 1 {
		archive.resetRequest(t)
		_, args := archive.plan(t)
		archive.apply(t, args)
	} else {
		monitorNativeRestoreSeedHistoryPaths(t, native, checkpoint, paths)
	}
	f.record = native.record(t)
	raw, err = os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	f.request = monitorNativeRestoreCohortRequest{Schema: monitorNativeRestoreCohortSchema, Expected: archive.request.Expected, Policy: native.policy, Original: monitorHistoryReference{Path: checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, Limits: durablevolume.PreparationCohortLimits{MaxRoots: 2, MaxPlanBytes: 32 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024, MaxEntries: 8192, MaxBytes: 512 * 1024 * 1024, MaxOwnerAttributes: 4096, MaxOwnerAttributeBytes: 16 * 1024 * 1024}}
	limits := durablevolume.InventoryLimits{MaxEntries: 4096, MaxBytes: 256 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 2048, MaxOwnerAttributeBytes: 8 * 1024 * 1024}
	if profile == "deepest" || profile == "longest" {
		limits.MaxDepth = 32
	}
	for index, source := range f.sources {
		f.files = append(f.files, monitorHistoryRestoreTestFiles(t, source.root, nested))
		target := storageSnapshotRestoreTargetWithLimits(t, source, native.ctx, owners[index][0], false, &limits)
		f.targets = append(f.targets, target)
		raw, err := os.ReadFile(target.target.requestPath)
		if err != nil {
			t.Fatal(err)
		}
		var preparation durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(raw, &preparation); err != nil {
			t.Fatal(err)
		}
		preparation.Limits.MaxDepth = limits.MaxDepth
		if index == 0 {
			preparation.Owners[0].RestoreCoverage = durablevolume.PreparationCompleteUnion
		} else if nested {
			for index := range preparation.Owners {
				preparation.Owners[index].RestoreCoverage = durablevolume.PreparationCompleteUnion
			}
		} else {
			preparation.Owners = nil
		}
		f.request.Preparations = append(f.request.Preparations, preparation)
	}
	return f
}

// The public plan retains exact artifacts; apply takes only its cohort digest.
func (self *monitorNativeCohortFixture) plan(t *testing.T) durablevolume.Reference {
	t.Helper()
	raw, err := json.Marshal(self.request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.targets[0].target.metadata, "cohort-request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	if code := runMain(self.native.ctx, []string{"monitor-native-archive", "restore-cohort-plan", "--request", path, "--request-sha256", monitorReadDigest(raw)}, &output, &diagnostic); code != 0 {
		t.Fatal("public cross-root cohort plan refused", code, diagnostic.String())
	}
	var result durablevolume.PreparationCohortResult
	if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || result.Applied || result.Complete || result.RestartAuthorized || len(result.Roots) != 2 {
		t.Fatal("cohort plan changed target authority", err, result)
	}
	for _, target := range self.targets {
		entries, err := os.ReadDir(target.target.root)
		if err != nil || len(entries) != 0 {
			t.Fatal("cohort plan wrote target", err)
		}
	}
	return result.Cohort
}

// Complete config output is directly consumable by the original runtime.
func (self *monitorNativeCohortFixture) apply(t *testing.T, reference durablevolume.Reference, short bool) context.Context {
	t.Helper()
	args := []string{"storage-prepare", "cohort-apply", "--cohort", reference.Path, "--cohort-sha256", reference.Sha256}
	var output, diagnostic bytes.Buffer
	if short {
		if code := runMain(self.native.ctx, args, storagePreparationShortOutput{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "not fully delivered") {
			t.Fatal("cohort did not reach lost output boundary", code, diagnostic.String())
		}
		diagnostic.Reset()
	}
	if code := runMain(self.native.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("public cohort apply refused", code, diagnostic.String())
	}
	var result durablevolume.PreparationCohortResult
	if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || !result.Applied || !result.Complete || result.RestartAuthorized {
		t.Fatal("cohort completion lost exact scope", err)
	}
	output.Reset()
	diagnostic.Reset()
	args[1] = "cohort-config"
	if code := runMain(self.native.ctx, args, &output, &diagnostic); code != 0 || output.String() != result.DeclarationDocument {
		t.Fatal("read-only combined declaration differs", code, diagnostic.String())
	}
	path := filepath.Join(self.targets[0].target.metadata, "combined-runtime.json")
	if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for index, target := range self.targets {
		if actual := monitorHistoryRestoreTestFiles(t, target.target.root, self.nested); !reflect.DeepEqual(actual, self.files[index]) {
			t.Fatal("cohort changed original file bytes", index)
		}
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), durablevolume.Reference{Path: path, Sha256: result.DeclarationSha256}), self.sources[0].storage.Host)
}

func TestMonitorNativeCohortRestoreKeepsOriginalPendingOutcome(t *testing.T) {
	f := newMonitorNativeCohortFixture(t, 1)
	reference := f.plan(t)
	f.native.ctx = f.apply(t, reference, true)
	if record := f.native.record(t); !reflect.DeepEqual(record, f.record) {
		t.Fatal("restore reset original pending economics")
	}
	run := f.native.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 102 || event.State.BatchCount != 2 || event.State.PendingThrough != nil || event.State.ArchiveSegments != 1 || monitorEconomicTestFee(event) != "12" {
		t.Fatal("cross-root continuation lost original outcome", event)
	}
	run.stop(t)
}

// A later root's copied checkpoint disappears after a valid public plan. The
// first target must retain zero mutations and no usable config may be emitted.
func TestMonitorNativeCohortRestoreRefusesLateRootBeforeEffects(t *testing.T) {
	f := newMonitorNativeCohortFixture(t, 1)
	reference := f.plan(t)
	path := filepath.Join(f.targets[1].archive, "a000.json.lock")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cohort-apply", "cohort-config"} {
		var output, diagnostic bytes.Buffer
		if code := runMain(f.native.ctx, []string{"storage-prepare", mode, "--cohort", reference.Path, "--cohort-sha256", reference.Sha256}, &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("missing later custody admitted", mode, code, diagnostic.String())
		}
	}
	for _, target := range f.targets {
		entries, err := os.ReadDir(target.target.root)
		if err != nil || len(entries) != 0 {
			t.Fatal("invalid later root mutated earlier peer", err)
		}
		if _, err := os.Stat(filepath.Join(target.target.metadata, "preparation.jsonl")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid cohort reserved target", err)
		}
	}
}

// The fact seam refuses only after the first root's real declaration exists.
// All file/attribute writes and journal recovery still use production I/O.
type monitorNativeCohortPressureHost struct {
	durablevolume.Host
	secondRoot           string
	completedDeclaration string
}

func (self *monitorNativeCohortPressureHost) Filesystem(file *os.File) (durablevolume.Filesystem, error) {
	if file.Name() == self.secondRoot {
		if _, err := os.Stat(self.completedDeclaration); err == nil {
			return durablevolume.Filesystem{}, syscall.EIO
		} else if !errors.Is(err, os.ErrNotExist) {
			return durablevolume.Filesystem{}, err
		}
	}
	return self.Host.Filesystem(file)
}

// A joined partial cohort resumes its original second root while the first
// journal and an unrelated live metrics owner retain their original authority.
func TestMonitorNativeCohortRestoreResumesPartialRootsWithHealthyPeer(t *testing.T) {
	f := newMonitorNativeCohortFixture(t, 1)
	reference := f.plan(t)
	old, _ := durablevolume.ReferenceFromContext(f.native.ctx)
	healthy, err := durablevolume.OpenWithHost(old, f.sources[0].storage.Roots[0], durablevolume.ReadWrite, f.sources[0].storage.Host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := healthy.Close(); err != nil {
			t.Error(err)
		}
	})
	pressure := &monitorNativeCohortPressureHost{Host: f.sources[0].storage.Host, secondRoot: f.request.Preparations[1].RootPath, completedDeclaration: f.request.Preparations[0].DeclarationPath}
	ctx := durablepath.WithHost(f.native.ctx, pressure)
	var output, diagnostic bytes.Buffer
	args := []string{"storage-prepare", "cohort-apply", "--cohort", reference.Path, "--cohort-sha256", reference.Sha256}
	if code := runMain(ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrUnavailable.Error()) {
		t.Fatal("partial cohort did not reach selected pressure boundary", code, diagnostic.String())
	}
	control := f.request.Preparations[0].ControlPath
	original, err := os.ReadFile(control)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(original, []byte(reference.Sha256)) {
		t.Fatal("completed root lost its original cohort binding")
	}
	f.native.ctx = f.apply(t, reference, false)
	after, err := os.ReadFile(control)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("resuming peer rewrote completed root journal", err)
	}
	if err := healthy.CheckWrite(); err != nil {
		t.Fatal("unrelated healthy role lost its lease", err)
	}
	run := f.native.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current || event.State.Cursor.Number != 102 || event.State.PendingThrough != nil {
		t.Fatal("partial cohort restart lost original pending outcome", event)
	}
	run.stop(t)
}

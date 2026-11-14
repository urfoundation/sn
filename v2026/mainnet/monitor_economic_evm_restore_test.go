//go:build linux || darwin

// The original evm checkpoint and its archive occupy separate real roots.
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
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A single synthetic mount has two restored roots and untouched metrics roots.
type monitorEvmRestoreFixture struct {
	evm     *monitorEvmFixture
	sources []*storagePreparationCommandFixture
	targets []*storageSnapshotRestoreFixture
	request monitorEvmRestoreCohortRequest
	record  monitorEconomicEvmCheckpoint
	files   []map[string]string
	nested  bool
}

// Fresh owner enrollment is through the actual dispatcher. Archive references
// are born on the second root; no copied signed path is subsequently relocated.
func newMonitorEvmRestoreFixture(t *testing.T, rootCount, segments int) *monitorEvmRestoreFixture {
	t.Helper()
	return newMonitorEvmRestoreFixtureLayout(t, rootCount, segments, false)
}

// Nested originals are born at their final logical paths. Explicit test-only
// fresh enrollment establishes their heads before any original role writes;
// restoration still goes through the production public plan/apply commands.
func newMonitorEvmRestoreFixtureLayout(t *testing.T, rootCount, segments int, nested bool) *monitorEvmRestoreFixture {
	t.Helper()
	profile := ""
	if nested {
		profile = "nested"
	}
	return newMonitorEvmRestoreFixtureNamespace(t, rootCount, segments, profile)
}

func newMonitorEvmRestoreFixtureNamespace(t *testing.T, rootCount, segments int, profile string) *monitorEvmRestoreFixture {
	t.Helper()
	nested := profile != ""
	if rootCount < 1 || rootCount > 2 || segments < 1 || segments > 512 {
		t.Fatal("invalid explicit synthetic restore profile")
	}
	parent, err := os.MkdirTemp(os.TempDir(), "er-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(parent); err != nil {
			t.Error(err)
		}
	})
	f := &monitorEvmRestoreFixture{nested: nested}
	for _, name := range []string{"a", "b"}[:rootCount] {
		path := filepath.Join(parent, name)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		f.sources = append(f.sources, newStoragePreparationCommandFixtureAt(t, path))
	}
	metricsRoots := make([]string, len(f.sources))
	for index, source := range f.sources {
		metricsRoots[index] = source.storage.Roots[0]
	}
	physical := durablefixture.New(t, t.Context(), metricsRoots...)
	declaration, err := durablevolume.Load(physical.Reference)
	if err != nil {
		t.Fatal(err)
	}
	evm := newMonitorEvmFixture(t, "settlement-vault", false)
	f.evm = evm
	evm.services.checkpointPath = filepath.Join(f.sources[0].root, "monitor.json")
	evm.services.metricsPath = filepath.Join(physical.Roots[0], "monitor.prom")
	evm.services.policyPath = filepath.Join(f.sources[0].metadata, "services.json")
	evm.policy.HistoryEntries = 5
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x4a}, ed25519.SeedSize))
	evm.policy.HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: "sha256:" + strings.Repeat("9", 64), InitialCapacity: monitorHistoryCapacity{Segments: 128, CatalogBytes: 64 * 1024, HeldReaders: 128}}
	checkpoint, _ := monitorEconomicEvmPaths(evm.services.checkpointPath, evm.services.metricsPath, evm.policy.Role)
	owners := [][]durablevolume.PreparationOwner{{storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(evm.services.checkpointPath), maxRpcReplyBytes), storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(checkpoint), maxRpcReplyBytes)}, nil}[:rootCount]
	paths := make([]string, segments)
	for index := range paths {
		name := fmt.Sprintf("a%03d.json", index)
		if nested {
			paths[index] = monitorHistoryRestoreTestPath(t, f.sources[rootCount-1].root, name, profile)
		} else {
			paths[index] = filepath.Join(f.sources[rootCount-1].root, name)
			owners[rootCount-1] = append(owners[rootCount-1], storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", name, maxRpcReplyBytes))
		}
	}
	if nested && rootCount == 2 {
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
	for _, config := range prepared[1:] {
		combined.Volumes[0].StateRoots = append(combined.Volumes[0].StateRoots, config.Volumes[0].StateRoots...)
	}
	combined.Volumes[0].StateRoots = append(combined.Volumes[0].StateRoots, declaration.Volumes[0].StateRoots...)
	raw, err := json.Marshal(combined)
	if err != nil {
		t.Fatal(err)
	}
	ref := durablevolume.Reference{Path: filepath.Join(parent, "original-volumes.json"), Sha256: monitorReadDigest(raw)}
	if err := os.WriteFile(ref.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	evm.ctx = durablepath.WithHost(durablevolume.WithReference(t.Context(), ref), physical.Host)
	evm.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{evm.policy}
	evm.services.writePolicy(t)
	run := evm.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current || event.State.Cursor.Number != 11 || event.State.PendingThrough == nil {
		t.Fatal("original pending evm prefix absent", event)
	}
	run.stop(t)
	metadata := filepath.Join(parent, "archive-operation")
	if err := os.Mkdir(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	archive := &monitorEvmArchiveFixture{evm: evm, checkpoint: checkpoint, archive: paths[0], metadata: metadata}
	archive.resetRequest(t)
	catalog := &monitorEvmCatalogFixture{archive: archive, key: key, request: monitorEvmCatalogRequest{Schema: monitorEvmCatalogRequestSchema, Expected: archive.request.Expected, Policy: evm.policy, Original: archive.request.Original, FormerWriterFence: archive.request.FormerWriterFence, Capacity: monitorHistoryCapacity{Segments: 512, CatalogBytes: 128 * 1024, HeldReaders: 512}, FutureSegments: 2}}
	plan := catalog.plan(t)
	catalog.apply(t, plan, catalog.approve(t, plan))
	if segments == 1 {
		archive.resetRequest(t)
		_, args := archive.plan(t)
		archive.apply(t, args)
	} else {
		monitorEvmRestoreSeedHistoryPaths(t, evm, checkpoint, paths)
	}
	f.record = evm.record(t)
	raw, err = os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	f.request = monitorEvmRestoreCohortRequest{Schema: monitorEvmRestoreCohortSchema, Expected: archive.request.Expected, Policy: evm.policy, Original: monitorHistoryReference{Path: checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, Limits: durablevolume.PreparationCohortLimits{MaxRoots: uint64(rootCount), MaxPlanBytes: 32 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024, MaxEntries: 8192, MaxBytes: 512 * 1024 * 1024, MaxOwnerAttributes: 4096, MaxOwnerAttributeBytes: 16 * 1024 * 1024}}
	limits := durablevolume.InventoryLimits{MaxEntries: 4096, MaxBytes: 256 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 2048, MaxOwnerAttributeBytes: 8 * 1024 * 1024}
	if profile == "deepest" || profile == "longest" {
		limits.MaxDepth = 32
	}
	for index, source := range f.sources {
		f.files = append(f.files, f.filesAt(t, source.root))
		target := storageSnapshotRestoreTargetWithLimits(t, source, evm.ctx, owners[index][0], false, &limits)
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

// A nested fixture retains exact bytes under complete relative names. This
// helper does not follow symlinks or hide unknown directories/files.
func (self *monitorEvmRestoreFixture) filesAt(t *testing.T, root string) map[string]string {
	t.Helper()
	return monitorHistoryRestoreTestFiles(t, root, self.nested)
}

func monitorHistoryRestoreTestFiles(t *testing.T, root string, nested bool) map[string]string {
	t.Helper()
	if !nested {
		return bootstrapSuccessorPreparationTestFiles(t, root)
	}
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("nested fixture has a non-regular retained member")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[relative] = string(raw)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

// Large histories are synthetic storage controls, never chain observations.
// Every original head still passes the production ledger and archive grammar.
func monitorEvmRestoreSeedHistoryPaths(t *testing.T, evm *monitorEvmFixture, checkpoint string, paths []string) {
	t.Helper()
	record := evm.record(t)
	client := monitorEvmFixtureClient(t, evm)
	for index, path := range paths {
		if index == 1 {
			// Complete the original pending page through the actual HTTP/ABI/
			// receipt reader before adding synthetic later storage history.
			observation, err := observeMonitorEconomicEvm(evm.ctx, client, evm.policy, &record.State)
			if err != nil {
				t.Fatal("original EVM pending page could not be observed", err)
			}
			next, err := record.State.append(evm.policy, observation, evm.services.clock.now())
			if err != nil || next.PendingThrough != nil || next.Cursor.Number != 13 {
				t.Fatal("original EVM pending page was not completed", err)
			}
			record.State = *next
		} else if index > 1 {
			number := record.State.Cursor.Number + 1
			header := types.CopyHeader(evm.blocks[13].header)
			header.Number, header.ParentHash = new(big.Int).SetUint64(number), common.HexToHash(record.State.Cursor.Hash)
			header.TxHash, header.ReceiptHash, header.GasUsed = types.EmptyTxsHash, types.EmptyReceiptsHash, 0
			header.Bloom = types.Bloom{}
			boundary := economicEmissionBoundary{Number: number, Hash: header.Hash().Hex()}
			snapshot := record.State.Snapshot.clone()
			evm.blocks[number] = &monitorEvmFixtureBlock{header: header, snapshot: snapshot, funded: map[string]string{}}
			evm.byHash[boundary.Hash] = evm.blocks[number]
			block := monitorEvmBlock{Boundary: boundary, ParentHash: record.State.Cursor.Hash, Snapshot: snapshot,
				Events: []monitorEconomicEvmEvent{{Block: boundary, TransactionHash: fmt.Sprintf("0x%064x", number), Name: "CoordinatorFixed", Values: map[string]string{}, ReceiptHash: rootObjectHash(number)}},
				Fees:   []monitorEconomicEvmFee{}, Funded: map[string]string{}}
			observation := &monitorEvmObservation{From: record.State.Cursor, RequestedThrough: boundary, Finalized: boundary, MappingHash: record.State.MappingHash, Before: snapshot, Blocks: []monitorEvmBlock{block}}
			next, err := record.State.append(evm.policy, observation, evm.services.clock.now())
			if err != nil {
				t.Fatal("synthetic original EVM append failed", index, err)
			}
			record.State = *next
		}
		raw, err := encodeMonitorEvmCheckpoint(record)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := openMonitorHistorySnapshot(evm.ctx, path, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(owner.publish(raw, nil), owner.close()); err != nil {
			t.Fatal(err)
		}
		record, err = compactMonitorEconomicEvm(record, monitorHistoryReference{Path: path, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, evm.policy)
		if err != nil {
			t.Fatal("accepted EVM catalog does not fit its signed capacity", index, err)
		}
	}
	evm.setFinalized(t, record.State.Cursor.Number)
	raw, err := encodeMonitorEvmCheckpoint(record)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := openMonitorHistorySnapshot(evm.ctx, checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(owner.publish(raw, nil), owner.close()); err != nil {
		t.Fatal(err)
	}
}

func (self *monitorEvmRestoreFixture) args(t *testing.T, request monitorEvmRestoreCohortRequest) []string {
	t.Helper()
	var value any = request
	mode := "restore-cohort-plan"
	if len(request.Preparations) == 1 {
		mode = "restore-request"
		value = monitorEvmRestoreRequest{Schema: monitorEvmRestoreRequestSchema, Expected: request.Expected, Policy: request.Policy, Original: request.Original, Preparation: request.Preparations[0]}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.targets[0].target.metadata, "evm-history-request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return []string{"monitor-evm-archive", mode, "--request", path, "--request-sha256", monitorReadDigest(raw)}
}

// The valid producer output is consumed by the actual storage dispatcher.
func (self *monitorEvmRestoreFixture) plan(t *testing.T) durablevolume.Reference {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(self.evm.ctx, self.args(t, self.request), &output, &diagnostic); code != 0 {
		t.Fatal("public EVM history restore planning failed", code, diagnostic.String())
	}
	self.unmodifiedTargets(t)
	if len(self.targets) == 1 {
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(output.Bytes(), &request); err != nil {
			t.Fatal(err)
		}
		checkpoints := 0
		for _, owner := range request.Owners {
			if owner.Kind == storageMonitorTreeKind {
				scope, err := storageMonitorTreeProfile(owner, false)
				if err != nil {
					t.Fatal(err)
				}
				checkpoints += len(scope.Snapshots)
			} else if owner.Kind == "mainnet-monitor-checkpoint" {
				checkpoints++
			} else {
				t.Fatal("unexpected EVM fixture owner", owner.Kind)
			}
		}
		if checkpoints != len(self.record.State.Archive.Segments)+2 {
			t.Fatal("EVM request omitted a retained segment or co-owner", checkpoints)
		}
		if err := os.WriteFile(self.targets[0].target.requestPath, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		self.targets[0].target.requestHash = monitorReadDigest(output.Bytes())
		path, hash := storagePreparationFreezeOwnerPlan(t, self.targets[0].target, "storage-prepare")
		return durablevolume.Reference{Path: path, Sha256: hash}
	}
	var result durablevolume.PreparationCohortResult
	if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || result.Applied || result.Complete || result.RestartAuthorized || len(result.Roots) != len(self.targets) {
		t.Fatal("EVM cohort plan changed target authority", err, result)
	}
	return result.Cohort
}

func (self *monitorEvmRestoreFixture) unmodifiedTargets(t *testing.T) {
	t.Helper()
	for _, target := range self.targets {
		entries, err := os.ReadDir(target.target.root)
		if err != nil || len(entries) != 0 {
			t.Fatal("refused or read-only operation wrote a target", err)
		}
		if _, err := os.Stat(filepath.Join(target.target.metadata, "preparation.jsonl")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refused or read-only operation reserved a target", err)
		}
	}
}

func (self *monitorEvmRestoreFixture) apply(t *testing.T, reference durablevolume.Reference, short bool) {
	t.Helper()
	cohort := len(self.targets) != 1
	mode, flagName := "apply", "plan"
	if cohort {
		mode, flagName = "cohort-apply", "cohort"
	}
	args := []string{"storage-prepare", mode, "--" + flagName, reference.Path, "--" + flagName + "-sha256", reference.Sha256}
	var output, diagnostic bytes.Buffer
	if short {
		if code := runMain(self.evm.ctx, args, storagePreparationShortOutput{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "not fully delivered") {
			t.Fatal("EVM restore did not reach completed output-loss boundary", code, diagnostic.String())
		}
		diagnostic.Reset()
	}
	if code := runMain(self.evm.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("public EVM restore apply failed", code, diagnostic.String())
	}
	if cohort {
		var result durablevolume.PreparationCohortResult
		if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || !result.Applied || !result.Complete || result.RestartAuthorized {
			t.Fatal("EVM cohort completion lost exact scope", err, result)
		}
		output.Reset()
		diagnostic.Reset()
		args[1] = "cohort-config"
		if code := runMain(self.evm.ctx, args, &output, &diagnostic); code != 0 || output.String() != result.DeclarationDocument {
			t.Fatal("completed EVM cohort declaration differs", code, diagnostic.String())
		}
		path := filepath.Join(self.targets[0].target.metadata, "combined-runtime.json")
		if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		self.evm.ctx = durablepath.WithHost(durablevolume.WithReference(t.Context(), durablevolume.Reference{Path: path, Sha256: result.DeclarationSha256}), self.sources[0].storage.Host)
	} else {
		var result durablevolume.PreparationResult
		if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || result.RestartAuthorized {
			t.Fatal("EVM single-root completion changed authority", err)
		}
		self.evm.ctx = monitorNativeRestoreContext(t, self.sources[0], durablevolume.WithReference(self.targets[0].target.ctx, result.Declaration))
	}
	for index, target := range self.targets {
		if actual := self.filesAt(t, target.target.root); !reflect.DeepEqual(actual, self.files[index]) {
			t.Fatal("EVM restore changed original member bytes", index)
		}
	}
	if record := self.evm.record(t); !reflect.DeepEqual(record, self.record) {
		t.Fatal("EVM physical restore changed approvals, reviews or economics")
	}
}

func monitorEvmRestorePendingContinues(t *testing.T, f *monitorEvmRestoreFixture) {
	t.Helper()
	run := f.evm.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 13 || event.State.BatchCount != 2 || event.State.PendingThrough != nil || event.State.ArchiveSegments != 1 || event.State.ObservedFeeCostWei == nil || *event.State.ObservedFeeCostWei != "84000" || event.State.ContractState == nil || event.State.ContractState.Counters["totalPaid"] != "15" || event.State.ContractState.Credits[f.evm.policy.Coldkeys[0]] != "0" || event.State.ContractState.Pools["1"] != "0" {
		t.Fatal("restored EVM continuation lost or replayed original credit, carry or fees", event)
	}
	if event.State.NativeFeeExecutionVerified || event.State.IndependentFinalityVerified || event.State.NativeFeeDebitRao != nil || event.State.NativeFeeRefundRao != nil {
		t.Fatal("EVM storage restoration invented independent execution authority", event)
	}
	run.stop(t)
}

func TestMonitorEvmRestorePublicSingleRootRetainsPendingFinancialOutcome(t *testing.T) {
	f := newMonitorEvmRestoreFixture(t, 1, 1)
	f.apply(t, f.plan(t), true)
	monitorEvmRestorePendingContinues(t, f)
}

func TestMonitorEvmRestorePublicCohortRetainsPendingFinancialOutcome(t *testing.T) {
	f := newMonitorEvmRestoreFixture(t, 2, 1)
	f.apply(t, f.plan(t), true)
	monitorEvmRestorePendingContinues(t, f)
}

func TestMonitorEvmRestorePublicCohortRejectsLateSourceBeforeEffects(t *testing.T) {
	f := newMonitorEvmRestoreFixture(t, 2, 1)
	reference := f.plan(t)
	marker := filepath.Join(f.targets[1].archive, "a000.json.lock")
	held := marker + ".held"
	if err := os.Rename(marker, held); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cohort-check", "cohort-apply", "cohort-config"} {
		var output, diagnostic bytes.Buffer
		if code := runMain(f.evm.ctx, []string{"storage-prepare", mode, "--cohort", reference.Path, "--cohort-sha256", reference.Sha256}, &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("missing exact original EVM custody was admitted", mode, code, diagnostic.String())
		}
		f.unmodifiedTargets(t)
	}
	if err := os.Rename(held, marker); err != nil {
		t.Fatal(err)
	}
	f.apply(t, reference, false)
	monitorEvmRestorePendingContinues(t, f)
}

func TestMonitorEvmRestorePublicRefusesChangedAuthorityCapacityAndOutput(t *testing.T) {
	f := newMonitorEvmRestoreFixture(t, 2, 1)
	// A complete valid baseline reaches the actual planner before any fault.
	f.plan(t)
	for _, fault := range []string{"declaration", "cancel", "source", "catalog", "bytes", "heads", "inodes", "unknown-owner", "short-output"} {
		raw, err := json.Marshal(f.request)
		if err != nil {
			t.Fatal(err)
		}
		var request monitorEvmRestoreCohortRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Fatal(err)
		}
		ctx := f.evm.ctx
		switch fault {
		case "declaration":
			ctx = t.Context()
		case "cancel":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		case "source":
			request.Preparations[1].RestoreSource = nil
		case "catalog":
			request.Policy.HistoryCatalog.ReviewSha256 = "sha256:" + strings.Repeat("1", 64)
		case "bytes":
			request.Preparations[1].Limits.MaxBytes = 1
		case "heads":
			request.Preparations[1].Limits.MaxOwnerAttributes = 1
		case "inodes":
			request.Preparations[1].MinAvailableInodes = 1
		case "unknown-owner":
			request.Preparations[1].Owners = append(request.Preparations[1].Owners, durablevolume.PreparationOwner{Kind: "unknown", RelativePath: ".", Purpose: "restore", RestoreCoverage: durablevolume.PreparationCompleteUnion})
		}
		var output, diagnostic bytes.Buffer
		args := f.args(t, request)
		if fault == "short-output" {
			if code := runMain(ctx, args, storagePreparationShortOutput{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "not delivered") {
				t.Fatal("valid EVM plan did not reach output refusal", code, diagnostic.String())
			}
		} else if code := runMain(ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("unreviewed EVM restore authority was admitted", fault, code, diagnostic.String())
		}
		f.unmodifiedTargets(t)
	}
}

func TestMonitorEvmRestoreRechecksFinancialAndReviewAncestry(t *testing.T) {
	f := newMonitorEvmRestoreFixture(t, 1, 2)
	reads := map[string]int{}
	read := func(reference monitorHistoryReference) ([]byte, error) {
		reads[reference.Path]++
		return os.ReadFile(filepath.Join(f.targets[0].archive, filepath.Base(reference.Path)))
	}
	if err := validateMonitorEvmRestoreHistory(f.request.Policy, f.request.Original, read); err != nil {
		t.Fatal("valid complete EVM history was refused", err)
	}
	if len(reads) != 3 {
		t.Fatal("complete original EVM history was not read once", reads)
	}
	for path, count := range reads {
		if count != 1 {
			t.Fatal("unchanged original was authenticated repeatedly", path, count)
		}
	}
	for _, fault := range []string{"fee", "credit", "reviews", "approval", "omitted-prefix"} {
		raw, err := json.Marshal(f.record)
		if err != nil {
			t.Fatal(err)
		}
		var candidate monitorEconomicEvmCheckpoint
		if err := json.Unmarshal(raw, &candidate); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "fee":
			candidate.State.Archive.FeeWei = "42001"
		case "credit":
			candidate.State.Snapshot.Credits[f.evm.policy.Coldkeys[0]] = "11"
		case "reviews":
			candidate.ResourceHistory = nil
		case "approval":
			candidate.State.Catalog.Revisions[0].Signature = strings.Repeat("0", 128)
		case "omitted-prefix":
			candidate.State.Archive.Segments = candidate.State.Archive.Segments[1:]
		}
		raw, err = encodeMonitorEvmCheckpoint(candidate)
		if err != nil {
			t.Fatal(err)
		}
		// Recompute the ordinary checksum so the actual semantic ancestry
		// check, not merely a stale JSON checksum, must reject the change.
		if err := validateMonitorEvmRestoreHistory(f.request.Policy, f.request.Original, func(reference monitorHistoryReference) ([]byte, error) {
			if reference.Path == f.request.Original.Path {
				return raw, nil
			}
			return read(reference)
		}); err == nil {
			t.Fatal("restoration accepted changed original EVM lineage", fault)
		}
		f.unmodifiedTargets(t)
	}
}

func TestMonitorEvmRestorePublicSingleRootInputAndCancellationKeepCustody(t *testing.T) {
	f := newMonitorEvmRestoreFixture(t, 1, 1)
	args := f.args(t, f.request)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.evm.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("valid single-root EVM producer refused", code, diagnostic.String())
	}
	for _, fault := range []string{"declaration", "cancel", "digest", "short-output"} {
		ctx := f.evm.ctx
		current := append([]string(nil), args...)
		switch fault {
		case "declaration":
			ctx = t.Context()
		case "cancel":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		case "digest":
			current[len(current)-1] = "sha256:" + strings.Repeat("1", 64)
		}
		output.Reset()
		diagnostic.Reset()
		if fault == "short-output" {
			if code := runMain(ctx, current, storagePreparationShortOutput{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "not delivered") {
				t.Fatal("single-root EVM producer did not reach output failure", code, diagnostic.String())
			}
		} else if code := runMain(ctx, current, &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("single-root EVM producer admitted unavailable or unreviewed input", fault, code, diagnostic.String())
		}
		f.unmodifiedTargets(t)
	}
}

func TestMonitorEvmRestoreCohortAdmitsAll512OriginalSegments(t *testing.T) {
	f := newMonitorEvmRestoreFixture(t, 2, 512)
	f.apply(t, f.plan(t), false)
	run := f.evm.start(t, monitorServiceHooks{})
	// This is the original policy's bounded admission window, not a sleep or
	// inferred success from file access. Only an actual public sample passes.
	var event monitorEvmTestEvent
	select {
	case event = <-run.sink.events:
	case <-run.done:
		t.Fatal("full EVM archive owner exited during admission", run.exit, run.diagnostic.String())
	case <-time.After(300 * time.Second):
		t.Fatal("full EVM archive public admission exceeded its300-second window")
	}
	if !event.Current || event.State.ArchiveSegments != 512 || event.State.Cursor != f.record.State.Cursor || event.State.BatchCount != f.record.State.BatchCount || event.State.ObservedFeeCostWei == nil || *event.State.ObservedFeeCostWei != "84000" || !reflect.DeepEqual(event.State.ContractState, f.record.State.Snapshot) {
		t.Fatal("full EVM archive continuation lost original financial lineage", event)
	}
	run.stop(t)
}

//go:build linux

// These public restore controls execute the two distinct original-program
// engines. The exported jobs are synthetic; restore grants no chain authority.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

type nativeProducerRestoreFixture struct {
	combined    *economicConservationRestoreFixture
	producer    *nativeProducerPublicFixture
	original    economicEmissionPolicy
	pendingPath string
	proofs      int64
	blocks      int64
}

// The first two completions are acknowledged in a real combined snapshot;
// the third is synced but its acknowledgement is lost. Approvals and jobs are
// on different original roots. Both roots also contain independent snapshots.
func newNativeProducerRestoreFixture(t *testing.T) *nativeProducerRestoreFixture {
	t.Helper()
	return newNativeProducerRestoreOptionsFixture(t, 2, false)
}

// A zero-completion source has a synced input but no replay or accounted job.
// A reviewed source configures its independently signed renewal before the
// first combined checkpoint; it does not rewrite a previously admitted policy.
func newNativeProducerRestoreOptionsFixture(t *testing.T, completed uint64, renewal bool, beforeExport ...func(*nativeProducerRestoreFixture)) *nativeProducerRestoreFixture {
	t.Helper()
	return newNativeProducerRestoreNamespaceFixture(t, completed, renewal, "", beforeExport...)
}

// Native review and artifact names are selected before the first original
// signature or execution. Monitor snapshot limits do not define their paths.
func newNativeProducerRestoreNamespaceFixture(t *testing.T, completed uint64, renewal bool, profile string, beforeExport ...func(*nativeProducerRestoreFixture)) *nativeProducerRestoreFixture {
	t.Helper()
	if profile != "" && profile != "approval" && profile != "runtime" && profile != "large-approvals" {
		t.Fatal("unknown explicit native restore namespace")
	}
	directory := os.Getenv("URNETWORK_NATIVE_CONSERVATION_FIXTURE")
	if !filepath.IsAbs(directory) {
		t.Fatal("combined restore requires original proof-drained contiguous jobs")
	}
	t.Setenv("URNETWORK_NATIVE_PRODUCER_FIXTURE", directory)
	p := nativeProducerPublicFixtureFrom(t, true)
	p.source.set(t, 100, "PendingServerEmission", make([]byte, 8))
	f := &economicConservationRestoreFixture{readFiles: nativeProducerRestoreTestFiles}
	parent := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, parent)
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(parent, name, "state")
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		f.sources = append(f.sources, newStoragePreparationCommandFixtureAt(t, path))
	}
	physical := nativeProducerRestoreObservationFixture(t, f.sources)
	declaration, err := durablevolume.Load(physical.Reference)
	if err != nil {
		t.Fatal(err)
	}
	owners := [][]durablevolume.PreparationOwner{
		{storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "independent-peer.json", maxRpcReplyBytes), storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "combined.json", maxRpcReplyBytes)},
		{storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "independent-peer.json", maxRpcReplyBytes)},
	}
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
		request.Limits = durablevolume.PreparationLimits{MaxEntries: 8192, MaxBytes: 256 * 1024 * 1024, MaxDepth: 8, MaxOwnerAttributes: 128, MaxOwnerAttributeBytes: 128 * 4096, MaxPlanBytes: 8 * 1024 * 1024}
		request.MinAvailableBytes, request.MinAvailableInodes = 256*1024*1024, 8192
		raw, err = json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source.requestPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
		source.requestHash = monitorReadDigest(raw)
		ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
		reference, present := durablevolume.ReferenceFromContext(ctx)
		if !present {
			t.Fatal("native restore prepared declaration absent")
		}
		config, err := durablevolume.Load(reference)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			f.retained = config
		} else {
			f.retained.Volumes[0].StateRoots = append(f.retained.Volumes[0].StateRoots, config.Volumes[0].StateRoots...)
		}
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
	ctx := durablepath.WithHost(durablevolume.WithReference(t.Context(), reference), physical.Host)

	// Select these original custody paths before the first execution. The
	// actual independent signature binds them; no restore-time key enrollment.
	execution := p.source.policy.Execution
	execution.Directory = filepath.Join(f.sources[1].root, "runtime", "native")
	if profile == "runtime" {
		execution.Directory = nativeProducerRestoreLongPath(t, f.sources[1].root, "native")
	}
	execution.Producer.Nodes = filepath.Join(execution.Directory, "nodes")
	if err := os.MkdirAll(execution.Producer.Nodes, 0700); err != nil {
		t.Fatal(err)
	}
	p.authority.Directory, p.authority.Nodes = execution.Directory, execution.Producer.Nodes
	if profile == "large-approvals" {
		// This is a full approval-document census with the original small
		// execution fixture. Unobserved additional identities do not claim a
		// densely populated runtime workload or a production capacity result.
		p.source.policy.MaximumUids = rootCensusLimit
		for index := len(p.authority.Providers); index < rootCensusLimit; index++ {
			p.authority.Providers = append(p.authority.Providers, nativeProducerProvider{Hotkey: fmt.Sprintf("0x%064x", index+65536), Coldkey: fmt.Sprintf("0x%064x", index+131072)})
		}
	}
	message, err := p.authority.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	p.authority.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	approvalPath := filepath.Join(f.sources[0].root, "native-approval.json")
	if profile == "approval" {
		approvalPath = nativeProducerRestoreLongPath(t, f.sources[0].root, strings.Repeat("a", 250)+".json")
		if err := os.MkdirAll(filepath.Dir(approvalPath), 0700); err != nil {
			t.Fatal(err)
		}
	}
	execution.Producer.Authority = nativeRenewalTestWrite(t, approvalPath, p.authority)
	p.ctx = ctx
	writePolicy := func() {
		t.Helper()
		raw, err := json.Marshal(p.source.policy)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.policy, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writePolicy()
	result := &nativeProducerRestoreFixture{combined: f, producer: p, original: p.source.policy}
	source := newEconomicConservationFixture(t, false)
	source.native = p.source
	source.policy.Native.Observation, source.policy.Native.BatchBlocks = result.original, 1
	source.policy.Vault.Network = result.original.Network
	source.checkpoint, source.path = filepath.Join(f.sources[0].root, "combined.json"), filepath.Join(f.sources[0].metadata, "economic-policy.json")
	source.writePolicy(t)
	f.archive = &economicConservationArchiveFixture{source: source, ctx: ctx, metadata: economicConservationTestRestoreMetadata(t, f.sources[0])}
	state := newEconomicConservationState(source.policy)
	for number := uint64(101); number <= 100+completed; number++ {
		if state.Native.ExecutionProducer != nil {
			p.ctx = context.WithValue(ctx, nativeProducerStateKey{}, state.Native.ExecutionProducer)
			p.source.policy.From = state.Native.Cursor
			p.source.policy.Through = economicEmissionBoundary{Number: number, Hash: p.source.chain.byHeight[number]}
			writePolicy()
		}
		// Continuous checkpoints require the actual parent-execution and
		// child-post-state runtime reads. The finite emission CLI deliberately
		// has another envelope and cannot be used as a monitor append input.
		observation, err := observeMonitorEconomicNative(p.ctx, p.source.client, source.policy.Native, &state.Native)
		if err != nil || observation == nil || !observation.Complete || observation.ExecutionProducer == nil {
			t.Fatal("real continuous producer baseline before restore", number, err)
		}
		for _, block := range observation.Blocks {
			if block.ExecutionRuntime == nil || block.PostStateRuntime == nil {
				t.Fatal("real continuous baseline omitted original runtime read context")
			}
		}
		if renewal && number == 101 {
			// This reviewed config remains outside both copied durable roots.
			// Its exact pin/signature must remain readable after restoration.
			nativeRenewalTestPublicReview(t, p, observation.ExecutionProducer)
			source.writePolicy(t)
			state = newEconomicConservationState(source.policy)
		}
		if err := state.appendNative(ctx, source.policy, observation, source.now); err != nil {
			t.Fatal(err)
		}
	}
	observeEconomicConservationTestClaims(t, ctx, source, state)
	state.ContentHash = state.hash()
	if err := state.validate(ctx, source.policy); err != nil {
		t.Fatal("original combined checkpoint", err)
	}
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	snapshot, err := openMonitorHistorySnapshot(ctx, source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(snapshot.publish(raw, nil), snapshot.close()); err != nil {
		t.Fatal(err)
	}
	// The expected checkpoint is the exact original durable representation,
	// including a first pending capture with no completed native window.
	// Private in-memory indexes and time locations are not restored evidence.
	f.state = source.state(t)
	if f.state.ContentHash != state.ContentHash || f.state.hash() != state.hash() {
		t.Fatal("original published restore checkpoint differs from constructed evidence")
	}
	f.request = economicConservationRestoreRequest{Schema: economicConservationRestoreSchema, Policy: source.policy, Original: monitorHistoryReference{Path: source.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, Limits: durablevolume.PreparationCohortLimits{MaxRoots: 2, MaxPlanBytes: 32 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024, MaxEntries: 16384, MaxBytes: 512 * 1024 * 1024, MaxOwnerAttributes: 256, MaxOwnerAttributeBytes: 256 * 4096}}
	// A real failed directory-sync acknowledgement leaves the completed job
	// visible, while the original accounting checkpoint remains at child102.
	p.source.policy.From, p.source.policy.Through = state.Native.Cursor, economicEmissionBoundary{Number: 101 + completed, Hash: p.source.chain.byHeight[101+completed]}
	writePolicy()
	var reached bool
	boundary := "/complete.json"
	if completed == 0 {
		boundary = "/input.json"
	}
	p.ctx = context.WithValue(context.WithValue(ctx, nativeProducerStateKey{}, state.Native.ExecutionProducer), nativeProducerSyncKey{}, func(relative string, directory *os.File) error {
		if strings.HasSuffix(relative, boundary) {
			reached = true
			return errors.Join(directory.Sync(), syscall.EIO)
		}
		return directory.Sync()
	})
	_, code, issue := p.command(t)
	if code == 0 || !reached || !strings.Contains(issue, syscall.EIO.Error()) {
		t.Fatal("real completion acknowledgement loss did not occur", code, issue)
	}
	result.pendingPath = filepath.Join(execution.Directory, "nodes", strings.Repeat("e", 64)+".pending")
	if err := os.WriteFile(result.pendingPath, []byte{1, 2, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	result.proofs, result.blocks = p.proofs.Load(), p.blocks.Load()
	// Successor controls may perform a real public adoption before the source
	// snapshot. Refresh exact original bytes; restore still derives every owner.
	for _, configure := range beforeExport {
		configure(result)
	}
	if len(beforeExport) != 0 {
		f.state = source.state(t)
		raw, err := os.ReadFile(source.checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		f.request.Original = monitorHistoryReference{Path: source.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}
	}
	limits := durablevolume.InventoryLimits{MaxEntries: 8192, MaxBytes: 256 * 1024 * 1024, MaxDepth: 8, MaxOwnerAttributes: 128, MaxOwnerAttributeBytes: 128 * 4096}
	if profile != "" {
		limits.MaxDepth = 32
	}
	for index, sourceRoot := range f.sources {
		requireEconomicConservationTestPreparation(t, sourceRoot)
		f.files = append(f.files, nativeProducerRestoreTestFiles(t, sourceRoot.root))
		target := storageSnapshotRestoreTargetWithLimits(t, sourceRoot, ctx, owners[index][0], false, &limits)
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
		f.request.Preparations = append(f.request.Preparations, request)
	}
	return result
}

// Only existing observation roots carry the initial durable declaration. The
// future preparation targets must retain no owner generation until public apply.
func nativeProducerRestoreObservationFixture(t *testing.T, sources []*storagePreparationCommandFixture) *durablefixture.Fixture {
	t.Helper()
	roots := make([]string, len(sources))
	for index, source := range sources {
		roots[index] = source.storage.Roots[0]
	}
	physical := durablefixture.New(t, t.Context(), roots...)
	declaration, err := durablevolume.Load(physical.Reference)
	if err != nil || len(declaration.Volumes) != 1 {
		t.Fatal("common fixture observation volume unavailable", err)
	}
	for _, source := range sources {
		raw, err := os.ReadFile(source.requestPath)
		if err != nil {
			t.Fatal(err)
		}
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(raw, &request); err != nil {
			t.Fatal(err)
		}
		// The fixture now observes the common mount, before either target is
		// prepared. Keep every original owner/path and bind that actual mount.
		request.MountPath = declaration.Volumes[0].MountPath
		request.FilesystemUuid = declaration.Volumes[0].FilesystemUuid
		request.FilesystemType = declaration.Volumes[0].FilesystemType
		raw, err = json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source.requestPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
		source.requestHash = monitorReadDigest(raw)
	}
	return physical
}

func TestNativeProducerRestorePublicCohortResumesOriginalUnacknowledgedJob(t *testing.T) {
	f := newNativeProducerRestoreFixture(t)
	work := map[string]uint64{}
	f.combined.archive.ctx = context.WithValue(f.combined.archive.ctx, nativeProducerRestoreWorkKey{}, func(stage string, units uint64) { work[stage] += units })
	plan := f.combined.plan(t)
	if work["completion-decoded"] != 3 || work["job-decoded"] != 3 || work["pending-retained"] != 1 || work["trie-node-verified"] == 0 || work["finality-window-decoded"] != 1 {
		t.Fatal("complete source review omitted or repeated original work", work)
	}
	f.combined.apply(t, plan, true)
	p := f.producer
	p.ctx = context.WithValue(f.combined.archive.ctx, nativeProducerStateKey{}, f.combined.state.Native.ExecutionProducer)
	before := mainnetNamespaceTest(t, p.source.policy.Execution.Directory)
	observation, code, issue := p.command(t)
	if code != 0 || !observation.Complete || observation.ExecutionProducer == nil || observation.ExecutionProducer.Completed != 3 || observation.ExecutionProducer.Cursor.Number != 103 || observation.ExecutionWindow == nil || observation.ExecutionWindow.MinerAllocation != "0" {
		t.Fatal("restored producer did not resume original unacknowledged job", code, issue, observation.ExecutionProducer)
	}
	if p.proofs.Load() != f.proofs || p.blocks.Load()-f.blocks != 1 || !reflect.DeepEqual(before, mainnetNamespaceTest(t, p.source.policy.Execution.Directory)) {
		t.Fatal("restore recaptured original proof or rewrote pending custody", p.proofs.Load()-f.proofs, p.blocks.Load()-f.blocks)
	}
	if observation.TargetMet != nil || observation.ActivationReady || observation.ActualNativeOutcomeVerified {
		t.Fatal("custody restoration minted economic authority")
	}
	if raw, err := os.ReadFile(f.pendingPath); err != nil || !bytes.Equal(raw, []byte{1, 2, 3}) {
		t.Fatal("unknown pending trie bytes were discarded", err)
	}
}

func TestNativeProducerRestorePublicRefusesOmittedApprovalOrArtifactRoot(t *testing.T) {
	f := newNativeProducerRestoreFixture(t)
	for _, omitted := range []int{0, 1} {
		request := f.combined.request
		request.Preparations = append([]durablevolume.PreparationRequest(nil), request.Preparations[1-omitted])
		var output, diagnostic bytes.Buffer
		args := f.combined.args(t, request)
		args[1] = "restore-request"
		if code := runMain(f.combined.archive.ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("omitted original checkpoint/approval/artifact root produced a plan", omitted, code, diagnostic.String())
		}
		f.combined.unchanged(t)
	}
}

func TestNativeProducerRestorePublicIncompleteFirstCaptureRemainsUnaccounted(t *testing.T) {
	f := newNativeProducerRestoreOptionsFixture(t, 0, false)
	if f.combined.state.Native.ExecutionProducer != nil || f.combined.state.Native.BatchCount != 0 {
		t.Fatal("incomplete first capture became an accounted job")
	}
	work := map[string]uint64{}
	f.combined.archive.ctx = context.WithValue(f.combined.archive.ctx, nativeProducerRestoreWorkKey{}, func(stage string, units uint64) { work[stage] += units })
	plan := f.combined.plan(t)
	if work["completion-decoded"] != 0 || work["job-decoded"] != 0 || work["pending-retained"] != 1 || work["member-read"] == 0 {
		t.Fatal("unfinished source was discarded or turned into a completion", work)
	}
	f.combined.apply(t, plan, true)
	p := f.producer
	p.ctx = f.combined.archive.ctx
	observation, code, issue := p.command(t)
	if code != 0 || !observation.Complete || observation.ExecutionProducer == nil || observation.ExecutionProducer.Completed != 1 || observation.ExecutionProducer.Cursor.Number != 101 {
		t.Fatal("restored first capture did not resume its original job", code, issue)
	}
	if raw, err := os.ReadFile(f.pendingPath); err != nil || !bytes.Equal(raw, []byte{1, 2, 3}) {
		t.Fatal("unfinished source bytes disappeared during real resume", err)
	}
}

func TestNativeProducerRestorePublicRenewalRetainsOriginalExternalApproval(t *testing.T) {
	f := newNativeProducerRestoreOptionsFixture(t, 2, true)
	prior := f.combined.state.Native.ExecutionProducer
	if prior == nil || len(prior.AuthorityRevisions) != 1 || prior.AuthorityRevisions[0].Capacity.Jobs <= f.producer.authority.MaximumJobs {
		t.Fatal("real source did not acknowledge monotonic original renewal", prior)
	}
	reference := f.producer.source.policy.Execution.Producer.Renewals[0]
	for _, source := range f.combined.sources {
		if _, inside := monitorHistoryRestoreRelative(source.root, reference.Path); inside {
			t.Fatal("external-approval fixture fell inside copied namespace")
		}
	}
	approval, err := os.ReadFile(reference.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Missing external config remains an unavailable original input. Restore
	// cannot synthesize it from a copied checkpoint or invent another signer.
	if err := os.Rename(reference.Path, reference.Path+".retained"); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMain(f.combined.archive.ctx, f.combined.args(t, f.combined.request), &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "no such file") {
		t.Fatal("missing original external approval granted restoration", code, diagnostic.String())
	}
	f.combined.unchanged(t)
	if err := os.Rename(reference.Path+".retained", reference.Path); err != nil {
		t.Fatal(err)
	}
	plan := f.combined.plan(t)
	f.combined.apply(t, plan, true)
	p := f.producer
	p.ctx = context.WithValue(f.combined.archive.ctx, nativeProducerStateKey{}, prior)
	observation, code, issue := p.command(t)
	if code != 0 || observation.ExecutionProducer == nil || observation.ExecutionProducer.Completed != 3 || observation.ExecutionProducer.AuthorityHash != reference.Sha256 || !reflect.DeepEqual(observation.ExecutionProducer.AuthorityRevisions, prior.AuthorityRevisions) {
		t.Fatal("restored renewal lost original cumulative jobs or acknowledgements", code, issue)
	}
	retained, err := os.ReadFile(reference.Path)
	if err != nil || !bytes.Equal(retained, approval) {
		t.Fatal("restore rewrote external independent approval", err)
	}
}

func TestNativeProducerRestorePublicChangedPendingOrNodeBytesRefuseBeforeEffects(t *testing.T) {
	f := newNativeProducerRestoreFixture(t)
	root := f.combined.targets[1].archive
	var paths []string
	for path := range f.combined.files[1] {
		if strings.Contains(path, "/nodes/") && !strings.HasSuffix(path, ".pending") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) < 1 {
		t.Fatal("real backend did not retain nodes and a pending member", paths)
	}
	for _, path := range []string{paths[0], filepath.Join("runtime", "native", "nodes", strings.Repeat("e", 64)+".pending")} {
		copied := filepath.Join(root, path)
		raw, err := os.ReadFile(copied)
		if err != nil || len(raw) == 0 {
			t.Fatal(err)
		}
		changed := append([]byte(nil), raw...)
		changed[0] ^= 1
		if err := os.WriteFile(copied, changed, 0600); err != nil {
			t.Fatal(err)
		}
		var output, diagnostic bytes.Buffer
		if code := runMain(f.combined.archive.ctx, f.combined.args(t, f.combined.request), &output, &diagnostic); code == 0 || output.Len() != 0 {
			t.Fatal("changed original native member reached staging", path, code, diagnostic.String())
		}
		f.combined.unchanged(t)
		if err := os.WriteFile(copied, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// Real callback cancellation occurs after one copied member read. No partial
// source review becomes a restore plan, and a fresh owner can review unchanged
// originals without erasing the canceled attempt.
func TestNativeProducerRestorePublicCancellationKeepsOriginalPendingCustody(t *testing.T) {
	f := newNativeProducerRestoreFixture(t)
	ctx, cancel := context.WithCancel(f.combined.archive.ctx)
	defer cancel()
	var reads uint64
	ctx = context.WithValue(ctx, nativeProducerRestoreWorkKey{}, func(stage string, units uint64) {
		if stage == "member-read" {
			reads += units
			cancel()
		}
	})
	var output, diagnostic bytes.Buffer
	if code := runMain(ctx, f.combined.args(t, f.combined.request), &output, &diagnostic); code == 0 || reads != 1 || output.Len() != 0 || !strings.Contains(diagnostic.String(), context.Canceled.Error()) {
		t.Fatal("source cancellation changed original custody", code, reads, diagnostic.String())
	}
	f.combined.unchanged(t)
	f.combined.plan(t)
}

// A rehashed inventory does not supersede the separately pinned original
// accounting checkpoint. The later unacknowledged link is changed consistently
// so the assertion isolates the original checkpoint/completion binding.
func TestNativeProducerRestorePublicSelfSealedHistoryCannotReplaceCheckpoint(t *testing.T) {
	f := newNativeProducerRestoreFixture(t)
	var previous string
	for number := uint64(102); number <= 103; number++ {
		path := filepath.Join("runtime", "native", fmt.Sprintf("b%010d-%s", number, strings.TrimPrefix(f.producer.source.chain.byHeight[number], "0x")), "complete.json")
		raw, err := os.ReadFile(filepath.Join(f.combined.targets[1].archive, path))
		if err != nil {
			t.Fatal(err)
		}
		var completion nativeProducerCompletion
		if err := decodePlanJson(raw, &completion); err != nil {
			t.Fatal(err)
		}
		if number == 102 {
			completion.OutcomeHash = monitorReadDigest([]byte("synthetic different original outcome"))
		} else {
			completion.Previous = previous
		}
		previous = rootObjectHash(completion)
		raw, err = json.Marshal(completion)
		if err != nil {
			t.Fatal(err)
		}
		f.replaceReviewedMember(t, 1, path, append(raw, '\n'))
	}
	var output, diagnostic bytes.Buffer
	if code := runMain(f.combined.archive.ctx, f.combined.args(t, f.combined.request), &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "exact acknowledged checkpoint") {
		t.Fatal("self-sealed completion history replaced original checkpoint", code, diagnostic.String())
	}
	f.combined.unchanged(t)
}

func TestNativeProducerRestorePublicSelfSealedNodeStillNeedsContentAddress(t *testing.T) {
	f := newNativeProducerRestoreFixture(t)
	var path string
	for name := range f.combined.files[1] {
		if strings.Contains(name, "/nodes/") && !strings.HasSuffix(name, ".pending") && (path == "" || name < path) {
			path = name
		}
	}
	if path == "" {
		t.Fatal("real backend fixture has no retained content-addressed node")
	}
	raw := []byte(f.combined.files[1][path])
	raw[0] ^= 1
	f.replaceReviewedMember(t, 1, path, raw)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.combined.archive.ctx, f.combined.args(t, f.combined.request), &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "content address") {
		t.Fatal("self-sealed copied node bypassed original content address", code, diagnostic.String())
	}
	f.combined.unchanged(t)
}

func TestNativeProducerRestorePublicPendingJobCannotReplaceCapturedInput(t *testing.T) {
	f := newNativeProducerRestoreFixture(t)
	directory := filepath.Join("runtime", "native", fmt.Sprintf("b%010d-%s", 103, strings.TrimPrefix(f.producer.source.chain.byHeight[103], "0x")))
	var job historicalReplayJob
	if err := decodePlanJson([]byte(f.combined.files[1][filepath.Join(directory, "job.json")]), &job); err != nil {
		t.Fatal(err)
	}
	job.ParentHash[0] ^= 0x80
	jobRaw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	var completion nativeProducerCompletion
	if err := decodePlanJson([]byte(f.combined.files[1][filepath.Join(directory, "complete.json")]), &completion); err != nil {
		t.Fatal(err)
	}
	completion.Admission.Job.Sha256 = monitorReadDigest(jobRaw)
	completionRaw, err := json.Marshal(completion)
	if err != nil {
		t.Fatal(err)
	}
	f.replaceReviewedMember(t, 1, filepath.Join(directory, "job.json"), jobRaw)
	f.replaceReviewedMember(t, 1, filepath.Join(directory, "complete.json"), completionRaw)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.combined.archive.ctx, f.combined.args(t, f.combined.request), &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "exact original capture input") {
		t.Fatal("self-sealed pending job replaced its original captured input", code, diagnostic.String())
	}
	f.combined.unchanged(t)
}

// A fault producer can rehash its own exported bytes. The fixed semantic
// owner still has to reproduce the independently pinned execution lineage.
func (self *nativeProducerRestoreFixture) replaceReviewedMember(t *testing.T, root int, path string, raw []byte) {
	t.Helper()
	request := &self.combined.request.Preparations[root]
	report, err := durablevolume.LoadPhysicalInventory(self.combined.archive.ctx, request.RestoreSource.Inventory)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for index := range report.Entries {
		entry := &report.Entries[index]
		if entry.Path != path {
			continue
		}
		found = true
		report.TotalBytes = report.TotalBytes - entry.Size + uint64(len(raw))
		entry.Size, entry.Sha256 = uint64(len(raw)), monitorReadDigest(raw)
	}
	if !found {
		t.Fatal("mutated source is outside original exported inventory", path)
	}
	if err := os.WriteFile(filepath.Join(self.combined.targets[root].archive, path), raw, 0600); err != nil {
		t.Fatal(err)
	}
	reportRaw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(self.combined.targets[root].target.metadata, "changed-inventory-"+strings.TrimPrefix(monitorReadDigest(reportRaw), "sha256:")+".json")
	if err := os.WriteFile(path, reportRaw, 0600); err != nil {
		t.Fatal(err)
	}
	request.RestoreSource.Inventory = durablevolume.Reference{Path: path, Sha256: monitorReadDigest(reportRaw)}
}

func TestNativeProducerRestoreGrammarKeepsPartialEvidenceAndRejectsForeignMembers(t *testing.T) {
	boundary := "b0000000101-" + strings.Repeat("a", 64)
	for _, path := range []string{"nodes/" + strings.Repeat("b", 64) + ".pending", "intents/0000000101.json.pending", boundary + "/job.json.pending", boundary + "/finality/.strecovery-" + strings.Repeat("c", 32) + ".tmp"} {
		maximum, pending, ok := storageNativeProducerMember(path)
		if !ok || !pending || maximum <= 0 {
			t.Fatal("original incomplete evidence lost fixed retention grammar", path)
		}
	}
	for _, path := range []string{"../outside", "nodes/" + strings.Repeat("B", 64), boundary + "/approval.json", boundary + "/finality/request-00000.json", boundary + "/finality/read-32769.json"} {
		if _, _, ok := storageNativeProducerMember(path); ok {
			t.Fatal("unknown member obtained producer ownership", path)
		}
	}
	if _, err := storageNativeProducerAuthorities(nil, storageNativeProducerScope{}); err == nil {
		t.Fatal("absent restore context acquired approval")
	}
}

func TestNativeProducerRestoreOriginalSignedApprovalAndCursorStayIndependent(t *testing.T) {
	ctx, policy, original, _, files := nativeRenewalTestInputs(t)
	defer func() {
		if err := files.close(); err != nil {
			t.Fatal(err)
		}
	}()
	scope := storageNativeProducerScope{Schema: storageNativeProducerSchema, Policy: policy, Cursor: policy.From, Checkpoint: monitorHistoryReference{Path: filepath.Join(filepath.Dir(files.path), "checkpoint.json"), Sha256: monitorReadDigest([]byte("synthetic original checkpoint")), Bytes: 29}}
	for _, reference := range append([]planFileReference{policy.Execution.Producer.Authority}, policy.Execution.Producer.Renewals...) {
		raw, err := nativeProducerReadApproval(ctx, reference)
		if err != nil {
			t.Fatal(err)
		}
		scope.Approvals = append(scope.Approvals, raw)
	}
	loaded, err := storageNativeProducerAuthorities(ctx, scope)
	if err != nil || !reflect.DeepEqual(loaded, original) {
		t.Fatal("copied signed approvals differ from original reader", err)
	}
	for _, fault := range []string{"original", "renewal", "cursor", "removed"} {
		changed := scope
		changed.Approvals = append([][]byte(nil), scope.Approvals...)
		switch fault {
		case "original", "renewal":
			index := 0
			if fault == "renewal" {
				index = 1
			}
			changed.Approvals[index] = append([]byte(nil), changed.Approvals[index]...)
			changed.Approvals[index][0] ^= 1
		case "cursor":
			changed.Cursor = policy.Through
		case "removed":
			changed.Approvals = changed.Approvals[:1]
		}
		if _, err := storageNativeProducerAuthorities(ctx, changed); err == nil {
			t.Fatal("copied approval or lost accounting acquired new authority", fault)
		}
	}
}

func TestNativeProducerRestoreCopiedReadKeepsIoUnknownAndExactMismatchDistinct(t *testing.T) {
	root := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, root)
	raw := []byte("synthetic original native artifact")
	path := filepath.Join(root, "member.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	member := durablevolume.PreparationFile{Path: "member.json", Kind: "file", Mode: 0600, Bytes: uint64(len(raw)), Sha256: monitorReadDigest(raw)}
	got, err := readStorageNativeProducerMember(t.Context(), file, member)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("actual protected copied read", err)
	}
	changed := append([]byte(nil), raw...)
	changed[0] ^= 1
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readStorageNativeProducerMember(t.Context(), file, member); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("fully returned differing bytes lost identity refusal", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readStorageNativeProducerMember(t.Context(), file, member); err == nil || errors.Is(err, durablevolume.ErrIdentity) || !errors.Is(err, syscall.EBADF) {
		t.Fatal("unobserved closed descriptor invented identity conflict", err)
	}
}

// The selected Substrate layout uses u32 block heights even though local
// counters are uint64. Refusal must precede reader effects or wrapped intervals.
func TestNativeProducerRestoreHeightBoundaryNeverWrapsPendingInterval(t *testing.T) {
	ctx, policy, authorities, _, files := nativeRenewalTestInputs(t)
	defer func() {
		if err := files.close(); err != nil {
			t.Fatal(err)
		}
	}()
	authorities = authorities[:1]
	authorities[0].value.From.Number = math.MaxUint32
	scope := storageNativeProducerScope{Policy: policy, Cursor: authorities[0].value.From}
	reads := 0
	read := func(durablevolume.PreparationFile) ([]byte, error) {
		reads++
		return nil, errors.New("unexpected original member read")
	}
	if err := validateStorageNativeProducerHistory(ctx, scope, authorities, ".", nil, read); err != nil || reads != 0 {
		t.Fatal("terminal original height invented another interval", err, reads)
	}
	for _, number := range []uint64{math.MaxUint32 + 1, math.MaxUint64} {
		changed := scope
		changed.Cursor.Number = number
		changed.State = &nativeExecutionProducerState{Completed: number - math.MaxUint32}
		if err := validateStorageNativeProducerHistory(ctx, changed, authorities, ".", nil, read); err == nil || reads != 0 {
			t.Fatal("out-of-layout cursor reached source work", number, err, reads)
		}
	}
	var principal *nativePrincipalPolicy
	if principal.queriesAt(scope.Cursor) != nil || principal.effectsAt(scope.Cursor) {
		t.Fatal("absent original principal authority fabricated a query")
	}
}

// Keep exact original payloads through nested artifacts; directory custody is
// independently checked by the real exported inventory and restore core.
func nativeProducerRestoreTestFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("fixture contains a nonregular original artifact")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
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

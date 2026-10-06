//go:build linux

// Actual public preparation, archive, restore and monitor continuation surround
// synthetic claims. Original paths, unresolved amounts and review history stay
// unchanged; no storage result grants financial or restart authority.
package main

import (
	"bytes"
	"context"
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
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

type monitorClaimRestoreFixture struct {
	claim          *monitorClaimArchiveFixture
	sources        []*storagePreparationCommandFixture
	targets        []*storageSnapshotRestoreFixture
	request        monitorClaimRestoreCohortRequest
	record         monitorClaimCheckpointRecord
	files          []map[string]string
	expectedOwners int
	retained       durablevolume.Config
}

// Snapshot enrollment runs through storage-prepare. The archive is created at
// its final original root before any backup, never rewritten after signing.
func newMonitorClaimRestoreFixture(t *testing.T, rootCount int) *monitorClaimRestoreFixture {
	return newMonitorClaimRestoreMutatedFixture(t, rootCount, nil)
}

// A negative fixture changes source bytes through the real original writer
// before export, so refusal exercises semantic history rather than a bad copy.
func newMonitorClaimRestoreMutatedFixture(t *testing.T, rootCount int, mutate func(*monitorClaimArchiveFixture)) *monitorClaimRestoreFixture {
	return newMonitorClaimRestoreConfiguredFixture(t, rootCount, nil, nil, mutate)
}

// Window controls provision every future archive owner before original use.
func newMonitorClaimRestoreConfiguredFixture(t *testing.T, rootCount int, catalog *monitorHistoryCatalogPolicy, beforeArchive, afterArchive func(*monitorClaimArchiveFixture)) *monitorClaimRestoreFixture {
	t.Helper()
	if rootCount < 1 || rootCount > 2 {
		t.Fatal("invalid explicit Claim restore roots")
	}
	f := &monitorClaimRestoreFixture{}
	parent := t.TempDir()
	for _, name := range []string{"a", "b"}[:rootCount] {
		path := filepath.Join(parent, name, "state")
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		f.sources = append(f.sources, newStoragePreparationCommandFixtureAt(t, path))
	}
	owners := make([][]durablevolume.PreparationOwner, rootCount)
	f.claim = newMonitorClaimArchivePreparedFixture(t, catalog, 2, func(claim *monitorClaimArchiveFixture) context.Context {
		if catalog != nil {
			claim.policy.EpochCapacity, claim.policy.ReviewHistoryEntries = maximumMonitorRetainedClaimEpochs, maximumMonitorProgressReviews
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
		claim.services.checkpointPath = filepath.Join(f.sources[0].root, "monitor.json")
		claim.services.metricsPath = filepath.Join(physical.Roots[0], "monitor.prom")
		claim.services.policyPath = filepath.Join(f.sources[0].metadata, "services.json")
		checkpoint, _ := monitorClaimPaths(claim.services.checkpointPath, claim.services.metricsPath, claim.policy.Role)
		claim.archive = filepath.Join(f.sources[rootCount-1].root, "claim-archive.json")
		owners[0] = []durablevolume.PreparationOwner{
			storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(claim.services.checkpointPath), maxRpcReplyBytes),
			storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(checkpoint), maxRpcReplyBytes),
		}
		owners[rootCount-1] = append(owners[rootCount-1], storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", filepath.Base(claim.archive), maxRpcReplyBytes))
		if catalog != nil {
			owners[rootCount-1] = append(owners[rootCount-1], storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "claim-window.json", maxRpcReplyBytes))
		}
		for _, group := range owners {
			f.expectedOwners += len(group)
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
			request.Limits = durablevolume.PreparationLimits{MaxEntries: 256, MaxBytes: 64 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 32, MaxOwnerAttributeBytes: 128 * 1024, MaxPlanBytes: 1024 * 1024}
			request.MinAvailableBytes, request.MinAvailableInodes = 64*1024*1024, 256
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
				t.Fatal("prepared Claim declaration absent")
			}
			config, err := durablevolume.Load(reference)
			if err != nil {
				t.Fatal(err)
			}
			prepared = append(prepared, config)
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
		reference := durablevolume.Reference{Path: filepath.Join(parent, "original-volumes.json"), Sha256: monitorReadDigest(raw)}
		if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return durablepath.WithHost(durablevolume.WithReference(t.Context(), reference), physical.Host)
	})
	if beforeArchive != nil {
		beforeArchive(f.claim)
		f.claim.resetRequest(t)
	}
	_, args := f.claim.plan(t)
	f.claim.apply(t, args)
	if afterArchive != nil {
		afterArchive(f.claim)
	}
	f.record = f.claim.record(t)
	reference, present := durablevolume.ReferenceFromContext(f.claim.ctx)
	if !present {
		t.Fatal("prepared Claim source declaration is absent")
	}
	var err error
	f.retained, err = durablevolume.Load(reference)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(f.claim.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	f.request = monitorClaimRestoreCohortRequest{Schema: monitorClaimRestoreCohortSchema, Expected: f.claim.request.Expected,
		Policy: f.claim.policy, Original: monitorHistoryReference{Path: f.claim.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))},
		Limits: durablevolume.PreparationCohortLimits{MaxRoots: uint64(rootCount), MaxPlanBytes: 8 * 1024 * 1024, MaxControlBytes: 16 * 1024 * 1024, MaxEntries: 512, MaxBytes: 128 * 1024 * 1024, MaxOwnerAttributes: 64, MaxOwnerAttributeBytes: 256 * 1024}}
	for index, source := range f.sources {
		f.files = append(f.files, bootstrapSuccessorPreparationTestFiles(t, source.root))
		target := storageSnapshotRestoreTarget(t, source, f.claim.ctx, owners[index][0], false)
		f.targets = append(f.targets, target)
		raw, err := os.ReadFile(target.target.requestPath)
		if err != nil {
			t.Fatal(err)
		}
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(raw, &request); err != nil {
			t.Fatal(err)
		}
		matched := false
		for _, volume := range f.retained.Volumes {
			if volume.MountPath == request.MountPath {
				matched = true
				if volume.MinAvailableBytes < request.MinAvailableBytes || volume.MinAvailableInodes < request.MinAvailableInodes {
					t.Fatal("prepared restore reserve was reduced before source export", volume.MinAvailableBytes, volume.MinAvailableInodes, request.MinAvailableBytes, request.MinAvailableInodes)
				}
			}
		}
		if !matched {
			t.Fatal("restore request lost its original mount declaration")
		}
		if index == 0 {
			request.Owners[0].RestoreCoverage = durablevolume.PreparationCompleteUnion
		} else {
			request.Owners = nil
		}
		f.request.Preparations = append(f.request.Preparations, request)
	}
	return f
}

func (self *monitorClaimRestoreFixture) args(t *testing.T, request monitorClaimRestoreCohortRequest) []string {
	t.Helper()
	var value any = request
	mode := "restore-cohort-plan"
	if len(request.Preparations) == 1 {
		mode = "restore-request"
		value = monitorClaimRestoreRequest{Schema: monitorClaimRestoreRequestSchema, Expected: request.Expected, Policy: request.Policy, Original: request.Original, Preparation: request.Preparations[0]}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.targets[0].target.metadata, "claim-history-request.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return []string{"monitor-claim-archive", mode, "--request", path, "--request-sha256", monitorReadDigest(raw)}
}

func (self *monitorClaimRestoreFixture) unmodifiedTargets(t *testing.T) {
	t.Helper()
	for _, target := range self.targets {
		entries, err := os.ReadDir(target.target.root)
		if err != nil || len(entries) != 0 {
			t.Fatal("Claim review changed a target", err)
		}
		if _, err := os.Stat(filepath.Join(target.target.metadata, "preparation.jsonl")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("Claim review reserved a target", err)
		}
	}
}

// Review emits a usable storage plan reference, without activating a writer.
func (self *monitorClaimRestoreFixture) plan(t *testing.T) durablevolume.Reference {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(self.claim.ctx, self.args(t, self.request), &output, &diagnostic); code != 0 {
		t.Fatal("public Claim restore review failed", code, diagnostic.String())
	}
	self.unmodifiedTargets(t)
	if len(self.targets) == 1 {
		var request durablevolume.PreparationRequest
		if err := decodeMonitorHistoryInput(output.Bytes(), &request); err != nil {
			t.Fatal(err)
		}
		if len(request.Owners) != self.expectedOwners {
			t.Fatal("Claim restore omitted an original co-owner or archive")
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
		t.Fatal("Claim cohort changed target authority", err, result)
	}
	return result.Cohort
}

// A restored cohort cannot enlarge reserve or replace marker/root authority
// still serving an untouched peer on the original synthetic physical mount.
func (self *monitorClaimRestoreFixture) unchangedHealthyAuthority(t *testing.T, restored durablevolume.Config) {
	t.Helper()
	selected := map[string]bool{}
	for _, request := range self.request.Preparations {
		selected[request.RootPath] = true
	}
	untouched := 0
	for _, before := range self.retained.Volumes {
		for _, root := range before.StateRoots {
			if selected[root.Path] {
				continue
			}
			untouched++
			matched := false
			for _, after := range restored.Volumes {
				if before.MountPath != after.MountPath {
					continue
				}
				oldAuthority, newAuthority := before, after
				oldAuthority.StateRoots, newAuthority.StateRoots = nil, nil
				if !reflect.DeepEqual(oldAuthority, newAuthority) {
					t.Fatal("Claim restore changed untouched mount reserve or marker authority", oldAuthority, newAuthority)
				}
				for _, candidate := range after.StateRoots {
					if candidate.Path == root.Path {
						matched = candidate == root
					}
				}
			}
			if !matched {
				t.Fatal("Claim restore lost an untouched original root", root.Path)
			}
		}
	}
	if untouched == 0 {
		t.Fatal("cohort fixture omitted actual untouched authority")
	}
}

// Actual output loss is retried against the same retained operation. Every
// original file is compared before the actual Claim monitor resumes.
func (self *monitorClaimRestoreFixture) apply(t *testing.T, reference durablevolume.Reference, short bool) {
	t.Helper()
	mode, flagName := "apply", "plan"
	if len(self.targets) != 1 {
		mode, flagName = "cohort-apply", "cohort"
	}
	args := []string{"storage-prepare", mode, "--" + flagName, reference.Path, "--" + flagName + "-sha256", reference.Sha256}
	var output, diagnostic bytes.Buffer
	if short {
		if code := runMain(self.claim.ctx, args, storagePreparationShortOutput{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "not fully delivered") {
			t.Fatal("Claim restore missed completed output-loss boundary", code, diagnostic.String())
		}
		diagnostic.Reset()
	}
	if code := runMain(self.claim.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("Claim restore apply failed", code, diagnostic.String())
	}
	if len(self.targets) != 1 {
		var result durablevolume.PreparationCohortResult
		if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || !result.Applied || !result.Complete || result.RestartAuthorized {
			t.Fatal("Claim cohort completion changed authority", err, result)
		}
		output.Reset()
		diagnostic.Reset()
		args[1] = "cohort-config"
		if code := runMain(self.claim.ctx, args, &output, &diagnostic); code != 0 || output.String() != result.DeclarationDocument {
			t.Fatal("Claim cohort declaration differs", code, diagnostic.String())
		}
		path := filepath.Join(self.targets[0].target.metadata, "restored-volumes.json")
		if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		self.claim.ctx = durablepath.WithHost(durablevolume.WithReference(t.Context(), durablevolume.Reference{Path: path, Sha256: result.DeclarationSha256}), self.sources[0].storage.Host)
		var restored durablevolume.Config
		if err := decodeMonitorHistoryInput(output.Bytes(), &restored); err != nil {
			t.Fatal(err)
		}
		self.unchangedHealthyAuthority(t, restored)
	} else {
		var result durablevolume.PreparationResult
		if err := decodeMonitorHistoryInput(output.Bytes(), &result); err != nil || result.RestartAuthorized {
			t.Fatal("Claim restore acquired restart authority", err)
		}
		self.claim.ctx = monitorNativeRestoreContext(t, self.sources[0], durablevolume.WithReference(self.targets[0].target.ctx, result.Declaration))
	}
	for index, target := range self.targets {
		if got := bootstrapSuccessorPreparationTestFiles(t, target.target.root); !reflect.DeepEqual(got, self.files[index]) {
			t.Fatal("Claim restore changed original bytes", index)
		}
	}
	if !reflect.DeepEqual(self.claim.record(t), self.record) {
		t.Fatal("Claim restore changed original unresolved evidence or policy")
	}
}

func monitorClaimRestoreContinues(t *testing.T, f *monitorClaimRestoreFixture) {
	t.Helper()
	f.claim.advance(nil)
	run := f.claim.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || !event.CheckpointCurrent || event.State.Overdue != 1 || event.State.Deferred != 1 || event.State.MerkleProofs != 1 || event.State.AcceptedReceipts != 1 {
		t.Fatal("restored Claim lost original unresolved/deferred obligations", event)
	}
	run.stop(t, 0)
	// A later claimed-leaf observation is retained while the original first
	// unclaimed proof and unrelated receipt/payment witness remain exact.
	f.claim.advance(func(value *protocol.ClaimProgress) {
		*value.Entries[0].Observation.LeafClaimed = true
		value.Entries[0].Observation.ObservedAt = value.PublishedAt
		value.Entries[0].QueueStatus = "finalized"
		value.FinalizedEntries, value.UnresolvedEntries, value.OldestUnresolvedEpoch = 2, 0, nil
	})
	run = f.claim.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current || event.State.ClaimedLeaves != 1 || event.State.Deferred != 1 {
		t.Fatal("restored Claim failed genuine later progress", event)
	}
	run.stop(t, 0)
	record := f.claim.record(t)
	admission, err := openMonitorClaimArchive(f.claim.ctx, f.claim.policy, record)
	if err != nil || admission == nil {
		t.Fatal("restored original archive cannot reopen", err)
	}
	defer func() {
		if err := admission.close(); err != nil {
			t.Error(err)
		}
	}()
	hydrated, err := hydrateMonitorClaimRecord(record, admission.epochStateKVs, f.claim.policy)
	if err != nil || len(hydrated.State.Epochs) != 2 {
		t.Fatal("restored Claim epoch census could not hydrate", err)
	}
	first, second := hydrated.State.Epochs[0], hydrated.State.Epochs[1]
	if first.Proof == nil || first.Proof.LeafClaimed == nil || *first.Proof.LeafClaimed || first.Observation == nil || first.Observation.LeafClaimed == nil || !*first.Observation.LeafClaimed || second.Observation == nil || second.Observation.PaymentStatus != "deferred" || second.Observation.AcceptedAmountRao != "25" || second.Observation.UnpaidCreditRao != "125" {
		t.Fatal("restored progress rewrote its first witness or deferred credit", err)
	}
}

func TestMonitorClaimRestorePublicSingleRootRetainsUnresolvedAndDeferred(t *testing.T) {
	f := newMonitorClaimRestoreFixture(t, 1)
	f.apply(t, f.plan(t), true)
	monitorClaimRestoreContinues(t, f)
}

func TestMonitorClaimRestorePublicCohortRetainsUnresolvedAndDeferred(t *testing.T) {
	f := newMonitorClaimRestoreFixture(t, 2)
	f.apply(t, f.plan(t), true)
	monitorClaimRestoreContinues(t, f)
}

// Actual128-epoch and128-review state passes through both public window and
// cross-volume restore commands. Later reviews and evidence continue on the
// original cursor; cold receipts and unresolved first witnesses remain exact.
func TestMonitorClaimWindowPublicFull128RolloverCohortRestoreAndWork(t *testing.T) {
	catalog, key := monitorClaimWindowTestCatalog()
	f := newMonitorClaimRestoreConfiguredFixture(t, 2, catalog,
		func(claim *monitorClaimArchiveFixture) { fillMonitorClaimWindowBoundary(t, claim) },
		func(claim *monitorClaimArchiveFixture) {
			claim.archive = filepath.Join(filepath.Dir(claim.archive), "claim-window.json")
			plan, args := monitorClaimWindowPlanFixture(t, claim, monitorClaimWindowNextPolicy(claim))
			applyMonitorClaimWindowFixture(t, claim, plan, args, key)
			for revision := 0; revision < 3; revision++ {
				record := claim.record(t)
				if record.PolicyHistory == nil || len(record.PolicyHistory.Entries) == 0 {
					t.Fatal("window publication lost its active policy acknowledgment")
				}
				if revision > 0 {
					claim.policy.FreshnessSeconds++
					last := record.PolicyHistory.Entries[len(record.PolicyHistory.Entries)-1]
					claim.policy.Renewal = &monitorProgressPolicyRenewal{Original: record.PolicyHistory.Entries[0].Resources, PreviousSha256: last.ContentHash, ReviewSha256: fmt.Sprintf("sha256:%064x", 0x4000+revision)}
				}
				claim.advance(nil)
				run := claim.start(t, monitorServiceHooks{})
				event := run.next(t)
				run.stop(t, 0)
				if !event.Current || !event.CheckpointCurrent || event.Window.Counts.PolicyReviews != 128 || event.Window.RetiredEpochs != 127 {
					t.Fatal("public window did not cross the original128-review boundary", event)
				}
			}
		})
	if f.record.Window == nil || f.record.PolicyHistory == nil || len(f.record.PolicyHistory.Entries) != 3 || len(f.record.State.Epochs) != 2 {
		t.Fatal("restore input did not retain more than128 acknowledged reviews")
	}
	firstProof := cloneMonitorClaimObservation(f.record.State.Epochs[0].Proof)
	if firstProof == nil || firstProof.LeafClaimed == nil || *firstProof.LeafClaimed {
		t.Fatal("original unresolved proof was not retained for restore")
	}
	f.apply(t, f.plan(t), true)
	f.claim.advance(nil)
	run := f.claim.start(t, monitorServiceHooks{})
	event := run.next(t)
	run.stop(t, 0)
	if !event.Current || !event.CheckpointCurrent || event.Window.RetiredEpochs != 127 || event.Window.Counts.Deferred != 127 || event.Window.Counts.PolicyReviews != 128 || event.State.MerkleProofs != 1 || event.State.Overdue != 1 {
		t.Fatal("public restored window lost retained credit or unresolved evidence", event)
	}
	f.claim.advance(func(value *protocol.ClaimProgress) {
		*value.Entries[0].Observation.LeafClaimed = true
		value.Entries[0].Observation.ObservedAt = value.PublishedAt
		value.Entries[0].QueueStatus = "finalized"
		value.FinalizedEntries, value.UnresolvedEntries, value.OldestUnresolvedEpoch = 128, 0, nil
	})
	run = f.claim.start(t, monitorServiceHooks{})
	event = run.next(t)
	run.stop(t, 0)
	if !event.Current || event.State.ClaimedLeaves != 1 || event.Window.Counts.Deferred != 127 {
		t.Fatal("restored window did not retain genuine later leaf progress", event)
	}
	record := f.claim.record(t)
	if len(record.State.Epochs) != 2 || !reflect.DeepEqual(record.State.Epochs[0].Proof, firstProof) || record.State.Epochs[0].Observation == nil || record.State.Epochs[0].Observation.LeafClaimed == nil || !*record.State.Epochs[0].Observation.LeafClaimed {
		t.Fatal("post-restore progress changed the original first witness")
	}
	work := map[string]uint64{}
	policy := f.claim.policy
	policy.work = func(stage string, count uint64) { work[stage] += count }
	worker, err := openMonitorClaimWorker(f.claim.ctx, policy, f.request.Expected, f.claim.services.checkpointPath, f.claim.services.metricsPath, monitorServiceHooks{})
	if err != nil || worker == nil {
		t.Fatal("restored complete window admission failed", err)
	}
	defer func() {
		if err := worker.close(monitorServiceHooks{}); err != nil {
			t.Error(err)
		}
	}()
	if work["window-read"] != 1 || work["archive-read"] != 1 || work["archive-decode"] != 1 || work["window-read-bytes"] == 0 || work["archive-read-bytes"] == 0 || work["hydrate-epoch"] < 128 {
		t.Fatal("restored complete history was skipped or read repeatedly", work)
	}
	t.Logf("actual full128 window/restore admission work: %v", work)
	clear(work)
	for range 5 {
		if code := worker.archiveAdmission.windows.publicationCode(f.claim.value.Load()); code != "ok" {
			t.Fatal("unchanged retired report did not retain its original identity", code)
		}
		if err := worker.save(); err != nil {
			t.Fatal(err)
		}
	}
	if work["window-read"] != 0 || work["archive-read"] != 0 || work["archive-decode"] != 0 || work["hydrate-epoch"] != 0 || work["validated-state-epoch"] != 10 || work["encoded-window-epoch"] != 5*2*(128+127) || work["retired-receipt-lookup"] != 5*uint64(len(f.claim.value.Load().Entries)) || work["checkpoint-encoded-bytes"] == 0 {
		t.Fatal("restored hot worker repeated full history or hid bounded transition work", work)
	}
	t.Logf("actual five hot publications after full128 restore: %v", work)
}

func TestMonitorClaimRestorePublicRejectsMissingLateSourceBeforeEffects(t *testing.T) {
	f := newMonitorClaimRestoreFixture(t, 2)
	reference := f.record.Archive.Segments[0]
	path := filepath.Join(f.targets[1].archive, filepath.Base(reference.Path))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	if code := runMain(f.claim.ctx, f.args(t, f.request), &output, &diagnostic); code == 0 || output.Len() != 0 {
		t.Fatal("missing original Claim source was admitted", code, diagnostic.String())
	}
	f.unmodifiedTargets(t)
}

func TestMonitorClaimRestorePublicRefusesIdentityCapacityAndCancellation(t *testing.T) {
	f := newMonitorClaimRestoreFixture(t, 1)
	for _, fault := range []string{"identity", "capacity", "digest", "cancel"} {
		request := f.request
		request.Preparations = append([]durablevolume.PreparationRequest(nil), f.request.Preparations...)
		ctx := f.claim.ctx
		cancel := func() {}
		switch fault {
		case "identity":
			request.Policy.ExpectedMember = "synthetic-other-member"
		case "capacity":
			request.Preparations[0].MinAvailableBytes = 1
		case "digest":
			request.Original.Sha256 = "sha256:" + strings.Repeat("f", 64)
		case "cancel":
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		var output, diagnostic bytes.Buffer
		code := runMain(ctx, f.args(t, request), &output, &diagnostic)
		cancel()
		if code == 0 || output.Len() != 0 {
			t.Fatal("invalid Claim restore produced authority", fault, code, diagnostic.String())
		}
		f.unmodifiedTargets(t)
	}
	var diagnostic bytes.Buffer
	if code := runMain(f.claim.ctx, f.args(t, f.request), storagePreparationShortOutput{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "not delivered") {
		t.Fatal("Claim request short output became success", code, diagnostic.String())
	}
	f.unmodifiedTargets(t)
}

// Actual source reads happen before the fault; an unavailable read never
// becomes observed absence, a reset, or an authoritative changed digest.
func TestMonitorClaimRestoreReplayReadCausesAndCancellationRemainTyped(t *testing.T) {
	f := newMonitorClaimArchiveFixture(t, nil)
	_, args := f.plan(t)
	f.apply(t, args)
	raw, err := os.ReadFile(f.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	original := monitorHistoryReference{Path: f.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}
	for _, fault := range []error{syscall.EIO, context.Canceled} {
		ctx, cancel := context.WithCancel(f.ctx)
		calls := 0
		err := validateMonitorClaimRestoreHistory(ctx, f.policy, original, func(reference monitorHistoryReference) ([]byte, error) {
			calls++
			raw, err := os.ReadFile(reference.Path)
			if err != nil {
				return nil, err
			}
			if calls == 2 {
				if errors.Is(fault, context.Canceled) {
					cancel()
					return raw, nil
				}
				return nil, fault
			}
			return raw, nil
		})
		cancel()
		if calls != 2 || !errors.Is(err, fault) || strings.Contains(fmt.Sprint(err), "differs") {
			t.Fatal("Claim read fault became history conflict", calls, fault, err)
		}
	}
}

// A complete, correctly exported physical namespace still cannot replace
// original proof/receipt facts with a self-consistent forged current head.
func TestMonitorClaimRestorePublicRechecksUnresolvedEvidenceAndPayment(t *testing.T) {
	for _, fault := range []string{"erase-proof", "change-deferred"} {
		f := newMonitorClaimRestoreMutatedFixture(t, 2, func(claim *monitorClaimArchiveFixture) {
			var original monitorClaimCheckpointRecord
			if err := decodeMonitorHistoryInput(claim.original, &original); err != nil {
				t.Fatal(err)
			}
			candidate := claim.record(t)
			if fault == "erase-proof" {
				candidate.State.Epochs[0] = cloneMonitorClaimEpoch(original.State.Epochs[0])
				candidate.State.Epochs[0].Proof = nil
			} else {
				candidate.State.Epochs[1] = cloneMonitorClaimEpoch(original.State.Epochs[1])
				candidate.State.Epochs[1].Observation.UnpaidCreditRao = "126"
			}
			raw, err := encodeMonitorClaimCheckpoint(candidate)
			if err != nil {
				t.Fatal(err)
			}
			writer, err := openMonitorHistorySnapshot(claim.ctx, claim.checkpoint, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(writer.publish(raw, nil), writer.close()); err != nil {
				t.Fatal(err)
			}
		})
		var output, diagnostic bytes.Buffer
		code := runMain(f.claim.ctx, f.args(t, f.request), &output, &diagnostic)
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "contradicted or erased archived evidence") {
			t.Fatal("Claim restore lost original financial semantic guard", fault, code, diagnostic.String())
		}
		f.unmodifiedTargets(t)
	}
}

// Increasing reserve for a restored root cannot alter a healthy co-owner's
// original declaration. Refusal is before reservation or target publication.
func TestMonitorClaimRestoreCohortRejectsChangedHealthyReserve(t *testing.T) {
	f := newMonitorClaimRestoreFixture(t, 2)
	for _, dimension := range []string{"bytes", "inodes"} {
		request := f.request
		request.Preparations = append([]durablevolume.PreparationRequest(nil), f.request.Preparations...)
		if len(f.retained.Volumes) != 1 {
			t.Fatal("fixture must have one independently retained mount")
		}
		if dimension == "bytes" {
			request.Preparations[0].MinAvailableBytes = f.retained.Volumes[0].MinAvailableBytes + 1
		} else {
			request.Preparations[0].MinAvailableInodes = f.retained.Volumes[0].MinAvailableInodes + 1
		}
		var output, diagnostic bytes.Buffer
		if code := runMain(f.claim.ctx, f.args(t, request), &output, &diagnostic); code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "cohort new reserve would change untouched root authority") {
			t.Fatal("Claim restore relaxed untouched reserve authority", dimension, code, diagnostic.String())
		}
		f.unmodifiedTargets(t)
	}
}

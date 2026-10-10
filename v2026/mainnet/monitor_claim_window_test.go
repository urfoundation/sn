//go:build linux || darwin

// These controls use the public offline plan/apply and public monitor command.
// Expected epochs and the test operational key are independently constructed;
// original receipt assertions never acquire chain or per-epoch paid authority.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

func monitorClaimWindowTestCatalog() (*monitorHistoryCatalogPolicy, ed25519.PrivateKey) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x68}, ed25519.SeedSize))
	return &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: "sha256:" + strings.Repeat("a", 64), InitialCapacity: monitorHistoryCapacity{Segments: 128, HeldReaders: 128, CatalogBytes: 128 * 1024}}, key
}

func newMonitorClaimWindowFixture(t *testing.T) (*monitorClaimArchiveFixture, ed25519.PrivateKey) {
	t.Helper()
	catalog, key := monitorClaimWindowTestCatalog()
	f := newMonitorClaimArchivePreparedFixture(t, catalog, 2, func(f *monitorClaimArchiveFixture) context.Context {
		f.policy.EpochCapacity, f.policy.ReviewHistoryEntries = maximumMonitorRetainedClaimEpochs, maximumMonitorProgressReviews
		f.services.policy.Claims = []monitorClaimPolicy{f.policy}
		f.services.writePolicy(t)
		return monitorTestStorageContext(t, t.Context(), f.services.args(f.url))
	})
	return f, key
}

// Each acknowledgment comes from an actual public reopen/save. The final
// reviewed prefix reaches128 epochs; three real bounded producer publications
// then supply127 original receipts while retaining the oldest unresolved proof.
func fillMonitorClaimWindowBoundary(t *testing.T, f *monitorClaimArchiveFixture) {
	t.Helper()
	initial := f.record(t)
	if initial.PolicyHistory == nil || len(initial.PolicyHistory.Entries) == 0 {
		t.Fatal("original public policy acknowledgment is absent")
	}
	original := initial.PolicyHistory.Entries[0].Resources
	for revision := 1; revision < maximumMonitorProgressReviews; revision++ {
		record := f.record(t)
		if record.PolicyHistory == nil || len(record.PolicyHistory.Entries) != revision {
			t.Fatal("prior independent policy acknowledgment is absent", revision)
		}
		last := record.PolicyHistory.Entries[len(record.PolicyHistory.Entries)-1]
		if revision == maximumMonitorProgressReviews-1 {
			for len(f.policy.Epochs) < maximumMonitorRetainedClaimEpochs {
				f.policy.Epochs = append(f.policy.Epochs, monitorClaimEpochPolicy{Epoch: 7 + int64(len(f.policy.Epochs)), ShareBps: 5000, AcceptBy: f.services.clock.now().Add(-time.Minute).Format(time.RFC3339Nano)})
			}
		} else {
			f.policy.FreshnessSeconds++
		}
		f.policy.Renewal = &monitorProgressPolicyRenewal{Original: original, PreviousSha256: last.ContentHash, ReviewSha256: fmt.Sprintf("sha256:%064x", 0x1000+revision)}
		f.advance(nil)
		run := f.start(t, monitorServiceHooks{})
		event := run.next(t)
		run.stop(t, 0)
		if !event.Current || !event.CheckpointCurrent {
			t.Fatal("public policy acknowledgment did not retain actual current evidence", revision, event)
		}
	}
	pending := cloneMonitorClaimProgress(f.value.Load()).Entries[0]
	accepted := cloneMonitorClaimProgress(f.value.Load()).Entries[1]
	for first := int64(8); first < 135; first += 63 {
		f.advance(func(value *protocol.ClaimProgress) {
			value.Entries = []protocol.ClaimProgressEntry{pending}
			for epoch := first; epoch < min(first+63, 135); epoch++ {
				entry := accepted
				entry.Epoch = epoch
				entry.Observation = cloneMonitorClaimObservation(accepted.Observation)
				entry.Observation.Epoch = epoch
				if epoch != 8 {
					entry.Observation.TransactionHash = fmt.Sprintf("0x%064x", epoch)
					entry.Observation.ObservedAt = value.PublishedAt
				}
				value.Entries = append(value.Entries, entry)
			}
			value.TotalEntries, value.FinalizedEntries, value.UnresolvedEntries = 128, 127, 1
			value.OmittedEntries = 128 - uint64(len(value.Entries))
			value.OmittedUnresolvedEntries = 0
		})
		if err := f.value.Load().Validate(); err != nil {
			t.Fatal("bounded full-window producer fixture invalid", err)
		}
		run := f.start(t, monitorServiceHooks{})
		event := run.next(t)
		run.stop(t, 0)
		if !event.Current || !event.CheckpointCurrent {
			t.Fatal("full-window original receipt page was not retained", first, event)
		}
	}
	record := f.record(t)
	if len(record.State.Epochs) != 128 || record.PolicyHistory == nil || len(record.PolicyHistory.Entries) != 128 || record.State.summary(f.policy, f.services.clock.now()).AcceptedReceipts != 127 {
		t.Fatal("fixture never reached the actual retained128 boundary")
	}
	f.resetRequest(t)
}

func monitorClaimWindowNextPolicy(f *monitorClaimArchiveFixture) monitorClaimPolicy {
	next := f.policy
	next.Renewal, next.Window = nil, nil
	highest := int64(-1)
	for _, epoch := range f.policy.Epochs {
		highest = max(highest, epoch.Epoch)
	}
	next.Epochs = []monitorClaimEpochPolicy{f.policy.Epochs[0], {Epoch: highest + 1, ShareBps: 5000, AcceptBy: f.services.clock.now().Add(time.Hour).Format(time.RFC3339Nano)}}
	return next
}

func monitorClaimWindowPlanFixture(t *testing.T, f *monitorClaimArchiveFixture, next monitorClaimPolicy) (monitorClaimWindowPlan, []string) {
	t.Helper()
	f.resetRequest(t)
	request := monitorClaimWindowRequest{Schema: monitorClaimWindowRequestSchema, Expected: f.request.Expected, Policy: f.policy, NextPolicy: next, Original: f.request.Original, ArchivePath: f.archive, FormerWriterFence: f.request.FormerWriterFence, FutureSegments: 1, ReviewSha256: "sha256:" + strings.Repeat("e", 64)}
	path, hash := f.document(t, "window-request", request)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, []string{"monitor-claim-window", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public Claim window plan refused", code, diagnostic.String())
	}
	var plan monitorClaimWindowPlan
	if err := decodeMonitorHistoryInput(output.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.RestartAuthorized || plan.PlanHash != plan.hash() || plan.NextPolicy.Window == nil {
		t.Fatal("window preview acquired authority or lost exact next policy")
	}
	path, hash = f.document(t, "window-plan", plan)
	return plan, []string{"monitor-claim-window", "apply", "--plan", path, "--plan-sha256", hash}
}

func monitorClaimWindowSignedArgs(t *testing.T, f *monitorClaimArchiveFixture, plan monitorClaimWindowPlan, args []string, key ed25519.PrivateKey) []string {
	t.Helper()
	message, err := hex.DecodeString(plan.SigningBytes)
	if err != nil {
		t.Fatal(err)
	}
	approval := monitorClaimWindowApproval{Schema: monitorClaimWindowApprovalSchema, TransitionHash: plan.Transition.hash(), Signature: hex.EncodeToString(ed25519.Sign(key, message))}
	path, hash := f.document(t, "window-approval", approval)
	return append(append([]string(nil), args...), "--approval", path, "--approval-sha256", hash)
}

func applyMonitorClaimWindowFixture(t *testing.T, f *monitorClaimArchiveFixture, plan monitorClaimWindowPlan, args []string, key ed25519.PrivateKey) []string {
	t.Helper()
	args = monitorClaimWindowSignedArgs(t, f, plan, args, key)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("public Claim window apply refused", code, diagnostic.String())
	}
	var report struct {
		Schema            string                  `json:"schema"`
		PlanHash          string                  `json:"plan_hash"`
		Policy            monitorClaimPolicy      `json:"policy"`
		Checkpoint        monitorHistoryReference `json:"checkpoint"`
		RestartAuthorized bool                    `json:"restart_authorized"`
	}
	if err := decodeMonitorHistoryInput(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.RestartAuthorized || report.PlanHash != plan.PlanHash || !reflect.DeepEqual(report.Policy, plan.NextPolicy) {
		t.Fatal("public window did not export exact independently approved next policy")
	}
	raw, err := os.ReadFile(f.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if report.Checkpoint.Sha256 != monitorReadDigest(raw) || report.Checkpoint.Bytes != uint64(len(raw)) {
		t.Fatal("window report differs from actual publication")
	}
	f.policy = report.Policy
	return args
}

func TestMonitorClaimWindowPublicFull128RetainsMoreThan128Reviews(t *testing.T) {
	f, key := newMonitorClaimWindowFixture(t)
	fillMonitorClaimWindowBoundary(t, f)
	original := append([]byte(nil), f.original...)
	plan, args := monitorClaimWindowPlanFixture(t, f, monitorClaimWindowNextPolicy(f))
	applyMonitorClaimWindowFixture(t, f, plan, args, key)
	retained, err := os.ReadFile(f.archive)
	if err != nil || !bytes.Equal(retained, original) {
		t.Fatal("window did not retain exact original128-epoch/128-review checkpoint", err)
	}
	f.advance(nil)
	run := f.start(t, monitorServiceHooks{})
	event := run.next(t)
	run.stop(t, 0)
	if !event.Current || !event.CheckpointCurrent || event.Window.RetiredEpochs != 127 || event.Window.Counts.Deferred != 127 || event.Window.Counts.PolicyReviews != 128 || event.State.Overdue != 1 || event.State.MerkleProofs != 1 {
		t.Fatal("public rollover restart lost unresolved or retired original facts", event)
	}
	record := f.record(t)
	if record.PolicyHistory == nil || len(record.PolicyHistory.Entries) != 1 {
		t.Fatal("window lost its initial policy acknowledgment")
	}
	originalResources := record.PolicyHistory.Entries[0].Resources
	for revision := 0; revision < 3; revision++ {
		last := record.PolicyHistory.Entries[len(record.PolicyHistory.Entries)-1]
		f.policy.FreshnessSeconds++
		f.policy.Renewal = &monitorProgressPolicyRenewal{Original: originalResources, PreviousSha256: last.ContentHash, ReviewSha256: fmt.Sprintf("sha256:%064x", 0x3000+revision)}
		f.advance(nil)
		run = f.start(t, monitorServiceHooks{})
		event = run.next(t)
		run.stop(t, 0)
		record = f.record(t)
		if !event.Current || !event.CheckpointCurrent || event.Window.Counts.PolicyReviews != 128 || record.PolicyHistory == nil || len(record.PolicyHistory.Entries) != revision+2 {
			t.Fatal("post-boundary independent review could not resume", revision, event)
		}
	}
	if len(record.PolicyHistory.Entries) != 4 || record.Window == nil || len(record.State.Epochs) != 2 {
		t.Fatal("review continuation did not cross128 without dropping original history")
	}
}

// Each fault occurs after an actual durable publication or delivered prefix.
// Re-applying the same plan can only accept those exact original/next bytes.
func TestMonitorClaimWindowPublicAcknowledgmentAndOutputLoss(t *testing.T) {
	for _, point := range []string{"window-archive", "window-checkpoint", "output"} {
		func() {
			f, key := newMonitorClaimWindowFixture(t)
			plan, args := monitorClaimWindowPlanFixture(t, f, monitorClaimWindowNextPolicy(f))
			args = monitorClaimWindowSignedArgs(t, f, plan, args, key)
			original := append([]byte(nil), f.original...)
			var output, diagnostic bytes.Buffer
			var writer io.Writer = &output
			hooks := monitorServiceHooks{}
			if point == "output" {
				writer = storagePreparationShortOutput{}
			} else {
				hooks.syncDirectory = func(_ string, kind string, _ *os.File) error {
					if kind == point {
						return syscall.EIO
					}
					return nil
				}
			}
			if code := runMainWithMonitorHooks(f.ctx, args, writer, &diagnostic, f.services.clock.now, hooks); code == 0 {
				t.Fatal("window fault was not observed", point)
			}
			retained, err := os.ReadFile(f.archive)
			if err != nil || !bytes.Equal(retained, original) {
				t.Fatal("failed window publication lost original evidence", point, err)
			}
			output.Reset()
			diagnostic.Reset()
			if code := runMain(f.ctx, args, &output, &diagnostic); code != 0 {
				t.Fatal("exact window replay after acknowledgment loss failed", point, code, diagnostic.String())
			}
			f.policy = plan.NextPolicy
			f.advance(nil)
			run := f.start(t, monitorServiceHooks{})
			event := run.next(t)
			run.stop(t, 0)
			if !event.Current || event.Window.RetiredEpochs != 1 || event.State.Overdue != 1 || event.State.MerkleProofs != 1 {
				t.Fatal("window recovery lost original liability", point, event)
			}
			output.Reset()
			diagnostic.Reset()
			if code := runMain(f.ctx, args, &output, &diagnostic); code == 0 {
				t.Fatal("old window approval reset later acknowledged progress", point)
			}
		}()
	}
}

func TestMonitorClaimWindowPublicRetiredContradictionAndHotWork(t *testing.T) {
	f, key := newMonitorClaimWindowFixture(t)
	plan, args := monitorClaimWindowPlanFixture(t, f, monitorClaimWindowNextPolicy(f))
	applyMonitorClaimWindowFixture(t, f, plan, args, key)
	work := map[string]uint64{}
	policy := f.policy
	policy.work = func(stage string, count uint64) { work[stage] += count }
	worker, err := openMonitorClaimWorker(f.ctx, policy, f.request.Expected, f.services.checkpointPath, f.services.metricsPath, monitorServiceHooks{})
	if err != nil || worker == nil {
		t.Fatal("window owner did not admit", err)
	}
	closed := false
	defer func() {
		if !closed {
			if err := worker.close(monitorServiceHooks{}); err != nil {
				t.Error(err)
			}
		}
	}()
	if work["window-read"] != 1 {
		t.Fatal("actual retained window admission was not counted", work)
	}
	clear(work)
	for range 5 {
		if code := worker.archiveAdmission.windows.publicationCode(f.value.Load()); code != "ok" {
			t.Fatal(code)
		}
		if err := worker.save(); err != nil {
			t.Fatal(err)
		}
	}
	if work["retired-receipt-lookup"] != 5*uint64(len(f.value.Load().Entries)) || work["window-read"] != 0 || work["archive-read"] != 0 || work["hydrate-epoch"] != 0 || work["validated-state-epoch"] != 5*uint64(len(f.policy.Epochs)) {
		t.Fatal("hot samples repeated history payload or census work", work)
	}
	err = worker.close(monitorServiceHooks{})
	closed = true
	if err != nil {
		t.Fatal(err)
	}
	f.advance(func(value *protocol.ClaimProgress) { value.Entries[1].Observation.UnpaidCreditRao = "126" })
	run := f.start(t, monitorServiceHooks{})
	event := run.next(t)
	run.stop(t, 3)
	if event.Status != "contradiction" || event.Current || event.Window.Counts.Deferred != 1 {
		t.Fatal("retired original payment mutation became healthy progress", event)
	}
}

func TestMonitorClaimWindowPublicUnknownEpochAndReviewGuards(t *testing.T) {
	f, key := newMonitorClaimWindowFixture(t)
	initial := f.record(t)
	if initial.PolicyHistory == nil || len(initial.PolicyHistory.Entries) == 0 {
		t.Fatal("original public policy acknowledgment is absent")
	}
	original := initial.PolicyHistory.Entries[0].Resources
	f.policy.FreshnessSeconds++
	f.policy.Renewal = &monitorProgressPolicyRenewal{Original: original, PreviousSha256: f.record(t).PolicyHistory.Entries[0].ContentHash, ReviewSha256: "sha256:" + strings.Repeat("d", 64)}
	f.advance(nil)
	run := f.start(t, monitorServiceHooks{})
	run.next(t)
	run.stop(t, 0)
	plan, args := monitorClaimWindowPlanFixture(t, f, monitorClaimWindowNextPolicy(f))
	wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x69}, ed25519.SeedSize))
	wrongArgs := monitorClaimWindowSignedArgs(t, f, plan, args, wrong)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, wrongArgs, &output, &diagnostic); code == 0 {
		t.Fatal("foreign operational key admitted a window")
	}
	before, err := os.ReadFile(f.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*monitorClaimWindowRequest){
		func(r *monitorClaimWindowRequest) {
			r.NextPolicy.Epochs = append([]monitorClaimEpochPolicy(nil), r.NextPolicy.Epochs[1:]...)
		},
		func(r *monitorClaimWindowRequest) { r.NextPolicy.Epochs[1].Epoch = r.Policy.Epochs[1].Epoch },
		func(r *monitorClaimWindowRequest) { r.ReviewSha256 = r.Policy.Renewal.ReviewSha256 },
		func(r *monitorClaimWindowRequest) { r.NextPolicy.ExpectedMember = "replacement" },
	} {
		request := plan.Request
		request.NextPolicy.Epochs = append([]monitorClaimEpochPolicy(nil), request.NextPolicy.Epochs...)
		mutation(&request)
		path, hash := f.document(t, "bad-window-request", request)
		output.Reset()
		diagnostic.Reset()
		if code := runMain(f.ctx, []string{"monitor-claim-window", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic); code == 0 {
			t.Fatal("window replaced unknown evidence, epoch or independent review")
		}
	}
	after, err := os.ReadFile(f.checkpoint)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused window changed retained evidence", err)
	}
	applyMonitorClaimWindowFixture(t, f, plan, args, key)
	record := f.record(t)
	if record.PolicyHistory == nil || len(record.PolicyHistory.Entries) != 1 {
		t.Fatal("window lost its initial policy acknowledgment")
	}
	f.policy.FreshnessSeconds++
	f.policy.Renewal = &monitorProgressPolicyRenewal{Original: record.PolicyHistory.Entries[0].Resources, PreviousSha256: record.PolicyHistory.Entries[0].ContentHash, ReviewSha256: plan.Request.Policy.Renewal.ReviewSha256}
	f.healthyPeer(t)
	f.advance(nil)
	checkpoint, err := os.ReadFile(f.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	reads := f.requests.Load()
	run = f.start(t, monitorServiceHooks{})
	f.refusedWhilePeerContinues(t, run, reads, checkpoint)
	if !strings.Contains(run.diagnostic.String(), "archived independent review") {
		t.Fatal("archived review reuse did not reach the retained authority guard", run.exit, run.diagnostic.String())
	}
}

// A full census can retire only the one known original receipt. Every absent
// or unresolved obligation survives; the next all-unresolved window holds.
func TestMonitorClaimWindowPublicFullUnknownCapacityHoldsWithoutLoss(t *testing.T) {
	f, key := newMonitorClaimWindowFixture(t)
	initial := f.record(t)
	if initial.PolicyHistory == nil || len(initial.PolicyHistory.Entries) != 1 {
		t.Fatal("original policy acknowledgment is absent")
	}
	for len(f.policy.Epochs) < maximumMonitorRetainedClaimEpochs {
		f.policy.Epochs = append(f.policy.Epochs, monitorClaimEpochPolicy{Epoch: 7 + int64(len(f.policy.Epochs)), ShareBps: 5000, AcceptBy: f.services.clock.now().Add(time.Hour).Format(time.RFC3339Nano)})
	}
	f.policy.Renewal = &monitorProgressPolicyRenewal{Original: initial.PolicyHistory.Entries[0].Resources, PreviousSha256: initial.PolicyHistory.Entries[0].ContentHash, ReviewSha256: "sha256:" + strings.Repeat("b", 64)}
	f.advance(nil)
	run := f.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current || !event.CheckpointCurrent {
		t.Fatal("actual128 expectation census did not admit", event)
	}
	run.stop(t, 0)
	next := f.policy
	next.Renewal, next.Window, next.Epochs = nil, nil, nil
	for _, epoch := range f.policy.Epochs {
		if epoch.Epoch != 8 {
			next.Epochs = append(next.Epochs, epoch)
		}
	}
	next.Epochs = append(next.Epochs, monitorClaimEpochPolicy{Epoch: 135, ShareBps: 5000, AcceptBy: f.services.clock.now().Add(time.Hour).Format(time.RFC3339Nano)})
	plan, args := monitorClaimWindowPlanFixture(t, f, next)
	before := append([]byte(nil), f.original...)
	for _, overflow := range []bool{false, true} {
		request := plan.Request
		request.NextPolicy.Epochs = append([]monitorClaimEpochPolicy(nil), next.Epochs...)
		if overflow {
			request.NextPolicy.Epochs = append(request.NextPolicy.Epochs, monitorClaimEpochPolicy{Epoch: 136, ShareBps: 5000, AcceptBy: next.Epochs[len(next.Epochs)-1].AcceptBy})
		} else {
			request.NextPolicy.Epochs = append(request.NextPolicy.Epochs[:1], request.NextPolicy.Epochs[2:]...)
		}
		path, hash := f.document(t, "capacity-refusal", request)
		var output, diagnostic bytes.Buffer
		if code := runMain(f.ctx, []string{"monitor-claim-window", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic); code == 0 {
			t.Fatal("public window dropped an unknown epoch or exceeded reviewed capacity", overflow)
		}
		if raw, err := os.ReadFile(f.checkpoint); err != nil || !bytes.Equal(raw, before) {
			t.Fatal("refused capacity change rewrote original obligations", err)
		}
	}
	applyMonitorClaimWindowFixture(t, f, plan, args, key)
	f.advance(nil)
	run = f.start(t, monitorServiceHooks{})
	event := run.next(t)
	run.stop(t, 0)
	record := f.record(t)
	if !event.Current || event.Window.RetiredEpochs != 1 || len(record.State.Epochs) != 128 || record.State.Epochs[0].Proof == nil || record.State.Epochs[1].Epoch != 9 || record.State.Epochs[127].Epoch != 135 {
		t.Fatal("public full-capacity window lost an unresolved original epoch", event)
	}
	f.archive = f.archive + ".next"
	f.resetRequest(t)
	next = f.policy
	next.Window, next.Renewal = nil, nil
	request := monitorClaimWindowRequest{Schema: monitorClaimWindowRequestSchema, Expected: f.request.Expected, Policy: f.policy, NextPolicy: next, Original: f.request.Original, ArchivePath: f.archive, FormerWriterFence: f.request.FormerWriterFence, FutureSegments: 1, ReviewSha256: "sha256:" + strings.Repeat("c", 64)}
	path, hash := f.document(t, "all-unresolved-window", request)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, []string{"monitor-claim-window", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "held by unresolved original epochs") {
		t.Fatal("all-unresolved window did not expose its capacity hold", code, diagnostic.String())
	}
	if raw, err := os.ReadFile(f.checkpoint); err != nil || !bytes.Equal(raw, f.original) {
		t.Fatal("all-unresolved capacity hold changed retained evidence", err)
	}
}

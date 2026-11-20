//go:build linux || darwin

// Funding projection borrows an admitted original index only between complete
// owner fences. Real public snapshots exercise cancellation, inode loss and
// refused candidate isolation without supplying a replacement custody verdict.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Actual settled receipt/payment facts enter a cold owner through public
// plan/apply. Additional pages preserve that exact original funding once.
func newEconomicFundingCustodyFixture(t *testing.T, pages int) *economicConservationArchiveFixture {
	t.Helper()
	if pages < 1 || pages > 3 {
		t.Fatal("invalid finite funding custody fixture")
	}
	f := newEconomicConservationArchiveFixture(t, false)
	f.sample(t, monitorServiceHooks{})
	for range pages {
		f.reset(t)
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("original funding archive preparation", code, issue)
		}
	}
	state := f.source.state(t)
	if state.Archive == nil || len(state.Archive.Segments) != pages || state.Archive.Counts.Claims != 2 || state.Archive.Counts.Payments != 1 || len(state.Claims) != 0 || len(state.Payments) != 0 {
		t.Fatal("funding custody fixture omitted original cold paid facts", state.Archive)
	}
	return f
}

// The direct projection still crosses real admitted file owners; cleanup is
// idempotent when a test deliberately closes one before its completion fence.
func economicFundingCustodyState(t *testing.T, f *economicConservationArchiveFixture, hooks monitorServiceHooks) (*economicConservationState, *economicConservationArchiveView) {
	t.Helper()
	state := f.source.state(t)
	view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, hooks)
	if err != nil || view == nil {
		t.Fatal("original funding custody admission", err)
	}
	t.Cleanup(func() {
		if err := view.close(); err != nil {
			t.Error(err)
		}
	})
	state.archiveView = view
	return &state, view
}

// This candidate will observe a later real funding page. Keep the original
// live native/vault network instead of borrowing the intentionally unobserved
// synthetic native-renewal fixture. Fee and Claim authority are enrolled before
// the first owner publication; neither a live policy nor an RPC is relabeled.
func newEconomicFundingCandidateFixture(t *testing.T) *economicConservationArchiveFixture {
	t.Helper()
	request := economicConservationRetirementTestRequest(t, "pair")
	f := newEconomicConservationArchiveFixture(t, false, func(source *economicConservationFixture) {
		source.policy.FeeAuthority = &request.Policy
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x75}, ed25519.SeedSize))
		source.policy.Claims[0].HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: monitorReadDigest([]byte("synthetic original funding Claim review")), InitialCapacity: monitorHistoryCapacity{Segments: 128, CatalogBytes: maximumMonitorHistoryCatalogBytes, HeldReaders: 128}}
		source.writePolicy(t)
	})
	if f.source.policy.Native.Observation.Network != f.source.native.policy.Network || f.source.policy.Vault.Network != f.source.vault.policy.Network {
		t.Fatal("candidate fixture changed original live reader network")
	}
	original := f.source.state(t)
	state, err := admitEconomicConservationNativeFees(f.ctx, f.source.policy, &original, request, time.Minute, historicalReplayHooks{})
	if err != nil || state == nil || len(state.NativeFees) != 1 || len(original.NativeFees) != 0 {
		t.Fatal("original live fee admission did not preserve its detached proof", err)
	}
	if err := state.validate(f.ctx, f.source.policy); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := openMonitorHistorySnapshot(f.ctx, f.source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(writer.publish(append(raw, '\n'), nil), writer.close()); err != nil {
		t.Fatal(err)
	}
	f.reset(t)
	return f
}

// Both an already closed owner and closure after arithmetic must discard all
// borrowed amounts. A nil projection prevents accidental use despite error.
func TestEconomicFundingClosedOriginalCannotPublishRetainedAmounts(t *testing.T) {
	for _, late := range []bool{false, true} {
		f := newEconomicFundingCustodyFixture(t, 1)
		state, view := economicFundingCustodyState(t, f, monitorServiceHooks{})
		calls := 0
		view.fundingWork = func(context.Context) {
			calls++
			if err := view.close(); err != nil {
				t.Fatal(err)
			}
		}
		if !late {
			if err := view.close(); err != nil {
				t.Fatal(err)
			}
		}
		projection, err := state.fundingSummary(f.ctx)
		expectedCalls := 0
		if late {
			expectedCalls = 1
		}
		if projection != nil || !errors.Is(err, os.ErrClosed) || calls != expectedCalls {
			t.Fatal("closed funding owner exposed retained amounts", late, projection, err, calls)
		}
	}
}

// Cancellation does not close a sibling owner or erase underlying facts, but
// neither early nor late cancellation may publish this projection's values.
func TestEconomicFundingCanceledProjectionKeepsOriginalFactsPrivate(t *testing.T) {
	f := newEconomicFundingCustodyFixture(t, 1)
	state, view := economicFundingCustodyState(t, f, monitorServiceHooks{})
	before, err := state.fundingSummary(f.ctx)
	if err != nil || before == nil || before.OriginalPayments != 1 {
		t.Fatal("healthy original funding projection", err, before)
	}
	for _, late := range []bool{false, true} {
		ctx, cancel := context.WithCancel(f.ctx)
		calls := 0
		view.fundingWork = func(context.Context) { calls++; cancel() }
		if !late {
			cancel()
		}
		projection, err := state.fundingSummary(ctx)
		cancel()
		expectedCalls := 0
		if late {
			expectedCalls = 1
		}
		if projection != nil || !errors.Is(err, context.Canceled) || calls != expectedCalls {
			t.Fatal("canceled funding work exposed a partial projection", late, projection, err, calls)
		}
	}
	view.fundingWork = nil
	after, err := state.fundingSummary(f.ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("canceled funding work changed retained original facts", err, before, after)
	}
}

// Direct map lookups do not recheck every growing prefix. Each projection
// performs exactly one complete physical fence on either side of arithmetic.
func TestEconomicFundingCustodyWorkIsTwoCompleteFencesPerProjection(t *testing.T) {
	for _, pages := range []int{1, 3} {
		f := newEconomicFundingCustodyFixture(t, pages)
		work := map[string]uint64{}
		state, view := economicFundingCustodyState(t, f, economicConservationAdmissionWork(work))
		if work["original-read"] != uint64(pages) || work["archive-custody-check"] != uint64(pages) || work["archive-index-ready"] != 1 {
			t.Fatal("funding cold admission repeated physical prefixes", pages, work)
		}
		clear(work)
		entries, retainedBytes := view.entries, view.bytes
		for attempt := uint64(1); attempt <= 3; attempt++ {
			projection, err := state.fundingSummary(f.ctx)
			if err != nil || projection == nil || projection.OriginalClaims != 2 || projection.OriginalPayments != 1 || work["archive-custody-check"] != 2*uint64(pages)*attempt || work["original-read"] != 0 || work["archive-checkpoint-decoded"] != 0 || view.entries != entries || view.bytes != retainedBytes {
				t.Fatal("funding lookup reread history or omitted complete owner fences", pages, attempt, err, projection, work)
			}
		}
	}
}

// Equal payload bytes do not preserve the original inode. Replacement occurs
// after funding arithmetic and before the final fence in the public command.
func TestEconomicFundingPublicEqualByteCustodyLossPublishesNoSummary(t *testing.T) {
	f := newEconomicFundingCustodyFixture(t, 1)
	state := f.source.state(t)
	path := state.Archive.Segments[0].Path
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{economicFundingWork: func(context.Context) {
		calls++
		economicConservationReplaceArchiveForTest(t, path)
	}})
	replacement, err := os.Stat(path)
	if code != 3 || err != nil || calls != 1 || output.Len() != 0 || os.SameFile(original, replacement) || !strings.Contains(diagnostic.String(), durablevolume.ErrIdentity.Error()) {
		t.Fatal("public funding used a replaced original after arithmetic", code, diagnostic.String(), calls, err)
	}
	oldBytes, err := os.ReadFile(path + ".retained-test-original")
	if err != nil {
		t.Fatal(err)
	}
	newBytes, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(oldBytes, newBytes) {
		t.Fatal("funding custody control changed content instead of original inode", err)
	}
}

// A stopped public owner produces no partial funding JSON after its final
// native/vault observations, while an independent reopen can still continue.
func TestEconomicFundingPublicCancellationPublishesNoSummary(t *testing.T) {
	f := newEconomicFundingCustodyFixture(t, 1)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	calls := 0
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{economicFundingWork: func(context.Context) { calls++; cancel() }})
	if code != 0 || calls != 1 || output.Len() != 0 || ctx.Err() != context.Canceled {
		t.Fatal("public canceled funding owner published a partial summary", code, diagnostic.String(), calls)
	}
	reopened := f.sample(t, monitorServiceHooks{})
	if reopened.Funding == nil || reopened.Funding.OriginalPayments != 1 || reopened.Funding.OriginalClaims != 2 {
		t.Fatal("canceled projection changed original cold funding", reopened)
	}
}

// Each public candidate opens its own archive view. A later signed Claim
// refusal must discard all of that view's funding/fee mutations; a separately
// held original and two identical successful retries must remain unchanged.
func TestEconomicFundingRefusedFeeCandidateCannotMutateOriginalIndex(t *testing.T) {
	f := newEconomicFundingCandidateFixture(t)
	f.request.RetireNativeFees = false
	f.request.ClaimWindows, f.request.NativeRenewal = nil, nil
	_, firstArgs := f.plan(t)
	if code, issue := f.apply(t, firstArgs, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("original hot fee and funding archive", code, issue)
	}
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	f.request.RetireNativeFees = true
	state, held := economicFundingCustodyState(t, f, monitorServiceHooks{})
	if held.funding == nil || len(state.NativeFees) != 1 || len(state.Claims) != 2 || len(state.Payments) != 1 || len(state.Archive.Segments) != 1 {
		t.Fatal("candidate isolation omitted hot fees, cold index or new paid originals", state)
	}
	before, err := state.fundingSummary(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	entries, retainedBytes := held.entries, held.bytes
	captured, accepted, paid := held.funding.captured, held.funding.accepted, held.funding.paid
	captureCount, claimCount, paymentCount := held.funding.captureCount, held.funding.claimCount, held.funding.paymentCount
	// Epoch one now has a matched original receipt and can retire. A later
	// expectation must advance its original high-water mark, never reuse it.
	next := economicConservationClaimWindowTestNext(t, f)
	proposal, code, issue := economicConservationClaimWindowTestPropose(t, f, next, monitorReadDigest([]byte("synthetic isolated funding Claim adoption")))
	if code != 0 || len(proposal.Window.Retired) != 1 || proposal.Window.Retired[0].Epoch != 1 || proposal.Window.HighestEpoch != 2 || len(proposal.Window.Next.Epochs) != 1 || proposal.Window.Next.Epochs[0].Epoch != 2 {
		t.Fatal("candidate isolation original Claim proposal", code, issue)
	}
	economicConservationClaimWindowTestSign(t, &proposal.Window)
	f.request.ClaimWindows = []economicConservationClaimWindow{proposal.Window}
	refused := f.request
	refused.ClaimWindows = append([]economicConservationClaimWindow(nil), f.request.ClaimWindows...)
	refused.ClaimWindows[0].HighestEpoch++
	economicConservationClaimWindowTestSign(t, &refused.ClaimWindows[0])
	economicConservationRevisionTestRefuse(t, f, refused, "exact original liabilities")
	plan, args := f.plan(t)
	again, _ := f.plan(t)
	if !reflect.DeepEqual(plan, again) {
		t.Fatal("refused funding candidate changed identical next plan")
	}
	for range 2 {
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("identical original funding/fee candidate retry", code, issue)
		}
	}
	after, err := state.fundingSummary(f.ctx)
	if err != nil || !reflect.DeepEqual(before, after) || held.entries != entries || held.bytes != retainedBytes || held.funding.captured != captured || held.funding.accepted != accepted || held.funding.paid != paid || held.funding.captureCount != captureCount || held.funding.claimCount != claimCount || held.funding.paymentCount != paymentCount {
		t.Fatal("candidate altered a separately held original funding index", err, before, after)
	}
	reopened := f.sample(t, monitorServiceHooks{})
	if reopened.Funding == nil || reopened.Funding.Captured != before.Captured || reopened.Funding.Accepted != before.Accepted || reopened.Funding.Paid != before.Paid || reopened.Funding.OriginalClaims != before.OriginalClaims || reopened.Funding.OriginalPayments != before.OriginalPayments || reopened.AdmittedNativeFees == nil || len(f.source.state(t).NativeFees) != 0 {
		t.Fatal("refused or repeated candidate counted original paid funding again", before, reopened)
	}
}

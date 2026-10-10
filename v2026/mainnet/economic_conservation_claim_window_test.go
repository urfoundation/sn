// Public offline proposals/adoption retain the same live combined owner. The
// synthetic native/receipt fixtures are the existing actual public readers;
// only the independent review signature is minted by this test.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func newEconomicConservationClaimWindowFixture(t *testing.T, full bool, unknownPayment bool) *economicConservationArchiveFixture {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x75}, ed25519.SeedSize))
	return newEconomicConservationArchiveFixture(t, true, func(source *economicConservationFixture) {
		claim := &source.policy.Claims[0]
		claim.EpochCapacity = maximumMonitorRetainedClaimEpochs
		claim.HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: monitorReadDigest([]byte("synthetic original Claim expectation review")), InitialCapacity: monitorHistoryCapacity{Segments: 512, CatalogBytes: maximumMonitorHistoryCatalogBytes, HeldReaders: 512}}
		if full {
			for epoch := int64(2); epoch <= maximumMonitorRetainedClaimEpochs; epoch++ {
				claim.Epochs = append(claim.Epochs, monitorClaimEpochPolicy{Epoch: epoch, ShareBps: 700, AcceptBy: source.now.Add(time.Hour).Format(time.RFC3339Nano)})
			}
		}
		source.claimPaymentUnknown.Store(unknownPayment)
	})
}

// The full-window fixture retires only epoch1, whose real receipt matched the
// checked fixture vault event. All127 unresolved expectations remain unchanged.
func economicConservationClaimWindowTestNext(t *testing.T, f *economicConservationArchiveFixture) monitorClaimPolicy {
	t.Helper()
	state := f.source.state(t)
	heads, err := state.claimHeads(f.source.policy)
	if err != nil || len(heads) != 1 {
		t.Fatal("current original Claim policy unavailable", err)
	}
	next := heads[0].Policy
	next.Epochs = slices.Clone(next.Epochs)
	if heads[0].Ordinal == 0 {
		next.Epochs = next.Epochs[1:]
		next.Epochs = append(next.Epochs, monitorClaimEpochPolicy{Epoch: heads[0].HighestEpoch + 1, ShareBps: 700, AcceptBy: f.source.now.Add(time.Hour).Format(time.RFC3339Nano)})
	} else {
		next.FreshnessSeconds++
	}
	return next
}

func economicConservationClaimWindowTestPropose(t *testing.T, f *economicConservationArchiveFixture, next monitorClaimPolicy, review string) (economicConservationClaimWindowProposal, int, string) {
	t.Helper()
	request := economicConservationClaimWindowRequest{Schema: "urnetwork-economic-claim-window-request-v1", Policy: f.source.policy, Original: f.request.Original, Next: next, ReviewSha256: review}
	path, pin := f.document(t, "claim-window-request.json", request)
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, []string{"economic-conservation-claim-window", "--request", path, "--request-sha256", pin}, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("read-only combined Claim proposal changed original custody")
	}
	var proposal economicConservationClaimWindowProposal
	if code == 0 {
		if err := decodePlanJson(output.Bytes(), &proposal); err != nil {
			t.Fatal(err)
		}
		message, err := proposal.Window.signingBytes()
		if err != nil || proposal.RestartAuthorized || proposal.SigningBytes != hex.EncodeToString(message) || proposal.ApplyCommand == "" {
			t.Fatal("public Claim proposal omitted exact unsigned authority", err, proposal)
		}
	} else if output.Len() != 0 {
		t.Fatal("refused Claim proposal emitted a partial authority document")
	}
	return proposal, code, diagnostic.String()
}

func economicConservationClaimWindowTestSign(t *testing.T, window *economicConservationClaimWindow) {
	t.Helper()
	message, err := window.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x75}, ed25519.SeedSize))
	window.Signature = hex.EncodeToString(ed25519.Sign(key, message))
}

func economicConservationClaimWindowTestPlan(t *testing.T, f *economicConservationArchiveFixture, next monitorClaimPolicy, review string) (economicConservationClaimWindow, economicConservationArchivePlan, []string) {
	t.Helper()
	proposal, code, issue := economicConservationClaimWindowTestPropose(t, f, next, review)
	if code != 0 {
		t.Fatal("public original-key Claim proposal failed", code, issue)
	}
	window := proposal.Window
	economicConservationClaimWindowTestSign(t, &window)
	f.request.ClaimWindows = []economicConservationClaimWindow{window}
	plan, args := f.plan(t)
	return window, plan, args
}

func TestEconomicConservationClaimWindowPublicFull128RetainsUnknownCreditAndResumes(t *testing.T) {
	f := newEconomicConservationClaimWindowFixture(t, true, false)
	before := f.source.state(t)
	raw, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	window, _, args := economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte("synthetic first full window review")))
	if len(window.Retired) != 1 || window.Retired[0].Epoch != 1 || window.HighestEpoch != 129 {
		t.Fatal("full window retired an unresolved original epoch", window)
	}
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public full Claim window adoption failed", code, issue)
	}
	retained, err := os.ReadFile(f.request.ArchivePath)
	if err != nil || !bytes.Equal(retained, raw) {
		t.Fatal("Claim adoption changed its exact original evidence", err)
	}
	after := f.source.state(t)
	if after.PolicyHash != before.PolicyHash || after.Native.Cursor != before.Native.Cursor || after.Vault.Cursor != before.Vault.Cursor || len(after.ClaimStates[0].Epochs) != 128 || after.ClaimStates[0].Epochs[0].Epoch != 2 || after.ClaimStates[0].Epochs[127].Epoch != 129 || !reflect.DeepEqual(before.Credits, after.Credits) || after.ClaimStates[0].Record.StartedAt != before.ClaimStates[0].Record.StartedAt {
		t.Fatal("Claim window reset unresolved credit, cursor, producer birth or original policy", after)
	}
	for index := 0; index < 127; index++ {
		if !reflect.DeepEqual(before.ClaimStates[0].Epochs[index+1], after.ClaimStates[0].Epochs[index]) {
			t.Fatal("Claim window changed its unresolved exact prefix", index)
		}
	}
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if summary.TargetMet != nil || summary.MatchedReceipts != 1 || summary.AggregatePayments != 1 || len(state.Payments) != 1 || len(state.Payments[0].Credit.Claims) != 2 || len(state.ClaimStates[0].Epochs) != 128 {
		t.Fatal("reopened window lost original deferred receipt or aggregate payment backing", summary, state)
	}
}

func TestEconomicConservationClaimWindowPublicLostAckAndShortOutputReconcile(t *testing.T) {
	for _, mode := range []string{"archive", "checkpoint", "output"} {
		f := newEconomicConservationClaimWindowFixture(t, false, false)
		_, plan, args := economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte("synthetic acknowledgment window review")))
		var fired atomic.Bool
		hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
			if err := file.Sync(); err != nil {
				return err
			}
			if role == economicConservationRole && kind == mode && fired.CompareAndSwap(false, true) {
				return syscall.EIO
			}
			return nil
		}}
		var output bytes.Buffer
		if mode == "output" {
			if code, _ := f.apply(t, args, economicConservationWindowShortWriter{}, hooks); code == 0 {
				t.Fatal("Claim adoption hid a short output after publication")
			}
		} else if code, _ := f.apply(t, args, &output, hooks); code == 0 || !fired.Load() {
			t.Fatal("Claim adoption did not reach lost durable acknowledgment", mode, code)
		}
		if code, issue := f.apply(t, args, &output, monitorServiceHooks{}); code != 0 {
			t.Fatal("Claim adoption did not reconcile exact original/next after acknowledgment loss", mode, code, issue)
		}
		raw, err := os.ReadFile(f.source.checkpoint)
		if err != nil || monitorReadDigest(raw) != plan.Next.Sha256 || f.source.state(t).ClaimWindows[0].Ordinal != 1 {
			t.Fatal("Claim acknowledgment recovery repeated or changed original window", mode, err)
		}
		f.sample(t, monitorServiceHooks{})
	}
}

type economicConservationWindowShortWriter struct{}

func (economicConservationWindowShortWriter) Write(raw []byte) (int, error) { return len(raw) - 1, nil }

func TestEconomicConservationClaimWindowPublicRefusesUnknownOmissionAndEpochReuse(t *testing.T) {
	f := newEconomicConservationClaimWindowFixture(t, true, false)
	next := economicConservationClaimWindowTestNext(t, f)
	next.Epochs = slices.Clone(next.Epochs[1:])
	if _, code, issue := economicConservationClaimWindowTestPropose(t, f, next, monitorReadDigest([]byte("synthetic omitted unknown"))); code == 0 || !strings.Contains(issue, "unresolved or unmatched") {
		t.Fatal("Claim proposal discarded a still unknown original epoch", code, issue)
	}
	next = economicConservationClaimWindowTestNext(t, f)
	next.Epochs[len(next.Epochs)-1].Epoch = 1
	if _, code, issue := economicConservationClaimWindowTestPropose(t, f, next, monitorReadDigest([]byte("synthetic reused epoch"))); code == 0 || !strings.Contains(issue, "reused an original expected epoch") {
		t.Fatal("Claim proposal reused a retired original identity", code, issue)
	}
}

func TestEconomicConservationClaimWindowPublicUnknownPaymentStaysHot(t *testing.T) {
	f := newEconomicConservationClaimWindowFixture(t, false, true)
	next := economicConservationClaimWindowTestNext(t, f)
	if _, code, issue := economicConservationClaimWindowTestPropose(t, f, next, monitorReadDigest([]byte("synthetic unknown payment"))); code == 0 || !strings.Contains(issue, "unresolved or unmatched") {
		t.Fatal("Claim proposal erased original unknown payment", code, issue)
	}
	if receipt := f.source.state(t).Receipts[0]; receipt.Observation.PaymentStatus != "unknown" || receipt.ClaimId == "" {
		t.Fatal("unknown-payment fixture did not retain its exact original acceptance", receipt)
	}
}

func TestEconomicConservationClaimWindowPublicRefusesEnrollmentSignatureAndOldReview(t *testing.T) {
	legacy := newEconomicConservationArchiveFixture(t, true)
	next := legacy.source.policy.Claims[0]
	next.Epochs = []monitorClaimEpochPolicy{{Epoch: 2, ShareBps: 700, AcceptBy: legacy.source.now.Add(time.Hour).Format(time.RFC3339Nano)}}
	if _, code, issue := economicConservationClaimWindowTestPropose(t, legacy, next, monitorReadDigest([]byte("synthetic legacy enrollment"))); code == 0 || !strings.Contains(issue, "enroll or replace") {
		t.Fatal("Claim proposal silently enrolled a legacy key", code, issue)
	}
	f := newEconomicConservationClaimWindowFixture(t, false, false)
	review := monitorReadDigest([]byte("synthetic first original review"))
	window, _, args := economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), review)
	f.request.ClaimWindows[0].Signature = strings.Repeat("0", 128)
	economicConservationRevisionTestRefuse(t, f, f.request, "original independent authority")
	f.request.ClaimWindows[0] = window
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	f.reset(t)
	if _, code, issue := economicConservationClaimWindowTestPropose(t, f, economicConservationClaimWindowTestNext(t, f), review); code == 0 || !strings.Contains(issue, "current independent review") {
		t.Fatal("Claim proposal reused its current review", code, issue)
	}
	_, _, args = economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte("synthetic second original review")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	f.reset(t)
	if _, code, issue := economicConservationClaimWindowTestPropose(t, f, economicConservationClaimWindowTestNext(t, f), review); code == 0 || !strings.Contains(issue, "archived independent review") {
		t.Fatal("Claim proposal reused a non-adjacent archived review", code, issue)
	}
}

func TestEconomicConservationClaimWindowPublicRetiredReceiptContradictionKeepsPeerProgress(t *testing.T) {
	f := newEconomicConservationClaimWindowFixture(t, false, false)
	_, _, args := economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte("synthetic retired receipt guard")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	f.source.claimFault.Store(true)
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &summary); err != nil {
		t.Fatal(err, diagnostic.String())
	}
	state := f.source.state(t)
	if code != 3 || !summary.NativeCurrent || !summary.VaultCurrent || summary.VaultCursor.Number != 12 || state.ClaimStates[0].Status != "contradiction" || summary.MatchedReceipts != 1 || summary.TargetMet != nil {
		t.Fatal("retired Claim contradiction erased original evidence or stopped healthy peers", code, diagnostic.String(), summary, state.ClaimStates[0])
	}
}

// This crosses the old128-review ceiling using public proposals and durable
// adoption. Only monotonic freshness resources change after the initial full
// epoch window; all127 unknown original expectations remain hot throughout.
func TestEconomicConservationClaimWindowPublicBeyond128ReviewsRetainsCompleteLineage(t *testing.T) {
	f := newEconomicConservationClaimWindowFixture(t, true, false)
	originalHash := f.source.policy.identityHash()
	var firstReview string
	for ordinal := uint64(1); ordinal <= 129; ordinal++ {
		review := monitorReadDigest([]byte(fmt.Sprintf("synthetic cumulative Claim review %d", ordinal)))
		if ordinal == 1 {
			firstReview = review
		}
		window, _, args := economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), review)
		if window.Ordinal != ordinal {
			t.Fatal("public Claim review reset original ordinal", ordinal, window)
		}
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("bounded public Claim review continuation failed", ordinal, code, issue)
		}
		f.reset(t)
	}
	state := f.source.state(t)
	if len(state.ClaimWindows) != 1 || state.ClaimWindows[0].Ordinal != 129 || len(state.Archive.ClaimHeads) != 1 || state.Archive.ClaimHeads[0].Ordinal != 128 || state.Archive.ClaimHeads[0].Retired != 1 || len(state.ClaimStates[0].Epochs) != 128 || state.ClaimStates[0].Epochs[0].Epoch != 2 || state.PolicyHash != originalHash {
		t.Fatal("Claim continuation capped/reset original reviews or dropped unresolved epochs", state)
	}
	if _, code, issue := economicConservationClaimWindowTestPropose(t, f, economicConservationClaimWindowTestNext(t, f), firstReview); code == 0 || !strings.Contains(issue, "archived independent review") {
		t.Fatal("long Claim history forgot its original review", code, issue)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	var reads, work atomic.Uint64
	var counts []uint64
	hooks := monitorServiceHooks{
		historyRead: func(role, stage string) {
			if role == economicConservationRole && stage == "conservation-archive-admission" {
				reads.Add(1)
			}
		},
		economicClaimWork: func(role, stage string, units uint64) { work.Add(units) },
		afterEvent: func(context.Context, string) {
			counts = append(counts, work.Load())
			if len(counts) == 3 {
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			f.source.now = f.source.now.Add(time.Second)
			return ctx.Err() == nil
		},
	}
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	if code != 0 || len(counts) != 3 || reads.Load() != 129 || counts[1]-counts[0] == 0 || counts[1]-counts[0] != counts[2]-counts[1] || counts[2]-counts[1] > 16*128 {
		t.Fatal("long Claim history repeated payload admission or scaled foreground work with review history", code, diagnostic.String(), reads.Load(), counts)
	}
	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte{'\n'})
	var summary economicConservationSummary
	if len(lines) != 3 || decodePlanJson(lines[2], &summary) != nil || summary.MatchedReceipts != 1 || summary.TargetMet != nil || !summary.NativeCurrent || !summary.VaultCurrent {
		t.Fatal("long reviewed Claim history did not reopen original combined progress", summary, output.String())
	}
}

func TestEconomicConservationClaimWindowPublicSignatureCannotForgeRetirement(t *testing.T) {
	f := newEconomicConservationClaimWindowFixture(t, true, false)
	proposal, code, issue := economicConservationClaimWindowTestPropose(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte("synthetic forged retirement")))
	if code != 0 {
		t.Fatal(code, issue)
	}
	window := proposal.Window
	window.Retired = append(window.Retired, monitorClaimArchivedEpoch{Epoch: 2, StateSha256: rootObjectHash(f.source.state(t).ClaimStates[0].Epochs[1])})
	economicConservationClaimWindowTestSign(t, &window)
	f.request.ClaimWindows = []economicConservationClaimWindow{window}
	economicConservationRevisionTestRefuse(t, f, f.request, "exact original liabilities")
	if state := f.source.state(t); len(state.ClaimStates[0].Epochs) != 128 || state.ClaimStates[0].Epochs[1].Observation != nil || state.Archive != nil {
		t.Fatal("signed supplied retirement changed original unresolved evidence", state)
	}
}

// The populated predecessor and newly indexed facts share one reviewed
// envelope. Neither can consume the other's reserved two-times margin.
func TestEconomicConservationClaimWindowCountsResidentPredecessorBeforeEffects(t *testing.T) {
	f := newEconomicConservationClaimWindowFixture(t, true, false)
	state := f.source.state(t)
	view := newEconomicConservationArchiveView(f.source.policy.initialResources())
	if err := view.setClaimBasis(f.source.policy, &state, f.request.Original); err != nil {
		t.Fatal(err)
	}
	if view.claimBasisEntries < 128 || view.claimBasisBytes < 128*256 {
		t.Fatal("populated Claim predecessor was not counted", view.claimBasisEntries, view.claimBasisBytes)
	}
	if err := view.charge(state.Receipts[0]); err != nil {
		t.Fatal(err)
	}
	original := view.claimBasis
	entries, bytes := view.entries, view.bytes
	view.resources.IndexBytes = 2 * (bytes + view.claimBasisBytes)
	view.resources.IndexEntries = 2 * (entries + view.claimBasisEntries)
	if err := view.setClaimBasis(f.source.policy, &state, f.request.Original); err != nil {
		t.Fatal("exact populated Claim capacity was refused", err)
	}
	original = view.claimBasis
	if err := view.charge(state.Receipts[0]); err == nil || view.entries != entries || view.bytes != bytes {
		t.Fatal("archive facts borrowed the resident Claim capacity or changed index on refusal", err)
	}
	view.resources.IndexEntries += 2
	view.resources.IndexBytes--
	if err := view.setClaimBasis(f.source.policy, &state, f.request.Original); err == nil || view.claimBasis != original {
		t.Fatal("Claim predecessor ignored the exact byte bound or changed admitted basis", err)
	}
	view.resources.IndexBytes++
	view.resources.IndexEntries -= 3
	if err := view.setClaimBasis(f.source.policy, &state, f.request.Original); err == nil || view.claimBasis != original {
		t.Fatal("Claim predecessor ignored the exact entry bound or changed admitted basis", err)
	}
}

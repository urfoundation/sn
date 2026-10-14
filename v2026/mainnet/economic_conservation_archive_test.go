package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

type economicConservationArchiveFixture struct {
	source   *economicConservationFixture
	ctx      context.Context
	metadata string
	key      ed25519.PrivateKey
	request  economicConservationArchiveRequest
	sequence uint64
	storage  *durablefixture.Fixture
}

func newEconomicConservationArchiveFixture(t *testing.T, signed bool, configure ...func(*economicConservationFixture)) *economicConservationArchiveFixture {
	t.Helper()
	f := &economicConservationArchiveFixture{source: newEconomicConservationFixture(t, false), metadata: t.TempDir()}
	for _, change := range configure {
		change(f.source)
	}
	if err := os.Chmod(f.metadata, 0700); err != nil {
		t.Fatal(err)
	}
	if signed {
		f.key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))
		f.source.policy.Continuation = &economicConservationContinuationPolicy{Schema: economicConservationResourcesSchema, ApprovalPublicKey: "0x" + hex.EncodeToString(f.key.Public().(ed25519.PublicKey)), ReviewSha256: monitorReadDigest([]byte("synthetic original resource review")), Initial: economicConservationResources{ActiveFacts: 256, ReadBudgetSeconds: 300, ArchiveSegments: 512, IndexEntries: 65536, IndexBytes: 64 * 1024 * 1024}}
		f.source.writePolicy(t)
	}
	f.ctx = monitorTestStorageContext(t, t.Context(), f.source.args(t))
	reference, ok := durablevolume.ReferenceFromContext(f.ctx)
	if !ok {
		t.Fatal("fixture durable declaration absent")
	}
	config, err := durablevolume.Load(reference)
	if err != nil {
		t.Fatal(err)
	}
	for index := range config.Volumes {
		config.Volumes[index].MinAvailableBytes = 64 * 1024 * 1024
		config.Volumes[index].MinAvailableInodes = 4096
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	reference = durablevolume.Reference{Path: filepath.Join(f.metadata, "volumes.json"), Sha256: monitorReadDigest(raw)}
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.ctx = durablevolume.WithReference(f.ctx, reference)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	return f
}

func (self *economicConservationArchiveFixture) sample(t *testing.T, hooks monitorServiceHooks) economicConservationSummary {
	t.Helper()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(self.ctx, self.source.args(t), &output, &diagnostic, func() time.Time { return self.source.now }, hooks)
	if code != 0 {
		t.Fatal("public conservation sample failed", code, diagnostic.String(), output.String())
	}
	var summary economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	return summary
}

func (self *economicConservationArchiveFixture) reset(t *testing.T) {
	t.Helper()
	self.sequence++
	raw, err := os.ReadFile(self.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(filepath.Dir(self.source.checkpoint), fmt.Sprintf("conservation-archive-%03d.json", self.sequence))
	provisionMonitorTestCustodyProfile(t, archive, self.source.policy.storageKind(), int(self.source.policy.storageMaximum()))
	self.request = economicConservationArchiveRequest{Schema: economicConservationArchiveRequestSchema, Policy: self.source.policy, Original: monitorHistoryReference{Path: self.source.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}, ArchivePath: archive, FutureSegments: 1, FutureIndexEntries: 16, FutureIndexBytes: 4096}
	fence := monitorHistoryWriterFence{Schema: monitorHistoryWriterFenceSchema, Original: self.request.Original, PolicyHash: self.source.policy.identityHash(), StoppedAndJoined: true}
	raw, err = json.Marshal(fence)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.metadata, fmt.Sprintf("fence-%03d.json", self.sequence))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	self.request.FormerWriterFence = planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
}

func (self *economicConservationArchiveFixture) document(t *testing.T, name string, value any) (string, string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.metadata, name)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path, monitorReadDigest(raw)
}

func (self *economicConservationArchiveFixture) plan(t *testing.T) (economicConservationArchivePlan, []string) {
	t.Helper()
	path, hash := self.document(t, "request.json", self.request)
	before := mainnetNamespaceTest(t, filepath.Dir(self.source.checkpoint))
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(self.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic, func() time.Time { return self.source.now }, monitorServiceHooks{})
	if code != 0 {
		t.Fatal("public combined archive plan failed", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(self.source.checkpoint))) {
		t.Fatal("read-only combined archive plan changed custody")
	}
	var plan economicConservationArchivePlan
	if err := decodePlanJson(output.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.RestartAuthorized || plan.RequiredHeadBytes < 2*plan.Next.Bytes || plan.RequiredIndexEntries < 2*self.request.FutureIndexEntries || plan.RequiredBytes <= 2*self.request.Original.Bytes {
		t.Fatal("archive plan lost distinct two-times resource forecast", plan)
	}
	path, hash = self.document(t, "plan.json", plan)
	return plan, []string{"economic-conservation-archive", "apply", "--plan", path, "--plan-sha256", hash}
}

func (self *economicConservationArchiveFixture) apply(t *testing.T, args []string, output io.Writer, hooks monitorServiceHooks) (int, string) {
	t.Helper()
	var diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(self.ctx, args, output, &diagnostic, func() time.Time { return self.source.now }, hooks)
	return code, diagnostic.String()
}

func (self *economicConservationArchiveFixture) sign(t *testing.T, to economicConservationResources) {
	t.Helper()
	state := self.source.state(t)
	from, err := state.resources(self.source.policy)
	if err != nil {
		t.Fatal(err)
	}
	previous, ordinal := self.source.policy.identityHash(), uint64(1)
	if state.Archive != nil {
		previous, ordinal = state.Archive.LastRenewalHash, state.Archive.LastRenewalOrdinal+1
	}
	if state.Renewal != nil {
		previous, ordinal = rootObjectHash(state.Renewal), state.Renewal.Ordinal+1
	}
	revision := &economicConservationRenewal{Schema: economicConservationRenewalSchema, PolicyHash: self.source.policy.identityHash(), Original: self.request.Original, Ordinal: ordinal, Previous: previous, ReviewSha256: monitorReadDigest([]byte(fmt.Sprintf("synthetic resource review %d", ordinal))), From: from, To: to}
	message, err := revision.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	revision.Signature = hex.EncodeToString(ed25519.Sign(self.key, message))
	self.request.Renewal = revision
}

func TestEconomicConservationArchivePublicRetainsUnmatchedSourceCarryAndReceipt(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	before := f.source.state(t)
	original, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public combined archive apply failed", code, issue)
	}
	archived, err := os.ReadFile(f.request.ArchivePath)
	if err != nil || !bytes.Equal(original, archived) {
		t.Fatal("combined archive rewrote original observations", err)
	}
	compact := f.source.state(t)
	if compact.Archive == nil || len(compact.Lots) != 1 || compact.Lots[0].Id != before.Lots[0].Id || len(compact.Captures) != 1 || compact.Captures[0].AmountDifferenceAlpha == nil || *compact.Captures[0].AmountDifferenceAlpha != "14" || !reflect.DeepEqual(compact.Carry, before.Carry) || !reflect.DeepEqual(compact.Credits, before.Credits) || len(compact.Claims) != 1 {
		t.Fatal("compaction removed unresolved source/carry/credit obligations", compact)
	}
	summary := f.sample(t, monitorServiceHooks{})
	after := f.source.state(t)
	if summary.EarningOccurrences != 2 || summary.Captures != 1 || summary.AggregatePayments != 1 || summary.MatchedReceipts != 1 || summary.Execution == nil || summary.Execution.ProviderEntitlement != "9" || summary.TargetMet != nil || len(after.Payments) != 1 || len(after.Payments[0].Credit.Claims) != 2 || after.ClaimStates[0].Epochs[0].Observation.PaymentStatus != "deferred" {
		t.Fatal("archive restart lost an original source or rewrote its deferred receipt", summary, after)
	}
}

func TestEconomicConservationArchivePublicAdmitsOnceThenUsesRetainedIndex(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if len(state.Claims) != 0 || len(state.Payments) != 0 || state.Archive == nil || state.Archive.Counts.Claims != 2 || state.Archive.Counts.Payments != 1 {
		t.Fatal("settled facts did not enter exact retained archive", state)
	}
	var reads, samples atomic.Uint64
	var output, diagnostic bytes.Buffer
	hooks := monitorServiceHooks{historyRead: func(role, step string) {
		if role == economicConservationRole && step == "conservation-archive-admission" {
			reads.Add(1)
		}
	}, afterEvent: func(context.Context, string) { samples.Add(1) }, wait: func(context.Context, string, time.Duration) bool { return samples.Load() < 3 }}
	code := runMainWithMonitorHooks(f.ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	if code != 0 || reads.Load() != 1 || samples.Load() != 3 {
		t.Fatal("continuous archive owner reread payloads or lost public progress", code, reads.Load(), samples.Load(), diagnostic.String())
	}
	decoder := json.NewDecoder(&output)
	for range 3 {
		var summary economicConservationSummary
		if err := decoder.Decode(&summary); err != nil {
			t.Fatal(err)
		}
		if summary.AcceptedClaims != 2 || summary.AggregatePayments != 1 || summary.MatchedReceipts != 1 || summary.ArchiveIndexEntries == 0 || summary.ActualNativeOutcomeVerified || summary.ActivationReady {
			t.Fatal("retained lookup lost original payment/receipt or invented authority", summary)
		}
	}
}

func TestEconomicConservationArchivePublicLostAcknowledgmentsReconcileExactHeads(t *testing.T) {
	for _, kind := range []string{"archive", "checkpoint", "output"} {
		f := newEconomicConservationArchiveFixture(t, false)
		plan, args := f.plan(t)
		hooks := monitorServiceHooks{}
		var output io.Writer = &bytes.Buffer{}
		if kind == "output" {
			output = economicConservationShortWriter{}
		} else {
			hooks.syncDirectory = func(_, step string, file *os.File) error {
				if step == kind {
					return errors.Join(file.Sync(), syscall.EIO)
				}
				return file.Sync()
			}
		}
		if code, issue := f.apply(t, args, output, hooks); code != 2 {
			t.Fatal("lost archive acknowledgment became success", kind, code, issue)
		}
		for range 2 {
			if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
				t.Fatal("same exact plan could not reconcile lost acknowledgment", kind, code, issue)
			}
		}
		raw, err := os.ReadFile(f.source.checkpoint)
		if err != nil || monitorReadDigest(raw) != plan.Next.Sha256 {
			t.Fatal("replay published something other than the exact planned head", kind, err)
		}
		summary := f.sample(t, monitorServiceHooks{})
		if summary.EarningOccurrences != 2 || summary.Captures != 1 || summary.AggregatePayments != 1 {
			t.Fatal("reconciled archive duplicated a financial occurrence", kind, summary)
		}
	}
}

func TestEconomicConservationArchiveCanceledReadNeverInventsIdentityDifference(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	_, args := f.plan(t)
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(ctx, args, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{historyRead: func(_, step string) {
		if step == "archive-original" {
			cancel()
		}
	}})
	if code != 2 || !strings.Contains(diagnostic.String(), "context canceled") || strings.Contains(diagnostic.String(), "is absent") || strings.Contains(diagnostic.String(), "differs") || output.Len() != 0 || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("canceled observation invented an archive contradiction or changed custody", code, diagnostic.String())
	}
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("cancellation destroyed the original continuation", code, issue)
	}
}

func TestEconomicConservationArchiveChangedCustodyStopsBeforeNextSourceRead(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	var observed uint64
	var samples int
	var output, diagnostic bytes.Buffer
	hooks := monitorServiceHooks{afterEvent: func(context.Context, string) {
		samples++
		observed = f.source.claimReads.Load()
		file, err := os.OpenFile(f.request.ArchivePath, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Error(err)
			return
		}
		_, writeErr := file.Write([]byte(" "))
		if err := errors.Join(writeErr, file.Close()); err != nil {
			t.Error(err)
		}
	}, wait: func(context.Context, string, time.Duration) bool { return samples < 2 }}
	code := runMainWithMonitorHooks(f.ctx, append(f.source.args(t), "--follow"), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	if code != 3 || observed == 0 || f.source.claimReads.Load() != observed || strings.Count(output.String(), "\n") != 1 {
		t.Fatal("changed retained owner reached new source reads or another sample", code, observed, f.source.claimReads.Load(), diagnostic.String())
	}
}

func TestEconomicConservationRenewalPublicPreservesOriginalPolicyAndReviewChain(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, true)
	original := f.source.state(t)
	resources := f.source.policy.initialResources()
	resources.ActiveFacts, resources.ReadBudgetSeconds = 16384, 600
	f.sign(t, resources)
	firstReview := *f.request.Renewal
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if state.PolicyHash != original.PolicyHash || state.Native.Cursor != original.Native.Cursor || state.Vault.Cursor != original.Vault.Cursor || state.Renewal == nil || *state.Renewal != firstReview {
		t.Fatal("resource adoption reset original identity or progress", state)
	}
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	resources.IndexEntries *= 2
	f.sign(t, resources)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	summary := f.sample(t, monitorServiceHooks{})
	state = f.source.state(t)
	if summary.Resources != resources || state.PolicyHash != original.PolicyHash || state.Renewal == nil || state.Renewal.Ordinal != 2 || state.Renewal.Previous != rootObjectHash(firstReview) || state.Archive.LastRenewalHash != rootObjectHash(firstReview) || summary.AcceptedClaims != 2 || summary.MatchedReceipts != 1 || summary.TargetMet != nil {
		t.Fatal("renewed original owner lost review/payment lineage", summary, state)
	}
}

func TestEconomicConservationRenewalPublicRejectsUnsignedShrunkOrForeignRevisions(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, true)
	resources := f.source.policy.initialResources()
	resources.ReadBudgetSeconds = 600
	f.sign(t, resources)
	accepted := *f.request.Renewal
	for _, kind := range []string{"signature", "shrink", "checkpoint", "key", "review"} {
		request := f.request
		renewal := accepted
		request.Renewal = &renewal
		switch kind {
		case "signature":
			renewal.Signature = strings.Repeat("0", 128)
		case "shrink":
			renewal.To.ActiveFacts--
		case "checkpoint":
			renewal.Original.Path += ".other"
		case "key":
			copied := *request.Policy.Continuation
			copied.ApprovalPublicKey = "0x" + strings.Repeat("42", 32)
			request.Policy.Continuation = &copied
		case "review":
			renewal.ReviewSha256 = request.Policy.Continuation.ReviewSha256
			message, err := renewal.signingBytes()
			if err != nil {
				t.Fatal(err)
			}
			renewal.Signature = hex.EncodeToString(ed25519.Sign(f.key, message))
		}
		path, hash := f.document(t, "rejected-request.json", request)
		before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
		var output, diagnostic bytes.Buffer
		code := runMain(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic)
		if code != 2 || output.Len() != 0 || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
			t.Fatal("unreviewed operational change altered original owner", kind, code, diagnostic.String())
		}
	}
}

func TestEconomicConservationLegacyPolicyCannotEnrollResourceApprover(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	f.request.Renewal = &economicConservationRenewal{Schema: economicConservationRenewalSchema, Original: f.request.Original}
	path, hash := f.document(t, "legacy-request.json", f.request)
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic)
	if code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "cannot enroll") {
		t.Fatal("legacy combined owner acquired a new signer", code, diagnostic.String())
	}
}

func TestEconomicConservationArchivePublicForecastRefusesWithoutDroppingUnknowns(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	f.request.FutureSegments = 64
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	path, hash := f.document(t, "capacity-request.json", f.request)
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic)
	if code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "two-times") || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("forecast failure pruned liabilities or performed archive effects", code, diagnostic.String())
	}
	state := f.source.state(t)
	if len(state.Lots) != 2 || len(state.Captures) != 1 || state.Captures[0].AmountDifferenceAlpha == nil || *state.Captures[0].AmountDifferenceAlpha != "14" {
		t.Fatal("unresolved capture changed under a capacity hold", state)
	}
}

func TestEconomicConservationPublicMoreThan128ReviewsRetainsOriginalLiabilities(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, true)
	original := f.source.state(t)
	resources := f.source.policy.initialResources()
	var firstReview string
	for index := uint64(1); index <= 130; index++ {
		resources.ReadBudgetSeconds++
		f.sign(t, resources)
		if index == 1 {
			firstReview = f.request.Renewal.ReviewSha256
		}
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("cumulative operational continuation failed", index, code, issue)
		}
		if index != 130 {
			f.reset(t)
		}
	}
	state := f.source.state(t)
	if state.Archive == nil || len(state.Archive.Segments) != 130 || state.Renewal == nil || state.Renewal.Ordinal != 130 || state.PolicyHash != original.PolicyHash || len(state.Lots) != 1 || state.Lots[0].Id != original.Lots[0].Id || !reflect.DeepEqual(state.Carry, original.Carry) || !reflect.DeepEqual(state.Credits, original.Credits) {
		t.Fatal("fixed review window reset source liabilities or original policy", state)
	}
	var reads atomic.Uint64
	summary := f.sample(t, monitorServiceHooks{historyRead: func(_, step string) {
		if step == "conservation-archive-admission" {
			reads.Add(1)
		}
	}})
	if reads.Load() != 130 || summary.Resources.ReadBudgetSeconds != 430 || summary.AggregatePayments != 1 || summary.EarningOccurrences != 2 || summary.TargetMet != nil {
		t.Fatal("public restart lost full review chain or original economic facts", reads.Load(), summary)
	}
	f.reset(t)
	resources.ReadBudgetSeconds++
	f.sign(t, resources)
	f.request.Renewal.ReviewSha256 = firstReview
	message, err := f.request.Renewal.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	f.request.Renewal.Signature = hex.EncodeToString(ed25519.Sign(f.key, message))
	path, hash := f.document(t, "old-review.json", f.request)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic); code != 2 || !strings.Contains(diagnostic.String(), "reused") || output.Len() != 0 {
		t.Fatal("an archived review reference became a new operational approval", code, diagnostic.String())
	}
}

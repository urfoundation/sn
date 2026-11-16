// Signed operational changes cross the actual archive and public fee consumer.
// The cryptographic context comes from the real synthetic chain964 export;
// the Go subprocess peer remains a test protocol, not a live-runtime claim.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
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
)

// Only the independently reviewed callsite source identity changes; the real
// signed transaction, receipt proof and native boundary remain the originals.
func economicConservationRevisionTestRequest(t *testing.T, review string) economicNativeFeeRequest {
	t.Helper()
	directory := os.Getenv("URNETWORK_ECONOMIC_FEE_FIXTURE_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("exact Server chain964 fee export directory is required")
	}
	input, _, job := historicalFeeContextTestFixtureAt(t, filepath.Join(directory, "success.json"), "success", "pair")
	job.ObservationProfile.SourceReviewSha256 = historicalReplayDigest(sha256.Sum256([]byte(review)))
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.Job.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	input.Job.Sha256 = monitorReadDigest(raw)
	request, _, _ := economicNativeFeeTestRequestForContext(t, input, job)
	return request
}

// Tests use one visibly synthetic original approver. Production only verifies
// its independently provisioned public key and never loads a private key.
func economicConservationRevisionTestSign(t *testing.T, revision *economicConservationFeeRevision) {
	t.Helper()
	message, err := revision.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x67}, ed25519.SeedSize))
	revision.Signature = hex.EncodeToString(ed25519.Sign(key, message))
}

func economicConservationRevisionTestAdoption(t *testing.T, f *economicConservationArchiveFixture, to economicNativeFeePolicy) *economicConservationFeeRevision {
	t.Helper()
	state := f.source.state(t)
	from, err := state.nativeFeeAuthority(f.source.policy)
	if err != nil || from == nil {
		t.Fatal("original fee revision authority is absent", err)
	}
	previous, ordinal := rootObjectHash(f.source.policy.FeeAuthority), uint64(1)
	if state.Archive != nil && state.Archive.FeeRevisionHead != nil {
		previous, ordinal = state.Archive.FeeRevisionHead.Hash, state.Archive.FeeRevisionHead.Ordinal+1
	}
	if state.FeeRevision != nil {
		previous, ordinal = rootObjectHash(state.FeeRevision), state.FeeRevision.Ordinal+1
	}
	revision := &economicConservationFeeRevision{Schema: economicConservationFeeRevisionSchema, PolicyHash: f.source.policy.identityHash(), Original: f.request.Original, Ordinal: ordinal, Previous: previous, ReviewSha256: monitorReadDigest([]byte(fmt.Sprintf("synthetic fee adoption review %d", ordinal))), From: *from, To: to}
	economicConservationRevisionTestSign(t, revision)
	f.request.FeeRevision = revision
	return revision
}

// A refused public plan cannot mutate the source namespace or manufacture a
// partial replacement plan, regardless of whether refusal is before or after read.
func economicConservationRevisionTestRefuse(t *testing.T, f *economicConservationArchiveFixture, request economicConservationArchiveRequest, issue string) {
	t.Helper()
	path, pin := f.document(t, "refused-fee-revision.json", request)
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", pin}, &output, &diagnostic)
	if code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), issue) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("unadmitted fee revision changed original owner or lost exact refusal", code, diagnostic.String(), issue)
	}
}

func TestEconomicConservationPublicFeeRevisionRetainsOriginalKeyReviewAndArchivedPolicies(t *testing.T) {
	f, originalRequest := newEconomicConservationFeeRetirementFixture(t, "pair")
	original := f.source.state(t)
	var first economicConservationFeeRevision
	for ordinal := uint64(1); ordinal <= 2; ordinal++ {
		request := economicConservationRevisionTestRequest(t, fmt.Sprintf("synthetic runtime review %d", ordinal))
		revision := economicConservationRevisionTestAdoption(t, f, request.Policy)
		if ordinal == 1 {
			first = *revision
		}
		prior, err := os.ReadFile(f.source.checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("original-key fee adoption failed", ordinal, code, issue)
		}
		retained, err := os.ReadFile(f.request.ArchivePath)
		if err != nil || !bytes.Equal(prior, retained) {
			t.Fatal("fee revision rewrote its original checkpoint", ordinal, err)
		}
		summary, code, issue := economicConservationRetirementTestSample(t, f, request)
		state := f.source.state(t)
		authority, err := state.nativeFeeAuthority(f.source.policy)
		if code != 0 || err != nil || authority == nil || *authority != request.Policy || state.PolicyHash != original.PolicyHash || state.FeeRevision == nil || state.FeeRevision.Ordinal != ordinal || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.OriginalRequests != ordinal+1 || summary.AdmittedNativeFees.AuthenticatedFees != 1 || summary.NativeFeeWithdrawalRao != nil || summary.NativeFeeRefundRao != nil || summary.TargetMet != nil || len(state.NativeFeeObligations) == 0 {
			t.Fatal("fee revision lost original identity, replay admission or selected census", ordinal, code, issue, err, summary)
		}
		if ordinal == 2 && (state.Archive.FeeRevisionHead == nil || state.Archive.FeeRevisionHead.Hash != rootObjectHash(first) || state.FeeRevision.Previous != rootObjectHash(first)) {
			t.Fatal("fee revision replaced archived original review lineage", state.FeeRevision, state.Archive)
		}
		f.reset(t)
		f.request.RetireNativeFees = true
	}
	archivedPolicy := economicConservationRevisionTestRequest(t, "synthetic runtime review 1")
	summary, code, issue := economicConservationRetirementTestSample(t, f, archivedPolicy)
	if code != 0 || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.OriginalRequests != 4 || summary.AdmittedNativeFees.AuthenticatedFees != 1 {
		t.Fatal("revision forgot an archived policy still needed for original backlog", code, issue, summary)
	}
	// A later context under the initial admitted profile still uses its own
	// original request signature. Historical approval remains bounded custody.
	backlog := economicConservationRetirementTestRequest(t, "pair")
	summary, code, issue = economicConservationRetirementTestSample(t, f, backlog)
	if code != 0 || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.OriginalRequests != 5 || summary.AdmittedNativeFees.AuthenticatedFees != 1 {
		t.Fatal("revision forgot historical policy authority or recounted original fee", code, issue, summary)
	}
	if err := os.Remove(originalRequest.Context.Job.Path); err != nil {
		t.Fatal(err)
	}
	before := *summary.AdmittedNativeFees
	summary, code, issue = economicConservationRetirementTestSample(t, f, originalRequest)
	if code != 0 || summary.AdmittedNativeFees == nil || !reflect.DeepEqual(before, *summary.AdmittedNativeFees) {
		t.Fatal("archived original request lost admission after revision or reran deleted input", code, issue, summary)
	}
}

func TestEconomicConservationPublicFeePolicyRequiresSignedRevisionBeforeReplay(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	request := economicConservationRevisionTestRequest(t, "synthetic unadmitted runtime review")
	path, pin := f.document(t, "unadmitted-fee-request.json", request)
	var starts atomic.Uint64
	var output, diagnostic bytes.Buffer
	args := append(f.source.args(t), "--native-fee-request", path, "--native-fee-request-sha256", pin)
	code := runMainWithMonitorHooks(f.ctx, args, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{nativeFeeReplay: historicalReplayHooks{beforeStart: func(context.Context, *os.File) { starts.Add(1) }}})
	var summary economicConservationSummary
	if err := decodePlanJson(output.Bytes(), &summary); err != nil {
		t.Fatal("optional fee refusal must still publish healthy siblings", code, diagnostic.String(), err)
	}
	if code != 3 || starts.Load() != 0 || !summary.NativeCurrent || !summary.VaultCurrent || summary.NativeFeeHeldRequest != rootObjectHash(planFileReference{Path: path, Sha256: pin}) || summary.NativeFeeHeldPolicy != rootObjectHash(request.Policy) || !strings.Contains(summary.NativeFeeIssue, "retained policy authority") || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.OriginalRequests != 1 {
		t.Fatal("unadmitted fee policy dispatched replay or rejected healthy siblings", code, starts.Load(), summary)
	}
	f.reset(t)
	f.request.RetireNativeFees = true
	economicConservationRevisionTestAdoption(t, f, request.Policy)
	_, apply := f.plan(t)
	if code, issue := f.apply(t, apply, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("reviewed fee adoption", code, issue)
	}
	summary, code, issue := economicConservationRetirementTestSample(t, f, request)
	if code != 0 || summary.NativeFeeIssue != "" || summary.NativeFeeHeldRequest != "" || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.OriginalRequests != 2 {
		t.Fatal("signed fee adoption could not retry exact previously quarantined request", code, issue, summary)
	}
}

func TestEconomicConservationPublicFeeRevisionDoesNotClearOriginalIntegrityQuarantine(t *testing.T) {
	f, request := newEconomicConservationFeeRetirementFixture(t, "pair")
	raw, err := os.ReadFile(request.Approval.Path)
	if err != nil {
		t.Fatal(err)
	}
	var approval economicNativeFeeApproval
	if err := decodePlanJson(raw, &approval); err != nil {
		t.Fatal(err)
	}
	foreign := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x68}, ed25519.SeedSize))
	economicNativeFeeTestSign(t, &request, &approval, foreign)
	summary, code, issue := economicConservationRetirementTestSample(t, f, request)
	if code != 3 || summary.NativeFeeHeldRequest == "" || summary.NativeFeeHeldPolicy != "" || !strings.Contains(summary.NativeFeeIssue, "signature is invalid") {
		t.Fatal("original proof conflict was not separately quarantined", code, issue, summary)
	}
	held := summary.NativeFeeHeldRequest
	f.reset(t)
	f.request.RetireNativeFees = true
	next := economicConservationRevisionTestRequest(t, "synthetic independent later runtime review")
	economicConservationRevisionTestAdoption(t, f, next.Policy)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if state.NativeFeeHeldRequest != held || state.NativeFeeHeldPolicy != "" || !strings.Contains(state.NativeFeeIssue, "signature is invalid") {
		t.Fatal("fee policy adoption cleared unrelated original integrity quarantine", state.NativeFeeHeldRequest, state.NativeFeeIssue)
	}
	summary, code, issue = economicConservationRetirementTestSample(t, f, request)
	if code != 3 || summary.NativeFeeHeldRequest != held || !summary.NativeCurrent || !summary.VaultCurrent || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.OriginalRequests != 1 {
		t.Fatal("retained signature conflict reran or stopped healthy continuation after revision", code, issue, summary)
	}
}

func TestEconomicConservationPublicFeeRevisionRefusesForeignAndSkippedLineage(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	request := economicConservationRevisionTestRequest(t, "synthetic proposed review")
	accepted := *economicConservationRevisionTestAdoption(t, f, request.Policy)
	for _, kind := range []string{"signature", "ordinal", "predecessor", "checkpoint", "key", "network"} {
		revision, candidate := accepted, f.request
		candidate.FeeRevision = &revision
		issue := "signature"
		switch kind {
		case "signature":
			revision.Signature = strings.Repeat("0", 128)
		case "ordinal":
			revision.Ordinal++
			economicConservationRevisionTestSign(t, &revision)
			issue = "exact original predecessor"
		case "predecessor":
			revision.Previous = monitorReadDigest([]byte("synthetic foreign predecessor"))
			economicConservationRevisionTestSign(t, &revision)
			issue = "exact original predecessor"
		case "checkpoint":
			revision.Original.Sha256 = monitorReadDigest([]byte("synthetic different checkpoint"))
			economicConservationRevisionTestSign(t, &revision)
			issue = "another original checkpoint"
		case "key":
			revision.To.ApprovalPublicKey = "0x" + strings.Repeat("42", 32)
		case "network":
			revision.To.EvmChainId++
		}
		economicConservationRevisionTestRefuse(t, f, candidate, issue)
	}
}

func TestEconomicConservationPublicFeeRevisionCannotReuseArchivedAdoptionReview(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	var review string
	for ordinal := 1; ordinal <= 2; ordinal++ {
		request := economicConservationRevisionTestRequest(t, fmt.Sprintf("synthetic review reuse predecessor %d", ordinal))
		revision := economicConservationRevisionTestAdoption(t, f, request.Policy)
		if ordinal == 1 {
			review = revision.ReviewSha256
		}
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal(code, issue)
		}
		f.reset(t)
		f.request.RetireNativeFees = true
	}
	request := economicConservationRevisionTestRequest(t, "synthetic third runtime review")
	revision := economicConservationRevisionTestAdoption(t, f, request.Policy)
	revision.ReviewSha256 = review
	economicConservationRevisionTestSign(t, revision)
	economicConservationRevisionTestRefuse(t, f, f.request, "reused its original or archived review")
}

func TestEconomicConservationPublicFeeRevisionLostAcknowledgmentReconcilesExactHeads(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	request := economicConservationRevisionTestRequest(t, "synthetic lost acknowledgment review")
	revision := *economicConservationRevisionTestAdoption(t, f, request.Policy)
	plan, args := f.plan(t)
	hooks := monitorServiceHooks{syncDirectory: func(_, step string, file *os.File) error {
		if step == "checkpoint" {
			return errors.Join(file.Sync(), syscall.EIO)
		}
		return file.Sync()
	}}
	if code, issue := f.apply(t, args, &bytes.Buffer{}, hooks); code != 2 {
		t.Fatal("lost fee revision acknowledgment became success", code, issue)
	}
	for range 2 {
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("exact fee revision retry could not reconcile retained heads", code, issue)
		}
	}
	raw, err := os.ReadFile(f.source.checkpoint)
	state := f.source.state(t)
	if err != nil || monitorReadDigest(raw) != plan.Next.Sha256 || state.FeeRevision == nil || *state.FeeRevision != revision || len(state.Archive.Segments) != 1 {
		t.Fatal("fee revision retry changed ordinal or original publication", err, state.FeeRevision)
	}
	if summary, code, issue := economicConservationRetirementTestSample(t, f, request); code != 0 || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.AuthenticatedFees != 1 {
		t.Fatal("reconciled revision lost actual fee consumption", code, issue, summary)
	}
}

func TestEconomicConservationPublicFeeRevisionHeadCannotReplaceRetainedSignature(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	request := economicConservationRevisionTestRequest(t, "synthetic original retained revision")
	economicConservationRevisionTestAdoption(t, f, request.Policy)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	f.reset(t)
	f.request.RetireNativeFees = true
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if state.FeeRevision != nil || state.Archive.FeeRevisionHead == nil {
		t.Fatal("exact signed revision did not enter original archive")
	}
	state.Archive.FeeRevisionHead.Policy.ProfileSha256 = monitorReadDigest([]byte("synthetic self sealed replacement"))
	state.ContentHash = state.hash()
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
	before, reads := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint)), f.source.claimReads.Load()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "summary differs from exact original checkpoints") || f.source.claimReads.Load() != reads || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("self sealed fee policy head replaced its original signed archive", code, diagnostic.String())
	}
}

func TestEconomicConservationFeeRevisionDecodedHeadCannotDispatch(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	request := economicConservationRevisionTestRequest(t, "synthetic held revision authority")
	economicConservationRevisionTestAdoption(t, f, request.Policy)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	f.reset(t)
	f.request.RetireNativeFees = true
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	path, pin := f.document(t, "unadmitted-archive-fee-request.json", request)
	var reads atomic.Uint64
	worker := newEconomicConservationFeeWorker(f.ctx, f.source.policy, planFileReference{Path: path, Sha256: pin}, 300*time.Second, monitorServiceHooks{beforeNativeFeeRead: func(context.Context, context.CancelFunc) { reads.Add(1) }})
	worker.start(&state)
	result, ready := worker.take(true)
	if err := worker.close(); err != nil {
		t.Fatal(err)
	}
	if !ready || result.evidence != nil || result.err == nil || !strings.Contains(result.err.Error(), "admitted original archive custody") || reads.Load() != 0 {
		t.Fatal("decoded fee head dispatched before held original archive admission", ready, result.err, reads.Load())
	}
}

func TestEconomicConservationLegacyPolicyCannotEnrollFeeRevisionApprover(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	f.request.FeeRevision = &economicConservationFeeRevision{Schema: economicConservationFeeRevisionSchema, Original: f.request.Original}
	economicConservationRevisionTestRefuse(t, f, f.request, "cannot enroll a fee revision approver")
}

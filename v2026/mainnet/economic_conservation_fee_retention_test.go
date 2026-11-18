// These tests cross the public verifier, archive plan/apply, owned reopen and
// live sample boundaries. The fee engine peer is synthetic, while its signed
// transaction/receipt/native proof inputs are the actual Server fixture export.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func economicConservationRetirementTestRequest(t *testing.T, mode string) economicNativeFeeRequest {
	t.Helper()
	directory := os.Getenv("URNETWORK_ECONOMIC_FEE_FIXTURE_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("exact Server chain964 fee export directory is required")
	}
	input, _, job := historicalFeeContextTestFixtureAt(t, filepath.Join(directory, "success.json"), "success", mode)
	request, _, _ := economicNativeFeeTestRequestForContext(t, input, job)
	return request
}

func economicConservationRetirementTestSample(t *testing.T, f *economicConservationArchiveFixture, request economicNativeFeeRequest) (economicConservationSummary, int, string) {
	t.Helper()
	path, digest := f.document(t, "native-fee-request.json", request)
	args := append(f.source.args(t), "--native-fee-request", path, "--native-fee-request-sha256", digest)
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, args, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if output.Len() != 0 {
		if err := decodePlanJson(output.Bytes(), &summary); err != nil {
			t.Fatal(err)
		}
	}
	return summary, code, diagnostic.String()
}

func newEconomicConservationFeeRetirementFixture(t *testing.T, mode string) (*economicConservationArchiveFixture, economicNativeFeeRequest) {
	t.Helper()
	request := economicConservationRetirementTestRequest(t, mode)
	f := newEconomicConservationArchiveFixture(t, false, func(source *economicConservationFixture) {
		source.policy.FeeAuthority = &request.Policy
		source.writePolicy(t)
	})
	if summary, code, issue := economicConservationRetirementTestSample(t, f, request); code != 0 || summary.AdmittedNativeFees == nil {
		t.Fatal("actual admitted fee report did not reach original checkpoint", code, issue)
	}
	f.reset(t)
	f.request.RetireNativeFees = true
	return f, request
}

func TestEconomicConservationPublicFeeRetirementKeepsOriginalProofUnknownsAndRetry(t *testing.T) {
	f, request := newEconomicConservationFeeRetirementFixture(t, "pair")
	before := f.source.state(t)
	original, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := before.feeSummary(f.source.policy)
	if err != nil {
		t.Fatal(err)
	}
	plan, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("exact fee retirement failed", code, issue)
	}
	archived, err := os.ReadFile(f.request.ArchivePath)
	if err != nil || !bytes.Equal(archived, original) {
		t.Fatal("fee retirement rewrote the original proof checkpoint", err)
	}
	after := f.source.state(t)
	compact, err := os.ReadFile(f.source.checkpoint)
	if err != nil || len(after.NativeFees) != 0 || after.Archive == nil || !after.Archive.retiresFees(plan.Archive) || !reflect.DeepEqual(expected, after.Archive.NativeFees) || uint64(len(after.NativeFeeObligations)) != expected.MissingFees || len(compact) >= len(original) {
		t.Fatal("fee retirement kept proof payload hot or lost exact selected knowledge", err, after.Archive)
	}
	if err := os.Remove(request.Context.Job.Path); err != nil {
		t.Fatal(err)
	}
	summary, code, issue := economicConservationRetirementTestSample(t, f, request)
	if code != 0 || !summary.NativeCurrent || !summary.VaultCurrent || summary.AdmittedNativeFees == nil || !reflect.DeepEqual(summary.AdmittedNativeFees, expected) || summary.NativeFeeWithdrawalRao != nil || summary.NativeFeeRefundRao != nil || summary.TargetMet != nil || summary.AdmittedNativeFees.MissingFees == 0 || summary.AdmittedNativeFees.WithdrawalRao != nil {
		t.Fatal("archived fee retry reread a deleted job or turned partial evidence into authority", code, issue, summary)
	}
	if len(f.source.state(t).NativeFees) != 0 {
		t.Fatal("archived request retry restored its full proof to the active head")
	}
}

func TestEconomicConservationPublicRetiredFeeOverlapCannotRepeatOriginalDebit(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	before := f.source.state(t).Archive.NativeFees
	request := economicConservationRetirementTestRequest(t, "pair")
	summary, code, issue := economicConservationRetirementTestSample(t, f, request)
	if code != 0 || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.OriginalRequests != 2 || summary.AdmittedNativeFees.SelectedTransactions != before.SelectedTransactions || summary.AdmittedNativeFees.AuthenticatedFees != before.AuthenticatedFees || summary.AdmittedNativeFees.MissingFees != before.MissingFees {
		t.Fatal("new archive request counted an original retired native fee twice", code, issue, summary.AdmittedNativeFees)
	}
	f.reset(t)
	f.request.RetireNativeFees = true
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	reopened := f.sample(t, monitorServiceHooks{})
	if !reflect.DeepEqual(reopened.AdmittedNativeFees, summary.AdmittedNativeFees) || len(f.source.state(t).NativeFees) != 0 {
		t.Fatal("second original fee retirement changed the cumulative deduplicated census")
	}
}

func TestEconomicConservationPublicRetiredUnknownFeeCanGainObservedRefund(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "missing")
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	before := f.source.state(t).Archive.NativeFees
	if before.AuthenticatedFees != 0 || before.MissingFees == 0 {
		t.Fatal("missing-refund original unexpectedly acquired known fees")
	}
	request := economicConservationRetirementTestRequest(t, "pair")
	summary, code, issue := economicConservationRetirementTestSample(t, f, request)
	if code != 0 || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.SelectedTransactions != before.SelectedTransactions || summary.AdmittedNativeFees.AuthenticatedFees != 1 || summary.AdmittedNativeFees.MissingFees+1 != before.MissingFees || summary.NativeFeeWithdrawalRao != nil || summary.TargetMet != nil {
		t.Fatal("retirement erased an unknown obligation or blocked actual later fee knowledge", code, issue, summary.AdmittedNativeFees)
	}
	if uint64(len(f.source.state(t).NativeFeeObligations)) != before.MissingFees-1 {
		t.Fatal("new original refund failed to resolve its exact hot obligation")
	}
}

func TestEconomicConservationPublicRetiredUnknownFeeMustRemainHot(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "missing")
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if len(state.NativeFeeObligations) == 0 || uint64(len(state.NativeFeeObligations)) != state.Archive.NativeFees.MissingFees {
		t.Fatal("retired unknown fees did not remain hot under original proof identities")
	}
	state.NativeFeeObligations = nil
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
	reads := f.source.claimReads.Load()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || output.Len() != 0 || reads != f.source.claimReads.Load() || !strings.Contains(diagnostic.String(), "active head dropped or changed an original unknown fee obligation") {
		t.Fatal("restored fee head erased original unknown obligations", code, diagnostic.String())
	}
}

func TestEconomicConservationPublicRetiredKnownFeeConflictPreservesPriorProof(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	original, err := os.ReadFile(f.request.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	before := f.source.state(t).Archive.NativeFees
	request := economicConservationRetirementTestRequest(t, "zero")
	summary, code, issue := economicConservationRetirementTestSample(t, f, request)
	if code != 3 || summary.NativeFeeHeldRequest == "" || summary.NativeFeeIssue == "" || !summary.NativeCurrent || !summary.VaultCurrent || summary.VaultCursor.Number != 13 || !reflect.DeepEqual(summary.AdmittedNativeFees, before) {
		t.Fatal("conflicting known refund replaced archived proof or stopped healthy siblings", code, issue, summary)
	}
	after, err := os.ReadFile(f.request.ArchivePath)
	if err != nil || !bytes.Equal(original, after) || len(f.source.state(t).NativeFees) != 0 {
		t.Fatal("refused known fee changed the original archive or active proof set", err)
	}
}

func TestEconomicConservationPublicFeeArchiveSummaryCannotSelfSealNewKnowledge(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "missing")
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	state.Archive.NativeFees.AuthenticatedFees++
	state.Archive.NativeFees.MissingFees--
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
	reads := f.source.claimReads.Load()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || output.Len() != 0 || reads != f.source.claimReads.Load() || !strings.Contains(diagnostic.String(), "archive summary differs from exact original checkpoints") {
		t.Fatal("self-sealed archived sum invented fee authority before original admission", code, diagnostic.String())
	}
}

func TestEconomicConservationLegacyHotFeeArchiveCanContinueWithExplicitRetirement(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	f.request.RetireNativeFees = false
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	legacy := f.source.state(t)
	if len(legacy.NativeFees) != 1 || legacy.Archive.NativeFees != nil || len(legacy.Archive.FeeRetirements) != 0 {
		t.Fatal("legacy fee compaction grammar changed without explicit retirement")
	}
	f.reset(t)
	f.request.RetireNativeFees = true
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if len(state.NativeFees) != 0 || len(state.Archive.Segments) != 2 || len(state.Archive.FeeRetirements) != 1 || summary.AdmittedNativeFees == nil || summary.AdmittedNativeFees.OriginalRequests != 1 || summary.AdmittedNativeFees.AuthenticatedFees != 1 {
		t.Fatal("explicit retirement rewrote the legacy prefix or counted its repeated proof", state.Archive, summary.AdmittedNativeFees)
	}
}

func TestEconomicConservationFeeRetirementForecastRefusesWithoutPruningProof(t *testing.T) {
	f, _ := newEconomicConservationFeeRetirementFixture(t, "pair")
	f.request.FutureIndexEntries = 64 * 1024
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	path, hash := f.document(t, "capacity-fee-request.json", f.request)
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic)
	if code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "two-times") || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) || len(f.source.state(t).NativeFees) != 1 {
		t.Fatal("fee retirement capacity refusal discarded original evidence", code, diagnostic.String())
	}
}

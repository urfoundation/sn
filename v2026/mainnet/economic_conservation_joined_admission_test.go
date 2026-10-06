//go:build linux

// The archive planner must admit one original checkpoint for all consumers.
// Fee retirement cannot leave Claim/native adoption using its earlier copy.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// The original fee verifier and Claim HTTP reader supply their own facts.
// Native approvals are real signatures, with no fabricated completed VM job.
func newEconomicConservationJoinedAdoptionFixture(t *testing.T) *economicConservationArchiveFixture {
	t.Helper()
	request := economicConservationRetirementTestRequest(t, "pair")
	f, references, _ := newEconomicConservationNativeRenewalFixture(t, func(source *economicConservationFixture) {
		source.policy.FeeAuthority = &request.Policy
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x75}, ed25519.SeedSize))
		source.policy.Claims[0].HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: monitorReadDigest([]byte("synthetic original joined Claim review")), InitialCapacity: monitorHistoryCapacity{Segments: 128, CatalogBytes: maximumMonitorHistoryCatalogBytes, HeldReaders: 128}}
	})
	original := f.source.state(t)
	state, err := admitEconomicConservationNativeFees(f.ctx, f.source.policy, &original, request, time.Minute, historicalReplayHooks{})
	if err != nil || state == nil || len(state.NativeFees) != 1 || len(original.NativeFees) != 0 {
		t.Fatal("original fee admission did not retain its detached proof", err)
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
	f.request.RetireNativeFees = true
	next := f.source.policy.Claims[0]
	next.FreshnessSeconds++
	claim, code, issue := economicConservationClaimWindowTestPropose(t, f, next, monitorReadDigest([]byte("synthetic joined Claim revision")))
	if code != 0 {
		t.Fatal("original Claim proposal", code, issue)
	}
	economicConservationClaimWindowTestSign(t, &claim.Window)
	f.request.ClaimWindows = []economicConservationClaimWindow{claim.Window}
	native, code, issue := economicConservationNativeRenewalTestPropose(t, f, references, monitorReadDigest([]byte("synthetic joined native revision")))
	if code != 0 {
		t.Fatal("original native proposal", code, issue)
	}
	economicConservationNativeRenewalTestSign(t, &native.Adoption)
	f.request.NativeRenewal = &native.Adoption
	return f
}

func TestEconomicConservationArchiveJoinsFeeRetirementClaimAndNativeAdoption(t *testing.T) {
	f := newEconomicConservationJoinedAdoptionFixture(t)
	original := f.source.state(t)
	originalRaw, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	expectedFees, err := original.feeSummary(f.source.policy)
	if err != nil || expectedFees == nil || expectedFees.MissingFees == 0 {
		t.Fatal("original exact selected fee census is absent", err)
	}
	plan, args := f.plan(t)
	var reached atomic.Bool
	hooks := monitorServiceHooks{syncDirectory: func(role, step string, directory *os.File) error {
		if err := directory.Sync(); err != nil {
			return err
		}
		if role == economicConservationRole && step == "checkpoint" && reached.CompareAndSwap(false, true) {
			return syscall.EIO
		}
		return nil
	}}
	if code, issue := f.apply(t, args, &bytes.Buffer{}, hooks); code == 0 || !reached.Load() || !strings.Contains(issue, syscall.EIO.Error()) {
		t.Fatal("joined adoption did not reach the real lost acknowledgement", code, issue)
	}
	for range 2 {
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("joined adoption did not reconcile original and next owners", code, issue)
		}
	}
	after := f.source.state(t)
	retained, err := os.ReadFile(f.request.ArchivePath)
	if err != nil || !bytes.Equal(retained, originalRaw) || after.PolicyHash != original.PolicyHash || after.Native.Cursor != original.Native.Cursor || len(after.NativeFees) != 0 || after.NativeRenewal == nil || after.NativeRenewal.Ordinal != 1 || len(after.ClaimWindows) != 1 || after.ClaimWindows[0].Ordinal != 1 || !reflect.DeepEqual(after.ClaimStates[0].Epochs, original.ClaimStates[0].Epochs) || after.Archive == nil || !reflect.DeepEqual(after.Archive.NativeFees, expectedFees) || uint64(len(after.NativeFeeObligations)) != expectedFees.MissingFees {
		t.Fatal("joined adoption lost original liabilities, approvals or partial fee knowledge", err, after.Archive)
	}
	raw, err := os.ReadFile(f.source.checkpoint)
	if err != nil || monitorReadDigest(raw) != plan.Next.Sha256 {
		t.Fatal("joined retry changed exact durable next checkpoint", err)
	}
	// A second public archive must rebuild and validate every prior view,
	// retaining the two signed heads and unknown fee obligations after restart.
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("reopened joined archive lost original authority", code, issue)
	}
	reopened := f.source.state(t)
	if len(reopened.Archive.ClaimHeads) != 1 || reopened.Archive.NativeApprovalHead == nil || !reflect.DeepEqual(reopened.Archive.NativeFees, expectedFees) || !reflect.DeepEqual(reopened.NativeFeeObligations, after.NativeFeeObligations) {
		t.Fatal("subsequent compaction erased a joined original lineage", reopened.Archive)
	}
}

func TestEconomicConservationArchiveFailedJoinedAdoptionKeepsOriginalEvidence(t *testing.T) {
	f := newEconomicConservationJoinedAdoptionFixture(t)
	before := f.source.state(t)
	request := f.request
	request.ClaimWindows = append([]economicConservationClaimWindow(nil), request.ClaimWindows...)
	request.ClaimWindows[0].HighestEpoch++
	economicConservationClaimWindowTestSign(t, &request.ClaimWindows[0])
	economicConservationRevisionTestRefuse(t, f, request, "exact original liabilities")
	after := f.source.state(t)
	if !reflect.DeepEqual(before, after) || len(after.NativeFees) != 1 || after.Archive != nil || after.NativeRenewal != nil || len(after.ClaimWindows) != 0 {
		t.Fatal("failed joined admission published candidate-private fee or approval state")
	}
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("same unchanged original did not admit its valid joined successor", code, issue)
	}
}

func TestEconomicConservationInitialClaimCensusRequiresActualObservation(t *testing.T) {
	source := newEconomicConservationFixture(t, false)
	state := newEconomicConservationState(source.policy)
	state.ContentHash = state.hash()
	if err := state.validate(t.Context(), source.policy); err == nil || !strings.Contains(err.Error(), "bounded observation and exact epoch census") {
		t.Fatal("unobserved Claim checkpoint acquired durable readiness", err)
	}
	before := source.claimReads.Load()
	observeEconomicConservationTestClaims(t, t.Context(), source, state)
	state.ContentHash = state.hash()
	if err := state.validate(t.Context(), source.policy); err != nil || source.claimReads.Load()-before != uint64(len(source.policy.Claims)) || len(state.ClaimStates[0].Epochs) != len(source.policy.Claims[0].Epochs) {
		t.Fatal("actual original HTTP observation did not establish bounded Claim census", err)
	}
}

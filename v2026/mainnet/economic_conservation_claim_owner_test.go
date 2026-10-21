//go:build linux || darwin

// Exact copied bytes do not move independently signed Claim authority to a
// different prepared owner. Ordinary compaction and physical restore preserve
// that original logical path even when no active signed window remains.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// One real full window precedes each copied-owner refusal. The archived case
// compacts again without a new signed window, leaving only the original head.
func economicConservationClaimOwnerFixture(t *testing.T, archived bool) *economicConservationArchiveFixture {
	t.Helper()
	f := newEconomicConservationHistoryAdmissionFixture(t, 1)
	if archived {
		f.reset(t)
		_, args := f.plan(t)
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("original Claim owner could not archive its current window", code, issue)
		}
		state := f.source.state(t)
		if len(state.ClaimWindows) != 0 || len(state.Archive.ClaimHeads) != 1 || state.Archive.ClaimHeads[0].Ordinal != 1 {
			t.Fatal("Claim owner fixture did not retain the archived-only original head")
		}
	}
	return f
}

// This second owner is independently provisioned in the admitted root and
// receives the exact original bytes through the real snapshot publisher.
func economicConservationClaimOwnerCopy(t *testing.T, f *economicConservationArchiveFixture) (string, []byte) {
	t.Helper()
	original := f.source.checkpoint
	raw, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(original), "copied-claim-checkpoint.json")
	provisionMonitorTestCustodyProfile(t, path, f.source.policy.storageKind(), int(f.source.policy.storageMaximum()))
	owner, err := f.source.policy.openHistorySnapshot(f.ctx, path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(owner.publish(raw, nil), owner.close()); err != nil {
		t.Fatal(err)
	}
	copy, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(copy, raw) {
		t.Fatal("copied Claim fixture changed original checkpoint bytes", err)
	}
	f.source.checkpoint = path
	f.reset(t)
	return original, raw
}

// Each refused route leaves both physical owners and every unknown epoch
// intact. Reopening the actual original then continues ordinary observations.
func economicConservationClaimOwnerUnchanged(t *testing.T, f *economicConservationArchiveFixture, original string, raw []byte) {
	t.Helper()
	for _, path := range []string{original, f.source.checkpoint} {
		current, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(current, raw) {
			t.Fatal("refused Claim owner changed original or copied checkpoint", path, err)
		}
	}
	f.source.checkpoint = original
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if summary.MatchedReceipts != 1 || !summary.NativeCurrent || !summary.VaultCurrent || len(state.ClaimStates[0].Epochs) != 128 || state.ClaimStates[0].Epochs[0].Epoch != 2 {
		t.Fatal("refused copied Claim owner stopped original complete progress", summary)
	}
}

func TestEconomicConservationClaimOwnerCopyRefusesLiveReads(t *testing.T) {
	for _, archived := range []bool{false, true} {
		f := economicConservationClaimOwnerFixture(t, archived)
		original, raw := economicConservationClaimOwnerCopy(t, f)
		before := mainnetNamespaceTest(t, filepath.Dir(original))
		reads := f.source.claimReads.Load()
		var output, diagnostic bytes.Buffer
		code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
		if code == 0 || output.Len() != 0 || reads != f.source.claimReads.Load() || !strings.Contains(diagnostic.String(), "Claim") || !strings.Contains(diagnostic.String(), "moved the original checkpoint") {
			t.Fatal("copied Claim checkpoint reached live sources or publication", archived, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(original))) {
			t.Fatal("copied Claim live refusal changed physical custody", archived)
		}
		economicConservationClaimOwnerUnchanged(t, f, original, raw)
	}
}

func TestEconomicConservationClaimOwnerCopyRefusesWindowProposal(t *testing.T) {
	for _, archived := range []bool{false, true} {
		f := economicConservationClaimOwnerFixture(t, archived)
		original, raw := economicConservationClaimOwnerCopy(t, f)
		_, code, issue := economicConservationClaimWindowTestPropose(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte("synthetic copied Claim owner review")))
		if code == 0 || !strings.Contains(issue, "moved the original checkpoint") {
			t.Fatal("copied Claim checkpoint acquired a new signing proposal", archived, code, issue)
		}
		economicConservationClaimOwnerUnchanged(t, f, original, raw)
	}
}

func TestEconomicConservationClaimOwnerCopyRefusesArchivePlan(t *testing.T) {
	for _, archived := range []bool{false, true} {
		f := economicConservationClaimOwnerFixture(t, archived)
		original, raw := economicConservationClaimOwnerCopy(t, f)
		path, pin := f.document(t, "copied-owner-archive-request.json", f.request)
		before := mainnetNamespaceTest(t, filepath.Dir(original))
		var output, diagnostic bytes.Buffer
		code := runMain(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", pin}, &output, &diagnostic)
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "moved the original checkpoint") {
			t.Fatal("copied Claim checkpoint acquired an archive continuation plan", archived, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(original))) {
			t.Fatal("copied Claim archive refusal changed physical custody", archived)
		}
		economicConservationClaimOwnerUnchanged(t, f, original, raw)
	}
}

// Original copied-source restore is valid at its original logical path. A
// caller cannot relabel identical copied bytes after the signed window retires.
func TestEconomicConservationClaimOwnerRestoreRetainsArchivedPath(t *testing.T) {
	for _, archived := range []bool{false, true} {
		f := economicConservationClaimOwnerFixture(t, archived)
		raw, err := os.ReadFile(f.source.checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		original := monitorHistoryReference{Path: f.source.checkpoint, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))}
		read := func(reference monitorHistoryReference) ([]byte, error) {
			if reference == original {
				return raw, nil
			}
			return os.ReadFile(reference.Path)
		}
		if err := validateEconomicConservationRestoreHistory(f.ctx, f.source.policy, original, read, nil, monitorServiceHooks{}); err != nil {
			t.Fatal("original Claim restore path refused exact retained history", archived, err)
		}
		original.Path = filepath.Join(filepath.Dir(original.Path), "relabeled-original.json")
		if err := validateEconomicConservationRestoreHistory(f.ctx, f.source.policy, original, read, nil, monitorServiceHooks{}); err == nil || !strings.Contains(err.Error(), "moved the original checkpoint") {
			t.Fatal("Claim restore relabeled an active or archived signed owner", archived, err)
		}
	}
}

// Both signatures remain valid. A new exact approval cannot rewrite the
// checkpoint path already admitted from its earlier signed original history.
func TestEconomicConservationClaimOwnerMixedSignedPathsRefuseAdmission(t *testing.T) {
	f := newEconomicConservationHistoryAdmissionFixture(t, 2)
	state := f.source.state(t)
	state.ClaimWindows[0].Original.Path = filepath.Join(filepath.Dir(f.source.checkpoint), "other-original.json")
	economicConservationClaimWindowTestSign(t, &state.ClaimWindows[0])
	state.ContentHash = state.hash()
	if err := state.validate(f.ctx, f.source.policy); err != nil {
		t.Fatal("mixed Claim path fixture did not retain valid original signatures", err)
	}
	view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, monitorServiceHooks{})
	if view != nil {
		_ = view.close()
	}
	if view != nil || err == nil || !strings.Contains(err.Error(), "moved the original checkpoint") {
		t.Fatal("complete Claim admission accepted different signed checkpoint owners", err)
	}
	f.sample(t, monitorServiceHooks{})
}

// A native proposal consumes the same combined owner, so the Claim path fence
// must apply even when the caller proposes a different component's approval.
func TestEconomicConservationClaimOwnerCopyRefusesNativeProposal(t *testing.T) {
	f, references, _ := newEconomicConservationNativeRenewalFixture(t, func(source *economicConservationFixture) {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x75}, ed25519.SeedSize))
		source.policy.Claims[0].HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: monitorReadDigest([]byte("synthetic original Claim owner review")), InitialCapacity: monitorHistoryCapacity{Segments: 512, CatalogBytes: maximumMonitorHistoryCatalogBytes, HeldReaders: 512}}
	})
	next := f.source.policy.Claims[0]
	next.FreshnessSeconds++
	_, _, args := economicConservationClaimWindowTestPlan(t, f, next, monitorReadDigest([]byte("synthetic native peer Claim window")))
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("native peer fixture could not adopt original Claim window", code, issue)
	}
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("native peer fixture could not archive original Claim window", code, issue)
	}
	original, raw := economicConservationClaimOwnerCopy(t, f)
	_, code, issue := economicConservationNativeRenewalTestPropose(t, f, references, monitorReadDigest([]byte("synthetic copied native peer proposal")))
	if code == 0 || !strings.Contains(issue, "moved the original checkpoint") {
		t.Fatal("native proposal borrowed a copied original Claim owner", code, issue)
	}
	current, err := os.ReadFile(f.source.checkpoint)
	if err != nil || !bytes.Equal(current, raw) {
		t.Fatal("refused native proposal changed copied Claim bytes", err)
	}
	f.source.checkpoint = original
	f.reset(t)
	if _, code, issue := economicConservationNativeRenewalTestPropose(t, f, references, monitorReadDigest([]byte("synthetic original native peer proposal"))); code != 0 {
		t.Fatal("copied owner refusal stopped the original native proposal", code, issue)
	}
}

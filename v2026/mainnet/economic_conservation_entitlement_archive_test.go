// Archive controls use the public plan/apply owner and retain original receipt
// bytes. A self-sealed replacement must still fail the admitted lineage checks.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func newEconomicEntitlementArchiveFixture(t *testing.T, missing bool, usage ...uint64) (*economicConservationArchiveFixture, *economicEntitlementFixture) {
	t.Helper()
	var entitlement *economicEntitlementFixture
	f := newEconomicConservationArchiveFixture(t, false, func(source *economicConservationFixture) {
		entitlement = configureEconomicEntitlementFixture(t, source, usage...)
		entitlement.missing.Store(missing)
	})
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	expected := 0
	if missing {
		expected = 3
	}
	if code != expected {
		t.Fatal("original entitlement archive source did not reach second public page", code, diagnostic.String())
	}
	f.reset(t)
	return f, entitlement
}

func TestEconomicEntitlementPublicLateCensusCannotRelabelColdAcceptedLeaf(t *testing.T) {
	f, entitlement := newEconomicEntitlementArchiveFixture(t, true, 8)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("pending original artifact archive", code, issue)
	}
	before := f.source.state(t)
	if len(before.Claims) != 0 || before.Archive.Counts.Claims != 2 {
		t.Fatal("negative did not move the original paid claim into held archive custody")
	}
	entitlement.missing.Store(false)
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	after := f.source.state(t)
	record := economicEntitlementRecord(t, f.source)
	if code != 3 || !record.CensusHeld || record.Census != nil || !strings.Contains(record.CensusIssue, "complete original payout leaf census") || len(after.Claims) != 0 || after.Archive.Counts.Claims != before.Archive.Counts.Claims || after.Archive.Counts.Payments != before.Archive.Counts.Payments {
		t.Fatal("late equal-total census replaced an already paid original leaf", code, diagnostic.String(), record)
	}
}

func TestEconomicEntitlementPublicArchiveKeepsOriginalCensusAndColdClaims(t *testing.T) {
	f, entitlement := newEconomicEntitlementArchiveFixture(t, false)
	before := economicEntitlementRecord(t, f.source)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public entitlement archive failed", code, issue)
	}
	state := f.source.state(t)
	if state.Archive == nil || state.Archive.Counts.Claims != 2 || len(state.Claims) != 0 || before.Census == nil {
		t.Fatal("paid claims were not actually retired before next census reconciliation", state)
	}
	summary := f.sample(t, monitorServiceHooks{})
	after := economicEntitlementRecord(t, f.source)
	if after.Census != nil || after.CensusReference == nil || after.CensusReference.ContentHash != before.Census.ContentHash || entitlement.artifactReads.Load() != 2 || summary.OriginalEntitlements.CompleteRoots != 1 || summary.TargetMet != nil {
		t.Fatal("archive/restart reread or changed original entitlement authority", summary, after)
	}
}

func TestEconomicEntitlementPublicArchiveCannotDropOriginalReceipt(t *testing.T) {
	f, entitlement := newEconomicEntitlementArchiveFixture(t, false)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if err := state.index(); err != nil {
		t.Fatal(err)
	}
	index, exists := state.entitlementIds["3/1"]
	if !exists || state.Entitlements[index].CensusReference == nil {
		t.Fatal("original active census was not retained before forgery")
	}
	state.Entitlements[index].CensusReference = nil
	state.ContentHash = state.hash()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := openMonitorHistorySnapshot(f.ctx, f.source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(owner.publish(append(raw, '\n'), nil), owner.close()); err != nil {
		t.Fatal(err)
	}
	reads, claims := entitlement.artifactReads.Load(), f.source.claimReads.Load()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || output.Len() != 0 || reads != entitlement.artifactReads.Load() || claims != f.source.claimReads.Load() || !strings.Contains(diagnostic.String(), "original archived census") {
		t.Fatal("self-sealed head dropped original census before public source reads", code, diagnostic.String())
	}
	actual, err := os.ReadFile(f.source.checkpoint)
	if err != nil || !bytes.Equal(actual, append(raw, '\n')) {
		t.Fatal("refusal rewrote the observed conflicting head", err)
	}
}

func TestEconomicEntitlementPublicArtifactAfterArchiveChecksOriginalPaidLeaves(t *testing.T) {
	f, entitlement := newEconomicEntitlementArchiveFixture(t, true)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("pending artifact could not preserve original archive", code, issue)
	}
	before := f.source.state(t)
	if len(before.Claims) != 0 || before.Archive.Counts.Claims != 2 || economicEntitlementRecord(t, f.source).Census != nil {
		t.Fatal("missing artifact control did not retain cold paid claims and hot obligation")
	}
	entitlement.missing.Store(false)
	summary := f.sample(t, monitorServiceHooks{})
	after := f.source.state(t)
	if summary.OriginalEntitlements.CompleteRoots != 1 || economicEntitlementRecord(t, f.source).Census == nil || len(after.Claims) != 0 || after.Archive.Counts.Claims != before.Archive.Counts.Claims || after.Archive.Counts.Payments != before.Archive.Counts.Payments {
		t.Fatal("late complete census lost cold original claims or replayed payment", summary)
	}
}

func TestEconomicEntitlementPublicCapacityHoldsUntilSignedArchiveGrowth(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x61}, ed25519.SeedSize))
	var entitlement *economicEntitlementFixture
	f := newEconomicConservationArchiveFixture(t, false, func(source *economicConservationFixture) {
		entitlement = configureEconomicEntitlementFixture(t, source, 6, 16)
		source.policy.MaximumFacts = 16
		source.policy.Continuation = &economicConservationContinuationPolicy{Schema: economicConservationResourcesSchema, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: monitorReadDigest([]byte("synthetic original entitlement capacity authority")), Initial: economicConservationResources{ActiveFacts: 16, ReadBudgetSeconds: 300, ArchiveSegments: 128, IndexEntries: 65536, IndexBytes: 64 * 1024 * 1024}}
		source.writePolicy(t)
	})
	f.key = key
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	before := economicEntitlementRecord(t, f.source)
	if code != 3 || before.Census != nil || before.CensusHeld || before.CensusCapacityBasis == "" || entitlement.artifactReads.Load() != 2 {
		t.Fatal("actual populated artifact did not retain a typed capacity hold", code, diagnostic.String(), before)
	}
	output.Reset()
	diagnostic.Reset()
	code = runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || entitlement.artifactReads.Load() != 2 || economicEntitlementRecord(t, f.source).CensusCapacityBasis != before.CensusCapacityBasis {
		t.Fatal("unchanged capacity repeated original HTTP/leaf work", code, diagnostic.String())
	}
	f.reset(t)
	resources := f.source.policy.initialResources()
	resources.ActiveFacts = 256
	f.sign(t, resources)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("public signed capacity adoption", code, issue)
	}
	summary := f.sample(t, monitorServiceHooks{})
	after := economicEntitlementRecord(t, f.source)
	if after.Census == nil || len(after.Census.Artifact.Providers) != 16 || after.CensusCapacityBasis != "" || after.CensusHeld || entitlement.artifactReads.Load() != 4 || summary.OriginalEntitlements.CompleteRoots != 1 || summary.TargetMet != nil {
		t.Fatal("signed growth lost the original capacity-held obligation", summary, after)
	}
}

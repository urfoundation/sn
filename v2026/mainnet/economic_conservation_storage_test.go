// Actual full-census exports traverse the public observer, owned snapshot and
// archive commands. Sizing never substitutes a smaller economic population.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func economicConservationTestStorageProfile() *economicConservationStorageProfile {
	return &economicConservationStorageProfile{Schema: economicConservationStorageSchema, Kind: economicConservationStorageKind, MaximumBytes: economicConservationStorageMaximum, ReviewSha256: monitorReadDigest([]byte("synthetic full UID physical storage review"))}
}

func economicConservationTestFullUidStorage(t *testing.T, count int, restore bool) {
	t.Helper()
	f, _ := newEconomicConservationPrincipalFixture(t, fmt.Sprintf("yuma-capacity-%d", count), true, nil)
	first := f.sample(t, monitorServiceHooks{})
	if first.Yuma == nil || !first.Yuma.Current || len(first.Yuma.Active) != 1 || len(first.Yuma.Active[0].Allocations) != count || len(first.Yuma.Active[0].Records) != 2+3*count || first.Yuma.MinerDenominator == nil || *first.Yuma.MinerDenominator != "98" || first.FullQuantizationToleranceAlpha == nil || *first.FullQuantizationToleranceAlpha != "2" {
		t.Fatal("full original UID census did not pass through the public large owner", count, first)
	}
	raw, err := os.ReadFile(f.source.checkpoint)
	if err != nil || len(raw) <= maxRpcReplyBytes || len(raw) >= economicConservationStorageMaximum || first.FactsRemaining == 0 || first.Resources.headBytes() != economicConservationStorageMaximum {
		t.Fatal("complete original witness did not exercise the admitted physical head", count, len(raw), err, first.Resources)
	}
	for _, allocation := range first.Yuma.Active[0].Allocations[2:] {
		if allocation.ActualMiner != "0" || allocation.ActualValidator != "0" {
			t.Fatal("explicit original zero-stake UID acquired allocation", allocation)
		}
	}
	if restore {
		economicConservationTestRestoreLargeHead(t, f.source.policy, raw)
	}
	f.reset(t)
	plan, args := f.plan(t)
	if plan.RequiredBytes < 2*(uint64(len(raw))+2*economicConservationStorageMaximum) || plan.RequiredHeadBytes > first.Resources.headBytes() {
		t.Fatal("large archive forecast borrowed a legacy byte reserve", plan)
	}
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("full UID archive failed", code, issue)
	}
	archived, err := os.ReadFile(f.request.ArchivePath)
	if err != nil || !bytes.Equal(archived, raw) {
		t.Fatal("full UID archive did not retain exact original bytes", err)
	}
	reader, original, err := f.source.policy.openHistoryReader(f.ctx, monitorHistoryReference{Path: f.request.ArchivePath, Sha256: monitorReadDigest(raw), Bytes: uint64(len(raw))})
	if err != nil {
		t.Fatal("large original reference could not reopen its physical owner", err)
	}
	if err := reader.close(); err != nil || !bytes.Equal(original, raw) {
		t.Fatal("large original reader changed the witness", err)
	}
	second := f.sample(t, monitorServiceHooks{})
	if second.Yuma == nil || !second.Yuma.Current || second.Yuma.Archived == nil || second.Yuma.Archived.Blocks != 1 || len(second.Yuma.Active) != 0 || *second.Yuma.MinerDenominator != "98" || *second.FullQuantizationToleranceAlpha != "2" || second.PolicyHash != first.PolicyHash || second.NativeCursor != first.NativeCursor || second.TargetMet != nil || second.ActualNativeOutcomeVerified || second.ActivationReady {
		t.Fatal("cold large owner lost full denominator or invented financial authority", second)
	}
}

func TestEconomicConservationPublic1024UidPhysicalHeadAndArchive(t *testing.T) {
	economicConservationTestFullUidStorage(t, 1024, false)
}
func TestEconomicConservationPublic2048UidPhysicalHeadArchiveAndRestore(t *testing.T) {
	economicConservationTestFullUidStorage(t, 2048, true)
}

// A small real witness isolates the signed byte-budget transition from the
// expensive full-census body; both use the same fixed physical owner format.
func newEconomicConservationStorageRenewalFixture(t *testing.T) *economicConservationArchiveFixture {
	t.Helper()
	f, _ := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	f.source.policy.StorageProfile = economicConservationTestStorageProfile()
	f.key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize))
	f.source.policy.Continuation = &economicConservationContinuationPolicy{Schema: economicConservationResourcesSchema, ApprovalPublicKey: "0x" + hex.EncodeToString(f.key.Public().(ed25519.PublicKey)), ReviewSha256: monitorReadDigest([]byte("synthetic original physical resource review")), Initial: economicConservationResources{ActiveFacts: f.source.policy.MaximumFacts, ReadBudgetSeconds: 300, ArchiveSegments: 128, IndexEntries: 65536, IndexBytes: 64 * 1024 * 1024}}
	f.source.checkpoint = filepath.Join(filepath.Dir(f.source.checkpoint), "large-original-head.json")
	provisionMonitorTestCustodyProfile(t, f.source.checkpoint, economicConservationStorageKind, economicConservationStorageMaximum)
	f.source.writePolicy(t)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	return f
}

func TestEconomicConservationPublicSignedHeadBytesPreservePhysicalOwnerAndHistory(t *testing.T) {
	f := newEconomicConservationStorageRenewalFixture(t)
	original := f.source.state(t)
	resources := f.source.policy.initialResources()
	resources.HeadBytes = 8 * 1024 * 1024
	f.sign(t, resources)
	review := *f.request.Renewal
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if summary.Resources.HeadBytes != resources.HeadBytes || state.PolicyHash != original.PolicyHash || state.Native.Cursor != original.Native.Cursor || state.Renewal == nil || *state.Renewal != review || state.Archive.Resources.HeadBytes != 0 {
		t.Fatal("signed head budget replaced physical authority or prior resource bytes", summary, state.Renewal)
	}
	f.reset(t)
	resources.HeadBytes = 16 * 1024 * 1024
	f.sign(t, resources)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state = f.source.state(t)
	if state.Renewal.Previous != rootObjectHash(review) || state.Archive.Resources.HeadBytes != 8*1024*1024 || state.PolicyHash != original.PolicyHash {
		t.Fatal("cold head budget lineage was reset", state.Renewal, state.Archive.Resources)
	}
}

func TestEconomicConservationPublicHeadBudgetCannotEnrollPhysicalProfile(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, true)
	resources := f.source.policy.initialResources()
	resources.HeadBytes = 2 * 1024 * 1024
	f.sign(t, resources)
	path, hash := f.document(t, "foreign-profile-request.json", f.request)
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "original physical storage profile") || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("operational review enrolled an unadmitted physical profile", code, diagnostic.String())
	}
}

func TestEconomicConservationPublicPhysicalProfileCannotReplaceOwnedHead(t *testing.T) {
	f, producer := newEconomicConservationPrincipalFixture(t, "yuma-legacy", true, nil)
	f.sample(t, monitorServiceHooks{})
	before, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	f.source.policy.StorageProfile = economicConservationTestStorageProfile()
	f.source.writePolicy(t)
	reads, requests := f.source.claimReads.Load(), producer.requests.Load()
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	after, err := os.ReadFile(f.source.checkpoint)
	if err != nil || code == 0 || output.Len() != 0 || reads != f.source.claimReads.Load() || requests != producer.requests.Load() || !bytes.Equal(before, after) || !strings.Contains(diagnostic.String(), "custody") {
		t.Fatal("new policy relabelled an existing physical head", code, diagnostic.String(), err)
	}
}

func TestEconomicConservationHeadBudgetRejectsShrinkAndForgedSignature(t *testing.T) {
	f := newEconomicConservationStorageRenewalFixture(t)
	resources := f.source.policy.initialResources()
	resources.HeadBytes = 8 * 1024 * 1024
	f.sign(t, resources)
	positive := *f.request.Renewal
	if err := positive.verify(f.source.policy); err != nil {
		t.Fatal("actual signed head budget prerequisite", err)
	}
	for _, mutate := range []func(*economicConservationRenewal){func(value *economicConservationRenewal) {
		value.To.HeadBytes = 2 * maxRpcReplyBytes
		value.From.HeadBytes = 4 * maxRpcReplyBytes
	}, func(value *economicConservationRenewal) { value.Signature = strings.Repeat("00", 64) }} {
		changed := positive
		mutate(&changed)
		if err := changed.verify(f.source.policy); err == nil {
			t.Fatal("shrinking or forged original head budget was admitted")
		}
	}
	legacy := f.source.policy
	legacy.StorageProfile = nil
	if legacy.identityHash() == f.source.policy.identityHash() {
		t.Fatal("physical profile disappeared from original policy identity")
	}
	bare, err := json.Marshal(economicConservationResources{ActiveFacts: 256, ReadBudgetSeconds: 300, ArchiveSegments: 128, IndexEntries: 65536, IndexBytes: 64 * 1024 * 1024})
	if err != nil || bytes.Contains(bare, []byte("head_bytes")) {
		t.Fatal("nil head budget changed legacy checksum grammar", err)
	}
}

// Publication and portable restoration use the original actual large witness,
// not a padding file. Complete combined producer restoration has its own scope.
func economicConservationTestRestoreLargeHead(t *testing.T, policy economicConservationPolicy, original []byte) {
	t.Helper()
	source := newStoragePreparationCommandFixture(t)
	owner := storagePreparationSnapshotOwner(t, economicConservationStorageKind, "conservation.json", economicConservationStorageMaximum)
	storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{owner})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	path := filepath.Join(source.root, "conservation.json")
	writer, err := policy.openHistorySnapshot(ctx, path, true)
	if err != nil {
		t.Fatal("public fresh large owner cannot open", err)
	}
	if err := errors.Join(writer.publish(original, nil), writer.close()); err != nil {
		t.Fatal("public large witness publication", err)
	}
	restoredFixture := storageSnapshotRestoreTarget(t, source, ctx, owner, false)
	restored := restoredFixture.apply(t)
	reader, raw, err := policy.openHistoryReader(restored, monitorHistoryReference{Path: path, Sha256: monitorReadDigest(original), Bytes: uint64(len(original))})
	if err != nil {
		t.Fatal("actual large restored owner cannot reopen", err)
	}
	defer reader.close()
	if !bytes.Equal(raw, original) {
		t.Fatal("physical restore rewrote the original full witness")
	}
	if _, err := decodeEconomicConservation(restored, raw, policy); err != nil {
		t.Fatal("physical restore lost full original conservation semantics", err)
	}
	if old, err := openMonitorHistorySnapshot(restored, path, false); err == nil {
		old.close()
		t.Fatal("legacy one-MiB owner reinterpreted a large physical snapshot")
	}
}

func TestEconomicConservationPublicSignedHeadBudgetCannotShrink(t *testing.T) {
	f := newEconomicConservationStorageRenewalFixture(t)
	resources := f.source.policy.initialResources()
	resources.HeadBytes = 8 * 1024 * 1024
	f.sign(t, resources)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	f.reset(t)
	resources.HeadBytes = 16 * 1024 * 1024
	f.sign(t, resources)
	revision := *f.request.Renewal
	revision.To.HeadBytes = 4 * 1024 * 1024
	revision.Signature = ""
	// Sign the malformed transition directly so refusal cannot be masked by a
	// stale signature or by the canonical request formatter's earlier guard.
	raw, err := json.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	revision.Signature = hex.EncodeToString(ed25519.Sign(f.key, append([]byte(economicConservationRenewalSchema+"\x00"), raw...)))
	f.request.Renewal = &revision
	path, hash := f.document(t, "shrinking-head-request.json", f.request)
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", hash}, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 2 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "independent signature") || !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
		t.Fatal("correctly signed shrinking head budget bypassed original monotonic resources", code, diagnostic.String())
	}
}

func TestEconomicConservationPhysicalHeadRefusesOverflowWithoutReplacingOriginal(t *testing.T) {
	source := newStoragePreparationCommandFixture(t)
	profile := economicConservationPolicy{StorageProfile: economicConservationTestStorageProfile()}
	owner := storagePreparationSnapshotOwner(t, economicConservationStorageKind, "bounded.json", economicConservationStorageMaximum)
	storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{owner})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	writer, err := profile.openHistorySnapshot(ctx, filepath.Join(source.root, "bounded.json"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.close()
	original := []byte("synthetic original bounded physical bytes\n")
	if err := writer.publish(original, nil); err != nil {
		t.Fatal(err)
	}
	if err := writer.publish(bytes.Repeat([]byte{'x'}, economicConservationStorageMaximum+1), nil); err == nil {
		t.Fatal("physical owner accepted an over-profile replacement")
	}
	raw, present, err := writer.read()
	if err != nil || !present || !bytes.Equal(raw, original) {
		t.Fatal("oversized replacement changed original committed physical head", err)
	}
}

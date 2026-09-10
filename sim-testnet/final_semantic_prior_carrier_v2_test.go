// Real signed archive publication and isolated disk stores exercise the
// complete prior run/reference census without large source allocations.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urnetwork/server/v2026"
)

// One genuine external state reference joins the two original run files.
type finalPriorCarrierFixtureV2 struct {
	archive  *streamingCampaignEvidenceFixture
	manifest *campaignEvidenceManifestPayload
	owner    common.Address
	origins  []string
	carriers []FinalCollectedPriorCarrierV2
	read     finalPriorCarrierReaderV2
}

func newFinalPriorCarrierFixtureV2(t *testing.T) *finalPriorCarrierFixtureV2 {
	t.Helper()
	archive := newStreamingCampaignEvidenceFixture(t)
	referenced := []byte("exact prior receipt\n")
	if err := atomicWrite(filepath.Join(archive.stateDir, "receipts", "prior.bin"), referenced, 0o600); err != nil {
		t.Fatal(err)
	}
	locator, err := json.Marshal(map[string]any{"schema": "prior-reference-fixture-v2", "source": FinalArtifactLocator{Kind: "original-reference", URI: "receipts/prior.bin", ContentHash: bytesSHA256(referenced), SizeBytes: uint64(len(referenced))}})
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(archive.stateDir, "runs", archive.runId, "locator.json"), locator, 0o600); err != nil {
		t.Fatal(err)
	}
	archive.hashes["locator.json"] = bytesSHA256(locator)
	envelope, err := archive.publish(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := decodeCampaignEvidenceManifest(envelope)
	if err != nil || len(manifest.Files) != 3 || len(manifest.References) != 1 {
		t.Fatalf("complete original graph: %v %v", manifest, err)
	}
	stores := func(operator int) (server.BlobStore, error) { return archive.stores[operator], nil }
	archive.events = nil
	origins, carriers, err := captureFinalPriorCarriersV2(t.Context(), archive.cfg, archive.stateDir, archive.runId, manifest, envelope.Signer, stores)
	if err != nil {
		t.Fatal(err)
	}
	read, err := finalPriorCarrierStoreReaderV2(archive.cfg, archive.stateDir, archive.runId, stores)
	if err != nil {
		t.Fatal(err)
	}
	return &finalPriorCarrierFixtureV2{archive: archive, manifest: manifest, owner: envelope.Signer, origins: origins[:], carriers: carriers, read: read}
}

// Both scopes and both immutable content/history replicas retain the original
// bytes. The complete-wire hash is intentionally not the route's inner hash.
func TestFinalCaptureCapacityPriorCarriersPreserveCompleteTwoReplicaOriginals(t *testing.T) {
	fixture := newFinalPriorCarrierFixtureV2(t)
	if len(fixture.carriers) != 4 || len(fixture.archive.events) != 16 {
		t.Fatalf("capture census/readback count: %d %v", len(fixture.carriers), fixture.archive.events)
	}
	if err := verifyFinalPriorCarriersV2(t.Context(), fixture.archive.cfg, fixture.archive.runId, fixture.manifest, fixture.origins, fixture.carriers, fixture.owner, fixture.read); err != nil {
		t.Fatal(err)
	}
	if len(fixture.archive.events) != 32 {
		t.Fatalf("independent readback omitted a replica: %v", fixture.archive.events)
	}
	for _, carrier := range fixture.carriers {
		if carrier.WireHash == carrier.EnvelopeHash || carrier.WireHash == carrier.LocalHash || carrier.LocalBytes != carrier.WireBytes+1 || !bytes.Equal(carrier.LocalSuffix, []byte{'\n'}) {
			t.Fatal("route, public wire and proven local original collapsed")
		}
	}
	prior := &FinalCollectedPriorPhaseInputs{SemanticStatus: finalSemanticCapturePendingStatus, RunID: fixture.archive.runId, CarrierOrigins: fixture.origins, PublicCarriers: fixture.carriers}
	excluded, err := finalPriorCarrierExcludedPathsV2(prior)
	if err != nil || len(excluded) != 4 {
		t.Fatalf("exact prior path census: %v %v", excluded, err)
	}
	if _, hidden := excluded[campaignEvidenceLocalArchiveDirectory+"/"+fixture.archive.runId+"/files/unlisted.evidence.json"]; hidden {
		t.Fatal("an unlisted same-directory original inherited custody")
	}
	for path, carrier := range excluded {
		if err := verifyFinalPriorCarrierLocalFileV2(t.Context(), fixture.archive.stateDir, path, carrier); err != nil {
			t.Fatal(err)
		}
	}
}

// Even one missing original reference cannot become a complete prior phase.
func TestFinalCaptureCapacityPriorCarriersRejectMissingMember(t *testing.T) {
	fixture := newFinalPriorCarrierFixtureV2(t)
	before := len(fixture.archive.events)
	if err := verifyFinalPriorCarriersV2(t.Context(), fixture.archive.cfg, fixture.archive.runId, fixture.manifest, fixture.origins, fixture.carriers[1:], fixture.owner, fixture.read); err == nil {
		t.Fatal("missing manifest member was accepted")
	}
	if len(fixture.archive.events) != before {
		t.Fatal("incomplete census caused public reads")
	}
}

// A malicious route returns genuine correctly hashed bytes for another inner
// envelope. Both exact wire hashes still match; signed route identity refuses.
func TestFinalCaptureCapacityPriorCarriersRejectSwappedRouteWithCorrectBody(t *testing.T) {
	fixture := newFinalPriorCarrierFixtureV2(t)
	carriers := append([]FinalCollectedPriorCarrierV2(nil), fixture.carriers...)
	manifest := *fixture.manifest
	manifest.References = append([]campaignEvidenceFileEntry(nil), manifest.References...)
	original := carriers[0].EnvelopeHash
	forged := "sha256:" + strings.Repeat("e7", 32)
	if carriers[0].Scope != "reference" {
		t.Fatal("canonical full-census fixture order changed")
	}
	carriers[0].EnvelopeHash = forged
	manifest.References[0].EnvelopeHash = forged
	calls := 0
	read := func(ctx context.Context, origin, hash string, maximumBytes uint64) ([]byte, error) {
		calls++
		if hash == forged {
			hash = original
		}
		return fixture.read(ctx, origin, hash, maximumBytes)
	}
	if err := verifyFinalPriorCarriersV2(t.Context(), fixture.archive.cfg, fixture.archive.runId, &manifest, fixture.origins, carriers, fixture.owner, read); err == nil || calls != 1 {
		t.Fatalf("swapped inner route escaped exact wire checks: calls=%d err=%v", calls, err)
	}
}

// An altered suffix cannot be normalized into the producer's original bytes.
func TestFinalCaptureCapacityPriorCarriersRejectChangedLocalSuffix(t *testing.T) {
	fixture := newFinalPriorCarrierFixtureV2(t)
	carriers := append([]FinalCollectedPriorCarrierV2(nil), fixture.carriers...)
	carriers[0].LocalSuffix = []byte("\r\n")
	carriers[0].LocalBytes++
	if err := verifyFinalPriorCarriersV2(t.Context(), fixture.archive.cfg, fixture.archive.runId, fixture.manifest, fixture.origins, carriers, fixture.owner, fixture.read); err == nil {
		t.Fatal("changed retained suffix was normalized")
	}
	path, err := finalPriorCarrierLocalPathV2(fixture.archive.runId, fixture.carriers[0].Scope, fixture.carriers[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(fixture.archive.stateDir, filepath.FromSlash(path))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw[:len(raw)-1], '\r', '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := captureFinalPriorCarriersV2(t.Context(), fixture.archive.cfg, fixture.archive.stateDir, fixture.archive.runId, fixture.manifest, fixture.owner, func(operator int) (server.BlobStore, error) { return fixture.archive.stores[operator], nil }); err == nil {
		t.Fatal("changed actual local original was normalized")
	}
}

// The same body at an unapproved origin is not the independently retained pair.
func TestFinalCaptureCapacityPriorCarriersRejectCrossOrigin(t *testing.T) {
	fixture := newFinalPriorCarrierFixtureV2(t)
	origins := append([]string(nil), fixture.origins...)
	origins[1] = "https://unapproved.example"
	before := len(fixture.archive.events)
	if err := verifyFinalPriorCarriersV2(t.Context(), fixture.archive.cfg, fixture.archive.runId, fixture.manifest, origins, fixture.carriers, fixture.owner, fixture.read); err == nil {
		t.Fatal("cross-origin carrier became retained custody")
	}
	if len(fixture.archive.events) != before {
		t.Fatal("unapproved origin caused a source read")
	}
}

// Cancellation after one actual retained read ends the owner before replica 2.
func TestFinalCaptureCapacityPriorCarriersCancellationAfterActualRead(t *testing.T) {
	fixture := newFinalPriorCarrierFixtureV2(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	read := func(ctx context.Context, origin, hash string, maximumBytes uint64) ([]byte, error) {
		calls++
		raw, err := fixture.read(ctx, origin, hash, maximumBytes)
		cancel()
		return raw, err
	}
	if err := verifyFinalPriorCarriersV2(ctx, fixture.archive.cfg, fixture.archive.runId, fixture.manifest, fixture.origins, fixture.carriers, fixture.owner, read); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancelled prior read continued: calls=%d err=%v", calls, err)
	}
}

// Old phase encodings do not acquire zero-valued new census fields or change
// historical canonical hashes merely because the new decoder knows V2.
func TestFinalCaptureCapacityPriorCarriersLeaveLegacyWireUnchanged(t *testing.T) {
	raw, err := json.Marshal(&FinalCollectedPriorPhaseInputs{})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"carrier_origins", "public_carriers"} {
		if bytes.Contains(raw, []byte(field)) {
			t.Fatalf("legacy wire acquired %s: %s", field, raw)
		}
	}
}

//go:build linux || darwin

// Real private directories and signed Http observations exercise the count
// owner; file names, empty bytes and a second operator cannot create capacity.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Only request counting is wrapped; the real transport and body remain owned
// by the production reader and its actual local Http server.
type releaseCaptureCapacityV2Transport struct{ calls atomic.Uint64 }

func (self *releaseCaptureCapacityV2Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	self.calls.Add(1)
	return http.DefaultTransport.RoundTrip(request)
}

// A whole batch and a singleton for another operator share one exact private
// namespace. Retrying the original slot at the limit does not allocate a file.
func TestReleaseCaptureV2CapacityActualBatchAndSingletonShareOperatorCensus(t *testing.T) {
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	fixture.cfg.StateDir = newReleaseHeadV2TestStateDir(t)
	fixture.cfg.EvidenceV2.Bounds.MaxCaptureFiles = 2
	ctx, owner := fixture.owner(t, t.Context(), 8*1024*1024)
	fixture.cfg.EvidenceV2.Bounds.MaxCaptureFiles = 100
	reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
	transport := &releaseCaptureCapacityV2Transport{}
	reader.client.Transport = transport
	first, err := captureReleaseClientKeysV2(ctx, fixture.chain, reader, fixture.cfg.StateDir, fixture.domain, []protocol.ClientKeyObservationRequest{fixture.request}, releaseClientKeyHistoryTestResponseBytes, 1024*1024)
	if err != nil || len(first) != 1 {
		t.Fatalf("actual first batch: %v", err)
	}
	domain, request := fixture.domain, fixture.request
	domain.NoID = fixture.cfg.Operators[1].NoID
	request.ClientID[1] = 2
	secondReader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, domain.NoID)
	secondReader.client.Transport = transport
	_, secondHash, _, err := captureReleaseClientKeyV2(ctx, fixture.chain, secondReader, fixture.cfg.StateDir, domain, request, releaseClientKeyHistoryTestResponseBytes, 1024*1024)
	if err != nil || secondHash == "" {
		t.Fatalf("actual second operator: %v", err)
	}
	before := transport.calls.Load()
	again, err := captureReleaseClientKeysV2(ctx, fixture.chain, reader, fixture.cfg.StateDir, fixture.domain, []protocol.ClientKeyObservationRequest{fixture.request}, releaseClientKeyHistoryTestResponseBytes, 1024*1024)
	if err != nil || len(again) != 1 || again[0] != first[0] || transport.calls.Load() != before {
		t.Fatalf("exact-limit immutable retry changed bytes or transport: %v", err)
	}
	request.ClientID[1] = 3
	if value, err := captureReleaseClientKeysV2(ctx, fixture.chain, secondReader, fixture.cfg.StateDir, domain, []protocol.ClientKeyObservationRequest{request}, releaseClientKeyHistoryTestResponseBytes, 1024*1024); err == nil || value != nil || transport.calls.Load() != before {
		t.Fatalf("one-over combined operator escaped its copied owner: %v", err)
	}
	if err := owner.finish(nil); err != nil {
		t.Fatal(err)
	}
}

// Count is independent of content length; zero-byte unknown crash/hostile
// files consume slots and a repeated census cannot refund them.
func TestReleaseCaptureV2CapacityRealCensusExactCountBytesAndCancellation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"operator-one", "operator-two", ".interrupted"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := openAttemptPrivateDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := directory.close(); err != nil {
			t.Error(err)
		}
	}()
	for index := 0; index < 2; index++ {
		used, count, err := censusReleaseEvidenceCapturesV2(t.Context(), directory, 1, 3)
		if err != nil || used != 0 || count != 3 {
			t.Fatalf("exact physical count: bytes=%d count=%d error=%v", used, count, err)
		}
	}
	if _, _, err := censusReleaseEvidenceCapturesV2(t.Context(), directory, 1, 2); err == nil {
		t.Fatal("zero-byte third file escaped count bound")
	}
	if err := os.WriteFile(filepath.Join(root, "operator-two"), []byte{1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := censusReleaseEvidenceCapturesV2(t.Context(), directory, 1, 3); err == nil {
		t.Fatal("count capacity widened the byte allowance")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if used, count, err := censusReleaseEvidenceCapturesV2(ctx, directory, 2, 3); !errors.Is(err, context.Canceled) || used != 0 || count != 0 {
		t.Fatal("canceled census acquired a prefix")
	}
}

// Missing optional count fields preserve the original ceiling; large byte
// budgets and unrelated control fields never synthesize a larger file owner.
func TestReleaseCaptureV2CapacityPreservesDefaultCountAndIndependentIntentLimit(t *testing.T) {
	bounds := ReleaseEvidenceV2Bounds{MaxHistoryBytes: 16 * 1024 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024}
	if bounds.CaptureFileLimit() != 16384 || bounds.IntentFileLimit() != 64*1024*1024 {
		t.Fatal("missing explicit count or aggregate bytes granted another control owner")
	}
	bounds.MaxHistoryBytes = 1
	if bounds.IntentFileLimit() != 1 || bounds.CaptureFileLimit() != 16384 {
		t.Fatal("small history or absent count default changed")
	}
}

// A direct owner cannot bypass configuration validation with an unbounded
// count. Refusal precedes allocation charging and every real authority read.
func TestReleaseCaptureV2CapacityRejectsUnboundedOwnerBeforeAuthorityReads(t *testing.T) {
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	fixture.cfg.EvidenceV2.Bounds.MaxCaptureFiles = ^uint64(0)
	budget := &releaseHeadV2Budget{limit: 8 * 1024 * 1024}
	ctx, owner, err := newReleaseClientKeyAuthorityV2Reads(t.Context(), fixture.chain, &fixture.cfg, &fixture.artifact, fixture.hotkey, budget)
	if err == nil || ctx != nil || owner != nil || budget.used != 0 || len(fixture.rpc.counts()) != 0 {
		t.Fatalf("unbounded capture owner reached allocation or authority: %v", err)
	}
}

// The actual store reader admits a canonical empty control file at its exact
// limit and refuses one additional physical byte before decoding or replay.
func TestReleaseCaptureV2CapacityIntentStoreUsesControlNotAggregateAllowance(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := marshalAttemptSettlementV2JSON(t.Context(), &steeringIntentFile{Schema: steeringIntentSchema, History: []SteeringIntent{}}, 1024, true, true)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "steering-intents.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &releaseRuntimeV2{cfg: ReleaseConfig{EvidenceV2: ReleaseEvidenceV2Config{Bounds: ReleaseEvidenceV2Bounds{MaxControlBytes: uint64(len(raw)), MaxHistoryBytes: 4 * uint64(len(raw))}}}}
	store := &IntentStore{path: path, stateDir: root, v2: &releaseIntentV2Owner{runtime: runtime}}
	read, err := store.readV2(t.Context())
	if err != nil || read == nil || read.file.Current != nil || len(read.file.History) != 0 {
		t.Fatalf("exact empty store: %v", err)
	}
	if err := read.custody.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(bytes.Clone(raw), ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if read, err := store.readV2(t.Context()); err == nil || read != nil || !strings.Contains(err.Error(), "bounded private regular file") {
		t.Fatalf("one-over intent file borrowed aggregate history: %v", err)
	}
}

// A genuine native signature over a large flat envelope is still not proof
// of measurement authority. Its exact wire ceiling is separate from the much
// larger control owner used by trusted full measurement replay.
func TestReleaseCaptureV2CapacitySignedEnvelopeExactWireAndOneOver(t *testing.T) {
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 1)
	_, envelope := fixture.seal(t)
	raw, err := canonicalReleaseMeasurementEnvelopeBytes(envelope)
	if err != nil {
		t.Fatal(err)
	}
	envelope.DeploymentID += strings.Repeat("x", int(ReleaseMeasurementEnvelopeV2MaximumBytes)-len(raw))
	resignReleaseMeasurementEnvelopeV2Test(t, envelope, fixture.hotkey, ReleaseMeasurementEnvelopeSigningDomainV2)
	raw, err = canonicalReleaseMeasurementEnvelopeBytes(envelope)
	if err != nil || uint64(len(raw)) != ReleaseMeasurementEnvelopeV2MaximumBytes {
		t.Fatalf("exact envelope construction: %d %v", len(raw), err)
	}
	if _, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), raw, 64*1024*1024); err != nil {
		t.Fatalf("exact signed envelope: %v", err)
	}
	envelope.DeploymentID += "x"
	resignReleaseMeasurementEnvelopeV2Test(t, envelope, fixture.hotkey, ReleaseMeasurementEnvelopeSigningDomainV2)
	raw, err = canonicalReleaseMeasurementEnvelopeBytes(envelope)
	if err != nil || !json.Valid(raw) {
		t.Fatal("one-over signed source is malformed")
	}
	if _, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), raw, 64*1024*1024); err == nil {
		t.Fatal("control scratch allowance enlarged the envelope wire limit")
	}
}

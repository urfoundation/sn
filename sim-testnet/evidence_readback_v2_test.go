// Real Http responses are streamed from the already published isolated disk
// stores. Logical buffer counts do not depend on garbage-collector timing.
package main

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urnetwork/server/v2026/startifact"
)

// Routing changes only the test transport destination. Request host and the
// signed route remain the independently configured operator identities.
type campaignReadbackFixtureV2 struct {
	prior         *finalPriorCarrierFixtureV2
	probe         *liveScenarioProbe
	public        *PublicDeploymentManifest
	requests      atomic.Uint64
	activeReaders atomic.Uint64
	peakReaders   atomic.Uint64
	missingSecond atomic.Bool
	changedSecond atomic.Bool
}

func newCampaignReadbackFixtureV2(t *testing.T) *campaignReadbackFixtureV2 {
	t.Helper()
	prior := newFinalPriorCarrierFixtureV2(t)
	fixture := &campaignReadbackFixtureV2{prior: prior}
	cfg := prior.archive.cfg
	fixture.public = &PublicDeploymentManifest{DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, EvidenceTransportProfile: publicEvidenceTransportHTTPS}
	operators := map[string]int{}
	for index, origin := range prior.origins {
		parsed, err := url.Parse(origin)
		if err != nil {
			t.Fatal(err)
		}
		operators[parsed.Host] = index + 1
		fixture.public.Operators = append(fixture.public.Operators, PublicOperator{NoID: index + 1, APIURL: origin})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fixture.requests.Add(1)
		operator := operators[request.Host]
		if operator == 0 || request.URL.Path != "/sn/evidence" || operator == 2 && fixture.missingSecond.Load() {
			http.NotFound(response, request)
			return
		}
		store := prior.archive.stores[operator].BlobStore
		key, err := startifact.EvidenceContentKey(store, request.URL.Query().Get("hash"))
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		reader, err := store.Get(request.Context(), key)
		if err != nil {
			http.NotFound(response, request)
			return
		}
		active := fixture.activeReaders.Add(1)
		for previous := fixture.peakReaders.Load(); previous < active; previous = fixture.peakReaders.Load() {
			if fixture.peakReaders.CompareAndSwap(previous, active) {
				break
			}
		}
		defer fixture.activeReaders.Add(^uint64(0))
		defer reader.Close()
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(response, reader)
		if operator == 2 && fixture.changedSecond.Load() {
			_, _ = io.WriteString(response, " ")
		}
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := server.Client().Transport
	fixture.probe = &liveScenarioProbe{cfg: cfg, client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		routed := request.Clone(request.Context())
		routed.Host = request.URL.Host
		routed.URL.Scheme, routed.URL.Host = target.Scheme, target.Host
		return transport.RoundTrip(routed)
	})}}
	return fixture
}

// Every original is fetched at both real endpoints, but no raw tape is kept
// in the returned map. Peak source ownership is one largest original object.
func TestCampaignEvidenceReadbackV2KeepsMetadataNotCompleteRawCorpus(t *testing.T) {
	fixture := newCampaignReadbackFixtureV2(t)
	readback, err := fixture.probe.streamPublicCampaignArchiveV2(t.Context(), fixture.public, fixture.prior.owner.Hex(), fixture.prior.manifest)
	if err != nil {
		t.Fatal(err)
	}
	var total, largest uint64
	for _, group := range [][]campaignEvidenceFileEntry{fixture.prior.manifest.Files, fixture.prior.manifest.References} {
		for _, entry := range group {
			total += entry.Size
			largest = max(largest, entry.Size)
		}
	}
	if readback.sourceCount != 4 || readback.sourceBytes != total || readback.peakOriginalBytes != largest || total <= largest || len(readback.controls) != 0 || readback.retainedControlBytes != 0 {
		t.Fatalf("raw corpus escaped object ownership: %+v", readback)
	}
	if fixture.requests.Load() != 8 || fixture.peakReaders.Load() != 1 || fixture.activeReaders.Load() != 0 {
		t.Fatalf("read ownership: calls=%d peak=%d active=%d", fixture.requests.Load(), fixture.peakReaders.Load(), fixture.activeReaders.Load())
	}
	t.Logf("actual two-origin Http/disk readback: %d original bytes, peak owned original=%d, retained control=%d, concurrent store readers=%d; heap/RSS not inferred", total, readback.peakOriginalBytes, readback.retainedControlBytes, fixture.peakReaders.Load())
}

// A missing physical second replica is an actual error, never pending analysis.
func TestCampaignEvidenceReadbackV2MissingReplicaCannotBecomePending(t *testing.T) {
	fixture := newCampaignReadbackFixtureV2(t)
	fixture.missingSecond.Store(true)
	readback, err := fixture.probe.streamPublicCampaignArchiveV2(t.Context(), fixture.public, fixture.prior.owner.Hex(), fixture.prior.manifest)
	if readback != nil || err == nil || isFinalSemanticAnalysisPending(err) || fixture.requests.Load() != 2 {
		t.Fatalf("missing actual replica: %v %v reads=%d", readback, err, fixture.requests.Load())
	}
}

// Signature-equivalent extra whitespace still changes the retained wire.
func TestCampaignEvidenceReadbackV2RejectsChangedActualReplicaBytes(t *testing.T) {
	fixture := newCampaignReadbackFixtureV2(t)
	fixture.changedSecond.Store(true)
	if readback, err := fixture.probe.streamPublicCampaignArchiveV2(t.Context(), fixture.public, fixture.prior.owner.Hex(), fixture.prior.manifest); readback != nil || err == nil || isFinalSemanticAnalysisPending(err) {
		t.Fatalf("changed actual replica became custody: %v %v", readback, err)
	}
}

// Closing the first actual Http body cancels the owner at an explicit event.
// The second origin and remaining source objects are never requested.
func TestCampaignEvidenceReadbackV2CancellationJoinsActualResponse(t *testing.T) {
	fixture := newCampaignReadbackFixtureV2(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	transport := fixture.probe.client.Transport
	fixture.probe.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(request)
		if err == nil {
			response.Body = &streamingCampaignEvidenceRead{ReadCloser: response.Body, closed: cancel}
		}
		return response, err
	})
	readback, err := fixture.probe.streamPublicCampaignArchiveV2(ctx, fixture.public, fixture.prior.owner.Hex(), fixture.prior.manifest)
	if readback != nil || !errors.Is(err, context.Canceled) || fixture.requests.Load() != 1 {
		t.Fatalf("cancelled read owner continued: %v %v reads=%d", readback, err, fixture.requests.Load())
	}
}

// The production pending dispatcher cannot turn a fully replicated but
// incomplete campaign control set into an analysis-pending success.
func TestCampaignEvidenceReadbackV2MissingCampaignControlsRemainRealError(t *testing.T) {
	fixture := newCampaignReadbackFixtureV2(t)
	err := fixture.probe.verifyPublicCampaignCaptureV2(t.Context(), fixture.public, fixture.prior.owner.Hex(), nil, scenarioCompletePayload{}, nil, fixture.prior.manifest)
	if err == nil || isFinalSemanticAnalysisPending(err) || fixture.requests.Load() != 8 {
		t.Fatalf("missing campaign controls became pending: %v reads=%d", err, fixture.requests.Load())
	}
}

// Preserve the real transport reader; inject faults only at observable read
// or close boundaries and count exactly how much ownership was consumed.
type campaignReadbackBodyFailureV2 struct {
	io.ReadCloser
	readErr   error
	closeErr  error
	onClose   func()
	reads     int
	readBytes int64
	closes    int
}

// A read may return owned bytes and an error together; neither may be lost.
func (self *campaignReadbackBodyFailureV2) Read(raw []byte) (int, error) {
	self.reads++
	count, err := self.ReadCloser.Read(raw)
	self.readBytes += int64(count)
	if self.readErr == nil {
		return count, err
	}
	return count, errors.Join(err, self.readErr)
}

// Joining is synchronous, including the explicit cancellation callback.
func (self *campaignReadbackBodyFailureV2) Close() error {
	self.closes++
	err := self.ReadCloser.Close()
	if self.onClose != nil {
		self.onClose()
	}
	return errors.Join(err, self.closeErr)
}

// Even complete, correctly signed bytes from both real origins cannot hide a
// failed response close. A fresh retry must re-read every original replica.
func TestCampaignEvidenceReadbackV2RejectsActualReplicaCloseFailure(t *testing.T) {
	t.Parallel()
	for _, failRequest := range []int{1, 2} {
		fixture := newCampaignReadbackFixtureV2(t)
		transport := fixture.probe.client.Transport
		closeErr := errors.New("synthetic response close failure")
		requests := 0
		var failedBody *campaignReadbackBodyFailureV2
		fixture.probe.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response, err := transport.RoundTrip(request)
			if err != nil {
				return response, err
			}
			requests++
			if requests == failRequest {
				failedBody = &campaignReadbackBodyFailureV2{ReadCloser: response.Body, closeErr: closeErr}
				response.Body = failedBody
			}
			return response, nil
		})
		readback, err := fixture.probe.streamPublicCampaignArchiveV2(t.Context(), fixture.public, fixture.prior.owner.Hex(), fixture.prior.manifest)
		if readback != nil || !errors.Is(err, closeErr) || isFinalSemanticAnalysisPending(err) || requests != failRequest || failedBody == nil || failedBody.closes != 1 {
			t.Fatalf("replica%d close failure became accepted evidence: readback=%v err=%v requests=%d body=%+v", failRequest, readback, err, requests, failedBody)
		}
		fixture.probe.client.Transport = transport
		readback, err = fixture.probe.streamPublicCampaignArchiveV2(t.Context(), fixture.public, fixture.prior.owner.Hex(), fixture.prior.manifest)
		if err != nil || readback == nil || fixture.requests.Load() != uint64(failRequest+8) || fixture.activeReaders.Load() != 0 {
			t.Fatalf("fresh retry did not revalidate all originals after close failure: %v reads=%d", err, fixture.requests.Load())
		}
	}
}

// All failing causes remain available to errors.Is and partial bytes cannot
// escape to signature or evidence processing after read/close failure.
func TestCampaignEvidenceReadbackV2BodyJoinsReadAndCloseFailures(t *testing.T) {
	t.Parallel()
	readErr := errors.New("synthetic body read failure")
	closeErr := errors.New("synthetic body close failure")
	body := &campaignReadbackBodyFailureV2{ReadCloser: io.NopCloser(strings.NewReader("partial")), readErr: readErr, closeErr: closeErr}
	raw, err := readEvidenceHttpBody(t.Context(), body, 32)
	if raw != nil || !errors.Is(err, readErr) || !errors.Is(err, closeErr) || body.readBytes == 0 || body.closes != 1 {
		t.Fatalf("failed read ownership escaped: raw=%q err=%v body=%+v", raw, err, body)
	}
}

// Cancellation happens at the joined close event, after a successful read.
func TestCampaignEvidenceReadbackV2BodyRejectsCancellationDuringClose(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &campaignReadbackBodyFailureV2{ReadCloser: io.NopCloser(strings.NewReader("complete")), onClose: cancel}
	raw, err := readEvidenceHttpBody(ctx, body, 32)
	if raw != nil || !errors.Is(err, context.Canceled) || body.readBytes != 8 || body.closes != 1 {
		t.Fatalf("canceled close became complete evidence: raw=%q err=%v body=%+v", raw, err, body)
	}
}

// Inclusive bounds need one extra byte to distinguish a complete body from
// an oversized valid prefix. Empty content remains valid under a zero bound.
func TestCampaignEvidenceReadbackV2BodyPreservesExactAndOneOverBounds(t *testing.T) {
	t.Parallel()
	for _, sample := range []struct {
		value    string
		maximum  int64
		accepted bool
	}{
		{value: "", maximum: 0, accepted: true},
		{value: "x", maximum: 0, accepted: false},
		{value: "full", maximum: 4, accepted: true},
		{value: "full-extra", maximum: 4, accepted: false},
	} {
		body := &campaignReadbackBodyFailureV2{ReadCloser: io.NopCloser(strings.NewReader(sample.value))}
		raw, err := readEvidenceHttpBody(t.Context(), body, sample.maximum)
		if (err == nil) != sample.accepted || body.closes != 1 || body.readBytes > sample.maximum+1 {
			t.Fatalf("bounded response ownership changed for %+v: raw=%q err=%v body=%+v", sample, raw, err, body)
		}
		if sample.accepted && string(raw) != sample.value || !sample.accepted && raw != nil {
			t.Fatal("response bounds returned truncated or unowned evidence")
		}
	}
}

// Invalid admission must still release the received response without reading
// it. In particular maximum+1 must not overflow and accept an empty prefix.
func TestCampaignEvidenceReadbackV2BodyRejectsInvalidOwnersAndLimits(t *testing.T) {
	t.Parallel()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, sample := range []struct {
		ctx     context.Context
		maximum int64
	}{
		{ctx: nil, maximum: 4},
		{ctx: t.Context(), maximum: -1},
		{ctx: t.Context(), maximum: math.MaxInt64},
		{ctx: canceled, maximum: 4},
	} {
		body := &campaignReadbackBodyFailureV2{ReadCloser: io.NopCloser(strings.NewReader("synthetic"))}
		raw, err := readEvidenceHttpBody(sample.ctx, body, sample.maximum)
		if raw != nil || err == nil || body.reads != 0 || body.closes != 1 {
			t.Fatalf("invalid response owner consumed evidence: raw=%q err=%v body=%+v", raw, err, body)
		}
	}
	if raw, err := readEvidenceHttpBody(t.Context(), nil, 4); raw != nil || err == nil {
		t.Fatal("absent response body was accepted")
	}
}

// The payout collector's adjacent bounded reader must honor close errors on
// successful and rejected statuses, without decoding a partial payout body.
func TestCampaignEvidenceReadbackV2PayoutReaderJoinsCloseFailures(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		closeErr := errors.New("synthetic payout response close failure")
		body := &campaignReadbackBodyFailureV2{ReadCloser: io.NopCloser(strings.NewReader("synthetic payout")), closeErr: closeErr}
		raw, err := readBoundedResponse(t.Context(), &http.Response{StatusCode: status, Body: body}, 32)
		if raw != nil || !errors.Is(err, closeErr) || body.closes != 1 || status != http.StatusOK && body.reads != 0 {
			t.Fatalf("payout response close failure was ignored: status=%d raw=%q err=%v body=%+v", status, raw, err, body)
		}
	}
}

// Rejected status handling closes before observing cancellation, just like a
// successful bounded read; the response must never be read or closed twice.
func TestCampaignEvidenceReadbackV2PayoutReaderObservesRejectedCloseCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &campaignReadbackBodyFailureV2{ReadCloser: io.NopCloser(strings.NewReader("synthetic rejection")), onClose: cancel}
	raw, err := readBoundedResponse(ctx, &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body}, 32)
	if raw != nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "503") || body.reads != 0 || body.closes != 1 {
		t.Fatalf("rejected response sampled cancellation before close: raw=%q err=%v body=%+v", raw, err, body)
	}
}

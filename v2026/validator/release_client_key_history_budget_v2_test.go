//go:build linux || darwin

// Genuine signed transport and retained bytes expose the live collection's
// additional control ownership without widening a production admission bound.
package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// The plural endpoint refuses the original inadequate complete-body allowance.
// Retry must preserve actual previously captured bytes, never a partial member
// from the refused batch, and request each genuinely missing client only once.
func TestReleaseClientKeyHistoryControlBudgetRefusesAndReusesOriginalCaptures(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 15)
	var posts atomic.Uint64
	var bodyRefusals atomic.Uint64
	var stateLock sync.Mutex
	requestedKVs := map[string]uint64{}
	refusedClientKVs := map[string]bool{}
	requestCensus := func() map[string]uint64 {
		stateLock.Lock()
		defer stateLock.Unlock()
		return maps.Clone(requestedKVs)
	}
	for noId, measurement := range fixture.steerer.contexts {
		reader := measurement.ClientKeyHistory
		transport := reader.client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		reader.client.Transport = clientKeyHistoryTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || request.URL.Path != "/sn/client-key/observations" || request.GetBody == nil {
				return nil, errors.New("budget fixture did not use the actual plural request")
			}
			copyBody, err := request.GetBody()
			if err != nil {
				return nil, err
			}
			encoded, readErr := io.ReadAll(io.LimitReader(copyBody, protocol.MaxClientKeyObservationBatchRequestBytes+1))
			if err := errors.Join(readErr, copyBody.Close()); err != nil {
				return nil, err
			}
			batch, err := protocol.DecodeClientKeyObservationBatchRequest(encoded)
			if err != nil {
				return nil, err
			}
			func() {
				stateLock.Lock()
				defer stateLock.Unlock()
				for _, member := range batch.Requests {
					requestedKVs[strconv.FormatUint(noId, 10)+"/"+connect.Id(member.ClientID).String()]++
				}
			}()
			posts.Add(1)
			response, err := transport.RoundTrip(request)
			if response != nil && response.StatusCode == http.StatusInternalServerError {
				bodyRefusals.Add(1)
				func() {
					stateLock.Lock()
					defer stateLock.Unlock()
					for _, member := range batch.Requests {
						refusedClientKVs[strconv.FormatUint(noId, 10)+"/"+connect.Id(member.ClientID).String()] = true
					}
				}()
			}
			return response, err
		})
	}
	options := fixture.options(t)
	admittedAllowance := options.MaxControlBytes
	collectionBytes := releaseHeadV2AccountingCollectionBudget(t, fixture, options)
	originalAllowance := fixture.measurement.options(t).MaxControlBytes
	if originalAllowance != 1024*1024 || options.MaxControlBytes <= originalAllowance {
		t.Fatal("live fixture no longer distinguishes response ownership from measurement-only admission")
	}
	// A completed earlier bounded batch supplies a genuine immutable prefix.
	// It uses the same real producer, signatures, Rpc and descriptor readback;
	// the later refusal must not manufacture a partially accepted response.
	var seedBinding *ReleaseBindingMeasurement
	for index := range fixture.measurement.artifact.Bindings {
		if fixture.measurement.artifact.Bindings[index].Active {
			seedBinding = &fixture.measurement.artifact.Bindings[index]
			break
		}
	}
	if seedBinding == nil {
		t.Fatal("actual source has no active client for the retained-prefix control")
	}
	seedClient, err := connect.ParseId(seedBinding.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	seedDomain, seedRequest, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, seedBinding.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, seedClient)
	if err != nil {
		t.Fatal(err)
	}
	seedBudget := releaseHeadV2Budget{limit: admittedAllowance}
	seedCtx, seedOwner, err := newReleaseClientKeyAuthorityV2Reads(t.Context(), fixture.steerer.chain, fixture.steerer.cfg, fixture.measurement.artifact, fixture.steerer.hotkey.PublicKey(), &seedBudget)
	if err != nil {
		t.Fatal(err)
	}
	seed, captureErr := captureReleaseClientKeysV2(seedCtx, fixture.steerer.chain, fixture.steerer.contexts[seedBinding.NoID].ClientKeyHistory, fixture.steerer.cfg.StateDir, seedDomain, []protocol.ClientKeyObservationRequest{seedRequest}, 2*releaseClientKeyHistoryTestResponseBytes, fixture.steerer.cfg.EvidenceV2.Bounds.MaxHistoryBytes)
	if err := seedOwner.finish(captureErr); err != nil || len(seed) != 1 || seed[0].registration.ClientID != seedRequest.ClientID || seed[0].registration.Domain != seedDomain || !seed[0].registration.Present || seed[0].registration.PublicKey != ([32]byte{0x31}) || seed[0].contentHash == "" {
		t.Fatalf("earlier actual batch did not retain its original source: %v", err)
	}
	seedPath, err := releaseClientKeyCaptureV2Path(fixture.steerer.cfg.StateDir, seedDomain, seedRequest)
	if err != nil {
		t.Fatal(err)
	}
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil || ReleaseMeasurementContentHash(seedBytes) != seed[0].contentHash {
		t.Fatalf("earlier batch original readback differs: %v", err)
	}
	beforeRefusals := bodyRefusals.Load()
	options.MaxControlBytes = originalAllowance
	refused, err := fixture.gather(t.Context(), options)
	if err == nil || !reflect.DeepEqual(refused, releaseHeadResult{}) ||
		!strings.Contains(err.Error(), "client-key batch returned Http 500") || bodyRefusals.Load()-beforeRefusals != 1 {
		t.Fatalf("original collection allowance did not fail at actual bounded response ownership: %v", err)
	}
	fixture.assertNoEMACommit(t)

	originals := map[string][]byte{}
	missingKVs := map[string]bool{}
	missingByOperatorKVs := map[uint64]uint64{}
	active := uint64(0)
	for _, binding := range fixture.measurement.artifact.Bindings {
		if !binding.Active {
			continue
		}
		active++
		clientId, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		domain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, binding.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientId)
		if err != nil {
			t.Fatal(err)
		}
		path, err := releaseClientKeyCaptureV2Path(fixture.steerer.cfg.StateDir, domain, request)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			missingKVs[strconv.FormatUint(binding.NoID, 10)+"/"+binding.ClientID] = true
			missingByOperatorKVs[binding.NoID]++
			continue
		}
		if err != nil || len(encoded) == 0 || len(encoded) > releaseClientKeyHistoryTestResponseBytes {
			t.Fatalf("refused collection left a malformed retained response: bytes=%d error=%v", len(encoded), err)
		}
		originals[path] = encoded
	}
	if len(originals) == 0 || uint64(len(originals)) >= active {
		t.Fatalf("body refusal did not occur after a genuine retained prefix: retained=%d active=%d", len(originals), active)
	}
	if !bytes.Equal(originals[seedPath], seedBytes) {
		t.Fatal("bounded batch refusal changed the earlier original signature")
	}
	refusedKVs := func() map[string]bool {
		stateLock.Lock()
		defer stateLock.Unlock()
		return maps.Clone(refusedClientKVs)
	}()
	if len(refusedKVs) == 0 {
		t.Fatal("actual bounded refusal did not identify its fresh client census")
	}
	for client := range refusedKVs {
		if !missingKVs[client] {
			t.Fatalf("refused complete batch retained a partial fresh client: %s", client)
		}
	}
	beforePosts := posts.Load()
	beforeRequestsKVs := requestCensus()
	result, err := fixture.gather(t.Context(), fixture.options(t))
	if err != nil || !reflect.DeepEqual(result.Weights, fixture.measurement.want.SelectedHead) ||
		!reflect.DeepEqual(releaseClientKeyTestChainBindings(result.Bindings), fixture.measurement.artifact.Bindings) {
		t.Fatalf("properly admitted retry changed genuine replay or independent bindings: %v", err)
	}
	wantPosts := uint64(0)
	for _, count := range missingByOperatorKVs {
		wantPosts += (count + protocol.MaxClientKeyObservationBatchClients - 1) / protocol.MaxClientKeyObservationBatchClients
	}
	if actual := posts.Load() - beforePosts; actual != wantPosts {
		t.Fatalf("retry changed the exact missing-source batch count: posts=%d want=%d missing=%d", actual, wantPosts, len(missingKVs))
	}
	afterRequestsKVs := requestCensus()
	for client, count := range afterRequestsKVs {
		want := uint64(0)
		if missingKVs[client] {
			want = 1
		}
		if count-beforeRequestsKVs[client] != want {
			t.Fatalf("retry requested an original or repeated a missing client: %s", client)
		}
	}
	for client := range missingKVs {
		if afterRequestsKVs[client]-beforeRequestsKVs[client] != 1 {
			t.Fatalf("retry omitted a missing client: %s", client)
		}
	}
	var responseBytes uint64
	for _, binding := range result.Bindings {
		if !binding.Active {
			continue
		}
		clientId, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		domain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, binding.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientId)
		if err != nil {
			t.Fatal(err)
		}
		path, err := releaseClientKeyCaptureV2Path(fixture.steerer.cfg.StateDir, domain, request)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := os.ReadFile(path)
		if err != nil || len(encoded) == 0 || len(encoded) > releaseClientKeyHistoryTestResponseBytes ||
			binding.ClientKeyObservationHash != ReleaseMeasurementContentHash(encoded) {
			t.Fatalf("retry did not bind the actual retained response: bytes=%d error=%v", len(encoded), err)
		}
		if original, found := originals[path]; found && !bytes.Equal(original, encoded) {
			t.Fatal("retry replaced an original request nonce, wrapper or signature")
		}
		responseBytes += uint64(len(encoded))
	}
	if collectionBytes+responseBytes*8 <= originalAllowance {
		t.Fatalf("actual combined ownership no longer proves the old allowance insufficient: collection_bytes=%d response_bytes=%d allowance=%d", collectionBytes, responseBytes, originalAllowance)
	}
	t.Logf("actual retained response census: active=%d bytes=%d charged_bytes=%d collection_bytes=%d old_allowance=%d admitted_allowance=%d", active, responseBytes, responseBytes*8, collectionBytes, originalAllowance, admittedAllowance)
	fixture.assertNoEMACommit(t)
}

// Both declared-length and chunked transport keep the exact one-byte boundary.
// Successful bytes still undergo the real historical operator-root read.
func TestReleaseClientKeyHistoryHttpExactSignedBodyBoundary(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 1)
	binding := fixture.measurement.artifact.Bindings[0]
	clientId, err := connect.ParseId(binding.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	domain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, binding.NoID, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientId)
	if err != nil {
		t.Fatal(err)
	}
	request.Nonce = [32]byte{3}
	encoded := releaseClientKeyTestResponse(t, fixture.steerer.cfg.DeploymentID, domain, request, [][32]byte{{0x31}})
	var chunked atomic.Bool
	var posts atomic.Uint64
	endpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		defer incoming.Body.Close()
		posts.Add(1)
		var args struct {
			ClientId string `json:"client_id"`
			Request  []byte `json:"request"`
		}
		var actual protocol.ClientKeyObservationRequest
		if incoming.Method != http.MethodPost || incoming.URL.Path != "/sn/client-key/observation" ||
			incoming.Header.Get("Authorization") != "Bearer client-key-test" ||
			json.NewDecoder(incoming.Body).Decode(&args) != nil || args.ClientId != clientId.String() ||
			json.Unmarshal(args.Request, &actual) != nil || actual != request {
			http.Error(writer, "actual request differs from its signed response", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if chunked.Load() {
			writer.(http.Flusher).Flush()
		} else {
			writer.Header().Set("Content-Length", strconv.Itoa(len(encoded)))
		}
		_, _ = writer.Write(encoded)
	}))
	defer endpoint.Close()
	reader, err := NewHTTPClientKeyHistoryReader(endpoint.URL, func() string { return "client-key-test" })
	if err != nil {
		t.Fatal(err)
	}
	for _, streamed := range []bool{false, true} {
		chunked.Store(streamed)
		refused, err := reader.Read(t.Context(), request, uint64(len(encoded)-1))
		cause := "exceeds its byte allowance"
		if streamed {
			cause = "incomplete or excessive"
		}
		if err == nil || refused != nil || !strings.Contains(err.Error(), cause) {
			t.Fatalf("chunked=%v one-byte-short admission published bytes or lost its cause: %v", streamed, err)
		}
		accepted, err := reader.Read(t.Context(), request, uint64(len(encoded)))
		if err != nil || !bytes.Equal(accepted, encoded) {
			t.Fatalf("chunked=%v exact actual body allowance failed: %v", streamed, err)
		}
		registration, err := verifyReleaseClientKeyCaptureV2(t.Context(), fixture.steerer.chain, accepted, uint64(len(encoded)), domain, request, false)
		if err != nil || !registration.Present || registration.PublicKey != ([32]byte{0x31}) {
			t.Fatalf("chunked=%v exact response did not join real historical root authority: %v", streamed, err)
		}
	}
	if posts.Load() != 4 {
		t.Fatalf("actual transport boundary did not consume every request: %d", posts.Load())
	}
}

// Plural transport keeps the complete signed member census at the exact byte
// boundary for both declared-length and chunked bodies, with no partial result.
func TestReleaseClientKeyHistoryBatchHttpExactSignedBodyBoundary(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 1)
	noId := fixture.measurement.artifact.Inputs[0].NoID
	var domain protocol.ClientKeyHistoryDomain
	var requestTs []protocol.ClientKeyObservationRequest
	response := protocol.ClientKeyObservationBatchResponse{}
	for _, binding := range fixture.measurement.artifact.Bindings {
		if binding.NoID != noId || !binding.Active || len(requestTs) == 2 {
			continue
		}
		clientId, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		currentDomain, request, err := releaseClientKeyDecisionV2(fixture.steerer.cfg, noId, fixture.steerer.hotkey.PublicKey(), fixture.measurement.artifact, clientId)
		if err != nil || len(requestTs) != 0 && currentDomain != domain {
			t.Fatalf("plural body boundary changed its independent source domain: %v", err)
		}
		domain = currentDomain
		request.Nonce = [32]byte{byte(len(requestTs) + 1)}
		requestTs = append(requestTs, request)
		response.Responses = append(response.Responses, releaseClientKeyTestResponse(t, fixture.steerer.cfg.DeploymentID, domain, request, [][32]byte{{0x31}}))
	}
	if len(requestTs) != 2 {
		t.Fatal("real plural boundary fixture lacks two active signed members")
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var chunked atomic.Bool
	var posts atomic.Uint64
	endpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		defer incoming.Body.Close()
		posts.Add(1)
		body, err := io.ReadAll(io.LimitReader(incoming.Body, protocol.MaxClientKeyObservationBatchRequestBytes+1))
		if err != nil {
			http.Error(writer, "request read failed", http.StatusBadRequest)
			return
		}
		actual, err := protocol.DecodeClientKeyObservationBatchRequest(body)
		if err != nil || incoming.Method != http.MethodPost || incoming.URL.Path != "/sn/client-key/observations" || incoming.Header.Get("Authorization") != "Bearer client-key-test" || !reflect.DeepEqual(actual.Requests, requestTs) {
			http.Error(writer, "actual plural request differs from signed members", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if chunked.Load() {
			writer.(http.Flusher).Flush()
		} else {
			writer.Header().Set("Content-Length", strconv.Itoa(len(encoded)))
		}
		_, _ = writer.Write(encoded)
	}))
	defer endpoint.Close()
	reader, err := NewHTTPClientKeyHistoryReader(endpoint.URL, func() string { return "client-key-test" })
	if err != nil {
		t.Fatal(err)
	}
	for _, streamed := range []bool{false, true} {
		chunked.Store(streamed)
		refused, err := reader.ReadBatch(t.Context(), requestTs, uint64(len(encoded)-1))
		cause := "exceeds its wire allowance"
		if streamed {
			cause = "incomplete or excessive"
		}
		if err == nil || refused != nil || !strings.Contains(err.Error(), cause) {
			t.Fatalf("chunked=%v one-byte-short plural admission returned members or lost its cause: %v", streamed, err)
		}
		accepted, err := reader.ReadBatch(t.Context(), requestTs, uint64(len(encoded)))
		if err != nil || len(accepted) != len(requestTs) {
			t.Fatalf("chunked=%v exact plural body admission failed: %v", streamed, err)
		}
		for index, source := range accepted {
			if !bytes.Equal(source, response.Responses[index]) {
				t.Fatal("plural body boundary changed an original signed member")
			}
			registration, err := verifyReleaseClientKeyCaptureV2(t.Context(), fixture.steerer.chain, source, uint64(len(encoded)), domain, requestTs[index], false)
			if err != nil || !registration.Present || registration.PublicKey != ([32]byte{0x31}) {
				t.Fatalf("chunked=%v exact plural member did not join real historical authority: %v", streamed, err)
			}
		}
	}
	if posts.Load() != 4 {
		t.Fatalf("plural body refusal retried or omitted a real request: %d", posts.Load())
	}
}

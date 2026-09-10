//go:build linux || darwin

// Actual Http response custody and concrete ethclient source authentication
// retain every client across live capture, bounded fallback and recovery.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// Ordered unique requests retain one independently configured decision.
func releaseClientKeyHistoryBatchV2Requests(fixture *releaseClientKeyAuthorityV2TestFixture, count int) []protocol.ClientKeyObservationRequest {
	requests := make([]protocol.ClientKeyObservationRequest, count)
	for index := range requests {
		requests[index] = fixture.request
		requests[index].ClientID = [16]byte{}
		binary.BigEndian.PutUint64(requests[index].ClientID[8:], uint64(index+1))
	}
	return requests
}

// Four genuine operator/validator observations each retain404 private slots;
// recovery reopens all1616 originals without a live authenticated session.
func TestReleaseClientKeyHistoryBatchFullPopulationCaptureAndRecovery(t *testing.T) {
	t.Parallel()
	for validatorIndex := uint64(1); validatorIndex <= 2; validatorIndex++ {
		fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
		fixture.cfg.ValidatorID, fixture.artifact.ValidatorID = validatorIndex, validatorIndex
		fixture.cfg.StateDir = newReleaseHeadV2TestStateDir(t)
		fixture.cfg.Operators = []OperatorConfig{{NoID: 1}, {NoID: 2}}
		fixture.hotkey[0] = byte(0x70 + validatorIndex)
		fixture.request.ValidatorHotkey = fixture.hotkey
		requests := releaseClientKeyHistoryBatchV2Requests(fixture, 404)
		readers := map[uint64]*HTTPClientKeyHistoryReader{}
		original := map[uint64][]releaseClientKeyBatchV2Capture{}
		for pass := 0; pass < 2; pass++ {
			ctx, owner := fixture.owner(t, t.Context(), protocol.MaxClientKeyObservationBatchControlBytes)
			for _, noId := range []uint64{1, 2} {
				domain := fixture.domain
				domain.NoID = noId
				if pass == 0 {
					readers[noId] = newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, noId)
				} else {
					readers[noId].byJwt = func() string { return "" }
				}
				captured, err := captureReleaseClientKeysV2(ctx, fixture.chain, readers[noId], fixture.cfg.StateDir, domain, requests, protocol.MaxClientKeyObservationBatchResponseBytes, 1024*1024*1024)
				if err != nil || len(captured) != 404 {
					_ = owner.finish(err)
					t.Fatalf("validator=%d operator=%d pass=%d count=%d error=%v", validatorIndex, noId, pass, len(captured), err)
				}
				for index, value := range captured {
					if value.registration.ClientID != requests[index].ClientID || value.registration.Domain.NoID != noId || value.registration.PublicKey != ([32]byte{0x31}) || value.contentHash == "" {
						t.Fatal("full census lost independent source identity")
					}
					if pass != 0 && value != original[noId][index] {
						t.Fatal("restart replaced original signed capture")
					}
				}
				if pass == 0 {
					original[noId] = captured
				}
			}
			if err := owner.finish(nil); err != nil {
				t.Fatal(err)
			}
		}
		if calls := fixture.rpc.counts()["eth_call"]; calls != 2*2*12 {
			t.Fatalf("full census reused cross-decision authority or repeated per-client views: %d", calls)
		}
	}
}

// Only an explicit work refusal permits one further logical reservation per
// client; actual smaller requests carry each original nonce unchanged.
func TestReleaseClientKeyHistoryBatchAdmissionFallbackReservesEachClientTwice(t *testing.T) {
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	requests := releaseClientKeyHistoryBatchV2Requests(fixture, 404)
	var stateLock sync.Mutex
	requestCount := 0
	reservations := map[[16]byte]int{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		encoded, err := io.ReadAll(io.LimitReader(r.Body, protocol.MaxClientKeyObservationBatchRequestBytes+1))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		batch, err := protocol.DecodeClientKeyObservationBatchRequest(encoded)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		stateLock.Lock()
		requestCount++
		count := requestCount
		for _, request := range batch.Requests {
			reservations[request.ClientID]++
		}
		stateLock.Unlock()
		if count == 1 {
			if len(batch.Requests) != 404 {
				t.Error("first request was not the complete census")
			}
			w.Header().Set("X-Ur-Client-Key-Batch-Admission", "work")
			http.Error(w, "pre-read work refusal", http.StatusRequestEntityTooLarge)
			return
		}
		if len(batch.Requests) > protocol.ClientKeyObservationFallbackBatchClients {
			t.Error("fallback expanded its explicit operational bound")
		}
		response := protocol.ClientKeyObservationBatchResponse{Responses: make([]json.RawMessage, len(batch.Requests))}
		for index, request := range batch.Requests {
			response.Responses[index] = releaseClientKeyTestResponse(t, fixture.cfg.DeploymentID, fixture.domain, request, [][32]byte{{0x31}})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer endpoint.Close()
	reader, err := NewHTTPClientKeyHistoryReader(endpoint.URL, func() string { return "test-owned-session" })
	if err != nil {
		t.Fatal(err)
	}
	responses, err := reader.ReadBatch(t.Context(), requests, protocol.MaxClientKeyObservationBatchResponseBytes)
	if err != nil || len(responses) != len(requests) {
		t.Fatal("actual fallback failed", err)
	}
	stateLock.Lock()
	defer stateLock.Unlock()
	if requestCount != 1+(404+15)/16 || len(reservations) != 404 {
		t.Fatal("fallback request census differs")
	}
	for index, request := range requests {
		if reservations[request.ClientID] != protocol.MaxClientKeyObservationReservationAttempts {
			t.Fatal("logical client reservation count differs")
		}
		response, err := protocol.DecodeClientKeyHistoryResponse(responses[index], protocol.MaxClientKeyHistoryResponseBytes)
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := protocol.DecodeClientKeyEvidence(response.Observation, fixture.domain, protocol.ClientKeyObservationEvidenceKind)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := protocol.DecodeClientKeyObservation(envelope.Payload)
		if err != nil || observation.Request != request {
			t.Fatal("fallback replaced the client's original nonce or decision", err)
		}
	}
}

// Quota, transport, unsigned and repeated work refusals cannot create a third
// reservation attempt or a successful-looking response prefix.
func TestReleaseClientKeyHistoryBatchFailureDoesNotRetryBeyondAdmission(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusRequestEntityTooLarge} {
		fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
		requests := releaseClientKeyHistoryBatchV2Requests(fixture, 32)
		var stateLock sync.Mutex
		calls := 0
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stateLock.Lock()
			calls++
			stateLock.Unlock()
			if status == http.StatusRequestEntityTooLarge {
				w.Header().Set("X-Ur-Client-Key-Batch-Admission", "work")
			}
			http.Error(w, "actual refusal", status)
		}))
		reader, err := NewHTTPClientKeyHistoryReader(endpoint.URL, func() string { return "test-session" })
		if err != nil {
			t.Fatal(err)
		}
		response, err := reader.ReadBatch(t.Context(), requests, protocol.MaxClientKeyObservationBatchResponseBytes)
		endpoint.Close()
		stateLock.Lock()
		count := calls
		stateLock.Unlock()
		want := 1
		if status == http.StatusRequestEntityTooLarge {
			want = protocol.MaxClientKeyObservationReservationAttempts
		}
		if err == nil || response != nil || count != want {
			t.Fatalf("status=%d calls=%d/%d result=%d error=%v", status, count, want, len(response), err)
		}
	}
}

// Reading the bounded canonical body through EOF enables net/http's real
// disconnect reader before a handler waits for client cancellation.
func readReleaseClientKeyHistoryCancellationRequest(request *http.Request, expected []protocol.ClientKeyObservationRequest) error {
	encoded, readErr := io.ReadAll(io.LimitReader(request.Body, protocol.MaxClientKeyObservationBatchRequestBytes+1))
	if err := errors.Join(readErr, request.Body.Close()); err != nil {
		return err
	}
	batch, err := protocol.DecodeClientKeyObservationBatchRequest(encoded)
	if err != nil {
		return err
	}
	if request.Method != http.MethodPost || request.URL.Path != "/sn/client-key/observations" || request.Header.Get("Authorization") != "Bearer test-session" || request.Header.Get("Content-Type") != "application/json" || !slices.Equal(batch.Requests, expected) || batch.MaximumResponseBytes != protocol.MaxClientKeyObservationBatchResponseBytes {
		return errors.New("cancellation request lost its actual session, complete client census or original decision")
	}
	return nil
}

// The actual client outcome is separate from cancellation: joining an error
// with context.Canceled must not conceal a nonnil response prefix.
type releaseClientKeyHistoryBatchCancellationResult struct {
	responses [][]byte
	err       error
}

// A complete 404-client request is consumed before the cancellation barrier.
// An unread POST parks the server's disconnect reader, even after the actual
// client correctly returns context.Canceled; that was the original timeout.
func TestReleaseClientKeyHistoryBatchCancellationJoinsNoAuthority(t *testing.T) {
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	requests := releaseClientKeyHistoryBatchV2Requests(fixture, 404)
	entered, left := make(chan error, 1), make(chan error, 1)
	var calls atomic.Uint64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := readReleaseClientKeyHistoryCancellationRequest(r, requests); err != nil {
			entered <- err
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		entered <- nil
		<-r.Context().Done()
		left <- r.Context().Err()
	}))
	defer endpoint.Close()
	reader, err := NewHTTPClientKeyHistoryReader(endpoint.URL, func() string { return "test-session" })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan releaseClientKeyHistoryBatchCancellationResult, 1)
	go func() {
		result, err := reader.ReadBatch(ctx, requests, protocol.MaxClientKeyObservationBatchResponseBytes)
		done <- releaseClientKeyHistoryBatchCancellationResult{responses: result, err: err}
	}()
	select {
	case err := <-entered:
		if err != nil {
			t.Fatal("complete cancellation request was not admitted", err)
		}
	case result := <-done:
		t.Fatal("batch ended before actual request consumption", result.err)
	}
	cancel()
	result := <-done
	if result.responses != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatal("batch lost cancellation or retained a response prefix", result.err)
	}
	if err := <-left; !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatal("actual server request did not join once after client cancellation", err, calls.Load())
	}
}

// The singleton request has a different outer wire. Its complete body must
// also reach EOF before cancellation can join the real server request owner.
func TestReleaseClientKeyHistoryBatchAdjacentSingletonCancellationJoinsNoAuthority(t *testing.T) {
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	requestBytes, err := json.Marshal(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(struct {
		ClientId string `json:"client_id"`
		Request  []byte `json:"request"`
	}{ClientId: connect.Id(fixture.request.ClientID).String(), Request: requestBytes})
	if err != nil {
		t.Fatal(err)
	}
	entered, left := make(chan error, 1), make(chan error, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, readErr := io.ReadAll(io.LimitReader(r.Body, int64(len(expected))+1))
		err := errors.Join(readErr, r.Body.Close())
		if err == nil && (!bytes.Equal(encoded, expected) || r.Method != http.MethodPost || r.URL.Path != "/sn/client-key/observation" || r.Header.Get("Authorization") != "Bearer test-session") {
			err = errors.New("singleton cancellation request differs from its original wire/session")
		}
		entered <- err
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		<-r.Context().Done()
		left <- r.Context().Err()
	}))
	defer endpoint.Close()
	reader, err := NewHTTPClientKeyHistoryReader(endpoint.URL, func() string { return "test-session" })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		response []byte
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		response, err := reader.Read(ctx, fixture.request, protocol.MaxClientKeyHistoryResponseBytes)
		done <- outcome{response: response, err: err}
	}()
	select {
	case err := <-entered:
		if err != nil {
			t.Fatal(err)
		}
	case result := <-done:
		t.Fatal("singleton ended before complete request consumption", result.err)
	}
	cancel()
	result := <-done
	if result.response != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatal("singleton cancellation retained response authority", result.err)
	}
	if err := <-left; !errors.Is(err, context.Canceled) {
		t.Fatal("singleton actual server request was not joined", err)
	}
}

// This observer forwards all I/O and close errors unchanged. Its barrier
// proves cancellation occurs after the concrete body has yielded real bytes.
type releaseClientKeyHistoryBatchCancellationBody struct {
	io.ReadCloser
	readOnce   sync.Once
	closeOnce  sync.Once
	read       chan struct{}
	closed     chan struct{}
	closeCount *atomic.Uint64
}

func (self *releaseClientKeyHistoryBatchCancellationBody) Read(target []byte) (int, error) {
	n, err := self.ReadCloser.Read(target)
	if n > 0 {
		self.readOnce.Do(func() { close(self.read) })
	}
	return n, err
}

func (self *releaseClientKeyHistoryBatchCancellationBody) Close() error {
	err := self.ReadCloser.Close()
	self.closeCount.Add(1)
	self.closeOnce.Do(func() { close(self.closed) })
	return err
}

// Only an actual successful RoundTrip installs the read/close observation.
type releaseClientKeyHistoryBatchCancellationTransport struct {
	read       chan struct{}
	closed     chan struct{}
	closeCount *atomic.Uint64
}

func (self *releaseClientKeyHistoryBatchCancellationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err == nil {
		response.Body = &releaseClientKeyHistoryBatchCancellationBody{ReadCloser: response.Body, read: self.read, closed: self.closed, closeCount: self.closeCount}
	}
	return response, err
}

// A genuine signed first member is still not a complete 404-member response.
// Cancellation must close its actual body and refuse the entire prefix.
func TestReleaseClientKeyHistoryBatchPartialResponseCancellationJoinsNoAuthority(t *testing.T) {
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	requests := releaseClientKeyHistoryBatchV2Requests(fixture, 404)
	first := releaseClientKeyTestResponse(t, fixture.cfg.DeploymentID, fixture.domain, requests[0], [][32]byte{{0x31}})
	prefix := append(append([]byte(`{"responses":[`), first...), ',')
	left := make(chan error, 1)
	var calls atomic.Uint64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := readReleaseClientKeyHistoryCancellationRequest(r, requests); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(prefix); err != nil {
			left <- err
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			left <- err
			return
		}
		<-r.Context().Done()
		left <- r.Context().Err()
	}))
	defer endpoint.Close()
	reader, err := NewHTTPClientKeyHistoryReader(endpoint.URL, func() string { return "test-session" })
	if err != nil {
		t.Fatal(err)
	}
	read, closed := make(chan struct{}), make(chan struct{})
	var closeCount atomic.Uint64
	reader.client.Transport = &releaseClientKeyHistoryBatchCancellationTransport{read: read, closed: closed, closeCount: &closeCount}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan releaseClientKeyHistoryBatchCancellationResult, 1)
	go func() {
		responses, err := reader.ReadBatch(ctx, requests, protocol.MaxClientKeyObservationBatchResponseBytes)
		done <- releaseClientKeyHistoryBatchCancellationResult{responses: responses, err: err}
	}()
	select {
	case <-read:
	case result := <-done:
		t.Fatal("batch ended before concrete partial-body consumption", result.err)
	}
	cancel()
	result := <-done
	if result.responses != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatal("partial batch retained response authority or lost cancellation", result.err)
	}
	<-closed
	if closeCount.Load() != 1 {
		t.Fatal("actual partial response body was not closed exactly once", closeCount.Load())
	}
	if err := <-left; !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatal("partial response server was not joined without retry", err, calls.Load())
	}
}

// Deadline observation forwards every request through the actual transport.
type releaseClientKeyHistoryBatchDeadlineTransport struct {
	transport http.RoundTripper
	observe   func(*http.Request)
}

func (self *releaseClientKeyHistoryBatchDeadlineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	self.observe(request)
	return self.transport.RoundTrip(request)
}

// The concrete Http client, not just protocol arithmetic, carries the plural
// ceiling while preserving ordinary singleton and earlier-parent deadlines.
func TestReleaseClientKeyHistoryBatchRealHttpPreservesDeadlineOwnership(t *testing.T) {
	fixture := newReleaseClientKeyAuthorityV2TestFixture(t)
	reader := newReleaseHeadV2ClientKeyReader(t, &fixture.cfg, fixture.domain.NoID)
	type observation struct {
		path      string
		deadline  time.Time
		remaining time.Duration
	}
	observed := make(chan observation, 3)
	reader.client.Transport = &releaseClientKeyHistoryBatchDeadlineTransport{transport: http.DefaultTransport, observe: func(request *http.Request) {
		deadline, ok := request.Context().Deadline()
		if !ok {
			t.Error("actual outgoing request has no deadline")
		}
		observed <- observation{path: request.URL.Path, deadline: deadline, remaining: time.Until(deadline)}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests := releaseClientKeyHistoryBatchV2Requests(fixture, 1)
	if _, err := reader.ReadBatch(ctx, requests, protocol.MaxClientKeyObservationBatchResponseBytes); err != nil {
		t.Fatal(err)
	}
	plural := <-observed
	if _, err := reader.Read(ctx, requests[0], protocol.MaxClientKeyHistoryResponseBytes); err != nil {
		t.Fatal(err)
	}
	singleton := <-observed
	parentDeadline := time.Now().Add(time.Minute)
	parent, stop := context.WithDeadline(ctx, parentDeadline)
	defer stop()
	if _, err := reader.ReadBatch(parent, requests, protocol.MaxClientKeyObservationBatchResponseBytes); err != nil {
		t.Fatal(err)
	}
	earlier := <-observed
	if plural.path != "/sn/client-key/observations" || plural.remaining <= 30*time.Second || plural.remaining > time.Duration(protocol.ClientKeyObservationBatchOperationSeconds)*time.Second || singleton.path != "/sn/client-key/observation" || singleton.remaining <= 0 || singleton.remaining > 30*time.Second || !earlier.deadline.Equal(parentDeadline) || reader.client.Timeout != 30*time.Second {
		t.Fatalf("actual deadline ownership differs: plural=%+v singleton=%+v earlier=%+v original=%s", plural, singleton, earlier, reader.client.Timeout)
	}
}

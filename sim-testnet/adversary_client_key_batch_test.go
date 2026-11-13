// Logical actor slots retain the approved hostile-work unit across real Http.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// A fixed clock makes the reservation exact; nanosecond pacing avoids turning
// the proof into a wall-clock wait or timing assertion.
func adversaryClientKeyBatchTestOwner(t testing.TB, count int) (*adversaryHTTP, []byte, time.Time) {
	t.Helper()
	request := protocol.ClientKeyObservationBatchRequest{MaximumResponseBytes: protocol.MaxClientKeyObservationBatchResponseBytes}
	for index := 0; index < count; index++ {
		item := protocol.ClientKeyObservationRequest{ValidatorHotkey: [32]byte{1}, NativeBlock: 100, NativeHash: [32]byte{2}, NativeEpoch: 3, DecisionBoundary: protocol.ClientKeyEffectiveBoundary{Epoch: 4, Block: 99, Hash: [32]byte{5}}, Nonce: [32]byte{6}}
		binary.BigEndian.PutUint64(item.ClientID[8:], uint64(index+1))
		request.Requests = append(request.Requests, item)
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := newAdversaryRequestGate(8)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1_700_000_000, 0)
	gate.interval, gate.now = time.Nanosecond, func() time.Time { return at }
	return &adversaryHTTP{gate: gate, timeout: 30 * time.Second}, body, at
}

// Successful upload fixtures consume the whole body before acknowledging it.
// The real actor closes each connection, so unread bytes can abort its writer.
type adversaryClientKeyBatchTestHandler struct {
	requests *atomic.Uint64
}

// Count only complete uploads; an interrupted body cannot become success.
func (self adversaryClientKeyBatchTestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	self.requests.Add(1)
	w.WriteHeader(http.StatusOK)
}

// One transport containing404 clients spends404 admitted logical actor slots.
func TestClientKeyHistoryAdversaryBatchChargesEveryActualLogicalMember(t *testing.T) {
	owner, body, at := adversaryClientKeyBatchTestOwner(t, 404)
	var requests atomic.Uint64
	endpoint := httptest.NewServer(adversaryClientKeyBatchTestHandler{requests: &requests})
	defer endpoint.Close()
	status, _, err := owner.do(t.Context(), http.MethodPost, endpoint.URL+"/sn/client-key/observations", "", body, 1024)
	if err != nil || status != http.StatusOK || requests.Load() != 1 || !owner.gate.next.Equal(at.Add(404*time.Nanosecond)) {
		t.Fatalf("batch spent the wrong logical slots: requests=%d next=%s error=%v", requests.Load(), owner.gate.next, err)
	}
}

// A synchronized pair spends both complete censuses before either live send.
func TestClientKeyHistoryAdversaryBatchPairChargesBothCensuses(t *testing.T) {
	owner, body, at := adversaryClientKeyBatchTestOwner(t, 16)
	var requests atomic.Uint64
	endpoint := httptest.NewServer(adversaryClientKeyBatchTestHandler{requests: &requests})
	defer endpoint.Close()
	responses, err := owner.doConcurrentPair(t.Context(), http.MethodPost, endpoint.URL+"/sn/client-key/observations", "", body, 1024)
	if err != nil || requests.Load() != 2 || !owner.gate.next.Equal(at.Add(32*time.Nanosecond)) {
		t.Fatalf("pair lost member charging: requests=%d next=%s error=%v", requests.Load(), owner.gate.next, err)
	}
	for _, response := range responses {
		if response.Status != http.StatusOK || response.Err != nil {
			t.Fatal("actual pair failed", response.Err)
		}
	}
}

// Invalid/excessive or cancelled work cannot reach the real operator endpoint.
func TestClientKeyHistoryAdversaryBatchRefusesInvalidAndCancelledIo(t *testing.T) {
	owner, body, at := adversaryClientKeyBatchTestOwner(t, 16)
	var requests atomic.Uint64
	endpoint := httptest.NewServer(adversaryClientKeyBatchTestHandler{requests: &requests})
	defer endpoint.Close()
	if _, _, err := owner.do(t.Context(), http.MethodPost, endpoint.URL+"/sn/client-key/observations", "", append(body, '\n'), 1024); err == nil || requests.Load() != 0 || !owner.gate.next.IsZero() {
		t.Fatal("noncanonical batch escaped local admission")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := owner.do(ctx, http.MethodPost, endpoint.URL+"/sn/client-key/observations", "", body, 1024); err == nil || requests.Load() != 0 || !owner.gate.next.Equal(at.Add(16*time.Nanosecond)) {
		t.Fatal("cancelled batch escaped or refunded its logical admission")
	}
}

// Observe the upload cursor at the response boundary without TCP scheduling.
type adversaryClientKeyBatchTestResponseWriter struct {
	*httptest.ResponseRecorder
	beforeHeader func(int)
}

// Record request consumption before any status reaches the client.
func (self *adversaryClientKeyBatchTestResponseWriter) WriteHeader(status int) {
	self.beforeHeader(status)
	self.ResponseRecorder.WriteHeader(status)
}

// Both small and full logical batches must finish uploading before the
// endpoint replies; the full 404-member transport regression remains above.
func TestClientKeyHistoryAdversaryBatchFixtureDrainsBeforeResponse(t *testing.T) {
	for _, count := range []int{404, 16, protocol.MaxClientKeyObservationBatchClients} {
		_, body, _ := adversaryClientKeyBatchTestOwner(t, count)
		unread := bytes.NewReader(body)
		request := httptest.NewRequest(http.MethodPost, "http://operator.example/sn/client-key/observations", unread)
		request.Close = true
		var requests atomic.Uint64
		response := &adversaryClientKeyBatchTestResponseWriter{
			ResponseRecorder: httptest.NewRecorder(),
			beforeHeader: func(status int) {
				if status != http.StatusOK || unread.Len() != 0 || requests.Load() != 1 {
					t.Errorf("%d-member batch replied before complete upload: status=%d unread=%d requests=%d", count, status, unread.Len(), requests.Load())
				}
			},
		}
		adversaryClientKeyBatchTestHandler{requests: &requests}.ServeHTTP(response, request)
		if response.Code != http.StatusOK || unread.Len() != 0 || requests.Load() != 1 {
			t.Fatalf("%d-member batch did not complete its upload: status=%d unread=%d requests=%d", count, response.Code, unread.Len(), requests.Load())
		}
	}
}

// An explicit terminal read error must neither acknowledge nor count the
// incomplete upload, including a complete 404-member body followed by failure.
func TestClientKeyHistoryAdversaryBatchFixtureRejectsIncompleteUpload(t *testing.T) {
	_, body, _ := adversaryClientKeyBatchTestOwner(t, 404)
	request := httptest.NewRequest(http.MethodPost, "http://operator.example/sn/client-key/observations", nil)
	request.Close = true
	request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), iotest.ErrReader(io.ErrUnexpectedEOF)))
	var requests atomic.Uint64
	response := httptest.NewRecorder()
	adversaryClientKeyBatchTestHandler{requests: &requests}.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || requests.Load() != 0 {
		t.Fatalf("incomplete batch upload was acknowledged: status=%d requests=%d", response.Code, requests.Load())
	}
}

// The real provider admission state machine uses40 Http slots/minute, not40
// logical methods. Exact slot times expose the separate30-second owner risk
// without sleeping or inventing a deadline on the enclosing native decision.
func TestClientKeyHistoryRpcBatchContenderDeadlineUsesActualHttpSlots(t *testing.T) {
	if protocol.ClientKeyObservationBatchOperationSeconds != 180 || protocol.ClientKeyObservationBatchLocalWorkSeconds != 36 || protocol.ClientKeyObservationBatchDeadlineContenders != 12 || protocol.ClientKeyObservationBatchDeadlineRequestsPerMinute != 40 {
		t.Fatal("finite plural-only planning envelope changed")
	}
	domain := protocol.ClientKeyHistoryDomain{ChainID: 945, GenesisHash: [32]byte{1}, Netuid: 521, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 1}
	limits := stabi.ClientKeyAuthorityRpcLimits{MaximumRequests: protocol.MaxClientKeyObservationBatchRpcRequests, MaximumMethods: protocol.MaxClientKeyObservationBatchRpcMethods, MaximumBytes: protocol.MaxClientKeyObservationBatchControlBytes}
	for _, boundaries := range []int{1, 17} {
		queries := make([]stabi.ClientKeyAuthorityQuery, boundaries)
		for index := range queries {
			hash := [32]byte{1}
			binary.BigEndian.PutUint64(hash[24:], uint64(index+1))
			queries[index] = stabi.ClientKeyAuthorityQuery{Domain: domain, Boundary: protocol.ClientKeyEffectiveBoundary{Epoch: 1, Block: uint64(index + 1), Hash: hash}}
		}
		work, err := stabi.PlanClientKeyAuthorityRpc(queries, limits)
		if err != nil {
			t.Fatal(err)
		}
		for _, contenders := range []uint64{4, protocol.ClientKeyObservationBatchDeadlineContenders, protocol.ClientKeyObservationBatchDeadlineContenders + 1} {
			gate, err := newRPCRequestGate(protocol.ClientKeyObservationBatchDeadlineRequestsPerMinute)
			if err != nil || gate.interval != 1500*time.Millisecond {
				t.Fatal("approved actual Http pacing changed", err)
			}
			at := time.Unix(1_700_000_000, 0)
			last := at
			// The actual Fifo admission state carries the chosen finite census.
			// External owners are not assumed to obey this planning envelope.
			for turn := uint64(0); turn < work.Requests; turn++ {
				waiters := make([]*rpcRequestWaiter, contenders)
				for index := range waiters {
					waiters[index] = gate.enqueue()
				}
				for _, waiter := range waiters {
					front, delay, _ := gate.waiterState(waiter, last)
					if !front {
						t.Fatal("actual provider queue lost its front")
					}
					last = last.Add(delay)
					if !gate.admit(waiter, last) {
						t.Fatal("due actual Http slot was not admitted")
					}
				}
			}
			want := time.Duration(contenders*work.Requests-1) * 1500 * time.Millisecond
			if last.Sub(at) != want {
				t.Fatalf("wrong transport unit: got=%s want=%s methods=%d", last.Sub(at), want, work.Methods)
			}
			if contenders == 4 && (boundaries == 1 && want != 22500*time.Millisecond || boundaries == 17 && want != 46500*time.Millisecond) {
				t.Fatal("original30s owner contention calculation changed")
			}
			if boundaries == 17 {
				required := want + time.Duration(protocol.ClientKeyObservationBatchLocalWorkSeconds)*time.Second
				limit := time.Duration(protocol.ClientKeyObservationBatchOperationSeconds) * time.Second
				if contenders == protocol.ClientKeyObservationBatchDeadlineContenders && required > limit || contenders > protocol.ClientKeyObservationBatchDeadlineContenders && required <= limit {
					t.Fatal("bounded planning envelope silently admits additional contenders")
				}
			}
		}
	}
}

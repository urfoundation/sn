package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

func monitorClaimTestValue(now time.Time) protocol.ClaimProgress {
	pool := protocol.ClaimProgressPool{ChainId: 964, Vault: "0x" + strings.Repeat("1", 40), NoId: "7", Coldkey: "0x" + strings.Repeat("2", 64)}
	observation := &protocol.ClaimObservation{Schema: protocol.ClaimObservationSchema, EvidenceKind: "signed-receipt", Authority: "configured-rpc-assertion", GenesisStatus: "unverified", Epoch: 7, ObservedAt: now.Format(time.RFC3339Nano), Pool: pool, ShareBps: 5000, ProofStatus: "contract-accepted", BlockNumber: 100, BlockHash: "0x" + strings.Repeat("3", 64), TransactionHash: "0x" + strings.Repeat("4", 64), Relayer: "0x" + strings.Repeat("5", 40), AcceptedAmountRao: "25", PaymentStatus: "deferred", UnpaidCreditRao: "125"}
	return protocol.ClaimProgress{Schema: protocol.ClaimProgressSchema, Member: "synthetic-claim", Status: "active", InstanceId: strings.Repeat("6", 32), StartedAt: now.Add(-time.Hour).Format(time.RFC3339Nano), PublishedAt: now.Format(time.RFC3339Nano), Sequence: 1, QueueSha256: strings.Repeat("7", 64), DeclaredPool: &pool, TotalEntries: 1, FinalizedEntries: 1, Entries: []protocol.ClaimProgressEntry{{Epoch: 7, QueueStatus: "finalized", ObservationStatus: "retained", DomainStatus: "match", Observation: observation}}}
}

// This raw independently supplied policy also compiles against the prior
// production parser, so absence of public claim monitoring is a causal control.
func monitorClaimTestPolicyWire(value protocol.ClaimProgress, endpoint string, now time.Time) map[string]any {
	return map[string]any{"role": "claim-a", "endpoint": endpoint, "expected_member": value.Member, "expected_pool": value.DeclaredPool, "freshness_seconds": 60, "epochs": []map[string]any{{"epoch": int64(7), "share_bps": uint64(5000), "accept_by": now.Add(-time.Minute).Format(time.RFC3339Nano)}}}
}

type monitorClaimTestEvent struct {
	Schema            string                   `json:"schema"`
	Role              string                   `json:"role"`
	Status            string                   `json:"status"`
	Current           bool                     `json:"current"`
	CheckpointCurrent bool                     `json:"checkpoint_current"`
	Window            monitorClaimWindowStatus `json:"window"`
	State             struct {
		AcceptedReceipts int       `json:"accepted_receipts"`
		ClaimedLeaves    int       `json:"claimed_leaves"`
		MerkleProofs     int       `json:"merkle_proofs"`
		Deferred         int       `json:"deferred"`
		Overdue          int       `json:"overdue"`
		Sequence         uint64    `json:"sequence"`
		ProgressAt       time.Time `json:"semantic_progress_at"`
	} `json:"state"`
}

type monitorClaimTestSink struct {
	events chan monitorClaimTestEvent
	peers  chan monitorServiceEvent
}

func (self *monitorClaimTestSink) Write(raw []byte) (int, error) {
	return self.WriteContext(context.Background(), raw)
}

func (self *monitorClaimTestSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	var event monitorClaimTestEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0, err
	}
	if event.Schema == "urnetwork-mainnet-claim-event-v1" {
		select {
		case self.events <- event:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	} else if event.Schema == "urnetwork-mainnet-validator-event-v1" && self.peers != nil {
		var peer monitorServiceEvent
		if err := json.Unmarshal(raw, &peer); err != nil {
			return 0, err
		}
		select {
		case self.peers <- peer:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return len(raw), nil
}

func TestMonitorClaimPublicExpectedPoolObservesAcceptedNotPaid(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorClaimTestValue(fixture.clock.now())
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	var requests, wrongRequest atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/claim-progress" || request.URL.Query().Get("id") != value.Member {
			wrongRequest.Add(1)
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	defer server.Close()
	policy := map[string]any{"schema": monitorServicesSchema, "validators": []any{}, "claims": []any{monitorClaimTestPolicyWire(value, server.URL+"/claim-progress", fixture.clock.now())}}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.policyPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	checkpoint := strings.TrimSuffix(fixture.checkpointPath, ".json") + ".claim-claim-a.json"
	provisionMonitorTestCustody(t, checkpoint)
	url, _, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	sink := &monitorClaimTestSink{events: make(chan monitorClaimTestEvent, 4)}
	var stderr bytes.Buffer
	var exit int
	done := make(chan struct{})
	hooks := monitorServiceHooks{wait: func(ctx context.Context, _ string, _ time.Duration) bool { <-ctx.Done(); return false }}
	t.Cleanup(cancel)
	args := fixture.args(url)
	ctx = monitorTestStorageContext(t, ctx, args)
	go func() {
		defer close(done)
		exit = runMainWithMonitorHooks(ctx, args, sink, &stderr, fixture.clock.now, hooks)
	}()
	t.Cleanup(func() { joinMonitorTestWorker(t, cancel, done) })
	select {
	case event := <-sink.events:
		if !event.Current || !event.CheckpointCurrent || event.Status != "ok" || event.State.AcceptedReceipts != 1 || event.State.Deferred != 1 || event.State.Overdue != 0 {
			t.Fatal("independent claim expectation confused accepted credit with payment", event)
		}
	case <-done:
		t.Fatal("public monitor refused independently configured claim expectations", exit, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("public claim monitor did not publish its completed sample")
	}
	cancel()
	<-done
	if exit != 0 || requests.Load() != 1 || wrongRequest.Load() != 0 {
		t.Fatal("public claim read or joined shutdown differed", exit, requests.Load(), wrongRequest.Load(), stderr.String())
	}
	metrics, err := os.ReadFile(strings.TrimSuffix(fixture.metricsPath, ".prom") + ".claim-claim-a.prom")
	if err != nil || !bytes.Contains(metrics, []byte("sn_mainnet_claim_independent_finality_verified{role=\"claim-a\"} 0")) || !bytes.Contains(metrics, []byte("sn_mainnet_claim_per_epoch_payment_known{role=\"claim-a\"} 0")) {
		t.Fatal("configured RPC assertion became independently verified payment", string(metrics), err)
	}
}

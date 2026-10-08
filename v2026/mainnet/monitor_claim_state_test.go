package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

func monitorClaimTestPolicy(t *testing.T, value protocol.ClaimProgress, endpoint string, now time.Time) monitorClaimPolicy {
	t.Helper()
	raw, err := json.Marshal(monitorClaimTestPolicyWire(value, endpoint, now))
	if err != nil {
		t.Fatal(err)
	}
	var policy monitorClaimPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	return policy
}

func monitorClaimTestPending(value protocol.ClaimProgress) protocol.ClaimProgress {
	value = *cloneMonitorClaimProgress(&value)
	value.FinalizedEntries, value.UnresolvedEntries = 0, 1
	epoch := value.Entries[0].Epoch
	value.OldestUnresolvedEpoch = &epoch
	claimed := false
	observation := value.Entries[0].Observation
	observation.EvidenceKind, observation.ProofStatus, observation.PaymentStatus = "finalized-leaf", "merkle-verified", "unknown"
	observation.LeafClaimed, observation.PayoutRoot = &claimed, "0x"+strings.Repeat("8", 64)
	observation.AcceptedAmountRao, observation.UnpaidCreditRao, observation.AggregatePaidRao = "", "", ""
	observation.TransactionHash, observation.Relayer = "", ""
	value.Entries[0].QueueStatus = "pending"
	return value
}

func TestMonitorClaimHeartbeatCannotMoveAcceptanceDeadline(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := monitorClaimTestPending(monitorClaimTestValue(now))
	policy := monitorClaimTestPolicy(t, value, "https://monitor.example/claim-progress", now)
	policy.Epochs[0].AcceptBy = now.Add(time.Minute).Format(time.RFC3339Nano)
	state := newMonitorClaimState(policy)
	state.observe(policy, &value, "ok", now)
	firstProgress := state.SemanticProgressAt
	if !state.current || state.Status != "ok" || firstProgress != now {
		t.Fatal(state)
	}
	value.Sequence++
	value.QueueSha256 = strings.Repeat("9", 64)
	value.PublishedAt = now.Add(2 * time.Minute).Format(time.RFC3339Nano)
	value.Entries[0].QueueStatus = "retry"
	value.Entries[0].Observation.ObservedAt = value.PublishedAt
	value.Entries[0].Observation.BlockNumber++
	value.Entries[0].Observation.BlockHash = "0x" + strings.Repeat("a", 64)
	state.observe(policy, &value, "ok", now.Add(2*time.Minute))
	if !state.current || state.Status != "overdue" || state.summary(policy, state.SampleAt).Overdue != 1 || state.SemanticProgressAt != firstProgress {
		t.Fatal("fresh acknowledgement or later finalized block hid stalled acceptance", state)
	}
}

func TestMonitorClaimWeakerReportingCannotResetProofProgress(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := monitorClaimTestPending(monitorClaimTestValue(now))
	policy := monitorClaimTestPolicy(t, value, "https://monitor.example/claim-progress", now)
	state := newMonitorClaimState(policy)
	state.observe(policy, &value, "ok", now)
	value.Sequence++
	value.PublishedAt = now.Add(time.Second).Format(time.RFC3339Nano)
	value.Entries[0].Observation = &protocol.ClaimObservation{Schema: protocol.ClaimObservationSchema, EvidenceKind: "api-no-claim", Authority: "api-assertion", GenesisStatus: "unverified", Epoch: 7, ObservedAt: value.PublishedAt, ProofStatus: "unknown", PaymentStatus: "unknown"}
	value.Entries[0].DomainStatus = "unknown"
	state.observe(policy, &value, "ok", now.Add(time.Second))
	if state.SemanticProgressAt != now || state.summary(policy, state.SampleAt).MerkleProofs != 1 {
		t.Fatal("weaker API reporting reset actual retained proof progress", state.SemanticProgressAt, state.summary(policy, state.SampleAt))
	}
	value.Sequence++
	value.PublishedAt = now.Add(2 * time.Second).Format(time.RFC3339Nano)
	value.Entries[0].Observation.ObservedAt = value.PublishedAt
	state.observe(policy, &value, "ok", now.Add(2*time.Second))
	if state.SemanticProgressAt != now {
		t.Fatal("repeated API absence became settlement progress")
	}
}

func TestMonitorClaimReceiptRetainsEarlierMerkleWitness(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := monitorClaimTestValue(now)
	policy := monitorClaimTestPolicy(t, value, "https://monitor.example/claim-progress", now)
	state := newMonitorClaimState(policy)
	proof := monitorClaimTestPending(value)
	state.observe(policy, &proof, "ok", now)
	value.Sequence = 2
	value.PublishedAt = now.Add(time.Second).Format(time.RFC3339Nano)
	state.observe(policy, &value, "ok", now.Add(time.Second))
	summary := state.summary(policy, state.SampleAt)
	if summary.MerkleProofs != 1 || summary.AcceptedReceipts != 1 || summary.Deferred != 1 || summary.Overdue != 0 {
		t.Fatal("receipt erased earlier proof or invented transferred payment", summary)
	}
	value.Sequence++
	value.PublishedAt = now.Add(2 * time.Second).Format(time.RFC3339Nano)
	value.Entries = nil
	value.OmittedEntries = 1
	state.observe(policy, &value, "ok", now.Add(2*time.Second))
	if !state.current || state.summary(policy, state.SampleAt) != summary || state.Record.OmittedEntries != 1 {
		t.Fatal("bounded producer omission erased retained assertions", state)
	}
	if err := validateMonitorClaimState(policy, *state); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorClaimSequenceAndRestartCannotRefreshCachedSuccess(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := monitorClaimTestValue(now)
	policy := monitorClaimTestPolicy(t, value, "https://monitor.example/claim-progress", now)
	state := newMonitorClaimState(policy)
	state.observe(policy, &value, "ok", now)
	state.observe(policy, &value, "ok", now.Add(time.Second))
	if state.current || state.Status != "stale" || state.LastAcceptedAt != now {
		t.Fatal("cached success refreshed claim observer", state)
	}
	value.InstanceId = strings.Repeat("a", 32)
	state.observe(policy, &value, "ok", now.Add(2*time.Second))
	if state.current || state.Status != "stale" {
		t.Fatal("old producer generation was admitted", state)
	}
	value.StartedAt, value.PublishedAt = now.Add(time.Second).Format(time.RFC3339Nano), now.Add(3*time.Second).Format(time.RFC3339Nano)
	state.observe(policy, &value, "ok", now.Add(3*time.Second))
	if !state.current || state.Restarts != 1 || state.SemanticProgressAt != now {
		t.Fatal("real restart reset evidence or invented progress", state)
	}
	value.StartedAt = now.Add(2 * time.Second).Format(time.RFC3339Nano)
	state.observe(policy, &value, "ok", now.Add(4*time.Second))
	if state.current || state.Status != "identity" {
		t.Fatal("one instance changed its birth identity", state)
	}
}

func TestMonitorClaimUnavailableAndClockKeepAcceptedHistory(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := monitorClaimTestValue(now)
	policy := monitorClaimTestPolicy(t, value, "https://monitor.example/claim-progress", now)
	state := newMonitorClaimState(policy)
	state.observe(policy, &value, "ok", now)
	for _, status := range []string{"unavailable", "unknown"} {
		state.observe(policy, nil, status, now.Add(time.Second))
		if state.current || state.Record.Sequence != 1 || state.LastAcceptedAt != now || state.summary(policy, state.SampleAt).AcceptedReceipts != 1 {
			t.Fatal(status, state)
		}
	}
	value.Sequence++
	state.observe(policy, &value, "ok", now.Add(-time.Second))
	if state.Status != "clock" || state.current || state.Record.Sequence != 1 {
		t.Fatal("clock rollback admitted a new view", state)
	}
	state.observe(policy, &value, "identity", now.Add(-time.Second))
	if state.Status != "identity" || !monitorClaimTerminal(state.Status) {
		t.Fatal("clock obscured complete identity refusal")
	}
}

func TestMonitorClaimContradictionCannotRewriteRetainedReceipt(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := monitorClaimTestValue(now)
	policy := monitorClaimTestPolicy(t, value, "https://monitor.example/claim-progress", now)
	for _, mutate := range []func(*protocol.ClaimObservation){
		func(o *protocol.ClaimObservation) { o.TransactionHash = "0x" + strings.Repeat("a", 64) },
		func(o *protocol.ClaimObservation) { o.BlockHash = "0x" + strings.Repeat("a", 64) },
		func(o *protocol.ClaimObservation) { o.AcceptedAmountRao = "26" },
		func(o *protocol.ClaimObservation) { o.UnpaidCreditRao = "126" },
	} {
		state := newMonitorClaimState(policy)
		state.observe(policy, &value, "ok", now)
		candidate := cloneMonitorClaimProgress(&value)
		candidate.Sequence++
		mutate(candidate.Entries[0].Observation)
		if err := candidate.Validate(); err != nil {
			t.Fatal("invalid causal fixture", err)
		}
		state.observe(policy, candidate, "ok", now.Add(time.Second))
		if state.Status != "contradiction" || state.current || state.Record.Sequence != 1 || state.Epochs[0].Observation.AcceptedAmountRao != "25" {
			t.Fatal("contradiction replaced original receipt", state)
		}
	}
}

func TestMonitorClaimAggregatePaymentRetainsExactAmountWithoutAllocation(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := monitorClaimTestValue(now)
	observation := value.Entries[0].Observation
	observation.PaymentStatus, observation.UnpaidCreditRao, observation.AggregatePaidRao = "aggregate-paid", "", "1901"
	policy := monitorClaimTestPolicy(t, value, "https://monitor.example/claim-progress", now)
	state := newMonitorClaimState(policy)
	state.observe(policy, &value, "ok", now)
	if state.summary(policy, now).AggregatePaid != 1 || state.Epochs[0].Observation.AggregatePaidRao != "1901" || state.Epochs[0].Observation.AcceptedAmountRao != "25" {
		t.Fatal("aggregate payment was allocated to one epoch", state)
	}
	raw := renderMonitorClaimMetrics(policy, state, true)
	if bytes.Contains(raw, []byte("1901")) || !bytes.Contains(raw, []byte("per_epoch_payment_known{role=\"claim-a\"} 0")) || !bytes.Contains(raw, []byte("independent_finality_verified{role=\"claim-a\"} 0")) {
		t.Fatal("asserted amounts became lossy or allocated metrics", string(raw))
	}
	value.Sequence++
	value.Entries[0].Observation.PaymentStatus, value.Entries[0].Observation.AggregatePaidRao = "invalid", ""
	state.observe(policy, &value, "ok", now.Add(time.Second))
	if state.summary(policy, state.SampleAt).InvalidPayment != 1 || state.Epochs[0].Observation.AggregatePaidRao != "1901" || state.SemanticProgressAt != now {
		t.Fatal("reporting degradation erased evidence or refreshed settlement", state)
	}
}

func TestMonitorClaimCheckpointReopenFencesLostOrRetargetedHistory(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorClaimTestValue(fixture.clock.now())
	policy := monitorClaimTestPolicy(t, value, "https://monitor.example/claim-progress", fixture.clock.now())
	fixture.policy.Claims = []monitorClaimPolicy{policy}
	fixture.writePolicy(t)
	ctx := monitorTestStorageContext(t, t.Context(), fixture.args("https://rpc.example"))
	worker, err := openMonitorClaimWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	worker.state.observe(policy, &value, "ok", fixture.clock.now())
	if err := worker.save(); err != nil {
		t.Fatal(err)
	}
	path := worker.checkpoint.path
	if err := worker.close(monitorServiceHooks{}); err != nil {
		t.Fatal(err)
	}
	changed := policy
	changed.Epochs = append([]monitorClaimEpochPolicy(nil), policy.Epochs...)
	changed.Epochs[0].AcceptBy = fixture.clock.now().Add(time.Hour).Format(time.RFC3339Nano)
	if owner, err := openMonitorClaimWorker(ctx, changed, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{}); err == nil {
		owner.close(monitorServiceHooks{})
		t.Fatal("restart changed original deadline")
	}
	next, err := openMonitorClaimWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if next.state.current || next.state.Record.Sequence != 1 {
		t.Fatal("reopen invented freshness")
	}
	next.state.observe(policy, &value, "ok", fixture.clock.now())
	if next.state.current || next.state.Status != "stale" {
		t.Fatal("reopen accepted cached source")
	}
	if err := next.close(monitorServiceHooks{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if owner, err := openMonitorClaimWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{}); !errors.Is(err, durablevolume.ErrIdentity) {
		if owner != nil {
			owner.close(monitorServiceHooks{})
		}
		t.Fatal("lost checkpoint became fresh observer", err)
	}
}

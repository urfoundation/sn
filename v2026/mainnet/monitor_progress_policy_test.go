// Real checkpoint owners and the public command retain prior observations
// across independently configured revisions. No producer chooses expectations.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Exactly one observed role sample completes; the real chain peer has entered
// its read before cancellation, so admission timing cannot prove this result.
func monitorProgressPolicyPublicSample(t *testing.T, fixture *monitorServicesFixture, role string, hooks monitorServiceHooks) {
	t.Helper()
	url, chainEntered, _ := monitorServicesBlockedChain(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hooks.afterEvent = func(ctx context.Context, got string) {
		if got != role {
			return
		}
		select {
		case <-chainEntered:
		case <-ctx.Done():
		}
		cancel()
	}
	hooks.wait = func(ctx context.Context, _ string, _ time.Duration) bool { <-ctx.Done(); return false }
	sink := &monitorProgressRetrySink{events: make(chan monitorProgressRetryEvent, 4)}
	var diagnostic bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- runMonitorStorageTestWithHooks(t, ctx, fixture.args(url), sink, &diagnostic, fixture.clock.now, hooks)
	}()
	var exit int
	select {
	case exit = <-done:
	case <-time.After(15 * time.Second):
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("public policy worker did not join cancellation")
		}
		t.Fatal("public policy sample did not reach its completed event")
	}
	if exit != 0 {
		t.Fatal("public renewed role did not join", exit, diagnostic.String())
	}
	select {
	case event := <-sink.events:
		if event.Role != role || !event.CheckpointCurrent {
			t.Fatal("renewed role did not publish its retained checkpoint", event)
		}
	default:
		t.Fatal("public renewed role produced no complete sample", diagnostic.String())
	}
}

func monitorClaimPolicyCheckpoint(t *testing.T, fixture *monitorServicesFixture, role string) (monitorClaimCheckpointRecord, []byte) {
	t.Helper()
	path, _ := monitorClaimPaths(fixture.checkpointPath, fixture.metricsPath, role)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record monitorClaimCheckpointRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return record, raw
}

func TestMonitorClaimPublicRenewalRetainsOriginalLiability(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorClaimTestPending(monitorClaimTestValue(fixture.clock.now()))
	var calls atomic.Uint64
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		candidate := cloneMonitorClaimProgress(&value)
		candidate.Sequence = calls.Add(1)
		_ = json.NewEncoder(w).Encode(candidate)
	}))
	defer source.Close()
	policy := monitorClaimTestPolicy(t, value, source.URL+"/claim-progress", fixture.clock.now())
	fixture.policy.Claims = []monitorClaimPolicy{policy}
	fixture.writePolicy(t)
	monitorProgressPolicyPublicSample(t, fixture, policy.Role, monitorServiceHooks{})
	prior, _ := monitorClaimPolicyCheckpoint(t, fixture, policy.Role)
	if prior.PolicyHistory == nil || len(prior.PolicyHistory.Entries) != 1 || len(prior.State.Epochs) != 1 || prior.State.Epochs[0].Proof == nil {
		t.Fatal("initial public claim history is absent", prior)
	}
	policy.Renewal = &monitorProgressPolicyRenewal{Original: policy.resources(), PreviousSha256: prior.PolicyHistory.Entries[0].ContentHash, ReviewSha256: "sha256:" + strings.Repeat("a", 64)}
	policy.Epochs = append(policy.Epochs, monitorClaimEpochPolicy{Epoch: 8, ShareBps: 7000, AcceptBy: fixture.clock.now().Add(time.Hour).Format(time.RFC3339Nano)})
	policy.ReadBudgetSeconds = 600
	fixture.policy.Claims[0] = policy
	fixture.writePolicy(t)
	monitorProgressPolicyPublicSample(t, fixture, policy.Role, monitorServiceHooks{})
	current, _ := monitorClaimPolicyCheckpoint(t, fixture, policy.Role)
	if current.PolicyHistory == nil || len(current.PolicyHistory.Entries) != 2 || current.PolicyHash != prior.PolicyHash || current.State.SemanticProgressAt != prior.State.SemanticProgressAt || len(current.State.Epochs) != 2 || current.State.Epochs[1].Observation != nil || rootObjectHash(current.State.Epochs[0]) != rootObjectHash(prior.State.Epochs[0]) {
		t.Fatal("renewal erased prior liability or invented later proof", current)
	}
	if summary := current.State.summary(policy, fixture.clock.now()); summary.Expected != 2 || summary.Unknown != 1 || summary.Overdue != 1 || summary.MerkleProofs != 1 {
		t.Fatal("new independent expectation hid original overdue work", summary)
	}
	monitorProgressPolicyPublicSample(t, fixture, policy.Role, monitorServiceHooks{})
	reopened, _ := monitorClaimPolicyCheckpoint(t, fixture, policy.Role)
	if reopened.PolicyHistory == nil || len(reopened.PolicyHistory.Entries) != 2 || reopened.PolicyHistory.Entries[1] != current.PolicyHistory.Entries[1] || calls.Load() != 3 {
		t.Fatal("same reviewed policy did not reopen original history", reopened.PolicyHistory, calls.Load())
	}
}

func TestMonitorProviderPublicBudgetRenewalKeepsIncidentAndRoster(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorProviderTestValue(fixture.clock.now())
	var outage atomic.Bool
	var calls atomic.Uint64
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sequence := calls.Add(1)
		if outage.Load() {
			w.WriteHeader(503)
			return
		}
		candidate := value
		candidate.Sequence = sequence
		_ = json.NewEncoder(w).Encode(candidate)
	}))
	defer source.Close()
	policy := monitorProviderTestPolicy(value, source.URL+"/provider-progress")
	fixture.policy.Providers = []monitorProviderPolicy{policy}
	fixture.writePolicy(t)
	monitorProgressPolicyPublicSample(t, fixture, policy.Role, monitorServiceHooks{})
	path, metrics := monitorProviderPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	read := func() monitorProviderCheckpointRecord {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record monitorProviderCheckpointRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	prior := read()
	if prior.PolicyHistory == nil || len(prior.PolicyHistory.Entries) != 1 || prior.State.Record == nil {
		t.Fatal("initial provider policy history is absent")
	}
	policy.Renewal = &monitorProgressPolicyRenewal{Original: policy.resources(), PreviousSha256: prior.PolicyHistory.Entries[0].ContentHash, ReviewSha256: "sha256:" + strings.Repeat("b", 64)}
	policy.ReadBudgetSeconds, policy.FreshnessSeconds = 600, 120
	fixture.policy.Providers[0] = policy
	fixture.writePolicy(t)
	outage.Store(true)
	waited := false
	monitorProgressPolicyPublicSample(t, fixture, policy.Role, monitorServiceHooks{rpcWait: func(ctx context.Context, role string, _ time.Duration) error {
		deadline, present := ctx.Deadline()
		if role != policy.Role || !present || time.Until(deadline) < 590*time.Second || time.Until(deadline) > 600*time.Second {
			return errors.New("renewed worker did not consume its reviewed600second budget")
		}
		waited = true
		return context.DeadlineExceeded
	}})
	held := read()
	if !waited || held.PolicyHistory == nil || len(held.PolicyHistory.Entries) != 2 || held.State.Status != "unavailable" || held.State.Record == nil || held.State.Record.Sequence != prior.State.Record.Sequence || held.State.Incidents != prior.State.Incidents+1 || held.PolicyHash != prior.PolicyHash {
		t.Fatal("renewal fabricated readiness or reset retained incident", held, waited)
	}
	outage.Store(false)
	monitorProgressPolicyPublicSample(t, fixture, policy.Role, monitorServiceHooks{})
	recovered := read()
	if recovered.PolicyHistory == nil || recovered.State.Record == nil || recovered.State.Status != "ok" || recovered.State.Incidents != held.State.Incidents || len(recovered.PolicyHistory.Entries) != 2 || rootObjectHash(recovered.State.Record.Members) != rootObjectHash(prior.State.Record.Members) {
		t.Fatal("renewed provider lost original roster or outage history", recovered)
	}
	raw, err := os.ReadFile(metrics)
	if err != nil || !bytes.Contains(raw, []byte("policy_review_signature_verified{role=\"provider-a\"} 0")) || !bytes.Contains(raw, []byte("policy_acknowledged_revisions{role=\"provider-a\"} 2")) {
		t.Fatal("local review reference gained signature authority", err, string(raw))
	}
}

// This exact four-field struct and hash procedure match the original800/ff42
// writer. It intentionally has no new policy-history field or renewal helper.
func monitorClaimLegacyPolicyFixture(t *testing.T) (*monitorServicesFixture, context.Context, monitorClaimPolicy, monitorClaimCheckpointRecord) {
	t.Helper()
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
	legacy := struct {
		Schema      string            `json:"schema"`
		PolicyHash  string            `json:"policy_hash"`
		State       monitorClaimState `json:"state"`
		ContentHash string            `json:"content_hash"`
	}{Schema: monitorClaimCheckpointSchema, PolicyHash: policy.hash(), State: *worker.state}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	legacy.ContentHash = hex.EncodeToString(digest[:])
	raw, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.checkpoint.directory.publish(filepath.Base(worker.checkpoint.path), append(raw, '\n'), 0600, worker.checkpoint.syncDirectory); err != nil {
		t.Fatal(err)
	}
	if err := worker.close(monitorServiceHooks{}); err != nil {
		t.Fatal(err)
	}
	var parsed monitorClaimCheckpointRecord
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if got, err := hashMonitorClaimCheckpoint(parsed); err != nil || got != legacy.ContentHash {
		t.Fatal("new layout changed original checkpoint grammar", got, legacy.ContentHash, err)
	}
	return fixture, ctx, policy, parsed
}

func TestMonitorClaimLegacyCheckpointRenewalPreservesExactOrigin(t *testing.T) {
	fixture, ctx, policy, prior := monitorClaimLegacyPolicyFixture(t)
	path, _ := monitorClaimPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	policy.Renewal = &monitorProgressPolicyRenewal{Original: policy.resources(), PreviousSha256: "sha256:" + prior.ContentHash, ReviewSha256: "sha256:" + strings.Repeat("c", 64)}
	policy.ReadBudgetSeconds = 600
	policy.Epochs = append(policy.Epochs, monitorClaimEpochPolicy{Epoch: 8, ShareBps: 1000, AcceptBy: fixture.clock.now().Add(time.Hour).Format(time.RFC3339Nano)})
	worker, err := openMonitorClaimWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal("literal prior checkpoint could not renew", err)
	}
	defer worker.close(monitorServiceHooks{})
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || worker.state.current || worker.acknowledgedPolicies != 0 || worker.policyHistory.LegacyCheckpointSha256 != prior.ContentHash || rootObjectHash(worker.state.Epochs[0]) != rootObjectHash(prior.State.Epochs[0]) {
		t.Fatal("legacy admission rewrote or refreshed historical evidence", err, worker.state)
	}
	if err := worker.save(); err != nil {
		t.Fatal(err)
	}
	if worker.acknowledgedPolicies != 2 || worker.policyHistory.Entries[0].PolicyHash != prior.PolicyHash {
		t.Fatal("legacy publication lost its exact original policy")
	}
}

func TestMonitorClaimRenewalRefusesExpectationAndIdentityRewrites(t *testing.T) {
	fixture, ctx, original, prior := monitorClaimLegacyPolicyFixture(t)
	path, _ := monitorClaimPaths(fixture.checkpointPath, fixture.metricsPath, original.Role)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*monitorClaimPolicy){
		func(p *monitorClaimPolicy) { p.ExpectedMember = "foreign-member" },
		func(p *monitorClaimPolicy) { p.ExpectedPool.NoId = "8" },
		func(p *monitorClaimPolicy) { p.Endpoint = "https://foreign.example/claim-progress" },
		func(p *monitorClaimPolicy) {
			p.Epochs[0].AcceptBy = fixture.clock.now().Add(time.Hour).Format(time.RFC3339Nano)
		},
		func(p *monitorClaimPolicy) { p.Epochs[0].ShareBps = 6000 },
		func(p *monitorClaimPolicy) { p.Epochs[0].PayoutRoot = "0x" + strings.Repeat("e", 64) },
		func(p *monitorClaimPolicy) { p.Epochs = p.Epochs[1:] },
	}
	for index, mutate := range mutations {
		policy := original
		policy.Epochs = append([]monitorClaimEpochPolicy(nil), original.Epochs...)
		policy.Renewal = &monitorProgressPolicyRenewal{Original: original.resources(), PreviousSha256: "sha256:" + prior.ContentHash, ReviewSha256: "sha256:" + strings.Repeat("d", 64)}
		policy.ReadBudgetSeconds = 600
		mutate(&policy)
		worker, err := openMonitorClaimWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
		if err == nil {
			worker.close(monitorServiceHooks{})
			t.Fatal("review digest allowed original expectation or identity replacement", index)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("refused policy rewrite mutated retained checkpoint", index, err)
		}
	}
}

func TestMonitorProgressPolicyAcknowledgmentRequiresDurablePublication(t *testing.T) {
	fixture, ctx, policy, prior := monitorClaimLegacyPolicyFixture(t)
	policy.Renewal = &monitorProgressPolicyRenewal{Original: policy.resources(), PreviousSha256: "sha256:" + prior.ContentHash, ReviewSha256: "sha256:" + strings.Repeat("e", 64)}
	policy.ReadBudgetSeconds = 600
	worker, err := openMonitorClaimWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.close(monitorServiceHooks{})
	worker.checkpoint.syncDirectory = func(file *os.File) error { return errors.Join(file.Sync(), syscall.EIO) }
	if err := worker.save(); err == nil {
		t.Fatal("lost publication acknowledgment was reported durable")
	}
	status := progressPolicyStatus(worker.policyHistory, worker.acknowledgedPolicies)
	if !status.Pending || status.Acknowledged != 0 || status.AcknowledgedSha256 != "" {
		t.Fatal("pending policy revision gained acknowledged authority", status)
	}
}

func TestMonitorProgressReviewCapacityGrowsWithoutDroppingAcknowledgments(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	policy := monitorProviderTestPolicy(monitorProviderTestValue(now), "https://monitor.example/provider-progress")
	original := policy.resources()
	history := newMonitorProgressPolicyHistory(original, policy.hash(), "")
	for index := 1; index < defaultMonitorProgressReviews; index++ {
		policy.ReadBudgetSeconds = 300 + uint64(index)
		policy.Renewal = &monitorProgressPolicyRenewal{Original: original, PreviousSha256: history.Entries[len(history.Entries)-1].ContentHash, ReviewSha256: fmt.Sprintf("sha256:%064x", index)}
		var err error
		history, _, _, err = renewMonitorProgressPolicy(false, policy.resources(), policy.Renewal, history.Entries[0].PolicyHash, "", history, policy.policyHashAt)
		if err != nil {
			t.Fatal(index, err)
		}
	}
	before := rootObjectHash(history)
	policy.ReadBudgetSeconds++
	policy.Renewal = &monitorProgressPolicyRenewal{Original: original, PreviousSha256: history.Entries[len(history.Entries)-1].ContentHash, ReviewSha256: fmt.Sprintf("sha256:%064x", 100)}
	if _, _, _, err := renewMonitorProgressPolicy(false, policy.resources(), policy.Renewal, history.Entries[0].PolicyHash, "", history, policy.policyHashAt); err == nil {
		t.Fatal("review capacity silently evicted original history")
	}
	if !progressPolicyStatus(history, len(history.Entries)).CapacityWarning || rootObjectHash(history) != before {
		t.Fatal("full review history was not retained and visible")
	}
	policy.ReviewHistoryEntries = 64
	next, _, _, err := renewMonitorProgressPolicy(false, policy.resources(), policy.Renewal, history.Entries[0].PolicyHash, "", history, policy.policyHashAt)
	if err != nil || next == nil || len(next.Entries) != 33 || rootObjectHash(next.Entries[:32]) != rootObjectHash(history.Entries) {
		t.Fatal("reviewed capacity growth lost original acknowledgments", err)
	}
	policy.ReadBudgetSeconds++
	policy.Renewal.PreviousSha256 = next.Entries[32].ContentHash
	policy.Renewal.ReviewSha256 = next.Entries[1].ReviewSha256
	if _, _, _, err := renewMonitorProgressPolicy(false, policy.resources(), policy.Renewal, next.Entries[0].PolicyHash, "", next, policy.policyHashAt); err == nil {
		t.Fatal("nonadjacent policy review reference reuse was accepted")
	}
}

func TestMonitorClaimEpochCapacityGrowthRetainsEveryExpectedDebt(t *testing.T) {
	fixture, ctx, policy, prior := monitorClaimLegacyPolicyFixture(t)
	original := policy.resources()
	for epoch := int64(8); epoch < 24; epoch++ {
		policy.Epochs = append(policy.Epochs, monitorClaimEpochPolicy{Epoch: epoch, ShareBps: 1000, AcceptBy: fixture.clock.now().Add(time.Hour).Format(time.RFC3339Nano)})
	}
	policy.Renewal = &monitorProgressPolicyRenewal{Original: original, PreviousSha256: "sha256:" + prior.ContentHash, ReviewSha256: "sha256:" + strings.Repeat("f", 64)}
	if policy.validate(monitorTestExpectation()) == nil {
		t.Fatal("configured epoch count exceeded unreviewed capacity")
	}
	policy.EpochCapacity = 32
	worker, err := openMonitorClaimWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal("reviewed larger epoch census could not retain original owner", err)
	}
	defer worker.close(monitorServiceHooks{})
	if len(worker.state.Epochs) != 17 || worker.state.Epochs[0].Observation == nil || worker.state.summary(policy, fixture.clock.now()).Unknown != 16 {
		t.Fatal("capacity growth omitted independent expected debt", worker.state)
	}
	if err := worker.save(); err != nil {
		t.Fatal(err)
	}
	shrunk := policy
	shrunk.EpochCapacity = 16
	if _, _, _, err := renewMonitorProgressPolicy(true, shrunk.resources(), shrunk.Renewal, prior.PolicyHash, "", worker.policyHistory, shrunk.policyHashAt); err == nil {
		t.Fatal("renewal shrank acknowledged epoch capacity")
	}
}

func TestMonitorProviderLegacyPolicyRenewalKeepsOriginalRoster(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	value := monitorProviderTestValue(fixture.clock.now())
	original := monitorProviderTestPolicy(value, "https://monitor.example/provider-progress")
	fixture.policy.Providers = []monitorProviderPolicy{original}
	fixture.writePolicy(t)
	ctx := monitorTestStorageContext(t, t.Context(), fixture.args("https://rpc.example"))
	worker, err := openMonitorProviderWorker(ctx, original, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	worker.state.observe(original, &value, "ok", fixture.clock.now())
	// Original ff42/800 provider checkpoint grammar, independently marshaled.
	legacy := struct {
		Schema      string               `json:"schema"`
		PolicyHash  string               `json:"policy_hash"`
		State       monitorProviderState `json:"state"`
		ContentHash string               `json:"content_hash"`
	}{Schema: monitorProviderCheckpointSchema, PolicyHash: original.hash(), State: *worker.state}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	legacy.ContentHash = hex.EncodeToString(digest[:])
	raw, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := worker.checkpoint.path
	if err := worker.checkpoint.directory.publish(filepath.Base(path), append(raw, '\n'), 0600, worker.checkpoint.syncDirectory); err != nil {
		t.Fatal(err)
	}
	if err := worker.close(monitorServiceHooks{}); err != nil {
		t.Fatal(err)
	}
	var decoded monitorProviderCheckpointRecord
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if hash, err := hashMonitorProviderCheckpoint(decoded); err != nil || hash != legacy.ContentHash {
		t.Fatal("provider legacy checksum changed under optional history layout", hash, err)
	}
	policy := original
	policy.ReadBudgetSeconds = 600
	policy.Renewal = &monitorProgressPolicyRenewal{Original: original.resources(), PreviousSha256: "sha256:" + legacy.ContentHash, ReviewSha256: "sha256:" + strings.Repeat("a", 64)}
	for _, mutation := range []func(*monitorProviderPolicy){
		func(p *monitorProviderPolicy) { p.ExpectedSource.ConfigHash = strings.Repeat("b", 64) },
		func(p *monitorProviderPolicy) { p.Members[0].ClientId = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" },
		func(p *monitorProviderPolicy) { p.Endpoint = "https://foreign.example/provider-progress" },
	} {
		candidate := policy
		candidate.Members = append([]monitorExpectedProviderMember(nil), policy.Members...)
		mutation(&candidate)
		owner, err := openMonitorProviderWorker(ctx, candidate, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
		if err == nil {
			owner.close(monitorServiceHooks{})
			t.Fatal("provider review replaced original roster or source")
		}
	}
	next, err := openMonitorProviderWorker(ctx, policy, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal("original provider checkpoint could not renew", err)
	}
	defer next.close(monitorServiceHooks{})
	if next.state.current || next.policyHistory.LegacyCheckpointSha256 != legacy.ContentHash || rootObjectHash(*next.state) != rootObjectHash(legacy.State) {
		t.Fatal("provider renewal rewrote original observation", next.state)
	}
	if err := next.save(); err != nil {
		t.Fatal(err)
	}
	if next.acknowledgedPolicies != 2 {
		t.Fatal("provider review was not acknowledged after publication")
	}
}

func TestMonitorPolicyRenewalCannotStartWithoutRetainedHistory(t *testing.T) {
	fixture := newMonitorServicesFixture(t)
	claim := monitorClaimTestPolicy(t, monitorClaimTestValue(fixture.clock.now()), "https://monitor.example/claim-progress", fixture.clock.now())
	provider := monitorProviderTestPolicy(monitorProviderTestValue(fixture.clock.now()), "https://monitor.example/provider-progress")
	claim.Renewal = &monitorProgressPolicyRenewal{Original: claim.resources(), PreviousSha256: "sha256:" + strings.Repeat("a", 64), ReviewSha256: "sha256:" + strings.Repeat("b", 64)}
	provider.Renewal = &monitorProgressPolicyRenewal{Original: provider.resources(), PreviousSha256: "sha256:" + strings.Repeat("c", 64), ReviewSha256: "sha256:" + strings.Repeat("d", 64)}
	claim.ReadBudgetSeconds, provider.ReadBudgetSeconds = 600, 600
	fixture.policy.Claims = []monitorClaimPolicy{claim}
	fixture.policy.Providers = []monitorProviderPolicy{provider}
	fixture.writePolicy(t)
	ctx := monitorTestStorageContext(t, t.Context(), fixture.args("https://rpc.example"))
	if owner, err := openMonitorClaimWorker(ctx, claim, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{}); err == nil {
		owner.close(monitorServiceHooks{})
		t.Fatal("renewal silently created fresh claim history")
	}
	if owner, err := openMonitorProviderWorker(ctx, provider, monitorTestExpectation(), fixture.checkpointPath, fixture.metricsPath, monitorServiceHooks{}); err == nil {
		owner.close(monitorServiceHooks{})
		t.Fatal("renewal silently created fresh provider history")
	}
}

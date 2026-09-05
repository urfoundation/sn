package validator

// First-assignment depth tests keep the configured release policy at M8. The
// fixture authenticates the untouched M8 SEED before an adversarial server
// genuinely signs another globally legal depth; no verifier is substituted.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urnetwork/connect"
)

// Used synchronously by one real engine. The server retains its own normal
// lock and verifies every SEED/EXTEND and signs every ASSIGN/FINAL normally.
type trailPolicyDepthTransport struct {
	server          *mockVerifyServer
	assignedDepth   int
	seedRequests    int
	extendRequests  int
	requestedDepth  int
	firstAssignment connect.VerifyAssignResult
}

// The only deviation is server-owned M after the real server has verified the
// original signed request. No request or validator signature is rewritten.
func (self *trailPolicyDepthTransport) PostVerify(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
	var envelope struct {
		TrailID *connect.Id `json:"trail_id"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	if envelope.TrailID != nil {
		self.extendRequests++
		return self.server.PostVerify(ctx, hop, body)
	}
	self.seedRequests++
	var seed connect.VerifySeedArgs
	if err := json.Unmarshal(body, &seed); err != nil {
		return nil, err
	}
	response, err := self.server.PostVerify(ctx, hop, body)
	if err != nil {
		return nil, err
	}
	var original connect.VerifyAssignResult
	if err := json.Unmarshal(response, &original); err != nil {
		return nil, err
	}
	response, err = func() ([]byte, error) {
		self.server.mu.Lock()
		defer self.server.mu.Unlock()
		state, exists := self.server.trails[original.TrailId]
		if !exists || state.m != seed.M || original.M != seed.M {
			return nil, errors.New("depth fixture did not authenticate the original requested depth")
		}
		self.requestedDepth = seed.M
		state.m = self.assignedDepth
		changed, err := self.server.assignResponse(state)
		if err != nil {
			return nil, err
		}
		state.lastResponse = bytes.Clone(changed)
		return changed, nil
	}()
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(response, &self.firstAssignment); err != nil {
		return nil, err
	}
	return response, nil
}

// Owns the unchanged M8 engine, policy statistics and optional durable
// admission lane. All state is private test data, never release runtime data.
type trailPolicyDepthFixture struct {
	engine    *TrailEngine
	stats     *StatsEngine
	ledger    *AttemptLedger
	store     *ProofStore
	transport *trailPolicyDepthTransport
}

// Keeps the real policy a-min and all eight-hop work; the alternate depth is
// only the test server's signed first response, not a configuration change.
func newTrailPolicyDepthFixture(t *testing.T, assignedDepth int, withLedger bool) trailPolicyDepthFixture {
	t.Helper()
	policy := exactPolicy(t)
	if policy.Verify.TrailDepth != 8 {
		t.Fatalf("release policy depth = %d, want the unchanged M8 policy", policy.Verify.TrailDepth)
	}
	server, key, clientID := newMockVerifyServer(t, connect.VerifyMMax)
	stateDir := filepath.Join(t.TempDir(), "private-state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewProofStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, key, clientID, policy.Verify.TrailDepth, store)
	stats.cfg = (StatsConfig{AMin: policy.Verify.ReliabilityAMin}).withDefaults()
	transport := &trailPolicyDepthTransport{server: server, assignedDepth: assignedDepth}
	engine.transport = transport
	fixture := trailPolicyDepthFixture{engine: engine, stats: stats, store: store, transport: transport}
	if withLedger {
		generation := uint64(1)
		fixture.ledger = configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	}
	return fixture
}

// Independently proves the altered response is a real server signature over
// the exact nonce, validator key, depth and ordered first assignment.
func assertTrailPolicyDepthAssignment(t *testing.T, fixture trailPolicyDepthFixture, depth int) {
	t.Helper()
	transport := fixture.transport
	assign := transport.firstAssignment
	if fixture.engine.cfg.M != 8 || fixture.stats.cfg.AMin != exactPolicy(t).Verify.ReliabilityAMin || transport.requestedDepth != 8 || assign.M != depth || len(assign.Trail) != 1 {
		t.Fatalf("depth fixture changed configuration or request: configured=%d requested=%d returned=%d", fixture.engine.cfg.M, transport.requestedDepth, assign.M)
	}
	walked := append(append([]connect.Id(nil), assign.Trail...), assign.NextHop)
	message, err := connect.BuildVerifyAssignMessage(assign.ServerKeyId, assign.TrailId, assign.ServerNonce, fixture.engine.vpk, byte(assign.M), walked)
	if err != nil || !ed25519.Verify(transport.server.serverPublicKeys()[assign.ServerKeyId], message, assign.AssignSig) {
		t.Fatalf("first assignment lacks a genuine server signature: %v", err)
	}
}

// A successful baseline must retain all M8 proof bytes, seven exposures and
// confirmations, and every durable checkpoint when that lane is enabled.
func assertTrailPolicyDepthM8Success(t *testing.T, fixture trailPolicyDepthFixture, proof *ProofRecord, err error) {
	t.Helper()
	if err != nil || proof == nil {
		t.Fatalf("unchanged M8 trail failed: %v", err)
	}
	assertTrailPolicyDepthAssignment(t, fixture, 8)
	if err := VerifyProofRecord(proof, fixture.engine.vpk, fixture.transport.server.serverPublicKeys(), 8); err != nil {
		t.Fatalf("full M8 proof verification failed: %v", err)
	}
	if len(fixture.stats.ProviderIDs()) != 7 {
		t.Fatal("M8 control omitted a real assigned provider")
	}
	for _, hop := range proof.Hops[1:] {
		if assignments, confirmations := fixture.stats.WindowCounts(hop.ClientId); assignments != 1 || confirmations != 1 {
			t.Fatalf("M8 provider count = %d/%d, want 1/1", assignments, confirmations)
		}
	}
	if fixture.ledger != nil {
		cut, err := fixture.ledger.BuildCut(attemptLedgerTestBoundary(), 1, 1)
		if err != nil || len(cut.Records) != 8 || fixture.stats.activeAttemptCount != 0 {
			t.Fatalf("M8 durable census or admission ownership differs: %v", err)
		}
		if err := VerifyAttemptLedgerCut(cut, fixture.engine.vpk, fixture.transport.server.serverPublicKeys()); err != nil {
			t.Fatal(err)
		}
	}
	stored, skipped, err := fixture.store.Load()
	if err != nil || skipped != 0 || len(stored) != 1 || stored[0].TrailId != proof.TrailId {
		t.Fatalf("M8 proof persistence differs: records=%d skipped=%d error=%v", len(stored), skipped, err)
	}
}

// The pre-fix branch proves the accepted alternate proof/cut cryptographically
// before reporting its causal failure. The fixed branch proves refusal before
// EXTEND or durable/statistical mutation and a full M8 retry on the same lane.
func assertTrailPolicyDepthRejection(t *testing.T, assignedDepth int, withLedger bool) {
	t.Helper()
	fixture := newTrailPolicyDepthFixture(t, assignedDepth, withLedger)
	proof, runErr := fixture.engine.RunTrail(context.Background())
	assertTrailPolicyDepthAssignment(t, fixture, assignedDepth)
	if runErr == nil {
		if err := VerifyProofRecord(proof, fixture.engine.vpk, fixture.transport.server.serverPublicKeys(), assignedDepth); err != nil {
			t.Fatalf("alternate-depth proof is not genuinely valid: %v", err)
		}
		if err := VerifyProofRecord(proof, fixture.engine.vpk, fixture.transport.server.serverPublicKeys(), 8); err == nil {
			t.Fatal("independent proof verifier lost its existing explicit policy-depth check")
		}
		var durableRecords uint64
		if fixture.ledger != nil {
			cut, err := fixture.ledger.BuildCut(attemptLedgerTestBoundary(), 1, 1)
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyAttemptLedgerCut(cut, fixture.engine.vpk, fixture.transport.server.serverPublicKeys()); err != nil {
				t.Fatalf("alternate cut lost genuine signatures: %v", err)
			}
			durableRecords = fixture.ledger.LastSequence()
		}
		t.Fatalf("configured M8 engine accepted signed M%d: %d EXTEND requests, %d durable records, %d assigned providers", assignedDepth, fixture.transport.extendRequests, durableRecords, len(fixture.stats.ProviderIDs()))
	}
	var trailErr *TrailError
	if proof != nil || !errors.As(runErr, &trailErr) || trailErr.Kind != TrailErrorProtocol {
		t.Fatalf("first-depth refusal did not report a protocol error: %v", runErr)
	}
	if fixture.transport.seedRequests != 1 || fixture.transport.extendRequests != 0 || len(fixture.stats.ProviderIDs()) != 0 || fixture.stats.activeAttemptCount != 0 || fixture.ledger != nil && fixture.ledger.LastSequence() != 0 {
		t.Fatal("first-depth refusal happened after an EXTEND, evidence mutation or leaked reservation")
	}
	stored, skipped, err := fixture.store.Load()
	if err != nil || len(stored) != 0 || skipped != 0 {
		t.Fatalf("refused depth persisted proof data: %d/%d %v", len(stored), skipped, err)
	}
	fixture.transport.assignedDepth = 8
	proof, err = fixture.engine.RunTrail(context.Background())
	assertTrailPolicyDepthM8Success(t, fixture, proof, err)
}

// A valid server signature cannot shorten the configured trail.
func TestTrailPolicyDepthRejectsSignedShorterFirstAssignment(t *testing.T) {
	assertTrailPolicyDepthRejection(t, connect.VerifyMMin, false)
}

// A valid server signature cannot exceed the configured work envelope.
func TestTrailPolicyDepthRejectsSignedLongerFirstAssignment(t *testing.T) {
	assertTrailPolicyDepthRejection(t, connect.VerifyMMax, false)
}

// The durable admission path must reject M4 before its first checkpoint.
func TestTrailPolicyDepthLedgerRejectsSignedShorterFirstAssignment(t *testing.T) {
	assertTrailPolicyDepthRejection(t, connect.VerifyMMin, true)
}

// The durable admission path must reject M16 before its first checkpoint.
func TestTrailPolicyDepthLedgerRejectsSignedLongerFirstAssignment(t *testing.T) {
	assertTrailPolicyDepthRejection(t, connect.VerifyMMax, true)
}

// The exact configured response retains the full legacy proof path.
func TestTrailPolicyDepthAcceptsSignedConfiguredDepth(t *testing.T) {
	fixture := newTrailPolicyDepthFixture(t, 8, false)
	proof, err := fixture.engine.RunTrail(context.Background())
	assertTrailPolicyDepthM8Success(t, fixture, proof, err)
}

// The exact configured response retains all durable M8 lifecycle work.
func TestTrailPolicyDepthLedgerAcceptsSignedConfiguredDepth(t *testing.T) {
	fixture := newTrailPolicyDepthFixture(t, 8, true)
	proof, err := fixture.engine.RunTrail(context.Background())
	assertTrailPolicyDepthM8Success(t, fixture, proof, err)
}

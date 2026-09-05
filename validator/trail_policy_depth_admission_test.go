package validator

// Adjacent controls preserve the server's documented default/clamp contract,
// every real legal-depth signature and the first-assignment ownership boundary.
// The original six causal M8 tests remain unchanged in their separate file.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

// Constructor defaults, not a test-side effective-depth mutation, choose the
// actual signed SEED. The real server fixture authenticates that request and
// signs the declared effective response with all normal protocol work.
func newTrailConfiguredDepthFixture(t *testing.T, requested, assigned int, withLedger bool) trailPolicyDepthFixture {
	t.Helper()
	fixture := newTrailPolicyDepthFixture(t, assigned, withLedger)
	prior := fixture.engine
	config := prior.cfg
	config.M, config.AttemptLedger, config.AttemptBoundaryResolver = requested, fixture.ledger, prior.resolve
	fixture.engine = NewTrailEngine(prior.clientId, prior.vsk, fixture.transport, prior.keys, prior.pickSeed, fixture.stats, fixture.store, prior.epochFn, config)
	return fixture
}

// Controls authenticate the full assignment and final proof at the effective
// legal depth, preserving every exposure, confirmation and durable checkpoint.
func assertTrailConfiguredDepthSuccess(t *testing.T, fixture trailPolicyDepthFixture, requested, effective int, proof *ProofRecord, runErr error) {
	t.Helper()
	if requested == 0 {
		requested = connect.VerifyMDefault
	}
	assign := fixture.transport.firstAssignment
	if runErr != nil || proof == nil || fixture.engine.cfg.M != requested || fixture.transport.requestedDepth != requested || assign.M != effective || fixture.stats.cfg.AMin != exactPolicy(t).Verify.ReliabilityAMin {
		t.Fatalf("configured=%d effective=%d signed request=%d assignment=%d proof=%+v error=%v", fixture.engine.cfg.M, effective, fixture.transport.requestedDepth, assign.M, proof, runErr)
	}
	walked := append(append([]connect.Id(nil), assign.Trail...), assign.NextHop)
	message, err := connect.BuildVerifyAssignMessage(assign.ServerKeyId, assign.TrailId, assign.ServerNonce, fixture.engine.vpk, byte(effective), walked)
	if err != nil || !ed25519.Verify(fixture.transport.server.serverPublicKeys()[assign.ServerKeyId], message, assign.AssignSig) {
		t.Fatalf("effective-depth first signature invalid: %v", err)
	}
	if err := VerifyProofRecord(proof, fixture.engine.vpk, fixture.transport.server.serverPublicKeys(), effective); err != nil {
		t.Fatal(err)
	}
	if len(proof.Hops) != effective || len(fixture.stats.ProviderIDs()) != effective-1 {
		t.Fatal("legal-depth control reduced its actual full trail work")
	}
	for _, hop := range proof.Hops[1:] {
		if assigned, confirmed := fixture.stats.WindowCounts(hop.ClientId); assigned != 1 || confirmed != 1 {
			t.Fatalf("provider counts = %d/%d, want 1/1", assigned, confirmed)
		}
	}
	if fixture.ledger != nil {
		cut, err := fixture.ledger.BuildCut(attemptLedgerTestBoundary(), 1, 1)
		if err != nil || len(cut.Records) != effective || fixture.stats.activeAttemptCount != 0 {
			t.Fatalf("legal-depth durable control: %v", err)
		}
		if err := VerifyAttemptLedgerCut(cut, fixture.engine.vpk, fixture.transport.server.serverPublicKeys()); err != nil {
			t.Fatal(err)
		}
	}
	stored, skipped, err := fixture.store.Load()
	if err != nil || skipped != 0 || len(stored) != 1 || stored[0].TrailId != proof.TrailId {
		t.Fatalf("legal-depth proof persistence = %d/%d: %v", len(stored), skipped, err)
	}
}

// Zero configuration becomes the actual default M8 before the signed SEED.
func TestTrailPolicyDepthDefaultConstructorUsesEffectiveM8(t *testing.T) {
	for _, durable := range []bool{false, true} {
		fixture := newTrailConfiguredDepthFixture(t, 0, connect.VerifyMDefault, durable)
		proof, err := fixture.engine.RunTrail(context.Background())
		assertTrailConfiguredDepthSuccess(t, fixture, 0, connect.VerifyMDefault, proof, err)
	}
}

// The server's positive lower clamp remains legal; no request byte is silently
// normalized or re-signed by this client-side response validation repair.
func TestTrailPolicyDepthLowerClampControls(t *testing.T) {
	for _, requested := range []int{1, connect.VerifyMMin - 1} {
		for _, durable := range []bool{false, true} {
			fixture := newTrailConfiguredDepthFixture(t, requested, connect.VerifyMMin, durable)
			proof, err := fixture.engine.RunTrail(context.Background())
			assertTrailConfiguredDepthSuccess(t, fixture, requested, connect.VerifyMMin, proof, err)
		}
	}
}

// The largest representable request retains its documented upper clamp;
// requests outside the one-byte signature domain are independently refused.
func TestTrailPolicyDepthUpperClampControls(t *testing.T) {
	for _, requested := range []int{connect.VerifyMMax + 1, connect.VerifyMMax + 100, 255} {
		for _, durable := range []bool{false, true} {
			fixture := newTrailConfiguredDepthFixture(t, requested, connect.VerifyMMax, durable)
			proof, err := fixture.engine.RunTrail(context.Background())
			assertTrailConfiguredDepthSuccess(t, fixture, requested, connect.VerifyMMax, proof, err)
		}
	}
}

// Generic legal boundary policies stay M4/M16; this is not a hardcoded M8 gate.
func TestTrailPolicyDepthLegalBoundaryControls(t *testing.T) {
	for _, depth := range []int{connect.VerifyMMin, connect.VerifyMMax} {
		for _, durable := range []bool{false, true} {
			fixture := newTrailConfiguredDepthFixture(t, depth, depth, durable)
			proof, err := fixture.engine.RunTrail(context.Background())
			assertTrailConfiguredDepthSuccess(t, fixture, depth, depth, proof, err)
		}
	}
}

// A signature at another globally legal depth cannot replace the default or
// either clamp; refusal still precedes all evidence and a full legal retry.
func TestTrailPolicyDepthRejectsWrongClampedFirstAssignment(t *testing.T) {
	for _, variation := range []struct {
		requested int
		effective int
		assigned  int
	}{
		{requested: 0, effective: connect.VerifyMDefault, assigned: connect.VerifyMMin},
		{requested: 1, effective: connect.VerifyMMin, assigned: connect.VerifyMDefault},
		{requested: 17, effective: connect.VerifyMMax, assigned: connect.VerifyMDefault},
		{requested: 255, effective: connect.VerifyMMax, assigned: connect.VerifyMMin},
	} {
		for _, durable := range []bool{false, true} {
			fixture := newTrailConfiguredDepthFixture(t, variation.requested, variation.assigned, durable)
			proof, err := fixture.engine.RunTrail(context.Background())
			var trailErr *TrailError
			if proof != nil || !errors.As(err, &trailErr) || trailErr.Kind != TrailErrorProtocol || !strings.Contains(err.Error(), "requested effective depth") {
				t.Fatalf("requested=%d assigned=%d refusal: %v", variation.requested, variation.assigned, err)
			}
			assign := fixture.transport.firstAssignment
			message, err := connect.BuildVerifyAssignMessage(assign.ServerKeyId, assign.TrailId, assign.ServerNonce, fixture.engine.vpk, byte(assign.M), append(append([]connect.Id(nil), assign.Trail...), assign.NextHop))
			if err != nil || !ed25519.Verify(fixture.transport.server.serverPublicKeys()[assign.ServerKeyId], message, assign.AssignSig) {
				t.Fatalf("refused clamp fixture lost its real signature: %v", err)
			}
			if fixture.transport.seedRequests != 1 || fixture.transport.extendRequests != 0 || len(fixture.stats.ProviderIDs()) != 0 || fixture.stats.activeAttemptCount != 0 || fixture.ledger != nil && fixture.ledger.LastSequence() != 0 {
				t.Fatal("clamp refusal changed durable/statistical ownership")
			}
			fixture.transport.assignedDepth = variation.effective
			proof, err = fixture.engine.RunTrail(context.Background())
			assertTrailConfiguredDepthSuccess(t, fixture, variation.requested, variation.effective, proof, err)
		}
	}
}

// Negative and nonrepresentable requests must fail before picker, SEED,
// statistics or durable admission. The signed wire really aliases M4/M260,
// so the guard protects canonicality rather than blessing integer truncation.
func TestTrailPolicyDepthRejectsInvalidRequestedEncodingBeforeSeed(t *testing.T) {
	for _, requested := range []int{-1, -256, 256, 260, int(^uint(0) >> 1)} {
		for _, durable := range []bool{false, true} {
			fixture := newTrailConfiguredDepthFixture(t, requested, connect.VerifyMMin, durable)
			if requested == 260 {
				nonce := bytes.Repeat([]byte{0x61}, connect.VerifyNonceSize)
				legal, err := connect.BuildVerifySeedMessage(fixture.engine.vpk, nonce, byte(connect.VerifyMMin))
				if err != nil {
					t.Fatal(err)
				}
				aliased, err := connect.BuildVerifySeedMessage(fixture.engine.vpk, nonce, byte(requested))
				if err != nil || !bytes.Equal(legal, aliased) || !ed25519.Verify(fixture.engine.vpk, aliased, ed25519.Sign(fixture.engine.vsk, legal)) {
					t.Fatalf("M4/M260 fixture did not reproduce the one-byte signature alias: %v", err)
				}
			}
			picked := false
			fixture.engine.pickSeed = func(context.Context) (connect.Id, error) {
				picked = true
				return fixture.transport.server.providers[0], nil
			}
			proof, err := fixture.engine.RunTrail(context.Background())
			var trailErr *TrailError
			if proof != nil || !errors.As(err, &trailErr) || trailErr.Kind != TrailErrorProtocol || !strings.Contains(err.Error(), "not byte-representable") || picked || fixture.transport.seedRequests != 0 || fixture.transport.extendRequests != 0 || len(fixture.stats.ProviderIDs()) != 0 || fixture.stats.activeAttemptCount != 0 || fixture.ledger != nil && fixture.ledger.LastSequence() != 0 {
				t.Fatalf("requested=%d reached picker, wire or evidence: %v", requested, err)
			}
			stored, skipped, err := fixture.store.Load()
			if err != nil || skipped != 0 || len(stored) != 0 {
				t.Fatalf("invalid requested depth persisted proof data: %v", err)
			}
		}
	}
}

// A synchronous wire adapter changes real server bytes without replacing the
// engine, validator signature or server verification paths underneath it.
type trailDepthAdmissionTransport func(context.Context, connect.Id, []byte) ([]byte, error)

// Forward the real transport contract unchanged apart from its owned bytes.
func (self trailDepthAdmissionTransport) PostVerify(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
	return self(ctx, hop, body)
}

// Matching configured depth does not bypass full signature authentication.
func TestTrailPolicyDepthMatchingDepthStillRequiresSignature(t *testing.T) {
	for _, depth := range []int{connect.VerifyMMin, connect.VerifyMDefault, connect.VerifyMMax} {
		fixture := newTrailConfiguredDepthFixture(t, depth, depth, true)
		fixture.engine.transport = trailDepthAdmissionTransport(func(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
			raw, err := fixture.transport.PostVerify(ctx, hop, body)
			if err != nil {
				return nil, err
			}
			var assign connect.VerifyAssignResult
			if err := json.Unmarshal(raw, &assign); err != nil {
				return nil, err
			}
			assign.AssignSig[0] ^= 1
			return json.Marshal(assign)
		})
		proof, err := fixture.engine.RunTrail(context.Background())
		var trailErr *TrailError
		if proof != nil || !errors.As(err, &trailErr) || trailErr.Kind != TrailErrorProtocol || !strings.Contains(err.Error(), "signature does not verify") || fixture.transport.extendRequests != 0 || fixture.ledger.LastSequence() != 0 || len(fixture.stats.ProviderIDs()) != 0 || fixture.stats.activeAttemptCount != 0 {
			t.Fatalf("matching M%d invalid signature reached admission: %v", depth, err)
		}
	}
}

// After the first M8 checkpoint, a genuinely signed alternative-depth reply
// must terminate that same M8 attempt without recording the substituted hop.
func TestTrailPolicyDepthLaterAssignmentCannotChangeEffectiveDepth(t *testing.T) {
	for _, substituted := range []int{connect.VerifyMMin, connect.VerifyMMax} {
		fixture := newTrailPolicyDepthFixture(t, connect.VerifyMDefault, true)
		changed := false
		fixture.engine.transport = trailDepthAdmissionTransport(func(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
			raw, err := fixture.transport.PostVerify(ctx, hop, body)
			if err != nil || fixture.transport.extendRequests == 0 || changed {
				return raw, err
			}
			changed = true
			var prior connect.VerifyAssignResult
			if err := json.Unmarshal(raw, &prior); err != nil {
				return nil, err
			}
			server := fixture.transport.server
			server.mu.Lock()
			defer server.mu.Unlock()
			state := server.trails[prior.TrailId]
			if state == nil || state.m != 8 || len(state.confirmed) != 2 {
				return nil, errors.New("later-depth fixture did not authenticate the genuine M8 extension")
			}
			state.m = substituted
			return server.assignResponse(state)
		})
		proof, err := fixture.engine.RunTrail(context.Background())
		var trailErr *TrailError
		if proof != nil || !errors.As(err, &trailErr) || trailErr.Kind != TrailErrorProtocol || !strings.Contains(err.Error(), "switched trail identity") || !changed || fixture.transport.extendRequests != 1 || fixture.ledger.LastSequence() != 2 || fixture.stats.activeAttemptCount != 0 {
			t.Fatalf("later M%d substitution disposition differs: %v", substituted, err)
		}
		cut, err := fixture.ledger.BuildCut(attemptLedgerTestBoundary(), 1, 1)
		if err != nil || VerifyAttemptLedgerCut(cut, fixture.engine.vpk, fixture.transport.server.serverPublicKeys()) != nil || len(cut.Records) != 2 || cut.Records[1].M != 8 || cut.Records[1].Disposition != AttemptDispositionProtocol || len(cut.Records[1].Assignments) != 1 || cut.Records[1].Assignments[0].Confirmed {
			t.Fatalf("later substitution changed original M8 evidence: %v", err)
		}
	}
}

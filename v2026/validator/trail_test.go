package validator

// Protocol-logic tests against an in-memory mock of the /verify server
// (VALIDATOR.md §4). The mock implements the full server side — source-hop
// attribution (the `hop` argument plays the source IP), SEED/EXTEND
// signature verification, ASSIGN/FINAL signing, idempotent retry caching —
// so both signature directions are exercised.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand"
	"sync"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// mockTrailState is one server-side trail.
type mockTrailState struct {
	trailId      connect.Id
	serverNonce  []byte
	vpk          []byte
	m            int
	confirmed    []connect.Id
	confirmedAt  []uint64
	pending      connect.Id
	lastResponse []byte // idempotent retry cache (§4.3)
	// lastVerifierSig is the depth-M EXTEND signature — the proof's
	// verifier_sig (§3.3).
	lastVerifierSig []byte
	complete        bool
}

// mockVerifyServer implements TrailTransport as an in-memory /verify server.
type mockVerifyServer struct {
	mu sync.Mutex

	serverKeyId byte
	serverKey   ed25519.PrivateKey

	// eligible providers assignable as next hops (§5.1)
	providers []connect.Id
	// the validator's registered key: SEED vpk must match (§2)
	validatorClientId connect.Id
	validatorVpk      ed25519.PublicKey

	trails map[connect.Id]*mockTrailState
	nowMs  uint64

	// fault injection
	dropNextExtendResponses int                 // lose N EXTEND responses (after processing)
	failHops                map[connect.Id]bool // hops that never respond
	poisonFinal             bool                // emit garbage final_sig
	extendCount             int                 // EXTENDs processed (not counting cache hits)
	cacheHits               int
}

func newMockVerifyServer(t *testing.T, providerCount int) (*mockVerifyServer, ed25519.PrivateKey, connect.Id) {
	t.Helper()
	_, serverKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	validatorPub, validatorKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := &mockVerifyServer{
		serverKeyId:       7,
		serverKey:         serverKey,
		validatorClientId: connect.NewId(),
		validatorVpk:      validatorPub,
		trails:            map[connect.Id]*mockTrailState{},
		nowMs:             1_700_000_000_000,
		failHops:          map[connect.Id]bool{},
	}
	for i := 0; i < providerCount; i++ {
		server.providers = append(server.providers, connect.NewId())
	}
	return server, validatorKey, server.validatorClientId
}

func (self *mockVerifyServer) serverPublicKeys() map[byte]ed25519.PublicKey {
	return map[byte]ed25519.PublicKey{
		self.serverKeyId: self.serverKey.Public().(ed25519.PublicKey),
	}
}

// sampleNext draws uniformly from eligible \ trail (§5.1).
func (self *mockVerifyServer) sampleNext(trail []connect.Id) (connect.Id, error) {
	used := map[connect.Id]bool{}
	for _, hop := range trail {
		used[hop] = true
	}
	var eligible []connect.Id
	for _, provider := range self.providers {
		if !used[provider] && provider != self.validatorClientId {
			eligible = append(eligible, provider)
		}
	}
	if len(eligible) == 0 {
		return connect.Id{}, fmt.Errorf("no eligible providers")
	}
	return eligible[mathrand.Intn(len(eligible))], nil
}

func (self *mockVerifyServer) signAssign(state *mockTrailState) ([]byte, error) {
	signed := append(append([]connect.Id{}, state.confirmed...), state.pending)
	message, err := connect.BuildVerifyAssignMessage(
		self.serverKeyId, state.trailId, state.serverNonce, state.vpk, byte(state.m), signed)
	if err != nil {
		return nil, err
	}
	return connect.SignVerifyMessage(self.serverKey, message), nil
}

func (self *mockVerifyServer) assignResponse(state *mockTrailState) ([]byte, error) {
	assignSig, err := self.signAssign(state)
	if err != nil {
		return nil, err
	}
	return json.Marshal(&connect.VerifyAssignResult{
		TrailId:     state.trailId,
		ServerNonce: state.serverNonce,
		Trail:       state.confirmed,
		NextHop:     state.pending,
		M:           state.m,
		ServerKeyId: self.serverKeyId,
		AssignSig:   assignSig,
	})
}

func (self *mockVerifyServer) finalResponse(state *mockTrailState) ([]byte, error) {
	hops := make([]connect.VerifyProofHop, len(state.confirmed))
	for i, hop := range state.confirmed {
		hops[i] = connect.VerifyProofHop{ClientId: hop, TimeMs: state.confirmedAt[i]}
	}
	finalMessage, err := connect.BuildVerifyFinalMessage(
		self.serverKeyId, state.trailId, state.serverNonce, state.vpk, byte(state.m), hops)
	if err != nil {
		return nil, err
	}
	digest := connect.VerifyFinalDigest(finalMessage)
	// v1 server-attested coverage: M−1 server-assigned completed hops (the
	// seed is excluded, §7.6).
	coverage := uint64(state.m - 1)
	// FINAL signs the 32-byte EFFORT digest (the on-chain 0x402 seam), which
	// binds coverage into the signature (review A2).
	effortDigest := connect.VerifyEffortDigest(digest, coverage)
	finalSig := ed25519.Sign(self.serverKey, effortDigest[:])
	if self.poisonFinal {
		finalSig = make([]byte, ed25519.SignatureSize)
		rand.Read(finalSig)
	}
	// verifier_sig = the validator's depth-M EXTEND signature (stored on
	// the last confirm below in state.lastVerifierSig via closure — the
	// mock re-derives it from the last EXTEND body instead; see
	// handleExtend where it is captured).
	return json.Marshal(&connect.VerifyFinalResult{
		Status: connect.VerifyStatusComplete,
		Proof: &connect.VerifyProof{
			Header: connect.VerifyProofHeader{
				TrailId:     state.trailId,
				ServerNonce: state.serverNonce,
				Vpk:         state.vpk,
				M:           state.m,
			},
			Hops:        hops,
			ServerKeyId: self.serverKeyId,
			Coverage:    coverage,
			FinalSig:    finalSig,
			VerifierSig: state.lastVerifierSig,
		},
	})
}

func (self *mockVerifyServer) PostVerify(ctx context.Context, hop connect.Id, jsonBody []byte) ([]byte, error) {
	self.mu.Lock()
	defer self.mu.Unlock()

	if self.failHops[hop] {
		return nil, fmt.Errorf("hop %s unreachable", hop)
	}

	var envelope struct {
		TrailId *connect.Id `json:"trail_id"`
		Vpk     []byte      `json:"vpk"`
	}
	if err := json.Unmarshal(jsonBody, &envelope); err != nil {
		return nil, err
	}

	var responseBody []byte
	var err error
	if envelope.TrailId == nil {
		responseBody, err = self.handleSeed(hop, jsonBody)
	} else {
		responseBody, err = self.handleExtend(hop, jsonBody)
		if err == nil && self.dropNextExtendResponses > 0 {
			// The server processed the EXTEND but the response is lost —
			// the §4.3 idempotent-retry scenario.
			self.dropNextExtendResponses--
			return nil, fmt.Errorf("simulated response loss")
		}
	}
	if err != nil {
		return nil, err
	}
	return responseBody, nil
}

func (self *mockVerifyServer) handleSeed(sourceHop connect.Id, body []byte) ([]byte, error) {
	var seed connect.VerifySeedArgs
	if err := json.Unmarshal(body, &seed); err != nil {
		return nil, err
	}
	// §4.1: vpk must be the registered key of client_id; verify seed_sig.
	if seed.ClientId != self.validatorClientId || !bytes.Equal(seed.Vpk, self.validatorVpk) {
		return nil, fmt.Errorf("unknown validator")
	}
	seedMessage, err := connect.BuildVerifySeedMessage(seed.Vpk, seed.ClientNonce, byte(seed.M))
	if err != nil {
		return nil, err
	}
	if !connect.VerifyVerifyMessageSignature(seed.Vpk, seedMessage, seed.SeedSig) {
		return nil, fmt.Errorf("bad seed sig")
	}
	// the validator may not seed through itself
	if sourceHop == self.validatorClientId {
		return nil, fmt.Errorf("self seed")
	}

	state := &mockTrailState{
		trailId:     connect.NewId(),
		serverNonce: make([]byte, connect.VerifyNonceSize),
		vpk:         append([]byte{}, seed.Vpk...),
		m:           seed.M,
		confirmed:   []connect.Id{sourceHop},
	}
	rand.Read(state.serverNonce)
	self.nowMs++
	state.confirmedAt = []uint64{self.nowMs}
	next, err := self.sampleNext(state.confirmed)
	if err != nil {
		return nil, err
	}
	state.pending = next
	self.trails[state.trailId] = state
	responseBody, err := self.assignResponse(state)
	if err != nil {
		return nil, err
	}
	state.lastResponse = responseBody
	return responseBody, nil
}

func (self *mockVerifyServer) handleExtend(sourceHop connect.Id, body []byte) ([]byte, error) {
	var extend connect.VerifyExtendArgs
	if err := json.Unmarshal(body, &extend); err != nil {
		return nil, err
	}
	state, ok := self.trails[extend.TrailId]
	if !ok {
		return nil, fmt.Errorf("unknown trail")
	}

	// §4.3 idempotent retry: submitted trail matches confirmed hops → same
	// response, no double count.
	if len(extend.Trail) == len(state.confirmed) {
		match := true
		for i := range extend.Trail {
			if extend.Trail[i] != state.confirmed[i] {
				match = false
				break
			}
		}
		if match && state.lastResponse != nil {
			self.cacheHits++
			return state.lastResponse, nil
		}
	}

	// §4.2 step 3: confirmed hops + the single pending hop.
	if len(extend.Trail) != len(state.confirmed)+1 {
		return nil, fmt.Errorf("bad trail length")
	}
	for i := range state.confirmed {
		if extend.Trail[i] != state.confirmed[i] {
			return nil, fmt.Errorf("history rewrite")
		}
	}
	claimed := extend.Trail[len(extend.Trail)-1]
	if claimed != state.pending {
		return nil, fmt.Errorf("claimed hop is not pending")
	}
	// §4.2 step 5: source == pending.
	if sourceHop != state.pending {
		return nil, fmt.Errorf("source is not the pending hop")
	}
	// §4.2 step 4: extend_sig under vpk.
	extendMessage, err := connect.BuildVerifyExtendMessage(
		state.trailId, state.serverNonce, state.vpk, byte(state.m), extend.Trail)
	if err != nil {
		return nil, err
	}
	if !connect.VerifyVerifyMessageSignature(state.vpk, extendMessage, extend.ExtendSig) {
		return nil, fmt.Errorf("bad extend sig")
	}

	self.extendCount++
	self.nowMs += 25
	state.confirmed = append(state.confirmed, claimed)
	state.confirmedAt = append(state.confirmedAt, self.nowMs)

	var responseBody []byte
	if len(state.confirmed) == state.m {
		state.complete = true
		state.lastVerifierSig = append([]byte{}, extend.ExtendSig...)
		responseBody, err = self.finalResponse(state)
	} else {
		next, sampleErr := self.sampleNext(state.confirmed)
		if sampleErr != nil {
			return nil, sampleErr
		}
		state.pending = next
		responseBody, err = self.assignResponse(state)
	}
	if err != nil {
		return nil, err
	}
	state.lastResponse = responseBody
	return responseBody, nil
}

// newTestEngine wires an engine to the mock with a fixed seed provider.
func newTestEngine(t *testing.T, server *mockVerifyServer, validatorKey ed25519.PrivateKey, clientId connect.Id, m int, store *ProofStore) (*TrailEngine, *StatsEngine, connect.Id) {
	t.Helper()
	seedHop := server.providers[0]
	stats := NewStatsEngine(StatsConfig{AMin: 1})
	engine := NewTrailEngine(
		clientId,
		validatorKey,
		server,
		NewStaticServerKeyRing(server.serverPublicKeys()),
		func(ctx context.Context) (connect.Id, error) { return seedHop, nil },
		stats,
		store,
		func() uint64 { return 42 },
		TrailEngineConfig{M: m, StepTimeout: 2 * time.Second, ExtendAttempts: 3, Pace: time.Millisecond},
	)
	return engine, stats, seedHop
}

func TestTrailHappyPath(t *testing.T) {
	server, validatorKey, clientId := newMockVerifyServer(t, 12)
	store, err := NewProofStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, seedHop := newTestEngine(t, server, validatorKey, clientId, 5, store)

	record, err := engine.RunTrail(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.M != 5 || len(record.Hops) != 5 {
		t.Fatalf("depth: M=%d hops=%d", record.M, len(record.Hops))
	}
	if record.Hops[0].ClientId != seedHop {
		t.Fatal("first hop is not the seed")
	}
	if record.Epoch != 42 {
		t.Fatalf("epoch stamp: %d", record.Epoch)
	}
	if record.Coverage != 4 {
		t.Fatalf("coverage: %d, want M-1=4", record.Coverage)
	}

	// The record's FinalDigest is the RAW VerifyFinalDigest of the canonical
	// FINAL message.
	finalMessage, err := connect.BuildVerifyFinalMessage(
		record.ServerKeyId, record.TrailId, record.ServerNonce, record.Vpk, byte(record.M), record.Hops)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := connect.VerifyFinalDigest(finalMessage)
	if !bytes.Equal(record.FinalDigest, wantDigest[:]) {
		t.Fatal("final digest mismatch")
	}
	// Both leaf signatures verify over the EFFORT digest
	// sha256(finalDigest ‖ coverage) — the exact value the contract recomputes
	// and the 0x402 precompile checks, binding the server-attested coverage in
	// (review A2).
	effortDigest := connect.VerifyEffortDigest(wantDigest, record.Coverage)
	vpk := validatorKey.Public().(ed25519.PublicKey)
	if !ed25519.Verify(vpk, effortDigest[:], record.VpkSig) {
		t.Fatal("vpk co-signature does not verify over the effort digest")
	}
	if !ed25519.Verify(server.serverKey.Public().(ed25519.PublicKey), effortDigest[:], record.FinalSig) {
		t.Fatal("final_sig does not verify over the effort digest")
	}
	// Neither signature verifies over the BARE final digest anymore: coverage
	// is bound in, so a forged coverage would break both.
	if ed25519.Verify(vpk, record.FinalDigest, record.VpkSig) {
		t.Fatal("vpk co-signature must not verify over the bare final digest")
	}
	if ed25519.Verify(server.serverKey.Public().(ed25519.PublicKey), record.FinalDigest, record.FinalSig) {
		t.Fatal("final_sig must not verify over the bare final digest")
	}
	// pathId derivation.
	wantPathId := TrailPathId(record.TrailId, record.Vpk, record.ServerKeyId)
	if !bytes.Equal(record.PathId, wantPathId[:]) {
		t.Fatal("pathId mismatch")
	}

	// Stats: the seed hop is excluded (§7.6); each of the 4 assigned hops
	// has a=1, c=1.
	if a, c := stats.WindowCounts(seedHop); a != 0 || c != 0 {
		t.Fatalf("seed hop was recorded: a=%d c=%d", a, c)
	}
	assigned := 0
	for _, hop := range record.Hops[1:] {
		a, c := stats.WindowCounts(hop.ClientId)
		if a != 1 || c != 1 {
			t.Fatalf("hop %s: a=%d c=%d, want 1/1", hop.ClientId, a, c)
		}
		assigned++
	}
	if assigned != 4 {
		t.Fatalf("assigned hops: %d", assigned)
	}

	// Persisted.
	records, _, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].TrailId != record.TrailId {
		t.Fatalf("store: %d records", len(records))
	}
}

func TestTrailExtendIdempotentRetry(t *testing.T) {
	server, validatorKey, clientId := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientId, 4, nil)

	// Lose the first EXTEND response after the server processed it: the
	// engine must retry the same body and get the cached response (§4.3)
	// without the server double-advancing.
	server.dropNextExtendResponses = 1

	record, err := engine.RunTrail(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if server.cacheHits < 1 {
		t.Fatal("retry did not hit the idempotency cache")
	}
	// 3 EXTENDs processed exactly once each (depth 4 = seed + 3 extends).
	if server.extendCount != 3 {
		t.Fatalf("server processed %d EXTENDs, want 3", server.extendCount)
	}
	for _, hop := range record.Hops[1:] {
		if a, c := stats.WindowCounts(hop.ClientId); a != 1 || c != 1 {
			t.Fatalf("hop %s: a=%d c=%d after retry", hop.ClientId, a, c)
		}
	}
}

func TestTrailHopFailureAttribution(t *testing.T) {
	server, validatorKey, clientId := newMockVerifyServer(t, 3)
	// With exactly 3 providers and the seed fixed, M=3 uses both others.
	engine, stats, seedHop := newTestEngine(t, server, validatorKey, clientId, 4, nil)
	_ = seedHop

	// Make every non-seed provider unreachable — the first assigned hop
	// fails and the trail is abandoned with the failure attributed to it.
	for _, provider := range server.providers[1:] {
		server.failHops[provider] = true
	}
	engine.cfg.StepTimeout = 200 * time.Millisecond

	_, err := engine.RunTrail(context.Background())
	var trailErr *TrailError
	if !errors.As(err, &trailErr) {
		t.Fatalf("expected TrailError, got %v", err)
	}
	if trailErr.Kind != TrailErrorHop {
		t.Fatalf("kind %d, want hop failure", trailErr.Kind)
	}
	// Exposure recorded, no confirmation: the local §7.2 attribution.
	a, c := stats.WindowCounts(trailErr.Hop)
	if a != 1 || c != 0 {
		t.Fatalf("failed hop: a=%d c=%d, want 1/0", a, c)
	}
	// The seed is still never recorded.
	if a, c := stats.WindowCounts(server.providers[0]); a != 0 || c != 0 {
		t.Fatalf("seed recorded on failure path: a=%d c=%d", a, c)
	}
}

func TestTrailPoisonFinalUnknownOutcome(t *testing.T) {
	server, validatorKey, clientId := newMockVerifyServer(t, 12)
	store, err := NewProofStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, stats, _ := newTestEngine(t, server, validatorKey, clientId, 4, store)

	// The server carries the trail to depth M then returns a FINAL whose
	// signature is garbage — from the client side this is exactly what a
	// poisoned trail looks like (§9): same shapes, same timing, only the
	// proof fails to verify.
	server.poisonFinal = true

	_, err = engine.RunTrail(context.Background())
	var trailErr *TrailError
	if !errors.As(err, &trailErr) {
		t.Fatalf("expected TrailError, got %v", err)
	}
	if trailErr.Kind != TrailErrorUnknownOutcome {
		t.Fatalf("kind %d, want unknown outcome", trailErr.Kind)
	}
	// The final hop's confirmation is NOT recorded (cannot be
	// distinguished from fabrication)…
	if a, c := stats.WindowCounts(trailErr.Hop); a != 1 || c != 0 {
		t.Fatalf("final hop under poison: a=%d c=%d, want 1/0", a, c)
	}
	// …and nothing is persisted.
	records, _, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("poisoned trail persisted %d records", len(records))
	}
}

func TestTrailBadAssignSigRejected(t *testing.T) {
	server, validatorKey, clientId := newMockVerifyServer(t, 12)
	// Swap the published key so every assign_sig verification fails.
	_, wrongKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	stats := NewStatsEngine(StatsConfig{})
	engine := NewTrailEngine(
		clientId,
		validatorKey,
		server,
		NewStaticServerKeyRing(map[byte]ed25519.PublicKey{server.serverKeyId: wrongKey.Public().(ed25519.PublicKey)}),
		func(ctx context.Context) (connect.Id, error) { return server.providers[0], nil },
		stats,
		nil,
		nil,
		TrailEngineConfig{M: 4, StepTimeout: time.Second},
	)
	_, err = engine.RunTrail(context.Background())
	var trailErr *TrailError
	if !errors.As(err, &trailErr) || trailErr.Kind != TrailErrorProtocol {
		t.Fatalf("expected protocol error, got %v", err)
	}
	// Nothing was recorded — the bogus assignment never counts.
	if len(stats.Exposure()) != 0 {
		t.Fatal("stats recorded under a bad assign signature")
	}
}

func TestTrailSeedFailureNotAttributed(t *testing.T) {
	server, validatorKey, clientId := newMockVerifyServer(t, 12)
	engine, stats, seedHop := newTestEngine(t, server, validatorKey, clientId, 4, nil)
	server.failHops[seedHop] = true
	engine.cfg.StepTimeout = 200 * time.Millisecond

	_, err := engine.RunTrail(context.Background())
	var trailErr *TrailError
	if !errors.As(err, &trailErr) || trailErr.Kind != TrailErrorSeed {
		t.Fatalf("expected seed error, got %v", err)
	}
	if len(stats.Exposure()) != 0 {
		t.Fatal("seed failure polluted the stats")
	}
}

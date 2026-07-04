package validator

// trail.go — the trail engine (VALIDATOR.md §§3–6; PLAN.md §7.2): walk
// server-assigned chains of providers through per-hop egress-pinned tunnels,
// verifying every server signature and co-signing completed proofs.
//
// The engine is written against the small TrailTransport seam so the
// protocol logic is fully testable against an in-memory server
// (trail_test.go); the production implementation (transport.go) opens a
// real egress-pinned tunnel per hop.
//
// Signature seams (connect/verify_wire.go is the shared canonical encoder):
//   - SEED / EXTEND: Ed25519 by the validator key over the RAW canonical
//     message bytes.
//   - ASSIGN: Ed25519 by the server key over the RAW canonical message.
//   - FINAL: Ed25519 signatures over the EFFORT digest
//     VerifyEffortDigest(VerifyFinalDigest(finalMessage), coverage) =
//     sha256(finalDigest ‖ uint256_be(coverage)) — NOT the raw FINAL bytes and
//     NOT the bare final digest — because the on-chain 0x402 precompile that
//     decides effort-leaf disputes verifies only 32-byte messages AND the leaf
//     must bind the server-attested coverage so it cannot be forged (review
//     A2). The validator's vpk co-signature (VpkSig below) signs the SAME
//     effort digest. The leaf carries the RAW finalDigest and coverage;
//     (final_sig, vpk_sig, final_digest, coverage) are exactly the (serverSig,
//     vpkSig, finalDigest, coverage) fields of the on-chain TrailLeaf, and the
//     contract recomputes the effort digest from finalDigest+coverage to verify
//     both signatures.
//
// Stats semantics (VALIDATOR.md §7):
//   - exposure recorded when an ASSIGN names the pending hop (§7.2),
//   - confirmation + latency recorded per step at the moment the response
//     that confirms the hop arrives (§7.5),
//   - the validator-chosen seed hop is never recorded (§7.6),
//   - a step that exhausts its idempotent retries within StepTimeout leaves
//     the exposure unconfirmed — the local failure attribution to the
//     pending hop (§4.4),
//   - a FINAL whose proof does not verify is an UNKNOWN outcome (poison
//     path, §9): the last hop's confirmation is not recorded (it cannot be
//     distinguished from a fabricated response) and no proof is persisted.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urnetwork/connect"
)

// TrailTransport posts one signed /verify body through the given provider
// hop's egress and returns the raw response body. Implementations must make
// the request egress from exactly that hop (the server attributes the hop
// from the source IP). ctx bounds the whole attempt.
type TrailTransport interface {
	PostVerify(ctx context.Context, hop connect.Id, jsonBody []byte) ([]byte, error)
}

// SeedPicker picks the validator-chosen entry provider for a new trail
// (§4.1). The production picker samples FindProviders2.
type SeedPicker func(ctx context.Context) (connect.Id, error)

// ServerKeyRing caches the published server Ed25519 verify keys
// (GET /verify/keys) by 1-byte server_key_id, refreshing once on a miss.
type ServerKeyRing struct {
	mu    sync.Mutex
	keys  map[byte]ed25519.PublicKey
	fetch func() (map[byte]ed25519.PublicKey, error)
}

func NewServerKeyRing(fetch func() (map[byte]ed25519.PublicKey, error)) *ServerKeyRing {
	return &ServerKeyRing{
		keys:  map[byte]ed25519.PublicKey{},
		fetch: fetch,
	}
}

// NewStaticServerKeyRing pins a fixed key set (tests, offline verification).
func NewStaticServerKeyRing(keys map[byte]ed25519.PublicKey) *ServerKeyRing {
	cp := map[byte]ed25519.PublicKey{}
	for id, k := range keys {
		cp[id] = k
	}
	return &ServerKeyRing{keys: cp}
}

// Key returns the published key for id, fetching once when unknown.
func (self *ServerKeyRing) Key(id byte) (ed25519.PublicKey, error) {
	self.mu.Lock()
	defer self.mu.Unlock()
	if k, ok := self.keys[id]; ok {
		return k, nil
	}
	if self.fetch == nil {
		return nil, fmt.Errorf("unknown server key id %d", id)
	}
	keys, err := self.fetch()
	if err != nil {
		return nil, fmt.Errorf("verify keys fetch: %w", err)
	}
	for kid, k := range keys {
		self.keys[kid] = k
	}
	if k, ok := self.keys[id]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("server key id %d is not published", id)
}

// ProofRecord is one completed, locally verified trail persisted as a JSONL
// line in <state_dir>/proofs.jsonl. It carries everything the effort-bounty
// phase's submit-trails flow (deferred — WHITEPAPER §9.3, D23; implementation
// parked at docs/parked/) needs to rebuild the epoch's 9-field effort leaves
// (PathId, Coverage, ServerKeyId, FinalDigest, FinalSig, VpkSig) plus the
// full published proof for audits/disputes.
type ProofRecord struct {
	Version int `json:"v"`
	// Epoch is the contract epoch open at completion time (0 = unknown —
	// recorded only when the run has chain access).
	Epoch uint64 `json:"epoch"`

	TrailId     connect.Id               `json:"trail_id"`
	ServerNonce []byte                   `json:"server_nonce"`
	Vpk         []byte                   `json:"vpk"`
	M           int                      `json:"m"`
	Hops        []connect.VerifyProofHop `json:"hops"`
	ServerKeyId byte                     `json:"server_key_id"`
	FinalSig    []byte                   `json:"final_sig"`
	VerifierSig []byte                   `json:"verifier_sig"`

	// FinalDigest = VerifyFinalDigest(canonical FINAL message) — the RAW final
	// digest. Both leaf signatures are over the EFFORT digest
	// VerifyEffortDigest(FinalDigest, Coverage): FinalSig is the server's and
	// VpkSig is the validator's Ed25519 co-signature. The contract recomputes
	// the effort digest from FinalDigest + Coverage to verify them (review A2).
	FinalDigest []byte `json:"final_digest"`
	VpkSig      []byte `json:"vpk_sig"`

	// Coverage is the SERVER-ATTESTED coverage taken from proof.Coverage and
	// bound into both leaf signatures via the effort digest (review A2). The v1
	// formula is M − 1 server-assigned completed hops (seed excluded, §7.6);
	// acceptFinal warns if the signed value disagrees but keeps what the server
	// signed rather than recomputing it locally.
	Coverage uint64 `json:"coverage"`
	// PathId = keccak256(trail_id(16) ‖ vpk(32) ‖ server_key_id(1))
	// (WHITEPAPER §9.1).
	PathId []byte `json:"path_id"`

	CompleteTimeMs uint64 `json:"complete_time_ms"`
}

// TrailPathId derives the whitepaper §9.1 path identity:
//
//	pathId = keccak256(trail_id ‖ vpk ‖ server_key_id)
//
// with each component at its wire width: trail_id 16 bytes, vpk 32 bytes,
// server_key_id 1 byte — 49 bytes total.
func TrailPathId(trailId connect.Id, vpk []byte, serverKeyId byte) [32]byte {
	return keccak256(trailId[:], vpk, []byte{serverKeyId})
}

// ProofStore is an append-only JSONL store of completed proofs.
type ProofStore struct {
	mu   sync.Mutex
	path string
}

func NewProofStore(dir string) (*ProofStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &ProofStore{path: filepath.Join(dir, "proofs.jsonl")}, nil
}

func (self *ProofStore) Append(record *ProofRecord) error {
	self.mu.Lock()
	defer self.mu.Unlock()
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(self.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// Load reads every parseable record. Unparseable lines (torn writes) are
// skipped with a count.
func (self *ProofStore) Load() ([]*ProofRecord, int, error) {
	self.mu.Lock()
	defer self.mu.Unlock()
	b, err := os.ReadFile(self.path)
	if os.IsNotExist(err) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var records []*ProofRecord
	skipped := 0
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec ProofRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			skipped++
			continue
		}
		records = append(records, &rec)
	}
	return records, skipped, nil
}

// --- wire response parsing ---

// verifyResponseEnvelope sniffs whether a /verify response is the FINAL
// shape (status == "complete") before decoding the full type.
type verifyResponseEnvelope struct {
	Status string `json:"status"`
}

// TrailErrorKind classifies a failed trail for pacing / diagnostics.
type TrailErrorKind int

const (
	// TrailErrorSeed — the validator-chosen seed hop failed; nothing is
	// attributable (§7.6 — the seed is not a measurement).
	TrailErrorSeed TrailErrorKind = iota
	// TrailErrorHop — a server-assigned pending hop failed; the exposure
	// stands unconfirmed (local §7.2 attribution).
	TrailErrorHop
	// TrailErrorProtocol — the server violated the protocol (bad signature,
	// inconsistent trail echo). Nothing recorded.
	TrailErrorProtocol
	// TrailErrorUnknownOutcome — a FINAL arrived but its proof does not
	// verify (poison-indistinguishable, §9). Nothing recorded for the last
	// hop; no proof persisted.
	TrailErrorUnknownOutcome
)

// TrailError is a classified trail failure.
type TrailError struct {
	Kind TrailErrorKind
	Hop  connect.Id // the implicated hop for TrailErrorHop
	Err  error
}

func (self *TrailError) Error() string {
	return fmt.Sprintf("trail error (kind %d, hop %s): %v", self.Kind, self.Hop, self.Err)
}

func (self *TrailError) Unwrap() error {
	return self.Err
}

// TrailEngineConfig tunes the engine. Zero values take defaults.
type TrailEngineConfig struct {
	// M is the requested trail depth (server clamps to [MMin, MMax]).
	M int
	// StepTimeout bounds one hop step end to end (VALIDATOR.md §5.5 T);
	// within it the same EXTEND body is retried idempotently (§4.3).
	StepTimeout time.Duration
	// ExtendAttempts is the max sends of one EXTEND body within its step.
	ExtendAttempts int
	// Pace is the sleep between consecutive trails per worker.
	Pace time.Duration
}

func (self TrailEngineConfig) withDefaults() TrailEngineConfig {
	if self.M == 0 {
		self.M = connect.VerifyMDefault
	}
	if self.StepTimeout == 0 {
		self.StepTimeout = 30 * time.Second
	}
	if self.ExtendAttempts == 0 {
		self.ExtendAttempts = 3
	}
	if self.Pace == 0 {
		self.Pace = 2 * time.Second
	}
	return self
}

// TrailEngine walks trails. Construct with NewTrailEngine; run one trail
// with RunTrail or a bounded-concurrency loop with Run.
type TrailEngine struct {
	clientId  connect.Id
	vsk       ed25519.PrivateKey
	vpk       ed25519.PublicKey
	transport TrailTransport
	keys      *ServerKeyRing
	pickSeed  SeedPicker
	stats     *StatsEngine
	store     *ProofStore
	// epochFn returns the current contract epoch for proof stamping
	// (nil / 0 = unknown).
	epochFn func() uint64
	cfg     TrailEngineConfig

	completed atomic.Uint64
	failed    atomic.Uint64
}

func NewTrailEngine(
	clientId connect.Id,
	vsk ed25519.PrivateKey,
	transport TrailTransport,
	keys *ServerKeyRing,
	pickSeed SeedPicker,
	stats *StatsEngine,
	store *ProofStore,
	epochFn func() uint64,
	cfg TrailEngineConfig,
) *TrailEngine {
	return &TrailEngine{
		clientId:  clientId,
		vsk:       vsk,
		vpk:       vsk.Public().(ed25519.PublicKey),
		transport: transport,
		keys:      keys,
		pickSeed:  pickSeed,
		stats:     stats,
		store:     store,
		epochFn:   epochFn,
		cfg:       cfg.withDefaults(),
	}
}

// Completed / Failed report engine counters.
func (self *TrailEngine) Completed() uint64 { return self.completed.Load() }
func (self *TrailEngine) Failed() uint64    { return self.failed.Load() }

// verifyAssign checks one ASSIGN response: internal consistency with the
// walked trail and the server signature over the canonical ASSIGN message.
func (self *TrailEngine) verifyAssign(assign *connect.VerifyAssignResult, wantTrail []connect.Id) error {
	if len(assign.ServerNonce) != connect.VerifyNonceSize {
		return fmt.Errorf("assign server_nonce is %d bytes", len(assign.ServerNonce))
	}
	if assign.M < connect.VerifyMMin || connect.VerifyMMax < assign.M {
		return fmt.Errorf("assign M=%d outside [%d, %d]", assign.M, connect.VerifyMMin, connect.VerifyMMax)
	}
	if len(assign.Trail) != len(wantTrail) {
		return fmt.Errorf("assign echoes %d confirmed hops, expected %d", len(assign.Trail), len(wantTrail))
	}
	for i := range wantTrail {
		if assign.Trail[i] != wantTrail[i] {
			return fmt.Errorf("assign rewrites confirmed hop %d", i)
		}
	}
	// The newly assigned hop must be fresh: not already in the trail and
	// not the validator itself (§5.1 sampling exclusions).
	for _, hop := range assign.Trail {
		if assign.NextHop == hop {
			return fmt.Errorf("assign repeats hop %s", assign.NextHop)
		}
	}
	if assign.NextHop == self.clientId {
		return fmt.Errorf("assign names the validator itself")
	}
	serverKey, err := self.keys.Key(assign.ServerKeyId)
	if err != nil {
		return err
	}
	signed := append(append([]connect.Id{}, assign.Trail...), assign.NextHop)
	assignMessage, err := connect.BuildVerifyAssignMessage(
		assign.ServerKeyId, assign.TrailId, assign.ServerNonce, self.vpk, byte(assign.M), signed)
	if err != nil {
		return err
	}
	if !connect.VerifyVerifyMessageSignature(serverKey, assignMessage, assign.AssignSig) {
		return fmt.Errorf("assign signature does not verify under server key %d", assign.ServerKeyId)
	}
	return nil
}

// postStep sends one body through hop with the step timeout, retrying the
// identical body (idempotent, §4.3) up to ExtendAttempts within the budget.
func (self *TrailEngine) postStep(ctx context.Context, hop connect.Id, body []byte) ([]byte, error) {
	stepCtx, cancel := context.WithTimeout(ctx, self.cfg.StepTimeout)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < self.cfg.ExtendAttempts; attempt++ {
		responseBody, err := self.transport.PostVerify(stepCtx, hop, body)
		if err == nil {
			return responseBody, nil
		}
		lastErr = err
		select {
		case <-stepCtx.Done():
			return nil, fmt.Errorf("step timeout after %d attempts: %w", attempt+1, lastErr)
		default:
		}
	}
	return nil, fmt.Errorf("step failed after %d attempts: %w", self.cfg.ExtendAttempts, lastErr)
}

// RunTrail walks one full trail: SEED through a picked entry, then EXTEND
// through each server-assigned hop, then verify + co-sign the FINAL proof
// and persist it. Returns the persisted record or a *TrailError.
func (self *TrailEngine) RunTrail(ctx context.Context) (*ProofRecord, error) {
	seedHop, err := self.pickSeed(ctx)
	if err != nil {
		return nil, &TrailError{Kind: TrailErrorSeed, Err: fmt.Errorf("seed pick: %w", err)}
	}

	// --- SEED (§4.1) ---
	clientNonce := make([]byte, connect.VerifyNonceSize)
	if _, err := rand.Read(clientNonce); err != nil {
		return nil, &TrailError{Kind: TrailErrorSeed, Err: err}
	}
	seedMessage, err := connect.BuildVerifySeedMessage(self.vpk, clientNonce, byte(self.cfg.M))
	if err != nil {
		return nil, &TrailError{Kind: TrailErrorSeed, Err: err}
	}
	seedBody, err := json.Marshal(&connect.VerifySeedArgs{
		ClientId:    self.clientId,
		Vpk:         self.vpk,
		ClientNonce: clientNonce,
		SeedSig:     connect.SignVerifyMessage(self.vsk, seedMessage),
		M:           self.cfg.M,
	})
	if err != nil {
		return nil, &TrailError{Kind: TrailErrorSeed, Err: err}
	}
	responseBody, err := self.postStep(ctx, seedHop, seedBody)
	if err != nil {
		// Seed failures are the validator's own pick — not attributable.
		return nil, &TrailError{Kind: TrailErrorSeed, Hop: seedHop, Err: err}
	}
	var envelope verifyResponseEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return nil, &TrailError{Kind: TrailErrorProtocol, Err: fmt.Errorf("seed response: %w", err)}
	}
	if envelope.Status == connect.VerifyStatusComplete {
		return nil, &TrailError{Kind: TrailErrorProtocol, Err: fmt.Errorf("server finalized at depth 1")}
	}
	var assign connect.VerifyAssignResult
	if err := json.Unmarshal(responseBody, &assign); err != nil {
		return nil, &TrailError{Kind: TrailErrorProtocol, Err: fmt.Errorf("seed response: %w", err)}
	}
	confirmed := []connect.Id{seedHop}
	if err := self.verifyAssign(&assign, confirmed); err != nil {
		return nil, &TrailError{Kind: TrailErrorProtocol, Err: err}
	}
	trailId := assign.TrailId
	serverNonce := assign.ServerNonce
	m := assign.M

	// The first server-assigned exposure (§7.2). The seed hop is never
	// recorded (§7.6).
	self.stats.RecordAssignment(assign.NextHop)

	// --- EXTEND loop (§4.2) ---
	for depth := 2; depth <= m; depth++ {
		pendingHop := assign.NextHop
		trail := append(append([]connect.Id{}, confirmed...), pendingHop)
		extendMessage, err := connect.BuildVerifyExtendMessage(trailId, serverNonce, self.vpk, byte(m), trail)
		if err != nil {
			return nil, &TrailError{Kind: TrailErrorProtocol, Err: err}
		}
		extendSig := connect.SignVerifyMessage(self.vsk, extendMessage)
		extendBody, err := json.Marshal(&connect.VerifyExtendArgs{
			ClientId:  self.clientId,
			TrailId:   trailId,
			Trail:     trail,
			ExtendSig: extendSig,
		})
		if err != nil {
			return nil, &TrailError{Kind: TrailErrorProtocol, Err: err}
		}

		stepStart := time.Now()
		responseBody, err := self.postStep(ctx, pendingHop, extendBody)
		if err != nil {
			// The assigned hop never confirmed: local failure attribution
			// to the pending hop (§4.4/§7.2) — the exposure recorded at
			// ASSIGN time stands unconfirmed.
			return nil, &TrailError{Kind: TrailErrorHop, Hop: pendingHop, Err: err}
		}
		latencyMs := float64(time.Since(stepStart)) / float64(time.Millisecond)

		if err := json.Unmarshal(responseBody, &envelope); err != nil {
			return nil, &TrailError{Kind: TrailErrorProtocol, Err: err}
		}

		if envelope.Status == connect.VerifyStatusComplete {
			if depth != m {
				return nil, &TrailError{Kind: TrailErrorProtocol, Err: fmt.Errorf("server finalized at depth %d, expected %d", depth, m)}
			}
			var final connect.VerifyFinalResult
			if err := json.Unmarshal(responseBody, &final); err != nil {
				return nil, &TrailError{Kind: TrailErrorProtocol, Err: err}
			}
			record, err := self.acceptFinal(final.Proof, trailId, serverNonce, m, trail, extendSig)
			if err != nil {
				// Unknown outcome (§9): a poisoned or forged FINAL is
				// indistinguishable from the real thing except by its
				// signatures. Do not record the last hop, do not persist.
				return nil, &TrailError{Kind: TrailErrorUnknownOutcome, Hop: pendingHop, Err: err}
			}
			// The last hop's confirmation is only known genuine via the
			// verified FINAL.
			self.stats.RecordConfirmation(pendingHop, latencyMs)
			// Each server-assigned hop's egress-IP-hash for the head routable-IP
			// score (§8.4/§11.1, D27) — from the VERIFIED proof only, seed
			// (hop 0) excluded (§7.6), symmetric with the confirmation stats.
			for i := 1; i < len(record.Hops); i++ {
				self.stats.RecordEgressHash(record.Hops[i].ClientId, record.Hops[i].EgressIpHash)
			}
			if self.store != nil {
				if err := self.store.Append(record); err != nil {
					return record, fmt.Errorf("proof persist: %w", err)
				}
			}
			return record, nil
		}

		var nextAssign connect.VerifyAssignResult
		if err := json.Unmarshal(responseBody, &nextAssign); err != nil {
			return nil, &TrailError{Kind: TrailErrorProtocol, Err: err}
		}
		if nextAssign.TrailId != trailId || !bytes.Equal(nextAssign.ServerNonce, serverNonce) || nextAssign.M != m {
			return nil, &TrailError{Kind: TrailErrorProtocol, Err: fmt.Errorf("assign switched trail identity")}
		}
		// The response confirms the pending hop: echoed confirmed hops must
		// now be the previous trail including it.
		if err := self.verifyAssign(&nextAssign, trail); err != nil {
			return nil, &TrailError{Kind: TrailErrorProtocol, Err: err}
		}
		self.stats.RecordConfirmation(pendingHop, latencyMs)
		self.stats.RecordAssignment(nextAssign.NextHop)
		confirmed = trail
		assign = nextAssign
	}
	return nil, &TrailError{Kind: TrailErrorProtocol, Err: fmt.Errorf("server never finalized at depth %d", m)}
}

// acceptFinal verifies a FINAL proof end to end and builds the ProofRecord:
//   - header matches the walked trail identity,
//   - hop ids equal the walked trail, times monotone nondecreasing,
//   - final_sig verifies under the PUBLISHED server key for server_key_id
//     over the EFFORT digest VerifyEffortDigest(VerifyFinalDigest(FINAL
//     message), proof.Coverage) — the server-attested coverage is bound in,
//   - verifier_sig verifies under our vpk over the depth-M EXTEND message
//     (it should byte-equal our own signature),
//
// then co-signs the SAME effort digest with the vpk. The record carries the
// RAW finalDigest and the server-attested coverage; the contract recomputes
// the effort digest from them to check both leaf signatures.
func (self *TrailEngine) acceptFinal(
	proof *connect.VerifyProof,
	trailId connect.Id,
	serverNonce []byte,
	m int,
	walkedTrail []connect.Id,
	sentExtendSig []byte,
) (*ProofRecord, error) {
	if proof == nil {
		return nil, fmt.Errorf("complete response carries no proof")
	}
	h := proof.Header
	if h.TrailId != trailId {
		return nil, fmt.Errorf("proof trail_id mismatch")
	}
	if !bytes.Equal(h.ServerNonce, serverNonce) {
		return nil, fmt.Errorf("proof server_nonce mismatch")
	}
	if !bytes.Equal(h.Vpk, self.vpk) {
		return nil, fmt.Errorf("proof vpk mismatch")
	}
	if h.M != m || len(proof.Hops) != m || len(walkedTrail) != m {
		return nil, fmt.Errorf("proof depth mismatch: header M=%d hops=%d walked=%d", h.M, len(proof.Hops), len(walkedTrail))
	}
	for i, hop := range proof.Hops {
		if hop.ClientId != walkedTrail[i] {
			return nil, fmt.Errorf("proof hop %d is not the walked hop", i)
		}
		if 0 < i && hop.TimeMs < proof.Hops[i-1].TimeMs {
			return nil, fmt.Errorf("proof hop times not monotone at %d", i)
		}
	}

	serverKey, err := self.keys.Key(proof.ServerKeyId)
	if err != nil {
		return nil, err
	}
	finalMessage, err := connect.BuildVerifyFinalMessage(
		proof.ServerKeyId, trailId, serverNonce, self.vpk, byte(m), proof.Hops)
	if err != nil {
		return nil, err
	}
	finalDigest := connect.VerifyFinalDigest(finalMessage)

	// Coverage is SERVER-ATTESTED: the server binds it into final_sig via the
	// effort digest, so the leaf commits a value the validator cannot forge
	// (review A2). Trust proof.Coverage rather than recomputing it; v1 should
	// always equal M−1 (server-assigned completed hops, seed excluded, §7.6) —
	// flag a disagreement but keep the signed value.
	coverage := proof.Coverage
	if coverage != uint64(m-1) {
		fmt.Printf("warning: server-attested coverage %d != M-1=%d for trail %s (using the signed value)\n", coverage, m-1, trailId)
	}

	// CRITICAL SEAM: final_sig signs the 32-byte EFFORT digest
	// sha256(finalDigest ‖ coverage), not the FINAL digest alone — the exact
	// value the 0x402 precompile checks in effort-leaf disputes, binding
	// coverage into the signature (review A2).
	effortDigest := connect.VerifyEffortDigest(finalDigest, coverage)
	if !ed25519.Verify(serverKey, effortDigest[:], proof.FinalSig) {
		return nil, fmt.Errorf("final_sig does not verify over the effort digest under server key %d", proof.ServerKeyId)
	}

	// verifier_sig is the depth-M EXTEND signature (§3.3) — ours.
	extendMessage, err := connect.BuildVerifyExtendMessage(trailId, serverNonce, self.vpk, byte(m), walkedTrail)
	if err != nil {
		return nil, err
	}
	if !connect.VerifyVerifyMessageSignature(self.vpk, extendMessage, proof.VerifierSig) {
		return nil, fmt.Errorf("verifier_sig does not verify under our vpk")
	}
	if !bytes.Equal(proof.VerifierSig, sentExtendSig) {
		// Not fatal (it verified), but flag the anomaly.
		fmt.Printf("warning: proof verifier_sig differs from the signature we sent for trail %s\n", trailId)
	}

	// Co-sign the EFFORT digest: this is the on-chain TrailLeaf vpkSig, and it
	// too attests coverage — both leaf signatures are over the effort digest.
	vpkSig := ed25519.Sign(self.vsk, effortDigest[:])

	epoch := uint64(0)
	if self.epochFn != nil {
		epoch = self.epochFn()
	}
	record := &ProofRecord{
		Version:        1,
		Epoch:          epoch,
		TrailId:        trailId,
		ServerNonce:    append([]byte{}, serverNonce...),
		Vpk:            append([]byte{}, self.vpk...),
		M:              m,
		Hops:           proof.Hops,
		ServerKeyId:    proof.ServerKeyId,
		FinalSig:       append([]byte{}, proof.FinalSig...),
		VerifierSig:    append([]byte{}, proof.VerifierSig...),
		FinalDigest:    finalDigest[:], // RAW final digest; contract derives the effort digest from this + coverage
		VpkSig:         vpkSig,
		Coverage:       coverage, // server-attested; bound into both leaf signatures
		CompleteTimeMs: proof.Hops[m-1].TimeMs,
	}
	pathId := TrailPathId(trailId, self.vpk, proof.ServerKeyId)
	record.PathId = pathId[:]
	return record, nil
}

// Run walks trails continuously with bounded concurrency until ctx is done.
func (self *TrailEngine) Run(ctx context.Context, concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				record, err := self.RunTrail(ctx)
				if err != nil {
					self.failed.Add(1)
					var trailErr *TrailError
					if errors.As(err, &trailErr) {
						fmt.Printf("[trail %d] %v\n", worker, trailErr)
					} else {
						fmt.Printf("[trail %d] error: %v\n", worker, err)
					}
				} else {
					self.completed.Add(1)
					fmt.Printf("[trail %d] completed trail %s depth %d (epoch %d, %d total)\n",
						worker, record.TrailId, record.M, record.Epoch, self.completed.Load())
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(self.cfg.Pace):
				}
			}
		}(i)
	}
	wg.Wait()
}

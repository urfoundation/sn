// Synthetic signed assignments reproduce disabled-provider selection and
// mid-walk assignment changes without changing response-integrity contracts.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// Owns deterministic role keys and a signed-response factory for one operator.
func newVerifyProviderFaultActor(t *testing.T) (*verifyAdversary, func([]connect.Id, connect.Id) []byte) {
	t.Helper()
	validatorPrivate := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	serverSeed := make([]byte, ed25519.SeedSize)
	serverSeed[0] = 1
	serverPrivate := ed25519.NewKeyFromSeed(serverSeed)
	validatorPublic := validatorPrivate.Public().(ed25519.PublicKey)
	keys, err := json.Marshal(map[string]any{"keys": []map[string]any{{"server_key_id": 7, "public_key": serverPrivate.Public().(ed25519.PublicKey)}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := testResolvedConfig(t)
	cfg.Policy.Verify.TrailDepth = connect.VerifyMMin
	actor := &verifyAdversary{
		cfg: cfg, faults: newAdversaryFaultWindow(time.Second),
		validators:         map[int]verifyAdversaryIdentity{1: {clientID: connect.Id{9}, private: validatorPrivate, public: validatorPublic}},
		seedProviders:      map[int][]connect.Id{1: {{1}, {2}, {3}}},
		providerSources:    map[int]map[connect.Id]string{1: {{1}: "127.90.0.1", {2}: "127.90.0.2", {3}: "127.90.0.3"}},
		providerTargetKVs:  map[connect.Id][]string{{1}: {"miner-1", "miner-swarm-1"}, {2}: {"miner-2", "miner-swarm-1"}, {3}: {"miner-3", "miner-swarm-1"}},
		lastRealAssignSize: map[int]int{},
		http: adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
			return adversaryGetTestResponse(http.StatusOK, string(keys)), nil
		}),
	}
	assign := func(trail []connect.Id, next connect.Id) []byte {
		trailId := connect.Id{8}
		nonce := make([]byte, connect.VerifyNonceSize)
		path := append(append([]connect.Id(nil), trail...), next)
		message, err := connect.BuildVerifyAssignMessage(7, trailId, nonce, validatorPublic, connect.VerifyMMin, path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(connect.VerifyAssignResult{TrailId: trailId, ServerNonce: nonce, Trail: trail, NextHop: next, M: connect.VerifyMMin, ServerKeyId: 7, AssignSig: ed25519.Sign(serverPrivate, message)})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	return actor, assign
}

// The old rotation demanded miner-1's source during its deliberate disable.
// The real signed walk now selects miner-2, then skips a disabled next hop before
// any EXTEND; this is neither a successful sample nor a waived response error.
func TestVerifyProviderFaultSelectionUsesUnaffectedSeed(t *testing.T) {
	actor, assign := newVerifyProviderFaultActor(t)
	actor.faults.Update([]string{"miner-1", "miner-3"})
	keysTransport := actor.http.transportForTest
	posts := 0
	actor.http.transportForTest = adversaryGetTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet {
			return keysTransport.RoundTrip(request)
		}
		posts++
		return adversaryGetTestResponse(http.StatusOK, string(assign([]connect.Id{{2}}, connect.Id{3}))), nil
	})
	result := actor.Sample(t.Context(), adversaryControlPhase, 0)
	if result.Outcome != adversaryOutcomeSkipped || result.Requests != 2 || posts != 1 || !strings.Contains(result.Detail, "target=miner-3") || len(result.Metrics) != 0 {
		t.Fatalf("disabled seed or next hop consumed a valid sample: %+v posts=%d", result, posts)
	}
}

// A signed later assignment may name an already faulted provider. Signature
// validation precedes route selection; corrupting that same assignment is hard.
func TestVerifyProviderFaultMidWalkAssignmentStaysStrict(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		actor, assign := newVerifyProviderFaultActor(t)
		actor.faults.Update([]string{"miner-3"})
		keysTransport := actor.http.transportForTest
		posts := 0
		actor.http.transportForTest = adversaryGetTestTransport(func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodGet {
				return keysTransport.RoundTrip(request)
			}
			posts++
			body := assign([]connect.Id{{1}}, connect.Id{2})
			if posts == 2 {
				body = assign([]connect.Id{{1}, {2}}, connect.Id{3})
				if corrupt {
					var value connect.VerifyAssignResult
					if err := json.Unmarshal(body, &value); err != nil {
						t.Fatal(err)
					}
					value.AssignSig[0] ^= 1
					body, _ = json.Marshal(value)
				}
			}
			return adversaryGetTestResponse(http.StatusOK, string(body)), nil
		})
		result := actor.Sample(t.Context(), adversaryControlPhase, 0)
		if result.Requests != 3 || posts != 2 {
			t.Fatalf("corrupt=%t disabled next hop was requested: %+v posts=%d", corrupt, result, posts)
		}
		if corrupt && (result.Outcome != adversaryOutcomeError || !strings.Contains(result.Detail, "signature is invalid")) || !corrupt && result.Outcome != adversaryOutcomeSkipped {
			t.Fatalf("corrupt=%t changed integrity accounting: %+v", corrupt, result)
		}
	}
}

// Exact target membership includes swarm ownership and only existing bounded
// restoration grace. Unrelated faults and an expired grace cannot change route.
func TestVerifyProviderFaultSelectionBoundedScope(t *testing.T) {
	actor, _ := newVerifyProviderFaultActor(t)
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	actor.faults.now = func() time.Time { return at }
	actor.faults.Update([]string{"miner-99"})
	if provider, err := actor.unaffectedSeedProvider(1, 0); err != nil || provider != (connect.Id{1}) {
		t.Fatalf("unrelated target changed rotation: %s %v", provider, err)
	}
	actor.faults.Update([]string{"miner-1"})
	actor.faults.Update(nil)
	if provider, err := actor.unaffectedSeedProvider(1, 0); err != nil || provider != (connect.Id{2}) {
		t.Fatalf("bounded restoration grace lost: %s %v", provider, err)
	}
	at = at.Add(2 * time.Second)
	if provider, err := actor.unaffectedSeedProvider(1, 0); err != nil || provider != (connect.Id{1}) {
		t.Fatalf("expired grace still changed rotation: %s %v", provider, err)
	}
	actor.faults.Update([]string{"miner-swarm-1"})
	result := actor.Sample(t.Context(), adversaryControlPhase, 0)
	if result.Outcome != adversaryOutcomeSkipped || result.Requests != 0 || len(result.Metrics) != 0 {
		t.Fatalf("exhausted routes claimed a completed sample: %+v", result)
	}
	joined := errors.Join(&adversaryVerifyRouteUnavailable{stage: "SEED"}, errors.New("invalid signature"))
	if actor.sampleError(1, joined, 2, 1).Outcome != adversaryOutcomeError {
		t.Fatal("joined semantic failure became a skipped route")
	}
}

// Explicit barriers force a fault update to arrive during a request. The
// publication waits for cancellation to join that bounded walk, then the next
// walk observes the target before the scheduler may disable it.
func TestVerifyProviderFaultUpdateDrainsBoundedWalk(t *testing.T) {
	actor, _ := newVerifyProviderFaultActor(t)
	requestReady, updateWaiting, updateDone, walkDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan error, 1)
	actor.faults.beforeWalkWaitForTest = func() { close(updateWaiting) }
	actor.http.transportForTest = adversaryGetTestTransport(func(request *http.Request) (*http.Response, error) {
		close(requestReady)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _, _, _, err := actor.walk(ctx, 1, 0, false); walkDone <- err }()
	<-requestReady
	go func() { actor.faults.Update([]string{"miner-1"}); close(updateDone) }()
	<-updateWaiting
	if actor.faults.Expected("miner-1") {
		t.Fatal("fault intent crossed an active signed walk")
	}
	cancel()
	if err := <-walkDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("walk cancellation lost: %v", err)
	}
	<-updateDone
	if !actor.faults.Expected("miner-1") {
		t.Fatal("fault intent was not published after the walk joined")
	}
	if provider, err := actor.unaffectedSeedProvider(1, 0); err != nil || provider != (connect.Id{2}) {
		t.Fatalf("next walk reused disabled target: %s %v", provider, err)
	}
}

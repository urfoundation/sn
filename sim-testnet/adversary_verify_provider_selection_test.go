// Production role membership must exclude exactly the routes disabled by a
// scheduled fault before issuing a valid walk, while retaining signed checks.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// Exercise the constructor and the real signed seed path with synthetic roles.
// The same cases fail on the old rotation with a wrong source hop, before the
// signed unavailable next hop can be recognized and skipped without an extend.
func TestVerifyProviderFaultSelectionProductionMembership(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Policy.Verify.TrailDepth = connect.VerifyMMin
	validatorSeed := make([]byte, ed25519.SeedSize)
	validatorSeed[0] = 61
	validatorPrivate := ed25519.NewKeyFromSeed(validatorSeed)
	validatorPublic := validatorPrivate.Public().(ed25519.PublicKey)
	roles := &RoleSecrets{Clients: map[string]ClientRoleSecret{}}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		label := fmt.Sprintf("validator-2-no-%d", operator)
		roles.Clients[label] = ClientRoleSecret{ClientIDHex: fmt.Sprintf("%032x", cfg.Config.Topology.Miners+operator), SeedHex: hex.EncodeToString(validatorSeed), PublicKeyHex: hex.EncodeToString(validatorPublic)}
	}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		roles.Clients[fmt.Sprintf("miner-%d", miner)] = ClientRoleSecret{ClientIDHex: fmt.Sprintf("%032x", miner)}
	}
	serverSeed := make([]byte, ed25519.SeedSize)
	serverSeed[0] = 62
	serverPrivate := ed25519.NewKeyFromSeed(serverSeed)
	keys, err := json.Marshal(map[string]any{"keys": []map[string]any{{"server_key_id": 7, "public_key": serverPrivate.Public().(ed25519.PublicKey)}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name         string
		miner        int
		healthyMiner int
		target       string
	}{
		{name: "target lifecycle", miner: fleetMemberMinerIndex(cfg, fleetLifecycleTargetFleet, 1), healthyMiner: fleetMemberMinerIndex(cfg, fleetLifecycleTargetFleet+cfg.Config.Topology.Operators, 1), target: lifecycleValidatorViewFaultTarget(1)},
		{name: "companion lifecycle", miner: fleetMemberMinerIndex(cfg, fleetLifecycleCompanionFleet, 1), healthyMiner: fleetMemberMinerIndex(cfg, fleetLifecycleCompanionFleet+cfg.Config.Topology.Operators, 1), target: lifecycleValidatorViewFaultTarget(2)},
		{name: "disabled member", miner: 1, healthyMiner: 2, target: "miner-1"},
		{name: "restarting swarm", miner: 1, healthyMiner: 105, target: "miner-swarm-1"},
	} {
		operator := operatorForMiner(cfg, test.miner)
		if operatorForMiner(cfg, test.healthyMiner) != operator {
			t.Fatalf("%s fixture crossed operators", test.name)
		}
		provider := func(miner int) connect.Id {
			encoded, err := hex.DecodeString(roles.Clients[fmt.Sprintf("miner-%d", miner)].ClientIDHex)
			if err != nil {
				t.Fatal(err)
			}
			id, err := connect.IdFromBytes(encoded)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
		excluded, healthy := provider(test.miner), provider(test.healthyMiner)
		trailId, nonce := connect.Id{81}, make([]byte, connect.VerifyNonceSize)
		message, err := connect.BuildVerifyAssignMessage(7, trailId, nonce, validatorPublic, connect.VerifyMMin, []connect.Id{healthy, excluded})
		if err != nil {
			t.Fatal(err)
		}
		assign, err := json.Marshal(connect.VerifyAssignResult{TrailId: trailId, ServerNonce: nonce, Trail: []connect.Id{healthy}, NextHop: excluded, M: connect.VerifyMMin, ServerKeyId: 7, AssignSig: ed25519.Sign(serverPrivate, message)})
		if err != nil {
			t.Fatal(err)
		}
		posts := 0
		client := adversaryGetTestClient(func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodGet && request.URL.Path == "/verify/keys" {
				return adversaryGetTestResponse(http.StatusOK, string(keys)), nil
			}
			posts++
			return adversaryGetTestResponse(http.StatusOK, string(assign)), nil
		})
		window := newAdversaryFaultWindow(time.Second)
		window.Update([]string{test.target})
		actor, err := newVerifyAdversary(cfg, roles, client, window)
		if err != nil {
			t.Fatal(err)
		}
		actor.seedProviders[operator] = []connect.Id{excluded, healthy}
		_, requests, _, err := actor.walk(t.Context(), operator, 0, false)
		result := actor.sampleError(operator, err, requests, 1)
		if result.Outcome != adversaryOutcomeSkipped || posts != 1 || requests != 2 || !strings.Contains(result.Detail, "target="+test.target) || len(result.Metrics) != 0 {
			t.Errorf("%s selected disabled seed or requested disabled next hop: %+v posts=%d", test.name, result, posts)
		}
	}
}

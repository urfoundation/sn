// Pinned synthetic coordinator replies exercise scheduling reentry and the
// accepted-window barrier without sending a transaction or waiting real epochs.
package main

import (
	"context"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Reentry accepts only the already scheduled exact generation. Every read
// is pinned and any transaction, balance reset or deposit-nonce RPC is refused.
func TestPolicyRateAmendmentPinnedScheduleReentry(t *testing.T) {
	cfg, old, plan := rateAmendmentTestPlans(t)
	prior := *cfg
	prior.Policy, prior.PolicyHash = &plan.PolicyRateAmendment.Previous, old.PolicyHash
	active := rateAmendmentTestSnapshot(&prior, 4, 1000)
	pending := rateAmendmentTestSnapshot(cfg, 11, 3100)
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{}
	addPolicyRevisionRPCOutput(t, outputs, parsed, "currentEpoch", nil, big.NewInt(10))
	addPolicyRevisionRPCOutput(t, outputs, parsed, "policyCount", nil, big.NewInt(3))
	addPolicyRevisionRPCOutput(t, outputs, parsed, "policyAt", []any{big.NewInt(10)}, active)
	addPolicyRevisionRPCOutput(t, outputs, parsed, "policyByIndex", []any{big.NewInt(2)}, pending)
	fixture := &policyRevisionRPCFixture{t: t, outputs: outputs}
	server := httptest.NewServer(fixture)
	defer server.Close()
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	plan.Deployment.CoordinatorProxy = common.Address{1}
	executor := &Executor{cfg: cfg, plan: plan, owner: &EvmTxManager{client: client}, payloads: &DeploymentPayloads{Manifest: plan.Deployment}}
	for range 2 {
		if err := executor.schedulePolicyRateAmendment(t.Context(), Action{}); err != nil {
			t.Fatalf("exact pending generation was rescheduled: %v", err)
		}
	}
	fixture.stateLock.Lock()
	addPolicyRevisionRPCOutput(t, outputs, parsed, "currentEpoch", nil, big.NewInt(11))
	addPolicyRevisionRPCOutput(t, outputs, parsed, "policyAt", []any{big.NewInt(11)}, pending)
	fixture.stateLock.Unlock()
	if err := executor.awaitBootstrapPolicy(t.Context()); err != nil {
		t.Fatalf("exact active generation refused: %v", err)
	}
	foreign := pending
	foreign.PolicyHash[0] ^= 1
	foreign.EffectiveEpoch++
	foreign.EffectiveBlock += foreign.EpochBlocks
	fixture.stateLock.Lock()
	addPolicyRevisionRPCOutput(t, outputs, parsed, "policyByIndex", []any{big.NewInt(2)}, foreign)
	fixture.stateLock.Unlock()
	if err := executor.schedulePolicyRateAmendment(t.Context(), Action{}); err == nil {
		t.Fatal("active generation hid a foreign pending schedule")
	}
	if err := executor.awaitBootstrapPolicy(t.Context()); err == nil {
		t.Fatal("activation wait skipped complete policy inventory authentication")
	}
}

// The first accepted epoch follows both activation and a complete lagged
// current-policy usage epoch. A readiness boolean cannot replace its proof.
func TestPolicyRateAmendmentAcceptanceStartsAfterCompleteSource(t *testing.T) {
	cfg, _, plan := rateAmendmentTestPlans(t)
	cfg, err := configWithPolicyRateAmendment(cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := scenarioDefinitionFor(cfg, "release-1.0")
	if err != nil {
		t.Fatal(err)
	}
	baseline := testScenarioObservation(cfg, 12)
	baseline.Status.Contracts.Policy.EffectiveEpoch = 11
	baseline.PolicyRateReadiness = &PolicyRateReadinessObservation{Ready: true, Head: baseline.Status.Contracts.FinalizedHead, AlphaPriceWei: "500000000000000", MinimumTransferTaoRao: 100_000}
	for noId := uint64(1); noId <= 2; noId++ {
		baseline.PolicyRateReadiness.Sources = append(baseline.PolicyRateReadiness.Sources, PolicyRateSourceObservation{NoId: noId, Epoch: 11, PolicyHash: cfg.PolicyHash, ContentHash: "sha256:" + strings.Repeat("ab", 32), TotalUsageBytes: 24 * 1024 * 1024})
	}
	baseline.ObservationHash, _ = canonicalHashHex(baseline)
	window, err := buildScenarioAcceptanceWindow(cfg, definition, baseline)
	if err != nil || window.FirstEpoch != 13 || window.FirstEpoch < baseline.Status.Contracts.Policy.EffectiveEpoch+2 {
		t.Fatalf("partial or pre-activation source admitted: %+v %v", window, err)
	}
	baseline.PolicyRateReadiness.Sources[0].Epoch = 10
	baseline.ObservationHash, _ = canonicalHashHex(baseline)
	if _, err := buildScenarioAcceptanceWindow(cfg, definition, baseline); err == nil {
		t.Fatal("ready flag concealed a source from before activation")
	}
}

// Not-ready observations remain ordinary retained preparation. The virtual
// clock proves that the controller waits and advances rather than failing or
// manufacturing an accepted epoch from the partial activation observation.
func TestPolicyRateAmendmentWaitRetainsReadinessProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, _, plan := rateAmendmentTestPlans(t)
		cfg, err := configWithPolicyRateAmendment(cfg, plan)
		if err != nil {
			t.Fatal(err)
		}
		pending := &ScenarioObservation{PolicyRateReadiness: &PolicyRateReadinessObservation{Detail: "incomplete source"}}
		ready := &ScenarioObservation{PolicyRateReadiness: &PolicyRateReadinessObservation{Ready: true}}
		probe := &scenarioIntervalProbe{observations: []*ScenarioObservation{pending, ready}}
		var retained []*ScenarioObservation
		started := time.Now()
		current, err := waitScenarioPolicyRateReadiness(context.Background(), cfg, "release-1.0", pending, probe, time.Second, func(value *ScenarioObservation) error {
			retained = append(retained, value)
			return nil
		})
		if err != nil || current == nil || !current.PolicyRateReadiness.Ready || len(retained) != 2 || probe.calls.Load() != 2 || time.Since(started) != 2*time.Second {
			t.Fatalf("readiness progress lost: retained=%d calls=%d elapsed=%s err=%v", len(retained), probe.calls.Load(), time.Since(started), err)
		}
	})
}

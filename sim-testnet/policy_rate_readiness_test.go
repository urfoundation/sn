// Synthetic signed-source projections exercise both admission gates while
// keeping low usage, failed authentication and final acceptance distinct.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// Models the output of the signature, membership and chain artifact reader.
func policyRateReadinessTestObservation(cfg *ResolvedConfig, usageBytes uint64) *ScenarioObservation {
	observation := testScenarioObservation(cfg, 12)
	observation.ObservedAt = "2020-01-02T03:04:05Z"
	observation.Status.Contracts.Policy.EffectiveEpoch = 11
	proof := &PolicyRateReadinessObservation{Head: observation.Status.Contracts.FinalizedHead, AlphaPriceWei: "500000000000000", MinimumTransferTaoRao: 100_000}
	for noId := 1; noId <= cfg.Config.Topology.Operators; noId++ {
		source := PolicyRateSourceObservation{NoId: uint64(noId), Epoch: 11, PolicyHash: cfg.PolicyHash, ContentHash: fmt.Sprintf("sha256:%064x", noId), TotalUsageBytes: usageBytes}
		proof.Sources = append(proof.Sources, source)
		observation.Operators = append(observation.Operators, OperatorObservation{NoID: noId, Healthy: true, RateSource: &source, ValidArtifacts: 1, MatchingArtifacts: 1, ArtifactHashes: []string{source.ContentHash}, LatestArtifactEpoch: source.Epoch, LatestArtifactHash: source.ContentHash, TierMembershipValid: true})
	}
	observation.PolicyRateReadiness = proof
	completePolicyRateReadiness(cfg, observation.Status.Contracts, observation.Operators, proof, big.NewInt(500_000_000_000_000))
	return observation
}

// Only the explicit provisional release owner may defer a native-floor margin.
func policyRateProvisionalTestConfig(t *testing.T) *ResolvedConfig {
	t.Helper()
	cfg, _, plan := rateAmendmentTestPlans(t)
	cfg, err := configWithPolicyRateAmendment(cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true, FinalAcceptance: false, Command: "scenario", Scenario: "release-1.0"}}
	return cfg
}

// Both gates admit the same honest shortfall, retaining only future full epochs.
func TestPolicyRateProvisionalLowUsageStartsFutureInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := policyRateProvisionalTestConfig(t)
		observation := policyRateReadinessTestObservation(cfg, 2*1024*1024)
		proof := observation.PolicyRateReadiness
		if proof.Ready || proof.ProvisionalLowUsage == nil || len(proof.ProvisionalLowUsage.Shortfalls) != len(cfg.Policy.Deposit.Tiers)*cfg.Config.Topology.Operators || proof.ProvisionalLowUsage.Shortfalls[0].EquivalentTaoRao != "39062" || proof.ProvisionalLowUsage.Shortfalls[0].RequiredTaoRao != "200000" {
			t.Fatalf("low usage lost its exact diagnostic: %+v", proof)
		}
		observation.ObservationHash, _ = canonicalHashHex(observation)
		before, _ := json.Marshal(observation)
		probe := &scenarioIntervalProbe{}
		started := time.Now()
		current, err := waitScenarioPolicyRateReadiness(t.Context(), cfg, "release-1.0", observation, probe, time.Second, func(*ScenarioObservation) error { return errors.New("already retained baseline was rewritten") })
		if err != nil || current != observation || probe.calls.Load() != 0 || time.Since(started) != 0 {
			t.Fatalf("provisional low usage remained blocked: %v", err)
		}
		definition, err := scenarioDefinitionFor(cfg, "release-1.0")
		if err != nil {
			t.Fatal(err)
		}
		window, err := buildScenarioAcceptanceWindow(cfg, definition, observation)
		if err != nil || window.FirstEpoch != observation.Status.Contracts.CurrentEpoch+1 || window.StartBlock != observation.Status.Contracts.CurrentEpochEnd {
			t.Fatalf("provisional interval credited historical or partial epochs: %+v %v", window, err)
		}
		after, _ := json.Marshal(observation)
		if string(before) != string(after) {
			t.Fatal("admission changed the retained observation or readiness flag")
		}
		result := &ScenarioResult{Name: "release-1.0", Result: "pass"}
		applyProvisionalScenarioProvenance(cfg, result)
		if !result.Provisional || result.FinalAcceptance == nil || *result.FinalAcceptance {
			t.Fatal("provisional interval claimed final acceptance")
		}
		if err := validateScenarioFinalSemanticSource(nil, nil, result, nil); err == nil || !strings.Contains(err.Error(), "provisional") {
			t.Fatalf("low-usage provisional result entered final acceptance: %v", err)
		}
		if _, _, err := finalSemanticSupplementRoots(context.Background(), cfg, nil, "", "", result); err == nil || !strings.Contains(err.Error(), "provisional") {
			t.Fatalf("low-usage provisional result entered final publication: %v", err)
		}
	})
}

// A low first operator must never hide a later foreign or incomplete source.
func TestPolicyRateLowUsageRequiresCompleteAuthenticatedCensus(t *testing.T) {
	cfg := policyRateProvisionalTestConfig(t)
	for _, edit := range []func(*ScenarioObservation){
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Sources[1].NoId = 1 },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Sources[1].PolicyHash = "foreign" },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Sources[1].Epoch-- },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Sources[1].ContentHash = "unverified" },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Sources = o.PolicyRateReadiness.Sources[:1] },
	} {
		observation := policyRateReadinessTestObservation(cfg, 2*1024*1024)
		edit(observation)
		var lowUsage *policyRateLowUsageError
		if err := validatePolicyRateReadiness(cfg, observation.Status.Contracts, observation.PolicyRateReadiness.Sources, big.NewInt(500_000_000_000_000)); err == nil || errors.As(err, &lowUsage) {
			t.Fatalf("untrusted source was classified as deferrable low usage: %v", err)
		}
		if err := validateScenarioPolicyRateAdmission(cfg, observation); err == nil {
			t.Fatal("foreign source entered provisional interval")
		}
	}
}

// Serialized flags cannot substitute for the complete observed source proof.
func TestPolicyRateLowUsageRejectsUnverifiedOrChangedProof(t *testing.T) {
	cfg := policyRateProvisionalTestConfig(t)
	for index, edit := range []func(*ScenarioObservation){
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Ready = true },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Head.Number++ },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.MinimumTransferTaoRao-- },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.AlphaPriceWei = "0" },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.AlphaPriceWei = "0500000000000000" },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Detail = "ready" },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.ProvisionalLowUsage = nil },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.ProvisionalLowUsage.FinalAcceptance = true },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.ProvisionalLowUsage.Scope = "all-errors" },
		func(o *ScenarioObservation) {
			o.PolicyRateReadiness.ProvisionalLowUsage.Shortfalls[0].EquivalentTaoRao = "200000"
		},
		func(o *ScenarioObservation) { o.Operators[1].Error = "invalid artifact signature" },
		func(o *ScenarioObservation) { o.Operators[1].Healthy = false },
		func(o *ScenarioObservation) { o.Operators[1].NoID = 1 },
		func(o *ScenarioObservation) { o.Operators[1].RateSource = nil },
		func(o *ScenarioObservation) { o.Operators[1].RateSource.TotalUsageBytes++ },
		func(o *ScenarioObservation) { o.Operators[1].ArtifactHashes = nil },
		func(o *ScenarioObservation) { o.Operators[1].MatchingArtifacts = 0 },
		func(o *ScenarioObservation) { o.Operators[1].LatestArtifactEpoch-- },
		func(o *ScenarioObservation) { o.Operators[1].TierMembershipValid = false },
		func(o *ScenarioObservation) { o.Operators = o.Operators[:1] },
	} {
		observation := policyRateReadinessTestObservation(cfg, 2*1024*1024)
		edit(observation)
		if err := validateScenarioPolicyRateAdmission(cfg, observation); err == nil {
			t.Fatalf("changed proof %d entered provisional interval", index)
		}
	}
}

// Read-only, strict, unrelated command and final-claim owners cannot defer.
func TestPolicyRateLowUsageRequiresProvisionalReleaseOwner(t *testing.T) {
	cfg := policyRateProvisionalTestConfig(t)
	observation := policyRateReadinessTestObservation(cfg, 2*1024*1024)
	for index, edit := range []func(*ResolvedConfig){
		func(c *ResolvedConfig) { c.provisionalResume = nil },
		func(c *ResolvedConfig) { c.readOnlyAudit = true },
		func(c *ResolvedConfig) { c.provisionalResume.Record.Provisional = false },
		func(c *ResolvedConfig) { c.provisionalResume.Record.FinalAcceptance = true },
		func(c *ResolvedConfig) { c.provisionalResume.Record.ReadOnly = true },
		func(c *ResolvedConfig) { c.provisionalResume.Record.Command = "doctor" },
		func(c *ResolvedConfig) { c.provisionalResume.Record.Scenario = "production-soak" },
	} {
		changed, state, record := *cfg, *cfg.provisionalResume, *cfg.provisionalResume.Record
		changed.provisionalResume, state.Record = &state, &record
		edit(&changed)
		if err := validateScenarioPolicyRateAdmission(&changed, observation); err == nil {
			t.Fatalf("unauthorized owner %d deferred readiness", index)
		}
	}
	strict := *cfg
	strict.provisionalResume = nil
	ready := policyRateReadinessTestObservation(&strict, 24*1024*1024)
	if err := validateScenarioPolicyRateAdmission(&strict, ready); err != nil {
		t.Fatalf("strict adequate margin was refused: %v", err)
	}
	wire, err := json.Marshal(ready.PolicyRateReadiness)
	if err != nil || strings.Contains(string(wire), "provisional_low_usage") {
		t.Fatalf("strict historical wire format gained a deferral: %s %v", wire, err)
	}
}

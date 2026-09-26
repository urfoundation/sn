// A complete signed usage source can precede its on-chain payout publication.
// Provisional observation retains that distinction without granting credit.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

// Keep authentic source membership and an independently matched older payout.
func policyRateUnmatchedSourceTestObservation(cfg *ResolvedConfig, usageBytes uint64) *ScenarioObservation {
	observation := policyRateReadinessTestObservation(cfg, usageBytes)
	for index := range observation.Operators {
		operator := &observation.Operators[index]
		operator.LatestArtifactEpoch = 8
		operator.LatestArtifactHash = fmt.Sprintf("sha256:%064x", 100+operator.NoID)
		operator.ArtifactHashes = append(operator.ArtifactHashes, operator.LatestArtifactHash)
		operator.ValidArtifacts++
	}
	completePolicyRateReadiness(cfg, observation.Status.Contracts, observation.Operators, observation.PolicyRateReadiness, big.NewInt(500_000_000_000_000))
	return observation
}

// Both actual startup gates must admit the signed source without awaiting an
// impossible zero-usage payout, while preserving future-only measurement.
func TestPolicyRateProvisionalUnmatchedSourceStartsFutureInterval(t *testing.T) {
	t.Parallel()
	cfg := policyRateProvisionalTestConfig(t)
	for _, usageBytes := range []uint64{0, 2 * 1024 * 1024} {
		observation := policyRateUnmatchedSourceTestObservation(cfg, usageBytes)
		proof := observation.PolicyRateReadiness
		if err := validateScenarioPolicyRateAdmission(cfg, observation); err != nil {
			t.Fatalf("signed usage source was confused with an older on-chain payout: %v", err)
		}
		if proof.Ready || proof.ProvisionalLowUsage == nil || proof.ProvisionalLowUsage.FinalAcceptance {
			t.Fatal("unmatched source gained readiness or final acceptance")
		}
		if len(proof.ProvisionalLowUsage.UnmatchedSources) != len(observation.Operators) || len(proof.ProvisionalLowUsage.Shortfalls) != len(cfg.Policy.Deposit.Tiers)*len(observation.Operators) {
			t.Fatal("source mismatch or all-tier shortfalls were omitted")
		}
		for index, unmatched := range proof.ProvisionalLowUsage.UnmatchedSources {
			operator := observation.Operators[index]
			expected := PolicyRateUnmatchedSource{NoId: uint64(operator.NoID), SourceEpoch: operator.RateSource.Epoch, SourceContentHash: operator.RateSource.ContentHash, LatestMatchingEpoch: operator.LatestArtifactEpoch, LatestMatchingContentHash: operator.LatestArtifactHash}
			if unmatched != expected {
				t.Fatalf("source diagnostic changed authenticated identities: %+v", unmatched)
			}
		}
		before, err := json.Marshal(observation)
		if err != nil {
			t.Fatal(err)
		}
		probe := &scenarioIntervalProbe{}
		current, err := waitScenarioPolicyRateReadiness(t.Context(), cfg, "release-1.0", observation, probe, time.Second, func(*ScenarioObservation) error {
			t.Fatal("admission rewrote the retained baseline")
			return nil
		})
		if err != nil || current != observation || probe.calls.Load() != 0 {
			t.Fatalf("authenticated low usage remained blocked: %v", err)
		}
		definition, err := scenarioDefinitionFor(cfg, "release-1.0")
		if err != nil {
			t.Fatal(err)
		}
		window, err := buildScenarioAcceptanceWindow(cfg, definition, observation)
		if err != nil || window.FirstEpoch != observation.Status.Contracts.CurrentEpoch+1 || window.StartBlock != observation.Status.Contracts.CurrentEpochEnd {
			t.Fatalf("unmatched source credited a historical or partial epoch: %+v %v", window, err)
		}
		after, err := json.Marshal(observation)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("admission changed the source or payout evidence", err)
		}
		result := &ScenarioResult{Name: "release-1.0", Result: "pass"}
		applyProvisionalScenarioProvenance(cfg, result)
		if !result.Provisional || result.FinalAcceptance == nil || *result.FinalAcceptance {
			t.Fatal("unmatched source acquired final acceptance")
		}
		if err := validateScenarioFinalSemanticSource(nil, nil, result, nil); err == nil || !strings.Contains(err.Error(), "provisional") {
			t.Fatalf("unmatched source entered final acceptance: %v", err)
		}
		if _, _, err := finalSemanticSupplementRoots(context.Background(), cfg, nil, "", "", result); err == nil || !strings.Contains(err.Error(), "provisional") {
			t.Fatalf("unmatched source entered final publication: %v", err)
		}
	}
}

// A lagging payout is distinct from a conflicting, missing or unhealthy source.
func TestPolicyRateProvisionalUnmatchedSourceRejectsUnauthenticatedCensus(t *testing.T) {
	t.Parallel()
	cfg := policyRateProvisionalTestConfig(t)
	for index, edit := range []func(*ScenarioObservation){
		func(o *ScenarioObservation) { o.Operators[1].ArtifactHashes = o.Operators[1].ArtifactHashes[1:] },
		func(o *ScenarioObservation) { o.Operators[1].ArtifactHashes = o.Operators[1].ArtifactHashes[:1] },
		func(o *ScenarioObservation) { o.Operators[1].LatestArtifactEpoch = o.Operators[1].RateSource.Epoch },
		func(o *ScenarioObservation) { o.Operators[1].LatestArtifactEpoch = o.Operators[1].RateSource.Epoch + 1 },
		func(o *ScenarioObservation) {
			o.Operators[1].LatestArtifactHash = o.Operators[1].RateSource.ContentHash
		},
		func(o *ScenarioObservation) { o.Operators[1].LatestArtifactHash = "unverified" },
		func(o *ScenarioObservation) { o.Operators[1].MatchingArtifacts = 0 },
		func(o *ScenarioObservation) { o.Operators[1].ValidArtifacts = 0 },
		func(o *ScenarioObservation) { o.Operators[1].Error = "invalid artifact signature" },
		func(o *ScenarioObservation) { o.Operators[1].Healthy = false },
		func(o *ScenarioObservation) { o.Operators[1].TierMembershipValid = false },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Sources[1].Epoch-- },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Sources[1].PolicyHash = "foreign" },
		func(o *ScenarioObservation) { o.PolicyRateReadiness.Sources[1].NoId = 1 },
		func(o *ScenarioObservation) { o.Operators[1].RateSource.TotalUsageBytes++ },
	} {
		observation := policyRateUnmatchedSourceTestObservation(cfg, 0)
		edit(observation)
		completePolicyRateReadiness(cfg, observation.Status.Contracts, observation.Operators, observation.PolicyRateReadiness, big.NewInt(500_000_000_000_000))
		if observation.PolicyRateReadiness.ProvisionalLowUsage != nil || validateScenarioPolicyRateAdmission(cfg, observation) == nil {
			t.Fatalf("untrusted source case %d acquired provisional admission", index)
		}
	}
}

// Every recorded mismatch is replayed against the actual source projection.
func TestPolicyRateProvisionalUnmatchedSourceRejectsChangedDiagnostic(t *testing.T) {
	t.Parallel()
	cfg := policyRateProvisionalTestConfig(t)
	for index, edit := range []func(*PolicyRateLowUsageDeferral){
		func(d *PolicyRateLowUsageDeferral) { d.UnmatchedSources = nil },
		func(d *PolicyRateLowUsageDeferral) { d.UnmatchedSources = d.UnmatchedSources[:1] },
		func(d *PolicyRateLowUsageDeferral) { d.UnmatchedSources[1].NoId = 1 },
		func(d *PolicyRateLowUsageDeferral) { d.UnmatchedSources[1].SourceEpoch-- },
		func(d *PolicyRateLowUsageDeferral) {
			d.UnmatchedSources[1].SourceContentHash = d.UnmatchedSources[1].LatestMatchingContentHash
		},
		func(d *PolicyRateLowUsageDeferral) { d.UnmatchedSources[1].LatestMatchingEpoch++ },
		func(d *PolicyRateLowUsageDeferral) {
			d.UnmatchedSources[1].LatestMatchingContentHash = d.UnmatchedSources[1].SourceContentHash
		},
		func(d *PolicyRateLowUsageDeferral) { d.FinalAcceptance = true },
	} {
		observation := policyRateUnmatchedSourceTestObservation(cfg, 0)
		edit(observation.PolicyRateReadiness.ProvisionalLowUsage)
		if err := validateScenarioPolicyRateAdmission(cfg, observation); err == nil {
			t.Fatalf("changed diagnostic case %d entered an interval", index)
		}
	}
	observation := policyRateUnmatchedSourceTestObservation(cfg, 0)
	strict := *cfg
	strict.provisionalResume = nil
	if err := validateScenarioPolicyRateAdmission(&strict, observation); err == nil {
		t.Fatal("strict owner inherited unmatched provisional source authority")
	}
	readOnly := *cfg
	readOnly.readOnlyAudit = true
	if err := validateScenarioPolicyRateAdmission(&readOnly, observation); err == nil {
		t.Fatal("read-only replay gained provisional startup authority")
	}
}

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/urfoundation/sn/payoutartifact"
)

func auxiliaryPayoutTierFixture(t *testing.T) (*ResolvedConfig, *payoutArtifact, map[[16]byte]int, payoutartifact.ProviderInput) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.Miners = 10
	cfg.Config.Topology.HeadFleets = 2
	cfg.Config.Topology.ChallengerFleets = 1
	cfg.Config.Topology.ClientsPerHeadFleet = 2
	clients := lifecyclePayoutTestClients(cfg)
	artifact := &payoutArtifact{NoID: 1}
	for clientID, miner := range clients {
		if operatorForMiner(cfg, miner) != 1 {
			continue
		}
		provider := payoutartifact.ProviderInput{ClientID: clientID, NetworkID: [16]byte{42}}
		if miner <= cfg.Config.Topology.fleetCandidateMiners() {
			provider.HeadExcluded, provider.ExclusionReason = true, "head_fleet_active"
		} else {
			provider.Eligible = true
			artifact.Leaves = append(artifact.Leaves, payoutartifact.Leaf{ClientID: clientID})
		}
		artifact.Providers = append(artifact.Providers, provider)
	}
	auxiliary := payoutartifact.ProviderInput{ClientID: [16]byte{200}, NetworkID: [16]byte{42}, UsageBytes: 1082492, ExclusionReason: "missing_payout_wallet"}
	return cfg, artifact, clients, auxiliary
}

func TestPayoutTierMembershipPreservesUnpaidAuxiliaryUsage(t *testing.T) {
	for _, provisional := range []bool{false, true} {
		cfg, artifact, clients, auxiliary := auxiliaryPayoutTierFixture(t)
		if provisional {
			cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}
		}
		artifact.Providers = append(artifact.Providers, auxiliary)
		before, _ := json.Marshal(artifact)
		summary, err := summarizePayoutTierMembership(cfg, 1, artifact, clients)
		if err != nil || summary.Providers != 7 || summary.ExcludedUnconfiguredProviders != 1 || summary.CandidateProviders != 4 || summary.CandidateHeadExcluded != 4 || summary.CandidateLeaves != 0 || summary.PoolTailProviders != 2 || summary.PoolTailLeaves != 2 {
			t.Fatalf("provisional=%v membership=%+v error=%v", provisional, summary, err)
		}
		after, _ := json.Marshal(artifact)
		if string(before) != string(after) {
			t.Fatal("cohort validation changed immutable provider usage or leaves")
		}
	}
}

func TestPayoutTierMembershipRejectsAuxiliaryParticipationAndSubstitution(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*payoutArtifact, *payoutartifact.ProviderInput)
	}{
		{name: "payout wallet", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.Coldkey[0] = 1 }},
		{name: "eligible", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.Eligible = true }},
		{name: "head excluded", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.HeadExcluded = true }},
		{name: "binding", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.BindingGeneration = 1 }},
		{name: "assignment", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.Assignments = 1 }},
		{name: "confirmation", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.Confirmations = 1 }},
		{name: "reliability", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.ReliabilityPPM = 1 }},
		{name: "reason", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.ExclusionReason = "head_fleet_active" }},
		{name: "foreign network", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.NetworkID[0]++ }},
		{name: "missing network", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.NetworkID = [16]byte{} }},
		{name: "zero client", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.ClientID = [16]byte{} }},
		{name: "foreign miner", change: func(_ *payoutArtifact, p *payoutartifact.ProviderInput) { p.ClientID = [16]byte{3} }},
		{name: "duplicate", change: func(a *payoutArtifact, p *payoutartifact.ProviderInput) { a.Providers = append(a.Providers, *p) }},
		{name: "leaf", change: func(a *payoutArtifact, p *payoutartifact.ProviderInput) {
			a.Leaves = append(a.Leaves, payoutartifact.Leaf{ClientID: p.ClientID})
		}},
		{name: "missing configured provider", change: func(a *payoutArtifact, _ *payoutartifact.ProviderInput) { a.Providers = a.Providers[1:] }},
	} {
		cfg, artifact, clients, auxiliary := auxiliaryPayoutTierFixture(t)
		test.change(artifact, &auxiliary)
		artifact.Providers = append(artifact.Providers, auxiliary)
		if summary, err := summarizePayoutTierMembership(cfg, 1, artifact, clients); err == nil {
			t.Fatalf("%s: unsafe auxiliary classification admitted: %+v", test.name, summary)
		}
	}
}

func TestFinalPayoutArtifactPreservesAuthenticatedUnpaidAuxiliaryUsage(t *testing.T) {
	fixture, original := finalPayoutArtifactTestFixture(t)
	base, err := payoutartifact.Decode(original)
	if err != nil {
		t.Fatal(err)
	}
	auxiliary := payoutartifact.ProviderInput{ClientID: [16]byte{200}, NetworkID: fixture.providers[0].NetworkID, UsageBytes: 1082492, ExclusionReason: "missing_payout_wallet"}
	providers := append(append([]payoutartifact.ProviderInput(nil), fixture.providers...), auxiliary)
	auxiliary.ClientID[0]++
	providers = append(providers, auxiliary)
	artifact, data := finalPayoutArtifactTestBuild(t, fixture, 1, providers, base.Start, base.End, fixture.reliabilityMin)
	expected := finalPayoutArtifactTestExpectation(artifact)
	if err := verifyFinalPayoutArtifact(fixture.evidence, fixture.pool, expected, fixture.assignments, fixture.reliabilityMin, data); err != nil {
		t.Fatalf("fully signed unpaid auxiliary census: %v", err)
	}
	if artifact.TotalUsageBytes != base.TotalUsageBytes+2*auxiliary.UsageBytes || artifact.ExcludedUsageBytes != base.ExcludedUsageBytes+2*auxiliary.UsageBytes || artifact.EligibleUsageBytes != base.EligibleUsageBytes || artifact.PayoutRoot != base.PayoutRoot || len(artifact.Leaves) != len(base.Leaves) {
		t.Fatal("auxiliary classification changed usage or payout semantics")
	}
	wrongUsage := *expected
	wrongUsage.UsageBytes = base.TotalUsageBytes
	if err := verifyFinalPayoutArtifact(fixture.evidence, fixture.pool, &wrongUsage, fixture.assignments, fixture.reliabilityMin, data); err == nil || !strings.Contains(err.Error(), "usage does not match") {
		t.Fatalf("excluded usage lost independent audit binding: %v", err)
	}
}

func TestFinalPayoutArtifactRejectsSignedAuxiliaryParticipationAndSubstitution(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*payoutartifact.ProviderInput)
	}{
		{name: "payout wallet", change: func(p *payoutartifact.ProviderInput) { p.Coldkey[0] = 1 }},
		{name: "eligible", change: func(p *payoutartifact.ProviderInput) { p.Eligible = true }},
		{name: "head excluded", change: func(p *payoutartifact.ProviderInput) { p.HeadExcluded = true }},
		{name: "binding", change: func(p *payoutartifact.ProviderInput) { p.BindingGeneration = 1 }},
		{name: "assignment", change: func(p *payoutartifact.ProviderInput) { p.Assignments = 1 }},
		{name: "confirmation", change: func(p *payoutartifact.ProviderInput) { p.Confirmations = 1 }},
		{name: "reason", change: func(p *payoutartifact.ProviderInput) { p.ExclusionReason = "head_fleet_active" }},
		{name: "foreign network", change: func(p *payoutartifact.ProviderInput) { p.NetworkID = [16]byte{200} }},
		{name: "missing network", change: func(p *payoutartifact.ProviderInput) { p.NetworkID = [16]byte{} }},
		{name: "foreign miner", change: func(p *payoutartifact.ProviderInput) { p.ClientID = [16]byte(finalPayoutArtifactTestClient(3)) }},
	} {
		fixture, original := finalPayoutArtifactTestFixture(t)
		base, err := payoutartifact.Decode(original)
		if err != nil {
			t.Fatal(err)
		}
		auxiliary := payoutartifact.ProviderInput{ClientID: [16]byte{200}, NetworkID: fixture.providers[0].NetworkID, UsageBytes: 872758, ExclusionReason: "missing_payout_wallet"}
		test.change(&auxiliary)
		providers := append(append([]payoutartifact.ProviderInput(nil), fixture.providers...), auxiliary)
		artifact, data := finalPayoutArtifactTestBuild(t, fixture, 1, providers, base.Start, base.End, fixture.reliabilityMin)
		if err := verifyFinalPayoutArtifact(fixture.evidence, fixture.pool, finalPayoutArtifactTestExpectation(artifact), fixture.assignments, fixture.reliabilityMin, data); err == nil {
			t.Fatalf("%s: fully signed unsafe auxiliary row was accepted", test.name)
		}
	}
}

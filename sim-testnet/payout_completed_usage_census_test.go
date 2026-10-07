// Completed-work membership preserves signed sparse sources and rejects orphan leaves.
package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/payoutartifact"
)

// The server emits the completed-work census, so a configured miner without
// credited work has neither a provider row nor a payout leaf in that epoch.
func TestPayoutTierMembershipAllowsAbsentInactiveConfiguredMiners(t *testing.T) {
	for _, omitted := range [][]int{{1}, {7}, {1, 7}} {
		cfg, artifact, clients, _ := auxiliaryPayoutTierFixture(t)
		absent := map[int]bool{}
		for _, miner := range omitted {
			absent[miner] = true
		}
		providers := artifact.Providers[:0]
		for _, provider := range artifact.Providers {
			if !absent[clients[provider.ClientID]] {
				providers = append(providers, provider)
			}
		}
		artifact.Providers = providers
		leaves := artifact.Leaves[:0]
		for _, leaf := range artifact.Leaves {
			if !absent[clients[leaf.ClientID]] {
				leaves = append(leaves, leaf)
			}
		}
		artifact.Leaves = leaves
		before, _ := json.Marshal(artifact)
		summary, err := summarizePayoutTierMembership(cfg, 1, artifact, clients)
		if err != nil || summary.Providers != 6-len(omitted) || summary.CandidateHeadExcluded != summary.CandidateProviders || summary.CandidateLeaves != 0 || summary.PoolTailLeaves != summary.PoolTailProviders {
			t.Fatalf("absent=%v summary=%+v err=%v", omitted, summary, err)
		}
		after, _ := json.Marshal(artifact)
		if string(before) != string(after) {
			t.Fatal("validation altered the committed completed-work census")
		}
	}
}

func TestPayoutTierMembershipStillRequiresObservedExclusionAndPayout(t *testing.T) {
	for _, removeTier := range []string{"head", "tail", "leaves"} {
		cfg, artifact, clients, _ := auxiliaryPayoutTierFixture(t)
		providers := artifact.Providers[:0]
		for _, provider := range artifact.Providers {
			candidate := clients[provider.ClientID] <= cfg.Config.Topology.fleetCandidateMiners()
			if removeTier == "head" && candidate || removeTier == "tail" && !candidate {
				continue
			}
			providers = append(providers, provider)
		}
		artifact.Providers = providers
		if removeTier == "tail" || removeTier == "leaves" {
			artifact.Leaves = nil
		}
		if _, err := summarizePayoutTierMembership(cfg, 1, artifact, clients); err == nil {
			t.Fatal("vacuous payout-tier evidence was accepted")
		}
	}
}

func TestPayoutTierMembershipRejectsOrphanLeafAsStructuralError(t *testing.T) {
	cfg, artifact, clients, _ := auxiliaryPayoutTierFixture(t)
	missing := artifact.Leaves[0].ClientID
	providers := artifact.Providers[:0]
	for _, provider := range artifact.Providers {
		if provider.ClientID != missing {
			providers = append(providers, provider)
		}
	}
	artifact.Providers = providers
	var cohort *payoutCohortExpectationError
	if _, err := summarizePayoutTierMembership(cfg, 1, artifact, clients); err == nil || errors.As(err, &cohort) || !strings.Contains(err.Error(), "leaf lacks its provider") {
		t.Fatalf("orphan leaf was hidden by changed cohort cardinality: %v", err)
	}
}

// Final replay already authenticates the exact committed source census rather
// than requiring a usage row for every configured assignment. Keep that bound:
// a naturally sparse source passes; a replacement for the signed source fails.
func TestFinalPayoutArtifactDistinguishesInactiveMinerFromOmittedSourceRow(t *testing.T) {
	fixture, original := finalPayoutArtifactTestFixture(t)
	base, err := payoutartifact.Decode(original)
	if err != nil {
		t.Fatal(err)
	}
	artifact, data := finalPayoutArtifactTestBuild(t, fixture, 1, fixture.providers[:1], base.Start, base.End, fixture.reliabilityMin)
	if err := verifyFinalPayoutArtifact(fixture.evidence, fixture.pool, finalPayoutArtifactTestExpectation(artifact), fixture.assignments, fixture.reliabilityMin, data); err != nil {
		t.Fatalf("signed completed source with inactive configured assignments: %v", err)
	}
	if err := verifyFinalPayoutArtifact(fixture.evidence, fixture.pool, fixture.expected, fixture.assignments, fixture.reliabilityMin, data); err == nil || !strings.Contains(err.Error(), "not the committed artifact hash") {
		t.Fatalf("replacement source omitted a committed provider: %v", err)
	}
}

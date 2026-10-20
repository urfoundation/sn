package main

import (
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/payoutartifact"
)

func lifecyclePayoutTestClients(cfg *ResolvedConfig) map[[16]byte]int {
	clients := make(map[[16]byte]int, cfg.Config.Topology.Miners)
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		var clientID [16]byte
		clientID[0] = byte(miner)
		clientID[1] = byte(miner >> 8)
		clients[clientID] = miner
	}
	return clients
}

func lifecyclePayoutTestArtifact(t *testing.T, cfg *ResolvedConfig, noID int, clients map[[16]byte]int) *payoutArtifact {
	t.Helper()
	tracked, err := fleetLifecycleTrackedClientIDs(cfg, noID, clients)
	if err != nil {
		t.Fatal(err)
	}
	artifact := &payoutArtifact{
		Epoch: 41, NoID: uint64(noID), ContentHash: "sha256:" + strings.Repeat("11", 32),
	}
	artifact.PayoutRoot[0] = 1
	for _, clientID := range tracked {
		artifact.Providers = append(artifact.Providers, payoutartifact.ProviderInput{
			ClientID: clientID, HeadExcluded: true, ExclusionReason: "head_fleet_active",
		})
	}
	return artifact
}

func TestCompactLifecyclePayoutArtifactRetainsExactHistoricalTierMembership(t *testing.T) {
	cfg := testResolvedConfig(t)
	clients := lifecyclePayoutTestClients(cfg)
	artifact := lifecyclePayoutTestArtifact(t, cfg, 1, clients)

	// Exercise both legal tiers in the same immutable historical artifact.
	artifact.Providers[0].HeadExcluded = false
	artifact.Providers[0].ExclusionReason = ""
	artifact.Providers[0].Eligible = true
	artifact.Leaves = append(artifact.Leaves, payoutartifact.Leaf{ClientID: artifact.Providers[0].ClientID})
	row, err := compactLifecyclePayoutArtifact(cfg, 1, artifact, clients)
	if err != nil {
		t.Fatal(err)
	}
	if row.Epoch != artifact.Epoch || row.NoID != artifact.NoID || row.ContentHash != artifact.ContentHash || len(row.Clients) != len(artifact.Providers) {
		t.Fatalf("compact lifecycle payout identity = %+v", row)
	}
	for index, client := range row.Clients {
		if index != 0 && row.Clients[index-1].ClientID >= client.ClientID {
			t.Fatal("compact lifecycle payout clients are not canonically ordered")
		}
		if client.Leaf == client.HeadExcluded {
			t.Fatalf("client %s does not have exclusive tier membership", client.ClientID)
		}
	}
	if !row.Clients[0].Leaf || row.Clients[0].HeadExcluded {
		t.Fatalf("paid lifecycle client was not retained exactly: %+v", row.Clients[0])
	}
	if err := validateOperatorLifecyclePayoutArtifactObservation(row); err != nil {
		t.Fatalf("canonical compact lifecycle payout rejected: %v", err)
	}
}

func TestCompactLifecyclePayoutArtifactRejectsEveryAmbiguousAdjacentTierState(t *testing.T) {
	cfg := testResolvedConfig(t)
	clients := lifecyclePayoutTestClients(cfg)
	tests := []struct {
		name   string
		mutate func(*payoutArtifact)
	}{
		{
			name: "both leaf and excluded",
			mutate: func(artifact *payoutArtifact) {
				artifact.Leaves = append(artifact.Leaves, payoutartifact.Leaf{ClientID: artifact.Providers[0].ClientID})
			},
		},
		{
			name: "neither leaf nor excluded",
			mutate: func(artifact *payoutArtifact) {
				artifact.Providers[0].HeadExcluded = false
				artifact.Providers[0].ExclusionReason = ""
			},
		},
		{
			name: "missing provider",
			mutate: func(artifact *payoutArtifact) {
				artifact.Providers = artifact.Providers[1:]
			},
		},
		{
			name: "duplicate provider",
			mutate: func(artifact *payoutArtifact) {
				artifact.Providers = append(artifact.Providers, artifact.Providers[0])
			},
		},
		{
			name: "duplicate leaf",
			mutate: func(artifact *payoutArtifact) {
				artifact.Providers[0].HeadExcluded = false
				artifact.Providers[0].ExclusionReason = ""
				artifact.Providers[0].Eligible = true
				leaf := payoutartifact.Leaf{ClientID: artifact.Providers[0].ClientID}
				artifact.Leaves = append(artifact.Leaves, leaf, leaf)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifact := lifecyclePayoutTestArtifact(t, cfg, 1, clients)
			test.mutate(artifact)
			if row, err := compactLifecyclePayoutArtifact(cfg, 1, artifact, clients); err == nil {
				t.Fatalf("ambiguous payout tier state accepted: %+v", row)
			}
		})
	}
}

func TestLifecyclePayoutEpochSelectionRetainsBothExactDynamicEpochs(t *testing.T) {
	evidence := &FleetLifecycleEvidence{FallbackEffectiveEpoch: 17, ProviderEffectiveEpoch: 29}
	epochs := fleetLifecyclePayoutEpochs(evidence)
	if len(epochs) != 2 || !epochs[17] || !epochs[29] || epochs[18] {
		t.Fatalf("dynamic lifecycle payout epochs = %v", epochs)
	}
	evidence.ProviderEffectiveEpoch = evidence.FallbackEffectiveEpoch
	if epochs = fleetLifecyclePayoutEpochs(evidence); len(epochs) != 1 || !epochs[17] {
		t.Fatalf("duplicate dynamic lifecycle payout epochs were not canonicalized: %v", epochs)
	}
}

func TestLifecyclePayoutObservationRejectsNoncanonicalOrderingAndIdentity(t *testing.T) {
	row := OperatorLifecyclePayoutArtifactObservation{
		Epoch: 1, NoID: 1, ContentHash: "sha256:" + strings.Repeat("11", 32), PayoutRoot: "0x" + strings.Repeat("22", 32),
		Clients: []OperatorPayoutClientTierObservation{
			{ClientID: "0x" + strings.Repeat("01", 16), Leaf: true},
			{ClientID: "0x" + strings.Repeat("02", 16), HeadExcluded: true},
		},
	}
	if err := validateOperatorLifecyclePayoutArtifactObservation(row); err != nil {
		t.Fatalf("canonical lifecycle payout observation rejected: %v", err)
	}
	row.Clients[0], row.Clients[1] = row.Clients[1], row.Clients[0]
	if err := validateOperatorLifecyclePayoutArtifactObservation(row); err == nil {
		t.Fatal("noncanonical lifecycle payout client ordering accepted")
	}
	row.Clients[0], row.Clients[1] = row.Clients[1], row.Clients[0]
	row.ContentHash = "sha256:" + strings.Repeat("AA", 32)
	if err := validateOperatorLifecyclePayoutArtifactObservation(row); err == nil {
		t.Fatal("noncanonical lifecycle payout content hash accepted")
	}
}

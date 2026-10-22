// SN25 launches with one network operator measured by one validator. Its
// verified provider window has one validator and one operator lane; the
// economic projection consumes that lane as it consumes one lane of a pair.
package main

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// Only the artifact operator's lane is projected; the complete single-lane
// window reconciles to the same provider measurement as the original rows.
func TestEconomicProviderTrialProjectionConsumesSingleOperatorWindow(t *testing.T) {
	rows := economicProviderMeasurementFixtureRows()
	artifact := economicProviderMeasurementTestArtifact(t, rows)
	domain := protocol.ClientKeyHistoryDomain{ChainID: artifact.ChainID, GenesisHash: common.HexToHash(artifact.GenesisHash), Netuid: artifact.Netuid, Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault, DeploymentIDHash: sha256.Sum256([]byte(artifact.DeploymentID)), PolicyHash: common.HexToHash(artifact.PolicyHash), NoID: artifact.NoID}
	work := &payoutartifact.VerifiedWholeWorkInventory{Complete: true, Domain: domain, Epoch: artifact.Epoch, Start: artifact.Start, End: artifact.End}
	workValues := &economicProviderWorkValues{artifactHash: artifact.ContentHash, authorityHash: "sha256:" + strings.Repeat("7", 64), windowHash: "sha256:" + strings.Repeat("8", 64), complete: true}
	wallets := map[[16]byte]*protocol.EarningWallet{}
	var bindings []validator.VerifiedProviderAttemptBinding
	var lane []validator.VerifiedProviderAttemptRow
	for _, row := range rows {
		work.ExpectedProviders = append(work.ExpectedProviders, payoutartifact.WholeWorkProvider{ClientId: row.ClientID, NetworkId: row.NetworkID, UsageBytes: row.UsageBytes})
		workValues.providers = append(workValues.providers, economicProviderWorkValue{clientId: row.ClientID, networkId: row.NetworkID, usageBytes: row.UsageBytes})
		wallets[row.ClientID] = &protocol.EarningWallet{Mode: protocol.EarningWalletModeProvider, ClientId: row.ClientID, NetworkId: row.NetworkID, Coldkey: row.Coldkey}
		bindings = append(bindings, validator.VerifiedProviderAttemptBinding{ClientId: row.ClientID, BindingGeneration: row.BindingGeneration})
		if row.Assignments > 0 {
			lane = append(lane, validator.VerifiedProviderAttemptRow{NoId: artifact.NoID, ClientId: row.ClientID, Assignments: row.Assignments, Confirmations: row.Confirmations})
		}
	}
	attempts := &validator.VerifiedProviderAttemptMeasurement{VerifiedProviderAttemptWindow: &validator.VerifiedProviderAttemptWindow{Domain: economicProviderAttemptDomain(domain), RegistryHash: [32]byte{9}, WindowHash: [32]byte{10},
		Window: protocol.ValidatorEvidenceWindow{Epoch: artifact.Epoch, StartBlock: artifact.Start.Number, EndBlock: artifact.End.Number, FinalizedBlock: artifact.End.Number}, CutCensusComplete: true, OwnedRequestsComplete: true, Providers: lane, Validators: 1, OperatorLanes: 1}, ReliabilityAMin: artifact.ReliabilityAMin}
	trials, err := economicProviderTrialProjection(t.Context(), artifact, work, attempts, wallets, bindings)
	if err != nil || trials == nil || len(trials.providers) != len(rows) {
		t.Fatalf("single-lane provider window was not projected: %v", err)
	}
	value, err := reconcileEconomicProviderMeasurements(t.Context(), artifact, workValues, trials)
	if err != nil || value == nil || value.Providers != 4 || value.CompletedBytes != 400 || value.Assignments != 30 || value.Confirmations != 16 || value.ProviderHash != artifact.ProviderSnapshotHash {
		t.Fatalf("single-lane provider measurement differs: %+v %v", value, err)
	}
	// Exposure recorded under another operator id is not this pool's exposure.
	attempts.Providers = append(append([]validator.VerifiedProviderAttemptRow(nil), lane...), validator.VerifiedProviderAttemptRow{NoId: artifact.NoID + 1, ClientId: rows[3].ClientID, Assignments: 99, Confirmations: 99})
	foreign, err := economicProviderTrialProjection(t.Context(), artifact, work, attempts, wallets, bindings)
	if err != nil || foreign == nil || foreign.providers[3].assignments != 0 || foreign.providers[3].eligible {
		t.Fatalf("another operator's lane changed the single-operator projection: %v", err)
	}
}

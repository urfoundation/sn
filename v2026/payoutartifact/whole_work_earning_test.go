// A straddling earning cutoff changes payout usage while retaining the full
// original epoch, all SDK obligations and both pre/post-cutoff settlements.
package payoutartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// The Server's ordinary projection path supplies its independently loaded
// earning selection without manufacturing complete SDK or participant proof.
func TestWholeWorkEarningProducerProjectionRetainsPreCutoffOriginals(t *testing.T) {
	fixture := newWholeWorkEarningTestFixture(t)
	providers := []ProviderInput{}
	for _, provider := range fixture.artifact.Providers {
		if provider.UsageBytes != 0 {
			providers = append(providers, provider)
		}
	}
	fixture.artifact.Providers = providers
	rebuildWholeWorkEarningTestArtifact(t, fixture, fixture.expected.EarningSelection.StartTime)
	value, err := VerifyClosedWorkReportsWithEarningSelection(t.Context(), fixture.artifact, common.Address{}, fixture.expected.EarningSelection)
	if err != nil || value == nil || value.Contracts != 2 || value.ClosedWork.UsageBytes != 100 || value.RegisteredReports != 0 || value.ReservedAmountJoins != 0 {
		t.Fatal("producer projection lost full originals or acquired full authority", value, err)
	}
}

// Rebuild publisher-controlled totals only. Original source receipts, reports
// and cuts remain independent of this selected SQL projection.
func rebuildWholeWorkEarningTestArtifact(t *testing.T, fixture *wholeWorkTestFixture, cutoff time.Time) {
	t.Helper()
	old := fixture.artifact
	providers := append([]ProviderInput(nil), old.Providers...)
	for index := range providers {
		providers[index].UsageBytes = 0
	}
	for _, row := range old.ClosedWork.Records {
		closed, err := time.Parse(time.RFC3339Nano, row.ClosedAt)
		if err != nil {
			t.Fatal(err)
		}
		if closed.Before(cutoff) {
			continue
		}
		snapshot, err := decodeClosedWorkSnapshot(row, old.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		for _, party := range *snapshot.Providers {
			id, err := closedWorkId(party.ClientId)
			if err != nil {
				t.Fatal(err)
			}
			for index := range providers {
				if providers[index].ClientID == id {
					providers[index].UsageBytes += uint64(*party.ByteCount)
				}
			}
		}
	}
	created, err := time.Parse(time.RFC3339Nano, old.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	fixture.artifact, err = BuildWithContext(t.Context(), BuildInput{ClosedWork: old.ClosedWork,
		DeploymentID: old.DeploymentID, GenesisHash: old.GenesisHash, PolicyHash: old.PolicyHash,
		ChainID: old.ChainID, Netuid: old.Netuid, Coordinator: old.Coordinator, SettlementVault: old.SettlementVault,
		Epoch: old.Epoch, NoID: old.NoID, Start: old.Start, End: old.End,
		OperatorSnapshotHash: old.OperatorSnapshotHash, FleetSnapshotHash: old.FleetSnapshotHash,
		Providers: providers, TotalUsers: old.TotalUsers, ReliabilityAMin: old.ReliabilityAMin, CreatedAt: created})
	if err != nil {
		t.Fatal(err)
	}
	closedWorkTestSign(t, fixture.artifact)
}

// The earlier and exact-boundary closes are original independently signed
// events. The configured earning identity is not taken from either publisher.
func newWholeWorkEarningTestFixture(t *testing.T) *wholeWorkTestFixture {
	t.Helper()
	fixture := participantEvidenceFixture(t)
	cutoff := fixture.inventory.Clock.StartTime.Add(30 * time.Minute)
	selection, err := NewWholeWorkEarningSelection(WholeWorkEarningIdentity{
		Schema: "synthetic-earning-policy-v1", CutoffUtc: cutoff.UTC().Format(time.RFC3339Nano),
		Attribution: "close_time", LegacyUsdc: "before_cutoff_only", Profile: "synthetic",
		ChainId: fixture.artifact.ChainID, GenesisHash: fixture.artifact.GenesisHash, Netuid: fixture.artifact.Netuid})
	if err != nil {
		t.Fatal(err)
	}
	fixture.expected.EarningSelection = selection
	fixture.artifact.ClosedWork.EarningStart = selection.StartTime.UTC().Format(time.RFC3339Nano)
	fixture.artifact.ClosedWork.EarningSelectionHash = selection.PolicyHash
	fixture.artifact.ClosedWork.EarningPolicyHash = "sha256:" + strings.Repeat("01", 32)
	for index := range fixture.artifact.ClosedWork.Records {
		row := &fixture.artifact.ClosedWork.Records[index]
		closed := cutoff
		if index == 0 {
			closed = cutoff.Add(-time.Microsecond)
		}
		participantSetOriginalTimes(t, fixture, row.ContractId, fixture.inventory.Clock.StartTime.Add(10*time.Second), closed)
		row.ClosedAt = closed.UTC().Format(time.RFC3339Nano)
		text := row.ClosedAt
		fixture.inventory.Window.Records[index].ClosedAt = &text
	}
	rebuildWholeWorkEarningTestArtifact(t, fixture, cutoff)
	return fixture
}

func TestWholeWorkStraddlingEarningCutoffRetainsFullOriginalEpoch(t *testing.T) {
	fixture := newWholeWorkEarningTestFixture(t)
	start := bytes.Clone(fixture.inventory.Clock.StartHeader)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Contracts != 2 || value.Credited != 2 || len(value.ReconciledContracts) != 2 || value.Reports == nil || value.Reports.ClosedWork.UsageBytes != 100 || value.Reports.ReservedAmountJoins != 2 {
		t.Fatal("straddling policy discarded original epoch or pre-cutoff proof", value, err)
	}
	if value.ExpectedProviders[0].UsageBytes != 100 || value.ExpectedProviders[1].UsageBytes != 0 || value.ExpectedProviders[2].UsageBytes != 0 || !bytes.Equal(start, fixture.inventory.Clock.StartHeader) || fixture.inventory.Window.Start != fixture.inventory.Clock.StartTime.Format(time.RFC3339Nano) {
		t.Fatal("earning selection changed physical epoch or exact boundary inclusion", value)
	}
}

func TestWholeWorkEarningDeclarationCannotChooseIndependentCutoff(t *testing.T) {
	fixture := newWholeWorkEarningTestFixture(t)
	fixture.expected.EarningSelection = nil
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("publisher cutoff declaration authorized itself", value, err)
	}
	if value, err := VerifyClosedWorkReports(t.Context(), fixture.artifact, fixture.expected.ClientKeyRootSigner); value != nil || !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("legacy report reader invented earning policy authority", value, err)
	}
	fixture = newWholeWorkEarningTestFixture(t)
	fixture.expected.EarningSelection.StartTime = fixture.expected.EarningSelection.StartTime.Add(time.Microsecond)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("conflicting independently selected earning cutoff accepted", err)
	}
}

func TestWholeWorkPreCutoffReportsRemainMandatoryOriginals(t *testing.T) {
	fixture := newWholeWorkEarningTestFixture(t)
	fixture.artifact.ClosedWork.Records[0].OriginalReports = nil
	closedWorkTestSign(t, fixture.artifact)
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("non-earning original was silently removed from complete proof", value, err)
	}
}

func TestWholeWorkEarningSelectionCannotBorrowPublisherCloseTime(t *testing.T) {
	fixture := newWholeWorkEarningTestFixture(t)
	changed := fixture.expected.EarningSelection.StartTime.Add(-time.Microsecond).Format(time.RFC3339Nano)
	fixture.artifact.ClosedWork.Records[1].ClosedAt = changed
	fixture.inventory.Window.Records[1].ClosedAt = &changed
	rebuildWholeWorkEarningTestArtifact(t, fixture, fixture.expected.EarningSelection.StartTime)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("publisher changed an original earning side of the boundary", err)
	}
}

func TestWholeWorkEarningIdentitySurvivesReadinessConfigRevision(t *testing.T) {
	fixture := newWholeWorkEarningTestFixture(t)
	fixture.artifact.ClosedWork.EarningPolicyHash = "sha256:" + strings.Repeat("02", 32)
	closedWorkTestSign(t, fixture.artifact)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.AttributionComplete || value.Reports.ClosedWork.UsageBytes != 100 {
		t.Fatal("readiness-only config audit invalidated immutable earning identity", value, err)
	}
	fixture.artifact.ClosedWork.EarningSelectionHash = "sha256:" + strings.Repeat("03", 32)
	closedWorkTestSign(t, fixture.artifact)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("different immutable earning identity survived audit transition", err)
	}
}

func TestWholeWorkEarningIdentityMatchesOriginalServerGrammar(t *testing.T) {
	identity := WholeWorkEarningIdentity{Schema: "urnetwork-provider-payout-transition-v1", CutoffUtc: "2026-10-06T00:00:00Z", Attribution: "close_time", LegacyUsdc: "before_cutoff_only", Profile: "mainnet", ChainId: 945, GenesisHash: "0x" + strings.Repeat("AB", 32), Netuid: 521}
	selection, err := NewWholeWorkEarningSelection(identity)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"schema":"urnetwork-provider-payout-transition-v1","cutoff_utc":"2026-10-06T00:00:00Z","attribution":"close_time","legacy_usdc":"before_cutoff_only","profile":"mainnet","chain_id":945,"genesis_hash":"0x` + strings.Repeat("ab", 32) + `","netuid":521}`)
	hash := sha256.Sum256(original)
	if selection.PolicyHash != "sha256:"+hex.EncodeToString(hash[:]) || !selection.StartTime.Equal(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("canonical earning identity differs from original Server grammar", selection)
	}
}

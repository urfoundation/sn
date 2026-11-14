// Capacity joins use the original SDK reservation and complete bilateral report
// chains. These fixtures keep actual signatures and full window originals;
// provider attribution remains a separate unproved component.
package payoutartifact

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"google.golang.org/protobuf/proto"
)

// The first contract has independently chosen endpoint totals and an actual
// reservation capacity. The second stays unchanged, exposing partial joins.
func reservedAmountFixture(t *testing.T, source, destination, capacity, credited uint64) *wholeWorkTestFixture {
	t.Helper()
	if credited%2 != 0 {
		t.Fatal("amount fixture requires two equal synthetic provider shares")
	}
	fixture := newWholeWorkTestFixture(t)
	row := &fixture.artifact.ClosedWork.Records[0]
	var reports ClosedWorkReports
	if err := json.Unmarshal(row.OriginalReports, &reports); err != nil {
		t.Fatal(err)
	}
	var terminalHeads [2][]byte
	for party, total := range []uint64{source, destination} {
		var previous [32]byte
		var cumulative uint64
		for step, amount := range []uint64{total / 2, total - total/2} {
			report := &reports.Reports[party*2+step]
			key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(80 + party*2 + step)}, 32))
			original, err := coreprotocol.DecodeOriginalCloseReport(report.Original)
			if err != nil {
				t.Fatal(err)
			}
			original.AckedByteCount = amount
			original, err = coreprotocol.SignOriginalCloseReport(original, key)
			if err != nil {
				t.Fatal(err)
			}
			report.Original, err = original.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			report.AckedBytes = &amount
			inventory, err := coreprotocol.DecodeOriginalCloseInventory(report.Inventory)
			if err != nil {
				t.Fatal(err)
			}
			cumulative += amount
			inventory.CumulativeAckedBytes, inventory.Previous = cumulative, previous
			inventory.ReportHash = sha256.Sum256(report.Original)
			inventory, err = coreprotocol.SignOriginalCloseInventory(inventory, key)
			if err != nil {
				t.Fatal(err)
			}
			report.Inventory, err = inventory.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			previous = sha256.Sum256(report.Inventory)
			if step == 1 {
				terminalHeads[party] = bytes.Clone(report.Inventory)
			}
		}
	}
	var err error
	row.OriginalReports, err = json.Marshal(reports)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := decodeClosedWorkSnapshot(*row, fixture.artifact.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	*snapshot.ByteCount = int64(credited)
	for index := range *snapshot.Providers {
		*(*snapshot.Providers)[index].ByteCount = int64(credited / 2)
	}
	row.Original, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	fixture.inventory.Window.Records[0].Original = bytes.Clone(row.Original)
	for owner := range fixture.inventory.Owners {
		fixture.changeCut(t, owner, 1, func(cut *coreprotocol.OriginalWorkCut) {
			original := &cut.Contracts[0]
			var stored coreprotocol.StoredContract
			if err := proto.Unmarshal(original.StoredContract, &stored); err != nil {
				t.Fatal(err)
			}
			stored.TransferByteCount = capacity
			original.StoredContract, err = proto.Marshal(&stored)
			if err != nil {
				t.Fatal(err)
			}
			original.LatestInventory = bytes.Clone(terminalHeads[owner])
		})
	}
	old := fixture.artifact
	providers := append([]ProviderInput(nil), old.Providers...)
	for index := range providers {
		providers[index].UsageBytes = 50 + credited/2
	}
	created, err := time.Parse(time.RFC3339Nano, old.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	fixture.artifact, err = BuildWithContext(t.Context(), BuildInput{
		ClosedWork: old.ClosedWork, DeploymentID: old.DeploymentID, GenesisHash: old.GenesisHash,
		PolicyHash: old.PolicyHash, ChainID: old.ChainID, Netuid: old.Netuid,
		Coordinator: old.Coordinator, SettlementVault: old.SettlementVault,
		Epoch: old.Epoch, NoID: old.NoID, Start: old.Start, End: old.End,
		OperatorSnapshotHash: old.OperatorSnapshotHash, FleetSnapshotHash: old.FleetSnapshotHash,
		Providers: providers, TotalUsers: old.TotalUsers, ReliabilityAMin: old.ReliabilityAMin, CreatedAt: created,
	})
	if err != nil {
		t.Fatal(err)
	}
	closedWorkTestSign(t, fixture.artifact)
	if err := VerifyWithContext(t.Context(), fixture.artifact); err != nil {
		t.Fatal("amount fixture lost canonical signed payout custody", err)
	}
	return fixture
}

// Reservations come from the decoded original SDK cut, not from the payout
// summary whose credited bytes are being independently checked.
func reservedAmountOriginals(t *testing.T, fixture *wholeWorkTestFixture) map[[16]byte]coreprotocol.OriginalWorkContract {
	t.Helper()
	cut, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), fixture.inventory.Owners[0].End)
	if err != nil {
		t.Fatal(err)
	}
	originals := make(map[[16]byte]coreprotocol.OriginalWorkContract, len(cut.Contracts))
	for _, original := range cut.Contracts {
		originals[original.ContractId] = original
	}
	return originals
}

// Unequal terminal reports credit the smaller completed amount. The former
// arithmetic mean rejected this genuine producer result at the full witness.
func TestWholeWorkReservedAmountsAcceptOriginalBilateralMinimum(t *testing.T) {
	fixture := reservedAmountFixture(t, 80, 100, 100, 80)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || value.Credited != 2 || len(value.ExpectedProviders) != 2 || value.ExpectedProviders[0].UsageBytes != 90 || value.ExpectedProviders[1].UsageBytes != 90 {
		t.Fatal("genuine bilateral minimum did not survive full original witness", value, err)
	}
}

// Even mutually signed reports cannot increase the original reserved capacity.
func TestWholeWorkReservedAmountsAcceptOriginalCapacityCap(t *testing.T) {
	fixture := reservedAmountFixture(t, 120, 140, 100, 100)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || value.Credited != 2 || value.ExpectedProviders[0].UsageBytes != 100 || value.ExpectedProviders[1].UsageBytes != 100 {
		t.Fatal("original capacity was not applied to complete endpoint reports", value, err)
	}
}

// The operator can re-sign its false SQL total and all derived shares. Those
// signatures cannot authorize a mean or an amount above the actual reservation.
func TestWholeWorkReservedAmountsRejectResignedMeanAndOverCapacity(t *testing.T) {
	for _, values := range []struct {
		source, destination, capacity, credited uint64
	}{
		{source: 80, destination: 100, capacity: 100, credited: 90},
		{source: 120, destination: 140, capacity: 100, credited: 130},
		{source: 120, destination: 120, capacity: 100, credited: 120},
	} {
		fixture := reservedAmountFixture(t, values.source, values.destination, values.capacity, values.credited)
		value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
		if value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
			t.Fatalf("re-signed false amount %+v acquired whole-work authority: %+v %v", values, value, err)
		}
	}
}

// A selected endpoint amount can be a genuine dispute outcome. Without the
// original adjudication, it is unavailable rather than falsely proved settled.
func TestWholeWorkReservedAmountsKeepUnprovedDisputeOutcomeUnknown(t *testing.T) {
	fixture := reservedAmountFixture(t, 80, 100, 100, 100)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if value != nil || !errors.Is(err, ErrClosedWorkUnavailable) || errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("missing original dispute outcome became proved settlement or false corruption", value, err)
	}
}

// Both terminal originals can survive without their earlier checkpoints. Their
// visible increment subtotal cannot prove that the full original total is false.
func TestWholeWorkReservedAmountsKeepMissingBilateralPrefixesUnknown(t *testing.T) {
	fixture := reservedAmountFixture(t, 100, 100, 100, 100)
	changeClosedReports(t, fixture.artifact, func(reports *ClosedWorkReports) {
		reports.Reports = []ClosedWorkReport{reports.Reports[1], reports.Reports[3]}
		reports.Count = 2
	})
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if value != nil || !errors.Is(err, ErrClosedWorkUnavailable) || errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("missing original checkpoints turned partial totals into false corruption", value, err)
	}
}

// Legacy signed report chains remain visible but cannot assert capacity proof;
// an empty supplied reservation map and a partial one are not complete either.
func TestClosedReportsReservedAmountsRequireEveryOriginalReservation(t *testing.T) {
	fixture := reservedAmountFixture(t, 100, 100, 100, 100)
	value, err := VerifyClosedWorkReports(t.Context(), fixture.artifact, fixture.expected.ClientKeyRootSigner)
	if err != nil || value == nil || value.AmountJoins != 2 || value.CompleteReportInventories != 2 || value.ReservedAmountJoins != 0 {
		t.Fatal("standalone reports manufactured original capacity authority", value, err)
	}
	for _, keep := range []int{0, 1} {
		originals := reservedAmountOriginals(t, fixture)
		for index, row := range fixture.artifact.ClosedWork.Records {
			if index >= keep {
				delete(originals, row.ContractId)
			}
		}
		value, err := verifyWholeWorkReports(t.Context(), fixture.artifact, fixture.expected.ClientKeyRootSigner, originals, fixture.authority.ExpectedProviders)
		if err != nil || value == nil || value.ReservedAmountJoins != uint64(keep) {
			t.Fatal("incomplete reservation census waived a missing original", value, err)
		}
	}
}

// Correctly signed reports cannot be relabelled as the other endpoint's
// original reservation; this verifies the private amount join itself.
func TestClosedReportsReservedAmountsBindOriginalEndpointRoles(t *testing.T) {
	fixture := reservedAmountFixture(t, 100, 100, 100, 100)
	originals := reservedAmountOriginals(t, fixture)
	id := fixture.artifact.ClosedWork.Records[0].ContractId
	original := originals[id]
	var stored coreprotocol.StoredContract
	if err := proto.Unmarshal(original.StoredContract, &stored); err != nil {
		t.Fatal(err)
	}
	stored.SourceId, stored.DestinationId = stored.DestinationId, stored.SourceId
	var err error
	original.StoredContract, err = proto.Marshal(&stored)
	if err != nil {
		t.Fatal(err)
	}
	originals[id] = original
	value, err := verifyWholeWorkReports(t.Context(), fixture.artifact, fixture.expected.ClientKeyRootSigner, originals, fixture.authority.ExpectedProviders)
	if value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("original endpoint roles were replaced by signed report labels", value, err)
	}
}

// An owner that is already canceled never exposes a prior or partial amount
// projection, including through the new complete-reservation entry point.
func TestClosedReportsReservedAmountsHonorCanceledOwner(t *testing.T) {
	fixture := reservedAmountFixture(t, 100, 100, 100, 100)
	originals := reservedAmountOriginals(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	value, err := verifyWholeWorkReports(ctx, fixture.artifact, fixture.expected.ClientKeyRootSigner, originals, fixture.authority.ExpectedProviders)
	if value != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled original amount owner exposed a projection", value, err)
	}
}

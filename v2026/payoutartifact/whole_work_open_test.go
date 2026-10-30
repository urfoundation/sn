// Still-open measurement uses the live owner's signed null observation, never
// a publisher's window label or an undated SDK checkpoint.
package payoutartifact

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Preserve the original request and reservation while leaving both SDK owners
// nonterminal. Only the independent live source supplies the boundary state.
func participantMakeStillOpen(t *testing.T, fixture *wholeWorkTestFixture, id [16]byte) protocol.ProviderWorkReceipt {
	t.Helper()
	for owner := range fixture.inventory.Owners {
		fixture.changeCut(t, owner, 1, func(cut *coreprotocol.OriginalWorkCut) {
			for index := range cut.Contracts {
				if cut.Contracts[index].ContractId == id {
					cut.Contracts[index].LatestInventory = nil
				}
			}
		})
	}
	rows := []ClosedWorkRecord{}
	for _, row := range fixture.artifact.ClosedWork.Records {
		if row.ContractId != id {
			rows = append(rows, row)
		}
	}
	fixture.artifact.ClosedWork.Records, fixture.artifact.ClosedWork.Count = rows, uint64(len(rows))
	for index := range fixture.inventory.Window.Records {
		row := &fixture.inventory.Window.Records[index]
		if row.ContractId == participantTestId(id) {
			row.Disposition, row.ClosedAt, row.Original = "open", nil, nil
		}
	}
	originals := [][]byte{}
	var reservationHash [32]byte
	for _, raw := range fixture.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Outcome != nil && original.Outcome.ContractId == participantTestId(id) {
			continue
		}
		if original.Reservation != nil && original.Reservation.ContractId == participantTestId(id) {
			reservationHash = sha256.Sum256(raw)
		}
		originals = append(originals, raw)
	}
	if reservationHash == ([32]byte{}) {
		t.Fatal("still-open fixture lacks its original reservation")
	}
	source := fixture.authority.WorkSources[0]
	original := protocol.ProviderWorkReceipt{DomainHash: source.DomainHash, SourceId: source.SourceId, Generation: source.Generation, Open: &protocol.ProviderWorkOpenObservation{
		ContractId: participantTestId(id), ReservationHash: reservationHash, Epoch: fixture.artifact.Epoch, Block: fixture.artifact.End.Number,
		BlockHash: [32]byte(commonWholeWorkHash(t, fixture.artifact.End.Hash)), BoundaryUnixMicro: fixture.inventory.Clock.EndTime.UnixMicro(), ObservedAtUnixMicro: fixture.inventory.Clock.EndTime.Add(time.Microsecond).UnixMicro(),
	}}
	original, err := protocol.SignProviderWorkReceipt(t.Context(), original, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{121}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := original.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.inventory.AttributionOriginals = append(originals, raw)
	creationRebuildArtifact(t, fixture)
	// The common publisher fixture rewrites rows by index. This mixed window
	// retains open rows, so restore exact identities after its aggregate rebuild.
	for index := range fixture.inventory.Window.Records {
		row := &fixture.inventory.Window.Records[index]
		row.Original = nil
		for _, credited := range fixture.artifact.ClosedWork.Records {
			if row.ContractId == participantTestId(credited.ContractId) {
				row.Original = bytes.Clone(credited.Original)
			}
		}
	}
	return original
}

// Keep the independently valid signature while reaching the semantic join.
func participantChangeStillOpen(t *testing.T, fixture *wholeWorkTestFixture, change func(*protocol.ProviderWorkOpenObservation)) {
	t.Helper()
	for index, raw := range fixture.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Open == nil {
			continue
		}
		change(original.Open)
		original, err = protocol.SignProviderWorkReceipt(t.Context(), original, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{121}, ed25519.SeedSize)))
		if err != nil {
			t.Fatal(err)
		}
		fixture.inventory.AttributionOriginals[index], err = original.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("still-open fixture lacks its original observation")
}

func TestWholeWorkStillOpenOriginalCompletesMixedProviderUniverse(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Contracts != 2 || value.Open != 1 || value.Credited != 1 || len(value.ExpectedProviders) != 3 || value.ExpectedProviders[0].UsageBytes != 0 || value.ExpectedProviders[1].UsageBytes != 100 || value.ExpectedProviders[2].UsageBytes != 0 {
		t.Fatalf("original live open observation did not complete the mixed census: %+v, %v", value, err)
	}
}

func TestWholeWorkStillOpenOnlyWindowHasCompleteZeroProviderVector(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	ids := [][16]byte{fixture.artifact.ClosedWork.Records[0].ContractId, fixture.artifact.ClosedWork.Records[1].ContractId}
	for _, id := range ids {
		participantMakeStillOpen(t, fixture, id)
	}
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Contracts != 2 || value.Open != 2 || value.Credited != 0 || len(value.ExpectedProviders) != 3 {
		t.Fatalf("original open-only work did not complete the zero census: %+v, %v", value, err)
	}
	for _, provider := range value.ExpectedProviders {
		if provider.UsageBytes != 0 {
			t.Fatalf("still-open contract credited provider bytes: %+v", provider)
		}
	}
}

func TestWholeWorkStillOpenNeedsExactOriginalObservation(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
	fixture.inventory.AttributionOriginals = fixture.inventory.AttributionOriginals[:len(fixture.inventory.AttributionOriginals)-1]
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete || value.Open != 1 {
		t.Fatalf("publisher open label or SDK nonterminal replaced the live original: %+v, %v", value, err)
	}
}

func TestWholeWorkStillOpenCannotBorrowAnotherBoundary(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*protocol.ProviderWorkOpenObservation)
	}{
		{name: "epoch", mutate: func(open *protocol.ProviderWorkOpenObservation) { open.Epoch++ }},
		{name: "block", mutate: func(open *protocol.ProviderWorkOpenObservation) { open.Block++ }},
		{name: "block hash", mutate: func(open *protocol.ProviderWorkOpenObservation) { open.BlockHash[0] ^= 1 }},
	} {
		fixture := participantEvidenceFixture(t)
		participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
		participantChangeStillOpen(t, fixture, change.mutate)
		value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
		if err != nil || value == nil || !value.Complete || value.AttributionComplete {
			t.Fatalf("another %s supplied this phase's open proof: %+v, %v", change.name, value, err)
		}
	}
}

func TestWholeWorkStillOpenRejectsOriginalReservationRebind(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
	participantChangeStillOpen(t, fixture, func(open *protocol.ProviderWorkOpenObservation) { open.ReservationHash[0] ^= 1 })
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("open observation rebound to another reservation: %+v, %v", value, err)
	}
}

func TestWholeWorkStillOpenRejectsChangedRecoveredBoundaryClock(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
	participantChangeStillOpen(t, fixture, func(open *protocol.ProviderWorkOpenObservation) { open.BoundaryUnixMicro-- })
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("open observation replaced the recovered Frontier clock: %+v, %v", value, err)
	}
}

func TestWholeWorkStillOpenNeedsSourceAtActualObservation(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	open := participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
	fixture.authority.WorkSources[0].ThroughUnixMicro = open.Open.ObservedAtUnixMicro
	fixture.signAuthority(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatalf("expired source supplied an open observation: %+v, %v", value, err)
	}
}

// Preserve the exact original reservation when adding a genuine terminal
// event after the selected SDK cuts, independently of publisher row timing.
func participantAppendLaterOutcome(t *testing.T, fixture *wholeWorkTestFixture, open protocol.ProviderWorkReceipt, closedAt int64) {
	t.Helper()
	original := protocol.ProviderWorkReceipt{DomainHash: open.DomainHash, SourceId: open.SourceId, Generation: open.Generation, Outcome: &protocol.ProviderWorkOutcome{
		ContractId: open.Open.ContractId, ReservationHash: open.Open.ReservationHash, ClosedAtUnixMicro: closedAt,
		Outcome: "settled", SourceBytes: 100, DestinationBytes: 100, Capacity: 100, SourceComplete: true, DestinationComplete: true,
	}}
	original, err := protocol.SignProviderWorkReceipt(t.Context(), original, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{121}, ed25519.SeedSize)))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := original.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.inventory.AttributionOriginals = append(fixture.inventory.AttributionOriginals, raw)
}

func TestWholeWorkStillOpenRejectsEarlierOriginalSettlement(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	open := participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
	participantAppendLaterOutcome(t, fixture, open, open.Open.ObservedAtUnixMicro-1)
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("original null observation followed an earlier terminal: %+v, %v", value, err)
	}
}

func TestWholeWorkStillOpenAllowsSameMicrosecondOriginalSettlement(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	open := participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
	participantAppendLaterOutcome(t, fixture, open, open.Open.ObservedAtUnixMicro)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Open != 1 || value.Credited != 1 {
		t.Fatalf("serialized open then close in one microsecond was rejected: %+v, %v", value, err)
	}
}

func TestWholeWorkStillOpenRejectsForkedPhaseObservation(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
	first := bytes.Clone(fixture.inventory.AttributionOriginals[len(fixture.inventory.AttributionOriginals)-1])
	participantChangeStillOpen(t, fixture, func(open *protocol.ProviderWorkOpenObservation) { open.ObservedAtUnixMicro++ })
	fixture.inventory.AttributionOriginals = append(fixture.inventory.AttributionOriginals, first)
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("second original replaced the first observation for one phase: %+v, %v", value, err)
	}
}

func TestWholeWorkStillOpenRetainsDistinctEarlierBoundaryObservations(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	participantMakeStillOpen(t, fixture, fixture.artifact.ClosedWork.Records[1].ContractId)
	current := bytes.Clone(fixture.inventory.AttributionOriginals[len(fixture.inventory.AttributionOriginals)-1])
	participantChangeStillOpen(t, fixture, func(open *protocol.ProviderWorkOpenObservation) {
		open.Epoch--
		open.Block--
		open.BlockHash[0] ^= 1
		open.BoundaryUnixMicro = fixture.inventory.Clock.StartTime.Add(time.Minute).UnixMicro()
		open.ObservedAtUnixMicro = open.BoundaryUnixMicro + 1
	})
	fixture.inventory.AttributionOriginals = append(fixture.inventory.AttributionOriginals, current)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Open != 1 {
		t.Fatalf("retained prior observation conflicted with the exact current phase: %+v, %v", value, err)
	}
	fixture.inventory.AttributionOriginals = fixture.inventory.AttributionOriginals[:len(fixture.inventory.AttributionOriginals)-1]
	value, err = VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatalf("retained prior observation substituted for the missing current one: %+v, %v", value, err)
	}
}

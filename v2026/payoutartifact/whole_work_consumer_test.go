// The complete consumer reuses verified report facts without re-entering the
// narrower legacy provider census or granting attribution from those counters.
package payoutartifact

import (
	"errors"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Reserved amounts and complete report chains survive the public whole-work
// boundary, while their presence alone still cannot authenticate participants.
func TestWholeWorkVerifiedReportsReachCompleteConsumer(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || value.Reports == nil {
		t.Fatal("complete consumer lost the verified original report result", value, err)
	}
	if value.Reports.Contracts != 2 || value.Reports.ReservedAmountJoins != 2 || value.Reports.CompleteReportInventories != 2 || value.Reports.SignedReports == 0 || value.Reports.RegisteredReports != value.Reports.SignedReports || value.AttributionComplete {
		t.Fatal("report result lost original joins or manufactured participant authority", value.Reports, value.AttributionComplete)
	}
}

// Sidecar additions share the original whole-witness bound; optional proof
// transport cannot widen the public parser or accumulate unbounded originals.
func TestWholeWorkAttributionOriginalTransportSharesWitnessCapacity(t *testing.T) {
	fixture := newWholeWorkTestFixture(t)
	fixture.inventory.AttributionOriginals = [][]byte{make([]byte, MaxWholeWorkInventoryBytes)}
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkCapacity) {
		t.Fatal("optional attribution originals bypassed complete witness capacity", value, err)
	}
	fixture.inventory.AttributionOriginals = make([][]byte, MaxClosedWorkRecords+1)
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkCapacity) {
		t.Fatal("optional attribution original count became unbounded", value, err)
	}
	fixture.inventory.AttributionOriginals = [][]byte{make([]byte, protocol.MaximumProviderWorkReceiptBytes+1)}
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkCapacity) {
		t.Fatal("one attribution receipt exceeded its original wire profile", value, err)
	}
}

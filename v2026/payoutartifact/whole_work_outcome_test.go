// Full original admission witnesses select settlement policy independently of
// the signed payout's byte count, including asymmetric completed endpoints.
package payoutartifact

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Replace the first contract's complete SDK report chains and the actual live
// outcome together. Its independent creation, owner roster and parties persist.
func wholeWorkOutcomeFixture(t *testing.T, source, destination, credited uint64, outcome string) *wholeWorkTestFixture {
	t.Helper()
	fixture := participantEvidenceFixture(t)
	reports := reservedAmountFixture(t, source, destination, 100, credited)
	if fixture.artifact.ClosedWork.Records[0].ContractId != reports.artifact.ClosedWork.Records[0].ContractId {
		t.Fatal("synthetic original contract identities differ")
	}
	row := &fixture.artifact.ClosedWork.Records[0]
	row.OriginalReports = bytes.Clone(reports.artifact.ClosedWork.Records[0].OriginalReports)
	for owner := 0; owner < 2; owner++ {
		original, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), reports.inventory.Owners[owner].End)
		if err != nil {
			t.Fatal(err)
		}
		fixture.changeCut(t, owner, 1, func(cut *coreprotocol.OriginalWorkCut) {
			cut.Contracts[0].LatestInventory = bytes.Clone(original.Contracts[0].LatestInventory)
		})
	}
	snapshot, err := decodeClosedWorkSnapshot(*row, fixture.artifact.Epoch)
	if err != nil || len(*snapshot.Providers) != 1 {
		t.Fatal("original single egress fixture differs", err)
	}
	*snapshot.ByteCount = int64(credited)
	*(*snapshot.Providers)[0].ByteCount = int64(credited)
	row.Original, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	creationRebuildArtifact(t, fixture)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{121}, ed25519.SeedSize))
	changed := false
	for index, raw := range fixture.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Outcome == nil || original.Outcome.ContractId != participantTestId(row.ContractId) {
			continue
		}
		original.Outcome.SourceBytes, original.Outcome.DestinationBytes, original.Outcome.Outcome = source, destination, outcome
		original, err = protocol.SignProviderWorkReceipt(t.Context(), original, key)
		if err != nil {
			t.Fatal(err)
		}
		fixture.inventory.AttributionOriginals[index], err = original.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		changed = true
	}
	if !changed {
		t.Fatal("original outcome fixture lost its live receipt")
	}
	return fixture
}

// A real selected destination amount may exceed the bilateral minimum while
// staying within the exact original reservation and terminal report total.
func TestWholeWorkOriginalOutcomeAuthenticatesSelectedDestinationAmount(t *testing.T) {
	fixture := wholeWorkOutcomeFixture(t, 80, 100, 100, "dispute_resolved_to_destination")
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Reports == nil || value.Reports.ReservedAmountJoins != 2 || value.ExpectedProviders[1].UsageBytes != 100 {
		t.Fatal("original destination adjudication did not reach full consumer", value, err)
	}
}

// The original source policy is distinct from blindly choosing the greater
// endpoint or always crediting the destination's completed report.
func TestWholeWorkOriginalOutcomeAuthenticatesSelectedSourceAmount(t *testing.T) {
	fixture := wholeWorkOutcomeFixture(t, 100, 80, 100, "dispute_resolved_to_source")
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Reports.ReservedAmountJoins != 2 || value.ExpectedProviders[1].UsageBytes != 100 {
		t.Fatal("original source adjudication did not reach full consumer", value, err)
	}
}

// Ordinary settlement still follows the original smaller completed report.
func TestWholeWorkOriginalOutcomeAuthenticatesAsymmetricSettlement(t *testing.T) {
	fixture := wholeWorkOutcomeFixture(t, 80, 100, 80, "settled")
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Reports.ReservedAmountJoins != 2 || value.ExpectedProviders[1].UsageBytes != 80 {
		t.Fatal("original ordinary settlement lost its asymmetric amount", value, err)
	}
}

// A publisher may resign a smaller amount and all matching provider rows. Its
// valid signature cannot replace the actual original selected outcome.
func TestWholeWorkOriginalOutcomeRejectsResignedBilateralFallback(t *testing.T) {
	fixture := wholeWorkOutcomeFixture(t, 80, 100, 80, "dispute_resolved_to_destination")
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("resigned bilateral minimum changed original adjudication", value, err)
	}
}

// Source signatures cannot select their own admission role. Without independent
// purpose approval, the otherwise valid original remains unavailable evidence.
func TestWholeWorkOriginalOutcomeRequiresIndependentAttributionPurpose(t *testing.T) {
	fixture := wholeWorkOutcomeFixture(t, 80, 100, 100, "dispute_resolved_to_destination")
	fixture.expected.AttributionSigner = common.Address{}
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkUnavailable) || errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("unadmitted source selected a dispute amount", value, err)
	}
}

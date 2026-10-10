// Prior stream dependencies come from a complete admitted window. These tests
// preserve that original result while changing current lookup or input claims.
package payoutartifact

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Produce the retained inputs through the same public full verifier used when
// admitting an original census, never by constructing a verified-result flag.
func priorCreationTestOriginals(t *testing.T) (*wholeWorkTestFixture, *VerifiedWholeWorkInventory, WholeWorkExpectation) {
	t.Helper()
	fixture := participantEvidenceFixture(t)
	verified, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || verified == nil || !verified.Complete || !verified.AttributionComplete || len(verified.RetainedCreations) != 2 {
		t.Fatal("actual complete prior did not retain its original creation dependencies", verified, err)
	}
	expected := fixture.expected
	expected.PriorContracts = append([]WholeWorkPriorContract(nil), verified.ReconciledContracts...)
	expected.PriorCreations = append([]WholeWorkRetainedCreation(nil), verified.RetainedCreations...)
	return fixture, verified, expected
}

func TestWholeWorkPriorCreationRetainsExactOriginalAfterFullAdmission(t *testing.T) {
	fixture, verified, expected := priorCreationTestOriginals(t)
	cut, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), fixture.inventory.Owners[0].End)
	if err != nil {
		t.Fatal(err)
	}
	for index, retained := range verified.RetainedCreations {
		if !bytes.Equal(retained.Original.OriginalCreation, cut.Contracts[index].OriginalCreation) || retained.Checkpoint != verified.ReconciledContracts[index] || retained.Owner != fixture.authority.Owners[0] {
			t.Fatal("retained original was reconstructed or selected from another SDK generation")
		}
	}
	domainHash, err := fixture.authority.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	next := *fixture.artifact
	next.Epoch++
	values, err := verifyWholeWorkPriorCreations(t.Context(), &next, domainHash, expected)
	if err != nil || len(values) != 2 {
		t.Fatal("independent retained originals could not survive retired current generation", values, err)
	}
	first := verified.RetainedCreations[0]
	first.Original.OriginalCreation[0] ^= 1
	if !bytes.Equal(fixture.inventory.Owners[0].End, mustPriorCreationCutBytes(t, cut)) {
		t.Fatal("returned prior dependency aliased the original admitted cut")
	}
}

// Readback uses the exact decoded canonical cut, not a regenerated signature.
func mustPriorCreationCutBytes(t *testing.T, value coreprotocol.OriginalWorkCut) []byte {
	t.Helper()
	raw, err := value.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWholeWorkPriorCreationNeedsIndependentCheckpointAndExactOwner(t *testing.T) {
	fixture, _, expected := priorCreationTestOriginals(t)
	domainHash, err := fixture.authority.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	next := *fixture.artifact
	next.Epoch++
	missing := expected
	missing.PriorContracts = nil
	if _, err := verifyWholeWorkPriorCreations(t.Context(), &next, domainHash, missing); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("original creation authorized an absent prior admission", err)
	}
	changed := expected
	changed.PriorCreations = append([]WholeWorkRetainedCreation(nil), expected.PriorCreations...)
	changed.PriorCreations[0].Checkpoint.InventoryHash = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	if _, err := verifyWholeWorkPriorCreations(t.Context(), &next, domainHash, changed); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("current claim replaced the independently retained original checkpoint", err)
	}
	changed.PriorCreations[0] = expected.PriorCreations[0]
	changed.PriorCreations[0].Owner.Generation[0] ^= 1
	if _, err := verifyWholeWorkPriorCreations(t.Context(), &next, domainHash, changed); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("current SDK generation borrowed a retired original creation", err)
	}
	if _, err := verifyWholeWorkPriorCreations(t.Context(), fixture.artifact, domainHash, expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("same-window candidate was accepted as a prior admission", err)
	}
}

func TestWholeWorkPriorCreationRequestsRequireOriginalSourceApproval(t *testing.T) {
	fixture := creationEvidenceFixture(t, false, [16]byte{3})
	participantAddIdleOwner(t, fixture)
	participantAttachOriginals(t, fixture)
	ids, err := ReadWholeWorkPriorCreationRequests(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || len(ids) != 2 || ids[0] != fixture.artifact.ClosedWork.Records[0].ContractId || ids[1] != fixture.artifact.ClosedWork.Records[1].ContractId {
		t.Fatal("signed original stream origins did not reach exact prior lookup", ids, err)
	}
	unknown := fixture.expected
	unknown.AttributionSigner = common.Address{}
	if _, err := ReadWholeWorkPriorCreationRequests(t.Context(), fixture.artifact, fixture.inventory, unknown); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("candidate original selected its own prior lookup purpose", err)
	}
	fixture.authority.WorkSources = nil
	fixture.signAuthority(t)
	if _, err := ReadWholeWorkPriorCreationRequests(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkUnavailable) {
		t.Fatal("unsigned source selection requested prior original authority", err)
	}
}

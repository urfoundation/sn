// Full signed witnesses retain original requests while the publisher mutates
// its own allocation. Aggregate equality must not hide a changed earning party.
package payoutartifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"google.golang.org/protobuf/proto"
)

// The source key signs an outgoing frame and the exact response for every
// original reservation. Requests are built independently of payout SQL rows.
func creationEvidenceFixture(t *testing.T, duplicateRequest bool, intermediaries ...[16]byte) *wholeWorkTestFixture {
	t.Helper()
	fixture := newWholeWorkTestFixture(t)
	fixture.changeCut(t, 0, 1, func(cut *coreprotocol.OriginalWorkCut) {
		for index := range cut.Contracts {
			original := &cut.Contracts[index]
			var stored coreprotocol.StoredContract
			if err := proto.Unmarshal(original.StoredContract, &stored); err != nil {
				t.Fatal(err)
			}
			version := uint32(1)
			requested := &coreprotocol.CreateContract{DestinationId: bytes.Clone(stored.DestinationId), TransferByteCount: 100, StreamVersion: &version, Companion: index == 1}
			for _, id := range intermediaries {
				requested.IntermediaryIds = append(requested.IntermediaryIds, id[:])
			}
			if len(intermediaries) != 0 {
				stored.StreamId = []byte{111, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, byte(index + 1)}
				var err error
				original.StoredContract, err = proto.Marshal(&stored)
				if err != nil {
					t.Fatal(err)
				}
			}
			requestBody, err := proto.Marshal(requested)
			if err != nil {
				t.Fatal(err)
			}
			requestFrame, err := proto.Marshal(&coreprotocol.Frame{MessageType: coreprotocol.MessageType_TransferCreateContract, MessageBytes: requestBody})
			if err != nil {
				t.Fatal(err)
			}
			requestId := [16]byte{byte(index + 112)}
			if duplicateRequest {
				requestId = [16]byte{112}
			}
			request, err := coreprotocol.SignOriginalContractRequest(t.Context(), coreprotocol.OriginalContractRequest{DomainHash: cut.DomainHash, ClientId: cut.ClientId, Generation: cut.Generation, RequestId: requestId, RequestFrame: requestFrame}, fixture.ownerKeys[0])
			if err != nil {
				t.Fatal(err)
			}
			requestRaw, err := request.Bytes(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			mode := coreprotocol.ProvideMode_Public
			if requested.Companion {
				mode = coreprotocol.ProvideMode_Network
			}
			responseBody, err := proto.Marshal(&coreprotocol.CreateContractResult{CreateContract: requested, Contract: &coreprotocol.Contract{StoredContractBytes: original.StoredContract, ProvideMode: mode}})
			if err != nil {
				t.Fatal(err)
			}
			responseFrame, err := proto.Marshal(&coreprotocol.Frame{MessageType: coreprotocol.MessageType_TransferCreateContractResult, MessageBytes: responseBody})
			if err != nil {
				t.Fatal(err)
			}
			admission, err := coreprotocol.SignOriginalContractAdmission(t.Context(), coreprotocol.OriginalContractAdmission{Request: requestRaw, ResultFrame: responseFrame}, fixture.ownerKeys[0])
			if err != nil {
				t.Fatal(err)
			}
			original.OriginalCreation, err = admission.Bytes(t.Context())
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	if len(intermediaries) != 0 {
		source, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), fixture.inventory.Owners[0].End)
		if err != nil {
			t.Fatal(err)
		}
		fixture.changeCut(t, 1, 1, func(cut *coreprotocol.OriginalWorkCut) {
			for index := range cut.Contracts {
				cut.Contracts[index].StoredContract = bytes.Clone(source.Contracts[index].StoredContract)
			}
		})
	}
	for index := range fixture.artifact.ClosedWork.Records {
		row := &fixture.artifact.ClosedWork.Records[index]
		snapshot, err := decodeClosedWorkSnapshot(*row, fixture.artifact.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		// The first request consumes at source; the normalized companion
		// consumes at destination. Each has one actual egress here.
		providerIndex := 1 - index
		provider := (*snapshot.Providers)[providerIndex]
		*provider.ByteCount = 100
		*snapshot.Providers = append((*snapshot.Providers)[:0], provider)
		row.Original, err = json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
	}
	creationRebuildArtifact(t, fixture)
	return fixture
}

// Rebuild and resign all publisher-controlled commitments after changing SQL.
// Original SDK cuts and client close reports remain byte-identical.
func creationRebuildArtifact(t *testing.T, fixture *wholeWorkTestFixture) {
	t.Helper()
	old := fixture.artifact
	providers := append([]ProviderInput(nil), old.Providers...)
	for index := range providers {
		providers[index].UsageBytes = 0
	}
	for rowIndex, row := range old.ClosedWork.Records {
		snapshot, err := decodeClosedWorkSnapshot(row, old.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		for _, original := range *snapshot.Providers {
			id, err := closedWorkId(original.ClientId)
			if err != nil {
				t.Fatal(err)
			}
			for index := range providers {
				if providers[index].ClientID == id {
					providers[index].UsageBytes += uint64(*original.ByteCount)
				}
			}
		}
		fixture.inventory.Window.Records[rowIndex].Original = bytes.Clone(row.Original)
	}
	for index := range providers {
		if providers[index].UsageBytes == 0 {
			providers[index].Eligible = false
		}
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
}

func TestWholeWorkOriginalRequestsDoNotClaimMissingSessionCompleteness(t *testing.T) {
	fixture := creationEvidenceFixture(t, false)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatal("request endpoint proofs became a complete extender/session census", value, err)
	}
}

func TestWholeWorkOriginalRequestRejectsResignedEqualTotalWrongEarningParty(t *testing.T) {
	fixture := creationEvidenceFixture(t, false)
	originalCut := bytes.Clone(fixture.inventory.Owners[0].End)
	row := &fixture.artifact.ClosedWork.Records[0]
	snapshot, err := decodeClosedWorkSnapshot(*row, fixture.artifact.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	// The source receives the entire same 100 bytes; the actual original
	// destination still exists in the independently expected provider roster.
	other, err := decodeClosedWorkSnapshot(fixture.artifact.ClosedWork.Records[1], fixture.artifact.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	(*snapshot.Providers)[0].ClientId = (*other.Providers)[0].ClientId
	(*snapshot.Providers)[0].NetworkId = (*other.Providers)[0].NetworkId
	row.Original, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	creationRebuildArtifact(t, fixture)
	if !bytes.Equal(originalCut, fixture.inventory.Owners[0].End) {
		t.Fatal("fixture changed the actual original request witness")
	}
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("equal total and resigned payout concealed a changed original earning endpoint", err)
	}
}

func TestWholeWorkOriginalRequestCannotEarnAsTwoDifferentReservations(t *testing.T) {
	fixture := creationEvidenceFixture(t, true)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("one source request was counted as two reservation admissions", err)
	}
}

func TestWholeWorkOriginalRequestRejectsConsumerAddedBesideActualEgress(t *testing.T) {
	fixture := creationEvidenceFixture(t, false)
	row := &fixture.artifact.ClosedWork.Records[0]
	snapshot, err := decodeClosedWorkSnapshot(*row, fixture.artifact.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	other, err := decodeClosedWorkSnapshot(fixture.artifact.ClosedWork.Records[1], fixture.artifact.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	consumer, egress := (*other.Providers)[0], (*snapshot.Providers)[0]
	half := int64(50)
	consumer.ByteCount, egress.ByteCount = &half, &half
	*snapshot.Providers = append((*snapshot.Providers)[:0], consumer, egress)
	row.Original, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	creationRebuildArtifact(t, fixture)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("the original consumer received an equal split beside the true egress", err)
	}
}

func TestWholeWorkOriginalStreamRequestRequiresItsActualIntermediary(t *testing.T) {
	fixture := creationEvidenceFixture(t, false, [16]byte{113})
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("publisher omitted a participant retained by original stream admission", err)
	}
}

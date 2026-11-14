// The SDK owns its own request and response signatures. The independent live
// reservation must also bind what the server received and the resulting direction.
package payoutartifact

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"google.golang.org/protobuf/proto"
)

// Resign only SDK-owned and publisher-owned bytes. The live reservation,
// endpoint census and selected settlement remain exactly the same originals.
func participantRelabelCreation(t *testing.T, fixture *wholeWorkTestFixture, changeRequest bool) {
	t.Helper()
	originalsHash := SnapshotHash(fixture.inventory.AttributionOriginals)
	fixture.changeCut(t, 0, 1, func(cut *coreprotocol.OriginalWorkCut) {
		contract := &cut.Contracts[0]
		admission, err := coreprotocol.DecodeOriginalContractAdmission(t.Context(), contract.OriginalCreation)
		if err != nil {
			t.Fatal(err)
		}
		request, err := coreprotocol.DecodeOriginalContractRequest(t.Context(), admission.Request)
		if err != nil {
			t.Fatal(err)
		}
		var requestFrame, resultFrame coreprotocol.Frame
		var requested coreprotocol.CreateContract
		var result coreprotocol.CreateContractResult
		for _, decode := range []struct {
			raw     []byte
			message proto.Message
		}{{raw: request.RequestFrame, message: &requestFrame}, {raw: admission.ResultFrame, message: &resultFrame}} {
			if err := proto.Unmarshal(decode.raw, decode.message); err != nil {
				t.Fatal(err)
			}
		}
		if err := proto.Unmarshal(requestFrame.MessageBytes, &requested); err != nil {
			t.Fatal(err)
		}
		if err := proto.Unmarshal(resultFrame.MessageBytes, &result); err != nil {
			t.Fatal(err)
		}
		if changeRequest {
			requested.Companion = true
			requestFrame.MessageBytes, err = proto.Marshal(&requested)
			if err != nil {
				t.Fatal(err)
			}
			request.RequestFrame, err = proto.Marshal(&requestFrame)
			if err != nil {
				t.Fatal(err)
			}
			request, err = coreprotocol.SignOriginalContractRequest(t.Context(), request, fixture.ownerKeys[0])
			if err != nil {
				t.Fatal(err)
			}
			admission.Request, err = request.Bytes(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			result.CreateContract = &requested
		} else {
			result.Contract.ProvideMode = coreprotocol.ProvideMode_Stream
		}
		resultFrame.MessageBytes, err = proto.Marshal(&result)
		if err != nil {
			t.Fatal(err)
		}
		admission.ResultFrame, err = proto.Marshal(&resultFrame)
		if err != nil {
			t.Fatal(err)
		}
		admission, err = coreprotocol.SignOriginalContractAdmission(t.Context(), admission, fixture.ownerKeys[0])
		if err != nil {
			t.Fatal(err)
		}
		contract.OriginalCreation, err = admission.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	})
	row := &fixture.artifact.ClosedWork.Records[0]
	snapshot, err := decodeClosedWorkSnapshot(*row, fixture.artifact.Epoch)
	if err != nil {
		t.Fatal(err)
	}
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
	if SnapshotHash(fixture.inventory.AttributionOriginals) != originalsHash {
		t.Fatal("relabeling fixture changed the independent server originals")
	}
}

// The SDK may sign a different companion request for the same stored contract;
// it cannot replace the independently witnessed request actually received.
func TestWholeWorkOriginalReservationRejectsSdkRequestDirectionRelabel(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.AttributionComplete {
		t.Fatal("original direction fixture is not fully proved", value, err)
	}
	participantRelabelCreation(t, fixture, true)
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("SDK request relabeling changed the original earning endpoint", value, err)
	}
}

// Keeping the request exact is insufficient when a source invents a different
// returned provide mode. The server's original usage direction must also match.
func TestWholeWorkOriginalReservationRejectsSdkResponseDirectionRelabel(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	participantRelabelCreation(t, fixture, false)
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatal("SDK response relabeling changed the original earning endpoint", value, err)
	}
}

// Earlier receipts omitted these facts. Their original canonical bytes remain
// valid custody, while absence of request/direction keeps attribution unknown.
func TestWholeWorkOriginalReservationMissingDirectionStaysUnknown(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{121}, ed25519.SeedSize))
	replacements := map[[32]byte][32]byte{}
	for index, raw := range fixture.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Reservation == nil {
			continue
		}
		original.Reservation.RequestFrameHash, original.Reservation.UsageOriginIsSource = nil, nil
		original, err = protocol.SignProviderWorkReceipt(t.Context(), original, key)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := original.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		replacements[sha256.Sum256(raw)] = sha256.Sum256(updated)
		fixture.inventory.AttributionOriginals[index] = updated
	}
	for index, raw := range fixture.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Outcome == nil {
			continue
		}
		original.Outcome.ReservationHash = replacements[original.Outcome.ReservationHash]
		original, err = protocol.SignProviderWorkReceipt(t.Context(), original, key)
		if err != nil {
			t.Fatal(err)
		}
		fixture.inventory.AttributionOriginals[index], err = original.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	}
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatal("missing original direction became invented authority or false corruption", value, err)
	}
}

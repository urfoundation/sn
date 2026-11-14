// A stream's original SDK may retire before a new generation reuses its cohort.
// Only a previously admitted original creation may bridge that dependency.
package payoutartifact

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"google.golang.org/protobuf/proto"
)

// Retain one actual source/destination contract while leaving every idle owner
// in the independent roster and its two signed boundary cuts.
func participantKeepContract(t *testing.T, fixture *wholeWorkTestFixture, index int) {
	t.Helper()
	fixture.artifact.ClosedWork.Records = append([]ClosedWorkRecord(nil), fixture.artifact.ClosedWork.Records[index])
	fixture.artifact.ClosedWork.Count = 1
	fixture.inventory.Window.Records = append([]ClosedWorkWindowRecord(nil), fixture.inventory.Window.Records[index])
	for owner := 0; owner < 2; owner++ {
		fixture.changeCut(t, owner, 1, func(cut *coreprotocol.OriginalWorkCut) {
			cut.Contracts = append([]coreprotocol.OriginalWorkContract(nil), cut.Contracts[index])
		})
	}
	creationRebuildArtifact(t, fixture)
}

// A new SDK lifecycle receives the existing stream id in its own actual reply.
// The old request stays byte-identical in independently retained prior custody.
func participantRebindInheritedCreation(t *testing.T, fixture *wholeWorkTestFixture, cut *coreprotocol.OriginalWorkCut, streamId [16]byte) {
	t.Helper()
	marshal := func(value proto.Message) []byte {
		raw, err := proto.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	contract := &cut.Contracts[0]
	admission, err := coreprotocol.DecodeOriginalContractAdmission(t.Context(), contract.OriginalCreation)
	if err != nil {
		t.Fatal(err)
	}
	request, err := coreprotocol.DecodeOriginalContractRequest(t.Context(), admission.Request)
	if err != nil {
		t.Fatal(err)
	}
	request.Generation = cut.Generation
	request, err = coreprotocol.SignOriginalContractRequest(t.Context(), request, fixture.ownerKeys[0])
	if err != nil {
		t.Fatal(err)
	}
	admission.Request, err = request.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var stored coreprotocol.StoredContract
	var frame coreprotocol.Frame
	var result coreprotocol.CreateContractResult
	if err := proto.Unmarshal(contract.StoredContract, &stored); err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(admission.ResultFrame, &frame); err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(frame.MessageBytes, &result); err != nil {
		t.Fatal(err)
	}
	stored.StreamId = streamId[:]
	contract.StoredContract = marshal(&stored)
	result.Contract.StoredContractBytes = contract.StoredContract
	frame.MessageBytes = marshal(&result)
	admission.ResultFrame = marshal(&frame)
	admission, err = coreprotocol.SignOriginalContractAdmission(t.Context(), admission, fixture.ownerKeys[0])
	if err != nil {
		t.Fatal(err)
	}
	contract.OriginalCreation, err = admission.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

// Advance physical headers and every permission/cut to the next epoch. The
// new lifecycle retains only its newly admitted contract, never the old origin.
func participantAdvanceInheritedEpoch(t *testing.T, fixture, prior *wholeWorkTestFixture, streamId [16]byte) {
	t.Helper()
	start, end := prior.inventory.Clock.EndTime, prior.inventory.Clock.EndTime.Add(time.Hour)
	last := &types.Header{Number: new(big.Int).SetUint64(prior.artifact.End.Number + 100), Time: uint64(end.Unix())}
	fixture.artifact.Epoch = prior.artifact.Epoch + 1
	fixture.artifact.Start, fixture.artifact.End = prior.artifact.End, Boundary{Number: last.Number.Uint64(), Hash: last.Hash().Hex()}
	fixture.artifact.CreatedAt = end.Format(time.RFC3339Nano)
	census := fixture.artifact.ClosedWork
	census.Epoch, census.Start, census.End = fixture.artifact.Epoch, fixture.artifact.Start, fixture.artifact.End
	census.WindowStart, census.WindowEnd = start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)
	census.Records[0].ClosedAt = start.Add(30 * time.Minute).Format(time.RFC3339Nano)
	fixture.inventory.Window.Start, fixture.inventory.Window.End = census.WindowStart, census.WindowEnd
	closed := census.Records[0].ClosedAt
	fixture.inventory.Window.Records[0].ClosedAt = &closed
	fixture.inventory.Clock = &ClosedWorkWindowClock{Start: fixture.artifact.Start, End: fixture.artifact.End, StartTime: start, EndTime: end, StartHeader: bytes.Clone(prior.inventory.Clock.EndHeader)}
	var err error
	fixture.inventory.Clock.EndHeader, err = rlp.EncodeToBytes(last)
	if err != nil {
		t.Fatal(err)
	}
	fixture.authority.Epoch, fixture.authority.Start, fixture.authority.End = fixture.artifact.Epoch, fixture.artifact.Start, fixture.artifact.End
	for owner := range fixture.authority.Owners {
		fixture.authority.Owners[owner].Generation[0] += 40
		for side, boundary := range []Boundary{fixture.artifact.Start, fixture.artifact.End} {
			fixture.changeCut(t, owner, side, func(cut *coreprotocol.OriginalWorkCut) {
				cut.Generation = fixture.authority.Owners[owner].Generation
				cut.Epoch, cut.Block, cut.BlockHash = fixture.artifact.Epoch, boundary.Number, [32]byte(commonWholeWorkHash(t, boundary.Hash))
				if side == 1 && owner == 0 {
					participantRebindInheritedCreation(t, fixture, cut, streamId)
				}
				if side == 1 && owner == 1 {
					var stored coreprotocol.StoredContract
					if err := proto.Unmarshal(cut.Contracts[0].StoredContract, &stored); err != nil {
						t.Fatal(err)
					}
					stored.StreamId = streamId[:]
					cut.Contracts[0].StoredContract, err = proto.Marshal(&stored)
					if err != nil {
						t.Fatal(err)
					}
				}
			})
			pair := &fixture.inventory.Owners[owner]
			raw := pair.StartRequest
			if side == 1 {
				raw = pair.EndRequest
			}
			request, err := coreprotocol.DecodeOriginalWorkRequest(raw, fixture.authority.RequestPublicKey)
			if err != nil {
				t.Fatal(err)
			}
			request.Generation, request.Epoch = fixture.authority.Owners[owner].Generation, fixture.artifact.Epoch
			request.Block, request.BlockHash = boundary.Number, [32]byte(commonWholeWorkHash(t, boundary.Hash))
			request.RequestId[2]++
			request.IssuedAtUnix = []time.Time{start, end}[side].Unix()
			request.ExpiresAtUnix = request.IssuedAtUnix + 300
			request, err = coreprotocol.SignOriginalWorkRequest(request, fixture.requestKey)
			if err != nil {
				t.Fatal(err)
			}
			raw, err = request.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if side == 0 {
				pair.StartRequest = raw
			} else {
				pair.EndRequest = raw
			}
		}
	}
	fixture.signAuthority(t)
	creationRebuildArtifact(t, fixture)
}

// The previous window is fully admitted through the public verifier first.
// Current input reuses only its exact original stream cohort and source birth.
func participantInheritedStreamFixture(t *testing.T) (*wholeWorkTestFixture, [16]byte) {
	t.Helper()
	prior := participantStreamEvidenceFixture(t)
	participantKeepContract(t, prior, 0)
	participantAttachOriginals(t, prior)
	verified, err := VerifyWholeWorkInventoryWithWitness(t.Context(), prior.artifact, prior.inventory, prior.expected)
	if err != nil || verified == nil || !verified.Complete || !verified.AttributionComplete || len(verified.RetainedCreations) != 1 {
		t.Fatal("original stream birth did not reach complete prior admission", verified, err)
	}
	retained := verified.RetainedCreations[0]
	admission, err := coreprotocol.DecodeOriginalContractAdmission(t.Context(), retained.Original.OriginalCreation)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := admission.Facts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture := participantStreamEvidenceFixture(t)
	participantKeepContract(t, fixture, 1)
	participantAdvanceInheritedEpoch(t, fixture, prior, facts.StreamId)
	key := participantAttachOriginals(t, fixture)
	fixture.expected.PriorContracts = append([]WholeWorkPriorContract(nil), verified.ReconciledContracts...)
	fixture.expected.PriorCreations = append([]WholeWorkRetainedCreation(nil), verified.RetainedCreations...)
	var reservation protocol.ProviderWorkReservation
	var cohortHash [32]byte
	originals := [][]byte{}
	for _, raw := range prior.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Outcome != nil {
			continue
		}
		if original.Reservation != nil {
			reservation = *original.Reservation
		}
		if original.Stream != nil {
			cohortHash = sha256.Sum256(raw)
		}
		originals = append(originals, bytes.Clone(raw))
	}
	for _, raw := range fixture.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Session != nil || original.Stream != nil {
			continue
		}
		if original.Reservation != nil {
			original.Reservation.SourceHead, original.Reservation.DestinationHead = reservation.SourceHead, reservation.DestinationHead
		}
		if original.Outcome != nil {
			original.Outcome.StreamHash = cohortHash
		}
		original, err = protocol.SignProviderWorkReceipt(t.Context(), original, key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = original.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		originals = append(originals, raw)
	}
	fixture.inventory.AttributionOriginals = originals
	participantSetOriginalTimes(t, fixture, fixture.artifact.ClosedWork.Records[0].ContractId, fixture.inventory.Clock.StartTime.Add(10*time.Second), fixture.inventory.Clock.StartTime.Add(30*time.Minute))
	return fixture, retained.Original.ContractId
}

func TestWholeWorkInheritedStreamCrossesRetiredSdkGenerationWithoutRecredit(t *testing.T) {
	fixture, origin := participantInheritedStreamFixture(t)
	retained := fixture.expected.PriorCreations[0]
	if retained.Owner.Generation == fixture.authority.Owners[0].Generation || len(fixture.authority.PriorContracts) != 0 {
		t.Fatal("fixture did not retire the original SDK or used a current prior proposal")
	}
	for _, pair := range fixture.inventory.Owners {
		for _, raw := range [][]byte{pair.Start, pair.End} {
			cut, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, contract := range cut.Contracts {
				if contract.ContractId == origin {
					t.Fatal("retired origin entered the new SDK census")
				}
			}
		}
	}
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Contracts != 1 || value.Credited != 1 || len(value.ReconciledContracts) != 1 || value.ReconciledContracts[0].ContractId == origin || len(value.RetainedCreations) != 1 || value.RetainedCreations[0].Owner.Generation != fixture.authority.Owners[0].Generation || len(value.ExpectedProviders) != 3 || value.ExpectedProviders[0].UsageBytes != 50 || value.ExpectedProviders[1].UsageBytes != 0 || value.ExpectedProviders[2].UsageBytes != 50 {
		t.Fatalf("retired stream birth was lost or recredited as current work: %+v, %v", value, err)
	}
}

func TestWholeWorkInheritedStreamMissingRetainedOriginStaysUnknown(t *testing.T) {
	fixture, _ := participantInheritedStreamFixture(t)
	fixture.expected.PriorCreations = nil
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatalf("current cohort invented missing retired SDK provenance: %+v, %v", value, err)
	}
}

func TestWholeWorkInheritedStreamRejectsChangedPriorCheckpoint(t *testing.T) {
	fixture, _ := participantInheritedStreamFixture(t)
	fixture.expected.PriorCreations[0].Checkpoint.StoredContractHash[0] ^= 1
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("changed prior hash authorized inherited stream membership: %+v, %v", value, err)
	}
}

func TestWholeWorkInheritedStreamRejectsChangedRetiredSourceGeneration(t *testing.T) {
	fixture, _ := participantInheritedStreamFixture(t)
	fixture.expected.PriorCreations[0].Owner.Generation[0]++
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("new SDK borrowed a retired source generation: %+v, %v", value, err)
	}
}

func TestWholeWorkInheritedStreamMissingOriginalReservationStaysUnknown(t *testing.T) {
	fixture, origin := participantInheritedStreamFixture(t)
	for index, raw := range fixture.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Reservation != nil && original.Reservation.ContractId == participantTestId(origin) {
			fixture.inventory.AttributionOriginals = append(fixture.inventory.AttributionOriginals[:index], fixture.inventory.AttributionOriginals[index+1:]...)
			break
		}
	}
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatalf("prior SDK signature invented the original live reservation: %+v, %v", value, err)
	}
}

func TestWholeWorkInheritedStreamRejectsChangedOriginalReservation(t *testing.T) {
	fixture, origin := participantInheritedStreamFixture(t)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{121}, ed25519.SeedSize))
	for index, raw := range fixture.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Reservation == nil || original.Reservation.ContractId != participantTestId(origin) {
			continue
		}
		original.Reservation.RequestFrameHash[0] ^= 1
		original, err = protocol.SignProviderWorkReceipt(t.Context(), original, key)
		if err != nil {
			t.Fatal(err)
		}
		fixture.inventory.AttributionOriginals[index], err = original.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	}
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("cohort concealed a different originally received request: %+v, %v", value, err)
	}
}

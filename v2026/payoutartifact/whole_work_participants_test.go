// These witnesses keep all SDK and admission originals while a publisher
// changes its own payout rows and resigns every publisher-controlled commitment.
package payoutartifact

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

func participantTestId(id [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}

// The independent roster includes a third idle provider with actual empty cuts.
// Its presence cannot make that provider an earning party of another contract.
func participantAddIdleOwner(t *testing.T, fixture *wholeWorkTestFixture) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{97}, 32))
	owner := WholeWorkOwner{ClientId: [16]byte{3}, NetworkId: [16]byte{30}, Generation: [16]byte{33}, PublicKey: [32]byte(key[32:])}
	pair := WholeWorkOwnerCuts{}
	for side, raw := range [][]byte{fixture.inventory.Owners[0].Start, fixture.inventory.Owners[0].End} {
		cut, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		cut.ClientId, cut.Generation, cut.PublicKey, cut.Revision, cut.Contracts = owner.ClientId, owner.Generation, owner.PublicKey, 0, []coreprotocol.OriginalWorkContract{}
		cut, err = coreprotocol.SignOriginalWorkCut(t.Context(), cut, key)
		if err != nil {
			t.Fatal(err)
		}
		cutRaw, err := cut.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		requestRaw := fixture.inventory.Owners[0].StartRequest
		if side == 1 {
			requestRaw = fixture.inventory.Owners[0].EndRequest
		}
		request, err := coreprotocol.DecodeOriginalWorkRequest(requestRaw, fixture.authority.RequestPublicKey)
		if err != nil {
			t.Fatal(err)
		}
		request.ClientId, request.Generation, request.PublicKey, request.RequestId = owner.ClientId, owner.Generation, owner.PublicKey, [16]byte{99, byte(side + 1)}
		request, err = coreprotocol.SignOriginalWorkRequest(request, fixture.requestKey)
		if err != nil {
			t.Fatal(err)
		}
		requestRaw, err = request.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if side == 0 {
			pair.Start, pair.StartRequest = cutRaw, requestRaw
		} else {
			pair.End, pair.EndRequest = cutRaw, requestRaw
		}
	}
	fixture.authority.Owners = append(fixture.authority.Owners, owner)
	fixture.authority.ExpectedProviders = append(fixture.authority.ExpectedProviders, WholeWorkExpectedProvider{ClientId: owner.ClientId, NetworkId: owner.NetworkId})
	fixture.ownerKeys = append(fixture.ownerKeys, key)
	fixture.inventory.Owners = append(fixture.inventory.Owners, pair)
	fixture.artifact.Providers = append(fixture.artifact.Providers, ProviderInput{ClientID: owner.ClientId, NetworkID: owner.NetworkId, Coldkey: [32]byte{33}, Assignments: 1, Confirmations: 0, Eligible: false})
	fixture.signAuthority(t)
	creationRebuildArtifact(t, fixture)
}

// The independent live source signs exact admission inputs, separately from
// the authority, SDK owners, client-report root and payout publisher.
func participantAttachOriginals(t *testing.T, fixture *wholeWorkTestFixture) ed25519.PrivateKey {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{121}, 32))
	domainHash, err := fixture.authority.Domain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	start, end := fixture.inventory.Clock.StartTime, fixture.inventory.Clock.EndTime
	source := protocol.ProviderWorkSourceAuthority{DomainHash: domainHash, SourceId: participantTestId([16]byte{122}), Generation: participantTestId([16]byte{123}), PublicKey: [32]byte(key[32:]), FromUnixMicro: start.Add(-2 * time.Hour).UnixMicro(), ThroughUnixMicro: end.Add(2 * time.Hour).UnixMicro(), MaxEndpointEvents: 128, MaxCohortMembers: 8, DirectoryPublicKeys: [][32]byte{}}
	fixture.authority.WorkSources = []protocol.ProviderWorkSourceAuthority{source}
	fixture.expected.AttributionSigner = fixture.expected.AuthoritySigner
	fixture.inventory.AttributionOriginals = [][]byte{}
	retain := func(original protocol.ProviderWorkReceipt) [32]byte {
		original.DomainHash, original.SourceId, original.Generation = source.DomainHash, source.SourceId, source.Generation
		value, err := protocol.SignProviderWorkReceipt(t.Context(), original, key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := value.Bytes(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		fixture.inventory.AttributionOriginals = append(fixture.inventory.AttributionOriginals, raw)
		return sha256.Sum256(raw)
	}
	networkKVs := map[[16]byte][16]byte{}
	headKVs := map[[16]byte]protocol.ProviderWorkEndpointHead{}
	for _, owner := range fixture.authority.Owners {
		networkKVs[owner.ClientId] = owner.NetworkId
		event := &protocol.ProviderWorkSessionEvent{ClientId: participantTestId(owner.ClientId), NetworkId: participantTestId(owner.NetworkId), Sequence: 1, Kind: "baseline", ObservedAtUnixMicro: start.Add(-time.Hour).UnixMicro()}
		hash := retain(protocol.ProviderWorkReceipt{Session: event})
		headKVs[owner.ClientId] = protocol.ProviderWorkEndpointHead{ClientId: event.ClientId, NetworkId: event.NetworkId, Sequence: 1, HeadHash: hash}
	}
	cut, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), fixture.inventory.Owners[0].End)
	if err != nil {
		t.Fatal(err)
	}
	streamKVs := map[[16]byte][32]byte{}
	for _, contract := range cut.Contracts {
		admission, err := coreprotocol.DecodeOriginalContractAdmission(t.Context(), contract.OriginalCreation)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := admission.Facts(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		request, err := coreprotocol.DecodeOriginalContractRequest(t.Context(), admission.Request)
		if err != nil {
			t.Fatal(err)
		}
		requestHash := sha256.Sum256(request.RequestFrame)
		originIsSource := facts.UsageOriginIsSource
		reservation := &protocol.ProviderWorkReservation{ContractId: participantTestId(contract.ContractId), SourceId: participantTestId(facts.SourceId), SourceNetworkId: participantTestId(networkKVs[facts.SourceId]), DestinationId: participantTestId(facts.DestinationId), DestinationNetworkId: participantTestId(networkKVs[facts.DestinationId]), CreatedAtUnixMicro: start.Add(10 * time.Second).UnixMicro(), Capacity: facts.ReservedBytes, SourceHead: headKVs[facts.SourceId], DestinationHead: headKVs[facts.DestinationId], Complete: true}
		reservation.RequestFrameHash, reservation.UsageOriginIsSource = &requestHash, &originIsSource
		reservationHash := retain(protocol.ProviderWorkReceipt{Reservation: reservation})
		streamHash := streamKVs[facts.StreamId]
		if facts.StreamId != ([16]byte{}) && streamHash == ([32]byte{}) {
			request, err := coreprotocol.DecodeOriginalContractRequest(t.Context(), admission.Request)
			if err != nil {
				t.Fatal(err)
			}
			cohort := &protocol.ProviderWorkStreamCohort{StreamId: participantTestId(facts.StreamId), OriginContractId: participantTestId(facts.ContractId), SourceId: participantTestId(facts.SourceId), DestinationId: participantTestId(facts.DestinationId), RequestFrameHash: sha256.Sum256(request.RequestFrame), CreatedAtUnixMicro: start.Add(20 * time.Second).UnixMicro(), Intermediaries: []protocol.ProviderWorkParticipant{}}
			for _, id := range facts.IntermediaryIds {
				cohort.Intermediaries = append(cohort.Intermediaries, protocol.ProviderWorkParticipant{ClientId: participantTestId(id), NetworkId: participantTestId(networkKVs[id])})
			}
			streamHash = retain(protocol.ProviderWorkReceipt{Stream: cohort})
			streamKVs[facts.StreamId] = streamHash
		}
		for _, row := range fixture.artifact.ClosedWork.Records {
			if row.ContractId == contract.ContractId {
				closedAt, err := time.Parse(time.RFC3339Nano, row.ClosedAt)
				if err != nil {
					t.Fatal(err)
				}
				retain(protocol.ProviderWorkReceipt{Outcome: &protocol.ProviderWorkOutcome{ContractId: reservation.ContractId, ReservationHash: reservationHash, StreamHash: streamHash, ClosedAtUnixMicro: closedAt.UnixMicro(), Outcome: "settled", SourceBytes: 100, DestinationBytes: 100, Capacity: facts.ReservedBytes, SourceComplete: true, DestinationComplete: true}})
			}
		}
	}
	fixture.signAuthority(t)
	return key
}

func participantEvidenceFixture(t *testing.T) *wholeWorkTestFixture {
	t.Helper()
	fixture := creationEvidenceFixture(t, false)
	participantAddIdleOwner(t, fixture)
	participantAttachOriginals(t, fixture)
	return fixture
}

func TestWholeWorkOriginalAdmissionsCompleteOrdinaryCompanionAndIdleProviders(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.Credited != 2 || len(value.ExpectedProviders) != 3 || value.ExpectedProviders[0].UsageBytes != 100 || value.ExpectedProviders[1].UsageBytes != 100 || value.ExpectedProviders[2].UsageBytes != 0 {
		t.Fatalf("original ordinary/companion/idle attribution = %+v, %v", value, err)
	}
}

func TestWholeWorkOriginalAdmissionsRejectResignedExtraIdlePartyAtEqualTotal(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	originalsHash := SnapshotHash(fixture.inventory.AttributionOriginals)
	row := &fixture.artifact.ClosedWork.Records[0]
	snapshot, err := decodeClosedWorkSnapshot(*row, fixture.artifact.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	provider := (*snapshot.Providers)[0]
	half := int64(50)
	provider.ByteCount = &half
	*snapshot.Providers = append((*snapshot.Providers)[:0], provider, provider)
	(*snapshot.Providers)[1].ClientId, (*snapshot.Providers)[1].NetworkId = participantTestId([16]byte{3}), participantTestId([16]byte{30})
	row.Original, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	creationRebuildArtifact(t, fixture)
	if SnapshotHash(fixture.inventory.AttributionOriginals) != originalsHash {
		t.Fatal("publisher mutation changed an admission original")
	}
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("independently enrolled idle party gained half the same bytes: %v", err)
	}
}

func TestWholeWorkOriginalAdmissionsCannotBorrowPublisherCloseClock(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	row := &fixture.artifact.ClosedWork.Records[0]
	closedAt, err := time.Parse(time.RFC3339Nano, row.ClosedAt)
	if err != nil {
		t.Fatal(err)
	}
	row.ClosedAt = closedAt.Add(time.Minute).Format(time.RFC3339Nano)
	changed := row.ClosedAt
	fixture.inventory.Window.Records[0].ClosedAt = &changed
	creationRebuildArtifact(t, fixture)
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("publisher selected another original close time: %v", err)
	}
}

func TestWholeWorkOriginalAdmissionsMissingBaselineCannotProveEmpty(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	fixture.inventory.AttributionOriginals = fixture.inventory.AttributionOriginals[1:]
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatalf("missing endpoint genesis became complete: %+v, %v", value, err)
	}
}

func TestWholeWorkOriginalAdmissionsNeedIndependentSourceApproval(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	fixture.authority.WorkSources = nil
	fixture.signAuthority(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatalf("original transport selected its own signing authority: %+v, %v", value, err)
	}
}

func TestWholeWorkOriginalAdmissionsRejectDuplicateReceipt(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	fixture.inventory.AttributionOriginals = append(fixture.inventory.AttributionOriginals, bytes.Clone(fixture.inventory.AttributionOriginals[0]))
	if _, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("duplicate original admitted as another event: %v", err)
	}
}

// An unavailable source is not permission to stop inspecting other present
// original bytes. A later contradiction must retain its integrity classification.
func TestWholeWorkOriginalAdmissionMissingAuthorityCannotHidePresentCorruption(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	original, err := protocol.DecodeProviderWorkReceipt(t.Context(), fixture.inventory.AttributionOriginals[0])
	if err != nil {
		t.Fatal(err)
	}
	original.SourceId = participantTestId([16]byte{124})
	original, err = protocol.SignProviderWorkReceipt(t.Context(), original, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{121}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := original.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.inventory.AttributionOriginals = append([][]byte{raw}, fixture.inventory.AttributionOriginals...)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatalf("unapproved source became attribution or corruption: %+v, %v", value, err)
	}
	original, err = protocol.DecodeProviderWorkReceipt(t.Context(), fixture.inventory.AttributionOriginals[1])
	if err != nil {
		t.Fatal(err)
	}
	original.Session.ObservedAtUnixMicro++
	fixture.inventory.AttributionOriginals[1], err = json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected); value != nil || !errors.Is(err, ErrClosedWorkIntegrity) {
		t.Fatalf("missing authority concealed a present changed original: %+v, %v", value, err)
	}
}

func TestWholeWorkOriginalAdmissionsNeedExplicitAttributionPurpose(t *testing.T) {
	fixture := participantEvidenceFixture(t)
	fixture.expected.AttributionSigner = common.Address{}
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || value.AttributionComplete {
		t.Fatalf("SDK census authority silently gained attribution purpose: %+v, %v", value, err)
	}
}

// Both contracts have one requested intermediary and an equal original split.
func participantStreamEvidenceFixture(t *testing.T) *wholeWorkTestFixture {
	t.Helper()
	fixture := creationEvidenceFixture(t, false, [16]byte{3})
	participantAddIdleOwner(t, fixture)
	for index := range fixture.artifact.ClosedWork.Records {
		row := &fixture.artifact.ClosedWork.Records[index]
		snapshot, err := decodeClosedWorkSnapshot(*row, fixture.artifact.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		provider := (*snapshot.Providers)[0]
		half := int64(50)
		provider.ByteCount = &half
		*snapshot.Providers = append((*snapshot.Providers)[:0], provider, provider)
		(*snapshot.Providers)[1].ClientId, (*snapshot.Providers)[1].NetworkId = participantTestId([16]byte{3}), participantTestId([16]byte{30})
		row.Original, err = json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
	}
	creationRebuildArtifact(t, fixture)
	participantAttachOriginals(t, fixture)
	return fixture
}

func TestWholeWorkOriginalStreamCohortCompletesOnlyActualRequestedMembers(t *testing.T) {
	fixture := participantStreamEvidenceFixture(t)
	value, err := VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || !value.Complete || !value.AttributionComplete || value.ExpectedProviders[2].UsageBytes != 100 {
		t.Fatalf("first original stream cohort did not complete: %+v, %v", value, err)
	}
	for index, raw := range fixture.inventory.AttributionOriginals {
		original, err := protocol.DecodeProviderWorkReceipt(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Stream != nil {
			fixture.inventory.AttributionOriginals = append(fixture.inventory.AttributionOriginals[:index], fixture.inventory.AttributionOriginals[index+1:]...)
			break
		}
	}
	value, err = VerifyWholeWorkInventoryWithWitness(t.Context(), fixture.artifact, fixture.inventory, fixture.expected)
	if err != nil || value == nil || value.AttributionComplete {
		t.Fatalf("SDK requested path replaced missing actual stream birth: %+v, %v", value, err)
	}
}

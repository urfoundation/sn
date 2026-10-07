// Actual admission originals complete the earning-party join only when their
// independent source, endpoint histories, stream cohort and outcome all agree.
package payoutartifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

type wholeWorkSourceIdentity struct {
	sourceId   string
	generation string
}

type wholeWorkEndpointIdentity struct {
	source    wholeWorkSourceIdentity
	clientId  string
	networkId string
}

type wholeWorkEndpointProof struct {
	source wholeWorkSourceIdentity
	state  protocol.ProviderWorkEndpointState
}

// The producer retains one first observation for each exact requested phase.
type wholeWorkOpenIdentity struct {
	contractId [16]byte
	epoch      uint64
	blockHash  [32]byte
}

// Receipt hashes identify originals shared by many contracts. Endpoint replay
// retains one state per original prefix, never a quadratic copy per contract.
type wholeWorkParticipantPool struct {
	authorityKVs   map[wholeWorkSourceIdentity]protocol.ProviderWorkSourceAuthority
	originalKVs    map[[32]byte]protocol.ProviderWorkReceipt
	reservationKVs map[[16]byte][32]byte
	outcomeKVs     map[[16]byte][32]byte
	openKVs        map[[16]byte][][32]byte
	streamKVs      map[[16]byte][32]byte
	endpointKVs    map[[32]byte]wholeWorkEndpointProof
}

// Only independently replayed outcomes may select a dispute amount policy.
// Complete additionally requires the entire original earning-party set.
type wholeWorkParticipantVerification struct {
	Complete           bool
	Outcomes           map[[16]byte]protocol.ProviderWorkOutcome
	FutureReservations map[[16]byte]protocol.ProviderWorkReservation
	OpenThroughEnd     map[[16]byte]protocol.ProviderWorkReceipt
}

// Convert component failures without disguising cancellation as malformed data.
func wholeWorkParticipantError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, protocol.ErrProviderWorkCapacity):
		return errors.Join(ErrClosedWorkCapacity, err)
	case errors.Is(err, protocol.ErrProviderWorkUnavailable):
		return errors.Join(ErrClosedWorkUnavailable, err)
	default:
		return errors.Join(ErrClosedWorkIntegrity, err)
	}
}

// The roster explicitly approves one global persistent admission owner at a
// time. Overlapping independent generations need a stronger multi-owner fence.
func (self *wholeWorkParticipantPool) uniqueSourceAt(original protocol.ProviderWorkReceipt) bool {
	count := 0
	for identity, authority := range self.authorityKVs {
		if at := original.ObservedUnixMicro(); authority.FromUnixMicro <= at && at < authority.ThroughUnixMicro {
			if identity != (wholeWorkSourceIdentity{sourceId: original.SourceId, generation: original.Generation}) {
				return false
			}
			count++
		}
	}
	return count == 1
}

// Read every original before deciding which missing components are unknown.
// Neither a self-described signer nor an artifact publisher supplies authority.
func readWholeWorkParticipantPool(ctx context.Context, domainHash [32]byte, authorities []protocol.ProviderWorkSourceAuthority, originals [][]byte) (*wholeWorkParticipantPool, error) {
	pool := &wholeWorkParticipantPool{
		authorityKVs: map[wholeWorkSourceIdentity]protocol.ProviderWorkSourceAuthority{}, originalKVs: map[[32]byte]protocol.ProviderWorkReceipt{},
		reservationKVs: map[[16]byte][32]byte{}, outcomeKVs: map[[16]byte][32]byte{}, openKVs: map[[16]byte][][32]byte{}, streamKVs: map[[16]byte][32]byte{}, endpointKVs: map[[32]byte]wholeWorkEndpointProof{},
	}
	for _, authority := range authorities {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := authority.Validate(); err != nil || authority.DomainHash != domainHash {
			return nil, errors.Join(ErrClosedWorkIntegrity, err)
		}
		identity := wholeWorkSourceIdentity{sourceId: authority.SourceId, generation: authority.Generation}
		if _, exists := pool.authorityKVs[identity]; exists {
			return nil, ErrClosedWorkIntegrity
		}
		pool.authorityKVs[identity] = authority
	}
	endpointKVs := map[wholeWorkEndpointIdentity][]protocol.ProviderWorkReceipt{}
	openKVs := map[wholeWorkOpenIdentity]bool{}
	seenOriginalKVs := map[[32]byte]bool{}
	var unavailableErr error
	for _, raw := range originals {
		original, err := protocol.DecodeProviderWorkReceipt(ctx, raw)
		if err != nil {
			return nil, wholeWorkParticipantError(err)
		}
		if original.DomainHash != domainHash {
			return nil, ErrClosedWorkIntegrity
		}
		hash := sha256.Sum256(raw)
		if seenOriginalKVs[hash] {
			return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("duplicate participant original"))
		}
		seenOriginalKVs[hash] = true
		source := wholeWorkSourceIdentity{sourceId: original.SourceId, generation: original.Generation}
		authority, exists := pool.authorityKVs[source]
		if !exists {
			if unavailableErr == nil {
				unavailableErr = ErrClosedWorkUnavailable
			}
			continue
		}
		if err := protocol.VerifyProviderWorkReceiptAuthority(ctx, original, authority); err != nil {
			if errors.Is(err, protocol.ErrProviderWorkUnavailable) {
				if unavailableErr == nil {
					unavailableErr = wholeWorkParticipantError(err)
				}
				continue
			}
			return nil, wholeWorkParticipantError(err)
		}
		pool.originalKVs[hash] = original
		var idText string
		var target map[[16]byte][32]byte
		switch {
		case original.Session != nil:
			event := original.Session
			endpoint := wholeWorkEndpointIdentity{source: source, clientId: event.ClientId, networkId: event.NetworkId}
			endpointKVs[endpoint] = append(endpointKVs[endpoint], original)
		case original.Reservation != nil:
			idText, target = original.Reservation.ContractId, pool.reservationKVs
		case original.Outcome != nil:
			idText, target = original.Outcome.ContractId, pool.outcomeKVs
		case original.Open != nil:
			id, _ := protocol.ParseProviderWorkId(original.Open.ContractId)
			identity := wholeWorkOpenIdentity{contractId: id, epoch: original.Open.Epoch, blockHash: original.Open.BlockHash}
			if openKVs[identity] {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("conflicting original open observation"))
			}
			openKVs[identity] = true
			pool.openKVs[id] = append(pool.openKVs[id], hash)
		case original.Stream != nil:
			idText, target = original.Stream.StreamId, pool.streamKVs
		}
		if target != nil {
			id, _ := protocol.ParseProviderWorkId(idText)
			if _, exists := target[id]; exists {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("conflicting original admission identity"))
			}
			target[id] = hash
		}
	}
	for endpoint, events := range endpointKVs {
		sort.Slice(events, func(i, j int) bool { return events[i].Session.Sequence < events[j].Session.Sequence })
		for index := 1; index < len(events); index++ {
			if events[index-1].Session.Sequence == events[index].Session.Sequence {
				return nil, ErrClosedWorkIntegrity
			}
		}
		states, err := protocol.ReplayProviderWorkEndpoint(ctx, pool.authorityKVs[endpoint.source], events)
		if err != nil && !errors.Is(err, protocol.ErrProviderWorkUnavailable) {
			return nil, wholeWorkParticipantError(err)
		}
		for _, state := range states {
			pool.endpointKVs[state.Head.HeadHash] = wholeWorkEndpointProof{source: endpoint.source, state: state}
		}
	}
	return pool, errors.Join(unavailableErr, ctx.Err())
}

// A complete zero-extender state is currently supported. Directory key records
// alone do not bind provider ownership or prove activation at this reservation.
func (self *wholeWorkParticipantPool) endpointComplete(original protocol.ProviderWorkReceipt, head protocol.ProviderWorkEndpointHead) (bool, error) {
	proof, exists := self.endpointKVs[head.HeadHash]
	if !exists {
		return false, nil
	}
	if proof.source != (wholeWorkSourceIdentity{sourceId: original.SourceId, generation: original.Generation}) || proof.state.Head != head {
		return false, ErrClosedWorkIntegrity
	}
	if proof.state.ObservedAtUnixMicro > original.ObservedUnixMicro() || proof.state.ActiveExtenders != 0 {
		return false, nil
	}
	return true, nil
}

// Stream members come from the first actual creation, including when this
// contract reused that stream or the companion request reverses its endpoints.
func (self *wholeWorkParticipantPool) streamParties(ctx context.Context, facts coreprotocol.OriginalContractCreationFacts, outcome protocol.ProviderWorkOutcome, contracts map[[16]byte]*wholeWorkContract, creations map[[16]byte]coreprotocol.OriginalContractCreationFacts, priorCreations map[[16]byte]wholeWorkPriorCreation, parties map[[16]byte][16]byte) (bool, error) {
	if facts.StreamId == ([16]byte{}) {
		if outcome.StreamHash != ([32]byte{}) {
			return false, ErrClosedWorkIntegrity
		}
		return true, nil
	}
	hash, exists := self.streamKVs[facts.StreamId]
	if !exists || outcome.StreamHash == ([32]byte{}) {
		return false, nil
	}
	if hash != outcome.StreamHash {
		return false, ErrClosedWorkIntegrity
	}
	original := self.originalKVs[hash]
	cohort := original.Stream
	if !self.uniqueSourceAt(original) {
		return false, nil
	}
	source, _ := protocol.ParseProviderWorkId(cohort.SourceId)
	destination, _ := protocol.ParseProviderWorkId(cohort.DestinationId)
	if !((source == facts.SourceId && destination == facts.DestinationId) || (source == facts.DestinationId && destination == facts.SourceId)) {
		return false, ErrClosedWorkIntegrity
	}
	originId, _ := protocol.ParseProviderWorkId(cohort.OriginContractId)
	origin, exists := creations[originId]
	contract := contracts[originId]
	var originalCreation coreprotocol.OriginalWorkContract
	if exists && contract != nil {
		originalCreation, exists = contract.ends[origin.SourceId]
	} else if !exists && contract == nil {
		// A retired generation supplies only the authenticated stream birth
		// dependency. It never becomes current work or a current roster member.
		prior, retained := priorCreations[originId]
		origin, originalCreation, exists = prior.Facts, prior.Original, retained
	} else {
		exists = false
	}
	originReservationHash, originReserved := self.reservationKVs[originId]
	if !exists || !originReserved {
		return false, nil
	}
	originReservationOriginal := self.originalKVs[originReservationHash]
	originReservation := originReservationOriginal.Reservation
	if !self.uniqueSourceAt(originReservationOriginal) || originReservation.RequestFrameHash == nil || originReservation.UsageOriginIsSource == nil {
		return false, nil
	}
	if cohort.CreatedAtUnixMicro < originReservation.CreatedAtUnixMicro || cohort.CreatedAtUnixMicro > outcome.ClosedAtUnixMicro {
		return false, nil
	}
	if origin.StreamId != facts.StreamId || origin.SourceId != source || origin.DestinationId != destination || len(origin.IntermediaryIds) != len(cohort.Intermediaries) {
		return false, ErrClosedWorkIntegrity
	}
	admission, err := coreprotocol.DecodeOriginalContractAdmission(ctx, originalCreation.OriginalCreation)
	if err != nil {
		return false, errors.Join(ErrClosedWorkIntegrity, err)
	}
	request, err := coreprotocol.DecodeOriginalContractRequest(ctx, admission.Request)
	if err != nil || sha256.Sum256(request.RequestFrame) != cohort.RequestFrameHash || *originReservation.RequestFrameHash != cohort.RequestFrameHash || *originReservation.UsageOriginIsSource != origin.UsageOriginIsSource || originReservation.Capacity != origin.ReservedBytes || originReservation.SourceId != cohort.SourceId || originReservation.DestinationId != cohort.DestinationId {
		return false, errors.Join(ErrClosedWorkIntegrity, err)
	}
	usageOrigin := facts.SourceId
	if !facts.UsageOriginIsSource {
		usageOrigin = facts.DestinationId
	}
	for index, member := range cohort.Intermediaries {
		client, _ := protocol.ParseProviderWorkId(member.ClientId)
		network, _ := protocol.ParseProviderWorkId(member.NetworkId)
		if client != origin.IntermediaryIds[index] {
			return false, ErrClosedWorkIntegrity
		}
		if client == usageOrigin {
			continue
		}
		if previous, exists := parties[client]; exists && previous != network {
			return false, ErrClosedWorkIntegrity
		}
		parties[client] = network
	}
	return true, nil
}

// The immutable reservation establishes actual creation time even when a late
// SDK cut contains later work. Request and direction joins precede that use.
func (self *wholeWorkParticipantPool) bindReservation(ctx context.Context, contract *wholeWorkContract, facts coreprotocol.OriginalContractCreationFacts, ownerNetworkKVs map[[16]byte][16]byte, windowEnd time.Time) (protocol.ProviderWorkReceipt, bool, error) {
	hash, exists := self.reservationKVs[facts.ContractId]
	if !exists || contract == nil {
		return protocol.ProviderWorkReceipt{}, false, nil
	}
	original := self.originalKVs[hash]
	reservation := original.Reservation
	source, _ := protocol.ParseProviderWorkId(reservation.SourceId)
	destination, _ := protocol.ParseProviderWorkId(reservation.DestinationId)
	sourceNetwork, _ := protocol.ParseProviderWorkId(reservation.SourceNetworkId)
	destinationNetwork, _ := protocol.ParseProviderWorkId(reservation.DestinationNetworkId)
	if source != facts.SourceId || destination != facts.DestinationId || reservation.Capacity != facts.ReservedBytes {
		return protocol.ProviderWorkReceipt{}, false, errors.Join(ErrClosedWorkIntegrity, errors.New("original reservation differs from SDK admission"))
	}
	// A future peer may enroll or change networks after this roster's window.
	// Its independently signed creation time can exclude it without attributing
	// any bytes or importing that later peer into the current provider universe.
	if time.UnixMicro(reservation.CreatedAtUnixMicro).Before(windowEnd) && (ownerNetworkKVs[source] != sourceNetwork || ownerNetworkKVs[destination] != destinationNetwork) {
		return protocol.ProviderWorkReceipt{}, false, errors.Join(ErrClosedWorkIntegrity, errors.New("original in-window reservation differs from its owner networks"))
	}
	if reservation.RequestFrameHash == nil || reservation.UsageOriginIsSource == nil || !self.uniqueSourceAt(original) {
		return original, false, nil
	}
	admission, err := coreprotocol.DecodeOriginalContractAdmission(ctx, contract.ends[source].OriginalCreation)
	if err != nil {
		return protocol.ProviderWorkReceipt{}, false, errors.Join(ErrClosedWorkIntegrity, err)
	}
	request, err := coreprotocol.DecodeOriginalContractRequest(ctx, admission.Request)
	if err != nil || sha256.Sum256(request.RequestFrame) != *reservation.RequestFrameHash || facts.UsageOriginIsSource != *reservation.UsageOriginIsSource {
		return protocol.ProviderWorkReceipt{}, false, errors.Join(ErrClosedWorkIntegrity, errors.New("SDK creation reinterprets its original received request or service direction"), err)
	}
	return original, true, ctx.Err()
}

// This is the only positive earning-party path: exact original requests,
// fenced endpoint absence, original stream membership and original settlement.
func verifyWholeWorkParticipants(ctx context.Context, artifact *Artifact, authority WholeWorkAuthority, inventory *WholeWorkInventory, contracts map[[16]byte]*wholeWorkContract, creations map[[16]byte]coreprotocol.OriginalContractCreationFacts, priorCreations map[[16]byte]wholeWorkPriorCreation) (*wholeWorkParticipantVerification, error) {
	if ctx == nil || artifact == nil || artifact.ClosedWork == nil || inventory == nil || inventory.Clock == nil {
		return nil, ErrClosedWorkUnavailable
	}
	result := &wholeWorkParticipantVerification{Outcomes: map[[16]byte]protocol.ProviderWorkOutcome{}, FutureReservations: map[[16]byte]protocol.ProviderWorkReservation{}, OpenThroughEnd: map[[16]byte]protocol.ProviderWorkReceipt{}}
	endHash, err := hex.DecodeString(strings.TrimPrefix(artifact.End.Hash, "0x"))
	if err != nil || len(endHash) != 32 {
		return nil, ErrClosedWorkIntegrity
	}
	domainHash, err := authority.Domain.Digest()
	if err != nil {
		return nil, err
	}
	pool, err := readWholeWorkParticipantPool(ctx, domainHash, authority.WorkSources, inventory.AttributionOriginals)
	if err != nil {
		if errors.Is(err, ErrClosedWorkUnavailable) {
			return result, nil
		}
		return nil, err
	}
	ownerNetworkKVs := map[[16]byte][16]byte{}
	for _, owner := range authority.Owners {
		if previous, exists := ownerNetworkKVs[owner.ClientId]; exists && previous != owner.NetworkId {
			return result, nil
		}
		ownerNetworkKVs[owner.ClientId] = owner.NetworkId
	}
	boundReservationKVs := map[[16]byte]protocol.ProviderWorkReceipt{}
	for id, facts := range creations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		original, bound, err := pool.bindReservation(ctx, contracts[id], facts, ownerNetworkKVs, inventory.Clock.EndTime)
		if err != nil {
			return nil, err
		}
		if !bound {
			continue
		}
		boundReservationKVs[id] = original
		reservation := original.Reservation
		future := !time.UnixMicro(reservation.CreatedAtUnixMicro).Before(inventory.Clock.EndTime)
		if future {
			result.FutureReservations[id] = *reservation
		}
		outcomeOriginal := pool.originalKVs[pool.outcomeKVs[id]]
		outcome := outcomeOriginal.Outcome
		if outcome != nil {
			if outcome.ReservationHash != pool.reservationKVs[id] || outcome.Capacity != facts.ReservedBytes {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("original settlement differs from its original reservation"))
			}
			if outcome.ClosedAtUnixMicro < reservation.CreatedAtUnixMicro {
				// An original clock reversal cannot date either side of the cut.
				delete(result.FutureReservations, id)
			}
		}
		for _, hash := range pool.openKVs[id] {
			openOriginal := pool.originalKVs[hash]
			open := openOriginal.Open
			if open.ReservationHash != pool.reservationKVs[id] || open.ObservedAtUnixMicro < reservation.CreatedAtUnixMicro {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("original open observation differs from its original reservation"))
			}
			// Serialized open then close can share a truncated microsecond.
			// A strictly earlier terminal contradicts the live null observation.
			if outcome != nil && outcome.ClosedAtUnixMicro < open.ObservedAtUnixMicro {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("original open observation follows its terminal outcome"))
			}
			if open.Epoch != artifact.Epoch || open.Block != artifact.End.Number || open.BlockHash != [32]byte(endHash) {
				continue
			}
			if open.BoundaryUnixMicro != inventory.Clock.EndTime.UnixMicro() {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("original open observation changed the recovered boundary clock"))
			}
			if !future && pool.uniqueSourceAt(openOriginal) {
				result.OpenThroughEnd[id] = openOriginal
			}
		}
		if !future && outcome != nil && outcome.ClosedAtUnixMicro >= reservation.CreatedAtUnixMicro && pool.uniqueSourceAt(outcomeOriginal) && !time.UnixMicro(outcome.ClosedAtUnixMicro).Before(inventory.Clock.EndTime) {
			// Timing evidence is separate from complete terminal amount proof:
			// a boundary cut may correctly retain an earlier checkpoint.
			if _, observed := result.OpenThroughEnd[id]; !observed {
				result.OpenThroughEnd[id] = outcomeOriginal
			}
		}
	}
	complete := true
	for _, row := range artifact.ClosedWork.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		facts, created := creations[row.ContractId]
		reservationOriginal, reserved := boundReservationKVs[row.ContractId]
		reservationHash := pool.reservationKVs[row.ContractId]
		outcomeHash, closed := pool.outcomeKVs[row.ContractId]
		contract := contracts[row.ContractId]
		if !created || !reserved || !closed || contract == nil {
			complete = false
			continue
		}
		outcomeOriginal := pool.originalKVs[outcomeHash]
		reservation, outcome := reservationOriginal.Reservation, outcomeOriginal.Outcome
		source, _ := protocol.ParseProviderWorkId(reservation.SourceId)
		destination, _ := protocol.ParseProviderWorkId(reservation.DestinationId)
		sourceNetwork, _ := protocol.ParseProviderWorkId(reservation.SourceNetworkId)
		destinationNetwork, _ := protocol.ParseProviderWorkId(reservation.DestinationNetworkId)
		if outcome.Capacity != facts.ReservedBytes || outcome.ReservationHash != reservationHash {
			return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("original reservation or settlement differs from SDK admission"))
		}
		closedAt, err := time.Parse(time.RFC3339Nano, row.ClosedAt)
		originalTime := time.UnixMicro(outcome.ClosedAtUnixMicro)
		if err != nil || !closedAt.Equal(originalTime) || originalTime.Before(inventory.Clock.StartTime) || !originalTime.Before(inventory.Clock.EndTime) {
			return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("payout changed the original settlement window"))
		}
		if reservation.CreatedAtUnixMicro > outcome.ClosedAtUnixMicro {
			complete = false
			continue
		}
		totalsComplete := true
		for _, party := range []struct {
			id       [16]byte
			amount   uint64
			terminal bool
		}{{id: source, amount: outcome.SourceBytes, terminal: outcome.SourceComplete}, {id: destination, amount: outcome.DestinationBytes, terminal: outcome.DestinationComplete}} {
			head, err := coreprotocol.DecodeOriginalCloseInventory(contract.ends[party.id].LatestInventory)
			if err != nil || !party.terminal || !head.Terminal {
				totalsComplete = false
				continue
			}
			if head.CumulativeAckedBytes != party.amount {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("original settlement differs from the terminal SDK report"))
			}
		}
		supported := totalsComplete && reservation.Complete && pool.uniqueSourceAt(reservationOriginal) && pool.uniqueSourceAt(outcomeOriginal)
		for _, head := range []protocol.ProviderWorkEndpointHead{reservation.SourceHead, reservation.DestinationHead} {
			present, err := pool.endpointComplete(reservationOriginal, head)
			if err != nil {
				return nil, err
			}
			supported = supported && present
		}
		if supported {
			result.Outcomes[row.ContractId] = *outcome
		}
		parties := map[[16]byte][16]byte{destination: destinationNetwork}
		if !facts.UsageOriginIsSource {
			parties = map[[16]byte][16]byte{source: sourceNetwork}
		}
		streamComplete, err := pool.streamParties(ctx, facts, *outcome, contracts, creations, priorCreations, parties)
		if err != nil {
			return nil, err
		}
		supported = supported && streamComplete
		if !supported {
			complete = false
			continue
		}
		snapshot, err := decodeClosedWorkSnapshot(row, artifact.Epoch)
		if err != nil {
			return nil, err
		}
		if snapshot.ExcludedReason != "" {
			complete = false
			continue
		}
		if len(parties) != len(*snapshot.Providers) {
			return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("payout changed the original service-party set"))
		}
		for _, provider := range *snapshot.Providers {
			client, _ := closedWorkId(provider.ClientId)
			network, _ := closedWorkId(provider.NetworkId)
			if expected, exists := parties[client]; !exists || expected != network {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("payout changed an original service party"))
			}
		}
	}
	result.Complete = complete
	return result, ctx.Err()
}

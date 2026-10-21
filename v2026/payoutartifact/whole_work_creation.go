// Original SDK requests bind actual earning endpoints and requested stream
// parties independently of the payout publisher's SQL allocation vector.
package payoutartifact

import (
	"bytes"
	"context"
	"errors"

	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Request identities are scoped to their original SDK lifecycle, not merely to
// a reusable client key. One actual create operation cannot earn as two results.
type wholeWorkCreationIdentity struct {
	clientId   [16]byte
	generation [16]byte
	requestId  [16]byte
}

// Multiple generations of the same client must remain independently admitted.
type wholeWorkCreationOwnerIdentity struct {
	clientId   [16]byte
	generation [16]byte
}

// Replay all present originals before deciding which components are unknown.
// This component cannot prove absence of extenders or inherited stream members;
// complete attribution still needs those independent admission-time originals.
func verifyWholeWorkCreations(
	ctx context.Context,
	artifact *Artifact,
	domainHash [32]byte,
	owners []WholeWorkOwner,
	contracts map[[16]byte]*wholeWorkContract,
) (map[[16]byte]coreprotocol.OriginalContractCreationFacts, error) {
	if ctx == nil {
		return nil, errors.New("original creation replay requires an owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if artifact == nil || artifact.ClosedWork == nil || domainHash == ([32]byte{}) {
		return nil, ErrClosedWorkUnavailable
	}
	ownerKVs := make(map[wholeWorkCreationOwnerIdentity]WholeWorkOwner, len(owners))
	for _, owner := range owners {
		identity := wholeWorkCreationOwnerIdentity{clientId: owner.ClientId, generation: owner.Generation}
		if _, exists := ownerKVs[identity]; exists {
			return nil, ErrClosedWorkIntegrity
		}
		ownerKVs[identity] = owner
	}
	result := make(map[[16]byte]coreprotocol.OriginalContractCreationFacts, len(contracts))
	requests := make(map[wholeWorkCreationIdentity][16]byte, len(contracts))
	for id, contract := range contracts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		original, exists := contract.ends[contract.source]
		if !exists || len(original.OriginalCreation) == 0 {
			continue
		}
		admission, err := coreprotocol.DecodeOriginalContractAdmission(ctx, original.OriginalCreation)
		if err != nil {
			return nil, errors.Join(ErrClosedWorkIntegrity, err)
		}
		facts, err := admission.Facts(ctx)
		if err != nil {
			return nil, errors.Join(ErrClosedWorkIntegrity, err)
		}
		owner, exists := ownerKVs[wholeWorkCreationOwnerIdentity{clientId: facts.ClientId, generation: facts.Generation}]
		if !exists || facts.DomainHash != domainHash || facts.ClientId != contract.source || facts.Generation != owner.Generation || facts.PublicKey != owner.PublicKey || facts.ContractId != id || facts.SourceId != contract.source || facts.DestinationId != contract.destination || !bytes.Equal(facts.StoredContract, contract.stored) {
			return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("original create request differs from its independently admitted reservation"))
		}
		identity := wholeWorkCreationIdentity{clientId: facts.ClientId, generation: facts.Generation, requestId: facts.RequestId}
		if previous, exists := requests[identity]; exists && previous != id {
			return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("one original create request was reused for different contracts"))
		}
		requests[identity] = id
		result[id] = facts
	}
	for _, row := range artifact.ClosedWork.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		facts, exists := result[row.ContractId]
		if !exists {
			continue
		}
		snapshot, err := decodeClosedWorkSnapshot(row, artifact.Epoch)
		if err != nil {
			return nil, err
		}
		if snapshot.ExcludedReason != "" {
			continue
		}
		providers := make(map[[16]byte]bool, len(*snapshot.Providers))
		origin := facts.SourceId
		if !facts.UsageOriginIsSource {
			origin = facts.DestinationId
		}
		for _, provider := range *snapshot.Providers {
			id, err := closedWorkId(provider.ClientId)
			if err != nil {
				return nil, err
			}
			if id == origin {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("payout credited the original consumer as a service participant"))
			}
			providers[id] = true
		}
		for id := range wholeWorkOriginalServiceParties(facts) {
			if !providers[id] {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("payout omitted an original service participant"))
			}
		}
	}
	return result, ctx.Err()
}

// Server admission always includes the non-origin endpoint. Non-companion
// stream creation also persists the request's intermediaries; companion Stream
// mode may inherit another request's path and therefore needs its own cohort.
func wholeWorkOriginalServiceParties(facts coreprotocol.OriginalContractCreationFacts) map[[16]byte]bool {
	origin, egress := facts.SourceId, facts.DestinationId
	if !facts.UsageOriginIsSource {
		origin, egress = facts.DestinationId, facts.SourceId
	}
	parties := map[[16]byte]bool{egress: true}
	if facts.StreamId != ([16]byte{}) && facts.ProvideMode != coreprotocol.ProvideMode_Stream {
		for _, id := range facts.IntermediaryIds {
			if id != origin {
				parties[id] = true
			}
		}
	}
	return parties
}

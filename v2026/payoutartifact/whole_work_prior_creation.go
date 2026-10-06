// Stream inheritance may depend on a retired SDK generation. Its original
// creation comes only from an independently retained complete prior admission.
package payoutartifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// This private result is a dependency for stream replay, never current work.
type wholeWorkPriorCreation struct {
	Original coreprotocol.OriginalWorkContract
	Facts    coreprotocol.OriginalContractCreationFacts
	Owner    WholeWorkOwner
}

// Copy the original bytes before they leave either admitted custody boundary.
func cloneWholeWorkCreation(value coreprotocol.OriginalWorkContract) coreprotocol.OriginalWorkContract {
	value.StoredContract = bytes.Clone(value.StoredContract)
	value.LatestInventory = bytes.Clone(value.LatestInventory)
	value.OriginalCreation = bytes.Clone(value.OriginalCreation)
	return value
}

// Only the independently retained prior checkpoint can authorize a retired
// creation. A current authority's proposed prior list is not consulted here.
func verifyWholeWorkPriorCreations(ctx context.Context, artifact *Artifact, domainHash [32]byte, expected WholeWorkExpectation) (map[[16]byte]wholeWorkPriorCreation, error) {
	if ctx == nil || artifact == nil {
		return nil, ErrClosedWorkUnavailable
	}
	if len(expected.PriorContracts) > MaxClosedWorkRecords || len(expected.PriorCreations) > MaxClosedWorkRecords {
		return nil, ErrClosedWorkCapacity
	}
	checkpoints := map[[16]byte]WholeWorkPriorContract{}
	for _, checkpoint := range expected.PriorContracts {
		if _, exists := checkpoints[checkpoint.ContractId]; exists {
			return nil, ErrClosedWorkIntegrity
		}
		checkpoints[checkpoint.ContractId] = checkpoint
	}
	result := map[[16]byte]wholeWorkPriorCreation{}
	used := 0
	for _, value := range expected.PriorCreations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		original := value.Original
		byteCount := len(original.StoredContract) + len(original.LatestInventory) + len(original.OriginalCreation)
		if byteCount > MaxWholeWorkInventoryBytes-used {
			return nil, ErrClosedWorkCapacity
		}
		used += byteCount
		id := original.ContractId
		if _, exists := result[id]; exists {
			return nil, ErrClosedWorkIntegrity
		}
		checkpoint, exists := checkpoints[id]
		if !exists {
			return nil, ErrClosedWorkUnavailable
		}
		if checkpoint != value.Checkpoint || checkpoint.ContractId != id || checkpoint.ReconciledEpoch >= artifact.Epoch || !canonicalClosedWorkDigest(checkpoint.InventoryHash) || checkpoint.StoredContractHash != sha256.Sum256(original.StoredContract) || checkpoint.SourceInventoryHash != sha256.Sum256(original.LatestInventory) || len(original.OriginalCreation) == 0 || value.Owner.NetworkId == ([16]byte{}) {
			return nil, ErrClosedWorkIntegrity
		}
		source, destination, err := original.Parties()
		if err != nil {
			return nil, errors.Join(ErrClosedWorkIntegrity, err)
		}
		head, err := coreprotocol.DecodeOriginalCloseInventory(original.LatestInventory)
		if err != nil || !head.Terminal || head.DomainHash != domainHash || head.ClientId != source || head.ContractId != id {
			return nil, errors.Join(ErrClosedWorkIntegrity, err)
		}
		admission, err := coreprotocol.DecodeOriginalContractAdmission(ctx, original.OriginalCreation)
		if err != nil {
			return nil, errors.Join(ErrClosedWorkIntegrity, err)
		}
		facts, err := admission.Facts(ctx)
		if err != nil || facts.DomainHash != domainHash || facts.ClientId != value.Owner.ClientId || facts.SourceId != source || facts.DestinationId != destination || facts.Generation != value.Owner.Generation || facts.PublicKey != value.Owner.PublicKey || facts.ContractId != id || !bytes.Equal(facts.StoredContract, original.StoredContract) {
			return nil, errors.Join(ErrClosedWorkIntegrity, err)
		}
		result[id] = wholeWorkPriorCreation{Original: cloneWholeWorkCreation(original), Facts: facts, Owner: value.Owner}
	}
	return result, ctx.Err()
}

// The index consumer may request these IDs only after the selected original
// authority and actual source signatures have passed. Returned IDs are lookup
// requests, not proof that any prior admission exists or was published.
func ReadWholeWorkPriorCreationRequests(ctx context.Context, artifact *Artifact, inventory *WholeWorkInventory, expected WholeWorkExpectation) ([][16]byte, error) {
	if ctx == nil || artifact == nil || expected.AuthoritySigner == (common.Address{}) || expected.AttributionSigner == (common.Address{}) {
		return nil, ErrClosedWorkUnavailable
	}
	owned, err := cloneWholeWorkInventory(ctx, inventory)
	if err != nil {
		return nil, err
	}
	if owned == nil {
		return nil, ErrClosedWorkUnavailable
	}
	authority, err := DecodeWholeWorkAuthority(ctx, owned.Authority, expected.AuthoritySigner)
	if err != nil {
		return nil, err
	}
	if expected.AuthoritySigner == artifact.Signer || expected.AttributionSigner != authority.Signer || expected.AuthorityHash != "" && expected.AuthorityHash != wholeWorkBytesHash(owned.Authority) {
		return nil, ErrClosedWorkIntegrity
	}
	domain, err := ClosedWorkReportDomain(artifact)
	if err != nil {
		return nil, err
	}
	if authority.Domain != domain || authority.Epoch != artifact.Epoch || authority.Start != artifact.Start || authority.End != artifact.End {
		return nil, ErrClosedWorkIntegrity
	}
	domainHash, err := domain.Digest()
	if err != nil {
		return nil, err
	}
	pool, err := readWholeWorkParticipantPool(ctx, domainHash, authority.WorkSources, owned.AttributionOriginals)
	if err != nil {
		return nil, err
	}
	ids := map[[16]byte]bool{}
	for _, hash := range pool.streamKVs {
		original := pool.originalKVs[hash]
		if !pool.uniqueSourceAt(original) {
			return nil, ErrClosedWorkUnavailable
		}
		id, err := protocol.ParseProviderWorkId(original.Stream.OriginContractId)
		if err != nil {
			return nil, errors.Join(ErrClosedWorkIntegrity, err)
		}
		ids[id] = true
	}
	result := make([][16]byte, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return bytes.Compare(result[i][:], result[j][:]) < 0 })
	return result, ctx.Err()
}

// Return current original creations only after their exact terminal checkpoint
// was derived in this verified window. The caller admits them with this census.
func retainWholeWorkCurrentCreations(ctx context.Context, artifact *Artifact, authority WholeWorkAuthority, reconciled []WholeWorkPriorContract, contracts map[[16]byte]*wholeWorkContract, creations map[[16]byte]coreprotocol.OriginalContractCreationFacts) ([]WholeWorkRetainedCreation, error) {
	owners := map[wholeWorkCreationOwnerIdentity]WholeWorkOwner{}
	for _, owner := range authority.Owners {
		owners[wholeWorkCreationOwnerIdentity{clientId: owner.ClientId, generation: owner.Generation}] = owner
	}
	result := []WholeWorkRetainedCreation{}
	for _, checkpoint := range reconciled {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if checkpoint.ReconciledEpoch != artifact.Epoch {
			continue
		}
		facts, exists := creations[checkpoint.ContractId]
		contract := contracts[checkpoint.ContractId]
		if !exists || contract == nil {
			continue
		}
		owner, exists := owners[wholeWorkCreationOwnerIdentity{clientId: facts.ClientId, generation: facts.Generation}]
		original, present := contract.ends[facts.SourceId]
		if !exists || !present || sha256.Sum256(original.StoredContract) != checkpoint.StoredContractHash || sha256.Sum256(original.LatestInventory) != checkpoint.SourceInventoryHash {
			return nil, ErrClosedWorkIntegrity
		}
		result = append(result, WholeWorkRetainedCreation{Checkpoint: checkpoint, Owner: owner, Original: cloneWholeWorkCreation(original)})
	}
	return result, ctx.Err()
}

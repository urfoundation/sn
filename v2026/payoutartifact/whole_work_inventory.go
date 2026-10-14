// Whole contract coverage is reconstructed from independently expected SDK
// generations and both original cuts, then joined with actual report chains.
package payoutartifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// Each owner contributes its own exact original; unioning identities alone
// cannot conceal a missing source, omitted SDK or substituted reservation.
type wholeWorkContract struct {
	source      [16]byte
	destination [16]byte
	stored      []byte
	active      bool
	ends        map[[16]byte]coreprotocol.OriginalWorkContract
	starts      map[[16]byte]coreprotocol.OriginalWorkContract
}

// Embedded optional evidence retains the old artifact's signed omission form.
func VerifyWholeWorkInventory(ctx context.Context, artifact *Artifact, expected WholeWorkExpectation) (*VerifiedWholeWorkInventory, error) {
	var inventory *WholeWorkInventory
	if artifact != nil && artifact.ClosedWork != nil {
		inventory = artifact.ClosedWork.WholeInventory
	}
	return VerifyWholeWorkInventoryWithWitness(ctx, artifact, inventory, expected)
}

// A companion witness is separately signed source evidence. The original
// artifact is verified unchanged; adding evidence never rewrites its signature.
func VerifyWholeWorkInventoryWithWitness(ctx context.Context, artifact *Artifact, inventory *WholeWorkInventory, expected WholeWorkExpectation) (*VerifiedWholeWorkInventory, error) {
	if ctx == nil {
		return nil, errors.New("whole-work verification requires an owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if inventory == nil || inventory.Schema != WholeWorkInventorySchema || artifact == nil || artifact.ClosedWork == nil || expected.AuthoritySigner == (common.Address{}) || expected.ClientKeyRootSigner == (common.Address{}) {
		return nil, ErrClosedWorkUnavailable
	}
	if expected.AuthoritySigner == artifact.Signer {
		return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("payout publisher cannot select complete SDK authority"))
	}
	owned, err := cloneWholeWorkInventory(ctx, inventory)
	if err != nil {
		return nil, err
	}
	authority, err := DecodeWholeWorkAuthority(ctx, owned.Authority, expected.AuthoritySigner)
	if err != nil {
		return nil, err
	}
	if authority.ExpectedProviders == nil {
		return nil, ErrClosedWorkUnavailable
	}
	authorityHash := wholeWorkBytesHash(owned.Authority)
	inventoryHash := SnapshotHash(owned)
	if expected.AuthorityHash != "" && (!canonicalClosedWorkDigest(expected.AuthorityHash) || expected.AuthorityHash != authorityHash) {
		return nil, ErrClosedWorkIntegrity
	}
	domain, err := ClosedWorkReportDomain(artifact)
	if err != nil {
		return nil, err
	}
	if authority.Domain != domain || authority.Epoch != artifact.Epoch || authority.Start != artifact.Start || authority.End != artifact.End {
		return nil, ErrClosedWorkIntegrity
	}
	if owned.Clock == nil || owned.Clock.HeaderProfile != authority.ClockProfile {
		return nil, ErrClosedWorkUnavailable
	}
	window, err := VerifyClosedWorkWindow(ctx, artifact, owned.Window, owned.Clock)
	if err != nil {
		return nil, err
	}
	if !window.EpochClockMatched {
		return nil, ErrClosedWorkUnavailable
	}
	if owned.Owners == nil || len(owned.Owners) != len(authority.Owners) {
		return nil, ErrClosedWorkUnavailable
	}
	domainHash, _ := domain.Digest()
	ownerKVs := make(map[[16]byte]WholeWorkOwner, len(authority.Owners))
	contracts := map[[16]byte]*wholeWorkContract{}
	for index, expectedOwner := range authority.Owners {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ownerKVs[expectedOwner.ClientId] = expectedOwner
		originals := owned.Owners[index]
		var cuts [2]coreprotocol.OriginalWorkCut
		for side, original := range []struct {
			request, cut []byte
			kind         string
			boundary     Boundary
			clockUnix    int64
		}{{request: originals.StartRequest, cut: originals.Start, kind: "start", boundary: artifact.Start, clockUnix: owned.Clock.StartTime.Unix()}, {request: originals.EndRequest, cut: originals.End, kind: "end", boundary: artifact.End, clockUnix: owned.Clock.EndTime.Unix()}} {
			if len(original.request) == 0 || len(original.cut) == 0 {
				return nil, ErrClosedWorkUnavailable
			}
			request, err := coreprotocol.DecodeOriginalWorkRequest(original.request, authority.RequestPublicKey)
			if err != nil {
				return nil, errors.Join(ErrClosedWorkIntegrity, err)
			}
			cut, err := coreprotocol.DecodeOriginalWorkCut(ctx, original.cut)
			if err != nil {
				return nil, errors.Join(ErrClosedWorkIntegrity, err)
			}
			hash, e := hex.DecodeString(strings.TrimPrefix(original.boundary.Hash, "0x"))
			if e != nil || len(hash) != 32 || !request.Matches(cut) || request.Kind != original.kind || cut.DomainHash != domainHash || cut.ClientId != expectedOwner.ClientId || cut.Generation != expectedOwner.Generation || cut.PublicKey != expectedOwner.PublicKey || cut.Epoch != artifact.Epoch || cut.Block != original.boundary.Number || cut.BlockHash != [32]byte(hash) {
				return nil, ErrClosedWorkIntegrity
			}
			if request.IssuedAtUnix < original.clockUnix {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("SDK cut permission predates its committed boundary"))
			}
			if !cut.Complete {
				return nil, ErrClosedWorkUnavailable
			}
			cuts[side] = cut
		}
		if cuts[1].Revision < cuts[0].Revision {
			return nil, ErrClosedWorkIntegrity
		}
		if cuts[1].Revision == cuts[0].Revision && SnapshotHash(cuts[1].Contracts) != SnapshotHash(cuts[0].Contracts) {
			return nil, ErrClosedWorkIntegrity
		}
		starts := make(map[[16]byte]coreprotocol.OriginalWorkContract, len(cuts[0].Contracts))
		for _, record := range cuts[0].Contracts {
			starts[record.ContractId] = record
		}
		for _, record := range cuts[1].Contracts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			source, destination, err := record.Parties()
			if err != nil {
				return nil, errors.Join(ErrClosedWorkIntegrity, err)
			}
			active := true
			if previous, exists := starts[record.ContractId]; exists {
				if !bytes.Equal(previous.StoredContract, record.StoredContract) || len(previous.OriginalCreation) != 0 && !bytes.Equal(previous.OriginalCreation, record.OriginalCreation) {
					return nil, ErrClosedWorkIntegrity
				}
				if len(previous.LatestInventory) != 0 {
					before, _ := coreprotocol.DecodeOriginalCloseInventory(previous.LatestInventory)
					after, e := coreprotocol.DecodeOriginalCloseInventory(record.LatestInventory)
					if e != nil || after.Sequence < before.Sequence || after.CumulativeAckedBytes < before.CumulativeAckedBytes || (before.Terminal || after.Sequence == before.Sequence) && !bytes.Equal(previous.LatestInventory, record.LatestInventory) {
						return nil, ErrClosedWorkIntegrity
					}
					// A late start capture does not date an already terminal contract.
					// Only independently approved prior context may exclude it below.
				}
				delete(starts, record.ContractId)
			}
			contract, exists := contracts[record.ContractId]
			if !exists {
				if len(contracts) >= MaxClosedWorkRecords {
					return nil, ErrClosedWorkCapacity
				}
				contract = &wholeWorkContract{source: source, destination: destination, stored: record.StoredContract, ends: map[[16]byte]coreprotocol.OriginalWorkContract{}, starts: map[[16]byte]coreprotocol.OriginalWorkContract{}}
				contracts[record.ContractId] = contract
			} else if !bytes.Equal(contract.stored, record.StoredContract) {
				return nil, ErrClosedWorkIntegrity
			}
			if _, exists := contract.ends[expectedOwner.ClientId]; exists {
				// A later generation cannot overwrite an older owner of carried
				// work. Its original handoff must be proved before coalescing it.
				return nil, ErrClosedWorkUnavailable
			}
			contract.active = contract.active || active
			contract.ends[expectedOwner.ClientId] = record
		}
		for _, record := range cuts[0].Contracts {
			if contract := contracts[record.ContractId]; contract != nil {
				contract.starts[expectedOwner.ClientId] = record
			}
		}
		if len(starts) != 0 {
			return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("SDK end cut omitted retained contract"))
		}
	}
	reservations := make(map[[16]byte]coreprotocol.OriginalWorkContract, len(contracts))
	for id, contract := range contracts {
		reservations[id] = coreprotocol.OriginalWorkContract{ContractId: id, StoredContract: contract.stored}
	}
	creations, err := verifyWholeWorkCreations(ctx, artifact, domainHash, authority.Owners, contracts)
	if err != nil {
		return nil, err
	}
	priorCreations, err := verifyWholeWorkPriorCreations(ctx, artifact, domainHash, expected)
	if err != nil {
		return nil, err
	}
	participants := &wholeWorkParticipantVerification{Complete: window.Credited == 0}
	if expected.AttributionSigner != (common.Address{}) && expected.AttributionSigner == authority.Signer {
		participants, err = verifyWholeWorkParticipants(ctx, artifact, authority, owned, contracts, creations, priorCreations)
		if err != nil {
			return nil, err
		}
		if participants == nil {
			return nil, ErrClosedWorkUnavailable
		}
		if participants.Complete {
			for _, row := range artifact.ClosedWork.Records {
				if _, proved := participants.Outcomes[row.ContractId]; !proved {
					return nil, ErrClosedWorkUnavailable
				}
			}
		}
	}
	closed, err := verifyWholeWorkReportsWithOutcomes(ctx, artifact, expected.ClientKeyRootSigner, reservations, authority.ExpectedProviders, participants.Outcomes, expected.EarningSelection)
	if err != nil {
		return nil, err
	}
	if closed.Window != nil && SnapshotHash(closed.Window) != SnapshotHash(owned.Window) {
		return nil, ErrClosedWorkIntegrity
	}
	if closed.CompleteReportInventories != artifact.ClosedWork.Count || closed.ReservedAmountJoins != artifact.ClosedWork.Count {
		return nil, ErrClosedWorkUnavailable
	}
	if len(expected.PriorContracts) > MaxClosedWorkRecords {
		return nil, ErrClosedWorkCapacity
	}
	expectedPriorKVs := make(map[[16]byte]WholeWorkPriorContract, len(expected.PriorContracts))
	for _, prior := range expected.PriorContracts {
		if _, exists := expectedPriorKVs[prior.ContractId]; exists {
			return nil, ErrClosedWorkIntegrity
		}
		expectedPriorKVs[prior.ContractId] = prior
	}
	reconciled := make([]WholeWorkPriorContract, 0, len(contracts))
	for _, prior := range authority.PriorContracts {
		contract, ok := contracts[prior.ContractId]
		if !ok || sha256.Sum256(contract.stored) != prior.StoredContractHash {
			return nil, ErrClosedWorkIntegrity
		}
		for _, party := range []struct {
			id   [16]byte
			hash [32]byte
		}{{id: contract.source, hash: prior.SourceInventoryHash}, {id: contract.destination, hash: prior.DestinationInventoryHash}} {
			start, startOk := contract.starts[party.id]
			end, endOk := contract.ends[party.id]
			if party.hash == ([32]byte{}) {
				if startOk || endOk {
					return nil, ErrClosedWorkIntegrity
				}
				continue
			}
			if !startOk || !endOk || sha256.Sum256(start.LatestInventory) != party.hash || sha256.Sum256(end.LatestInventory) != party.hash {
				return nil, ErrClosedWorkIntegrity
			}
			head, err := coreprotocol.DecodeOriginalCloseInventory(end.LatestInventory)
			if err != nil || !head.Terminal || prior.DestinationInventoryHash == ([32]byte{}) && head.CumulativeAckedBytes != 0 {
				return nil, ErrClosedWorkIntegrity
			}
		}
		admitted, exists := expectedPriorKVs[prior.ContractId]
		if !exists {
			return nil, ErrClosedWorkUnavailable
		}
		if admitted != prior {
			return nil, ErrClosedWorkIntegrity
		}
		contract.active = false
		reconciled = append(reconciled, prior)
	}
	rows := make(map[[16]byte]ClosedWorkWindowRecord, len(owned.Window.Records))
	for _, row := range owned.Window.Records {
		id, _ := closedWorkId(row.ContractId)
		rows[id] = row
	}
	credited := make(map[[16]byte]ClosedWorkRecord, len(artifact.ClosedWork.Records))
	for _, row := range artifact.ClosedWork.Records {
		credited[row.ContractId] = row
	}
	for id, contract := range contracts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !contract.active {
			continue
		}
		if _, reconciled := expectedPriorKVs[id]; reconciled {
			return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("previously reconciled contract was reassigned to another window"))
		}
		row, exists := rows[id]
		if _, future := participants.FutureReservations[id]; future {
			if exists {
				return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("window includes an independently dated future reservation"))
			}
			// A later SDK cut may include work admitted at or after End. Only
			// the independently bound original reservation can exclude it.
			continue
		}
		if !exists {
			// A delayed boundary capture can include later admissions, just as
			// a delayed start can include earlier completions. Neither signed
			// cut dates the individual event; do not invent its window or fault.
			return nil, errors.Join(ErrClosedWorkUnavailable, errors.New("independently inventoried contract lacks original window assignment"))
		}
		if _, ok := ownerKVs[contract.source]; !ok {
			return nil, ErrClosedWorkIntegrity
		}
		if _, ok := ownerKVs[contract.destination]; !ok {
			return nil, ErrClosedWorkIntegrity
		}
		source, sourcePresent := contract.ends[contract.source]
		if !sourcePresent {
			return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("whole work lacks its expected source owner"))
		}

		destination, destinationPresent := contract.ends[contract.destination]
		sourceHead, sourceErr := coreprotocol.DecodeOriginalCloseInventory(source.LatestInventory)
		destinationHead, destinationErr := coreprotocol.DecodeOriginalCloseInventory(destination.LatestInventory)
		switch row.Disposition {
		case "credited":
			if sourceErr != nil || destinationErr != nil || !sourceHead.Terminal || !destinationHead.Terminal {
				return nil, ErrClosedWorkUnavailable
			}
			var reports ClosedWorkReports
			if err := json.Unmarshal(credited[id].OriginalReports, &reports); err != nil {
				return nil, ErrClosedWorkIntegrity
			}
			joined := map[[16]byte]bool{}
			for _, report := range reports.Reports {
				if report.Checkpoint == nil || *report.Checkpoint {
					continue
				}
				client, err := closedWorkId(report.ClientId)
				original, ok := contract.ends[client]
				if err != nil || !ok || !bytes.Equal(original.LatestInventory, report.Inventory) || report.Party == "source" && client != contract.source || report.Party == "destination" && client != contract.destination {
					return nil, ErrClosedWorkIntegrity
				}
				joined[client] = true
			}
			if !joined[contract.source] || !joined[contract.destination] {
				return nil, ErrClosedWorkUnavailable
			}
		case "canceled":
			if sourceErr != nil || !sourceHead.Terminal || sourceHead.CumulativeAckedBytes != 0 {
				return nil, ErrClosedWorkUnavailable
			}
			if destinationPresent && (destinationErr != nil || !destinationHead.Terminal || destinationHead.CumulativeAckedBytes != 0) {
				return nil, ErrClosedWorkUnavailable
			}
			// A signed zero terminal proves no earned bytes, but carries no
			// original cancellation clock. SQL cannot supply that missing fact.
			participants.Complete = false
		case "open":
			if _, dated := participants.OpenThroughEnd[id]; !dated {
				// A late nonterminal SDK cut does not itself place admission
				// inside this epoch or prove the original state at End.
				participants.Complete = false
			}
			if sourceErr == nil && sourceHead.Terminal && destinationErr == nil && destinationHead.Terminal {
				if _, open := participants.OpenThroughEnd[id]; !open {
					return nil, ErrClosedWorkUnavailable
				}
			}
		case "unassigned_canceled":
			return nil, ErrClosedWorkUnavailable
		default:
			return nil, ErrClosedWorkIntegrity
		}
		if row.Disposition == "credited" || row.Disposition == "canceled" {
			checkpoint := WholeWorkPriorContract{ContractId: id, ReconciledEpoch: artifact.Epoch, InventoryHash: inventoryHash, StoredContractHash: sha256.Sum256(contract.stored), SourceInventoryHash: sha256.Sum256(source.LatestInventory)}
			if destinationPresent {
				checkpoint.DestinationInventoryHash = sha256.Sum256(destination.LatestInventory)
			}
			reconciled = append(reconciled, checkpoint)
		}
		delete(rows, id)
	}
	if len(rows) != 0 {
		return nil, errors.Join(ErrClosedWorkIntegrity, errors.New("window includes work outside the independently complete SDK census"))
	}
	earningStart, err := verifyWholeWorkEarningSelection(artifact.ClosedWork, expected.EarningSelection)
	if err != nil {
		return nil, err
	}
	providerKVs := map[[16]byte]WholeWorkProvider{}
	for _, provider := range authority.ExpectedProviders {
		providerKVs[provider.ClientId] = WholeWorkProvider{ClientId: provider.ClientId, NetworkId: provider.NetworkId, WalletHeadHash: provider.WalletHeadHash, WalletGeneration: provider.WalletGeneration}
	}
	for _, row := range artifact.ClosedWork.Records {
		snapshot, err := decodeClosedWorkSnapshot(row, artifact.Epoch)
		if err != nil {
			return nil, err
		}
		for _, provider := range *snapshot.Providers {
			id, _ := closedWorkId(provider.ClientId)
			network, _ := closedWorkId(provider.NetworkId)
			owner, ok := ownerKVs[id]
			if !ok || owner.NetworkId != network {
				return nil, ErrClosedWorkIntegrity
			}
			value, expected := providerKVs[id]
			if !expected || value.NetworkId != network || uint64(*provider.ByteCount) > ^uint64(0)-value.UsageBytes {
				return nil, ErrClosedWorkIntegrity
			}
			closedAt, err := time.Parse(time.RFC3339Nano, row.ClosedAt)
			if err != nil {
				return nil, ErrClosedWorkIntegrity
			}
			if earningStart.IsZero() || !closedAt.Before(earningStart) {
				value.UsageBytes += uint64(*provider.ByteCount)
			}
			providerKVs[id] = value
		}
	}
	providers := make([]WholeWorkProvider, 0, len(providerKVs))
	for _, provider := range providerKVs {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool { return bytes.Compare(providers[i].ClientId[:], providers[j].ClientId[:]) < 0 })
	sort.Slice(reconciled, func(i, j int) bool {
		return bytes.Compare(reconciled[i].ContractId[:], reconciled[j].ContractId[:]) < 0
	})
	var retainedCreations []WholeWorkRetainedCreation
	if participants.Complete {
		retainedCreations, err = retainWholeWorkCurrentCreations(ctx, artifact, authority, reconciled, contracts, creations)
		if err != nil {
			return nil, err
		}
	}
	return &VerifiedWholeWorkInventory{Complete: true, AttributionComplete: participants.Complete, Domain: domain, Epoch: artifact.Epoch, Start: artifact.Start, End: artifact.End, AuthorityHash: authorityHash, InventoryHash: inventoryHash, WindowHash: window.Hash, Contracts: window.Credited + window.Canceled + window.Open, Credited: window.Credited, Canceled: window.Canceled, Open: window.Open, ExpectedProviders: providers, ReconciledContracts: reconciled, RetainedCreations: retainedCreations, Reports: closed}, ctx.Err()
}

// Public sidecar reads use a strict bounded grammar before any expensive join.
func DecodeWholeWorkInventory(ctx context.Context, raw []byte) (*WholeWorkInventory, error) {
	if ctx == nil {
		return nil, errors.New("whole-work inventory requires an owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, ErrClosedWorkUnavailable
	}
	if len(raw) > MaxWholeWorkInventoryBytes {
		return nil, ErrClosedWorkCapacity
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, errors.Join(ErrClosedWorkIntegrity, err)
	}
	var value WholeWorkInventory
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.Join(ErrClosedWorkIntegrity, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrClosedWorkIntegrity
	}
	if value.Schema != WholeWorkInventorySchema {
		return nil, ErrClosedWorkUnavailable
	}
	return cloneWholeWorkInventory(ctx, &value)
}

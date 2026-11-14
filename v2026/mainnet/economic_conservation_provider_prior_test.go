// Retained provider history is indexed by actual contract identity. Publisher
// proposals cannot select which previous credits the containing owner remembers.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

// A private admitted index is a projection of a containing original census;
// these tests exercise that projection's identity gate, not source signatures.
func economicProviderPriorFixture() (*economicConservationArchiveView, *economicConservationState, *payoutartifact.WholeWorkAuthority, *payoutartifact.WholeWorkInventory, payoutartifact.WholeWorkPriorContract) {
	domain := protocol.ClientKeyHistoryDomain{ChainID: 964, NoID: 1}
	original := payoutartifact.WholeWorkPriorContract{ContractId: [16]byte{17}, ReconciledEpoch: 2, InventoryHash: "sha256:" + strings.Repeat("1", 64), StoredContractHash: [32]byte{2}, SourceInventoryHash: [32]byte{3}, DestinationInventoryHash: [32]byte{4}}
	hash := strings.Repeat("5", 64)
	record := economicConservationEntitlement{Id: economicConservationEntitlementId("2", "1"), Epoch: "2", PoolId: "1", Funded: "7", Census: &economicConservationEntitlementCensus{ContentHash: hash}}
	key := economicProviderContractKey{Domain: domain, ContractId: original.ContractId}
	view := &economicConservationArchiveView{providerCensuses: map[string]*economicProviderAdmitted{hash: {Domain: domain, Epoch: 2, InventoryHash: original.InventoryHash, NewContracts: map[[16]byte]payoutartifact.WholeWorkPriorContract{original.ContractId: original}}}, providerContracts: map[economicProviderContractKey]map[string]struct{}{key: {hash: {}}}, entitlementVerified: map[string]string{hash: economicEntitlementFundingHash(record)}, entitlements: map[string]economicConservationEntitlement{}}
	state := &economicConservationState{Entitlements: []economicConservationEntitlement{record}, entitlementIds: map[string]int{record.Id: 0}}
	authority := &payoutartifact.WholeWorkAuthority{Domain: domain, Epoch: 3}
	id := original.ContractId
	inventory := &payoutartifact.WholeWorkInventory{Window: &payoutartifact.ClosedWorkWindow{Records: []payoutartifact.ClosedWorkWindowRecord{{ContractId: fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), Disposition: "credited"}}}}
	return view, state, authority, inventory, original
}

// Omitting a prior proposal cannot hide the same terminal contract. The same
// identity is returned after its census retires into the held cold index.
func TestEconomicProviderPriorLookupIncludesOmittedProposalAcrossRetirement(t *testing.T) {
	view, state, authority, inventory, original := economicProviderPriorFixture()
	for stage := 0; stage < 2; stage++ {
		values, err := view.providerPriorContracts(t.Context(), state, authority, inventory)
		if err != nil || len(values) != 1 || values[0] != original {
			t.Fatal("original prior credit depended on proposal or active location", stage, values, err)
		}
		record := state.Entitlements[0]
		view.entitlements[record.Id] = record
		state.entitlementIds = nil
	}
	authority.PriorContracts = []payoutartifact.WholeWorkPriorContract{original}
	values, err := view.providerPriorContracts(t.Context(), state, authority, inventory)
	if err != nil || len(values) != 1 || values[0] != original {
		t.Fatal("legitimate exact prior exclusion changed", values, err)
	}
}

// A refused candidate may have a detached cached result; it cannot establish
// earlier credit until its exact complete census and funding are admitted.
func TestEconomicProviderPriorLookupRefusesUnpublishedAndForeignPredecessors(t *testing.T) {
	view, state, authority, inventory, original := economicProviderPriorFixture()
	record := state.Entitlements[0]
	delete(view.entitlementVerified, record.censusHash())
	values, err := view.providerPriorContracts(t.Context(), state, authority, inventory)
	if err != nil || len(values) != 0 {
		t.Fatal("refused candidate acquired prior publication", values, err)
	}
	authority.PriorContracts = []payoutartifact.WholeWorkPriorContract{original}
	if values, err := view.providerPriorContracts(t.Context(), state, authority, inventory); !errors.Is(err, payoutartifact.ErrClosedWorkUnavailable) || values != nil {
		t.Fatal("unpublished proposed exclusion acquired authority", values, err)
	}
	view.entitlementVerified[record.censusHash()] = economicEntitlementFundingHash(record)
	authority.PriorContracts[0].SourceInventoryHash[0]++
	if values, err := view.providerPriorContracts(t.Context(), state, authority, inventory); !errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) || values != nil {
		t.Fatal("changed original terminal head acquired prior authority", values, err)
	}
	authority.PriorContracts = nil
	authority.Domain.NoID = 2
	if values, err := view.providerPriorContracts(t.Context(), state, authority, inventory); err != nil || len(values) != 0 {
		t.Fatal("foreign pool borrowed retained contract history", values, err)
	}
}

// Already credited later work cannot be moved into an earlier delayed payout.
// Cancellation also leaves no partial checkpoint inventory for a caller to use.
func TestEconomicProviderPriorLookupRejectsReverseReplayAndCancellation(t *testing.T) {
	view, state, authority, inventory, _ := economicProviderPriorFixture()
	authority.Epoch = 1
	if values, err := view.providerPriorContracts(t.Context(), state, authority, inventory); !errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) || values != nil {
		t.Fatal("later original credit moved to an earlier window", values, err)
	}
	authority.Epoch = 3
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if values, err := view.providerPriorContracts(ctx, state, authority, inventory); !errors.Is(err, context.Canceled) || values != nil {
		t.Fatal("canceled lookup published partial prior authority", values, err)
	}
}

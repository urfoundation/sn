// Retired stream dependencies retain the exact admitted original creation;
// these index tests do not substitute for the full signed source replay roots.
package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
)

// The private index fixture deliberately uses opaque bytes. The production
// verifier admits them only after full signatures, parties and checkpoints pass.
func economicProviderPriorCreationFixture() (*economicConservationArchiveView, *economicConservationState, *payoutartifact.WholeWorkAuthority, *payoutartifact.WholeWorkInventory, payoutartifact.WholeWorkRetainedCreation) {
	view, state, authority, inventory, checkpoint := economicProviderPriorFixture()
	creation := payoutartifact.WholeWorkRetainedCreation{Checkpoint: checkpoint, Owner: payoutartifact.WholeWorkOwner{ClientId: [16]byte{7}, NetworkId: [16]byte{8}, Generation: [16]byte{3}, PublicKey: [32]byte{9}}, Original: coreprotocol.OriginalWorkContract{ContractId: checkpoint.ContractId, StoredContract: []byte("original reservation"), LatestInventory: []byte("original terminal head"), OriginalCreation: []byte("original admitted request")}}
	view.providerCensuses[state.Entitlements[0].censusHash()].NewCreations = map[[16]byte]payoutartifact.WholeWorkRetainedCreation{checkpoint.ContractId: creation}
	inventory.Window.Records = nil
	return view, state, authority, inventory, creation
}

// An inherited origin can be absent from every current SDK cut and window row.
// The same admitted original survives retirement and is returned as a copy.
func TestEconomicProviderPriorCreationSurvivesRetirementWithoutProposal(t *testing.T) {
	view, state, authority, inventory, original := economicProviderPriorCreationFixture()
	original = cloneEconomicProviderCreation(original)
	for stage := 0; stage < 2; stage++ {
		contracts, creations, err := view.providerPriorOriginals(t.Context(), state, authority, inventory, [][16]byte{original.Checkpoint.ContractId})
		if err != nil || len(contracts) != 1 || contracts[0] != original.Checkpoint || len(creations) != 1 || !reflect.DeepEqual(creations[0], original) {
			t.Fatal("retired original creation depended on current proposals", stage, contracts, creations, err)
		}
		creations[0].Original.StoredContract[0]++
		creations[0].Original.LatestInventory[0]++
		creations[0].Original.OriginalCreation[0]++
		record := state.Entitlements[0]
		if !reflect.DeepEqual(view.providerCensuses[record.censusHash()].NewCreations[original.Checkpoint.ContractId], original) {
			t.Fatal("returned original creation mutated retained private custody")
		}
		view.entitlements[record.Id] = record
		state.entitlementIds = nil
	}
}

// Neither a refused census nor foreign pool membership can supply an original
// birth. An altered checkpoint is a contradiction, not a missing dependency.
func TestEconomicProviderPriorCreationRequiresPublishedExactCheckpoint(t *testing.T) {
	view, state, authority, inventory, original := economicProviderPriorCreationFixture()
	record := state.Entitlements[0]
	delete(view.entitlementVerified, record.censusHash())
	contracts, creations, err := view.providerPriorOriginals(t.Context(), state, authority, inventory, [][16]byte{original.Checkpoint.ContractId})
	if err != nil || len(contracts) != 0 || len(creations) != 0 {
		t.Fatal("unpublished candidate supplied retained original creation", contracts, creations, err)
	}
	view.entitlementVerified[record.censusHash()] = economicEntitlementFundingHash(record)
	authority.Domain.NoID++
	contracts, creations, err = view.providerPriorOriginals(t.Context(), state, authority, inventory, [][16]byte{original.Checkpoint.ContractId})
	if err != nil || len(contracts) != 0 || len(creations) != 0 {
		t.Fatal("foreign pool borrowed original creation", contracts, creations, err)
	}
	authority.Domain.NoID--
	altered := original
	altered.Checkpoint.SourceInventoryHash[0]++
	view.providerCensuses[record.censusHash()].NewCreations[original.Checkpoint.ContractId] = altered
	contracts, creations, err = view.providerPriorOriginals(t.Context(), state, authority, inventory, [][16]byte{original.Checkpoint.ContractId})
	if !errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) || contracts != nil || creations != nil {
		t.Fatal("changed creation checkpoint acquired retained authority", contracts, creations, err)
	}
	view.providerCensuses[record.censusHash()].NewCreations[original.Checkpoint.ContractId] = original
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	contracts, creations, err = view.providerPriorOriginals(ctx, state, authority, inventory, [][16]byte{original.Checkpoint.ContractId})
	if !errors.Is(err, context.Canceled) || contracts != nil || creations != nil {
		t.Fatal("canceled original lookup returned partial authority", contracts, creations, err)
	}
}

// Raw originals consume the same reviewed index reserve as their checkpoints.
// Capacity refusal and later parent refusal restore every charge and membership.
func TestEconomicProviderPriorCreationCapacityAndRefusalPreserveReserve(t *testing.T) {
	_, state, _, _, creation := economicProviderPriorCreationFixture()
	creation.Original.OriginalCreation = bytes.Repeat([]byte{0x71}, 8192)
	value := &economicProviderAdmitted{Epoch: 2, InventoryHash: creation.Checkpoint.InventoryHash, NewContracts: map[[16]byte]payoutartifact.WholeWorkPriorContract{creation.Checkpoint.ContractId: creation.Checkpoint}}
	baseline := economicProviderCompleteView(t.Context())
	if err := baseline.retainProviderOriginals(t.Context(), state.Entitlements[0].censusHash(), value); err != nil {
		t.Fatal(err)
	}
	value.NewCreations = map[[16]byte]payoutartifact.WholeWorkRetainedCreation{creation.Checkpoint.ContractId: creation}
	view := economicProviderCompleteView(t.Context())
	view.resources.IndexBytes = 2 * (baseline.bytes + 128)
	candidate, err := view.beginProviderCandidate()
	if err != nil {
		t.Fatal(err)
	}
	err = view.retainProviderOriginals(t.Context(), "candidate", value)
	candidate.finish(false)
	if !errors.Is(err, errMonitorEconomicCapacity) || view.entries != 0 || view.bytes != 0 || len(view.providerCensuses) != 0 || len(view.providerContracts) != 0 {
		t.Fatal("raw original creation escaped reviewed capacity", err, view.entries, view.bytes)
	}
	view.resources.IndexBytes = 1024 * 1024
	candidate, err = view.beginProviderCandidate()
	if err != nil {
		t.Fatal(err)
	}
	if err := view.retainProviderOriginals(t.Context(), "candidate", value); err != nil || view.bytes <= baseline.bytes+8192 {
		t.Fatal("healthy original creation did not retain actual bytes", err, view.bytes)
	}
	candidate.finish(false)
	if view.entries != 0 || view.bytes != 0 || len(view.providerCensuses) != 0 || len(view.providerContracts) != 0 {
		t.Fatal("refused original creation survived parent rollback")
	}
}

// A repeated immutable census cannot swap its original bytes while preserving
// the same financial projection and inventory identifier.
func TestEconomicProviderPriorCreationCacheRejectsChangedOriginal(t *testing.T) {
	view, state, _, _, creation := economicProviderPriorCreationFixture()
	key := state.Entitlements[0].censusHash()
	original := view.providerCensuses[key]
	changed := *original
	changed.NewCreations = map[[16]byte]payoutartifact.WholeWorkRetainedCreation{creation.Checkpoint.ContractId: cloneEconomicProviderCreation(creation)}
	value := changed.NewCreations[creation.Checkpoint.ContractId]
	value.Original.OriginalCreation[0]++
	changed.NewCreations[creation.Checkpoint.ContractId] = value
	if err := view.retainProviderOriginals(t.Context(), key, &changed); !errors.Is(err, errRpcIntegrity) || view.providerCensuses[key] != original {
		t.Fatal("changed original creation reused immutable census", err)
	}
	if err := view.retainProviderOriginals(t.Context(), key, original); err != nil {
		t.Fatal("unchanged original creation retry failed", err)
	}
}

// This selected root requires both actual producer exports. It proves the cold
// consumer derives raw creation custody from complete originals, not fixture flags.
func TestEconomicProviderCompleteOriginalsRetainPriorCreations(t *testing.T) {
	policy, record := economicProviderCompleteFixture(t)
	view := economicProviderCompleteView(t.Context())
	value, err := view.verifyProviderOriginals(t.Context(), policy, &economicConservationState{}, record)
	if err != nil || value == nil || len(value.NewContracts) != 7 || len(value.NewCreations) != 7 {
		t.Fatal("complete originals did not retain exact creation dependencies", value, err)
	}
	if err := view.retainProviderOriginals(t.Context(), record.censusHash(), value); err != nil {
		t.Fatal(err)
	}
	view.entitlementVerified = map[string]string{record.censusHash(): economicEntitlementFundingHash(record)}
	view.entitlements = map[string]economicConservationEntitlement{record.Id: record}
	authority := &payoutartifact.WholeWorkAuthority{Domain: value.Domain, Epoch: value.Epoch + 1}
	inventory := &payoutartifact.WholeWorkInventory{Window: &payoutartifact.ClosedWorkWindow{}}
	for id, expected := range value.NewCreations {
		contracts, originals, err := view.providerPriorOriginals(t.Context(), &economicConservationState{}, authority, inventory, [][16]byte{id})
		if err != nil || len(contracts) != 1 || contracts[0] != expected.Checkpoint || len(originals) != 1 || !reflect.DeepEqual(originals[0], expected) {
			t.Fatal("actual retired original creation is unreachable", id, originals, err)
		}
	}
}

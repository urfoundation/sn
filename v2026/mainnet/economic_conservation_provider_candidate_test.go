// These tests inspect the private admission transaction, not source authority.
// Full original witness verification and public acquisition have separate roots.
package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Existing original membership remains in place while a candidate acquires a
// different census under both a shared contract and a new contract identity.
func economicProviderCandidateFixture() (*economicConservationArchiveView, *economicProviderAdmitted) {
	domain := protocol.ClientKeyHistoryDomain{ChainID: 964, NoID: 1}
	shared := economicProviderContractKey{Domain: domain, ContractId: [16]byte{1}}
	view := &economicConservationArchiveView{
		resources: economicConservationResources{IndexEntries: 1000, IndexBytes: 1024 * 1024}, entries: 3, bytes: 2000,
		providerCensuses:    map[string]*economicProviderAdmitted{"original": {Domain: domain, Epoch: 1}},
		providerContracts:   map[economicProviderContractKey]map[string]struct{}{shared: {"original": {}}},
		entitlementVerified: map[string]string{"original": "original-funding"},
		entitlementLeaves:   map[string]map[string]string{"original": {"original-coldkey": "10000"}},
	}
	value := &economicProviderAdmitted{Domain: domain, Epoch: 2, InventoryHash: "synthetic-private-index", NewContracts: map[[16]byte]payoutartifact.WholeWorkPriorContract{
		{1}: {ContractId: [16]byte{1}, ReconciledEpoch: 2}, {2}: {ContractId: [16]byte{2}, ReconciledEpoch: 2},
	}}
	return view, value
}

// The real retention function writes and charges before a later parent refusal.
// Rollback preserves every preexisting key and permits an identical clean retry.
func TestEconomicProviderCandidateRefusalRestoresOriginalIndexAndRetry(t *testing.T) {
	view, value := economicProviderCandidateFixture()
	original := view.providerCensuses["original"]
	for attempt := 0; attempt < 2; attempt++ {
		candidate, err := view.beginProviderCandidate()
		if err != nil {
			t.Fatal(err)
		}
		if err := view.retainProviderOriginals(t.Context(), "candidate", value); err != nil {
			t.Fatal(err)
		}
		candidate.captureCensus("candidate")
		if err := view.charge([]string{"candidate", "candidate-funding"}); err != nil {
			t.Fatal(err)
		}
		view.entitlementVerified["candidate"] = "candidate-funding"
		view.entitlementLeaves["candidate"] = map[string]string{"candidate-coldkey": "10000"}
		candidate.finish(false)
		candidate.finish(false)
		if view.providerCandidate != nil || view.entries != 3 || view.bytes != 2000 || len(view.providerCensuses) != 1 || view.providerCensuses["original"] != original || len(view.providerContracts) != 1 || len(view.providerContracts[economicProviderContractKey{Domain: value.Domain, ContractId: [16]byte{1}}]) != 1 || !reflect.DeepEqual(view.entitlementVerified, map[string]string{"original": "original-funding"}) || !reflect.DeepEqual(view.entitlementLeaves, map[string]map[string]string{"original": {"original-coldkey": "10000"}}) {
			t.Fatal("refused provider candidate changed original private admission", attempt, view.entries, view.bytes)
		}
	}
	candidate, err := view.beginProviderCandidate()
	if err != nil {
		t.Fatal(err)
	}
	if err := view.retainProviderOriginals(t.Context(), "candidate", value); err != nil {
		t.Fatal(err)
	}
	candidate.finish(true)
	entries, used := view.entries, view.bytes
	if len(view.providerCensuses) != 2 || entries <= 3 || used <= 2000 {
		t.Fatal("accepted provider candidate did not retain its originals", entries, used)
	}
	if err := view.retainProviderOriginals(t.Context(), "candidate", value); err != nil || view.entries != entries || view.bytes != used {
		t.Fatal("identical original retry was charged twice", err, view.entries, view.bytes)
	}
}

// A refused first candidate restores nil optional caches, while cancellation
// and partial capacity charging cannot consume the healthy owner's reserve.
func TestEconomicProviderCandidateCancellationAndCapacityRestoreReserve(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		_, value := economicProviderCandidateFixture()
		view := &economicConservationArchiveView{resources: economicConservationResources{IndexEntries: 4, IndexBytes: 1024 * 1024}}
		candidate, err := view.beginProviderCandidate()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		if canceled {
			cancel()
		}
		err = view.retainProviderOriginals(ctx, "candidate", value)
		cancel()
		if !errors.Is(err, errMonitorEconomicCapacity) && !canceled || canceled && !errors.Is(err, context.Canceled) {
			t.Fatal("original candidate refusal changed", canceled, err)
		}
		candidate.finish(false)
		if view.entries != 0 || view.bytes != 0 || view.providerCensuses != nil || view.providerContracts != nil || view.entitlementVerified != nil || view.entitlementLeaves != nil || view.providerCandidate != nil {
			t.Fatal("refused first provider retained private allocation", canceled, view.entries, view.bytes)
		}
	}
}

// Parent serialization is part of the ownership contract; a nested candidate
// cannot replace the first operation's undo journal or discard its keys.
func TestEconomicProviderCandidateCannotReplaceAnotherParentTransaction(t *testing.T) {
	view, _ := economicProviderCandidateFixture()
	original, err := view.beginProviderCandidate()
	if err != nil {
		t.Fatal(err)
	}
	if candidate, err := view.beginProviderCandidate(); err == nil || candidate != nil || view.providerCandidate != original {
		t.Fatal("nested provider candidate replaced original parent", candidate, err)
	}
	original.finish(false)
}

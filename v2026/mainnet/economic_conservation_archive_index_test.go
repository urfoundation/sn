// Index admission preserves legacy receipt-only calls while rejecting broken
// archive declarations before original evidence or capacity counters change.
package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestEconomicConservationArchiveIndexNilOperandsRefuseBeforeMutation(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	original := f.source.state(t)
	resources, err := original.resources(f.source.policy)
	if err != nil {
		t.Fatal(err)
	}
	view := newEconomicConservationArchiveView(resources)
	for _, check := range []struct{ original, compacted *economicConservationState }{
		{original: nil, compacted: &economicConservationState{}},
		{original: &original, compacted: nil},
	} {
		if err := view.admit(t.Context(), check.original, check.compacted); err == nil || !strings.Contains(err.Error(), "requires original and compacted") {
			t.Fatal("nil archive operand did not refuse before indexing", err)
		}
		if view.entries != 0 || view.bytes != 0 || len(view.receipts) != 0 || len(view.feeEvidence) != 0 {
			t.Fatal("nil operand partially changed original index", view)
		}
	}
	var absent *economicConservationArchiveView
	if err := absent.admit(t.Context(), &original, &economicConservationState{}); err == nil {
		t.Fatal("nil archive owner was accepted")
	}
}

func TestEconomicConservationArchiveIndexMalformedCatalogRefusesBeforeMutation(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	original := f.source.state(t)
	resources, err := original.resources(f.source.policy)
	if err != nil {
		t.Fatal(err)
	}
	valid := f.request.Original
	for _, archive := range []*economicConservationArchive{
		{},
		{Segments: []monitorHistoryReference{{}}},
		{Segments: []monitorHistoryReference{valid, valid}},
		{Segments: []monitorHistoryReference{valid}, NativeFees: &economicConservationFeeSummary{}},
		{Segments: []monitorHistoryReference{valid}, FeeRetirements: []monitorHistoryReference{valid}},
	} {
		view := newEconomicConservationArchiveView(resources)
		before := rootObjectHash(original)
		if err := view.admit(t.Context(), &original, &economicConservationState{Archive: archive}); err == nil {
			t.Fatal("malformed archive catalog was indexed", archive)
		}
		if view.entries != 0 || view.bytes != 0 || len(view.receipts) != 0 || len(view.feeEvidence) != 0 || before != rootObjectHash(original) {
			t.Fatal("malformed catalog partially changed original index or source", view)
		}
	}
}

func TestEconomicConservationArchiveIndexLostCustodyRefusesBeforeMutation(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	original := f.source.state(t)
	resources, err := original.resources(f.source.policy)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := openMonitorHistoryReader(f.ctx, f.request.Original)
	if err != nil {
		t.Fatal(err)
	}
	view := newEconomicConservationArchiveView(resources)
	view.owners = []*monitorHistorySnapshot{owner}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if err := view.admit(t.Context(), &original, &economicConservationState{}); !errors.Is(err, os.ErrClosed) {
		t.Fatal("lost original owner did not refuse archive indexing", err)
	}
	if view.entries != 0 || view.bytes != 0 || len(view.receipts) != 0 {
		t.Fatal("lost owner changed original index")
	}
	// Receipt-only admission remains valid with a live legacy owner and is
	// idempotent for the first known original, not an implicit fee retirement.
	view = newEconomicConservationArchiveView(resources)
	input := &economicConservationState{Receipts: original.Receipts}
	if err := view.admit(t.Context(), input, &economicConservationState{}); err != nil {
		t.Fatal("legacy receipt-only admission", err)
	}
	entries, retainedBytes := view.entries, view.bytes
	if err := view.admit(t.Context(), input, &economicConservationState{}); err != nil || view.entries != entries || view.bytes != retainedBytes || !reflect.DeepEqual(view.receipts[economicConservationReceiptKey(original.Receipts[0])], original.Receipts[0]) {
		t.Fatal("known receipt retry changed original identity or capacity", err)
	}
}

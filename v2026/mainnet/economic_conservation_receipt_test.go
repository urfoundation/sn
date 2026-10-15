// Receipt archive guards preserve the first original transaction evidence;
// observation refresh and unknown-to-known refinement have distinct controls.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEconomicConservationArchivedReceiptContradictionRefusesPublicReopenAndPlan(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	original := f.source.state(t)
	if len(original.Receipts) != 1 || original.Receipts[0].Observation.UnpaidCreditRao != "12" {
		t.Fatal("actual signed receipt fixture is absent", original.Receipts)
	}
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("archive baseline", code, issue)
	}
	state := f.source.state(t)
	forged := original.Receipts[0]
	forged.Observation.UnpaidCreditRao = "13"
	state.Receipts = []economicConservationReceipt{forged}
	state.ContentHash = state.hash()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	// Publish through the real physical-head owner so this tests the logical
	// receipt guard rather than failing an earlier named-inode custody check.
	writer, err := openMonitorHistorySnapshot(f.ctx, f.source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(writer.publish(append(raw, '\n'), nil), writer.close()); err != nil {
		t.Fatal(err)
	}
	f.reset(t)
	path, hash := f.document(t, "contradictory-request.json", f.request)
	before := mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))
	reads := f.source.claimReads.Load()
	for _, check := range []struct {
		command []string
		status  int
	}{
		{command: f.source.args(t), status: 3},
		{command: []string{"economic-conservation-archive", "plan", "--request", path, "--request-sha256", hash}, status: 2},
	} {
		var output, diagnostic bytes.Buffer
		code := runMainWithMonitorHooks(f.ctx, check.command, &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
		if code != check.status || output.Len() != 0 || !strings.Contains(diagnostic.String(), "archived original Claim receipt contradicts") || f.source.claimReads.Load() != reads {
			t.Fatal("public archived receipt contradiction was overwritten or reached another source read", code, output.String(), diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.source.checkpoint))) {
			t.Fatal("refused archived receipt changed the original custody")
		}
	}
}

func TestEconomicConservationArchiveIndexNeverOverwritesOriginalReceipt(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	original := f.source.state(t)
	resources, err := original.resources(f.source.policy)
	if err != nil || len(original.Receipts) != 1 {
		t.Fatal("baseline receipt resources", err, original.Receipts)
	}
	view := newEconomicConservationArchiveView(resources)
	input := &economicConservationState{Receipts: original.Receipts}
	if err := view.admit(t.Context(), input, &economicConservationState{}); err != nil {
		t.Fatal("original index admission", err)
	}
	first := original.Receipts[0]
	key := economicConservationReceiptKey(first)
	entries, retainedBytes := view.entries, view.bytes
	for _, change := range []struct {
		name  string
		apply func(*economicConservationReceipt)
	}{
		{name: "original transaction", apply: func(value *economicConservationReceipt) {
			value.Observation.TransactionHash = "0x" + strings.Repeat("12", 32)
		}},
		{name: "original block", apply: func(value *economicConservationReceipt) { value.Observation.BlockNumber++ }},
		{name: "accepted amount", apply: func(value *economicConservationReceipt) { value.Observation.AcceptedAmountRao = "8" }},
		{name: "deferred amount", apply: func(value *economicConservationReceipt) { value.Observation.UnpaidCreditRao = "13" }},
		{name: "later aggregate payment", apply: func(value *economicConservationReceipt) {
			value.Observation.PaymentStatus, value.Observation.UnpaidCreditRao, value.Observation.AggregatePaidRao = "aggregate-paid", "", "12"
		}},
		{name: "relayer", apply: func(value *economicConservationReceipt) { value.Observation.Relayer = "0x" + strings.Repeat("23", 20) }},
	} {
		changed := first
		change.apply(&changed)
		if err := changed.Observation.Validate(); err != nil {
			t.Fatal("contradiction must have valid wire grammar", change.name, err)
		}
		input.Receipts = []economicConservationReceipt{changed}
		if err := view.admit(t.Context(), input, &economicConservationState{}); err == nil || !strings.Contains(err.Error(), "archived original Claim receipt contradicts") {
			t.Fatal("archive index replaced a different original receipt", change.name, err)
		}
		if !reflect.DeepEqual(view.receipts[key], first) || view.entries != entries || view.bytes != retainedBytes {
			t.Fatal("refused receipt changed original index evidence or capacity", change.name)
		}
	}
}

func TestEconomicConservationPublicReceiptRefreshRetainsFirstArchiveBytes(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	f.sample(t, monitorServiceHooks{})
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("archive baseline", code, issue)
	}
	original, err := os.ReadFile(f.request.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	baseline := f.sample(t, monitorServiceHooks{})
	birth := f.source.state(t).ClaimStates[0].Record.StartedAt
	for range 3 {
		f.source.now = f.source.now.Add(time.Second)
		summary := f.sample(t, monitorServiceHooks{})
		state := f.source.state(t)
		if summary.MatchedReceipts != 1 || len(state.Receipts) != 0 || summary.ArchiveIndexEntries != baseline.ArchiveIndexEntries || summary.ArchiveIndexBytes != baseline.ArchiveIndexBytes {
			t.Fatal("observation refresh replaced original receipt or charged new capacity", summary, state.Receipts)
		}
		if state.ClaimStates[0].Record.StartedAt != birth || state.ClaimStates[0].Record.PublishedAt != f.source.now.Format(time.RFC3339Nano) {
			t.Fatal("receipt refresh changed its original instance birth or lost new publication", state.ClaimStates[0])
		}
	}
	retained, err := os.ReadFile(f.request.ArchivePath)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("receipt refresh rewrote original archived bytes", err)
	}
}

func TestEconomicConservationPublicUnknownReceiptPaymentRemainsHotUntilKnown(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false, func(source *economicConservationFixture) { source.claimPaymentUnknown.Store(true) })
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("unknown receipt archive", code, issue)
	}
	unknown := f.source.state(t)
	if len(unknown.Receipts) != 1 || unknown.Receipts[0].ClaimId == "" || unknown.Receipts[0].Observation.PaymentStatus != "unknown" || unknown.Archive.Counts.Receipts != 0 {
		t.Fatal("archive retired an unresolved original receipt payment", unknown.Receipts, unknown.Archive)
	}
	f.source.claimPaymentUnknown.Store(false)
	f.sample(t, monitorServiceHooks{})
	known := f.source.state(t)
	if len(known.Receipts) != 1 || known.Receipts[0].Observation.PaymentStatus != "deferred" || known.Receipts[0].Observation.UnpaidCreditRao != "12" || known.Receipts[0].Observation.TransactionHash != unknown.Receipts[0].Observation.TransactionHash {
		t.Fatal("same original receipt did not retain a known payment refinement", known.Receipts)
	}
	f.reset(t)
	_, args = f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal("known receipt archive", code, issue)
	}
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if len(state.Receipts) != 0 || state.Archive.Counts.Receipts != 1 || summary.MatchedReceipts != 1 || summary.TargetMet != nil {
		t.Fatal("known original receipt lost archive identity or invented conformance", summary, state.Receipts)
	}
}

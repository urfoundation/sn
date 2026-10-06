//go:build linux

package main

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func publishMonitorEvmArchiveTestRecord(t *testing.T, fixture *monitorEvmArchiveFixture, record monitorEconomicEvmCheckpoint) {
	t.Helper()
	raw, err := encodeMonitorEvmCheckpoint(record)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := openMonitorHistorySnapshot(fixture.evm.ctx, fixture.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(owner.publish(raw, nil), owner.close()); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorEvmArchiveRetainsResourceReviewsAcrossLargerReadBudget(t *testing.T) {
	f := newMonitorEvmArchiveFixture(t, false)
	original := f.evm.record(t)
	_, args := f.plan(t)
	f.apply(t, args)
	prior := f.evm.policy.resources()
	f.evm.policy.ReadBudgetSeconds = 600
	f.evm.policy.HistoryEntries = 10
	f.evm.policy.ResourceRevision = &monitorEvmResourceRevision{Original: prior, ReviewSha256: "sha256:" + strings.Repeat("6", 64)}
	f.evm.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{f.evm.policy}
	f.evm.services.writePolicy(t)
	run := f.evm.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 13 || event.State.ObservedFeeCostWei == nil || *event.State.ObservedFeeCostWei != "84000" || event.State.ResourceReviewHistory.Entries != 2 {
		t.Fatal("larger read budget reset the original archive or review history", event)
	}
	run.stop(t)
	priorRecord := f.evm.record(t)
	if !monitorEvmReviewsRetain(priorRecord.ResourceHistory, original.ResourceHistory) {
		t.Fatal("compatible growth discarded the original resource review")
	}
	f.archive = filepath.Join(filepath.Dir(f.checkpoint), "evm-review-archive-002.json")
	f.resetRequest(t)
	_, args = f.plan(t)
	f.apply(t, args)
	compacted := f.evm.record(t)
	if !reflect.DeepEqual(compacted.ResourceHistory, priorRecord.ResourceHistory) || compacted.ResourceReviewSha256 != priorRecord.ResourceReviewSha256 || !reflect.DeepEqual(compacted.Resources, priorRecord.Resources) {
		t.Fatal("second rollover rewrote acknowledged resource provenance")
	}
	run = f.evm.start(t, monitorServiceHooks{})
	event = run.next(t)
	if !event.Current || event.State.ArchiveSegments != 2 || event.State.ResourceReviewHistory.Entries != 2 || event.State.BatchCount != 2 {
		t.Fatal("renewed-policy restart lost retained original progress", event)
	}
	run.stop(t)
}

func TestMonitorEvmArchiveImportsLegacyLatestReviewUsingOriginalChecksum(t *testing.T) {
	f := newMonitorEvmArchiveFixture(t, false)
	legacy := f.evm.record(t)
	// Exact original latest-only field grammar: new optional fields are absent.
	// This is a synthetic checkpoint, not claimed to come from an old binary.
	legacy.ResourceHistory, legacy.State.Retained = nil, nil
	legacy.ContentHash = legacy.hash()
	publishMonitorEvmArchiveTestRecord(t, f, legacy)
	f.resetRequest(t)
	_, args := f.plan(t)
	f.apply(t, args)
	compacted := f.evm.record(t)
	if compacted.ResourceHistory == nil || compacted.ResourceHistory.LegacyCheckpointSha256 != legacy.ContentHash || len(compacted.ResourceHistory.Entries) != 1 || compacted.State.Archive == nil || compacted.State.Archive.Segments[0].Sha256 != f.request.Original.Sha256 {
		t.Fatal("archive replaced original latest-only review provenance", compacted)
	}
	run := f.evm.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || !event.State.ResourceReviewHistory.LegacyLatestOnly || event.State.ObservedFeeCostWei == nil || *event.State.ObservedFeeCostWei != "84000" {
		t.Fatal("legacy archive restart lost progress or fabricated complete review history", event)
	}
	run.stop(t)
}

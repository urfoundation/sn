//go:build linux

// Public preparation and the default monitor opener share the original physical
// profile. Larger borrowed reads cannot enroll or replace that owner.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Reopening retains the original physical head, finality and outage clocks.
func TestMonitorCheckpointDefaultProfileReopensPreparedOriginal(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	owner := storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "checkpoint.json", 1024*1024)
	storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{owner})
	ctx := storagePreparationApplyOwnerCommand(t, f, "storage-prepare")
	path := filepath.Join(f.root, "checkpoint.json")
	store, err := openMonitorCheckpoint(path, monitorTestExpectation(), ctx)
	if err != nil {
		t.Fatal("default monitor cannot admit the publicly prepared one-MiB owner", err)
	}
	defer store.close()
	if store.directory.head == nil {
		t.Fatal("default checkpoint bypassed original physical custody")
	}
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	want := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: base,
		lastSuccessAt: base.Add(time.Second), unavailableSince: base.Add(2 * time.Second)}
	if err := store.save(want); err != nil {
		t.Fatal(err)
	}
	if duplicate, err := openMonitorCheckpoint(path, monitorTestExpectation(), ctx); err == nil {
		duplicate.close()
		t.Fatal("second default monitor acquired the retained checkpoint owner")
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	original := mainnetNamespaceTest(t, f.root)
	reopened, err := openMonitorCheckpoint(path, monitorTestExpectation(), ctx)
	if err != nil {
		t.Fatal("default monitor cannot reopen its exact original physical profile", err)
	}
	defer reopened.close()
	got, err := reopened.load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("checkpoint restart lost original finality or outage clocks", got, err)
	}
	if err := reopened.close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, mainnetNamespaceTest(t, f.root)) {
		t.Fatal("checkpoint reopen changed original inodes, bytes or custody attributes")
	}
}

// The actual publication boundary accepts one MiB and preserves that original
// when a borrowed-read size or another role's profile is requested.
func TestMonitorCheckpointDefaultProfileKeepsPhysicalByteLimit(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	const maximum = 1024 * 1024
	owner := storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "checkpoint.json", maximum)
	storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{owner})
	ctx := storagePreparationApplyOwnerCommand(t, f, "storage-prepare")
	path := filepath.Join(f.root, "checkpoint.json")
	store, err := openMonitorCheckpoint(path, monitorTestExpectation(), ctx)
	if err != nil {
		t.Fatal("default monitor cannot admit the publicly prepared one-MiB owner", err)
	}
	defer store.close()
	want := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	if err := store.save(want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) >= maximum {
		t.Fatal("synthetic checkpoint cannot exercise the physical byte boundary", err)
	}
	// JSON whitespace preserves the same semantic record at the exact byte cap.
	atLimit := append(raw, bytes.Repeat([]byte{' '}, maximum-len(raw))...)
	if err := store.directory.publish(filepath.Base(path), atLimit, 0600, nil); err != nil {
		t.Fatal("original physical owner refused its exact approved byte limit", err)
	}
	original := mainnetNamespaceTest(t, f.root)
	for _, size := range []int{maximum + 1, maxMonitorCheckpointBytes} {
		oversized := append(append([]byte(nil), atLimit...), bytes.Repeat([]byte{' '}, size-maximum)...)
		if err := store.directory.publish(filepath.Base(path), oversized, 0600, nil); err == nil {
			t.Fatalf("physical checkpoint owner accepted %d bytes beyond its one-MiB authority", size)
		}
		if !reflect.DeepEqual(original, mainnetNamespaceTest(t, f.root)) {
			t.Fatal("over-profile publication replaced original checkpoint custody", size)
		}
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []struct {
		kind    string
		maximum int
	}{
		{kind: "mainnet-monitor-checkpoint", maximum: maxMonitorCheckpointBytes},
		{kind: economicConservationStorageKind, maximum: economicConservationStorageMaximum},
	} {
		if wrong, err := openMonitorCheckpointProfile(path, monitorTestExpectation(), profile.kind, profile.maximum, ctx); err == nil {
			wrong.close()
			t.Fatal("another physical profile acquired the original monitor checkpoint", profile.kind, profile.maximum)
		}
		if !reflect.DeepEqual(original, mainnetNamespaceTest(t, f.root)) {
			t.Fatal("refused physical profile changed original checkpoint custody", profile.kind, profile.maximum)
		}
	}
	reopened, err := openMonitorCheckpoint(path, monitorTestExpectation(), ctx)
	if err != nil {
		t.Fatal("refused enlargement prevented original checkpoint continuation", err)
	}
	defer reopened.close()
	got, err := reopened.load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("exact one-MiB checkpoint failed semantic recovery", got, err)
	}
	if !reflect.DeepEqual(original, mainnetNamespaceTest(t, f.root)) {
		t.Fatal("reading the original byte limit changed checkpoint custody")
	}
}

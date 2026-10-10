// Checkpoint admission uses its own producer ceiling while generic records and
// loaded process identities retain their original smaller and physical scopes.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The actual protected file primitive must reach a valid checkpoint-sized file
// while the unchanged record profile refuses that same finite byte request.
func TestRepairRootCheckpointUsesExplicitProducerProfile(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "checkpoint.json")
	raw := bytes.Repeat([]byte{'x'}, 128*1024+1)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	host := &repairValidatorHost{rootUid: uint32(os.Geteuid()), rootGid: uint32(os.Getegid()), trustRoot: directory}
	got, err := host.readRootCheckpoint(t.Context(), path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("root producer profile rejected retained bytes", err)
	}
	if _, err := host.read(t.Context(), path, host.rootUid, maxMonitorCheckpointBytes, true); err == nil {
		t.Fatal("generic record acquired checkpoint allowance")
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, maxMonitorCheckpointBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.readRootCheckpoint(t.Context(), path); err == nil {
		t.Fatal("root checkpoint ceiling expanded")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := host.readRootCheckpoint(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal("checkpoint lost cancellation", err)
	}
}

// Controller-shaped loading must use the physical test host's explicit owner;
// production construction fixes both ids to root and exposes no user selector.
func TestRepairRootLoadedEnvelopeRetainsPhysicalOwnerAndClosure(t *testing.T) {
	f := newRepairRootPassiveFixture(t)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadRepairRootPassiveEnvelope(f.ctx(), raw, f.key, f.root.host.files.host)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.profile().Unit.Uid != f.envelope.profile().Unit.Uid || loaded.profile().Unit.Gid != f.envelope.profile().Unit.Gid {
		t.Fatal("loaded root owner differs")
	}
	if err := loaded.ready(f.ctx(), f.root.host.files.host, f.root.now); err != nil {
		t.Fatal("loaded actual root readiness failed", err)
	}
	want := map[string]bool{loaded.approval.Plan.OriginalApproval.Path: false, loaded.approval.Plan.OriginalJournal.Path: false, loaded.approval.Plan.OriginalCheckpoint.Path: false}
	for _, path := range loaded.protectedPaths() {
		if _, ok := want[path]; ok {
			want[path] = true
		}
	}
	for path, present := range want {
		if !present {
			t.Fatal("controller closure lost original", path)
		}
	}
}

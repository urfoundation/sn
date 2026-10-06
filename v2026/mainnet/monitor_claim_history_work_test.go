//go:build linux

// Work counters sit at actual decoding, hash and epoch-visit call sites. They
// never supply evidence or replace a guard. Disk reads and public durable
// samples create the admitted history before any measurements are collected.
package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Each new source publication changes a real typed observation. The original
// first proof remains retained while the public archive command moves bytes.
func newMonitorClaimHistoryWorkFixture(t *testing.T, epochs, segments int) *monitorClaimArchiveFixture {
	t.Helper()
	if segments < 1 || segments > 32 {
		t.Fatal("work probe segment profile is out of bounds")
	}
	f := newMonitorClaimArchiveCensusFixture(t, nil, epochs)
	for index := range segments {
		if index != 0 {
			f.advance(func(value *protocol.ClaimProgress) {
				value.Entries[0].Observation.BlockNumber++
				value.Entries[0].Observation.BlockHash = fmt.Sprintf("0x%064x", index+200)
				value.Entries[0].Observation.ObservedAt = value.PublishedAt
			})
			run := f.start(t, monitorServiceHooks{})
			if event := run.next(t); !event.Current || event.State.MerkleProofs != epochs-1 || event.State.Deferred != 1 {
				t.Fatal("real history seed lost its original proof or deferred payment", index, event)
			}
			run.stop(t, 0)
		}
		f.archive = filepath.Join(filepath.Dir(f.checkpoint), fmt.Sprintf("claim-history-%03d.json", index))
		f.resetRequest(t)
		_, args := f.plan(t)
		f.apply(t, args)
	}
	return f
}

// Full admission is proportional to actual original pages and their payload
// census. Hot saves do no full-history replays. Policy/state validation work is
// reported separately rather than being hidden behind the no-reread claim.
func TestMonitorClaimFullHistoryCountsAdmissionAndHotSaveWork(t *testing.T) {
	for _, profile := range []struct{ epochs, segments int }{{epochs: 8, segments: 2}, {epochs: 16, segments: 4}, {epochs: 32, segments: 8}} {
		func() {
			f := newMonitorClaimHistoryWorkFixture(t, profile.epochs, profile.segments)
			work := map[string]uint64{}
			policy := f.policy
			policy.work = func(stage string, units uint64) { work[stage] += units }
			worker, err := openMonitorClaimWorker(f.ctx, policy, monitorTestExpectation(), f.services.checkpointPath, f.services.metricsPath, monitorServiceHooks{})
			if err != nil || worker == nil {
				t.Fatal("actual full Claim history did not admit", profile, err)
			}
			defer func() {
				if err := worker.close(monitorServiceHooks{}); err != nil {
					t.Error(err)
				}
			}()
			if work["archive-read"] != uint64(profile.segments) || work["archive-decode"] != uint64(profile.segments) || work["compact-epoch"] != uint64(profile.epochs*profile.segments) || work["hydrate-epoch"] != uint64(profile.epochs*(profile.segments+1)) || work["archive-read-bytes"] == 0 {
				t.Fatal("complete original history was skipped or repeatedly admitted", profile, work)
			}
			t.Logf("actual admission: epochs=%d segments=%d work=%v", profile.epochs, profile.segments, work)
			before := rootObjectHash(worker.state.Epochs)
			for range 3 {
				clear(work)
				if err := worker.save(); err != nil {
					t.Fatal("owned Claim hot save failed", err)
				}
				if work["archive-read"] != 0 || work["archive-decode"] != 0 || work["hydrate-epoch"] != 0 || work["compact-epoch"] != 0 || work["validated-state-epoch"] != uint64(profile.epochs) || work["policy-hash"] == 0 || work["policy-hashed-epoch"] < uint64(profile.epochs) {
					t.Fatal("Claim hot save changed its actual full-history or policy/state work", profile, work)
				}
				if rootObjectHash(worker.state.Epochs) != before {
					t.Fatal("work probe changed original evidence")
				}
				t.Logf("actual hot save: epochs=%d segments=%d work=%v", profile.epochs, profile.segments, work)
			}
		}()
	}
}

// The first real original is read and decoded before cancellation. Reopening
// must authenticate the full original prefix again; no partial result escapes.
func TestMonitorClaimFullHistoryCancellationDiscardsPartialAdmission(t *testing.T) {
	f := newMonitorClaimHistoryWorkFixture(t, 8, 4)
	record := f.record(t)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	work := map[string]uint64{}
	policy := f.policy
	policy.work = func(stage string, units uint64) {
		work[stage] += units
		if stage == "archive-decode" {
			cancel()
		}
	}
	owner, err := openMonitorClaimArchive(ctx, policy, record)
	if owner != nil {
		_ = owner.close()
	}
	if owner != nil || !errors.Is(err, context.Canceled) || work["archive-read"] != 1 || work["archive-decode"] != 1 || work["hydrate-epoch"] != 0 {
		t.Fatal("canceled partial original history was published or over-read", err, work)
	}
	clear(work)
	policy.work = func(stage string, units uint64) { work[stage] += units }
	owner, err = openMonitorClaimArchive(f.ctx, policy, record)
	if err != nil || owner == nil {
		t.Fatal("fresh Claim owner could not replay the complete original history", err)
	}
	defer func() {
		if err := owner.close(); err != nil {
			t.Error(err)
		}
	}()
	if work["archive-read"] != 4 || work["archive-decode"] != 4 || !reflect.DeepEqual(record, f.record(t)) {
		t.Fatal("fresh admission inherited partial cache or rewrote original head", work)
	}
}

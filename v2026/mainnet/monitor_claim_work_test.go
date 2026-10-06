//go:build linux

package main

import (
	"errors"
	"reflect"
	"testing"
)

// Public HTTP admission and archive plan/apply establish the real original
// state. The counter observes the actual save path's lookup primitive; it
// cannot return a hash, alter a guard or skip the durable publication.
func TestMonitorClaimArchivedSaveWorkGrowsWithEpochCensus(t *testing.T) {
	for _, census := range []int{8, 16, 32, 64} {
		func() {
			f := newMonitorClaimArchiveCensusFixture(t, nil, census)
			_, args := f.plan(t)
			f.apply(t, args)
			worker, err := openMonitorClaimWorker(f.ctx, f.policy, monitorTestExpectation(), f.services.checkpointPath, f.services.metricsPath, monitorServiceHooks{})
			if err != nil || worker == nil {
				t.Fatal("actual growing archive could not reopen", err)
			}
			t.Cleanup(func() {
				if err := worker.close(monitorServiceHooks{}); err != nil {
					t.Error(err)
				}
			})
			index := worker.archiveAdmission.commitments
			if index == nil || len(index.hashes) != census || len(index.ordered) != census {
				t.Fatal("archive admission lost the complete commitment census")
			}
			visits := 0
			index.visit = func() { visits++ }
			before := rootObjectHash(worker.state.Epochs)
			for range 3 {
				visits = 0
				if err := worker.save(); err != nil {
					t.Fatal("actual durable save failed", err)
				}
				if visits != census || worker.archiveAdmission.commitments != index {
					t.Fatal("archived save repeated census search instead of one admitted lookup per epoch", census, visits)
				}
				record := f.record(t)
				if rootObjectHash(worker.state.Epochs) != before || len(record.State.Epochs) != census {
					t.Fatal("work reduction changed live evidence or expected epoch census")
				}
				for _, epoch := range record.State.Epochs {
					if !monitorClaimEpochPlaceholder(epoch) {
						t.Fatal("real save did not retain the original archive commitment", epoch.Epoch)
					}
				}
			}
			// An admitted lookup caches only the immutable original hash. A
			// later observation still hashes its actual bytes on every save.
			worker.state.Epochs[0] = cloneMonitorClaimEpoch(worker.state.Epochs[0])
			*worker.state.Epochs[0].Observation.LeafClaimed = true
			visits = 0
			if err := worker.save(); err != nil {
				t.Fatal("changed observation could not publish", err)
			}
			stored := f.record(t).State.Epochs[0]
			if visits != census || stored.Archived || stored.Observation == nil || !*stored.Observation.LeafClaimed || stored.Proof == nil || *stored.Proof.LeafClaimed {
				t.Fatal("commitment lookup reused stale evidence or lost the first proof", visits, stored)
			}
		}()
	}
}

// Building an index must not turn duplicate or reordered commitments into a
// permissible map overwrite. Refusal precedes source sampling and head writes.
func TestMonitorClaimEpochIndexRetainsOrderedDuplicateAdmission(t *testing.T) {
	for _, fault := range []string{"duplicate", "reordered"} {
		func() {
			f := newMonitorClaimArchiveCensusFixture(t, nil, 8)
			_, args := f.plan(t)
			f.apply(t, args)
			candidate := f.record(t)
			if fault == "duplicate" {
				candidate.Archive.Epochs[1] = candidate.Archive.Epochs[0]
			} else {
				candidate.Archive.Epochs[0], candidate.Archive.Epochs[1] = candidate.Archive.Epochs[1], candidate.Archive.Epochs[0]
			}
			raw, err := encodeMonitorClaimCheckpoint(candidate)
			if err != nil {
				t.Fatal(err)
			}
			writer, err := openMonitorHistorySnapshot(f.ctx, f.checkpoint, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(writer.publish(raw, nil), writer.close()); err != nil {
				t.Fatal(err)
			}
			before, requests := f.record(t), f.requests.Load()
			worker, err := openMonitorClaimWorker(f.ctx, f.policy, monitorTestExpectation(), f.services.checkpointPath, f.services.metricsPath, monitorServiceHooks{})
			if worker != nil {
				_ = worker.close(monitorServiceHooks{})
			}
			if err == nil || worker != nil || f.requests.Load() != requests || !reflect.DeepEqual(before, f.record(t)) {
				t.Fatal("index construction admitted or rewrote a changed ordered commitment census", fault, err)
			}
		}()
	}
}

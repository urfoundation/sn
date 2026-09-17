//go:build linux || darwin

// Proof JSONL is a bounded derived view of the authenticated disk ledger.
// Prepare the entire view census after semantic recovery and before any trail
// worker starts; no operator receives a partially prepared ProofStore batch.
package validator

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
)

// Observers see actual construction and the final publication boundary. They
// may refuse work, not replace the production reconciler or grant authority.
type releaseEvidenceV2ProofObserver struct {
	opened        func(uint64, *ProofStore) error
	beforePublish func() error
}

// The startup owner must exclusively own disk and its participants until this
// returns. Historical/current-cursor authentication and Initialize/Recover must
// already have completed; attached Stats are not themselves proof of history.
func prepareReleaseEvidenceV2ProofStores(ctx context.Context, disk *releaseEvidenceV2DiskState) error {
	return prepareReleaseEvidenceV2ProofStoresObserved(ctx, disk, releaseEvidenceV2ProofObserver{})
}

// All acquisition fields and destinations are owned before callbacks. Each
// real projection uses its existing record/count/byte limits and cancellable
// disk reader; independent operators run up to the effective core count.
func prepareReleaseEvidenceV2ProofStoresObserved(ctx context.Context, disk *releaseEvidenceV2DiskState, observer releaseEvidenceV2ProofObserver) error {
	if ctx == nil || disk == nil || len(disk.participants) == 0 || len(disk.states) != len(disk.participants) {
		return errors.New("evidence proof startup census is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	type acquisition struct {
		participant AttemptSettlementRuntimeV2Participant
		state       *releaseAttemptState
	}
	plan := make([]acquisition, len(disk.participants))
	seen := map[uint64]bool{}
	states := map[*releaseAttemptState]bool{}
	ledgers := map[*AttemptLedger]bool{}
	engines := map[*StatsEngine]bool{}
	paths := make([]string, len(plan))
	for index, participant := range disk.participants {
		state := disk.states[participant.NoID]
		if participant.NoID == 0 || seen[participant.NoID] || state == nil || states[state] || state.store != nil ||
			participant.Ledger == nil || participant.Ledger.disk == nil || ledgers[participant.Ledger] ||
			participant.Stats == nil || engines[participant.Stats] || state.ledger != participant.Ledger || state.stats != participant.Stats ||
			participant.Ledger.identity.NoID != participant.NoID || participant.StateDir != filepath.Dir(participant.Ledger.path) {
			return errors.New("evidence proof startup has duplicate, competing or unowned participants")
		}
		seen[participant.NoID], states[state], ledgers[participant.Ledger], engines[participant.Stats] = true, true, true, true
		plan[index], paths[index] = acquisition{participant: participant, state: state}, participant.StateDir
	}
	if err := validateAttemptSettlementV2Paths(paths); err != nil {
		return err
	}
	// This checks lifecycle readiness only. Independent activation/cursor
	// replay remains mandatory at the outer startup entry, not a callback.
	check := func() error {
		if len(disk.states) != len(plan) || len(disk.participants) != len(plan) {
			return errors.New("evidence proof startup census changed during preparation")
		}
		for index, item := range plan {
			participant := item.participant
			if disk.states[participant.NoID] != item.state || disk.participants[index] != participant || item.state.store != nil || item.state.ledger != participant.Ledger || item.state.stats != participant.Stats {
				return errors.New("evidence proof startup acquisition changed during preparation")
			}
			ready := func() bool {
				stats := participant.Stats
				stats.mu.Lock()
				defer stats.mu.Unlock()
				return stats.attemptLedger == participant.Ledger && stats.attemptV2 != nil && stats.settlementEpochKnown && stats.activeAttemptCount == 0 && !stats.attemptCutPending && !stats.attemptSettlementCutPending
			}()
			if !ready {
				return errors.New("evidence proof startup requires recovered V2 Stats without active workers")
			}
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return err
	}
	ownedCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stores := make([]*ProofStore, len(plan))
	failures := make([]error, len(plan))
	completed := make([]bool, len(plan))
	var next atomic.Uint64
	var joined sync.WaitGroup
	for worker := 0; worker < min(len(plan), runtime.GOMAXPROCS(0)); worker++ {
		joined.Add(1)
		go func() {
			defer joined.Done()
			for {
				index := next.Add(1) - 1
				if index >= uint64(len(plan)) || ownedCtx.Err() != nil {
					return
				}
				item := plan[index]
				outcome := errors.New("proof projection did not complete")
				func() {
					defer func() {
						if err := errors.Join(outcome, ownedCtx.Err()); err != nil {
							failures[index] = fmt.Errorf("evidence proof no_id %d: %w", item.participant.NoID, err)
							cancel()
						}
					}()
					store, err := NewProofStore(item.participant.StateDir)
					if err != nil {
						outcome = err
						return
					}
					if observer.opened != nil {
						if err := observer.opened(item.participant.NoID, store); err != nil {
							outcome = err
							return
						}
					}
					if err := store.ReconcileAttemptProofsContext(ownedCtx, item.participant.Ledger); err != nil {
						outcome = err
						return
					}
					stores[index], completed[index], outcome = store, true, nil
				}()
			}
		}()
	}
	joined.Wait()
	for index, item := range plan {
		if !completed[index] && failures[index] == nil {
			failures[index] = fmt.Errorf("evidence proof no_id %d preparation was not completed", item.participant.NoID)
		}
	}
	if err := errors.Join(append(failures, ownedCtx.Err())...); err != nil {
		return err
	}
	if observer.beforePublish != nil {
		if err := observer.beforePublish(); err != nil {
			return err
		}
	}
	if err := check(); err != nil {
		return err
	}
	// No callback or fallible operation can split the root-owned pointer batch.
	// On earlier failures any actual derived files remain for verified retry.
	for index, item := range plan {
		item.state.store = stores[index]
	}
	return nil
}

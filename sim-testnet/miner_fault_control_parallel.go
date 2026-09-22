// Bounded control rounds let independent swarm members progress in parallel.
// One coordinator owns all durable writes and joins every dispatched request.
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"
)

const (
	minerControlProgressSchema = "urnetwork-sim-active-faults-v3"
	minerControlRoundTimeout   = 10 * time.Second
	minerControlParallelLimit  = 16
	minerControlSwarmLimit     = 4
)

// Pending work has durable exact intent and must be reconciled on the next
// heartbeat. Only transport failures can acquire this nonterminal type.
type minerControlPendingError struct{ cause error }

// Retain the transport cause for final diagnostics without certifying success.
func (self *minerControlPendingError) Error() string {
	return fmt.Sprintf("miner control remains pending: %v", self.cause)
}

// Preserve error identity for callers that distinguish cancellation.
func (self *minerControlPendingError) Unwrap() error { return self.cause }

// Match the whole owner error; an unrelated joined failure cannot inherit it.
func minerControlPending(err error) bool {
	_, ok := err.(*minerControlPendingError)
	return ok
}

// Round cancellation may accompany sibling transport failures, but a joined
// integrity/status error must never acquire nonterminal retry authority.
func minerControlRoundRetryable(err error, canceled bool) bool {
	if minerControlTransientError(err) {
		return true
	}
	var invalid *minerControlInvalidStatusError
	if !canceled || errors.As(err, &invalid) {
		return false
	}
	if err == context.Canceled {
		return true
	}
	switch err := err.(type) {
	case interface{ Unwrap() []error }:
		causes := err.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !minerControlRoundRetryable(cause, canceled) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return minerControlRoundRetryable(err.Unwrap(), canceled)
	default:
		return false
	}
}

// Preserve the complete legacy singleton before adding any parallel target.
func upgradeMinerControlProgress(active *activeFaultFile) {
	if active.Schema != minerControlProgressSchema {
		for index := range active.MinerControls {
			if pending := active.MinerControls[index].Pending; pending != "" {
				active.MinerControls[index].PendingTargets = []string{pending}
			}
		}
	}
	active.Schema = minerControlProgressSchema
}

// The completion read happens after actual control reconciliation, before the
// driver releases durable recovery intent. It cannot backdate a fault window.
type minerControlCompletedTransition struct {
	faultId string
	action  string
	head    ChainHead
}

// Workers request persistence through this channel and wait for its result.
// The coordinator stays alive through every result, including cancellation.
type minerControlProgressRequest struct {
	process FaultProcessEvidence
	before  bool
	failure error
	reply   chan error
}

// One dispatch has one final result after all its progress requests complete.
type minerControlRoundResult struct {
	process FaultProcessEvidence
	swarm   int
	err     error
}

// This driver-local cache is only a cursor through one unfinished transition.
// Disk completion alone cannot populate it; every entry follows a live response
// and successful durable write. Worker generations and opposite actions differ.
type minerControlGeneration struct {
	process   FaultProcessEvidence
	startedAt string
	restarts  int
}

// Snapshot the owning generation on every round. A reopened driver has no
// cache and must reconcile its retained disk census incrementally again.
func (self *liveScenarioFaultDriver) minerControlGenerations(processes []FaultProcessEvidence) (map[string]minerControlGeneration, error) {
	states, _, err := self.processSnapshot()
	if err != nil {
		return nil, err
	}
	processGenerationKVs := make(map[string]minerControlGeneration, len(processes))
	for _, process := range processes {
		var miner int
		_, _ = fmt.Sscanf(process.ID, "miner-%d", &miner)
		swarm, err := minerSwarmFor(self.cfg, miner)
		if err != nil {
			return nil, err
		}
		state, ok := states[fmt.Sprintf("miner-swarm-%d", swarm)]
		if !ok || state.PID != process.PID || state.Identity != process.Identity {
			return nil, fmt.Errorf("miner control owner changed before reconciliation for %s", process.ID)
		}
		processGenerationKVs[process.ID] = minerControlGeneration{process: process, startedAt: state.StartedAt, restarts: state.Restarts}
	}
	return processGenerationKVs, nil
}

// Pre-acceptance and cleanup callers own bounded waits rather than a live
// heartbeat. They retry only durable pending work under their original ctx.
func waitMinerFaultTransition(ctx context.Context, transition func() ([]FaultProcessEvidence, error)) ([]FaultProcessEvidence, error) {
	for {
		processes, err := transition()
		if !minerControlPending(err) {
			return processes, err
		}
		if err := waitSupervisorRestart(ctx, minerControlRetryDelay); err != nil {
			return processes, err
		}
	}
}

// Existing/prearmed transitions may predate the current observation. New
// control completion can only move the observed timestamp forward.
func minerFaultCompletedHead(driver scenarioFaultDriver, spec scenarioFaultSpec, action string, observed ChainHead) (ChainHead, error) {
	if live, ok := driver.(*liveScenarioFaultDriver); ok && spec.Kind == "miner-control" {
		completed := live.minerControlCompleted
		if completed.faultId == spec.ID && completed.action == action {
			if completed.head.Number == observed.Number && completed.head.Hash != observed.Hash {
				return ChainHead{}, errors.New("miner control completion substituted the observed finalized block")
			}
			if completed.head.Number > observed.Number {
				return completed.head, nil
			}
		}
	}
	return observed, nil
}

// Workers never acquire a state lock around requests or durable writes.
// Already completed members are freshly reconciled; they do not get another
// mutation just because a sibling timed out or the driver was replaced.
func (self *liveScenarioFaultDriver) controlMinerRound(ctx context.Context, processes []FaultProcessEvidence, action string, progress *minerFaultControlProgress, persist func() error) error {
	roundCtx, cancel := context.WithTimeout(ctx, minerControlRoundTimeout)
	if self.minerControlRoundContext != nil {
		cancel()
		roundCtx, cancel = self.minerControlRoundContext(ctx)
	}
	defer cancel()
	parallel := minerControlParallelLimit
	if self.minerControlParallel > 0 {
		parallel = min(parallel, self.minerControlParallel)
	}
	processGenerationKVs, err := self.minerControlGenerations(processes)
	if err != nil {
		return err
	}
	transitionKey := progress.FaultHash + ":" + action
	if self.minerControlReconciled == nil {
		self.minerControlReconciled = map[string]map[string]minerControlGeneration{}
	}
	reconciled := self.minerControlReconciled[transitionKey]
	if reconciled == nil {
		reconciled = map[string]minerControlGeneration{}
		self.minerControlReconciled[transitionKey] = reconciled
	}
	remaining := make([]FaultProcessEvidence, 0, len(processes))
	for _, process := range processes {
		if reconciled[process.ID] != processGenerationKVs[process.ID] || !slices.Contains(progress.Completed, process) {
			delete(reconciled, process.ID)
			remaining = append(remaining, process)
		}
	}
	// Resolve ambiguous pending operations before starting another target.
	sort.SliceStable(remaining, func(i, j int) bool {
		return slices.Contains(progress.PendingTargets, remaining[i].ID) && !slices.Contains(progress.PendingTargets, remaining[j].ID)
	})
	requests := make(chan minerControlProgressRequest)
	results := make(chan minerControlRoundResult, parallel)
	swarmCounts := map[int]int{}
	running := 0
	var failures []error
	var hardFailure error
	projectPending := func() {
		sort.Strings(progress.PendingTargets)
		progress.Pending = ""
		if len(progress.PendingTargets) != 0 {
			progress.Pending = progress.PendingTargets[0]
		}
	}
	recordFailure := func(target string, failure error) error {
		progress.LastError = fmt.Sprintf("%s: %v", target, failure)
		if len(progress.LastError) > minerControlMaximumError {
			progress.LastError = progress.LastError[:minerControlMaximumError]
		}
		return persist()
	}
	for len(remaining) != 0 || running != 0 {
		for roundCtx.Err() == nil && hardFailure == nil && running < parallel {
			selected, swarm := -1, 0
			for index, process := range remaining {
				var miner int
				_, _ = fmt.Sscanf(process.ID, "miner-%d", &miner)
				candidate, _ := minerSwarmFor(self.cfg, miner)
				if swarmCounts[candidate] < minerControlSwarmLimit {
					selected, swarm = index, candidate
					break
				}
			}
			if selected < 0 {
				break
			}
			process := remaining[selected]
			remaining = slices.Delete(remaining, selected, selected+1)
			progress.Completed = slices.DeleteFunc(progress.Completed, func(value FaultProcessEvidence) bool { return value.ID == process.ID })
			if !slices.Contains(progress.PendingTargets, process.ID) {
				progress.PendingTargets = append(progress.PendingTargets, process.ID)
			}
			projectPending()
			if err := persist(); err != nil {
				hardFailure = err
				cancel()
				break
			}
			running++
			swarmCounts[swarm]++
			go func() {
				result := minerControlRoundResult{process: process, swarm: swarm, err: errors.New("miner control worker ended before reconciliation")}
				defer func() { results <- result }()
				requestProgress := func(before bool, failure error) error {
					reply := make(chan error, 1)
					requests <- minerControlProgressRequest{process: process, before: before, failure: failure, reply: reply}
					return <-reply
				}
				result.err = self.controlMiner(roundCtx, swarm, process.ID, action,
					func() error { return requestProgress(true, nil) },
					func(failure error) error { return requestProgress(false, failure) })
			}()
		}
		if running == 0 {
			break
		}
		select {
		case request := <-requests:
			var err error
			if request.before {
				if progress.Attempts == math.MaxUint64 {
					err = errors.New("miner control attempt counter exhausted")
				} else {
					progress.Attempts++
					err = persist()
				}
			} else {
				err = recordFailure(request.process.ID, request.failure)
			}
			if err != nil {
				hardFailure = errors.Join(hardFailure, err)
				cancel()
			}
			request.reply <- err
		case result := <-results:
			running--
			swarmCounts[result.swarm]--
			if result.err != nil {
				failure := fmt.Errorf("%s %s: %w", action, result.process.ID, result.err)
				failures = append(failures, failure)
				if !minerControlRoundRetryable(result.err, roundCtx.Err() != nil) {
					hardFailure = errors.Join(hardFailure, failure)
					cancel()
				}
				if err := recordFailure(result.process.ID, result.err); err != nil {
					hardFailure = errors.Join(hardFailure, err)
					cancel()
				}
			} else {
				progress.Completed = append(progress.Completed, result.process)
				sort.Slice(progress.Completed, func(i, j int) bool { return progress.Completed[i].ID < progress.Completed[j].ID })
				progress.PendingTargets = slices.DeleteFunc(progress.PendingTargets, func(target string) bool { return target == result.process.ID })
				projectPending()
				if len(progress.PendingTargets) == 0 {
					progress.LastError = ""
				}
				if err := persist(); err != nil {
					hardFailure = errors.Join(hardFailure, err)
					cancel()
				} else {
					reconciled[result.process.ID] = processGenerationKVs[result.process.ID]
				}
			}
		}
	}
	if hardFailure != nil {
		delete(self.minerControlReconciled, transitionKey)
		return hardFailure
	}
	if err := ctx.Err(); err != nil {
		delete(self.minerControlReconciled, transitionKey)
		return err
	}
	if len(failures) != 0 || len(remaining) != 0 || len(progress.Completed) != len(processes) {
		return &minerControlPendingError{cause: errors.Join(append(failures, roundCtx.Err())...)}
	}
	return nil
}

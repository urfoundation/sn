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
)

const (
	minerControlProgressSchema = "urnetwork-sim-active-faults-v3"
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
	if err == errMinerControlLifecyclePending {
		return true
	}
	if minerControlTransientError(err) {
		return true
	}
	var invalid *minerControlInvalidStatusError
	if errors.As(err, &invalid) {
		return false
	}
	if canceled && err == context.Canceled {
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
	states, specs, err := self.processSnapshot()
	if err != nil {
		return nil, err
	}
	processGenerationKVs := make(map[string]minerControlGeneration, len(processes))
	var changed []string
	for _, process := range processes {
		var miner int
		_, _ = fmt.Sscanf(process.ID, "miner-%d", &miner)
		swarm, err := minerSwarmFor(self.cfg, miner)
		if err != nil {
			return nil, err
		}
		ownerId := fmt.Sprintf("miner-swarm-%d", swarm)
		state, stateOk := states[ownerId]
		spec, specOk := specs[ownerId]
		if !stateOk || !specOk || spec.Role != "miner-swarm" || spec.Identity == "" || spec.Identity != process.Identity || state.Role != spec.Role || state.Identity != spec.Identity || state.PID < 0 || state.PID == 1 || state.Restarts < 0 {
			return nil, fmt.Errorf("miner control owner identity changed before reconciliation for %s", process.ID)
		}
		if state.PID != process.PID {
			changed = append(changed, process.ID)
		}
		processGenerationKVs[process.ID] = minerControlGeneration{process: process, startedAt: state.StartedAt, restarts: state.Restarts}
	}
	if len(changed) != 0 {
		return nil, &minerControlPendingError{cause: &minerControlOwnerUnavailableError{targets: changed}}
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

// Each wave admits a bounded census in one durable write. Workers rendezvous
// at mutation and result checkpoints, so one slow fsync cannot consume the
// request timeout of a member which has not dispatched yet.
func (self *liveScenarioFaultDriver) controlMinerRound(ctx context.Context, processes []FaultProcessEvidence, action string, progress *minerFaultControlProgress, persist func() error) error {
	processGenerationKVs, err := self.minerControlGenerations(processes)
	if err != nil {
		return err
	}
	roundCtx, cancel := context.WithCancel(ctx)
	if self.minerControlRoundContext != nil {
		cancel()
		roundCtx, cancel = self.minerControlRoundContext(ctx)
	}
	defer cancel()
	parallel := minerControlParallelLimit
	if self.minerControlParallel > 0 {
		parallel = min(parallel, self.minerControlParallel)
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
	// Ambiguous durable calls are reconciled before another target is admitted.
	sort.SliceStable(remaining, func(i, j int) bool {
		return slices.Contains(progress.PendingTargets, remaining[i].ID) && !slices.Contains(progress.PendingTargets, remaining[j].ID)
	})
	projectPending := func() {
		sort.Strings(progress.PendingTargets)
		progress.Pending = ""
		if len(progress.PendingTargets) != 0 {
			progress.Pending = progress.PendingTargets[0]
		}
		if len(progress.PendingTargets) == 0 {
			progress.LastError = ""
		}
	}
	recordFailure := func(target string, failure error) {
		progress.LastError = fmt.Sprintf("%s: %v", target, failure)
		if len(progress.LastError) > minerControlMaximumError {
			progress.LastError = progress.LastError[:minerControlMaximumError]
		}
	}
	var failures []error
	for len(remaining) != 0 && roundCtx.Err() == nil {
		var wave []minerControlRoundResult
		swarmCounts := map[int]int{}
		for index := 0; index < len(remaining) && len(wave) < parallel; {
			process := remaining[index]
			var miner int
			_, _ = fmt.Sscanf(process.ID, "miner-%d", &miner)
			swarm, err := minerSwarmFor(self.cfg, miner)
			if err != nil {
				return err
			}
			if swarmCounts[swarm] >= minerControlSwarmLimit {
				index++
				continue
			}
			wave = append(wave, minerControlRoundResult{process: process, swarm: swarm})
			swarmCounts[swarm]++
			remaining = slices.Delete(remaining, index, index+1)
			progress.Completed = slices.DeleteFunc(progress.Completed, func(value FaultProcessEvidence) bool { return value.ID == process.ID })
			if !slices.Contains(progress.PendingTargets, process.ID) {
				progress.PendingTargets = append(progress.PendingTargets, process.ID)
			}
		}
		projectPending()
		if err := persist(); err != nil {
			return err
		}
		requests := make(chan minerControlProgressRequest, len(wave))
		results := make(chan minerControlRoundResult, len(wave))
		for _, target := range wave {
			go func() {
				result := target
				result.err = errors.New("miner control worker ended before reconciliation")
				defer func() { results <- result }()
				requestProgress := func(before bool, failure error) error {
					reply := make(chan error, 1)
					requests <- minerControlProgressRequest{process: target.process, before: before, failure: failure, reply: reply}
					return <-reply
				}
				result.err = self.controlMiner(roundCtx, target.swarm, target.process.ID, action,
					func() error { return requestProgress(true, nil) },
					func(failure error) error { return requestProgress(false, failure) })
			}()
		}
		running := len(wave)
		var waiting []minerControlProgressRequest
		var completed []FaultProcessEvidence
		var hardFailure, persistenceFailure error
		dirty := false
		for running != 0 || dirty {
			// All workers have either returned or yielded before a request. No
			// HTTP deadline is running while their durable checkpoint is flushed.
			if len(waiting) == running && dirty {
				checkpointErr := persistenceFailure
				if checkpointErr == nil {
					projectPending()
					sort.Slice(progress.Completed, func(i, j int) bool { return progress.Completed[i].ID < progress.Completed[j].ID })
					checkpointErr = persist()
				}
				if checkpointErr != nil {
					persistenceFailure = checkpointErr
					cancel()
				} else {
					for _, process := range completed {
						reconciled[process.ID] = processGenerationKVs[process.ID]
					}
				}
				completed = nil
				dirty = false
				for _, request := range waiting {
					request.reply <- errors.Join(checkpointErr, hardFailure, roundCtx.Err())
				}
				waiting = nil
				if running == 0 {
					break
				}
			}
			select {
			case request := <-requests:
				dirty = true
				if request.before {
					if progress.Attempts == math.MaxUint64 {
						hardFailure = errors.Join(hardFailure, errors.New("miner control attempt counter exhausted"))
						cancel()
					} else {
						progress.Attempts++
					}
				} else {
					recordFailure(request.process.ID, request.failure)
				}
				waiting = append(waiting, request)
			case result := <-results:
				dirty = true
				running--
				if result.err != nil {
					failure := fmt.Errorf("%s %s: %w", action, result.process.ID, result.err)
					failures = append(failures, failure)
					recordFailure(result.process.ID, result.err)
					if !minerControlRoundRetryable(result.err, roundCtx.Err() != nil) && !minerControlStorageRetryable(result.err) && persistenceFailure == nil {
						hardFailure = errors.Join(hardFailure, failure)
						cancel()
					}
				} else {
					progress.Completed = append(progress.Completed, result.process)
					progress.PendingTargets = slices.DeleteFunc(progress.PendingTargets, func(target string) bool { return target == result.process.ID })
					completed = append(completed, result.process)
				}
			}
		}
		if hardFailure != nil {
			delete(self.minerControlReconciled, transitionKey)
			return errors.Join(hardFailure, persistenceFailure)
		}
		if persistenceFailure != nil {
			return persistenceFailure
		}
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

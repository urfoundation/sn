package main

import (
	"context"
	"time"
)

// scenarioFinalizedHeadProbe is deliberately smaller than scenarioProbe. A
// complete observation reads the full contract, fleet, validator and miner
// evidence set and can take longer than a short fault interval. The scheduler
// uses this authenticated finalized checkpoint to keep purely block-timed
// faults moving while that observation is in progress.
type scenarioFinalizedHeadProbe interface {
	FinalizedHead(context.Context) (ChainHead, error)
}

type scenarioSnapshotResult struct {
	observation *ScenarioObservation
	err         error
}

// waitScenarioSnapshot keeps fault timing independent of expensive evidence
// collection. The heartbeat is best-effort: a transient head read failure is
// retried at the next interval, while an actual fault transition failure is
// returned to the caller and retains the normal fail-closed campaign path.
func waitScenarioSnapshot(ctx context.Context, probe scenarioProbe, interval time.Duration, heartbeat func(context.Context, ChainHead) error) (*ScenarioObservation, error) {
	result := make(chan scenarioSnapshotResult, 1)
	snapshotCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		observation, err := probe.Snapshot(snapshotCtx)
		result <- scenarioSnapshotResult{observation: observation, err: err}
	}()
	if heartbeat == nil || interval <= 0 {
		select {
		case value := <-result:
			return value.observation, value.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	headProbe, supported := probe.(scenarioFinalizedHeadProbe)
	if !supported {
		select {
		case value := <-result:
			return value.observation, value.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case value := <-result:
			return value.observation, value.err
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// A head read must never let an unhealthy RPC pin the scheduler.
			budget := interval
			if budget > 10*time.Second {
				budget = 10 * time.Second
			}
			headCtx, headCancel := context.WithTimeout(ctx, budget)
			head, err := headProbe.FinalizedHead(headCtx)
			headCancel()
			if err != nil {
				continue
			}
			if err := heartbeat(ctx, head); err != nil {
				return nil, err
			}
		}
	}
}

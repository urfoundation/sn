// One continuous native producer owns its RPC, capture/replay descendants and
// one bounded result slot. Only the parent publishes combined state; another
// domain's progress cannot be overwritten by this worker's older snapshot.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type economicConservationNativeResult struct {
	nativeHash  string
	policyHash  string
	observation *economicEmissionObservation
	err         error
}

// The parent alone accesses lifecycle fields. The child receives a detached
// native cursor and immutable original approval lineage, never the live state.
type economicConservationNativeWorker struct {
	ctx    context.Context
	policy economicConservationPolicy
	client *rpcClient
	hooks  monitorServiceHooks
	active bool
	cancel context.CancelFunc
	done   chan struct{}
	result chan economicConservationNativeResult
}

func newEconomicConservationNativeWorker(ctx context.Context, policy economicConservationPolicy, client *rpcClient, hooks monitorServiceHooks) *economicConservationNativeWorker {
	return &economicConservationNativeWorker{ctx: ctx, policy: policy, client: client, hooks: hooks, result: make(chan economicConservationNativeResult, 1)}
}

// Each attempt retains the admitted full read budget. The captured predecessor
// also binds pending producer completion, original anchor and renewal chain.
func (self *economicConservationNativeWorker) start(state *economicConservationState) error {
	if self == nil || self.active || self.ctx.Err() != nil || state.NativeHeld {
		return nil
	}
	if err := state.archiveView.requireLive(); err != nil {
		return err
	}
	policy, err := state.operatingPolicy(self.policy)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(state.Native)
	if err != nil {
		return err
	}
	if uint64(len(raw)) > policy.storageMaximum() {
		return errMonitorEconomicCapacity
	}
	var prior monitorEconomicNativeState
	if err := decodePlanJson(raw, &prior); err != nil {
		return err
	}
	// Policy references point only to original immutable inputs. Neither the
	// parent nor the observer changes them during a retained command lifetime.
	result := economicConservationNativeResult{nativeHash: rootObjectHash(prior), policyHash: rootObjectHash(policy.Native)}
	budget := time.Duration(monitorEconomicReadSeconds(policy.ReadBudgetSeconds)) * time.Second
	self.client.retryWindow = budget
	ctx, cancel := context.WithTimeout(self.ctx, budget)
	self.cancel, self.done, self.active = cancel, make(chan struct{}), true
	done := self.done
	go func() {
		defer close(done)
		defer cancel()
		result.observation, result.err = observeMonitorEconomicNative(ctx, self.client, policy.Native, &prior)
		self.result <- result
		if self.hooks.afterConservationNativeRead != nil {
			self.hooks.afterConservationNativeRead(ctx, result.err)
		}
	}()
	return nil
}

// Poll only. A complete unavailable read is a result; an unfinished read is
// pending and cannot clear an earlier issue or invent a fresh native cursor.
func (self *economicConservationNativeWorker) take() (economicConservationNativeResult, bool) {
	if self == nil || !self.active {
		return economicConservationNativeResult{}, false
	}
	select {
	case result := <-self.result:
		<-self.done
		self.active = false
		return result, true
	default:
		return economicConservationNativeResult{}, false
	}
}

// Cancellation joins capture/replay and their owned pipes before outer
// checkpoint/archive custody is released. A completed hard cause is retained.
func (self *economicConservationNativeWorker) close() error {
	if self == nil || !self.active {
		return nil
	}
	self.cancel()
	<-self.done
	result, ready := self.take()
	if !ready {
		return errors.New("economic native owner joined without its original result")
	}
	if result.err == nil || monitorOnlyCancellationCauses(result.err, 0) {
		return nil
	}
	return result.err
}

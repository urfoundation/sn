// One independently bounded verifier owns its child process and one result
// slot. Follow sampling never waits for it. The parent cancels and joins it
// before releasing checkpoint/archive ownership.
package main

import (
	"context"
	"errors"
	"time"
)

type economicConservationFeeResult struct {
	reference planFileReference
	request   *economicNativeFeeRequest
	evidence  *economicNativeFeeEvidence
	known     string
	err       error
}

// Lifecycle fields are accessed only by the parent loop. The child owns its
// immutable request snapshot and communicates only through the bounded channel.
type economicConservationFeeWorker struct {
	ctx       context.Context
	policy    economicConservationPolicy
	reference planFileReference
	budget    time.Duration
	hooks     monitorServiceHooks
	active    bool
	cancel    context.CancelFunc
	done      chan struct{}
	result    chan economicConservationFeeResult
}

func newEconomicConservationFeeWorker(ctx context.Context, policy economicConservationPolicy, reference planFileReference, budget time.Duration, hooks monitorServiceHooks) *economicConservationFeeWorker {
	return &economicConservationFeeWorker{ctx: ctx, policy: policy, reference: reference, budget: budget, hooks: hooks, result: make(chan economicConservationFeeResult, 1)}
}

// Each new attempt gets the original finite budget; previously admitted facts
// need only an identity lookup, never a second original runtime execution.
func (self *economicConservationFeeWorker) start(state *economicConservationState) {
	if self == nil || self.active || self.ctx.Err() != nil || state.NativeFeeHeldRequest == rootObjectHash(self.reference) {
		return
	}
	known := make(map[string]string, len(state.NativeFees))
	for _, value := range state.NativeFees {
		known[value.Evidence.RequestHash] = value.Evidence.ContentHash
	}
	if state.archiveView != nil {
		for request, digest := range state.archiveView.feeRetired {
			known[request] = digest
		}
	}
	ctx, cancel := context.WithTimeout(self.ctx, self.budget)
	authorities, authorityErr := state.admittedNativeFeePolicies(self.policy)
	self.cancel, self.done, self.active = cancel, make(chan struct{}), true
	done := self.done
	go func() {
		defer close(done)
		defer cancel()
		result := economicConservationFeeResult{reference: self.reference, err: authorityErr}
		if result.err == nil {
			result = self.read(ctx, cancel, known, authorities)
		}
		// Exactly one owned producer uses this slot. Publish even a completed
		// integrity refusal racing cancellation so cleanup can retain its cause.
		self.result <- result
		if self.hooks.afterNativeFeeRead != nil {
			self.hooks.afterNativeFeeRead(ctx, result.err)
		}
	}()
}

func (self *economicConservationFeeWorker) read(ctx context.Context, cancel context.CancelFunc, known map[string]string, authorities map[string]economicNativeFeePolicy) economicConservationFeeResult {
	result := economicConservationFeeResult{reference: self.reference}
	if self.hooks.beforeNativeFeeRead != nil {
		self.hooks.beforeNativeFeeRead(ctx, cancel)
	}
	raw, digest, err := readBootstrapRootFile(ctx, self.reference.Path, 64*1024)
	if err != nil {
		result.err = err
		return result
	}
	if digest != self.reference.Sha256 {
		result.err = errors.Join(errEconomicNativeFeeIntegrity, errors.New("economic native fee request differs from its exact pin"))
		return result
	}
	var request economicNativeFeeRequest
	if err := decodePlanJson(raw, &request); err != nil {
		result.err = errors.Join(errEconomicNativeFeeIntegrity, err)
		return result
	}
	result.request = &request
	requestHash := rootObjectHash(request)
	if known[requestHash] != "" {
		result.known, result.err = known[requestHash], ctx.Err()
		return result
	}
	if authority, known := authorities[rootObjectHash(request.Policy)]; !known || request.Policy != authority {
		result.err = errors.Join(errEconomicNativeFeeIntegrity, errEconomicNativeFeeUnadmittedPolicy)
		return result
	}
	result.evidence, result.err = runEconomicNativeFeeEvidence(ctx, request, self.budget, self.hooks.nativeFeeReplay)
	return result
}

// Blocking is used only for an explicitly finite one-shot sample. Follow mode
// polls the result slot and continues healthy observations while it is empty.
func (self *economicConservationFeeWorker) take(wait bool) (economicConservationFeeResult, bool) {
	if self == nil || !self.active {
		return economicConservationFeeResult{}, false
	}
	if wait {
		<-self.done
	}
	select {
	case result := <-self.result:
		<-self.done
		self.active = false
		return result, true
	default:
		return economicConservationFeeResult{}, false
	}
}

func (self *economicConservationFeeWorker) close() error {
	if self == nil || !self.active {
		return nil
	}
	self.cancel()
	result, _ := self.take(true)
	if result.err == nil || errors.Is(result.err, context.Canceled) && monitorOnlyCancellationCauses(result.err, 0) {
		return nil
	}
	return result.err
}

// A failed fee branch updates only its own issue/quarantine fields. Prior fees
// and every domain cursor survive. Completed source contradictions remain held
// for that exact input; an unrelated, separately pinned request is independent.
func applyEconomicConservationFeeResult(ctx context.Context, policy economicConservationPolicy, state *economicConservationState, result economicConservationFeeResult) (*economicConservationState, error) {
	var err = result.err
	next := state
	if err == nil && result.request == nil {
		err = errors.Join(errEconomicNativeFeeIntegrity, errors.New("economic fee worker omitted its original request"))
	}
	if err == nil && result.known != "" {
		if state.retainedNativeFee(rootObjectHash(result.request)) != result.known {
			err = errors.Join(errEconomicNativeFeeIntegrity, errors.New("economic retained fee handoff lost original evidence"))
		}
	} else if err == nil {
		next, err = appendEconomicConservationNativeFeeEvidence(ctx, policy, state, *result.request, result.evidence)
	}
	if err != nil {
		next = state
		next.NativeFeeIssue = economicConservationIssue(err)
		if errors.Is(err, errEconomicNativeFeeIntegrity) {
			next.NativeFeeHeldRequest = rootObjectHash(result.reference)
			next.NativeFeeHeldPolicy = ""
			if errors.Is(err, errEconomicNativeFeeUnadmittedPolicy) && result.request != nil {
				next.NativeFeeHeldPolicy = rootObjectHash(result.request.Policy)
			}
		}
	} else {
		next.NativeFeeIssue, next.NativeFeeHeldRequest = "", ""
		next.NativeFeeHeldPolicy = ""
	}
	next.NativeFeePending = false
	if ctx.Err() != nil {
		return nil, errors.Join(ctx.Err(), err)
	}
	return next, nil
}

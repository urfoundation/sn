// A completed off-chain transition gets a fresh finalized read after its
// effect, while its exact durable recovery intent is still retained. The head
// is an observation checkpoint, not on-chain inclusion of an off-chain event.
package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const faultCompletionTimeout = 5 * time.Minute

// Driver-local completion belongs to the complete specification and action.
// Reopening the driver must reconcile the durable effect and obtain a new head.
type faultCompletedTransition struct {
	faultId   string
	faultHash string
	action    string
	head      ChainHead
}

// An unavailable completion read does not undo a signal or authorize a second
// one. Only this exact fault/action can adopt the retained transition later.
type faultCompletionPendingError struct {
	faultHash string
	action    string
	cause     error
}

func (self *faultCompletionPendingError) Error() string {
	return fmt.Sprintf("fault %s completion checkpoint remains pending: %v", self.action, self.cause)
}

func (self *faultCompletionPendingError) Unwrap() error { return self.cause }

// Keep miner-control's existing durable pending contract separate.
func faultCompletionKind(kind string) bool {
	return kind == "process-restart" || kind == "process-pause" || kind == "container-restart" || kind == "validator-view-filter"
}

// Mutation has started even if its fresh completion read is pending. Keep
// expected-fault attribution scoped to that exact retained transition.
func scenarioFaultApplyPending(record ScenarioFaultRecord) bool {
	return record.Status == "pending" && (record.Kind == "miner-control" && record.ControlStartedBlock != 0 || faultCompletionKind(record.Kind) && record.ApplyStartedBlock != 0)
}

// Matching only a nested error would admit an unrelated joined failure.
func faultCompletionPending(spec scenarioFaultSpec, action string, err error) bool {
	pending, ok := err.(*faultCompletionPendingError)
	if !ok || pending == nil || !faultCompletionKind(spec.Kind) || pending.action != action {
		return false
	}
	hash, hashErr := canonicalHashHex(spec)
	return hashErr == nil && pending.faultHash == hash
}

// Each retry performs only a read. A full 300-second owner budget handles
// repeated fast failures as well as 30-second attempts, without multiplying
// the budget inside nested RPC helpers. Exhaustion leaves durable work pending.
func (self *liveScenarioFaultDriver) captureFaultCompletion(ctx context.Context, spec scenarioFaultSpec, action string) error {
	if self.faultCompletionHead == nil {
		// Standalone fault cleanup does not produce block-timed scenario
		// evidence. The scenario constructor always installs its fresh reader.
		return nil
	}
	if (!faultCompletionKind(spec.Kind) && spec.Kind != "miner-control") || (action != "disable" && action != "enable") {
		return errors.New("fault completion owner is invalid")
	}
	hash, err := canonicalHashHex(spec)
	if err != nil {
		return err
	}
	self.faultCompleted = faultCompletedTransition{}
	headCtx, cancel := context.WithTimeout(ctx, faultCompletionTimeout)
	if self.faultCompletionContext != nil {
		cancel()
		headCtx, cancel = self.faultCompletionContext(ctx)
	}
	defer cancel()
	policy := defaultFinalSemanticRPCRetryPolicy()
	if self.faultCompletionRetryPolicy != nil {
		policy = *self.faultCompletionRetryPolicy
	}
	var head ChainHead
	for {
		err = retryEvmReadRpcCall(headCtx, "fault completion finalized head", policy, func(attemptCtx context.Context) error {
			var readErr error
			head, readErr = self.faultCompletionHead(attemptCtx)
			if readErr != nil {
				return readErr
			}
			if head.Number == 0 || !validCanonicalHashHex(head.Hash) {
				return errors.New("fault completion has an invalid finalized head")
			}
			return nil
		})
		if ownerErr := ctx.Err(); ownerErr != nil {
			return ownerErr
		}
		if err == nil {
			self.faultCompleted = faultCompletedTransition{faultId: spec.ID, faultHash: hash, action: action, head: head}
			return nil
		}
		if !scenarioSnapshotTransportError(err, true) {
			return err
		}
		if headCtx.Err() != nil {
			break
		}
		if waitErr := policy.wait(headCtx, policy.maximumRetryDelay); waitErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if headCtx.Err() == nil || !errors.Is(waitErr, headCtx.Err()) {
				return errors.Join(err, waitErr)
			}
			break
		}
	}
	if spec.Kind == "miner-control" {
		return &minerControlPendingError{cause: err}
	}
	return &faultCompletionPendingError{faultHash: hash, action: action, cause: err}
}

// Pre-acceptance callers have their own finite owner context and keep retrying
// only the same exact pending application. Completed mutations are adopted.
func waitScenarioFaultApply(ctx context.Context, spec scenarioFaultSpec, apply func() ([]FaultProcessEvidence, error)) ([]FaultProcessEvidence, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		processes, err := apply()
		if !(spec.Kind == "miner-control" && minerControlPending(err) || faultCompletionPending(spec, "disable", err)) {
			return processes, err
		}
		if err := waitSupervisorRestart(ctx, minerControlRetryDelay); err != nil {
			return processes, err
		}
	}
}

// These additive fields retain non-miner apply progress without claiming a
// block of completed activation. Legacy records omit them byte-for-byte.
func validateScenarioCampaignFaultApply(record ScenarioFaultRecord) error {
	if record.ApplyPendingRounds == 0 {
		if record.ApplyStartedBlock != 0 || record.ApplyStartedBlockHash != "" {
			return fmt.Errorf("scenario campaign fault %q has an apply boundary without retries", record.ID)
		}
		return nil
	}
	if !faultCompletionKind(record.Kind) || (record.Status != "pending" && record.Status != "active" && record.Status != "restored") ||
		record.ApplyStartedBlock < record.TriggerBlock || !validCanonicalHashHex(record.ApplyStartedBlockHash) ||
		record.AppliedBlock != 0 && record.AppliedBlock < record.ApplyStartedBlock ||
		record.Status == "pending" && record.Error == "" || len(record.Targets) == 0 || len(record.Processes) != len(record.Targets) {
		return fmt.Errorf("scenario campaign fault %q has malformed apply retry evidence", record.ID)
	}
	targets := make(map[string]bool, len(record.Targets))
	for _, target := range record.Targets {
		if target == "" || targets[target] {
			return fmt.Errorf("scenario campaign fault %q has an invalid apply retry target census", record.ID)
		}
		targets[target] = true
	}
	for _, process := range record.Processes {
		if !targets[process.ID] || process.PID <= 1 || process.Role == "" || process.Identity == "" {
			return fmt.Errorf("scenario campaign fault %q has invalid apply retry process evidence", record.ID)
		}
		delete(targets, process.ID)
	}
	return nil
}

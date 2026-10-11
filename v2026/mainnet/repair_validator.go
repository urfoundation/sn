// One process owns this stopped-generation incident through a permanent local
// journal. Systemd side effects cannot be rolled back or silently attempted twice.
package main

import (
	"context"
	"errors"
	"time"
)

// The durable disposition is explicit about uncertainty and observation scope.
type repairValidatorResult struct {
	Schema              string                        `json:"schema"`
	IncidentId          string                        `json:"incident_id"`
	Status              string                        `json:"status"`
	StartConsumed       bool                          `json:"start_consumed"`
	Observations        uint32                        `json:"observations"`
	Generation          *repairValidatorGeneration    `json:"acknowledged_generation,omitempty"`
	Completed           *repairValidatorPostcondition `json:"process_postcondition,omitempty"`
	OperatorDisposition string                        `json:"operator_disposition"`
	ChainSuccessProven  bool                          `json:"chain_success_proven"`
}

// A retained completion is evidence of one past observation, never live health.
func (self repairValidatorRecord) result() repairValidatorResult {
	disposition := "No start was issued; inspect the retained incident and signed limits."
	if !self.StartAt.IsZero() {
		disposition = "The one start allowance is consumed. Automatic retry cannot start this unit again; reconcile the retained generation."
	}
	if self.Generation == nil && !self.StartAt.IsZero() {
		disposition = "Start allowance consumed; effect is uncertain. Manual host reconciliation is required. Do not delete or replace the journal, and do not retry with another state path."
	}
	if self.Completed != nil {
		disposition = "The acknowledged generation published exact-source process progress. Retain this completed incident; continue protocol and liability monitoring."
	}
	return repairValidatorResult{Schema: repairValidatorSchema, IncidentId: self.Approval.Plan.IncidentId, Status: self.Status, StartConsumed: !self.StartAt.IsZero(), Observations: self.Observations, Generation: self.Generation, Completed: self.Completed, OperatorDisposition: disposition, ChainSuccessProven: false}
}

// Matching pid alone cannot admit an unrelated service invocation or host boot.
func repairValidatorRunning(plan repairValidatorPlan, manager repairValidatorManager, generation repairValidatorGeneration) bool {
	return manager.Active == "active" && manager.SubState == "running" && manager.MainPid == generation.Pid && manager.Generation == generation && manager.Cgroup == "/system.slice/"+plan.Unit.Name && repairValidatorHex(generation.InvocationId, 16) && generation.InvocationId != plan.Previous.InvocationId && generation.Pid > 1 && generation.StartedUsec > plan.Previous.StartedUsec
}

// This step performs at most one actual start, with no detached worker. A
// consumed unacknowledged start remains uncertain even if a new process exists.
func resumeRepairValidator(ctx context.Context, store *repairValidatorStore, host *repairValidatorHost, now func() time.Time) (result repairValidatorResult, resultErr error) {
	if ctx == nil || ctx.Err() != nil || store == nil || host == nil || now == nil {
		var cause error
		if ctx != nil {
			cause = ctx.Err()
		}
		return repairValidatorResult{}, errors.Join(errors.New("validator repair resume owner is unavailable"), cause)
	}
	record, err := store.load(ctx)
	if err != nil {
		return repairValidatorResult{}, err
	}
	finish := func(status string, cause error) (repairValidatorResult, error) {
		record.Status = status
		return record.result(), errors.Join(cause, store.save(record))
	}
	if record.Completed != nil {
		return record.result(), nil
	}
	if !record.StartAt.IsZero() && record.Generation == nil {
		return finish("uncertain-consumed-start", nil)
	}
	plan := store.approval.Plan
	control, err := host.control(ctx, plan.Unit)
	if err != nil {
		return finish("source-refused", err)
	}
	defer func() { resultErr = errors.Join(resultErr, control.close()) }()
	if record.StartAt.IsZero() {
		if err := host.refuseActiveClaim(plan); err != nil {
			return finish("source-refused", err)
		}
	}
	stamp := now()
	if stamp.IsZero() || stamp.Before(record.HighWaterAt) {
		return finish("clock-rollback", errors.New("validator repair clock moved backwards"))
	}
	record.HighWaterAt = stamp
	if record.StartAt.IsZero() && (stamp.Before(plan.ValidFrom) || !stamp.Before(plan.ExpiresAt)) {
		return finish("approval-window-closed", nil)
	}
	if record.Observations >= plan.MaximumObservations {
		return finish("observation-limit", nil)
	}
	record.Observations++
	record.Status = "observation-reserved"
	if err := store.save(record); err != nil {
		return record.result(), err
	}
	manager, err := host.inspect(ctx, plan)
	if err != nil {
		return finish("source-refused", err)
	}
	if record.StartAt.IsZero() {
		if err := host.stopped(ctx, plan, manager); err != nil {
			if repairValidatorObservationPending(err) {
				return finish("source-refused", err)
			}
			return finish("generation-changed", err)
		}
		stamp = now()
		incidentAt, err := host.incident(ctx, plan, stamp)
		if err != nil {
			return finish("source-refused", err)
		}
		// Expensive reads precede this final clock/authority check. The private
		// host custody owner excludes concurrent administrative starts/changes.
		stamp = now()
		if stamp.Before(record.HighWaterAt) || stamp.Before(plan.ValidFrom) || !stamp.Before(plan.ExpiresAt) || ctx.Err() != nil {
			return finish("approval-window-closed", ctx.Err())
		}
		monotonic, err := host.monotonic()
		if err != nil {
			return finish("source-refused", repairValidatorObservationError("cannot read validator repair monotonic clock", err, false))
		}
		if monotonic <= plan.Previous.StartedUsec {
			return finish("source-refused", errors.New("validator repair monotonic clock differs"))
		}
		record.HighWaterAt, record.StartAt, record.StartMonotonicUsec, record.Status = stamp, stamp, monotonic, "start-consumed"
		if err := store.save(record); err != nil {
			return record.result(), err
		}
		// fsync is not an instantaneous boundary. Expiry and this retained
		// incident's age must still admit the effect after durable consumption.
		actionAt := now()
		if actionAt.Before(record.HighWaterAt) || actionAt.Before(plan.ValidFrom) || !actionAt.Before(plan.ExpiresAt) || actionAt.Sub(incidentAt) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second || ctx.Err() != nil {
			return finish("uncertain-consumed-start", errors.Join(errors.New("validator repair authority window closed during durable reservation"), ctx.Err()))
		}
		record.HighWaterAt = actionAt
		if err := control.validate(); err != nil {
			return finish("uncertain-consumed-start", err)
		}
		if err := host.start(ctx, plan, func() error {
			dispatchAt := now()
			if dispatchAt.Before(record.HighWaterAt) || dispatchAt.Before(plan.ValidFrom) || !dispatchAt.Before(plan.ExpiresAt) || dispatchAt.Sub(incidentAt) > time.Duration(plan.MaximumSampleAgeSeconds)*time.Second || ctx.Err() != nil {
				return errors.Join(errors.New("validator repair authority window closed before start dispatch"), ctx.Err())
			}
			record.HighWaterAt = dispatchAt
			return nil
		}); err != nil {
			return finish("uncertain-consumed-start", err)
		}
		manager, err = host.inspect(ctx, plan)
		if err != nil {
			return finish("uncertain-consumed-start", err)
		}
		if !repairValidatorRunning(plan, manager, manager.Generation) || manager.Generation.StartedUsec < record.StartMonotonicUsec {
			return finish("uncertain-consumed-start", errors.New("validator repair start lacks an attributable generation"))
		}
		generation := manager.Generation
		record.Generation, record.Status = &generation, "waiting-progress"
		if err := store.save(record); err != nil {
			return record.result(), err
		}
	}
	if !repairValidatorRunning(plan, manager, *record.Generation) {
		return finish("generation-changed", errors.New("validator repair acknowledged generation changed"))
	}
	stamp = now()
	if stamp.Before(record.HighWaterAt) {
		return finish("clock-rollback", errors.New("validator repair postcondition clock moved backwards"))
	}
	record.HighWaterAt = stamp
	postcondition, err := host.progress(ctx, plan, record.StartAt, stamp)
	if err != nil {
		return finish("waiting-progress", err)
	}
	manager, err = host.inspect(ctx, plan)
	if err != nil {
		return finish("source-refused", err)
	}
	if !repairValidatorRunning(plan, manager, *record.Generation) {
		return finish("generation-changed", errors.New("validator repair generation changed during postcondition"))
	}
	record.Completed = postcondition
	return finish("resumed-generation-observed", nil)
}

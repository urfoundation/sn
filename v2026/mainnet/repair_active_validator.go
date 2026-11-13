// A diagnosed active steering hang has separate, signed one-stop/one-start
// authority. Every command is joined; durable uncertainty never grants a retry.
package main

import (
	"context"
	"errors"
	"time"
)

// Only a historical process-liveness postcondition is reported as completion.
type repairActiveValidatorResult struct {
	Schema              string                        `json:"schema"`
	IncidentId          string                        `json:"incident_id"`
	Status              string                        `json:"status"`
	StopConsumed        bool                          `json:"stop_consumed"`
	StartConsumed       bool                          `json:"start_consumed"`
	Observations        uint32                        `json:"observations"`
	JoinedAt            time.Time                     `json:"joined_at"`
	Generation          *repairValidatorGeneration    `json:"acknowledged_generation,omitempty"`
	Completed           *repairValidatorPostcondition `json:"process_postcondition,omitempty"`
	OperatorDisposition string                        `json:"operator_disposition"`
	ChainSuccessProven  bool                          `json:"chain_success_proven"`
}

// Operators retain the original incident and uncertain effects across failures.
func (self repairActiveValidatorRecord) result() repairActiveValidatorResult {
	disposition := "No stop or start consumed; retain the independent approval and original steering incident."
	if !self.StopAt.IsZero() {
		disposition = "One stop allowance is consumed. Resume may only observe the exact old generation's bounded join; it cannot repeat stop. Retain all custody artifacts."
	}
	if !self.StartAt.IsZero() {
		disposition = "One start allowance is consumed. Never retry with another envelope or journal. Reconcile the acknowledged generation or obtain manual host disposition."
	}
	if self.Completed != nil {
		disposition = "The acknowledged replacement published a fresh steering outcome. This is historical process liveness; continue chain, intent and liability monitoring."
	}
	return repairActiveValidatorResult{Schema: repairActiveValidatorSchema, IncidentId: self.Approval.Plan.Process.IncidentId, Status: self.Status, StopConsumed: !self.StopAt.IsZero(), StartConsumed: !self.StartAt.IsZero(), Observations: self.Observations, JoinedAt: self.JoinedAt, Generation: self.Generation, Completed: self.Completed, OperatorDisposition: disposition, ChainSuccessProven: false}
}

// Each invocation consumes at most one observation and issues each concrete
// action once per lifetime claim. No sleep, background worker or implicit restart.
func resumeRepairActiveValidator(ctx context.Context, store *repairActiveValidatorStore, host *repairValidatorHost, now func() time.Time) (result repairActiveValidatorResult, resultErr error) {
	if ctx == nil || ctx.Err() != nil || store == nil || host == nil || now == nil {
		var cause error
		if ctx != nil {
			cause = ctx.Err()
		}
		return result, errors.Join(errors.New("active repair resume owner unavailable"), cause)
	}
	record, err := store.load(ctx)
	if err != nil {
		return result, err
	}
	finish := func(status string, cause error) (repairActiveValidatorResult, error) {
		record.Status = status
		return record.result(), errors.Join(cause, store.save(record))
	}
	plan := store.approval.Plan
	p := plan.Process
	control, err := host.control(ctx, p.Unit)
	if err != nil {
		return finish("source-refused", err)
	}
	defer func() { resultErr = errors.Join(resultErr, control.close()) }()
	if err := host.activeClaim(ctx, store.approval, store.publicKey, false); err != nil {
		return record.result(), err
	}
	if record.Completed != nil {
		return record.result(), nil
	}
	if !record.StartAt.IsZero() && record.Generation == nil {
		return finish("uncertain-consumed-start", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(plan.JoinWindowSeconds+2*p.CommandTimeoutSeconds)*time.Second)
	defer cancel()
	stamp := now()
	if stamp.IsZero() || stamp.Before(record.HighWaterAt) {
		return finish("clock-rollback", errors.New("active repair clock moved backwards"))
	}
	record.HighWaterAt = stamp
	if record.StartAt.IsZero() && (stamp.Before(p.ValidFrom) || !stamp.Before(p.ExpiresAt)) {
		return finish("approval-window-closed", nil)
	}
	if record.Observations >= p.MaximumObservations {
		return finish("observation-limit", nil)
	}
	record.Observations++
	record.Status = "observation-reserved"
	if err := store.save(record); err != nil {
		return record.result(), err
	}
	// Read wall time after the monotonic read, so that read cannot outlive the
	// wall-clock authority check at the final start boundary.
	actionClock := func(join bool) (time.Time, uint64, error) {
		mono, err := host.monotonic()
		stamp := now()
		if err := errors.Join(err, ctx.Err()); err != nil {
			return stamp, mono, repairValidatorObservationError("cannot read active repair action clock", err, false)
		}
		if stamp.Before(record.HighWaterAt) || stamp.Before(p.ValidFrom) || !stamp.Before(p.ExpiresAt) || mono <= p.Previous.StartedUsec {
			return stamp, mono, errors.New("active repair action clock or authority window closed")
		}
		if join && (mono < record.StopMonotonicUsec || mono-record.StopMonotonicUsec > uint64(plan.JoinWindowSeconds)*1000000 || stamp.Sub(record.StopAt) > time.Duration(plan.JoinWindowSeconds)*time.Second) {
			return stamp, mono, errors.New("active repair join window closed")
		}
		if mono < record.JoinedMonotonicUsec || mono < record.StartMonotonicUsec {
			return stamp, mono, errors.New("active repair monotonic clock moved backwards")
		}
		return stamp, mono, nil
	}
	// Recheck all physical custody and signed clocks after every durable cut.
	actionCheck := func(join bool) (time.Time, uint64, error) {
		stamp, mono, err := actionClock(join)
		if err != nil {
			return stamp, mono, err
		}
		return stamp, mono, errors.Join(control.validate(), store.validateOwner(), host.activeClaim(ctx, store.approval, store.publicKey, false))
	}
	manager, err := host.inspectActive(ctx, plan)
	if err != nil {
		return finish("source-refused", err)
	}
	if record.StopAt.IsZero() {
		if !repairActiveValidatorRunning(p, manager) {
			return finish("generation-changed", errors.New("active repair approved generation is not active"))
		}
		if err := host.activeIncident(ctx, plan, now(), true); err != nil {
			return finish("source-refused", err)
		}
		stamp, mono, err := actionCheck(false)
		if err != nil {
			return finish("source-refused", err)
		}
		if err := plan.stopWindow(stamp); err != nil {
			return finish("approval-window-closed", err)
		}
		record.HighWaterAt, record.StopAt, record.StopMonotonicUsec, record.Status = stamp, stamp, mono, "stop-consumed"
		if err := store.save(record); err != nil {
			return record.result(), err
		}
		manager, err = host.inspectActive(ctx, plan)
		if err != nil {
			return finish("source-refused", err)
		}
		if !repairActiveValidatorRunning(p, manager) {
			return finish("generation-changed", errors.New("active repair generation changed after stop reservation"))
		}
		if err := host.activeIncident(ctx, plan, now(), true); err != nil {
			return finish("source-refused", err)
		}
		stamp, _, err = actionCheck(true)
		if err != nil {
			return finish("source-refused", err)
		}
		if err := plan.stopWindow(stamp); err != nil {
			return finish("approval-window-closed", err)
		}
		record.HighWaterAt = stamp
		if err := host.stopActive(ctx, plan); err != nil {
			return finish("waiting-join", err)
		}
		manager, err = host.inspectActive(ctx, plan)
		if err != nil {
			return finish("waiting-join", err)
		}
	}
	if record.StartAt.IsZero() {
		stamp, mono, err := actionCheck(true)
		if err != nil {
			if repairValidatorObservationPending(err) {
				return finish("source-refused", err)
			}
			return finish("join-window-closed", err)
		}
		if err := host.stopped(ctx, p, manager); err != nil {
			return finish("waiting-join", err)
		}
		if err := host.activeIncident(ctx, plan, now(), false); err != nil {
			return finish("source-refused", err)
		}
		if record.JoinedAt.IsZero() {
			record.HighWaterAt, record.JoinedAt, record.JoinedMonotonicUsec, record.Status = stamp, stamp, mono, "join-observed"
			if err := store.save(record); err != nil {
				return record.result(), err
			}
		}
		stamp, mono, err = actionCheck(true)
		if err != nil {
			if repairValidatorObservationPending(err) {
				return finish("source-refused", err)
			}
			return finish("join-window-closed", err)
		}
		record.HighWaterAt, record.StartAt, record.StartMonotonicUsec, record.Status = stamp, stamp, mono, "start-consumed"
		if err := store.save(record); err != nil {
			return record.result(), err
		}
		manager, err = host.inspectActive(ctx, plan)
		if err == nil {
			err = host.stopped(ctx, p, manager)
		}
		if err == nil {
			err = host.activeIncident(ctx, plan, now(), false)
		}
		if err != nil {
			return finish("uncertain-consumed-start", err)
		}
		stamp, _, err = actionCheck(true)
		if err != nil {
			return finish("uncertain-consumed-start", err)
		}
		record.HighWaterAt = stamp
		if err := host.start(ctx, p, func() error {
			dispatchAt, _, err := actionClock(true)
			if err == nil {
				record.HighWaterAt = dispatchAt
			}
			return err
		}); err != nil {
			return finish("uncertain-consumed-start", err)
		}
		manager, err = host.inspectActive(ctx, plan)
		if err != nil {
			return finish("uncertain-consumed-start", err)
		}
		if !repairValidatorRunning(p, manager, manager.Generation) || manager.Generation.StartedUsec < record.StartMonotonicUsec {
			return finish("uncertain-consumed-start", errors.New("active repair start has no attributable generation"))
		}
		generation := manager.Generation
		record.Generation, record.Status = &generation, "waiting-progress"
		if err := store.save(record); err != nil {
			return record.result(), err
		}
	}
	if !repairValidatorRunning(p, manager, *record.Generation) {
		return finish("generation-changed", errors.New("active repair acknowledged generation changed"))
	}
	stamp = now()
	if stamp.Before(record.HighWaterAt) {
		return finish("clock-rollback", errors.New("active repair completion clock moved backwards"))
	}
	record.HighWaterAt = stamp
	postcondition, err := host.activeProgress(ctx, plan, record.StartAt, stamp)
	if err != nil {
		return finish("waiting-progress", err)
	}
	manager, err = host.inspectActive(ctx, plan)
	if err != nil {
		return finish("source-refused", err)
	}
	if !repairValidatorRunning(p, manager, *record.Generation) {
		return finish("generation-changed", errors.New("active repair generation changed during progress read"))
	}
	record.Completed = postcondition
	return finish("responsive-generation-observed", nil)
}

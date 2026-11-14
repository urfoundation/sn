// Original signed repair adapters retain every effect and generation boundary.
// This consumer neither issues signatures nor broadens their action vocabulary.
package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Only one signature domain is populated after independent verification.
type repairControllerEnvelope struct {
	validator *repairValidatorApproval
	active    *repairActiveValidatorApproval
	process   repairControllerProcess
	plan      repairValidatorPlan
}

// Each stopped-process role retains its own signature and original custody.
// This interface grants no validator action or controller-selected command.
type repairControllerProcess interface {
	profile() repairValidatorPlan
	protectedPaths() []string
	ready(context.Context, *repairValidatorHost, time.Time) error
	claim(context.Context, string, *repairValidatorHost, func() time.Time) error
	resume(context.Context, string, *repairValidatorHost, func() time.Time) (string, bool, error)
}

// Protected original file bytes and independent manifest key both bind authority.
func loadRepairControllerEnvelope(ctx context.Context, entry repairControllerEntry, host *repairValidatorHost) (repairControllerEnvelope, error) {
	var result repairControllerEnvelope
	if err := host.pin(ctx, entry.Approval, 64*1024, false); err != nil {
		return result, err
	}
	raw, digest, err := readPlanFile(ctx, entry.Approval.Path, 64*1024)
	if err != nil {
		return result, err
	}
	if digest != entry.Approval.Sha256 {
		return result, errors.Join(errRpcIntegrity, errors.New("repair controller approval bytes changed"))
	}
	switch entry.Kind {
	case "validator":
		var approval repairValidatorApproval
		if err := decodePlanJson(raw, &approval); err != nil {
			return result, errors.Join(errRpcIntegrity, err)
		}
		if err := approval.validate(entry.PublicKey); err != nil {
			return result, errors.Join(errRpcIntegrity, err)
		}
		result.validator, result.plan = &approval, approval.Plan
	case "active-validator":
		var approval repairActiveValidatorApproval
		if err := decodePlanJson(raw, &approval); err != nil {
			return result, errors.Join(errRpcIntegrity, err)
		}
		if err := approval.validate(entry.PublicKey); err != nil {
			return result, errors.Join(errRpcIntegrity, err)
		}
		result.active, result.plan = &approval, approval.Plan.Process
	case "root-passive":
		process, err := loadRepairRootPassiveEnvelope(ctx, raw, entry.PublicKey, host)
		if process != nil {
			result.process, result.plan = process, process.profile()
		}
		if err != nil {
			return result, err
		}
	case "operator":
		process, err := loadRepairOperatorEnvelope(ctx, raw, entry.PublicKey, host)
		if process != nil {
			result.process, result.plan = process, process.profile()
		}
		if err != nil {
			return result, err
		}
	default:
		return result, errors.Join(errRpcIntegrity, errors.New("repair controller has no adapter for this action"))
	}
	return result, host.parents(result.plan.StatePath, host.rootUid)
}

// Output admission includes every original role input and lifetime marker.
// Generation claims are protected names, even when two entries share a unit.
func (self repairControllerEnvelope) protectedPaths() []string {
	if self.process != nil {
		return append(self.process.protectedPaths(), repairProcessClaimPath(self.plan))
	}
	p := self.plan
	paths := []string{p.Unit.File.Path, p.Unit.File.Path + ".sn-control.lock", p.Unit.Binary.Path, p.Unit.Config.Path, p.Systemctl.Path, p.MonitorCheckpoint, p.Unit.ProgressFile}
	if p.Unit.DurableVolumes != nil {
		paths = append(paths, p.Unit.DurableVolumes.Path)
	}
	if self.active != nil {
		paths = append(paths, self.active.Plan.MonitorServices.Path)
	}
	return paths
}

// Check current incident availability before recording a claim intent. Signed
// authority remains unchanged while absent or stale monitoring stays pending.
func (self repairControllerEnvelope) ready(ctx context.Context, host *repairValidatorHost, now time.Time) error {
	if self.process != nil {
		return self.process.ready(ctx, host, now)
	}
	p := self.plan
	if now.Before(p.ValidFrom) {
		return errRepairControllerPending
	}
	if !now.Before(p.ExpiresAt) {
		return errors.Join(errRepairControllerHeld, errors.New("original repair approval expired"))
	}
	raw, err := host.read(ctx, p.MonitorCheckpoint, p.MonitorUid, maxMonitorServiceCheckpointBytes, true)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !errors.Is(err, durablevolume.ErrIdentity) {
			return errors.Join(errRepairControllerPending, err)
		}
		return err
	}
	var current monitorServiceCheckpointRecord
	if err := decodePlanJson(raw, &current); err != nil {
		return errors.Join(errRpcIntegrity, err)
	}
	hash, err := hashMonitorServiceCheckpoint(current)
	if err != nil || hash != current.ContentHash || current.Role != p.Role || current.Expected != p.Source || !current.State.ClockFaultAt.IsZero() || current.State.SampleAt.After(now) {
		return errors.Join(errRpcIntegrity, errors.New("repair controller incident integrity differs"), err)
	}
	if current.State.SampleAt.Before(p.Original.State.SampleAt) || now.Sub(current.State.SampleAt) > time.Duration(p.MaximumSampleAgeSeconds)*time.Second {
		return errRepairControllerPending
	}
	if self.active != nil {
		if err := self.active.Plan.incident(current); err != nil {
			return errors.Join(errRepairControllerPending, err)
		}
		manager, err := host.inspectActive(ctx, self.active.Plan)
		if err != nil {
			return err
		}
		if !repairActiveValidatorRunning(p, manager) {
			return errors.Join(errRepairControllerHeld, errors.New("original active generation differs"))
		}
		return host.activeIncident(ctx, self.active.Plan, now, true)
	}
	if err := p.incident(current); err != nil {
		return errors.Join(errRepairControllerPending, err)
	}
	manager, err := host.inspect(ctx, p)
	if err == nil {
		err = host.stopped(ctx, p, manager)
	}
	return err
}

// Original claim primitives keep their durable counter and permanent generation
// markers. The controller must have published its own intent before this call.
func (self repairControllerEnvelope) claim(ctx context.Context, key string, host *repairValidatorHost, now func() time.Time) (resultErr error) {
	if self.process != nil {
		return self.process.claim(ctx, key, host, now)
	}
	stamp := now()
	if err := self.ready(ctx, host, stamp); err != nil {
		return err
	}
	admit := func() error {
		current := now()
		if current.Before(stamp) || current.Before(self.plan.ValidFrom) || !current.Before(self.plan.ExpiresAt) || ctx.Err() != nil {
			return errors.Join(errRepairControllerHeld, errors.New("original claim window changed during admission"), ctx.Err())
		}
		return nil
	}
	if self.active != nil {
		control, err := host.control(ctx, self.plan.Unit)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, control.close()) }()
		if err := self.ready(ctx, host, now()); err != nil {
			return err
		}
		if err := admit(); err != nil {
			return err
		}
		if err := host.activeClaim(ctx, *self.active, key, true); err != nil {
			return err
		}
		store, err := openRepairActiveValidatorStore(ctx, *self.active, key, true, now())
		if err != nil {
			return err
		}
		return store.close()
	}
	if err := admit(); err != nil {
		return err
	}
	store, err := openRepairValidatorStore(ctx, *self.validator, key, true, now())
	if err != nil {
		return err
	}
	return store.close()
}

// Every repeat opens the existing signed journal. Missing state after intent is
// a custody failure even when the currently observed service looks healthy.
func (self repairControllerEnvelope) resume(ctx context.Context, key string, host *repairValidatorHost, now func() time.Time) (string, bool, error) {
	var status string
	var complete bool
	var err error
	if self.process != nil {
		status, complete, err = self.process.resume(ctx, key, host, now)
	} else if self.active != nil {
		var store *repairActiveValidatorStore
		store, err = openRepairActiveValidatorStore(ctx, *self.active, key, false, now())
		if err == nil {
			var result repairActiveValidatorResult
			result, err = resumeRepairActiveValidator(ctx, store, host, now)
			status, complete = result.Status, result.Completed != nil
			err = errors.Join(err, store.close())
		}
	} else {
		var store *repairValidatorStore
		store, err = openRepairValidatorStore(ctx, *self.validator, key, false, now())
		if err == nil {
			var result repairValidatorResult
			result, err = resumeRepairValidator(ctx, store, host, now)
			status, complete = result.Status, result.Completed != nil
			err = errors.Join(err, store.close())
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		err = errors.Join(durablevolume.ErrIdentity, err)
	}
	if err == nil && !complete {
		switch status {
		case "claimed", "observation-reserved", "waiting-progress", "waiting-join", "join-observed", "source-refused":
		default:
			err = errors.Join(errRepairControllerHeld, errors.New("original repair allowance requires reconciliation"))
		}
	}
	return status, complete, err
}

// The actual public consumer fixes these adapters; a test may inject only its
// private host transport, never an executable command or action from the manifest.
func repairControllerHostStep(host *repairValidatorHost, now func() time.Time) repairControllerStep {
	var stateLock sync.Mutex
	units := map[string]*sync.Mutex{}
	return func(ctx context.Context, entry repairControllerEntry, attempted bool, reserve func() error) (string, bool, error) {
		envelope, err := loadRepairControllerEnvelope(ctx, entry, host)
		if err != nil {
			return "approval-refused", false, err
		}
		stateLock.Lock()
		unit := units[envelope.plan.Unit.Name]
		if unit == nil {
			unit = &sync.Mutex{}
			units[envelope.plan.Unit.Name] = unit
		}
		stateLock.Unlock()
		if !unit.TryLock() {
			return "original-unit-busy", false, errRepairControllerPending
		}
		defer unit.Unlock()
		if err := ctx.Err(); err != nil {
			return "cancelled", false, err
		}
		if attempted {
			if _, err := os.Lstat(envelope.plan.StatePath); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					err = errors.Join(durablevolume.ErrIdentity, err)
				}
				return "original-journal-unavailable", false, err
			}
		}
		if !attempted {
			_, statErr := os.Lstat(envelope.plan.StatePath)
			create := errors.Is(statErr, os.ErrNotExist)
			if statErr != nil && !create {
				return "journal-unavailable", false, statErr
			}
			if create {
				if err := envelope.ready(ctx, host, now()); err != nil {
					return "incident-pending", false, err
				}
			}
			if err := reserve(); err != nil {
				return "claim-intent-refused", false, err
			}
			if create {
				if err := envelope.claim(ctx, entry.PublicKey, host, now); err != nil {
					return "claim-refused", false, err
				}
			}
		}
		return envelope.resume(ctx, entry.PublicKey, host, now)
	}
}

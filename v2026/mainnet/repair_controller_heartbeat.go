// Controller liveness is a separate current-owner observation. It never
// advances a repair outcome, edits an allowance or grants another action.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const repairControllerHeartbeatInterval = 30 * time.Second

// A completed refusal remains a hold even when shutdown also cancels a child.
// This marks the disposition without relabelling an unknown error as corruption.
var errRepairControllerHeartbeatHeld = errors.New("repair controller heartbeat publication is held")

// Tests can deliver timer wakes and observe joined publications, but cannot
// replace a checkpoint, successful owner check or metrics publication.
type repairControllerHeartbeatHooks struct {
	ticks     <-chan time.Time
	afterTick func(time.Time, error)
}

type repairControllerHeartbeatKey struct{}

// One mutex serializes actual checkpoint writes, owner rechecks and metrics
// publication. Host operations never hold it. The retained record is copied
// only after successful durable checkpoint acknowledgment.
type repairControllerHeartbeat struct {
	stateLock     sync.Mutex
	record        repairControllerRecord
	store         *repairValidatorFileOwner
	metrics       *monitorMetricsStore
	manifest      repairControllerManifest
	hash          string
	now           func() time.Time
	checkManifest func(context.Context) error
	cancelOwner   context.CancelFunc
	stderr        io.Writer
	lastHeartbeat time.Time
	publication   error
	outputHeld    error
	fatal         error
	stop          context.CancelFunc
	joined        chan struct{}
}

// A copy prevents the scheduler's next in-memory mutation from becoming a
// heartbeat's outcome before the checkpoint containing it is acknowledged.
func (self *repairControllerHeartbeat) retain(record repairControllerRecord) {
	self.record = record
	self.record.Entries = append([]repairControllerEntryState(nil), record.Entries...)
}

// The heartbeat starts only after original graph admission and both prepared
// owners have opened. No startup or missing-graph marker asserts liveness.
func (self *repairControllerHeartbeat) start(ctx context.Context) {
	owner, stop := context.WithCancel(ctx)
	self.stop, self.joined = stop, make(chan struct{})
	hooks, _ := ctx.Value(repairControllerHeartbeatKey{}).(repairControllerHeartbeatHooks)
	go func() {
		defer close(self.joined)
		for {
			ticks := hooks.ticks
			if ticks == nil {
				ticks = time.After(repairControllerHeartbeatInterval)
			}
			select {
			case <-owner.Done():
				return
			case _, open := <-ticks:
				if !open {
					return
				}
			}
			if owner.Err() != nil {
				return
			}
			err := self.publish(owner)
			if hooks.afterTick != nil {
				hooks.afterTick(self.now(), err)
			}
		}
	}()
}

// Stop and join the timer owner before closing any descriptor it could use.
// Optional publication failures remain visible without interrupting an already
// admitted repair; required custody failure cancels and joins its owner.
func (self *repairControllerHeartbeat) close() error {
	if self.stop != nil {
		self.stop()
		<-self.joined
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return errors.Join(self.fatal, self.outputHeld, self.publication)
}

// A temporary unreadable fence withholds the heartbeat. Only a completed hard
// refusal cancels the controller; both original causes remain available at exit.
func (self *repairControllerHeartbeat) checkWithLock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	manifestErr := self.checkManifest(ctx)
	ownerErr := self.store.validateOwner()
	err := errors.Join(manifestErr, ownerErr)
	// Classify independent observations before joining: a proved manifest
	// mismatch cannot inherit availability from an unrelated owner read.
	manifestHeld := manifestErr != nil && !rootMonitorStartupPending(manifestErr)
	ownerHeld := ownerErr != nil && !repairValidatorObservationPending(ownerErr) && !monitorOnlyCancellationCauses(ownerErr, 0)
	if manifestHeld || ownerHeld {
		self.fatal = errors.Join(errRepairControllerHeartbeatHeld, err)
		self.cancelOwner()
	}
	return err
}

func (self *repairControllerHeartbeat) check(ctx context.Context) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.checkWithLock(ctx)
}

// Called under the same lock as durable publication, this fence cannot observe
// an intermediate expected hash or refresh an unacknowledged scheduler change.
func (self *repairControllerHeartbeat) publishWithLock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := self.checkWithLock(ctx); err != nil {
		if !monitorOnlyCancellationCauses(err, 0) {
			self.publication = err
		}
		return err
	}
	if self.outputHeld != nil {
		return self.outputHeld
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stamp := self.now()
	if stamp.IsZero() || stamp.Unix() < 0 || stamp.Before(self.record.HighWaterAt) || stamp.Before(self.lastHeartbeat) {
		err := errors.Join(errRpcIntegrity, errors.New("repair controller heartbeat clock moved backwards"))
		self.fatal = errors.Join(self.fatal, err)
		self.cancelOwner()
		return err
	}
	raw := append(self.record.metrics(), []byte(fmt.Sprintf("sn_mainnet_repair_controller_heartbeat_timestamp_seconds %d\n", stamp.Unix()))...)
	if err := self.metrics.saveRaw(raw); err != nil {
		self.publication = err
		if !rootMonitorStartupPending(err) {
			self.outputHeld = errors.Join(errRepairControllerHeartbeatHeld, err)
		}
		return err
	}
	self.lastHeartbeat, self.publication = stamp, nil
	return nil
}

func (self *repairControllerHeartbeat) publish(ctx context.Context) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.publishWithLock(ctx)
}

// Repair effects still depend on durable intent acknowledgment. A failed
// optional textfile write cannot turn that acknowledged intent into a retry.
func (self *repairControllerHeartbeat) persist(ctx context.Context, record repairControllerRecord) error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err := saveRepairControllerStore(self.store, record, self.manifest, self.hash); err != nil {
		return err
	}
	self.retain(record)
	if err := self.publishWithLock(ctx); err != nil {
		if self.fatal != nil {
			return self.fatal
		}
		fmt.Fprintln(self.stderr, "repair controller metrics observation:", err)
	}
	return nil
}

// Role-specific signatures share only a stopped-process custody engine. A
// durable reservation precedes the sole start; an uncertain effect never retries.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var errRepairProcessPending = errors.New("approved process repair evidence is pending")
var errRepairProcessHeld = errors.New("approved process repair requires reconciliation")

// Each implementation verifies its own signature domain and original authority.
// The profile exposes physical custody, never validator or signing permission.
type repairProcessEnvelope interface {
	profile() repairValidatorPlan
	schema() string
	approvalHash() string
	validate(string) error
	open(context.Context, *repairValidatorHost) (repairProcessCustody, error)
}

// The caller borrows one exact original authority until its joined effect ends.
// Progress returns a retained local observation hash, never chain success.
type repairProcessCustody interface {
	check(context.Context) error
	inspect(context.Context) (repairValidatorManager, error)
	beforeStart(context.Context, time.Time) error
	start(context.Context, func() error) error
	progress(context.Context, time.Time, time.Time) (string, error)
	close() error
}

// Role and approval hashes prevent replay through another process adapter.
// No monetary budget, signed transaction or nonce can be created by this record.
type repairProcessRecord struct {
	Schema             string                     `json:"schema"`
	ApprovalHash       string                     `json:"approval_hash"`
	PublicKey          string                     `json:"independent_public_key"`
	IncidentId         string                     `json:"incident_id"`
	HighWaterAt        time.Time                  `json:"high_water_at"`
	Observations       uint32                     `json:"observations"`
	StartAt            time.Time                  `json:"start_consumed_at"`
	StartMonotonicUsec uint64                     `json:"start_consumed_monotonic_usec"`
	Generation         *repairValidatorGeneration `json:"acknowledged_generation,omitempty"`
	CompletedAt        time.Time                  `json:"completed_at"`
	PostconditionHash  string                     `json:"postcondition_hash,omitempty"`
	Status             string                     `json:"status"`
	ContentHash        string                     `json:"content_hash"`
}

// This owner remains bound to the supplied original signature and permanent
// generation marker. Missing state never becomes a new claim.
type repairProcessStore struct {
	*repairValidatorFileOwner
	envelope repairProcessEnvelope
	key      string
}

// A checksum protects local continuity; independent authority is the envelope.
func (self repairProcessRecord) validate(envelope repairProcessEnvelope, key string) error {
	hash := self.ContentHash
	self.ContentHash = ""
	p := envelope.profile()
	if self.Schema != envelope.schema() || self.ApprovalHash != envelope.approvalHash() || self.PublicKey != key || self.IncidentId != p.IncidentId || hash != rootObjectHash(self) || self.HighWaterAt.IsZero() || self.Observations > p.MaximumObservations || self.StartAt.IsZero() != (self.StartMonotonicUsec == 0) || self.StartAt.After(self.HighWaterAt) || self.StartAt.IsZero() && (self.Generation != nil || !self.CompletedAt.IsZero()) || !self.StartAt.IsZero() && self.Observations == 0 {
		return errors.Join(errRpcIntegrity, errors.New("process repair journal authority or consumed allowance differs"))
	}
	if g := self.Generation; g != nil && (!repairValidatorHex(g.InvocationId, 16) || g.InvocationId == p.Previous.InvocationId || g.Pid <= 1 || g.StartedUsec <= p.Previous.StartedUsec || g.StartedUsec < self.StartMonotonicUsec) {
		return errors.Join(errRpcIntegrity, errors.New("process repair acknowledged generation differs"))
	}
	if !self.CompletedAt.IsZero() && (self.Generation == nil || self.CompletedAt.Before(self.StartAt) || self.CompletedAt.After(self.HighWaterAt) || !planSha256(self.PostconditionHash)) || self.CompletedAt.IsZero() && self.PostconditionHash != "" || (!self.CompletedAt.IsZero()) != (self.Status == "resumed-generation-observed") {
		return errors.Join(errRpcIntegrity, errors.New("process repair completion lost its original observation"))
	}
	switch self.Status {
	case "claimed", "observation-reserved", "start-consumed", "uncertain-consumed-start", "waiting-progress", "resumed-generation-observed", "approval-window-closed", "observation-limit", "generation-changed", "source-refused", "clock-rollback":
		return nil
	default:
		return errors.Join(errRpcIntegrity, errors.New("process repair journal disposition is unknown"))
	}
}

// The exact path is signed; the separate generation marker excludes alternate
// signed journals for the same boot and previous process invocation.
func openRepairProcessStore(ctx context.Context, envelope repairProcessEnvelope, key string, create bool, now time.Time) (*repairProcessStore, error) {
	if now.IsZero() || envelope == nil {
		return nil, errors.New("process repair store authority or clock is absent")
	}
	if err := envelope.validate(key); err != nil {
		return nil, err
	}
	owner, err := openRepairValidatorFileOwner(ctx, envelope.profile().StatePath, envelope.schema()+" "+envelope.approvalHash()+" "+key+"\n", create)
	if err != nil {
		return nil, err
	}
	self := &repairProcessStore{repairValidatorFileOwner: owner, envelope: envelope, key: key}
	if create {
		err = self.save(repairProcessRecord{Schema: envelope.schema(), ApprovalHash: envelope.approvalHash(), PublicKey: key, IncidentId: envelope.profile().IncidentId, HighWaterAt: now, Status: "claimed"})
	} else {
		_, err = self.load(ctx)
	}
	if err != nil {
		return nil, errors.Join(err, self.close())
	}
	return self, nil
}

// Both the named marker and the last observed journal remain physically owned.
func (self *repairProcessStore) load(ctx context.Context) (repairProcessRecord, error) {
	var record repairProcessRecord
	if err := self.validateOwner(); err != nil {
		return record, err
	}
	raw, _, err := self.storage.readFile(ctx, self.path, 64*1024)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, errors.Join(errRpcIntegrity, err)
	}
	if err := record.validate(self.envelope, self.key); err != nil {
		return record, err
	}
	self.expectedHash = monitorReadDigest(raw)
	return record, self.validateOwner()
}

// Ambiguous publication cannot refund an effect reservation or remain writable.
func (self *repairProcessStore) save(record repairProcessRecord) error {
	if err := self.validateOwner(); err != nil {
		return err
	}
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(self.envelope, self.key); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) >= 64*1024 {
		return errors.Join(errors.New("process repair journal exceeds its bound"), err)
	}
	raw = append(raw, '\n')
	if err := self.storage.publish(self.path, raw, self.syncDirectory); err != nil {
		if mainnetDurableAdmissionPending(err) {
			return err
		}
		self.poisoned = errors.Join(errMainnetDurablePublicationUncertain, err)
		return self.poisoned
	}
	self.expectedHash = monitorReadDigest(raw)
	return self.validateOwner()
}

// Sharing the existing per-generation name excludes active-validator adapters
// too; the marker itself retains the distinct role signature domain.
func repairProcessClaimPath(plan repairValidatorPlan) string {
	return plan.Unit.File.Path + ".sn-active-" + strings.ReplaceAll(plan.BootId, "-", "") + "-" + plan.Previous.InvocationId + ".claim"
}

// Two exclusive permanent names conservatively retain partial creation. Neither
// a new approval, another state path nor deletion of one name renews permission.
func repairProcessClaim(ctx context.Context, host *repairValidatorHost, envelope repairProcessEnvelope, key string, create bool) error {
	path := repairProcessClaimPath(envelope.profile())
	marker := []byte(envelope.schema() + " " + envelope.approvalHash() + " " + key + "\n")
	if err := host.parents(path, host.rootUid); err != nil {
		return err
	}
	if create {
		for _, name := range []string{path + ".lock", path} {
			if err := ctx.Err(); err != nil {
				return err
			}
			fd, err := syscall.Open(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
			if err != nil {
				return errors.Join(errRepairProcessHeld, errors.New("process generation is already claimed or unavailable"), err)
			}
			file := os.NewFile(uintptr(fd), name)
			count, writeErr := file.Write(marker)
			if writeErr == nil && count != len(marker) {
				writeErr = io.ErrShortWrite
			}
			if err := errors.Join(writeErr, file.Sync(), file.Close(), ctx.Err()); err != nil {
				return errors.Join(errRepairProcessHeld, err)
			}
		}
		directory, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		if err := errors.Join(directory.Sync(), directory.Close(), ctx.Err()); err != nil {
			return err
		}
	}
	for _, name := range []string{path, path + ".lock"} {
		raw, err := host.read(ctx, name, host.rootUid, 512, true)
		if err != nil {
			return repairValidatorObservationError("cannot read retained process generation claim", err, true)
		}
		if string(raw) != string(marker) {
			return errors.Join(errRpcIntegrity, errors.New("process generation belongs to another repair approval"))
		}
	}
	return nil
}

// A live or replaced process never grants stopped-process authority. The caller
// owns the unit lock when this check precedes an actual claim or start.
func repairProcessReady(ctx context.Context, envelope repairProcessEnvelope, host *repairValidatorHost, custody repairProcessCustody, now time.Time) error {
	p := envelope.profile()
	if err := ctx.Err(); err != nil {
		return err
	}
	if now.IsZero() || now.Before(p.ValidFrom) {
		return errRepairProcessPending
	}
	if !now.Before(p.ExpiresAt) {
		return errors.Join(errRepairProcessHeld, errors.New("process repair approval expired"))
	}
	if err := custody.check(ctx); err != nil {
		return err
	}
	manager, err := custody.inspect(ctx)
	if err != nil {
		return err
	}
	if err := host.stopped(ctx, p, manager); err != nil {
		return err
	}
	return custody.beforeStart(ctx, now)
}

// Claiming never starts a service. Generation custody is consumed before the
// incident journal is created, leaving an interrupted claim held for review.
func claimRepairProcess(ctx context.Context, envelope repairProcessEnvelope, key string, host *repairValidatorHost, now func() time.Time) (resultErr error) {
	if ctx == nil || envelope == nil || host == nil || now == nil {
		return errors.New("process repair claim owner is absent")
	}
	if err := envelope.validate(key); err != nil {
		return err
	}
	p := envelope.profile()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.CommandTimeoutSeconds)*time.Second)
	defer cancel()
	control, err := host.control(ctx, p.Unit)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, control.close()) }()
	custody, err := envelope.open(ctx, host)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, custody.close()) }()
	stamp := now()
	if err := repairProcessReady(ctx, envelope, host, custody, stamp); err != nil {
		return err
	}
	if current := now(); current.Before(stamp) || !current.Before(p.ExpiresAt) {
		return errRepairProcessHeld
	}
	if err := errors.Join(control.validate(), custody.check(ctx)); err != nil {
		return err
	}
	if err := repairProcessClaim(ctx, host, envelope, key, true); err != nil {
		return err
	}
	store, err := openRepairProcessStore(ctx, envelope, key, true, stamp)
	if err != nil {
		return err
	}
	return store.close()
}

// Each invocation has one finite read owner. Only unfinished observations may
// repeat; no path adopts an unacknowledged start or launches a second process.
func resumeRepairProcess(ctx context.Context, store *repairProcessStore, host *repairValidatorHost, now func() time.Time) (status string, completed bool, resultErr error) {
	if ctx == nil || store == nil || host == nil || now == nil {
		return "", false, errors.New("process repair resume owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	record, err := store.load(ctx)
	if err != nil {
		return "", false, err
	}
	finish := func(status string, cause error) (string, bool, error) {
		record.Status = status
		return status, !record.CompletedAt.IsZero(), errors.Join(cause, store.save(record))
	}
	if !record.CompletedAt.IsZero() {
		return record.Status, true, nil
	}
	if !record.StartAt.IsZero() && record.Generation == nil {
		return finish("uncertain-consumed-start", errRepairProcessHeld)
	}
	p := store.envelope.profile()
	owner, cancel := context.WithTimeout(ctx, time.Duration(p.CommandTimeoutSeconds)*time.Second)
	defer cancel()
	stamp := now()
	if stamp.IsZero() || stamp.Before(record.HighWaterAt) {
		return finish("clock-rollback", errors.Join(errRepairProcessHeld, errors.New("process repair clock moved backwards")))
	}
	record.HighWaterAt = stamp
	if record.StartAt.IsZero() && (stamp.Before(p.ValidFrom) || !stamp.Before(p.ExpiresAt)) {
		return finish("approval-window-closed", errRepairProcessHeld)
	}
	if record.Observations >= p.MaximumObservations {
		return finish("observation-limit", errRepairProcessHeld)
	}
	record.Observations++
	record.Status = "observation-reserved"
	if err := store.save(record); err != nil {
		return record.Status, false, err
	}
	control, err := host.control(owner, p.Unit)
	if err != nil {
		return finish("source-refused", err)
	}
	defer func() { resultErr = errors.Join(resultErr, control.close()) }()
	if err := repairProcessClaim(owner, host, store.envelope, store.key, false); err != nil {
		return finish("source-refused", err)
	}
	custody, err := store.envelope.open(owner, host)
	if err != nil {
		return finish("source-refused", err)
	}
	defer func() { resultErr = errors.Join(resultErr, custody.close()) }()
	if record.StartAt.IsZero() {
		if err := repairProcessReady(owner, store.envelope, host, custody, now()); err != nil {
			return finish("source-refused", err)
		}
		stamp = now()
		if stamp.Before(record.HighWaterAt) || !stamp.Before(p.ExpiresAt) || owner.Err() != nil {
			return finish("approval-window-closed", errors.Join(errRepairProcessHeld, owner.Err()))
		}
		monotonic, err := host.monotonic()
		if err != nil {
			return finish("source-refused", repairValidatorObservationError("cannot read process repair monotonic clock", err, false))
		}
		if monotonic <= p.Previous.StartedUsec {
			return finish("source-refused", errors.Join(errRepairProcessHeld, errors.New("process repair monotonic clock differs")))
		}
		record.HighWaterAt, record.StartAt, record.StartMonotonicUsec, record.Status = stamp, stamp, monotonic, "start-consumed"
		if err := store.save(record); err != nil {
			return record.Status, false, err
		}
		actionAt := now()
		if actionAt.Before(stamp) || !actionAt.Before(p.ExpiresAt) || owner.Err() != nil {
			return finish("uncertain-consumed-start", errors.Join(errRepairProcessHeld, owner.Err()))
		}
		record.HighWaterAt = actionAt
		if err := errors.Join(control.validate(), store.validateOwner(), custody.check(owner), custody.beforeStart(owner, actionAt)); err != nil {
			return finish("uncertain-consumed-start", err)
		}
		if err := custody.start(owner, func() error {
			dispatchAt := now()
			if dispatchAt.Before(record.HighWaterAt) || dispatchAt.Before(p.ValidFrom) || !dispatchAt.Before(p.ExpiresAt) || owner.Err() != nil {
				return errors.Join(errRepairProcessHeld, errors.New("process repair authority window closed before start dispatch"), owner.Err())
			}
			record.HighWaterAt = dispatchAt
			return nil
		}); err != nil {
			return finish("uncertain-consumed-start", err)
		}
		manager, err := custody.inspect(owner)
		if err != nil {
			return finish("uncertain-consumed-start", err)
		}
		if !repairValidatorRunning(p, manager, manager.Generation) || manager.Generation.StartedUsec < record.StartMonotonicUsec {
			return finish("uncertain-consumed-start", errors.Join(errRepairProcessHeld, errors.New("process repair start has no attributable generation")))
		}
		generation := manager.Generation
		record.Generation, record.Status = &generation, "waiting-progress"
		if err := store.save(record); err != nil {
			return record.Status, false, err
		}
	}
	manager, err := custody.inspect(owner)
	if err != nil {
		return finish("source-refused", err)
	}
	if !repairValidatorRunning(p, manager, *record.Generation) {
		return finish("generation-changed", errRepairProcessHeld)
	}
	stamp = now()
	if stamp.Before(record.HighWaterAt) {
		return finish("clock-rollback", errRepairProcessHeld)
	}
	record.HighWaterAt = stamp
	hash, err := custody.progress(owner, record.StartAt, stamp)
	if err != nil {
		return finish("waiting-progress", err)
	}
	manager, err = custody.inspect(owner)
	if err != nil {
		return finish("source-refused", err)
	}
	if !repairValidatorRunning(p, manager, *record.Generation) {
		return finish("generation-changed", errRepairProcessHeld)
	}
	if err := errors.Join(custody.check(owner), control.validate(), owner.Err()); err != nil {
		return finish("source-refused", err)
	}
	record.CompletedAt, record.PostconditionHash = stamp, hash
	return finish("resumed-generation-observed", nil)
}

// Permanent initial-start claims exclude alternate signed activation journals.
// Local ownership depends on the separately signed privileged custody policy.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// A fixed name is independent of release, boot, approval and journal path.
func validatorActivationFirstStartPath(unit repairValidatorUnit) string {
	return unit.File.Path + ".sn-first-start.claim"
}

// A transient file read is not evidence of startup absence. Fresh manager
// admission and the original protected-parent reader precede this final check.
func (self *validatorActivationHost) absentCurrentProgress(ctx context.Context, unit repairValidatorUnit) error {
	if ctx == nil || ctx.Err() != nil {
		return errors.New("validator prestart progress observation is unavailable")
	}
	if _, err := os.Lstat(unit.ProgressFile); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(errors.New("validator initial current admission requires genuinely absent prestart progress"), err)
	}
	return ctx.Err()
}

// Callers hold the common unit control lock. Two exclusive permanent artifacts
// make either single loss a refusal; partial creation is never repaired.
func (self *validatorActivationHost) firstStartClaim(ctx context.Context, record validatorActivationRecord, index int, create bool) error {
	if ctx == nil || ctx.Err() != nil || self == nil || self.host == nil || index < 0 || index >= len(record.Units) || record.CurrentAuthority == nil {
		return errors.New("validator first-start claim lacks its exact current owner")
	}
	path := validatorActivationFirstStartPath(record.Approval.Plan.Units[index].Unit)
	marker := []byte(validatorActivationCurrentSchema + " " + rootObjectHash(record.Approval) + " " + record.PublicKey + " " + rootObjectHash(*record.CurrentAuthority) + "\n")
	if err := self.host.parents(path, self.host.rootUid); err != nil {
		return err
	}
	if create {
		for _, name := range []string{path + ".lock", path} {
			fd, err := syscall.Open(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
			if err != nil {
				return errors.Join(errors.New("validator first start already has permanent custody or unavailable evidence"), err)
			}
			file := os.NewFile(uintptr(fd), name)
			_, writeErr := file.Write(marker)
			if err := errors.Join(writeErr, file.Sync(), file.Close(), ctx.Err()); err != nil {
				return err
			}
		}
		directory, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
			return err
		}
	}
	for _, name := range []string{path, path + ".lock"} {
		raw, err := self.host.read(ctx, name, self.host.rootUid, 512, true)
		if err != nil || string(raw) != string(marker) {
			return errors.Join(errors.New("validator first-start claim differs or is missing"), err)
		}
	}
	return ctx.Err()
}

// Only the synchronous post-reservation reader may observe the still-stopped
// target as prestart. The original consumed record remains unchanged and synced.
// Units is a fixed two-element array: this value copy owns both unit structs.
func validatorActivationPreStartRecord(record validatorActivationRecord, pending int) validatorActivationRecord {
	if validatorActivationCurrentPending(record, pending) {
		unit := &record.Units[pending]
		unit.StartAt, unit.StartMonotonicUsec, unit.Readiness, unit.Status = time.Time{}, 0, nil, "installed"
		unit.CurrentAuthorityHash, unit.RecheckedReadinessHash = "", ""
	}
	return record
}

// Only the newly synced reservation in this synchronous operation can borrow
// the stopped view. Command reopen refuses all unacknowledged consumed starts.
func validatorActivationCurrentPending(record validatorActivationRecord, pending int) bool {
	if pending < 0 || pending >= len(record.Units) || record.CurrentAuthority == nil || record.Status != "operation-reserved" {
		return false
	}
	unit := record.Units[pending]
	return unit.Status == "start-consumed" && !unit.StartAt.IsZero() && unit.StartMonotonicUsec != 0 && unit.Generation == nil && unit.Completed == nil &&
		!unit.StartAt.After(record.HighWaterAt) && unit.RecheckedReadinessHash == "" && unit.CurrentAuthorityHash == rootObjectHash(record.CurrentAuthority.Approval) &&
		unit.Readiness != nil && unit.Readiness.Current != nil && unit.Readiness.Current.AuthorityHash == unit.CurrentAuthorityHash && unit.Readiness.PlanHash == record.Approval.Plan.PlanHash
}

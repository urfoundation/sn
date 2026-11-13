// Actual protected files and public repair commands distinguish a failed read
// from positive replacement without replenishing any consumed action allowance.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Read the actual retained journal at a deterministic manager-read boundary.
func repairObservationTestRecord(t *testing.T, path string, record any) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, record); err != nil {
		t.Fatal(err)
	}
	return raw
}

// Dependency and active-stop-profile queries are separate from the generation.
func repairObservationTestManager(args []string, unit string) bool {
	if len(args) == 0 || args[len(args)-1] != unit {
		return false
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--property=") && strings.Contains(arg, ",InvocationID,") {
			return true
		}
	}
	return false
}

// Every failed descriptor/name/byte observation preserves the original journal
// and admits the same owner once the observation is available again.
func TestRepairValidatorOwnerReadErrorsDoNotPoison(t *testing.T) {
	f := newRepairValidatorFixture(t)
	f.claim()
	operation := ""
	var injected error
	observed := 0
	ctx := context.WithValue(f.storage.Context, repairValidatorObservationKey{}, func(step string) error {
		if step == operation {
			observed++
			return injected
		}
		return nil
	})
	store, err := openRepairValidatorStore(ctx, f.approval, f.publicKey, false, f.now)
	if err != nil {
		t.Fatal("valid retained owner was refused", err)
	}
	t.Cleanup(func() {
		if err := store.close(); err != nil {
			t.Error(err)
		}
	})
	var record repairValidatorRecord
	original := repairObservationTestRecord(t, f.approval.Plan.StatePath, &record)
	for _, step := range []string{"owner-parent-stat", "owner-open-stat", "owner-name-stat", "owner-marker-read", "owner-journal-read"} {
		for _, cause := range []error{syscall.EIO, syscall.EMFILE} {
			operation, injected, observed = step, cause, 0
			err := store.validateOwner()
			if observed != 1 || !errors.Is(err, cause) || !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrIdentity) {
				t.Fatal("failed observation claimed changed custody or lost cause", step, observed, err)
			}
			operation, injected = "", nil
			if err := store.validateOwner(); err != nil {
				t.Fatal("unchanged owner stayed poisoned after observation recovered", step, err)
			}
		}
	}
	if current := repairObservationTestRecord(t, f.approval.Plan.StatePath, &record); !bytes.Equal(current, original) || !record.StartAt.IsZero() || f.starts != 0 {
		t.Fatal("failed reads changed original authority or consumed a start")
	}
}

// Cancellation after actual bytes were read admits no marker/journal result;
// closing and reopening the same original authority does not reset its budget.
func TestRepairValidatorOwnerReadCancellationRetainsOriginal(t *testing.T) {
	f := newRepairValidatorFixture(t)
	f.claim()
	base, cancel := context.WithCancel(f.storage.Context)
	defer cancel()
	armed := false
	ctx := context.WithValue(base, repairValidatorObservationKey{}, func(step string) error {
		if armed && step == "owner-marker-read" {
			cancel()
		}
		return nil
	})
	store, err := openRepairValidatorStore(ctx, f.approval, f.publicKey, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	armed = true
	err = store.validateOwner()
	if !errors.Is(err, context.Canceled) || errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("canceled read invented identity loss", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if result, exit, detail := f.command("status"); exit != 0 || result.StartConsumed || result.Observations != 0 {
		t.Fatal("canceled inspection lost the original allowance", result, exit, detail)
	}
}

// A missing/replaced actual inode stays lost even if the old name is restored.
func TestRepairValidatorOwnerReplacementRemainsSticky(t *testing.T) {
	f := newRepairValidatorFixture(t)
	f.claim()
	store, err := openRepairValidatorStore(f.storage.Context, f.approval, f.publicKey, false, f.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.close(); err != nil {
			t.Error(err)
		}
	})
	path := f.approval.Plan.StatePath + ".lock"
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := store.validateOwner(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("missing original custody marker was admitted", err)
	}
	if err := os.Rename(path+".original", path); err != nil {
		t.Fatal(err)
	}
	if err := store.validateOwner(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("restoring a name readmitted the failed owner", err)
	}
}

// The shared per-unit controller uses the same observation taxonomy and retains
// all real flock/name/marker checks while injected read errors come and go.
func TestRepairValidatorControlReadErrorsRecover(t *testing.T) {
	f := newRepairValidatorFixture(t)
	operation := ""
	ctx := context.WithValue(f.storage.Context, repairValidatorObservationKey{}, func(step string) error {
		if step == operation {
			return syscall.EIO
		}
		return nil
	})
	control, err := f.host.control(ctx, f.approval.Plan.Unit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := control.close(); err != nil {
			t.Error(err)
		}
	})
	for _, step := range []string{"control-open-stat", "control-name-stat", "control-marker-read"} {
		operation = step
		if err := control.validate(); !errors.Is(err, syscall.EIO) || !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("control observation error invented replacement", step, err)
		}
		operation = ""
		if err := control.validate(); err != nil {
			t.Fatal("control read failure poisoned unchanged custody", step, err)
		}
	}
}

// Equal bytes in a different real inode cannot borrow the retained control fd.
func TestRepairValidatorControlReplacementCannotBeUndoneInOwner(t *testing.T) {
	f := newRepairValidatorFixture(t)
	control, err := f.host.control(f.storage.Context, f.approval.Plan.Unit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := control.close(); err != nil {
			t.Error(err)
		}
	})
	if err := os.Rename(control.path, control.path+".original"); err != nil {
		t.Fatal(err)
	}
	repairValidatorTestWrite(t, control.path, []byte(control.marker), 0600)
	if err := control.validate(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("equal replacement marker borrowed original control", err)
	}
	if err := os.Remove(control.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(control.path+".original", control.path); err != nil {
		t.Fatal(err)
	}
	if err := control.validate(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("restored name erased confirmed control loss", err)
	}
}

// Closed descriptor misuse must retain os.ErrClosed without asserting a
// filesystem replacement; nil and normally closed owners also refuse safely.
func TestRepairValidatorClosedControlDoesNotClaimIdentityLoss(t *testing.T) {
	f := newRepairValidatorFixture(t)
	control, err := f.host.control(f.storage.Context, f.approval.Plan.Unit)
	if err != nil {
		t.Fatal(err)
	}
	if err := control.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := control.validate(); !errors.Is(err, os.ErrClosed) || errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("closed caller descriptor claimed a replacement", err)
	}
	if err := control.close(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed descriptor cleanup lost original cause", err)
	}
	for _, owner := range []*repairValidatorControl{nil, control} {
		if err := owner.validate(); err == nil || errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("unavailable caller owner claimed identity loss", err)
		}
	}
}

// Public postcondition readback can become unavailable after a real acknowledged
// start. Recovery observes that same generation without a second start.
func TestRepairValidatorPublicReadbackUnavailableRetainsAcknowledgment(t *testing.T) {
	f := newRepairValidatorFixture(t)
	f.claim()
	original := f.host.execute
	faults := 0
	f.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		if repairObservationTestManager(args, f.approval.Plan.Unit.Name) {
			var record repairValidatorRecord
			repairObservationTestRecord(t, f.approval.Plan.StatePath, &record)
			if record.Generation != nil && faults == 0 {
				faults++
				return nil, syscall.EIO
			}
		}
		return original(ctx, path, args)
	}
	result, exit, detail := f.command("resume")
	if faults != 1 || exit == 0 || result.Status != "source-refused" || !result.StartConsumed || result.Generation == nil || result.Completed != nil || f.starts != 1 || !strings.Contains(detail, syscall.EIO.Error()) {
		t.Fatal("failed readback claimed a changed generation or another effect", result, exit, detail, faults, f.starts)
	}
	var record repairValidatorRecord
	repairObservationTestRecord(t, f.approval.Plan.StatePath, &record)
	if record.Status != "source-refused" || rootObjectHash(record.Approval) != rootObjectHash(f.approval) || record.Generation == nil {
		t.Fatal("readback failure did not retain original signed authority and generation", record)
	}
	result, exit, detail = f.command("resume")
	if exit != 0 || result.Completed == nil || f.starts != 1 || result.Observations != 2 {
		t.Fatal("same generation could not continue after observation recovered", result, exit, detail, f.starts)
	}
}

// A consumed stop reservation cannot be refunded by unavailable readback, even
// if no actual stop was reached. Later resume may observe but cannot issue stop.
func TestRepairActiveValidatorPublicReservedReadFailureHasNoNewEffect(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	original := f.host.execute
	faults := 0
	f.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		if repairObservationTestManager(args, f.active.Plan.Process.Unit.Name) {
			var record repairActiveValidatorRecord
			repairObservationTestRecord(t, f.active.Plan.Process.StatePath, &record)
			if !record.StopAt.IsZero() && f.stops == 0 && faults == 0 {
				faults++
				return nil, syscall.EIO
			}
		}
		return original(ctx, path, args)
	}
	result, exit, detail := f.commandActive("resume")
	if faults != 1 || exit == 0 || result.Status != "source-refused" || !result.StopConsumed || result.StartConsumed || f.stops != 0 || f.starts != 0 || !strings.Contains(detail, syscall.EIO.Error()) {
		t.Fatal("stop reservation read failure invented generation loss or an effect", result, exit, detail, faults)
	}
	result, exit, detail = f.commandActive("resume")
	if exit == 0 || !result.StopConsumed || result.StartConsumed || result.Completed != nil || f.stops != 0 || f.starts != 0 {
		t.Fatal("resuming observation repeated a consumed stop", result, exit, detail)
	}
}

// The active repair's final generation read uses the same retained disposition.
func TestRepairActiveValidatorPublicReadbackUnavailableRetainsAcknowledgment(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	original := f.host.execute
	faults := 0
	f.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		if repairObservationTestManager(args, f.active.Plan.Process.Unit.Name) {
			var record repairActiveValidatorRecord
			repairObservationTestRecord(t, f.active.Plan.Process.StatePath, &record)
			if record.Generation != nil && faults == 0 {
				faults++
				return nil, syscall.EIO
			}
		}
		return original(ctx, path, args)
	}
	result, exit, detail := f.commandActive("resume")
	if faults != 1 || exit == 0 || result.Status != "source-refused" || !result.StopConsumed || !result.StartConsumed || result.Generation == nil || result.Completed != nil || f.stops != 1 || f.starts != 1 {
		t.Fatal("active readback failure lost acknowledgment or invented generation loss", result, exit, detail, faults)
	}
	result, exit, detail = f.commandActive("resume")
	if exit != 0 || result.Completed == nil || f.stops != 1 || f.starts != 1 || result.Observations != 2 {
		t.Fatal("active same-generation observation issued another action", result, exit, detail)
	}
}

// Caller cancellation at the final manager read cannot admit a completion or
// consume another start; reopening with a live caller retains the first start.
func TestRepairValidatorPublicCanceledReadbackRetainsOriginal(t *testing.T) {
	f := newRepairValidatorFixture(t)
	f.claim()
	base := f.storage.Context
	ctx, cancel := context.WithCancel(base)
	defer cancel()
	f.storage.Context = ctx
	original := f.host.execute
	faults := 0
	f.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		if repairObservationTestManager(args, f.approval.Plan.Unit.Name) {
			var record repairValidatorRecord
			repairObservationTestRecord(t, f.approval.Plan.StatePath, &record)
			if record.Generation != nil && faults == 0 {
				faults++
				cancel()
				return nil, ctx.Err()
			}
		}
		return original(ctx, path, args)
	}
	result, exit, detail := f.command("resume")
	if faults != 1 || exit == 0 || result.Status != "source-refused" || result.Completed != nil || !result.StartConsumed || f.starts != 1 || !strings.Contains(detail, context.Canceled.Error()) {
		t.Fatal("canceled readback was reported as changed generation", result, exit, detail, faults)
	}
	f.storage.Context = base
	result, exit, detail = f.command("resume")
	if exit != 0 || result.Completed == nil || f.starts != 1 {
		t.Fatal("reopened canceled readback lost its original generation", result, exit, detail)
	}
}

// A positively returned different invocation remains a refusal after the read
// error correction. No postcondition or extra start is accepted.
func TestRepairValidatorPublicReturnedGenerationConflictStillRefuses(t *testing.T) {
	f := newRepairValidatorFixture(t)
	f.claim()
	original := f.host.execute
	conflicts := 0
	f.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		if repairObservationTestManager(args, f.approval.Plan.Unit.Name) {
			var record repairValidatorRecord
			repairObservationTestRecord(t, f.approval.Plan.StatePath, &record)
			if record.Generation != nil {
				conflicts++
				f.manager["InvocationID"] = strings.Repeat("9", 32)
			}
		}
		return original(ctx, path, args)
	}
	result, exit, detail := f.command("resume")
	if conflicts != 1 || exit == 0 || result.Status != "generation-changed" || result.Completed != nil || result.Generation == nil || f.starts != 1 {
		t.Fatal("returned generation conflict was weakened", result, exit, detail, conflicts)
	}
}

// Adjacent boot, machine, cgroup and prerequisite readers preserve failure
// causes, rather than manufacturing positive host/dependency contradictions.
func TestRepairValidatorHostReadErrorsKeepObservationCauses(t *testing.T) {
	f := newRepairValidatorFixture(t)
	for _, operation := range []string{"host-read:" + f.host.machinePath, "host-boot-read", "host-pin-stat", "host-cgroup-read"} {
		observed := 0
		ctx := context.WithValue(f.storage.Context, repairValidatorObservationKey{}, func(step string) error {
			if step == operation {
				observed++
				return syscall.EIO
			}
			return nil
		})
		manager, err := f.host.inspect(ctx, f.approval.Plan)
		if err == nil {
			err = f.host.stopped(ctx, f.approval.Plan, manager)
		}
		if observed != 1 || !errors.Is(err, syscall.EIO) || !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("failed host observation claimed a contradiction", operation, observed, err)
		}
	}
	original := f.host.execute
	f.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		if len(args) > 0 && args[len(args)-1] == "-.mount" {
			return nil, syscall.EMFILE
		}
		return original(ctx, path, args)
	}
	if _, err := f.host.inspect(f.storage.Context, f.approval.Plan); !errors.Is(err, syscall.EMFILE) || !errors.Is(err, durablevolume.ErrUnavailable) {
		t.Fatal("failed prerequisite read claimed an inactive mount", err)
	}
	f.host.execute = original
	manager, err := f.host.inspect(f.storage.Context, f.approval.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.host.stopped(f.storage.Context, f.approval.Plan, manager); err != nil || f.starts != 0 {
		t.Fatal("unchanged host could not be observed after recovery", err)
	}
}

// The stopped public path refuses unavailable cgroup evidence before consuming
// a start. Restoring the same reader permits the one originally approved start.
func TestRepairValidatorPublicCgroupReadFailurePreservesStart(t *testing.T) {
	f := newRepairValidatorFixture(t)
	f.claim()
	original := f.host.cgroupType
	f.host.cgroupType = func(string) (int64, error) { return 0, syscall.EIO }
	result, exit, detail := f.command("resume")
	if exit == 0 || result.Status != "source-refused" || result.StartConsumed || f.starts != 0 || !strings.Contains(detail, syscall.EIO.Error()) {
		t.Fatal("failed cgroup read invented a changed generation or consumed start", result, exit, detail)
	}
	f.host.cgroupType = original
	result, exit, detail = f.command("resume")
	if exit != 0 || result.Completed == nil || f.starts != 1 || result.Observations != 2 {
		t.Fatal("same approved stopped generation could not recover", result, exit, detail)
	}
}

// Active repair's original policy, monitor and permanent claims retain their
// exact bytes when observation is unavailable; recovery grants no new envelope.
func TestRepairActiveValidatorRetainedReadErrorsPreserveAuthority(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	p := f.active.Plan.Process
	for _, path := range []string{f.active.Plan.MonitorServices.Path, p.MonitorCheckpoint, f.active.Plan.claimPath(), f.active.Plan.claimPath() + ".lock"} {
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		observed := 0
		ctx := context.WithValue(f.storage.Context, repairValidatorObservationKey{}, func(step string) error {
			if step == "host-read:"+path {
				observed++
				return syscall.EIO
			}
			return nil
		})
		err = f.host.activeClaim(ctx, f.active, f.publicKey, false)
		if err == nil {
			err = f.host.activeIncident(ctx, f.active.Plan, f.now, true)
		}
		if observed != 1 || !errors.Is(err, syscall.EIO) || !errors.Is(err, durablevolume.ErrUnavailable) || errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("unavailable active authority read invented a mismatch", path, observed, err)
		}
		current, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(current, original) || f.stops != 0 || f.starts != 0 {
			t.Fatal("failed read changed original active authority", path, err)
		}
	}
	result, exit, detail := f.commandActive("resume")
	if exit != 0 || result.Completed == nil || f.stops != 1 || f.starts != 1 || result.Observations != 1 {
		t.Fatal("unchanged active repair authority did not recover", result, exit, detail)
	}
}

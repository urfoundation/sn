package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The barrier observes the actual durable intent, never an inferred call time.
func repairStartWindowReserved(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var record struct {
		Status  string    `json:"status"`
		StartAt time.Time `json:"start_consumed_at"`
		Units   []struct {
			Status  string    `json:"status"`
			StartAt time.Time `json:"start_consumed_at"`
		} `json:"units"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return false, err
	}
	if record.Status == "start-consumed" && !record.StartAt.IsZero() {
		return true, nil
	}
	for _, unit := range record.Units {
		if unit.Status == "start-consumed" && !unit.StartAt.IsZero() {
			return true, nil
		}
	}
	return false, nil
}

// Delegate the real fixture inspector first, including its credential and
// volume checks. The selected completed inspection then advances a fake clock.
func repairStartWindowInspection(t *testing.T, host *repairValidatorHost, path string, selected int, change func() error) *int {
	t.Helper()
	inspect := host.storageCommand
	if inspect == nil {
		t.Fatal("fixture has no owned storage transport")
	}
	observed := new(int)
	host.storageCommand = func(ctx context.Context, command *exec.Cmd) error {
		if err := inspect(ctx, command); err != nil {
			return err
		}
		reserved, err := repairStartWindowReserved(path)
		if err != nil {
			return err
		}
		if reserved {
			(*observed)++
			if *observed == selected {
				return change()
			}
		}
		return nil
	}
	return observed
}

// Operator beforeStart inspects once after reservation; its start adapter
// independently inspects again. Expiry in that last read must still refuse.
func TestRepairOperatorStartWindowAfterStorageInspection(t *testing.T) {
	for _, change := range []string{"unchanged", "expiry", "rollback"} {
		f := newRepairOperatorFixture(t)
		f.claim()
		originalAt := f.base.now
		original, err := os.ReadFile(f.envelope.approval.Plan.OriginalCensus.Path)
		if err != nil {
			t.Fatal(err)
		}
		before := mainnetNamespaceTest(t, f.envelope.original.Plan.StateDirectory)
		inspections := repairStartWindowInspection(t, f.base.host, f.envelope.approval.Plan.StatePath, 2, func() error {
			switch change {
			case "expiry":
				f.base.now = f.envelope.approval.Plan.ExpiresAt
			case "rollback":
				f.base.now = originalAt.Add(-time.Nanosecond)
			}
			return nil
		})
		status, complete, err := f.resume()
		if *inspections != 2 {
			t.Fatal("operator did not complete the final reserved storage inspection", change, *inspections, err)
		}
		if change == "unchanged" {
			if err != nil || !complete || status != "resumed-generation-observed" || f.base.starts != 1 {
				t.Fatal("unchanged operator clock did not dispatch one start", status, complete, err, f.base.starts)
			}
		} else if !errors.Is(err, errRepairProcessHeld) || complete || status != "uncertain-consumed-start" || f.base.starts != 0 {
			t.Fatal("late operator clock change dispatched start", change, status, complete, err, f.base.starts)
		}
		starts := f.base.starts
		f.base.now = originalAt
		reopened, completed, reopenErr := f.resume()
		if reopened != status || completed != complete || f.base.starts != starts || (change != "unchanged" && !errors.Is(reopenErr, errRepairProcessHeld)) {
			t.Fatal("operator reopen changed a consumed outcome", change, reopened, completed, reopenErr, f.base.starts)
		}
		after, readErr := os.ReadFile(f.envelope.approval.Plan.OriginalCensus.Path)
		if readErr != nil || !bytes.Equal(original, after) || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.envelope.original.Plan.StateDirectory)) {
			t.Fatal("late operator refusal changed signed attempts, journals or quota", change, readErr)
		}
	}
}

// Successful complete-census reads also occur after durable consumption. Their
// latency cannot extend authority, even though both storage inspections pass.
func TestRepairOperatorStartWindowAfterOriginalCensus(t *testing.T) {
	for _, change := range []string{"expiry", "rollback"} {
		f := newRepairOperatorFixture(t)
		f.claim()
		originalAt := f.base.now
		changed := false
		f.reader.before = func(ctx context.Context) error {
			reserved, err := repairStartWindowReserved(f.envelope.approval.Plan.StatePath)
			if err != nil {
				return err
			}
			if reserved && !changed {
				changed = true
				if change == "expiry" {
					f.base.now = f.envelope.approval.Plan.ExpiresAt
				} else {
					f.base.now = originalAt.Add(-time.Nanosecond)
				}
			}
			return ctx.Err()
		}
		status, complete, err := f.resume()
		if !changed || !errors.Is(err, errRepairProcessHeld) || complete || status != "uncertain-consumed-start" || f.base.starts != 0 {
			t.Fatal("successful original census extended operator authority", change, status, complete, err, f.base.starts)
		}
		f.base.now = originalAt
		status, complete, err = f.resume()
		if !errors.Is(err, errRepairProcessHeld) || complete || status != "uncertain-consumed-start" || f.base.starts != 0 {
			t.Fatal("census delay refunded consumed start", change, status, complete, err, f.base.starts)
		}
	}
}

// The public stopped-validator command retains the incident-age restriction
// through the final storage read as well as the signed approval interval.
func TestRepairValidatorStartWindowAfterStorageInspection(t *testing.T) {
	for _, change := range []string{"unchanged", "expiry", "rollback", "incident-age"} {
		f := newRepairValidatorFixture(t)
		f.claim()
		originalAt := f.now
		inspections := repairStartWindowInspection(t, f.host, f.approval.Plan.StatePath, 1, func() error {
			switch change {
			case "expiry":
				f.now = f.approval.Plan.ExpiresAt
			case "rollback":
				f.now = originalAt.Add(-time.Nanosecond)
			case "incident-age":
				f.now = originalAt.Add(time.Duration(f.approval.Plan.MaximumSampleAgeSeconds+1) * time.Second)
			}
			return nil
		})
		result, code, detail := f.command("resume")
		if *inspections != 1 || !result.StartConsumed {
			t.Fatal("validator did not reach the reserved inspection", change, *inspections, result, code, detail)
		}
		if change == "unchanged" {
			if code != 0 || result.Completed == nil || f.starts != 1 {
				t.Fatal("unchanged validator clock refused start", result, code, detail, f.starts)
			}
		} else if code == 0 || result.Status != "uncertain-consumed-start" || result.Generation != nil || f.starts != 0 {
			t.Fatal("late validator clock change dispatched start", change, result, code, detail, f.starts)
		}
		starts, status := f.starts, result.Status
		f.now = originalAt
		result, _, detail = f.command("resume")
		if result.Status != status || !result.StartConsumed || f.starts != starts {
			t.Fatal("validator reopen refunded consumed start", change, result, detail, f.starts)
		}
	}
}

// The active route has already consumed and joined its sole stop. Neither wall
// nor monotonic join headroom can expire during inspection and still start.
func TestRepairActiveValidatorStartWindowAfterStorageInspection(t *testing.T) {
	for _, change := range []string{"unchanged", "expiry", "rollback", "join-wall", "join-monotonic", "monotonic-rollback"} {
		f := newRepairActiveValidatorFixture(t)
		f.claimActive()
		originalAt, originalClock := f.now, f.host.monotonic
		inspections := repairStartWindowInspection(t, f.host, f.active.Plan.Process.StatePath, 1, func() error {
			switch change {
			case "expiry":
				f.now = f.active.Plan.Process.ExpiresAt
			case "rollback":
				f.now = originalAt.Add(-time.Nanosecond)
			case "join-wall":
				f.now = originalAt.Add(time.Duration(f.active.Plan.JoinWindowSeconds)*time.Second + time.Nanosecond)
			case "join-monotonic":
				f.host.monotonic = func() (uint64, error) { return 150 + uint64(f.active.Plan.JoinWindowSeconds)*1000000 + 1, nil }
			case "monotonic-rollback":
				f.host.monotonic = func() (uint64, error) { return 149, nil }
			}
			return nil
		})
		result, code, detail := f.commandActive("resume")
		if *inspections != 1 || !result.StopConsumed || !result.StartConsumed || result.JoinedAt.IsZero() || f.stops != 1 {
			t.Fatal("active repair lost its completed stop/join before inspection", change, *inspections, result, code, detail, f.stops)
		}
		if change == "unchanged" {
			if code != 0 || result.Completed == nil || f.starts != 1 {
				t.Fatal("unchanged active repair clock refused start", result, code, detail, f.starts)
			}
		} else if code == 0 || result.Status != "uncertain-consumed-start" || result.Generation != nil || f.starts != 0 {
			t.Fatal("late active repair clock change dispatched start", change, result, code, detail, f.starts)
		}
		starts, status := f.starts, result.Status
		f.now, f.host.monotonic = originalAt, originalClock
		result, _, detail = f.commandActive("resume")
		if result.Status != status || !result.StopConsumed || !result.StartConsumed || f.stops != 1 || f.starts != starts {
			t.Fatal("active repair reopen repeated a consumed action", change, result, detail, f.stops, f.starts)
		}
	}
}

// Repairing the original passive monitor uses the same final generic host
// inspection, while retaining its independent original checkpoint authority.
func TestRepairRootPassiveStartWindowAfterStorageInspection(t *testing.T) {
	for _, change := range []string{"unchanged", "expiry", "rollback"} {
		f := newRepairRootPassiveFixture(t)
		f.claim()
		originalAt := f.root.now
		inspections := repairStartWindowInspection(t, f.root.host.files.host, f.envelope.approval.Plan.StatePath, 1, func() error {
			if change == "expiry" {
				f.root.now = f.envelope.approval.Plan.ExpiresAt
			} else if change == "rollback" {
				f.root.now = originalAt.Add(-time.Nanosecond)
			}
			return nil
		})
		status, complete, err := f.resume()
		if *inspections != 1 || complete {
			t.Fatal("passive repair did not reach its reserved inspection", change, *inspections, status, complete, err)
		}
		if change == "unchanged" {
			if status != "waiting-progress" || !errors.Is(err, errRepairProcessPending) || f.root.starts != 2 {
				t.Fatal("unchanged passive repair clock refused start", status, err, f.root.starts)
			}
		} else if status != "uncertain-consumed-start" || !errors.Is(err, errRepairProcessHeld) || f.root.starts != 1 {
			t.Fatal("late passive repair clock change dispatched start", change, status, err, f.root.starts)
		}
		starts := f.root.starts
		f.root.now = originalAt
		reopened, completed, reopenErr := f.resume()
		if reopened != status || completed || f.root.starts != starts {
			t.Fatal("passive repair reopen repeated start", change, reopened, completed, reopenErr, f.root.starts)
		}
	}
}

// Initial passive activation also has a signed window and a fresh observation;
// the fixed host command must enforce both after its final storage inspection.
func TestRootPassiveHostStartWindowAfterStorageInspection(t *testing.T) {
	for _, change := range []string{"unchanged", "expiry", "rollback", "observation-age"} {
		f := newRootPassiveHostFixture(t)
		f.require("claim", "claimed")
		f.require("install", "installed")
		f.require("admit", "admitted-read-only")
		originalAt := f.now
		inspections := repairStartWindowInspection(t, f.host.files.host, f.approval.Plan.StatePath, 1, func() error {
			switch change {
			case "expiry":
				f.now = f.approval.Plan.ExpiresAt
			case "rollback":
				f.now = originalAt.Add(-time.Nanosecond)
			case "observation-age":
				f.now = originalAt.Add(time.Duration(f.approval.Plan.MaximumSampleAgeSeconds+1) * time.Second)
			}
			return nil
		})
		result, code, detail := f.command("start")
		if *inspections != 1 || !result.StartConsumed {
			t.Fatal("passive activation did not reach its reserved inspection", change, *inspections, result, code, detail)
		}
		if change == "unchanged" {
			if code != 0 || result.Status != "acknowledged-running" || result.Generation == nil || f.starts != 1 {
				t.Fatal("unchanged passive activation clock refused start", result, code, detail, f.starts)
			}
		} else if code == 0 || result.Status != "uncertain-consumed-start" || result.Generation != nil || f.starts != 0 {
			t.Fatal("late passive activation clock change dispatched start", change, result, code, detail, f.starts)
		}
		starts, status := f.starts, result.Status
		f.now = originalAt
		result, _, detail = f.command("resume")
		if result.Status != status || !result.StartConsumed || f.starts != starts {
			t.Fatal("passive activation reopen repeated start", change, result, detail, f.starts)
		}
	}
}

// An actual inspector read error remains the cause when time also changes.
// Unknown storage must not be relabeled expiry or a source-integrity conflict.
func TestRepairStartStorageFailurePrecedesClockRefusal(t *testing.T) {
	for _, change := range []string{"expiry", "rollback"} {
		operator := newRepairOperatorFixture(t)
		operator.claim()
		originalAt := operator.base.now
		inspections := repairStartWindowInspection(t, operator.base.host, operator.envelope.approval.Plan.StatePath, 2, func() error {
			if change == "expiry" {
				operator.base.now = operator.envelope.approval.Plan.ExpiresAt
			} else {
				operator.base.now = originalAt.Add(-time.Nanosecond)
			}
			return syscall.EIO
		})
		status, complete, err := operator.resume()
		if *inspections != 2 || status != "uncertain-consumed-start" || complete || operator.base.starts != 0 || !errors.Is(err, syscall.EIO) || errors.Is(err, errRpcIntegrity) || errors.Is(err, errRepairProcessHeld) || strings.Contains(err.Error(), "authority window") {
			t.Fatal("operator storage error was replaced by a clock or identity verdict", change, status, complete, err, operator.base.starts)
		}
		operator.base.now = originalAt
		if status, complete, err = operator.resume(); status != "uncertain-consumed-start" || complete || !errors.Is(err, errRepairProcessHeld) || operator.base.starts != 0 {
			t.Fatal("failed operator inspection refunded start", change, status, complete, err)
		}

		validator := newRepairValidatorFixture(t)
		validator.claim()
		originalAt = validator.now
		inspections = repairStartWindowInspection(t, validator.host, validator.approval.Plan.StatePath, 1, func() error {
			if change == "expiry" {
				validator.now = validator.approval.Plan.ExpiresAt
			} else {
				validator.now = originalAt.Add(-time.Nanosecond)
			}
			return syscall.EIO
		})
		store, err := openRepairValidatorStore(validator.storage.Context, validator.approval, validator.publicKey, false, validator.now)
		if err != nil {
			t.Fatal(err)
		}
		result, err := resumeRepairValidator(validator.storage.Context, store, validator.host, func() time.Time { return validator.now })
		closeErr := store.close()
		if *inspections != 1 || result.Status != "uncertain-consumed-start" || !result.StartConsumed || result.Generation != nil || validator.starts != 0 || !errors.Is(err, syscall.EIO) || errors.Is(err, errRpcIntegrity) || strings.Contains(err.Error(), "authority window") || closeErr != nil {
			t.Fatal("generic storage error was replaced by a clock or identity verdict", change, result, err, closeErr, validator.starts)
		}
		validator.now = originalAt
		result, _, detail := validator.command("resume")
		if result.Status != "uncertain-consumed-start" || !result.StartConsumed || result.Generation != nil || validator.starts != 0 {
			t.Fatal("failed validator inspection refunded start", change, result, detail)
		}
	}
}

// A failed final monotonic observation remains unknown even if the wall clock
// simultaneously reaches expiry; it cannot turn into a completed contradiction.
func TestRepairActiveValidatorDispatchClockReadFailureRetainsCause(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	inspections := repairStartWindowInspection(t, f.host, f.active.Plan.Process.StatePath, 1, func() error {
		f.now = f.active.Plan.Process.ExpiresAt
		f.host.monotonic = func() (uint64, error) { return 0, syscall.EIO }
		return nil
	})
	store := f.openActive()
	result, err := resumeRepairActiveValidator(f.storage.Context, store, f.host, func() time.Time { return f.now })
	if *inspections != 1 || !errors.Is(err, syscall.EIO) || !repairValidatorObservationPending(err) || errors.Is(err, errRpcIntegrity) || strings.Contains(err.Error(), "window closed") || result.Status != "uncertain-consumed-start" || !result.StopConsumed || !result.StartConsumed || result.Generation != nil || f.stops != 1 || f.starts != 0 {
		t.Fatal("unknown dispatch clock became an expiry verdict or lost completed stop", result, err, f.stops, f.starts)
	}
}

// Initial validator activation retains the completed first role when the
// second role's final successful storage read outlives its own start window.
func TestValidatorActivationStartWindowAfterStorageInspection(t *testing.T) {
	for _, change := range []string{"unchanged", "expiry", "rollback", "read-age"} {
		f := newValidatorActivationFixture(t)
		f.installed()
		originalAt := f.now
		inspections := repairStartWindowInspection(t, f.host.host, f.approval.Plan.StatePath, 2, func() error {
			switch change {
			case "expiry":
				f.now = f.approval.Plan.ExpiresAt
			case "rollback":
				f.now = originalAt.Add(-time.Nanosecond)
			case "read-age":
				f.now = originalAt.Add(time.Duration(f.approval.Plan.MaximumSampleAgeSeconds+1) * time.Second)
			}
			return nil
		})
		result, code, detail := f.command(t.Context(), "start", f)
		if *inspections != 2 || result.Units[0].Completed == nil || result.Units[1].StartAt.IsZero() {
			t.Fatal("activation lost its first completed role or second reservation", change, *inspections, result, code, detail)
		}
		if change == "unchanged" {
			if code != 0 || result.Status != "processes-observed" || result.Units[1].Completed == nil || f.starts != [2]int{1, 1} {
				t.Fatal("unchanged activation clock refused second start", result, code, detail, f.starts)
			}
		} else if code == 0 || result.Status != "partial" || result.Units[1].Status != "uncertain-consumed-start" || result.Units[1].Generation != nil || f.starts != [2]int{1, 0} {
			t.Fatal("late activation clock change dispatched second start", change, result, code, detail, f.starts)
		}
		starts, completed := f.starts, *result.Units[0].Completed
		f.now = originalAt
		result, _, detail = f.command(t.Context(), "resume", nil)
		if result.Units[0].Completed == nil || *result.Units[0].Completed != completed || result.Units[1].StartAt.IsZero() || f.starts != starts {
			t.Fatal("activation reopen reset completed or consumed work", change, result, detail, f.starts)
		}
	}
}

// A narrower current approval expires independently of the unit approval and
// evidence age. The public current-authority path must retain that distinction.
func TestValidatorActivationCurrentWindowAfterStorageInspection(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	originalAt := f.f.now
	f.authority.retained.Approval.Authorization.ExpiresAt = originalAt.Add(30 * time.Second)
	f.sign()
	inspections := repairStartWindowInspection(t, f.f.host.host, f.f.approval.Plan.StatePath, 1, func() error {
		f.f.now = f.authority.retained.Approval.Authorization.ExpiresAt
		return nil
	})
	result, code, detail := f.f.command(t.Context(), "start", f.authority)
	if *inspections != 1 || code == 0 || result.Status != "partial" || result.Units[0].StartAt.IsZero() || result.Units[0].Status != "uncertain-consumed-start" || result.Units[0].Generation != nil || f.f.starts != [2]int{} || !strings.Contains(detail, "current admission window") {
		t.Fatal("storage inspection outlived distinct current authority", *inspections, result, code, detail, f.f.starts)
	}
	f.f.now = originalAt
	result, code, detail = f.f.command(t.Context(), "resume", nil)
	if code == 0 || result.Units[0].StartAt.IsZero() || result.Units[0].Generation != nil || f.f.starts != [2]int{} {
		t.Fatal("current authority expiry refunded start", result, code, detail, f.f.starts)
	}
}

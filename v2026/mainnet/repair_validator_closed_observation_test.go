// Public repair dispositions retain unavailable descriptor observations without
// replenishing the signed start/stop, observation or join-window allowances.
package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The stopped-generation entry point checks real retained files before the
// injected filesystem observation and later consumes only the original start.
func TestRepairValidatorPublicClosedCgroupObservationPreservesStart(t *testing.T) {
	for _, cause := range []error{os.ErrClosed, syscall.EBADF} {
		f := newRepairValidatorFixture(t)
		f.claim()
		approval, err := os.ReadFile(f.approvalPath)
		if err != nil {
			t.Fatal(err)
		}
		original := f.host.cgroupType
		calls := 0
		f.host.cgroupType = func(string) (int64, error) { calls++; return 0, cause }
		result, exit, detail := f.command("resume")
		if calls != 1 || exit == 0 || result.Status != "source-refused" || result.StartConsumed || result.Generation != nil || f.starts != 0 || !strings.Contains(detail, cause.Error()) {
			t.Fatal("unread cgroup was reported as a changed generation", cause, result, exit, detail, calls)
		}
		f.host.cgroupType = original
		result, exit, detail = f.command("resume")
		if exit != 0 || result.Completed == nil || f.starts != 1 || result.Observations != 2 {
			t.Fatal("same original generation failed to recover", cause, result, exit, detail)
		}
		current, err := os.ReadFile(f.approvalPath)
		if err != nil || !bytes.Equal(approval, current) {
			t.Fatal("observation recovery changed signed authority", err)
		}
	}
}

// The journal selects each fault boundary rather than a fragile call count:
// after the sole stop and before or after the retained join acknowledgment.
func TestRepairActiveValidatorPublicClosedClockPreservesJoinedAllowance(t *testing.T) {
	for _, afterJoin := range []bool{false, true} {
		for _, cause := range []error{os.ErrClosed, syscall.EBADF} {
			f := newRepairActiveValidatorFixture(t)
			f.claimActive()
			original := f.host.monotonic
			faults := 0
			f.host.monotonic = func() (uint64, error) {
				var record repairActiveValidatorRecord
				repairObservationTestRecord(t, f.active.Plan.Process.StatePath, &record)
				if f.stops == 1 && record.StartAt.IsZero() && (!record.JoinedAt.IsZero() == afterJoin) && faults == 0 {
					faults++
					return 0, cause
				}
				return original()
			}
			result, exit, detail := f.commandActive("resume")
			if faults != 1 || exit == 0 || result.Status != "source-refused" || !result.StopConsumed || result.StartConsumed || f.stops != 1 || f.starts != 0 || !strings.Contains(detail, cause.Error()) {
				t.Fatal("unread action clock was reported as a closed join window", cause, afterJoin, result, exit, detail, faults)
			}
			var retained repairActiveValidatorRecord
			repairObservationTestRecord(t, f.active.Plan.Process.StatePath, &retained)
			if retained.JoinedAt.IsZero() == afterJoin || rootObjectHash(retained.Approval) != rootObjectHash(f.active) {
				t.Fatal("clock failure changed the original join or approval", retained)
			}
			result, exit, detail = f.commandActive("resume")
			if exit != 0 || result.Completed == nil || f.stops != 1 || f.starts != 1 || result.Observations != 2 {
				t.Fatal("clock recovery repeated or lost an original action", cause, afterJoin, result, exit, detail)
			}
		}
	}
}

// A read failure never extends elapsed signed authority. Recovery after the
// original join deadline retains the consumed stop and refuses a start.
func TestRepairActiveValidatorClosedClockCannotExtendOriginalWindow(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	f.claimActive()
	original := f.host.monotonic
	f.host.monotonic = func() (uint64, error) {
		if f.stops == 1 {
			return 0, syscall.EBADF
		}
		return original()
	}
	result, exit, detail := f.commandActive("resume")
	if exit == 0 || result.Status != "source-refused" || f.stops != 1 || f.starts != 0 {
		t.Fatal("closed clock did not preserve the stopped observation", result, exit, detail)
	}
	f.host.monotonic = original
	f.now = f.now.Add(61 * time.Second)
	result, exit, detail = f.commandActive("resume")
	if exit == 0 || result.Status != "join-window-closed" || !result.StopConsumed || result.StartConsumed || f.stops != 1 || f.starts != 0 {
		t.Fatal("unavailable clock replenished the original join window", result, exit, detail)
	}
}

// Joining an observation failure to positively established integrity loss
// cannot turn that loss into retry permission or publish a new generation.
func TestRepairValidatorClosedObservationCannotHideCustodyLoss(t *testing.T) {
	f := newRepairValidatorFixture(t)
	f.claim()
	f.host.cgroupType = func(string) (int64, error) {
		return 0, errors.Join(durablevolume.ErrIdentity, syscall.EBADF)
	}
	result, exit, detail := f.command("resume")
	if exit == 0 || result.Status != "generation-changed" || result.StartConsumed || f.starts != 0 || !strings.Contains(detail, durablevolume.ErrIdentity.Error()) || !strings.Contains(detail, syscall.EBADF.Error()) {
		t.Fatal("closed observation masked established identity loss", result, exit, detail)
	}
	for _, authority := range []error{durablevolume.ErrIdentity, errRpcIntegrity, errMainnetDurablePublicationUncertain} {
		if repairValidatorObservationPending(errors.Join(authority, os.ErrClosed)) {
			t.Fatal("closed descriptor weakened authority precedence", authority)
		}
	}
}

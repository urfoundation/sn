package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const syntheticDoctorService = "urnetwork-sim-synthetic-doctor.service"

// A status exit is data from the command boundary, not a string classification.
type doctorSystemdExit struct{ code int }

func (self doctorSystemdExit) Error() string { return fmt.Sprintf("synthetic exit %d", self.code) }
func (self doctorSystemdExit) ExitCode() int { return self.code }

type doctorSystemdFixture struct {
	t          *testing.T
	manager    string
	managerErr error
	failed     string
	failedErr  error
	unit       string
	unitErr    error
	calls      []string
}

func newDoctorSystemdFixture(t *testing.T) *doctorSystemdFixture {
	return &doctorSystemdFixture{
		t: t, manager: "running\n",
		unit: "Id=" + syntheticDoctorService + "\nLoadState=loaded\nActiveState=active\nSubState=running\nCanStart=yes\n",
	}
}

// Every test refuses any command beyond the three exact read-only observations.
// In particular, a fix cannot pass by resetting another owner's failed latch.
func (self *doctorSystemdFixture) run(args ...string) ([]byte, error) {
	command := strings.Join(args, " ")
	self.calls = append(self.calls, command)
	switch command {
	case "--user is-system-running":
		return []byte(self.manager), self.managerErr
	case "--user list-units --state=failed --all --plain --no-legend --no-pager":
		return []byte(self.failed), self.failedErr
	case "--user show " + syntheticDoctorService + " --property=Id --property=LoadState --property=ActiveState --property=SubState --property=CanStart --no-pager":
		return []byte(self.unit), self.unitErr
	default:
		self.t.Fatalf("unexpected or mutating manager command: %q", args)
		return nil, errors.New("unreachable manager command")
	}
}

func TestSystemdUserManagerScopesHistoricalFailuresToOwnedUnit(t *testing.T) {
	self := newDoctorSystemdFixture(t)
	self.manager, self.managerErr = "degraded\n", doctorSystemdExit{code: 1}
	var historical []string
	for index := range 10 {
		name := fmt.Sprintf("synthetic-prior-campaign-%02d.service", index)
		historical = append(historical, name)
		self.failed += name + " loaded failed failed previous campaign\n"
	}
	detail, err := inspectSystemdUserManager(self.run, syntheticDoctorService)
	if err != nil || len(self.calls) != 3 || !strings.Contains(detail, "owned_service="+syntheticDoctorService+" load=loaded active=active/running can_start=yes") {
		t.Fatalf("historical failures blocked the observed startable owner: detail=%q calls=%q err=%v", detail, self.calls, err)
	}
	if !strings.Contains(detail, fmt.Sprintf("failed_units=%q", historical)) {
		t.Fatalf("historical failure evidence was discarded: %s", detail)
	}
}

func TestSystemdUserManagerKeepsOwnedFailedServiceRecoverable(t *testing.T) {
	self := newDoctorSystemdFixture(t)
	self.manager, self.managerErr = "degraded\n", doctorSystemdExit{code: 1}
	self.failed = syntheticDoctorService + " loaded failed failed prior supervisor\nsynthetic-other.timer loaded failed failed prior timer\n"
	self.unit = strings.ReplaceAll(strings.ReplaceAll(self.unit, "ActiveState=active", "ActiveState=failed"), "SubState=running", "SubState=failed")
	detail, err := inspectSystemdUserManager(self.run, syntheticDoctorService)
	if err != nil || len(self.calls) != 3 || !strings.Contains(detail, "active=failed/failed can_start=yes") || !strings.Contains(detail, "synthetic-other.timer") {
		t.Fatalf("owned recovery or historical evidence changed: detail=%q calls=%q err=%v", detail, self.calls, err)
	}
	// Startability never makes the same terminal service ready for live work.
	if !(supervisorServiceStatus{ActiveState: "failed", SubState: "failed"}).terminal() {
		t.Fatal("doctor recovery permission weakened live supervisor readiness")
	}
}

func TestSystemdUserManagerObservesOwnedUnitWhileRunning(t *testing.T) {
	self := newDoctorSystemdFixture(t)
	detail, err := inspectSystemdUserManager(self.run, syntheticDoctorService)
	if err != nil || len(self.calls) != 2 || !strings.Contains(detail, "owned_service="+syntheticDoctorService) {
		t.Fatalf("running manager skipped its exact unit: detail=%q calls=%q err=%v", detail, self.calls, err)
	}
}

func TestSystemdUserManagerAllowsFreshUninstalledUnit(t *testing.T) {
	self := newDoctorSystemdFixture(t)
	self.manager, self.managerErr = "degraded\n", doctorSystemdExit{code: 1}
	self.failed = "synthetic-prior.service loaded failed failed previous campaign\n"
	self.unit = "Id=" + syntheticDoctorService + "\nLoadState=not-found\nActiveState=inactive\nSubState=dead\nCanStart=no\n"
	detail, err := inspectSystemdUserManager(self.run, syntheticDoctorService)
	if err != nil || len(self.calls) != 3 || !strings.Contains(detail, "load=not-found active=inactive/dead can_start=no") {
		t.Fatalf("fresh unit absence was not authenticated: detail=%q calls=%q err=%v", detail, self.calls, err)
	}
}

func TestSystemdUserManagerRejectsUnavailableOrUnknownManager(t *testing.T) {
	for _, test := range []struct {
		name  string
		state string
		err   error
	}{
		{name: "empty", state: ""},
		{name: "starting", state: "starting\n"},
		{name: "stopping", state: "stopping\n"},
		{name: "offline", state: "offline\n"},
		{name: "running query canceled", state: "running\n", err: context.Canceled},
		{name: "degraded query deadline", state: "degraded\n", err: context.DeadlineExceeded},
		{name: "degraded wrong exit", state: "degraded\n", err: doctorSystemdExit{code: 2}},
		{name: "degraded joined cancellation", state: "degraded\n", err: errors.Join(doctorSystemdExit{code: 1}, context.Canceled)},
		{name: "string lookalike", state: "degraded\n", err: errors.New("exit status 1")},
	} {
		self := newDoctorSystemdFixture(t)
		self.manager, self.managerErr = test.state, test.err
		detail, err := inspectSystemdUserManager(self.run, syntheticDoctorService)
		if err == nil || len(self.calls) != 1 || test.err != nil && !errors.Is(err, test.err) {
			t.Fatalf("%s: manager error lost: detail=%q calls=%q err=%v", test.name, detail, self.calls, err)
		}
	}
}

func TestSystemdUserManagerPreservesInventoryReadFailure(t *testing.T) {
	self := newDoctorSystemdFixture(t)
	self.manager, self.managerErr = "degraded\n", doctorSystemdExit{code: 1}
	self.failed = "synthetic-prior.service loaded failed failed previous campaign\n"
	self.failedErr = context.DeadlineExceeded
	_, err := inspectSystemdUserManager(self.run, syntheticDoctorService)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "synthetic-prior.service") || len(self.calls) != 2 {
		t.Fatalf("failed inventory query lost its cause or evidence: calls=%q err=%v", self.calls, err)
	}
}

func TestSystemdUserManagerRejectsUnexplainedDegradation(t *testing.T) {
	self := newDoctorSystemdFixture(t)
	self.manager, self.managerErr = "degraded\n", doctorSystemdExit{code: 1}
	detail, err := inspectSystemdUserManager(self.run, syntheticDoctorService)
	if err == nil || len(self.calls) != 2 || !strings.Contains(detail, "failed_units=[]") {
		t.Fatalf("unknown manager failure admitted: detail=%q calls=%q err=%v", detail, self.calls, err)
	}
}

func TestSystemdUserManagerRejectsUnusableOrAmbiguousOwnedUnit(t *testing.T) {
	valid := newDoctorSystemdFixture(t).unit
	for _, test := range []struct {
		name string
		unit string
	}{
		{name: "empty", unit: ""},
		{name: "missing property", unit: strings.ReplaceAll(valid, "CanStart=yes\n", "")},
		{name: "duplicate property", unit: valid + "Id=" + syntheticDoctorService + "\n"},
		{name: "unexpected property", unit: valid + "Extra=unknown\n"},
		{name: "alias", unit: strings.ReplaceAll(valid, syntheticDoctorService, "synthetic-other.service")},
		{name: "empty property", unit: strings.ReplaceAll(valid, "CanStart=yes", "CanStart=")},
		{name: "masked", unit: strings.ReplaceAll(valid, "LoadState=loaded", "LoadState=masked")},
		{name: "bad-setting", unit: strings.ReplaceAll(valid, "LoadState=loaded", "LoadState=bad-setting")},
		{name: "load error", unit: strings.ReplaceAll(valid, "LoadState=loaded", "LoadState=error")},
		{name: "unstartable", unit: strings.ReplaceAll(valid, "CanStart=yes", "CanStart=no")},
		{name: "unknown startability", unit: strings.ReplaceAll(valid, "CanStart=yes", "CanStart=unknown")},
		{name: "active missing unit", unit: strings.ReplaceAll(valid, "LoadState=loaded", "LoadState=not-found")},
	} {
		self := newDoctorSystemdFixture(t)
		self.unit = test.unit
		detail, err := inspectSystemdUserManager(self.run, syntheticDoctorService)
		if err == nil || len(self.calls) != 2 {
			t.Fatalf("%s: unusable owned unit admitted: detail=%q calls=%q err=%v", test.name, detail, self.calls, err)
		}
	}
}

func TestSystemdUserManagerPreservesOwnedUnitQueryFailure(t *testing.T) {
	self := newDoctorSystemdFixture(t)
	self.unitErr = context.DeadlineExceeded
	_, err := inspectSystemdUserManager(self.run, syntheticDoctorService)
	if !errors.Is(err, context.DeadlineExceeded) || len(self.calls) != 2 {
		t.Fatalf("partial owned-unit bytes hid a query error: calls=%q err=%v", self.calls, err)
	}
}

func TestSystemdUserManagerRequiresOwnedScope(t *testing.T) {
	self := newDoctorSystemdFixture(t)
	if _, err := inspectSystemdUserManager(self.run, ""); err == nil || len(self.calls) != 0 {
		t.Fatalf("missing owner dispatched a query: calls=%q err=%v", self.calls, err)
	}
	if _, err := inspectSystemdUserManager(nil, syntheticDoctorService); err == nil {
		t.Fatal("missing manager runner was admitted")
	}
}

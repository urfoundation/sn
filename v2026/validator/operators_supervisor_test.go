//go:build linux || darwin

// The supervisor runs against injected runners, sign-in, provisioning, timers
// and log, plus one end-to-end test that provisions a pristine directory
// through clientauth and hands it to the real runner.
package validator

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/hotkeyauth"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

type operatorSupervisorTest struct {
	base     string
	source   operatorTestSource
	runner   *operatorTestRunner
	clock    *operatorTestClock
	log      *operatorTestLog
	settings allOperatorsSettings
}

func newOperatorSupervisorTest(t *testing.T) *operatorSupervisorTest {
	t.Helper()
	base := operatorTestDir(t)
	return &operatorSupervisorTest{
		base:     base,
		source:   newOperatorTestSource(),
		runner:   newOperatorTestRunner(),
		clock:    newOperatorTestClock(),
		log:      newOperatorTestLog(),
		settings: allOperatorsSettings{measurement: measurementRunSettings{stateDir: base, concurrency: 2, m: 6}},
	}
}

func (self *operatorSupervisorTest) hooks() operatorSupervisorHooks {
	return operatorSupervisorHooks{run: self.runner.run, after: self.clock.after, now: self.clock.Now, log: self.log.log}
}

func (self *operatorSupervisorTest) start(t *testing.T, settings operatorSupervisorSettings, hooks operatorSupervisorHooks) *operatorSupervisor {
	t.Helper()
	if settings.runner == nil {
		settings.runner = self.settings.operatorRunner
	}
	supervisor := newOperatorSupervisor(t.Context(), self.source, settings, hooks)
	t.Cleanup(supervisor.Close)
	return supervisor
}

func TestOperatorSupervisorStartsOneRunnerPerListedOperator(t *testing.T) {
	test := newOperatorSupervisorTest(t)
	one, two := operatorTestOperator("one"), operatorTestOperator("two")
	for _, operator := range []operatorlist.Operator{one, two} {
		writeOperatorTestJwt(t, test.settings.operatorRunner(operator).networkPath)
	}
	test.start(t, operatorSupervisorSettings{}, test.hooks())
	test.source.publish(one, two)
	runs := map[string]*operatorTestRun{}
	for range 2 {
		run := test.runner.next(t)
		runs[run.operator.Domain] = run
	}
	for _, operator := range []operatorlist.Operator{one, two} {
		run, directory := runs[operator.Domain], filepath.Join(test.base, "operators", operator.Domain)
		if run == nil || run.settings.apiUrl != operator.ApiUrl || run.settings.connectUrl != operator.ConnectUrl ||
			run.settings.stateDir != directory || run.settings.networkPath != filepath.Join(directory, "jwt") ||
			run.settings.concurrency != 2 || run.settings.m != 6 {
			t.Fatalf("operator %s runner settings: %+v", operator.Domain, run)
		}
	}
}

func TestOperatorSupervisorStopsDelistedOperatorAndKeepsItsState(t *testing.T) {
	test := newOperatorSupervisorTest(t)
	one, two := operatorTestOperator("one"), operatorTestOperator("two")
	for _, operator := range []operatorlist.Operator{one, two} {
		writeOperatorTestJwt(t, test.settings.operatorRunner(operator).networkPath)
	}
	supervisor := test.start(t, operatorSupervisorSettings{}, test.hooks())
	test.source.publish(one, two)
	runs := map[string]*operatorTestRun{}
	for range 2 {
		run := test.runner.next(t)
		runs[run.operator.Domain] = run
	}
	test.source.publish(one)
	test.log.wait(t, "operator two.example was delisted")
	waitOperatorTestStopped(t, runs["two.example"])
	if runs["one.example"].ctx.Err() != nil {
		t.Fatal("delisting one operator stopped another")
	}
	if _, err := os.Stat(test.settings.operatorRunner(two).networkPath); err != nil {
		t.Fatal("delisted operator state was not kept", err)
	}
	supervisor.Close()
	waitOperatorTestStopped(t, runs["one.example"])
	test.runner.assertNoMoreRuns(t)
}

func TestOperatorSupervisorRestartsOperatorWhoseEndpointsChange(t *testing.T) {
	test := newOperatorSupervisorTest(t)
	one := operatorTestOperator("one")
	writeOperatorTestJwt(t, test.settings.operatorRunner(one).networkPath)
	test.start(t, operatorSupervisorSettings{}, test.hooks())
	test.source.publish(one)
	first := test.runner.next(t)
	changed := one
	changed.ConnectUrl = "wss://connect-two.one.example"
	test.source.publish(changed)
	waitOperatorTestStopped(t, first)
	second := test.runner.next(t)
	if second.settings.connectUrl != changed.ConnectUrl || second.settings.apiUrl != one.ApiUrl || second.settings.stateDir != first.settings.stateDir {
		t.Fatalf("changed endpoints did not restart the runner on them: %+v", second.settings)
	}
	test.log.wait(t, "operator one.example changed endpoints")
}

func TestOperatorSupervisorBacksOffAfterRunnerFailures(t *testing.T) {
	test := newOperatorSupervisorTest(t)
	one, two := operatorTestOperator("one"), operatorTestOperator("two")
	for _, operator := range []operatorlist.Operator{one, two} {
		writeOperatorTestJwt(t, test.settings.operatorRunner(operator).networkPath)
	}
	test.start(t, operatorSupervisorSettings{}, test.hooks())
	test.source.publish(one, two)
	var failing, steady *operatorTestRun
	for range 2 {
		run := test.runner.next(t)
		if run.operator.Domain == one.Domain {
			failing = run
		} else {
			steady = run
		}
	}
	canary := errors.New("synthetic runner failure")
	for _, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute} {
		failing.fail <- canary
		wait := test.clock.next(t)
		if wait.delay != want {
			t.Fatalf("restart delay %s, want %s", wait.delay, want)
		}
		test.clock.fire(wait)
		failing = test.runner.next(t)
		if failing.operator.Domain != one.Domain {
			t.Fatalf("a failure restarted %s", failing.operator.Domain)
		}
	}
	test.log.wait(t, "operator one.example failed: synthetic runner failure; retrying in 10m0s")
	// A runner that stays up through the longest delay starts a new series.
	test.clock.advance(operatorRestartMaximum)
	failing.fail <- canary
	if wait := test.clock.next(t); wait.delay != operatorRestartMinimum {
		t.Fatalf("restart delay after a long run %s, want %s", wait.delay, operatorRestartMinimum)
	}
	if steady.ctx.Err() != nil {
		t.Fatal("a failing operator stopped another operator's runner")
	}
}

func TestOperatorSupervisorAwaitsAuthUntilTheJwtAppears(t *testing.T) {
	test := newOperatorSupervisorTest(t)
	one := operatorTestOperator("one")
	var provisions atomic.Int32
	hooks := test.hooks()
	hooks.provision = func(context.Context, measurementRunSettings) (bool, error) {
		provisions.Add(1)
		return false, nil
	}
	test.start(t, operatorSupervisorSettings{}, hooks)
	test.source.publish(one)
	if line := test.log.wait(t, "awaiting auth"); line != "operator one.example is awaiting auth: validator auth --operator=one.example" {
		t.Fatalf("awaiting-auth line %q", line)
	}
	wait := test.clock.next(t)
	if wait.delay > 5*time.Minute {
		t.Fatalf("awaiting auth rechecks after %s", wait.delay)
	}
	writeOperatorTestJwt(t, test.settings.operatorRunner(one).networkPath)
	test.clock.fire(wait)
	if run := test.runner.next(t); run.operator != one {
		t.Fatalf("authenticated operator did not start: %+v", run.operator)
	}
	if provisions.Load() != 0 {
		t.Fatal("an operator without auto-register was provisioned")
	}
}

func TestOperatorSupervisorSignsInAndProvisionsWithAutoRegister(t *testing.T) {
	test := newOperatorSupervisorTest(t)
	operator, server := newOperatorTestSignIn(t)
	one := operatorlist.Operator{Domain: "one.example", ApiUrl: server.URL, ConnectUrl: "ws://127.0.0.1:9"}
	hotkey := operatorTestHotkey(t, 7)
	provisioned := make(chan measurementRunSettings, 4)
	hooks := test.hooks()
	hooks.provision = func(ctx context.Context, settings measurementRunSettings) (bool, error) {
		provisioned <- settings
		return true, nil
	}
	test.start(t, operatorSupervisorSettings{hotkey: hotkey}, hooks)
	test.source.publish(one)
	run := test.runner.next(t)
	jwtPath := filepath.Join(test.base, "operators", "one.example", "jwt")
	if run.settings.networkPath != jwtPath {
		t.Fatalf("runner network JWT path %s", run.settings.networkPath)
	}
	token, err := clientauth.ReadToken(jwtPath)
	if err != nil || token != "jwt:"+hotkeyauth.NetworkName(hotkey.PublicKey()) || operator.networkCount() != 1 {
		t.Fatal("auto-register did not write the hotkey network's JWT", err)
	}
	if info, err := os.Stat(jwtPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("auto-register JWT is not private", err)
	}
	select {
	case settings := <-provisioned:
		if settings.stateDir != run.settings.stateDir || settings.networkPath != jwtPath || settings.apiUrl != server.URL {
			t.Fatalf("provisioned another directory: %+v", settings)
		}
	default:
		t.Fatal("the runner started before its directory was provisioned")
	}
	test.log.wait(t, "operator one.example signed in with the hotkey")
}

func TestOperatorSupervisorProvisionsOnlyWithAutoRegister(t *testing.T) {
	test := newOperatorSupervisorTest(t)
	one := operatorTestOperator("one")
	writeOperatorTestJwt(t, test.settings.operatorRunner(one).networkPath)
	hooks := test.hooks()
	hooks.provision = func(context.Context, measurementRunSettings) (bool, error) {
		t.Error("an operator without auto-register was provisioned")
		return false, nil
	}
	test.start(t, operatorSupervisorSettings{provisionFlags: "--auto-register --hotkey_seed_file=<path>"}, hooks)
	test.source.publish(one)
	run := test.runner.next(t)
	run.fail <- &clientauth.RegistrationRefusedError{Code: "measurement_key_requires_explicit_recovery"}
	if wait := test.clock.next(t); wait.delay != operatorRestartMinimum {
		t.Fatalf("a refused pristine directory retries after %s", wait.delay)
	}
	line := test.log.wait(t, "operator one.example failed")
	if !strings.HasSuffix(line, "; restart with --auto-register --hotkey_seed_file=<path> to provision a pristine directory") {
		t.Fatalf("pristine refusal did not name the provisioning flags: %q", line)
	}
}

// A pristine directory is provisioned through clientauth's one creation, then
// the existing runner takes over the created client unchanged.
func TestOperatorSupervisorProvisionsAPristineDirectoryForTheRunner(t *testing.T) {
	fixture := newMeasurementRunFixture(t)
	fixture.dir = operatorTestDir(t)
	fixture.networkPath = filepath.Join(operatorTestDir(t), "jwt")
	if err := clientauth.WriteToken(fixture.networkPath, measurementTestToken(t, "", "bootstrap")); err != nil {
		t.Fatal(err)
	}
	authenticated := make(chan connect.Id, 1)
	ctx := context.WithValue(t.Context(), measurementRunHooksKey{}, measurementRunHooks{afterAuthenticated: func(_ string, id connect.Id, _ []byte) error {
		authenticated <- id
		return nil
	}})
	source, clock, log := newOperatorTestSource(), newOperatorTestClock(), newOperatorTestLog()
	supervisor := newOperatorSupervisor(ctx, source, operatorSupervisorSettings{
		runner: func(operatorlist.Operator) measurementRunSettings { return fixture.settings(false) },
		hotkey: operatorTestHotkey(t, 9),
		output: io.Discard,
	}, operatorSupervisorHooks{after: clock.after, now: clock.Now, log: log.log})
	defer supervisor.Close()
	source.publish(operatorlist.Operator{Domain: "one.example", ApiUrl: fixture.server.URL, ConnectUrl: "ws" + strings.TrimPrefix(fixture.server.URL, "http")})
	select {
	case id := <-authenticated:
		if id.String() != measurementTestClientId {
			t.Fatalf("runner authenticated as %s", id)
		}
	case <-time.After(operatorTestTimeout):
		t.Fatal("the runner did not authenticate with the provisioned client")
	}
	log.wait(t, "operator one.example provisioned a measurement identity")
	supervisor.Close()
	if posts, legacy, _, _ := fixture.counts(); posts != 1 || legacy != 0 {
		t.Fatalf("provisioning made %d registrations and %d legacy allocations", posts, legacy)
	}
	if _, err := os.Stat(filepath.Join(fixture.dir, ".validator.key.provisioned")); err != nil {
		t.Fatal("completed provisioning left no provisioned marker", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.dir, ".validator.key.provisioning")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed provisioning kept its creation authority", err)
	}
}

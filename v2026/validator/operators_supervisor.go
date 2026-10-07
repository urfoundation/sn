// One flag-mode measurement runner per listed operator, keyed by domain and
// driven by operator list snapshots. A delisted operator's runner is cancelled
// and joined, and its state directory is kept; changed endpoints restart it. A
// failed runner restarts after 30 seconds, doubling to 10 minutes, and an
// operator awaiting auth is rechecked every 5 minutes. Each operator has its
// own credentials, identity and failures, and none of them stops another.
package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

const operatorRestartMinimum = 30 * time.Second
const operatorRestartMaximum = 10 * time.Minute
const operatorAuthRecheck = 5 * time.Minute

// The refresher, or a test's sequence of lists.
type operatorListSource interface {
	Snapshot() (*operatorlist.Snapshot, chan struct{})
}

type operatorSupervisorSettings struct {
	// The existing runner's settings for one listed operator, with its own
	// state directory and network JWT path.
	runner func(operatorlist.Operator) measurementRunSettings
	// Listed operators to run; nil runs every one.
	include func(operatorlist.Operator) bool
	// Signs in and provisions pristine directories. Without it an operator
	// with no JWT awaits auth.
	hotkey *crv4.Keypair
	// The command that authenticates one operator, for the awaiting-auth line.
	authCommand func(string) string
	// Named when a runner without a hotkey finds no measurement identity.
	provisionFlags string
	// Runner output, each line prefixed with its operator's domain.
	output io.Writer
}

// Test seams, as operatorlist's refresherHooks. Production runs the existing
// runner, hotkey sign-in, clientauth provisioning, real timers and stderr.
type operatorSupervisorHooks struct {
	run       func(context.Context, operatorlist.Operator, measurementRunSettings) error
	signIn    func(context.Context, string) (string, error)
	provision func(context.Context, measurementRunSettings) (bool, error)
	after     func(time.Duration) <-chan time.Time
	now       func() time.Time
	log       func(string)
}

type operatorSupervisor struct {
	source   operatorListSource
	settings operatorSupervisorSettings
	hooks    operatorSupervisorHooks
	cancel   context.CancelFunc
	done     chan struct{}
}

type operatorWorker struct {
	operator operatorlist.Operator
	cancel   context.CancelFunc
	done     chan struct{}
}

func newOperatorSupervisor(ctx context.Context, source operatorListSource, settings operatorSupervisorSettings, hooks operatorSupervisorHooks) *operatorSupervisor {
	if settings.output == nil {
		settings.output = os.Stdout
	}
	if settings.authCommand == nil {
		settings.authCommand = func(domain string) string { return "validator auth --operator=" + domain }
	}
	if hooks.run == nil {
		output, outputLock := settings.output, &sync.Mutex{}
		hooks.run = func(ctx context.Context, operator operatorlist.Operator, runner measurementRunSettings) error {
			return runner.run(ctx, operatorOutput{prefix: "operator " + operator.Domain + ": ", output: output, outputLock: outputLock})
		}
	}
	if hooks.signIn == nil {
		hotkey := settings.hotkey
		hooks.signIn = func(ctx context.Context, apiUrl string) (string, error) {
			return hotkeySignInJwt(ctx, apiUrl, hotkey)
		}
	}
	if hooks.provision == nil {
		hooks.provision = provisionOperatorMeasurement
	}
	if hooks.after == nil {
		hooks.after = time.After
	}
	if hooks.now == nil {
		hooks.now = time.Now
	}
	if hooks.log == nil {
		hooks.log = logOperatorLine
	}
	ctx, cancel := context.WithCancel(ctx)
	self := &operatorSupervisor{source: source, settings: settings, hooks: hooks, cancel: cancel, done: make(chan struct{})}
	go self.run(ctx)
	return self
}

// Cancels and joins every runner.
func (self *operatorSupervisor) Close() {
	self.cancel()
	<-self.done
}

func (self *operatorSupervisor) run(ctx context.Context) {
	defer close(self.done)
	workers := map[string]*operatorWorker{}
	defer func() {
		for _, worker := range workers {
			worker.cancel()
		}
		for _, worker := range workers {
			<-worker.done
		}
	}()
	for {
		snapshot, update := self.source.Snapshot()
		self.reconcile(ctx, snapshot, workers)
		select {
		case <-ctx.Done():
			return
		case <-update:
		}
	}
}

// Stopped runners are joined before replacements start, so a state
// directory never has two runners.
func (self *operatorSupervisor) reconcile(ctx context.Context, snapshot *operatorlist.Snapshot, workers map[string]*operatorWorker) {
	listed := map[string]operatorlist.Operator{}
	var selected []operatorlist.Operator
	if snapshot != nil {
		for _, operator := range snapshot.List.Operators {
			listed[operator.Domain] = operator
			if self.settings.include == nil || self.settings.include(operator) {
				selected = append(selected, operator)
			}
		}
	}
	running := map[string]bool{}
	for _, operator := range selected {
		running[operator.Domain] = true
	}
	var stopping []*operatorWorker
	for domain, worker := range workers {
		operator, ok := listed[domain]
		if running[domain] && operator == worker.operator {
			continue
		}
		directory := self.settings.runner(worker.operator).stateDir
		switch {
		case !ok:
			self.hooks.log(fmt.Sprintf("operator %s was delisted; stopping its runner and keeping %s", domain, directory))
		case !running[domain]:
			self.hooks.log(fmt.Sprintf("operator %s is no longer selected; stopping its runner and keeping %s", domain, directory))
		default:
			self.hooks.log(fmt.Sprintf("operator %s changed endpoints; restarting its runner", domain))
		}
		worker.cancel()
		stopping = append(stopping, worker)
		delete(workers, domain)
	}
	for _, worker := range stopping {
		<-worker.done
	}
	for _, operator := range selected {
		if _, ok := workers[operator.Domain]; ok {
			continue
		}
		workerCtx, cancel := context.WithCancel(ctx)
		worker := &operatorWorker{operator: operator, cancel: cancel, done: make(chan struct{})}
		workers[operator.Domain] = worker
		go func() {
			defer close(worker.done)
			self.supervise(workerCtx, operator)
		}()
	}
}

// One operator until its context ends: credentials, identity, then the
// runner, restarted after each failure.
func (self *operatorSupervisor) supervise(ctx context.Context, operator operatorlist.Operator) {
	settings := self.settings.runner(operator)
	self.hooks.log(fmt.Sprintf("operator %s is listed at %s and %s; its state is in %s", operator.Domain, operator.ApiUrl, operator.ConnectUrl, settings.stateDir))
	failures := 0
	for ctx.Err() == nil {
		delay := operatorAuthRecheck
		ready, err := self.prepare(ctx, operator, settings)
		if err == nil && ready {
			started := self.hooks.now()
			err = self.hooks.run(ctx, operator, settings)
			if err == nil && ctx.Err() == nil {
				err = errors.New("runner stopped")
			}
			// A runner that outlasted the longest delay starts a new series.
			if self.hooks.now().Sub(started) >= operatorRestartMaximum {
				failures = 0
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			failures++
			// 30 seconds, doubling to 10 minutes.
			delay = min(operatorRestartMinimum<<min(failures-1, 5), operatorRestartMaximum)
			self.hooks.log(fmt.Sprintf("operator %s failed: %v; retrying in %s%s", operator.Domain, err, delay, self.provisionHint(err)))
		}
		select {
		case <-ctx.Done():
			return
		case <-self.hooks.after(delay):
		}
	}
}

// The network JWT, then with a hotkey the measurement identity. Not ready
// means the operator awaits auth.
func (self *operatorSupervisor) prepare(ctx context.Context, operator operatorlist.Operator, settings measurementRunSettings) (bool, error) {
	raw, err := os.ReadFile(settings.networkPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		if self.settings.hotkey == nil {
			self.hooks.log(fmt.Sprintf("operator %s is awaiting auth: %s", operator.Domain, self.settings.authCommand(operator.Domain)))
			return false, nil
		}
		byJwt, err := self.hooks.signIn(ctx, operator.ApiUrl)
		if err != nil {
			return false, fmt.Errorf("hotkey sign-in: %w", err)
		}
		if err := clientauth.WriteToken(settings.networkPath, byJwt); err != nil {
			return false, err
		}
		self.hooks.log(fmt.Sprintf("operator %s signed in with the hotkey; wrote %s", operator.Domain, settings.networkPath))
	}
	if self.settings.hotkey != nil {
		provisioned, err := self.hooks.provision(ctx, settings)
		if err != nil {
			return false, fmt.Errorf("measurement provisioning: %w", err)
		}
		if provisioned {
			self.hooks.log(fmt.Sprintf("operator %s provisioned a measurement identity in %s", operator.Domain, settings.stateDir))
		}
	}
	return true, nil
}

// Names the flags that provision a directory with no measurement identity.
func (self *operatorSupervisor) provisionHint(err error) string {
	var refused *clientauth.RegistrationRefusedError
	if self.settings.hotkey != nil || self.settings.provisionFlags == "" || !errors.As(err, &refused) {
		return ""
	}
	switch refused.Code {
	case "measurement_key_requires_explicit_recovery", "legacy_identity_requires_explicit_recovery":
		return "; restart with " + self.settings.provisionFlags + " to provision a pristine directory"
	}
	return ""
}

// Provisions a pristine operator directory, or completes an interrupted
// provisioning, through clientauth's one permitted creation. Any other
// directory is the runner's: it holds an identity already, or a remnant the
// runner refuses as before.
func provisionOperatorMeasurement(ctx context.Context, settings measurementRunSettings) (provisioned bool, returnErr error) {
	owner, err := clientauth.ProvisionValidatorMeasurementClientKey(ctx, filepath.Join(settings.stateDir, ".validator.key"))
	var refused *clientauth.RegistrationRefusedError
	if errors.As(err, &refused) && refused.Code == "measurement_provisioning_requires_pristine_directory" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { returnErr = errors.Join(returnErr, owner.Close()) }()
	strategySettings := settings.strategySettings
	if strategySettings == nil {
		strategySettings = connect.DefaultClientStrategySettings()
	}
	strategy := connect.NewClientStrategy(ctx, strategySettings)
	defer strategy.Close()
	api := sdk.NewApi(ctx, strategy, settings.apiUrl)
	defer func() { returnErr = errors.Join(returnErr, api.CloseAndWait(context.Background())) }()
	attempt, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	if _, _, err := owner.ProvisionClientJwt(attempt, api, settings.networkPath); err != nil {
		return false, err
	}
	return true, nil
}

// Prefixes each runner write, which is one line, with the operator's domain.
type operatorOutput struct {
	prefix     string
	output     io.Writer
	outputLock *sync.Mutex
}

func (self operatorOutput) Write(raw []byte) (int, error) {
	self.outputLock.Lock()
	defer self.outputLock.Unlock()
	if _, err := io.WriteString(self.output, self.prefix); err != nil {
		return 0, err
	}
	return self.output.Write(raw)
}

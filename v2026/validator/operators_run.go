// The operator-list modes of validator run. Flag mode measures every listed
// operator with the existing runner and never steers. Production mode runs the
// release exactly as without the list; beside it, in the same process, the
// list is refreshed, its drift from the pinned operators is reported and, with
// --observe-unpinned-operators, listed operators the config does not pin are
// measured for observation only. Nothing here reaches the signed config,
// weights, evidence or protocol state.
package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

const operatorListDefaultInterval = time.Hour

type operatorListOptions struct {
	url      string
	interval time.Duration
	// --operators-refresh was given. Production mode follows the list only
	// then, and only then is the report file written.
	refresh bool
}

func operatorListOptionsFromOpts(opts docopt.Opts) (operatorListOptions, error) {
	options := operatorListOptions{url: optString(opts, "--operators-url", operatorlist.DefaultUrl), interval: operatorListDefaultInterval}
	raw := optString(opts, "--operators-refresh", "")
	if raw == "" {
		return options, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil {
		return options, fmt.Errorf("--operators-refresh=%s is not a Go duration such as 1h", raw)
	}
	if interval < time.Minute {
		return options, fmt.Errorf("--operators-refresh=%s is shorter than 1m", raw)
	}
	options.interval, options.refresh = interval, true
	return options, nil
}

// The hotkey for operator sign-in. A missing seed file is an error naming
// it; a hotkey is never created here.
func loadOperatorHotkey(path string) (*crv4.Keypair, error) {
	seed, err := crv4.LoadSeedFile(path)
	if err != nil {
		return nil, fmt.Errorf("hotkey seed file %s: %w", path, err)
	}
	return crv4.KeypairFromSeed(seed)
}

// Flag mode for every listed operator.
type allOperatorsSettings struct {
	// Shared runner options. Each operator replaces the endpoints, the state
	// directory and the network JWT path.
	measurement measurementRunSettings
	list        operatorListOptions
	// Set only with --auto-register.
	hotkeySeedFile string
}

func allOperatorsSettingsFromOpts(opts docopt.Opts) (allOperatorsSettings, error) {
	if err := rejectLegacySteeringOptions(opts); err != nil {
		return allOperatorsSettings{}, err
	}
	measurement, err := measurementSettingsFromOpts(opts)
	if err != nil {
		return allOperatorsSettings{}, err
	}
	list, err := operatorListOptionsFromOpts(opts)
	if err != nil {
		return allOperatorsSettings{}, err
	}
	settings := allOperatorsSettings{measurement: measurement, list: list}
	if optBool(opts, "--auto-register") {
		path := optString(opts, "--hotkey_seed_file", "")
		if path == "" || strings.HasPrefix(path, "<state_dir>") {
			return allOperatorsSettings{}, errors.New("--auto-register requires --hotkey_seed_file")
		}
		settings.hotkeySeedFile = expandHome(path)
	}
	return settings, nil
}

// The existing runner on the operator's endpoints, under
// <state_dir>/operators/<domain> with its network JWT there.
func (self allOperatorsSettings) operatorRunner(operator operatorlist.Operator) measurementRunSettings {
	runner := self.measurement
	runner.apiUrl, runner.connectUrl = operator.ApiUrl, operator.ConnectUrl
	runner.stateDir = operatorlist.DomainStateDir(self.measurement.stateDir, operator.Domain)
	runner.networkPath = filepath.Join(runner.stateDir, "jwt")
	return runner
}

// Names the list and state directory only when auth's defaults differ.
func (self allOperatorsSettings) authCommand(domain string) string {
	command := "validator auth --operator=" + domain
	if self.list.url != operatorlist.DefaultUrl {
		command += " --operators-url=" + self.list.url
	}
	if base, err := defaultStateDir(); err != nil || filepath.Clean(base) != self.measurement.stateDir {
		command += " --state_dir=" + self.measurement.stateDir
	}
	return command
}

func (self allOperatorsSettings) run(ctx context.Context, output io.Writer, hooks operatorSupervisorHooks) error {
	var hotkey *crv4.Keypair
	if self.hotkeySeedFile != "" {
		var err error
		if hotkey, err = loadOperatorHotkey(self.hotkeySeedFile); err != nil {
			return err
		}
	}
	refresher, err := operatorlist.NewRefresher(ctx, operatorlist.RefreshSettings{
		Url:       self.list.url,
		Interval:  self.list.interval,
		CachePath: filepath.Join(self.measurement.stateDir, "operators.yml"),
	})
	if err != nil {
		return err
	}
	defer refresher.Close()
	reportPath := ""
	if self.list.refresh {
		reportPath = filepath.Join(self.measurement.stateDir, "operators-report.json")
	}
	reporter := newOperatorListReporter(ctx, refresher, operatorListReportSettings{url: self.list.url, path: reportPath, now: hooks.now, log: hooks.log})
	defer reporter.Close()
	supervisor := newOperatorSupervisor(ctx, refresher, operatorSupervisorSettings{
		runner:         self.operatorRunner,
		hotkey:         hotkey,
		authCommand:    self.authCommand,
		provisionFlags: "--auto-register --hotkey_seed_file=<path>",
		output:         output,
	}, hooks)
	defer supervisor.Close()
	fmt.Fprintf(output, "validator %s measuring every operator listed at %s (concurrency %d, M %d each); steering disabled\n", RequireVersion(), self.list.url, self.measurement.concurrency, self.measurement.m)
	<-ctx.Done()
	return nil
}

// Startup errors are option errors (a seed file, the list URL); after startup
// the runners report their own failures and the process runs until a signal.
func runAllOperators(opts docopt.Opts) {
	settings, err := allOperatorsSettingsFromOpts(opts)
	exitOnError("validator run", err)
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	defer stopSignals()
	exitOnError("validator run", settings.run(ctx, os.Stdout, operatorSupervisorHooks{}))
}

// The list beside a production release. Observation runners use the flag-mode
// defaults, without chain reads.
type productionOperatorListSettings struct {
	list    operatorListOptions
	observe bool
	// Each observed operator replaces the endpoints and paths.
	measurement measurementRunSettings
}

func productionOperatorListSettingsFromOpts(opts docopt.Opts) (productionOperatorListSettings, error) {
	list, err := operatorListOptionsFromOpts(opts)
	if err != nil {
		return productionOperatorListSettings{}, err
	}
	return productionOperatorListSettings{
		list:        list,
		observe:     optBool(opts, "--observe-unpinned-operators"),
		measurement: measurementRunSettings{concurrency: optInt(opts, "--concurrency", 4), m: optInt(opts, "--m", connect.VerifyMDefault)},
	}, nil
}

// Owns the refresher, the drift report and any observation runners.
type productionOperatorList struct {
	refresher  *operatorlist.Refresher
	reporter   *operatorListReporter
	supervisor *operatorSupervisor
}

// The list cache, the report and observed-operators/<domain> all live under
// the config's state_dir. Observation signs in with the config's hotkey and
// provisions pristine directories; pinned operators are never observed.
func startProductionOperatorList(ctx context.Context, cfg *ReleaseConfig, settings productionOperatorListSettings, output io.Writer, hooks operatorSupervisorHooks) (*productionOperatorList, error) {
	var hotkey *crv4.Keypair
	if settings.observe {
		var err error
		if hotkey, err = loadOperatorHotkey(cfg.HotkeySeedFile); err != nil {
			return nil, err
		}
	}
	refresher, err := operatorlist.NewRefresher(ctx, operatorlist.RefreshSettings{
		Url:       settings.list.url,
		Interval:  settings.list.interval,
		CachePath: filepath.Join(cfg.StateDir, "operators.yml"),
	})
	if err != nil {
		return nil, err
	}
	self := &productionOperatorList{refresher: refresher}
	self.reporter = newOperatorListReporter(ctx, refresher, operatorListReportSettings{
		pinned: cfg.Operators,
		url:    settings.list.url,
		path:   filepath.Join(cfg.StateDir, "operators-report.json"),
		now:    hooks.now,
		log:    hooks.log,
	})
	if settings.observe {
		pinned := pinnedOperatorEndpoints(cfg.Operators)
		base := filepath.Join(cfg.StateDir, "observed-operators")
		self.supervisor = newOperatorSupervisor(ctx, refresher, operatorSupervisorSettings{
			runner: func(operator operatorlist.Operator) measurementRunSettings {
				runner := settings.measurement
				runner.apiUrl, runner.connectUrl = operator.ApiUrl, operator.ConnectUrl
				runner.stateDir = filepath.Join(base, operator.Domain)
				runner.networkPath = filepath.Join(runner.stateDir, "jwt")
				return runner
			},
			include: func(operator operatorlist.Operator) bool {
				return !pinned[normalizedOperatorEndpoint(operator.ApiUrl)]
			},
			hotkey: hotkey,
			output: output,
		}, hooks)
	}
	return self, nil
}

func (self *productionOperatorList) Close() {
	if self.supervisor != nil {
		self.supervisor.Close()
	}
	self.reporter.Close()
	self.refresher.Close()
}

// The pinned operators and state paths only: the strict document decode,
// without the approvals, keys or validation the release runner applies.
func decodeOperatorListReleaseConfig(path string) (*ReleaseConfig, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maximumReleaseConfigBytes+1))
	if err != nil {
		return nil, err
	}
	return decodeReleaseConfigDocument(abs, raw)
}

// The release gets exactly the context and arguments runReleaseConfig gives
// it; the list shares only the process and its signals, and is joined after
// the release returns.
func runReleaseWithOperatorList(configPath, progressPath string, reference durablevolume.Reference, settings productionOperatorListSettings) {
	cfg, err := decodeOperatorListReleaseConfig(configPath)
	exitOnError("validator run", err)
	ctx := context.Background()
	if reference.Path != "" || reference.Sha256 != "" {
		ctx = durablevolume.WithReference(ctx, reference)
	}
	event := connect.NewEventWithContext(ctx)
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	listCtx, cancelList := context.WithCancel(event.Ctx())
	list, err := startProductionOperatorList(listCtx, cfg, settings, os.Stdout, operatorSupervisorHooks{})
	if err != nil {
		cancelList()
		exitOnError("validator run", err)
	}
	releaseErr := RunReleaseWithProgress(event.Ctx(), configPath, progressPath)
	cancelList()
	list.Close()
	if releaseErr != nil {
		panic(releaseErr)
	}
}

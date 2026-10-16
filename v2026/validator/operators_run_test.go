//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/operatorlist"
)

func TestOperatorListUsageForms(t *testing.T) {
	for _, testCase := range []struct {
		args []string
		key  string
		want map[string]any
	}{
		{args: []string{"run", "--all-operators"}, key: "run", want: map[string]any{"--all-operators": true, "--operators-url": operatorlist.DefaultUrl, "--operators-refresh": nil}},
		{args: []string{"run", "--all-operators", "--operators-url=https://list.example/operators.yml", "--operators-refresh=30m",
			"--auto-register", "--hotkey_seed_file=/keys/hot.seed", "--concurrency=2", "--m=6", "--rpc=https://rpc-a.example", "--rpc=https://rpc-b.example",
			"--contract=0x00000000000000000000000000000000000000a1", "--state_dir=/synthetic/state", "--adopt-legacy-measurement-key", "-vv"},
			key: "run", want: map[string]any{"--operators-url": "https://list.example/operators.yml", "--operators-refresh": "30m", "--auto-register": true,
				"--hotkey_seed_file": "/keys/hot.seed", "--state_dir": "/synthetic/state", "--adopt-legacy-measurement-key": true}},
		{args: []string{"run", "--config=/etc/ur-validator/release.yml", "--operators-refresh=1h"}, key: "run",
			want: map[string]any{"--config": "/etc/ur-validator/release.yml", "--operators-refresh": "1h", "--observe-unpinned-operators": false}},
		{args: []string{"run", "--config=/etc/ur-validator/release.yml", "--progress-file=/var/lib/ur-validator-observation/progress.json",
			"--durable-volumes=/etc/ur-validator/volumes.json", "--durable-volumes-sha256=" + strings.Repeat("ab", 32),
			"--operators-refresh=1h", "--operators-url=https://list.example/operators.yml", "--observe-unpinned-operators", "-v"},
			key: "run", want: map[string]any{"--operators-url": "https://list.example/operators.yml", "--observe-unpinned-operators": true}},
		{args: []string{"auth", "--operator=one.example"}, key: "auth", want: map[string]any{"--operator": "one.example"}},
		{args: []string{"auth", "--hotkey_seed_file=/keys/hot.seed", "-f", "--operator=one.example", "--operators-url=https://list.example/operators.yml", "--state_dir=/synthetic/state"},
			key: "auth", want: map[string]any{"--hotkey_seed_file": "/keys/hot.seed", "-f": true, "--state_dir": "/synthetic/state"}},
		{args: []string{"auth", "--hotkey_seed_file=/keys/hot.seed", "--api_url=https://api.one.example"}, key: "auth", want: map[string]any{"--operator": nil}},
		{args: []string{"auth", "--user_auth=synthetic@one.example", "--password=synthetic", "--operator=one.example"}, key: "auth", want: map[string]any{"--operator": "one.example"}},
		{args: []string{"auth", "SYNTHETICCODE", "--operator=one.example"}, key: "auth", want: map[string]any{"<auth_code>": "SYNTHETICCODE"}},
	} {
		opts, err := parseValidatorArgsForTest(t, testCase.args)
		if err != nil {
			t.Fatalf("%v: %v", testCase.args, err)
		}
		if selected, _ := opts.Bool(testCase.key); !selected {
			t.Fatalf("%v: command %s not selected", testCase.args, testCase.key)
		}
		for key, want := range testCase.want {
			if got := opts[key]; got != want {
				t.Fatalf("%v: %s = %#v, want %#v", testCase.args, key, got, want)
			}
		}
	}
}

func TestOperatorListUsageRefusesMixedForms(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--all-operators", "--api_url=https://api.one.example"},
		{"run", "--all-operators", "--connect_url=wss://connect.one.example"},
		{"run", "--all-operators", "--auto-register"},
		{"run", "--all-operators", "--hotkey_seed_file=/keys/hot.seed"},
		{"run", "--all-operators", "--config=/etc/ur-validator/release.yml"},
		{"run", "--all-operators", "--observe-unpinned-operators"},
		{"run", "--all-operators", "--progress-file=/var/lib/ur-validator-observation/progress.json"},
		{"run", "--config=/etc/ur-validator/release.yml", "--operators-url=https://list.example/operators.yml"},
		{"run", "--config=/etc/ur-validator/release.yml", "--observe-unpinned-operators"},
		{"run", "--config=/etc/ur-validator/release.yml", "--operators-refresh=1h", "--auto-register"},
		{"run", "--config=/etc/ur-validator/release.yml", "--operators-refresh=1h", "--state_dir=/synthetic/state"},
		{"run", "--operators-refresh=1h"},
		{"run", "--operators-url=https://list.example/operators.yml"},
		{"run", "--api_url=https://api.one.example", "--auto-register", "--hotkey_seed_file=/keys/hot.seed"},
		{"auth", "--operator=one.example", "--api_url=https://api.one.example"},
		{"auth", "--operators-url=https://list.example/operators.yml"},
		{"auth", "--state_dir=/synthetic/state"},
		{"auth", "SYNTHETICCODE", "--hotkey_seed_file=/keys/hot.seed"},
		{"auth", "--user_auth=synthetic@one.example", "--hotkey_seed_file=/keys/hot.seed"},
		{"auth", "--operator=one.example", "--operators-refresh=1h"},
	} {
		// Invalid input must return through the real parser rather than exit
		// through docopt's help handler.
		_, err := parseValidatorArgsForTest(t, args)
		var invalid *docopt.UserError
		if !errors.As(err, &invalid) {
			t.Errorf("%v parsed: %v", args, err)
		}
	}
}

func TestOperatorListRefreshIsAGoDurationOfAtLeastAMinute(t *testing.T) {
	for _, raw := range []string{"30s", "59s", "1", "hourly", "-1h"} {
		if _, err := operatorListOptionsFromOpts(docopt.Opts{"--operators-refresh": raw}); err == nil {
			t.Errorf("--operators-refresh=%s accepted", raw)
		}
	}
	options, err := operatorListOptionsFromOpts(docopt.Opts{"--operators-url": operatorlist.DefaultUrl})
	if err != nil || options.refresh || options.interval != time.Hour || options.url != operatorlist.DefaultUrl {
		t.Fatalf("default list options: %+v %v", options, err)
	}
	options, err = operatorListOptionsFromOpts(docopt.Opts{"--operators-refresh": "90m", "--operators-url": "https://list.example/operators.yml"})
	if err != nil || !options.refresh || options.interval != 90*time.Minute || options.url != "https://list.example/operators.yml" {
		t.Fatalf("explicit list options: %+v %v", options, err)
	}
}

func TestAllOperatorsSettingsKeepEachOperatorUnderItsDomain(t *testing.T) {
	opts, err := parseValidatorArgsForTest(t, []string{"run", "--all-operators", "--auto-register", "--hotkey_seed_file=/keys/hot.seed",
		"--state_dir=/synthetic/state", "--concurrency=3", "--operators-refresh=2h"})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := allOperatorsSettingsFromOpts(opts)
	if err != nil {
		t.Fatal(err)
	}
	if settings.hotkeySeedFile != "/keys/hot.seed" || settings.measurement.stateDir != "/synthetic/state" || settings.measurement.concurrency != 3 ||
		!settings.list.refresh || settings.list.interval != 2*time.Hour {
		t.Fatalf("all-operators settings: %+v", settings)
	}
	runner := settings.operatorRunner(operatorTestOperator("one"))
	if runner.stateDir != "/synthetic/state/operators/one.example" || runner.networkPath != "/synthetic/state/operators/one.example/jwt" ||
		runner.apiUrl != "https://api.one.example" || runner.connectUrl != "wss://connect.one.example" || runner.concurrency != 3 {
		t.Fatalf("operator runner settings: %+v", runner)
	}
	if command := settings.authCommand("one.example"); command != "validator auth --operator=one.example --state_dir=/synthetic/state" {
		t.Fatalf("auth command %q", command)
	}
	defaults, err := parseValidatorArgsForTest(t, []string{"run", "--all-operators"})
	if err != nil {
		t.Fatal(err)
	}
	settings, err = allOperatorsSettingsFromOpts(defaults)
	if err != nil || settings.hotkeySeedFile != "" || settings.list.refresh {
		t.Fatalf("default all-operators settings: %+v %v", settings, err)
	}
	if command := settings.authCommand("one.example"); command != "validator auth --operator=one.example" {
		t.Fatalf("default auth command %q", command)
	}
	// Defense behind the usage: the seed file stays required.
	if _, err := allOperatorsSettingsFromOpts(docopt.Opts{"--all-operators": true, "--auto-register": true, "--hotkey_seed_file": "<state_dir>/hotkey.seed", "--state_dir": "/synthetic/state"}); err == nil {
		t.Fatal("auto-register admitted the placeholder hotkey seed file")
	}
}

// Flag mode caches the list and writes the report under --state_dir, runs each
// listed operator from its own directory and joins the runners on shutdown.
func TestAllOperatorsRunFollowsTheListUnderTheStateDir(t *testing.T) {
	stateDir := operatorTestDir(t)
	one := operatorTestOperator("one")
	server := newOperatorTestListServer(t, one)
	settings := allOperatorsSettings{
		measurement: measurementRunSettings{stateDir: stateDir, concurrency: 1, m: 4},
		list:        operatorListOptions{url: server.URL, interval: time.Hour, refresh: true},
	}
	writeOperatorTestJwt(t, settings.operatorRunner(one).networkPath)
	runner, clock, log := newOperatorTestRunner(), newOperatorTestClock(), newOperatorTestLog()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- settings.run(ctx, io.Discard, operatorSupervisorHooks{run: runner.run, after: clock.after, now: clock.Now, log: log.log})
	}()
	run := runner.next(t)
	if run.operator != one || run.settings.stateDir != filepath.Join(stateDir, "operators", "one.example") {
		t.Fatalf("flag-mode runner: %+v %+v", run.operator, run.settings)
	}
	log.wait(t, "1 listed: one.example")
	for _, name := range []string{"operators.yml", "operators-report.json"} {
		if info, err := os.Stat(filepath.Join(stateDir, name)); err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("flag mode did not keep %s under the state dir: %v", name, err)
		}
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(operatorTestTimeout):
		t.Fatal("flag mode did not return after cancellation")
	}
	waitOperatorTestStopped(t, run)
	runner.assertNoMoreRuns(t)
}

// Without observation the production list never loads the hotkey or starts a
// runner; it only caches the list and reports drift.
func TestProductionOperatorListWithoutObservationOnlyReports(t *testing.T) {
	stateDir := operatorTestDir(t)
	one := operatorTestOperator("one")
	server := newOperatorTestListServer(t, one)
	cfg := &ReleaseConfig{StateDir: stateDir, HotkeySeedFile: filepath.Join(stateDir, "absent.seed"), Operators: []OperatorConfig{{NoID: 1, APIURL: one.ApiUrl, ConnectURL: one.ConnectUrl}}}
	runner, log := newOperatorTestRunner(), newOperatorTestLog()
	settings := productionOperatorListSettings{list: operatorListOptions{url: server.URL, interval: time.Hour, refresh: true}}
	list, err := startProductionOperatorList(t.Context(), cfg, settings, io.Discard, operatorSupervisorHooks{run: runner.run, log: log.log})
	if err != nil {
		t.Fatal(err)
	}
	log.wait(t, "pinned no_id 1 https://api.one.example is listed as one.example")
	list.Close()
	runner.assertNoMoreRuns(t)
	if _, err := os.Stat(filepath.Join(stateDir, "observed-operators")); !os.IsNotExist(err) {
		t.Fatal("production without observation created observation state", err)
	}
}

// The production list owns only files under the config's state_dir: the list
// cache, the report and observed-operators. Pinned operators are never run.
func TestProductionOperatorListObservesOnlyUnpinnedOperators(t *testing.T) {
	stateDir := operatorTestDir(t)
	seedPath := filepath.Join(stateDir, "hotkey.seed")
	writeOperatorTestHotkeySeed(t, seedPath, 11)
	pinnedOne, unpinned := operatorTestOperator("one"), operatorTestOperator("two")
	server := newOperatorTestListServer(t, pinnedOne, unpinned)
	cfg := &ReleaseConfig{StateDir: stateDir, HotkeySeedFile: seedPath, Operators: []OperatorConfig{
		{NoID: 1, APIURL: "HTTPS://api.one.example/", ConnectURL: "wss://connect.one.example"},
		{NoID: 2, APIURL: "https://api.gone.example", ConnectURL: "wss://connect.gone.example"},
	}}
	runner, clock, log := newOperatorTestRunner(), newOperatorTestClock(), newOperatorTestLog()
	signIns := 0
	hooks := operatorSupervisorHooks{
		run: runner.run,
		signIn: func(_ context.Context, apiUrl string) (string, error) {
			signIns++
			if apiUrl != unpinned.ApiUrl {
				t.Errorf("signed in to %s", apiUrl)
			}
			return "synthetic-network-jwt", nil
		},
		provision: func(context.Context, measurementRunSettings) (bool, error) { return true, nil },
		after:     clock.after,
		now:       clock.Now,
		log:       log.log,
	}
	settings := productionOperatorListSettings{list: operatorListOptions{url: server.URL, interval: time.Hour, refresh: true}, observe: true, measurement: measurementRunSettings{concurrency: 1, m: 4}}
	list, err := startProductionOperatorList(t.Context(), cfg, settings, io.Discard, hooks)
	if err != nil {
		t.Fatal(err)
	}
	run := runner.next(t)
	directory := filepath.Join(stateDir, "observed-operators", "two.example")
	if run.operator != unpinned || run.settings.stateDir != directory || run.settings.networkPath != filepath.Join(directory, "jwt") || run.settings.concurrency != 1 {
		t.Fatalf("observation runner: %+v %+v", run.operator, run.settings)
	}
	log.wait(t, "listed operator two.example is not pinned by the config")
	raw, err := os.ReadFile(filepath.Join(stateDir, "operators-report.json"))
	if err != nil || !strings.Contains(string(raw), `"domain": "one.example"`) || !strings.Contains(string(raw), `"listed": false`) {
		t.Fatalf("production report: %s %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "operators.yml")); err != nil {
		t.Fatal("production list was not cached under the config state_dir", err)
	}
	list.Close()
	runner.assertNoMoreRuns(t)
	if signIns != 1 {
		t.Fatalf("%d sign-ins", signIns)
	}
}

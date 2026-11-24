//go:build linux || darwin

// validator run without --config runs the home that validator setup made.
// There is no embedded document to fall back on: a missing home is a clear
// error naming the setup command, and every home default yields to a flag.
package validator

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"
)

// A home as setup and activation leave it: the rendered run configuration and
// the storage result. The run handlers below never open the configuration.
func validatorRunTestHome(t *testing.T, activated bool) validatorHome {
	t.Helper()
	t.Setenv("HOME", identityTestStateDir(t))
	home, err := resolveValidatorHome("")
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{home.StorageDir(), home.Activation()} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(home.InitialConfig(), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if activated {
		if err := os.WriteFile(home.RunConfig(), []byte("rendered\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		result := `{"schema":"synthetic","plan":{"path":"/srv/plan.json","sha256":"sha256:` + strings.Repeat("1", 64) + `"},` +
			`"declaration":{"path":"/etc/validator/durable-volumes.json","sha256":"sha256:` + strings.Repeat("2", 64) + `"},"restart_authorized":false}` + "\n"
		if err := os.WriteFile(home.StorageResult(), []byte(result), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func validatorRunTestHandlers(t *testing.T, release func(docopt.Opts, string)) validatorRunHandlers {
	return validatorRunHandlers{
		release:      release,
		measurement:  func(docopt.Opts) { t.Fatal("the home selected measurement") },
		allOperators: func(docopt.Opts) { t.Fatal("the home selected operator-list measurement") },
	}
}

// Before, a bare `validator run` started the embedded template and failed deep
// in production loading with "owner-recycle approved successor selection is
// absent". Now it names the missing home and the command that makes one.
func TestValidatorRunWithoutSetupFailsClearly(t *testing.T) {
	t.Setenv("HOME", identityTestStateDir(t))
	opts, err := parseValidatorArgsForTest(t, []string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	err = dispatchValidatorRun(opts, validatorRunTestHandlers(t, func(docopt.Opts, string) { t.Fatal("run started without a configuration") }))
	if err == nil || !strings.Contains(err.Error(), "no validator is set up in") || !strings.Contains(err.Error(), "validator setup --wallet.name=<wallet> --wallet.hotkey=<hotkey>") ||
		!strings.Contains(err.Error(), filepath.Join(".urnetwork", "sn25-validator")) {
		t.Fatalf("a bare run without a home did not name setup: %v", err)
	}
}

// Setup without activation names the remaining steps instead.
func TestValidatorRunNamesTheMissingActivation(t *testing.T) {
	home := validatorRunTestHome(t, false)
	opts, err := parseValidatorArgsForTest(t, []string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	err = dispatchValidatorRun(opts, validatorRunTestHandlers(t, func(docopt.Opts, string) { t.Fatal("run started before activation") }))
	if err == nil || !strings.Contains(err.Error(), "not activated yet") || !strings.Contains(err.Error(), home.RunConfig()) || !strings.Contains(err.Error(), "validator setup activate") {
		t.Fatalf("run before activation did not name the step: %v", err)
	}
}

// The home supplies the rendered configuration, the storage declaration, the
// progress file, verified startup progress and a 60m steering budget.
func TestValidatorRunUsesTheHomeDefaults(t *testing.T) {
	home := validatorRunTestHome(t, true)
	for _, args := range [][]string{{"run"}, {"run", "--home=" + home.Root()}} {
		opts, err := parseValidatorArgsForTest(t, args)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		err = dispatchValidatorRun(opts, validatorRunTestHandlers(t, func(got docopt.Opts, path string) {
			calls++
			if path != home.RunConfig() {
				t.Fatalf("%v ran %q, not the home's rendered configuration", args, path)
			}
			for key, want := range map[string]string{
				"--durable-volumes": "/etc/validator/durable-volumes.json", "--durable-volumes-sha256": "sha256:" + strings.Repeat("2", 64),
				"--progress-file": home.Progress(), "--steering-operation-timeout": "60m",
			} {
				if optString(got, key, "") != want {
					t.Fatalf("%v: %s is %q, want the home default %q", args, key, optString(got, key, ""), want)
				}
			}
			if !optBool(got, "--startup-verified-progress") {
				t.Fatalf("%v: verified startup progress is off", args)
			}
		}))
		if err != nil || calls != 1 {
			t.Fatalf("%v: release calls=%d, error=%v", args, calls, err)
		}
	}
}

// Every home default yields to the operator's flag.
func TestValidatorRunFlagsOverrideTheHome(t *testing.T) {
	home := validatorRunTestHome(t, true)
	opts, err := parseValidatorArgsForTest(t, []string{"run", "--progress-file=/var/lib/validator/progress.json", "--steering-operation-timeout=90m",
		"--durable-volumes=/etc/other/durable-volumes.json", "--durable-volumes-sha256=sha256:" + strings.Repeat("3", 64)})
	if err != nil {
		t.Fatal(err)
	}
	err = dispatchValidatorRun(opts, validatorRunTestHandlers(t, func(got docopt.Opts, path string) {
		if path != home.RunConfig() || optString(got, "--progress-file", "") != "/var/lib/validator/progress.json" || optString(got, "--steering-operation-timeout", "") != "90m" ||
			optString(got, "--durable-volumes", "") != "/etc/other/durable-volumes.json" || optString(got, "--durable-volumes-sha256", "") != "sha256:"+strings.Repeat("3", 64) {
			t.Fatalf("a home default replaced a flag: %v", got)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
}

// A home without its storage result cannot start: the declaration is required.
func TestValidatorRunNeedsTheHomeStorageDeclaration(t *testing.T) {
	home := validatorRunTestHome(t, true)
	if err := os.Remove(home.StorageResult()); err != nil {
		t.Fatal(err)
	}
	opts, err := parseValidatorArgsForTest(t, []string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	err = dispatchValidatorRun(opts, validatorRunTestHandlers(t, func(docopt.Opts, string) { t.Fatal("run started without a storage declaration") }))
	if err == nil || !strings.Contains(err.Error(), "validator setup storage") {
		t.Fatalf("a home without prepared storage did not name the step: %v", err)
	}
}

// An explicit --config is the whole document; it never falls back to a home.
func TestValidatorRunExplicitConfigNeverDispatchesDefaults(t *testing.T) {
	validatorRunTestHome(t, true)
	path := filepath.Join(t.TempDir(), "missing.yml")
	opts, err := parseValidatorArgsForTest(t, []string{"run", "--config=" + path})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	handlers := validatorRunTestHandlers(t, func(got docopt.Opts, selected string) {
		calls++
		if selected != path || optString(got, "--progress-file", "") != "" || optBool(got, "--startup-verified-progress") {
			t.Fatalf("explicit config path or flags changed: %q", selected)
		}
		if cfg, err := LoadReleaseConfig(selected); cfg != nil || !os.IsNotExist(err) {
			t.Fatalf("missing explicit config fell back: %v", err)
		}
	})
	if err := dispatchValidatorRun(opts, handlers); err != nil || calls != 1 {
		t.Fatalf("explicit release calls=%d, error=%v", calls, err)
	}
	for _, args := range [][]string{{"run", "--config="}, {"run", "--config=   "}, {"run", "--home="}} {
		opts, err := parseValidatorArgsForTest(t, args)
		if err != nil {
			t.Fatal(err)
		}
		if err := dispatchValidatorRun(opts, handlers); err == nil || calls != 1 {
			t.Fatal("an explicit empty path selected a runner", args, err)
		}
	}
	if _, err := parseValidatorArgsForTest(t, []string{"run", "--config=" + path, "--home=/srv/validator"}); err == nil {
		t.Fatal("--config and --home parsed together")
	}
}

// There is no implicit document left for any loader.
func TestReleaseConfigHasNoImplicitDocument(t *testing.T) {
	if _, _, err := readReleaseConfigFile(""); err == nil {
		t.Fatal("an empty path selected a document")
	}
	if cfg, err := LoadReleaseConfig(""); cfg != nil || err == nil {
		t.Fatalf("an empty path loaded a configuration: %v", err)
	}
	if cfg, err := decodeOperatorListReleaseConfig(""); cfg != nil || err == nil {
		t.Fatalf("the operator list decoded an implicit document: %v", err)
	}
}

// Decoding for the operator list keeps the explicit document's own state.
func TestOperatorListReleaseConfigPreservesDocumentSemantics(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yml")
	if cfg, err := decodeOperatorListReleaseConfig(missing); cfg != nil || !os.IsNotExist(err) {
		t.Fatalf("operator list fell back from a missing explicit config: %v", err)
	}
	want := validReleaseConfig(t)
	for index := range want.EvidenceV2.Operators {
		want.EvidenceV2.Operators[index] = ReleaseEvidenceV2OperatorConfig{NoID: want.EvidenceV2.Operators[index].NoID}
	}
	pending := writeReleaseConfig(t, want)
	got, err := decodeOperatorListReleaseConfig(pending)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeploymentID != want.DeploymentID || got.StateDir != want.StateDir ||
		len(got.Operators) != len(want.Operators) || got.Operators[1].NoID != want.Operators[1].NoID {
		t.Fatal("operator list did not use the complete explicit replacement")
	}
	if _, err := LoadReleaseConfig(pending); !errors.Is(err, ErrReleaseEvidenceV2ActivationPending) {
		t.Fatalf("operator-list decode lost the lifecycle's pending-activation state: %v", err)
	}
}

func TestValidatorRunMeasurementModesRemainExplicit(t *testing.T) {
	for _, flag := range []string{"--measurement-only", "--all-operators"} {
		opts, err := parseValidatorArgsForTest(t, []string{"run", flag})
		if err != nil {
			t.Fatal(err)
		}
		var selected string
		handlers := validatorRunHandlers{
			release:      func(docopt.Opts, string) { t.Fatal("measurement acquired release dispatch") },
			measurement:  func(docopt.Opts) { selected = "--measurement-only" },
			allOperators: func(docopt.Opts) { selected = "--all-operators" },
		}
		if err := dispatchValidatorRun(opts, handlers); err != nil || selected != flag {
			t.Fatalf("mode %s selected %s: %v", flag, selected, err)
		}
	}
	for _, args := range [][]string{
		{"run", "--measurement-only", "--config=release.yml"},
		{"run", "--all-operators", "--config=release.yml"},
		{"run", "--measurement-only", "--home=/srv/validator"},
		{"run", "--all-operators", "--home=/srv/validator"},
		{"run", "--measurement-only", "--all-operators"},
		{"run", "--measurement-only", "--ledger_auditor_key_file=auditor.key"},
		{"run", "--measurement-only", "--steering-operation-timeout=35m"},
		{"run", "--all-operators", "--steering-operation-timeout=35m"},
		{"run", "--measurement-only", "--substrate=ws://127.0.0.1:9944", "--netuid=7"},
		{"run", "--api_url=http://127.0.0.1:8080"},
	} {
		if _, err := parseValidatorArgsForTest(t, args); err == nil {
			t.Fatal("mixed or implicit measurement arguments parsed", args)
		}
	}
}

func TestValidatorRunHomeDoesNotChangeOtherCommands(t *testing.T) {
	for _, args := range [][]string{
		{"status"},
		{"init", "--state_dir=/synthetic/validator"},
	} {
		opts, err := parseValidatorArgsForTest(t, args)
		if err != nil {
			t.Fatal(err)
		}
		// Empty handlers would panic if another command entered a run mode.
		if err := dispatchValidatorRun(opts, validatorRunHandlers{}); err == nil {
			t.Fatal("another command acquired release dispatch", args)
		}
	}
	if _, err := parseValidatorArgsForTest(t, []string{"register", "--coldkey_seed_file=/synthetic/cold.seed"}); err == nil {
		t.Fatal("register lost its explicit config requirement")
	}
}

package validator

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"
)

func TestOmittedReleaseConfigReadsBundledDocument(t *testing.T) {
	want, err := os.ReadFile("release.yml")
	if err != nil {
		t.Fatal(err)
	}
	state, err := defaultStateDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.WriteFile("release.yml", []byte("unexpected working-directory document\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, data, err := readReleaseConfigFile("")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(state, "release.yml") || !bytes.Equal(data, want) {
		t.Fatalf("default did not use the bundled document and state-dir anchor: %s", path)
	}
	data[0] ^= 1
	_, again, err := readReleaseConfigFile("")
	if err != nil || !bytes.Equal(again, want) {
		t.Fatal("caller mutated the embedded default")
	}
	cfg, err := decodeReleaseConfigDocument(path, again)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != state || cfg.HotkeySeedFile != filepath.Join(state, "hotkey.seed") ||
		len(cfg.Operators) != 1 || cfg.Operators[0].StateDir != filepath.Join(state, "no-1") ||
		cfg.Operators[0].ClientKeySeedFile != filepath.Join(state, "no-1", "client.key") {
		t.Fatal("bundled custody paths depend on the caller's working directory")
	}
}

func TestExplicitReleaseConfigNeverFallsBackOrMerges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.yml")
	if _, _, err := readReleaseConfigFile(path); !os.IsNotExist(err) {
		t.Fatalf("missing explicit file fell back: %v", err)
	}
	want := []byte("explicit configuration bytes\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	_, got, err := readReleaseConfigFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("explicit file changed: %q, %v", got, err)
	}
	if _, _, err := readReleaseConfigFile("   "); err == nil {
		t.Fatal("an explicitly invalid path selected defaults")
	}
}

func TestBundledReleaseConfigDoesNotBypassProductionValidation(t *testing.T) {
	if cfg, err := LoadReleaseConfig(""); cfg != nil || err == nil ||
		!strings.Contains(err.Error(), "owner-recycle approved successor selection is absent") {
		t.Fatalf("bundled document did not reach the normal independent-authority refusal: %v", err)
	}
}

func TestValidatorRunOmittedConfigDispatchesEmbeddedRelease(t *testing.T) {
	want, err := os.ReadFile("release.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"run"},
		{"run", "--progress-file=/synthetic/progress.json"},
		{"run", "--operators-refresh=1m"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			opts, err := parseValidatorArgsForTest(t, args)
			if err != nil {
				t.Fatal(err)
			}
			releaseCalls := 0
			handlers := validatorRunHandlers{
				release: func(_ docopt.Opts, path string) {
					releaseCalls++
					if path != "" {
						t.Fatalf("omission became a filesystem lookup: %q", path)
					}
					_, got, err := readReleaseConfigFile(path)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("run did not select the embedded document: %v", err)
					}
					if optString(opts, "--operators-refresh", "") != "" {
						if cfg, err := decodeOperatorListReleaseConfig(path); err != nil || cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion {
							t.Fatalf("operator-list path did not decode the embedded release: %v", err)
						}
					}
					if cfg, err := LoadReleaseConfig(path); cfg != nil || err == nil ||
						!strings.Contains(err.Error(), "owner-recycle approved successor selection is absent") {
						t.Fatalf("run skipped independent-authority validation: %v", err)
					}
				},
				measurement:  func(docopt.Opts) { t.Fatal("omission selected measurement mode") },
				allOperators: func(docopt.Opts) { t.Fatal("omission selected the operator-list runner") },
			}
			if err := dispatchValidatorRun(opts, handlers); err != nil || releaseCalls != 1 {
				t.Fatalf("run release calls=%d, error=%v", releaseCalls, err)
			}
		})
	}
}

func TestValidatorRunExplicitConfigNeverDispatchesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yml")
	opts, err := parseValidatorArgsForTest(t, []string{"run", "--config=" + path})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	handlers := validatorRunHandlers{
		release: func(_ docopt.Opts, got string) {
			calls++
			if got != path {
				t.Fatalf("explicit config path changed: %q", got)
			}
			if cfg, err := LoadReleaseConfig(got); cfg != nil || !os.IsNotExist(err) {
				t.Fatalf("missing explicit config selected embedded defaults: %v", err)
			}
		},
		measurement:  func(docopt.Opts) { t.Fatal("explicit config selected measurement") },
		allOperators: func(docopt.Opts) { t.Fatal("explicit config selected operator-list measurement") },
	}
	if err := dispatchValidatorRun(opts, handlers); err != nil || calls != 1 {
		t.Fatalf("explicit release calls=%d, error=%v", calls, err)
	}
	for _, value := range []string{"", "   "} {
		opts, err := parseValidatorArgsForTest(t, []string{"run", "--config=" + value})
		if err != nil {
			t.Fatal(err)
		}
		if err := dispatchValidatorRun(opts, handlers); err == nil || calls != 1 {
			t.Fatal("explicit empty config selected a runner", value, err)
		}
	}
}

func TestOperatorListReleaseConfigPreservesDocumentSemantics(t *testing.T) {
	anchor, bundled, err := readReleaseConfigFile("")
	if err != nil {
		t.Fatal(err)
	}
	unsigned := filepath.Join(t.TempDir(), "release.yml")
	if err := os.WriteFile(unsigned, bundled, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", unsigned} {
		state := filepath.Dir(anchor)
		if path != "" {
			state = filepath.Dir(path)
		}
		cfg, err := decodeOperatorListReleaseConfig(path)
		if err != nil || cfg.StateDir != state || cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion || len(cfg.Operators) != 1 {
			t.Fatalf("operator list did not decode the exact document for %q: %v", path, err)
		}
		// Document-only decoding grants no authority; the actual release
		// lifecycle still applies the production loader to this same path.
		if cfg, err := LoadReleaseConfig(path); cfg != nil || err == nil ||
			!strings.Contains(err.Error(), "owner-recycle approved successor selection is absent") {
			t.Fatalf("document decode bypassed the production loader: %v", err)
		}
	}
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
			release:     func(docopt.Opts, string) { t.Fatal("measurement acquired release dispatch") },
			measurement: func(docopt.Opts) { selected = "--measurement-only" },
			allOperators: func(docopt.Opts) {
				selected = "--all-operators"
			},
		}
		if err := dispatchValidatorRun(opts, handlers); err != nil || selected != flag {
			t.Fatalf("mode %s selected %s: %v", flag, selected, err)
		}
	}
	for _, args := range [][]string{
		{"run", "--measurement-only", "--config=release.yml"},
		{"run", "--all-operators", "--config=release.yml"},
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

func TestEmbeddedReleaseDefaultDoesNotChangeOtherCommands(t *testing.T) {
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

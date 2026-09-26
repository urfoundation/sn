//go:build linux || darwin

package validator

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/crv4"
)

func TestNativeCommandUsageForms(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		key  string
	}{
		{name: "init config", args: []string{"init", "--config=/etc/ur-validator/release.yml"}, key: "init"},
		{name: "init state dir with operators", args: []string{"init", "--state_dir=/var/lib/ur-validator", "--no_id=1", "--no_id=2"}, key: "init"},
		{name: "register dry run", args: []string{"register", "--config=r.yml", "--coldkey_seed_file=cold.seed"}, key: "register"},
		{name: "register apply", args: []string{"register", "--config=r.yml", "--coldkey_seed_file=cold.seed", "--burn_limit_rao=1500000000", "--fee_limit_rao=5000000", "--apply"}, key: "register"},
		{name: "stake add", args: []string{"stake", "add", "--amount_rao=1000000000", "--config=r.yml", "--coldkey_seed_file=cold.seed", "--limit_price_rao=2000000000", "--allow_partial", "--apply"}, key: "stake"},
		{name: "activate", args: []string{"activate", "--config=r.yml", "--relayer_key_file=relay.key", "--apply"}, key: "activate"},
		{name: "activate dry run", args: []string{"activate", "--config=r.yml", "--dry-run"}, key: "activate"},
	} {
		opts, err := parseValidatorArgsForTest(t, testCase.args)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		if selected, _ := opts.Bool(testCase.key); !selected {
			t.Fatalf("%s: command %q not selected: %v", testCase.name, testCase.key, opts)
		}
	}
	opts, err := parseValidatorArgsForTest(t, []string{"init", "--state_dir=/var/lib/ur-validator", "--no_id=1", "--no_id=2"})
	if err != nil {
		t.Fatal(err)
	}
	if got := optStringList(opts, "--no_id"); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("--no_id list = %v", got)
	}
	// The shared docopt defaults are placeholders, never literal paths.
	if stateDir, hotkey := initSeedPaths(opts); stateDir != "/var/lib/ur-validator" || hotkey != "/var/lib/ur-validator/hotkey.seed" {
		t.Fatalf("init paths = %q %q", stateDir, hotkey)
	}
	opts, err = parseValidatorArgsForTest(t, []string{"init", "--state_dir=/var/lib/ur-validator", "--hotkey_seed_file=/keys/hot.seed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, hotkey := initSeedPaths(opts); hotkey != "/keys/hot.seed" {
		t.Fatalf("explicit hotkey path = %q", hotkey)
	}
	opts, err = parseValidatorArgsForTest(t, []string{"init", "--config=/etc/ur-validator/release.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if stateDir, _ := initSeedPaths(opts); stateDir != "" {
		t.Fatalf("config form resolved a state dir from the default placeholder: %q", stateDir)
	}
	opts, err = parseValidatorArgsForTest(t, []string{"register", "--config=r.yml", "--coldkey_seed_file=cold.seed"})
	if err != nil {
		t.Fatal(err)
	}
	if optUint64(opts, "--fee_limit_rao", 0) != defaultNativeFeeLimitRao || optUint64(opts, "--burn_limit_rao", 0) != 0 || optBool(opts, "--apply") {
		t.Fatalf("register defaults = %v", opts)
	}
	for _, args := range [][]string{
		{"register", "--config=r.yml"},
		{"register", "--config=r.yml", "--coldkey_seed_file=cold.seed", "--apply", "--dry-run"},
		{"stake", "add", "--config=r.yml", "--coldkey_seed_file=cold.seed"},
		{"init"},
	} {
		if _, err := parseValidatorArgsForTest(t, args); err == nil {
			t.Fatalf("%v parsed without its required options", args)
		}
	}
}

func TestInitCreatesPrivateSeedsForConfigAndStateDirForms(t *testing.T) {
	path := writeReleaseConfig(t, unrenderedReleaseConfig(t))
	// The loaded configuration carries the normalized per-operator seed paths
	// init must honor.
	cfg, err := LoadReleaseConfigPreActivation(path)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "init-*")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := runInit(docopt.Opts{"--config": path}, output); err != nil {
		t.Fatal(err)
	}
	seed, err := crv4.LoadSeedFile(cfg.HotkeySeedFile)
	if err != nil {
		t.Fatal(err)
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	printed, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(printed), "hotkey ss58: "+hotkey.Address()) {
		t.Fatalf("init output lacks the hotkey ss58:\n%s", printed)
	}
	for _, operator := range cfg.Operators {
		clientSeed, err := loadClientSeed(operator.ClientKeySeedFile)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(operator.ClientKeySeedFile)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("client seed mode = %v, %v", info, err)
		}
		vpk := ed25519.NewKeyFromSeed(clientSeed).Public().(ed25519.PublicKey)
		if !strings.Contains(string(printed), "vpk (no_id "+itoa(operator.NoID)+"): 0x"+hexString(vpk)) {
			t.Fatalf("init output lacks the no_id %d vpk:\n%s", operator.NoID, printed)
		}
	}
	// Re-running never replaces an existing seed.
	if err := runInit(docopt.Opts{"--config": path}, output); err != nil {
		t.Fatal(err)
	}
	again, err := crv4.LoadSeedFile(cfg.HotkeySeedFile)
	if err != nil || again != seed {
		t.Fatal("init replaced an existing hotkey seed")
	}
	// The state-dir form lays operators out beside the hotkey seed.
	stateDir := filepath.Join(t.TempDir(), "validator")
	if err := runInit(docopt.Opts{"--state_dir": stateDir, "--no_id": []string{"1", "3"}}, output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hotkey.seed", filepath.Join("no-1", "client.key"), filepath.Join("no-3", "client.key")} {
		info, err := os.Stat(filepath.Join(stateDir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
	if err := runInit(docopt.Opts{"--state_dir": stateDir, "--no_id": []string{"0"}}, output); err == nil {
		t.Fatal("no_id 0 accepted")
	}
}

func itoa(value uint64) string {
	return strings.TrimSpace(strings.Replace(string(rune('0'+value)), "\x00", "", -1))
}

func hexString(value []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(value)*2)
	for _, b := range value {
		out = append(out, digits[b>>4], digits[b&0x0f])
	}
	return string(out)
}

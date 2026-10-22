package onchain

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// v2WatcherUnit reads a launch example unit's settings, key by key.
func v2WatcherUnit(t *testing.T, name string) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile("../../mainnet/" + name)
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string][]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			t.Fatalf("%s: %q is not a setting", name, line)
		}
		settings[key] = append(settings[key], value)
	}
	return settings
}

// CL-1, VERSION2-LAUNCH.md step 13: the watcher unit's ExecStart parses with
// snclaim's real usage as `v2 check --all` with --challenge, --demand and the
// --key_file they need; without the key file check refuses before any RPC.
// The timer starts it hourly: about 300 blocks at 12 seconds, so vault2's
// 14,400-block challenge window holds 48 runs.
func TestV2WatcherUnitExample(t *testing.T) {
	service := v2WatcherUnit(t, "settlement-v2-watcher.example.service")
	if !slices.Equal(service["Type"], []string{"oneshot"}) || !slices.Equal(service["User"], []string{"snclaim-watcher"}) || len(service["ExecStart"]) != 1 {
		t.Fatalf("watcher service profile: %v", service)
	}
	argv := strings.Fields(service["ExecStart"][0])
	opts, err := parseArgv(t, argv[1:])
	if err != nil || argv[0] != "/usr/local/bin/snclaim" || !boolOpt(opts, "v2") || !boolOpt(opts, "check") || !boolOpt(opts, "--all") || !boolOpt(opts, "--challenge") || !boolOpt(opts, "--demand") || boolOpt(opts, "--dry-run") ||
		strOpt(opts, "--key_file") != "<O21-watcher-key-file>" || strOpt(opts, "--vault") != "<vault2-address>" || strOpt(opts, "--no_id") != "1" || !slices.Equal(strsOpt(opts, "--rpc"), []string{"<O21-evm-rpc-url>"}) ||
		strOpt(opts, "--api_url") != "<operator-api-url>" || strOpt(opts, "--chain_id") != "964" {
		t.Fatalf("watcher ExecStart %q = %v, %v", argv, opts, err)
	}
	filled := strings.NewReplacer("<vault2-address>", v2TestVault, "<O21-evm-rpc-url>", "http://127.0.0.1:1", "<operator-api-url>", "https://api.example").Replace(service["ExecStart"][0])
	opts, err = parseArgv(t, slices.DeleteFunc(strings.Fields(filled)[1:], func(arg string) bool { return strings.HasPrefix(arg, "--key_file=") }))
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdV2Check(t.Context(), opts); err == nil || err.Error() != "--challenge and --demand send transactions: --key_file is required" {
		t.Fatalf("watcher without its key file: %v", err)
	}
	timer := v2WatcherUnit(t, "settlement-v2-watcher.example.timer")
	period := map[string]time.Duration{"hourly": time.Hour}[strings.Join(timer["OnCalendar"], ",")]
	if period == 0 || !slices.Equal(timer["Persistent"], []string{"true"}) || !slices.Equal(timer["Unit"], []string{"sn-mainnet-settlement-v2-watcher.service"}) || 14_400*12*time.Second/period < 48 {
		t.Fatalf("watcher timer: %v", timer)
	}
}

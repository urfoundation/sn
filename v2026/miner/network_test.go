package miner

// network_test.go — the saved custom network: validation, precedence,
// round-trip, reset, and the failure modes an operator can actually
// produce by hand-editing network.json.
//
// Every test drives an explicit t.TempDir(). Overriding $HOME would not
// work on Windows (os.UserHomeDir reads %USERPROFILE% there), and the
// reset tests would then delete the real ~/.urnetwork/network.json of
// whoever ran `go test`.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateApiUrl(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"https://example.com", false},
		{"http://example.com", false},
		{"ws://example.com", true},
		{"wss://example.com", true},
		{"ftp://example.com", true},
		{"https://", true},
		{"not a url", true},
		{"", true},
	}
	for _, c := range cases {
		err := validateApiUrl(c.url)
		if c.wantErr && err == nil {
			t.Errorf("validateApiUrl(%q): expected error, got nil", c.url)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateApiUrl(%q): unexpected error: %s", c.url, err)
		}
	}
}

func TestValidateConnectUrl(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"ws://example.com", false},
		{"wss://example.com", false},
		{"http://example.com", true},
		{"https://example.com", true},
		{"ftp://example.com", true},
		{"wss://", true},
		{"not a url", true},
		{"", true},
	}
	for _, c := range cases {
		err := validateConnectUrl(c.url)
		if c.wantErr && err == nil {
			t.Errorf("validateConnectUrl(%q): expected error, got nil", c.url)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateConnectUrl(%q): unexpected error: %s", c.url, err)
		}
	}
}

func TestInsecureNetworkWarning(t *testing.T) {
	cases := []struct {
		name       string
		apiUrl     string
		connectUrl string
		wantWarn   bool
	}{
		{"both encrypted", "https://example.com", "wss://example.com", false},
		{"plaintext api", "http://example.com", "wss://example.com", true},
		{"plaintext connect", "https://example.com", "ws://example.com", true},
		{"both plaintext", "http://example.com", "ws://example.com", true},
		// https:// must not trip the http:// check on a prefix match
		{"https is not http", "https://http.example.com", "wss://ws.example.com", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warning := insecureNetworkWarning(c.apiUrl, c.connectUrl)
			if c.wantWarn && warning == "" {
				t.Errorf("insecureNetworkWarning(%q, %q) = \"\", want a warning", c.apiUrl, c.connectUrl)
			}
			if !c.wantWarn && warning != "" {
				t.Errorf("insecureNetworkWarning(%q, %q) = %q, want no warning", c.apiUrl, c.connectUrl, warning)
			}
		})
	}
}

func TestNetworkSwitchNoticeOnlyWhenAJwtExists(t *testing.T) {
	dir := t.TempDir()

	// a fresh install has no jwt to invalidate, so switching is silent
	if notice := networkSwitchNotice(dir); notice != "" {
		t.Errorf("networkSwitchNotice on an empty dir = %q, want \"\"", notice)
	}

	if err := os.WriteFile(filepath.Join(dir, "jwt"), []byte("jwt"), 0600); err != nil {
		t.Fatal(err)
	}
	if notice := networkSwitchNotice(dir); notice == "" {
		t.Error("networkSwitchNotice with a jwt present = \"\", want a notice")
	}
}

func TestReadNetworkConfigMissing(t *testing.T) {
	_, ok, err := readNetworkConfig(t.TempDir())
	if err != nil {
		t.Fatalf("readNetworkConfig: unexpected error: %s", err)
	}
	if ok {
		t.Fatalf("readNetworkConfig: expected ok=false for missing file")
	}
}

func TestWriteThenReadNetworkConfig(t *testing.T) {
	dir := t.TempDir()
	if err := writeNetworkConfig(dir, "https://example.com", "wss://example.com"); err != nil {
		t.Fatalf("writeNetworkConfig: unexpected error: %s", err)
	}
	cfg, ok, err := readNetworkConfig(dir)
	if err != nil {
		t.Fatalf("readNetworkConfig: unexpected error: %s", err)
	}
	if !ok {
		t.Fatalf("readNetworkConfig: expected ok=true after write")
	}
	if cfg.ApiUrl != "https://example.com" {
		t.Errorf("ApiUrl = %q, want %q", cfg.ApiUrl, "https://example.com")
	}
	if cfg.ConnectUrl != "wss://example.com" {
		t.Errorf("ConnectUrl = %q, want %q", cfg.ConnectUrl, "wss://example.com")
	}

	// the write lands on the documented name, and leaves no temp file behind
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != networkConfigFileName {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir contents = %v, want only [%s]", names, networkConfigFileName)
	}
}

func TestNetworkConfigFileIsNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	dir := t.TempDir()
	if err := writeNetworkConfig(dir, "https://example.com", "wss://example.com"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, networkConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	// this file names the endpoint the provider's credentials are sent to
	if perm := info.Mode().Perm(); perm&0077 != 0 {
		t.Errorf("network.json mode = %o, want no group/other access", perm)
	}
}

func TestWriteNetworkConfigRejectsBadUrls(t *testing.T) {
	dir := t.TempDir()
	if err := writeNetworkConfig(dir, "ws://example.com", "wss://example.com"); err == nil {
		t.Fatalf("writeNetworkConfig: expected error for bad api_url scheme")
	}
	if err := writeNetworkConfig(dir, "https://example.com", "https://example.com"); err == nil {
		t.Fatalf("writeNetworkConfig: expected error for bad connect_url scheme")
	}
	// Nothing should have been written.
	_, ok, err := readNetworkConfig(dir)
	if err != nil {
		t.Fatalf("readNetworkConfig: unexpected error: %s", err)
	}
	if ok {
		t.Fatalf("readNetworkConfig: expected ok=false after rejected write")
	}
}

// TestReadNetworkConfigRejectsIncompleteFiles covers the hand-edited
// file that parses as JSON but does not describe a usable network.
// Returning it would hand an empty base URL to the api client and fail
// somewhere far away from the cause.
func TestReadNetworkConfigRejectsIncompleteFiles(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"empty object", `{}`},
		{"json null", `null`},
		{"empty file", ``},
		{"missing connect_url", `{"api_url": "https://example.com"}`},
		{"missing api_url", `{"connect_url": "wss://example.com"}`},
		{"empty api_url", `{"api_url": "", "connect_url": "wss://example.com"}`},
		{"wrong api_url scheme", `{"api_url": "ws://example.com", "connect_url": "wss://example.com"}`},
		{"wrong connect_url scheme", `{"api_url": "https://example.com", "connect_url": "https://example.com"}`},
		{"truncated json", `{"api_url": "https://exa`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, networkConfigFileName), []byte(c.content), 0600); err != nil {
				t.Fatal(err)
			}
			_, ok, err := readNetworkConfig(dir)
			if err == nil {
				t.Fatalf("readNetworkConfig(%s): expected error, got nil", c.content)
			}
			if ok {
				t.Errorf("readNetworkConfig(%s): ok=true alongside an error", c.content)
			}
		})
	}
}

func TestResetNetworkConfig(t *testing.T) {
	dir := t.TempDir()

	// Reset on a missing file is a no-op, not an error.
	if err := resetNetworkConfig(dir); err != nil {
		t.Fatalf("resetNetworkConfig on missing file: unexpected error: %s", err)
	}

	if err := writeNetworkConfig(dir, "https://example.com", "wss://example.com"); err != nil {
		t.Fatalf("writeNetworkConfig: unexpected error: %s", err)
	}
	if err := resetNetworkConfig(dir); err != nil {
		t.Fatalf("resetNetworkConfig: unexpected error: %s", err)
	}
	_, ok, err := readNetworkConfig(dir)
	if err != nil {
		t.Fatalf("readNetworkConfig: unexpected error: %s", err)
	}
	if ok {
		t.Fatalf("readNetworkConfig: expected ok=false after reset")
	}
}

// TestWriteNetworkConfigOverwrites guards the rename in writeFileAtomic:
// writing over an existing config must replace it, not fail.
func TestWriteNetworkConfigOverwrites(t *testing.T) {
	dir := t.TempDir()
	if err := writeNetworkConfig(dir, "https://first.example.com", "wss://first.example.com"); err != nil {
		t.Fatalf("writeNetworkConfig: unexpected error: %s", err)
	}
	if err := writeNetworkConfig(dir, "https://second.example.com", "wss://second.example.com"); err != nil {
		t.Fatalf("writeNetworkConfig (overwrite): unexpected error: %s", err)
	}
	cfg, ok, err := readNetworkConfig(dir)
	if err != nil || !ok {
		t.Fatalf("readNetworkConfig: ok=%t err=%v", ok, err)
	}
	if cfg.ApiUrl != "https://second.example.com" {
		t.Errorf("ApiUrl = %q, want the second write", cfg.ApiUrl)
	}
}

func TestProviderStatePathIsUnderTheStateDir(t *testing.T) {
	dir, err := providerStateDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %s", err)
	}
	p, err := providerStatePath(networkConfigFileName)
	if err != nil {
		t.Fatalf("providerStatePath: unexpected error: %s", err)
	}
	if want := filepath.Join(dir, networkConfigFileName); p != want {
		t.Errorf("providerStatePath = %q, want %q", p, want)
	}
	if filepath.Base(dir) != ".urnetwork" {
		t.Errorf("providerStateDir = %q, want it to end in .urnetwork", dir)
	}
}

func TestResolveApiUrlPrecedence(t *testing.T) {
	dir := t.TempDir()

	// Neither flag nor saved config: falls back to DefaultApiUrl.
	opts := parseArgsForTest(t, []string{"provide"})
	got, err := resolveApiUrlIn(opts, dir)
	if err != nil {
		t.Fatalf("resolveApiUrlIn: unexpected error: %s", err)
	}
	if got != DefaultApiUrl {
		t.Errorf("resolveApiUrlIn (no flag, no saved) = %q, want %q", got, DefaultApiUrl)
	}

	// Saved config present, no flag: saved config wins.
	if err := writeNetworkConfig(dir, "https://saved.example.com", "wss://saved.example.com"); err != nil {
		t.Fatalf("writeNetworkConfig: unexpected error: %s", err)
	}
	got, err = resolveApiUrlIn(opts, dir)
	if err != nil {
		t.Fatalf("resolveApiUrlIn: unexpected error: %s", err)
	}
	if got != "https://saved.example.com" {
		t.Errorf("resolveApiUrlIn (saved, no flag) = %q, want %q", got, "https://saved.example.com")
	}

	// Flag present: flag wins over saved config.
	flagOpts := parseArgsForTest(t, []string{"provide", "--api_url=https://flag.example.com"})
	got, err = resolveApiUrlIn(flagOpts, dir)
	if err != nil {
		t.Fatalf("resolveApiUrlIn: unexpected error: %s", err)
	}
	if got != "https://flag.example.com" {
		t.Errorf("resolveApiUrlIn (flag) = %q, want %q", got, "https://flag.example.com")
	}
}

func TestResolveConnectUrlPrecedence(t *testing.T) {
	dir := t.TempDir()

	opts := parseArgsForTest(t, []string{"provide"})
	got, err := resolveConnectUrlIn(opts, dir)
	if err != nil {
		t.Fatalf("resolveConnectUrlIn: unexpected error: %s", err)
	}
	if got != DefaultConnectUrl {
		t.Errorf("resolveConnectUrlIn (no flag, no saved) = %q, want %q", got, DefaultConnectUrl)
	}

	if err := writeNetworkConfig(dir, "https://saved.example.com", "wss://saved.example.com"); err != nil {
		t.Fatalf("writeNetworkConfig: unexpected error: %s", err)
	}
	got, err = resolveConnectUrlIn(opts, dir)
	if err != nil {
		t.Fatalf("resolveConnectUrlIn: unexpected error: %s", err)
	}
	if got != "wss://saved.example.com" {
		t.Errorf("resolveConnectUrlIn (saved, no flag) = %q, want %q", got, "wss://saved.example.com")
	}

	flagOpts := parseArgsForTest(t, []string{"provide", "--connect_url=wss://flag.example.com"})
	got, err = resolveConnectUrlIn(flagOpts, dir)
	if err != nil {
		t.Fatalf("resolveConnectUrlIn: unexpected error: %s", err)
	}
	if got != "wss://flag.example.com" {
		t.Errorf("resolveConnectUrlIn (flag) = %q, want %q", got, "wss://flag.example.com")
	}
}

// TestResolveUrlFlagWinsOverACorruptConfig: an explicit flag must not be
// held hostage by a config file it is overriding anyway. This is the
// escape hatch an operator reaches for once the config is broken.
func TestResolveUrlFlagWinsOverACorruptConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, networkConfigFileName), []byte("{not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	opts := parseArgsForTest(t, []string{
		"provide",
		"--api_url=https://flag.example.com",
		"--connect_url=wss://flag.example.com",
	})
	apiUrl, err := resolveApiUrlIn(opts, dir)
	if err != nil || apiUrl != "https://flag.example.com" {
		t.Errorf("resolveApiUrlIn = %q, err %v; want the flag value", apiUrl, err)
	}
	connectUrl, err := resolveConnectUrlIn(opts, dir)
	if err != nil || connectUrl != "wss://flag.example.com" {
		t.Errorf("resolveConnectUrlIn = %q, err %v; want the flag value", connectUrl, err)
	}
}

func TestChooseNetworkCmdSaves(t *testing.T) {
	dir := t.TempDir()
	opts := parseArgsForTest(t, []string{"choose_network", "https://example.com", "wss://example.com"})
	if err := chooseNetworkCmdIn(opts, dir); err != nil {
		t.Fatalf("chooseNetworkCmdIn: unexpected error: %s", err)
	}

	cfg, ok, err := readNetworkConfig(dir)
	if err != nil {
		t.Fatalf("readNetworkConfig: unexpected error: %s", err)
	}
	if !ok {
		t.Fatalf("expected network config to be saved")
	}
	if cfg.ApiUrl != "https://example.com" || cfg.ConnectUrl != "wss://example.com" {
		t.Fatalf("saved config = %+v, want api_url=https://example.com connect_url=wss://example.com", cfg)
	}
}

func TestChooseNetworkCmdRejectsBadUrls(t *testing.T) {
	dir := t.TempDir()
	opts := parseArgsForTest(t, []string{"choose_network", "https://example.com", "https://example.com"})
	if err := chooseNetworkCmdIn(opts, dir); err == nil {
		t.Fatalf("chooseNetworkCmdIn: expected an error for a bad connect_url scheme")
	}
	if _, ok, _ := readNetworkConfig(dir); ok {
		t.Errorf("a rejected choose_network must not leave a config behind")
	}
}

func TestChooseNetworkCmdReset(t *testing.T) {
	dir := t.TempDir()
	if err := writeNetworkConfig(dir, "https://example.com", "wss://example.com"); err != nil {
		t.Fatalf("writeNetworkConfig: unexpected error: %s", err)
	}

	opts := parseArgsForTest(t, []string{"choose_network", "--reset"})
	if err := chooseNetworkCmdIn(opts, dir); err != nil {
		t.Fatalf("chooseNetworkCmdIn: unexpected error: %s", err)
	}

	_, ok, err := readNetworkConfig(dir)
	if err != nil {
		t.Fatalf("readNetworkConfig: unexpected error: %s", err)
	}
	if ok {
		t.Fatalf("expected network config to be cleared after reset")
	}
}

// TestChooseNetworkCmdShow: --show must report both states without
// changing anything, and must surface a corrupt config as an error
// rather than claiming the main network is in effect.
func TestChooseNetworkCmdShow(t *testing.T) {
	opts := parseArgsForTest(t, []string{"choose_network", "--show"})

	dir := t.TempDir()
	if err := chooseNetworkCmdIn(opts, dir); err != nil {
		t.Fatalf("chooseNetworkCmdIn (no saved network): unexpected error: %s", err)
	}
	if _, ok, _ := readNetworkConfig(dir); ok {
		t.Errorf("--show must not create a config")
	}

	if err := writeNetworkConfig(dir, "https://example.com", "wss://example.com"); err != nil {
		t.Fatal(err)
	}
	if err := chooseNetworkCmdIn(opts, dir); err != nil {
		t.Fatalf("chooseNetworkCmdIn (saved network): unexpected error: %s", err)
	}
	cfg, ok, err := readNetworkConfig(dir)
	if err != nil || !ok || cfg.ApiUrl != "https://example.com" {
		t.Errorf("--show changed the saved config: ok=%t err=%v cfg=%+v", ok, err, cfg)
	}

	corrupt := t.TempDir()
	if err := os.WriteFile(filepath.Join(corrupt, networkConfigFileName), []byte("{nope"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := chooseNetworkCmdIn(opts, corrupt); err == nil {
		t.Errorf("chooseNetworkCmdIn (corrupt config): expected an error, got nil")
	}
}

func TestResolveUrlCorruptConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, networkConfigFileName), []byte("{not valid json"), 0600); err != nil {
		t.Fatalf("WriteFile: unexpected error: %s", err)
	}

	opts := parseArgsForTest(t, []string{"provide"})
	if _, err := resolveApiUrlIn(opts, dir); err == nil {
		t.Fatalf("resolveApiUrlIn: expected error for corrupt config, got nil")
	}
	if _, err := resolveConnectUrlIn(opts, dir); err == nil {
		t.Fatalf("resolveConnectUrlIn: expected error for corrupt config, got nil")
	}
}

package miner

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

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

func TestReadNetworkConfigMissing(t *testing.T) {
	withTempHome(t)
	_, ok, err := readNetworkConfig()
	if err != nil {
		t.Fatalf("readNetworkConfig: unexpected error: %s", err)
	}
	if ok {
		t.Fatalf("readNetworkConfig: expected ok=false for missing file")
	}
}

func TestWriteThenReadNetworkConfig(t *testing.T) {
	withTempHome(t)
	if err := writeNetworkConfig("https://example.com", "wss://example.com"); err != nil {
		t.Fatalf("writeNetworkConfig: unexpected error: %s", err)
	}
	cfg, ok, err := readNetworkConfig()
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
}

func TestWriteNetworkConfigRejectsBadUrls(t *testing.T) {
	withTempHome(t)
	if err := writeNetworkConfig("ws://example.com", "wss://example.com"); err == nil {
		t.Fatalf("writeNetworkConfig: expected error for bad api_url scheme")
	}
	if err := writeNetworkConfig("https://example.com", "https://example.com"); err == nil {
		t.Fatalf("writeNetworkConfig: expected error for bad connect_url scheme")
	}
	// Nothing should have been written.
	_, ok, err := readNetworkConfig()
	if err != nil {
		t.Fatalf("readNetworkConfig: unexpected error: %s", err)
	}
	if ok {
		t.Fatalf("readNetworkConfig: expected ok=false after rejected write")
	}
}

func TestResetNetworkConfig(t *testing.T) {
	withTempHome(t)

	// Reset on a missing file is a no-op, not an error.
	if err := resetNetworkConfig(); err != nil {
		t.Fatalf("resetNetworkConfig on missing file: unexpected error: %s", err)
	}

	if err := writeNetworkConfig("https://example.com", "wss://example.com"); err != nil {
		t.Fatalf("writeNetworkConfig: unexpected error: %s", err)
	}
	if err := resetNetworkConfig(); err != nil {
		t.Fatalf("resetNetworkConfig: unexpected error: %s", err)
	}
	_, ok, err := readNetworkConfig()
	if err != nil {
		t.Fatalf("readNetworkConfig: unexpected error: %s", err)
	}
	if ok {
		t.Fatalf("readNetworkConfig: expected ok=false after reset")
	}
}

func TestNetworkConfigPath(t *testing.T) {
	home := withTempHome(t)
	p, err := networkConfigPath()
	if err != nil {
		t.Fatalf("networkConfigPath: unexpected error: %s", err)
	}
	want := filepath.Join(home, ".urnetwork", "network.json")
	if p != want {
		t.Errorf("networkConfigPath = %q, want %q", p, want)
	}
	// Path resolution must not require the file or directory to exist.
	if _, err := os.Stat(filepath.Dir(p)); err == nil {
		t.Fatalf("expected ~/.urnetwork to not exist yet before any write")
	}
}

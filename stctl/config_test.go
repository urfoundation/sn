package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestConfigRoundTrip marshals a Config, loads it back through loadConfig,
// and requires equality.
func TestConfigRoundTrip(t *testing.T) {
	want := &Config{
		RpcUrls:         []string{"https://test.chain.opentensor.ai", "http://127.0.0.1:9944"},
		ChainId:         945,
		ContractAddress: "0x00112233445566778899aAbBcCdDeEfF00112233",
		Netuid:          350,
		KeyFile:         "~/.urnetwork/stctl.key",
	}
	data, err := yaml.Marshal(want)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "stctl.yml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// TestExampleConfigParses keeps the example printed by deploy-status valid
// and pinned to the documented testnet values.
func TestExampleConfigParses(t *testing.T) {
	config, err := parseConfig([]byte(exampleConfig))
	if err != nil {
		t.Fatalf("example config does not parse: %v", err)
	}
	if err := config.validate(); err != nil {
		t.Fatalf("example config does not validate: %v", err)
	}
	if len(config.RpcUrls) != 1 || config.RpcUrls[0] != "https://test.chain.opentensor.ai" {
		t.Errorf("example rpc_urls = %v, want [https://test.chain.opentensor.ai]", config.RpcUrls)
	}
	if config.ChainId != 945 {
		t.Errorf("example chain_id = %d, want 945", config.ChainId)
	}
	if config.KeyFile != "~/.urnetwork/stctl.key" {
		t.Errorf("example key_file = %q", config.KeyFile)
	}
	// the placeholder zero contract address must be rejected at use time
	if _, err := config.contractAddr(); err == nil {
		t.Error("contractAddr() accepted the zero placeholder address")
	}
}

// TestConfigRejects covers strict decoding and validation failures.
func TestConfigRejects(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"unknown field", "rpc_urls: [x]\nchain_id: 945\nrpc_url: typo\n"},
		{"no rpc urls", "chain_id: 945\n"},
		{"no chain id", "rpc_urls: [x]\n"},
		{"bad contract address", "rpc_urls: [x]\nchain_id: 945\ncontract_address: nothex\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stctl.yml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			if _, err := loadConfig(path); err == nil {
				t.Fatalf("loadConfig accepted %q", tc.yaml)
			}
		})
	}
}

// TestConfigMissingFile points the operator at deploy-status.
func TestConfigMissingFile(t *testing.T) {
	_, err := loadConfig(filepath.Join(t.TempDir(), "nope.yml"))
	if err == nil {
		t.Fatal("loadConfig succeeded on a missing file")
	}
	if !strings.Contains(err.Error(), "deploy-status") {
		t.Errorf("missing-file error should mention deploy-status: %v", err)
	}
}

// TestExpandHome expands the leading tilde only.
func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	if got := expandHome("~/x/y"); got != filepath.Join(home, "x", "y") {
		t.Errorf("expandHome(~/x/y) = %q", got)
	}
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("expandHome(/abs/path) = %q", got)
	}
	if got := expandHome("rel/~path"); got != "rel/~path" {
		t.Errorf("expandHome(rel/~path) = %q", got)
	}
}

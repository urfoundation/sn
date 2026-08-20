package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictYAMLRejectsUnknownAndMultipleDocuments(t *testing.T) {
	type sample struct {
		Name string `yaml:"name"`
	}
	for name, content := range map[string]string{
		"unknown":  "name: ok\nextra: no\n",
		"multiple": "name: ok\n---\nname: second\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := strictYAML(path, new(sample)); err == nil {
				t.Fatalf("strict YAML accepted %s input", name)
			}
		})
	}
}

func TestHarnessConfigRequiresTestnetOnlyReferences(t *testing.T) {
	r := testResolvedConfig(t)
	if err := r.Config.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Config.LaunchInputs.Wallet = "vault://main/st.yml#wallet"
	if err := r.Config.Validate(); err == nil || !strings.Contains(err.Error(), "testnet-prefixed") {
		t.Fatalf("mainnet wallet reference was not rejected: %v", err)
	}
}

func TestResolveEnvTemplatesFailsClosed(t *testing.T) {
	t.Setenv("SIM_TEST_RPC", "snow:9944")
	got, err := resolveEnvTemplates("{{ env:SIM_TEST_RPC }}", true)
	if err != nil || got != "snow:9944" {
		t.Fatalf("resolved = %q, %v", got, err)
	}
	if _, err := resolveEnvTemplates("{{ env:SIM_TEST_RPC_MISSING }}:9944", true); err == nil {
		t.Fatal("missing environment variable accepted for a write-capable load")
	}
	got, err = resolveEnvTemplates("{{ env:SIM_TEST_RPC_MISSING }}:9944", false)
	if err != nil || !strings.Contains(got, "{{") {
		t.Fatalf("read-only unresolved template = %q, %v", got, err)
	}
}

func TestWalletSecretReferencesFailClosedAndRequirePrivateFiles(t *testing.T) {
	t.Setenv("SIM_TEST_WALLET_SECRET", "  bottom drive obey  ")
	if got, err := resolveSecretValue("env:SIM_TEST_WALLET_SECRET", true); err != nil || got != "bottom drive obey" {
		t.Fatalf("environment secret = %q, %v", got, err)
	}
	if _, err := resolveSecretValue("env:bad-name", true); err == nil {
		t.Fatal("invalid environment reference accepted")
	}
	if _, err := resolveSecretValue("env:SIM_TEST_WALLET_MISSING", true); err == nil {
		t.Fatal("missing write-capable environment secret accepted")
	}
	if got, err := resolveSecretValue("env:SIM_TEST_WALLET_MISSING", false); err != nil || got != "" {
		t.Fatalf("secretless read-only environment reference = %q, %v", got, err)
	}

	path := filepath.Join(t.TempDir(), "wallet.secret")
	if err := os.WriteFile(path, []byte("//Alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveSecretValue("file:"+path, true); err != nil || got != "//Alice" {
		t.Fatalf("file secret = %q, %v", got, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSecretValue("file:"+path, true); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("world-readable secret file accepted: %v", err)
	}
	if _, err := resolveSecretValue("file:relative.secret", true); err == nil {
		t.Fatal("relative secret file accepted")
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := resolveSecretValue("file:"+missing, true); err == nil {
		t.Fatal("missing write-capable secret file accepted")
	}
	if got, err := resolveSecretValue("file:"+missing, false); err != nil || got != "" {
		t.Fatalf("secretless read-only file reference = %q, %v", got, err)
	}
}

func TestResolvedConfigJSONRedactsWalletAndVault(t *testing.T) {
	r := testResolvedConfig(t)
	r.Vault = map[string]any{"testnet-wallet": "do not serialize"}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, secret := range []string{r.WalletSecret, r.WalletMaterial, "do not serialize", "WalletSecret", "WalletMaterial"} {
		if strings.Contains(text, secret) {
			t.Fatalf("resolved config JSON leaked %q: %s", secret, text)
		}
	}
}

func TestResolvedConfigRejectsDuplicatePublicOperatorOrigins(t *testing.T) {
	if _, err := validateOperatorAPIOrigins([]string{"https://same.example/", "https://same.example"}, 2); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("duplicate public operator origins were accepted: %v", err)
	}
}

func TestGovernanceProfilesRemainEnvironmentSeparated(t *testing.T) {
	valid := map[string]any{"testnet-contract-governance": "single-owner", "contract_governance": "safe-2-of-3"}
	if err := validateGovernanceSeparation(valid); err != nil {
		t.Fatal(err)
	}
	valid["contract_governance"] = "single-owner"
	if err := validateGovernanceSeparation(valid); err == nil || !strings.Contains(err.Error(), "mainnet") {
		t.Fatalf("unsafe mainnet governance was accepted: %v", err)
	}
}

func TestRepositoryDiscoveryUsesModuleIdentity(t *testing.T) {
	r := testResolvedConfig(t)
	configPath := filepath.Join(r.Repos.SN, "sim-testnet", "testnet.yml")
	got, err := discoverRepos(configPath, LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.SN != r.Repos.SN || got.Server != r.Repos.Server || got.Vault != r.Repos.Vault || got.PlatformConfig != r.Repos.PlatformConfig {
		t.Fatalf("discovered repositories = %+v, want %+v", got, r.Repos)
	}
}

func validCompatibilityGates() map[string]CompatibilityGate {
	result := map[string]CompatibilityGate{}
	for _, name := range []string{"alpha_values", "bonds_penalty", "max_allowed_validators", "max_weight_limit", "min_delegate_take", "subnet_owner_cut", "tao_weight"} {
		kind := "u16"
		expected := []uint64{1}
		if name == "alpha_values" {
			kind, expected = "u16_pair", []uint64{1, 2}
		}
		if name == "tao_weight" {
			kind = "u64"
		}
		result[name] = CompatibilityGate{Storage: name, Scope: "netuid", Kind: kind, Rule: "exact", Expected: expected, Decision: "tested"}
	}
	result["registration_burn"] = CompatibilityGate{Storage: "Burn", Scope: "netuid", Kind: "u64", Rule: "nonzero", Decision: "tested"}
	return result
}

func TestCompatibilityGateSchemaAndEvaluationFailClosed(t *testing.T) {
	gates := validCompatibilityGates()
	if err := validateCompatibilityGates(gates); err != nil {
		t.Fatal(err)
	}
	delete(gates, "tao_weight")
	if err := validateCompatibilityGates(gates); err == nil {
		t.Fatal("missing required compatibility gate accepted")
	}
	if err := evaluateCompatibilityGate(CompatibilityGate{Rule: "exact", Expected: []uint64{7}}, []uint64{7}); err != nil {
		t.Fatal(err)
	}
	if err := evaluateCompatibilityGate(CompatibilityGate{Rule: "exact", Expected: []uint64{7}}, []uint64{8}); err == nil {
		t.Fatal("incorrect exact runtime value accepted")
	}
	if err := evaluateCompatibilityGate(CompatibilityGate{Rule: "nonzero"}, []uint64{0}); err == nil {
		t.Fatal("zero nonzero-gate value accepted")
	}
}

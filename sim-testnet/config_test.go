package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/nacl/secretbox"
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
	r = testResolvedConfig(t)
	r.Config.LaunchInputs.WalletPassword = "vault://main/st.yml#wallet-password"
	if err := r.Config.Validate(); err == nil || !strings.Contains(err.Error(), "testnet-prefixed") {
		t.Fatalf("mainnet wallet password reference was not rejected: %v", err)
	}
}

func TestHarnessConfigRejectsUnsafeValidatorBootstrapAndAlphaMargin(t *testing.T) {
	tests := []func(*HarnessConfig){
		func(c *HarnessConfig) { c.AlphaTransfers.MinimumTAOEquivalentMarginBPS = 0 },
		func(c *HarnessConfig) { c.AlphaTransfers.MinimumTAOEquivalentMarginBPS = 5_001 },
		func(c *HarnessConfig) { c.ValidatorBootstrap.ReserveMinimumShareBPS = 5_000 },
		func(c *HarnessConfig) {
			c.ValidatorBootstrap.ReserveTargetShareBPS = c.ValidatorBootstrap.ReserveMinimumShareBPS
		},
		func(c *HarnessConfig) { c.ValidatorBootstrap.ReserveTargetShareBPS = 9_001 },
		func(c *HarnessConfig) { c.ValidatorBootstrap.IndependentTargetAlphaRao = 0 },
		func(c *HarnessConfig) { c.ValidatorBootstrap.MaximumReserveRepairAlphaRao = 999_999_999_999 },
		func(c *HarnessConfig) { c.ValidatorBootstrap.MinimumSourceRemainingAlphaRao = 0 },
	}
	for index, mutate := range tests {
		cfg := testResolvedConfig(t).Config
		mutate(cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatalf("unsafe alpha/validator bootstrap mutation %d was accepted", index)
		}
	}
}

func TestLightHarnessConfigPreservesReleaseChecksAndUsesLightnode(t *testing.T) {
	var cfg HarnessConfig
	if err := strictYAML("testnet-light.yml", &cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.LaunchInputs.Authority != "vault://main/st.yml#testnet-lightnode-authority" {
		t.Fatalf("light authority = %q", cfg.LaunchInputs.Authority)
	}
	if cfg.Deployment.DeploymentID != "ur-subnet-testnet-light-v1" {
		t.Fatalf("light deployment id = %q", cfg.Deployment.DeploymentID)
	}
	if cfg.Topology.Operators != 2 || cfg.Topology.Miners != 1_000 || cfg.Topology.Validators != 2 || cfg.Topology.HeadSlots != 200 || cfg.Topology.MinerSwarmProcesses != 20 || cfg.Scenarios.Launch != "smoke" {
		t.Fatalf("light profile weakened release topology or smoke: %+v / %+v", cfg.Topology, cfg.Scenarios)
	}
}

func TestReleaseHarnessSelectsOfficialPublicRPCOverrides(t *testing.T) {
	var cfg HarnessConfig
	if err := strictYAML("testnet.yml", &cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	substrate, evm, mode, err := resolveOperationalRPCs("private.example:9944", cfg.LaunchInputs.PublicSubstrateRPCOverride, cfg.LaunchInputs.PublicEVMRPCOverride)
	if err != nil {
		t.Fatal(err)
	}
	if mode != rpcModePublicOverride || substrate != "wss://test.finney.opentensor.ai:443" || evm != "https://test.chain.opentensor.ai" || cfg.LaunchInputs.PublicEVMMaximumRequestsPerMinute != 40 {
		t.Fatalf("operational RPC selection = %q %q %q", mode, substrate, evm)
	}
}

func TestHarnessConfigBoundsPublicEVMRequestCeiling(t *testing.T) {
	for _, value := range []int{0, 61} {
		cfg := testResolvedConfig(t).Config
		cfg.LaunchInputs.PublicEVMMaximumRequestsPerMinute = value
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "public EVM request ceiling") {
			t.Fatalf("public EVM request ceiling %d was accepted: %v", value, err)
		}
	}
}

func TestOperationalRPCOverrideRequiresAProtocolTypedPair(t *testing.T) {
	if _, _, _, err := resolveOperationalRPCs("private.example:9944", "wss://test.finney.opentensor.ai:443", ""); err == nil {
		t.Fatal("half-configured public RPC override was accepted")
	}
	if _, _, _, err := resolveOperationalRPCs("private.example:9944", "https://test.finney.opentensor.ai", "https://test.chain.opentensor.ai"); err == nil {
		t.Fatal("HTTP Substrate override was accepted")
	}
	if _, _, _, err := resolveOperationalRPCs("private.example:9944", "wss://test.finney.opentensor.ai:443", "wss://test.chain.opentensor.ai"); err == nil {
		t.Fatal("WebSocket EVM override was accepted")
	}
	for _, endpoints := range [][2]string{
		{"wss://:443", "https://test.chain.opentensor.ai"},
		{"wss://user@test.finney.opentensor.ai:443", "https://test.chain.opentensor.ai"},
		{"wss://test.finney.opentensor.ai:443/rpc", "https://test.chain.opentensor.ai"},
		{"wss://test.finney.opentensor.ai:443", "https://test.chain.opentensor.ai?token=secret"},
	} {
		if _, _, _, err := resolveOperationalRPCs("private.example:9944", endpoints[0], endpoints[1]); err == nil {
			t.Fatalf("unsafe public RPC override was accepted: %q %q", endpoints[0], endpoints[1])
		}
	}
}

func TestOperationalRPCFallsBackToPrivateAuthority(t *testing.T) {
	substrate, evm, mode, err := resolveOperationalRPCs("private.example:9944", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if mode != rpcModePrivateAuthority || substrate != "ws://private.example:9944" || evm != "http://private.example:9944" {
		t.Fatalf("private fallback = %q %q %q", mode, substrate, evm)
	}
}

func TestHarnessConfigRequiresNativeRegistrationEconomicLimits(t *testing.T) {
	r := testResolvedConfig(t)
	r.Config.Budgets.MaximumRegistrationBurnRao = 0
	if err := r.Config.Validate(); err == nil || !strings.Contains(err.Error(), "registration burn") {
		t.Fatalf("zero registration burn limit was accepted: %v", err)
	}
	r = testResolvedConfig(t)
	r.Config.Budgets.MaximumNativeTransactionFeeRao = 0
	if err := r.Config.Validate(); err == nil || !strings.Contains(err.Error(), "native transaction fee") {
		t.Fatalf("zero native transaction fee limit was accepted: %v", err)
	}
	r = testResolvedConfig(t)
	r.Config.Budgets.MaximumEVMFeePerGasWei = 0
	if err := r.Config.Validate(); err == nil || !strings.Contains(err.Error(), "EVM fee per gas") {
		t.Fatalf("zero EVM fee-per-gas limit was accepted: %v", err)
	}
}

func TestHarnessConfigRejectsRegistrationBudgetOutsideApprovalRange(t *testing.T) {
	cfg := testResolvedConfig(t).Config
	cfg.Budgets.MaximumRegistrations = int(math.MaxUint32) + 1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "uint32 approval range") {
		t.Fatalf("unrepresentable registration budget was accepted: %v", err)
	}
}

func TestReleaseCampaignBudgetCoversEveryProductionBoundary(t *testing.T) {
	r := testResolvedConfig(t)
	r.Release.Runtime.SpecVersion = r.Public.Chain.ExpectedRuntimeSpec
	r.Hyperparameters.ObservedCompatibilityGates = validCompatibilityGates()
	required, err := releaseCampaignDepositRequirement(r)
	if err != nil {
		t.Fatal(err)
	}
	if required != 196_000_000_000 {
		t.Fatalf("release campaign requirement = %d, want 196000000000", required)
	}
	r.Policy.Deposit.TotalTestCampaignCapRao = required
	if err := r.Validate(); err != nil {
		t.Fatalf("exact release campaign requirement was rejected: %v", err)
	}
	r.Policy.Deposit.TotalTestCampaignCapRao--
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "require at least 196000000000") {
		t.Fatalf("underfunded release campaign was accepted: %v", err)
	}
}

func TestReleaseCampaignBudgetFailsClosedOnIncompleteAndOverflowingInputs(t *testing.T) {
	if _, err := releaseCampaignDepositRequirement(nil); err == nil {
		t.Fatal("nil release campaign configuration was accepted")
	}
	r := testResolvedConfig(t)
	r.Config.Topology.Operators = 1
	if _, err := releaseCampaignDepositRequirement(r); err == nil {
		t.Fatal("single-operator release campaign was accepted")
	}
	r = testResolvedConfig(t)
	r.Policy.Deposit.EpochCapRaoPerOperator = ^uint64(0)
	if _, err := releaseCampaignDepositRequirement(r); err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("overflowing release campaign was accepted: %v", err)
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

func TestBTWALLETNACLDecryptsAndFailsClosed(t *testing.T) {
	password := "unit-test-password"
	plain := []byte(`{"secretPhrase":"//Alice","cryptoType":1}`)
	const memoryKiB = uint32(32)
	derived := argon2.Key([]byte(password), btwalletNACLSalt[:], 1, memoryKiB, 1, 32)
	var key [32]byte
	copy(key[:], derived)
	var nonce [24]byte
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	encrypted := append([]byte(btwalletNACLPrefix), nonce[:]...)
	encrypted = append(encrypted, secretbox.Seal(nil, plain, &nonce, &key)...)
	got, err := decryptBTWALLETNACL(encrypted, password, 1, memoryKiB, 1)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decrypted = %q, %v", got, err)
	}
	if _, err := decryptBTWALLETNACL(encrypted, "wrong", 1, memoryKiB, 1); err == nil {
		t.Fatal("wrong wallet password was accepted")
	}
	if _, err := decryptBTWALLETNACL([]byte("not-a-keyfile"), password, 1, memoryKiB, 1); err == nil {
		t.Fatal("malformed keyfile was accepted")
	}
}

func TestVaultRelativePathsRejectTraversalAndSymlinkEscape(t *testing.T) {
	vault := t.TempDir()
	inside := filepath.Join(vault, "secret")
	if err := os.WriteFile(inside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalInside, err := filepath.EvalSymlinks(inside)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolveVaultRelativePath(vault, "vault-file:secret", "vault-file:", true); err != nil || got != canonicalInside {
		t.Fatalf("inside path = %q, want %q: %v", got, canonicalInside, err)
	}
	if _, err := resolveVaultRelativePath(vault, "vault-file:../secret", "vault-file:", true); err == nil {
		t.Fatal("vault traversal was accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(vault, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveVaultRelativePath(vault, "vault-file:escape", "vault-file:", true); err == nil {
		t.Fatal("vault symlink escape was accepted")
	}
}

func TestVaultWalletFilesRejectTraversalSymlinkAndPublicSecretPermissions(t *testing.T) {
	wallet := t.TempDir()
	inside := filepath.Join(wallet, "coldkey")
	if err := os.WriteFile(inside, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalInside, err := filepath.EvalSymlinks(inside)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolveVaultWalletFile(wallet, "coldkey", true); err != nil || got != canonicalInside {
		t.Fatalf("inside wallet file = %q, want %q: %v", got, canonicalInside, err)
	}
	if _, err := resolveVaultWalletFile(wallet, "../coldkey", true); err == nil {
		t.Fatal("wallet file traversal was accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(wallet, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveVaultWalletFile(wallet, "escape", true); err == nil {
		t.Fatal("wallet child symlink escape was accepted")
	}
	if err := os.Chmod(inside, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveVaultWalletFile(wallet, "coldkey", true); err == nil {
		t.Fatal("world-readable wallet coldkey was accepted")
	}
	if _, err := resolveVaultWalletFile(wallet, "coldkey", false); err != nil {
		t.Fatalf("public wallet file permissions were rejected: %v", err)
	}
}

func TestBTWALLETMaterialMustMatchPublicIdentity(t *testing.T) {
	ring, err := signature.KeyringPairFromSecret("//Alice", 42)
	if err != nil {
		t.Fatal(err)
	}
	cryptoType := uint8(1)
	public := btwalletKeyfile{PublicKey: "0x" + hex.EncodeToString(ring.PublicKey), AccountID: "0x" + hex.EncodeToString(ring.PublicKey), SS58Address: ring.Address, CryptoType: &cryptoType}
	if got, err := materialFromBTWALLET(btwalletKeyfile{SecretPhrase: "//Alice", SS58Address: ring.Address, CryptoType: &cryptoType}, public); err != nil || got != "//Alice" {
		t.Fatalf("wallet material = %q, %v", got, err)
	}
	public.SS58Address = "5Wrong"
	if _, err := materialFromBTWALLET(btwalletKeyfile{SecretPhrase: "//Alice", CryptoType: &cryptoType}, public); err == nil {
		t.Fatal("wallet identity mismatch was accepted")
	}
}

func TestBTWALLETPublicRequiresMatchingSS58Identity(t *testing.T) {
	alice, err := signature.KeyringPairFromSecret("//Alice", 42)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := signature.KeyringPairFromSecret("//Bob", 42)
	if err != nil {
		t.Fatal(err)
	}
	cryptoType := uint8(1)
	value := btwalletKeyfile{
		AccountID:   "0x" + hex.EncodeToString(alice.PublicKey),
		PublicKey:   "0x" + hex.EncodeToString(alice.PublicKey),
		SS58Address: bob.Address,
		CryptoType:  &cryptoType,
	}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "coldkeypub.txt")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBTWALLETPublic(path); err == nil || !strings.Contains(err.Error(), "ss58Address") {
		t.Fatalf("mismatched public wallet identity was accepted: %v", err)
	}
	value.SS58Address = alice.Address
	b, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBTWALLETPublic(path); err != nil {
		t.Fatalf("matching public wallet identity was rejected: %v", err)
	}
}

func TestUnsignedVaultValuesRejectNegativeAndOverflow(t *testing.T) {
	for name, value := range map[string]any{
		"negative integer": -1,
		"negative string":  "-1",
		"overflow":         "18446744073709551616",
	} {
		if _, err := parseUnsignedVaultValue("testnet-limit", value); err == nil {
			t.Errorf("%s: unsafe unsigned value %v was accepted", name, value)
		}
	}
	if value, err := parseUnsignedVaultValue("testnet-limit", " 42 "); err != nil || value != 42 {
		t.Fatalf("valid unsigned value = %d, %v", value, err)
	}
}

func TestDecimalVaultValueAcceptsCampaignGasBeyondUint64OnlyCanonically(t *testing.T) {
	value, err := parseDecimalVaultValue("testnet-spending-limit-evm-gas-wei", " 100000000000000000000 ")
	if err != nil || value != DecimalUint("100000000000000000000") {
		t.Fatalf("large EVM gas vault value = %s, %v", value, err)
	}
	for name, input := range map[string]any{
		"negative":       "-1",
		"leading zero":   "01",
		"fraction":       "1.5",
		"floating value": 1.5,
	} {
		if _, err := parseDecimalVaultValue("testnet-spending-limit-evm-gas-wei", input); err == nil {
			t.Errorf("%s decimal vault value %v was accepted", name, input)
		}
	}
}

func TestResolvedConfigJSONRedactsWalletAndVault(t *testing.T) {
	r := testResolvedConfig(t)
	r.Vault = map[string]any{"testnet-wallet": "do not serialize"}
	r.Authority = "wss://rpc-user:rpc-password@example.test/path?token=rpc-token"
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, secret := range []string{r.WalletSecret, r.WalletMaterial, r.WalletPasswordSecret, r.WalletPassword, "do not serialize", "WalletSecret", "WalletMaterial", "WalletPassword", "rpc-user", "rpc-password", "rpc-token"} {
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

func TestResolvedConfigRequiresCanonicalHyperparameterLifecycle(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ResolvedConfig)
		want   string
	}{
		{name: "missing bootstrap burn half-life", mutate: func(cfg *ResolvedConfig) { delete(cfg.Hyperparameters.OwnerControlled, "burn_half_life") }, want: "no bootstrap value"},
		{name: "non-one bootstrap burn half-life", mutate: func(cfg *ResolvedConfig) { cfg.Hyperparameters.OwnerControlled["burn_half_life"] = 2 }, want: "one-block bootstrap"},
		{name: "missing production restore", mutate: func(cfg *ResolvedConfig) { delete(cfg.Hyperparameters.ProductionOwnerControlled, "burn_half_life") }, want: "restore burn_half_life"},
		{name: "incorrect production restore", mutate: func(cfg *ResolvedConfig) { cfg.Hyperparameters.ProductionOwnerControlled["burn_half_life"] = 359 }, want: "restore burn_half_life"},
		{name: "expired bootstrap immunity window", mutate: func(cfg *ResolvedConfig) { cfg.Hyperparameters.OwnerControlled["immunity_period"] = 7_200 }, want: "reviewed 50000-block"},
		{name: "overwide bootstrap immunity window", mutate: func(cfg *ResolvedConfig) { cfg.Hyperparameters.OwnerControlled["immunity_period"] = 50_001 }, want: "reviewed 50000-block"},
		{name: "unsupported bootstrap key", mutate: func(cfg *ResolvedConfig) { cfg.Hyperparameters.OwnerControlled["burn_magic"] = 1 }, want: "unsupported"},
		{name: "unsupported production key", mutate: func(cfg *ResolvedConfig) {
			delete(cfg.Hyperparameters.ProductionOwnerControlled, "burn_half_life")
			cfg.Hyperparameters.ProductionOwnerControlled["burn_magic"] = 360
		}, want: "unsupported"},
		{name: "invalid unsigned bootstrap value", mutate: func(cfg *ResolvedConfig) { cfg.Hyperparameters.OwnerControlled["tempo"] = -1 }, want: "owner hyperparameter tempo"},
	}
	for _, test := range tests {
		cfg := testResolvedConfig(t)
		cfg.Release.Runtime.SpecVersion = cfg.Public.Chain.ExpectedRuntimeSpec
		cfg.Hyperparameters.ObservedCompatibilityGates = validCompatibilityGates()
		test.mutate(cfg)
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error=%v, want substring %q", test.name, err, test.want)
		}
	}
}

func TestResolvedConfigRequiresRuntimeDefaultMinTransfer(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Release.Runtime.SpecVersion = cfg.Public.Chain.ExpectedRuntimeSpec
	cfg.Hyperparameters.ObservedCompatibilityGates = validCompatibilityGates()
	cfg.Public.Chain.ExpectedDefaultMinTransferRao = 0
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "runtime transfer minimum") {
		t.Fatalf("zero public runtime transfer minimum was accepted: %v", err)
	}
}

func TestReleaseConfigHashBindsPublicAndHyperparameterManifests(t *testing.T) {
	cfg := testResolvedConfig(t)
	public := *cfg.Public
	public.Chain.ExpectedBlockSeconds++
	publicHash, err := releaseConfigHash(cfg.Config, &public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	if publicHash == cfg.ConfigHash {
		t.Fatal("public chain-manifest drift did not change the release config hash")
	}

	hyperparameters := *cfg.Hyperparameters
	hyperparameters.OwnerControlled = make(map[string]any, len(cfg.Hyperparameters.OwnerControlled))
	for name, value := range cfg.Hyperparameters.OwnerControlled {
		hyperparameters.OwnerControlled[name] = value
	}
	hyperparameters.OwnerControlled["tempo"] = uint64(361)
	hyperparameterHash, err := releaseConfigHash(cfg.Config, cfg.Public, &hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	if hyperparameterHash == cfg.ConfigHash {
		t.Fatal("owner hyperparameter drift did not change the release config hash")
	}
}

func TestReleaseConfigHashRejectsIncompleteManifestSet(t *testing.T) {
	cfg := testResolvedConfig(t)
	if _, err := releaseConfigHash(cfg.Config, nil, cfg.Hyperparameters); err == nil {
		t.Fatal("incomplete release manifest set was accepted")
	}
}

package validator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/protocol"
)

func validReleaseConfig(t *testing.T) ReleaseConfig {
	t.Helper()
	p, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := p.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	return ReleaseConfig{
		SchemaVersion:   1,
		Production:      true,
		Release:         "1.0",
		DeploymentID:    "test-deployment",
		ValidatorID:     1,
		ChainID:         945,
		GenesisHash:     "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105",
		RuntimeSpec:     447,
		Netuid:          7,
		Coordinator:     "0x1111111111111111111111111111111111111111",
		SettlementVault: "0x2222222222222222222222222222222222222222",
		DeployBlock:     10,
		PolicyHash:      h,
		RPC:             []string{"https://evm.example"},
		Substrate:       []string{"wss://substrate.example"},
		StateDir:        filepath.Join(root, "state"),
		HotkeySeedFile:  filepath.Join(root, "hotkey.seed"),
		ControlledNOIDs: []uint64{},
		TrailDepth:      p.Verify.TrailDepth,
		PollSeconds:     2,
		Policy:          *p,
		Operators: []OperatorConfig{
			{NoID: 1, APIURL: "https://one.example", ConnectURL: "wss://one.example/connect", ArtifactSigner: "0x1111111111111111111111111111111111111111", StateDir: filepath.Join(root, "no-1"), Concurrency: 2},
			{NoID: 2, APIURL: "https://two.example", ConnectURL: "wss://two.example/connect", ArtifactSigner: "0x2222222222222222222222222222222222222222", StateDir: filepath.Join(root, "no-2"), Concurrency: 2},
		},
	}
}

func writeReleaseConfig(t *testing.T, cfg ReleaseConfig) string {
	t.Helper()
	b, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "validator.yml")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReleaseConfigStrictAndNormalizesOperatorSecrets(t *testing.T) {
	cfg := validReleaseConfig(t)
	path := writeReleaseConfig(t, cfg)
	got, err := LoadReleaseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got.Operators[0].ClientJWTFile) || !strings.HasSuffix(got.Operators[0].ClientJWTFile, "client.jwt") {
		t.Fatalf("client JWT path not normalized: %q", got.Operators[0].ClientJWTFile)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, []byte("unknown_release_field: true\n")...)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReleaseConfig(path); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestReleaseConfigFailsClosedOnSharedOperatorStateAndPolicyDrift(t *testing.T) {
	cfg := validReleaseConfig(t)
	cfg.Operators[1].StateDir = cfg.Operators[0].StateDir
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err == nil {
		t.Fatal("shared operator state was accepted")
	}

	cfg = validReleaseConfig(t)
	cfg.Policy.Steering.Theta.Numerator++
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err == nil || !strings.Contains(err.Error(), "policy hash mismatch") {
		t.Fatalf("policy drift error = %v", err)
	}
}

func TestReleaseConfigRequiresMinimumNOsAndExplicitControlledNOs(t *testing.T) {
	cfg := validReleaseConfig(t)
	cfg.Operators = cfg.Operators[:1]
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err == nil {
		t.Fatal("single-NO production config accepted")
	}
	cfg = validReleaseConfig(t)
	cfg.ControlledNOIDs = []uint64{99}
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err == nil {
		t.Fatal("unknown controlled NO accepted")
	}
}

func TestReleaseConfigRequiresDistinctOperatorArtifactSigners(t *testing.T) {
	cfg := validReleaseConfig(t)
	cfg.Operators[1].ArtifactSigner = cfg.Operators[0].ArtifactSigner
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err == nil || !strings.Contains(err.Error(), "artifact_signer aliases") {
		t.Fatalf("shared artifact signer error = %v", err)
	}
	cfg = validReleaseConfig(t)
	cfg.Operators[0].ArtifactSigner = ""
	if _, err := LoadReleaseConfig(writeReleaseConfig(t, cfg)); err == nil || !strings.Contains(err.Error(), "artifact_signer") {
		t.Fatalf("missing artifact signer error = %v", err)
	}
}

//go:build linux || darwin

// Synthetic private inputs exercise the existing renderer's interrupted-write
// boundary. No deployment capture, live credential or external RPC is used.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/server/controller"
	"gopkg.in/yaml.v3"
)

// Reuse the generated constructor/receipt fixture with wholly test-owned
// source files, capacities, identities and renderer prerequisites.
func runtimeConfigNativeRenderFixtureTest(t *testing.T) (*ResolvedConfig, string, *RoleSecrets) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://evm.example"
	cfg.Repos.PlatformConfig = testOperatorConfigSources(t)
	cfg.Repos.Vault = t.TempDir()
	for _, source := range []struct{ path, contents string }{
		{path: "local/st.yml", contents: "profile: synthetic-overwritten\n"},
		{path: "local/pg.yml", contents: "authority: postgres.example\n"},
		{path: "main/minio.yml", contents: "authority: objects.example:23900\ntls: true\nbucket: blob\naccess_key: synthetic-access\nsecret_key: synthetic-secret\n"},
	} {
		if err := atomicWrite(filepath.Join(cfg.Repos.Vault, filepath.FromSlash(source.path)), []byte(source.contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	budget, err := requiredRuntimeClientKeyUploadBudget(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.Artifacts.AttemptUpload = &budget
	stateDir := t.TempDir()
	// Seed only the reviewed bounds and census from the generic fixture. Its
	// placeholder input files belong to a separate namespace; this state root
	// is populated once by the authenticated retained-setup helper below.
	configureRuntimeEvidenceV2Test(t, cfg, t.TempDir())
	// The compatibility preview exercises the retained V2 namespace path. Keep
	// the harness config as the explicit provisioning template, while the
	// helper's retained files provide the complete original activation setup.
	cfg.Config.ValidatorEvidenceV2 = runtimeEvidenceTemplateV2(cfg.Config.ValidatorEvidenceV2)
	cfg.Config.ProvisionValidatorEvidenceV2 = true
	cfg.Config.ValidatorEvidenceActivationGasUnits = 1_000_000
	cfg.ConfigHash, err = releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for label, client := range roles.Clients {
		client.ClientIDHex = strings.Repeat("01", 16)
		roles.Clients[label] = client
	}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		root := filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", miner), "state")
		for _, source := range []struct{ name, contents string }{
			{name: "jwt", contents: "synthetic-network-jwt\n"},
			{name: ".provider.jwt", contents: "synthetic-provider-jwt\n"},
			{name: ".provider.key", contents: strings.Repeat("01", 32) + "\n"},
		} {
			if err := atomicWrite(filepath.Join(root, source.name), []byte(source.contents), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	reserved := prepareRuntimeReservedRenderTest(t, cfg, stateDir, roles)
	retainRuntimeEvidenceLaunchInputsTest(t, cfg, stateDir, roles, reserved.plan)
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	return cfg, stateDir, roles
}

// Install only the former reviewed runtime tuple in generated inputs. The
// old manifest remains internally authentic, reproducing the carried failure.
func staleRuntimeConfigNativePinsTest(t *testing.T, cfg *ResolvedConfig, stateDir string) map[string][]byte {
	t.Helper()
	previous, ok := crv4.ReviewedRuntimeArtifact(crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 460, TransactionVersion: 1, StateVersion: 1})
	if !ok {
		t.Fatal("former reviewed runtime fixture is absent")
	}
	originalWireKVs := map[string][]byte{}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "vault", "st.yml")
		wire, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		originalWireKVs[path] = wire
		root, err := runtimeAttemptUploadYAML(wire)
		if err != nil {
			t.Fatal(err)
		}
		for index := 0; index < len(root.Content); index += 2 {
			if root.Content[index].Value != "testnet-reserved-attempt-upload" {
				continue
			}
			var reserved controller.StReservedAttemptUploadConfig
			if err := root.Content[index+1].Decode(&reserved); err != nil {
				t.Fatal(err)
			}
			reserved.Admission.Deployment.NativeRuntime = previous
			if err := root.Content[index+1].Encode(reserved); err != nil {
				t.Fatal(err)
			}
		}
		wire, err = yaml.Marshal(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validator), "validator.yml")
		wire, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		originalWireKVs[path] = wire
		var value map[string]any
		if err := yaml.Unmarshal(wire, &value); err != nil {
			t.Fatal(err)
		}
		value["runtime_spec"], value["runtime_code_hash"], value["runtime_metadata_hash"] = previous.Version.SpecVersion, previous.CodeHash, previous.MetadataHash
		wire, err = yaml.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rewriteRuntimeConfigManifest(t, stateDir, func(manifest *RuntimeConfigManifest) {
		for index := range manifest.Files {
			file := &manifest.Files[index]
			path := filepath.Join(stateDir, filepath.FromSlash(file.Path))
			if _, changed := originalWireKVs[path]; !changed {
				continue
			}
			var err error
			file.SHA256, _, err = runtimeConfigFileDigest(path)
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "runtime reserved staging differs") {
		t.Fatalf("stale rendered runtime did not reproduce the carried postcondition failure: %v", err)
	}
	return originalWireKVs
}

// An explicit filesystem failure after operator one forces partial rendering.
// Retry uses the normal renderer and restores every validator before startup.
func TestRuntimeConfigNativeRefreshRecoversPartialRenderWithoutChangingHistory(t *testing.T) {
	t.Parallel()
	cfg, stateDir, roles := runtimeConfigNativeRenderFixtureTest(t)
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	originalWireKVs := staleRuntimeConfigNativePinsTest(t, cfg, stateDir)
	manifestBefore, err := os.ReadFile(runtimeConfigManifestPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(stateDir, "runtime", "operator-2", "vault", "st.yml")
	stale, err := os.ReadFile(blocked)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err == nil {
		t.Fatal("partial render unexpectedly completed through its blocked second output")
	}
	first := filepath.Join(stateDir, "runtime", "operator-1", "vault", "st.yml")
	if wire, err := os.ReadFile(first); err != nil || !bytes.Equal(wire, originalWireKVs[first]) {
		t.Fatalf("failure did not occur after the first operator was refreshed: %v", err)
	}
	if wire, err := os.ReadFile(runtimeConfigManifestPath(stateDir)); err != nil || !bytes.Equal(wire, manifestBefore) {
		t.Fatalf("incomplete rendering published a replacement manifest: %v", err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil {
		t.Fatal("partial rendering became a verified current postcondition")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(blocked, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err != nil {
		t.Fatalf("ordinary retry could not finish current rendering: %v", err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	for path, want := range originalWireKVs {
		if wire, err := os.ReadFile(path); err != nil || !bytes.Equal(wire, want) {
			t.Fatalf("current retry changed capacity, authority, custody or configuration at %s: %v", path, err)
		}
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validator), "validator.yml")
		loaded, err := validatorpkg.LoadReleaseConfig(path)
		if err != nil || loaded.RuntimeSpec != cfg.Public.Chain.ExpectedRuntimeSpec || loaded.RuntimeCodeHash != cfg.Release.Runtime.CodeHash || loaded.RuntimeMetadataHash != cfg.Release.Runtime.MetadataHash {
			t.Fatalf("validator could not start with the current derived runtime: %v", err)
		}
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("completed retry changed original plan, journal, signed references, custody or other non-derived bytes")
	}
}

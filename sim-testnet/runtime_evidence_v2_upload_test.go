//go:build linux || darwin

package main

// Real V2 rendering, strict server quota decoding and owned runtime-file reads
// preserve explicit testnet capacity through approval and immutable manifests.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urnetwork/server/v2026/model"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

// These small limits belong to isolated controls, never a production fallback.
func runtimeAttemptUploadTestBudget() model.StAttemptUploadBudget {
	return model.StAttemptUploadBudget{RequestsPerHour: 4096, BytesPerHour: 64 * 1024 * 1024, AccountRequestsPerHour: 1024, AccountBytesPerHour: 16 * 1024 * 1024}
}

// Both actual operator files are valid before any mutation control. Other
// immutable inputs retain the existing fixture's complete inventory.
func newRuntimeAttemptUploadManifestTestFixture(t *testing.T) (*ResolvedConfig, string) {
	t.Helper()
	cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
	budget := runtimeAttemptUploadTestBudget()
	cfg.Config.Artifacts.AttemptUpload = &budget
	wire, err := yaml.Marshal(map[string]any{"profile": "testnet", "testnet-enabled": true, "testnet-attempt-upload": budget})
	if err != nil {
		t.Fatal(err)
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "vault", "st.yml")
		if err := atomicWrite(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	return cfg, stateDir
}

// Shared server validation rejects every missing/overflowing counter and each
// account/global mismatch. The admitted value cannot follow later pointer edits.
func TestRuntimeEvidenceV2AttemptUploadBudgetAdmissionOwnsExactValues(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	want := runtimeAttemptUploadTestBudget()
	cfg.Config.Artifacts.AttemptUpload = &want
	owned, err := runtimeAttemptUploadBudget(cfg)
	if err != nil || owned != want {
		t.Fatalf("explicit upload limits changed at admission: %v", err)
	}
	cfg.Config.Artifacts.AttemptUpload.BytesPerHour++
	if owned.BytesPerHour == cfg.Config.Artifacts.AttemptUpload.BytesPerHour {
		t.Fatal("admitted quota retained the caller's mutable alias")
	}
	for _, invalid := range []*ResolvedConfig{nil, {}, {Config: &HarnessConfig{}}} {
		if _, err := runtimeAttemptUploadBudget(invalid); err == nil {
			t.Fatal("missing upload owner acquired capacity")
		}
	}
	for _, field := range []string{"requests", "bytes", "account-requests", "account-bytes"} {
		for _, limit := range []uint64{0, 9007199254740992} {
			budget := runtimeAttemptUploadTestBudget()
			switch field {
			case "requests":
				budget.RequestsPerHour = limit
			case "bytes":
				budget.BytesPerHour = limit
			case "account-requests":
				budget.AccountRequestsPerHour = limit
			case "account-bytes":
				budget.AccountBytesPerHour = limit
			}
			cfg.Config.Artifacts.AttemptUpload = &budget
			if _, err := runtimeAttemptUploadBudget(cfg); err == nil {
				t.Fatalf("invalid %s capacity %d was accepted", field, limit)
			}
			if err := cfg.Config.Validate(); err == nil {
				t.Fatalf("harness accepted invalid %s capacity", field)
			}
		}
	}
	for _, budget := range []model.StAttemptUploadBudget{
		{RequestsPerHour: 1, BytesPerHour: 2, AccountRequestsPerHour: 2, AccountBytesPerHour: 1},
		{RequestsPerHour: 2, BytesPerHour: 1, AccountRequestsPerHour: 1, AccountBytesPerHour: 2},
	} {
		cfg.Config.Artifacts.AttemptUpload = &budget
		if _, err := runtimeAttemptUploadBudget(cfg); err == nil {
			t.Fatal("account limit exceeded the deployment limit")
		}
	}
}

// Original nodes are decoded by the real strict loader and server budget,
// including fractional values that yaml.v3 otherwise truncates into uint64.
func TestRuntimeEvidenceV2AttemptUploadStrictHarnessBudget(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	budget := runtimeAttemptUploadTestBudget()
	cfg.Config.Artifacts.AttemptUpload = &budget
	wire, err := yaml.Marshal(cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "harness.yml")
	if err := atomicWrite(path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	var actual HarnessConfig
	if err := strictYAML(path, &actual); err != nil || actual.Artifacts.AttemptUpload == nil || *actual.Artifacts.AttemptUpload != budget {
		t.Fatalf("exact upload capacity did not survive the real strict loader: %v", err)
	}
	needle := "requests_per_hour: 4096"
	if strings.Count(string(wire), needle) != 1 {
		t.Fatal("quota fixture lacks its unique canonical scalar")
	}
	for _, replacement := range []string{"requests_per_hour: 3.5", "requests_per_hour: 4096.0", "requests_per_hour: '4096'", "requests_per_hour: true", "requests_per_hour: -1", "requests_per_hour: 9007199254740992", "requests_per_hour: 0", "requests_per_hour: 4_096", "unexpected_upload_limit: 4096"} {
		changed := strings.Replace(string(wire), needle, replacement, 1)
		if err := atomicWrite(path, []byte(changed), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := strictYAML(path, &actual); err == nil {
			t.Fatalf("strict harness admitted %s", replacement)
		}
	}
}

// Optional planning fields do not retroactively alter old JSON approvals.
// Omission never provides the mandatory capacity for actual rendering.
func TestRuntimeEvidenceV2AttemptUploadPreservesUnprovisionedPlanningWire(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	if cfg.Config.Artifacts.AttemptUpload != nil {
		t.Fatal("plan fixture unexpectedly provisions live upload capacity")
	}
	wire, err := json.Marshal(cfg.Config.Artifacts)
	if err != nil || bytes.Contains(wire, []byte("attempt_upload")) {
		t.Fatalf("omission changed historical artifact configuration wire: %v", err)
	}
	identity, err := runtimeAttemptUploadIdentity(cfg)
	if err != nil || identity != "" {
		t.Fatalf("omission invented provisioned runtime capacity: %v", err)
	}
	if _, err := runtimeAttemptUploadBudget(cfg); err == nil {
		t.Fatal("unprovisioned planning acquired rendering authority")
	}
	manifest, err := json.Marshal(RuntimeConfigManifest{})
	if err != nil || bytes.Contains(manifest, []byte("attempt_upload_hash")) {
		t.Fatalf("omission changed old runtime manifest wire: %v", err)
	}
}

// Invalid quota is refused before configuration overlays, copied secrets or
// later source I/O. The retained old renderer reaches those other operations.
func TestRuntimeEvidenceV2AttemptUploadRenderRefusesInvalidBeforeMutation(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"missing", "zero", "overflow", "account"} {
		cfg := testResolvedConfig(t)
		cfg.Public.Chain.EVMPublicReadEndpoint = "https://test.chain.opentensor.ai"
		cfg.Repos.PlatformConfig = testOperatorConfigSources(t)
		cfg.Repos.Vault = t.TempDir()
		stateDir := t.TempDir()
		configureRuntimeEvidenceV2Test(t, cfg, stateDir)
		budget := runtimeAttemptUploadTestBudget()
		cfg.Config.Artifacts.AttemptUpload = &budget
		switch fault {
		case "missing":
			cfg.Config.Artifacts.AttemptUpload = nil
		case "zero":
			budget.AccountBytesPerHour = 0
		case "overflow":
			budget.BytesPerHour = 9007199254740992
		case "account":
			budget.AccountRequestsPerHour = budget.RequestsPerHour + 1
		}
		deployment := ContractDeployment{Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, CoordinatorProxy: common.HexToAddress("0x4000000000000000000000000000000000000004"), SettlementVault: common.HexToAddress("0x2000000000000000000000000000000000000002"), DeployBlock: 123, DeployBlockHash: "0x" + strings.Repeat("ab", 32), CoordinatorEventStartBlock: 100, CoordinatorEventStartBlockHash: "0x" + strings.Repeat("cd", 32)}
		if err := saveContractDeployment(stateDir, deployment); err != nil {
			t.Fatal(err)
		}
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		if err := RenderRuntimeConfigs(cfg, stateDir, nil); err == nil || !strings.Contains(err.Error(), "attempt upload") {
			t.Fatalf("renderer failed to reject invalid attempt upload budget before source I/O (%s): %v", fault, err)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatal("invalid upload quota changed runtime state before admission")
		}
	}
}

// The independent capacity identity catches all four changes even while the
// caller retains the old cached ConfigHash and all actual files are unchanged.
func TestRuntimeEvidenceV2AttemptUploadManifestBindsEveryLimit(t *testing.T) {
	t.Parallel()
	cfg, stateDir := newRuntimeAttemptUploadManifestTestFixture(t)
	before, err := releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"requests", "bytes", "account-requests", "account-bytes"} {
		budget := runtimeAttemptUploadTestBudget()
		switch field {
		case "requests":
			budget.RequestsPerHour++
		case "bytes":
			budget.BytesPerHour++
		case "account-requests":
			budget.AccountRequestsPerHour++
		case "account-bytes":
			budget.AccountBytesPerHour++
		}
		cfg.Config.Artifacts.AttemptUpload = &budget
		after, err := releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
		if err != nil || after == before {
			t.Fatalf("source hash omitted %s capacity: %v", field, err)
		}
		if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "identity") {
			t.Fatalf("runtime manifest ignored actual %s capacity with stale ConfigHash: %v", field, err)
		}
	}
	cfg.Config.Artifacts.AttemptUpload = nil
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil {
		t.Fatal("runtime manifest lost its provisioned capacity requirement")
	}
}

// The manifest may not adopt substituted settings merely by updating their
// content digest and its own public hash. Every configured operator is checked.
func TestRuntimeEvidenceV2AttemptUploadManifestRejectsRehashedSettings(t *testing.T) {
	t.Parallel()
	cfg, stateDir := newRuntimeAttemptUploadManifestTestFixture(t)
	relative := "runtime/operator-2/vault/st.yml"
	path := filepath.Join(stateDir, filepath.FromSlash(relative))
	budget := *cfg.Config.Artifacts.AttemptUpload
	budget.BytesPerHour++
	wire, err := yaml.Marshal(map[string]any{"profile": "testnet", "testnet-enabled": true, "testnet-attempt-upload": budget})
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	rewriteRuntimeConfigManifest(t, stateDir, func(manifest *RuntimeConfigManifest) {
		for index := range manifest.Files {
			if manifest.Files[index].Path == relative {
				manifest.Files[index].SHA256 = bytesSHA256(wire)
			}
		}
	})
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil {
		t.Fatal("rehashing authorized altered attempt upload settings")
	}
	if err := writeRuntimeConfigManifest(cfg, stateDir); err == nil {
		t.Fatal("manifest rebuild adopted altered attempt upload settings")
	}
}

// The actual same-descriptor reader rejects aliases, FIFOs, nonprivate modes
// and oversized control documents without waiting for a pipe writer.
func TestRuntimeEvidenceV2AttemptUploadConfigRequiresOwnedBoundedFile(t *testing.T) {
	t.Parallel()
	cfg, stateDir := newRuntimeAttemptUploadManifestTestFixture(t)
	relative := "runtime/operator-1/vault/st.yml"
	path := filepath.Join(stateDir, filepath.FromSlash(relative))
	wire, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "settings.yml")
	if err := os.WriteFile(backup, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"symlink", "fifo", "mode", "oversize"} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "symlink":
			err = os.Symlink(backup, path)
		case "fifo":
			err = unix.Mkfifo(path, 0o600)
		case "mode":
			err = os.WriteFile(path, wire, 0o644)
			if err == nil {
				err = os.Chmod(path, 0o644)
			}
		case "oversize":
			err = os.WriteFile(path, bytes.Repeat([]byte(" "), maximumRuntimeAttemptUploadConfigBytes+1), 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
		if fault == "mode" {
			info, err := os.Lstat(path)
			if err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("public upload config control has the wrong actual mode: %v", err)
			}
		}
		if digest, _, err := runtimeManifestInputDigest(cfg, stateDir, relative); err == nil || digest != "" {
			t.Fatalf("%s settings acquired a runtime digest: %v", fault, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Profile routing and original YAML spelling are independent of a document's
// self-consistent hash. A second YAML document cannot hide different settings.
func TestRuntimeEvidenceV2AttemptUploadConfigRequiresExactProfileAndYAML(t *testing.T) {
	t.Parallel()
	cfg, stateDir := newRuntimeAttemptUploadManifestTestFixture(t)
	relative := "runtime/operator-1/vault/st.yml"
	path := filepath.Join(stateDir, filepath.FromSlash(relative))
	wire, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{
		strings.Replace(string(wire), "profile: testnet", "profile: mainnet", 1),
		strings.Replace(string(wire), "testnet-enabled: true", "testnet-enabled: false", 1),
		strings.Replace(string(wire), "requests_per_hour: 4096", "requests_per_hour: 3.5", 1),
		strings.Replace(string(wire), "requests_per_hour: 4096", "requests_per_hour: '4096'", 1),
		strings.Replace(string(wire), "testnet-attempt-upload:", "attempt_upload:", 1),
		string(wire) + "profile: testnet\n",
		string(wire) + "attempt_upload: null\n",
		string(wire) + "---\nprofile: mainnet\n",
		"profile: testnet\ntestnet-enabled: true\ndefaults: &quota {requests_per_hour: 4096, bytes_per_hour: 67108864, account_requests_per_hour: 1024, account_bytes_per_hour: 16777216}\ntestnet-attempt-upload: *quota\n",
	} {
		if changed == string(wire) {
			t.Fatal("fault did not change the actual settings")
		}
		if err := atomicWrite(path, []byte(changed), 0o600); err != nil {
			t.Fatal(err)
		}
		if digest, _, err := runtimeManifestInputDigest(cfg, stateDir, relative); err == nil || digest != "" {
			t.Fatalf("noncanonical operator settings acquired runtime upload authority: %v", err)
		}
	}
}

// The actual outer launcher must reject capacity before its first child
// process. The retained namespace fixture owns the real migration executable.
func TestRuntimeEvidenceV2AttemptUploadLaunchRefusesBeforeMigration(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"missing", "zero", "overflow", "account"} {
		cfg, stateDir, plan, roles, bins, marker := validatorNamespaceLaunchBoundaryFixture(t)
		budget := runtimeAttemptUploadTestBudget()
		cfg.Config.Artifacts.AttemptUpload = &budget
		switch fault {
		case "missing":
			cfg.Config.Artifacts.AttemptUpload = nil
		case "zero":
			budget.AccountBytesPerHour = 0
		case "overflow":
			budget.BytesPerHour = 9007199254740992
		case "account":
			budget.AccountRequestsPerHour = budget.RequestsPerHour + 1
		}
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		err := LaunchDeployment(t.Context(), cfg, stateDir, plan, roles, nil, bins, false)
		if _, markerErr := os.Stat(marker); !errors.Is(markerErr, os.ErrNotExist) {
			t.Fatalf("invalid upload quota reached the real migration process (%s): %v", fault, markerErr)
		}
		if err == nil || !strings.Contains(err.Error(), "attempt upload") || !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatalf("outer launch failed capacity admission before mutation (%s): %v", fault, err)
		}
	}
}

// Real post-CREATE recovery would replace the saved deployment before the
// inner renderer sees capacity. Direct dispatch must fence that earlier work.
func TestRuntimeEvidenceV2AttemptUploadDispatchRefusesBeforePayloadRecovery(t *testing.T) {
	t.Parallel()
	executor, reader := deploymentBoundaryTestExecution(t)
	executor.cfg.Public.Chain.EVMPublicReadEndpoint = "https://test.chain.opentensor.ai"
	if executor.cfg.Config.Artifacts.AttemptUpload != nil || executor.payloads != nil {
		t.Fatal("unprovisioned recovery fixture already acquired upload capacity or payloads")
	}
	before := validatorNamespaceTreeSnapshot(t, executor.stateDir)
	err := executor.execute(t.Context(), Action{ID: "config.render"})
	if err == nil || !strings.Contains(err.Error(), "attempt upload") || executor.payloads != nil || !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, executor.stateDir)) {
		t.Fatalf("unprovisioned dispatch recovered payloads before upload admission: %v", err)
	}
	if reader.unexpectedRequests.Load() != 0 {
		t.Fatal("unprovisioned dispatch made an unapproved RPC request")
	}
}

// An existing invalid journal is a deterministic stop for the old apply path,
// after it creates deployment.lock. No plan build or external service is used.
func TestRuntimeEvidenceV2AttemptUploadApplyRefusesBeforeJournalOwnership(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"setup", "launch", "resume", "scenario"} {
		cfg := testResolvedConfig(t)
		stateDir := t.TempDir()
		if err := os.Chmod(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(stateDir, "journal.jsonl"), []byte("retained invalid journal\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		err := runMutation(context.Background(), command, cfg, stateDir, cliOptions{Apply: true, Name: "smoke"})
		if err == nil || !strings.Contains(err.Error(), "attempt upload") || !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatalf("unprovisioned apply acquired journal or setup authority (%s): %v", command, err)
		}
	}
}

// Valid capacity reaches the independently retained journal boundary. This
// prevents a blanket refusal from masquerading as the outer admission fix.
func TestRuntimeEvidenceV2AttemptUploadApplyAdmitsConfiguredJournalBoundary(t *testing.T) {
	t.Parallel()
	cfg := testResolvedConfig(t)
	budget := runtimeAttemptUploadTestBudget()
	cfg.Config.Artifacts.AttemptUpload = &budget
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(stateDir, "journal.jsonl")
	want := []byte("retained invalid journal\n")
	if err := atomicWrite(journalPath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	err := runMutation(context.Background(), "setup", cfg, stateDir, cliOptions{Apply: true})
	if err == nil || !strings.Contains(err.Error(), "journal line 1") || strings.Contains(err.Error(), "attempt upload") {
		t.Fatalf("valid upload capacity did not reach the real journal boundary: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "deployment.lock")); err != nil {
		t.Fatalf("admitted journal owner did not acquire its lock: %v", err)
	}
	if raw, err := os.ReadFile(journalPath); err != nil || !bytes.Equal(raw, want) {
		t.Fatalf("admitted journal failure changed retained bytes: %v", err)
	}
}

// Generated inputs are independently required for each operator even when
// the source vault contains only auth.yml and no st.yml or pg.yml at all.
func TestRuntimeEvidenceV2AttemptUploadManifestOwnsGeneratedOperatorConfigs(t *testing.T) {
	t.Parallel()
	cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
	expected, err := expectedRuntimeConfigFiles(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	var manifest RuntimeConfigManifest
	if err := decodeStrictJSONFile(runtimeConfigManifestPath(stateDir), &manifest); err != nil {
		t.Fatal(err)
	}
	for operator := 1; operator <= 2; operator++ {
		for _, name := range []string{"st.yml", "pg.yml"} {
			if _, err := os.Lstat(filepath.Join(cfg.Repos.Vault, "local", name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("generated config fixture accidentally depends on a copied %s: %v", name, err)
			}
			relative := fmt.Sprintf("runtime/operator-%d/vault/%s", operator, name)
			if mode, found := expected[relative]; !found || mode != 0o600 {
				t.Errorf("generated operator config is missing from private inventory: %s", relative)
				continue
			}
			wire, err := os.ReadFile(filepath.Join(stateDir, filepath.FromSlash(relative)))
			if err != nil {
				t.Fatal(err)
			}
			matches := 0
			for _, entry := range manifest.Files {
				if entry.Path == relative {
					matches++
					if entry.Mode != "0600" || entry.SHA256 != bytesSHA256(wire) {
						t.Errorf("generated config lost its original private byte binding: %s", relative)
					}
				}
			}
			if matches != 1 {
				t.Errorf("generated config appears %d times in actual manifest: %s", matches, relative)
			}
		}
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal("complete generated inventory did not pass real manifest readback", err)
	}
}

// Neither a missing generated input nor an authenticated manifest with that
// input omitted may authorize startup. Changed bytes and modes remain bound.
func TestRuntimeEvidenceV2AttemptUploadManifestRejectsGeneratedConfigOmissionAndTamper(t *testing.T) {
	t.Parallel()
	for operator := 1; operator <= 2; operator++ {
		for _, name := range []string{"st.yml", "pg.yml"} {
			for _, fault := range []string{"missing-file", "missing-entry", "bytes", "mode"} {
				cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
				relative := fmt.Sprintf("runtime/operator-%d/vault/%s", operator, name)
				path := filepath.Join(stateDir, filepath.FromSlash(relative))
				wire, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
					t.Fatal("generated config baseline did not pass actual readback", err)
				}
				switch fault {
				case "missing-file":
					err = os.Rename(path, filepath.Join(stateDir, "retained-generated-source"))
				case "missing-entry":
					rewriteRuntimeConfigManifest(t, stateDir, func(value *RuntimeConfigManifest) {
						kept := value.Files[:0]
						for _, entry := range value.Files {
							if entry.Path != relative {
								kept = append(kept, entry)
							}
						}
						value.Files = kept
					})
				case "bytes":
					err = os.WriteFile(path, append(bytes.Clone(wire), '\n'), 0o600)
				case "mode":
					err = os.Chmod(path, 0o644)
				}
				if err != nil {
					t.Fatal(err)
				}
				if fault == "mode" {
					info, err := os.Lstat(path)
					if err != nil || info.Mode().Perm() != 0o644 {
						t.Fatalf("generated public-mode control is not public: %v", err)
					}
				}
				if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil {
					t.Fatalf("generated config %s admitted %s at real readback", relative, fault)
				}
				if fault == "missing-file" || fault == "mode" {
					if err := writeRuntimeConfigManifest(cfg, stateDir); err == nil {
						t.Fatalf("manifest rebuild adopted %s generated input %s", fault, relative)
					}
				}
			}
		}
	}
}

// Naming two generated inputs cannot authorize their siblings or a wider
// static namespace. Both operator roots retain the exact extra-file refusal.
func TestRuntimeEvidenceV2AttemptUploadGeneratedConfigRejectsExtraFiles(t *testing.T) {
	t.Parallel()
	for operator := 1; operator <= 2; operator++ {
		for _, name := range []string{"st.yml.extra", "pg.yml.extra", "st.yaml"} {
			cfg, stateDir := runtimeConfigManifestFixtureForOperators(t, 2)
			if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "vault", name)
			if err := atomicWrite(path, []byte("unapproved generated sibling\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "unexpected static runtime config") {
				t.Fatalf("extra generated sibling %s gained static authority: %v", name, err)
			}
		}
	}
}

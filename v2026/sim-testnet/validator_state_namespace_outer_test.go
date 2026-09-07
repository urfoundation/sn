package main

// Outer operational entry points preserve protected state before migration or
// payload replacement. Ownership checks and truthful refusal audit remain valid.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Supplies a real approved plan and a test-owned migration executable. The
// executable records admission then exits immediately, without DB/network I/O.
func validatorNamespaceLaunchBoundaryFixture(t *testing.T) (*ResolvedConfig, string, *SetupPlan, *RoleSecrets, map[string]string, string) {
	t.Helper()
	cfg := testResolvedConfig(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Repos.Server = t.TempDir()
	binary := filepath.Join(cfg.Repos.Server, "migration-boundary")
	writeValidatorStateTestFile(t, binary, "#!/bin/sh\nprintf 'test-controlled migration admission\\n' > migration-invoked\nexit 73\n")
	if err := os.Chmod(binary, 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	// The lock may be acquired/released normally; it is not an operational
	// mutation and its existing bytes must not be replaced or erased.
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "supervisor.lock"), "retained ownership lock\n")
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "journal.jsonl"), "retained deployment journal\n")
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "operator-1", "vault", "st.yml"), "retained operator configuration\n")
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "operator-1", "vault", "provider_egress.yml"), "retained original credential\n")
	return cfg, stateDir, plan, roles, map[string]string{"server-ctl": binary}, filepath.Join(cfg.Repos.Server, "migration-invoked")
}

// A later validator's signed disk authority must be checked before the first
// database migration. The controlled executable proves real process admission.
func TestValidatorStateNamespaceLaunchRejectsBeforeDatabaseMigration(t *testing.T) {
	cfg, stateDir, plan, roles, bins, marker := validatorNamespaceLaunchBoundaryFixture(t)
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "validator-1", "state", "operators", "no-1", "stats.json"), `{"assignments":1}`)
	fixture := readValidatorNamespaceStoreFixture(t)
	installValidatorNamespaceStoreFixture(t, filepath.Join(stateDir, "runtime", "validator-2", "state", "operators", "no-1", "attempt-ledger.records"), fixture)
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	err := LaunchDeployment(context.Background(), cfg, stateDir, plan, roles, nil, bins, false)
	if _, markerErr := os.Stat(marker); markerErr == nil {
		t.Fatal("launch invoked database migration before refusing protected disk authority")
	} else if !errors.Is(markerErr, os.ErrNotExist) {
		t.Fatal(markerErr)
	}
	if err == nil || !strings.Contains(err.Error(), "protected disk attempt-ledger state") {
		t.Fatalf("launch did not reach protected disk refusal: %v", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("protected launch changed runtime state, credentials or configuration")
	}
}

// Empty and valid unsigned namespaces still reach the controlled migration
// stage. Read-only preflight must not archive legacy bytes ahead of rendering.
func TestValidatorStateNamespaceLaunchPreflightPreservesUnsignedStartup(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		cfg, stateDir, plan, roles, bins, marker := validatorNamespaceLaunchBoundaryFixture(t)
		if legacy {
			writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "validator-1", "state", "operators", "no-1", "stats.json"), `{"assignments":1}`)
		}
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		err := LaunchDeployment(context.Background(), cfg, stateDir, plan, roles, nil, bins, false)
		if err == nil || !strings.Contains(err.Error(), "operator 1 database migrations: exit status 73") {
			t.Fatalf("legacy=%t did not reach the controlled migration boundary: %v", legacy, err)
		}
		if raw, err := os.ReadFile(marker); err != nil || string(raw) != "test-controlled migration admission\n" {
			t.Fatalf("legacy=%t migration admission marker differs: %v", legacy, err)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatalf("legacy=%t preflight changed or prematurely archived runtime state", legacy)
		}
	}
}

// The ordinary config.render dispatch currently recovers/replaces a persisted
// contract observation and retains payloads before the inner renderer refuses.
// Its existing crash-recovery fixture permits only local read-only RPC methods.
func TestValidatorStateNamespaceConfigRenderRejectsBeforePayloadRecovery(t *testing.T) {
	executor, reader := deploymentBoundaryTestExecution(t)
	executor.cfg.Public.Chain.EVMPublicReadEndpoint = "https://test.chain.opentensor.ai"
	stateDir := executor.stateDir
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "validator-1", "state", "operators", "no-1", "stats.json"), `{"assignments":1}`)
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "operator-1", "vault", "st.yml"), "retained operator configuration\n")
	writeValidatorStateTestFile(t, filepath.Join(stateDir, "runtime", "operator-1", "vault", "provider_egress.yml"), "retained original credential\n")
	fixture := readValidatorNamespaceStoreFixture(t)
	installValidatorNamespaceStoreFixture(t, filepath.Join(stateDir, "runtime", "validator-2", "state", "operators", "no-1", "attempt-ledger.records"), fixture)
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	if executor.payloads != nil {
		t.Fatal("config.render fixture already retained payloads")
	}
	// Exercise the same private dispatch used after Execute's legitimate intent
	// audit. This test does not prohibit that wrapper's refusal journal entry.
	err := executor.execute(context.Background(), Action{ID: "config.render"})
	if err == nil || !strings.Contains(err.Error(), "protected disk attempt-ledger state") {
		t.Fatalf("config.render did not reach protected disk refusal: %v", err)
	}
	if executor.payloads != nil || !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("config.render recovered or retained deployment payloads before refusing protected disk authority")
	}
	if reader.unexpectedRequests.Load() != 0 {
		t.Fatal("config.render attempted an unapproved RPC operation")
	}
}

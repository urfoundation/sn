//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Authority and capture tests retain the complete signed setup, but consume
// only validator-owned files. Use the same admitted rendering stage as fleet
// startup without repeatedly materializing every miner's durable payout key.
func renderFinalValidatorFixtureTest(t *testing.T, fixture *runtimeEvidenceProvisionV2TestFixture) ContractDeployment {
	t.Helper()
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	deployment := fixture.plan.Deployment
	deployment.DeployBlock, deployment.CoordinatorEventStartBlock = 100, 100
	deployment.DeployBlockHash = common.Hash{8}.Hex()
	deployment.CoordinatorEventStartBlockHash = deployment.DeployBlockHash
	if err := saveContractDeployment(fixture.stateDir, deployment); err != nil {
		t.Fatal(err)
	}
	if _, _, err := renderValidatorConfigs(fixture.cfg, fixture.stateDir, fixture.roles, &deployment); err != nil {
		t.Fatal(err)
	}
	return deployment
}

// Withholding unrelated custody makes a return to full-fleet fixture rendering
// fail deterministically at its first miner. Timing and disk speed are not the
// regression oracle. Full renderer tests still load all miners and swarms.
func TestFinalValidatorFixtureDoesNotRequireOrMaterializeMinerCustody(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
	if fixture.cfg.Config.Topology.Miners != 1_000 {
		t.Fatal("validator fixture lost the complete approved miner topology")
	}
	for miner := 1; miner <= fixture.cfg.Config.Topology.Miners; miner++ {
		delete(fixture.roles.Substrate, fmt.Sprintf("miner-%d-payout", miner))
		delete(fixture.roles.Clients, fmt.Sprintf("miner-%d", miner))
	}
	deployment := renderFinalValidatorFixtureTest(t, fixture)
	for id := 1; id <= fixture.cfg.Config.Topology.Validators; id++ {
		path := filepath.Join(fixture.stateDir, "runtime", fmt.Sprintf("validator-%d", id), "validator.yml")
		config, err := validatorpkg.LoadReleaseConfig(path)
		if err != nil {
			t.Fatalf("validator %d actual rendered configuration: %v", id, err)
		}
		if config.ValidatorID != uint64(id) || config.DeployBlock != deployment.CoordinatorEventStartBlock || len(config.Operators) != fixture.cfg.Config.Topology.Operators {
			t.Fatalf("validator %d lost its admitted setup", id)
		}
	}
	if fixture.cfg.Config.Topology.Miners != 1_000 {
		t.Fatal("validator rendering shrank the approved miner topology")
	}
	for _, name := range []string{"runtime", "secrets"} {
		entries, err := os.ReadDir(filepath.Join(fixture.stateDir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "miner-") || strings.HasPrefix(entry.Name(), "claim-relayer-") || strings.HasSuffix(entry.Name(), "-claim-relayer.key") {
				t.Fatalf("validator fixture materialized unrelated custody: %s/%s", name, entry.Name())
			}
		}
	}
}

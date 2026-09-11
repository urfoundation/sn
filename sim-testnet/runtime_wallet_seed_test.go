package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/crv4"
)

func TestSimulatorClientSeedCustodyWalletPreservesProvisionedRole(t *testing.T) {
	stateDir := t.TempDir()
	roleFor := func(seed [32]byte) SubstrateRoleSecret {
		key, err := crv4.KeypairFromSeed(seed)
		if err != nil {
			t.Fatal(err)
		}
		public := key.PublicKey()
		return SubstrateRoleSecret{
			Label: "miner-1-payout", SeedHex: hex.EncodeToString(seed[:]),
			PublicKeyHex: hex.EncodeToString(public[:]), SS58: key.Address(),
		}
	}
	seed := [32]byte{17}
	role := roleFor(seed)
	if err := ensureMinerPayoutSeed(stateDir, 1, role); err != nil {
		t.Fatal(err)
	}
	path := minerPayoutSeedPath(stateDir, 1)
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 {
		t.Fatalf("rendered payout seed custody: %v", err)
	}
	if actual, err := crv4.LoadSeedFile(path); err != nil || actual != seed {
		t.Fatalf("rendered payout seed is not the existing role: %v", err)
	}
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureMinerPayoutSeed(stateDir, 1, role); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("rerender replaced existing payout custody: %v", err)
	}
	for _, changed := range []SubstrateRoleSecret{
		roleFor([32]byte{18}),
		{Label: role.Label, SeedHex: role.SeedHex, SS58: role.SS58, PublicKeyHex: "invalid"},
		{Label: "miner-2-payout", SeedHex: role.SeedHex, SS58: role.SS58, PublicKeyHex: role.PublicKeyHex},
	} {
		if err := ensureMinerPayoutSeed(stateDir, 1, changed); err == nil {
			t.Fatal("changed payout identity was accepted during rerender")
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, beforeBytes) {
			t.Fatalf("refused role change altered occupied seed bytes: %v", err)
		}
	}
}

func TestSimulatorClientSeedCustodyWalletRevisionRequiresFreshRender(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the former renderer's authenticated local action, preserving
	// every transaction, approved configuration and policy field.
	for index := range prior.Actions {
		action := &prior.Actions[index]
		if action.ID == "config.render" {
			delete(action.Parameters, "runtime_config_format")
			action.IntentHash, err = actionIntentHash(*action)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	prior.PlanHash, err = prior.hash()
	if err != nil {
		t.Fatal(err)
	}
	oldRender := actionByID(t, prior, "config.render")
	entries := []JournalEntry{{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash,
		ActionID: oldRender.ID, IntentHash: oldRender.IntentHash, Stage: StageVerified}}
	lock := *cfg.Release
	lock.Repositories = cloneMap(cfg.Release.Repositories)
	lock.Repositories["sn_go_source_hash"] = "sha256:" + strings.Repeat("bc", 32)
	cfg.Release = &lock
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, testSetupFacts(), entries, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if revised.ConfigHash != prior.ConfigHash || revised.PolicyHash != prior.PolicyHash || revised.MaximumSpend != prior.MaximumSpend || revised.Limits != prior.Limits {
		t.Fatal("render format revision changed the approved configuration, policy or spending limits")
	}
	if revised.ReleaseLockHash == prior.ReleaseLockHash || revised.PlanHash == prior.PlanHash || !revised.allowedPlanHashes()[prior.PlanHash] {
		t.Fatal("render format revision lost the source change or authenticated plan lineage")
	}
	currentRender := actionByID(t, revised, "config.render")
	if currentRender.Parameters["runtime_config_format"] != runtimeConfigFormatVersion || currentRender.IntentHash == oldRender.IntentHash || len(currentRender.AcceptedPriorIntentHashes) != 0 || actionAcceptsIntent(currentRender, oldRender.IntentHash) {
		t.Fatal("old render receipt was grandfathered into the new required file format")
	}
	for _, before := range prior.Actions {
		if before.ID != "config.render" && actionByID(t, revised, before.ID).IntentHash != before.IntentHash {
			t.Fatalf("local render revision changed unrelated action %s", before.ID)
		}
	}
	executor := &Executor{cfg: cfg, plan: revised, journal: &Journal{entries: entries}}
	if _, carried := executor.verifiedActionEntry(currentRender); carried {
		t.Fatal("old manifest receipt suppressed the required rerender")
	}
	if err := executor.verifyCarriedActionHistory(context.Background()); err != nil {
		t.Fatalf("old render manifest was checked before required rerender: %v", err)
	}
	// After ordinary rendering and verification, the new exact intent remains
	// eligible for normal receipt reuse; no compatibility alias is necessary.
	executor.journal.entries = append(executor.journal.entries, JournalEntry{
		DeploymentID: revised.DeploymentID, PlanHash: revised.PlanHash,
		ActionID: currentRender.ID, IntentHash: currentRender.IntentHash, Stage: StageVerified,
	})
	if _, verified := executor.verifiedActionEntry(currentRender); !verified {
		t.Fatal("new exact render receipt was not selected")
	}
}

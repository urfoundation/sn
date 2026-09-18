package main

// Reproduces the actual provisioner's per-operator validator keys at real
// proof and settlement consumers, without services, timers, or chain writes.

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Uses only the existing synthetic test wallet. Full provisioner derivation
// remains unchanged, including the 1,000-miner topology and configured M8.
func newSimulatorProvisionedOperatorPathFixture(t *testing.T) *simulatorClientSeedTestFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.Config.Topology.Validators = 1
	cfg.Netuid = 521
	if cfg.Config.Topology.Operators != 2 || cfg.Policy.Verify.TrailDepth != 8 {
		t.Fatal("operator identity fixture lost its two-operator M8 policy")
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first := roles.Clients["validator-1-no-1"]
	second := roles.Clients["validator-1-no-2"]
	if first.PublicKeyHex == "" || second.PublicKeyHex == "" || first.PublicKeyHex == second.PublicKeyHex {
		t.Fatal("real provisioner did not derive independent operator path keys")
	}
	operatorSeedKVs := map[uint64][]byte{}
	for noID, role := range map[uint64]ClientRoleSecret{1: first, 2: second} {
		seed, err := hex.DecodeString(role.SeedHex)
		if err != nil || len(seed) != ed25519.SeedSize {
			t.Fatalf("operator %d provisioned test seed is invalid", noID)
		}
		operatorSeedKVs[noID] = seed
	}
	fixture := newSimulatorClientSeedTestFixtureWithOperatorSeeds(t, cfg, operatorSeedKVs)
	data, err := validatorpkg.ReadAttemptSettlementClosure(fixture.root, 42)
	if err != nil {
		t.Fatal(err)
	}
	serverKeyKVs := map[uint64]map[byte]ed25519.PublicKey{}
	for _, operator := range fixture.terminal.Operators {
		serverKeyKVs[uint64(operator.NoID)] = map[byte]ed25519.PublicKey{
			1: operator.VerifyKeys[0].PublicKey,
		}
	}
	closure, err := validatorpkg.DecodeAttemptSettlementClosureWithServerKeys(data, serverKeyKVs)
	if err != nil || len(closure.Transitions) != 2 {
		t.Fatalf("real producer's fully signed operator closure is invalid: %v", err)
	}
	for _, transition := range closure.Transitions {
		seed, ok := operatorSeedKVs[transition.Identity.NoID]
		if !ok {
			t.Fatal("signed closure added an unprovisioned operator")
		}
		want := "0x" + hex.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
		if transition.Identity.ValidatorVPK != want || transition.Identity.ValidatorID != 1 || transition.Identity.Netuid != 521 {
			t.Fatal("real signed closure differs from the provisioned operator identity")
		}
	}
	return fixture
}

// The real proof inspector already routes both complete M8 trails correctly;
// a malformed fixture cannot stand in for a collector identity failure.
func TestSimulatorOperatorPathIdentityProvisionedProofsAndClosure(t *testing.T) {
	fixture := newSimulatorProvisionedOperatorPathFixture(t)
	if err := fixture.read(t, "proofs"); err != nil {
		t.Fatalf("genuine provisioned operator path proofs were rejected: %v", err)
	}
}

// A terminal batch is valid with the distinct keys that setup actually writes.
// An explicit wait callback makes any needless retry a deterministic failure.
func TestSimulatorOperatorPathIdentityClosureWaitAcceptsDistinctKeys(t *testing.T) {
	fixture := newSimulatorProvisionedOperatorPathFixture(t)
	if err := fixture.read(t, "closures"); err != nil {
		t.Fatalf("terminal closure waiter rejected genuine per-operator provisioned keys: %v", err)
	}
}

// Requiring every local authority includes operator two, even when a legacy
// shared-key batch is cryptographically valid. No fixture key is repaired.
func TestSimulatorOperatorPathIdentityWaitRejectsUnsafeSecondKey(t *testing.T) {
	for _, kind := range []string{"leaf-symlink", "hardlink", "ancestor-symlink", "file-0644", "file-0660", "parent-0775", "parent-0777"} {
		fixture := newSimulatorClientSeedTestFixture(t)
		fixture.seedPath = filepath.Join(fixture.root, "operators", "no-2", "client.key")
		if kind == "ancestor-symlink" {
			parent := filepath.Dir(fixture.seedPath)
			if err := os.Rename(parent, parent+".owned"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("no-2.owned", parent); err != nil {
				t.Fatal(err)
			}
		} else {
			fixture.makeUnsafe(t, kind)
		}
		err := fixture.read(t, "closures")
		if err == nil {
			t.Errorf("terminal closure waiter accepted unsafe operator-2 custody: %s", kind)
		} else if !strings.Contains(err.Error(), "terminal closure path identity is unavailable") {
			t.Fatalf("%s did not fail at the second operator seed boundary: %v", kind, err)
		}
	}
}

// Per-operator routing never permits another operator's server signature to
// authenticate a trail; this uses the actual cryptographic closure decoder.
func TestSimulatorOperatorPathIdentityRejectsSwappedServerKeys(t *testing.T) {
	fixture := newSimulatorProvisionedOperatorPathFixture(t)
	data, err := validatorpkg.ReadAttemptSettlementClosure(fixture.root, 42)
	if err != nil {
		t.Fatal(err)
	}
	serverKeyKVs := map[uint64]map[byte]ed25519.PublicKey{
		1: {1: fixture.terminal.Operators[1].VerifyKeys[0].PublicKey},
		2: {1: fixture.terminal.Operators[0].VerifyKeys[0].PublicKey},
	}
	if _, err := validatorpkg.DecodeAttemptSettlementClosureWithServerKeys(data, serverKeyKVs); err == nil {
		t.Fatal("operator closure accepted server keys from the wrong operator")
	}
}

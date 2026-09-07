package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoleSecretsAreDeterministicDistinctAndPublicViewIsRedacted(t *testing.T) {
	cfg := testResolvedConfig(t)
	a, err := buildRoleSecretsUncached(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildRoleSecretsUncached(cfg)
	if err != nil {
		t.Fatal(err)
	}
	af, _ := a.secretFingerprint()
	bf, _ := b.secretFingerprint()
	if af != bf {
		t.Fatalf("role derivation is not deterministic: %s != %s", af, bf)
	}
	if validatorHotkeyLabel(1) != "reserve-hotkey" || a.Substrate["reserve-hotkey"].SeedHex == "" || a.Substrate["escrow-hotkey"].SeedHex == "" {
		t.Fatal("reserve-validator or escrow registration roles are missing")
	}
	if _, exists := a.Substrate["validator-1-hotkey"]; exists {
		t.Fatal("validator 1 has an unused hotkey distinct from the reserve validator")
	}
	wantSubstrate := 2 + 2*cfg.Config.Topology.fleetCandidates() + 2*cfg.Config.Topology.ChurnFloorUIDs + 3*cfg.Config.Topology.Operators + 2*cfg.Config.Topology.Validators - 1 + cfg.Config.Topology.Miners
	wantSubstrate += int(maximumContractRegistrationGeneration(cfg.Config.Topology)) * contractRegistrationRoleCount(cfg.Config.Topology)
	if len(a.Substrate) != wantSubstrate || len(a.Clients) != cfg.Config.Topology.Miners+cfg.Config.Topology.Validators*cfg.Config.Topology.Operators || a.Substrate[fmt.Sprintf("miner-%d-payout", cfg.Config.Topology.Miners)].SeedHex == "" {
		t.Fatalf("full launch topology was not derived: substrate=%d/%d clients=%d", len(a.Substrate), wantSubstrate, len(a.Clients))
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		if a.EVM[fmt.Sprintf("operator-%d-claim-relayer", i)].PrivateKeyHex == "" {
			t.Fatalf("operator %d claim relayer role is missing", i)
		}
	}
	seen := map[string]string{}
	for label, role := range a.EVM {
		if prior := seen[role.Address]; prior != "" {
			t.Fatalf("EVM roles %s and %s share %s", prior, label, role.Address)
		}
		seen[role.Address] = label
	}
	public, err := json.Marshal(a.Public())
	if err != nil {
		t.Fatal(err)
	}
	text := string(public)
	for _, role := range a.EVM {
		if strings.Contains(text, role.PrivateKeyHex) {
			t.Fatalf("public identities leaked EVM private key for %s", role.Label)
		}
	}
	for _, role := range a.Substrate {
		if strings.Contains(text, role.SeedHex) {
			t.Fatalf("public identities leaked substrate seed for %s", role.Label)
		}
	}
	for _, role := range a.Clients {
		if strings.Contains(text, role.SeedHex) {
			t.Fatalf("public identities leaked client seed for %s", role.Label)
		}
	}
}

func TestRoleSecretsCacheIsKeyedAndReturnsDetachedCopies(t *testing.T) {
	cfg := testResolvedConfig(t)
	first, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	role := first.Substrate["miner-1-payout"]
	role.SeedHex = "mutated"
	first.Substrate["miner-1-payout"] = role
	second, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if second.Substrate["miner-1-payout"].SeedHex == "mutated" {
		t.Fatal("role-secret cache returned shared mutable state")
	}
	originalKey, err := roleSecretsCacheKey(cfg)
	if err != nil {
		t.Fatal(err)
	}
	changed := *cfg
	changed.WalletMaterial += "-changed"
	changedKey, err := roleSecretsCacheKey(&changed)
	if err != nil {
		t.Fatal(err)
	}
	if originalKey == changedKey {
		t.Fatal("wallet-material change did not invalidate the role-secret cache")
	}
}

func TestRoleStorePermissionsAndStableAssignedClientIDs(t *testing.T) {
	cfg := testResolvedConfig(t)
	dir := t.TempDir()
	roles, err := LoadOrWriteRoleSecrets(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	client := roles.Clients["miner-1"]
	client.ClientIDHex = strings.Repeat("ab", 16)
	roles.Clients["miner-1"] = client
	path := filepath.Join(dir, "secrets", "roles.json")
	if err := saveRoleSecrets(path, roles); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOrWriteRoleSecrets(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Clients["miner-1"].ClientIDHex != client.ClientIDHex {
		t.Fatal("server-assigned client id was regenerated")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("role store mode = %o", info.Mode().Perm())
	}
}

func TestRoleStoreExtendsOnlyMissingDeterministicContractGenerations(t *testing.T) {
	cfg := testResolvedConfig(t)
	expected, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	legacy := cloneRoleSecrets(expected)
	for label := range legacy.Substrate {
		generation, _, _, contractRole := parseContractRegistrationRoleLabel(cfg.Config.Topology, label)
		if contractRole && generation > 0 {
			delete(legacy.Substrate, label)
		}
	}
	client := legacy.Clients["miner-1"]
	client.ClientIDHex = strings.Repeat("ab", 16)
	legacy.Clients["miner-1"] = client
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets", "roles.json")
	if err := saveRoleSecrets(path, legacy); err != nil {
		t.Fatal(err)
	}
	extended, err := LoadOrWriteRoleSecrets(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(extended.Substrate) != len(expected.Substrate) || extended.Clients["miner-1"].ClientIDHex != client.ClientIDHex {
		t.Fatalf("role extension lost identities or assigned client ID: substrate=%d/%d client=%q", len(extended.Substrate), len(expected.Substrate), extended.Clients["miner-1"].ClientIDHex)
	}
	for _, label := range contractRegistrationRoleLabels(cfg.Config.Topology, 1) {
		if extended.Substrate[label] != expected.Substrate[label] {
			t.Fatalf("extended role %s is not the deterministic identity", label)
		}
	}
	reloaded, err := LoadOrWriteRoleSecrets(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint, _ := extended.secretFingerprint(); fingerprint == "" {
		t.Fatal("extended role store has no fingerprint")
	} else if reloadedFingerprint, _ := reloaded.secretFingerprint(); fingerprint != reloadedFingerprint {
		t.Fatal("persisted role extension is not stable")
	}
}

func TestRoleStoreExtensionRejectsMissingMutatedAndExtraAdjacentRoles(t *testing.T) {
	cfg := testResolvedConfig(t)
	expected, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*RoleSecrets)
	}{
		{name: "missing base role", mutate: func(roles *RoleSecrets) { delete(roles.Substrate, "reserve-hotkey") }},
		{name: "mutated generation role", mutate: func(roles *RoleSecrets) {
			label := escrowHotkeyLabelForGeneration(1)
			role := roles.Substrate[label]
			role.SeedHex = strings.Repeat("00", 32)
			roles.Substrate[label] = role
		}},
		{name: "extra substrate role", mutate: func(roles *RoleSecrets) {
			role := roles.Substrate["reserve-hotkey"]
			role.Label = "foreign"
			roles.Substrate["foreign"] = role
		}},
		{name: "missing EVM role", mutate: func(roles *RoleSecrets) { delete(roles.EVM, "guardian") }},
		{name: "mutated client role", mutate: func(roles *RoleSecrets) {
			role := roles.Clients["miner-1"]
			role.SeedHex = strings.Repeat("11", 32)
			roles.Clients["miner-1"] = role
		}},
	}
	for _, test := range tests {
		got := cloneRoleSecrets(expected)
		test.mutate(got)
		if _, _, err := extendRoleSecretsWithContractGenerations(cfg.Config.Topology, got, expected); err == nil {
			t.Errorf("%s was accepted", test.name)
		}
	}
}

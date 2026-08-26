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
	a, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildRoleSecrets(cfg)
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
	for i := 1; i <= cfg.Config.Topology.Miners; i++ {
		if a.EVM[fmt.Sprintf("miner-%d-claim-relayer", i)].PrivateKeyHex == "" {
			t.Fatalf("miner %d claim relayer role is missing", i)
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

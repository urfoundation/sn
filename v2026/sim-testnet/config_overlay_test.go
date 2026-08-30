package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testOperatorConfigSources(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"local/settings.yml":               "all: {}\n",
		"local/redis.yml":                  "authority: local\n",
		"all/apple_roots.pem":              "certificate\n",
		"all/iso-country-list.yml":         "US: United States\n",
		"all/city-list.yml":                "US: {}\n",
		"all/mmdb/2026.7.2/ip-ipinfo.mmdb": "mmdb\n",
		"all/arindb/2026.2.18/arin.mmdb":   "arin\n",
	}
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestOperatorConfigOverlayExposesLocalAndSharedResources(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Repos.PlatformConfig = testOperatorConfigSources(t)
	stateDir := t.TempDir()
	if err := ensureOperatorConfigOverlays(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	if err := ensureOperatorConfigOverlays(cfg, stateDir); err != nil {
		t.Fatalf("idempotent overlay preparation failed: %v", err)
	}
	if err := validateOperatorConfigOverlays(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		home := operatorConfigHome(stateDir, operator)
		for _, path := range []string{
			filepath.Join(home, operatorEnvironment(operator), "settings.yml"),
			filepath.Join(home, "all", "apple_roots.pem"),
			filepath.Join(home, "all", "iso-country-list.yml"),
			filepath.Join(home, "all", "city-list.yml"),
			filepath.Join(home, "all", "mmdb", "2026.7.2", "ip-ipinfo.mmdb"),
			filepath.Join(home, "all", "arindb", "2026.2.18", "arin.mmdb"),
		} {
			if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("resolved operator resource %s is unavailable: %v", path, err)
			}
		}
	}
}

func TestOperatorConfigOverlayFailsClosedForMissingOrRepointedSources(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Repos.PlatformConfig = testOperatorConfigSources(t)
	missing := filepath.Join(cfg.Repos.PlatformConfig, "all", "arindb", "2026.2.18", "arin.mmdb")
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	if err := validateOperatorConfigSources(cfg); err == nil || !strings.Contains(err.Error(), "arin.mmdb") {
		t.Fatalf("missing shared resource was accepted: %v", err)
	}
	if err := os.WriteFile(missing, []byte("arin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	home := operatorConfigHome(stateDir, 1)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	wrong := filepath.Join(t.TempDir(), "wrong")
	if err := os.Mkdir(wrong, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(wrong, filepath.Join(home, operatorEnvironment(1))); err != nil {
		t.Fatal(err)
	}
	if err := ensureOperatorConfigOverlays(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "not the approved link") {
		t.Fatalf("repointed operator config overlay was accepted: %v", err)
	}
}

func TestConfigRenderIntentBindsOperatorResourceOverlay(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		if action.ID == "config.render" {
			if action.Parameters["operator_config_overlay"] != operatorConfigOverlayVersion {
				t.Fatalf("config render parameters = %v", action.Parameters)
			}
			return
		}
	}
	t.Fatal("config.render action is missing")
}

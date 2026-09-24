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
		"all/mmdb/2000.1.2/ip-ipinfo.mmdb": "synthetic mmdb\n",
		"all/city-list.yml":                "cities: []\n",
		"all/iso-country-list.yml":         "countries: []\n",
		"all/arindb/2000.1.2/arin.mmdb":    "synthetic arin\n",
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
	for _, name := range []string{"geolite2.mmdb", "places.yml"} {
		matches, err := filepath.Glob(filepath.Join(cfg.Repos.PlatformConfig, "all", "mmdb", "*", name))
		if err != nil || len(matches) != 0 {
			t.Fatalf("current server fixture unexpectedly requires future resource %s: %v, %v", name, matches, err)
		}
	}
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
			filepath.Join(home, "all", "mmdb", "2000.1.2", "ip-ipinfo.mmdb"),
			filepath.Join(home, "all", "city-list.yml"),
			filepath.Join(home, "all", "iso-country-list.yml"),
			filepath.Join(home, "all", "arindb", "2000.1.2", "arin.mmdb"),
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
	missing := filepath.Join(cfg.Repos.PlatformConfig, "all", "arindb", "2000.1.2", "arin.mmdb")
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

// Unrelated databases cannot replace the exact one the server opens. Missing
// and empty seeder resources must also fail before any overlay is published.
func TestOperatorConfigOverlayRequiresConsumedCurrentResources(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.Repos.PlatformConfig = testOperatorConfigSources(t)
	for _, name := range []string{"geolite2.mmdb", "ip.mmdb", "places.yml"} {
		path := filepath.Join(cfg.Repos.PlatformConfig, "all", "mmdb", "2000.1.2", name)
		if err := os.WriteFile(path, []byte("unrelated synthetic resource\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{
		"mmdb/2000.1.2/ip-ipinfo.mmdb",
		"arindb/2000.1.2/arin.mmdb",
		"apple_roots.pem",
		"city-list.yml",
		"iso-country-list.yml",
	} {
		path := filepath.Join(cfg.Repos.PlatformConfig, "all", filepath.FromSlash(name))
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		stateDir := t.TempDir()
		if err := ensureOperatorConfigOverlays(cfg, stateDir); err == nil || !strings.Contains(err.Error(), filepath.Base(name)) {
			t.Fatalf("missing consumed resource %s was accepted: %v", name, err)
		}
		if _, err := os.Lstat(operatorConfigHome(stateDir, 1)); !os.IsNotExist(err) {
			t.Fatalf("missing resource %s published an operator overlay: %v", name, err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := validateOperatorConfigSources(cfg); err == nil || !strings.Contains(err.Error(), filepath.Base(name)) {
			t.Fatalf("empty consumed resource %s was accepted: %v", name, err)
		}
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := validateOperatorConfigSources(cfg); err != nil {
			t.Fatalf("restored consumed resource %s remained unavailable: %v", name, err)
		}
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

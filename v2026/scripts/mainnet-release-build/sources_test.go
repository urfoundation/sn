// Source admission keeps immutable API bytes and the nested RPC fork together.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// These public module identities are the independently promoted release inputs.
func buildFixturePromotedApiModules() []goModule {
	return []goModule{
		{Path: "github.com/urnetwork/sdk/v2026", Replace: &goModule{Path: "github.com/urnetwork/sdk/v2026", Version: "v0.0.0-20261001021058-5d37be3876e5", Sum: "h1:PYuzGCWhMRuCnZS9qoSeMvwSZk9NQzX861Ir5xFFpHA=", GoModSum: "h1:IuPYnLZ5j1SP+gm3O4a4SxbOT4U/CPKEqONI1iLO5yk="}},
		{Path: "github.com/urnetwork/connect/v2026", Replace: &goModule{Path: "github.com/urnetwork/connect/v2026", Version: "v0.0.0-20261001021459-e1b5d77b5029", Sum: "h1:jcnGC6MmKNn2x6HU6RfYHM24V2//K6myMw9ajM0MQsg=", GoModSum: "h1:A88Ceqd8zbfpUB4+1pTsT1G3kiS9kgB22ZuljBZVw4k="}},
		{Path: "github.com/pion/sctp", Replace: &goModule{Path: "github.com/urnetwork/connect/v2026/sctp", Version: "v0.0.0-20261001021459-e1b5d77b5029", Sum: "h1:GfovpOIWKd6TjkYsWLkMNE/zKEoGNC/5NfrPRCI+50A=", GoModSum: "h1:7KFmTwLcoYgJs/Z+99nJvsWL0qDpuyloSI0RbAqlrz0="}},
	}
}

// The old allowlist refused the already promoted SDK, Connect and SCTP graph.
func TestReleaseBuildAcceptsPromotedApiPins(t *testing.T) {
	for _, module := range buildFixturePromotedApiModules() {
		if err := validateReleaseModule(module); err != nil {
			t.Errorf("promoted %s rejected: %v", module.Path, err)
		}
	}
}

// Every bound field matters; a version alone cannot authenticate source bytes.
func TestReleaseBuildRejectsPromotedApiPinSubstitution(t *testing.T) {
	for _, module := range buildFixturePromotedApiModules() {
		for _, field := range []string{"path", "version", "sum", "go.mod sum"} {
			replacement := *module.Replace
			switch field {
			case "path":
				replacement.Path = "modules.example/substitute"
			case "version":
				replacement.Version = "v0.0.0-synthetic"
			case "sum":
				replacement.Sum = "h1:synthetic-body"
			case "go.mod sum":
				replacement.GoModSum = "h1:synthetic-module"
			}
			if err := validateReleaseModule(goModule{Path: module.Path, Replace: &replacement}); err == nil {
				t.Errorf("accepted %s substitution for %s", field, module.Path)
			}
		}
	}
}

// A physically tracked nested module inherits SN's exact source pin in both
// main-module graphs, even though their relative replacement strings differ.
func TestReleaseBuildPinsNestedRpcForkToSnCommit(t *testing.T) {
	workspace := t.TempDir()
	directory := filepath.Join(workspace, "sn", "third_party/go-substrate-rpc-client")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	goMod := filepath.Join(directory, "go.mod")
	if err := os.WriteFile(goMod, []byte("module modules.example/synthetic-fork\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repo := repositoryPin{Name: "sn", Path: "sn", Commit: strings.Repeat("a", 40)}
	config := buildConfig{Workspace: workspace, Output: t.TempDir(), Git: toolPin{Path: git}, Repositories: []repositoryPin{repo}}
	root := filepath.Join(workspace, repo.Path)
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "--", "third_party/go-substrate-rpc-client/go.mod"}} {
		if _, err := buildQuery(t.Context(), root, buildEnvironment(config.Output), git, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := buildModuleRepository(t.Context(), config, directory)
	if err != nil || owner != repo {
		t.Fatalf("nested source ownership differs: %+v, %v", owner, err)
	}
	artifact, err := buildFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"./third_party/go-substrate-rpc-client", "../sn/third_party/go-substrate-rpc-client"} {
		module := buildModule{Path: "github.com/centrifuge/go-substrate-rpc-client/v4", Version: "v4.2.2-0.20240919131012-e3b938563803", EffectivePath: path, Directory: directory, Repository: owner.Name, Commit: owner.Commit, GoModSha256: artifact.Sha256}
		if err := validateReleaseRpcFork(config, module); err != nil {
			t.Fatalf("pinned fork rejected for %s: %v", path, err)
		}
		info := &debug.BuildInfo{Deps: []*debug.Module{{Path: module.Path, Version: module.Version, Replace: &debug.Module{Path: path, Version: "(devel)"}}}}
		if err := validateLinkedBuildModules(info, []buildModule{module}); err != nil {
			t.Fatalf("linked fork lost SN provenance for %s: %v", path, err)
		}
	}
	if _, err := buildQuery(t.Context(), root, buildEnvironment(config.Output), git, "rm", "--cached", "--", "third_party/go-substrate-rpc-client/go.mod"); err != nil {
		t.Fatal(err)
	}
	if _, err := buildModuleRepository(t.Context(), config, directory); err == nil {
		t.Fatal("untracked nested fork retained pinned source ownership")
	}
}

// Neither upstream nor another clean local module substitutes for the fork.
func TestReleaseBuildRejectsRpcForkIdentitySubstitution(t *testing.T) {
	repo := repositoryPin{Name: "sn", Path: "sn", Commit: strings.Repeat("a", 40)}
	config := buildConfig{Workspace: t.TempDir(), Repositories: []repositoryPin{repo}}
	for _, field := range []string{"directory", "repository", "commit", "remote", "graph only", "go.mod"} {
		module := buildModule{Path: "github.com/centrifuge/go-substrate-rpc-client/v4", Directory: filepath.Join(config.Workspace, "sn", "third_party/go-substrate-rpc-client"), Repository: "sn", Commit: repo.Commit, GoModSha256: buildFixtureDigest("synthetic go.mod")}
		switch field {
		case "directory":
			module.Directory = filepath.Join(config.Workspace, "sn", "substitute")
		case "repository":
			module.Repository = "server"
		case "commit":
			module.Commit = strings.Repeat("b", 40)
		case "remote":
			module.EffectiveVersion = "v4.2.2"
		case "graph only":
			module.GraphOnly = true
		case "go.mod":
			module.GoModSha256 = ""
		}
		if err := validateReleaseRpcFork(config, module); err == nil {
			t.Errorf("accepted RPC fork %s substitution", field)
		}
	}
}

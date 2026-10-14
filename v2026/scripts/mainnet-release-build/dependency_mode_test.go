// Each dependency mode admits only its own reviewed source: checksum-bound
// published modules, or census checkouts at their recorded commits and trees.
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A complete synthetic config for one mode; pinned omits the three local
// census repositories.
func buildFixtureConfig(mode string) buildConfig {
	config := buildConfig{Schema: buildSchema, CandidateId: "synthetic", Version: "synthetic", SourceDateEpoch: 1, Workspace: "/synthetic/workspace", Output: "/synthetic/output", DependencyMode: mode}
	names := []string{"sn", "server", "proxy", "glog", "goidenticons", "userwireguard", "warp"}
	if mode == "local" {
		names = append(names, "connect", "sdk", "gvisor")
	}
	for _, name := range names {
		config.Repositories = append(config.Repositories, repositoryPin{Name: name, Path: name, Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40)})
	}
	for _, name := range []string{"forge-std", "openzeppelin-contracts", "openzeppelin-contracts-upgradeable"} {
		config.Repositories = append(config.Repositories, repositoryPin{Name: name, Path: "sn/evm/lib/" + name, Commit: strings.Repeat("c", 40), Tree: strings.Repeat("d", 40)})
	}
	return config
}

// Only the two reviewed modes exist; a near miss fails closed before the census.
func TestReleaseBuildRefusesUnknownDependencyMode(t *testing.T) {
	for _, mode := range []string{"mixed", "Pinned", "LOCAL", " local", "sibling"} {
		config := buildFixtureConfig("local")
		config.DependencyMode = mode
		if err := config.validate(); err == nil || err.Error() != "dependency mode must be pinned or local" {
			t.Errorf("dependency mode %q was not refused as unknown: %v", mode, err)
		}
	}
}

// An existing config without the field keeps its original pinned meaning.
func TestReleaseBuildOmittedDependencyModeKeepsPinnedCensus(t *testing.T) {
	config := buildFixtureConfig("")
	if err := config.validate(); err != nil || config.dependencyMode() != "pinned" {
		t.Fatalf("omitted mode did not keep the pinned census: %q %v", config.dependencyMode(), err)
	}
	config = buildFixtureConfig("local")
	config.DependencyMode = ""
	if err := config.validate(); err == nil {
		t.Fatal("omitted mode admitted the local census repositories")
	}
}

// Each mode requires its exact census: pinned refuses the local checkouts and
// local requires them, each under its own name as path and fully pinned.
func TestReleaseBuildDependencyModeSelectsItsCensus(t *testing.T) {
	for _, mode := range []string{"pinned", "local"} {
		if err := buildFixtureConfig(mode).validate(); err != nil {
			t.Fatalf("complete %s census rejected: %v", mode, err)
		}
	}
	pinned := buildFixtureConfig("local")
	pinned.DependencyMode = "pinned"
	if err := pinned.validate(); err == nil {
		t.Error("pinned mode admitted connect, sdk and gvisor checkouts")
	}
	for _, name := range []string{"connect", "sdk", "gvisor"} {
		for _, change := range []string{"missing", "path", "commit", "tree"} {
			config := buildFixtureConfig("local")
			for index, repo := range config.Repositories {
				if repo.Name != name {
					continue
				}
				switch change {
				case "missing":
					config.Repositories = append(config.Repositories[:index], config.Repositories[index+1:]...)
				case "path":
					config.Repositories[index].Path = "sn/" + name
				case "commit":
					config.Repositories[index].Commit = "main"
				case "tree":
					config.Repositories[index].Tree = ""
				}
				break
			}
			if err := config.validate(); err == nil {
				t.Errorf("local mode accepted a %s %s census repository", change, name)
			}
		}
	}
}

// A local build takes each API module and the gVisor fork from its own census
// checkout, at its recorded commit, and from nothing else.
func TestReleaseBuildLocalModeAdmitsOnlyCensusCheckouts(t *testing.T) {
	config := buildFixtureConfig("local")
	targets := []struct{ path, version, replacement, repository, subdirectory string }{
		{"github.com/urnetwork/sdk/v2026", "v0.0.0", "../sdk", "sdk", ""},
		{"github.com/urnetwork/connect/v2026", "v0.0.0", "../connect", "connect", ""},
		{"github.com/pion/sctp", "v1.11.1", "../connect/sctp", "connect", "sctp"},
		{"gvisor.dev/gvisor", "v0.0.0-20260909230408-9b1144b679cb", "../gvisor", "gvisor", ""},
	}
	for _, target := range targets {
		admitted := buildModule{Path: target.path, Version: target.version, EffectivePath: target.replacement, Directory: filepath.Join(config.Workspace, target.repository, target.subdirectory), Repository: target.repository, Commit: strings.Repeat("a", 40), GoModSha256: buildFixtureDigest(target.path)}
		if err := validateLocalReleaseModule(config, admitted); err != nil {
			t.Fatalf("census checkout of %s rejected: %v", target.path, err)
		}
		for _, change := range []string{"non-census path", "other census checkout", "nested directory", "unrecorded commit", "published module", "graph only", "go.mod"} {
			module := admitted
			switch change {
			case "non-census path":
				module.EffectivePath, module.Directory, module.Repository, module.Commit = "../elsewhere", filepath.Join(config.Workspace, "elsewhere"), "", ""
			case "other census checkout":
				module.Directory, module.Repository = filepath.Join(config.Workspace, "warp"), "warp"
			case "nested directory":
				module.Directory = filepath.Join(module.Directory, "nested")
			case "unrecorded commit":
				module.Commit = strings.Repeat("e", 40)
			case "published module":
				module.EffectivePath, module.EffectiveVersion, module.Repository, module.Commit = target.path, "v0.0.0-20261001021058-5d37be3876e5", "", ""
			case "graph only":
				module.GraphOnly = true
			case "go.mod":
				module.GoModSha256 = ""
			}
			if err := validateLocalReleaseModule(config, module); err == nil {
				t.Errorf("local mode accepted %s for %s", change, target.path)
			}
		}
	}
	if err := validateLocalReleaseModule(config, buildModule{Path: "modules.example/other", EffectiveVersion: "v1.0.0"}); err != nil {
		t.Fatalf("an unrelated module was held to a census checkout: %v", err)
	}
}

// Ownership precedes the census check: a replacement into a checkout outside the
// recorded repositories is refused even when that directory is clean Git.
func TestReleaseBuildLocalReplaceOutsideCensusIsRefused(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	config := buildConfig{Workspace: workspace, Output: t.TempDir(), DependencyMode: "local", Git: toolPin{Path: git}, Repositories: []repositoryPin{{Name: "sdk", Path: "sdk"}}}
	for _, name := range []string{"sdk", "elsewhere"} {
		root := filepath.Join(workspace, name)
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module modules.example/"+name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "--quiet"}, {"add", "--", "go.mod"}} {
			if _, err := buildQuery(t.Context(), root, buildEnvironment(config.Output), git, args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	if owner, err := buildModuleRepository(t.Context(), config, filepath.Join(workspace, "sdk")); err != nil || owner.Name != "sdk" {
		t.Fatalf("census sibling ownership differs: %+v, %v", owner, err)
	}
	if _, err := buildModuleRepository(t.Context(), config, filepath.Join(workspace, "elsewhere")); err == nil || !strings.Contains(err.Error(), "escapes the pinned source closure") {
		t.Fatalf("replacement outside the census was not refused: %v", err)
	}
}

// A local build binds each sibling like every other repository: a physical
// clean checkout at the recorded commit and tree.
func TestReleaseBuildLocalModeRefusesDirtyMovedOrLinkedSibling(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(workspace, "sdk")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module modules.example/sdk\n"), 0600); err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		raw, err := buildQuery(t.Context(), root, append(buildEnvironment(output), "GIT_AUTHOR_NAME=synthetic", "GIT_AUTHOR_EMAIL=synthetic@example.invalid", "GIT_COMMITTER_NAME=synthetic", "GIT_COMMITTER_EMAIL=synthetic@example.invalid"), git, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(raw))
	}
	run("init", "--quiet")
	run("add", "--", "go.mod")
	run("commit", "--quiet", "-m", "synthetic sibling")
	repo := repositoryPin{Name: "sdk", Path: "sdk", Commit: run("rev-parse", "HEAD"), Tree: run("rev-parse", "HEAD^{tree}")}
	config := buildConfig{Workspace: workspace, Output: output, DependencyMode: "local", Git: toolPin{Path: git}, Repositories: []repositoryPin{repo}}
	if err := verifyBuildSources(t.Context(), config); err != nil {
		t.Fatalf("clean recorded sibling rejected: %v", err)
	}
	untracked := filepath.Join(root, "untracked.go")
	if err := os.WriteFile(untracked, []byte("package sdk\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyBuildSources(t.Context(), config); err == nil {
		t.Error("untracked sibling source was accepted")
	}
	if err := os.Remove(untracked); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module modules.example/modified\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyBuildSources(t.Context(), config); err == nil {
		t.Error("modified sibling source was accepted")
	}
	run("commit", "--quiet", "-am", "move the sibling")
	if err := verifyBuildSources(t.Context(), config); err == nil {
		t.Error("sibling moved from its recorded commit was accepted")
	}
	config.Repositories[0].Commit, config.Repositories[0].Tree = run("rev-parse", "HEAD"), run("rev-parse", "HEAD^{tree}")
	if err := verifyBuildSources(t.Context(), config); err != nil {
		t.Fatalf("re-recorded sibling rejected: %v", err)
	}
	linked := filepath.Join(workspace, "linked")
	if err := os.Rename(root, linked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linked, root); err != nil {
		t.Fatal(err)
	}
	if err := verifyBuildSources(t.Context(), config); err == nil {
		t.Error("symlinked sibling checkout was accepted")
	}
}

// The manifest records the mode it built with, while a manifest sealed before
// the mode existed keeps its original bytes and seal.
func TestReleaseBuildManifestRecordsDependencyMode(t *testing.T) {
	before := buildFixtureManifest()
	if err := sealBuildManifest(&before); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "dependency_mode") {
		t.Fatal("a manifest without a recorded mode gained a dependency_mode field")
	}
	for _, mode := range []string{"pinned", "local"} {
		manifest := buildFixtureManifest()
		manifest.DependencyMode = mode
		if err := sealBuildManifest(&manifest); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"dependency_mode":"`+mode+`"`) || manifest.ContentHash == before.ContentHash {
			t.Fatalf("%s mode is not recorded under the manifest seal", mode)
		}
	}
}

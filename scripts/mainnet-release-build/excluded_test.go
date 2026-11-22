// No release build requires or links a module that the release-1.0 source
// freeze excludes from release qualification.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

// The builder excludes exactly the source freeze's non-release modules, in its
// order, so the classification has one reviewed list.
func TestReleaseBuildExcludesExactlyTheFreezeNonReleaseModules(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "check-release-source-freeze.sh"))
	if err != nil {
		t.Fatal(err)
	}
	declarations := regexp.MustCompile(`(?m)^non_release_modules=\(([^)]*)\)$`).FindAllStringSubmatch(string(raw), -1)
	if len(declarations) != 1 {
		t.Fatalf("source freeze declares non_release_modules %d times, want 1", len(declarations))
	}
	if declared := strings.Fields(declarations[0][1]); !slices.Equal(declared, releaseExcludedModules) {
		t.Fatalf("builder excludes %q, source freeze excludes %q", releaseExcludedModules, declared)
	}
}

// A captured graph or a linked binary that contains an excluded module, by its
// declared path, its effective path or its directory, is refused; the server's
// own forks and every other module pass.
func TestReleaseBuildRefusesExcludedModules(t *testing.T) {
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tool := excludedModule{Directory: filepath.Join(workspace, "server", "tools", "competition-qualification-index"), Path: "competitionqualificationindex"}
	excluded := []excludedModule{tool}
	clean := []buildModule{
		{Path: "github.com/urnetwork/server", Directory: filepath.Join(workspace, "server")},
		{Path: "github.com/jackc/pgx/v5", EffectivePath: "../server/third_party/pgx", Directory: filepath.Join(workspace, "server", "third_party", "pgx")},
		{Path: "github.com/urnetwork/quic-go", EffectivePath: "../quic-go", Directory: filepath.Join(workspace, "quic-go")},
	}
	if err := validateReleaseExcludedModules("the server module graph", clean, excluded); err != nil {
		t.Fatalf("graph without an excluded module was refused: %v", err)
	}
	for _, module := range []buildModule{
		{Path: tool.Path},
		{Path: "example.invalid/other", EffectivePath: tool.Path},
		{Path: "example.invalid/other", Directory: tool.Directory},
		{Path: "example.invalid/other", Directory: filepath.Join(tool.Directory, "nested")},
	} {
		if err := validateReleaseExcludedModules("the server module graph", append(slices.Clone(clean), module), excluded); err == nil || !strings.Contains(err.Error(), "the server module graph contains the excluded module "+tool.Path) {
			t.Errorf("graph with excluded %+v was admitted: %v", module, err)
		}
	}
	info := &debug.BuildInfo{Deps: []*debug.Module{
		{Path: "github.com/jackc/pgx/v5", Version: "v5.7.0", Replace: &debug.Module{Path: "../server/third_party/pgx"}},
		{Path: "github.com/urnetwork/quic-go", Version: "v0.0.0", Replace: &debug.Module{Path: "../quic-go"}},
	}}
	if err := validateBinaryExcludedModules("server-api", info, clean, excluded); err != nil {
		t.Fatalf("binary without an excluded module was refused: %v", err)
	}
	for _, dependency := range []*debug.Module{
		{Path: tool.Path, Version: "v0.0.0"},
		{Path: "example.invalid/other", Version: "v1.0.0", Replace: &debug.Module{Path: tool.Path, Version: "v0.0.0"}},
	} {
		linked := &debug.BuildInfo{Deps: append(slices.Clone(info.Deps), dependency)}
		if err := validateBinaryExcludedModules("server-api", linked, clean, excluded); err == nil || !strings.Contains(err.Error(), "release binary server-api contains the excluded module "+tool.Path) {
			t.Errorf("binary linking %+v was admitted: %v", dependency, err)
		}
	}
	if err := validateBinaryExcludedModules("server-api", nil, clean, excluded); err == nil {
		t.Error("binary without build info was admitted")
	}
}

// An excluded module is identified by the path its own go.mod declares; one
// the workspace lacks is skipped, and an unreadable declaration is refused.
func TestReleaseBuildReadsExcludedModuleDeclarations(t *testing.T) {
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := buildConfig{Workspace: workspace}
	if identities, err := releaseExcludedModuleIdentities(config); err != nil || len(identities) != 0 {
		t.Fatalf("empty workspace identified %+v: %v", identities, err)
	}
	directory := filepath.Join(workspace, "server", "tools", "competition-qa-capacity-guard")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(directory, "go.mod")
	if err := os.WriteFile(manifest, []byte("// A standalone tool.\nmodule operator-capacity-guard.example/tests // its own module\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	identities, err := releaseExcludedModuleIdentities(config)
	if err != nil || !slices.Equal(identities, []excludedModule{{Directory: directory, Path: "operator-capacity-guard.example/tests"}}) {
		t.Fatalf("excluded module identities %+v: %v", identities, err)
	}
	if err := os.WriteFile(manifest, []byte("module \"example.invalid/quoted\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if identities, err := releaseExcludedModuleIdentities(config); err != nil || len(identities) != 1 || identities[0].Path != "example.invalid/quoted" {
		t.Fatalf("quoted module path identified %+v: %v", identities, err)
	}
	if err := os.WriteFile(manifest, []byte("go 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := releaseExcludedModuleIdentities(config); err == nil {
		t.Fatal("excluded module without a module path was admitted")
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(workspace, "elsewhere.mod"), manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := releaseExcludedModuleIdentities(config); err == nil {
		t.Fatal("symlinked excluded module declaration was admitted")
	}
}

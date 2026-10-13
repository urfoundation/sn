// Capture the compiler's actual module graph and physical source closure.
// A declared repository or source-lock label cannot substitute for resolution.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
)

// Both declared and effective identities survive replacements in the manifest.
type buildModule struct {
	Path              string   `json:"path"`
	Version           string   `json:"version"`
	EffectivePath     string   `json:"effective_path"`
	EffectiveVersion  string   `json:"effective_version"`
	Sum               string   `json:"sum"`
	GoModSum          string   `json:"go_mod_sum"`
	Directory         string   `json:"directory"`
	Repository        string   `json:"repository,omitempty"`
	Commit            string   `json:"commit,omitempty"`
	GoModSha256       string   `json:"go_mod_sha256"`
	ZipPath           string   `json:"zip_path,omitempty"`
	ZipSha256         string   `json:"zip_sha256,omitempty"`
	GraphOnly         bool     `json:"graph_only_unqualified"`
	UnqualifiedFields []string `json:"unqualified_fields"`
}

type moduleQualification struct {
	GraphNodes            int `json:"graph_nodes"`
	UnqualifiedGraphNodes int `json:"unqualified_graph_nodes"`
	MissingGoModMetadata  int `json:"missing_go_mod_metadata"`
	GoModOnly             int `json:"go_mod_metadata_without_source_body"`
}

// Lazy graph nodes need not have downloaded bodies or metadata. Retain their
// identity and missing fields, but never admit them as linked binary evidence.
func moduleMissingProvenance(module goModule) []string {
	missing := []string{}
	if module.GoMod == "" {
		missing = append(missing, "go_mod_file")
	}
	if module.Dir == "" {
		missing = append(missing, "source_directory")
	}
	if module.Version != "" {
		if !strings.HasPrefix(module.GoModSum, "h1:") {
			missing = append(missing, "go_mod_sum")
		}
		if !strings.HasPrefix(module.Sum, "h1:") {
			missing = append(missing, "module_sum")
		}
	}
	return missing
}

func countBuildModuleQualification(modules []buildModule) moduleQualification {
	result := moduleQualification{GraphNodes: len(modules)}
	for _, module := range modules {
		if !module.GraphOnly {
			continue
		}
		result.UnqualifiedGraphNodes++
		if module.GoModSha256 == "" || module.GoModSum == "" {
			result.MissingGoModMetadata++
		} else {
			result.GoModOnly++
		}
	}
	return result
}

// Build info identifies actual linked modules independently of the lazy graph.
// A local '(devel)' version is accepted only with exact pinned Git ownership.
func validateLinkedBuildModules(info *debug.BuildInfo, modules []buildModule) error {
	if info == nil {
		return errors.New("readable binary build info required")
	}
	byPath := map[string]buildModule{}
	for _, module := range modules {
		if _, exists := byPath[module.Path]; exists {
			return errors.New("duplicate captured module identity")
		}
		byPath[module.Path] = module
	}
	seen := map[string]bool{}
	for _, dependency := range info.Deps {
		if dependency == nil || seen[dependency.Path] {
			return errors.New("nil or duplicate binary module")
		}
		seen[dependency.Path] = true
		module, ok := byPath[dependency.Path]
		if !ok || module.GraphOnly || module.GoModSha256 == "" || module.Directory == "" {
			return fmt.Errorf("linked module %s lacks authenticated materialized provenance", dependency.Path)
		}
		effective := dependency
		if dependency.Replace != nil {
			effective = dependency.Replace
		}
		version := effective.Version
		if module.EffectiveVersion == "" {
			if module.Repository == "" || module.Commit == "" {
				return errors.New("linked local module has no pinned repository")
			}
			if version == "(devel)" {
				version = ""
			}
		} else if !strings.HasPrefix(module.Sum, "h1:") || !strings.HasPrefix(module.GoModSum, "h1:") || effective.Sum != module.Sum {
			return fmt.Errorf("linked remote module %s lacks matching body/go.mod sums", dependency.Path)
		}
		if dependency.Version != module.Version || effective.Path != module.EffectivePath || version != module.EffectiveVersion {
			return fmt.Errorf("linked module %s identity differs from resolved graph", dependency.Path)
		}
	}
	return nil
}

// Go's JSON stream is decoded as objects, never flattened into shell text.
type goModule struct {
	Path     string
	Version  string
	Sum      string
	GoModSum string
	Dir      string
	GoMod    string
	Main     bool
	Replace  *goModule
}

// Known API provenance closes both the SDK and adjacent transport/fork mismatch.
func validateReleaseModule(module goModule) error {
	pins := map[string][4]string{
		"github.com/urnetwork/sdk/v2026":     {"github.com/urnetwork/sdk/v2026", "v0.0.0-20261001021058-5d37be3876e5", "h1:PYuzGCWhMRuCnZS9qoSeMvwSZk9NQzX861Ir5xFFpHA=", "h1:IuPYnLZ5j1SP+gm3O4a4SxbOT4U/CPKEqONI1iLO5yk="},
		"github.com/urnetwork/connect/v2026": {"github.com/urnetwork/connect/v2026", "v0.0.0-20261001021459-e1b5d77b5029", "h1:jcnGC6MmKNn2x6HU6RfYHM24V2//K6myMw9ajM0MQsg=", "h1:A88Ceqd8zbfpUB4+1pTsT1G3kiS9kgB22ZuljBZVw4k="},
		"github.com/pion/sctp":         {"github.com/urnetwork/connect/v2026/sctp", "v0.0.0-20261001021459-e1b5d77b5029", "h1:GfovpOIWKd6TjkYsWLkMNE/zKEoGNC/5NfrPRCI+50A=", "h1:7KFmTwLcoYgJs/Z+99nJvsWL0qDpuyloSI0RbAqlrz0="},
	}
	if expected, ok := pins[module.Path]; ok {
		if module.Replace == nil || module.Replace.Path != expected[0] || module.Replace.Version != expected[1] || module.Replace.Sum != expected[2] || module.Replace.GoModSum != expected[3] {
			return fmt.Errorf("%s must resolve to its checksum-bound reviewed release API", module.Path)
		}
	}
	return nil
}

// Both main modules must compile the corrected transport tracked inside SN.
// A stock remote module or a different local checkout cannot stand in for it.
func validateReleaseRpcFork(config buildConfig, module buildModule) error {
	if module.Path != "github.com/centrifuge/go-substrate-rpc-client/v4" {
		return nil
	}
	for _, repo := range config.Repositories {
		if repo.Name == "sn" && repo.Commit != "" && module.Repository == repo.Name && module.Commit == repo.Commit &&
			module.Directory == filepath.Join(config.Workspace, repo.Path, "third_party/go-substrate-rpc-client") &&
			module.EffectiveVersion == "" && !module.GraphOnly && module.GoModSha256 != "" {
			return nil
		}
	}
	return errors.New("substrate RPC module must resolve to the reviewed fork in the pinned SN repository")
}

// All roots must be exact physical Git directories with unchanged tracked state.
func verifyBuildSources(ctx context.Context, config buildConfig) error {
	physical, err := filepath.EvalSymlinks(config.Workspace)
	if err != nil || physical != config.Workspace {
		return errors.New("release workspace must be a physical directory")
	}
	for _, repo := range config.Repositories {
		path := filepath.Join(config.Workspace, repo.Path)
		physical, err := filepath.EvalSymlinks(path)
		if err != nil || physical != path {
			return fmt.Errorf("%s source is not a physical checkout", repo.Name)
		}
		queries := []struct {
			args     []string
			expected string
		}{
			{args: []string{"rev-parse", "--show-toplevel"}, expected: path},
			{args: []string{"rev-parse", "HEAD"}, expected: repo.Commit},
			{args: []string{"rev-parse", "HEAD^{tree}"}, expected: repo.Tree},
			{args: []string{"status", "--porcelain=v1", "--untracked-files=all"}, expected: ""},
		}
		for _, query := range queries {
			raw, err := buildQuery(ctx, path, buildEnvironment(config.Output), config.Git.Path, query.args...)
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(raw)) != query.expected {
				return fmt.Errorf("%s source identity/cleanliness differs for %v", repo.Name, query.args)
			}
		}
	}
	return nil
}

// Local module files must be tracked under one explicitly pinned repository.
func buildModuleRepository(ctx context.Context, config buildConfig, path string) (repositoryPin, error) {
	physical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return repositoryPin{}, err
	}
	for _, repo := range config.Repositories {
		root := filepath.Join(config.Workspace, repo.Path)
		relative, err := filepath.Rel(root, physical)
		if err != nil || relative != "." && !filepath.IsLocal(relative) {
			continue
		}
		module := filepath.Join(relative, "go.mod")
		if _, err := buildQuery(ctx, root, buildEnvironment(config.Output), config.Git.Path, "ls-files", "--error-unmatch", "--", module); err != nil {
			return repositoryPin{}, fmt.Errorf("local module go.mod is not tracked: %w", err)
		}
		return repo, nil
	}
	return repositoryPin{}, fmt.Errorf("local module escapes the pinned source closure: %s", path)
}

// Each main module gets its own compiler resolution, including effective sums.
func captureBuildModules(ctx context.Context, config buildConfig, repository string) ([]buildModule, error) {
	dir := filepath.Join(config.Workspace, repository)
	moduleCache, err := buildQuery(ctx, dir, buildEnvironment(config.Output), config.Go.Path, "env", "GOMODCACHE")
	if err != nil {
		return nil, err
	}
	raw, err := buildQuery(ctx, dir, buildEnvironment(config.Output), config.Go.Path, "list", "-mod=readonly", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	result := []buildModule{}
	seen := map[string]bool{}
	for len(result) < 4096 {
		var module goModule
		err := decoder.Decode(&module)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if module.Path == "" || seen[module.Path] {
			return nil, errors.New("duplicate or missing Go module identity")
		}
		seen[module.Path] = true
		if err := validateReleaseModule(module); err != nil {
			return nil, err
		}
		effective := module
		if module.Replace != nil {
			effective = *module.Replace
		}
		entry := buildModule{Path: module.Path, Version: module.Version, EffectivePath: effective.Path, EffectiveVersion: effective.Version, Sum: effective.Sum, GoModSum: effective.GoModSum, Directory: module.Dir}
		if entry.Directory == "" {
			entry.Directory = effective.Dir
		}
		entry.UnqualifiedFields = moduleMissingProvenance(effective)
		entry.GraphOnly = len(entry.UnqualifiedFields) != 0
		if effective.GoMod != "" {
			mod, err := buildFile(effective.GoMod)
			if err != nil {
				return nil, err
			}
			entry.GoModSha256 = mod.Sha256
		}
		if effective.Version == "" {
			if entry.GraphOnly {
				return nil, fmt.Errorf("local module %s lacks source metadata", module.Path)
			}
			repo, err := buildModuleRepository(ctx, config, entry.Directory)
			if err != nil {
				return nil, err
			}
			entry.Repository, entry.Commit = repo.Name, repo.Commit
		}
		if err := validateReleaseRpcFork(config, entry); err != nil {
			return nil, err
		}
		if module.Path == "github.com/urnetwork/sdk/v2026" || module.Path == "github.com/urnetwork/connect/v2026" || module.Path == "github.com/pion/sctp" {
			if entry.GraphOnly {
				return nil, fmt.Errorf("required API module %s lacks materialized provenance", module.Path)
			}
			entry.ZipPath = filepath.Join(strings.TrimSpace(string(moduleCache)), "cache/download", effective.Path, "@v", effective.Version+".zip")
			zip, err := buildFile(entry.ZipPath)
			if err != nil {
				return nil, err
			}
			entry.ZipSha256 = zip.Sha256
		}
		result = append(result, entry)
	}
	if len(result) == 4096 {
		return nil, errors.New("module census exceeds 4096")
	}
	for _, path := range []string{"github.com/urnetwork/sdk/v2026", "github.com/urnetwork/connect/v2026", "github.com/pion/sctp", "github.com/centrifuge/go-substrate-rpc-client/v4"} {
		if !seen[path] {
			return nil, fmt.Errorf("required release module %s is absent", path)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

// Read input config/module/migration files again at publication, not cached hashes.
func retainBuildInput(config buildConfig, id, kind, path string) (buildArtifact, error) {
	raw, err := readBuildInput(path, 64*1024*1024)
	if err != nil {
		return buildArtifact{}, err
	}
	target := filepath.Join(config.Output, "inputs", id)
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return buildArtifact{}, err
	}
	n, writeErr := file.Write(raw)
	if n != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return buildArtifact{}, err
	}
	artifact, err := buildFile(target)
	if err != nil {
		return buildArtifact{}, err
	}
	artifact.Id, artifact.Kind, artifact.Path = id, kind, filepath.ToSlash(filepath.Join("inputs", id))
	return artifact, nil
}

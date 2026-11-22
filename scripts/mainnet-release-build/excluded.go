package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
)

// Modules outside release qualification, as non_release_modules in
// scripts/check-release-source-freeze.sh declares them: tooling and test data
// that no release build may require or link. The source freeze inventories
// them without qualifying them, so the builder proves that no module graph it
// captures and no binary it builds contains one.
var releaseExcludedModules = []string{
	"connect/tools/flightgate-devices",
	"connect/vlessinterop",
	"sdk/cgo/build",
	"sdk/packaging",
	"server/connect/sim-latency/baseline",
	"server/connect/sim-latency/baseline/prelaunch-cold-r9-2026-10-25/principal-reproduction",
	"server/tools/competition-census-memory-projector",
	"server/tools/competition-qa-capacity-guard",
	"server/tools/competition-qualification-index",
	"server/tools/competition-worker-image-archive",
}

// An excluded module that the workspace holds: its directory and the module
// path its own go.mod declares.
type excludedModule struct {
	Directory string
	Path      string
}

// Each excluded module present in the workspace. Pinned mode has no connect or
// SDK checkout, so their excluded modules are absent there, and a module
// archive never contains a nested module.
func releaseExcludedModuleIdentities(config buildConfig) ([]excludedModule, error) {
	var result []excludedModule
	for _, relative := range releaseExcludedModules {
		directory := filepath.Join(config.Workspace, filepath.FromSlash(relative))
		manifest := filepath.Join(directory, "go.mod")
		if _, err := os.Lstat(manifest); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		raw, err := readBuildInput(manifest, 1024*1024)
		if err != nil {
			return nil, fmt.Errorf("excluded module %s: %w", relative, err)
		}
		path, err := goModModulePath(raw)
		if err != nil {
			return nil, fmt.Errorf("excluded module %s: %w", relative, err)
		}
		result = append(result, excludedModule{Directory: directory, Path: path})
	}
	return result, nil
}

// The module path that a go.mod declares.
func goModModulePath(raw []byte) (string, error) {
	for _, line := range strings.Split(string(raw), "\n") {
		if comment := strings.Index(line, "//"); comment >= 0 {
			line = line[:comment]
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "module" {
			continue
		}
		path := fields[1]
		if strings.HasPrefix(path, `"`) || strings.HasPrefix(path, "`") {
			unquoted, err := strconv.Unquote(path)
			if err != nil {
				return "", err
			}
			path = unquoted
		}
		if path != "" {
			return path, nil
		}
	}
	return "", errors.New("go.mod declares no module path")
}

// No module of a captured graph or of a binary's links is excluded: neither
// its declared nor its effective path is an excluded module's path, and its
// directory is not an excluded module's directory or inside one.
func validateReleaseExcludedModules(scope string, modules []buildModule, excluded []excludedModule) error {
	for _, module := range modules {
		for _, item := range excluded {
			inside := false
			if module.Directory != "" {
				relative, err := filepath.Rel(item.Directory, module.Directory)
				inside = err == nil && filepath.IsLocal(relative)
			}
			if inside || module.Path == item.Path || module.EffectivePath == item.Path {
				return fmt.Errorf("%s contains the excluded module %s", scope, item.Path)
			}
		}
	}
	return nil
}

// A release binary links no excluded module, by the module the graph resolved
// each dependency to or by the path its replacement names.
func validateBinaryExcludedModules(role string, info *debug.BuildInfo, modules []buildModule, excluded []excludedModule) error {
	if info == nil {
		return errors.New("readable binary build info required")
	}
	captured := map[string]buildModule{}
	for _, module := range modules {
		captured[module.Path] = module
	}
	linked := []buildModule{}
	for _, dependency := range info.Deps {
		if dependency == nil {
			continue
		}
		module, ok := captured[dependency.Path]
		if !ok {
			module = buildModule{Path: dependency.Path}
		}
		linked = append(linked, module)
		if dependency.Replace != nil {
			linked = append(linked, buildModule{Path: dependency.Replace.Path})
		}
	}
	return validateReleaseExcludedModules("release binary "+role, linked, excluded)
}

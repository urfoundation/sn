// A fixed, local build sequence composes the current production selection.
package main

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"
)

// Validate build metadata without starting the resulting application.
func validateBuildInfo(info *debug.BuildInfo, role buildRole, repo repositoryPin) error {
	if info == nil {
		return errors.New("readable binary build info required")
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		if _, ok := settings[setting.Key]; ok {
			return errors.New("duplicate binary build setting")
		}
		settings[setting.Key] = setting.Value
	}
	wanted := map[string]string{"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0", "-trimpath": "true", "vcs": "git", "vcs.revision": repo.Commit, "vcs.modified": "false"}
	for key, value := range wanted {
		if settings[key] != value {
			return fmt.Errorf("%s binary build setting %s differs: %q", role.Id, key, settings[key])
		}
	}
	module := "github.com/urfoundation/sn/v2026"
	if role.Repository == "server" {
		module = "github.com/urnetwork/server/v2026"
	}
	if info.Main.Path != module || info.Path != module+strings.TrimPrefix(role.Package, ".") {
		return errors.New("binary package/main module differs")
	}
	return nil
}

// No overwrite, implicit repository discovery, compiler download or network
// application is available. Failure retains logs and never emits a sealed result.
func executeBuild(ctx context.Context, config buildConfig, rawConfig []byte) error {
	if err := config.validate(); err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(config.Output))
	if err != nil || parent != filepath.Dir(config.Output) {
		return errors.New("output parent must be a physical existing directory")
	}
	tools := []toolPin{config.Go, config.Forge, config.Solc, config.Git}
	for _, tool := range tools {
		if err := verifyBuildTool(tool); err != nil {
			return err
		}
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	gitPath, err = filepath.EvalSymlinks(gitPath)
	if err != nil || gitPath != config.Git.Path {
		return errors.New("Go VCS helper must resolve to the pinned Git executable")
	}
	if err := verifyBuildSources(ctx, config); err != nil {
		return err
	}
	if err := os.Mkdir(config.Output, 0700); err != nil {
		return fmt.Errorf("fresh output required: %w", err)
	}
	for _, directory := range []string{"logs", "inputs", "binaries", "contexts", "images", "cache/go", "tmp"} {
		if err := os.MkdirAll(filepath.Join(config.Output, directory), 0700); err != nil {
			return err
		}
	}
	configHash := sha256.Sum256(rawConfig)
	manifest := buildManifest{Schema: buildSchema, ConfigSha256: "sha256:" + hex.EncodeToString(configHash[:]), CandidateId: config.CandidateId, Platform: "linux/amd64", Version: config.Version, SourceDateEpoch: config.SourceDateEpoch, Repositories: config.Repositories, Roles: releaseRoles(), Modules: map[string][]buildModule{}, Artifacts: []buildArtifact{}, Contracts: []buildContract{}, Images: []buildImage{}, MissingImages: []string{}, Limitations: []string{"This composition does not grant release or deployment approval.", "Prepared image contexts have no OCI digest or independent rootfs/embedded-binary readback.", "A second independent build, full compiler installation attestation and arm64 qualification remain open.", "The historical contract catalogue is preserved separately from explicit fresh selection; exact equality applies only to selected bytes.", "Any externally held signed mainnet commitment must be checked before selecting a catalogue for the first mainnet plan.", "Runtime secrets, deployed configuration, live chain state, signing devices and deployment target compatibility are not qualified."}}
	manifest.ModuleQualification = map[string]moduleQualification{}
	manifest.ContractCatalog = config.ContractCatalog
	if manifest.ContractCatalog == "" {
		manifest.ContractCatalog = "retained"
	}
	if err := writeBuildBytes(filepath.Join(config.Output, "inputs/config.json"), rawConfig); err != nil {
		return err
	}
	repositories := map[string]repositoryPin{}
	for _, repo := range config.Repositories {
		repositories[repo.Name] = repo
	}
	for _, tool := range []struct {
		name string
		pin  toolPin
		args []string
	}{{name: "go", pin: config.Go, args: []string{"version"}}, {name: "forge", pin: config.Forge, args: []string{"--version"}}, {name: "solc", pin: config.Solc, args: []string{"--version"}}, {name: "git", pin: config.Git, args: []string{"--version"}}} {
		if err := buildCommand(ctx, config, "tool-"+tool.name, config.Workspace, tool.pin.Path, tool.args...); err != nil {
			return err
		}
	}
	for _, name := range []string{"sn", "server"} {
		modules, err := captureBuildModules(ctx, config, name)
		if err != nil {
			return err
		}
		manifest.Modules[name] = modules
		manifest.ModuleQualification[name] = countBuildModuleQualification(modules)
		if err := writeBuildJson(filepath.Join(config.Output, "inputs", name+"-effective-modules.json"), modules); err != nil {
			return err
		}
		if err := buildCommand(ctx, config, "verify-modules-"+name, filepath.Join(config.Workspace, name), config.Go.Path, "mod", "verify"); err != nil {
			return err
		}
	}
	// These three API-bearing replacements must retain immutable downloaded bytes,
	// not merely a version label; both main modules already require the same sums.
	for _, module := range manifest.Modules["sn"] {
		if module.ZipPath != "" {
			name := "module-" + strings.ReplaceAll(module.Path, "/", "-") + ".zip"
			artifact, err := copyBuildFile(module.ZipPath, filepath.Join(config.Output, "inputs", name), 0600)
			if err != nil {
				return err
			}
			if artifact.Sha256 != module.ZipSha256 {
				return errors.New("module archive changed during retention")
			}
			artifact.Id, artifact.Kind, artifact.Path = name, "module-archive", filepath.ToSlash(filepath.Join("inputs", name))
			manifest.Artifacts = append(manifest.Artifacts, artifact)
		}
	}
	// Retain source inputs independently of commands and executable build info.
	inputs := map[string]string{"foundry.toml": "sn/evm/foundry.toml", "remappings.txt": "sn/evm/remappings.txt", "contracts_gen.go": "sn/sim-testnet/contracts_gen.go", "gencontracts.go": "sn/sim-testnet/gencontracts/main.go", "runtime-packages.lock.json": "server/cli/runtime-packages.lock.json"}
	for _, repo := range config.Repositories {
		for _, name := range []string{"go.mod", "go.sum"} {
			source := filepath.Join(repo.Path, name)
			if _, err := os.Lstat(filepath.Join(config.Workspace, source)); err == nil {
				inputs[repo.Name+"-"+name] = source
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	for _, pattern := range []string{"server/db_migration*.go", "server/monitor/signal_migrations*.go"} {
		matches, err := filepath.Glob(filepath.Join(config.Workspace, pattern))
		if err != nil {
			return err
		}
		for _, path := range matches {
			if !strings.HasSuffix(path, "_test.go") {
				relative, err := filepath.Rel(config.Workspace, path)
				if err != nil {
					return err
				}
				inputs[strings.ReplaceAll(filepath.ToSlash(relative), "/", "-")] = relative
			}
		}
	}
	for _, role := range manifest.Roles {
		if role.Image {
			name := strings.TrimPrefix(role.Id, "server-")
			inputs["image-"+name+"-Makefile"] = "server/cli/" + name + "/Makefile"
		}
	}
	inputNames := []string{}
	for name := range inputs {
		inputNames = append(inputNames, name)
	}
	sort.Strings(inputNames)
	for _, name := range inputNames {
		artifact, err := retainBuildInput(config, name, "source-input", filepath.Join(config.Workspace, inputs[name]))
		if err != nil {
			return err
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	binaries := map[string]buildArtifact{}
	for _, role := range manifest.Roles {
		path := filepath.Join(config.Output, "binaries", role.Id)
		flags := "-s -w"
		if role.Repository == "sn" && role.Id != "sn-mainnet" {
			flags += " -X main.Version=" + config.Version
		}
		if err := buildCommand(ctx, config, "build-"+role.Id, filepath.Join(config.Workspace, role.Repository), config.Go.Path, "build", "-mod=readonly", "-trimpath", "-buildvcs=true", "-ldflags="+flags, "-o", path, role.Package); err != nil {
			return err
		}
		info, err := buildinfo.ReadFile(path)
		if err != nil {
			return err
		}
		if err := validateBuildInfo(info, role, repositories[role.Repository]); err != nil {
			return err
		}
		if err := validateLinkedBuildModules(info, manifest.Modules[role.Repository]); err != nil {
			return err
		}
		if err := writeBuildJson(filepath.Join(config.Output, "inputs", role.Id+"-buildinfo.json"), info); err != nil {
			return err
		}
		artifact, err := buildFile(path)
		if err != nil {
			return err
		}
		artifact.Id, artifact.Kind, artifact.Path = role.Id, "binary", filepath.ToSlash(filepath.Join("binaries", role.Id))
		binaries[role.Id] = artifact
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	// Compile production sources plus their probe/drill generator dependencies.
	evm := filepath.Join(config.Workspace, "sn/evm")
	if err := buildCommand(ctx, config, "build-contracts", filepath.Join(config.Workspace, "sn"), config.Forge.Path, "build", "--offline", "--force", "--skip", "test", "--skip", "script", "--use", config.Solc.Path, "--root", evm, "--build-info"); err != nil {
		return err
	}
	export := filepath.Join(config.Output, "inputs/contracts-release.json")
	if err := buildCommand(ctx, config, "check-contracts", filepath.Join(config.Workspace, "sn"), config.Go.Path, "run", "-mod=readonly", "./sim-testnet/gencontracts", "--check", "evm/out", "sim-testnet/contracts_gen.go"); err != nil {
		return err
	}
	selectedBinding := "sim-testnet/contracts_gen.go"
	if manifest.ContractCatalog == "fresh" {
		// The original catalogue remains evidence. A new private file selects exact
		// current compiler bytes for a separately reviewed unsigned first plan.
		historical := filepath.Join(config.Output, "inputs/contracts-retained.json")
		if err := buildCommand(ctx, config, "export-retained-contracts", filepath.Join(config.Workspace, "sn"), config.Go.Path, "run", "-mod=readonly", "./sim-testnet/gencontracts", "--release-json", "evm/out", "sim-testnet/contracts_gen.go", historical); err != nil {
			return err
		}
		selectedBinding = filepath.Join(config.Output, "inputs/contracts-fresh-binding.go")
		if err := buildCommand(ctx, config, "generate-fresh-contracts", filepath.Join(config.Workspace, "sn"), config.Go.Path, "run", "-mod=readonly", "./sim-testnet/gencontracts", "evm/out", selectedBinding); err != nil {
			return err
		}
	}
	if err := buildCommand(ctx, config, "export-contracts", filepath.Join(config.Workspace, "sn"), config.Go.Path, "run", "-mod=readonly", "./sim-testnet/gencontracts", "--release-json", "evm/out", selectedBinding, export); err != nil {
		return err
	}
	contracts, contractArtifacts, err := captureBuildContracts(config, export)
	if err != nil {
		return err
	}
	manifest.Contracts = contracts
	manifest.Artifacts = append(manifest.Artifacts, contractArtifacts...)
	manifest.SourceToBytecodeExact = true
	for _, contract := range contracts {
		manifest.SourceToBytecodeExact = manifest.SourceToBytecodeExact && contract.ExactBytes
	}
	buildInfos, err := filepath.Glob(filepath.Join(evm, "out/build-info/*.json"))
	if err != nil || len(buildInfos) == 0 {
		return errors.New("compiler build info missing")
	}
	for _, path := range buildInfos {
		artifact, err := retainBuildInput(config, "solc-"+filepath.Base(path), "compiler-input", path)
		if err != nil {
			return err
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	images, imageArtifacts, err := prepareBuildImages(config, binaries)
	if err != nil {
		return err
	}
	manifest.Images = images
	manifest.Artifacts = append(manifest.Artifacts, imageArtifacts...)
	for _, image := range images {
		manifest.MissingImages = append(manifest.MissingImages, image.Id)
	}
	if err := writeBuildJson(filepath.Join(config.Output, "inputs/image-build-commands.json"), images); err != nil {
		return err
	}
	// Final source, graph, tool and file reads close mutation during compilation.
	for _, name := range []string{"sn", "server"} {
		modules, err := captureBuildModules(ctx, config, name)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(manifest.Modules[name], modules) {
			return errors.New("effective module graph changed during build")
		}
		if err := buildCommand(ctx, config, "verify-final-modules-"+name, filepath.Join(config.Workspace, name), config.Go.Path, "mod", "verify"); err != nil {
			return err
		}
	}
	if err := verifyBuildSources(ctx, config); err != nil {
		return err
	}
	for _, tool := range tools {
		if err := verifyBuildTool(tool); err != nil {
			return err
		}
	}
	for _, artifact := range manifest.Artifacts {
		actual, err := buildFile(filepath.Join(config.Output, artifact.Path))
		if err != nil {
			return err
		}
		if artifact.Sha256 != actual.Sha256 || artifact.Bytes != actual.Bytes {
			return errors.New("retained build artifact changed")
		}
	}
	seenPaths := map[string]bool{}
	for _, artifact := range manifest.Artifacts {
		seenPaths[artifact.Path] = true
	}
	for _, directory := range []string{"inputs", "logs"} {
		entries, err := os.ReadDir(filepath.Join(config.Output, directory))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			relative := filepath.ToSlash(filepath.Join(directory, entry.Name()))
			if seenPaths[relative] {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() == 0 {
				continue
			}
			artifact, err := buildFile(filepath.Join(config.Output, relative))
			if err != nil {
				return err
			}
			artifact.Id, artifact.Kind, artifact.Path = directory+"-"+entry.Name(), "build-evidence", relative
			manifest.Artifacts = append(manifest.Artifacts, artifact)
		}
	}
	if err := sealBuildManifest(&manifest); err != nil {
		return err
	}
	return writeBuildJson(filepath.Join(config.Output, "manifest.json"), manifest)
}

// Release builds compile an explicit role set twice from a clean physical source
// graph. The retained local comparison is an unsigned candidate, not approval.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const releaseBuildConfigSchema = "urnetwork-mainnet-release-build-config-v1"
const releaseBuildSchema = "urnetwork-mainnet-release-build-v1"
const releaseBuildProfile = "linux-amd64-static-v1"

// The output must be a new external directory. There are no caller-supplied
// shell fragments, package paths, compiler flags or executable inputs.
type releaseBuildConfig struct {
	Schema      string            `json:"schema"`
	CandidateId string            `json:"candidate_id"`
	SourceSnDir string            `json:"source_sn_dir"`
	SourceLock  planFileReference `json:"source_lock"`
	OutputDir   string            `json:"output_dir"`
	Profile     string            `json:"profile"`
	Roles       []string          `json:"roles"`
}

// A role identifies a real entry package; taskworker and recovery tooling have
// separate identities even though both belong to the server repository.
type releaseBuildRole struct {
	Id           string `json:"id"`
	Purpose      string `json:"purpose"`
	Module       string `json:"module"`
	Package      string `json:"package"`
	GoExperiment string `json:"go_experiment"`
	Version      string `json:"version,omitempty"`
	Ldflags      string `json:"ldflags,omitempty"`
}

// Both output identities are retained so the successful comparison is directly
// reviewable. Inputs contain compiler-resolved modules and actual package files.
type releaseBuiltRole struct {
	Role      releaseBuildRole  `json:"role"`
	Inputs    planFileReference `json:"inputs"`
	First     releaseArtifact   `json:"first"`
	Second    releaseArtifact   `json:"second"`
	BuildInfo releaseBuildInfo  `json:"build_info"`
	BuildArgs []string          `json:"build_arguments_before_output_and_package"`
}

// Equality refers to these two executions on this builder with a shared cache.
// Other release categories and independent reproducibility remain unqualified.
type releaseBuildManifest struct {
	Schema                     string                `json:"schema"`
	CandidateId                string                `json:"candidate_id"`
	Status                     string                `json:"status"`
	Profile                    string                `json:"profile"`
	ConfigSha256               string                `json:"config_sha256"`
	SourceLock                 planFileReference     `json:"source_lock"`
	SourceLockContentHash      string                `json:"source_lock_content_hash"`
	SourceSnDir                string                `json:"source_sn_dir"`
	Inventory                  planFileReference     `json:"inventory"`
	Toolchain                  releaseBuildToolchain `json:"toolchain"`
	Roles                      []releaseBuiltRole    `json:"roles"`
	SameBuilderByteEquality    bool                  `json:"same_builder_byte_equality"`
	IndependentRebuildVerified bool                  `json:"independent_rebuild_verified"`
	ReleaseComplete            bool                  `json:"release_complete"`
	DeploymentApproved         bool                  `json:"deployment_approved"`
	Limitations                []string              `json:"limitations"`
	ContentHash                string                `json:"content_hash"`
}

// Selectors are closed and deterministic. Version stamps identify the exact SN
// source head, while server binaries retain their existing unversioned recipe.
func releaseBuildRoles(lock sourceLock) []releaseBuildRole {
	commit := ""
	for _, repository := range lock.Repositories {
		if repository.Path == "." {
			commit = repository.Commit
		}
	}
	version := "0.0.0-mainnet-candidate." + commit
	return []releaseBuildRole{
		{Id: "sn-mainnet", Purpose: "mainnet-operator-tooling", Module: "github.com/urfoundation/sn", Package: "./mainnet", Ldflags: "-w -s"},
		{Id: "sn-miner", Purpose: "miner-service", Module: "github.com/urfoundation/sn", Package: "./cli/miner", GoExperiment: "greenteagc", Version: version, Ldflags: "-w -s -X main.Version=" + version},
		{Id: "sn-validator", Purpose: "validator-service", Module: "github.com/urfoundation/sn", Package: "./cli/validator", Version: version, Ldflags: "-w -s -X main.Version=" + version},
		{Id: "server-api", Purpose: "api-service", Module: "github.com/urnetwork/server", Package: "./cli/api", GoExperiment: "greenteagc"},
		{Id: "server-taskworker", Purpose: "production-taskworker-service", Module: "github.com/urnetwork/server", Package: "./cli/taskworker", GoExperiment: "greenteagc"},
		{Id: "server-strecovery", Purpose: "offline-recovery-tooling", Module: "github.com/urnetwork/server", Package: "./cli/strecovery", GoExperiment: "greenteagc"},
	}
}

// Admission is complete before creating output. Physical source roots prevent
// one apparent sibling graph from resolving to unrelated live repositories.
func (self releaseBuildConfig) validate(lock sourceLock) ([]releaseBuildRole, error) {
	if self.Schema != releaseBuildConfigSchema || !planLabel(self.CandidateId) || self.Profile != releaseBuildProfile ||
		len(self.Roles) == 0 || len(self.Roles) > 6 {
		return nil, errors.New("release build requires its schema, candidate ID, linux-amd64-static-v1 profile and one to six roles")
	}
	for _, path := range []string{self.SourceSnDir, self.OutputDir} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "$\x00") {
			return nil, errors.New("release build requires clean absolute literal source and output paths")
		}
	}
	physical, err := filepath.EvalSymlinks(self.SourceSnDir)
	if err != nil || physical != self.SourceSnDir {
		return nil, errors.Join(errors.New("release build source must be physical, without symlink aliases"), err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(self.OutputDir))
	if err != nil || parent != filepath.Dir(self.OutputDir) {
		return nil, errors.Join(errors.New("release build output parent must exist and be physical"), err)
	}
	for _, repository := range lock.Repositories {
		root := filepath.Clean(filepath.Join(self.SourceSnDir, repository.Path))
		physical, err := filepath.EvalSymlinks(root)
		if err != nil || physical != root {
			return nil, errors.Join(errors.New("release build dependency repository must be physical"), err)
		}
		if releaseBuildWithin(root, self.OutputDir) {
			return nil, errors.New("release build output cannot be inside any locked source repository")
		}
	}
	availableKVs := map[string]releaseBuildRole{}
	for _, role := range releaseBuildRoles(lock) {
		availableKVs[role.Id] = role
	}
	selected := []releaseBuildRole{}
	seenKVs := map[string]bool{}
	for _, id := range self.Roles {
		role, ok := availableKVs[id]
		if !ok || seenKVs[id] {
			return nil, errors.New("release build role is unknown or repeated")
		}
		seenKVs[id] = true
		selected = append(selected, role)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Id < selected[j].Id })
	return selected, nil
}

// Only event observers are injectable. Tests mutate real files at an exact
// boundary; source admission, compilation and byte comparison stay real.
type releaseBuildHooks struct {
	afterBuild func(pass int, role releaseBuildRole, path string)
	beforeSeal func()
}

// Writes once into the exclusively created output directory. Failed attempts
// remain available for diagnosis but never receive a final manifest.
func releaseBuildWrite(path string, value any) (planFileReference, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return planFileReference{}, err
	}
	raw = append(raw, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return planFileReference{}, err
	}
	_, writeErr := file.Write(raw)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	return planFileReference{Path: path, Sha256: releaseBuildDigest(raw)}, err
}

// Canonical JSON seals and file references use the existing plan digest form.
func releaseBuildDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Absolute path containment is component-aware, including the directory itself.
func releaseBuildWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// The workflow never executes an artifact. Each compile has a deadline and a
// joined process group; source/input/toolchain fences surround both passes.
func buildReleaseCandidate(ctx context.Context, configPath string, hooks releaseBuildHooks) (releaseBuildManifest, error) {
	manifest := releaseBuildManifest{}
	raw, configHash, err := readPlanFile(ctx, configPath, maximumPlanManifestBytes)
	if err != nil {
		return manifest, err
	}
	var config releaseBuildConfig
	if err := decodePlanJson(raw, &config); err != nil {
		return manifest, err
	}
	lockRaw, err := readPlanReference(ctx, configPath, config.SourceLock, maximumPlanManifestBytes)
	if err != nil {
		return manifest, err
	}
	var lock sourceLock
	if err := decodePlanJson(lockRaw, &lock); err != nil {
		return manifest, err
	}
	if err := validatePlanSourceLock(lock); err != nil {
		return manifest, err
	}
	roles, err := config.validate(lock)
	if err != nil {
		return manifest, err
	}
	if err := verifyReleaseInventorySources(ctx, config.SourceSnDir, lock); err != nil {
		return manifest, err
	}
	if err := os.Mkdir(config.OutputDir, 0700); err != nil {
		return manifest, fmt.Errorf("release build output must be new: %w", err)
	}
	for _, name := range []string{"first", "second", "inputs", "tmp"} {
		if err := os.Mkdir(filepath.Join(config.OutputDir, name), 0700); err != nil {
			return manifest, err
		}
	}
	builder, err := newReleaseBuilder(ctx, config, lock)
	if err != nil {
		return manifest, err
	}
	lockReference, err := releaseBuildWrite(filepath.Join(config.OutputDir, "source-lock.json"), lock)
	if err != nil {
		return manifest, err
	}
	manifest = releaseBuildManifest{Schema: releaseBuildSchema, CandidateId: config.CandidateId, Status: "unapproved_build_candidate", Profile: config.Profile,
		ConfigSha256: configHash, SourceLock: lockReference, SourceLockContentHash: lock.ContentHash, SourceSnDir: config.SourceSnDir, Toolchain: builder.toolchain,
		Roles: []releaseBuiltRole{}, Limitations: []string{
			"Two serial builds use one builder, one toolchain and a shared cache; independent or cache-free reproducibility is not established.",
			"Only the selected Linux/amd64 static Go executables are built; other platforms, proxy roles, images, contracts, migrations, deployment configuration and policy remain uncovered.",
			"Taskworker bytes do not select or qualify the subnet-operator runtime workload, database schema, service credentials or deployed service identity.",
			"The recovery CLI is tooling, not a running operator service. No artifact is executed, no RPC is called and no signer or deployment is activated.",
			"Source, compiler input, toolchain and artifact checks are sequential local observations, not an atomic filesystem snapshot or signed build attestation.",
		}}
	verifiedModuleKVs := map[string]bool{}
	for _, role := range roles {
		if !verifiedModuleKVs[role.Module] {
			if err := builder.verifyModuleCache(ctx, role); err != nil {
				return releaseBuildManifest{}, err
			}
			verifiedModuleKVs[role.Module] = true
		}
	}
	inputsKVs := map[string]releaseBuildInputs{}
	for pass := 0; pass < 2; pass++ {
		for index, role := range roles {
			inputs, err := builder.capture(ctx, role)
			if err != nil {
				return releaseBuildManifest{}, err
			}
			if pass == 1 && !reflect.DeepEqual(inputsKVs[role.Id], inputs) {
				return releaseBuildManifest{}, errors.New("release build compiler inputs changed between passes")
			}
			name := "first"
			if pass == 1 {
				name = "second"
			}
			path := filepath.Join(config.OutputDir, name, role.Id)
			args := builder.arguments(role)
			if _, err := builder.command(ctx, role, 15*time.Minute, append(args, "-o", path, role.Package)...); err != nil {
				return releaseBuildManifest{}, fmt.Errorf("build %s pass %d: %w", role.Id, pass+1, err)
			}
			if hooks.afterBuild != nil {
				hooks.afterBuild(pass, role, path)
			}
			current, err := builder.capture(ctx, role)
			if err != nil || !reflect.DeepEqual(inputs, current) {
				return releaseBuildManifest{}, errors.Join(errors.New("release build compiler inputs changed during compilation"), err)
			}
			artifact, info, err := builder.artifact(ctx, role, path, inputs)
			if err != nil {
				return releaseBuildManifest{}, err
			}
			if pass == 0 {
				inputsKVs[role.Id] = inputs
				reference, err := releaseBuildWrite(filepath.Join(config.OutputDir, "inputs", role.Id+".json"), inputs)
				if err != nil {
					return releaseBuildManifest{}, err
				}
				manifest.Roles = append(manifest.Roles, releaseBuiltRole{Role: role, Inputs: reference, First: artifact, BuildInfo: info, BuildArgs: args})
			} else {
				first := manifest.Roles[index]
				if first.First.Sha256 != artifact.Sha256 || first.First.Bytes != artifact.Bytes || !reflect.DeepEqual(first.BuildInfo, info) {
					return releaseBuildManifest{}, fmt.Errorf("release build repeated bytes differ for %s", role.Id)
				}
				manifest.Roles[index].Second = artifact
			}
		}
	}
	if hooks.beforeSeal != nil {
		hooks.beforeSeal()
	}
	for _, result := range manifest.Roles {
		inputs, err := builder.capture(ctx, result.Role)
		if err != nil || !reflect.DeepEqual(inputsKVs[result.Role.Id], inputs) {
			return releaseBuildManifest{}, errors.Join(errors.New("release build final compiler input fence changed"), err)
		}
		for _, artifact := range []releaseArtifact{result.First, result.Second} {
			if err := releaseBuildVerifyArtifact(ctx, artifact); err != nil {
				return releaseBuildManifest{}, fmt.Errorf("release build final artifact fence: %w", err)
			}
		}
		if _, err := readPlanReference(ctx, configPath, result.Inputs, 32*1024*1024); err != nil {
			return releaseBuildManifest{}, err
		}
	}
	if err := builder.verifyToolchain(ctx); err != nil {
		return releaseBuildManifest{}, err
	}
	currentConfig, _, err := readPlanFile(ctx, configPath, maximumPlanManifestBytes)
	if err != nil || releaseBuildDigest(currentConfig) != configHash {
		return releaseBuildManifest{}, errors.Join(errors.New("release build configuration changed"), err)
	}
	if _, err := readPlanReference(ctx, configPath, config.SourceLock, maximumPlanManifestBytes); err != nil {
		return releaseBuildManifest{}, err
	}
	for _, role := range roles {
		if verifiedModuleKVs[role.Module] {
			if err := builder.verifyModuleCache(ctx, role); err != nil {
				return releaseBuildManifest{}, err
			}
			verifiedModuleKVs[role.Module] = false
		}
	}
	manifest.Inventory, err = builder.writeInventory(ctx, manifest)
	if err != nil {
		return releaseBuildManifest{}, err
	}
	manifest.SameBuilderByteEquality = true
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return releaseBuildManifest{}, err
	}
	manifest.ContentHash = releaseBuildDigest(append([]byte(releaseBuildSchema+"\x00"), encoded...))
	if _, err := releaseBuildWrite(filepath.Join(config.OutputDir, "build-manifest.json"), manifest); err != nil {
		return releaseBuildManifest{}, err
	}
	return manifest, nil
}

// The public command has a bounded total duration and prints a manifest only
// after all local fences and the compatible inventory have succeeded.
func runReleaseBuildCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("release-build", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := flags.String("config", "", "strict local release build configuration")
	timeout := flags.Duration("timeout", 90*time.Minute, "total offline build deadline, at most three hours")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *config == "" || *timeout <= 0 || *timeout > 3*time.Hour {
		fmt.Fprintln(stderr, "release-build requires --config and a positive --timeout of at most three hours")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	manifest, err := buildReleaseCandidate(ctx, *config, releaseBuildHooks{})
	if err != nil {
		fmt.Fprintln(stderr, "release-build:", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(manifest); err != nil {
		fmt.Fprintln(stderr, "release-build output:", err)
		return 1
	}
	return 0
}

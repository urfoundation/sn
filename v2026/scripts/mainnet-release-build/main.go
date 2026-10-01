// Build a complete offline Linux/amd64 candidate from explicit clean sources.
// This command never runs an application, pushes an image or grants approval.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

const buildSchema = "urnetwork-mainnet-release-build-v1"
const maximumBuildFileBytes int64 = 4 * 1024 * 1024 * 1024

// Absolute tool paths and independent hashes identify what will execute.
type toolPin struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
}

// Repository paths are relative to the isolated physical workspace.
type repositoryPin struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
}

// No environment interpolation, arbitrary build command or mutable source ref.
type buildConfig struct {
	Schema          string          `json:"schema"`
	CandidateId     string          `json:"candidate_id"`
	Workspace       string          `json:"workspace"`
	Output          string          `json:"output"`
	Version         string          `json:"version"`
	SourceDateEpoch int64           `json:"source_date_epoch"`
	ContractCatalog string          `json:"contract_catalog,omitempty"`
	Go              toolPin         `json:"go"`
	Forge           toolPin         `json:"forge"`
	Solc            toolPin         `json:"solc"`
	Git             toolPin         `json:"git"`
	Repositories    []repositoryPin `json:"repositories"`
}

// Capabilities sharing a binary remain explicit; no category-presence inference.
type buildRole struct {
	Id           string   `json:"id"`
	Repository   string   `json:"repository"`
	Package      string   `json:"package"`
	Capabilities []string `json:"capabilities"`
	Image        bool     `json:"image"`
}

// All currently selected source commands survive composition, including monitors
// and maintenance commands omitted by the historical seven-binary inventory.
func releaseRoles() []buildRole {
	roles := []buildRole{
		{Id: "sn-mainnet", Repository: "sn", Package: "./mainnet", Capabilities: []string{"mainnet-control", "root-service", "root-monitor", "operator-monitor", "owner-signing", "bootstrap-chain", "bootstrap-contracts", "activate-validators", "repair-validator"}},
		{Id: "sn-miner", Repository: "sn", Package: "./cli/miner", Capabilities: []string{"miner"}},
		{Id: "sn-validator", Repository: "sn", Package: "./cli/validator", Capabilities: []string{"ur-validator"}},
		{Id: "sn-snclaim", Repository: "sn", Package: "./cli/snclaim", Capabilities: []string{"claim-recovery"}},
	}
	for _, name := range []string{"api", "taskworker", "proxy", "connect", "alt", "gossip", "mcp", "competitionworker", "monitor", "strecovery", "competitiondbinit", "competitionpatch", "geolite2export"} {
		roles = append(roles, buildRole{Id: "server-" + name, Repository: "server", Package: "./cli/" + name, Capabilities: []string{name}, Image: name != "monitor" && name != "strecovery" && name != "competitiondbinit" && name != "competitionpatch" && name != "geolite2export"})
	}
	return roles
}

// Exact files identify output bytes, while graph and commands identify inputs.
type buildArtifact struct {
	Id     string `json:"id"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// Approval remains separate even when every selected output is built.
type buildManifest struct {
	Schema                  string                         `json:"schema"`
	ConfigSha256            string                         `json:"config_sha256"`
	CandidateId             string                         `json:"candidate_id"`
	Platform                string                         `json:"platform"`
	Version                 string                         `json:"version"`
	SourceDateEpoch         int64                          `json:"source_date_epoch"`
	ContractCatalog         string                         `json:"contract_catalog"`
	Repositories            []repositoryPin                `json:"repositories"`
	Roles                   []buildRole                    `json:"roles"`
	Modules                 map[string][]buildModule       `json:"effective_modules"`
	ModuleQualification     map[string]moduleQualification `json:"module_qualification"`
	Artifacts               []buildArtifact                `json:"artifacts"`
	Contracts               []buildContract                `json:"contracts"`
	Images                  []buildImage                   `json:"images"`
	MissingImages           []string                       `json:"missing_images"`
	SourceToBytecodeExact   bool                           `json:"source_to_bytecode_exact"`
	SourceToImageVerified   bool                           `json:"source_to_image_verified"`
	ReproducibilityVerified bool                           `json:"reproducibility_verified"`
	ReleaseComplete         bool                           `json:"release_complete"`
	DeploymentApproved      bool                           `json:"deployment_approved"`
	Limitations             []string                       `json:"limitations"`
	ContentHash             string                         `json:"content_hash"`
}

// One bounded buffer prevents a compiler error stream consuming all disk.
type buildLog struct {
	file      *os.File
	remaining int64
}

func (self *buildLog) Write(raw []byte) (int, error) {
	if int64(len(raw)) > self.remaining {
		return 0, errors.New("build command output exceeds 16 MiB")
	}
	n, err := self.file.Write(raw)
	self.remaining -= int64(n)
	return n, err
}

// Metadata is bounded while the child writes, before allocating its full output.
type buildBuffer struct {
	bytes.Buffer
	limit int
}

func (self *buildBuffer) Write(raw []byte) (int, error) {
	if len(raw) > self.limit-self.Len() {
		return 0, errors.New("build metadata exceeds limit")
	}
	return self.Buffer.Write(raw)
}

// The fixed environment prevents shell, workspace or compiler-option injection.
func buildEnvironment(output string) []string {
	blocked := map[string]bool{}
	for _, name := range []string{"GOWORK", "GOFLAGS", "GOENV", "GOTOOLCHAIN", "GOROOT", "GOPROXY", "GOSUMDB", "GOEXPERIMENT", "CGO_ENABLED", "GOOS", "GOARCH", "GOCACHE", "GOTMPDIR", "TMPDIR", "GOAMD64", "GOARM64", "GO386", "GOFIPS140", "GODEBUG", "LD_PRELOAD", "LD_LIBRARY_PATH", "BASH_ENV", "ENV", "FOUNDRY_PROFILE", "FOUNDRY_CONFIG", "DAPP_SOLC", "SOLC_VERSION"} {
		blocked[name] = true
	}
	result := []string{}
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if !blocked[name] && !strings.HasPrefix(name, "FOUNDRY_") && !strings.HasPrefix(name, "DAPP_") {
			result = append(result, value)
		}
	}
	return append(result, "GOWORK=off", "GOFLAGS=", "GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOEXPERIMENT=greenteagc", "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOAMD64=v1", "GOCACHE="+filepath.Join(output, "cache/go"), "TMPDIR="+filepath.Join(output, "tmp"))
}

// Logs and command records are retained before execution; no command is a shell.
func buildCommand(ctx context.Context, config buildConfig, id, dir, tool string, args ...string) error {
	return runBuildCommand(ctx, config.Output, id, dir, tool, buildEnvironment(config.Output), args...)
}

// Each executor supplies its own closed environment and shares bounded receipts.
func runBuildCommand(ctx context.Context, output, id, dir, tool string, environment []string, args ...string) error {
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	logPath := filepath.Join(output, "logs", id+".log")
	file, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	record := struct {
		Directory string   `json:"directory"`
		Tool      string   `json:"tool"`
		Arguments []string `json:"arguments"`
	}{Directory: dir, Tool: tool, Arguments: args}
	if err := writeBuildJson(filepath.Join(output, "logs", id+".command.json"), record); err != nil {
		return err
	}
	command := exec.CommandContext(commandCtx, tool, args...)
	command.Dir = dir
	command.Env = environment
	command.WaitDelay = 2 * time.Second
	writer := &buildLog{file: file, remaining: 16 * 1024 * 1024}
	command.Stdout = writer
	command.Stderr = writer
	err = command.Run()
	code := 0
	if err != nil {
		code = 1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
	}
	if saveErr := os.WriteFile(filepath.Join(output, "logs", id+".exit"), []byte(fmt.Sprintln(code)), 0600); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	if err != nil {
		return fmt.Errorf("%s failed; retain %s: %w", id, logPath, errors.Join(err, commandCtx.Err()))
	}
	return nil
}

// Small metadata commands have the same finite output and time limits.
func buildQuery(ctx context.Context, dir string, env []string, tool string, args ...string) ([]byte, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(queryCtx, tool, args...)
	command.Dir = dir
	command.Env = env
	command.WaitDelay = 2 * time.Second
	output := &buildBuffer{limit: 16 * 1024 * 1024}
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("%s %v: %w: %.4096s", tool, args, errors.Join(err, queryCtx.Err()), output.Bytes())
	}
	return output.Bytes(), nil
}

// Hash a regular file without loading executable/image-sized content in memory.
func buildFile(path string) (buildArtifact, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return buildArtifact{}, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximumBuildFileBytes {
		return buildArtifact{}, errors.New("build artifact must be a nonempty bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return buildArtifact{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return buildArtifact{}, errors.New("build file identity changed")
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, maximumBuildFileBytes+1))
	if err != nil {
		return buildArtifact{}, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || n != before.Size() || after.Size() != before.Size() || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		return buildArtifact{}, errors.New("build artifact changed while hashing")
	}
	return buildArtifact{Sha256: "sha256:" + hex.EncodeToString(digest.Sum(nil)), Bytes: n}, nil
}

// Publication uses exclusive files. A failed build cannot overwrite a candidate.
func writeBuildJson(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeBuildBytes(path, raw)
}

func writeBuildBytes(path string, raw []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(raw)
	if n != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Sync(), file.Close())
}

// Every selected tool is rechecked before and after the complete build.
func verifyBuildTool(pin toolPin) error {
	if !filepath.IsAbs(pin.Path) || filepath.Clean(pin.Path) != pin.Path {
		return errors.New("tool path must be canonical absolute")
	}
	file, err := buildFile(pin.Path)
	if err != nil {
		return err
	}
	if file.Sha256 != pin.Sha256 {
		return fmt.Errorf("tool pin differs: %s", pin.Path)
	}
	info, err := os.Stat(pin.Path)
	if err != nil || info.Mode().Perm()&0111 == 0 {
		return errors.New("build tool is not executable")
	}
	return nil
}

// Fixed role/output scope avoids accidentally narrowing to a convenient subset.
func (self buildConfig) validate() error {
	if self.ContractCatalog != "" && self.ContractCatalog != "retained" && self.ContractCatalog != "fresh" {
		return errors.New("contract catalogue must be retained or fresh")
	}
	label := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$`)
	if self.Schema != buildSchema || !label.MatchString(self.CandidateId) || !label.MatchString(self.Version) || self.SourceDateEpoch <= 0 {
		return errors.New("release build schema, identity, version or source epoch differs")
	}
	for _, path := range []string{self.Workspace, self.Output} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("workspace/output paths must be canonical absolute")
		}
	}
	relative, err := filepath.Rel(self.Workspace, self.Output)
	if err != nil || relative == "." || filepath.IsLocal(relative) {
		return errors.New("build output must be outside the source workspace")
	}
	wanted := map[string]string{"sn": "sn", "server": "server", "proxy": "proxy", "glog": "glog", "goidenticons": "goidenticons", "userwireguard": "userwireguard", "warp": "warp", "forge-std": "sn/evm/lib/forge-std", "openzeppelin-contracts": "sn/evm/lib/openzeppelin-contracts", "openzeppelin-contracts-upgradeable": "sn/evm/lib/openzeppelin-contracts-upgradeable"}
	if len(self.Repositories) != len(wanted) {
		return errors.New("release requires the complete source and Solidity library closure")
	}
	commit := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for _, repo := range self.Repositories {
		path, ok := wanted[repo.Name]
		if !ok || repo.Path != path || !commit.MatchString(repo.Commit) || !commit.MatchString(repo.Tree) {
			return errors.New("duplicate, missing or unpinned release repository")
		}
		delete(wanted, repo.Name)
	}
	return nil
}

// No live-network, signing or deployment command is available at this boundary.
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	flags := flag.NewFlagSet("mainnet-release-build", flag.ContinueOnError)
	configPath := flags.String("config", "", "exact release build config")
	imageConfigPath := flags.String("image-config", "", "offline scratch-image supplement config")
	packageImageConfigPath := flags.String("package-image-config", "", "offline seven-service image supplement config")
	aggregateImageConfigPath := flags.String("aggregate-image-config", "", "offline verification and aggregation of pinned image receipts")
	if err := flags.Parse(os.Args[1:]); err != nil || flags.NArg() != 0 {
		os.Exit(2)
	}
	selected := 0
	for _, path := range []string{*configPath, *imageConfigPath, *packageImageConfigPath, *aggregateImageConfigPath} {
		if path != "" {
			selected++
		}
	}
	if selected != 1 {
		os.Exit(2)
	}
	if *aggregateImageConfigPath != "" {
		if err := executeImageAggregateConfig(ctx, *aggregateImageConfigPath); err != nil {
			fmt.Fprintln(os.Stderr, "release image aggregation:", err)
			os.Exit(1)
		}
		return
	}
	if *packageImageConfigPath != "" {
		if err := executePackageImageConfig(ctx, *packageImageConfigPath); err != nil {
			fmt.Fprintln(os.Stderr, "release package image build:", err)
			os.Exit(1)
		}
		return
	}
	if *imageConfigPath != "" {
		if err := executeImageConfig(ctx, *imageConfigPath); err != nil {
			fmt.Fprintln(os.Stderr, "release image build:", err)
			os.Exit(1)
		}
		return
	}
	raw, err := readBuildInput(*configPath, 1024*1024)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bounded build config required", err)
		os.Exit(2)
	}
	var config buildConfig
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	err = validateBuildJsonKeys(raw, true)
	if err == nil {
		err = decoder.Decode(&config)
	}
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = errors.New("trailing build config data")
		}
	}
	if err == nil {
		err = config.validate()
	}
	if err == nil {
		err = executeBuild(ctx, config, raw)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release build:", err)
		os.Exit(1)
	}
}

// Stable artifact ordering and a domain-separated seal exclude approval claims.
func sealBuildManifest(manifest *buildManifest) error {
	if manifest.ContractCatalog == "" {
		manifest.ContractCatalog = "retained"
	}
	if manifest.ReleaseComplete || manifest.DeploymentApproved || manifest.ReproducibilityVerified || manifest.SourceToImageVerified {
		return errors.New("composition cannot grant reproducibility, image, release or deployment approval")
	}
	sort.Slice(manifest.Artifacts, func(i, j int) bool { return manifest.Artifacts[i].Id < manifest.Artifacts[j].Id })
	if !reflect.DeepEqual(manifest.Roles, releaseRoles()) {
		return errors.New("release role census differs")
	}
	seen := map[string]buildArtifact{}
	paths := map[string]bool{}
	digest := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for _, artifact := range manifest.Artifacts {
		if _, ok := seen[artifact.Id]; ok || artifact.Id == "" || paths[artifact.Path] || !filepath.IsLocal(artifact.Path) || artifact.Path != filepath.ToSlash(filepath.Clean(artifact.Path)) || !digest.MatchString(artifact.Sha256) || artifact.Bytes <= 0 {
			return errors.New("duplicate or invalid release artifact identity")
		}
		seen[artifact.Id] = artifact
		paths[artifact.Path] = true
	}
	for _, role := range manifest.Roles {
		if artifact, ok := seen[role.Id]; !ok || artifact.Kind != "binary" {
			return fmt.Errorf("required role %s has no binary", role.Id)
		}
	}
	if err := validateContractCensus(manifest.Contracts, seen, manifest.SourceToBytecodeExact, manifest.ContractCatalog); err != nil {
		return err
	}
	if err := validateImageCensus(manifest.Images, manifest.MissingImages, seen); err != nil {
		return err
	}
	manifest.ContentHash = ""
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(append([]byte(buildSchema+"\x00"), raw...))
	manifest.ContentHash = "sha256:" + hex.EncodeToString(hash[:])
	return nil
}

// Configuration duplicates are ambiguous even if their final decoded value fits.
func validateBuildJson(raw []byte) error {
	return validateBuildJsonKeys(raw, false)
}

// Owned configuration structs also reject case aliases accepted by encoding/json.
// Contract metadata maps retain their case-sensitive key semantics.
func validateBuildJsonKeys(raw []byte, rejectCaseAliases bool) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 32 {
			return errors.New("build JSON nesting exceeds bound")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		if delimiter != '{' && delimiter != '[' {
			return errors.New("unexpected JSON delimiter")
		}
		keys := map[string]bool{}
		for decoder.More() {
			if delimiter == '{' {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if rejectCaseAliases {
					name = strings.ToLower(strings.ToUpper(name))
				}
				if !ok || keys[name] {
					return errors.New("duplicate JSON member")
				}
				keys[name] = true
			}
			if err := visit(depth + 1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if delimiter == '{' && end != json.Delim('}') || delimiter == '[' && end != json.Delim(']') {
			return errors.New("unbalanced build JSON")
		}
		return nil
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing build JSON")
	}
	return nil
}

// Bound reads before allocation and reject links at retained-file boundaries.
func readBuildInput(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("bounded regular input required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("input identity changed")
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) || int64(len(raw)) != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, errors.New("input changed while reading")
	}
	return raw, nil
}

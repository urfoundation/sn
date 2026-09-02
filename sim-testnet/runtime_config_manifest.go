package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const runtimeConfigManifestSchema = "urnetwork-sim-runtime-config-manifest-v1"

// Bind one immutable process input by state-relative path, content and mode.
type RuntimeConfigFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Mode   string `json:"mode"`
}

// Bind every immutable process input to one deployment/config/policy identity.
type RuntimeConfigManifest struct {
	Schema       string              `json:"schema"`
	DeploymentID string              `json:"deployment_id"`
	ConfigHash   string              `json:"config_hash"`
	PolicyHash   string              `json:"policy_hash"`
	Files        []RuntimeConfigFile `json:"files"`
	ManifestHash string              `json:"manifest_hash"`
}

// Return the evidence fields persisted in the config-render postcondition.
type runtimeConfigVerification struct {
	FileCount    int
	ManifestHash string
}

// Keep the manifest outside mutable per-process state directories.
func runtimeConfigManifestPath(stateDir string) string {
	return filepath.Join(stateDir, "runtime-config-manifest.json")
}

// Normalize a path into the deployment state directory and reject conflicting
// mode expectations for the same file.
func addRuntimeConfigPath(paths map[string]os.FileMode, stateDir, path string, mode os.FileMode) error {
	relative, err := filepath.Rel(stateDir, path)
	if err != nil {
		return err
	}
	relative = filepath.ToSlash(filepath.Clean(relative))
	if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || filepath.IsAbs(relative) {
		return fmt.Errorf("runtime config path %q escapes the state directory", path)
	}
	if existing, ok := paths[relative]; ok && existing != mode {
		return fmt.Errorf("runtime config path %s has conflicting modes", relative)
	}
	paths[relative] = mode
	return nil
}

// Enumerate every rendered immutable file read by the launched operator,
// miner, validator, and swarm processes. Mutable state, queues, evidence, and
// logs are deliberately excluded. The two operator config overlays are exact
// directory links into the separately release-locked platform-config checkout
// and are validated below rather than represented as regular files.
func expectedRuntimeConfigFiles(cfg *ResolvedConfig, stateDir string) (map[string]os.FileMode, error) {
	if cfg == nil || cfg.Config == nil || strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("runtime config manifest context is incomplete")
	}
	paths := map[string]os.FileMode{}
	vaultSource := filepath.Join(cfg.Repos.Vault, "local")
	vaultFiles := make([]string, 0)
	if err := filepath.WalkDir(vaultSource, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("vault runtime source %s is not a regular file", path)
		}
		relative, err := filepath.Rel(vaultSource, path)
		if err != nil {
			return err
		}
		vaultFiles = append(vaultFiles, relative)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("enumerate vault runtime sources: %w", err)
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		root := filepath.Join(stateDir, "runtime", "operator-"+strconv.Itoa(operator))
		for _, relative := range vaultFiles {
			if err := addRuntimeConfigPath(paths, stateDir, filepath.Join(root, "vault", relative), 0o600); err != nil {
				return nil, err
			}
		}
		for _, relative := range []string{"vault/pg_maintenance.yml", "vault/verify.yml", "vault/minio.yml", "site/settings.yml", "config/tls.yml"} {
			if err := addRuntimeConfigPath(paths, stateDir, filepath.Join(root, filepath.FromSlash(relative)), 0o600); err != nil {
				return nil, err
			}
		}
		host := operatorConnectHostIP(operator)
		for _, extension := range []string{".crt", ".key"} {
			if err := addRuntimeConfigPath(paths, stateDir, filepath.Join(root, "vault", "tls", host, host+extension), 0o600); err != nil {
				return nil, err
			}
		}
		if err := addRuntimeConfigPath(paths, stateDir, filepath.Join(stateDir, "secrets", fmt.Sprintf("operator-%d-claim-relayer.key", operator)), 0o600); err != nil {
			return nil, err
		}
	}
	if err := addRuntimeConfigPath(paths, stateDir, operatorConnectCAFile(stateDir), 0o644); err != nil {
		return nil, err
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		if err := addRuntimeConfigPath(paths, stateDir, filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validator), "validator.yml"), 0o600); err != nil {
			return nil, err
		}
		if err := addRuntimeConfigPath(paths, stateDir, filepath.Join(stateDir, "secrets", fmt.Sprintf("validator-%d-hotkey.seed", validator)), 0o600); err != nil {
			return nil, err
		}
		for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
			path := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validator), "state", "operators", fmt.Sprintf("no-%d", operator), "client.key")
			if err := addRuntimeConfigPath(paths, stateDir, path, 0o600); err != nil {
				return nil, err
			}
		}
	}
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		root := filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", miner))
		for _, name := range []string{"miner.yml", "claim-daemon.yml"} {
			if err := addRuntimeConfigPath(paths, stateDir, filepath.Join(root, name), 0o600); err != nil {
				return nil, err
			}
		}
	}
	for swarm := 1; swarm <= cfg.Config.Topology.MinerSwarmProcesses; swarm++ {
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-swarm-%d", swarm), "swarm.json")
		if err := addRuntimeConfigPath(paths, stateDir, path, 0o600); err != nil {
			return nil, err
		}
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("claim-relayer-%d", operator), "swarm.json")
		if err := addRuntimeConfigPath(paths, stateDir, path, 0o600); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// Reject a substituted state root or parent directory before reading a static
// file. Checking only the final component would still follow a symlinked miner
// or validator directory outside the deployment boundary.
func validateRuntimeConfigPathAncestry(stateDir, relative string) error {
	root := filepath.Clean(stateDir)
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return stateMismatchError(err, "runtime config state root is not a real directory")
	}
	directory := filepath.Dir(filepath.FromSlash(relative))
	if directory == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(directory, string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("runtime config %s has unsafe ancestry", relative)
		}
		current = filepath.Join(current, component)
		info, err = os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return stateMismatchError(err, "runtime config %s parent %s is not a real directory", relative, current)
		}
	}
	return nil
}

// Read only a regular file without following a final-component symlink.
func runtimeConfigFileDigest(path string) (string, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("runtime config %s is not a regular file", path)
	}
	wire, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	digest := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(digest[:]), info.Mode().Perm(), nil
}

// Hash the canonical manifest with its self-authenticating field cleared.
func runtimeConfigManifestHash(manifest RuntimeConfigManifest) (string, error) {
	manifest.ManifestHash = ""
	return canonicalHashHex(manifest)
}

// Observe the complete expected static input set after atomic rendering.
func buildRuntimeConfigManifest(cfg *ResolvedConfig, stateDir string) (*RuntimeConfigManifest, error) {
	expected, err := expectedRuntimeConfigFiles(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(expected))
	for path := range expected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	manifest := &RuntimeConfigManifest{
		Schema: runtimeConfigManifestSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash,
	}
	for _, relative := range paths {
		if err := validateRuntimeConfigPathAncestry(stateDir, relative); err != nil {
			return nil, err
		}
		digest, mode, err := runtimeConfigFileDigest(filepath.Join(stateDir, filepath.FromSlash(relative)))
		if err != nil {
			return nil, err
		}
		if mode != expected[relative] {
			return nil, fmt.Errorf("runtime config %s mode is %04o, want %04o", relative, mode, expected[relative])
		}
		manifest.Files = append(manifest.Files, RuntimeConfigFile{Path: relative, SHA256: digest, Mode: fmt.Sprintf("%04o", mode)})
	}
	manifest.ManifestHash, err = runtimeConfigManifestHash(*manifest)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

// Persist one private manifest atomically beside the runtime tree.
func writeRuntimeConfigManifest(cfg *ResolvedConfig, stateDir string) error {
	manifest, err := buildRuntimeConfigManifest(cfg, stateDir)
	if err != nil {
		return err
	}
	wire, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(runtimeConfigManifestPath(stateDir), append(wire, '\n'), 0o600)
}

// Recognize only the two directory links deliberately installed by the
// operator overlay renderer. Their targets are release-locked repository paths
// and exactDirectorySymlink rejects a regular-file or foreign-target
// substitution. No other link is exempt from the static inventory.
func approvedRuntimeConfigOverlay(cfg *ResolvedConfig, stateDir, path string) (bool, error) {
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		home := operatorConfigHome(stateDir, operator)
		for _, overlay := range []struct{ name, target string }{
			{operatorEnvironment(operator), filepath.Join(cfg.Repos.PlatformConfig, "local")},
			{"all", filepath.Join(cfg.Repos.PlatformConfig, "all")},
		} {
			link := filepath.Join(home, overlay.name)
			if filepath.Clean(path) != filepath.Clean(link) {
				continue
			}
			relative, err := filepath.Rel(stateDir, link)
			if err != nil {
				return true, err
			}
			if err := validateRuntimeConfigPathAncestry(stateDir, filepath.ToSlash(relative)); err != nil {
				return true, err
			}
			return true, exactDirectorySymlink(link, overlay.target)
		}
	}
	return false, nil
}

// Reject additional files in directories which contain only static inputs,
// while accepting the exact independently validated operator overlays.
func validateRuntimeConfigStaticTrees(cfg *ResolvedConfig, stateDir string, expected map[string]os.FileMode) error {
	roots := make([]string, 0, cfg.Config.Topology.Operators*3+cfg.Config.Topology.MinerSwarmProcesses+cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		root := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator))
		roots = append(roots, filepath.Join(root, "vault"), filepath.Join(root, "config"), filepath.Join(root, "site"))
	}
	for swarm := 1; swarm <= cfg.Config.Topology.MinerSwarmProcesses; swarm++ {
		roots = append(roots, filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-swarm-%d", swarm)))
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		roots = append(roots, filepath.Join(stateDir, "runtime", fmt.Sprintf("claim-relayer-%d", operator)))
	}
	for _, root := range roots {
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if approved, overlayErr := approvedRuntimeConfigOverlay(cfg, stateDir, path); approved {
				return overlayErr
			}
			relative, err := filepath.Rel(stateDir, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if _, ok := expected[relative]; !ok {
				return fmt.Errorf("unexpected static runtime config %s", relative)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// Authenticate the manifest identity and complete expected inventory before a
// caller selects either every static input or a security-critical subset.
func authenticatedRuntimeConfigManifest(cfg *ResolvedConfig, stateDir string) (*RuntimeConfigManifest, map[string]os.FileMode, error) {
	var manifest RuntimeConfigManifest
	path := runtimeConfigManifestPath(stateDir)
	if err := decodeStrictJSONFile(path, &manifest); err != nil {
		return nil, nil, fmt.Errorf("read runtime config manifest: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, nil, stateMismatchError(err, "runtime config manifest is absent or not private")
	}
	if manifest.Schema != runtimeConfigManifestSchema || manifest.DeploymentID != cfg.Config.Deployment.DeploymentID ||
		!strings.EqualFold(manifest.ConfigHash, cfg.ConfigHash) || !strings.EqualFold(manifest.PolicyHash, cfg.PolicyHash) {
		return nil, nil, errors.New("runtime config manifest identity does not match the active deployment")
	}
	if _, err := decodeHex32("runtime config manifest config hash", manifest.ConfigHash); err != nil {
		return nil, nil, err
	}
	if _, err := decodeHex32("runtime config manifest policy hash", manifest.PolicyHash); err != nil {
		return nil, nil, err
	}
	wantManifestHash, err := runtimeConfigManifestHash(manifest)
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(manifest.ManifestHash, wantManifestHash) {
		return nil, nil, errors.New("runtime config manifest hash is invalid")
	}
	expected, err := expectedRuntimeConfigFiles(cfg, stateDir)
	if err != nil {
		return nil, nil, err
	}
	if len(manifest.Files) != len(expected) {
		return nil, nil, fmt.Errorf("runtime config manifest has %d files, want %d", len(manifest.Files), len(expected))
	}
	previous := ""
	for _, file := range manifest.Files {
		if file.Path == "" || file.Path <= previous {
			return nil, nil, errors.New("runtime config manifest paths are not unique and sorted")
		}
		previous = file.Path
		mode, ok := expected[file.Path]
		if !ok || file.Mode != fmt.Sprintf("%04o", mode) {
			return nil, nil, fmt.Errorf("runtime config manifest entry %s is unexpected", file.Path)
		}
		if err := validateRuntimeConfigPathAncestry(stateDir, file.Path); err != nil {
			return nil, nil, err
		}
	}
	return &manifest, expected, nil
}

func verifyRuntimeConfigManifestFile(stateDir string, file RuntimeConfigFile, expectedMode os.FileMode) error {
	digest, observedMode, err := runtimeConfigFileDigest(filepath.Join(stateDir, filepath.FromSlash(file.Path)))
	if err != nil {
		return err
	}
	if observedMode != expectedMode || !strings.EqualFold(file.SHA256, digest) {
		return fmt.Errorf("runtime config %s differs from its manifest", file.Path)
	}
	return nil
}

// Reauthenticate only the rendered object-store inputs at the scenario's
// completion boundary. Other runtime inputs may have undergone an approved
// live transition, such as production verify-key rotation.
func verifyRuntimeBlobConfigManifest(cfg *ResolvedConfig, stateDir string) error {
	manifest, expected, err := authenticatedRuntimeConfigManifest(cfg, stateDir)
	if err != nil {
		return err
	}
	files := make(map[string]RuntimeConfigFile, len(manifest.Files))
	for _, file := range manifest.Files {
		files[file.Path] = file
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		relative := filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("operator-%d", operator), "vault", "minio.yml"))
		file, ok := files[relative]
		mode, expectedOK := expected[relative]
		if !ok || !expectedOK {
			return fmt.Errorf("runtime config manifest is missing %s", relative)
		}
		if err := verifyRuntimeConfigManifestFile(stateDir, file, mode); err != nil {
			return err
		}
	}
	return nil
}

// Reconstruct identity, inventory, modes and bytes before accepting config
// rendering as a durable setup postcondition.
func verifyRuntimeConfigManifest(cfg *ResolvedConfig, stateDir string) (runtimeConfigVerification, error) {
	manifest, expected, err := authenticatedRuntimeConfigManifest(cfg, stateDir)
	if err != nil {
		return runtimeConfigVerification{}, err
	}
	for _, file := range manifest.Files {
		mode := expected[file.Path]
		if err := verifyRuntimeConfigManifestFile(stateDir, file, mode); err != nil {
			return runtimeConfigVerification{}, err
		}
	}
	if err := validateRuntimeConfigStaticTrees(cfg, stateDir, expected); err != nil {
		return runtimeConfigVerification{}, err
	}
	return runtimeConfigVerification{FileCount: len(expected), ManifestHash: manifest.ManifestHash}, nil
}

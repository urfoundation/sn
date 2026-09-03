package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	releaseLayoutTypeASTID = regexp.MustCompile(`(t_(?:struct|contract|enum|userDefinedValueType)\([^)]*\))\d+`)
	releaseGitCommit       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	releaseSHA256          = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func normalizeReleaseStorageLayout(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if key == "astId" || key == "contract" {
				continue
			}
			out[releaseLayoutTypeASTID.ReplaceAllString(key, "$1")] = normalizeReleaseStorageLayout(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = normalizeReleaseStorageLayout(typed[i])
		}
		return out
	case string:
		return releaseLayoutTypeASTID.ReplaceAllString(typed, "$1")
	default:
		return value
	}
}

type releaseLockObservation struct {
	EVMBuild       map[string]string
	Repositories   map[string]string
	Interfaces     map[string]string
	Infrastructure map[string]string
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestNamedFiles(root string, names []string) (string, error) {
	paths := append([]string(nil), names...)
	sort.Strings(paths)
	h := sha256.New()
	var size [8]byte
	for _, name := range paths {
		clean := filepath.Clean(name)
		if filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("unsafe release-lock path %q", name)
		}
		data, err := os.ReadFile(filepath.Join(root, clean))
		if err != nil {
			return "", err
		}
		portable := filepath.ToSlash(clean)
		binary.BigEndian.PutUint64(size[:], uint64(len(portable)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(portable))
		binary.BigEndian.PutUint64(size[:], uint64(len(data)))
		_, _ = h.Write(size[:])
		_, _ = h.Write(data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// cleanGitSubtreeHash binds large, shared runtime assets without reading
// gigabytes into every doctor process. Git's clean-tree check proves that the
// index and worktree match HEAD (including absence of untracked resources),
// while ls-tree supplies the reviewed file modes, object IDs, and paths for a
// stable SHA-256 release-lock fingerprint.
func cleanGitSubtreeHash(root, subtree string) (string, error) {
	clean := filepath.Clean(subtree)
	if filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe git subtree %q", subtree)
	}
	status := exec.Command("git", "-C", root, "status", "--porcelain=v1", "--untracked-files=all", "--", clean)
	statusOutput, err := status.Output()
	if err != nil {
		return "", fmt.Errorf("inspect platform config subtree %s: %w", clean, err)
	}
	if len(bytes.TrimSpace(statusOutput)) != 0 {
		return "", fmt.Errorf("platform config subtree %s differs from reviewed HEAD: %s", clean, strings.TrimSpace(string(statusOutput)))
	}
	tree := exec.Command("git", "-C", root, "ls-tree", "-r", "--full-tree", "HEAD", "--", clean)
	treeOutput, err := tree.Output()
	if err != nil {
		return "", fmt.Errorf("read platform config subtree %s: %w", clean, err)
	}
	if len(bytes.TrimSpace(treeOutput)) == 0 {
		return "", fmt.Errorf("platform config subtree %s has no reviewed files", clean)
	}
	return digestBytes(treeOutput), nil
}

// The SDK's mobile release toolchain is a nested Go module under build/. The
// general Go source digest deliberately ignores build directories so generated
// artifacts cannot perturb a release lock, therefore bind this reviewed source
// tree separately. cleanGitSubtreeHash also rejects uncommitted or untracked
// files anywhere in the module before a release can proceed.
func sdkMobileBuildTreeHash(sdkRoot string) (string, error) {
	return cleanGitSubtreeHash(sdkRoot, "build")
}

func filesUnder(root string, roots []string, include func(string) bool) ([]string, error) {
	var names []string
	for _, relativeRoot := range roots {
		start := filepath.Join(root, relativeRoot)
		err := filepath.WalkDir(start, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "out", "lib", "build", "runs", "node_modules", "vendor":
					if path != start {
						return filepath.SkipDir
					}
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if include(filepath.ToSlash(rel)) {
				names = append(names, rel)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return names, nil
}

func goReleaseSourceHash(root string) (string, error) {
	names, err := filesUnder(root, []string{"."}, func(name string) bool {
		base := filepath.Base(name)
		return (strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")) || base == "go.mod" || base == "go.sum"
	})
	if err != nil {
		return "", err
	}
	return digestNamedFiles(root, names)
}

// Bind a Go module's source digest to one clean commit observed on both sides
// of the read. This closes the gap where a sibling checkout changes while a
// release-lock observation is walking its files.
func observeCleanGoReleaseSource(root string) (string, string, error) {
	revision, err := cleanGitRevision(root)
	if err != nil {
		return "", "", err
	}
	hash, err := goReleaseSourceHash(root)
	if err != nil {
		return "", "", err
	}
	confirmedRevision, err := cleanGitRevision(root)
	if err != nil {
		return "", "", err
	}
	if confirmedRevision != revision {
		return "", "", errors.New("dependency checkout changed during source observation")
	}
	return hash, revision, nil
}

// Name the two independently checked fields exactly as they appear in the
// release lock so observation and schema validation cannot drift apart.
func observeOperatorProxyReleaseSource(root string) (map[string]string, error) {
	hash, commit, err := observeCleanGoReleaseSource(root)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"operator_proxy_go_source_hash": hash,
		"operator_proxy_commit":         commit,
	}, nil
}

func soliditySourceHash(snRoot string) (string, error) {
	names, err := filesUnder(snRoot, []string{"evm/src", "evm/script"}, func(name string) bool {
		return strings.HasSuffix(name, ".sol")
	})
	if err != nil {
		return "", err
	}
	return digestNamedFiles(snRoot, names)
}

func cleanGitRevision(root string) (string, error) {
	status := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all")
	statusOutput, err := status.Output()
	if err != nil {
		return "", fmt.Errorf("inspect dependency checkout %s: %w", root, err)
	}
	if len(bytes.TrimSpace(statusOutput)) != 0 {
		return "", fmt.Errorf("dependency checkout %s has uncommitted or untracked files", root)
	}
	revision := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	revisionOutput, err := revision.Output()
	if err != nil {
		return "", fmt.Errorf("read dependency revision %s: %w", root, err)
	}
	value := strings.TrimSpace(string(revisionOutput))
	if !releaseGitCommit.MatchString(value) {
		return "", fmt.Errorf("dependency checkout %s returned an invalid revision", root)
	}
	return value, nil
}

func parseFoundryVersion(output []byte) (string, string, error) {
	var version string
	var commit string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "forge Version:"):
			if version != "" {
				return "", "", errors.New("forge version output contains duplicate version fields")
			}
			version = strings.TrimSpace(strings.TrimPrefix(line, "forge Version:"))
		case strings.HasPrefix(line, "Commit SHA:"):
			if commit != "" {
				return "", "", errors.New("forge version output contains duplicate commit fields")
			}
			commit = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "Commit SHA:")))
		}
	}
	if version == "" || !releaseGitCommit.MatchString(commit) {
		return "", "", errors.New("forge version output is incomplete or malformed")
	}
	return version, commit, nil
}

func observeFoundryVersion() (string, string, error) {
	executable, err := exec.LookPath("forge")
	if err != nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", "", fmt.Errorf("locate forge: %w", err)
		}
		executable = filepath.Join(home, ".foundry", "bin", "forge")
	}
	output, err := exec.Command(executable, "--version").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("read forge version: %w: %s", err, bytes.TrimSpace(output))
	}
	return parseFoundryVersion(output)
}

func protocolSourceHash(snRoot string) (string, error) {
	names, err := filesUnder(snRoot, []string{"protocol", "docs/spec"}, func(name string) bool {
		return !strings.HasSuffix(name, "_test.go")
	})
	if err != nil {
		return "", err
	}
	names = append(names, "WHITEPAPER.md", "VALIDATOR.md")
	return digestNamedFiles(snRoot, names)
}

// serverLocalDependencyConfigHash covers the server/local compose contract
// mirrored by sim-testnet and every PostgreSQL init hook mounted into the
// release containers. Enumerating the directory also makes newly added hooks
// fail the lock instead of executing as unreviewed container input.
func serverLocalDependencyConfigHash(serverRoot string) (string, error) {
	names, err := filesUnder(serverRoot, []string{"local/postgres/initdb"}, func(string) bool { return true })
	if err != nil {
		return "", err
	}
	names = append(names, "local/docker-compose.yml")
	return digestNamedFiles(serverRoot, names)
}

// releaseABI names one generated interface included in the aggregate ABI lock.
type releaseABI struct {
	name string
	abi  string
}

var generatedReleaseABIs = []releaseABI{
	{"Coordinator", CoordinatorABI},
	{"CoordinatorAdversary", CoordinatorAdversaryABI},
	{"ERC1967Proxy", ERC1967ProxyABI},
	{"FleetBatcher", FleetBatcherABI},
	{"ReserveSink", ReserveSinkABI},
	{"SettlementVault", SettlementVaultABI},
	{"SubnetProbe", SubnetProbeABI},
}

// digestReleaseABIs hashes the ordered contract names and exact generated ABIs.
func digestReleaseABIs(entries []releaseABI) string {
	h := sha256.New()
	var size [8]byte
	for _, entry := range entries {
		binary.BigEndian.PutUint64(size[:], uint64(len(entry.name)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(entry.name))
		binary.BigEndian.PutUint64(size[:], uint64(len(entry.abi)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(entry.abi))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// generatedABIHash returns the aggregate ABI hash for every deployed artifact.
func generatedABIHash() string {
	return digestReleaseABIs(generatedReleaseABIs)
}

// foundryStorageLayoutHash pins the upgradeable coordinator's complete
// compiler-emitted storage layout. Decoding and re-encoding through encoding/json
// makes the fingerprint independent of whitespace and object-key order while
// retaining slot, offset, type, label, and nested member information.
func foundryStorageLayoutHash(snRoot, artifact string) (string, error) {
	b, err := os.ReadFile(filepath.Join(snRoot, artifact))
	if err != nil {
		return "", err
	}
	var value struct {
		StorageLayout any `json:"storageLayout"`
	}
	if err := json.Unmarshal(b, &value); err != nil {
		return "", fmt.Errorf("decode Foundry artifact storage layout: %w", err)
	}
	if value.StorageLayout == nil {
		return "", errors.New("Foundry artifact has no storageLayout")
	}
	canonical, err := json.Marshal(normalizeReleaseStorageLayout(value.StorageLayout))
	if err != nil {
		return "", fmt.Errorf("canonicalize Foundry storage layout: %w", err)
	}
	return digestBytes(canonical), nil
}

func moduleRoot(parent, name string) (string, error) {
	root := filepath.Join(parent, name)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("release module %s: %w", name, err)
	}
	return root, nil
}

// Lock the rendered RPC behavior, not only the playbook that happens to install
// it. A template or included-task change can otherwise alter the endpoint while
// leaving the release lock green.
var subtensorGatewayReleaseFiles = []string{
	"main/ansible/playbook-subtensor.yml",
	"main/ansible/tasks/subtensor-gateway.yml",
	"main/ansible/host_files/snow/subtensor/nginx.conf.j2",
	"main/ansible/host_files/snow/subtensor/nginx-overlay.conf.j2",
}

// The node lock binds both side-by-side service definitions and the complete
// independent lightnode rollout/readiness path used before sim-testnet-light
// is allowed to run.
var subtensorNodeReleaseFiles = []string{
	"main/ansible/host_files/snow/subtensor/vars.yml",
	"main/ansible/host_files/snow/subtensor/docker-compose.yml.j2",
	"main/ansible/host_files/snow/subtensor/subtensor.service",
	"main/ansible/playbook-subtensor-lightnode.yml",
	"main/ansible/run-playbook.sh",
	"main/ansible/run-subtensor-lightnode.sh",
	"main/ansible/tasks/subtensor-lightnode-preflight.yml",
}

func observeReleaseLock(cfg *ResolvedConfig) (*releaseLockObservation, error) {
	if cfg == nil || cfg.Repos.SN == "" || cfg.Repos.Server == "" || cfg.Repos.OperatorProxy == "" {
		return nil, errors.New("release repository paths are incomplete")
	}
	observation := &releaseLockObservation{EVMBuild: map[string]string{}, Repositories: map[string]string{}, Interfaces: map[string]string{}, Infrastructure: map[string]string{}}
	var err error
	observation.EVMBuild["source_hash"], err = soliditySourceHash(cfg.Repos.SN)
	if err != nil {
		return nil, err
	}
	observation.EVMBuild["foundry"], observation.EVMBuild["foundry_commit"], err = observeFoundryVersion()
	if err != nil {
		return nil, err
	}
	for key, relative := range map[string]string{
		"forge_std_commit":                          "forge-std",
		"openzeppelin_contracts_commit":             "openzeppelin-contracts",
		"openzeppelin_contracts_upgradeable_commit": "openzeppelin-contracts-upgradeable",
	} {
		observation.EVMBuild[key], err = cleanGitRevision(filepath.Join(cfg.Repos.SN, "evm", "lib", relative))
		if err != nil {
			return nil, err
		}
	}
	foundry, err := os.ReadFile(filepath.Join(cfg.Repos.SN, "evm", "foundry.toml"))
	if err != nil {
		return nil, err
	}
	observation.EVMBuild["compiler_settings_hash"] = digestBytes(foundry)
	observation.EVMBuild["reserve_sink_runtime_hash"] = ReserveSinkRuntimeBytecodeHash
	observation.EVMBuild["settlement_vault_runtime_hash"] = SettlementVaultRuntimeBytecodeHash
	observation.EVMBuild["coordinator_implementation_runtime_hash"] = CoordinatorRuntimeBytecodeHash
	observation.EVMBuild["coordinator_proxy_runtime_hash"] = ERC1967ProxyRuntimeBytecodeHash
	observation.EVMBuild["governance_drill_implementation_runtime_hash"] = CoordinatorAdversaryRuntimeBytecodeHash
	observation.EVMBuild["precompile_probe_runtime_hash"] = SubnetProbeRuntimeBytecodeHash
	observation.EVMBuild["fleet_batcher_runtime_hash"] = FleetBatcherRuntimeBytecodeHash
	observation.EVMBuild["reserve_sink_artifact_hash"] = ReserveSinkFoundryArtifactHash
	observation.EVMBuild["settlement_vault_artifact_hash"] = SettlementVaultFoundryArtifactHash
	observation.EVMBuild["coordinator_implementation_artifact_hash"] = CoordinatorFoundryArtifactHash
	observation.EVMBuild["coordinator_proxy_artifact_hash"] = ERC1967ProxyFoundryArtifactHash
	observation.EVMBuild["governance_drill_implementation_artifact_hash"] = CoordinatorAdversaryFoundryArtifactHash
	observation.EVMBuild["precompile_probe_artifact_hash"] = SubnetProbeFoundryArtifactHash
	observation.EVMBuild["fleet_batcher_artifact_hash"] = FleetBatcherFoundryArtifactHash
	observation.EVMBuild["governance_drill_storage_layout_hash"] = CoordinatorAdversaryStorageLayoutHash
	observation.EVMBuild["fleet_batcher_storage_layout_hash"] = FleetBatcherStorageLayoutHash
	observation.EVMBuild["abi_hash"] = generatedABIHash()
	observation.EVMBuild["coordinator_storage_layout_hash"] = CoordinatorStorageLayoutHash

	parent := filepath.Dir(cfg.Repos.SN)
	modules := map[string]string{"sn": cfg.Repos.SN, "server": cfg.Repos.Server}
	for _, name := range []string{"connect", "sdk", "glog", "goidenticons", "proxy", "userwireguard"} {
		root, rootErr := moduleRoot(parent, name)
		if rootErr != nil {
			return nil, rootErr
		}
		modules[name] = root
	}
	for name, root := range modules {
		hash, hashErr := goReleaseSourceHash(root)
		if hashErr != nil {
			return nil, fmt.Errorf("hash %s module: %w", name, hashErr)
		}
		observation.Repositories[name+"_go_source_hash"] = hash
	}
	operatorProxyObservation, err := observeOperatorProxyReleaseSource(cfg.Repos.OperatorProxy)
	if err != nil {
		return nil, fmt.Errorf("observe operator-proxy module: %w", err)
	}
	for key, value := range operatorProxyObservation {
		observation.Repositories[key] = value
	}
	observation.Repositories["sdk_mobile_build_tree_hash"], err = sdkMobileBuildTreeHash(modules["sdk"])
	if err != nil {
		return nil, fmt.Errorf("hash SDK mobile build module: %w", err)
	}
	observation.Repositories["protocol_source_hash"], err = protocolSourceHash(cfg.Repos.SN)
	if err != nil {
		return nil, err
	}
	configNames, err := filesUnder(cfg.Repos.PlatformConfig, []string{"local"}, func(string) bool { return true })
	if err != nil {
		return nil, fmt.Errorf("enumerate platform config: %w", err)
	}
	observation.Repositories["platform_config_source_hash"], err = digestNamedFiles(cfg.Repos.PlatformConfig, configNames)
	if err != nil {
		return nil, fmt.Errorf("hash platform config: %w", err)
	}
	observation.Repositories["platform_config_shared_tree_hash"], err = cleanGitSubtreeHash(cfg.Repos.PlatformConfig, "all")
	if err != nil {
		return nil, fmt.Errorf("hash shared platform config: %w", err)
	}

	interfaces, err := filesUnder(cfg.Repos.SN, []string{"evm/src/interfaces"}, func(name string) bool { return strings.HasSuffix(name, ".sol") })
	if err != nil {
		return nil, err
	}
	observation.Interfaces["precompile_interfaces_source_hash"], err = digestNamedFiles(cfg.Repos.SN, interfaces)
	if err != nil {
		return nil, err
	}

	xops := filepath.Join(parent, "xops")
	observation.Infrastructure["gateway_config_hash"], err = digestNamedFiles(xops, subtensorGatewayReleaseFiles)
	if err != nil {
		return nil, fmt.Errorf("hash RPC gateway config: %w", err)
	}
	observation.Infrastructure["node_config_hash"], err = digestNamedFiles(xops, subtensorNodeReleaseFiles)
	if err != nil {
		return nil, fmt.Errorf("hash Subtensor node config: %w", err)
	}
	observation.Infrastructure["server_local_config_hash"], err = serverLocalDependencyConfigHash(cfg.Repos.Server)
	if err != nil {
		return nil, fmt.Errorf("hash server/local dependency config: %w", err)
	}
	return observation, nil
}

func lockString(section map[string]any, key string) (string, error) {
	value, ok := section[key]
	if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" {
		return "", fmt.Errorf("release lock field %s is missing", key)
	}
	return strings.TrimSpace(fmt.Sprint(value)), nil
}

func compareLockSection(name string, locked map[string]any, observed map[string]string, allowedAnnotations map[string]struct{}) error {
	for key, got := range observed {
		want, err := lockString(locked, key)
		if err != nil {
			return err
		}
		if !strings.EqualFold(want, got) {
			return fmt.Errorf("release lock %s.%s mismatch: locked=%s observed=%s", name, key, want, got)
		}
	}
	for key := range locked {
		if _, ok := observed[key]; ok {
			continue
		}
		if _, ok := allowedAnnotations[key]; !ok {
			return fmt.Errorf("release lock %s.%s is not an observed field or an allowed annotation", name, key)
		}
	}
	return nil
}

// Require the independently versioned operator-proxy module to carry both its
// exact clean commit and its production-source digest in the release schema.
func validateReleaseRepositorySchema(repositories map[string]any) error {
	sourceHash, err := lockString(repositories, "operator_proxy_go_source_hash")
	if err != nil {
		return err
	}
	if !releaseSHA256.MatchString(sourceHash) {
		return errors.New("release lock repositories.operator_proxy_go_source_hash is not a canonical SHA-256 digest")
	}
	commit, err := lockString(repositories, "operator_proxy_commit")
	if err != nil {
		return err
	}
	if !releaseGitCommit.MatchString(commit) {
		return errors.New("release lock repositories.operator_proxy_commit is not a canonical Git commit")
	}
	return nil
}

func validateReleaseLock(cfg *ResolvedConfig) error {
	if cfg.Release == nil || cfg.Release.SchemaVersion != 1 || cfg.Release.Release != "1.0" || cfg.Release.Runtime.SourceTag != "testnet" || cfg.Release.Runtime.SourceCommit != "da06f033663896ef2fdbbfc3ecc68ca908fba0f5" || cfg.Release.Runtime.SpecVersion != 452 || cfg.Release.Runtime.TransactionVersion != 1 || !strings.EqualFold(cfg.Release.Runtime.CodeHash, "0x40a8c3c99a47d6739b086236308535fab26d5fd4cc5c88eb83f6a3c8b928f7cc") {
		return errors.New("release lock runtime identity is not the reviewed testnet runtime 452 release")
	}
	if !strings.Contains(cfg.Release.Runtime.Image, "@sha256:") || strings.Contains(cfg.Release.Runtime.Image, "placeholder") {
		return fmt.Errorf("runtime image is not digest-pinned")
	}
	for name, image := range cfg.Release.Dependencies {
		if !strings.Contains(image, "@sha256:") || strings.Contains(image, "placeholder") {
			return fmt.Errorf("dependency %s is not digest-pinned", name)
		}
	}
	for _, section := range []map[string]any{cfg.Release.EVMBuild, cfg.Release.Repositories, cfg.Release.Interfaces, cfg.Release.Infrastructure} {
		for key, value := range section {
			if value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" || strings.Contains(strings.ToLower(fmt.Sprint(value)), "placeholder") {
				return fmt.Errorf("release lock field %s is unresolved", key)
			}
		}
	}
	if err := validateReleaseRepositorySchema(cfg.Release.Repositories); err != nil {
		return err
	}
	for key, want := range map[string]string{
		"solidity":                      "0.8.24",
		"evm_version":                   "cancun",
		"foundry":                       "1.7.1",
		"foundry_commit":                "4072e48705af9d93e3c0f6e29e93b5e9a40caed8",
		"optimizer":                     "true",
		"optimizer_runs":                "200",
		"forge_std_commit":              "bf647bd6046f2f7da30d0c2bf435e5c76a780c1b",
		"openzeppelin_contracts_commit": "5fd1781b1454fd1ef8e722282f86f9293cacf256",
		"openzeppelin_contracts_upgradeable_commit": "7bf4727aacdbfaa0f36cbd664654d0c9e1dc52bf",
	} {
		got, err := lockString(cfg.Release.EVMBuild, key)
		if err != nil || !strings.EqualFold(got, want) {
			return fmt.Errorf("release lock evm_build.%s=%q, want %q", key, got, want)
		}
	}
	observed, err := observeReleaseLock(cfg)
	if err != nil {
		return err
	}
	evmAnnotations := map[string]struct{}{
		"solidity": {}, "evm_version": {}, "optimizer": {}, "optimizer_runs": {},
	}
	repositoryAnnotations := map[string]struct{}{
		"sn_audited_base_commit": {}, "server_audited_base_commit": {}, "vault_audited_base_commit": {},
	}
	if err := compareLockSection("evm_build", cfg.Release.EVMBuild, observed.EVMBuild, evmAnnotations); err != nil {
		return err
	}
	if err := compareLockSection("repositories", cfg.Release.Repositories, observed.Repositories, repositoryAnnotations); err != nil {
		return err
	}
	if err := compareLockSection("interfaces", cfg.Release.Interfaces, observed.Interfaces, nil); err != nil {
		return err
	}
	if err := compareLockSection("infrastructure", cfg.Release.Infrastructure, observed.Infrastructure, nil); err != nil {
		return err
	}
	return nil
}

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var releaseLayoutTypeASTID = regexp.MustCompile(`(t_(?:struct|contract|enum|userDefinedValueType)\([^)]*\))\d+`)

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

func soliditySourceHash(snRoot string) (string, error) {
	names, err := filesUnder(snRoot, []string{"evm/src"}, func(name string) bool { return strings.HasSuffix(name, ".sol") })
	if err != nil {
		return "", err
	}
	return digestNamedFiles(snRoot, names)
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

func generatedABIHash() string {
	entries := []struct{ name, abi string }{
		{"Coordinator", CoordinatorABI},
		{"CoordinatorAdversary", CoordinatorAdversaryABI},
		{"ERC1967Proxy", ERC1967ProxyABI},
		{"ReserveSink", ReserveSinkABI},
		{"SettlementVault", SettlementVaultABI},
		{"SubnetProbe", SubnetProbeABI},
	}
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

func observeReleaseLock(cfg *ResolvedConfig) (*releaseLockObservation, error) {
	if cfg == nil || cfg.Repos.SN == "" || cfg.Repos.Server == "" {
		return nil, errors.New("release repository paths are incomplete")
	}
	observation := &releaseLockObservation{EVMBuild: map[string]string{}, Repositories: map[string]string{}, Interfaces: map[string]string{}, Infrastructure: map[string]string{}}
	var err error
	observation.EVMBuild["source_hash"], err = soliditySourceHash(cfg.Repos.SN)
	if err != nil {
		return nil, err
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
	observation.EVMBuild["reserve_sink_artifact_hash"] = ReserveSinkFoundryArtifactHash
	observation.EVMBuild["settlement_vault_artifact_hash"] = SettlementVaultFoundryArtifactHash
	observation.EVMBuild["coordinator_implementation_artifact_hash"] = CoordinatorFoundryArtifactHash
	observation.EVMBuild["coordinator_proxy_artifact_hash"] = ERC1967ProxyFoundryArtifactHash
	observation.EVMBuild["governance_drill_implementation_artifact_hash"] = CoordinatorAdversaryFoundryArtifactHash
	observation.EVMBuild["precompile_probe_artifact_hash"] = SubnetProbeFoundryArtifactHash
	observation.EVMBuild["governance_drill_storage_layout_hash"] = CoordinatorAdversaryStorageLayoutHash
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

	interfaces, err := filesUnder(cfg.Repos.SN, []string{"evm/src/interfaces"}, func(name string) bool { return strings.HasSuffix(name, ".sol") })
	if err != nil {
		return nil, err
	}
	observation.Interfaces["precompile_interfaces_source_hash"], err = digestNamedFiles(cfg.Repos.SN, interfaces)
	if err != nil {
		return nil, err
	}

	xops := filepath.Join(parent, "xops")
	observation.Infrastructure["gateway_config_hash"], err = digestNamedFiles(xops, []string{"main/ansible/playbook-subtensor.yml"})
	if err != nil {
		return nil, fmt.Errorf("hash RPC gateway config: %w", err)
	}
	observation.Infrastructure["node_config_hash"], err = digestNamedFiles(xops, []string{"main/ansible/host_files/snow/subtensor/vars.yml", "main/ansible/host_files/snow/subtensor/docker-compose.yml.j2"})
	if err != nil {
		return nil, fmt.Errorf("hash Subtensor node config: %w", err)
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

func validateReleaseLock(cfg *ResolvedConfig) error {
	if cfg.Release == nil || cfg.Release.SchemaVersion != 1 || cfg.Release.Release != "1.0" || cfg.Release.Runtime.SourceTag != "v447" || cfg.Release.Runtime.SourceCommit != "1f090af85d1771c5d8ece1f0910576fbd129906e" || cfg.Release.Runtime.SpecVersion != 447 || cfg.Release.Runtime.TransactionVersion != 1 {
		return errors.New("release lock runtime identity is not the reviewed v447 release")
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
	for key, want := range map[string]string{"solidity": "0.8.24", "evm_version": "cancun", "foundry": "1.7.1", "optimizer": "true", "optimizer_runs": "200"} {
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
		"solidity": {}, "evm_version": {}, "foundry": {}, "optimizer": {}, "optimizer_runs": {},
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

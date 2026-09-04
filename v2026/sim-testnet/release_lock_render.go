package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

var releaseEVMObservedKeys = []string{
	"source_hash",
	"abigen",
	"foundry",
	"foundry_commit",
	"forge_std_commit",
	"openzeppelin_contracts_commit",
	"openzeppelin_contracts_upgradeable_commit",
	"compiler_settings_hash",
	"reserve_sink_runtime_hash",
	"settlement_vault_runtime_hash",
	"coordinator_implementation_runtime_hash",
	"coordinator_proxy_runtime_hash",
	"governance_drill_implementation_runtime_hash",
	"precompile_probe_runtime_hash",
	"fleet_batcher_runtime_hash",
	"reserve_sink_artifact_hash",
	"settlement_vault_artifact_hash",
	"coordinator_implementation_artifact_hash",
	"coordinator_proxy_artifact_hash",
	"governance_drill_implementation_artifact_hash",
	"precompile_probe_artifact_hash",
	"fleet_batcher_artifact_hash",
	"governance_drill_storage_layout_hash",
	"fleet_batcher_storage_layout_hash",
	"abi_hash",
	"coordinator_storage_layout_hash",
}

var releaseRepositoryObservedKeys = []string{
	"sn_go_source_hash",
	"server_go_source_hash",
	"connect_go_source_hash",
	"sdk_go_source_hash",
	"glog_go_source_hash",
	"goidenticons_go_source_hash",
	"proxy_go_source_hash",
	"userwireguard_go_source_hash",
	"operator_proxy_go_source_hash",
	"operator_proxy_commit",
	"sdk_mobile_build_tree_hash",
	"protocol_source_hash",
	"platform_config_source_hash",
	"platform_config_shared_tree_hash",
}

var releaseInterfaceObservedKeys = []string{
	"precompile_interfaces_source_hash",
}

var releaseInfrastructureObservedKeys = []string{
	"gateway_config_hash",
	"node_config_hash",
	"server_local_config_hash",
}

var releaseEVMAnnotationKeys = []string{
	"solidity",
	"evm_version",
	"optimizer",
	"optimizer_runs",
}

var releaseRepositoryAnnotationKeys = []string{
	"sn_audited_base_commit",
	"server_audited_base_commit",
	"vault_audited_base_commit",
}

type releaseRepository struct {
	Name string
	Root string
}

type releaseRepositoryRevision struct {
	Name     string
	Root     string
	Revision string
}

type preparedReleaseLockUpdate struct {
	Path      string
	Original  []byte
	Candidate []byte
	Mode      os.FileMode
	Snapshot  []releaseRepositoryRevision
}

type releaseLockWriter func(path string, data []byte, mode os.FileMode) error

func releaseKeySet(groups ...[]string) map[string]struct{} {
	keys := map[string]struct{}{}
	for _, group := range groups {
		for _, key := range group {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func validateObservedReleaseSection(name string, section map[string]string, keys []string) error {
	if section == nil {
		return fmt.Errorf("release-lock observation %s is missing", name)
	}
	expected := releaseKeySet(keys)
	for _, key := range keys {
		value, ok := section[key]
		if !ok {
			return fmt.Errorf("release-lock observation %s.%s is missing", name, key)
		}
		if value == "" || value != strings.TrimSpace(value) || strings.Contains(strings.ToLower(value), "placeholder") {
			return fmt.Errorf("release-lock observation %s.%s is unresolved", name, key)
		}
		switch {
		case strings.HasSuffix(key, "commit"):
			if !releaseGitCommit.MatchString(value) {
				return fmt.Errorf("release-lock observation %s.%s is not a canonical Git commit", name, key)
			}
		case name == "evm_build" && (strings.HasSuffix(key, "_runtime_hash") || strings.HasSuffix(key, "_artifact_hash")):
			if !releaseHex256.MatchString(value) {
				return fmt.Errorf("release-lock observation %s.%s is not a canonical 256-bit hash", name, key)
			}
		case strings.HasSuffix(key, "hash"):
			if !releaseSHA256.MatchString(value) {
				return fmt.Errorf("release-lock observation %s.%s is not a canonical SHA-256 digest", name, key)
			}
		}
	}
	for key := range section {
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("release-lock observation %s.%s is not in the observation schema", name, key)
		}
	}
	return nil
}

func validateReleaseLockObservation(observation *releaseLockObservation) error {
	if observation == nil {
		return errors.New("release-lock observation is missing")
	}
	if err := validateObservedReleaseSection("evm_build", observation.EVMBuild, releaseEVMObservedKeys); err != nil {
		return err
	}
	if err := validateObservedReleaseSection("repositories", observation.Repositories, releaseRepositoryObservedKeys); err != nil {
		return err
	}
	if err := validateObservedReleaseSection("interfaces", observation.Interfaces, releaseInterfaceObservedKeys); err != nil {
		return err
	}
	return validateObservedReleaseSection("infrastructure", observation.Infrastructure, releaseInfrastructureObservedKeys)
}

func releaseObservationRepositories(cfg *ResolvedConfig) ([]releaseRepository, error) {
	if cfg == nil || cfg.Repos.SN == "" || cfg.Repos.Server == "" || cfg.Repos.OperatorProxy == "" || cfg.Repos.Vault == "" || cfg.Repos.PlatformConfig == "" {
		return nil, errors.New("release repository paths are incomplete")
	}
	parent := filepath.Dir(cfg.Repos.SN)
	repositories := []releaseRepository{
		{Name: "sn", Root: cfg.Repos.SN},
		{Name: "server", Root: cfg.Repos.Server},
		{Name: "operator-proxy", Root: cfg.Repos.OperatorProxy},
		{Name: "vault", Root: cfg.Repos.Vault},
		{Name: "platform-config", Root: cfg.Repos.PlatformConfig},
	}
	for _, name := range []string{"connect", "sdk", "glog", "goidenticons", "proxy", "userwireguard"} {
		root, err := moduleRoot(parent, name)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, releaseRepository{Name: name, Root: root})
	}
	repositories = append(repositories,
		releaseRepository{Name: "xops", Root: filepath.Join(parent, "xops")},
		releaseRepository{Name: "forge-std", Root: filepath.Join(cfg.Repos.SN, "evm", "lib", "forge-std")},
		releaseRepository{Name: "openzeppelin-contracts", Root: filepath.Join(cfg.Repos.SN, "evm", "lib", "openzeppelin-contracts")},
		releaseRepository{Name: "openzeppelin-contracts-upgradeable", Root: filepath.Join(cfg.Repos.SN, "evm", "lib", "openzeppelin-contracts-upgradeable")},
	)
	seenNames := map[string]struct{}{}
	seenRoots := map[string]string{}
	for index := range repositories {
		repository := &repositories[index]
		if repository.Name == "" || repository.Root == "" {
			return nil, errors.New("release repository identity is incomplete")
		}
		if _, ok := seenNames[repository.Name]; ok {
			return nil, fmt.Errorf("duplicate release repository name %s", repository.Name)
		}
		seenNames[repository.Name] = struct{}{}
		resolved, err := filepath.EvalSymlinks(repository.Root)
		if err != nil {
			return nil, fmt.Errorf("resolve release repository %s: %w", repository.Name, err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return nil, err
		}
		if previous, ok := seenRoots[resolved]; ok {
			return nil, fmt.Errorf("release repositories %s and %s resolve to the same worktree", previous, repository.Name)
		}
		seenRoots[resolved] = repository.Name
		repository.Root = resolved
	}
	return repositories, nil
}

func cleanReleaseRepositorySnapshot(repositories []releaseRepository) ([]releaseRepositoryRevision, error) {
	if len(repositories) == 0 {
		return nil, errors.New("release repository inventory is empty")
	}
	snapshot := make([]releaseRepositoryRevision, 0, len(repositories))
	for _, repository := range repositories {
		revision, err := cleanGitRevision(repository.Root)
		if err != nil {
			return nil, fmt.Errorf("release repository %s: %w", repository.Name, err)
		}
		snapshot = append(snapshot, releaseRepositoryRevision{Name: repository.Name, Root: repository.Root, Revision: revision})
	}
	return snapshot, nil
}

func compareReleaseRepositorySnapshots(want, got []releaseRepositoryRevision) error {
	if len(want) != len(got) {
		return errors.New("release repository inventory changed during observation")
	}
	for index := range want {
		if want[index] != got[index] {
			return fmt.Errorf("release repository %s changed during observation", want[index].Name)
		}
	}
	return nil
}

func observeReleaseLockWithSnapshot(cfg *ResolvedConfig) (*releaseLockObservation, []releaseRepositoryRevision, error) {
	repositories, err := releaseObservationRepositories(cfg)
	if err != nil {
		return nil, nil, err
	}
	before, err := cleanReleaseRepositorySnapshot(repositories)
	if err != nil {
		return nil, nil, err
	}
	observation, err := observeReleaseLockUnchecked(cfg)
	if err != nil {
		return nil, nil, err
	}
	after, err := cleanReleaseRepositorySnapshot(repositories)
	if err != nil {
		return nil, nil, err
	}
	if err := compareReleaseRepositorySnapshots(before, after); err != nil {
		return nil, nil, err
	}
	return observation, after, nil
}

func observeReleaseLock(cfg *ResolvedConfig) (*releaseLockObservation, error) {
	observation, _, err := observeReleaseLockWithSnapshot(cfg)
	return observation, err
}

func releaseLockObservedSection(section map[string]any, keys []string) (map[string]string, error) {
	observed := make(map[string]string, len(keys))
	for _, key := range keys {
		value, err := lockString(section, key)
		if err != nil {
			return nil, err
		}
		observed[key] = value
	}
	return observed, nil
}

func validateReleaseLockSectionSchema(name string, section map[string]any, observedKeys, annotationKeys []string) error {
	if section == nil {
		return fmt.Errorf("release lock section %s is missing", name)
	}
	expected := releaseKeySet(observedKeys, annotationKeys)
	for key := range expected {
		value, err := lockString(section, key)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(value), "placeholder") {
			return fmt.Errorf("release lock field %s.%s is unresolved", name, key)
		}
	}
	for key := range section {
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("release lock %s.%s is not in the release schema", name, key)
		}
	}
	observed, err := releaseLockObservedSection(section, observedKeys)
	if err != nil {
		return err
	}
	return validateObservedReleaseSection(name, observed, observedKeys)
}

func validateReleaseLockStatic(lock *ReleaseLock) error {
	if lock == nil || lock.SchemaVersion != 1 || lock.Release != "1.0" {
		return errors.New("release lock schema or release is not the reviewed 1.0 release")
	}
	if err := validateReviewedRuntimeIdentity(lock); err != nil {
		return err
	}
	if !releaseImageDigest.MatchString(lock.Runtime.Image) || strings.Contains(strings.ToLower(lock.Runtime.Image), "placeholder") {
		return errors.New("runtime image is not digest-pinned")
	}
	for _, name := range []string{"postgres", "redis"} {
		image, ok := lock.Dependencies[name]
		if !ok || !releaseImageDigest.MatchString(image) || strings.Contains(strings.ToLower(image), "placeholder") {
			return fmt.Errorf("dependency %s is not digest-pinned", name)
		}
	}
	for name, image := range lock.Dependencies {
		if name == "" || !releaseImageDigest.MatchString(image) || strings.Contains(strings.ToLower(image), "placeholder") {
			return fmt.Errorf("dependency %s is not digest-pinned", name)
		}
	}
	if err := validateReleaseLockSectionSchema("evm_build", lock.EVMBuild, releaseEVMObservedKeys, releaseEVMAnnotationKeys); err != nil {
		return err
	}
	if err := validateReleaseLockSectionSchema("repositories", lock.Repositories, releaseRepositoryObservedKeys, releaseRepositoryAnnotationKeys); err != nil {
		return err
	}
	if err := validateReleaseLockSectionSchema("interfaces", lock.Interfaces, releaseInterfaceObservedKeys, nil); err != nil {
		return err
	}
	if err := validateReleaseLockSectionSchema("infrastructure", lock.Infrastructure, releaseInfrastructureObservedKeys, nil); err != nil {
		return err
	}
	if err := validateReleaseRepositorySchema(lock.Repositories); err != nil {
		return err
	}
	for _, key := range releaseRepositoryAnnotationKeys {
		value, err := lockString(lock.Repositories, key)
		if err != nil || !releaseGitCommit.MatchString(value) {
			return fmt.Errorf("release lock repositories.%s is not a canonical Git commit", key)
		}
	}
	for key, want := range map[string]string{
		"abigen":                        "1.17.0",
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
		got, err := lockString(lock.EVMBuild, key)
		if err != nil || !strings.EqualFold(got, want) {
			return fmt.Errorf("release lock evm_build.%s=%q, want %q", key, got, want)
		}
	}
	return nil
}

func copyReleaseLockAnnotations(section map[string]any, keys []string) map[string]any {
	copy := make(map[string]any, len(keys))
	for _, key := range keys {
		copy[key] = section[key]
	}
	return copy
}

func mergeReleaseObservation(section map[string]any, observation map[string]string) map[string]any {
	for key, value := range observation {
		section[key] = value
	}
	return section
}

func releaseLockWithObservation(current *ReleaseLock, observation *releaseLockObservation) (*ReleaseLock, error) {
	if err := validateReleaseLockStatic(current); err != nil {
		return nil, err
	}
	if err := validateReleaseLockObservation(observation); err != nil {
		return nil, err
	}
	dependencies := make(map[string]string, len(current.Dependencies))
	for name, image := range current.Dependencies {
		dependencies[name] = image
	}
	candidate := &ReleaseLock{
		SchemaVersion:  current.SchemaVersion,
		Release:        current.Release,
		Runtime:        current.Runtime,
		EVMBuild:       mergeReleaseObservation(copyReleaseLockAnnotations(current.EVMBuild, releaseEVMAnnotationKeys), observation.EVMBuild),
		Repositories:   mergeReleaseObservation(copyReleaseLockAnnotations(current.Repositories, releaseRepositoryAnnotationKeys), observation.Repositories),
		Dependencies:   dependencies,
		Interfaces:     mergeReleaseObservation(map[string]any{}, observation.Interfaces),
		Infrastructure: mergeReleaseObservation(map[string]any{}, observation.Infrastructure),
	}
	if err := validateReleaseLockStatic(candidate); err != nil {
		return nil, fmt.Errorf("observed release lock is invalid: %w", err)
	}
	return candidate, nil
}

func decodeReleaseLockBytes(data []byte) (*ReleaseLock, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	lock := new(ReleaseLock)
	if err := decoder.Decode(lock); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("release lock contains multiple YAML documents")
		}
		return nil, fmt.Errorf("decode trailing release-lock YAML: %w", err)
	}
	return lock, nil
}

func canonicalReleaseLockBytes(lock *ReleaseLock) ([]byte, error) {
	if err := validateReleaseLockStatic(lock); err != nil {
		return nil, err
	}
	encoded, err := yaml.Marshal(lock)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeReleaseLockBytes(encoded)
	if err != nil {
		return nil, fmt.Errorf("round-trip canonical release lock: %w", err)
	}
	if !reflect.DeepEqual(decoded, lock) {
		return nil, errors.New("canonical release-lock rendering changed semantic values")
	}
	confirmed, err := yaml.Marshal(decoded)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(encoded, confirmed) {
		return nil, errors.New("canonical release-lock rendering is nondeterministic")
	}
	return encoded, nil
}

func configuredReleaseLockPath(cfg *ResolvedConfig) (string, os.FileMode, error) {
	if cfg == nil || cfg.Config == nil || cfg.Repos.SN == "" {
		return "", 0, errors.New("resolved release-lock configuration is incomplete")
	}
	configured := cfg.Config.Manifests.ReleaseLock
	if configured == "" || configured != strings.TrimSpace(configured) {
		return "", 0, errors.New("configured release-lock path is empty or noncanonical")
	}
	path := configured
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(cfg.ConfigPath), path)
	}
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", 0, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", 0, errors.New("configured release-lock path is not a regular file")
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", 0, err
	}
	resolvedSN, err := filepath.EvalSymlinks(cfg.Repos.SN)
	if err != nil {
		return "", 0, err
	}
	resolvedSN, err = filepath.Abs(resolvedSN)
	if err != nil {
		return "", 0, err
	}
	relative, err := filepath.Rel(resolvedSN, resolvedPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", 0, errors.New("configured release-lock path escapes the SN repository")
	}
	return resolvedPath, info.Mode().Perm(), nil
}

func prepareReleaseLockUpdate(cfg *ResolvedConfig) (*preparedReleaseLockUpdate, error) {
	path, mode, err := configuredReleaseLockPath(cfg)
	if err != nil {
		return nil, err
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	loaded, err := decodeReleaseLockBytes(original)
	if err != nil {
		return nil, fmt.Errorf("decode configured release lock: %w", err)
	}
	if !reflect.DeepEqual(loaded, cfg.Release) {
		return nil, errors.New("configured release lock changed after configuration load")
	}
	observation, snapshot, err := observeReleaseLockWithSnapshot(cfg)
	if err != nil {
		return nil, err
	}
	candidate, err := releaseLockWithObservation(loaded, observation)
	if err != nil {
		return nil, err
	}
	encoded, err := canonicalReleaseLockBytes(candidate)
	if err != nil {
		return nil, err
	}
	return &preparedReleaseLockUpdate{Path: path, Original: original, Candidate: encoded, Mode: mode, Snapshot: snapshot}, nil
}

func writeReleaseLockUpdate(update *preparedReleaseLockUpdate, writer releaseLockWriter) (bool, error) {
	if update == nil || update.Path == "" || len(update.Original) == 0 || len(update.Candidate) == 0 || writer == nil {
		return false, errors.New("prepared release-lock update is incomplete")
	}
	info, err := os.Lstat(update.Path)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != update.Mode.Perm() {
		return false, errors.New("release-lock target identity or permissions changed before update")
	}
	current, err := os.ReadFile(update.Path)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(current, update.Original) {
		return false, errors.New("release-lock bytes changed before update")
	}
	if bytes.Equal(update.Original, update.Candidate) {
		return false, nil
	}
	if err := writer(update.Path, update.Candidate, update.Mode); err != nil {
		return false, err
	}
	written, err := os.ReadFile(update.Path)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(written, update.Candidate) {
		return false, errors.New("atomic release-lock writer did not install the exact candidate bytes")
	}
	return true, nil
}

func applyReleaseLockUpdate(cfg *ResolvedConfig, update *preparedReleaseLockUpdate, writer releaseLockWriter) (bool, error) {
	if update == nil {
		return false, errors.New("prepared release-lock update is missing")
	}
	repositories, err := releaseObservationRepositories(cfg)
	if err != nil {
		return false, err
	}
	current, err := cleanReleaseRepositorySnapshot(repositories)
	if err != nil {
		return false, err
	}
	if err := compareReleaseRepositorySnapshots(update.Snapshot, current); err != nil {
		return false, err
	}
	return writeReleaseLockUpdate(update, writer)
}

func runReleaseLockCommand(cfg *ResolvedConfig, options cliOptions) error {
	if options.Format != "human" || options.StateDir != "" || options.PlanHash != "" || options.Name != "" || options.Manifest != "" || options.Detach {
		return errors.New("release-lock accepts only repository overrides, --config, and --apply")
	}
	update, err := prepareReleaseLockUpdate(cfg)
	if err != nil {
		return err
	}
	if !options.Apply {
		_, err = os.Stdout.Write(update.Candidate)
		return err
	}
	written, err := applyReleaseLockUpdate(cfg, update, atomicWrite)
	if err != nil {
		return err
	}
	status := "unchanged"
	if written {
		status = "updated"
	}
	_, err = fmt.Fprintf(os.Stdout, "%s %s (%s)\n", status, update.Path, digestBytes(update.Candidate))
	return err
}

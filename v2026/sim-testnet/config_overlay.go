package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// The server resolver accepts one config root, then searches its literal,
// WARP_ENV, and all children. A simulator operator needs the checked-out
// config/local deployment defaults and the shared/versioned config/all assets
// at the same time, while retaining a non-local WARP_ENV so missing MinIO
// configuration cannot silently select the local blob backend.
const operatorConfigOverlayVersion = "local-all-v1"

func operatorEnvironment(operator int) string {
	return fmt.Sprintf("sim-testnet-op-%d", operator)
}

func operatorConfigHome(stateDir string, operator int) string {
	return filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator), "config")
}

func requiredVersionedConfigResource(root, group, name string) (string, error) {
	pattern := filepath.Join(root, "all", group, "*", name)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	for index := len(matches) - 1; index >= 0; index-- {
		info, statErr := os.Stat(matches[index])
		if statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return matches[index], nil
		}
	}
	return "", fmt.Errorf("required versioned config resource all/%s/*/%s is unavailable", group, name)
}

func validateOperatorConfigSources(cfg *ResolvedConfig) error {
	if cfg == nil || cfg.Repos.PlatformConfig == "" {
		return errors.New("platform config repository is unavailable")
	}
	for _, directory := range []string{"local", "all"} {
		path := filepath.Join(cfg.Repos.PlatformConfig, directory)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return stateMismatchError(err, "platform config source %s is not a directory", path)
		}
	}
	for _, name := range []string{"settings.yml", "redis.yml"} {
		path := filepath.Join(cfg.Repos.PlatformConfig, "local", name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return stateMismatchError(err, "required local config resource %s is unavailable", path)
		}
	}
	for _, resource := range []struct{ group, name string }{
		{"mmdb", "ip-ipinfo.mmdb"},
		{"arindb", "arin.mmdb"},
	} {
		if _, err := requiredVersionedConfigResource(cfg.Repos.PlatformConfig, resource.group, resource.name); err != nil {
			return err
		}
	}
	for _, name := range []string{"apple_roots.pem", "iso-country-list.yml", "city-list.yml"} {
		path := filepath.Join(cfg.Repos.PlatformConfig, "all", name)
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return stateMismatchError(err, "required shared config resource %s is unavailable", path)
		}
	}
	return nil
}

func exactDirectorySymlink(linkPath, target string) error {
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	target = filepath.Clean(target)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return stateMismatchError(err, "operator config source %s is not a directory", target)
	}
	link, err := os.Readlink(linkPath)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(linkPath), link)
	}
	if filepath.Clean(link) != target {
		return fmt.Errorf("operator config link %s targets %s, want %s", linkPath, filepath.Clean(link), target)
	}
	return nil
}

func ensureExactDirectorySymlink(linkPath, target string) error {
	if err := exactDirectorySymlink(linkPath, target); err == nil {
		return nil
	} else if _, lstatErr := os.Lstat(linkPath); lstatErr == nil {
		return fmt.Errorf("operator config overlay path %s already exists but is not the approved link: %w", linkPath, err)
	} else if !errors.Is(lstatErr, os.ErrNotExist) {
		return lstatErr
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o700); err != nil {
		return err
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := os.Symlink(filepath.Clean(target), linkPath); err != nil {
		return err
	}
	return exactDirectorySymlink(linkPath, target)
}

func ensureOperatorConfigOverlays(cfg *ResolvedConfig, stateDir string) error {
	if err := validateOperatorConfigSources(cfg); err != nil {
		return err
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		home := operatorConfigHome(stateDir, operator)
		if err := os.MkdirAll(home, 0o700); err != nil {
			return err
		}
		if err := ensureExactDirectorySymlink(filepath.Join(home, operatorEnvironment(operator)), filepath.Join(cfg.Repos.PlatformConfig, "local")); err != nil {
			return err
		}
		if err := ensureExactDirectorySymlink(filepath.Join(home, "all"), filepath.Join(cfg.Repos.PlatformConfig, "all")); err != nil {
			return err
		}
	}
	return nil
}

func validateOperatorConfigOverlays(cfg *ResolvedConfig, stateDir string) error {
	if err := validateOperatorConfigSources(cfg); err != nil {
		return err
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		home := operatorConfigHome(stateDir, operator)
		if err := exactDirectorySymlink(filepath.Join(home, operatorEnvironment(operator)), filepath.Join(cfg.Repos.PlatformConfig, "local")); err != nil {
			return err
		}
		if err := exactDirectorySymlink(filepath.Join(home, "all"), filepath.Join(cfg.Repos.PlatformConfig, "all")); err != nil {
			return err
		}
	}
	return nil
}

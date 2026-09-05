package main

// executable_attestation.go authenticates the release driver before an apply
// may write source, start processes, or change testnet state. Read-only
// development commands and safely fenced teardown remain available.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"
)

// Distinguish read-only use, release-lock creation, and locked release writes.
type executableAttestationMode uint8

const (
	executableAttestationNone executableAttestationMode = iota
	executableAttestationPushedSource
	executableAttestationLockedSource
)

// Retain the authenticated subset of Go's in-process build information.
type releaseExecutableBuildIdentity struct {
	PackagePath string
	ModulePath  string
	Revision    string
	Modified    bool
	Trimpath    bool
}

// Retain the three independent source authorities which must agree.
type releaseExecutableSourceIdentity struct {
	Revision         string
	PushedRevision   string
	SourceHash       string
	LockedSourceHash string
}

// Permit command dispatch tests to supply an inert identity observer.
type releaseExecutableAuthenticator func(context.Context, *ResolvedConfig, executableAttestationMode) error

// Permit command dispatch tests to resolve an inert configuration without
// consulting repositories, secrets, or public services.
type resolvedConfigLoader func(LoadOptions) (*ResolvedConfig, error)

// Bound the one public source-freshness request made before an apply.
const currentSNMainQueryTimeout = 10 * time.Second

// Supply the independently observed public origin/main tip in deterministic
// tests without weakening the production network boundary.
type currentSNRevisionObserver func(context.Context) (string, error)

// Select the strictest identity required by one public command. An apply flag
// is never treated as a development/read-only invocation, even on a command
// which does not otherwise consume it. Stop remains available during source
// repair and relies on its exact process-ownership fence for safe teardown.
func executableAttestationModeForCommand(command string, options cliOptions) executableAttestationMode {
	if command == "stop" {
		return executableAttestationNone
	}
	if options.Apply {
		if command == "release-lock" {
			return executableAttestationPushedSource
		}
		return executableAttestationLockedSource
	}
	return executableAttestationNone
}

// Apply the command policy through an explicit callback so dispatch coverage
// can be tested without trusting the Go test binary's own build information.
func authenticateCommandExecutable(ctx context.Context, cfg *ResolvedConfig, command string, options cliOptions, authenticate releaseExecutableAuthenticator) error {
	mode := executableAttestationModeForCommand(command, options)
	if mode == executableAttestationNone {
		return nil
	}
	if authenticate == nil {
		return errors.New("release executable authenticator is unavailable")
	}
	if err := authenticate(ctx, cfg, mode); err != nil {
		return fmt.Errorf("%s executable attestation: %w", command, err)
	}
	return nil
}

// Require one canonical absolute path to name the same regular executable the
// kernel reports. Relative/PATH, symlink, and hard-link aliases are rejected so
// the reviewed command cannot silently name different bytes.
func validateReleaseExecutablePath(invocationPath, executablePath string) error {
	validate := func(label, path string) (os.FileInfo, error) {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, fmt.Errorf("%s must be a canonical absolute path", label)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", label, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return nil, fmt.Errorf("%s must be an executable regular file, not a symlink", label)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 {
			return nil, fmt.Errorf("%s must have exactly one filesystem link", label)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", label, err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return nil, err
		}
		if resolved != path {
			return nil, fmt.Errorf("%s traverses a symlink", label)
		}
		return info, nil
	}
	invocationInfo, err := validate("invoked executable", invocationPath)
	if err != nil {
		return err
	}
	executableInfo, err := validate("running executable", executablePath)
	if err != nil {
		return err
	}
	if invocationPath != executablePath || !os.SameFile(invocationInfo, executableInfo) {
		return errors.New("invoked executable path differs from the running executable")
	}
	return nil
}

// Decode only Go's signed-in build settings needed to distinguish a canonical
// release build from go run, a dirty developer build, or another module.
func parseReleaseExecutableBuildInfo(info *debug.BuildInfo) (releaseExecutableBuildIdentity, error) {
	if info == nil {
		return releaseExecutableBuildIdentity{}, errors.New("Go build information is unavailable")
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		if _, duplicate := settings[setting.Key]; duplicate {
			return releaseExecutableBuildIdentity{}, fmt.Errorf("Go build information repeats setting %q", setting.Key)
		}
		settings[setting.Key] = setting.Value
	}
	if info.Path != "github.com/urfoundation/sn/sim-testnet" || info.Main.Path != "github.com/urfoundation/sn" {
		return releaseExecutableBuildIdentity{}, fmt.Errorf("running executable has package/module %q/%q", info.Path, info.Main.Path)
	}
	if settings["vcs"] != "git" {
		return releaseExecutableBuildIdentity{}, errors.New("running executable is not stamped from Git")
	}
	revision := strings.TrimSpace(settings["vcs.revision"])
	if !releaseGitCommit.MatchString(revision) {
		return releaseExecutableBuildIdentity{}, errors.New("running executable has no canonical VCS revision")
	}
	modified, modifiedOK := settings["vcs.modified"]
	if !modifiedOK || (modified != "true" && modified != "false") {
		return releaseExecutableBuildIdentity{}, errors.New("running executable has no canonical VCS modification status")
	}
	if modified != "false" {
		return releaseExecutableBuildIdentity{}, errors.New("running executable was built from a modified VCS checkout")
	}
	trimpath, trimpathOK := settings["-trimpath"]
	if !trimpathOK || trimpath != "true" {
		return releaseExecutableBuildIdentity{}, errors.New("running executable is not a canonical -trimpath release build")
	}
	return releaseExecutableBuildIdentity{
		PackagePath: info.Path,
		ModulePath:  info.Main.Path,
		Revision:    revision,
		Modified:    false,
		Trimpath:    true,
	}, nil
}

// Recognize the same canonical GitHub transports accepted by the workspace
// source-freeze gate; local or lookalike remotes cannot count as pushed source.
func validSNOriginURL(value string) bool {
	switch strings.TrimSpace(value) {
	case "git@github.com:urfoundation/sn", "git@github.com:urfoundation/sn.git", "https://github.com/urfoundation/sn", "https://github.com/urfoundation/sn.git":
		return true
	default:
		return false
	}
}

// Remove Git repository/config redirects from the public branch observation.
// Network proxy and certificate variables remain available, while Git cannot
// replace the reviewed GitHub URL through local, global, or system config.
func releaseGitNetworkEnvironment(environment []string) []string {
	clean := make([]string, 0, len(environment)+3)
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		clean = append(clean, entry)
	}
	return append(clean, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
}

// Decode the one exact ref record requested from GitHub without accepting an
// advertised sibling ref, extra output, or a noncanonical commit identity.
func parseCurrentSNRevision(output []byte) (string, error) {
	line := string(output)
	const suffix = "\trefs/heads/main\n"
	if !strings.HasSuffix(line, suffix) || strings.Count(line, "\n") != 1 || strings.Contains(line, "\r") {
		return "", errors.New("current SN origin/main returned a malformed revision")
	}
	revision := strings.TrimSuffix(line, suffix)
	if !releaseGitCommit.MatchString(revision) {
		return "", errors.New("current SN origin/main returned an invalid revision")
	}
	return revision, nil
}

// Query the canonical public GitHub URL from an empty directory with a short
// deadline and config redirects disabled. One process authenticates exactly
// once, so no cross-command freshness cache can authorize a later mutation.
func observeCurrentSNRevision(ctx context.Context) (string, error) {
	queryCtx, cancel := context.WithTimeout(ctx, currentSNMainQueryTimeout)
	defer cancel()
	workDir, err := os.MkdirTemp("", "sim-testnet-sn-origin-")
	if err != nil {
		return "", fmt.Errorf("create isolated SN origin query directory: %w", err)
	}
	defer os.Remove(workDir)
	command := exec.CommandContext(queryCtx, "git", "-c", "http.followRedirects=false", "ls-remote", "--exit-code", "https://github.com/urfoundation/sn.git", "refs/heads/main")
	command.Dir = workDir
	command.Env = releaseGitNetworkEnvironment(os.Environ())
	output, err := command.Output()
	if err != nil {
		if queryCtx.Err() != nil {
			return "", fmt.Errorf("query current SN origin/main within %s: %w", currentSNMainQueryTimeout, queryCtx.Err())
		}
		return "", fmt.Errorf("query current SN origin/main: %w", err)
	}
	return parseCurrentSNRevision(output)
}

// Resolve the configured upstream and compare its fetched revision with one
// bounded, noninteractive observation of the current GitHub branch. This
// prevents either a stale remote-tracking ref or an unpushed commit from
// authorizing a mutation after the terminal source-freeze gate.
func pushedSNRevision(ctx context.Context, root string, observeCurrent currentSNRevisionObserver) (string, error) {
	if observeCurrent == nil {
		return "", errors.New("current SN origin/main observer is unavailable")
	}
	originCommand := exec.CommandContext(ctx, "git", "-C", root, "config", "--get", "remote.origin.url")
	originOutput, err := originCommand.Output()
	if err != nil {
		return "", fmt.Errorf("read SN origin: %w", err)
	}
	if !validSNOriginURL(string(originOutput)) {
		return "", errors.New("SN origin is not github.com/urfoundation/sn")
	}
	upstreamCommand := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	upstreamOutput, err := upstreamCommand.Output()
	if err != nil {
		return "", fmt.Errorf("read SN upstream: %w", err)
	}
	if strings.TrimSpace(string(upstreamOutput)) != "origin/main" {
		return "", errors.New("SN upstream is not origin/main")
	}
	revisionCommand := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "@{upstream}^{commit}")
	revisionOutput, err := revisionCommand.Output()
	if err != nil {
		return "", fmt.Errorf("read pushed SN revision: %w", err)
	}
	fetchedRevision := strings.TrimSpace(string(revisionOutput))
	if !releaseGitCommit.MatchString(fetchedRevision) {
		return "", errors.New("SN upstream returned an invalid revision")
	}
	remoteRevision, err := observeCurrent(ctx)
	if err != nil {
		return "", err
	}
	if fetchedRevision != remoteRevision {
		return "", fmt.Errorf("fetched SN origin/main %s differs from current GitHub branch %s", fetchedRevision, remoteRevision)
	}
	return remoteRevision, nil
}

// Observe a clean commit on both sides of the production-source digest, then
// bind it to the locally fetched canonical upstream and, for release commands,
// the exact source digest in the loaded release lock.
func observeReleaseExecutableSourceWithCurrent(ctx context.Context, cfg *ResolvedConfig, mode executableAttestationMode, observeCurrent currentSNRevisionObserver) (releaseExecutableSourceIdentity, error) {
	if cfg == nil || cfg.Repos.SN == "" {
		return releaseExecutableSourceIdentity{}, errors.New("SN release checkout is unavailable")
	}
	sourceHash, revision, err := observeCleanGoReleaseSource(cfg.Repos.SN)
	if err != nil {
		return releaseExecutableSourceIdentity{}, fmt.Errorf("observe clean SN release source: %w", err)
	}
	pushedRevision, err := pushedSNRevision(ctx, cfg.Repos.SN, observeCurrent)
	if err != nil {
		return releaseExecutableSourceIdentity{}, err
	}
	lockedSourceHash := ""
	if mode == executableAttestationLockedSource {
		if cfg.Release == nil {
			return releaseExecutableSourceIdentity{}, errors.New("release lock is unavailable")
		}
		lockedSourceHash, err = lockString(cfg.Release.Repositories, "sn_go_source_hash")
		if err != nil {
			return releaseExecutableSourceIdentity{}, err
		}
	}
	return releaseExecutableSourceIdentity{
		Revision: revision, PushedRevision: pushedRevision,
		SourceHash: sourceHash, LockedSourceHash: lockedSourceHash,
	}, nil
}

// Observe release source against the live canonical GitHub branch.
func observeReleaseExecutableSource(ctx context.Context, cfg *ResolvedConfig, mode executableAttestationMode) (releaseExecutableSourceIdentity, error) {
	return observeReleaseExecutableSourceWithCurrent(ctx, cfg, mode, observeCurrentSNRevision)
}

// Join independently observed build, checkout, upstream, and release-lock
// identities. Each equality has a distinct failure message for operator action.
func validateReleaseExecutableIdentity(build releaseExecutableBuildIdentity, source releaseExecutableSourceIdentity, mode executableAttestationMode) error {
	if mode != executableAttestationPushedSource && mode != executableAttestationLockedSource {
		return errors.New("release executable attestation mode is invalid")
	}
	if build.Modified {
		return errors.New("running executable was built from a modified VCS checkout")
	}
	if !build.Trimpath {
		return errors.New("running executable is not a canonical release build")
	}
	if !releaseGitCommit.MatchString(build.Revision) || !releaseGitCommit.MatchString(source.Revision) || !releaseGitCommit.MatchString(source.PushedRevision) {
		return errors.New("release executable revision identity is incomplete")
	}
	if build.Revision != source.Revision {
		return fmt.Errorf("running executable revision %s differs from SN checkout %s", build.Revision, source.Revision)
	}
	if source.Revision != source.PushedRevision {
		return fmt.Errorf("SN checkout revision %s differs from pushed origin/main %s", source.Revision, source.PushedRevision)
	}
	if !releaseSHA256.MatchString(source.SourceHash) {
		return errors.New("observed SN source hash is not canonical")
	}
	if mode == executableAttestationLockedSource {
		if !releaseSHA256.MatchString(source.LockedSourceHash) {
			return errors.New("release-locked SN source hash is not canonical")
		}
		if source.SourceHash != source.LockedSourceHash {
			return fmt.Errorf("SN source hash %s differs from release lock %s", source.SourceHash, source.LockedSourceHash)
		}
	}
	return nil
}

// Authenticate the exact running process before dispatch reaches any public
// mutation. The release-lock refresh is the sole source-only mode because it
// creates the new lock; all other writes require the already locked digest.
func authenticateRunningReleaseExecutable(ctx context.Context, cfg *ResolvedConfig, mode executableAttestationMode) error {
	if mode == executableAttestationNone {
		return nil
	}
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate running executable: %w", err)
	}
	if err := validateReleaseExecutablePath(os.Args[0], executablePath); err != nil {
		return err
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.New("running executable has no Go build information")
	}
	build, err := parseReleaseExecutableBuildInfo(info)
	if err != nil {
		return err
	}
	source, err := observeReleaseExecutableSource(ctx, cfg, mode)
	if err != nil {
		return err
	}
	return validateReleaseExecutableIdentity(build, source, mode)
}

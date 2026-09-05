package main

// executable_attestation_test.go fixes the release-driver authorization
// boundary independently of live RPCs, wallets, and source-tree timing.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// Construct one valid build-info record which individual tests can corrupt.
func testReleaseExecutableBuildInfo(revision string) *debug.BuildInfo {
	return &debug.BuildInfo{
		Path: "github.com/urfoundation/sn/sim-testnet",
		Main: debug.Module{Path: "github.com/urfoundation/sn"},
		Settings: []debug.BuildSetting{
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: revision},
			{Key: "vcs.modified", Value: "false"},
			{Key: "-trimpath", Value: "true"},
		},
	}
}

// Every public apply path requires attestation, while ordinary development
// reads, mutation dry-runs, and safely fenced teardown remain available.
func TestExecutableAttestationModeCoversEveryPublicMutation(t *testing.T) {
	for _, command := range []string{"setup", "launch", "resume", "scenario", "retire"} {
		if got := executableAttestationModeForCommand(command, cliOptions{Apply: true}); got != executableAttestationLockedSource {
			t.Fatalf("%s apply attestation = %d", command, got)
		}
		if got := executableAttestationModeForCommand(command, cliOptions{}); got != executableAttestationNone {
			t.Fatalf("%s dry-run attestation = %d", command, got)
		}
	}
	if got := executableAttestationModeForCommand("release-lock", cliOptions{Apply: true}); got != executableAttestationPushedSource {
		t.Fatalf("release-lock apply attestation = %d", got)
	}
	for _, command := range []string{"doctor", "plan", "status", "inspect", "analyze", "tail"} {
		if got := executableAttestationModeForCommand(command, cliOptions{Apply: true}); got != executableAttestationLockedSource {
			t.Fatalf("%s with an extraneous apply flag attestation = %d", command, got)
		}
	}
	if got := executableAttestationModeForCommand("stop", cliOptions{Apply: true}); got != executableAttestationNone {
		t.Fatalf("stop with an extraneous apply flag attestation = %d", got)
	}
}

// Read-only commands and release-lock rendering do not require release build
// identity, preserving diagnosis from a dirty checkout without permitting writes.
func TestExecutableAttestationModeAllowsReadOnlyDevelopmentCommands(t *testing.T) {
	for _, command := range []string{"doctor", "release-lock", "plan", "status", "inspect", "analyze", "tail", "stop"} {
		if got := executableAttestationModeForCommand(command, cliOptions{}); got != executableAttestationNone {
			t.Fatalf("%s read attestation = %d", command, got)
		}
	}
}

// Stop must remain available while a failed release checkout is dirty or its
// lock is being repaired; process ownership is enforced by StopDeployment.
func TestStopExecutableAttestationRemainsAvailableDuringSourceDrift(t *testing.T) {
	err := authenticateCommandExecutable(context.Background(), nil, "stop", cliOptions{}, func(context.Context, *ResolvedConfig, executableAttestationMode) error {
		t.Fatal("stop consulted release source identity")
		return os.ErrInvalid
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A canonical absolute regular file is the only unambiguous invocation path.
func TestReleaseExecutablePathAcceptsExactAbsoluteRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sim-testnet")
	if err := os.WriteFile(path, []byte("release"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseExecutablePath(path, path); err != nil {
		t.Fatal(err)
	}
}

// Reject every common way PATH lookup, symlinks, and aliases can make the
// reviewed command name differ from the running executable.
func TestReleaseExecutablePathRejectsAmbiguousAliases(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	realPath := filepath.Join(realDirectory, "sim-testnet")
	otherPath := filepath.Join(realDirectory, "other")
	if err := os.WriteFile(realPath, []byte("release"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("other"), 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(root, "sim-testnet-link")
	if err := os.Symlink(realPath, symlinkPath); err != nil {
		t.Fatal(err)
	}
	directoryLink := filepath.Join(root, "linked-directory")
	if err := os.Symlink(realDirectory, directoryLink); err != nil {
		t.Fatal(err)
	}
	hardLinkPath := filepath.Join(root, "sim-testnet-hard-link")
	if err := os.Link(realPath, hardLinkPath); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		invocation string
		executable string
	}{
		{name: "PATH", invocation: "sim-testnet", executable: realPath},
		{name: "relative", invocation: "./sim-testnet", executable: realPath},
		{name: "file symlink", invocation: symlinkPath, executable: realPath},
		{name: "directory symlink", invocation: filepath.Join(directoryLink, "sim-testnet"), executable: realPath},
		{name: "different file", invocation: otherPath, executable: realPath},
		{name: "hard-link alias", invocation: hardLinkPath, executable: realPath},
	} {
		if err := validateReleaseExecutablePath(test.invocation, test.executable); err == nil {
			t.Errorf("%s executable alias was accepted", test.name)
		}
	}
	if err := validateReleaseExecutablePath(hardLinkPath, hardLinkPath); err == nil {
		t.Fatal("multiply linked executable was accepted under its kernel path")
	}
}

// Command dispatch cannot omit the authenticator or accidentally call it for
// a development read; this seam avoids depending on the test binary's VCS data.
func TestAuthenticateCommandExecutableInvokesExactSelectedMode(t *testing.T) {
	cfg := new(ResolvedConfig)
	var calls []executableAttestationMode
	authenticate := func(_ context.Context, got *ResolvedConfig, mode executableAttestationMode) error {
		if got != cfg {
			t.Fatal("authenticator received a different resolved configuration")
		}
		calls = append(calls, mode)
		return nil
	}
	if err := authenticateCommandExecutable(context.Background(), cfg, "doctor", cliOptions{}, authenticate); err != nil {
		t.Fatal(err)
	}
	if err := authenticateCommandExecutable(context.Background(), cfg, "release-lock", cliOptions{Apply: true}, authenticate); err != nil {
		t.Fatal(err)
	}
	if err := authenticateCommandExecutable(context.Background(), cfg, "launch", cliOptions{Apply: true}, authenticate); err != nil {
		t.Fatal(err)
	}
	if err := authenticateCommandExecutable(context.Background(), cfg, "stop", cliOptions{}, authenticate); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != executableAttestationPushedSource || calls[1] != executableAttestationLockedSource {
		t.Fatalf("authenticator modes = %v", calls)
	}
}

// Every public apply spelling reaches its exact authenticator before release
// lock generation, state resolution, reads, process launch, or chain writes.
func TestRunMainAuthenticatesEveryPublicApplyBeforeDispatch(t *testing.T) {
	rejected := errors.New("test executable identity rejected")
	for _, command := range []string{"doctor", "release-lock", "plan", "setup", "launch", "resume", "status", "inspect", "analyze", "scenario", "tail", "retire"} {
		loadCalls := 0
		authenticateCalls := 0
		cfg := new(ResolvedConfig)
		err := runMainWithReleaseDependencies([]string{command, "--apply"}, func(LoadOptions) (*ResolvedConfig, error) {
			loadCalls++
			return cfg, nil
		}, func(_ context.Context, got *ResolvedConfig, mode executableAttestationMode) error {
			authenticateCalls++
			if got != cfg {
				t.Fatalf("%s authenticator received a different resolved configuration", command)
			}
			wantMode := executableAttestationLockedSource
			if command == "release-lock" {
				wantMode = executableAttestationPushedSource
			}
			if mode != wantMode {
				t.Fatalf("%s authenticator mode = %d, want %d", command, mode, wantMode)
			}
			return rejected
		})
		if !errors.Is(err, rejected) {
			t.Fatalf("%s apply error = %v", command, err)
		}
		if loadCalls != 1 || authenticateCalls != 1 {
			t.Fatalf("%s apply load/authenticate calls = %d/%d", command, loadCalls, authenticateCalls)
		}
	}
}

// Source drift must not make the process-ownership-fenced stop command depend
// on executable authentication after a failed live campaign.
func TestRunMainStopBypassesExecutableAttestation(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "supervisor.state.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	authenticateCalls := 0
	err := runMainWithReleaseDependencies([]string{"stop", "--apply", "--state-dir", stateDir}, func(LoadOptions) (*ResolvedConfig, error) {
		return new(ResolvedConfig), nil
	}, func(context.Context, *ResolvedConfig, executableAttestationMode) error {
		authenticateCalls++
		return errors.New("unexpected executable authentication")
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Fatalf("stop malformed-state error = %v", err)
	}
	if authenticateCalls != 0 {
		t.Fatalf("stop executable authentication calls = %d", authenticateCalls)
	}
}

// A selected mutation never falls through when its authenticator is absent or
// rejects the process identity.
func TestAuthenticateCommandExecutableFailsClosed(t *testing.T) {
	if err := authenticateCommandExecutable(context.Background(), new(ResolvedConfig), "launch", cliOptions{Apply: true}, nil); err == nil {
		t.Fatal("mutation with no authenticator was accepted")
	}
	err := authenticateCommandExecutable(context.Background(), new(ResolvedConfig), "doctor", cliOptions{Apply: true}, func(context.Context, *ResolvedConfig, executableAttestationMode) error {
		return os.ErrInvalid
	})
	if err == nil || !strings.Contains(err.Error(), "doctor executable attestation") {
		t.Fatalf("rejected mutation error = %v", err)
	}
}

// Parse the exact clean, stamped, reproducible Go build shape used by release
// binaries; developer builds are tested separately below.
func TestParseReleaseExecutableBuildInfoAcceptsCanonicalRelease(t *testing.T) {
	revision := strings.Repeat("a", 40)
	identity, err := parseReleaseExecutableBuildInfo(testReleaseExecutableBuildInfo(revision))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Revision != revision || identity.Modified || !identity.Trimpath || identity.PackagePath != "github.com/urfoundation/sn/sim-testnet" || identity.ModulePath != "github.com/urfoundation/sn" {
		t.Fatalf("build identity = %+v", identity)
	}
}

// Missing VCS evidence, modified builds, go-run defaults, wrong modules, and
// duplicate settings all fail before any source or chain operation begins.
func TestParseReleaseExecutableBuildInfoRejectsDevelopmentAndAmbiguousBuilds(t *testing.T) {
	revision := strings.Repeat("a", 40)
	modified := testReleaseExecutableBuildInfo(revision)
	modified.Settings[2].Value = "true"
	goRun := testReleaseExecutableBuildInfo(revision)
	goRun.Settings = goRun.Settings[:3]
	wrongModule := testReleaseExecutableBuildInfo(revision)
	wrongModule.Main.Path = "example.invalid/sn"
	missingRevision := testReleaseExecutableBuildInfo(revision)
	missingRevision.Settings[1].Value = ""
	uppercaseRevision := testReleaseExecutableBuildInfo(strings.Repeat("A", 40))
	duplicate := testReleaseExecutableBuildInfo(revision)
	duplicate.Settings = append(duplicate.Settings, debug.BuildSetting{Key: "vcs.revision", Value: revision})
	for _, test := range []struct {
		name string
		info *debug.BuildInfo
	}{
		{name: "missing build info", info: nil},
		{name: "modified", info: modified},
		{name: "go run", info: goRun},
		{name: "wrong module", info: wrongModule},
		{name: "missing revision", info: missingRevision},
		{name: "uppercase revision", info: uppercaseRevision},
		{name: "duplicate setting", info: duplicate},
	} {
		if _, err := parseReleaseExecutableBuildInfo(test.info); err == nil {
			t.Errorf("%s build was accepted", test.name)
		}
	}
}

// Exact equality between running build, clean checkout, fetched upstream, and
// release-locked production-source bytes is sufficient for a normal mutation.
func TestReleaseExecutableIdentityAcceptsExactLockedPushedSource(t *testing.T) {
	revision := strings.Repeat("a", 40)
	hash := "sha256:" + strings.Repeat("b", 64)
	build := releaseExecutableBuildIdentity{PackagePath: "github.com/urfoundation/sn/sim-testnet", ModulePath: "github.com/urfoundation/sn", Revision: revision, Trimpath: true}
	source := releaseExecutableSourceIdentity{Revision: revision, PushedRevision: revision, SourceHash: hash, LockedSourceHash: hash}
	if err := validateReleaseExecutableIdentity(build, source, executableAttestationLockedSource); err != nil {
		t.Fatal(err)
	}
}

// Each stale or incompletely authenticated identity dimension fails closed
// with no dependence on Git, the network, or scheduler timing.
func TestReleaseExecutableIdentityRejectsStaleDirtyAndUnlockedBuilds(t *testing.T) {
	revision := strings.Repeat("a", 40)
	otherRevision := strings.Repeat("c", 40)
	hash := "sha256:" + strings.Repeat("b", 64)
	otherHash := "sha256:" + strings.Repeat("d", 64)
	validBuild := releaseExecutableBuildIdentity{PackagePath: "github.com/urfoundation/sn/sim-testnet", ModulePath: "github.com/urfoundation/sn", Revision: revision, Trimpath: true}
	validSource := releaseExecutableSourceIdentity{Revision: revision, PushedRevision: revision, SourceHash: hash, LockedSourceHash: hash}
	for _, test := range []struct {
		name   string
		build  releaseExecutableBuildIdentity
		source releaseExecutableSourceIdentity
	}{
		{name: "stale executable", build: releaseExecutableBuildIdentity{Revision: otherRevision, Trimpath: true}, source: validSource},
		{name: "modified executable", build: releaseExecutableBuildIdentity{Revision: revision, Modified: true, Trimpath: true}, source: validSource},
		{name: "non-release executable", build: releaseExecutableBuildIdentity{Revision: revision}, source: validSource},
		{name: "unpushed checkout", build: validBuild, source: releaseExecutableSourceIdentity{Revision: revision, PushedRevision: otherRevision, SourceHash: hash, LockedSourceHash: hash}},
		{name: "unlocked source", build: validBuild, source: releaseExecutableSourceIdentity{Revision: revision, PushedRevision: revision, SourceHash: hash, LockedSourceHash: otherHash}},
		{name: "missing locked source", build: validBuild, source: releaseExecutableSourceIdentity{Revision: revision, PushedRevision: revision, SourceHash: hash}},
	} {
		if err := validateReleaseExecutableIdentity(test.build, test.source, executableAttestationLockedSource); err == nil {
			t.Errorf("%s identity was accepted", test.name)
		}
	}
}

// A lock refresh may replace an old digest, but only from an exact clean build
// of the clean commit already represented by fetched origin/main.
func TestReleaseExecutableIdentityAllowsPushedSourceDuringLockRefresh(t *testing.T) {
	revision := strings.Repeat("a", 40)
	build := releaseExecutableBuildIdentity{Revision: revision, Trimpath: true}
	source := releaseExecutableSourceIdentity{Revision: revision, PushedRevision: revision, SourceHash: "sha256:" + strings.Repeat("b", 64), LockedSourceHash: "not-the-old-lock"}
	if err := validateReleaseExecutableIdentity(build, source, executableAttestationPushedSource); err != nil {
		t.Fatal(err)
	}
}

// Create a clean pushed fixture whose production hash can be locked without a
// network fetch. Tests update the local remote-tracking ref explicitly.
func testReleaseExecutableSourceFixture(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "checkout")
	remote := filepath.Join(base, "origin.git")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, base, "init", "--bare", "-q", remote)
	runTestGit(t, root, "init", "-q")
	runTestGit(t, root, "config", "user.email", "sim-testnet@example.invalid")
	runTestGit(t, root, "config", "user.name", "sim-testnet")
	runTestGit(t, root, "branch", "-M", "main")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/urfoundation/sn\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(sourcePath, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "go.mod", "main.go")
	runTestGit(t, root, "commit", "-qm", "release source")
	runTestGit(t, root, "config", "url.file://"+remote+".insteadOf", "git@github.com:urfoundation/sn.git")
	runTestGit(t, root, "remote", "add", "origin", "git@github.com:urfoundation/sn.git")
	runTestGit(t, root, "push", "-qu", "origin", "main")
	return root, sourcePath
}

// Reuse the fixture's reviewed fetched ref as its hermetic current-remote
// observation; dedicated tests below exercise drift between the two.
func testCurrentSNRevision(t *testing.T, root string) currentSNRevisionObserver {
	t.Helper()
	revision := strings.TrimSpace(string(testGitOutput(t, root, "rev-parse", "origin/main")))
	return func(context.Context) (string, error) {
		return revision, nil
	}
}

// Exercise the complete local source observer without reaching public GitHub.
func observeTestReleaseExecutableSource(t *testing.T, root string, cfg *ResolvedConfig, mode executableAttestationMode) (releaseExecutableSourceIdentity, error) {
	t.Helper()
	return observeReleaseExecutableSourceWithCurrent(context.Background(), cfg, mode, testCurrentSNRevision(t, root))
}

// The source observer rejects both tracked and untracked dirt, including files
// excluded from the production digest, before accepting the release lock.
func TestReleaseExecutableSourceRejectsTrackedAndUntrackedDirt(t *testing.T) {
	root, sourcePath := testReleaseExecutableSourceFixture(t)
	hash, err := goReleaseSourceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ResolvedConfig{Repos: RepoPaths{SN: root}, Release: &ReleaseLock{Repositories: map[string]any{"sn_go_source_hash": hash}}}
	if _, err := observeTestReleaseExecutableSource(t, root, cfg, executableAttestationLockedSource); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("package main\n\nvar dirty = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := observeTestReleaseExecutableSource(t, root, cfg, executableAttestationLockedSource); err == nil {
		t.Fatal("tracked source dirt was accepted")
	}
	if err := os.WriteFile(sourcePath, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	untrackedPath := filepath.Join(root, "operator-notes.txt")
	if err := os.WriteFile(untrackedPath, []byte("unreviewed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := observeTestReleaseExecutableSource(t, root, cfg, executableAttestationLockedSource); err == nil {
		t.Fatal("untracked checkout content was accepted")
	}
}

// A clean local commit remains unauthorized until the canonical remote-tracking
// branch names that exact revision.
func TestReleaseExecutableSourceRejectsUnpushedCommit(t *testing.T) {
	root, sourcePath := testReleaseExecutableSourceFixture(t)
	if err := os.WriteFile(sourcePath, []byte("package main\n\nvar next = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "main.go")
	runTestGit(t, root, "commit", "-qm", "local only")
	hash, err := goReleaseSourceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ResolvedConfig{Repos: RepoPaths{SN: root}, Release: &ReleaseLock{Repositories: map[string]any{"sn_go_source_hash": hash}}}
	source, err := observeTestReleaseExecutableSource(t, root, cfg, executableAttestationLockedSource)
	if err != nil {
		t.Fatal(err)
	}
	build := releaseExecutableBuildIdentity{Revision: source.Revision, Trimpath: true}
	if err := validateReleaseExecutableIdentity(build, source, executableAttestationLockedSource); err == nil || !strings.Contains(err.Error(), "pushed origin/main") {
		t.Fatalf("unpushed source error = %v", err)
	}
}

// A locally cached origin/main ref cannot stand in for the bounded live remote
// observation, even when its repository and branch names are canonical.
func TestPushedSNRevisionRejectsStaleFetchedRemoteTrackingRef(t *testing.T) {
	root, sourcePath := testReleaseExecutableSourceFixture(t)
	staleRevision := strings.TrimSpace(string(testGitOutput(t, root, "rev-parse", "origin/main")))
	if err := os.WriteFile(sourcePath, []byte("package main\n\nvar pushed = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "main.go")
	runTestGit(t, root, "commit", "-qm", "pushed update")
	runTestGit(t, root, "push", "-q", "origin", "main")
	currentRevision := strings.TrimSpace(string(testGitOutput(t, root, "rev-parse", "origin/main")))
	runTestGit(t, root, "update-ref", "refs/remotes/origin/main", staleRevision)
	if _, err := pushedSNRevision(context.Background(), root, func(context.Context) (string, error) { return currentRevision, nil }); err == nil || !strings.Contains(err.Error(), "differs from current GitHub branch") {
		t.Fatalf("stale fetched origin error = %v", err)
	}
}

// Cancellation reaches every Git observation, while the live branch query has
// its own short deadline so an unavailable forge cannot block an apply.
func TestPushedSNRevisionHonorsCancellationAndBoundedQuery(t *testing.T) {
	if currentSNMainQueryTimeout <= 0 || currentSNMainQueryTimeout > 10*time.Second {
		t.Fatalf("current SN main query timeout = %s", currentSNMainQueryTimeout)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := observeCurrentSNRevision(ctx); err == nil {
		t.Fatal("canceled current SN main query was accepted")
	}
}

// The public Git query accepts exactly one canonical main-ref advertisement.
func TestParseCurrentSNRevisionRejectsAmbiguousRemoteOutput(t *testing.T) {
	revision := strings.Repeat("a", 40)
	got, err := parseCurrentSNRevision([]byte(revision + "\trefs/heads/main\n"))
	if err != nil || got != revision {
		t.Fatalf("canonical current revision = %q, %v", got, err)
	}
	for _, output := range []string{
		"",
		revision + "\trefs/heads/main",
		revision + "\trefs/heads/main\r\n",
		revision + "\trefs/heads/main\n" + revision + "\trefs/heads/other\n",
		strings.Repeat("A", 40) + "\trefs/heads/main\n",
		revision + "\trefs/heads/release\n",
	} {
		if _, err := parseCurrentSNRevision([]byte(output)); err == nil {
			t.Errorf("ambiguous current revision output %q was accepted", output)
		}
	}
}

// Repository-local and inherited Git controls cannot redirect or weaken the
// independent HTTPS branch observation, while ordinary proxy settings remain.
func TestReleaseGitNetworkEnvironmentRemovesGitControls(t *testing.T) {
	environment := releaseGitNetworkEnvironment([]string{
		"PATH=/usr/bin",
		"HTTPS_PROXY=http://proxy.example",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=url.file:///attacker.insteadOf",
		"GIT_CONFIG_VALUE_0=https://github.com/",
		"GIT_EXEC_PATH=/attacker",
		"GIT_SSL_NO_VERIFY=true",
	})
	joined := "\n" + strings.Join(environment, "\n") + "\n"
	for _, forbidden := range []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=", "GIT_CONFIG_VALUE_0=", "GIT_EXEC_PATH=", "GIT_SSL_NO_VERIFY=true"} {
		if strings.Contains(joined, "\n"+forbidden) {
			t.Errorf("network environment retained %q: %v", forbidden, environment)
		}
	}
	for _, required := range []string{"PATH=/usr/bin", "HTTPS_PROXY=http://proxy.example", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0"} {
		if strings.Count(joined, "\n"+required+"\n") != 1 {
			t.Errorf("network environment count for %q is not one: %v", required, environment)
		}
	}
}

// Only the canonical project remote can supply the pushed source identity.
func TestReleaseExecutableSourceRejectsLookalikeOrigin(t *testing.T) {
	root, _ := testReleaseExecutableSourceFixture(t)
	runTestGit(t, root, "remote", "set-url", "origin", "git@github.com:attacker/sn.git")
	if _, err := pushedSNRevision(context.Background(), root, func(context.Context) (string, error) {
		t.Fatal("lookalike origin reached the public observer")
		return "", os.ErrInvalid
	}); err == nil || !strings.Contains(err.Error(), "github.com/urfoundation/sn") {
		t.Fatalf("lookalike origin error = %v", err)
	}
}

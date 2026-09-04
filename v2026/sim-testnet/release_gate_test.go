package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A sibling release module can advance while the long race and Solidity gates
// are running. The final check must cover every module hashed by the release
// lock and occur after all other checked-in gate work.
func TestLocalReleaseGateRechecksCompleteWorkspaceAtEnd(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	const repositories = "release_repos=(sn server operator-proxy connect sdk glog goidenticons proxy userwireguard vault xops config)"
	if !strings.Contains(script, repositories) || !strings.Contains(script, `for repo in "${release_repos[@]}"`) {
		t.Fatal("local release gate does not check every release repository")
	}
	if !strings.Contains(script, `git -C "$workspace/$repo" diff --check`) || !strings.Contains(script, `git -C "$workspace/$repo" diff --cached --check`) {
		t.Fatal("local release gate does not check both unstaged and staged patches")
	}
	if !strings.Contains(script, `"$workspace/server/connect/sim-latency/baseline/verify.sh"`) ||
		!strings.Contains(script, `go list ./... | grep -v '^github\.com/urnetwork/server/connect/sim-latency/baseline/'`) ||
		!strings.Contains(script, `go test "${server_packages[@]}" -run '^$'`) {
		t.Fatal("local release gate does not verify the immutable server baseline and compile every executable package")
	}
	// A mapfile process substitution masks go-list/grep failure even under
	// `set -euo pipefail`, potentially turning a broken package census into an
	// empty successful compile gate. Materialize and validate the pipeline
	// result before splitting it into the argv array.
	if strings.Contains(script, `mapfile -t server_packages < <(go list`) ||
		!strings.Contains(script, `server_package_list="$(go list ./... | grep -v '^github\.com/urnetwork/server/connect/sim-latency/baseline/')"`) ||
		!strings.Contains(script, `[[ -n "$server_package_list" ]]`) ||
		!strings.Contains(script, `mapfile -t server_packages <<<"$server_package_list"`) {
		t.Fatal("local release gate can mask a failed or empty executable server package census")
	}
	for _, required := range []string{
		"CoreStClient(BlockHashes|FinalizedHead|Epoch)",
		"StSyncChainEventsBatchesCanonicalEventBlocks",
		"StSyncChainEventsRejectsIncompleteCanonicalBatchBeforeMutation",
		"StAccountReconcile",
		"VerifySimulationAssignmentFilter(BlocksSeedPendingAndFutureAssignments|DoesNotAffectAnotherValidator)",
		"AuthNetworkClientFeedsConfiguredProxyEgressNamespace",
		"PaymentReconcile(SkipsStripeWithSKUOnlyVault|MalformedCredentialResourcesSkipAllStores)",
		"StripeReconcileCredentialsRequireNonblankAPIToken",
		"StTransactionIntentReservationUsesChainAccountNonceScope",
		"StTransactionAttemptCandidatesConvergeOnOneWinner",
		"StatsAlphaPriceURLIsMainnetOnly",
		"StatsGaugeVecReplaceDeletesStaleSeries",
		"go test -race ./monitor",
		"ParallelEvalCancellationJoinsAttemptWorker",
		"PlatformTransportCloseAndWaitJoinsPendingDial",
		"StreamReplacementReceiveDoesNotJoinAndPublishesAfterOldExit",
		"AddrGeneratorCloseJoinsBlockedProducer",
		"ClientCancelClosesContractManagerAdmission",
		"PeerConnectionResolveNetCancelsStunAndTurnLookups",
		"WebRtcPeerTeardownCancelsBlockedStunResolution",
		"PeerConnPionStartupAndTeardownAreSerialized",
		"WebRtcManagerCloseAndWaitReleasesOwnedResources",
		"WebRtcTestManagersHaveJoiningOwners",
		"P2pStreamProbeStreamSequenceCancelSynchronouslyWithdrawsReadiness",
		"ZZZNoPerInstanceLifecycleResidue",
		"BlockerGeneratedTables",
		"CfaaBlockedPrefix6Invariant",
		"ApiCloseAndWaitJoinsRefreshWorker",
		"NetworkSpaceCloseJoinsClientStrategyRelease",
		"NetworkSpaceManagerReplacementDoesNotHoldStateLockDuringClose",
		"NetworkSpaceManagerStaleRemovePreservesReplacement",
		"NetworkSpaceManagerStaleActiveSelectionPreservesReplacement",
		"NetworkSpaceCloseJoinsAsyncLocalStateAndRejectsLateJob",
		"NetworkSpaceManagerCloseRejectsRacingUpdate",
		"SimProviderDisconnectJoinsPendingTransportDial",
		"SimProviderCloseJoinsPendingTransportDial",
		"DeviceLocalProviderCloseAndWaitJoinsAdmittedMigration",
		"DeviceLocalCloseAndWaitJoinsOwnedApiRefresh",
		"DeviceLocalCloseAndWaitJoinsDestinationGeneration",
		"SecurityPolicyMonitorCloseAndWaitJoinsRun",
		"DeviceLocalRpcManagerCloseAndWaitJoinsAccept",
		"DeviceRemoteRpcCloseAndWaitJoinsAdmittedCallback",
		"DeviceRemoteCloseAndWaitJoinsBlockedDial",
		"DeviceRemoteSetRpcServerRejectsAfterClose",
		"RpcClientCallParentCancellationClosesTransport",
		"DeviceLocalAppliesApiRefreshAndLogout",
		"DeviceRemoteAppliesStandaloneApiRefreshAndLogout",
		"ApiSubnetHeadlessBindings",
		"ApiHeadlessAuthAndProviderBindings",
		"ApiSubnetPoolClaimEscapesNoUserInputIntoQuery",
		"RedeemBalanceCodeDecodeAndClassify",
		"Stripe.*",
		"sdk/build",
		"go test ./cmd/mobileexports -count=1",
		"go test -race ./cmd/mobileexports -count=1",
		"DatabaseTimeMatchesPostgresPrecision",
		"RouterRetainsDirectH3LoopbackSettings",
		"RouterDirectH3LoopbackCompletesHandshake",
		"TestConnectionVerifyEgressDisabledAvoidsVerifySettings",
		"TestConnectionVerifyEgressUsesControllerHashNamespace",
		"TestIncrementRateLimitWindowResetsAtExactBoundary",
		"TestClientDriverProbeMatchmakingUsesPoolIdentityAndQualitySpec",
		"TestPlatformPacketConnClampsQuic(SocketRequests|RequestToDeviceMemoryTarget)",
		"ProxyDeviceManagerSharesOneNetworkSpaceLifetime",
		"ProxyDeviceManagerCloseAndWaitJoinsOwnedNetworkSpace",
		"ProxyDeviceManagerCloseJoinsAdmittedOpenAndRejectsLateOpen",
		"ProxyDeviceManagerPreservesInjectedNetworkSpace",
		"WgClientStackCloseAndWaitJoinsTCPDispatcher",
		"TunnelAttemptCloseJoinsPumpBeforeGenerator",
		"TunnelAttemptCloseReleasesPartialConstruction",
		"go test -race ./connect",
		"go test -race ./connect/sim-latency",
		"export WARP_ENV=local",
		"export WARP_SERVICE=test",
		"export BRINGYOUR_POSTGRES_HOSTNAME=local-pg.bringyour.com",
		"export BRINGYOUR_REDIS_HOSTNAME=local-redis.bringyour.com",
		"go test -race ./controller -run \"$controller_db_tests\"",
		"go test -race ./model -run \"$model_db_tests\"",
		"go test -timeout 20m ./proxy -count=1",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("local release gate omits operator regression %s", required)
		}
	}
	databaseIndex := strings.Index(script, `if [[ "${RUN_SERVER_DB_TESTS:-0}" == "1" ]]`)
	for _, databaseTest := range []string{
		"StSyncChainEventsBatchesCanonicalEventBlocks",
		"StSyncChainEventsRejectsIncompleteCanonicalBatchBeforeMutation",
		"StAccountReconcile",
		"VerifySimulationAssignmentFilter(BlocksSeedPendingAndFutureAssignments|DoesNotAffectAnotherValidator)",
		"AuthNetworkClientFeedsConfiguredProxyEgressNamespace",
		"PaymentReconcile(SkipsStripeWithSKUOnlyVault|MalformedCredentialResourcesSkipAllStores)",
		"TestConnectionVerifyEgressUsesControllerHashNamespace",
		"TestIncrementRateLimitWindowResetsAtExactBoundary",
		"StTransactionIntentReservationUsesChainAccountNonceScope",
		"StTransactionAttemptCandidatesConvergeOnOneWinner",
	} {
		if testIndex := strings.Index(script, databaseTest); databaseIndex < 0 || testIndex <= databaseIndex {
			t.Errorf("database-backed operator regression %s is outside the isolated database gate", databaseTest)
		}
	}
	patchIndex := strings.LastIndex(script, `echo "[release-1.0] patch hygiene"`)
	lockIndex := strings.LastIndex(script, `go test ./sim-testnet -run '^TestReleaseLockMatchesCheckout$' -count=1`)
	passedIndex := strings.LastIndex(script, `echo "[release-1.0] local release gate passed"`)
	if patchIndex < 0 || lockIndex <= patchIndex || passedIndex <= lockIndex {
		t.Fatalf("final release-lock ordering patch=%d lock=%d passed=%d", patchIndex, lockIndex, passedIndex)
	}
}

// Keep enough deadline headroom for the complete launch-scale race suite. The
// three focused integrity shards exceed 62 minutes when serialized, before the
// rest of the package and concurrent live-campaign load. A 90-minute deadline
// retains deterministic headroom without changing test selection.
func TestLocalReleaseGateAllowsCompleteSimulatorRaceSuite(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scriptBytes), "go test -race -timeout 90m ./sim-testnet") {
		t.Fatal("local release gate lacks the reviewed 90-minute full simulator race deadline")
	}
}

// Keep operator-proxy checks inside the module directory and make every source
// gate explicit. Mere patch-hygiene coverage cannot prove that the independent
// module compiles, remains tidy, or passes its concurrent behavior suite.
func assertOperatorProxyReleaseGate(t *testing.T, scriptPath, heading string) {
	t.Helper()
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	const repositories = "release_repos=(sn server operator-proxy connect sdk glog goidenticons proxy userwireguard vault xops config)"
	if strings.Count(script, repositories) != 1 || !strings.Contains(script, `for repo in "${release_repos[@]}"`) {
		t.Fatalf("%s does not fence the complete release repository set", scriptPath)
	}
	start := strings.Index(script, heading)
	if start < 0 {
		t.Fatalf("%s has no operator-proxy gate", scriptPath)
	}
	section := script[start+len(heading):]
	if end := strings.Index(section, "\necho \""); end >= 0 {
		section = section[:end]
	}
	required := []string{
		`cd "$workspace/operator-proxy"`,
		"go mod tidy -diff",
		"go build ./...",
		"go vet ./...",
		`unformatted="$(gofmt -l .)"`,
		`if [[ -n "$unformatted" ]]`,
		"gofmt -d .",
		"go test -count=1 -timeout 20m ./...",
		"go test -race -count=1 -timeout 20m ./...",
	}
	previous := -1
	for _, command := range required {
		if strings.Count(section, command) != 1 {
			t.Errorf("%s operator-proxy gate has %d copies of %q", scriptPath, strings.Count(section, command), command)
			continue
		}
		index := strings.Index(section, command)
		if index <= previous {
			t.Errorf("%s operator-proxy gate orders %q before its prerequisite", scriptPath, command)
		}
		previous = index
	}
}

// Construct all twelve repositories and every classified Go module without
// making ordinary tests depend on the developer's current dirty worktree.
func releaseSourceFreezeFixture(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	remoteRoot := filepath.Join(workspace, ".release-remotes")
	if err := os.MkdirAll(remoteRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	var rewrites strings.Builder
	branches := map[string]string{
		"sn": "main", "server": "main", "operator-proxy": "main", "connect": "main",
		"sdk": "main", "glog": "master", "goidenticons": "main", "proxy": "main",
		"userwireguard": "master", "vault": "main", "xops": "main", "config": "main",
	}
	origins := map[string]string{
		"sn": "urfoundation/sn", "server": "urnetwork/server", "operator-proxy": "urnetwork/operator-proxy",
		"connect": "urnetwork/connect", "sdk": "urnetwork/sdk", "glog": "urnetwork/glog",
		"goidenticons": "urnetwork/goidenticons", "proxy": "urnetwork/proxy", "userwireguard": "urnetwork/userwireguard",
		"vault": "urnetwork/vault", "xops": "urnetwork/xops", "config": "urnetwork/config",
	}
	modules := []string{
		"connect", "glog", "goidenticons", "operator-proxy", "proxy", "sdk", "sdk/build",
		"sdk/cgo", "sdk/js", "server", "server/connect/sim-latency/baseline", "sn",
		"sn/third_party/npipe", "userwireguard", "xops/echo", "xops/router",
	}
	for repo, branch := range branches {
		root := filepath.Join(workspace, repo)
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, module := range modules {
			if module != repo && !strings.HasPrefix(module, repo+"/") {
				continue
			}
			moduleRoot := filepath.Join(workspace, filepath.FromSlash(module))
			if err := os.MkdirAll(moduleRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			moduleName := "example.invalid/" + strings.ReplaceAll(module, "/", "-")
			if err := os.WriteFile(filepath.Join(moduleRoot, "go.mod"), []byte("module "+moduleName+"\n\ngo 1.24\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if repo == "server" {
			archiveRoot := filepath.Join(root, "connect", "sim-latency", "baseline")
			readme := "Preserved measurement inputs are Go test source files.\nThe module file is covered by the manifest.\n"
			if err := os.WriteFile(filepath.Join(archiveRoot, "README.md"), []byte(readme), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(archiveRoot, "MANIFEST.sha256"), []byte("fixture\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(archiveRoot, "verify.sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		runTestGit(t, root, "init", "-q", "-b", branch)
		runTestGit(t, root, "config", "user.email", "sim-testnet@example.invalid")
		runTestGit(t, root, "config", "user.name", "sim-testnet")
		runTestGit(t, root, "add", ".")
		runTestGit(t, root, "commit", "-qm", "fixture source")
		originURL := "git@github.com:" + origins[repo] + ".git"
		if repo == "server" {
			originURL = "https://github.com/" + origins[repo] + ".git"
		}
		runTestGit(t, root, "config", "remote.origin.url", originURL)
		runTestGit(t, root, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
		runTestGit(t, root, "update-ref", "refs/remotes/origin/"+branch, "HEAD")
		runTestGit(t, root, "config", "branch."+branch+".remote", "origin")
		runTestGit(t, root, "config", "branch."+branch+".merge", "refs/heads/"+branch)
		bareRoot := filepath.Join(remoteRoot, repo+".git")
		command := exec.Command("git", "clone", "--bare", "-q", root, bareRoot)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("create %s fixture remote: %v\n%s", repo, err, output)
		}
		fmt.Fprintf(&rewrites, "[url \"file://%s\"]\n\tinsteadOf = %s\n", filepath.ToSlash(bareRoot), originURL)
	}
	gitConfig := filepath.Join(workspace, ".release-fixture.gitconfig")
	if err := os.WriteFile(gitConfig, []byte(rewrites.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return workspace
}

// The freeze command accepts a complete clean fixture and emits an exact
// twelve-repository revision/upstream snapshot.
func TestReleaseSourceFreezeRecordsCompleteCleanWorkspace(t *testing.T) {
	workspace := releaseSourceFreezeFixture(t)
	command := exec.Command("../scripts/check-release-source-freeze.sh", workspace)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("clean source freeze: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 12 {
		t.Fatalf("source freeze recorded %d repositories, want 12: %s", len(lines), output)
	}
	expectedOrigins := map[string]string{
		"sn": "github.com/urfoundation/sn/v2026", "server": "github.com/urnetwork/server/v2026", "operator-proxy": "github.com/urnetwork/operator-proxy/v2026",
		"connect": "github.com/urnetwork/connect/v2026", "sdk": "github.com/urnetwork/sdk/v2026", "glog": "github.com/urnetwork/glog/v2026",
		"goidenticons": "github.com/urnetwork/goidenticons/v2026", "proxy": "github.com/urnetwork/proxy/v2026", "userwireguard": "github.com/urnetwork/userwireguard/v2026",
		"vault": "github.com/urnetwork/vault", "xops": "github.com/urnetwork/xops", "config": "github.com/urnetwork/config",
	}
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(fields[1]) ||
			!strings.HasPrefix(fields[2], "origin/") || fields[3] != expectedOrigins[fields[0]] {
			t.Errorf("non-canonical source freeze record %q", line)
		}
	}
}

// Cleanliness and provenance must be established before a tracked verifier can
// execute; otherwise a dirty checkout could run arbitrary code before failing.
func TestReleaseSourceFreezeRejectsDirtyVerifierBeforeExecution(t *testing.T) {
	workspace := releaseSourceFreezeFixture(t)
	marker := filepath.Join(workspace, "verifier-executed")
	verifier := filepath.Join(workspace, "server", "connect", "sim-latency", "baseline", "verify.sh")
	body := "#!/usr/bin/env bash\nset -euo pipefail\ntouch \"" + marker + "\"\n"
	if err := os.WriteFile(verifier, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("../scripts/check-release-source-freeze.sh", workspace).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "tracked, staged, or untracked changes: server") {
		t.Fatalf("dirty verifier was accepted: %v\n%s", err, output)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("dirty verifier executed before rejection: %v", statErr)
	}
}

// Indexed arrays and lookup functions retain compatibility with the Bash 3.2
// still shipped by some otherwise supported checkout hosts.
func TestReleaseSourceFreezeDoesNotRequireAssociativeArrays(t *testing.T) {
	script, err := os.ReadFile("../scripts/check-release-source-freeze.sh")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(script), "declare -A") {
		t.Fatal("source-freeze gate requires Bash associative arrays")
	}
}

// A local origin/* ref cannot establish provenance when the remote itself was
// replaced; require the exact reviewed GitHub repository after normalization.
func TestReleaseSourceFreezeRejectsWrongOrigin(t *testing.T) {
	workspace := releaseSourceFreezeFixture(t)
	serverRoot := filepath.Join(workspace, "server")
	runTestGit(t, serverRoot, "config", "remote.origin.url", "git@github.com:attacker/server.git")
	output, err := exec.Command("../scripts/check-release-source-freeze.sh", workspace).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "origin github.com/attacker/server, want github.com/urnetwork/server") {
		t.Fatalf("wrong repository origin was accepted: %v\n%s", err, output)
	}
}

// A cached origin ref is not proof that the release checkout matches GitHub.
// Advance the fixture remote without updating the release checkout and require
// the source-freeze check to fetch and reject the stale local revision.
func TestReleaseSourceFreezeRefreshesRemoteBeforeAcceptingRevision(t *testing.T) {
	workspace := releaseSourceFreezeFixture(t)
	advanceRoot := filepath.Join(t.TempDir(), "config-advance")
	command := exec.Command("git", "clone", "-q", "git@github.com:urnetwork/config.git", advanceRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("clone fixture config remote: %v\n%s", err, output)
	}
	runTestGit(t, advanceRoot, "config", "user.email", "sim-testnet@example.invalid")
	runTestGit(t, advanceRoot, "config", "user.name", "sim-testnet")
	if err := os.WriteFile(filepath.Join(advanceRoot, "remote.yml"), []byte("advanced\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, advanceRoot, "add", "remote.yml")
	runTestGit(t, advanceRoot, "commit", "-qm", "advance authoritative remote")
	runTestGit(t, advanceRoot, "push", "-q", "origin", "HEAD:refs/heads/main")
	output, err := exec.Command("../scripts/check-release-source-freeze.sh", workspace).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "config revision") || !strings.Contains(string(output), "differs from origin/main") {
		t.Fatalf("remote-ahead repository was accepted: %v\n%s", err, output)
	}
}

// Module census follows reviewed Git source, not ignored runtime/build trees
// that can contain generated go.mod files without changing a release commit.
func TestReleaseSourceFreezeIgnoresIgnoredRuntimeModules(t *testing.T) {
	workspace := releaseSourceFreezeFixture(t)
	serverRoot := filepath.Join(workspace, "server")
	runTestGit(t, serverRoot, "config", "core.excludesFile", filepath.Join(serverRoot, ".git", "release-test-excludes"))
	if err := os.WriteFile(filepath.Join(serverRoot, ".git", "release-test-excludes"), []byte("runtime/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(serverRoot, "runtime")
	if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, "go.mod"), []byte("module example.invalid/generated\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("../scripts/check-release-source-freeze.sh", workspace).CombinedOutput(); err != nil {
		t.Fatalf("ignored generated module changed the tracked release census: %v\n%s", err, output)
	}
}

// Reproduce the former patch-hygiene gap for both untracked and ordinary
// tracked changes, then prove a clean commit still fails when upstream lags.
func TestReleaseSourceFreezeRejectsDirtyUntrackedAndUnsynchronizedRepositories(t *testing.T) {
	workspace := releaseSourceFreezeFixture(t)
	script := "../scripts/check-release-source-freeze.sh"
	run := func() (string, error) {
		output, err := exec.Command(script, workspace).CombinedOutput()
		return string(output), err
	}
	untracked := filepath.Join(workspace, "config", "untracked.yml")
	if err := os.WriteFile(untracked, []byte("unreviewed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := run(); err == nil || !strings.Contains(output, "tracked, staged, or untracked changes: config") {
		t.Fatalf("untracked source was accepted: %v\n%s", err, output)
	}
	if err := os.Remove(untracked); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(workspace, "config", "README.md")
	if err := os.WriteFile(readme, []byte("modified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := run(); err == nil || !strings.Contains(output, "tracked, staged, or untracked changes: config") {
		t.Fatalf("tracked source was accepted: %v\n%s", err, output)
	}
	if err := os.WriteFile(readme, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "config", "reviewed.yml"), []byte("reviewed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, filepath.Join(workspace, "config"), "add", "reviewed.yml")
	runTestGit(t, filepath.Join(workspace, "config"), "commit", "-qm", "advance without upstream")
	if output, err := run(); err == nil || !strings.Contains(output, "differs from origin/main") {
		t.Fatalf("unsynchronized source was accepted: %v\n%s", err, output)
	}
}

// Any new go.mod must be deliberately classified as buildable or archived;
// an unreviewed nested module cannot silently escape tidy verification.
func TestReleaseSourceFreezeRejectsUnclassifiedGoModule(t *testing.T) {
	workspace := releaseSourceFreezeFixture(t)
	serverRoot := filepath.Join(workspace, "server")
	moduleRoot := filepath.Join(serverRoot, "new-module")
	if err := os.Mkdir(moduleRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleRoot, "go.mod"), []byte("module example.invalid/new-module\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, serverRoot, "add", "new-module/go.mod")
	runTestGit(t, serverRoot, "commit", "-qm", "add unclassified module")
	runTestGit(t, serverRoot, "update-ref", "refs/remotes/origin/main", "HEAD")
	runTestGit(t, serverRoot, "push", "-q", "--force", "origin", "HEAD:refs/heads/main")
	output, err := exec.Command("../scripts/check-release-source-freeze.sh", workspace).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "module inventory differs") || !strings.Contains(string(output), "server/new-module") {
		t.Fatalf("unclassified Go module was accepted: %v\n%s", err, output)
	}
}

// The only non-tidy module is a manifest-authenticated archive whose README
// explains why preserved patched test inputs cannot resolve independently.
func TestReleaseSourceFreezeExceptionIsAuthenticatedHistoricalArchive(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/check-release-source-freeze.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	if !strings.Contains(script, "archived_modules=(server/connect/sim-latency/baseline)") ||
		!strings.Contains(script, `"$archive_root/verify.sh"`) ||
		!strings.Contains(script, "GOWORK=off go mod tidy -diff") {
		t.Fatal("source freeze does not separate the authenticated archive from every live tidy module")
	}
	readme, err := os.ReadFile("../../server/connect/sim-latency/baseline/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "measurement inputs are Go test source files") || !strings.Contains(string(readme), "module file is covered by the manifest") {
		t.Fatal("sim-latency archive no longer documents its non-buildable authenticated boundary")
	}
	manifest, err := os.ReadFile("../../server/connect/sim-latency/baseline/MANIFEST.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^[0-9a-f]{64}  go\.mod$`).Match(manifest) {
		t.Fatal("sim-latency archive manifest does not authenticate its Go module boundary")
	}
}

// Both release entry points validate a clean source snapshot before testing
// and compare a second full snapshot only after the release-lock check.
func TestReleaseGatesBracketWorkWithExactSourceFreezeSnapshots(t *testing.T) {
	for _, scriptPath := range []string{"../scripts/test-release-1.0-local.sh", "../scripts/test-release-1.0-producer-gate.sh"} {
		scriptBytes, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatal(err)
		}
		script := string(scriptBytes)
		if strings.Count(script, "check-release-source-freeze.sh") != 2 ||
			!strings.Contains(script, `if [[ "$final_release_source_snapshot" != "$release_source_snapshot" ]]`) {
			t.Errorf("%s does not bracket work with exact source snapshots", scriptPath)
		}
		lockIndex := strings.LastIndex(script, "TestReleaseLockMatchesCheckout")
		freezeIndex := strings.LastIndex(script, "check-release-source-freeze.sh")
		passedIndex := strings.LastIndex(script, "gate passed")
		if lockIndex < 0 || freezeIndex <= lockIndex || passedIndex <= freezeIndex {
			t.Errorf("%s terminal source-freeze order lock=%d freeze=%d pass=%d", scriptPath, lockIndex, freezeIndex, passedIndex)
		}
	}
}

// Go decision models are useful supplements, but they cannot prove what the
// deployed FRAME runtime contains. Require both release entry points to hash
// the exact reviewed upstream Rust sources and regression tests at the tag's
// immutable commit before any local gate work begins.
func TestReleaseGatesAttestPinnedRuntime453RustSource(t *testing.T) {
	manifestBytes, err := os.ReadFile("../docs/spec/runtime-v453-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(manifestBytes)), "\n")
	if len(rows) != 12 {
		t.Fatalf("runtime 453 source manifest has %d rows, want 12", len(rows))
	}
	requiredPaths := map[string]bool{
		"pallets/drand/src/tests.rs":                   false,
		"pallets/drand/src/verifier.rs":                false,
		"pallets/proxy/src/lib.rs":                     false,
		"pallets/proxy/src/tests.rs":                   false,
		"pallets/subtensor/src/macros/dispatches.rs":   false,
		"pallets/subtensor/src/staking/stake_utils.rs": false,
		"pallets/subtensor/src/subnets/subnet.rs":      false,
		"pallets/subtensor/src/tests/move_stake.rs":    false,
		"pallets/subtensor/src/tests/networks.rs":      false,
		"precompiles/src/balance_transfer.rs":          false,
		"runtime/src/lib.rs":                           false,
		"runtime/tests/precompiles.rs":                 false,
	}
	for _, row := range rows {
		fields := strings.Fields(row)
		if len(fields) != 2 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(fields[0]) {
			t.Fatalf("non-canonical runtime 453 source row %q", row)
		}
		if _, ok := requiredPaths[fields[1]]; !ok {
			t.Fatalf("unexpected runtime 453 source path %q", fields[1])
		}
		requiredPaths[fields[1]] = true
	}
	for path, present := range requiredPaths {
		if !present {
			t.Errorf("runtime 453 source manifest omits %s", path)
		}
	}

	checkerBytes, err := os.ReadFile("../scripts/check-runtime-v453-source.sh")
	if err != nil {
		t.Fatal(err)
	}
	checker := string(checkerBytes)
	for _, required := range []string{
		"https://github.com/RaoFoundation/subtensor",
		"https://raw.githubusercontent.com/RaoFoundation/subtensor",
		"v453",
		"823bdcbc58a29f60b243be4737a7c72b34ac7d93",
		"runtime-v453-source.sha256",
		"git ls-remote --tags",
		"SUBTENSOR_RUNTIME_SOURCE",
		"status --porcelain=v1 --untracked-files=all",
		"sha256sum",
	} {
		if !strings.Contains(checker, required) {
			t.Errorf("runtime source checker omits %q", required)
		}
	}

	for _, scriptPath := range []string{"../scripts/test-release-1.0-local.sh", "../scripts/test-release-1.0-producer-gate.sh"} {
		scriptBytes, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatal(err)
		}
		script := string(scriptBytes)
		attestation := strings.Index(script, "check-runtime-v453-source.sh")
		preflight := strings.Index(script, "check-release-source-freeze.sh")
		if strings.Count(script, "check-runtime-v453-source.sh") != 1 || attestation <= preflight {
			t.Errorf("%s does not attest runtime 453 immediately after source-freeze preflight", scriptPath)
		}
	}
}

// Operator-proxy CI builds only against the reviewed sibling commits and uses
// tidy's read-only diff mode instead of mutating its checkout before testing.
func TestOperatorProxyCIPinsCurrentSiblingRevisions(t *testing.T) {
	workflow, err := os.ReadFile("../../operator-proxy/.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, revision := range []string{"9ac9a96c96f5e3c2d7fd6e928b28b471567d0a54", "2bdcce5f8be023947f26a247eb5665c56b69b2e3"} {
		if strings.Count(text, revision) != 2 {
			t.Errorf("operator-proxy CI revision %s appears %d times, want checkout and assertion", revision, strings.Count(text, revision))
		}
	}
	if !strings.Contains(text, "run: go mod tidy -diff") || strings.Contains(text, "git diff --exit-code go.mod go.sum") {
		t.Fatal("operator-proxy CI does not use an immutable tidy check")
	}
}

func TestLocalReleaseGateCoversCompleteOperatorProxyModule(t *testing.T) {
	assertOperatorProxyReleaseGate(t, "../scripts/test-release-1.0-local.sh", `echo "[release-1.0] operator-proxy module"`)
}

func TestProducerReleaseGateCoversCompleteOperatorProxyModule(t *testing.T) {
	assertOperatorProxyReleaseGate(t, "../scripts/test-release-1.0-producer-gate.sh", `echo "[release-1.0 producer] operator-proxy source and behavior"`)
}

func TestSolidityStaticGateCoversEveryDeployedRoot(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-solidity-static.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, root := range []string{
		"src/STCoordinator.sol",
		"src/STFleetBatcher.sol",
		"src/probe/STSubnetProbe.sol",
		"src/testnet/STCoordinatorAdversary.sol",
	} {
		if strings.Count(script, root) != 1 {
			t.Errorf("deployable Solidity root %s appears %d times in the static gate", root, strings.Count(script, root))
		}
	}
	if !strings.Contains(script, "all ${#contracts[@]} deployable roots") {
		t.Fatal("Solidity static gate does not report its complete deployed-root count")
	}
}

// Keep the live launch path bounded without weakening acceptance. This gate
// exercises every mutable evidence producer and the closed-capture boundary;
// expensive semantic reconstruction and broad suites belong to the concurrent
// offline gate in test-release-1.0-local.sh.
func TestProducerGateSeparatesCaptureFromOfflineAnalysis(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, required := range []string{
		"go test ./validator ./sim-testnet -run '^$'",
		"Attempt|Deposited|ReleaseMeasurement|IntentStore|SteeringIntent|MeasurementStats|ExactPoolQuality|ReleaseSteeringLoop",
		"FinalCollected(Bundle|File|Chain)|FinalSemantic(PublicCapture|LaunchFoundation)",
		"ScenarioProcessLogGate|ReleaseAndProductionScenariosRequireProcessLogGate|ScenarioCompletion",
		"CampaignEvidence|DirectScenarioCompletion|EvidenceFileHashes",
		"ExactReleaseCampaignGate|ScenarioCampaignAttempt|ProductionHandoff|InitialScenarioFailure|ProductionPolicyEvidence",
		"PrepareSignedAttemptStateNamespace|ClassifyValidatorAttemptState",
		"go test -race ./validator",
		"go test -race ./sim-testnet",
		"forge test --summary",
		"go run ./sim-testnet/gencontracts --check",
		"./stabi/generate.sh --check",
		"go test ./st ./startifact",
		"VerifyController(FullTrailFlow|PoisonAndFailurePaths",
		"StSyncChainEventsBatchesCanonicalEventBlocks",
		"StTransactionIntentReservationUsesChainAccountNonceScope",
		"go test ./sim-testnet -run '^TestReleaseLockMatchesCheckout$'",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("launch-critical producer gate omits %s", required)
		}
	}
	for _, deferred := range []string{
		"go test ./...",
		"ProduceFinalSemanticOutputs",
		"FinalSemanticEvidenceBuild",
		"go test -race -timeout 90m ./sim-testnet",
		"test-solidity-static.sh",
	} {
		if strings.Contains(script, deferred) {
			t.Errorf("offline acceptance work %s leaked into the launch-critical gate", deferred)
		}
	}
	lockIndex := strings.LastIndex(script, "TestReleaseLockMatchesCheckout")
	passedIndex := strings.LastIndex(script, "launch-critical gate passed")
	if lockIndex < 0 || passedIndex <= lockIndex {
		t.Fatalf("producer gate is not release-lock fenced: lock=%d pass=%d", lockIndex, passedIndex)
	}
}

func assertReleaseGateAssignmentFilterProfile(t *testing.T, scriptPath, gate string) {
	t.Helper()
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	profileIndex := strings.Index(script, "export WARP_ENV=local")
	if profileIndex < 0 {
		t.Fatalf("%s gate has no isolated database profile", gate)
	}
	beforeProfile, databaseProfile := script[:profileIndex], script[profileIndex:]
	for _, name := range []string{
		"VerifySimulationAssignmentFilter(BlocksSeedPendingAndFutureAssignments|DoesNotAffectAnotherValidator)",
		"StSyncChainEventsBatchesCanonicalEventBlocks",
		"StTransactionIntentReservationUsesChainAccountNonceScope",
	} {
		if strings.Contains(beforeProfile, name) || !strings.Contains(databaseProfile, name) {
			t.Errorf("database-backed %s test %s is not confined to the isolated profile", gate, name)
		}
	}
	match := regexp.MustCompile(`(?m)^\s*filter_pure_tests='([^'\n]+)'\s*$`).FindStringSubmatch(script)
	if len(match) != 2 {
		t.Fatalf("%s gate has no explicit pure assignment-filter selector", gate)
	}
	selector, err := regexp.Compile(match[1])
	if err != nil {
		t.Fatalf("compile pure assignment-filter selector: %v", err)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "server", "controller", "*_test.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("enumerate operator controller tests: files=%d err=%v", len(files), err)
	}
	testName := regexp.MustCompile(`(?m)^func (TestVerifySimulationAssignmentFilter[[:alnum:]_]+)\(`)
	selected := 0
	for _, file := range files {
		raw, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, found := range testName.FindAllSubmatch(raw, -1) {
			if !selector.Match(found[1]) {
				continue
			}
			selected++
			if strings.HasSuffix(file, "_db_test.go") {
				t.Errorf("pre-profile selector admits database test %s from %s", found[1], file)
			}
		}
	}
	if selected < 10 {
		t.Fatalf("pure assignment-filter coverage unexpectedly shrank to %d tests", selected)
	}
}

func TestProducerGateDoesNotSelectDatabaseTestsBeforeItsIsolatedProfile(t *testing.T) {
	assertReleaseGateAssignmentFilterProfile(t, "../scripts/test-release-1.0-producer-gate.sh", "producer")
}

func TestLocalGateDoesNotSelectDatabaseTestsBeforeItsIsolatedProfile(t *testing.T) {
	assertReleaseGateAssignmentFilterProfile(t, "../scripts/test-release-1.0-local.sh", "local")
}

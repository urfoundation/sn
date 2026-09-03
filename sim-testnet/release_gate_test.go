package main

import (
	"os"
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
// measured baseline exceeded 9m40s before the policy-migration regressions were
// added, so restoring Go's 10-minute default would deterministically truncate
// required coverage on this release host.
func TestLocalReleaseGateAllowsCompleteSimulatorRaceSuite(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scriptBytes), "go test -race -timeout 15m ./sim-testnet") {
		t.Fatal("local release gate lacks the reviewed 15-minute full simulator race deadline")
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
		"go mod tidy",
		"git diff --exit-code -- go.mod go.sum",
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
		"go test -race -timeout 15m ./sim-testnet",
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

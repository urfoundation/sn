package main

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const releaseConnectPolicySelector = "^Test(BlockerGeneratedTables|BlockerDefaultDataSmoke|BlockerDataGuards|BlockerHashVectors|BlockerZeroAlloc|BlockerFalsePositiveProbe|BlockerIp4|BlockerIp6|BlockerToggleRace|CfaaPortClassification|CfaaBlockedIps|CfaaDisabled|CfaaIngressMirrorsSourceDrops|CfaaBlockedPrefixInvariant|CfaaBlockedIp4BruteForce|CfaaBlockedIp4ZeroAlloc|CfaaSearch6|CfaaInspectV6|CfaaBlockedPrefix6Invariant|CfaaInspectIcmp|TelegramCallReflectorRanges|TelegramCallV12TcpFallback|CfaaTelegramCallException|SecurityPolicyAllowsTelegramCallReflectors)$"

const releaseConnectP2PSignalSelector = "^Test(WebRtc|WebRtcMessageRoundTrip|P2pTransportAutoSelectsFastPathForCapablePeer|SignalPipeDropsBeforeDestinationRegistration|DelayedSignalPipe(ResolvesDestinationAtDispatch|DropsMissingDestinationAtDispatch|CancellationReturnsOwnedFrames|FullQueueDoesNotBlockDispatch|CancellationUnblocksCapacitySender)|MatchExpectedUnorderedP2pMessages(AcceptsPermutation|RejectsInvalidContent))$"

const releaseParserFramingSelector = "^Test((Policy|ReleaseConfig|ClaimDaemonConfig|StrictYAML|RenderedValidatorPolicy)RejectsMalformedTrailingYAML|FinalSemanticPathProofArtifact(Count|RejectsMalformedTrailingJSON)|ParseFleetManifestStrictCanonicalRoundTrip|PolicyStrictAndFailClosed|LoadReleaseConfigStrictAndNormalizesOperatorSecrets|ClaimDaemonConfigStrictAndPortable|StrictYAMLRejectsUnknownAndMultipleDocuments|ReleaseGatesPinProviderAndTransportRegressions)$"

// Both gates certify the real modules whose upstream changes affect traffic,
// token lifetime, per-provider accounting, and strict input framing before an
// operator can publish.
func TestReleaseGatesPinProviderAndTransportRegressions(t *testing.T) {
	groups := []struct {
		variable string
		selector string
		pkg      string
	}{
		{variable: "payout_allocation_tests", selector: "^Test(EvenContractPayoutShare|AllocateContractParticipantPayouts|AllocateContractParticipantPayoutEligibilityMatrix)$", pkg: "./model"},
		{variable: "provider_attribution_tests", selector: "^Test(ContractPayout|CompanionContractPayout|ContractParticipant|StEpochProviderUsage|StatsProviderPayouts|StatsProviders|StatsQueryPlans|SearchProviderStatsRollup|RemoveOldSearchProviderStats|RemoveOldVerifyProviderStats)", pkg: "./model"},
		{variable: "transport_identity_tests", selector: "^Test(PlatformTransportAuthSnapshotsAreAtomicAndOwned|PlatformTransportH[13]ReconnectUsesUpdatedAuthSnapshot|TunTcpInboundFlowUsesStableBoundedShards|TunTcpInboundShardHandoffCadenceIsBounded|TunWriteCompletesFiniteTcpInboundHandoffBeforeReturn|TunWriteRetainsTcpInboundYieldCadence|TunWriteBatchFinishesEveryTcpInboundHandoff)$", pkg: "."},
		{variable: "token_transport_tests", selector: "^Test(ApiTokenManager|DeviceRemoteRpcPublicationWakesOnlyOutstandingRefresh|ApiCloseAndWaitJoinsRefreshWorker|DeviceLocalAppliesApiRefreshAndLogout|DeviceRemoteAppliesStandaloneApiRefreshAndLogout)", pkg: "."},
		{variable: "provider_input_tests", selector: "^Test(StCanonicalProviderUsages|StBuildReleaseProviderInputs)", pkg: "./controller"},
		{variable: "test_env_fail_fast_tests", selector: "^Test(DefaultTestEnvReleaseFailFast|RunRetriesUntilPass|RunFailsAfterExhaustion|RunReportsPanicOriginAfterExhaustion)", pkg: "."},
		{variable: "parser_framing_tests", selector: releaseParserFramingSelector, pkg: "./protocol ./miner ./validator ./sim-testnet"},
	}
	for _, path := range []string{"../scripts/test-release-1.0-producer-gate.sh", "../scripts/test-release-1.0-local.sh"} {
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		script := string(value)
		strictExport := "export WARP_TEST_ENV_FAIL_FAST=1"
		if strings.Count(script, strictExport) != 1 || strings.Index(script, strictExport) > strings.Index(script, "go test ") {
			t.Fatalf("%s must export strict default TestEnv behavior before its first test", path)
		}
		for _, group := range groups {
			selector, err := releaseConnectPolicySelectorAssignment(script, group.variable)
			if err != nil || selector != group.selector {
				t.Fatalf("%s %s selector = %q, error %v; want %q", path, group.variable, selector, err, group.selector)
			}
			for _, mode := range []string{"", "-race "} {
				command := fmt.Sprintf("go test %s%s -run \"$%s\" -count=1", mode, group.pkg, group.variable)
				if strings.Count(script, command) != 1 {
					t.Fatalf("%s does not execute exactly one %s", path, command)
				}
			}
		}
	}
	// These complete source groups prevent a future test rename from silently
	// escaping the launch selection while its underlying behavior still ships.
	for _, source := range []struct {
		path     string
		selector string
	}{
		{path: "../../server/model/contract_provider_attribution_test.go", selector: groups[1].selector},
		{path: "../../server/model/provider_payout_attribution_test.go", selector: groups[1].selector},
		{path: "../../server/model/provider_model_test.go", selector: groups[1].selector},
		{path: "../../server/model/provider_stats_plan_test.go", selector: groups[1].selector},
		{path: "../../connect/transport_auth_test.go", selector: groups[2].selector},
		{path: "../../sdk/device_token_manager_transport_test.go", selector: groups[3].selector},
		{path: "../../server/controller/st_payout_canonical_test.go", selector: groups[4].selector},
	} {
		value, err := os.ReadFile(source.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyReleaseSourceTestCoverage(source.selector, "^Test", []string{string(value)}); err != nil {
			t.Fatalf("%s: %v", source.path, err)
		}
	}
	// Require each affected decoder's regression independently of the gate's
	// selectable names; the dedicated harness source includes both entry points.
	for _, source := range []struct {
		path, required string
	}{
		{path: "../protocol/policy_test.go", required: "^TestPolicyRejectsMalformedTrailingYAML$"},
		{path: "../validator/config_test.go", required: "^TestReleaseConfigRejectsMalformedTrailingYAML$"},
		{path: "../miner/claim_daemon_test.go", required: "^TestClaimDaemonConfigRejectsMalformedTrailingYAML$"},
		{path: "trailing_yaml_test.go", required: "^Test"},
	} {
		value, err := os.ReadFile(source.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyReleaseSourceTestCoverage(releaseParserFramingSelector, source.required, []string{string(value)}); err != nil {
			t.Fatalf("%s: %v", source.path, err)
		}
	}
}

const releaseAdversarialSelector = "^Test(Adversarial|Adversary|VerifyAdversary|RPCAdversary|ConsensusWeightComparison|Runtime454)"

const releaseRuntimeClientSelector = "^Test(DialChainContext|FinalizedHeadContext|FinalizedBlock|BlockHashContext|BlockIdentityCache|ExactBlockIdentity|AccountNonceContext|ReleaseStateReaders|ReleaseExactBlock|ReleaseSnapshot|ReleaseSteeringSource|VerifyFinalizedExtrinsicContext|LocateFinalizedExtrinsic|FleetCommitmentAtContext|FleetCommitmentInfoRuntime|RuntimeArtifactMetadata|RuntimeMetadataAtContext|FleetRuntime|FleetFinalizedRuntime|BindFleetRuntime|DialFleetNativeContext|ReleaseEpochStartBlockAtContext|ReleaseConfigRequiresExactNativeRuntimeIdentity|InitialReleaseSnapshot|AuthenticatePinnedNativeRuntime|ReleaseNativeEndpointTimeout)"

const releaseSyntheticEVMIdentitySelector = "^Test(WaitFinalized|EVMBlockIdentity|ClaimReceiptIdentity|FinalizedClaimReceipt|UncertainClaimRetryable|SyntheticEVM|EthEVMBlockReader|EVMFinality|FinalizedEVMHead|BoundFinalizedEVMHead|ReceiptRequiresCanonicalHashAndFinalizedHeight|ProducerGatePinsSyntheticEVMIdentityRegressions)"

const releaseSemanticIntegritySelector = "^Test(FinalNative|FinalPublicNative|FinalSemanticFleetAudit|FinalPublicFleetAudit|FinalSemanticVault|FinalSemanticCycleConviction|FinalSemanticCoordinatorRuntime|FinalSemanticCoordinatorUpgrade|FinalClaimPaymentLedger|FinalSemanticReceiptPayload|PublicFinalSemantic|FinalSemanticPoolOperatorVersion|FinalSemanticEpochDeposit|FinalPublicChainVerificationRejectsV2ReceiptOnlyTranscript|FinalSemanticDishonestDepositReceiptPayload|FinalSemanticEvidenceBuildRenderAndArtifacts|FinalSemanticArtifactVerificationCache|FinalSemanticFixture|FinalFleetLifecycle|FinalSemanticFleetByUIDAt|FinalPayoutAssignmentsAt|FinalPayoutArtifact|FinalSemanticDeployment|FinalSemanticBuilder|FinalSemanticPoolRegistration|FinalSemantic(Pool|Head|Validator)UIDZero|FinalFleetGeneration|FinalSemanticHistorical|FinalSemanticEvidenceFailsClosed|FinalSemanticPathProofArtifact|FinalSemanticPoolAuditDistinguishesUnderpaymentFromRecovery|FinalSemanticDishonestDepositDecisionsAndPublicReplay|FinalSemanticSettlementAccountingBindsBothHeadsAndEventDeltas|FinalSemanticCarryModelFailsClosedOnAdjacentAccountingErrors|FinalPublicChainVerificationRequiresTwoCanonicalOperatorOrigins|PublicScenarioBundle|SemanticMismatchBranches|StateMismatchError|FinalEVMLogQueryRanges|FinalCollectedCoordinatorBaselines|ReleaseHistoryRuntimeArtifacts|ProducerGatePinsCompleteAdversarialRegressions|ProducerGatePinsSyntheticEVMIdentityRegressions|ProducerGatePinsSemanticIntegrityRegressions|ProducerGatePinsExactBlockRuntimeClientRegressions|ReleaseSemanticCensus)"

// Extracts the exact sorted top-level test declarations selected from source.
func releaseSelectedTestDeclarations(selector string, sources []string) ([]string, error) {
	compiled, err := regexp.Compile(selector)
	if err != nil {
		return nil, err
	}
	testDeclaration := regexp.MustCompile(`(?m)^func (Test[[:alnum:]_]+)\(`)
	seen := map[string]bool{}
	selected := []string{}
	for _, source := range sources {
		for _, match := range testDeclaration.FindAllStringSubmatch(source, -1) {
			name := match[1]
			if !compiled.MatchString(name) {
				continue
			}
			if seen[name] {
				return nil, fmt.Errorf("selected test declaration %s is duplicated", name)
			}
			seen[name] = true
			selected = append(selected, name)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("semantic-integrity selector matched no test declarations")
	}
	sort.Strings(selected)
	return selected, nil
}

// Requires the reviewed census to be exact, sorted, unique, and nonempty.
func verifyReleaseSemanticCensus(recorded, selected []string) error {
	if len(recorded) == 0 || len(recorded) != len(selected) {
		return fmt.Errorf("semantic-integrity census count %d differs from selected count %d", len(recorded), len(selected))
	}
	for index := range selected {
		if recorded[index] != selected[index] {
			return fmt.Errorf("semantic-integrity census entry %d is %q, want %q", index, recorded[index], selected[index])
		}
		if index > 0 && recorded[index] <= recorded[index-1] {
			return fmt.Errorf("semantic-integrity census entry %d is not sorted and unique", index)
		}
	}
	return nil
}

// Every declaration in a reviewed source group must remain in its gate even
// when a renamed test no longer matches the original selection prefix.
func verifyReleaseSourceTestCoverage(selector, requiredSelector string, sources []string) error {
	required, err := releaseSelectedTestDeclarations(requiredSelector, sources)
	if err != nil {
		return err
	}
	selected, err := releaseSelectedTestDeclarations(selector, sources)
	if err != nil {
		return err
	}
	for _, name := range required {
		index := sort.SearchStrings(selected, name)
		if index == len(selected) || selected[index] != name {
			return fmt.Errorf("release selector omits source regression %s", name)
		}
	}
	return nil
}

// Extracts one unambiguous shell selector assignment.
func releaseConnectPolicySelectorAssignment(script string, variable string) (string, error) {
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(variable) + `='([^'\n]+)'\s*$`)
	matches := pattern.FindAllStringSubmatch(script, -1)
	if len(matches) != 1 {
		return "", fmt.Errorf("release gate has %d %s assignments, want exactly 1", len(matches), variable)
	}
	return matches[0][1], nil
}

// Pins every generated-policy consumer selected by either release gate.
func assertReleaseConnectPolicySelector(t *testing.T, script string, variable string) {
	t.Helper()
	selector, err := releaseConnectPolicySelectorAssignment(script, variable)
	if err != nil {
		t.Fatal(err)
	}
	if selector != releaseConnectPolicySelector {
		t.Errorf("%s = %q, want exact generated-policy selector %q", variable, selector, releaseConnectPolicySelector)
	}
}

// Both release gates pin the exact P2P failure and adjacent signaling tests.
func assertReleaseConnectP2PSignalSelector(t *testing.T, script string) {
	t.Helper()
	selector, err := releaseConnectPolicySelectorAssignment(script, "p2p_signal_tests")
	if err != nil {
		t.Fatal(err)
	}
	if selector != releaseConnectP2PSignalSelector {
		t.Errorf("p2p_signal_tests = %q, want exact P2P signaling selector %q", selector, releaseConnectP2PSignalSelector)
	}
	for _, command := range []string{
		`go test . -run "$p2p_signal_tests" -count=1`,
		`go test -race . -run "$p2p_signal_tests" -count=1`,
	} {
		if !strings.Contains(script, command) {
			t.Errorf("release gate omits %s", command)
		}
	}
}

// Rejects a later shell assignment that could replace the reviewed selector.
func TestReleaseConnectPolicySelectorRejectsDuplicateAssignments(t *testing.T) {
	script := "policy_tests='" + releaseConnectPolicySelector + "'\npolicy_tests='^TestWeakened$'\n"
	if _, err := releaseConnectPolicySelectorAssignment(script, "policy_tests"); err == nil {
		t.Fatal("duplicate policy selector assignments accepted")
	}
}

// Rejects a script that omits the reviewed selector entirely.
func TestReleaseConnectPolicySelectorRejectsMissingAssignment(t *testing.T) {
	if _, err := releaseConnectPolicySelectorAssignment("echo no-policy-selector\n", "policy_tests"); err == nil {
		t.Fatal("missing policy selector assignment accepted")
	}
}

// The launch-critical and aggregate gates must execute the same exact P2P
// signaling regression set in ordinary and race modes.
func TestReleaseGatesPinConnectP2PSignalRegressions(t *testing.T) {
	for _, scriptPath := range []string{
		"../scripts/test-release-1.0-producer-gate.sh",
		"../scripts/test-release-1.0-local.sh",
	} {
		scriptBytes, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatal(err)
		}
		assertReleaseConnectP2PSignalSelector(t, string(scriptBytes))
	}
}

// The producer gate must exercise every simulator adversarial implementation
// before any release campaign can mutate the shared testnet.
func TestProducerGatePinsCompleteAdversarialRegressions(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	selector, err := releaseConnectPolicySelectorAssignment(script, "adversarial_tests")
	if err != nil {
		t.Fatal(err)
	}
	if selector != releaseAdversarialSelector {
		t.Errorf("adversarial_tests = %q, want exact adversarial selector %q", selector, releaseAdversarialSelector)
	}
	source, err := os.ReadFile("adversary_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseSourceTestCoverage(selector, "^Test(Adversarial|Adversary|VerifyAdversary|RPCAdversary|ConsensusWeightComparison)", []string{string(source)}); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"TestRPCAdversaryRejectsObservedRuntimeCodeHashDrift",
		"TestAdversaryReleaseBytecodeMetricsRejectMalformedAndEmptyRuntime",
		"TestAdversaryAdditionalMetricModelsMeasureMultiMetricRows",
		"TestConsensusWeightComparisonIncludesUIDZero",
	} {
		if !strings.Contains(string(source), "func "+required+"(") || !regexp.MustCompile(selector).MatchString(required) {
			t.Errorf("adversarial source regression %s is absent or unselected", required)
		}
	}
	for _, command := range []string{
		`go test ./sim-testnet -run "$adversarial_tests" -count=1 -timeout 5m`,
		`go test -race ./sim-testnet -run "$adversarial_tests" -count=1 -timeout 10m`,
	} {
		if strings.Count(script, command) != 1 {
			t.Errorf("producer gate has %d copies of %q, want exactly 1", strings.Count(script, command), command)
		}
	}
}

// The first testnet write is gated by the exact EVM number/hash identity,
// canonical-hash reads, bounded batches, cancellation, and the real release
// steering consumer. Keeping the selector exact prevents a later harness edit
// from silently dropping the reorg or 1,000-provider regressions.
func TestProducerGatePinsExactBlockRuntimeClientRegressions(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	selector, err := releaseConnectPolicySelectorAssignment(script, "runtime_client_tests")
	if err != nil {
		t.Fatal(err)
	}
	if selector != releaseRuntimeClientSelector {
		t.Errorf("runtime_client_tests = %q, want exact runtime-client selector %q", selector, releaseRuntimeClientSelector)
	}
	for _, command := range []string{
		`go test ./crv4 ./miner ./validator -run "$runtime_client_tests" -count=1`,
		`go test -race ./crv4 ./miner ./validator -run "$runtime_client_tests" -count=1`,
	} {
		if strings.Count(script, command) != 1 {
			t.Errorf("producer gate has %d copies of %q, want exactly 1", strings.Count(script, command), command)
		}
	}
}

// Both gates explicitly execute the RPC identity regressions at each live
// consumer, including the separately compiled onchain transaction package.
func TestProducerGatePinsSyntheticEVMIdentityRegressions(t *testing.T) {
	for _, path := range []string{"../scripts/test-release-1.0-producer-gate.sh", "../scripts/test-release-1.0-local.sh"} {
		scriptBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		script := string(scriptBytes)
		selector, err := releaseConnectPolicySelectorAssignment(script, "synthetic_evm_identity_tests")
		if err != nil {
			t.Fatal(err)
		}
		if selector != releaseSyntheticEVMIdentitySelector {
			t.Errorf("%s: synthetic identity selector = %q, want %q", path, selector, releaseSyntheticEVMIdentitySelector)
		}
		for _, command := range []string{
			`go test ./miner/onchain ./miner ./sim-testnet -run "$synthetic_evm_identity_tests" -count=1 -timeout 5m`,
			`go test -race ./miner/onchain ./miner ./sim-testnet -run "$synthetic_evm_identity_tests" -count=1 -timeout 10m`,
		} {
			if strings.Count(script, command) != 1 {
				t.Errorf("%s has %d copies of %q, want exactly 1", path, strings.Count(script, command), command)
			}
		}
	}
	for _, sourcePath := range []string{"../miner/onchain/finality_test.go", "../miner/claim_receipt_identity_test.go", "synthetic_evm_identity_test.go", "synthetic_evm_fleet_identity_test.go"} {
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		all, err := releaseSelectedTestDeclarations("^Test", []string{string(source)})
		if err != nil {
			t.Fatal(err)
		}
		selected, err := releaseSelectedTestDeclarations(releaseSyntheticEVMIdentitySelector, []string{string(source)})
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyReleaseSemanticCensus(all, selected); err != nil {
			t.Errorf("%s: synthetic identity regressions escaped the gate: %v", sourcePath, err)
		}
	}
}

// The producer gate must prove exact native payout causality, every ordinary
// fleet generation, and historical EVM state/receipt identity before it can
// issue the first shared-testnet mutation.
func TestProducerGatePinsSemanticIntegrityRegressions(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	selector, err := releaseConnectPolicySelectorAssignment(script, "semantic_integrity_tests")
	if err != nil {
		t.Fatal(err)
	}
	if selector != releaseSemanticIntegritySelector {
		t.Errorf("semantic_integrity_tests = %q, want exact semantic-integrity selector %q", selector, releaseSemanticIntegritySelector)
	}
	testFiles, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	testSources := make([]string, 0, len(testFiles))
	allTestSource := strings.Builder{}
	for _, testFile := range testFiles {
		source, readErr := os.ReadFile(testFile)
		if readErr != nil {
			t.Fatal(readErr)
		}
		testSource := string(source)
		testSources = append(testSources, testSource)
		allTestSource.WriteString(testSource)
	}
	selectedDeclarations, err := releaseSelectedTestDeclarations(selector, testSources)
	if err != nil {
		t.Fatal(err)
	}
	censusBytes, err := os.ReadFile("semantic-integrity-tests.txt")
	if err != nil {
		t.Fatal(err)
	}
	census := strings.Fields(string(censusBytes))
	if string(censusBytes) != strings.Join(census, "\n")+"\n" {
		t.Error("semantic-integrity census must contain one test name per line and a final newline")
	}
	if err := verifyReleaseSemanticCensus(census, selectedDeclarations); err != nil {
		t.Error(err)
		t.Errorf("semantic-integrity census differs from the exact selected declarations\nrecorded:\n%s\nselected:\n%s", strings.Join(census, "\n"), strings.Join(selectedDeclarations, "\n"))
	}
	allTestSourceText := allTestSource.String()
	for _, required := range []string{
		"TestFinalSemanticArtifactVerificationCacheBindsExactBytesAndIsConcurrent",
		"TestFinalSemanticFixtureRewardDecisionsFollowAllVerifiedCycles",
		"TestFinalSemanticFixtureLifecycleCensusPreservesVerifiedTop200Boundary",
		"TestFinalSemanticFixtureWorkersJoinAllCasesWithBoundedConcurrency",
		"TestFinalSemanticFixtureWorkersRejectNonReturningCases",
		"TestFinalFleetLifecycleHeadAtUsesExactSettlementTransitions",
		"TestFinalSemanticFleetByUIDAtRejectsTerminalBackdatingAndAmbiguity",
		"TestFinalPayoutAssignmentsAtUsesExactLifecycleEpochMembership",
		"TestFinalFleetLifecyclePublicReplayRejectsEventAndVectorSubstitution",
		"TestFinalSemanticEvidenceFailsClosed",
		"TestFinalSemanticPathProofArtifactCount",
		"TestFinalSemanticPathProofArtifactRejectsMalformedTrailingJSON",
		"TestFinalSemanticPoolAuditDistinguishesUnderpaymentFromRecovery",
		"TestFinalSemanticDishonestDepositDecisionsAndPublicReplay",
		"TestFinalSemanticSettlementAccountingBindsBothHeadsAndEventDeltas",
		"TestFinalSemanticCarryModelFailsClosedOnAdjacentAccountingErrors",
		"TestFinalPublicChainVerificationRequiresTwoCanonicalOperatorOrigins",
		"TestFinalSemanticHistoricalCaptureUsesLiveDeploymentForZeroPlanBlock",
		"TestFinalSemanticHistoricalCoordinatorTargetCensusIncludesAllFinalizedActions",
		"TestFinalSemanticHistoricalCommitmentOracleScheduleBindsCalldataEventAndPostcondition",
		"TestFinalSemanticHistoricalReceiptArtifactRejectsEnvelopeAndLogMutations",
		"TestFinalSemanticHistoricalCoordinatorBaselinesReplayEveryInitialization",
		"TestFinalSemanticHistoricalCoordinatorReceiptTranscriptPinsExecutionHead",
		"TestFinalSemanticHistoricalCoordinatorTimelineArtifactRebuildsTransitions",
		"TestFinalSemanticHistoricalCoordinatorTimelineBindsExecutionAndPostState",
		"TestFinalSemanticHistoricalCoordinatorTimelineBuildsCompleteArchiveRanges",
		"TestFinalSemanticHistoricalCoordinatorTimelineCarriesLaterPlanCallAcrossOldUpgrade",
		"TestFinalSemanticHistoricalCoordinatorTimelineRejectsBrokenTransitionChain",
		"TestFinalSemanticHistoricalCoordinatorTimelineRejectsMixedUpgradeBlock",
		"TestFinalSemanticHistoricalCoordinatorTimelineRejectsSubstitutedBaseline",
		"TestFinalSemanticHistoricalOracleWindowArtifactRejectsBoundMutations",
		"TestFinalSemanticHistoricalOracleWindowDigestBindsPublicChronologyProjection",
		"TestFinalSemanticHistoricalOracleWindowTranscriptRejectsMutations",
		"TestFinalSemanticHistoricalOracleWindowOnChainBindsCoordinatorProxy",
		"TestFinalSemanticHistoricalReaderFactorySnapshotCannotMutateVerifiedCopy",
		"TestFinalSemanticHistoricalPlanHistoryRejectsBareNamespace",
		"TestFinalSemanticDeploymentBoundarySeparatesProxyHistoryFromReleaseGraph",
		"TestFinalSemanticDeploymentBoundaryRuntimeConfigsAreAcceptedByReleaseLoaders",
		"TestFinalSemanticHistoricalArtifactCensusIncludesCarriedFleetProofs",
		"TestFinalFleetGenerationSourceRetainsBothChallengerPostconditionsForReplay",
		"TestFinalSemanticBuilderDepositReceiptSelectsExactCumulativePrefix",
		"TestFinalSemanticBuilderDepositReceiptRejectsNonCanonicalAuditAmounts",
		"TestFinalSemanticBuilderDepositReceiptRejectsNonpositiveDepositEvent",
		"TestFinalSemanticBuilderDepositReceiptRejectsLaterDepositsWithinObservedHead",
		"TestFinalSemanticBuilderDepositReceiptExcludesHistoricalEmitterAndGeneration",
		"TestFinalSemanticBuilderBindsDishonestUnderpaymentToValidatorRecovery",
		"TestFinalSemanticBuilderRejectsIncompleteRecoveryValidatorCensus",
		"TestFinalSemanticBuilderChecksEveryRecoveryObservationHead",
		"TestFinalSemanticHeadUIDZeroIsValidAndStillUnique",
		"TestFinalSemanticHeadUIDZeroRejectsMismatchedRegistration",
		"TestFinalSemanticPoolUIDZeroIsValidAndStillUnique",
		"TestFinalSemanticValidatorUIDZeroIsValidAndStillUnique",
		"TestFinalSemanticValidatorUIDZeroRejectsMismatchedRegistration",
		"TestPublicScenarioBundleRequiresReplicatedOwnerCompletionCommit",
		"TestStateMismatchErrorPreservesCausesWithoutFormattingNilWraps",
		"TestSemanticMismatchBranchesNeverWrapPotentiallyNilErrors",
		"TestFinalEVMLogQueryRangesRespectOfficialInclusiveLimit",
		"TestFinalCollectedCoordinatorBaselinesRequireInitializerLog",
		"TestReleaseHistoryRuntimeArtifactsCoverExactFourVersionDomain",
		"TestProducerGatePinsCompleteAdversarialRegressions",
		"TestProducerGatePinsSyntheticEVMIdentityRegressions",
		"TestProducerGatePinsSemanticIntegrityRegressions",
		"TestProducerGatePinsExactBlockRuntimeClientRegressions",
		"TestReleaseSemanticCensusPinsCompleteRegressionSourceGroups",
	} {
		selected, selectErr := releaseSelectedTestDeclarations(selector, []string{"func " + required + "(t *testing.T) {}\n"})
		if selectErr != nil || len(selected) != 1 || selected[0] != required {
			t.Errorf("semantic-integrity selector omits lifecycle proof %s", required)
		}
		if !strings.Contains(allTestSourceText, "func "+required+"(") {
			t.Errorf("semantic-integrity proof %s is absent from source", required)
		}
	}
	for _, censusCommand := range []string{
		`semantic_integrity_census="$sn_repo/sim-testnet/semantic-integrity-tests.txt"`,
		`semantic_integrity_actual="$(go test ./sim-testnet -list "$semantic_integrity_tests" | sed -n '/^Test/p' | LC_ALL=C sort)"`,
		`diff -u "$semantic_integrity_census" <(printf '%s\n' "$semantic_integrity_actual")`,
		`semantic-integrity selector matched no tests`,
	} {
		if strings.Count(script, censusCommand) != 1 {
			t.Errorf("producer gate has %d copies of %q, want exactly 1", strings.Count(script, censusCommand), censusCommand)
		}
	}
	for _, command := range []string{
		`go test ./sim-testnet -run "$semantic_integrity_tests" -count=1 -parallel=4 -timeout 15m`,
		`go test -race ./sim-testnet -run "$semantic_integrity_tests" -count=1 -parallel=4 -timeout 25m`,
	} {
		if strings.Count(script, command) != 1 {
			t.Errorf("producer gate has %d copies of %q, want exactly 1", strings.Count(script, command), command)
		}
	}
	// Trace helpers too: a new indirect full-fixture user must not restore a
	// long serial prefix before Go releases the parallel roots.
	declarations := map[string]*ast.FuncDecl{}
	callees := map[string]map[string]bool{}
	for _, source := range testSources {
		parsed, err := parser.ParseFile(token.NewFileSet(), "fixture_test.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || function.Body == nil {
				continue
			}
			name := function.Name.Name
			declarations[name] = function
			callees[name] = map[string]bool{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if ok {
					if identifier, ok := call.Fun.(*ast.Ident); ok {
						callees[name][identifier.Name] = true
					}
				}
				return true
			})
		}
	}
	wrapper := declarations["runFinalSemanticTestCases"]
	scheduler := declarations["runFinalSemanticTestCasesWithSpawn"]
	if err := verifyReleaseSemanticTestScheduler(wrapper, scheduler); err != nil {
		t.Fatal(err)
	}
	// Mutate only this parsed copy: losing either the worker bound or the
	// per-callback join must still be rejected by the structural gate itself.
	var workerLoop, callbackLoop *ast.RangeStmt
	ast.Inspect(scheduler.Body, func(node ast.Node) bool {
		loop, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if _, ok := loop.X.(*ast.CallExpr); ok {
			workerLoop = loop
		} else if source, ok := loop.X.(*ast.Ident); ok && source.Name == "indices" {
			callbackLoop = loop
		}
		return true
	})
	if workerLoop == nil || callbackLoop == nil {
		t.Fatal("semantic scheduler structural controls lost their loop targets")
	}
	bound := workerLoop.X
	workerLoop.X = ast.NewIdent("unboundedWorkers")
	boundErr := verifyReleaseSemanticTestScheduler(wrapper, scheduler)
	workerLoop.X = bound
	if boundErr == nil || !strings.Contains(boundErr.Error(), "bounded worker loop") {
		t.Fatalf("semantic scheduler accepted a lost worker bound: %v", boundErr)
	}
	statements := callbackLoop.Body.List
	callbackLoop.Body.List = statements[:len(statements)-1]
	joinErr := verifyReleaseSemanticTestScheduler(wrapper, scheduler)
	callbackLoop.Body.List = statements
	if joinErr == nil || !strings.Contains(joinErr.Error(), "joined callback") {
		t.Fatalf("semantic scheduler accepted a lost callback join: %v", joinErr)
	}
	fixtureUsers := map[string]bool{"finalSemanticFixture": true}
	for changed := true; changed; {
		changed = false
		for caller, calls := range callees {
			if fixtureUsers[caller] {
				continue
			}
			for callee := range calls {
				if fixtureUsers[callee] {
					fixtureUsers[caller], changed = true, true
					break
				}
			}
		}
	}
	for name := range fixtureUsers {
		if !strings.HasPrefix(name, "Test") {
			continue
		}
		function := declarations[name]
		parallel := false
		if function != nil && len(function.Body.List) != 0 {
			if statement, ok := function.Body.List[0].(*ast.ExprStmt); ok {
				if call, ok := statement.X.(*ast.CallExpr); ok && len(call.Args) == 0 {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Parallel" {
						identifier, ok := selector.X.(*ast.Ident)
						parallel = ok && identifier.Name == "t"
					}
				}
			}
		}
		if !parallel {
			t.Errorf("release-scale fixture consumer %s must start with t.Parallel under the bounded gate", name)
		}
	}
	if finalSemanticTestCaseWorkers != 4 {
		t.Fatalf("independent semantic view workers=%d, want exactly 4", finalSemanticTestCaseWorkers)
	}
	// Barriers prove useful concurrency, independent mutable snapshots, full
	// completion despite errors, and canonical reporting without sleep races.
	view := finalPublicScenarioTestView{
		objects: map[string][]byte{"object": []byte("immutable")}, commitVisible: map[int]bool{1: true},
		supplementHistory: map[int][]string{1: {"original"}}, objectOverrides: map[int]map[string][]byte{1: {"override": []byte("immutable")}},
	}
	started := make(chan struct{}, finalSemanticTestCaseWorkers)
	release := make(chan struct{})
	var completed atomic.Uint64
	cases := make([]finalSemanticTestCase, 2*finalSemanticTestCaseWorkers)
	caseErrs := make([]error, len(cases))
	for index := range cases {
		copy := view.snapshot()
		caseErrs[index] = fmt.Errorf("case%d", index)
		cases[index] = finalSemanticTestCase{name: fmt.Sprint(index), verify: func(context.Context) error {
			if index < finalSemanticTestCaseWorkers {
				started <- struct{}{}
				<-release
			}
			copy.objects["object"] = []byte("replaced")
			copy.commitVisible[1] = false
			copy.supplementHistory[1][0] = "replaced"
			copy.objectOverrides[1]["override"] = nil
			completed.Or(uint64(1) << index)
			return caseErrs[index]
		}}
	}
	done := make(chan []error, 1)
	go func() { done <- runFinalSemanticTestCases(context.Background(), cases) }()
	for range finalSemanticTestCaseWorkers {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			close(release)
			<-done
			t.Fatal("independent semantic views did not reach the explicit concurrency barrier")
		}
	}
	close(release)
	results := <-done
	if completed.Load() != uint64(1)<<len(cases)-1 || len(results) != len(cases) {
		t.Fatalf("semantic cases were skipped after a failure: completed=%b results=%d", completed.Load(), len(results))
	}
	for index, err := range results {
		if !errors.Is(err, caseErrs[index]) {
			t.Errorf("semantic result%d lost canonical failure order: %v", index, err)
		}
	}
	if string(view.objects["object"]) != "immutable" || !view.commitVisible[1] || view.supplementHistory[1][0] != "original" || string(view.objectOverrides[1]["override"]) != "immutable" {
		t.Fatal("parallel public-replay view poisoned another snapshot's mutable state")
	}
}

// Distinguish the synchronous pool-creation seam from its joined callback
// children; a callback Goexit must not require unbounded replacement workers.
func verifyReleaseSemanticTestScheduler(wrapper, scheduler *ast.FuncDecl) error {
	isName := func(node ast.Node, want string) bool {
		name, ok := node.(*ast.Ident)
		return ok && name.Name == want
	}
	if wrapper == nil || wrapper.Body == nil || len(wrapper.Body.List) != 1 || scheduler == nil || scheduler.Body == nil {
		return errors.New("bounded semantic test scheduler or its sole wrapper is missing")
	}
	returned, ok := wrapper.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(returned.Results) != 1 {
		return errors.New("semantic scheduler wrapper does not return its sole delegate")
	}
	delegate, ok := returned.Results[0].(*ast.CallExpr)
	if !ok || !isName(delegate.Fun, scheduler.Name.Name) || len(delegate.Args) != 3 || !isName(delegate.Args[0], "ctx") || !isName(delegate.Args[1], "cases") {
		return errors.New("semantic scheduler wrapper lost its exact context, cases, or spawn delegate")
	}
	launcher, ok := delegate.Args[2].(*ast.FuncLit)
	if !ok || len(launcher.Type.Params.List) != 1 || len(launcher.Type.Params.List[0].Names) != 1 || len(launcher.Body.List) != 1 {
		return errors.New("semantic scheduler wrapper lost its sole worker launcher")
	}
	launch, ok := launcher.Body.List[0].(*ast.GoStmt)
	if !ok || !isName(launch.Call.Fun, launcher.Type.Params.List[0].Names[0].Name) || len(launch.Call.Args) != 0 {
		return errors.New("semantic scheduler wrapper must launch exactly its supplied worker")
	}
	boundedLoops, workerStarts, callbackStarts := 0, 0, 0
	var boundedLoop *ast.RangeStmt
	ast.Inspect(scheduler.Body, func(node ast.Node) bool {
		if _, ok := node.(*ast.GoStmt); ok {
			callbackStarts++
		}
		if call, ok := node.(*ast.CallExpr); ok && isName(call.Fun, "spawn") {
			workerStarts++
		}
		loop, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		call, ok := loop.X.(*ast.CallExpr)
		if !ok || !isName(call.Fun, "min") || len(call.Args) != 2 || !isName(call.Args[0], "finalSemanticTestCaseWorkers") {
			return true
		}
		length, ok := call.Args[1].(*ast.CallExpr)
		if ok && isName(length.Fun, "len") && len(length.Args) == 1 && isName(length.Args[0], "cases") {
			boundedLoops++
			boundedLoop = loop
		}
		return true
	})
	if boundedLoops != 1 || workerStarts != 1 || callbackStarts != 1 {
		return fmt.Errorf("semantic scheduler lost its sole bounded worker loop: bounded=%d starts=%d callbacks=%d", boundedLoops, workerStarts, callbackStarts)
	}
	var worker *ast.FuncLit
	for _, statement := range boundedLoop.Body.List {
		expression, ok := statement.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expression.X.(*ast.CallExpr)
		if ok && isName(call.Fun, "spawn") && len(call.Args) == 1 {
			worker, _ = call.Args[0].(*ast.FuncLit)
		}
	}
	if worker == nil {
		return errors.New("semantic scheduler moved worker creation outside its bounded worker loop")
	}
	joinedCallbacks := 0
	ast.Inspect(worker.Body, func(node ast.Node) bool {
		loop, ok := node.(*ast.RangeStmt)
		if !ok || !isName(loop.X, "indices") {
			return true
		}
		for index, statement := range loop.Body.List {
			launch, ok := statement.(*ast.GoStmt)
			if !ok || index+1 != len(loop.Body.List)-1 {
				continue
			}
			callback, ok := launch.Call.Fun.(*ast.FuncLit)
			if !ok || len(launch.Call.Args) != 0 || len(callback.Body.List) != 2 {
				continue
			}
			completion, ok := callback.Body.List[0].(*ast.DeferStmt)
			if !ok || !isName(completion.Call.Fun, "close") || len(completion.Call.Args) != 1 {
				continue
			}
			completionChannel, ok := completion.Call.Args[0].(*ast.Ident)
			join, joinOK := loop.Body.List[index+1].(*ast.ExprStmt)
			if !ok || !joinOK {
				continue
			}
			receive, ok := join.X.(*ast.UnaryExpr)
			if ok && receive.Op == token.ARROW && isName(receive.X, completionChannel.Name) {
				joinedCallbacks++
			}
		}
		return true
	})
	if joinedCallbacks != 1 {
		return fmt.Errorf("semantic scheduler must retain exactly one joined callback per worker: got %d", joinedCallbacks)
	}
	return nil
}

// Review the complete deployment, chronology, fleet, registration, and builder
// source groups independently of the checked-in list. A list regenerated from
// a weakened selector must still fail when it drops one of these regressions
// or the exact-byte artifact verification cache proofs.
func TestReleaseSemanticCensusPinsCompleteRegressionSourceGroups(t *testing.T) {
	for _, group := range []struct {
		pattern  string
		required string
	}{
		{pattern: "final_semantic_deployment_anchor_test.go", required: "^Test"},
		{pattern: "final_semantic_deployment_boundary_test.go", required: "^Test"},
		{pattern: "final_semantic_chronology*_test.go", required: "^Test"},
		{pattern: "final_semantic_public_chronology_test.go", required: "^Test"},
		{pattern: "final_semantic_historical_capture_test.go", required: "^Test"},
		{pattern: "final_semantic_fleet_generation*_test.go", required: "^Test"},
		{pattern: "final_semantic_native*_test.go", required: "^Test"},
		{pattern: "final_semantic_registration_test.go", required: "^Test"},
		{pattern: "final_semantic_source_builder_test.go", required: "^TestFinalSemanticBuilder"},
		{pattern: "final_semantic_evidence_test.go", required: "^TestFinalSemanticArtifactVerificationCache"},
		{pattern: "final_semantic_path_proof_test.go", required: "^Test"},
	} {
		paths, err := filepath.Glob(group.pattern)
		if err != nil || len(paths) == 0 {
			t.Fatalf("regression source group %s is absent: %v", group.pattern, err)
		}
		for _, path := range paths {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyReleaseSourceTestCoverage(releaseSemanticIntegritySelector, group.required, []string{string(source)}); err != nil {
				t.Errorf("%s: %v", path, err)
			}
		}
	}
}

// A syntactically valid selector and exact recorded subset are insufficient
// when an adjacent declaration has escaped its independently reviewed group.
func TestReleaseSemanticCensusRejectsSourceSelectorOmissions(t *testing.T) {
	source := "func TestFinalNativeAlpha(t *testing.T) {}\nfunc TestRenamedAdjacent(t *testing.T) {}\n"
	if err := verifyReleaseSourceTestCoverage("^Test", "^Test", []string{source}); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"^TestFinalNative", "^TestMissing$"} {
		if err := verifyReleaseSourceTestCoverage(selector, "^Test", []string{source}); err == nil {
			t.Errorf("source selector %q accepted an omitted regression", selector)
		}
	}
}

// An exact reviewed list is accepted only when source selection and persisted
// ordering agree byte-for-entry.
func TestReleaseSemanticCensusAcceptsExactSortedSelection(t *testing.T) {
	sources := []string{
		"func TestFinalNativeZulu(t *testing.T) {}\nfunc TestUnselected(t *testing.T) {}\n",
		"func TestFinalNativeAlpha(t *testing.T) {}\n",
	}
	selected, err := releaseSelectedTestDeclarations(`^TestFinalNative`, sources)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestFinalNativeAlpha", "TestFinalNativeZulu"}
	if err := verifyReleaseSemanticCensus(want, selected); err != nil {
		t.Fatal(err)
	}
}

// An empty or nonmatching selector must fail before the producer can mistake
// a vacuous Go test invocation for evidence.
func TestReleaseSemanticCensusRejectsZeroSelection(t *testing.T) {
	for _, selector := range []string{"^TestMissing$", "^$"} {
		if _, err := releaseSelectedTestDeclarations(selector, []string{"func TestFinalNativeAlpha(t *testing.T) {}\n"}); err == nil {
			t.Errorf("selector %q accepted zero declarations", selector)
		}
	}
}

// Every addition, deletion, rename, duplicate, and ordering change is release
// fatal until the checked-in list receives explicit review.
func TestReleaseSemanticCensusRejectsDeclarationDrift(t *testing.T) {
	selected := []string{"TestFinalNativeAlpha", "TestFinalNativeBeta"}
	cases := []struct {
		label    string
		recorded []string
	}{
		{label: "missing", recorded: []string{"TestFinalNativeAlpha"}},
		{label: "added", recorded: []string{"TestFinalNativeAlpha", "TestFinalNativeBeta", "TestFinalNativeGamma"}},
		{label: "renamed", recorded: []string{"TestFinalNativeAlpha", "TestFinalNativeDelta"}},
		{label: "duplicate", recorded: []string{"TestFinalNativeAlpha", "TestFinalNativeAlpha"}},
		{label: "out of order", recorded: []string{"TestFinalNativeBeta", "TestFinalNativeAlpha"}},
	}
	for _, testCase := range cases {
		if err := verifyReleaseSemanticCensus(testCase.recorded, selected); err == nil {
			t.Errorf("%s census drift accepted", testCase.label)
		}
	}
	if _, err := releaseSelectedTestDeclarations(`^TestFinalNative`, []string{
		"func TestFinalNativeAlpha(t *testing.T) {}\n",
		"func TestFinalNativeAlpha(t *testing.T) {}\n",
	}); err == nil {
		t.Error("duplicate selected source declaration accepted")
	}
}

// Both release gates must detect whole-file and adjacent identifier rewrites
// before trying to compile the simulator package they can corrupt.
func TestReleaseGatesRunSourceIntegrityBeforeSimulatorCompile(t *testing.T) {
	for _, scriptPath := range []string{
		"../scripts/test-release-1.0-producer-gate.sh",
		"../scripts/test-release-1.0-local.sh",
	} {
		scriptBytes, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatal(err)
		}
		script := string(scriptBytes)
		for _, command := range []string{
			"go test ./sim-testnet/sourceguard -count=1",
			"go run ./sim-testnet/sourceguard ./sim-testnet",
		} {
			if strings.Count(script, command) != 1 {
				t.Errorf("%s has %d copies of %q, want exactly 1", scriptPath, strings.Count(script, command), command)
			}
		}
		integrityIndex := strings.Index(script, "go run ./sim-testnet/sourceguard ./sim-testnet")
		compileIndex := strings.Index(script, "Go tests")
		if strings.Contains(scriptPath, "producer") {
			compileIndex = strings.Index(script, "compile complete simulator and validator graph")
		}
		if integrityIndex < 0 || compileIndex < 0 || integrityIndex >= compileIndex {
			t.Errorf("%s does not run source-integrity validation before simulator compilation", scriptPath)
		}
	}
}

// The aggregate certificate retains process-global ordering and residue that
// exact-name shards erase, including one reproducible alternate ordering.
func TestAggregateGateRunsUnshardedConnectRaceCertificates(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, command := range []string{
		"go test -count=1 -timeout 30m .",
		"GOMAXPROCS=4 go test -race -count=1 -timeout 30m .",
		"GOMAXPROCS=4 go test -race -count=1 -timeout 30m -shuffle=4535211000 .",
	} {
		if strings.Count(script, command) != 1 {
			t.Errorf("aggregate gate has %d copies of %q, want exactly 1", strings.Count(script, command), command)
		}
	}
}

// A sibling release module can advance while the long race and Solidity gates
// are running. The final check must cover every module hashed by the release
// lock and occur after all other checked-in gate work.
func TestLocalReleaseGateRechecksCompleteWorkspaceAtEnd(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	assertReleaseConnectPolicySelector(t, script, "security_table_tests")
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
		"CfaaInspectIcmp",
		"go test ./blocker ./security -count=1",
		"go test -race ./blocker ./security -count=1",
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

// The exact binding generator must fail before runtime attestation and long
// language suites, not near the end after their work has already completed.
func TestReleaseGatesPreflightBindingToolBeforeLongWork(t *testing.T) {
	testCases := []struct {
		path          string
		sourceMarker  string
		runtimeMarker string
		longMarker    string
	}{
		{
			path:          "../scripts/test-release-1.0-local.sh",
			sourceMarker:  "source-freeze preflight",
			runtimeMarker: "runtime 454 source attestation",
			longMarker:    "sn Go tests",
		},
		{
			path:          "../scripts/test-release-1.0-producer-gate.sh",
			sourceMarker:  "source-freeze preflight",
			runtimeMarker: "runtime 454 source attestation",
			longMarker:    "compile complete simulator and validator graph",
		},
	}
	const preflight = `"$sn_repo/stabi/generate.sh" --preflight`
	for _, testCase := range testCases {
		scriptBytes, err := os.ReadFile(testCase.path)
		if err != nil {
			t.Fatal(err)
		}
		script := string(scriptBytes)
		if count := strings.Count(script, preflight); count != 1 {
			t.Errorf("%s binding preflight count = %d, want 1", testCase.path, count)
			continue
		}
		sourceIndex := strings.Index(script, testCase.sourceMarker)
		preflightIndex := strings.Index(script, preflight)
		runtimeIndex := strings.Index(script, testCase.runtimeMarker)
		longIndex := strings.Index(script, testCase.longMarker)
		if sourceIndex < 0 || preflightIndex <= sourceIndex || runtimeIndex <= preflightIndex || longIndex <= preflightIndex {
			t.Errorf("%s preflight ordering source=%d preflight=%d runtime=%d long=%d", testCase.path, sourceIndex, preflightIndex, runtimeIndex, longIndex)
		}
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
	if !strings.Contains(string(scriptBytes), "go test -race -parallel=4 -timeout 90m ./sim-testnet") {
		t.Fatal("local release gate lacks the reviewed 90-minute full simulator race deadline")
	}
}

// Keeps the additive contract-lane diagnostic in both release paths instead
// of merely compiling its regression without executing it.
func TestReleaseGatesExerciseContractSenderRoleDiagnostic(t *testing.T) {
	testCases := []struct {
		path     string
		commands []string
	}{
		{
			path: "../scripts/test-release-1.0-local.sh",
			commands: []string{
				`go test . -run "$contract_sender_diagnostic_tests" -count=1`,
				`go test -race . -run "$contract_sender_diagnostic_tests" -count=1`,
			},
		},
		{
			path: "../scripts/test-release-1.0-producer-gate.sh",
			commands: []string{
				`go test . -run '^TestCreateContractReportsSenderSequenceRole$' -count=1`,
			},
		},
	}
	for _, testCase := range testCases {
		scriptBytes, err := os.ReadFile(testCase.path)
		if err != nil {
			t.Errorf("read %s: %v", testCase.path, err)
			continue
		}
		script := string(scriptBytes)
		if testCase.path == "../scripts/test-release-1.0-local.sh" && !strings.Contains(script, `contract_sender_diagnostic_tests='^TestCreateContractReportsSenderSequenceRole$'`) {
			t.Errorf("%s lacks the exact contract sender diagnostic selector", testCase.path)
		}
		for _, command := range testCase.commands {
			if strings.Count(script, command) != 1 {
				t.Errorf("%s contains %d copies of %q, want 1", testCase.path, strings.Count(script, command), command)
			}
		}
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
		"sn": "github.com/urfoundation/sn", "server": "github.com/urnetwork/server", "operator-proxy": "github.com/urnetwork/operator-proxy",
		"connect": "github.com/urnetwork/connect", "sdk": "github.com/urnetwork/sdk", "glog": "github.com/urnetwork/glog",
		"goidenticons": "github.com/urnetwork/goidenticons", "proxy": "github.com/urnetwork/proxy", "userwireguard": "github.com/urnetwork/userwireguard",
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

// Mirrors the strict release manifest so unknown or missing provenance fields
// fail the deterministic gate test before the network-backed checker runs.
type releaseRuntimeMetadataArtifactManifest struct {
	SchemaVersion         uint32                           `json:"schema_version"`
	Network               string                           `json:"network"`
	SubstrateRPCURL       string                           `json:"substrate_rpc_url"`
	GenesisHash           string                           `json:"genesis_hash"`
	RuntimeSpecName       string                           `json:"runtime_spec_name"`
	TransactionVersion    uint32                           `json:"transaction_version"`
	StateVersion          uint8                            `json:"state_version"`
	MetadataVersion       uint32                           `json:"metadata_version"`
	RuntimeCodeStorageKey string                           `json:"runtime_code_storage_key"`
	PolkadotSDKRevision   string                           `json:"polkadot_sdk_revision"`
	Artifacts             []releaseRuntimeMetadataArtifact `json:"artifacts"`
}

// Binds one historical observation to its exact source ref, Wasm bytes and
// runtime-produced metadata bytes.
type releaseRuntimeMetadataArtifact struct {
	SpecVersion          uint32  `json:"spec_version"`
	SourceRefKind        string  `json:"source_ref_kind"`
	SourceRefName        string  `json:"source_ref_name"`
	SourceCommit         string  `json:"source_commit"`
	ObservationBlock     uint64  `json:"observation_block"`
	ObservationBlockHash string  `json:"observation_block_hash"`
	CodeSource           string  `json:"code_source"`
	CodeURL              *string `json:"code_url"`
	CodeSize             uint64  `json:"code_size"`
	CodeSHA256           string  `json:"code_sha256"`
	CodeBlake2b256       string  `json:"code_blake2b_256"`
	MetadataSize         uint64  `json:"metadata_size"`
	MetadataSHA256       string  `json:"metadata_sha256"`
	MetadataBlake2b256   string  `json:"metadata_blake2b_256"`
}

// Go decision models are useful supplements, but they cannot prove what the
// deployed FRAME runtime contains. Require both release entry points to hash
// the exact reviewed upstream Rust sources and execute the exact observed Wasm
// under a storage-free host boundary before any local gate work begins.
func TestReleaseGatesAttestPinnedRuntime454RustSource(t *testing.T) {
	manifestBytes, err := os.ReadFile("../docs/spec/runtime-v454-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(manifestBytes)), "\n")
	if len(rows) != 24 {
		t.Fatalf("runtime 454 source manifest has %d rows, want 24", len(rows))
	}
	requiredPaths := map[string]bool{
		"pallets/drand/src/tests.rs":                                          false,
		"pallets/drand/src/verifier.rs":                                       false,
		"pallets/proxy/src/lib.rs":                                            false,
		"pallets/proxy/src/tests.rs":                                          false,
		"pallets/subtensor/src/benchmarks/benchmarks.rs":                      false,
		"pallets/subtensor/src/lib.rs":                                        false,
		"pallets/subtensor/src/macros/dispatches.rs":                          false,
		"pallets/subtensor/src/macros/errors.rs":                              false,
		"pallets/subtensor/src/macros/hooks.rs":                               false,
		"pallets/subtensor/src/migrations/migrate_cleanup_staking_hotkeys.rs": false,
		"pallets/subtensor/src/migrations/migrate_storage_bloat_v2.rs":        false,
		"pallets/subtensor/src/staking/claim_root.rs":                         false,
		"pallets/subtensor/src/staking/stake_utils.rs":                        false,
		"pallets/subtensor/src/subnets/subnet.rs":                             false,
		"pallets/subtensor/src/tests/claim_root.rs":                           false,
		"pallets/subtensor/src/tests/migration.rs":                            false,
		"pallets/subtensor/src/tests/move_stake.rs":                           false,
		"pallets/subtensor/src/tests/networks.rs":                             false,
		"pallets/subtensor/src/tests/swap_hotkey_with_subnet.rs":              false,
		"precompiles/src/balance_transfer.rs":                                 false,
		"primitives/share-pool/src/lib.rs":                                    false,
		"runtime/src/lib.rs":                                                  false,
		"runtime/tests/claim_root_weight.rs":                                  false,
		"runtime/tests/precompiles.rs":                                        false,
	}
	for _, row := range rows {
		fields := strings.Fields(row)
		if len(fields) != 2 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(fields[0]) {
			t.Fatalf("non-canonical runtime 454 source row %q", row)
		}
		if _, ok := requiredPaths[fields[1]]; !ok {
			t.Fatalf("unexpected runtime 454 source path %q", fields[1])
		}
		requiredPaths[fields[1]] = true
	}
	for path, present := range requiredPaths {
		if !present {
			t.Errorf("runtime 454 source manifest omits %s", path)
		}
	}
	metadataManifestBytes, err := os.ReadFile("../docs/spec/runtime-metadata-static-source.sha256")
	if err != nil {
		t.Fatal(err)
	}
	metadataRows := strings.Split(strings.TrimSpace(string(metadataManifestBytes)), "\n")
	if len(metadataRows) != 12 {
		t.Fatalf("runtime metadata source manifest has %d rows, want 12", len(metadataRows))
	}
	wantMetadataCommits := map[string]string{
		"head:release-v451": "d78d9cc6a6ee4d805f74a35414baaef8be025a5f",
		"tag:v452":          "da06f033663896ef2fdbbfc3ecc68ca908fba0f5",
		"tag:v453":          "823bdcbc58a29f60b243be4737a7c72b34ac7d93",
		"tag:v454":          "14cde6410fe8ec81a940e290c56f94a632a0988d",
	}
	seenMetadataPaths := map[string]bool{}
	for _, row := range metadataRows {
		fields := strings.Fields(row)
		if len(fields) != 5 {
			t.Fatalf("non-canonical runtime metadata source row %q", row)
		}
		ref := fields[0] + ":" + fields[1]
		if wantMetadataCommits[ref] != fields[2] || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(fields[3]) {
			t.Fatalf("non-canonical runtime metadata source row %q", row)
		}
		if fields[4] != "runtime/src/lib.rs" && fields[4] != "runtime/tests/metadata.rs" && fields[4] != "support/procedural-fork/src/construct_runtime/expand/metadata.rs" {
			t.Fatalf("unexpected runtime metadata source path %q", fields[4])
		}
		key := ref + ":" + fields[4]
		if seenMetadataPaths[key] {
			t.Fatalf("duplicate runtime metadata source row %q", key)
		}
		seenMetadataPaths[key] = true
	}
	artifactManifestBytes, err := os.ReadFile("../docs/spec/runtime-metadata-artifacts.json")
	if err != nil {
		t.Fatal(err)
	}
	var artifactManifest releaseRuntimeMetadataArtifactManifest
	if err := decodeStrictJSONBytes(artifactManifestBytes, &artifactManifest); err != nil {
		t.Fatalf("decode runtime metadata artifact manifest: %v", err)
	}
	if artifactManifest.SchemaVersion != 1 || artifactManifest.Network != "bittensor-testnet" ||
		artifactManifest.SubstrateRPCURL != "https://test.finney.opentensor.ai:443" ||
		artifactManifest.GenesisHash != testnetGenesis || artifactManifest.RuntimeSpecName != "node-subtensor" ||
		artifactManifest.TransactionVersion != 1 || artifactManifest.StateVersion != 1 || artifactManifest.MetadataVersion != 14 ||
		artifactManifest.RuntimeCodeStorageKey != "0x3a636f6465" ||
		artifactManifest.PolkadotSDKRevision != "cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a" {
		t.Fatalf("runtime metadata artifact manifest identity=%+v", artifactManifest)
	}
	if len(artifactManifest.Artifacts) != 4 {
		t.Fatalf("runtime metadata artifact manifest has %d artifacts, want 4", len(artifactManifest.Artifacts))
	}
	wantArtifacts := map[uint32]releaseRuntimeMetadataArtifact{
		451: {
			SpecVersion: 451, SourceRefKind: "head", SourceRefName: "release-v451", SourceCommit: "d78d9cc6a6ee4d805f74a35414baaef8be025a5f",
			ObservationBlock: 7887357, ObservationBlockHash: "0x1e708e8ce43d0205a7b9841874d7c3fa59bcc100af5f6dc6f7e087ea7b2d92ac",
			CodeSource: "substrate-storage", CodeSize: 2514870, CodeSHA256: "7a16d39f2c1f7c9984834a7cede18baf1d08306707bc8aeb0e2409324b2ec56f",
			CodeBlake2b256: "0xf3554a22dfcefa9b42b3a0a5e58c1e6c871795ecc9ea9da78bf0900e23e57c08",
			MetadataSize:   334487, MetadataSHA256: "39787898474fa5d27ce07097c30f1c7ba1472abdcb2c88b7957039eba1144ba7",
			MetadataBlake2b256: "0xeecd7e7c00377caec23c3dc754fd621963cc456fa5d02a4f66ff267b0494bd9d",
		},
		452: {
			SpecVersion: 452, SourceRefKind: "tag", SourceRefName: "v452", SourceCommit: "da06f033663896ef2fdbbfc3ecc68ca908fba0f5",
			ObservationBlock: 7891059, ObservationBlockHash: "0x638f2b015e5b6e11b27ee7aaae272f0965ba2f7c1b71dcb50472bfe99676b236",
			CodeSource: "github-release", CodeSize: 2515476, CodeSHA256: "05616e9ddf330e4c0f880fff1fb155162b7837bb4171b7ffc779680379eb9d8b",
			CodeBlake2b256: "0x40a8c3c99a47d6739b086236308535fab26d5fd4cc5c88eb83f6a3c8b928f7cc",
			MetadataSize:   334487, MetadataSHA256: "79fc9235a87651a0cd5b93856d4b5696ffb8a0bd26c6f30a1f1402ac8aaad195",
			MetadataBlake2b256: "0x2e1d4f992a978fdd58652c8cf434c26bb8f89170e6a0fdbc9362b29e8fe8a835",
		},
		453: {
			SpecVersion: 453, SourceRefKind: "tag", SourceRefName: "v453", SourceCommit: "823bdcbc58a29f60b243be4737a7c72b34ac7d93",
			ObservationBlock: 7925883, ObservationBlockHash: "0x87c707403ffe5b36afb7796e1bd84126cdbcf181a61f97c3f1491c8354ae96f0",
			CodeSource: "github-release", CodeSize: 2515038, CodeSHA256: "9e51859faf28a69365005e7dd7f152f239a305c468869b2f54303aba938d840e",
			CodeBlake2b256: "0xabe169cc148e2a63068772788c191fa6566f02aa2ea9afb80cdeb28217bab4d4",
			MetadataSize:   334667, MetadataSHA256: "99380e7d01eccc41ffa1304e782658c86b38ba9986acefa371e79ad367f76658",
			MetadataBlake2b256: "0xb00e7e0188d537136a973df4d5c5f2c86ef903ffff49c1cf8d129dabc98b07ce",
		},
		454: {
			SpecVersion: 454, SourceRefKind: "tag", SourceRefName: "v454", SourceCommit: "14cde6410fe8ec81a940e290c56f94a632a0988d",
			ObservationBlock: 7934387, ObservationBlockHash: "0x5b3f3455125d78812299002a1926792a6876b03ac636ae53e93e4115f15a392b",
			CodeSource: "github-release", CodeSize: 2515968, CodeSHA256: "a55e76b4f4620bcdb4c787e499c87a35abb9913ba4cde001b08a00d1945ac4db",
			CodeBlake2b256: "0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef",
			MetadataSize:   334642, MetadataSHA256: "b592bafacd0f3cce1340a91f237f82a531968bd833cbd27339328c80ce92b1cf",
			MetadataBlake2b256: "0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702",
		},
	}
	for index, artifact := range artifactManifest.Artifacts {
		want, ok := wantArtifacts[artifact.SpecVersion]
		if !ok || artifact.SpecVersion != uint32(451+index) {
			t.Fatalf("runtime metadata artifact %d has unexpected version %d", index, artifact.SpecVersion)
		}
		if artifact.CodeURL != nil {
			want.CodeURL = artifact.CodeURL
			wantURL := fmt.Sprintf("https://github.com/RaoFoundation/subtensor/releases/download/v%d/subtensor.wasm", artifact.SpecVersion)
			if artifact.CodeSource != "github-release" || *artifact.CodeURL != wantURL {
				t.Fatalf("runtime metadata artifact %d URL/source=%s/%s", artifact.SpecVersion, *artifact.CodeURL, artifact.CodeSource)
			}
		} else if artifact.SpecVersion != 451 || artifact.CodeSource != "substrate-storage" {
			t.Fatalf("runtime metadata artifact %d lacks its release URL", artifact.SpecVersion)
		}
		if artifact != want {
			t.Fatalf("runtime metadata artifact %d=%+v, want %+v", artifact.SpecVersion, artifact, want)
		}
	}

	checkerBytes, err := os.ReadFile("../scripts/check-runtime-v454-source.sh")
	if err != nil {
		t.Fatal(err)
	}
	checker := string(checkerBytes)
	for _, required := range []string{
		"https://github.com/RaoFoundation/subtensor",
		"https://raw.githubusercontent.com/RaoFoundation/subtensor",
		"v454",
		"14cde6410fe8ec81a940e290c56f94a632a0988d",
		"runtime-v454-source.sha256",
		"runtime-metadata-static-source.sha256",
		"d78d9cc6a6ee4d805f74a35414baaef8be025a5f",
		"da06f033663896ef2fdbbfc3ecc68ca908fba0f5",
		"support/procedural-fork/src/construct_runtime/expand/metadata.rs",
		"runtime/tests/metadata.rs",
		"git ls-remote --heads",
		"git ls-remote --tags",
		"SUBTENSOR_RUNTIME_SOURCE",
		"status --porcelain=v1 --untracked-files=all",
		"sha256sum",
	} {
		if !strings.Contains(checker, required) {
			t.Errorf("runtime source checker omits %q", required)
		}
	}
	artifactCheckerBytes, err := os.ReadFile("../scripts/check-runtime-metadata-artifacts.sh")
	if err != nil {
		t.Fatal(err)
	}
	artifactChecker := string(artifactCheckerBytes)
	for _, required := range []string{
		"runtime-metadata-artifacts.json",
		"tools/runtime-metadata-probe",
		"Cargo.toml",
		"chain_getBlockHash",
		"state_getStorageHash",
		"state_getStorage",
		"cargo build",
		"--locked",
		"probe_process_ids",
		"wait \"${probe_process_ids[$spec_version]}\"",
		"sha256sum",
		"runtime metadata artifacts verified",
	} {
		if !strings.Contains(artifactChecker, required) {
			t.Errorf("runtime metadata artifact checker omits %q", required)
		}
	}

	for _, scriptPath := range []string{"../scripts/test-release-1.0-local.sh", "../scripts/test-release-1.0-producer-gate.sh"} {
		scriptBytes, err := os.ReadFile(scriptPath)
		if err != nil {
			t.Fatal(err)
		}
		script := string(scriptBytes)
		attestation := strings.Index(script, "check-runtime-v454-source.sh")
		artifactAttestation := strings.Index(script, "check-runtime-metadata-artifacts.sh")
		preflight := strings.Index(script, "check-release-source-freeze.sh")
		if strings.Count(script, "check-runtime-v454-source.sh") != 1 || strings.Count(script, "check-runtime-metadata-artifacts.sh") != 1 ||
			attestation <= preflight || artifactAttestation <= attestation {
			t.Errorf("%s does not attest runtime source and exact artifacts immediately after source-freeze preflight", scriptPath)
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

// Keep the live launch path bounded without weakening acceptance. Deterministic
// semantic fixtures qualify the evidence schema here, while production capture
// reconstruction and broad suites remain in the concurrent offline phase.
func TestProducerGateSeparatesCaptureFromProductionAnalysis(t *testing.T) {
	scriptBytes, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	assertReleaseConnectPolicySelector(t, script, "policy_tests")
	for _, required := range []string{
		"go test ./validator ./sim-testnet -run '^$'",
		"authenticated release driver",
		"ExecutableAttestation|StopExecutableAttestation|ReleaseExecutable|AuthenticateCommandExecutable|RunMain|ParseReleaseExecutableBuildInfo|PushedSNRevision|ParseCurrentSNRevision|ReleaseGitNetworkEnvironment",
		"go test ./sim-testnet -run \"$executable_attestation_tests\"",
		"go test -race ./sim-testnet -run \"$executable_attestation_tests\"",
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
		"Generated policy data is executable release input",
		"BlockerGeneratedTables|BlockerDefaultDataSmoke|BlockerDataGuards|BlockerHashVectors",
		"CfaaPortClassification|CfaaBlockedIps|CfaaDisabled|CfaaIngressMirrorsSourceDrops|CfaaBlockedPrefixInvariant",
		"CfaaBlockedIp4BruteForce|CfaaBlockedIp4ZeroAlloc|CfaaSearch6|CfaaInspectV6|CfaaBlockedPrefix6Invariant|CfaaInspectIcmp",
		"go test . -run \"$policy_tests\"",
		"go test -race . -run \"$policy_tests\"",
		"go test ./blocker ./security -count=1",
		"go test -race ./blocker ./security -count=1",
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
		"go test -race -parallel=4 -timeout 90m ./sim-testnet",
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
